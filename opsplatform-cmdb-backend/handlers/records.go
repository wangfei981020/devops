package handlers

import (
	"database/sql"
	"net/http"

	"github.com/gin-gonic/gin"
)

// RecordHandler 解析层（domain_records）：一个域名下挂多条解析。
type RecordHandler struct {
	DB *sql.DB
}

func NewRecordHandler(db *sql.DB) *RecordHandler { return &RecordHandler{DB: db} }

func (h *RecordHandler) Register(r *gin.RouterGroup) {
	r.GET("/domains/:ciid/records", h.List)
	r.POST("/domains/:ciid/records", h.Create)
	r.PUT("/records/:id", h.Update)
	r.DELETE("/records/:id", h.Delete)
}

type recordOut struct {
	ID           int64  `json:"id"`
	Host         string `json:"host"`
	RecordType   string `json:"record_type"`
	CdnID        *int   `json:"cdn_id"`
	CdnName      string `json:"cdn_name"`
	Cname        string `json:"cname"`
	OriginIP     string `json:"origin_ip"`
	CertExpiryAt string `json:"cert_expiry_at"`
	CertCheckMsg string `json:"cert_check_msg"`
	Project      string `json:"project"`
	Env          string `json:"env"`
	Module       string `json:"module"`
	Operator     string `json:"operator"`
	UpdatedAt    string `json:"updated_at"`
}

func (h *RecordHandler) List(c *gin.Context) {
	rows, err := h.DB.Query(`
		SELECT r.id, r.host, r.record_type, r.cdn_id, COALESCE(d.name,''),
		       r.cname, r.origin_ip, r.cert_expiry_at, r.cert_check_msg,
		       r.project, r.env, r.module, r.operator, r.updated_at
		FROM domain_records r LEFT JOIN cdns d ON d.id=r.cdn_id
		WHERE r.domain_ci_id=? ORDER BY r.id`, c.Param("ciid"))
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	defer rows.Close()
	out := []recordOut{}
	for rows.Next() {
		var o recordOut
		var cdnID sql.NullInt64
		var certExp sql.NullTime
		var updated sql.NullTime
		if err := rows.Scan(&o.ID, &o.Host, &o.RecordType, &cdnID, &o.CdnName,
			&o.Cname, &o.OriginIP, &certExp, &o.CertCheckMsg,
			&o.Project, &o.Env, &o.Module, &o.Operator, &updated); err != nil {
			c.JSON(500, gin.H{"error": err.Error()})
			return
		}
		if cdnID.Valid {
			v := int(cdnID.Int64)
			o.CdnID = &v
		}
		if certExp.Valid {
			o.CertExpiryAt = certExp.Time.Format("2006-01-02")
		}
		if updated.Valid {
			o.UpdatedAt = updated.Time.Format("2006-01-02 15:04")
		}
		out = append(out, o)
	}
	c.JSON(http.StatusOK, out)
}

type recordIn struct {
	Host       string `json:"host"`
	RecordType string `json:"record_type"`
	CdnID      *int   `json:"cdn_id"`
	Cname      string `json:"cname"`
	OriginIP   string `json:"origin_ip"`
	Project    string `json:"project"`
	Env        string `json:"env"`
	Module     string `json:"module"`
}

func (h *RecordHandler) Create(c *gin.Context) {
	var in recordIn
	if err := c.ShouldBindJSON(&in); err != nil || in.Host == "" {
		c.JSON(400, gin.H{"error": "host 必填"})
		return
	}
	if in.RecordType == "" {
		in.RecordType = "A"
	}
	res, err := h.DB.Exec(`INSERT INTO domain_records
		(domain_ci_id, host, record_type, cdn_id, cname, origin_ip, project, env, module, operator)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		c.Param("ciid"), in.Host, in.RecordType, nullableInt(in.CdnID), in.Cname, in.OriginIP,
		in.Project, in.Env, in.Module, currentUser(c))
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	id, _ := res.LastInsertId()
	WriteAudit(h.DB, c, "create_record", in.Host)
	c.JSON(201, gin.H{"id": id})
}

func (h *RecordHandler) Update(c *gin.Context) {
	var in recordIn
	if err := c.ShouldBindJSON(&in); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	if _, err := h.DB.Exec(`UPDATE domain_records SET host=?, record_type=?, cdn_id=?, cname=?, origin_ip=?,
		project=?, env=?, module=?, operator=? WHERE id=?`,
		in.Host, in.RecordType, nullableInt(in.CdnID), in.Cname, in.OriginIP,
		in.Project, in.Env, in.Module, currentUser(c), c.Param("id")); err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	WriteAudit(h.DB, c, "update_record", in.Host)
	c.JSON(200, gin.H{"ok": true})
}

func (h *RecordHandler) Delete(c *gin.Context) {
	if _, err := h.DB.Exec(`DELETE FROM domain_records WHERE id=?`, c.Param("id")); err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	WriteAudit(h.DB, c, "delete_record", c.Param("id"))
	c.JSON(200, gin.H{"ok": true})
}
