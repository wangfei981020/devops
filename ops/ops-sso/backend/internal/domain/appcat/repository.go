package appcat

import (
	"context"
	"database/sql"
	"fmt"

	"ops-sso-backend/internal/store"
)

type Repo struct{ st *store.Store }

func NewRepo(st *store.Store) *Repo { return &Repo{st: st} }

// ListGroups 列出分组，带每组的应用数。
func (r *Repo) ListGroups(ctx context.Context) ([]Group, error) {
	q, err := r.st.Tenant(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := q.Query(`SELECT g.id, g.code, g.name, g.description, g.sort_order,
		    (SELECT COUNT(*) FROM app_group_members m
		      JOIN apps a ON a.id = m.app_id AND a.tenant_id = m.tenant_id AND a.deleted_at IS NULL
		     WHERE m.tenant_id = g.tenant_id AND m.group_id = g.id) AS app_count
		FROM app_groups g
		WHERE g.tenant_id = ? AND g.deleted_at IS NULL
		ORDER BY g.sort_order, g.id`)
	if err != nil {
		return nil, fmt.Errorf("list groups: %w", err)
	}
	defer rows.Close()
	var out []Group
	for rows.Next() {
		var g Group
		if err := rows.Scan(&g.ID, &g.Code, &g.Name, &g.Description, &g.SortOrder, &g.AppCount); err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	return out, rows.Err()
}

func (r *Repo) CreateGroup(ctx context.Context, g Group) (int64, error) {
	if err := g.Validate(); err != nil {
		return 0, err
	}
	q, err := r.st.Tenant(ctx)
	if err != nil {
		return 0, err
	}
	res, err := q.Insert(`INSERT INTO app_groups (tenant_id, code, name, description, sort_order)
		VALUES (?, ?, ?, ?, ?)`, g.Code, g.Name, g.Description, g.SortOrder)
	if err != nil {
		return 0, fmt.Errorf("create group: %w", err)
	}
	return res.LastInsertId()
}

func (r *Repo) UpdateGroup(ctx context.Context, g Group) error {
	if err := g.Validate(); err != nil {
		return err
	}
	q, err := r.st.Tenant(ctx)
	if err != nil {
		return err
	}
	_, err = q.Exec(`UPDATE app_groups SET name = ?, description = ?, sort_order = ?
		WHERE tenant_id = ? AND id = ? AND deleted_at IS NULL`, g.Name, g.Description, g.SortOrder, g.ID)
	return err
}

// DeleteGroup 删除分组。
//
// ⚠️ 分组被删时，挂在它上面的授权规则会**一起失效** —— 这可能让一批人突然进不去。
// 所以先算出影响面返回给调用方，由界面二次确认后再真删。
func (r *Repo) DeleteGroup(ctx context.Context, id int64) (affectedPolicies int, err error) {
	q, err := r.st.Tenant(ctx)
	if err != nil {
		return 0, err
	}
	if err = q.QueryRow(`SELECT COUNT(*) FROM access_policies
		WHERE tenant_id = ? AND scope = 'group' AND scope_id = ? AND deleted_at IS NULL`,
		id).Scan(&affectedPolicies); err != nil {
		return 0, err
	}
	if _, err = q.Exec(`UPDATE app_groups SET deleted_at = NOW()
		WHERE tenant_id = ? AND id = ? AND deleted_at IS NULL`, id); err != nil {
		return affectedPolicies, err
	}
	// del_key 必须一起写：只写 deleted_at 的话这条规则仍占着唯一索引，
	// 同一个主体在同一作用域再也配不回来（见迁移 002 / 010）
	if _, err = q.Exec(`UPDATE access_policies SET deleted_at = NOW(), del_key = id
		WHERE tenant_id = ? AND scope = 'group' AND scope_id = ? AND deleted_at IS NULL`, id); err != nil {
		return affectedPolicies, err
	}
	_, err = q.Exec(`DELETE FROM app_group_members WHERE tenant_id = ? AND group_id = ?`, id)
	return affectedPolicies, err
}

// CountPoliciesForGroup 删除前的影响面预览。
func (r *Repo) CountPoliciesForGroup(ctx context.Context, id int64) (int, error) {
	q, err := r.st.Tenant(ctx)
	if err != nil {
		return 0, err
	}
	var n int
	err = q.QueryRow(`SELECT COUNT(*) FROM access_policies
		WHERE tenant_id = ? AND scope = 'group' AND scope_id = ? AND deleted_at IS NULL`, id).Scan(&n)
	return n, err
}

// ListApps 列出应用，带分组归属。
func (r *Repo) ListApps(ctx context.Context) ([]App, error) {
	q, err := r.st.Tenant(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := q.Query(`SELECT id, code, name, description, connect_type, env, base_url,
		       icon_text, icon_color, status, show_when_denied
		FROM apps WHERE tenant_id = ? AND deleted_at IS NULL ORDER BY name`)
	if err != nil {
		return nil, fmt.Errorf("list apps: %w", err)
	}
	byID := map[int64]*App{}
	var out []App
	for rows.Next() {
		var a App
		var showDenied int
		if err := rows.Scan(&a.ID, &a.Code, &a.Name, &a.Description, &a.ConnectType, &a.Env,
			&a.BaseURL, &a.IconText, &a.IconColor, &a.Status, &showDenied); err != nil {
			rows.Close()
			return nil, err
		}
		a.ShowWhenDenied = showDenied == 1
		out = append(out, a)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i := range out {
		byID[out[i].ID] = &out[i]
	}

	mrows, err := q.Query(`SELECT app_id, group_id, is_primary FROM app_group_members WHERE tenant_id = ?`)
	if err != nil {
		return nil, fmt.Errorf("list app groups: %w", err)
	}
	defer mrows.Close()
	for mrows.Next() {
		var appID, groupID int64
		var primary int
		if err := mrows.Scan(&appID, &groupID, &primary); err != nil {
			return nil, err
		}
		if a := byID[appID]; a != nil {
			a.GroupIDs = append(a.GroupIDs, groupID)
			if primary == 1 {
				a.PrimaryGroupID = groupID
			}
		}
	}
	return out, mrows.Err()
}

func (r *Repo) CreateApp(ctx context.Context, a App) (int64, error) {
	if err := a.Validate(); err != nil {
		return 0, err
	}
	q, err := r.st.Tenant(ctx)
	if err != nil {
		return 0, err
	}
	showDenied := 0
	if a.ShowWhenDenied {
		showDenied = 1
	}
	res, err := q.Insert(`INSERT INTO apps
		(tenant_id, code, name, description, connect_type, env, base_url, icon_text, icon_color, show_when_denied)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		a.Code, a.Name, a.Description, string(a.ConnectType), a.Env, a.BaseURL, a.IconText, a.IconColor, showDenied)
	if err != nil {
		return 0, fmt.Errorf("create app: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, err
	}
	if err := r.SetAppGroups(ctx, id, a.GroupIDs, a.PrimaryGroupID); err != nil {
		return id, err
	}
	return id, nil
}

// SetAppGroups 重设应用的分组归属（含主分组）。
//
// 整体替换而不是增量改：增量接口在前端要维护 add/remove 两个列表，
// 少发一个就会留下幽灵归属，而这种数据错误会直接变成授权错误。
func (r *Repo) SetAppGroups(ctx context.Context, appID int64, groupIDs []int64, primaryID int64) error {
	q, err := r.st.Tenant(ctx)
	if err != nil {
		return err
	}
	if _, err := q.Exec(`DELETE FROM app_group_members WHERE tenant_id = ? AND app_id = ?`, appID); err != nil {
		return err
	}
	for _, gid := range groupIDs {
		primary := 0
		if gid == primaryID {
			primary = 1
		}
		if _, err := q.Insert(`INSERT INTO app_group_members (tenant_id, group_id, app_id, is_primary)
			VALUES (?, ?, ?, ?)`, gid, appID, primary); err != nil {
			return fmt.Errorf("bind app %d to group %d: %w", appID, gid, err)
		}
	}
	return nil
}

// GetApp 取单个应用（含分组归属）。
func (r *Repo) GetApp(ctx context.Context, id int64) (App, error) {
	apps, err := r.ListApps(ctx)
	if err != nil {
		return App{}, err
	}
	for _, a := range apps {
		if a.ID == id {
			return a, nil
		}
	}
	return App{}, sql.ErrNoRows
}

// AppDeps 删除一个应用会连带影响什么。
//
// 每一项都是**当前活着的**数量，不含早已软删的。
type AppDeps struct {
	OIDCClients int `json:"oidc_clients"`
	Routes      int `json:"routes"`
	PathRules   int `json:"path_rules"`
	Policies    int `json:"policies"`
	Groups      int `json:"groups"`
	// Credentials 表单填充与拨测用的凭据。这两类是**硬删**：
	// 它们是密文，留着没有任何审计价值，只有泄露风险。
	Credentials int `json:"credentials"`
	// Requests 指向这个应用的访问申请。**不删**，只是数出来告诉人一声：
	// 申请是一份历史记录，删掉等于抹掉"谁在什么时候要过这个权限"。
	Requests int `json:"requests"`
}

// CountAppDeps 删除前的影响面预览。
//
// # 为什么必须先预览
//
// 删一个应用不只是列表里少一行：它名下的授权规则、接口级策略、网关路由
// 会一起失效。网关路由尤其致命 —— 那是一个域名的入口，删了之后
// 那个域名立刻没人接（最多 30 秒后生效），而点删除的人未必知道它挂着路由。
//
// 所以先把数字摆出来，由界面二次确认。与 DeleteGroup 同一个套路。
func (r *Repo) CountAppDeps(ctx context.Context, id int64) (AppDeps, error) {
	q, err := r.st.Tenant(ctx)
	if err != nil {
		return AppDeps{}, err
	}
	var d AppDeps
	// 一个个数而不是一条大 SQL：大 SQL 里某一项写错了不会报错，
	// 只会让某个数字恒为 0，而"0 条路由"恰恰是最危险的那种错。
	for _, item := range []struct {
		query string
		into  *int
	}{
		{`SELECT COUNT(*) FROM oidc_clients WHERE tenant_id = ? AND app_id = ? AND deleted_at IS NULL`, &d.OIDCClients},
		{`SELECT COUNT(*) FROM app_routes WHERE tenant_id = ? AND app_id = ? AND deleted_at IS NULL`, &d.Routes},
		{`SELECT COUNT(*) FROM path_rules WHERE tenant_id = ? AND app_id = ? AND deleted_at IS NULL`, &d.PathRules},
		{`SELECT COUNT(*) FROM access_policies WHERE tenant_id = ? AND scope = 'app' AND scope_id = ? AND deleted_at IS NULL`, &d.Policies},
		{`SELECT COUNT(*) FROM app_group_members WHERE tenant_id = ? AND app_id = ?`, &d.Groups},
		{`SELECT COUNT(*) FROM formfill_credentials WHERE tenant_id = ? AND app_id = ?`, &d.Credentials},
		{`SELECT COUNT(*) FROM access_requests WHERE tenant_id = ? AND app_id = ?`, &d.Requests},
	} {
		if err := q.QueryRow(item.query, id).Scan(item.into); err != nil {
			return AppDeps{}, err
		}
	}
	var probeCreds int
	if err := q.QueryRow(`SELECT COUNT(*) FROM probe_credentials
		WHERE tenant_id = ? AND app_id = ?`, id).Scan(&probeCreds); err != nil {
		return AppDeps{}, err
	}
	d.Credentials += probeCreds
	return d, nil
}

// UpdateApp 改应用的可变字段。
//
// ⚠️ code 不在其中：它是下游配置里填的那个值，改了等于对方系统里那一行
// 突然指向不存在的应用，而下游不会有任何提示。界面上也写着"定了就别改"。
//
// ⚠️ connect_type 也不在其中，理由见 handler 里的说明（改接入方式
// 不会让已经建好的 OIDC 客户端或网关路由消失，只会让界面和实际不符）。
func (r *Repo) UpdateApp(ctx context.Context, a App) error {
	if err := a.Validate(); err != nil {
		return err
	}
	q, err := r.st.Tenant(ctx)
	if err != nil {
		return err
	}
	// ⚠️ 存在性必须单独查，**不能拿 RowsAffected 当"找到没有"用**。
	//
	// MySQL 的 RowsAffected 数的是**真正被改动的行**，不是匹配到的行：
	// 打开编辑框什么都不改直接保存，UPDATE 匹配到 1 行、改动 0 行，
	// 于是 n == 0 —— 而这里原本把它翻成 sql.ErrNoRows，上层再翻成 404，
	// 界面报「不存在」。实测就是这么撞上的：那个应用明明就在列表里。
	//
	// （DSN 上开 CLIENT_FOUND_ROWS 能让它返回匹配数，但那会改变**所有**
	// RowsAffected 的语义 —— 比如审批那处靠"改动了才算成功"来判并发抢占，
	// 全局改掉会让它在状态不对时也报成功。所以只在这一处显式查。）
	var exists int
	if err := q.QueryRow(`SELECT COUNT(*) FROM apps
		WHERE tenant_id = ? AND id = ? AND deleted_at IS NULL`, a.ID).Scan(&exists); err != nil {
		return err
	}
	if exists == 0 {
		return sql.ErrNoRows
	}

	showDenied := 0
	if a.ShowWhenDenied {
		showDenied = 1
	}
	if _, err := q.Exec(`UPDATE apps SET name = ?, description = ?, env = ?, base_url = ?,
		    icon_text = ?, icon_color = ?, show_when_denied = ?
		WHERE tenant_id = ? AND id = ? AND deleted_at IS NULL`,
		a.Name, a.Description, a.Env, a.BaseURL, a.IconText, a.IconColor, showDenied, a.ID); err != nil {
		return fmt.Errorf("update app: %w", err)
	}
	return r.SetAppGroups(ctx, a.ID, a.GroupIDs, a.PrimaryGroupID)
}

// DeleteApp 软删应用，并把挂在它上面的东西一起收起来。
//
// # 为什么必须级联
//
// 不级联的话会留下一批指向空应用的规则和路由：网关那边已经不转发了
// （reload 的查询带 a.deleted_at IS NULL），但控制台的路由列表照旧显示 ——
// 界面说"这个域名归 X 管"，实际那个域名谁都不管。
// 两边判据不一致的东西，排障时最费时间。
//
// # 为什么不硬删
//
// 三个月后复盘"那天谁把这个系统摘了"，硬删之后无从查起。
// 审计里记的是 id，行没了连名字都对不上。
//
// ⚠️ 每条软删都要**同时**写 del_key = id，否则标识仍被占着、
// 同名应用再也建不回来（见迁移 010）。
func (r *Repo) DeleteApp(ctx context.Context, id int64) (AppDeps, error) {
	q, err := r.st.Tenant(ctx)
	if err != nil {
		return AppDeps{}, err
	}
	// 先确认这个 id 真属于本租户且还活着。放在最前面，是为了让
	// "不存在"和"删了一半"这两种失败在上层能分得开。
	var exists int
	if err := q.QueryRow(`SELECT COUNT(*) FROM apps
		WHERE tenant_id = ? AND id = ? AND deleted_at IS NULL`, id).Scan(&exists); err != nil {
		return AppDeps{}, err
	}
	if exists == 0 {
		return AppDeps{}, sql.ErrNoRows
	}

	deps, err := r.CountAppDeps(ctx, id)
	if err != nil {
		return AppDeps{}, err
	}

	// ★ 顺序是刻意的：**先收下挂件，最后才删应用本身**。
	//
	// 反过来写（先删应用）在中途失败时会留下一个删不掉也回不来的状态 ——
	// 应用已经是 deleted，再点一次删除只会得到 404，而它名下的路由和规则
	// 还挂着。反过来，应用留到最后删：中途失败时应用仍然活着、仍然能再点一次，
	// 而每条语句都带 `deleted_at IS NULL`，重跑一遍是幂等的。
	//
	// 这个顺序换来的是"失败之后能自己修好"，不需要给 store 加事务。
	for _, stmt := range []string{
		`UPDATE oidc_clients SET deleted_at = NOW(), del_key = id
		   WHERE tenant_id = ? AND app_id = ? AND deleted_at IS NULL`,
		`UPDATE app_routes SET deleted_at = NOW(), del_key = id
		   WHERE tenant_id = ? AND app_id = ? AND deleted_at IS NULL`,
		`UPDATE path_rules SET deleted_at = NOW(), del_key = id
		   WHERE tenant_id = ? AND app_id = ? AND deleted_at IS NULL`,
		`UPDATE access_policies SET deleted_at = NOW(), del_key = id
		   WHERE tenant_id = ? AND scope = 'app' AND scope_id = ? AND deleted_at IS NULL`,
		// 分组归属和凭据是硬删：前者是纯关联行，后者是密文（留着只有泄露风险）
		`DELETE FROM app_group_members WHERE tenant_id = ? AND app_id = ?`,
		`DELETE FROM formfill_credentials WHERE tenant_id = ? AND app_id = ?`,
		`DELETE FROM probe_credentials WHERE tenant_id = ? AND app_id = ?`,
	} {
		if _, err := q.Exec(stmt, id); err != nil {
			return deps, fmt.Errorf("delete app %d cascade: %w", id, err)
		}
	}

	if _, err := q.Exec(`UPDATE apps SET deleted_at = NOW(), del_key = id
		WHERE tenant_id = ? AND id = ? AND deleted_at IS NULL`, id); err != nil {
		return deps, fmt.Errorf("delete app: %w", err)
	}
	return deps, nil
}
