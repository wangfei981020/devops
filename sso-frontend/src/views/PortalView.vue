<template>
  <div class="portal-layout" :class="{ collapsed: sidebarCollapsed }">
    <!-- Sidebar -->
    <aside class="sidebar" :class="{ collapsed: sidebarCollapsed }">
      <div class="sidebar-header">
        <div class="sidebar-logo" v-if="!sidebarCollapsed">
          <svg class="logo-icon" viewBox="0 0 130 32" fill="none" xmlns="http://www.w3.org/2000/svg">
            <defs>
              <linearGradient id="ssoGrad" x1="0%" y1="0%" x2="100%" y2="100%">
                <stop offset="0%" style="stop-color:#3b82f6"/>
                <stop offset="50%" style="stop-color:#8b5cf6"/>
                <stop offset="100%" style="stop-color:#06b6d4"/>
              </linearGradient>
              <linearGradient id="ssoGrad2" x1="0%" y1="0%" x2="100%" y2="0%">
                <stop offset="0%" style="stop-color:#3b82f6"/>
                <stop offset="100%" style="stop-color:#8b5cf6"/>
              </linearGradient>
            </defs>
            <path d="M4 16a8 8 0 0 1 8-8h0a8 8 0 0 1 8 8v0a8 8 0 0 1-8 8h0a8 8 0 0 1-8-8z" fill="url(#ssoGrad)" opacity="0.9"/>
            <path d="M12 10v4l3 2" stroke="white" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round" fill="none"/>
            <circle cx="12" cy="16" r="6" stroke="white" stroke-width="1.2" fill="none" opacity="0.4"/>
            <text x="28" y="21" font-family="system-ui, -apple-system, sans-serif" font-size="14" font-weight="700" fill="url(#ssoGrad2)">SSO Gateway</text>
          </svg>
        </div>
        <div v-else class="sidebar-logo-mini">
          <div class="logo-mini-icon">
            <svg viewBox="0 0 24 24" fill="none" stroke="white" stroke-width="2"><path d="M12 22s8-4 8-10V5l-8-3-8 3v7c0 6 8 10 8 10z"/></svg>
          </div>
        </div>
        <button class="collapse-btn" @click="sidebarCollapsed = !sidebarCollapsed" :title="sidebarCollapsed ? '展开' : '收起'">
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" width="14" height="14">
            <path v-if="sidebarCollapsed" d="M9 5l7 7-7 7"/>
            <path v-else d="M15 5l-7 7 7-7"/>
          </svg>
        </button>
      </div>

      <!-- Environment badge -->
      <div v-if="envLabel && !sidebarCollapsed" class="env-bar">
        <span class="env-badge" :style="{ background: envColor + '18', color: envColor, borderColor: envColor + '40' }">{{ envLabel }}</span>
      </div>

      <nav class="sidebar-nav" v-if="!sidebarCollapsed">
        <!-- Navigation Group -->
        <div class="nav-group">
          <button class="nav-group-header" :class="{ expanded: expandedGroups.nav }" @click="toggleGroup('nav')">
            <span class="nav-group-icon">
              <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" width="18" height="18"><rect x="3" y="3" width="7" height="7" rx="1"/><rect x="14" y="3" width="7" height="7" rx="1"/><rect x="3" y="14" width="7" height="7" rx="1"/><rect x="14" y="14" width="7" height="7" rx="1"/></svg>
            </span>
            <span class="nav-group-title">导航菜单</span>
            <span class="nav-group-arrow" :class="{ expanded: expandedGroups.nav }">
              <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M9 5l7 7-7 7"/></svg>
            </span>
          </button>
          <div class="nav-sub-menu" v-show="expandedGroups.nav">
            <button class="nav-item sub active">服务门户</button>
            <button v-if="authStore.isAdmin" class="nav-item sub" @click="$router.push('/admin')">管理后台</button>
          </div>
        </div>

        <!-- Environment Groups -->
        <div class="nav-group" v-for="env in envSections" :key="env.name">
          <button class="nav-group-header" :class="{ expanded: expandedGroups[env.name] }" @click="toggleGroup(env.name)">
            <span class="nav-group-env-dot" :style="{ background: env.color }"></span>
            <span class="nav-group-title">{{ env.label }}</span>
            <span class="nav-group-count">{{ env.services.length }}</span>
            <span class="nav-group-arrow" :class="{ expanded: expandedGroups[env.name] }">
              <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M9 5l7 7-7 7"/></svg>
            </span>
          </button>
          <div class="nav-sub-menu" v-show="expandedGroups[env.name]">
            <button
              v-for="svc in env.services" :key="svc.client_id"
              class="nav-item sub svc-item"
              @click="openService(svc)"
            >
              <span class="nav-svc-icon" :style="{ background: getGradient(svc.client_name) }">
                <img v-if="isImageUrl(svc.icon)" :src="svc.icon" :alt="svc.client_name" class="nav-svc-logo" />
                <span v-else>{{ getInitial(svc.client_name) }}</span>
              </span>
              <span class="nav-svc-name">{{ svc.client_name }}</span>
            </button>
          </div>
        </div>
      </nav>

      <!-- Collapsed sidebar icons -->
      <nav class="sidebar-nav-mini" v-else>
        <div class="nav-mini-item active" title="服务门户">
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" width="18" height="18"><rect x="3" y="3" width="7" height="7" rx="1"/><rect x="14" y="3" width="7" height="7" rx="1"/><rect x="3" y="14" width="7" height="7" rx="1"/><rect x="14" y="14" width="7" height="7" rx="1"/></svg>
        </div>
        <div v-if="authStore.isAdmin" class="nav-mini-item" title="管理后台" @click="$router.push('/admin')">
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" width="18" height="18"><circle cx="12" cy="12" r="3"/><path d="M12.22 2h-.44a2 2 0 0 0-2 2v.18a2 2 0 0 1-1 1.73l-.43.25a2 2 0 0 1-2 0l-.15-.08a2 2 0 0 0-2.73.73l-.22.38a2 2 0 0 0 .73 2.73l.15.1a2 2 0 0 1 1 1.72v.51a2 2 0 0 1-1 1.74l-.15.09a2 2 0 0 0-.73 2.73l.22.38a2 2 0 0 0 2.73.73l.15-.08a2 2 0 0 1 2 0l.43.25a2 2 0 0 1 1 1.73V20a2 2 0 0 0 2 2h.44a2 2 0 0 0 2-2v-.18a2 2 0 0 1 1-1.73l.43-.25a2 2 0 0 1 2 0l.15.08a2 2 0 0 0 2.73-.73l.22-.39a2 2 0 0 0-.73-2.73l-.15-.08a2 2 0 0 1-1-1.74v-.5a2 2 0 0 1 1-1.74l.15-.09a2 2 0 0 0 .73-2.73l-.22-.38a2 2 0 0 0-2.73-.73l-.15.08a2 2 0 0 1-2 0l-.43-.25a2 2 0 0 1-1-1.73V4a2 2 0 0 0-2-2z"/></svg>
        </div>
      </nav>

      <!-- User info at bottom -->
      <div class="sidebar-bottom">
        <div class="sidebar-user" :title="sidebarCollapsed ? authStore.displayName : ''">
          <div class="user-avatar">{{ getInitial(authStore.displayName) }}</div>
          <template v-if="!sidebarCollapsed">
            <div class="user-info">
              <div class="user-name">{{ authStore.displayName }}</div>
              <div class="user-role">{{ authStore.isAdmin ? '管理员' : '用户' }}</div>
            </div>
            <button class="logout-btn" @click="handleLogout" title="退出登录">
              <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" width="16" height="16"><path d="M9 21H5a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h4"/><polyline points="16 17 21 12 16 7"/><line x1="21" y1="12" x2="9" y2="12"/></svg>
            </button>
          </template>
        </div>
      </div>
    </aside>

    <!-- Main Content -->
    <div class="main-content">
      <div class="page-header">
        <div>
          <h1>{{ greeting }}，{{ authStore.displayName }}</h1>
          <p class="page-desc">这是你的服务总览</p>
        </div>
      </div>

      <!-- Stats Row -->
      <div class="stats-row">
        <div class="stat-card">
          <div class="stat-icon" style="background: rgba(99,102,241,0.1); color: #6366f1;">
            <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" width="20" height="20"><rect x="3" y="3" width="7" height="7" rx="1"/><rect x="14" y="3" width="7" height="7" rx="1"/><rect x="3" y="14" width="7" height="7" rx="1"/><rect x="14" y="14" width="7" height="7" rx="1"/></svg>
          </div>
          <div>
            <div class="stat-value">{{ services.length }}</div>
            <div class="stat-label">已接入服务</div>
          </div>
        </div>
        <div class="stat-card">
          <div class="stat-icon" style="background: rgba(34,197,94,0.1); color: #22c55e;">
            <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" width="20" height="20"><path d="M21 12a9 9 0 1 1-18 0 9 9 0 0 1 18 0z"/><path d="M9 12l2 2 4-4"/></svg>
          </div>
          <div>
            <div class="stat-value">{{ environments.length }}</div>
            <div class="stat-label">运行环境</div>
          </div>
        </div>
        <div class="stat-card">
          <div class="stat-icon" style="background: rgba(59,130,246,0.1); color: #3b82f6;">
            <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" width="20" height="20"><path d="M22 12h-4l-3 9L9 3l-3 9H2"/></svg>
          </div>
          <div>
            <div class="stat-value" style="color: #22c55e">正常</div>
            <div class="stat-label">服务状态</div>
          </div>
        </div>
      </div>

      <!-- Loading -->
      <div v-if="loading" class="loading-state">
        <div class="spinner"></div>
        <span>加载中...</span>
      </div>

      <!-- Empty -->
      <div v-else-if="services.length === 0" class="empty-state">
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" width="48" height="48" style="color: #cbd5e1; margin-bottom: 12px;"><rect x="3" y="3" width="18" height="18" rx="2"/><path d="M3 9h18"/><path d="M9 21V9"/></svg>
        <p>暂无可用服务</p>
      </div>

      <!-- Services grouped by environment -->
      <template v-else>
        <div v-for="env in envSections" :key="env.name" class="env-section">
          <div class="section-title">
            <span class="env-indicator" :style="{ background: env.color }"></span>
            <span>{{ env.label }}</span>
            <span class="section-count">{{ env.services.length }} 个服务</span>
          </div>
          <div class="services-grid">
            <div v-for="svc in env.services" :key="svc.client_id" class="svc-card" @click="openService(svc)">
              <div class="svc-icon" :style="{ background: isImageUrl(svc.icon) ? '#f8fafc' : getGradient(svc.client_name) }">
                <img v-if="isImageUrl(svc.icon)" :src="svc.icon" :alt="svc.client_name" class="svc-logo" />
                <span v-else class="svc-initial">{{ getInitial(svc.client_name) }}</span>
              </div>
              <div class="svc-info">
                <h3>{{ svc.client_name }}</h3>
                <p>{{ svc.description || '暂无描述' }}</p>
              </div>
              <div class="svc-arrow">
                <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" width="16" height="16"><path d="M5 12h14M12 5l7 7-7 7"/></svg>
              </div>
            </div>
          </div>
        </div>
      </template>
    </div>
  </div>
