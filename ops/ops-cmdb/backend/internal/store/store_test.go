package store

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// 这个包是租户隔离的唯一入口。一个漏洞就是跨租户数据泄露 ——
// 而那类事故的特点是**没有任何报错**，数据就那么被别的租户看见了。
// 所以这里测的重点是「该拒绝的有没有拒绝」，而不是「能不能查出数据」。

// ── 上下文 ────────────────────────────────────────────────────

// ★ 没有租户上下文必须报错，绝不能退化成「查全表」。
// 忘记设上下文的后果必须是报错，不能是泄露。
func TestNoTenantContextIsRejected(t *testing.T) {
	st := New(nil)
	if _, err := st.Tenant(context.Background()); !errors.Is(err, ErrNoTenantContext) {
		t.Fatalf("err = %v, want ErrNoTenantContext", err)
	}
}

func TestTenantRoundTrip(t *testing.T) {
	ctx := WithTenant(context.Background(), 42)
	id, ok := TenantFrom(ctx)
	if !ok || id != 42 {
		t.Fatalf("TenantFrom = (%d, %v), want (42, true)", id, ok)
	}
}

// MustTenant 在缺上下文时必须 panic —— 那是编码错误（handler 挂在了中间件之前），
// 静默吞掉会变成「查不到数据」，比崩溃难查得多。
func TestMustTenantPanicsWithoutContext(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("MustTenant 在无租户上下文时应 panic")
		}
	}()
	New(nil).MustTenant(context.Background())
}

// ── 强制声明 ──────────────────────────────────────────────────

// ★ 核心：语句里没有 tenant_id 条件就必须被拒绝。
func TestQueryWithoutTenantFilterIsRejected(t *testing.T) {
	sc := &Scoped{raw: nil, tenant: 7, ctx: context.Background()}

	bad := []string{
		"SELECT id, name FROM hosts",
		"SELECT * FROM hosts WHERE zone = ?",
		"SELECT count(*) FROM hosts WHERE status = 'RUNNING' AND zone = ?",
		"UPDATE hosts SET name = ? WHERE id = ?",
		"DELETE FROM hosts WHERE id = ?",
	}
	for _, q := range bad {
		if _, err := sc.Query(q); !errors.Is(err, ErrMissingTenantFilter) {
			t.Errorf("Query(%q) err = %v, want ErrMissingTenantFilter", firstLine(q), err)
		}
		if _, err := sc.Exec(q); !errors.Is(err, ErrMissingTenantFilter) {
			t.Errorf("Exec(%q) err = %v, want ErrMissingTenantFilter", firstLine(q), err)
		}
	}
}

// ★ 硬编码租户值同样要拒绝 —— 它比忘记过滤更危险，因为**看起来是对的**。
func TestHardcodedTenantValueIsRejected(t *testing.T) {
	sc := &Scoped{raw: nil, tenant: 7, ctx: context.Background()}
	for _, q := range []string{
		"SELECT * FROM hosts WHERE tenant_id = 5",
		"SELECT * FROM hosts WHERE tenant_id=1 AND zone = ?",
	} {
		if _, err := sc.Query(q); !errors.Is(err, ErrMissingTenantFilter) {
			t.Errorf("Query(%q) 应被拒绝（硬编码租户值），得到 %v", q, err)
		}
	}
}

// 各种合法写法都要认得（表别名、大小写、多余空格）。
func TestValidTenantFilterFormsAreAccepted(t *testing.T) {
	for _, q := range []string{
		"SELECT * FROM hosts WHERE tenant_id = ?",
		"SELECT * FROM hosts WHERE tenant_id=?",
		"SELECT * FROM hosts h WHERE h.tenant_id = ? AND h.zone = ?",
		"SELECT * FROM hosts WHERE TENANT_ID = ?",
		"SELECT * FROM hosts WHERE  tenant_id   =   ?  AND x = ?",
	} {
		if err := checkFilter(q); err != nil {
			t.Errorf("checkFilter(%q) = %v, want nil", q, err)
		}
	}
}

// ★ INSERT 的租户列在列清单里而不是 WHERE 里，规则不同，必须单独校验。
// 混用会让其中一种被放行。
func TestInsertRequiresTenantColumn(t *testing.T) {
	sc := &Scoped{raw: nil, tenant: 7, ctx: context.Background()}

	if _, err := sc.Insert("INSERT INTO hosts (name, zone) VALUES (?, ?)", "a", "b"); err == nil {
		t.Error("INSERT 缺 tenant_id 列应被拒绝")
	}
	// 合法形式不该在校验阶段被拒（raw 为 nil 会在执行时 panic，所以只验校验逻辑）
	if !insertPattern.MatchString("INSERT INTO hosts (tenant_id, name) VALUES (?, ?)") {
		t.Error("合法 INSERT 未通过校验")
	}
	if !insertPattern.MatchString("insert into `hosts` (`tenant_id`, `name`) values (?, ?)") {
		t.Error("反引号 + 小写形式未通过校验")
	}
}

