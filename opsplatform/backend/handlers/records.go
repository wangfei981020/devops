package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"

	"opsplatform/database"
	"opsplatform/models"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
)

// HandleGetRecords 获取所有记录
func HandleGetRecords(w http.ResponseWriter, r *http.Request) {
	records, err := GetAllRecords()
	if err != nil {
		SafeError(w, "操作失败", http.StatusInternalServerError, err)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	json.NewEncoder(w).Encode(records)
}

// HandleGetRecord 获取单条记录
func HandleGetRecord(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id := vars["id"]

	record, err := GetRecord(id)
	if err != nil {
		SafeError(w, "操作失败", http.StatusInternalServerError, err)
		return
	}
	if record == nil {
		http.Error(w, "记录不存在", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	json.NewEncoder(w).Encode(record)
}

// HandleAddRecord 添加记录
func HandleAddRecord(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Record   models.Record `json:"record"`
		Operator string        `json:"operator"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "无效的请求数据", http.StatusBadRequest)
		return
	}

	if req.Operator == "" {
		http.Error(w, "操作人不能为空", http.StatusBadRequest)
		return
	}

	if req.Record.ConnectionID == "" || req.Record.Project == "" || req.Record.VID == "" || req.Record.SrcIP == "" || req.Record.DestIP == "" || req.Record.Port == "" {
		http.Error(w, "连接ID、项目、VID、源IP、目标IP、端口不能为空", http.StatusBadRequest)
		return
	}

	// 检查连接ID唯一性
	if exists, _ := ConnectionIDExists(req.Record.ConnectionID, ""); exists {
		http.Error(w, "连接ID已存在，请使用不同的连接ID", http.StatusBadRequest)
		return
	}

	ip := GetClientIP(r)
	if err := AddRecord(&req.Record, req.Operator, ip); err != nil {
		SafeError(w, "操作失败", http.StatusInternalServerError, err)
		return
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(req.Record)
}

// HandleBatchAddRecords 批量添加记录
func HandleBatchAddRecords(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Records  []models.Record `json:"records"`
		Operator string          `json:"operator"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "无效的请求数据", http.StatusBadRequest)
		return
	}

	if req.Operator == "" {
		http.Error(w, "操作人不能为空", http.StatusBadRequest)
		return
	}

	if len(req.Records) == 0 {
		http.Error(w, "记录列表不能为空", http.StatusBadRequest)
		return
	}

	ip := GetClientIP(r)
	addedRecords := make([]models.Record, 0, len(req.Records))

	// 先检查所有连接ID的唯一性
	for i, record := range req.Records {
		if record.ConnectionID == "" {
			http.Error(w, fmt.Sprintf("第 %d 条记录的连接ID不能为空", i+1), http.StatusBadRequest)
			return
		}
		if exists, _ := ConnectionIDExists(record.ConnectionID, ""); exists {
			http.Error(w, fmt.Sprintf("连接ID '%s' 已存在", record.ConnectionID), http.StatusBadRequest)
			return
		}
	}

	for _, record := range req.Records {
		rec := record
		if err := AddRecord(&rec, req.Operator, ip); err != nil {
			SafeError(w, "操作失败", http.StatusInternalServerError, err)
			return
		}
		addedRecords = append(addedRecords, rec)
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"message": fmt.Sprintf("成功添加 %d 条记录", len(addedRecords)),
		"count":   len(addedRecords),
		"records": addedRecords,
	})
}

// HandleUpdateRecord 更新记录
func HandleUpdateRecord(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id := vars["id"]

	var req struct {
		Record   models.Record `json:"record"`
		Operator string        `json:"operator"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "无效的请求数据", http.StatusBadRequest)
		return
	}

	if req.Operator == "" {
		http.Error(w, "操作人不能为空", http.StatusBadRequest)
		return
	}

	ip := GetClientIP(r)
	if err := UpdateRecord(id, &req.Record, req.Operator, ip); err != nil {
		SafeError(w, "资源不存在", http.StatusNotFound, err)
		return
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	json.NewEncoder(w).Encode(req.Record)
}

// HandleDeleteRecord 删除记录
func HandleDeleteRecord(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id := vars["id"]

	var req struct {
		Operator string `json:"operator"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "无效的请求数据", http.StatusBadRequest)
		return
	}

	if req.Operator == "" {
		http.Error(w, "操作人不能为空", http.StatusBadRequest)
		return
	}

	ip := GetClientIP(r)
	if err := DeleteRecord(id, req.Operator, ip); err != nil {
		SafeError(w, "资源不存在", http.StatusNotFound, err)
		return
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	json.NewEncoder(w).Encode(map[string]string{"message": "删除成功"})
}

