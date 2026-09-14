package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"

	"github.com/gorilla/mux"
	"opsplatform-alert-backend/database"
)

func HandleListContacts(w http.ResponseWriter, r *http.Request) {
	rows, err := database.DB.Query(`SELECT id, name, lark_id, COALESCE(telegram_id,''), phone, email,
		description, status, created_at, updated_at FROM alert_contacts ORDER BY name`)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "查询失败")
		return
	}
	defer rows.Close()

	var list []map[string]interface{}
	for rows.Next() {
		var id, status int
		var name, larkID, telegramID, phone, email, desc, createdAt, updatedAt string
		rows.Scan(&id, &name, &larkID, &telegramID, &phone, &email, &desc, &status, &createdAt, &updatedAt)
		list = append(list, map[string]interface{}{
			"id": id, "name": name, "lark_id": larkID, "telegram_id": telegramID,
			"phone": phone, "email": email, "description": desc, "status": status,
			"created_at": createdAt, "updated_at": updatedAt,
		})
	}
	if err := rows.Err(); err != nil {
		jsonError(w, http.StatusInternalServerError, "查询失败")
		return
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
		TelegramID  string `json:"telegram_id"`
		Phone       string `json:"phone"`
		Email       string `json:"email"`
		Description string `json:"description"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, http.StatusBadRequest, "无效的请求")
		return
	}
	// A contact is useful as long as it can be @-mentioned on at least one platform.
	if req.Name == "" || (req.LarkID == "" && req.TelegramID == "") {
		jsonError(w, http.StatusBadRequest, "姓名必填，Lark ID 与 Telegram ID 至少填一个")
		return
	}

	result, err := database.DB.Exec(
		`INSERT INTO alert_contacts (name, lark_id, telegram_id, phone, email, description) VALUES (?, ?, ?, ?, ?, ?)`,
		req.Name, req.LarkID, req.TelegramID, req.Phone, req.Email, req.Description)
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
		TelegramID  string `json:"telegram_id"`
		Phone       string `json:"phone"`
		Email       string `json:"email"`
		Description string `json:"description"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, http.StatusBadRequest, "无效的请求")
		return
	}
	// A contact is useful as long as it can be @-mentioned on at least one platform.
	if req.Name == "" || (req.LarkID == "" && req.TelegramID == "") {
		jsonError(w, http.StatusBadRequest, "姓名必填，Lark ID 与 Telegram ID 至少填一个")
		return
	}
	database.DB.Exec("UPDATE alert_contacts SET name=?, lark_id=?, telegram_id=?, phone=?, email=?, description=? WHERE id=?",
		req.Name, req.LarkID, req.TelegramID, req.Phone, req.Email, req.Description, id)
	SaveAuditLog(r, "update_contact", "contact", req.Name, fmt.Sprintf("更新通知人 ID=%d", id))
	jsonSuccess(w, nil)
}

func HandleBatchCreateContacts(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Items []struct {
			Name       string `json:"name"`
			LarkID     string `json:"lark_id"`
			TelegramID string `json:"telegram_id"`
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
		if item.Name == "" || (item.LarkID == "" && item.TelegramID == "") {
			skipped++
			continue
		}
		var exists int
		database.DB.QueryRow("SELECT COUNT(*) FROM alert_contacts WHERE name = ?", item.Name).Scan(&exists)
		if exists > 0 {
			// Only overwrite the ids the caller actually supplied.
			database.DB.Exec(`UPDATE alert_contacts SET
				lark_id = IF(? = '', lark_id, ?),
				telegram_id = IF(? = '', telegram_id, ?)
				WHERE name = ?`,
				item.LarkID, item.LarkID, item.TelegramID, item.TelegramID, item.Name)
			skipped++
			continue
		}
		_, err := database.DB.Exec("INSERT INTO alert_contacts (name, lark_id, telegram_id) VALUES (?, ?, ?)",
			item.Name, item.LarkID, item.TelegramID)
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

// Resolving contact names to @-mentions lives in alert.resolveAtUsers, next to
// the code that sends the message. A second copy stood here, exported and never
// called by anything — two implementations of the same contact lookup, free to
// drift apart with nothing to notice.
