package handlers

import (
	"database/sql"
	"net/http"

	"github.com/gin-gonic/gin"
)

// BasicHandler 基础配置：项目 / 环境 独立维护。
type BasicHandler struct {
	DB *sql.DB
}

func NewBasicHandler(db *sql.DB) *BasicHandler { return &BasicHandler{DB: db} }

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
	q := `SELECT id, scope, label, color, sort_order FROM lifecycle_statuses`
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
		ID        int    `json:"id"`
		Scope     string `json:"scope"`
		Label     string `json:"label"`
		Color     string `json:"color"`
		SortOrder int    `json:"sort_order"`
	}
	out := []s{}
	for rows.Next() {
		var x s
		if rows.Scan(&x.ID, &x.Scope, &x.Label, &x.Color, &x.SortOrder) == nil {
			out = append(out, x)
		}
	}
	c.JSON(http.StatusOK, out)
}

func (h *BasicHandler) CreateStatus(c *gin.Context) {
	var in struct {
		Scope     string `json:"scope"`
		Label     string `json:"label"`
		Color     string `json:"color"`
		SortOrder int    `json:"sort_order"`
	}
	if err := c.ShouldBindJSON(&in); err != nil || in.Label == "" || (in.Scope != "project" && in.Scope != "domain") {
		c.JSON(400, gin.H{"error": "scope(project/domain) 与 label 必填"})
		return
	}
	res, err := h.DB.Exec(`INSERT INTO lifecycle_statuses (scope, label, color, sort_order) VALUES (?, ?, ?, ?)`, in.Scope, in.Label, in.Color, in.SortOrder)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	id, _ := res.LastInsertId()
	c.JSON(201, gin.H{"id": id})
}

func (h *BasicHandler) UpdateStatus(c *gin.Context) {
	var in struct {
		Label     string `json:"label"`
		Color     string `json:"color"`
		SortOrder int    `json:"sort_order"`
	}
	if err := c.ShouldBindJSON(&in); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	if _, err := h.DB.Exec(`UPDATE lifecycle_statuses SET label=?, color=?, sort_order=? WHERE id=?`, in.Label, in.Color, in.SortOrder, c.Param("id")); err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"ok": true})
}

func (h *BasicHandler) DeleteStatus(c *gin.Context) {
	if _, err := h.DB.Exec(`DELETE FROM lifecycle_statuses WHERE id=?`, c.Param("id")); err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"ok": true})
}

// ---- 项目 ----

func (h *BasicHandler) ListProjects(c *gin.Context) {
	rows, err := h.DB.Query(`SELECT id, name, remark, color, sort_order, status FROM projects ORDER BY sort_order, id`)
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
	var in struct {
		Name      string `json:"name"`
		Remark    string `json:"remark"`
		Color     string `json:"color"`
		SortOrder int    `json:"sort_order"`
		Status    string `json:"status"`
	}
	if err := c.ShouldBindJSON(&in); err != nil || in.Name == "" {
		c.JSON(400, gin.H{"error": "name 必填"})
		return
	}
	res, err := h.DB.Exec(`INSERT INTO projects (name, remark, color, sort_order, status) VALUES (?, ?, ?, ?, ?)`, in.Name, in.Remark, in.Color, in.SortOrder, in.Status)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	id, _ := res.LastInsertId()
	c.JSON(201, gin.H{"id": id})
}

func (h *BasicHandler) UpdateProject(c *gin.Context) {
	var in struct {
		Name      string `json:"name"`
		Remark    string `json:"remark"`
		Color     string `json:"color"`
		SortOrder int    `json:"sort_order"`
		Status    string `json:"status"`
	}
	if err := c.ShouldBindJSON(&in); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	if _, err := h.DB.Exec(`UPDATE projects SET name=?, remark=?, color=?, sort_order=?, status=? WHERE id=?`, in.Name, in.Remark, in.Color, in.SortOrder, in.Status, c.Param("id")); err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"ok": true})
}