// HandleBatchDeleteRecords 批量删除记录
func HandleBatchDeleteRecords(w http.ResponseWriter, r *http.Request) {
	var req struct {
		IDs      []string `json:"ids"`
		Operator string   `json:"operator"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "无效的请求数据", http.StatusBadRequest)
		return
	}

	if req.Operator == "" {
		http.Error(w, "操作人不能为空", http.StatusBadRequest)
		return
	}

	if len(req.IDs) == 0 {
		http.Error(w, "请选择要删除的记录", http.StatusBadRequest)
		return
	}

	ip := GetClientIP(r)
	deleted := 0
	for _, id := range req.IDs {
		if err := DeleteRecord(id, req.Operator, ip); err == nil {
			deleted++
		}
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"message": fmt.Sprintf("成功删除 %d 条记录", deleted),
		"count":   deleted,
	})
}

// HandleBatchUpdateStatus 批量更新状态
func HandleBatchUpdateStatus(w http.ResponseWriter, r *http.Request) {
	var req struct {
		IDs      []string `json:"ids"`
		Status   string   `json:"status"`
		Operator string   `json:"operator"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "无效的请求数据", http.StatusBadRequest)
		return
	}

	if req.Operator == "" {
		http.Error(w, "操作人不能为空", http.StatusBadRequest)
		return
	}

	if len(req.IDs) == 0 {
		http.Error(w, "请选择要修改的记录", http.StatusBadRequest)
		return
	}

	if req.Status != "active" && req.Status != "inactive" && req.Status != "pending" {
		http.Error(w, "无效的状态值", http.StatusBadRequest)
		return
	}

	ip := GetClientIP(r)
	updated := 0
	for _, id := range req.IDs {
		if err := UpdateRecordStatus(id, req.Status, req.Operator, ip); err == nil {
			updated++
		}
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"message": fmt.Sprintf("成功更新 %d 条记录状态", updated),
		"count":   updated,
	})
}

// GetClientIP 获取客户端IP
func GetClientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		return xff
	}
	if xri := r.Header.Get("X-Real-IP"); xri != "" {
		return xri
	}
	return r.RemoteAddr
}

