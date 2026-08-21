package handlers

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"ops-cmdb-backend/internal/store"
)

// AutoLinkModules 按解析记录的 host 匹配 K8s 入口(VS/Ingress/HTTPRoute)自动填模块+使用中(仅补空/原为auto的,不覆盖手动)。
// 规则(用户定):模块 = 匹配到的 VirtualService 名 = 后端 Service 去 -svc。关联到入口即默认"使用中"。
func (h *DomainHandler) AutoLinkModules(c *gin.Context) {
	sc, err := h.Store.Tenant(c.Request.Context())
	if err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	rows, err := sc.Query(`SELECT id, host, COALESCE(life_status,''), COALESCE(status_source,'') FROM domain_records
		WHERE tenant_id = ? AND ignored=0 AND stale=0 AND (module='' OR module_source='auto')`)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	type rec struct {
		id     int64
		host   string
		life   string
		srcSts string
	}
	recs := []rec{}
	for rows.Next() {
		var r rec
		if rows.Scan(&r.id, &r.host, &r.life, &r.srcSts) == nil {
			recs = append(recs, r)
		}
	}
	rows.Close()

	filled := 0
	details := []gin.H{}
	for _, r := range recs {
		mod, via := h.inferModule(sc, r.host)
		if mod == "" {
			continue
		}
		// 使用中:仅当当前状态为空或原本就是自动时,才自动设/更新为"使用中",不覆盖用户手动改的状态
		setStatus := r.life == "" || r.srcSts == "auto"
		var e error
		if setStatus {
			_, e = sc.Exec(`UPDATE domain_records SET module=?, module_source='auto', life_status='使用中', status_source='auto' WHERE tenant_id = ? AND id=?`, mod, r.id)
		} else {
			_, e = sc.Exec(`UPDATE domain_records SET module=?, module_source='auto' WHERE tenant_id = ? AND id=?`, mod, r.id)
		}
		if e == nil {
			filled++
			details = append(details, gin.H{"domain": r.host, "module": mod, "via": via})
		}
	}
	SetAuditTarget(c, "filled="+strconv.Itoa(filled))
	out := gin.H{"ok": true, "filled": filled, "scanned": len(recs), "details": details}

	// 🔴 扫了但一条都没填上时，必须说清是**哪种**没填上。
	//
	//	生产上 828 条跑完得到 `filled: 0`，人无从判断是
	//	  ① 根本没有 K8s 入口数据可比对（采集没接 / 集群没纳管）
	//	  ② 有入口数据，但域名对不上（台账里的域名不在任何 VS/Ingress 上）
	//	这两种的下一步完全相反：①去接采集，②去看域名是不是配在别处。
	//	不说的话，一次"成功但什么都没做"的调用和"确实没什么可做"长得一样（OPSCMDB-079）。
	if filled == 0 && len(recs) > 0 {
		// ⚠️ 三张表分开数：租户句柄只注入**一个** tenant_id，
		//	写成一条带三个 `?` 的子查询会因参数个数不匹配而报错 ——
		//	而我第一版用 `_ =` 吞掉了那个错，ingressRows 保持 0，
		//	于是「有 2 条入口」被报成「一条都没有」。
		//	**把错误吞成 0** 正是这条问题本身的形态，我在修它时又写了一遍。
		ingressRows, cntErr := countIngressObjects(sc)
		if cntErr != nil {
			// 数不出来就说数不出来，别拿一个不确定的 0 去下结论
			out["reason_key"] = "domains:autoLinkResult.countFailed"
			out["reason"] = "一条都没填上，但也没能统计可比对的入口数量：" + cntErr.Error()
			c.JSON(http.StatusOK, out)
			return
		}
		if ingressRows == 0 {
			out["reason_key"] = "domains:autoLinkResult.noIngressData"
			out["reason"] = "没有可比对的 K8s 入口数据（VirtualService / Ingress / HTTPRoute 一条都没有）。" +
				"这不是「没有匹配」，是**没得比** —— 先确认集群已纳管且入口对象在采集范围内"
		} else {
			out["reason_key"] = "domains:autoLinkResult.noHostMatch"
			out["reason_params"] = gin.H{"routes": ingressRows}
			out["reason"] = "有 " + strconv.Itoa(ingressRows) + " 条 K8s 入口记录，但没有一条的 host 与台账里的域名对得上。" +
				"多半是这些域名根本没配在 K8s 入口上（走 CDN 直接回源、或配在别的集群）"
		}
	}
	c.JSON(http.StatusOK, out)
}

// inferModule 按域名从 VS/Ingress/HTTPRoute 反推模块名，返回(模块, 来源)。
func (h *DomainHandler) inferModule(sc *store.Scoped, domain string) (string, string) {
	// VS: 模块 = VS 名
	if rows, _ := sc.Query(`SELECT name,hosts,backends FROM k8s_virtualservices WHERE tenant_id = ? AND hosts LIKE ?`, "%"+domain+"%"); rows != nil {
		for rows.Next() {
			var name, hosts, backends string
			_ = rows.Scan(&name, &hosts, &backends)
			if hostMatch(hosts, domain) {
				rows.Close()
				if name != "" {
					return name, "VirtualService"
				}
				return firstModuleFromBackends(backends), "VirtualService"
			}
		}
		rows.Close()
	}
	// Ingress: 后端 svc 去 -svc
	if rows, _ := sc.Query(`SELECT hosts,svc_names FROM k8s_ingresses WHERE tenant_id = ? AND hosts LIKE ?`, "%"+domain+"%"); rows != nil {
		for rows.Next() {
			var hosts, svc string
			_ = rows.Scan(&hosts, &svc)
			if hostMatch(hosts, domain) {
				rows.Close()
				return firstModuleFromBackends(svc), "Ingress"
			}
		}
		rows.Close()
	}
	// HTTPRoute
	if rows, _ := sc.Query(`SELECT hostnames,backends FROM k8s_httproutes WHERE tenant_id = ? AND hostnames LIKE ?`, "%"+domain+"%"); rows != nil {
		for rows.Next() {
			var hosts, backends string
			_ = rows.Scan(&hosts, &backends)
			if hostMatch(hosts, domain) {
				rows.Close()
				return firstModuleFromBackends(backends), "HTTPRoute"
			}
		}
		rows.Close()
	}
	return "", ""
}

func firstModuleFromBackends(csv string) string {
	for _, b := range splitCSV(csv) {
		if m := moduleFromSvc(b); m != "" {
			return m
		}
	}
	return ""
}

// countIngressObjects 数三张入口表的总行数。
//
// ⚠️ 必须**分开数**：租户作用域句柄每条语句只注入一个 tenant_id，
//
//	一条 SQL 里放三个 `WHERE tenant_id = ?` 会参数个数不匹配。
func countIngressObjects(sc *store.Scoped) (int, error) {
	total := 0
	for _, tbl := range []string{"k8s_virtualservices", "k8s_ingresses", "k8s_httproutes"} {
		var n int
		if err := sc.QueryRow(`SELECT COUNT(*) FROM ` + tbl + ` WHERE tenant_id = ?`).Scan(&n); err != nil {
			return 0, err
		}
		total += n
	}
	return total, nil
}
