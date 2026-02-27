package handlers

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"

	"opsplatform/database"
	"opsplatform/models"
)

// 为了测试方便，可替换的时间函数
var timeNow = time.Now

// StartTimeKey Context key for request start time (使用 auth.go 中定义的 contextKey 类型)
const StartTimeKey contextKey = "startTime"

// HandleGetAuditLogs 获取审计日志
func HandleGetAuditLogs(w http.ResponseWriter, r *http.Request) {
	logs, err := GetAllAuditLogs()
	if err != nil {
		SafeError(w, "操作失败", http.StatusInternalServerError, err)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	json.NewEncoder(w).Encode(logs)
}

// HandleExportRecords 导出记录
func HandleExportRecords(w http.ResponseWriter, r *http.Request) {
	records, err := GetAllRecords()
	if err != nil {
		SafeError(w, "操作失败", http.StatusInternalServerError, err)
		return
	}

	// 应用过滤条件
	env := r.URL.Query().Get("env")
	status := r.URL.Query().Get("status")
	search := r.URL.Query().Get("search")

	var filtered []*models.Record
	for _, rec := range records {
		if env != "" && rec.Env != env {
			continue
		}
		if status != "" && rec.Status != status {
			continue
		}
		if search != "" {
			found := false
			searchLower := search
			for _, field := range []string{rec.Project, rec.VID, rec.SrcIP, rec.DestIP, rec.DestPort} {
				if containsIgnoreCase(field, searchLower) {
					found = true
					break
				}
			}
			if !found {
				continue
			}
		}
		filtered = append(filtered, rec)
	}

	timestamp := timeNow().Format("20060102_150405")
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=records_%s.csv", timestamp))
	// 添加 BOM 以支持 Excel 打开中文
	w.Write([]byte{0xEF, 0xBB, 0xBF})

	writer := csv.NewWriter(w)
	writer.Write([]string{"ID", "项目", "环境", "模块名", "VID", "源地址", "目标地址", "连接ID", "状态", "更新人", "更新时间"})
	for _, rec := range filtered {
		// 格式化 IP:端口
		srcAddr := rec.SrcIP
		if rec.SrcPort != "" {
			srcAddr = rec.SrcIP + ":" + rec.SrcPort
		}
		destAddr := rec.DestIP
		if rec.DestPort != "" {
			destAddr = rec.DestIP + ":" + rec.DestPort
		}
		// 状态转中文
		status := "启用"
		if rec.Status == "inactive" {
			status = "停用"
		}
		// 环境转大写
		env := strings.ToUpper(rec.Env)
		writer.Write([]string{
			rec.ID, rec.Project, env, rec.Module, rec.VID, srcAddr, destAddr,
			rec.ConnectionID, status, rec.UpdatedBy, rec.UpdatedAt,
		})
	}
	writer.Flush()
}

// HandleExportAuditLogs 导出审计日志
func HandleExportAuditLogs(w http.ResponseWriter, r *http.Request) {
	logs, err := GetAllAuditLogs()
	if err != nil {
		SafeError(w, "操作失败", http.StatusInternalServerError, err)
		return
	}

	timestamp := timeNow().Format("20060102_150405")
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=audit_logs_%s.csv", timestamp))
	w.Write([]byte{0xEF, 0xBB, 0xBF})

	writer := csv.NewWriter(w)
	writer.Write([]string{"ID", "操作", "记录ID", "操作人", "变更", "IP", "时间"})
	for _, log := range logs {
		writer.Write([]string{
			log.ID, log.Action, log.RecordID, log.Operator, log.Changes, log.IP, log.CreatedAt,
		})
	}
	writer.Flush()
}

// HandleQueryRecords 查询记录 API
func HandleQueryRecords(w http.ResponseWriter, r *http.Request) {
	records, err := GetAllRecords()
	if err != nil {
		SafeError(w, "操作失败", http.StatusInternalServerError, err)
		return
	}

	// 获取查询参数
	project := r.URL.Query().Get("project")
	env := r.URL.Query().Get("env")
	vid := r.URL.Query().Get("vid")
	srcIP := r.URL.Query().Get("src_ip")
	destIP := r.URL.Query().Get("dest_ip")
	port := r.URL.Query().Get("port")
	status := r.URL.Query().Get("status")

	var filtered []*models.Record
	for _, rec := range records {
		if project != "" && rec.Project != project {
			continue
		}
		if env != "" && rec.Env != env {
			continue
		}
		if vid != "" && rec.VID != vid {
			continue
		}
		if srcIP != "" && rec.SrcIP != srcIP {
			continue
		}
		if destIP != "" && rec.DestIP != destIP {
			continue
		}
		if port != "" && rec.DestPort != port {
			continue
		}
		if status != "" && rec.Status != status {
			continue
		}
		filtered = append(filtered, rec)
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	json.NewEncoder(w).Encode(filtered)
}

// 数据库操作函数
func GetAllAuditLogs() ([]*models.AuditLog, error) {
	rows, err := database.DB.Query(`
		SELECT id, COALESCE(trace_id, ''), action, COALESCE(method, ''), COALESCE(path, ''), 
		       COALESCE(status_code, 200), COALESCE(duration, 0), record_id, 
		       COALESCE(target_type, ''), COALESCE(target_id, ''),
		       operator, COALESCE(old_data, ''), COALESCE(new_data, ''), changes, ip, created_at
		FROM audit_logs ORDER BY created_at DESC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var logs []*models.AuditLog
	for rows.Next() {
		l := &models.AuditLog{}
		err := rows.Scan(&l.ID, &l.TraceID, &l.Action, &l.Method, &l.Path, &l.StatusCode, &l.Duration, 
			&l.RecordID, &l.TargetType, &l.TargetID, &l.Operator, &l.OldData, &l.NewData, &l.Changes, &l.IP, &l.CreatedAt)
		if err != nil {
			return nil, err
		}
		logs = append(logs, l)
	}
	return logs, nil
}

func AddAuditLog(action, recordID, operator, oldData, newData, changes, ip string) error {
	return AddAuditLogWithDetails(action, recordID, operator, oldData, newData, changes, ip, "", "", 200, 0)
}

// AddAuditLogFromRequest 从 HTTP 请求中自动获取 IP、方法、路径、耗时和当前用户
func AddAuditLogFromRequest(r *http.Request, action, recordID, operator, oldData, newData, changes string) error {
	ip := GetClientIP(r)
	method := r.Method
	path := r.URL.Path
	
	// 如果 operator 为空，自动从 context 获取当前登录用户
	if operator == "" {
		_, username, _ := GetUserFromContext(r)
		if username != "" {
			operator = username
		}
	}
	
	// 计算耗时（使用微秒，确保快速操作也有值）
	duration := int64(0)
	if start, ok := r.Context().Value(StartTimeKey).(time.Time); ok {
		durationMicro := time.Since(start).Microseconds()
		duration = durationMicro / 1000 // 转换为毫秒
		if duration == 0 && durationMicro > 0 {
			duration = 1 // 至少显示 1ms
		}
	}
	
	return AddAuditLogWithDetails(action, recordID, operator, oldData, newData, changes, ip, method, path, 200, duration)
}

// parseRecordID 解析 recordID 提取 target_type 和 target_id
// 格式: "type:id" 例如 "user:123", "domain:456", "record:789"
func parseRecordID(recordID string) (targetType, targetID string) {
	if idx := strings.Index(recordID, ":"); idx > 0 {
		targetType = recordID[:idx]
		targetID = recordID[idx+1:]
	} else {
		targetType = "unknown"
		targetID = recordID
	}
	return
}

// ===== 敏感数据脱敏 =====

// sensitiveFields 需要脱敏的敏感字段
var sensitiveFields = []string{
	"password", "Password", "pwd", "secret", "Secret",
	"token", "Token", "mfa_secret", "MFASecret",
	"authorization", "Authorization", "api_key", "apiKey",
	"credit_card", "creditCard", "ssn", "phone", "Phone",
	"email", "Email", "id_card", "idCard",
}

// maskString 脱敏字符串，保留前后各2个字符
func maskString(s string) string {
	if len(s) <= 4 {
		return "****"
	}
	runes := []rune(s)
	if len(runes) <= 6 {
		return string(runes[:1]) + "****" + string(runes[len(runes)-1:])
	}
	return string(runes[:2]) + "****" + string(runes[len(runes)-2:])
}

// maskEmail 脱敏邮箱地址
func maskEmail(email string) string {
	parts := strings.Split(email, "@")
	if len(parts) != 2 {
		return maskString(email)
	}
	username := parts[0]
	domain := parts[1]
	if len(username) <= 2 {
		return "**@" + domain
	}
	return string(username[0]) + "***@" + domain
}

// maskPhone 脱敏手机号码
func maskPhone(phone string) string {
	// 移除非数字字符
	digits := ""
	for _, c := range phone {
		if c >= '0' && c <= '9' {
			digits += string(c)
		}
	}
	if len(digits) < 7 {
		return "****"
	}
	// 保留前3后4
	return digits[:3] + "****" + digits[len(digits)-4:]
}


// sanitizeJSON 脱敏 JSON 字符串中的敏感字段
func sanitizeJSON(jsonStr string) string {
	if jsonStr == "" {
		return jsonStr
	}
	
	result := jsonStr
	for _, field := range sensitiveFields {
		// 匹配 "field":"value" 或 "field": "value" 格式
		patterns := []string{
			`"` + field + `":"[^"]*"`,
			`"` + field + `": "[^"]*"`,
			`"` + field + `":"[^"]*"`,
		}
		for _, pattern := range patterns {
			re := regexp.MustCompile(pattern)
			result = re.ReplaceAllStringFunc(result, func(match string) string {
				return `"` + field + `":"****"`
			})
		}
	}
	return result
}

