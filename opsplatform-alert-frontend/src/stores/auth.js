import { defineStore } from 'pinia'
import { ref, computed } from 'vue'
import api from '../api'

export const useAuthStore = defineStore('auth', () => {
  const user = ref(JSON.parse(localStorage.getItem('alert_user') || 'null'))
  const token = ref(localStorage.getItem('alert_token') || '')

  const isLoggedIn = computed(() => !!token.value)
  const isAdmin = computed(() => user.value?.role === 'admin')

  async function login(username, password) {
    const res = await api.post('/login', { username, password })
    if (res.code === 0) {
      token.value = res.data.token
      user.value = res.data.user
      localStorage.setItem('alert_token', res.data.token)
      localStorage.setItem('alert_user', JSON.stringify(res.data.user))
    }
    return res
  }

  async function portalAuth(portalToken) {
    const res = await api.post('/portal-auth', { token: portalToken })
    if (res.code === 0) {
      token.value = res.data.token
      user.value = res.data.user
      localStorage.setItem('alert_token', res.data.token)
      localStorage.setItem('alert_user', JSON.stringify(res.data.user))
    }
    return res
  }

  async function logout() {
    try { await api.post('/logout') } catch (e) { /* ignore */ }
    token.value = ''
    user.value = null
    localStorage.removeItem('alert_token')
    localStorage.removeItem('alert_user')
  }

  return { user, token, isLoggedIn, isAdmin, login, portalAuth, logout }
})
