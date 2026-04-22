package handlers

import (
	"database/sql"
	"net/http"
	"strconv"
	"strings"

	"github.com/gorilla/mux"
	"opsplatform-deploy-backend/database"
	"opsplatform-deploy-backend/models"
)

// GET /api/contacts
func HandleListContacts(w http.ResponseWriter, r *http.Request) {
	rows, err := database.DB.Query(`SELECT id, name, lark_id, remark, created_at, updated_at FROM contact ORDER BY name`)
	if err != nil {
		InternalErr(w, r, err)
		return
	}
	defer rows.Close()
	list := []models.Contact{}
	for rows.Next() {
		var c models.Contact
		_ = rows.Scan(&c.ID, &c.Name, &c.LarkID, &c.Remark, &c.CreatedAt, &c.UpdatedAt)
		list = append(list, c)
	}
	JSONSuccess(w, list)
}

type contactReq struct {
	Name   string `json:"name"`
	LarkID string `json:"lark_id"`
	Remark string `json:"remark"`
}

// POST /api/contacts
func HandleCreateContact(w http.ResponseWriter, r *http.Request) {
	var req contactReq
	if !DecodeJSON(w, r, &req) {
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if err := ValidateLabel(req.Name); err != nil {
		JSONError(w, 40001, "name: "+err.Error())
		return
	}
	res, err := database.DB.Exec(`INSERT INTO contact (name, lark_id, remark) VALUES (?, ?, ?)`,
		req.Name, strings.TrimSpace(req.LarkID), strings.TrimSpace(req.Remark))
	if err != nil {
		if strings.Contains(err.Error(), "Duplicate") {
			JSONError(w, 40900, "name 已存在")
			return
		}
		InternalErr(w, r, err)
		return
	}
	id, _ := res.LastInsertId()
	Audit(r, "contact.create", "contact", req.Name, nil)
	JSONSuccess(w, map[string]interface{}{"id": id})
}

// PUT /api/contacts/{id}
func HandleUpdateContact(w http.ResponseWriter, r *http.Request) {
	id := ParseID(mux.Vars(r)["id"])
	var req contactReq
	if !DecodeJSON(w, r, &req) {
		return
	}
	_, err := database.DB.Exec(`UPDATE contact SET lark_id=?, remark=? WHERE id=?`,
		strings.TrimSpace(req.LarkID), strings.TrimSpace(req.Remark), id)
	if err != nil {
		InternalErr(w, r, err)
		return
	}
	Audit(r, "contact.update", "contact", strconv.FormatInt(id, 10), nil)
	JSONSuccess(w, nil)
}

// DELETE /api/contacts/{id}
func HandleDeleteContact(w http.ResponseWriter, r *http.Request) {
	id := ParseID(mux.Vars(r)["id"])
	if _, err := database.DB.Exec(`DELETE FROM contact WHERE id=?`, id); err != nil {
		InternalErr(w, r, err)
		return
	}
	Audit(r, "contact.delete", "contact", strconv.FormatInt(id, 10), nil)
	JSONSuccess(w, nil)
}

// LookupContactLarkID 按 operator 的 username 精确匹配 contact.name
// 不再用 display_name 兜底（避免冒名艾特）
func LookupContactLarkID(operator string) string {
	if operator == "" || operator == "system" {
		return ""
	}
	var larkID string
	err := database.DB.QueryRow(`SELECT lark_id FROM contact WHERE name=?`, operator).Scan(&larkID)
	if err == sql.ErrNoRows {
		return ""
	}
	return larkID
}
