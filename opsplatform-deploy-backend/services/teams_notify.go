package services

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"
)

// ============================================================================
// Microsoft Teams 通知（Teams「工作流」/ Power Automate Webhook）—— 迁新发布系统前的临时双发
//
// 照搬运维平台 opsplatform/backend/services/teams_notify.go（桌台维护告警在用），两边行为统一。
// 发布系统的差异只有一处：卡片正文沿用飞书的，里面的 <at id="..."></at> 发 Teams 前去掉，
// @ 人改由 mentions 放进卡片（按通知人的 Teams 账号）。
//
// 地址配在「系统设置 → 通知渠道」（lark_bot.teams_webhook，AES 加密存库）。
//
// 运维平台原注释：
//   - 地址只认 *.environment.api.powerplatform.com 的 /powerautomate/ 路径，必须带 sig
//     （sig 就是凭据：拿到地址就能往群里发）。端口只允许不写或 443 —— 工作流生成的地址
//     本来就显式带 :443，告警系统曾因为拒绝端口误拒过真实地址。
//   - 200 和 202 都算成功：工作流先回 202 再发卡片，只认 200 会把每次送达都记成失败。
//   - 202 只代表「工作流收下了」，卡片之后在 Power Automate 里发；被限流或执行失败
//     发送方看不到，只能去 Power Automate 运行历史查。
//   - 错误信息里不出现地址：net/http 的连接错误会把整条 URL（含 sig）带出来。
//   - @人：提及直接放进卡片。10-21 在告警系统实测过，账号不存在时 Teams 把它当普通
//     文字，卡片照常送达、同卡其他人照常提醒，所以 @ 写错不影响告警发送。
// ============================================================================

const (
	teamsHostSuffix = ".environment.api.powerplatform.com"
	teamsPathPrefix = "/powerautomate/"
	// teamsMaxBodyRunes 卡片正文上限。Teams 单条消息约 28KB，正文留足余量。
	teamsMaxBodyRunes = 8000
	teamsMaxRetryWait = 60 * time.Second
)

var teamsClient = &http.Client{
	Timeout: 15 * time.Second,
	// 地址是使用者填的：不跟随重定向，3xx 原样当失败返回
	CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
}

// NormalizeTeamsWebhook 去掉地址里所有空白和不可见格式字符（零宽空格、BOM、NBSP …）。
// 从 Teams / Power Automate 页面复制地址常带进这些字符，真实 URL 里不会有。
func NormalizeTeamsWebhook(raw string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) || unicode.Is(unicode.Cf, r) {
			return -1
		}
		return r
	}, raw)
}

// ValidateTeamsWebhook 校验 Teams 工作流地址。调用前先 NormalizeTeamsWebhook。
// 每种不合规各报各的原因 —— 一句话对应好几个原因，使用者只能瞎猜。
func ValidateTeamsWebhook(raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return errors.New("Teams Webhook 地址格式不正确")
	}
	if u.Scheme != "https" {
		return errors.New("Teams Webhook 地址必须是 https://")
	}
	if u.User != nil {
		return errors.New("Teams Webhook 地址不能带账号密码")
	}
	if p := u.Port(); p != "" && p != "443" {
		return fmt.Errorf("Teams Webhook 地址的端口只能是 443，当前是 %s", p)
	}
	host := strings.ToLower(u.Hostname())
	prefix, ok := strings.CutSuffix(host, teamsHostSuffix)
	if !ok || prefix == "" {
		return fmt.Errorf("Teams Webhook 地址只能是 *%s（Teams「工作流」生成的地址），当前主机是 %s", teamsHostSuffix, host)
	}
	if u.Fragment != "" {
		return errors.New("Teams Webhook 地址不能带 #")
	}
	if !strings.HasPrefix(u.Path, teamsPathPrefix) {
		return errors.New("Teams Webhook 地址路径必须以 /powerautomate/ 开头 —— 请在 Teams 工作流里复制完整的 HTTP POST 地址")
	}
	for _, seg := range strings.Split(u.Path, "/") {
		if seg == ".." || seg == "." {
			return errors.New("Teams Webhook 地址路径里不能有 . 或 ..")
		}
	}
	if u.Query().Get("sig") == "" {
		return errors.New("Teams Webhook 地址缺少 sig 参数 —— 请复制完整地址（包括 ? 后面的部分）")
	}
	return nil
}

