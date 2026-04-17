import { createRouter, createWebHistory } from 'vue-router'
import { useAuthStore } from '@/stores/auth'
import api from '@/api'

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
      { path: '', redirect: to => ({ path: '/dashboard', query: to.query }) },
      { path: 'dashboard', name: 'Dashboard', component: () => import('@/views/DashboardView.vue'), meta: { title: '仪表盘', menuKey: 'dashboard' } },
      { path: 'spaces', name: 'Spaces', component: () => import('@/views/SpacesView.vue'), meta: { title: '空间列表', menuKey: 'spaces' } },
      { path: 'spaces/:key', name: 'SpaceDetail', component: () => import('@/views/SpaceDetailView.vue'), meta: { title: '空间内容', menuKey: 'spaces' } },
      { path: 'content/:id', name: 'ContentDetail', component: () => import('@/views/ContentDetailView.vue'), meta: { title: '页面详情', menuKey: 'spaces' } },
      { path: 'search', name: 'Search', component: () => import('@/views/SearchView.vue'), meta: { title: '搜索', menuKey: 'search' } },
      { path: 'jira', name: 'JiraProjects', component: () => import('@/views/JiraProjectsView.vue'), meta: { title: 'Jira 项目', menuKey: 'jira' } },
      { path: 'jira/project/:key', name: 'JiraIssues', component: () => import('@/views/JiraIssuesView.vue'), meta: { title: '项目工单', menuKey: 'jira' } },
      { path: 'jira/issue/:key', name: 'JiraIssueDetail', component: () => import('@/views/JiraIssueDetailView.vue'), meta: { title: '工单详情', menuKey: 'jira' } },
      { path: 'report', name: 'Report', component: () => import('@/views/ReportView.vue'), meta: { title: '生成报告', menuKey: 'report' } },
      { path: 'alert-stats', name: 'AlertStats', component: () => import('@/views/AlertStatsView.vue'), meta: { title: '告警统计', menuKey: 'alert-stats' } },
      { path: 'maintenance-windows', name: 'MaintenanceWindows', component: () => import('@/views/MaintenanceWindowsView.vue'), meta: { title: '维护窗口', menuKey: 'alert-stats' } },
      { path: 'settings', name: 'Settings', component: () => import('@/views/SettingsView.vue'), meta: { title: '系统设置', menuKey: 'settings' } },
    ]
  },
  { path: '/:pathMatch(.*)*', redirect: to => ({ path: '/dashboard', query: to.query }) }
]

const router = createRouter({
  history: createWebHistory(),
  routes
})

// SSO cookie 检测
async function checkCookieAuth() {
  try {
    const res = await api.get('/api/users/me')
    if (res.data?.data) {
      const authStore = useAuthStore()
      authStore.setToken('cookie-auth')
      authStore.setUser(res.data.data)
      return true
    }
  } catch (e) { /* ignore */ }
  return false
}

// Portal 免登认证
async function checkPortalAuth(token) {
  try {
    const res = await api.post('/api/portal-auth', { token })
    const d = res.data?.data ?? res.data
    if (d?.token && d?.user) {
      const authStore = useAuthStore()
      authStore.setToken(d.token)
      authStore.setUser(d.user)
      if (d.permissions) {
        authStore.setPermissions(d.permissions)
      }
      return true
    }
  } catch (e) { /* ignore */ }
  return false
}

router.beforeEach(async (to, from, next) => {
  const authStore = useAuthStore()

  // Portal 免登
  if (to.query.portal_token) {
    const ok = await checkPortalAuth(to.query.portal_token)
    if (ok) {
      const q = { ...to.query }
      delete q.portal_token
      q.portal_login = '1'
      return next({ path: to.path, query: q })
    }
  }

  // SSO 登录回调
  if (to.query.sso_login === '1') {
    const ok = await checkCookieAuth()
    if (ok) {
      return next({ path: to.path, query: {} })
    }
  }

  // 不需要认证的页面
  if (to.meta.requiresAuth === false) {
    if (authStore.isLoggedIn) return next('/dashboard')
    return next()
  }

  // 需要认证
  if (!authStore.isLoggedIn) {
    const ok = await checkCookieAuth()
    if (!ok) return next({ path: '/login', query: { redirect: to.fullPath } })
  }

  // 管理员页面权限检查
  if (to.meta.requiresAdmin && !authStore.isAdmin) {
    return next('/dashboard')
  }

  // 菜单权限检查
  if (to.meta.menuKey && !authStore.hasMenu(to.meta.menuKey)) {
    return next('/dashboard')
  }

  next()
})

export default router
