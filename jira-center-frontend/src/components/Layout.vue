<template>
  <div class="layout">
    <Sidebar :collapsed="sidebarCollapsed" @toggle="sidebarCollapsed = !sidebarCollapsed" />
    <div class="layout-main" :class="{ collapsed: sidebarCollapsed }">
      <header class="topbar">
        <h2 class="page-title">{{ $route.meta.title || '' }}</h2>
        <div class="topbar-right">
          <button class="btn btn-sm" @click="theme.toggle()" :title="theme.current.value === 'dark' ? '切换亮色' : '切换暗色'">
            {{ theme.current.value === 'dark' ? '☀' : '☾' }}
          </button>
          <span class="user-name">{{ authStore.displayName }}</span>
          <button class="btn btn-sm" @click="handleLogout">退出</button>
        </div>
      </header>
      <main class="content">
        <router-view />
      </main>
    </div>
  </div>
</template>

<script setup>
import { ref, inject } from 'vue'
import { useRouter } from 'vue-router'
import { useAuthStore } from '@/stores/auth'
import Sidebar from './Sidebar.vue'

const authStore = useAuthStore()
const router = useRouter()
const theme = inject('theme')
const sidebarCollapsed = ref(false)

async function handleLogout() {
  await authStore.logout()
  router.push('/login')
}
</script>

<style scoped>
.layout {
  display: flex;
  height: 100vh;
  overflow: hidden;
}

.layout-main {
  flex: 1;
  display: flex;
  flex-direction: column;
  margin-left: 240px;
  transition: margin-left var(--transition-slow);
  overflow: hidden;
}
.layout-main.collapsed { margin-left: 64px; }

.topbar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  height: 56px;
  padding: 0 24px;
  background: var(--bg-secondary);
  border-bottom: 1px solid var(--border);
  flex-shrink: 0;
}

.page-title {
  font-size: 16px;
  font-weight: 600;
  color: var(--text-primary);
}

.topbar-right {
  display: flex;
  align-items: center;
  gap: 12px;
}

.user-name {
  font-size: 14px;
  color: var(--text-secondary);
}

.content {
  flex: 1;
  padding: 24px;
  overflow-y: auto;
}
</style>
