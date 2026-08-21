package api

import (
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"ops-sso-backend/internal/apierr"
	"ops-sso-backend/internal/domain/access"
	"ops-sso-backend/internal/domain/approval"
	"ops-sso-backend/logx"
)

// listRequests 申请列表。默认看待审批的。
func listRequests(c *gin.Context, d Deps) (any, error) {
	status := c.DefaultQuery("status", "")

	// ⚠️ 归属过滤在**服务端**强制，不看前端传了什么。
	//
	// 非管理员一律只能看自己的：`mine=1` 是个便利参数，不是权限判据 ——
	// 把它当判据的话，去掉这个参数就能看到全公司的申请，
	// 而申请里带着"谁在什么时候想进哪个系统、为什么"，
	// 那是一份相当完整的行为画像。
	mine := actorID(c)
	if identity(c).IsAdmin() && c.Query("mine") != "1" {
		mine = 0 // 只有管理员、且没有显式要"我的"时，才看全量
	}
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "50"))

	reqs, err := d.Approval.List(c.Request.Context(), status, mine, limit)
	if err != nil {
		return nil, err
	}
	out := make([]gin.H, 0, len(reqs))
	for _, r := range reqs {
		out = append(out, requestJSON(r))
	}
	// 名字批量补上。**不补名字这一页就没法用**：
	// 审批人看到的是「#3 申请 #2」，他既不知道谁在申请，也不知道申请的是什么，
	// 只能去库里查两次才敢点批准 —— 那这一页等于没有。
	fillRequestNames(c, d, out)
	return gin.H{"items": out, "total": len(out)}, nil
}

// fillRequestNames 把申请里的 requester_id / app_id 换成人能读的名字。
//
// 一次两条 IN 查询，不做 N+1。查不到的**明确标出来**（"#7（已删除？）"），
// 而不是留空：申请人被删了而申请还在，是真实会发生的事，
// 留空会让人以为是界面坏了。
func fillRequestNames(c *gin.Context, d Deps, items []gin.H) {
	if len(items) == 0 {
		return
	}
	var userIDs, appIDs []int64
	for _, it := range items {
		if v, ok := it["requester_id"].(int64); ok && v > 0 {
			userIDs = append(userIDs, v)
		}
		if v, ok := it["app_id"].(int64); ok && v > 0 {
			appIDs = append(appIDs, v)
		}
		if v, ok := it["approver_id"].(int64); ok && v > 0 {
			userIDs = append(userIDs, v)
		}
	}

	q, err := d.Store.Tenant(c.Request.Context())
	if err != nil {
		logx.Line("approval", "取租户上下文失败，申请列表将只显示 ID: "+err.Error())
		return
	}
	users := map[int64]string{}
	fillUsers(q, userIDs, users)
	apps := map[int64]string{}
	fill(q, `SELECT id, name FROM apps WHERE tenant_id = ? AND id IN`, appIDs, apps)

	name := func(m map[int64]string, id int64) string {
		if id <= 0 {
			return ""
		}
		if n, ok := m[id]; ok && n != "" {
			return n
		}
		return fmt.Sprintf("#%d", id)
	}
	for _, it := range items {
		if v, ok := it["requester_id"].(int64); ok {
			it["requester_name"] = name(users, v)
		}
		if v, ok := it["app_id"].(int64); ok {
			it["app_name"] = name(apps, v)
		}
		if v, ok := it["approver_id"].(int64); ok && v > 0 {
			it["approver_name"] = name(users, v)
		}
	}
}

