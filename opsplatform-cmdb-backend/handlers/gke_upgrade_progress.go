// 升级过程看板与实测节奏提取（只读）。
//
// 两个用途：
//  1. 升级进行中——看到每个池升到哪了、这一批起了几台、卡没卡住
//  2. 升级完成后——自动算出「单节点耗时中位数 / 最慢一台 / 实测并行度」，
//     这三个数是把 UAT 实测外推到生产窗口的全部依据
//
// 数据来自 k8s_node_version_events（每轮采集比对节点集合得到）。
// ⚠️ 时间粒度 = 采集间隔（120s），任何据此算出的耗时都有 ±2 分钟误差，
// 输出里始终带着这句话——否则「单节点 8 分钟」会被当成精确值拿去排生产窗口。
package handlers

import (
	"database/sql"
	"fmt"
	"math"
	"net/http"
	"sort"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"opsplatform-cmdb-backend/logx"
)

// 一次升级里，相邻两个节点事件之间的最大间隔。超过它就认为是两次不同的升级。
// 取 60 分钟：BLUE_GREEN 的整池观察期可能长达半小时，取小了会把一次升级切成好几段。
const upgradeBurstGapMinutes = 60

// 同一批次内 added 事件的最大间隔。超过它算下一批。
// 取 5 分钟：批次之间至少隔着一个 soak 期，而批内节点是并行创建的，差不了几分钟。
const batchGapMinutes = 5

type nodeEventOut struct {
	// scope=node 精度 ±2 分钟（k8s 采集 120s 一轮）；
	// scope=control_plane 精度 ±6 小时（gke_upgrade_sync 6h 一轮），只说明「升过」，不能算耗时
	Scope       string `json:"scope"`
	Node        string `json:"node"`
	Pool        string `json:"pool"`
	Event       string `json:"event"`
	FromVersion string `json:"from_version"`
	ToVersion   string `json:"to_version"`
	DetectedAt  string `json:"detected_at"`
	Precision   string `json:"precision"`
}

// measuredPace 从事件流里还原出的一次节点池升级的真实节奏。
type measuredPace struct {
	Pool         string
	At           string // 那次升级的开始时刻
	Nodes        int    // 换掉了几台
	Batches      int    // 分了几批
	TotalMinutes int
	BatchSizes   []int
	PerBatchMins []int
}

type poolPaceOut struct {
	Pool         string `json:"pool"`
	At           string `json:"at"`
	Nodes        int    `json:"nodes"`
	Batches      int    `json:"batches"`
	TotalMinutes int    `json:"total_minutes"`
	BatchSize    int    `json:"batch_size"`      // 实测并行度（每批几台）
	MedianBatch  int    `json:"median_batch_minutes"`
	SlowestBatch int    `json:"slowest_batch_minutes"`
	Note         string `json:"note"`
}

type progressOut struct {
	ClusterID int    `json:"cluster_id"`
	Cluster   string `json:"cluster"`
	Since     string `json:"since"`

	Events     []nodeEventOut `json:"events"`
	Pools      []poolPaceOut  `json:"pools"`
	Extrapolate string        `json:"extrapolate"` // 怎么拿这些数外推到别的集群

	CollectionHealthy bool   `json:"collection_healthy"` // 采集此刻是否正常
	CollectionNote    string `json:"collection_note"`

	Precision string   `json:"precision"`
	Warnings  []string `json:"warnings"`
}

