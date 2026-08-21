package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"ops-cmdb-backend/diag"
	"ops-cmdb-backend/logx"
)

// 诊断上下文补全：把 CMDB 自己已经有的数据接进 Pod 诊断。
//
// # 为什么这些必须接进来
//
// 每一条都是「CMDB 明明有、但诊断没用上」，于是本来免费能判出来的根因，
// 要么退化成泛化结论，要么被推给 AI 去花钱。
//
//	节点状态      → 一个磁盘满的节点能拖垮一批 Pod，而逐个查永远查不到节点头上
//	配置审计      → config_audit 早算出「缺 Secret ls-nacos 的 TIDB_HOST」，
//	                而诊断只会说"配置引用有问题"，两个工具各知道一半
//	镜像变更      → 变更关联只看 k8s 的 restartedAt 注解，而发版是换 tag，不写注解
//	内存实测      → 没有它，"调高 limit" 调到多少全靠猜
//
// 🔴 成本分层里的位置：这一整层都是**免费**的（查自己的库 / 已有的观测源）。
// 按定下的原则，能在这里解决的绝不该花钱问模型。
//
// ⚠️ 每一条都必须**失败不影响主流程**：补全拿不到只是少一份证据，
// 不能让整个诊断挂掉。但失败要留痕 —— 否则"少了一份证据"和
// "本来就没有"在结果里长得一模一样。

// enrichDiagnosis 在跑规则之前把上下文补全。
func (h *K8sDiagHandler) enrichDiagnosis(ctx context.Context, cid int, dc *diag.DiagnosisContext) {
	h.fillNodePressure(ctx, cid, dc)
	h.fillConfigIssues(ctx, cid, dc)
	h.fillImageChange(ctx, cid, dc)
	h.fillMemUsage(ctx, cid, dc)
}

// fillNodePressure 这个 Pod 落在哪个节点、那个节点有没有压力。
func (h *K8sDiagHandler) fillNodePressure(ctx context.Context, cid int, dc *diag.DiagnosisContext) {
	var node, conds string
	err := h.DB.QueryRowContext(ctx, `SELECT p.node_name, COALESCE(n.conditions,'')
		FROM k8s_pods p
		LEFT JOIN k8s_nodes n ON n.cluster_id = p.cluster_id AND n.name = p.node_name
		WHERE p.cluster_id=? AND p.namespace=? AND p.name=?`,
		cid, dc.Namespace, dc.PodName).Scan(&node, &conds)
	if err != nil {
		return // Pod 可能刚建还没采到，不是错误
	}
	dc.NodeName = node
	dc.NodePressure = strings.TrimSpace(conds)
}

// fillConfigIssues 这个 Pod 引用了但不存在的 ConfigMap/Secret。
//
// 🔴 **复用 config_audit 的判定链**（loadRefs + judgeRef），绝不在这里另写一套。
//
//	两套判据早晚会分叉，而分叉的表现是"诊断说缺、配置审计页说不缺"——
//	那种矛盾最消耗信任，而且很难查（两边看起来都对）。
//
// ⚠️ 只取这一个 Pod 的引用，且跳过 optional 的（缺了也不影响启动，
// 报出来只会把真问题淹掉）——这两条也和 config_audit 保持一致。
func (h *K8sDiagHandler) fillConfigIssues(ctx context.Context, cid int, dc *diag.DiagnosisContext) {
	refs, err := h.loadRefs(cid, dc.Namespace)
	if err != nil {
		logx.J("k8s", "config_refs_query_failed", map[string]any{
			"cluster_id": cid, "ns": dc.Namespace, "pod": dc.PodName, "err": err.Error(),
		})
		return
	}
	cmIndex, err := h.loadConfigMapIndex(cid, dc.Namespace)
	if err != nil {
		return
	}
	secIdx := h.loadSecretIndex(cid, dc.Namespace)

	// 事件佐证：KSM 没覆盖到的命名空间要靠它，与 config_audit 同一口径
	var evidence map[string]bool
	evidenceOK := false
	if anyBad(refs) {
		evidence, evidenceOK = h.notFoundEvidence(cid, dc.Namespace)
	}

	for _, r := range refs {
		if r.optional {
			continue
		}
		// 只要这个 Pod 的
		mine := false
		for _, p := range r.pods {
			if p == dc.PodName {
				mine = true
				break
			}
		}
		if !mine {
			continue
		}
		f := judgeRef(r, cmIndex, secIdx, evidence, evidenceOK)
		if f == nil || f.Status != "missing" {
			continue
		}
		item := fmt.Sprintf("%s %s", f.RefKind, f.RefName)
		if f.RefKey != "" {
			item += " 的键 " + f.RefKey
		}
		if f.Source != "" {
			item += "（来源：" + f.Source + "）"
		}
		dc.ConfigIssues = append(dc.ConfigIssues, item+" 不存在")
		if len(dc.ConfigIssues) >= 8 {
			break // 够定性了，再多只会把结论淹掉
		}
	}
	_ = ctx
}

// fillImageChange 这个工作负载最近一次镜像变更。
//
// 🔴 只取**近期**的：三个月前换过镜像和现在崩了没有关系，
// 硬关联上去会把人引向错误的回滚决定。
const imageChangeWindow = 7 * 24 * time.Hour

