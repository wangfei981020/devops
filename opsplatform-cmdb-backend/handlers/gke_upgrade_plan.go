// GKE 升级预案生成器（只读，不触碰集群）。
//
// 目的：把「排一次集群升级」从人工翻文档+连服务器查，变成打开页面就能拿到的完整预案。
// 执行仍由人在 GCP 控制台完成——CMDB 负责回答「该怎么做、要多久、会断什么、卡在哪」。
//
// 输出的每一项都必须能回答「这个数字从哪来」：实测值、API 参数、还是经验区间。
// 排升级窗口是拿它去跟业务谈停机时间的，来源不清的数字比没有数字更糟。
package handlers

import (
	"database/sql"
	"fmt"
	"math"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"opsplatform-cmdb-backend/logx"
)

type GKEUpgradePlanHandler struct {
	DB *sql.DB
}

func NewGKEUpgradePlanHandler(db *sql.DB) *GKEUpgradePlanHandler {
	return &GKEUpgradePlanHandler{DB: db}
}

func (h *GKEUpgradePlanHandler) Register(r *gin.RouterGroup) {
	r.GET("/gke/upgrade/plan", h.Plan)
	r.GET("/gke/upgrade/progress", h.Progress)
	r.GET("/gke/available-versions", h.AvailableVersions)
}

// AvailableVersions 某集群所在区域可选的升级目标版本。
// 给前端下拉框用，也让人不必手输——手输没有任何校验，
// 打错一个字符预案照样算得出来，要到控制台才发现选不到这个版本。
func (h *GKEUpgradePlanHandler) AvailableVersions(c *gin.Context) {
	cid, _ := strconv.Atoi(c.Query("cluster_id"))
	if cid == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "cluster_id 必填"})
		return
	}
	var project, location string
	if err := h.DB.QueryRow(`SELECT project_id, location FROM k8s_clusters WHERE id=?`, cid).
		Scan(&project, &location); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "集群不存在"})
		return
	}

	kind := c.Query("kind")
	if kind == "" {
		kind = "master"
	}
	rows, err := h.DB.Query(`SELECT version FROM gke_available_versions
		WHERE project_id=? AND location=? AND kind=? ORDER BY sort_order`, project, location, kind)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "查询可用版本失败: " + err.Error()})
		return
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var v string
		if rows.Scan(&v) == nil {
			out = append(out, v)
		}
	}
	// 空清单不等于「没有可用版本」，多半是还没采过。必须让前端能区分，
	// 否则下拉框空着会被读成「这个集群没法升级」
	note := ""
	if len(out) == 0 {
		note = "尚未采集到该区域的可用版本清单。请先在「定时任务」里跑一次「GKE 集群升级信息采集」；" +
			"在此之前目标版本只能手输，且 CMDB 无法校验它是否存在"
	}
	c.JSON(http.StatusOK, gin.H{
		"cluster_id": cid, "project_id": project, "location": location,
		"kind": kind, "versions": out, "total": len(out), "note": note,
	})
}

// ---------------------------------------------------------------------------
// 经验区间：没有本集群实测数据时的兜底。
//
// 这些是 GKE 节点重建的粗略量级，**不是**任何一次实测的结果。凡是用到它们的预估，
// 输出里的 basis 都会写明「经验区间」，并在 incomplete 里列出缺哪些参数。
// 一旦某个池升过一次、k8s_node_version_events 里有了实测节奏，就改用实测值。
const (
	// 单批节点从开始重建到 Ready 的量级。下限是空载小节点，上限含镜像拉取与有状态 Pod 重挂 PVC。
	batchRebuildMinMinutes = 8
	batchRebuildMaxMinutes = 20
	// BLUE_GREEN 创建整个绿池的时间（节点并行创建，与池大小弱相关）
	blueGreenProvisionMinMinutes = 5
	blueGreenProvisionMaxMinutes = 12
	// 控制面小版本升级的量级。本集群有实测时优先用实测。
	controlPlaneMinMinutes = 6
	controlPlaneMaxMinutes = 20
)

type estimateOut struct {
	MinMinutes int      `json:"min_minutes"`
	MaxMinutes int      `json:"max_minutes"`
	Basis      string   `json:"basis"`      // 这个数字怎么来的：实测 / API 参数 / 经验区间
	Batches    int      `json:"batches"`    // 批次数，0=算不出
	Incomplete []string `json:"incomplete"` // 缺了哪些参数导致只能给区间
	Measured   bool     `json:"measured"`   // true=用了本集群实测值
}

type poolPlanOut struct {
	Name           string `json:"name"`
	NodeCount      int    `json:"node_count"`
	CurrentVersion string `json:"current_version"`
	Strategy       string `json:"strategy"`

	BatchNodeCount  *int     `json:"batch_node_count"`
	BatchPercentage *float64 `json:"batch_percentage"`
	BatchSoakSec    *int     `json:"batch_soak_sec"`
	NodePoolSoakSec *int     `json:"node_pool_soak_sec"`
	RolloutPolicy   string   `json:"rollout_policy"`

	Estimate estimateOut `json:"estimate"`

	ExtraNodesNeeded int    `json:"extra_nodes_needed"` // 升级期间临时多占的节点数（配额需求）
	QuotaNote        string `json:"quota_note"`

	AutoRepairOff bool `json:"auto_repair_off"`

	SingleReplicaWorkloads []affectedWorkload `json:"single_replica_workloads"` // 必然中断
	ConcentratedNodes      []concentratedNode `json:"concentrated_nodes"`       // 单点集中：一台上压了多个有状态 Pod
	Warnings               []string           `json:"warnings"`
}

