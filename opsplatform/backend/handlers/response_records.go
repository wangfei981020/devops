package handlers

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"opsplatform/database"
	"opsplatform/storage"

	"github.com/google/uuid"
)

// ResponseRecordBucket 响应记录附件专用 bucket（v739：跟桌台维护那套解耦）
const ResponseRecordBucket = "response-records"

// ResponseRecord 员工响应记录
type ResponseRecord struct {
	ID             int     `json:"id"`
	Responder      string  `json:"responder"`
	MessageSource  string  `json:"message_source"`
	MessageContent string  `json:"message_content"`
	MentionedAt    string  `json:"mentioned_at"`            // T0 艾特/消息发出
	RespondedAt    string  `json:"responded_at"`            // T1 开始响应
	CompletedAt    *string `json:"completed_at,omitempty"`  // T2 处理完成（可空）
	HasIncident    int     `json:"has_incident"`
	IncidentTicket string  `json:"incident_ticket"`
	HandleResult   string  `json:"handle_result"`
	Remark         string  `json:"remark"`
	Attachments    string  `json:"attachments"` // JSON 数组字符串
	Status         string  `json:"status"`
	CreatedAt      string  `json:"created_at"`
	CreatedBy      string  `json:"created_by"`
	UpdatedAt      string  `json:"updated_at"`
	UpdatedBy      string  `json:"updated_by"`
}

const tsLayout = "2006-01-02 15:04:05"

