package api

import (
	"database/sql"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"ops-alert-backend/internal/api/middleware"
)

type msgTemplateReq struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	ChannelType string `json:"channel_type"`
	TitleTmpl   string `json:"title_tmpl"`
	BodyTmpl    string `json:"body_tmpl"`
}

func (s *Server) listMsgTemplates(c *gin.Context) {
	sc, err := s.st.Tenant(c.Request.Context())
	if err != nil {
		abortTenant(c, err)
		return
	}
	rows, err := sc.Query(`SELECT id, name, description, channel_type, title_tmpl, body_tmpl,
			is_builtin, created_by, updated_at
		FROM message_templates WHERE tenant_id = ? AND deleted_at IS NULL
		ORDER BY is_builtin DESC, name`)
	if err != nil {
		abortQuery(c, err)
		return
	}
	defer rows.Close()
	items := []gin.H{}
	for rows.Next() {
		var id int64
		var name, desc, ch, title, body, by string
		var builtin int
		var updated sql.NullTime
		if err := rows.Scan(&id, &name, &desc, &ch, &title, &body, &builtin, &by, &updated); err != nil {
			abortQuery(c, err)
			return
		}
		items = append(items, gin.H{
			"id": id, "name": name, "description": desc, "channel_type": ch,
			"title_tmpl": title, "body_tmpl": body,
			"is_builtin": builtin == 1, "created_by": by,
			"updated_at": func() any {
				if updated.Valid {
					return updated.Time
				}
				return nil
			}(),
		})
	}
	// 可用变量清单与渲染实现出自同一处（engine.BuiltinVarNames），
	// 各写一份必然出现"文档里有、实际是 (无此变量)"
	vars := []gin.H{}
	for _, v := range s.eng.TemplateVarNames() {
		vars = append(vars, gin.H{"name": v[0], "desc": v[1]})
	}
	c.JSON(http.StatusOK, gin.H{"items": items, "variables": vars})
}

