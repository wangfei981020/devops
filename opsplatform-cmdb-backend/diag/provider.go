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
	Matched    bool               `json:"matched"`     // 是否命中已知模式
	RootCause  string             `json:"root_cause"`
	Confidence string             `json:"confidence"`  // high/medium/low
	Evidence   []string           `json:"evidence"`
	Change     *ChangeCorrelation `json:"change_correlation,omitempty"`
	Solutions  []Solution         `json:"solutions"`
	Provider   string             `json:"provider"` // rule / ai:xxx
}
