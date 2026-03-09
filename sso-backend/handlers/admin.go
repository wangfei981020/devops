package handlers

import (
	"encoding/json"
	"net/http"
	"strings"

	"golang.org/x/crypto/bcrypt"

	"sso-backend/database"
)

// ============ Users ============

func HandleAdminListUsers(w http.ResponseWriter, r *http.Request) {
	users, err := database.ListAllUsers()
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "获取用户列表失败")
		return
	}
	jsonOK(w, users)
}

type CreateUserRequest struct {
	Username    string `json:"username"`
	Password    string `json:"password"`
	DisplayName string `json:"display_name"`
	Email       string `json:"email"`
	Phone       string `json:"phone"`
	Status      string `json:"status"`
	AuthSource  string `json:"auth_source"`
}

func HandleAdminCreateUser(w http.ResponseWriter, r *http.Request) {
	var req CreateUserRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, http.StatusBadRequest, "无效的请求")
		return
	}
	if req.Username == "" || req.Password == "" {
		jsonError(w, http.StatusBadRequest, "用户名和密码不能为空")
		return
	}
	if req.Status == "" {
		req.Status = "active"
	}
	if req.AuthSource == "" {
		req.AuthSource = "local"
	}
	if req.DisplayName == "" {
		req.DisplayName = req.Username
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "密码加密失败")
		return
	}

	id := database.GenerateID("user")
	err = database.CreateUser(id, req.Username, string(hash), req.DisplayName, req.Email, req.Phone, req.Status, req.AuthSource)
	if err != nil {
		if strings.Contains(err.Error(), "Duplicate") {
			jsonError(w, http.StatusConflict, "用户名已存在")
			return
		}
		jsonError(w, http.StatusInternalServerError, "创建用户失败")
		return
	}

	adminID := GetUserID(r)
	adminName := GetUsername(r)
	database.SaveAuditLog(adminID, adminName, "create_user", "user", id, "创建用户: "+req.Username, GetClientIP(r), r.UserAgent(), "success")

	jsonSuccess(w, "用户创建成功")
}

type UpdateUserRequest struct {
	DisplayName string `json:"display_name"`
	Email       string `json:"email"`
	Phone       string `json:"phone"`
	Status      string `json:"status"`
	Password    string `json:"password"`
}

func HandleAdminUpdateUser(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/api/admin/users/")
	if id == "" {
		jsonError(w, http.StatusBadRequest, "缺少用户ID")
		return
	}

	var req UpdateUserRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, http.StatusBadRequest, "无效的请求")
		return
	}

	passwordHash := ""
	if req.Password != "" {
		hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
		if err != nil {
			jsonError(w, http.StatusInternalServerError, "密码加密失败")
			return
		}
		passwordHash = string(hash)
	}

	err := database.UpdateUser(id, req.DisplayName, req.Email, req.Phone, req.Status, passwordHash)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "更新用户失败")
		return
	}

	adminID := GetUserID(r)
	adminName := GetUsername(r)
	database.SaveAuditLog(adminID, adminName, "update_user", "user", id, "更新用户信息", GetClientIP(r), r.UserAgent(), "success")

	jsonSuccess(w, "用户更新成功")
}

func HandleAdminDeleteUser(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/api/admin/users/")
	if id == "" {
		jsonError(w, http.StatusBadRequest, "缺少用户ID")
		return
	}

	// Prevent deleting self
	if id == GetUserID(r) {
		jsonError(w, http.StatusBadRequest, "不能删除自己")
		return
	}

	err := database.DeleteUser(id)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "删除用户失败")
		return
	}

	adminID := GetUserID(r)
	adminName := GetUsername(r)
	database.SaveAuditLog(adminID, adminName, "delete_user", "user", id, "删除用户", GetClientIP(r), r.UserAgent(), "success")

	jsonSuccess(w, "用户删除成功")
}

// ============ User Roles ============

type AssignRolesRequest struct {
	RoleIDs []string `json:"role_ids"`
}

func HandleAdminUserRoles(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/admin/users/")
	parts := strings.Split(path, "/")
	if len(parts) < 2 || parts[1] != "roles" {
		jsonError(w, http.StatusBadRequest, "无效的路径")
		return
	}
	userID := parts[0]

	switch r.Method {
	case "GET":
		roles, err := database.GetUserRoleIDs(userID)
		if err != nil {
			jsonError(w, http.StatusInternalServerError, "获取角色失败")
			return
		}
		jsonOK(w, roles)
	case "PUT":
		var req AssignRolesRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			jsonError(w, http.StatusBadRequest, "无效的请求")
			return
		}
		if err := database.SetUserRoles(userID, req.RoleIDs); err != nil {
			jsonError(w, http.StatusInternalServerError, "分配角色失败")
			return
		}
		adminID := GetUserID(r)
		adminName := GetUsername(r)
		database.SaveAuditLog(adminID, adminName, "assign_roles", "user", userID, "分配角色", GetClientIP(r), r.UserAgent(), "success")
		jsonSuccess(w, "角色分配成功")
	default:
		jsonError(w, http.StatusMethodNotAllowed, "Method not allowed")
	}
}

