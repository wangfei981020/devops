package api

import (
	"fmt"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
)

// 告警数量时序。值班台顶部那张图用它。
//
// # 为什么按「事件创建时刻」而不是「当前未恢复数」
//
// 未恢复数是一个瞬时快照，画成曲线会得到一条几乎不动的线 ——
// 一条 3 天没恢复的告警会把整段曲线抬高，而真正要看的是**新增速率**：
// 什么时候开始出问题、有没有随发布跳变。
//
// # 为什么不做成通用的聚合接口
//
// 时间桶的对齐、空桶补零、时区这些一旦做成参数化，
// 调用方各传各的就会出现"两张图对不上"。这里把口径钉死在服务端。
func (s *Server) incidentSeries(c *gin.Context) {
	sc, err := s.st.Tenant(c.Request.Context())
	if err != nil {
		abortTenant(c, err)
		return
	}
	rangeSec := rangeSeconds(c.DefaultQuery("range", "6h"))
	if rangeSec == 0 {
		rangeSec = 6 * 3600
	}
	// 固定 48 个桶：图宽有限，桶太多画出来是噪声，太少看不出跳变。
	// 6 小时 → 7.5 分钟一个桶；24 小时 → 30 分钟一个桶。
	const buckets = 48
	stepSec := rangeSec / buckets

	rows, err := sc.Query(fmt.Sprintf(`
		SELECT FLOOR(UNIX_TIMESTAMP(first_at) / %d) AS bucket,
		       severity, COUNT(*)
		  FROM incidents
		 WHERE tenant_id = ? AND first_at >= DATE_SUB(NOW(3), INTERVAL %d SECOND)
		 GROUP BY bucket, severity`, stepSec, rangeSec))
	if err != nil {
		abortQuery(c, err)
		return
	}
	defer rows.Close()

	type point struct {
		T        int64 `json:"t"`
		Critical int   `json:"critical"`
		Warning  int   `json:"warning"`
		Info     int   `json:"info"`
	}
	// ⚠️ 先把 48 个桶全部建出来再填数，**不能只返回有数据的桶**。
	// 只返回非空桶的话，前端连线会把"中间那段没有告警"画成一条直连的斜线，
	// 看起来像持续在响 —— 而真相是那段时间很安静。
	now := time.Now().Unix()
	start := (now - int64(rangeSec)) / int64(stepSec)
	series := make([]point, buckets)
	idx := map[int64]int{}
	for i := range series {
		b := start + int64(i)
		series[i] = point{T: b * int64(stepSec)}
		idx[b] = i
	}
	for rows.Next() {
		var bucket int64
		var sev string
		var n int
		if err := rows.Scan(&bucket, &sev, &n); err != nil {
			abortQuery(c, err)
			return
		}
		i, ok := idx[bucket]
		if !ok {
			continue // 边界桶，落在窗口外
		}
		switch sev {
		case "critical":
			series[i].Critical += n
		case "warning":
			series[i].Warning += n
		default:
			series[i].Info += n
		}
	}
	if err := rows.Err(); err != nil {
		abortQuery(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"step_sec": stepSec, "points": series})
}
