package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"opsplatform-alert-backend/alert"
	"opsplatform-alert-backend/models"
)

// 站点 × 桌台 候选关系扫描。
//
// 运维平台需要这层关系才能确定心跳告警的监控范围，而中台接口给不了它。这边有
// Loki，能看到「哪个站点在哪张桌台上出现过」——日志出现过就证明这个组合真实存在，
// 所以扫出来的结果可以直接作为关系导入。
//
// 反过来不成立：日志里没出现，不代表关系不存在。可能就是这段时间没人玩，也可能
// 这张桌台已经死了一周——而后者恰恰是最该被监控的。所以导出的结果只能用来「增」，
// 运维平台那边也只增不删。

type heartbeatScanReq struct {
	LokiConnectionID int    `json:"loki_connection_id"`
	LogQL            string `json:"logql"`
	DimPattern       string `json:"dim_pattern"`
	ScanRange        string `json:"scan_range"`
}

// HandleHeartbeatScanPairs POST /api/alert-rules/scan-pairs
//
// 扫描窗口默认给 7d 而不是规则自己的告警窗口：几分钟的窗口只能看到此刻在玩的桌台，
// 用它当关系全集会漏掉绝大多数组合。
func HandleHeartbeatScanPairs(w http.ResponseWriter, r *http.Request) {
	var req heartbeatScanReq
	if json.NewDecoder(r.Body).Decode(&req) != nil {
		jsonError(w, http.StatusBadRequest, "无效的请求")
		return
	}
	if req.ScanRange == "" {
		req.ScanRange = "7d"
	}

	// 扫描窗口跨天，比一轮告警查询重得多
	ctx, cancel := context.WithTimeout(r.Context(), 120*time.Second)
	defer cancel()

	rule := &models.AlertRule{
		LokiConnectionID: req.LokiConnectionID,
		LogQL:            req.LogQL,
		DimPattern:       req.DimPattern,
		TimeRange:        req.ScanRange,
	}
	pairs, query, err := alert.ScanHeartbeatPairs(ctx, rule, handlerLokiClientFunc())
	if err != nil {
		jsonError(w, http.StatusBadRequest, "扫描失败: "+err.Error())
		return
	}

	jsonSuccess(w, map[string]interface{}{
		"scan_range": req.ScanRange,
		"pairs":      pairs,
		"count":      len(pairs),
		"query":      query,
		// 这段整个贴进运维平台「站点×桌台 → 导入候选」即可
		"payload": map[string]interface{}{
			"scan_range": req.ScanRange,
			"pairs":      pairs,
		},
		"hint": fmt.Sprintf("扫描 %s 得到 %d 个组合。导入到运维平台后即生效；"+
			"日志里没出现过的组合要在那边手工补——它们扫不出来，而且往往正是最该监控的。",
			req.ScanRange, len(pairs)),
	})
}
