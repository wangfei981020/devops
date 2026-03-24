<template>
  <div class="app-layout">
    <aside class="sidebar">
      <div class="sidebar-header">
        <h2>ES Alert</h2>
        <div class="subtitle">告警管理平台</div>
      </div>
      <nav class="sidebar-nav">
        <router-link to="/dashboard" class="nav-item" :class="{ active: $route.path === '/dashboard' }">
          <LayoutDashboard :size="18" /> 仪表盘
        </router-link>
        <router-link to="/alert-rules" class="nav-item" :class="{ active: $route.path.startsWith('/alert-rules') }">
          <Bell :size="18" /> 告警规则
        </router-link>
        <router-link to="/es-connections" class="nav-item" :class="{ active: $route.path === '/es-connections' }">
          <Database :size="18" /> ES 连接
        </router-link>
        <router-link to="/lark-configs" class="nav-item" :class="{ active: $route.path === '/lark-configs' }">
          <Send :size="18" /> Lark 配置
        </router-link>
        <router-link to="/alert-logs" class="nav-item" :class="{ active: $route.path === '/alert-logs' }">
          <FileText :size="18" /> 告警日志
        </router-link>
      </nav>
      <div class="sidebar-footer">
        <div>{{ auth.user?.username }}</div>
      </div>
    </aside>
    <div class="main-content">
      <header class="topbar">
        <div style="font-weight: 600;">{{ pageTitle }}</div>
        <div class="flex items-center gap-2">
          <span class="text-sm text-secondary">{{ auth.user?.username }}</span>
          <button class="btn btn-outline btn-sm" @click="handleLogout">
            <LogOut :size="14" /> 退出
          </button>
        </div>
      </header>
      <main class="page-content">
        <router-view />
      </main>
    </div>
  </div>
</template>

<script setup>
import { computed } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useAuthStore } from '../stores/auth'
import { LayoutDashboard, Bell, Database, Send, FileText, LogOut } from 'lucide-vue-next'

const route = useRoute()
const router = useRouter()
const auth = useAuthStore()

const pageTitle = computed(() => {
  const map = {
    '/dashboard': '仪表盘',
    '/alert-rules': '告警规则',
    '/es-connections': 'ES 连接管理',
    '/lark-configs': 'Lark 配置',
    '/alert-logs': '告警日志'
  }
  for (const [path, title] of Object.entries(map)) {
    if (route.path.startsWith(path)) return title
  }
  return 'ES 告警平台'
})

async function handleLogout() {
  await auth.logout()
  router.push('/login')
}
</script>
