// GKE 升级信息与操作历史采集（只读）。
//
// 回答四个此前完全查不到的问题：
//  1. 下次自动升级会升到哪个版本、被什么挡住了 —— fetchClusterUpgradeInfo
//  2. 节点池自动升级/自动修复开没开、升级时同时挂几个节点 —— nodePools.list
//  3. 历史上什么时候升过、是 Google 自动升的还是我们手动升的 —— upgradeDetails[].startType
//  4. 哪些节点被静默 drain 重建过 —— operations.list 里 operationType=AUTO_REPAIR_NODES
//
// 权限：container.clusters.get + container.operations.list，都在 container.read-only scope 内。
package k8ssource

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	container "google.golang.org/api/container/v1"
	"google.golang.org/api/option"

	"opsplatform-cmdb-backend/logx"
)

// UpgradeRecord 一条升级记录（来自 upgradeDetails[]，带 StartType）。
type UpgradeRecord struct {
	Scope          string `json:"scope"` // control_plane / nodepool
	Pool           string `json:"pool"`
	StartType      string `json:"start_type"` // AUTOMATIC / MANUAL —— 核心字段
	State          string `json:"state"`      // SUCCEEDED/FAILED/CANCELED/RUNNING/UNKNOWN
	InitialVersion string `json:"initial_version"`
	TargetVersion  string `json:"target_version"`
	StartTime      string `json:"start_time"` // RFC3339
	EndTime        string `json:"end_time"`
}

// ClusterUpgradeSnapshot 集群级升级信息（clusters.get + fetchClusterUpgradeInfo 合并）。
type ClusterUpgradeSnapshot struct {
	ReleaseChannel        string          `json:"release_channel"`
	CurrentMasterVersion  string          `json:"current_master_version"`
	MinorTargetVersion    string          `json:"minor_target_version"`
	PatchTargetVersion    string          `json:"patch_target_version"`
	AutoUpgradeStatus     []string        `json:"auto_upgrade_status"`
	PausedReason          []string        `json:"paused_reason"`
	EOSStandard           string          `json:"eos_standard"` // RFC3339
	EOSExtended           string          `json:"eos_extended"`
	MaintenancePolicyJSON string          `json:"maintenance_policy_json"`
	UpgradeDetails        []UpgradeRecord `json:"upgrade_details"`
}

// NodePoolSnapshot 节点池快照（nodePools.list + fetchNodePoolUpgradeInfo 合并）。
type NodePoolSnapshot struct {
	Name                 string          `json:"name"`
	NodeCount            int             `json:"node_count"`
	Version              string          `json:"version"`
	Status               string          `json:"status"`
	AutoUpgrade          bool            `json:"auto_upgrade"`
	AutoRepair           bool            `json:"auto_repair"`
	AutoUpgradeStartTime string          `json:"auto_upgrade_start_time"` // 仅在升级临近时才有值
	UpgradeDescription   string          `json:"upgrade_description"`
	MaxSurge             int             `json:"max_surge"`
	MaxUnavailable       int             `json:"max_unavailable"`
	Strategy             string          `json:"strategy"` // SURGE / BLUE_GREEN
	BlueGreenPhase       string          `json:"blue_green_phase"`

	// BLUE_GREEN 的批次与观察期参数。只有 Strategy=BLUE_GREEN 时才有意义，
	// 是升级耗时预估的决定性输入（见 migration 070）。
	// 两个 *Sec 用指针：API 没给和「配成 0」必须区分得开——
	// 存成 0 会让预估时长凭空少掉观察期，而观察期往往是 BLUE_GREEN 里最长的一段。
	BGRolloutPolicy   string   `json:"bg_rollout_policy"` // STANDARD / AUTOSCALED / ""
	BGBatchNodeCount  int      `json:"bg_batch_node_count"`
	BGBatchPercentage float64  `json:"bg_batch_percentage"`
	BGBatchSoakSec    *int     `json:"bg_batch_soak_sec"`
	BGNodePoolSoakSec *int     `json:"bg_node_pool_soak_sec"`
	AutoUpgradeStatus    []string        `json:"auto_upgrade_status"`
	PausedReason         []string        `json:"paused_reason"`
	MinorTargetVersion   string          `json:"minor_target_version"`
	EOSStandard          string          `json:"eos_standard"`
	EOSExtended          string          `json:"eos_extended"`
	UpgradeDetails       []UpgradeRecord `json:"upgrade_details"`
}

