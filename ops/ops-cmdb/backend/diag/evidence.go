package diag

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// 证据提炼：把「日志里其实写着，只是没人看见」的那部分捞出来。
//
// # 为什么要有这一层
//
// 规则命中不了的时候，原来只把**日志末尾 8 行**塞进证据。而真正的报错常常
// 不在末尾 —— 它在中间，被后面的心跳/健康检查刷出去了。
//
// 实测 g50-dev/g50-plaza-game-server-backend（重启 11215 次）：
// 末尾 30 行**全是**每 3 秒一条的 `redis health check success`，
// 于是诊断只能说「容器启动后即崩溃，去看日志末尾」—— 而末尾没有任何报错。
//
// 🔴 这一层不做判定，只做**筛选和排版**：从更大的窗口里把像报错的行捞出来、
// 把重复的折叠掉、按原顺序摆好。它不会编造任何东西，因为输出的每一行
// 都是日志里原样存在的。
//
// # 它在分层里的位置
//
//	0 规则       确定性形态 → 精确根因+处置（证书到期日、nginx 行号、缺哪个 Secret）
//	1 证据提炼   规则没命中 → 把日志里真实存在的报错摆出来，让人自己判      ← 本文件
//	2 AI 兜底    第 1 层一条报错行都没捞到时才动
//
// 🔴 第 1 层的价值在于它**零成本**。能在这里解决的绝不该花钱问 AI ——
// 而且它给的是原始证据，比任何判定都更可信。

// 判定「这一行像不像报错」。
//
// ⚠️ 只用**通用**特征，不要往里加业务词。这一层的职责是筛选不是判定，
// 加业务词等于把规则库的活挪到这里做，而且做得更差（没有结构化输出）。
var errorLinePatterns = regexp.MustCompile(`(?i)\b(error|fatal|panic|exception|emerg|` +
	`fail(ed|ure)?|refused|denied|timeout|timed out|unable to|cannot|can't|` +
	`no such|not found|invalid|unauthorized|forbidden)\b`)

// 明显是正常输出的行，即使含上面的词也不算报错。
//
// 🔴 没有这一条会把「health check success」这种也捞进来 —— 一旦噪音进了
// 「报错行」列表，这一层就失去了全部价值：人还是得自己在里面找。
var notErrorPatterns = regexp.MustCompile(`(?i)(health check success|probe succeeded|` +
	`no error|0 error|errors?[:=]\s*0|success|\bok\b.*\b(200|204)\b)`)

// ExtractedEvidence 提炼结果。
type ExtractedEvidence struct {
	// Lines 捞出来的报错行（已折叠重复），保持日志里的先后顺序
	Lines []string `json:"lines"`
	// Scanned 一共看了多少行
	Scanned int `json:"scanned"`
	// Suppressed 因为重复被折叠掉的行数
	Suppressed int `json:"suppressed"`
	// NoErrorFound 扫完一条报错行都没有 —— 这是**要走 AI 的唯一前提**
	NoErrorFound bool `json:"no_error_found"`
	// Note 给人看的一句话说明
	Note string `json:"note"`
}

// digitsOnly 用来判断两行是不是「同一条日志的不同时刻」。
var digitsOnly = regexp.MustCompile(`[0-9]+`)

// ansiCodes 日志里的颜色码，折叠去重前要剥掉，否则同一行会因为颜色不同被当成两行。
var ansiCodes = regexp.MustCompile(`\x1b\[[0-9;]*m`)

// ExtractErrors 从整段日志里捞报错行。
//
// maxLines 是最多返回几条（够人判断即可，不是越多越好）。
func ExtractErrors(tail string, maxLines int) ExtractedEvidence {
	out := ExtractedEvidence{Lines: []string{}}
	if strings.TrimSpace(tail) == "" {
		out.NoErrorFound = true
		out.Note = "没有日志内容可供提炼"
		return out
	}
	lines := strings.Split(strings.TrimRight(tail, "\n"), "\n")
	out.Scanned = len(lines)

	type hit struct {
		text  string
		count int
		first int
	}
	seen := map[string]*hit{}
	order := []string{}

	for i, raw := range lines {
		l := strings.TrimSpace(ansiCodes.ReplaceAllString(raw, ""))
		if l == "" {
			continue
		}
		if !errorLinePatterns.MatchString(l) || notErrorPatterns.MatchString(l) {
			continue
		}
		// 折叠：把数字（时间戳/计数/ID）抹掉再比，否则同一条报错每秒一遍会占满列表
		key := digitsOnly.ReplaceAllString(l, "#")
		if h, ok := seen[key]; ok {
			h.count++
			continue
		}
		seen[key] = &hit{text: l, count: 1, first: i}
		order = append(order, key)
	}

	hits := make([]*hit, 0, len(order))
	for _, k := range order {
		hits = append(hits, seen[k])
	}
	// 保持日志里的先后顺序：**根因通常是第一条报错**，后面的多是它引发的连锁
	sort.SliceStable(hits, func(i, j int) bool { return hits[i].first < hits[j].first })

	for _, h := range hits {
		if len(out.Lines) >= maxLines {
			out.Suppressed += h.count
			continue
		}
		text := trimTo(h.text, 400)
		if h.count > 1 {
			text = fmt.Sprintf("%s（重复 %d 次）", text, h.count)
			out.Suppressed += h.count - 1
		}
		out.Lines = append(out.Lines, text)
	}

	switch {
	case len(out.Lines) == 0:
		out.NoErrorFound = true
		out.Note = fmt.Sprintf("扫了这个容器最近 %d 行日志，**一条报错都没有**。"+
			"应用是在没有任何自述的情况下退出的 —— 多为被外部强制终止（探针判死/优雅停机超时/OOM），"+
			"或它把错误写到了别处（stderr 被吞、写进了文件而不是标准输出）。"+
			"先看事件里有没有 Killing/OOMKilled；要查更早的历史用 query_loki", out.Scanned)
	default:
		out.Note = fmt.Sprintf("从最近 %d 行日志里捞出 %d 条报错（已折叠重复）。"+
			"⚠️ **第一条通常最接近根因**，后面的多是它引发的连锁反应", out.Scanned, len(out.Lines))
	}
	return out
}