type affectedWorkload struct {
	Namespace string `json:"namespace"`
	Kind      string `json:"kind"`
	Name      string `json:"name"`
	Node      string `json:"node"`
}

type concentratedNode struct {
	Node     string   `json:"node"`
	Count    int      `json:"count"`
	Pods     []string `json:"pods"`
	RiskNote string   `json:"risk_note"`
}

type blockingPDBOut struct {
	Namespace string   `json:"namespace"`
	Name      string   `json:"name"`
	RiskNote  string   `json:"risk_note"`
	Pools     []string `json:"pools"` // 该命名空间的 Pod 落在哪些池（近似，见 pools_note）
	PoolsNote string   `json:"pools_note"`
}

type baselineOut struct {
	Nodes     int `json:"nodes"`
	Pods      int `json:"pods"`
	Running   int `json:"running"`
	Failed    int `json:"failed"`
	Pending   int `json:"pending"`
	Workloads int `json:"workloads"`

	// false = 从没采集过 Pod，上面的数字不能当基线用。
	// 「集群是空的」与「没采过」数字上都是 0，但前者能做基线、后者不能。
	PodsCollected bool `json:"pods_collected"`

	// 升级前就已存在的异常。升级后原样出现 = 不是升级造成的。
	// 没有这份名单，事后看到 filebeat OOM 会被误判成升级事故。
	KnownBad []knownBadOut `json:"known_bad"`
	Note     string        `json:"note"`
	TakenAt  string        `json:"taken_at"`
}

type knownBadOut struct {
	Namespace string `json:"namespace"`
	Pod       string `json:"pod"`
	Phase     string `json:"phase"`
	Restarts  int    `json:"restarts"`
	Reason    string `json:"reason"`
}

type controlPlanePlanOut struct {
	CurrentVersion string      `json:"current_version"`
	TargetVersion  string      `json:"target_version"`
	MinorJump      int         `json:"minor_jump"`
	Estimate       estimateOut `json:"estimate"`
	Warnings       []string    `json:"warnings"`
}

type upgradePlanOut struct {
	ClusterID     int    `json:"cluster_id"`
	Cluster       string `json:"cluster"`
	Environment   string `json:"environment"`
	Project       string `json:"project_id"`
	Location      string `json:"location"`
	TargetVersion string `json:"target_version"`
	GeneratedAt   string `json:"generated_at"`

	ControlPlane controlPlanePlanOut `json:"control_plane"`
	Pools        []poolPlanOut       `json:"pools"`

	BlockingPDBs  []blockingPDBOut `json:"blocking_pdbs"`
	PDBsCollected bool             `json:"pdbs_collected"` // false → 「无阻塞」这个结论不成立
	PDBNote       string           `json:"pdb_note"`

	Baseline baselineOut `json:"baseline"`

	TotalEstimate estimateOut `json:"total_estimate"`
	ConsoleSteps  []string    `json:"console_steps"`
	Verification  []string    `json:"verification"`
	Warnings      []string    `json:"warnings"`
}

// Plan 生成一个集群的完整升级预案。
//
// target_version 不传时用 CMDB 推断的下一个小版本；推断不出就只出「现状+风险」部分，
// 不编一个版本号出来——升错版本的代价远大于让人自己填一次。
func (h *GKEUpgradePlanHandler) Plan(c *gin.Context) {
	cid, _ := strconv.Atoi(c.Query("cluster_id"))
	if cid == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "cluster_id 必填"})
		return
	}

	states, err := loadClusterUpgradeStates(h.DB)
	if err != nil {
		// 查询失败必须报错而不是返回空预案：空预案会被当成「没有风险」
		c.JSON(http.StatusInternalServerError, gin.H{"error": "读取集群升级状态失败: " + err.Error()})
		return
	}
	var st *clusterUpgradeState
	for i := range states {
		if states[i].ClusterID == cid {
			st = &states[i]
			break
		}
	}
	if st == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "集群不存在或尚未采集 GKE 升级信息"})
		return
	}

	target := strings.TrimSpace(c.Query("target_version"))
	if target == "" {
		target = st.MinorTarget
	}

	out := upgradePlanOut{
		ClusterID: st.ClusterID, Cluster: st.Name, Environment: st.Env,
		Project: st.Project, Location: st.Location,
		TargetVersion: target,
		GeneratedAt:   time.Now().Format("2006-01-02 15:04:05"),
	}
	if target == "" {
		out.Warnings = append(out.Warnings,
			"未指定目标版本，且 CMDB 也没推断出来（多半是该集群未加入发布通道）。"+
				"耗时预估与偏斜判断已跳过——请在 GCP 控制台确认可选版本后重新生成")
	} else if w := h.validateTarget(st, target); w != "" {
		// 填了个不存在的版本时，整份预案都是照着一个升不上去的目标算的。
		// 这必须在最显眼的地方说，不能等人到控制台下拉框里找不到才发现。
		out.Warnings = append(out.Warnings, w)
	}

	out.ControlPlane = h.planControlPlane(st, target)
	measuredPerPool := h.measuredPoolPace(cid)

	for _, p := range st.Pools {
		out.Pools = append(out.Pools, h.planPool(cid, p, target, measuredPerPool[p.Name], h.poolHistoryPace(cid, p.Name)))
	}

	out.BlockingPDBs, out.PDBsCollected, out.PDBNote = h.blockingPDBs(cid)
	out.Baseline = h.baseline(cid)
	out.TotalEstimate = totalEstimate(out.ControlPlane.Estimate, out.Pools)
	out.ConsoleSteps = consoleSteps(st, target, out.Pools)
	out.Verification = verificationChecklist(out.Baseline, target)
	out.Warnings = append(out.Warnings, planLevelWarnings(st, out)...)

	normalizePlanSlices(&out)

	logx.J("gke_plan", "generated", map[string]any{
		"cluster_id": cid, "target": target, "pools": len(out.Pools),
		"blocking_pdbs": len(out.BlockingPDBs), "pdbs_collected": out.PDBsCollected,
		"total_min": out.TotalEstimate.MinMinutes, "total_max": out.TotalEstimate.MaxMinutes,
	})
	c.JSON(http.StatusOK, out)
}

