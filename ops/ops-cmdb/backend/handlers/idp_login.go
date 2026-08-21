package handlers

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"

	"ops-cmdb-backend/internal/license"
	"ops-cmdb-backend/internal/store"
	"ops-cmdb-backend/logx"
)

// OIDC 授权码登录链路。
//
// 全流程：
//	 浏览器 → GET /api/auth/sso/start  → 302 到 IdP
//	 用户在 IdP 输密码
//	 IdP → GET /api/auth/sso/callback?code=..&state=..
//	 后端拿 code 换 id_token → 验签 → 找/建用户 → 发会话 → 302 回前端
//
// ⚠️ 这条链路上任何一步失败都必须**跳回登录页并显示原因**，不能返回一个
// JSON 错误页。用户是从浏览器地址栏过来的，看到一坨 JSON 只会去截图问人，
// 而错误里往往已经写清了是 nonce 不符还是 aud 不符。

// ssoErr 失败时跳回登录页，把原因放在 query 里。
//
// ⚠️ 原因用 reason（我们自己的短码）而不是原始错误串：错误串里可能带
// IdP 返回的内部信息。detail 只进日志。
func ssoErr(c *gin.Context, reason string, err error, fields map[string]any) {
	if fields == nil {
		fields = map[string]any{}
	}
	fields["reason"] = reason
	if err != nil {
		fields["err"] = err.Error()
	}
	logx.J("idp", "login_failed", fields)
	c.Redirect(http.StatusFound, "/login?sso_error="+url.QueryEscape(reason))
}

// recordFailure 把失败原因写回配置行，接入页会显示它。
//
// 失败的人和能改配置的人往往不是同一个：用户看到"取不到用户名"，
// 管理员打开配置页看到一切正常，中间隔着一次"你截个图发我"。
// 把线索放到管理员看得见的地方，他自己就能闭环。
//
// ⚠️ 只记 claim 的**名字**，不记值——值里是邮箱和姓名。
func (h *IdPHandler) recordFailure(ctx context.Context, reason, user string, claims []string) {
	sort.Strings(claims)
	if _, err := h.DB.ExecContext(ctx, `UPDATE idp_configs
		SET last_error=?, last_error_at=NOW(), last_error_user=?, last_claims=?
		WHERE id=1`, reason, user, strings.Join(claims, ",")); err != nil {
		logx.J("idp", "record_failure_failed", map[string]any{"err": err.Error()})
	}
}

func randHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// Start GET /api/auth/sso/start?redirect=/hosts
//
//	@Summary		发起 SSO 登录
//	@Description	302 跳到 IdP 的授权页。失败时跳回 /login?sso_error=…
//	@Tags			auth
//	@Param			redirect	query	string	false	"登录后回到哪个页面"
//	@Success		302
//	@Router			/auth/sso/start [get]
func (h *IdPHandler) Start(c *gin.Context) {
	cfg, _, err := h.load(c.Request.Context())
	if err != nil || cfg == nil || !cfg.Enabled {
		ssoErr(c, "sso_disabled", err, nil)
		return
	}
	ep, err := discover(c.Request.Context(), cfg.Issuer)
	if err != nil {
		ssoErr(c, "idp_unreachable", err, map[string]any{"issuer": cfg.Issuer})
		return
	}

	state, nonce := randHex(16), randHex(16)
	// 只保留站内相对路径。开放跳转（?redirect=https://evil.com）会让这个接口
	// 变成一个带我们域名的钓鱼跳板
	redirect := c.Query("redirect")
	if !strings.HasPrefix(redirect, "/") || strings.HasPrefix(redirect, "//") {
		redirect = "/"
	}
	if _, err := h.DB.ExecContext(c.Request.Context(),
		`INSERT INTO idp_login_states (state, nonce, redirect, expires_at) VALUES (?,?,?,?)`,
		state, nonce, redirect, time.Now().Add(stateTTL)); err != nil {
		ssoErr(c, "state_save_failed", err, nil)
		return
	}
	// 顺手清过期的，不另起定时任务
	h.DB.Exec(`DELETE FROM idp_login_states WHERE expires_at < NOW()`)

	q := url.Values{}
	q.Set("response_type", "code")
	q.Set("client_id", cfg.ClientID)
	q.Set("redirect_uri", redirectURI(c))
	q.Set("scope", cfg.Scopes)
	q.Set("state", state)
	q.Set("nonce", nonce)
	sep := "?"
	if strings.Contains(ep.AuthURL, "?") {
		sep = "&"
	}
	logx.J("idp", "login_start", map[string]any{
		"issuer": cfg.Issuer, "redirect_uri": redirectURI(c), "ip": c.ClientIP(),
	})
	c.Redirect(http.StatusFound, ep.AuthURL+sep+q.Encode())
}

