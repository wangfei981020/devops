package handlers

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"opsplatform-cmdb-backend/logx"
)

// 审计查询 + 变更回滚。
//
//	回滚分三级：
//	  L1 local    纯本地库改动，直接反向写回
//	  L2 external 会写外部系统（Cloudflare/GoDaddy 的 DNS），要二次确认
//	  L3 none     不可回滚（证书签发、同步、token 重置…），按钮置灰并给出原因
//
//	回滚前必须做冲突检测：拿当前行和"当时改完的样子"比对，
//	不一致说明这条记录后来被别人动过——直接回滚就是静默覆盖他人改动。

type AuditHandler struct{ DB *sql.DB }

func NewAuditHandler(db *sql.DB) *AuditHandler { return &AuditHandler{DB: db} }

func (h *AuditHandler) Register(r *gin.RouterGroup) {
	r.GET("/audit-logs", h.List)
	r.GET("/audit-logs/:id", h.Detail)
	r.GET("/audit-logs/:id/changes", h.Changes)
	// 对象维度的变更历史：域名/证书/集群详情页内嵌用
	r.GET("/audit-history", h.ObjectHistory)
	r.POST("/audit-changes/:cid/revert", h.RevertChange)
	r.POST("/audit-logs/:id/revert", h.RevertAll)
}

// List GET /api/audit-logs —— 后端分页 + 多维筛选
func (h *AuditHandler) List(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	if page < 1 {
		page = 1
	}
	size, _ := strconv.Atoi(c.DefaultQuery("page_size", "10"))
	if size < 1 || size > 200 {
		size = 10
	}

	where := []string{"1=1"}
	args := []interface{}{}
	add := func(cond string, v interface{}) { where = append(where, cond); args = append(args, v) }

	if v := c.Query("username"); v != "" {
		add("username = ?", v)
	}
	if v := c.Query("action"); v != "" {
		add("action = ?", v)
	}
	if v := c.Query("action_prefix"); v != "" {
		add("action LIKE ?", v+"%")
	}
	if v := c.Query("target_type"); v != "" {
		add("target_type = ?", v)
	}
	if v := c.Query("status"); v != "" {
		add("status = ?", v)
	}
	if v := c.Query("actor_source"); v != "" {
		add("actor_source = ?", v)
	}
	if v := c.Query("q"); v != "" {
		where = append(where, "(target LIKE ? OR path LIKE ?)")
		args = append(args, "%"+v+"%", "%"+v+"%")
	}
	if v := c.Query("since"); v != "" {
		add("at >= ?", v)
	}
	if v := c.Query("until"); v != "" {
		add("at < ?", v)
	}
	// 只看有变更明细的（能回滚的那些）
	if c.Query("changed_only") == "1" {
		where = append(where, "change_count > 0")
	}
	cond := strings.Join(where, " AND ")

	var total int
	if err := h.DB.QueryRow("SELECT COUNT(*) FROM audit_logs WHERE "+cond, args...).Scan(&total); err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}

	rows, err := h.DB.Query(`SELECT id, username, IFNULL(actor_source,'local'), action,
		IFNULL(target_type,''), IFNULL(target_id,''), IFNULL(target,''), IFNULL(ip,''),
		IFNULL(method,''), IFNULL(path,''), IFNULL(perm_code,''), IFNULL(status,'success'),
		IFNULL(error_msg,''), IFNULL(change_count,0), IFNULL(duration_ms,0), revert_of, at
		FROM audit_logs WHERE `+cond+` ORDER BY id DESC LIMIT ? OFFSET ?`,
		append(args, size, (page-1)*size)...)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	defer rows.Close()

	list := []gin.H{}
	for rows.Next() {
		var id int64
		var user, src, action, ttype, tid, tname, ip, method, path, perm, status, errMsg string
		var cnt, dur int
		var revertOf sql.NullInt64
		var at time.Time
		if err := rows.Scan(&id, &user, &src, &action, &ttype, &tid, &tname, &ip,
			&method, &path, &perm, &status, &errMsg, &cnt, &dur, &revertOf, &at); err != nil {
			continue
		}
		item := gin.H{
			"id": id, "username": user, "actor_source": src, "action": action,
			"target_type": ttype, "target_id": tid, "target": tname, "ip": ip,
			"method": method, "path": path, "perm_code": perm, "status": status,
			"error_msg": errMsg, "change_count": cnt, "duration_ms": dur,
			"at": at.Format("2006-01-02 15:04:05"),
		}
		if revertOf.Valid {
			item["revert_of"] = revertOf.Int64
		}
		list = append(list, item)
	}
	c.JSON(200, gin.H{"total": total, "page": page, "page_size": size, "list": list})
}

