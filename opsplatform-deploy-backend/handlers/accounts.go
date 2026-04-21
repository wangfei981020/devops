package handlers

import (
	"database/sql"
	"net/http"
	"strings"

	"github.com/gorilla/mux"
	"opsplatform-deploy-backend/database"
	"opsplatform-deploy-backend/models"
)

// GET /api/accounts
func HandleListAccounts(w http.ResponseWriter, r *http.Request) {
	rows, err := database.DB.Query(`SELECT id, username, display_name, lark_id, email, remark, created_at, updated_at
		FROM account ORDER BY username`)
	if err != nil {
		JSONError(w, 50000, err.Error())
		return
	}
	defer rows.Close()
	list := []models.Account{}
	for rows.Next() {
		var a models.Account
		_ = rows.Scan(&a.ID, &a.Username, &a.DisplayName, &a.LarkID, &a.Email, &a.Remark, &a.CreatedAt, &a.UpdatedAt)
		list = append(list, a)
	}
	JSONSuccess(w, list)
}

type accountReq struct {
	Username    string `json:"username"`
	DisplayName string `json:"display_name"`
	LarkID      string `json:"lark_id"`
	Email       string `json:"email"`
	Remark      string `json:"remark"`
}

// POST /api/accounts
func HandleCreateAccount(w http.ResponseWriter, r *http.Request) {
	var req accountReq
	if !DecodeJSON(w, r, &req) {
		return
	}
	req.Username = strings.TrimSpace(req.Username)
	if req.Username == "" {
		JSONError(w, 40001, "username 必填")
		return
	}
	res, err := database.DB.Exec(`INSERT INTO account (username, display_name, lark_id, email, remark)
		VALUES (?, ?, ?, ?, ?)`,
		req.Username, strings.TrimSpace(req.DisplayName), strings.TrimSpace(req.LarkID),
		strings.TrimSpace(req.Email), strings.TrimSpace(req.Remark))
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

// PUT /api/accounts/{id}
func HandleUpdateAccount(w http.ResponseWriter, r *http.Request) {
	id := ParseID(mux.Vars(r)["id"])
	var req accountReq
	if !DecodeJSON(w, r, &req) {
		return
	}
	_, err := database.DB.Exec(`UPDATE account SET display_name=?, lark_id=?, email=?, remark=? WHERE id=?`,
		strings.TrimSpace(req.DisplayName), strings.TrimSpace(req.LarkID),
		strings.TrimSpace(req.Email), strings.TrimSpace(req.Remark), id)
	if err != nil {
		JSONError(w, 50000, err.Error())
		return
	}
	JSONSuccess(w, nil)
}

// DELETE /api/accounts/{id}
func HandleDeleteAccount(w http.ResponseWriter, r *http.Request) {
	id := ParseID(mux.Vars(r)["id"])
	if _, err := database.DB.Exec(`DELETE FROM account WHERE id=?`, id); err != nil {
		JSONError(w, 50000, err.Error())
		return
	}
	JSONSuccess(w, nil)
}

// LookupAccountLarkID 按 username 查 lark_id（供 Lark 艾特用）
func LookupAccountLarkID(username string) string {
	if username == "" || username == "system" {
		return ""
	}
	var larkID string
	err := database.DB.QueryRow(`SELECT lark_id FROM account WHERE username=?`, username).Scan(&larkID)
	if err == sql.ErrNoRows {
		return ""
	}
	return larkID
}
