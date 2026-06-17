package handlers

import (
	"database/sql"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"
)

type AuthHandler struct {
	DB     *sql.DB
	Secret []byte
}

func NewAuthHandler(db *sql.DB, secret string) *AuthHandler {
	return &AuthHandler{DB: db, Secret: []byte(secret)}
}

// EnsureAdmin 首启 seed 一个 admin（密码取 ADMIN_PASSWORD，默认 admin123）。
func (h *AuthHandler) EnsureAdmin() {
	var n int
	if err := h.DB.QueryRow(`SELECT COUNT(*) FROM users WHERE username='admin'`).Scan(&n); err != nil {
		log.Printf("EnsureAdmin check: %v", err)
		return
	}
	if n > 0 {
		return
	}
	pw := os.Getenv("ADMIN_PASSWORD")
	if pw == "" {
		pw = "admin123"
	}
	hash, _ := bcrypt.GenerateFromPassword([]byte(pw), bcrypt.DefaultCost)
	if _, err := h.DB.Exec(`INSERT INTO users (username, password_hash, display_name, is_admin) VALUES ('admin', ?, '管理员', 1)`, string(hash)); err != nil {
		log.Printf("EnsureAdmin seed: %v", err)
		return
	}
	log.Printf("seeded admin user (password from ADMIN_PASSWORD or default admin123)")
}

func (h *AuthHandler) RegisterPublic(r *gin.RouterGroup) {
	r.POST("/login", h.Login)
}

func (h *AuthHandler) Login(c *gin.Context) {
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	var id int
	var hash, display string
	err := h.DB.QueryRow(`SELECT id, password_hash, display_name FROM users WHERE username=?`, req.Username).
		Scan(&id, &hash, &display)
	if err != nil || bcrypt.CompareHashAndPassword([]byte(hash), []byte(req.Password)) != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "用户名或密码错误"})
		return
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"uid": id, "username": req.Username, "exp": time.Now().Add(24 * time.Hour).Unix(),
	})
	signed, _ := tok.SignedString(h.Secret)
	c.JSON(http.StatusOK, gin.H{"token": signed, "username": req.Username, "display_name": display})
}

// Middleware 校验 JWT（header Authorization: Bearer xxx）。
func (h *AuthHandler) Middleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		auth := c.GetHeader("Authorization")
		raw := strings.TrimPrefix(auth, "Bearer ")
		if raw == "" || raw == auth && !strings.HasPrefix(auth, "Bearer ") {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "未登录"})
			return
		}
		tok, err := jwt.Parse(raw, func(t *jwt.Token) (interface{}, error) { return h.Secret, nil })
		if err != nil || !tok.Valid {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "登录已失效"})
			return
		}
		if claims, ok := tok.Claims.(jwt.MapClaims); ok {
			c.Set("username", claims["username"])
		}
		c.Next()
	}
}
