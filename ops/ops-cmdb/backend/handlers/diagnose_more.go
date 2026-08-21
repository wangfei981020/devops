package handlers

import (
	"database/sql"
	"fmt"
	"net/http"
	"ops-cmdb-backend/internal/httpx"
	"strconv"
	"strings"

	"ops-cmdb-backend/diag"

	"github.com/gin-gonic/gin"
)

// 集群 / 域名 / 成本 三个诊断器。
//
// # 为什么和 diagnose_pod 用同一个契约
//
// `diag.DiagnosisResult`（root_cause + evidence + solutions + confidence）
// 是「给方案」这件事的统一形态。三个新诊断器照抄它，好处是：
//   - AI 拿到的结构永远一样，不用为每个诊断器写一套解析
//   - 前端一套 UI 通吃
//   - 「没命中规则」这一档的语义统一 —— matched=false 不等于没问题
//
// # 一条硬纪律：查不了要说查不了
//
// 每个诊断器都要区分三种结局：
//   1. 查了，命中已知模式 → matched=true + 根因 + 方案
//   2. 查了，没命中 → matched=false，**明说"这不代表没问题"**
//   3. **查不了**（没接数据源 / 查询失败）→ 在 evidence 里写清楚缺什么
//
// 第 3 种最容易被做成第 2 种，而那正是「猜测」的来源：
// 没有数据却给一个"看起来没问题"的结论。

