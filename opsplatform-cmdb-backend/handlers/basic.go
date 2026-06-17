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
}

// ---- 项目 ----

func (h *BasicHandler) ListProjects(c *gin.Context) {
	rows, err := h.DB.Query(`SELECT id, name, remark, sort_order FROM projects ORDER BY sort_order, id`)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	defer rows.Close()
	type p struct {
		ID        int    `json:"id"`
		Name      string `json:"name"`
		Remark    string `json:"remark"`
		SortOrder int    `json:"sort_order"`
	}
	out := []p{}
	for rows.Next() {
		var x p
		if rows.Scan(&x.ID, &x.Name, &x.Remark, &x.SortOrder) == nil {
			out = append(out, x)
		}
	}
	c.JSON(http.StatusOK, out)
}

func (h *BasicHandler) CreateProject(c *gin.Context) {
	var in struct {
		Name      string `json:"name"`
		Remark    string `json:"remark"`
		SortOrder int    `json:"sort_order"`
	}
	if err := c.ShouldBindJSON(&in); err != nil || in.Name == "" {
		c.JSON(400, gin.H{"error": "name 必填"})
		return
	}
	res, err := h.DB.Exec(`INSERT INTO projects (name, remark, sort_order) VALUES (?, ?, ?)`, in.Name, in.Remark, in.SortOrder)
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
		SortOrder int    `json:"sort_order"`
	}
	if err := c.ShouldBindJSON(&in); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	if _, err := h.DB.Exec(`UPDATE projects SET name=?, remark=?, sort_order=? WHERE id=?`, in.Name, in.Remark, in.SortOrder, c.Param("id")); err != nil {
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
	rows, err := h.DB.Query(`SELECT id, code, name, tag_type, sort_order FROM environments ORDER BY sort_order, id`)
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
		SortOrder int    `json:"sort_order"`
	}
	out := []e{}
	for rows.Next() {
		var x e
		if rows.Scan(&x.ID, &x.Code, &x.Name, &x.TagType, &x.SortOrder) == nil {
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
		SortOrder int    `json:"sort_order"`
	}
	if err := c.ShouldBindJSON(&in); err != nil || in.Code == "" {
		c.JSON(400, gin.H{"error": "code 必填"})
		return
	}
	if in.TagType == "" {
		in.TagType = "info"
	}
	res, err := h.DB.Exec(`INSERT INTO environments (code, name, tag_type, sort_order) VALUES (?, ?, ?, ?)`, in.Code, in.Name, in.TagType, in.SortOrder)
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
		SortOrder int    `json:"sort_order"`
	}
	if err := c.ShouldBindJSON(&in); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	if _, err := h.DB.Exec(`UPDATE environments SET code=?, name=?, tag_type=?, sort_order=? WHERE id=?`, in.Code, in.Name, in.TagType, in.SortOrder, c.Param("id")); err != nil {
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
