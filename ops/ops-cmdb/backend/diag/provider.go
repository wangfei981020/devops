package diag

// Provider 是诊断引擎的可插拔接口。一期只实现 RuleProvider；
// 后续 AI（Claude 或其他）实现同一接口即可挂入，前端/采集层无需改动。
type Provider interface {
	Name() string
	Diagnose(c *DiagnosisContext) *DiagnosisResult
}

// Solution 一条处置建议（一期只给方案，用户手动执行）。
type Solution struct {
	Text string `json:"text"`
	Link string `json:"link,omitempty"`
}

// ChangeCorrelation 变更关联（K8s 原生信号 + 未来 deploy 发布历史）。
type ChangeCorrelation struct {
	Related bool   `json:"related"`
	Source  string `json:"source,omitempty"` // k8s-native / deploy
	Summary string `json:"summary,omitempty"`
	Link    string `json:"link,omitempty"`
}

// DiagnosisResult 统一输出契约（规则版与未来 AI 版结构一致，前端一套 UI 通吃）。
type DiagnosisResult struct {
	Matched    bool               `json:"matched"` // 是否命中已知模式
	RootCause  string             `json:"root_cause"`
	Confidence string             `json:"confidence"` // high/medium/low
	Evidence   []string           `json:"evidence"`
	Change     *ChangeCorrelation `json:"change_correlation,omitempty"`
	Solutions  []Solution         `json:"solutions"`
	Provider   string             `json:"provider"` // rule / ai:xxx

	// Generic 标记「这条只是泛化结论，没真正判出根因」。
	//
	// 🔴 加它是为了让「还差多少」变成一个**可以量出来的数字**。
	// 此前只能靠人一个个点 diagnose_pod 抽样，而抽样天然先命中高频形态，
	// 长尾永远在后面 —— 每修一轮又冒出新的 miss，看不到头。
	//
	// 打了这个标记之后，批量自检（/api/k8s/diagnose-sweep）就能直接回答
	// 「这个集群 N 个异常 Pod 里，有几个我们其实没判出根因」。
	//
	// ⚠️ 只有**兜底**规则才设它：CrashLoopBackOff / 非零退出 / 未识别 /
	// 拉镜像但原因已过期。这几条给的都是"它挂了"，不是"为什么挂"。
	Generic bool `json:"generic,omitempty"`

	// Extracted 从日志里捞出来的报错行（规则没命中时才有）。
	//
	// 🔴 这是**证据不是判定**：每一行都是日志里原样存在的，本层不做任何推断。
	// 它的用途是「规则判不出来，但日志里其实写着」——那种情况下，
	// 把真实报错摆给人看，比任何编出来的结论都有用，而且零成本。
	//
	// ⚠️ `NoErrorFound=true` 是**允许调用 AI 的唯一前提**：
	// 连一条报错行都捞不到，才轮得到花钱让模型猜。
	Extracted *ExtractedEvidence `json:"extracted,omitempty"`

	// AIGate 这一条**为什么走了 / 为什么没走** AI 兜底。
	//
	// 🔴 无论走不走都要给：只在走了时给的话，
	//	「这个 Pod 怎么没让 AI 看看」就只能去翻日志。
	//	而它恰恰是人对 AI 兜底的第一个疑问。
	AIGate *AIGateDecision `json:"ai_gate,omitempty"`
}
