package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"

	"github.com/anthropics/anthropic-sdk-go"
)

const systemPrompt = `你是「运维 AI 助手」，服务于内部运维平台。你通过 MCP 工具只读查询 CMDB（集群/主机/域名/证书/成本/K8s 诊断等）。

规则：
- 你是只读的，不执行任何变更。需要动手时，给出可复制的命令，让用户自己执行。
- 排障时先用工具取证（如 diagnose_pod / pod_logs / list_pods / workload_changes），再给根因和处置建议。
- 用简体中文回答。命令用代码块。结论先行，简明。
- 拿不准就说明，不要编造数据。`

// namedMCP 是一个带名字的 MCP 服务器连接。
type namedMCP struct {
	name   string
	client *MCPClient
}

// Chatter 持有 Anthropic 客户端 + 多个 MCP 客户端 + 缓存的工具定义。
type Chatter struct {
	ac    anthropic.Client
	mcps  []namedMCP
	model string
	oauth bool // 用订阅 OAuth token 时,system 首段需带 Claude Code 身份,否则被拒
	tools []anthropic.ToolUnionParam
	owner map[string]*MCPClient // 工具名 -> 归属的 MCP 客户端(调用路由)
}

func NewChatter(ac anthropic.Client, mcps []namedMCP, model string, oauth bool) *Chatter {
	return &Chatter{ac: ac, mcps: mcps, model: model, oauth: oauth}
}

// claudeCodeIdentity 是订阅 OAuth token 走 API 时服务端要求的 system 首段身份。
const claudeCodeIdentity = "You are Claude Code, Anthropic's official CLI for Claude."

// loadTools 从所有启用的 MCP 服务器拉工具、聚合、建工具名->归属路由表（缓存）。
// 只要有任一服务器连上就返回成功;全部失败才报错。
func (c *Chatter) loadTools(ctx context.Context) error {
	if c.tools != nil && c.owner != nil {
		return nil
	}
	tools := make([]anthropic.ToolUnionParam, 0, 64)
	owner := map[string]*MCPClient{}
	var lastErr error
	okCount := 0
	for _, m := range c.mcps {
		_ = m.client.Initialize(ctx)
		mts, err := m.client.ListTools(ctx)
		if err != nil {
			lastErr = fmt.Errorf("MCP[%s]: %w", m.name, err)
			log.Printf("⚠️  MCP[%s] 拉工具失败: %v", m.name, err)
			continue
		}
		okCount++
		for _, t := range mts {
			if _, dup := owner[t.Name]; dup {
				log.Printf("⚠️  工具名冲突 %q(已属其他 MCP,跳过 %s)", t.Name, m.name)
				continue
			}
			schema := anthropic.ToolInputSchemaParam{}
			if len(t.InputSchema) > 0 {
				var raw map[string]any
				if json.Unmarshal(t.InputSchema, &raw) == nil {
					schema.Properties = raw["properties"]
					if req, ok := raw["required"].([]any); ok {
						for _, r := range req {
							if s, ok := r.(string); ok {
								schema.Required = append(schema.Required, s)
							}
						}
					}
				}
			}
			tp := anthropic.ToolParam{Name: t.Name, Description: anthropic.String(t.Description), InputSchema: schema}
			tools = append(tools, anthropic.ToolUnionParam{OfTool: &tp})
			owner[t.Name] = m.client
		}
		log.Printf("已从 MCP[%s] 载入 %d 个工具", m.name, len(mts))
	}
	if okCount == 0 && len(c.mcps) > 0 {
		if lastErr != nil {
			return lastErr
		}
		return fmt.Errorf("无可用 MCP 服务器")
	}
	// Prompt Caching:在最后一个工具打缓存断点 → 缓存整个工具块(静态,每轮循环都重发,命中省~90%)
	if n := len(tools); n > 0 && tools[n-1].OfTool != nil {
		tools[n-1].OfTool.CacheControl = anthropic.NewCacheControlEphemeralParam()
	}
	c.tools = tools
	c.owner = owner
	return nil
}