// 用 Exec 跑 INSERT 会被拒 —— 因为 INSERT 里没有 `tenant_id = ?`。
// 这是有意的：强迫走 Insert()，那里的校验才是对的。
func TestExecRejectsInsert(t *testing.T) {
	sc := &Scoped{raw: nil, tenant: 7, ctx: context.Background()}
	q := "INSERT INTO hosts (tenant_id, name) VALUES (?, ?)"
	if _, err := sc.Exec(q, "x"); !errors.Is(err, ErrMissingTenantFilter) {
		t.Errorf("Exec 跑 INSERT 应被拒绝（要用 Insert）")
	}
}

// ── 平台白名单 ────────────────────────────────────────────────

// ★ 用平台查询器碰业务表必须被拒绝 —— 那是最容易的一种越界。
func TestPlatformCannotTouchTenantTables(t *testing.T) {
	for _, q := range []string{
		"SELECT * FROM hosts WHERE id = ?",
		"SELECT * FROM domains",
		"UPDATE certificates SET x = ?",
		"INSERT INTO cloud_accounts (name) VALUES (?)",
		"SELECT u.id FROM users u JOIN hosts h ON h.owner = u.id", // JOIN 里混入业务表
	} {
		if err := checkPlatformTables(q); !errors.Is(err, ErrNotPlatformTable) {
			t.Errorf("checkPlatformTables(%q) = %v, want ErrNotPlatformTable", firstLine(q), err)
		}
	}
}

func TestPlatformAllowsWhitelistedTables(t *testing.T) {
	for _, q := range []string{
		"SELECT * FROM users WHERE id = ?",
		"SELECT * FROM tenants",
		"SELECT u.id FROM users u JOIN user_tenants ut ON ut.user_id = u.id",
		"UPDATE licenses SET payload = ?",
		"INSERT INTO auth_sessions (token) VALUES (?)",
	} {
		if err := checkPlatformTables(q); err != nil {
			t.Errorf("checkPlatformTables(%q) = %v, want nil", firstLine(q), err)
		}
	}
}

// ★ 白名单是安全边界，内容要锁住 —— 加一张表进去等于宣布它跨租户共享。
// 这条测试让「顺手加一个」变成一次显式的、需要改测试的决定。
func TestWhitelistContentIsLocked(t *testing.T) {
	// 每一条都要说得出"为什么这份数据不属于任何租户"。
	want := map[string]bool{
		"tenants": true, "users": true, "user_tenants": true, "auth_sessions": true,
		"idp_configs": true, "idp_group_mappings": true,
		"licenses": true, "schema_migrations": true, "ci_types": true,
		// leases：2026-08-07 加入。它协调的是**进程**不是数据 ——
		// 「谁是 leader」对所有租户是同一个答案。
		// 若给它加 tenant_id，就成了每个租户各选一个 leader，
		// 那正好取消了这张表存在的意义（保证任务只跑一次）。
		// 它也不含任何租户可见的业务数据，只有 owner/到期时刻/栅栏令牌。
		"leases": true,
		// install_identity：2026-08-09 加入。它存的是**这套安装**的 UUID，
		// 是安装指纹的两个输入之一（另一个是 MySQL 实例的 server_id）。
		// 指纹标识的是"这套部署"，与租户无关 —— 带 tenant_id 会让每个租户
		// 算出不同的指纹，而签发方只签一个，于是多租户环境里必然有租户对不上。
		// 表里只有一个 UUID 和创建时间，不含任何业务数据。
		"install_identity": true,
	}
	for _, n := range PlatformTableNames() {
		if !want[n] {
			t.Errorf("白名单里多了 %q —— 新增平台级表必须经过评审并更新本测试", n)
		}
		delete(want, n)
	}
	for n := range want {
		t.Errorf("白名单里少了 %q", n)
	}
}

