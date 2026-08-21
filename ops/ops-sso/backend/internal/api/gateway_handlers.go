package api

import (
	"database/sql"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"ops-sso-backend/internal/apierr"
	"ops-sso-backend/internal/domain/access"
	"ops-sso-backend/internal/domain/pathpolicy"
)

// ══════════════════════════════════════════════════════════════════
// 路径级策略
// ══════════════════════════════════════════════════════════════════

func listPathRules(c *gin.Context, d Deps) (any, error) {
	appID, _ := strconv.ParseInt(c.Query("app_id"), 10, 64)
	if appID <= 0 {
		return nil, apierr.BadRequest(apierr.CodeInvalidParam, map[string]any{"field": "app_id"})
	}
	rules, err := d.Path.LoadForApp(c.Request.Context(), appID)
	if err != nil {
		return nil, err
	}
	ver, _ := d.Path.Version(c.Request.Context(), appID)

	// 按判定具体度排序返回：界面上看到的顺序 = 真实生效顺序。
	// 这张表**没有** order 字段，顺序不是语义的一部分（见 pathpolicy 包注释）。
	out := make([]gin.H, 0, len(rules))
	for _, r := range rules {
		out = append(out, pathRuleJSON(r))
	}
	return gin.H{"items": out, "total": len(out), "policy_version": ver}, nil
}

func pathRuleJSON(r pathpolicy.Rule) gin.H {
	return gin.H{
		"id": r.ID, "app_id": r.AppID, "methods": r.Methods, "path_pattern": r.PathPattern,
		"subject_type": r.SubjectType, "subject_id": r.SubjectID, "decision": r.Decision,
		"require_ticket": r.RequireTicket, "device_state": r.DeviceState,
		"time_window": r.TimeWindow, "source_kind": r.SourceKind, "mfa_ttl_sec": r.MFATTLSec,
	}
}

type pathRuleReq struct {
	AppID         int64  `json:"app_id"`
	Methods       string `json:"methods"`
	PathPattern   string `json:"path_pattern"`
	SubjectType   string `json:"subject_type"`
	SubjectID     int64  `json:"subject_id"`
	Decision      string `json:"decision"`
	RequireTicket bool   `json:"require_ticket"`
	DeviceState   string `json:"device_state"`
	TimeWindow    string `json:"time_window"`
	SourceKind    string `json:"source_kind"`
	MFATTLSec     int    `json:"mfa_ttl_sec"`
	Note          string `json:"note"`
}

func createPathRule(c *gin.Context, d Deps) (any, error) {
	var req pathRuleReq
	if err := c.ShouldBindJSON(&req); err != nil {
		return nil, apierr.BadRequest(apierr.CodeInvalidParam, nil)
	}
	if req.Methods == "" {
		req.Methods = "*"
	}
	if req.SubjectType == "" {
		req.SubjectType = "public"
	}
	rule := pathpolicy.Rule{
		AppID: req.AppID, Methods: req.Methods, PathPattern: req.PathPattern,
		SubjectType: pathpolicy.SubjectType(req.SubjectType), SubjectID: req.SubjectID,
		Decision: pathpolicy.Decision(req.Decision), RequireTicket: req.RequireTicket,
		DeviceState: req.DeviceState, TimeWindow: req.TimeWindow, SourceKind: req.SourceKind,
		MFATTLSec: req.MFATTLSec,
	}
	id, err := d.Path.Create(c.Request.Context(), rule, req.Note, actorID(c))
	if err != nil {
		switch {
		case errors.Is(err, pathpolicy.ErrDuplicate):
			return nil, apierr.New(http.StatusConflict, apierr.CodeDuplicateRule, nil)
		case errors.Is(err, pathpolicy.ErrInvalidRule):
			return nil, apierr.BadRequest(apierr.CodeInvalidRule, map[string]any{"reason": err.Error()})
		}
		return nil, err
	}
	d.Audit.Write(c.Request.Context(), auditEntry{
		TenantID: identity(c).TenantID, ActorID: actorID(c), ActorName: identity(c).Username,
		Action: "path_rule.create", ObjectType: "path_rule", ObjectID: id, ClientIP: clientIP(c),
		Detail: map[string]any{"app_id": req.AppID, "path": req.PathPattern, "decision": req.Decision},
	})
	return gin.H{"id": id}, nil
}

