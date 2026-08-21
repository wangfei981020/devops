package handlers

import (
	"database/sql"
	"errors"
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"

	"ops-cmdb-backend/internal/httpx"
	"ops-cmdb-backend/internal/store"
)

// BasicHandler 基础配置：项目 / 环境 独立维护。
type BasicHandler struct {
	// 迁移期同时持有：Store 用于有租户语义的读写，DB 供尚未迁移的内部函数。
	Store *store.Store
	DB    *sql.DB
}

func NewBasicHandler(st *store.Store, db *sql.DB) *BasicHandler {
	return &BasicHandler{Store: st, DB: db}
}

func (h *BasicHandler) Register(r *gin.RouterGroup) {
	r.GET("/projects", h.ListProjects)
	r.POST("/projects", h.CreateProject)
	r.PUT("/projects/:id", h.UpdateProject)
	r.DELETE("/projects/:id", h.DeleteProject)
	r.GET("/environments", h.ListEnvs)
	r.POST("/environments", h.CreateEnv)
	r.PUT("/environments/:id", h.UpdateEnv)
	r.DELETE("/environments/:id", h.DeleteEnv)
	r.GET("/cdns", h.ListCdns)
	r.POST("/cdns", h.CreateCdn)
	r.PUT("/cdns/:id", h.UpdateCdn)
	r.DELETE("/cdns/:id", h.DeleteCdn)
	// 生命周期状态字典（可自定义）：scope=project/domain
	r.GET("/lifecycle-statuses", h.ListStatuses)
	r.POST("/lifecycle-statuses", h.CreateStatus)
	r.PUT("/lifecycle-statuses/:id", h.UpdateStatus)
	r.DELETE("/lifecycle-statuses/:id", h.DeleteStatus)
}

// ---- 生命周期状态字典 ----

func (h *BasicHandler) ListStatuses(c *gin.Context) {
	scope := c.Query("scope")
	q := `SELECT id, scope, label, COALESCE(label_en,''), color, sort_order FROM lifecycle_statuses`
	args := []any{}
	if scope != "" {
		q += ` WHERE scope=?`
		args = append(args, scope)
	}
	q += ` ORDER BY scope, sort_order, id`
	rows, err := h.DB.Query(q, args...)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	defer rows.Close()
	type s struct {
		ID    int    `json:"id"`
		Scope string `json:"scope"`
		Label string `json:"label"`
		// LabelEn 英文显示名。空 = 没填，界面回退显示中文（见 environments.name_en 的说明）
		LabelEn   string `json:"label_en"`
		Color     string `json:"color"`
		SortOrder int    `json:"sort_order"`
	}
	out := []s{}
	for rows.Next() {
		var x s
		if rows.Scan(&x.ID, &x.Scope, &x.Label, &x.LabelEn, &x.Color, &x.SortOrder) == nil {
			out = append(out, x)
		}
	}
	c.JSON(http.StatusOK, out)
}

func (h *BasicHandler) CreateStatus(c *gin.Context) {
	sc, err := h.Store.Tenant(c.Request.Context())
	if err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	var in struct {
		Scope     string `json:"scope"`
		Label     string `json:"label"`
		Color     string `json:"color"`
		SortOrder int    `json:"sort_order"`
	}
	if err := c.ShouldBindJSON(&in); err != nil || in.Label == "" || (in.Scope != "project" && in.Scope != "domain") {
		httpx.RequiredAll(c, "scope", "label")
		return
	}
	res, err := sc.Insert(`INSERT INTO lifecycle_statuses (tenant_id, scope, label, color, sort_order) VALUES (?, ?, ?, ?, ?)`, in.Scope, in.Label, in.Color, in.SortOrder)
	if err != nil {
		// 重名是用户输错了，不是服务端故障：报 500 会让人去找运维，
		// 而原始的 "Duplicate entry 'x' for key 'lifecycle_statuses.code'" 既泄露表结构又看不懂
		if isDupKeyErr(err) {
			failDuplicate(c, "生命周期状态", in.Label, 0)
			return
		}
		httpx.Fail(c, httpx.CodeInternal, errors.New(SafeErr("新增生命周期状态", err)), nil)
		return
	}
	id, _ := res.LastInsertId()
	AuditCreated(c, "lifecycle_statuses", id)
	c.JSON(201, gin.H{"id": id})
}

