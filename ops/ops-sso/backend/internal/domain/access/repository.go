package access

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/go-sql-driver/mysql"

	"ops-sso-backend/internal/store"
)

// Repo 授权规则与主体事实的读写。
//
// 注意所有 SQL 都显式带 `tenant_id = ?` —— store 层强制要求，
// 漏了不会跑出错误结果，而是直接报错。这是故意的：宁可报错也不要跨租户。
type Repo struct{ st *store.Store }

func NewRepo(st *store.Store) *Repo { return &Repo{st: st} }

// LoadSubject 取一个人在授权求值里需要的全部事实：用户组、角色、部门（含祖先链）。
//
// 部门要连祖先一起取，因为规则挂在上级部门时也要命中下级的人。
// 用一次 JOIN 把祖先链取回来，而不是递归查 —— departments.path 存的就是 /1/7/23/。
func (r *Repo) LoadSubject(ctx context.Context, userID int64) (Subject, error) {
	sub := Subject{
		UserID:    userID,
		RoleIDs:   map[int64]bool{},
		GroupIDs:  map[int64]bool{},
		DeptDepth: map[int64]int{},
	}
	q, err := r.st.Tenant(ctx)
	if err != nil {
		return sub, err
	}

	rows, err := q.Query(`SELECT group_id FROM user_group_members
		WHERE tenant_id = ? AND user_id = ?`, userID)
	if err != nil {
		return sub, fmt.Errorf("load user groups: %w", err)
	}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return sub, err
		}
		sub.GroupIDs[id] = true
	}
	rows.Close()

	rows, err = q.Query(`SELECT ut.role_id FROM user_roles ut
		WHERE ut.tenant_id = ? AND ut.user_id = ?`, userID)
	if err == nil {
		for rows.Next() {
			var id int64
			if err := rows.Scan(&id); err == nil {
				sub.RoleIDs[id] = true
			}
		}
		rows.Close()
	}
	// user_roles 是后续版本才有的表；缺表不该让授权求值整体失败 ——
	// 少一类主体只会让判定更严格（更少 allow 命中），不会放宽。

	// 直属部门 + 全部祖先。d2 是 d1 的祖先当且仅当 d1.path 以 d2 的 path 前缀开头。
	rows, err = q.Query(`SELECT DISTINCT d2.id, d2.depth
		FROM user_departments ud
		JOIN departments d1 ON d1.id = ud.dept_id AND d1.tenant_id = ud.tenant_id AND d1.deleted_at IS NULL
		JOIN departments d2 ON d2.tenant_id = ud.tenant_id AND d2.deleted_at IS NULL
		     AND (d2.id = d1.id OR d1.path LIKE CONCAT(d2.path, '%'))
		WHERE ud.tenant_id = ? AND ud.user_id = ?`, userID)
	if err != nil {
		return sub, fmt.Errorf("load user departments: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var depth int
		if err := rows.Scan(&id, &depth); err != nil {
			return sub, err
		}
		sub.DeptDepth[id] = depth
	}
	return sub, rows.Err()
}

