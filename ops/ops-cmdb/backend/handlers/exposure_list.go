package handlers

import (
	"database/sql"
	"net"
	"net/http"
	"sort"
	"strings"

	"github.com/gin-gonic/gin"

	"ops-cmdb-backend/internal/httpx"
)

// 暴露面：把"从公网能打到什么"汇成一张表。
//
// 数据来自三处，判据与各自列表页**完全一致**（不另起一套）：
//	云负载均衡   scheme=EXTERNAL 且 VIP 是公网地址
//	k8s Service  有 Ingress 主机名，或外部 IP 是公网地址（见 svcs_list.go）
//	主机         有外网 IP
//
// ⚠️ 判据不一致的话，安全复核会得出和资源页不同的结论，而没人说得清哪个对。

type ExposureHandler struct{ DB *sql.DB }

func NewExposureHandler(db *sql.DB) *ExposureHandler { return &ExposureHandler{DB: db} }

func (h *ExposureHandler) Register(r *gin.RouterGroup) {
	r.GET("/exposure-list", h.List)
}

type exposureOut struct {
	// Kind lb / service / host
	Kind string `json:"kind"`
	Name string `json:"name"`
	// Endpoint 公网可达的入口（IP 或主机名）
	Endpoint string `json:"endpoint"`
	Ports    string `json:"ports"`
	Scope    string `json:"scope"`
	// Protected 前面是否挂了 CDN/WAF 之类。
	//
	// ⚠️ 用指针：null = **我们没法判断**（没接 CDN 数据源），不是"没有防护"。
	// 兜底成 false 会让一张安全清单声称"这 40 个入口全都裸奔"，
	// 而其中一半可能在 CDN 后面 —— 假报告比没有报告更糟。
	Protected *bool `json:"protected"`

	// PortsKnown 我们**知不知道**这个入口开了哪些端口。
	//
	// ⚠️ 必须和 Ports 分开：空字符串会被读成"没开端口"，
	//	而主机行原本就一直是空的（端口由防火墙规则决定，不在主机记录里）。
	//	一张安全清单上，"不知道"被显示成"没有"是最危险的那种错
	//	（OPSCMDB-031 P1-52：7 台有公网 IP 的主机端口列全是 –，
	//	 于是"这台机器能被访问到什么"在界面上无从得知）。
	PortsKnown bool `json:"ports_known"`

	// PortsBasis 端口是从哪儿判出来的，便于复核时追到源头。
	//	lb_rule / service_spec / firewall / none
	//
	// ⚠️ **风险分级不在这里做**：前端已有一套 isAllPorts/portRisk，
	//	且刻意与防火墙页保持同一判据（"两处给出不同结论会让人无所适从"）。
	//	后端再加一套就是第三份实现 —— 本项目已经因为"同一判据写了两遍"
	//	出过三次「两个页面对同一事实给出相反结论」。
	//	这里只负责**把数据供全**，判档留给唯一那处。
	PortsBasis string `json:"ports_basis"`
}

