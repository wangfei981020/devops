package handlers

import (
	"database/sql"
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"

	"opsplatform-cmdb-backend/crypto"
)

// BundleHandler 提供 A+ 拉取式取证书：目标机用 deploy_token 自助拉取最新证书。
// 该路由不走 JWT 鉴权（用 token 自鉴权），注册在登录中间件之前。
type BundleHandler struct {
	DB     *sql.DB
	Cipher *crypto.Cipher
}

func NewBundleHandler(db *sql.DB, cipher *crypto.Cipher) *BundleHandler {
	return &BundleHandler{DB: db, Cipher: cipher}
}

func (h *BundleHandler) RegisterPublic(r *gin.RouterGroup) {
	r.GET("/certs/:id/bundle", h.Bundle)
}

// Bundle GET /certs/:id/bundle?token=xxx&part=version|fullchain|key
// 无 part：返回 JSON {version, fullchain, key}；带 ETag，If-None-Match 命中返回 304。
func (h *BundleHandler) Bundle(c *gin.Context) {
	id := c.Param("id")
	token := c.Query("token")
	var cert, keyEnc, dbToken string
	var version int
	err := h.DB.QueryRow(`SELECT COALESCE(cert_pem,''), COALESCE(key_pem_enc,''), deploy_token, version FROM certificates WHERE ci_id=?`, id).
		Scan(&cert, &keyEnc, &dbToken, &version)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		return
	}
	if token == "" || token != dbToken {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid token"})
		return
	}
	if version == 0 {
		c.JSON(http.StatusConflict, gin.H{"error": "证书尚未签发"})
		return
	}

	etag := fmt.Sprintf(`"v%d"`, version)
	c.Header("ETag", etag)
	if c.GetHeader("If-None-Match") == etag {
		c.Status(http.StatusNotModified)
		return
	}

	switch c.Query("part") {
	case "version":
		c.String(http.StatusOK, "%d", version)
		return
	case "fullchain":
		c.Data(http.StatusOK, "application/x-pem-file", []byte(cert))
		return
	case "key":
		key := ""
		if keyEnc != "" {
			key, _ = h.Cipher.Decrypt(keyEnc)
		}
		c.Data(http.StatusOK, "application/x-pem-file", []byte(key))
		return
	}
	key := ""
	if keyEnc != "" {
		key, _ = h.Cipher.Decrypt(keyEnc)
	}
	c.JSON(http.StatusOK, gin.H{"version": version, "fullchain": cert, "key": key})
}