// Callback GET /api/auth/sso/callback?code=..&state=..
//
//	@Summary		SSO 回调
//	@Description	换 token、验 id_token、发会话，然后 302 回前端。
//	@Tags			auth
//	@Success		302
//	@Router			/auth/sso/callback [get]
func (h *IdPHandler) Callback(c *gin.Context) {
	ctx := c.Request.Context()

	// IdP 自己就报错了（用户点了拒绝、client_id 不对……）。
	// 这个分支很容易漏写，漏了的表现是"点登录转一圈回来还在登录页"，没有任何线索
	if e := c.Query("error"); e != "" {
		ssoErr(c, "idp_returned_error", fmt.Errorf("%s: %s", e, c.Query("error_description")), nil)
		return
	}
	code, state := c.Query("code"), c.Query("state")
	if code == "" || state == "" {
		ssoErr(c, "missing_code", nil, nil)
		return
	}

	// state 一次性：查出来立刻删。不删的话同一个 code+state 可以被重放
	var nonce, redirect string
	err := h.DB.QueryRowContext(ctx,
		`SELECT nonce, redirect FROM idp_login_states WHERE state=? AND expires_at > NOW()`,
		state).Scan(&nonce, &redirect)
	if err != nil {
		ssoErr(c, "state_invalid", err, nil)
		return
	}
	h.DB.ExecContext(ctx, `DELETE FROM idp_login_states WHERE state=?`, state)

	cfg, secret, err := h.load(ctx)
	if err != nil || cfg == nil || !cfg.Enabled {
		ssoErr(c, "sso_disabled", err, nil)
		return
	}
	ep, err := discover(ctx, cfg.Issuer)
	if err != nil {
		ssoErr(c, "idp_unreachable", err, nil)
		return
	}

	rawID, err := exchangeCode(ctx, ep.TokenURL, cfg.ClientID, secret, code, redirectURI(c))
	if err != nil {
		h.recordFailure(ctx, "code_exchange_failed", "", nil)
		ssoErr(c, "code_exchange_failed", err, map[string]any{"token_url": ep.TokenURL})
		return
	}

	claims, err := verifyIDToken(ctx, rawID, ep, cfg.ClientID, nonce)
	if err != nil {
		h.recordFailure(ctx, "id_token_invalid", "", nil)
		ssoErr(c, "id_token_invalid", err, nil)
		return
	}

	subject, _ := claims["sub"].(string)
	if subject == "" {
		ssoErr(c, "no_subject", nil, nil)
		return
	}
	username := claimStr(claims, cfg.UsernameClaim)
	if username == "" {
		// 配的 claim 在这个 IdP 上不存在。把**实际拿到的 claim 名字列出来**，
		// 否则客户只能猜自己该填哪个——这是接 SSO 最常卡住的一步
		keys := make([]string, 0, len(claims))
		for k := range claims {
			keys = append(keys, k)
		}
		// 标签优先用 email / name：sub 是一串不透明的编码（Dex 给的是 base64），
		// 管理员看到它既认不出是谁，也没法联系本人重试
		who := claimStr(claims, "email")
		if who == "" {
			who = claimStr(claims, "name")
		}
		h.recordFailure(ctx, "username_claim_missing", who, keys)
		ssoErr(c, "username_claim_missing", fmt.Errorf("配的是 %q，id_token 里有 %v",
			cfg.UsernameClaim, keys), nil)
		return
	}
	display := claimStr(claims, cfg.NameClaim)
	if display == "" {
		display = username
	}

	userID, roleCode, err := h.resolveUser(ctx, cfg, subject, username, display)
	if err != nil {
		h.recordFailure(ctx, "user_resolve_failed", username, nil)
		ssoErr(c, "user_resolve_failed", err, map[string]any{"sub": subject, "user": username})
		return
	}

	perms, unrestricted := permsOfLocalRole(h.DB, roleCode)
	sessionRole := roleCode
	if unrestricted {
		sessionRole = "admin"
	}
	var permArg map[string]bool
	if !unrestricted {
		permArg = perms
	}
	// auth_source 记 "oidc"：出问题时要能一眼分清这个会话是密码进来的还是 SSO 进来的
	token, _, err := h.Auth.issueSession(userID, username, sessionRole, "oidc", permArg, "")
	if err != nil {
		ssoErr(c, "session_failed", err, nil)
		return
	}
	h.DB.ExecContext(ctx, `UPDATE users SET last_login_at=NOW() WHERE id=?`, userID)
	// 成功了就清掉上次的失败。不清的话，一条早就修好的旧错误会一直挂在配置页上，
	// 下次真出问题时没人会再相信它
	h.DB.ExecContext(ctx, `UPDATE idp_configs SET last_error='', last_error_at=NULL,
		last_error_user='', last_claims='' WHERE id=1 AND last_error<>''`)
	WriteAuditAs(h.DB, h.Auth.ctxWithUser(c, username), "auth.login.success", "auth_source=oidc")
	logx.J("idp", "login_success", map[string]any{
		"user": username, "sub": subject, "role": sessionRole, "ip": c.ClientIP(),
	})

	// token 走 fragment（# 后面）而不是 query：fragment 不会进 Referer、
	// 不会被反向代理和 CDN 记进访问日志。query 里的 token 会留在一路的日志里
	if redirect == "" {
		redirect = "/"
	}
	c.Redirect(http.StatusFound, "/login#sso_token="+url.QueryEscape(token)+
		"&redirect="+url.QueryEscape(redirect))
}

