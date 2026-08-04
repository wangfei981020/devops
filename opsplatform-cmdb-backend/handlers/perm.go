package handlers

import (
	"database/sql"
	"fmt"
	"net/http"
	"sort"
	"strings"

	"github.com/gin-gonic/gin"

	"opsplatform-cmdb-backend/logx"
)

// 接口级权限校验。
//
//	231 个路由逐个手挂中间件不现实——漏一个就是一个洞，而且新加路由必然会忘。
//	这里改成表驱动：一张 路由→权限码 的映射表 + 一个全局中间件，
//	再加一个启动自检把没覆盖到的路由打出来。
//
//	设计要点：
//	  1. 未映射的路由**拒绝**（fail-closed）。新接口忘配权限不会变成后门，
//	     而是启动日志里立刻 WARN + 该接口 403。
//	  2. 匹配用 gin 的 c.FullPath()，拿到的是注册时的路由模板
//	     （/api/domains/:ciid/dns-records），不是实际 URL，所以匹配精确、
//	     不会被路径里的用户数据干扰。
//	  3. 公共字典接口（环境/项目/集群下拉等）登录即可读。这些被几乎每个页面
//	     当下拉框用，挂菜单权限会导致"有权看主机却拉不出环境列表"的连坐。
//	     白名单是显式清单，不做前缀通配。

// permRule 一条前缀规则：该前缀下的读操作要 Read，写操作要 Write。
// Write 留空表示写操作也只需 Read（目前没有这种情况，留着是为了表达清楚）。
type permRule struct {
	Prefix string
	Read   string
	Write  string
}

