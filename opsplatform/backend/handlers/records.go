package handlers

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"

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

	if req.Record.ConnectionID == "" || req.Record.Project == "" || req.Record.VID == "" || req.Record.SrcIP == "" || req.Record.DestIP == "" || req.Record.DestPort == "" {
		http.Error(w, "连接ID、项目、VID、源IP、目标IP、端口不能为空", http.StatusBadRequest)
		return
	}

	// 检查连接ID唯一性
	if exists, _ := ConnectionIDExists(req.Record.ConnectionID, ""); exists {
		// 查询现有记录的详细信息
		existingRecord, _ := GetRecordByConnectionID(req.Record.ConnectionID)
		if existingRecord != nil {
			http.Error(w, fmt.Sprintf("连接ID已存在，禁止添加！\n现有记录：项目=%s, VID=%s, 连接ID=%s", 
				existingRecord.Project, existingRecord.VID, existingRecord.ConnectionID), http.StatusBadRequest)
		} else {
			http.Error(w, "连接ID已存在，请使用不同的连接ID", http.StatusBadRequest)
		}
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

// HandleBatchCheckRecords 批量检测连接ID是否存在
func HandleBatchCheckRecords(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Records []models.Record `json:"records"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "请求格式错误", http.StatusBadRequest)
		return
	}

	if len(req.Records) == 0 {
		http.Error(w, "记录列表不能为空", http.StatusBadRequest)
		return
	}

	// 检测结果
	type CheckResult struct {
		ConnectionID string `json:"connection_id"`
		Project      string `json:"project"`
		Module       string `json:"module"`
		VID          string `json:"vid"`
		SrcAddr      string `json:"src_addr"`
		DestAddr     string `json:"dest_addr"`
		Exists       bool   `json:"exists"`
		ExistingInfo string `json:"existing_info,omitempty"` // 已存在记录的信息
	}

	existsRecords := make([]CheckResult, 0)
	newRecords := make([]CheckResult, 0)

	for _, record := range req.Records {
		srcAddr := record.SrcIP
		if record.SrcPort != "" {
			srcAddr = record.SrcIP + ":" + record.SrcPort
		}
		destAddr := record.DestIP
		if record.DestPort != "" {
			destAddr = record.DestIP + ":" + record.DestPort
		}

		result := CheckResult{
			ConnectionID: record.ConnectionID,
			Project:      record.Project,
			Module:       record.Module,
			VID:          record.VID,
			SrcAddr:      srcAddr,
			DestAddr:     destAddr,
		}

		// 检查连接ID是否存在
		if exists, _ := ConnectionIDExists(record.ConnectionID, ""); exists {
			result.Exists = true
			// 获取已存在记录的详细信息
			if existingRec, err := GetRecordByConnectionID(record.ConnectionID); err == nil && existingRec != nil {
				result.ExistingInfo = fmt.Sprintf("项目=%s, VID=%s, 源=%s:%s, 目标=%s:%s", 
					existingRec.Project, existingRec.VID, existingRec.SrcIP, existingRec.SrcPort, 
					existingRec.DestIP, existingRec.DestPort)
			}
			existsRecords = append(existsRecords, result)
		} else {
			result.Exists = false
			newRecords = append(newRecords, result)
		}
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"exists_count": len(existsRecords),
		"new_count":    len(newRecords),
		"exists":       existsRecords,
		"new":          newRecords,
	})
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
	
	// 第一遍：检查所有记录的连接ID是否有重复
	duplicateRecords := make([]string, 0)
	seenConnIds := make(map[string]models.Record) // 记录连接ID和对应的记录信息
	
	for _, record := range req.Records {
		if record.ConnectionID == "" {
			http.Error(w, "连接ID不能为空", http.StatusBadRequest)
			return
		}
		
		// 检查批次内是否有重复
		if existingRec, found := seenConnIds[record.ConnectionID]; found {
			duplicateRecords = append(duplicateRecords, fmt.Sprintf("连接ID=%s 批次内重复（项目=%s, VID=%s 与 项目=%s, VID=%s）", 
				record.ConnectionID, record.Project, record.VID, existingRec.Project, existingRec.VID))
		} else {
			seenConnIds[record.ConnectionID] = record
		}
		
		// 检查数据库中是否已存在
		if exists, _ := ConnectionIDExists(record.ConnectionID, ""); exists {
			existingRec, _ := GetRecordByConnectionID(record.ConnectionID)
			if existingRec != nil {
				duplicateRecords = append(duplicateRecords, fmt.Sprintf("连接ID=%s 数据库已存在（现有记录：项目=%s, VID=%s）", 
					record.ConnectionID, existingRec.Project, existingRec.VID))
			} else {
				duplicateRecords = append(duplicateRecords, record.ConnectionID+" (数据库已存在)")
			}
		}
	}
	
	// 如果有任何重复的连接ID，直接报错，不添加任何记录
	if len(duplicateRecords) > 0 {
		http.Error(w, fmt.Sprintf("存在重复的连接ID，禁止添加:\n%s", strings.Join(duplicateRecords, "\n")), http.StatusBadRequest)
		return
	}
	
	// 第二遍：添加所有记录
	addedRecords := make([]models.Record, 0, len(req.Records))
	failedRecords := make([]string, 0)
	
	for _, record := range req.Records {
		rec := record
		if err := AddRecord(&rec, req.Operator, ip); err != nil {
			log.Printf("[记录] 添加失败 %s: %v", record.ConnectionID, err)
			failedRecords = append(failedRecords, record.ConnectionID+" (添加失败)")
			continue
		}
		addedRecords = append(addedRecords, rec)
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	
	response := map[string]interface{}{
		"count":   len(addedRecords),
		"records": addedRecords,
		"message": fmt.Sprintf("成功添加 %d 条记录", len(addedRecords)),
	}
	
	if len(failedRecords) > 0 {
		response["failed"] = failedRecords
	}
	
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(response)
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

	if err := UpdateRecord(id, &req.Record, req.Operator, r); err != nil {
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

	if err := DeleteRecord(id, req.Operator, r); err != nil {
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

	deleted := 0
	for _, id := range req.IDs {
		if err := DeleteRecord(id, req.Operator, r); err == nil {
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

	updated := 0
	for _, id := range req.IDs {
		if err := UpdateRecordStatus(id, req.Status, req.Operator, r); err == nil {
			updated++
		}
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"message": fmt.Sprintf("成功更新 %d 条记录状态", updated),
		"count":   updated,
	})
}

// HandleBatchUpdate 批量更新记录
func HandleBatchUpdate(w http.ResponseWriter, r *http.Request) {
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

	updated := 0
	failed := 0
	for _, record := range req.Records {
		if record.ID == "" {
			failed++
			continue
		}
		if err := UpdateRecord(record.ID, &record, req.Operator, r); err != nil {
			failed++
			continue
		}
		updated++
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"message": fmt.Sprintf("成功更新 %d 条记录", updated),
		"count":   updated,
		"failed":  failed,
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
		SELECT id, COALESCE(connection_id, ''), project, env, COALESCE(module, ''), vid, 
		       src_ip, COALESCE(src_port, ''), dest_ip, dest_port, status, 
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
		err := rows.Scan(&r.ID, &r.ConnectionID, &r.Project, &r.Env, &r.Module, &r.VID, 
			&r.SrcIP, &r.SrcPort, &r.DestIP, &r.DestPort, &r.Status, 
			&r.Operator, &r.CreatedAt, &r.UpdatedAt, &r.CreatedBy, &r.UpdatedBy)
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

// GetRecordByConnectionID 根据连接ID获取记录
func GetRecordByConnectionID(connectionID string) (*models.Record, error) {
	r := &models.Record{}
	err := database.DB.QueryRow(`
		SELECT id, COALESCE(connection_id, ''), COALESCE(project, ''), COALESCE(env, 'UAT'), 
		       COALESCE(module, ''), COALESCE(vid, ''), 
		       COALESCE(src_ip, ''), COALESCE(src_port, ''), COALESCE(dest_ip, ''), COALESCE(dest_port, ''), 
		       COALESCE(status, 'active'), COALESCE(operator, ''), created_at, updated_at,
		       COALESCE(created_by, ''), COALESCE(updated_by, '')
		FROM records WHERE connection_id = ?
	`, connectionID).Scan(&r.ID, &r.ConnectionID, &r.Project, &r.Env, &r.Module, &r.VID, 
		&r.SrcIP, &r.SrcPort, &r.DestIP, &r.DestPort, &r.Status, 
		&r.Operator, &r.CreatedAt, &r.UpdatedAt, &r.CreatedBy, &r.UpdatedBy)
	if err != nil {
		return nil, err
	}
	return r, nil
}

func GetRecord(id string) (*models.Record, error) {
	r := &models.Record{}
	err := database.DB.QueryRow(`
		SELECT id, COALESCE(connection_id, ''), project, env, COALESCE(module, ''), vid, 
		       src_ip, COALESCE(src_port, ''), dest_ip, dest_port, status,
		       COALESCE(operator, ''), created_at, updated_at,
		       COALESCE(created_by, ''), COALESCE(updated_by, '')
		FROM records WHERE id = ?
	`, id).Scan(&r.ID, &r.ConnectionID, &r.Project, &r.Env, &r.Module, &r.VID, 
		&r.SrcIP, &r.SrcPort, &r.DestIP, &r.DestPort, &r.Status, 
		&r.Operator, &r.CreatedAt, &r.UpdatedAt, &r.CreatedBy, &r.UpdatedBy)
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
		INSERT INTO records (id, connection_id, project, env, module, vid, src_ip, src_port, dest_ip, dest_port, status, operator, created_at, updated_at, created_by, updated_by)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, r.ID, r.ConnectionID, r.Project, r.Env, r.Module, r.VID, r.SrcIP, r.SrcPort, r.DestIP, r.DestPort, r.Status, r.Operator, r.CreatedAt, r.UpdatedAt, r.CreatedBy, r.UpdatedBy)
	if err != nil {
		return err
	}

	// 保存历史记录
	SaveRecordHistory(r.ID, "create", r, "创建记录", operator)

	newDataJSON, _ := json.Marshal(r)
	return AddAuditLog("create", r.ID, operator, "", string(newDataJSON),
		fmt.Sprintf("创建记录: 连接ID=%s, 项目=%s, VID=%s, %s->%s:%s", r.ConnectionID, r.Project, r.VID, r.SrcIP, r.DestIP, r.DestPort), ip)
}

func UpdateRecord(id string, rec *models.Record, operator string, req *http.Request) error {
	oldRecord, err := GetRecord(id)
	if err != nil || oldRecord == nil {
		return fmt.Errorf("记录不存在: %s", id)
	}

	// 检查连接ID唯一性（排除当前记录）
	if rec.ConnectionID != oldRecord.ConnectionID {
		if exists, _ := ConnectionIDExists(rec.ConnectionID, id); exists {
			return fmt.Errorf("连接ID '%s' 已存在", rec.ConnectionID)
		}
	}

	oldDataJSON, _ := json.Marshal(oldRecord)
	changes := generateChanges(oldRecord, rec)
	changesJSON := generateChangesJSON(oldRecord, rec)

	rec.ID = id
	rec.CreatedAt = oldRecord.CreatedAt
	rec.CreatedBy = oldRecord.CreatedBy
	rec.UpdatedAt = timeNow().Format("2006-01-02 15:04:05")
	rec.UpdatedBy = operator

	_, err = database.DB.Exec(`
		UPDATE records SET connection_id=?, project=?, env=?, module=?, vid=?, src_ip=?, src_port=?, dest_ip=?, dest_port=?, status=?, operator=?, updated_at=?, updated_by=?
		WHERE id=?
	`, rec.ConnectionID, rec.Project, rec.Env, rec.Module, rec.VID, rec.SrcIP, rec.SrcPort, rec.DestIP, rec.DestPort, rec.Status, rec.Operator, rec.UpdatedAt, rec.UpdatedBy, id)
	if err != nil {
		return err
	}

	// 保存历史记录（保存更新后的版本）
	SaveRecordHistory(id, "update", rec, changesJSON, operator)

	newDataJSON, _ := json.Marshal(rec)
	return AddAuditLogFromRequest(req, "update", "record:"+id, operator, string(oldDataJSON), string(newDataJSON), changes)
}

func DeleteRecord(id string, operator string, req *http.Request) error {
	oldRecord, err := GetRecord(id)
	if err != nil || oldRecord == nil {
		return fmt.Errorf("记录不存在: %s", id)
	}

	oldDataJSON, _ := json.Marshal(oldRecord)

	_, err = database.DB.Exec("DELETE FROM records WHERE id=?", id)
	if err != nil {
		return err
	}

	return AddAuditLogFromRequest(req, "delete", "record:"+id, operator, string(oldDataJSON), "",
		fmt.Sprintf("删除记录: 项目=%s, VID=%s, %s->%s:%s", oldRecord.Project, oldRecord.VID, oldRecord.SrcIP, oldRecord.DestIP, oldRecord.DestPort))
}

// UpdateRecordStatus 更新记录状态
func UpdateRecordStatus(id, status, operator string, req *http.Request) error {
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

	return AddAuditLogFromRequest(req, "update", "record:"+id, operator, string(oldDataJSON), string(newDataJSON), changes)
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
	if old.DestPort != new.DestPort {
		changes = append(changes, fmt.Sprintf("端口: %s → %s", old.DestPort, new.DestPort))
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
	if old.DestPort != new.DestPort {
		changes["port"] = map[string]string{"old": old.DestPort, "new": new.DestPort}
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
	SaveRecordHistory(recordID, "update", currentRecord, "回滚前备份", operator)

	// 更新记录为历史版本
	now := timeNow().Format("2006-01-02 15:04:05")
	_, err = database.DB.Exec(`
		UPDATE records SET connection_id=?, project=?, env=?, module=?, vid=?, src_ip=?, src_port=?, dest_ip=?, dest_port=?, status=?, updated_at=?, updated_by=?
		WHERE id=?
	`, oldRecord.ConnectionID, oldRecord.Project, oldRecord.Env, oldRecord.Module, oldRecord.VID, oldRecord.SrcIP, oldRecord.SrcPort, oldRecord.DestIP, oldRecord.DestPort, oldRecord.Status, now, operator, recordID)
	if err != nil {
		http.Error(w, "回滚失败", http.StatusInternalServerError)
		return
	}

	// 保存回滚后的历史
	oldRecord.UpdatedAt = now
	oldRecord.UpdatedBy = operator
	SaveRecordHistory(recordID, "rollback", &oldRecord, "回滚到历史版本", operator)

	// 记录审计日志 - 包含回滚前和回滚后的完整数据
	currentSnapshot, _ := json.Marshal(currentRecord)
	rollbackSnapshot, _ := json.Marshal(oldRecord)
	AddAuditLogFromRequest(r, "rollback", recordID, operator, string(currentSnapshot), string(rollbackSnapshot), "回滚到历史版本")

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	json.NewEncoder(w).Encode(map[string]string{"status": "success"})
}

