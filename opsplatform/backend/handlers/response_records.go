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

// ResponseRecordPrefix v742: 响应记录附件存到主 bucket 的子目录下（不再用独立 bucket）
// 原因: 生产 MinIO 账号通常按 bucket 配置最小权限 (Resource: arn:aws:s3:::opsplatform/*)，
// 没有 CreateBucket 权限，所以复用主 bucket + 前缀区分更合理
const ResponseRecordPrefix = "response-records/"

// ResponderEntry v740: 单个响应人的时间记录
// v743: 加 MentionedAt 让每人有自己的"被艾特时间"（支持转交场景）
// v744: 加 Note 让 "未响应/仅响应" 状态有解释
// v746: 拆分 Reason (固定标签从预设选, 能聚合统计) + Note (自由备注, 补充说明)
type ResponderEntry struct {
	Responder   string `json:"responder"`
	MentionedAt string `json:"mentioned_at"` // T0
	RespondedAt string `json:"responded_at"` // T1 空 = 未响应
	CompletedAt string `json:"completed_at"` // T2 空 = 未完成
	Reason      string `json:"reason"`       // v746: 固定原因标签（从 response_reasons 预设选）
	Note        string `json:"note"`         // v744: 自由备注（可选补充说明）
}

// ResponseRecord 员工响应记录
type ResponseRecord struct {
	ID             int              `json:"id"`
	Responder      string           `json:"responder"`            // 兼容字段：首响应人
	Responders     []ResponderEntry `json:"responders"`           // v740: 多响应人
	MessageSource  string           `json:"message_source"`
	MessageContent string           `json:"message_content"`
	MentionedAt    string           `json:"mentioned_at"`         // T0 艾特/消息发出
	RespondedAt    string           `json:"responded_at"`         // 兼容字段：首响应时间
	CompletedAt    *string          `json:"completed_at,omitempty"`
	HasIncident    int              `json:"has_incident"`
	IncidentTicket string           `json:"incident_ticket"`
	HandleResult   string           `json:"handle_result"`
	Remark         string           `json:"remark"`
	Attachments    string           `json:"attachments"`
	Status         string           `json:"status"`
	CreatedAt      string           `json:"created_at"`
	CreatedBy      string           `json:"created_by"`
	UpdatedAt      string           `json:"updated_at"`
	UpdatedBy      string           `json:"updated_by"`
}