// validateTarget 校验目标版本在该集群所在区域是否真的可选。
//
// 返回空串表示通过（或无从校验）。三种结果分得开：
//
//	合法        → ""
//	清单没采到  → 提示去采，但不说「版本不存在」——没采到不等于不存在
//	确实不在清单→ 明确报错，并给出最接近的几个候选
//
// 这个区分很要紧：把「不知道」说成「不存在」会让人以为版本号写错了，
// 反过来把「不存在」当成「不知道」则会让人照着一个升不上去的目标排完整个窗口。
func (h *GKEUpgradePlanHandler) validateTarget(st *clusterUpgradeState, target string) string {
	rows, err := h.DB.Query(`SELECT version FROM gke_available_versions
		WHERE project_id=? AND location=? AND kind='master' ORDER BY sort_order`,
		st.Project, st.Location)
	if err != nil {
		logx.J("gke_plan", "validate_target_failed", map[string]any{
			"cluster_id": st.ClusterID, "err": err.Error()})
		return ""
	}
	defer rows.Close()
	var all []string
	for rows.Next() {
		var v string
		if rows.Scan(&v) == nil {
			all = append(all, v)
		}
	}
	if len(all) == 0 {
		return "无法校验目标版本：尚未采集该区域的可用版本清单。" +
			"请在「定时任务」里跑一次「GKE 集群升级信息采集」后重新生成预案"
	}
	for _, v := range all {
		if v == target {
			return ""
		}
	}
	// 同一小版本下的候选最有参考价值——多半就是补丁号敲错了
	maj, min, ok := parseMinor(target)
	var near []string
	if ok {
		prefix := fmt.Sprintf("%d.%d.", maj, min)
		for _, v := range all {
			if strings.HasPrefix(strings.TrimPrefix(v, "v"), prefix) {
				near = append(near, v)
				if len(near) >= 5 {
					break
				}
			}
		}
	}
	if len(near) == 0 && len(all) > 5 {
		near = all[:5]
	} else if len(near) == 0 {
		near = all
	}
	return fmt.Sprintf("🔴 目标版本 %s 不在 %s 的可选清单里（共 %d 个可选），"+
		"照这个版本算出来的预案没有意义。相近的可选版本：%s",
		target, st.Location, len(all), strings.Join(near, "、"))
}

// normalizePlanSlices 把所有 nil 切片换成空切片。
//
// Go 的 nil 切片序列化成 JSON null 而不是 []，消费方（前端 v-for、MCP 侧的 AI）
// 拿到 null 去迭代就会炸。这类字段全是「清单」语义，空清单就该是 []，
// 让每个调用方各自防御一遍 null 是把成本推给了下游。
func normalizePlanSlices(o *upgradePlanOut) {
	if o.Warnings == nil {
		o.Warnings = []string{}
	}
	if o.Pools == nil {
		o.Pools = []poolPlanOut{}
	}
	if o.BlockingPDBs == nil {
		o.BlockingPDBs = []blockingPDBOut{}
	}
	if o.ConsoleSteps == nil {
		o.ConsoleSteps = []string{}
	}
	if o.Verification == nil {
		o.Verification = []string{}
	}
	if o.Baseline.KnownBad == nil {
		o.Baseline.KnownBad = []knownBadOut{}
	}
	if o.ControlPlane.Warnings == nil {
		o.ControlPlane.Warnings = []string{}
	}
	normalizeEstimate(&o.ControlPlane.Estimate)
	normalizeEstimate(&o.TotalEstimate)
	for i := range o.Pools {
		p := &o.Pools[i]
		if p.Warnings == nil {
			p.Warnings = []string{}
		}
		if p.SingleReplicaWorkloads == nil {
			p.SingleReplicaWorkloads = []affectedWorkload{}
		}
		if p.ConcentratedNodes == nil {
			p.ConcentratedNodes = []concentratedNode{}
		}
		normalizeEstimate(&p.Estimate)
	}
}

func normalizeEstimate(e *estimateOut) {
	if e.Incomplete == nil {
		e.Incomplete = []string{}
	}
}

