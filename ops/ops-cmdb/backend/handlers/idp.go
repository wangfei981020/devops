package handlers

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"ops-cmdb-backend/crypto"
	"ops-cmdb-backend/internal/httpx"
	"ops-cmdb-backend/internal/license"
	"ops-cmdb-backend/logx"
)

// OIDC 单点登录（CMDB 作为 RP）。
//
// ⚠️ 方向别搞反：CMDB 是**依赖方**，它去连别人的 IdP（飞书 / Okta / OneGate），
// 而不是自己当 IdP 让别人来连。
//
// ⚠️ 无论 SSO 怎么配，**本地账号永远保留**。IdP 挂了、配错了、证书过期了，
// 都得有一条进得来的路——否则唯一的修复手段是直接改数据库。
// 这条在 Harbor 接 SSO 时踩过：`always_sign_in` 那个开关实测无效，
// 靠的就是 admin 的本地密码兜底。

type IdPHandler struct {
	DB     *sql.DB
	Cipher *crypto.Cipher
	Auth   *AuthHandler
	// License 用于 JIT 建号前查席位容量。可以为 nil（没接授权时不查）
	License *license.Manager
}

func NewIdPHandler(db *sql.DB, ci *crypto.Cipher, auth *AuthHandler, lic *license.Manager) *IdPHandler {
	return &IdPHandler{DB: db, Cipher: ci, Auth: auth, License: lic}
}

// RegisterPublic 不需要登录态的：登录页要问"配没配 SSO"，以及整个登录跳转链路。
func (h *IdPHandler) RegisterPublic(r *gin.RouterGroup) {
	r.GET("/auth/sso", h.Public)      // 登录页用：有没有开、按钮上写什么
	r.GET("/auth/sso/start", h.Start) // 302 到 IdP
	r.GET("/auth/sso/callback", h.Callback)
}

// RegisterAuthed 配置类接口，要登录 + 权限。
func (h *IdPHandler) RegisterAuthed(r *gin.RouterGroup) {
	r.GET("/idp-config", h.Get)
	r.PUT("/idp-config", h.Save)
	r.POST("/idp-config/discover", h.Discover)
}

// stateTTL state 的有效期。够走完"跳到 IdP → 输密码 → 跳回来"，又不至于留一堆可重放的凭证。
const stateTTL = 10 * time.Minute

type idpConfig struct {
	Enabled       bool   `json:"enabled"`
	DisplayName   string `json:"display_name"`
	Issuer        string `json:"issuer"`
	ClientID      string `json:"client_id"`
	Scopes        string `json:"scopes"`
	UsernameClaim string `json:"username_claim"`
	NameClaim     string `json:"name_claim"`
	JITEnabled    bool   `json:"jit_enabled"`
	JITRoleCode   string `json:"jit_role_code"`
	// AllowPrivate 允许 issuer 指向内网 / 用 http。自建身份源的客户需要它
	AllowPrivate bool `json:"allow_private"`
	// HasSecret 只暴露"配没配"。client_secret 任何接口都不回传
	HasSecret bool   `json:"has_secret"`
	UpdatedAt string `json:"updated_at,omitempty"`
	// RedirectURI 回调地址，给客户去 IdP 那边登记用。
	// 不显示的话，客户得自己拼，拼错一个字符就是一句 redirect_uri_mismatch
	RedirectURI string `json:"redirect_uri"`

	// 最近一次登录失败。给管理员看的线索——出问题的人和能改配置的人不是同一个
	LastError     string `json:"last_error,omitempty"`
	LastErrorAt   string `json:"last_error_at,omitempty"`
	LastErrorUser string `json:"last_error_user,omitempty"`
	// LastClaims 身份源实际返回的 claim 名字。配「用户名取自」时照着这个填即可
	LastClaims []string `json:"last_claims,omitempty"`
}

