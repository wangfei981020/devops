// Package httpx 提供列表接口的统一约定：分页、筛选、排序、facets。
//
// 存在的理由是它必须只有一份。260 个路由如果各写各的分页，参数名会漂
// （page/pageNo/pageNum），边界处理会不一致（size=0 是全量还是空），
// 前端就得为每个接口记一套规则。
package httpx

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
)

const (
	DefaultSize = 50
	MaxSize     = 200
)

// PageQuery 是所有列表接口的入参约定。
type PageQuery struct {
	Page int
	Size int

	// SortBy 是**前端字段名**，不是 SQL 列名。转换在 OrderBy 里做。
	SortBy   string
	SortDesc bool

	// Keyword 模糊搜索词，具体搜哪些列由各 handler 决定
	Keyword string

	// Filters 精确匹配的筛选项，如 status=running、cluster=g32-prod
	Filters map[string]string
}

// BindPage 从 query string 解析分页参数。
//
// 刻意不返回 error：分页参数写错（page=abc）应该退回默认值继续查，
// 而不是让整个列表报错。用户看到的是"翻页没生效"，比看到一个 400 强。
func BindPage(c *gin.Context, filterKeys ...string) PageQuery {
	q := PageQuery{
		Page:    1,
		Size:    DefaultSize,
		Keyword: strings.TrimSpace(c.Query("q")),
		Filters: map[string]string{},
	}

	if v, err := strconv.Atoi(c.Query("page")); err == nil && v > 0 {
		q.Page = v
	}
	if v, err := strconv.Atoi(c.Query("size")); err == nil && v > 0 {
		// 上限是防御性的：没有它，一个 size=1000000 的请求就能把内存打满。
		// 客户端拿不到自己要的条数，但服务不会倒。
		q.Size = min(v, MaxSize)
	}

	// sort=-cost 表示按 cost 降序，sort=name 表示升序。
	// 用前缀而不是额外的 order 参数：少一个参数就少一处前后端能对不上的地方。
	if s := strings.TrimSpace(c.Query("sort")); s != "" {
		q.SortDesc = strings.HasPrefix(s, "-")
		q.SortBy = strings.TrimPrefix(s, "-")
	}

	// 只接受声明过的筛选键，未声明的一律忽略。
	// 否则前端拼错参数名（cluter=xxx）会静默失效，而界面看起来筛选生效了。
	for _, k := range filterKeys {
		if v := strings.TrimSpace(c.Query(k)); v != "" && v != "all" {
			q.Filters[k] = v
		}
	}
	return q
}

func (q PageQuery) Offset() int { return (q.Page - 1) * q.Size }

// OrderBy 把前端字段名翻译成 SQL 片段。
//
// ⚠️ allowed 是白名单，**不在白名单里的一律用 fallback**。
// 直接把 q.SortBy 拼进 SQL 就是注入漏洞：sort=name;DROP TABLE hosts--
// 白名单让"能排哪些列"成为服务端的决定，客户端只能从中挑。
func (q PageQuery) OrderBy(allowed map[string]string, fallback string) string {
	col, ok := allowed[q.SortBy]
	if !ok || col == "" {
		return fallback
	}
	if q.SortDesc {
		return col + " DESC"
	}
	return col + " ASC"
}

// ListResponse 是所有列表接口的出参约定。
type ListResponse[T any] struct {
	// ⚠️ 绝不能是 nil。nil 切片序列化成 JSON null，
	// 前端 data.items.length 直接抛异常 —— 而这只在"结果为空"时发生，
	// 有数据的时候一切正常，最容易漏测。NewList 保证了这一点。
	Items []T `json:"items"`

	Page  int   `json:"page"`
	Size  int   `json:"size"`
	Total int64 `json:"total"`

	// Facets 每个筛选维度下各取值的**全量**计数，如 {"status":{"running":347}}。
	//
	// 必须按全量统计，不能按当前页。筛选下拉要显示"运行中 347"，
	// 若只统计当前页，那个数字就是错的 —— 而它看起来完全正常，没人会怀疑。
	Facets map[string]map[string]int64 `json:"facets,omitempty"`

	// Freshness 这批数据"多久没更新就算旧了"的判据。
	//
	//	🔴 判据必须由**后端**给：它来自那个采集任务自己的 cron 周期。
	//	前端写死一个"6 小时"，任务周期一改就分叉，而分叉的表现是
	//	页面安静地不报警（OPSCMDB-031 P1-5：主机页写着「17 小时前」，
	//	既没标红，又和数据源页的「从没同步过」互相矛盾，三处三个说法）。
	Freshness *Freshness `json:"freshness,omitempty"`

	// Caveat 这批数据的**已知局限**：某一列为什么整片是空的。
	//
	//	🔴 起因是一次生产 P0：证书列表里 890 张证书有 828 张到期日为空，
	//	页面上没有任何地方说为什么 —— 而真相是"证书到期检测（443）"这个
	//	定时任务被停用且从没跑过，也就是**证书临期提醒根本不工作**。
	//	看的人只会以为"数据还没采到"，不会想到去开一个任务。
	//	（那两张 8 月 21 日到期的生产网关证书就是这么活到剩 28 小时的。）
	//
	//	⚠️ 与 Freshness 的区别：Freshness 说"这批数据旧了"，
	//	Caveat 说"这批数据里有一整类信息压根不存在，以及为什么"。
	//	两者都不能靠前端猜 —— 前端只看得见空值，看不见空值的来历。
	Caveat *Caveat `json:"caveat,omitempty"`
}

