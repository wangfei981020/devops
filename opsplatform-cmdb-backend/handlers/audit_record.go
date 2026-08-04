package handlers

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/url"
	"reflect"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"opsplatform-cmdb-backend/crypto"
	"opsplatform-cmdb-backend/logx"
)

// 变更追溯：记录每次写操作改了哪张表哪一行、改前改后各是什么。
//
//	三种捕获模式：
//	  A 自动快照 —— 中间件按路由映射知道"这个接口动的是哪张表、主键从哪个参数取"，
//	    请求前后各查一次，自动 diff。覆盖绝大多数单对象 CRUD，handler 零改动。
//	  B 显式记录 —— 批量、跨表、写外部系统的接口，在 handler 里手动
//	    rec.Snap(table, pk) 圈出改动点。
//	  C 动作型 —— sync/test/run 这类没有行变更的，只写主表。
//
//	不记什么：K8s 每 120s 的全量同步、CDN 每 6h 同步、成本快照——这些是
//	"系统观测到的世界变化"，不是"人做的决定"。一次全量同步能产生上万行 diff，
//	一周就把表撑死，而它们本来就有 task_runs 记录和事件中心时间线。

// auditCipher 由 main 注入。没有它就退化成"只记 diff 不存原文"——
// 那样审计还能看，但回滚会因为拿不到原值而不可用。
var auditCipher *crypto.Cipher

// SetAuditCipher 注入加密器（main 启动时调用一次）
func SetAuditCipher(c *crypto.Cipher) { auditCipher = c }

// ── 脱敏 ────────────────────────────────────────────────────────────

// 敏感字段名（子串匹配）。这些字段的值不进 diff_json，只标记"变了"。
// 审计意义上，知道"某人改过这个凭据"已经足够追责；把值也记下来，
// 等于把凭据又抄了一份到另一张表里。
var sensitiveFieldKeywords = []string{
	"password", "passwd", "secret", "token", "credential", "private_key",
	"api_key", "apikey", "access_key", "kubeconfig", "cert_key",
}

func isSensitiveField(name string) bool {
	low := strings.ToLower(name)
	for _, kw := range sensitiveFieldKeywords {
		if strings.Contains(low, kw) {
			return true
		}
	}
	return false
}

// URL 类字段脱敏到 scheme://host：防止审计详情泄露内网地址、仓库路径这类情报
var urlFieldKeywords = []string{"url", "endpoint", "webhook", "_api", "server"}

func isURLField(name string) bool {
	low := strings.ToLower(name)
	for _, kw := range urlFieldKeywords {
		if strings.Contains(low, kw) {
			return true
		}
	}
	return false
}

func maskURLToHost(s string) string {
	if s == "" {
		return ""
	}
	u, err := url.Parse(s)
	if err != nil || u.Host == "" {
		return s
	}
	return u.Scheme + "://" + u.Host + "/..."
}

func maskValue(name string, v interface{}) interface{} {
	if isSensitiveField(name) {
		return "***"
	}
	if isURLField(name) {
		if s, ok := v.(string); ok {
			return maskURLToHost(s)
		}
	}
	return v
}

// auditDiff 比较改动前后，只输出变化了的字段。
//
//	敏感字段输出 {"changed": true}，不带值；
//	未变的字段完全不出现——审计详情要一眼能看出"到底动了什么"，
//	把 40 个没变的字段也列出来等于什么都没说。
func auditDiff(before, after map[string]interface{}) map[string]interface{} {
	out := map[string]interface{}{}
	for k, av := range after {
		bv, present := before[k]
		if present && reflect.DeepEqual(fmt.Sprint(bv), fmt.Sprint(av)) {
			continue
		}
		if isSensitiveField(k) {
			out[k] = map[string]interface{}{"changed": true}
			continue
		}
		out[k] = map[string]interface{}{"old": maskValue(k, bv), "new": maskValue(k, av)}
	}
	// before 有、after 没有的字段（被清空/删除）
	for k, bv := range before {
		if _, ok := after[k]; ok {
			continue
		}
		if isSensitiveField(k) {
			out[k] = map[string]interface{}{"changed": true}
			continue
		}
		out[k] = map[string]interface{}{"old": maskValue(k, bv), "new": nil}
	}
	return out
}

// encRow 把整行加密。加密不可用时返回空串——审计照记，只是这条回滚不了。
func encRow(row map[string]interface{}) string {
	if row == nil || auditCipher == nil {
		return ""
	}
	b, err := json.Marshal(row)
	if err != nil {
		return ""
	}
	enc, err := auditCipher.Encrypt(string(b))
	if err != nil {
		logx.Line("audit", fmt.Sprintf("WARN 加密变更快照失败: %v（该条将无法回滚）", err))
		return ""
	}
	return enc
}

