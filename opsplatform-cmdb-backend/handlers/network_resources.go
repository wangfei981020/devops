package handlers

import (
	"database/sql"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"opsplatform-cmdb-backend/cloudsource"
)

func itoa(n int) string { return strconv.Itoa(n) }

// NetworkHandler 云网络资源只读台账：VPC/子网/防火墙/静态IP/负载均衡（多云预留 provider，当前 GCP）。
type NetworkHandler struct{ DB *sql.DB }

func NewNetworkHandler(db *sql.DB) *NetworkHandler { return &NetworkHandler{DB: db} }

func (h *NetworkHandler) Register(r *gin.RouterGroup) {
	r.GET("/cloud-networks", h.ListNetworks)
	r.GET("/cloud-subnets", h.ListSubnets)
	r.GET("/cloud-firewalls", h.ListFirewalls)
	r.GET("/cloud-addresses", h.ListAddresses)
	r.GET("/cloud-loadbalancers", h.ListLoadBalancers)
	r.GET("/cloud-ips", h.ListIPs) // 聚合视图
}

// 敏感端口口径统一在 expose_surface.go 的 sensitivePortNames，这里直接复用其字符串形式，
// 避免防火墙判定和暴露面判定各记一份导致结论互相打架（原先这里缺 ZooKeeper/Nacos/Kafka 等端口）。

// fwHighRisk 入站 allow + 源含 0.0.0.0/0 + 命中敏感端口(或 all/未限端口) → 高危。
func fwHighRisk(direction, action, protocols, sourceRanges string) bool {
	if strings.ToUpper(direction) != "INGRESS" || action != "allow" {
		return false
	}
	if !strings.Contains(sourceRanges, "0.0.0.0/0") {
		return false
	}
	p := strings.ToLower(protocols)
	if p == "" || strings.Contains(p, "all") {
		return true // 全放行
	}
	for port := range sensitivePorts {
		if strings.Contains(protocols, ":"+port) || strings.HasSuffix(protocols, ","+port) || strings.Contains(protocols, ","+port+",") || strings.HasSuffix(protocols, ":"+port) {
			return true
		}
	}
	// 协议未带端口（如裸 tcp/udp）视为放开该协议全部端口
	for _, seg := range strings.Split(protocols, ";") {
		if seg != "" && !strings.Contains(seg, ":") {
			return true
		}
	}
	return false
}