</template>

<script setup>
import { ref, reactive, computed, onMounted } from 'vue'
import { useRouter } from 'vue-router'
import { useAuthStore } from '../stores/auth'
import api from '../api'

const router = useRouter()
const authStore = useAuthStore()

const services = ref([])
const environments = ref([])
const loading = ref(true)
const envLabel = ref('')
const envColor = ref('#3b82f6')
const sidebarCollapsed = ref(false)

const expandedGroups = reactive({ nav: true })

function toggleGroup(key) {
  expandedGroups[key] = !expandedGroups[key]
}

const greeting = computed(() => {
  const h = new Date().getHours()
  if (h < 6) return '夜深了'
  if (h < 12) return '上午好'
  if (h < 18) return '下午好'
  return '晚上好'
})

// Group services by environment
const envSections = computed(() => {
  const envMap = {}
  for (const env of environments.value) {
    envMap[env.name] = { name: env.name, label: env.label, color: env.color, sort: env.sort_order, services: [] }
    // Auto-expand all environment groups
    if (expandedGroups[env.name] === undefined) expandedGroups[env.name] = true
  }
  envMap[''] = { name: '', label: '未分类', color: '#6b7280', sort: 999, services: [] }

  for (const svc of services.value) {
    const envName = svc.environment || ''
    if (envMap[envName]) {
      envMap[envName].services.push(svc)
    } else {
      envMap[''].services.push(svc)
    }
  }

  return Object.values(envMap)
    .filter(e => e.services.length > 0)
    .sort((a, b) => a.sort - b.sort)
})

