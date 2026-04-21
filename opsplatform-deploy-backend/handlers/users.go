package handlers

import (
	"net/http"
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
		JSONError(w, 50000, err.Error())
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
	if req.Username == "" || req.Password == "" {
		JSONError(w, 40001, "username 和 password 必填")
		return
	}
	if req.Role != "admin" {
		req.Role = "user"
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		JSONError(w, 50000, err.Error())
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
		JSONError(w, 50000, err.Error())
		return
	}
	id, _ := res.LastInsertId()
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
	if _, err := database.DB.Exec(`UPDATE users SET display_name=?, role=? WHERE id=?`,
		strings.TrimSpace(req.DisplayName), req.Role, id); err != nil {
		JSONError(w, 50000, err.Error())
		return
	}
	JSONSuccess(w, nil)
}

// PUT /api/users/{id}/toggle
func HandleToggleUser(w http.ResponseWriter, r *http.Request) {
	id := ParseID(mux.Vars(r)["id"])
	if _, err := database.DB.Exec(`UPDATE users SET status = 1 - status WHERE id=?`, id); err != nil {
		JSONError(w, 50000, err.Error())
		return
	}
	JSONSuccess(w, nil)
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
		JSONError(w, 50000, err.Error())
		return
	}
	if _, err := database.DB.Exec(`UPDATE users SET password_hash=? WHERE id=? AND auth_source='local'`, string(hash), id); err != nil {
		JSONError(w, 50000, err.Error())
		return
	}
	JSONSuccess(w, nil)
}

// DELETE /api/users/{id}
func HandleDeleteUser(w http.ResponseWriter, r *http.Request) {
	id := ParseID(mux.Vars(r)["id"])
	if _, err := database.DB.Exec(`DELETE FROM users WHERE id=?`, id); err != nil {
		JSONError(w, 50000, err.Error())
		return
	}
	database.DB.Exec(`DELETE FROM sessions WHERE user_id=?`, id)
	JSONSuccess(w, nil)
}
