package handlers

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"

	"github.com/gin-gonic/gin"

	"opsplatform-cmdb-backend/logx"
)

// 本地账号的角色。
//
//	## 为什么会有这一份
//
//	CMDB 的权限真相在运维平台，SSO 用户每次登录实时拉。但本地账号是
//	"运维平台不可用时的兜底通道"，它拿不到那份权限——于是长期只有一档：
//	**全权限**。想开个只读号做不到，只能去运维平台走建号/配角色/绑应用三步。
//
//	所以这里给本地账号单独准备一份角色定义。边界没变（它仍然是兜底通道），
//	只是这个通道可以按角色收窄了。
//
//	## 和运维平台那份的关系
//
//	角色代号刻意保持一致（cmdb_viewer / cmdb_asset / ...），便于两边对照。
//	但**不做同步**：同步意味着 CMDB 要在运维平台不可用时还能拿到最新角色定义，
//	那正是这条兜底通道要规避的依赖。宁可两边各存一份、代号对齐。

// 角色代号。cmdb_admin 是唯一等价于"不受限"的那个。
const (
	roleAdmin   = "cmdb_admin"
	roleViewer  = "cmdb_viewer"
	roleAsset   = "cmdb_asset"
	roleCluster = "cmdb_cluster"
	roleCost    = "cmdb_cost"
)

// allMenuCodes / allButtonCodes 是 CMDB 认得的全部权限码。
//
//	⚠️ 这两个列表必须和运维平台的种子、以及前端 router 里的 ROUTE_PERM 保持一致。
//	对不齐的后果：这里给了某个码但运维平台没种，界面上就是一个永远勾不上的权限。
var allMenuCodes = []string{
	"menu:cmdb_overview",
	"menu:cmdb_hosts", "menu:cmdb_cloud_ips", "menu:cmdb_cloud_networks",
	"menu:cmdb_cloud_firewalls", "menu:cmdb_cloud_lbs", "menu:cmdb_cloud_audit",
	"menu:cmdb_domains", "menu:cmdb_dns_records", "menu:cmdb_cdn_sites",
	"menu:cmdb_certs", "menu:cmdb_cert_inspect", "menu:cmdb_relations",
	"menu:cmdb_k8s_clusters", "menu:cmdb_version_upgrade", "menu:cmdb_k8s_nodes",
	"menu:cmdb_k8s_workloads", "menu:cmdb_k8s_pods", "menu:cmdb_k8s_networking",
	"menu:cmdb_k8s_storage", "menu:cmdb_k8s_events", "menu:cmdb_k8s_health",
	"menu:cmdb_k8s_topology", "menu:cmdb_k8s_ns_project",
	"menu:cmdb_event_center", "menu:cmdb_alerts", "menu:cmdb_k8s_usage", "menu:cmdb_cost",
	"menu:cmdb_basic", "menu:cmdb_integrations", "menu:cmdb_notify",
	"menu:cmdb_cron", "menu:cmdb_task_runs", "menu:cmdb_users", "menu:cmdb_audit",
}

var allButtonCodes = []string{
	"cmdb:manage_domains", "cmdb:sync_domains", "cmdb:manage_records", "cmdb:manage_dns",
	"cmdb:manage_certs", "cmdb:issue_cert", "cmdb:manage_cdn", "cmdb:sync_cdn",
	"cmdb:manage_hosts", "cmdb:sync_cloud", "cmdb:manage_clusters", "cmdb:sync_k8s",
	"cmdb:k8s_diag", "cmdb:manage_ns_project", "cmdb:manage_upgrade",
	"cmdb:manage_basic", "cmdb:manage_integrations", "cmdb:manage_mcp",
	"cmdb:manage_notify", "cmdb:manage_cron", "cmdb:run_task",
	"cmdb:manage_users", "cmdb:view_audit", "cmdb:revert_change",
}

func withParent(codes ...[]string) []string {
	// menu:cmdb 是父权限，缺了它连门都进不去
	out := []string{"menu:cmdb"}
	for _, c := range codes {
		out = append(out, c...)
	}
	sort.Strings(out)
	return out
}

func exceptCodes(src []string, drop ...string) []string {
	skip := make(map[string]bool, len(drop))
	for _, d := range drop {
		skip[d] = true
	}
	out := make([]string, 0, len(src))
	for _, s := range src {
		if !skip[s] {
			out = append(out, s)
		}
	}
	return out
}

type localRoleSeed struct {
	Code, Name, Desc string
	Perms            []string
}

