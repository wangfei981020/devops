package handlers

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strings"

	"ops-cmdb-backend/crypto"
	"ops-cmdb-backend/internal/store"
	"ops-cmdb-backend/logx"
)

// 接入自检（task_key = integration_check）。
//
// # 为什么需要一个**主动**的检查
//
// 「集群的指标标签值」与「观测端点」是一对：那个值必须真的存在于端点的
// cluster 标签取值里，否则**所有带集群条件的查询都静默返回空** ——
// 而空结果看起来和"这个集群确实没有东西"一模一样。
//
// 🔴 判定逻辑（verifyClusterValue）其实一直都有，问题是它**只在有人去查那个集群时才跑**。
//
//	2026-08-19 生产上 g32-prod 配的是集群名 `g32-prod-cluster`，
//	而 VM 里的真实取值是 `prod-k8s-cluster-01` —— 磁盘水位 / 用量 / OOM 全线失效。
//	它挂了多久没人知道，因为**没人会逐个点开每个集群去看那个字段**（OPSCMDB-044）。
//
// ⚠️ 所以这里做的不是新判据，而是把已有判据从「按需触发」改成「定时全量跑一遍 + 落库」，
//
//	让总览页能主动报出来。缺的从来不是判断力，是**有没有人去问**。
//
// # 为什么这条检查不属于任何单个页面
//
// 集群页看自己：标签值填了，看着是对的。
// 观测端点页看自己：连得通，看着也是对的。
// **只有把两边放在一起才看得出不匹配** —— 这类问题必须有个地方专门负责跨对象核对。

// integrationIssue 一条自检结论。
type integrationIssue struct {
	Kind      string // cluster_label / no_endpoint
	ClusterID int
	Cluster   string
	Detail    string
}