func (h *BasicHandler) UpdateStatus(c *gin.Context) {
	sc, err := h.Store.Tenant(c.Request.Context())
	if err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	// 🔴 指针 = 三态：没传（不动它）/ 显式清空 / 改成它。
	//	用普通类型的话，只想改排序就会把名字一起清空（OPSCMDB-083）。
	var in struct {
		Label     *string `json:"label"`
		Color     *string `json:"color"`
		SortOrder *int    `json:"sort_order"`
	}
	if err := c.ShouldBindJSON(&in); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	if requireNonBlank(c, "label", in.Label) {
		return
	}
	p := &patchSet{}
	p.Add("label", in.Label)
	p.Add("color", in.Color)
	p.Add("sort_order", in.SortOrder)
	if p.Empty() {
		httpx.Invalid(c, "body", "至少要传一个要改的字段")
		return
	}
	if _, err := sc.Exec(`UPDATE lifecycle_statuses SET `+p.SQL()+` WHERE tenant_id = ? AND id=?`,
		append(p.Args(), c.Param("id"))...); err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"ok": true})
}

func (h *BasicHandler) DeleteStatus(c *gin.Context) {
	sc, err := h.Store.Tenant(c.Request.Context())
	if err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	if _, err := sc.Exec(`DELETE FROM lifecycle_statuses WHERE tenant_id = ? AND id=?`, c.Param("id")); err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"ok": true})
}

// ---- 项目 ----

func (h *BasicHandler) ListProjects(c *gin.Context) {
	sc, err := h.Store.Tenant(c.Request.Context())
	if err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	rows, err := sc.Query(`SELECT id, name, remark, color, sort_order, status FROM projects WHERE tenant_id = ? ORDER BY sort_order, id`)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	defer rows.Close()
	type p struct {
		ID        int    `json:"id"`
		Name      string `json:"name"`
		Remark    string `json:"remark"`
		Color     string `json:"color"`
		SortOrder int    `json:"sort_order"`
		Status    string `json:"status"`
	}
	out := []p{}
	for rows.Next() {
		var x p
		if rows.Scan(&x.ID, &x.Name, &x.Remark, &x.Color, &x.SortOrder, &x.Status) == nil {
			out = append(out, x)
		}
	}
	c.JSON(http.StatusOK, out)
}

func (h *BasicHandler) CreateProject(c *gin.Context) {
	sc, err := h.Store.Tenant(c.Request.Context())
	if err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	var in struct {
		Name      string `json:"name"`
		Remark    string `json:"remark"`
		Color     string `json:"color"`
		SortOrder int    `json:"sort_order"`
		Status    string `json:"status"`
	}
	if err := c.ShouldBindJSON(&in); err != nil || in.Name == "" {
		httpx.Required(c, "name")
		return
	}
	res, err := sc.Insert(`INSERT INTO projects (tenant_id, name, remark, color, sort_order, status) VALUES (?, ?, ?, ?, ?, ?)`, in.Name, in.Remark, in.Color, in.SortOrder, in.Status)
	if err != nil {
		// 重名是用户输错了，不是服务端故障：报 500 会让人去找运维，
		// 而原始的 "Duplicate entry 'x' for key 'projects.code'" 既泄露表结构又看不懂
		if isDupKeyErr(err) {
			failDuplicate(c, "项目", in.Name, 0)
			return
		}
		httpx.Fail(c, httpx.CodeInternal, errors.New(SafeErr("新增项目", err)), nil)
		return
	}
	id, _ := res.LastInsertId()
	AuditCreated(c, "projects", id)
	c.JSON(201, gin.H{"id": id})
}

func (h *BasicHandler) UpdateProject(c *gin.Context) {
	sc, err := h.Store.Tenant(c.Request.Context())
	if err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	// 🔴 指针 = 三态：没传（不动它）/ 显式清空 / 改成它。
	//	用普通类型的话，只想改排序就会把名字一起清空（OPSCMDB-083）。
	var in struct {
		Name      *string `json:"name"`
		Remark    *string `json:"remark"`
		Color     *string `json:"color"`
		SortOrder *int    `json:"sort_order"`
		Status    *string `json:"status"`
	}
	if err := c.ShouldBindJSON(&in); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	if requireNonBlank(c, "name", in.Name) {
		return
	}
	p := &patchSet{}
	p.Add("name", in.Name)
	p.Add("remark", in.Remark)
	p.Add("color", in.Color)
	p.Add("sort_order", in.SortOrder)
	p.Add("status", in.Status)
	if p.Empty() {
		httpx.Invalid(c, "body", "至少要传一个要改的字段")
		return
	}
	if _, err := sc.Exec(`UPDATE projects SET `+p.SQL()+` WHERE tenant_id = ? AND id=?`,
		append(p.Args(), c.Param("id"))...); err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"ok": true})
}

