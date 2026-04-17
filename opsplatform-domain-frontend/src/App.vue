<template>
  <!-- 登录页不显示侧边栏 -->
  <router-view v-if="$route.path === '/login'" />

  <!-- 主布局：左侧边栏 + 右侧内容 -->
  <div v-else class="app-layout">
    <aside class="sidebar">
      <!-- Logo -->
      <div class="sidebar-header">
        <svg width="26" height="26" viewBox="0 0 40 40" fill="none">
          <circle cx="20" cy="20" r="20" fill="#3b82f6"/>
          <path d="M10 20 Q15 10 20 20 Q25 30 30 20" stroke="white" stroke-width="2.5" fill="none" stroke-linecap="round"/>
          <circle cx="10" cy="20" r="2.5" fill="white"/>
          <circle cx="30" cy="20" r="2.5" fill="white"/>
        </svg>
        <span class="sidebar-title">域名管理平台</span>
      </div>

      <!-- 菜单 -->
      <nav class="sidebar-nav">
        <router-link to="/domains" class="nav-item">
          <svg class="nav-icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8">
            <rect x="3" y="3" width="7" height="7" rx="1.5"/>
            <rect x="14" y="3" width="7" height="7" rx="1.5"/>
            <rect x="3" y="14" width="7" height="7" rx="1.5"/>
            <rect x="14" y="14" width="7" height="7" rx="1.5"/>
          </svg>
          <span>域名列表</span>
        </router-link>

        <router-link to="/settings" class="nav-item">
          <svg class="nav-icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8">
            <circle cx="12" cy="12" r="3"/>
            <path d="M19.4 15a1.65 1.65 0 00.33 1.82l.06.06a2 2 0 010 2.83 2 2 0 01-2.83 0l-.06-.06a1.65 1.65 0 00-1.82-.33 1.65 1.65 0 00-1 1.51V21a2 2 0 01-4 0v-.09A1.65 1.65 0 009 19.4a1.65 1.65 0 00-1.82.33l-.06.06a2 2 0 01-2.83-2.83l.06-.06A1.65 1.65 0 004.68 15a1.65 1.65 0 00-1.51-1H3a2 2 0 010-4h.09A1.65 1.65 0 004.6 9a1.65 1.65 0 00-.33-1.82l-.06-.06a2 2 0 012.83-2.83l.06.06A1.65 1.65 0 009 4.68a1.65 1.65 0 001-1.51V3a2 2 0 014 0v.09a1.65 1.65 0 001 1.51 1.65 1.65 0 001.82-.33l.06-.06a2 2 0 012.83 2.83l-.06.06A1.65 1.65 0 0019.4 9a1.65 1.65 0 001.51 1H21a2 2 0 010 4h-.09a1.65 1.65 0 00-1.51 1z"/>
          </svg>
          <span>系统设置</span>
        </router-link>
      </nav>
    </aside>

    <!-- 右侧：顶部 header + 内容区 -->
    <div class="right-area">
      <!-- 顶部 header -->
      <header class="top-header">
        <span class="page-title">{{ pageTitle }}</span>
        <div class="header-right">
          <span class="username">
            <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8">
              <path d="M20 21v-2a4 4 0 00-4-4H8a4 4 0 00-4 4v2"/>
              <circle cx="12" cy="7" r="4"/>
            </svg>
            admin
          </span>
          <button class="btn-logout" @click="handleLogout">退出</button>
        </div>
      </header>

      <!-- 内容区 -->
      <main class="main-area">
        <router-view />
      </main>
    </div>
  </div>
</template>

<script setup>
import { computed } from 'vue'
import { useRouter, useRoute } from 'vue-router'

const router = useRouter()
const route = useRoute()

const pageTitle = computed(() => {
  const map = { '/domains': '域名列表', '/settings': '系统设置' }
  return map[route.path] || '域名管理平台'
})

function handleLogout() {
  localStorage.removeItem('domain_token')
  router.push('/login')
}
</script>

<style>
* {
  box-sizing: border-box;
  margin: 0;
  padding: 0;
}

body {
  font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, 'Helvetica Neue', Arial, sans-serif;
  background: #f0f2f5;
  color: #333;
}

.app-layout {
  display: flex;
  min-height: 100vh;
}

/* ===== 侧边栏 ===== */
.sidebar {
  width: 220px;
  flex-shrink: 0;
  background: #0f172a;
  display: flex;
  flex-direction: column;
  position: fixed;
  left: 0;
  top: 0;
  bottom: 0;
  z-index: 100;
}

.sidebar-header {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 20px 18px 18px;
  border-bottom: 1px solid rgba(255,255,255,0.07);
}

.sidebar-title {
  color: #f1f5f9;
  font-size: 14px;
  font-weight: 700;
  letter-spacing: 0.3px;
  line-height: 1.3;
}

/* 导航菜单 */
.sidebar-nav {
  flex: 1;
  padding: 12px 10px;
  display: flex;
  flex-direction: column;
  gap: 2px;
}

.nav-item {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 10px 12px;
  border-radius: 8px;
  color: #94a3b8;
  text-decoration: none;
  font-size: 13.5px;
  font-weight: 500;
  transition: all 0.18s;
  cursor: pointer;
}

.nav-item:hover {
  background: rgba(255,255,255,0.07);
  color: #e2e8f0;
}

.nav-item.router-link-active {
  background: rgba(59,130,246,0.18);
  color: #60a5fa;
}

.nav-icon {
  width: 18px;
  height: 18px;
  flex-shrink: 0;
}

/* 底部退出 */
.sidebar-footer {
  padding: 12px 10px;
  border-top: 1px solid rgba(255,255,255,0.07);
}

.btn-logout {
  display: flex;
  align-items: center;
  gap: 10px;
  width: 100%;
  padding: 10px 12px;
  border-radius: 8px;
  background: transparent;
  border: none;
  color: #64748b;
  font-size: 13.5px;
  font-weight: 500;
  cursor: pointer;
  transition: all 0.18s;
  text-align: left;
}

.btn-logout:hover {
  background: rgba(239,68,68,0.12);
  color: #f87171;
}

/* ===== 右侧区域 ===== */
.right-area {
  margin-left: 220px;
  width: calc(100vw - 220px);
  flex: 1;
  min-width: 0;
  display: flex;
  flex-direction: column;
  min-height: 100vh;
  overflow-x: hidden;
}

/* 顶部 header */
.top-header {
  height: 48px;
  background: #0f172a;
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 0 24px;
  position: sticky;
  top: 0;
  z-index: 99;
  border-bottom: 1px solid rgba(255,255,255,0.06);
}

.page-title {
  color: #f1f5f9;
  font-size: 15px;
  font-weight: 600;
}

.header-right {
  display: flex;
  align-items: center;
  gap: 16px;
}

.username {
  display: flex;
  align-items: center;
  gap: 6px;
  color: #94a3b8;
  font-size: 13px;
}

.btn-logout {
  background: transparent;
  border: 1px solid #334155;
  color: #94a3b8;
  padding: 4px 14px;
  border-radius: 6px;
  font-size: 13px;
  cursor: pointer;
  transition: all 0.18s;
}

.btn-logout:hover {
  border-color: #ef4444;
  color: #ef4444;
}

/* 内容区 */
.main-area {
  flex: 1;
  background: #f0f2f5;
  min-width: 0;
  overflow-x: hidden;
}
</style>
