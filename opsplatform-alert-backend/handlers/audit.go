package handlers

import (
	"encoding/json"
	"net/http"
	"strings"

	"opsplatform-alert-backend/database"
)

// SaveAuditLog records an operation in the audit log
func SaveAuditLog(r *http.Request, action, targetType, targetName, detail string) {
	username := ""
	authSource := "local"
	if u := r.Context().Value(contextUsername); u != nil {
		username = u.(string)
	}
	ip := r.Header.Get("X-Forwarded-For")
	if ip == "" {
		ip = r.Header.Get("X-Real-IP")
	}
	if ip == "" {
		ip = strings.Split(r.RemoteAddr, ":")[0]
	}

	database.DB.Exec(`INSERT INTO audit_logs (username, auth_source, action, target_type, target_name, detail, ip)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		username, authSource, action, targetType, targetName, detail, ip)
}

// HandleListAuditLogs returns audit logs with pagination
func HandleListAuditLogs(w http.ResponseWriter, r *http.Request) {
	page := r.URL.Query().Get("page")
	if page == "" {
		page = "1"
	}
	pageSize := r.URL.Query().Get("page_size")
	if pageSize == "" {
		pageSize = "50"
	}
	action := r.URL.Query().Get("action")
	username := r.URL.Query().Get("username")

	// Count total
	countQuery := "SELECT COUNT(*) FROM audit_logs WHERE 1=1"
	args := []interface{}{}
	if action != "" {
		countQuery += " AND action = ?"
		args = append(args, action)
	}
	if username != "" {
		countQuery += " AND username LIKE ?"
		args = append(args, "%"+username+"%")
	}
	var total int
	database.DB.QueryRow(countQuery, args...).Scan(&total)

	// Query logs
	query := "SELECT id, username, auth_source, action, target_type, target_name, detail, ip, created_at FROM audit_logs WHERE 1=1"
	queryArgs := []interface{}{}
	if action != "" {
		query += " AND action = ?"
		queryArgs = append(queryArgs, action)
	}
	if username != "" {
		query += " AND username LIKE ?"
		queryArgs = append(queryArgs, "%"+username+"%")
	}
	query += " ORDER BY id DESC LIMIT ? OFFSET ?"

	var p, ps int
	json.Unmarshal([]byte(page), &p)
	json.Unmarshal([]byte(pageSize), &ps)
	if p < 1 {
		p = 1
	}
	if ps < 1 || ps > 200 {
		ps = 50
	}
	queryArgs = append(queryArgs, ps, (p-1)*ps)

	rows, err := database.DB.Query(query, queryArgs...)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "查询失败")
		return
	}
	defer rows.Close()

	var list []map[string]interface{}
	for rows.Next() {
		var id int64
		var uname, authSrc, act, tType, tName, detail, ip, createdAt string
		rows.Scan(&id, &uname, &authSrc, &act, &tType, &tName, &detail, &ip, &createdAt)
		list = append(list, map[string]interface{}{
			"id":          id,
			"username":    uname,
			"auth_source": authSrc,
			"action":      act,
			"target_type": tType,
			"target_name": tName,
			"detail":      detail,
			"ip":          ip,
			"created_at":  createdAt,
		})
	}
	if list == nil {
		list = []map[string]interface{}{}
	}

	jsonSuccess(w, map[string]interface{}{
		"list":  list,
		"total": total,
		"page":  p,
	})
}
