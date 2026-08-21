package handlers

import (
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"ops-cmdb-backend/internal/httpx"
	"ops-cmdb-backend/logx"
)

// MCP 接入令牌管理。
//
// 一个接入方一条令牌，各自绑角色、各自可吊销。
// 原来是单个全局 token + 管理员身份，见 migration 109 的说明。

// mcpTokenOut 列表里的一条。**不含令牌本身**——它只在创建那一刻返回一次。
type mcpTokenOut struct {
	ID       int    `json:"id"`
	Name     string `json:"name"`
	Hint     string `json:"hint"`
	RoleCode string `json:"role_code"`
	Enabled  bool   `json:"enabled"`
	// Unrestricted 这条令牌不受权限约束（升级前遗留）。界面上要显眼地标出来
	Unrestricted bool `json:"unrestricted"`
	// Tools 这条令牌实际能看到的工具数（已过授权分档 + 角色两层）。
	// MCP 信息卡上那个数只按授权档次算，不含角色 —— 这里给的才是真数
	Tools     int    `json:"tools"`
	CreatedBy string `json:"created_by"`
	CreatedAt string `json:"created_at"`
	// LastUsedAt null = 从没用过。⚠️ 不能压成"很久以前"——
	// "建了没人用"和"用过但很久没用"要采取的动作不一样
	LastUsedAt *string `json:"last_used_at"`
	LastUsedIP string  `json:"last_used_ip"`
	// ExpiresAt null = **永久有效**。
	//
	//	⚠️ 界面必须把"永久"明说出来，不能只留一个空白 ——
	//	空白会被读成"这一项没填"，而它的实际含义是
	//	"这个能读全库的凭据永远不会失效"（OPSCMDB-031 P1-70）。
	ExpiresAt *string `json:"expires_at"`
	// Expired 已经过期。过期的令牌调用会被拒，界面上要和"停用"一样显眼
	Expired bool `json:"expired"`
	// ExpiringSoon 30 天内到期。给人留出换发的时间，
	// 而不是等 AI 全线 401 之后才发现
	ExpiringSoon bool `json:"expiring_soon"`
}

// ListTokens GET /api/mcp/tokens
//
//	@Summary		MCP 令牌列表
//	@Description	不返回令牌本身，只有前 8 位提示。
//	@Tags			mcp
//	@Produce		json
//	@Success		200	{object}	httpx.ListResponse[handlers.mcpTokenOut]
//	@Router			/mcp/tokens [get]
func (h *MCPHandler) ListTokens(c *gin.Context) {
	rows, err := h.DB.QueryContext(c.Request.Context(),
		`SELECT id, name, token_hint, role_code, enabled, created_by, created_at,
		 last_used_at, last_used_ip, expires_at FROM mcp_tokens ORDER BY id DESC`)
	if err != nil {
		httpx.Fail(c, httpx.CodeInternal, err, nil)
		return
	}
	defer rows.Close()

	out := []mcpTokenOut{}
	for rows.Next() {
		var t mcpTokenOut
		var enabled int
		var created time.Time
		var lastUsed, expires sql.NullTime
		if rows.Scan(&t.ID, &t.Name, &t.Hint, &t.RoleCode, &enabled, &t.CreatedBy,
			&created, &lastUsed, &t.LastUsedIP, &expires) != nil {
			continue
		}
		t.Enabled = enabled == 1
		t.Unrestricted = t.RoleCode == "" || t.RoleCode == roleAdmin
		// ⚠️ 逐令牌算**它实际能看到几个工具**。
		//
		// MCP 信息卡上那个数只按授权档次算，不含角色过滤 —— 接入方按那个数
		// 去规划，实际调用时会撞上一批看不见的工具（OPSCMDB-008）。
		// 这里给的是真数：和这条令牌 tools/list 拿到的完全一致。
		t.Tools = len(h.toolSchemas(t.RoleCode))
		t.CreatedAt = created.Format(time.RFC3339)
		if lastUsed.Valid {
			s := lastUsed.Time.Format(time.RFC3339)
			t.LastUsedAt = &s
		}
		if expires.Valid {
			s := expires.Time.Format(time.RFC3339)
			t.ExpiresAt = &s
			t.Expired = time.Now().After(expires.Time)
			t.ExpiringSoon = !t.Expired && time.Until(expires.Time) < 30*24*time.Hour
		}
		out = append(out, t)
	}
	c.JSON(http.StatusOK, gin.H{"items": out, "total": len(out)})
}

