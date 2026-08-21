package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"ops-cmdb-backend/diag"
	"ops-cmdb-backend/logx"
)

// ─────────────────────────────────────────────────────────────────
// Layer 2：AI 兜底的实际调用（OPSCMDB-043 B6）。
//
// 准入判定与成本闸门在 diag/aigate.go —— 那部分不碰网络，能单独测。
// 这里只负责：拼 prompt → 调 API → 记账 → 落审计。
//
// 🔴 三条硬约束（用户拍板）：
//
//	① 每次调用都要能回答"为什么前面几层没结论、喂了什么、花了多少"
//	② 界面必须能一眼看出「这条是 AI 判的」，且置信度不得高于 medium
//	③ 单轮成本是**硬上限**，用满就停并如实说还剩多少没诊断
//
// ⚠️ 默认关闭。开启意味着**业务日志会出站到 Anthropic**，
//
//	这是一个需要人明确知情的决定，不该靠默认值替人做。
// ─────────────────────────────────────────────────────────────────

// aiConfig AI 兜底的配置。全部来自环境变量，不进数据库 ——
// API Key 进库意味着又多一个要脱敏、要轮转、要防泄露的地方。
type aiConfig struct {
	Enabled bool
	APIKey  string
	Model   string
	BaseURL string
	// MaxCallsPerRound 单轮（一次 diagnose_pod 或一次 sweep）最多调几次
	MaxCallsPerRound int
	// MaxCostPerRoundUSD 单轮成本上限
	MaxCostPerRoundUSD float64
	Timeout            time.Duration
}

// 环境变量读取辅助。放在本文件里而不是 config 包：
// AI 兜底是可选能力，配置项跟着能力走，不进主配置结构 ——
// 主配置里多一个字段，所有部署都要面对"这个要不要填"。
func aiEnvStr(k, def string) string {
	if v := strings.TrimSpace(os.Getenv(k)); v != "" {
		return v
	}
	return def
}

func aiEnvBool(k string, def bool) bool {
	switch strings.ToLower(aiEnvStr(k, "")) {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	}
	return def
}

func aiEnvInt(k string, def int) int {
	if n, err := strconv.Atoi(aiEnvStr(k, "")); err == nil {
		return n
	}
	return def
}

func aiEnvFloat(k string, def float64) float64 {
	if f, err := strconv.ParseFloat(aiEnvStr(k, ""), 64); err == nil {
		return f
	}
	return def
}

func loadAIConfig() aiConfig {
	c := aiConfig{
		Enabled:            aiEnvBool("OPS_AI_ENABLED", false),
		APIKey:             strings.TrimSpace(aiEnvStr("OPS_AI_API_KEY", "")),
		Model:              aiEnvStr("OPS_AI_MODEL", "claude-haiku-4-5-20251001"),
		BaseURL:            strings.TrimRight(aiEnvStr("OPS_AI_BASE_URL", "https://api.anthropic.com"), "/"),
		MaxCallsPerRound:   aiEnvInt("OPS_AI_MAX_CALLS", 5),
		MaxCostPerRoundUSD: aiEnvFloat("OPS_AI_MAX_COST_USD", 0.50),
		Timeout:            time.Duration(aiEnvInt("OPS_AI_TIMEOUT_SEC", 30)) * time.Second,
	}
	// ⚠️ 开了但没给 Key 要**说出来**，不能静默降级成"没开"。
	//	静默降级的话，人配了半天以为生效了，而每条诊断都在悄悄走规则兜底。
	if c.Enabled && c.APIKey == "" {
		logx.J("diag_ai", "enabled_without_key", map[string]any{
			"warn": "OPS_AI_ENABLED=1 但没有 OPS_AI_API_KEY，AI 兜底实际不会生效",
		})
		c.Enabled = false
	}
	return c
}

