package handlers

import (
	"database/sql"
	"fmt"
	"net/http"
	"net/netip"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"opsplatform-cmdb-backend/cloudsource"
	"opsplatform-cmdb-backend/logx"
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
	if !fwSourceIsAnywhere(sourceRanges) {
		return false
	}
	p := strings.ToLower(strings.TrimSpace(protocols))
	if p == "" || strings.Contains(p, "all") {
		return true // 全放行
	}
	return fwOpensSensitivePort(p)
}

// fwSourceIsAnywhere 源网段里有没有"任意来源"。
//
//	⚠️ 不能用 strings.Contains(s, "0.0.0.0/0") 硬比字符串。
//	生产上 infra-it-04 的源写的是 `0.0.0.0`（**没有 /0**）——语义完全等同
//	全网放行，但字符串对不上，于是它在高危清单里**根本没出现**。
//	一条 source=0.0.0.0 + 全端口 + allow + 入站的规则被判成安全，
//	这比少报一条更糟：清单看着挺干净，真高危却不在里面。
//
//	用 net/netip 解析成前缀再判：只要前缀长度为 0（覆盖整个地址空间）
//	就是任意来源，v4 v6 都认。裸 IP 按 /32（/128）算，不算任意来源——
//	只有 0.0.0.0 这个特殊值除外，GCP 里它就是 0.0.0.0/0 的省略写法。
func fwSourceIsAnywhere(sourceRanges string) bool {
	for _, raw := range strings.FieldsFunc(sourceRanges, func(r rune) bool {
		return r == ',' || r == ';' || r == ' ' || r == '\n' || r == '\t'
	}) {
		v := strings.TrimSpace(raw)
		if v == "" {
			continue
		}
		if pfx, err := netip.ParsePrefix(v); err == nil {
			if pfx.Bits() == 0 {
				return true // 0.0.0.0/0 或 ::/0
			}
			continue
		}
		// 不带掩码的写法：0.0.0.0 / :: 都等同全网
		if addr, err := netip.ParseAddr(v); err == nil && addr.IsUnspecified() {
			return true
		}
	}
	return false
}

// fwOpensSensitivePort 解析防火墙的 protocols 串，判断是否放行了敏感端口。
// 格式形如 "tcp:80,443;udp:53"、"tcp:1000-2000"，或裸 "tcp"（等于放开该协议全部端口）。
//
// 必须真正解析出端口数字，不能用子串包含：":2181" 会匹配到 "tcp:21810"，
// 把一个普通业务端口误报成公网暴露 ZooKeeper。误报的代价不只是烦人——
// 高危清单里混进假货，真高危就会被一起忽略。
func fwOpensSensitivePort(protocols string) bool {
	for _, seg := range strings.Split(protocols, ";") {
		if seg = strings.TrimSpace(seg); seg == "" {
			continue
		}
		_, ports, hasPorts := strings.Cut(seg, ":")
		if !hasPorts || strings.TrimSpace(ports) == "" {
			return true // 裸 tcp/udp：未限端口即放开全部
		}
		for _, item := range strings.Split(ports, ",") {
			lo, hi, ok := parsePortRange(strings.TrimSpace(item))
			if !ok {
				continue
			}
			for port := range sensitivePortNames {
				if port >= lo && port <= hi {
					return true
				}
			}
		}
	}
	return false
}

// parsePortRange 解析单端口 "6379" 或端口范围 "1000-2000"。
func parsePortRange(s string) (lo, hi int, ok bool) {
	if a, b, found := strings.Cut(s, "-"); found {
		l, e1 := strconv.Atoi(strings.TrimSpace(a))
		r, e2 := strconv.Atoi(strings.TrimSpace(b))
		if e1 != nil || e2 != nil || l > r {
			return 0, 0, false
		}
		return l, r, true
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return 0, 0, false
	}
	return n, n, true
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
	syncLoadBalancers(db, provider, accountID, project, nr.LoadBalancers)
}

