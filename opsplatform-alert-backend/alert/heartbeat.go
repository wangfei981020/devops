package alert

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"opsplatform-alert-backend/database"
	lokiclient "opsplatform-alert-backend/loki"
	"opsplatform-alert-backend/models"
	"opsplatform-alert-backend/notify"
	"opsplatform-alert-backend/timezone"
)

// Heartbeat mode watches for business activity that stops.
//
// The other two modes ask "did this line appear" (found) or "did this container
// go quiet" (not_found). Neither fits "room 46001 at site BingoPlus has had
// nobody join for fifteen minutes", because the thing being watched is not a
// stream — it is a combination of values that only exist inside the log text.
//
// The naive shape of this — one query per combination — dies immediately at
// real scale: a hundred sites times a hundred rooms is ten thousand Loki
// queries every five minutes. So the query count here is fixed at two,
// regardless of how many combinations exist:
//
//	current  = sum by (dims) (count_over_time(<selector+filter> | regexp <pattern> [15m]))
//	baseline = the same thing over 7d, cached
//
// Whatever the baseline has and the current window does not is what has gone
// quiet. Adding rooms or sites changes neither the query count nor its cost.

const (
	// heartbeatBaselineTTL is how long a computed baseline is reused.
	//
	// The baseline query spans days and is genuinely expensive — measured
	// against this platform's own Loki, a 7d aggregation over a busy container
	// times out. Running it every cycle is not an option, and it does not need
	// to be current: "which rooms normally have traffic" changes on the scale of
	// days, not minutes.
	heartbeatBaselineTTL = time.Hour

	// heartbeatMaxListed caps how many missing combinations one card lists.
	// Beyond this the message stops being readable and the useful information
	// is the count, not the enumeration.
	heartbeatMaxListed = 20
)

// hbDim is one dimension combination and how many lines it had in the window.
type hbDim struct {
	Key    string            // 稳定排序后的组合键，用于比对
	Labels map[string]string // site_id → …, room_id → …
	Count  float64
	// MinHourly 是基线窗口里「最冷那一小时」的条数，只有基线才有（当前窗口不需要）。
	//
	// 它比 Count 准，因为 Count 会被高峰时段撑起来。实测房间 1013 在 24h 里有 130+
	// 条，看总数像个正常房间，但其中有两个整小时一条都没有——那两小时用 5 分钟窗口
	// 去监控，必然误报。
	MinHourly float64
	// ActiveHours / ExpectHours：基线窗口里有日志的小时数 与 应有的小时数。
	// 两者不等就说明存在完全空窗的小时，而 Loki 不会为空窗返回任何点，
	// 所以只能靠这两个数的差额发现它们。
	ActiveHours int
	ExpectHours int
}

// heartbeatDimNames pulls the dimension names out of the extraction pattern.
//
// The named capture groups ARE the dimensions — `(?P<site_id>\d+)` means
// "aggregate by site_id". Deriving them from the pattern rather than asking for
// a separate list removes the chance of the two disagreeing, which would show
// up as an aggregation on a label that never exists and therefore a single
// empty group.
func heartbeatDimNames(pattern string) ([]string, error) {
	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil, fmt.Errorf("维度提取正则无法编译: %w", err)
	}
	var dims []string
	for _, n := range re.SubexpNames() {
		if n != "" {
			dims = append(dims, n)
		}
	}
	if len(dims) == 0 {
		return nil, fmt.Errorf("维度提取正则里没有命名组，形如 (?P<room_id>\\d+) 的命名组才是聚合维度")
	}
	return dims, nil
}

