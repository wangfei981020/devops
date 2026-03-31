package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"

	"github.com/gorilla/mux"
	"opsplatform-alert-backend/database"
	"opsplatform-alert-backend/models"
)

func HandleListContacts(w http.ResponseWriter, r *http.Request) {
	rows, err := database.DB.Query("SELECT id, name, lark_id, phone, email, description, status, created_at, updated_at FROM alert_contacts ORDER BY name")
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "查询失败")
		return
	}
	defer rows.Close()

	var list []map[string]interface{}
	for rows.Next() {
		var id, status int
		var name, larkID, phone, email, desc, createdAt, updatedAt string
		rows.Scan(&id, &name, &larkID, &phone, &email, &desc, &status, &createdAt, &updatedAt)
		list = append(list, map[string]interface{}{
			"id": id, "name": name, "lark_id": larkID, "phone": phone,
			"email": email, "description": desc, "status": status,
			"created_at": createdAt, "updated_at": updatedAt,
		})
	}
	if list == nil {
		list = []map[string]interface{}{}
	}
	jsonSuccess(w, list)
}

func HandleCreateContact(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name        string `json:"name"`
		LarkID      string `json:"lark_id"`
		Phone       string `json:"phone"`
		Email       string `json:"email"`
		Description string `json:"description"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, http.StatusBadRequest, "无效的请求")
		return
	}
	if req.Name == "" || req.LarkID == "" {
		jsonError(w, http.StatusBadRequest, "姓名和 Lark ID 不能为空")
		return
	}

	result, err := database.DB.Exec(`INSERT INTO alert_contacts (name, lark_id, phone, email, description) VALUES (?, ?, ?, ?, ?)`,
		req.Name, req.LarkID, req.Phone, req.Email, req.Description)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "创建失败: "+err.Error())
		return
	}
	id, _ := result.LastInsertId()
	SaveAuditLog(r, "create_contact", "contact", req.Name, fmt.Sprintf("创建通知人 ID=%d", id))
	jsonSuccess(w, map[string]interface{}{"id": id})
}

func HandleUpdateContact(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.Atoi(mux.Vars(r)["id"])
	var req struct {
		Name        string `json:"name"`
		LarkID      string `json:"lark_id"`
		Phone       string `json:"phone"`
		Email       string `json:"email"`
		Description string `json:"description"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, http.StatusBadRequest, "无效的请求")
		return
	}
	database.DB.Exec("UPDATE alert_contacts SET name=?, lark_id=?, phone=?, email=?, description=? WHERE id=?",
		req.Name, req.LarkID, req.Phone, req.Email, req.Description, id)
	SaveAuditLog(r, "update_contact", "contact", req.Name, fmt.Sprintf("更新通知人 ID=%d", id))
	jsonSuccess(w, nil)
}

func HandleBatchCreateContacts(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Items []struct {
			Name   string `json:"name"`
			LarkID string `json:"lark_id"`
		} `json:"items"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, http.StatusBadRequest, "无效的请求")
		return
	}
	if len(req.Items) == 0 {
		jsonError(w, http.StatusBadRequest, "列表不能为空")
		return
	}

	created := 0
	skipped := 0
	for _, item := range req.Items {
		if item.Name == "" || item.LarkID == "" {
			skipped++
			continue
		}
		// Check if name already exists
		var exists int
		database.DB.QueryRow("SELECT COUNT(*) FROM alert_contacts WHERE name = ?", item.Name).Scan(&exists)
		if exists > 0 {
			// Update lark_id if already exists
			database.DB.Exec("UPDATE alert_contacts SET lark_id = ? WHERE name = ?", item.LarkID, item.Name)
			skipped++
			continue
		}
		_, err := database.DB.Exec("INSERT INTO alert_contacts (name, lark_id) VALUES (?, ?)", item.Name, item.LarkID)
		if err != nil {
			skipped++
			continue
		}
		created++
	}
	SaveAuditLog(r, "batch_create_contacts", "contact", fmt.Sprintf("共%d条", len(req.Items)), fmt.Sprintf("批量创建通知人 新增=%d 跳过=%d", created, skipped))
	jsonSuccess(w, map[string]interface{}{
		"created": created,
		"skipped": skipped,
		"total":   len(req.Items),
	})
}

func HandleDeleteContact(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.Atoi(mux.Vars(r)["id"])
	database.DB.Exec("DELETE FROM alert_contacts WHERE id = ?", id)
	SaveAuditLog(r, "delete_contact", "contact", fmt.Sprintf("ID=%d", id), "删除通知人")
	jsonSuccess(w, nil)
}

// ResolveAtUsers takes a JSON array of names like ["Bruce","Cesar"]
// and returns []AtUser with lark_id filled from alert_contacts table
func ResolveAtUsers(atUsersJSON string) []models.AtUser {
	if atUsersJSON == "" {
		return nil
	}

	// Try parsing as name array first: ["Bruce","Cesar"]
	var names []string
	if err := json.Unmarshal([]byte(atUsersJSON), &names); err == nil && len(names) > 0 {
		var result []models.AtUser
		for _, name := range names {
			var larkID string
			err := database.DB.QueryRow("SELECT lark_id FROM alert_contacts WHERE name = ? AND status = 1", name).Scan(&larkID)
			if err == nil && larkID != "" {
				result = append(result, models.AtUser{Name: name, UserID: larkID})
			}
		}
		return result
	}

	// Fallback: try parsing as old format [{"name":"Bruce","user_id":"ou_xxx"}]
	var users []models.AtUser
	if err := json.Unmarshal([]byte(atUsersJSON), &users); err == nil {
		return users
	}

	return nil
}