// Progress 升级过程看板。hours 默认 24，升级当晚看这个就够；事后复盘可以拉长。
func (h *GKEUpgradePlanHandler) Progress(c *gin.Context) {
	cid, _ := strconv.Atoi(c.Query("cluster_id"))
	if cid == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "cluster_id 必填"})
		return
	}
	hours, _ := strconv.Atoi(c.Query("hours"))
	if hours <= 0 {
		hours = 24
	}
	since := time.Now().Add(-time.Duration(hours) * time.Hour)

	events, err := h.nodeEvents(cid, since)
	if err != nil {
		// 同样的原则：查不到 ≠ 没发生。失败必须报错，不能返回空事件列表
		c.JSON(http.StatusInternalServerError, gin.H{"error": "查询节点变更事件失败: " + err.Error()})
		return
	}

	out := progressOut{
		ClusterID: cid, Since: since.Format("2006-01-02 15:04:05"),
		Events:    make([]nodeEventOut, 0, len(events)),
		Precision: "时间粒度 = 采集间隔（120 秒）。所有耗时都有 ±2 分钟误差，排生产窗口时按上限算",
	}
	_ = h.DB.QueryRow(`SELECT name FROM k8s_clusters WHERE id=?`, cid).Scan(&out.Cluster)

	cpEvents := 0
	for _, e := range events {
		o := nodeEventOut{
			Scope: e.Scope, Node: e.Node, Pool: e.Pool, Event: e.Event,
			FromVersion: e.From, ToVersion: e.To,
			DetectedAt: e.At.Format("2006-01-02 15:04:05"),
			Precision:  "±2 分钟（k8s 采集 120 秒一轮）",
		}
		if e.Scope == "control_plane" {
			cpEvents++
			// 精度差一个数量级，必须逐条标出来——同一张列表里混着两种精度，
			// 不标的话很容易拿控制面那行去算耗时，结果偏差以小时计
			o.Precision = "±6 小时（GKE 采集 6 小时一轮）；控制面耗时请查升级历史的 started_at/ended_at"
		}
		out.Events = append(out.Events, o)
	}
	if cpEvents > 0 {
		out.Warnings = append(out.Warnings, fmt.Sprintf(
			"列表含 %d 条控制面版本变更。它们回答的是「升过、从哪版到哪版」，"+
				"不能用来算耗时——采集间隔 6 小时，误差就是 6 小时。控制面耗时取升级历史里 GCP 给的真实时刻", cpEvents))
	}

	for _, p := range pacesFromEvents(events) {
		out.Pools = append(out.Pools, toPaceOut(p))
	}

	out.CollectionHealthy, out.CollectionNote = h.collectionHealth(cid)
	out.Extrapolate = extrapolateNote(out.Pools)
	// 同 normalizePlanSlices 的理由：空清单要是 []，不能是 null
	if out.Pools == nil {
		out.Pools = []poolPaceOut{}
	}
	if out.Warnings == nil {
		out.Warnings = []string{}
	}

	if len(events) == 0 {
		out.Warnings = append(out.Warnings, fmt.Sprintf(
			"最近 %d 小时没有任何节点增删——要么没在升级，要么升级还没开始动节点。"+
				"注意首次采集会建立基线而不产生事件", hours))
	}
	if !out.CollectionHealthy {
		out.Warnings = append(out.Warnings,
			"采集当前不正常：升级期间控制面重启会让 apiserver 短暂不可达，这是预期内的；"+
				"但采集中断期间发生的节点变更会被合并到恢复后的第一轮，耗时会偏大")
	}

	logx.J("gke_plan", "progress", map[string]any{
		"cluster_id": cid, "hours": hours, "events": len(events), "pools": len(out.Pools),
	})
	c.JSON(http.StatusOK, out)
}

type rawEvent struct {
	Scope, Node, Pool, Event, From, To string
	At                                 time.Time
}

func (h *GKEUpgradePlanHandler) nodeEvents(cid int, since time.Time) ([]rawEvent, error) {
	rows, err := h.DB.Query(`
		SELECT COALESCE(scope,'node'),node_name,pool,event,from_version,to_version,detected_at
		  FROM k8s_node_version_events
		 WHERE cluster_id=? AND detected_at>=?
		 ORDER BY detected_at, node_name`, cid, since)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []rawEvent{}
	for rows.Next() {
		var e rawEvent
		var at sql.NullTime
		if rows.Scan(&e.Scope, &e.Node, &e.Pool, &e.Event, &e.From, &e.To, &at) != nil {
			continue
		}
		if !at.Valid {
			continue
		}
		e.At = at.Time
		out = append(out, e)
	}
	return out, rows.Err()
}

