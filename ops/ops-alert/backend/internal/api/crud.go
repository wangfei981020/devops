package api

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"ops-alert-backend/datasource"
	"ops-alert-backend/internal/api/middleware"
	"ops-alert-backend/internal/store"
	"ops-alert-backend/notify"
)

// 本文件是接入侧四类对象的写接口：数据源、通知渠道、路由、静默。
//
// 共同约定：
//   - 凭据只写不读。auth_enc / config_enc 永远不出现在响应里，连密文都不给。
//   - 删除一律软删（deleted_at），因为事件、投递记录会引用它们；
//     硬删会让历史事件的「发给了谁」变成空白。
//   - 每个写操作都落审计。

func (s *Server) registerCRUD(g *gin.RouterGroup) {
	g.POST("/datasources", s.createDatasource)
	g.PUT("/datasources/:id", s.updateDatasource)
	g.DELETE("/datasources/:id", s.deleteDatasource)
	g.POST("/datasources/:id/test", s.testDatasource)

	g.GET("/notifiers", s.listNotifiers)
	g.POST("/notifiers", s.createNotifier)
	g.DELETE("/notifiers/:id", s.deleteNotifier)
	g.POST("/notifiers/:id/test", s.testNotifier)

	g.GET("/routes", s.listRoutes)
	g.POST("/routes", s.createRoute)
	g.DELETE("/routes/:id", s.deleteRoute)
	g.POST("/routes/simulate", s.simulateRoute)

	g.GET("/silences", s.listSilences)
	g.POST("/silences", s.createSilence)
	g.DELETE("/silences/:id", s.deleteSilence)

	g.GET("/audit", s.listAudit)
}

type datasourceReq struct {
	Name     string            `json:"name"`
	Type     string            `json:"type"`
	Endpoint string            `json:"endpoint"`
	SkipTLS  bool              `json:"skip_tls"`
	Auth     datasource.Auth   `json:"auth"`
	Spec     datasource.Spec   `json:"spec"`
	Labels   map[string]string `json:"labels,omitempty"`
}

func (s *Server) createDatasource(c *gin.Context) {
	sc, err := s.st.Tenant(c.Request.Context())
	if err != nil {
		abortTenant(c, err)
		return
	}
	var req datasourceReq
	if err := c.ShouldBindJSON(&req); err != nil || req.Name == "" || req.Endpoint == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "bad_request"})
		return
	}
	// 类型必须是已实现的：让配置错误在保存时就暴露，
	// 而不是等到第一个周期跑起来才发现"这个类型没人处理"。
	if _, err := datasource.New(datasource.Config{Kind: req.Type, Endpoint: req.Endpoint}); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "unsupported_type", "detail": err.Error()})
		return
	}
	authEnc, err := s.encryptJSON(req.Auth)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "encrypt_failed"})
		return
	}
	specBlob, _ := json.Marshal(req.Spec)
	res, err := sc.Insert(`INSERT INTO datasources (tenant_id, name, type, endpoint, auth_enc, spec, skip_tls)
		VALUES (?, ?, ?, ?, ?, ?, ?)`, req.Name, req.Type, req.Endpoint, authEnc, specBlob, boolToInt(req.SkipTLS))
	if err != nil {
		abortQuery(c, err)
		return
	}
	id, _ := res.LastInsertId()
	s.audit(c, sc, "datasource.create", "datasource", id, gin.H{"name": req.Name, "type": req.Type})
	c.JSON(http.StatusOK, gin.H{"id": id})
}

