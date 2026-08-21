// Package access 是访问授权的领域层：谁能用哪个应用。
//
// 这一层**不依赖 HTTP，也不依赖数据库** —— 全部是纯函数与值类型，
// 因为授权判定是整个产品最不能出错的地方，必须能被穷举测试。
//
// # 三层作用域
//
//	global  全局：一条规则管所有应用
//	group   应用分组：一条规则管一个自定义分组里的所有应用
//	app     单个应用
//
// # 主体
//
//	public  所有已认证用户
//	dept    部门（按子树匹配，越深越具体）
//	role    角色
//	group   用户组
//	user    个人
//
// # 判定顺序（这是整个产品语义上最关键的一段）
//
//  1. 任何一条**强制拒绝**命中 → 拒绝。这是安全负责人的硬保证，下层改不动。
//  2. 否则在所有命中的规则里排序取第一条：
//     ① 主体越具体越优先（user > group > role > dept > public）
//     ② 主体具体度相同时，作用域越具体越优先（app > group > global）
//     ③ 前两项都相同时，deny 优先于 allow
//  3. 一条都没命中 → 拒绝（默认拒绝，不是默认放行）
//
// # 为什么是「先比主体，再比作用域」，而不是反过来
//
// 反过来会出这种事：安全上点名「张三全局禁止访问」，却被一条粗粒度的
// 「研发组可访问 Jira」覆盖掉 —— 点名禁某个人反而不如按组授权管用。
// 先比主体就不会：
//
//	全局 · 张三 deny   +  应用 · 研发组 allow   → 拒绝（点名的赢）
//	全局 · 研发组 deny +  应用 · 研发组 allow   → 放行（同主体，应用级赢）← 「单独配置优先全局」
//	全局 · 全员 allow  +  应用 · 外包组 deny    → 拒绝（外包组比全员具体）
//
// 第二行就是「单独配置优先于全局」的准确含义：**同一个主体**在更具体的
// 作用域上被重新配置时，以更具体的为准。
package access

import (
	"errors"
	"fmt"
)

// Effect 规则的判定结果。
type Effect string

const (
	Allow Effect = "allow"
	Deny  Effect = "deny"
)

// Scope 作用域，从粗到细。
type Scope string

const (
	ScopeGlobal Scope = "global" // 所有应用
	ScopeGroup  Scope = "group"  // 某个应用分组下的所有应用
	ScopeApp    Scope = "app"    // 单个应用
)

// SubjectType 规则作用的主体类型。
type SubjectType string

const (
	SubjectPublic SubjectType = "public" // 所有已认证用户，无 SubjectID
	SubjectDept   SubjectType = "dept"   // 部门，按子树匹配
	SubjectRole   SubjectType = "role"
	SubjectGroup  SubjectType = "group" // 用户组
	SubjectUser   SubjectType = "user"
)

// 主体具体度。数越大越具体，排序时优先。
//
// 为什么用户组（group）比角色（role）具体：角色是「一类身份」，通常按岗位批量套用；
// 用户组是「人为圈定的一批人」，圈的时候就想清楚了要给谁。真实场景里，
// 「研发」角色有几百人，「支付组核心成员」用户组只有 6 个人。
//
// 部门（dept）的具体度按树深度递增：`研发中心/后端组` 比 `研发中心` 具体。
// 基数 100、步长 1，所以部门树深到 99 层才会撞上 role —— 现实里不会。
const (
	rankPublic   = 0
	rankDeptBase = 100
	rankRole     = 200
	rankGroup    = 300
	rankUser     = 400
)

// 作用域具体度。
const (
	rankScopeGlobal = 0
	rankScopeGroup  = 1
	rankScopeApp    = 2
)

// Rule 一条授权规则。
type Rule struct {
	ID      int64
	Scope   Scope
	ScopeID int64 // group→分组 ID，app→应用 ID，global→0

	SubjectType SubjectType
	SubjectID   int64 // public 时为 0

	Effect Effect

	// Enforced 强制：本条不可被任何更具体的规则覆盖。
	//
	// **只对 deny 生效**。不做「强制 allow」是刻意的：那等于在系统里造一个
	// 谁也关不掉的后门，安全评审第一轮就会被挑出来。想让某人一定进得去，
	// 应该是「没有任何 deny 命中他」，而不是「有一条谁也删不掉的 allow」。
	Enforced bool

	// Note 为什么加这条规则。
	//
	// ⚠️ **不参与判定** —— 纯粹是给人看的。放在这里是因为它随规则一起查出来、
	// 一起展示；Evaluate 不读它，加它不改变任何判定行为。
	//
	// 界面上是必填项：半年后复核时，这一栏是唯一能看的东西。
	// 没有它，规则表就是一堆谁也不敢删的历史遗留。
	Note string
}

