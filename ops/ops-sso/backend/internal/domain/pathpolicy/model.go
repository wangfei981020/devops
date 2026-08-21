// Package pathpolicy 是网关的细粒度判定：某个人对某个应用的**某个接口**能不能动手。
//
// 与 access 包的分工：
//   - access    回答「他能不能用这个应用」（门户是否显示、能不能建立会话）
//   - pathpolicy 回答「他能不能调这个接口」（读放行、写要二次验证、删要工单）
//
// # 为什么没有「顺序」
//
// 原型里画的是「自上而下首次命中生效」的有序列表，六角色验证时测试提了一个 P0：
// **顺序即语义**。把「设备未纳管 → 拒绝」从第 4 条拖到第 1 条，全公司服务账号
// 瞬间被拒（服务账号没有「设备」这个概念）。当时只有保存前预演，没有调序预演。
//
// 与其再补一个调序预演，不如让顺序**不再是语义的一部分**：按具体度判定。
//
//	路径越长越具体：/api/v2/projects/pay/** 比 /api/v2/** 具体
//	方法指定的比通配的具体：POST 比 * 具体
//	带条件的比不带的具体：限了设备/时间/来源的，比没限的具体
//	主体越具体越优先：与 access 包同一套排序（user > group > role > dept > public）
//	同具体度时：deny > challenge > allow
//
// 于是规则怎么排都不影响结果，「拖一下就出事」这个类别的事故被设计掉了。
//
// 代价：不能再写「先放行 A，再拒绝 A 的子集」这种依赖顺序的技巧。
// 但那种写法本来就是事故来源 —— 用更具体的规则表达同样的意图更清楚，也更可复核。
package pathpolicy

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

// Decision 网关的三态判定。
type Decision string

const (
	Allow     Decision = "allow"     // 放行
	Challenge Decision = "challenge" // 需二次验证（含需工单）
	Deny      Decision = "deny"      // 拒绝
)

// severity 越大越"严"。同具体度时取最严的：deny > challenge > allow。
func severity(d Decision) int {
	switch d {
	case Deny:
		return 3
	case Challenge:
		return 2
	default:
		return 1
	}
}

// SubjectType 与 access 包保持同一套取值 —— 同一个概念在两处不同名，
// 是这类系统里最容易长出 bug 的地方。
type SubjectType string

const (
	SubjectPublic SubjectType = "public"
	SubjectDept   SubjectType = "dept"
	SubjectRole   SubjectType = "role"
	SubjectGroup  SubjectType = "group"
	SubjectUser   SubjectType = "user"
)

// Rule 一条路径级规则。
type Rule struct {
	ID          int64
	AppID       int64
	Methods     string // "*" 或 "POST,PUT"
	PathPattern string // 前缀匹配，"/**" 结尾表示子树
	SubjectType SubjectType
	SubjectID   int64
	Decision    Decision

	RequireTicket bool
	DeviceState   string // "" 不限 / managed / unmanaged
	TimeWindow    string // "" 不限 / "08:00-22:00"
	SourceKind    string // "" 不限 / office / vpn / internet
	MFATTLSec     int
}

// Request 一次待判定的访问。
type Request struct {
	AppID       int64
	Method      string
	Path        string
	DeviceState string // managed / unmanaged / ""（服务账号没有设备）
	SourceKind  string // office / vpn / internet
	HasTicket   bool
	At          time.Time

	// 主体事实，与 access.Subject 同构。这里不直接引用 access.Subject，
	// 是为了让两个包各自可以独立演进 —— 转换在调用方做一次。
	UserID    int64
	RoleIDs   map[int64]bool
	GroupIDs  map[int64]bool
	DeptDepth map[int64]int
}

// Reason 判定原因码。给审计与界面用，**不含中文**。
type Reason string

const (
	ReasonRule        Reason = "path_rule"
	ReasonDefaultDeny Reason = "path_default_deny"
	ReasonNoTicket    Reason = "ticket_required"
)

// Result 判定结果。
type Result struct {
	Decision Decision
	Reason   Reason
	Rule     *Rule
	MFATTL   int
	// Candidates 命中的规则按具体度降序，给「为什么是这个结果」用
	Candidates []Scored
}

// Scored 一条候选规则及其具体度打分。
type Scored struct {
	Rule     Rule
	PathRank int
	SubjRank int
	CondRank int
	Won      bool
}

var ErrInvalidRule = errors.New("pathpolicy: 规则不合法")

// Validate 规则自身的合法性。
func (r Rule) Validate() error {
	if r.AppID == 0 {
		return fmt.Errorf("%w: 必须绑定应用", ErrInvalidRule)
	}
	if !strings.HasPrefix(r.PathPattern, "/") {
		return fmt.Errorf("%w: 路径必须以 / 开头", ErrInvalidRule)
	}
	switch r.Decision {
	case Allow, Challenge, Deny:
	default:
		return fmt.Errorf("%w: 未知判定 %q", ErrInvalidRule, r.Decision)
	}
	if r.TimeWindow != "" {
		if _, _, err := parseWindow(r.TimeWindow); err != nil {
			return fmt.Errorf("%w: 时间窗口 %q 不合法", ErrInvalidRule, r.TimeWindow)
		}
	}
	switch r.SubjectType {
	case SubjectPublic:
		if r.SubjectID != 0 {
			return fmt.Errorf("%w: public 不能带 subject_id", ErrInvalidRule)
		}
	case SubjectDept, SubjectRole, SubjectGroup, SubjectUser:
		if r.SubjectID == 0 {
			return fmt.Errorf("%w: %s 必须带 subject_id", ErrInvalidRule, r.SubjectType)
		}
	default:
		return fmt.Errorf("%w: 未知主体类型 %q", ErrInvalidRule, r.SubjectType)
	}
	return nil
}
