package api

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"ops-alert-backend/internal/api/middleware"
	"ops-alert-backend/logx"
)

// 单点登录（OIDC）。
//
// # 为什么做成"什么都能填"
//
// 这套系统交付给多个子公司自建部署，各家用什么身份源事先不知道。
// 端点、claim 名、scope 全部可配 —— 预置几个 IdP 的后果是第四家来了要改代码，
// 而他们拿不到这条路。
//
// # 🔴 claim 默认从 id_token 和 userinfo **两处都取**
//
// 这是踩出来的：MXID 把 name 和 email 只放在 userinfo，不放进 id_token。
// 只读 id_token 的接入方必然报 "Claim not found"，而 IdP 侧看一切正常
// （它确实下发了，只是在另一个端点）。两处都取、userinfo 覆盖 id_token，
// 是唯一不会漏的选择。

type oidcConfig struct {
	Enabled       bool
	DisplayName   string
	Issuer        string
	AutoDiscover  bool
	AuthURL       string
	TokenURL      string
	UserinfoURL   string
	JWKSURL       string
	ClientID      string
	ClientSecret  string
	Scopes        string
	UsernameClaim string
	EmailClaim    string
	NameClaim     string
	GroupsClaim   string
	ClaimSource   string
	JITEnabled    bool
	DefaultRole   string
	RoleMapping   map[string]string
}

func (s *Server) loadOIDC(ctx context.Context) (*oidcConfig, error) {
	p := s.st.Platform("api/oidc.go")
	var c oidcConfig
	var enabled, autoDisc, jit int
	var secretEnc sql.NullString
	var mapping []byte
	err := p.QueryRow(ctx, `SELECT enabled, display_name, issuer, auto_discover,
			auth_url, token_url, userinfo_url, jwks_url, client_id, client_secret_enc,
			scopes, username_claim, email_claim, name_claim, groups_claim, claim_source,
			jit_enabled, default_role, role_mapping
		FROM oidc_config WHERE id = 1`).
		Scan(&enabled, &c.DisplayName, &c.Issuer, &autoDisc, &c.AuthURL, &c.TokenURL,
			&c.UserinfoURL, &c.JWKSURL, &c.ClientID, &secretEnc, &c.Scopes,
			&c.UsernameClaim, &c.EmailClaim, &c.NameClaim, &c.GroupsClaim, &c.ClaimSource,
			&jit, &c.DefaultRole, &mapping)
	if err != nil {
		return nil, err
	}
	c.Enabled, c.AutoDiscover, c.JITEnabled = enabled == 1, autoDisc == 1, jit == 1
	if secretEnc.Valid && secretEnc.String != "" {
		if plain, err := s.cipher.Decrypt(secretEnc.String); err == nil {
			c.ClientSecret = plain
		} else {
			// 解不开通常是 ALERT_AES_KEY 换过。说出来 ——
			// 静默当成空密钥的话，登录会在 token 交换那步失败，
			// 报错是 "invalid_client"，指向的方向完全错了
			logx.J("oidc", "secret_decrypt_failed", map[string]any{
				"err":  err.Error(),
				"note": "客户端密钥解不开（ALERT_AES_KEY 是否变更过？），需要重新填一遍",
			})
		}
	}
	_ = json.Unmarshal(mapping, &c.RoleMapping)
	return &c, nil
}

// discovered 是从 /.well-known/openid-configuration 拉到的端点。
type discovered struct {
	Issuer      string `json:"issuer"`
	AuthURL     string `json:"authorization_endpoint"`
	TokenURL    string `json:"token_endpoint"`
	UserinfoURL string `json:"userinfo_endpoint"`
	JWKSURL     string `json:"jwks_uri"`
}

