package handlers

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"log"
	"net/http"
	"strings"
	"time"

	"opsplatform-probe-backend/database"
)

// ====== Agent registration ======

type RegisterReq struct {
	AgentID  string `json:"agent_id"`
	Hostname string `json:"hostname"`
	IP       string `json:"ip"`
	Version  string `json:"version"`
	OS       string `json:"os"`
	Arch     string `json:"arch"`
	Tags     string `json:"tags"`
}

type RegisterResp struct {
	AgentID  string `json:"agent_id"`
	Token    string `json:"token,omitempty"`
	Status   string `json:"status"`
	Approved bool   `json:"approved"`
	Message  string `json:"message,omitempty"`
}

// HandleAgentRegister - new Agent on first start, or re-register after binary replace.
// First call returns a fresh token; subsequent calls require X-Agent-ID + Bearer token.
func HandleAgentRegister(w http.ResponseWriter, r *http.Request) {
	// Rate limit by client IP (fix #8)
	if !checkRegisterLimit(w, r) {
		return
	}

	var req RegisterReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.AgentID == "" {
		jsonError(w, http.StatusBadRequest, "invalid register request")
		return
	}
	clientIP := getClientIP(r)
	if req.IP == "" {
		req.IP = clientIP
	}

	// Hard cap on pending agents to avoid DB filling attacks
	var pendingCount int
	database.DB.QueryRow("SELECT COUNT(*) FROM agents WHERE approved = 0").Scan(&pendingCount)
	if pendingCount > 500 {
		jsonError(w, http.StatusServiceUnavailable, "太多待审批 Agent, 请联系管理员")
		return
	}

	var dbID int
	var existingHash string
	err := database.DB.QueryRow("SELECT id, token_hash FROM agents WHERE agent_id = ?", req.AgentID).Scan(&dbID, &existingHash)
	if err != nil {
		// New agent: create + issue fresh token
		token := newRandomToken()
		hash := sha256.Sum256([]byte(token))
		tokenHash := hex.EncodeToString(hash[:])
		approved := 0
		status := "pending"
		if !cfg.AgentApprovalRequired {
			approved = 1
			status = "online"
		}
		// Token expiry (fix #2)
		var expiresAt interface{} = nil
		if cfg.AgentTokenExpireDays > 0 {
			expiresAt = time.Now().AddDate(0, 0, cfg.AgentTokenExpireDays)
		}
		_, err := database.DB.Exec(
			`INSERT INTO agents (agent_id, hostname, ip, version, os, arch, tags, token_hash, status, approved,
			   last_heartbeat_at, last_seen_ip, token_issued_at, token_expires_at)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, NOW(), ?, NOW(), ?)`,
			req.AgentID, req.Hostname, req.IP, req.Version, req.OS, req.Arch, req.Tags, tokenHash, status, approved, clientIP, expiresAt,
		)
		if err != nil {
			jsonError(w, http.StatusInternalServerError, "register failed")
			return
		}
		log.Printf("[register] new agent=%s ip=%s token=%s expires=%v", req.AgentID, clientIP, maskToken(token), expiresAt)
		jsonSuccess(w, RegisterResp{
			AgentID:  req.AgentID,
			Token:    token,
			Status:   status,
			Approved: approved == 1,
			Message:  "registered, waiting for approval",
		})
		return
	}

	// Existing agent: must come with valid token
	auth := r.Header.Get("Authorization")
	if !strings.HasPrefix(auth, "Bearer ") {
		jsonError(w, http.StatusUnauthorized, "agent already exists, token required to re-register")
		return
	}
	token := strings.TrimPrefix(auth, "Bearer ")
	hash := sha256.Sum256([]byte(token))
	tokenHash := hex.EncodeToString(hash[:])
	if tokenHash != existingHash {
		jsonError(w, http.StatusUnauthorized, "invalid token for existing agent")
		return
	}
	database.DB.Exec(
		`UPDATE agents SET hostname=?, ip=?, version=?, os=?, arch=?, tags=?, last_heartbeat_at=NOW(), last_seen_ip=? WHERE agent_id=?`,
		req.Hostname, req.IP, req.Version, req.OS, req.Arch, req.Tags, clientIP, req.AgentID,
	)
	var status string
	var approved int
	database.DB.QueryRow("SELECT status, approved FROM agents WHERE agent_id=?", req.AgentID).Scan(&status, &approved)
	jsonSuccess(w, RegisterResp{
		AgentID:  req.AgentID,
		Status:   status,
		Approved: approved == 1,
	})
}