// ---------------------------------------------------------------------------
// @人
// ---------------------------------------------------------------------------

// TeamsMention 一个要 @ 的人。Account 为空表示没有可用账号，只在卡片上注明。
type TeamsMention struct {
	Name    string
	Account string
	Derived bool // 账号是按「姓名小写@后缀」推导的，不是填写的
}

var (
	teamsAccountRe   = regexp.MustCompile(`^[a-z0-9][a-z0-9._%+-]{0,63}@[a-z0-9](?:[a-z0-9-]{0,62}[a-z0-9])?(?:\.[a-z0-9](?:[a-z0-9-]{0,62}[a-z0-9])?)+$`)
	teamsDomainRe    = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,62}[a-z0-9])?(?:\.[a-z0-9](?:[a-z0-9-]{0,62}[a-z0-9])?)+$`)
	teamsLocalPartRe = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,63}$`)
)

// NormalizeTeamsAccount 转小写、去空白。
func NormalizeTeamsAccount(s string) string { return strings.ToLower(strings.TrimSpace(s)) }

// ValidTeamsAccount 账号格式像不像登录邮箱（不代表账号存在）。
func ValidTeamsAccount(s string) bool { return len(s) <= 254 && teamsAccountRe.MatchString(s) }

// ValidTeamsDomain 默认后缀是否可用（空 = 不推导）。
func ValidTeamsDomain(s string) bool {
	return s == "" || (len(s) <= 253 && teamsDomainRe.MatchString(s))
}

// TeamsAccountFor 通知人的 Teams 账号：填写的优先；没填时姓名是纯字母数字（. _ - 也行）
// 就推导成「姓名小写@后缀」；姓名有空格、中文推导不出，返回空。
func TeamsAccountFor(name, explicit, domain string) (string, bool) {
	if a := NormalizeTeamsAccount(explicit); a != "" {
		return a, false
	}
	local := strings.ToLower(strings.TrimSpace(name))
	if domain == "" || !teamsLocalPartRe.MatchString(local) {
		return "", false
	}
	return local + "@" + domain, true
}

// TeamsMentionLog 本次 @ 了谁、用的哪个账号，写日志用：Teams 不回告账号存不存在，
// 有人说没被提醒到时靠这一行对照。
func TeamsMentionLog(ms []TeamsMention) string {
	var on, missing []string
	for _, m := range ms {
		if m.Account == "" {
			missing = append(missing, m.Name)
			continue
		}
		how := "填写"
		if m.Derived {
			how = "默认"
		}
		on = append(on, fmt.Sprintf("%s(%s %s)", m.Name, m.Account, how))
	}
	s := "@ " + strings.Join(on, " ")
	if len(on) == 0 {
		s = "@ 无"
	}
	if len(missing) > 0 {
		s += "；未配置 Teams 账号：" + strings.Join(missing, "、")
	}
	return s
}

// ---------------------------------------------------------------------------
// 卡片
// ---------------------------------------------------------------------------

// larkAtRe 飞书的 <at id="..."></at>。飞书 ID 在 Teams 里没用，发 Teams 前去掉。
var larkAtRe = regexp.MustCompile(`<at id="[^"]*"></at>\s*`)

// teamsStyle 把 Lark 卡片的 header 颜色映射成 Adaptive Card 的容器样式。
func teamsStyle(color string) string {
	switch color {
	case "red":
		return "attention"
	case "orange", "yellow":
		return "warning"
	case "green":
		return "good"
	default:
		return "accent"
	}
}

