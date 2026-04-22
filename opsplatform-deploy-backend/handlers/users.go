package handlers

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/gorilla/mux"
	"golang.org/x/crypto/bcrypt"
	"opsplatform-deploy-backend/database"
)

var _ = jwt.RegisteredClaims{}
var _ = time.Now

type userDTO struct {
	ID          int       `json:"id"`
	Username    string    `json:"username"`
	DisplayName string    `json:"display_name"`
	Role        string    `json:"role"`
	AuthSource  string    `json:"auth_source"`
	Status      int       `json:"status"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// GET /api/users
func HandleListUsers(w http.ResponseWriter, r *http.Request) {
	rows, err := database.DB.Query(`SELECT id, username, IFNULL(display_name,''), role, IFNULL(auth_source,'local'), status, created_at, updated_at
		FROM users ORDER BY auth_source, username`)
	if err != nil {
		InternalErr(w, r, err)
		return
	}
	defer rows.Close()
	list := []userDTO{}
	for rows.Next() {
		var u userDTO
		_ = rows.Scan(&u.ID, &u.Username, &u.DisplayName, &u.Role, &u.AuthSource, &u.Status, &u.CreatedAt, &u.UpdatedAt)
		list = append(list, u)
	}
	JSONSuccess(w, list)
}

type createUserReq struct {
	Username    string `json:"username"`
	Password    string `json:"password"`
	DisplayName string `json:"display_name"`
	Role        string `json:"role"`
}

// POST /api/users  (admin only; 创建本地账号)
func HandleCreateUser(w http.ResponseWriter, r *http.Request) {
	var req createUserReq
	if !DecodeJSON(w, r, &req) {
		return
	}
	req.Username = strings.TrimSpace(req.Username)
	if err := ValidateUsername(req.Username); err != nil {
		JSONError(w, 40001, "username: "+err.Error())
		return
	}
	if len(req.Password) < 6 {
		JSONError(w, 40001, "密码至少 6 位")
		return
	}
	if req.Role != "admin" {
		req.Role = "user"
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		InternalErr(w, r, err)
		return
	}
	res, err := database.DB.Exec(
		`INSERT INTO users (username, password_hash, display_name, role, auth_source, status)
		 VALUES (?, ?, ?, ?, 'local', 1)`,
		req.Username, string(hash), strings.TrimSpace(req.DisplayName), req.Role)
	if err != nil {
		if strings.Contains(err.Error(), "Duplicate") {
			JSONError(w, 40900, "username 已存在")
			return
		}
		InternalErr(w, r, err)
		return
	}
	id, _ := res.LastInsertId()
	Audit(r, "user.create", "user", req.Username, map[string]interface{}{"role": req.Role})
	JSONSuccess(w, map[string]interface{}{"id": id})
}

type updateUserReq struct {
	DisplayName string `json:"display_name"`
	Role        string `json:"role"`
}

// PUT /api/users/{id}
func HandleUpdateUser(w http.ResponseWriter, r *http.Request) {
	id := ParseID(mux.Vars(r)["id"])
	var req updateUserReq
	if !DecodeJSON(w, r, &req) {
		return
	}
	if req.Role != "admin" {
		req.Role = "user"
	}
	// 降级为 user 前，检查是否最后一个启用中的 admin
	if req.Role != "admin" {
		if ok, err := canDemoteOrDisableAdmin(id); err != nil {
			InternalErr(w, r, err)
			return
		} else if !ok {
			JSONError(w, 40300, "不能降级最后一个启用中的管理员")
			return
		}
	}
	if _, err := database.DB.Exec(`UPDATE users SET display_name=?, role=? WHERE id=?`,
		strings.TrimSpace(req.DisplayName), req.Role, id); err != nil {
		InternalErr(w, r, err)
		return
	}
	Audit(r, "user.update", "user", strconv.FormatInt(id, 10), map[string]interface{}{"role": req.Role})
	JSONSuccess(w, nil)
}

// PUT /api/users/{id}/toggle
func HandleToggleUser(w http.ResponseWriter, r *http.Request) {
	id := ParseID(mux.Vars(r)["id"])
	// 如果当前是启用中的 admin，要禁用前检查是否最后一个
	var role string
	var status int
	if err := database.DB.QueryRow(`SELECT role, status FROM users WHERE id=?`, id).Scan(&role, &status); err != nil {
		InternalErr(w, r, err)
		return
	}
	if role == "admin" && status == 1 {
		if ok, err := canDemoteOrDisableAdmin(id); err != nil {
			InternalErr(w, r, err)
			return
		} else if !ok {
			JSONError(w, 40300, "不能禁用最后一个启用中的管理员")
			return
		}
	}
	if _, err := database.DB.Exec(`UPDATE users SET status = 1 - status WHERE id=?`, id); err != nil {
		InternalErr(w, r, err)
		return
	}
	// 禁用用户时顺便清掉其 session
	if status == 1 {
		database.DB.Exec(`DELETE FROM sessions WHERE user_id=?`, id)
	}
	newStatus := 1 - status
	Audit(r, "user.toggle", "user", strconv.FormatInt(id, 10), map[string]interface{}{"status": newStatus})
	JSONSuccess(w, nil)
}

// canDemoteOrDisableAdmin 返回 true 表示允许降级/禁用 targetID 这个 admin
// 条件：除了 targetID 之外，至少还有一个启用中的 admin
func canDemoteOrDisableAdmin(targetID int64) (bool, error) {
	var cnt int
	err := database.DB.QueryRow(
		`SELECT COUNT(*) FROM users WHERE role='admin' AND status=1 AND id<>?`, targetID).Scan(&cnt)
	if err != nil {
		return false, err
	}
	return cnt > 0, nil
}

type resetPwdReq struct {
	Password string `json:"password"`
}

// POST /api/users/{id}/reset-password
func HandleResetPassword(w http.ResponseWriter, r *http.Request) {
	id := ParseID(mux.Vars(r)["id"])
	var req resetPwdReq
	if !DecodeJSON(w, r, &req) {
		return
	}
	if req.Password == "" {
		JSONError(w, 40001, "password 必填")
		return
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		InternalErr(w, r, err)
		return
	}
	if _, err := database.DB.Exec(`UPDATE users SET password_hash=? WHERE id=? AND auth_source='local'`, string(hash), id); err != nil {
		InternalErr(w, r, err)
		return
	}
	// 重置密码后踢掉目标用户的所有活跃 session
	database.DB.Exec(`DELETE FROM sessions WHERE user_id=?`, id)
	Audit(r, "user.reset_password", "user", strconv.FormatInt(id, 10), nil)
	JSONSuccess(w, nil)
}

// DELETE /api/users/{id}
func HandleDeleteUser(w http.ResponseWriter, r *http.Request) {
	id := ParseID(mux.Vars(r)["id"])
	// 删除前检查是否最后一个启用中的 admin
	var role string
	var status int
	if err := database.DB.QueryRow(`SELECT role, status FROM users WHERE id=?`, id).Scan(&role, &status); err == nil {
		if role == "admin" && status == 1 {
			if ok, err := canDemoteOrDisableAdmin(id); err != nil {
				InternalErr(w, r, err)
				return
			} else if !ok {
				JSONError(w, 40300, "不能删除最后一个启用中的管理员")
				return
			}
		}
	}
	if _, err := database.DB.Exec(`DELETE FROM users WHERE id=?`, id); err != nil {
		InternalErr(w, r, err)
		return
	}
	database.DB.Exec(`DELETE FROM sessions WHERE user_id=?`, id)
	Audit(r, "user.delete", "user", strconv.FormatInt(id, 10), nil)
	JSONSuccess(w, nil)
}
