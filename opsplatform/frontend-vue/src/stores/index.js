import { defineStore } from 'pinia'
import { ref, computed } from 'vue'
import api from '@/api'

export const useAuthStore = defineStore('auth', () => {
  // 从 localStorage 初始化用户信息
  const cachedUser = localStorage.getItem('currentUser')
  let initialUser = null
  if (cachedUser) {
    try {
      initialUser = JSON.parse(cachedUser)
    } catch (e) {}
  }
  const user = ref(initialUser)
  const token = ref(localStorage.getItem('token') || '')
  const mfaPending = ref(false)
  const mfaPendingUserId = ref('')
  const mfaNeedBinding = ref(false)
  const myPermissions = ref({})
  const myMenus = ref([])
  const myRoles = ref([])

  const isLoggedIn = computed(() => !!token.value)

  async function login(username, password) {
    const res = await api.post('/api/login', { username, password })
    const data = res.data
    
    if (data.require_mfa) {
      mfaPending.value = true
      mfaPendingUserId.value = data.user_id
      return { requireMFA: true, userId: data.user_id }
    }
    
    if (data.need_binding) {
      mfaNeedBinding.value = true
      mfaPendingUserId.value = data.user_id
      return { needBinding: true, userId: data.user_id }
    }
    
    if (data.token) {
      token.value = data.token
      user.value = data.user
      localStorage.setItem('token', data.token)
      localStorage.setItem('currentUser', JSON.stringify(data.user))
    }
    return data
  }

  async function verifyMFA(userId, code) {
    const res = await api.post('/api/mfa/verify', { user_id: userId, code })
    const data = res.data
    if (data.token) {
      token.value = data.token
      user.value = data.user
      localStorage.setItem('token', data.token)
      localStorage.setItem('currentUser', JSON.stringify(data.user))
      mfaPending.value = false
      mfaPendingUserId.value = ''
    }
    return data
  }

  async function setupMFA(userId) {
    const res = await api.post('/api/mfa/setup', { user_id: userId })
    return res.data
  }

  async function bindMFA(userId, code, secret) {
    const res = await api.post('/api/mfa/bind-login', { user_id: userId, code, secret })
    const data = res.data
    if (data.token) {
      token.value = data.token
      user.value = data.user
      localStorage.setItem('token', data.token)
      localStorage.setItem('currentUser', JSON.stringify(data.user))
      mfaNeedBinding.value = false
      mfaPendingUserId.value = ''
    }
    return data
  }

  function cancelMFA() {
    mfaPending.value = false
    mfaNeedBinding.value = false
    mfaPendingUserId.value = ''
  }

  function logout() {
    token.value = ''
    user.value = null
    localStorage.removeItem('token')
  }

  async function fetchUser() {
    if (!token.value) return null
    try {
      const res = await api.get('/api/users/me')
      user.value = res.data
      return res.data
    } catch (e) {
      if (e.response?.status === 401) {
        logout()
      }
      return null
    }
  }

  function setUser(userData) {
    user.value = userData
  }

  function setToken(newToken) {
    token.value = newToken
  }

  async function loadMyPermissions() {
    if (!token.value) return
    try {
      const res = await api.get('/api/my/permissions')
      const data = res.data || {}
      myPermissions.value = data.permissions || {}
      myMenus.value = data.menus || []
      myRoles.value = data.roles || []
      console.log('[权限调试] API 返回:', JSON.stringify(data))
      console.log('[权限调试] myPermissions:', JSON.stringify(myPermissions.value))
      console.log('[权限调试] 加载权限完成:', Object.keys(myPermissions.value).length, '条, 角色:', myRoles.value)
    } catch (e) {
      console.error('[权限调试] 加载权限失败:', e)
    }
  }

  function isSuperAdmin() {
    // 检查用户通过 user_roles 表关联的角色（新 RBAC 系统）
    if (myRoles.value.includes('role_super_admin')) return true
    
    // 兼容旧数据：检查用户表中的 role 字段
    let role = user.value?.role || ''
    if (!role) {
      const cached = localStorage.getItem('currentUser')
      if (cached) {
        try {
          const cachedUser = JSON.parse(cached)
          role = cachedUser?.role || ''
        } catch (e) {}
      }
    }
    // 只有 super_admin 或 超级管理员 才是真正的超级管理员
    return role === 'super_admin' || role === '超级管理员'
  }

  function hasPermission(code) {
    if (isSuperAdmin()) return true
    return myPermissions.value[code] === true
  }

  function hasMenu(menuCode) {
    if (menuCode === 'welcome') return true
    if (isSuperAdmin()) return true
    return myPermissions.value['menu:' + menuCode] === true
  }

  function hasMenuGroup(groupCode) {
    if (isSuperAdmin()) return true
    const menuGroupMap = {
      'system': ['welcome', 'users', 'roles', 'permissions', 'audit', 'api-manage', 'schedule', 'task-pool', 'incidents', 'duty'],
      'resource': ['assets', 'domains', 'merchants', 'network', 'service-config', 'topology'],
      'monitor': ['metrics', 'alerts', 'alert-rules', 'alert-notify', 'dashboard-screen'],
      'k8s': ['clusters', 'workloads', 'configmaps', 'storage', 'terminal'],
      'ticket': ['tickets', 'workflow', 'sla', 'ticket-template'],
      'automation': ['jobs', 'crontab', 'inspection', 'self-healing'],
      'aiops': ['anomaly', 'root-cause', 'predict', 'smart-alert', 'capacity'],
      'release': ['deploy', 'change', 'rollback'],
      'logs': ['log-query', 'log-analysis', 'log-alert'],
      'security': ['vault', 'secrets', 'certificates'],
      'settings': ['datasources', 'sys-params', 'settings', 'sso-config']
    }
    const subMenus = menuGroupMap[groupCode] || []
    for (const sub of subMenus) {
      if (sub === 'welcome') return true
      const permKey = 'menu:' + sub
      if (myPermissions.value[permKey] === true) {
        console.log('[权限调试] hasMenuGroup', groupCode, '-> 子菜单', sub, '有权限')
        return true
      }
    }
    const result = myPermissions.value['menu:' + groupCode] === true
    console.log('[权限调试] hasMenuGroup', groupCode, '-> 结果:', result)
    return result
  }

  return {
    user,
    token,
    isLoggedIn,
    mfaPending,
    mfaPendingUserId,
    mfaNeedBinding,
    myPermissions,
    myMenus,
    myRoles,
    login,
    logout,
    fetchUser,
    setUser,
    setToken,
    verifyMFA,
    setupMFA,
    bindMFA,
    cancelMFA,
    loadMyPermissions,
    hasPermission,
    hasMenu,
    hasMenuGroup,
    isSuperAdmin
  }
})

