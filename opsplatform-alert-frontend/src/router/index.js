import { createRouter, createWebHistory } from 'vue-router'
import { useAuthStore } from '../stores/auth'

const routes = [
  { path: '/login', name: 'Login', component: () => import('../views/LoginView.vue'), meta: { public: true } },
  {
    path: '/',
    component: () => import('../components/Layout.vue'),
    children: [
      { path: '', redirect: '/dashboard' },
      { path: 'dashboard', name: 'Dashboard', component: () => import('../views/DashboardView.vue'), meta: { menuKey: 'dashboard' } },
      { path: 'alert-rules', name: 'AlertRules', component: () => import('../views/AlertRulesView.vue'), meta: { menuKey: 'rules' } },
      { path: 'alert-rules/create', name: 'CreateAlertRule', component: () => import('../views/AlertRuleFormView.vue'), meta: { menuKey: 'rules' } },
      { path: 'alert-rules/:id/edit', name: 'EditAlertRule', component: () => import('../views/AlertRuleFormView.vue'), meta: { menuKey: 'rules' } },
      { path: 'es-explore', name: 'ESExplore', component: () => import('../views/ESExploreView.vue'), meta: { menuKey: 'explore' } },
      { path: 'es-connections', name: 'ESConnections', component: () => import('../views/ESConnectionsView.vue'), meta: { menuKey: 'connections' } },
      { path: 'loki-connections', name: 'LokiConnections', component: () => import('../views/LokiConnectionsView.vue'), meta: { menuKey: 'connections' } },
      { path: 'lark-configs', name: 'LarkConfigs', component: () => import('../views/LarkConfigsView.vue'), meta: { menuKey: 'lark' } },
      { path: 'alert-logs', name: 'AlertLogs', component: () => import('../views/AlertLogsView.vue'), meta: { menuKey: 'logs' } },
      { path: 'mutes', name: 'Mutes', component: () => import('../views/MutesView.vue'), meta: { menuKey: 'rules' } },
      { path: 'contacts', name: 'Contacts', component: () => import('../views/ContactsView.vue'), meta: { menuKey: 'contacts' } },
      { path: 'audit-logs', name: 'AuditLogs', component: () => import('../views/AuditLogsView.vue'), meta: { menuKey: 'audit' } },
      { path: 'users', name: 'Users', component: () => import('../views/UsersView.vue'), meta: { menuKey: 'users' } },
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

  // Menu permission check
  if (to.meta.menuKey && !auth.hasMenu(to.meta.menuKey)) {
    return next('/dashboard')
  }

  next()
})

export default router
