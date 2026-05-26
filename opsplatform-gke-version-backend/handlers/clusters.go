package handlers

import (
	"database/sql"
	"encoding/json"
	"net/http"

	"github.com/gin-gonic/gin"
	"opsplatform-gke-version-backend/models"
)

type ClustersHandler struct {
	DB *sql.DB
}

func NewClustersHandler(db *sql.DB) *ClustersHandler {
	return &ClustersHandler{DB: db}
}

func (h *ClustersHandler) Register(r *gin.RouterGroup) {
	r.GET("/clusters", h.List)
	r.GET("/clusters/:id", h.Get)
	r.POST("/clusters", h.Create)
	r.PUT("/clusters/:id", h.Update)
	r.DELETE("/clusters/:id", h.Delete)
}

type clusterWithSnap struct {
	models.Cluster
	Snapshot *models.ClusterSnapshot `json:"snapshot"`
}

func (h *ClustersHandler) List(c *gin.Context) {
	rows, err := h.DB.Query(`
		SELECT c.id, c.project_id, c.location, c.name, c.enabled, c.created_at, c.updated_at,
		       s.current_version, s.max_upgradable_version, s.latest_available_version,
		       s.current_to_max_versions_behind, s.current_to_max_version_diff,
		       s.max_to_latest_versions_behind, s.max_to_latest_version_diff,
		       s.current_to_latest_versions_behind, s.current_to_latest_version_diff,
		       s.std_support_end, s.ext_support_end, s.nodepools_json,
		       s.last_refreshed_at, s.last_error
		FROM clusters c
		LEFT JOIN cluster_snapshots s ON s.cluster_id = c.id
		ORDER BY c.id`)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	defer rows.Close()
	out := []clusterWithSnap{}
	for rows.Next() {
		var cw clusterWithSnap
		var (
			cur, maxUp, latest, std, ext, lastErr sql.NullString
			cm, ml, cl2                           sql.NullInt64
			cmd, mld, cld                         sql.NullFloat64
			npJSON                                sql.NullString
			lastRefreshed                         sql.NullTime
		)
		if err := rows.Scan(&cw.ID, &cw.ProjectID, &cw.Location, &cw.Name, &cw.Enabled, &cw.CreatedAt, &cw.UpdatedAt,
			&cur, &maxUp, &latest, &cm, &cmd, &ml, &mld, &cl2, &cld,
			&std, &ext, &npJSON, &lastRefreshed, &lastErr); err != nil {
			c.JSON(500, gin.H{"error": err.Error()})
			return
		}
		if cur.Valid {
			snap := &models.ClusterSnapshot{
				ClusterID:                     cw.ID,
				CurrentVersion:                cur.String,
				MaxUpgradableVersion:          maxUp.String,
				LatestAvailableVersion:        latest.String,
				CurrentToMaxVersionsBehind:    int(cm.Int64),
				CurrentToMaxVersionDiff:       cmd.Float64,
				MaxToLatestVersionsBehind:     int(ml.Int64),
				MaxToLatestVersionDiff:        mld.Float64,
				CurrentToLatestVersionsBehind: int(cl2.Int64),
				CurrentToLatestVersionDiff:    cld.Float64,
				StdSupportEnd:                 std.String,
				ExtSupportEnd:                 ext.String,
				LastError:                     lastErr.String,
			}
			if lastRefreshed.Valid {
				snap.LastRefreshedAt = &lastRefreshed.Time
			}
			if npJSON.Valid && npJSON.String != "" {
				_ = json.Unmarshal([]byte(npJSON.String), &snap.NodePools)
			}
			cw.Snapshot = snap
		}
		out = append(out, cw)
	}
	c.JSON(http.StatusOK, out)
}

func (h *ClustersHandler) Get(c *gin.Context) {
	id := c.Param("id")
	row := h.DB.QueryRow(`SELECT id, project_id, location, name, enabled, created_at, updated_at FROM clusters WHERE id=?`, id)
	var cl models.Cluster
	if err := row.Scan(&cl.ID, &cl.ProjectID, &cl.Location, &cl.Name, &cl.Enabled, &cl.CreatedAt, &cl.UpdatedAt); err != nil {
		c.JSON(404, gin.H{"error": "not found"})
		return
	}
	c.JSON(200, cl)
}

type clusterReq struct {
	ProjectID string `json:"project_id" binding:"required"`
	Location  string `json:"location" binding:"required"`
	Name      string `json:"name" binding:"required"`
	Enabled   int    `json:"enabled"`
}

func (h *ClustersHandler) Create(c *gin.Context) {
	var req clusterReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	res, err := h.DB.Exec(`INSERT INTO clusters (project_id, location, name, enabled) VALUES (?, ?, ?, ?)`,
		req.ProjectID, req.Location, req.Name, 1)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	id, _ := res.LastInsertId()
	c.JSON(201, gin.H{"id": id})
}

func (h *ClustersHandler) Update(c *gin.Context) {
	id := c.Param("id")
	var req clusterReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	_, err := h.DB.Exec(`UPDATE clusters SET project_id=?, location=?, name=?, enabled=? WHERE id=?`,
		req.ProjectID, req.Location, req.Name, req.Enabled, id)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"ok": true})
}

func (h *ClustersHandler) Delete(c *gin.Context) {
	id := c.Param("id")
	_, err := h.DB.Exec(`DELETE FROM clusters WHERE id=?`, id)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"ok": true})
}
