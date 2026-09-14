package handlers

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/gorilla/mux"
	"opsplatform-alert-backend/database"
	"opsplatform-alert-backend/models"
	"opsplatform-alert-backend/notify"
)

const channelSelectCols = `id, channel_type, name, webhook_url, lark_type,
	bot_token, chat_id, thread_id, proxy_url, description, status, created_at, updated_at`

func HandleListNotifyChannels(w http.ResponseWriter, r *http.Request) {
	rows, err := database.DB.Query("SELECT " + channelSelectCols + " FROM notify_channels ORDER BY id DESC")
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "查询失败")
		return
	}
	defer rows.Close()

	list := []models.NotifyChannel{}
	for rows.Next() {
		var c models.NotifyChannel
		if err := rows.Scan(&c.ID, &c.ChannelType, &c.Name, &c.WebhookURL, &c.LarkType,
			&c.BotToken, &c.ChatID, &c.ThreadID, &c.ProxyURL, &c.Description,
			&c.Status, &c.CreatedAt, &c.UpdatedAt); err != nil {
			continue
		}
		// Never ship a bot token to the browser; it is a full-control credential.
		c.BotToken = maskToken(c.BotToken)
		// A proxy URL can carry user:pass@ — same class of secret, same rule.
		c.ProxyURL = maskProxyURL(c.ProxyURL)
		list = append(list, c)
	}
	if err := rows.Err(); err != nil {
		jsonError(w, http.StatusInternalServerError, "查询失败")
		return
	}
	jsonSuccess(w, list)
}

// maskToken keeps the numeric bot id visible so operators can tell two bots
// apart, and hides the secret half.
func maskToken(token string) string {
	if token == "" {
		return ""
	}
	for i, ch := range token {
		if ch == ':' {
			return token[:i] + ":****"
		}
	}
	return "****"
}

// proxyUserinfoMask is what replaces the credential half of a proxy URL on its
// way to the browser. It is also the marker that tells an update the client is
// echoing back a masked value rather than typing a new one.
const proxyUserinfoMask = "***:***"

// maskProxyURL hides any userinfo in a proxy URL. "http://bob:s3cr3t@p:8080"
// becomes "http://***:***@p:8080". A URL that will not parse is masked by hand
// rather than passed through, so a malformed value cannot leak a password.
func maskProxyURL(raw string) string {
	if raw == "" || !strings.Contains(raw, "@") {
		return raw
	}
	if u, err := url.Parse(raw); err == nil && u.User != nil {
		u.User = nil // url.User would percent-escape the mask's colon
		s := u.String()
		if i := strings.Index(s, "://"); i != -1 {
			return s[:i+3] + proxyUserinfoMask + "@" + s[i+3:]
		}
		return proxyUserinfoMask + "@" + s
	}
	// Unparseable but contains '@': mask everything before the last one.
	at := strings.LastIndex(raw, "@")
	scheme := ""
	if i := strings.Index(raw, "://"); i != -1 && i < at {
		scheme = raw[:i+3]
	}
	return scheme + proxyUserinfoMask + raw[at:]
}

// isMaskedProxyURL reports whether the client sent back the mask the list
// endpoint handed it. Unlike bot_token there is no separate "keep it" signal,
// and proxy_url is written unconditionally, so without this an operator who
// edits any other field would overwrite a real proxy credential with "***:***".
func isMaskedProxyURL(raw string) bool {
	return strings.Contains(raw, proxyUserinfoMask+"@")
}

// isMaskedToken reports whether token is what the browser echoes back after
// receiving a maskToken'd value — never a real credential the client typed.
func isMaskedToken(token string) bool {
	return strings.HasSuffix(token, "****")
}

// validateChannel normalises a request and rejects incomplete configurations.
// On create (isUpdate == false) a credential is mandatory. On update, an
// empty or masked bot_token is legal and means "keep the stored value" — the
// browser only ever holds a masked token, so it can never send back the real
// one.
func validateChannel(req *models.CreateNotifyChannelReq, isUpdate bool) error {
	if req.Name == "" {
		return fmt.Errorf("名称不能为空")
	}
	if req.ChannelType == "" {
		req.ChannelType = "lark"
	}
	switch req.ChannelType {
	case "lark":
		if req.WebhookURL == "" {
			return fmt.Errorf("Lark 渠道必须填写 Webhook URL")
		}
		if req.LarkType == "" {
			req.LarkType = "feishu"
		}
	case "telegram":
		hasToken := req.BotToken != "" && !isMaskedToken(req.BotToken)
		if !hasToken && !isUpdate {
			return fmt.Errorf("Telegram 渠道必须填写 Bot Token")
		}
		if req.ChatID == "" {
			return fmt.Errorf("Telegram 渠道必须填写 Chat ID")
		}
	default:
		return fmt.Errorf("不支持的渠道类型: %s", req.ChannelType)
	}
	return nil
}

