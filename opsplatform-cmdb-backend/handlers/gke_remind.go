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
// 提醒节奏：①② 用 T-30/T-7；③ 因为完全拦不住，额外加 T-14 一档。
package handlers

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"opsplatform-cmdb-backend/logx"
	"opsplatform-cmdb-backend/notify"
)

// 提醒档位。强制升级多一档 T-14：它拦不住，只能靠更早知道。
var (
	remindGates      = []int{30, 7}
	forcedGates      = []int{30, 14, 7}
	nodeFollowupDays = 7 // 官方「控制面升完后 typically a few days」，取一周作为节点池的保守窗口
)

type gkeRemindItem struct {
	Cluster string
	Env     string
	Urgent  bool
	Rules   []string // 命中的规则，用于日志与摘要
	Lines   []string
}

// gkeUpgradeRemindCore 每天跑一次。
func gkeUpgradeRemindCore(db *sql.DB) (string, []TaskFailure, bool) {
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
		return fmt.Sprintf("扫描 %d 个集群（其中 %d 个有采集数据），无一命中提醒规则", len(states), scanned), nil, true
	}

	sent := sendUpgradeRemind(db, items)
	rules := map[string]int{}
	for _, it := range items {
		for _, r := range it.Rules {
			rules[r]++
		}
	}
	summary := fmt.Sprintf("扫描 %d 个集群，%d 个命中提醒规则（%s），飞书投递：%s",
		len(states), len(items), ruleSummary(rules), sent)
	logx.J("gke_remind", "done", map[string]any{
		"clusters": len(states), "items": len(items), "rules": rules, "delivery": sent,
	})
	return summary, nil, true
}

// buildRemindItem 按三条时间线判定一个集群。
func buildRemindItem(s *clusterUpgradeState, today time.Time) gkeRemindItem {
	it := gkeRemindItem{Cluster: s.DisplayName, Env: s.Env}

	// ③ 强制升级（EOS）—— 最要命，放最前。用 EffectiveEOS，控制面与节点池取最早。
	if d := s.EffectiveEOSDays; d != nil && hitGate(*d, forcedGates) {
		it.Urgent = *d <= 14
		it.Rules = append(it.Rules, "forced_eos")
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

	// 偏斜临界：会真出兼容故障，不受 30 天窗口限制，命中就报
	if s.SkewCritical {
		it.Rules = append(it.Rules, "skew_critical")
		it.Urgent = true
		it.Lines = append(it.Lines, "🔴 版本偏斜临界："+s.SkewNote)
	}
	return it
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
	urgent := 0
	for _, it := range items {
		if it.Urgent {
			urgent++
		}
	}
	var b strings.Builder
	fmt.Fprintf(&b, "⚠️ GKE 升级预警（%d 个集群", len(items))
	if urgent > 0 {
		fmt.Fprintf(&b, "，其中 %d 个紧急", urgent)
	}
	b.WriteString("）\n")
	for _, it := range items {
		icon := "🟡"
		if it.Urgent {
			icon = "🔴"
		}
		fmt.Fprintf(&b, "\n%s %s（%s）\n", icon, it.Cluster, it.Env)
		for _, l := range it.Lines {
			fmt.Fprintf(&b, "   %s\n", l)
		}
	}
	b.WriteString("\n控制面与节点池是两个独立事件，需分别安排时间。" +
		"强制升级（支持结束）无法阻止，务必在该日期前自行升完。")
	if err := notify.SendFeishu(webhook, b.String()); err != nil {
		logx.J("gke_remind", "send_failed", map[string]any{"group": group, "err": err.Error()})
		return "投递失败：" + err.Error()
	}
	return "已发送到 " + group
}
