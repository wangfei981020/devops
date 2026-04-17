package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/gorilla/mux"
	"opsplatform-probe-backend/database"
)

type TargetReq struct {
	Name           string   `json:"name"`
	Type           string   `json:"type"` // http/tcp/dns
	Target         string   `json:"target"`
	Port           int      `json:"port"`
	Method         string   `json:"method"`
	ExpectedStatus int      `json:"expected_status"`
	TimeoutSec     int      `json:"timeout_sec"`
	GroupName      string   `json:"group_name"`
	Description    string   `json:"description"`
	AgentScope     string   `json:"agent_scope"` // all/group/specific
	ScopeGroupID   int      `json:"scope_group_id"`
	AgentIDs       []string `json:"agent_ids"`
	Enabled        *bool    `json:"enabled"`
}

func validTargetType(t string) bool {
	return t == "http" || t == "tcp" || t == "dns"
}

func HandleListTargets(w http.ResponseWriter, r *http.Request) {
	rows, err := database.DB.Query(`SELECT id, name, type, target, port, method, expected_status, timeout_sec,
	   group_name, description, agent_scope, scope_group_id, enabled, created_by, created_at, updated_at
	  FROM probe_targets ORDER BY id DESC`)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "查询失败")
		return
	}
	defer rows.Close()
	list := []map[string]interface{}{}
	for rows.Next() {
		var id, port, status, timeout, scopeGroup, enabled int
		var name, ttype, addr, method, group, desc, scope, createdBy, createdAt, updatedAt string
		rows.Scan(&id, &name, &ttype, &addr, &port, &method, &status, &timeout, &group, &desc, &scope, &scopeGroup, &enabled, &createdBy, &createdAt, &updatedAt)
		list = append(list, map[string]interface{}{
			"id":              id,
			"name":            name,
			"type":            ttype,
			"target":          addr,
			"port":            port,
			"method":          method,
			"expected_status": status,
			"timeout_sec":     timeout,
			"group_name":      group,
			"description":     desc,
			"agent_scope":     scope,
			"scope_group_id":  scopeGroup,
			"enabled":         enabled == 1,
			"created_by":      createdBy,
			"created_at":      createdAt,
			"updated_at":      updatedAt,
		})
	}
	jsonSuccess(w, list)
}

