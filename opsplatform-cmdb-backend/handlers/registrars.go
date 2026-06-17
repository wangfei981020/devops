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
	ID         int    `json:"id"`
	Name       string `json:"name"`
	Provider   string `json:"provider"`
	HasCred    bool   `json:"has_cred"`
	Enabled    int    `json:"enabled"`
}

func (h *RegistrarHandler) List(c *gin.Context) {
	rows, err := h.DB.Query(`SELECT id, name, provider,
		CASE WHEN credential_enc IS NULL OR credential_enc='' THEN 0 ELSE 1 END, enabled
		FROM registrars ORDER BY id`)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	defer rows.Close()
	out := []registrarOut{}
	for rows.Next() {
		var r registrarOut
		var has int
		if err := rows.Scan(&r.ID, &r.Name, &r.Provider, &has, &r.Enabled); err != nil {
			c.JSON(500, gin.H{"error": err.Error()})
			return
		}
		r.HasCred = has == 1
		out = append(out, r)
	}
	c.JSON(http.StatusOK, out)
}

type registrarIn struct {
	Name       string         `json:"name"`
	Provider   string         `json:"provider"`
	Credential map[string]any `json:"credential"` // 厂商凭据(明文输入)，存储前加密
	Enabled    int            `json:"enabled"`
}

func (h *RegistrarHandler) Create(c *gin.Context) {
	var in registrarIn
	if err := c.ShouldBindJSON(&in); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	enc, err := h.encCred(in.Credential)
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
	WriteAudit(h.DB, c, "create_registrar", in.Name)
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
	// 仅当传了非空凭据才更新（留空=保留原值）
	if len(in.Credential) > 0 {
		enc, err := h.encCred(in.Credential)
		if err != nil {
			c.JSON(500, gin.H{"error": err.Error()})
			return
		}
		_, _ = h.DB.Exec(`UPDATE registrars SET credential_enc=? WHERE id=?`, enc, id)
	}
	c.JSON(200, gin.H{"ok": true})
}

func (h *RegistrarHandler) Delete(c *gin.Context) {
	if _, err := h.DB.Exec(`DELETE FROM registrars WHERE id=?`, c.Param("id")); err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"ok": true})
}

func (h *RegistrarHandler) encCred(cred map[string]any) (string, error) {
	if len(cred) == 0 {
		return "", nil
	}
	b, _ := json.Marshal(cred)
	return h.Cipher.Encrypt(string(b))
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
