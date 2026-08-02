// GKE 升级提醒：把「会被升级」提前变成日程上的待办，让人有时间自己安排。
//
// ⚠️ 状态一律从 loadClusterUpgradeStates 取，**不许在这里另写 SQL**。
// 之前这里只 JOIN gke_cluster_upgrade、没碰 gke_node_pools，导致 g32 三天后 35 个节点进 EOS
// 而飞书一条不发（看板却是红的）。同一语义分叉两次已经出过事。
//
// 按三条互相独立的时间线组织，因为它们「能不能拦」完全不同：
//
//	① 控制面自动升级 —— 关不掉，只能维护排除延迟 → 提前安排窗口
//	② 节点池自动升级 —— 可以关；关了平时就不升 → 但别忘了③
//	③ 强制升级(EOS)  —— 完全不可阻止，关了 autoUpgrade 也照升 → 必须自己先升完
//
// 提醒节奏：T-30/14/7/3/1，越近越密（CMDB-023）。T-3 起 @ 人。
package handlers

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"opsplatform-cmdb-backend/logx"
	"opsplatform-cmdb-backend/notify"
)

// 提醒档位。越近越密：实测一轮完整升级要 2.5 小时（等满观察期）或约 1 小时（跳过），
// 原来 T-30 / T-7 两档，提前 7 天才第二次提醒，留给排期协调的时间不够（CMDB-023）。
var (
	remindGates      = []int{30, 14, 7, 3, 1}
	forcedGates      = []int{30, 14, 7, 3, 1}
	atMentionDays    = 3 // 剩余 ≤3 天时 @ 人：这时候还没排上窗口就真的来不及了
	nodeFollowupDays = 7 // 官方「控制面升完后 typically a few days」，取一周作为节点池的保守窗口
)

type gkeRemindItem struct {
	Cluster string
	Env     string
	Urgent  bool
	Rules   []string // 命中的规则，用于日志与摘要
	Lines   []string // 详细说明，进任务记录的 findings（页面上看得到全文）

	// —— 以下为飞书排版用的结构化字段（CMDB-023）——
	// 旧版把所有内容拼成 Lines 直接往飞书倒，单条消息 1800 字、解释文案逐集群重复。
	// 改成结构化后，飞书那边才能按倒计时排序、把共同解释抽到消息末尾只说一次。
	EOSDays  *int   // 强制升级倒计时（nil=未命中该规则）
	EOSDate  string // 强制升级截止日
	PoolCnt  int    // 受影响节点池数
	NodeCnt  int    // 受影响节点数
	Action   string // 一句话动作建议：看完就知道要干什么
	SkewNow  int    // 当前已落后几个小版本（CMDB-024 的事实值）
	SkewText string // 偏斜详情，只在 🟡 区块或行尾标记里用
}

// gkeUpgradeRemindCore 每天跑一次。
func gkeUpgradeRemindCore(ctx context.Context, db *sql.DB) (string, []TaskFailure, bool) {
	states, err := loadClusterUpgradeStates(db)
	if err != nil {
		return "查询失败：" + err.Error(), []TaskFailure{{Target: "db", Reason: err.Error()}}, false
	}
	today := localMidnight()

	items := []gkeRemindItem{}
	scanned := 0
	for _, s := range states {
		if !s.Synced {
			continue
		}
		scanned++
		if it := buildRemindItem(&s, today); len(it.Lines) > 0 {
			items = append(items, it)
		}
	}

	if len(items) == 0 {
		// P2-5：零提醒也必须留痕。静默早退时，「没有需要提醒的集群」和「压根没扫到数据」
		// 在日志里长得一样，会让人误以为确实没事。
		logx.J("gke_remind", "no_items", map[string]any{
			"clusters_total": len(states), "clusters_synced": scanned,
			"gates": fmt.Sprintf("常规 T-%v / 强制 T-%v", remindGates, forcedGates),
			"note":  "已扫描但无集群命中任何规则",
		})
		// 全绿也发一条（每天一次）。用户明确要求保留：静默久了分不清
		// 是真没问题、还是这个任务已经死了（CMDB-023 四）。
		sent := sendAllGreen(db, scanned)
		return fmt.Sprintf("扫描 %d 个集群（其中 %d 个有采集数据），无一命中提醒规则，飞书投递：%s",
			len(states), scanned, sent), nil, true
	}

	sent := sendUpgradeRemind(db, items)
	rules := map[string]int{}
	findings := []TaskFinding{}
	for _, it := range items {
		for _, r := range it.Rules {
			rules[r]++
		}
		// 命中的集群逐个留痕：只说"3 个命中"看不出该去处理哪个集群
		lv := "warning"
		if it.Urgent {
			lv = "critical"
		}
		f := TaskFinding{
			Level: lv, Target: it.Cluster, Value: ruleSummary(countRules(it.Rules)),
			Detail: strings.Join(it.Lines, "；"),
		}
		AddFinding(ctx, f)
		findings = append(findings, f)
	}
	summary := fmt.Sprintf("扫描 %d 个集群，%d 个命中提醒规则（%s）【%s】，飞书投递：%s",
		len(states), len(items), ruleSummary(rules), SummarizeFindings(findings, 3), sent)
	logx.J("gke_remind", "done", map[string]any{
		"clusters": len(states), "items": len(items), "rules": rules, "delivery": sent,
	})
	return summary, nil, true
}

