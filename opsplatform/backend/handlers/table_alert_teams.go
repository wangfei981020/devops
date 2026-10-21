package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"opsplatform/database"
	"opsplatform/services"
)

// 桌台维护告警发 Teams。
//
// 通知群（table_alert_lark_bots）多了一个类型：lark = Lark 机器人，teams = Teams 工作流。
// 规则里的「艾特谁」仍存 Lark ID 列表（页面按通讯录勾选）；发 Teams 时按 Lark ID 反查
// 通讯人，用他的 Teams 账号 @，没填就按「姓名小写@默认后缀」推导。

const (
	taBotKindLark  = "lark"
	taBotKindTeams = "teams"
	// taTeamsDomainKey 系统参数里的 Teams 账号默认后缀，如 example.com；空 = 不推导。
	taTeamsDomainKey = "table_alert_teams_domain"
)

func taTeamsDomain() string {
	var v string
	database.DB.QueryRow(`SELECT setting_value FROM system_settings WHERE setting_key = ? LIMIT 1`, taTeamsDomainKey).Scan(&v)
	return strings.TrimSpace(v)
}

// HandleTAGetTeamsSettings GET /api/table-alert/teams-settings
func HandleTAGetTeamsSettings(w http.ResponseWriter, r *http.Request) {
	if !taRequirePerm(w, r, taPermRead) {
		return
	}
	respondJSON(w, http.StatusOK, map[string]string{"mention_domain": taTeamsDomain()})
}

// HandleTASaveTeamsSettings PUT /api/table-alert/teams-settings
func HandleTASaveTeamsSettings(w http.ResponseWriter, r *http.Request) {
	if !taRequirePerm(w, r, taPermContactManage) {
		return
	}
	var req struct {
		MentionDomain string `json:"mention_domain"`
	}
	if json.NewDecoder(r.Body).Decode(&req) != nil {
		respondError(w, http.StatusBadRequest, "请求体格式错误")
		return
	}
	// "@example.com"、"EXAMPLE.com " 都按 example.com 存
	domain := strings.TrimPrefix(strings.ToLower(strings.TrimSpace(req.MentionDomain)), "@")
	if !services.ValidTeamsDomain(domain) {
		respondError(w, http.StatusBadRequest, "默认后缀应为域名，如 example.com（不带 @）")
		return
	}
	prev := taTeamsDomain()
	if _, err := database.DB.Exec(`INSERT INTO system_settings (setting_key, setting_value) VALUES (?, ?)
		ON DUPLICATE KEY UPDATE setting_value = ?`, taTeamsDomainKey, domain, domain); err != nil {
		respondError(w, http.StatusInternalServerError, "保存失败: "+err.Error())
		return
	}
	taInfof("Teams 账号默认后缀 %q → %q（操作人 %s）", prev, domain, taOperator(r))
	respondJSON(w, http.StatusOK, map[string]string{"message": "已保存"})
}

// taTeamsMentions 把规则里的 Lark ID 换成 Teams 提及。
//
// 通讯录里找不到这个 Lark ID 的（人被删了、ID 改了），名字未知，在卡片上按「未配置」列出
// ID 的末几位，让人能对上是谁。
func taTeamsMentions(atLarkIDs []string) []services.TeamsMention {
	if len(atLarkIDs) == 0 {
		return nil
	}
	domain := taTeamsDomain()
	type contact struct{ name, teams string }
	byLark := map[string]contact{}
	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(atLarkIDs)), ",")
	args := make([]interface{}, len(atLarkIDs))
	for i, id := range atLarkIDs {
		args[i] = id
	}
	rows, err := database.DB.Query(`SELECT lark_id, name, COALESCE(teams_id,'') FROM table_alert_contacts
		WHERE lark_id IN (`+placeholders+`)`, args...)
	if err == nil {
		for rows.Next() {
			var lark, name, teams string
			if rows.Scan(&lark, &name, &teams) == nil {
				byLark[lark] = contact{name, teams}
			}
		}
		rows.Close()
	} else {
		taErrorf("按 Lark ID 查通知人失败（本次 Teams 不 @ 人，告警照发）: %v", err)
	}
	out := make([]services.TeamsMention, 0, len(atLarkIDs))
	for _, id := range atLarkIDs {
		c, ok := byLark[id]
		if !ok {
			tail := id
			if len(tail) > 6 {
				tail = "…" + tail[len(tail)-6:]
			}
			out = append(out, services.TeamsMention{Name: "通讯录里没有 Lark ID " + tail + " 的人"})
			continue
		}
		acc, derived := services.TeamsAccountFor(c.name, c.teams, domain)
		out = append(out, services.TeamsMention{Name: c.name, Account: acc, Derived: derived})
	}
	return out
}

// taSendToBot 按通知群的类型发一条卡片。retry=true 走带重试的告警发送，false 是页面测试。
// 返回值里的 note 是写进日志的附加说明（Teams 的 @ 明细）。
func taSendToBot(kind, botName, webhook, secret, title, body, color, linkLabel, linkURL string,
	atIDs []string, retry bool) (note string, err error) {

	if kind != taBotKindTeams {
		if retry {
			return "", services.SendLarkCardWithRetry(context.Background(), webhook, secret, title, body, color, linkLabel, linkURL, atIDs...)
		}
		return "", services.SendLarkCard(context.Background(), webhook, secret, title, body, color, linkLabel, linkURL, atIDs...)
	}
	mentions := taTeamsMentions(atIDs)
	if len(mentions) > 0 {
		// Teams 不回告账号存不存在：有人说没被提醒到时，靠这一行对照用的是哪个账号
		note = services.TeamsMentionLog(mentions)
		taInfof("Teams 群「%s」%s", botName, note)
	}
	if retry {
		err = services.SendTeamsCardWithRetry(context.Background(), webhook, title, body, color, linkLabel, linkURL, mentions)
	} else {
		err = services.SendTeamsCard(context.Background(), webhook, title, body, color, linkLabel, linkURL, mentions)
	}
	return note, err
}

// taCheckBotWebhook 按类型校验并规整 webhook。Teams 地址去掉复制带进来的不可见字符。
func taCheckBotWebhook(kind, webhook string) (string, error) {
	switch kind {
	case taBotKindTeams:
		webhook = services.NormalizeTeamsWebhook(webhook)
		if err := services.ValidateTeamsWebhook(webhook); err != nil {
			return "", err
		}
		return webhook, nil
	case taBotKindLark:
		return strings.TrimSpace(webhook), nil
	default:
		return "", fmt.Errorf("不支持的群类型: %s", kind)
	}
}

func taBotKindLabel(kind string) string {
	if kind == taBotKindTeams {
		return "Teams"
	}
	return "Lark"
}