// Validate 规则自身的合法性。写库前必须过这一关。
func (r Rule) Validate() error {
	switch r.Scope {
	case ScopeGlobal:
		if r.ScopeID != 0 {
			return fmt.Errorf("%w: global 作用域不能带 scope_id", ErrInvalidRule)
		}
	case ScopeGroup, ScopeApp:
		if r.ScopeID == 0 {
			return fmt.Errorf("%w: %s 作用域必须带 scope_id", ErrInvalidRule, r.Scope)
		}
	default:
		return fmt.Errorf("%w: 未知作用域 %q", ErrInvalidRule, r.Scope)
	}

	switch r.SubjectType {
	case SubjectPublic:
		if r.SubjectID != 0 {
			return fmt.Errorf("%w: public 主体不能带 subject_id", ErrInvalidRule)
		}
	case SubjectDept, SubjectRole, SubjectGroup, SubjectUser:
		if r.SubjectID == 0 {
			return fmt.Errorf("%w: %s 主体必须带 subject_id", ErrInvalidRule, r.SubjectType)
		}
	default:
		return fmt.Errorf("%w: 未知主体类型 %q", ErrInvalidRule, r.SubjectType)
	}

	switch r.Effect {
	case Allow:
		if r.Enforced {
			return fmt.Errorf("%w: 只有拒绝可以设为强制，强制放行等于不可关闭的后门", ErrEnforcedAllow)
		}
	case Deny:
	default:
		return fmt.Errorf("%w: 未知 effect %q", ErrInvalidRule, r.Effect)
	}
	return nil
}

var (
	// ErrInvalidRule 规则本身不合法。
	ErrInvalidRule = errors.New("access: 规则不合法")
	// ErrEnforcedAllow 试图创建「强制放行」。
	ErrEnforcedAllow = errors.New("access: 不支持强制放行")
)

// Subject 求值时的主体：一个已认证的人（或服务账号）。
//
// DeptDepth 是「部门 ID → 该部门在树中的深度」，**必须含祖先链** ——
// 用户在 `研发中心/后端组/支付组` 时，三个 ID 都要在这个 map 里，
// 这样一条挂在 `研发中心` 上的规则才能按子树命中他。
// 深度从 1 开始（根部门 = 1）。
type Subject struct {
	UserID    int64
	RoleIDs   map[int64]bool
	GroupIDs  map[int64]bool
	DeptDepth map[int64]int
}

// App 被访问的应用，以及它属于哪些自定义分组（可多归属）。
type App struct {
	ID       int64
	GroupIDs []int64
}

// Outcome 一条候选规则在本次判定里的下场。给「生效结果试算」界面用。
type Outcome string

const (
	OutcomeWon        Outcome = "won"         // 就是它定的
	OutcomeEnforced   Outcome = "enforced"    // 强制拒绝，短路了后面的一切
	OutcomeShadowed   Outcome = "shadowed"    // 被更具体的规则盖住
	OutcomeLostToDeny Outcome = "lost_to_den" // 同等具体度下输给了 deny
)

// Step 判定轨迹里的一条。
//
// **不含任何中文** —— 后端只给码与参数，界面负责翻译。
// 这条纪律的代价是这里看起来啰嗦，收益是英文版不用回来改后端。
type Step struct {
	Rule         Rule
	SubjectRank  int
	ScopeRank    int
	Outcome      Outcome
	MatchedDepth int // 仅 dept 主体：按哪一级部门命中的
}

// Reason 判定结论的原因码。
type Reason string

const (
	ReasonEnforcedDeny Reason = "enforced_deny"    // 命中强制拒绝
	ReasonRule         Reason = "rule"             // 按优先级选出的规则
	ReasonDefaultDeny  Reason = "default_deny"     // 一条都没命中
	ReasonAppNotFound  Reason = "app_not_found"    // 应用不存在或已删除
	ReasonNoSubject    Reason = "subject_required" // 未认证
)

// Decision 一次判定的完整结果。
type Decision struct {
	Effect Effect
	Reason Reason
	Rule   *Rule  // 定下这个结果的规则；默认拒绝时为 nil
	Trace  []Step // 所有命中主体的候选规则及其下场，按优先级降序
}

// Allowed 是否放行。
func (d Decision) Allowed() bool { return d.Effect == Allow }
