package api

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"ops-sso-backend/internal/apierr"
	"ops-sso-backend/internal/domain/access"
	"ops-sso-backend/internal/domain/oidcp"
	"ops-sso-backend/internal/store"
)

// ══════════════════════════════════════════════════════════════════
// OpenID Provider：下游应用（CMDB、告警平台…）用标准协议接进来
// ══════════════════════════════════════════════════════════════════

// oidcDiscovery 发现文档。客户端库拿到它就知道去哪调什么。
func oidcDiscovery(c *gin.Context, d Deps) (any, error) {
	return oidcp.Discovery(d.Issuer), nil
}

// oidcJWKS 公钥集。下游用它验 id_token 的签名。
//
// 发布**所有未退役**的公钥，不只是当前签名用的那把：
// 轮换期间旧 token 还在有效期内，下游必须还能验它们。
func oidcJWKS(c *gin.Context, d Deps) (any, error) {
	keys, err := d.OIDCP.Keys(withPlatformTenant(c))
	if err != nil {
		return nil, err
	}
	out := make([]map[string]any, 0, len(keys))
	for _, k := range keys {
		out = append(out, k.PublicJWK())
	}
	return gin.H{"keys": out}, nil
}

// oidcAuthorize 授权端点。
//
// # 与普通 IdP 的关键差别就在这里
//
// 签发授权码之前**先跑一次访问判定**：没被授权用这个应用的人，
// 走到这一步就被拒。普通 IdP 只回答"你是谁"，下游拿到 token 后
// 还得自己判权限 —— 那正是"每个系统各写一套权限、各写错一遍"的由来。
func oidcAuthorize(c *gin.Context, d Deps) (any, error) {
	q := c.Request.URL.Query()
	clientID := q.Get("client_id")
	redirectURI := q.Get("redirect_uri")
	state := q.Get("state")

	ctx := withDefaultTenant(c)
	client, err := d.OIDCP.GetClient(ctx, clientID)
	if err != nil || !client.Enabled {
		// ★ client_id 或 redirect_uri 不对时**绝不跳转** ——
		// 直接把错误显示在我们自己的页面上。
		// 往一个没验证过的地址跳，等于亲手把 error 参数（有时含用户信息）送给攻击者。
		return nil, apierr.BadRequest(apierr.CodeOIDCBadClient, nil)
	}
	if err := client.ValidateRedirectURI(redirectURI); err != nil {
		return nil, apierr.BadRequest(apierr.CodeOIDCBadRedirect, nil)
	}

	// 从这里开始出错才可以按协议跳回去带 error
	if q.Get("response_type") != "code" {
		return nil, redirectErr(c, redirectURI, state, "unsupported_response_type",
			"只支持授权码流")
	}
	challenge := q.Get("code_challenge")
	if client.RequirePKCE && challenge == "" {
		return nil, redirectErr(c, redirectURI, state, "invalid_request",
			"该客户端必须使用 PKCE")
	}
	if challenge != "" && q.Get("code_challenge_method") != "S256" {
		return nil, redirectErr(c, redirectURI, state, "invalid_request",
			"code_challenge_method 只支持 S256")
	}

	// 未登录 → 先去登录页，登完回来
	id := identity(c)
	if id.UserID == 0 {
		back := "/oidc/authorize?" + q.Encode()
		c.Redirect(http.StatusFound, "/login?next="+url.QueryEscape(back))
		c.Abort()
		return nil, nil
	}

	// ★ 必须改密的账号，在改密之前不能换出令牌。
	//
	// passwordChangeGuard 只挂在业务接口那一组上，**这条路绕过了它** ——
	// 实测过：管理员刚重置完口令的账号，直接走 OIDC 就能拿到 code，
	// 下游照常放人进去，而它一次都没改过密。
	//
	// 这条路径的危险之处在于：bootstrap 口令是**重置的那个人知道的**，
	// 「必须改密」的全部意义就是让它在被本人改掉之前什么都做不了。
	// 能绕过的强制等于没有，而这里绕过之后拿到的还是下游系统的完整访问权。
	//
	// 跳改密页而不是报错：这时候人正站在下游的登录跳转里，
	// 一句 error 只会让他回到下游看一个含糊的失败，不知道该去哪。
	if id.MustChange {
		back := "/oidc/authorize?" + q.Encode()
		// 跳 /login 而不是某个专门的改密路由：前端的认证闸门看到"会话有效
		// 但必须改密"就会渲染改密页。多造一个路由等于多一处要同步的真相。
		c.Redirect(http.StatusFound, "/login?next="+url.QueryEscape(back))
		c.Abort()
		return nil, nil
	}

	// ── 访问判定 ──
	app, err := d.AppCat.GetApp(ctx, client.AppID)
	if err != nil {
		return nil, apierr.NotFound()
	}
	sub, err := d.Access.LoadSubject(ctx, id.UserID)
	if err != nil {
		return nil, err
	}
	rules, err := d.Access.LoadRulesForApp(ctx, client.AppID)
	if err != nil {
		return nil, err
	}
	// 生效中的临时提权一并叠加 —— 与网关判定同一套语义，不分叉
	if d.Approval != nil {
		if grants, err := d.Approval.ActiveFor(ctx, id.UserID, time.Now()); err == nil {
			rules = append(rules, approvalRules(grants)...)
		}
	}
	dec := access.Evaluate(sub, access.App{ID: app.ID, GroupIDs: app.GroupIDs}, rules)
	if !dec.Allowed() {
		ruleID := int64(0)
		if dec.Rule != nil {
			ruleID = dec.Rule.ID
		}
		d.Audit.Write(ctx, auditEntry{
			TenantID: id.TenantID, ActorID: id.UserID, ActorName: id.Username,
			Action: "oidc.authorize_denied", ObjectType: "app", ObjectID: client.AppID,
			ClientIP: clientIP(c),
			Detail:   map[string]any{"client_id": clientID, "reason": dec.Reason, "rule": ruleID},
		})
		// access_denied 是 OIDC 标准错误码，客户端库认得
		return nil, redirectErr(c, redirectURI, state, "access_denied",
			"未被授权访问该应用")
	}

	// ── 签发授权码 ──
	now := time.Now()
	ac, err := oidcp.NewAuthCode(clientID, id.UserID, id.TenantID, id.SessionID,
		redirectURI, q.Get("nonce"), q.Get("scope"), challenge,
		q.Get("code_challenge_method"), now, now)
	if err != nil {
		return nil, err
	}
	if err := d.OIDCP.SaveCode(ctx, ac); err != nil {
		return nil, err
	}
	d.Audit.Write(ctx, auditEntry{
		TenantID: id.TenantID, ActorID: id.UserID, ActorName: id.Username,
		Action: "oidc.authorize", ObjectType: "app", ObjectID: client.AppID,
		ClientIP: clientIP(c), Detail: map[string]any{"client_id": clientID},
	})

	u, _ := url.Parse(redirectURI)
	rq := u.Query()
	rq.Set("code", ac.Code)
	if state != "" {
		rq.Set("state", state)
	}
	u.RawQuery = rq.Encode()
	c.Redirect(http.StatusFound, u.String())
	c.Abort()
	return nil, nil
}