// SyncProjectNetwork 把一个 project 的网络资源刷进库（只读镜像：按 account+project 删旧插新）。
func SyncProjectNetwork(db *sql.DB, provider string, accountID int, project string, nr *cloudsource.NetworkResources) {
	if provider == "" {
		provider = "gcp"
	}
	// VPC
	logExec(db, "网络同步写", `DELETE FROM cloud_networks WHERE cloud_account_id=? AND project=?`, accountID, project)
	for _, n := range nr.Networks {
		logExec(db, "网络同步写", `INSERT INTO cloud_networks (provider,cloud_account_id,project,name,mode,self_link,synced_at) VALUES (?,?,?,?,?,?,NOW())`,
			provider, accountID, project, n.Name, n.Mode, n.SelfLink)
	}
	// 子网
	logExec(db, "网络同步写", `DELETE FROM cloud_subnets WHERE cloud_account_id=? AND project=?`, accountID, project)
	for _, s := range nr.Subnets {
		logExec(db, "网络同步写", `INSERT INTO cloud_subnets (provider,cloud_account_id,project,name,network,region,cidr,gateway,self_link,synced_at) VALUES (?,?,?,?,?,?,?,?,?,NOW())`,
			provider, accountID, project, s.Name, s.Network, s.Region, s.CIDR, s.Gateway, s.SelfLink)
	}
	// 防火墙
	logExec(db, "网络同步写", `DELETE FROM cloud_firewalls WHERE cloud_account_id=? AND project=?`, accountID, project)
	for _, f := range nr.Firewalls {
		hr := 0
		if fwHighRisk(f.Direction, f.Action, f.Protocols, f.SourceRanges) {
			hr = 1
		}
		disabled := 0
		if f.Disabled {
			disabled = 1
		}
		logExec(db, "网络同步写", `INSERT INTO cloud_firewalls (provider,cloud_account_id,project,name,network,direction,priority,action,protocols,source_ranges,target_tags,disabled,high_risk,self_link,synced_at)
			VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,NOW())`,
			provider, accountID, project, f.Name, f.Network, f.Direction, f.Priority, f.Action, f.Protocols, f.SourceRanges, f.TargetTags, disabled, hr, f.SelfLink)
	}
	// 静态IP
	logExec(db, "网络同步写", `DELETE FROM cloud_addresses WHERE cloud_account_id=? AND project=?`, accountID, project)
	for _, a := range nr.Addresses {
		logExec(db, "网络同步写", `INSERT INTO cloud_addresses (provider,cloud_account_id,project,name,address,addr_type,status,region,users,self_link,synced_at) VALUES (?,?,?,?,?,?,?,?,?,?,NOW())`,
			provider, accountID, project, a.Name, a.Address, a.Type, a.Status, a.Region, a.Users, a.SelfLink)
	}
	// 负载均衡 + 后端成员
	logExec(db, "网络同步写", `DELETE FROM cloud_loadbalancers WHERE cloud_account_id=? AND project=?`, accountID, project)
	logExec(db, "网络同步写", `DELETE FROM cloud_lb_backends WHERE cloud_account_id=? AND project=?`, accountID, project)
	for _, l := range nr.LoadBalancers {
		logExec(db, "网络同步写", `INSERT INTO cloud_loadbalancers (provider,cloud_account_id,project,name,scheme,vip,port_range,protocol,target,region,self_link,synced_at) VALUES (?,?,?,?,?,?,?,?,?,?,?,NOW())`,
			provider, accountID, project, l.Name, l.Scheme, l.VIP, l.PortRange, l.Protocol, l.Target, l.Region, l.SelfLink)
		for _, b := range l.Backends {
			logExec(db, "网络同步写", `INSERT INTO cloud_lb_backends (cloud_account_id,project,lb_name,instance,group_name,zone,synced_at) VALUES (?,?,?,?,?,?,NOW())`,
				accountID, project, l.Name, b.Instance, b.Group, b.Zone)
		}
	}
}

// projName 项目自定义名映射（account+project_id -> name），列表展示用。
func (h *NetworkHandler) projNames() map[string]string {
	m := map[string]string{}
	rows, _ := h.DB.Query(`SELECT account_id, project_id, name FROM cloud_account_projects`)
	if rows != nil {
		for rows.Next() {
			var aid int
			var pid, name string
			if rows.Scan(&aid, &pid, &name) == nil {
				m[itoa(aid)+"/"+pid] = name
			}
		}
		rows.Close()
	}
	return m
}

func (h *NetworkHandler) ListNetworks(c *gin.Context) {
	rows, err := h.DB.Query(`SELECT n.provider, n.cloud_account_id, n.project, n.name, n.mode,
		(SELECT COUNT(*) FROM cloud_subnets s WHERE s.cloud_account_id=n.cloud_account_id AND s.project=n.project AND s.network=n.name),
		(SELECT COUNT(*) FROM cloud_firewalls f WHERE f.cloud_account_id=n.cloud_account_id AND f.project=n.project AND f.network=n.name)
		FROM cloud_networks n ORDER BY n.provider, n.name`)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	defer rows.Close()
	pn := h.projNames()
	out := []gin.H{}
	for rows.Next() {
		var provider, project, name, mode string
		var aid, subnets, fws int
		if rows.Scan(&provider, &aid, &project, &name, &mode, &subnets, &fws) == nil {
			out = append(out, gin.H{"provider": provider, "project": pn[itoa(aid)+"/"+project], "project_id": project,
				"name": name, "mode": mode, "subnet_count": subnets, "firewall_count": fws})
		}
	}
	c.JSON(http.StatusOK, out)
}