// resolveEndpoints 补齐端点。自动发现失败时**返回错误而不是回落到手填字段**：
// 回落的话，用户以为自动发现成功了，实际用的是几个空串，
// 表现为跳转到一个空 URL —— 浏览器只会说"网址无效"。
func (s *Server) resolveEndpoints(ctx context.Context, c *oidcConfig) error {
	if !c.AutoDiscover {
		if c.AuthURL == "" || c.TokenURL == "" {
			return fmt.Errorf("关闭了自动发现，但授权端点或令牌端点是空的")
		}
		return nil
	}
	if c.Issuer == "" {
		return fmt.Errorf("开启了自动发现，但 issuer 是空的")
	}
	u := strings.TrimSuffix(c.Issuer, "/") + "/.well-known/openid-configuration"
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	resp, err := (&http.Client{Timeout: 10 * time.Second}).Do(req)
	if err != nil {
		return fmt.Errorf("拉取 %s 失败: %w", u, err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("拉取 %s 返回 %d：%s", u, resp.StatusCode, truncateStr(string(body), 200))
	}
	var d discovered
	if err := json.Unmarshal(body, &d); err != nil {
		return fmt.Errorf("%s 返回的不是合法的发现文档: %w", u, err)
	}
	if d.AuthURL == "" || d.TokenURL == "" {
		return fmt.Errorf("发现文档里缺 authorization_endpoint 或 token_endpoint。" +
			"有的 IdP 不提供完整的发现文档，请关掉自动发现并手填端点")
	}
	c.AuthURL, c.TokenURL, c.JWKSURL = d.AuthURL, d.TokenURL, d.JWKSURL
	if d.UserinfoURL != "" {
		c.UserinfoURL = d.UserinfoURL
	}
	return nil
}

// redirectURI 回调地址。
//
// 🔴 必须与 IdP 里注册的**完全一致**（含协议、端口、路径），差一个字符就被拒。
//
// ⚠️ 用 X-Forwarded-* 而不是 c.Request.Host：服务在 Ingress/Istio 后面时，
// Host 是内部地址。反代那侧也必须转 `$http_host` 而不是 `$host` ——
// `$host` **不含端口**，非标准端口的部署会算出一个少了端口的 redirect_uri，
// 而 IdP 只会回一句 "redirect_uri mismatch"，不告诉你差在哪。
func redirectURI(c *gin.Context) string {
	scheme := "https"
	if v := c.GetHeader("X-Forwarded-Proto"); v != "" {
		scheme = strings.Split(v, ",")[0]
	} else if c.Request.TLS == nil {
		scheme = "http"
	}
	host := c.GetHeader("X-Forwarded-Host")
	if host == "" {
		host = c.Request.Host
	}
	return fmt.Sprintf("%s://%s/api/v1/auth/oidc/callback", scheme, strings.Split(host, ",")[0])
}

// oidcPublicConfig 登录页用的公开信息：要不要显示 SSO 按钮、按钮上写什么。
//
// ⚠️ 这是**未登录**就能调的接口，所以只回这两项。
// 把 issuer、client_id 一起回出去等于向公网泄露身份源拓扑。
func (s *Server) oidcPublicConfig(c *gin.Context) {
	cfg, err := s.loadOIDC(c.Request.Context())
	if err != nil {
		// 配置读不出来时说"没开 SSO"是安全的降级：登录页照常显示密码框。
		// 但要记日志——静默的话，"SSO 按钮不见了"会查很久
		logx.J("oidc", "public_config_error", map[string]any{"err": err.Error()})
		c.JSON(http.StatusOK, gin.H{"enabled": false})
		return
	}
	name := cfg.DisplayName
	if name == "" {
		name = "单点登录"
	}
	c.JSON(http.StatusOK, gin.H{"enabled": cfg.Enabled, "display_name": name})
}

// oidcLogin 发起登录：生成 state/nonce 落库，跳到 IdP。
func (s *Server) oidcLogin(c *gin.Context) {
	cfg, err := s.loadOIDC(c.Request.Context())
	if err != nil || !cfg.Enabled {
		c.JSON(http.StatusBadRequest, gin.H{"error": "sso_disabled", "detail": "未启用单点登录"})
		return
	}
	if err := s.resolveEndpoints(c.Request.Context(), cfg); err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "endpoint_error", "detail": err.Error()})
		return
	}
	state, nonce := randToken(), randToken()
	p := s.st.Platform("api/oidc.go")
	if _, err := p.Exec(c.Request.Context(),
		`INSERT INTO oidc_states (state, nonce, redirect) VALUES (?, ?, ?)`,
		state, nonce, c.Query("redirect")); err != nil {
		abortQuery(c, err)
		return
	}
	q := url.Values{
		"response_type": {"code"},
		"client_id":     {cfg.ClientID},
		"redirect_uri":  {redirectURI(c)},
		"scope":         {cfg.Scopes},
		"state":         {state},
		"nonce":         {nonce},
	}
	c.Redirect(http.StatusFound, cfg.AuthURL+"?"+q.Encode())
}

