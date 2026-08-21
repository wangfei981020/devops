// Package registrar 域名注册商的凭据管理。
//
// # 这是 handler 迁移到 store 层的样板
//
// 迁移要点（其余 56 个 handler 照此办理）：
//
//  1. 持有 *store.Store 而不是 *sql.DB
//  2. 每个方法开头取 scoped：sc, err := h.Store.Tenant(c.Request.Context())
//  3. 每条 SQL 加 tenant_id 条件；租户值不要自己传，store 层会作为第一个参数注入
//  4. INSERT 走 sc.Insert()，列清单里显式含 tenant_id
//  5. 内部函数（非 HTTP）要接受 context，不能再直接拿 *sql.DB
//
// # 迁移过程中修掉的越权漏洞
//
// 原实现的 Update / Delete 用的是 `WHERE id = ?` —— 只要知道 id，
// **任何租户都能改删别的租户的注册商**，而注册商里存着域名厂商的 API 凭据。
// 这类漏洞的特征是：功能测试全过（自己的数据能改），只有跨租户用例能抓到。
package registrar

import (
	"context"
	"encoding/json"
	"net/http"
	"ops-cmdb-backend/dnsource"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"ops-cmdb-backend/crypto"
	"ops-cmdb-backend/internal/store"
)

type Handler struct {
	Store  *store.Store
	Cipher *crypto.Cipher
}

func New(st *store.Store, cipher *crypto.Cipher) *Handler {
	return &Handler{Store: st, Cipher: cipher}
}

func (h *Handler) Register(r *gin.RouterGroup) {
	r.GET("/registrars", h.List)
	r.POST("/registrars", h.Create)
	r.PUT("/registrars/:id", h.Update)
	r.DELETE("/registrars/:id", h.Delete)
}

type out struct {
	ID       int64  `json:"id"`
	Name     string `json:"name"`
	Provider string `json:"provider"`
	HasCred  bool   `json:"has_cred"`
	// SyncSupported 这个厂商有没有真正的同步实现。
	// 🔴 白名单里能选的厂商 ≠ 能自动同步的厂商：曾经 5 个可选、只有 1 个有实现，
	// 选了其余几个会保存成功、显示「已启用」，同步时才失败且无任何报错。
	// 把它显式回给前端，让界面能说清"选了会发生什么"。
	SyncSupported bool `json:"sync_supported"`
	DryRun        bool `json:"dry_run"` // 预演模式（写回/续费只打日志不真发/不扣费）
	Enabled       int  `json:"enabled"`
}

// Providers 认得的注册商类型。
//
// ⚠️ 不校验的话，打错一个字母（godady / namechep）会**静默建出一条永远不同步的记录**：
// 同步时按 provider 找不到实现，而界面上它显示"已启用"。
// 这类错误没有任何报错，只表现为"这个注册商下的域名到期日一直不更新"。
//
// "other" 是给只想登记、不需要自动同步的注册商用的 —— 它是个明确的选择，
// 不是兜底：兜底会让打错的值也悄悄通过。
var Providers = map[string]string{
	"godaddy":    "GoDaddy",
	"namecheap":  "Namecheap",
	"aliyun":     "阿里云",
	"cloudflare": "Cloudflare",
	"other":      "其他（仅登记，不自动同步）",
}

// patchIn 更新用。name/provider 必填（provider 上面就在校验），
// enabled 用指针 —— 不传就不动它，否则任何一次保存都会把停用的注册商重新启用。
type patchIn struct {
	Name       string         `json:"name"`
	Provider   string         `json:"provider"`
	Credential map[string]any `json:"credential"`
	DryRun     *bool          `json:"dry_run"`
	Enabled    *int           `json:"enabled"`
}

type in struct {
	Name       string         `json:"name"`
	Provider   string         `json:"provider"`
	Credential map[string]any `json:"credential"` // 明文输入，存储前加密；编辑时留空=保留原值
	DryRun     *bool          `json:"dry_run"`
	Enabled    int            `json:"enabled"`
}

