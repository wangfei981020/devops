package handlers

import (
	"database/sql"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"

	"github.com/gorilla/mux"
	"opsplatform-deploy-backend/database"
	"opsplatform-deploy-backend/models"
	"opsplatform-deploy-backend/services"
)

// GET /api/contacts
func HandleListContacts(w http.ResponseWriter, r *http.Request) {
	rows, err := database.DB.Query(`SELECT id, name, lark_id, teams_id, remark, created_at, updated_at FROM contact ORDER BY name`)
	if err != nil {
		InternalErr(w, r, err)
		return
	}
	defer rows.Close()
	domain := teamsMentionDomain()
	list := []models.Contact{}
	for rows.Next() {
		var c models.Contact
		_ = rows.Scan(&c.ID, &c.Name, &c.LarkID, &c.TeamsID, &c.Remark, &c.CreatedAt, &c.UpdatedAt)
		// Teams 卡片实际会 @ 的账号：填写的、推导的（页面灰显）、或者没有
		c.TeamsAccount, c.TeamsDerived = services.TeamsAccountFor(c.Name, c.TeamsID, domain)
		list = append(list, c)
	}
	JSONSuccess(w, list)
}

type contactReq struct {
	Name    string `json:"name"`
	LarkID  string `json:"lark_id"`
	TeamsID string `json:"teams_id"`
	Remark  string `json:"remark"`
}

// normTeamsID 规整并校验 Teams 账号（空 = 按默认后缀推导）
func (q *contactReq) normTeamsID() error {
	q.TeamsID = services.NormalizeTeamsAccount(q.TeamsID)
	if q.TeamsID != "" && !services.ValidTeamsAccount(q.TeamsID) {
		return fmt.Errorf("Teams 账号应为登录邮箱，如 dave@example.com")
	}
	return nil
}

// POST /api/contacts
func HandleCreateContact(w http.ResponseWriter, r *http.Request) {
	var req contactReq
	if !DecodeJSON(w, r, &req) {
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if err := ValidateLabel(req.Name); err != nil {
		JSONError(w, 40001, "name: "+err.Error())
		return
	}
	if err := req.normTeamsID(); err != nil {
		JSONError(w, 40001, err.Error())
		return
	}
	res, err := database.DB.Exec(`INSERT INTO contact (name, lark_id, teams_id, remark) VALUES (?, ?, ?, ?)`,
		req.Name, strings.TrimSpace(req.LarkID), req.TeamsID, strings.TrimSpace(req.Remark))
	if err != nil {
		if strings.Contains(err.Error(), "Duplicate") {
			JSONError(w, 40900, "name 已存在")
			return
		}
		InternalErr(w, r, err)
		return
	}
	id, _ := res.LastInsertId()
	Audit(r, "contact.create", "contact", req.Name, nil)
	JSONSuccess(w, map[string]interface{}{"id": id})
}

// PUT /api/contacts/{id}
func HandleUpdateContact(w http.ResponseWriter, r *http.Request) {
	id := ParseID(mux.Vars(r)["id"])
	var req contactReq
	if !DecodeJSON(w, r, &req) {
		return
	}
	if err := req.normTeamsID(); err != nil {
		JSONError(w, 40001, err.Error())
		return
	}
	_, err := database.DB.Exec(`UPDATE contact SET lark_id=?, teams_id=?, remark=? WHERE id=?`,
		strings.TrimSpace(req.LarkID), req.TeamsID, strings.TrimSpace(req.Remark), id)
	if err != nil {
		InternalErr(w, r, err)
		return
	}
	Audit(r, "contact.update", "contact", strconv.FormatInt(id, 10), auditDetailFromReq(req))
	JSONSuccess(w, nil)
}

// DELETE /api/contacts/{id}
func HandleDeleteContact(w http.ResponseWriter, r *http.Request) {
	id := ParseID(mux.Vars(r)["id"])
	if _, err := database.DB.Exec(`DELETE FROM contact WHERE id=?`, id); err != nil {
		InternalErr(w, r, err)
		return
	}
	Audit(r, "contact.delete", "contact", strconv.FormatInt(id, 10), nil)
	JSONSuccess(w, nil)
}

// LookupContactLarkID 按 operator 的 username 精确匹配 contact.name
// 不再用 display_name 兜底（避免冒名艾特）
func LookupContactLarkID(operator string) string {
	if operator == "" || operator == "system" {
		return ""
	}
	var larkID string
	err := database.DB.QueryRow(`SELECT lark_id FROM contact WHERE name=?`, operator).Scan(&larkID)
	if err == sql.ErrNoRows {
		return ""
	}
	return larkID
}

// ---------------------------------------------------------------------------
// Teams @人（照运维平台 table_alert_teams.go）
// ---------------------------------------------------------------------------

// teamsMentionDomain Teams 账号默认后缀，如 example.com；空 = 不推导。
func teamsMentionDomain() string {
	var v string
	_ = database.DB.QueryRow(`SELECT teams_mention_domain FROM global_config WHERE id=1`).Scan(&v)
	return strings.TrimSpace(v)
}

// GET /api/teams-settings
func HandleGetTeamsSettings(w http.ResponseWriter, r *http.Request) {
	JSONSuccess(w, map[string]string{"mention_domain": teamsMentionDomain()})
}

// PUT /api/teams-settings
func HandleSaveTeamsSettings(w http.ResponseWriter, r *http.Request) {
	var req struct {
		MentionDomain string `json:"mention_domain"`
	}
	if !DecodeJSON(w, r, &req) {
		return
	}
	// "@example.com"、"EXAMPLE.com " 都按 example.com 存
	domain := strings.TrimPrefix(strings.ToLower(strings.TrimSpace(req.MentionDomain)), "@")
	if !services.ValidTeamsDomain(domain) {
		JSONError(w, 40001, "默认后缀应为域名，如 example.com（不带 @）")
		return
	}
	if _, err := database.DB.Exec(`UPDATE global_config SET teams_mention_domain=? WHERE id=1`, domain); err != nil {
		InternalErr(w, r, err)
		return
	}
	Audit(r, "teams_settings.update", "global_config", "teams_mention_domain", map[string]interface{}{"mention_domain": domain})
	JSONSuccess(w, nil)
}

// teamsMentions 把要 @ 的 Lark ID 换成 Teams 提及。
//
// 通讯录里找不到这个 Lark ID 的（人被删了、ID 改了），名字未知，在卡片上按「未配置」列出
// ID 的末几位，让人能对上是谁。
func teamsMentions(atLarkIDs []string) []services.TeamsMention {
	ids := dedupNonEmpty(atLarkIDs)
	if len(ids) == 0 {
		return nil
	}
	domain := teamsMentionDomain()
	type contact struct{ name, teams string }
	byLark := map[string]contact{}
	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",")
	args := make([]interface{}, len(ids))
	for i, id := range ids {
		args[i] = id
	}
	rows, err := database.DB.Query(`SELECT lark_id, name, teams_id FROM contact WHERE lark_id IN (`+placeholders+`)`, args...)
	if err == nil {
		for rows.Next() {
			var lark, name, teams string
			if rows.Scan(&lark, &name, &teams) == nil {
				byLark[lark] = contact{name, teams}
			}
		}
		rows.Close()
	} else {
		log.Printf("[teams] 按 Lark ID 查通知人失败（本次 Teams 不 @ 人，通知照发）: %v", err)
	}
	out := make([]services.TeamsMention, 0, len(ids))
	for _, id := range ids {
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
