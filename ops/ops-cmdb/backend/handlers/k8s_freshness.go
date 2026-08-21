package handlers

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"ops-cmdb-backend/k8ssource"
)

// K8s 数据是周期性全量采集的快照。AI 只连 CMDB 排障时必须能判断「手上这份数据是不是新的」，
// 否则会拿着几小时前的快照下结论，还以为是现状。SyncState 把 k8s_sync_state 翻译成可直接采信的判定。

// staleFactor：距上次成功同步超过「同步周期 × 此倍数」即判为 stale。
// 取 3 是为了容忍偶发一两轮抖动（APIServer 慢、网络抖），超过就说明同步是真的出问题了。
const staleFactor = 3

type syncResourceState struct {
	Resource   string `json:"resource"`
	LastSync   string `json:"last_sync"`
	AgeSec     int64  `json:"age_sec"`
	OK         bool   `json:"ok"`
	Count      int    `json:"count"`
	DurationMs int    `json:"duration_ms"`
	Freshness  string `json:"freshness"` // fresh | stale | failed | never
	Err        string `json:"err,omitempty"`
	// SkipNote 这一类**有一部分没采到**的原因（CRD 没装、没权限、开关没开）。
	//
	//	⚠️ 它和 Err 不是一回事：Err 非空 = 这一轮失败了；
	//	SkipNote 非空 = 采集没失败，但少了一整块，**而 count 看着是正常的**。
	//	实测某集群 `gateways: ok=true, count=6`（6 个全是 Istio 的），
	//	Gateway API 那一套一个没采到 —— 采集自称成功，
	//	所以这个缺失躲过了所有新鲜度检查（OPSCMDB-031 P0-11）。
	SkipNote string `json:"skip_note,omitempty"`
}

type clusterSyncState struct {
	ClusterID   int                 `json:"cluster_id"`
	ClusterName string              `json:"cluster_name"`
	Overall     string              `json:"overall"` // fresh | stale | failed | never
	Trustworthy bool                `json:"trustworthy"`
	Advice      string              `json:"advice"`
	IntervalSec int                 `json:"interval_sec"`
	StaleAfter  int                 `json:"stale_after_sec"`
	Resources   []syncResourceState `json:"resources"`
	ObsStack    []obsStackState     `json:"obs_stack,omitempty"`
}

// obsStackState 观测组件自身的健康。
//
// K8s 资源采集正常 ≠ 数据可信：日志类结论依赖 Loki、用量类结论依赖 Prometheus。
// 这两个组件自己挂了的时候，CMDB 此前毫无察觉——Loki 已经被 OOMKilled，
// data_freshness 还在回「全部资源采集正常，数据可直接采信」。
// 监控自己瞎了却报告自己好着，是最危险的一种失效。
type obsStackState struct {
	Type    string `json:"type"` // prometheus | loki
	Name    string `json:"name"`
	Healthy bool   `json:"healthy"`
	Detail  string `json:"detail"`
	// Selected 查询本集群时**实际会用**的就是这一条。
	//
	// 🔴 为什么必须标出来：不限集群的数据源（cluster_id=0）对每个集群都会列出来，
	// 于是查 DEV 时四条全出现（uat-loki / dev-loki / infra-vm / dev-prometheus）。
	// 不标的话有两种误读：以为 DEV 的指标来自 infra-vm；或某条坏了时以为影响本集群。
	Selected bool `json:"selected"`
	// SelectedNote 说明为什么是它 —— 只给结论不给依据，下次换了人还得重新推一遍
	SelectedNote string `json:"selected_note,omitempty"`
}

// obsProbePath 各类观测源的探活路径。取一个「有数据才会 200」的轻量端点，
// 不用根路径——根路径 200 只能证明进程还在，证明不了它还能回答查询。
var obsProbePath = map[string]string{
	"prometheus": "/api/v1/query?query=up",
	"loki":       "/loki/api/v1/labels",
}

