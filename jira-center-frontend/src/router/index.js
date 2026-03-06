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
      { path: '', redirect: '/dashboard' },
      { path: 'dashboard', name: 'Dashboard', component: () => import('@/views/DashboardView.vue'), meta: { title: '仪表盘' } },
      { path: 'projects', name: 'Projects', component: () => import('@/views/ProjectsView.vue'), meta: { title: '项目列表' } },
      { path: 'issues', name: 'Issues', component: () => import('@/views/IssuesView.vue'), meta: { title: '工单列表' } },
      { path: 'issues/:key', name: 'IssueDetail', component: () => import('@/views/IssueDetailView.vue'), meta: { title: '工单详情' } },
      { path: 'stats', name: 'Stats', component: () => import('@/views/StatsView.vue'), meta: { title: '统计分析' } },
      { path: 'report', name: 'Report', component: () => import('@/views/ReportView.vue'), meta: { title: '项目报告' } },
      { path: 'settings', name: 'Settings', component: () => import('@/views/SettingsView.vue'), meta: { title: '系统设置', requiresAdmin: true } },
    ]
  },
  { path: '/:pathMatch(.*)*', redirect: '/dashboard' }
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

router.beforeEach(async (to, from, next) => {
  const authStore = useAuthStore()

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

  next()
})

export default router
