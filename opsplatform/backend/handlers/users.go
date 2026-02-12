package handlers

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"os"
	"strings"

	"opsplatform/database"
	"opsplatform/models"

	"github.com/gorilla/mux"
	"golang.org/x/crypto/bcrypt"
)

// hashPassword 使用 bcrypt 加密密码
func hashPassword(password string) (string, error) {
	bytes, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	return string(bytes), err
}

// checkPassword 验证密码是否正确
func checkPassword(hashedPassword, password string) bool {
	err := bcrypt.CompareHashAndPassword([]byte(hashedPassword), []byte(password))
	return err == nil
}

// LoginResponse 登录响应
type LoginResponse struct {
	RequireMFA      bool         `json:"require_mfa"`               // 是否需要 MFA 验证
	UserID          string       `json:"user_id,omitempty"`         // 需要 MFA 时返回用户 ID
	User            *models.User `json:"user,omitempty"`            // 登录成功时返回用户信息
	Token           string       `json:"token,omitempty"`           // JWT token（兼容旧客户端）
	NeedBinding     bool         `json:"need_binding,omitempty"`    // 是否需要绑定 MFA
	ExpiresAt       string       `json:"expires_at,omitempty"`      // 会话过期时间
	TimeoutMinutes  int          `json:"timeout_minutes,omitempty"` // 会话超时（分钟）
}

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

	// 获取客户端 IP
	ip := GetClientIP(r)
	
	// 检查 IP 白名单
	if !IsIPWhitelisted(ip) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"error":   "IP_BLOCKED",
			"message": "您的 IP 地址不在白名单中",
			"ip":      ip,
		})
		return
	}

	// 检查登录速率限制
	allowed, remaining := CheckLoginRateLimit(ip)
	if !allowed {
		http.Error(w, fmt.Sprintf("登录尝试过多，请在 %d 分钟后重试", int(remaining.Minutes())+1), http.StatusTooManyRequests)
		return
	}

	user, err := GetUserByUsername(req.Username)
	if err != nil || user == nil {
		RecordLoginAttempt(ip, false)
		http.Error(w, "用户名或密码错误", http.StatusUnauthorized)
		return
	}

	// 验证密码（仅支持 bcrypt 加密）
	if !checkPassword(user.Password, req.Password) {
		RecordLoginAttempt(ip, false)
		http.Error(w, "用户名或密码错误", http.StatusUnauthorized)
		return
	}

	// 记录登录成功
	RecordLoginAttempt(ip, true)

	if user.Status != "active" {
		http.Error(w, "用户已被禁用", http.StatusForbidden)
		return
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")

	// 计算是否已绑定 MFA
	user.MFABound = user.MFASecret != ""

	// 检查是否启用了 MFA 且已绑定
	if user.MFAEnabled && user.MFABound {
		// 需要进行 MFA 验证
		json.NewEncoder(w).Encode(LoginResponse{
			RequireMFA: true,
			UserID:     user.ID,
		})
		return
	}

	// 如果启用了 MFA 但未绑定，需要先绑定
	if user.MFAEnabled && !user.MFABound {
		user.Password = ""
		user.MFASecret = ""
		json.NewEncoder(w).Encode(LoginResponse{
			RequireMFA:  false,
			User:        user,
			NeedBinding: true, // 需要绑定 MFA
		})
		return
	}

	// 未启用 MFA，直接登录成功
	// 生成 JWT token
	token, expiresAt, err := GenerateToken(user.ID, user.Username, user.Role)
	if err != nil {
		http.Error(w, "生成认证令牌失败", http.StatusInternalServerError)
		return
	}

	// 设置 HttpOnly Cookie
	SetAuthCookie(w, token, expiresAt)

	AddAuditLogFromRequest(r, "login", "user:"+user.ID, user.Username, "", "", fmt.Sprintf("用户登录成功: %s (%s)", user.Username, user.DisplayName))

	user.Password = ""
	user.MFASecret = ""
	json.NewEncoder(w).Encode(LoginResponse{
		RequireMFA:     false,
		User:           user,
		Token:          token, // 兼容旧客户端
		ExpiresAt:      expiresAt.Format("2006-01-02T15:04:05Z07:00"),
		TimeoutMinutes: int(GetSessionTimeout().Minutes()),
	})
}

// HandleGetUsers 获取所有用户
func HandleGetUsers(w http.ResponseWriter, r *http.Request) {
	users, err := GetAllUsers()
	if err != nil {
		SafeError(w, "获取用户列表失败", http.StatusInternalServerError, err)
		return
	}

	// 移除敏感信息，计算 MFABound
	for _, u := range users {
		u.MFABound = u.MFASecret != "" // 有密钥即为已绑定
		u.Password = ""
		u.MFASecret = "" // 不返回 MFA 密钥
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
		SafeError(w, "创建用户失败", http.StatusInternalServerError, err)
		return
	}

	// 记录审计日志
	changes := fmt.Sprintf("创建用户: %s (%s), 角色=%s", req.User.Username, req.User.DisplayName, req.User.Role)
	AddAuditLogFromRequest(r, "create", "user:"+req.User.ID, req.CreatedBy, "", "", changes)

	req.User.Password = ""
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(req.User)
}