// buildHeartbeatQuery wraps the rule's own selector and line filter into an
// aggregation.
//
// The rule stores the part a person writes — `{container="x"} |= "加入房间"` —
// and this adds the extraction and the sum. Backticks delimit the regex so the
// author does not have to double every backslash; a pattern containing one is
// rejected rather than silently producing a broken query.
func buildHeartbeatQuery(logql, pattern string, dims []string, rng string) (string, error) {
	logql = strings.TrimSpace(logql)
	if logql == "" {
		return "", fmt.Errorf("LogQL 查询不能为空")
	}
	if !strings.HasPrefix(logql, "{") {
		return "", fmt.Errorf("心跳模式的 LogQL 要从流选择器开始，形如 {container=\"x\"} |= \"关键词\"")
	}
	if strings.Contains(pattern, "`") {
		return "", fmt.Errorf("维度提取正则不能包含反引号")
	}
	if rng == "" {
		rng = "5m"
	}
	return fmt.Sprintf("sum by (%s) (count_over_time(%s | regexp `%s` [%s]))",
		strings.Join(dims, ", "), logql, pattern, rng), nil
}

// dimKey builds a stable identity for a combination.
//
// Sorted, so the same combination always produces the same key no matter what
// order Loki returned the labels in — the key is compared against a previous
// run's, and an unstable one would read every cycle as "the set changed".
func dimKey(labels map[string]string, dims []string) string {
	parts := make([]string, 0, len(dims))
	for _, d := range dims {
		parts = append(parts, d+"="+labels[d])
	}
	sort.Strings(parts)
	return strings.Join(parts, "|")
}

// LokiClientFunc 按连接 id 取一个 Loki 客户端。引擎和 handlers 各有自己的取法，
// 心跳的判定逻辑因此不绑死在 Engine 上——预览要复用同一套判定，而不是照着再写
// 一遍；写两遍的结果一定是预览说会告警、实际不告，或者反过来。
type LokiClientFunc func(int) (*lokiclient.Client, error)

// queryHeartbeatDims runs one aggregation and returns the combinations it found.
func queryHeartbeatDims(ctx context.Context, rule *models.AlertRule, dims []string, rng string, getClient LokiClientFunc) (map[string]hbDim, string, error) {
	q, err := buildHeartbeatQuery(rule.LogQL, rule.DimPattern, dims, rng)
	if err != nil {
		return nil, "", err
	}
	client, err := getClient(rule.LokiConnectionID)
	if err != nil {
		return nil, q, err
	}
	samples, err := client.QueryInstant(ctx, q, time.Now())
	if err != nil {
		return nil, q, err
	}

	out := make(map[string]hbDim, len(samples))
	for _, s := range samples {
		// A sample with no dimension labels is the aggregation of every line the
		// extraction failed on. It is not a combination and must not be treated
		// as one — but it IS worth knowing about, so it gets logged by the
		// caller rather than silently dropped.
		empty := true
		for _, d := range dims {
			if s.Labels[d] != "" {
				empty = false
				break
			}
		}
		if empty {
			continue
		}
		k := dimKey(s.Labels, dims)
		out[k] = hbDim{Key: k, Labels: s.Labels, Count: s.Value}
	}
	return out, q, nil
}

// heartbeatBaselineStep 是基线的分桶宽度。
//
// 一小时既是业务上「这个房间空了多久算不正常」的自然单位，也让 7d 窗口只有 168
// 个点——足够细到能看出空窗，又不至于让响应大到要分页。
const heartbeatBaselineStep = time.Hour

// queryHeartbeatHourly 用一次区间聚合同时得到每个组合的总次数和最冷小时次数。
//
// 这里从 instant 查询换成了 step=1h 的区间查询，换来两样东西：
//
//  1. min_hourly。LogQL 不支持 PromQL 那种子查询，
//     `min_over_time(sum by (...) (...)[7d:1h])` 会被直接拒掉
//     （parse error: unexpected SUM），所以逐小时序列只能拉回来在 Go 里取最小值。
//  2. 顺带绕开了 `[7d]` 一次性聚合的超时——分桶后每个子查询只覆盖一小时，
//     而扫描的日志总量不变（各桶互不重叠）。
//
// 🔴 空窗的小时在响应里是整段缺失的，不是值为 0 的点。对返回的点取 min 算出来的是
// 「有流量的那些小时里的最小值」，恰好问反了。所以这里拿点数和期望桶数比，缺多少
// 就说明有多少个整小时是零。
func queryHeartbeatHourly(ctx context.Context, rule *models.AlertRule, dims []string, rng string,
	getClient LokiClientFunc) (map[string]hbDim, string, error) {

	q, err := buildHeartbeatQuery(rule.LogQL, rule.DimPattern, dims, "1h")
	if err != nil {
		return nil, "", err
	}
	client, err := getClient(rule.LokiConnectionID)
	if err != nil {
		return nil, q, err
	}
	dur, err := parseRangeDuration(rng)
	if err != nil {
		return nil, q, err
	}

	// 对齐到整点：不对齐的话最后一个桶只覆盖不足一小时，次数天然偏低，
	// 会让几乎每个组合的 min_hourly 都被那个残桶拉到很小。
	end := time.Now().Truncate(heartbeatBaselineStep)
	start := end.Add(-dur)

	series, err := client.QueryMatrix(ctx, q, start, end, heartbeatBaselineStep)
	if err != nil {
		return nil, q, err
	}
	return summarizeHourly(series, dims, expectedBuckets(dur, heartbeatBaselineStep)), q, nil
}