// buildTeamsPayload 生成工作流 Webhook 需要的 {"type":"message","attachments":[卡片]}。
//
// body 是 Lark 的 lark_md 写法（**粗体**、换行）。Teams 会把 TextBlock 里的单个换行吞掉，
// 所以一行一个 TextBlock。
func buildTeamsPayload(title, body, color, linkLabel, linkURL string, mentions []TeamsMention) ([]byte, error) {
	body = larkAtRe.ReplaceAllString(body, "")
	if r := []rune(body); len(r) > teamsMaxBodyRunes {
		body = string(r[:teamsMaxBodyRunes]) + "\n…（内容过长已截断）"
	}
	items := []interface{}{map[string]interface{}{
		"type": "Container", "style": teamsStyle(color), "bleed": true,
		"items": []interface{}{map[string]interface{}{
			"type": "TextBlock", "text": title, "wrap": true, "size": "Medium", "weight": "Bolder",
		}},
	}}
	gap := false
	for _, line := range strings.Split(body, "\n") {
		if strings.TrimSpace(line) == "" {
			gap = true
			continue
		}
		spacing := "None"
		if gap {
			spacing, gap = "Medium", false
		}
		items = append(items, map[string]interface{}{"type": "TextBlock", "text": line, "wrap": true, "spacing": spacing})
	}

	var tags, missing []string
	var entities []map[string]interface{}
	seen := map[string]bool{}
	for _, m := range mentions {
		if m.Account == "" {
			missing = append(missing, m.Name)
			continue
		}
		if seen[m.Account] {
			continue
		}
		seen[m.Account] = true
		tag := "<at>" + m.Name + "</at>"
		tags = append(tags, tag)
		entities = append(entities, map[string]interface{}{
			"type": "mention", "text": tag,
			"mentioned": map[string]interface{}{"id": m.Account, "name": m.Name},
		})
	}
	if len(tags) > 0 {
		items = append(items, map[string]interface{}{
			"type": "TextBlock", "wrap": true, "spacing": "Medium", "text": "通知: " + strings.Join(tags, "  "),
		})
	}
	if len(missing) > 0 {
		items = append(items, map[string]interface{}{
			"type": "TextBlock", "wrap": true, "isSubtle": true, "size": "Small", "spacing": "Small",
			"text": "⚠ 未配置 Teams 账号，未能提醒：" + strings.Join(missing, "、"),
		})
	}
	msteams := map[string]interface{}{"width": "Full"}
	if len(entities) > 0 {
		msteams["entities"] = entities
	}
	card := map[string]interface{}{
		"type": "AdaptiveCard", "$schema": "http://adaptivecards.io/schemas/adaptive-card.json",
		"version": "1.4", "msteams": msteams, "body": items,
	}
	if linkURL != "" {
		if linkLabel == "" {
			linkLabel = "查看详情"
		}
		card["actions"] = []interface{}{map[string]interface{}{"type": "Action.OpenUrl", "title": linkLabel, "url": linkURL}}
	}
	env := map[string]interface{}{
		"type": "message",
		"attachments": []interface{}{map[string]interface{}{
			"contentType": "application/vnd.microsoft.card.adaptive", "contentUrl": nil, "content": card,
		}},
	}
	// 不做 HTML 转义：默认会把 <at> 写成 <at>，等价但不直观，还白占卡片大小
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(env); err != nil {
		return nil, err
	}
	return bytes.TrimRight(buf.Bytes(), "\n"), nil
}

// ---------------------------------------------------------------------------
// 发送
// ---------------------------------------------------------------------------

// TeamsStatusError Teams 返回的非成功状态码。
type TeamsStatusError struct {
	StatusCode int
}

func (e *TeamsStatusError) Error() string {
	switch {
	case e.StatusCode == http.StatusTooManyRequests:
		return "被 Teams 限流（同一账号 5 分钟约 25 条），请稍后再试"
	case e.StatusCode == 401 || e.StatusCode == 403:
		return fmt.Sprintf("Teams 返回 HTTP %d：地址里的 sig 不对，或工作流已关闭", e.StatusCode)
	case e.StatusCode == 404:
		return "Teams 返回 HTTP 404：工作流不存在，可能已被删除"
	default:
		return fmt.Sprintf("Teams 返回 HTTP %d", e.StatusCode)
	}
}