// resolveUser 按 subject 找账号；找不到时看 JIT 开没开。
func (h *IdPHandler) resolveUser(ctx context.Context, cfg *idpConfig, subject, username, display string) (int, string, error) {
	var id int
	var roleCode string
	err := h.DB.QueryRowContext(ctx,
		`SELECT id, IFNULL(role_code,'') FROM users WHERE idp_subject=?`, subject).Scan(&id, &roleCode)
	if err == nil {
		// 名字可能在 IdP 那边改过，跟着更新；但**不动角色**——
		// 角色是我们这边授予的，不能被 IdP 的一次登录覆盖掉
		h.DB.ExecContext(ctx, `UPDATE users SET display_name=?, username=? WHERE id=?`,
			display, username, id)
		return id, roleCode, nil
	}

	// 同名的本地账号：认领它，而不是再建一个。
	//
	// ⚠️ 只认领还没绑过的（idp_subject IS NULL）。已经绑给别人的账号不能抢——
	// 否则 IdP 里注册一个同名用户就能接管本地管理员
	var localID int
	var localSrc, localRole string
	if e := h.DB.QueryRowContext(ctx,
		`SELECT id, IFNULL(auth_source,'local'), IFNULL(role_code,'') FROM users
		 WHERE username=? AND idp_subject IS NULL`, username).Scan(&localID, &localSrc, &localRole); e == nil {
		if _, e2 := h.DB.ExecContext(ctx,
			`UPDATE users SET idp_subject=?, display_name=? WHERE id=?`, subject, display, localID); e2 != nil {
			return 0, "", e2
		}
		logx.J("idp", "linked_local_user", map[string]any{
			"user": username, "sub": subject,
			"note": "同名本地账号已绑定到 SSO，密码登录仍然可用",
		})
		return localID, localRole, nil
	}

	if !cfg.JITEnabled {
		// ⚠️ 这句错误要能直接指导操作。"登录失败"会让客户以为是 SSO 没配对，
		// 实际上 SSO 是通的，只是这个人没账号
		return 0, "", fmt.Errorf("用户 %s 在本系统没有账号，且未开启自动创建（JIT）", username)
	}

	// JIT 之前检查席位。超了**不拦**，与授权规范一致（容量只告警不阻断），
	// 但必须留下日志——否则客户是在续费时才第一次知道自己超了多少
	if h.License != nil {
		var used int64
		h.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM users`).Scan(&used)
		if limit := h.License.Limit(license.CapSeats); limit > 0 && used >= limit {
			logx.J("idp", "seats_exceeded", map[string]any{
				"used": used, "limit": limit, "new_user": username,
				"note": "JIT 仍会创建账号（容量只告警不阻断），但已超出授权席位",
			})
		}
	}

	res, err := h.DB.ExecContext(ctx,
		`INSERT INTO users (username, password_hash, display_name, is_admin, role_code, auth_source, idp_subject)
		 VALUES (?, '', ?, 0, ?, 'oidc', ?)`,
		username, display, cfg.JITRoleCode, subject)
	if err != nil {
		return 0, "", err
	}
	newID, _ := res.LastInsertId()

	// ⚠️ 建号必须同时落租户归属，与用户管理页那条一字不差。
	// 漏了的话这个账号**能登录、但每个业务接口都 403**：
	// 登录时 resolveTenant 查不到归属会回落到默认租户并照常发会话，
	// 而请求时的租户中间件按 user_tenants 校验，两边判据不一致。
	// SSO 场景更难查——报障的人只会说"用飞书登进去是空白的"，
	// 而登录本身明明成功了。
	if _, e := h.DB.ExecContext(ctx, store.SeedUserTenantQuery, newID); e != nil {
		// 不回滚建号：账号已经建好了，回滚反而更难解释。但后果要明说
		logx.J("idp", "jit_tenant_failed", map[string]any{
			"user": username, "id": newID, "err": e.Error(),
			"note": "账号已建但租户归属没落上，该账号会在每个业务接口 403",
		})
	}

	logx.J("idp", "jit_created", map[string]any{
		"user": username, "sub": subject, "role": cfg.JITRoleCode,
	})
	return int(newID), cfg.JITRoleCode, nil
}

// claimStr 取字符串 claim。数字型的 sub 也接住（有 IdP 会给 int）。
func claimStr(m jwt.MapClaims, key string) string {
	switch v := m[key].(type) {
	case string:
		return v
	case float64:
		return fmt.Sprintf("%.0f", v)
	}
	return ""
}

// exchangeCode 拿授权码换 id_token。
func exchangeCode(ctx context.Context, tokenURL, clientID, secret, code, redirect string) (string, error) {
	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("code", code)
	form.Set("redirect_uri", redirect)
	form.Set("client_id", clientID)
	form.Set("client_secret", secret)

	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, tokenURL,
		strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := (&http.Client{Timeout: 10 * time.Second}).Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 256*1024))
	if resp.StatusCode != http.StatusOK {
		// 带上响应体：IdP 的 error_description 通常已经说清了是 secret 错了
		// 还是 redirect_uri 对不上，丢掉它等于把唯一的线索扔了
		return "", fmt.Errorf("token 端点返回 %d: %s", resp.StatusCode, string(body))
	}
	var out struct {
		IDToken string `json:"id_token"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return "", err
	}
	if out.IDToken == "" {
		// scope 里漏了 openid 就是这个结果
		return "", fmt.Errorf("响应里没有 id_token（检查 scopes 是否包含 openid）")
	}
	return out.IDToken, nil
}

