package store

import "testing"

// TestTenantArgIndex 租户参数要插在它**实际的占位符位置**上。
//
// # 这个测试对应的是一次真实故障
//
// 原来是无条件把租户放第一位，隐含假设「tenant_id = ? 一定是第一个 ?」。
// SELECT 基本成立，UPDATE 不成立：
//
//	UPDATE access_requests SET status = ?, ... WHERE tenant_id = ? AND id = ?
//
// 前置之后每个参数错位一位，MySQL 报的是
// `Truncated incorrect DOUBLE value: '2026-08-10 22:48:10'`
// ——时间戳被绑到了 tenant_id 上。报错里根本没提到租户，
// 要从"哪个字段类型对不上"一路倒推回参数顺序才能找到。
func TestTenantArgIndex(t *testing.T) {
	cases := []struct {
		name  string
		query string
		want  int
	}{
		{"SELECT 租户在最前", `SELECT a FROM t WHERE tenant_id = ? AND b = ?`, 0},
		{"SELECT 租户带表别名", `SELECT a FROM t x WHERE x.tenant_id = ? AND b = ?`, 0},
		{"UPDATE 租户在 SET 之后", `UPDATE t SET a = ?, b = ? WHERE tenant_id = ? AND id = ?`, 2},
		{"UPDATE 单个 SET", `UPDATE t SET a = ? WHERE tenant_id = ?`, 1},
		{"DELETE 租户在最前", `DELETE FROM t WHERE tenant_id = ? AND id = ?`, 0},
		{"JOIN 里的租户在后", `SELECT a FROM t JOIN u ON u.id = ? AND u.tenant_id = ?`, 1},
		{"没有租户过滤", `SELECT 1`, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := tenantArgIndex(c.query); got != c.want {
				t.Fatalf("位置算错了：want %d, got %d\n  %s", c.want, got, c.query)
			}
		})
	}
}

func TestInsertAt(t *testing.T) {
	args := []any{"a", "b", "id"}

	// 插在最前：与旧行为一致
	got := insertAt(TenantID(7), args, 0)
	if len(got) != 4 || got[0] != int64(7) || got[1] != "a" {
		t.Fatalf("插最前的结果不对: %#v", got)
	}

	// 插在中间：这正是 UPDATE 的情形
	got = insertAt(TenantID(7), args, 2)
	if len(got) != 4 || got[0] != "a" || got[1] != "b" || got[2] != int64(7) || got[3] != "id" {
		t.Fatalf("插中间的结果不对: %#v", got)
	}

	// 插在末尾
	got = insertAt(TenantID(7), args, 3)
	if len(got) != 4 || got[3] != int64(7) {
		t.Fatalf("插末尾的结果不对: %#v", got)
	}

	// 越界：退回前置而不是丢参数 —— 少一个参数会让语句直接报错，
	// 而位置算错至少还有大多数语句是对的
	got = insertAt(TenantID(7), args, 99)
	if len(got) != 4 || got[0] != int64(7) {
		t.Fatalf("越界时应退回前置: %#v", got)
	}

	// 不改原切片：调用方可能复用 args
	if len(args) != 3 || args[0] != "a" {
		t.Fatalf("原参数被改了: %#v", args)
	}
}

// 真实语句回归：审批落库那条。参数顺序必须是
// status, approver_id, note, decided_at, granted_at, expires_at, [tenant], id
func TestDecideStatementBinding(t *testing.T) {
	const q = `UPDATE access_requests
		SET status = ?, approver_id = ?, approver_note = ?, decided_at = ?,
		    granted_at = ?, expires_at = ?
		WHERE tenant_id = ? AND id = ? AND status = 'pending'`

	args := []any{"approved", int64(1), "note", "t1", "t2", "t3", int64(42)}
	got := insertAt(TenantID(9), args, tenantArgIndex(q))

	if len(got) != 8 {
		t.Fatalf("参数个数不对: %d", len(got))
	}
	if got[6] != int64(9) {
		t.Fatalf("租户没落在第 7 位（WHERE tenant_id）：%#v", got)
	}
	if got[7] != int64(42) {
		t.Fatalf("id 没落在最后一位：%#v", got)
	}
	if got[0] != "approved" {
		t.Fatalf("status 被挤走了：%#v", got)
	}
}