// 数据库操作函数
func GetAllRecords() ([]*models.Record, error) {
	rows, err := database.DB.Query(`
		SELECT id, COALESCE(connection_id, ''), project, env, COALESCE(module, ''), vid, src_ip, dest_ip, port, status, 
		       COALESCE(operator, ''), created_at, updated_at, 
		       COALESCE(created_by, ''), COALESCE(updated_by, '')
		FROM records ORDER BY updated_at DESC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var records []*models.Record
	for rows.Next() {
		r := &models.Record{}
		err := rows.Scan(&r.ID, &r.ConnectionID, &r.Project, &r.Env, &r.Module, &r.VID, &r.SrcIP, &r.DestIP,
			&r.Port, &r.Status, &r.Operator, &r.CreatedAt, &r.UpdatedAt,
			&r.CreatedBy, &r.UpdatedBy)
		if err != nil {
			return nil, err
		}
		records = append(records, r)
	}
	return records, nil
}

// ConnectionIDExists 检查连接ID是否已存在
func ConnectionIDExists(connectionID, excludeID string) (bool, error) {
	var count int
	var err error
	if excludeID == "" {
		err = database.DB.QueryRow("SELECT COUNT(*) FROM records WHERE connection_id = ?", connectionID).Scan(&count)
	} else {
		err = database.DB.QueryRow("SELECT COUNT(*) FROM records WHERE connection_id = ? AND id != ?", connectionID, excludeID).Scan(&count)
	}
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

func GetRecord(id string) (*models.Record, error) {
	r := &models.Record{}
	err := database.DB.QueryRow(`
		SELECT id, COALESCE(connection_id, ''), project, env, COALESCE(module, ''), vid, src_ip, dest_ip, port, status,
		       COALESCE(operator, ''), created_at, updated_at,
		       COALESCE(created_by, ''), COALESCE(updated_by, '')
		FROM records WHERE id = ?
	`, id).Scan(&r.ID, &r.ConnectionID, &r.Project, &r.Env, &r.Module, &r.VID, &r.SrcIP, &r.DestIP,
		&r.Port, &r.Status, &r.Operator, &r.CreatedAt, &r.UpdatedAt,
		&r.CreatedBy, &r.UpdatedBy)
	if err != nil {
		return nil, nil
	}
	return r, nil
}

func AddRecord(r *models.Record, operator, ip string) error {
	r.ID = fmt.Sprintf("rec_%d", timeNow().UnixNano())
	r.CreatedAt = timeNow().Format("2006-01-02 15:04:05")
	r.UpdatedAt = r.CreatedAt
	r.CreatedBy = operator
	r.UpdatedBy = operator

	_, err := database.DB.Exec(`
		INSERT INTO records (id, connection_id, project, env, module, vid, src_ip, dest_ip, port, status, operator, created_at, updated_at, created_by, updated_by)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, r.ID, r.ConnectionID, r.Project, r.Env, r.Module, r.VID, r.SrcIP, r.DestIP, r.Port, r.Status, r.Operator, r.CreatedAt, r.UpdatedAt, r.CreatedBy, r.UpdatedBy)
	if err != nil {
		return err
	}

	// 保存历史记录
	SaveRecordHistory(r.ID, "create", r, "创建记录", operator)

	newDataJSON, _ := json.Marshal(r)
	return AddAuditLog("create", r.ID, operator, "", string(newDataJSON),
		fmt.Sprintf("创建记录: 连接ID=%s, 项目=%s, VID=%s, %s->%s:%s", r.ConnectionID, r.Project, r.VID, r.SrcIP, r.DestIP, r.Port), ip)
}

func UpdateRecord(id string, r *models.Record, operator, ip string) error {
	oldRecord, err := GetRecord(id)
	if err != nil || oldRecord == nil {
		return fmt.Errorf("记录不存在: %s", id)
	}

	// 检查连接ID唯一性（排除当前记录）
	if r.ConnectionID != oldRecord.ConnectionID {
		if exists, _ := ConnectionIDExists(r.ConnectionID, id); exists {
			return fmt.Errorf("连接ID '%s' 已存在", r.ConnectionID)
		}
	}

	oldDataJSON, _ := json.Marshal(oldRecord)
	changes := generateChanges(oldRecord, r)
	changesJSON := generateChangesJSON(oldRecord, r)

	r.ID = id
	r.CreatedAt = oldRecord.CreatedAt
	r.CreatedBy = oldRecord.CreatedBy
	r.UpdatedAt = timeNow().Format("2006-01-02 15:04:05")
	r.UpdatedBy = operator

	_, err = database.DB.Exec(`
		UPDATE records SET connection_id=?, project=?, env=?, module=?, vid=?, src_ip=?, dest_ip=?, port=?, status=?, operator=?, updated_at=?, updated_by=?
		WHERE id=?
	`, r.ConnectionID, r.Project, r.Env, r.Module, r.VID, r.SrcIP, r.DestIP, r.Port, r.Status, r.Operator, r.UpdatedAt, r.UpdatedBy, id)
	if err != nil {
		return err
	}

	// 保存历史记录（保存更新后的版本）
	SaveRecordHistory(id, "update", r, changesJSON, operator)

	newDataJSON, _ := json.Marshal(r)
	return AddAuditLog("update", id, operator, string(oldDataJSON), string(newDataJSON), changes, ip)
}

func DeleteRecord(id string, operator, ip string) error {
	oldRecord, err := GetRecord(id)
	if err != nil || oldRecord == nil {
		return fmt.Errorf("记录不存在: %s", id)
	}

	oldDataJSON, _ := json.Marshal(oldRecord)

	_, err = database.DB.Exec("DELETE FROM records WHERE id=?", id)
	if err != nil {
		return err
	}

	return AddAuditLog("delete", id, operator, string(oldDataJSON), "",
		fmt.Sprintf("删除记录: 项目=%s, VID=%s, %s->%s:%s", oldRecord.Project, oldRecord.VID, oldRecord.SrcIP, oldRecord.DestIP, oldRecord.Port), ip)
}