// oidcCallback 处理回调：换 token、取 claim、JIT 建号、发本地会话。
func (s *Server) oidcCallback(c *gin.Context) {
	fail := func(reason string) {
		// ⚠️ 失败要带着原因跳回登录页，**不能只回一个 JSON**。
		// 用户是被浏览器重定向过来的，看到一坨 JSON 会以为系统崩了。
		logx.J("oidc", "callback_failed", map[string]any{"reason": reason})
		c.Redirect(http.StatusFound, "/#/login?sso_error="+url.QueryEscape(reason))
	}
	if e := c.Query("error"); e != "" {
		// IdP 侧就拒了（用户取消授权、应用没被授权）。原样带出来 ——
		// 换成自己的话术会丢掉 IdP 给的具体原因
		fail(e + ": " + c.Query("error_description"))
		return
	}
	code, state := c.Query("code"), c.Query("state")
	if code == "" || state == "" {
		fail("回调缺少 code 或 state")
		return
	}

	ctx := c.Request.Context()
	p := s.st.Platform("api/oidc.go")
	var nonce string
	var createdAt time.Time
	if err := p.QueryRow(ctx, `SELECT nonce, created_at FROM oidc_states WHERE state = ?`, state).
		Scan(&nonce, &createdAt); err != nil {
		// state 找不到 = 伪造的回调，或者这条 state 已经用过（重放）。
		// 也可能是超过 10 分钟的陈旧登录 —— 三者对用户都是"重新登录一次"
		fail("登录状态已失效，请重新发起登录")
		return
	}
	// 用完即删。不删的话同一个 state 可以被重放
	_, _ = p.Exec(ctx, `DELETE FROM oidc_states WHERE state = ?`, state)
	if time.Since(createdAt) > 10*time.Minute {
		fail("登录状态已过期，请重新发起登录")
		return
	}

	cfg, err := s.loadOIDC(ctx)
	if err != nil || !cfg.Enabled {
		fail("单点登录未启用")
		return
	}
	if err := s.resolveEndpoints(ctx, cfg); err != nil {
		fail("身份源端点解析失败：" + err.Error())
		return
	}

	claims, err := s.exchangeAndFetchClaims(ctx, cfg, code, redirectURI(c))
	if err != nil {
		fail(err.Error())
		return
	}

	username := claimStr(claims, cfg.UsernameClaim)
	if username == "" {
		// 🔴 这是最常见的接入失败。把**实际拿到的 claim 名**列出来，
		// 否则用户只知道"用户名为空"，不知道该把 username_claim 改成什么。
		fail(fmt.Sprintf("身份源没有下发 %q。实际拿到的字段有：%s。"+
			"请在 SSO 配置里把「用户名字段」改成其中之一，"+
			"或确认「claim 来源」选的是「两处都取」",
			cfg.UsernameClaim, strings.Join(claimNames(claims), ", ")))
		return
	}

	token, err := s.upsertSSOUser(ctx, cfg, username,
		claimStr(claims, cfg.EmailClaim), claimStr(claims, cfg.NameClaim),
		claimList(claims, cfg.GroupsClaim))
	if err != nil {
		fail(err.Error())
		return
	}
	// 令牌通过 URL 片段回传（不是 query）：片段不会进服务端日志、
	// 不会进 Referer 头。前端读完立刻从地址栏里清掉
	c.Redirect(http.StatusFound, "/#/sso?token="+url.QueryEscape(token))
}