// integrationCheckCore 逐租户核对「集群标签值 ↔ 观测端点」。
//
// 返回 summary（写进任务历史）、失败明细、是否整体成功。
func integrationCheckCore(ctx context.Context, st *store.Store, db *sql.DB, cipher *crypto.Cipher) (string, []TaskFailure, bool) {
	type cl struct {
		tenant store.TenantID
		id     int
		name   string
		env    string
	}
	var clusters []cl
	tenantCount := store.CountActiveTenants(ctx, st, "integration_check")
	failedTenants, err := store.ForEachTenant(ctx, st, "integration_check", func(sc *store.Scoped, t store.TenantID) error {
		// ⚠️ 必须逐租户走 Scoped，不能用 Platform()：k8s_clusters 是租户表。
		//	disk_watch 曾因为这个写法从多租户改造后一次都没跑成功过，
		//	而界面上它一直显示「正常」。
		rows, e := sc.Query(`SELECT id, name, COALESCE(environment,'') FROM k8s_clusters
			WHERE tenant_id = ? AND enabled = 1 ORDER BY id`)
		if e != nil {
			return e
		}
		defer rows.Close()
		for rows.Next() {
			var c cl
			if rows.Scan(&c.id, &c.name, &c.env) == nil {
				c.tenant = t
				clusters = append(clusters, c)
			}
		}
		return rows.Err()
	})
	if err != nil {
		return "", []TaskFailure{{Target: "tenants", Reason: "遍历租户失败：" + err.Error()}}, false
	}

	var issues []integrationIssue
	var failures []TaskFailure
	checked := 0
	for _, c := range clusters {
		base, token, clusterLabel, e := resolveEndpointFull(db, cipher, "prometheus", c.env, c.id)
		if e != nil || base == "" {
			// 没绑到指标源本身也是一种接入缺口 —— 但它和"配错了"要分开报：
			// 前者是"还没接"，后者是"接了但不通"，处置动作不同。
			issues = append(issues, integrationIssue{
				Kind: "no_endpoint", ClusterID: c.id, Cluster: c.name,
				Detail: "没有匹配的指标数据源，所有指标类判定（磁盘/用量/OOM）对这个集群都不可用",
			})
			continue
		}
		if clusterLabel == "" {
			// 单集群源不需要隔离，这是正常配置，不算问题
			checked++
			continue
		}
		label, value := clusterSelectorParts(db, clusterLabel, c.id)
		if label == "" {
			continue
		}
		// 🔴 三态：核对通过 / 核对出问题 / **没核对成**。
		//	`verifyClusterValue` 在自检本身失败时返回 nil，对按需调用是对的
		//	（别拿不确定的结论覆盖真实的空结果），但巡检不能把它当"通过" ——
		//	本地实测：数据源 DNS 解析不了，四个集群全被报成"0 个有问题"，
		//	而实际上一个都没核对成。把"没检查成"渲染成"检查通过"，
		//	正是这个产品要防的那种失效。
		bad, verified := verifyClusterValueEx(base, token, label, value)
		if !verified {
			issues = append(issues, integrationIssue{
				Kind: "unverified", ClusterID: c.id, Cluster: c.name,
				Detail: "没能核对：读不到数据源的标签取值（源不可达 / 令牌无效 / 该标签一个取值都没有）。" +
					"这不代表配置是对的，只代表**没查成**",
			})
			continue
		}
		checked++
		if bad == nil {
			continue
		}
		avail, _ := bad["available_values"].([]string)
		sort.Strings(avail)
		if len(avail) > 8 {
			avail = append(avail[:8:8], "…")
		}
		issues = append(issues, integrationIssue{
			Kind: "cluster_label", ClusterID: c.id, Cluster: c.name,
			Detail: fmt.Sprintf("标签值 %s=%q 在数据源里不存在，所有带集群条件的查询都会返回空。可选值：%s",
				label, value, strings.Join(avail, ", ")),
		})
	}

	// 落库：总览页从这里读，不再重跑一遍探测（那会让打开首页去打一圈外部数据源）
	if e := saveIntegrationIssues(ctx, st, issues); e != nil {
		failures = append(failures, TaskFailure{Target: "integration_issues", Reason: "写入结果失败：" + e.Error()})
	}

	logx.J("integration_check", "done", map[string]any{
		"tenants": tenantCount, "clusters": len(clusters), "checked": checked, "issues": len(issues),
	})
	ok := len(failures) == 0 && failedTenants == 0
	// ⚠️ summary 必须把「核对成的」和「总数」分开说。
	//	只说"核对 N 个集群、M 个有问题"时，N 里混着没核对成的那些，
	//	读的人会以为全查过了。
	unverified := 0
	for _, is := range issues {
		if is.Kind == "unverified" {
			unverified++
		}
	}
	summary := fmt.Sprintf("%d 个集群：核对成 %d 个，其中 %d 个有问题",
		len(clusters), checked, len(issues)-unverified)
	if unverified > 0 {
		summary += fmt.Sprintf("；%d 个没核对成（源不可达等）", unverified)
	}
	if failedTenants > 0 {
		summary += fmt.Sprintf("；%d 个租户没跑成功", failedTenants)
	}
	return summary, failures, ok
}

// saveIntegrationIssues 全量覆盖写。
//
// ⚠️ 用「先删后插」而不是逐条 upsert：修好的问题必须**消失**。
//
//	只 upsert 的话，一条已经改对的记录会永远留在表里，
//	界面上就成了"改完也不消失"——那正是这类巡检最容易失去信任的方式。
func saveIntegrationIssues(ctx context.Context, st *store.Store, issues []integrationIssue) error {
	_, err := store.ForEachTenant(ctx, st, "integration_check", func(sc *store.Scoped, t store.TenantID) error {
		if _, e := sc.Exec(`DELETE FROM integration_issues WHERE tenant_id = ?`); e != nil {
			return e
		}
		for _, is := range issues {
			// ⚠️ 写租户表用 sc.Insert（它注入 tenant_id）；sc.Exec 是给带 WHERE 的语句用的，
			//	对 INSERT 会被租户守卫拦下：`语句缺少 tenant_id 过滤条件`。
			//	这个守卫救过我一次 —— 否则这条数据会写成 tenant_id=0 的孤儿行。
			if _, e := sc.Insert(`INSERT INTO integration_issues
				(tenant_id, kind, cluster_id, cluster_name, detail) VALUES (?,?,?,?,?)`,
				is.Kind, is.ClusterID, is.Cluster, is.Detail); e != nil {
				return e
			}
		}
		return nil
	})
	return err
}