// OperationRecord 一条 GKE 操作记录。
type OperationRecord struct {
	Name          string `json:"name"`
	Type          string `json:"type"` // UPGRADE_MASTER / UPGRADE_NODES / AUTO_REPAIR_NODES / ...
	Status        string `json:"status"`
	StartTime     string `json:"start_time"`
	EndTime       string `json:"end_time"`
	Detail        string `json:"detail"`
	StatusMessage string `json:"status_message"`
	TargetLink    string `json:"target_link"`
	Location      string `json:"location"`
	Pool          string `json:"pool"`          // 从 TargetLink 解析
	RepairReason  string `json:"repair_reason"` // 从 Detail/StatusMessage 解析，解析不出留空
}

func gkeService(ctx context.Context, saJSON []byte) (*container.Service, error) {
	svc, err := container.NewService(ctx, option.WithCredentialsJSON(saJSON), option.WithScopes(container.CloudPlatformScope))
	if err != nil {
		return nil, fmt.Errorf("GKE 客户端: %w", err)
	}
	return svc, nil
}

func clusterPath(project, location, cluster string) string {
	return fmt.Sprintf("projects/%s/locations/%s/clusters/%s", project, location, cluster)
}

// FetchClusterUpgrade 取集群的通道、维护策略、升级目标、暂停原因和升级历史。
func FetchClusterUpgrade(ctx context.Context, saJSON []byte, project, location, cluster string) (*ClusterUpgradeSnapshot, error) {
	svc, err := gkeService(ctx, saJSON)
	if err != nil {
		return nil, err
	}
	name := clusterPath(project, location, cluster)
	out := &ClusterUpgradeSnapshot{}

	// clusters.get —— 通道 + 维护策略（维护窗口/维护排除都在这里）
	c, err := svc.Projects.Locations.Clusters.Get(name).Context(ctx).Do()
	if err != nil {
		return nil, fmt.Errorf("clusters.get %s: %w", cluster, err)
	}
	out.CurrentMasterVersion = c.CurrentMasterVersion
	if c.ReleaseChannel != nil {
		out.ReleaseChannel = c.ReleaseChannel.Channel
	}
	// 通道为空不是异常：官方规则是「未入通道的集群自动升级日期取 Stable 列」，
	// 但必须让人看见，否则排期算错了没人知道原因。
	if out.ReleaseChannel == "" || out.ReleaseChannel == "UNSPECIFIED" {
		logx.J("gke_upgrade", "no_release_channel", map[string]any{
			"cluster": cluster, "note": "未加入发布通道，排期将回退取 STABLE 列",
		})
	}
	if c.MaintenancePolicy != nil {
		if b, e := json.Marshal(c.MaintenancePolicy); e == nil {
			out.MaintenancePolicyJSON = string(b)
		}
	}

	// fetchClusterUpgradeInfo —— 升级目标 / 暂停原因 / 支持截止 / 历史
	ui, err := svc.Projects.Locations.Clusters.FetchClusterUpgradeInfo(name).Context(ctx).Do()
	if err != nil {
		return nil, fmt.Errorf("fetchClusterUpgradeInfo %s: %w", cluster, err)
	}
	out.MinorTargetVersion = ui.MinorTargetVersion
	out.PatchTargetVersion = ui.PatchTargetVersion
	out.AutoUpgradeStatus = ui.AutoUpgradeStatus
	out.PausedReason = ui.PausedReason
	out.EOSStandard = ui.EndOfStandardSupportTimestamp
	out.EOSExtended = ui.EndOfExtendedSupportTimestamp
	out.UpgradeDetails = toUpgradeRecords(ui.UpgradeDetails, "control_plane", "")

	logx.J("gke_upgrade", "cluster_fetched", map[string]any{
		"cluster": cluster, "channel": out.ReleaseChannel, "master": out.CurrentMasterVersion,
		"minor_target": out.MinorTargetVersion, "status": strings.Join(out.AutoUpgradeStatus, ","),
		"paused": strings.Join(out.PausedReason, ","), "history": len(out.UpgradeDetails),
	})
	return out, nil
}

