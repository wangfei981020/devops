package handlers

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"netdoc/database"
	"netdoc/models"
)

// 为了测试方便，可替换的时间函数
var timeNow = time.Now

// HandleGetAuditLogs 获取审计日志
func HandleGetAuditLogs(w http.ResponseWriter, r *http.Request) {
	logs, err := GetAllAuditLogs()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	json.NewEncoder(w).Encode(logs)
}

// HandleExportRecords 导出记录
func HandleExportRecords(w http.ResponseWriter, r *http.Request) {
	records, err := GetAllRecords()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
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
			for _, field := range []string{rec.Project, rec.VID, rec.SrcIP, rec.DestIP, rec.Port} {
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
	writer.Write([]string{"ID", "项目", "环境", "VID", "源IP", "目标IP", "端口", "状态", "更新人", "更新时间"})
	for _, rec := range filtered {
		writer.Write([]string{
			rec.ID, rec.Project, rec.Env, rec.VID, rec.SrcIP, rec.DestIP,
			rec.Port, rec.Status, rec.UpdatedBy, rec.UpdatedAt,
		})
	}
	writer.Flush()
}

// HandleExportAuditLogs 导出审计日志
func HandleExportAuditLogs(w http.ResponseWriter, r *http.Request) {
	logs, err := GetAllAuditLogs()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
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
		http.Error(w, err.Error(), http.StatusInternalServerError)
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
		if port != "" && rec.Port != port {
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
		SELECT id, action, record_id, operator, COALESCE(old_data, ''), COALESCE(new_data, ''), changes, ip, created_at
		FROM audit_logs ORDER BY created_at DESC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var logs []*models.AuditLog
	for rows.Next() {
		l := &models.AuditLog{}
		err := rows.Scan(&l.ID, &l.Action, &l.RecordID, &l.Operator, &l.OldData, &l.NewData, &l.Changes, &l.IP, &l.CreatedAt)
		if err != nil {
			return nil, err
		}
		logs = append(logs, l)
	}
	return logs, nil
}

func AddAuditLog(action, recordID, operator, oldData, newData, changes, ip string) error {
	log := &models.AuditLog{
		ID:        fmt.Sprintf("log_%d", timeNow().UnixNano()),
		Action:    action,
		RecordID:  recordID,
		Operator:  operator,
		OldData:   oldData,
		NewData:   newData,
		Changes:   changes,
		IP:        ip,
		CreatedAt: timeNow().Format("2006-01-02 15:04:05"),
	}

	_, err := database.DB.Exec(`
		INSERT INTO audit_logs (id, action, record_id, operator, old_data, new_data, changes, ip, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, log.ID, log.Action, log.RecordID, log.Operator, log.OldData, log.NewData, log.Changes, log.IP, log.CreatedAt)
	return err
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