const gradients = [
  'linear-gradient(135deg, #6366f1, #8b5cf6)',
  'linear-gradient(135deg, #ec4899, #f43f5e)',
  'linear-gradient(135deg, #06b6d4, #0ea5e9)',
  'linear-gradient(135deg, #22c55e, #10b981)',
  'linear-gradient(135deg, #f59e0b, #d97706)',
  'linear-gradient(135deg, #a855f7, #7c3aed)',
  'linear-gradient(135deg, #f97316, #ea580c)',
  'linear-gradient(135deg, #14b8a6, #0d9488)',
]

function getGradient(name) {
  let hash = 0
  for (let i = 0; i < (name || '').length; i++) hash = name.charCodeAt(i) + ((hash << 5) - hash)
  return gradients[Math.abs(hash) % gradients.length]
}

function getInitial(name) {
  return (name || '?').charAt(0).toUpperCase()
}

function isImageUrl(icon) {
  return icon && (icon.startsWith('/') || icon.startsWith('http'))
}

function openService(svc) {
  if (!svc.home_url) return
  // 通过应用的 OIDC 登录端点跳转，实现真正的 SSO 单点登录
  // 应用会发起 OIDC 流程 → SSO 检测到当前 session → 自动签发 token → 免密码登录
  const oidcUrl = svc.home_url.replace(/\/$/, '') + '/api/oidc/login'
  window.open(oidcUrl, '_blank')
}