// planControlPlane 控制面部分。
//
// 控制面升级期间 apiserver 不可用（单区域集群）——已运行的 Pod 不受影响，
// 但 kubectl / ArgoCD / HPA / 调度全停。这条必须显式说出来，
// 因为它是唯一一个「业务无感但运维全停」的阶段，最容易在沟通时被漏掉。
func (h *GKEUpgradePlanHandler) planControlPlane(st *clusterUpgradeState, target string) controlPlanePlanOut {
	out := controlPlanePlanOut{
		CurrentVersion: st.MasterVersion,
		TargetVersion:  target,
		MinorJump:      minorDistance(st.MasterVersion, target),
	}

	est := estimateOut{
		MinMinutes: controlPlaneMinMinutes, MaxMinutes: controlPlaneMaxMinutes,
		Basis: "经验区间（GKE 控制面小版本升级量级）",
	}
	// 本集群升过就用本集群的——同一个控制面的历史时长比任何通用经验都准
	if lo, hi, n, ok := h.measuredControlPlane(st.ClusterID); ok {
		est.MinMinutes, est.MaxMinutes, est.Measured = lo, hi, true
		est.Basis = fmt.Sprintf("本集群实测（%d 次历史升级）", n)
	} else {
		est.Incomplete = append(est.Incomplete, "本集群无控制面升级历史记录，用的是经验区间")
	}
	out.Estimate = est

	out.Warnings = append(out.Warnings,
		"控制面升级期间 apiserver 不可用：kubectl、ArgoCD 同步、HPA 伸缩、新 Pod 调度全部停摆。"+
			"已运行的 Pod 与 Service 流量不受影响（kubelet/kube-proxy 不依赖 apiserver 存活）")

	if out.MinorJump > 1 {
		out.Warnings = append(out.Warnings, fmt.Sprintf(
			"目标版本跨了 %d 个小版本，GKE 控制面一次只能升一个小版本，必须分 %d 次升",
			out.MinorJump, out.MinorJump))
	}
	if st.SkewCritical {
		if st.SkewCurrent >= 2 {
			// 已经踩在硬限制上，不是"升完之后"的事，措辞不能再用将来时（CMDB-024）
			out.Warnings = append(out.Warnings, fmt.Sprintf(
				"节点池 %s 当前已落后控制面 %d 个小版本，正踩在 GKE 硬限制上——"+
					"必须先把该池升上来，控制面再升会被 GKE 拒绝",
				st.SkewPool, st.SkewCurrent))
		} else {
			out.Warnings = append(out.Warnings,
				"控制面升级后节点池将落后 2 个小版本，触及 GKE 硬限制——"+
					"控制面升完必须紧接着升节点池，不能隔夜")
		}
	}
	return out
}

// planPool 单个节点池的预案。
func (h *GKEUpgradePlanHandler) planPool(cid int, p poolState, target string, measured *measuredPace, hist *historyPace) poolPlanOut {
	out := poolPlanOut{
		Name: p.Name, NodeCount: p.NodeCount, CurrentVersion: p.Version,
		Strategy: p.Strategy, RolloutPolicy: p.BGRolloutPolicy,
		BatchNodeCount: p.BGBatchNodeCount, BatchPercentage: p.BGBatchPercentage,
		BatchSoakSec: p.BGBatchSoakSec, NodePoolSoakSec: p.BGNodePoolSoakSec,
		AutoRepairOff: p.RepairOff,
	}

	out.Estimate = estimatePool(p, measured, hist)
	out.ExtraNodesNeeded, out.QuotaNote = quotaNeed(p)

	out.SingleReplicaWorkloads = h.singleReplicaInPool(cid, p.Name)
	out.ConcentratedNodes = h.concentratedNodes(cid, p.Name)

	if p.RepairOff {
		out.Warnings = append(out.Warnings,
			"该池 autoRepair=false：升级中若有节点起不来，GKE 不会自动重建，必须人工介入。"+
				"排窗口时要预留人工处置时间")
	}
	if n := len(out.SingleReplicaWorkloads); n > 0 {
		out.Warnings = append(out.Warnings, fmt.Sprintf(
			"该池上有 %d 个单副本工作负载，升级期间必然中断（Pod 驱逐→新节点重建→PVC 重挂），"+
				"通常 1~3 分钟/个，没有优雅升级的办法，只能控制时长", n))
	}
	return out
}

// estimatePool 按策略算耗时。
//
// BLUE_GREEN：  创建绿池 + 批次数 × (每批重建 + batchSoak) + nodePoolSoak
// SURGE：       ceil(节点数 / maxSurge) × 每批重建
//
// 取值分四档，越靠前越可信，逐级降级并在 basis 里写明用的是哪一档：
//  1. 本池节点事件实测（CMDB 自采，±2 分钟）
//  2. 本池 GCP 升级历史（精确到秒，但保留期约两周）
//  3. 其他池的 GCP 历史反推出的每批耗时（真实测量，但工作负载不同）
//  4. 通用经验区间（最后手段，2026-07-31 实测证明它能偏差数倍）
//
// 任何一个参数缺失都不会被当成 0——观察期常常是 BLUE_GREEN 里最长的一段，
// 当成 0 会让预估严重偏小，而偏小的预估会直接变成排错的停机窗口。
func estimatePool(p poolState, measured *measuredPace, hist *historyPace) estimateOut {
	est := estimateOut{}

	if p.NodeCount <= 0 {
		est.Basis = "节点数未知，无法预估"
		est.Incomplete = append(est.Incomplete, "node_count 为 0")
		return est
	}

	// 有本池实测节奏就直接用，比任何参数推算都可信
	if measured != nil && measured.TotalMinutes > 0 {
		est.MinMinutes, est.MaxMinutes = measured.TotalMinutes, measured.TotalMinutes
		est.Batches = measured.Batches
		est.Measured = true
		est.Basis = fmt.Sprintf("本池实测（%s 那次升级，%d 个节点分 %d 批，耗时 %d 分钟）",
			measured.At, measured.Nodes, measured.Batches, measured.TotalMinutes)
		if measured.Nodes != p.NodeCount {
			est.Incomplete = append(est.Incomplete, fmt.Sprintf(
				"实测时该池 %d 个节点，现在是 %d 个，需按比例调整", measured.Nodes, p.NodeCount))
		}
		return est
	}

	if strings.EqualFold(p.Strategy, "BLUE_GREEN") {
		return estimateBlueGreen(p, hist)
	}
	return estimateSurge(p, hist)
}

