import { createRouter, createWebHistory } from 'vue-router'
import { useAuthStore } from '@/stores'

const routes = [
  {
    path: '/login',
    name: 'Login',
    component: () => import('@/views/LoginView.vue'),
    meta: { requiresAuth: false }
  },
  {
    path: '/',
    component: () => import('@/components/Layout.vue'),
    children: [
      { path: '', redirect: '/welcome' },
      { path: 'welcome', name: 'Welcome', component: () => import('@/views/WelcomeView.vue'), meta: { title: '欢迎页' } },
      { path: 'users', name: 'Users', component: () => import('@/views/UsersView.vue'), meta: { title: '用户管理' } },
      { path: 'roles', name: 'Roles', component: () => import('@/views/RolesView.vue'), meta: { title: '角色管理' } },
      { path: 'permissions', name: 'Permissions', component: () => import('@/views/PermissionsView.vue'), meta: { title: '权限配置' } },
      { path: 'audit', name: 'Audit', component: () => import('@/views/AuditView.vue'), meta: { title: '审计日志' } },
      { path: 'api-manage', name: 'ApiManage', component: () => import('@/views/PlaceholderView.vue'), meta: { title: '接口管理' } },
      { path: 'schedule', name: 'Schedule', component: () => import('@/views/ScheduleView.vue'), meta: { title: '排班管理' } },
      { path: 'task-pool', name: 'TaskPool', component: () => import('@/views/PlaceholderView.vue'), meta: { title: '任务池' } },
      { path: 'incidents', name: 'Incidents', component: () => import('@/views/PlaceholderView.vue'), meta: { title: '失误记录' } },
      { path: 'duty-records', name: 'DutyRecords', component: () => import('@/views/DutyRecordsView.vue'), meta: { title: '值班记录' } },
      { path: 'table-maintenance', name: 'TableMaintenance', component: () => import('@/views/TableMaintenanceView.vue'), meta: { title: '桌台维护记录' } },
      { path: 'table-hierarchy-config', name: 'TableHierarchyConfig', component: () => import('@/views/TableHierarchyConfigView.vue'), meta: { title: '桌台配置' } },
      { path: 'table-management', name: 'TableManagement', component: () => import('@/views/TableManagementView.vue'), meta: { title: '桌台管理' } },
      { path: 'api-keys', name: 'APIKeys', component: () => import('@/views/APIKeyManageView.vue'), meta: { title: 'API Key 管理' } },
      { path: 'assets', name: 'Assets', component: () => import('@/views/PlaceholderView.vue'), meta: { title: '资产管理' } },
      { path: 'domains', name: 'Domains', component: () => import('@/views/DomainsView.vue'), meta: { title: '域名管理' } },
      { path: 'merchants', name: 'Merchants', component: () => import('@/views/MerchantsView.vue'), meta: { title: '商户管理' } },
      { path: 'network', name: 'Network', component: () => import('@/views/NetworkView.vue'), meta: { title: '网络管理' } },
      { path: 'service-config', name: 'ServiceConfig', component: () => import('@/views/ServiceConfigView.vue'), meta: { title: '服务配置' } },
      { path: 'topology', name: 'Topology', component: () => import('@/views/TopologyView.vue'), meta: { title: '网络拓扑' } },
      { path: 'metrics', name: 'Metrics', component: () => import('@/views/PlaceholderView.vue'), meta: { title: '指标监控' } },
      { path: 'alerts', name: 'Alerts', component: () => import('@/views/PlaceholderView.vue'), meta: { title: '告警管理' } },
      { path: 'alert-rules', name: 'AlertRules', component: () => import('@/views/PlaceholderView.vue'), meta: { title: '告警规则' } },
      { path: 'alert-notify', name: 'AlertNotify', component: () => import('@/views/PlaceholderView.vue'), meta: { title: '通知配置' } },
      { path: 'dashboard-screen', name: 'DashboardScreen', component: () => import('@/views/PlaceholderView.vue'), meta: { title: '大屏展示' } },
      { path: 'clusters', name: 'Clusters', component: () => import('@/views/PlaceholderView.vue'), meta: { title: '集群管理' } },
      { path: 'workloads', name: 'Workloads', component: () => import('@/views/PlaceholderView.vue'), meta: { title: '工作负载' } },
      { path: 'configmaps', name: 'ConfigMaps', component: () => import('@/views/PlaceholderView.vue'), meta: { title: '配置管理' } },
      { path: 'storage', name: 'Storage', component: () => import('@/views/PlaceholderView.vue'), meta: { title: '存储管理' } },
      { path: 'terminal', name: 'Terminal', component: () => import('@/views/PlaceholderView.vue'), meta: { title: '容器终端' } },
      { path: 'tickets', name: 'Tickets', component: () => import('@/views/PlaceholderView.vue'), meta: { title: '工单管理' } },
      { path: 'workflow', name: 'Workflow', component: () => import('@/views/PlaceholderView.vue'), meta: { title: '流程引擎' } },
      { path: 'sla', name: 'Sla', component: () => import('@/views/PlaceholderView.vue'), meta: { title: 'SLA管理' } },
      { path: 'ticket-template', name: 'TicketTemplate', component: () => import('@/views/PlaceholderView.vue'), meta: { title: '工单模板' } },
      { path: 'jobs', name: 'Jobs', component: () => import('@/views/PlaceholderView.vue'), meta: { title: '作业平台' } },
      { path: 'crontab', name: 'Crontab', component: () => import('@/views/PlaceholderView.vue'), meta: { title: '定时任务' } },
      { path: 'inspection', name: 'Inspection', component: () => import('@/views/PlaceholderView.vue'), meta: { title: '自动巡检' } },
      { path: 'self-healing', name: 'SelfHealing', component: () => import('@/views/PlaceholderView.vue'), meta: { title: '自愈策略' } },
      { path: 'anomaly', name: 'Anomaly', component: () => import('@/views/PlaceholderView.vue'), meta: { title: '异常检测' } },
      { path: 'root-cause', name: 'RootCause', component: () => import('@/views/PlaceholderView.vue'), meta: { title: '根因分析' } },
      { path: 'predict', name: 'Predict', component: () => import('@/views/PlaceholderView.vue'), meta: { title: '故障预测' } },
      { path: 'smart-alert', name: 'SmartAlert', component: () => import('@/views/PlaceholderView.vue'), meta: { title: '智能告警' } },
      { path: 'capacity', name: 'Capacity', component: () => import('@/views/PlaceholderView.vue'), meta: { title: '容量预测' } },
      { path: 'deploy', name: 'Deploy', component: () => import('@/views/PlaceholderView.vue'), meta: { title: '发布管理' } },
      { path: 'change', name: 'Change', component: () => import('@/views/PlaceholderView.vue'), meta: { title: '变更管理' } },
      { path: 'rollback', name: 'Rollback', component: () => import('@/views/PlaceholderView.vue'), meta: { title: '回滚管理' } },
      { path: 'log-query', name: 'LogQuery', component: () => import('@/views/PlaceholderView.vue'), meta: { title: '日志查询' } },
      { path: 'log-analysis', name: 'LogAnalysis', component: () => import('@/views/PlaceholderView.vue'), meta: { title: '日志分析' } },
      { path: 'log-alert', name: 'LogAlert', component: () => import('@/views/PlaceholderView.vue'), meta: { title: '日志告警' } },
      { path: 'vault', name: 'Vault', component: () => import('@/views/PlaceholderView.vue'), meta: { title: '密码库' } },
      { path: 'secrets', name: 'Secrets', component: () => import('@/views/PlaceholderView.vue'), meta: { title: '密钥管理' } },
      { path: 'certificates', name: 'Certificates', component: () => import('@/views/PlaceholderView.vue'), meta: { title: '证书管理' } },
      { path: 'datasources', name: 'Datasources', component: () => import('@/views/PlaceholderView.vue'), meta: { title: '数据源配置' } },
      { path: 'sys-params', name: 'SysParams', component: () => import('@/views/SysParamsView.vue'), meta: { title: '系统参数' } },
      { path: 'sso-config', name: 'SSOConfig', component: () => import('@/views/SSOConfigView.vue'), meta: { title: 'SSO配置' } },
      { path: 'settings', name: 'Settings', component: () => import('@/views/SettingsView.vue'), meta: { title: '系统设置' } },
      { path: 'apps-manage', name: 'AppsManage', component: () => import('@/views/AppsManageView.vue'), meta: { title: '应用管理' } },
      { path: 'app/:appKey', name: 'AppFrame', component: () => import('@/views/AppFrameView.vue'), meta: { title: '外部应用' } }
    ]
  },
  {
    path: '/:pathMatch(.*)*',
    redirect: '/welcome'
  }
]

