package handlers

import (
	"crypto/subtle"
	"database/sql"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"

	"opsplatform-cmdb-backend/crypto"
	"opsplatform-cmdb-backend/logx"
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
	r.GET("/certs/:id/bundle", bundleRateLimit(), h.Bundle)
}

// bundleRateLimit 按来源 IP 限速。
//
//	正常用法是目标机定时拉一次（分钟级），所以限得很紧不影响使用；
//	但能把"拿到一个 token 后逐个 ci_id 扫"的速度压下来，并在审计里
//	留下明显的 429 尖峰。这不是防线（token 对了就能拿），是**减速带 + 信号**。
func bundleRateLimit() gin.HandlerFunc {
	const (
		windowSec = 60
		maxPerIP  = 30
	)
	type bucket struct {
		start int64
		n     int
	}
	var mu sync.Mutex
	buckets := map[string]*bucket{}

	return func(c *gin.Context) {
		ip := c.ClientIP()
		now := time.Now().Unix()
		mu.Lock()
		b := buckets[ip]
		if b == nil || now-b.start >= windowSec {
			b = &bucket{start: now}
			buckets[ip] = b
			// 顺手清理过期桶，避免被伪造 IP 打爆内存
			for k, v := range buckets {
				if now-v.start >= windowSec*5 {
					delete(buckets, k)
				}
			}
		}
		b.n++
		over := b.n > maxPerIP
		mu.Unlock()

		if over {
			logx.J("cert_bundle", "rate_limited", map[string]any{"ip": ip, "ci_id": c.Param("id")})
			c.AbortWithStatusJSON(http.StatusTooManyRequests,
				gin.H{"error": "请求过于频繁，请稍后重试"})
			return
		}
		c.Next()
	}
}

func orDefault(v, def string) string {
	if v == "" {
		return def
	}
	return v
}

// Bundle GET /certs/:id/bundle?part=version|fullchain|key
// 鉴权 token 优先从 Header 取（X-Deploy-Token 或 Authorization: Bearer），query ?token= 仅向后兼容
// （query 会进 access log 泄露，目标机应尽快改用 Header）。比较用常量时间防时序侧信道。
func (h *BundleHandler) Bundle(c *gin.Context) {
	id := c.Param("id")
	token := c.GetHeader("X-Deploy-Token")
	if token == "" {
		if a := c.GetHeader("Authorization"); strings.HasPrefix(a, "Bearer ") {
			token = strings.TrimPrefix(a, "Bearer ")
		}
	}
	if token == "" {
		token = c.Query("token") // 向后兼容，不推荐（进日志）
	}
	var cert, keyEnc, dbToken string
	var version int
	err := h.DB.QueryRow(`SELECT COALESCE(cert_pem,''), COALESCE(key_pem_enc,''), deploy_token, version FROM certificates WHERE ci_id=?`, id).
		Scan(&cert, &keyEnc, &dbToken, &version)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		return
	}
	if token == "" || dbToken == "" || subtle.ConstantTimeCompare([]byte(token), []byte(dbToken)) != 1 {
		// 失败也必须留痕：有人拿着错 token 逐个试 ci_id，就是在爆破私钥。
		// 只记来源和 ci_id，绝不记 token 本身（日志会被更多人看到）。
		logx.J("cert_bundle", "auth_failed", map[string]any{
			"ci_id": id, "ip": c.ClientIP(), "ua": truncate(c.Request.UserAgent(), 80)})
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid token"})
		return
	}
	// ⚠️ 这个路由能拿到私钥且不走登录，所以每一次成功取证都要能追溯：
	//	泄露发生后要回答的第一个问题是"到底有没有被导出过、谁导的"，
	//	没有这条记录就只能靠猜。
	logx.J("cert_bundle", "fetch", map[string]any{
		"ci_id": id, "part": orDefault(c.Query("part"), "all"),
		"ip": c.ClientIP(), "ua": truncate(c.Request.UserAgent(), 80), "version": version})
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
		key, derr := h.decKey(keyEnc)
		if derr != nil {
			logx.J("cert", "key_decrypt_fail", map[string]any{"ci_id": id, "op": "bundle", "error": derr.Error()})
			c.JSON(http.StatusInternalServerError, gin.H{"error": "私钥解密失败，请联系管理员"})
			return
		}
		c.Data(http.StatusOK, "application/x-pem-file", []byte(key))
		return
	}
	key, derr := h.decKey(keyEnc)
	if derr != nil {
		logx.J("cert", "key_decrypt_fail", map[string]any{"ci_id": id, "op": "bundle", "error": derr.Error()})
		c.JSON(http.StatusInternalServerError, gin.H{"error": "私钥解密失败，请联系管理员"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"version": version, "fullchain": cert, "key": key})
}

// decKey 解密私钥密文；空密文返回空串（不报错），解密失败返回错误（不静默下发空私钥）。
func (h *BundleHandler) decKey(keyEnc string) (string, error) {
	if keyEnc == "" {
		return "", nil
	}
	return h.Cipher.Decrypt(keyEnc)
}
