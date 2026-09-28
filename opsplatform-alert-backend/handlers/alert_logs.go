package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"opsplatform-alert-backend/database"
	"opsplatform-alert-backend/timezone"
)

// alertLogFilters turns the query string into one set of SQL conditions.
//
// The count and the page query have to agree: when they were assembled
// separately, side by side, a filter added to one and missed in the other gave
// a list of five rows claiming to be page one of nine hundred. Building them
// once removes that whole class of bug.
//
// Dates are the other trap. The picker hands over a calendar day as the person
// reading the page understands it, and created_at is a UTC instant, so the day
// has to be anchored in the display timezone before it becomes a bound. Passing
// the bare string let MySQL read it as UTC — asking for 9-28 in Beijing quietly
// searched 08:00 on the 28th through 08:00 on the 29th.
func alertLogFilters(r *http.Request) (string, []interface{}) {
	where := []string{"1=1"}
	args := []interface{}{}
	q := r.URL.Query()

	if v := q.Get("rule_id"); v != "" {
		if rid, err := strconv.Atoi(v); err == nil {
			where = append(where, "rule_id = ?")
			args = append(args, rid)
		}
	}
	if v := q.Get("status"); v != "" {
		where = append(where, "status = ?")
		args = append(args, v)
	}
	if v := q.Get("severity"); v != "" {
		where = append(where, "severity = ?")
		args = append(args, v)
	}
	// One parameter serves both levels of the project tree: a top-level id
	// matches its own rules and every rule under its environments, a child id
	// matches only itself. The alternative — separate project and environment
	// parameters — would need the two kept consistent by every caller.
	if v := q.Get("project_id"); v != "" {
		if pid, err := strconv.Atoi(v); err == nil && pid > 0 {
			where = append(where, `rule_id IN (SELECT id FROM alert_rules WHERE project_id IN
				(SELECT id FROM alert_projects WHERE id = ? OR parent_id = ?))`)
			args = append(args, pid, pid)
		}
	}

	if t, ok := dayStart(q.Get("start_date")); ok {
		where = append(where, "created_at >= ?")
		args = append(args, t)
	}
	if t, ok := dayEnd(q.Get("end_date")); ok {
		where = append(where, "created_at <= ?")
		args = append(args, t)
	}

	return strings.Join(where, " AND "), args
}

// dayStart and dayEnd turn a picked calendar day into an instant bound.
//
// The day means the day in the display timezone — that is the calendar the
// person clicking the picker is reading. Handing MySQL the bare "2026-09-28
// 00:00:00" made it a UTC bound instead, shifting the whole window by the
// offset. Listing the wrong rows is confusing; the log cleaner runs the same
// two bounds through a DELETE, where the shift silently removes eight hours of
// the wrong data, so both callers go through here.
func dayStart(date string) (time.Time, bool) {
	return parseDayBound(date, " 00:00:00")
}

func dayEnd(date string) (time.Time, bool) {
	return parseDayBound(date, " 23:59:59")
}

func parseDayBound(date, clock string) (time.Time, bool) {
	if date == "" {
		return time.Time{}, false
	}
	t, err := time.ParseInLocation("2006-01-02 15:04:05", date+clock, timezone.Location())
	if err != nil {
		return time.Time{}, false
	}
	return t, true
}

