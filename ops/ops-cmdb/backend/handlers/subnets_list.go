package handlers

import (
	"database/sql"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"ops-cmdb-backend/internal/httpx"
)

// 网络与子网。以**子网**为行：网段、区域、所属 VPC 都挂在子网上，
// 而 VPC 本身只有一个名字和模式，单独成页没什么可看的。

type SubnetListHandler struct{ DB *sql.DB }

func NewSubnetListHandler(db *sql.DB) *SubnetListHandler { return &SubnetListHandler{DB: db} }

func (h *SubnetListHandler) Register(r *gin.RouterGroup) {
	r.GET("/cloud-subnet-list", h.List)
}

type subnetOut struct {
	ID      int    `json:"id"`
	Name    string `json:"name"`
	Network string `json:"network"`
	// NetworkMode auto / custom。auto 模式的 VPC 会在每个区域自动建子网，
	// 网段是固定的 —— 看到意料之外的子网时，这一列能立刻解释"它哪来的"
	NetworkMode string `json:"network_mode"`
	Project     string `json:"project"`
	Region      string `json:"region"`
	CIDR        string `json:"cidr"`
	Gateway     string `json:"gateway"`
	Provider    string `json:"provider"`

	// Stale 云上已经查不到它了。
	//
	// ⚠️ 不能因为 stale 就把行删掉：别的资源可能还引用着这个网段
	// （安全组规则、对端路由）。留着并显式标注，才查得出"这条规则指向的子网没了"。
	Stale    bool   `json:"stale"`
	SyncedAt string `json:"synced_at,omitempty"`
}

// List GET /api/cloud-subnet-list
//
//	@Summary		子网列表
//	@Description	网段、区域与所属 VPC。云上已删除的保留并标注（别的资源可能还引用着它）。
//	@Tags			cloud
//	@Produce		json
//	@Param			page	query		int		false	"页码，从 1 开始"
//	@Param			size	query		int		false	"每页条数"
//	@Param			q		query		string	false	"按子网名/网段/VPC 搜索"
//	@Param			region	query		string	false	"区域，all 或具体值"
//	@Param			project	query		string	false	"云项目，all 或具体值"
//	@Success		200		{object}	httpx.ListResponse[handlers.subnetOut]
//	@Router			/cloud-subnet-list [get]
func (h *SubnetListHandler) List(c *gin.Context) {
	q := httpx.BindPage(c, "region", "project")

	rows, err := h.DB.Query(`SELECT s.id, s.name, s.network, COALESCE(n.mode,''), s.project,
		s.region, s.cidr, COALESCE(s.gateway,''), s.provider, s.stale, s.synced_at
		FROM cloud_subnets s
		LEFT JOIN cloud_networks n ON n.name = s.network AND n.project = s.project
		ORDER BY s.project, s.region, s.name`)
	if err != nil {
		httpx.Fail(c, httpx.CodeInternal, err, nil)
		return
	}
	defer rows.Close()

	all := []subnetOut{}
	for rows.Next() {
		var o subnetOut
		var stale int
		var synced sql.NullTime
		if err := rows.Scan(&o.ID, &o.Name, &o.Network, &o.NetworkMode, &o.Project,
			&o.Region, &o.CIDR, &o.Gateway, &o.Provider, &stale, &synced); err != nil {
			continue
		}
		o.Stale = stale == 1
		if synced.Valid {
			o.SyncedAt = synced.Time.Format(time.RFC3339)
		}
		all = append(all, o)
	}

	items, total, facets := subnetPage(all, q)
	c.JSON(http.StatusOK, httpx.NewList(items, q, total).WithFacets(facets))
}

func subnetPage(all []subnetOut, q httpx.PageQuery) ([]subnetOut, int64, map[string]map[string]int64) {
	match := func(s subnetOut) bool {
		if q.Keyword == "" {
			return true
		}
		kw := strings.ToLower(q.Keyword)
		return strings.Contains(strings.ToLower(s.Name), kw) ||
			strings.Contains(strings.ToLower(s.CIDR), kw) ||
			strings.Contains(strings.ToLower(s.Network), kw)
	}
	region, project := q.Filters["region"], q.Filters["project"]
	facets := map[string]map[string]int64{"region": {}, "project": {}}
	for _, s := range all {
		if !match(s) {
			continue
		}
		// 分面排除自己那一维
		if project == "" || project == "all" || s.Project == project {
			facets["region"][s.Region]++
			facets["region"]["all"]++
		}
		if region == "" || region == "all" || s.Region == region {
			facets["project"][s.Project]++
			facets["project"]["all"]++
		}
	}

	filtered := make([]subnetOut, 0, len(all))
	for _, s := range all {
		if !match(s) {
			continue
		}
		if region != "" && region != "all" && s.Region != region {
			continue
		}
		if project != "" && project != "all" && s.Project != project {
			continue
		}
		filtered = append(filtered, s)
	}

	sort.SliceStable(filtered, func(i, j int) bool {
		a, b := filtered[i], filtered[j]
		var less bool
		switch q.SortBy {
		case "cidr":
			less = a.CIDR < b.CIDR
		case "region":
			less = a.Region < b.Region
		default:
			// 已消失的排最前：它们是需要有人去确认的历史遗留
			if a.Stale != b.Stale {
				return a.Stale
			}
			less = a.Name < b.Name
		}
		if q.SortDesc {
			return !less
		}
		return less
	})

	total := int64(len(filtered))
	lo := min(q.Offset(), len(filtered))
	hi := min(lo+q.Size, len(filtered))
	return filtered[lo:hi], total, facets
}
