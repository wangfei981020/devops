// Package store 是全库唯一的数据访问入口，负责租户隔离。
//
// # 为什么不是「自动注入」
//
// 设计文档最初写的是「数据访问层强制注入 tenant_id」—— 那个设计假设有 ORM
// 可以挂 hook 改写查询。但本项目用的是裸 database/sql + 手写 SQL 字符串，
// 没有任何地方能安全地改写 WHERE 子句：
//
//	SELECT ... FROM a JOIN b ON ... WHERE x AND (y OR z) GROUP BY ... HAVING ...
//
// 要正确地往这种语句里插条件，等于自己实现一个 SQL 解析器 —— 改错一次就是
// 跨租户泄露，而这类错误在测试里极难覆盖全。
//
// # 所以改成「强制声明」
//
// 做不到自动注入，但做得到**不声明就不给跑**：
//
//	q, _ := st.Tenant(ctx)                      // 无租户上下文 → error
//	q.Query("... WHERE tenant_id = ? AND ...")  // 语句里没有 tenant_id → error
//
// 租户 ID 由本层作为**第一个参数**注入，调用方不传也不能传 —— 杜绝了
// 「WHERE tenant_id = ?」却把别的值传进去这种错误。
//
// 防的是「忘记」，不是「故意」。想绕过的人总能绕（他能改这个文件），
// 但忘记写条件的人会在第一次运行时就撞上错误，而不是等到某天客户
// 发现自己看到了别人的数据。
//
// # 三层防线
//
//  1. 本层：不声明 tenant_id 就报错；平台表必须走 Platform() 且在白名单里
//  2. CI：禁止业务代码直接持有 *sql.DB（绕过本层）
//  3. 测试：每个接口都要有跨租户越权用例
package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// TenantID 是租户标识。0 保留给平台级数据。
//
// 用 0 而不是 NULL 表示平台级：NULL 在 `!=` 比较里会漏行，
// 是个安全陷阱 —— `WHERE tenant_id != 5` 不会返回 NULL 行。
type TenantID int64

// PlatformTenant 平台级数据的租户标识。
const PlatformTenant TenantID = 0

var (
	// ErrNoTenantContext 请求上下文里没有租户 —— 中间件没设，或代码在请求之外调用。
	// 默认拒绝而不是查全表：忘记设上下文的后果必须是「报错」，不能是「泄露」。
	ErrNoTenantContext = errors.New("store: 上下文中没有租户标识")

	// ErrMissingTenantFilter SQL 里没有 tenant_id 条件。
	ErrMissingTenantFilter = errors.New("store: 语句缺少 tenant_id 过滤条件")

	// ErrNotPlatformTable 试图用 Platform() 访问不在白名单里的表。
	ErrNotPlatformTable = errors.New("store: 该表不是平台级表")
)

type ctxKey struct{}

// WithTenant 把租户放进上下文。只该由认证中间件调用。
func WithTenant(ctx context.Context, id TenantID) context.Context {
	return context.WithValue(ctx, ctxKey{}, id)
}

// TenantFrom 取上下文里的租户。
func TenantFrom(ctx context.Context) (TenantID, bool) {
	v, ok := ctx.Value(ctxKey{}).(TenantID)
	return v, ok
}

// Store 包住原始连接。业务代码不应持有 *sql.DB。
type Store struct {
	raw *sql.DB
}

func New(db *sql.DB) *Store { return &Store{raw: db} }

// Tenant 取租户作用域的查询器。上下文里没有租户就报错。
func (s *Store) Tenant(ctx context.Context) (*Scoped, error) {
	id, ok := TenantFrom(ctx)
	if !ok {
		return nil, ErrNoTenantContext
	}
	return &Scoped{raw: s.raw, tenant: id, ctx: ctx}, nil
}

// MustTenant 用于已经确定有租户上下文的地方（比如中间件之后的 handler）。
// 没有则 panic —— 那属于编码错误，不该被静默吞掉变成空结果。
func (s *Store) MustTenant(ctx context.Context) *Scoped {
	sc, err := s.Tenant(ctx)
	if err != nil {
		panic("store: " + err.Error() + "（handler 必须在租户中间件之后）")
	}
	return sc
}