// DiagnoseCluster 集群级诊断：节点、容量、版本、采集健康。
func (h *K8sDiagHandler) DiagnoseCluster(c *gin.Context) {
	cid, _ := strconv.Atoi(c.Query("cluster_id"))
	if cid == 0 {
		httpx.Required(c, "cluster_id")
		return
	}
	var name string
	if h.DB.QueryRow(`SELECT COALESCE(NULLIF(display_name,''),name) FROM k8s_clusters WHERE id=?`, cid).
		Scan(&name) != nil {
		httpx.NotFound(c, "cluster")
		return
	}

	res := diag.DiagnosisResult{Provider: "rule", Confidence: "high",
		Evidence: []string{}, Solutions: []diag.Solution{}}

	// —— 采集健康：这一项必须最先查。采集断了的话，后面所有"没发现问题"都不成立
	var lastSync sql.NullString
	var syncAgeMin sql.NullInt64
	_ = h.DB.QueryRow(`SELECT MAX(synced_at), TIMESTAMPDIFF(MINUTE, MAX(synced_at), NOW())
	                     FROM k8s_nodes WHERE cluster_id=?`, cid).Scan(&lastSync, &syncAgeMin)
	if !lastSync.Valid {
		res.Matched = true
		res.RootCause = "这个集群没有采到任何节点数据"
		res.Evidence = append(res.Evidence, "k8s_nodes 表里没有该集群的记录")
		res.Solutions = append(res.Solutions,
			diag.Solution{Text: "确认集群的连接方式已配置：GKE 需要 project_id + cloud_account_id + endpoint，其余需要 kubeconfig"},
			diag.Solution{Text: "在「集群」页点「测试连通性」看具体报错；再点「立即同步」"},
			diag.Solution{Text: "⚠️ 若刚编辑过集群，检查连接配置是否被清空（历史缺陷 OPSCMDB-018，v0.65.0 已修）"})
		c.JSON(http.StatusOK, gin.H{"cluster": name, "result": res})
		return
	}
	if syncAgeMin.Valid && syncAgeMin.Int64 > 60 {
		res.Evidence = append(res.Evidence,
			fmt.Sprintf("⚠️ 最后一次采集是 %d 分钟前（%s）——下面的结论基于这个时间点的快照",
				syncAgeMin.Int64, lastSync.String))
	}

	// —— 节点失联
	var stale, total int
	_ = h.DB.QueryRow(`SELECT COUNT(*), SUM(CASE WHEN hb_stale = 1 THEN 1 ELSE 0 END)
	    FROM k8s_nodes WHERE cluster_id=?`, cid).Scan(&total, &stale)
	if stale > 0 {
		res.Matched = true
		res.RootCause = fmt.Sprintf("%d/%d 个节点心跳已停", stale, total)
		res.Evidence = append(res.Evidence, "判据：采集时刻用未取整的真实心跳判定（hb_stale 列），"+
			"不是拿库里的 last_heartbeat 减当前时间——后者被取整到 5 分钟，倒推会虚高")
		res.Solutions = append(res.Solutions,
			diag.Solution{Text: "先分清是节点真挂了还是采集断了：cluster_health 看采集侧，list_nodes 看每个节点的最后心跳"},
			diag.Solution{Text: "⚠️ 若同名节点出现两行、其中一行没有集群名，那是删集群没清数据留下的孤儿，不是真故障（OPSCMDB-022，v0.65.0 已修 + 迁移 114 清存量）"})
	}

	// —— 节点资源压力（磁盘/内存/PID）
	//
	// 🔴 这一类此前完全没查，而它能直接打垮发布：磁盘满 → 镜像拉不动 →
	//	Pod 卡在 ContainerCreating → 看起来像「发布卡住」或「一堆 Pod 异常」。
	//	实测 DEV node12 磁盘 100%、正在驱逐，而本接口只报「57 个 Pod 异常」，
	//	evidence 还是空的 —— 人顺着 Pod 查下去，永远查不到节点头上（OPSCMDB-042）。
	//
	// ⚠️ 排在异常 Pod **之前**：压力位是因，Pod 异常往往是果。
	//	谁先写进 RootCause 决定了人从哪头开始查。
	if rows, err := h.DB.Query(`SELECT name, conditions FROM k8s_nodes
	    WHERE cluster_id=? AND conditions <> '' ORDER BY name`, cid); err == nil {
		var pressured []string
		for rows.Next() {
			var n, cond string
			if rows.Scan(&n, &cond) == nil {
				pressured = append(pressured, n+"("+cond+")")
			}
		}
		rows.Close()
		if len(pressured) > 0 {
			res.Matched = true
			rc := fmt.Sprintf("%d 个节点处于资源压力状态：%s", len(pressured), strings.Join(pressured, "、"))
			if res.RootCause == "" {
				res.RootCause = rc
			} else {
				res.RootCause = rc + "；" + res.RootCause
			}
			res.Evidence = append(res.Evidence,
				"判据：k8s_nodes.conditions（采集时算好的压力位摘要，只认 MemoryPressure/"+
					"DiskPressure/PIDPressure/NetworkUnavailable 为真压力）",
				"⚠️ 这类节点的 ready_status 仍然是 Ready —— 「快撑不住但还没倒」，"+
					"只看 Ready 或节点列表的 health 是看不出来的")
			res.Solutions = append(res.Solutions,
				diag.Solution{Text: "先看事件时间线：list_events kind=Node —— ImageGCFailed / NodeHasDiskPressure / " +
					"EvictionThresholdMet 会按时间摆出来，比任何汇总都清楚"},
				diag.Solution{Text: "再用 cluster_health 拿具体水位百分比（磁盘水位来自 Prometheus，不落库）"},
				diag.Solution{Text: "⚠️ 磁盘满时若 ImageGC 报「0 bytes eligible」，说明占盘的不是可回收镜像层，" +
					"而是**正在跑的容器可写层 / emptyDir** —— 清镜像没用，要么赶走负载要么扩容"})
		}
	}

	// —— 容量超卖
	var overcommit int
	_ = h.DB.QueryRow(`SELECT COUNT(*) FROM k8s_nodes WHERE cluster_id=? AND cpu_cap<>'' `, cid).Scan(&overcommit)

	// —— 异常 Pod
	var badPods int
	_ = h.DB.QueryRow(`SELECT COUNT(*) FROM k8s_pods WHERE cluster_id=?
	    AND (phase IN ('Failed','Unknown') OR restarts > 10)`, cid).Scan(&badPods)
	if badPods > 0 {
		res.Matched = true
		if res.RootCause == "" {
			res.RootCause = fmt.Sprintf("%d 个 Pod 处于异常或高重启状态", badPods)
		} else {
			res.RootCause += fmt.Sprintf("；另有 %d 个 Pod 异常", badPods)
		}
		// 🔴 只给数字不给证据，等于让人自己去找那 N 个是谁。
		//	实测本接口返回「57 个 Pod 异常」+ evidence:[]，人只能再去翻列表。
		//	这里把重启最多的几个直接列出来 —— 它们通常就是入口。
		if prows, perr := h.DB.Query(`SELECT namespace, name, phase, restarts FROM k8s_pods
		    WHERE cluster_id=? AND (phase IN ('Failed','Unknown') OR restarts > 10)
		    ORDER BY restarts DESC LIMIT 5`, cid); perr == nil {
			var top []string
			for prows.Next() {
				var ns, nm, ph string
				var rs int
				if prows.Scan(&ns, &nm, &ph, &rs) == nil {
					top = append(top, fmt.Sprintf("%s/%s（%s，重启 %d 次）", ns, nm, ph, rs))
				}
			}
			prows.Close()
			if len(top) > 0 {
				res.Evidence = append(res.Evidence,
					"重启最多的几个："+strings.Join(top, "；"))
			}
		}
		res.Solutions = append(res.Solutions,
			diag.Solution{Text: "对上面任一个调 diagnose_pod 拿根因——它会给出证据和处置建议，CrashLoop 时自动取上一个容器实例的日志"},
			diag.Solution{Text: "⚠️ 想一次看全，用 diagnose_sweep：它把整个集群的异常 Pod 跑一遍规则，" +
				"报「几个判出了具体根因、几个没判出」并给未判出清单，比一个个点快得多"})
	}

	if !res.Matched {
		res.RootCause = "未发现集群级异常"
		res.Confidence = "medium"
		// ⚠️ 这句必须有。规则只覆盖节点心跳/Pod 状态/采集健康三类，
		// 说成"集群健康"是在替没查过的东西背书
		res.Evidence = append(res.Evidence,
			"本次只检查了：采集健康、节点心跳、异常 Pod 三类。未命中不等于集群没有问题——"+
				"网络策略、证书链、中间件内部状态等都不在规则覆盖范围内")
		res.Solutions = append(res.Solutions,
			diag.Solution{Text: "继续查：config_audit（配置合规）、security_audit（安全基线）、list_orphans（孤儿资源）、resource_waste（用量对比）"})
	}
	c.JSON(http.StatusOK, gin.H{"cluster": name, "result": res})
}