func (h *BasicHandler) DeleteProject(c *gin.Context) {
	sc, err := h.Store.Tenant(c.Request.Context())
	if err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	// 同 DeleteEnv：引用按 code 存，删掉之后引用方不会跟着变
	// ⚠️ projects 表**没有 code 列**，引用方存的是 name。
	// 照搬 environments 的写法（SELECT code）会让每次删除都返回"项目不存在"
	var code string
	if err := sc.QueryRow(`SELECT name FROM projects WHERE tenant_id = ? AND id=?`, c.Param("id")).Scan(&code); err != nil {
		httpx.NotFound(c, "project")
		return
	}
	if n, where := countRefs(sc, projectRefs, code); n != 0 {
		if n < 0 {
			httpx.FailKey(c, httpx.CodeInternal, "error.cannotVerifyProjectUsage", errors.New(where), nil)
			return
		}
		httpx.FailKey(c, httpx.CodeConflict, "error.projectInUse", nil,
			map[string]any{"count": n, "name": code, "where": where})
		return
	}
	if _, err := sc.Exec(`DELETE FROM projects WHERE tenant_id = ? AND id=?`, c.Param("id")); err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"ok": true})
}

// ---- 环境 ----

func (h *BasicHandler) ListEnvs(c *gin.Context) {
	sc, err := h.Store.Tenant(c.Request.Context())
	if err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	rows, err := sc.Query(`SELECT id, code, name, COALESCE(name_en,''), tag_type, color, sort_order FROM environments WHERE tenant_id = ? ORDER BY sort_order, id`)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	defer rows.Close()
	type e struct {
		ID   int    `json:"id"`
		Code string `json:"code"`
		Name string `json:"name"`
		// NameEn 英文显示名。空 = 没填，界面回退显示中文名。
		//
		//	⚠️ 这是**数据**不是文案：这些字典客户可以自己增删改，
		//	他新建的「灰度」环境语言包里不可能有。所以英文名必须存库。
		//	（英文界面下枚举值全是中文 —— OPSCMDB-031 P1-76）
		NameEn    string `json:"name_en"`
		TagType   string `json:"tag_type"`
		Color     string `json:"color"`
		SortOrder int    `json:"sort_order"`
	}
	out := []e{}
	for rows.Next() {
		var x e
		if rows.Scan(&x.ID, &x.Code, &x.Name, &x.NameEn, &x.TagType, &x.Color, &x.SortOrder) == nil {
			out = append(out, x)
		}
	}
	c.JSON(http.StatusOK, out)
}

func (h *BasicHandler) CreateEnv(c *gin.Context) {
	sc, err := h.Store.Tenant(c.Request.Context())
	if err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	var in struct {
		Code string `json:"code"`
		Name string `json:"name"`
		// NameEn 可选。留空则英文界面回退显示中文名 ——
		// 绝大多数客户只用一种语言，不该逼他们每个字典项填两遍
		NameEn    string `json:"name_en"`
		TagType   string `json:"tag_type"`
		Color     string `json:"color"`
		SortOrder int    `json:"sort_order"`
	}
	if err := c.ShouldBindJSON(&in); err != nil || in.Code == "" {
		httpx.Required(c, "code")
		return
	}
	if in.TagType == "" {
		in.TagType = "info"
	}
	res, err := sc.Insert(`INSERT INTO environments (tenant_id, code, name, name_en, tag_type, color, sort_order) VALUES (?, ?, ?, ?, ?, ?, ?)`, in.Code, in.Name, in.NameEn, in.TagType, in.Color, in.SortOrder)
	if err != nil {
		// 重名是用户输错了，不是服务端故障：报 500 会让人去找运维，
		// 而原始的 "Duplicate entry 'x' for key 'environments.code'" 既泄露表结构又看不懂
		if isDupKeyErr(err) {
			failDuplicate(c, "环境", in.Code, 0)
			return
		}
		httpx.Fail(c, httpx.CodeInternal, errors.New(SafeErr("新增环境", err)), nil)
		return
	}
	id, _ := res.LastInsertId()
	AuditCreated(c, "environments", id)
	c.JSON(201, gin.H{"id": id})
}