func (h *K8sDiagHandler) fillImageChange(ctx context.Context, cid int, dc *diag.DiagnosisContext) {
	if dc.OwnerName == "" {
		return
	}
	// ReplicaSet 名是 <deploy>-<hash>，变更表里记的是工作负载名
	wl := dc.OwnerName
	if dc.OwnerKind == "ReplicaSet" {
		if i := strings.LastIndex(wl, "-"); i > 0 {
			wl = wl[:i]
		}
	}
	var oldV, newV string
	var changedAt time.Time
	err := h.DB.QueryRowContext(ctx, `SELECT old_value, new_value, changed_at
		FROM k8s_changes
		WHERE cluster_id=? AND namespace=? AND name=? AND field='image'
		  AND changed_at > ?
		ORDER BY changed_at DESC LIMIT 1`,
		cid, dc.Namespace, wl, time.Now().Add(-imageChangeWindow)).Scan(&oldV, &newV, &changedAt)
	if err != nil {
		return
	}
	dc.ImageChange = fmt.Sprintf("🔴 %s 换过镜像（%s）：%s → %s —— 先确认是不是这次发版引入的，是则回滚止血",
		changedAt.Format("2006-01-02 15:04"), wl, shortImage(oldV), shortImage(newV))
}

// shortImage 只留仓库最后一段 + tag，完整地址太长会把结论淹掉。
func shortImage(s string) string {
	if i := strings.LastIndex(s, "/"); i >= 0 && i+1 < len(s) {
		return s[i+1:]
	}
	return s
}

// fillMemUsage OOM 类问题的实测内存用量。
//
// ⚠️ 只在**确实是 OOM 形态**时才查：这一步要打 Prometheus，
// 对每个 Pod 都查会把诊断拖慢，而绝大多数问题跟内存无关。
func (h *K8sDiagHandler) fillMemUsage(ctx context.Context, cid int, dc *diag.DiagnosisContext) {
	oom := false
	for _, cc := range dc.Containers {
		if strings.EqualFold(cc.LastReason, "OOMKilled") || strings.EqualFold(cc.StateReason, "OOMKilled") {
			oom = true
			break
		}
	}
	if !oom {
		return
	}
	q := fmt.Sprintf(`max_over_time(container_memory_working_set_bytes{namespace=%q,pod=%q,container!=""}[6h])`,
		dc.Namespace, dc.PodName)
	v, err := h.promScalar(ctx, cid, q)
	if err != nil || v <= 0 {
		// 🔴 查不到必须说出来，不能静默省略 —— 否则"没查到用量"会被读成"用量正常"。
		//
		// ⚠️ 但也别把原因说死成"没接指标源"：真实架构是一套 VM 集中采多个集群，
		//	指标常常是有的，只是这个集群没绑上去或 cluster 标签值不对。
		//	说成"没接"会让人去装一个本不该装的 Prometheus。
		dc.MemUsage = "（取不到实测用量。可能是：这个集群没绑指标数据源 / 绑了但「指标集群标签值」不对 / " +
			"该 Pod 已经不在了。⚠️ 若你们是一套 VictoriaMetrics 集中采多个集群，" +
			"多半是后两种——先去「集群健康」看那条「节点磁盘水位未检查」的说明，它会指出具体是哪一种。" +
			"没有实测值时别凭感觉调 limit）"
		return
	}
	dc.MemUsage = fmt.Sprintf("近 6 小时峰值 %.0f MiB（container_memory_working_set_bytes）", v/(1<<20))
}

// promScalar 跑一条 instant 查询并取第一个标量值。
//
// ⚠️ 和 lokiTail 一样：没接指标源时返回错误而不是 0 ——
// 返回 0 会被读成"用量为零"，那比没有数据更糟。
func (h *K8sDiagHandler) promScalar(ctx context.Context, cid int, promQL string) (float64, error) {
	var env string
	_ = h.DB.QueryRowContext(ctx, `SELECT COALESCE(environment,'') FROM k8s_clusters WHERE id=?`, cid).Scan(&env)
	base, token, err := resolveEndpoint(h.DB, h.Cipher, "prometheus", env, cid)
	if err != nil {
		return 0, err
	}
	u := strings.TrimRight(base, "/") + "/api/v1/query?query=" + urlQueryEscape(promQL)
	code, body, err := obsGet(u, token, 8*time.Second)
	if err != nil {
		return 0, err
	}
	if code != 200 {
		return 0, fmt.Errorf("指标源返回 HTTP %d", code)
	}
	return firstPromValue(body)
}

// urlQueryEscape 单独封一层，免得在这个文件里再 import net/url。
func urlQueryEscape(s string) string { return url.QueryEscape(s) }

// firstPromValue 从 Prometheus 的 instant 响应里取第一个样本值。
//
// ⚠️ 空 result 不是错误也不是 0 —— 它是"这个查询没匹配到任何序列"，
// 必须区分开：返回 0 会让调用方以为"用量是 0"。
func firstPromValue(body string) (float64, error) {
	var r struct {
		Status string `json:"status"`
		Data   struct {
			Result []struct {
				Value []any `json:"value"`
			} `json:"result"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(body), &r); err != nil {
		return 0, err
	}
	if r.Status != "success" {
		return 0, fmt.Errorf("指标查询未成功：%s", r.Status)
	}
	if len(r.Data.Result) == 0 {
		return 0, errors.New("查询没有匹配到任何序列")
	}
	v := r.Data.Result[0].Value
	if len(v) < 2 {
		return 0, errors.New("返回的样本格式不对")
	}
	str, ok := v[1].(string)
	if !ok {
		return 0, errors.New("样本值不是字符串")
	}
	return strconv.ParseFloat(str, 64)
}