func (h *NetworkHandler) ListSubnets(c *gin.Context) {
	rows, err := h.DB.Query(`SELECT provider, cloud_account_id, project, name, network, region, cidr, gateway FROM cloud_subnets ORDER BY provider, network, region, name`)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	defer rows.Close()
	pn := h.projNames()
	out := []gin.H{}
	for rows.Next() {
		var provider, project, name, network, region, cidr, gw string
		var aid int
		if rows.Scan(&provider, &aid, &project, &name, &network, &region, &cidr, &gw) == nil {
			out = append(out, gin.H{"provider": provider, "project": pn[itoa(aid)+"/"+project], "name": name,
				"network": network, "region": region, "cidr": cidr, "gateway": gw})
		}
	}
	c.JSON(http.StatusOK, out)
}

func (h *NetworkHandler) ListFirewalls(c *gin.Context) {
	rows, err := h.DB.Query(`SELECT provider, cloud_account_id, project, name, network, direction, priority, action, protocols, source_ranges, target_tags, disabled, high_risk FROM cloud_firewalls ORDER BY provider, network, priority`)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	defer rows.Close()
	pn := h.projNames()
	out := []gin.H{}
	for rows.Next() {
		var provider, project, name, network, direction, action, protocols, src, tags string
		var aid, priority, disabled, hr int
		if rows.Scan(&provider, &aid, &project, &name, &network, &direction, &priority, &action, &protocols, &src, &tags, &disabled, &hr) == nil {
			out = append(out, gin.H{"provider": provider, "project": pn[itoa(aid)+"/"+project], "name": name, "network": network,
				"direction": direction, "priority": priority, "action": action, "protocols": protocols,
				"source_ranges": src, "target_tags": tags, "disabled": disabled == 1, "high_risk": hr == 1})
		}
	}
	c.JSON(http.StatusOK, out)
}

func (h *NetworkHandler) ListAddresses(c *gin.Context) {
	rows, err := h.DB.Query(`SELECT provider, cloud_account_id, project, name, address, addr_type, status, region, users FROM cloud_addresses ORDER BY provider, region, address`)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	defer rows.Close()
	pn := h.projNames()
	out := []gin.H{}
	for rows.Next() {
		var provider, project, name, address, atype, status, region, users string
		var aid int
		if rows.Scan(&provider, &aid, &project, &name, &address, &atype, &status, &region, &users) == nil {
			out = append(out, gin.H{"provider": provider, "project": pn[itoa(aid)+"/"+project], "name": name,
				"address": address, "type": atype, "status": status, "region": region, "users": users})
		}
	}
	c.JSON(http.StatusOK, out)
}

func (h *NetworkHandler) ListLoadBalancers(c *gin.Context) {
	// 预取所有后端成员（LEFT JOIN 主机表取内网IP），按 账号/项目/LB 分组
	backends := map[string][]gin.H{}
	if brows, _ := h.DB.Query(`
		SELECT b.cloud_account_id, b.project, b.lb_name, b.instance, b.group_name, b.zone, COALESCE(h.internal_ip,'')
		FROM cloud_lb_backends b
		LEFT JOIN cis c ON c.type='host' AND c.name=b.instance
		LEFT JOIN hosts h ON h.ci_id=c.id AND h.project=b.project AND h.stale=0
		ORDER BY b.instance`); brows != nil {
		for brows.Next() {
			var aid int
			var project, lb, inst, grp, zone, ip string
			if brows.Scan(&aid, &project, &lb, &inst, &grp, &zone, &ip) == nil {
				key := itoa(aid) + "/" + project + "/" + lb
				backends[key] = append(backends[key], gin.H{"instance": inst, "group": grp, "zone": zone, "internal_ip": ip})
			}
		}
		brows.Close()
	}

	rows, err := h.DB.Query(`SELECT id, provider, cloud_account_id, project, name, scheme, vip, port_range, protocol, target, region, self_link FROM cloud_loadbalancers ORDER BY provider, name`)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	defer rows.Close()
	pn := h.projNames()
	out := []gin.H{}
	for rows.Next() {
		var provider, project, name, scheme, vip, port, protocol, target, region, selfLink string
		var id, aid int
		if rows.Scan(&id, &provider, &aid, &project, &name, &scheme, &vip, &port, &protocol, &target, &region, &selfLink) == nil {
			bs := backends[itoa(aid)+"/"+project+"/"+name]
			if bs == nil {
				bs = []gin.H{}
			}
			out = append(out, gin.H{"id": id, "provider": provider, "project": pn[itoa(aid)+"/"+project], "name": name,
				"scheme": scheme, "vip": vip, "port_range": port, "protocol": protocol, "target": target, "region": region,
				"self_link": selfLink, "backends": bs})
		}
	}
	c.JSON(http.StatusOK, out)
}