// expectedBuckets 是「这个窗口应该有多少个整桶」。
//
// 取 floor 而不是 floor+1 是刻意的，虽然 Loki 两端都给点、铺满窗口的序列实际会有
// floor+1 个。留这一个点的余量是因为两个方向的代价不对称：少算一个桶只会漏判一个
// 空窗小时；多算一个桶会让铺满整个窗口的组合全部被判成「有空窗」，于是所有房间
// 一起静默掉出监控范围——没有任何告警，也没有任何报错。
func expectedBuckets(window, step time.Duration) int {
	if step <= 0 || window < step {
		return 0
	}
	return int(window / step)
}

// summarizeHourly 把逐小时序列折成每个组合的总次数与最冷小时次数。
//
// expect 是这个窗口应该有多少个整小时。它取 floor(窗口/步长) 而不是 +1：Loki 两端
// 都会给点，铺满窗口的序列实际点数通常是 floor+1，这里故意留一个点的余量。方向是
// 刻意选的——宁可漏判一个空窗小时，也不能因为端点算差一个就把铺满整个窗口的组合
// 误判成「有空窗」，那会让所有房间一起静默掉出监控范围。
func summarizeHourly(series []lokiclient.MatrixSeries, dims []string, expect int) map[string]hbDim {
	out := make(map[string]hbDim, len(series))
	for _, sr := range series {
		// 没有任何维度标签的那条是「正则没提取出来」的合计，不是一个组合。
		empty := true
		for _, d := range dims {
			if sr.Labels[d] != "" {
				empty = false
				break
			}
		}
		if empty {
			continue
		}
		total := 0.0
		minHourly := math.Inf(1)
		for _, v := range sr.Values {
			total += v
			if v < minHourly {
				minHourly = v
			}
		}
		// 🔴 缺的桶就是零。Loki 不为空窗返回点，所以只有拿点数和期望桶数比才能
		// 发现它们；对返回的点取 min 得到的是「有流量的那些小时里的最小值」。
		if len(sr.Values) < expect || math.IsInf(minHourly, 1) {
			minHourly = 0
		}
		k := dimKey(sr.Labels, dims)
		out[k] = hbDim{
			Key: k, Labels: sr.Labels, Count: total,
			MinHourly: minHourly, ActiveHours: len(sr.Values), ExpectHours: expect,
		}
	}
	return out
}

// parseRangeDuration 认 LogQL 的窗口写法，其中 d 是 time.ParseDuration 不认的。
func parseRangeDuration(rng string) (time.Duration, error) {
	if strings.HasSuffix(rng, "d") {
		n, err := strconv.Atoi(strings.TrimSuffix(rng, "d"))
		if err != nil || n <= 0 {
			return 0, fmt.Errorf("基线窗口 %q 无法解析", rng)
		}
		return time.Duration(n) * 24 * time.Hour, nil
	}
	d, err := time.ParseDuration(rng)
	if err != nil || d <= 0 {
		return 0, fmt.Errorf("基线窗口 %q 无法解析", rng)
	}
	return d, nil
}