// exchangeAndFetchClaims 换 token 并取 claim。
//
// 🔴 默认把 id_token 与 userinfo **两处的 claim 合并**（userinfo 覆盖 id_token）。
// 只读一处必然在某些 IdP 上漏字段，而漏的表现是"Claim not found"，
// 排查方向会被引向 IdP 配置（那边看起来完全正常）。
func (s *Server) exchangeAndFetchClaims(ctx context.Context, cfg *oidcConfig,
	code, redirect string,
) (map[string]any, error) {
	form := url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"redirect_uri":  {redirect},
		"client_id":     {cfg.ClientID},
		"client_secret": {cfg.ClientSecret},
	}
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, cfg.TokenURL,
		strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := (&http.Client{Timeout: 15 * time.Second}).Do(req)
	if err != nil {
		return nil, fmt.Errorf("换取令牌失败：%v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK {
		// ⚠️ 把 IdP 的原文带出来。它通常写着 invalid_client /
		// redirect_uri mismatch —— 那才是能直接指向问题的东西
		return nil, fmt.Errorf("身份源拒绝了令牌请求（%d）：%s",
			resp.StatusCode, truncateStr(string(body), 300))
	}
	var tok struct {
		AccessToken string `json:"access_token"`
		IDToken     string `json:"id_token"`
	}
	if err := json.Unmarshal(body, &tok); err != nil {
		return nil, fmt.Errorf("令牌响应不是合法 JSON：%v", err)
	}

	merged := map[string]any{}
	if cfg.ClaimSource != "userinfo" && tok.IDToken != "" {
		if c, err := decodeJWTPayload(tok.IDToken); err == nil {
			for k, v := range c {
				merged[k] = v
			}
		} else {
			logx.J("oidc", "id_token_decode_failed", map[string]any{"err": err.Error()})
		}
	}
	if cfg.ClaimSource != "id_token" && cfg.UserinfoURL != "" && tok.AccessToken != "" {
		if c, err := fetchUserinfo(ctx, cfg.UserinfoURL, tok.AccessToken); err == nil {
			// userinfo 覆盖 id_token：同一个字段两处都有时，
			// userinfo 那份通常更完整（id_token 会为了体积裁字段）
			for k, v := range c {
				merged[k] = v
			}
		} else {
			// ⚠️ userinfo 取不到不算致命（id_token 可能已经够了），
			// 但必须记 —— 缺字段时这条日志是唯一的线索
			logx.J("oidc", "userinfo_failed", map[string]any{
				"err": err.Error(), "note": "只用 id_token 里的 claim 继续",
			})
		}
	}
	if len(merged) == 0 {
		return nil, fmt.Errorf("id_token 和 userinfo 都没有拿到任何字段。" +
			"请检查 scope 是否包含 openid，以及「claim 来源」的设置")
	}
	return merged, nil
}

func fetchUserinfo(ctx context.Context, u, accessToken string) (map[string]any, error) {
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	req.Header.Set("Authorization", "Bearer "+accessToken)
	resp, err := (&http.Client{Timeout: 10 * time.Second}).Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("userinfo 返回 %d: %s", resp.StatusCode, truncateStr(string(body), 200))
	}
	var out map[string]any
	return out, json.Unmarshal(body, &out)
}

// decodeJWTPayload 只解 payload，**不验签**。
//
// ⚠️ 不验签在这里是安全的：id_token 是我们自己刚从 token_endpoint
// 用 client_secret 通过 TLS 换回来的，不经过用户的手。
// 如果哪天改成接受前端传来的 id_token（隐式流），**必须**加上 JWKS 验签 ——
// 那时不验签等于任何人都能伪造身份。
func decodeJWTPayload(tok string) (map[string]any, error) {
	parts := strings.Split(tok, ".")
	if len(parts) != 3 {
		return nil, fmt.Errorf("id_token 不是三段式 JWT")
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, err
	}
	var out map[string]any
	return out, json.Unmarshal(raw, &out)
}

// claimStr 取字符串 claim，支持 `a.b` 形式的嵌套路径
// （Keycloak 的角色在 realm_access.roles 里）。
func claimStr(m map[string]any, path string) string {
	v := claimAt(m, path)
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

// claimList 取字符串数组 claim。
//
// ⚠️ 单个值也要当成一元数组：有的 IdP 在只有一个群组时下发字符串而不是数组，
// 只认数组的话那个人的角色映射会静默不生效。
func claimList(m map[string]any, path string) []string {
	switch v := claimAt(m, path).(type) {
	case []any:
		out := make([]string, 0, len(v))
		for _, x := range v {
			if s, ok := x.(string); ok {
				out = append(out, s)
			}
		}
		return out
	case string:
		if v == "" {
			return nil
		}
		return []string{v}
	}
	return nil
}

func claimAt(m map[string]any, path string) any {
	if path == "" {
		return nil
	}
	var cur any = m
	for _, k := range strings.Split(path, ".") {
		mm, ok := cur.(map[string]any)
		if !ok {
			return nil
		}
		cur = mm[k]
	}
	return cur
}

// claimNames 列出实际拿到的 claim 名，用于接入失败时的提示。
func claimNames(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sortStrings(out)
	if len(out) > 25 {
		out = append(out[:25], "…")
	}
	return out
}

func randToken() string {
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

func truncateStr(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}

func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] < s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}

