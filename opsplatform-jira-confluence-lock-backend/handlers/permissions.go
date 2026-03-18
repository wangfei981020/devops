package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"

	"opsplatform-jira-confluence-lock-backend/database"

	"github.com/gorilla/mux"
)

type PermissionRule struct {
	ID          int64  `json:"id"`
	RuleType    string `json:"rule_type"` // user 或 group
	RuleValue   string `json:"rule_value"`
	Description string `json:"description"`
	Enabled     bool   `json:"enabled"`
	CreatedAt   string `json:"created_at"`
	UpdatedAt   string `json:"updated_at"`
}

// HandleGetPermissions 获取权限规则列表
func HandleGetPermissions(w http.ResponseWriter, r *http.Request) {
	rows, err := database.DB.Query(`SELECT id, rule_type, rule_value, description, enabled, created_at, updated_at FROM permission_rules ORDER BY rule_type, id`)
	if err != nil {
		respondInternalError(w, "查询失败")
		return
	}
	defer rows.Close()

	var rules []PermissionRule
	for rows.Next() {
		var rule PermissionRule
		if err := rows.Scan(&rule.ID, &rule.RuleType, &rule.RuleValue, &rule.Description, &rule.Enabled, &rule.CreatedAt, &rule.UpdatedAt); err == nil {
			rules = append(rules, rule)
		}
	}
	if rules == nil {
		rules = []PermissionRule{}
	}

	respondSuccess(w, rules)
}

// HandleCreatePermission 新增权限规则
func HandleCreatePermission(w http.ResponseWriter, r *http.Request) {
	var rule PermissionRule
	if err := json.NewDecoder(r.Body).Decode(&rule); err != nil {
		respondBadRequest(w, "无效的请求数据")
		return
	}

	if rule.RuleType != "user" && rule.RuleType != "group" {
		respondBadRequest(w, "rule_type 必须是 user 或 group")
		return
	}
	if rule.RuleValue == "" {
		respondBadRequest(w, "rule_value 不能为空")
		return
	}

	result, err := database.DB.Exec(`
		INSERT INTO permission_rules (rule_type, rule_value, description, enabled)
		VALUES (?, ?, ?, ?)
	`, rule.RuleType, rule.RuleValue, rule.Description, rule.Enabled)
	if err != nil {
		respondInternalError(w, "创建失败: "+err.Error())
		return
	}

	id, _ := result.LastInsertId()
	rule.ID = id
	respondSuccess(w, rule)
}

// HandleUpdatePermission 修改权限规则
func HandleUpdatePermission(w http.ResponseWriter, r *http.Request) {
	idStr := mux.Vars(r)["id"]
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		respondBadRequest(w, "无效的 ID")
		return
	}

	var rule PermissionRule
	if err := json.NewDecoder(r.Body).Decode(&rule); err != nil {
		respondBadRequest(w, "无效的请求数据")
		return
	}

	_, err = database.DB.Exec(`
		UPDATE permission_rules SET rule_type=?, rule_value=?, description=?, enabled=?, updated_at=NOW()
		WHERE id=?
	`, rule.RuleType, rule.RuleValue, rule.Description, rule.Enabled, id)
	if err != nil {
		respondInternalError(w, "更新失败: "+err.Error())
		return
	}

	respondSuccess(w, map[string]string{"message": "更新成功"})
}

// HandleDeletePermission 删除权限规则
func HandleDeletePermission(w http.ResponseWriter, r *http.Request) {
	idStr := mux.Vars(r)["id"]
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		respondBadRequest(w, "无效的 ID")
		return
	}

	result, err := database.DB.Exec(`DELETE FROM permission_rules WHERE id=?`, id)
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

// GetActivePermissions 获取所有启用的权限规则，返回用户列表和组列表
func GetActivePermissions() ([]string, []string) {
	rows, err := database.DB.Query(`SELECT rule_type, rule_value FROM permission_rules WHERE enabled = 1`)
	if err != nil {
		return nil, nil
	}
	defer rows.Close()

	var users, groups []string
	for rows.Next() {
		var ruleType, ruleValue string
		if err := rows.Scan(&ruleType, &ruleValue); err == nil {
			if ruleType == "user" {
				users = append(users, ruleValue)
			} else if ruleType == "group" {
				groups = append(groups, ruleValue)
			}
		}
	}
	return users, groups
}
