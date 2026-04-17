import { createRouter, createWebHashHistory } from 'vue-router'

const routes = [
  { path: '/login', component: () => import('../views/LoginView.vue') },
  {
    path: '/',
    component: () => import('../layouts/MainLayout.vue'),
    redirect: '/dashboard',
    children: [
      { path: 'dashboard', component: () => import('../views/DashboardView.vue') },
      { path: 'agents', component: () => import('../views/AgentsView.vue') },
      { path: 'agent-groups', component: () => import('../views/AgentGroupsView.vue') },
      { path: 'targets', component: () => import('../views/TargetsView.vue') },
      { path: 'probe', component: () => import('../views/ProbeView.vue') },
      { path: 'results', component: () => import('../views/ResultsView.vue') },
      { path: 'versions', component: () => import('../views/VersionsView.vue') },
      { path: 'upgrades', component: () => import('../views/UpgradesView.vue') },
      { path: 'audit', component: () => import('../views/AuditView.vue') },
      { path: 'users', component: () => import('../views/UsersView.vue') }
    ]
  }
]

const router = createRouter({
  history: createWebHashHistory(),
  routes
})

router.beforeEach((to, from, next) => {
  if (to.path === '/login') return next()
  // We can't read the httpOnly cookie from JS. Use a non-sensitive marker
  // (only contains "1") to know whether the user has logged in via the cookie.
  // Real auth check happens server-side; backend returns 401 if cookie missing.
  const marker = localStorage.getItem('probe_logged_in')
  if (!marker) return next('/login')
  next()
})

export default router