// Detail 一条操作流水 + 它的全部变更明细
func (h *AuditHandler) Detail(c *gin.Context) {
	id := c.Param("id")
	var out gin.H
	var uid int64
	var user, action, tname, status string
	var at time.Time
	err := h.DB.QueryRow(`SELECT id, username, action, IFNULL(target,''), IFNULL(status,'success'), at
		FROM audit_logs WHERE id=?`, id).Scan(&uid, &user, &action, &tname, &status, &at)
	if err != nil {
		c.JSON(404, gin.H{"error": "记录不存在"})
		return
	}
	out = gin.H{"id": uid, "username": user, "action": action, "target": tname,
		"status": status, "at": at.Format("2006-01-02 15:04:05")}
	out["changes"] = h.loadChanges(id)
	c.JSON(200, out)
}

// Changes 只取变更明细
func (h *AuditHandler) Changes(c *gin.Context) {
	c.JSON(200, gin.H{"list": h.loadChanges(c.Param("id"))})
}

// loadChanges 读变更明细。
// 注意：**不返回 before_enc / after_enc**——那是加密的完整原文，
// 只在服务端回滚流程里解密使用，任何接口都不该把它送出去。
func (h *AuditHandler) loadChanges(auditID string) []gin.H {
	rows, err := h.DB.Query(`SELECT id, seq, table_name, IFNULL(row_pk,''), op,
		IFNULL(diff_json,'{}'), revert_kind, reverted_at, IFNULL(reverted_by,''),
		before_enc IS NOT NULL AS has_before
		FROM audit_changes WHERE audit_id=? ORDER BY seq`, auditID)
	if err != nil {
		return []gin.H{}
	}
	defer rows.Close()
	out := []gin.H{}
	for rows.Next() {
		var id int64
		var seq int
		var table, pk, op, diffStr, kind, revBy string
		var revAt sql.NullTime
		var hasBefore bool
		if err := rows.Scan(&id, &seq, &table, &pk, &op, &diffStr, &kind, &revAt, &revBy, &hasBefore); err != nil {
			continue
		}
		var diff map[string]interface{}
		_ = json.Unmarshal([]byte(diffStr), &diff)
		item := gin.H{
			"id": id, "seq": seq, "table": table, "row_pk": pk, "op": op,
			"diff": diff, "revert_kind": kind, "revertable": revertable(kind, op, hasBefore),
			"revert_blocked_reason": revertBlockReason(kind, op, hasBefore),
		}
		if revAt.Valid {
			item["reverted_at"] = revAt.Time.Format("2006-01-02 15:04:05")
			item["reverted_by"] = revBy
		}
		out = append(out, item)
	}
	return out
}

// revertable 这条变更能不能回滚
func revertable(kind, op string, hasBefore bool) bool {
	if kind == "none" {
		return false
	}
	// INSERT 的回滚 = 删掉新建的行，不需要 before
	if op == "INSERT" {
		return true
	}
	return hasBefore
}

// revertBlockReason 不能回滚时给出**具体**原因，而不是干巴巴一句"不支持"
func revertBlockReason(kind, op string, hasBefore bool) string {
	if kind == "none" {
		return "这类操作本身不可逆（如证书签发受 CA 速率限制、同步任务、Token 重置后旧值已作废），只能重新执行对应操作"
	}
	if op != "INSERT" && !hasBefore {
		return "没有保存变更前的快照，无法还原（可能是加密不可用，或该接口未登记变更捕获）"
	}
	return ""
}

// ObjectHistory GET /api/audit-history?table=domains&pk=123
// 对象详情页内嵌的「变更历史」——比让人去审计页大海捞针实用得多
func (h *AuditHandler) ObjectHistory(c *gin.Context) {
	table := c.Query("table")
	pk := c.Query("pk")
	if table == "" || pk == "" {
		c.JSON(400, gin.H{"error": "缺少 table 或 pk"})
		return
	}
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "20"))
	if limit < 1 || limit > 100 {
		limit = 20
	}
	rows, err := h.DB.Query(`SELECT ch.id, ch.op, IFNULL(ch.diff_json,'{}'), ch.revert_kind,
		ch.reverted_at, l.id, l.username, l.action, l.at
		FROM audit_changes ch JOIN audit_logs l ON l.id = ch.audit_id
		WHERE ch.table_name=? AND ch.row_pk=? ORDER BY ch.id DESC LIMIT ?`, table, pk, limit)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	defer rows.Close()
	out := []gin.H{}
	for rows.Next() {
		var cid, aid int64
		var op, diffStr, kind, user, action string
		var revAt sql.NullTime
		var at time.Time
		if err := rows.Scan(&cid, &op, &diffStr, &kind, &revAt, &aid, &user, &action, &at); err != nil {
			continue
		}
		var diff map[string]interface{}
		_ = json.Unmarshal([]byte(diffStr), &diff)
		item := gin.H{"change_id": cid, "audit_id": aid, "op": op, "diff": diff,
			"revert_kind": kind, "username": user, "action": action,
			"at": at.Format("2006-01-02 15:04:05")}
		if revAt.Valid {
			item["reverted_at"] = revAt.Time.Format("2006-01-02 15:04:05")
		}
		out = append(out, item)
	}
	c.JSON(200, gin.H{"list": out})
}