func deletePathRule(c *gin.Context, d Deps) (any, error) {
	id, err := pathID(c)
	if err != nil {
		return nil, err
	}
	if err := d.Path.Delete(c.Request.Context(), id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, apierr.CrossTenant()
		}
		return nil, err
	}
	d.Audit.Write(c.Request.Context(), auditEntry{
		TenantID: identity(c).TenantID, ActorID: actorID(c), ActorName: identity(c).Username,
		Action: "path_rule.delete", ObjectType: "path_rule", ObjectID: id, ClientIP: clientIP(c),
	})
	return nil, nil
}

// simulatePath 接口级试算：他调这个接口会怎样，以及每条候选规则的具体度打分。
//
// 这是「被拒时自己查」的那个入口 —— 开发不用去问运维，看一眼就知道
// 是哪条规则、因为哪个条件、要满足什么才能过。
func simulatePath(c *gin.Context, d Deps) (any, error) {
	var req struct {
		UserID      int64  `json:"user_id"`
		AppID       int64  `json:"app_id"`
		Method      string `json:"method"`
		Path        string `json:"path"`
		DeviceState string `json:"device_state"`
		SourceKind  string `json:"source_kind"`
		HasTicket   bool   `json:"has_ticket"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || req.AppID <= 0 || req.Path == "" {
		return nil, apierr.BadRequest(apierr.CodeInvalidParam, nil)
	}
	ctx := c.Request.Context()

	sub, err := d.Access.LoadSubject(ctx, req.UserID)
	if err != nil {
		return nil, err
	}
	rules, err := d.Path.LoadForApp(ctx, req.AppID)
	if err != nil {
		return nil, err
	}
	ver, _ := d.Path.Version(ctx, req.AppID)

	// 先过应用级授权：连应用都进不去，就没必要谈接口。
	// 两级判定的顺序在网关里也是这个 —— 这里保持一致，否则试算结果会骗人。
	app, err := d.AppCat.GetApp(ctx, req.AppID)
	if err != nil {
		return nil, apierr.CrossTenant()
	}
	appRules, err := d.Access.LoadRulesForApp(ctx, req.AppID)
	if err != nil {
		return nil, err
	}
	appDec := access.Evaluate(sub, access.App{ID: app.ID, GroupIDs: app.GroupIDs}, appRules)
	if !appDec.Allowed() {
		return gin.H{
			"stage": "app", "decision": "deny", "reason": appDec.Reason,
			"app_decision": decisionJSON(appDec, nil), "policy_version": ver,
		}, nil
	}

	res := pathpolicy.Evaluate(pathpolicy.Request{
		AppID: req.AppID, Method: req.Method, Path: req.Path,
		DeviceState: req.DeviceState, SourceKind: req.SourceKind, HasTicket: req.HasTicket,
		At:     time.Now(),
		UserID: sub.UserID, RoleIDs: sub.RoleIDs, GroupIDs: sub.GroupIDs, DeptDepth: sub.DeptDepth,
	}, rules)

	cands := make([]gin.H, 0, len(res.Candidates))
	for _, s := range res.Candidates {
		cands = append(cands, gin.H{
			"rule": pathRuleJSON(s.Rule), "path_rank": s.PathRank,
			"subject_rank": s.SubjRank, "cond_rank": s.CondRank, "won": s.Won,
		})
	}
	out := gin.H{
		"stage": "path", "decision": res.Decision, "reason": res.Reason,
		"candidates": cands, "policy_version": ver, "app_decision": decisionJSON(appDec, nil),
	}
	if res.Rule != nil {
		out["decided_by"] = pathRuleJSON(*res.Rule)
		out["mfa_ttl_sec"] = res.MFATTL
	}
	return out, nil
}

// ══════════════════════════════════════════════════════════════════
// 拨测探针（P0-2）
// ══════════════════════════════════════════════════════════════════

func listProbes(c *gin.Context, d Deps) (any, error) {
	q, err := d.Store.Tenant(c.Request.Context())
	if err != nil {
		return nil, err
	}
	rows, err := q.Query(`SELECT p.id, p.app_id, a.name, p.username, p.allowed_cidr, p.interval_sec,
		       p.quiet_hours, p.last_probe_at, p.last_result, p.last_detail, p.revoked_at
		FROM probe_credentials p JOIN apps a ON a.id = p.app_id AND a.tenant_id = p.tenant_id
		WHERE p.tenant_id = ?`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []gin.H{}
	for rows.Next() {
		var (
			id, appID                            int64
			appName, user, cidr, quiet, res, det string
			interval                             int
			last                                 sql.NullTime
			revoked                              sql.NullTime
		)
		if err := rows.Scan(&id, &appID, &appName, &user, &cidr, &interval, &quiet,
			&last, &res, &det, &revoked); err != nil {
			return nil, err
		}
		// ⚠️ 绝不返回 secret_enc —— 生产上出过的两个 P0 都是「接口把凭据发给了不该看的人」
		item := gin.H{
			"id": id, "app_id": appID, "app_name": appName, "username": user,
			"allowed_cidr": cidr, "interval_sec": interval, "quiet_hours": quiet,
			"read_only": true, "revoked": revoked.Valid,
		}
		if last.Valid {
			item["last_probe_at"] = last.Time
		}
		// 从没测过时 last_result 是空串 —— 界面显示「—」而不是「健康」。
		// 「还没测」和「测过没问题」必须长得不一样。
		item["last_result"] = res
		item["last_detail"] = det
		out = append(out, item)
	}
	return gin.H{"items": out, "total": len(out)}, nil
}

func upsertProbe(c *gin.Context, d Deps) (any, error) {
	var req struct {
		AppID       int64  `json:"app_id"`
		Username    string `json:"username"`
		Secret      string `json:"secret"`
		AllowedCIDR string `json:"allowed_cidr"`
		IntervalSec int    `json:"interval_sec"`
		QuietHours  string `json:"quiet_hours"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || req.AppID <= 0 || req.Username == "" {
		return nil, apierr.BadRequest(apierr.CodeInvalidParam, nil)
	}
	// 来源网段必填：探针账号是长期有效的凭据，不限来源等于给了一把万能钥匙。
	if req.AllowedCIDR == "" {
		return nil, apierr.BadRequest(apierr.CodeProbeNeedsCIDR, nil)
	}
	if req.IntervalSec < 60 {
		// 下限 60 秒：5 分钟一次已经是每天 288 次，再密老系统扛不住 ——
		// 拨测把被测系统压垮，是这个功能最讽刺的失败模式
		req.IntervalSec = 300
	}
	q, err := d.Store.Tenant(c.Request.Context())
	if err != nil {
		return nil, err
	}
	enc, err := d.Secrets.Seal([]byte(req.Secret))
	if err != nil {
		return nil, err
	}
	_, err = q.Insert(`INSERT INTO probe_credentials
		(tenant_id, app_id, username, secret_enc, allowed_cidr, interval_sec, quiet_hours)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON DUPLICATE KEY UPDATE username=VALUES(username), secret_enc=VALUES(secret_enc),
		  allowed_cidr=VALUES(allowed_cidr), interval_sec=VALUES(interval_sec),
		  quiet_hours=VALUES(quiet_hours), revoked_at=NULL`,
		req.AppID, req.Username, enc, req.AllowedCIDR, req.IntervalSec, req.QuietHours)
	if err != nil {
		return nil, err
	}
	d.Audit.Write(c.Request.Context(), auditEntry{
		TenantID: identity(c).TenantID, ActorID: actorID(c), ActorName: identity(c).Username,
		Action: "probe.upsert", ObjectType: "app", ObjectID: req.AppID, ClientIP: clientIP(c),
		// 只记来源网段与频率，不记凭据
		Detail: map[string]any{"cidr": req.AllowedCIDR, "interval": req.IntervalSec},
	})
	return gin.H{"ok": true}, nil
}

func revokeProbe(c *gin.Context, d Deps) (any, error) {
	id, err := pathID(c)
	if err != nil {
		return nil, err
	}
	q, err := d.Store.Tenant(c.Request.Context())
	if err != nil {
		return nil, err
	}
	// 吊销后 last_result 一并清空：探针停了，链路健康必须退回「未覆盖 —」，
	// 而不是停在最后一次成功的「健康」上 —— 那会让人以为它还在测
	if _, err := q.Exec(`UPDATE probe_credentials SET revoked_at = NOW(), last_result = ''
		WHERE tenant_id = ? AND id = ?`, id); err != nil {
		return nil, err
	}
	d.Audit.Write(c.Request.Context(), auditEntry{
		TenantID: identity(c).TenantID, ActorID: actorID(c), ActorName: identity(c).Username,
		Action: "probe.revoke", ObjectType: "probe", ObjectID: id, ClientIP: clientIP(c),
	})
	return nil, nil
}

// ══════════════════════════════════════════════════════════════════
// 审计查询
// ══════════════════════════════════════════════════════════════════

func listAccessEvents(c *gin.Context, d Deps) (any, error) {
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "50"))
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	offset, _ := strconv.Atoi(c.DefaultQuery("offset", "0"))
	decision := c.Query("decision")
	userFilter, _ := strconv.ParseInt(c.DefaultQuery("user_id", "0"), 10, 64)

	q, err := d.Store.Tenant(c.Request.Context())
	if err != nil {
		return nil, err
	}
	// 后端分页 + 后端过滤：18 万条不可能拉回前端再筛
	rows, err := q.Query(`SELECT id, request_id, occurred_at, user_id, user_label, app_id, app_code,
		       method, path, decision, reason, matched_rule, policy_version, client_ip, device_state, gateway
		FROM access_events
		WHERE tenant_id = ?
		  AND (? = '' OR decision = ?)
		  AND (? = 0 OR user_id = ?)
		ORDER BY occurred_at DESC LIMIT ? OFFSET ?`,
		decision, decision, userFilter, userFilter, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []gin.H{}
	for rows.Next() {
		var (
			id, uid, appID, rule             int64
			reqID, label, code, method, path string
			dec, reason, ver, ip, device, gw string
			at                               time.Time
		)
		if err := rows.Scan(&id, &reqID, &at, &uid, &label, &appID, &code, &method, &path,
			&dec, &reason, &rule, &ver, &ip, &device, &gw); err != nil {
			return nil, err
		}
		out = append(out, gin.H{
			"id": id, "request_id": reqID, "occurred_at": at, "user_id": uid, "user_label": label,
			"app_id": appID, "app_code": code, "method": method, "path": path,
			"decision": dec, "reason": reason, "matched_rule": rule,
			"policy_version": ver, "client_ip": ip, "device_state": device, "gateway": gw,
		})
	}
	return gin.H{"items": out, "limit": limit, "offset": offset}, nil
}

