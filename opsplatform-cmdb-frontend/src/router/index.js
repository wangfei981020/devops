import { createRouter, createWebHistory } from 'vue-router'
import { TOKEN_KEY } from '../api/http'

const routes = [
  { path: '/login', component: () => import('../views/Login.vue'), meta: { public: true } },
  { path: '/', redirect: '/overview' },
  { path: '/overview', component: () => import('../views/Overview.vue'), meta: { title: '总览' } },
  { path: '/domains', component: () => import('../views/Domains.vue'), meta: { title: '域名' } },
  { path: '/dns-records', component: () => import('../views/DnsRecords.vue'), meta: { title: 'DNS 记录' } },
  { path: '/certs', component: () => import('../views/Certs.vue'), meta: { title: '证书' } },
  { path: '/cert-inspect', component: () => import('../views/CertInspect.vue'), meta: { title: '证书巡检' } },
  { path: '/certs/:id', component: () => import('../views/CertDetail.vue'), meta: { title: '证书详情' } },
  { path: '/relations', component: () => import('../views/Relations.vue'), meta: { title: '关系图谱' } },
  { path: '/dashboard', component: () => import('../views/Dashboard.vue'), meta: { title: '展示台' } },
  { path: '/basic', component: () => import('../views/Basic.vue'), meta: { title: '基础配置' } },
  { path: '/models', component: () => import('../views/Models.vue'), meta: { title: '模型管理' } },
  { path: '/settings', component: () => import('../views/Settings.vue'), meta: { title: '设置' } },
  { path: '/cron', component: () => import('../views/Cron.vue'), meta: { title: '定时任务' } },
  { path: '/notify', component: () => import('../views/Notify.vue'), meta: { title: '通知' } },
]

const router = createRouter({ history: createWebHistory(), routes })
router.beforeEach((to) => {
  const t = localStorage.getItem(TOKEN_KEY)
  if (!to.meta.public && !t) return '/login'
  if (to.path === '/login' && t) return '/'
})

export default router
