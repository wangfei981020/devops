package handlers

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"opsplatform-cmdb-backend/logx"
	"opsplatform-cmdb-backend/models"
)

// logExec 执行写并在出错时打日志（替代 `_, _ = db.Exec` 静默吞错，尤其同步/导入写路径）。
// 返回受影响行数（出错返回 0）。desc 用于日志定位。
func logExec(db *sql.DB, desc, query string, args ...any) int64 {
	res, err := db.Exec(query, args...)
	if err != nil {
		logx.J("db", "exec_fail", map[string]any{"desc": desc, "error": err.Error()})
		return 0
	}
	n, _ := res.RowsAffected()
	return n
}

// scannable 兼容 *sql.Row 与 *sql.Rows
type scannable interface{ Scan(dest ...any) error }

// scanCI 按统一列序扫描一条 CI（不含 labels/relations）
func scanCI(s scannable) (models.CI, error) {
	var ci models.CI
	err := s.Scan(&ci.ID, &ci.Type, &ci.Name, &ci.Project, &ci.Env, &ci.Module,
		&ci.Owner, &ci.Status, &ci.Remark, &ci.CreatedAt, &ci.UpdatedAt)
	return ci, err
}

func parseID(s string) (int64, error) { return strconv.ParseInt(s, 10, 64) }

// currentUser 取当前登录账号（JWT 中间件写入），用于自动记录操作人。
func currentUser(c *gin.Context) string {
	u, _ := c.Get("username")
	s, _ := u.(string)
	return s
}

func nullableInt(p *int) any {
	if p == nil {
		return nil
	}
	return *p
}

// replaceLabelsDB 全量替换某 CI 的自定义标签
func replaceLabelsDB(db *sql.DB, ciID int64, labels map[string]string) {
	_, _ = db.Exec(`DELETE FROM ci_labels WHERE ci_id=?`, ciID)
	for k, v := range labels {
		if k == "" {
			continue
		}
		_, _ = db.Exec(`INSERT INTO ci_labels (ci_id, k, v) VALUES (?, ?, ?)`, ciID, k, v)
	}
}

// RequestID 给每个请求分配/透传 request_id：写进 gin 上下文、请求 context（供日志携带）、响应头。
// 一个请求的所有 [tag] JSON 日志都会带同一个 request_id，排查时按 id grep 即可串起全链路。
func RequestID() gin.HandlerFunc {
	return func(c *gin.Context) {
		rid := c.GetHeader("X-Request-Id")
		if rid == "" {
			rid = genRequestID()
		}
		c.Set("request_id", rid)
		c.Request = c.Request.WithContext(logx.WithRequestID(c.Request.Context(), rid))
		c.Header("X-Request-Id", rid)
		c.Next()
	}
}

// genRequestID 生成短随机 id（16 hex）；失败退化到纳秒时间戳，保证非空。
func genRequestID() string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return "req-" + strconv.FormatInt(time.Now().UnixNano(), 36)
	}
	return hex.EncodeToString(b)
}

// AccessLog gin 访问日志改结构化 JSON（替代默认 [GIN] 文本行），带 request_id 串全链路。
func AccessLog() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()
		logx.J("access", "http", map[string]any{
			"method":     c.Request.Method,
			"path":       c.Request.URL.Path,
			"status":     c.Writer.Status(),
			"latency_ms": float64(time.Since(start).Microseconds()) / 1000.0,
			"ip":         c.ClientIP(),
			"size":       c.Writer.Size(),
			"request_id": c.GetString("request_id"),
		})
	}
}

// CORS 允许跨域（前端经 nginx 同源代理，开发期直连也放开）。
func CORS() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("Access-Control-Allow-Origin", "*")
		c.Header("Access-Control-Allow-Methods", "GET,POST,PUT,DELETE,OPTIONS")
		c.Header("Access-Control-Allow-Headers", "Origin,Content-Type,Authorization")
		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(204)
			return
		}
		c.Next()
	}
}

// WriteAudit 记录审计日志（who/action/target/ip）。表在阶段1建，缺表时静默跳过。
func WriteAudit(db *sql.DB, c *gin.Context, action, target string) {
	user, _ := c.Get("username")
	uname, _ := user.(string)
	_, _ = db.Exec(`INSERT INTO audit_logs (username, action, target, ip) VALUES (?, ?, ?, ?)`,
		uname, action, target, c.ClientIP())
}
