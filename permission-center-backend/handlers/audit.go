package handlers

import (
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"permission-center-backend/database"
)

// HandleAdminAuditLogs handles audit log queries
// GET /api/admin/audit-logs
func HandleAdminAuditLogs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		jsonError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	// Query params
	action := r.URL.Query().Get("action")
	username := r.URL.Query().Get("username")
	startDate := r.URL.Query().Get("start_date")
	endDate := r.URL.Query().Get("end_date")
	pageStr := r.URL.Query().Get("page")
	pageSizeStr := r.URL.Query().Get("page_size")

	page := 1
	pageSize := 50
	if p, err := strconv.Atoi(pageStr); err == nil && p > 0 {
		page = p
	}
	if ps, err := strconv.Atoi(pageSizeStr); err == nil && ps > 0 && ps <= 200 {
		pageSize = ps
	}

	query := `SELECT id, COALESCE(user_id,''), COALESCE(username,''), action, COALESCE(target_type,''), COALESCE(target_id,''), COALESCE(detail,''), COALESCE(ip,''), status, created_at FROM pc_audit_logs`
	countQuery := `SELECT COUNT(*) FROM pc_audit_logs`

	var conditions []string
	var args []interface{}

	if action != "" {
		conditions = append(conditions, "action = ?")
		args = append(args, action)
	}
	if username != "" {
		conditions = append(conditions, "username LIKE ?")
		args = append(args, "%"+username+"%")
	}
	if startDate != "" {
		conditions = append(conditions, "created_at >= ?")
		args = append(args, startDate)
	}
	if endDate != "" {
		conditions = append(conditions, "created_at <= ?")
		args = append(args, endDate+" 23:59:59")
	}

	if len(conditions) > 0 {
		where := " WHERE " + strings.Join(conditions, " AND ")
		query += where
		countQuery += where
	}

	// Get total count
	var total int
	database.DB.QueryRow(countQuery, args...).Scan(&total)

	// Add pagination
	offset := (page - 1) * pageSize
	query += " ORDER BY created_at DESC LIMIT ? OFFSET ?"
	args = append(args, pageSize, offset)

	rows, err := database.DB.Query(query, args...)
	if err != nil {
		log.Printf("[Audit] Query failed: %v", err)
		jsonError(w, http.StatusInternalServerError, "查询失败")
		return
	}
	defer rows.Close()

	var logs []map[string]interface{}
	for rows.Next() {
		var id, userID, uname, act, targetType, targetID, detail, ip, status string
		var createdAt time.Time
		rows.Scan(&id, &userID, &uname, &act, &targetType, &targetID, &detail, &ip, &status, &createdAt)
		logs = append(logs, map[string]interface{}{
			"id":          id,
			"user_id":     userID,
			"username":    uname,
			"action":      act,
			"target_type": targetType,
			"target_id":   targetID,
			"detail":      detail,
			"ip":          ip,
			"status":      status,
			"created_at":  createdAt.Format("2006-01-02 15:04:05"),
		})
	}
	if logs == nil {
		logs = []map[string]interface{}{}
	}

	jsonOK(w, map[string]interface{}{
		"items":     logs,
		"total":     total,
		"page":      page,
		"page_size": pageSize,
	})
}
