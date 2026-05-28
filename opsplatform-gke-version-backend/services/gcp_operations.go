package services

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	containerpb "cloud.google.com/go/container/apiv1/containerpb"
)

// UpgradeOp：经过解析的 GCP 升级操作（master 或 nodepool）
type UpgradeOp struct {
	OperationID   string    // GCP operation 名（如 "operation-1716878400-abc"），全局唯一
	OperationType string    // UPGRADE_MASTER / UPGRADE_NODES
	ClusterName   string    // 从 targetLink 解析的集群名（用于过滤本集群归属）
	NodepoolName  string    // 节点池升级时填，master 升级为 ''
	Status        string    // DONE / RUNNING / ...
	StartTime     time.Time // GCP 真实操作开始时间
	EndTime       time.Time // GCP 真实操作结束时间（升级完成时刻 = 新版本开始运行时刻）
	RawDetail     string    // GCP detail 文本原文，便于排查
	TargetLink    string    // GCP operation.targetLink 原文
	// 甲：从 detail 文本正则提取的目标版本（升级到啥版本）
	DetailToVersion string
}

// 甲：从 operation.Detail 文本中正则提取目标版本号
// 常见文本格式（GCP 历史观察，可能变化）：
//
//	"Upgrade master to 1.33.10-gke.1115000"
//	"Upgrade node pool app-pool-01 to 1.33.5-gke.2172001"
//	"Auto-upgrade node pool middleware-pool-03 to 1.33.1-gke.1584000"
//	"Upgrade nodes to 1.32.x-gke.x" / "Upgrade master to 1.33.x"
//
// 只在 detail 包含 "to" 或 "to version" 后面才匹配版本，避免误抓。
var versionRegex = regexp.MustCompile(`\d+\.\d+\.\d+(?:-gke\.\d+)?`)

func parseDetailVersion(detail string) string {
	if detail == "" {
		return ""
	}
	low := strings.ToLower(detail)
	// 必须有 "to" 关键字，避免把 source 版本认作 target
	if !strings.Contains(low, " to ") && !strings.Contains(low, "to:") {
		return ""
	}
	// 取最后一次出现的版本号（GCP 文案里 target 版本一般在最后）
	matches := versionRegex.FindAllString(detail, -1)
	if len(matches) == 0 {
		return ""
	}
	return matches[len(matches)-1]
}

// 解析 targetLink 取节点池名，例：
//
//	projects/X/locations/Y/clusters/Z/nodePools/POOL → POOL
//	projects/X/zones/Y/clusters/Z/nodePools/POOL     → POOL
//
// master 升级 targetLink 没有 /nodePools/ 段，返回 ''。
func parseNodepoolFromTarget(targetLink string) string {
	idx := strings.Index(targetLink, "/nodePools/")
	if idx < 0 {
		return ""
	}
	rest := targetLink[idx+len("/nodePools/"):]
	// 截到下一个 '/' 之前（一般 nodePools/POOL_NAME 已是末尾）
	if slash := strings.Index(rest, "/"); slash >= 0 {
		return rest[:slash]
	}
	return rest
}

// parseRFC3339 把 GCP 返回的 RFC3339 时间字符串转 time.Time；解析失败返回零值
func parseRFC3339(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		// 有的 GCP 字段是 RFC3339Nano
		t2, err2 := time.Parse(time.RFC3339Nano, s)
		if err2 != nil {
			return time.Time{}
		}
		return t2
	}
	return t
}

// ListUpgradeOps：拉某 project+location 下所有 UPGRADE_MASTER / UPGRADE_NODES 操作。
//
// GCP Container API 的 ListOperations 是按 location 返回的，**不能直接按 cluster 过滤**，
// 所以上层调一次拿全部然后用 targetLink 含 clusterName 来过滤。
// 这里只过滤出 UPGRADE_* 类型，缩小返回集。
func (g *GCPClient) ListUpgradeOps(ctx context.Context, project, location string) ([]UpgradeOp, error) {
	req := &containerpb.ListOperationsRequest{
		Parent: fmt.Sprintf("projects/%s/locations/%s", project, location),
	}
	resp, err := g.cm.ListOperations(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("list operations: %w", err)
	}
	out := make([]UpgradeOp, 0, len(resp.GetOperations()))
	for _, op := range resp.GetOperations() {
		t := op.GetOperationType()
		if t != containerpb.Operation_UPGRADE_MASTER && t != containerpb.Operation_UPGRADE_NODES {
			continue
		}
		uo := UpgradeOp{
			OperationID:     op.GetName(),
			OperationType:   t.String(),
			ClusterName:     ClusterNameFromTarget(op.GetTargetLink()),
			NodepoolName:    parseNodepoolFromTarget(op.GetTargetLink()),
			Status:          op.GetStatus().String(),
			StartTime:       parseRFC3339(op.GetStartTime()),
			EndTime:         parseRFC3339(op.GetEndTime()),
			RawDetail:       op.GetDetail(),
			TargetLink:      op.GetTargetLink(),
			DetailToVersion: parseDetailVersion(op.GetDetail()),
		}
		out = append(out, uo)
	}
	return out, nil
}

// ClusterNameFromTarget 从 operation.targetLink 提取 cluster 名，
// 用于在多集群共享 location 时判断 op 归属。
func ClusterNameFromTarget(targetLink string) string {
	const key = "/clusters/"
	idx := strings.Index(targetLink, key)
	if idx < 0 {
		return ""
	}
	rest := targetLink[idx+len(key):]
	for i, c := range rest {
		if c == '/' {
			return rest[:i]
		}
	}
	return rest
}