// pacesFromEvents 把事件流切成「每个池最近一次升级」，还原批次节奏。
//
// 一次升级在事件流里长这样（BLUE_GREEN，批次=3）：
//
//	10:00 added×3(新版本)   10:05 removed×3(旧版本)
//	10:20 added×3           10:25 removed×3
//	10:40 added×3           10:45 removed×3
//
// 所以批次数 = added 事件按时间聚簇后的簇数，批大小 = 每簇的节点数。
func pacesFromEvents(events []rawEvent) []measuredPace {
	byPool := map[string][]rawEvent{}
	for _, e := range events {
		// 控制面事件不参与节奏计算：它的 detected_at 精度是 6 小时，
		// 混进来会把批次切分和耗时全带偏（pool 为空本来也会被下面滤掉，这里显式挡一道）
		if e.Scope == "control_plane" || e.Pool == "" {
			continue
		}
		byPool[e.Pool] = append(byPool[e.Pool], e)
	}

	out := []measuredPace{}
	for pool, evs := range byPool {
		burst := latestBurst(evs)
		if len(burst) == 0 {
			continue
		}
		p := measuredPace{
			Pool:         pool,
			At:           burst[0].At.Format("2006-01-02 15:04:05"),
			TotalMinutes: int(burst[len(burst)-1].At.Sub(burst[0].At).Minutes()),
		}

		// 只用 added 判批次：removed 是滞后的清理动作，节奏没有 added 干净
		var added []rawEvent
		seen := map[string]bool{}
		for _, e := range burst {
			if e.Event == "added" && !seen[e.Node] {
				seen[e.Node] = true
				added = append(added, e)
			}
		}
		p.Nodes = len(added)
		for _, batch := range clusterByTime(added, batchGapMinutes) {
			p.Batches++
			p.BatchSizes = append(p.BatchSizes, len(batch))
			p.PerBatchMins = append(p.PerBatchMins,
				int(batch[len(batch)-1].At.Sub(batch[0].At).Minutes()))
		}
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Pool < out[j].Pool })
	return out
}

// latestBurst 取最近一簇事件（相邻间隔 <= upgradeBurstGapMinutes 视为同一次升级）。
func latestBurst(evs []rawEvent) []rawEvent {
	if len(evs) == 0 {
		return nil
	}
	sort.Slice(evs, func(i, j int) bool { return evs[i].At.Before(evs[j].At) })
	start := 0
	for i := 1; i < len(evs); i++ {
		if evs[i].At.Sub(evs[i-1].At) > upgradeBurstGapMinutes*time.Minute {
			start = i
		}
	}
	return evs[start:]
}

// clusterByTime 把有序事件按时间间隔聚簇。
func clusterByTime(evs []rawEvent, gapMinutes int) [][]rawEvent {
	if len(evs) == 0 {
		return nil
	}
	out := [][]rawEvent{{evs[0]}}
	for i := 1; i < len(evs); i++ {
		if evs[i].At.Sub(evs[i-1].At) > time.Duration(gapMinutes)*time.Minute {
			out = append(out, []rawEvent{evs[i]})
			continue
		}
		out[len(out)-1] = append(out[len(out)-1], evs[i])
	}
	return out
}

func toPaceOut(p measuredPace) poolPaceOut {
	o := poolPaceOut{
		Pool: p.Pool, At: p.At, Nodes: p.Nodes,
		Batches: p.Batches, TotalMinutes: p.TotalMinutes,
	}
	if len(p.BatchSizes) > 0 {
		o.BatchSize = medianInt(p.BatchSizes)
	}
	if len(p.PerBatchMins) > 0 {
		o.MedianBatch = medianInt(p.PerBatchMins)
		o.SlowestBatch = maxInt(p.PerBatchMins)
	}
	switch {
	case p.Batches <= 1:
		o.Note = "只有一批，说明整池一次性换完——批次参数可能没配（GKE 用了默认值），或池太小不足一批"
	default:
		o.Note = fmt.Sprintf("实测并行度约 %d 台/批，共 %d 批。外推到 N 台的池：ceil(N/%d) × 单批耗时",
			o.BatchSize, p.Batches, o.BatchSize)
	}
	return o
}

// extrapolateNote 把实测值翻译成「别的集群该排多久」的算法。
//
// 这是整个功能的落点：UAT 升完之后，人要拿着这段话去排生产窗口。
// 所以它必须写成能直接照做的形式，而不是一堆待解释的数字。
func extrapolateNote(pools []poolPaceOut) string {
	if len(pools) == 0 {
		return "还没有实测数据。升过一次之后，这里会给出可直接套用到其他集群的推算公式"
	}
	var b []string
	b = append(b, "外推到其他集群：总时长 ≈ 控制面耗时 + Σ 各池 ceil(池节点数 ÷ 实测并行度) × 单批耗时")
	for _, p := range pools {
		if p.BatchSize > 0 && p.MedianBatch > 0 {
			b = append(b, fmt.Sprintf(
				"　%s 实测：%d 台分 %d 批（每批 %d 台），单批中位 %d 分钟、最慢 %d 分钟，整池 %d 分钟",
				p.Pool, p.Nodes, p.Batches, p.BatchSize, p.MedianBatch, p.SlowestBatch, p.TotalMinutes))
		}
	}
	b = append(b, "⚠️ 直接外推会偏小，至少还要考虑三件本集群可能没有的事："+
		"① 目标集群若有余量为 0 的 PDB，drain 会被卡到超时才强杀，每节点可能多等一小时；"+
		"② 节点数更多的池，批次轮数更多；"+
		"③ 多副本有状态中间件的数据重同步比单副本慢得多。"+
		"建议按 1.3~1.5 的系数上浮，并用目标集群自己的预案页复核 PDB 与单副本清单")
	return joinLines(b)
}

