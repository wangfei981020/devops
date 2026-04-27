package handlers

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"opsplatform-deploy-backend/crypto"
	"opsplatform-deploy-backend/database"
	"opsplatform-deploy-backend/services"
)

// =========================================================================
//   Harbor + ArgoCD App Cache 相关 HTTP 端点
//
// - GET  /api/harbor/tags?project_env_id=N&module=xxx&limit=50
// - POST /api/global-config/test-harbor
// - POST /api/argocd-app-cache/refresh?project_env_id=N
// =========================================================================

// 单例 + 配置变化时 invalidate
var (
	harborClientMu       sync.Mutex
	harborClientInstance *services.HarborClient
	harborClientCfgKey   string // 当前实例对应的 (url|user|token-bcrypt-of-secret) hash 短串，配置变化重建
)

// getHarborClient 拉 / 复用 HarborClient 单例
//
//	Harbor 配置变化时（保存系统设置）外部调 InvalidateHarborClient() 清掉，
//	下次再 getHarborClient 重建
func getHarborClient() *services.HarborClient {
	harborClientMu.Lock()
	defer harborClientMu.Unlock()
	var url, user, encToken string
	_ = database.DB.QueryRow(`SELECT IFNULL(harbor_url,''), IFNULL(harbor_user,''), IFNULL(harbor_token,'')
		FROM global_config WHERE id=1`).Scan(&url, &user, &encToken)
	url = strings.TrimSpace(url)
	user = strings.TrimSpace(user)
	if url == "" || user == "" || encToken == "" {
		harborClientInstance = nil
		harborClientCfgKey = ""
		return nil
	}
	token, _ := crypto.Decrypt(encToken)
	if token == "" {
		harborClientInstance = nil
		harborClientCfgKey = ""
		return nil
	}
	// cfg key 用 enc 字符串本身（已加密，不会泄露明文；DB 改了 enc 也变）
	cfgKey := url + "|" + user + "|" + encToken
	if harborClientInstance != nil && harborClientCfgKey == cfgKey {
		return harborClientInstance
	}
	harborClientInstance = services.NewHarborClient(url, user, token)
	harborClientCfgKey = cfgKey
	return harborClientInstance
}

// InvalidateHarborClient 当用户保存了新的 Harbor 配置时调用，强制下次重建
func InvalidateHarborClient() {
	harborClientMu.Lock()
	defer harborClientMu.Unlock()
	if harborClientInstance != nil {
		harborClientInstance.InvalidateAll()
	}
	harborClientInstance = nil
	harborClientCfgKey = ""
}