// countRules 把单个集群命中的规则列表转成计数 map，复用 ruleSummary 的中文表述。
func countRules(rs []string) map[string]int {
	m := map[string]int{}
	for _, r := range rs {
		m[r]++
	}
	return m
}

// buildRemindItem 按三条时间线判定一个集群。
func buildRemindItem(s *clusterUpgradeState, today time.Time) gkeRemindItem {
	it := gkeRemindItem{Cluster: s.DisplayName, Env: s.Env}

	// ③ 强制升级（EOS）—— 最要命，放最前。用 EffectiveEOS，控制面与节点池取最早。
	if d := s.EffectiveEOSDays; d != nil && hitGate(*d, forcedGates) {
		it.Urgent = *d <= 14
		it.Rules = append(it.Rules, "forced_eos")
		it.EOSDays, it.EOSDate = d, s.EffectiveEOS
		it.PoolCnt, it.NodeCnt = strandedCount(s)
		it.Action = upgradeAction(s)
		via := ""
		if s.EffectiveEOSSource != "控制面" {
			via = fmt.Sprintf("（最早到期的是%s，控制面本身是 %s）", s.EffectiveEOSSource, s.ControlPlaneEOS)
		}
		it.Lines = append(it.Lines, fmt.Sprintf(
			"🔴 强制升级：当前版本%s %s 结束，还有 %d 天%s", basisOf(s), s.EffectiveEOS, *d, via))
		it.Lines = append(it.Lines,
			"   支持结束时 GKE 会强制升级，关闭 autoUpgrade 或设维护排除都拦不住，只能赶在此前自己升完")
		if n := strandedPools(s); n != "" {
			it.Lines = append(it.Lines, "   受影响："+n)
		}
	}

	// ① 升级时间线。
	// ⚠️ 主语必须跟着日期的来源走：predicted_upgrade_at 的第一优先级是**节点池**的
	// autoUpgradeStartTime，此时说「控制面 1.34.8 → 1.35，预计 N 天后」是张冠李戴——
	// 到期的其实是某个节点池。按 PredictedSource 分主语。
	if !s.Blocked && s.DaysMin != nil && hitGate(*s.DaysMin, remindGates) {
		if *s.DaysMin <= 7 {
			it.Urgent = true
		}
		if s.PredictedSource == "autoUpgradeStartTime" {
			it.Rules = append(it.Rules, "nodepool_imminent")
			owner := earliestStartTimePool(s)
			it.Lines = append(it.Lines, fmt.Sprintf(
				"节点池 %s：即将升级，预计 %s", owner.Name, whenText(s)))
			it.Lines = append(it.Lines,
				"   该时刻由 GKE 直接给出（autoUpgradeStartTime，仅在升级临近时才有值），是最后的拦截机会")
			it.Lines = append(it.Lines, controlPlaneContextLine(s))
		} else {
			it.Rules = append(it.Rules, "control_plane")
			it.Lines = append(it.Lines, fmt.Sprintf(
				"控制面：%s → %s，预计 %s（通道 %s）",
				s.MasterVersion, targetOf(s), whenText(s), channelOr(s.ReleaseChannel)))
			it.Lines = append(it.Lines, "   控制面自动升级无法关闭，只能用维护排除延迟（最长 180 天）")
			if s.PredictedSource == "inferred_next_minor" {
				it.Lines = append(it.Lines, "   ⚠ 目标版本是推断值：GKE 尚未排期，按当前小版本 +1 估算")
			}
			it.Lines = append(it.Lines, nodePoolLine(s))
		}
	}

	// ② 节点池独立触发：任何节点池自己拿到了 autoUpgradeStartTime 就单独报，
	// 不依附控制面那条判定——否则控制面不在窗口内时，临期的节点池会被整条跳过。
	for i := range s.Pools {
		p := &s.Pools[i]
		if p.StartTime == "" || s.PredictedSource == "autoUpgradeStartTime" {
			continue // 已由 ① 覆盖，避免同一节点池报两次
		}
		if d := daysUntil(dateOf(p.StartTime), today); d != nil && hitGate(*d, remindGates) {
			it.Rules = append(it.Rules, "nodepool_imminent")
			if *d <= 7 {
				it.Urgent = true
			}
			it.Lines = append(it.Lines, fmt.Sprintf(
				"节点池 %s：GKE 已给出升级时刻 %s（还有 %d 天）", p.Name, p.StartTime, *d))
		}
	}

	// 维护排除到期：排除期一过就恢复自动升级，等于白挡
	if s.Blocked {
		if end, d := exclusionEnd(s.MaintenancePolicyJSON, today); end != "" && d != nil && hitGate(*d, remindGates) {
			it.Rules = append(it.Rules, "exclusion_expiry")
			it.Lines = append(it.Lines, fmt.Sprintf(
				"维护排除 %s 到期（还有 %d 天），之后恢复自动升级，需在此前完成计划内升级", end, *d))
		}
	}

	// 偏斜临界：会真出兼容故障，不受 30 天窗口限制，命中就报。
	// ⚪ 落后 1 个小版本属正常范围，不进告警（只在看板显示）——
	// g32 生产 5 个池里 4 个是这种，每次都跟着刷但不需要人动手（CMDB-023 分档）
	if s.SkewCritical {
		it.Rules = append(it.Rules, "skew_critical")
		it.Urgent = true
		it.SkewNow = s.SkewCurrent
		// 事实句优先；控制面还没升时只有预测句（CMDB-024）
		if s.SkewNote != "" {
			it.SkewText = s.SkewNote
		} else {
			it.SkewText = s.SkewForecast
		}
		it.Lines = append(it.Lines, "🔴 版本偏斜："+it.SkewText)
		if s.SkewForecast != "" && s.SkewNote != "" {
			it.Lines = append(it.Lines, "   "+s.SkewForecast)
		}
	}
	return it
}

