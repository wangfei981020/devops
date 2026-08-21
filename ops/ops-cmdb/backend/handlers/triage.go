package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// Triage 分诊入口：一次给出「当前所有值得关注的事 + 各自该往哪查」。
//
// # 为什么单独做一个
//
// 「发现问题」的数据本来就有（/api/overview 的 attention），但它是给**人**看的：
// 每条给一个界面链接。AI 拿到链接没用 —— 它要知道的是**下一步调哪个工具**。
//
// 没有这个入口时，AI 只能挨个试 20 多个 list_* 工具去撞问题，
// 结果是既慢又容易漏：漏掉的那一类它自己也不知道漏了。
//
// # 它不产生新数据
//
// 严格复用 overview 的 attention 判定，只做两件事：
//   - 按严重度排序
//   - 给每条挂上「下一步查什么」（工具名 + 为什么查它）
//
// ⚠️ 所以它不会和界面上的数字打架 —— 同一个判据算出来的。
// 如果哪天两边对不上，那是 overview 改了而这里没跟，不是两套逻辑。
func (h *OverviewHandler) Triage(c *gin.Context) {
	ov := h.buildOverview()

	items := []gin.H{}
	for _, a := range ov.Attention {
		// ⚠️ Count 是 *int64：nil = **这项没统计成功**，0 = 确实没有。
		// 把 nil 当 0 跳过的话，一个查询失败会被 AI 读成"这类没问题"
		if a.Count == nil {
			g := triageGuide(a.Key)
			items = append(items, gin.H{
				"key": a.Key, "count": nil, "severity": "unknown",
				"what":      g.what + "（本项统计失败，结论不成立）",
				"next_tool": g.tool,
				"why":       "先确认这项为什么查不出来——在它查通之前，不能说这类没有问题",
				"ui_link":   a.Link,
			})
			continue
		}
		if *a.Count <= 0 {
			continue
		}
		g := triageGuide(a.Key)
		items = append(items, gin.H{
			"key":       a.Key,
			"count":     *a.Count,
			"severity":  a.Severity,
			"what":      g.what,
			"next_tool": g.tool,
			"why":       g.why,
			"ui_link":   a.Link,
		})
	}

	// high 排前面：分诊的意义就是先看最要命的
	rank := map[string]int{"high": 0, "medium": 1, "low": 2}
	for i := 1; i < len(items); i++ {
		for j := i; j > 0; j-- {
			a, _ := items[j-1]["severity"].(string)
			b, _ := items[j]["severity"].(string)
			if rank[a] <= rank[b] {
				break
			}
			items[j-1], items[j] = items[j], items[j-1]
		}
	}

	out := gin.H{
		"ok":       true,
		"findings": items,
		"count":    len(items),
	}
	if len(items) == 0 {
		// ⚠️ 「没有发现」和「一切正常」不是一回事。
		// 采集没跑通时同样会是空的，所以必须把数据新鲜度一起给出来，
		// 让调用方自己判断这个"空"可不可信
		out["note"] = "本次分诊没有发现需要关注的事。⚠️ 这不等于系统健康：" +
			"若下面的采集新鲜度显示数据是旧的或没采到，这个「空」不成立。"
		out["freshness"] = ov.Freshness
	}
	c.JSON(http.StatusOK, out)
}

type guide struct{ what, tool, why string }

// triageGuide 把「发现了什么」翻译成「下一步查什么」。
//
//	⚠️ 每条都要给 why。只给工具名的话，AI 会照着调但不知道在找什么，
//	拿到结果也串不成结论 —— 那还是在猜。
func triageGuide(key string) guide {
	switch key {
	case "nodesStale":
		return guide{"节点失联，状态不可信", "list_nodes + cluster_health",
			"先确认是真失联还是采集断了：cluster_health 看采集侧，list_nodes 看最后心跳。" +
				"⚠️ 若同名节点出现两行且一行没有集群名，那是删集群没清数据留下的孤儿（OPSCMDB-022）"}
	case "nodesPressure":
		// 🔴 指向「事件」而不是「节点列表」是有意的：
		//	节点列表的 health 只看 ready/心跳，磁盘 100% 也显示「正常」；
		//	而 list_events(kind=Node) 里 ImageGCFailed / NodeHasDiskPressure /
		//	EvictionThresholdMet 会把整条时间线摆出来 —— 实测就是靠它才拿到真相的。
		return guide{"节点存在资源压力（磁盘/内存/PID/网络）",
			"list_events kind=Node + cluster_health + node_usage",
			"⚠️ 这类故障的表现常常**不在节点上**：磁盘满会让镜像拉不动、Pod 被驱逐，" +
				"看起来像「发布卡住」或「一堆 Pod 异常」。先看 list_events kind=Node 的时间线" +
				"（ImageGCFailed / NodeHasDiskPressure / EvictionThresholdMet），" +
				"再用 cluster_health 拿具体水位。" +
				"⚠️ 别只看 list_nodes 的 health —— 它只反映 ready 和心跳，不看压力位"}
	case "podsBad":
		return guide{"Pod 异常", "diagnose_pod",
			"直接给根因+证据+处置建议。CrashLoopBackOff 时它会自动取上一个容器实例的日志"}
	case "workloadsDown":
		return guide{"工作负载副本全挂", "diagnose_pod + workload_changes",
			"先诊断任一副本拿根因，再看变更历史确认是不是刚改过镜像或配置"}
	case "nsTerminating":
		return guide{"命名空间卡在 Terminating", "list_namespaces + get_manifest",
			"卡住通常是 finalizer 没清；get_manifest 看 finalizers 字段能直接定位是谁挂着"}
	case "pvcsBroken":
		return guide{"存储卷丢失或待绑定", "list_pvcs + list_events",
			"Pending 多半是没有匹配的 StorageClass 或配额不足，事件里会写明"}
	case "certsExpired", "certsFailing", "certsUnknown":
		return guide{"证书过期/续期失败/状态未知", "list_certificates",
			"⚠️ 三种状态处置完全不同：过期要续、失败要看 ACME 报错、" +
				"未知是探测不到（可能域名根本没解析），别当成同一件事"}
	case "domainsExpired":
		return guide{"域名已过期", "list_domains + domain_topology",
			"先确认是否还在用：domain_topology 看它背后有没有活着的服务，没有就不用续"}
	case "domainsUnresolved":
		return guide{"域名解析异常", "cdn_domain_check + dns_consistency",
			"先看解析有没有走 CDN，再看两边 DNS 是否冲突——" +
				"「改了没生效」最常见的原因是改在了 NS 没指向的那一边"}
	case "lbsEmpty":
		return guide{"负载均衡确认无后端", "list_loadbalancers + expose_surface",
			"无后端 = 打不通。配合暴露面看它是不是还挂在公网上"}
	case "lbsUnknown":
		return guide{"负载均衡后端未采到", "list_loadbalancers",
			"⚠️ 「未采到」不等于「没有后端」，别据此判定它是空的"}
	case "orphanNodeRows":
		return guide{"集群已删但节点数据没清干净", "list_clusters",
			"这是数据残留不是节点故障：对应的集群已经不在了，节点行还留着。" +
				"⚠️ 它会让「全站节点数」偏大；处置是清数据，不是去查节点（OPSCMDB-022）"}
	case "clustersNotIngested":
		return guide{"集群未接入", "list_clusters + data_freshness",
			"先确认是没配凭据还是配了但采集失败——两者处置完全不同"}
	default:
		return guide{key, "list_alerts + event_center", "先看告警与事件中心确认影响面"}
	}
}
