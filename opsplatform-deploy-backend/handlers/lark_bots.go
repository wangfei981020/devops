package handlers

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/gorilla/mux"
	"opsplatform-deploy-backend/crypto"
	"opsplatform-deploy-backend/database"
	"opsplatform-deploy-backend/models"
	"opsplatform-deploy-backend/services"
)

// GET /api/lark-bots
func HandleListLarkBots(w http.ResponseWriter, r *http.Request) {
	rows, err := database.DB.Query(`SELECT id, name, webhook, secret, description, created_at, updated_at FROM lark_bot ORDER BY name`)
	if err != nil {
		JSONError(w, 50000, err.Error())
		return
	}
	defer rows.Close()
	list := []models.LarkBot{}
	for rows.Next() {
		var b models.LarkBot
		_ = rows.Scan(&b.ID, &b.Name, &b.Webhook, &b.Secret, &b.Description, &b.CreatedAt, &b.UpdatedAt)
		b.Secret = maskToken(b.Secret)
		list = append(list, b)
	}
	JSONSuccess(w, list)
}

type larkBotReq struct {
	Name        string `json:"name"`
	Webhook     string `json:"webhook"`
	Secret      string `json:"secret"`
	Description string `json:"description"`
}

// POST /api/lark-bots
func HandleCreateLarkBot(w http.ResponseWriter, r *http.Request) {
	var req larkBotReq
	if !DecodeJSON(w, r, &req) {
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	req.Webhook = strings.TrimSpace(req.Webhook)
	if req.Name == "" || req.Webhook == "" {
		JSONError(w, 40001, "name 和 webhook 必填")
		return
	}
	encSec, _ := crypto.Encrypt(req.Secret)
	res, err := database.DB.Exec(`INSERT INTO lark_bot (name, webhook, secret, description) VALUES (?, ?, ?, ?)`,
		req.Name, req.Webhook, encSec, strings.TrimSpace(req.Description))
	if err != nil {
		if strings.Contains(err.Error(), "Duplicate") {
			JSONError(w, 40900, "name 已存在")
			return
		}
		JSONError(w, 50000, err.Error())
		return
	}
	id, _ := res.LastInsertId()
	JSONSuccess(w, map[string]interface{}{"id": id})
}

// PUT /api/lark-bots/{id}
func HandleUpdateLarkBot(w http.ResponseWriter, r *http.Request) {
	id := ParseID(mux.Vars(r)["id"])
	var req larkBotReq
	if !DecodeJSON(w, r, &req) {
		return
	}
	sets := []string{"webhook=?", "description=?"}
	args := []interface{}{strings.TrimSpace(req.Webhook), strings.TrimSpace(req.Description)}
	if req.Secret != "" {
		enc, _ := crypto.Encrypt(req.Secret)
		sets = append(sets, "secret=?")
		args = append(args, enc)
	}
	args = append(args, id)
	q := "UPDATE lark_bot SET " + joinComma(sets) + " WHERE id=?"
	if _, err := database.DB.Exec(q, args...); err != nil {
		JSONError(w, 50000, err.Error())
		return
	}
	JSONSuccess(w, nil)
}

// DELETE /api/lark-bots/{id}
func HandleDeleteLarkBot(w http.ResponseWriter, r *http.Request) {
	id := ParseID(mux.Vars(r)["id"])
	var cnt int
	_ = database.DB.QueryRow(`SELECT COUNT(*) FROM project_env WHERE lark_bot_id=?`, id).Scan(&cnt)
	if cnt > 0 {
		JSONError(w, 40900, "还有 "+intToStr(cnt)+" 个项目环境在使用此 Lark 机器人")
		return
	}
	if _, err := database.DB.Exec(`DELETE FROM lark_bot WHERE id=?`, id); err != nil {
		JSONError(w, 50000, err.Error())
		return
	}
	JSONSuccess(w, nil)
}

// POST /api/lark-bots/{id}/test
func HandleTestLarkBot(w http.ResponseWriter, r *http.Request) {
	id := ParseID(mux.Vars(r)["id"])
	b, err := LoadLarkBotDecrypted(id)
	if err != nil {
		JSONError(w, 40400, "lark bot not found")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
	defer cancel()
	if err := services.SendLarkCard(ctx, b.Webhook, b.Secret,
		"✅ 测试消息", "这是来自 Deploy Center 的测试消息", "blue", "", ""); err != nil {
		JSONError(w, 50005, "发送失败: "+err.Error())
		return
	}
	JSONSuccess(w, map[string]interface{}{"ok": true})
}

// LoadLarkBotDecrypted 内部：加载并解密 secret
func LoadLarkBotDecrypted(id int64) (*models.LarkBot, error) {
	var b models.LarkBot
	err := database.DB.QueryRow(`SELECT id, name, webhook, secret, description, created_at, updated_at
		FROM lark_bot WHERE id=?`, id).
		Scan(&b.ID, &b.Name, &b.Webhook, &b.Secret, &b.Description, &b.CreatedAt, &b.UpdatedAt)
	if err != nil {
		return nil, err
	}
	b.Secret, _ = crypto.Decrypt(b.Secret)
	return &b, nil
}
