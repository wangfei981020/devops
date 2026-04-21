import { defineStore } from 'pinia'
import { ref, computed } from 'vue'
import http from '../api'

const LS_TOKEN = 'deploy_token'
const LS_USER = 'deploy_user'
const LS_PERMS = 'deploy_permissions'

export const useAuthStore = defineStore('auth', () => {
  const user = ref(JSON.parse(localStorage.getItem(LS_USER) || 'null'))
  const token = ref(localStorage.getItem(LS_TOKEN) || '')
  const permissions = ref(JSON.parse(localStorage.getItem(LS_PERMS) || '{}'))

  const isLoggedIn = computed(() => !!token.value)
  const isAdmin = computed(() => user.value?.role === 'admin')

  // 本地账号和 admin 放行；portal 用户查权限
  function hasPermission(code) {
    if (!user.value) return false
    if (user.value.auth_source !== 'portal') return true
    if (user.value.role === 'admin') return true
    if (!permissions.value || Object.keys(permissions.value).length === 0) return false
    return !!permissions.value[code]
  }
  function hasMenu(menuCode) {
    return hasPermission('menu:deploy_center_' + menuCode)
  }
  function hasButton(buttonCode) {
    return hasPermission('deploy_center:' + buttonCode)
  }

  async function login(username, password) {
    const data = await http.post('/login', { username, password })
    token.value = data.token
    user.value = data.user
    permissions.value = {}
    localStorage.setItem(LS_TOKEN, data.token)
    localStorage.setItem(LS_USER, JSON.stringify(data.user))
    localStorage.setItem(LS_PERMS, '{}')
    return data
  }

  async function portalAuth(portalToken) {
    const data = await http.post('/portal-auth', { token: portalToken })
    token.value = data.token
    user.value = data.user
    permissions.value = data.permissions || {}
    localStorage.setItem(LS_TOKEN, data.token)
    localStorage.setItem(LS_USER, JSON.stringify(data.user))
    localStorage.setItem(LS_PERMS, JSON.stringify(data.permissions || {}))
    return data
  }

  async function refreshPermissions() {
    if (!token.value || user.value?.auth_source !== 'portal') return
    try {
      const data = await http.get('/refresh-permissions')
      if (data?.permissions) {
        permissions.value = data.permissions
        localStorage.setItem(LS_PERMS, JSON.stringify(data.permissions))
      }
    } catch (_) {}
  }

  async function logout() {
    try { await http.post('/logout') } catch (_) {}
    token.value = ''
    user.value = null
    permissions.value = {}
    localStorage.removeItem(LS_TOKEN)
    localStorage.removeItem(LS_USER)
    localStorage.removeItem(LS_PERMS)
  }

  return { user, token, permissions, isLoggedIn, isAdmin, hasPermission, hasMenu, hasButton, login, portalAuth, refreshPermissions, logout }
})
