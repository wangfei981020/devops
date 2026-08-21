package api

import (
	"context"
	"fmt"

	"ops-sso-backend/internal/domain/access"
	"ops-sso-backend/internal/store"
	"ops-sso-backend/logx"
)

// subjectNames 把规则里的主体 ID 批量解析成人能读的名字。
//
// # 为什么必须做这件事
//
// 界面上出现 `用户组 #7` 就是把内部实现漏给了用户 —— 没人记得住 7 是哪个组，
// 而复核规则时第一件事就是"这条管的是谁"。走查时这条被专门挑出来过。
//
// # 为什么是批量而不是逐条查
//
// 规则表动辄几十上百条，逐条查就是 N+1：一次列表请求打出上百条 SQL。
// 这里先把 ID 按类型收集，每类一条 IN 查询。
//
// # 查不到时给什么
//
// 给 `用户组 #7（已删除？）` 这种，**不是空字符串也不是原样的 ID**。
// 主体被删了而规则还在，是真实会发生的事（删组时没清规则），
// 而那条规则仍然在参与判定 —— 显示成空白会让人以为是界面坏了，
// 显示成裸 ID 又看不出这是个异常。要让人一眼看出"这条该清理了"。
type subjectResolver struct {
	users  map[int64]string
	groups map[int64]string
	depts  map[int64]string
}

func resolveSubjects(ctx context.Context, st *store.Store, rules []access.Rule) *subjectResolver {
	r := &subjectResolver{
		users:  map[int64]string{},
		groups: map[int64]string{},
		depts:  map[int64]string{},
	}
	var userIDs, groupIDs, deptIDs []int64
	for _, rule := range rules {
		switch rule.SubjectType {
		case access.SubjectUser:
			userIDs = append(userIDs, rule.SubjectID)
		case access.SubjectGroup:
			groupIDs = append(groupIDs, rule.SubjectID)
		case access.SubjectDept:
			deptIDs = append(deptIDs, rule.SubjectID)
		}
	}

	q, err := st.Tenant(ctx)
	if err != nil {
		logx.Line("subject", "取租户上下文失败: "+err.Error())
		// 解析不出名字不该让整个列表失败 —— 规则本身是查到了的。
		// 退回显示 ID，比整页报错强。
		return r
	}

	// users 用 display_name，空则退回 username。
	//
	// ⚠️ `users` 表**没有 tenant_id** —— 它是全局表（001_init_core.sql 里
	// 明写了不带 tenant_id 的只有 tenants/users/user_tenants/auth_sessions 等 5 张），
	// 用户与租户的关系在 user_tenants 里。
	// 早先照抄了其他表的 `WHERE tenant_id = ?`，SQL 直接报
	// 「Unknown column 'tenant_id'」，而 fill() 吞掉了错误 ——
	// 表现是所有用户主体都显示成「已删除？」，而用户明明在。
	// 这也说明吞错误的代价：真因藏在一个不会被打印的 err 里。
	fillUsers(q, userIDs, r.users)
	fill(q, `SELECT id, name FROM user_groups WHERE tenant_id = ? AND id IN`, groupIDs, r.groups)
	fill(q, `SELECT id, name FROM departments WHERE tenant_id = ? AND id IN`, deptIDs, r.depts)
	return r
}

// fillUsers 单独一个函数：users 是全局表，要 JOIN user_tenants 才能限定租户。
// 不 JOIN 的话会把别的租户的用户名也解析出来 —— 跨租户信息泄露。
func fillUsers(q *store.Scoped, ids []int64, out map[int64]string) {
	if len(ids) == 0 {
		return
	}
	ph, args := placeholders(ids)
	rows, err := q.Query(`SELECT u.id, IF(u.display_name = '', u.username, u.display_name)
		FROM users u JOIN user_tenants ut ON ut.user_id = u.id AND ut.tenant_id = ?
		WHERE u.id IN`+ph, args...)
	if err != nil {
		// ⚠️ 必须打日志。解析失败会退化成「已删除？」，而那看起来像数据问题
		// 而不是 SQL 问题 —— 上一版就是因为这里静默吞掉，
		// 「Unknown column 'tenant_id'」藏了很久才被发现。
		logx.Line("subject", "解析用户名字失败: "+err.Error())
		return
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var name string
		if rows.Scan(&id, &name) == nil {
			out[id] = name
		}
	}
}

func placeholders(ids []int64) (string, []any) {
	s := "("
	args := make([]any, 0, len(ids))
	for i, id := range ids {
		if i > 0 {
			s += ","
		}
		s += "?"
		args = append(args, id)
	}
	return s + ")", args
}

func fill(q *store.Scoped, prefix string, ids []int64, out map[int64]string) {
	if len(ids) == 0 {
		return
	}
	ph, args := placeholders(ids)
	rows, err := q.Query(prefix+ph, args...)
	if err != nil {
		logx.Line("subject", "解析主体名字失败: "+err.Error())
		return
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var name string
		if rows.Scan(&id, &name) == nil {
			out[id] = name
		}
	}
}

// Name 主体的显示名。第二个返回值表示是否解析成功。
func (r *subjectResolver) Name(t access.SubjectType, id int64) (string, bool) {
	if r == nil {
		return "", false
	}
	var m map[int64]string
	switch t {
	case access.SubjectUser:
		m = r.users
	case access.SubjectGroup:
		m = r.groups
	case access.SubjectDept:
		m = r.depts
	default:
		// public 没有 ID；role 目前没有对应的表，前端按类型渲染即可
		return "", false
	}
	n, ok := m[id]
	if !ok || n == "" {
		return "", false
	}
	return n, true
}

// annotate 给规则的 JSON 补上 subject_name 与 subject_missing。
func (r *subjectResolver) annotate(h map[string]any, t access.SubjectType, id int64) {
	if t == access.SubjectPublic {
		return // 「所有人」没有具体主体，前端用固定文案
	}
	if name, ok := r.Name(t, id); ok {
		h["subject_name"] = name
		return
	}
	// 解析不到：可能是主体被删了而规则还在，也可能是 role 这类还没有表。
	// **明确标出来**，而不是留空让前端猜。
	h["subject_name"] = fmt.Sprintf("#%d", id)
	h["subject_missing"] = true
}
