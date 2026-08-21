// Package approval 是临时提权：申请 → 审批 → 到期自动回收。
//
// # 与 access 包的关系
//
// access 管"常设授权"，本包管"有生命周期的临时授权"。
// 生效中的提权在判定时**动态叠加**成 access.Rule —— 两套判定语义不分叉，
// 否则「他到底能不能进」就会有两个不同的答案。
//
// # 三条产品约束
//
//  1. **默认不给常设权限**：所有提权都有到期时间，没有"永久"选项
//  2. **到期回收必须杀会话**：只改数据库标记的话，人已经拿到的会话还能用到
//     Cookie 过期 —— "临时"二字就是假的
//  3. **SoD 命中即拒绝，运维不可绕过**：能被绕过的职责分离，在审计眼里等于没有
package approval

import (
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
)

type Status string

const (
	StatusPending   Status = "pending"
	StatusApproved  Status = "approved"
	StatusRejected  Status = "rejected"
	StatusBlocked   Status = "blocked" // SoD 拦截，与人工拒绝分开
	StatusExpired   Status = "expired"
	StatusRevoked   Status = "revoked"
	StatusCancelled Status = "cancelled"
)

type Risk string

const (
	RiskLow    Risk = "low"
	RiskMedium Risk = "medium"
	RiskHigh   Risk = "high"
)

// MaxDuration 单次提权的上限。
//
// 24 小时。再长就不是"临时"了 —— 需要更久的应当走常设授权 + 定期复核，
// 那条路上有复核机制，而临时提权没有。
const MaxDuration = 24 * time.Hour

// SelfApprovalWindow 超过这个时长必须二级审批。
const TwoStepThreshold = 8 * time.Hour

// MinReasonRunes 理由的最少字符数。数字符不数字节 —— 见 Validate。
const MinReasonRunes = 5

var (
	ErrInvalidRequest = errors.New("approval: 申请不合法")
	ErrSelfApproval   = errors.New("approval: 不能审批自己的申请")
	ErrNotPending     = errors.New("approval: 该申请已经处理过了")
	ErrSoDBlocked     = errors.New("approval: 职责分离规则拦截")
)

// Request 一次提权申请。
type Request struct {
	ID          int64
	TenantID    int64
	RequesterID int64
	AppID       int64
	Scope       string
	Reason      string
	TicketRef   string
	Duration    time.Duration

	Status Status
	Risk   Risk

	BlockedRule string
	BlockedNote string

	ApproverID   int64
	ApproverNote string
	DecidedAt    *time.Time

	GrantedAt *time.Time
	ExpiresAt *time.Time
	RevokedAt *time.Time

	CreatedAt time.Time
}

// Validate 申请自身的合法性。
func (r Request) Validate() error {
	if r.AppID == 0 {
		return fmt.Errorf("%w: 必须指定应用", ErrInvalidRequest)
	}
	// ★ 数**字符**不是数字节。
	//
	// 用 len() 的话，"见工单"（3 个字、9 字节）能过，而 "abcd"（4 个字、4 字节）被拒 ——
	// 同样敷衍，判定却相反，且对中文用户格外宽松。这是本项目里
	// 任何"最少几个字"的校验都必须走 RuneCount 的原因。
	if utf8.RuneCountInString(strings.TrimSpace(r.Reason)) < MinReasonRunes {
		// 理由是审批人唯一能看的东西。允许写"1"的话，
		// 三个月后复核时没有任何人能回答"当时为什么给他开"
		return fmt.Errorf("%w: 必须写清为什么需要（至少 %d 个字）", ErrInvalidRequest, MinReasonRunes)
	}
	if r.Duration <= 0 {
		return fmt.Errorf("%w: 必须指定时长", ErrInvalidRequest)
	}
	if r.Duration > MaxDuration {
		return fmt.Errorf("%w: 单次最长 %v，更久的需求应走常设授权 + 定期复核",
			ErrInvalidRequest, MaxDuration)
	}
	if r.Scope != "" && !strings.HasPrefix(r.Scope, "/") {
		return fmt.Errorf("%w: 作用域必须是以 / 开头的路径前缀", ErrInvalidRequest)
	}
	return nil
}