// ── 回滚 ────────────────────────────────────────────────────────────

type changeRow struct {
	ID         int64
	AuditID    int64
	Table      string
	PK         string
	Op         string
	BeforeEnc  string
	AfterEnc   string
	RevertKind string
	RevertedAt sql.NullTime
}

func (h *AuditHandler) loadChange(cid string) (*changeRow, error) {
	var r changeRow
	var bEnc, aEnc sql.NullString
	err := h.DB.QueryRow(`SELECT id, audit_id, table_name, IFNULL(row_pk,''), op,
		before_enc, after_enc, revert_kind, reverted_at FROM audit_changes WHERE id=?`, cid).
		Scan(&r.ID, &r.AuditID, &r.Table, &r.PK, &r.Op, &bEnc, &aEnc, &r.RevertKind, &r.RevertedAt)
	if err != nil {
		return nil, err
	}
	r.BeforeEnc, r.AfterEnc = bEnc.String, aEnc.String
	return &r, nil
}

// RevertChange POST /api/audit-changes/:cid/revert
//
//	body: {"force": false}
//	force=true 表示"我知道这条记录后来被别人改过，仍然要覆盖"，会额外记审计。
func (h *AuditHandler) RevertChange(c *gin.Context) {
	var body struct {
		Force bool `json:"force"`
	}
	_ = c.ShouldBindJSON(&body)

	ch, err := h.loadChange(c.Param("cid"))
	if err != nil {
		c.JSON(404, gin.H{"error": "变更记录不存在"})
		return
	}
	if ch.RevertedAt.Valid {
		c.JSON(409, gin.H{"error": "这条变更已经回滚过了"})
		return
	}
	if ch.RevertKind == "none" {
		c.JSON(400, gin.H{"error": revertBlockReason("none", ch.Op, false)})
		return
	}
	if ch.RevertKind == "external" {
		// 写外部系统的回滚：本期只在界面上给出"要向厂商发起哪些写入"的说明，
		// 由人去对应页面执行。自动反向调用 DNS API 需要把注册商适配器的写路径
		// 也接进来，且一旦中途失败会留下"一半回滚"的状态，风险高于收益。
		c.JSON(400, gin.H{
			"error":  "这条变更会改动外部系统（DNS 解析），暂不支持一键回滚",
			"hint":   "请到「DNS 记录」页按变更详情里的原值手动改回，改完这条会自动留下新的审计记录",
			"detail": gin.H{"table": ch.Table, "row_pk": ch.PK, "op": ch.Op},
		})
		return
	}

	before, errB := decRow(ch.BeforeEnc)
	after, _ := decRow(ch.AfterEnc)

	// ── 冲突检测 ──
	// 拿当前行和"当时改完的样子"比：不一致说明后来被别人动过。
	// 没有这一步，回滚就是个静默覆盖他人改动的工具。
	if ch.Op != "INSERT" || after != nil {
		cur := snapshotRow(h.DB, ch.Table, auditPKCol(ch.Table), ch.PK)
		if conflict, fields := detectConflict(after, cur); conflict && !body.Force {
			who, when := h.lastToucher(ch.Table, ch.PK, ch.ID)
			c.JSON(409, gin.H{
				"error":           "该记录在这次变更之后又被改过，回滚会覆盖那些改动",
				"conflict":        true,
				"fields":          fields,
				"last_changed_by": who, "last_changed_at": when,
				"hint": "确认无误可以强制回滚，强制回滚同样会留下审计记录",
			})
			return
		}
	}

	pkCol := auditPKCol(ch.Table)
	var opDone string
	switch ch.Op {
	case "INSERT":
		// 回滚新建 = 删掉这一行
		if _, err := h.DB.Exec(fmt.Sprintf("DELETE FROM `%s` WHERE `%s`=?", ch.Table, pkCol), ch.PK); err != nil {
			c.JSON(500, gin.H{"error": "回滚失败：" + err.Error()})
			return
		}
		opDone = "DELETE"
	case "DELETE":
		// 回滚删除 = 把整行按 before 重新插回去
		if errB != nil || before == nil {
			c.JSON(400, gin.H{"error": "没有变更前快照，无法恢复被删除的记录"})
			return
		}
		if err := insertRow(h.DB, ch.Table, before); err != nil {
			c.JSON(500, gin.H{"error": "恢复失败：" + err.Error()})
			return
		}
		opDone = "INSERT"
	default: // UPDATE
		if errB != nil || before == nil {
			c.JSON(400, gin.H{"error": "没有变更前快照，无法还原"})
			return
		}
		if err := updateRow(h.DB, ch.Table, pkCol, ch.PK, before); err != nil {
			c.JSON(500, gin.H{"error": "还原失败：" + err.Error()})
			return
		}
		opDone = "UPDATE"
	}

	// 回滚本身也是一次变更：记新的审计 + 标记原记录已回滚，于是回滚还能再被回滚
	newAuditID := h.writeRevertAudit(c, ch, opDone, body.Force)
	h.DB.Exec(`UPDATE audit_changes SET reverted_at=NOW(), reverted_by=?, revert_audit_id=? WHERE id=?`,
		UsernameFromCtx(c), newAuditID, ch.ID)

	logx.Line("audit", fmt.Sprintf("revert ok change=%d table=%s pk=%s by=%s force=%v",
		ch.ID, ch.Table, ch.PK, UsernameFromCtx(c), body.Force))
	c.JSON(200, gin.H{"ok": true, "revert_audit_id": newAuditID, "op": opDone})
}

