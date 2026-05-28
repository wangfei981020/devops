package handlers

import (
	"database/sql"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"opsplatform-gke-version-backend/services"
)

type UpgradesHandler struct{ DB *sql.DB }

func NewUpgradesHandler(db *sql.DB) *UpgradesHandler { return &UpgradesHandler{DB: db} }

func (h *UpgradesHandler) Register(r *gin.RouterGroup) {
	r.GET("/clusters/:id/upgrades", h.List)
}

// upgradeEntry：单条响应
type upgradeEntry struct {
	OperationID     string     `json:"operation_id"`
	OperationType   string     `json:"operation_type"`
	FromVersion     string     `json:"from_version"`
	ToVersion       string     `json:"to_version"`
	FromSource      string     `json:"from_source"`
	ToSource        string     `json:"to_source"`
	Status          string     `json:"status"`
	StartedAt       *time.Time `json:"started_at,omitempty"`
	EndedAt         *time.Time `json:"ended_at,omitempty"`
	RawDetail       string     `json:"raw_detail,omitempty"`
	DurationSeconds int64      `json:"duration_seconds"` // operation 本身耗时
}

// 单组：master 或某个 nodepool
type upgradeGroup struct {
	Target               string         `json:"target"` // 'master' 或具体 nodepool 名
	Kind                 string         `json:"kind"`   // 'master' / 'nodepool'
	CurrentVersionSince  *time.Time     `json:"current_version_since,omitempty"` // 当前版本开始运行时间
	CurrentVersionDays   int            `json:"current_version_days"`            // 当前版本运行了多少天
	Events               []upgradeEntry `json:"events"`
}

type upgradesResp struct {
	Groups []upgradeGroup `json:"groups"`
}

func (h *UpgradesHandler) List(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		c.JSON(400, gin.H{"error": "invalid cluster id"})
		return
	}
	events, err := services.ListUpgradesByCluster(h.DB, id)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}

	// 按 target（master='' or nodepool name）分组
	now := time.Now()
	groups := map[string]*upgradeGroup{}
	order := []string{}

	for _, e := range events {
		key := e.NodepoolName // '' = master
		g, ok := groups[key]
		if !ok {
			g = &upgradeGroup{Target: targetLabel(key), Kind: targetKind(key)}
			groups[key] = g
			order = append(order, key)
		}
		entry := upgradeEntry{
			OperationID:   e.OperationID,
			OperationType: e.OperationType,
			FromVersion:   e.FromVersion,
			ToVersion:     e.ToVersion,
			FromSource:    e.FromSource,
			ToSource:      e.ToSource,
			Status:        e.Status,
			StartedAt:     e.StartedAt,
			EndedAt:       e.EndedAt,
			RawDetail:     e.RawDetail,
		}
		if e.StartedAt != nil && e.EndedAt != nil {
			entry.DurationSeconds = int64(e.EndedAt.Sub(*e.StartedAt).Seconds())
		}
		g.Events = append(g.Events, entry)
	}

	// 算每组"当前版本运行多少天"= 最近一次 status=DONE 事件的 ended_at 到 now
	for _, g := range groups {
		for _, ev := range g.Events {
			if ev.Status == "DONE" && ev.EndedAt != nil {
				since := *ev.EndedAt
				g.CurrentVersionSince = &since
				g.CurrentVersionDays = int(now.Sub(since).Hours() / 24)
				break // events 已按 ended_at DESC，取第一条
			}
		}
	}

	resp := upgradesResp{Groups: make([]upgradeGroup, 0, len(order))}
	// master 排第一，nodepool 按名字字母序
	if g, ok := groups[""]; ok {
		resp.Groups = append(resp.Groups, *g)
	}
	for _, key := range order {
		if key == "" {
			continue
		}
		resp.Groups = append(resp.Groups, *groups[key])
	}
	c.JSON(200, resp)
}

func targetLabel(key string) string {
	if key == "" {
		return "master"
	}
	return key
}
func targetKind(key string) string {
	if key == "" {
		return "master"
	}
	return "nodepool"
}