func HandleCreateNotifyChannel(w http.ResponseWriter, r *http.Request) {
	var req models.CreateNotifyChannelReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, http.StatusBadRequest, "无效的请求")
		return
	}
	if err := validateChannel(&req, false); err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}

	result, err := database.DB.Exec(`INSERT INTO notify_channels
		(channel_type, name, webhook_url, secret, lark_type, bot_token, chat_id, thread_id, proxy_url, description)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		req.ChannelType, req.Name, req.WebhookURL, req.Secret, req.LarkType,
		req.BotToken, req.ChatID, req.ThreadID, req.ProxyURL, req.Description)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "创建失败: "+err.Error())
		return
	}
	id, _ := result.LastInsertId()
	SaveAuditLog(r, "create_channel", "channel", req.Name,
		fmt.Sprintf("创建通知渠道 ID=%d type=%s", id, req.ChannelType))
	jsonSuccess(w, map[string]interface{}{"id": id})
}

func HandleUpdateNotifyChannel(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.Atoi(mux.Vars(r)["id"])
	var req models.CreateNotifyChannelReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, http.StatusBadRequest, "无效的请求")
		return
	}

	// The stored row is authoritative for channel_type, exactly as it already
	// is for the test endpoint. The UI locks the type dropdown after the first
	// save, but a stale tab or a hand-made PUT could otherwise flip a Lark
	// channel to Telegram — and update-mode validation does not demand a bot
	// token — leaving channel_type='telegram' with an empty token and an
	// overwritten, unrecoverable webhook_url.
	stored, err := loadStoredChannel(id)
	if err != nil {
		jsonError(w, http.StatusNotFound, "渠道不存在")
		return
	}
	if req.ChannelType == "" {
		req.ChannelType = stored.ChannelType
	}
	if req.ChannelType != stored.ChannelType {
		jsonError(w, http.StatusBadRequest,
			fmt.Sprintf("渠道类型不可修改（当前为 %s，请求为 %s）", stored.ChannelType, req.ChannelType))
		return
	}

	if err := validateChannel(&req, true); err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	// A masked proxy_url ("http://***:***@host") is the exact string the list
	// endpoint handed the browser, not something an operator typed. proxy_url
	// is written unconditionally, so echoing it back would replace a working
	// proxy credential with the mask; keep the stored value instead.
	if isMaskedProxyURL(req.ProxyURL) {
		req.ProxyURL = stored.ProxyURL
	}
	// A masked token ("123:****") is not a credential the browser ever typed —
	// it is the exact string HandleListNotifyChannels handed back. Treat it
	// the same as an empty value so the SQL's IF(? = '', ...) preserve branch
	// below keeps the real stored token instead of overwriting it with the
	// mask itself.
	if isMaskedToken(req.BotToken) {
		req.BotToken = ""
	}

	// An empty secret/bot_token means "leave the stored credential alone" —
	// the list endpoint masks them, so the UI cannot echo the real value back.
	_, err = database.DB.Exec(`UPDATE notify_channels SET
		channel_type=?, name=?, webhook_url=?,
		secret = IF(? = '', secret, ?),
		lark_type=?,
		bot_token = IF(? = '', bot_token, ?),
		chat_id=?, thread_id=?, proxy_url=?, description=?
		WHERE id=?`,
		req.ChannelType, req.Name, req.WebhookURL,
		req.Secret, req.Secret,
		req.LarkType,
		req.BotToken, req.BotToken,
		req.ChatID, req.ThreadID, req.ProxyURL, req.Description, id)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "更新失败: "+err.Error())
		return
	}
	SaveAuditLog(r, "update_channel", "channel", req.Name, fmt.Sprintf("更新通知渠道 ID=%d", id))
	jsonSuccess(w, nil)
}

func HandleDeleteNotifyChannel(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.Atoi(mux.Vars(r)["id"])

	// A safety guard must fail closed: if either count query errors, refuse the
	// delete rather than silently treating the channel as unused.
	var count int
	if err := database.DB.QueryRow(`SELECT COUNT(*) FROM alert_rule_channels WHERE channel_id = ?`, id).Scan(&count); err != nil {
		jsonError(w, http.StatusInternalServerError, "检查渠道使用情况失败: "+err.Error())
		return
	}
	if count > 0 {
		jsonError(w, http.StatusBadRequest, "该渠道正在被告警规则使用，无法删除")
		return
	}
	// The engine falls back to alert_rules.lark_config_id for any rule that
	// has no alert_rule_channels rows yet (rule creation does not write them
	// until Task 9). Missing this check lets an operator delete a channel
	// that is a rule's only destination with no warning at all.
	var legacyCount int
	if err := database.DB.QueryRow(`SELECT COUNT(*) FROM alert_rules WHERE lark_config_id = ?`, id).Scan(&legacyCount); err != nil {
		jsonError(w, http.StatusInternalServerError, "检查渠道使用情况失败: "+err.Error())
		return
	}
	if legacyCount > 0 {
		jsonError(w, http.StatusBadRequest, "该渠道正在被告警规则使用，无法删除")
		return
	}

	database.DB.Exec("DELETE FROM notify_channels WHERE id = ?", id)
	SaveAuditLog(r, "delete_channel", "channel", fmt.Sprintf("ID=%d", id), "删除通知渠道")
	jsonSuccess(w, nil)
}

func HandleToggleNotifyChannel(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.Atoi(mux.Vars(r)["id"])
	database.DB.Exec("UPDATE notify_channels SET status = IF(status=1, 0, 1) WHERE id = ?", id)
	SaveAuditLog(r, "toggle_channel", "channel", fmt.Sprintf("ID=%d", id), "切换通知渠道状态")
	jsonSuccess(w, nil)
}

// loadStoredChannel fetches a channel's full row, credentials included, for
// internal use only (never serialised back to a client wholesale).
func loadStoredChannel(id int) (models.NotifyChannel, error) {
	var c models.NotifyChannel
	err := database.DB.QueryRow(`SELECT id, channel_type, name, webhook_url, secret, lark_type,
		bot_token, chat_id, thread_id, proxy_url, description, status, created_at, updated_at
		FROM notify_channels WHERE id = ?`, id).
		Scan(&c.ID, &c.ChannelType, &c.Name, &c.WebhookURL, &c.Secret, &c.LarkType,
			&c.BotToken, &c.ChatID, &c.ThreadID, &c.ProxyURL, &c.Description,
			&c.Status, &c.CreatedAt, &c.UpdatedAt)
	return c, err
}

func HandleTestNotifyChannel(w http.ResponseWriter, r *http.Request) {
	var req models.CreateNotifyChannelReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, http.StatusBadRequest, "无效的请求")
		return
	}

	var ch models.NotifyChannel

	if idStr := r.URL.Query().Get("id"); idStr != "" {
		id, err := strconv.Atoi(idStr)
		if err != nil || id <= 0 {
			jsonError(w, http.StatusBadRequest, "无效的渠道ID")
			return
		}
		// Load the stored channel as the base of truth. The request body may
		// override only non-credential display fields; channel_type and every
		// credential (secret, bot_token) always come from the stored row.
		// This is deliberate: without it, a caller could pair a victim
		// channel's id with a body entirely of its own choosing (e.g. its own
		// unreachable proxy_url) and use the resulting send failure to
		// exfiltrate the victim's real credential through the error message.
		stored, err := loadStoredChannel(id)
		if err != nil {
			jsonError(w, http.StatusBadRequest, "渠道不存在")
			return
		}
		ch = stored
		if req.Name != "" {
			ch.Name = req.Name
		}
		if req.WebhookURL != "" {
			ch.WebhookURL = req.WebhookURL
		}
		if req.LarkType != "" {
			ch.LarkType = req.LarkType
		}
		if req.ChatID != "" {
			ch.ChatID = req.ChatID
		}
		if req.ThreadID != 0 {
			ch.ThreadID = req.ThreadID
		}
		if req.ProxyURL != "" {
			ch.ProxyURL = req.ProxyURL
		}
		if req.Description != "" {
			ch.Description = req.Description
		}
		// channel_type, secret and bot_token are intentionally never taken
		// from req here.
	} else {
		// No id: the client is testing brand-new, unsaved credentials, so it
		// must supply a complete configuration itself.
		if err := validateChannel(&req, false); err != nil {
			jsonError(w, http.StatusBadRequest, err.Error())
			return
		}
		ch = models.NotifyChannel{
			ChannelType: req.ChannelType,
			Name:        req.Name,
			WebhookURL:  req.WebhookURL,
			Secret:      req.Secret,
			LarkType:    req.LarkType,
			BotToken:    req.BotToken,
			ChatID:      req.ChatID,
			ThreadID:    req.ThreadID,
			ProxyURL:    req.ProxyURL,
		}
	}

	sender, err := notify.New(ch)
	if err != nil {
		jsonError(w, http.StatusBadRequest, "配置无效: "+err.Error())
		return
	}
	resp, err := sender.TestWebhook()
	if err != nil {
		// Never return a raw send error to the client: telegram's transport
		// errors embed the request URL (bot token included), and any network
		// hiccup — or an attacker-chosen unreachable proxy_url — is enough to
		// trigger one. Log the detail server-side only.
		log.Printf("[notify-channels] test send failed for channel %q (id=%d, type=%s): %v",
			ch.Name, ch.ID, ch.ChannelType, err)
		jsonError(w, http.StatusBadRequest, fmt.Sprintf("测试失败: 渠道「%s」发送未成功，请检查配置", ch.Name))
		return
	}
	jsonSuccess(w, map[string]string{"response": resp})
}