export const useAppStore = defineStore('app', () => {
  const theme = ref(localStorage.getItem('theme') || 'dark')
  const showToastFlag = ref(false)
  const toastMessage = ref('')
  const toastType = ref('info')
  
  let toastTimeout = null

  // 确认弹窗状态
  const confirmDialog = ref({
    show: false,
    type: 'warning',
    title: '确认操作',
    message: '',
    okText: '确定',
    cancelText: '取消',
    resolve: null
  })

  function setTheme(newTheme) {
    theme.value = newTheme
    localStorage.setItem('theme', newTheme)
    document.body.classList.toggle('light-mode', newTheme === 'light')
  }

  function toggleTheme() {
    setTheme(theme.value === 'dark' ? 'light' : 'dark')
  }

  function showToast(message, type = 'info', duration = 3000) {
    if (toastTimeout) {
      clearTimeout(toastTimeout)
    }
    
    toastMessage.value = message
    toastType.value = type
    showToastFlag.value = true
    
    toastTimeout = setTimeout(() => {
      showToastFlag.value = false
    }, duration)
  }

  function showConfirm(options) {
    return new Promise((resolve) => {
      confirmDialog.value = {
        show: true,
        type: options.type || 'warning',
        title: options.title || '确认操作',
        message: options.message || '确定要执行此操作吗？',
        okText: options.okText || '确定',
        cancelText: options.cancelText || '取消',
        resolve
      }
    })
  }

  function confirmOk() {
    if (confirmDialog.value.resolve) {
      confirmDialog.value.resolve(true)
    }
    confirmDialog.value.show = false
  }

  function confirmCancel() {
    if (confirmDialog.value.resolve) {
      confirmDialog.value.resolve(false)
    }
    confirmDialog.value.show = false
  }

  function initTheme() {
    document.body.classList.toggle('light-mode', theme.value === 'light')
  }

  return {
    theme,
    showToastFlag,
    toastMessage,
    toastType,
    confirmDialog,
    setTheme,
    toggleTheme,
    showToast,
    showConfirm,
    confirmOk,
    confirmCancel,
    initTheme
  }
})
