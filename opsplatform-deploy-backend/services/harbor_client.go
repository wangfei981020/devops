package services

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// =========================================================================
//   Harbor Client
//
// 用 Robot 账号 + HTTP Basic Auth（跟生产 Jenkins 流水线相同）。
// 仅拉最近 50 条 tag（按推送时间倒序），下拉框够用、不压 Harbor。
//
// 缓存：
//   ListTags / VerifyTag 都按 (project, repo) 缓存 60s
//   submit 路径会调 invalidate 强制刷新；前端手动刷新按钮也走 invalidate
// =========================================================================

const (
	defaultTagPageSize = 50
	maxTagPageSize     = 200
	harborCacheTTL     = 60 * time.Second
	harborHTTPTimeout  = 8 * time.Second
)

// HarborClient 单例使用：URL/User/Token 在配置变化时由 InvalidateHarbor() 触发刷新
type HarborClient struct {
	BaseURL string // e.g. https://harbor.slileisure.com
	User    string // robot 账号，如 robot$public-pull
	Token   string // robot 密码

	HTTP *http.Client

	mu    sync.Mutex
	cache map[string]*harborTagCacheEntry // key = "<proj>/<repo>" → entry
}

type harborTagCacheEntry struct {
	tags     []HarborTag
	cachedAt time.Time
}

// HarborTag 单个 tag 的元数据
type HarborTag struct {
	Name     string    `json:"name"`
	PushedAt time.Time `json:"pushed_at"`
	Digest   string    `json:"digest"`
	SizeMB   float64   `json:"size_mb"`
}

// NewHarborClient 建实例。url 末尾的 / 会被 trim
func NewHarborClient(baseURL, user, token string) *HarborClient {
	return &HarborClient{
		BaseURL: strings.TrimRight(baseURL, "/"),
		User:    user,
		Token:   token,
		HTTP:    &http.Client{Timeout: harborHTTPTimeout},
		cache:   make(map[string]*harborTagCacheEntry),
	}
}

func (c *HarborClient) ok() bool {
	return c != nil && c.BaseURL != "" && c.User != "" && c.Token != ""
}

// authHeader 返回 "Basic base64(user:token)"
// Harbor robot 账号格式 robot$xxx 含 $ 是合法字符，原样拼即可
func (c *HarborClient) authHeader() string {
	creds := c.User + ":" + c.Token
	return "Basic " + base64.StdEncoding.EncodeToString([]byte(creds))
}

// ListTags 拉某 repo 最近 limit 个 tag（按推送时间倒序）
//
//	repo 格式：project/repository，如 "g32/user-client-backend"
//	limit <=0 用默认 50；上限 200
//	带 60s 缓存（同 repo 60s 内不重复调 Harbor）
func (c *HarborClient) ListTags(ctx context.Context, repo string, limit int) ([]HarborTag, error) {
	if !c.ok() {
		return nil, fmt.Errorf("harbor 未配置")
	}
	if limit <= 0 {
		limit = defaultTagPageSize
	}
	if limit > maxTagPageSize {
		limit = maxTagPageSize
	}
	repo = strings.Trim(repo, "/")
	if repo == "" {
		return nil, fmt.Errorf("repo 不能为空")
	}

	// 缓存 hit
	c.mu.Lock()
	if e, ok := c.cache[repo]; ok && time.Since(e.cachedAt) < harborCacheTTL {
		out := append([]HarborTag(nil), e.tags...)
		c.mu.Unlock()
		return out, nil
	}
	c.mu.Unlock()

	parts := strings.SplitN(repo, "/", 2)
	if len(parts) != 2 {
		return nil, fmt.Errorf("repo 格式必须是 project/repo，得到 %q", repo)
	}
	project := url.PathEscape(parts[0])
	repoSegment := url.PathEscape(strings.ReplaceAll(parts[1], "/", "%2F"))

	apiURL := fmt.Sprintf(
		"%s/api/v2.0/projects/%s/repositories/%s/artifacts?page=1&page_size=%d&with_tag=true&sort=-push_time",
		c.BaseURL, project, repoSegment, limit,
	)

	req, _ := http.NewRequestWithContext(ctx, "GET", apiURL, nil)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", c.authHeader())
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("调用 Harbor 失败: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		// 把 Harbor 真实返回的 body 写到 backend log，让运维能查
		log.Printf("[harbor][ListTags] status=%d url=%s body=%s",
			resp.StatusCode, req.URL.String(), truncate(string(body), 1000))
	}
	if resp.StatusCode == 401 || resp.StatusCode == 403 {
		// 透传 Harbor 真实错误 message，前端 toast 显示前 200 字
		return nil, fmt.Errorf("Harbor 拒绝请求 (%d): %s", resp.StatusCode, truncate(string(body), 200))
	}
	if resp.StatusCode == 404 {
		// repo 不存在 — 返回空列表（caller 可据此判定）
		return nil, nil
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("Harbor 返回 %d: %s", resp.StatusCode, truncate(string(body), 200))
	}

	// Harbor v2.0 artifacts 返回数组：
	//   [{ digest, push_time, size, tags: [{name}, ...] }, ...]
	var artifacts []struct {
		Digest   string    `json:"digest"`
		PushTime time.Time `json:"push_time"`
		Size     int64     `json:"size"`
		Tags     []struct {
			Name string `json:"name"`
		} `json:"tags"`
	}
	if err := json.Unmarshal(body, &artifacts); err != nil {
		return nil, fmt.Errorf("解析 Harbor 响应失败: %w", err)
	}

	tags := make([]HarborTag, 0, limit)
	for _, art := range artifacts {
		for _, t := range art.Tags {
			tags = append(tags, HarborTag{
				Name:     t.Name,
				PushedAt: art.PushTime,
				Digest:   art.Digest,
				SizeMB:   float64(art.Size) / 1024.0 / 1024.0,
			})
		}
	}

	// 写缓存
	c.mu.Lock()
	c.cache[repo] = &harborTagCacheEntry{tags: tags, cachedAt: time.Now()}
	c.mu.Unlock()
	return tags, nil
}