// oidcToken 令牌端点：用授权码换 id_token。
func oidcToken(c *gin.Context, d Deps) (any, error) {
	ctx := withDefaultTenant(c)

	clientID := c.PostForm("client_id")
	clientSecret := c.PostForm("client_secret")
	// 也支持 HTTP Basic —— 不少客户端库默认用它
	if u, p, ok := c.Request.BasicAuth(); ok {
		clientID, clientSecret = u, p
	}
	if c.PostForm("grant_type") != "authorization_code" {
		return nil, oidcErr(c, "unsupported_grant_type", "只支持 authorization_code")
	}

	client, err := d.OIDCP.GetClient(ctx, clientID)
	if err != nil || !client.Enabled {
		return nil, oidcErr(c, "invalid_client", "未知客户端")
	}
	if err := d.OIDCP.VerifySecret(ctx, clientID, clientSecret); err != nil {
		return nil, oidcErr(c, "invalid_client", "客户端认证失败")
	}

	ac, err := d.OIDCP.ConsumeCode(ctx, c.PostForm("code"))
	if err != nil {
		switch {
		case errors.Is(err, oidcp.ErrCodeExpired):
			return nil, oidcErr(c, "invalid_grant", "授权码已过期")
		default:
			// 已用过与不存在返回同一个错误 —— 区分开等于确认"这个 code 存在过"
			return nil, oidcErr(c, "invalid_grant", "授权码无效")
		}
	}
	// code 是发给谁的，就只能被谁换
	if ac.ClientID != clientID {
		return nil, oidcErr(c, "invalid_grant", "授权码与客户端不匹配")
	}
	// redirect_uri 必须与发码时**完全一致**：这是防止授权码被换到
	// 另一个回调地址的最后一道锁
	if ac.RedirectURI != c.PostForm("redirect_uri") {
		return nil, oidcErr(c, "invalid_grant", "redirect_uri 与发码时不一致")
	}
	if err := oidcp.VerifyPKCE(ac.Challenge, ac.Method, c.PostForm("code_verifier")); err != nil {
		return nil, oidcErr(c, "invalid_grant", "PKCE 校验失败")
	}

	sub, err := d.OIDCP.LoadSubject(ctx, ac.UserID)
	if err != nil {
		return nil, err
	}
	key, err := d.OIDCP.ActiveKey(withPlatformTenant(c))
	if err != nil {
		return nil, err
	}
	sid, err := oidcp.NewSID()
	if err != nil {
		return nil, err
	}
	now := time.Now()
	claims := oidcp.IDTokenClaims(client, sub, d.Issuer, ac.Nonce, sid, ac.AuthTime, now)
	idToken, err := oidcp.SignIDToken(key, claims)
	if err != nil {
		return nil, err
	}
	// 记下下游会话，关口登出时按 sid 通知它们
	_ = d.OIDCP.RecordSession(ctx, ac.SessionID, ac.UserID, clientID, sid)

	d.Audit.Write(ctx, auditEntry{
		TenantID: ac.TenantID, ActorID: ac.UserID, ActorName: sub.Username,
		Action: "oidc.token", ObjectType: "app", ObjectID: client.AppID,
		ClientIP: clientIP(c), Detail: map[string]any{"client_id": clientID},
	})

	// 不发 refresh_token：关口的会话本身就是"长期凭证"，
	// 再发一个可离线续期的 refresh_token，等于绕开了会话注销与离职断权。
	return gin.H{
		"access_token": idToken, // 复用同一个 JWT，userinfo 用它校验
		"id_token":     idToken,
		"token_type":   "Bearer",
		"expires_in":   int(client.IDTokenTTL.Seconds()),
		"scope":        ac.Scope,
	}, nil
}