// ============ User Clients ============

type AssignClientsRequest struct {
	ClientIDs []string `json:"client_ids"`
}

func HandleAdminUserClients(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/admin/users/")
	parts := strings.Split(path, "/")
	if len(parts) < 2 || parts[1] != "clients" {
		jsonError(w, http.StatusBadRequest, "无效的路径")
		return
	}
	userID := parts[0]

	switch r.Method {
	case "GET":
		clientIDs, err := database.GetUserClientIDs(userID)
		if err != nil {
			jsonError(w, http.StatusInternalServerError, "获取应用授权失败")
			return
		}
		jsonOK(w, clientIDs)
	case "PUT":
		var req AssignClientsRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			jsonError(w, http.StatusBadRequest, "无效的请求")
			return
		}
		if err := database.SetUserClients(userID, req.ClientIDs); err != nil {
			jsonError(w, http.StatusInternalServerError, "分配应用失败")
			return
		}
		adminID := GetUserID(r)
		adminName := GetUsername(r)
		database.SaveAuditLog(adminID, adminName, "assign_clients", "user", userID, "分配应用授权", GetClientIP(r), r.UserAgent(), "success")
		jsonSuccess(w, "应用授权成功")
	default:
		jsonError(w, http.StatusMethodNotAllowed, "Method not allowed")
	}
}

// ============ Clients ============

func HandleAdminListClients(w http.ResponseWriter, r *http.Request) {
	clients, err := database.ListAllClients()
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "获取应用列表失败")
		return
	}
	jsonOK(w, clients)
}

type CreateClientRequest struct {
	ClientID     string `json:"client_id"`
	ClientSecret string `json:"client_secret"`
	ClientName   string `json:"client_name"`
	Description  string `json:"description"`
	Icon         string `json:"icon"`
	HomeURL      string `json:"home_url"`
	RedirectURIs string `json:"redirect_uris"`
	Environment  string `json:"environment"`
}

func HandleAdminCreateClient(w http.ResponseWriter, r *http.Request) {
	var req CreateClientRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, http.StatusBadRequest, "无效的请求")
		return
	}
	if req.ClientID == "" || req.ClientSecret == "" || req.ClientName == "" {
		jsonError(w, http.StatusBadRequest, "client_id, client_secret, client_name 不能为空")
		return
	}
	if req.RedirectURIs == "" {
		req.RedirectURIs = "[]"
	}

	id := database.GenerateID("client")
	err := database.CreateClient(id, req.ClientID, req.ClientSecret, req.ClientName, req.Description, req.Icon, req.HomeURL, req.RedirectURIs, req.Environment)
	if err != nil {
		if strings.Contains(err.Error(), "Duplicate") {
			jsonError(w, http.StatusConflict, "client_id 已存在")
			return
		}
		jsonError(w, http.StatusInternalServerError, "创建应用失败")
		return
	}

	adminID := GetUserID(r)
	adminName := GetUsername(r)
	database.SaveAuditLog(adminID, adminName, "create_client", "client", id, "创建应用: "+req.ClientName, GetClientIP(r), r.UserAgent(), "success")

	jsonSuccess(w, "应用创建成功")
}

type UpdateClientRequest struct {
	ClientName   string `json:"client_name"`
	ClientSecret string `json:"client_secret"`
	Description  string `json:"description"`
	Icon         string `json:"icon"`
	HomeURL      string `json:"home_url"`
	RedirectURIs string `json:"redirect_uris"`
	Status       string `json:"status"`
	Environment  string `json:"environment"`
}

func HandleAdminUpdateClient(w http.ResponseWriter, r *http.Request) {
	clientID := strings.TrimPrefix(r.URL.Path, "/api/admin/clients/")
	if clientID == "" {
		jsonError(w, http.StatusBadRequest, "缺少 client_id")
		return
	}

	var req UpdateClientRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, http.StatusBadRequest, "无效的请求")
		return
	}

	err := database.UpdateClient(clientID, req.ClientName, req.ClientSecret, req.Description, req.Icon, req.HomeURL, req.RedirectURIs, req.Status, req.Environment)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "更新应用失败")
		return
	}

	adminID := GetUserID(r)
	adminName := GetUsername(r)
	database.SaveAuditLog(adminID, adminName, "update_client", "client", clientID, "更新应用", GetClientIP(r), r.UserAgent(), "success")

	jsonSuccess(w, "应用更新成功")
}

