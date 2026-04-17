package handlers

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"opsplatform-probe-backend/database"
)

// ProbeReq is a manual probe request: many agents x many targets.
type ProbeReq struct {
	AgentIDs  []string `json:"agent_ids"`
	TargetIDs []int    `json:"target_ids"`
}

// HandleManualProbe creates a batch with N*M tasks. Returns batch_id; client polls results.
func HandleManualProbe(w http.ResponseWriter, r *http.Request) {
	var req ProbeReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, http.StatusBadRequest, "invalid")
		return
	}
	if len(req.AgentIDs) == 0 || len(req.TargetIDs) == 0 {
		jsonError(w, http.StatusBadRequest, "agent_ids and target_ids required")
		return
	}

	username := ""
	if v := r.Context().Value(contextUsername); v != nil {
		username = v.(string)
	}
	batchID := newBatchID()
	total := 0

	// Pre-load target info
	type tinfo struct {
		ID         int
		Type       string
		Target     string
		Port       int
		Method     string
		TimeoutSec int
		Scope      string
		ScopeGroup int
	}
	targets := map[int]tinfo{}
	for _, tid := range req.TargetIDs {
		var t tinfo
		err := database.DB.QueryRow(
			"SELECT id, type, target, port, method, timeout_sec, agent_scope, scope_group_id FROM probe_targets WHERE id=? AND enabled=1",
			tid,
		).Scan(&t.ID, &t.Type, &t.Target, &t.Port, &t.Method, &t.TimeoutSec, &t.Scope, &t.ScopeGroup)
		if err != nil {
			continue
		}
		targets[tid] = t
	}

	// Pre-load agent info: agent_id -> group_id
	agentGroup := map[string]int{}
	for _, aid := range req.AgentIDs {
		var gid, approved int
		var status string
		err := database.DB.QueryRow("SELECT group_id, approved, status FROM agents WHERE agent_id=?", aid).Scan(&gid, &approved, &status)
		if err != nil || approved == 0 || status == "disabled" {
			continue
		}
		agentGroup[aid] = gid
	}

	// Pre-load specific bindings
	specificBind := map[int]map[string]bool{} // target_id -> set of agent_id
	for _, t := range targets {
		if t.Scope == "specific" {
			specificBind[t.ID] = map[string]bool{}
			rows, _ := database.DB.Query("SELECT agent_id FROM probe_target_agents WHERE target_id=?", t.ID)
			for rows.Next() {
				var aid string
				rows.Scan(&aid)
				specificBind[t.ID][aid] = true
			}
			rows.Close()
		}
	}

	for aid, gid := range agentGroup {
		for tid, t := range targets {
			eligible := false
			switch t.Scope {
			case "all":
				eligible = true
			case "group":
				eligible = gid == t.ScopeGroup
			case "specific":
				eligible = specificBind[tid][aid]
			}
			if !eligible {
				continue
			}
			_, err := database.DB.Exec(
				`INSERT INTO probe_tasks (batch_id, agent_id, target_id, target_type, target_addr, target_port, method, timeout_sec, status)
				 VALUES (?, ?, ?, ?, ?, ?, ?, ?, 'pending')`,
				batchID, aid, tid, t.Type, t.Target, t.Port, t.Method, t.TimeoutSec,
			)
			if err == nil {
				total++
			}
		}
	}

	database.DB.Exec("INSERT INTO probe_batches (batch_id, created_by, source, total_tasks) VALUES (?, ?, 'manual', ?)",
		batchID, username, total)

	SaveAuditLog(r, "manual_probe", "batch", batchID, "")
	jsonSuccess(w, map[string]interface{}{
		"batch_id": batchID,
		"total":    total,
	})
}

// HandleGetBatchResult returns the matrix view of a batch.
func HandleGetBatchResult(w http.ResponseWriter, r *http.Request) {
	batchID := r.URL.Query().Get("batch_id")
	if batchID == "" {
		jsonError(w, http.StatusBadRequest, "batch_id required")
		return
	}
	var total, done int
	database.DB.QueryRow("SELECT total_tasks, done_tasks FROM probe_batches WHERE batch_id=?", batchID).Scan(&total, &done)

	rows, err := database.DB.Query(
		`SELECT agent_id, target_id, target_name, target_type, target_addr, target_port,
		        success, latency_ms, status_code, resolved_ip, error, probed_at
		   FROM probe_results WHERE batch_id=?`,
		batchID,
	)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "查询失败")
		return
	}
	defer rows.Close()
	results := []map[string]interface{}{}
	for rows.Next() {
		var success, port, status, latency, tid int
		var aid, tname, ttype, addr, ip, errMsg, ts string
		rows.Scan(&aid, &tid, &tname, &ttype, &addr, &port, &success, &latency, &status, &ip, &errMsg, &ts)
		results = append(results, map[string]interface{}{
			"agent_id":    aid,
			"target_id":   tid,
			"target_name": tname,
			"target_type": ttype,
			"target_addr": addr,
			"target_port": port,
			"success":     success == 1,
			"latency_ms":  latency,
			"status_code": status,
			"resolved_ip": ip,
			"error":       errMsg,
			"probed_at":   ts,
		})
	}
	jsonSuccess(w, map[string]interface{}{
		"batch_id": batchID,
		"total":    total,
		"done":     done,
		"finished": done >= total,
		"results":  results,
	})
}