// oidcUserinfo 用户信息端点。
func oidcUserinfo(c *gin.Context, d Deps) (any, error) {
	tok := strings.TrimPrefix(c.GetHeader("Authorization"), "Bearer ")
	if tok == "" {
		return nil, apierr.Unauthorized()
	}
	ctx := withPlatformTenant(c)
	keys, err := d.OIDCP.Keys(ctx)
	if err != nil {
		return nil, err
	}
	uid, err := d.OIDCP.VerifyOwnToken(tok, keys, d.Issuer, time.Now())
	if err != nil {
		return nil, apierr.Unauthorized()
	}
	sub, err := d.OIDCP.LoadSubject(withDefaultTenant(c), uid)
	if err != nil {
		return nil, err
	}
	return gin.H{
		"sub": itoa(uid), "name": sub.DisplayName, "email": sub.Email,
		// ⚠️ 空切片要给 []，不能给 nil —— Go 的 nil 切片序列化成 null，
		// 下游拿到 null 去做 .length / 遍历就是运行期错误。
		// id_token 那边是"没有组就不带这个 claim"（见 provider.go），
		// 而 userinfo 是固定形状的响应体，字段在就必须是数组。
		"preferred_username": sub.Username, "groups": orEmpty(sub.Groups),
		"employee_id": sub.EmployeeID,
	}, nil
}