func (h *Handler) List(c *gin.Context) {
	sc, err := h.Store.Tenant(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	rows, err := sc.Query(`SELECT id, name, provider, COALESCE(credential_enc,''), enabled
		  FROM registrars WHERE tenant_id = ? ORDER BY id`)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	defer rows.Close()

	list := []out{}
	for rows.Next() {
		var r out
		var enc string
		if err := rows.Scan(&r.ID, &r.Name, &r.Provider, &enc, &r.Enabled); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		// 只回传"有没有凭据"，绝不回传凭据本身 —— 生产上出过两个 P0，
		// 都是接口把凭据发给了不该看的人。
		r.HasCred = enc != ""
		r.SyncSupported = dnsource.SyncSupported(r.Provider)
		if enc != "" {
			if plain, e := h.Cipher.Decrypt(enc); e == nil {
				var m map[string]string
				if json.Unmarshal([]byte(plain), &m) == nil && m["dry_run"] == "1" {
					r.DryRun = true
				}
			}
		}
		list = append(list, r)
	}
	c.JSON(http.StatusOK, list)
}

func (h *Handler) Create(c *gin.Context) {
	sc, err := h.Store.Tenant(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	var body in
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if _, ok := Providers[body.Provider]; !ok {
		c.JSON(http.StatusBadRequest, gin.H{"error": "unknown provider", "provider": body.Provider})
		return
	}

	cred := strMap(body.Credential)
	if body.DryRun != nil && *body.DryRun {
		cred["dry_run"] = "1"
	}
	enc, err := h.encrypt(cred)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	res, err := sc.Insert(
		`INSERT INTO registrars (tenant_id, name, provider, credential_enc, enabled) VALUES (?, ?, ?, ?, ?)`,
		body.Name, body.Provider, enc, 1)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	id, _ := res.LastInsertId()
	c.JSON(http.StatusCreated, gin.H{"id": id})
}

func (h *Handler) Update(c *gin.Context) {
	sc, err := h.Store.Tenant(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "id 不合法"})
		return
	}
	var body patchIn
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	// 更新路径同样要校验：只在新建时拦，改一次名就能把 provider 改成打错的值
	if _, ok := Providers[body.Provider]; !ok {
		c.JSON(http.StatusBadRequest, gin.H{"error": "unknown provider", "provider": body.Provider})
		return
	}

	// ★ 越权修复：原来是 `WHERE id=?`，任何租户都能改别人的注册商。
	// 现在 tenant_id 由 store 层注入，改不到别人的行 —— 影响 0 行即视为不存在。
	// ⚠️ provider 上面已强制校验（传了就必须是认得的），所以这里一定有值。
	//	name 与 enabled 用请求体里的值 —— 但 name 为空要拦下：
	//	一个没有名字的注册商在列表里认不出来（OPSCMDB-083）。
	if strings.TrimSpace(body.Name) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "name 不能为空"})
		return
	}
	cols := []string{"name=?", "provider=?"}
	args := []any{body.Name, body.Provider}
	if body.Enabled != nil {
		cols = append(cols, "enabled=?")
		args = append(args, *body.Enabled)
	}
	res, err := sc.Exec(`UPDATE registrars SET `+strings.Join(cols, ", ")+
		` WHERE tenant_id = ? AND id=?`, append(args, id)...)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		// 404 而不是 403：403 泄露了"这个 id 存在"这一事实。
		// 对跨租户场景，不存在与无权限必须不可区分。
		c.JSON(http.StatusNotFound, gin.H{"error": "注册商不存在"})
		return
	}

	// 凭据合并更新：以现有凭据为底，只覆盖本次传的非空字段 + dry_run 开关，
	// 从而支持"只改预演开关不动 key/secret"（留空=保留原值）。
	provided := strMap(body.Credential)
	if len(provided) > 0 || body.DryRun != nil {
		_, existing, _ := LoadCredential(c.Request.Context(), h.Store, h.Cipher, id)
		if existing == nil {
			existing = map[string]string{}
		}
		for k, v := range provided {
			existing[k] = v
		}
		if body.DryRun != nil {
			if *body.DryRun {
				existing["dry_run"] = "1"
			} else {
				delete(existing, "dry_run")
			}
		}
		enc, err := h.encrypt(existing)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		if _, err := sc.Exec(`UPDATE registrars SET credential_enc=? WHERE tenant_id = ? AND id=?`, enc, id); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func (h *Handler) Delete(c *gin.Context) {
	sc, err := h.Store.Tenant(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "id 不合法"})
		return
	}
	// ★ 同样的越权修复
	res, err := sc.Exec(`DELETE FROM registrars WHERE tenant_id = ? AND id=?`, id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "注册商不存在"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// LoadCredential 解密取用某注册商凭据（内部用，不经 HTTP 暴露）。
//
// ⚠️ 迁移后必须接受 context —— 内部调用同样要受租户约束。
// 原实现直接收 *sql.DB，等于给内部调用开了一个绕过隔离的口子。
func LoadCredential(ctx context.Context, st *store.Store, cipher *crypto.Cipher, registrarID int64) (provider string, cred map[string]string, err error) {
	sc, err := st.Tenant(ctx)
	if err != nil {
		return "", nil, err
	}
	var enc string
	err = sc.QueryRow(
		`SELECT provider, COALESCE(credential_enc,'') FROM registrars WHERE tenant_id = ? AND id=?`,
		registrarID).Scan(&provider, &enc)
	if err != nil {
		return "", nil, err
	}
	cred = map[string]string{}
	if enc != "" {
		plain, e := cipher.Decrypt(enc)
		if e != nil {
			return provider, nil, e
		}
		_ = json.Unmarshal([]byte(plain), &cred)
	}
	return provider, cred, nil
}

// strMap 把凭据转为 map[string]string，丢弃空值（编辑时空=保留原值）。
func strMap(m map[string]any) map[string]string {
	out := map[string]string{}
	for k, v := range m {
		if s, ok := v.(string); ok && s != "" {
			out[k] = s
		}
	}
	return out
}

func (h *Handler) encrypt(cred map[string]string) (string, error) {
	if len(cred) == 0 {
		return "", nil
	}
	b, _ := json.Marshal(cred)
	return h.Cipher.Encrypt(string(b))
}