// jwksCache JWKS 缓存。IdP 的公钥很少变，每次登录都拉一次既慢又给对方添压力。
var jwksCache = struct {
	sync.Mutex
	m map[string]jwksEntry
}{m: map[string]jwksEntry{}}

type jwksEntry struct {
	keys map[string]*rsa.PublicKey
	at   time.Time
}

const jwksTTL = 10 * time.Minute

// verifyIDToken 验签 + 校验 iss / aud / exp / nonce。
//
// ⚠️ 这是整条链路的**唯一**安全边界。id_token 是从浏览器转发过来的，
// 不验签的话任何人都能自己造一个 {"sub":"admin"} 直接登进来。
// 用 jwt.Parse 而不是 ParseUnverified，且必须锁定签名算法族——
// 否则 alg=none 或 alg=HS256（拿公钥当 HMAC 密钥）都能绕过。
func verifyIDToken(ctx context.Context, raw string, ep *oidcEndpoints, clientID, nonce string) (jwt.MapClaims, error) {
	keys, err := fetchJWKS(ctx, ep.JWKSURL)
	if err != nil {
		return nil, fmt.Errorf("取 JWKS 失败: %w", err)
	}
	claims := jwt.MapClaims{}
	_, err = jwt.ParseWithClaims(raw, claims, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodRSA); !ok {
			return nil, fmt.Errorf("签名算法 %v 不接受，只认 RS256/384/512", t.Header["alg"])
		}
		kid, _ := t.Header["kid"].(string)
		if k, ok := keys[kid]; ok {
			return k, nil
		}
		// 没有 kid 且只有一把公钥时用那一把（小型 IdP 常见）
		if kid == "" && len(keys) == 1 {
			for _, k := range keys {
				return k, nil
			}
		}
		return nil, fmt.Errorf("JWKS 里没有 kid=%q", kid)
	}, jwt.WithIssuer(ep.Issuer), jwt.WithAudience(clientID), jwt.WithExpirationRequired())
	if err != nil {
		return nil, err
	}
	// nonce 必须和发起时存的一致：防的是把别处拿到的 id_token 拿来重放
	if got, _ := claims["nonce"].(string); got != nonce {
		return nil, fmt.Errorf("nonce 不符")
	}
	return claims, nil
}

