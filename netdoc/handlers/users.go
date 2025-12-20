package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"

	"netdoc/database"
	"netdoc/models"

	"github.com/gorilla/mux"
)

// HandleLogin 用户登录
func HandleLogin(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "无效的请求数据", http.StatusBadRequest)
		return
	}

	user, err := GetUserByUsername(req.Username)
	if err != nil || user == nil {
		http.Error(w, "用户不存在", http.StatusUnauthorized)
		return
	}

	if user.Password != req.Password {
		http.Error(w, "密码错误", http.StatusUnauthorized)
		return
	}

	if user.Status != "active" {
		http.Error(w, "用户已被禁用", http.StatusForbidden)
		return
	}

	// 返回用户信息（不含密码）
	user.Password = ""
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	json.NewEncoder(w).Encode(user)
}

// HandleGetUsers 获取所有用户
func HandleGetUsers(w http.ResponseWriter, r *http.Request) {
	users, err := GetAllUsers()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// 移除密码
	for _, u := range users {
		u.Password = ""
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	json.NewEncoder(w).Encode(users)
}

// HandleCreateUser 创建用户
func HandleCreateUser(w http.ResponseWriter, r *http.Request) {
	var req struct {
		User      models.User `json:"user"`
		CreatedBy string      `json:"created_by"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "无效的请求数据", http.StatusBadRequest)
		return
	}

	if req.User.Username == "" || req.User.Password == "" || req.User.DisplayName == "" {
		http.Error(w, "用户名、密码和显示名称不能为空", http.StatusBadRequest)
		return
	}

	// 检查用户名是否已存在
	existing, _ := GetUserByUsername(req.User.Username)
	if existing != nil {
		http.Error(w, "用户名已存在", http.StatusBadRequest)
		return
	}

	if err := AddUser(&req.User); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	req.User.Password = ""
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(req.User)
}

// HandleUpdateUser 更新用户
func HandleUpdateUser(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id := vars["id"]

	var user models.User
	if err := json.NewDecoder(r.Body).Decode(&user); err != nil {
		http.Error(w, "无效的请求数据", http.StatusBadRequest)
		return
	}

	if err := UpdateUser(id, &user); err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	user.Password = ""
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	json.NewEncoder(w).Encode(user)
}

// HandleDeleteUser 删除用户
func HandleDeleteUser(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id := vars["id"]

	// 检查是否是管理员
	user, err := GetUserByID(id)
	if err != nil || user == nil {
		http.Error(w, "用户不存在", http.StatusNotFound)
		return
	}

	if user.Role == "admin" {
		http.Error(w, "不能删除管理员用户", http.StatusForbidden)
		return
	}

	if err := DeleteUser(id); err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	json.NewEncoder(w).Encode(map[string]string{"message": "删除成功"})
}

// 数据库操作函数
func GetAllUsers() ([]*models.User, error) {
	rows, err := database.DB.Query(`
		SELECT id, username, password, display_name, role, status, COALESCE(permissions, ''), created_at
		FROM users ORDER BY created_at DESC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var users []*models.User
	for rows.Next() {
		u := &models.User{}
		err := rows.Scan(&u.ID, &u.Username, &u.Password, &u.DisplayName, &u.Role, &u.Status, &u.Permissions, &u.CreatedAt)
		if err != nil {
			return nil, err
		}
		users = append(users, u)
	}
	return users, nil
}

func GetUserByUsername(username string) (*models.User, error) {
	u := &models.User{}
	err := database.DB.QueryRow(`
		SELECT id, username, password, display_name, role, status, COALESCE(permissions, ''), created_at
		FROM users WHERE username = ?
	`, username).Scan(&u.ID, &u.Username, &u.Password, &u.DisplayName, &u.Role, &u.Status, &u.Permissions, &u.CreatedAt)
	if err != nil {
		return nil, nil
	}
	return u, nil
}

func GetUserByID(id string) (*models.User, error) {
	u := &models.User{}
	err := database.DB.QueryRow(`
		SELECT id, username, password, display_name, role, status, COALESCE(permissions, ''), created_at
		FROM users WHERE id = ?
	`, id).Scan(&u.ID, &u.Username, &u.Password, &u.DisplayName, &u.Role, &u.Status, &u.Permissions, &u.CreatedAt)
	if err != nil {
		return nil, nil
	}
	return u, nil
}

func AddUser(u *models.User) error {
	u.ID = fmt.Sprintf("user_%d", timeNow().UnixNano())
	u.CreatedAt = timeNow().Format("2006-01-02 15:04:05")

	_, err := database.DB.Exec(`
		INSERT INTO users (id, username, password, display_name, role, status, permissions, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`, u.ID, u.Username, u.Password, u.DisplayName, u.Role, u.Status, u.Permissions, u.CreatedAt)
	return err
}

func UpdateUser(id string, u *models.User) error {
	oldUser, err := GetUserByID(id)
	if err != nil || oldUser == nil {
		return fmt.Errorf("用户不存在: %s", id)
	}

	// 如果密码为空，保留旧密码
	if u.Password == "" {
		u.Password = oldUser.Password
	}

	_, err = database.DB.Exec(`
		UPDATE users SET display_name=?, role=?, status=?, permissions=?, password=?
		WHERE id=?
	`, u.DisplayName, u.Role, u.Status, u.Permissions, u.Password, id)
	return err
}

func DeleteUser(id string) error {
	_, err := database.DB.Exec("DELETE FROM users WHERE id=?", id)
	return err
}

// InitDefaultAdmin 初始化默认管理员
func InitDefaultAdmin() error {
	admin, _ := GetUserByUsername("admin")
	if admin == nil {
		u := &models.User{
			Username:    "admin",
			Password:    "admin123",
			DisplayName: "管理员",
			Role:        "admin",
			Status:      "active",
			Permissions: "",
		}
		return AddUser(u)
	}
	return nil
}





