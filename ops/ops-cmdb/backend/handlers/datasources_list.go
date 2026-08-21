package handlers

import (
	"database/sql"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/robfig/cron/v3"

	"ops-cmdb-backend/internal/httpx"
	"ops-cmdb-backend/k8ssource"
	"ops-cmdb-backend/logx"
)

// 数据源：云账号 / 集群 / 观测端点 三类接入的健康状况。
//
// ⚠️⚠️ 这个接口**绝不返回任何凭据字段**（cred_enc / kubeconfig_enc / token_enc）。
// 它只回答"配没配、连不连得上、多久没同步了"。
// 用显式列清单而不是 SELECT *：加字段必须有人手写一行（CONVENTIONS §3.4.2）。

type DataSourceHandler struct{ DB *sql.DB }

func NewDataSourceHandler(db *sql.DB) *DataSourceHandler { return &DataSourceHandler{DB: db} }

func (h *DataSourceHandler) Register(r *gin.RouterGroup) {
	r.GET("/datasource-list", h.List)
}

// dsStaleAfter 停摆判定的**回退**阈值 —— 只在取不到该数据源自己的调度周期时才用。
//
// ⚠️ 不要把它当成通用阈值。它曾经是所有数据源共用的固定值，
// 而 host_sync 每天只跑一次（0 3 * * *）——实测一天 24 小时里有 18 小时
// 会被它误报成「同步停摆」，且提示语还断言「早该同步了却没有」，
// 把一个守时的任务说成失职（OPSCMDB-031 P1-55，同一判据错误在
// 凭据管理 / 数据源 / 全局态势三个页面都表现过，因为它们读同一个接口）。
//
// 正常路径走 scheduleStaleWindow（2 × 周期 + 1 小时）。
const dsStaleAfter = 6 * time.Hour

type dataSourceOut struct {
	// Kind cloud / cluster / obs —— 三类接入
	Kind string `json:"kind"`
	Name string `json:"name"`
	// Type 各类下的细分（gcp / gke / prometheus / loki / n9e …），原样透传
	Type    string `json:"type"`
	Enabled bool   `json:"enabled"`

	// HasCredential 本条记录里有没有存凭据。**只给布尔，绝不给内容**。
	//
	// 🔴 不要拿它当健康判据。「没存凭据」≠「采不到东西」，三类都有反例：
	//   - GKE 集群走云账号的 service account，本来就不存 kubeconfig
	//   - 内网 Prometheus / Loki 无鉴权，留空是正常配置（编辑框自己写着"需要鉴权时填"）
	// 健康判定看 Health 字段。
	HasCredential bool `json:"has_credential"`

	// Credential 凭据从哪来：
	//   configured = 本条存了凭据
	//   inherited  = 不需要自己存（如 GKE 走绑定的云账号）
	//   none       = 没存。对无鉴权端点是正常的，对云账号才是问题
	Credential string `json:"credential"`

	// CredRequired 这类数据源**没凭据就一定采不到**。
	//
	//	🔴 「该有凭据却没有」和「凭据字段为空」是两件事。
	//
	//	凭据管理页原来按 `has_credential=false` 分组，于是
	//	3 个 2~4 分钟前刚同步成功的 K8s 集群被列进「启用了但没配凭据」——
	//	它们走集群内 ServiceAccount / 云账号继承，**本来就不需要**在这里配
	//	（OPSCMDB-031 P2-50）。
	//
	//	误报和真问题混在同一个数字里，那个数字就没人信了：
	//	「7 处缺凭据」里 3 处是假的，剩下 4 处真的也跟着被忽略。
	//
	//	判据放后端：它知道每类数据源的认证方式，前端不该再猜一次。
	CredRequired bool `json:"cred_required"`

	// Health 这个数据源现在到底能不能用：
	//   ok / no_credential / stale / never_synced / not_applicable / disabled
	//
	// 🔴 判定顺序：**事实优先于推断**。有新鲜的同步记录就一定能采到，
	// 此时无论凭据字段是什么都判 ok —— 曾经反过来写，于是
	// 「缺凭据」和「2 分钟前同步成功」出现在同一行里自相矛盾。
	Health string `json:"health"`
	// HealthNote 给人看的一句话，说明这个判定是怎么来的
	HealthNote string `json:"health_note,omitempty"`

	// LastSyncAt 空 = 从没同步过（不是"刚同步完"）
	LastSyncAt string `json:"last_sync_at,omitempty"`
	// LastResult 上次同步结果，原样透传（可能是错误串）
	LastResult string `json:"last_result"`
	// Stale 早该同步了却没有 —— 数据源静默停摆
	Stale bool `json:"stale"`
	// ScheduleKnown 停摆判定用的是**这个数据源自己的调度周期**还是回退的固定值。
	//
	//	⚠️ false 时**不要**断言「早该同步了却没有」——
	//	那句话在一个按时运行的每日任务上是错的，它把守时说成了失职。
	ScheduleKnown bool `json:"schedule_known"`
}