// LoadRulesForApp 取与某个应用相关的候选规则：全局 + 它所属分组 + 它本身。
//
// 不是把全表捞回来再过滤：一个租户可能有上万条规则，而与单个应用相关的
// 通常只有几十条。SQL 里筛掉，求值在内存里做。
func (r *Repo) LoadRulesForApp(ctx context.Context, appID int64) ([]Rule, error) {
	q, err := r.st.Tenant(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := q.Query(`SELECT p.id, p.scope, p.scope_id, p.subject_type, p.subject_id, p.effect, p.enforced, p.note
		FROM access_policies p
		WHERE p.tenant_id = ? AND p.deleted_at IS NULL AND (
		      p.scope = 'global'
		   OR (p.scope = 'app'   AND p.scope_id = ?)
		   OR (p.scope = 'group' AND p.scope_id IN (
		          SELECT m.group_id FROM app_group_members m
		          WHERE m.tenant_id = p.tenant_id AND m.app_id = ?))
		)`, appID, appID)
	if err != nil {
		return nil, fmt.Errorf("load rules: %w", err)
	}
	defer rows.Close()
	return scanRules(rows)
}

// LoadAllRules 取租户全部规则。门户一次算几十个应用时用它，避免 N 次查询。
func (r *Repo) LoadAllRules(ctx context.Context) ([]Rule, error) {
	q, err := r.st.Tenant(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := q.Query(`SELECT id, scope, scope_id, subject_type, subject_id, effect, enforced, note
		FROM access_policies WHERE tenant_id = ? AND deleted_at IS NULL`)
	if err != nil {
		return nil, fmt.Errorf("load all rules: %w", err)
	}
	defer rows.Close()
	return scanRules(rows)
}

// ListRules 按作用域列规则，给控制台的授权页用。
func (r *Repo) ListRules(ctx context.Context, scope Scope, scopeID int64) ([]Rule, error) {
	q, err := r.st.Tenant(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := q.Query(`SELECT id, scope, scope_id, subject_type, subject_id, effect, enforced, note
		FROM access_policies
		WHERE tenant_id = ? AND deleted_at IS NULL AND scope = ? AND scope_id = ?
		ORDER BY subject_type, subject_id`, string(scope), scopeID)
	if err != nil {
		return nil, fmt.Errorf("list rules: %w", err)
	}
	defer rows.Close()
	return scanRules(rows)
}

// Get 取单条规则。
//
// 存在的理由是删除前要把内容记进审计：只记「删除了规则 #7」的话，
// 三个月后复盘时 #7 已经没了，**永远查不出被删掉的是什么** ——
// 而"是谁把那条拒绝删了、删的是哪一条"恰恰是审计要回答的问题。
func (r *Repo) Get(ctx context.Context, id int64) (Rule, error) {
	q, err := r.st.Tenant(ctx)
	if err != nil {
		return Rule{}, err
	}
	rows, err := q.Query(`SELECT id, scope, scope_id, subject_type, subject_id, effect, enforced, note
		FROM access_policies
		WHERE tenant_id = ? AND id = ? AND deleted_at IS NULL`, id)
	if err != nil {
		return Rule{}, fmt.Errorf("get rule: %w", err)
	}
	defer rows.Close()
	list, err := scanRules(rows)
	if err != nil {
		return Rule{}, err
	}
	if len(list) == 0 {
		return Rule{}, sql.ErrNoRows
	}
	return list[0], nil
}

func scanRules(rows *sql.Rows) ([]Rule, error) {
	var out []Rule
	for rows.Next() {
		var r Rule
		var enforced int
		if err := rows.Scan(&r.ID, &r.Scope, &r.ScopeID, &r.SubjectType, &r.SubjectID, &r.Effect, &enforced, &r.Note); err != nil {
			return nil, err
		}
		r.Enforced = enforced == 1
		out = append(out, r)
	}
	return out, rows.Err()
}

// Create 新增一条规则。校验在领域层做过一遍，这里再做一遍 ——
// 因为 Repo 也可能被后台任务、导入工具调用，不是只有 HTTP 一条路进来。
func (r *Repo) Create(ctx context.Context, rule Rule, note string, actorID int64) (int64, error) {
	if err := rule.Validate(); err != nil {
		return 0, err
	}
	q, err := r.st.Tenant(ctx)
	if err != nil {
		return 0, err
	}
	enforced := 0
	if rule.Enforced {
		enforced = 1
	}
	res, err := q.Insert(`INSERT INTO access_policies
		(tenant_id, scope, scope_id, subject_type, subject_id, effect, enforced, note, created_by)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		string(rule.Scope), rule.ScopeID, string(rule.SubjectType), rule.SubjectID,
		string(rule.Effect), enforced, note, actorID)
	if err != nil {
		if isDuplicate(err) {
			// 同一主体在同一作用域已经有一条规则了。
			// 不做「自动覆盖」：把 allow 悄悄改成 deny 会让人以为自己没改成功。
			return 0, ErrDuplicate
		}
		return 0, fmt.Errorf("create policy: %w", err)
	}
	return res.LastInsertId()
}

// ErrDuplicate 同一主体在同一作用域已有规则。
var ErrDuplicate = errors.New("access: 该主体在此作用域已有规则")

// isDuplicate 识别 MySQL 的唯一键冲突（1062）。
//
// 用错误码而不是匹配错误文本：文本随 MySQL 版本和语言变，
// 匹配文本的代码会在某次升级后静默失效 —— 而失效的表现是「重复规则又能建了」。
func isDuplicate(err error) bool {
	var me *mysql.MySQLError
	return errors.As(err, &me) && me.Number == 1062
}

// Delete 软删除一条规则。
//
// 软删是必须的：授权规则被删掉后，审计里那条「按规则 #37 放行」要还能查到 #37 是什么。
// 硬删会让三个月后的复盘变成猜谜。
//
// del_key 同时置为 id：让唯一索引只约束活行，这样同一个主体可以被反复
// 配置→删除→再配置（见迁移 002）。
func (r *Repo) Delete(ctx context.Context, id int64) error {
	q, err := r.st.Tenant(ctx)
	if err != nil {
		return err
	}
	res, err := q.Exec(`UPDATE access_policies SET deleted_at = NOW(), del_key = id
		WHERE tenant_id = ? AND id = ? AND deleted_at IS NULL`, id)
	if err != nil {
		return fmt.Errorf("delete policy: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}