// builtinLocalRoles 与运维平台的 5 个模板一一对应（代号相同、范围相同）。
func builtinLocalRoles() []localRoleSeed {
	return []localRoleSeed{
		{roleAdmin, "CMDB 管理员", "不受权限约束，等同于运维平台不可用时的兜底账号",
			withParent(allMenuCodes, allButtonCodes)},

		{roleViewer, "CMDB 只读", "看得到全部台账但改不了任何东西；不含云成本、接入凭据与操作审计",
			withParent(exceptCodes(allMenuCodes,
				"menu:cmdb_cost", "menu:cmdb_integrations", "menu:cmdb_users", "menu:cmdb_audit"))},

		{roleAsset, "CMDB 资产管理员",
			"域名/证书/CDN 与云资源台账的日常维护。不含 DNS 写入与证书签发（那两项会直接改动线上解析和调用 CA）",
			withParent([]string{
				"menu:cmdb_overview",
				"menu:cmdb_domains", "menu:cmdb_dns_records", "menu:cmdb_cdn_sites",
				"menu:cmdb_certs", "menu:cmdb_cert_inspect",
				"menu:cmdb_hosts", "menu:cmdb_cloud_ips", "menu:cmdb_cloud_networks",
				"menu:cmdb_cloud_firewalls", "menu:cmdb_cloud_lbs", "menu:cmdb_cloud_audit",
			}, []string{
				"cmdb:manage_domains", "cmdb:sync_domains", "cmdb:manage_certs",
				"cmdb:manage_records", "cmdb:manage_cdn", "cmdb:sync_cdn",
			})},

		{roleCluster, "CMDB 集群管理员",
			"K8s 多集群查看、体检与升级计划维护。不含集群纳管（纳管需要填写集群凭据）",
			withParent([]string{
				"menu:cmdb_overview",
				"menu:cmdb_k8s_clusters", "menu:cmdb_version_upgrade", "menu:cmdb_k8s_nodes",
				"menu:cmdb_k8s_workloads", "menu:cmdb_k8s_pods", "menu:cmdb_k8s_networking",
				"menu:cmdb_k8s_storage", "menu:cmdb_k8s_events", "menu:cmdb_k8s_health",
				"menu:cmdb_k8s_topology", "menu:cmdb_k8s_ns_project",
				"menu:cmdb_event_center", "menu:cmdb_alerts", "menu:cmdb_k8s_usage", "menu:cmdb_cost",
			}, []string{
				"cmdb:sync_k8s", "cmdb:manage_ns_project", "cmdb:manage_upgrade", "cmdb:k8s_diag",
			})},

		{roleCost, "CMDB 成本分析", "只看云成本与资源使用率，不涉及任何资产明细与配置",
			withParent([]string{"menu:cmdb_overview", "menu:cmdb_cost", "menu:cmdb_k8s_usage"})},
	}
}

// SeedLocalRoles 首次启动灌一次内置角色。
//
//	⚠️ 只在角色**不存在**时插入，绝不覆盖。管理员在界面上收窄过某个角色，
//	重启把他的调整改回去、还不告诉他，是最难查的一类"配置回滚"。
//	（运维平台那边的模板也是同样的取舍。）
func SeedLocalRoles(db *sql.DB) {
	for _, r := range builtinLocalRoles() {
		b, _ := json.Marshal(r.Perms)
		res, err := db.Exec(
			`INSERT IGNORE INTO local_roles (code, name, description, permissions, is_builtin)
			 VALUES (?, ?, ?, ?, 1)`, r.Code, r.Name, r.Desc, string(b))
		if err != nil {
			logx.Line("local_roles", fmt.Sprintf("WARN 播种角色 %s 失败: %v", r.Code, err))
			continue
		}
		if n, _ := res.RowsAffected(); n > 0 {
			logx.Line("local_roles", fmt.Sprintf("已创建内置角色 %s（%d 条权限）", r.Code, len(r.Perms)))
		}
	}
}

// permsOfLocalRole 取某个角色的权限表。
//
//	返回 (perms, unrestricted)。unrestricted=true 表示这个身份不受权限码约束：
//	  - role_code 为空 —— 升级前就存在的老账号，必须保持原样全权限，
//	    否则一次升级就把人锁在门外
//	  - role_code = cmdb_admin —— 明确的管理员
func permsOfLocalRole(db *sql.DB, roleCode string) (map[string]bool, bool) {
	if roleCode == "" || roleCode == roleAdmin {
		return map[string]bool{}, true
	}
	var raw string
	if err := db.QueryRow(`SELECT permissions FROM local_roles WHERE code=?`, roleCode).Scan(&raw); err != nil {
		// 角色被删了或者代号写错了。**不能当成全放行**——那是把配置错误
		// 变成提权。给空权限，用户会看到空菜单并去找管理员，这是安全的失败方向。
		logx.Line("local_roles", fmt.Sprintf("WARN 角色 %q 不存在，按无权限处理: %v", roleCode, err))
		return map[string]bool{}, false
	}
	var list []string
	if err := json.Unmarshal([]byte(raw), &list); err != nil {
		logx.Line("local_roles", fmt.Sprintf("WARN 角色 %q 权限解析失败，按无权限处理: %v", roleCode, err))
		return map[string]bool{}, false
	}
	out := make(map[string]bool, len(list))
	for _, c := range list {
		out[c] = true
	}
	return out, false
}

// LocalRoleHandler 角色的读接口。写操作暂不开放——
// 内置 5 个角色够覆盖当前场景，先不引入"自定义角色"那一整套编辑 UI。
type LocalRoleHandler struct{ DB *sql.DB }

// List GET /api/local-roles —— 给用户管理页的角色下拉用
func (h *LocalRoleHandler) List(c *gin.Context) {
	rows, err := h.DB.Query(
		`SELECT code, name, description, permissions, is_builtin FROM local_roles ORDER BY code`)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "读取角色失败：" + err.Error()})
		return
	}
	defer rows.Close()

	type item struct {
		Code      string `json:"code"`
		Name      string `json:"name"`
		Desc      string `json:"description"`
		PermCount int    `json:"perm_count"`
		IsBuiltin bool   `json:"is_builtin"`
		// Unrestricted 让前端能把"管理员"这一档标出来——
		// 它的 perm_count 再大也不是靠权限码生效的
		Unrestricted bool `json:"unrestricted"`
	}
	out := []item{}
	for rows.Next() {
		var it item
		var raw string
		var builtin int
		if rows.Scan(&it.Code, &it.Name, &it.Desc, &raw, &builtin) != nil {
			continue
		}
		var list []string
		_ = json.Unmarshal([]byte(raw), &list)
		it.PermCount = len(list)
		it.IsBuiltin = builtin == 1
		it.Unrestricted = it.Code == roleAdmin
		out = append(out, it)
	}
	c.JSON(http.StatusOK, gin.H{"list": out})
}
