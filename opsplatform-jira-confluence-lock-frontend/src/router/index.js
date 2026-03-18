import { createRouter, createWebHistory } from 'vue-router'

const routes = [
  { path: '/', redirect: '/events' },
  { path: '/events', name: 'Events', component: () => import('@/views/EventsView.vue') },
  { path: '/triggers', name: 'Triggers', component: () => import('@/views/TriggersView.vue') },
  { path: '/permissions', name: 'Permissions', component: () => import('@/views/PermissionsView.vue') },
  { path: '/settings', name: 'Settings', component: () => import('@/views/SettingsView.vue') },
]

export default createRouter({
  history: createWebHistory(),
  routes
})
