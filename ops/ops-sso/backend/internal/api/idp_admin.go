package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"ops-sso-backend/internal/apierr"
	"ops-sso-backend/logx"
)

// ══════════════════════════════════════════════════════════════════
// 身份源（IdP）管理
// ══════════════════════════════════════════════════════════════════
//
// 在这之前，身份源只能**直接写库**：登录用的公开接口早就有了，
// 但没有任何一个管理接口能建它。装上这套系统的人要接飞书/Teams，
// 第一步就得去连数据库 —— 那不叫产品。

// idpConfigJSON 一条身份源配置。
//
// ⚠️ **绝不返回 client_secret**，哪怕是密文。它是换 token 的凭据，
// 一旦泄露，对方就能拿着它冒充本系统去上游换令牌。
// 界面上只显示"已配置 / 未配置"。
func idpConfigJSON(r idpRow) gin.H {
	return gin.H{
		"id": r.ID, "name": r.Name, "protocol": r.Protocol, "issuer": r.Issuer,
		"client_id": r.ClientID, "has_secret": r.HasSecret,
		"auth_url": r.AuthURL, "token_url": r.TokenURL, "jwks_url": r.JWKSURL,
		"redirect_uri": r.RedirectURI, "scopes": r.Scopes,
		"subject_claim": r.SubjectClaim, "name_claim": r.NameClaim,
		"email_claim": r.EmailClaim, "groups_claim": r.GroupsClaim,
		"jit_create": r.JITCreate, "jit_groups": r.JITGroups,
		"enabled": r.Enabled,
	}
}

type idpRow struct {
	ID                                               int64
	Name, Protocol, Issuer, ClientID                 string
	AuthURL, TokenURL, JWKSURL, RedirectURI, Scopes  string
	SubjectClaim, NameClaim, EmailClaim, GroupsClaim string
	JITGroups                                        string
	HasSecret, JITCreate, Enabled                    bool
}