// Caveat 一批数据的已知局限。
type Caveat struct {
	// Kind 机器可读的局限类型，前端据此决定色调（never 用 bad，partial 用 warn）
	Kind string `json:"kind"`
	// NoteKey / NoteParams 前端语言包的 key 和插值参数。
	//
	// ⚠️ 必须能说出"去哪儿做什么"。只说"数据缺失"等于告诉人有问题但不给出路。
	// ⚠️ 不发拼好的句子：后端拼的中文在英文界面上永远是中文（OPSCMDB-054）。
	NoteKey    string         `json:"note_key"`
	NoteParams map[string]any `json:"note_params,omitempty"`
}

// WithCaveat 挂上已知局限。kind 为空或 ok 时不挂（没有局限就不要造一条）。
func (r ListResponse[T]) WithCaveat(kind, noteKey string, params map[string]any) ListResponse[T] {
	if kind == "" || kind == "ok" || noteKey == "" {
		return r
	}
	r.Caveat = &Caveat{Kind: kind, NoteKey: noteKey, NoteParams: params}
	return r
}

// Freshness 一批数据的过期判据。
type Freshness struct {
	// TaskKey 负责刷新这批数据的定时任务
	TaskKey string `json:"task_key"`
	// StaleAfterSeconds 超过这个时长没更新就算旧了。Known=false 时无意义
	StaleAfterSeconds int64 `json:"stale_after_seconds"`
	// Known 这次有没有取到该任务的周期。
	//
	//	⚠️ false 时前端**不能**下"数据是旧的"这个结论 ——
	//	取不到周期和"数据很新"是两件事，压成同一种表现就又回到
	//	"看起来一切正常"。
	Known bool `json:"known"`
}

func NewList[T any](items []T, q PageQuery, total int64) ListResponse[T] {
	if items == nil {
		items = []T{}
	}
	return ListResponse[T]{Items: items, Page: q.Page, Size: q.Size, Total: total}
}

// WithFreshness 挂上过期判据。
func (r ListResponse[T]) WithFreshness(f *Freshness) ListResponse[T] {
	r.Freshness = f
	return r
}

// WithFacets 挂上分面计数。
func (r ListResponse[T]) WithFacets(f map[string]map[string]int64) ListResponse[T] {
	r.Facets = f
	return r
}

// LimitClause 返回 "LIMIT ? OFFSET ?" 用的两个值。
// 单独成函数是为了让调用方不会写反顺序 —— 写反了不报错，只是翻页结果诡异。
func (q PageQuery) LimitClause() (limit, offset int) {
	return q.Size, q.Offset()
}

// WhereBuilder 拼 WHERE 子句，参数一律走占位符。
//
// 用它而不是 fmt.Sprintf 拼条件：拼字符串迟早会有人把用户输入拼进去。
type WhereBuilder struct {
	conds []string
	args  []any
}

func (w *WhereBuilder) Add(cond string, args ...any) *WhereBuilder {
	w.conds = append(w.conds, cond)
	w.args = append(w.args, args...)
	return w
}

// AddIf 条件为真时才加，省掉调用方一堆 if。
func (w *WhereBuilder) AddIf(ok bool, cond string, args ...any) *WhereBuilder {
	if ok {
		w.Add(cond, args...)
	}
	return w
}

// EscapeLike 转义 LIKE 的通配符并包上 %。
//
// 不转义的话，用户搜 "50%" 里的 % 会被当成通配符，
// 结果是"搜出一堆不相关的"，而没人会想到是转义问题。
func EscapeLike(keyword string) string {
	esc := strings.NewReplacer("\\", "\\\\", "%", "\\%", "_", "\\_").Replace(keyword)
	return "%" + esc + "%"
}

// Like 单列模糊匹配。表达式里有多个占位符时用 Add + EscapeLike，
// 别指望这个函数会把参数复制多份 —— 那种隐式行为迟早对不上。
func (w *WhereBuilder) Like(expr, keyword string) *WhereBuilder {
	if keyword == "" {
		return w
	}
	return w.Add(expr, EscapeLike(keyword))
}

func (w *WhereBuilder) SQL() string {
	if len(w.conds) == 0 {
		return ""
	}
	return " WHERE " + strings.Join(w.conds, " AND ")
}

func (w *WhereBuilder) Args() []any { return w.args }

// Clone 复制一份，用于在同一批条件上派生出 count 查询。
func (w *WhereBuilder) Clone() *WhereBuilder {
	c := &WhereBuilder{conds: make([]string, len(w.conds)), args: make([]any, len(w.args))}
	copy(c.conds, w.conds)
	copy(c.args, w.args)
	return c
}

// FacetSQL 生成 "SELECT <col>, COUNT(*) FROM ... GROUP BY <col>" 的列部分。
// col 必须来自调用方的常量，不接受用户输入。
func FacetSQL(table, col, where string) string {
	return fmt.Sprintf("SELECT %s, COUNT(*) FROM %s%s GROUP BY %s", col, table, where, col)
}