// oidcLogout RP 发起的登出：注销关口会话，并把下游会话一并标记注销。
//
// 只清自己的 cookie 而下游还登着，"单点登出"就是假的。
func oidcLogout(c *gin.Context, d Deps) (any, error) {
	ctx := withDefaultTenant(c)
	id := identity(c)
	post := c.Query("post_logout_redirect_uri")

	if id.SessionID > 0 {
		_ = d.OIDCP.RevokeGateSessions(ctx, id.SessionID)
		_ = d.Auth.Revoke(ctx, id.SessionID, "oidc_logout")
	}
	c.SetCookie(SessionCookie, "", -1, "/", "", false, true)

	if post != "" {
		// 跳转地址必须在白名单里 —— 否则登出接口就是个开放重定向
		if cid := c.Query("client_id"); cid != "" {
			if client, err := d.OIDCP.GetClient(ctx, cid); err == nil && client.ValidatePostLogout(post) {
				c.Redirect(http.StatusFound, post)
				c.Abort()
				return nil, nil
			}
		}
		return nil, apierr.BadRequest(apierr.CodeOIDCBadRedirect, nil)
	}
	c.Redirect(http.StatusFound, "/login")
	c.Abort()
	return nil, nil
}

// ── 客户端管理（控制台）──────────────────────────────────────────

func listOIDCClients(c *gin.Context, d Deps) (any, error) {
	cs, err := d.OIDCP.ListClients(c.Request.Context())
	if err != nil {
		return nil, err
	}
	out := make([]gin.H, 0, len(cs))
	for _, x := range cs {
		out = append(out, gin.H{
			"id": x.ID, "app_id": x.AppID, "client_id": x.ClientID,
			"redirect_uris": x.RedirectURIs, "post_logout_uris": x.PostLogout,
			"scopes": x.Scopes, "public_client": x.PublicClient,
			"require_pkce": x.RequirePKCE, "claims": x.Claims,
			"id_token_ttl_sec": int(x.IDTokenTTL.Seconds()), "enabled": x.Enabled,
			// ⚠️ 绝不返回 secret，连哈希都不返回
		})
	}
	return gin.H{"items": out, "total": len(out), "issuer": d.Issuer}, nil
}

func createOIDCClient(c *gin.Context, d Deps) (any, error) {
	var req clientOpts
	if err := c.ShouldBindJSON(&req); err != nil || req.AppID == 0 {
		return nil, apierr.BadRequest(apierr.CodeInvalidParam, nil)
	}
	clientID, secret, err := createClientFor(c, d, req)
	if err != nil {
		return nil, err
	}
	return gin.H{
		"client_id": clientID,
		// 明文只返回这一次
		"client_secret": secret,
		"warning_code":  "oidc.secret_shown_once",
		"issuer":        d.Issuer,
		"discovery_url": d.Issuer + "/.well-known/openid-configuration",
	}, nil
}

// ── 工具 ──

// redirectErr 按 OIDC 协议把错误跳回客户端。
func redirectErr(c *gin.Context, redirectURI, state, code, desc string) error {
	u, err := url.Parse(redirectURI)
	if err != nil {
		return apierr.BadRequest(apierr.CodeOIDCBadRedirect, nil)
	}
	q := u.Query()
	q.Set("error", code)
	q.Set("error_description", desc)
	if state != "" {
		q.Set("state", state)
	}
	u.RawQuery = q.Encode()
	c.Redirect(http.StatusFound, u.String())
	c.Abort()
	return nil
}