// aiPrompt 拼给模型的输入。
//
// 🔴 只放**事实**，不放我们的猜测。
//
//	把规则的泛化结论（"容器反复重启"）写进去会给模型一个锚，
//	它会顺着那个方向编 —— 而我们要的恰恰是它从原始材料里看出别的东西。
//
// ⚠️ 全文要过 diag.Redact。它减少泄露面但不保证不泄露，
//
//	所以这段文字最终**会出站**这件事必须让人知道（配置项的说明里写了）。
func aiPrompt(c *diag.DiagnosisContext) string {
	var b strings.Builder
	b.WriteString("你在排查一个 Kubernetes Pod 的异常。以下是我们采到的全部材料。\n")
	b.WriteString("规则引擎已经扫过一遍，没有命中已知故障模式；日志里也没有任何一行含报错关键词。\n\n")

	fmt.Fprintf(&b, "命名空间/名称：%s/%s\n", c.Namespace, c.PodName)
	fmt.Fprintf(&b, "状态：phase=%s reason=%s 重启=%d\n", c.Phase, c.PodReason, c.Restarts)
	if c.NodeName != "" {
		fmt.Fprintf(&b, "所在节点：%s", c.NodeName)
		if c.NodePressure != "" {
			fmt.Fprintf(&b, "（该节点有压力位：%s）", c.NodePressure)
		}
		b.WriteString("\n")
	}
	if len(c.ConfigIssues) > 0 {
		fmt.Fprintf(&b, "引用了但不存在的配置：%s\n", strings.Join(c.ConfigIssues, "、"))
	}
	if c.MemUsage != "" {
		fmt.Fprintf(&b, "内存用量：%s\n", c.MemUsage)
	}
	if c.ImageChange != "" {
		fmt.Fprintf(&b, "近期变更：%s\n", c.ImageChange)
	}
	if len(c.Events) > 0 {
		b.WriteString("\n事件：\n")
		for _, e := range c.Events {
			fmt.Fprintf(&b, "  [%s] %s: %s\n", e.Type, e.Reason, e.Message)
		}
	}
	// LogTails 是 container -> 末尾日志。逐个容器写出来并标明是哪个 ——
	// 多容器 Pod 里"哪个容器报的"往往就是答案的一半
	for name, tail := range c.LogTails {
		if strings.TrimSpace(tail) == "" {
			continue
		}
		fmt.Fprintf(&b, "\n容器 %s 的日志（末尾）：\n%s\n", name, tail)
	}

	b.WriteString(`
请只根据上面的材料回答，用 JSON，不要有别的文字：
{"root_cause":"一句话说明最可能的根因","solutions":["具体到可以照做的一步","第二步"],"uncertain":"哪些地方材料不足以下结论"}

要求：
- 材料里没有的东西不要编。你没有集群访问权限，也拿不到更多日志。
- 如果材料确实不足以判断，root_cause 就写「材料不足以判断」，并在 uncertain 里说清楚还缺什么。
  给一个编出来的根因，比说不知道更有害 —— 人会照着它去查，浪费一轮。
- solutions 要写"下一步做什么"，不要写"建议排查一下"这种没有动作的话。`)
	return diag.Redact(b.String())
}

type aiAnswer struct {
	RootCause string   `json:"root_cause"`
	Solutions []string `json:"solutions"`
	Uncertain string   `json:"uncertain"`
}