// HandleListHarborTags GET /api/harbor/tags?project_env_id=N&module=xxx&page=1&limit=100
//
//	拉某模块对应 Harbor repo 第 page 页的 tag (按推送时间倒序，每页 limit 个)
//	module 来自 modules 表，需要先扫描出来才能查到 image_repository
//	Response 含 has_more 让前端决定是否还能加载更早
func HandleListHarborTags(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	envID, _ := strconv.ParseInt(q.Get("project_env_id"), 10, 64)
	module := strings.TrimSpace(q.Get("module"))
	page, _ := strconv.Atoi(q.Get("page"))
	limit, _ := strconv.Atoi(q.Get("limit"))
	if envID <= 0 || module == "" {
		JSONError(w, 40001, "project_env_id / module 必填")
		return
	}
	if !IsEnvIDAllowed(r, envID) {
		JSONError(w, 40300, "无权访问该环境")
		return
	}

	// 查 modules 表拿 image_repository
	var imageRepo string
	err := database.DB.QueryRow(
		`SELECT IFNULL(image_repository,'') FROM module WHERE project_env_id=? AND name=?`,
		envID, module).Scan(&imageRepo)
	if err != nil || imageRepo == "" {
		JSONError(w, 40400, "模块未在系统中登记，请先到「项目配置」扫描该环境")
		return
	}
	_, projectRepo, ok := services.SplitImageRepoForHarbor(imageRepo)
	if !ok {
		JSONError(w, 40001, "镜像地址格式异常: "+imageRepo)
		return
	}

	hc := getHarborClient()
	if hc == nil {
		JSONError(w, 40001, "未配置 Harbor，请到系统设置 → Harbor 镜像仓库 配置")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	res, err := hc.ListTags(ctx, projectRepo, page, limit)
	if err != nil {
		JSONError(w, 50001, err.Error())
		return
	}
	if res == nil {
		res = &services.ListTagsResult{}
	}
	JSONSuccess(w, map[string]interface{}{
		"module":     module,
		"image_repo": imageRepo,
		"page":       page,
		"tags":       res.Tags,    // [{name, pushed_at, digest, size_mb}]
		"has_more":   res.HasMore, // 是否还有下一页
		"fetched_at": time.Now().Unix(),
	})
}

// HandleTestHarbor POST /api/global-config/test-harbor
//
//	body: {"harbor_url": "...", "harbor_user": "...", "harbor_token": "..."}
//	任一字段空则从 DB 取已有值
func HandleTestHarbor(w http.ResponseWriter, r *http.Request) {
	var req struct {
		HarborURL   string `json:"harbor_url"`
		HarborUser  string `json:"harbor_user"`
		HarborToken string `json:"harbor_token"`
	}
	_ = DecodeJSON(newDiscardW(), r, &req)
	url, user, token := strings.TrimSpace(req.HarborURL), strings.TrimSpace(req.HarborUser), req.HarborToken

	if url == "" || user == "" || token == "" {
		var dbURL, dbUser, dbEnc string
		_ = database.DB.QueryRow(`SELECT IFNULL(harbor_url,''), IFNULL(harbor_user,''), IFNULL(harbor_token,'')
			FROM global_config WHERE id=1`).Scan(&dbURL, &dbUser, &dbEnc)
		if url == "" {
			url = dbURL
		}
		if user == "" {
			user = dbUser
		}
		if token == "" {
			dec, _ := crypto.Decrypt(dbEnc)
			token = dec
		}
	}
	if url == "" || user == "" || token == "" {
		JSONError(w, 40001, "harbor_url / user / token 必填")
		return
	}

	tester := services.NewHarborClient(url, user, token)
	ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
	defer cancel()
	if err := tester.TestConnection(ctx); err != nil {
		JSONError(w, 50001, "Harbor 连接失败："+err.Error())
		return
	}
	JSONSuccess(w, map[string]interface{}{"ok": true, "url": url, "user": user})
}

// HandleRefreshArgocdAppCache POST /api/argocd-app-cache/refresh?project_env_id=N
//
//	用户点 [刷新] 按钮 → 强制刷该 env 关联 ArgoCD 实例的 app 列表缓存
func HandleRefreshArgocdAppCache(w http.ResponseWriter, r *http.Request) {
	envID, _ := strconv.ParseInt(r.URL.Query().Get("project_env_id"), 10, 64)
	if envID <= 0 {
		JSONError(w, 40001, "project_env_id 必填")
		return
	}
	if !IsEnvIDAllowed(r, envID) {
		JSONError(w, 40300, "无权访问该环境")
		return
	}
	p, err := LoadProjectEnvDecrypted(envID)
	if err != nil {
		JSONError(w, 40400, "project_env 不存在")
		return
	}
	argoURL, argoToken, err := ResolveArgocdForEnv(p)
	if err != nil {
		JSONError(w, 40001, err.Error())
		return
	}
	client := services.NewArgocdClient(argoURL, argoToken)
	cache := services.GetArgocdAppCache()
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	count, err := cache.RefreshNow(ctx, client)
	if err != nil {
		JSONError(w, 50001, "刷新失败："+err.Error())
		return
	}
	t, _ := cache.LastRefreshedAt(client)
	JSONSuccess(w, map[string]interface{}{
		"app_count":  count,
		"refresh_at": t.Unix(),
	})
}

// LoadGlobalConfigForPrecheck 帮 deploy 流程构建 PrecheckInput
//
//	返回 (harborClient, harborVerifyOnSubmit)
//	harborClient 为 nil 表示 Harbor 未配置；caller 应跳过 Harbor 校验
func LoadGlobalConfigForPrecheck() (hc *services.HarborClient, verify bool) {
	var v int
	_ = database.DB.QueryRow(`SELECT IFNULL(harbor_verify_on_submit, 1) FROM global_config WHERE id=1`).Scan(&v)
	return getHarborClient(), v == 1
}
