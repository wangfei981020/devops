package store

import (
	"context"
	"errors"
	"testing"
)

// TestScopedTx_RejectsMissingFilter 事务路径必须和非事务路径一样严 ——
// 否则「用事务」就成了绕过租户校验的后门，而级联删除恰恰都在事务里。
func TestScopedTx_RejectsMissingFilter(t *testing.T) {
	st := New(nil) // 不会真连库：校验在发 SQL 之前就失败
	sc := &Scoped{raw: st.raw, tenant: 7, ctx: context.Background()}
	tx := &ScopedTx{tenant: 7, s: sc}

	cases := []struct {
		name string
		run  func() error
	}{
		{"Exec 缺过滤", func() error { _, err := tx.Exec(`DELETE FROM hosts WHERE id=?`, 1); return err }},
		{"Query 缺过滤", func() error { _, err := tx.Query(`SELECT id FROM hosts`); return err }},
		{"Insert 列清单缺 tenant_id", func() error {
			_, err := tx.Insert(`INSERT INTO hosts (name) VALUES (?)`, "a")
			return err
		}},
		{"硬编码租户同样拒绝", func() error {
			_, err := tx.Exec(`DELETE FROM hosts WHERE tenant_id = 5 AND id=?`, 1)
			return err
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if err := c.run(); !errors.Is(err, ErrMissingTenantFilter) {
				t.Fatalf("期望 ErrMissingTenantFilter，得到 %v", err)
			}
		})
	}
}

// TestScopedTx_ErrorTextMatchesScoped 两条路径的报错文案必须一致。
func TestScopedTx_ErrorTextMatchesScoped(t *testing.T) {
	q := `INSERT INTO hosts (name) VALUES (?)`
	sc := &Scoped{tenant: 1, ctx: context.Background()}
	tx := &ScopedTx{tenant: 1, s: sc}
	_, e1 := sc.Insert(q, "a")
	_, e2 := tx.Insert(q, "a")
	if e1.Error() != e2.Error() {
		t.Fatalf("报错文案不一致：\n Scoped: %v\n Tx:     %v", e1, e2)
	}
}
