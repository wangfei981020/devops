// GKE 官网版本排期表抓取与解析。
//
// 为什么要抓 HTML：GKE 的「某个小版本什么时候自动升级」只发布在文档页上，**没有 API**。
// container API 只在升级临近时才给 autoUpgradeStartTime，提前一个月预警必须靠这张表。
// 来源 https://docs.cloud.google.com/kubernetes-engine/docs/release-schedule
//
// 解析上的两个坑（2026-07-31 实测确认，别按直觉改）：
//
//  1. 表头是两层，且表头单元格数(15) ≠ 数据行列数(11)。
//     按表头顺序平铺映射会整体错位，必须按 colspan/rowspan 展开：
//     Minor(rowspan=2) | Rapid(colspan=2) | Regular(colspan=2) | Stable(colspan=2)
//     | Extended(colspan=2) | EOS标准(rowspan=2) | EOS扩展(rowspan=2)
//     → 1 + 2×4 + 1 + 1 = 11，与数据行吻合。
//
//  2. 日期有三种粒度，且脚注数字直接粘在值尾（`2026-08` + 脚注4 → 抓下来是 `2026-084`）。
//     官网原文：Dates with only a month (2025-03) or quarter year (2025-Q3) are approximations
//     that will be updated with a date when it is known.
//     所以每个日期都要带 precision 落库，前端和飞书卡片在非 day 粒度时必须注明「日期会变」。
package k8ssource

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"golang.org/x/net/html"

	"opsplatform-cmdb-backend/logx"
)

const gkeScheduleURL = "https://docs.cloud.google.com/kubernetes-engine/docs/release-schedule"

// DateVal 一个排期日期：原文 + 归一化日期 + 粒度。
// Date 为 nil 表示没解析出来（原文可能是 "N/A" 之类），此时 Precision=unknown。
type DateVal struct {
	Raw       string `json:"raw"`
	Date      string `json:"date"`      // YYYY-MM-DD，归一化到当月/当季首日；空=未解析出
	Precision string `json:"precision"` // day/month/quarter/unknown
}

// ScheduleRow 一个 (小版本, 通道) 的排期。EOS 是版本级属性，四个通道行冗余同值。
type ScheduleRow struct {
	MinorVersion string  `json:"minor_version"`
	Channel      string  `json:"channel"` // RAPID/REGULAR/STABLE/EXTENDED
	Available    DateVal `json:"available"`
	AutoUpgrade  DateVal `json:"auto_upgrade"`
	EOSStandard  DateVal `json:"eos_standard"`
	EOSExtended  DateVal `json:"eos_extended"`
}

// 已知通道名 → 落库枚举。表头里出现未登记的通道名会 WARN（官网加通道时能第一时间发现）。
var scheduleChannels = map[string]string{
	"rapid": "RAPID", "regular": "REGULAR", "stable": "STABLE", "extended": "EXTENDED",
}

var (
	reDay      = regexp.MustCompile(`^(\d{4})-(\d{2})-(\d{2})$`)
	reMonth    = regexp.MustCompile(`^(\d{4})-(\d{2})$`)
	reQuarter  = regexp.MustCompile(`^(\d{4})-Q([1-4])$`)
	reMinorVer = regexp.MustCompile(`^\d+\.\d+`)
)

