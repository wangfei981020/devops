<script setup>
import { ref, computed, onMounted } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useAuthStore, useAppStore } from '@/stores'
import Sidebar from './Sidebar.vue'

const route = useRoute()
const router = useRouter()
const authStore = useAuthStore()
const appStore = useAppStore()

const showUserMenu = ref(false)
const currentLang = ref('zh-CN')
const permissionsLoaded = ref(false)

const pageTitle = computed(() => route.meta?.title || '欢迎页')

const currentUser = computed(() => authStore.user || JSON.parse(localStorage.getItem('currentUser') || '{}'))

const menuGroupMap = {
  welcome: '系统管理', users: '系统管理', roles: '系统管理', permissions: '系统管理', 
  audit: '系统管理', 'api-manage': '系统管理', schedule: '系统管理', 'task-pool': '系统管理', incidents: '系统管理',
  assets: '资源管理', domains: '资源管理', merchants: '资源管理', network: '资源管理', 
  'service-config': '资源管理', topology: '资源管理',
  metrics: '监控告警', alerts: '监控告警', 'alert-rules': '监控告警', 'alert-notify': '监控告警', 'dashboard-screen': '监控告警',
  clusters: 'K8S运维', workloads: 'K8S运维', configmaps: 'K8S运维', storage: 'K8S运维', terminal: 'K8S运维',
  tickets: '工单系统', workflow: '工单系统', sla: '工单系统', 'ticket-template': '工单系统',
  jobs: '自动化运维', crontab: '自动化运维', inspection: '自动化运维', 'self-healing': '自动化运维',
  anomaly: '智能运维', 'root-cause': '智能运维', predict: '智能运维', 'smart-alert': '智能运维', capacity: '智能运维',
  deploy: '变更发布', change: '变更发布', rollback: '变更发布',
  'log-query': '日志服务', 'log-analysis': '日志服务', 'log-alert': '日志服务',
  vault: '安全工具', secrets: '安全工具', certificates: '安全工具',
  datasources: '系统设置', 'sys-params': '系统设置', settings: '系统设置'
}

const menuGroup = computed(() => {
  const path = route.path.replace('/', '')
  return menuGroupMap[path] || '系统管理'
})

// 用于强制 Sidebar 重新渲染的 key
const sidebarKey = computed(() => Object.keys(authStore.myPermissions || {}).length)

onMounted(async () => {
  if (authStore.token) {
    if (!authStore.user) {
      await authStore.fetchUser()
    }
    await authStore.loadMyPermissions()
    permissionsLoaded.value = true
    console.log('[Layout] 权限加载完成，权限数量:', Object.keys(authStore.myPermissions || {}).length)
  } else {
    permissionsLoaded.value = true
  }
  document.addEventListener('click', (e) => {
    if (!e.target.closest('.user-dropdown')) showUserMenu.value = false
  })
})

function logout() {
  authStore.logout()
  router.push('/login')
}

function toggleTheme() {
  appStore.toggleTheme()
}

function openProfileSettings() {
  showUserMenu.value = false
  router.push('/settings')
}

function setLang(code) {
  currentLang.value = code
  localStorage.setItem('lang', code)
}

function getAvatarColor(name) {
  const colors = ['#3b82f6', '#8b5cf6', '#ec4899', '#f59e0b', '#10b981', '#6366f1']
  let hash = 0
  for (let i = 0; i < (name || 'U').length; i++) hash = name.charCodeAt(i) + ((hash << 5) - hash)
  return colors[Math.abs(hash) % colors.length]
}
</script>

