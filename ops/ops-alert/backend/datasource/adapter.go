// Package datasource 是数据源适配层。
//
// # 为什么要有这一层
//
// 上一代把 *lark.Sender 和 *es.Client 直接写进检测引擎的函数签名，
// 换个日志系统或换个 IM 都要改引擎。这里的约定是：
//
//	引擎只认 Adapter 接口，永远不知道对面是 Loki 还是 Elasticsearch。
//
// 新增一种数据源 = 实现 Probe + Query 两个方法 + 在 New 里注册一行，引擎不动。
// 二期的 Prometheus / VictoriaMetrics 走同一个接口：
// 指标样本填进 Result.Hits（Line 留空、Value 有值），判定逻辑复用。
package datasource

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"
)

// 一期实现的类型。二期在这里加常量并在 New 注册。
const (
	KindLoki       = "loki"
	KindES         = "elasticsearch"
	KindOpenSearch = "opensearch"
)

// Auth 是各类型共用的认证信息，整体加密后落库，永不回显。
type Auth struct {
	Username string `json:"username,omitempty"`
	Password string `json:"password,omitempty"`
	APIKey   string `json:"api_key,omitempty"`
	Token    string `json:"token,omitempty"`
	OrgID    string `json:"org_id,omitempty"` // Loki 多租户头 X-Scope-OrgID
}

// Spec 是类型特有配置（存 datasources.spec）。
type Spec struct {
	Version      string `json:"version,omitempty"`       // ES 大版本："7" / "8"
	IndexPattern string `json:"index_pattern,omitempty"` // ES 索引模式
	TimeField    string `json:"time_field,omitempty"`    // ES 时间字段，默认 @timestamp
}

// Config 由上层从 datasources 行解密组装。
type Config struct {
	ID       int64
	Name     string
	Kind     string
	Endpoint string
	SkipTLS  bool
	Auth     Auth
	Spec     Spec
}

// Query 是一次检测查询。
//
// Expr 的含义随类型而变（Loki 是 LogQL，ES 是查询串或 DSL），
// 这是刻意的：把各家查询语言硬抹平成一套 DSL 会丢掉表达力，
// 而运维本来就熟悉自己那套语法。
type Query struct {
	Expr    string
	From    time.Time
	To      time.Time
	Limit   int
	GroupBy []string // 分组维度（标签名 / 字段名）
}

// Hit 是一条命中。日志类填 Line/Labels/Fields；指标类（二期）填 Value。
type Hit struct {
	Time   time.Time         `json:"time"`
	Line   string            `json:"line,omitempty"`
	Value  float64           `json:"value,omitempty"`
	Labels map[string]string `json:"labels,omitempty"`
	Fields map[string]any    `json:"fields,omitempty"`
}

// Result 是一次查询的结果。
type Result struct {
	Hits   []Hit
	Total  int64
	Groups map[string]int // 分组键 → 命中数
	TookMs int
	// Truncated 表示结果被 Limit 截断。
	// 判定时必须知道这件事：命中 1000 条（截断）与命中 1000 条（正好）
	// 对「阈值 ≥ 500」是同一个结论，但对「日志量突变」的比例计算不是。
	Truncated bool
}

// Adapter 是所有数据源的统一接口。
type Adapter interface {
	Kind() string
	// Probe 检查连通性与认证。失败信息会落库并显示在界面上——
	// 数据源不可达必须是显性状态，不能表现为「这里一直是空的」。
	Probe(ctx context.Context) error
	Query(ctx context.Context, q Query) (*Result, error)
}

// New 按类型构造适配器。类型不认识时返回错误而不是 nil：
// 静默返回 nil 会让调用方在下一行 panic，堆栈里看不出真正原因。
func New(cfg Config) (Adapter, error) {
	switch cfg.Kind {
	case KindLoki:
		return newLoki(cfg), nil
	case KindES, KindOpenSearch:
		// OpenSearch 是 ES 7 的分支，查询接口兼容，共用一个适配器。
		return newElastic(cfg), nil
	default:
		// 二期类型（prometheus / victoriametrics / clickhouse / mysql）会在这里注册。
		return nil, fmt.Errorf("数据源类型 %q 尚未实现（一期只支持 loki / elasticsearch / opensearch）", cfg.Kind)
	}
}

// GroupKey 把一组标签值拼成分组键。
//
// 用 \x1f（单元分隔符）而不是常见的 "-" 或 "/"：标签值里出现分隔符会让
// 两个不同的分组拼出同一个键，两条独立告警就此合并成一条，
// 而这种错在数据量小的时候根本不会出现。
const groupSep = "\x1f"

func GroupKey(labels map[string]string, by []string) string {
	if len(by) == 0 {
		return ""
	}
	parts := make([]string, 0, len(by))
	for _, k := range by {
		parts = append(parts, labels[k])
	}
	return strings.Join(parts, groupSep)
}

// SplitGroupKey 还原分组键，供界面展示。
func SplitGroupKey(key string) []string {
	if key == "" {
		return nil
	}
	return strings.Split(key, groupSep)
}

// CountByGroup 按分组统计命中数。
func CountByGroup(hits []Hit, by []string) map[string]int {
	out := map[string]int{}
	for _, h := range hits {
		out[GroupKey(h.Labels, by)]++
	}
	return out
}

// SortedGroups 返回按命中数降序的分组键，保证展示顺序稳定
// （map 遍历顺序随机，会让同样的数据每次刷新排出不同的顺序）。
func SortedGroups(groups map[string]int) []string {
	keys := make([]string, 0, len(groups))
	for k := range groups {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		if groups[keys[i]] != groups[keys[j]] {
			return groups[keys[i]] > groups[keys[j]]
		}
		return keys[i] < keys[j]
	})
	return keys
}
