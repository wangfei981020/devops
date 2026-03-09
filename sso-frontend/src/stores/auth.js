import { ref, computed } from 'vue'
import { defineStore } from 'pinia'
import api from '../api'

export const useAuthStore = defineStore('auth', () => {
  const user = ref(null)
  const loading = ref(false)

  const isLoggedIn = computed(() => !!user.value)
  const isAdmin = computed(() => user.value?.is_admin === true)
  const displayName = computed(() => user.value?.display_name || user.value?.username || '')

  async function login(username, password) {
    const res = await api.post('/api/login', { username, password })
    user.value = res.data.data
    return res.data
  }

  async function logout() {
    try {
      await api.post('/api/logout')
    } catch (e) {}
    user.value = null
  }

  async function checkSession() {
    try {
      loading.value = true
      const res = await api.get('/api/session')
      user.value = res.data.data
    } catch (e) {
      user.value = null
    } finally {
      loading.value = false
    }
  }

  return { user, loading, isLoggedIn, isAdmin, displayName, login, logout, checkSession }
})