// 前缀规则，按 Prefix 最长匹配。顺序无所谓，查表时会先按长度排序。
var permPrefixRules = []permRule{
	// 资产：域名 / DNS / 证书 / CDN
	{"/api/domains", "menu:cmdb_domains", "cmdb:manage_domains"},
	{"/api/renewals", "menu:cmdb_domains", ""},
	{"/api/records", "menu:cmdb_domains", "cmdb:manage_records"},
	{"/api/dns-records", "menu:cmdb_dns_records", "cmdb:manage_dns"},
	{"/api/dns-consistency", "menu:cmdb_dns_records", ""},
	{"/api/certs", "menu:cmdb_certs", "cmdb:manage_certs"},
	{"/api/cert-inspect", "menu:cmdb_cert_inspect", ""},
	{"/api/cdns", "menu:cmdb_cdn_sites", "cmdb:manage_cdn"},
	{"/api/origin-rules", "menu:cmdb_cdn_sites", "cmdb:manage_cdn"},
	{"/api/cdn/", "menu:cmdb_cdn_sites", "cmdb:sync_cdn"},

	// 云资源
	{"/api/hosts", "menu:cmdb_hosts", ""},
	{"/api/cis", "menu:cmdb_hosts", "cmdb:manage_hosts"},
	{"/api/cloud-projects", "menu:cmdb_hosts", "cmdb:manage_cloud_projects"},
	{"/api/cloud-ips", "menu:cmdb_cloud_ips", ""},
	{"/api/cloud-addresses", "menu:cmdb_cloud_ips", ""},
	{"/api/cloud-networks", "menu:cmdb_cloud_networks", ""},
	{"/api/cloud-subnets", "menu:cmdb_cloud_networks", ""},
	{"/api/cloud-firewalls", "menu:cmdb_cloud_firewalls", ""},
	{"/api/cloud-loadbalancers", "menu:cmdb_cloud_lbs", ""},
	{"/api/cloud-iam", "menu:cmdb_cloud_audit", ""},
	{"/api/cloud-dns", "menu:cmdb_cloud_audit", ""},

	// K8s
	{"/api/k8s/clusters", "menu:cmdb_k8s_clusters", "cmdb:manage_clusters"},
	{"/api/k8s/sync-state", "menu:cmdb_k8s_clusters", ""},
	{"/api/k8s/nodes", "menu:cmdb_k8s_nodes", ""},
	{"/api/k8s/node-capacity", "menu:cmdb_k8s_nodes", ""},
	{"/api/k8s/node-pools", "menu:cmdb_k8s_nodes", ""},
	{"/api/k8s/node-usage", "menu:cmdb_k8s_nodes", ""},
	{"/api/k8s/workloads", "menu:cmdb_k8s_workloads", ""},
	{"/api/k8s/manifest", "menu:cmdb_k8s_workloads", ""},
	{"/api/k8s/pods", "menu:cmdb_k8s_pods", ""},
	{"/api/k8s/pod-usage", "menu:cmdb_k8s_pods", ""},
	{"/api/k8s/services", "menu:cmdb_k8s_networking", ""},
	{"/api/k8s/ingresses", "menu:cmdb_k8s_networking", ""},
	{"/api/k8s/gateways", "menu:cmdb_k8s_networking", ""},
	{"/api/k8s/httproutes", "menu:cmdb_k8s_networking", ""},
	{"/api/k8s/virtualservices", "menu:cmdb_k8s_networking", ""},
	{"/api/k8s/expose-surface", "menu:cmdb_k8s_networking", ""},
	{"/api/k8s/pvcs", "menu:cmdb_k8s_storage", ""},
	{"/api/k8s/pvc-usage", "menu:cmdb_k8s_storage", ""},
	{"/api/k8s/hpas", "menu:cmdb_k8s_storage", ""},
	{"/api/k8s/pdbs", "menu:cmdb_k8s_storage", ""},
	{"/api/k8s/events", "menu:cmdb_k8s_events", ""},
	{"/api/k8s/changes", "menu:cmdb_k8s_events", ""},
	{"/api/k8s/health", "menu:cmdb_k8s_health", ""},
	{"/api/k8s/config-audit", "menu:cmdb_k8s_health", ""},
	{"/api/k8s/security-audit", "menu:cmdb_k8s_health", ""},
	{"/api/k8s/orphans", "menu:cmdb_k8s_health", ""},
	{"/api/k8s/topology", "menu:cmdb_k8s_topology", ""},
	{"/api/k8s/impact", "menu:cmdb_k8s_topology", ""},
	{"/api/k8s/namespaces", "menu:cmdb_k8s_ns_project", ""},
	{"/api/k8s/ns-overview", "menu:cmdb_k8s_ns_project", ""},
	{"/api/k8s/ns-projects", "menu:cmdb_k8s_ns_project", "cmdb:manage_ns_project"},
	{"/api/k8s/event-center", "menu:cmdb_event_center", ""},
	{"/api/alerts", "menu:cmdb_alerts,menu:cmdb_event_center", ""},
	{"/api/gke/", "menu:cmdb_version_upgrade", "cmdb:manage_upgrade"},

	// 实时诊断：虽是读，但绕过快照直连生产 apiserver，日志里可能有业务数据，
	// 所以读也要按钮权限，不是给了菜单就能拉日志。
	{"/api/k8s/diagnose", "cmdb:k8s_diag", ""},
	{"/api/k8s/pod-logs", "cmdb:k8s_diag", ""},
	{"/api/k8s/pod-events", "cmdb:k8s_diag", ""},

	// 观测 / 成本
	{"/api/obs/", "menu:cmdb_k8s_usage", ""},
	{"/api/devops/", "menu:cmdb_k8s_usage", ""},
	{"/api/k8s/resource-waste", "menu:cmdb_k8s_usage", ""},
	{"/api/k8s/idle-cost", "menu:cmdb_cost", ""},
	{"/api/k8s/cost", "menu:cmdb_cost", "cmdb:manage_cost_rates"},
	{"/api/cloud-compute-rates", "menu:cmdb_cost", "cmdb:manage_cost_rates"},
	{"/api/cloud-disk-rates", "menu:cmdb_cost", "cmdb:manage_cost_rates"},

	// 接入凭据：读也要菜单权限，凭据元信息（账号名/端点/关联资源）本身就是情报
	{"/api/registrars", "menu:cmdb_integrations", "cmdb:manage_integrations"},
	{"/api/cloud-accounts", "menu:cmdb_integrations", "cmdb:manage_integrations"},
	{"/api/acme-accounts", "menu:cmdb_integrations", "cmdb:manage_integrations"},
	{"/api/obs-endpoints", "menu:cmdb_integrations", "cmdb:manage_integrations"},
	{"/api/harbor/", "menu:cmdb_integrations", "cmdb:manage_integrations"},
	{"/api/sources/", "menu:cmdb_integrations", "cmdb:sync_domains"},
	{"/api/mcp/", "menu:cmdb_integrations", "cmdb:manage_mcp"},

	// 系统
	{"/api/notify-users", "menu:cmdb_notify", "cmdb:manage_notify"},
	{"/api/lark-groups", "menu:cmdb_notify", "cmdb:manage_notify"},
	{"/api/notify/", "menu:cmdb_notify", "cmdb:manage_notify"},
	{"/api/scheduled-tasks", "menu:cmdb_cron", "cmdb:manage_cron"},
	{"/api/task-runs", "menu:cmdb_task_runs", "cmdb:run_task"},
	{"/api/relations", "menu:cmdb_basic", "cmdb:manage_basic"},
	{"/api/dashboard", "menu:cmdb_overview", ""},

	// 审计：看审计要菜单权限；回滚是独立按钮权限——能改 ≠ 能把别人的改动撤掉
	{"/api/users", "menu:cmdb_users", "cmdb:manage_users"},
	{"/api/audit-logs", "menu:cmdb_audit", "cmdb:revert_change"},
	{"/api/audit-changes", "menu:cmdb_audit", "cmdb:revert_change"},
	// 对象详情页内嵌的变更历史：跟着对象走，不单独要审计菜单权限，
	// 否则"能看这个域名却看不到它改过什么"，那段历史等于没有
	{"/api/audit-history", "", ""},
}

