package handlers

import (
	"database/sql"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"opsplatform-gke-version-backend/services"
)

type NodesHandler struct{ DB *sql.DB }

func NewNodesHandler(db *sql.DB) *NodesHandler { return &NodesHandler{DB: db} }

func (h *NodesHandler) Register(r *gin.RouterGroup) {
	r.GET("/clusters/:id/nodes", h.List)
}

// nodeEntry：单个 VM 实例响应
type nodeEntry struct {
	Name         string    `json:"name"`
	Zone         string    `json:"zone"`
	Version      string    `json:"version"`
	GCPCreatedAt time.Time `json:"gcp_created_at"`
	AgeDays      int       `json:"age_days"`
	LastSeenAt   time.Time `json:"last_seen_at"`
}

// nodepoolGroup：节点池聚合
type nodepoolGroup struct {
	Name          string      `json:"name"`
	NodeCount     int         `json:"node_count"`
	OldestAgeDays int         `json:"oldest_age_days"` // 最老 node 的天数，等价于"当前版本至少跑了多少天"
	NewestAgeDays int         `json:"newest_age_days"` // 最新 node 的天数
	Nodes         []nodeEntry `json:"nodes"`
}

type nodesResp struct {
	Nodepools []nodepoolGroup `json:"nodepools"`
}

func (h *NodesHandler) List(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		c.JSON(400, gin.H{"error": "invalid cluster id"})
		return
	}

	nodes, err := services.ListNodesByCluster(h.DB, id)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}

	// 按 nodepool 分组，同时计算 oldest / newest age
	now := time.Now()
	groupMap := map[string]*nodepoolGroup{}
	order := []string{} // 保持稳定输出顺序：第一次出现的 nodepool 名顺序

	for _, n := range nodes {
		age := daysBetween(n.GCPCreatedAt, now)
		entry := nodeEntry{
			Name:         n.NodeName,
			Zone:         n.Zone,
			Version:      n.Version,
			GCPCreatedAt: n.GCPCreatedAt,
			AgeDays:      age,
			LastSeenAt:   n.LastSeenAt,
		}
		g, ok := groupMap[n.NodepoolName]
		if !ok {
			g = &nodepoolGroup{
				Name:          n.NodepoolName,
				OldestAgeDays: age,
				NewestAgeDays: age,
			}
			groupMap[n.NodepoolName] = g
			order = append(order, n.NodepoolName)
		}
		if age > g.OldestAgeDays {
			g.OldestAgeDays = age
		}
		if age < g.NewestAgeDays {
			g.NewestAgeDays = age
		}
		g.Nodes = append(g.Nodes, entry)
		g.NodeCount++
	}

	resp := nodesResp{Nodepools: make([]nodepoolGroup, 0, len(order))}
	for _, name := range order {
		resp.Nodepools = append(resp.Nodepools, *groupMap[name])
	}
	c.JSON(200, resp)
}

// daysBetween：向下取整的天数差，gcp_created_at 在未来或零值时返回 0。
func daysBetween(start, end time.Time) int {
	if start.IsZero() || end.Before(start) {
		return 0
	}
	return int(end.Sub(start).Hours() / 24)
}
