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
	want := map[string]bool{
		"tenants": true, "users": true, "user_tenants": true, "auth_sessions": true,
		"idp_configs": true, "licenses": true, "schema_migrations": true,
		"local_credentials": true, "break_glass_codes": true,
		"mfa_secrets": true, "idp_auth_states": true, "oidc_signing_keys": true,
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
		"step_up_tickets", // 票据是"在某租户的某应用上提权"，是租户级的
		"oidc_clients",    // 客户端属于租户的应用
		"oidc_auth_codes", "oidc_sessions",
		"apps",            // 应用属于租户
		"app_groups",      // 分组由租户自定义
		"access_policies", // ★ 授权规则 —— 这张表误入白名单等于一个租户的规则作用到所有租户
		"user_groups", "departments",
		"settings",   // 0=平台级，非 0=租户级，租户级覆盖平台级
		"audit_logs", // 平台操作用 0，租户操作用租户 ID
	} {
		if IsPlatformTable(n) {
			t.Errorf("%q 不该在平台白名单里 —— 它是租户级表", n)
		}
	}
}

// ── 参数注入 ──────────────────────────────────────────────────

// ★ 租户 ID 必须是第一个参数，且由本层填 —— 调用方无从传错。
func TestTenantIDIsPrependedAsFirstArg(t *testing.T) {
	got := prepend(42, []any{"zone-a", 100})
	if len(got) != 3 {
		t.Fatalf("参数个数 = %d, want 3", len(got))
	}
	if got[0] != int64(42) {
		t.Errorf("首个参数 = %v (%T), want int64(42)", got[0], got[0])
	}
	if got[1] != "zone-a" || got[2] != 100 {
		t.Errorf("后续参数顺序被打乱: %v", got[1:])
	}
}

func TestPrependWithNoArgs(t *testing.T) {
	got := prepend(7, nil)
	if len(got) != 1 || got[0] != int64(7) {
		t.Errorf("prepend(7, nil) = %v, want [7]", got)
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

// ★ 检查器自身的误报回归。
//
// `INSERT ... ON DUPLICATE KEY UPDATE secret_enc = VALUES(secret_enc)` 里的
// `UPDATE secret_enc` 曾被当成表名，导致一条完全正常的语句被白名单拦下。
// 误报比漏报更糟 —— 防线一旦开始喊狼来了，人就会绕过它。
func TestPlatformCheckerDoesNotFalseFlagOnDuplicateKeyUpdate(t *testing.T) {
	ok := []string{
		"INSERT INTO mfa_secrets (user_id, secret_enc) VALUES (?, ?) ON DUPLICATE KEY UPDATE secret_enc = VALUES(secret_enc)",
		"INSERT INTO users (username) VALUES (?) ON DUPLICATE KEY UPDATE display_name = VALUES(display_name), email = VALUES(email)",
		"insert into local_credentials (user_id, password_hash) values (?, ?) on duplicate key update password_hash = values(password_hash)",
	}
	for _, q := range ok {
		if err := checkPlatformTables(q); err != nil {
			t.Errorf("正常语句被误判为越界：%v\n%s", err, q)
		}
	}

	// 但真正的越界仍然要拦住 —— 修误报不能把漏报一起修出来
	bad := []string{
		"INSERT INTO apps (tenant_id, code) VALUES (?, ?) ON DUPLICATE KEY UPDATE code = VALUES(code)",
		"SELECT * FROM access_policies WHERE id = ?",
		"UPDATE app_groups SET name = ? WHERE id = ?",
	}
	for _, q := range bad {
		if err := checkPlatformTables(q); err == nil {
			t.Errorf("业务表越界未被拦住：%s", q)
		}
	}
}