func (h *BasicHandler) UpdateEnv(c *gin.Context) {
	sc, err := h.Store.Tenant(c.Request.Context())
	if err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	// 🔴 指针 = 三态：没传（不动它）/ 显式清空 / 改成它。
	//	用普通类型的话，只想改排序就会把名字一起清空（OPSCMDB-083）。
	var in struct {
		Code      *string `json:"code"`
		Name      *string `json:"name"`
		NameEn    *string `json:"name_en"`
		TagType   *string `json:"tag_type"`
		Color     *string `json:"color"`
		SortOrder *int    `json:"sort_order"`
	}
	if err := c.ShouldBindJSON(&in); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	if requireNonBlank(c, "code", in.Code) {
		return
	}
	if requireNonBlank(c, "name", in.Name) {
		return
	}
	p := &patchSet{}
	p.Add("code", in.Code)
	p.Add("name", in.Name)
	p.Add("name_en", in.NameEn)
	p.Add("tag_type", in.TagType)
	p.Add("color", in.Color)
	p.Add("sort_order", in.SortOrder)
	if p.Empty() {
		httpx.Invalid(c, "body", "至少要传一个要改的字段")
		return
	}
	if _, err := sc.Exec(`UPDATE environments SET `+p.SQL()+` WHERE tenant_id = ? AND id=?`,
		append(p.Args(), c.Param("id"))...); err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"ok": true})
}

// countRefs 数一数还有多少东西在用这个字典值。
//
// # 为什么删除必须先数
//
// 字典项被删掉之后，引用它的资产**不会跟着变** —— `hosts.project` 里那个
// 字符串还在，只是筛选下拉里再也没有这一项了。表现是：那批资产
// 从筛选视图里消失，列表里却还在，没有任何报错。
// 想把它们找回来只能靠人记得原来叫什么。
//
// 所以删除前必须回答"还有谁在用"，并且把数字报给用户 ——
// 光说"不能删"没用，他得知道要先去改多少条。
func countRefs(sc *store.Scoped, pairs [][2]string, value string) (int, string) {
	total, first := 0, ""
	for _, p := range pairs {
		table, col := p[0], p[1]
		var n int
		// #nosec G201 -- table/col 来自下面写死的常量表，不是用户输入
		q := fmt.Sprintf("SELECT COUNT(*) FROM %s WHERE tenant_id = ? AND %s = ?", table, col)
		if err := sc.QueryRow(q, value).Scan(&n); err != nil {
			// 数不出来时**不放行**：这时候"没有引用"这个结论并不成立
			return -1, table
		}
		if n > 0 {
			total += n
			if first == "" {
				first = table
			}
		}
	}
	return total, first
}

// envRefs / projectRefs 引用这两个字典的表。
//
// ⚠️ 加新表时要记得补进来 —— 漏一张的后果是那张表的资产在字典项被删后
// 静默失联。没有办法自动发现（列名不统一：environment / env 都有）。
var envRefs = [][2]string{
	{"cis", "env"}, {"k8s_clusters", "environment"},
	{"domain_records", "env"}, {"harbor_registries", "env"},
}

// ⚠️ projects 这张字典存的是**业务项目**（"游戏中台"、"支付"），
// 引用它的只有 k8s_ns_project.project。
//
// `hosts.project` / `cis.project` / `cloud_*.project` 看着同名，
// 存的却是**云项目 ID**（g32-prod、infra-01）—— 完全是另一套东西。
// 第一版把它们都算成引用，结果会是：删一个从没被用过的业务项目，
// 被告知"还有 9 条资产在用"，而那 9 条和它毫无关系。
// 列名相同不等于语义相同，这个必须查数据确认，不能靠名字推。
var projectRefs = [][2]string{
	{"k8s_ns_project", "project"},
}

