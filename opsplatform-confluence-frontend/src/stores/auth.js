import { defineStore } from 'pinia'
import { ref, computed } from 'vue'
import api from '@/api'

export const useAuthStore = defineStore('auth', () => {
  const user = ref(JSON.parse(localStorage.getItem('currentUser') || 'null'))
  const token = ref(localStorage.getItem('token') || '')
  const permissions = ref(JSON.parse(localStorage.getItem('confluencePermissions') || '{}'))

  const isLoggedIn = computed(() => !!token.value)
  const isAdmin = computed(() => {
    const role = user.value?.role
    return role === 'admin' || role === 'super_admin'
  })
  const displayName = computed(() => user.value?.display_name || user.value?.username || '')

  function hasPermission(code) {
    if (user.value?.auth_source !== 'portal') return true
    if (!permissions.value || Object.keys(permissions.value).length === 0) return true
    return !!permissions.value[code]
  }

  function hasMenu(menuCode) {
    return hasPermission('menu:confluence_' + menuCode)
  }

  async function login(username, password) {
    const res = await api.post('/api/login', { username, password })
    const data = res.data.data
    user.value = data.user
    token.value = data.token
    localStorage.setItem('token', data.token)
    localStorage.setItem('currentUser', JSON.stringify(data.user))
    return data
  }

  async function logout() {
    try { await api.post('/api/logout') } catch (e) { /* ignore */ }
    user.value = null
    token.value = ''
    permissions.value = {}
    localStorage.removeItem('token')
    localStorage.removeItem('currentUser')
    localStorage.removeItem('confluencePermissions')
  }

  async function fetchUser() {
    try {
      const res = await api.get('/api/users/me')
      user.value = res.data.data
      localStorage.setItem('currentUser', JSON.stringify(res.data.data))
      return res.data.data
    } catch (e) {
      user.value = null
      token.value = ''
      localStorage.removeItem('token')
      localStorage.removeItem('currentUser')
      return null
    }
  }

  function setUser(u) {
    user.value = u
    localStorage.setItem('currentUser', JSON.stringify(u))
  }

  function setToken(t) {
    token.value = t
    localStorage.setItem('token', t)
  }

  function setPermissions(perms) {
    permissions.value = perms || {}
    localStorage.setItem('confluencePermissions', JSON.stringify(permissions.value))
  }

  return {
    user, token, permissions,
    isLoggedIn, isAdmin, displayName,
    hasPermission, hasMenu,
    login, logout, fetchUser,
    setUser, setToken, setPermissions
  }
})