// List GET /api/datasource-list
//
//	@Summary		数据源接入状况
//	@Description	云账号/集群/观测端点三类。**不返回任何凭据内容**，只给"配没配"。
//	@Tags			admin
//	@Produce		json
//	@Param			kind	query	string	false	"类别"	Enums(all, cloud, cluster, obs)
//	@Success		200		{object}	httpx.ListResponse[handlers.dataSourceOut]
//	@Router			/datasource-list [get]
func (h *DataSourceHandler) List(c *gin.Context) {
	q := httpx.BindPage(c, "kind")
	now := time.Now()
	all := []dataSourceOut{}

	// —— 云账号 ——
	//
	// 🔴 同步时间必须从 cloud_account_projects 取，**不是** cloud_accounts。
	// 采集回写的是前者（hosts.go 的 syncProject），后者那一列从来没人写 ——
	// 直接读 cloud_accounts.last_sync_at 会让一个每小时正常同步的账号
	// 永远显示「从没同步过」，进而被判成「缺凭据，采不到任何东西」。
	//
	// 凭据同理：可以配在账号上，也可以按项目单独配。只看账号级会漏判。
	type acctAgg struct {
		lastSync sql.NullTime
		hasCred  bool
		result   string
		// 逐项目的结果，用来算账号级汇总
		results []string
	}
	perAcct := map[int]*acctAgg{}
	// 🔴 原来这里是 `COALESCE(MAX(last_result),'')` —— **MAX() 对字符串取字典序最大的那个**。
	//
	//	`MAX("同步 25 台在用", "同步 38 台在用")` → "同步 38 台在用"
	//	于是账号级显示「同步完成: 38 台在用」，读起来像"这个账号一共同步了 38 台"，
	//	**而实际是 25+38=63 台**（OPSCMDB-031 P1-56 / P1-58 —— 一个根因两页表现）。
	//
	//	账号级要的是**汇总**，不是"其中某一个项目的结果"。所以逐行读回来自己归并。
	if rows, err := h.DB.Query(`SELECT account_id, last_sync_at,
		CASE WHEN cred_enc IS NOT NULL AND cred_enc <> '' THEN 1 ELSE 0 END,
		COALESCE(last_result,'')
		FROM cloud_account_projects`); err == nil {
		for rows.Next() {
			var id, hasCred int
			var ls sql.NullTime
			var res string
			if rows.Scan(&id, &ls, &hasCred, &res) == nil {
				a := perAcct[id]
				if a == nil {
					a = &acctAgg{}
					perAcct[id] = a
				}
				// 账号的"最后同步"取所有项目里最晚的那个
				if ls.Valid && (!a.lastSync.Valid || ls.Time.After(a.lastSync.Time)) {
					a.lastSync = ls
				}
				if hasCred == 1 {
					a.hasCred = true
				}
				if res != "" {
					a.results = append(a.results, res)
				}
			}
		}
		rows.Close()
	}

	if rows, err := h.DB.Query(`SELECT id, name, provider,
		(cred_enc IS NOT NULL AND cred_enc <> '') AS has_cred FROM cloud_accounts`); err == nil {
		for rows.Next() {
			o := dataSourceOut{Kind: "cloud", Enabled: true}
			var id, hasCred int
			if rows.Scan(&id, &o.Name, &o.Type, &hasCred) == nil {
				agg := perAcct[id]
				credOnAcct := hasCred == 1
				credOnProj := agg != nil && agg.hasCred
				o.HasCredential = credOnAcct || credOnProj
				o.Credential = credState(o.HasCredential, false)
				// 云账号必须拿密钥去调云 API，没有就一定采不到
				o.CredRequired = true
				if agg != nil {
					o.LastResult = summarizeProjectResults(agg.results)
					if agg.lastSync.Valid {
						o.LastSyncAt = agg.lastSync.Time.Format(time.RFC3339)
						// ⚠️ 用 host_sync **自己的**调度周期，不用固定 6 小时。
						// 它每天 03:00 跑一次，固定阈值会让它每天有 22 小时被误报为停摆
						win, known := scheduleStaleWindow(h.DB, "host_sync")
						o.Stale = now.Sub(agg.lastSync.Time) > win
						o.ScheduleKnown = known
					}
				}
				// ⚠️ 不再假设"云账号没凭据就一定采不到"。
				// 跑在 GKE 里时可以用 Workload Identity / ADC，凭据来自元数据服务器、
				// 库里根本没有。生产实测：GCP 账号 cred_enc 为空，但主机采到了 25 台。
				// 所以仍然由 finalizeHealth 按事实判定，只在**完全没有任何采集痕迹**时
				// 才提示可能缺凭据。
				finalizeHealth(&o, now, false)
				if o.Health == "never_synced" && !o.HasCredential {
					o.HealthNote = "既没有存凭据，也没有任何同步记录。" +
						"若本实例跑在云上并使用 Workload Identity / ADC（凭据来自元数据服务器、不入库），" +
						"这属正常，请点「立即同步」验证；否则需要补配凭据"
				}
				all = append(all, o)
			}
		}
		rows.Close()
	}

	// —— 集群 ——
	if rows, err := h.DB.Query(`SELECT COALESCE(display_name, name), provider, enabled,
		(kubeconfig_enc IS NOT NULL AND kubeconfig_enc <> '') AS has_kc,
		COALESCE(cloud_account_id, 0) FROM k8s_clusters`); err == nil {
		for rows.Next() {
			o := dataSourceOut{Kind: "cluster"}
			var enabled, hasKC, cloudAcct int
			if rows.Scan(&o.Name, &o.Type, &enabled, &hasKC, &cloudAcct) == nil {
				o.Enabled = enabled == 1
				o.HasCredential = hasKC == 1
				// ⚠️ GKE 用绑定云账号的 service account 认证，**不存 kubeconfig**。
				// 只看 kubeconfig_enc 会把正常工作的 GKE 集群判成"缺凭据"
				o.Credential = credState(hasKC == 1, cloudAcct > 0)
				// ⚠️ 集群**不是**必须在这里配凭据：GKE 走绑定云账号的
				// service account，自建集群可以用集群内 ServiceAccount。
				// 判据是"能不能采到"，而那由下面的 finalizeHealth 按事实定
				o.CredRequired = false
				// 集群的"最后同步"看节点表 —— 它没有自己的 last_sync 字段
				var t sql.NullTime
				if h.DB.QueryRow(`SELECT MAX(n.synced_at) FROM k8s_nodes n
					JOIN k8s_clusters c ON c.id = n.cluster_id
					WHERE COALESCE(c.display_name, c.name) = ?`, o.Name).Scan(&t) == nil && t.Valid {
					o.LastSyncAt = t.Time.Format(time.RFC3339)
					// 集群不走 scheduled_tasks，它由采集器按固定间隔轮询。
					// 同样用「2 × 周期 + 余量」而不是那个给云账号定的 6 小时
					clusterWin := 2*time.Duration(k8ssource.DefaultSyncIntervalSec)*time.Second + time.Hour
					o.Stale = o.Enabled && now.Sub(t.Time) > clusterWin
					o.ScheduleKnown = true
				}
				finalizeHealth(&o, now, false)
				all = append(all, o)
			}
		}
		rows.Close()
	}

	// —— 观测端点 ——
	if rows, err := h.DB.Query(`SELECT name, type, enabled,
		(token_enc IS NOT NULL AND token_enc <> '') AS has_token FROM obs_endpoints`); err == nil {
		for rows.Next() {
			o := dataSourceOut{Kind: "obs"}
			var enabled, hasToken int
			if rows.Scan(&o.Name, &o.Type, &enabled, &hasToken) == nil {
				o.Enabled = enabled == 1
				o.HasCredential = hasToken == 1
				// ⚠️ 内网 Prometheus / Loki 通常无鉴权，留空是**正常配置**，
				// 不是缺失。真正该报的是"测试连通性失败"，那要实际去连，不在本接口做
				o.Credential = credState(hasToken == 1, false)
				// ⚠️ 观测端点**不是**必须配令牌：内网 Prometheus / Loki
				// 通常无鉴权，编辑框自己写着"需要鉴权时填"。
				// 把它们列进「缺凭据」是把一个正常配置说成缺失
				o.CredRequired = false
				finalizeHealth(&o, now, false)
				all = append(all, o)
			}
		}
		rows.Close()
	}

	kind := q.Filters["kind"]
	facets := map[string]map[string]int64{"kind": {}}
	for _, d := range all {
		facets["kind"][d.Kind]++
		facets["kind"]["all"]++
	}
	filtered := make([]dataSourceOut, 0, len(all))
	for _, d := range all {
		if kind != "" && kind != "all" && d.Kind != kind {
			continue
		}
		filtered = append(filtered, d)
	}

	// 有问题的排前面。判据用 Health，不用 HasCredential ——
	// 后者只说明"字段空不空"，与能不能采到无关
	sev := func(d dataSourceOut) int {
		switch d.Health {
		case "no_credential":
			return 0
		case "stale":
			return 1
		case "never_synced":
			return 2
		case "disabled":
			return 4
		case "not_applicable":
			return 3 // 正常态，和 ok 同级 —— 不该被顶到"要处理"的位置
		default:
			return 3
		}
	}
	sort.SliceStable(filtered, func(i, j int) bool {
		if s1, s2 := sev(filtered[i]), sev(filtered[j]); s1 != s2 {
			return s1 < s2
		}
		return filtered[i].Name < filtered[j].Name
	})

	total := int64(len(filtered))
	lo := min(q.Offset(), len(filtered))
	hi := min(lo+q.Size, len(filtered))
	c.JSON(http.StatusOK, httpx.NewList(filtered[lo:hi], q, total).WithFacets(facets))
}