// upsertSSOUser 按 SSO 身份找/建本地账号，返回本地会话令牌。
func (s *Server) upsertSSOUser(ctx context.Context, cfg *oidcConfig,
	username, email, displayName string, groups []string,
) (string, error) {
	p := s.st.Platform("api/oidc.go")
	var id int64
	var role, status, authSource string
	err := p.QueryRow(ctx, `SELECT id, role_code, status, auth_source FROM users
		WHERE username = ? AND deleted_at IS NULL`, username).Scan(&id, &role, &status, &authSource)

	mapped := mapRole(cfg, groups)

	switch {
	case err == sql.ErrNoRows:
		if !cfg.JITEnabled {
			return "", fmt.Errorf("账号 %q 在本系统里不存在，且未开启自动建号。"+
				"请让管理员先建好账号，或在 SSO 配置里打开自动建号", username)
		}
		res, err := p.Exec(ctx, `INSERT INTO users
			(username, display_name, email, auth_source, role_code, status, password_hash)
			VALUES (?, ?, ?, 'oidc', ?, 'active', '')`,
			username, displayName, email, mapped)
		if err != nil {
			return "", fmt.Errorf("自动建号失败：%v", err)
		}
		id, _ = res.LastInsertId()
		logx.J("oidc", "user_created", map[string]any{
			"username": username, "role": mapped, "groups": groups})
		// 新号要绑租户。⚠️ 不绑的话登录能成功但每个接口 403 ——
		// 表现为"SSO 登进来了什么都看不到"
		var tenantID int64
		if err := p.QueryRow(ctx, `SELECT id FROM tenants ORDER BY id LIMIT 1`).Scan(&tenantID); err != nil {
			return "", fmt.Errorf("找不到可归属的租户：%v", err)
		}
		if _, err := p.Exec(ctx,
			`INSERT IGNORE INTO user_tenants (user_id, tenant_id) VALUES (?, ?)`, id, tenantID); err != nil {
			return "", fmt.Errorf("绑定租户失败：%v", err)
		}
		role = mapped

	case err != nil:
		return "", fmt.Errorf("查询账号失败：%v", err)

	default:
		if status != "active" {
			// 被停用的账号不能靠 SSO 绕回来
			return "", fmt.Errorf("账号 %q 已被停用", username)
		}
		// 🔴 **本地账号不被 SSO 接管**。
		//
		// 接管的话，身份源里只要有一个同名用户，就能顶掉本地 admin ——
		// 那是一条从"能登 IdP"直达"本系统管理员"的路径。
		// 同名时按已有账号登录，但不改它的角色。
		if authSource != "oidc" {
			logx.J("oidc", "local_account_not_overridden", map[string]any{
				"username": username,
				"note":     "同名本地账号存在，按本地账号登录且不套用角色映射",
			})
			break
		}
		// ⚠️ 每次登录都重新套用角色映射。人在 IdP 里被移出某个组之后，
		// 只有这样才会真的降权 —— 只在建号时映射一次的话，
		// 权限会永远停在他第一次登录时的样子。
		if mapped != "" && mapped != role {
			if _, err := p.Exec(ctx, `UPDATE users SET role_code = ? WHERE id = ?`, mapped, id); err == nil {
				logx.J("oidc", "role_updated", map[string]any{
					"username": username, "from": role, "to": mapped, "groups": groups})
				role = mapped
			}
		}
		if displayName != "" || email != "" {
			_, _ = p.Exec(ctx, `UPDATE users SET display_name = ?, email = ? WHERE id = ?`,
				displayName, email, id)
		}
	}

	var tenantID int64
	if err := p.QueryRow(ctx,
		`SELECT tenant_id FROM user_tenants WHERE user_id = ? ORDER BY tenant_id LIMIT 1`, id).
		Scan(&tenantID); err != nil {
		return "", fmt.Errorf("账号没有归属租户，请联系管理员")
	}
	return middleware.Issue(s.cfg.JWTSecret, middleware.Claims{
		UserID: id, TenantID: tenantID, Username: username, Role: role,
	}, time.Duration(s.cfg.SessionHours)*time.Hour)
}

// mapRole 按群组映射角色。没命中时给默认角色。
//
// 🔴 默认角色绝不能是 admin。默认给管理员的话，
// 身份源里任何一个人登录一次就成了这套系统的管理员。
func mapRole(cfg *oidcConfig, groups []string) string {
	for _, g := range groups {
		if r, ok := cfg.RoleMapping[g]; ok && r != "" {
			return r
		}
	}
	if cfg.DefaultRole == "" {
		return "viewer"
	}
	return cfg.DefaultRole
}
