package handlers

import (
	"database/sql"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"opsplatform-cmdb-backend/logx"
)

// snapRow 快照粒度：工作负载/主机/PVC。
type snapRow struct {
	cluster, mode, gcp, biz, env, typ, key, spec string
	cost                                         float64
}

// snapshotRows 把当前成本聚合到稳定粒度（Pod→工作负载），并附规格摘要（归因用）。
func (h *K8sCostHandler) snapshotRows() []snapRow {
	// 工作负载规格：cluster|ns|name → "Nx :tag"
	wlSpec := map[string]string{}
	if rows, _ := h.DB.Query(`SELECT cluster_id,namespace,name,replicas_desired,image_tag FROM k8s_workloads`); rows != nil {
		for rows.Next() {
			var cid, rep int
			var ns, name, tag string
			if rows.Scan(&cid, &ns, &name, &rep, &tag) == nil {
				wlSpec[strconv.Itoa(cid)+"|"+ns+"|"+name] = fmt.Sprintf("%d副本 :%s", rep, tag)
			}
		}
		rows.Close()
	}
	// 主机机型：name → machine_type
	hostSpec := map[string]string{}
	if rows, _ := h.DB.Query(`SELECT c.name, h.machine_type FROM cis c JOIN hosts h ON h.ci_id=c.id WHERE c.type='host'`); rows != nil {
		for rows.Next() {
			var name, mt string
			if rows.Scan(&name, &mt) == nil {
				hostSpec[name] = mt
			}
		}
		rows.Close()
	}
	// 需要 cluster_id 还原工作负载 spec key：buildItems 里没带 cluster_id 的原始 id... 用 clusters name→id 反查
	nameToID := map[string]int{}
	for id, ci := range h.clusters() {
		nameToID[ci.name] = id
	}
	agg := map[string]*snapRow{}
	for _, it := range h.buildItems() {
		rk := it.Namespace + "/" + it.Name
		k := it.Cluster + "|" + it.Type + "|" + rk
		r := agg[k]
		if r == nil {
			r = &snapRow{cluster: it.Cluster, mode: it.Mode, gcp: it.GcpProject, biz: it.BizProject, env: it.Env, typ: it.Type, key: rk}
			// 规格
			switch it.Type {
			case "k8s_compute":
				r.spec = wlSpec[strconv.Itoa(nameToID[it.Cluster])+"|"+it.Namespace+"|"+it.Name]
			case "traditional":
				r.spec = hostSpec[it.Name]
			}
			agg[k] = r
		}
		r.cost += it.Cost
	}
	out := make([]snapRow, 0, len(agg))
	for _, r := range agg {
		r.cost = round2(r.cost)
		out = append(out, *r)
	}
	return out
}

// takeSnapshot 计算并写入指定月份快照（全量替换该月）。
func (h *K8sCostHandler) takeSnapshot(month string) (int, error) {
	rows := h.snapshotRows()
	if _, err := h.DB.Exec(`DELETE FROM cost_snapshots WHERE month=?`, month); err != nil {
		return 0, err
	}
	for _, r := range rows {
		_, _ = h.DB.Exec(`INSERT INTO cost_snapshots (month,cluster,mode,gcp_project,biz_project,env,type,resource_key,spec,cost)
			VALUES (?,?,?,?,?,?,?,?,?,?)`,
			month, r.cluster, r.mode, r.gcp, r.biz, r.env, r.typ, r.key, r.spec, r.cost)
	}
	return len(rows), nil
}

func (h *K8sCostHandler) SnapshotNow(c *gin.Context) {
	month := c.Query("month")
	if month == "" {
		month = time.Now().Format("2006-01")
	}
	n, err := h.takeSnapshot(month)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	SetAuditTarget(c, month)
	c.JSON(http.StatusOK, gin.H{"ok": true, "month": month, "rows": n})
}