// callAI 打一次 Anthropic Messages API。
func callAI(ctx context.Context, cfg aiConfig, prompt string) (aiAnswer, diag.AIUsage, error) {
	started := time.Now()
	var out aiAnswer
	usage := diag.AIUsage{Model: cfg.Model}

	body, _ := json.Marshal(map[string]any{
		"model":      cfg.Model,
		"max_tokens": 1024,
		"messages":   []map[string]string{{"role": "user", "content": prompt}},
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, cfg.BaseURL+"/v1/messages", bytes.NewReader(body))
	if err != nil {
		return out, usage, err
	}
	req.Header.Set("content-type", "application/json")
	req.Header.Set("x-api-key", cfg.APIKey)
	req.Header.Set("anthropic-version", "2023-06-01")

	resp, err := (&http.Client{Timeout: cfg.Timeout}).Do(req)
	if err != nil {
		return out, usage, err
	}
	defer resp.Body.Close()
	var raw struct {
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
		Usage struct {
			InputTokens  int `json:"input_tokens"`
			OutputTokens int `json:"output_tokens"`
		} `json:"usage"`
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return out, usage, fmt.Errorf("解析响应失败: %w", err)
	}
	usage.InputTokens = raw.Usage.InputTokens
	usage.OutputTokens = raw.Usage.OutputTokens
	usage.ElapsedMS = time.Since(started).Milliseconds()
	cost, known := diag.AICost(cfg.Model, usage.InputTokens, usage.OutputTokens)
	usage.CostUSD = cost
	usage.CostKnown = known
	if !known {
		// ⚠️ 不认得的型号计 0 并 WARN。编一个价钱比不记更坏：
		//	账单和我们记的数对不上时，人会信我们记的那个
		logx.J("diag_ai", "unknown_model_price", map[string]any{
			"model": cfg.Model, "warn": "价目表里没有这个型号，本次成本按 0 记，实际会产生费用",
		})
	}
	if resp.StatusCode != http.StatusOK {
		msg := fmt.Sprintf("HTTP %d", resp.StatusCode)
		if raw.Error != nil {
			msg += ": " + raw.Error.Message
		}
		return out, usage, fmt.Errorf("%s", msg)
	}
	text := ""
	for _, c := range raw.Content {
		text += c.Text
	}
	// 模型可能把 JSON 包在 ```json 里
	text = strings.TrimSpace(text)
	if i := strings.Index(text, "{"); i >= 0 {
		if j := strings.LastIndex(text, "}"); j > i {
			text = text[i : j+1]
		}
	}
	if err := json.Unmarshal([]byte(text), &out); err != nil {
		return out, usage, fmt.Errorf("模型没有返回可解析的 JSON: %w", err)
	}
	return out, usage, nil
}

// maybeAI 在规则/证据/历史三层都没给出根因时，走 AI 兜底。
//
// 🔴 无论走不走，都**必须**落一条日志说明理由（用户要求的"详细说明"）。
//
//	只在走了的时候记录的话，「这个 Pod 为什么没走 AI」永远查不到 ——
//	而那正是排查"AI 怎么没帮上忙"时第一个要问的。
//
// 返回值：是否改写了 res。
func (h *K8sDiagHandler) maybeAI(
	ctx context.Context, cid int, dc *diag.DiagnosisContext,
	res *diag.DiagnosisResult, budget *diag.AIBudget, force bool,
) bool {
	cfg := loadAIConfig()
	gate := diag.ShouldCallAI(res, cfg.Enabled)
	if force {
		gate = diag.ShouldCallAIForced(res, cfg.Enabled)
	}

	obj := dc.Namespace + "/" + dc.PodName
	// 逐层说明先落一条，不管后面走不走
	logx.J("diag_ai", "gate", map[string]any{
		"cluster_id": cid, "object": obj,
		"allowed": gate.Allowed, "code": gate.Code, "reason": gate.Reason,
		"forced": force,
		"layers": gate.LayersTried,
	})
	// 界面也要能看到"为什么没走 AI" —— 只写日志的话没人会去翻
	res.AIGate = &gate
	if !gate.Allowed {
		return false
	}

	// ③ 硬上限。⚠️ 拿不到额度时**必须记账并说出来**，不能静默跳过 ——
	//	静默跳过的结果是「扫完了，这些就是全部」，而那是假的。
	if !budget.Take() {
		_, _, skipped, note := budget.Summary()
		gate.Allowed = false
		gate.Code = "budget_exceeded"
		gate.Reason = note + fmt.Sprintf("，本轮已跳过 %d 个对象未做 AI 诊断", skipped)
		res.AIGate = &gate
		logx.J("diag_ai", "budget_exceeded", map[string]any{
			"cluster_id": cid, "object": obj, "skipped": skipped, "note": note,
		})
		return false
	}

	prompt := aiPrompt(dc)
	ans, usage, err := callAI(ctx, cfg, prompt)
	budget.Charge(usage.CostUSD, usage.CostKnown)

	rec := diag.AICallRecord{
		At: time.Now(), Cluster: cid, Object: obj, Gate: gate,
		// ⚠️ prompt 已在 aiPrompt 里过了 Redact；这里再存的是同一份
		Prompt: prompt, Usage: usage,
	}
	if err != nil {
		rec.Err = err.Error()
		h.recordAICall(rec)
		// 🔴 调用失败**不能吞掉**：吞掉的话界面显示的还是规则那条泛化结论，
		//	而人以为"AI 也看过了，就这样"。要说清楚 AI 这一层没跑成。
		gate.Reason = "AI 调用失败：" + err.Error()
		gate.Code = "call_failed"
		gate.Allowed = false
		res.AIGate = &gate
		logx.J("diag_ai", "call_failed", map[string]any{
			"cluster_id": cid, "object": obj, "err": err.Error(),
			"input_tokens": usage.InputTokens, "output_tokens": usage.OutputTokens,
		})
		return false
	}
	rec.Answer = ans.RootCause
	h.recordAICall(rec)
	logx.J("diag_ai", "answered", map[string]any{
		"cluster_id": cid, "object": obj, "model": usage.Model,
		"input_tokens": usage.InputTokens, "output_tokens": usage.OutputTokens,
		"cost_usd": usage.CostUSD, "elapsed_ms": usage.ElapsedMS,
		"root_cause": ans.RootCause,
	})

	// 模型自己说材料不足时，**不要**把它当成根因写上去。
	// 「材料不足以判断」是一个诚实的结论，比编一个根因有用 ——
	// 但它不该占据"根因"那一栏，那会让人以为我们判过了。
	if strings.Contains(ans.RootCause, "材料不足") || strings.TrimSpace(ans.RootCause) == "" {
		gate.Reason = "AI 也认为材料不足以判断：" + ans.Uncertain
		gate.Code = "ai_insufficient"
		res.AIGate = &gate
		return false
	}

	// ② 明确标注：Provider 带 ai: 前缀，置信度**封顶 medium**。
	//	AI 的结论不该和规则的确定性判定长得一样 ——
	//	规则是"日志里写着 OutOfMemoryError"，AI 是"看起来像"。
	res.RootCause = ans.RootCause
	res.Provider = "ai:" + usage.Model
	res.Confidence = "medium"
	res.Matched = true
	res.Generic = false
	res.Solutions = res.Solutions[:0]
	for _, s := range ans.Solutions {
		res.Solutions = append(res.Solutions, diag.Solution{Text: s})
	}
	if ans.Uncertain != "" {
		// 不确定的部分要一并给出来：AI 判定必须连同它的边界一起展示
		res.Solutions = append(res.Solutions,
			diag.Solution{Text: "⚠️ 模型认为材料不足以确定的部分：" + ans.Uncertain})
	}
	return true
}

// recordAICall 把一次 AI 调用写进审计。
//
// 🔴 这条记录就是"详细说明"本身，三件事一件不能少：
// 为什么走到 AI（gate.layers）/ 喂了什么（prompt）/ 花了多少（usage）。
func (h *K8sDiagHandler) recordAICall(rec diag.AICallRecord) {
	blob, err := json.Marshal(rec)
	if err != nil {
		logx.J("diag_ai", "record_marshal_err", map[string]any{"err": err.Error()})
		return
	}
	if _, err := h.DB.Exec(
		// ⚠️ cost_known 必须存：cost_usd=0 有两种含义（真没花钱 / 认不出型号按 0 记），
		//	只存金额的话，对账时分不开这两件事
		`INSERT INTO ai_call_logs (at, cluster_id, object, model, input_tokens, output_tokens,
		    cost_usd, cost_known, elapsed_ms, gate_code, err, detail)
		 VALUES (?,?,?,?,?,?,?,?,?,?,?,?)`,
		rec.At, rec.Cluster, rec.Object, rec.Usage.Model,
		rec.Usage.InputTokens, rec.Usage.OutputTokens, rec.Usage.CostUSD,
		b2int(rec.Usage.CostKnown), rec.Usage.ElapsedMS,
		rec.Gate.Code, rec.Err, string(blob)); err != nil {
		// 写不进去不该让诊断失败，但必须留痕 ——
		// 否则"AI 花了钱"这件事会连一条记录都没有
		logx.J("diag_ai", "record_persist_err", map[string]any{"err": err.Error(), "object": rec.Object})
	}
}
