package handlers

import (
	"context"
	"fmt"
	"net/http"
	"ops-cmdb-backend/internal/httpx"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"

	"ops-cmdb-backend/diag"
	"ops-cmdb-backend/logx"
)

// 批量自检：把整个集群的异常 Pod 跑一遍诊断，报「有几个真判出了根因、有几个只给了泛化结论」。
//
// # 为什么需要它
//
// 🔴 在这之前，"诊断到底覆盖了多少"只能靠人一个个点 diagnose_pod 抽样。
// 而抽样天然先命中高频形态：每修一轮，下一轮又冒出新的 miss，看不到头。
// 2026-08-19 连着四轮都是这样 —— 不是规则不行，是**没有能一次量出命中率的东西**。
//
// 有了它，「还差多少」从一个感觉变成一个数字：
//
//	扫了 84 个异常 Pod，其中 61 个判出了具体根因，23 个只给了泛化结论（附清单）
//
// 那 23 个就是下一轮要补的信号，**而且是按真实分布排的**，不是按我猜的顺序。
//
// # ⚠️ 这是个昂贵操作，必须显式调用
//
// 每个 Pod 都要打一次 APIServer（get pod + list events + read logs），
// 读日志还要经 APIServer 代理到节点 kubelet。所以：
//   - 默认上限 40 个，最大 100
//   - 并发 6，避免把 APIServer 打满
//   - 全局 4 分钟超时，到点就返回已完成的部分并说明**没扫完**
//
// 🔴 超时返回时必须说清"扫了几个/共几个"。不说的话，
// 一个被截断的 0 miss 会被读成"全都判出来了"。
const (
	sweepDefaultLimit = 30
	sweepMaxLimit     = 100
	sweepConcurrency  = 6
	// 🔴 预算必须**短于调用方的超时**，否则超时发生在调用方那一侧 ——
	//	结果是直接报错、一条数据都拿不到，而我们其实已经扫完了大半。
	//
	//	⚠️ 这里的"调用方"是 **ops-cmdb 自己**：MCP 工具的实现是进程内回调
	//	自己的 REST API（handlers/mcp.go 的 internalGet），那个 http.Client
	//	写死 30 秒。我一度以为是 Claude 侧的 MCP 代理，猜错了 ——
	//	两层超时都是自家的，只是没对齐。
	//
	//	现在 diagnose_sweep 在 mcp.go 的 slowTools 里，回调超时放宽到 120s，
	//	所以这里可以用 90s；到点就收工，把已扫的连同「没扫完」一起返回。
	//	下面的 init 会断言两者的关系，防止改了一边忘了另一边。
	sweepBudget = 90 * time.Second
)

// 🔴 两个超时必须成对维护，否则会退回"直接报错、什么都拿不到"那个状态。
//
// 这种跨文件的隐式约束最容易在改动中失效：改 sweepBudget 的人未必知道
// mcp.go 里还有一层，而失效的表现是**看起来像超时/网络问题**，
// 没人会想到是两个常量对不上。所以在启动时直接断言。
func init() {
	if sweepBudget >= slowToolTimeout {
		panic(fmt.Sprintf(
			"diagnose_sweep 的时间预算(%s)必须小于 MCP 慢工具回调超时(%s)——"+
				"否则接口还没来得及「扫到哪算哪地返回」就被上层掐断，调用方只会拿到一个纯错误。"+
				"改了 sweepBudget 就要同步 mcp.go 的 slowToolTimeout",
			sweepBudget, slowToolTimeout))
	}
}

type sweepItem struct {
	Namespace string `json:"namespace"`
	Pod       string `json:"pod"`
	Phase     string `json:"phase"`
	Restarts  int    `json:"restarts"`
	RootCause string `json:"root_cause"`
	Generic   bool   `json:"generic"`
	// Provider 这条根因是谁给的：rule / ai:<model>。
	// ⚠️ 必须逐条给出来 —— 一轮 sweep 里规则判的和 AI 判的混在一起时，
	//	两者的可信度完全不同，不标出来等于把"看起来像"当成"日志里写着"。
	Provider string `json:"provider,omitempty"`
	// AINote 为什么走了 / 没走 AI。没走时这里说清是哪一层已经有结论了。
	AINote string `json:"ai_note,omitempty"`
	Err    string `json:"error,omitempty"`
}

