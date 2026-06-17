package handlers

import (
	"database/sql"
	"net/http"

	"github.com/gin-gonic/gin"

	"opsplatform-cmdb-backend/models"
)

type RelationHandler struct {
	DB *sql.DB
}

func NewRelationHandler(db *sql.DB) *RelationHandler { return &RelationHandler{DB: db} }

func (h *RelationHandler) Register(r *gin.RouterGroup) {
	r.GET("/relations", h.List)
	r.POST("/relations", h.Create)
	r.DELETE("/relations/:id", h.Delete)
}

// List 全量关系（用于关系图谱）或某 CI 的关系（?ci_id=）
func (h *RelationHandler) List(c *gin.Context) {
	ciID := c.Query("ci_id")
	var rows *sql.Rows
	var err error
	base := `SELECT r.id, r.src_ci_id, r.dst_ci_id, r.rel_type,
	         s.name, s.type, d.name, d.type
	         FROM ci_relations r
	         JOIN cis s ON s.id=r.src_ci_id
	         JOIN cis d ON d.id=r.dst_ci_id`
	if ciID != "" {
		rows, err = h.DB.Query(base+` WHERE r.src_ci_id=? OR r.dst_ci_id=?`, ciID, ciID)
	} else {
		rows, err = h.DB.Query(base)
	}
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	defer rows.Close()
	type edge struct {
		ID       int64  `json:"id"`
		SrcCIID  int64  `json:"src_ci_id"`
		DstCIID  int64  `json:"dst_ci_id"`
		RelType  string `json:"rel_type"`
		SrcName  string `json:"src_name"`
		SrcType  string `json:"src_type"`
		DstName  string `json:"dst_name"`
		DstType  string `json:"dst_type"`
	}
	out := []edge{}
	for rows.Next() {
		var e edge
		if rows.Scan(&e.ID, &e.SrcCIID, &e.DstCIID, &e.RelType, &e.SrcName, &e.SrcType, &e.DstName, &e.DstType) == nil {
			out = append(out, e)
		}
	}
	c.JSON(http.StatusOK, out)
}

func (h *RelationHandler) Create(c *gin.Context) {
	var in models.Relation
	if err := c.ShouldBindJSON(&in); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	if in.RelType == "" {
		in.RelType = "related"
	}
	res, err := h.DB.Exec(`INSERT IGNORE INTO ci_relations (src_ci_id, dst_ci_id, rel_type) VALUES (?, ?, ?)`,
		in.SrcCIID, in.DstCIID, in.RelType)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	id, _ := res.LastInsertId()
	c.JSON(201, gin.H{"id": id})
}

func (h *RelationHandler) Delete(c *gin.Context) {
	if _, err := h.DB.Exec(`DELETE FROM ci_relations WHERE id=?`, c.Param("id")); err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"ok": true})
}