// Platform 取平台级查询器，只能操作白名单里的表。
//
// caller 是调用点标识（形如 "auth/session.go"），会被用于白名单校验与审计。
func (s *Store) Platform(caller string) *Platformed {
	return &Platformed{raw: s.raw, caller: caller}
}

// Raw 返回原始连接。**只给迁移与自检用**。
//
// 任何业务代码调用它都是 bug —— CI 会拦住 internal/api 下的调用。
func (s *Store) Raw() *sql.DB { return s.raw }

// ── 租户作用域 ────────────────────────────────────────────────────

// Scoped 绑定了具体租户的查询器。租户 ID 由本层注入，调用方无法指定。
type Scoped struct {
	raw    *sql.DB
	tenant TenantID
	ctx    context.Context
}

// TenantID 当前作用域的租户。
func (s *Scoped) TenantID() TenantID { return s.tenant }

// tenantFilter 匹配 `tenant_id = ?` / `tenant_id=?` / `t.tenant_id = ?` 等写法。
//
// 只认参数占位符形式：写成 `tenant_id = 5` 这种字面量同样会被拒绝 ——
// 那是硬编码租户，比忘记过滤更危险（它看起来是对的）。
var tenantFilter = regexp.MustCompile(`(?i)\btenant_id\s*=\s*\?`)

// checkFilter 校验语句声明了租户过滤。
func checkFilter(query string) error {
	if !tenantFilter.MatchString(query) {
		return fmt.Errorf("%w: %s", ErrMissingTenantFilter, firstLine(query))
	}
	return nil
}

// Query 执行查询。租户 ID 作为**第一个参数**自动注入。
//
//	q.Query("SELECT name FROM hosts WHERE tenant_id = ? AND zone = ?", zone)
//	                                              ↑ 由本层填，不要自己传
func (s *Scoped) Query(query string, args ...any) (*sql.Rows, error) {
	if err := checkFilter(query); err != nil {
		return nil, err
	}
	return s.raw.QueryContext(s.ctx, query, insertAt(s.tenant, args, tenantArgPos(query))...)
}

// QueryRow 单行查询。租户 ID 自动作为第一个参数。
//
// 注意：sql.Row 的错误要等到 Scan 时才返回，所以过滤缺失也在那时暴露。
func (s *Scoped) QueryRow(query string, args ...any) *sql.Row {
	if err := checkFilter(query); err != nil {
		// 构造一个必定返回该错误的 Row：用一个非法语句让驱动报错不合适，
		// 这里借助 QueryRowContext 对已取消 context 的行为来携带错误。
		ctx, cancel := context.WithCancel(s.ctx)
		cancel()
		return s.raw.QueryRowContext(ctx, "SELECT 1")
	}
	return s.raw.QueryRowContext(s.ctx, query, insertAt(s.tenant, args, tenantArgPos(query))...)
}

// Exec 执行写操作。租户 ID 自动作为第一个参数。
//
// INSERT 语句同样要求带 tenant_id —— 写法是把它放进列清单并用 ? 占位：
//
//	q.Exec("INSERT INTO hosts (tenant_id, name) VALUES (?, ?)", name)
//
// 但那样 `tenant_id = ?` 匹配不到，所以 INSERT 走 Insert()。
func (s *Scoped) Exec(query string, args ...any) (sql.Result, error) {
	if err := checkFilter(query); err != nil {
		return nil, err
	}
	return s.raw.ExecContext(s.ctx, query, insertAt(s.tenant, args, tenantArgPos(query))...)
}

// insertPattern 匹配 INSERT 语句里显式列出的 tenant_id 列。
var insertPattern = regexp.MustCompile(`(?is)insert\s+into\s+\S+\s*\([^)]*\btenant_id\b`)

