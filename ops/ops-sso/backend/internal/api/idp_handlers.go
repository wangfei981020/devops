package api

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"ops-sso-backend/internal/apierr"
	"ops-sso-backend/internal/domain/idp"
)

// idpList 登录页要显示哪些「用 XX 登录」按钮。
//
// 未认证接口，所以只返回 id 与显示名 —— 连 client_id 都不给。
func idpList(c *gin.Context, d Deps) (any, error) {
	if d.IdP == nil {
		return gin.H{"items": []gin.H{}}, nil
	}
	cfgs, err := d.IdP.ListEnabled(c.Request.Context())
	if err != nil {
		// 上游配置读不出来时，登录页要**显示本地登录**而不是白屏。
		// 返回空列表 + 错误标记，让前端能说清"企业登录暂时不可用，可用本地账号"
		return gin.H{"items": []gin.H{}, "degraded": true}, nil
	}
	out := make([]gin.H, 0, len(cfgs))
	for _, cfg := range cfgs {
		out = append(out, gin.H{"id": cfg.ID, "name": cfg.Name})
	}
	return gin.H{"items": out}, nil
}

// idpStart 把人送去上游登录。
func idpStart(c *gin.Context, d Deps) (any, error) {
	cfgID, _ := strconv.ParseInt(c.Query("config_id"), 10, 64)
	if cfgID <= 0 || d.IdP == nil {
		return nil, apierr.BadRequest(apierr.CodeInvalidParam, map[string]any{"field": "config_id"})
	}
	ctx := c.Request.Context()
	cfg, _, err := d.IdP.Get(ctx, cfgID)
	if err != nil || !cfg.Enabled {
		return nil, apierr.NotFound()
	}

	st, err := idp.NewAuthState(cfgID, safeNext(c.Query("next")))
	if err != nil {
		return nil, err
	}
	if err := d.IdP.SaveState(ctx, st); err != nil {
		return nil, err
	}
	c.Redirect(http.StatusFound, cfg.AuthorizeURL(st))
	c.Abort()
	return nil, nil
}

// safeNext 只允许站内相对路径。
//
// 不做这个校验的话，`?next=https://evil.example.com` 就是一个开放重定向 ——
// 攻击者拿我们的域名做钓鱼跳板，而登录流程看起来完全正常。
func safeNext(next string) string {
	if next == "" || !strings.HasPrefix(next, "/") {
		return "/"
	}
	// `//evil.com` 会被浏览器当成协议相对的绝对地址
	if strings.HasPrefix(next, "//") {
		return "/"
	}
	return next
}

// idpCallback 上游回调：验签 → 校验 claim → 落地用户 → 发会话。
func idpCallback(c *gin.Context, d Deps) (any, error) {
	ctx := c.Request.Context()
	if e := c.Query("error"); e != "" {
		// 上游明确拒绝（用户点了取消、应用未授权等）。
		// 不把上游的 error_description 原样显示 —— 那是别人家的文案，
		// 且可能被注入。给自己的码，前端渲染自己的文案。
		return nil, apierr.New(http.StatusUnauthorized, apierr.CodeIdPRejected, nil)
	}
	code, state := c.Query("code"), c.Query("state")
	if code == "" || state == "" {
		return nil, apierr.BadRequest(apierr.CodeInvalidParam, nil)
	}

	st, err := d.IdP.ConsumeState(ctx, state)
	if err != nil {
		return nil, idpErr(err)
	}
	cfg, clientSecret, err := d.IdP.Get(ctx, st.ConfigID)
	if err != nil {
		return nil, apierr.NotFound()
	}

	idToken, err := d.IdP.ExchangeCode(ctx, cfg, clientSecret, code, st.CodeVerifier)
	if err != nil {
		return nil, apierr.New(http.StatusBadGateway, apierr.CodeIdPUnreachable, nil)
	}

	// ★ 顺序不能反：先验签，再解析、再信任里面的任何字段。
	// 反了就等于信任任何人伪造的 token。
	if err := d.IdP.VerifyToken(ctx, cfg, idToken); err != nil {
		return nil, apierr.New(http.StatusUnauthorized, apierr.CodeIdPBadToken, nil)
	}
	claims, err := idp.ParseClaims(idToken)
	if err != nil {
		return nil, apierr.New(http.StatusUnauthorized, apierr.CodeIdPBadToken, nil)
	}
	if err := cfg.Validate(claims, st, time.Now()); err != nil {
		return nil, apierr.New(http.StatusUnauthorized, apierr.CodeIdPBadToken, nil)
	}

	ident, err := cfg.MapIdentity(claims)
	if err != nil {
		// 拿不到稳定标识：这是**配置问题**，不是用户的错。
		// 单独的码，让界面提示管理员去检查 subject_claim。
		return nil, apierr.New(http.StatusUnauthorized, apierr.CodeIdPNoSubject, nil)
	}

	tenantID := int64(1)
	uid, created, err := d.IdP.Resolve(ctx, cfg, ident, tenantID)
	if err != nil {
		if errors.Is(err, idp.ErrJITDisabled) {
			return nil, apierr.New(http.StatusForbidden, apierr.CodeIdPNoAccount, nil)
		}
		return nil, err
	}

	tok, identity, err := d.Auth.IssueForExternal(ctx, uid, tenantID, "oidc",
		clientIP(c), c.GetHeader("User-Agent"))
	if err != nil {
		return nil, err
	}
	setSessionCookie(c, tok)

	d.Audit.Write(ctx, auditEntry{
		TenantID: tenantID, ActorID: uid, ActorName: identity.Username,
		Action: "auth.oidc_login", ObjectType: "user", ObjectID: uid, ClientIP: clientIP(c),
		Detail: map[string]any{"idp": cfg.Name, "jit_created": created},
	})

	// 浏览器过来的是一次跳转，不是 XHR —— 直接跳回原来要去的地方
	c.Redirect(http.StatusFound, safeNext(st.Next))
	c.Abort()
	return nil, nil
}

func idpErr(err error) error {
	switch {
	case errors.Is(err, idp.ErrStateExpired):
		return apierr.New(http.StatusUnauthorized, apierr.CodeIdPStateExpired, nil)
	case errors.Is(err, idp.ErrStateMismatch):
		return apierr.New(http.StatusUnauthorized, apierr.CodeIdPStateInvalid, nil)
	}
	return err
}