// FetchGKESchedule 抓官网排期表并解析。
// 任何解析异常都返回 error，调用方据此保留上次数据（绝不能用半截结果覆盖）。
func FetchGKESchedule(ctx context.Context) ([]ScheduleRow, error) {
	doc, err := fetchScheduleDoc(ctx)
	if err != nil {
		return nil, err
	}
	table := findFirstTable(doc)
	if table == nil {
		return nil, fmt.Errorf("排期页未找到 table（页面结构可能已变）")
	}
	trs := findAll(table, "tr")
	if len(trs) < 3 {
		return nil, fmt.Errorf("排期表行数异常: %d（至少需要 2 行表头 + 1 行数据）", len(trs))
	}

	cols, err := expandHeader(trs[0], trs[1])
	if err != nil {
		return nil, err
	}
	logx.J("gke_schedule", "header_expanded", map[string]any{"cols": len(cols)})

	rows := []ScheduleRow{}
	skipped := 0
	for _, tr := range trs[2:] {
		cells := findAll(tr, "td")
		if len(cells) == 0 {
			cells = findAll(tr, "th")
		}
		if len(cells) == 0 {
			continue
		}
		// 列数不符说明页面结构变了。跳过该行并 WARN，不猜、不硬填。
		if len(cells) != len(cols) {
			skipped++
			logx.J("gke_schedule", "row_col_mismatch", map[string]any{
				"want": len(cols), "got": len(cells), "first_cell": textOf(cells[0]),
			})
			continue
		}
		ver := strings.TrimSpace(textOf(cells[0]))
		if m := reMinorVer.FindString(ver); m != "" {
			ver = m // 表格里版本可能带 "(release date)" 之类后缀
		} else {
			skipped++
			logx.J("gke_schedule", "bad_minor_version", map[string]any{"raw": ver})
			continue
		}

		// 先取版本级的两个 EOS，再按通道分组装行
		byChannel := map[string]*ScheduleRow{}
		var eosStd, eosExt DateVal
		for i, c := range cols[1:] {
			val := parseDateVal(textOf(cells[i+1]))
			switch {
			case c.Sub == "" && strings.Contains(strings.ToLower(c.Group), "standard"):
				eosStd = val
			case c.Sub == "" && strings.Contains(strings.ToLower(c.Group), "extended"):
				eosExt = val
			case c.Sub != "":
				ch, ok := scheduleChannels[strings.ToLower(c.Group)]
				if !ok {
					logx.J("gke_schedule", "unknown_channel", map[string]any{"group": c.Group, "version": ver})
					continue
				}
				r := byChannel[ch]
				if r == nil {
					r = &ScheduleRow{MinorVersion: ver, Channel: ch}
					byChannel[ch] = r
				}
				if strings.Contains(strings.ToLower(c.Sub), "auto") {
					r.AutoUpgrade = val
				} else {
					r.Available = val
				}
			}
		}
		for _, r := range byChannel {
			r.EOSStandard, r.EOSExtended = eosStd, eosExt
			rows = append(rows, *r)
		}
	}

	if len(rows) == 0 {
		return nil, fmt.Errorf("排期表解析出 0 行（跳过 %d 行，页面结构可能已变）", skipped)
	}
	logx.J("gke_schedule", "parsed", map[string]any{"rows": len(rows), "skipped_rows": skipped})
	return rows, nil
}

func fetchScheduleDoc(ctx context.Context) (*html.Node, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, gkeScheduleURL, nil)
	if err != nil {
		return nil, err
	}
	// 不带 UA 有被文档站拦的风险；这里声明成普通浏览器。
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; opsplatform-cmdb/1.0)")
	// http.Client 默认跟随重定向（含跨域）——cloud.google.com 已 301 到 docs.cloud.google.com。
	cli := &http.Client{Timeout: 30 * time.Second}
	resp, err := cli.Do(req)
	if err != nil {
		return nil, fmt.Errorf("抓排期页: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("抓排期页 HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, fmt.Errorf("读排期页: %w", err)
	}
	return html.Parse(strings.NewReader(string(body)))
}

// colSpec 展开后的一个数据列：Group=第一层表头，Sub=第二层（rowspan=2 的列 Sub 为空）。
type colSpec struct{ Group, Sub string }

