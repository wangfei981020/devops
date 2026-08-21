package diag

import (
	"fmt"
	"strings"
	"sync"
	"time"
)

// ─────────────────────────────────────────────────────────────────
// Layer 2：AI 兜底的**准入判定**与**成本闸门**。
//
// 这个文件只管"能不能调"和"调了要记什么"，不含任何 HTTP 调用 ——
// 真正的调用在 handlers/diag_ai.go。分开是因为准入规则要能脱离网络测试。
//
// # 分层原则（OPSCMDB-041 定的，别绕过）
//
//	Layer 0   规则判定        免费
//	Layer 1   证据提炼        免费（把日志里真实存在的报错行摆出来）
//	Layer 1.5 CMDB 历史事件   免费
//	Layer 2   AI              **花钱，最后一档**
//
// 🔴 用户原话：「能直接解决的，就解决；必须要通过 AI 的，那你就要给一个
// 详细的说明，并且记录了」。所以这一层的每一次调用都必须能回答三个问题：
//
//	1. 为什么前面几层都没结论（不是"没试"，是"试了、结论是这个"）
//	2. 喂给模型的到底是什么（原文，不是摘要）
//	3. 花了多少钱
//
// 缺任何一条，这次调用就不该发生。
// ─────────────────────────────────────────────────────────────────

// AIGateDecision 一次准入判定的完整结果。
//
// ⚠️ 无论允不允许，都要产出这个结构并落日志。
// 只在"允许"时记录的话，「为什么这个 Pod 没走 AI」就永远查不到 ——
// 而那恰恰是排查"AI 怎么没帮上忙"时第一个要问的。
type AIGateDecision struct {
	Allowed bool `json:"allowed"`
	// Reason 给人看的一句话。拒绝时必须说清楚是**哪一层**已经给出了结论。
	Reason string `json:"reason"`
	// Code 机器可读的拒绝原因，用于统计「AI 被挡下来的原因分布」。
	//	rule_matched    规则已判出具体根因
	//	evidence_found  日志里有报错行，Layer 1 已经把它摆出来了
	//	historical      Layer 1.5 从 CMDB 历史事件里找到了
	//	disabled        没开 AI 兜底
	//	budget_exceeded 本轮成本上限已用尽
	//	no_context      连日志和事件都没采到，喂给模型也是空的
	Code string `json:"code,omitempty"`
	// LayersTried 前面几层各自的结论，逐层记。这是"详细说明"的主体。
	LayersTried []string `json:"layers_tried"`
}

// aiGateReason 组装一条逐层说明。
func layerNote(layer, verdict string) string { return layer + "：" + verdict }

// ShouldCallAI 判断这条诊断结果要不要走 AI。
//
// 🔴 判据只有一个：**Layer 1 连一条报错行都没捞到**。
//
//	日志里明明写着 `OutOfMemoryError` 却去问模型，是白花钱 ——
//	Layer 1 已经把那一行原样摆出来了，比模型的转述更可信。
//	所以只有 `NoErrorFound` 时才轮得到这一层。
//
// ⚠️ `res.Generic` 不能单独作为判据：泛化结论 + 日志里有报错行的组合，
//
//	正确的下一步是**看那些报错行**，不是问 AI。
func ShouldCallAI(res *DiagnosisResult, enabled bool) AIGateDecision {
	return shouldCallAI(res, enabled, false)
}

// ShouldCallAIForced 与 ShouldCallAI 相同，但**跳过前几层的"已有结论"判定**。
//
// 🔴 为什么需要它：`rule_matched` 此前被当成"已解决"，而它只是"已有初判"。
//
//	实测过一次（OPSCMDB-064）：规则把「库名末尾多了个换行符」判成
//	「凭据不对」——方向对了一半，confidence 还是 high，于是 AI 不再介入。
//	而 AI 恰恰最可能看出"右引号跑到了下一行"这种视觉线索。
//	人看着结论不对时，必须有一条路能说"我不信，再看一遍"。
//
// ⚠️ 只放开"前面已经有结论"这两层（rule_matched / evidence_found）。
//
//	disabled / no_context / budget_exceeded 是**硬约束**，强制也绕不过：
//	没开 AI、没日志、没额度时强行调用，得到的要么是报错要么是编的。
func ShouldCallAIForced(res *DiagnosisResult, enabled bool) AIGateDecision {
	return shouldCallAI(res, enabled, true)
}