func (s *Server) updateDatasource(c *gin.Context) {
	sc, err := s.st.Tenant(c.Request.Context())
	if err != nil {
		abortTenant(c, err)
		return
	}
	id, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	var req datasourceReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "bad_request"})
		return
	}
	specBlob, _ := json.Marshal(req.Spec)
	// 空凭据 = 不改凭据。否则「只改个名字」会把密码清空，
	// 而现象是几分钟后数据源突然连不上，没人会联想到刚才的改名。
	if req.Auth == (datasource.Auth{}) {
		_, err = sc.Exec(`UPDATE datasources SET name=?, endpoint=?, spec=?, skip_tls=?
			WHERE tenant_id = ? AND id = ?`, req.Name, req.Endpoint, specBlob, boolToInt(req.SkipTLS), id)
	} else {
		var authEnc string
		authEnc, err = s.encryptJSON(req.Auth)
		if err == nil {
			_, err = sc.Exec(`UPDATE datasources SET name=?, endpoint=?, auth_enc=?, spec=?, skip_tls=?
				WHERE tenant_id = ? AND id = ?`, req.Name, req.Endpoint, authEnc, specBlob, boolToInt(req.SkipTLS), id)
		}
	}
	if err != nil {
		abortQuery(c, err)
		return
	}
	s.audit(c, sc, "datasource.update", "datasource", id, gin.H{"name": req.Name})
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func (s *Server) deleteDatasource(c *gin.Context) {
	sc, err := s.st.Tenant(c.Request.Context())
	if err != nil {
		abortTenant(c, err)
		return
	}
	id, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	// 还有规则挂着就不让删：删掉之后那些规则会每周期报"数据源不存在"，
	// 而值班看到的是一堆莫名其妙的失败，不知道是谁删了什么。
	var used int
	if err := sc.QueryRow(`SELECT COUNT(*) FROM rules
		WHERE tenant_id = ? AND datasource_id = ? AND deleted_at IS NULL`, id).Scan(&used); err != nil {
		abortQuery(c, err)
		return
	}
	if used > 0 {
		c.JSON(http.StatusConflict, gin.H{"error": "in_use", "detail": fmt.Sprintf("还有 %d 条规则在用", used)})
		return
	}
	if _, err := sc.Exec(`UPDATE datasources SET deleted_at = NOW(3)
		WHERE tenant_id = ? AND id = ?`, id); err != nil {
		abortQuery(c, err)
		return
	}
	s.audit(c, sc, "datasource.delete", "datasource", id, nil)
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// testDatasource 探测连通性并把结果落库。
//
// 结果落库而不是只返回给点按钮的人：数据源不可达要在列表里是显性状态，
// 否则只有点过测试的人知道，其他人看到的是"一切正常"。
func (s *Server) testDatasource(c *gin.Context) {
	sc, err := s.st.Tenant(c.Request.Context())
	if err != nil {
		abortTenant(c, err)
		return
	}
	id, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	ad, name, err := s.openDatasource(sc, id)
	if err != nil {
		s.recordProbe(sc, id, "down", 0, err.Error())
		c.JSON(http.StatusOK, gin.H{"ok": false, "error": err.Error()})
		return
	}
	started := time.Now()
	if err := ad.Probe(c.Request.Context()); err != nil {
		s.recordProbe(sc, id, "down", int(time.Since(started).Milliseconds()), err.Error())
		c.JSON(http.StatusOK, gin.H{"ok": false, "name": name, "error": err.Error()})
		return
	}
	ms := int(time.Since(started).Milliseconds())
	s.recordProbe(sc, id, "up", ms, "")
	c.JSON(http.StatusOK, gin.H{"ok": true, "name": name, "latency_ms": ms})
}

func (s *Server) recordProbe(sc *store.Scoped, id int64, status string, ms int, errMsg string) {
	_, _ = sc.Exec(`UPDATE datasources SET status=?, probe_at=NOW(3), probe_ms=?, probe_error=?
		WHERE tenant_id = ? AND id = ?`, status, ms, truncate(errMsg, 500), id)
}

// ── 通知渠道 ───────────────────────────────────────────────────

func (s *Server) listNotifiers(c *gin.Context) {
	sc, err := s.st.Tenant(c.Request.Context())
	if err != nil {
		abortTenant(c, err)
		return
	}
	rows, err := sc.Query(`SELECT id, name, type, status FROM notifiers
		WHERE tenant_id = ? AND deleted_at IS NULL ORDER BY id`)
	if err != nil {
		abortQuery(c, err)
		return
	}
	defer rows.Close()
	out := []gin.H{}
	for rows.Next() {
		var id int64
		var name, typ, status string
		if err := rows.Scan(&id, &name, &typ, &status); err != nil {
			abortQuery(c, err)
			return
		}
		// 能力随响应一起给前端：模板编辑器要据此决定显示哪些字段
		// （不支持 @人 的渠道不该出现 @人 输入框）。
		caps := gin.H{}
		if sender, err := notify.New(typ, []byte(`{"url":"http://x","webhook_url":"http://x"}`)); err == nil {
			ca := sender.Caps()
			caps = gin.H{"rich_card": ca.RichCard, "mention": ca.Mention, "buttons": ca.Buttons, "max_runes": ca.MaxRunes}
		}
		out = append(out, gin.H{"id": id, "name": name, "type": typ, "status": status, "caps": caps})
	}
	c.JSON(http.StatusOK, gin.H{"items": out})
}

func (s *Server) createNotifier(c *gin.Context) {
	sc, err := s.st.Tenant(c.Request.Context())
	if err != nil {
		abortTenant(c, err)
		return
	}
	var req struct {
		Name   string          `json:"name"`
		Type   string          `json:"type"`
		Config json.RawMessage `json:"config"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || req.Name == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "bad_request"})
		return
	}
	// 先构造一次：配置缺字段（比如飞书没填 webhook_url）在保存时就报错，
	// 而不是等到第一次告警发不出去才发现。
	if _, err := notify.New(req.Type, req.Config); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_config", "detail": err.Error()})
		return
	}
	enc, err := s.cipher.Encrypt(string(req.Config))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "encrypt_failed"})
		return
	}
	res, err := sc.Insert(`INSERT INTO notifiers (tenant_id, name, type, config_enc)
		VALUES (?, ?, ?, ?)`, req.Name, req.Type, enc)
	if err != nil {
		abortQuery(c, err)
		return
	}
	id, _ := res.LastInsertId()
	s.audit(c, sc, "notifier.create", "notifier", id, gin.H{"name": req.Name, "type": req.Type})
	c.JSON(http.StatusOK, gin.H{"id": id})
}

func (s *Server) deleteNotifier(c *gin.Context) {
	sc, err := s.st.Tenant(c.Request.Context())
	if err != nil {
		abortTenant(c, err)
		return
	}
	id, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	if _, err := sc.Exec(`UPDATE notifiers SET deleted_at = NOW(3) WHERE tenant_id = ? AND id = ?`, id); err != nil {
		abortQuery(c, err)
		return
	}
	s.audit(c, sc, "notifier.delete", "notifier", id, nil)
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// testNotifier 发一条真实的测试消息。
//
// 不做"假装成功"：这个按钮的全部价值就是确认对端真能收到。
func (s *Server) testNotifier(c *gin.Context) {
	sc, err := s.st.Tenant(c.Request.Context())
	if err != nil {
		abortTenant(c, err)
		return
	}
	id, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	var typ string
	var enc sql.NullString
	if err := sc.QueryRow(`SELECT type, config_enc FROM notifiers
		WHERE tenant_id = ? AND id = ? AND deleted_at IS NULL`, id).Scan(&typ, &enc); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "not_found"})
		return
	}
	plain := ""
	if enc.Valid && enc.String != "" {
		plain, err = s.cipher.Decrypt(enc.String)
		if err != nil {
			c.JSON(http.StatusOK, gin.H{"ok": false, "error": "配置解密失败（AES 密钥是否变更？）"})
			return
		}
	}
	sender, err := notify.New(typ, []byte(plain))
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"ok": false, "error": err.Error()})
		return
	}
	err = sender.Send(c.Request.Context(), notify.Message{
		Title:    "OpsAlert 测试消息",
		Severity: "info",
		Fields:   []notify.Field{{Key: "来源", Value: "渠道连通性测试"}},
	})
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"ok": false, "error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// ── 路由 ───────────────────────────────────────────────────────

func (s *Server) listRoutes(c *gin.Context) {
	sc, err := s.st.Tenant(c.Request.Context())
	if err != nil {
		abortTenant(c, err)
		return
	}
	rows, err := sc.Query(`SELECT id, name, matchers, continue_on, notifier_ids, group_wait,
			repeat_sec, escalation, is_fallback, sort_order
		FROM routes WHERE tenant_id = ? AND deleted_at IS NULL
		ORDER BY is_fallback ASC, sort_order ASC, id ASC`)
	if err != nil {
		abortQuery(c, err)
		return
	}
	defer rows.Close()
	out := []gin.H{}
	for rows.Next() {
		var id int64
		var name string
		var matchers, notifiers, escalation []byte
		var cont, groupWait, repeatSec, isFallback, sortOrder int
		if err := rows.Scan(&id, &name, &matchers, &cont, &notifiers, &groupWait,
			&repeatSec, &escalation, &isFallback, &sortOrder); err != nil {
			abortQuery(c, err)
			return
		}
		out = append(out, gin.H{
			"id": id, "name": name, "matchers": json.RawMessage(matchers),
			"continue": cont == 1, "notifier_ids": json.RawMessage(notifiers),
			"group_wait": groupWait, "repeat_sec": repeatSec,
			"escalation":  rawOrNull(escalation),
			"is_fallback": isFallback == 1, "sort_order": sortOrder,
		})
	}
	c.JSON(http.StatusOK, gin.H{"items": out})
}

func (s *Server) createRoute(c *gin.Context) {
	sc, err := s.st.Tenant(c.Request.Context())
	if err != nil {
		abortTenant(c, err)
		return
	}
	var req struct {
		Name        string          `json:"name"`
		Matchers    json.RawMessage `json:"matchers"`
		Continue    bool            `json:"continue"`
		NotifierIDs []int64         `json:"notifier_ids"`
		RepeatSec   int             `json:"repeat_sec"`
		Escalation  json.RawMessage `json:"escalation"`
		SortOrder   int             `json:"sort_order"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || req.Name == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "bad_request"})
		return
	}
	if len(req.NotifierIDs) == 0 {
		// 没有渠道的路由 = 事件走到这里就消失了。
		c.JSON(http.StatusBadRequest, gin.H{"error": "no_notifier", "detail": "路由必须至少绑定一个通知渠道"})
		return
	}
	if len(req.Matchers) == 0 {
		req.Matchers = json.RawMessage("[]")
	}
	ids, _ := json.Marshal(req.NotifierIDs)
	var esc any
	if len(req.Escalation) > 0 {
		esc = []byte(req.Escalation)
	}
	res, err := sc.Insert(`INSERT INTO routes (tenant_id, name, matchers, continue_on, notifier_ids,
			repeat_sec, escalation, sort_order)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		req.Name, []byte(req.Matchers), boolToInt(req.Continue), ids,
		defaultInt(req.RepeatSec, 14400), esc, req.SortOrder)
	if err != nil {
		abortQuery(c, err)
		return
	}
	id, _ := res.LastInsertId()
	s.audit(c, sc, "route.create", "route", id, gin.H{"name": req.Name})
	c.JSON(http.StatusOK, gin.H{"id": id})
}

func (s *Server) deleteRoute(c *gin.Context) {
	sc, err := s.st.Tenant(c.Request.Context())
	if err != nil {
		abortTenant(c, err)
		return
	}
	id, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	// 兜底路由不可删：没有兜底时，未匹配任何条件的事件会静默消失，
	// 而这件事没有任何地方会报错。
	var isFallback int
	if err := sc.QueryRow(`SELECT is_fallback FROM routes WHERE tenant_id = ? AND id = ?`, id).
		Scan(&isFallback); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "not_found"})
		return
	}
	if isFallback == 1 {
		c.JSON(http.StatusConflict, gin.H{"error": "fallback_protected",
			"detail": "兜底路由不可删除：删掉之后未匹配的事件会静默消失"})
		return
	}
	if _, err := sc.Exec(`UPDATE routes SET deleted_at = NOW(3) WHERE tenant_id = ? AND id = ?`, id); err != nil {
		abortQuery(c, err)
		return
	}
	s.audit(c, sc, "route.delete", "route", id, nil)
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// simulateRoute 路由试算：给一组标签，看它会走到哪条分支、发给谁。
//
// 开源方案里配错路由只能等下一次真实告警才发现。
func (s *Server) simulateRoute(c *gin.Context) {
	sc, err := s.st.Tenant(c.Request.Context())
	if err != nil {
		abortTenant(c, err)
		return
	}
	var req struct {
		Labels map[string]string `json:"labels"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "bad_request"})
		return
	}
	m := s.eng.MatchRoutePublic(sc, req.Labels)
	names := []string{}
	for _, id := range m.NotifierIDs {
		var n string
		if err := sc.QueryRow(`SELECT name FROM notifiers WHERE tenant_id = ? AND id = ?`, id).Scan(&n); err == nil {
			names = append(names, n)
		}
	}
	c.JSON(http.StatusOK, gin.H{
		"route_id": m.ID, "reason": m.Reason, "notifiers": names,
		"repeat_sec": m.RepeatSec, "will_notify": len(m.NotifierIDs) > 0,
	})
}

