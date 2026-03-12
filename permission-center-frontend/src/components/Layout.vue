<script setup>
import { ref, computed } from 'vue'
import { useRouter, useRoute } from 'vue-router'
import { useAuthStore } from '@/stores/auth'
import { useAppStore } from '@/stores/app'

const router = useRouter()
const route = useRoute()
const authStore = useAuthStore()
const appStore = useAppStore()
const showUserMenu = ref(false)

const menuItems = [
  { key: 'dashboard', name: '首页', icon: '📊', path: '/dashboard' },
  { key: 'users', name: '用户管理', icon: '👥', path: '/users' },
  { key: 'roles', name: '角色管理', icon: '🔑', path: '/roles' },
  { key: 'permissions', name: '权限管理', icon: '🛡️', path: '/permissions' },
  { key: 'services', name: '服务管理', icon: '🔗', path: '/services' },
  { key: 'audit', name: '审计日志', icon: '📋', path: '/audit' },
  { key: 'settings', name: '系统设置', icon: '⚙️', path: '/settings' }
]

const currentPath = computed(() => route.path)

function navigateTo(path) {
  router.push(path)
}

function handleLogout() {
  authStore.logout()
  router.push('/login')
}

function toggleLang() {
  appStore.setLanguage(appStore.language === 'zh-CN' ? 'en-US' : 'zh-CN')
}
</script>

<template>
  <div class="layout">
    <!-- Sidebar -->
    <aside class="sidebar">
      <div class="sidebar-header">
        <span class="logo">🛡️</span>
        <span class="title">权限中心</span>
      </div>

      <nav class="sidebar-nav">
        <a
          v-for="item in menuItems"
          :key="item.key"
          class="nav-item"
          :class="{ active: currentPath === item.path }"
          @click="navigateTo(item.path)"
        >
          <span class="nav-icon">{{ item.icon }}</span>
          <span class="nav-text">{{ item.name }}</span>
        </a>
      </nav>
    </aside>

    <!-- Main Content -->
    <div class="main">
      <!-- Header -->
      <header class="header">
        <div class="header-left">
          <h2>{{ menuItems.find(i => currentPath === i.path)?.name || '权限管理中心' }}</h2>
        </div>
        <div class="header-right">
          <button class="lang-btn" @click="toggleLang">
            {{ appStore.language === 'zh-CN' ? 'EN' : '中' }}
          </button>
          <div class="user-info" @click="showUserMenu = !showUserMenu">
            <span class="avatar">{{ (authStore.user?.username || 'U')[0].toUpperCase() }}</span>
            <span class="username">{{ authStore.user?.username || '未登录' }}</span>
            <span class="arrow">▾</span>

            <div class="user-dropdown" v-if="showUserMenu" @click.stop>
              <div class="dropdown-item" @click="handleLogout">
                🚪 退出登录
              </div>
            </div>
          </div>
        </div>
      </header>

      <!-- Content -->
      <main class="content">
        <router-view />
      </main>
    </div>
  </div>
</template>

<style scoped>
.layout {
  display: flex;
  min-height: 100vh;
  background: #0f172a;
  color: #e2e8f0;
}

/* Sidebar */
.sidebar {
  width: 220px;
  background: #1e293b;
  border-right: 1px solid rgba(255,255,255,0.06);
  display: flex;
  flex-direction: column;
  flex-shrink: 0;
}
.sidebar-header {
  padding: 20px;
  display: flex;
  align-items: center;
  gap: 10px;
  border-bottom: 1px solid rgba(255,255,255,0.06);
}
.logo { font-size: 24px; }
.title { font-size: 16px; font-weight: 600; color: #f1f5f9; }

.sidebar-nav {
  flex: 1;
  padding: 12px 8px;
  display: flex;
  flex-direction: column;
  gap: 2px;
}
.nav-item {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 10px 14px;
  border-radius: 8px;
  cursor: pointer;
  color: #94a3b8;
  font-size: 14px;
  transition: all 0.15s;
  text-decoration: none;
}
.nav-item:hover { background: rgba(255,255,255,0.06); color: #e2e8f0; }
.nav-item.active { background: #3b82f6; color: white; }
.nav-icon { font-size: 16px; width: 22px; text-align: center; }

/* Header */
.main { flex: 1; display: flex; flex-direction: column; min-width: 0; }
.header {
  height: 56px;
  padding: 0 24px;
  display: flex;
  align-items: center;
  justify-content: space-between;
  border-bottom: 1px solid rgba(255,255,255,0.06);
  background: #1e293b;
  flex-shrink: 0;
}
.header h2 { font-size: 16px; font-weight: 500; margin: 0; }
.header-right { display: flex; align-items: center; gap: 12px; }

.lang-btn {
  padding: 4px 10px;
  background: rgba(255,255,255,0.08);
  border: 1px solid rgba(255,255,255,0.1);
  border-radius: 6px;
  color: #94a3b8;
  cursor: pointer;
  font-size: 12px;
}
.lang-btn:hover { background: rgba(255,255,255,0.12); }

.user-info {
  display: flex;
  align-items: center;
  gap: 8px;
  cursor: pointer;
  padding: 4px 8px;
  border-radius: 6px;
  position: relative;
}
.user-info:hover { background: rgba(255,255,255,0.06); }
.avatar {
  width: 28px; height: 28px;
  background: #3b82f6;
  border-radius: 50%;
  display: flex;
  align-items: center;
  justify-content: center;
  font-size: 13px;
  font-weight: 600;
  color: white;
}
.username { font-size: 13px; color: #cbd5e1; }
.arrow { font-size: 10px; color: #64748b; }

.user-dropdown {
  position: absolute;
  top: 100%;
  right: 0;
  margin-top: 4px;
  background: #1e293b;
  border: 1px solid rgba(255,255,255,0.1);
  border-radius: 8px;
  box-shadow: 0 10px 30px rgba(0,0,0,0.3);
  min-width: 140px;
  z-index: 100;
  overflow: hidden;
}
.dropdown-item {
  padding: 10px 16px;
  cursor: pointer;
  font-size: 13px;
  color: #cbd5e1;
}
.dropdown-item:hover { background: rgba(255,255,255,0.06); }

/* Content */
.content {
  flex: 1;
  padding: 24px;
  overflow-y: auto;
}
</style>