// strandedCount 受强制升级影响的节点池数与节点数。
// 旧版把池名全列出来（5 个占两整行），而真正有用的是规模和「最早到期的是哪个」。
func strandedCount(s *clusterUpgradeState) (pools, nodes int) {
	for _, p := range s.Pools {
		if p.EOSStandardAt != "" && p.EOSStandardAt == s.EffectiveEOS {
			pools++
			nodes += p.NodeCount
		}
	}
	if pools == 0 { // 到期的是控制面自己，那影响面就是全部节点池
		for _, p := range s.Pools {
			pools++
			nodes += p.NodeCount
		}
	}
	return
}

// upgradeAction 一句话回答「我现在要干什么」——CMDB-023 的第四条设计原则：
// 每条告警都必须能回答这个问题，否则看完还是不知道先动哪个。
func upgradeAction(s *clusterUpgradeState) string {
	autoOff := 0
	for _, p := range s.Pools {
		if !p.AutoUpgrade {
			autoOff++
		}
	}
	// 控制面已经追上目标版本 → 只剩节点池要升
	if s.MinorTarget != "" && minorOf(s.MasterVersion) == minorOf(s.MinorTarget) {
		return fmt.Sprintf("控制面已 %s → 只升节点池", minorOf(s.MasterVersion))
	}
	if s.EffectiveEOSSource != "控制面" && s.EffectiveEOSSource != "" {
		if autoOff > 0 {
			return fmt.Sprintf("%s 到期 → 自动升级关着，须人工升", s.EffectiveEOSSource)
		}
		return s.EffectiveEOSSource + " 到期 → 自动升级开着，确认窗口即可"
	}
	if autoOff > 0 {
		return fmt.Sprintf("控制面 %s → 升完节点不会跟随，%d 个池须人工升", minorOf(s.MasterVersion), autoOff)
	}
	return fmt.Sprintf("控制面 %s → 节点池会自动跟随", minorOf(s.MasterVersion))
}