func heartbeatBaselineKey(ruleID int) string {
	return fmt.Sprintf("alert:hb:baseline:%d", ruleID)
}

// heartbeatBaseline returns the set of combinations that should have traffic,
// recomputing it at most once per heartbeatBaselineTTL.
func heartbeatBaseline(ctx context.Context, rule *models.AlertRule, dims []string, getClient LokiClientFunc, useCache bool) (map[string]hbDim, bool, error) {
	key := heartbeatBaselineKey(rule.ID)
	if useCache {
		if raw, err := database.RDB.Get(ctx, key).Result(); err == nil && raw != "" {
			var cached map[string]hbDim
			if json.Unmarshal([]byte(raw), &cached) == nil && len(cached) > 0 {
				return cached, true, nil
			}
		}
	}

	rng := baselineRangeOf(rule)
	baseline, q, err := queryHeartbeatHourly(ctx, rule, dims, rng, getClient)
	if err != nil {
		return nil, false, fmt.Errorf("基线查询失败(%s): %w", rng, err)
	}
	log.Printf("[Heartbeat] rule %d: 基线窗口 %s 得到 %d 个组合, query=%s", rule.ID, rng, len(baseline), q)

	if b, err := json.Marshal(baseline); err == nil {
		database.RDB.Set(ctx, key, b, heartbeatBaselineTTL)
	}
	return baseline, false, nil
}

// InvalidateHeartbeatBaseline drops a rule's cached baseline, so an edit to the
// query or the window takes effect on the next run instead of up to an hour
// later.
func InvalidateHeartbeatBaseline(ctx context.Context, ruleID int) {
	database.RDB.Del(ctx, heartbeatBaselineKey(ruleID))
}

// HeartbeatEntry 是一个维度组合及其基线次数。判定结果里的「异常」和「正常」
// 两个列表都用它——预览要把两边都摆出来，只给异常的话没法判断阈值卡得合不合适。
type HeartbeatEntry struct {
	Key      string
	Labels   map[string]string
	Baseline float64
	SiteID   string
	RoomID   string
	// MinHourly / ActiveHours / ExpectHours 来自基线，用来解释这个组合为什么被
	// 纳入或排除。预览必须显示它们：否则「为什么这个房间不在监控里」只能靠猜。
	MinHourly   float64
	ActiveHours int
	ExpectHours int
	// WhyWatching 在「观察中」列表里说明差在哪一项（总次数还是最冷小时）。
	WhyWatching string
}

// executeHeartbeat is the whole mode: two queries, a set difference, one card.
// HeartbeatResult 是一次心跳判定的完整结果。
//
// 发送路径和预览共用这一个函数：预览若照着判定逻辑再写一遍，迟早会出现「预览说
// 会告警、实际不告」或者反过来的情况，而那种不一致比没有预览更糟——人会照着预览
// 去调阈值。
type HeartbeatResult struct {
	Dims          []string
	CurrentQuery  string
	BaselineQuery string
	Baseline      map[string]hbDim
	Current       map[string]hbDim
	Missing       []HeartbeatEntry // 基线有、当前窗口没有，且通过了低频与字典过滤
	Alive         []HeartbeatEntry // 当前窗口有活动的，预览要列出来当对照
	// Watching 是在监控范围内（站点已关注、房间在用）但基线还不够判定的组合。
	//
	// 以前这批只有一个计数，于是新上线的房间是「静默不监控」——字典里有、告警里
	// 永远不出现，没有任何地方提醒你。列出来之后至少能看到它在攒基线，以及还差多少。
	Watching          []HeartbeatEntry
	SkippedLowTraffic int
	SkippedByDict     int
	Dict              *Dict
	BaselineCached    bool
	// BaselineRange 是实际生效的窗口（规则留空时是默认的 7d），让调用方不必
	// 自己再推一遍默认值——推错了显示出来的数字会和实际查询对不上。
	BaselineRange string
}

