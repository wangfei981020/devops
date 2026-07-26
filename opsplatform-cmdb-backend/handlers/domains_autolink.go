package handlers

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
)

// AutoLinkModules 从 K8s 入口自动填域名的模块(仅补空的,不覆盖手动值)。
// 规则(用户定):模块 = 匹配到的 VirtualService 名 = 后端 Service 去 -svc。优先 VS，其次 Ingress/HTTPRoute 后端。
func (h *DomainHandler) AutoLinkModules(c *gin.Context) {
	rows, err := h.DB.Query(`SELECT c.id, c.name FROM cis c JOIN domains d ON d.ci_id=c.id
		WHERE c.type='domain' AND (c.module IS NULL OR c.module='')`)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	type dom struct {
		id   int64
		name string
	}
	doms := []dom{}
	for rows.Next() {
		var d dom
		if rows.Scan(&d.id, &d.name) == nil {
			doms = append(doms, d)
		}
	}
	rows.Close()

	filled := 0
	details := []gin.H{}
	for _, d := range doms {
		mod, via := h.inferModule(d.name)
		if mod == "" {
			continue
		}
		if _, e := h.DB.Exec(`UPDATE cis SET module=? WHERE id=?`, mod, d.id); e == nil {
			filled++
			details = append(details, gin.H{"domain": d.name, "module": mod, "via": via})
		}
	}
	WriteAudit(h.DB, c, "auto_link_domain_modules", "filled="+strconv.Itoa(filled))
	c.JSON(http.StatusOK, gin.H{"ok": true, "filled": filled, "scanned": len(doms), "details": details})
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