// setConvCache 把对话历史的缓存断点移到最后一条消息的末块(先清所有旧断点,保证同一时刻只有一个),
// 让循环里反复重发的历史(尤其大块工具返回)命中缓存。短对话(<最小缓存长度)时断点自动被忽略,无害。
func setConvCache(messages []anthropic.MessageParam) {
	for _, m := range messages {
		for i := range m.Content {
			if cc := m.Content[i].GetCacheControl(); cc != nil {
				*cc = anthropic.CacheControlEphemeralParam{}
			}
		}
	}
	if n := len(messages); n > 0 {
		c := messages[n-1].Content
		if len(c) > 0 {
			if cc := c[len(c)-1].GetCacheControl(); cc != nil {
				*cc = anthropic.NewCacheControlEphemeralParam()
			}
		}
	}
}

// Event 是推给前端的 SSE 事件。
type Event struct {
	Type string `json:"type"` // tool | tool_done | text | error | done
	Name string `json:"name,omitempty"`
	Args string `json:"args,omitempty"`
	Text string `json:"text,omitempty"`
}

// Turn 处理一轮对话：messages 是完整历史（最后一条是本次用户输入）。
// emit 回调把过程事件推给前端。返回本轮助手最终文本（供上层追加进历史）。
func (c *Chatter) Turn(ctx context.Context, messages []anthropic.MessageParam, emit func(Event)) (string, []anthropic.MessageParam, error) {
	if err := c.loadTools(ctx); err != nil {
		emit(Event{Type: "error", Text: "载入工具失败: " + err.Error()})
		return "", messages, err
	}
	system := []anthropic.TextBlockParam{{Text: systemPrompt}}
	if c.oauth { // 订阅 token:首段必须是 Claude Code 身份
		system = append([]anthropic.TextBlockParam{{Text: claudeCodeIdentity}}, system...)
	}
	// Prompt Caching:系统提示末段打断点(涵盖 工具+系统 整个静态前缀)
	system[len(system)-1].CacheControl = anthropic.NewCacheControlEphemeralParam()
	finalText := ""
	for iter := 0; iter < 12; iter++ { // 兜底最多 12 轮工具调用
		setConvCache(messages) // 对话历史(含大块工具返回)也缓存,断点移到最后一条
		msg, err := c.ac.Messages.New(ctx, anthropic.MessageNewParams{
			Model:     anthropic.Model(c.model),
			MaxTokens: 8192,
			System:    system,
			Thinking:  anthropic.ThinkingConfigParamUnion{OfAdaptive: &anthropic.ThinkingConfigAdaptiveParam{}},
			Tools:     c.tools,
			Messages:  messages,
		})
		if err != nil {
			emit(Event{Type: "error", Text: "调用模型失败: " + err.Error()})
			return finalText, messages, err
		}
		u := msg.Usage
		log.Printf("用量[iter %d] 输入=%d 输出=%d 缓存写=%d 缓存读=%d", iter, u.InputTokens, u.OutputTokens, u.CacheCreationInputTokens, u.CacheReadInputTokens)
		messages = append(messages, msg.ToParam())

		var toolResults []anthropic.ContentBlockParamUnion
		for _, block := range msg.Content {
			switch b := block.AsAny().(type) {
			case anthropic.TextBlock:
				if b.Text != "" {
					finalText += b.Text
					emit(Event{Type: "text", Text: b.Text})
				}
			case anthropic.ToolUseBlock:
				emit(Event{Type: "tool", Name: b.Name, Args: string(b.Input)})
				var out string
				var isErr bool
				if cli := c.owner[b.Name]; cli != nil {
					var cerr error
					out, isErr, cerr = cli.CallTool(ctx, b.Name, b.Input)
					if cerr != nil {
						out, isErr = "工具调用失败: "+cerr.Error(), true
					}
				} else {
					out, isErr = "无对应 MCP 服务器提供工具: "+b.Name, true
				}
				emit(Event{Type: "tool_done", Name: b.Name})
				toolResults = append(toolResults, anthropic.NewToolResultBlock(b.ID, truncate(out, 20000), isErr))
			}
		}

		if msg.StopReason != anthropic.StopReasonToolUse || len(toolResults) == 0 {
			break // 模型给完答案了
		}
		messages = append(messages, anthropic.NewUserMessage(toolResults...))
	}
	emit(Event{Type: "done"})
	return finalText, messages, nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + fmt.Sprintf("\n…（已截断，共 %d 字节）", len(s))
}