// Monitored 是真正被监控的组合数：基线里过了站点白名单、房间在用、基线次数三道
// 门槛的那些。它等于 活跃 + 异常。
//
// 单看基线总数会高估监控范围——基线里有大量被过滤掉的组合，把它当成「在监控 72 个」
// 会让人以为覆盖面比实际大得多。
func (r *HeartbeatResult) Monitored() int { return len(r.Alive) + len(r.Missing) }

// EvaluateHeartbeat 跑完两次聚合并算出差集，不发送、不写任何状态。
//
// useBaselineCache=false 时强制重算基线：预览里改了查询或维度正则后，缓存里那份
// 是按旧配置算的，拿它对照会给出误导的结果。
func EvaluateHeartbeat(ctx context.Context, rule *models.AlertRule, getClient LokiClientFunc, useBaselineCache bool) (*HeartbeatResult, error) {
	dims, err := heartbeatDimNames(rule.DimPattern)
	if err != nil {
		return nil, err
	}

	current, curQuery, err := queryHeartbeatDims(ctx, rule, dims, rule.TimeRange, getClient)
	if err != nil {
		return nil, fmt.Errorf("当前窗口查询失败: %w", err)
	}

	baseline, cached, err := heartbeatBaseline(ctx, rule, dims, getClient, useBaselineCache)
	if err != nil {
		return nil, err
	}
	// 基线现在是 step=1h 的区间查询，展示出来的语句必须是真正执行的那条（窗口 1h），
	// 不能写成 baselineRangeOf(rule)——那是区间的总跨度，不是分桶宽度。照着错的语句
	// 去 Loki 里手动验证会得到完全不同的数字。
	baseQuery, _ := buildHeartbeatQuery(rule.LogQL, rule.DimPattern, dims, "1h")

	// 字典是可选的：没有它照样告警，只是消息里显示原始 id。名字丢了是可读性问题，
	// 不告警是监控问题。
	var dict *Dict
	if rule.DictSourceID > 0 {
		d, dErr := GetDict(ctx, rule.DictSourceID)
		if dErr != nil {
			log.Printf("[Heartbeat] rule %d: 字典不可用，本轮用原始 id: %v", rule.ID, dErr)
		} else {
			dict = d
		}
	}

	res := &HeartbeatResult{
		Dims: dims, CurrentQuery: curQuery, BaselineQuery: baseQuery,
		Baseline: baseline, Current: current, Dict: dict, BaselineCached: cached,
		BaselineRange: baselineRangeOf(rule),
	}
	classifyHeartbeat(res, float64(rule.BaselineMinHits), float64(rule.BaselineMinHourly))

	sort.Slice(res.Missing, func(i, j int) bool { return res.Missing[i].Baseline > res.Missing[j].Baseline })
	sort.Slice(res.Alive, func(i, j int) bool { return res.Alive[i].Baseline > res.Alive[j].Baseline })
	// 观察中按「最接近达标」排前面：调阈值时先看的就是差一点的那几个。
	sort.Slice(res.Watching, func(i, j int) bool { return res.Watching[i].Baseline > res.Watching[j].Baseline })
	return res, nil
}

