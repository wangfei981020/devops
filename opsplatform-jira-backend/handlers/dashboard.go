package handlers

import (
	"encoding/json"
	"log"
	"net/http"
	"sync"

	"opsplatform-jira-backend/jira"
)

// DashboardData 仪表盘聚合数据
type DashboardData struct {
	ProjectsCount       int                      `json:"projects_count"`
	TotalIssues         int                      `json:"total_issues"`
	OpenIssues          int                      `json:"open_issues"`
	ResolvedIssues      int                      `json:"resolved_issues"`
	StatusDistribution  []map[string]interface{} `json:"status_distribution"`
	PriorityDistribution []map[string]interface{} `json:"priority_distribution"`
	AssigneeWorkload    []map[string]interface{} `json:"assignee_workload"`
	RecentCreated       int                      `json:"recent_created"`
	RecentResolved      int                      `json:"recent_resolved"`
}

// HandleGetDashboardData 获取仪表盘聚合数据
func HandleGetDashboardData(w http.ResponseWriter, r *http.Request) {
	client, err := jira.NewClient()
	if err != nil {
		respondBadRequest(w, err.Error())
		return
	}

	project := r.URL.Query().Get("project")
	jqlPrefix := ""
	if project != "" {
		jqlPrefix = "project = " + project + " AND "
	}

	dashboard := &DashboardData{}
	var wg sync.WaitGroup
	var mu sync.Mutex

	// 1. 获取项目数量
	wg.Add(1)
	go func() {
		defer wg.Done()
		var projects []json.RawMessage
		if err := client.GetJSON("/rest/api/2/project", &projects); err == nil {
			mu.Lock()
			dashboard.ProjectsCount = len(projects)
			mu.Unlock()
		}
	}()

	// 2. 获取所有工单统计
	wg.Add(1)
	go func() {
		defer wg.Done()
		var result struct {
			Total int `json:"total"`
		}
		if err := client.GetJSON("/rest/api/2/search?jql="+jqlPrefix+"order by created DESC&maxResults=0", &result); err == nil {
			mu.Lock()
			dashboard.TotalIssues = result.Total
			mu.Unlock()
		}
	}()

	// 3. 获取未解决工单数
	wg.Add(1)
	go func() {
		defer wg.Done()
		var result struct {
			Total int `json:"total"`
		}
		if err := client.GetJSON("/rest/api/2/search?jql="+jqlPrefix+"resolution = Unresolved&maxResults=0", &result); err == nil {
			mu.Lock()
			dashboard.OpenIssues = result.Total
			mu.Unlock()
		}
	}()

	// 4. 获取最近30天创建的工单（用于状态/优先级/负责人分布）
	wg.Add(1)
	go func() {
		defer wg.Done()
		var result struct {
			Total  int `json:"total"`
			Issues []struct {
				Fields struct {
					Status   map[string]interface{} `json:"status"`
					Priority map[string]interface{} `json:"priority"`
					Assignee map[string]interface{} `json:"assignee"`
				} `json:"fields"`
			} `json:"issues"`
		}
		jql := jqlPrefix + "created >= -30d ORDER BY created DESC"
		if err := client.GetJSON("/rest/api/2/search?jql="+jql+"&maxResults=1000&fields=status,priority,assignee", &result); err != nil {
			log.Printf("[Dashboard] 获取最近工单失败: %v", err)
			return
		}

		mu.Lock()
		dashboard.RecentCreated = result.Total

		// 统计状态分布
		statusMap := make(map[string]int)
		priorityMap := make(map[string]int)
		assigneeMap := make(map[string]int)

		for _, issue := range result.Issues {
			if issue.Fields.Status != nil {
				name, _ := issue.Fields.Status["name"].(string)
				if name != "" {
					statusMap[name]++
				}
			}
			if issue.Fields.Priority != nil {
				name, _ := issue.Fields.Priority["name"].(string)
				if name != "" {
					priorityMap[name]++
				}
			}
			if issue.Fields.Assignee != nil {
				name, _ := issue.Fields.Assignee["displayName"].(string)
				if name == "" {
					name, _ = issue.Fields.Assignee["name"].(string)
				}
				if name != "" {
					assigneeMap[name]++
				}
			} else {
				assigneeMap["未分配"]++
			}
		}

		for name, count := range statusMap {
			dashboard.StatusDistribution = append(dashboard.StatusDistribution, map[string]interface{}{
				"name": name, "value": count,
			})
		}
		for name, count := range priorityMap {
			dashboard.PriorityDistribution = append(dashboard.PriorityDistribution, map[string]interface{}{
				"name": name, "value": count,
			})
		}
		for name, count := range assigneeMap {
			dashboard.AssigneeWorkload = append(dashboard.AssigneeWorkload, map[string]interface{}{
				"name": name, "value": count,
			})
		}
		mu.Unlock()
	}()

	// 5. 获取最近30天解决的工单数
	wg.Add(1)
	go func() {
		defer wg.Done()
		var result struct {
			Total int `json:"total"`
		}
		jql := jqlPrefix + "resolved >= -30d"
		if err := client.GetJSON("/rest/api/2/search?jql="+jql+"&maxResults=0", &result); err == nil {
			mu.Lock()
			dashboard.RecentResolved = result.Total
			mu.Unlock()
		}
	}()

	wg.Wait()

	dashboard.ResolvedIssues = dashboard.TotalIssues - dashboard.OpenIssues
	if dashboard.StatusDistribution == nil {
		dashboard.StatusDistribution = []map[string]interface{}{}
	}
	if dashboard.PriorityDistribution == nil {
		dashboard.PriorityDistribution = []map[string]interface{}{}
	}
	if dashboard.AssigneeWorkload == nil {
		dashboard.AssigneeWorkload = []map[string]interface{}{}
	}

	respondSuccess(w, dashboard)
}