// 精确规则：`METHOD /完整路由模板` → 权限码。
// 用来盖掉前缀规则不够细的地方——同一前缀下的写操作并不都是同一类动作。
var permExactRules = map[string]string{
	// 写 DNS 走的是域名的子路由，但它直接改云端解析，权限必须独立于「域名管理」
	"POST /api/domains/:ciid/dns-records":              "cmdb:manage_dns",
	"POST /api/domains/:ciid/dns-records/batch":        "cmdb:manage_dns",
	"POST /api/domains/:ciid/dns-records/batch-update": "cmdb:manage_dns",
	"POST /api/domains/:ciid/dns-records/batch-delete": "cmdb:manage_dns",

	// 同步刷新是"拉数据"，不该被当成"改台账"
	"POST /api/domains/sync":               "cmdb:sync_domains",
	"POST /api/domains/refresh-all":        "cmdb:sync_domains",
	"POST /api/domains/:ciid/refresh":      "cmdb:sync_domains",
	"POST /api/domains/:ciid/sync-records": "cmdb:sync_domains",
	// 批量续费和单个续费同权限（都是真金白银的域名管理动作）；
	// preview 只读不扣费，但会暴露台账内容，同样要域名菜单权限
	"POST /api/domains/renew-batch":         "cmdb:manage_domains",
	"POST /api/domains/renew-batch/preview": "menu:cmdb_domains",
	"GET /api/domains/renew-batch/:id":      "menu:cmdb_domains",
	"POST /api/cloud-accounts/:id/sync":     "cmdb:sync_cloud",
	"POST /api/cloud-projects/:pid/sync":    "cmdb:sync_cloud",
	"POST /api/k8s/clusters/:id/sync":       "cmdb:sync_k8s",
	"POST /api/cdn/accounts/:id/sync":       "cmdb:sync_cdn",

	// 签发/续签会真的调 CA（有速率限制），和"录入证书"完全两回事
	"POST /api/certs/:id/renew":     "cmdb:issue_cert",
	"POST /api/certs/:id/dns-ready": "cmdb:issue_cert",

	// 证书复检属于证书管理，虽然挂在域名/记录路径下
	"POST /api/domains/:ciid/check-all-certs": "cmdb:manage_certs",
	"POST /api/records/:id/check-cert":        "cmdb:manage_certs",
	"PUT /api/records/:id/cert-ignore":        "cmdb:manage_certs",

	// 基础配置的四张字典表：读是公共的（见白名单），写要 manage_basic
	"POST /api/environments":             "cmdb:manage_basic",
	"PUT /api/environments/:id":          "cmdb:manage_basic",
	"DELETE /api/environments/:id":       "cmdb:manage_basic",
	"POST /api/projects":                 "cmdb:manage_basic",
	"PUT /api/projects/:id":              "cmdb:manage_basic",
	"DELETE /api/projects/:id":           "cmdb:manage_basic",
	"POST /api/lifecycle-statuses":       "cmdb:manage_basic",
	"PUT /api/lifecycle-statuses/:id":    "cmdb:manage_basic",
	"DELETE /api/lifecycle-statuses/:id": "cmdb:manage_basic",
	"PUT /api/settings":                  "cmdb:manage_basic",

	// 手动跑任务 ≠ 改任务配置（值班常要补跑一次，但不该能改 cron 表达式）
	"POST /api/scheduled-tasks/:key/run": "cmdb:run_task",
	"PUT /api/scheduled-tasks/:key":      "cmdb:manage_cron",

	"POST /api/mcp/regenerate": "cmdb:manage_mcp",

	// 数据源的「同步状态」是 DNS 记录页要显示的功能信息（上次同步时间/结果），
	// 不是凭据。整条 /api/sources/ 归到接入管理的话，有 DNS 权限但没有接入管理
	// 权限的角色（如资产管理员）打开 DNS 记录页就是一片 403。
	// 「API 用量」留在接入管理，那才是配额/账号维度的信息。
	"GET /api/sources/:id/sync-status": "menu:cmdb_dns_records",

	// 注册商名录：域名页要显示"这个域名在哪家注册商"，属于字典而非凭据
	// （handler 明确不回传密钥，只给 has_cred 布尔，见 registrars.go 的 List）。
	// 不放行则有域名权限但无接入管理权限的角色（如资产管理员）打开域名页一片 403；
	// 但也不该进全局白名单——集群管理员之类完全用不到它的角色没必要看见。
	"GET /api/registrars": "menu:cmdb_domains,menu:cmdb_dns_records,menu:cmdb_integrations",
}

