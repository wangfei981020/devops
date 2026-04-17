package handlers

import (
	"net/http"
	"strconv"

	"github.com/gorilla/mux"
	"opsplatform-deploy-backend/database"
	"opsplatform-deploy-backend/models"
)

func HandleListLarkConfigs(w http.ResponseWriter, r *http.Request) {
	rows, err := database.DB.Query(`SELECT id, name, webhook_url, secret, lark_type, description, status, created_at, updated_at FROM lark_configs ORDER BY id`)
	if err != nil {
		jsonError(w, 50000, err.Error())
		return
	}
	defer rows.Close()
	list := []models.LarkConfig{}
	for rows.Next() {
		var c models.LarkConfig
		rows.Scan(&c.ID, &c.Name, &c.WebhookURL, &c.Secret, &c.LarkType, &c.Description, &c.Status, &c.CreatedAt, &c.UpdatedAt)
		list = append(list, c)
	}
	jsonSuccess(w, list)
}

func HandleCreateLarkConfig(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name        string `json:"name"`
		WebhookURL  string `json:"webhook_url"`
		Secret      string `json:"secret"`
		LarkType    string `json:"lark_type"`
		Description string `json:"description"`
	}
	if err := decodeJSON(r, &req); err != nil {
		jsonError(w, 40000, "无效的请求")
		return
	}
	if req.Name == "" || req.WebhookURL == "" {
		jsonError(w, 40001, "名称和 webhook URL 不能为空")
		return
	}
	if req.LarkType == "" {
		req.LarkType = "feishu"
	}
	res, err := database.DB.Exec(`INSERT INTO lark_configs (name, webhook_url, secret, lark_type, description) VALUES (?,?,?,?,?)`,
		req.Name, req.WebhookURL, req.Secret, req.LarkType, req.Description)
	if err != nil {
		jsonError(w, 50000, err.Error())
		return
	}
	id, _ := res.LastInsertId()
	jsonSuccess(w, map[string]interface{}{"id": id})
}

func HandleUpdateLarkConfig(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(mux.Vars(r)["id"], 10, 64)
	var req struct {
		Name        string `json:"name"`
		WebhookURL  string `json:"webhook_url"`
		Secret      string `json:"secret"`
		LarkType    string `json:"lark_type"`
		Description string `json:"description"`
		Status      int    `json:"status"`
	}
	if err := decodeJSON(r, &req); err != nil {
		jsonError(w, 40000, "无效的请求")
		return
	}
	_, err := database.DB.Exec(`UPDATE lark_configs SET name=?, webhook_url=?, secret=?, lark_type=?, description=?, status=? WHERE id=?`,
		req.Name, req.WebhookURL, req.Secret, req.LarkType, req.Description, req.Status, id)
	if err != nil {
		jsonError(w, 50000, err.Error())
		return
	}
	jsonSuccess(w, nil)
}

func HandleDeleteLarkConfig(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(mux.Vars(r)["id"], 10, 64)
	_, err := database.DB.Exec(`DELETE FROM lark_configs WHERE id=?`, id)
	if err != nil {
		jsonError(w, 50000, err.Error())
		return
	}
	jsonSuccess(w, nil)
}

func HandleTestLarkConfig(w http.ResponseWriter, r *http.Request) {
	// TODO: 阶段 2 实现, 调用 lark/sender 发送测试卡片
	jsonSuccess(w, map[string]interface{}{"status": "stub", "msg": "测试发送功能待实现 (阶段2)"})
}
