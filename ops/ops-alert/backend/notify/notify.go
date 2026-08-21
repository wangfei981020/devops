// Package notify 是通知渠道适配层。
//
// 引擎只认 Sender 接口，不知道对面是飞书还是短信。
// 上一代把 *lark.Sender 写进了检测引擎的函数签名，换个 IM 要改引擎。
//
// # 能力由代码声明，不存库
//
// 各渠道能力（卡片 / @人 / 按钮 / 字数上限）结构性不同，存库会与实现漂移：
// 升级后代码支持了按钮而库里还写着不支持，或者反过来——而这种不一致
// 不会报错，只会让消息渲染成奇怪的样子。
package notify

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

const (
	TypeFeishu  = "feishu"
	TypeWebhook = "webhook"
)

// Caps 描述渠道能力。模板按能力自动降级：
// 短信只取标题行，不支持富文本的渠道拿纯文本，而不是把 2KB 的 JSON 塞进去。
type Caps struct {
	RichCard bool // 支持卡片/富文本
	Mention  bool // 支持 @人
	Buttons  bool // 支持交互按钮
	MaxRunes int  // 0 = 不限
}

// Message 是渠道无关的通知内容。渲染成各家格式是各适配器的事。
type Message struct {
	Title    string
	Severity string
	// Fields 是有序的键值对（不是 map：map 遍历顺序随机，
	// 同一条告警每次发出来字段顺序都不同，值班的人没法形成肌肉记忆）
	Fields   []Field
	Sample   string   // 命中样本（日志原文片段）
	Link     string   // 事件详情链接
	Mentions []string // 渠道内的用户标识
	AtAll    bool

	// TemplateVars 供文案模板替换用的变量表。
	//
	// ⚠️ 各渠道的 Send **不该读它** —— 它是渲染的输入，不是消息内容。
	// 模板在 engine.sendOne 里套完之后，渠道拿到的已经是最终文案。
	// 为空表示这条消息不套模板（比如日报，它自己就是成品）。
	TemplateVars map[string]string `json:"-"`
}

type Field struct{ Key, Value string }

// Sender 是所有渠道的统一接口。
type Sender interface {
	Type() string
	Caps() Caps
	// Send 返回 nil 表示对端已确认接收。
	// ⚠️ 实现里绝不能把「HTTP 200 但业务码非 0」当成成功：
	// 飞书 webhook 限流时返回的就是 200 + 业务错误码，
	// 把它当成功会让「已通知」是假的，而值班以为没人叫他就是没事。
	Send(ctx context.Context, msg Message) error
}

// ErrUnsupportedType 类型未实现。返回错误而不是 nil sender，
// 让配置错误在保存时就暴露，而不是等到第一次告警发不出去。
var ErrUnsupportedType = errors.New("notify: 不支持的渠道类型")

// New 按类型构造渠道。config 是该渠道解密后的配置 JSON。
func New(typ string, config []byte) (Sender, error) {
	switch typ {
	case TypeFeishu:
		return newFeishu(config)
	case TypeWebhook:
		return newWebhook(config)
	default:
		// 二期：wecom / dingtalk / slack / teams / email / sms / voice
		return nil, fmt.Errorf("%w: %s", ErrUnsupportedType, typ)
	}
}

// PlainText 把 Message 降级成纯文本，供不支持富文本的渠道使用。
func PlainText(msg Message, caps Caps) string {
	var b strings.Builder
	b.WriteString("[" + severityLabel(msg.Severity) + "] " + msg.Title)
	for _, f := range msg.Fields {
		b.WriteString("\n" + f.Key + ": " + f.Value)
	}
	if msg.Sample != "" {
		b.WriteString("\n" + msg.Sample)
	}
	if msg.Link != "" {
		b.WriteString("\n" + msg.Link)
	}
	s := b.String()
	if caps.MaxRunes > 0 {
		r := []rune(s)
		if len(r) > caps.MaxRunes {
			// 截断留出省略号的位置，并保证标题行完整——
			// 被截掉一半的告警比没有告警更容易误导。
			s = string(r[:caps.MaxRunes-1]) + "…"
		}
	}
	return s
}

func severityLabel(s string) string {
	switch s {
	case "critical":
		return "紧急"
	case "warning":
		return "警告"
	case "info":
		return "提示"
	default:
		// 不认识的严重度按最高处理并保留原值：
		// 静默降级成 info 会让上游新增的级别被吞掉。
		return s
	}
}
