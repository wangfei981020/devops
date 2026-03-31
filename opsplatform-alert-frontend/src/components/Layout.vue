<template>
  <div class="app-layout">
    <aside class="sidebar">
      <div class="sidebar-header">
        <h2>Log Alert</h2>
        <div class="subtitle">告警管理平台</div>
      </div>
      <nav class="sidebar-nav">
        <router-link v-if="auth.hasMenu('dashboard')" to="/dashboard" class="nav-item" :class="{ active: $route.path === '/dashboard' }">
          <LayoutDashboard :size="18" /> 仪表盘
        </router-link>
        <router-link v-if="auth.hasMenu('rules')" to="/alert-rules" class="nav-item" :class="{ active: $route.path.startsWith('/alert-rules') }">
          <Bell :size="18" /> 告警规则
        </router-link>
        <router-link v-if="auth.hasMenu('explore')" to="/es-explore" class="nav-item" :class="{ active: $route.path === '/es-explore' }">
          <Search :size="18" /> 日志查询
        </router-link>
        <router-link v-if="auth.hasMenu('connections')" to="/es-connections" class="nav-item" :class="{ active: $route.path === '/es-connections' }">
          <Database :size="18" /> ES 连接
        </router-link>
        <router-link v-if="auth.hasMenu('connections')" to="/loki-connections" class="nav-item" :class="{ active: $route.path === '/loki-connections' }">
          <Database :size="18" /> Loki 连接
        </router-link>
        <router-link v-if="auth.hasMenu('lark')" to="/lark-configs" class="nav-item" :class="{ active: $route.path === '/lark-configs' }">
          <Send :size="18" /> Lark 配置
        </router-link>
        <router-link v-if="auth.hasMenu('logs')" to="/alert-logs" class="nav-item" :class="{ active: $route.path === '/alert-logs' }">
          <FileText :size="18" /> 告警日志
        </router-link>
        <router-link v-if="auth.hasMenu('contacts')" to="/contacts" class="nav-item" :class="{ active: $route.path === '/contacts' }">
          <UserPlus :size="18" /> 通知人管理
        </router-link>
        <router-link v-if="auth.hasMenu('audit')" to="/audit-logs" class="nav-item" :class="{ active: $route.path === '/audit-logs' }">
          <ClipboardList :size="18" /> 操作日志
        </router-link>
        <router-link v-if="auth.hasMenu('users')" to="/users" class="nav-item" :class="{ active: $route.path === '/users' }">
          <Users :size="18" /> 账号管理
        </router-link>
      </nav>
      <div class="sidebar-footer">
        <div>{{ auth.user?.username }}</div>
      </div>
    </aside>
    <div class="main-content">
      <header class="topbar">
        <div style="font-weight: 600;">{{ pageTitle }}</div>
        <div class="flex items-center gap-3">
          <span v-if="auth.user?.auth_source === 'portal'" class="portal-badge">SSO</span>
          <span style="font-weight: 500;">{{ auth.user?.username }}</span>
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
import { computed, onMounted } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useAuthStore } from '../stores/auth'
import { LayoutDashboard, Bell, Search, Database, Send, FileText, UserPlus, Users, ClipboardList, LogOut } from 'lucide-vue-next'

const route = useRoute()
const router = useRouter()
const auth = useAuthStore()

const pageTitle = computed(() => {
  const map = {
    '/dashboard': '仪表盘',
    '/alert-rules': '告警规则',
    '/es-explore': '日志查询',
    '/es-connections': 'ES 连接管理',
    '/loki-connections': 'Loki 连接管理',
    '/lark-configs': 'Lark 配置',
    '/alert-logs': '告警日志',
    '/contacts': '通知人管理',
    '/audit-logs': '操作日志',
    '/users': '账号管理'
  }
  for (const [path, title] of Object.entries(map)) {
    if (route.path.startsWith(path)) return title
  }
  return 'Log Alert 告警平台'
})

onMounted(() => {
  auth.refreshPermissions()
})

async function handleLogout() {
  await auth.logout()
  router.push('/login')
}
</script>

<style scoped>
.portal-badge {
  background: rgba(16, 185, 129, 0.15);
  color: #10b981;
  padding: 2px 8px;
  border-radius: 4px;
  font-size: 11px;
  font-weight: 600;
  letter-spacing: 0.5px;
}
</style>