func decRow(enc string) (map[string]interface{}, error) {
	if enc == "" {
		return nil, fmt.Errorf("没有保存变更前快照")
	}
	if auditCipher == nil {
		return nil, fmt.Errorf("加密器不可用")
	}
	plain, err := auditCipher.Decrypt(enc)
	if err != nil {
		return nil, err
	}
	var m map[string]interface{}
	if err := json.Unmarshal([]byte(plain), &m); err != nil {
		return nil, err
	}
	return m, nil
}

// ── 行快照 ──────────────────────────────────────────────────────────

// snapshotRow 读一整行成 map。行不存在返回 nil（不是错误——INSERT 的 before 本来就是空）。
func snapshotRow(db *sql.DB, table, pkCol, pk string) map[string]interface{} {
	if pk == "" {
		return nil
	}
	// 表名和主键列名来自代码里的映射表，不接受用户输入，不存在注入面
	q := fmt.Sprintf("SELECT * FROM `%s` WHERE `%s` = ? LIMIT 1", table, pkCol)
	rows, err := db.Query(q, pk)
	if err != nil {
		logx.Line("audit", fmt.Sprintf("WARN 快照失败 %s.%s=%s: %v", table, pkCol, pk, err))
		return nil
	}
	defer rows.Close()
	cols, err := rows.Columns()
	if err != nil || !rows.Next() {
		return nil
	}
	vals := make([]interface{}, len(cols))
	ptrs := make([]interface{}, len(cols))
	for i := range vals {
		ptrs[i] = &vals[i]
	}
	if err := rows.Scan(ptrs...); err != nil {
		return nil
	}
	out := map[string]interface{}{}
	for i, c := range cols {
		switch v := vals[i].(type) {
		case []byte:
			out[c] = string(v)
		case time.Time:
			out[c] = v.Format("2006-01-02 15:04:05")
		default:
			out[c] = v
		}
	}
	return out
}

// ── Recorder ────────────────────────────────────────────────────────

type pendingChange struct {
	Table      string
	PK         string
	Op         string
	Before     map[string]interface{}
	After      map[string]interface{}
	RevertKind string
}

// Recorder 收集一次请求内的所有行变更，请求结束时统一落库。
type Recorder struct {
	db      *sql.DB
	c       *gin.Context
	changes []pendingChange
	// 自动模式用：请求前拍好的 before，等 handler 跑完再取 after
	autoTable  string
	autoPKCol  string
	autoPK     string
	autoOp     string
	autoBefore map[string]interface{}
	started    time.Time
}

const ctxRecorder = "audit_recorder"

// RecorderFrom 取当前请求的 Recorder，供 handler 做显式埋点（模式 B）。
// 没有就返回 nil，调用方用 rec.Snap 之类方法时要判空。
func RecorderFrom(c *gin.Context) *Recorder {
	if v, ok := c.Get(ctxRecorder); ok {
		if r, ok := v.(*Recorder); ok {
			return r
		}
	}
	return nil
}

// Snap 显式记一次行变更（模式 B）：在改动**之前**调，它会先拍 before，
// 返回一个 done 函数，改完之后调 done() 拍 after。
//
//	用法：
//	  done := rec.Snap("dns_records", "12", "UPDATE", "external")
//	  ... 执行改动 ...
//	  done()
func (r *Recorder) Snap(table, pk, op, revertKind string) func() {
	if r == nil {
		return func() {}
	}
	before := snapshotRow(r.db, table, auditPKCol(table), pk)
	return func() {
		after := snapshotRow(r.db, table, auditPKCol(table), pk)
		r.add(table, pk, op, before, after, revertKind)
	}
}

// SnapDeleted 删除专用：只拍 before（after 必然为空）
func (r *Recorder) SnapDeleted(table, pk string) func() {
	if r == nil {
		return func() {}
	}
	before := snapshotRow(r.db, table, auditPKCol(table), pk)
	return func() { r.add(table, pk, "DELETE", before, nil, "local") }
}

// AddRaw 直接塞一条变更（跨表/非行级改动，比如写外部系统的记录）
func (r *Recorder) AddRaw(table, pk, op string, before, after map[string]interface{}, revertKind string) {
	if r == nil {
		return
	}
	r.add(table, pk, op, before, after, revertKind)
}

func (r *Recorder) add(table, pk, op string, before, after map[string]interface{}, revertKind string) {
	if before == nil && after == nil {
		return // 什么都没查到，不记空账
	}
	if revertKind == "" {
		revertKind = "local"
	}
	r.changes = append(r.changes, pendingChange{
		Table: table, PK: pk, Op: op, Before: before, After: after, RevertKind: revertKind,
	})
}