// collectionHealth 采集此刻是否正常。
//
// 升级期间控制面重启会让采集短暂失败，这是预期内的；但要让看板上能看见，
// 否则「事件流突然没了」会被误读成「升级停住了」。
//
// ⚠️ k8s_sync_state 只存当前状态不存历史，所以 apiserver 的中断**时长**这里给不出来。
// 想拿到准确的中断窗口，只能在升级当时盯着看——这一点在返回文案里说清楚，不假装能算。
func (h *GKEUpgradePlanHandler) collectionHealth(cid int) (bool, string) {
	var ok int
	var lastSync sql.NullTime
	var errMsg string
	err := h.DB.QueryRow(`SELECT ok, last_sync, COALESCE(err,'') FROM k8s_sync_state
	                       WHERE cluster_id=? AND resource='nodes'`, cid).Scan(&ok, &lastSync, &errMsg)
	if err != nil {
		return false, "查不到节点采集状态：该集群可能从未采集成功过"
	}
	age := ""
	if lastSync.Valid {
		age = fmt.Sprintf("，上次成功 %s（%.0f 分钟前）",
			lastSync.Time.Format("15:04:05"), time.Since(lastSync.Time).Minutes())
	}
	if ok == 1 {
		return true, "节点采集正常" + age +
			"。注意：apiserver 的中断时长无法事后重建（采集状态只存当前值不存历史），需要在升级当时记录"
	}
	return false, "节点采集失败：" + errMsg + age +
		"。控制面升级期间出现这个是正常的，升完会自动恢复"
}

// measuredPoolPace 各池最近一次升级的实测节奏，供预案的耗时预估优先使用。
// 只看最近 90 天：更早的实测跨了太多版本，参考价值下降。
func (h *GKEUpgradePlanHandler) measuredPoolPace(cid int) map[string]*measuredPace {
	events, err := h.nodeEvents(cid, time.Now().AddDate(0, 0, -90))
	if err != nil {
		logx.J("gke_plan", "measured_pace_failed", map[string]any{"cluster_id": cid, "err": err.Error()})
		return nil
	}
	out := map[string]*measuredPace{}
	for _, p := range pacesFromEvents(events) {
		// 只换了一台多半是 auto-repair 单节点重建，不是升级，拿它当基准会严重偏小
		if p.Nodes < 2 {
			continue
		}
		cp := p
		out[p.Pool] = &cp
	}
	return out
}

// historyPace 从 GCP 升级历史里反推出的「每批实际要多久」。
//
// 为什么必须有这一档：预案原先只有「本集群节点事件实测」和「通用经验区间」两级，
// 而节点事件要升过一次才有，新集群永远落到经验区间上。可 gke_upgrade_history 里
// 早就躺着 GCP 给的真实耗时（精确到秒），一直没被用。
//
// 2026-07-31 用当时仅有的 3 条节点池历史校验，经验区间（8~20 分钟/批）错得离谱：
//   demo-pool-01        1 节点 1 批 → 实测每批 67 分钟
//   elasticsearch-pool  3 节点 1 批 → 实测每批  3 分钟
//   kafka-pool          3 节点 3 批 → 实测每批 22 分钟
// 相差 22 倍。原因是耗时主要取决于节点上跑的是什么（优雅停机、PVC 重挂、PDB 等待），
// 跟节点数几乎无关——拿一个通用分钟数去套，本来就套不准。
type historyPace struct {
	PerBatchMin int
	PerBatchMax int
	// 中位数：样本里常有离群值（2026-07-31 实测中 demo-pool-01 单节点花了 67 分钟／批，
	// 而同期 elasticsearch 三节点只花 3 分钟）。只给 min~max 的话，
	// 上限被离群值拉高、下限又过于乐观，排窗口的人只能二选一。
	PerBatchMedian int
	Samples        int
	SamePool       bool     // true=就是这个池自己的历史，可信度最高
	From           []string // 数据来自哪几个池，必须让人看得见
	perBatch       []int    // 原始样本，用于算中位数
}