// FetchNodePools 取集群下全部节点池，含各自的自动管理开关、升级策略与升级历史。
// 单个节点池的 upgradeInfo 取失败不影响其他池（只记日志），因为 list 的结果本身已经有价值。
func FetchNodePools(ctx context.Context, saJSON []byte, project, location, cluster string) ([]NodePoolSnapshot, error) {
	svc, err := gkeService(ctx, saJSON)
	if err != nil {
		return nil, err
	}
	parent := clusterPath(project, location, cluster)
	resp, err := svc.Projects.Locations.Clusters.NodePools.List(parent).Context(ctx).Do()
	if err != nil {
		return nil, fmt.Errorf("nodePools.list %s: %w", cluster, err)
	}

	out := make([]NodePoolSnapshot, 0, len(resp.NodePools))
	for _, np := range resp.NodePools {
		s := NodePoolSnapshot{
			Name:      np.Name,
			NodeCount: int(np.InitialNodeCount),
			Version:   np.Version,
			Status:    np.Status,
		}
		// 实际节点数以 instanceGroupUrls 数量为准的做法不可靠（多 zone 会分组），
		// 用 autoscaling 的当前值或 initialNodeCount 兜底；真实数量以 CMDB k8s_nodes 表为准。
		if np.Management != nil {
			s.AutoUpgrade, s.AutoRepair = np.Management.AutoUpgrade, np.Management.AutoRepair
			if np.Management.UpgradeOptions != nil {
				s.AutoUpgradeStartTime = np.Management.UpgradeOptions.AutoUpgradeStartTime
				s.UpgradeDescription = np.Management.UpgradeOptions.Description
				if s.AutoUpgradeStartTime != "" {
					// 这是最后的拦截机会：官方只在「升级即将开始」时才填这个字段
					logx.J("gke_upgrade", "auto_upgrade_imminent", map[string]any{
						"cluster": cluster, "pool": np.Name, "start_time": s.AutoUpgradeStartTime,
					})
				}
			}
		}
		if np.UpgradeSettings != nil {
			s.MaxSurge, s.MaxUnavailable = int(np.UpgradeSettings.MaxSurge), int(np.UpgradeSettings.MaxUnavailable)
			s.Strategy = np.UpgradeSettings.Strategy
			applyBlueGreenSettings(&s, np.UpgradeSettings, cluster)
		}
		if np.UpdateInfo != nil && np.UpdateInfo.BlueGreenInfo != nil {
			s.BlueGreenPhase = np.UpdateInfo.BlueGreenInfo.Phase
		}

		// 节点池级 upgradeInfo（2026-07-31 实测：与集群级同构，能独立拿到暂停原因和历史）
		npName := parent + "/nodePools/" + np.Name
		if ui, e := svc.Projects.Locations.Clusters.NodePools.FetchNodePoolUpgradeInfo(npName).Context(ctx).Do(); e != nil {
			logx.J("gke_upgrade", "nodepool_upgradeinfo_failed", map[string]any{
				"cluster": cluster, "pool": np.Name, "err": e.Error(),
			})
		} else {
			s.AutoUpgradeStatus, s.PausedReason = ui.AutoUpgradeStatus, ui.PausedReason
			s.MinorTargetVersion, s.EOSStandard = ui.MinorTargetVersion, ui.EndOfStandardSupportTimestamp
			s.EOSExtended = ui.EndOfExtendedSupportTimestamp
			s.UpgradeDetails = toUpgradeRecords(ui.UpgradeDetails, "nodepool", np.Name)
		}
		out = append(out, s)
	}
	logx.J("gke_upgrade", "nodepools_fetched", map[string]any{"cluster": cluster, "pools": len(out)})
	return out, nil
}