func HandleAdminDeleteClient(w http.ResponseWriter, r *http.Request) {
	clientID := strings.TrimPrefix(r.URL.Path, "/api/admin/clients/")
	if clientID == "" {
		jsonError(w, http.StatusBadRequest, "缺少 client_id")
		return
	}

	err := database.DeleteClient(clientID)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "删除应用失败")
		return
	}

	adminID := GetUserID(r)
	adminName := GetUsername(r)
	database.SaveAuditLog(adminID, adminName, "delete_client", "client", clientID, "删除应用", GetClientIP(r), r.UserAgent(), "success")

	jsonSuccess(w, "应用删除成功")
}

// ============ Roles ============

func HandleAdminListRoles(w http.ResponseWriter, r *http.Request) {
	roles, err := database.ListAllRoles()
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "获取角色列表失败")
		return
	}
	jsonOK(w, roles)
}

// ============ Audit Logs ============

func HandleAdminAuditLogs(w http.ResponseWriter, r *http.Request) {
	limit := 200
	logs, err := database.ListAuditLogs(limit)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "获取审计日志失败")
		return
	}
	jsonOK(w, logs)
}

// ============ Router helper ============

func HandleAdminUsers(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/admin/users")

	// /api/admin/users/{id}/roles
	if strings.Contains(path, "/roles") {
		HandleAdminUserRoles(w, r)
		return
	}
	// /api/admin/users/{id}/clients
	if strings.Contains(path, "/clients") {
		HandleAdminUserClients(w, r)
		return
	}

	switch r.Method {
	case "GET":
		if path == "" || path == "/" {
			HandleAdminListUsers(w, r)
		}
	case "POST":
		HandleAdminCreateUser(w, r)
	case "PUT":
		HandleAdminUpdateUser(w, r)
	case "DELETE":
		HandleAdminDeleteUser(w, r)
	default:
		jsonError(w, http.StatusMethodNotAllowed, "Method not allowed")
	}
}

func HandleAdminClients(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case "GET":
		HandleAdminListClients(w, r)
	case "POST":
		HandleAdminCreateClient(w, r)
	case "PUT":
		HandleAdminUpdateClient(w, r)
	case "DELETE":
		HandleAdminDeleteClient(w, r)
	default:
		jsonError(w, http.StatusMethodNotAllowed, "Method not allowed")
	}
}

// ============ Environments ============

func HandleAdminEnvironments(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/admin/environments")
	id := strings.TrimPrefix(path, "/")

	switch r.Method {
	case "GET":
		envs, err := database.ListEnvironments()
		if err != nil {
			jsonError(w, http.StatusInternalServerError, "获取环境列表失败")
			return
		}
		jsonOK(w, envs)
	case "POST":
		var req struct {
			Name      string `json:"name"`
			Label     string `json:"label"`
			Color     string `json:"color"`
			SortOrder int    `json:"sort_order"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			jsonError(w, http.StatusBadRequest, "无效的请求")
			return
		}
		if req.Name == "" || req.Label == "" {
			jsonError(w, http.StatusBadRequest, "name 和 label 不能为空")
			return
		}
		if req.Color == "" {
			req.Color = "#6b7280"
		}
		newID := database.GenerateID("env")
		if err := database.CreateEnvironment(newID, req.Name, req.Label, req.Color, req.SortOrder); err != nil {
			if strings.Contains(err.Error(), "Duplicate") {
				jsonError(w, http.StatusConflict, "环境名称已存在")
				return
			}
			jsonError(w, http.StatusInternalServerError, "创建环境失败")
			return
		}
		adminID := GetUserID(r)
		adminName := GetUsername(r)
		database.SaveAuditLog(adminID, adminName, "create_env", "environment", newID, "创建环境: "+req.Label, GetClientIP(r), r.UserAgent(), "success")
		jsonSuccess(w, "环境创建成功")
	case "PUT":
		if id == "" {
			jsonError(w, http.StatusBadRequest, "缺少环境ID")
			return
		}
		var req struct {
			Name      string `json:"name"`
			Label     string `json:"label"`
			Color     string `json:"color"`
			SortOrder int    `json:"sort_order"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			jsonError(w, http.StatusBadRequest, "无效的请求")
			return
		}
		if err := database.UpdateEnvironment(id, req.Name, req.Label, req.Color, req.SortOrder); err != nil {
			jsonError(w, http.StatusInternalServerError, "更新环境失败")
			return
		}
		jsonSuccess(w, "环境更新成功")
	case "DELETE":
		if id == "" {
			jsonError(w, http.StatusBadRequest, "缺少环境ID")
			return
		}
		if err := database.DeleteEnvironment(id); err != nil {
			jsonError(w, http.StatusInternalServerError, "删除环境失败")
			return
		}
		adminID := GetUserID(r)
		adminName := GetUsername(r)
		database.SaveAuditLog(adminID, adminName, "delete_env", "environment", id, "删除环境", GetClientIP(r), r.UserAgent(), "success")
		jsonSuccess(w, "环境删除成功")
	default:
		jsonError(w, http.StatusMethodNotAllowed, "Method not allowed")
	}
}
