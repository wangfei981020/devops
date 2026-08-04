package handlers

import (
	"database/sql"
	"encoding/json"
	"net/http"

	"github.com/gin-gonic/gin"

	"opsplatform-cmdb-backend/crypto"
)

type RegistrarHandler struct {
	DB     *sql.DB
	Cipher *crypto.Cipher
}

func NewRegistrarHandler(db *sql.DB, cipher *crypto.Cipher) *RegistrarHandler {
	return &RegistrarHandler{DB: db, Cipher: cipher}
}

func (h *RegistrarHandler) Register(r *gin.RouterGroup) {
	r.GET("/registrars", h.List)
	r.POST("/registrars", h.Create)
	r.PUT("/registrars/:id", h.Update)
	r.DELETE("/registrars/:id", h.Delete)
}

type registrarOut struct {
	ID       int    `json:"id"`
	Name     string `json:"name"`
	Provider string `json:"provider"`
	HasCred  bool   `json:"has_cred"`
	DryRun   bool   `json:"dry_run"` // 预演模式（写回/续费只打日志不真发/不扣费）
	Enabled  int    `json:"enabled"`
}

func (h *RegistrarHandler) List(c *gin.Context) {
	rows, err := h.DB.Query(`SELECT id, name, provider, COALESCE(credential_enc,''), enabled
		FROM registrars ORDER BY id`)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	defer rows.Close()
	out := []registrarOut{}
	for rows.Next() {
		var r registrarOut
		var enc string
		if err := rows.Scan(&r.ID, &r.Name, &r.Provider, &enc, &r.Enabled); err != nil {
			c.JSON(500, gin.H{"error": err.Error()})
			return
		}
		r.HasCred = enc != ""
		// 解密读 dry_run（不回传密钥本身）
		if enc != "" {
			if plain, e := h.Cipher.Decrypt(enc); e == nil {
				var m map[string]string
				if json.Unmarshal([]byte(plain), &m) == nil && m["dry_run"] == "1" {
					r.DryRun = true
				}
			}
		}
		out = append(out, r)
	}
	c.JSON(http.StatusOK, out)
}

type registrarIn struct {
	Name       string         `json:"name"`
	Provider   string         `json:"provider"`
	Credential map[string]any `json:"credential"` // 厂商凭据(明文输入)，存储前加密；编辑时留空=保留原值
	DryRun     *bool          `json:"dry_run"`    // 预演模式开关（写回/续费只打日志不真发/不扣费）
	Enabled    int            `json:"enabled"`
}

func (h *RegistrarHandler) Create(c *gin.Context) {
	var in registrarIn
	if err := c.ShouldBindJSON(&in); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	cred := strMap(in.Credential)
	if in.DryRun != nil && *in.DryRun {
		cred["dry_run"] = "1"
	}
	enc, err := h.encCredStr(cred)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	res, err := h.DB.Exec(`INSERT INTO registrars (name, provider, credential_enc, enabled) VALUES (?, ?, ?, ?)`,
		in.Name, in.Provider, enc, 1)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	id, _ := res.LastInsertId()
	AuditCreated(c, "registrars", id)
	SetAuditTarget(c, in.Name)
	c.JSON(201, gin.H{"id": id})
}

func (h *RegistrarHandler) Update(c *gin.Context) {
	id := c.Param("id")
	var in registrarIn
	if err := c.ShouldBindJSON(&in); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	if _, err := h.DB.Exec(`UPDATE registrars SET name=?, provider=?, enabled=? WHERE id=?`,
		in.Name, in.Provider, in.Enabled, id); err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	// 凭据合并更新：以现有凭据为底，只覆盖本次传的非空字段 + dry_run 开关，
	// 从而支持"只改预演开关不动 key/secret"（留空=保留原值）。
	provided := strMap(in.Credential)
	idInt, _ := parseID(id)
	if len(provided) > 0 || in.DryRun != nil {
		_, existing, _ := LoadCredential(h.DB, h.Cipher, int(idInt))
		if existing == nil {
			existing = map[string]string{}
		}
		for k, v := range provided { // 只覆盖非空
			existing[k] = v
		}
		if in.DryRun != nil {
			if *in.DryRun {
				existing["dry_run"] = "1"
			} else {
				delete(existing, "dry_run")
			}
		}
		enc, err := h.encCredStr(existing)
		if err != nil {
			c.JSON(500, gin.H{"error": err.Error()})
			return
		}
		if _, err := h.DB.Exec(`UPDATE registrars SET credential_enc=? WHERE id=?`, enc, id); err != nil {
			c.JSON(500, gin.H{"error": err.Error()})
			return
		}
	}
	SetAuditTarget(c, in.Name)
	c.JSON(200, gin.H{"ok": true})
}

// strMap 把 map[string]any 的凭据转为 map[string]string，丢弃空值（编辑时空=保留原值）。
func strMap(m map[string]any) map[string]string {
	out := map[string]string{}
	for k, v := range m {
		if s, ok := v.(string); ok && s != "" {
			out[k] = s
		}
	}
	return out
}

func (h *RegistrarHandler) encCredStr(cred map[string]string) (string, error) {
	if len(cred) == 0 {
		return "", nil
	}
	b, _ := json.Marshal(cred)
	return h.Cipher.Encrypt(string(b))
}

func (h *RegistrarHandler) Delete(c *gin.Context) {
	if _, err := h.DB.Exec(`DELETE FROM registrars WHERE id=?`, c.Param("id")); err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"ok": true})
}

// LoadCredential 供 ACME 模块解密取用某注册商凭据（内部用，不经 HTTP 暴露）。
func LoadCredential(db *sql.DB, cipher *crypto.Cipher, registrarID int) (provider string, cred map[string]string, err error) {
	var enc string
	err = db.QueryRow(`SELECT provider, COALESCE(credential_enc,'') FROM registrars WHERE id=?`, registrarID).Scan(&provider, &enc)
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