func HandleCreateTarget(w http.ResponseWriter, r *http.Request) {
	var req TargetReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, http.StatusBadRequest, "invalid")
		return
	}
	if req.Name == "" || !validTargetType(req.Type) || req.Target == "" {
		jsonError(w, http.StatusBadRequest, "name/type/target required")
		return
	}
	if req.TimeoutSec <= 0 || req.TimeoutSec > 60 {
		req.TimeoutSec = 5
	}
	if req.AgentScope == "" {
		req.AgentScope = "all"
	}
	if req.Method == "" {
		req.Method = "GET"
	}
	enabled := 1
	if req.Enabled != nil && !*req.Enabled {
		enabled = 0
	}
	username := ""
	if v := r.Context().Value(contextUsername); v != nil {
		username = v.(string)
	}
	res, err := database.DB.Exec(
		`INSERT INTO probe_targets (name, type, target, port, method, expected_status, timeout_sec,
		   group_name, description, agent_scope, scope_group_id, enabled, created_by)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		req.Name, req.Type, req.Target, req.Port, req.Method, req.ExpectedStatus, req.TimeoutSec,
		req.GroupName, req.Description, req.AgentScope, req.ScopeGroupID, enabled, username,
	)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "创建失败")
		return
	}
	tid, _ := res.LastInsertId()
	if req.AgentScope == "specific" {
		for _, aid := range req.AgentIDs {
			database.DB.Exec("INSERT IGNORE INTO probe_target_agents (target_id, agent_id) VALUES (?, ?)", tid, aid)
		}
	}
	SaveAuditLog(r, "create_target", "target", req.Name, "")
	jsonSuccess(w, map[string]interface{}{"id": tid})
}

func HandleUpdateTarget(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
	var req TargetReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, http.StatusBadRequest, "invalid")
		return
	}
	if req.TimeoutSec <= 0 || req.TimeoutSec > 60 {
		req.TimeoutSec = 5
	}
	enabled := 1
	if req.Enabled != nil && !*req.Enabled {
		enabled = 0
	}

	// Read current scope to detect changes (fix #13 consistency)
	var oldScope string
	var oldScopeGroup, oldEnabled int
	database.DB.QueryRow(
		"SELECT agent_scope, scope_group_id, enabled FROM probe_targets WHERE id=?", id,
	).Scan(&oldScope, &oldScopeGroup, &oldEnabled)

	_, err := database.DB.Exec(
		`UPDATE probe_targets SET name=?, type=?, target=?, port=?, method=?, expected_status=?, timeout_sec=?,
		   group_name=?, description=?, agent_scope=?, scope_group_id=?, enabled=? WHERE id=?`,
		req.Name, req.Type, req.Target, req.Port, req.Method, req.ExpectedStatus, req.TimeoutSec,
		req.GroupName, req.Description, req.AgentScope, req.ScopeGroupID, enabled, id,
	)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "更新失败")
		return
	}
	if req.AgentScope == "specific" {
		database.DB.Exec("DELETE FROM probe_target_agents WHERE target_id=?", id)
		tid, _ := strconv.Atoi(id)
		for _, aid := range req.AgentIDs {
			database.DB.Exec("INSERT IGNORE INTO probe_target_agents (target_id, agent_id) VALUES (?, ?)", tid, aid)
		}
	}

	// Fix #13: 如果 scope/组/启用状态 发生变化, 把该目标所有未完成的 probe_tasks 置为 expired,
	// 避免"不该测的 Agent 还在拿老任务探测". 已 running 的 Agent 会在上报时被 409 拒绝.
	scopeChanged := oldScope != req.AgentScope || oldScopeGroup != req.ScopeGroupID
	enabledChanged := oldEnabled != enabled
	if scopeChanged || enabledChanged {
		res, _ := database.DB.Exec(
			"UPDATE probe_tasks SET status='expired' WHERE target_id=? AND status IN ('pending','running')",
			id,
		)
		if res != nil {
			if n, _ := res.RowsAffected(); n > 0 {
				SaveAuditLog(r, "target_scope_change_expire_tasks", "target", id, fmt.Sprintf("expired=%d", n))
			}
		}
	}

	SaveAuditLog(r, "update_target", "target", id, "")
	jsonSuccess(w, map[string]interface{}{"ok": true})
}

func HandleDeleteTarget(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
	database.DB.Exec("DELETE FROM probe_target_agents WHERE target_id=?", id)
	database.DB.Exec("DELETE FROM probe_targets WHERE id=?", id)
	SaveAuditLog(r, "delete_target", "target", id, "")
	jsonSuccess(w, map[string]interface{}{"ok": true})
}

// HandleListTargetAgents returns the list of agent IDs that a target is bound to (specific scope).
func HandleListTargetAgents(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
	rows, _ := database.DB.Query("SELECT agent_id FROM probe_target_agents WHERE target_id=?", id)
	defer rows.Close()
	list := []string{}
	for rows.Next() {
		var aid string
		rows.Scan(&aid)
		list = append(list, aid)
	}
	jsonSuccess(w, list)
}

// HandleGetEligibleAgents returns agents that may probe a given target (based on scope).
func HandleGetEligibleAgents(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
	var scope string
	var scopeGroupID int
	err := database.DB.QueryRow("SELECT agent_scope, scope_group_id FROM probe_targets WHERE id=?", id).Scan(&scope, &scopeGroupID)
	if err != nil {
		jsonError(w, http.StatusNotFound, "target not found")
		return
	}
	var q string
	args := []interface{}{}
	switch scope {
	case "all":
		q = `SELECT agent_id, hostname, status FROM agents WHERE approved=1 AND status<>'disabled'`
	case "group":
		q = `SELECT agent_id, hostname, status FROM agents WHERE approved=1 AND status<>'disabled' AND group_id=?`
		args = append(args, scopeGroupID)
	case "specific":
		q = `SELECT a.agent_id, a.hostname, a.status FROM agents a INNER JOIN probe_target_agents t ON t.agent_id=a.agent_id
		     WHERE t.target_id=? AND a.approved=1 AND a.status<>'disabled'`
		args = append(args, id)
	default:
		jsonError(w, http.StatusInternalServerError, "unknown scope")
		return
	}
	rows, err := database.DB.Query(q, args...)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()
	list := []map[string]interface{}{}
	for rows.Next() {
		var aid, host, st string
		rows.Scan(&aid, &host, &st)
		list = append(list, map[string]interface{}{"agent_id": aid, "hostname": host, "status": st})
	}
	jsonSuccess(w, list)
}

func init() {
	_ = strings.ToLower
}

// ====== Batch import ======

// BatchImportReq accepts a list of lines, each parsed into a target.
// Each line format examples:
//   http://www.baidu.com
//   https://api.example.com/health
//   redis.internal:6379           (TCP)
//   192.168.1.10:3306             (TCP)
//   example.com                   (DNS)
//
// Optional comma-separated columns: line,name,group
type BatchImportReq struct {
	Type         string `json:"type"`         // 'auto' or 'http'/'tcp'/'dns'
	Lines        string `json:"lines"`        // multiline text
	GroupName    string `json:"group_name"`   // applied to all
	AgentScope   string `json:"agent_scope"`  // applied to all
	ScopeGroupID int    `json:"scope_group_id"`
	TimeoutSec   int    `json:"timeout_sec"`
}

type BatchImportResult struct {
	Created int      `json:"created"`
	Skipped []string `json:"skipped"`
}

func HandleBatchImportTargets(w http.ResponseWriter, r *http.Request) {
	var req BatchImportReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, http.StatusBadRequest, "invalid")
		return
	}
	if req.TimeoutSec <= 0 {
		req.TimeoutSec = 5
	}
	if req.AgentScope == "" {
		req.AgentScope = "all"
	}
	if req.Type == "" {
		req.Type = "auto"
	}

	username := ""
	if v := r.Context().Value(contextUsername); v != nil {
		username = v.(string)
	}

	created := 0
	skipped := []string{}
	for _, raw := range strings.Split(req.Lines, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		// Check for per-line type prefix: "http ", "tcp ", "dns " (space or tab)
		lineType := req.Type
		lower := strings.ToLower(line)
		if strings.HasPrefix(lower, "http ") || strings.HasPrefix(lower, "http\t") {
			lineType = "http"
			line = strings.TrimSpace(line[4:])
		} else if strings.HasPrefix(lower, "tcp ") || strings.HasPrefix(lower, "tcp\t") {
			lineType = "tcp"
			line = strings.TrimSpace(line[3:])
		} else if strings.HasPrefix(lower, "dns ") || strings.HasPrefix(lower, "dns\t") {
			lineType = "dns"
			line = strings.TrimSpace(line[3:])
		}

		// Optional name after comma
		name := ""
		addrField := line
		if i := strings.Index(line, ","); i > 0 {
			addrField = strings.TrimSpace(line[:i])
			name = strings.TrimSpace(line[i+1:])
		}

		ttype, target, port := classifyTarget(addrField, lineType)
		if ttype == "" {
			skipped = append(skipped, line+" (无法识别)")
			continue
		}
		if name == "" {
			name = target
			if port > 0 {
				name = fmt.Sprintf("%s:%d", target, port)
			}
		}
		_, err := database.DB.Exec(
			`INSERT INTO probe_targets (name, type, target, port, method, timeout_sec,
			   group_name, agent_scope, scope_group_id, enabled, created_by)
			 VALUES (?, ?, ?, ?, 'GET', ?, ?, ?, ?, 1, ?)`,
			name, ttype, target, port, req.TimeoutSec,
			req.GroupName, req.AgentScope, req.ScopeGroupID, username,
		)
		if err != nil {
			skipped = append(skipped, line+" ("+err.Error()+")")
			continue
		}
		created++
	}
	SaveAuditLog(r, "batch_import_targets", "target", "", fmt.Sprintf("created=%d skipped=%d", created, len(skipped)))
	jsonSuccess(w, BatchImportResult{Created: created, Skipped: skipped})
}

// classifyTarget infers (type, host, port) from a single address string.
// hint can be 'auto' or a forced type ('http'/'tcp'/'dns').
func classifyTarget(s, hint string) (ttype, target string, port int) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", "", 0
	}
	// Forced type
	if hint == "http" {
		if !strings.HasPrefix(s, "http://") && !strings.HasPrefix(s, "https://") {
			s = "http://" + s
		}
		return "http", s, 0
	}
	if hint == "dns" {
		host := s
		if i := strings.Index(host, "://"); i >= 0 {
			host = host[i+3:]
		}
		if i := strings.IndexAny(host, "/:"); i >= 0 {
			host = host[:i]
		}
		return "dns", host, 0
	}
	if hint == "tcp" {
		host, p := splitHostPort(s)
		if p == 0 {
			return "", "", 0
		}
		return "tcp", host, p
	}
	// Auto-detect
	if strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://") {
		return "http", s, 0
	}
	if h, p := splitHostPort(s); p > 0 {
		return "tcp", h, p
	}
	// Plain hostname / IP -> DNS
	return "dns", s, 0
}

func splitHostPort(s string) (string, int) {
	if i := strings.LastIndex(s, ":"); i > 0 && i < len(s)-1 {
		host := s[:i]
		portStr := s[i+1:]
		p, err := strconv.Atoi(portStr)
		if err == nil && p > 0 && p <= 65535 {
			return host, p
		}
	}
	return s, 0
}
