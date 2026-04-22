package handlers

import (
	"encoding/json"
	"log"
	"net/http"
	"strconv"

	"opsplatform-deploy-backend/database"
)

// Audit 异步写一条审计日志（失败只 log 不阻塞请求）
// action 命名惯例：<target>.<verb>，如 user.create / project_env.update / lark_bot.test
func Audit(r *http.Request, action, targetType, targetName string, detail map[string]interface{}) {
	username := UsernameFromCtx(r)
	if username == "" {
		username = "system"
	}
	go saveAudit(username, action, targetType, targetName, detail, clientIP(r))
}

func saveAudit(username, action, targetType, targetName string, detail map[string]interface{}, ip string) {
	var detailStr string
	if len(detail) > 0 {
		b, _ := json.Marshal(detail)
		detailStr = string(b)
	}
	// auth_source 从 users 表查一次（简单，失败就默认 local）
	var authSource string
	_ = database.DB.QueryRow(`SELECT IFNULL(auth_source,'local') FROM users WHERE username=? LIMIT 1`, username).
		Scan(&authSource)
	if authSource == "" {
		authSource = "local"
	}
	_, err := database.DB.Exec(
		`INSERT INTO audit_log (username, auth_source, action, target_type, target_name, detail, ip)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		username, authSource, action, targetType, targetName, detailStr, ip)
	if err != nil {
		log.Printf("[audit] write failed: %v (action=%s target=%s)", err, action, targetName)
	}
}

// GET /api/audit-logs  (admin only)  —— 简单分页查询
func HandleListAuditLogs(w http.ResponseWriter, r *http.Request) {
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	if page < 1 {
		page = 1
	}
	pageSize, _ := strconv.Atoi(r.URL.Query().Get("page_size"))
	if pageSize < 1 || pageSize > 200 {
		pageSize = 50
	}

	var total int64
	_ = database.DB.QueryRow(`SELECT COUNT(*) FROM audit_log`).Scan(&total)

	rows, err := database.DB.Query(
		`SELECT id, username, auth_source, action, target_type, target_name, IFNULL(detail,''), ip, created_at
		 FROM audit_log ORDER BY id DESC LIMIT ? OFFSET ?`,
		pageSize, (page-1)*pageSize)
	if err != nil {
		InternalErr(w, r, err)
		return
	}
	defer rows.Close()
	type logItem struct {
		ID         int64  `json:"id"`
		Username   string `json:"username"`
		AuthSource string `json:"auth_source"`
		Action     string `json:"action"`
		TargetType string `json:"target_type"`
		TargetName string `json:"target_name"`
		Detail     string `json:"detail"`
		IP         string `json:"ip"`
		CreatedAt  string `json:"created_at"`
	}
	list := []logItem{}
	for rows.Next() {
		var it logItem
		var createdAt string
		_ = rows.Scan(&it.ID, &it.Username, &it.AuthSource, &it.Action, &it.TargetType, &it.TargetName, &it.Detail, &it.IP, &createdAt)
		it.CreatedAt = createdAt
		list = append(list, it)
	}
	JSONSuccess(w, map[string]interface{}{"total": total, "list": list})
}
