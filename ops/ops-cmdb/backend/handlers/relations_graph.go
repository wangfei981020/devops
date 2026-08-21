package handlers

import (
	"fmt"
	"net/http"
	"ops-cmdb-backend/internal/httpx"
	"sort"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
)

// 全链路拓扑的子图接口。
//
// # 为什么不复用 /relations 让前端自己画
//
// 生产上 657 条边里 **617 条是同一个事实的重复表达**：GKE 的 LoadBalancer
// 天然全挂在同一批节点上，15 个 LB 各挂同样的 35 台机器 = 525 条线。
// 全量力导向渲染出来是个毛线团，真正有价值的链路（域名→LB→主机，35 条）
// 全被淹没。
//
// 解法是**降低信息冗余**而不是换布局引擎或缩小字号 ——
// 后端集合完全相同的多个 LB，其共同后端折成一个池节点：525 条 → 15 条。
//
// 折叠必须在后端做：前端对 657 条边做 O(n²) 分组会卡死，
// 而且每个客户端各算一遍是纯浪费。
//
// 见 docs/plans/cmdb-relations-graph-redesign.md

// graphNode 图上的一个点。池节点用 Members 承载被折叠的成员。
type graphNode struct {
	ID   string `json:"id"` // 单体= "ci:123"；池= "pool:<签名哈希>"
	CIID int64  `json:"ci_id,omitempty"`
	Name string `json:"name"`
	Type string `json:"type"` // host / domain / certificate / loadbalancer …
	// Layer 分层列号。依赖关系天然有方向（证书→域名→入口→主机），
	// 力导向会把它揉成团、抹掉方向感，所以固定分层。
	Layer int `json:"layer"`
	// Pool 为 true 时是折叠出来的池节点
	Pool    bool         `json:"pool,omitempty"`
	Count   int          `json:"count,omitempty"`
	Members []poolMember `json:"members,omitempty"`
}

type poolMember struct {
	CIID int64  `json:"ci_id"`
	Name string `json:"name"`
	Type string `json:"type"`
}

type graphEdge struct {
	Src  string `json:"src"`
	Dst  string `json:"dst"`
	Type string `json:"rel_type"`
	// Count 这条边代表了多少条原始边（折叠后 >1）
	Count int `json:"count,omitempty"`
}

type graphResp struct {
	Nodes []graphNode `json:"nodes"`
	Edges []graphEdge `json:"edges"`
	// Stats 让人知道折叠掉了多少 —— 隐去规模会让人以为图就这么大
	Stats struct {
		RawEdges    int `json:"raw_edges"`
		ShownEdges  int `json:"shown_edges"`
		PooledNodes int `json:"pooled_nodes"`
	} `json:"stats"`
	// Note 没有关系时的说明。空图必须解释原因，
	// 否则用户以为功能坏了 —— 实际可能只是没跑过建边任务。
	Note string `json:"note,omitempty"`
}

// layerOf 决定节点画在第几列。数字越小越靠左（越"上游"）。
//
//	证书 → 域名 → 入口(LB) → 主机
//
// 未知类型放最后一列而不是丢弃：丢弃会让链路断掉，
// 而用户看到断链会以为数据错了。
func layerOf(t string) int {
	switch t {
	case "certificate":
		return 0
	case "domain":
		return 1
	case ciTypeLB:
		return 2
	case "host":
		return 3
	default:
		return 4
	}
}

func (h *RelationHandler) RegisterGraph(r *gin.RouterGroup) {
	r.GET("/relations/graph", h.Graph)
	r.GET("/relations/entries", h.Entries)
}