func idpAdminList(c *gin.Context, d Deps) (any, error) {
	rows, err := d.Store.Raw().QueryContext(c.Request.Context(),
		`SELECT id, name, protocol, issuer, client_id, LENGTH(client_secret_enc) > 0,
		        auth_url, token_url, jwks_url, redirect_uri, scopes,
		        subject_claim, name_claim, email_claim, groups_claim,
		        jit_create, jit_groups, enabled
		 FROM idp_configs ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []gin.H{}
	for rows.Next() {
		var r idpRow
		var jit, enabled int
		if err := rows.Scan(&r.ID, &r.Name, &r.Protocol, &r.Issuer, &r.ClientID, &r.HasSecret,
			&r.AuthURL, &r.TokenURL, &r.JWKSURL, &r.RedirectURI, &r.Scopes,
			&r.SubjectClaim, &r.NameClaim, &r.EmailClaim, &r.GroupsClaim,
			&jit, &r.JITGroups, &enabled); err != nil {
			return nil, err
		}
		r.JITCreate, r.Enabled = jit == 1, enabled == 1
		out = append(out, idpConfigJSON(r))
	}
	return gin.H{"items": out, "total": len(out)}, rows.Err()
}

type idpSaveReq struct {
	Name         string `json:"name"`
	Issuer       string `json:"issuer"`
	ClientID     string `json:"client_id"`
	ClientSecret string `json:"client_secret"`
	AuthURL      string `json:"auth_url"`
	TokenURL     string `json:"token_url"`
	JWKSURL      string `json:"jwks_url"`
	RedirectURI  string `json:"redirect_uri"`
	Scopes       string `json:"scopes"`
	SubjectClaim string `json:"subject_claim"`
	NameClaim    string `json:"name_claim"`
	EmailClaim   string `json:"email_claim"`
	GroupsClaim  string `json:"groups_claim"`
	JITCreate    bool   `json:"jit_create"`
	JITGroups    string `json:"jit_groups"`
	Enabled      bool   `json:"enabled"`
}

func idpAdminSave(c *gin.Context, d Deps) (any, error) {
	var req idpSaveReq
	if err := c.ShouldBindJSON(&req); err != nil {
		return nil, apierr.BadRequest(apierr.CodeInvalidParam, nil)
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" || strings.TrimSpace(req.ClientID) == "" {
		return nil, apierr.BadRequest(apierr.CodeInvalidParam, nil)
	}
	// 三个地址缺一个，登录就会在某一步失败 —— 而失败点在上游，很难查。
	// 在这里拦下来，比让人配完再去猜哪一步断了强。
	if req.AuthURL == "" || req.TokenURL == "" || req.JWKSURL == "" {
		return nil, apierr.BadRequest(apierr.CodeIdPMissingEndpoint, nil)
	}
	if req.SubjectClaim == "" {
		req.SubjectClaim = "sub"
	}

	ctx := c.Request.Context()
	id := identity(c)
	idStr := c.Param("id")

	if idStr == "" {
		if req.ClientSecret == "" {
			return nil, apierr.BadRequest(apierr.CodeIdPNeedSecret, nil)
		}
		enc, err := d.Secrets.Seal([]byte(req.ClientSecret))
		if err != nil {
			return nil, err
		}
		res, err := d.Store.Raw().ExecContext(ctx, `INSERT INTO idp_configs
			(name, protocol, issuer, client_id, client_secret_enc, auth_url, token_url, jwks_url,
			 redirect_uri, scopes, subject_claim, name_claim, email_claim, groups_claim,
			 jit_create, jit_groups, enabled)
			VALUES (?, 'oidc', ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			req.Name, req.Issuer, req.ClientID, enc, req.AuthURL, req.TokenURL, req.JWKSURL,
			req.RedirectURI, req.Scopes, req.SubjectClaim, req.NameClaim, req.EmailClaim,
			req.GroupsClaim, b2i(req.JITCreate), req.JITGroups, b2i(req.Enabled))
		if err != nil {
			return nil, err
		}
		newID, _ := res.LastInsertId()
		d.Audit.Write(ctx, auditEntry{
			TenantID: id.TenantID, ActorID: actorID(c), ActorName: id.Username,
			Action: "idp.create", ObjectType: "idp", ObjectID: newID, ClientIP: clientIP(c),
			// ⚠️ 不记 secret
			Detail: map[string]any{"name": req.Name, "issuer": req.Issuer, "jit": req.JITCreate},
		})
		return gin.H{"id": newID}, nil
	}

	cfgID, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		return nil, apierr.BadRequest(apierr.CodeInvalidParam, nil)
	}

	// 空 secret = 不改。让人每次编辑都重填一遍密钥，只会导致他去别处复制粘贴，
	// 或者干脆换一个更好记（更弱）的。
	if req.ClientSecret != "" {
		enc, err := d.Secrets.Seal([]byte(req.ClientSecret))
		if err != nil {
			return nil, err
		}
		if _, err := d.Store.Raw().ExecContext(ctx,
			`UPDATE idp_configs SET client_secret_enc = ? WHERE id = ?`, enc, cfgID); err != nil {
			return nil, err
		}
	}
	if _, err := d.Store.Raw().ExecContext(ctx, `UPDATE idp_configs SET
		name = ?, issuer = ?, client_id = ?, auth_url = ?, token_url = ?, jwks_url = ?,
		redirect_uri = ?, scopes = ?, subject_claim = ?, name_claim = ?, email_claim = ?,
		groups_claim = ?, jit_create = ?, jit_groups = ?, enabled = ?
		WHERE id = ?`,
		req.Name, req.Issuer, req.ClientID, req.AuthURL, req.TokenURL, req.JWKSURL,
		req.RedirectURI, req.Scopes, req.SubjectClaim, req.NameClaim, req.EmailClaim,
		req.GroupsClaim, b2i(req.JITCreate), req.JITGroups, b2i(req.Enabled), cfgID); err != nil {
		return nil, err
	}
	d.Audit.Write(ctx, auditEntry{
		TenantID: id.TenantID, ActorID: actorID(c), ActorName: id.Username,
		Action: "idp.update", ObjectType: "idp", ObjectID: cfgID, ClientIP: clientIP(c),
		Detail: map[string]any{
			"name": req.Name, "enabled": req.Enabled, "jit": req.JITCreate,
			"secret_changed": req.ClientSecret != "",
		},
	})
	return gin.H{"id": cfgID}, nil
}

