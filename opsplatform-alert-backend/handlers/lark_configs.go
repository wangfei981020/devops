package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"

	"github.com/gorilla/mux"
	"opsplatform-alert-backend/database"
	"opsplatform-alert-backend/lark"
	"opsplatform-alert-backend/models"
)

func HandleListLarkConfigs(w http.ResponseWriter, r *http.Request) {
	rows, err := database.DB.Query("SELECT id, name, webhook_url, lark_type, description, status, created_at, updated_at FROM lark_configs ORDER BY id DESC")
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "查询失败")
		return
	}
	defer rows.Close()

	var list []models.LarkConfig
	for rows.Next() {
		var c models.LarkConfig
		rows.Scan(&c.ID, &c.Name, &c.WebhookURL, &c.LarkType, &c.Description, &c.Status, &c.CreatedAt, &c.UpdatedAt)
		list = append(list, c)
	}
	if list == nil {
		list = []models.LarkConfig{}
	}
	jsonSuccess(w, list)
}

func HandleCreateLarkConfig(w http.ResponseWriter, r *http.Request) {
	var req models.CreateLarkConfigReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, http.StatusBadRequest, "无效的请求")
		return
	}
	if req.Name == "" || req.WebhookURL == "" {
		jsonError(w, http.StatusBadRequest, "名称和Webhook URL不能为空")
		return
	}
	if req.LarkType == "" {
		req.LarkType = "feishu"
	}

	result, err := database.DB.Exec(`INSERT INTO lark_configs (name, webhook_url, secret, lark_type, description)
		VALUES (?, ?, ?, ?, ?)`, req.Name, req.WebhookURL, req.Secret, req.LarkType, req.Description)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "创建失败: "+err.Error())
		return
	}
	id, _ := result.LastInsertId()
	SaveAuditLog(r, "create_lark", "lark", req.Name, fmt.Sprintf("创建Lark配置 ID=%d", id))
	jsonSuccess(w, map[string]interface{}{"id": id})
}

func HandleUpdateLarkConfig(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.Atoi(mux.Vars(r)["id"])
	var req models.CreateLarkConfigReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, http.StatusBadRequest, "无效的请求")
		return
	}

	_, err := database.DB.Exec(`UPDATE lark_configs SET name=?, webhook_url=?, secret=?, lark_type=?, description=? WHERE id=?`,
		req.Name, req.WebhookURL, req.Secret, req.LarkType, req.Description, id)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "更新失败")
		return
	}
	SaveAuditLog(r, "update_lark", "lark", req.Name, fmt.Sprintf("更新Lark配置 ID=%d", id))
	jsonSuccess(w, nil)
}

func HandleDeleteLarkConfig(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.Atoi(mux.Vars(r)["id"])

	var count int
	database.DB.QueryRow("SELECT COUNT(*) FROM alert_rules WHERE lark_config_id = ?", id).Scan(&count)
	if count > 0 {
		jsonError(w, http.StatusBadRequest, "该配置正在被告警规则使用，无法删除")
		return
	}

	database.DB.Exec("DELETE FROM lark_configs WHERE id = ?", id)
	SaveAuditLog(r, "delete_lark", "lark", fmt.Sprintf("ID=%d", id), "删除Lark配置")
	jsonSuccess(w, nil)
}

func HandleToggleLarkConfig(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.Atoi(mux.Vars(r)["id"])
	database.DB.Exec("UPDATE lark_configs SET status = IF(status=1, 0, 1) WHERE id = ?", id)
	SaveAuditLog(r, "toggle_lark", "lark", fmt.Sprintf("ID=%d", id), "切换Lark配置状态")
	jsonSuccess(w, nil)
}

func HandleTestLarkConfig(w http.ResponseWriter, r *http.Request) {
	var req models.CreateLarkConfigReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, http.StatusBadRequest, "无效的请求")
		return
	}

	sender := lark.NewSender(models.LarkConfig{
		WebhookURL: req.WebhookURL,
		Secret:     req.Secret,
		LarkType:   req.LarkType,
	})

	resp, err := sender.TestWebhook()
	if err != nil {
		jsonError(w, http.StatusBadRequest, "测试失败: "+err.Error())
		return
	}
	jsonSuccess(w, map[string]string{"response": resp})
}