func (h *IdPHandler) load(ctx context.Context) (*idpConfig, string, error) {
	var c idpConfig
	var enabled, jit, allowPriv int
	var secret sql.NullString
	var updated, lastAt sql.NullTime
	var lastClaims string
	err := h.DB.QueryRowContext(ctx, `SELECT enabled, display_name, issuer, client_id,
		COALESCE(client_secret_enc,''), scopes, username_claim, name_claim, jit_enabled,
		jit_role_code, allow_private, last_error, last_error_at, last_error_user, last_claims,
		updated_at FROM idp_configs WHERE id=1`).
		Scan(&enabled, &c.DisplayName, &c.Issuer, &c.ClientID, &secret, &c.Scopes,
			&c.UsernameClaim, &c.NameClaim, &jit, &c.JITRoleCode, &allowPriv,
			&c.LastError, &lastAt, &c.LastErrorUser, &lastClaims, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, "", nil
	}
	if err != nil {
		return nil, "", err
	}
	c.Enabled = enabled == 1
	c.JITEnabled = jit == 1
	c.AllowPrivate = allowPriv == 1
	c.HasSecret = secret.Valid && secret.String != ""
	if updated.Valid {
		c.UpdatedAt = updated.Time.Format(time.RFC3339)
	}
	if lastAt.Valid {
		c.LastErrorAt = lastAt.Time.Format(time.RFC3339)
	}
	if lastClaims != "" {
		c.LastClaims = strings.Split(lastClaims, ",")
	}
	plain := ""
	if c.HasSecret {
		if p, e := h.Cipher.Decrypt(secret.String); e == nil {
			plain = p
		}
	}
	return &c, plain, nil
}

// Public GET /api/auth/sso —— 登录页用，**不需要登录态**。
//
//	@Summary		SSO 是否可用
//	@Description	只返回"开没开"和按钮文案。没开时返回 enabled=false，不报错。
//	@Tags			auth
//	@Produce		json
//	@Success		200	{object}	map[string]any
//	@Router			/auth/sso [get]
func (h *IdPHandler) Public(c *gin.Context) {
	cfg, _, err := h.load(c.Request.Context())
	// ⚠️ 查不出来时按"没开"返回，而不是报错：登录页因为 SSO 配置查询失败
	// 而整页打不开的话，本地账号这条逃生通道也跟着没了
	if err != nil || cfg == nil || !cfg.Enabled || cfg.Issuer == "" || cfg.ClientID == "" {
		if err != nil {
			logx.J("idp", "public_load_failed", map[string]any{"err": err.Error(),
				"note": "登录页按未配置 SSO 处理，本地登录不受影响"})
		}
		c.JSON(http.StatusOK, gin.H{"enabled": false})
		return
	}
	c.JSON(http.StatusOK, gin.H{"enabled": true, "display_name": cfg.DisplayName})
}

// Get GET /api/idp-config
//
//	@Summary		SSO 配置
//	@Description	**不返回 client_secret**，只给 has_secret。
//	@Tags			admin
//	@Produce		json
//	@Success		200	{object}	handlers.idpConfig
//	@Router			/idp-config [get]
func (h *IdPHandler) Get(c *gin.Context) {
	cfg, _, err := h.load(c.Request.Context())
	if err != nil {
		httpx.Fail(c, httpx.CodeInternal, err, nil)
		return
	}
	if cfg == nil {
		cfg = &idpConfig{
			Scopes: "openid profile email", UsernameClaim: "preferred_username",
			NameClaim: "name", JITRoleCode: "cmdb_viewer",
		}
	}
	cfg.RedirectURI = redirectURI(c)
	c.JSON(http.StatusOK, cfg)
}

type idpIn struct {
	Enabled       bool   `json:"enabled"`
	DisplayName   string `json:"display_name"`
	Issuer        string `json:"issuer"`
	ClientID      string `json:"client_id"`
	ClientSecret  string `json:"client_secret"` // 空 = 保持原值
	Scopes        string `json:"scopes"`
	UsernameClaim string `json:"username_claim"`
	NameClaim     string `json:"name_claim"`
	JITEnabled    bool   `json:"jit_enabled"`
	JITRoleCode   string `json:"jit_role_code"`
	AllowPrivate  bool   `json:"allow_private"`
}