// List GET /api/exposure-list
//
//	@Summary		暴露面
//	@Description	公网可达的入口清单。判据与各资源页一致；无法判断防护状态时返回 null 而非 false。
//	@Tags			security
//	@Produce		json
//	@Param			kind	query	string	false	"类别"	Enums(all, lb, service, host)
//	@Success		200		{object}	httpx.ListResponse[handlers.exposureOut]
//	@Router			/exposure-list [get]
func (h *ExposureHandler) List(c *gin.Context) {
	q := httpx.BindPage(c, "kind")
	all := []exposureOut{}

	// 云 LB：EXTERNAL 且 VIP 是公网
	if rows, err := h.DB.Query(`SELECT name, COALESCE(vip,''), COALESCE(port_range,''),
		COALESCE(protocol,''), project FROM cloud_loadbalancers
		WHERE stale = 0 AND scheme = 'EXTERNAL'`); err == nil {
		for rows.Next() {
			var name, vip, ports, proto, project string
			if rows.Scan(&name, &vip, &ports, &proto, &project) == nil && isPublicIP(vip) {
				all = append(all, exposureOut{
					Kind: "lb", Name: name, Endpoint: vip,
					Ports: strings.TrimSpace(ports + " " + proto), Scope: project,
					PortsKnown: true, PortsBasis: "lb_rule",
				})
			}
		}
		rows.Close()
	}

	// k8s Service：与 svcs_list.go 同一条判据
	if rows, err := h.DB.Query(`SELECT s.namespace, s.name, COALESCE(s.external_ip,''),
		COALESCE(s.ports,''), COALESCE(cl.name, ''), COALESCE(cl.display_name, cl.name, ''),
		COALESCE((SELECT GROUP_CONCAT(DISTINCT i.hosts) FROM k8s_ingresses i
		          WHERE i.cluster_id = s.cluster_id AND i.namespace = s.namespace
		            AND FIND_IN_SET(s.name, REPLACE(i.svc_names,' ','')) > 0), '')
		FROM k8s_services s LEFT JOIN k8s_clusters cl ON cl.id = s.cluster_id`); err == nil {
		for rows.Next() {
			var ns, name, extIP, ports, clName, clDisp, hosts string
			if rows.Scan(&ns, &name, &extIP, &ports, &clName, &clDisp, &hosts) != nil {
				continue
			}
			// Scope 是拼给人看的单串，用全站统一口径（技术名必须可见）
			cluster := clusterLabel(clDisp, clName)
			endpoint := ""
			if hosts != "" {
				endpoint = hosts
			} else if isPublicIP(extIP) {
				endpoint = extIP
			}
			if endpoint == "" {
				continue
			}
			all = append(all, exposureOut{
				Kind: "service", Name: ns + "/" + name, Endpoint: endpoint,
				Ports: ports, Scope: cluster,
				PortsKnown: ports != "", PortsBasis: "service_spec",
			})
		}
		rows.Close()
	}

	// 主机：有外网 IP。
	//
	// # ⚠️ 端口必须从防火墙规则推，不能留空
	//
	//	主机记录里没有"开了哪些端口"这回事 —— 那由 VPC 防火墙决定。
	//	原来这一列一直是空的，界面渲染成 –，读起来就是"没开端口"，
	//	而实际上有公网 IP 的机器能被访问到什么，**完全取决于这些规则**
	//	（OPSCMDB-031 P1-52：产品自己就有防火墙页，两张表却没打通）。
	//
	//	只算**真正能从公网打进来的**规则：INGRESS + ALLOW + 未停用 + 源含 0.0.0.0/0。
	//	target_tags 为空表示作用于该网络内所有实例，否则要和主机的 network_tags 有交集。
	fwByProject := map[string][]hostFirewall{}
	if rows, err := h.DB.Query(`SELECT project, COALESCE(protocols,''), COALESCE(target_tags,'')
		FROM cloud_firewalls
		WHERE direction = 'INGRESS' AND action = 'ALLOW' AND disabled = 0
		  AND source_ranges LIKE '%0.0.0.0/0%'`); err == nil {
		for rows.Next() {
			var project, protocols, tags string
			if rows.Scan(&project, &protocols, &tags) == nil {
				fwByProject[project] = append(fwByProject[project], hostFirewall{
					protocols: protocols, targetTags: splitTags(tags),
				})
			}
		}
		rows.Close()
	}

	if rows, err := h.DB.Query(`SELECT c.name, h.external_ip, h.project, COALESCE(h.network_tags,'')
		FROM hosts h JOIN cis c ON c.id = h.ci_id
		WHERE c.type='host' AND h.stale = 0 AND h.external_ip <> ''`); err == nil {
		for rows.Next() {
			var name, ip, project, tags string
			if rows.Scan(&name, &ip, &project, &tags) != nil || !isPublicIP(ip) {
				continue
			}
			ports, known, basis := hostOpenPorts(fwByProject[project], splitTags(tags))
			all = append(all, exposureOut{
				Kind: "host", Name: name, Endpoint: ip, Scope: project,
				Ports: ports, PortsKnown: known, PortsBasis: basis,
			})
		}
		rows.Close()
	}

	// Protected 全部保持 nil：没接 CDN/WAF 数据源，我们判不了。
	// 这一点必须在界面上说清楚，而不是显示成"未防护"
	_ = net.ParseIP // isPublicIP 用到 net，这里显式引用避免误删导入

	kind := q.Filters["kind"]
	facets := map[string]map[string]int64{"kind": {}}
	for _, e := range all {
		facets["kind"][e.Kind]++
		facets["kind"]["all"]++
	}
	filtered := make([]exposureOut, 0, len(all))
	for _, e := range all {
		if q.Keyword != "" &&
			!strings.Contains(strings.ToLower(e.Name+" "+e.Endpoint), strings.ToLower(q.Keyword)) {
			continue
		}
		if kind != "" && kind != "all" && e.Kind != kind {
			continue
		}
		filtered = append(filtered, e)
	}
	sort.SliceStable(filtered, func(i, j int) bool {
		if filtered[i].Kind != filtered[j].Kind {
			return filtered[i].Kind < filtered[j].Kind
		}
		return filtered[i].Name < filtered[j].Name
	})

	total := int64(len(filtered))
	lo := min(q.Offset(), len(filtered))
	hi := min(lo+q.Size, len(filtered))
	c.JSON(http.StatusOK, httpx.NewList(filtered[lo:hi], q, total).WithFacets(facets))
}

// hostFirewall 一条能从公网打进来的入站规则（已筛过 source_ranges 含 0.0.0.0/0）。
type hostFirewall struct {
	protocols  string
	targetTags []string
}

func splitTags(s string) []string {
	out := []string{}
	for _, t := range strings.Split(s, ",") {
		if t = strings.TrimSpace(t); t != "" {
			out = append(out, t)
		}
	}
	return out
}

// hostOpenPorts 把作用于这台主机的规则合成一行端口描述。
//
// 返回 known=false 的两种情形要分清：
//   - 该 project 一条公网入站规则都没采到 → 我们不知道（可能是没采防火墙，也可能真没规则）
//   - 采到了但没有一条命中这台主机 → 这台机器**没有**公网入站放行，是可以下结论的
func hostOpenPorts(rules []hostFirewall, hostTags []string) (ports string, known bool, basis string) {
	if len(rules) == 0 {
		return "", false, "none"
	}
	tagSet := map[string]bool{}
	for _, t := range hostTags {
		tagSet[t] = true
	}
	seen := map[string]bool{}
	var parts []string
	for _, r := range rules {
		applies := len(r.targetTags) == 0 // 空 = 作用于网络内所有实例
		for _, t := range r.targetTags {
			if tagSet[t] {
				applies = true
				break
			}
		}
		if !applies {
			continue
		}
		for _, p := range strings.Split(r.protocols, ",") {
			if p = strings.TrimSpace(p); p != "" && !seen[p] {
				seen[p] = true
				parts = append(parts, p)
			}
		}
	}
	if len(parts) == 0 {
		// 采到了规则、但没有一条命中 → 这是个确定的结论
		return "无公网入站放行", true, "firewall"
	}
	sort.Strings(parts)
	return strings.Join(parts, ", "), true, "firewall"
}
