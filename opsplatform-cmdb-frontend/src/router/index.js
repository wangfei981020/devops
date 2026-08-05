import { createRouter, createWebHistory } from 'vue-router'
import { TOKEN_KEY } from '../api/http'
import { useAuthStore } from '../stores/auth'

const routes = [
  { path: '/login', component: () => import('../views/Login.vue'), meta: { public: true } },
  { path: '/', redirect: '/overview' },
  { path: '/overview', component: () => import('../views/Overview.vue'), meta: { title: '总览' } },
  { path: '/domains', component: () => import('../views/Domains.vue'), meta: { title: '域名' } },
  { path: '/dns-records', component: () => import('../views/DnsRecords.vue'), meta: { title: 'DNS 记录' } },
  { path: '/hosts', component: () => import('../views/Hosts.vue'), meta: { title: '主机' } },
  { path: '/cloud-ips', component: () => import('../views/CloudIps.vue'), meta: { title: 'IP 地址' } },
  { path: '/cloud-networks', component: () => import('../views/CloudNetworks.vue'), meta: { title: 'VPC 网络' } },
  { path: '/cloud-firewalls', component: () => import('../views/CloudFirewalls.vue'), meta: { title: '防火墙' } },
  { path: '/cloud-audit', component: () => import('../views/CloudAudit.vue'), meta: { title: '云平台审计' } },
  { path: '/cloud-lbs', component: () => import('../views/CloudLoadBalancers.vue'), meta: { title: '负载均衡' } },
  { path: '/cdn-sites', component: () => import('../views/CdnSites.vue'), meta: { title: 'CDN 站点' } },
  { path: '/certs', component: () => import('../views/Certs.vue'), meta: { title: '证书' } },
  { path: '/cert-inspect', component: () => import('../views/CertInspect.vue'), meta: { title: '到期巡检' } },
  { path: '/certs/:id', component: () => import('../views/CertDetail.vue'), meta: { title: '证书详情' } },
  { path: '/relations', component: () => import('../views/Relations.vue'), meta: { title: '关系图谱' } },
  // 展示台已删（内容是总览的子集，两个图表都无效）。重定向而不是删路由：老书签有个去处。
  { path: '/dashboard', redirect: '/overview' },
  { path: '/basic', component: () => import('../views/Basic.vue'), meta: { title: '基础配置' } },
  // 模型管理已并入基础配置的「CI 类型」卡片（原页只有一个只读表格）。
  { path: '/models', redirect: '/basic' },
  // 「设置」页内容已并入基础配置（原页整页只剩一个指标导出白名单）。
  // 路由保留为重定向而不是删掉：老书签/收藏直接删会变成空白页，而不是给人一个去处。
  { path: '/settings', redirect: '/basic' },
  { path: '/cron', component: () => import('../views/Cron.vue'), meta: { title: '定时任务' } },
  { path: '/task-runs', component: () => import('../views/TaskRuns.vue'), meta: { title: '执行记录' } },
  { path: '/notify', component: () => import('../views/Notify.vue'), meta: { title: '通知' } },
  { path: '/k8s-clusters', component: () => import('../views/K8sClusters.vue'), meta: { title: 'K8s 集群' } },
  { path: '/k8s-nodes', component: () => import('../views/K8sNodes.vue'), meta: { title: 'K8s 节点' } },
  { path: '/k8s-workloads', component: () => import('../views/K8sWorkloads.vue'), meta: { title: 'K8s 工作负载' } },
  { path: '/k8s-pods', component: () => import('../views/K8sPods.vue'), meta: { title: 'K8s Pod' } },
  { path: '/k8s-health', component: () => import('../views/K8sHealth.vue'), meta: { title: '集群体检' } },
  { path: '/k8s-usage', component: () => import('../views/K8sUsage.vue'), meta: { title: '资源使用率' } },
  { path: '/event-center', component: () => import('../views/EventCenter.vue'), meta: { title: '事件中心' } },
  { path: '/alerts', component: () => import('../views/Alerts.vue'), meta: { title: '告警' } },
  { path: '/k8s-networking', component: () => import('../views/K8sNetworking.vue'), meta: { title: 'K8s 网络' } },
  { path: '/k8s-storage', component: () => import('../views/K8sStorage.vue'), meta: { title: 'K8s 存储/伸缩' } },
  { path: '/k8s-topology', component: () => import('../views/K8sTopology.vue'), meta: { title: 'K8s 全链路' } },
  { path: '/k8s-ns-project', component: () => import('../views/K8sNsProject.vue'), meta: { title: '命名空间归属' } },
  { path: '/version-upgrade', component: () => import('../views/VersionUpgrade.vue'), meta: { title: '版本与升级' } },
  { path: '/cost', component: () => import('../views/K8sCost.vue'), meta: { title: '云成本' } },
  { path: '/k8s-events', component: () => import('../views/K8sEvents.vue'), meta: { title: 'K8s 事件' } },
  // 接入管理：注册商/云账号/CDN/数据源/ACME 五类凭据统一入口。
  // 旧路径保留并重定向到对应 tab——收藏夹和文档里的链接不会失效。
  { path: '/integrations', component: () => import('../views/Integrations.vue'), meta: { title: '接入管理' } },
  { path: '/obs-endpoints', redirect: { path: '/integrations', query: { tab: 'obs' } } },
  { path: '/mcp', redirect: { path: '/integrations', query: { tab: 'mcp' } } },
  { path: '/cloud-accounts', redirect: { path: '/integrations', query: { tab: 'cloud' } } },
  { path: '/users', component: () => import('../views/Users.vue'), meta: { title: '用户管理' } },
  { path: '/audit', component: () => import('../views/Audit.vue'), meta: { title: '操作审计' } },
  { path: '/forbidden', component: () => import('../views/Forbidden.vue'),
    meta: { title: '无权访问' } },
  // catch-all 兜底：没有它时未知路径会渲染成空白页，面包屑还默认回退到「总览」，
  // 看起来像总览页坏了而不是地址不存在（CMDB-018）。必须放在最后。
  { path: '/:pathMatch(.*)*', name: 'NotFound', component: () => import('../views/NotFound.vue'),
    meta: { title: '页面不存在' } },
]

