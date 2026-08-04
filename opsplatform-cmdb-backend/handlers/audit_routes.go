package handlers

import (
	"database/sql"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

// 审计的路由映射：哪个接口动的是哪张表、主键从哪个路径参数取、能不能回滚。
//
//	有了这张表，绝大多数单对象 CRUD 的变更捕获就不需要改 handler——
//	中间件在 handler 前后各拍一次行快照，自动 diff。
//
//	没登记的写接口不会漏记操作流水（主表照写），只是没有行级 before/after，
//	因此回滚不了。批量和写外部系统的接口本来就要走显式埋点（模式 B）。

type auditRoute struct {
	Action     string // 审计动作名，<对象>.<动作>
	TargetType string
	Table      string // 空 = 动作型，不做行快照
	PKParam    string // 路径参数名，如 ":id" 取 "id"
	Op         string // INSERT/UPDATE/DELETE；空则按 HTTP 方法推断
	RevertKind string // local/external/none
}

// auditPKCols 各表的主键列名。默认 id，这里只列例外。
var auditPKCols = map[string]string{
	"cis":                    "id",
	"domains":                "ci_id",
	"settings":               "k",
	"scheduled_tasks":        "task_key",
	"k8s_ns_project":         "id",
	"cloud_account_projects": "id",
}

func auditPKCol(table string) string {
	if c, ok := auditPKCols[table]; ok {
		return c
	}
	return "id"
}

// auditRoutes：key 是 `METHOD /完整路由模板`
var auditRoutes = map[string]auditRoute{
	// ── 域名台账 ──
	"POST /api/domains":         {"domain.create", "domain", "cis", "", "INSERT", "local"},
	"PUT /api/domains/:ciid":    {"domain.update", "domain", "domains", "ciid", "UPDATE", "local"},
	"DELETE /api/domains/:ciid": {"domain.delete", "domain", "domains", "ciid", "DELETE", "local"},
	// 续费和自动续费会写回注册商，回滚要二次确认
	"POST /api/domains/:ciid/renew": {"domain.renew", "domain", "", "ciid", "", "none"},
	// 批量续费：钱已经花出去了，回滚不了；预览是只读的
	"POST /api/domains/renew-batch":         {"domain.batch_renew", "domain", "", "", "", "none"},
	"POST /api/domains/renew-batch/preview": {"domain.batch_renew_preview", "domain", "", "", "", "none"},
	"POST /api/domains/:ciid/auto-renew":    {"domain.auto_renew", "domain", "domains", "ciid", "UPDATE", "external"},

	// ── 解析记录台账 ──
	"PUT /api/records/:id":             {"record.update", "record", "domain_records", "id", "UPDATE", "local"},
	"DELETE /api/records/:id":          {"record.delete", "record", "domain_records", "id", "DELETE", "local"},
	"PUT /api/records/:id/cert-ignore": {"record.cert_ignore", "record", "domain_records", "id", "UPDATE", "local"},

	// ── DNS 记录（写云端，回滚要二次确认）──
	//
	//	⚠️ Table 故意留空 = 关掉自动快照。写回厂商成功后会 refreshDomainDNSCache，
	//	它是「DELETE 整域名的 dns_records 再重插」，**行 id 会变**。
	//	自动快照按旧 id 取 after 必然查不到，diff 会变成"所有字段被删光"——
	//	一个完全错误的结论。所以这几个接口在 handler 里显式埋点，
	//	记的是 type/name/data/ttl 这些业务字段，回滚也是照它们调 DNS API 写回去，
	//	不依赖本地行 id。
	"PUT /api/dns-records/:id":    {"dns_record.update", "dns_record", "", "id", "UPDATE", "external"},
	"DELETE /api/dns-records/:id": {"dns_record.delete", "dns_record", "", "id", "DELETE", "external"},

	// ── 证书 ──
	"POST /api/certs":               {"cert.create", "cert", "certificates", "", "INSERT", "local"},
	"DELETE /api/certs/:id":         {"cert.delete", "cert", "certificates", "id", "DELETE", "local"},
	"POST /api/certs/:id/renew":     {"cert.renew", "cert", "", "id", "", "none"},
	"POST /api/certs/:id/dns-ready": {"cert.dns_ready", "cert", "", "id", "", "none"},

	// ── CDN ──
	"POST /api/cdns":               {"cdn.create", "cdn", "cdns", "", "INSERT", "local"},
	"PUT /api/cdns/:id":            {"cdn.update", "cdn", "cdns", "id", "UPDATE", "local"},
	"DELETE /api/cdns/:id":         {"cdn.delete", "cdn", "cdns", "id", "DELETE", "local"},
	"POST /api/origin-rules":       {"origin_rule.upsert", "origin_rule", "origin_ip_rules", "", "INSERT", "local"},
	"DELETE /api/origin-rules/:id": {"origin_rule.delete", "origin_rule", "origin_ip_rules", "id", "DELETE", "local"},

	// ── 配置项 / 主机 ──
	"POST /api/cis":           {"ci.create", "ci", "cis", "", "INSERT", "local"},
	"PUT /api/cis/:id":        {"ci.update", "ci", "cis", "id", "UPDATE", "local"},
	"DELETE /api/cis/:id":     {"ci.delete", "ci", "cis", "id", "DELETE", "local"},
	"PUT /api/cis/:id/labels": {"ci.update_labels", "ci", "cis", "id", "UPDATE", "local"},

	// ── 云账号 / 项目（凭据类）──
	"POST /api/cloud-accounts":        {"cloud_account.create", "cloud_account", "cloud_accounts", "", "INSERT", "local"},
	"PUT /api/cloud-accounts/:id":     {"cloud_account.update", "cloud_account", "cloud_accounts", "id", "UPDATE", "local"},
	"DELETE /api/cloud-accounts/:id":  {"cloud_account.delete", "cloud_account", "cloud_accounts", "id", "DELETE", "local"},
	"PUT /api/cloud-projects/:pid":    {"cloud_project.update", "cloud_project", "cloud_account_projects", "pid", "UPDATE", "local"},
	"DELETE /api/cloud-projects/:pid": {"cloud_project.delete", "cloud_project", "cloud_account_projects", "pid", "DELETE", "local"},

	// ── K8s 集群纳管 ──
	"POST /api/k8s/clusters":       {"cluster.create", "cluster", "k8s_clusters", "", "INSERT", "local"},
	"PUT /api/k8s/clusters/:id":    {"cluster.update", "cluster", "k8s_clusters", "id", "UPDATE", "local"},
	"DELETE /api/k8s/clusters/:id": {"cluster.delete", "cluster", "k8s_clusters", "id", "DELETE", "local"},

	// ── 接入凭据 ──
	"POST /api/registrars":              {"registrar.create", "registrar", "registrars", "", "INSERT", "local"},
	"PUT /api/registrars/:id":           {"registrar.update", "registrar", "registrars", "id", "UPDATE", "local"},
	"DELETE /api/registrars/:id":        {"registrar.delete", "registrar", "registrars", "id", "DELETE", "local"},
	"POST /api/acme-accounts":           {"acme_account.create", "acme_account", "acme_accounts", "", "INSERT", "local"},
	"DELETE /api/acme-accounts/:id":     {"acme_account.delete", "acme_account", "acme_accounts", "id", "DELETE", "local"},
	"POST /api/obs-endpoints":           {"obs_endpoint.create", "obs_endpoint", "obs_endpoints", "", "INSERT", "local"},
	"PUT /api/obs-endpoints/:id":        {"obs_endpoint.update", "obs_endpoint", "obs_endpoints", "id", "UPDATE", "local"},
	"DELETE /api/obs-endpoints/:id":     {"obs_endpoint.delete", "obs_endpoint", "obs_endpoints", "id", "DELETE", "local"},
	"POST /api/harbor/registries":       {"harbor.create", "harbor", "harbor_registries", "", "INSERT", "local"},
	"PUT /api/harbor/registries/:id":    {"harbor.update", "harbor", "harbor_registries", "id", "UPDATE", "local"},
	"DELETE /api/harbor/registries/:id": {"harbor.delete", "harbor", "harbor_registries", "id", "DELETE", "local"},
	"POST /api/cdn/accounts":            {"cdn_account.create", "cdn_account", "cdn_accounts", "", "INSERT", "local"},
	"DELETE /api/cdn/accounts/:id":      {"cdn_account.delete", "cdn_account", "cdn_accounts", "id", "DELETE", "local"},
	// token 一旦重置旧的立刻作废，找不回来
	"POST /api/mcp/regenerate": {"mcp.regenerate_token", "mcp", "", "", "", "none"},

	// ── 基础配置 ──
	"POST /api/environments":             {"environment.create", "environment", "environments", "", "INSERT", "local"},
	"PUT /api/environments/:id":          {"environment.update", "environment", "environments", "id", "UPDATE", "local"},
	"DELETE /api/environments/:id":       {"environment.delete", "environment", "environments", "id", "DELETE", "local"},
	"POST /api/projects":                 {"project.create", "project", "projects", "", "INSERT", "local"},
	"PUT /api/projects/:id":              {"project.update", "project", "projects", "id", "UPDATE", "local"},
	"DELETE /api/projects/:id":           {"project.delete", "project", "projects", "id", "DELETE", "local"},
	"POST /api/lifecycle-statuses":       {"lifecycle_status.create", "lifecycle_status", "lifecycle_statuses", "", "INSERT", "local"},
	"PUT /api/lifecycle-statuses/:id":    {"lifecycle_status.update", "lifecycle_status", "lifecycle_statuses", "id", "UPDATE", "local"},
	"DELETE /api/lifecycle-statuses/:id": {"lifecycle_status.delete", "lifecycle_status", "lifecycle_statuses", "id", "DELETE", "local"},
	"PUT /api/settings":                  {"settings.update", "settings", "", "", "", "local"},
	"POST /api/relations":                {"relation.create", "relation", "ci_relations", "", "INSERT", "local"},
	"DELETE /api/relations/:id":          {"relation.delete", "relation", "ci_relations", "id", "DELETE", "local"},

	// ── 通知 ──
	"POST /api/notify-users":         {"notify_user.create", "notify_user", "notify_users", "", "INSERT", "local"},
	"DELETE /api/notify-users/:id":   {"notify_user.delete", "notify_user", "notify_users", "id", "DELETE", "local"},
	"POST /api/lark-groups":          {"lark_group.create", "lark_group", "lark_groups", "", "INSERT", "local"},
	"PUT /api/lark-groups/:id":       {"lark_group.update", "lark_group", "lark_groups", "id", "UPDATE", "local"},
	"DELETE /api/lark-groups/:id":    {"lark_group.delete", "lark_group", "lark_groups", "id", "DELETE", "local"},
	"POST /api/notify/test":          {"notify.test", "notify", "", "", "", "none"},
	"POST /api/lark-groups/:id/test": {"lark_group.test", "lark_group", "", "id", "", "none"},

	// ── 定时任务 ──
	"PUT /api/scheduled-tasks/:key":          {"cron.update", "cron", "scheduled_tasks", "key", "UPDATE", "local"},
	"POST /api/scheduled-tasks/:key/run":     {"cron.run", "cron", "", "key", "", "none"},
	"POST /api/task-runs/:id/cancel":         {"task_run.cancel", "task_run", "", "id", "", "none"},
	"POST /api/task-runs/:id/retry-failures": {"task_run.retry", "task_run", "", "id", "", "none"},

	// ── 成本 ──
	"POST /api/cloud-compute-rates":       {"compute_rate.create", "compute_rate", "cloud_compute_rates", "", "INSERT", "local"},
	"PUT /api/cloud-compute-rates/:id":    {"compute_rate.update", "compute_rate", "cloud_compute_rates", "id", "UPDATE", "local"},
	"DELETE /api/cloud-compute-rates/:id": {"compute_rate.delete", "compute_rate", "cloud_compute_rates", "id", "DELETE", "local"},
	"POST /api/cloud-disk-rates":          {"disk_rate.create", "disk_rate", "cloud_disk_rates", "", "INSERT", "local"},
	"PUT /api/cloud-disk-rates/:id":       {"disk_rate.update", "disk_rate", "cloud_disk_rates", "id", "UPDATE", "local"},
	"DELETE /api/cloud-disk-rates/:id":    {"disk_rate.delete", "disk_rate", "cloud_disk_rates", "id", "DELETE", "local"},
	"POST /api/k8s/cost/node-override":    {"cost.node_override", "cost", "", "", "", "local"},
	"POST /api/k8s/cost/snapshot":         {"cost.snapshot", "cost", "", "", "", "none"},

	// ── GKE 升级 ──
	"POST /api/gke/upgrade/baseline":                {"gke.set_baseline", "gke", "", "", "", "none"},
	"PUT /api/gke/version-schedule/:id":             {"gke.override_schedule", "gke", "gke_version_schedule", "id", "UPDATE", "local"},
	"DELETE /api/gke/version-schedule/:id/override": {"gke.clear_override", "gke", "gke_version_schedule", "id", "UPDATE", "local"},

	// ── 同步类：只记动作，不记行变更（数据差异由 task_runs 承载）──
	"POST /api/domains/sync":               {"domain.sync", "domain", "", "", "", "none"},
	"POST /api/domains/refresh-all":        {"domain.refresh_all", "domain", "", "", "", "none"},
	"POST /api/domains/:ciid/refresh":      {"domain.refresh", "domain", "", "ciid", "", "none"},
	"POST /api/domains/:ciid/sync-records": {"domain.sync_records", "domain", "", "ciid", "", "none"},
	"POST /api/sources/:id/sync":           {"source.sync", "source", "", "id", "", "none"},
	"POST /api/cloud-accounts/:id/sync":    {"cloud_account.sync", "cloud_account", "", "id", "", "none"},
	"POST /api/cloud-projects/:pid/sync":   {"cloud_project.sync", "cloud_project", "", "pid", "", "none"},
	"POST /api/k8s/clusters/:id/sync":      {"cluster.sync", "cluster", "", "id", "", "none"},
	"POST /api/k8s/clusters/:id/test":      {"cluster.test", "cluster", "", "id", "", "none"},
	"POST /api/k8s/clusters/discover":      {"cluster.discover", "cluster", "", "", "", "none"},
	"POST /api/cdn/accounts/:id/sync":      {"cdn_account.sync", "cdn_account", "", "id", "", "none"},
	"POST /api/cdn/accounts/:id/verify":    {"cdn_account.verify", "cdn_account", "", "id", "", "none"},
	"POST /api/obs-endpoints/:id/test":     {"obs_endpoint.test", "obs_endpoint", "", "id", "", "none"},
	"POST /api/harbor/registries/:id/test": {"harbor.test", "harbor", "", "id", "", "none"},

	// ── 批量类：走显式埋点（模式 B），这里只登记动作名 ──
	"POST /api/domains/:ciid/dns-records":              {"dns_record.create", "dns_record", "", "ciid", "", "external"},
	"POST /api/domains/:ciid/dns-records/batch":        {"dns_record.batch_create", "dns_record", "", "ciid", "", "external"},
	"POST /api/domains/:ciid/dns-records/batch-update": {"dns_record.batch_update", "dns_record", "", "ciid", "", "external"},
	"POST /api/domains/:ciid/dns-records/batch-delete": {"dns_record.batch_delete", "dns_record", "", "ciid", "", "external"},
	"POST /api/domains/bulk-status":                    {"domain.bulk_status", "domain", "", "", "", "local"},
	"POST /api/domains/bulk-ignore":                    {"domain.bulk_ignore", "domain", "", "", "", "local"},
	"POST /api/domains/auto-link-modules":              {"domain.auto_link", "domain", "", "", "", "local"},
	"POST /api/domains/:ciid/records":                  {"record.create", "record", "", "ciid", "", "local"},
	"POST /api/records/bulk-update":                    {"record.bulk_update", "record", "", "", "", "local"},
	"POST /api/records/bulk-ignore":                    {"record.bulk_ignore", "record", "", "", "", "local"},
	"POST /api/records/:id/check-cert":                 {"record.check_cert", "record", "", "id", "", "none"},
	"POST /api/domains/:ciid/check-all-certs":          {"domain.check_all_certs", "domain", "", "ciid", "", "none"},
	"POST /api/k8s/ns-projects":                        {"ns_project.upsert", "ns_project", "", "", "", "local"},
	"POST /api/cloud-accounts/:id/projects":            {"cloud_project.create", "cloud_project", "cloud_account_projects", "", "INSERT", "local"},
}

// AuditMiddleware 自动变更捕获（模式 A）+ 操作流水（所有写请求）。
//
//	挂在 PermGuard 之后：被权限挡下的请求由 denyPerm 单独记 denied，
//	不该在这里再记一条 success。
func AuditMiddleware(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		if !isWriteMethod(c.Request.Method) {
			c.Next()
			return
		}
		full := c.FullPath()
		// 审计接口自己写审计（回滚要记"回滚的回滚"所需的快照），
		// 再被通用中间件记一条 unmapped 就是重复账
		if full == "" || strings.HasPrefix(full, "/api/audit-") {
			c.Next()
			return
		}
		key := c.Request.Method + " " + full
		rt, known := auditRoutes[key]

		rec := &Recorder{db: db, c: c, started: time.Now()}
		c.Set(ctxRecorder, rec)

		// 模式 A：改动前拍 before
		var pk string
		if known && rt.Table != "" {
			if rt.PKParam != "" {
				pk = c.Param(rt.PKParam)
			}
			if pk != "" {
				rec.autoTable, rec.autoPK, rec.autoOp = rt.Table, pk, rt.Op
				rec.autoBefore = snapshotRow(db, rt.Table, auditPKCol(rt.Table), pk)
			}
		}

		c.Next()

		// 4xx/5xx 也记——失败的尝试同样是要审计的事实
		status := "success"
		if c.Writer.Status() >= http.StatusBadRequest {
			status = "fail"
		}

		// 模式 A：改动后拍 after 并入账（显式埋点已经自己 add 过了，这里不重复）
		if rec.autoTable != "" && len(rec.changes) == 0 && status == "success" {
			after := snapshotRow(db, rec.autoTable, auditPKCol(rec.autoTable), rec.autoPK)
			op := rec.autoOp
			if op == "" {
				op = opFromMethod(c.Request.Method)
			}
			rec.add(rec.autoTable, rec.autoPK, op, rec.autoBefore, after, rt.RevertKind)
		}

		action := rt.Action
		if !known {
			// 没登记的写接口：流水照记，动作名从路径推导，便于事后补登记
			action = "unmapped." + strings.TrimPrefix(strings.ReplaceAll(full, "/", "."), ".api.")
		}
		permCode, _ := c.Get(ctxPermCode)
		pcStr, _ := permCode.(string)

		rec.flush(auditEntry{
			Action:     action,
			TargetType: rt.TargetType,
			TargetID:   pk,
			TargetName: auditTargetName(c, rec, pk),
			Status:     status,
			ErrorMsg:   c.Errors.String(),
			PermCode:   pcStr,
		})
	}
}