// RevertAll 回滚一次操作里的所有变更（逐条走同样的检查）
func (h *AuditHandler) RevertAll(c *gin.Context) {
	rows, err := h.DB.Query(`SELECT id FROM audit_changes WHERE audit_id=? AND reverted_at IS NULL
		AND revert_kind='local' ORDER BY seq DESC`, c.Param("id"))
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	var ids []string
	for rows.Next() {
		var id int64
		rows.Scan(&id)
		ids = append(ids, fmt.Sprint(id))
	}
	rows.Close()
	if len(ids) == 0 {
		c.JSON(400, gin.H{"error": "这次操作没有可回滚的本地变更"})
		return
	}
	c.JSON(200, gin.H{"ok": true, "change_ids": ids,
		"hint": "请逐条确认后回滚——批量回滚会跳过冲突检测，风险太高"})
}

// detectConflict 当前行是否已偏离"当时改完的样子"。
// 只比对当时确实写入过的字段：updated_at 这类每次都变的列不算冲突。
func detectConflict(after, cur map[string]interface{}) (bool, []string) {
	if after == nil || cur == nil {
		return false, nil
	}
	var diff []string
	for k, av := range after {
		if k == "updated_at" || k == "created_at" || k == "last_sync_at" {
			continue
		}
		cv, ok := cur[k]
		if !ok {
			continue
		}
		if fmt.Sprint(av) != fmt.Sprint(cv) {
			diff = append(diff, k)
		}
	}
	sort.Strings(diff)
	return len(diff) > 0, diff
}

// lastToucher 这一行最近一次是谁改的（给冲突提示用）
func (h *AuditHandler) lastToucher(table, pk string, afterChangeID int64) (string, string) {
	var user string
	var at time.Time
	err := h.DB.QueryRow(`SELECT l.username, l.at FROM audit_changes ch
		JOIN audit_logs l ON l.id=ch.audit_id
		WHERE ch.table_name=? AND ch.row_pk=? AND ch.id>? ORDER BY ch.id DESC LIMIT 1`,
		table, pk, afterChangeID).Scan(&user, &at)
	if err != nil {
		return "", ""
	}
	return user, at.Format("2006-01-02 15:04:05")
}

