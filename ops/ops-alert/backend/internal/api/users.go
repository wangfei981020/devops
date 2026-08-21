package api

import (
	"database/sql"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"

	"ops-alert-backend/internal/api/middleware"
)

// 用户与角色管理。
//
// 与 ops-cmdb 的同名模块保持同一套语义（最后一个管理员不能降权、
// 角色必须存在、不回显密码哈希），但有一处**故意不同**：
//
//	CMDB 改角色后要显式作废该用户的所有会话，因为它把权限快照留在会话里。
//	本产品的 loadPerms 每个请求都从库现取（见 perm.go 的说明），
//	所以降权下一个请求就生效，不需要踢人。
//	—— 这不是省事，是两边的权限读取方式本来就不同；
//	   如果哪天这里改成从 token 读权限，就必须把踢会话补回来。

func (s *Server) registerUsers(g *gin.RouterGroup) {
	g.GET("/users", s.listUsers)
	g.POST("/users", s.createUser)
	g.PUT("/users/:id/role", s.changeUserRole)
	g.PUT("/users/:id/status", s.changeUserStatus)
	g.PUT("/users/:id/password", s.resetUserPassword)
	g.DELETE("/users/:id", s.deleteUser)
	g.GET("/roles", s.listRoles)
}

func (s *Server) listUsers(c *gin.Context) {
	p := s.st.Platform("list_users")
	rows, err := p.Query(c.Request.Context(),
		`SELECT u.id, u.username, u.display_name, u.auth_source, u.role_code, u.status,
		        u.created_at, COALESCE(r.name, ''), COALESCE(r.unrestricted, 0)
		   FROM users u LEFT JOIN roles r ON r.code = u.role_code
		  WHERE u.deleted_at IS NULL ORDER BY u.id`)
	if err != nil {
		abortQuery(c, err)
		return
	}
	defer rows.Close()
	type item struct {
		ID       int64  `json:"id"`
		Username string `json:"username"`
		Display  string `json:"display_name"`
		Source   string `json:"auth_source"`
		RoleCode string `json:"role_code"`
		RoleName string `json:"role_name"`
		// 是否不受权限码约束。列表里要能一眼看出谁是管理员
		Unrestricted bool   `json:"unrestricted"`
		Status       string `json:"status"`
		CreatedAt    string `json:"created_at"`
	}
	out := []item{}
	for rows.Next() {
		var it item
		var unres int
		if err := rows.Scan(&it.ID, &it.Username, &it.Display, &it.Source, &it.RoleCode,
			&it.Status, &it.CreatedAt, &it.RoleName, &unres); err != nil {
			abortQuery(c, err)
			return
		}
		it.Unrestricted = unres == 1
		out = append(out, it)
	}
	if err := rows.Err(); err != nil {
		abortQuery(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": out})
}

// listRoles 角色清单。给用户管理页的下拉用，也给角色页展示权限。
//
// ⚠️ 角色**从库里读**，前端不许写死一份。CMDB 那边写死过，
// 结果界面上给出两个数据库里根本不存在的角色，选了保存报"角色不存在"，
// 而用户明明是从下拉里选的，会以为系统坏了。
func (s *Server) listRoles(c *gin.Context) {
	p := s.st.Platform("list_roles")
	rows, err := p.Query(c.Request.Context(),
		`SELECT r.code, r.name, r.description, r.is_builtin, r.unrestricted,
		        (SELECT COUNT(*) FROM role_permissions rp WHERE rp.role_code = r.code),
		        (SELECT COUNT(*) FROM users u WHERE u.role_code = r.code AND u.deleted_at IS NULL)
		   FROM roles r ORDER BY r.unrestricted DESC, r.code`)
	if err != nil {
		abortQuery(c, err)
		return
	}
	defer rows.Close()
	type role struct {
		Code      string `json:"code"`
		Name      string `json:"name"`
		Desc      string `json:"description"`
		IsBuiltin bool   `json:"is_builtin"`
		// ⚠️ unrestricted 的角色，perm_count 再小也不代表权限少 ——
		// 它根本不走权限码。界面上必须把这件事说清楚
		Unrestricted bool `json:"unrestricted"`
		PermCount    int  `json:"perm_count"`
		UserCount    int  `json:"user_count"`
	}
	out := []role{}
	for rows.Next() {
		var r role
		var builtin, unres int
		if err := rows.Scan(&r.Code, &r.Name, &r.Desc, &builtin, &unres, &r.PermCount, &r.UserCount); err != nil {
			abortQuery(c, err)
			return
		}
		r.IsBuiltin, r.Unrestricted = builtin == 1, unres == 1
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		abortQuery(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": out})
}

func (s *Server) roleExists(c *gin.Context, code string) bool {
	var n int
	p := s.st.Platform("role_exists")
	if err := p.QueryRow(c.Request.Context(),
		`SELECT COUNT(*) FROM roles WHERE code = ?`, code).Scan(&n); err != nil {
		return false
	}
	return n > 0
}

// lastAdminBlocked 判断这次操作会不会把最后一个不受限管理员弄没。
//
// ⚠️ 空 role_code 也算管理员候选人吗？**不算**。
// 008_rbac.sql 已把存量空角色收敛成 viewer，空值一律视为受限。
// 反过来算的话，一个空角色账号会被当成"还有管理员在"，
// 于是真正的管理员被降权，而那个空角色账号其实什么都干不了 —— 谁也进不来了。
func (s *Server) lastAdminBlocked(c *gin.Context, excludeID int64) bool {
	var others int
	p := s.st.Platform("last_admin")
	err := p.QueryRow(c.Request.Context(),
		`SELECT COUNT(*) FROM users u JOIN roles r ON r.code = u.role_code
		  WHERE u.deleted_at IS NULL AND u.status = 'active'
		    AND r.unrestricted = 1 AND u.id <> ?`, excludeID).Scan(&others)
	if err != nil {
		// 查不出来时按"会出事"处理：宁可拒绝一次合法操作，
		// 也不能因为一次数据库抖动把最后一个管理员放掉
		return true
	}
	return others == 0
}

func (s *Server) createUser(c *gin.Context) {
	var req struct {
		Username string `json:"username"`
		Display  string `json:"display_name"`
		Password string `json:"password"`
		RoleCode string `json:"role_code"`
	}
	if err := c.ShouldBindJSON(&req); err != nil ||
		strings.TrimSpace(req.Username) == "" || req.Password == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "bad_request",
			"detail": "用户名和密码必填"})
		return
	}
	if len(req.Password) < 8 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "weak_password",
			"detail": "密码至少 8 位"})
		return
	}
	if !s.roleExists(c, req.RoleCode) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "role_not_found", "detail": req.RoleCode})
		return
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		abortQuery(c, err)
		return
	}
	p := s.st.Platform("create_user")
	res, err := p.Exec(c.Request.Context(),
		`INSERT INTO users (username, display_name, password_hash, auth_source, role_code, status)
		 VALUES (?, ?, ?, 'local', ?, 'active')`,
		strings.TrimSpace(req.Username), req.Display, string(hash), req.RoleCode)
	if err != nil {
		abortQuery(c, err) // 重名走 1062 → 409，见 abortQuery
		return
	}
	id, _ := res.LastInsertId()
	// 新账号要挂到租户下，否则登录时租户解析失败，表现为"密码对但登不进去"
	if _, err := p.Exec(c.Request.Context(),
		`INSERT IGNORE INTO user_tenants (user_id, tenant_id, role_code)
		 SELECT ?, tenant_id, ? FROM user_tenants WHERE user_id = ? LIMIT 1`,
		id, req.RoleCode, middleware.CurrentUser(c).UserID); err != nil {
		abortLog(c, err)
	}
	s.auditPlatform(c, "user.create", id, gin.H{"username": req.Username, "role": req.RoleCode})
	c.JSON(http.StatusOK, gin.H{"id": id})
}