// batchRange 每批重建耗时取哪个区间：有真实历史就用历史，没有才退到经验值。
// 返回的第三个值是这个区间的来源说明，必须一路带到 basis 里——
// 「3~67 分钟」这种宽区间只有在知道它是实测得来的时候才有意义。
func batchRange(hist *historyPace) (int, int, string) {
	if hist != nil && hist.Samples > 0 {
		src := "其他节点池的 GCP 升级历史"
		if hist.SamePool {
			src = "本池的 GCP 升级历史"
		}
		return hist.PerBatchMin, hist.PerBatchMax,
			fmt.Sprintf("%s实测每批 %d~%d 分钟（中位数 %d，%d 个样本：%s）。"+
				"样本离散时上限多半被离群值拉高，排窗口建议按中位数×批次数打底、再留出余量",
				src, hist.PerBatchMin, hist.PerBatchMax, hist.PerBatchMedian, hist.Samples,
				strings.Join(hist.From, "、"))
	}
	return batchRebuildMinMinutes, batchRebuildMaxMinutes,
		fmt.Sprintf("经验区间每批 %d~%d 分钟（无任何历史实测可用）", batchRebuildMinMinutes, batchRebuildMaxMinutes)
}

func estimateBlueGreen(p poolState, hist *historyPace) estimateOut {
	loBatch, hiBatch, batchBasis := batchRange(hist)
	est := estimateOut{Basis: "按 blueGreenSettings 参数推算，" + batchBasis}
	est.Measured = hist != nil && hist.Samples > 0
	if hist == nil || hist.Samples == 0 {
		est.Incomplete = append(est.Incomplete,
			"单批耗时用的是经验区间——实测显示它可能偏差数倍（1 节点的池曾花 67 分钟/批，3 节点的池只花 3 分钟）")
	} else if !hist.SamePool {
		est.Incomplete = append(est.Incomplete,
			"单批耗时来自其他节点池的实测，本池工作负载不同（优雅停机、PVC 重挂、PDB 等待都会显著改变耗时），只能当参考区间")
	}

	batch := blueGreenBatchSize(p)
	if batch <= 0 {
		est.Incomplete = append(est.Incomplete,
			"batchNodeCount 与 batchPercentage 都没配，批次数算不出——GKE 会用它自己的默认批次大小")
		// 给一个覆盖「一次全排空」到「一次一台」的区间，宽但诚实
		est.Batches = 0
		est.MinMinutes = blueGreenProvisionMinMinutes + loBatch
		est.MaxMinutes = blueGreenProvisionMaxMinutes + p.NodeCount*hiBatch
	} else {
		est.Batches = int(math.Ceil(float64(p.NodeCount) / float64(batch)))
		est.MinMinutes = blueGreenProvisionMinMinutes + est.Batches*loBatch
		est.MaxMinutes = blueGreenProvisionMaxMinutes + est.Batches*hiBatch
	}

	// 观察期是确定值（API 给了就是多少），直接加进去；没给则标出来
	if p.BGBatchSoakSec != nil && est.Batches > 0 {
		add := est.Batches * *p.BGBatchSoakSec / 60
		est.MinMinutes += add
		est.MaxMinutes += add
	} else if p.BGBatchSoakSec == nil {
		est.Incomplete = append(est.Incomplete, "batchSoakDuration 未配，每批之间的观察期按 GKE 默认值走，未计入")
	}
	if p.BGNodePoolSoakSec != nil {
		add := *p.BGNodePoolSoakSec / 60
		est.MinMinutes += add
		est.MaxMinutes += add
	} else {
		est.Incomplete = append(est.Incomplete, "nodePoolSoakDuration 未配，整池观察期按 GKE 默认值走，未计入")
	}

	if p.BGRolloutPolicy == "AUTOSCALED" {
		est.Incomplete = append(est.Incomplete,
			"批次由 cluster autoscaler 决定，没有固定批次参数，预估只能给宽区间")
	}
	return est
}

func estimateSurge(p poolState, hist *historyPace) estimateOut {
	loBatch, hiBatch, batchBasis := batchRange(hist)
	est := estimateOut{Basis: "按 maxSurge 推算，" + batchBasis}
	est.Measured = hist != nil && hist.Samples > 0
	surge := p.MaxSurge
	if surge <= 0 {
		// maxSurge=0 且 maxUnavailable=0 是无效配置（升级无法推进）；
		// 更可能是 API 没返回。按每次 1 台算并标注。
		surge = 1
		est.Incomplete = append(est.Incomplete,
			"maxSurge 为 0，按每次 1 台估算；若实际配置不同请在控制台核对 upgradeSettings")
	}
	est.Batches = int(math.Ceil(float64(p.NodeCount) / float64(surge)))
	est.MinMinutes = est.Batches * loBatch
	est.MaxMinutes = est.Batches * hiBatch
	return est
}

func blueGreenBatchSize(p poolState) int {
	if p.BGBatchNodeCount != nil && *p.BGBatchNodeCount > 0 {
		return *p.BGBatchNodeCount
	}
	if p.BGBatchPercentage != nil && *p.BGBatchPercentage > 0 {
		n := int(math.Ceil(float64(p.NodeCount) * *p.BGBatchPercentage))
		if n < 1 {
			n = 1
		}
		return n
	}
	return 0
}

// quotaNeed 升级期间临时多占多少节点。
//
// BLUE_GREEN 会先把整个绿池起起来再切，所以峰值是双倍——配额不够时升级会直接失败，
// 而这个失败发生在深夜窗口里，代价很高。
func quotaNeed(p poolState) (int, string) {
	if strings.EqualFold(p.Strategy, "BLUE_GREEN") {
		return p.NodeCount, fmt.Sprintf(
			"BLUE_GREEN 会先创建等量的绿池再切流，升级峰值需要 %d 台（现有 %d + 新建 %d）。"+
				"升级前须确认 GCP 的 CPU / 内存 / 磁盘 / 内网 IP 配额都够翻倍，否则升级会中途失败",
			p.NodeCount*2, p.NodeCount, p.NodeCount)
	}
	surge := p.MaxSurge
	if surge <= 0 {
		surge = 1
	}
	return surge, fmt.Sprintf("SURGE 策略升级期间额外占用 %d 台，配额压力小", surge)
}

