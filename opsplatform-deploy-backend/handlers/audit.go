package handlers

import (
	"encoding/json"
	"log"
	"net/http"
	"net/url"
	"reflect"
	"strconv"
	"strings"

	"opsplatform-deploy-backend/database"
)

// 敏感字段名（部分匹配）—— diff 不输出明文，只标 changed:true
//
//	变更值不能进 audit_log 即便它已被前端"加密保存"过，万一日志被泄露原文
//	会被反推。审计意义上知道"改过密码 / 改过 token"已足够追溯责任。
var sensitiveFieldKeywords = []string{
	"password", "secret", "token", "key", "credential",
	// 排除一些误命中的列
	// AES key 之类的列名不会出现在业务 update 输入里
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

// URL 类字段名（部分匹配）—— 审计日志只保留 scheme://host，路径/参数脱敏
//
//	防 audit 详情泄露 internal hostname / 仓库路径等情报。
//	gitlab_url / harbor_url / lark_default_webhook / minio_endpoint /
//	deploy_center_base_url / list_version_api / argocd_url / agent.url 全在范围。
var urlFieldKeywords = []string{"url", "endpoint", "webhook", "_api"}

func isURLField(name string) bool {
	low := strings.ToLower(name)
	for _, kw := range urlFieldKeywords {
		if strings.Contains(low, kw) {
			return true
		}
	}
	return false
}

// maskURLToHost: http://gitlab.com/owner/repo?x=1 → http://gitlab.com/...
// 非 URL 字符串原样返回；空串返回空。
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

// maskValueIfURL：值是字符串且字段名是 URL 类时，脱敏到 host。
func maskValueIfURL(name string, v interface{}) interface{} {
	if !isURLField(name) {
		return v
	}
	if s, ok := v.(string); ok {
		return maskURLToHost(s)
	}
	return v
}

// auditDiff 比较 before/after 两个 map，返回每个变化字段的 {old, new}
//
//	敏感字段（含 password/secret/token/key/credential）只输出 {"changed": true}
//	未变的字段不出现，输出尽可能精简
//	输入 map 的 key 是字段中文/英文名，建议用 DB 列名一致以便溯源
//	用 reflect.DeepEqual 处理 nil / slice / map 等非平凡类型
func auditDiff(before, after map[string]interface{}) map[string]interface{} {
	out := map[string]interface{}{}
	for k, av := range after {
		bv, present := before[k]
		if present && reflect.DeepEqual(bv, av) {
			continue
		}
		if isSensitiveField(k) {
			out[k] = map[string]interface{}{"changed": true}
		} else {
			out[k] = map[string]interface{}{"old": maskValueIfURL(k, bv), "new": maskValueIfURL(k, av)}
		}
	}
	// before 里有但 after 里没的字段（被清空 / 删除）
	for k, bv := range before {
		if _, ok := after[k]; ok {
			continue
		}
		if isSensitiveField(k) {
			out[k] = map[string]interface{}{"changed": true}
		} else {
			out[k] = map[string]interface{}{"old": maskValueIfURL(k, bv), "new": nil}
		}
	}
	return out
}

// auditDetailFromReq 把请求体序列化为 audit detail（替代过去 nil detail）
//
//	仅列非 nil 字段（PATCH 语义：用户没传的字段不记）
//	敏感字段（password/secret/token/key/credential）值替换为 "***changed***"
//	返回内容会进 audit_log.detail JSON，前端审计页可以直接展开查看"改了哪些字段、新值是什么"
func auditDetailFromReq(req interface{}) map[string]interface{} {
	raw, _ := json.Marshal(req)
	var m map[string]interface{}
	if err := json.Unmarshal(raw, &m); err != nil || m == nil {
		return nil
	}
	for k, v := range m {
		if v == nil {
			delete(m, k)
			continue
		}
		// 字符串空值视为未传（部分 handler 用 *string + omitempty，但有些 handler 不用 ptr，空串当未改）
		if s, ok := v.(string); ok && s == "" {
			delete(m, k)
			continue
		}
		if isSensitiveField(k) {
			m[k] = "***changed***"
		} else {
			m[k] = maskValueIfURL(k, v)
		}
	}
	if len(m) == 0 {
		return nil
	}
	return m
}

// Audit 异步写一条审计日志（失败只 log 不阻塞请求）
// action 命名惯例：<target>.<verb>，如 user.create / project_env.update / lark_bot.test
func Audit(r *http.Request, action, targetType, targetName string, detail map[string]interface{}) {
	username := UsernameFromCtx(r)
	if username == "" {
		username = "system"
	}
	go saveAudit(username, action, targetType, targetName, detail, clientIP(r))
}

// AuditAs 显式指定操作人（用于登录路径：用户还没鉴权过、ctx 没 username）
//
//	失败登录场景：username 取请求体里的 username，让管理员能看到"谁尝试登录失败"
func AuditAs(r *http.Request, username, action, targetType, targetName string, detail map[string]interface{}) {
	if username == "" {
		username = "anonymous"
	}
	go saveAudit(username, action, targetType, targetName, detail, clientIP(r))
}

func saveAudit(username, action, targetType, targetName string, detail map[string]interface{}, ip string) {
	var detailStr string
	if len(detail) > 0 {
		b, _ := json.Marshal(detail)
		detailStr = string(b)
	}
	// auth_source 从 users 表查一次（简单，失败就默认 local）
	var authSource string
	_ = database.DB.QueryRow(`SELECT IFNULL(auth_source,'local') FROM users WHERE username=? LIMIT 1`, username).
		Scan(&authSource)
	if authSource == "" {
		authSource = "local"
	}
	_, err := database.DB.Exec(
		`INSERT INTO audit_log (username, auth_source, action, target_type, target_name, detail, ip)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		username, authSource, action, targetType, targetName, detailStr, ip)
	if err != nil {
		log.Printf("[audit] write failed: %v (action=%s target=%s)", err, action, targetName)
	}
}

// HandleListAuditLogs GET /api/audit-logs  (admin only)
//
//	支持筛选：
//	  ?page=1&page_size=50
//	  &username=cesar              精确匹配操作人
//	  &action=user.create          完整匹配
//	  &action_prefix=user.         前缀匹配（按目标类型筛选所有动作）
//	  &q=keyword                   target_name 模糊匹配
//	  &since=2026-04-28T00:00:00   created_at >= since
//	  &until=2026-04-29T00:00:00   created_at <  until
func HandleListAuditLogs(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	page, _ := strconv.Atoi(q.Get("page"))
	if page < 1 {
		page = 1
	}
	pageSize, _ := strconv.Atoi(q.Get("page_size"))
	if pageSize < 1 || pageSize > 200 {
		pageSize = 50
	}

	where := []string{"1=1"}
	args := []interface{}{}
	if v := q.Get("username"); v != "" {
		where = append(where, "username = ?")
		args = append(args, v)
	}
	if v := q.Get("action"); v != "" {
		where = append(where, "action = ?")
		args = append(args, v)
	}
	if v := q.Get("action_prefix"); v != "" {
		where = append(where, "action LIKE ?")
		args = append(args, v+"%")
	}
	if v := q.Get("q"); v != "" {
		where = append(where, "(target_name LIKE ? OR detail LIKE ?)")
		args = append(args, "%"+v+"%", "%"+v+"%")
	}
	if v := q.Get("since"); v != "" {
		where = append(where, "created_at >= ?")
		args = append(args, v)
	}
	if v := q.Get("until"); v != "" {
		where = append(where, "created_at < ?")
		args = append(args, v)
	}
	whereSQL := strings.Join(where, " AND ")

	var total int64
	_ = database.DB.QueryRow(`SELECT COUNT(*) FROM audit_log WHERE `+whereSQL, args...).Scan(&total)

	listArgs := append([]interface{}{}, args...)
	listArgs = append(listArgs, pageSize, (page-1)*pageSize)
	rows, err := database.DB.Query(
		`SELECT id, username, auth_source, action, target_type, target_name, IFNULL(detail,''), ip, created_at
		 FROM audit_log WHERE `+whereSQL+` ORDER BY id DESC LIMIT ? OFFSET ?`,
		listArgs...)
	if err != nil {
		InternalErr(w, r, err)
		return
	}
	defer rows.Close()
	type logItem struct {
		ID         int64  `json:"id"`
		Username   string `json:"username"`
		AuthSource string `json:"auth_source"`
		Action     string `json:"action"`
		TargetType string `json:"target_type"`
		TargetName string `json:"target_name"`
		Detail     string `json:"detail"`
		IP         string `json:"ip"`
		CreatedAt  string `json:"created_at"`
	}
	list := []logItem{}
	for rows.Next() {
		var it logItem
		var createdAt string
		_ = rows.Scan(&it.ID, &it.Username, &it.AuthSource, &it.Action, &it.TargetType, &it.TargetName, &it.Detail, &it.IP, &createdAt)
		it.CreatedAt = createdAt
		list = append(list, it)
	}
	JSONSuccess(w, map[string]interface{}{"total": total, "list": list})
}

// HandleListAuditActionTypes GET /api/audit-logs/action-types
//
//	返回所有出现过的 action 列表（前端做下拉筛选用）
//	按 action 字典序，加上每个 action 的最近一次时间方便排序
func HandleListAuditActionTypes(w http.ResponseWriter, r *http.Request) {
	rows, err := database.DB.Query(
		`SELECT action, COUNT(*) AS cnt FROM audit_log GROUP BY action ORDER BY action`)
	if err != nil {
		InternalErr(w, r, err)
		return
	}
	defer rows.Close()
	type item struct {
		Action string `json:"action"`
		Count  int    `json:"count"`
	}
	out := []item{}
	for rows.Next() {
		var it item
		_ = rows.Scan(&it.Action, &it.Count)
		out = append(out, it)
	}
	JSONSuccess(w, map[string]interface{}{"actions": out})
}