// ====== Heartbeat ======

type HeartbeatReq struct {
	Version  string `json:"version"`
	Running  int    `json:"running_tasks"`
}

func HandleAgentHeartbeat(w http.ResponseWriter, r *http.Request) {
	agentID := currentAgentID(r)
	if !checkHeartbeatLimit(w, agentID) {
		return
	}
	var req HeartbeatReq
	json.NewDecoder(r.Body).Decode(&req)
	clientIP := getClientIP(r)
	database.DB.Exec(
		`UPDATE agents SET last_heartbeat_at=NOW(), last_seen_ip=?, version=COALESCE(NULLIF(?,''), version), status=CASE WHEN status='offline' THEN 'online' WHEN status='pending' THEN 'pending' ELSE 'online' END WHERE agent_id=?`,
		clientIP, req.Version, agentID,
	)
	jsonSuccess(w, map[string]interface{}{"ok": true, "server_time": time.Now().Unix()})
}

// ====== Task pull ======

func HandleAgentPullTasks(w http.ResponseWriter, r *http.Request) {
	agentID := currentAgentID(r)
	if !checkPullLimit(w, agentID) {
		return
	}
	limit := cfg.AgentTaskFetchLimit
	if limit <= 0 {
		limit = 20
	}

	probeRows, err := database.DB.Query(
		`SELECT id, batch_id, target_id, target_type, target_addr, target_port, method, timeout_sec
		   FROM probe_tasks WHERE agent_id=? AND status='pending' ORDER BY id ASC LIMIT ?`,
		agentID, limit,
	)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "pull failed")
		return
	}
	probeTasks := []map[string]interface{}{}
	probeIDs := []int64{}
	for probeRows.Next() {
		var id int64
		var batchID, ttype, addr, method string
		var targetID, port, timeout int
		probeRows.Scan(&id, &batchID, &targetID, &ttype, &addr, &port, &method, &timeout)
		probeTasks = append(probeTasks, map[string]interface{}{
			"task_id":     id,
			"batch_id":    batchID,
			"target_id":   targetID,
			"type":        ttype,
			"target":      addr,
			"port":        port,
			"method":      method,
			"timeout_sec": timeout,
		})
		probeIDs = append(probeIDs, id)
	}
	probeRows.Close()
	if len(probeIDs) > 0 {
		// Mark fetched
		placeholders := strings.Repeat("?,", len(probeIDs))
		placeholders = placeholders[:len(placeholders)-1]
		args := make([]interface{}, len(probeIDs))
		for i, id := range probeIDs {
			args[i] = id
		}
		database.DB.Exec("UPDATE probe_tasks SET status='running', fetched_at=NOW() WHERE id IN ("+placeholders+")", args...)
	}

	// Pull pending upgrade tasks
	upgRows, err := database.DB.Query(
		`SELECT id, to_version FROM agent_upgrade_tasks WHERE agent_id=? AND status='pending' ORDER BY id ASC LIMIT 1`,
		agentID,
	)
	upgradeTasks := []map[string]interface{}{}
	upgIDs := []int64{}
	if err == nil {
		for upgRows.Next() {
			var id int64
			var toVer string
			upgRows.Scan(&id, &toVer)
			// Lookup version metadata
			var sha string
			var size int64
			database.DB.QueryRow("SELECT sha256, size_bytes FROM agent_versions WHERE version=?", toVer).Scan(&sha, &size)
			upgradeTasks = append(upgradeTasks, map[string]interface{}{
				"task_id":  id,
				"version":  toVer,
				"sha256":   sha,
				"size":     size,
				"download": "/api/agent/upgrade/download?version=" + toVer,
			})
			upgIDs = append(upgIDs, id)
		}
		upgRows.Close()
		for _, id := range upgIDs {
			database.DB.Exec("UPDATE agent_upgrade_tasks SET status='downloading', fetched_at=NOW() WHERE id=?", id)
		}
	}

	jsonSuccess(w, map[string]interface{}{
		"probe_tasks":   probeTasks,
		"upgrade_tasks": upgradeTasks,
		"server_time":   time.Now().Unix(),
	})
}

// ====== Result reporting ======