func HandleListAlertLogs(w http.ResponseWriter, r *http.Request) {
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))

	if page <= 0 {
		page = 1
	}
	if limit <= 0 {
		limit = 20
	}
	offset := (page - 1) * limit

	where, args := alertLogFilters(r)

	var total int64
	database.DB.QueryRow("SELECT COUNT(*) FROM alert_logs WHERE "+where, args...).Scan(&total)

	// Query
	//
	// created_at is handed over as the instant it is, never pre-shifted. This
	// column used to be wrapped in CONVERT_TZ(created_at, '+00:00', '+08:00'),
	// left over from before the platform had a display timezone: the session is
	// pinned to +00:00 and the driver labels rows UTC (see database.InitMySQL),
	// so that call turned a correct UTC instant into a Beijing wall clock still
	// wearing a "Z", and the frontend's formatTime then added the eight hours a
	// second time — an alert that fired at 11:20 listed as 19:20. Which zone a
	// person reads is decided in one place, utils/datetime.js, from the
	// configured display timezone.
	query := `SELECT id, rule_id, rule_name, severity, message, COALESCE(es_raw,''),
		COALESCE(lark_response,''), status, COALESCE(error_msg,''), created_at
		FROM alert_logs WHERE ` + where + ` ORDER BY id DESC LIMIT ? OFFSET ?`

	queryArgs := append(append([]interface{}{}, args...), limit, offset)

	rows, err := database.DB.Query(query, queryArgs...)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "查询失败")
		return
	}
	defer rows.Close()

	var list []map[string]interface{}
	for rows.Next() {
		var id int64
		var ruleID int
		var ruleName, sev, msg, esRaw, larkResp, stat, errMsg string
		var createdAt string

		rows.Scan(&id, &ruleID, &ruleName, &sev, &msg, &esRaw, &larkResp, &stat, &errMsg, &createdAt)

		item := map[string]interface{}{
			"id":            id,
			"rule_id":       ruleID,
			"rule_name":     ruleName,
			"severity":      sev,
			"message":       msg,
			"es_raw":        esRaw,
			"lark_response": larkResp,
			"status":        stat,
			"error_msg":     errMsg,
			"created_at":    createdAt,
		}
		list = append(list, item)
	}
	if list == nil {
		list = []map[string]interface{}{}
	}

	jsonPaginated(w, list, total, page, limit)
}

// HandleGetAlertStats returns alert statistics.
//
// The dashboard has moved to /dashboard, which returns these same counts inside
// a larger payload. This endpoint stays because an existing installation may
// have scripts or dashboards pointing at it, and it now shares the one
// implementation with /dashboard rather than carrying a second copy of the same
// queries — the two were already drifting: "today" was still being counted in
// the database session's day here while the dashboard had moved to the
// platform's display timezone, so the same platform reported two different
// numbers depending on which URL you asked.
func HandleGetAlertStats(w http.ResponseWriter, r *http.Request) {
	counts := dashboardCounts()

	stats := map[string]interface{}{}
	for k, v := range counts {
		stats[k] = v
	}
	jsonSuccess(w, stats)
}

// HandleCleanAlertLogs cleans old logs
func HandleCleanAlertLogs(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ProjectID int    `json:"project_id"`
		RuleID    int    `json:"rule_id"`
		StartDate string `json:"start_date"`
		EndDate   string `json:"end_date"`
		Status    string `json:"status"`
		Preview   bool   `json:"preview"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, http.StatusBadRequest, "无效的请求")
		return
	}

	// Build WHERE clause
	where := "1=1"
	args := []interface{}{}

	if req.ProjectID > 0 {
		where += " AND rule_id IN (SELECT id FROM alert_rules WHERE project_id IN (SELECT id FROM alert_projects WHERE id = ? OR parent_id = ?))"
		args = append(args, req.ProjectID, req.ProjectID)
	}
	if req.RuleID > 0 {
		where += " AND rule_id = ?"
		args = append(args, req.RuleID)
	}
	if t, ok := dayStart(req.StartDate); ok {
		where += " AND created_at >= ?"
		args = append(args, t)
	}
	if t, ok := dayEnd(req.EndDate); ok {
		where += " AND created_at <= ?"
		args = append(args, t)
	}
	if req.Status != "" {
		where += " AND status = ?"
		args = append(args, req.Status)
	}

	// Preview mode: only count
	if req.Preview {
		var count int64
		database.DB.QueryRow("SELECT COUNT(*) FROM alert_logs WHERE "+where, args...).Scan(&count)
		jsonSuccess(w, map[string]interface{}{"count": count})
		return
	}

	// Delete
	result, _ := database.DB.Exec("DELETE FROM alert_logs WHERE "+where, args...)
	count, _ := result.RowsAffected()

	SaveAuditLog(r, "clean_logs", "alert_log", fmt.Sprintf("清理%d条", count),
		fmt.Sprintf("project_id=%d rule_id=%d date=%s~%s status=%s", req.ProjectID, req.RuleID, req.StartDate, req.EndDate, req.Status))

	jsonSuccess(w, map[string]interface{}{
		"deleted": count,
		"message": fmt.Sprintf("已清理 %d 条日志", count),
	})
}
