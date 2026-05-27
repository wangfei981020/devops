package handlers

import (
	"context"
	"database/sql"

	"github.com/gin-gonic/gin"
	"opsplatform-gke-version-backend/models"
	"opsplatform-gke-version-backend/services"
)

type RefreshHandler struct {
	DB      *sql.DB
	Scraper *services.Scraper
}

func NewRefreshHandler(db *sql.DB, s *services.Scraper) *RefreshHandler {
	return &RefreshHandler{DB: db, Scraper: s}
}

func (h *RefreshHandler) Register(r *gin.RouterGroup) {
	r.POST("/refresh", h.Refresh)
}

type refreshReq struct {
	All        bool  `json:"all"`
	ClusterIDs []int `json:"cluster_ids"`
}

func (h *RefreshHandler) Refresh(c *gin.Context) {
	var req refreshReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	if req.All {
		go h.Scraper.ScrapeAll(context.Background())
		c.JSON(202, gin.H{"started": "all"})
		return
	}
	if len(req.ClusterIDs) == 0 {
		c.JSON(400, gin.H{"error": "cluster_ids required when all=false"})
		return
	}
	go func() {
		for _, id := range req.ClusterIDs {
			cl, err := loadCluster(h.DB, id)
			if err != nil {
				continue
			}
			_ = h.Scraper.ScrapeOne(context.Background(), cl)
		}
	}()
	c.JSON(202, gin.H{"started": req.ClusterIDs})
}

func loadCluster(db *sql.DB, id int) (*models.Cluster, error) {
	row := db.QueryRow(`SELECT id, project_id, location, name, COALESCE(sa_key_json, ''), enabled, created_at, updated_at FROM clusters WHERE id=?`, id)
	cl := &models.Cluster{}
	err := row.Scan(&cl.ID, &cl.ProjectID, &cl.Location, &cl.Name, &cl.SAKeyJSON, &cl.Enabled, &cl.CreatedAt, &cl.UpdatedAt)
	if err == nil {
		cl.HasSAKey = cl.SAKeyJSON != ""
	}
	return cl, err
}
