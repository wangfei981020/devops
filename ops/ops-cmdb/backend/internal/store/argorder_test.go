package store

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"io"
	"sync"
	"testing"
)

// ── 记录实参的假驱动 ────────────────────────────────────────────
//
// 这组测试要回答的问题不需要真数据库：占位符按语句里出现的先后
// 从左到右绑定，所以只要能看到「最终发出去的参数序列」就能判定对错。

type capture struct {
	mu    sync.Mutex
	query string
	args  []driver.NamedValue
}

type capConn struct{ c *capture }
type capStmt struct {
	c     *capture
	query string
}

func (d *capture) Open(string) (driver.Conn, error) { return &capConn{c: d}, nil }

func (c *capConn) Prepare(q string) (driver.Stmt, error) { return &capStmt{c: c.c, query: q}, nil }
func (c *capConn) Close() error                          { return nil }
func (c *capConn) Begin() (driver.Tx, error)             { return nil, io.EOF }

func (s *capStmt) Close() error  { return nil }
func (s *capStmt) NumInput() int { return -1 }
func (s *capStmt) Exec([]driver.Value) (driver.Result, error) {
	return nil, driver.ErrSkip
}
func (s *capStmt) Query([]driver.Value) (driver.Rows, error) { return nil, driver.ErrSkip }

func (s *capStmt) ExecContext(_ context.Context, args []driver.NamedValue) (driver.Result, error) {
	s.c.mu.Lock()
	defer s.c.mu.Unlock()
	s.c.query, s.c.args = s.query, args
	return driver.RowsAffected(1), nil
}

func newCaptureDB(t *testing.T, name string) (*sql.DB, *capture) {
	t.Helper()
	cap := &capture{}
	sql.Register(name, cap)
	db, err := sql.Open(name, "")
	if err != nil {
		t.Fatalf("打开假驱动: %v", err)
	}
	return db, cap
}

// ★ UPDATE 的租户占位符不在第一位 —— 这是最容易错、也最难发现的一类。
//
// `UPDATE t SET name=? WHERE tenant_id=? AND id=?` 里，占位符顺序是
// name → tenant_id → id。若实现无脑把租户号放在参数列表最前面，绑定就变成
// name=租户号、tenant_id=名字、id=id。
//
// 这种错**跨租户测试抓不到**：tenant_id 被绑成一个字符串，条件匹配不到任何行，
// 于是返回 0 行 → 404，看起来跟"正确地拒绝了越权"一模一样。
func TestExec_TenantArgGoesToItsPlaceholderPosition(t *testing.T) {
	db, cap := newCaptureDB(t, "capture-update")
	sc := &Scoped{raw: db, tenant: 42, ctx: context.Background()}

	if _, err := sc.Exec(`UPDATE registrars SET name=?, provider=? WHERE tenant_id = ? AND id=?`,
		"after", "namecheap", int64(7)); err != nil {
		t.Fatalf("Exec: %v", err)
	}

	got := make([]any, len(cap.args))
	for i, a := range cap.args {
		got[i] = a.Value
	}
	want := []any{"after", "namecheap", int64(42), int64(7)}
	if len(got) != len(want) {
		t.Fatalf("参数个数 %d，want %d：%v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("参数错位：位置 %d 是 %v，want %v\n完整实参 %v\nSQL %s",
				i, got[i], want[i], got, cap.query)
		}
	}
}

// SELECT/DELETE 里租户条件通常就在第一位，这条守住不要改坏。
func TestExec_TenantFirstPlaceholderStillWorks(t *testing.T) {
	db, cap := newCaptureDB(t, "capture-delete")
	sc := &Scoped{raw: db, tenant: 9, ctx: context.Background()}

	if _, err := sc.Exec(`DELETE FROM hosts WHERE tenant_id = ? AND id=?`, int64(3)); err != nil {
		t.Fatalf("Exec: %v", err)
	}
	if len(cap.args) != 2 || cap.args[0].Value != int64(9) || cap.args[1].Value != int64(3) {
		t.Fatalf("实参 %v，want [9 3]", cap.args)
	}
}

// INSERT 的租户位由**列清单里的位置**决定，不是语句里的字符位置。
func TestInsert_TenantArgFollowsColumnPosition(t *testing.T) {
	db, cap := newCaptureDB(t, "capture-insert")
	sc := &Scoped{raw: db, tenant: 5, ctx: context.Background()}

	// tenant_id 放在第二列
	if _, err := sc.Insert(`INSERT INTO hosts (name, tenant_id, zone) VALUES (?, ?, ?)`,
		"web-01", "us-east1-b"); err != nil {
		t.Fatalf("Insert: %v", err)
	}
	got := []any{cap.args[0].Value, cap.args[1].Value, cap.args[2].Value}
	want := []any{"web-01", int64(5), "us-east1-b"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("参数错位：位置 %d 是 %v，want %v（完整 %v）", i, got[i], want[i], got)
		}
	}
}

// 字符串字面量里的 ? 不能被当成占位符数进去。
func TestPlaceholdersBefore_IgnoresLiteralsAndComments(t *testing.T) {
	cases := []struct {
		name  string
		query string
		want  int
	}{
		{"普通", `UPDATE t SET a=?, b=? WHERE tenant_id = ?`, 2},
		{"字面量里的问号", `UPDATE t SET a=?, note='what?' WHERE tenant_id = ?`, 1},
		{"行注释里的问号", "UPDATE t SET a=? -- 真的吗?\n WHERE tenant_id = ?", 1},
		{"块注释里的问号", `UPDATE t SET a=? /* ?? */ WHERE tenant_id = ?`, 1},
		{"租户在最前", `DELETE FROM t WHERE tenant_id = ? AND id=?`, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := tenantArgPos(c.query); got != c.want {
				t.Errorf("tenantArgPos = %d，want %d", got, c.want)
			}
		})
	}
}