// earliestStartTimePool 找出给出最早 autoUpgradeStartTime 的节点池——
// predictUpgradeDate 取的就是这个值，所以主语归它。
func earliestStartTimePool(s *clusterUpgradeState) poolState {
	var best poolState
	for _, p := range s.Pools {
		if p.StartTime == "" {
			continue
		}
		if best.StartTime == "" || p.StartTime < best.StartTime {
			best = p
		}
	}
	if best.Name == "" {
		best.Name = "（未能定位到具体节点池）"
	}
	return best
}

// controlPlaneContextLine 节点池临期时，把控制面的排期作为背景补一句，
// 避免只看到节点池而忽略控制面也会动。
func controlPlaneContextLine(s *clusterUpgradeState) string {
	if s.ControlPlaneEOS == "" {
		return "   控制面：" + s.MasterVersion + "（排期未知）"
	}
	return fmt.Sprintf("   控制面：%s，支持截止 %s（控制面自动升级无法关闭）",
		s.MasterVersion, s.ControlPlaneEOS)
}

// dateOf 从 "2006-01-02 15:04:05" 取日期部分。
func dateOf(dt string) string {
	if len(dt) >= 10 {
		return dt[:10]
	}
	return dt
}

// basisOf 期限基准的措辞。EXTENDED 通道的硬期限是扩展支持，说成「标准支持」会误导。
func basisOf(s *clusterUpgradeState) string {
	if s.EOSBasis != "" {
		return s.EOSBasis
	}
	return "标准支持"
}

func hitGate(days int, gates []int) bool {
	if days < 0 {
		return true // 已过期的更要报
	}
	for _, g := range gates {
		if days <= g {
			return true
		}
	}
	return false
}

func targetOf(s *clusterUpgradeState) string {
	if s.MinorTarget != "" {
		return s.MinorTarget
	}
	if s.InferredTarget != "" {
		return s.InferredTarget + "（推断）"
	}
	return "未知版本"
}

// whenText 预计时间的措辞。非 day 粒度只能说「最早」，官网只承诺月/季度范围。
func whenText(s *clusterUpgradeState) string {
	if s.DaysMin == nil {
		return "时间未知"
	}
	if s.PredictedPrecision == "day" {
		return fmt.Sprintf("%d 天后（%s 起）", *s.DaysMin, s.PredictedAt)
	}
	return fmt.Sprintf("最早 %d 天后（%s，官网只给到%s粒度）",
		*s.DaysMin, s.WindowText, precisionCN(s.PredictedPrecision))
}

// nodePoolLine 节点池那一行。它和控制面是两个独立事件，人要分别安排时间。
func nodePoolLine(s *clusterUpgradeState) string {
	total, autoOn, autoOff := 0, 0, 0
	for _, p := range s.Pools {
		total += p.NodeCount
		if p.AutoUpgrade {
			autoOn++
		} else {
			autoOff++
		}
	}
	if len(s.Pools) == 0 {
		return "   节点池：未采集到节点池信息"
	}
	switch {
	case autoOff == 0:
		return fmt.Sprintf("   节点池：%d 个池 / %d 节点，自动升级全开 → 控制面升完后约 %d 天内陆续跟随升级",
			len(s.Pools), total, nodeFollowupDays)
	case autoOn == 0:
		return fmt.Sprintf("   节点池：%d 个池 / %d 节点，自动升级全关 → 控制面升级后节点不会跟随，需人工安排",
			len(s.Pools), total)
	default:
		return fmt.Sprintf("   节点池：%d 个池 / %d 节点（%d 个开自动升级、%d 个关）→ 关闭的那些需人工安排",
			len(s.Pools), total, autoOn, autoOff)
	}
}

func strandedPools(s *clusterUpgradeState) string {
	names, nodes := []string{}, 0
	for _, p := range s.Pools {
		if p.EOSStandardAt != "" && p.EOSStandardAt == s.EffectiveEOS {
			names = append(names, p.Name)
			nodes += p.NodeCount
		}
	}
	if len(names) == 0 {
		return ""
	}
	return fmt.Sprintf("%d 个节点池 / %d 个节点：%s", len(names), nodes, strings.Join(names, "、"))
}