// checkObsStack 探一遍该集群用到的观测数据源是否真的能回答查询。
func (h *K8sResourceHandler) checkObsStack(cid int) []obsStackState {
	rows, err := h.DB.Query(`SELECT id, name, type FROM obs_endpoints
		WHERE enabled=1 AND type IN ('prometheus','loki')
		  AND (cluster_id=? OR cluster_id=0 OR cluster_id IS NULL)`, cid)
	if err != nil {
		return nil
	}
	defer rows.Close()
	type ep struct {
		id        int
		name, typ string
	}
	eps := []ep{}
	for rows.Next() {
		var e ep
		if rows.Scan(&e.id, &e.name, &e.typ) == nil {
			eps = append(eps, e)
		}
	}

	// 先问一次解析器：本集群查 prometheus / loki 时实际会落到哪个地址。
	// ⚠️ 必须复用同一个解析函数，自己再实现一套选取规则的话，
	// 这里标的「选中」和真实查询用的可能是两条不同的源 —— 那比不标更糟
	chosen := map[string]string{}
	for _, typ := range []string{"prometheus", "loki"} {
		if base, _, _, err := resolveEndpointFull(h.DB, h.Cipher, typ, clusterEnvOf(h.DB, cid), cid); err == nil {
			chosen[typ] = strings.TrimRight(base, "/")
		}
	}

	out := make([]obsStackState, 0, len(eps))
	for _, e := range eps {
		st := obsStackState{Type: e.typ, Name: e.name}
		base, token, err := resolveEndpointByID(h.DB, h.Cipher, e.id)
		if err != nil {
			st.Detail = "取数据源配置失败: " + err.Error()
			out = append(out, st)
			continue
		}
		if strings.TrimRight(base, "/") == chosen[e.typ] {
			st.Selected = true
			st.SelectedNote = "本集群查 " + e.typ + " 时用的就是这条"
		} else {
			st.SelectedNote = "本集群不会用它（按环境/集群匹配后选中的是另一条）"
		}
		code, _, err := obsGet(base+obsProbePath[e.typ], token, 8*time.Second)
		switch {
		case err != nil:
			st.Detail = "连不上: " + err.Error()
		case code != 200:
			st.Detail = fmt.Sprintf("探活返回 HTTP %d（进程可能在跑，但已无法回答查询）", code)
		default:
			st.Healthy, st.Detail = true, "正常"
		}
		out = append(out, st)
	}
	return out
}

// SyncState 报告各集群每类资源的采集新鲜度。cluster_id 可选，不传则返回全部启用集群。
func (h *K8sResourceHandler) SyncState(c *gin.Context) {
	interval := k8ssource.DefaultSyncIntervalSec
	staleAfter := interval * staleFactor

	q := `SELECT c.id, c.name, COALESCE(s.resource,''), s.last_sync, COALESCE(s.ok,0), COALESCE(s.err,''),
	         COALESCE(s.duration_ms,0), COALESCE(s.count,0), COALESCE(s.skip_note,'')
	      FROM k8s_clusters c LEFT JOIN k8s_sync_state s ON s.cluster_id=c.id
	      WHERE c.enabled=1`
	args := []any{}
	if cid := c.Query("cluster_id"); cid != "" {
		q += " AND c.id=?"
		args = append(args, cid)
	}
	q += " ORDER BY c.id, s.resource"

	rows, err := h.DB.Query(q, args...)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	defer rows.Close()

	byCluster := map[int]*clusterSyncState{}
	order := []int{}
	for rows.Next() {
		var cid int
		var cname, res, errMsg, skipNote string
		var lastSync *time.Time
		var ok, durMs, cnt int
		if err := rows.Scan(&cid, &cname, &res, &lastSync, &ok, &errMsg, &durMs, &cnt, &skipNote); err != nil {
			continue
		}
		st, seen := byCluster[cid]
		if !seen {
			st = &clusterSyncState{
				ClusterID: cid, ClusterName: cname,
				IntervalSec: interval, StaleAfter: staleAfter,
			}
			byCluster[cid] = st
			order = append(order, cid)
		}
		if res == "" { // 集群从未同步过（LEFT JOIN 无匹配行）
			continue
		}
		r := syncResourceState{Resource: res, OK: ok == 1, Count: cnt, DurationMs: durMs, Err: errMsg,
			SkipNote: skipNote}
		switch {
		case lastSync == nil:
			r.Freshness, r.AgeSec = "never", -1
		default:
			r.LastSync = lastSync.Format("2006-01-02 15:04:05")
			r.AgeSec = int64(time.Since(*lastSync).Seconds())
			switch {
			case ok != 1:
				r.Freshness = "failed"
			case r.AgeSec > int64(staleAfter):
				r.Freshness = "stale"
			default:
				r.Freshness = "fresh"
			}
		}
		st.Resources = append(st.Resources, r)
	}

	out := make([]clusterSyncState, 0, len(order))
	for _, cid := range order {
		st := byCluster[cid]
		st.Overall, st.Trustworthy, st.Advice = summarizeFreshness(st.Resources, staleAfter)
		st.ObsStack = h.checkObsStack(cid)
		// 采集本身没问题，但底层观测组件挂了的话，基于日志/指标的结论一样不可信。
		// 之前 Loki 已经 OOMKilled 了，这里还在报「数据可直接采信」——监控自己瞎了却说自己好着。
		//
		// 🔴 只看**本集群实际会用到**的那几条（Selected）。
		//
		//	原来是任意一条不健康就把整个集群判成不可信 —— 而这份清单里
		//	还包含 cluster_id=0 的全局数据源和别的集群的源。
		//	实测：08:46 查 UAT 时 dev-loki 恰好在抖，于是 UAT 被判成
		//	「数据不可信」，而 UAT 自己的 loki 好好的（OPSCMDB-031 P2-1）。
		//
		//	别的集群的数据源抖动能把本集群判成不可信 —— 这不是保守，
		//	是**误报**，而误报多了 trustworthy 这个字段就没人看了。
		if st.Trustworthy {
			for _, o := range st.ObsStack {
				if o.Healthy || !o.Selected {
					continue
				}
				st.Trustworthy = false
				st.Advice = "K8s 资源采集正常，但本集群实际使用的观测组件异常（" +
					o.Type + " · " + o.Name + "：" + o.Detail +
					"）——依赖日志/指标的结论（如 pipeline_log、resource_waste、磁盘水位）此时不可信"
				break
			}
		}
		// ⚠️ 一条都没选中也是个问题，而且是更严重的那种：
		//	不是"用的那条坏了"，是**根本没有可用的数据源**。
		//	原来这种情况下 trustworthy 保持 true —— 因为循环里一条不健康的都没有
		//	（清单里可能全是别的集群的源，它们都很健康）。
		if st.Trustworthy {
			if note := missingObsNote(st.ObsStack); note != "" {
				st.Trustworthy = false
				st.Advice = note
			}
		}
		out = append(out, *st)
	}
	c.JSON(http.StatusOK, gin.H{"checked_at": time.Now().Format("2006-01-02 15:04:05"), "clusters": out})
}

