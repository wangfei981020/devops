import { defineStore } from 'pinia'
import { ref, computed } from 'vue'
import api from '@/api'

export const useAuthStore = defineStore('auth', () => {
  const user = ref(JSON.parse(localStorage.getItem('pc_user') || 'null'))
  const token = ref(localStorage.getItem('pc_token') || '')
  const isLoggedIn = computed(() => !!token.value)

  function setAuth(tokenVal, userVal) {
    token.value = tokenVal
    user.value = userVal
    localStorage.setItem('pc_token', tokenVal)
    localStorage.setItem('pc_user', JSON.stringify(userVal))
  }

  async function logout() {
    try {
      await api.post('/api/auth/logout')
    } catch (e) {
      // Ignore errors on logout
    }
    token.value = ''
    user.value = null
    localStorage.removeItem('pc_token')
    localStorage.removeItem('pc_user')
  }

  async function checkSession() {
    if (!token.value) return false
    try {
      const res = await api.get('/api/my/permissions?service_code=permission-center')
      return true
    } catch {
      logout()
      return false
    }
  }

  return { user, token, isLoggedIn, setAuth, logout, checkSession }
})
