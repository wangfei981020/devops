package pathpolicy

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/go-sql-driver/mysql"

	"ops-sso-backend/internal/store"
)

// ErrDuplicate 同一条件的规则已存在。
var ErrDuplicate = errors.New("pathpolicy: 该规则已存在")

type Repo struct{ st *store.Store }

func NewRepo(st *store.Store) *Repo { return &Repo{st: st} }

const ruleCols = `id, app_id, methods, path_pattern, subject_type, subject_id, decision,
	require_ticket, device_state, time_window, source_kind, mfa_ttl_sec`

// LoadForApp 取某应用的全部路径规则。网关每次判定都会调它（外层带缓存）。
func (r *Repo) LoadForApp(ctx context.Context, appID int64) ([]Rule, error) {
	q, err := r.st.Tenant(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := q.Query(`SELECT `+ruleCols+` FROM path_rules
		WHERE tenant_id = ? AND app_id = ? AND deleted_at IS NULL`, appID)
	if err != nil {
		return nil, fmt.Errorf("load path rules: %w", err)
	}
	defer rows.Close()
	return scan(rows)
}

func scan(rows *sql.Rows) ([]Rule, error) {
	var out []Rule
	for rows.Next() {
		var r Rule
		var ticket int
		if err := rows.Scan(&r.ID, &r.AppID, &r.Methods, &r.PathPattern, &r.SubjectType,
			&r.SubjectID, &r.Decision, &ticket, &r.DeviceState, &r.TimeWindow,
			&r.SourceKind, &r.MFATTLSec); err != nil {
			return nil, err
		}
		r.RequireTicket = ticket == 1
		out = append(out, r)
	}
	return out, rows.Err()
}

// Create 新增一条路径规则。
func (r *Repo) Create(ctx context.Context, rule Rule, note string, actorID int64) (int64, error) {
	if err := rule.Validate(); err != nil {
		return 0, err
	}
	q, err := r.st.Tenant(ctx)
	if err != nil {
		return 0, err
	}
	ticket := 0
	if rule.RequireTicket {
		ticket = 1
	}
	if rule.MFATTLSec <= 0 {
		rule.MFATTLSec = 1800
	}
	res, err := q.Insert(`INSERT INTO path_rules
		(tenant_id, app_id, methods, path_pattern, subject_type, subject_id, decision,
		 require_ticket, device_state, time_window, source_kind, mfa_ttl_sec, note, created_by)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		rule.AppID, rule.Methods, rule.PathPattern, string(rule.SubjectType), rule.SubjectID,
		string(rule.Decision), ticket, rule.DeviceState, rule.TimeWindow, rule.SourceKind,
		rule.MFATTLSec, note, actorID)
	if err != nil {
		var me *mysql.MySQLError
		if errors.As(err, &me) && me.Number == 1062 {
			return 0, ErrDuplicate
		}
		return 0, fmt.Errorf("create path rule: %w", err)
	}
	return res.LastInsertId()
}

// Delete 软删。del_key 同时置为 id，让唯一索引只约束活行（见迁移 002 的同款处理）。
func (r *Repo) Delete(ctx context.Context, id int64) error {
	q, err := r.st.Tenant(ctx)
	if err != nil {
		return err
	}
	res, err := q.Exec(`UPDATE path_rules SET deleted_at = NOW(), del_key = id
		WHERE tenant_id = ? AND id = ? AND deleted_at IS NULL`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// Version 返回某应用当前策略集合的版本标识。
//
// # 为什么必须有
//
// 审计里少了策略版本号，三个月后没人能复现「当时为什么放行」—— 策略早就改过了。
// 用 max(id) + 条数 + 最后变更时间拼出来，够唯一也够便宜；
// 不用哈希整张表是因为网关每次判定都要带上它，不能是个昂贵操作。
func (r *Repo) Version(ctx context.Context, appID int64) (string, error) {
	q, err := r.st.Tenant(ctx)
	if err != nil {
		return "", err
	}
	var maxID, cnt int64
	var lastChange sql.NullString
	err = q.QueryRow(`SELECT COALESCE(MAX(id),0), COUNT(*), COALESCE(MAX(created_at),'')
		FROM path_rules WHERE tenant_id = ? AND app_id = ? AND deleted_at IS NULL`, appID).
		Scan(&maxID, &cnt, &lastChange)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("p%d.n%d", maxID, cnt), nil
}