func (h *K8sCostHandler) Months(c *gin.Context) {
	rows, _ := h.DB.Query(`SELECT DISTINCT month FROM cost_snapshots ORDER BY month DESC`)
	out := []string{}
	if rows != nil {
		for rows.Next() {
			var m string
			if rows.Scan(&m) == nil {
				out = append(out, m)
			}
		}
		rows.Close()
	}
	c.JSON(http.StatusOK, out)
}

func prevMonth(m string) string {
	t, err := time.Parse("2006-01", m)
	if err != nil {
		return ""
	}
	return t.AddDate(0, -1, 0).Format("2006-01")
}

// monthsOfPeriod 返回周期覆盖的月份列表（含 anchor）。
func monthsOfPeriod(period, anchor string) []string {
	t, err := time.Parse("2006-01", anchor)
	if err != nil {
		return []string{anchor}
	}
	switch period {
	case "quarter":
		q := (int(t.Month()) - 1) / 3
		start := time.Date(t.Year(), time.Month(q*3+1), 1, 0, 0, 0, 0, time.UTC)
		return []string{start.Format("2006-01"), start.AddDate(0, 1, 0).Format("2006-01"), start.AddDate(0, 2, 0).Format("2006-01")}
	case "year":
		out := []string{}
		for i := 1; i <= 12; i++ {
			out = append(out, fmt.Sprintf("%d-%02d", t.Year(), i))
		}
		return out
	default:
		return []string{anchor}
	}
}

func (h *K8sCostHandler) monthTotal(month string) float64 {
	var t sql.NullFloat64
	h.DB.QueryRow(`SELECT SUM(cost) FROM cost_snapshots WHERE month=?`, month).Scan(&t)
	return round2(t.Float64)
}

// Report 月/季/年报告：周期总额 + 环比(vs 上一周期) + 按维度 + 12月趋势。
func (h *K8sCostHandler) Report(c *gin.Context) {
	period := c.Query("period")
	if period == "" {
		period = "month"
	}
	anchor := c.Query("anchor")
	if anchor == "" {
		anchor = time.Now().Format("2006-01")
	}
	months := monthsOfPeriod(period, anchor)
	inClause := ""
	args := []any{}
	for i, m := range months {
		if i > 0 {
			inClause += ","
		}
		inClause += "?"
		args = append(args, m)
	}
	var total float64
	dim := c.Query("dim")
	if dim == "" {
		dim = "biz_project"
	}
	col := map[string]string{"biz_project": "biz_project", "gcp_project": "gcp_project", "cluster": "cluster", "env": "env", "type": "type"}[dim]
	if col == "" {
		col = "biz_project"
	}
	groups := []gin.H{}
	if rows, _ := h.DB.Query(`SELECT `+col+`, SUM(cost) FROM cost_snapshots WHERE month IN (`+inClause+`) GROUP BY `+col+` ORDER BY SUM(cost) DESC`, args...); rows != nil {
		for rows.Next() {
			var name string
			var cost float64
			if rows.Scan(&name, &cost) == nil {
				groups = append(groups, gin.H{"name": name, "cost": round2(cost)})
				total += cost
			}
		}
		rows.Close()
	}
	// 环比：与上一同长度周期比（简化：月→上月；季→上季首月锚；年→去年）
	var prevTotal float64
	if period == "month" {
		prevTotal = h.monthTotal(prevMonth(anchor))
	} else {
		// 上一周期锚 = anchor 往前一个周期长度
		t, _ := time.Parse("2006-01", anchor)
		var pa string
		if period == "quarter" {
			pa = t.AddDate(0, -3, 0).Format("2006-01")
		} else {
			pa = t.AddDate(-1, 0, 0).Format("2006-01")
		}
		for _, m := range monthsOfPeriod(period, pa) {
			prevTotal += h.monthTotal(m)
		}
	}
	// 12 月趋势
	trend := []gin.H{}
	tt, _ := time.Parse("2006-01", anchor)
	for i := 11; i >= 0; i-- {
		m := tt.AddDate(0, -i, 0).Format("2006-01")
		trend = append(trend, gin.H{"month": m, "cost": h.monthTotal(m)})
	}
	c.JSON(http.StatusOK, gin.H{
		"period": period, "anchor": anchor, "months": months, "dim": dim,
		"total": round2(total), "prev_total": round2(prevTotal), "delta": round2(total - prevTotal),
		"groups": groups, "trend": trend,
	})
}

