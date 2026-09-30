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
	Key    string
	Labels map[string]string
	SiteID string
	RoomID string
	// Source 是这条对应关系的来源：auto=日志扫出来的，manual=人工补的。
	// 人工补的组合从来没有过日志，第一次告警时要能分辨「真出问题了」还是
	// 「这条关系本来就配错了」。
	Source string
	// Hits 是运维平台记录的扫描命中数，用来判断这个组合平时活不活跃。
	Hits int64
	// MaintainWhy 说明被维护抑制的原因，空表示没被抑制。
	MaintainWhy string
}

// executeHeartbeat is the whole mode: two queries, a set difference, one card.
// HeartbeatResult 是一次心跳判定的完整结果。
//
// 发送路径和预览共用这一个函数：预览若照着判定逻辑再写一遍，迟早会出现「预览说
// 会告警、实际不告」或者反过来的情况，而那种不一致比没有预览更糟——人会照着预览
// 去调阈值。
type HeartbeatResult struct {
	Dims         []string
	CurrentQuery string
	Current      map[string]hbDim
	TimeRange    string

	// Scope 是本轮的监控范围全集 = 关系表 ∩ 已关注站点 ∩ 在用桌台。
	//
	// 它来自运维平台，不来自 Loki。这个方向很重要：用「日志里出现过什么」来决定
	// 监控谁，就永远发现不了一张整周没有日志的在用桌台——而那恰恰是最该告警的情况。
	Scope       []HeartbeatEntry
	Missing     []HeartbeatEntry // 范围内、本窗口无活动 → 告警
	Alive       []HeartbeatEntry // 范围内、本窗口有活动
	Maintaining []HeartbeatEntry // 范围内、但维护中，本轮不判定

	// OutOfScope 是本窗口有日志、却不在监控范围里的组合。
	//
	// 这是对应关系表的体检：日志证明这个组合真实存在，范围里却没有它，说明站点没
	// 关注、桌台标成了非在用、或者关系表缺了一条。不摆出来的话，一张漏配的桌台会
	// 永远安静地不被监控，而且没有任何迹象。
	OutOfScope []HeartbeatEntry

	// 范围推导的分解，预览要把加减法原样摆出来
	PairTotal        int
	SkipNotWatched   int
	SkipNotInService int
	SkipUnknown      int // 关系指向了字典里已经没有的桌台或站点

	Dict *Dict
}

// Monitored 是本轮真正判定了的组合数 = 活跃 + 异常。
//
// 不含维护中的：它们在范围内，但这一轮不判定。把它们算进来会让人以为覆盖面比实际
// 大。范围总数看 len(Scope)。
func (r *HeartbeatResult) Monitored() int { return len(r.Alive) + len(r.Missing) }

// EvaluateHeartbeat 跑一次判定，不发送、不写任何状态。
//
// 只有一次 Loki 查询：本窗口有活动的组合。监控范围来自字典，不需要再问 Loki
// 「平时有哪些组合」——那个问题的答案里没有已经死了一周的桌台。
//
// 参数 useBaselineCache 保留是为了不动调用方签名；现在没有基线，它不起作用。
func EvaluateHeartbeat(ctx context.Context, rule *models.AlertRule, getClient LokiClientFunc, _ bool) (*HeartbeatResult, error) {
	dims, err := heartbeatDimNames(rule.DimPattern)
	if err != nil {
		return nil, err
	}
	if err := checkHeartbeatDims(dims); err != nil {
		return nil, err
	}

	current, curQuery, err := queryHeartbeatDims(ctx, rule, dims, rule.TimeRange, getClient)
	if err != nil {
		return nil, fmt.Errorf("当前窗口查询失败: %w", err)
	}

	// 字典在这里已经不是可选的了：它就是监控范围。拿不到就没有范围，这一轮什么
	// 都判不了——报错比「静默地一个都不告警」好，后者看起来和「一切正常」一样。
	if rule.DictSourceID <= 0 {
		return nil, fmt.Errorf("心跳模式必须配置字典源：监控范围来自运维平台的「站点 × 桌台」对应关系")
	}
	dict, err := GetDict(ctx, rule.DictSourceID)
	if err != nil {
		return nil, fmt.Errorf("字典不可用，无法确定监控范围: %w", err)
	}

	res := &HeartbeatResult{
		Dims: dims, CurrentQuery: curQuery, Current: current,
		TimeRange: rule.TimeRange, Dict: dict,
	}
	classifyHeartbeat(res, time.Now())

	byName := func(a, b HeartbeatEntry) bool {
		if a.SiteID != b.SiteID {
			return dict.SiteLabel(a.SiteID) < dict.SiteLabel(b.SiteID)
		}
		return dict.RoomLabel(a.RoomID) < dict.RoomLabel(b.RoomID)
	}
	sort.Slice(res.Missing, func(i, j int) bool { return byName(res.Missing[i], res.Missing[j]) })
	sort.Slice(res.Alive, func(i, j int) bool { return byName(res.Alive[i], res.Alive[j]) })
	sort.Slice(res.Maintaining, func(i, j int) bool { return byName(res.Maintaining[i], res.Maintaining[j]) })
	sort.Slice(res.OutOfScope, func(i, j int) bool { return byName(res.OutOfScope[i], res.OutOfScope[j]) })
	return res, nil
}