func shouldCallAI(res *DiagnosisResult, enabled, force bool) AIGateDecision {
	d := AIGateDecision{LayersTried: []string{}}

	// Layer 0
	if res == nil {
		return AIGateDecision{Allowed: false, Code: "no_context", Reason: "没有诊断结果可供判断"}
	}
	if res.Matched && !res.Generic {
		note := layerNote("Layer 0 规则", "命中具体根因「"+res.RootCause+"」")
		if force {
			// 强制复核：把规则结论留在层级说明里，让模型和人都看得到"初判是什么"
			d.LayersTried = append(d.LayersTried, note+"（已请求复核，不作为终止条件）")
		} else {
			d.LayersTried = append(d.LayersTried, note)
			d.Reason = "规则已判出具体根因；若结论看着不对，可请求 AI 复核（force_ai）"
			d.Code = "rule_matched"
			return d
		}
	}
	d.LayersTried = append(d.LayersTried,
		layerNote("Layer 0 规则", "只给出泛化结论「"+res.RootCause+"」，未判出根因"))

	// Layer 1
	if res.Extracted == nil {
		d.LayersTried = append(d.LayersTried, layerNote("Layer 1 证据提炼", "未执行（没有日志可提炼）"))
		d.Reason = "连日志都没采到，喂给模型的会是空的 —— 先解决采集"
		d.Code = "no_context"
		return d
	}
	if !res.Extracted.NoErrorFound {
		note := layerNote("Layer 1 证据提炼",
			fmt.Sprintf("扫了 %d 行，捞到 %d 条报错行", res.Extracted.Scanned, len(res.Extracted.Lines)))
		if force {
			d.LayersTried = append(d.LayersTried, note+"（已请求复核，不作为终止条件）")
		} else {
			d.LayersTried = append(d.LayersTried, note)
			d.Reason = "日志里有真实报错行，看那几行比问模型更可信也更便宜"
			d.Code = "evidence_found"
			return d
		}
	}
	d.LayersTried = append(d.LayersTried, layerNote("Layer 1 证据提炼",
		fmt.Sprintf("扫了 %d 行，一条报错行都没有", res.Extracted.Scanned)))

	// Layer 1.5
	if res.Change != nil && res.Change.Related {
		d.LayersTried = append(d.LayersTried, layerNote("Layer 1.5 变更关联", res.Change.Summary))
	} else {
		d.LayersTried = append(d.LayersTried, layerNote("Layer 1.5 变更关联", "没有找到相关变更"))
	}

	if !enabled {
		d.Reason = "AI 兜底未启用（设置 OPS_AI_ENABLED=1 并配好 API Key 后生效）"
		d.Code = "disabled"
		return d
	}

	d.Allowed = true
	d.Reason = "前三层都没给出根因，且日志里确实没有报错行 —— 这是 AI 唯一该出场的情形"
	return d
}

// ─────────────────────────────────────────────────────────────────
// 成本闸门
// ─────────────────────────────────────────────────────────────────

// AIBudget 一轮诊断（单次 diagnose_pod 或一次 sweep）的**硬上限**。
//
// 🔴 是硬上限不是软提示：用满就停，并**如实说出来**还有多少个没诊断。
//
//	软提示（"注意成本"）在批量场景下等于没有 —— 一次 sweep 扫几百个 Pod，
//	没有闸门就是几百次调用。
//
// ⚠️ 停下来时必须让调用方知道"还剩 M 个没看"，不能静默截断。
//
//	静默截断的结果是「扫完了，这些就是全部问题」—— 而那是假的。
//	（workflow 那边同样的教训：no silent caps。）
type AIBudget struct {
	mu sync.Mutex
	// MaxCalls 本轮最多调几次。<=0 表示不允许调用（相当于关闭）。
	MaxCalls int
	// MaxCostUSD 本轮成本上限（美元）。<=0 表示不按金额限制，只看次数。
	MaxCostUSD float64
	// unknownCost 本轮出现过价目表里没有的型号 —— 金额不可核算，见 Charge。
	unknownCost bool

	used     int
	costUSD  float64
	skipped  int
	stopNote string
}

// NewAIBudget 建一轮预算。
func NewAIBudget(maxCalls int, maxCostUSD float64) *AIBudget {
	return &AIBudget{MaxCalls: maxCalls, MaxCostUSD: maxCostUSD}
}