func ruleSummary(rules map[string]int) string {
	if len(rules) == 0 {
		return "无"
	}
	cn := map[string]string{
		"forced_eos": "强制升级", "control_plane": "控制面升级",
		"exclusion_expiry": "排除期到期", "skew_critical": "偏斜临界",
	}
	parts := []string{}
	for k, v := range rules {
		name := cn[k]
		if name == "" {
			name = k
		}
		parts = append(parts, fmt.Sprintf("%s×%d", name, v))
	}
	return strings.Join(parts, " ")
}

func channelOr(c string) string {
	if c == "" || c == "UNSPECIFIED" {
		return "未入通道（按 Stable 排期）"
	}
	return c
}

// exclusionEnd 从 maintenancePolicy JSON 里找最晚的维护排除结束时间。
func exclusionEnd(maintJSON string, today time.Time) (string, *int) {
	if maintJSON == "" {
		return "", nil
	}
	var mp struct {
		Window struct {
			MaintenanceExclusions map[string]struct {
				EndTime string `json:"endTime"`
			} `json:"maintenanceExclusions"`
		} `json:"window"`
	}
	if json.Unmarshal([]byte(maintJSON), &mp) != nil {
		return "", nil
	}
	latest := ""
	for _, ex := range mp.Window.MaintenanceExclusions {
		if len(ex.EndTime) >= 10 && ex.EndTime[:10] > latest {
			latest = ex.EndTime[:10]
		}
	}
	if latest == "" {
		return "", nil
	}
	return latest, daysUntil(latest, today)
}

func sendUpgradeRemind(db *sql.DB, items []gkeRemindItem) string {
	webhook, group := larkWebhookForTask(db, "gke_upgrade_remind")
	if webhook == "" {
		logx.J("gke_remind", "no_group", map[string]any{
			"items": len(items), "note": "gke_upgrade_remind 未配飞书群，提醒只进日志和任务记录",
		})
		return "未配置群（提醒未送达）"
	}
	text := renderUpgradeRemind(items)
	// 关掉这类告警时任务照跑（数据仍在采集与看板里），只是不投递飞书
	if !alertEnabled(db, "notify_gke_upgrade") {
		return "已跳过投递：GKE 升级预警在「通知」页被关闭"
	}
	atSeg, atNames := "", ""
	// @人只在真的来不及时才用：T-3 起 @，平时只进群。半夜被 @ 醒却发现还有 20 天，
	// 下次就没人认真看了（CMDB-023 五）
	if shouldAtMention(items) {
		atSeg, atNames = atMentionsForTask2(db, "gke_upgrade_remind")
		text += atSeg
	}
	if err := notify.SendFeishu(webhook, text); err != nil {
		logx.J("gke_remind", "send_failed", map[string]any{"group": group, "err": err.Error()})
		return "投递失败：" + err.Error()
	}
	if atNames == "" {
		return "已发送到 " + group + "（未配通知人，无 @）"
	}
	return "已发送到 " + group + "，@" + atNames
}