// 公共字典接口：登录即可读，不挂菜单权限。
//
//	这些被几乎每个页面当下拉框/元数据用。挂上菜单权限会出现
//	"有权看主机页，却因为没有基础配置权限而拉不出环境下拉框"这种连坐。
//	返回内容都是名称类元数据，集群列表里的 endpoint/凭据字段在 handler 侧已掩码。
var permPublicRead = map[string]bool{
	"GET /api/me":                  true,
	"GET /api/refresh-permissions": true,
	"POST /api/logout":             true,
	"GET /api/environments":        true,
	"GET /api/projects":            true,
	"GET /api/lifecycle-statuses":  true,
	"GET /api/ci-types":            true,
	"GET /api/settings":            true,
	"GET /api/k8s/clusters":        true,
	"GET /api/mcp/info":            true,
}

// permSortedRules 是按 Prefix 长度倒序的规则表，保证最长前缀优先命中
// （/api/k8s/cost 必须先于 /api/k8s/ 之类的短前缀被检查）。
var permSortedRules []permRule

func init() {
	permSortedRules = make([]permRule, len(permPrefixRules))
	copy(permSortedRules, permPrefixRules)
	sort.SliceStable(permSortedRules, func(i, j int) bool {
		return len(permSortedRules[i].Prefix) > len(permSortedRules[j].Prefix)
	})
}

func isWriteMethod(m string) bool {
	switch m {
	case http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodPatch:
		return true
	}
	return false
}

// resolvePerm 解析某个路由需要的权限码。
//
//	code == "" && ok  → 公共接口，登录即可
//	code != "" && ok  → 需要该权限码
//	!ok               → 没有任何规则覆盖，调用方必须拒绝
func resolvePerm(method, fullPath string) (string, bool) {
	key := method + " " + fullPath
	if permPublicRead[key] {
		return "", true
	}
	if code, hit := permExactRules[key]; hit {
		return code, true
	}
	for _, r := range permSortedRules {
		if !strings.HasPrefix(fullPath, r.Prefix) {
			continue
		}
		if isWriteMethod(method) && r.Write != "" {
			return r.Write, true
		}
		return r.Read, true
	}
	return "", false
}