// Take 申请一次调用额度。返回 false 表示预算用尽，调用方**必须**停下并记账。
func (b *AIBudget) Take() bool {
	if b == nil {
		return false
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.MaxCalls > 0 && b.used >= b.MaxCalls {
		b.skipped++
		b.stopNote = fmt.Sprintf("本轮 AI 调用次数已达上限 %d 次", b.MaxCalls)
		return false
	}
	// 上一次调用用的是价目表里没有的型号 —— 成本记成了 0，金额闸门等于没有。
	// 不知道花了多少就不再花（见 Charge 的说明）。
	if b.unknownCost {
		b.skipped++
		b.stopNote = "上一次调用的型号不在价目表里，本轮成本无法核算，已停止继续调用"
		return false
	}
	if b.MaxCostUSD > 0 && b.costUSD >= b.MaxCostUSD {
		b.skipped++
		b.stopNote = fmt.Sprintf("本轮 AI 成本已达上限 US$%.4f", b.MaxCostUSD)
		return false
	}
	b.used++
	return true
}

// Charge 记一次实际花费。⚠️ 必须在拿到用量之后调用，而不是估算。
//
// known=false 表示**价目表里没有这个型号**，本次成本按 0 记。
//
// 🔴 那样金额上限就失效了：`Charge(0)` 永远累加不到 MaxCostUSD，
//
//	一轮 sweep 能一直调下去，只剩次数上限拦着。换个新模型（价目表还没跟上）
//	就正好落进这个洞 —— 而"用了新模型"恰恰是最可能花超的时候。
//
// ⚠️ 修法不是编一个价钱（那比不记更坏：账单对不上时人会信我们记的那个），
//
//	而是让**未知型号本身成为停止条件**：不知道花了多少，就不能继续花。
func (b *AIBudget) Charge(costUSD float64, known bool) {
	if b == nil {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.costUSD += costUSD
	if !known {
		b.unknownCost = true
	}
}

// Summary 本轮账单。**跳过数 >0 时必须显示给人看**。
func (b *AIBudget) Summary() (calls int, costUSD float64, skipped int, note string) {
	if b == nil {
		return 0, 0, 0, ""
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.used, b.costUSD, b.skipped, b.stopNote
}

// ─────────────────────────────────────────────────────────────────
// 计价
// ─────────────────────────────────────────────────────────────────

// AIUsage 一次调用的用量与折算成本。全部要落审计。
type AIUsage struct {
	Model        string  `json:"model"`
	InputTokens  int     `json:"input_tokens"`
	OutputTokens int     `json:"output_tokens"`
	CostUSD      float64 `json:"cost_usd"`
	// CostKnown 这个型号在价目表里认得。
	//
	// ⚠️ false 时 CostUSD 是 0，但那**不是"没花钱"** —— 是"不知道花了多少"。
	//	落库和界面上都要能分开这两件事，否则账单对不上时没人查得出来。
	CostKnown bool  `json:"cost_known"`
	ElapsedMS int64 `json:"elapsed_ms"`
}

// 每百万 token 的单价（美元）。
//
// ⚠️ 这是**会变**的外部事实，不是常量真理。改价时必须同时改这里，
// 否则账单和我们记的数会对不上，而对不上时人会信我们记的那个（错的）。
// 认不出的型号一律按 0 计价并在日志里 WARN —— 编一个价钱比不记更坏。
var aiPricePerMTok = map[string]struct{ in, out float64 }{
	"claude-opus-5":             {15, 75},
	"claude-sonnet-5":           {3, 15},
	"claude-haiku-4-5":          {0.8, 4},
	"claude-haiku-4-5-20251001": {0.8, 4},
}

// AICost 折算成本。返回 (成本, 是否认得这个型号)。
func AICost(model string, in, out int) (float64, bool) {
	p, ok := aiPricePerMTok[strings.TrimSpace(model)]
	if !ok {
		return 0, false
	}
	return float64(in)/1e6*p.in + float64(out)/1e6*p.out, true
}

// AICallRecord 一次 AI 调用的完整记录 —— 这就是「详细说明」本身。
//
// 🔴 三件事一件都不能少（用户明确要求）：
//
//	为什么走到了 AI      Gate.LayersTried（逐层结论）
//	喂进去的是什么        Prompt（原文，不是摘要）
//	花了多少             Usage
//
// ⚠️ Prompt 里可能有业务日志。落库前必须过一遍脱敏，
// 而脱敏必须在**写入前**做 —— 写进去再想删就晚了。
type AICallRecord struct {
	At      time.Time      `json:"at"`
	Cluster int            `json:"cluster_id"`
	Object  string         `json:"object"` // ns/name
	Gate    AIGateDecision `json:"gate"`
	Prompt  string         `json:"prompt"`
	Answer  string         `json:"answer"`
	Usage   AIUsage        `json:"usage"`
	Err     string         `json:"err,omitempty"`
}