// syncLoadBalancers 写负载均衡主表 + 后端成员明细。
//
//	## 为什么单拎出来、为什么用事务
//
//	生产上 7 个 LB 长期是「采集时追溯到 35 个后端，但 cloud_lb_backends 里
//	一条都查不到」，而写入端**没有任何报错**（既无 exec_fail，也没触发
//	"只存进 M 条"的告警）。丢的还不是零散几条，是**按 project 整批丢**。
//
//	原来的写法是「先 DELETE 整个 project，再逐条 INSERT」，两者之间没有事务。
//	而网络同步是**按 project 并发跑**的：只要同一个 (account, project) 被两个
//	worker 同时处理（project 在 cloud_account_projects 里重复登记、或定时任务
//	和手动同步撞上），B 的 DELETE 就会插进 A 的「写完主表、正在插明细」的
//	窗口里，把 A 已经插进去的成员删掉。主表的 backend_count=35 早已落库，
//	明细却是空的——恰好就是生产看到的样子。
//
//	所以整段放进一个事务：DELETE 拿到的行锁会挡住另一个 worker 的 DELETE，
//	不再有中间窗口。并且插完立刻**在同一事务里回读计数**——
//	这是把"写没写进去"钉死的唯一办法，比事后猜有用得多。
func syncLoadBalancers(db *sql.DB, provider string, accountID int, project string, lbs []cloudsource.LoadBalancer) {
	tx, err := db.Begin()
	if err != nil {
		logx.J("network_sync", "lb_tx_begin_fail", map[string]any{
			"account": accountID, "project": project, "err": err.Error()})
		return
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	if _, err := tx.Exec(`DELETE FROM cloud_loadbalancers WHERE cloud_account_id=? AND project=?`, accountID, project); err != nil {
		logx.J("network_sync", "lb_delete_fail", map[string]any{
			"account": accountID, "project": project, "table": "cloud_loadbalancers", "err": err.Error()})
		return
	}
	if _, err := tx.Exec(`DELETE FROM cloud_lb_backends WHERE cloud_account_id=? AND project=?`, accountID, project); err != nil {
		logx.J("network_sync", "lb_delete_fail", map[string]any{
			"account": accountID, "project": project, "table": "cloud_lb_backends", "err": err.Error()})
		return
	}

	resolved, written := 0, 0
	for _, l := range lbs {
		if _, err := tx.Exec(`INSERT INTO cloud_loadbalancers (provider,cloud_account_id,project,name,scheme,vip,port_range,protocol,target,backend_state,backend_count,region,self_link,synced_at) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,NOW())`,
			provider, accountID, project, l.Name, l.Scheme, l.VIP, l.PortRange, l.Protocol, l.Target,
			l.BackendState, len(l.Backends), l.Region, l.SelfLink); err != nil {
			logx.J("network_sync", "lb_insert_fail", map[string]any{
				"account": accountID, "project": project, "lb": l.Name, "err": err.Error()})
			return
		}
		resolved += len(l.Backends)
		for _, b := range l.Backends {
			if _, err := tx.Exec(`INSERT INTO cloud_lb_backends (cloud_account_id,project,lb_name,instance,group_name,zone,synced_at) VALUES (?,?,?,?,?,?,NOW())`,
				accountID, project, l.Name, b.Instance, b.Group, b.Zone); err != nil {
				// 一条明细写不进去就整批回滚：宁可这个 project 的 LB 保持上一轮的
				// 旧数据，也不要留下"主表说有 35 个后端、明细只有 12 条"的残缺状态
				logx.J("network_sync", "lb_backend_insert_fail", map[string]any{
					"account": accountID, "project": project, "lb": l.Name,
					"instance": b.Instance, "err": err.Error()})
				return
			}
			written++
		}
		// 写入端和读取端各自打出自己用的 key，对不上时肉眼即可比对
		if len(l.Backends) > 0 {
			logx.J("network_sync", "lb_backends_written", map[string]any{
				"key": lbKey(accountID, project, l.Name), "n": len(l.Backends), "state": l.BackendState})
		}
	}

	// 回读校验：还在事务里，读到的就是这次真正写进去的行数
	var back int
	if err := tx.QueryRow(`SELECT COUNT(*) FROM cloud_lb_backends WHERE cloud_account_id=? AND project=?`,
		accountID, project).Scan(&back); err != nil {
		logx.J("network_sync", "lb_readback_fail", map[string]any{
			"account": accountID, "project": project, "err": err.Error()})
	} else if back != written {
		logx.Line("network_sync", fmt.Sprintf(
			"WARN project=%s 后端成员写入 %d 条，事务内回读只有 %d 条——写入环节丢数据",
			project, written, back))
	}

	if err := tx.Commit(); err != nil {
		logx.J("network_sync", "lb_commit_fail", map[string]any{
			"account": accountID, "project": project, "err": err.Error(),
			"note": "本次 LB 与后端成员全部回滚，库里仍是上一轮数据"})
		return
	}
	committed = true
	logx.J("network_sync", "lb_synced", map[string]any{
		"account": accountID, "project": project,
		"lbs": len(lbs), "backends_resolved": resolved, "backends_written": written})
}

// lbKey 后端成员的分组键，写入端和读取端必须用同一个函数拼，
// 免得两边各写一份、某天悄悄改歪一个。
func lbKey(accountID int, project, lbName string) string {
	return itoa(accountID) + "/" + project + "/" + lbName
}

// projName 项目自定义名映射（account+project_id -> name），列表展示用。
// projNames 取 账号/项目 → 项目显示名 的映射。
// 查询失败时返回空 map（调用方会退化成显示项目 ID），但**必须留下日志**：
// 原先这里是 `rows, _ :=`，DB 一抖动整列「项目」就会变空白，页面看起来只是"没填项目名"，
// 排查时完全无从得知发生过一次查询失败。
func (h *NetworkHandler) projNames() map[string]string {
	m := map[string]string{}
	rows, err := h.DB.Query(`SELECT account_id, project_id, name FROM cloud_account_projects`)
	if err != nil {
		logx.J("cloud", "proj_names_failed", map[string]any{"err": err.Error()})
		return m
	}
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
			// high_risk 按当前规则实时重算，不直接读存列。
			// 存列是入库那一刻算的，敏感端口字典一旦扩充（比如补进 ZooKeeper 2181、
			// RocketMQ 9876），存量记录不会自动跟着变，得等下一次同步才刷新——
			// 那期间查出来的结论是错的，而且没人会意识到。判定规则应当即时生效。
			live := fwHighRisk(direction, action, protocols, src)
			out = append(out, gin.H{"provider": provider, "project": pn[itoa(aid)+"/"+project], "name": name, "network": network,
				"direction": direction, "priority": priority, "action": action, "protocols": protocols,
				"source_ranges": src, "target_tags": tags, "disabled": disabled == 1, "high_risk": live})
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
	//
	// ⚠️ 这段原来是 `brows, _ := h.DB.Query(...)`：查询失败被丢弃，backends 退化成
	//	空 map，81 个 LB **全部**显示成没有后端。而这个 JOIN 是
	//	`cis.name = b.instance`，两边都没有索引，行数一多就可能超时——
	//	一次超时就让整页数据静默消失，且界面上看起来"就是没有后端"。
	//	这正是"失败态退化成空态"的老毛病，也是 LB 后端修了几版都没修好的
	//	候选原因之一。现在错误必须现形，并且往上传成失败态。
	backends := map[string][]gin.H{}
	backendRows, scanFails := 0, 0
	brows, berr := h.DB.Query(`
		SELECT b.cloud_account_id, b.project, b.lb_name, b.instance, b.group_name, b.zone, COALESCE(h.internal_ip,'')
		FROM cloud_lb_backends b
		LEFT JOIN cis c ON c.type='host' AND c.name=b.instance
		LEFT JOIN hosts h ON h.ci_id=c.id AND h.project=b.project AND h.stale=0
		ORDER BY b.instance`)
	if berr != nil {
		logx.J("network_sync", "lb_backends_query_fail", map[string]any{"err": berr.Error()})
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "读取 LB 后端成员失败：" + berr.Error() + "（不是「没有后端」，是这次查询本身失败了）"})
		return
	}
	for brows.Next() {
		var aid int
		var project, lb, inst, grp, zone, ip string
		if err := brows.Scan(&aid, &project, &lb, &inst, &grp, &zone, &ip); err != nil {
			scanFails++
			continue
		}
		backendRows++
		key := lbKey(aid, project, lb)
		backends[key] = append(backends[key], gin.H{"instance": inst, "group": grp, "zone": zone, "internal_ip": ip})
	}
	// rows.Err() 从来没人查：迭代中途断开（超时、连接被掐）会让结果**静默截断**，
	// 剩下的 LB 全变成"没有后端"，而这和"真的没有后端"在界面上一模一样
	rowsErr := brows.Err()
	brows.Close()
	if rowsErr != nil {
		logx.J("network_sync", "lb_backends_rows_err", map[string]any{
			"err": rowsErr.Error(), "read_before_break": backendRows})
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "读取 LB 后端成员时中断：" + rowsErr.Error() + "（已读 " + itoa(backendRows) + " 行，结果不完整）"})
		return
	}
	if scanFails > 0 {
		logx.J("network_sync", "lb_backends_scan_fail", map[string]any{
			"count": scanFails, "note": "这些行被丢弃，对应 LB 会少显示后端"})
	}
	logx.J("network_sync", "lb_backends_loaded", map[string]any{
		"rows": backendRows, "lb_keys": len(backends)})

	rows, err := h.DB.Query(`SELECT id, provider, cloud_account_id, project, name, scheme, vip, port_range, protocol, target, IFNULL(backend_state,''), IFNULL(backend_count,0), region, self_link FROM cloud_loadbalancers ORDER BY provider, name`)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	defer rows.Close()
	pn := h.projNames()
	out := []gin.H{}
	for rows.Next() {
		var provider, project, name, scheme, vip, port, protocol, target, bState, region, selfLink string
		var id, aid, bCount int
		if rows.Scan(&id, &provider, &aid, &project, &name, &scheme, &vip, &port, &protocol, &target, &bState, &bCount, &region, &selfLink) == nil {
			bs := backends[lbKey(aid, project, name)]
			if bs == nil {
				bs = []gin.H{}
			}
			// ⚠️ 采集时追溯到了、读出来却没有 —— 这是**数据丢了**，不是"没有后端"。
			//
			//	把它显示成"没有后端"比修复前更误导：原来 81 个统一显示 0，
			//	人知道这块不可信；标成"追溯成功"却给不出实例，排障会以为
			//	"重新同步就有了"，而同步并不会改变结果。
			//	所以单列一个状态，并把采集时的计数摆出来供对照。
			if bState == "ok" && len(bs) == 0 && bCount > 0 {
				bState = "lost"
				// 把读取端用的 key 原样打出来。写入端在 lb_backends_written 里打的是
				// 同一个函数拼的 key——两条日志一比对，是"key 对不上"还是
				// "根本没写进去"立刻就分得清，不用再猜。
				logx.J("network_sync", "lb_backends_lost", map[string]any{
					"lookup_key": lbKey(aid, project, name), "resolved_at_sync": bCount,
					"loaded_rows_total": backendRows, "loaded_lb_keys": len(backends),
					"note": "采集时追溯到了后端，按此 key 查不到明细——比对同 key 的 lb_backends_written 日志",
				})
			}
			out = append(out, gin.H{"id": id, "provider": provider, "project": pn[itoa(aid)+"/"+project], "name": name,
				"scheme": scheme, "vip": vip, "port_range": port, "protocol": protocol, "target": target, "region": region,
				// backend_state 让界面能区分「真的没后端」和「没追溯到」。
				// 空字符串 = 这条是本次改动之前采集的，还没有状态信息。
				"self_link": selfLink, "backends": bs, "backend_state": bState,
				// 采集时追溯到的条数，用来和实际返回的 backends 对账
				"backend_count": bCount})
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