// poolHistoryPace 取历史里的每批耗时。优先本池，没有再用其他池的同类实测。
//
// 反推方式：每批耗时 = (总时长 − 整池观察期 − 各批观察期) ÷ 批次数
// 观察期是配置的确定值，扣掉后剩下的才是真正在建节点、排空 Pod 的时间。
//
// ⚠️ 用的是各池**当前**的 batch/soak 配置去还原**当时**那次升级，配置改过就会有偏差。
// 所以跨池那档只作参考区间，且在 basis 里点名来源，不假装是本池实测。
func (h *GKEUpgradePlanHandler) poolHistoryPace(cid int, pool string) *historyPace {
	rows, err := h.DB.Query(`
		SELECT h.cluster_id, h.pool, h.started_at, h.ended_at,
		       COALESCE(p.node_count,0), COALESCE(p.bg_batch_node_count,0),
		       COALESCE(p.bg_node_pool_soak_sec,0), COALESCE(p.bg_batch_soak_sec,0)
		  FROM gke_upgrade_history h
		  LEFT JOIN gke_node_pools p ON p.cluster_id=h.cluster_id AND p.name=h.pool
		 WHERE h.scope='nodepool' AND h.state='SUCCEEDED'
		   AND h.started_at IS NOT NULL AND h.ended_at IS NOT NULL AND h.pool<>''`)
	if err != nil {
		logx.J("gke_plan", "history_pace_failed", map[string]any{"cluster_id": cid, "err": err.Error()})
		return nil
	}
	defer rows.Close()

	same := &historyPace{SamePool: true}
	other := &historyPace{}
	for rows.Next() {
		var hcid, nodeCount, batch, poolSoak, batchSoak int
		var hpool string
		var st, en sql.NullTime
		if rows.Scan(&hcid, &hpool, &st, &en, &nodeCount, &batch, &poolSoak, &batchSoak) != nil {
			continue
		}
		if !st.Valid || !en.Valid {
			continue
		}
		total := int(en.Time.Sub(st.Time).Minutes())
		if total <= 0 {
			continue
		}
		batches := 1
		if batch > 0 && nodeCount > 0 {
			batches = int(math.Ceil(float64(nodeCount) / float64(batch)))
		}
		// 扣掉配置死等的观察期，剩下才是真正干活的时间
		work := total - poolSoak/60 - batches*batchSoak/60
		if work < 1 {
			// 观察期比总时长还长，说明配置和当时对不上，这条样本不可用
			continue
		}
		per := work / batches
		if per < 1 {
			per = 1
		}

		t := other
		if hcid == cid && hpool == pool {
			t = same
		}
		if t.Samples == 0 || per < t.PerBatchMin {
			t.PerBatchMin = per
		}
		if per > t.PerBatchMax {
			t.PerBatchMax = per
		}
		t.Samples++
		t.perBatch = append(t.perBatch, per)
		t.From = append(t.From, fmt.Sprintf("%s(%d分/批)", hpool, per))
	}

	if same.Samples > 0 {
		same.PerBatchMedian = medianInt(same.perBatch)
		return same
	}
	if other.Samples > 0 {
		other.PerBatchMedian = medianInt(other.perBatch)
		return other
	}
	return nil
}

// measuredControlPlane 本集群控制面升级的历史耗时区间。
func (h *GKEUpgradePlanHandler) measuredControlPlane(cid int) (int, int, int, bool) {
	rows, err := h.DB.Query(`
		SELECT started_at, ended_at FROM gke_upgrade_history
		 WHERE cluster_id=? AND scope='control_plane' AND state='SUCCEEDED'
		   AND started_at IS NOT NULL AND ended_at IS NOT NULL`, cid)
	if err != nil {
		return 0, 0, 0, false
	}
	defer rows.Close()
	var mins []int
	for rows.Next() {
		var s, e sql.NullTime
		if rows.Scan(&s, &e) != nil || !s.Valid || !e.Valid {
			continue
		}
		if d := int(e.Time.Sub(s.Time).Minutes()); d > 0 {
			mins = append(mins, d)
		}
	}
	if len(mins) == 0 {
		return 0, 0, 0, false
	}
	sort.Ints(mins)
	return mins[0], mins[len(mins)-1], len(mins), true
}

func medianInt(v []int) int {
	s := append([]int{}, v...)
	sort.Ints(s)
	return s[len(s)/2]
}

func maxInt(v []int) int {
	m := v[0]
	for _, x := range v {
		if x > m {
			m = x
		}
	}
	return m
}

func joinLines(s []string) string {
	out := ""
	for i, x := range s {
		if i > 0 {
			out += "\n"
		}
		out += x
	}
	return out
}