// VerifyTag 校验 (repo, tag) 是否存在于 Harbor
//
//	直接调 .../artifacts/<tag>，404 表示不存在
//	成功响应表示存在，error 透传上层
//
// 比 ListTags + 内存遍历轻：单 GET，不分页。
func (c *HarborClient) VerifyTag(ctx context.Context, repo, tag string) (bool, error) {
	if !c.ok() {
		return false, fmt.Errorf("harbor 未配置")
	}
	repo = strings.Trim(repo, "/")
	tag = strings.TrimSpace(tag)
	if repo == "" || tag == "" {
		return false, fmt.Errorf("repo / tag 不能为空")
	}
	parts := strings.SplitN(repo, "/", 2)
	if len(parts) != 2 {
		return false, fmt.Errorf("repo 格式必须是 project/repo，得到 %q", repo)
	}
	project := url.PathEscape(parts[0])
	repoSegment := url.PathEscape(strings.ReplaceAll(parts[1], "/", "%2F"))
	apiURL := fmt.Sprintf("%s/api/v2.0/projects/%s/repositories/%s/artifacts/%s",
		c.BaseURL, project, repoSegment, url.PathEscape(tag))

	req, _ := http.NewRequestWithContext(ctx, "GET", apiURL, nil)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", c.authHeader())
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return false, fmt.Errorf("调用 Harbor 失败: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode == 200 {
		return true, nil
	}
	if resp.StatusCode == 404 {
		return false, nil
	}
	if resp.StatusCode >= 400 {
		log.Printf("[harbor][VerifyTag] status=%d url=%s body=%s",
			resp.StatusCode, req.URL.String(), truncate(string(body), 1000))
	}
	if resp.StatusCode == 401 || resp.StatusCode == 403 {
		return false, fmt.Errorf("Harbor 拒绝请求 (%d): %s", resp.StatusCode, truncate(string(body), 200))
	}
	return false, fmt.Errorf("Harbor 返回异常状态码 %d", resp.StatusCode)
}

// TestConnection 给 「测试连接」 按钮用 — 调一个 Robot 账号也能访问的 API
//
//	用 GET /api/v2.0/projects?page=1&page_size=1
//	  ✓ Robot 账号可访问（"users/current" 仅人类用户可访问，Robot 会 401，跟密码对错无关）
//	  ✓ Anonymous 也能调（返公开 projects），所以 401 必然代表 token 无效
//
// 行为：
//   - 200 → 凭证有效（Robot/User 都行）
//   - 401 → token 真的无效
//   - 其它 → 连通异常
func (c *HarborClient) TestConnection(ctx context.Context) error {
	if !c.ok() {
		return fmt.Errorf("URL/user/token 必填")
	}
	apiURL := c.BaseURL + "/api/v2.0/projects?page=1&page_size=1"
	req, _ := http.NewRequestWithContext(ctx, "GET", apiURL, nil)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", c.authHeader())
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return fmt.Errorf("无法连通 Harbor: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	log.Printf("[harbor][TestConnection] status=%d url=%s body_len=%d",
		resp.StatusCode, req.URL.String(), len(body))
	if resp.StatusCode == 200 {
		return nil
	}
	if resp.StatusCode == 401 || resp.StatusCode == 403 {
		return fmt.Errorf("user/token 无效 (%d): %s", resp.StatusCode, truncate(string(body), 200))
	}
	return fmt.Errorf("Harbor 返回异常状态码 %d: %s", resp.StatusCode, truncate(string(body), 200))
}

// InvalidateRepo 清掉某 repo 的缓存（提交时强制重拉 / 用户手动刷新）
func (c *HarborClient) InvalidateRepo(repo string) {
	if c == nil {
		return
	}
	c.mu.Lock()
	delete(c.cache, strings.Trim(repo, "/"))
	c.mu.Unlock()
}

// InvalidateAll 清整个客户端缓存（配置变更后调用）
func (c *HarborClient) InvalidateAll() {
	if c == nil {
		return
	}
	c.mu.Lock()
	c.cache = make(map[string]*harborTagCacheEntry)
	c.mu.Unlock()
}

// SplitImageRepoForHarbor 把完整镜像地址拆出 Harbor 域名 + project/repo
//
//	"harbor.slileisure.com/g32/user-client-backend"
//	→ ("harbor.slileisure.com", "g32/user-client-backend", true)
//
// 第三个 bool 表示是否成功拆分。失败时让上层走全量 image 字符串处理。
func SplitImageRepoForHarbor(image string) (host string, projectRepo string, ok bool) {
	image = strings.TrimSpace(image)
	if image == "" {
		return "", "", false
	}
	// 去 protocol 前缀（如果有）
	for _, p := range []string{"https://", "http://"} {
		image = strings.TrimPrefix(image, p)
	}
	// 第一个 / 之前是 host
	idx := strings.Index(image, "/")
	if idx <= 0 || idx == len(image)-1 {
		return "", "", false
	}
	return image[:idx], image[idx+1:], true
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