<template>
  <div class="app-layout">
    <Sidebar :key="sidebarKey" />
    
    <div class="main-area">
      <header class="top-header">
        <div class="header-breadcrumb">
          <span class="breadcrumb-item">运维管理平台</span>
          <span class="breadcrumb-sep">/</span>
          <span class="breadcrumb-item">{{ menuGroup }}</span>
          <span class="breadcrumb-sep">/</span>
          <span class="breadcrumb-current">{{ pageTitle }}</span>
        </div>
        <div class="header-actions">
          <button class="header-btn" @click="toggleTheme" :title="appStore.theme === 'dark' ? '切换浅色' : '切换深色'">
            <svg v-if="appStore.theme === 'dark'" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><circle cx="12" cy="12" r="5"/><line x1="12" y1="1" x2="12" y2="3"/><line x1="12" y1="21" x2="12" y2="23"/><line x1="4.22" y1="4.22" x2="5.64" y2="5.64"/><line x1="18.36" y1="18.36" x2="19.78" y2="19.78"/><line x1="1" y1="12" x2="3" y2="12"/><line x1="21" y1="12" x2="23" y2="12"/><line x1="4.22" y1="19.78" x2="5.64" y2="18.36"/><line x1="18.36" y1="5.64" x2="19.78" y2="4.22"/></svg>
            <svg v-else viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M21 12.79A9 9 0 1 1 11.21 3 7 7 0 0 0 21 12.79z"/></svg>
          </button>
          
          <div class="user-dropdown">
            <button class="user-btn" @click.stop="showUserMenu = !showUserMenu">
              <div class="user-avatar-sm" :style="{ background: getAvatarColor(currentUser.username || 'U') }">{{ (currentUser.display_name || currentUser.username || 'U').charAt(0).toUpperCase() }}</div>
              <span class="user-name">{{ currentUser.display_name || currentUser.username || '用户' }}</span>
              <svg class="dropdown-arrow" :class="{ open: showUserMenu }" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M6 9l6 6 6-6"/></svg>
            </button>
            
            <div class="user-menu" v-show="showUserMenu">
              <div class="user-menu-header">
                <div class="user-avatar-lg" :style="{ background: getAvatarColor(currentUser.username || 'U') }">{{ (currentUser.display_name || currentUser.username || 'U').charAt(0).toUpperCase() }}</div>
                <div class="user-menu-info">
                  <div class="user-menu-name">{{ currentUser.display_name || currentUser.username }}</div>
                  <div class="user-menu-role">{{ currentUser.role === 'super_admin' ? '超级管理员' : currentUser.role === 'admin' ? '管理员' : '普通用户' }}</div>
                </div>
              </div>
              <div class="user-menu-divider"></div>
              <div class="user-menu-item" @click="openProfileSettings">
                <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M20 21v-2a4 4 0 0 0-4-4H8a4 4 0 0 0-4 4v2"/><circle cx="12" cy="7" r="4"/></svg>
                <span>个人设置</span>
              </div>
              <div class="user-menu-divider"></div>
              <div class="user-menu-label">界面语言</div>
              <div class="user-menu-item" :class="{ selected: currentLang === 'zh-CN' }" @click="setLang('zh-CN')">
                <span class="lang-flag">CN</span>
                <span>中文</span>
                <span v-if="currentLang === 'zh-CN'" class="menu-check">✓</span>
              </div>
              <div class="user-menu-item" :class="{ selected: currentLang === 'en-US' }" @click="setLang('en-US')">
                <span class="lang-flag">US</span>
                <span>English</span>
                <span v-if="currentLang === 'en-US'" class="menu-check">✓</span>
              </div>
              <div class="user-menu-divider"></div>
              <div class="user-menu-item logout" @click="logout">
                <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M9 21H5a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h4"/><polyline points="16 17 21 12 16 7"/><line x1="21" y1="12" x2="9" y2="12"/></svg>
                <span>退出登录</span>
              </div>
            </div>
          </div>
        </div>
      </header>
      
      <main class="main-content">
        <router-view />
      </main>
    </div>

    <!-- 全局确认弹窗 -->
    <Teleport to="body">
      <div class="confirm-overlay" :class="{ show: appStore.confirmDialog.show }" @click.self="appStore.confirmCancel()">
        <div class="confirm-modal" :class="appStore.confirmDialog.type">
          <div class="confirm-icon">
            <svg v-if="appStore.confirmDialog.type === 'danger'" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><circle cx="12" cy="12" r="10"/><line x1="15" y1="9" x2="9" y2="15"/><line x1="9" y1="9" x2="15" y2="15"/></svg>
            <svg v-else viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M10.29 3.86L1.82 18a2 2 0 001.71 3h16.94a2 2 0 001.71-3L13.71 3.86a2 2 0 00-3.42 0z"/><line x1="12" y1="9" x2="12" y2="13"/><line x1="12" y1="17" x2="12.01" y2="17"/></svg>
          </div>
          <div class="confirm-title">{{ appStore.confirmDialog.title }}</div>
          <div class="confirm-message">{{ appStore.confirmDialog.message }}</div>
          <div class="confirm-actions">
            <button class="confirm-btn cancel" @click="appStore.confirmCancel()">{{ appStore.confirmDialog.cancelText }}</button>
            <button class="confirm-btn ok" :class="appStore.confirmDialog.type" @click="appStore.confirmOk()">{{ appStore.confirmDialog.okText }}</button>
          </div>
        </div>
      </div>
    </Teleport>
  </div>
