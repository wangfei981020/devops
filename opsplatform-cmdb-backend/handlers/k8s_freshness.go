package handlers

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"opsplatform-cmdb-backend/k8ssource"
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

	out := make([]obsStackState, 0, len(eps))
	for _, e := range eps {
		st := obsStackState{Type: e.typ, Name: e.name}
		base, token, err := resolveEndpointByID(h.DB, h.Cipher, e.id)
		if err != nil {
			st.Detail = "取数据源配置失败: " + err.Error()
			out = append(out, st)
			continue
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
	         COALESCE(s.duration_ms,0), COALESCE(s.count,0)
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
		var cname, res, errMsg string
		var lastSync *time.Time
		var ok, durMs, cnt int
		if err := rows.Scan(&cid, &cname, &res, &lastSync, &ok, &errMsg, &durMs, &cnt); err != nil {
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
		r := syncResourceState{Resource: res, OK: ok == 1, Count: cnt, DurationMs: durMs, Err: errMsg}
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
		if st.Trustworthy {
			for _, o := range st.ObsStack {
				if !o.Healthy {
					st.Trustworthy = false
					st.Advice = "K8s 资源采集正常，但观测组件异常（" + o.Type + "：" + o.Detail +
						"）——依赖日志/指标的结论（如 pipeline_log、resource_waste、磁盘水位）此时不可信"
					break
				}
			}
		}
		out = append(out, *st)
	}
	c.JSON(http.StatusOK, gin.H{"checked_at": time.Now().Format("2006-01-02 15:04:05"), "clusters": out})
}

// summarizeFreshness 把逐资源状态收敛成一句「这份数据能不能信」。
// 只要有一类资源坏了就不能整体采信——AI 拿着半份数据下结论比没数据更危险。
func summarizeFreshness(rs []syncResourceState, staleAfter int) (overall string, trust bool, advice string) {
	if len(rs) == 0 {
		return "never", false, "该集群从未成功采集过，CMDB 里没有它的数据，不要基于此下任何结论"
	}
	var failed, stale []string
	for _, r := range rs {
		switch r.Freshness {
		case "failed":
			failed = append(failed, r.Resource)
		case "stale", "never":
			stale = append(stale, r.Resource)
		}
	}
	switch {
	case len(failed) > 0:
		return "failed", false,
			"这些资源最近一次采集失败: " + joinMax(failed, 8) + "；它们的数据是上一次成功采集的旧值，先查采集报错再下结论"
	case len(stale) > 0:
		return "stale", false,
			"这些资源超过 " + itoa(staleAfter) + " 秒未更新: " + joinMax(stale, 8) + "；数据可能已过时，建议先确认采集器是否在跑"
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