// HandleUpdateUser 更新用户
func HandleUpdateUser(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id := vars["id"]

	// 获取旧用户信息用于审计
	oldUser, _ := GetUserByID(id)

	var user models.User
	if err := json.NewDecoder(r.Body).Decode(&user); err != nil {
		http.Error(w, "无效的请求数据", http.StatusBadRequest)
		return
	}

	// 获取操作者（从请求头或body）
	operator := r.Header.Get("X-Operator")
	if operator == "" {
		operator = "system"
	}

	if err := UpdateUser(id, &user); err != nil {
		SafeError(w, "更新用户失败", http.StatusNotFound, err)
		return
	}

	// 记录审计日志
	var changes []string
	if oldUser != nil {
		if oldUser.DisplayName != user.DisplayName && user.DisplayName != "" {
			changes = append(changes, fmt.Sprintf("显示名称: %s → %s", oldUser.DisplayName, user.DisplayName))
		}
		if oldUser.Role != user.Role && user.Role != "" {
			changes = append(changes, fmt.Sprintf("角色: %s → %s", oldUser.Role, user.Role))
		}
		if oldUser.Status != user.Status && user.Status != "" {
			changes = append(changes, fmt.Sprintf("状态: %s → %s", oldUser.Status, user.Status))
		}
		if oldUser.Permissions != user.Permissions {
			oldPerms := oldUser.Permissions
			newPerms := user.Permissions
			if oldPerms == "" {
				oldPerms = "无"
			}
			if newPerms == "" {
				newPerms = "无"
			}
			changes = append(changes, fmt.Sprintf("权限: %s → %s", oldPerms, newPerms))
		}
		if oldUser.MFAEnabled != user.MFAEnabled {
			if user.MFAEnabled {
				changes = append(changes, "启用 MFA")
			} else {
				changes = append(changes, "禁用 MFA")
			}
		}
	}
	if len(changes) > 0 {
		AddAuditLogFromRequest(r, "update", "user:"+id, operator, "", "", fmt.Sprintf("更新用户 %s: %s", user.Username, strings.Join(changes, ", ")))
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

	// 获取操作者
	operator := r.Header.Get("X-Operator")
	if operator == "" {
		operator = "system"
	}

	if err := DeleteUser(id); err != nil {
		SafeError(w, "删除用户失败", http.StatusNotFound, err)
		return
	}

	// 记录审计日志
	changes := fmt.Sprintf("删除用户: %s (%s)", user.Username, user.DisplayName)
	AddAuditLogFromRequest(r, "delete", "user:"+id, operator, "", "", changes)

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	json.NewEncoder(w).Encode(map[string]string{"message": "删除成功"})
}

// 数据库操作函数
func GetAllUsers() ([]*models.User, error) {
	rows, err := database.DB.Query(`
		SELECT id, username, password, display_name, COALESCE(phone, ''), COALESCE(email, ''), COALESCE(description, ''),
		       role, status, COALESCE(permissions, ''), COALESCE(mfa_enabled, 0), COALESCE(mfa_secret, ''), created_at
		FROM users ORDER BY created_at DESC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var users []*models.User
	for rows.Next() {
		u := &models.User{}
		err := rows.Scan(&u.ID, &u.Username, &u.Password, &u.DisplayName, &u.Phone, &u.Email, &u.Description, &u.Role, &u.Status, &u.Permissions, &u.MFAEnabled, &u.MFASecret, &u.CreatedAt)
		if err != nil {
			return nil, err
		}
		// 计算 MFABound：有 MFASecret 即表示已绑定
		u.MFABound = u.MFASecret != ""
		users = append(users, u)
	}
	return users, nil
}

func GetUserByUsername(username string) (*models.User, error) {
	u := &models.User{}
	err := database.DB.QueryRow(`
		SELECT id, username, password, display_name, COALESCE(phone, ''), COALESCE(email, ''), COALESCE(description, ''),
		       role, status, COALESCE(permissions, ''), COALESCE(mfa_enabled, 0), COALESCE(mfa_secret, ''), created_at
		FROM users WHERE username = ?
	`, username).Scan(&u.ID, &u.Username, &u.Password, &u.DisplayName, &u.Phone, &u.Email, &u.Description, &u.Role, &u.Status, &u.Permissions, &u.MFAEnabled, &u.MFASecret, &u.CreatedAt)
	if err != nil {
		return nil, nil
	}
	// 计算 MFABound
	u.MFABound = u.MFASecret != ""
	return u, nil
}

func GetUserByID(id string) (*models.User, error) {
	u := &models.User{}
	err := database.DB.QueryRow(`
		SELECT id, username, password, display_name, COALESCE(phone, ''), COALESCE(email, ''), COALESCE(description, ''),
		       role, status, COALESCE(permissions, ''), COALESCE(mfa_enabled, 0), COALESCE(mfa_secret, ''), created_at
		FROM users WHERE id = ?
	`, id).Scan(&u.ID, &u.Username, &u.Password, &u.DisplayName, &u.Phone, &u.Email, &u.Description, &u.Role, &u.Status, &u.Permissions, &u.MFAEnabled, &u.MFASecret, &u.CreatedAt)
	if err != nil {
		return nil, nil
	}
	// 计算 MFABound
	u.MFABound = u.MFASecret != ""
	return u, nil
}

func AddUser(u *models.User) error {
	u.ID = fmt.Sprintf("user_%d", timeNow().UnixNano())
	u.CreatedAt = timeNow().Format("2006-01-02 15:04:05")

	// 设置默认值
	if u.Status == "" {
		u.Status = "active"
	}
	if u.Role == "" {
		u.Role = "user"
	}

	// 加密密码
	hashedPassword, err := hashPassword(u.Password)
	if err != nil {
		return fmt.Errorf("密码加密失败: %v", err)
	}

	_, err = database.DB.Exec(`
		INSERT INTO users (id, username, password, display_name, phone, email, description, role, status, permissions, mfa_enabled, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, u.ID, u.Username, hashedPassword, u.DisplayName, u.Phone, u.Email, u.Description, u.Role, u.Status, u.Permissions, u.MFAEnabled, u.CreatedAt)
	return err
}

func UpdateUser(id string, u *models.User) error {
	oldUser, err := GetUserByID(id)
	if err != nil || oldUser == nil {
		return fmt.Errorf("用户不存在: %s", id)
	}

	// 确保 status 和 role 不为空（保留原值或使用默认值）
	if u.Status == "" {
		u.Status = oldUser.Status
		if u.Status == "" {
			u.Status = "active"
		}
	}
	if u.Role == "" {
		u.Role = oldUser.Role
		if u.Role == "" {
			u.Role = "user"
		}
	}

	// 如果密码为空，保留旧密码；否则加密新密码
	passwordToSave := oldUser.Password
	if u.Password != "" {
		hashedPassword, err := hashPassword(u.Password)
		if err != nil {
			return fmt.Errorf("密码加密失败: %v", err)
		}
		passwordToSave = hashedPassword
	}

	// MFA 逻辑：保留原有 mfa_secret，只有当明确从开启变为关闭时才清除
	mfaSecret := oldUser.MFASecret
	mfaEnabled := u.MFAEnabled
	
	// 如果请求中没有明确设置 mfa_enabled，保留原值
	// 注意：Go 中 bool 的零值是 false，但我们应该保留原值
	// 只在管理员明确从 enabled=true 改为 enabled=false 时清空 secret
	if oldUser.MFAEnabled && !u.MFAEnabled {
		// 明确关闭 MFA，清空密钥
		mfaSecret = ""
	} else if !oldUser.MFAEnabled && !u.MFAEnabled {
		// 原本就是关闭的，保持 mfa_enabled 和 mfa_secret 不变
		mfaEnabled = oldUser.MFAEnabled
		mfaSecret = oldUser.MFASecret
	}

	_, err = database.DB.Exec(`
		UPDATE users SET display_name=?, phone=?, email=?, description=?, role=?, status=?, permissions=?, password=?, mfa_enabled=?, mfa_secret=?
		WHERE id=?
	`, u.DisplayName, u.Phone, u.Email, u.Description, u.Role, u.Status, u.Permissions, passwordToSave, mfaEnabled, mfaSecret, id)
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
		// 从环境变量获取初始密码，否则生成随机密码
		password := os.Getenv("ADMIN_PASSWORD")
		if password == "" {
			// 生成随机密码
			password = generateSecurePassword(16)
			fmt.Printf("\n╔════════════════════════════════════════════════════════════════╗\n")
			fmt.Printf("║  ⚠️  首次启动 - 默认管理员账号已创建                            ║\n")
			fmt.Printf("╠════════════════════════════════════════════════════════════════╣\n")
			fmt.Printf("║  用户名: admin                                                  ║\n")
			fmt.Printf("║  密码:   %-54s ║\n", password)
			fmt.Printf("║  ⚠️  请立即登录并修改密码！                                     ║\n")
			fmt.Printf("╚════════════════════════════════════════════════════════════════╝\n\n")
		}

		u := &models.User{
			Username:    "admin",
			Password:    password,
			DisplayName: "管理员",
			Role:        "admin",
			Status:      "active",
			Permissions: "",
		}
		return AddUser(u)
	}
	return nil
}

// generateSecurePassword 生成安全随机密码
func generateSecurePassword(length int) string {
	const charset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789!@#$%^&*"
	b := make([]byte, length)
	for i := range b {
		n, _ := rand.Int(rand.Reader, big.NewInt(int64(len(charset))))
		b[i] = charset[n.Int64()]
	}
	return string(b)
}