func requestJSON(r approval.Request) gin.H {
	h := gin.H{
		"id": r.ID, "requester_id": r.RequesterID, "app_id": r.AppID,
		"scope": r.Scope, "reason": r.Reason, "ticket_ref": r.TicketRef,
		"duration_sec": int64(r.Duration / time.Second),
		"status":       r.Status, "risk": r.Risk,
		"approver_id": r.ApproverID, "approver_note": r.ApproverNote,
		"created_at":     r.CreatedAt,
		"needs_two_step": r.NeedsTwoStep(),
	}
	// 被 SoD 拦下时必须带上"为什么"与"怎么才能行"——
	// 只回一个 blocked，申请人只知道不行，不知道下一步做什么
	if r.Status == approval.StatusBlocked {
		h["blocked_rule"] = r.BlockedRule
		h["blocked_note"] = r.BlockedNote
	}
	if r.ExpiresAt != nil {
		h["expires_at"] = r.ExpiresAt
		h["active"] = r.Active(time.Now())
	}
	if r.DecidedAt != nil {
		h["decided_at"] = r.DecidedAt
	}
	return h
}

// createRequest 发起申请。
//
// SoD 在**提交时**就检查，而不是等审批人点下去才发现 ——
// 让人填完表单等半天再被告知"制度上不允许"，是最糟的交互。
func createRequest(c *gin.Context, d Deps) (any, error) {
	var req struct {
		AppID       int64  `json:"app_id"`
		Scope       string `json:"scope"`
		Reason      string `json:"reason"`
		TicketRef   string `json:"ticket_ref"`
		DurationSec int64  `json:"duration_sec"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		return nil, apierr.BadRequest(apierr.CodeInvalidParam, nil)
	}
	ctx := c.Request.Context()
	id := identity(c)

	app, err := d.AppCat.GetApp(ctx, req.AppID)
	if err != nil {
		return nil, apierr.CrossTenant()
	}

	r := approval.Request{
		TenantID: id.TenantID, RequesterID: id.UserID, AppID: req.AppID,
		Scope: req.Scope, Reason: req.Reason, TicketRef: req.TicketRef,
		Duration: time.Duration(req.DurationSec) * time.Second,
		Status:   approval.StatusPending,
	}
	r.Risk = approval.RiskOf(app.Env, r.Scope, r.Duration)
	if err := r.Validate(); err != nil {
		return nil, apierr.BadRequest(apierr.CodeInvalidRequest, map[string]any{"reason": err.Error()})
	}

	// ── 职责分离 ──
	rules, err := d.Approval.SoDRules(ctx)
	if err != nil {
		return nil, err
	}
	wantCaps, err := d.Approval.AppCapabilities(ctx, req.AppID)
	if err != nil {
		return nil, err
	}
	held, err := d.Approval.HeldCapabilities(ctx, id.UserID, time.Now())
	if err != nil {
		return nil, err
	}
	if conflict := approval.CheckSoD(rules, held, wantCaps); conflict.Blocked {
		r.Block(conflict, time.Now())
		newID, err := d.Approval.Create(ctx, r)
		if err != nil {
			return nil, err
		}
		r.ID = newID
		d.Audit.Write(ctx, auditEntry{
			TenantID: id.TenantID, ActorID: id.UserID, ActorName: id.Username,
			Action: "approval.blocked", ObjectType: "access_request", ObjectID: newID,
			ClientIP: clientIP(c),
			Detail:   map[string]any{"rule": conflict.Rule.Code, "held_via": conflict.HeldVia},
		})
		// 200 而不是 4xx：申请**确实被受理了**，只是结论是 blocked。
		// 返回错误码的话前端会当成"提交失败"，人就会一直重试。
		return requestJSON(r), nil
	}

	newID, err := d.Approval.Create(ctx, r)
	if err != nil {
		return nil, err
	}
	r.ID = newID
	d.Audit.Write(ctx, auditEntry{
		TenantID: id.TenantID, ActorID: id.UserID, ActorName: id.Username,
		Action: "approval.create", ObjectType: "access_request", ObjectID: newID,
		ClientIP: clientIP(c),
		Detail: map[string]any{"app_id": req.AppID, "scope": req.Scope,
			"duration_sec": req.DurationSec, "ticket": req.TicketRef},
	})
	return requestJSON(r), nil
}

// decideRequest 审批。
func decideRequest(c *gin.Context, d Deps) (any, error) {
	reqID, err := pathID(c)
	if err != nil {
		return nil, err
	}
	var body struct {
		Approve bool   `json:"approve"`
		Note    string `json:"note"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		return nil, apierr.BadRequest(apierr.CodeInvalidParam, nil)
	}
	ctx := c.Request.Context()
	id := identity(c)

	r, err := d.Approval.Get(ctx, reqID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, apierr.CrossTenant()
		}
		return nil, err
	}
	if err := r.Decide(id.UserID, body.Approve, body.Note, time.Now()); err != nil {
		switch {
		case errors.Is(err, approval.ErrSelfApproval):
			return nil, apierr.New(http.StatusForbidden, apierr.CodeSelfApproval, nil)
		case errors.Is(err, approval.ErrNotPending):
			return nil, apierr.New(http.StatusConflict, apierr.CodeAlreadyDecided, nil)
		}
		return nil, err
	}
	if err := d.Approval.Decide(ctx, r); err != nil {
		if errors.Is(err, approval.ErrNotPending) {
			// 并发：另一个审批人抢先了。给 409，让界面刷新看结论，
			// 而不是静默覆盖别人的意见
			return nil, apierr.New(http.StatusConflict, apierr.CodeAlreadyDecided, nil)
		}
		return nil, err
	}

	action := "approval.reject"
	if body.Approve {
		action = "approval.approve"
	}
	d.Audit.Write(ctx, auditEntry{
		TenantID: id.TenantID, ActorID: id.UserID, ActorName: id.Username,
		Action: action, ObjectType: "access_request", ObjectID: reqID, ClientIP: clientIP(c),
		Detail: map[string]any{"requester_id": r.RequesterID, "note": body.Note},
	})
	return requestJSON(r), nil
}