// 这几张表看起来像平台级，实际带 tenant_id，绝不能进白名单。
func TestLookalikeTablesStayOutOfWhitelist(t *testing.T) {
	for _, n := range []string{
		"settings",           // 0=平台级，非 0=租户级，租户级覆盖平台级
		"local_roles",        // 0=内置模板，租户可建自己的角色
		"environments",       // 各租户可自行配置
		"lifecycle_statuses", // 同上
		"audit_logs",         // 平台操作用 0，租户操作用租户 ID
		"projects", "hosts", "domains", "certificates",
	} {
		if IsPlatformTable(n) {
			t.Errorf("%q 不该在平台白名单里 —— 它是租户级表", n)
		}
	}
}

// ── 参数注入 ──────────────────────────────────────────────────

// ★ 租户 ID 由本层填，插在**它自己那个占位符**的位置上。
//
//	⚠️ 这两条测试原本写的是「租户 ID 必须是第一个参数」——
//	那正是当时实现的行为，于是测试把 bug 一起固化成了规范。
//	对 SELECT/DELETE 碰巧成立，对 UPDATE 则每一列都错位。
//	断言写成"实现现在是怎样"而不是"正确应该怎样"，就会变成这样。
func TestInsertAtPutsTenantAtGivenPosition(t *testing.T) {
	got := insertAt(42, []any{"zone-a", 100}, 1)
	want := []any{"zone-a", int64(42), 100}
	if len(got) != len(want) {
		t.Fatalf("参数个数 = %d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("位置 %d = %v, want %v（完整 %v）", i, got[i], want[i], got)
		}
	}
}

func TestInsertAtWithNoArgs(t *testing.T) {
	got := insertAt(7, nil, 0)
	if len(got) != 1 || got[0] != int64(7) {
		t.Errorf("insertAt(7, nil, 0) = %v, want [7]", got)
	}
}

// 位置越界时兜底到 0，而不是 panic —— 校验已经保证语句里有租户条件。
func TestInsertAtOutOfRangeFallsBackToFront(t *testing.T) {
	got := insertAt(3, []any{"a"}, 99)
	if len(got) != 2 || got[0] != int64(3) {
		t.Errorf("越界应兜底到最前，得到 %v", got)
	}
}

// ── 错误信息可读性 ────────────────────────────────────────────

// 报错要带上出问题的语句片段 —— 否则在几百个查询里定位是灾难。
func TestErrorIncludesQuerySnippet(t *testing.T) {
	sc := &Scoped{raw: nil, tenant: 1, ctx: context.Background()}
	_, err := sc.Query("SELECT id FROM hosts WHERE zone = ?")
	if err == nil || !strings.Contains(err.Error(), "SELECT id FROM hosts") {
		t.Errorf("错误信息应包含语句片段，得到: %v", err)
	}
}

func TestFirstLineTruncatesLongQuery(t *testing.T) {
	long := "SELECT " + strings.Repeat("x", 300) + " FROM t"
	if got := firstLine(long); len(got) > 130 {
		t.Errorf("firstLine 长度 %d，应被截断", len(got))
	}
	multi := "SELECT a\nFROM t\nWHERE x = ?"
	if got := firstLine(multi); strings.Contains(got, "\n") {
		t.Errorf("firstLine 应只取首行，得到 %q", got)
	}
}

// ★ ON DUPLICATE KEY UPDATE 后面跟的是**列名**，不能当成表名校验。
//
// 这条是真库测试抓出来的：upsert 一律被拒，报错还说
// 「"fence" 不是平台级表」—— 一个根本不存在的表名，指向完全错误的方向。
// 纯逻辑测试里没人会想到去构造 upsert 语句，所以之前一直没暴露。
func TestPlatformAllowsUpsertOnWhitelistedTable(t *testing.T) {
	for _, q := range []string{
		"INSERT INTO leases (name, owner, fence) VALUES (?,?,1) ON DUPLICATE KEY UPDATE fence = fence + 1",
		"INSERT INTO leases (name, owner) VALUES (?,?) ON DUPLICATE KEY UPDATE owner = VALUES(owner), expires_at = VALUES(expires_at)",
	} {
		if err := checkPlatformTables(q); err != nil {
			t.Errorf("checkPlatformTables(%q) = %v, want nil", firstLine(q), err)
		}
	}
}

// 但 upsert **之前**的表名照样要校验 —— 切掉尾巴不能把前面的检查也一起丢了。
func TestPlatformStillRejectsUpsertOnBusinessTable(t *testing.T) {
	q := "INSERT INTO hosts (name) VALUES (?) ON DUPLICATE KEY UPDATE name = VALUES(name)"
	if err := checkPlatformTables(q); !errors.Is(err, ErrNotPlatformTable) {
		t.Errorf("checkPlatformTables(%q) = %v, want ErrNotPlatformTable", firstLine(q), err)
	}
}