// HandleListResponseRecords GET /api/response-records?year=&month=&responder=&source=&has_incident=
func HandleListResponseRecords(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	q := r.URL.Query()
	where := []string{"1=1"}
	args := []interface{}{}

	if year := q.Get("year"); year != "" {
		if month := q.Get("month"); month != "" {
			y, _ := strconv.Atoi(year)
			m, _ := strconv.Atoi(month)
			start := time.Date(y, time.Month(m), 1, 0, 0, 0, 0, time.UTC)
			end := start.AddDate(0, 1, 0)
			where = append(where, "mentioned_at >= ? AND mentioned_at < ?")
			args = append(args, start.Format(tsLayout), end.Format(tsLayout))
		}
	}
	if responder := q.Get("responder"); responder != "" {
		where = append(where, "responder = ?")
		args = append(args, responder)
	}
	if source := q.Get("source"); source != "" {
		where = append(where, "message_source = ?")
		args = append(args, source)
	}
	if hi := q.Get("has_incident"); hi == "1" {
		where = append(where, "has_incident = 1")
	}
	if kw := q.Get("keyword"); kw != "" {
		where = append(where, "(message_content LIKE ? OR responder LIKE ? OR incident_ticket LIKE ?)")
		like := "%" + kw + "%"
		args = append(args, like, like, like)
	}

	sqlStr := "SELECT id, responder, message_source, message_content, mentioned_at, responded_at, completed_at, has_incident, incident_ticket, handle_result, remark, attachments, status, created_at, created_by, updated_at, updated_by FROM response_records WHERE " +
		strings.Join(where, " AND ") + " ORDER BY mentioned_at DESC"

	rows, err := database.DB.Query(sqlStr, args...)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	out := []ResponseRecord{}
	for rows.Next() {
		var rec ResponseRecord
		var completed sql.NullString
		var mentioned, responded, created, updated time.Time
		if err := rows.Scan(&rec.ID, &rec.Responder, &rec.MessageSource, &rec.MessageContent,
			&mentioned, &responded, &completed, &rec.HasIncident, &rec.IncidentTicket,
			&rec.HandleResult, &rec.Remark, &rec.Attachments, &rec.Status,
			&created, &rec.CreatedBy, &updated, &rec.UpdatedBy); err != nil {
			continue
		}
		rec.MentionedAt = mentioned.Format(tsLayout)
		rec.RespondedAt = responded.Format(tsLayout)
		if completed.Valid {
			s := completed.String
			// MySQL driver 返回的是 "2026-05-29 10:00:00" 格式，原样保留
			rec.CompletedAt = &s
		}
		rec.CreatedAt = created.Format(tsLayout)
		rec.UpdatedAt = updated.Format(tsLayout)
		if rec.Attachments == "" {
			rec.Attachments = "[]"
		}
		out = append(out, rec)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(out)
}

type responseRecordPayload struct {
	Responder      string  `json:"responder"`
	MessageSource  string  `json:"message_source"`
	MessageContent string  `json:"message_content"`
	MentionedAt    string  `json:"mentioned_at"`
	RespondedAt    string  `json:"responded_at"`
	CompletedAt    *string `json:"completed_at"`
	HasIncident    int     `json:"has_incident"`
	IncidentTicket string  `json:"incident_ticket"`
	HandleResult   string  `json:"handle_result"`
	Remark         string  `json:"remark"`
	Attachments    string  `json:"attachments"`
}

// HandleCreateResponseRecord POST /api/response-records
func HandleCreateResponseRecord(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var p responseRecordPayload
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}
	if p.Responder == "" || p.MessageContent == "" || p.MentionedAt == "" || p.RespondedAt == "" {
		http.Error(w, "responder / message_content / mentioned_at / responded_at 必填", http.StatusBadRequest)
		return
	}
	status := "processing"
	if p.CompletedAt != nil && *p.CompletedAt != "" {
		status = "completed"
	}
	if p.Attachments == "" {
		p.Attachments = "[]"
	}
	operator := r.Header.Get("X-Operator")

	res, err := database.DB.Exec(`INSERT INTO response_records
		(responder, message_source, message_content, mentioned_at, responded_at, completed_at,
		 has_incident, incident_ticket, handle_result, remark, attachments, status, created_by, updated_by)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		p.Responder, p.MessageSource, p.MessageContent, p.MentionedAt, p.RespondedAt, sqlNullTime(p.CompletedAt),
		p.HasIncident, p.IncidentTicket, p.HandleResult, p.Remark, p.Attachments, status, operator, operator)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	id, _ := res.LastInsertId()

	AddAuditLogFromRequest(r, "response_record_create", "response_record:"+strconv.FormatInt(id, 10), operator, "", "", "")

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"id": id})
}

// HandleUpdateResponseRecord PUT /api/response-records/{id}
func HandleUpdateResponseRecord(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	idStr := strings.TrimPrefix(r.URL.Path, "/api/response-records/")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}
	var p responseRecordPayload
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}
	status := "processing"
	if p.CompletedAt != nil && *p.CompletedAt != "" {
		status = "completed"
	}
	if p.Attachments == "" {
		p.Attachments = "[]"
	}
	operator := r.Header.Get("X-Operator")

	_, err = database.DB.Exec(`UPDATE response_records SET
		responder=?, message_source=?, message_content=?, mentioned_at=?, responded_at=?, completed_at=?,
		has_incident=?, incident_ticket=?, handle_result=?, remark=?, attachments=?, status=?, updated_by=?
		WHERE id=?`,
		p.Responder, p.MessageSource, p.MessageContent, p.MentionedAt, p.RespondedAt, sqlNullTime(p.CompletedAt),
		p.HasIncident, p.IncidentTicket, p.HandleResult, p.Remark, p.Attachments, status, operator, id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	AddAuditLogFromRequest(r, "response_record_update", "response_record:"+strconv.Itoa(id), operator, "", "", "")

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"success": true})
}

// HandleDeleteResponseRecord DELETE /api/response-records/{id}
func HandleDeleteResponseRecord(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	idStr := strings.TrimPrefix(r.URL.Path, "/api/response-records/")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}
	_, err = database.DB.Exec("DELETE FROM response_records WHERE id=?", id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	operator := r.Header.Get("X-Operator")
	AddAuditLogFromRequest(r, "response_record_delete", "response_record:"+strconv.Itoa(id), operator, "", "", "")

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"success": true})
}

func sqlNullTime(s *string) interface{} {
	if s == nil || *s == "" {
		return nil
	}
	return *s
}

// ============================ 消息来源 CRUD ============================

type responseSource struct {
	ID        int    `json:"id"`
	Code      string `json:"code"`
	Label     string `json:"label"`
	Color     string `json:"color"`
	SortOrder int    `json:"sort_order"`
	Status    string `json:"status"`
}

// HandleListResponseSources GET /api/response-record-sources
func HandleListResponseSources(w http.ResponseWriter, r *http.Request) {
	rows, err := database.DB.Query(`SELECT id, code, label, color, sort_order, status FROM response_record_sources WHERE status='active' ORDER BY sort_order, id`)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()
	out := []responseSource{}
	for rows.Next() {
		var s responseSource
		rows.Scan(&s.ID, &s.Code, &s.Label, &s.Color, &s.SortOrder, &s.Status)
		out = append(out, s)
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(out)
}

// HandleCreateResponseSource POST /api/response-record-sources
func HandleCreateResponseSource(w http.ResponseWriter, r *http.Request) {
	var s responseSource
	if err := json.NewDecoder(r.Body).Decode(&s); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}
	if s.Code == "" || s.Label == "" {
		http.Error(w, "code 和 label 必填", http.StatusBadRequest)
		return
	}
	if s.Color == "" {
		s.Color = "#94a3b8"
	}
	res, err := database.DB.Exec(`INSERT INTO response_record_sources (code, label, color, sort_order, status) VALUES (?, ?, ?, ?, 'active')`,
		s.Code, s.Label, s.Color, s.SortOrder)
	if err != nil {
		if strings.Contains(err.Error(), "Duplicate") {
			http.Error(w, "code 已存在", http.StatusConflict)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	id, _ := res.LastInsertId()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"id": id})
}

// HandleUpdateResponseSource PUT /api/response-record-sources/{id}
func HandleUpdateResponseSource(w http.ResponseWriter, r *http.Request) {
	idStr := strings.TrimPrefix(r.URL.Path, "/api/response-record-sources/")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}
	var s responseSource
	if err := json.NewDecoder(r.Body).Decode(&s); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}
	_, err = database.DB.Exec(`UPDATE response_record_sources SET label=?, color=?, sort_order=? WHERE id=?`,
		s.Label, s.Color, s.SortOrder, id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"success": true})
}

// HandleDeleteResponseSource DELETE /api/response-record-sources/{id}
func HandleDeleteResponseSource(w http.ResponseWriter, r *http.Request) {
	idStr := strings.TrimPrefix(r.URL.Path, "/api/response-record-sources/")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}
	// 软删除：置 disabled 而非物理删除（保护历史记录的来源码引用）
	_, err = database.DB.Exec(`UPDATE response_record_sources SET status='disabled' WHERE id=?`, id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"success": true})
}

// ============================ 附件上传（独立 bucket） ============================

// HandleResponseAttachmentUpload POST /api/response-records/upload
// 上传到独立 bucket response-records，复用桌台维护的图片白名单 + 10MB 限制
func HandleResponseAttachmentUpload(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		http.Error(w, "解析表单失败", http.StatusBadRequest)
		return
	}
	file, fileHeader, err := r.FormFile("file")
	if err != nil {
		http.Error(w, "未选择文件", http.StatusBadRequest)
		return
	}
	defer file.Close()

	allowedExts := map[string]bool{".jpg": true, ".jpeg": true, ".png": true, ".gif": true, ".webp": true, ".bmp": true,
		".pdf": true, ".doc": true, ".docx": true, ".xls": true, ".xlsx": true, ".txt": true, ".log": true}
	contentTypes := map[string]string{
		".jpg": "image/jpeg", ".jpeg": "image/jpeg", ".png": "image/png", ".gif": "image/gif",
		".webp": "image/webp", ".bmp": "image/bmp", ".pdf": "application/pdf",
		".doc": "application/msword", ".docx": "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
		".xls": "application/vnd.ms-excel", ".xlsx": "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
		".txt": "text/plain", ".log": "text/plain",
	}

	ext := strings.ToLower(filepath.Ext(fileHeader.Filename))
	if !allowedExts[ext] {
		http.Error(w, fmt.Sprintf("不支持的文件类型: %s", ext), http.StatusBadRequest)
		return
	}
	if fileHeader.Size == 0 {
		http.Error(w, "文件为空", http.StatusBadRequest)
		return
	}
	if fileHeader.Size > 10*1024*1024 {
		http.Error(w, "文件超过 10MB", http.StatusBadRequest)
		return
	}

	objectName := fmt.Sprintf("%s_%s%s", time.Now().Format("20060102150405"), uuid.New().String()[:8], ext)
	contentType := contentTypes[ext]
	if contentType == "" {
		contentType = "application/octet-stream"
	}

	store := storage.GetStorage()
	if store == nil {
		http.Error(w, "MinIO 未初始化", http.StatusInternalServerError)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	// 确保 bucket 存在（启动时已建，这里兜底）
	if err := store.EnsureBucket(ctx, ResponseRecordBucket); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	fileURL, err := store.UploadTo(ctx, ResponseRecordBucket, objectName, file, fileHeader.Size, contentType)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"path": fileURL,
		"name": fileHeader.Filename,
		"size": fileHeader.Size,
	})
}
