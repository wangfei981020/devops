package handlers

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
)

// AutoLinkModules 按解析记录的 host 匹配 K8s 入口(VS/Ingress/HTTPRoute)自动填模块+使用中(仅补空/原为auto的,不覆盖手动)。
// 规则(用户定):模块 = 匹配到的 VirtualService 名 = 后端 Service 去 -svc。关联到入口即默认"使用中"。
func (h *DomainHandler) AutoLinkModules(c *gin.Context) {
	rows, err := h.DB.Query(`SELECT id, host, COALESCE(life_status,''), COALESCE(status_source,'') FROM domain_records
		WHERE ignored=0 AND stale=0 AND (module='' OR module_source='auto')`)
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
		mod, via := h.inferModule(r.host)
		if mod == "" {
			continue
		}
		// 使用中:仅当当前状态为空或原本就是自动时,才自动设/更新为"使用中",不覆盖用户手动改的状态
		setStatus := r.life == "" || r.srcSts == "auto"
		var e error
		if setStatus {
			_, e = h.DB.Exec(`UPDATE domain_records SET module=?, module_source='auto', life_status='使用中', status_source='auto' WHERE id=?`, mod, r.id)
		} else {
			_, e = h.DB.Exec(`UPDATE domain_records SET module=?, module_source='auto' WHERE id=?`, mod, r.id)
		}
		if e == nil {
			filled++
			details = append(details, gin.H{"domain": r.host, "module": mod, "via": via})
		}
	}
	SetAuditTarget(c, "filled="+strconv.Itoa(filled))
	c.JSON(http.StatusOK, gin.H{"ok": true, "filled": filled, "scanned": len(recs), "details": details})
}

// inferModule 按域名从 VS/Ingress/HTTPRoute 反推模块名，返回(模块, 来源)。
func (h *DomainHandler) inferModule(domain string) (string, string) {
	// VS: 模块 = VS 名
	if rows, _ := h.DB.Query(`SELECT name,hosts,backends FROM k8s_virtualservices WHERE hosts LIKE ?`, "%"+domain+"%"); rows != nil {
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
	if rows, _ := h.DB.Query(`SELECT hosts,svc_names FROM k8s_ingresses WHERE hosts LIKE ?`, "%"+domain+"%"); rows != nil {
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
	if rows, _ := h.DB.Query(`SELECT hostnames,backends FROM k8s_httproutes WHERE hostnames LIKE ?`, "%"+domain+"%"); rows != nil {
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