// sanitizeChanges 脱敏变更描述中的敏感信息
func sanitizeChanges(changes string) string {
	result := changes
	
	// 脱敏密码相关
	passwordPatterns := []string{
		`密码[：:]\s*\S+`,
		`password[：:]\s*\S+`,
		`Password[：:]\s*\S+`,
	}
	for _, pattern := range passwordPatterns {
		re := regexp.MustCompile(pattern)
		result = re.ReplaceAllString(result, "密码: ****")
	}
	
	// 脱敏邮箱
	emailPattern := regexp.MustCompile(`[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}`)
	result = emailPattern.ReplaceAllStringFunc(result, maskEmail)
	
	// 脱敏手机号（中国大陆格式）
	phonePattern := regexp.MustCompile(`1[3-9]\d{9}`)
	result = phonePattern.ReplaceAllStringFunc(result, maskPhone)
	
	return result
}

// AddAuditLogWithDetails 记录审计日志（包含请求详情，自动脱敏敏感数据）
func AddAuditLogWithDetails(action, recordID, operator, oldData, newData, changes, ip, method, path string, statusCode int, duration int64) error {
	// 生成 trace_id：16位十六进制字符串
	traceID := fmt.Sprintf("%016x", timeNow().UnixNano())
	
	// 解析 recordID 提取 target_type 和 target_id
	targetType, targetID := parseRecordID(recordID)

	// 脱敏处理（IP 地址保留原始值）
	sanitizedOldData := sanitizeJSON(oldData)
	sanitizedNewData := sanitizeJSON(newData)
	sanitizedChanges := sanitizeChanges(changes)

	log := &models.AuditLog{
		ID:         fmt.Sprintf("log_%d", timeNow().UnixNano()),
		TraceID:    traceID,
		Action:     action,
		Method:     method,
		Path:       path,
		StatusCode: statusCode,
		Duration:   duration,
		RecordID:   recordID,
		TargetType: targetType,
		TargetID:   targetID,
		Operator:   operator,
		OldData:    sanitizedOldData,
		NewData:    sanitizedNewData,
		Changes:    sanitizedChanges,
		IP:         ip,
		CreatedAt:  timeNow().Format("2006-01-02 15:04:05"),
	}

	_, err := database.DB.Exec(`
		INSERT INTO audit_logs (id, trace_id, action, method, path, status_code, duration, record_id, target_type, target_id, operator, old_data, new_data, changes, ip, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, log.ID, log.TraceID, log.Action, log.Method, log.Path, log.StatusCode, log.Duration, log.RecordID, log.TargetType, log.TargetID, log.Operator, log.OldData, log.NewData, log.Changes, log.IP, log.CreatedAt)
	return err
}

// HandleDeleteAuditLog 删除审计日志（仅超级管理员）
func HandleDeleteAuditLog(w http.ResponseWriter, r *http.Request) {
	// 从 context 获取当前登录用户
	_, username, _ := GetUserFromContext(r)
	if username == "" {
		SafeError(w, "未授权", http.StatusUnauthorized, nil)
		return
	}

	// 检查是否是超级管理员
	user, err := GetUserByUsername(username)
	if err != nil || user == nil {
		SafeError(w, "用户不存在", http.StatusNotFound, err)
		return
	}

	if user.Role != "super_admin" {
		SafeError(w, "只有超级管理员才能删除审计日志", http.StatusForbidden, nil)
		return
	}

	// 获取要删除的日志ID
	logID := strings.TrimPrefix(r.URL.Path, "/api/audit-logs/")
	if logID == "" {
		SafeError(w, "日志ID不能为空", http.StatusBadRequest, nil)
		return
	}

	// 先查询要删除的日志详情（用于记录）
	log, err := GetAuditLogByID(logID)
	if err != nil {
		SafeError(w, "日志不存在", http.StatusNotFound, err)
		return
	}

	// 执行删除
	err = DeleteAuditLog(logID)
	if err != nil {
		SafeError(w, "删除失败", http.StatusInternalServerError, err)
		return
	}

	// 记录删除操作的审计日志
	oldDataJSON, _ := json.Marshal(log)
	AddAuditLogFromRequest(r, "DELETE", "audit_log:"+logID, username, string(oldDataJSON), "", 
		fmt.Sprintf("删除审计日志: trace_id=%s, action=%s, operator=%s", log.TraceID, log.Action, log.Operator))

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	json.NewEncoder(w).Encode(map[string]string{"message": "删除成功"})
}

// HandleBatchDeleteAuditLogs 批量删除审计日志（仅超级管理员）
func HandleBatchDeleteAuditLogs(w http.ResponseWriter, r *http.Request) {
	// 从 context 获取当前登录用户
	_, username, _ := GetUserFromContext(r)
	if username == "" {
		SafeError(w, "未授权", http.StatusUnauthorized, nil)
		return
	}

	user, err := GetUserByUsername(username)
	if err != nil || user == nil {
		SafeError(w, "用户不存在", http.StatusNotFound, err)
		return
	}

	if user.Role != "super_admin" {
		SafeError(w, "只有超级管理员才能删除审计日志", http.StatusForbidden, nil)
		return
	}

	var req struct {
		IDs []string `json:"ids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		SafeError(w, "参数错误", http.StatusBadRequest, err)
		return
	}

	if len(req.IDs) == 0 {
		SafeError(w, "请选择要删除的日志", http.StatusBadRequest, nil)
		return
	}

	// 批量删除
	deletedCount := 0
	for _, id := range req.IDs {
		if err := DeleteAuditLog(id); err == nil {
			deletedCount++
		}
	}

	// 记录批量删除操作
	AddAuditLogFromRequest(r, "DELETE", "audit_log:batch", username, "", "",
		fmt.Sprintf("批量删除审计日志: 共%d条", deletedCount))

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"message": "删除成功",
		"count":   deletedCount,
	})
}

