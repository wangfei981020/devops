package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"opsplatform/database"
	"opsplatform/models"

	"github.com/gorilla/mux"
)

// HandleGetDataSources 获取所有数据源
func HandleGetDataSources(w http.ResponseWriter, r *http.Request) {
	sources, err := GetAllDataSources()
	if err != nil {
		SafeError(w, "操作失败", http.StatusInternalServerError, err)
		return
	}

	// 隐藏密码
	for _, s := range sources {
		s.Password = ""
		s.Token = ""
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	json.NewEncoder(w).Encode(sources)
}

// HandleAddDataSource 添加数据源
func HandleAddDataSource(w http.ResponseWriter, r *http.Request) {
	var req struct {
		DataSource models.DataSource `json:"datasource"`
		Operator   string            `json:"operator"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "无效的请求数据", http.StatusBadRequest)
		return
	}

	if req.DataSource.Name == "" || req.DataSource.URL == "" {
		http.Error(w, "名称和URL不能为空", http.StatusBadRequest)
		return
	}

	if err := AddDataSource(&req.DataSource, req.Operator); err != nil {
		SafeError(w, "操作失败", http.StatusInternalServerError, err)
		return
	}

	// 记录审计日志
	changes := fmt.Sprintf("添加数据源: %s (%s), 类型=%s, URL=%s", req.DataSource.Name, req.DataSource.Description, req.DataSource.Type, req.DataSource.URL)
	AddAuditLogFromRequest(r, "create", "datasource:"+req.DataSource.ID, req.Operator, "", "", changes)

	req.DataSource.Password = ""
	req.DataSource.Token = ""
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(req.DataSource)
}

// HandleUpdateDataSource 更新数据源
func HandleUpdateDataSource(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id := vars["id"]

	// 获取旧数据源信息
	oldDS, _ := GetDataSourceByID(id)

	var ds models.DataSource
	if err := json.NewDecoder(r.Body).Decode(&ds); err != nil {
		http.Error(w, "无效的请求数据", http.StatusBadRequest)
		return
	}

	operator := r.Header.Get("X-Operator")
	if operator == "" {
		operator = "system"
	}

	if err := UpdateDataSource(id, &ds); err != nil {
		SafeError(w, "资源不存在", http.StatusNotFound, err)
		return
	}

	// 记录审计日志
	var changes []string
	if oldDS != nil {
		if oldDS.Name != ds.Name && ds.Name != "" {
			changes = append(changes, fmt.Sprintf("名称: %s → %s", oldDS.Name, ds.Name))
		}
		if oldDS.URL != ds.URL && ds.URL != "" {
			changes = append(changes, fmt.Sprintf("URL: %s → %s", oldDS.URL, ds.URL))
		}
		if oldDS.Status != ds.Status && ds.Status != "" {
			changes = append(changes, fmt.Sprintf("状态: %s → %s", oldDS.Status, ds.Status))
		}
	}
	if len(changes) > 0 {
		AddAuditLogFromRequest(r, "update", "datasource:"+id, operator, "", "", fmt.Sprintf("更新数据源 %s: %s", ds.Name, strings.Join(changes, ", ")))
	}

	ds.Password = ""
	ds.Token = ""
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	json.NewEncoder(w).Encode(ds)
}

// HandleDeleteDataSource 删除数据源
func HandleDeleteDataSource(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id := vars["id"]

	// 获取数据源信息用于审计
	ds, _ := GetDataSourceByID(id)

	operator := r.Header.Get("X-Operator")
	if operator == "" {
		operator = "system"
	}

	if err := DeleteDataSource(id); err != nil {
		SafeError(w, "资源不存在", http.StatusNotFound, err)
		return
	}

	// 记录审计日志
	if ds != nil {
		changes := fmt.Sprintf("删除数据源: %s (%s)", ds.Name, ds.Type)
		AddAuditLogFromRequest(r, "delete", "datasource:"+id, operator, "", "", changes)
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	json.NewEncoder(w).Encode(map[string]string{"message": "删除成功"})
}

// HandleTestDataSource 测试数据源连接
func HandleTestDataSource(w http.ResponseWriter, r *http.Request) {
	var ds models.DataSource
	if err := json.NewDecoder(r.Body).Decode(&ds); err != nil {
		http.Error(w, "无效的请求数据", http.StatusBadRequest)
		return
	}

	success, message := testConnection(&ds)

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": success,
		"message": message,
	})
}

func testConnection(ds *models.DataSource) (bool, string) {
	client := &http.Client{Timeout: 10 * time.Second}

	testURL := ds.URL
	switch ds.Type {
	case "prometheus":
		// 测试 Prometheus API
		testURL = ds.URL + "/-/healthy"
	case "jira":
		// 测试 Jira API
		u, err := url.Parse(ds.URL)
		if err != nil {
			return false, "无效的URL"
		}
		testURL = u.Scheme + "://" + u.Host + "/rest/api/2/serverInfo"
	case "domain":
		// 域名检测服务
		testURL = ds.URL + "/health"
	}

	req, err := http.NewRequest("GET", testURL, nil)
	if err != nil {
		return false, "创建请求失败: " + err.Error()
	}

	if ds.Token != "" {
		req.Header.Set("Authorization", "Bearer "+ds.Token)
	} else if ds.Username != "" && ds.Password != "" {
		req.SetBasicAuth(ds.Username, ds.Password)
	}

	resp, err := client.Do(req)
	if err != nil {
		return false, "连接失败: " + err.Error()
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return true, "连接成功"
	}

	return false, fmt.Sprintf("连接失败，状态码: %d", resp.StatusCode)
}

// 数据库操作函数
func GetAllDataSources() ([]*models.DataSource, error) {
	rows, err := database.DB.Query(`
		SELECT id, name, type, url, COALESCE(username, ''), COALESCE(password, ''), 
		       COALESCE(token, ''), COALESCE(description, ''), status, created_at, COALESCE(created_by, '')
		FROM datasources ORDER BY created_at DESC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var sources []*models.DataSource
	for rows.Next() {
		s := &models.DataSource{}
		err := rows.Scan(&s.ID, &s.Name, &s.Type, &s.URL, &s.Username, &s.Password,
			&s.Token, &s.Description, &s.Status, &s.CreatedAt, &s.CreatedBy)
		if err != nil {
			return nil, err
		}
		sources = append(sources, s)
	}
	return sources, nil
}

func GetDataSource(id string) (*models.DataSource, error) {
	s := &models.DataSource{}
	err := database.DB.QueryRow(`
		SELECT id, name, type, url, COALESCE(username, ''), COALESCE(password, ''),
		       COALESCE(token, ''), COALESCE(description, ''), status, created_at, COALESCE(created_by, '')
		FROM datasources WHERE id = ?
	`, id).Scan(&s.ID, &s.Name, &s.Type, &s.URL, &s.Username, &s.Password,
		&s.Token, &s.Description, &s.Status, &s.CreatedAt, &s.CreatedBy)
	if err != nil {
		return nil, nil
	}
	return s, nil
}

func AddDataSource(s *models.DataSource, operator string) error {
	s.ID = fmt.Sprintf("ds_%d", timeNow().UnixNano())
	s.CreatedAt = timeNow().Format("2006-01-02 15:04:05")
	s.CreatedBy = operator

	if s.Status == "" {
		s.Status = "active"
	}

	_, err := database.DB.Exec(`
		INSERT INTO datasources (id, name, type, url, username, password, token, description, status, created_at, created_by)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, s.ID, s.Name, s.Type, s.URL, s.Username, s.Password, s.Token, s.Description, s.Status, s.CreatedAt, s.CreatedBy)
	return err
}

func UpdateDataSource(id string, s *models.DataSource) error {
	oldDS, err := GetDataSource(id)
	if err != nil || oldDS == nil {
		return fmt.Errorf("数据源不存在: %s", id)
	}

	// 如果密码为空，保留旧密码
	if s.Password == "" {
		s.Password = oldDS.Password
	}
	if s.Token == "" {
		s.Token = oldDS.Token
	}

	_, err = database.DB.Exec(`
		UPDATE datasources SET name=?, type=?, url=?, username=?, password=?, token=?, description=?, status=?
		WHERE id=?
	`, s.Name, s.Type, s.URL, s.Username, s.Password, s.Token, s.Description, s.Status, id)
	return err
}

func DeleteDataSource(id string) error {
	_, err := database.DB.Exec("DELETE FROM datasources WHERE id=?", id)
	return err
}






