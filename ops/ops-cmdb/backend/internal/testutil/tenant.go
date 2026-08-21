// Package testutil 提供跨租户越权测试的脚手架。
//
// # 为什么必须用真实数据库
//
// 租户隔离的失效点在 SQL 层：一条忘了 tenant_id 的语句，用 mock 跑照样"通过"，
// 因为 mock 返回的是你让它返回的东西。只有真实数据库会诚实地把别人的行返回给你。
//
// 所以这套工具**不提供 mock**。没有 TEST_MYSQL_DSN 就跳过，
// 让缺失变成显式的 skip，而不是一个虚假的绿色。
//
// # 用法
//
//	func TestXxxCrossTenant(t *testing.T) {
//	    env := testutil.NewTenantEnv(t)      // 建两个租户 + 各自一行数据
//	    defer env.Close()
//	    ...
//	    env.AssertNotVisible(t, "registrars", env.B.RowID, env.A.ID)
//	}
package testutil

import (
	"database/sql"
	"fmt"
	"os"
	"testing"

	_ "github.com/go-sql-driver/mysql"

	"ops-cmdb-backend/internal/store"
)

// Tenant 一个测试租户及其数据。
type Tenant struct {
	ID    store.TenantID
	Name  string
	RowID int64 // 该租户在当前被测表里的那行
}

// TenantEnv 两个互不相干的租户。名字刻意叫 A/B 而不是 1/2 ——
// 断言里出现 "租户 A 看到了 B 的数据" 比 "租户 1 看到了 2 的数据" 好读。
type TenantEnv struct {
	DB    *sql.DB
	Store *store.Store
	A, B  Tenant

	table string
	t     *testing.T
}

// NewTenantEnv 准备两个租户。没有 TEST_MYSQL_DSN 则 skip。
func NewTenantEnv(t *testing.T) *TenantEnv {
	t.Helper()
	dsn := os.Getenv("TEST_MYSQL_DSN")
	if dsn == "" {
		t.Skip("未设 TEST_MYSQL_DSN，跳过跨租户越权测试")
	}
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		t.Fatalf("连接数据库: %v", err)
	}
	if err := db.Ping(); err != nil {
		t.Fatalf("数据库不可达: %v", err)
	}

	env := &TenantEnv{DB: db, Store: store.New(db), t: t}
	env.A = env.createTenant("tenant-a")
	env.B = env.createTenant("tenant-b")
	return env
}

func (e *TenantEnv) createTenant(slug string) Tenant {
	e.t.Helper()
	// 用 slug 去重，重复跑测试不会堆积垃圾租户
	res, err := e.DB.Exec(
		`INSERT INTO tenants (name, slug) VALUES (?, ?)
		 ON DUPLICATE KEY UPDATE name = VALUES(name), id = LAST_INSERT_ID(id)`,
		slug, slug)
	if err != nil {
		e.t.Fatalf("建租户 %s: %v", slug, err)
	}
	id, _ := res.LastInsertId()
	return Tenant{ID: store.TenantID(id), Name: slug}
}

// SeedRow 往指定表里给两个租户各插一行，返回各自的行 id。
//
// cols 是除 tenant_id 外的列，vals 是对应的值 —— 两个租户插一样的内容，
// 这样"能不能看到"就只取决于隔离，与数据内容无关。
func (e *TenantEnv) SeedRow(table string, cols []string, vals ...any) {
	e.t.Helper()
	e.table = table

	colList := "tenant_id"
	ph := "?"
	for _, c := range cols {
		colList += ", " + c
		ph += ", ?"
	}
	q := fmt.Sprintf(`INSERT INTO %s (%s) VALUES (%s)`, table, colList, ph)

	for _, tn := range []*Tenant{&e.A, &e.B} {
		args := append([]any{int64(tn.ID)}, vals...)
		res, err := e.DB.Exec(q, args...)
		if err != nil {
			e.t.Fatalf("给租户 %s 插入 %s: %v", tn.Name, table, err)
		}
		tn.RowID, _ = res.LastInsertId()
	}
}

// AssertNotVisible 断言 viewer 租户看不到 rowID 这一行。
//
// **这是整套隔离的核心断言。** 它直接查库，不经过任何应用层 ——
// 因为要验证的正是"应用层的过滤有没有真的生效到 SQL 上"。
func (e *TenantEnv) AssertNotVisible(t *testing.T, table string, rowID int64, viewer store.TenantID) {
	t.Helper()
	var n int
	err := e.DB.QueryRow(
		fmt.Sprintf(`SELECT COUNT(*) FROM %s WHERE id = ? AND tenant_id = ?`, table),
		rowID, int64(viewer)).Scan(&n)
	if err != nil {
		t.Fatalf("查询 %s: %v", table, err)
	}
	if n != 0 {
		t.Errorf("租户 %d 能看到不属于它的 %s#%d —— 跨租户泄露", viewer, table, rowID)
	}
}

// AssertVisible 断言 owner 看得到自己的行（防止隔离过头把自己的也挡了）。
func (e *TenantEnv) AssertVisible(t *testing.T, table string, rowID int64, owner store.TenantID) {
	t.Helper()
	var n int
	err := e.DB.QueryRow(
		fmt.Sprintf(`SELECT COUNT(*) FROM %s WHERE id = ? AND tenant_id = ?`, table),
		rowID, int64(owner)).Scan(&n)
	if err != nil {
		t.Fatalf("查询 %s: %v", table, err)
	}
	if n != 1 {
		t.Errorf("租户 %d 看不到自己的 %s#%d —— 隔离过头了", owner, table, rowID)
	}
}

// CountFor 返回某租户在表里的行数，用于验证列表接口的隔离。
func (e *TenantEnv) CountFor(table string, tenant store.TenantID) int {
	e.t.Helper()
	var n int
	if err := e.DB.QueryRow(
		fmt.Sprintf(`SELECT COUNT(*) FROM %s WHERE tenant_id = ?`, table),
		int64(tenant)).Scan(&n); err != nil {
		e.t.Fatalf("统计 %s: %v", table, err)
	}
	return n
}

// Close 清理测试数据。
//
// 只删自己造的两个租户的数据，不 TRUNCATE —— 测试库里可能还有别的东西，
// 而"测试把别人的数据清了"是最招人恨的失败模式。
func (e *TenantEnv) Close() {
	if e.table != "" {
		for _, tn := range []Tenant{e.A, e.B} {
			_, _ = e.DB.Exec(fmt.Sprintf(`DELETE FROM %s WHERE tenant_id = ?`, e.table), int64(tn.ID))
		}
	}
	for _, tn := range []Tenant{e.A, e.B} {
		_, _ = e.DB.Exec(`DELETE FROM tenants WHERE id = ?`, int64(tn.ID))
	}
	_ = e.DB.Close()
}