// ── 写库 ────────────────────────────────────────────────────────────

type auditEntry struct {
	Action     string
	TargetType string
	TargetID   string
	TargetName string
	Status     string
	ErrorMsg   string
	PermCode   string
	RevertOf   sql.NullInt64
}

// flush 落库：主表一条 + 子表 N 条。
// 失败只打日志不影响请求——审计写不进去不该让业务操作跟着失败。
func (r *Recorder) flush(e auditEntry) {
	if r == nil {
		return
	}
	traceID, _ := r.c.Get("request_id")
	tid, _ := traceID.(string)
	src, _ := r.c.Get(ctxAuthSource)
	srcStr, _ := src.(string)
	if srcStr == "" {
		srcStr = "local"
	}

	res, err := r.db.Exec(`INSERT INTO audit_logs
		(username, action, target, ip, trace_id, actor_source, target_type, target_id,
		 method, path, perm_code, status, error_msg, change_count, duration_ms, revert_of)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		UsernameFromCtx(r.c), e.Action, e.TargetName, r.c.ClientIP(), tid, srcStr,
		e.TargetType, e.TargetID, r.c.Request.Method, r.c.FullPath(), e.PermCode,
		e.Status, truncate(e.ErrorMsg, 500), len(r.changes),
		int(time.Since(r.started).Milliseconds()), e.RevertOf)
	if err != nil {
		logx.Line("audit", fmt.Sprintf("WARN 写审计失败 action=%s: %v", e.Action, err))
		return
	}
	auditID, _ := res.LastInsertId()

	for i, ch := range r.changes {
		diff := auditDiff(ch.Before, ch.After)
		diffJSON, _ := json.Marshal(diff)
		_, err := r.db.Exec(`INSERT INTO audit_changes
			(audit_id, seq, table_name, row_pk, op, before_enc, after_enc, diff_json, revert_kind)
			VALUES (?,?,?,?,?,?,?,?,?)`,
			auditID, i, ch.Table, ch.PK, ch.Op,
			nullIfEmpty(encRow(ch.Before)), nullIfEmpty(encRow(ch.After)),
			string(diffJSON), ch.RevertKind)
		if err != nil {
			logx.Line("audit", fmt.Sprintf("WARN 写变更明细失败 %s.%s: %v", ch.Table, ch.PK, err))
		}
	}
}

// logxLine 带格式化的日志封装（logx.Line 只接受成品字符串）
func logxLine(tag, format string, args ...interface{}) {
	logx.Line(tag, fmt.Sprintf(format, args...))
}

// AuditCreated 新建成功后由 handler 调一次，补记这一行的快照。
//
//	自动快照模式对 INSERT 无能为力：新行的主键是在 handler 内部
//	（AUTO_INCREMENT 或业务生成）才产生的，中间件在请求前拿不到它，
//	事后也没法可靠地"猜"出刚插入的是哪一行（并发下更不行）。
//	所以创建类接口需要这一行显式调用，换来的是新建对象同样可追溯、可回滚。
func AuditCreated(c *gin.Context, table string, pk interface{}) {
	rec := RecorderFrom(c)
	if rec == nil {
		return
	}
	pkStr := fmt.Sprint(pk)
	if pkStr == "" || pkStr == "0" {
		return
	}
	after := snapshotRow(rec.db, table, auditPKCol(table), pkStr)
	rec.add(table, pkStr, "INSERT", nil, after, "local")
}

// auditDNSChange 记一条 DNS 解析变更。
//
//	DNS 走这条专用入口而不是通用的行快照：写回厂商成功后本地 dns_records
//	会被整域名删掉重插，行 id 变了，按 id 取快照必然落空。这里记的是
//	domain/type/name/data 这组业务字段——回滚时正是拿它们调 DNS API 写回去。
//	revert_kind 固定 external：回滚会真的改动线上解析，必须二次确认。
func auditDNSChange(c *gin.Context, op, domain, rowID string, before, after map[string]interface{}) {
	rec := RecorderFrom(c)
	if rec == nil {
		return
	}
	rec.add("dns_records", rowID, op, before, after, "external")
}

// SetAuditTarget 由 handler 提供一个人看得懂的操作对象描述，覆盖中间件的自动取名。
//
//	批量操作尤其需要：中间件是从行快照里找 name 字段，而"批量删除 5 条解析"
//	这种操作没有单一对象可取名。原先散在各 handler 里的 WriteAudit 调用
//	正是干这个的，现在改成给中间件供稿——描述信息保留，但不再各记一条流水。
func SetAuditTarget(c *gin.Context, name string) {
	c.Set("audit_target_name", name)
}

func auditTargetOverride(c *gin.Context) string {
	if v, ok := c.Get("audit_target_name"); ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}
