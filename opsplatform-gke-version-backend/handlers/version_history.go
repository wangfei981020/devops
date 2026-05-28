package handlers

import (
	"database/sql"
	"time"

	"github.com/gin-gonic/gin"
)

type VersionHistoryHandler struct{ DB *sql.DB }

func NewVersionHistoryHandler(db *sql.DB) *VersionHistoryHandler {
	return &VersionHistoryHandler{DB: db}
}

func (h *VersionHistoryHandler) Register(r *gin.RouterGroup) {
	r.GET("/clusters/:id/version_history", h.Get)
}

type versionEntry struct {
	Version      string     `json:"version"`
	StartedAt    time.Time  `json:"started_at"`
	EndedAt      *time.Time `json:"ended_at,omitempty"`
	DurationDays int        `json:"duration_days"`
	Current      bool       `json:"current"`
}

type historyResp struct {
	Cluster   []versionEntry            `json:"cluster"`
	Nodepools map[string][]versionEntry `json:"nodepools"`
}

func (h *VersionHistoryHandler) Get(c *gin.Context) {
	clusterID := c.Param("id")
	rows, err := h.DB.Query(`SELECT COALESCE(nodepool_name, ''), version, started_at, ended_at
		FROM version_history WHERE cluster_id=? ORDER BY nodepool_name, started_at DESC`, clusterID)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	defer rows.Close()

	resp := historyResp{
		Cluster:   []versionEntry{},
		Nodepools: map[string][]versionEntry{},
	}
	now := time.Now()

	for rows.Next() {
		var (
			nodepool string
			version  string
			started  time.Time
			ended    sql.NullTime
		)
		if err := rows.Scan(&nodepool, &version, &started, &ended); err != nil {
			c.JSON(500, gin.H{"error": err.Error()})
			return
		}
		entry := versionEntry{Version: version, StartedAt: started}
		var endRef time.Time
		if ended.Valid {
			entry.EndedAt = &ended.Time
			entry.Current = false
			endRef = ended.Time
		} else {
			entry.Current = true
			endRef = now
		}
		entry.DurationDays = int(endRef.Sub(started).Hours() / 24)
		if entry.DurationDays < 0 {
			entry.DurationDays = 0
		}

		if nodepool == "" {
			resp.Cluster = append(resp.Cluster, entry)
		} else {
			resp.Nodepools[nodepool] = append(resp.Nodepools[nodepool], entry)
		}
	}

	c.JSON(200, resp)
}
