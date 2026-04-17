package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"

	"opsplatform-probe-backend/database"
)

// HandleDispatchUpgrade pushes an upgrade task for one or more agents.
type DispatchUpgradeReq struct {
	AgentIDs       []string `json:"agent_ids"`
	ToVersion      string   `json:"to_version"`
	AllowDowngrade bool     `json:"allow_downgrade"`
}

func HandleDispatchUpgrade(w http.ResponseWriter, r *http.Request) {
	var req DispatchUpgradeReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, http.StatusBadRequest, "invalid")
		return
	}
	if req.ToVersion == "" || len(req.AgentIDs) == 0 {
		jsonError(w, http.StatusBadRequest, "to_version and agent_ids required")
		return
	}
	var versionExists int
	database.DB.QueryRow("SELECT COUNT(*) FROM agent_versions WHERE version=?", req.ToVersion).Scan(&versionExists)
	if versionExists == 0 {
		jsonError(w, http.StatusNotFound, "目标版本不存在")
		return
	}

	username := ""
	if v := r.Context().Value(contextUsername); v != nil {
		username = v.(string)
	}
	created := 0
	skipped := []string{}
	for _, aid := range req.AgentIDs {
		var curVersion, status string
		var approved int
		err := database.DB.QueryRow("SELECT version, status, approved FROM agents WHERE agent_id=?", aid).Scan(&curVersion, &status, &approved)
		if err != nil {
			skipped = append(skipped, aid+":not_found")
			continue
		}
		if approved == 0 {
			skipped = append(skipped, aid+":not_approved")
			continue
		}
		if curVersion == req.ToVersion {
			skipped = append(skipped, aid+":same_version")
			continue
		}
		if !req.AllowDowngrade && curVersion != "" && versionLess(req.ToVersion, curVersion) {
			skipped = append(skipped, aid+":downgrade_blocked")
			continue
		}
		// Skip if there's already a pending/in-progress upgrade for this agent
		var pending int
		database.DB.QueryRow("SELECT COUNT(*) FROM agent_upgrade_tasks WHERE agent_id=? AND status IN ('pending','downloading','upgrading')", aid).Scan(&pending)
		if pending > 0 {
			skipped = append(skipped, aid+":already_in_progress")
			continue
		}
		_, err = database.DB.Exec(
			"INSERT INTO agent_upgrade_tasks (agent_id, from_version, to_version, status, created_by) VALUES (?, ?, ?, 'pending', ?)",
			aid, curVersion, req.ToVersion, username,
		)
		if err == nil {
			created++
		}
	}
	SaveAuditLog(r, "dispatch_upgrade", "upgrade", req.ToVersion, fmt.Sprintf("created=%d skipped=%d", created, len(skipped)))
	jsonSuccess(w, map[string]interface{}{
		"created": created,
		"skipped": skipped,
	})
}

// HandleListUpgradeTasks returns recent upgrade tasks.
func HandleListUpgradeTasks(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	agentID := r.URL.Query().Get("agent_id")
	q := `SELECT id, agent_id, from_version, to_version, status, error, created_by, created_at, fetched_at, finished_at
	      FROM agent_upgrade_tasks WHERE 1=1`
	args := []interface{}{}
	if agentID != "" {
		q += " AND agent_id=?"
		args = append(args, agentID)
	}
	q += " ORDER BY id DESC LIMIT ?"
	args = append(args, limit)
	rows, err := database.DB.Query(q, args...)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "查询失败")
		return
	}
	defer rows.Close()
	list := []map[string]interface{}{}
	for rows.Next() {
		var id int64
		var aid, from, to, status, errMsg, by, ct string
		var fetched, finished *string
		rows.Scan(&id, &aid, &from, &to, &status, &errMsg, &by, &ct, &fetched, &finished)
		list = append(list, map[string]interface{}{
			"id":           id,
			"agent_id":     aid,
			"from_version": from,
			"to_version":   to,
			"status":       status,
			"error":        errMsg,
			"created_by":   by,
			"created_at":   ct,
			"fetched_at":   fetched,
			"finished_at":  finished,
		})
	}
	jsonSuccess(w, list)
}

// versionLess returns true if a < b (semantic-ish).
func versionLess(a, b string) bool {
	pa := parseVersion(a)
	pb := parseVersion(b)
	for i := 0; i < 3; i++ {
		if pa[i] != pb[i] {
			return pa[i] < pb[i]
		}
	}
	return false
}

func parseVersion(v string) [3]int {
	var out [3]int
	if len(v) > 0 && v[0] == 'v' {
		v = v[1:]
	}
	parts := splitDot(v)
	for i := 0; i < 3 && i < len(parts); i++ {
		n := 0
		for _, c := range parts[i] {
			if c < '0' || c > '9' {
				break
			}
			n = n*10 + int(c-'0')
		}
		out[i] = n
	}
	return out
}

func splitDot(s string) []string {
	out := []string{}
	cur := ""
	for _, c := range s {
		if c == '.' {
			out = append(out, cur)
			cur = ""
		} else {
			cur += string(c)
		}
	}
	if cur != "" {
		out = append(out, cur)
	}
	return out
}