func (h *AuditHandler) writeRevertAudit(c *gin.Context, ch *changeRow, op string, forced bool) int64 {
	var origAction string
	h.DB.QueryRow(`SELECT action FROM audit_logs WHERE id=?`, ch.AuditID).Scan(&origAction)
	target := fmt.Sprintf("%s#%s", ch.Table, ch.PK)
	if forced {
		target += "（强制）"
	}
	src, _ := c.Get(ctxAuthSource)
	srcStr, _ := src.(string)
	res, err := h.DB.Exec(`INSERT INTO audit_logs
		(username, action, target, ip, actor_source, target_type, target_id, method, path,
		 status, change_count, revert_of)
		VALUES (?,?,?,?,?,?,?,?,?,'success',1,?)`,
		UsernameFromCtx(c), origAction+".revert", target, c.ClientIP(), srcStr,
		ch.Table, ch.PK, c.Request.Method, c.FullPath(), ch.AuditID)
	if err != nil {
		logx.Line("audit", fmt.Sprintf("WARN 写回滚审计失败: %v", err))
		return 0
	}
	id, _ := res.LastInsertId()
	// 回滚动作自身的 before/after 也存下来，于是"回滚的回滚"同样可行
	cur := snapshotRow(h.DB, ch.Table, auditPKCol(ch.Table), ch.PK)
	before, _ := decRow(ch.AfterEnc)
	diff, _ := json.Marshal(auditDiff(before, cur))
	h.DB.Exec(`INSERT INTO audit_changes
		(audit_id, seq, table_name, row_pk, op, before_enc, after_enc, diff_json, revert_kind)
		VALUES (?,0,?,?,?,?,?,?,'local')`,
		id, ch.Table, ch.PK, op, nullIfEmpty(ch.AfterEnc), nullIfEmpty(encRow(cur)), string(diff))
	return id
}

// ── 行写回 ──────────────────────────────────────────────────────────

// insertRow 按快照整行插回去（恢复被删除的记录）
func insertRow(db *sql.DB, table string, row map[string]interface{}) error {
	cols := make([]string, 0, len(row))
	for k := range row {
		cols = append(cols, k)
	}
	sort.Strings(cols)
	ph := make([]string, len(cols))
	args := make([]interface{}, len(cols))
	quoted := make([]string, len(cols))
	for i, c := range cols {
		quoted[i] = "`" + c + "`"
		ph[i] = "?"
		args[i] = row[c]
	}
	q := fmt.Sprintf("INSERT INTO `%s` (%s) VALUES (%s)",
		table, strings.Join(quoted, ","), strings.Join(ph, ","))
	_, err := db.Exec(q, args...)
	return err
}

// updateRow 按快照还原一行。
// 主键列不参与 SET——它是定位条件，不该被改。
func updateRow(db *sql.DB, table, pkCol, pk string, row map[string]interface{}) error {
	cols := make([]string, 0, len(row))
	for k := range row {
		if k == pkCol || k == "created_at" {
			continue
		}
		cols = append(cols, k)
	}
	sort.Strings(cols)
	if len(cols) == 0 {
		return fmt.Errorf("快照里没有可还原的字段")
	}
	sets := make([]string, len(cols))
	args := make([]interface{}, 0, len(cols)+1)
	for i, c := range cols {
		sets[i] = "`" + c + "`=?"
		args = append(args, row[c])
	}
	args = append(args, pk)
	q := fmt.Sprintf("UPDATE `%s` SET %s WHERE `%s`=?", table, strings.Join(sets, ","), pkCol)
	_, err := db.Exec(q, args...)
	return err
}

// StartAuditCleanup 按保留期清理。默认 0 = 永久保留，不删。
//
//	审计表没有删除接口，管理员在 UI 上也删不掉——只有这个任务能删，
//	而且清理动作本身也记一条审计。
func StartAuditCleanup(db *sql.DB) {
	go func() {
		for {
			time.Sleep(24 * time.Hour)
			cleanupAuditOnce(db)
		}
	}()
}

func cleanupAuditOnce(db *sql.DB) {
	get := func(k string) int {
		var v string
		db.QueryRow(`SELECT v FROM settings WHERE k=?`, k).Scan(&v)
		n, _ := strconv.Atoi(v)
		return n
	}
	logDays := get("audit_retention_days")
	changeDays := get("audit_changes_retention_days")

	if changeDays > 0 {
		if res, err := db.Exec(`DELETE FROM audit_changes WHERE created_at < DATE_SUB(NOW(), INTERVAL ? DAY)`, changeDays); err == nil {
			if n, _ := res.RowsAffected(); n > 0 {
				logx.Line("audit", fmt.Sprintf("清理变更明细 %d 条（保留 %d 天）", n, changeDays))
				db.Exec(`INSERT INTO audit_logs (username, action, target, actor_source, status)
					VALUES ('system','audit.cleanup',?,'system','success')`,
					fmt.Sprintf("清理变更明细 %d 条", n))
			}
		}
	}
	if logDays > 0 {
		if res, err := db.Exec(`DELETE FROM audit_logs WHERE at < DATE_SUB(NOW(), INTERVAL ? DAY)
			AND action <> 'audit.cleanup'`, logDays); err == nil {
			if n, _ := res.RowsAffected(); n > 0 {
				logx.Line("audit", fmt.Sprintf("清理操作流水 %d 条（保留 %d 天）", n, logDays))
			}
		}
	}
}

var _ = http.StatusOK