// ── 静默 ───────────────────────────────────────────────────────

func (s *Server) listSilences(c *gin.Context) {
	sc, err := s.st.Tenant(c.Request.Context())
	if err != nil {
		abortTenant(c, err)
		return
	}
	rows, err := sc.Query(`SELECT id, kind, matchers, comment, starts_at, ends_at,
			renew_count, created_by, (ends_at > NOW(3) AND starts_at <= NOW(3)) AS active
		FROM silences WHERE tenant_id = ? AND deleted_at IS NULL ORDER BY ends_at DESC`)
	if err != nil {
		abortQuery(c, err)
		return
	}
	defer rows.Close()
	out := []gin.H{}
	for rows.Next() {
		var id int64
		var kind, comment, createdBy string
		var matchers []byte
		var starts, ends time.Time
		var renew, active int
		if err := rows.Scan(&id, &kind, &matchers, &comment, &starts, &ends, &renew, &createdBy, &active); err != nil {
			abortQuery(c, err)
			return
		}
		out = append(out, gin.H{
			"id": id, "kind": kind, "matchers": json.RawMessage(matchers), "comment": comment,
			"starts_at": starts, "ends_at": ends, "renew_count": renew,
			"created_by": createdBy, "active": active == 1,
			// 反复续期是「假装没问题」的最常见方式，标出来让治理页能盯住。
			"suspicious": renew >= 3,
		})
	}
	c.JSON(http.StatusOK, gin.H{"items": out})
}