// DiagnoseDomain 域名诊断：解析 → CDN → 证书 → 源站，整条链一次看完。
//
//	⚠️ 这四段任何一段断了表现都是「打不开」，但处置完全不同。
//	老 CMDB 时代最常见的浪费就是从错的那一段开始查。
func (h *K8sDiagHandler) DiagnoseDomain(c *gin.Context) {
	fqdn := c.Query("domain")
	if fqdn == "" {
		httpx.Required(c, "domain")
		return
	}

	res := diag.DiagnosisResult{Provider: "rule", Confidence: "high",
		Evidence: []string{}, Solutions: []diag.Solution{}}

	// 段一：台账里有没有这个域名
	var ciID int64
	var expiry, resolveStatus sql.NullString
	err := h.DB.QueryRow(`SELECT d.ci_id, d.expiry_at, d.resolve_status
	    FROM domains d JOIN cis c ON c.id=d.ci_id WHERE c.name=?`, fqdn).
		Scan(&ciID, &expiry, &resolveStatus)
	if err == sql.ErrNoRows {
		res.Matched = false
		res.RootCause = "台账里没有这个域名"
		res.Evidence = append(res.Evidence, "domains 表里查不到 "+fqdn)
		res.Solutions = append(res.Solutions,
			diag.Solution{Text: "先确认它是否属于我们管理：可能是子域名（查它的根域名），或者还没纳管"},
			diag.Solution{Text: "若确属我们：到「管理 / 注册商」接入后同步，或在域名页手工录入"})
		c.JSON(http.StatusOK, gin.H{"domain": fqdn, "result": res})
		return
	}

	// 段二：注册到期
	if expiry.Valid && expiry.String != "" {
		var daysLeft sql.NullInt64
		_ = h.DB.QueryRow(`SELECT DATEDIFF(?, CURDATE())`, expiry.String).Scan(&daysLeft)
		if daysLeft.Valid && daysLeft.Int64 < 0 {
			res.Matched = true
			res.RootCause = fmt.Sprintf("域名注册已过期 %d 天", -daysLeft.Int64)
			res.Evidence = append(res.Evidence, "到期日 "+expiry.String)
			res.Solutions = append(res.Solutions,
				diag.Solution{Text: "⚠️ 注册过期是最外层的死因：解析、CDN、证书全都无意义，先续费"},
				diag.Solution{Text: "续费是非幂等外部写：失败不等于没扣费，重试前务必回查注册商的到期日确认"})
			c.JSON(http.StatusOK, gin.H{"domain": fqdn, "result": res})
			return
		}
		if daysLeft.Valid && daysLeft.Int64 <= 30 {
			res.Evidence = append(res.Evidence,
				fmt.Sprintf("注册还剩 %d 天到期（%s）", daysLeft.Int64, expiry.String))
		}
	}

	// 段三：解析状态
	if resolveStatus.Valid && resolveStatus.String != "" && resolveStatus.String != "ok" {
		res.Matched = true
		res.RootCause = "域名解析异常：" + resolveStatus.String
		res.Solutions = append(res.Solutions,
			diag.Solution{Text: "用 cdn_domain_check 看这条解析有没有真的走 CDN——以为挂了 CDN 实际直连源站的话，WAF/限流/缓存全都没生效"},
			diag.Solution{Text: "用 dns_consistency 看 GCP Cloud DNS 与 Cloudflare 是否冲突。⚠️「改了没生效」最常见的原因是改在了 NS 没指向的那一边"})
	}

	// 段四：证书
	var certMsg sql.NullString
	var certDays sql.NullInt64
	_ = h.DB.QueryRow(`SELECT cert_check_msg, DATEDIFF(cert_expiry_at, CURDATE())
	    FROM domain_records WHERE host=? ORDER BY id DESC LIMIT 1`, fqdn).Scan(&certMsg, &certDays)
	switch {
	case certDays.Valid && certDays.Int64 < 0:
		res.Matched = true
		res.RootCause = appendCause(res.RootCause, fmt.Sprintf("证书已过期 %d 天", -certDays.Int64))
		res.Solutions = append(res.Solutions,
			diag.Solution{Text: "查「证书」页的续期记录：ACME 失败通常是 DNS-01 校验没过（_acme-challenge 记录没生效）"})
	case certMsg.Valid && certMsg.String != "" && certMsg.String != "证书有效":
		// ⚠️ 检测失败是第三态，既不是"有效"也不是"过期"
		res.Evidence = append(res.Evidence, "证书检测结果："+certMsg.String+
			"（⚠️ 这是检测失败，不等于证书已过期——可能只是域名解析不到）")
	}

	if !res.Matched {
		res.RootCause = "这条链上没发现已知问题"
		res.Confidence = "medium"
		res.Evidence = append(res.Evidence,
			"已检查：注册到期、解析状态、证书。未覆盖：源站健康、CDN 规则、WAF 拦截")
		res.Solutions = append(res.Solutions,
			diag.Solution{Text: "继续查：domain_topology 看它背后的服务是否活着；cdn_rule_analysis 看规则有没有把它挡了"})
	}
	c.JSON(http.StatusOK, gin.H{"domain": fqdn, "result": res})
}