// singleReplicaInPool 该池上必然中断的单副本工作负载。
//
// 判据是 replicas_desired=1 的 Deployment/StatefulSet——它们没有第二个副本接管，
// Pod 一被驱逐服务就断，这是 PDB 也救不了的。
func (h *GKEUpgradePlanHandler) singleReplicaInPool(cid int, pool string) []affectedWorkload {
	rows, err := h.DB.Query(`
		SELECT DISTINCT w.namespace, w.kind, w.name, p.node_name
		  FROM k8s_workloads w
		  JOIN k8s_pods  p ON p.cluster_id=w.cluster_id AND p.namespace=w.namespace AND p.workload=w.name
		  JOIN k8s_nodes n ON n.cluster_id=p.cluster_id AND n.name=p.node_name
		 WHERE w.cluster_id=? AND n.pool=? AND w.replicas_desired=1
		   AND w.kind IN ('Deployment','StatefulSet')
		 ORDER BY w.namespace, w.name`, cid, pool)
	if err != nil {
		logx.J("gke_plan", "single_replica_query_failed", map[string]any{
			"cluster_id": cid, "pool": pool, "err": err.Error(),
		})
		return nil
	}
	defer rows.Close()
	out := []affectedWorkload{}
	for rows.Next() {
		var w affectedWorkload
		if rows.Scan(&w.Namespace, &w.Kind, &w.Name, &w.Node) == nil {
			out = append(out, w)
		}
	}
	return out
}