func (s *Server) createSilence(c *gin.Context) {
	sc, err := s.st.Tenant(c.Request.Context())
	if err != nil {
		abortTenant(c, err)
		return
	}
	var req struct {
		Kind     string          `json:"kind"`
		Matchers json.RawMessage `json:"matchers"`
		Comment  string          `json:"comment"`
		Hours    int             `json:"hours"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "bad_request"})
		return
	}
	if req.Hours <= 0 {
		// 静默必须会过期。永久静默 = 假装没问题，而且没人会回来关掉它。
		c.JSON(http.StatusBadRequest, gin.H{"error": "no_end_time", "detail": "静默必须设置结束时间"})
		return
	}
	if len(req.Matchers) == 0 || string(req.Matchers) == "[]" {
		// 空条件会静默所有告警——这是最危险的一次点击。
		c.JSON(http.StatusBadRequest, gin.H{"error": "empty_matchers",
			"detail": "静默条件不能为空，否则会静默全部告警"})
		return
	}
	kind := req.Kind
	if kind == "" {
		kind = "silence"
	}
	user := middleware.CurrentUser(c)
	res, err := sc.Insert(`INSERT INTO silences (tenant_id, kind, matchers, comment, starts_at, ends_at, created_by)
		VALUES (?, ?, ?, ?, NOW(3), DATE_ADD(NOW(3), INTERVAL ? HOUR), ?)`,
		kind, []byte(req.Matchers), req.Comment, req.Hours, user.Username)
	if err != nil {
		abortQuery(c, err)
		return
	}
	id, _ := res.LastInsertId()
	s.audit(c, sc, "silence.create", "silence", id, gin.H{"hours": req.Hours, "comment": req.Comment})
	c.JSON(http.StatusOK, gin.H{"id": id})
}

func (s *Server) deleteSilence(c *gin.Context) {
	sc, err := s.st.Tenant(c.Request.Context())
	if err != nil {
		abortTenant(c, err)
		return
	}
	id, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	if _, err := sc.Exec(`UPDATE silences SET deleted_at = NOW(3) WHERE tenant_id = ? AND id = ?`, id); err != nil {
		abortQuery(c, err)
		return
	}
	s.audit(c, sc, "silence.delete", "silence", id, nil)
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// ── 审计 ───────────────────────────────────────────────────────

func (s *Server) listAudit(c *gin.Context) {
	sc, err := s.st.Tenant(c.Request.Context())
	if err != nil {
		abortTenant(c, err)
		return
	}
	rows, err := sc.Query(`SELECT actor, action, target_type, target_id, detail, ip, created_at
		FROM audit_logs WHERE tenant_id = ? ORDER BY id DESC LIMIT 200`)
	if err != nil {
		abortQuery(c, err)
		return
	}
	defer rows.Close()
	out := []gin.H{}
	for rows.Next() {
		var actor, action, tType, tID, ip string
		var detail []byte
		var at time.Time
		if err := rows.Scan(&actor, &action, &tType, &tID, &detail, &ip, &at); err != nil {
			abortQuery(c, err)
			return
		}
		out = append(out, gin.H{"actor": actor, "action": action, "target_type": tType,
			"target_id": tID, "detail": rawOrNull(detail), "ip": ip, "at": at})
	}
	c.JSON(http.StatusOK, gin.H{"items": out})
}

func (s *Server) audit(c *gin.Context, sc *store.Scoped, action, targetType string, targetID int64, detail any) {
	blob, _ := json.Marshal(detail)
	user := middleware.CurrentUser(c)
	if _, err := sc.Insert(`INSERT INTO audit_logs (tenant_id, actor, action, target_type, target_id, detail, ip)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		user.Username, action, targetType, strconv.FormatInt(targetID, 10), blob, c.ClientIP()); err != nil {
		// 审计写失败不该让业务操作回滚，但必须留下日志——
		// 审计有缺口本身就是需要被发现的事。
		abortLog(c, err)
	}
}

func (s *Server) encryptJSON(v any) (string, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	return s.cipher.Encrypt(string(b))
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func defaultInt(v, def int) int {
	if v <= 0 {
		return def
	}
	return v
}

func rawOrNull(b []byte) any {
	if len(b) == 0 {
		return nil
	}
	return json.RawMessage(b)
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}

// jsonOrNull 把 map 编码成 JSON 列的值。
//
// ⚠️ 空 map 存 NULL 而不是 `{}`。两者在 SQL 里能区分，
// 而"没配过标签"和"配过但清空了"在语义上确实不同 ——
// 后者以后可能要用来表达"显式覆盖成空"。
func jsonOrNull(m map[string]string) any {
	if len(m) == 0 {
		return nil
	}
	b, err := json.Marshal(m)
	if err != nil {
		return nil
	}
	return b
}