// ListIPs 聚合 IP 台账：静态IP(cloud_addresses) + 主机内外网IP(hosts) + 负载均衡VIP(cloud_loadbalancers)。
func (h *NetworkHandler) ListIPs(c *gin.Context) {
	out := []gin.H{}
	pn := h.projNames()
	// 静态/预留 IP
	if rows, _ := h.DB.Query(`SELECT provider, cloud_account_id, project, address, addr_type, status, region, users, name FROM cloud_addresses WHERE address<>''`); rows != nil {
		for rows.Next() {
			var provider, project, address, atype, status, region, users, name string
			var aid int
			if rows.Scan(&provider, &aid, &project, &address, &atype, &status, &region, &users, &name) == nil {
				kind := "外网(静态)"
				if strings.EqualFold(atype, "INTERNAL") {
					kind = "内网(静态)"
				}
				owner := users
				idle := status != "IN_USE" // 预留但没绑 → 闲置计费
				if owner == "" && idle {
					owner = "—（未绑定）"
				}
				out = append(out, gin.H{"provider": provider, "project": pn[itoa(aid)+"/"+project], "ip": address,
					"kind": kind, "owner": owner, "region": region, "idle": idle, "name": name})
			}
		}
		rows.Close()
	}
	// 主机内外网 IP
	if rows, _ := h.DB.Query(`SELECT h.provider, h.cloud_account_id, h.project, c.name, h.internal_ip, h.external_ip, h.region, h.vpc
		FROM hosts h JOIN cis c ON c.id=h.ci_id WHERE c.type='host' AND h.stale=0`); rows != nil {
		for rows.Next() {
			var provider, project, name, in, ex, region, vpc string
			var aid int
			if rows.Scan(&provider, &aid, &project, &name, &in, &ex, &region, &vpc) == nil {
				p := pn[itoa(aid)+"/"+project]
				if in != "" {
					out = append(out, gin.H{"provider": provider, "project": p, "ip": in, "kind": "内网", "owner": name, "region": region, "idle": false, "vpc": vpc})
				}
				if ex != "" {
					out = append(out, gin.H{"provider": provider, "project": p, "ip": ex, "kind": "外网", "owner": name, "region": region, "idle": false, "vpc": vpc})
				}
			}
		}
		rows.Close()
	}
	// 负载均衡 VIP
	if rows, _ := h.DB.Query(`SELECT provider, cloud_account_id, project, name, vip, region FROM cloud_loadbalancers WHERE vip<>''`); rows != nil {
		for rows.Next() {
			var provider, project, name, vip, region string
			var aid int
			if rows.Scan(&provider, &aid, &project, &name, &vip, &region) == nil {
				out = append(out, gin.H{"provider": provider, "project": pn[itoa(aid)+"/"+project], "ip": vip, "kind": "VIP", "owner": name, "region": region, "idle": false})
			}
		}
		rows.Close()
	}
	c.JSON(http.StatusOK, out)
}