type ProbeResultReq struct {
	TaskID     int64  `json:"task_id"`
	Success    bool   `json:"success"`
	LatencyMs  int    `json:"latency_ms"`
	StatusCode int    `json:"status_code"`
	ResolvedIP string `json:"resolved_ip"`
	Error      string `json:"error"`
}

func HandleAgentReportResult(w http.ResponseWriter, r *http.Request) {
	agentID := currentAgentID(r)
	if !checkReportLimit(w, agentID) {
		return
	}
	var req ProbeResultReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, http.StatusBadRequest, "invalid result")
		return
	}
	if len(req.Error) > 1000 {
		req.Error = req.Error[:1000]
	}

	// Strict ownership check (fix #4): task must belong to this agent AND be in running state
	var batchID, ttype, addr, taskStatus string
	var targetID, port int
	err := database.DB.QueryRow(
		"SELECT batch_id, target_id, target_type, target_addr, target_port, status FROM probe_tasks WHERE id=? AND agent_id=?",
		req.TaskID, agentID,
	).Scan(&batchID, &targetID, &ttype, &addr, &port, &taskStatus)
	if err != nil {
		jsonError(w, http.StatusNotFound, "task not found or not owned by this agent")
		return
	}
	if taskStatus == "done" || taskStatus == "expired" {
		jsonError(w, http.StatusConflict, "task already finalized")
		return
	}

	var targetName string
	database.DB.QueryRow("SELECT name FROM probe_targets WHERE id=?", targetID).Scan(&targetName)

	successInt := 0
	if req.Success {
		successInt = 1
	}
	database.DB.Exec(
		`INSERT INTO probe_results (task_id, batch_id, agent_id, target_id, target_name, target_type, target_addr, target_port,
		   success, latency_ms, status_code, resolved_ip, error)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		req.TaskID, batchID, agentID, targetID, targetName, ttype, addr, port,
		successInt, req.LatencyMs, req.StatusCode, req.ResolvedIP, req.Error,
	)
	database.DB.Exec("UPDATE probe_tasks SET status='done' WHERE id=?", req.TaskID)
	database.DB.Exec("UPDATE probe_batches SET done_tasks = done_tasks + 1 WHERE batch_id=?", batchID)

	jsonSuccess(w, map[string]interface{}{"ok": true})
}

// ====== Upgrade status reporting ======

type UpgradeStatusReq struct {
	TaskID  int64  `json:"task_id"`
	Status  string `json:"status"` // downloading/upgrading/success/failed/rollback
	Version string `json:"version"`
	Error   string `json:"error"`
}

func HandleAgentUpgradeStatus(w http.ResponseWriter, r *http.Request) {
	agentID := currentAgentID(r)
	var req UpgradeStatusReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, http.StatusBadRequest, "invalid")
		return
	}
	allowed := map[string]bool{"downloading": true, "upgrading": true, "success": true, "failed": true, "rollback": true}
	if !allowed[req.Status] {
		jsonError(w, http.StatusBadRequest, "invalid status")
		return
	}
	if len(req.Error) > 1000 {
		req.Error = req.Error[:1000]
	}
	finished := req.Status == "success" || req.Status == "failed" || req.Status == "rollback"
	if finished {
		database.DB.Exec("UPDATE agent_upgrade_tasks SET status=?, error=?, finished_at=NOW() WHERE id=? AND agent_id=?",
			req.Status, req.Error, req.TaskID, agentID)
		if req.Status == "success" && req.Version != "" {
			database.DB.Exec("UPDATE agents SET version=? WHERE agent_id=?", req.Version, agentID)
		}
	} else {
		database.DB.Exec("UPDATE agent_upgrade_tasks SET status=?, error=? WHERE id=? AND agent_id=?",
			req.Status, req.Error, req.TaskID, agentID)
	}
	jsonSuccess(w, map[string]interface{}{"ok": true})
}

// ====== Helpers ======

func newRandomToken() string {
	b := make([]byte, 32)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func getClientIP(r *http.Request) string {
	if v := r.Header.Get("X-Forwarded-For"); v != "" {
		return strings.Split(v, ",")[0]
	}
	if v := r.Header.Get("X-Real-IP"); v != "" {
		return v
	}
	host := r.RemoteAddr
	if i := strings.LastIndex(host, ":"); i >= 0 {
		host = host[:i]
	}
	return host
}
