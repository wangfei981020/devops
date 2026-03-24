package handlers

import (
	"net/http"
	"strconv"

	"opsplatform-alert-backend/database"
)

func HandleListAlertLogs(w http.ResponseWriter, r *http.Request) {
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	ruleID := r.URL.Query().Get("rule_id")
	status := r.URL.Query().Get("status")
	severity := r.URL.Query().Get("severity")
	startDate := r.URL.Query().Get("start_date")
	endDate := r.URL.Query().Get("end_date")

	if page <= 0 {
		page = 1
	}
	if limit <= 0 {
		limit = 20
	}
	offset := (page - 1) * limit

	// Count
	countQuery := "SELECT COUNT(*) FROM alert_logs WHERE 1=1"
	args := []interface{}{}

	if ruleID != "" {
		countQuery += " AND rule_id = ?"
		rid, _ := strconv.Atoi(ruleID)
		args = append(args, rid)
	}
	if status != "" {
		countQuery += " AND status = ?"
		args = append(args, status)
	}
	if severity != "" {
		countQuery += " AND severity = ?"
		args = append(args, severity)
	}
	if startDate != "" {
		countQuery += " AND created_at >= ?"
		args = append(args, startDate+" 00:00:00")
	}
	if endDate != "" {
		countQuery += " AND created_at <= ?"
		args = append(args, endDate+" 23:59:59")
	}

	var total int64
	database.DB.QueryRow(countQuery, args...).Scan(&total)

	// Query
	query := `SELECT id, rule_id, rule_name, severity, message, COALESCE(es_raw,''),
		COALESCE(lark_response,''), status, COALESCE(error_msg,''), created_at
		FROM alert_logs WHERE 1=1`

	queryArgs := []interface{}{}
	if ruleID != "" {
		query += " AND rule_id = ?"
		rid, _ := strconv.Atoi(ruleID)
		queryArgs = append(queryArgs, rid)
	}
	if status != "" {
		query += " AND status = ?"
		queryArgs = append(queryArgs, status)
	}
	if severity != "" {
		query += " AND severity = ?"
		queryArgs = append(queryArgs, severity)
	}
	if startDate != "" {
		query += " AND created_at >= ?"
		queryArgs = append(queryArgs, startDate+" 00:00:00")
	}
	if endDate != "" {
		query += " AND created_at <= ?"
		queryArgs = append(queryArgs, endDate+" 23:59:59")
	}

	query += " ORDER BY id DESC LIMIT ? OFFSET ?"
	queryArgs = append(queryArgs, limit, offset)

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

// HandleGetAlertStats returns alert statistics
func HandleGetAlertStats(w http.ResponseWriter, r *http.Request) {
	stats := map[string]interface{}{}

	// Total rules
	var totalRules, enabledRules int
	database.DB.QueryRow("SELECT COUNT(*) FROM alert_rules").Scan(&totalRules)
	database.DB.QueryRow("SELECT COUNT(*) FROM alert_rules WHERE status = 1").Scan(&enabledRules)

	// Today's alerts
	var todayTotal, todaySuccess, todayFailed int
	database.DB.QueryRow("SELECT COUNT(*) FROM alert_logs WHERE DATE(created_at) = CURDATE()").Scan(&todayTotal)
	database.DB.QueryRow("SELECT COUNT(*) FROM alert_logs WHERE DATE(created_at) = CURDATE() AND status = 'success'").Scan(&todaySuccess)
	database.DB.QueryRow("SELECT COUNT(*) FROM alert_logs WHERE DATE(created_at) = CURDATE() AND status = 'failed'").Scan(&todayFailed)

	// ES connections
	var esTotal, esActive int
	database.DB.QueryRow("SELECT COUNT(*) FROM es_connections").Scan(&esTotal)
	database.DB.QueryRow("SELECT COUNT(*) FROM es_connections WHERE status = 1").Scan(&esActive)

	// Lark configs
	var larkTotal, larkActive int
	database.DB.QueryRow("SELECT COUNT(*) FROM lark_configs").Scan(&larkTotal)
	database.DB.QueryRow("SELECT COUNT(*) FROM lark_configs WHERE status = 1").Scan(&larkActive)

	stats["rules_total"] = totalRules
	stats["rules_enabled"] = enabledRules
	stats["today_alerts"] = todayTotal
	stats["today_success"] = todaySuccess
	stats["today_failed"] = todayFailed
	stats["es_connections"] = esTotal
	stats["es_active"] = esActive
	stats["lark_configs"] = larkTotal
	stats["lark_active"] = larkActive

	jsonSuccess(w, stats)
}

// HandleCleanAlertLogs cleans old logs
func HandleCleanAlertLogs(w http.ResponseWriter, r *http.Request) {
	days := r.URL.Query().Get("days")
	if days == "" {
		days = "30"
	}
	d, _ := strconv.Atoi(days)
	if d < 1 {
		d = 30
	}

	result, _ := database.DB.Exec("DELETE FROM alert_logs WHERE created_at < DATE_SUB(NOW(), INTERVAL ? DAY)", d)
	count, _ := result.RowsAffected()

	jsonSuccess(w, map[string]interface{}{
		"deleted": count,
		"message": "已清理" + strconv.FormatInt(count, 10) + "条日志",
	})
}