// credState 凭据从哪来。inherited = 不需要自己存（当前只有 GKE 走云账号这一种）。
func credState(has, inherited bool) string {
	switch {
	case has:
		return "configured"
	case inherited:
		return "inherited"
	default:
		return "none"
	}
}

// finalizeHealth 定这个数据源到底能不能用。
//
// 🔴 判定顺序是本函数的全部要点：**事实优先于推断**。
//
// 有新鲜的同步记录 = 它刚刚确实采到了东西，此时任何"它应该采不到"的推断都是错的。
// 曾经反过来写（先看凭据字段），于是生产上出现了
// 「缺凭据，采不到任何东西」和「最后同步 2 分钟前」并排显示在同一行的情况 ——
// 同一行里自相矛盾，读的人只能自己猜哪个是真的。
//
// credRequired：这类数据源没凭据就一定采不到（只有云账号是这样，
// 它必须拿密钥去调云 API）。观测端点无鉴权是常态，集群可以从云账号继承。
func finalizeHealth(o *dataSourceOut, now time.Time, credRequired bool) {
	switch {
	case !o.Enabled:
		o.Health = "disabled"
		o.HealthNote = "已停用，不会采集"
	case o.LastSyncAt != "" && !o.Stale:
		// 事实：它刚采过。不管凭据字段长什么样
		o.Health = "ok"
		if o.Credential == "none" {
			o.HealthNote = "没有单独配凭据，但同步正常——该端点多半无需鉴权"
		}
	case o.Stale:
		o.Health = "stale"
		// ⚠️ 措辞要看判据可不可信。
		//
		//	「早该同步了却没有」是一个**断言**。用固定阈值判出来的 stale
		//	在每日任务上是错的（一天 22 小时都会命中），
		//	这时说这句话等于把一个守时的任务描述成失职（P1-55）。
		//	用到了真实调度周期才敢这么说；否则只陈述事实，让人自己去核对。
		if o.ScheduleKnown {
			o.HealthNote = "按它自己的调度周期算，早该同步了却没有 —— 数据是旧的"
		} else {
			o.HealthNote = "距上次同步已经很久了。⚠️ 这个判断用的是通用阈值（没取到它的调度周期），" +
				"如果它本来就是低频任务，这条提示可能是误报 —— 去「运行 / 巡检」看它的实际周期和上次执行"
		}
	case credRequired && o.Credential == "none":
		o.Health = "no_credential"
		o.HealthNote = "没有凭据，调不了云 API，采不到任何东西"
	case o.LastSyncAt == "":
		// ⚠️ "从没同步过"要和"缺凭据"分开：前者可能只是还没到点，
		// 也可能是这类数据源根本不走定时同步。
		//
		// 🔴 文案必须按类别给。写一句通用的会串台——
		// 曾经三类都显示"观测端点是被查询时才用的"，云账号那行读起来完全不知所云
		switch o.Kind {
		case "obs":
			// 🔴 观测端点的"从没同步"**是正常状态**，不是问题。
			//
			//	它是被查询时才用的，本来就不产生同步记录。
			//	原来它和云账号/集群共用 `never_synced`，而那个状态在界面上是**橙色**
			//	（本产品里橙色一贯表示需要注意）——于是同一行里
			//	**颜色在报警、文字在安抚**（OPSCMDB-031 P1-61）：
			//	  徽章：● 从未同步（橙）
			//	  说明：本来就不一定有同步记录
			//	6 条观测端点全是这个样子。
			//
			//	单独一个状态才能让颜色说对话。
			o.Health = "not_applicable"
			// ⚠️ 文案里**不要**写"请点「测试连通性」"。
			//
			//	那个按钮在「观测端点」页上，而这一页是纯只读的（11 行 0 个按钮）。
			//	给出具体指引却指向一个不存在的位置，比不给指引更糟 ——
			//	它让人确信有路可走，然后白找一圈（P1-59）。
			//	本轮这是同一模式的第三次（证书空态指向不存在的菜单、
			//	变更影响面说"列表页每行都能拿到 ID"而列表页不显示 ID）。
			//	要给路就给能点的，前端会在这一行渲染一个到观测端点页的链接。
			o.HealthNote = "观测端点是被查询时才用的，没有同步记录是正常的"
		case "cloud":
			o.Health = "never_synced"
			o.HealthNote = "从没同步过。云账号是定时同步的，长期为空说明同步没跑起来——" +
				"点「立即同步」看报什么错"
		case "cluster":
			o.Health = "never_synced"
			o.HealthNote = "从没采集过。检查集群是否启用、凭据或绑定的云账号是否可用"
		default:
			o.Health = "never_synced"
			o.HealthNote = "从没同步过"
		}
	default:
		o.Health = "ok"
	}
}

