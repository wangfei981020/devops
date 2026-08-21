package handlers

import (
	"database/sql"
	"net"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"ops-cmdb-backend/internal/httpx"
)

// 服务与入口。以 **Service** 为行，把指向它的 Ingress 主机名并到同一行 ——
// 排障时问的是"这个服务从外面怎么进来"，把 Service 和 Ingress 分成两页，
// 人得自己在脑子里做这个 join。

type SvcListHandler struct{ DB *sql.DB }

func NewSvcListHandler(db *sql.DB) *SvcListHandler { return &SvcListHandler{DB: db} }

func (h *SvcListHandler) Register(r *gin.RouterGroup) {
	r.GET("/k8s/service-list", h.List)
}

type svcOut struct {
	ClusterID   int    `json:"cluster_id"`
	ClusterName string `json:"cluster_name"`
	// ClusterDisplay 别名；没设时等于技术名。
	//
	// 🔴 cluster_name 装的必须是**技术名**（kubectl / PromQL 标签里的那个）。
	//	原来这里装的是 COALESCE(display_name, name) —— 一个字段两种含义，
	//	前端拿到「开发环境集群」就再也拿不到 dev-k8s-cluster-01，
	//	想按全站统一的 clusterLabel 渲染也做不到（OPSCMDB-078）。
	ClusterDisplay string `json:"cluster_display_name"`
	Namespace      string `json:"namespace"`
	Name           string `json:"name"`
	// Type 原样透传：ClusterIP / NodePort / LoadBalancer / ExternalName
	Type       string `json:"type"`
	ClusterIP  string `json:"cluster_ip"`
	ExternalIP string `json:"external_ip"`
	Ports      string `json:"ports"`

	// Hosts 指向这个 Service 的 Ingress 主机名（去重）。
	// 空数组 = 没有 Ingress 指向它，这是**信息**（内部服务本来就没有）
	Hosts []string `json:"hosts"`

	// Exposed 是不是对外可达。
	//
	// ⚠️ 判据是"有外部 IP 或有 Ingress 主机名"，不是"type==LoadBalancer"：
	// 内网 LB（scheme=INTERNAL）也是 LoadBalancer 类型，把它算成对外暴露
	// 会让暴露面清单里塞满误报，而误报多了真的就没人看了。
	Exposed bool `json:"exposed"`

	// InternalLB 拿到的是私网 VIP —— 内网负载均衡，不算对外暴露。
	// 单独出一个字段而不是只让它落进 internal：界面上要能说清
	// "它有 VIP，只是那个 VIP 在内网"，否则看起来像没配好。
	InternalLB bool `json:"internal_lb"`

	// PendingLB type=LoadBalancer 但一直没拿到外部 IP。
	// 这不是"没暴露"，是**卡住了** —— 云厂商配额用尽、
	// 子网没空 IP 时就是这个现象，而 Service 看起来一切正常。
	PendingLB bool   `json:"pending_lb"`
	SyncedAt  string `json:"synced_at,omitempty"`
}

// List GET /api/k8s/service-list
//
//	@Summary		服务与入口
//	@Description	Service 为行，指向它的 Ingress 主机名并入同一行。LoadBalancer 拿不到外部 IP 会单独标注。
//	@Tags			k8s
//	@Produce		json
//	@Param			page		query		int		false	"页码，从 1 开始"
//	@Param			size		query		int		false	"每页条数"
//	@Param			q			query		string	false	"按服务名/主机名/IP 搜索"
//	@Param			cluster		query		string	false	"集群名，all 或集群名"
//	@Param			namespace	query		string	false	"命名空间，all 或具体值"
//	@Param			exposure	query		string	false	"暴露情况"	Enums(all, exposed, pending, internal)
//	@Success		200			{object}	httpx.ListResponse[handlers.svcOut]
//	@Router			/k8s/service-list [get]
func (h *SvcListHandler) List(c *gin.Context) {
	q := httpx.BindPage(c, "cluster", "namespace", "exposure")

	rows, err := h.DB.Query(`SELECT s.cluster_id, COALESCE(cl.name, ''), COALESCE(cl.display_name, cl.name, ''),
		s.namespace, s.name, COALESCE(s.type,''), COALESCE(s.cluster_ip,''),
		COALESCE(s.external_ip,''), COALESCE(s.ports,''), s.synced_at
		FROM k8s_services s
		LEFT JOIN k8s_clusters cl ON cl.id = s.cluster_id
		ORDER BY s.cluster_id, s.namespace, s.name`)
	if err != nil {
		httpx.Fail(c, httpx.CodeInternal, err, nil)
		return
	}
	defer rows.Close()

	all := []svcOut{}
	for rows.Next() {
		var o svcOut
		var synced sql.NullTime
		if err := rows.Scan(&o.ClusterID, &o.ClusterName, &o.ClusterDisplay, &o.Namespace, &o.Name, &o.Type,
			&o.ClusterIP, &o.ExternalIP, &o.Ports, &synced); err != nil {
			continue
		}
		o.Hosts = []string{}
		if synced.Valid {
			o.SyncedAt = synced.Time.Format(time.RFC3339)
		}
		all = append(all, o)
	}

	h.fillIngress(all)
	for i := range all {
		// ⚠️ 有 external_ip **不等于**对外可达：k8s 把内网 LB 的 VIP
		// 也写进同一个字段（status.loadBalancer.ingress）。
		// 不排掉私网地址的话，每个内网 LB 都会进暴露面清单 ——
		// 而误报多了，真正暴露在公网的那几个就没人看了。
		all[i].Exposed = len(all[i].Hosts) > 0 || isPublicIP(all[i].ExternalIP)
		all[i].InternalLB = all[i].ExternalIP != "" && !isPublicIP(all[i].ExternalIP)
		all[i].PendingLB = all[i].Type == "LoadBalancer" && all[i].ExternalIP == ""
	}

	items, total, facets := svcPage(all, q)
	c.JSON(http.StatusOK, httpx.NewList(items, q, total).WithFacets(facets))
}

