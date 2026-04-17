package handlers

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"opsplatform-probe-backend/database"
)

var auditChainMu sync.Mutex

// SaveAuditLog writes an audit log entry with a chained hash for tamper-evidence.
// The hash chain enables off-line verification: row N hash = sha256(row N-1 hash + fields).
// Also mirrors the entry to stdout (k8s logs) as a secondary append-only store.
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

	auditChainMu.Lock()
	defer auditChainMu.Unlock()

	// 上一条 row_hash
	var prevHash string
	database.DB.QueryRow("SELECT row_hash FROM audit_logs ORDER BY id DESC LIMIT 1").Scan(&prevHash)

	createdAt := time.Now().UTC().Format(time.RFC3339Nano)
	// 计算当前行 hash = sha256(prev_hash || fields)
	payload := strings.Join([]string{
		prevHash, username, authSource, action, targetType, targetName, detail, ip, createdAt,
	}, "|")
	sum := sha256.Sum256([]byte(payload))
	rowHash := hex.EncodeToString(sum[:])

	_, err := database.DB.Exec(`INSERT INTO audit_logs
		(username, auth_source, action, target_type, target_name, detail, ip, prev_hash, row_hash)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		username, authSource, action, targetType, targetName, detail, ip, prevHash, rowHash)
	if err != nil {
		log.Printf("[AUDIT][DB_FAIL] %v | %s %s %s %s %s", err, username, action, targetType, targetName, detail)
	}
	// Mirror to stdout (k8s logs, append-only from a DB admin's perspective)
	log.Printf("[AUDIT] user=%s src=%s action=%s type=%s target=%s ip=%s hash=%s",
		username, authSource, action, targetType, targetName, ip, rowHash[:16])
}

// VerifyAuditChain recomputes the full chain and returns the first tampered row id, or 0 if clean.
// Intended to be invoked manually by an admin via an endpoint or CLI job.
func VerifyAuditChain() (int64, error) {
	rows, err := database.DB.Query(`SELECT id, username, auth_source, action, target_type, target_name, detail, ip, created_at, prev_hash, row_hash FROM audit_logs ORDER BY id ASC`)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	prev := ""
	for rows.Next() {
		var id int64
		var username, authSource, action, targetType, targetName, detail, ip, createdAt, prevHash, rowHash string
		if err := rows.Scan(&id, &username, &authSource, &action, &targetType, &targetName, &detail, &ip, &createdAt, &prevHash, &rowHash); err != nil {
			return 0, err
		}
		if prevHash != prev {
			return id, fmt.Errorf("row %d prev_hash mismatch", id)
		}
		payload := strings.Join([]string{prevHash, username, authSource, action, targetType, targetName, detail, ip, createdAt}, "|")
		sum := sha256.Sum256([]byte(payload))
		if hex.EncodeToString(sum[:]) != rowHash && rowHash != "" {
			// Allow empty rowHash for legacy rows pre-migration
			return id, fmt.Errorf("row %d hash mismatch", id)
		}
		prev = rowHash
	}
	return 0, nil
}

func HandleVerifyAuditChain(w http.ResponseWriter, r *http.Request) {
	tamperedID, err := VerifyAuditChain()
	if err != nil {
		jsonSuccess(w, map[string]interface{}{
			"ok":          false,
			"tampered_at": tamperedID,
			"error":       err.Error(),
		})
		return
	}
	jsonSuccess(w, map[string]interface{}{"ok": true})
}

func HandleListAuditLogs(w http.ResponseWriter, r *http.Request) {
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	if page < 1 {
		page = 1
	}
	pageSize, _ := strconv.Atoi(r.URL.Query().Get("page_size"))
	if pageSize < 1 || pageSize > 200 {
		pageSize = 50
	}
	action := r.URL.Query().Get("action")
	username := r.URL.Query().Get("username")

	where := "WHERE 1=1"
	args := []interface{}{}
	if action != "" {
		where += " AND action = ?"
		args = append(args, action)
	}
	if username != "" {
		where += " AND username LIKE ?"
		args = append(args, "%"+username+"%")
	}

	var total int
	database.DB.QueryRow("SELECT COUNT(*) FROM audit_logs "+where, args...).Scan(&total)

	queryArgs := append(args, pageSize, (page-1)*pageSize)
	rows, err := database.DB.Query(
		"SELECT id, username, auth_source, action, target_type, target_name, detail, ip, created_at FROM audit_logs "+where+
			" ORDER BY id DESC LIMIT ? OFFSET ?", queryArgs...)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "查询失败")
		return
	}
	defer rows.Close()

	list := []map[string]interface{}{}
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

	jsonSuccess(w, map[string]interface{}{
		"list":  list,
		"total": total,
		"page":  page,
	})
	_ = json.NewEncoder
}