// scheduleStaleWindow 按数据源**自己的调度周期**算容忍窗口。
//
// # 🔴 为什么不能所有数据源共用一个小时数
//
// 原来是 `dsStaleAfter = 6h` 一刀切。而 `host_sync` 是**每天跑一次**（`0 3 * * *`）：
// 上午 9 点之后距上次执行就超过 6 小时，于是**从每天 09:00 一直到次日 03:00**
// 这个数据源都被标成「同步停摆」——**一天 24 小时里约 22 小时是误报**
// （OPSCMDB-031 P1-55，同一个判据错误在凭据管理、数据源、全局态势三个页面都表现过）。
//
// 更糟的是 health_note 直接断言「早该同步了却没有」——
// 这句话在 13:55 是错的，它把一个**按时运行**的任务描述成了失职。
//
// ⚠️ 这是同一类根因的第三次出现（前两次是节点心跳误报 OPSCMDB-025 / 027）：
// **判定阈值 ≈ 或小于信号本身的更新周期。**
// 正确做法是拿该信号自己的周期算窗口，而不是拍一个固定值。
//
// 窗口取 `2 × 周期 + 1 小时`：
//   - 2 倍是为了容忍"错过一轮"（一次失败重试、一次部署重启都会错过一轮）
//   - 加 1 小时是给执行本身留时间（全量同步可能跑十几分钟）
//
// 取不到 cron 表达式时回退到固定值 —— 但那时**不该**断言"早该同步了"，
// 调用方据 known 决定措辞。
func scheduleStaleWindow(db *sql.DB, taskKey string) (window time.Duration, known bool) {
	var expr string
	if db.QueryRow(`SELECT schedule FROM scheduled_tasks WHERE task_key=?`, taskKey).Scan(&expr) != nil {
		return dsStaleAfter, false
	}
	sched, err := cron.ParseStandard(strings.TrimSpace(expr))
	if err != nil {
		// ⚠️ 解析不了要留痕：一个写错的 cron 表达式会让这里悄悄退回固定阈值，
		// 而那正是误报的来源（迁移 089 就修过一次 6 段写成 5 段的问题）
		logx.J("datasources", "cron_unparsable", map[string]any{
			"task": taskKey, "schedule": expr, "err": err.Error(),
			"note": "停摆判定退回固定 6 小时阈值，每日任务会被误报为停摆",
		})
		return dsStaleAfter, false
	}
	// 用连续两次触发的间隔当周期。对 `@every 90s` 和标准 cron 都适用
	now := time.Now()
	n1 := sched.Next(now)
	n2 := sched.Next(n1)
	period := n2.Sub(n1)
	if period <= 0 {
		return dsStaleAfter, false
	}
	return 2*period + time.Hour, true
}

