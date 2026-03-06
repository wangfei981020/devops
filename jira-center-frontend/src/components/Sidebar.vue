<template>
  <aside class="sidebar" :class="{ collapsed }">
    <div class="sidebar-header" @click="$emit('toggle')">
      <div class="logo" v-if="!collapsed">
        <svg width="24" height="24" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M4 15s1-1 4-1 5 2 8 2 4-1 4-1V3s-1 1-4 1-5-2-8-2-4 1-4 1z"/><line x1="4" x2="4" y1="22" y2="15"/></svg>
        <span class="logo-text">Jira Center</span>
      </div>
      <div class="logo-icon" v-else>
        <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M4 15s1-1 4-1 5 2 8 2 4-1 4-1V3s-1 1-4 1-5-2-8-2-4 1-4 1z"/><line x1="4" x2="4" y1="22" y2="15"/></svg>
      </div>
    </div>

    <nav class="sidebar-nav">
      <router-link
        v-for="item in menuItems"
        :key="item.path"
        :to="item.path"
        class="nav-item"
        :class="{ active: $route.path === item.path || ($route.path.startsWith(item.path) && item.path !== '/dashboard') }"
      >
        <component :is="item.icon" :size="20" />
        <span v-if="!collapsed" class="nav-label">{{ item.label }}</span>
      </router-link>
    </nav>

    <div class="sidebar-footer" v-if="!collapsed">
      <span class="version">v1</span>
    </div>
  </aside>
</template>

<script setup>
import { computed } from 'vue'
import { useAuthStore } from '@/stores/auth'
import { LayoutDashboard, FolderKanban, ListTodo, BarChart3, FileText, Settings } from 'lucide-vue-next'

defineProps({ collapsed: Boolean })
defineEmits(['toggle'])

const authStore = useAuthStore()

const menuItems = computed(() => {
  const items = [
    { path: '/dashboard', label: '仪表盘', icon: LayoutDashboard },
    { path: '/projects', label: '项目列表', icon: FolderKanban },
    { path: '/issues', label: '工单列表', icon: ListTodo },
    { path: '/stats', label: '统计分析', icon: BarChart3 },
    { path: '/report', label: '项目报告', icon: FileText },
  ]
  if (authStore.isAdmin) {
    items.push({ path: '/settings', label: '系统设置', icon: Settings })
  }
  return items
})
</script>

<style scoped>
.sidebar {
  position: fixed;
  left: 0;
  top: 0;
  bottom: 0;
  width: 240px;
  background: var(--sidebar-bg);
  border-right: 1px solid var(--border);
  display: flex;
  flex-direction: column;
  transition: width var(--transition-slow);
  z-index: 100;
  overflow: hidden;
}
.sidebar.collapsed { width: 64px; }

.sidebar-header {
  display: flex;
  align-items: center;
  height: 56px;
  padding: 0 16px;
  cursor: pointer;
  border-bottom: 1px solid var(--border);
  flex-shrink: 0;
}

.logo {
  display: flex;
  align-items: center;
  gap: 10px;
  color: var(--primary-light);
}
.logo-text {
  font-size: 16px;
  font-weight: 700;
  white-space: nowrap;
}
.logo-icon {
  display: flex;
  align-items: center;
  justify-content: center;
  width: 32px;
  color: var(--primary-light);
}

.sidebar-nav {
  flex: 1;
  padding: 8px;
  overflow-y: auto;
}

.nav-item {
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 10px 12px;
  border-radius: var(--radius);
  color: var(--sidebar-text);
  text-decoration: none;
  font-size: 14px;
  font-weight: 500;
  transition: all var(--transition);
  margin-bottom: 2px;
  cursor: pointer;
}
.nav-item:hover {
  background: var(--sidebar-active);
  color: var(--sidebar-text-active);
}
.nav-item.active {
  background: var(--sidebar-active);
  color: var(--sidebar-text-active);
}

.collapsed .nav-item {
  justify-content: center;
  padding: 10px;
}

.nav-label { white-space: nowrap; }

.sidebar-footer {
  padding: 12px 16px;
  border-top: 1px solid var(--border);
  flex-shrink: 0;
}
.version {
  font-size: 12px;
  color: var(--text-muted);
  font-family: var(--font-mono);
}
</style>
