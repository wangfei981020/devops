package api

import (
	"context"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"ops-alert-backend/datasource"
)

// 日志检索。
//
// # 它存在的理由只有一个：回答"为什么没告警"
//
// 告警系统最贵的一类问题不是误报，而是**该报没报**。
// 排查它需要回到原始日志：那条日志到底进没进来、长什么样、
// 规则的查询语句写的和实际数据对不对得上。
//
// 没有这个页面的话，人得离开告警系统去 Grafana / Kibana 查一遍，
// 而那边连的数据源、时间窗、查询语法可能都和规则里配的不一样 ——
// 于是"我在 Kibana 里能查到啊"和"规则确实没命中"同时成立，谁也说服不了谁。
// 这个页面用的是**规则用的那条链路**，查出来的就是规则看到的。

type exploreReq struct {
	DatasourceID int64  `json:"datasource_id"`
	Expr         string `json:"expr"`
	RangeSec     int    `json:"range_sec"`
	Limit        int    `json:"limit"`
}

func (s *Server) exploreLogs(c *gin.Context) {
	sc, err := s.st.Tenant(c.Request.Context())
	if err != nil {
		abortTenant(c, err)
		return
	}
	var req exploreReq
	if err := c.ShouldBindJSON(&req); err != nil || req.DatasourceID == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "bad_request"})
		return
	}
	if req.RangeSec <= 0 {
		req.RangeSec = 900
	}
	// 上限卡死在服务端。前端传 100000 的话，一次检索能把后端内存打满，
	// 而检索是任何登录用户都能调的接口。
	if req.Limit <= 0 || req.Limit > 500 {
		req.Limit = 200
	}

	ad, kind, err := s.eng.OpenDatasource(sc, req.DatasourceID)
	if err != nil {
		// ⚠️ 数据源打不开要说清楚是**数据源**的问题，不能返回空结果。
		// 空结果在检索页上读作"这段时间没有日志"，
		// 于是排查的人会得出"日志确实没进来"的**相反结论**。
		c.JSON(http.StatusBadGateway, gin.H{
			"error": "datasource_unavailable", "detail": err.Error()})
		return
	}

	to := time.Now()
	from := to.Add(-time.Duration(req.RangeSec) * time.Second)
	// 超时挂在请求上下文上：客户端断开时查询要跟着取消，
	// 否则一个不停刷新的页面会在数据源那边堆积几十个僵尸查询
	ctx, cancel := context.WithTimeout(c.Request.Context(), 30*time.Second)
	defer cancel()
	res, err := ad.Query(ctx, datasource.Query{
		Expr: req.Expr, From: from, To: to, Limit: req.Limit,
	})
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{
			"error": "query_failed", "detail": err.Error(), "kind": kind})
		return
	}

	hits := make([]gin.H, 0, len(res.Hits))
	for _, h := range res.Hits {
		hits = append(hits, gin.H{
			"time": h.Time, "line": h.Line, "labels": h.Labels, "fields": h.Fields,
		})
	}
	c.JSON(http.StatusOK, gin.H{
		"hits": hits, "total": res.Total, "took_ms": res.TookMs,
		// ⚠️ truncated 必须传给前端并显示出来。
		// 截断后的 200 条看起来就是"一共 200 条"，而真相可能是几万条 ——
		// 拿它去估算日志量会差一个数量级。
		"truncated": res.Truncated,
		"kind":      kind,
		"from":      from, "to": to,
	})
}

// ⚠️ 错误响应的可读原因字段是 **detail**，不是 message。
//
// 前端的 api() 封装只读 `error`（机器码）和 `detail`（给人看的原因）。
// 写成 message 的话字段照样发出去了、接口测试也照样通过，
// 但界面上只剩一个 "query_failed" —— 排查的人拿它什么也做不了，
// 而真正有用的"connection refused 到 127.0.0.1:3100"被静默丢在响应体里。
// 本文件曾经就是这样。