// summarizeProjectResults 把各项目的同步结果汇总成账号级的一句话。
//
// # ⚠️ 不能"取其中一个"
//
//	原来是 SQL 里 `MAX(last_result)` —— 字典序最大的那个。
//	两个项目分别同步了 25 台和 38 台时，账号级显示「同步 38 台在用」，
//	读起来像"这个账号一共 38 台"，而实际是 63 台（P1-56 / P1-58）。
//
// # 判定顺序：先说坏消息
//
//	有失败就报失败 —— 那是需要行动的。全成功时只说"N 个项目都正常"，
//	具体每个项目同步了多少台，在云账号页各自那一行里看得到。
func summarizeProjectResults(results []string) string {
	if len(results) == 0 {
		return ""
	}
	failed := make([]string, 0, len(results))
	for _, r := range results {
		if isFailedSyncResult(r) {
			failed = append(failed, r)
		}
	}
	if len(failed) == 0 {
		if len(results) == 1 {
			return results[0]
		}
		return fmt.Sprintf("%d 个项目均同步正常", len(results))
	}
	// 失败的原样带出来 —— 那句话往往就是"这个项目为什么没数据"的答案
	if len(failed) == len(results) {
		return fmt.Sprintf("%d 个项目全部失败：%s", len(failed), strings.Join(failed, "；"))
	}
	return fmt.Sprintf("%d/%d 个项目失败：%s", len(failed), len(results), strings.Join(failed, "；"))
}

// isFailedSyncResult 这条结果串算不算失败。
//
//	⚠️ 用**否定式**判据（含"失败"字样才算失败），不是肯定式（含"成功"才算成功）。
//	上游的成功文案有好几种写法（"同步 25 台在用"、"采集完成"、"ok"），
//	肯定式判据会把没见过的成功写法误判成失败 —— 而误报会让这条提示很快被忽略。
func isFailedSyncResult(r string) bool {
	for _, kw := range []string{"失败", "错误", "error", "failed", "denied", "timeout", "超时"} {
		if strings.Contains(strings.ToLower(r), strings.ToLower(kw)) {
			return true
		}
	}
	return false
}
