package services

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// =========================================================================
//   三方一致性预检
//
// 目的：在 git push 之前确认每个模块在 Harbor / ArgoCD / GitLab 三方都齐
// 全。任一缺失立即标记给用户，**不阻止提交**：缺失项自动跳过、通过项继续走
// 流水线（按用户的设计）。
//
// 调用时机：
//   - PreviewImage 接口：返回 PrecheckResult 给前端展示分组
//   - UpdateImage 内部：submit 前再调一次（强制 refresh ArgoCD 缓存），用最新数据决定哪些跳过
//
// Restart 路径不需要 Harbor 校验（不改 tag），只查 ArgoCD + GitLab 即可
// =========================================================================

// PrecheckItem 单模块的预检结果
type PrecheckItem struct {
	Module    string `json:"module"`
	NewTag    string `json:"new_tag,omitempty"`    // restart 路径为空
	OldTag    string `json:"old_tag,omitempty"`    // restart 路径为空
	ArgocdApp string `json:"argocd_app"`           // 拼出的 ArgoCD app 名
	Pass      bool   `json:"pass"`                 // 全部通过
	HarborOK  bool   `json:"harbor_ok"`            // tag 在 Harbor (restart 路径恒 true)
	ArgocdOK  bool   `json:"argocd_ok"`            // app 在 ArgoCD
	GitlabOK  bool   `json:"gitlab_ok"`            // chart 目录在 git repo
	Reason    string `json:"reason,omitempty"`     // 不通过的中文原因（面向用户）
}

// PrecheckBundle 一次预检的完整结果
type PrecheckBundle struct {
	Passed []PrecheckItem `json:"passed"`
	Failed []PrecheckItem `json:"failed"`
	// 缓存上次刷新时刻（让前端展示"X 秒前同步"）
	ArgocdCacheAt int64 `json:"argocd_cache_at,omitempty"` // unix seconds
}

// PrecheckInput 调用方传一次性数据，避免在这里查 DB
type PrecheckInput struct {
	// 必填
	ProjectEnvName  string
	GitCacheRoot    string  // git_cache 目录绝对路径（拼 chart 目录用）
	ChartBasePath   string  // 模块 chart 相对路径前缀，如 charts/g32-uat
	ProjectEnvLower string  // ArgoCD app 名后缀，小写 env name
	HarborClient    *HarborClient // 可空 (Harbor 未配置)
	ArgocdClient    *ArgocdClient // 可空（restart 模式可用，但通常都需要）
	ArgocdCache     *ArgocdAppCache

	// 模块清单 + 目标 tag（restart 路径 NewTag 留空表示"不校验 Harbor"）
	Items []PrecheckRequestItem
}

// PrecheckRequestItem 输入的单模块项
type PrecheckRequestItem struct {
	Module          string
	NewTag          string // 为空 = restart 路径，跳过 Harbor 校验
	OldTag          string
	ImageRepository string // 完整 image，如 harbor.slileisure.com/g32/user-client-backend
}

// RunPrecheck 跑一遍三方校验。先强刷 ArgoCD 缓存（不计入失败），然后并发跑各模块。
//
// 注意：这里**不**因为某个外部服务挂了就让全部模块 fail；如果某子检查失败（如 Harbor 401），
// 把那一项标 false 但其他项继续。具体哪个挂了由 Reason 透传。
func RunPrecheck(ctx context.Context, in PrecheckInput) PrecheckBundle {
	// 1. 强刷 ArgoCD 缓存（提交时调用）
	var argocdCacheAt int64
	if in.ArgocdClient != nil && in.ArgocdCache != nil {
		_, _ = in.ArgocdCache.RefreshNow(ctx, in.ArgocdClient)
		if t, ok := in.ArgocdCache.LastRefreshedAt(in.ArgocdClient); ok {
			argocdCacheAt = t.Unix()
		}
	}

	// 2. 并发跑每个模块的三方检查（限并发 10）
	results := make([]PrecheckItem, len(in.Items))
	sem := make(chan struct{}, 10)
	var wg sync.WaitGroup
	for i, item := range in.Items {
		i, item := i, item
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			results[i] = checkOne(ctx, in, item)
		}()
	}
	wg.Wait()

	// 3. 分组
	var passed, failed []PrecheckItem
	for _, r := range results {
		if r.Pass {
			passed = append(passed, r)
		} else {
			failed = append(failed, r)
		}
	}
	return PrecheckBundle{Passed: passed, Failed: failed, ArgocdCacheAt: argocdCacheAt}
}

// checkOne 跑单个模块的三方检查
func checkOne(ctx context.Context, in PrecheckInput, item PrecheckRequestItem) PrecheckItem {
	out := PrecheckItem{
		Module:    item.Module,
		NewTag:    item.NewTag,
		OldTag:    item.OldTag,
		ArgocdApp: strings.ToLower(item.Module) + "-" + in.ProjectEnvLower,
		HarborOK:  true, // restart 模式没 NewTag → 默认 true
		ArgocdOK:  true,
		GitlabOK:  true,
	}

	// --- Harbor: 仅 NewTag 非空时校验 ---
	if item.NewTag != "" && in.HarborClient != nil {
		host, projectRepo, ok := SplitImageRepoForHarbor(item.ImageRepository)
		if !ok {
			out.HarborOK = false
			out.Reason = "镜像地址格式异常，无法在 Harbor 校验"
		} else if !strings.Contains(in.HarborClient.BaseURL, host) {
			// 镜像不在我们配置的 Harbor → 跳过校验，pass
			// 用户可能用了别家 registry，不强制校验
		} else {
			exists, err := in.HarborClient.VerifyTag(ctx, projectRepo, item.NewTag)
			if err != nil {
				out.HarborOK = false
				out.Reason = "Harbor 校验失败：" + err.Error()
			} else if !exists {
				out.HarborOK = false
				out.Reason = "Harbor 找不到镜像版本：" + item.NewTag
			}
		}
	}

	// --- ArgoCD: 看 app 是否在缓存里 ---
	if in.ArgocdClient != nil && in.ArgocdCache != nil {
		exists, _, err := in.ArgocdCache.Has(ctx, in.ArgocdClient, out.ArgocdApp)
		if err != nil {
			out.ArgocdOK = false
			if out.Reason == "" {
				out.Reason = "ArgoCD 校验失败：" + err.Error()
			}
		} else if !exists {
			out.ArgocdOK = false
			if out.Reason == "" {
				out.Reason = "ArgoCD 尚未接入该服务：" + out.ArgocdApp
			}
		}
	}

	// --- GitLab: 看 chart 目录是否存在（已 clone 到 git_cache） ---
	if in.GitCacheRoot != "" && in.ChartBasePath != "" {
		base := filepath.Join(in.GitCacheRoot, in.ProjectEnvName, in.ChartBasePath, item.Module)
		if st, err := os.Stat(base); err != nil || !st.IsDir() {
			out.GitlabOK = false
			if out.Reason == "" {
				out.Reason = "GitLab 仓库未找到模块：" + item.Module
			}
		}
	}

	out.Pass = out.HarborOK && out.ArgocdOK && out.GitlabOK
	return out
}
