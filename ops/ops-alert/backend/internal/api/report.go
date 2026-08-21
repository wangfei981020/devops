package api

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
)

type reportReq struct {
	Enabled     bool    `json:"enabled"`
	SendAt      string  `json:"send_at"`
	NotifierIDs []int64 `json:"notifier_ids"`
}

func (s *Server) getReportConfig(c *gin.Context) {
	sc, err := s.st.Tenant(c.Request.Context())
	if err != nil {
		abortTenant(c, err)
		return
	}
	var enabled int
	var sendAt, lastError string
	var rawNotifiers []byte
	var lastSent sql.NullTime
	err = sc.QueryRow(`SELECT enabled, send_at, notifier_ids, last_sent_on, last_error
		FROM report_configs WHERE tenant_id = ?`).
		Scan(&enabled, &sendAt, &rawNotifiers, &lastSent, &lastError)
	if err == sql.ErrNoRows {
		// 没配过是正常状态。返回默认值而不是 404：
		// 404 会让前端渲染成错误态，而"还没配"不是错误。
		c.JSON(http.StatusOK, gin.H{
			"enabled": false, "send_at": "09:00", "notifier_ids": []int64{},
			"last_sent_on": nil, "last_error": "", "configured": false,
		})
		return
	}
	if err != nil {
		abortQuery(c, err)
		return
	}
	ids := []int64{}
	_ = json.Unmarshal(rawNotifiers, &ids)
	var last any
	if lastSent.Valid {
		last = lastSent.Time.Format("2006-01-02")
	}
	c.JSON(http.StatusOK, gin.H{
		"enabled": enabled == 1, "send_at": sendAt, "notifier_ids": ids,
		"last_sent_on": last, "last_error": lastError, "configured": true,
	})
}

func (s *Server) saveReportConfig(c *gin.Context) {
	sc, err := s.st.Tenant(c.Request.Context())
	if err != nil {
		abortTenant(c, err)
		return
	}
	var req reportReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "bad_request"})
		return
	}
	if _, err := time.Parse("15:04", req.SendAt); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "bad_send_at", "detail": "发送时刻应形如 09:00（24 小时制）"})
		return
	}
	// ⚠️ 开着日报却没选渠道 = 永远不会收到，而界面上开关是绿的。
	// 保存时就拦住，别等到第二天早上"怎么没收到"。
	if req.Enabled && len(req.NotifierIDs) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "no_notifier", "detail": "开启日报必须至少选一个投递渠道，否则日报永远发不出去"})
		return
	}
	ids, _ := json.Marshal(req.NotifierIDs)
	// 改了配置就清掉 last_error：那条错误说的是旧配置的事，
	// 留着会让人以为新配置也是坏的。last_sent_on 保留 —— 它是事实。
	if _, err := sc.Exec(`INSERT INTO report_configs (tenant_id, enabled, send_at, notifier_ids, last_error)
		VALUES (?, ?, ?, ?, '')
		ON DUPLICATE KEY UPDATE enabled = VALUES(enabled), send_at = VALUES(send_at),
			notifier_ids = VALUES(notifier_ids), last_error = ''`,
		boolToInt(req.Enabled), req.SendAt, ids); err != nil {
		abortQuery(c, err)
		return
	}
	s.audit(c, sc, "report.save", "report", 0,
		gin.H{"enabled": req.Enabled, "send_at": req.SendAt, "notifiers": len(req.NotifierIDs)})
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// previewReport 现在就生成一份日报内容返回，不发送。
//
// 没有这个的话，配完日报只能等到第二天早上才知道对不对 ——
// 而那时如果不对，又要再等一天。日报是最容易"配了但没生效"的功能，
// 因为它一天只有一次验证机会。
func (s *Server) previewReport(c *gin.Context) {
	sc, err := s.st.Tenant(c.Request.Context())
	if err != nil {
		abortTenant(c, err)
		return
	}
	fields, detail, err := s.eng.BuildReportPreview(sc, time.Now())
	if err != nil {
		abortQuery(c, err)
		return
	}
	out := make([]gin.H, 0, len(fields))
	for _, f := range fields {
		out = append(out, gin.H{"key": f.Key, "value": f.Value})
	}
	c.JSON(http.StatusOK, gin.H{"fields": out, "detail": detail})
}

// sendReportNow 立即发一份。用于确认渠道真的能收到。
//
// ⚠️ 它**不推进 last_sent_on**：试发不该顶掉当天的正式发送。
// 顶掉的表现是"我上午试发了一下，结果第二天早上的日报没来"。
func (s *Server) sendReportNow(c *gin.Context) {
	sc, err := s.st.Tenant(c.Request.Context())
	if err != nil {
		abortTenant(c, err)
		return
	}
	sent, total, sendErr := s.eng.SendReportNow(c.Request.Context(), sc, time.Now())
	s.audit(c, sc, "report.send_now", "report", 0, gin.H{"sent": sent, "total": total})
	if sent == 0 {
		// 一条都没发出去必须是失败响应。返回 200 + "已发送" 是
		// 这个产品明令禁止的那类退化：界面说成功，人却收不到。
		msg := "没有配置投递渠道"
		if sendErr != nil {
			msg = sendErr.Error()
		}
		c.JSON(http.StatusBadGateway, gin.H{"error": "send_failed", "detail": msg})
		return
	}
	c.JSON(http.StatusOK, gin.H{"sent": sent, "total": total, "partial_error": errText(sendErr)})
}

func errText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