// CreateToken POST /api/mcp/tokens  {"name":"...","role_code":"cmdb_viewer"}
//
//	@Summary		新建 MCP 令牌
//	@Description	明文令牌**只在这次响应里返回一次**，之后无法再取。
//	@Tags			mcp
//	@Accept			json
//	@Produce		json
//	@Success		200	{object}	map[string]any
//	@Failure		400	{object}	httpx.APIError
//	@Router			/mcp/tokens [post]
func (h *MCPHandler) CreateToken(c *gin.Context) {
	var in struct {
		Name     string `json:"name"`
		RoleCode string `json:"role_code"`
		// ExpiresAt RFC3339 时刻。空 = 永久有效（见 migration 119 的取舍说明）
		ExpiresAt string `json:"expires_at"`
	}
	if err := c.ShouldBindJSON(&in); err != nil || in.Name == "" {
		httpx.FailKey(c, httpx.CodeBadRequest, "error.mcpTokenNeedsName", err, nil)
		return
	}
	// ⚠️ 不允许建"不受限"的令牌。
	//
	// 角色是这条令牌能看到多少东西的唯一闸门，允许留空 = 允许绕开整套权限体系，
	// 而它绕得毫无痕迹：接口照常返回数据，没有任何人会收到提醒。
	// 遗留的空角色令牌只能从升级里来，不能从这个接口里来。
	if in.RoleCode == "" {
		httpx.FailKey(c, httpx.CodeBadRequest, "error.mcpTokenNeedsRole", nil, nil)
		return
	}
	var exists int
	if err := h.DB.QueryRowContext(c.Request.Context(),
		`SELECT COUNT(*) FROM local_roles WHERE code=?`, in.RoleCode).Scan(&exists); err != nil || exists == 0 {
		httpx.FailKey(c, httpx.CodeBadRequest, "error.roleNotFound", nil,
			map[string]any{"role": in.RoleCode})
		return
	}

	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		httpx.Fail(c, httpx.CodeInternal, err, nil)
		return
	}
	tok := hex.EncodeToString(b)
	sum := sha256.Sum256([]byte(tok))

	exp, perr := parseTokenExpiry(in.ExpiresAt)
	if perr != nil {
		httpx.FailKey(c, httpx.CodeBadRequest, "error.mcpTokenBadExpiry", perr, nil)
		return
	}
	res, err := h.DB.ExecContext(c.Request.Context(),
		`INSERT INTO mcp_tokens (name, token_hash, token_hint, role_code, created_by, expires_at)
		 VALUES (?,?,?,?,?,?)`,
		in.Name, hex.EncodeToString(sum[:]), tok[:8], in.RoleCode, UsernameFromCtx(c), exp)
	if err != nil {
		httpx.Fail(c, httpx.CodeInternal, err, nil)
		return
	}
	id, _ := res.LastInsertId()
	SetAuditTarget(c, in.Name+" 角色="+in.RoleCode)
	logx.J("mcp", "token_created", map[string]any{
		"by": UsernameFromCtx(c), "name": in.Name, "role": in.RoleCode, "id": id,
	})
	// token 只在这里出现一次。列表接口永远不会再返回它
	c.JSON(http.StatusOK, gin.H{"ok": true, "id": id, "token": tok})
}