// HandleListResults returns paginated probe results with filters.
// Supports filters: agent_id, target_id, success, start_time, end_time, source (manual/scheduled),
// created_by (executor username). Joins probe_batches to expose who triggered each batch.
func HandleListResults(w http.ResponseWriter, r *http.Request) {
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	if page < 1 {
		page = 1
	}
	pageSize, _ := strconv.Atoi(r.URL.Query().Get("page_size"))
	if pageSize < 1 || pageSize > 200 {
		pageSize = 50
	}
	agentID := r.URL.Query().Get("agent_id")
	targetID := r.URL.Query().Get("target_id")
	successFilter := r.URL.Query().Get("success")
	startTime := r.URL.Query().Get("start_time")
	endTime := r.URL.Query().Get("end_time")
	createdBy := r.URL.Query().Get("created_by")
	source := r.URL.Query().Get("source")

	where := "WHERE 1=1"
	args := []interface{}{}
	if agentID != "" {
		where += " AND r.agent_id=?"
		args = append(args, agentID)
	}
	if targetID != "" {
		where += " AND r.target_id=?"
		args = append(args, targetID)
	}
	if successFilter == "1" || successFilter == "0" {
		where += " AND r.success=?"
		args = append(args, successFilter)
	}
	if startTime != "" {
		where += " AND r.probed_at >= ?"
		args = append(args, startTime)
	}
	if endTime != "" {
		where += " AND r.probed_at <= ?"
		args = append(args, endTime)
	}
	if createdBy != "" {
		where += " AND b.created_by LIKE ?"
		args = append(args, "%"+createdBy+"%")
	}
	if source != "" {
		where += " AND b.source = ?"
		args = append(args, source)
	}

	baseFrom := "FROM probe_results r LEFT JOIN probe_batches b ON b.batch_id = r.batch_id"

	var total int
	database.DB.QueryRow("SELECT COUNT(*) "+baseFrom+" "+where, args...).Scan(&total)

	queryArgs := append(args, pageSize, (page-1)*pageSize)
	rows, err := database.DB.Query(
		`SELECT r.id, r.batch_id, r.agent_id, r.target_id, r.target_name, r.target_type, r.target_addr, r.target_port,
		   r.success, r.latency_ms, r.status_code, r.resolved_ip, r.error, r.probed_at,
		   COALESCE(b.created_by, ''), COALESCE(b.source, ''), COALESCE(b.created_at, r.probed_at)
		 `+baseFrom+" "+where+" ORDER BY r.id DESC LIMIT ? OFFSET ?", queryArgs...)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "查询失败")
		return
	}
	defer rows.Close()
	list := []map[string]interface{}{}
	for rows.Next() {
		var id int64
		var port, success, status, latency, tid int
		var batchID, aid, tname, ttype, addr, ip, errMsg, ts, createdByVal, srcVal, batchCreatedAt string
		rows.Scan(&id, &batchID, &aid, &tid, &tname, &ttype, &addr, &port,
			&success, &latency, &status, &ip, &errMsg, &ts,
			&createdByVal, &srcVal, &batchCreatedAt)
		list = append(list, map[string]interface{}{
			"id":               id,
			"batch_id":         batchID,
			"agent_id":         aid,
			"target_id":        tid,
			"target_name":      tname,
			"target_type":      ttype,
			"target_addr":      addr,
			"target_port":      port,
			"success":          success == 1,
			"latency_ms":       latency,
			"status_code":      status,
			"resolved_ip":      ip,
			"error":            errMsg,
			"probed_at":        ts,
			"created_by":       createdByVal,
			"source":           srcVal,
			"batch_created_at": batchCreatedAt,
		})
	}
	jsonSuccess(w, map[string]interface{}{"list": list, "total": total, "page": page})
}

// HandleCleanResults deletes results older than X days.
func HandleCleanResults(w http.ResponseWriter, r *http.Request) {
	var req struct {
		BeforeDays int `json:"before_days"`
	}
	json.NewDecoder(r.Body).Decode(&req)
	if req.BeforeDays <= 0 {
		req.BeforeDays = 30
	}
	res, err := database.DB.Exec("DELETE FROM probe_results WHERE probed_at < (NOW() - INTERVAL ? DAY)", req.BeforeDays)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "清理失败")
		return
	}
	n, _ := res.RowsAffected()
	SaveAuditLog(r, "clean_results", "result", "", "deleted "+strconv.FormatInt(n, 10))
	jsonSuccess(w, map[string]interface{}{"deleted": n})
}

// Mark stale tasks as expired periodically. Called from main.
func StartTaskExpirer() {
	go func() {
		for {
			time.Sleep(1 * time.Minute)
			if database.DB == nil {
				continue
			}
			database.DB.Exec("UPDATE probe_tasks SET status='expired' WHERE status IN ('pending','running') AND created_at < (NOW() - INTERVAL 10 MINUTE)")
		}
	}()
}

func newBatchID() string {
	b := make([]byte, 12)
	rand.Read(b)
	return hex.EncodeToString(b)
}
