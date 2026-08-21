package handlers

import (
	"database/sql"
	"net/http"
	"ops-cmdb-backend/internal/httpx"
	"strings"

	"github.com/gin-gonic/gin"

	"ops-cmdb-backend/internal/store"

	"ops-cmdb-backend/models"
)

type CIHandler struct {
	// 迁移期同时持有：Store 用于有租户语义的读写，DB 供尚未迁移的内部函数。
	Store *store.Store
	DB    *sql.DB
}

func NewCIHandler(st *store.Store, db *sql.DB) *CIHandler {
	return &CIHandler{Store: st, DB: db}
}

func (h *CIHandler) Register(r *gin.RouterGroup) {
	r.GET("/ci-types", h.ListTypes)
	r.GET("/cis", h.List)
	r.POST("/cis", h.Create)
	r.GET("/cis/:id", h.Get)
	r.PUT("/cis/:id", h.Update)
	r.DELETE("/cis/:id", h.Delete)
	r.PUT("/cis/:id/labels", h.SetLabels)
}

func (h *CIHandler) ListTypes(c *gin.Context) {
	rows, err := h.DB.Query(`SELECT id, code, name, COALESCE(name_en,''), icon, sort_order FROM ci_types ORDER BY sort_order, id`)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	defer rows.Close()
	out := []models.CIType{}
	for rows.Next() {
		var t models.CIType
		if err := rows.Scan(&t.ID, &t.Code, &t.Name, &t.NameEn, &t.Icon, &t.SortOrder); err != nil {
			c.JSON(500, gin.H{"error": err.Error()})
			return
		}
		out = append(out, t)
	}
	c.JSON(http.StatusOK, out)
}

// List 列表：?type=&env=&project=&q=（q 模糊匹配 name）
func (h *CIHandler) List(c *gin.Context) {
	var where []string
	var args []any
	if v := c.Query("type"); v != "" {
		where = append(where, "type=?")
		args = append(args, v)
	}
	if v := c.Query("env"); v != "" {
		where = append(where, "env=?")
		args = append(args, v)
	}
	if v := c.Query("project"); v != "" {
		where = append(where, "project=?")
		args = append(args, v)
	}
	if v := c.Query("q"); v != "" {
		where = append(where, "name LIKE ?")
		args = append(args, "%"+v+"%")
	}
	q := `SELECT id, type, name, project, env, module, owner, status, remark, created_at, updated_at FROM cis`
	if len(where) > 0 {
		q += " WHERE " + strings.Join(where, " AND ")
	}
	q += " ORDER BY id DESC"
	rows, err := h.DB.Query(q, args...)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	defer rows.Close()
	out := []models.CI{}
	for rows.Next() {
		ci, err := scanCI(rows)
		if err != nil {
			c.JSON(500, gin.H{"error": err.Error()})
			return
		}
		out = append(out, ci)
	}
	c.JSON(http.StatusOK, out)
}

func (h *CIHandler) Get(c *gin.Context) {
	id := c.Param("id")
	row := h.DB.QueryRow(`SELECT id, type, name, project, env, module, owner, status, remark, created_at, updated_at FROM cis WHERE id=?`, id)
	ci, err := scanCI(row)
	if err == sql.ErrNoRows {
		httpx.NotFound(c, "ci")
		return
	}
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	ci.Labels = h.loadLabels(ci.ID)
	ci.Relations = h.loadRelations(ci.ID)
	c.JSON(http.StatusOK, ci)
}

type ciInput struct {
	Type    string            `json:"type"`
	Name    string            `json:"name"`
	Project string            `json:"project"`
	Env     string            `json:"env"`
	Module  string            `json:"module"`
	Owner   string            `json:"owner"`
	Status  string            `json:"status"`
	Remark  string            `json:"remark"`
	Labels  map[string]string `json:"labels"`
}

