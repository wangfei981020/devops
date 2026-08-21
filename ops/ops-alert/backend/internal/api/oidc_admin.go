package api

import (
	"encoding/json"
	"net/http"

	"github.com/gin-gonic/gin"

	"ops-alert-backend/internal/api/middleware"
)

type oidcReq struct {
	Enabled       bool              `json:"enabled"`
	DisplayName   string            `json:"display_name"`
	Issuer        string            `json:"issuer"`
	AutoDiscover  bool              `json:"auto_discover"`
	AuthURL       string            `json:"auth_url"`
	TokenURL      string            `json:"token_url"`
	UserinfoURL   string            `json:"userinfo_url"`
	JWKSURL       string            `json:"jwks_url"`
	ClientID      string            `json:"client_id"`
	ClientSecret  string            `json:"client_secret"`
	Scopes        string            `json:"scopes"`
	UsernameClaim string            `json:"username_claim"`
	EmailClaim    string            `json:"email_claim"`
	NameClaim     string            `json:"name_claim"`
	GroupsClaim   string            `json:"groups_claim"`
	ClaimSource   string            `json:"claim_source"`
	JITEnabled    bool              `json:"jit_enabled"`
	DefaultRole   string            `json:"default_role"`
	RoleMapping   map[string]string `json:"role_mapping"`
}

func (s *Server) getOIDCConfig(c *gin.Context) {
	cfg, err := s.loadOIDC(c.Request.Context())
	if err != nil {
		abortQuery(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"enabled": cfg.Enabled, "display_name": cfg.DisplayName,
		"issuer": cfg.Issuer, "auto_discover": cfg.AutoDiscover,
		"auth_url": cfg.AuthURL, "token_url": cfg.TokenURL,
		"userinfo_url": cfg.UserinfoURL, "jwks_url": cfg.JWKSURL,
		"client_id": cfg.ClientID,
		// 🔴 密钥**只回"配没配过"，绝不回原文**。
		// 回原文的话，任何能打开这个页面的人都能拿走它 ——
		// 而这个页面的权限是 menu 级的，不是"能看密钥"级的。
		"client_secret_set": cfg.ClientSecret != "",
		"scopes":            cfg.Scopes,
		"username_claim":    cfg.UsernameClaim, "email_claim": cfg.EmailClaim,
		"name_claim": cfg.NameClaim, "groups_claim": cfg.GroupsClaim,
		"claim_source": cfg.ClaimSource,
		"jit_enabled":  cfg.JITEnabled, "default_role": cfg.DefaultRole,
		"role_mapping": cfg.RoleMapping,
		// 回调地址由请求推导，界面要显示它 —— 用户得把这一串
		// 一字不差地填进 IdP 的应用配置里
		"redirect_uri": redirectURI(c),
	})
}

