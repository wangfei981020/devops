package handlers

import (
	"database/sql"
	"encoding/json"
	"time"

	"github.com/gin-gonic/gin"
	"opsplatform-gke-version-backend/models"
)

type OverviewHandler struct{ DB *sql.DB }

func NewOverviewHandler(db *sql.DB) *OverviewHandler { return &OverviewHandler{DB: db} }

func (h *OverviewHandler) Register(r *gin.RouterGroup) {
	r.GET("/overview", h.Get)
}

type overviewResp struct {
	TotalClusters         int                  `json:"total_clusters"`
	TotalNodepools        int                  `json:"total_nodepools"`
	ClustersBehind        int                  `json:"clusters_behind"`         // current_to_latest >= 5
	NodepoolsBehind       int                  `json:"nodepools_behind"`
	EOLWithin90d          int                  `json:"eol_within_90d"`
	TopClusters           []clusterTopRow      `json:"top_clusters"`
	TopNodepools          []nodepoolTopRow     `json:"top_nodepools"`
	EOLAlerts             []eolAlertRow        `json:"eol_alerts"`
}

type clusterTopRow struct {
	ID                            int     `json:"id"`
	Name                          string  `json:"name"`
	ProjectID                     string  `json:"project_id"`
	Location                      string  `json:"location"`
	CurrentVersion                string  `json:"current_version"`
	LatestAvailableVersion        string  `json:"latest_available_version"`
	CurrentToLatestVersionsBehind int     `json:"current_to_latest_versions_behind"`
	StdSupportEnd                 string  `json:"std_support_end"`
	DaysToStdEOL                  *int    `json:"days_to_std_eol"`
}

type nodepoolTopRow struct {
	ClusterID                     int     `json:"cluster_id"`
	ClusterName                   string  `json:"cluster_name"`
	NodepoolName                  string  `json:"nodepool_name"`
	CurrentVersion                string  `json:"current_version"`
	LatestAvailableVersion        string  `json:"latest_available_version"`
	CurrentToLatestVersionsBehind int     `json:"current_to_latest_versions_behind"`
}

type eolAlertRow struct {
	ClusterID     int    `json:"cluster_id"`
	ClusterName   string `json:"cluster_name"`
	StdSupportEnd string `json:"std_support_end"`
	DaysRemaining int    `json:"days_remaining"`
}

const behindThresholdForKPI = 5

func (h *OverviewHandler) Get(c *gin.Context) {
	resp := overviewResp{
		TopClusters:  []clusterTopRow{},
		TopNodepools: []nodepoolTopRow{},
		EOLAlerts:    []eolAlertRow{},
	}

	// 集群总数
	_ = h.DB.QueryRow(`SELECT COUNT(*) FROM clusters WHERE enabled=1`).Scan(&resp.TotalClusters)

	// 集群层 KPI 和 Top5
	rows, err := h.DB.Query(`
		SELECT c.id, c.name, c.project_id, c.location,
		       COALESCE(s.current_version, ''),
		       COALESCE(s.latest_available_version, ''),
		       COALESCE(s.current_to_latest_versions_behind, 0),
		       COALESCE(s.std_support_end, ''),
		       s.nodepools_json
		FROM clusters c
		LEFT JOIN cluster_snapshots s ON s.cluster_id = c.id
		WHERE c.enabled=1
		ORDER BY s.current_to_latest_versions_behind DESC`)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	defer rows.Close()

	now := time.Now()
	allNodepools := []nodepoolTopRow{}

	for rows.Next() {
		var (
			row     clusterTopRow
			stdEnd  string
			npJSON  sql.NullString
		)
		if err := rows.Scan(&row.ID, &row.Name, &row.ProjectID, &row.Location,
			&row.CurrentVersion, &row.LatestAvailableVersion, &row.CurrentToLatestVersionsBehind,
			&stdEnd, &npJSON); err != nil {
			continue
		}
		row.StdSupportEnd = stdEnd
		if stdEnd != "" {
			if t, err := time.Parse("2006-01-02", stdEnd); err == nil {
				d := int(t.Sub(now).Hours() / 24)
				row.DaysToStdEOL = &d
				if d <= 90 && d >= 0 {
					resp.EOLWithin90d++
					resp.EOLAlerts = append(resp.EOLAlerts, eolAlertRow{
						ClusterID: row.ID, ClusterName: row.Name,
						StdSupportEnd: stdEnd, DaysRemaining: d,
					})
				}
			}
		}
		if row.CurrentToLatestVersionsBehind >= behindThresholdForKPI {
			resp.ClustersBehind++
		}
		if len(resp.TopClusters) < 5 && row.CurrentToLatestVersionsBehind > 0 {
			resp.TopClusters = append(resp.TopClusters, row)
		}

		// 节点池
		if npJSON.Valid && npJSON.String != "" {
			var nps []models.NodePoolInfo
			_ = json.Unmarshal([]byte(npJSON.String), &nps)
			for _, np := range nps {
				resp.TotalNodepools++
				if np.CurrentToLatestVersionsBehind >= behindThresholdForKPI {
					resp.NodepoolsBehind++
				}
				if np.CurrentToLatestVersionsBehind > 0 {
					allNodepools = append(allNodepools, nodepoolTopRow{
						ClusterID:                     row.ID,
						ClusterName:                   row.Name,
						NodepoolName:                  np.Name,
						CurrentVersion:                np.CurrentVersion,
						LatestAvailableVersion:        np.LatestAvailableVersion,
						CurrentToLatestVersionsBehind: np.CurrentToLatestVersionsBehind,
					})
				}
			}
		}
	}

	// Top5 节点池：按落后程度排序
	for i := range allNodepools {
		for j := i + 1; j < len(allNodepools); j++ {
			if allNodepools[j].CurrentToLatestVersionsBehind > allNodepools[i].CurrentToLatestVersionsBehind {
				allNodepools[i], allNodepools[j] = allNodepools[j], allNodepools[i]
			}
		}
	}
	if len(allNodepools) > 5 {
		allNodepools = allNodepools[:5]
	}
	resp.TopNodepools = allNodepools

	// EOL Alerts 按剩余天数升序
	for i := range resp.EOLAlerts {
		for j := i + 1; j < len(resp.EOLAlerts); j++ {
			if resp.EOLAlerts[j].DaysRemaining < resp.EOLAlerts[i].DaysRemaining {
				resp.EOLAlerts[i], resp.EOLAlerts[j] = resp.EOLAlerts[j], resp.EOLAlerts[i]
			}
		}
	}

	c.JSON(200, resp)
}