// missingObsNote 本集群有没有选中可用的 prometheus / loki。
//
//	⚠️ "清单里有健康的数据源"不等于"本集群用得上它" ——
//	清单里可能全是别的集群的源。判据必须是 Selected。
func missingObsNote(stack []obsStackState) string {
	has := map[string]bool{}
	for _, o := range stack {
		if o.Selected {
			has[o.Type] = true
		}
	}
	missing := []string{}
	for _, typ := range []string{"prometheus", "loki"} {
		if !has[typ] {
			missing = append(missing, typ)
		}
	}
	if len(missing) == 0 {
		return ""
	}
	return "K8s 资源采集正常，但本集群没有匹配到可用的 " + strings.Join(missing, " / ") +
		" 数据源——依赖日志/指标的结论此时无从得出（不是「没问题」，是查不了）。" +
		"去「观测端点」页确认是否配了本集群或本环境的源"
}

// summarizeFreshness 把逐资源状态收敛成一句「这份数据能不能信」。
// 只要有一类资源坏了就不能整体采信——AI 拿着半份数据下结论比没数据更危险。
func summarizeFreshness(rs []syncResourceState, staleAfter int) (overall string, trust bool, advice string) {
	if len(rs) == 0 {
		return "never", false, "该集群从未成功采集过，CMDB 里没有它的数据，不要基于此下任何结论"
	}
	var failed, stale, skipped []string
	for _, r := range rs {
		switch r.Freshness {
		case "failed":
			failed = append(failed, r.Resource)
		case "stale", "never":
			stale = append(stale, r.Resource)
		}
		// ⚠️ 部分跳过的也不能算"可直接采信"。
		//
		//	这一类的 freshness 是 fresh、ok 是 true、count 也有数字 ——
		//	所有指标都正常，唯独少了一整块数据。
		//	原来它会走进 default 分支，得到"全部资源采集正常且在新鲜期内，
		//	数据可直接采信"这句断言，而那句话是错的（P0-11）。
		if r.SkipNote != "" {
			skipped = append(skipped, r.Resource)
		}
	}
	switch {
	case len(failed) > 0:
		return "failed", false,
			"这些资源最近一次采集失败: " + joinMax(failed, 8) + "；它们的数据是上一次成功采集的旧值，先查采集报错再下结论"
	case len(stale) > 0:
		return "stale", false,
			"这些资源超过 " + itoa(staleAfter) + " 秒未更新: " + joinMax(stale, 8) + "；数据可能已过时，建议先确认采集器是否在跑"
	case len(skipped) > 0:
		// 单独一档 partial：既不是 fresh（有缺口）也不是 failed（采集没失败）。
		// 压成 fresh 会掩盖缺口，压成 failed 会让人去查一个不存在的报错
		return "partial", false,
			"这些资源只采到了一部分: " + joinMax(skipped, 8) +
				"；采集本身没有失败，但有整类对象没被采到（CRD 未安装 / 缺权限 / 开关未开），" +
				"逐项原因见 skip_note。基于这几类下结论前先把缺口补上"
	default:
		return "fresh", true, "全部资源采集正常且在新鲜期内，数据可直接采信"
	}
}

// joinMax 拼接资源名，超过 n 个只列前 n 个并标注剩余数量，避免 advice 变成一长串。
func joinMax(ss []string, n int) string {
	if len(ss) <= n {
		return strings.Join(ss, ", ")
	}
	return strings.Join(ss[:n], ", ") + " 等 " + itoa(len(ss)) + " 类"
}