// oidcErr token 端点的错误格式（RFC 6749 §5.2）。
func oidcErr(c *gin.Context, code, desc string) error {
	status := http.StatusBadRequest
	if code == "invalid_client" {
		status = http.StatusUnauthorized
	}
	c.JSON(status, gin.H{"error": code, "error_description": desc})
	c.Abort()
	return nil
}

// withDefaultTenant OIDC 端点是按 client_id 定位租户的，
// 但在查到 client 之前就需要一个租户上下文。目前单租户，固定 1；
// 多租户上线后改成按 client_id 前缀或独立域名区分。
func withDefaultTenant(c *gin.Context) context.Context {
	if id := identity(c); id.TenantID > 0 {
		return store.WithTenant(c.Request.Context(), store.TenantID(id.TenantID))
	}
	return store.WithTenant(c.Request.Context(), store.TenantID(1))
}

// withPlatformTenant 签名密钥是平台级的，但 store 仍要求上下文里有租户。
func withPlatformTenant(c *gin.Context) context.Context {
	return store.WithTenant(c.Request.Context(), store.TenantID(1))
}

// orEmpty 保证 JSON 里出现的是 [] 而不是 null。
func orEmpty(v []string) []string {
	if v == nil {
		return []string{}
	}
	return v
}

// clientOpts 建一个 OIDC 客户端要的全部输入。
//
// client_id 不在里面：它一律由系统生成（见 generateClientID）。
type clientOpts struct {
	AppID        int64    `json:"app_id"`
	RedirectURIs []string `json:"redirect_uris"`
	PostLogout   []string `json:"post_logout_uris"`
	PublicClient bool     `json:"public_client"`
	Claims       []string `json:"claims"`
	TTLSec       int      `json:"id_token_ttl_sec"`
}

// createClientFor 建一个 OIDC 客户端，返回 (client_id, 明文 secret)。
//
// 抽出来是因为有两条路会用：接入应用时顺带建、以及单独在客户端页建。
// 两条路必须产出完全一样的东西 —— 各写一遍迟早会分叉，
// 而分叉的表现是"从 A 路建的能用、从 B 路建的不能用"，极难查。
func createClientFor(c *gin.Context, d Deps, o clientOpts) (string, string, error) {
	// 没有回调地址的客户端**跑不完任何一次登录**（授权端点第一步就会拒）。
	// 允许建出来，等于在库里留一个看着正常、一用就报错的东西。
	if len(o.RedirectURIs) == 0 {
		return "", "", apierr.BadRequest(apierr.CodeOIDCBadRedirect, nil)
	}
	if len(o.Claims) == 0 {
		o.Claims = []string{"sub", "name", "email", "groups", "employee_id"}
	}
	clientID, err := generateClientID()
	if err != nil {
		return "", "", err
	}
	client := oidcp.Client{
		AppID: o.AppID, ClientID: clientID,
		RedirectURIs: o.RedirectURIs, PostLogout: o.PostLogout,
		Scopes:       []string{"openid", "profile", "email", "groups"},
		PublicClient: o.PublicClient, RequirePKCE: true,
		IDTokenTTL: time.Duration(o.TTLSec) * time.Second, Claims: o.Claims,
	}
	secret, err := d.OIDCP.CreateClient(c.Request.Context(), client, actorID(c))
	if err != nil {
		return "", "", err
	}
	d.Audit.Write(c.Request.Context(), auditEntry{
		TenantID: identity(c).TenantID, ActorID: actorID(c), ActorName: identity(c).Username,
		Action: "oidc_client.create", ObjectType: "app", ObjectID: o.AppID,
		ClientIP: clientIP(c), Detail: map[string]any{"client_id": clientID},
		// ⚠️ secret 绝不写进审计
	})
	return clientID, secret, nil
}