// Attribution 环比归因：month vs 上月，逐资源列出 新增/移除/涨跌，按 |delta| 排序。
func (h *K8sCostHandler) Attribution(c *gin.Context) {
	month := c.Query("month")
	if month == "" {
		month = time.Now().Format("2006-01")
	}
	prev := prevMonth(month)
	load := func(m string) map[string]snapRow {
		out := map[string]snapRow{}
		rows, _ := h.DB.Query(`SELECT cluster,type,resource_key,biz_project,spec,cost FROM cost_snapshots WHERE month=?`, m)
		if rows != nil {
			for rows.Next() {
				var r snapRow
				if rows.Scan(&r.cluster, &r.typ, &r.key, &r.biz, &r.spec, &r.cost) == nil {
					out[r.cluster+"|"+r.typ+"|"+r.key] = r
				}
			}
			rows.Close()
		}
		return out
	}
	cur, old := load(month), load(prev)
	movers := []gin.H{}
	seen := map[string]bool{}
	for k, cr := range cur {
		seen[k] = true
		if pr, ok := old[k]; ok {
			d := cr.cost - pr.cost
			if d > 0.009 || d < -0.009 {
				reason := "成本变化"
				if cr.spec != pr.spec && pr.spec != "" {
					reason = "规格变化 " + pr.spec + "→" + cr.spec
				}
				movers = append(movers, gin.H{"resource": cr.key, "cluster": cr.cluster, "type": cr.typ, "project": cr.biz,
					"old": round2(pr.cost), "new": round2(cr.cost), "delta": round2(d), "reason": reason})
			}
		} else {
			movers = append(movers, gin.H{"resource": cr.key, "cluster": cr.cluster, "type": cr.typ, "project": cr.biz,
				"old": 0.0, "new": round2(cr.cost), "delta": round2(cr.cost), "reason": "新增 " + cr.spec})
		}
	}
	for k, pr := range old {
		if !seen[k] {
			movers = append(movers, gin.H{"resource": pr.key, "cluster": pr.cluster, "type": pr.typ, "project": pr.biz,
				"old": round2(pr.cost), "new": 0.0, "delta": round2(-pr.cost), "reason": "移除"})
		}
	}
	// 按 |delta| 降序
	for i := 0; i < len(movers); i++ {
		for j := i + 1; j < len(movers); j++ {
			if abs(movers[j]["delta"].(float64)) > abs(movers[i]["delta"].(float64)) {
				movers[i], movers[j] = movers[j], movers[i]
			}
		}
	}
	c.JSON(http.StatusOK, gin.H{"month": month, "prev": prev,
		"total": h.monthTotal(month), "prev_total": h.monthTotal(prev),
		"delta": round2(h.monthTotal(month) - h.monthTotal(prev)), "movers": movers})
}

func abs(f float64) float64 {
	if f < 0 {
		return -f
	}
	return f
}

// StartCostSnapshotScheduler 每天刷新当月快照（当月始终反映最新估算；跨月后上月自动定格）。
func StartCostSnapshotScheduler(db *sql.DB) {
	h := &K8sCostHandler{DB: db}
	for {
		time.Sleep(6 * time.Hour)
		m := time.Now().Format("2006-01")
		if n, err := h.takeSnapshot(m); err != nil {
			logx.J("cost_snap", "err", map[string]any{"month": m, "err": err.Error()})
		} else {
			logx.J("cost_snap", "ok", map[string]any{"month": m, "rows": n})
		}
	}
}
