package approval

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"ops-sso-backend/internal/domain/access"
	"ops-sso-backend/internal/store"
)

type Repo struct{ st *store.Store }

func NewRepo(st *store.Store) *Repo { return &Repo{st: st} }

const reqCols = `id, requester_id, app_id, scope, reason, ticket_ref, duration_sec,
	status, risk, blocked_rule, blocked_note, approver_id, approver_note,
	decided_at, granted_at, expires_at, revoked_at, created_at`

func scanRequests(rows *sql.Rows) ([]Request, error) {
	var out []Request
	for rows.Next() {
		var r Request
		var durSec int64
		var decided, granted, expires, revoked sql.NullTime
		if err := rows.Scan(&r.ID, &r.RequesterID, &r.AppID, &r.Scope, &r.Reason, &r.TicketRef,
			&durSec, &r.Status, &r.Risk, &r.BlockedRule, &r.BlockedNote,
			&r.ApproverID, &r.ApproverNote, &decided, &granted, &expires, &revoked,
			&r.CreatedAt); err != nil {
			return nil, err
		}
		r.Duration = time.Duration(durSec) * time.Second
		if decided.Valid {
			t := decided.Time
			r.DecidedAt = &t
		}
		if granted.Valid {
			t := granted.Time
			r.GrantedAt = &t
		}
		if expires.Valid {
			t := expires.Time
			r.ExpiresAt = &t
		}
		if revoked.Valid {
			t := revoked.Time
			r.RevokedAt = &t
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// Create 提交一次申请。SoD 已在服务层检查过，这里只落库。
func (r *Repo) Create(ctx context.Context, req Request) (int64, error) {
	if err := req.Validate(); err != nil {
		return 0, err
	}
	q, err := r.st.Tenant(ctx)
	if err != nil {
		return 0, err
	}
	res, err := q.Insert(`INSERT INTO access_requests
		(tenant_id, requester_id, app_id, scope, reason, ticket_ref, duration_sec,
		 status, risk, blocked_rule, blocked_note, decided_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		req.RequesterID, req.AppID, req.Scope, req.Reason, req.TicketRef,
		int64(req.Duration/time.Second), string(req.Status), string(req.Risk),
		req.BlockedRule, req.BlockedNote, req.DecidedAt)
	if err != nil {
		return 0, fmt.Errorf("approval: 提交申请失败: %w", err)
	}
	return res.LastInsertId()
}

// Get 取一条申请。
func (r *Repo) Get(ctx context.Context, id int64) (Request, error) {
	q, err := r.st.Tenant(ctx)
	if err != nil {
		return Request{}, err
	}
	rows, err := q.Query(`SELECT `+reqCols+` FROM access_requests
		WHERE tenant_id = ? AND id = ?`, id)
	if err != nil {
		return Request{}, err
	}
	defer rows.Close()
	list, err := scanRequests(rows)
	if err != nil {
		return Request{}, err
	}
	if len(list) == 0 {
		return Request{}, sql.ErrNoRows
	}
	return list[0], nil
}

// List 按状态列申请。
func (r *Repo) List(ctx context.Context, status string, requesterID int64, limit int) ([]Request, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	q, err := r.st.Tenant(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := q.Query(`SELECT `+reqCols+` FROM access_requests
		WHERE tenant_id = ?
		  AND (? = '' OR status = ?)
		  AND (? = 0 OR requester_id = ?)
		ORDER BY id DESC LIMIT ?`,
		status, status, requesterID, requesterID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanRequests(rows)
}

// Decide 落库审批结果。
//
// WHERE 带 `status = 'pending'`：两个审批人同时点，只有一个能成 ——
// 没有这个条件的话，第二个人的意见会静默覆盖第一个人的。
func (r *Repo) Decide(ctx context.Context, req Request) error {
	q, err := r.st.Tenant(ctx)
	if err != nil {
		return err
	}
	res, err := q.Exec(`UPDATE access_requests
		SET status = ?, approver_id = ?, approver_note = ?, decided_at = ?,
		    granted_at = ?, expires_at = ?
		WHERE tenant_id = ? AND id = ? AND status = 'pending'`,
		string(req.Status), req.ApproverID, req.ApproverNote, req.DecidedAt,
		req.GrantedAt, req.ExpiresAt, req.ID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotPending
	}
	return nil
}

// ActiveFor 取某人当前生效中的提权。
func (r *Repo) ActiveFor(ctx context.Context, userID int64, now time.Time) ([]Request, error) {
	q, err := r.st.Tenant(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := q.Query(`SELECT `+reqCols+` FROM access_requests
		WHERE tenant_id = ? AND requester_id = ? AND status = 'approved'
		  AND revoked_at IS NULL AND expires_at > ?
		ORDER BY expires_at`, userID, now)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanRequests(rows)
}

// ActiveRules 把生效中的提权转成 access.Rule，叠加到判定里。
//
// **这是两套语义不分叉的关键**：临时提权不走单独的判定分支，
// 而是变成一条"应用级 · 个人 · 放行"规则参与同一次求值。
// 于是「主体越具体越优先」等所有语义自动适用，不需要再想一遍。
//
// ID 用负数：与真实规则的 ID 空间隔开，审计里一眼能看出这是临时提权。
func ActiveRules(reqs []Request, now time.Time) []access.Rule {
	var out []access.Rule
	for _, r := range reqs {
		if !r.Active(now) {
			continue
		}
		out = append(out, access.Rule{
			ID:          -r.ID,
			Scope:       access.ScopeApp,
			ScopeID:     r.AppID,
			SubjectType: access.SubjectUser,
			SubjectID:   r.RequesterID,
			Effect:      access.Allow,
		})
	}
	return out
}

// ── 到期回收 ────────────────────────────────────────────────────

// Expired 找出该回收的提权。
func (r *Repo) Expired(ctx context.Context, now time.Time, limit int) ([]Request, error) {
	if limit <= 0 {
		limit = 200
	}
	q, err := r.st.Tenant(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := q.Query(`SELECT `+reqCols+` FROM access_requests
		WHERE tenant_id = ? AND status = 'approved' AND revoked_at IS NULL AND expires_at <= ?
		ORDER BY expires_at LIMIT ?`, now, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanRequests(rows)
}

// MarkExpired 标记已回收。
func (r *Repo) MarkExpired(ctx context.Context, id int64, reason string) error {
	q, err := r.st.Tenant(ctx)
	if err != nil {
		return err
	}
	_, err = q.Exec(`UPDATE access_requests SET status = 'expired', revoked_at = NOW(), revoke_reason = ?
		WHERE tenant_id = ? AND id = ? AND status = 'approved'`, reason, id)
	return err
}

// Revoke 提前收回（申请人交还，或管理员强制收）。
func (r *Repo) Revoke(ctx context.Context, id int64, reason string) error {
	q, err := r.st.Tenant(ctx)
	if err != nil {
		return err
	}
	res, err := q.Exec(`UPDATE access_requests SET status = 'revoked', revoked_at = NOW(), revoke_reason = ?
		WHERE tenant_id = ? AND id = ? AND status = 'approved' AND revoked_at IS NULL`, reason, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return errors.New("approval: 该提权已不在生效中")
	}
	return nil
}

// ── 能力标签与 SoD ──────────────────────────────────────────────

// SoDRules 取启用中的职责分离规则。
func (r *Repo) SoDRules(ctx context.Context) ([]SoDRule, error) {
	q, err := r.st.Tenant(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := q.Query(`SELECT id, code, name, capability_a, capability_b, note, enabled
		FROM sod_rules WHERE tenant_id = ?`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SoDRule
	for rows.Next() {
		var s SoDRule
		var en int
		if err := rows.Scan(&s.ID, &s.Code, &s.Name, &s.CapabilityA, &s.CapabilityB, &s.Note, &en); err != nil {
			return nil, err
		}
		s.Enabled = en == 1
		out = append(out, s)
	}
	return out, rows.Err()
}

// AppCapabilities 某个应用具备哪些能力标签。
func (r *Repo) AppCapabilities(ctx context.Context, appID int64) ([]string, error) {
	q, err := r.st.Tenant(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := q.Query(`SELECT capability FROM app_capabilities
		WHERE tenant_id = ? AND app_id = ?`, appID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var c string
		if err := rows.Scan(&c); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// HeldCapabilities 某人当前已持有的能力 → 通过什么持有的。
//
// 两个来源都要算：常设授权命中的应用，以及生效中的临时提权。
// 只算前者的话，"先申请提权拿到发布权、再申请审批权"就能绕过 SoD。
func (r *Repo) HeldCapabilities(ctx context.Context, userID int64, now time.Time) (map[string]string, error) {
	q, err := r.st.Tenant(ctx)
	if err != nil {
		return nil, err
	}
	held := map[string]string{}

	// 常设授权：按用户/组/角色直接命中且 effect=allow 的应用
	rows, err := q.Query(`SELECT DISTINCT ac.capability, a.name
		FROM access_policies p
		JOIN app_capabilities ac ON ac.tenant_id = p.tenant_id
		JOIN apps a ON a.id = ac.app_id AND a.tenant_id = ac.tenant_id
		WHERE p.tenant_id = ? AND p.deleted_at IS NULL AND p.effect = 'allow'
		  AND ((p.scope = 'app' AND p.scope_id = ac.app_id)
		    OR (p.scope = 'group' AND p.scope_id IN (
		          SELECT m.group_id FROM app_group_members m
		          WHERE m.tenant_id = p.tenant_id AND m.app_id = ac.app_id)))
		  AND ((p.subject_type = 'user' AND p.subject_id = ?)
		    OR (p.subject_type = 'group' AND p.subject_id IN (
		          SELECT g.group_id FROM user_group_members g
		          WHERE g.tenant_id = p.tenant_id AND g.user_id = ?)))`,
		userID, userID)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var cap, appName string
		if err := rows.Scan(&cap, &appName); err != nil {
			rows.Close()
			return nil, err
		}
		held[cap] = "常设授权 · " + appName
	}
	rows.Close()

	// 生效中的临时提权
	active, err := r.ActiveFor(ctx, userID, now)
	if err != nil {
		return nil, err
	}
	for _, req := range active {
		caps, err := r.AppCapabilities(ctx, req.AppID)
		if err != nil {
			return nil, err
		}
		for _, c := range caps {
			if _, exists := held[c]; !exists {
				held[c] = fmt.Sprintf("临时提权 #%d（%s）", req.ID, req.TicketRef)
			}
		}
	}
	return held, nil
}
