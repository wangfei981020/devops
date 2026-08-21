package store

import "database/sql"

// Begin 开一个带租户作用域的事务。
//
// # 为什么必须有这个
//
// 级联删除天然需要事务：删云账号要连带删它下面的 project、主机、CI、磁盘。
// 没有事务的话，中途失败会留下一半数据 —— 主机没了但 CI 还在，
// 界面上就是一堆点不开的幽灵条目。
//
// 而如果 store 层不提供事务，人就会退回去用 h.DB.Begin()，
// 于是这一整串删除**全部绕开租户校验** —— 恰恰是最该被管住的地方
// （级联删除是破坏力最大的操作）失去了防护。
//
// 所以这里提供 ScopedTx：语法和 Scoped 一样，租户过滤一样强制。
func (s *Scoped) Begin() (*ScopedTx, error) {
	tx, err := s.raw.BeginTx(s.ctx, nil)
	if err != nil {
		return nil, err
	}
	return &ScopedTx{tx: tx, tenant: s.tenant, s: s}, nil
}

// ScopedTx 事务内的租户作用域查询器。方法语义与 Scoped 完全一致。
type ScopedTx struct {
	tx     *sql.Tx
	tenant TenantID
	s      *Scoped
}

// Exec 事务内写操作，租户 ID 自动作为第一个参数。
func (t *ScopedTx) Exec(query string, args ...any) (sql.Result, error) {
	if err := checkFilter(query); err != nil {
		return nil, err
	}
	return t.tx.ExecContext(t.s.ctx, query, insertAt(t.tenant, args, tenantArgPos(query))...)
}

// Insert 事务内插入，规则同 Scoped.Insert。
func (t *ScopedTx) Insert(query string, args ...any) (sql.Result, error) {
	if !insertPattern.MatchString(query) {
		return nil, errInsertNoTenant(query)
	}
	return t.tx.ExecContext(t.s.ctx, query, insertAt(t.tenant, args, insertTenantColPos(query))...)
}

// Query 事务内查询。
func (t *ScopedTx) Query(query string, args ...any) (*sql.Rows, error) {
	if err := checkFilter(query); err != nil {
		return nil, err
	}
	return t.tx.QueryContext(t.s.ctx, query, insertAt(t.tenant, args, tenantArgPos(query))...)
}

// QueryRow 事务内单行查询。
func (t *ScopedTx) QueryRow(query string, args ...any) *sql.Row {
	if err := checkFilter(query); err != nil {
		// 与 Scoped.QueryRow 同样的处理：错误只能在 Scan 时暴露。
		return t.tx.QueryRowContext(t.s.ctx, "SELECT 1 WHERE 1=0")
	}
	return t.tx.QueryRowContext(t.s.ctx, query, insertAt(t.tenant, args, tenantArgPos(query))...)
}

func (t *ScopedTx) Commit() error   { return t.tx.Commit() }
func (t *ScopedTx) Rollback() error { return t.tx.Rollback() }

// TenantID 当前事务的租户。
func (t *ScopedTx) TenantID() TenantID { return t.tenant }
