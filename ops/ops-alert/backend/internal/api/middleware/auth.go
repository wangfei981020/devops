// Package middleware 是请求入口的认证与租户注入。
package middleware

import (
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"

	"ops-alert-backend/internal/store"
)

type Claims struct {
	UserID   int64  `json:"uid"`
	TenantID int64  `json:"tid"`
	Username string `json:"usr"`
	Role     string `json:"role"`
	jwt.RegisteredClaims
}

// Issue 签发会话 token。
func Issue(secret string, c Claims, ttl time.Duration) (string, error) {
	c.RegisteredClaims = jwt.RegisteredClaims{
		ExpiresAt: jwt.NewNumericDate(time.Now().Add(ttl)),
		IssuedAt:  jwt.NewNumericDate(time.Now()),
	}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, c).SignedString([]byte(secret))
}

// Auth 校验 token 并把租户放进请求上下文。
//
// 租户只能从这里进入上下文——store 层拿不到租户会直接拒绝执行，
// 所以「忘了设租户」的后果是报错，不是查全表。
func Auth(secret string) gin.HandlerFunc {
	return func(c *gin.Context) {
		raw := strings.TrimPrefix(c.GetHeader("Authorization"), "Bearer ")
		if raw == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
			return
		}
		var claims Claims
		token, err := jwt.ParseWithClaims(raw, &claims, func(t *jwt.Token) (any, error) {
			// 显式校验签名算法：不校验的话，攻击者可以把 alg 改成 none
			// 伪造任意身份，而库默认是接受的。
			if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
				return nil, jwt.ErrSignatureInvalid
			}
			return []byte(secret), nil
		})
		if err != nil || !token.Valid {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "invalid token"})
			return
		}
		ctx := store.WithTenant(c.Request.Context(), store.TenantID(claims.TenantID))
		c.Request = c.Request.WithContext(ctx)
		c.Set("user", claims)
		c.Next()
	}
}

// CurrentUser 取当前登录者。
func CurrentUser(c *gin.Context) Claims {
	v, _ := c.Get("user")
	claims, _ := v.(Claims)
	return claims
}