// UpdateRecordStatus 更新记录状态
func UpdateRecordStatus(id, status, operator, ip string) error {
	oldRecord, err := GetRecord(id)
	if err != nil || oldRecord == nil {
		return fmt.Errorf("记录不存在: %s", id)
	}

	oldStatus := oldRecord.Status
	if oldStatus == status {
		return nil // 状态相同，无需更新
	}

	oldDataJSON, _ := json.Marshal(oldRecord)

	updatedAt := timeNow().Format("2006-01-02 15:04:05")
	_, err = database.DB.Exec(`
		UPDATE records SET status=?, operator=?, updated_at=?, updated_by=? WHERE id=?
	`, status, operator, updatedAt, operator, id)
	if err != nil {
		return err
	}

	// 更新记录对象用于审计
	oldRecord.Status = status
	oldRecord.UpdatedAt = updatedAt
	oldRecord.UpdatedBy = operator
	newDataJSON, _ := json.Marshal(oldRecord)

	statusText := map[string]string{"active": "启用", "inactive": "停用", "pending": "待定"}
	changes := fmt.Sprintf("状态: %s → %s", statusText[oldStatus], statusText[status])

	return AddAuditLog("update", id, operator, string(oldDataJSON), string(newDataJSON), changes, ip)
}

func generateChanges(old, new *models.Record) string {
	var changes []string

	if old.ConnectionID != new.ConnectionID {
		changes = append(changes, fmt.Sprintf("连接ID: %s → %s", old.ConnectionID, new.ConnectionID))
	}
	if old.Project != new.Project {
		changes = append(changes, fmt.Sprintf("项目: %s → %s", old.Project, new.Project))
	}
	if old.Env != new.Env {
		changes = append(changes, fmt.Sprintf("环境: %s → %s", old.Env, new.Env))
	}
	if old.VID != new.VID {
		changes = append(changes, fmt.Sprintf("VID: %s → %s", old.VID, new.VID))
	}
	if old.SrcIP != new.SrcIP {
		changes = append(changes, fmt.Sprintf("源IP: %s → %s", old.SrcIP, new.SrcIP))
	}
	if old.DestIP != new.DestIP {
		changes = append(changes, fmt.Sprintf("目标IP: %s → %s", old.DestIP, new.DestIP))
	}
	if old.Port != new.Port {
		changes = append(changes, fmt.Sprintf("端口: %s → %s", old.Port, new.Port))
	}
	if old.Status != new.Status {
		changes = append(changes, fmt.Sprintf("状态: %s → %s", old.Status, new.Status))
	}

	if len(changes) == 0 {
		return "无变更"
	}

	result := ""
	for i, c := range changes {
		if i > 0 {
			result += "; "
		}
		result += c
	}
	return result
}

// generateChangesJSON 生成JSON格式的变更记录
func generateChangesJSON(old, new *models.Record) string {
	changes := make(map[string]map[string]string)

	if old.Project != new.Project {
		changes["project"] = map[string]string{"old": old.Project, "new": new.Project}
	}
	if old.Env != new.Env {
		changes["env"] = map[string]string{"old": old.Env, "new": new.Env}
	}
	if old.VID != new.VID {
		changes["vid"] = map[string]string{"old": old.VID, "new": new.VID}
	}
	if old.SrcIP != new.SrcIP {
		changes["src_ip"] = map[string]string{"old": old.SrcIP, "new": new.SrcIP}
	}
	if old.DestIP != new.DestIP {
		changes["dest_ip"] = map[string]string{"old": old.DestIP, "new": new.DestIP}
	}
	if old.Port != new.Port {
		changes["port"] = map[string]string{"old": old.Port, "new": new.Port}
	}
	if old.Status != new.Status {
		changes["status"] = map[string]string{"old": old.Status, "new": new.Status}
	}
	if old.ConnectionID != new.ConnectionID {
		changes["connection_id"] = map[string]string{"old": old.ConnectionID, "new": new.ConnectionID}
	}

	if len(changes) == 0 {
		return "{}"
	}

	data, _ := json.Marshal(changes)
	return string(data)
}