// Save PUT /api/idp-config
//
//	@Summary		保存 SSO 配置
//	@Description	client_secret 留空表示保持不变。开启前会校验 issuer 可达且不是内网地址。
//	@Tags			admin
//	@Accept			json
//	@Produce		json
//	@Success		200	{object}	map[string]any
//	@Failure		400	{object}	httpx.APIError
//	@Router			/idp-config [put]
func (h *IdPHandler) Save(c *gin.Context) {
	var in idpIn
	if err := c.ShouldBindJSON(&in); err != nil {
		httpx.Fail(c, httpx.CodeBadRequest, err, nil)
		return
	}
	if in.Enabled {
		// 开启时才严格校验：允许先存一份不完整的草稿，但**不允许开着它**
		if in.Issuer == "" || in.ClientID == "" {
			httpx.FailKey(c, httpx.CodeBadRequest, "error.idpMissingBasics", nil, nil)
			return
		}
		if err := checkIssuerSafe(in.Issuer, in.AllowPrivate); err != nil {
			httpx.FailKey(c, httpx.CodeBadRequest, "error.idpPrivateHost", err,
				map[string]any{"host": in.Issuer})
			return
		}
	}
	if in.UsernameClaim == "" {
		in.UsernameClaim = "preferred_username"
	}
	if in.NameClaim == "" {
		in.NameClaim = "name"
	}
	if in.Scopes == "" {
		in.Scopes = "openid profile email"
	}
	if in.JITRoleCode == "" {
		// ⚠️ 默认给只读而不是不受限：JIT 的默认值错一次，
		// 全公司登进来的人都是管理员
		in.JITRoleCode = "cmdb_viewer"
	}

	enabled, jit, allowPriv := 0, 0, 0
	if in.Enabled {
		enabled = 1
	}
	if in.JITEnabled {
		jit = 1
	}
	if in.AllowPrivate {
		allowPriv = 1
	}

	if in.ClientSecret == "" {
		// 留空 = 不动密钥。这条 SQL 刻意不含 client_secret_enc 列
		_, err := h.DB.ExecContext(c.Request.Context(), `INSERT INTO idp_configs
			(id, enabled, display_name, issuer, client_id, scopes, username_claim, name_claim,
			 jit_enabled, jit_role_code, allow_private, updated_by)
			VALUES (1,?,?,?,?,?,?,?,?,?,?,?)
			ON DUPLICATE KEY UPDATE enabled=VALUES(enabled), display_name=VALUES(display_name),
			 issuer=VALUES(issuer), client_id=VALUES(client_id), scopes=VALUES(scopes),
			 username_claim=VALUES(username_claim), name_claim=VALUES(name_claim),
			 jit_enabled=VALUES(jit_enabled), jit_role_code=VALUES(jit_role_code),
			 allow_private=VALUES(allow_private), updated_by=VALUES(updated_by)`,
			enabled, in.DisplayName, in.Issuer, in.ClientID, in.Scopes, in.UsernameClaim,
			in.NameClaim, jit, in.JITRoleCode, allowPriv, UsernameFromCtx(c))
		if err != nil {
			httpx.Fail(c, httpx.CodeInternal, err, nil)
			return
		}
	} else {
		enc, err := h.Cipher.Encrypt(in.ClientSecret)
		if err != nil {
			httpx.FailKey(c, httpx.CodeInternal, "error.credEncryptFailed", err, nil)
			return
		}
		_, err = h.DB.ExecContext(c.Request.Context(), `INSERT INTO idp_configs
			(id, enabled, display_name, issuer, client_id, client_secret_enc, scopes,
			 username_claim, name_claim, jit_enabled, jit_role_code, allow_private, updated_by)
			VALUES (1,?,?,?,?,?,?,?,?,?,?,?,?)
			ON DUPLICATE KEY UPDATE enabled=VALUES(enabled), display_name=VALUES(display_name),
			 issuer=VALUES(issuer), client_id=VALUES(client_id),
			 client_secret_enc=VALUES(client_secret_enc), scopes=VALUES(scopes),
			 username_claim=VALUES(username_claim), name_claim=VALUES(name_claim),
			 jit_enabled=VALUES(jit_enabled), jit_role_code=VALUES(jit_role_code),
			 allow_private=VALUES(allow_private), updated_by=VALUES(updated_by)`,
			enabled, in.DisplayName, in.Issuer, in.ClientID, enc, in.Scopes, in.UsernameClaim,
			in.NameClaim, jit, in.JITRoleCode, allowPriv, UsernameFromCtx(c))
		if err != nil {
			httpx.Fail(c, httpx.CodeInternal, err, nil)
			return
		}
	}
	SetAuditTarget(c, in.Issuer)
	logx.J("idp", "config_saved", map[string]any{
		"user": UsernameFromCtx(c), "issuer": in.Issuer,
		"enabled": in.Enabled, "jit": in.JITEnabled,
		// 放开内网/http 是个安全相关的决定，必须留痕：
		// 事后审计时要能看出这条是谁什么时候打开的
		"allow_private": in.AllowPrivate,
	})
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// oidcEndpoints discovery 文档里我们用得到的部分。
type oidcEndpoints struct {
	Issuer   string `json:"issuer"`
	AuthURL  string `json:"authorization_endpoint"`
	TokenURL string `json:"token_endpoint"`
	JWKSURL  string `json:"jwks_uri"`
}

// Discover POST /api/idp-config/discover  {"issuer": "..."}
//
//	@Summary		探测 IdP 端点
//	@Description	拉 {issuer}/.well-known/openid-configuration。拒绝内网地址。
//	@Tags			admin
//	@Accept			json
//	@Produce		json
//	@Success		200	{object}	handlers.oidcEndpoints
//	@Failure		400	{object}	httpx.APIError
//	@Router			/idp-config/discover [post]
func (h *IdPHandler) Discover(c *gin.Context) {
	var in struct {
		Issuer       string `json:"issuer"`
		AllowPrivate bool   `json:"allow_private"`
	}
	if err := c.ShouldBindJSON(&in); err != nil || in.Issuer == "" {
		httpx.Fail(c, httpx.CodeBadRequest, err, nil)
		return
	}
	if err := checkIssuerSafe(in.Issuer, in.AllowPrivate); err != nil {
		httpx.FailKey(c, httpx.CodeBadRequest, "error.idpPrivateHost", err,
			map[string]any{"host": in.Issuer})
		return
	}
	ep, err := discover(c.Request.Context(), in.Issuer)
	if err != nil {
		httpx.FailKey(c, httpx.CodeUpstreamError, "error.idpUnreachable", err, nil)
		return
	}
	c.JSON(http.StatusOK, ep)
}

// checkIssuerSafe 默认拒绝把 discovery 请求拉向内网；allowPrivate 是管理员的显式放行。
//
// ⚠️ 不拦的话，这个接口就成了**内网探测入口**：填 http://10.0.0.5:8080，
// 后端就替攻击者去请求内网服务，并把结果（能不能连、返回什么）交出来。
// 这是 SSRF 的经典形态，而"配置 IdP"这个功能天然需要发外部请求，最容易被忽略。
//
// ⚠️ 但它不能是死规则：自建 Keycloak / AD FS / Authelia 本来就跑在私网上，
// 不少还只有 http（TLS 在入口层终止）。一律拒绝会把一整类正常客户挡在门外，
// 而他们看到的会是一句"你的地址不安全"——对他们来说毫无道理。
// 所以做成"默认关 + 显式开"：手滑填错不会真发出去，有意为之则放行并留痕。
func checkIssuerSafe(issuer string, allowPrivate bool) error {
	u, err := url.Parse(issuer)
	if err != nil {
		return fmt.Errorf("地址解析失败: %w", err)
	}
	if u.Scheme != "https" && !allowPrivate {
		// OIDC 的 id_token 依赖传输层安全，http 下中间人可以直接改
		return fmt.Errorf("必须是 https（自建身份源可勾选「内网身份源」放开）")
	}
	if u.Scheme != "https" && u.Scheme != "http" {
		return fmt.Errorf("协议只能是 https 或 http，收到 %q", u.Scheme)
	}
	host := u.Hostname()
	if host == "" {
		return fmt.Errorf("缺少主机名")
	}
	if allowPrivate {
		return nil
	}
	ips, err := net.LookupIP(host)
	if err != nil {
		return fmt.Errorf("域名解析失败: %w", err)
	}
	for _, ip := range ips {
		if ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsUnspecified() {
			return fmt.Errorf("解析到内网地址 %s（自建身份源可勾选「内网身份源」放开）", ip)
		}
	}
	return nil
}

func discover(ctx context.Context, issuer string) (*oidcEndpoints, error) {
	u := strings.TrimRight(issuer, "/") + "/.well-known/openid-configuration"
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	cl := &http.Client{Timeout: 8 * time.Second}
	resp, err := cl.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("discovery 返回 %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 256*1024))
	if err != nil {
		return nil, err
	}
	var ep oidcEndpoints
	if err := json.Unmarshal(body, &ep); err != nil {
		return nil, err
	}
	if ep.AuthURL == "" || ep.TokenURL == "" {
		return nil, fmt.Errorf("discovery 里缺 authorization_endpoint 或 token_endpoint")
	}
	return &ep, nil
}

// redirectURI 回调地址。
//
// 从请求头推导而不是写死在配置里：同一个镜像会跑在不同域名下
// （本地 localhost:30833、生产 cmdb.example.com），写死就得每个环境改一次配置，
// 而改漏的那次表现是 redirect_uri_mismatch —— 一句看不出哪里错的报错。
//
// # ⚠️ scheme 的推导要多留一条退路
//
//	首选 X-Forwarded-Proto，但**不能只靠它**：TLS 终止在入口网关、
//	而网关没配这个头时，这里会推出 `http://`，
//	而页面本身是 https —— 客户照着登记，得到的正是
//	`redirect_uri_mismatch`（OPSCMDB-031 P0-22 实测就是这样）。
//
//	所以再看 Origin / Referer：那是**浏览器**填的，
//	它知道真实协议，而且在这个页面的请求里几乎总是有。
//	（本项目记过同一类根因：反代 Host 头必须用 $http_host，
//	当时那条笔记的原话就是"OIDC redirect_uri 首当其冲"。）
//
//	即便如此仍可能推错，所以前端会拿 location.protocol 再比一次，
//	对不上就直接把正确值显示出来 —— 两道防线都不昂贵，而配错的代价很高。
func redirectURI(c *gin.Context) string {
	host := c.GetHeader("X-Forwarded-Host")
	if host == "" {
		host = c.Request.Host
	}
	return schemeOf(c) + "://" + host + "/api/auth/sso/callback"
}

// schemeOf 推断外部访问用的协议。
func schemeOf(c *gin.Context) string {
	// ① 反代明确告知的最可信
	if p := c.GetHeader("X-Forwarded-Proto"); p != "" {
		// 可能是 "https, http"（多层代理各追加一个），取第一个 —— 那是最外层
		if i := strings.IndexByte(p, ','); i > 0 {
			p = p[:i]
		}
		return strings.TrimSpace(p)
	}
	// ② 浏览器填的 Origin / Referer 知道真实协议
	for _, h := range []string{"Origin", "Referer"} {
		if v := c.GetHeader(h); v != "" {
			if i := strings.Index(v, "://"); i > 0 {
				return v[:i]
			}
		}
	}
	// ③ 直连时看连接本身
	if c.Request.TLS != nil {
		return "https"
	}
	// ④ 其余常见的代理头。
	//	🔴 生产实测：前三条一条都没命中，返回的仍是 http://（OPSCMDB-031 P0-22）。
	//	说明入口那一层既不传 X-Forwarded-Proto，同源 GET 也不带 Origin/Referer。
	//	这几个是别的网关会用的等价头，成本极低而覆盖面明显更宽。
	if v := c.GetHeader("X-Forwarded-Scheme"); v != "" {
		return strings.TrimSpace(v)
	}
	// Forwarded: proto=https;host=...（RFC 7239 的标准写法）
	if v := c.GetHeader("Forwarded"); v != "" {
		for _, part := range strings.Split(v, ";") {
			part = strings.TrimSpace(part)
			if after, ok := strings.CutPrefix(strings.ToLower(part), "proto="); ok {
				return strings.Trim(strings.TrimSpace(after), `"`)
			}
		}
	}
	// 某些 LB 用端口表达：X-Forwarded-Port: 443
	if p := c.GetHeader("X-Forwarded-Port"); p == "443" {
		return "https"
	}
	// ⑤ 仍然推不出来。
	//	⚠️ 这里返回 http 是**如实**的（我们确实没有任何证据表明是 https），
	//	不要为了"生产多半是 https"就默认成 https —— 那会让本地 http 部署
	//	拿到一个错的值，而且同样不会报错。
	//	前端会用 location.protocol 纠正并提示，那才是有真实依据的一侧。
	logx.J("idp", "scheme_undetermined", map[string]any{
		"host": c.Request.Host,
		"warn": "推不出外部协议（X-Forwarded-Proto / Origin / Referer / Forwarded 都没有），" +
			"按 http 返回。若入口是 https，请让网关传 X-Forwarded-Proto。",
	})
	return "http"
}
