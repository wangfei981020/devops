package handlers

import (
	"database/sql"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"ops-cmdb-backend/logx"
)

// 全局态势。
//
// 这一页的存在理由：13 个列表页各自为政，人登录进来第一眼看到的是主机列表，
// 而他真正想先知道的是「今天有什么不对劲」。
//
// ⚠️ 所以它**只汇总已有的判据，不新造判据**。每一条都能点进对应列表页看到
// 一模一样的数字 —— 首页说 3 条异常、点进去看到 5 条，是最快毁掉信任的方式。
// 判据一旦要改，改的是各自列表页里那份，这里跟着走。

type OverviewHandler struct{ DB *sql.DB }

func NewOverviewHandler(db *sql.DB) *OverviewHandler { return &OverviewHandler{DB: db} }

func (h *OverviewHandler) Register(r *gin.RouterGroup) {
	r.GET("/overview", h.Situation)
	// 分诊：给 AI 用的入口，复用同一套判据，额外给出「下一步调哪个工具」
	r.GET("/triage", h.Triage)
}

// attentionItem 一条"需要人看一眼"的事。
type attentionItem struct {
	// Key 用于前端取文案与跳转目标。后端不返回句子（那样英文界面永远漏中文）
	Key string `json:"key"`
	// Count 命中条数。
	//
	// ⚠️ 指针：null = **这项没能统计出来**（查询失败/ 数据源没接），
	// 不是"0 条"。0 是好消息，null 是我们不知道 —— 把后者显示成 0
	// 等于在首页上给一个我们根本没看过的维度发合格证。
	Count *int64 `json:"count"`
	// Severity high / medium，决定前端色调与排序
	Severity string `json:"severity"`
	// Link 点进去看明细的目标（含筛选条件），前端直接用
	Link string `json:"link"`
}

type situationOut struct {
	// Attention 按严重度排好序的待办。空数组 = 全部正常且都统计成功了
	Attention []attentionItem `json:"attention"`

	// Inventory 家底盘点。同样用指针表达"没统计出来"
	Inventory map[string]*int64 `json:"inventory"`

	// Freshness 各类数据最后一次采集的时刻。台账是快照不是实时，
	// 首页尤其要显示它 —— 否则整页数字看起来都像"此刻"
	Freshness map[string]string `json:"freshness"`

	GeneratedAt string `json:"generated_at"`
}

// Situation GET /api/overview
//
//	@Summary		全局态势
//	@Description	汇总各列表页已有的判据，不新造判据。统计失败的项返回 null 而非 0。
//	@Tags			overview
//	@Produce		json
//	@Success		200	{object}	handlers.situationOut
//	@Router			/overview [get]
func (h *OverviewHandler) Situation(c *gin.Context) {
	c.JSON(http.StatusOK, h.buildOverview())
}

