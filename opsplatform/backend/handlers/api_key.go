package handlers

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"

	"opsplatform/database"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
)

// ========== 类型定义 ==========

type APIKey struct {
	ID              string     `json:"id"`
	Name            string     `json:"name"`
	Description     string     `json:"description"`
	KeyPrefix       string     `json:"key_prefix"`
	KeySuffix       string     `json:"key_suffix"`
	Domain          string     `json:"domain"`
	Scopes          []string   `json:"scopes"`
	AllowedTableIDs []string   `json:"allowed_table_ids"`
	Enabled         bool       `json:"enabled"`
	ExpiresAt       *time.Time `json:"expires_at"`
	LastUsedAt      *time.Time `json:"last_used_at"`
	CreatedBy       string     `json:"created_by"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
}

// ========== Key 生成与哈希 ==========

// 完整 key = "opsk_" + 32 位 URL-safe 随机字符（共 37 字符）
// key_prefix = 前 13 位（"opsk_" + 8 位随机），用于列表识别
// key_suffix = 后 6 位
func generateAPIKey() (fullKey, prefix, suffix, hash string, err error) {
	raw := make([]byte, 24)
	if _, err = rand.Read(raw); err != nil {
		return
	}
	body := strings.NewReplacer("+", "a", "/", "b", "=", "c").Replace(base64.StdEncoding.EncodeToString(raw))
	if len(body) < 32 {
		body = body + strings.Repeat("x", 32-len(body))
	}
	body = body[:32]
	fullKey = "opsk_" + body
	prefix = fullKey[:13]
	suffix = fullKey[len(fullKey)-6:]
	h := sha256.Sum256([]byte(fullKey))
	hash = hex.EncodeToString(h[:])
	return
}

func hashAPIKey(fullKey string) string {
	h := sha256.Sum256([]byte(fullKey))
	return hex.EncodeToString(h[:])
}

// ========== last_used_at 节流更新（60s 窗口） ==========

var (
	lastUsedCache = struct {
		sync.Mutex
		m map[string]time.Time
	}{m: make(map[string]time.Time)}
	lastUsedThrottle = 60 * time.Second
)

func touchAPIKeyLastUsed(keyID string) {
	lastUsedCache.Lock()
	now := time.Now()
	if last, ok := lastUsedCache.m[keyID]; ok && now.Sub(last) < lastUsedThrottle {
		lastUsedCache.Unlock()
		return
	}
	lastUsedCache.m[keyID] = now
	lastUsedCache.Unlock()
	database.DB.Exec(`UPDATE api_keys SET last_used_at = ? WHERE id = ?`, now.Format("2006-01-02 15:04:05"), keyID)
}

// ========== 路径权限映射 ==========

// API Key 允许访问的路径 + method → 所需权限码后缀
// 权限码最终为 "<domain>:<suffix>"，例如 table_maintenance:create
var apiKeyRouteMap = []struct {
	Method      string
	PathPattern *regexp.Regexp
	PermSuffix  string
	// 是否会出现 tableID 参数（用于 allowed_table_ids 校验）
	HasTableID bool
}{
	// 桌台维护/自定义表 行数据
	{"POST", regexp.MustCompile(`^/api/custom-tables/([^/]+)/rows$`), "create", true},
	{"PUT", regexp.MustCompile(`^/api/custom-tables/([^/]+)/rows/[^/]+$`), "update", true},
	{"PATCH", regexp.MustCompile(`^/api/custom-tables/([^/]+)/rows/[^/]+$`), "update", true},
	{"DELETE", regexp.MustCompile(`^/api/custom-tables/([^/]+)/rows/[^/]+$`), "delete", true},
	{"GET", regexp.MustCompile(`^/api/custom-tables/([^/]+)$`), "read", true},
	// 桌台层级配置只读（查询合法 projects/sites/tables/gameTypes）
	{"GET", regexp.MustCompile(`^/api/hierarchy$`), "read", false},
	// 存储
	{"POST", regexp.MustCompile(`^/api/storage/upload$`), "upload", false},
	{"POST", regexp.MustCompile(`^/api/storage/presign/batch$`), "read", false},
	{"GET", regexp.MustCompile(`^/api/storage/presign$`), "read", false},
}

// matchAPIKeyRoute 返回：是否匹配、所需权限后缀、路径中的 tableID（可能为空）
func matchAPIKeyRoute(method, path string) (matched bool, permSuffix, tableID string) {
	for _, rt := range apiKeyRouteMap {
		if rt.Method != method {
			continue
		}
		m := rt.PathPattern.FindStringSubmatch(path)
		if m == nil {
			continue
		}
		matched = true
		permSuffix = rt.PermSuffix
		if rt.HasTableID && len(m) > 1 {
			tableID = m[1]
		}
		return
	}
	return
}

// ========== 中间件：API Key 优先，退回 JWT ==========

const (
	ctxAPIKeyID   contextKey = "apiKeyID"
	ctxAPIKeyName contextKey = "apiKeyName"
)

// APIKeyOrJWTMiddleware 包在原 AuthMiddleware 外层：
// - 带 X-API-Key 头 → 走 API Key 验证
// - 否则 → 退回 JWT AuthMiddleware 逻辑
func APIKeyOrJWTMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "OPTIONS" {
			next.ServeHTTP(w, r)
			return
		}

		keyHeader := r.Header.Get("X-API-Key")
		if keyHeader == "" {
			AuthMiddleware(next).ServeHTTP(w, r)
			return
		}

		// 解析 API Key
		key, err := loadAPIKeyByToken(keyHeader)
		if err != nil {
			sendError(w, "无效的 API Key", http.StatusUnauthorized)
			return
		}
		if !key.Enabled {
			sendError(w, "API Key 已禁用", http.StatusUnauthorized)
			return
		}
		if key.ExpiresAt != nil && time.Now().After(*key.ExpiresAt) {
			sendError(w, "API Key 已过期", http.StatusUnauthorized)
			return
		}

		// 路径 / 权限 / tableID 校验
		matched, permSuffix, tableID := matchAPIKeyRoute(r.Method, r.URL.Path)
		if !matched {
			sendError(w, "此接口不支持 API Key 访问", http.StatusForbidden)
			return
		}
		need := key.Domain + ":" + permSuffix
		if !contains(key.Scopes, need) {
			sendError(w, fmt.Sprintf("API Key 缺少权限: %s", need), http.StatusForbidden)
			return
		}
		if tableID != "" && !contains(key.AllowedTableIDs, tableID) {
			sendError(w, "API Key 无权访问该表", http.StatusForbidden)
			return
		}

		// 异步节流更新 last_used_at（通过 touchAPIKeyLastUsed 内部节流）
		touchAPIKeyLastUsed(key.ID)

		// 覆盖 X-Operator，记入审计日志
		r.Header.Set("X-Operator", "apikey:"+key.Name)

		// 把 key 信息挂到 context（便于日志）
		ctx := context.WithValue(r.Context(), ctxAPIKeyID, key.ID)
		ctx = context.WithValue(ctx, ctxAPIKeyName, key.Name)

		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// loadAPIKeyByToken 根据完整 key 查库
func loadAPIKeyByToken(token string) (*APIKey, error) {
	hash := hashAPIKey(token)
	return loadAPIKeyByHash(hash)
}

func loadAPIKeyByHash(hash string) (*APIKey, error) {
	row := database.DB.QueryRow(`
		SELECT id, name, description, key_prefix, key_suffix, domain, scopes, COALESCE(allowed_table_ids, '[]'),
		       enabled, expires_at, last_used_at, created_by, created_at, updated_at
		FROM api_keys WHERE key_hash = ?
	`, hash)
	return scanAPIKey(row)
}

func scanAPIKey(row *sql.Row) (*APIKey, error) {
	var k APIKey
	var scopesStr, tablesStr string
	var expiresAt, lastUsedAt sql.NullTime
	var enabled int
	err := row.Scan(&k.ID, &k.Name, &k.Description, &k.KeyPrefix, &k.KeySuffix, &k.Domain,
		&scopesStr, &tablesStr, &enabled, &expiresAt, &lastUsedAt,
		&k.CreatedBy, &k.CreatedAt, &k.UpdatedAt)
	if err != nil {
		return nil, err
	}
	json.Unmarshal([]byte(scopesStr), &k.Scopes)
	if k.Scopes == nil {
		k.Scopes = []string{}
	}
	json.Unmarshal([]byte(tablesStr), &k.AllowedTableIDs)
	if k.AllowedTableIDs == nil {
		k.AllowedTableIDs = []string{}
	}
	k.Enabled = enabled == 1
	if expiresAt.Valid {
		t := expiresAt.Time
		k.ExpiresAt = &t
	}
	if lastUsedAt.Valid {
		t := lastUsedAt.Time
		k.LastUsedAt = &t
	}
	return &k, nil
}

func contains(arr []string, s string) bool {
	for _, x := range arr {
		if x == s {
			return true
		}
	}
	return false
}

// ========== CRUD Handlers ==========

// HandleListAPIKeys GET /api/api-keys
// 只要有 menu:api_keys 菜单权限或任一操作权限即可查看列表
func HandleListAPIKeys(w http.ResponseWriter, r *http.Request) {
	if !isAdminOrHasPerm(r, "menu:api_keys", "api_key:create", "api_key:update", "api_key:delete") {
		sendError(w, "无权限", http.StatusForbidden)
		return
	}

	rows, err := database.DB.Query(`
		SELECT id, name, description, key_prefix, key_suffix, domain, scopes, COALESCE(allowed_table_ids, '[]'),
		       enabled, expires_at, last_used_at, created_by, created_at, updated_at
		FROM api_keys ORDER BY created_at DESC
	`)
	if err != nil {
		SafeError(w, "查询失败", http.StatusInternalServerError, err)
		return
	}
	defer rows.Close()

	list := []APIKey{}
	for rows.Next() {
		var k APIKey
		var scopesStr, tablesStr string
		var expiresAt, lastUsedAt sql.NullTime
		var enabled int
		if err := rows.Scan(&k.ID, &k.Name, &k.Description, &k.KeyPrefix, &k.KeySuffix, &k.Domain,
			&scopesStr, &tablesStr, &enabled, &expiresAt, &lastUsedAt,
			&k.CreatedBy, &k.CreatedAt, &k.UpdatedAt); err != nil {
			continue
		}
		json.Unmarshal([]byte(scopesStr), &k.Scopes)
		if k.Scopes == nil {
			k.Scopes = []string{}
		}
		json.Unmarshal([]byte(tablesStr), &k.AllowedTableIDs)
		if k.AllowedTableIDs == nil {
			k.AllowedTableIDs = []string{}
		}
		k.Enabled = enabled == 1
		if expiresAt.Valid {
			t := expiresAt.Time
			k.ExpiresAt = &t
		}
		if lastUsedAt.Valid {
			t := lastUsedAt.Time
			k.LastUsedAt = &t
		}
		list = append(list, k)
	}
	respondJSON(w, http.StatusOK, list)
}

type createAPIKeyReq struct {
	Name            string   `json:"name"`
	Description     string   `json:"description"`
	Domain          string   `json:"domain"`
	Scopes          []string `json:"scopes"`
	AllowedTableIDs []string `json:"allowed_table_ids"`
	ExpiresAt       string   `json:"expires_at"` // "" 表示永久；格式 "2026-05-01 00:00:00"
}

// HandleCreateAPIKey POST /api/api-keys
// 响应里带 full_key，只此一次返回
func HandleCreateAPIKey(w http.ResponseWriter, r *http.Request) {
	if !isAdminOrHasPerm(r, "api_key:create") {
		sendError(w, "无权限", http.StatusForbidden)
		return
	}
	_, username, _ := GetUserFromContext(r)

	var req createAPIKeyReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		sendError(w, "请求参数无效", http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(req.Name) == "" {
		sendError(w, "名称不能为空", http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(req.Domain) == "" {
		sendError(w, "业务域不能为空", http.StatusBadRequest)
		return
	}
	if len(req.Scopes) == 0 {
		sendError(w, "至少勾选一个权限", http.StatusBadRequest)
		return
	}
	if len(req.AllowedTableIDs) == 0 {
		sendError(w, "必须至少勾选一张允许访问的表", http.StatusBadRequest)
		return
	}

	fullKey, prefix, suffix, hash, err := generateAPIKey()
	if err != nil {
		SafeError(w, "生成 key 失败", http.StatusInternalServerError, err)
		return
	}

	scopesJSON, _ := json.Marshal(req.Scopes)
	tablesJSON, _ := json.Marshal(req.AllowedTableIDs)

	var expiresAt interface{}
	if req.ExpiresAt != "" {
		t, err := time.ParseInLocation("2006-01-02 15:04:05", req.ExpiresAt, time.Local)
		if err != nil {
			sendError(w, "过期时间格式错误，应为 YYYY-MM-DD HH:MM:SS", http.StatusBadRequest)
			return
		}
		expiresAt = t.Format("2006-01-02 15:04:05")
	} else {
		expiresAt = nil
	}

	id := uuid.New().String()
	_, err = database.DB.Exec(`
		INSERT INTO api_keys (id, name, description, key_hash, key_prefix, key_suffix, domain, scopes, allowed_table_ids, enabled, expires_at, created_by)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, 1, ?, ?)
	`, id, req.Name, req.Description, hash, prefix, suffix, req.Domain, string(scopesJSON), string(tablesJSON), expiresAt, username)
	if err != nil {
		SafeError(w, "保存失败", http.StatusInternalServerError, err)
		return
	}

	log.Printf("[APIKey] user=%s created key id=%s name=%s domain=%s", username, id, req.Name, req.Domain)

	respondJSON(w, http.StatusOK, map[string]interface{}{
		"id":         id,
		"name":       req.Name,
		"key":        fullKey, // 仅此一次返回明文
		"key_prefix": prefix,
		"key_suffix": suffix,
		"warning":    "请妥善保存完整 key，此后只能看到前缀和后 6 位，丢失需重新生成",
	})
}

type updateAPIKeyReq struct {
	Name            *string   `json:"name"`
	Description     *string   `json:"description"`
	Scopes          *[]string `json:"scopes"`
	AllowedTableIDs *[]string `json:"allowed_table_ids"`
	Enabled         *bool     `json:"enabled"`
	ExpiresAt       *string   `json:"expires_at"` // "" 永久；"null" 不变；有值则更新
	ClearExpires    bool      `json:"clear_expires"`
}

// HandleUpdateAPIKey PUT /api/api-keys/{id}
func HandleUpdateAPIKey(w http.ResponseWriter, r *http.Request) {
	if !isAdminOrHasPerm(r, "api_key:update") {
		sendError(w, "无权限", http.StatusForbidden)
		return
	}
	id := mux.Vars(r)["id"]

	var req updateAPIKeyReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		sendError(w, "请求参数无效", http.StatusBadRequest)
		return
	}

	sets := []string{}
	args := []interface{}{}
	if req.Name != nil {
		sets = append(sets, "name = ?")
		args = append(args, *req.Name)
	}
	if req.Description != nil {
		sets = append(sets, "description = ?")
		args = append(args, *req.Description)
	}
	if req.Scopes != nil {
		b, _ := json.Marshal(*req.Scopes)
		sets = append(sets, "scopes = ?")
		args = append(args, string(b))
	}
	if req.AllowedTableIDs != nil {
		if len(*req.AllowedTableIDs) == 0 {
			sendError(w, "必须至少保留一张允许访问的表", http.StatusBadRequest)
			return
		}
		b, _ := json.Marshal(*req.AllowedTableIDs)
		sets = append(sets, "allowed_table_ids = ?")
		args = append(args, string(b))
	}
	if req.Enabled != nil {
		sets = append(sets, "enabled = ?")
		if *req.Enabled {
			args = append(args, 1)
		} else {
			args = append(args, 0)
		}
	}
	if req.ClearExpires {
		sets = append(sets, "expires_at = NULL")
	} else if req.ExpiresAt != nil && *req.ExpiresAt != "" {
		t, err := time.ParseInLocation("2006-01-02 15:04:05", *req.ExpiresAt, time.Local)
		if err != nil {
			sendError(w, "过期时间格式错误", http.StatusBadRequest)
			return
		}
		sets = append(sets, "expires_at = ?")
		args = append(args, t.Format("2006-01-02 15:04:05"))
	}
	if len(sets) == 0 {
		sendError(w, "没有需要更新的字段", http.StatusBadRequest)
		return
	}
	args = append(args, id)

	query := "UPDATE api_keys SET " + strings.Join(sets, ", ") + " WHERE id = ?"
	_, err := database.DB.Exec(query, args...)
	if err != nil {
		SafeError(w, "更新失败", http.StatusInternalServerError, err)
		return
	}
	respondJSON(w, http.StatusOK, map[string]interface{}{"success": true})
}

// HandleDeleteAPIKey DELETE /api/api-keys/{id}
func HandleDeleteAPIKey(w http.ResponseWriter, r *http.Request) {
	if !isAdminOrHasPerm(r, "api_key:delete") {
		sendError(w, "无权限", http.StatusForbidden)
		return
	}
	id := mux.Vars(r)["id"]
	_, err := database.DB.Exec(`DELETE FROM api_keys WHERE id = ?`, id)
	if err != nil {
		SafeError(w, "删除失败", http.StatusInternalServerError, err)
		return
	}
	respondJSON(w, http.StatusOK, map[string]interface{}{"success": true})
}

// isAdminOrHasPerm 判断当前请求的 JWT 用户是不是管理员，或拥有任一权限码
func isAdminOrHasPerm(r *http.Request, codes ...string) bool {
	_, username, role := GetUserFromContext(r)
	if username == "" {
		return false
	}
	if role == "super_admin" || role == "admin" || role == "超级管理员" {
		return true
	}
	for _, code := range codes {
		ok, _ := UserHasPermission(username, role, code)
		if ok {
			return true
		}
	}
	return false
}