// Entries 左侧起点列表：按类型分组的 CI，带关系条数。
//
// 只列**有关系**的对象。没有边的 CI 放进来只会让人点开看到空图。
func (h *RelationHandler) Entries(c *gin.Context) {
	sc, err := h.Store.Tenant(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	q := strings.TrimSpace(c.Query("q"))
	// 支持 `host:xxx` / `lb:xxx` 前缀限定类型
	typeFilter := ""
	if i := strings.Index(q, ":"); i > 0 {
		switch strings.ToLower(q[:i]) {
		case "host":
			typeFilter, q = "host", strings.TrimSpace(q[i+1:])
		case "lb":
			typeFilter, q = ciTypeLB, strings.TrimSpace(q[i+1:])
		case "domain":
			typeFilter, q = "domain", strings.TrimSpace(q[i+1:])
		case "cert":
			typeFilter, q = "certificate", strings.TrimSpace(q[i+1:])
		// K8s 侧的两类（OPSCMDB-031 P1-27）。前缀名用短的：
		// 起点框是给人手打的，`svc:` 比 `k8s_service:` 现实得多
		case "svc", "service":
			typeFilter, q = ciTypeK8sService, strings.TrimSpace(q[i+1:])
		case "ing", "ingress":
			typeFilter, q = ciTypeK8sIngress, strings.TrimSpace(q[i+1:])
		}
	}

	sql := `SELECT c.id, c.name, c.type, c.project,
			(SELECT COUNT(*) FROM ci_relations r
			  WHERE r.tenant_id = c.tenant_id AND (r.src_ci_id=c.id OR r.dst_ci_id=c.id)) AS deg
		FROM cis c WHERE c.tenant_id = ?`
	args := []any{}
	if typeFilter != "" {
		sql += " AND c.type=?"
		args = append(args, typeFilter)
	}
	if q != "" {
		sql += " AND c.name LIKE ?"
		args = append(args, "%"+q+"%")
	}
	sql += " HAVING deg > 0 ORDER BY c.type, deg DESC, c.name LIMIT 500"

	rows, err := sc.Query(sql, args...)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	defer rows.Close()

	type entry struct {
		CIID    int64  `json:"ci_id"`
		Name    string `json:"name"`
		Type    string `json:"type"`
		Project string `json:"project"`
		Degree  int    `json:"degree"`
	}
	out := []entry{}
	for rows.Next() {
		var e entry
		if rows.Scan(&e.CIID, &e.Name, &e.Type, &e.Project, &e.Degree) == nil {
			out = append(out, e)
		}
	}

	// 🔴 一条都没搜到时，再拿这个名字去**主表**查一次（不要求有关系边）。
	//
	//	"没有这个资源" 和 "资源在，但还没建关系边" 是两种相反的结论，
	//	而界面此前把它们合成同一句 `No matching resource`：
	//	用户搜 `g32-prod-db-manager`（主机页第一行就是它）得到"查无此物"，
	//	自然会以为 CMDB 根本没采到这台机器（OPSCMDB-077）。
	//
	//	这一类**是多数**：实测主机 68/127 有边（46% 没有）、域名 13/62（79% 没有）。
	//
	// ⚠️ 回查结果的 degree 必然是 0，调用方据此渲染"存在但无法分析影响"，
	//	不能把它们当成可用起点 —— 关系边少是产品有意保守的选择
	//	（没采 Service selector 就不按同名猜，见 relations_auto_link），
	//	要修的是"如实告诉用户"，不是放宽建边规则。
	if len(out) == 0 && q != "" {
		sql2 := `SELECT c.id, c.name, c.type, c.project FROM cis c
			WHERE c.tenant_id = ? AND c.name LIKE ?`
		args2 := []any{"%" + q + "%"}
		if typeFilter != "" {
			sql2 += " AND c.type=?"
			args2 = append(args2, typeFilter)
		}
		sql2 += " ORDER BY c.type, c.name LIMIT 20"
		if r2, err2 := sc.Query(sql2, args2...); err2 == nil {
			defer r2.Close()
			for r2.Next() {
				var e entry
				if r2.Scan(&e.CIID, &e.Name, &e.Type, &e.Project) == nil {
					e.Degree = 0
					out = append(out, e)
				}
			}
		}
		// ⚠️ 回查失败不当成"没有" —— 静默吞掉会把"查询出错"渲染成"资源不存在"，
		//	那正是这条问题本身的形态。出错就让主结果保持空，由前端按加载失败处理。
	}
	c.JSON(http.StatusOK, out)
}

// Graph 以某个 CI 为中心的子图。
//
//	node   起点 ci_id（必填）—— 不做"全景图"：142 个节点即使折叠后仍不可读，
//	       而且它诱导人去"看全貌"，那个需求本身不成立。要看规模用统计数字。
//	dir    forward=依赖（这个域名背后是谁） / reverse=影响面（这台机器挂了影响谁）
//	hops   跳数，默认 2
func (h *RelationHandler) Graph(c *gin.Context) {
	sc, err := h.Store.Tenant(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	center, err := strconv.ParseInt(c.Query("node"), 10, 64)
	if err != nil || center <= 0 {
		httpx.Required(c, "node")
		return
	}
	dir := c.DefaultQuery("dir", "forward")
	if dir != "forward" && dir != "reverse" {
		httpx.Invalid(c, "dir", "forward|reverse")
		return
	}
	hops, _ := strconv.Atoi(c.DefaultQuery("hops", "2"))
	if hops < 1 || hops > 4 {
		hops = 2
	}

	// 起点必须存在且属于本租户。不存在时明确 404 ——
	// 返回空图会让人以为"这个对象没有关系"。
	var centerName, centerType string
	if sc.QueryRow(`SELECT name, type FROM cis WHERE tenant_id = ? AND id=?`, center).
		Scan(&centerName, &centerType) != nil {
		httpx.NotFound(c, "object")
		return
	}

	// ── BFS 展开 ──
	type rawEdge struct {
		src, dst         int64
		relType          string
		srcName, srcType string
		dstName, dstType string
	}
	seen := map[int64]bool{center: true}
	frontier := []int64{center}
	var raws []rawEdge
	edgeSeen := map[string]bool{}

	for hop := 0; hop < hops && len(frontier) > 0; hop++ {
		ph := make([]string, len(frontier))
		args := make([]any, 0, len(frontier))
		for i, id := range frontier {
			ph[i] = "?"
			args = append(args, id)
		}
		in := strings.Join(ph, ",")
		// 方向决定从哪一端展开：
		//   forward（依赖）沿 src→dst 走：域名 → LB → 主机
		//   reverse（影响面）沿 dst→src 走：主机 → LB → 域名
		var where string
		if dir == "forward" {
			where = "r.src_ci_id IN (" + in + ")"
		} else {
			where = "r.dst_ci_id IN (" + in + ")"
		}
		rows, err := sc.Query(`SELECT r.src_ci_id, r.dst_ci_id, r.rel_type,
				s.name, s.type, d.name, d.type
			FROM ci_relations r
			JOIN cis s ON s.id=r.src_ci_id
			JOIN cis d ON d.id=r.dst_ci_id
			WHERE r.tenant_id = ? AND `+where, args...)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		var next []int64
		for rows.Next() {
			var e rawEdge
			if rows.Scan(&e.src, &e.dst, &e.relType, &e.srcName, &e.srcType, &e.dstName, &e.dstType) != nil {
				continue
			}
			k := fmt.Sprintf("%d-%d-%s", e.src, e.dst, e.relType)
			if edgeSeen[k] {
				continue
			}
			edgeSeen[k] = true
			raws = append(raws, e)
			other := e.dst
			if dir == "reverse" {
				other = e.src
			}
			if !seen[other] {
				seen[other] = true
				next = append(next, other)
			}
		}
		rows.Close()
		frontier = next
	}

	resp := graphResp{Nodes: []graphNode{}, Edges: []graphEdge{}}
	resp.Stats.RawEdges = len(raws)
	if len(raws) == 0 {
		// 空图必须解释原因。孤零零画一个点会让人以为功能坏了，
		// 实际多半只是没跑过自动建边任务。
		resp.Note = "该对象暂无已建立的关系。可到「自动化 · 采集任务」执行 relations_auto_link 重建关系。"
		resp.Nodes = append(resp.Nodes, graphNode{
			ID: fmt.Sprintf("ci:%d", center), CIID: center,
			Name: centerName, Type: centerType, Layer: layerOf(centerType),
		})
		c.JSON(http.StatusOK, resp)
		return
	}

	// ── 同构折叠 ──
	//
	// 把「连接关系完全相同」的一组对象折成一个池。
	//
	//	⚠️ 分组依据随方向而变，这一点最容易写错：
	//
	//	  正向（依赖）：按**上游集合**给下游分组
	//	      15 个 LB 各挂同样 35 台机器 → 35 台折成一个「主机组」
	//
	//	  反向（影响面）：按**下游集合**给上游分组
	//	      15 个 LB 都指向同一台机器 → 15 个 LB 折成一个「负载均衡组」
	//
	//	第一版两个方向都按上游分组，结果反向视图完全不折叠：
	//	一台机器的影响面画出 15 个一模一样的 LB 方块。
	//	这个 bug 只有用**真实形态的数据**才会暴露 —— 空库和小样本都看不出来。
	//
	//	⚠️ 按签名分组，**不按名字前缀猜**。名字相似但连接不同的必须分开 ——
	//	把不同的事实合并成一个，比乱更危险。
	peers := map[int64]map[int64]bool{} // 待折叠节点 -> 它另一端的集合
	meta := map[int64]poolMember{}
	for _, e := range raws {
		// 正向展开 src→dst，所以折 dst；反向展开 dst→src，所以折 src
		foldee, other := e.dst, e.src
		if dir == "reverse" {
			foldee, other = e.src, e.dst
		}
		if peers[foldee] == nil {
			peers[foldee] = map[int64]bool{}
		}
		peers[foldee][other] = true
		meta[e.src] = poolMember{CIID: e.src, Name: e.srcName, Type: e.srcType}
		meta[e.dst] = poolMember{CIID: e.dst, Name: e.dstName, Type: e.dstType}
	}

	sigOf := func(set map[int64]bool) string {
		ids := make([]int64, 0, len(set))
		for id := range set {
			ids = append(ids, id)
		}
		sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
		parts := make([]string, len(ids))
		for i, id := range ids {
			parts[i] = strconv.FormatInt(id, 10)
		}
		return strings.Join(parts, ",")
	}

	// 同一签名 + 同一类型的下游归为一组。类型也要参与分组：
	// 上游相同但类型不同的对象（比如同一个 LB 同时挂主机和另一个 LB）
	// 合并会画出类型混杂的池，看不出这是什么东西。
	groups := map[string][]int64{}
	for id, set := range peers {
		// 中心节点永远单独成点，不参与折叠 —— 它是用户选中的对象
		if id == center {
			continue
		}
		key := meta[id].Type + "|" + sigOf(set)
		groups[key] = append(groups[key], id)
	}

	poolOf := map[int64]string{} // dst ci_id -> pool node id
	nodes := map[string]*graphNode{}
	addSingle := func(id int64) string {
		nid := fmt.Sprintf("ci:%d", id)
		if nodes[nid] == nil {
			m := meta[id]
			nodes[nid] = &graphNode{ID: nid, CIID: id, Name: m.Name, Type: m.Type, Layer: layerOf(m.Type)}
		}
		return nid
	}

	for key, members := range groups {
		// 只有 2 个及以上成员才折叠。1 个成员的"池"是纯粹的噪音。
		if len(members) < 2 {
			continue
		}
		sort.Slice(members, func(i, j int) bool { return meta[members[i]].Name < meta[members[j]].Name })
		typ := meta[members[0]].Type
		nid := "pool:" + strconv.Itoa(len(nodes)) + ":" + typ
		pm := make([]poolMember, 0, len(members))
		for _, m := range members {
			pm = append(pm, meta[m])
			poolOf[m] = nid
		}
		_ = key
		nodes[nid] = &graphNode{
			ID: nid, Name: poolLabel(typ), Type: typ, Layer: layerOf(typ),
			Pool: true, Count: len(members), Members: pm,
		}
		resp.Stats.PooledNodes += len(members)
	}

	// ── 生成边（折叠后去重并计数）──
	edgeAgg := map[string]*graphEdge{}
	for _, e := range raws {
		src := poolOf[e.src]
		if src == "" {
			src = addSingle(e.src)
		}
		dst := poolOf[e.dst]
		if dst == "" {
			dst = addSingle(e.dst)
		}
		if src == dst {
			continue // 折叠后自环无意义
		}
		k := src + "->" + dst + "|" + e.relType
		if edgeAgg[k] == nil {
			edgeAgg[k] = &graphEdge{Src: src, Dst: dst, Type: e.relType}
		}
		edgeAgg[k].Count++
	}

	for _, n := range nodes {
		resp.Nodes = append(resp.Nodes, *n)
	}
	for _, e := range edgeAgg {
		resp.Edges = append(resp.Edges, *e)
	}
	sort.Slice(resp.Nodes, func(i, j int) bool {
		if resp.Nodes[i].Layer != resp.Nodes[j].Layer {
			return resp.Nodes[i].Layer < resp.Nodes[j].Layer
		}
		return resp.Nodes[i].Name < resp.Nodes[j].Name
	})
	resp.Stats.ShownEdges = len(resp.Edges)
	c.JSON(http.StatusOK, resp)
}

func poolLabel(t string) string {
	switch t {
	case "host":
		return "主机组"
	case ciTypeLB:
		return "负载均衡组"
	case "domain":
		return "域名组"
	case "certificate":
		return "证书组"
	}
	return t + " 组"
}