async function loadServices() {
  try {
    const res = await api.get('/api/portal/services')
    services.value = res.data.data || []
  } catch (e) {
    console.error('Failed to load services:', e)
  } finally {
    loading.value = false
  }
}

async function loadEnvironments() {
  try {
    const res = await api.get('/api/public/environments')
    environments.value = res.data.data || []
  } catch (e) {
    console.error('Failed to load environments:', e)
  }
}

async function loadEnv() {
  try {
    const res = await api.get('/api/public/env')
    envLabel.value = res.data.data?.env_label || ''
    envColor.value = res.data.data?.env_color || '#3b82f6'
  } catch (e) {}
}

async function handleLogout() {
  await authStore.logout()
  router.push('/login')
}

onMounted(() => {
  loadEnvironments()
  loadServices()
  loadEnv()
})
</script>

<style scoped>
.portal-layout {
  display: flex;
  min-height: 100vh;
  background: #f1f5f9;
  font-family: -apple-system, BlinkMacSystemFont, 'Inter', 'PingFang SC', 'Microsoft YaHei', sans-serif;
}

/* ===== Sidebar ===== */
.sidebar {
  width: 220px;
  height: 100vh;
  background: linear-gradient(180deg, #1e293b 0%, #0f172a 100%);
  display: flex;
  flex-direction: column;
  position: fixed;
  left: 0;
  top: 0;
  z-index: 100;
  transition: width 0.25s cubic-bezier(0.4, 0, 0.2, 1);
  overflow: hidden;
}
.sidebar.collapsed { width: 60px; }

.sidebar::-webkit-scrollbar { width: 4px; }
.sidebar::-webkit-scrollbar-thumb { background: rgba(255,255,255,0.1); border-radius: 2px; }

/* Header */
.sidebar-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 14px 12px;
  border-bottom: 1px solid rgba(255, 255, 255, 0.06);
  min-height: 56px;
  flex-shrink: 0;
}
.sidebar-logo { display: flex; align-items: center; flex: 1; overflow: hidden; }
.logo-icon { height: 26px; width: auto; }