// 路由 → 菜单权限页代号，对应运维平台的 menu:cmdb_<代号>。
//
//	集中放一张表而不是给每条路由加 meta：新增页面时这里漏一条，
//	beforeEach 会当成"未登记页面"放行（前端不拦），但后端 perm.go 仍然拦得住——
//	前端只负责别让人点进一个注定 403 的页面，安全边界始终在后端。
const ROUTE_PERM = {
  '/overview': 'overview',
  '/hosts': 'hosts',
  '/cloud-ips': 'cloud_ips',
  '/cloud-networks': 'cloud_networks',
  '/cloud-firewalls': 'cloud_firewalls',
  '/cloud-lbs': 'cloud_lbs',
  '/cloud-audit': 'cloud_audit',
  '/domains': 'domains',
  '/dns-records': 'dns_records',
  '/cdn-sites': 'cdn_sites',
  '/certs': 'certs',
  '/cert-inspect': 'cert_inspect',
  '/relations': 'relations',
  '/k8s-clusters': 'k8s_clusters',
  '/version-upgrade': 'version_upgrade',
  '/k8s-nodes': 'k8s_nodes',
  '/k8s-workloads': 'k8s_workloads',
  '/k8s-pods': 'k8s_pods',
  '/k8s-networking': 'k8s_networking',
  '/k8s-storage': 'k8s_storage',
  '/k8s-events': 'k8s_events',
  '/k8s-health': 'k8s_health',
  '/k8s-topology': 'k8s_topology',
  '/k8s-ns-project': 'k8s_ns_project',
  '/event-center': 'event_center',
  '/alerts': 'alerts',
  '/k8s-usage': 'k8s_usage',
  '/cost': 'cost',
  '/basic': 'basic',
  '/integrations': 'integrations',
  '/notify': 'notify',
  '/cron': 'cron',
  '/task-runs': 'task_runs',
  '/users': 'users',
  '/audit': 'audit',
}

// PUBLIC_ROUTES 明确不需要权限的页面。
//
//	必须是**显式白名单**，不能靠"查不到权限码就放行"来兜——见下面 permOf 的说明。
const PUBLIC_ROUTES = new Set(['/login', '/forbidden'])

// permOf 支持带参数的详情页（/certs/123 归到 certs）。
// 导出给 App.vue 过滤菜单用——路由拦截和菜单显隐必须依据同一张表，
// 否则会出现"菜单上看得见、点进去被拦"这种自相矛盾的状态。
//
//	返回 null 表示**这个路径没有登记权限码**。注意 null 的含义是"不知道"，
//	不是"不需要权限"——守卫必须把它当拒绝处理（见 beforeEach 的说明）。
export function permOf(path) {
  if (ROUTE_PERM[path]) return ROUTE_PERM[path]
  if (path.startsWith('/certs/')) return 'certs'
  return null
}