// revokeRequest 提前交还或强制收回。
func revokeRequest(c *gin.Context, d Deps) (any, error) {
	reqID, err := pathID(c)
	if err != nil {
		return nil, err
	}
	ctx := c.Request.Context()
	id := identity(c)

	r, err := d.Approval.Get(ctx, reqID)
	if err != nil {
		return nil, apierr.CrossTenant()
	}
	if err := d.Approval.Revoke(ctx, reqID, "manual"); err != nil {
		return nil, apierr.New(http.StatusConflict, apierr.CodeAlreadyDecided, nil)
	}
	// ★ 回收必须连带杀掉提权票据。
	// 只改数据库标记的话，他手里那张 30 分钟的票还能继续用 ——
	// "收回权限"就成了一句空话。
	if d.MFA != nil {
		if n, _ := d.MFA.RevokeUserTickets(ctx, r.RequesterID); n > 0 {
			_ = n
		}
	}
	d.Audit.Write(ctx, auditEntry{
		TenantID: id.TenantID, ActorID: id.UserID, ActorName: id.Username,
		Action: "approval.revoke", ObjectType: "access_request", ObjectID: reqID,
		ClientIP: clientIP(c), Detail: map[string]any{"requester_id": r.RequesterID},
	})
	return nil, nil
}

// myGrants 我当前生效中的临时权限（门户用）。
func myGrants(c *gin.Context, d Deps) (any, error) {
	reqs, err := d.Approval.ActiveFor(c.Request.Context(), actorID(c), time.Now())
	if err != nil {
		return nil, err
	}
	out := make([]gin.H, 0, len(reqs))
	now := time.Now()
	for _, r := range reqs {
		h := requestJSON(r)
		// 剩余秒数直接给前端，免得它自己算时区/时钟偏差
		if r.ExpiresAt != nil {
			h["remaining_sec"] = int64(r.ExpiresAt.Sub(now).Seconds())
		}
		out = append(out, h)
	}
	return gin.H{"items": out, "total": len(out)}, nil
}

// approvalRules 把生效中的临时提权转成判定规则。
//
// 网关、门户、OIDC 授权端点三处都要用同一套 —— 只在一处叠加的话，
// "我明明申请过"会在另外两处变成投诉。
func approvalRules(reqs []approval.Request) []access.Rule {
	return approval.ActiveRules(reqs, time.Now())
}
