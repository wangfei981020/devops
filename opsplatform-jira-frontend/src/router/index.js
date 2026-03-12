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
      { path: 'projects', name: 'Projects', component: () => import('@/views/ProjectsView.vue'), meta: { title: '项目列表', menuKey: 'projects' } },
      { path: 'issues', name: 'Issues', component: () => import('@/views/IssuesView.vue'), meta: { title: '工单列表', menuKey: 'issues' } },
      { path: 'issues/:key', name: 'IssueDetail', component: () => import('@/views/IssueDetailView.vue'), meta: { title: '工单详情', menuKey: 'issues' } },
      { path: 'stats', name: 'Stats', component: () => import('@/views/StatsView.vue'), meta: { title: '统计分析', menuKey: 'stats' } },
      { path: 'report', name: 'Report', component: () => import('@/views/ReportView.vue'), meta: { title: '项目报告', menuKey: 'report' } },
      { path: 'settings', name: 'Settings', component: () => import('@/views/SettingsView.vue'), meta: { title: '系统设置', requiresAdmin: true, menuKey: 'settings' } },
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

// Portal 免登认证（运维平台 iframe 传入 token）
async function checkPortalAuth(token) {
  try {
    const res = await api.post('/api/portal-auth', { token })
    const d = res.data?.data ?? res.data
    if (d?.token && d?.user) {
      const authStore = useAuthStore()
      authStore.setToken(d.token)
      authStore.setUser(d.user)
      // 存储从运维平台获取的权限
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

  // Portal 免登（运维平台 iframe 传入 portal_token）
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
    // 尝试 cookie 认证
    const ok = await checkCookieAuth()
    if (!ok) return next({ path: '/login', query: { redirect: to.fullPath } })
  }

  // 管理员页面权限检查
  if (to.meta.requiresAdmin && !authStore.isAdmin) {
    return next('/dashboard')
  }

  // 菜单权限检查（portal 用户受运维平台权限控制）
  if (to.meta.menuKey && !authStore.hasMenu(to.meta.menuKey)) {
    return next('/dashboard')
  }

  next()
})

export default router