</template>

<style scoped>
.app-layout {
  display: flex;
  min-height: 100vh;
  background: var(--bg-main);
}

.main-area {
  flex: 1;
  margin-left: 200px;
  display: flex;
  flex-direction: column;
  height: 100vh;
  overflow: hidden;
}

.top-header {
  height: 48px;
  padding: 0 24px;
  background: var(--bg-card);
  border-bottom: 1px solid var(--border-color);
  display: flex;
  align-items: center;
  justify-content: space-between;
  position: sticky;
  top: 0;
  z-index: 50;
}

.header-breadcrumb {
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: 13px;
}

.breadcrumb-item {
  color: var(--text-muted);
}

.breadcrumb-sep {
  color: var(--text-muted);
  opacity: 0.5;
}

.breadcrumb-current {
  color: var(--text-primary);
  font-weight: 500;
}

.header-actions {
  display: flex;
  align-items: center;
  gap: 8px;
}

.header-btn {
  width: 32px;
  height: 32px;
  display: flex;
  align-items: center;
  justify-content: center;
  background: transparent;
  border: none;
  border-radius: 6px;
  color: var(--text-muted);
  cursor: pointer;
  transition: all 0.2s;
}

.header-btn:hover {
  background: var(--bg-hover);
  color: var(--text-primary);
}

.header-btn svg {
  width: 18px;
  height: 18px;
}

.main-content {
  flex: 1;
  padding: 24px;
  overflow-y: auto;
  overflow-x: auto;
  height: calc(100vh - 48px);
  width: 100%;
}

.user-dropdown {
  position: relative;
}

.user-btn {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 4px 10px 4px 4px;
  background: var(--bg-hover);
  border: 1px solid var(--border-color);
  border-radius: 8px;
  cursor: pointer;
  transition: all 0.2s;
}

.user-btn:hover {
  border-color: #3b82f6;
}

.user-avatar-sm {
  width: 28px;
  height: 28px;
  border-radius: 6px;
  display: flex;
  align-items: center;
  justify-content: center;
  color: #fff;
  font-size: 12px;
  font-weight: 600;
}

.user-name {
  font-size: 13px;
  color: var(--text-primary);
  font-weight: 500;
}

.dropdown-arrow {
  width: 14px;
  height: 14px;
  color: var(--text-muted);
  transition: transform 0.2s;
}

.dropdown-arrow.open {
  transform: rotate(180deg);
}

.user-menu {
  position: absolute;
  top: calc(100% + 8px);
  right: 0;
  width: 220px;
  background: var(--bg-card);
  border: 1px solid var(--border-color);
  border-radius: 12px;
  box-shadow: 0 10px 40px rgba(0,0,0,0.15);
  z-index: 1000;
  padding: 8px 0;
}

.user-menu-header {
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 12px 16px;
}

.user-avatar-lg {
  width: 40px;
  height: 40px;
  border-radius: 10px;
  display: flex;
  align-items: center;
  justify-content: center;
  color: #fff;
  font-size: 16px;
  font-weight: 600;
}

