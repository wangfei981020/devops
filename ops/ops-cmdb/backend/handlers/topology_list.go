package handlers

import (
	"database/sql"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"ops-cmdb-backend/internal/httpx"
)

// 拓扑：资源之间的关系，以及"动这一个会影响到谁"。
//
// ⚠️ 这里只回答**我们确实记录了关系**的部分。
// 一个没有任何关系记录的资源，不代表它是孤立的 —— 更可能是关系还没采集/建立。
// 界面上必须说清楚这一点，否则"影响面：无"会被当成"随便动"，
// 而那正是变更把线上打挂的经典路径。

type TopologyHandler struct{ DB *sql.DB }

func NewTopologyHandler(db *sql.DB) *TopologyHandler { return &TopologyHandler{DB: db} }

func (h *TopologyHandler) Register(r *gin.RouterGroup) {
	r.GET("/relation-list", h.List)
	r.GET("/impact", h.Impact)
}

type relationOut struct {
	ID      int    `json:"id"`
	SrcID   int    `json:"src_ci_id"`
	SrcName string `json:"src_name"`
	SrcType string `json:"src_type"`
	DstID   int    `json:"dst_ci_id"`
	DstName string `json:"dst_name"`
	DstType string `json:"dst_type"`
	RelType string `json:"rel_type"`
	// Origin 关系是怎么来的：sync（采集推断）/ manual（人工登记）。
	// 采集推断的关系会随下一轮同步消失，人工登记的不会 —— 处置方式不同
	Origin string `json:"origin"`
}

// List GET /api/relation-list
//
//	@Summary		资源关系
//	@Description	已记录的资源间关系。没有关系记录 ≠ 该资源孤立。
//	@Tags			topology
//	@Produce		json
//	@Param			q		query	string	false	"按资源名搜索"
//	@Param			rel_type	query	string	false	"关系类型，all 或具体值"
//	@Success		200		{object}	httpx.ListResponse[handlers.relationOut]
//	@Router			/relation-list [get]
func (h *TopologyHandler) List(c *gin.Context) {
	q := httpx.BindPage(c, "rel_type")

	rows, err := h.DB.Query(`SELECT r.id, r.src_ci_id, COALESCE(s.name,''), COALESCE(s.type,''),
		r.dst_ci_id, COALESCE(d.name,''), COALESCE(d.type,''), r.rel_type, COALESCE(r.origin,'')
		FROM ci_relations r
		LEFT JOIN cis s ON s.id = r.src_ci_id
		LEFT JOIN cis d ON d.id = r.dst_ci_id
		ORDER BY r.id DESC`)
	if err != nil {
		httpx.Fail(c, httpx.CodeInternal, err, nil)
		return
	}
	defer rows.Close()

	all := []relationOut{}
	for rows.Next() {
		var o relationOut
		if rows.Scan(&o.ID, &o.SrcID, &o.SrcName, &o.SrcType, &o.DstID, &o.DstName,
			&o.DstType, &o.RelType, &o.Origin) == nil {
			all = append(all, o)
		}
	}

	relType := q.Filters["rel_type"]
	facets := map[string]map[string]int64{"rel_type": {}}
	for _, r := range all {
		facets["rel_type"][r.RelType]++
		facets["rel_type"]["all"]++
	}
	filtered := make([]relationOut, 0, len(all))
	for _, r := range all {
		if q.Keyword != "" &&
			!strings.Contains(strings.ToLower(r.SrcName+" "+r.DstName), strings.ToLower(q.Keyword)) {
			continue
		}
		if relType != "" && relType != "all" && r.RelType != relType {
			continue
		}
		filtered = append(filtered, r)
	}
	sort.SliceStable(filtered, func(i, j int) bool { return filtered[i].SrcName < filtered[j].SrcName })

	total := int64(len(filtered))
	lo := min(q.Offset(), len(filtered))
	hi := min(lo+q.Size, len(filtered))
	c.JSON(http.StatusOK, httpx.NewList(filtered[lo:hi], q, total).WithFacets(facets))
}

type impactNode struct {
	CIID  int    `json:"ci_id"`
	Name  string `json:"name"`
	Type  string `json:"type"`
	Depth int    `json:"depth"`
	Via   string `json:"via"`
}

type impactOut struct {
	Root     string       `json:"root"`
	Affected []impactNode `json:"affected"`
	// Truncated 是否因深度上限而截断。
	// ⚠️ 截断了必须说 —— 一份"影响面"清单如果悄悄少了一半，
	// 比没有这份清单更危险
	Truncated bool `json:"truncated"`
	// HasRelations 这个资源**有没有任何关系记录**。
	// false 时界面要说"我们没有它的关系数据"，而不是"影响面：无"
	HasRelations bool `json:"has_relations"`
}

// impactMaxDepth 往外找几层。
//
// 取 3：再深下去几乎必然覆盖半个环境，那样的"影响面"没有指导意义。
const impactMaxDepth = 3

// Impact GET /api/impact?ci_id=
//
//	@Summary		变更影响面
//	@Description	沿已记录的关系往外找 3 层。**没有关系记录 ≠ 没有影响**。
//	@Tags			topology
//	@Produce		json
//	@Param			ci_id	query		int	true	"起点资源 ID"
//	@Success		200		{object}	handlers.impactOut
//	@Router			/impact [get]
func (h *TopologyHandler) Impact(c *gin.Context) {
	rootID, err := strconv.Atoi(c.Query("ci_id"))
	if err != nil || rootID <= 0 {
		httpx.Fail(c, httpx.CodeBadRequest, err, nil)
		return
	}
	out := impactOut{Affected: []impactNode{}}
	_ = h.DB.QueryRow(`SELECT name FROM cis WHERE id=?`, rootID).Scan(&out.Root)

	seen := map[int]bool{rootID: true}
	frontier := []int{rootID}
	for depth := 1; depth <= impactMaxDepth && len(frontier) > 0; depth++ {
		next := []int{}
		for _, id := range frontier {
			// 双向都算：改一个数据库，连它的应用受影响；
			// 停一台机器，跑在上面的东西也受影响
			rows, err := h.DB.Query(`SELECT r.dst_ci_id, c.name, c.type, r.rel_type FROM ci_relations r
				JOIN cis c ON c.id = r.dst_ci_id WHERE r.src_ci_id = ?
				UNION
				SELECT r.src_ci_id, c.name, c.type, r.rel_type FROM ci_relations r
				JOIN cis c ON c.id = r.src_ci_id WHERE r.dst_ci_id = ?`, id, id)
			if err != nil {
				continue
			}
			for rows.Next() {
				var n impactNode
				if rows.Scan(&n.CIID, &n.Name, &n.Type, &n.Via) != nil || seen[n.CIID] {
					continue
				}
				seen[n.CIID] = true
				n.Depth = depth
				out.Affected = append(out.Affected, n)
				next = append(next, n.CIID)
			}
			rows.Close()
		}
		frontier = next
		if depth == impactMaxDepth && len(frontier) > 0 {
			out.Truncated = true
		}
	}

	var rel int
	_ = h.DB.QueryRow(`SELECT COUNT(*) FROM ci_relations WHERE src_ci_id=? OR dst_ci_id=?`,
		rootID, rootID).Scan(&rel)
	out.HasRelations = rel > 0

	c.JSON(http.StatusOK, out)
}
