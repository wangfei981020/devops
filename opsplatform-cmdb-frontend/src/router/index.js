import { createRouter, createWebHistory } from 'vue-router'
import { TOKEN_KEY } from '../api/http'

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
  { path: '/cloud-lbs', component: () => import('../views/CloudLoadBalancers.vue'), meta: { title: '负载均衡' } },
  { path: '/certs', component: () => import('../views/Certs.vue'), meta: { title: '证书' } },
  { path: '/cert-inspect', component: () => import('../views/CertInspect.vue'), meta: { title: '到期巡检' } },
  { path: '/certs/:id', component: () => import('../views/CertDetail.vue'), meta: { title: '证书详情' } },
  { path: '/relations', component: () => import('../views/Relations.vue'), meta: { title: '关系图谱' } },
  { path: '/dashboard', component: () => import('../views/Dashboard.vue'), meta: { title: '展示台' } },
  { path: '/basic', component: () => import('../views/Basic.vue'), meta: { title: '基础配置' } },
  { path: '/models', component: () => import('../views/Models.vue'), meta: { title: '模型管理' } },
  { path: '/settings', component: () => import('../views/Settings.vue'), meta: { title: '设置' } },
  { path: '/cron', component: () => import('../views/Cron.vue'), meta: { title: '定时任务' } },
  { path: '/task-runs', component: () => import('../views/TaskRuns.vue'), meta: { title: '执行记录' } },
  { path: '/notify', component: () => import('../views/Notify.vue'), meta: { title: '通知' } },
  { path: '/k8s-clusters', component: () => import('../views/K8sClusters.vue'), meta: { title: 'K8s 集群' } },
  { path: '/k8s-nodes', component: () => import('../views/K8sNodes.vue'), meta: { title: 'K8s 节点' } },
  { path: '/k8s-workloads', component: () => import('../views/K8sWorkloads.vue'), meta: { title: 'K8s 工作负载' } },
  { path: '/k8s-pods', component: () => import('../views/K8sPods.vue'), meta: { title: 'K8s Pod' } },
  { path: '/k8s-networking', component: () => import('../views/K8sNetworking.vue'), meta: { title: 'K8s 网络' } },
  { path: '/k8s-storage', component: () => import('../views/K8sStorage.vue'), meta: { title: 'K8s 存储/伸缩' } },
  { path: '/k8s-topology', component: () => import('../views/K8sTopology.vue'), meta: { title: 'K8s 全链路' } },
  { path: '/k8s-ns-project', component: () => import('../views/K8sNsProject.vue'), meta: { title: '命名空间归属' } },
  { path: '/cost', component: () => import('../views/K8sCost.vue'), meta: { title: '云成本' } },
  { path: '/k8s-events', component: () => import('../views/K8sEvents.vue'), meta: { title: 'K8s 事件' } },
  { path: '/obs-endpoints', component: () => import('../views/ObsEndpoints.vue'), meta: { title: '数据源接入' } },
  { path: '/mcp', component: () => import('../views/McpAccess.vue'), meta: { title: 'AI 接入' } },
]

const router = createRouter({ history: createWebHistory(), routes })
router.beforeEach((to) => {
  const t = localStorage.getItem(TOKEN_KEY)
  if (!to.meta.public && !t) return '/login'
  if (to.path === '/login' && t) return '/'
})

export default router