// idpDiscover 拉上游的 discovery 文档，把三个地址填出来。
//
// # 为什么值得做
//
// 手抄 authorization/token/jwks 三个地址是接入时最容易出错的一步，
// 而抄错的表现是「跳过去就白屏」或「回来报一个上游的错」——
// 都指不回本系统的配置。让机器去抄。
//
// # ⚠️ 这是一个由管理员指定目标的服务端出站请求（SSRF 面）
//
// 缓解：只允许 https、拒绝解析到私网/回环的地址、5 秒超时、不跟跳转。
// 拒绝私网是关键 —— 否则这就是一个探测内网的接口，
// 而它的返回体会把探测结果直接交给调用方。
func idpDiscover(c *gin.Context, d Deps) (any, error) {
	var req struct {
		Issuer string `json:"issuer"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		return nil, apierr.BadRequest(apierr.CodeInvalidParam, nil)
	}
	raw := strings.TrimRight(strings.TrimSpace(req.Issuer), "/")
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Host == "" {
		return nil, apierr.BadRequest(apierr.CodeIdPBadIssuer, nil)
	}
	// 查询串和片段会被原样拼进 `<issuer>/.well-known/...`，拼出来的根本不是
	// 合法的 discovery 地址。⚠️ 这条原先只写在错误文案里而**没有实现** ——
	// 文案承诺了一条不存在的规则，比不写还糟：看的人会以为已经拦住了。
	if u.RawQuery != "" || u.Fragment != "" {
		return nil, apierr.BadRequest(apierr.CodeIdPBadIssuer, nil)
	}
	if err := rejectPrivateHost(u.Hostname()); err != nil {
		return nil, apierr.BadRequest(apierr.CodeIdPPrivateHost, map[string]any{"host": u.Hostname()})
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()
	httpReq, _ := http.NewRequestWithContext(ctx, http.MethodGet,
		raw+"/.well-known/openid-configuration", nil)
	cl := &http.Client{
		Timeout: 5 * time.Second,
		// 不跟跳转：跳转能把请求带去一个绕过上面那些检查的地址
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	resp, err := cl.Do(httpReq)
	if err != nil {
		logx.Line("idp", "拉 discovery 失败 issuer="+raw+": "+err.Error())
		return nil, apierr.BadRequest(apierr.CodeIdPUnreachable, nil)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, apierr.BadRequest(apierr.CodeIdPUnreachable,
			map[string]any{"status": resp.StatusCode})
	}
	var doc struct {
		Issuer   string `json:"issuer"`
		AuthURL  string `json:"authorization_endpoint"`
		TokenURL string `json:"token_endpoint"`
		JWKSURL  string `json:"jwks_uri"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&doc); err != nil {
		return nil, apierr.BadRequest(apierr.CodeIdPUnreachable, nil)
	}
	return gin.H{
		"issuer": doc.Issuer, "auth_url": doc.AuthURL,
		"token_url": doc.TokenURL, "jwks_url": doc.JWKSURL,
	}, nil
}

// rejectPrivateHost 拒绝解析到私网 / 回环 / 链路本地的主机名。
func rejectPrivateHost(host string) error {
	ips, err := net.LookupIP(host)
	if err != nil {
		return fmt.Errorf("解析不了 %s", host)
	}
	for _, ip := range ips {
		if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsUnspecified() {
			return fmt.Errorf("%s 解析到内网地址 %s", host, ip)
		}
	}
	return nil
}

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}
