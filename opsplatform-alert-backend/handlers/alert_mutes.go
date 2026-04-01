package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/gorilla/mux"
	"opsplatform-alert-backend/database"
)

// HandleListMutes returns active mutes for a rule
func HandleListMutes(w http.ResponseWriter, r *http.Request) {
	ruleID := r.URL.Query().Get("rule_id")

	query := `SELECT m.id, m.rule_id, COALESCE(r.name,'(已删除)'), m.group_key, m.mute_until, m.reason, m.created_by, m.created_at
		FROM alert_mutes m LEFT JOIN alert_rules r ON m.rule_id = r.id WHERE m.mute_until > NOW()`
	args := []interface{}{}

	if ruleID != "" {
		query += " AND m.rule_id = ?"
		rid, _ := strconv.Atoi(ruleID)
		args = append(args, rid)
	}

	projectID := r.URL.Query().Get("project_id")
	if projectID != "" {
		pid, _ := strconv.Atoi(projectID)
		if pid > 0 {
			query += " AND r.project_id IN (SELECT id FROM alert_projects WHERE id = ? OR parent_id = ?)"
			args = append(args, pid, pid)
		}
	}

	query += " ORDER BY m.created_at DESC"

	rows, err := database.DB.Query(query, args...)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "查询失败")
		return
	}
	defer rows.Close()

	var list []map[string]interface{}
	for rows.Next() {
		var id, rid int
		var ruleName, groupKey, reason, createdBy, muteUntil, createdAt string
		rows.Scan(&id, &rid, &ruleName, &groupKey, &muteUntil, &reason, &createdBy, &createdAt)
		list = append(list, map[string]interface{}{
			"id":         id,
			"rule_id":    rid,
			"rule_name":  ruleName,
			"group_key":  groupKey,
			"mute_until": muteUntil,
			"reason":     reason,
			"created_by": createdBy,
			"created_at": createdAt,
		})
	}
	if list == nil {
		list = []map[string]interface{}{}
	}
	jsonSuccess(w, list)
}

// HandleCreateMute creates a mute for a container
func HandleCreateMute(w http.ResponseWriter, r *http.Request) {
	var req struct {
		RuleID   int    `json:"rule_id"`
		GroupKey string `json:"group_key"`
		Duration string `json:"duration"` // 1h/3h/6h/12h/24h/7d/30d or custom like "2h30m"
		Reason   string `json:"reason"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, http.StatusBadRequest, "无效的请求")
		return
	}
	if req.RuleID == 0 || req.GroupKey == "" || req.Duration == "" {
		jsonError(w, http.StatusBadRequest, "规则ID、容器名和屏蔽时长不能为空")
		return
	}

	// Parse duration
	var muteUntil time.Time
	if req.Duration == "forever" {
		muteUntil = time.Now().AddDate(1, 0, 0) // 1 year
	} else {
		d, err := parseMuteDuration(req.Duration)
		if err != nil {
			jsonError(w, http.StatusBadRequest, "无效的时长格式")
			return
		}
		muteUntil = time.Now().Add(d)
	}

	// Get current user
	username := ""
	if u := r.Context().Value(contextUsername); u != nil {
		username = u.(string)
	}

	// Upsert: delete old mute for same rule+group, insert new
	database.DB.Exec("DELETE FROM alert_mutes WHERE rule_id = ? AND group_key = ?", req.RuleID, req.GroupKey)

	result, err := database.DB.Exec(`INSERT INTO alert_mutes (rule_id, group_key, mute_until, reason, created_by)
		VALUES (?, ?, ?, ?, ?)`, req.RuleID, req.GroupKey, muteUntil, req.Reason, username)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "创建失败: "+err.Error())
		return
	}

	id, _ := result.LastInsertId()
	SaveAuditLog(r, "create_mute", "mute", req.GroupKey, fmt.Sprintf("屏蔽容器 %s 至 %s", req.GroupKey, muteUntil.Format("2006-01-02 15:04:05")))
	jsonSuccess(w, map[string]interface{}{
		"id":         id,
		"mute_until": muteUntil.Format("2006-01-02 15:04:05"),
		"message":    "屏蔽已设置",
	})
}

// HandleDeleteMute removes a mute
func HandleDeleteMute(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.Atoi(mux.Vars(r)["id"])
	database.DB.Exec("DELETE FROM alert_mutes WHERE id = ?", id)
	SaveAuditLog(r, "delete_mute", "mute", fmt.Sprintf("ID=%d", id), "取消屏蔽")
	jsonSuccess(w, nil)
}

// IsMuted checks if a group is currently muted for a rule
func IsMuted(ruleID int, groupKey string) bool {
	var count int
	database.DB.QueryRow("SELECT COUNT(*) FROM alert_mutes WHERE rule_id = ? AND group_key = ? AND mute_until > NOW()",
		ruleID, groupKey).Scan(&count)
	return count > 0
}

func parseMuteDuration(s string) (time.Duration, error) {
	// Support day/week/month shortcuts
	switch s {
	case "1d":
		return 24 * time.Hour, nil
	case "3d":
		return 3 * 24 * time.Hour, nil
	case "7d", "1w":
		return 7 * 24 * time.Hour, nil
	case "14d", "2w":
		return 14 * 24 * time.Hour, nil
	case "30d", "1M":
		return 30 * 24 * time.Hour, nil
	}
	return time.ParseDuration(s)
}