// ========== 历史记录相关 ==========

// SaveRecordHistory 保存记录历史
func SaveRecordHistory(recordID, action string, record *models.Record, changes string, operator string) error {
	snapshot, _ := json.Marshal(record)
	id := uuid.New().String()
	createdAt := timeNow().Format("2006-01-02 15:04:05")

	_, err := database.DB.Exec(`
		INSERT INTO record_history (id, record_id, action, snapshot, changes, created_at, created_by)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`, id, recordID, action, string(snapshot), changes, createdAt, operator)
	return err
}

// HandleGetRecordHistory 获取记录历史
func HandleGetRecordHistory(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	recordID := vars["id"]

	rows, err := database.DB.Query(`
		SELECT id, record_id, action, snapshot, changes, created_at, created_by
		FROM record_history
		WHERE record_id = ?
		ORDER BY created_at DESC
	`, recordID)
	if err != nil {
		SafeError(w, "操作失败", http.StatusInternalServerError, err)
		return
	}
	defer rows.Close()

	type History struct {
		ID        string `json:"id"`
		RecordID  string `json:"record_id"`
		Action    string `json:"action"`
		Snapshot  string `json:"snapshot"`
		Changes   string `json:"changes"`
		CreatedAt string `json:"created_at"`
		CreatedBy string `json:"created_by"`
	}

	histories := []History{}
	for rows.Next() {
		var h History
		if err := rows.Scan(&h.ID, &h.RecordID, &h.Action, &h.Snapshot, &h.Changes, &h.CreatedAt, &h.CreatedBy); err != nil {
			continue
		}
		histories = append(histories, h)
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	json.NewEncoder(w).Encode(histories)
}

// HandleRollbackRecord 回滚记录到指定版本
func HandleRollbackRecord(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	recordID := vars["id"]

	var req struct {
		HistoryID string `json:"history_id"`
		Operator  string `json:"operator"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "无效的请求数据", http.StatusBadRequest)
		return
	}

	operator := req.Operator
	if operator == "" {
		operator = "system"
	}

	// 获取历史快照
	var snapshot string
	var historyCreatedBy string
	err := database.DB.QueryRow(`
		SELECT snapshot, created_by FROM record_history WHERE id = ? AND record_id = ?
	`, req.HistoryID, recordID).Scan(&snapshot, &historyCreatedBy)
	if err != nil {
		http.Error(w, "历史记录不存在", http.StatusNotFound)
		return
	}

	// 解析快照
	var oldRecord models.Record
	if err := json.Unmarshal([]byte(snapshot), &oldRecord); err != nil {
		http.Error(w, "解析快照失败", http.StatusInternalServerError)
		return
	}

	// 获取当前记录（用于保存回滚前的历史）
	currentRecord, err := GetRecord(recordID)
	if err != nil || currentRecord == nil {
		http.Error(w, "当前记录不存在", http.StatusNotFound)
		return
	}

	// 保存回滚前的历史
	ip := GetClientIP(r)
	SaveRecordHistory(recordID, "update", currentRecord, "回滚前备份", operator)

	// 更新记录为历史版本
	now := timeNow().Format("2006-01-02 15:04:05")
	_, err = database.DB.Exec(`
		UPDATE records SET connection_id=?, project=?, env=?, module=?, vid=?, src_ip=?, dest_ip=?, port=?, status=?, updated_at=?, updated_by=?
		WHERE id=?
	`, oldRecord.ConnectionID, oldRecord.Project, oldRecord.Env, oldRecord.Module, oldRecord.VID, oldRecord.SrcIP, oldRecord.DestIP, oldRecord.Port, oldRecord.Status, now, operator, recordID)
	if err != nil {
		http.Error(w, "回滚失败", http.StatusInternalServerError)
		return
	}

	// 保存回滚后的历史
	oldRecord.UpdatedAt = now
	oldRecord.UpdatedBy = operator
	SaveRecordHistory(recordID, "rollback", &oldRecord, "回滚到历史版本", operator)

	// 记录审计日志
	AddAuditLog("rollback", recordID, operator, "", snapshot, "回滚到历史版本", ip)

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	json.NewEncoder(w).Encode(map[string]string{"status": "success"})
}

