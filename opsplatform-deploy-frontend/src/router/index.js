import { createRouter, createWebHistory } from 'vue-router'
import AppLayout from '../components/AppLayout.vue'
import { useAuthStore } from '../stores/auth'

const routes = [
  { path: '/login', component: () => import('../views/LoginView.vue'), meta: { public: true, title: '登录' } },
  {
    path: '/',
    component: AppLayout,
    redirect: '/dashboard',
    children: [
      { path: 'dashboard', component: () => import('../views/DashboardView.vue'), meta: { title: '发布概览', menuKey: 'dashboard' } },
      { path: 'deploy', component: () => import('../views/DeployConsole.vue'), meta: { title: '部署控制台', menuKey: 'console' } },
      { path: 'projects', component: () => import('../views/ProjectsConfig.vue'), meta: { title: '项目配置', menuKey: 'projects' } },
      { path: 'history', component: () => import('../views/DeploymentHistory.vue'), meta: { title: '发布历史', menuKey: 'history' } },
      { path: 'settings', component: () => import('../views/SystemSettings.vue'), meta: { title: '系统设置', menuKey: 'settings' } },
    ],
  },
]

const router = createRouter({ history: createWebHistory(), routes })

router.beforeEach(async (to, from, next) => {
  // SSO 入口：运维平台跳转带 ?portal_token=xxx
  const portalToken = to.query.portal_token
  if (portalToken) {
    const auth = useAuthStore()
    try {
      await auth.portalAuth(portalToken)
      const query = { ...to.query }
      delete query.portal_token
      return next({ path: to.path === '/login' ? '/' : to.path, query, replace: true })
    } catch (_) {
      return next('/login')
    }
  }

  if (to.meta.public) return next()

  const auth = useAuthStore()
  if (!auth.isLoggedIn) return next('/login')

  if (to.meta.menuKey && !auth.hasMenu(to.meta.menuKey)) {
    // 找一个有权限的菜单跳过去，否则 login
    const order = ['dashboard', 'console', 'history', 'projects', 'settings']
    const fallback = order.find(k => auth.hasMenu(k))
    if (!fallback) return next('/login')
    const pathMap = { dashboard: '/dashboard', console: '/deploy', projects: '/projects', history: '/history', settings: '/settings' }
    return next(pathMap[fallback])
  }
  next()
})

export default router