func (s *Server) changeUserRole(c *gin.Context) {
	id, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	var req struct {
		RoleCode string `json:"role_code"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || req.RoleCode == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "bad_request"})
		return
	}
	if !s.roleExists(c, req.RoleCode) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "role_not_found", "detail": req.RoleCode})
		return
	}
	// 降权前先确认不是最后一个管理员
	var unres int
	p := s.st.Platform("change_role")
	if err := p.QueryRow(c.Request.Context(),
		`SELECT COALESCE(r.unrestricted, 0) FROM users u LEFT JOIN roles r ON r.code = u.role_code
		  WHERE u.id = ? AND u.deleted_at IS NULL`, id).Scan(&unres); err == sql.ErrNoRows {
		c.JSON(http.StatusNotFound, gin.H{"error": "not_found"})
		return
	} else if err != nil {
		abortQuery(c, err)
		return
	}
	newIsAdmin := s.roleUnrestricted(c, req.RoleCode)
	if unres == 1 && !newIsAdmin && s.lastAdminBlocked(c, id) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "last_admin",
			"detail": "这是最后一个管理员，降权后将无人能管理本系统"})
		return
	}
	if _, err := p.Exec(c.Request.Context(),
		`UPDATE users SET role_code = ? WHERE id = ? AND deleted_at IS NULL`, req.RoleCode, id); err != nil {
		abortQuery(c, err)
		return
	}
	s.auditPlatform(c, "user.change_role", id, gin.H{"role": req.RoleCode})
	// 权限每请求现取，所以下一个请求就生效，不需要踢会话（见文件头说明）
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func (s *Server) roleUnrestricted(c *gin.Context, code string) bool {
	var n int
	p := s.st.Platform("role_unrestricted")
	if err := p.QueryRow(c.Request.Context(),
		`SELECT COALESCE(unrestricted, 0) FROM roles WHERE code = ?`, code).Scan(&n); err != nil {
		return false
	}
	return n == 1
}

func (s *Server) changeUserStatus(c *gin.Context) {
	id, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	var req struct {
		Status string `json:"status"`
	}
	if err := c.ShouldBindJSON(&req); err != nil ||
		(req.Status != "active" && req.Status != "disabled") {
		c.JSON(http.StatusBadRequest, gin.H{"error": "bad_request"})
		return
	}
	// 停用最后一个管理员和降权一样危险
	if req.Status == "disabled" && s.lastAdminBlocked(c, id) {
		var unres int
		p := s.st.Platform("check_admin")
		_ = p.QueryRow(c.Request.Context(),
			`SELECT COALESCE(r.unrestricted,0) FROM users u LEFT JOIN roles r ON r.code=u.role_code
			  WHERE u.id = ?`, id).Scan(&unres)
		if unres == 1 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "last_admin",
				"detail": "这是最后一个管理员，停用后将无人能管理本系统"})
			return
		}
	}
	p := s.st.Platform("change_status")
	if _, err := p.Exec(c.Request.Context(),
		`UPDATE users SET status = ? WHERE id = ? AND deleted_at IS NULL`, req.Status, id); err != nil {
		abortQuery(c, err)
		return
	}
	s.auditPlatform(c, "user.change_status", id, gin.H{"status": req.Status})
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func (s *Server) resetUserPassword(c *gin.Context) {
	id, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	var req struct {
		Password string `json:"password"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || len(req.Password) < 8 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "weak_password", "detail": "密码至少 8 位"})
		return
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		abortQuery(c, err)
		return
	}
	p := s.st.Platform("reset_password")
	if _, err := p.Exec(c.Request.Context(),
		`UPDATE users SET password_hash = ? WHERE id = ? AND deleted_at IS NULL AND auth_source = 'local'`,
		string(hash), id); err != nil {
		abortQuery(c, err)
		return
	}
	// ⚠️ 审计里只记"改过密码"，不记密码本身也不记哈希
	s.auditPlatform(c, "user.reset_password", id, gin.H{})
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func (s *Server) deleteUser(c *gin.Context) {
	id, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	if id == middleware.CurrentUser(c).UserID {
		c.JSON(http.StatusBadRequest, gin.H{"error": "self_delete",
			"detail": "不能删除自己"})
		return
	}
	if s.lastAdminBlocked(c, id) {
		var unres int
		p := s.st.Platform("check_admin")
		_ = p.QueryRow(c.Request.Context(),
			`SELECT COALESCE(r.unrestricted,0) FROM users u LEFT JOIN roles r ON r.code=u.role_code
			  WHERE u.id = ?`, id).Scan(&unres)
		if unres == 1 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "last_admin",
				"detail": "这是最后一个管理员，删除后将无人能管理本系统"})
			return
		}
	}
	p := s.st.Platform("delete_user")
	// 软删。审计里可能引用过这个账号，物理删除会让历史记录指向一个不存在的人。
	// deleted_at 非空后 alive 生成列变 NULL，同名账号可以再建（见 009 迁移）
	if _, err := p.Exec(c.Request.Context(),
		`UPDATE users SET deleted_at = NOW(3) WHERE id = ? AND deleted_at IS NULL`, id); err != nil {
		abortQuery(c, err)
		return
	}
	if _, err := p.Exec(c.Request.Context(),
		`DELETE FROM user_tenants WHERE user_id = ?`, id); err != nil {
		abortLog(c, err)
	}
	s.auditPlatform(c, "user.delete", id, gin.H{})
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// auditPlatform 记平台级操作的审计。
//
// 用户管理动的是平台表（users），但审计本身是租户表 —— 走当前租户即可：
// 谁在这个租户里改了账号，对该租户是可见的。
func (s *Server) auditPlatform(c *gin.Context, action string, targetID int64, detail gin.H) {
	sc, err := s.st.Tenant(c.Request.Context())
	if err != nil {
		abortLog(c, err)
		return
	}
	s.audit(c, sc, action, "user", targetID, detail)
}
