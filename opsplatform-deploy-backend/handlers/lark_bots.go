package handlers

import (
	"context"
	"fmt"
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

// GET /api/lark-bots（页面上叫「通知渠道」：飞书 + Teams）
// 非 admin 用户不返回 webhook URL（URL 本身可被任何人用来发 lark 消息）；
// Teams 地址带 sig 等同凭据，对谁都不返回，只给 teams_configured
func HandleListLarkBots(w http.ResponseWriter, r *http.Request) {
	rows, err := database.DB.Query(`SELECT id, name, webhook, secret, teams_webhook <> '', description, created_at, updated_at FROM lark_bot ORDER BY name`)
	if err != nil {
		InternalErr(w, r, err)
		return
	}
	defer rows.Close()
	isAdmin := IsAdmin(r)
	list := []models.LarkBot{}
	for rows.Next() {
		var b models.LarkBot
		_ = rows.Scan(&b.ID, &b.Name, &b.Webhook, &b.Secret, &b.TeamsConfigured, &b.Description, &b.CreatedAt, &b.UpdatedAt)
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
	// TeamsWebhook 不传 = 保持原值（编辑时页面不回显地址）；传 "" = 清除；传地址 = 设置
	TeamsWebhook *string `json:"teams_webhook"`
}

// auditView 审计用：去掉 Teams 地址（带 sig），只记有没有改
func (q larkBotReq) auditView() larkBotReq {
	if q.TeamsWebhook != nil && *q.TeamsWebhook != "" {
		v := "****"
		q.TeamsWebhook = &v
	}
	return q
}

// validateChannel 校验渠道最终状态：飞书、Teams 都可留空，但至少填一个；填了的必须合规。
func validateChannel(larkWebhook, teamsWebhook string) error {
	if larkWebhook == "" && teamsWebhook == "" {
		return fmt.Errorf("飞书 Webhook 和 Teams 地址至少填一个")
	}
	if larkWebhook != "" {
		if err := ValidateLarkWebhookURL(larkWebhook); err != nil {
			return err
		}
	}
	if teamsWebhook != "" {
		if err := services.ValidateTeamsWebhook(teamsWebhook); err != nil {
			return err
		}
	}
	return nil
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
	teams := ""
	if req.TeamsWebhook != nil {
		teams = services.NormalizeTeamsWebhook(*req.TeamsWebhook)
	}
	if err := validateChannel(req.Webhook, teams); err != nil {
		JSONError(w, 40001, err.Error())
		return
	}
	encSec, _ := crypto.Encrypt(req.Secret)
	encTeams := ""
	if teams != "" {
		var err error
		if encTeams, err = crypto.Encrypt(teams); err != nil {
			InternalErr(w, r, err)
			return
		}
	}
	res, err := database.DB.Exec(`INSERT INTO lark_bot (name, webhook, secret, teams_webhook, description) VALUES (?, ?, ?, ?, ?)`,
		req.Name, req.Webhook, encSec, encTeams, strings.TrimSpace(req.Description))
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
	old, err := LoadLarkBotDecrypted(id)
	if err != nil {
		JSONError(w, 40400, "通知渠道不存在")
		return
	}
	req.Webhook = strings.TrimSpace(req.Webhook)
	teams := old.TeamsWebhook
	if req.TeamsWebhook != nil {
		teams = services.NormalizeTeamsWebhook(*req.TeamsWebhook)
	}
	if err := validateChannel(req.Webhook, teams); err != nil {
		JSONError(w, 40001, err.Error())
		return
	}
	sets := []string{"webhook=?", "description=?"}
	args := []interface{}{req.Webhook, strings.TrimSpace(req.Description)}
	if req.TeamsWebhook != nil {
		encTeams := ""
		if teams != "" {
			if encTeams, err = crypto.Encrypt(teams); err != nil {
				InternalErr(w, r, err)
				return
			}
		}
		sets = append(sets, "teams_webhook=?")
		args = append(args, encTeams)
	}
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
	Audit(r, "lark_bot.update", "lark_bot", strconv.FormatInt(id, 10), auditDetailFromReq(req.auditView()))
	JSONSuccess(w, nil)
}

// DELETE /api/lark-bots/{id}
func HandleDeleteLarkBot(w http.ResponseWriter, r *http.Request) {
	id := ParseID(mux.Vars(r)["id"])
	var cnt int
	_ = database.DB.QueryRow(`SELECT COUNT(*) FROM project_env WHERE lark_bot_id=?`, id).Scan(&cnt)
	if cnt > 0 {
		JSONError(w, 40900, "还有 "+strconv.Itoa(cnt)+" 个项目环境在使用此通知渠道")
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
// （保留兼容）根据 ID 从 DB 读取测试
func HandleTestLarkBot(w http.ResponseWriter, r *http.Request) {
	id := ParseID(mux.Vars(r)["id"])
	b, err := LoadLarkBotDecrypted(id)
	if err != nil {
		JSONError(w, 40400, "lark bot not found")
		return
	}
	if b.Webhook == "" {
		JSONError(w, 40001, "这个渠道没配飞书")
		return
	}
	doTestLark(w, r, b.Webhook, b.Secret)
}

// POST /api/lark-bots/test
// 新：前端 body 传 {id?, webhook?, secret?} 测试发送。未保存时也能测。
// channel=teams 时测 Teams：{id?, channel:"teams", teams_webhook?}，地址没填就用库里的。
func HandleTestLarkBotByBody(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID           *int64 `json:"id"`
		Webhook      string `json:"webhook"`
		Secret       string `json:"secret"`
		Channel      string `json:"channel"`
		TeamsWebhook string `json:"teams_webhook"`
	}
	if !DecodeJSON(w, r, &req) {
		return
	}
	if req.Channel == "teams" {
		teams := services.NormalizeTeamsWebhook(req.TeamsWebhook)
		if teams == "" && req.ID != nil && *req.ID > 0 {
			if b, err := LoadLarkBotDecrypted(*req.ID); err == nil {
				teams = b.TeamsWebhook
			}
		}
		if teams == "" {
			JSONError(w, 40001, "Teams 地址必填")
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
		defer cancel()
		if err := services.SendTeamsCard(ctx, teams, "✅ 测试消息", "这是来自 Deploy Center 的测试消息", "blue", "", "", nil); err != nil {
			JSONError(w, 50005, "发送失败: "+err.Error())
			return
		}
		JSONSuccess(w, map[string]interface{}{"ok": true})
		return
	}
	webhook, secret := strings.TrimSpace(req.Webhook), req.Secret
	if req.ID != nil && *req.ID > 0 {
		if b, err := LoadLarkBotDecrypted(*req.ID); err == nil {
			// 🔒 安全：改了 webhook（可能指向攻击者地址）必须重新提供 secret，
			// 否则用 DB 真 secret 给攻击者 webhook 算 HMAC 签名 → 攻击者从签名+timestamp 反推 secret。
			webhookChanged := webhook != "" && webhook != b.Webhook
			if webhookChanged && secret == "" {
				JSONError(w, 40000, "修改了 webhook 时必须重新提供 secret（防止 HMAC 签名材料外泄到不可信地址）")
				return
			}
			if webhook == "" {
				webhook = b.Webhook
			}
			if secret == "" {
				secret = b.Secret
			}
		}
	}
	if webhook == "" {
		JSONError(w, 40001, "webhook 必填")
		return
	}
	doTestLark(w, r, webhook, secret)
}

func doTestLark(w http.ResponseWriter, r *http.Request, webhook, secret string) {
	ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
	defer cancel()
	if err := services.SendLarkCard(ctx, webhook, secret,
		"✅ 测试消息", "这是来自 Deploy Center 的测试消息", "blue", "", ""); err != nil {
		JSONError(w, 50005, "发送失败: "+err.Error())
		return
	}
	JSONSuccess(w, map[string]interface{}{"ok": true})
}

// LoadLarkBotDecrypted 内部：加载并解密 secret、teams_webhook
func LoadLarkBotDecrypted(id int64) (*models.LarkBot, error) {
	var b models.LarkBot
	err := database.DB.QueryRow(`SELECT id, name, webhook, secret, teams_webhook, description, created_at, updated_at
		FROM lark_bot WHERE id=?`, id).
		Scan(&b.ID, &b.Name, &b.Webhook, &b.Secret, &b.TeamsWebhook, &b.Description, &b.CreatedAt, &b.UpdatedAt)
	if err != nil {
		return nil, err
	}
	b.Secret, _ = crypto.Decrypt(b.Secret)
	b.TeamsWebhook, _ = crypto.Decrypt(b.TeamsWebhook)
	b.TeamsConfigured = b.TeamsWebhook != ""
	return &b, nil
}
