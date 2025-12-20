package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"

	"netdoc/database"
	"netdoc/models"

	"github.com/gorilla/mux"
)

// HandleGetRecords 获取所有记录
func HandleGetRecords(w http.ResponseWriter, r *http.Request) {
	records, err := GetAllRecords()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
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
		http.Error(w, err.Error(), http.StatusInternalServerError)
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

	if req.Record.Project == "" || req.Record.VID == "" || req.Record.SrcIP == "" || req.Record.DestIP == "" || req.Record.Port == "" {
		http.Error(w, "项目、VID、源IP、目标IP、端口不能为空", http.StatusBadRequest)
		return
	}

	ip := GetClientIP(r)
	if err := AddRecord(&req.Record, req.Operator, ip); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
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

	for _, record := range req.Records {
		rec := record
		if err := AddRecord(&rec, req.Operator, ip); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
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
		http.Error(w, err.Error(), http.StatusNotFound)
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
		http.Error(w, err.Error(), http.StatusNotFound)
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
		SELECT id, project, env, vid, src_ip, dest_ip, port, status, 
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
		err := rows.Scan(&r.ID, &r.Project, &r.Env, &r.VID, &r.SrcIP, &r.DestIP,
			&r.Port, &r.Status, &r.Operator, &r.CreatedAt, &r.UpdatedAt,
			&r.CreatedBy, &r.UpdatedBy)
		if err != nil {
			return nil, err
		}
		records = append(records, r)
	}
	return records, nil
}

func GetRecord(id string) (*models.Record, error) {
	r := &models.Record{}
	err := database.DB.QueryRow(`
		SELECT id, project, env, vid, src_ip, dest_ip, port, status,
		       COALESCE(operator, ''), created_at, updated_at,
		       COALESCE(created_by, ''), COALESCE(updated_by, '')
		FROM records WHERE id = ?
	`, id).Scan(&r.ID, &r.Project, &r.Env, &r.VID, &r.SrcIP, &r.DestIP,
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
		INSERT INTO records (id, project, env, vid, src_ip, dest_ip, port, status, operator, created_at, updated_at, created_by, updated_by)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, r.ID, r.Project, r.Env, r.VID, r.SrcIP, r.DestIP, r.Port, r.Status, r.Operator, r.CreatedAt, r.UpdatedAt, r.CreatedBy, r.UpdatedBy)
	if err != nil {
		return err
	}

	newDataJSON, _ := json.Marshal(r)
	return AddAuditLog("create", r.ID, operator, "", string(newDataJSON),
		fmt.Sprintf("创建记录: 项目=%s, VID=%s, %s->%s:%s", r.Project, r.VID, r.SrcIP, r.DestIP, r.Port), ip)
}

func UpdateRecord(id string, r *models.Record, operator, ip string) error {
	oldRecord, err := GetRecord(id)
	if err != nil || oldRecord == nil {
		return fmt.Errorf("记录不存在: %s", id)
	}

	oldDataJSON, _ := json.Marshal(oldRecord)
	changes := generateChanges(oldRecord, r)

	r.ID = id
	r.CreatedAt = oldRecord.CreatedAt
	r.CreatedBy = oldRecord.CreatedBy
	r.UpdatedAt = timeNow().Format("2006-01-02 15:04:05")
	r.UpdatedBy = operator

	_, err = database.DB.Exec(`
		UPDATE records SET project=?, env=?, vid=?, src_ip=?, dest_ip=?, port=?, status=?, operator=?, updated_at=?, updated_by=?
		WHERE id=?
	`, r.Project, r.Env, r.VID, r.SrcIP, r.DestIP, r.Port, r.Status, r.Operator, r.UpdatedAt, r.UpdatedBy, id)
	if err != nil {
		return err
	}

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

func generateChanges(old, new *models.Record) string {
	var changes []string

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




