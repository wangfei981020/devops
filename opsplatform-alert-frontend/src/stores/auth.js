import { defineStore } from 'pinia'
import { ref, computed } from 'vue'
import api from '../api'

export const useAuthStore = defineStore('auth', () => {
  const user = ref(JSON.parse(localStorage.getItem('alert_user') || 'null'))
  const token = ref(localStorage.getItem('alert_token') || '')
  const permissions = ref(JSON.parse(localStorage.getItem('alert_permissions') || '{}'))

  const isLoggedIn = computed(() => !!token.value)
  const isAdmin = computed(() => user.value?.role === 'admin')

  // Permission check: local users and admins bypass, portal users check permissions
  function hasPermission(code) {
    if (!user.value) return false
    if (user.value.auth_source !== 'portal') return true  // local users always allowed
    if (user.value.role === 'admin') return true           // admin bypass
    if (!permissions.value || Object.keys(permissions.value).length === 0) return false  // no perms = deny all
    return !!permissions.value[code]
  }

  function hasMenu(menuCode) {
    return hasPermission('menu:alert_' + menuCode)
  }

  function hasButton(buttonCode) {
    return hasPermission('alert:' + buttonCode)
  }

  async function login(username, password) {
    const res = await api.post('/login', { username, password })
    if (res.code === 0) {
      token.value = res.data.token
      user.value = res.data.user
      permissions.value = {}
      localStorage.setItem('alert_token', res.data.token)
      localStorage.setItem('alert_user', JSON.stringify(res.data.user))
      localStorage.setItem('alert_permissions', '{}')
    }
    return res
  }

  async function portalAuth(portalToken) {
    const res = await api.post('/portal-auth', { token: portalToken })
    if (res.code === 0) {
      token.value = res.data.token
      user.value = res.data.user
      permissions.value = res.data.permissions || {}
      localStorage.setItem('alert_token', res.data.token)
      localStorage.setItem('alert_user', JSON.stringify(res.data.user))
      localStorage.setItem('alert_permissions', JSON.stringify(res.data.permissions || {}))
    }
    return res
  }

  async function refreshPermissions() {
    if (!token.value || user.value?.auth_source !== 'portal') return
    try {
      const res = await api.get('/refresh-permissions')
      if (res.code === 0 && res.data.permissions) {
        permissions.value = res.data.permissions
        localStorage.setItem('alert_permissions', JSON.stringify(res.data.permissions))
      }
    } catch (e) { /* ignore */ }
  }

  async function logout() {
    try { await api.post('/logout') } catch (e) { /* ignore */ }
    token.value = ''
    user.value = null
    permissions.value = {}
    localStorage.removeItem('alert_token')
    localStorage.removeItem('alert_user')
    localStorage.removeItem('alert_permissions')
  }

  return { user, token, permissions, isLoggedIn, isAdmin, hasPermission, hasMenu, hasButton, login, portalAuth, refreshPermissions, logout }
})
