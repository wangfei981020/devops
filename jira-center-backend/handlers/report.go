package handlers

import (
	"encoding/json"
	"log"
	"net/http"
	"net/url"
	"strings"
	"sync"

	"jira-center-backend/database"
	"jira-center-backend/jira"
)

// ReportConfig 报告配置
type ReportConfig struct {
	Sections      []ReportSection       `json:"sections"`
	FieldMappings map[string][]FieldMap `json:"field_mappings"`
}

// ReportSection 报告章节配置
type ReportSection struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	Enabled   bool   `json:"enabled"`
	Project   string `json:"project"`   // 章节专属项目Key（留空则用报告主项目）
	IssueType string `json:"issuetype"` // 工单类型名称，如 "故障"、"升级"
	Filter    string `json:"filter"`    // 额外 JQL 条件
}

// FieldMap 字段映射
type FieldMap struct {
	Column  string `json:"column"`   // 报告表格列名
	FieldID string `json:"field_id"` // Jira 字段ID
}

// HandleGetReportConfig 获取报告配置
func HandleGetReportConfig(w http.ResponseWriter, r *http.Request) {
	var configJSON string
	err := database.DB.QueryRow(`SELECT setting_value FROM system_settings WHERE setting_key = 'report_config'`).Scan(&configJSON)
	if err != nil || configJSON == "" {
		// 返回默认配置
		respondSuccess(w, defaultReportConfig())
		return
	}

	var config ReportConfig
	if err := json.Unmarshal([]byte(configJSON), &config); err != nil {
		respondSuccess(w, defaultReportConfig())
		return
	}
	respondSuccess(w, config)
}

// HandleSaveReportConfig 保存报告配置
func HandleSaveReportConfig(w http.ResponseWriter, r *http.Request) {
	var config ReportConfig
	if err := json.NewDecoder(r.Body).Decode(&config); err != nil {
		respondBadRequest(w, "无效的请求数据")
		return
	}

	data, _ := json.Marshal(config)
	database.DB.Exec(`
		INSERT INTO system_settings (setting_key, setting_value, updated_at)
		VALUES ('report_config', ?, NOW())
		ON DUPLICATE KEY UPDATE setting_value = VALUES(setting_value), updated_at = NOW()
	`, string(data))

	_, username, _ := GetUserFromContext(r)
	AddAuditLog("", username, "更新报告配置", "settings", "report", "修改报告模板配置", GetClientIP(r))

	respondSuccess(w, map[string]string{"message": "配置已保存"})
}

