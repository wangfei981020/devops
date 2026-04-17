package handlers

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/gorilla/mux"
	"opsplatform-probe-backend/database"
)

// HandleListAgents returns all agents with status info.
func HandleListAgents(w http.ResponseWriter, r *http.Request) {
	status := r.URL.Query().Get("status")
	groupID := r.URL.Query().Get("group_id")
	q := `SELECT a.id, a.agent_id, a.hostname, a.ip, a.version, a.os, a.arch, a.tags, a.group_id,
	       a.status, a.approved, a.approved_by, a.approved_at, a.last_heartbeat_at, a.last_seen_ip, a.created_at,
	       COALESCE(g.name, '') as group_name
	      FROM agents a LEFT JOIN agent_groups g ON g.id = a.group_id WHERE 1=1`
	args := []interface{}{}
	if status != "" {
		q += " AND a.status = ?"
		args = append(args, status)
	}
	if groupID != "" {
		q += " AND a.group_id = ?"
		args = append(args, groupID)
	}
	q += " ORDER BY a.id DESC"
	rows, err := database.DB.Query(q, args...)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "查询失败")
		return
	}
	defer rows.Close()

	// Auto-mark offline if heartbeat too old
	timeout := cfg.AgentHeartbeatTimeout
	if timeout <= 0 {
		timeout = 30
	}
	database.DB.Exec("UPDATE agents SET status='offline' WHERE status='online' AND last_heartbeat_at IS NOT NULL AND last_heartbeat_at < (NOW() - INTERVAL ? SECOND)", timeout)

	list := []map[string]interface{}{}
	for rows.Next() {
		var id, gid, approved int
		var aid, host, ip, ver, os, arch, tags, st, appBy, appAt, lhb, lip, ct, gname string
		rows.Scan(&id, &aid, &host, &ip, &ver, &os, &arch, &tags, &gid, &st, &approved, &appBy, &appAt, &lhb, &lip, &ct, &gname)
		list = append(list, map[string]interface{}{
			"id":                id,
			"agent_id":          aid,
			"hostname":          host,
			"ip":                ip,
			"version":           ver,
			"os":                os,
			"arch":              arch,
			"tags":              tags,
			"group_id":          gid,
			"group_name":        gname,
			"status":            st,
			"approved":          approved == 1,
			"approved_by":       appBy,
			"approved_at":       appAt,
			"last_heartbeat_at": lhb,
			"last_seen_ip":      lip,
			"created_at":        ct,
		})
	}
	jsonSuccess(w, list)
}

// HandleApproveAgent marks a pending agent as approved.
func HandleApproveAgent(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
	username := r.Context().Value(contextUsername).(string)
	res, err := database.DB.Exec(
		"UPDATE agents SET approved=1, approved_by=?, approved_at=NOW(), status=CASE WHEN last_heartbeat_at IS NOT NULL AND last_heartbeat_at > (NOW() - INTERVAL ? SECOND) THEN 'online' ELSE 'offline' END WHERE id=? AND approved=0",
		username, cfg.AgentHeartbeatTimeout, id,
	)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "审批失败")
		return
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		jsonError(w, http.StatusBadRequest, "Agent 已审批或不存在")
		return
	}
	SaveAuditLog(r, "approve_agent", "agent", id, "审批 Agent")
	jsonSuccess(w, map[string]interface{}{"ok": true})
}

// HandleOfflineAgent marks an agent as offline (admin manual offline before delete).
func HandleOfflineAgent(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
	res, err := database.DB.Exec("UPDATE agents SET status='disabled' WHERE id=?", id)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "下线失败")
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		jsonError(w, http.StatusNotFound, "Agent 不存在")
		return
	}
	SaveAuditLog(r, "offline_agent", "agent", id, "管理员下线 Agent")
	jsonSuccess(w, map[string]interface{}{"ok": true})
}

// HandleDeleteAgent removes an agent. Online agents cannot be deleted (must offline first).
func HandleDeleteAgent(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
	var status string
	var lastHb *time.Time
	err := database.DB.QueryRow("SELECT status, last_heartbeat_at FROM agents WHERE id=?", id).Scan(&status, &lastHb)
	if err != nil {
		jsonError(w, http.StatusNotFound, "Agent 不存在")
		return
	}
	// Auto-refresh: still online if heartbeat recent
	if status == "online" || (lastHb != nil && time.Since(*lastHb) < time.Duration(cfg.AgentHeartbeatTimeout)*time.Second) {
		jsonError(w, http.StatusBadRequest, "在线 Agent 不允许删除，请先下线")
		return
	}
	database.DB.Exec("DELETE FROM agents WHERE id=?", id)
	database.DB.Exec("DELETE FROM probe_target_agents WHERE agent_id=(SELECT agent_id FROM agents WHERE id=?)", id)
	SaveAuditLog(r, "delete_agent", "agent", id, "删除 Agent")
	jsonSuccess(w, map[string]interface{}{"ok": true})
}