.user-menu-info {
  flex: 1;
}

.user-menu-name {
  font-size: 14px;
  font-weight: 600;
  color: var(--text-primary);
}

.user-menu-role {
  font-size: 12px;
  color: var(--text-muted);
}

.user-menu-divider {
  height: 1px;
  background: var(--border-color);
  margin: 4px 12px;
}

.user-menu-label {
  font-size: 11px;
  color: var(--text-muted);
  text-transform: uppercase;
  letter-spacing: 0.5px;
  padding: 8px 16px 4px;
}

.user-menu-item {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 10px 16px;
  font-size: 13px;
  color: var(--text-primary);
  cursor: pointer;
  transition: background 0.15s;
}

.user-menu-item:hover {
  background: var(--bg-hover);
}

.user-menu-item svg {
  width: 16px;
  height: 16px;
  color: var(--text-muted);
}

.user-menu-item.logout {
  color: #ef4444;
}

.user-menu-item.logout svg {
  color: #ef4444;
}

.lang-flag {
  width: 22px;
  height: 16px;
  background: #e2e8f0;
  border-radius: 3px;
  font-size: 10px;
  font-weight: 600;
  color: #475569;
  display: flex;
  align-items: center;
  justify-content: center;
}

.menu-check {
  margin-left: auto;
  color: #3b82f6;
  font-weight: 600;
}

/* 确认弹窗样式 */
.confirm-overlay {
  position: fixed;
  inset: 0;
  background: rgba(0, 0, 0, 0.5);
  display: flex;
  align-items: center;
  justify-content: center;
  z-index: 10000;
  opacity: 0;
  visibility: hidden;
  transition: all 0.2s;
}

.confirm-overlay.show {
  opacity: 1;
  visibility: visible;
}

.confirm-modal {
  background: var(--bg-card);
  border-radius: 16px;
  padding: 32px;
  width: 400px;
  max-width: 90vw;
  text-align: center;
  box-shadow: 0 20px 40px rgba(0, 0, 0, 0.3);
  transform: scale(0.9);
  transition: transform 0.2s;
}

.confirm-overlay.show .confirm-modal {
  transform: scale(1);
}

.confirm-icon {
  width: 56px;
  height: 56px;
  border-radius: 50%;
  display: flex;
  align-items: center;
  justify-content: center;
  margin: 0 auto 16px;
}

.confirm-modal.warning .confirm-icon {
  background: rgba(245, 158, 11, 0.15);
  color: #f59e0b;
}

.confirm-modal.danger .confirm-icon {
  background: rgba(239, 68, 68, 0.15);
  color: #ef4444;
}

.confirm-icon svg {
  width: 28px;
  height: 28px;
}

.confirm-title {
  font-size: 18px;
  font-weight: 600;
  color: var(--text-primary);
  margin-bottom: 8px;
}

.confirm-message {
  font-size: 14px;
  color: var(--text-secondary);
  line-height: 1.6;
  margin-bottom: 24px;
  white-space: pre-line;
}

.confirm-actions {
  display: flex;
  gap: 12px;
  justify-content: center;
}

.confirm-btn {
  padding: 10px 24px;
  border-radius: 8px;
  font-size: 14px;
  font-weight: 500;
  cursor: pointer;
  transition: all 0.2s;
  border: none;
}

.confirm-btn.cancel {
  background: var(--bg-input);
  color: var(--text-primary);
}

.confirm-btn.cancel:hover {
  background: var(--bg-hover);
}

.confirm-btn.ok {
  background: #3b82f6;
  color: #fff;
}

.confirm-btn.ok:hover {
  background: #2563eb;
}

.confirm-btn.ok.danger {
  background: #ef4444;
}

.confirm-btn.ok.danger:hover {
  background: #dc2626;
}

.confirm-btn.ok.warning {
  background: #f59e0b;
}

.confirm-btn.ok.warning:hover {
  background: #d97706;
}
</style>