func (s *Server) saveOIDCConfig(c *gin.Context) {
	var req oidcReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "bad_request"})
		return
	}
	// ⚠️ 开启前先挡掉明显配不通的组合。不挡的话用户点了"启用"，
	// 登录页出现按钮，点下去跳到一个空 URL —— 浏览器只说"网址无效"
	if req.Enabled {
		if req.ClientID == "" {
			c.JSON(http.StatusBadRequest, gin.H{
				"error": "missing_client_id", "detail": "客户端 ID 不能为空"})
			return
		}
		if req.AutoDiscover && req.Issuer == "" {
			c.JSON(http.StatusBadRequest, gin.H{
				"error": "missing_issuer", "detail": "开启自动发现时 issuer 不能为空"})
			return
		}
		if !req.AutoDiscover && (req.AuthURL == "" || req.TokenURL == "") {
			c.JSON(http.StatusBadRequest, gin.H{
				"error":  "missing_endpoints",
				"detail": "关闭自动发现时，授权端点和令牌端点都必须手填"})
			return
		}
		if req.UsernameClaim == "" {
			c.JSON(http.StatusBadRequest, gin.H{
				"error":  "missing_username_claim",
				"detail": "用户名字段不能为空——拿不到用户名就没法建立本地账号"})
			return
		}
	}
	// 🔴 默认角色不能是 admin。给了的话，身份源里任何一个人
	// 登录一次就成了这套系统的管理员。
	if req.DefaultRole == "admin" {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "unsafe_default_role",
			"detail": "默认角色不能是管理员。身份源里任何一个人登录一次就会成为本系统管理员——" +
				"请用 viewer 或 oncall，再通过群组映射给少数人更高权限。"})
		return
	}
	if req.ClaimSource != "id_token" && req.ClaimSource != "userinfo" && req.ClaimSource != "both" {
		req.ClaimSource = "both"
	}

	mapping, _ := json.Marshal(req.RoleMapping)
	p := s.st.Platform("api/oidc_admin.go")
	// 密钥留空 = 不改。这样用户改别的字段时不用重新输一遍 ——
	// 而"每次保存都要重填密钥"必然导致有人把它清空
	secretSQL, args := "", []any{}
	if req.ClientSecret != "" {
		enc, err := s.cipher.Encrypt(req.ClientSecret)
		if err != nil {
			abortQuery(c, err)
			return
		}
		secretSQL = ", client_secret_enc = ?"
		args = append(args, enc)
	}
	user := middleware.CurrentUser(c)
	q := `UPDATE oidc_config SET enabled=?, display_name=?, issuer=?, auto_discover=?,
			auth_url=?, token_url=?, userinfo_url=?, jwks_url=?, client_id=?,
			scopes=?, username_claim=?, email_claim=?, name_claim=?, groups_claim=?,
			claim_source=?, jit_enabled=?, default_role=?, role_mapping=?, updated_by=?` +
		secretSQL + ` WHERE id=1`
	full := []any{boolToInt(req.Enabled), req.DisplayName, req.Issuer, boolToInt(req.AutoDiscover),
		req.AuthURL, req.TokenURL, req.UserinfoURL, req.JWKSURL, req.ClientID,
		req.Scopes, req.UsernameClaim, req.EmailClaim, req.NameClaim, req.GroupsClaim,
		req.ClaimSource, boolToInt(req.JITEnabled), req.DefaultRole, mapping, user.Username}
	full = append(full, args...)
	if _, err := p.Exec(c.Request.Context(), q, full...); err != nil {
		abortQuery(c, err)
		return
	}
	if sc, err := s.st.Tenant(c.Request.Context()); err == nil {
		s.audit(c, sc, "oidc.save", "oidc", 1,
			gin.H{"enabled": req.Enabled, "issuer": req.Issuer, "jit": req.JITEnabled})
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// testOIDC 试探身份源是否可达、端点是否解析得出来。
//
// # 为什么必须有
//
// 没有它的话，配置对不对只能靠"真登录一次"验证 —— 而真登录一次会把
// 浏览器跳走，失败时停在 IdP 的错误页上，回不到配置界面。
// 接入 SSO 本来就要来回调几次，每次都跳走一遍代价太大。
//
// ⚠️ 它验的是**连通性和端点**，验不了 client_secret 对不对
// （那要真的走一次授权码流程）。返回里要说清楚这一点，
// 否则"测试通过"会被理解成"配好了"。
func (s *Server) testOIDC(c *gin.Context) {
	cfg, err := s.loadOIDC(c.Request.Context())
	if err != nil {
		abortQuery(c, err)
		return
	}
	steps := []gin.H{}
	ok := true
	add := func(name string, good bool, detail string) {
		steps = append(steps, gin.H{"step": name, "ok": good, "detail": detail})
		if !good {
			ok = false
		}
	}

	if cfg.ClientID == "" {
		add("客户端 ID", false, "没有填")
	} else {
		add("客户端 ID", true, cfg.ClientID)
	}
	if cfg.ClientSecret == "" {
		// 公共客户端（PKCE）确实可以没有密钥，所以这里是提醒不是失败
		add("客户端密钥", true, "未设置。若身份源要求机密客户端，令牌交换会失败")
	} else {
		add("客户端密钥", true, "已设置")
	}

	if err := s.resolveEndpoints(c.Request.Context(), cfg); err != nil {
		add("端点解析", false, err.Error())
	} else {
		add("端点解析", true, "授权端点 "+cfg.AuthURL)
		if cfg.UserinfoURL == "" && cfg.ClaimSource != "id_token" {
			// ⚠️ 这条是 MXID 那个坑的预防：claim 来源设成要读 userinfo，
			// 但根本没有 userinfo 端点 —— 登录时表现为"某些字段拿不到"
			add("userinfo 端点", false,
				"claim 来源包含 userinfo，但没有 userinfo 端点。"+
					"要么手填它，要么把 claim 来源改成「只读 id_token」")
		} else if cfg.UserinfoURL != "" {
			add("userinfo 端点", true, cfg.UserinfoURL)
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"ok": ok, "steps": steps,
		"redirect_uri": redirectURI(c),
		// 说清楚这次测试**没有**验什么，否则"测试通过"会被当成"配好了"
		"caveat": "本测试只验证连通性与端点。客户端密钥是否正确、" +
			"claim 名是否对得上，只有真正登录一次才知道 —— " +
			"登录失败时的提示里会列出身份源实际下发的字段名。",
	})
}