// HandleReissueAgentToken signs a new token for an existing agent and invalidates the old one.
func HandleReissueAgentToken(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
	var agentID string
	err := database.DB.QueryRow("SELECT agent_id FROM agents WHERE id=?", id).Scan(&agentID)
	if err != nil {
		jsonError(w, http.StatusNotFound, "Agent 不存在")
		return
	}
	token := newRandomToken()
	hash := sha256.Sum256([]byte(token))
	tokenHash := hex.EncodeToString(hash[:])
	var expiresAt interface{} = nil
	if cfg.AgentTokenExpireDays > 0 {
		expiresAt = time.Now().AddDate(0, 0, cfg.AgentTokenExpireDays)
	}
	_, err = database.DB.Exec(
		"UPDATE agents SET token_hash=?, token_issued_at=NOW(), token_expires_at=? WHERE id=?",
		tokenHash, expiresAt, id,
	)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "重签发失败")
		return
	}
	SaveAuditLog(r, "reissue_agent_token", "agent", agentID, "重新签发 Agent Token")
	// Token 仅在此接口响应中返回一次
	jsonSuccess(w, map[string]interface{}{
		"agent_id":   agentID,
		"token":      token,
		"expires_at": expiresAt,
		"message":    "请立即把新 token 写入 Agent 配置文件, 旧 token 已失效",
	})
}

// HandleUpdateAgent updates editable fields (group_id, tags).
func HandleUpdateAgent(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
	var req struct {
		GroupID *int    `json:"group_id"`
		Tags    *string `json:"tags"`
	}
	json.NewDecoder(r.Body).Decode(&req)
	if req.GroupID != nil {
		database.DB.Exec("UPDATE agents SET group_id=? WHERE id=?", *req.GroupID, id)
	}
	if req.Tags != nil {
		database.DB.Exec("UPDATE agents SET tags=? WHERE id=?", *req.Tags, id)
	}
	SaveAuditLog(r, "update_agent", "agent", id, "更新 Agent 属性")
	jsonSuccess(w, map[string]interface{}{"ok": true})
}

// ====== Agent groups ======

func HandleListGroups(w http.ResponseWriter, r *http.Request) {
	rows, err := database.DB.Query(`SELECT g.id, g.name, g.description, g.created_at, COUNT(a.id) as agent_count
	  FROM agent_groups g LEFT JOIN agents a ON a.group_id = g.id GROUP BY g.id ORDER BY g.id`)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "查询失败")
		return
	}
	defer rows.Close()
	list := []map[string]interface{}{}
	for rows.Next() {
		var id, count int
		var name, desc, ct string
		rows.Scan(&id, &name, &desc, &ct, &count)
		list = append(list, map[string]interface{}{
			"id": id, "name": name, "description": desc, "created_at": ct, "agent_count": count,
		})
	}
	jsonSuccess(w, list)
}

func HandleCreateGroup(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name        string `json:"name"`
		Description string `json:"description"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Name == "" {
		jsonError(w, http.StatusBadRequest, "name required")
		return
	}
	res, err := database.DB.Exec("INSERT INTO agent_groups (name, description) VALUES (?, ?)", req.Name, req.Description)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "创建失败: "+err.Error())
		return
	}
	id, _ := res.LastInsertId()
	SaveAuditLog(r, "create_group", "group", req.Name, "")
	jsonSuccess(w, map[string]interface{}{"id": id})
}

func HandleUpdateGroup(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
	var req struct {
		Name        string `json:"name"`
		Description string `json:"description"`
	}
	json.NewDecoder(r.Body).Decode(&req)
	database.DB.Exec("UPDATE agent_groups SET name=?, description=? WHERE id=?", req.Name, req.Description, id)
	SaveAuditLog(r, "update_group", "group", id, "")
	jsonSuccess(w, map[string]interface{}{"ok": true})
}

func HandleDeleteGroup(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
	gid, _ := strconv.Atoi(id)
	// Detach agents from this group
	database.DB.Exec("UPDATE agents SET group_id=0 WHERE group_id=?", gid)
	database.DB.Exec("DELETE FROM agent_groups WHERE id=?", gid)
	SaveAuditLog(r, "delete_group", "group", id, "")
	jsonSuccess(w, map[string]interface{}{"ok": true})
}