// HasPerm 当前用户是否拥有某权限码。
//
//	本地账号（auth_source=local）无条件放行：它是运维平台不可用时的兜底通道，
//	本来就不该受运维平台下发的权限码约束。
//	code 允许写成 "a,b,c"（逗号分隔）表示**任一命中即可**。
//	用于那些被多个页面共用的字典接口：与其为了不误伤而放进全局白名单
//	（那样连完全无关的角色也能读），不如精确列出"哪几类页面需要它"。
func HasPerm(c *gin.Context, code string) bool {
	if IsAdmin(c) {
		return true
	}
	if code == "" {
		return true
	}
	perms := permsFromCtx(c)
	for _, one := range strings.Split(code, ",") {
		if perms[strings.TrimSpace(one)] {
			return true
		}
	}
	return false
}

// RequireButton 单个按钮权限的中间件，给需要额外收紧的路由用。
// 常规路由由 PermGuard 统一按映射表判，不必逐个挂。
func RequireButton(db *sql.DB, code string) gin.HandlerFunc {
	return func(c *gin.Context) {
		if !HasPerm(c, code) {
			denyPerm(db, c, code)
			return
		}
		c.Next()
	}
}

// PermGuard 全局接口级校验。挂在鉴权中间件之后。
func PermGuard(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		full := c.FullPath()
		if full == "" { // 404，交给 gin 自己处理
			c.Next()
			return
		}
		code, ok := resolvePerm(c.Request.Method, full)
		if !ok {
			// 走到这里说明有路由没进映射表。启动自检本该拦下，
			// 打 WARN 是为了万一（比如运行期动态注册的路由）也能被发现。
			logx.Line("perm", fmt.Sprintf("WARN 路由未配置权限，已拒绝: %s %s", c.Request.Method, full))
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "该接口未配置权限，请联系管理员"})
			return
		}
		if !HasPerm(c, code) {
			denyPerm(db, c, code)
			return
		}
		c.Set(ctxPermCode, code) // 审计里记"凭哪个权限干的"，便于事后审授权是否过宽
		c.Next()
	}
}

// denyPerm 统一的 403：记审计（谁在试探什么）+ 打日志
func denyPerm(db *sql.DB, c *gin.Context, code string) {
	logx.Line("perm", fmt.Sprintf("denied user=%s %s %s need=%s",
		UsernameFromCtx(c), c.Request.Method, c.FullPath(), code))
	if db != nil {
		// status 必须落 denied 而不是默认的 success——审计页要能筛出
		// "谁在试探什么"，被拒的记录混在成功里就等于没记
		db.Exec(`INSERT INTO audit_logs (username, action, target, ip, actor_source, method, path, perm_code, status)
			VALUES (?, 'perm.denied', ?, ?, ?, ?, ?, ?, 'denied')`,
			UsernameFromCtx(c), fmt.Sprintf("%s %s", c.Request.Method, c.FullPath()),
			c.ClientIP(), actorSourceOf(c), c.Request.Method, c.FullPath(), code)
	}
	c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
		"error": "没有操作权限", "need": code,
	})
}

// AuditPermCheck 启动自检：把没有权限规则覆盖的路由打出来。
//
//	fail-closed 意味着漏配 = 接口直接 403。与其等用户报"页面打不开"，
//	不如启动时就在日志里把清单列出来。
func AuditPermCheck(routes gin.RoutesInfo, skipPrefixes []string) {
	var missing []string
	for _, r := range routes {
		if !strings.HasPrefix(r.Path, "/api/") {
			continue
		}
		skip := false
		for _, p := range skipPrefixes {
			if strings.HasPrefix(r.Path, p) {
				skip = true
				break
			}
		}
		if skip {
			continue
		}
		if _, ok := resolvePerm(r.Method, r.Path); !ok {
			missing = append(missing, r.Method+" "+r.Path)
		}
	}
	if len(missing) == 0 {
		logx.Line("perm", fmt.Sprintf("权限映射自检通过，%d 条路由全部有规则", len(routes)))
		return
	}
	sort.Strings(missing)
	logx.Line("perm", fmt.Sprintf("WARN 有 %d 条路由未配置权限，这些接口会直接 403：", len(missing)))
	for _, m := range missing {
		logx.Line("perm", "WARN   未配置权限: "+m)
	}
}