// UpdateToken PUT /api/mcp/tokens/:id  {"enabled":false,"role_code":"..."}
//
//	@Summary		改 MCP 令牌（启停 / 换角色）
//	@Tags			mcp
//	@Accept			json
//	@Produce		json
//	@Success		200	{object}	map[string]any
//	@Router			/mcp/tokens/{id} [put]
func (h *MCPHandler) UpdateToken(c *gin.Context) {
	id := c.Param("id")
	var in struct {
		Enabled  *bool   `json:"enabled"`
		RoleCode *string `json:"role_code"`
		// ExpiresAt 传空字符串 = 改成永久。
		//	⚠️ 用指针区分"没传这一项"和"传了空串" ——
		//	不区分的话，只改角色的那次请求会顺手把有效期清成永久
		ExpiresAt *string `json:"expires_at"`
	}
	if err := c.ShouldBindJSON(&in); err != nil {
		httpx.Fail(c, httpx.CodeBadRequest, err, nil)
		return
	}
	if in.Enabled != nil {
		v := 0
		if *in.Enabled {
			v = 1
		}
		if _, err := h.DB.ExecContext(c.Request.Context(),
			`UPDATE mcp_tokens SET enabled=? WHERE id=?`, v, id); err != nil {
			httpx.Fail(c, httpx.CodeInternal, err, nil)
			return
		}
	}
	if in.RoleCode != nil {
		// 同 Create：不允许把角色改成空
		if *in.RoleCode == "" {
			httpx.FailKey(c, httpx.CodeBadRequest, "error.mcpTokenNeedsRole", nil, nil)
			return
		}
		if _, err := h.DB.ExecContext(c.Request.Context(),
			`UPDATE mcp_tokens SET role_code=? WHERE id=?`, *in.RoleCode, id); err != nil {
			httpx.Fail(c, httpx.CodeInternal, err, nil)
			return
		}
	}
	if in.ExpiresAt != nil {
		exp, perr := parseTokenExpiry(*in.ExpiresAt)
		if perr != nil {
			httpx.FailKey(c, httpx.CodeBadRequest, "error.mcpTokenBadExpiry", perr, nil)
			return
		}
		if _, err := h.DB.ExecContext(c.Request.Context(),
			`UPDATE mcp_tokens SET expires_at=? WHERE id=?`, exp, id); err != nil {
			httpx.Fail(c, httpx.CodeInternal, err, nil)
			return
		}
	}
	SetAuditTarget(c, "token#"+id)
	logx.J("mcp", "token_updated", map[string]any{
		"by": UsernameFromCtx(c), "id": id, "enabled": in.Enabled, "role": in.RoleCode,
		"expires_at": in.ExpiresAt,
	})
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// DeleteToken DELETE /api/mcp/tokens/:id
//
//	@Summary		删除 MCP 令牌
//	@Description	立即失效，用它的接入方下一次调用就会 401。
//	@Tags			mcp
//	@Produce		json
//	@Success		200	{object}	map[string]any
//	@Router			/mcp/tokens/{id} [delete]
func (h *MCPHandler) DeleteToken(c *gin.Context) {
	id := c.Param("id")
	var name string
	h.DB.QueryRowContext(c.Request.Context(), `SELECT name FROM mcp_tokens WHERE id=?`, id).Scan(&name)
	if _, err := h.DB.ExecContext(c.Request.Context(), `DELETE FROM mcp_tokens WHERE id=?`, id); err != nil {
		httpx.Fail(c, httpx.CodeInternal, err, nil)
		return
	}
	SetAuditTarget(c, name)
	logx.J("mcp", "token_deleted", map[string]any{"by": UsernameFromCtx(c), "id": id, "name": name})
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// resolveToken 按明文令牌查出这条接入的身份。
//
// 用哈希查，不做全表遍历比较：令牌哈希上有唯一索引，一次索引命中。
// （常数时间比较在这里没有意义——哈希本身已经隔断了逐字节试探。）
func (h *MCPHandler) resolveToken(raw string) (id int, name, role string, ok bool) {
	if raw == "" {
		return 0, "", "", false
	}
	sum := sha256.Sum256([]byte(raw))
	var enabled int
	var expires sql.NullTime
	err := h.DB.QueryRow(
		`SELECT id, name, role_code, enabled, expires_at FROM mcp_tokens WHERE token_hash=?`,
		hex.EncodeToString(sum[:])).Scan(&id, &name, &role, &enabled, &expires)
	if err != nil || enabled == 0 {
		return 0, "", "", false
	}
	// 🔴 过期即拒。有效期不落到这里，那它就只是界面上的一句话 ——
	//	而"界面写着已过期、实际照常能用"比没有有效期更糟
	if expires.Valid && time.Now().After(expires.Time) {
		logx.J("mcp", "token_expired", map[string]any{
			"id": id, "name": name, "expired_at": expires.Time.Format(time.RFC3339),
			"hint": "令牌已过期，调用被拒。去「AI 接入」页续期或新建一条",
		})
		return 0, "", "", false
	}
	return id, name, role, true
}

// parseTokenExpiry 解析有效期。空 = 永久（NULL）。
//
//	⚠️ 不接受**过去**的时刻：那等于建一条生下来就是死的令牌，
//	而调用方会拿着它调半天，只看到 401，查不出为什么。
//	（一个不会让人停下来的检查等于没有检查——这里让它在创建时就停下来。）
func parseTokenExpiry(s string) (any, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, nil
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		// 也接受 `2026-12-31` 这种只有日期的写法（界面上的日期选择器给的就是它）
		t, err = time.ParseInLocation("2006-01-02", s, time.Local)
		if err != nil {
			return nil, fmt.Errorf("有效期格式不对（要 2026-12-31 或 RFC3339）：%s", s)
		}
		// 只给日期时按当天结束算，否则"选今天"等于立刻过期
		t = t.Add(24*time.Hour - time.Second)
	}
	if t.Before(time.Now()) {
		return nil, fmt.Errorf("有效期 %s 已经是过去的时刻——这样建出来的令牌一次都用不了",
			t.Format("2006-01-02 15:04"))
	}
	return t, nil
}

// touchToken 记一次使用。
//
// 异步、失败不影响调用：这是运营信息，不是鉴权的一部分。
// 让它挡住工具调用的话，一次数据库抖动就会让 AI 拿不到数据。
func (h *MCPHandler) touchToken(id int, ip string) {
	go func() {
		if _, err := h.DB.Exec(
			`UPDATE mcp_tokens SET last_used_at=NOW(), last_used_ip=? WHERE id=?`, ip, id); err != nil {
			logx.J("mcp", "touch_token_failed", map[string]any{"id": id, "err": err.Error()})
		}
	}()
}