// checkHeartbeatDims 确认维度就是 site_id 和 room_id。
//
// 监控范围现在由「站点 × 桌台」的对应关系张成，维度名对不上就没法把范围里的组合
// 和 Loki 返回的标签对起来。不拦的话表现是范围里每个组合都「没有活动」——
// 一轮告警几十条，而根因只是正则里的组名拼错了。
func checkHeartbeatDims(dims []string) error {
	want := map[string]bool{"site_id": true, "room_id": true}
	for _, d := range dims {
		if !want[d] {
			return fmt.Errorf("心跳模式的维度只能是 site_id 和 room_id，正则里出现了 %q —— "+
				"监控范围来自运维平台的「站点 × 桌台」关系，维度名对不上就无法匹配", d)
		}
		delete(want, d)
	}
	if len(want) > 0 {
		missing := make([]string, 0, len(want))
		for k := range want {
			missing = append(missing, k)
		}
		sort.Strings(missing)
		return fmt.Errorf("维度提取正则缺少命名组: %s —— 形如 (?P<site_id>\\d+) 和 (?P<room_id>\\d+)",
			strings.Join(missing, ", "))
	}
	return nil
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
	dict := res.Dict

	log.Printf("[Heartbeat] rule %d: 关系 %d － 站点未关注 %d － 桌台非在用 %d － 已失效 %d ＝ 范围 %d"+
		"（维护中 %d 不判定）| 活跃 %d / 异常 %d | 范围外有活动 %d | query=%s",
		rule.ID, res.PairTotal, res.SkipNotWatched, res.SkipNotInService, res.SkipUnknown,
		len(res.Scope), len(res.Maintaining), len(res.Alive), len(res.Missing),
		len(res.OutOfScope), res.CurrentQuery)

	// id 不进告警消息，但必须留在日志里：排查时要靠它回到运维平台和 Loki。
	for _, m := range res.Missing {
		log.Printf("[Heartbeat] rule %d MISSING site_id=%s room_id=%s source=%s hits=%d",
			rule.ID, m.SiteID, m.RoomID, m.Source, m.Hits)
	}
	for _, m := range res.Maintaining {
		log.Printf("[Heartbeat] rule %d SUPPRESSED site_id=%s room_id=%s reason=%s",
			rule.ID, m.SiteID, m.RoomID, m.MaintainWhy)
	}
	// 范围外有活动 = 关系表漏了一条。它不会告警，所以日志是唯一的线索。
	for _, m := range res.OutOfScope {
		log.Printf("[Heartbeat] rule %d OUT_OF_SCOPE site_id=%s room_id=%s —— "+
			"日志里有活动但不在监控范围，检查运维平台的站点×桌台关系",
			rule.ID, m.SiteID, m.RoomID)
	}

	if len(res.Scope) == 0 {
		msg := "监控范围是空的：运维平台里没有「已关注站点 × 在用桌台」的对应关系"
		log.Printf("[Heartbeat] rule %d: %s", rule.ID, msg)
		database.DB.Exec("UPDATE alert_rules SET last_error=? WHERE id=?", msg, rule.ID)
		return
	}
	database.DB.Exec("UPDATE alert_rules SET last_error='' WHERE id=?", rule.ID)

	// ── 单组合节流 ──
	//
	// 以前按「整个异常集合」算一个签名做节流。范围从几十个扩到全部在用桌台之后
	// 这个做法会失效：夜里组合频繁进出集合，签名每轮都在变，等于没有节流；而且
	// 一个组合恢复会让其余所有组合重新发一遍。
	//
	// 现在每个组合各记各的「上次告过」，互不影响。
	interval := heartbeatInterval(rule.AlertInterval)
	once := rule.AlertInterval == AlertIntervalOnce
	var due []HeartbeatEntry
	nowStr := time.Now().Format(time.RFC3339)
	for _, m := range res.Missing {
		k := heartbeatComboKey(rule.ID, m.Key)
		last, _ := database.RDB.Get(ctx, k).Result()
		if last == "" {
			due = append(due, m)
			continue
		}
		if once {
			continue
		}
		if interval > 0 {
			if t, pErr := time.Parse(time.RFC3339, last); pErr == nil && time.Since(t) < interval {
				continue
			}
		}
		due = append(due, m)
	}

	// 恢复通知：上一轮告过、这一轮有活动了的组合
	var recovered []HeartbeatEntry
	for _, a := range res.Alive {
		k := heartbeatComboKey(rule.ID, a.Key)
		if v, _ := database.RDB.Get(ctx, k).Result(); v != "" {
			recovered = append(recovered, a)
			database.RDB.Del(ctx, k)
		}
	}
	// 维护中的组合也要清掉告警状态：维护结束后它如果还是没活动，应当作为一条
	// 新告警重新发出来，而不是被「上次告过」压住。
	for _, m := range res.Maintaining {
		database.RDB.Del(ctx, heartbeatComboKey(rule.ID, m.Key))
	}

	if len(recovered) > 0 && rule.RecoveryEnabled == 1 {
		e.sendHeartbeatRecovery(ctx, rule, sender, atUsers, atAll, ruleIDStr, recovered, len(res.Scope))
	}
	if len(due) == 0 {
		if len(res.Missing) > 0 {
			log.Printf("[Heartbeat] rule %d: %d 个异常组合都在告警间隔内，本轮不发",
				rule.ID, len(res.Missing))
		}
		return
	}

	title, message := e.renderHeartbeat(rule, res, due)
	resp, sErr := sender.SendCard(title, message, rule.Severity, atUsers, atAll)

	raw, _ := json.Marshal(map[string]interface{}{
		"missing":      due,
		"missing_all":  len(res.Missing),
		"scope_size":   len(res.Scope),
		"maintaining":  res.Maintaining,
		"out_of_scope": res.OutOfScope,
		"current_size": len(res.Current),
		"query":        res.CurrentQuery,
		"dict_version": dictVersionOf(dict),
		"dict_stale":   dict != nil && dict.Stale,
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
	// 发送成功后才记「告过」：发失败还记上的话，这个组合会被间隔压住，
	// 下一轮不再重试，故障就此无声。
	for _, m := range due {
		database.RDB.Set(ctx, heartbeatComboKey(rule.ID, m.Key), nowStr, 7*24*time.Hour)
	}
	log.Printf("[Heartbeat] rule %d: 已发送 %d 个异常组合（异常总数 %d，其余在间隔内）",
		rule.ID, len(due), len(res.Missing))
}

// heartbeatComboKey 是单个组合的节流键。带规则 id，避免两条规则监控同一组合时互相压制。
func heartbeatComboKey(ruleID int, comboKey string) string {
	return fmt.Sprintf("alert:hb:combo:%d:%s", ruleID, comboKey)
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

// renderHeartbeat 渲染告警卡片。
//
// 卡片里只出现名字，id 留在日志和告警详情里 —— 收告警的人要的是「BPreal 的 D059
// 没人了」，不是一串雪花 ID。
func (e *Engine) renderHeartbeat(rule *models.AlertRule, res *HeartbeatResult,
	due []HeartbeatEntry) (string, string) {

	dict := res.Dict
	title := rule.MessageTitle
	if title == "" {
		title = rule.Name
	}

	var b strings.Builder
	fmt.Fprintf(&b, "**级别:** %s | **%s 内无活动:** %d / %d 个监控组合\n",
		rule.Severity, rule.TimeRange, len(res.Missing), len(res.Scope))
	if len(due) < len(res.Missing) {
		fmt.Fprintf(&b, "**本次列出:** %d 个（其余在告警间隔内，已告过）\n", len(due))
	}
	if len(res.Maintaining) > 0 {
		fmt.Fprintf(&b, "**维护中不判定:** %d 个\n", len(res.Maintaining))
	}

	if dict != nil && dict.Stale {
		fmt.Fprintf(&b, "**⚠ 名单可能过期:** 字典最后同步于 %s（%s）\n",
			timezone.Format(dict.SyncedAt), truncateForLog(dict.StaleWhy, 80))
	}

	shown := due
	if len(shown) > heartbeatMaxListed {
		shown = shown[:heartbeatMaxListed]
	}
	for i, m := range shown {
		fmt.Fprintf(&b, "\n—— %d/%d ——\n", i+1, len(due))
		fmt.Fprintf(&b, "**站点:** %s\n", dict.SiteLabel(m.SiteID))
		fmt.Fprintf(&b, "**桌台:** %s\n", dict.RoomLabel(m.RoomID))
		// 人工补的关系从来没有过日志，第一次告警时要能分辨「真出问题了」还是
		// 「这条关系本来就配错了」。
		if m.Source == "manual" && m.Hits == 0 {
			fmt.Fprintf(&b, "**注意:** 这条对应关系是人工录入的，日志里从未出现过 —— "+
				"也可能是关系配错了\n")
		} else if m.Hits > 0 {
			fmt.Fprintf(&b, "**平时活跃度:** 扫描窗口内 %d 条\n", m.Hits)
		}
	}
	if len(due) > len(shown) {
		fmt.Fprintf(&b, "\n…另有 %d 个未列出，完整名单见告警详情\n", len(due)-len(shown))
	}
	return title, b.String()
}

func baselineRangeOf(rule *models.AlertRule) string {
	if rule.BaselineRange == "" {
		return "7d"
	}
	return rule.BaselineRange
}

// sendHeartbeatRecovery 只报本轮真正恢复的那些组合。
//
// 以前是「异常集合清空了」才发一条「全部恢复」。范围扩大之后这个时刻几乎不会出现
// （夜里总有桌台是空的），于是恢复通知等于永远不发。按组合报就没有这个问题：
// 哪张桌台回来了就说哪张。
func (e *Engine) sendHeartbeatRecovery(ctx context.Context, rule *models.AlertRule,
	sender notify.Notifier, atUsers []models.AtUser, atAll bool, ruleIDStr string,
	recovered []HeartbeatEntry, scopeSize int) {

	dict, _ := GetDict(ctx, rule.DictSourceID)
	title := rule.RecoveryTitle
	if title == "" {
		title = rule.MessageTitle + " - 已恢复"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "**级别:** %s | %d 个组合在 %s 内恢复活动（监控范围 %d）\n",
		rule.Severity, len(recovered), rule.TimeRange, scopeSize)
	shown := recovered
	if len(shown) > heartbeatMaxListed {
		shown = shown[:heartbeatMaxListed]
	}
	for _, m := range shown {
		fmt.Fprintf(&b, "\n**%s** / %s\n", dict.SiteLabel(m.SiteID), dict.RoomLabel(m.RoomID))
	}
	if len(recovered) > len(shown) {
		fmt.Fprintf(&b, "\n…另有 %d 个未列出\n", len(recovered)-len(shown))
	}
	msg := b.String()
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

// classifyHeartbeat 把监控范围里的每个组合分成三类：维护中的、有活动的、异常的。
//
// 🔴 范围来自字典的对应关系表，不来自 Loki。
//
// 之前的实现反过来：先用 Loki 基线扫出「出现过的组合」，再拿字典去做减法。那样
// 一张整周没有日志的在用桌台根本进不了基线，于是永远不会告警——而它恰恰是最该
// 告警的情况（桌台已经死了一周）。日志只能证明「出现过的组合存在」，证明不了
// 「没出现的组合不存在」。
//
// 抽成不碰 Loki 的纯函数，是因为这里的顺序和边界曾经写错过，而错法都很隐蔽。
func classifyHeartbeat(res *HeartbeatResult, now time.Time) {
	d := res.Dict
	if d == nil {
		// 没有字典就没有监控范围。这不是「监控全部」，而是「什么都判不了」——
		// 本窗口有活动的组合全部记成范围外，让调用方看到范围是空的并说明原因。
		for k, c := range res.Current {
			res.OutOfScope = append(res.OutOfScope, entryFromCurrent(k, c))
		}
		return
	}

	res.PairTotal = len(d.Pairs)
	inScope := make(map[string]bool, len(d.Pairs))

	for _, pr := range d.Pairs {
		site, siteOK := d.Sites[pr.SiteID]
		room, roomOK := d.Rooms[pr.RoomID]

		// 关系指向了字典里已经没有的桌台或站点：桌台下架、站点删掉，而关系没跟着清。
		// 单独计数而不是混进「未关注」，否则运维平台那边不知道该去清哪一类。
		if !siteOK || !roomOK {
			res.SkipUnknown++
			continue
		}
		if !site.Watched {
			res.SkipNotWatched++
			continue
		}
		if !room.InService {
			res.SkipNotInService++
			continue
		}

		labels := map[string]string{"site_id": pr.SiteID, "room_id": pr.RoomID}
		key := dimKey(labels, res.Dims)
		entry := HeartbeatEntry{
			Key: key, Labels: labels, SiteID: pr.SiteID, RoomID: pr.RoomID,
			Source: pr.Source, Hits: pr.Hits,
		}
		res.Scope = append(res.Scope, entry)
		inScope[key] = true

		// 维护抑制先于活动判定：维护中的桌台没有日志是预期的，拿它去告警只会让人
		// 对告警麻木，而那正是真故障被忽略的原因。
		if why := maintainReason(d, room, pr.SiteID, now); why != "" {
			entry.MaintainWhy = why
			res.Maintaining = append(res.Maintaining, entry)
			continue
		}

		if _, alive := res.Current[key]; alive {
			res.Alive = append(res.Alive, entry)
			continue
		}
		res.Missing = append(res.Missing, entry)
	}

	// 本窗口有日志、却不在范围里的组合 —— 关系表的体检项
	for k, c := range res.Current {
		if !inScope[k] {
			res.OutOfScope = append(res.OutOfScope, entryFromCurrent(k, c))
		}
	}
}

func entryFromCurrent(key string, c hbDim) HeartbeatEntry {
	return HeartbeatEntry{
		Key: key, Labels: c.Labels,
		SiteID: c.Labels["site_id"], RoomID: c.Labels["room_id"],
	}
}

// maintainReason 返回这个 (站点 × 桌台) 组合此刻被维护抑制的原因，空表示不抑制。
//
// 两个来源，都要看：
//  1. 中台实时标记 —— gameRoomMaintainList 给的是「这张桌台在哪些站点维护中」，
//     所以能精确到「A 站点维护、B 站点照常」，不必整张桌台一刀切。
//  2. 例行维护窗口 —— 运维平台配置的计划内保养。只有 action=suppress 才抑制；
//     annotate 的意思是「照常告警但标注出来」，把它也抑制掉就违背了配置意图。
func maintainReason(d *Dict, room DictRoom, siteID string, now time.Time) string {
	if room.Maintaining {
		if len(room.MaintainSites) == 0 {
			// 维护判定规则配成 status_equals 时拿不到站点列表，只能整台抑制
			return "中台标记维护中（未给出站点范围，整台抑制）"
		}
		for _, s := range room.MaintainSites {
			if s == siteID {
				return "中台标记维护中"
			}
		}
		// 这张桌台在别的站点维护，与本站点无关 —— 不抑制，继续往下判
	}
	if w := ActiveMaintWindow(d, room.RoomNo, room.TableNo, now); w != nil && w.Action == "suppress" {
		return "例行维护窗口「" + w.Name + "」"
	}
	return ""
}

// ScanHeartbeatPair 是扫出来的一个候选组合。
type ScanHeartbeatPair struct {
	RoomID string `json:"room_id"`
	SiteID string `json:"site_id"`
	Hits   int64  `json:"hits"`
}

// ScanHeartbeatPairs 扫出这段时间里出现过的全部「站点 × 桌台」组合。
//
// 用来给运维平台的对应关系表灌初始数据。这是 Loki 唯一还参与「范围」这件事的
// 地方，而且是一次性的：日志能证明存在，不能证明不存在，所以结果只增不删。
func ScanHeartbeatPairs(ctx context.Context, rule *models.AlertRule,
	getClient LokiClientFunc) ([]ScanHeartbeatPair, string, error) {

	dims, err := heartbeatDimNames(rule.DimPattern)
	if err != nil {
		return nil, "", err
	}
	if err := checkHeartbeatDims(dims); err != nil {
		return nil, "", err
	}
	found, q, err := queryHeartbeatDims(ctx, rule, dims, rule.TimeRange, getClient)
	if err != nil {
		return nil, q, err
	}

	out := make([]ScanHeartbeatPair, 0, len(found))
	for _, d := range found {
		site, room := d.Labels["site_id"], d.Labels["room_id"]
		if site == "" || room == "" {
			// 两个维度缺一个就不是一个组合。灌进关系表会变成一条永远匹配不上的
			// 关系，然后每轮都告警。
			continue
		}
		out = append(out, ScanHeartbeatPair{RoomID: room, SiteID: site, Hits: int64(d.Count)})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].SiteID != out[j].SiteID {
			return out[i].SiteID < out[j].SiteID
		}
		return out[i].RoomID < out[j].RoomID
	})
	return out, q, nil
}