// executeHeartbeat 是告警路径：判定交给 EvaluateHeartbeat，这里只负责节流和发送。
func (e *Engine) executeHeartbeat(ctx context.Context, rule *models.AlertRule,
	sender notify.Notifier, atUsers []models.AtUser, atAll bool, ruleIDStr string) {

	res, err := EvaluateHeartbeat(ctx, rule, e.getLokiClient, true)
	if err != nil {
		log.Printf("[Heartbeat] rule %d: %v", rule.ID, err)
		database.DB.Exec("UPDATE alert_rules SET last_error=? WHERE id=?", err.Error(), rule.ID)
		return
	}
	missing := res.Missing
	baseline, current, curQuery := res.Baseline, res.Current, res.CurrentQuery
	dict := res.Dict

	log.Printf("[Heartbeat] rule %d: 基线 %d → 实际监控 %d（站点/房间过滤 %d，低频跳过 %d）| 活跃 %d / 异常 %d | Loki 当前返回 %d 个组合 | query=%s",
		rule.ID, len(baseline), res.Monitored(), res.SkippedByDict, res.SkippedLowTraffic,
		len(res.Alive), len(missing), len(current), curQuery)
	for _, m := range missing {
		// id 不进告警消息，但必须留在日志里：排查时要靠它回到运维平台和 Loki。
		log.Printf("[Heartbeat] rule %d MISSING site_id=%s room_id=%s baseline=%.0f",
			rule.ID, m.SiteID, m.RoomID, m.Baseline)
	}

	stateKey := fmt.Sprintf("alert:hb:state:%d", rule.ID)
	if len(missing) == 0 {
		if prev, _ := database.RDB.Get(ctx, stateKey).Result(); prev != "" && rule.RecoveryEnabled == 1 {
			e.sendHeartbeatRecovery(ctx, rule, sender, atUsers, atAll, ruleIDStr, len(baseline))
		}
		database.RDB.Del(ctx, stateKey)
		return
	}

	// The alert interval must not silence a NEW outage. Keying the throttle on
	// the exact set of missing combinations means a repeat of the same problem
	// waits, while one more room going quiet gets through immediately.
	sig := heartbeatSignature(missing)
	if prev, _ := database.RDB.Get(ctx, stateKey).Result(); prev == sig {
		if rule.AlertInterval == AlertIntervalOnce {
			log.Printf("[Heartbeat] rule %d: 异常集合未变且设为只告警一次，跳过", rule.ID)
			return
		}
		if interval := heartbeatInterval(rule.AlertInterval); interval > 0 {
			lastKey := stateKey + ":last"
			if lastStr, _ := database.RDB.Get(ctx, lastKey).Result(); lastStr != "" {
				if last, pErr := time.Parse(time.RFC3339, lastStr); pErr == nil && time.Since(last) < interval {
					log.Printf("[Heartbeat] rule %d: 异常集合未变且未到告警间隔，跳过", rule.ID)
					return
				}
			}
		}
	}

	title, message := e.renderHeartbeat(rule, dict, missing, len(baseline), len(current))
	resp, sErr := sender.SendCard(title, message, rule.Severity, atUsers, atAll)

	// The ids stay out of the card but go into the log record, so the detail
	// page can answer "which room was that" without anyone opening a terminal.
	raw, _ := json.Marshal(map[string]interface{}{
		"missing":       missing,
		"baseline_size": len(baseline),
		"current_size":  len(current),
		"query":         curQuery,
		"dict_version":  dictVersionOf(dict),
		"dict_stale":    dict != nil && dict.Stale,
	})

	if sErr != nil {
		saveAlertLog(rule, message, string(raw), "failed", sErr.Error(), resp)
		if Metrics != nil {
			Metrics.RecordAlertFired(ruleIDStr, rule.Name, rule.Severity)
			Metrics.RecordSendFailed(ruleIDStr, rule.Name, rule.Severity)
		}
		return
	}
	saveAlertLog(rule, message, string(raw), "success", partialSendNote(resp), resp)
	if Metrics != nil {
		Metrics.RecordAlertFired(ruleIDStr, rule.Name, rule.Severity)
		Metrics.RecordSendSuccess(ruleIDStr, rule.Name, rule.Severity)
	}
	database.RDB.Set(ctx, stateKey, sig, 7*24*time.Hour)
	database.RDB.Set(ctx, stateKey+":last", time.Now().Format(time.RFC3339), 7*24*time.Hour)
	log.Printf("[Heartbeat] rule %d: 已发送，异常 %d 个", rule.ID, len(missing))
}

func dictVersionOf(d *Dict) string {
	if d == nil {
		return ""
	}
	return d.Version
}

// heartbeatSignature identifies a set of missing combinations, so a repeat of
// the same outage can be told apart from a new one.
func heartbeatSignature(missing []HeartbeatEntry) string {
	keys := make([]string, 0, len(missing))
	for _, m := range missing {
		keys = append(keys, m.Key)
	}
	sort.Strings(keys)
	h := sha256.Sum256([]byte(strings.Join(keys, ";")))
	return hex.EncodeToString(h[:])[:16]
}