func fetchJWKS(ctx context.Context, jwksURL string) (map[string]*rsa.PublicKey, error) {
	if jwksURL == "" {
		return nil, fmt.Errorf("discovery 里没有 jwks_uri")
	}
	jwksCache.Lock()
	if e, ok := jwksCache.m[jwksURL]; ok && time.Since(e.at) < jwksTTL {
		jwksCache.Unlock()
		return e.keys, nil
	}
	jwksCache.Unlock()

	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, jwksURL, nil)
	resp, err := (&http.Client{Timeout: 8 * time.Second}).Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("JWKS 返回 %d", resp.StatusCode)
	}
	var doc struct {
		Keys []struct {
			Kid string `json:"kid"`
			Kty string `json:"kty"`
			N   string `json:"n"`
			E   string `json:"e"`
		} `json:"keys"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 512*1024)).Decode(&doc); err != nil {
		return nil, err
	}
	keys := map[string]*rsa.PublicKey{}
	for _, k := range doc.Keys {
		if k.Kty != "RSA" {
			continue
		}
		nb, err := base64.RawURLEncoding.DecodeString(k.N)
		if err != nil {
			continue
		}
		eb, err := base64.RawURLEncoding.DecodeString(k.E)
		if err != nil {
			continue
		}
		keys[k.Kid] = &rsa.PublicKey{
			N: new(big.Int).SetBytes(nb),
			E: int(new(big.Int).SetBytes(eb).Int64()),
		}
	}
	if len(keys) == 0 {
		return nil, fmt.Errorf("JWKS 里没有可用的 RSA 公钥")
	}
	jwksCache.Lock()
	jwksCache.m[jwksURL] = jwksEntry{keys: keys, at: time.Now()}
	jwksCache.Unlock()
	return keys, nil
}