// SendTeamsCard 发一次，不重试（页面「发送测试」用）。
func SendTeamsCard(ctx context.Context, webhook, title, body, color, linkLabel, linkURL string, mentions []TeamsMention) error {
	_, err := sendTeamsOnce(ctx, webhook, title, body, color, linkLabel, linkURL, mentions)
	return err
}

// SendTeamsCardWithRetry 告警通知用：429 按 Retry-After 等（最多 60 秒），5xx 和网络错误
// 按 1s/2s 退避，共 3 次；其他 4xx 重试没用，直接返回。
func SendTeamsCardWithRetry(ctx context.Context, webhook, title, body, color, linkLabel, linkURL string, mentions []TeamsMention) error {
	var lastErr error
	for attempt := 1; attempt <= 3; attempt++ {
		wait, err := sendTeamsOnce(ctx, webhook, title, body, color, linkLabel, linkURL, mentions)
		if err == nil {
			if attempt > 1 {
				log.Printf("[teams] 第 %d 次尝试发送成功", attempt)
			}
			return nil
		}
		lastErr = err
		if wait < 0 || attempt == 3 {
			break
		}
		if wait == 0 {
			wait = time.Duration(1<<(attempt-1)) * time.Second
		}
		log.Printf("[teams] 第 %d/3 次发送失败，%v 后重试: %v", attempt, wait, err)
		select {
		case <-time.After(wait):
		case <-ctx.Done():
			return fmt.Errorf("重试过程中被取消: %w", ctx.Err())
		}
	}
	return lastErr
}

// sendTeamsOnce 发一次。返回的等待时间：>0 服务端要求等这么久（429），0 可重试由调用方
// 决定退避（5xx、网络错误），<0 不可重试。
func sendTeamsOnce(ctx context.Context, webhook, title, body, color, linkLabel, linkURL string, mentions []TeamsMention) (time.Duration, error) {
	webhook = NormalizeTeamsWebhook(webhook)
	// 发之前再校验一次：库里的旧数据、手改过的数据，也不能让这里变成「往任意地址发请求」
	if err := ValidateTeamsWebhook(webhook); err != nil {
		return -1, err
	}
	payload, err := buildTeamsPayload(title, body, color, linkLabel, linkURL, mentions)
	if err != nil {
		return -1, err
	}
	reqCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, "POST", webhook, bytes.NewReader(payload))
	if err != nil {
		return -1, errors.New("Teams Webhook 地址无法构造请求")
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := teamsClient.Do(req)
	if err != nil {
		return 0, fmt.Errorf("发送 Teams 消息失败: %w", redactTeamsURL(err, webhook))
	}
	defer resp.Body.Close()
	return teamsStatus(resp)
}

// teamsStatus 把 Teams 的响应换成「等多久 / 错误」，见 sendTeamsOnce 的返回约定。
func teamsStatus(resp *http.Response) (time.Duration, error) {
	io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
	switch {
	case resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusAccepted:
		return 0, nil
	case resp.StatusCode == http.StatusTooManyRequests:
		secs, _ := strconv.Atoi(strings.TrimSpace(resp.Header.Get("Retry-After")))
		wait := time.Duration(secs) * time.Second
		if wait <= 0 {
			wait = 5 * time.Second
		}
		if wait > teamsMaxRetryWait {
			wait = teamsMaxRetryWait
		}
		return wait, &TeamsStatusError{StatusCode: resp.StatusCode}
	case resp.StatusCode >= 500:
		return 0, &TeamsStatusError{StatusCode: resp.StatusCode}
	default:
		return -1, &TeamsStatusError{StatusCode: resp.StatusCode}
	}
}

// redactTeamsURL 从错误信息里去掉整条地址和单独的 sig。
func redactTeamsURL(err error, raw string) error {
	msg := strings.ReplaceAll(err.Error(), raw, "<teams-webhook>")
	if u, perr := url.Parse(raw); perr == nil {
		if sig := u.Query().Get("sig"); sig != "" {
			msg = strings.ReplaceAll(msg, sig, "****")
			msg = strings.ReplaceAll(msg, url.QueryEscape(sig), "****")
		}
	}
	return errors.New(msg)
}
