package handlers

import (
	"database/sql"
	"fmt"
	"log"
	"net/http"
)

// DomainMeta API 文档业务域元信息
type DomainMeta struct {
	Code        string `json:"code"`
	Name        string `json:"name"`
	Description string `json:"description"`
	SortOrder   int    `json:"sort_order"`
}

// apiDomainMeta 所有 API 业务域。
// 未来新增业务域时往这个数组追加一行；启动迁移会自动 INSERT IGNORE
// 对应的 menu:api_docs:<code> 和 api_docs:try_it:<code> 两条权限码。
var apiDomainMeta = []DomainMeta{
	{
		Code:        "table_maintenance",
		Name:        "桌台维护记录",
		Description: "桌台维护、操作日志、截图等记录的接入接口",
		SortOrder:   1,
	},
}

// EnsureAPIDocsPermissions 启动时确保 API 文档相关权限码存在。
// 由 main.go 在 database.InitDB() 之后调用一次。
func EnsureAPIDocsPermissions(db *sql.DB) {
	// 总菜单权限（固定一条）
	db.Exec(`INSERT IGNORE INTO permissions (id, code, name, type, sort_order, description) VALUES (?, ?, ?, ?, ?, ?)`,
		"perm_menu_api_docs",
		"menu:api_docs",
		"[接口文档] 菜单可见",
		"menu",
		200,
		`可以看到侧栏的"接口文档"菜单项`)

	// 每个业务域 2 条
	for i, d := range apiDomainMeta {
		db.Exec(`INSERT IGNORE INTO permissions (id, code, name, type, sort_order, description) VALUES (?, ?, ?, ?, ?, ?)`,
			"perm_menu_api_docs_"+d.Code,
			"menu:api_docs:"+d.Code,
			fmt.Sprintf("[接口文档] 查看 %s 文档", d.Name),
			"menu",
			201+i*2,
			fmt.Sprintf("允许查看 %s 业务域的 API 文档", d.Name))
		db.Exec(`INSERT IGNORE INTO permissions (id, code, name, type, sort_order, description) VALUES (?, ?, ?, ?, ?, ?)`,
			"perm_api_docs_try_it_"+d.Code,
			"api_docs:try_it:"+d.Code,
			fmt.Sprintf("[接口文档] 在 %s 文档调试", d.Name),
			"button",
			202+i*2,
			fmt.Sprintf("允许在 %s 业务域的 API 文档中使用 Try it out 调试", d.Name))
	}
	log.Printf("[Migration] api_docs permissions ensured (domains=%d)", len(apiDomainMeta))
}

// DomainResponseItem 业务域响应
type DomainResponseItem struct {
	Code        string `json:"code"`
	Name        string `json:"name"`
	Description string `json:"description"`
	SortOrder   int    `json:"sort_order"`
	CanTryIt    bool   `json:"can_try_it"`
}

// HandleGetAPIDocsDomains 返回当前 JWT 用户可见的业务域列表
// @Summary      获取当前用户可见的 API 文档业务域
// @Tags         api_docs
// @Produce      json
// @Success      200  {array}   DomainResponseItem
// @Failure      401  {object}  ErrorResponse
// @Router       /api-docs/domains [get]
func HandleGetAPIDocsDomains(w http.ResponseWriter, r *http.Request) {
	_, username, role := GetUserFromContext(r)
	if username == "" {
		sendError(w, "未登录", http.StatusUnauthorized)
		return
	}
	isSuperAdmin := role == "super_admin" || role == "admin" || role == "超级管理员"

	out := make([]DomainResponseItem, 0, len(apiDomainMeta))
	for _, d := range apiDomainMeta {
		menuCode := "menu:api_docs:" + d.Code
		tryCode := "api_docs:try_it:" + d.Code

		canSee := isSuperAdmin
		if !canSee {
			if ok, _ := UserHasPermission(username, role, menuCode); ok {
				canSee = true
			}
		}
		if !canSee {
			continue
		}

		canTry := isSuperAdmin
		if !canTry {
			if ok, _ := UserHasPermission(username, role, tryCode); ok {
				canTry = true
			}
		}

		out = append(out, DomainResponseItem{
			Code:        d.Code,
			Name:        d.Name,
			Description: d.Description,
			SortOrder:   d.SortOrder,
			CanTryIt:    canTry,
		})
	}
	respondJSON(w, http.StatusOK, out)
}

// apiDomainHasMenuPermission 查当前请求用户对某业务域 menu 权限
func apiDomainHasMenuPermission(r *http.Request, domain string) bool {
	_, username, role := GetUserFromContext(r)
	if username == "" {
		return false
	}
	if role == "super_admin" || role == "admin" || role == "超级管理员" {
		return true
	}
	ok, _ := UserHasPermission(username, role, "menu:api_docs:"+domain)
	return ok
}

// apiDomainCanTryIt 查当前请求用户对某业务域 try_it 权限
func apiDomainCanTryIt(r *http.Request, domain string) bool {
	_, username, role := GetUserFromContext(r)
	if username == "" {
		return false
	}
	if role == "super_admin" || role == "admin" || role == "超级管理员" {
		return true
	}
	ok, _ := UserHasPermission(username, role, "api_docs:try_it:"+domain)
	return ok
}
