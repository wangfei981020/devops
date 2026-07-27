import { createRouter, createWebHistory } from 'vue-router'
import Chat from '../views/Chat.vue'
import Settings from '../views/Settings.vue'

const routes = [
  { path: '/', name: 'chat', component: Chat },
  { path: '/settings', name: 'settings', component: Settings },
]

export default createRouter({ history: createWebHistory(), routes })