// auditRoutePerms 启动自检：有没有路由忘了登记权限码。
//
//	守卫改成白名单制之后，忘登记的后果从"全员可见"变成"全员被拦"——
//	方向是对的（安全），但对开发者一样难查：页面莫名其妙进不去。
//	所以启动时把漏网的直接打出来，开发阶段一眼就能看见。
function auditRoutePerms() {
  const missing = []
  for (const r of routes) {
    const p = r.path
    if (!p || p.startsWith('/:') || r.redirect) continue // 通配/重定向不需要权限码
    if (PUBLIC_ROUTES.has(p)) continue
    if (p.includes(':')) continue                        // 详情页由 permOf 的前缀分支覆盖
    if (!ROUTE_PERM[p]) missing.push(p)
  }
  if (missing.length) {
    console.error('[权限] 以下路由没有登记权限码，将被守卫拒绝访问：', missing,
      '\n  → 请在 router/index.js 的 ROUTE_PERM 里补上，或加进 PUBLIC_ROUTES')
  }
}
auditRoutePerms()

// firstAllowedPath 返回这个人**第一个进得去**的页面。
//
//	没有它会出两个死循环：
//	  1. `/` 固定重定向到 /overview——没有总览权限的人一登录就被弹到无权访问页
//	  2. 无权访问页的「返回总览」按钮同样弹回无权访问页，按几次都出不去
//	ROUTE_PERM 的键序就是菜单顺序，取第一个能进的即可。
//	一个都进不去才返回 /forbidden——那种情况本来就该看到"你没有任何权限"。
export function firstAllowedPath(auth) {
  for (const [path, page] of Object.entries(ROUTE_PERM)) {
    if (auth.hasMenu(page)) return path
  }
  return '/forbidden'
}

const router = createRouter({ history: createWebHistory(), routes })

router.beforeEach(async (to, from) => {
  // SSO 入口：运维平台跳转带 ?portal_token=xxx（与发布中心同一约定）
  const portalToken = to.query.portal_token
  if (portalToken) {
    const auth = useAuthStore()
    try {
      await auth.portalAuth(portalToken)
      const query = { ...to.query }
      delete query.portal_token
      return { path: to.path === '/login' ? '/' : to.path, query, replace: true }
    } catch (_) {
      return '/login'
    }
  }

  const t = localStorage.getItem(TOKEN_KEY)
  if (!to.meta.public && !t) return '/login'
  if (to.path === '/login' && t) return '/'
  if (to.meta.public || !t) return

  const auth = useAuthStore()

  // ⚠️ 必须**先**把权限对新，再做拦截判断。
  // 原来这一步放在 App.vue 的 onMounted 里，比守卫晚跑，于是菜单按新权限渲染、
  // 路由按旧快照拦截——"菜单里有这一项，点进去说无权访问"。
  // 整个页面生命周期只拉一次，失败不阻断（见 auth.ensureFresh）。
  await auth.ensureFresh()

  // ⚠️ 白名单制：**没登记权限码的路由一律拒绝**，不是放行。
  //
  //	原来写的是 `if (page && !auth.hasMenu(page))`——permOf 返回 null 时
  //	整个判断被短路掉，等于默认放行。任何新加的路由忘配权限码就自动
  //	对全员开放，而且不会有任何提示（CMDB-045）。
  //	默认分支必须是拒绝：漏配的代价从"悄悄泄露"变成"页面进不去"，
  //	后者会被立刻发现，前者可能永远发现不了。
  //	公开页面走 PUBLIC_ROUTES 显式声明。
  if (PUBLIC_ROUTES.has(to.path)) return
  // 地址根本不存在（命中 catch-all）→ 显示「页面不存在」，不是「无权访问」。
  // 这两件事对用户是完全不同的信息：一个是打错了地址，一个是要去找管理员。
  // 少了这一条，白名单制会把所有 404 说成没权限（CMDB-018 治过的那类误导）。
  if (to.name === 'NotFound') return
  const page = permOf(to.path)
  if (!page) {
    console.error(`[权限] 路由 ${to.path} 未登记权限码，已拒绝访问`)
    return { path: '/forbidden', query: { from: to.path }, replace: true }
  }
  if (!auth.hasMenu(page)) {
    // 首页是所有人的默认落点，没权限时静默换成他进得去的第一个页面——
    // 一登录就撞"无权访问"太劝退，而且那不是他主动要去的地方
    if (to.path === '/overview' && from.path === '/') {
      const alt = firstAllowedPath(auth)
      if (alt !== '/forbidden') return { path: alt, replace: true }
    }
    return { path: '/forbidden', query: { from: to.path }, replace: true }
  }
})

export default router
