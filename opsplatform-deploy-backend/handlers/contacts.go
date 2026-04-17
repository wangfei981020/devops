package handlers

import (
	"net/http"
	"strconv"

	"github.com/gorilla/mux"
	"opsplatform-deploy-backend/database"
	"opsplatform-deploy-backend/models"
)

func HandleListContacts(w http.ResponseWriter, r *http.Request) {
	rows, err := database.DB.Query(`SELECT id, name, lark_id, remark, status, created_at, updated_at FROM contacts ORDER BY name`)
	if err != nil {
		jsonError(w, 50000, err.Error())
		return
	}
	defer rows.Close()
	list := []models.Contact{}
	for rows.Next() {
		var c models.Contact
		rows.Scan(&c.ID, &c.Name, &c.LarkID, &c.Remark, &c.Status, &c.CreatedAt, &c.UpdatedAt)
		list = append(list, c)
	}
	jsonSuccess(w, list)
}

func HandleGetContact(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(mux.Vars(r)["id"], 10, 64)
	var c models.Contact
	err := database.DB.QueryRow(`SELECT id, name, lark_id, remark, status, created_at, updated_at FROM contacts WHERE id=?`, id).
		Scan(&c.ID, &c.Name, &c.LarkID, &c.Remark, &c.Status, &c.CreatedAt, &c.UpdatedAt)
	if err != nil {
		jsonError(w, 40400, "通知人不存在")
		return
	}
	jsonSuccess(w, c)
}

func HandleCreateContact(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name   string `json:"name"`
		LarkID string `json:"lark_id"`
		Remark string `json:"remark"`
	}
	if err := decodeJSON(r, &req); err != nil {
		jsonError(w, 40000, "无效的请求")
		return
	}
	if req.Name == "" || req.LarkID == "" {
		jsonError(w, 40001, "姓名和 Lark ID 不能为空")
		return
	}
	res, err := database.DB.Exec(`INSERT INTO contacts (name, lark_id, remark) VALUES (?,?,?)`,
		req.Name, req.LarkID, req.Remark)
	if err != nil {
		jsonError(w, 50000, "创建失败: "+err.Error())
		return
	}
	id, _ := res.LastInsertId()
	jsonSuccess(w, map[string]interface{}{"id": id})
}

func HandleUpdateContact(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(mux.Vars(r)["id"], 10, 64)
	var req struct {
		Name   string `json:"name"`
		LarkID string `json:"lark_id"`
		Remark string `json:"remark"`
		Status int    `json:"status"`
	}
	if err := decodeJSON(r, &req); err != nil {
		jsonError(w, 40000, "无效的请求")
		return
	}
	_, err := database.DB.Exec(`UPDATE contacts SET name=?, lark_id=?, remark=?, status=? WHERE id=?`,
		req.Name, req.LarkID, req.Remark, req.Status, id)
	if err != nil {
		jsonError(w, 50000, err.Error())
		return
	}
	jsonSuccess(w, nil)
}

func HandleDeleteContact(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(mux.Vars(r)["id"], 10, 64)
	_, err := database.DB.Exec(`DELETE FROM contacts WHERE id=?`, id)
	if err != nil {
		jsonError(w, 50000, err.Error())
		return
	}
	jsonSuccess(w, nil)
}