// applyBlueGreenSettings 把 upgradeSettings.blueGreenSettings 填进快照。
//
// 这组参数决定 BLUE_GREEN 升级要多久：
//
//	总时长 ≈ (节点数 ÷ 每批节点数) × (每批重建时长 + batchSoak) + nodePoolSoak
//
// 所以「没采到」和「配成 0」必须区分：前者预估算不准要标出来，后者是真的不等待。
// 每一种缺失都打日志——排期算出来不对时，得能直接从日志看出是哪个参数没拿到，
// 而不是回头去猜。
func applyBlueGreenSettings(s *NodePoolSnapshot, us *container.UpgradeSettings, cluster string) {
	// 策略是 SURGE 或空（GKE 默认 SURGE）时没有 blueGreenSettings，属正常，不必记。
	if !strings.EqualFold(s.Strategy, "BLUE_GREEN") {
		if s.Strategy != "" && !strings.EqualFold(s.Strategy, "SURGE") {
			// 出现文档之外的策略值：预估公式不认识它，必须让人看见
			logx.J("gke_upgrade", "unknown_upgrade_strategy", map[string]any{
				"cluster": cluster, "pool": s.Name, "strategy": s.Strategy,
				"hint": "只认 SURGE / BLUE_GREEN，耗时预估将无法计算",
			})
		}
		return
	}

	bg := us.BlueGreenSettings
	if bg == nil {
		// BLUE_GREEN 但没给参数：GKE 会用它自己的默认值，而默认值官方未承诺稳定，
		// 预估只能给区间。这是「预估不准」的头号原因，必须记。
		logx.J("gke_upgrade", "bluegreen_settings_absent", map[string]any{
			"cluster": cluster, "pool": s.Name,
			"hint": "策略为 BLUE_GREEN 但 API 未返回 blueGreenSettings，将按 GKE 默认值走，耗时预估只能给区间",
		})
		return
	}

	s.BGNodePoolSoakSec = parseGKEDurationSec(bg.NodePoolSoakDuration, cluster, s.Name, "nodePoolSoakDuration")

	switch {
	case bg.StandardRolloutPolicy != nil:
		s.BGRolloutPolicy = "STANDARD"
		p := bg.StandardRolloutPolicy
		s.BGBatchNodeCount, s.BGBatchPercentage = int(p.BatchNodeCount), p.BatchPercentage
		s.BGBatchSoakSec = parseGKEDurationSec(p.BatchSoakDuration, cluster, s.Name, "batchSoakDuration")
		if p.BatchNodeCount == 0 && p.BatchPercentage == 0 {
			// 两个都为 0 时 GKE 用默认批次大小，我们算不出批次数
			logx.J("gke_upgrade", "bluegreen_batch_size_absent", map[string]any{
				"cluster": cluster, "pool": s.Name,
				"hint": "batchNodeCount 与 batchPercentage 均为 0，批次数无法计算，耗时预估只能给区间",
			})
		}
	case bg.AutoscaledRolloutPolicy != nil:
		// autoscaled 策略由 cluster autoscaler 决定批次，没有可读的批次参数，
		// 耗时只能靠实测反推——这正是要做逐节点耗时记录的原因之一。
		s.BGRolloutPolicy = "AUTOSCALED"
		logx.J("gke_upgrade", "bluegreen_autoscaled_policy", map[string]any{
			"cluster": cluster, "pool": s.Name,
			"hint": "批次由 autoscaler 决定，无固定参数，耗时预估须依赖历史实测值",
		})
	default:
		logx.J("gke_upgrade", "bluegreen_rollout_policy_absent", map[string]any{
			"cluster": cluster, "pool": s.Name,
			"hint": "blueGreenSettings 里两种 rolloutPolicy 都没有，按 GKE 默认值走",
		})
	}
}

// parseGKEDurationSec 解析 protobuf Duration 的 JSON 形式（"3600s" / "1.5s"）为秒。
// 空字符串返回 nil（API 没给），解析失败也返回 nil 并 WARN——
// 绝不能退化成 0，0 意味着「不等待」，会让预估时长凭空少掉整个观察期。
func parseGKEDurationSec(raw, cluster, pool, field string) *int {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	v, err := strconv.ParseFloat(strings.TrimSuffix(strings.TrimSpace(raw), "s"), 64)
	if err != nil {
		logx.J("gke_upgrade", "bad_duration", map[string]any{
			"cluster": cluster, "pool": pool, "field": field, "raw": raw, "err": err.Error(),
			"hint": "无法解析为秒，按「未知」处理而非 0，耗时预估会标注缺参",
		})
		return nil
	}
	sec := int(v + 0.5)
	return &sec
}