func (h *BasicHandler) DeleteEnv(c *gin.Context) {
	sc, err := h.Store.Tenant(c.Request.Context())
	if err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	// 先查出 code：引用是按 code 存的，不是按 id
	var code string
	if err := sc.QueryRow(`SELECT code FROM environments WHERE tenant_id = ? AND id=?`, c.Param("id")).Scan(&code); err != nil {
		httpx.NotFound(c, "environment")
		return
	}
	if n, where := countRefs(sc, envRefs, code); n != 0 {
		if n < 0 {
			httpx.FailKey(c, httpx.CodeInternal, "error.cannotVerifyEnvUsage", errors.New(where), nil)
			return
		}
		// ⚠️ 走 httpx.FailKey 而不是裸 gin.H{"error": ...}：
		// 裸的那种前端归一不出来，detail 会退化成 "DELETE /api/environments/1 → 409"，
		// 而这句话里真正有用的是"还有 18 条"这个数字 —— 用户接下来的工作量。
		// 实测就是这么退化的（见 §2.1 最后一段）。
		httpx.FailKey(c, httpx.CodeConflict, "error.envInUse", nil,
			map[string]any{"count": n, "code": code, "where": where})
		return
	}
	if _, err := sc.Exec(`DELETE FROM environments WHERE tenant_id = ? AND id=?`, c.Param("id")); err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"ok": true})
}

// ---- CDN 厂商 ----

func (h *BasicHandler) ListCdns(c *gin.Context) {
	sc, err := h.Store.Tenant(c.Request.Context())
	if err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	rows, err := sc.Query(`SELECT id, name, sort_order FROM cdns WHERE tenant_id = ? ORDER BY sort_order, id`)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	defer rows.Close()
	type cdn struct {
		ID        int    `json:"id"`
		Name      string `json:"name"`
		SortOrder int    `json:"sort_order"`
	}
	out := []cdn{}
	for rows.Next() {
		var x cdn
		if rows.Scan(&x.ID, &x.Name, &x.SortOrder) == nil {
			out = append(out, x)
		}
	}
	c.JSON(http.StatusOK, out)
}

func (h *BasicHandler) CreateCdn(c *gin.Context) {
	sc, err := h.Store.Tenant(c.Request.Context())
	if err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	var in struct {
		Name      string `json:"name"`
		SortOrder int    `json:"sort_order"`
	}
	if err := c.ShouldBindJSON(&in); err != nil || in.Name == "" {
		httpx.Required(c, "name")
		return
	}
	res, err := sc.Insert(`INSERT INTO cdns (tenant_id, name, sort_order) VALUES (?, ?, ?)`, in.Name, in.SortOrder)
	if err != nil {
		// 重名是用户输错了，不是服务端故障：报 500 会让人去找运维，
		// 而原始的 "Duplicate entry 'x' for key 'cdns.code'" 既泄露表结构又看不懂
		if isDupKeyErr(err) {
			failDuplicate(c, "CDN 接入", in.Name, 0)
			return
		}
		httpx.Fail(c, httpx.CodeInternal, errors.New(SafeErr("新增CDN 接入", err)), nil)
		return
	}
	id, _ := res.LastInsertId()
	AuditCreated(c, "cdns", id)
	c.JSON(201, gin.H{"id": id})
}

func (h *BasicHandler) UpdateCdn(c *gin.Context) {
	sc, err := h.Store.Tenant(c.Request.Context())
	if err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	// 🔴 指针 = 三态：没传（不动它）/ 显式清空 / 改成它。
	//	用普通类型的话，只想改排序就会把名字一起清空（OPSCMDB-083）。
	var in struct {
		Name      *string `json:"name"`
		SortOrder *int    `json:"sort_order"`
	}
	if err := c.ShouldBindJSON(&in); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	if requireNonBlank(c, "name", in.Name) {
		return
	}
	p := &patchSet{}
	p.Add("name", in.Name)
	p.Add("sort_order", in.SortOrder)
	if p.Empty() {
		httpx.Invalid(c, "body", "至少要传一个要改的字段")
		return
	}
	if _, err := sc.Exec(`UPDATE cdns SET `+p.SQL()+` WHERE tenant_id = ? AND id=?`,
		append(p.Args(), c.Param("id"))...); err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"ok": true})
}

func (h *BasicHandler) DeleteCdn(c *gin.Context) {
	sc, err := h.Store.Tenant(c.Request.Context())
	if err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	if _, err := sc.Exec(`DELETE FROM cdns WHERE tenant_id = ? AND id=?`, c.Param("id")); err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"ok": true})
}