// renderHeartbeat writes the card. Names only — the ids live in the log and in
// the alert record, because a nineteen-digit number in a chat message costs a
// line of space and tells the reader nothing they can act on.
func (e *Engine) renderHeartbeat(rule *models.AlertRule, dict *Dict, missing []HeartbeatEntry, baselineSize, currentSize int) (string, string) {
	title := rule.MessageTitle
	if title == "" {
		title = rule.Name
	}

	var b strings.Builder
	fmt.Fprintf(&b, "**级别:** %s | **%s 内无活动:** %d / %d\n",
		rule.Severity, rule.TimeRange, len(missing), baselineSize)

	if dict != nil && dict.Stale {
		fmt.Fprintf(&b, "**⚠ 名称可能过期:** 字典最后同步于 %s（%s）\n",
			timezone.Format(dict.SyncedAt), truncateForLog(dict.StaleWhy, 80))
	}

	shown := missing
	if len(shown) > heartbeatMaxListed {
		shown = shown[:heartbeatMaxListed]
	}
	for i, m := range shown {
		fmt.Fprintf(&b, "\n—— %d/%d ——\n", i+1, len(missing))
		if m.SiteID != "" {
			fmt.Fprintf(&b, "**站点:** %s\n", dict.SiteLabel(m.SiteID))
		}
		if m.RoomID != "" {
			fmt.Fprintf(&b, "**房间:** %s\n", dict.RoomLabel(m.RoomID))
		}
		// Dimensions other than the two known ones still get printed; a rule
		// watching something else entirely should not produce a blank card.
		for k, v := range m.Labels {
			if k != "site_id" && k != "room_id" {
				fmt.Fprintf(&b, "**%s:** %s\n", k, v)
			}
		}
		// 基线行同时给总次数和最冷小时：光看总次数判断不了"这个房间平时是不是本来
		// 就会安静一阵"，而那正是决定要不要立刻去现场的依据。
		if m.ExpectHours > 0 {
			// 铺满窗口时不显示分数：期望桶数刻意留了一个桶的余量，铺满的序列实际点数
			// 会比期望多一个，照原样显示就成了「25/24 小时有活动」——数字没错，但读起来
			// 像个 bug，而这张卡片是给正在处理故障的人看的。
			if m.ActiveHours >= m.ExpectHours {
				fmt.Fprintf(&b, "**基线:** %s 内 %.0f 次，最冷一小时 %.0f 次（每小时都有活动）\n",
					baselineRangeOf(rule), m.Baseline, m.MinHourly)
			} else {
				fmt.Fprintf(&b, "**基线:** %s 内 %.0f 次，最冷一小时 %.0f 次（%d 个整小时没有日志）\n",
					baselineRangeOf(rule), m.Baseline, m.MinHourly, m.ExpectHours-m.ActiveHours)
			}
		} else {
			fmt.Fprintf(&b, "**基线:** %s 内 %.0f 次\n", baselineRangeOf(rule), m.Baseline)
		}
	}
	if len(missing) > len(shown) {
		fmt.Fprintf(&b, "\n…另有 %d 个未列出，完整名单见告警详情\n", len(missing)-len(shown))
	}
	return title, b.String()
}

func baselineRangeOf(rule *models.AlertRule) string {
	if rule.BaselineRange == "" {
		return "7d"
	}
	return rule.BaselineRange
}

func (e *Engine) sendHeartbeatRecovery(ctx context.Context, rule *models.AlertRule,
	sender notify.Notifier, atUsers []models.AtUser, atAll bool, ruleIDStr string, baselineSize int) {

	title := rule.RecoveryTitle
	if title == "" {
		title = rule.MessageTitle + " - 已恢复"
	}
	msg := fmt.Sprintf("**级别:** %s | 全部 %d 个组合在 %s 内都有活动\n",
		rule.Severity, baselineSize, rule.TimeRange)
	resp, err := sender.SendCard(title, msg, rule.Severity, atUsers, atAll)
	if err != nil {
		saveAlertLog(rule, msg, "", "failed", err.Error(), resp)
		return
	}
	saveAlertLog(rule, msg, "", "success", partialSendNote(resp), resp)
	log.Printf("[Heartbeat] rule %d: 已发送恢复通知", rule.ID)
}

