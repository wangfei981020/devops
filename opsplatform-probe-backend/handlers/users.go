package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"unicode"

	"github.com/gorilla/mux"
	"golang.org/x/crypto/bcrypt"
	"opsplatform-probe-backend/database"
)

// bcryptCost is the work factor used for password hashing.
// 12 ≈ 250ms on modern CPU, recommended for production.
const bcryptCost = 12

// validatePassword enforces minimum complexity:
// length >= 12, contains 3 of {lower, upper, digit, symbol}
func validatePassword(pw string) error {
	if len(pw) < 12 {
		return fmt.Errorf("密码至少 12 个字符")
	}
	if len(pw) > 128 {
		return fmt.Errorf("密码过长 (最多 128 字符)")
	}
	var hasLower, hasUpper, hasDigit, hasSymbol bool
	for _, r := range pw {
		switch {
		case unicode.IsLower(r):
			hasLower = true
		case unicode.IsUpper(r):
			hasUpper = true
		case unicode.IsDigit(r):
			hasDigit = true
		case unicode.IsPunct(r) || unicode.IsSymbol(r):
			hasSymbol = true
		}
	}
	classes := 0
	if hasLower {
		classes++
	}
	if hasUpper {
		classes++
	}
	if hasDigit {
		classes++
	}
	if hasSymbol {
		classes++
	}
	if classes < 3 {
		return fmt.Errorf("密码至少需要包含 大写字母/小写字母/数字/符号 中的 3 类")
	}
	return nil
}

func HandleListUsers(w http.ResponseWriter, r *http.Request) {
	rows, err := database.DB.Query(`SELECT id, username, display_name, role, status, COALESCE(auth_source,'local'), created_at, updated_at
		FROM users ORDER BY id DESC`)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "查询失败")
		return
	}
	defer rows.Close()

	list := []map[string]interface{}{}
	for rows.Next() {
		var id, status int
		var username, displayName, role, authSource, createdAt, updatedAt string
		rows.Scan(&id, &username, &displayName, &role, &status, &authSource, &createdAt, &updatedAt)
		list = append(list, map[string]interface{}{
			"id":           id,
			"username":     username,
			"display_name": displayName,
			"role":         role,
			"status":       status,
			"auth_source":  authSource,
			"created_at":   createdAt,
			"updated_at":   updatedAt,
		})
	}
	jsonSuccess(w, list)
}

func HandleCreateUser(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Username    string `json:"username"`
		Password    string `json:"password"`
		DisplayName string `json:"display_name"`
		Role        string `json:"role"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, http.StatusBadRequest, "无效的请求")
		return
	}
	if req.Username == "" || req.Password == "" {
		jsonError(w, http.StatusBadRequest, "用户名和密码不能为空")
		return
	}
	if err := validatePassword(req.Password); err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	if req.Role == "" {
		req.Role = "user"
	}
	if req.DisplayName == "" {
		req.DisplayName = req.Username
	}

	var count int
	database.DB.QueryRow("SELECT COUNT(*) FROM users WHERE username = ? AND auth_source = 'local'", req.Username).Scan(&count)
	if count > 0 {
		jsonError(w, http.StatusBadRequest, "用户名已存在")
		return
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcryptCost)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "密码加密失败")
		return
	}

	result, err := database.DB.Exec(`INSERT INTO users (username, password_hash, display_name, role, auth_source, status)
		VALUES (?, ?, ?, ?, 'local', 1)`, req.Username, string(hash), req.DisplayName, req.Role)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "创建失败: "+err.Error())
		return
	}
	id, _ := result.LastInsertId()
	SaveAuditLog(r, "create_user", "user", req.Username, fmt.Sprintf("创建用户 ID=%d 角色=%s", id, req.Role))
	jsonSuccess(w, map[string]interface{}{"id": id, "message": "用户已创建"})
}

func HandleUpdateUser(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.Atoi(mux.Vars(r)["id"])
	var req struct {
		DisplayName string `json:"display_name"`
		Role        string `json:"role"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, http.StatusBadRequest, "无效的请求")
		return
	}
	_, err := database.DB.Exec("UPDATE users SET display_name = ?, role = ? WHERE id = ?",
		req.DisplayName, req.Role, id)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "更新失败")
		return
	}
	SaveAuditLog(r, "update_user", "user", fmt.Sprintf("ID=%d", id), fmt.Sprintf("更新用户 角色=%s", req.Role))
	jsonSuccess(w, nil)
}

func HandleResetPassword(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.Atoi(mux.Vars(r)["id"])
	var req struct {
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Password == "" {
		jsonError(w, http.StatusBadRequest, "密码不能为空")
		return
	}
	if err := validatePassword(req.Password); err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcryptCost)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "密码加密失败")
		return
	}
	database.DB.Exec("UPDATE users SET password_hash = ? WHERE id = ?", string(hash), id)
	// 失效该用户所有现有 session, 强制重新登录 (fix #10)
	database.DB.Exec("DELETE FROM sessions WHERE user_id = ?", id)
	SaveAuditLog(r, "reset_password", "user", fmt.Sprintf("ID=%d", id), "重置用户密码并使所有 session 失效")
	jsonSuccess(w, map[string]string{"message": "密码已重置, 该用户需重新登录"})
}

func HandleToggleUser(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.Atoi(mux.Vars(r)["id"])
	currentUserID := r.Context().Value(contextUserID).(int)
	if id == currentUserID {
		jsonError(w, http.StatusBadRequest, "不能禁用自己的账号")
		return
	}
	database.DB.Exec("UPDATE users SET status = IF(status=1, 0, 1) WHERE id = ?", id)
	var status int
	database.DB.QueryRow("SELECT status FROM users WHERE id = ?", id).Scan(&status)
	SaveAuditLog(r, "toggle_user", "user", fmt.Sprintf("ID=%d", id), fmt.Sprintf("用户状态变更为 %d", status))
	jsonSuccess(w, map[string]interface{}{"status": status})
}

func HandleDeleteUser(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.Atoi(mux.Vars(r)["id"])
	currentUserID := r.Context().Value(contextUserID).(int)
	if id == currentUserID {
		jsonError(w, http.StatusBadRequest, "不能删除自己的账号")
		return
	}
	var role string
	database.DB.QueryRow("SELECT role FROM users WHERE id = ?", id).Scan(&role)
	if role == "admin" {
		var adminCount int
		database.DB.QueryRow("SELECT COUNT(*) FROM users WHERE role = 'admin' AND status = 1").Scan(&adminCount)
		if adminCount <= 1 {
			jsonError(w, http.StatusBadRequest, "不能删除最后一个管理员")
			return
		}
	}
	database.DB.Exec("DELETE FROM sessions WHERE user_id = ?", id)
	database.DB.Exec("DELETE FROM users WHERE id = ?", id)
	SaveAuditLog(r, "delete_user", "user", fmt.Sprintf("ID=%d", id), "删除用户")
	jsonSuccess(w, nil)
}