// ListGKEOperations 列出该 project 下所有 location 的操作记录。
// 注意：ListOperationsResponse 没有分页字段，一次返回全部（保留期由 GKE 决定，官方未文档化）。
func ListGKEOperations(ctx context.Context, saJSON []byte, project string) ([]OperationRecord, error) {
	svc, err := gkeService(ctx, saJSON)
	if err != nil {
		return nil, err
	}
	resp, err := svc.Projects.Locations.Operations.List("projects/" + project + "/locations/-").Context(ctx).Do()
	if err != nil {
		return nil, fmt.Errorf("operations.list %s: %w", project, err)
	}
	if len(resp.MissingZones) > 0 {
		// 部分 zone 查不到时结果是不完整的，必须让人看见，否则「没有修复记录」会被误读成「没发生过」
		logx.J("gke_upgrade", "operations_missing_zones", map[string]any{
			"project": project, "zones": strings.Join(resp.MissingZones, ","),
		})
	}
	out := make([]OperationRecord, 0, len(resp.Operations))
	byType := map[string]int{}
	for _, op := range resp.Operations {
		r := OperationRecord{
			Name: op.Name, Type: op.OperationType, Status: op.Status,
			StartTime: op.StartTime, EndTime: op.EndTime,
			Detail: op.Detail, StatusMessage: op.StatusMessage,
			TargetLink: op.TargetLink, Location: op.Location,
			Pool: poolFromTargetLink(op.TargetLink),
		}
		if op.OperationType == "AUTO_REPAIR_NODES" {
			r.RepairReason = parseRepairReason(op.Detail, op.StatusMessage)
			if r.RepairReason == "" {
				// operationReason 在 REST v1 里不存在（只有 gcloud CLI 有），
				// 所以原因只能从文本猜。猜不出时必须 WARN，别让人以为「没有原因」。
				logx.J("gke_upgrade", "repair_reason_unparsed", map[string]any{
					"project": project, "op": op.Name,
					"detail": truncate(op.Detail, 200), "status_message": truncate(op.StatusMessage, 200),
				})
			}
		}
		byType[op.OperationType]++
		out = append(out, r)
	}
	logx.J("gke_upgrade", "operations_fetched", map[string]any{
		"project": project, "total": len(out),
		"upgrade_master": byType["UPGRADE_MASTER"], "upgrade_nodes": byType["UPGRADE_NODES"],
		"auto_repair": byType["AUTO_REPAIR_NODES"],
	})
	return out, nil
}

func toUpgradeRecords(ds []*container.UpgradeDetails, scope, pool string) []UpgradeRecord {
	out := make([]UpgradeRecord, 0, len(ds))
	for _, d := range ds {
		out = append(out, UpgradeRecord{
			Scope: scope, Pool: pool,
			StartType: d.StartType, State: d.State,
			InitialVersion: d.InitialVersion, TargetVersion: d.TargetVersion,
			StartTime: d.StartTime, EndTime: d.EndTime,
		})
	}
	return out
}

// reRepairReason 从操作文本里捞 AUTO_REPAIR_* 形式的原因码。
// ⚠️ 这是权宜之计：REST v1/v1beta1 的 Operation 结构里没有 operationReason 字段（2026-07-31 实测），
// 文档里提到的该字段是 gcloud CLI 的输出。等实调拿到真实样本后再按实际文本改进这里的规则。
var reRepairReason = regexp.MustCompile(`AUTO_REPAIR[A-Z_]*`)

func parseRepairReason(detail, statusMessage string) string {
	for _, s := range []string{detail, statusMessage} {
		if m := reRepairReason.FindString(s); m != "" {
			return m
		}
	}
	return ""
}

// poolFromTargetLink 从 targetLink 尾部解析节点池名（.../nodePools/<name>）。
func poolFromTargetLink(link string) string {
	if i := strings.LastIndex(link, "/nodePools/"); i >= 0 {
		return link[i+len("/nodePools/"):]
	}
	return ""
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// ParseGKETime 把 API 的 RFC3339 时间转成 MySQL DATETIME 用的时间；解析不出返回零值和 false。
func ParseGKETime(s string) (time.Time, bool) {
	if s == "" {
		return time.Time{}, false
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t, true
	}
	logx.J("gke_upgrade", "bad_timestamp", map[string]any{"raw": s})
	return time.Time{}, false
}