// heartbeatInterval parses the repeat interval, accepting the "7d" form that
// time.ParseDuration does not.
//
// "once" is handled by the caller rather than here: it is not a duration but a
// different rule ("do not repeat until the set changes"), and flattening it to
// a number would make it indistinguishable from "no interval configured",
// which means the opposite — alert every cycle.
func heartbeatInterval(s string) time.Duration {
	s = strings.TrimSpace(s)
	if s == "" || s == AlertIntervalOnce {
		return 0
	}
	if strings.HasSuffix(s, "d") {
		days := 1
		fmt.Sscanf(s, "%dd", &days)
		return time.Duration(days) * 24 * time.Hour
	}
	if d, err := time.ParseDuration(s); err == nil {
		return d
	}
	return 0
}

// classifyHeartbeat 把基线里的每个组合分成三类：被过滤掉的、有活动的、异常的。
//
// 抽成不碰 Loki 的纯函数，是因为这里的顺序曾经写错过，而错法很隐蔽：先判活跃、
// 活跃的直接收下，过滤就只作用在「可能告警的」那一批，当前有活动的整批绕过白
// 名单——预览于是既显示「站点未关注 0」，又在活跃列表里列出没登记的站点。两个
// 数字自相矛盾，却都是"真的"。这种错误只有把顺序本身钉进测试才拦得住。
func classifyHeartbeat(res *HeartbeatResult, minHits, minHourly float64) {
	for k, b := range res.Baseline {
		roomID := b.Labels["room_id"]
		siteID := b.Labels["site_id"]
		entry := HeartbeatEntry{
			Key: k, Labels: b.Labels, Baseline: b.Count, SiteID: siteID, RoomID: roomID,
			MinHourly: b.MinHourly, ActiveHours: b.ActiveHours, ExpectHours: b.ExpectHours,
		}

		// ① 先划定监控范围 —— 站点白名单 + 房间在用
		if res.Dict != nil {
			if siteID != "" {
				st, ok := res.Dict.Sites[siteID]
				if !ok || !st.Watched {
					res.SkippedByDict++
					continue
				}
			}
			if roomID != "" {
				if rm, ok := res.Dict.Rooms[roomID]; ok && !rm.InService {
					res.SkippedByDict++
					continue
				}
			}
		}

		// ② 再筛掉基线还判不了的组合，两个判据都不满足才算够。
		//
		// 这批不是「不用管」，而是「还判不了」：新上线的房间、刚放量的站点都落在这里。
		// 所以它们进 Watching 被列出来，而不是只记一个计数——静默不监控和监控一样
		// 都是一个状态，区别只在有没有人知道。
		switch {
		case minHits > 0 && b.Count < minHits:
			entry.WhyWatching = fmt.Sprintf("基线总次数 %.0f < %.0f", b.Count, minHits)
		case minHourly > 0 && b.MinHourly < minHourly:
			// 最冷小时判据。b.MinHourly==0 意味着基线里存在完全空窗的整小时，
			// 这种组合用几分钟的窗口去监控一定会误报。
			if b.MinHourly == 0 && b.ExpectHours > 0 {
				entry.WhyWatching = fmt.Sprintf("基线里有 %d 个整小时没有日志（%d/%d 小时有活动）",
					b.ExpectHours-b.ActiveHours, b.ActiveHours, b.ExpectHours)
			} else {
				entry.WhyWatching = fmt.Sprintf("最冷一小时 %.0f 次 < %.0f", b.MinHourly, minHourly)
			}
		}
		if entry.WhyWatching != "" {
			res.Watching = append(res.Watching, entry)
			res.SkippedLowTraffic++
			continue
		}

		// ③ 到这里才是真正被监控的，分活跃与异常
		if _, alive := res.Current[k]; alive {
			res.Alive = append(res.Alive, entry)
			continue
		}
		res.Missing = append(res.Missing, entry)
	}
}