// NeedsTwoStep 是否需要二级审批。
func (r Request) NeedsTwoStep() bool { return r.Duration > TwoStepThreshold }

// Active 此刻是否处于生效中。
func (r Request) Active(now time.Time) bool {
	if r.Status != StatusApproved || r.ExpiresAt == nil || r.RevokedAt != nil {
		return false
	}
	return now.Before(*r.ExpiresAt)
}

// Covers 这次提权是否覆盖某个路径。
//
// 与 mfa.Ticket.Covers 同一套语义：前缀必须在路径分隔处对齐。
// 两处用不同规则的话，"我明明申请过"就会变成一个查不清的投诉。
func (r Request) Covers(path string) bool {
	if r.Scope == "" {
		return true
	}
	if path == r.Scope {
		return true
	}
	return strings.HasPrefix(path, strings.TrimSuffix(r.Scope, "/")+"/")
}

// ── 职责分离 ────────────────────────────────────────────────────

// SoDRule 两个能力互斥。
type SoDRule struct {
	ID          int64
	Code        string
	Name        string
	CapabilityA string
	CapabilityB string
	Note        string
	Enabled     bool
}

// Conflict SoD 检查结果。
type Conflict struct {
	Blocked bool
	Rule    SoDRule
	// HeldVia 他已经通过什么持有了冲突的另一半 —— 拒绝时必须说清楚，
	// 否则申请人只知道"不行"，不知道怎么才能行
	HeldVia string
}

// CheckSoD 判断给某人授予 wantCaps 会不会违反职责分离。
//
// heldCaps 是他**当前已持有**的能力（含常设授权与生效中的临时提权）。
func CheckSoD(rules []SoDRule, heldCaps map[string]string, wantCaps []string) Conflict {
	for _, rule := range rules {
		if !rule.Enabled {
			continue
		}
		for _, want := range wantCaps {
			var other string
			switch want {
			case rule.CapabilityA:
				other = rule.CapabilityB
			case rule.CapabilityB:
				other = rule.CapabilityA
			default:
				continue
			}
			if via, held := heldCaps[other]; held {
				return Conflict{Blocked: true, Rule: rule, HeldVia: via}
			}
		}
	}
	return Conflict{}
}

// Decide 审批。
//
// approverID 与申请人相同时直接拒绝 —— 自己批自己是 SoD 的最基本形态，
// 这条不该依赖配置，写死在代码里。
func (r *Request) Decide(approverID int64, approve bool, note string, now time.Time) error {
	if r.Status != StatusPending {
		return ErrNotPending
	}
	if approverID == r.RequesterID {
		return ErrSelfApproval
	}
	r.ApproverID = approverID
	r.ApproverNote = note
	r.DecidedAt = &now
	if !approve {
		r.Status = StatusRejected
		return nil
	}
	r.Status = StatusApproved
	exp := now.Add(r.Duration)
	r.GrantedAt = &now
	r.ExpiresAt = &exp
	return nil
}

// Block 被 SoD 拦下。
//
// 单独一个状态而不是复用 rejected：界面上要能一眼分出
// 「人不同意」和「制度不允许」——两者的下一步动作完全不同。
func (r *Request) Block(c Conflict, now time.Time) {
	r.Status = StatusBlocked
	r.BlockedRule = c.Rule.Code
	r.BlockedNote = fmt.Sprintf("与 %s 冲突：已通过「%s」持有 %s",
		c.Rule.Name, c.HeldVia, conflictOther(c))
	r.DecidedAt = &now
}

func conflictOther(c Conflict) string {
	return c.Rule.CapabilityA + " / " + c.Rule.CapabilityB
}

// RiskOf 按环境与作用域估风险等级。
//
// 只是给审批人排序用，**不参与判定** —— 风险高低不该影响能不能批，
// 那是人的决定。
func RiskOf(env, scope string, duration time.Duration) Risk {
	prod := strings.EqualFold(env, "PROD")
	wide := scope == ""
	switch {
	case prod && wide:
		return RiskHigh
	case prod || (wide && duration > TwoStepThreshold):
		return RiskMedium
	default:
		return RiskLow
	}
}