const router = createRouter({
  history: createWebHistory(),
  routes
})

// 检查 Cookie 认证状态（用于 SSO 登录）
async function checkCookieAuth(authStore) {
  try {
    const response = await fetch('/api/users/me', {
      credentials: 'include'
    })
    if (response.ok) {
      const data = await response.json()
      // SSO 登录成功，同步用户信息到 localStorage 和 authStore
      localStorage.setItem('token', 'cookie-auth')
      localStorage.setItem('currentUser', JSON.stringify(data))
      
      // 更新 authStore 中的用户信息
      authStore.setToken('cookie-auth')
      authStore.setUser(data)
      
      // 加载用户权限
      await authStore.loadMyPermissions()
      
      console.log('[SSO] 用户信息已同步:', data.username || data.display_name)
      return true
    }
  } catch (e) {
    console.log('Cookie 认证检查失败:', e)
  }
  return false
}

// 路径到权限代码的映射
const pathToPermMap = {
  '/users': 'users',
  '/roles': 'roles',
  '/permissions': 'permissions',
  '/audit': 'audit',
  '/api-manage': 'api-manage',
  '/schedule': 'schedule',
  '/task-pool': 'task-pool',
  '/incidents': 'incidents',
  '/duty-records': 'duty',
  '/table-maintenance': 'table_maintenance',
  '/table-hierarchy-config': 'table_hierarchy_config',
  '/table-management': 'table_management',
  '/api-keys': 'api_keys',
  '/api-docs': 'api_docs',
  '/assets': 'assets',
  '/domains': 'domains',
  '/merchants': 'merchants',
  '/network': 'network',
  '/service-config': 'service-config',
  '/topology': 'topology',
  '/metrics': 'metrics',
  '/alerts': 'alerts',
  '/alert-rules': 'alert-rules',
  '/alert-notify': 'alert-notify',
  '/dashboard-screen': 'dashboard-screen',
  '/vault': 'vault',
  '/secrets': 'secrets',
  '/certificates': 'certificates',
  '/datasources': 'datasources',
  '/sys-params': 'sys-params',
  '/settings': 'settings',
  '/sso-config': 'sso-config'
}

router.beforeEach(async (to, from, next) => {
  const authStore = useAuthStore()
  const token = localStorage.getItem('token')
  
  if (to.meta.requiresAuth !== false && !token) {
    // 没有本地 token，尝试检查 Cookie 认证（SSO 场景）
    const hasCookieAuth = await checkCookieAuth(authStore)
    if (hasCookieAuth) {
      next()
    } else {
      next('/login')
    }
  } else if (to.path === '/login' && token) {
    next('/welcome')
  } else {
    // 确保权限已加载（避免刷新后才显示菜单的问题）
    if (token && to.meta.requiresAuth !== false && Object.keys(authStore.myPermissions || {}).length === 0) {
      console.log('[路由守卫] 权限未加载，正在加载...')
      await authStore.loadMyPermissions()
    }
    
    // 检查页面权限
    const permCode = pathToPermMap[to.path]
    if (permCode && !authStore.hasMenu(permCode)) {
      console.log('[路由守卫] 无权限访问:', to.path, permCode)
      next('/welcome')
      return
    }
    
    next()
  }
})

export default router