.sidebar-logo-mini { display: flex; align-items: center; justify-content: center; flex: 1; }
.logo-mini-icon {
  width: 30px; height: 30px; border-radius: 8px;
  background: linear-gradient(135deg, #3b82f6, #8b5cf6);
  display: flex; align-items: center; justify-content: center;
}
.logo-mini-icon svg { width: 16px; height: 16px; color: white; }

.collapse-btn {
  width: 24px; height: 24px; min-width: 24px; border-radius: 4px;
  border: none; background: rgba(255,255,255,0.05);
  color: rgba(255,255,255,0.4); cursor: pointer;
  display: flex; align-items: center; justify-content: center;
  transition: all 0.2s;
}
.collapse-btn:hover { background: rgba(255,255,255,0.1); color: rgba(255,255,255,0.8); }

/* Env badge bar */
.env-bar {
  padding: 8px 14px;
  border-bottom: 1px solid rgba(255, 255, 255, 0.04);
}
.env-badge {
  display: inline-block;
  padding: 2px 10px;
  border-radius: 4px;
  font-size: 11px;
  font-weight: 600;
  border: 1px solid;
}

/* Navigation */
.sidebar-nav {
  flex: 1;
  padding: 6px 0;
  overflow-y: auto;
  overflow-x: hidden;
}

.nav-group {
  margin-bottom: 2px;
}

.nav-group-header {
  width: 100%;
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 9px 14px;
  background: transparent;
  border: none;
  color: rgba(255, 255, 255, 0.65);
  font-size: 13px;
  font-weight: 500;
  cursor: pointer;
  transition: all 0.15s;
  text-align: left;
}
.nav-group-header:hover {
  color: #fff;
  background: rgba(255, 255, 255, 0.04);
}

.nav-group-icon {
  width: 18px;
  height: 18px;
  display: flex;
  align-items: center;
  justify-content: center;
  flex-shrink: 0;
  opacity: 0.8;
}

.nav-group-env-dot {
  width: 8px;
  height: 8px;
  border-radius: 50%;
  flex-shrink: 0;
}

.nav-group-title {
  flex: 1;
}

.nav-group-count {
  font-size: 11px;
  color: rgba(255,255,255,0.3);
  background: rgba(255,255,255,0.06);
  padding: 1px 6px;
  border-radius: 8px;
  min-width: 18px;
  text-align: center;
}

.nav-group-arrow {
  width: 14px;
  height: 14px;
  transition: transform 0.2s;
  opacity: 0.4;
  display: flex;
  align-items: center;
  justify-content: center;
}
.nav-group-arrow svg { width: 12px; height: 12px; }
.nav-group-arrow.expanded { transform: rotate(90deg); }

/* Sub menu */
.nav-sub-menu {
  padding: 0 0 4px 0;
}

.nav-item.sub {
  width: 100%;
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 8px 14px 8px 36px;
  background: transparent;
  border: none;
  color: rgba(255, 255, 255, 0.5);
  font-size: 13px;
  cursor: pointer;
  transition: all 0.15s;
  text-align: left;
  border-radius: 0;
  position: relative;
}
.nav-item.sub:hover {
  color: rgba(255, 255, 255, 0.85);
  background: rgba(59, 130, 246, 0.06);
}
.nav-item.sub.active {
  color: #fff;
  background: linear-gradient(90deg, #3b82f6 0%, #2563eb 100%);
  border-radius: 6px 0 0 6px;
  margin-left: 8px;
  padding-left: 28px;
  font-weight: 500;
}

/* Service items in sidebar */
.nav-item.svc-item {
  padding: 6px 14px 6px 30px;
  gap: 8px;
}
.nav-svc-icon {
  width: 22px;
  height: 22px;
  min-width: 22px;
  border-radius: 5px;
  display: flex;
  align-items: center;
  justify-content: center;
  font-size: 11px;
  font-weight: 600;
  color: white;
  overflow: hidden;
}
.nav-svc-logo {
  width: 100%;
  height: 100%;
  object-fit: contain;
}
.nav-svc-name {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

/* Collapsed mini nav */
.sidebar-nav-mini {
  flex: 1;
  padding: 8px 0;
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 4px;
}
.nav-mini-item {
  width: 38px;
  height: 38px;
  border-radius: 8px;
  display: flex;
  align-items: center;
  justify-content: center;
  color: rgba(255,255,255,0.5);
  cursor: pointer;
  transition: all 0.15s;
}
.nav-mini-item:hover { background: rgba(255,255,255,0.08); color: rgba(255,255,255,0.9); }
.nav-mini-item.active { background: linear-gradient(135deg, #3b82f6, #2563eb); color: white; }

/* Sidebar bottom - user info */
.sidebar-bottom {
  border-top: 1px solid rgba(255, 255, 255, 0.06);
  padding: 10px 12px;
  flex-shrink: 0;
}
.sidebar-user {
  display: flex;
  align-items: center;
  gap: 8px;
}
.user-avatar {
  width: 32px;
  height: 32px;
  min-width: 32px;
  border-radius: 8px;
  background: linear-gradient(135deg, #3b82f6, #6366f1);
  display: flex;
  align-items: center;
  justify-content: center;
  color: white;
  font-size: 13px;
  font-weight: 600;
}
.user-info { flex: 1; overflow: hidden; }
.user-name {
  font-size: 12px;
  color: rgba(255,255,255,0.85);
  font-weight: 500;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
.user-role { font-size: 11px; color: rgba(255,255,255,0.35); }

.logout-btn {
  width: 28px; height: 28px; min-width: 28px; border-radius: 6px;
  border: none; background: transparent; color: rgba(255,255,255,0.35);
  cursor: pointer; display: flex; align-items: center; justify-content: center;
  transition: all 0.2s;
}
.logout-btn:hover { background: rgba(239,68,68,0.15); color: #f87171; }

/* ===== Main Content ===== */
.main-content {
  flex: 1;
  margin-left: 220px;
  padding: 28px 36px;
  transition: margin-left 0.25s cubic-bezier(0.4, 0, 0.2, 1);
  max-width: 1200px;
}
.portal-layout.collapsed .main-content { margin-left: 60px; }

.page-header { margin-bottom: 24px; }
.page-header h1 {
  font-size: 22px;
  font-weight: 700;
  color: #0f172a;
  margin: 0 0 4px 0;
}
.page-desc { color: #64748b; font-size: 13px; margin: 0; }

/* Stats */
.stats-row {
  display: grid;
  grid-template-columns: repeat(3, 1fr);
  gap: 14px;
  margin-bottom: 28px;
}
.stat-card {
  background: #fff;
  border-radius: 10px;
  padding: 16px 18px;
  border: 1px solid #e2e8f0;
  display: flex;
  align-items: center;
  gap: 14px;
}
.stat-icon {
  width: 40px;
  height: 40px;
  border-radius: 10px;
  display: flex;
  align-items: center;
  justify-content: center;
  flex-shrink: 0;
}
.stat-label { font-size: 12px; color: #64748b; }
.stat-value { font-size: 22px; font-weight: 700; color: #0f172a; line-height: 1.2; }

/* Loading / Empty */
.loading-state {
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 10px;
  padding: 60px;
  color: #64748b;
}
.spinner {
  width: 20px; height: 20px;
  border: 2px solid #e2e8f0;
  border-top-color: #3b82f6;
  border-radius: 50%;
  animation: spin 0.8s linear infinite;
}
@keyframes spin { to { transform: rotate(360deg); } }
.empty-state {
  text-align: center;
  padding: 60px;
  color: #94a3b8;
  font-size: 14px;
  display: flex;
  flex-direction: column;
  align-items: center;
}

/* Environment sections */
.env-section { margin-bottom: 28px; }
.section-title {
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: 14px;
  font-weight: 600;
  color: #1e293b;
  margin-bottom: 12px;
  padding-bottom: 8px;
  border-bottom: 1px solid #e2e8f0;
}
.env-indicator {
  width: 8px;
  height: 8px;
  border-radius: 2px;
  flex-shrink: 0;
}
.section-count {
  margin-left: auto;
  font-size: 12px;
  color: #94a3b8;
  font-weight: 400;
}

/* Service cards */
.services-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(300px, 1fr));
  gap: 12px;
}
.svc-card {
  display: flex;
  align-items: center;
  gap: 14px;
  padding: 16px 18px;
  background: #fff;
  border-radius: 10px;
  border: 1px solid #e2e8f0;
  cursor: pointer;
  transition: all 0.2s;
}
.svc-card:hover {
  border-color: #3b82f6;
  box-shadow: 0 2px 12px rgba(59,130,246,0.08);
  transform: translateY(-1px);
}

.svc-icon {
  width: 44px;
  height: 44px;
  min-width: 44px;
  border-radius: 10px;
  display: flex;
  align-items: center;
  justify-content: center;
  overflow: hidden;
}
.svc-logo { width: 100%; height: 100%; object-fit: contain; padding: 4px; }
.svc-initial { font-size: 18px; font-weight: 700; color: white; }

.svc-info { flex: 1; min-width: 0; }
.svc-info h3 {
  font-size: 14px;
  font-weight: 600;
  color: #1e293b;
  margin: 0 0 2px 0;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
.svc-info p {
  font-size: 12px;
  color: #94a3b8;
  margin: 0;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.svc-arrow {
  color: #cbd5e1;
  flex-shrink: 0;
  transition: all 0.2s;
}
.svc-card:hover .svc-arrow { color: #3b82f6; transform: translateX(2px); }

/* Responsive */
@media (max-width: 768px) {
  .sidebar { width: 60px; }
  .sidebar .sidebar-logo, .sidebar .env-bar, .sidebar .sidebar-nav { display: none; }
  .sidebar .sidebar-nav-mini { display: flex; }
  .main-content { margin-left: 60px; padding: 20px 16px; }
  .stats-row { grid-template-columns: 1fr; }
  .services-grid { grid-template-columns: 1fr; }
}
</style>