func (h *BasicHandler) DeleteProject(c *gin.Context) {
	if _, err := h.DB.Exec(`DELETE FROM projects WHERE id=?`, c.Param("id")); err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"ok": true})
}

// ---- 环境 ----

func (h *BasicHandler) ListEnvs(c *gin.Context) {
	rows, err := h.DB.Query(`SELECT id, code, name, tag_type, color, sort_order FROM environments ORDER BY sort_order, id`)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	defer rows.Close()
	type e struct {
		ID        int    `json:"id"`
		Code      string `json:"code"`
		Name      string `json:"name"`
		TagType   string `json:"tag_type"`
		Color     string `json:"color"`
		SortOrder int    `json:"sort_order"`
	}
	out := []e{}
	for rows.Next() {
		var x e
		if rows.Scan(&x.ID, &x.Code, &x.Name, &x.TagType, &x.Color, &x.SortOrder) == nil {
			out = append(out, x)
		}
	}
	c.JSON(http.StatusOK, out)
}

func (h *BasicHandler) CreateEnv(c *gin.Context) {
	var in struct {
		Code      string `json:"code"`
		Name      string `json:"name"`
		TagType   string `json:"tag_type"`
		Color     string `json:"color"`
		SortOrder int    `json:"sort_order"`
	}
	if err := c.ShouldBindJSON(&in); err != nil || in.Code == "" {
		c.JSON(400, gin.H{"error": "code 必填"})
		return
	}
	if in.TagType == "" {
		in.TagType = "info"
	}
	res, err := h.DB.Exec(`INSERT INTO environments (code, name, tag_type, color, sort_order) VALUES (?, ?, ?, ?, ?)`, in.Code, in.Name, in.TagType, in.Color, in.SortOrder)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	id, _ := res.LastInsertId()
	c.JSON(201, gin.H{"id": id})
}

func (h *BasicHandler) UpdateEnv(c *gin.Context) {
	var in struct {
		Code      string `json:"code"`
		Name      string `json:"name"`
		TagType   string `json:"tag_type"`
		Color     string `json:"color"`
		SortOrder int    `json:"sort_order"`
	}
	if err := c.ShouldBindJSON(&in); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	if _, err := h.DB.Exec(`UPDATE environments SET code=?, name=?, tag_type=?, color=?, sort_order=? WHERE id=?`, in.Code, in.Name, in.TagType, in.Color, in.SortOrder, c.Param("id")); err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"ok": true})
}

func (h *BasicHandler) DeleteEnv(c *gin.Context) {
	if _, err := h.DB.Exec(`DELETE FROM environments WHERE id=?`, c.Param("id")); err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"ok": true})
}

// ---- CDN 厂商 ----

func (h *BasicHandler) ListCdns(c *gin.Context) {
	rows, err := h.DB.Query(`SELECT id, name, sort_order FROM cdns ORDER BY sort_order, id`)
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
	var in struct {
		Name      string `json:"name"`
		SortOrder int    `json:"sort_order"`
	}
	if err := c.ShouldBindJSON(&in); err != nil || in.Name == "" {
		c.JSON(400, gin.H{"error": "name 必填"})
		return
	}
	res, err := h.DB.Exec(`INSERT INTO cdns (name, sort_order) VALUES (?, ?)`, in.Name, in.SortOrder)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	id, _ := res.LastInsertId()
	c.JSON(201, gin.H{"id": id})
}

func (h *BasicHandler) UpdateCdn(c *gin.Context) {
	var in struct {
		Name      string `json:"name"`
		SortOrder int    `json:"sort_order"`
	}
	if err := c.ShouldBindJSON(&in); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	if _, err := h.DB.Exec(`UPDATE cdns SET name=?, sort_order=? WHERE id=?`, in.Name, in.SortOrder, c.Param("id")); err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"ok": true})
}

func (h *BasicHandler) DeleteCdn(c *gin.Context) {
	if _, err := h.DB.Exec(`DELETE FROM cdns WHERE id=?`, c.Param("id")); err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"ok": true})
}