// verifyAuditChain 校验审计哈希链。
//
// 界面上那个「校验完整性」按钮就是它。结论要说清**断在哪一条**，
// 只说"校验失败"除了让人心慌之外没有任何用处。
func verifyAuditChain(c *gin.Context, d Deps) (any, error) {
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "1000"))
	res, err := d.Audit.VerifyChain(c.Request.Context(), identity(c).TenantID, limit)
	if err != nil {
		return nil, err
	}
	out := gin.H{"ok": res.OK, "checked": res.Checked}
	if !res.OK {
		out["broken_at_seq"] = res.BrokenAt
		out["reason"] = res.Reason // 原因码，文案在前端语言包
	}
	return out, nil
}

func listAuditLogs(c *gin.Context, d Deps) (any, error) {
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "50"))
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	q, err := d.Store.Tenant(c.Request.Context())
	if err != nil {
		return nil, err
	}
	rows, err := q.Query(`SELECT id, actor_id, actor_name, action, object_type, object_id,
		       detail, client_ip, created_at
		FROM audit_logs WHERE tenant_id = ? ORDER BY id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []gin.H{}
	for rows.Next() {
		var (
			id, actorID, objID         int64
			actor, action, objType, ip string
			detail                     sql.NullString
			at                         time.Time
		)
		if err := rows.Scan(&id, &actorID, &actor, &action, &objType, &objID, &detail, &ip, &at); err != nil {
			return nil, err
		}
		out = append(out, gin.H{
			"id": id, "actor_id": actorID, "actor_name": actor, "action": action,
			"object_type": objType, "object_id": objID, "detail": detail.String,
			"client_ip": ip, "created_at": at,
		})
	}
	return gin.H{"items": out, "limit": limit}, nil
}