// deriveLegacyFields v740/v743: 从 responders 算出兼容字段
// v743 新语义:
//   - status: 至少 1 人 completed_at 非空 → completed；否则 processing (没人解决 = 还在处理)
//   - completed_at (主表): 已解决人里最晚的 completed_at；没人解决就 NULL
//   - responded_at (主表): 最早的非空 responded_at；没人响应就用 fallback (mainMentionedAt)
//   - responder (主表): 首位响应的人；没人响应就用第一个 responder 名
//   - mentioned_at 主表用调用方传入的 mainMentionedAt（外部固定，不靠 responders 派生）
func deriveLegacyFields(responders []ResponderEntry, mainMentionedAt string) (string, string, *string, string) {
	if len(responders) == 0 {
		return "", mainMentionedAt, nil, "processing"
	}

	// 1. 找最早响应的人（responded_at 非空里取 min）
	firstResponder, firstRespondedAt := "", ""
	for _, r := range responders {
		if r.RespondedAt == "" {
			continue
		}
		if firstRespondedAt == "" || r.RespondedAt < firstRespondedAt {
			firstRespondedAt = r.RespondedAt
			firstResponder = r.Responder
		}
	}
	// 没人响应 → 兼容字段降级用首位 responder + mainMentionedAt
	if firstResponder == "" {
		firstResponder = responders[0].Responder
		firstRespondedAt = mainMentionedAt
	}

	// 2. status: 至少 1 人 completed_at 非空 → completed
	status := "processing"
	maxCompleted := ""
	for _, r := range responders {
		if r.CompletedAt != "" {
			status = "completed"
			if r.CompletedAt > maxCompleted {
				maxCompleted = r.CompletedAt
			}
		}
	}
	var completedPtr *string
	if maxCompleted != "" {
		completedPtr = &maxCompleted
	}
	return firstResponder, firstRespondedAt, completedPtr, status
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

	// v741: 优先用自定义日期范围 start_date / end_date (YYYY-MM-DD，含两端全天)
	if start := q.Get("start_date"); start != "" {
		end := q.Get("end_date")
		if end == "" {
			end = start
		}
		// 包含 end_date 的全天 → 用 end+1day 作为右开区间
		endTime, err := time.Parse("2006-01-02", end)
		if err == nil {
			where = append(where, "mentioned_at >= ? AND mentioned_at < ?")
			args = append(args, start+" 00:00:00", endTime.AddDate(0, 0, 1).Format("2006-01-02")+" 00:00:00")
		}
	} else if year := q.Get("year"); year != "" {
		// 老接口兼容: ?year=&month=
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

	// v740: responder 筛选改成搜 responders JSON
	for i, w := range where {
		if w == "responder = ?" {
			where[i] = "responders LIKE ?"
			// 找到对应的 arg，加上 % 包围
			argIdx := 0
			for _, w2 := range where[:i] {
				argIdx += strings.Count(w2, "?")
			}
			if argIdx < len(args) {
				args[argIdx] = "%\"responder\":\"" + args[argIdx].(string) + "\"%"
			}
		}
	}

	sqlStr := "SELECT id, responder, IFNULL(responders, '[]'), message_source, message_content, mentioned_at, responded_at, completed_at, has_incident, incident_ticket, handle_result, remark, attachments, status, created_at, created_by, updated_at, updated_by FROM response_records WHERE " +
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
		var respondersJSON string
		var completed sql.NullString
		var mentioned, responded, created, updated time.Time
		if err := rows.Scan(&rec.ID, &rec.Responder, &respondersJSON, &rec.MessageSource, &rec.MessageContent,
			&mentioned, &responded, &completed, &rec.HasIncident, &rec.IncidentTicket,
			&rec.HandleResult, &rec.Remark, &rec.Attachments, &rec.Status,
			&created, &rec.CreatedBy, &updated, &rec.UpdatedBy); err != nil {
			continue
		}
		rec.MentionedAt = mentioned.Format(tsLayout)
		rec.RespondedAt = responded.Format(tsLayout)
		if completed.Valid {
			s := completed.String
			rec.CompletedAt = &s
		}
		rec.CreatedAt = created.Format(tsLayout)
		rec.UpdatedAt = updated.Format(tsLayout)
		if rec.Attachments == "" {
			rec.Attachments = "[]"
		}
		// 解析 responders JSON
		if respondersJSON != "" && respondersJSON != "[]" {
			_ = json.Unmarshal([]byte(respondersJSON), &rec.Responders)
		}
		out = append(out, rec)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(out)
}

type responseRecordPayload struct {
	Responders     []ResponderEntry `json:"responders"`
	MessageSource  string           `json:"message_source"`
	MessageContent string           `json:"message_content"`
	MentionedAt    string           `json:"mentioned_at"`
	HasIncident    int              `json:"has_incident"`
	IncidentTicket string           `json:"incident_ticket"`
	HandleResult   string           `json:"handle_result"`
	Remark         string           `json:"remark"`
	Attachments    string           `json:"attachments"`
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
	if len(p.Responders) == 0 || p.MessageContent == "" || p.MentionedAt == "" {
		http.Error(w, "responders / message_content / mentioned_at 必填", http.StatusBadRequest)
		return
	}
	// v743: responded_at / completed_at 都可空 (空 = 未响应 / 未解决)
	// 每行至少要有 responder 和 mentioned_at
	for i, rr := range p.Responders {
		if rr.Responder == "" {
			http.Error(w, fmt.Sprintf("responders[%d].responder 必填", i), http.StatusBadRequest)
			return
		}
		// mentioned_at 兜底：没填就用主表 mentioned_at
		if rr.MentionedAt == "" {
			p.Responders[i].MentionedAt = p.MentionedAt
		}
	}
	if p.Attachments == "" {
		p.Attachments = "[]"
	}

	// 兼容字段自动派生
	firstResponder, firstRespondedAt, completedPtr, status := deriveLegacyFields(p.Responders, p.MentionedAt)
	respondersJSON, _ := json.Marshal(p.Responders)
	operator := r.Header.Get("X-Operator")

	res, err := database.DB.Exec(`INSERT INTO response_records
		(responder, responders, message_source, message_content, mentioned_at, responded_at, completed_at,
		 has_incident, incident_ticket, handle_result, remark, attachments, status, created_by, updated_by)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		firstResponder, string(respondersJSON), p.MessageSource, p.MessageContent, p.MentionedAt,
		firstRespondedAt, sqlNullTime(completedPtr),
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
	if len(p.Responders) == 0 {
		http.Error(w, "至少 1 个响应人", http.StatusBadRequest)
		return
	}
	for i, rr := range p.Responders {
		if rr.Responder == "" {
			http.Error(w, fmt.Sprintf("responders[%d].responder 必填", i), http.StatusBadRequest)
			return
		}
		if rr.MentionedAt == "" {
			p.Responders[i].MentionedAt = p.MentionedAt
		}
	}
	if p.Attachments == "" {
		p.Attachments = "[]"
	}

	firstResponder, firstRespondedAt, completedPtr, status := deriveLegacyFields(p.Responders, p.MentionedAt)
	respondersJSON, _ := json.Marshal(p.Responders)
	operator := r.Header.Get("X-Operator")

	_, err = database.DB.Exec(`UPDATE response_records SET
		responder=?, responders=?, message_source=?, message_content=?, mentioned_at=?, responded_at=?, completed_at=?,
		has_incident=?, incident_ticket=?, handle_result=?, remark=?, attachments=?, status=?, updated_by=?
		WHERE id=?`,
		firstResponder, string(respondersJSON), p.MessageSource, p.MessageContent, p.MentionedAt,
		firstRespondedAt, sqlNullTime(completedPtr),
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

// ============================ 预设原因 CRUD ============================

type responseReason struct {
	ID        int    `json:"id"`
	Label     string `json:"label"`
	Category  string `json:"category"`
	SortOrder int    `json:"sort_order"`
	Status    string `json:"status"`
}

// HandleListResponseReasons GET /api/response-reasons
func HandleListResponseReasons(w http.ResponseWriter, r *http.Request) {
	rows, err := database.DB.Query(`SELECT id, label, category, sort_order, status FROM response_reasons WHERE status='active' ORDER BY category, sort_order, id`)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()
	out := []responseReason{}
	for rows.Next() {
		var s responseReason
		rows.Scan(&s.ID, &s.Label, &s.Category, &s.SortOrder, &s.Status)
		out = append(out, s)
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(out)
}

// HandleCreateResponseReason POST /api/response-reasons
func HandleCreateResponseReason(w http.ResponseWriter, r *http.Request) {
	var s responseReason
	if err := json.NewDecoder(r.Body).Decode(&s); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}
	if s.Label == "" {
		http.Error(w, "label 必填", http.StatusBadRequest)
		return
	}
	if s.Category == "" {
		s.Category = "all"
	}
	res, err := database.DB.Exec(`INSERT INTO response_reasons (label, category, sort_order, status) VALUES (?, ?, ?, 'active')`,
		s.Label, s.Category, s.SortOrder)
	if err != nil {
		if strings.Contains(err.Error(), "Duplicate") {
			http.Error(w, "label 已存在", http.StatusConflict)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	id, _ := res.LastInsertId()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"id": id})
}

// HandleUpdateResponseReason PUT /api/response-reasons/{id}
func HandleUpdateResponseReason(w http.ResponseWriter, r *http.Request) {
	idStr := strings.TrimPrefix(r.URL.Path, "/api/response-reasons/")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}
	var s responseReason
	if err := json.NewDecoder(r.Body).Decode(&s); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}
	_, err = database.DB.Exec(`UPDATE response_reasons SET label=?, category=?, sort_order=? WHERE id=?`,
		s.Label, s.Category, s.SortOrder, id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"success": true})
}

// HandleDeleteResponseReason DELETE /api/response-reasons/{id}
func HandleDeleteResponseReason(w http.ResponseWriter, r *http.Request) {
	idStr := strings.TrimPrefix(r.URL.Path, "/api/response-reasons/")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}
	_, err = database.DB.Exec(`UPDATE response_reasons SET status='disabled' WHERE id=?`, id)
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

	// v742: 走主 bucket + response-records/ 前缀（无需 CreateBucket 权限）
	fileURL, err := store.Upload(ctx, ResponseRecordPrefix+objectName, file, fileHeader.Size, contentType)
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
