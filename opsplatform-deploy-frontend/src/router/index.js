import { createRouter, createWebHistory } from 'vue-router'
import AppLayout from '../components/AppLayout.vue'

const routes = [
  {
    path: '/',
    component: AppLayout,
    redirect: '/deploy',
    children: [
      { path: 'deploy', component: () => import('../views/DeployConsole.vue'), meta: { title: '部署控制台' } },
      { path: 'projects', component: () => import('../views/ProjectsConfig.vue'), meta: { title: '项目配置' } },
      { path: 'history', component: () => import('../views/DeploymentHistory.vue'), meta: { title: '发布历史' } },
      { path: 'settings', component: () => import('../views/SystemSettings.vue'), meta: { title: '系统设置' } },
    ],
  },
]

export default createRouter({ history: createWebHistory(), routes })