func (s *Server) createMsgTemplate(c *gin.Context) {
	sc, err := s.st.Tenant(c.Request.Context())
	if err != nil {
		abortTenant(c, err)
		return
	}
	var req msgTemplateReq
	if err := c.ShouldBindJSON(&req); err != nil || req.Name == "" || req.BodyTmpl == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "bad_request", "detail": "名称和正文模板都不能为空"})
		return
	}
	user := middleware.CurrentUser(c)
	res, err := sc.Insert(`INSERT INTO message_templates
		(tenant_id, name, description, channel_type, title_tmpl, body_tmpl, created_by)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		req.Name, req.Description, req.ChannelType, req.TitleTmpl, req.BodyTmpl, user.Username)
	if err != nil {
		abortQuery(c, err)
		return
	}
	id, _ := res.LastInsertId()
	s.audit(c, sc, "msg_template.create", "msg_template", id, gin.H{"name": req.Name})
	c.JSON(http.StatusOK, gin.H{"id": id})
}

func (s *Server) updateMsgTemplate(c *gin.Context) {
	sc, err := s.st.Tenant(c.Request.Context())
	if err != nil {
		abortTenant(c, err)
		return
	}
	id, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	var req msgTemplateReq
	if err := c.ShouldBindJSON(&req); err != nil || req.Name == "" || req.BodyTmpl == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "bad_request", "detail": "名称和正文模板都不能为空"})
		return
	}
	// ⚠️ 内置模板不能改。它是渲染失败时的兜底 ——
	// 允许改的话，兜底可能回落到一个同样坏掉的模板上，
	// 那时整条通知链路没有任何一处是可靠的。
	res, err := sc.Exec(`UPDATE message_templates
		SET name = ?, description = ?, channel_type = ?, title_tmpl = ?, body_tmpl = ?
		WHERE tenant_id = ? AND id = ? AND deleted_at IS NULL AND is_builtin = 0`,
		req.Name, req.Description, req.ChannelType, req.TitleTmpl, req.BodyTmpl, id)
	if err != nil {
		abortQuery(c, err)
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		// 0 行有两种原因：不存在 / 是内置的。分开说 ——
		// 合并成"更新失败"会让人反复点保存
		if builtinExists(sc, id) {
			c.JSON(http.StatusBadRequest, gin.H{
				"error":  "builtin_readonly",
				"detail": "内置模板不能修改（它是渲染失败时的兜底）。请复制一份再改。"})
			return
		}
		c.JSON(http.StatusNotFound, gin.H{"error": "not_found"})
		return
	}
	s.audit(c, sc, "msg_template.update", "msg_template", id, gin.H{"name": req.Name})
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func (s *Server) deleteMsgTemplate(c *gin.Context) {
	sc, err := s.st.Tenant(c.Request.Context())
	if err != nil {
		abortTenant(c, err)
		return
	}
	id, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	// ⚠️ 还被渠道引用着的模板不能删。
	// 删掉的后果不是报错，而是那个渠道**静默回落到内置文案** ——
	// 用户会以为自己的模板还在生效。
	var used int
	_ = sc.QueryRow(`SELECT COUNT(*) FROM notifiers
		WHERE tenant_id = ? AND template_id = ? AND deleted_at IS NULL`, id).Scan(&used)
	if used > 0 {
		c.JSON(http.StatusConflict, gin.H{
			"error":  "in_use",
			"detail": "还有 " + strconv.Itoa(used) + " 个通知渠道在用这个模板。先把它们改成别的模板再删。"})
		return
	}
	res, err := sc.Exec(`UPDATE message_templates SET deleted_at = NOW(3)
		WHERE tenant_id = ? AND id = ? AND deleted_at IS NULL AND is_builtin = 0`, id)
	if err != nil {
		abortQuery(c, err)
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		if builtinExists(sc, id) {
			c.JSON(http.StatusBadRequest, gin.H{
				"error": "builtin_readonly", "detail": "内置模板不能删除（它是渲染失败时的兜底）"})
			return
		}
		c.JSON(http.StatusNotFound, gin.H{"error": "not_found"})
		return
	}
	s.audit(c, sc, "msg_template.delete", "msg_template", id, nil)
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func builtinExists(sc interface {
	QueryRow(string, ...any) *sql.Row
}, id int64) bool {
	var n int
	_ = sc.QueryRow(`SELECT COUNT(*) FROM message_templates
		WHERE tenant_id = ? AND id = ? AND deleted_at IS NULL AND is_builtin = 1`, id).Scan(&n)
	return n > 0
}

// previewMsgTemplate 拿**真实事件**渲染一次模板。
//
// # 为什么必须能预览
//
// 不能预览的话，改完模板要等下一次真实告警才知道对不对 ——
// 而告警不是想有就有的。等到有的时候，收到的是一条格式错乱的告警，
// 那正是最不该出问题的时刻。
//
// # 为什么用真实事件而不是假数据
//
// 假数据里每个变量都有值，于是"这个变量在我的规则里其实取不到"
// 这类问题在预览里永远看不出来 —— 而它恰恰是最常见的模板错误。
func (s *Server) previewMsgTemplate(c *gin.Context) {
	sc, err := s.st.Tenant(c.Request.Context())
	if err != nil {
		abortTenant(c, err)
		return
	}
	var req struct {
		TitleTmpl  string `json:"title_tmpl"`
		BodyTmpl   string `json:"body_tmpl"`
		IncidentID int64  `json:"incident_id"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "bad_request"})
		return
	}
	title, body, warn, sampleFrom, err := s.eng.PreviewTemplate(sc, req.TitleTmpl, req.BodyTmpl, req.IncidentID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "preview_failed", "detail": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"title": title, "body": body,
		// warn 非空 = 模板里有变量在这条事件上不存在。
		// 它不是错误（告警照发），但必须显示出来
		"warning": warn,
		// 说清楚是拿哪条事件渲染的。不说的话，用户看到 "(未提取到)"
		// 会以为模板写错了，而真相可能只是这条事件本来就没有那个字段
		"sample_from": sampleFrom,
	})
}
