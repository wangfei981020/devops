package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"

	"opsplatform-jira-confluence-lock-backend/database"

	"github.com/gorilla/mux"
)

type TriggerRule struct {
	ID             int64  `json:"id"`
	RuleName       string `json:"rule_name"`
	Action         string `json:"action"`          // lock 或 unlock
	ProjectKey     string `json:"project_key"`     // Jira 项目 key，空=匹配所有
	TransitionName string `json:"transition_name"` // 工作流状态名
	Enabled        bool   `json:"enabled"`
	CreatedAt      string `json:"created_at"`
	UpdatedAt      string `json:"updated_at"`
}

// HandleGetTriggers 获取触发规则列表
func HandleGetTriggers(w http.ResponseWriter, r *http.Request) {
	rows, err := database.DB.Query(`SELECT id, rule_name, action, project_key, transition_name, enabled, created_at, updated_at FROM trigger_rules ORDER BY id DESC`)
	if err != nil {
		respondInternalError(w, "查询失败")
		return
	}
	defer rows.Close()

	var rules []TriggerRule
	for rows.Next() {
		var rule TriggerRule
		if err := rows.Scan(&rule.ID, &rule.RuleName, &rule.Action, &rule.ProjectKey, &rule.TransitionName, &rule.Enabled, &rule.CreatedAt, &rule.UpdatedAt); err == nil {
			rules = append(rules, rule)
		}
	}
	if rules == nil {
		rules = []TriggerRule{}
	}

	respondSuccess(w, rules)
}

// HandleCreateTrigger 新增触发规则
func HandleCreateTrigger(w http.ResponseWriter, r *http.Request) {
	var rule TriggerRule
	if err := json.NewDecoder(r.Body).Decode(&rule); err != nil {
		respondBadRequest(w, "无效的请求数据")
		return
	}

	if rule.Action != "lock" && rule.Action != "unlock" {
		respondBadRequest(w, "action 必须是 lock 或 unlock")
		return
	}
	if rule.TransitionName == "" {
		respondBadRequest(w, "transition_name 不能为空")
		return
	}

	result, err := database.DB.Exec(`
		INSERT INTO trigger_rules (rule_name, action, project_key, transition_name, enabled)
		VALUES (?, ?, ?, ?, ?)
	`, rule.RuleName, rule.Action, rule.ProjectKey, rule.TransitionName, rule.Enabled)
	if err != nil {
		respondInternalError(w, "创建失败: "+err.Error())
		return
	}

	id, _ := result.LastInsertId()
	rule.ID = id
	respondSuccess(w, rule)
}

// HandleUpdateTrigger 修改触发规则
func HandleUpdateTrigger(w http.ResponseWriter, r *http.Request) {
	idStr := mux.Vars(r)["id"]
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		respondBadRequest(w, "无效的 ID")
		return
	}

	var rule TriggerRule
	if err := json.NewDecoder(r.Body).Decode(&rule); err != nil {
		respondBadRequest(w, "无效的请求数据")
		return
	}

	if rule.Action != "lock" && rule.Action != "unlock" {
		respondBadRequest(w, "action 必须是 lock 或 unlock")
		return
	}

	_, err = database.DB.Exec(`
		UPDATE trigger_rules SET rule_name=?, action=?, project_key=?, transition_name=?, enabled=?, updated_at=NOW()
		WHERE id=?
	`, rule.RuleName, rule.Action, rule.ProjectKey, rule.TransitionName, rule.Enabled, id)
	if err != nil {
		respondInternalError(w, "更新失败: "+err.Error())
		return
	}

	respondSuccess(w, map[string]string{"message": "更新成功"})
}

// HandleDeleteTrigger 删除触发规则
func HandleDeleteTrigger(w http.ResponseWriter, r *http.Request) {
	idStr := mux.Vars(r)["id"]
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		respondBadRequest(w, "无效的 ID")
		return
	}

	result, err := database.DB.Exec(`DELETE FROM trigger_rules WHERE id=?`, id)
	if err != nil {
		respondInternalError(w, "删除失败: "+err.Error())
		return
	}

	affected, _ := result.RowsAffected()
	if affected == 0 {
		respondBadRequest(w, fmt.Sprintf("规则 ID=%d 不存在", id))
		return
	}

	respondSuccess(w, map[string]string{"message": "删除成功"})
}

// MatchTriggerRule 匹配触发规则，返回 action（lock/unlock）和规则名
func MatchTriggerRule(projectKey, transitionName string) (string, string, bool) {
	rows, err := database.DB.Query(`
		SELECT action, rule_name, project_key, transition_name FROM trigger_rules WHERE enabled = 1
	`)
	if err != nil {
		return "", "", false
	}
	defer rows.Close()

	for rows.Next() {
		var action, ruleName, ruleProjectKey, ruleTransition string
		if err := rows.Scan(&action, &ruleName, &ruleProjectKey, &ruleTransition); err != nil {
			continue
		}
		// project_key 为空则匹配所有项目
		if ruleProjectKey != "" && ruleProjectKey != projectKey {
			continue
		}
		if ruleTransition == transitionName {
			return action, ruleName, true
		}
	}
	return "", "", false
}
