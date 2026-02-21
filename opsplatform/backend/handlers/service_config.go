package handlers

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"

	"opsplatform/database"
	"opsplatform/models"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
)

// HandleGetServiceConfigs 获取服务配置列表
func HandleGetServiceConfigs(w http.ResponseWriter, r *http.Request) {
	project := r.URL.Query().Get("project")
	env := r.URL.Query().Get("env")
	serviceType := r.URL.Query().Get("type")

	query := `SELECT id, project, service_name, service_type, domain, port, env, namespace, replicas, image, COALESCE(remark,''), status, sort_order, created_at, COALESCE(created_by,''), COALESCE(updated_at,''), COALESCE(updated_by,'') FROM service_configs WHERE 1=1`
	args := []interface{}{}

	if project != "" {
		query += " AND project = ?"
		args = append(args, project)
	}
	if env != "" {
		query += " AND env = ?"
		args = append(args, env)
	}
	if serviceType != "" {
		query += " AND service_type = ?"
		args = append(args, serviceType)
	}

	query += " ORDER BY sort_order ASC, project ASC, service_name ASC"

	rows, err := database.DB.Query(query, args...)
	if err != nil {
		log.Printf("[服务配置] 查询失败: %v", err)
		sendError(w, "查询失败", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	services := make([]models.ServiceConfig, 0)
	for rows.Next() {
		var s models.ServiceConfig
		err := rows.Scan(&s.ID, &s.Project, &s.ServiceName, &s.ServiceType, &s.Domain, &s.Port, &s.Env, &s.Namespace, &s.Replicas, &s.Image, &s.Remark, &s.Status, &s.SortOrder, &s.CreatedAt, &s.CreatedBy, &s.UpdatedAt, &s.UpdatedBy)
		if err != nil {
			log.Printf("扫描服务配置失败: %v", err)
			continue
		}
		services = append(services, s)
	}

	// 查询每个服务的依赖
	for i := range services {
		deps, err := getServiceDependencies(services[i].ID)
		if err != nil {
			log.Printf("查询服务依赖失败: %v", err)
		}
		services[i].Dependencies = deps
	}

	respondJSON(w, http.StatusOK, services)
}

// HandleGetServiceConfig 获取单个服务配置
func HandleGetServiceConfig(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id := vars["id"]

	var s models.ServiceConfig
	err := database.DB.QueryRow(`SELECT id, project, service_name, service_type, domain, port, env, namespace, replicas, image, COALESCE(remark,''), status, sort_order, created_at, COALESCE(created_by,''), COALESCE(updated_at,''), COALESCE(updated_by,'') FROM service_configs WHERE id = ?`, id).
		Scan(&s.ID, &s.Project, &s.ServiceName, &s.ServiceType, &s.Domain, &s.Port, &s.Env, &s.Namespace, &s.Replicas, &s.Image, &s.Remark, &s.Status, &s.SortOrder, &s.CreatedAt, &s.CreatedBy, &s.UpdatedAt, &s.UpdatedBy)
	if err == sql.ErrNoRows {
		sendError(w, "服务配置不存在", http.StatusNotFound)
		return
	}
	if err != nil {
		sendError(w, "查询失败", http.StatusInternalServerError)
		return
	}

	deps, _ := getServiceDependencies(s.ID)
	s.Dependencies = deps

	respondJSON(w, http.StatusOK, s)
}

// HandleCreateServiceConfig 创建服务配置
func HandleCreateServiceConfig(w http.ResponseWriter, r *http.Request) {
	var s models.ServiceConfig
	if err := json.NewDecoder(r.Body).Decode(&s); err != nil {
		sendError(w, "请求参数无效", http.StatusBadRequest)
		return
	}

	if s.ServiceName == "" {
		sendError(w, "服务名称不能为空", http.StatusBadRequest)
		return
	}

	s.ID = uuid.New().String()
	now := time.Now().Format("2006-01-02 15:04:05")
	operator := r.Header.Get("X-Operator")

	_, err := database.DB.Exec(`INSERT INTO service_configs (id, project, service_name, service_type, domain, port, env, namespace, replicas, image, remark, status, sort_order, created_at, created_by, updated_at, updated_by) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		s.ID, s.Project, s.ServiceName, s.ServiceType, s.Domain, s.Port, s.Env, s.Namespace, s.Replicas, s.Image, s.Remark, s.Status, s.SortOrder, now, operator, now, operator)
	if err != nil {
		log.Printf("[服务配置] 创建失败: %v", err)
		sendError(w, "创建失败", http.StatusInternalServerError)
		return
	}

	s.CreatedAt = now
	s.CreatedBy = operator
	s.UpdatedAt = now
	s.UpdatedBy = operator

	// 如果携带了依赖，一起创建
	if len(s.Dependencies) > 0 {
		createDependenciesForService(s.ID, s.Dependencies, now)
	}

	// 重新加载依赖
	deps, _ := getServiceDependencies(s.ID)
	s.Dependencies = deps

	respondJSON(w, http.StatusCreated, s)
}

// createDependenciesForService 为服务批量创建依赖
func createDependenciesForService(serviceID string, deps []models.ServiceDependency, now string) {
	for _, dep := range deps {
		if dep.DependencyName == "" {
			continue
		}
		dep.ID = uuid.New().String()
		if dep.Status == "" {
			dep.Status = "active"
		}
		if dep.DependencyType == "" {
			dep.DependencyType = "other"
		}

		encPwd := ""
		if dep.Password != "" {
			var err error
			encPwd, err = encryptPassword(dep.Password)
			if err != nil {
				log.Printf("加密密码失败: %v", err)
				encPwd = dep.Password
			}
		}

		database.DB.Exec(`INSERT INTO service_dependencies (id, service_id, dependency_type, dependency_name, host, port, database_name, username, password, conn_string, remark, status, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			dep.ID, serviceID, dep.DependencyType, dep.DependencyName, dep.Host, dep.Port, dep.Database, dep.Username, encPwd, dep.ConnString, dep.Remark, dep.Status, now, now)
	}
}

// HandleBatchCreateServiceConfigs 批量创建服务配置
func HandleBatchCreateServiceConfigs(w http.ResponseWriter, r *http.Request) {
	var services []models.ServiceConfig
	if err := json.NewDecoder(r.Body).Decode(&services); err != nil {
		sendError(w, "请求参数无效", http.StatusBadRequest)
		return
	}

	if len(services) == 0 {
		sendError(w, "服务列表不能为空", http.StatusBadRequest)
		return
	}

	operator := r.Header.Get("X-Operator")
	now := time.Now().Format("2006-01-02 15:04:05")

	successCount := 0
	failCount := 0
	var failReasons []string

	for _, s := range services {
		if s.ServiceName == "" {
			failCount++
			failReasons = append(failReasons, "服务名为空")
			continue
		}
		if s.Status == "" {
			s.Status = "active"
		}
		if s.ServiceType == "" {
			s.ServiceType = "backend"
		}
		if s.Env == "" {
			s.Env = "prod"
		}

		s.ID = uuid.New().String()
		_, err := database.DB.Exec(`INSERT INTO service_configs (id, project, service_name, service_type, domain, port, env, namespace, replicas, image, remark, status, sort_order, created_at, created_by, updated_at, updated_by) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			s.ID, s.Project, s.ServiceName, s.ServiceType, s.Domain, s.Port, s.Env, s.Namespace, s.Replicas, s.Image, s.Remark, s.Status, s.SortOrder, now, operator, now, operator)
		if err != nil {
			failCount++
			log.Printf("[服务配置] 批量创建失败 %s: %v", s.ServiceName, err)
			failReasons = append(failReasons, s.ServiceName+": 创建失败")
			continue
		}

		// 创建依赖
		if len(s.Dependencies) > 0 {
			createDependenciesForService(s.ID, s.Dependencies, now)
		}

		successCount++
	}

	respondJSON(w, http.StatusOK, map[string]interface{}{
		"success_count": successCount,
		"fail_count":    failCount,
		"fail_reasons":  failReasons,
		"message":       fmt.Sprintf("成功 %d 个，失败 %d 个", successCount, failCount),
	})
}

// HandleUpdateServiceConfig 更新服务配置
func HandleUpdateServiceConfig(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id := vars["id"]

	var s models.ServiceConfig
	if err := json.NewDecoder(r.Body).Decode(&s); err != nil {
		sendError(w, "请求参数无效", http.StatusBadRequest)
		return
	}

	operator := r.Header.Get("X-Operator")
	now := time.Now().Format("2006-01-02 15:04:05")

	_, err := database.DB.Exec(`UPDATE service_configs SET project=?, service_name=?, service_type=?, domain=?, port=?, env=?, namespace=?, replicas=?, image=?, remark=?, status=?, sort_order=?, updated_at=?, updated_by=? WHERE id=?`,
		s.Project, s.ServiceName, s.ServiceType, s.Domain, s.Port, s.Env, s.Namespace, s.Replicas, s.Image, s.Remark, s.Status, s.SortOrder, now, operator, id)
	if err != nil {
		log.Printf("[服务配置] 更新失败: %v", err)
			sendError(w, "更新失败", http.StatusInternalServerError)
		return
	}

	respondJSON(w, http.StatusOK, map[string]string{"message": "更新成功"})
}

// HandleDeleteServiceConfig 删除服务配置
func HandleDeleteServiceConfig(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id := vars["id"]

	// 同时删除依赖
	database.DB.Exec("DELETE FROM service_dependencies WHERE service_id = ?", id)
	_, err := database.DB.Exec("DELETE FROM service_configs WHERE id = ?", id)
	if err != nil {
		sendError(w, "删除失败", http.StatusInternalServerError)
		return
	}

	respondJSON(w, http.StatusOK, map[string]string{"message": "删除成功"})
}

// HandleGetServiceDependencies 获取服务依赖列表
func HandleGetServiceDependencies(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	serviceID := vars["id"]

	deps, err := getServiceDependencies(serviceID)
	if err != nil {
		sendError(w, "查询失败", http.StatusInternalServerError)
		return
	}

	respondJSON(w, http.StatusOK, deps)
}

// HandleCreateServiceDependency 创建服务依赖
func HandleCreateServiceDependency(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	serviceID := vars["id"]

	var dep models.ServiceDependency
	if err := json.NewDecoder(r.Body).Decode(&dep); err != nil {
		sendError(w, "请求参数无效", http.StatusBadRequest)
		return
	}

	if dep.DependencyName == "" {
		sendError(w, "依赖名称不能为空", http.StatusBadRequest)
		return
	}

	dep.ID = uuid.New().String()
	dep.ServiceID = serviceID
	now := time.Now().Format("2006-01-02 15:04:05")

	// 加密密码
	encPwd := ""
	if dep.Password != "" {
		var err error
		encPwd, err = encryptPassword(dep.Password)
		if err != nil {
			log.Printf("加密密码失败: %v", err)
			encPwd = dep.Password
		}
	}

	_, err := database.DB.Exec(`INSERT INTO service_dependencies (id, service_id, dependency_type, dependency_name, host, port, database_name, username, password, conn_string, remark, status, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		dep.ID, serviceID, dep.DependencyType, dep.DependencyName, dep.Host, dep.Port, dep.Database, dep.Username, encPwd, dep.ConnString, dep.Remark, dep.Status, now, now)
	if err != nil {
		log.Printf("[服务配置] 创建失败: %v", err)
		sendError(w, "创建失败", http.StatusInternalServerError)
		return
	}

	dep.CreatedAt = now
	dep.UpdatedAt = now
	dep.Password = "" // 不返回密码

	respondJSON(w, http.StatusCreated, dep)
}

// HandleUpdateServiceDependency 更新服务依赖
func HandleUpdateServiceDependency(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	depID := vars["depId"]

	var dep models.ServiceDependency
	if err := json.NewDecoder(r.Body).Decode(&dep); err != nil {
		sendError(w, "请求参数无效", http.StatusBadRequest)
		return
	}

	now := time.Now().Format("2006-01-02 15:04:05")

	// 如果密码不为空则重新加密
	if dep.Password != "" {
		encPwd, err := encryptPassword(dep.Password)
		if err != nil {
			log.Printf("加密密码失败: %v", err)
			encPwd = dep.Password
		}
		_, err = database.DB.Exec(`UPDATE service_dependencies SET dependency_type=?, dependency_name=?, host=?, port=?, database_name=?, username=?, password=?, conn_string=?, remark=?, status=?, updated_at=? WHERE id=?`,
			dep.DependencyType, dep.DependencyName, dep.Host, dep.Port, dep.Database, dep.Username, encPwd, dep.ConnString, dep.Remark, dep.Status, now, depID)
		if err != nil {
			log.Printf("[服务配置] 更新失败: %v", err)
			sendError(w, "更新失败", http.StatusInternalServerError)
			return
		}
	} else {
		// 不更新密码
		_, err := database.DB.Exec(`UPDATE service_dependencies SET dependency_type=?, dependency_name=?, host=?, port=?, database_name=?, username=?, conn_string=?, remark=?, status=?, updated_at=? WHERE id=?`,
			dep.DependencyType, dep.DependencyName, dep.Host, dep.Port, dep.Database, dep.Username, dep.ConnString, dep.Remark, dep.Status, now, depID)
		if err != nil {
			log.Printf("[服务配置] 更新失败: %v", err)
			sendError(w, "更新失败", http.StatusInternalServerError)
			return
		}
	}

	respondJSON(w, http.StatusOK, map[string]string{"message": "更新成功"})
}

// HandleDeleteServiceDependency 删除服务依赖
func HandleDeleteServiceDependency(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	depID := vars["depId"]

	_, err := database.DB.Exec("DELETE FROM service_dependencies WHERE id = ?", depID)
	if err != nil {
		sendError(w, "删除失败", http.StatusInternalServerError)
		return
	}

	respondJSON(w, http.StatusOK, map[string]string{"message": "删除成功"})
}

// HandleGetServiceDepPassword 获取依赖的解密密码
func HandleGetServiceDepPassword(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	depID := vars["depId"]

	var encPwd string
	err := database.DB.QueryRow("SELECT COALESCE(password,'') FROM service_dependencies WHERE id = ?", depID).Scan(&encPwd)
	if err != nil {
		sendError(w, "未找到", http.StatusNotFound)
		return
	}

	pwd := ""
	if encPwd != "" {
		pwd, err = decryptPassword(encPwd)
		if err != nil {
			log.Printf("解密密码失败: %v", err)
			pwd = "(解密失败)"
		}
	}

	respondJSON(w, http.StatusOK, map[string]string{"password": pwd})
}

// HandleGetServiceProjects 获取所有项目列表（去重）
func HandleGetServiceProjects(w http.ResponseWriter, r *http.Request) {
	rows, err := database.DB.Query("SELECT DISTINCT project FROM service_configs WHERE project != '' ORDER BY project")
	if err != nil {
		sendError(w, "查询失败", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	projects := make([]string, 0)
	for rows.Next() {
		var p string
		rows.Scan(&p)
		projects = append(projects, p)
	}

	respondJSON(w, http.StatusOK, projects)
}

// getServiceDependencies 获取服务依赖列表（内部使用）
func getServiceDependencies(serviceID string) ([]models.ServiceDependency, error) {
	rows, err := database.DB.Query(`SELECT id, service_id, dependency_type, dependency_name, host, port, COALESCE(database_name,''), COALESCE(username,''), COALESCE(conn_string,''), COALESCE(remark,''), status, created_at, updated_at FROM service_dependencies WHERE service_id = ? ORDER BY dependency_type, dependency_name`, serviceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	deps := make([]models.ServiceDependency, 0)
	for rows.Next() {
		var d models.ServiceDependency
		err := rows.Scan(&d.ID, &d.ServiceID, &d.DependencyType, &d.DependencyName, &d.Host, &d.Port, &d.Database, &d.Username, &d.ConnString, &d.Remark, &d.Status, &d.CreatedAt, &d.UpdatedAt)
		if err != nil {
			log.Printf("扫描依赖数据失败: %v", err)
			continue
		}
		// 密码不返回
		d.Password = ""
		deps = append(deps, d)
	}

	return deps, nil
}