// ---- 飞书排版（CMDB-023）----
//
// 旧版把每个集群的所有说明逐条铺开，单条消息约 1800 字、三个集群刷满一屏，
// 且「支持结束时 GKE 会强制升级…」这类**不随集群变化的解释**在每个集群下各出现一遍。
// 用户原话：「现在一堆字，看的比较麻烦，要简洁点，一眼就知道问题」。
//
// 四条原则：
//  1. 标题给结论不给分类 —— 倒计时进标题
//  2. 共同解释只说一次 —— 统一放分隔线下方，不随集群数量重复
//  3. 用对齐代替句子 —— 「受影响：5 个节点池 / 35 个节点：a、b、c…」→「35 节点 / 5 池」
//  4. 每条都要能回答「我现在干什么」—— 每行跟一句动作建议
//
// 🔴（强制升级）与 🟡（偏斜已达硬限制）分区块不混排：两者要人做的事不同，
// 🔴 是「排窗口」，🟡 是「这个池别再拖了」。同一集群两条都中时只在 🔴 出现一次，
// 行尾挂 🟡 标记，避免同一个集群在消息里露两遍。
func renderUpgradeRemind(items []gkeRemindItem) string {
	forced, skewOnly := []gkeRemindItem{}, []gkeRemindItem{}
	for _, it := range items {
		switch {
		case it.EOSDays != nil:
			forced = append(forced, it) // 两条都中的也归这里，偏斜降为行尾标记
		case it.SkewNow >= 2 || it.SkewText != "":
			skewOnly = append(skewOnly, it)
		}
	}
	// 倒计时升序：最急的排最前，一眼看到的就是最该动手的那个
	sort.SliceStable(forced, func(i, j int) bool { return *forced[i].EOSDays < *forced[j].EOSDays })

	var b strings.Builder
	if len(forced) > 0 {
		fmt.Fprintf(&b, "🔴 GKE 强制升级 · 最早 %d 天后\n\n", *forced[0].EOSDays)
		for _, it := range forced {
			skewTag := ""
			if it.SkewNow >= 2 {
				skewTag = fmt.Sprintf(" · 🟡偏斜%d", it.SkewNow)
			}
			fmt.Fprintf(&b, "⏰ %s · %s · %s（%s）· %d 节点/%d 池%s\n",
				daysText(*it.EOSDays), shortDate(it.EOSDate), it.Cluster, it.Env,
				it.NodeCnt, it.PoolCnt, skewTag)
			if it.Action != "" {
				fmt.Fprintf(&b, "     %s\n", it.Action)
			}
		}
	}
	if len(skewOnly) > 0 {
		if b.Len() > 0 {
			b.WriteString("\n")
		}
		b.WriteString("🟡 版本偏斜已顶到硬限制（不影响期限，但该池不能再等）\n\n")
		for _, it := range skewOnly {
			fmt.Fprintf(&b, "· %s（%s）%s\n", it.Cluster, it.Env, it.SkewText)
		}
	}

	// 共同解释：只说一次，不随集群数量重复
	b.WriteString("\n─────────────────────\n")
	if len(forced) > 0 {
		b.WriteString("到期后 GKE 强制升级，关 autoUpgrade / 设维护排除都拦不住\n")
	}
	if len(skewOnly) > 0 || anySkew(forced) {
		b.WriteString("偏斜达 3 个时该池将无法调度\n")
	}
	// 实测耗时来自 2026-07-31 UAT 那次完整升级，给的是排窗口要预留多久
	b.WriteString("实测：控制面 6 分钟 ｜ 每池 18~24 分钟（跳过观察期，等满 +40 分钟）\n")
	b.WriteString("📋 生成预案 → 「K8s → 版本与升级」")
	return b.String()
}

// renderAllGreen 全绿播报。用户明确要求保留：静默久了分不清是真没问题还是任务死了。
func renderAllGreen(total int) string {
	return fmt.Sprintf("✅ GKE 版本巡检 · %d 个集群，30 天内无强制升级、无偏斜", total)
}

// shouldAtMention 只在 T-3 以内才 @ 人。
func shouldAtMention(items []gkeRemindItem) bool {
	for _, it := range items {
		if it.EOSDays != nil && *it.EOSDays <= atMentionDays {
			return true
		}
	}
	return false
}

func anySkew(items []gkeRemindItem) bool {
	for _, it := range items {
		if it.SkewNow >= 2 {
			return true
		}
	}
	return false
}

// daysText 倒计时。已过期用负数会看不懂，直接说「已过期 N 天」。
func daysText(d int) string {
	if d < 0 {
		return fmt.Sprintf("已过期 %d 天", -d)
	}
	if d == 0 {
		return "今天到期"
	}
	return fmt.Sprintf("%d 天", d)
}

// shortDate 2026-08-03 → 08-03，日期只用来区分先后，年份是噪声。
func shortDate(d string) string {
	if len(d) == 10 {
		return d[5:]
	}
	return d
}

// sendAllGreen 全绿播报：一行，够确认「任务还活着」即可。
func sendAllGreen(db *sql.DB, scanned int) string {
	webhook, group := larkWebhookForTask(db, "gke_upgrade_remind")
	if webhook == "" {
		return "未配置群"
	}
	if !alertEnabled(db, "notify_gke_upgrade") {
		return "已跳过（通知页关闭）"
	}
	// 全绿不 @ 人：没事也 @ 是消耗信任最快的方式
	if err := notify.SendFeishu(webhook, renderAllGreen(scanned)); err != nil {
		logx.J("gke_remind", "green_send_failed", map[string]any{"group": group, "err": err.Error()})
		return "投递失败：" + err.Error()
	}
	return "已发送到 " + group
}
