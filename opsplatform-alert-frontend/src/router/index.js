import { createRouter, createWebHistory } from 'vue-router'
import { useAuthStore } from '../stores/auth'

const routes = [
  { path: '/login', name: 'Login', component: () => import('../views/LoginView.vue'), meta: { public: true } },
  {
    path: '/',
    component: () => import('../components/Layout.vue'),
    children: [
      { path: '', redirect: '/dashboard' },
      { path: 'dashboard', name: 'Dashboard', component: () => import('../views/DashboardView.vue') },
      { path: 'alert-rules', name: 'AlertRules', component: () => import('../views/AlertRulesView.vue') },
      { path: 'alert-rules/create', name: 'CreateAlertRule', component: () => import('../views/AlertRuleFormView.vue') },
      { path: 'alert-rules/:id/edit', name: 'EditAlertRule', component: () => import('../views/AlertRuleFormView.vue') },
      { path: 'es-explore', name: 'ESExplore', component: () => import('../views/ESExploreView.vue') },
      { path: 'es-connections', name: 'ESConnections', component: () => import('../views/ESConnectionsView.vue') },
      { path: 'loki-connections', name: 'LokiConnections', component: () => import('../views/LokiConnectionsView.vue') },
      { path: 'lark-configs', name: 'LarkConfigs', component: () => import('../views/LarkConfigsView.vue') },
      { path: 'alert-logs', name: 'AlertLogs', component: () => import('../views/AlertLogsView.vue') },
      { path: 'users', name: 'Users', component: () => import('../views/UsersView.vue') },
    ]
  }
]

const router = createRouter({
  history: createWebHistory(),
  routes
})

router.beforeEach((to, from, next) => {
  // Handle portal auth
  const portalToken = to.query.portal_token
  if (portalToken) {
    const auth = useAuthStore()
    auth.portalAuth(portalToken).then(() => {
      const query = { ...to.query }
      delete query.portal_token
      next({ path: to.path, query })
    }).catch(() => next('/login'))
    return
  }

  if (to.meta.public) return next()

  const auth = useAuthStore()
  if (!auth.isLoggedIn) return next('/login')
  next()
})

export default router
