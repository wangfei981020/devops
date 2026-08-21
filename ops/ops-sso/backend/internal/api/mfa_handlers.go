package api

import (
	"errors"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"ops-sso-backend/internal/apierr"
	"ops-sso-backend/internal/domain/mfa"
)

// ── 绑定 ─────────────────────────────────────────────────────────

// mfaStatus 我绑没绑二次验证。
func mfaStatus(c *gin.Context, d Deps) (any, error) {
	ok, err := d.MFA.Enrolled(c.Request.Context(), actorID(c))
	if err != nil {
		return nil, err
	}
	return gin.H{"enrolled": ok}, nil
}

// mfaBeginEnroll 生成密钥与二维码内容。
//
// 密钥明文只在这一次返回。前端要提示「用验证器扫码后必须验一次才算绑定成功」——
// 少了确认那一步，用户扫失败却以为绑好了，下次需要验证时就被永久挡在门外。
func mfaBeginEnroll(c *gin.Context, d Deps) (any, error) {
	id := identity(c)
	account := id.Username
	if account == "" {
		account = "user"
	}
	secret, uri, err := d.MFA.BeginEnroll(c.Request.Context(), id.UserID, "OneGate", account)
	if err != nil {
		return nil, err
	}
	d.Audit.Write(c.Request.Context(), auditEntry{
		TenantID: id.TenantID, ActorID: id.UserID, ActorName: id.Username,
		Action: "mfa.enroll_begin", ObjectType: "user", ObjectID: id.UserID, ClientIP: clientIP(c),
		// ⚠️ 绝不把密钥写进审计 —— 审计会被导出、转发、截图
	})
	return gin.H{"secret": secret, "otpauth_uri": uri, "confirm_required": true}, nil
}

func mfaConfirmEnroll(c *gin.Context, d Deps) (any, error) {
	var req struct {
		Code string `json:"code"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		return nil, apierr.BadRequest(apierr.CodeInvalidParam, nil)
	}
	id := identity(c)
	if err := d.MFA.ConfirmEnroll(c.Request.Context(), id.UserID, req.Code, time.Now()); err != nil {
		return nil, mfaErr(err)
	}
	d.Audit.Write(c.Request.Context(), auditEntry{
		TenantID: id.TenantID, ActorID: id.UserID, ActorName: id.Username,
		Action: "mfa.enroll_confirm", ObjectType: "user", ObjectID: id.UserID, ClientIP: clientIP(c),
	})
	return gin.H{"enrolled": true}, nil
}

// ── 挑战 ─────────────────────────────────────────────────────────

// mfaChallenge 用一次验证码换一张短时提权票据。
//
// 这是网关那个 428 的另一半：网关说「还差一步」，人在这里补上那一步，
// 然后带着票据回去重试原来的请求。
func mfaChallenge(c *gin.Context, d Deps) (any, error) {
	var req struct {
		AppID     int64  `json:"app_id"`
		Scope     string `json:"scope"`
		TicketRef string `json:"ticket_ref"`
		Code      string `json:"code"`
		TTLSec    int    `json:"ttl_sec"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || req.AppID <= 0 {
		return nil, apierr.BadRequest(apierr.CodeInvalidParam, nil)
	}
	ttl := time.Duration(req.TTLSec) * time.Second
	if ttl <= 0 || ttl > time.Hour {
		// 上限 1 小时：再长就等于把二次验证摊薄成"每天验一次"，
		// 那还不如不做 —— 它的价值全在"临近操作时刚验过"
		ttl = 30 * time.Minute
	}
	id := identity(c)
	tk, err := d.MFA.IssueTicket(c.Request.Context(), id.UserID, req.AppID,
		req.Scope, req.TicketRef, req.Code, ttl, time.Now())
	if err != nil {
		return nil, mfaErr(err)
	}
	d.Audit.Write(c.Request.Context(), auditEntry{
		TenantID: id.TenantID, ActorID: id.UserID, ActorName: id.Username,
		Action: "mfa.step_up", ObjectType: "app", ObjectID: req.AppID, ClientIP: clientIP(c),
		Detail: map[string]any{"scope": req.Scope, "ticket_ref": req.TicketRef, "ttl_sec": int(ttl.Seconds())},
	})
	return gin.H{
		"ticket_id":  tk.ID,
		"expires_at": tk.ExpiresAt,
		"scope":      tk.Scope,
	}, nil
}

func mfaErr(err error) error {
	switch {
	case errors.Is(err, mfa.ErrNotEnrolled):
		// 单独的码：界面要引导去绑定，而不是让人对着验证码框发呆
		return apierr.New(http.StatusPreconditionRequired, apierr.CodeMFANotEnrolled, nil)
	case errors.Is(err, mfa.ErrBadCode):
		return apierr.New(http.StatusUnauthorized, apierr.CodeMFABadCode, nil)
	case errors.Is(err, mfa.ErrAlreadyDone):
		return apierr.New(http.StatusConflict, apierr.CodeMFAAlreadyEnrolled, nil)
	}
	return err
}
