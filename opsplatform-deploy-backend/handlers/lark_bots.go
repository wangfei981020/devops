package handlers

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/mux"
	"opsplatform-deploy-backend/crypto"
	"opsplatform-deploy-backend/database"
	"opsplatform-deploy-backend/models"
	"opsplatform-deploy-backend/services"
)

// GET /api/lark-bots
// 非 admin 用户不返回 webhook URL（URL 本身可被任何人用来发 lark 消息）
func HandleListLarkBots(w http.ResponseWriter, r *http.Request) {
	rows, err := database.DB.Query(`SELECT id, name, webhook, secret, description, created_at, updated_at FROM lark_bot ORDER BY name`)
	if err != nil {
		InternalErr(w, r, err)
		return
	}
	defer rows.Close()
	isAdmin := IsAdmin(r)
	list := []models.LarkBot{}
	for rows.Next() {
		var b models.LarkBot
		_ = rows.Scan(&b.ID, &b.Name, &b.Webhook, &b.Secret, &b.Description, &b.CreatedAt, &b.UpdatedAt)
		b.Secret = maskToken(b.Secret)
		if !isAdmin {
			b.Webhook = maskToken(b.Webhook)
		}
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
	if err := ValidateName(req.Name); err != nil {
		JSONError(w, 40001, "name: "+err.Error())
		return
	}
	if err := ValidateLarkWebhookURL(req.Webhook); err != nil {
		JSONError(w, 40001, err.Error())
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
		InternalErr(w, r, err)
		return
	}
	id, _ := res.LastInsertId()
	Audit(r, "lark_bot.create", "lark_bot", req.Name, nil)
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
	q := "UPDATE lark_bot SET " + strings.Join(sets, ",") + " WHERE id=?"
	if _, err := database.DB.Exec(q, args...); err != nil {
		InternalErr(w, r, err)
		return
	}
	Audit(r, "lark_bot.update", "lark_bot", strconv.FormatInt(id, 10), nil)
	JSONSuccess(w, nil)
}

// DELETE /api/lark-bots/{id}
func HandleDeleteLarkBot(w http.ResponseWriter, r *http.Request) {
	id := ParseID(mux.Vars(r)["id"])
	var cnt int
	_ = database.DB.QueryRow(`SELECT COUNT(*) FROM project_env WHERE lark_bot_id=?`, id).Scan(&cnt)
	if cnt > 0 {
		JSONError(w, 40900, "还有 "+strconv.Itoa(cnt)+" 个项目环境在使用此 Lark 机器人")
		return
	}
	if _, err := database.DB.Exec(`DELETE FROM lark_bot WHERE id=?`, id); err != nil {
		InternalErr(w, r, err)
		return
	}
	Audit(r, "lark_bot.delete", "lark_bot", strconv.FormatInt(id, 10), nil)
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