func opFromMethod(m string) string {
	switch m {
	case http.MethodPost:
		return "INSERT"
	case http.MethodDelete:
		return "DELETE"
	default:
		return "UPDATE"
	}
}

// auditTargetName 给审计列表一个人看得懂的对象名：
// 优先用变更快照里的 name/domain 之类字段，退化到主键。
func auditTargetName(c *gin.Context, rec *Recorder, pk string) string {
	// handler 显式给的描述优先——批量操作("删除 5 条")没有单一对象可取名
	if s := auditTargetOverride(c); s != "" {
		return s
	}
	for _, ch := range rec.changes {
		row := ch.After
		if row == nil {
			row = ch.Before
		}
		for _, k := range []string{"name", "domain", "cn", "host", "record_name", "title", "k"} {
			if v, ok := row[k]; ok {
				if s := strings.TrimSpace(toStr(v)); s != "" {
					return s
				}
			}
		}
	}
	return pk
}

func toStr(v interface{}) string {
	if v == nil {
		return ""
	}
	switch x := v.(type) {
	case string:
		return x
	case []byte:
		return string(x)
	}
	return ""
}

// AuditRouteCheck 启动自检：写接口有没有登记到 auditRoutes。
// 没登记不影响安全（流水照记），但那条变更回滚不了，值得在日志里点名。
func AuditRouteCheck(routes gin.RoutesInfo) {
	var missing []string
	for _, r := range routes {
		if !strings.HasPrefix(r.Path, "/api/") || !isWriteMethod(r.Method) {
			continue
		}
		// 回滚接口自己写审计（含"回滚的回滚"所需的快照），不走通用捕获
		if r.Path == "/api/login" || r.Path == "/api/portal-auth" ||
			r.Path == "/api/logout" || strings.HasPrefix(r.Path, "/api/mcp") ||
			strings.HasPrefix(r.Path, "/api/audit-") {
			continue
		}
		if _, ok := auditRoutes[r.Method+" "+r.Path]; !ok {
			missing = append(missing, r.Method+" "+r.Path)
		}
	}
	if len(missing) == 0 {
		logCount := len(auditRoutes)
		logxLine("audit", "变更捕获自检通过，%d 条写接口全部登记", logCount)
		return
	}
	sort.Strings(missing)
	logxLine("audit", "WARN 有 %d 条写接口未登记变更捕获（操作仍会记流水，但无法回滚）：", len(missing))
	for _, m := range missing {
		logxLine("audit", "WARN   未登记: %s", m)
	}
}