// buildOverview 汇总态势。抽出来是为了让 /triage 复用**同一套判据** ——
// 两处各算一遍的话，界面和 AI 迟早给出不一样的结论，而那种不一致最难查。
func (h *OverviewHandler) buildOverview() situationOut {
	out := situationOut{
		Attention:   []attentionItem{},
		Inventory:   map[string]*int64{},
		Freshness:   map[string]string{},
		GeneratedAt: time.Now().Format(time.RFC3339),
	}

	// count 查一个数。查不出来返回 nil —— 见 attentionItem.Count 的注释。
	count := func(q string, args ...any) *int64 {
		var n int64
		if err := h.DB.QueryRow(q, args...).Scan(&n); err != nil {
			return nil
		}
		return &n
	}

	add := func(key, severity, link string, n *int64) {
		// 统计失败也要出现在列表里（前端渲染成"未能统计"），
		// 悄悄跳过等于让一个坏掉的维度从首页上消失
		if n != nil && *n == 0 {
			return
		}
		out.Attention = append(out.Attention, attentionItem{
			Key: key, Count: n, Severity: severity, Link: link,
		})
	}

	// —— 判据全部与各列表页一字不差 ——

	// 节点失联：读采集时刻算好的 hb_stale，判据与节点列表页一字不差。
	// 🔴 不要改回 `last_heartbeat < NOW() - INTERVAL`：那个列是取整过的，
	// 拿它倒推年龄会误报（UAT 16 台误报 8 台，见 migration 115）。
	// 🔴 必须 INNER JOIN：LEFT JOIN 会把「集群已删但节点行还在」的孤儿算进来，
	// 而节点列表页按集群展示、看不到这些行 —— 于是首页说「1 个失联」，
	// 点「查看」跳到 /k8s/nodes?status=stale 却是 0 条。
	// 生产实测过这个死路：告警说有问题、点进去什么也没有，是最伤信任的一种 bug。
	//
	// ⚠️ 计数与列表必须用同一套条件。孤儿行不是不管，而是单独作为一条
	// 「数据残留」列出来（见下面的 orphanNodeRows）—— 它是数据完整性问题，
	// 不是节点健康问题，混在一起两边都说不清。
	// 🔴 节点资源压力（磁盘/内存/PID）—— 能直接打垮发布的一类故障，
	//	而入口层此前对它完全失明（OPSCMDB-042）。
	//
	//	实测 DEV：node12 磁盘 100%、kubelet 反复 ImageGC 失败、已进入 DiskPressure 驱逐，
	//	导致 Jenkins 构建拉 405MB 镜像花了 12 分 16 秒、卡在「队列中」。
	//	而 triage / diagnose_cluster / list_nodes 三个入口**全都显示正常**，
	//	只有 cluster_health 报了出来 —— 判据本来就有，只是被关在一个入口里。
	//
	// ⚠️ 关键在于这一条**不需要 Prometheus**：k8s_nodes.conditions 列里
	//	已经存着压力位摘要（采集时算好的，只认 MemoryPressure/DiskPressure/
	//	PIDPressure/NetworkUnavailable 为真压力）。attention 这一层不碰观测数据源，
	//	所以磁盘水位百分比进不来，但「有没有压力」这件事一句 SQL 就够了。
	add("nodesPressure", "high", "/k8s/nodes?status=pressure",
		count(`SELECT COUNT(*) FROM k8s_nodes n
			JOIN k8s_clusters cl ON cl.id = n.cluster_id
			WHERE n.conditions <> ''`))

	add("nodesStale", "high", "/k8s/nodes?status=stale",
		count(`SELECT COUNT(*) FROM k8s_nodes n
			JOIN k8s_clusters cl ON cl.id = n.cluster_id
			WHERE n.hb_stale = 1
			   OR n.synced_at IS NULL
			   OR n.synced_at < NOW() - INTERVAL ? SECOND`,
			int(collectionStaleAfter.Seconds())))

	// 孤儿节点行：集群已经删了，节点数据没清干净（OPSCMDB-022）。
	// 单独列出来而不是并进 nodesStale —— 处置完全不同：
	// 前者要去查节点，后者要去清数据。
	add("orphanNodeRows", "medium", "/k8s/clusters",
		count(`SELECT COUNT(*) FROM k8s_nodes n
			WHERE NOT EXISTS (SELECT 1 FROM k8s_clusters cl WHERE cl.id = n.cluster_id)`))

	// Pod 异常：不是 Running/Succeeded，或重启过（同 podHealthCase）
	add("podsBad", "medium", "/k8s/pods?health=bad",
		count(`SELECT COUNT(*) FROM k8s_pods WHERE phase NOT IN ('Running','Succeeded')`))

	// 工作负载全挂：期望 > 0 但就绪 0（scaled_zero 不算，那是正常状态）
	add("workloadsDown", "high", "/k8s/workloads?health=down",
		count(`SELECT COUNT(*) FROM k8s_workloads WHERE replicas_desired > 0 AND replicas_ready = 0`))

	// 卷丢失 / 待绑定
	add("pvcsBroken", "high", "/k8s/pvcs?health=lost",
		count(`SELECT COUNT(*) FROM k8s_pvcs WHERE status IN ('Lost','Pending')`))

	// 证书：已过期 + 续期失败。读不出到期日的单独一条 —— 那是"我们不知道"
	add("certsExpired", "high", "/resources/certs?health=expired",
		count(`SELECT COUNT(*) FROM certificates WHERE expiry_at IS NOT NULL AND expiry_at < NOW()`))
	add("certsFailing", "high", "/resources/certs?health=failing",
		count(`SELECT COUNT(*) FROM certificates WHERE last_error <> '' AND expiry_at >= NOW()`))
	add("certsUnknown", "medium", "/resources/certs?health=unknown",
		count(`SELECT COUNT(*) FROM certificates WHERE expiry_at IS NULL`))

	// 域名：已过期 / 解析异常（被忽略的不计入 —— 那是人为决定不管的）
	add("domainsExpired", "high", "/resources/domains?health=expired",
		count(`SELECT COUNT(*) FROM domains WHERE ignored = 0 AND expiry_at IS NOT NULL AND expiry_at < NOW()`))
	add("domainsUnresolved", "high", "/resources/domains?health=unresolved",
		count(`SELECT COUNT(*) FROM domains WHERE ignored = 0 AND resolve_status <> '' AND resolve_status <> 'ok'`))

	// LB 无后端：只数"确认 0 个"的项目，未采过的不算（那是采集缺口，见下一条）
	add("lbsEmpty", "high", "/resources/lbs?health=empty",
		count(`SELECT COUNT(*) FROM cloud_loadbalancers l
			WHERE l.stale = 0
			  AND EXISTS (SELECT 1 FROM cloud_lb_backends b WHERE b.project = l.project)
			  AND NOT EXISTS (SELECT 1 FROM cloud_lb_backends b
			                  WHERE b.project = l.project AND b.lb_name = l.name)`))

	// 采集缺口单独成条，不和业务故障混在一起：
	// 一个要去查连通性，一个要去修业务
	add("lbsUnknown", "medium", "/resources/lbs?health=unknown",
		count(`SELECT COUNT(*) FROM cloud_loadbalancers l
			WHERE l.stale = 0
			  AND NOT EXISTS (SELECT 1 FROM cloud_lb_backends b WHERE b.project = l.project)`))
	add("clustersNotIngested", "medium", "/k8s/clusters",
		count(`SELECT COUNT(*) FROM k8s_clusters c WHERE c.enabled = 1
			AND NOT EXISTS (SELECT 1 FROM k8s_nodes n WHERE n.cluster_id = c.id)`))

	// 🔴 接入配错：集群的指标标签值在观测端点里不存在。
	//
	//	后果不是"报错"，是**所有带集群条件的查询静默返回空** ——
	//	而空看起来和"这个集群确实没有东西"一模一样。生产上因此让
	//	磁盘水位/用量/OOM 全线失效，挂了多久没人知道（OPSCMDB-044）。
	//
	// ⚠️ 判定不在这里做，读的是 integration_check 任务落库的结果。
	//	在这里现探的话，每次打开首页都要去打一圈外部数据源。
	add("integrationBroken", "high", "/k8s/clusters",
		count(`SELECT COUNT(*) FROM integration_issues WHERE kind = 'cluster_label'`))

	// ⚠️ 「没核对成」要单独报，不能并进上面那条，也不能不报。
	//	并进去 → 把"不知道"说成"有问题"；不报 → 把"不知道"说成"没问题"。
	//	两者都是在首页上给一个没看过的维度发合格证。
	add("integrationUnverified", "medium", "/admin/obs-endpoints",
		count(`SELECT COUNT(*) FROM integration_issues WHERE kind = 'unverified'`))

	// 命名空间卡在 Terminating：会一直占着名字让同名重建失败
	add("nsTerminating", "medium", "/k8s/namespaces?phase=Terminating",
		count(`SELECT COUNT(*) FROM k8s_namespaces WHERE phase = 'Terminating'`))

	// 🔴 临期域名 —— 总览页上一直**一个字都没有**（OPSCMDB-070）。
	//
	//	`/api/dashboard` 早就把它算好并按剩余天数排好序了，只是总览没接。
	//	而它比这一页已经在显示的东西都更要紧：
	//	  46 个工作负载副本全挂 → 可回滚、可重启
	//	  37 个 LB 无后端      → 改配置即可
	//	  **域名过期            → 要花钱，且不可逆**
	//	（进赎回期后费用是正常续费的数倍，再往后直接释放）。
	//
	// ⚠️ 与证书分成两条，不合并。两者的处置路径完全不同：
	//	证书是自动续期链路的问题（去修 cert-manager / ACME），
	//	域名是**采购动作**（要有人去付钱）。合成一条"有东西要到期了"，
	//	看的人不知道该找谁。
	// 🔴 判据必须与域名列表页的 `health=soon` **一字不差**，否则首页说 6 个、
	//	点进去却是别的数 —— 那是最伤信任的一种 bug（见上方 nodesStale 的教训）。
	//
	//	列表页的 domainHealth 是**分档**的，顺序是：
	//	  ignored → expired → unresolved → unknown → soon → cert_soon → ok
	//	也就是说一个 20 天后到期、但当前解析不通的域名，会被归进 unresolved
	//	而**不是** soon。所以这里必须同样排除掉前面那几档，
	//	不能只写"30 天内到期"（那样会多算，点进去对不上）。
	//
	// ⚠️ 筛选值叫 `soon` 不叫 `expiring` —— 我第一版写的是 expiring，
	//	而域名页根本没有这个取值，链接过去会得到一个没有筛选效果的列表。
	//	**链接里的筛选值必须从前端的 options 里抄，不能按语义猜。**
	add("domainsExpiring", "high", "/resources/domains?health=soon",
		count(`SELECT COUNT(*) FROM cis c JOIN domains d ON d.ci_id=c.id
			WHERE c.type='domain' AND d.stale=0 AND d.ignored=0
			  AND d.expiry_at IS NOT NULL
			  AND DATEDIFF(d.expiry_at, NOW()) BETWEEN 0 AND 30
			  AND COALESCE(d.resolve_status,'') IN ('', 'ok')`))

	// 🔴 从没跑过的定时任务 —— 这一条比任何单项数据都更根本。
	//
	//	一个从没跑过的任务，意味着**所有依赖它的判断都是空的**，
	//	而空在界面上长得跟"没问题"一模一样：
	//	  证书到期检测（443）停用且从没跑过 → 890 张证书里 828 张到期日"未知"
	//	  → 总览的证书卡片说"无临期证书"→ 而实际有两张 44 小时后到期
	//	（生产实测，OPSCMDB-060 / PROD-CERT-20260819）。
	//
	//	定时任务页自己已经把这件事标出来了（"2 tasks never ran"），
	//	但**没人会先去那一页** —— 人从总览进来，看到一片绿就走了。
	//	所以它必须出现在这里。
	//
	// ⚠️ 判据是 last_run_at IS NULL，**不看 enabled**：
	//	停用且没跑过、启用了却没跑过，两者后果一样（数据是空的），
	//	而后者更隐蔽 —— 界面上那个任务看着是"开着的"。
	add("tasksNeverRan", "high", "/runtime/cron",
		count(`SELECT COUNT(*) FROM scheduled_tasks WHERE last_run_at IS NULL`))

	sortAttention(out.Attention)

	// —— 家底 ——
	out.Inventory["hosts"] = count(`SELECT COUNT(*) FROM hosts WHERE stale = 0`)
	out.Inventory["clusters"] = count(`SELECT COUNT(*) FROM k8s_clusters WHERE enabled = 1`)
	out.Inventory["nodes"] = count(`SELECT COUNT(*) FROM k8s_nodes`)
	out.Inventory["pods"] = count(`SELECT COUNT(*) FROM k8s_pods`)
	out.Inventory["domains"] = count(`SELECT COUNT(*) FROM domains WHERE ignored = 0`)
	out.Inventory["certs"] = count(`SELECT COUNT(*) FROM certificates`)

	// —— 新鲜度 ——
	// ⚠️ 查询**失败**和**真的没数据**必须区分开。
	//
	//	原来这里两种情况都是"不放进 map"，前端一律显示「未接入」。
	//	于是一个写错列名的 SQL（比如我把 cert_check_at 写成 cert_checked_at）
	//	会伪装成「证书未接入」—— Go 编译不报错（SQL 是字符串），
	//	界面上也看不出异常，因为「未接入」本身是个合法状态。
	//	这正是这一页要修的那类问题，不能在修它的时候又造一个。
	//
	//	所以查询出错时**留日志**。数据真的为空（NULL）不留 —— 那是正常状态。
	fresh := func(key, q string) {
		var t sql.NullTime
		if err := h.DB.QueryRow(q).Scan(&t); err != nil {
			logx.J("overview", "freshness_query_fail", map[string]any{
				"key": key, "err": err.Error(),
				"note": "这一项会显示成「未接入」，但真实原因是查询失败——多半是列名或表名写错了",
			})
			return
		}
		if t.Valid {
			out.Freshness[key] = t.Time.Format(time.RFC3339)
		}
		// t 为 NULL = 这一类确实没采过。不放进 map，前端显示"未接入"，这是对的
	}
	fresh("hosts", `SELECT MAX(synced_at) FROM hosts`)
	fresh("k8s", `SELECT MAX(synced_at) FROM k8s_nodes`)
	fresh("domains", `SELECT MAX(last_synced_at) FROM domains`)
	// ⚠️ 证书这一行原来没有，而它恰恰是最该有的一行。
	//
	//	家底里「证书 0」看起来像个确定的事实，没人会去质疑它。
	//	但如果新鲜度里有一行「证书 从未采集」，看的人立刻就知道那个 0 不可信 ——
	//	而这正是 OPSCMDB-031 P0-4 只能靠 MCP 交叉比对才发现的原因（P1-6）。
	//
	//	取的是 443 实测探测的时刻（domain_records.cert_checked_at），
	//	不是 certificates 表 —— 因为「有没有在探」才是这一行要回答的问题。
	//	certificates 表是我方签发的台账，它为空是正常的。
	fresh("certs", `SELECT MAX(cert_check_at) FROM domain_records`)

	return out
}

// sortAttention 高危在前；同级按数量降序。
//
// ⚠️ 统计失败（Count == nil）排在**最前**：它意味着这个维度我们完全没看到，
// 比"看到了 3 条"更值得先处理。排在最后的话，它会被淹没在正常项里，
// 而首页看起来一切正常。
func sortAttention(items []attentionItem) {
	rank := func(a attentionItem) int {
		if a.Count == nil {
			return 0
		}
		if a.Severity == "high" {
			return 1
		}
		return 2
	}
	for i := 1; i < len(items); i++ {
		for j := i; j > 0; j-- {
			a, b := items[j-1], items[j]
			ra, rb := rank(a), rank(b)
			if ra < rb {
				break
			}
			if ra == rb {
				var ca, cb int64
				if a.Count != nil {
					ca = *a.Count
				}
				if b.Count != nil {
					cb = *b.Count
				}
				if ca >= cb {
					break
				}
			}
			items[j-1], items[j] = items[j], items[j-1]
		}
	}
}