// concentratedNodes 单点集中：一台节点上压了多个有状态/单副本 Pod。
//
// drain 这样一台，多个服务同时中断。UAT 那台同时扛着 sso-mysql + 两个 redis 成员，
// 排升级顺序时必须知道这种节点在哪——它决定了「哪个池放最后升」。
func (h *GKEUpgradePlanHandler) concentratedNodes(cid int, pool string) []concentratedNode {
	rows, err := h.DB.Query(`
		SELECT p.node_name, p.namespace, p.name
		  FROM k8s_pods  p
		  JOIN k8s_nodes n ON n.cluster_id=p.cluster_id AND n.name=p.node_name
		  JOIN k8s_workloads w ON w.cluster_id=p.cluster_id AND w.namespace=p.namespace AND w.name=p.workload
		 WHERE p.cluster_id=? AND n.pool=?
		   AND (w.kind='StatefulSet' OR w.replicas_desired=1)
		 ORDER BY p.node_name, p.namespace, p.name`, cid, pool)
	if err != nil {
		logx.J("gke_plan", "concentrated_query_failed", map[string]any{
			"cluster_id": cid, "pool": pool, "err": err.Error(),
		})
		return nil
	}
	defer rows.Close()

	byNode := map[string][]string{}
	for rows.Next() {
		var node, ns, name string
		if rows.Scan(&node, &ns, &name) == nil {
			byNode[node] = append(byNode[node], ns+"/"+name)
		}
	}

	out := []concentratedNode{}
	for node, pods := range byNode {
		// 2 个及以上才算集中——1 个是常态，报出来只会淹没真正的风险点
		if len(pods) < 2 {
			continue
		}
		out = append(out, concentratedNode{
			Node: node, Count: len(pods), Pods: pods,
			RiskNote: fmt.Sprintf("drain 这一台会同时中断 %d 个有状态/单副本服务，"+
				"升级前应确认它们不是同一套集群的多数派（如 Redis Cluster 的多个成员、etcd 的多个副本）", len(pods)),
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		return out[i].Node < out[j].Node
	})
	return out
}

// blockingPDBs 余量为 0 的 PDB —— drain 会被它们卡到超时。
//
// ⚠️ Pod↔PDB 的精确关联做不到：PDB 用 label selector 选 Pod，而 CMDB 的 k8s_pods
// 没有存 label。这里退而用命名空间聚合——列出该 PDB 所在命名空间的 Pod 落在哪些节点池。
// 这是**近似**，输出里明确标注，不能拿它当「这个池一定会被卡住」的断言。
func (h *GKEUpgradePlanHandler) blockingPDBs(cid int) ([]blockingPDBOut, bool, string) {
	collected := pdbCollected(h.DB, cid)
	if !collected {
		return nil, false, "该集群未采集到 PodDisruptionBudget（多半是只读 RBAC 缺 policy 组）。" +
			"因此「无阻塞风险」这个结论不成立——升级前请在控制台确认，或补齐 RBAC 后重新采集"
	}

	rows, err := h.DB.Query(`
		SELECT namespace,name,current_healthy,desired_healthy,expected_pods,disruptions_allowed
		  FROM k8s_pdbs WHERE cluster_id=? AND disruptions_allowed=0
		 ORDER BY namespace,name`, cid)
	if err != nil {
		logx.J("gke_plan", "pdb_query_failed", map[string]any{"cluster_id": cid, "err": err.Error()})
		return nil, false, "查询 PDB 失败: " + err.Error()
	}
	defer rows.Close()

	out := []blockingPDBOut{}
	for rows.Next() {
		var p pdbOut
		if rows.Scan(&p.Namespace, &p.Name, &p.CurrentHealthy, &p.DesiredHealthy,
			&p.ExpectedPods, &p.DisruptionsAllowed) != nil {
			continue
		}
		out = append(out, blockingPDBOut{
			Namespace: p.Namespace, Name: p.Name, RiskNote: pdbRiskNote(p),
			Pools:     h.poolsOfNamespace(cid, p.Namespace),
			PoolsNote: "按命名空间聚合的近似值：CMDB 未存 Pod label，无法精确匹配 PDB 的 selector",
		})
	}

	note := "余量为 0 的 PDB 会让节点 drain 被一直拒绝，直到 GKE 超时强杀，单节点可能因此多花一小时。" +
		"升级前应先把副本修好，或临时放宽这些 PDB"
	if len(out) == 0 {
		note = "当前没有余量为 0 的 PDB。注意这是采集时刻的瞬时值——" +
			"有副本正在重启时余量会临时变 0，升级开始前建议再确认一次"
	}
	return out, true, note
}

func (h *GKEUpgradePlanHandler) poolsOfNamespace(cid int, ns string) []string {
	rows, err := h.DB.Query(`
		SELECT DISTINCT n.pool FROM k8s_pods p
		  JOIN k8s_nodes n ON n.cluster_id=p.cluster_id AND n.name=p.node_name
		 WHERE p.cluster_id=? AND p.namespace=? AND n.pool<>'' ORDER BY n.pool`, cid, ns)
	if err != nil {
		return nil
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var s string
		if rows.Scan(&s) == nil {
			out = append(out, s)
		}
	}
	return out
}

// baseline 升级前基线快照。
//
// 这是预案里最容易被跳过、事后最后悔没留的一项：集群升级前本来就有一堆异常 Pod，
// 升级后看到它们会以为是升级砸的，白白中止升级或熬夜排查。
// 把「升级前就坏的」列清楚，事后逐条比对即可。
func (h *GKEUpgradePlanHandler) baseline(cid int) baselineOut {
	b := baselineOut{TakenAt: time.Now().Format("2006-01-02 15:04:05")}
	b.Note = "升级后重新生成一次预案，逐项比对这些数字。" +
		"下面「升级前已存在的异常」若原样出现，说明与升级无关，不必排查"

	// 「集群里没有 Pod」和「从没采集过 Pod」在数字上都是 0，但对预案的意义天差地别：
	// 前者可以拿来做基线，后者说明这份基线根本不能用。和 PDB 的 collected 是同一类问题。
	b.PodsCollected = resourceCollected(h.DB, cid, "pods")
	if !b.PodsCollected {
		b.Note = "⚠️ 该集群尚未成功采集过 Pod 数据，下面的规模数字不可用作基线。" +
			"请先在「K8s → 集群管理」对该集群执行一次采集，再重新生成预案"
	}

	_ = h.DB.QueryRow(`SELECT COUNT(*) FROM k8s_nodes WHERE cluster_id=?`, cid).Scan(&b.Nodes)
	_ = h.DB.QueryRow(`SELECT COUNT(*) FROM k8s_workloads WHERE cluster_id=?`, cid).Scan(&b.Workloads)
	_ = h.DB.QueryRow(`
		SELECT COUNT(*),
		       SUM(phase='Running'), SUM(phase='Failed'), SUM(phase='Pending')
		  FROM k8s_pods WHERE cluster_id=?`, cid).Scan(&b.Pods, &b.Running, &b.Failed, &b.Pending)

	// 重启 50 次以上，或不在 Running 状态——两类都是「升级前就不健康」
	rows, err := h.DB.Query(`
		SELECT namespace,name,phase,restarts FROM k8s_pods
		 WHERE cluster_id=? AND (restarts>=50 OR phase<>'Running')
		 ORDER BY restarts DESC, namespace, name LIMIT 200`, cid)
	if err != nil {
		logx.J("gke_plan", "baseline_query_failed", map[string]any{"cluster_id": cid, "err": err.Error()})
		return b
	}
	defer rows.Close()
	for rows.Next() {
		var k knownBadOut
		if rows.Scan(&k.Namespace, &k.Pod, &k.Phase, &k.Restarts) != nil {
			continue
		}
		switch {
		case k.Restarts >= 50:
			k.Reason = fmt.Sprintf("已重启 %d 次（升级前即如此）", k.Restarts)
		default:
			k.Reason = "升级前已处于 " + k.Phase + " 状态"
		}
		b.KnownBad = append(b.KnownBad, k)
	}
	return b
}

func totalEstimate(cp estimateOut, pools []poolPlanOut) estimateOut {
	out := estimateOut{
		MinMinutes: cp.MinMinutes, MaxMinutes: cp.MaxMinutes,
		Basis:      "控制面 + 各节点池逐个串行（GKE 不支持多池并行升级）",
		Incomplete: append([]string{}, cp.Incomplete...),
		Measured:   cp.Measured,
	}
	for _, p := range pools {
		out.MinMinutes += p.Estimate.MinMinutes
		out.MaxMinutes += p.Estimate.MaxMinutes
		for _, s := range p.Estimate.Incomplete {
			out.Incomplete = append(out.Incomplete, p.Name+"："+s)
		}
		if !p.Estimate.Measured {
			out.Measured = false
		}
	}
	return out
}

// consoleSteps 控制台执行步骤。
//
// 用户明确要求执行走 GCP 控制台而非命令行，所以这里给的是页面路径而不是 gcloud 命令。
// 顺序是硬约束不是建议：控制面必须先升，影响面小的池先升，扛着关键有状态服务的池放最后。
func consoleSteps(st *clusterUpgradeState, target string, pools []poolPlanOut) []string {
	// 项目/区域可能为空（非 GKE 或未采集全），空值不要留下「（项目 ，区域 ）」这种半截括号
	loc := ""
	switch {
	case st.Project != "" && st.Location != "":
		loc = fmt.Sprintf("（项目 %s，区域 %s）", st.Project, st.Location)
	case st.Project != "":
		loc = fmt.Sprintf("（项目 %s）", st.Project)
	case st.Location != "":
		loc = fmt.Sprintf("（区域 %s）", st.Location)
	}
	steps := []string{
		fmt.Sprintf("GCP 控制台 → Kubernetes Engine → 集群 → %s%s", st.Name, loc),
	}
	if target != "" {
		steps = append(steps, fmt.Sprintf(
			"① 控制面：集群详情页「基本信息 → 版本」右侧的升级图标 → 选择 %s → 确认。"+
				"此后 kubectl 会失败若干分钟，属正常", target))
	} else {
		steps = append(steps, "① 控制面：集群详情页「基本信息 → 版本」右侧升级图标 → 在下拉框里选目标版本")
	}
	steps = append(steps,
		"② 等控制面升完再动节点池——控制面没升完就升节点池会被 GKE 拒绝",
		"③ 立即在 CMDB「定时任务」里手动跑一次「GKE 集群升级信息采集」。"+
			"GCP 的升级历史保留期很短，且同一对象连升两次时前一次的记录可能被覆盖，"+
			"所以每个阶段升完都要立刻采一次，不能等定时任务",
	)

	// 池的升级顺序：单副本工作负载少的先升，多的后升
	ordered := append([]poolPlanOut{}, pools...)
	sort.Slice(ordered, func(i, j int) bool {
		return len(ordered[i].SingleReplicaWorkloads) < len(ordered[j].SingleReplicaWorkloads)
	})
	for i, p := range ordered {
		// 理由必须随位置变化：所有池都没有单副本服务时，若一律写「影响面最小，先升」，
		// 排在后面的池也会顶着「先升」两个字，照着做就乱了
		var why string
		n := len(p.SingleReplicaWorkloads)
		switch {
		case len(ordered) == 1:
			// 只有一个池，谈不上先后，说「排在后面」反而让人以为漏了别的池
			if n > 0 {
				why = fmt.Sprintf("唯一节点池，有 %d 个单副本服务会中断", n)
			} else {
				why = "唯一节点池"
			}
		case n > 0:
			why = fmt.Sprintf("有 %d 个单副本服务会中断，故排在后面", n)
		case i == 0:
			why = "无单副本服务，影响面最小，先升"
		default:
			why = "无单副本服务，顺序不敏感"
		}
		steps = append(steps, fmt.Sprintf(
			"④.%d 节点池 %s（%d 台，%s）：集群详情页「节点」标签 → 点该节点池 → 「升级」→ 选目标版本。升完立刻再采集一次",
			i+1, p.Name, p.NodeCount, why))
	}
	steps = append(steps,
		"⑤ 全部升完后，在 CMDB 重新生成一次本预案，用「升级前基线」逐项比对")
	return steps
}

func verificationChecklist(b baselineOut, target string) []string {
	v := []string{}
	if target != "" {
		v = append(v, "所有节点的 kubelet 版本都变成 "+target+"（CMDB「版本与升级」看板，或 list_nodes）")
	}
	v = append(v, "全部节点池版本一致，「偏斜」与「脱队」标签消失")
	// 没采过 Pod 时给不出可比对的数字，此时写「回到 0」是误导——直接说清楚该先干什么
	if b.PodsCollected {
		v = append(v,
			fmt.Sprintf("Pod 总数回到 %d 左右（升级前基线），Running %d", b.Pods, b.Running),
			fmt.Sprintf("Failed 不超过 %d、Pending 不超过 %d（升级前基线）；超出的部分逐个与「升级前已存在的异常」比对",
				b.Failed, b.Pending))
	} else {
		v = append(v, "⚠️ 升级前没有 Pod 基线可比对（该集群未采集过 Pod）——"+
			"升级前请先跑一次采集，否则升级后无法区分「新坏的」和「本来就坏的」")
	}
	v = append(v,
		"Pending 若明显增多，先查是不是 PVC 跨可用区挂不上——multi-zonal 集群升级后的典型问题",
		"支持截止日期已推后（CMDB 看板「支持截止」列）",
		"单副本服务逐个确认可用：登录、数据库连接、CI 任务能跑",
		"重新打开升级前关掉的 ArgoCD auto-sync 与 CI 定时任务",
	)
	return v
}

func planLevelWarnings(st *clusterUpgradeState, out upgradePlanOut) []string {
	var w []string
	if !out.PDBsCollected {
		w = append(w, "未采集到 PDB，drain 阻塞风险为「未知」而非「无风险」——这两者区别很大")
	}
	// 缺参数的提示由前端单独渲染成带明细的一条，这里不再重复一遍摘要
	if strings.TrimSpace(st.ReleaseChannel) == "" {
		w = append(w, "该集群未加入发布通道，自动升级日期按 STABLE 通道推算，与实际可能有偏差")
	}
	return w
}

// minorDistance 算两个 GKE 版本相差几个小版本；解析不出返回 0。
func minorDistance(from, to string) int {
	fMaj, fMin, ok1 := parseMinor(from)
	tMaj, tMin, ok2 := parseMinor(to)
	if !ok1 || !ok2 || tMaj != fMaj {
		return 0
	}
	if d := tMin - fMin; d > 0 {
		return d
	}
	return 0
}

func parseMinor(v string) (int, int, bool) {
	v = strings.TrimPrefix(strings.TrimSpace(v), "v")
	parts := strings.SplitN(v, ".", 3)
	if len(parts) < 2 {
		return 0, 0, false
	}
	maj, e1 := strconv.Atoi(parts[0])
	min, e2 := strconv.Atoi(parts[1])
	if e1 != nil || e2 != nil {
		return 0, 0, false
	}
	return maj, min, true
}