// DiagnoseSweep GET /api/k8s/diagnose-sweep?cluster_id=&limit=
func (h *K8sDiagHandler) DiagnoseSweep(c *gin.Context) {
	cid, _ := strconv.Atoi(c.Query("cluster_id"))
	if cid == 0 {
		httpx.Required(c, "cluster_id")
		return
	}
	limit := sweepDefaultLimit
	if v, err := strconv.Atoi(c.Query("limit")); err == nil && v > 0 {
		limit = v
	}
	if limit > sweepMaxLimit {
		limit = sweepMaxLimit
	}
	// offset 用来翻页扫完全部异常 Pod。
	// ⚠️ 没有它的话，无论 limit 给多大都只能看到「重启最多的那批」——
	//	而长尾（restarts=0 的 Pending 类）永远扫不到，命中率就只覆盖了头部。
	offset, _ := strconv.Atoi(c.Query("offset"))
	if offset < 0 {
		offset = 0
	}

	cs, err := h.Pool.ClientFor(cid)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}
	var clusterName string
	_ = h.DB.QueryRow(`SELECT COALESCE(display_name,name) FROM k8s_clusters WHERE id=?`, cid).Scan(&clusterName)

	// 异常 Pod 的口径与体检页一致：非 Running/Succeeded，或重启过百。
	// 重启高的按次数倒序 —— 烧得最久的最该先判出来。
	type target struct {
		ns, pod, phase string
		restarts       int
	}
	var total int
	_ = h.DB.QueryRow(`SELECT COUNT(*) FROM k8s_pods
		WHERE cluster_id=? AND (phase NOT IN ('Running','Succeeded') OR restarts > 100)`, cid).Scan(&total)

	rows, err := h.DB.Query(`SELECT namespace,name,phase,restarts FROM k8s_pods
		WHERE cluster_id=? AND (phase NOT IN ('Running','Succeeded') OR restarts > 100)
		ORDER BY restarts DESC, namespace, name LIMIT ? OFFSET ?`, cid, limit, offset)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	var targets []target
	for rows.Next() {
		var t target
		if rows.Scan(&t.ns, &t.pod, &t.phase, &t.restarts) == nil {
			targets = append(targets, t)
		}
	}
	rows.Close()

	ctx, cancel := context.WithTimeout(c.Request.Context(), sweepBudget)
	defer cancel()

	// AI 预算整轮共享一个。
	// ⚠️ 必须在扇出**之前**建，且整轮共用 —— 每个 Pod 各建一个的话
	//	上限就形同虚设（N 个 Pod × 每个 N 次）。
	//	AIBudget 内部有锁，可以在并发 goroutine 里直接用。
	aiCfg := loadAIConfig()
	budget := diag.NewAIBudget(aiCfg.MaxCallsPerRound, aiCfg.MaxCostPerRoundUSD)

	var (
		mu    sync.Mutex
		items []sweepItem
		wg    sync.WaitGroup
		sem   = make(chan struct{}, sweepConcurrency)
	)
	for _, t := range targets {
		if ctx.Err() != nil {
			break // 预算用完，剩下的不扫了（下面会说清楚扫了几个）
		}
		wg.Add(1)
		sem <- struct{}{}
		go func(t target) {
			defer wg.Done()
			defer func() { <-sem }()
			it := sweepItem{Namespace: t.ns, Pod: t.pod, Phase: t.phase, Restarts: t.restarts}
			// 单个 Pod 自己的超时，避免一个卡死拖垮整轮
			pctx, pcancel := context.WithTimeout(ctx, 25*time.Second)
			defer pcancel()
			dc, err := diag.Collect(pctx, cs, clusterName, t.ns, t.pod)
			if err != nil {
				// 🔴 采集失败**不能**算成"判出来了"，也不能算成泛化 ——
				//	它是第三种状态：没数据。混进任何一边都会让命中率失真
				it.Err = err.Error()
			} else {
				res := diag.RuleProvider{}.Diagnose(dc)
				// 🔴 批量场景才是成本真正会失控的地方：一轮扫几百个 Pod，
				//	没有闸门就是几百次调用。budget 在整轮共享（见下方创建处），
				//	用满就停 —— 并且**跳过数会出现在结果里**，
				//	不能让人以为"这一轮全看过了"。
				h.maybeAI(pctx, cid, dc, res, budget, false) // ⚠️ 批量扫描永不强制：一次 sweep 上百个 Pod，强制=账单翻倍
				it.RootCause, it.Generic = res.RootCause, res.Generic
				it.Provider = res.Provider
				if res.AIGate != nil {
					it.AINote = res.AIGate.Reason
				}
			}
			mu.Lock()
			items = append(items, it)
			mu.Unlock()
		}(t)
	}
	wg.Wait()

	// AI 账单单独报。⚠️ skipped>0 时必须显示 ——
	//	静默截断的结果是「扫完了，这些就是全部问题」，而那是假的。
	aiCalls, aiCost, aiSkipped, aiNote := budget.Summary()

	sort.SliceStable(items, func(i, j int) bool { return items[i].Restarts > items[j].Restarts })

	var specific, generic, failed int
	byCause := map[string]int{}
	unresolved := []sweepItem{}
	for _, it := range items {
		switch {
		case it.Err != "":
			failed++
		case it.Generic:
			generic++
			unresolved = append(unresolved, it)
		default:
			specific++
			byCause[it.RootCause]++
		}
	}

	// 🔴 未判出的那批要**放在一起看**，逐条列出来会漏掉最有用的信息。
	//
	// 实测 DEV 全量扫完：22 个未判出里 **15 个挤在 metersphere2 一个命名空间**，
	// 而且形态完全相同（镜像拉不下来 + 事件已过期）。
	// 逐条读的话，那是 15 个互不相干的谜；放在一起看，它是**一个**问题。
	//
	// 而且这时有一条只在整体视角下才成立的建议：
	// 与其逐个查，不如删掉其中一个 Pod 让它重建 —— 新的失败事件会立刻写出来，
	// 一次就能给整批定性。
	unresolvedPatterns := []gin.H{}
	if len(unresolved) > 0 {
		rows := make([][]any, 0, len(unresolved))
		for _, u := range unresolved {
			rows = append(rows, []any{u.Namespace, u.Pod, u.RootCause})
		}
		unresolvedPatterns = concentrations([]string{"命名空间", "Pod", "原因"}, rows)

		// 「事件已过期」这一类有专属的破局办法，值得单独说
		expired := 0
		for _, u := range unresolved {
			if strings.Contains(u.RootCause, "事件已过期") {
				expired++
			}
		}
		if expired >= 3 {
			unresolvedPatterns = append(unresolvedPatterns, gin.H{
				"column": "原因", "value": "事件已过期", "count": expired, "total": len(unresolved),
				"hint": strconv.Itoa(expired) + " 个未判出都是「镜像拉不下来但事件已过期」。" +
					"K8s 事件默认只留 1 小时，这些 Pod 挂了很久，原始报错查不到了 —— **不是没有报错**。" +
					"破局办法：删掉其中**一个** Pod 让它重建，新的失败事件会立刻写出来，一次就能给整批定性" +
					"（⚠️ 先确认这些 Pod 可以重建）",
			})
		}
	}

	resp := gin.H{
		"cluster_id": cid,
		"bad_pods":   total,
		"offset":     offset,
		"scanned":    len(items),
		"summary": gin.H{
			"specific": specific,
			"generic":  generic,
			"failed":   failed,
		},
		// 只给泛化结论的那批就是下一轮要补的信号，按重启次数倒序
		"unresolved": unresolved,
		"by_cause":   byCause,
	}
	// AI 账单。⚠️ 即使一次都没调也要给这一栏 ——
	//	省略的话人无从判断"这轮到底有没有花钱"，
	//	而"我以为没开"是最容易发生的误会。
	resp["ai"] = gin.H{
		"calls": aiCalls, "cost_usd": aiCost, "skipped": aiSkipped,
	}
	if aiSkipped > 0 {
		// 🔴 硬上限触发时必须显式说明还剩多少没诊断。
		//	静默截断 = 「扫完了，这些就是全部问题」，而那是假的。
		resp["ai"].(gin.H)["note"] = aiNote +
			"。这 " + strconv.Itoa(aiSkipped) + " 个对象**没有经过 AI 诊断**，" +
			"它们的结论只来自规则层。要继续的话调大 OPS_AI_MAX_CALLS / OPS_AI_MAX_COST_USD 后重跑。"
	}
	if len(unresolvedPatterns) > 0 {
		resp["unresolved_patterns"] = unresolvedPatterns
	}
	if specific+generic > 0 {
		resp["hit_rate"] = strconv.Itoa(specific*100/(specific+generic)) + "%"
	}
	// ⚠️ 没扫全时必须说出来：被截断的"0 个未判出"会被读成"全判出来了"
	if offset+len(items) < total {
		why := "受 limit 限制"
		if len(items) < len(targets) {
			// 🔴 这两种「没扫完」必须分开说：
			//	被 limit 截断 → 调大 limit 或翻页就行
			//	预算到点     → 说明单个 Pod 很慢（多为读日志卡在 kubelet），
			//	               调大 limit 只会更早到点，要改的是分页步长
			why = "**时间预算到点**（本次只完成 " + strconv.Itoa(len(items)) + " / " +
				strconv.Itoa(len(targets)) + " 个，多为读日志慢）"
		}
		resp["not_all_scanned"] = "已扫 " + strconv.Itoa(offset+len(items)) + " / " + strconv.Itoa(total) +
			" 个异常 Pod（" + why + "）。上面的命中率**只覆盖已扫的这批**，不是全量。" +
			"继续扫下一页：offset=" + strconv.Itoa(offset+len(items)) + "&limit=" + strconv.Itoa(limit)
	}
	if failed > 0 {
		resp["collect_failed_note"] = strconv.Itoa(failed) + " 个 Pod 采集失败（多为已被重建或读日志无权限）——" +
			"它们既没算进命中也没算进未判出，避免让命中率失真"
	}
	logx.J("k8s", "diagnose_sweep", map[string]any{
		"cluster_id": cid, "scanned": len(items), "specific": specific, "generic": generic, "failed": failed,
	})
	c.JSON(http.StatusOK, resp)
}
