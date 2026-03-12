import { defineStore } from 'pinia'
import { ref } from 'vue'

export const useAppStore = defineStore('app', () => {
  const language = ref(localStorage.getItem('pc_lang') || 'zh-CN')
  const showToastFlag = ref(false)
  const toastMessage = ref('')
  const toastType = ref('info')
  let toastTimeout = null

  function setLanguage(lang) {
    language.value = lang
    localStorage.setItem('pc_lang', lang)
  }

  function showToast(message, type = 'info', duration = 3000) {
    if (toastTimeout) clearTimeout(toastTimeout)
    toastMessage.value = message
    toastType.value = type
    showToastFlag.value = true
    toastTimeout = setTimeout(() => { showToastFlag.value = false }, duration)
  }

  // Simple i18n
  const zhCN = {
    users: '用户管理', roles: '角色管理', permissions: '权限管理',
    services: '服务管理', audit: '审计日志', dashboard: '首页',
    save: '保存', cancel: '取消', delete: '删除', edit: '编辑',
    add: '添加', search: '搜索', confirm: '确定', loading: '加载中...',
    logout: '退出登录', permCenter: '权限管理中心'
  }
  const enUS = {
    users: 'Users', roles: 'Roles', permissions: 'Permissions',
    services: 'Services', audit: 'Audit Logs', dashboard: 'Dashboard',
    save: 'Save', cancel: 'Cancel', delete: 'Delete', edit: 'Edit',
    add: 'Add', search: 'Search', confirm: 'Confirm', loading: 'Loading...',
    logout: 'Logout', permCenter: 'Permission Center'
  }

  function t(key) {
    const dict = language.value === 'en-US' ? enUS : zhCN
    return dict[key] || key
  }

  return { language, showToastFlag, toastMessage, toastType, setLanguage, showToast, t }
})
