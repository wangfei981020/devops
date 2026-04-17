import { createRouter, createWebHistory } from 'vue-router'

const routes = [
  {
    path: '/',
    component: () => import('../components/Layout.vue'),
    children: [
      { path: '', redirect: '/dashboard' },
      { path: 'dashboard', name: 'Dashboard', component: () => import('../views/DashboardView.vue') },
      { path: 'projects', name: 'Projects', component: () => import('../views/ProjectsView.vue') },
      { path: 'projects/:id', name: 'ProjectDetail', component: () => import('../views/ProjectDetailView.vue') },
      { path: 'modules/create', name: 'CreateModule', component: () => import('../views/ModuleFormView.vue') },
      { path: 'modules/:id', name: 'ModuleDetail', component: () => import('../views/ModuleDetailView.vue') },
      { path: 'modules/:id/edit', name: 'EditModule', component: () => import('../views/ModuleFormView.vue') },
      { path: 'environments', name: 'Environments', component: () => import('../views/EnvironmentsView.vue') },
      { path: 'chart-templates', name: 'ChartTemplates', component: () => import('../views/ChartTemplatesView.vue') },
      { path: 'secrets', name: 'Secrets', component: () => import('../views/SecretsView.vue') },
      { path: 'contacts', name: 'Contacts', component: () => import('../views/ContactsView.vue') },
      { path: 'lark-configs', name: 'LarkConfigs', component: () => import('../views/LarkConfigsView.vue') },
      { path: 'env-templates', name: 'EnvTemplates', component: () => import('../views/EnvTemplatesView.vue') },
      { path: 'deployments', name: 'Deployments', component: () => import('../views/DeploymentsView.vue') },
      { path: 'global-config', name: 'GlobalConfig', component: () => import('../views/GlobalConfigView.vue') }
    ]
  }
]

const router = createRouter({
  history: createWebHistory(),
  routes
})

export default router