// expandHeader 按 colspan/rowspan 把两层表头展开成与数据行一一对应的列清单。
func expandHeader(tr1, tr2 *html.Node) ([]colSpec, error) {
	h1, h2 := findAll(tr1, "th"), findAll(tr2, "th")
	if len(h1) == 0 {
		return nil, fmt.Errorf("第一层表头为空")
	}
	cols, i2 := []colSpec{}, 0
	for _, th := range h1 {
		name := stripFootnote(textOf(th))
		cs, rs := attrInt(th, "colspan", 1), attrInt(th, "rowspan", 1)
		if rs >= 2 {
			cols = append(cols, colSpec{Group: name}) // 跨两行，没有子表头
			continue
		}
		for k := 0; k < cs; k++ {
			sub := ""
			if i2 < len(h2) {
				sub = stripFootnote(textOf(h2[i2]))
				i2++
			} else {
				return nil, fmt.Errorf("第二层表头不足：%q 需要 %d 个子列，只剩 %d", name, cs, len(h2)-i2)
			}
			cols = append(cols, colSpec{Group: name, Sub: sub})
		}
	}
	if i2 != len(h2) {
		return nil, fmt.Errorf("第二层表头有 %d 个未被消费（表头结构已变）", len(h2)-i2)
	}
	return cols, nil
}

// stripFootnote 去掉表头文字尾部的脚注编号（"Available1" → "Available"）。
func stripFootnote(s string) string {
	s = strings.TrimSpace(s)
	return strings.TrimSpace(strings.TrimRight(s, "0123456789"))
}

// parseDateVal 解析一个排期单元格。
// 脚注数字直接粘在值尾（"2026-08"+脚注4 → "2026-084"），所以先按原文匹配，
// 匹配不上再削掉最后一位数字重试一次——只重试一次，避免把 "2026-0" 这种残值也当成功。
func parseDateVal(raw string) DateVal {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return DateVal{Raw: raw, Precision: "unknown"}
	}
	for attempt, s := 0, raw; attempt < 2; attempt, s = attempt+1, trimLastDigit(s) {
		if m := reDay.FindStringSubmatch(s); m != nil {
			return DateVal{Raw: s, Date: s, Precision: "day"}
		}
		if m := reMonth.FindStringSubmatch(s); m != nil {
			return DateVal{Raw: s, Date: m[1] + "-" + m[2] + "-01", Precision: "month"}
		}
		if m := reQuarter.FindStringSubmatch(s); m != nil {
			q, _ := strconv.Atoi(m[2])
			return DateVal{Raw: s, Date: fmt.Sprintf("%s-%02d-01", m[1], (q-1)*3+1), Precision: "quarter"}
		}
	}
	// 解析不出来不是致命错误（官网偶有 "N/A"），但必须让人看见
	logx.J("gke_schedule", "unparsed_date", map[string]any{"raw": raw})
	return DateVal{Raw: raw, Precision: "unknown"}
}

func trimLastDigit(s string) string {
	if s != "" && s[len(s)-1] >= '0' && s[len(s)-1] <= '9' {
		return s[:len(s)-1]
	}
	return s
}

// ---- 极简 HTML 遍历helpers（只够本文件用，不外泄）----

func findFirstTable(n *html.Node) *html.Node {
	if n.Type == html.ElementNode && n.Data == "table" {
		return n
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if r := findFirstTable(c); r != nil {
			return r
		}
	}
	return nil
}

// findAll 收集后代中所有指定标签（tr 里找 td 不会串到嵌套表格，因为官网这张表没有嵌套）。
func findAll(n *html.Node, tag string) []*html.Node {
	out := []*html.Node{}
	var walk func(*html.Node)
	walk = func(x *html.Node) {
		if x.Type == html.ElementNode && x.Data == tag {
			out = append(out, x)
			return // 不递归进同名标签内部
		}
		for c := x.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		walk(c)
	}
	return out
}

func textOf(n *html.Node) string {
	var b strings.Builder
	var walk func(*html.Node)
	walk = func(x *html.Node) {
		if x.Type == html.TextNode {
			b.WriteString(x.Data)
		}
		for c := x.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(n)
	return strings.Join(strings.Fields(b.String()), " ")
}

func attrInt(n *html.Node, key string, def int) int {
	for _, a := range n.Attr {
		if a.Key == key {
			if v, err := strconv.Atoi(strings.TrimSpace(a.Val)); err == nil {
				return v
			}
		}
	}
	return def
}