// fillIngress 把 Ingress 的主机名并到对应 Service 上。
//
// svc_names 是逗号分隔的后端 service 名（采集时就这么存的），
// 所以这里在内存里拆开配对，而不是在 SQL 里 FIND_IN_SET ——
// 后者在这张表上走不了索引，而且可读性差得多。
func (h *SvcListHandler) fillIngress(all []svcOut) {
	if len(all) == 0 {
		return
	}
	type key struct {
		cid int
		ns  string
		svc string
	}
	hosts := map[key]map[string]bool{}
	rows, err := h.DB.Query(`SELECT cluster_id, namespace, COALESCE(hosts,''), COALESCE(svc_names,'')
		FROM k8s_ingresses`)
	if err != nil {
		return
	}
	defer rows.Close()
	for rows.Next() {
		var cid int
		var ns, hs, svcs string
		if rows.Scan(&cid, &ns, &hs, &svcs) != nil {
			continue
		}
		for _, svc := range strings.Split(svcs, ",") {
			svc = strings.TrimSpace(svc)
			if svc == "" {
				continue
			}
			k := key{cid, ns, svc}
			if hosts[k] == nil {
				hosts[k] = map[string]bool{}
			}
			for _, hst := range strings.Split(hs, ",") {
				if hst = strings.TrimSpace(hst); hst != "" {
					hosts[k][hst] = true
				}
			}
		}
	}
	for i := range all {
		set := hosts[key{all[i].ClusterID, all[i].Namespace, all[i].Name}]
		out := make([]string, 0, len(set))
		for hst := range set {
			out = append(out, hst)
		}
		sort.Strings(out) // 顺序稳定：随 map 遍历变的话，同一行每次刷新都在动
		all[i].Hosts = out
	}
}

// isPublicIP 判断是不是公网地址。
//
// 只认 IPv4/IPv6 的私网 + 回环 + 链路本地：够覆盖云上 LB 的全部情形，
// 而把判断写得更"聪明"（比如查路由表）会引入一个离线环境里没法验证的依赖。
//
// ⚠️ 解析失败时返回 false（当成非公网）。反过来会让一个我们读不懂的值
// 被当成公网地址直接进暴露面清单 —— 宁可漏一条待人工核对，
// 也不要往安全清单里塞噪声。
func isPublicIP(s string) bool {
	ip := net.ParseIP(strings.TrimSpace(s))
	if ip == nil {
		return false
	}
	return !ip.IsPrivate() && !ip.IsLoopback() && !ip.IsLinkLocalUnicast() && !ip.IsUnspecified()
}

func svcExposure(s svcOut) string {
	if s.PendingLB {
		return "pending"
	}
	if s.Exposed {
		return "exposed"
	}
	return "internal"
}

func svcPage(all []svcOut, q httpx.PageQuery) ([]svcOut, int64, map[string]map[string]int64) {
	match := func(s svcOut) bool {
		if q.Keyword == "" {
			return true
		}
		kw := strings.ToLower(q.Keyword)
		return strings.Contains(strings.ToLower(s.Name), kw) ||
			strings.Contains(strings.ToLower(s.ExternalIP), kw) ||
			strings.Contains(strings.ToLower(strings.Join(s.Hosts, ",")), kw)
	}
	cluster, ns, exposure := q.Filters["cluster"], q.Filters["namespace"], q.Filters["exposure"]
	facets := map[string]map[string]int64{"cluster": {}, "namespace": {}, "exposure": {}}
	for _, s := range all {
		if !match(s) {
			continue
		}
		if (cluster == "" || cluster == "all" || s.ClusterName == cluster) &&
			(ns == "" || ns == "all" || s.Namespace == ns) {
			facets["exposure"][svcExposure(s)]++
			facets["exposure"]["all"]++
		}
		if (exposure == "" || exposure == "all" || svcExposure(s) == exposure) &&
			(ns == "" || ns == "all" || s.Namespace == ns) {
			facets["cluster"][s.ClusterName]++
			facets["cluster"]["all"]++
		}
		if (exposure == "" || exposure == "all" || svcExposure(s) == exposure) &&
			(cluster == "" || cluster == "all" || s.ClusterName == cluster) {
			facets["namespace"][s.Namespace]++
			facets["namespace"]["all"]++
		}
	}

	filtered := make([]svcOut, 0, len(all))
	for _, s := range all {
		if !match(s) {
			continue
		}
		if cluster != "" && cluster != "all" && s.ClusterName != cluster {
			continue
		}
		if ns != "" && ns != "all" && s.Namespace != ns {
			continue
		}
		if exposure != "" && exposure != "all" && svcExposure(s) != exposure {
			continue
		}
		filtered = append(filtered, s)
	}

	sort.SliceStable(filtered, func(i, j int) bool {
		a, b := filtered[i], filtered[j]
		var less bool
		switch q.SortBy {
		case "namespace":
			less = a.Namespace < b.Namespace
		default:
			// 卡住的 LB 排最前：它看起来一切正常，实际一直没拿到 IP
			if a.PendingLB != b.PendingLB {
				return a.PendingLB
			}
			// 其次是对外暴露的：安全复核先看这些
			if a.Exposed != b.Exposed {
				return a.Exposed
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