// HandleGetReportData 生成报告数据（支持多项目聚合）
func HandleGetReportData(w http.ResponseWriter, r *http.Request) {
	client, err := jira.NewClient()
	if err != nil {
		respondBadRequest(w, err.Error())
		return
	}

	dateFrom := r.URL.Query().Get("date_from")
	dateTo := r.URL.Query().Get("date_to")

	if dateFrom == "" || dateTo == "" {
		respondBadRequest(w, "date_from, date_to 参数必填")
		return
	}

	// 获取报告配置
	var configJSON string
	database.DB.QueryRow(`SELECT setting_value FROM system_settings WHERE setting_key = 'report_config'`).Scan(&configJSON)
	config := defaultReportConfig()
	if configJSON != "" {
		json.Unmarshal([]byte(configJSON), &config)
	}

	dateCondition := "created >= \"" + dateFrom + "\" AND created <= \"" + dateTo + " 23:59\""

	// 收集所有启用章节的项目（用于总体统计）
	projectSet := map[string]bool{}
	for _, sec := range config.Sections {
		if sec.Enabled && sec.Project != "" {
			projectSet[sec.Project] = true
		}
	}

	// 并行查询各章节数据
	type sectionData struct {
		ID     string      `json:"id"`
		Title  string      `json:"title"`
		Issues interface{} `json:"issues"`
		Total  int         `json:"total"`
	}

	var wg sync.WaitGroup
	sections := make([]sectionData, len(config.Sections))

	for i, sec := range config.Sections {
		if !sec.Enabled || sec.Project == "" {
			sections[i] = sectionData{ID: sec.ID, Title: sec.Title, Issues: []interface{}{}, Total: 0}
			continue
		}

		wg.Add(1)
		go func(idx int, section ReportSection) {
			defer wg.Done()

			conditions := []string{"project = " + section.Project}
			if section.IssueType != "" {
				conditions = append(conditions, "issuetype = \""+section.IssueType+"\"")
			}
			if section.Filter != "" {
				conditions = append(conditions, section.Filter)
			}
			conditions = append(conditions, dateCondition)
			jql := strings.Join(conditions, " AND ") + " ORDER BY created ASC"

			params := url.Values{}
			params.Set("jql", jql)
			params.Set("maxResults", "500")
			params.Set("fields", "*all")

			data, err := client.GetRaw("/rest/api/2/search?" + params.Encode())
			if err != nil {
				log.Printf("[Report] 查询 %s 失败: %v", section.ID, err)
				sections[idx] = sectionData{ID: section.ID, Title: section.Title, Issues: []interface{}{}, Total: 0}
				return
			}

			var result struct {
				Total  int               `json:"total"`
				Issues []json.RawMessage `json:"issues"`
			}
			json.Unmarshal(data, &result)
			sections[idx] = sectionData{ID: section.ID, Title: section.Title, Issues: result.Issues, Total: result.Total}
		}(i, sec)
	}

	// 总体统计：聚合所有章节项目
	var allIssuesData json.RawMessage
	var resolvedData json.RawMessage

	if len(projectSet) > 0 {
		projectKeys := make([]string, 0, len(projectSet))
		for k := range projectSet {
			projectKeys = append(projectKeys, k)
		}
		projectJQL := "project in (" + strings.Join(projectKeys, ", ") + ")"

		wg.Add(1)
		go func() {
			defer wg.Done()
			jql := projectJQL + " AND " + dateCondition + " ORDER BY created ASC"
			params := url.Values{}
			params.Set("jql", jql)
			params.Set("maxResults", "1000")
			params.Set("fields", "*all")
			data, err := client.GetRaw("/rest/api/2/search?" + params.Encode())
			if err != nil {
				log.Printf("[Report] 总体查询失败: %v", err)
				return
			}
			allIssuesData = data
		}()

		wg.Add(1)
		go func() {
			defer wg.Done()
			resolvedJQL := projectJQL + " AND resolved >= \"" + dateFrom + "\" AND resolved <= \"" + dateTo + " 23:59\" ORDER BY resolved ASC"
			params := url.Values{}
			params.Set("jql", resolvedJQL)
			params.Set("maxResults", "1000")
			params.Set("fields", "*all")
			data, err := client.GetRaw("/rest/api/2/search?" + params.Encode())
			if err != nil {
				log.Printf("[Report] 已解决查询失败: %v", err)
				return
			}
			resolvedData = data
		}()
	}

	wg.Wait()

	respondSuccess(w, map[string]interface{}{
		"date_from":  dateFrom,
		"date_to":    dateTo,
		"sections":   sections,
		"all_issues": allIssuesData,
		"resolved":   resolvedData,
		"config":     config,
	})
}

func defaultReportConfig() ReportConfig {
	return ReportConfig{
		Sections: []ReportSection{
			{ID: "faults", Title: "可用性与故障情况", Enabled: true, Project: "", IssueType: "", Filter: ""},
			{ID: "upgrades", Title: "升级上线与生产变更质量管控", Enabled: true, Project: "", IssueType: "", Filter: ""},
			{ID: "changes", Title: "变更管理", Enabled: true, Project: "", IssueType: "", Filter: ""},
			{ID: "summary", Title: "工单统计", Enabled: true, Project: "", IssueType: "", Filter: ""},
		},
		FieldMappings: map[string][]FieldMap{
			"faults": {
				{Column: "故障标题", FieldID: "summary"},
				{Column: "故障级别", FieldID: "priority"},
				{Column: "故障影响", FieldID: "description"},
				{Column: "故障开始时间", FieldID: "created"},
				{Column: "故障发现时间", FieldID: "created"},
				{Column: "故障解决时间", FieldID: "resolutiondate"},
				{Column: "故障时长", FieldID: "_duration"},
				{Column: "故障原因", FieldID: ""},
				{Column: "故障归属", FieldID: "assignee"},
				{Column: "故障发现方式", FieldID: ""},
			},
			"upgrades": {
				{Column: "升级分类", FieldID: "labels"},
				{Column: "升级内容", FieldID: "summary"},
				{Column: "升级方式", FieldID: ""},
				{Column: "升级单", FieldID: "_key"},
				{Column: "升级状态", FieldID: "resolution"},
			},
			"changes": {
				{Column: "变更单号", FieldID: "_key"},
				{Column: "变更内容", FieldID: "summary"},
				{Column: "变更类型", FieldID: "labels"},
				{Column: "变更时间", FieldID: "created"},
				{Column: "变更状态", FieldID: "status"},
				{Column: "负责人", FieldID: "assignee"},
			},
		},
	}
}