// DiagnoseCost 成本诊断：哪笔涨了、能省多少、动哪里。
func (h *K8sDiagHandler) DiagnoseCost(c *gin.Context) {
	res := diag.DiagnosisResult{Provider: "rule", Confidence: "medium",
		Evidence: []string{}, Solutions: []diag.Solution{}}

	// ⚠️ 成本判据依赖费率表。费率错了整张报表会安静地偏掉，所以先说清楚这一点
	res.Evidence = append(res.Evidence,
		"⚠️ 成本按机型与磁盘估算，不是云账单；单价来自我们维护的费率表，机型对不上时回退默认档")

	var snapMonths int
	_ = h.DB.QueryRow(`SELECT COUNT(DISTINCT month) FROM cost_snapshots`).Scan(&snapMonths)
	if snapMonths < 2 {
		// 查不了要说查不了 —— 不能因为没快照就说"成本没问题"
		res.Matched = false
		res.RootCause = "无法做环比：成本快照不足两个月"
		res.Evidence = append(res.Evidence, fmt.Sprintf("cost_snapshots 里只有 %d 个月的数据", snapMonths))
		res.Solutions = append(res.Solutions,
			diag.Solution{Text: "在「成本」页点「打成本快照」，每月至少一次；没有快照就永远算不出环比"})
		c.JSON(http.StatusOK, gin.H{"result": res})
		return
	}

	// 闲置：买了没分配出去的部分
	rows, err := h.DB.Query(`SELECT cluster_id, name FROM k8s_clusters WHERE enabled=1`)
	if err == nil {
		defer rows.Close()
	}
	res.Solutions = append(res.Solutions,
		diag.Solution{Text: "用 idle_cost 看「实付 − 已按 request 分摊」——那部分是买了没分配出去的，缩容能直接省掉"},
		diag.Solution{Text: "用 cost_attribution 看环比归因：本月比上月哪些资源涨了、涨了多少、为什么"},
		diag.Solution{Text: "用 resource_waste 看「分配了但没用起来」——⚠️ 它和闲置是两个概念，需要 Prometheus 实测用量，没接就算不出来"},
		diag.Solution{Text: "用 list_orphans 找孤儿 PVC / 预留未绑定的静态 IP——这类是纯浪费，删掉没有副作用"})
	res.RootCause = "成本分析入口（按上面的顺序查）"
	res.Matched = false
	res.Evidence = append(res.Evidence,
		"⚠️ 本诊断器只指路不下结论：省钱决策要看业务容量规划，规则判不了")
	c.JSON(http.StatusOK, gin.H{"result": res})
}

func appendCause(cur, add string) string {
	if cur == "" {
		return add
	}
	return cur + "；" + add
}