func (h *CIHandler) Create(c *gin.Context) {
	var in ciInput
	if err := c.ShouldBindJSON(&in); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	if in.Status == "" {
		in.Status = "active"
	}
	// 同类型同名的 CI 只能有一条 —— 数据库上没有这个约束，只能在这里挡（见 dupcheck.go）
	if id, dup := ciExists(h.DB, in.Type, in.Name); dup {
		failDuplicate(c, "配置项", in.Name, id)
		return
	}
	res, err := h.DB.Exec(`INSERT INTO cis (type, name, project, env, module, owner, status, remark)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		in.Type, in.Name, in.Project, in.Env, in.Module, in.Owner, in.Status, in.Remark)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	id, _ := res.LastInsertId()
	AuditCreated(c, "cis", id)
	replaceLabelsDB(h.DB, id, in.Labels)
	SetAuditTarget(c, in.Type+"/"+in.Name)
	c.JSON(201, gin.H{"id": id})
}

func (h *CIHandler) Update(c *gin.Context) {
	sc, err := h.Store.Tenant(c.Request.Context())
	if err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	id := c.Param("id")
	var in ciInput
	if err := c.ShouldBindJSON(&in); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	if _, err := sc.Exec(`UPDATE cis SET name=?, project=?, env=?, module=?, owner=?, status=?, remark=? WHERE tenant_id = ? AND id=?`,
		in.Name, in.Project, in.Env, in.Module, in.Owner, in.Status, in.Remark, id); err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	if in.Labels != nil {
		if iid, err := parseID(id); err == nil {
			replaceLabelsDB(h.DB, iid, in.Labels)
		}
	}
	SetAuditTarget(c, in.Name)
	c.JSON(200, gin.H{"ok": true})
}

// Delete 级联删标签 + 关系 + 专属表行 + CI 本体
func (h *CIHandler) Delete(c *gin.Context) {
	sc, err := h.Store.Tenant(c.Request.Context())
	if err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	id := c.Param("id")
	tx, err := sc.Begin()
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	defer tx.Rollback() //nolint:errcheck // Commit 成功后是 no-op
	// 先删父行确认归属，再动子表 —— 顺序反了的话，别的租户的 id
	// 会先把人家的标签、关系、专属表行删光，最后才发现父行删不动。
	res, err := tx.Exec(`DELETE FROM cis WHERE tenant_id = ? AND id=?`, id)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		httpx.NotFound(c, "ci")
		return
	}
	for _, stmt := range []string{
		`DELETE FROM ci_labels WHERE tenant_id = ? AND ci_id=?`,
		`DELETE FROM ci_relations WHERE tenant_id = ? AND (src_ci_id=? OR dst_ci_id=?)`,
		`DELETE FROM domains WHERE tenant_id = ? AND ci_id=?`,
		`DELETE FROM certificates WHERE tenant_id = ? AND ci_id=?`,
	} {
		var e error
		if strings.Contains(stmt, "OR dst_ci_id") {
			_, e = tx.Exec(stmt, id, id)
		} else {
			_, e = tx.Exec(stmt, id)
		}
		if e != nil {
			c.JSON(500, gin.H{"error": e.Error()})
			return
		}
	}
	if err := tx.Commit(); err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	SetAuditTarget(c, id)
	c.JSON(200, gin.H{"ok": true})
}

// SetLabels 全量替换某 CI 的标签
func (h *CIHandler) SetLabels(c *gin.Context) {
	iid, err := parseID(c.Param("id"))
	if err != nil {
		httpx.Invalid(c, "id", "number")
		return
	}
	var in struct {
		Labels map[string]string `json:"labels"`
	}
	if err := c.ShouldBindJSON(&in); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	replaceLabelsDB(h.DB, iid, in.Labels)
	c.JSON(200, gin.H{"ok": true})
}

// ---- helpers ----

func (h *CIHandler) loadLabels(ciID int64) map[string]string {
	m := map[string]string{}
	rows, err := h.DB.Query(`SELECT k, v FROM ci_labels WHERE ci_id=?`, ciID)
	if err != nil {
		return m
	}
	defer rows.Close()
	for rows.Next() {
		var k, v string
		if rows.Scan(&k, &v) == nil {
			m[k] = v
		}
	}
	return m
}

func (h *CIHandler) loadRelations(ciID int64) []models.Relation {
	out := []models.Relation{}
	rows, err := h.DB.Query(`
		SELECT r.id, r.src_ci_id, r.dst_ci_id, r.rel_type,
		       CASE WHEN r.src_ci_id=? THEN r.dst_ci_id ELSE r.src_ci_id END AS peer_id,
		       c.name, c.type,
		       CASE WHEN r.src_ci_id=? THEN 'out' ELSE 'in' END AS direction
		FROM ci_relations r
		JOIN cis c ON c.id = CASE WHEN r.src_ci_id=? THEN r.dst_ci_id ELSE r.src_ci_id END
		WHERE r.src_ci_id=? OR r.dst_ci_id=?`, ciID, ciID, ciID, ciID, ciID)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var r models.Relation
		if rows.Scan(&r.ID, &r.SrcCIID, &r.DstCIID, &r.RelType, &r.PeerID, &r.PeerName, &r.PeerType, &r.Direction) == nil {
			out = append(out, r)
		}
	}
	return out
}