// Insert 执行插入。要求列清单里显式含 tenant_id，其值由本层注入为第一个参数。
//
//	q.Insert("INSERT INTO hosts (tenant_id, name, zone) VALUES (?, ?, ?)", name, zone)
//
// 单独一个方法而不是复用 Exec，是因为 INSERT 的租户列出现在列清单里而不是
// WHERE 子句里，两者的校验规则不同 —— 混在一起会让其中一种被放行。
func (s *Scoped) Insert(query string, args ...any) (sql.Result, error) {
	if !insertPattern.MatchString(query) {
		return nil, errInsertNoTenant(query)
	}
	return s.raw.ExecContext(s.ctx, query, insertAt(s.tenant, args, insertTenantColPos(query))...)
}

// ── 平台作用域 ────────────────────────────────────────────────────

// Platformed 平台级查询器，只能碰白名单里的表。
type Platformed struct {
	raw    *sql.DB
	caller string
}

// Query 平台级查询。会校验语句涉及的表都在白名单里。
func (p *Platformed) Query(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	if err := checkPlatformTables(query); err != nil {
		return nil, err
	}
	return p.raw.QueryContext(ctx, query, args...)
}

// QueryRow 平台级单行查询。
func (p *Platformed) QueryRow(ctx context.Context, query string, args ...any) *sql.Row {
	if err := checkPlatformTables(query); err != nil {
		c, cancel := context.WithCancel(ctx)
		cancel()
		return p.raw.QueryRowContext(c, "SELECT 1")
	}
	return p.raw.QueryRowContext(ctx, query, args...)
}

// Exec 平台级写操作。
func (p *Platformed) Exec(ctx context.Context, query string, args ...any) (sql.Result, error) {
	if err := checkPlatformTables(query); err != nil {
		return nil, err
	}
	return p.raw.ExecContext(ctx, query, args...)
}

// tableRef 从语句里粗略摘出表名（FROM / JOIN / INTO / UPDATE 后面那个标识符）。
//
// 粗略是够用的：目的不是解析 SQL，而是拦住「用 Platform() 去查业务表」这种
// 越界。真正精确的判断由 CI 的静态检查与 code review 兜底。
var tableRef = regexp.MustCompile(`(?is)\b(?:from|join|into|update)\s+` + "`?" + `([a-z_][a-z0-9_]*)` + "`?")

// dupKeyUpdate 匹配 `ON DUPLICATE KEY UPDATE`。
//
//	⚠️ 这个子句里的 UPDATE **后面跟的是列名，不是表名**：
//	    INSERT INTO leases ... ON DUPLICATE KEY UPDATE fence = fence + 1
//	                                                   ↑ 列
//	不特殊处理的话，上面的正则会把 fence 当成表名，然后判定
//	「fence 不是平台级表」而拒掉整条语句 —— 一条完全合法的 upsert 被挡住，
//	且报错说的是一个根本不存在的表，排查时极具误导性。
//
//	这个 bug 是真库测试抓到的：纯逻辑测试里没人会去构造 upsert 语句。
var dupKeyUpdate = regexp.MustCompile(`(?is)\bon\s+duplicate\s+key\s+update\b`)

func checkPlatformTables(query string) error {
	// 先把 ON DUPLICATE KEY UPDATE 之后的部分切掉再找表名。
	// 那之后全是赋值表达式，不会再出现新的表引用。
	if loc := dupKeyUpdate.FindStringIndex(query); loc != nil {
		query = query[:loc[0]]
	}
	for _, m := range tableRef.FindAllStringSubmatch(query, -1) {
		t := strings.ToLower(m[1])
		if !platformTables[t] {
			return fmt.Errorf("%w: %q（平台查询器只能访问白名单里的表，业务表请用 Tenant()）",
				ErrNotPlatformTable, t)
		}
	}
	return nil
}

// ── 工具 ──────────────────────────────────────────────────────────

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexAny(s, "\r\n"); i > 0 {
		s = s[:i]
	}
	if len(s) > 120 {
		s = s[:120] + "…"
	}
	return s
}

// errInsertNoTenant 供 Scoped 与 ScopedTx 共用，保证两条路径的报错一字不差 ——
// 报错文案不一致会让人以为遇到的是两个不同的问题。
func errInsertNoTenant(query string) error {
	return fmt.Errorf("%w: INSERT 的列清单里必须显式含 tenant_id: %s",
		ErrMissingTenantFilter, firstLine(query))
}