// GetAuditLogByID 根据ID获取审计日志
func GetAuditLogByID(id string) (*models.AuditLog, error) {
	log := &models.AuditLog{}
	err := database.DB.QueryRow(`
		SELECT id, COALESCE(trace_id, ''), action, COALESCE(method, ''), COALESCE(path, ''),
		       COALESCE(status_code, 200), COALESCE(duration, 0), record_id,
		       COALESCE(target_type, ''), COALESCE(target_id, ''),
		       operator, COALESCE(old_data, ''), COALESCE(new_data, ''), changes, ip, created_at
		FROM audit_logs WHERE id = ?
	`, id).Scan(&log.ID, &log.TraceID, &log.Action, &log.Method, &log.Path, &log.StatusCode, &log.Duration,
		&log.RecordID, &log.TargetType, &log.TargetID, &log.Operator, &log.OldData, &log.NewData, &log.Changes, &log.IP, &log.CreatedAt)
	if err != nil {
		return nil, err
	}
	return log, nil
}

// DeleteAuditLog 删除审计日志
func DeleteAuditLog(id string) error {
	_, err := database.DB.Exec("DELETE FROM audit_logs WHERE id = ?", id)
	return err
}

// HandleFixEmptyOperators 修复空的 operator 字段
func HandleFixEmptyOperators(w http.ResponseWriter, r *http.Request) {
	// 从 context 获取当前登录用户，确保是超级管理员
	_, username, role := GetUserFromContext(r)
	if username == "" {
		SafeError(w, "未授权", http.StatusUnauthorized, nil)
		return
	}

	if role != "super_admin" {
		SafeError(w, "只有超级管理员才能执行此操作", http.StatusForbidden, nil)
		return
	}

	// 将空的、unknown 的 operator 更新为 admin
	result, err := database.DB.Exec(`
		UPDATE audit_logs
		SET operator = 'admin'
		WHERE (operator = '' OR operator IS NULL OR operator = 'unknown')
	`)
	if err != nil {
		SafeError(w, "修复失败", http.StatusInternalServerError, err)
		return
	}

	affected, _ := result.RowsAffected()

	// 记录这次修复操作
	AddAuditLog("FIX", "audit_logs:fix_operators", username, "", "",
		fmt.Sprintf("修复空的 operator 字段：共 %d 条记录", affected),
		GetClientIP(r))

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success":  true,
		"message":  fmt.Sprintf("修复完成，共更新 %d 条记录", affected),
		"affected": affected,
	})
}

func containsIgnoreCase(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 ||
		(len(s) > 0 && len(substr) > 0 && containsLower(toLower(s), toLower(substr))))
}

func containsLower(s, substr string) bool {
	for i := 0; i+len(substr) <= len(s); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func toLower(s string) string {
	b := []byte(s)
	for i, c := range b {
		if c >= 'A' && c <= 'Z' {
			b[i] = c + 32
		}
	}
	return string(b)
}






