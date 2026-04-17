<script setup>
import { ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'

const route = useRoute()
const router = useRouter()

const collapsed = ref(false)

// 初始化折叠状态（从 localStorage 读）
try {
  const saved = localStorage.getItem('deploy_sidebar_collapsed')
  if (saved === '1') collapsed.value = true
} catch (e) {}

function toggleCollapse() {
  collapsed.value = !collapsed.value
  try { localStorage.setItem('deploy_sidebar_collapsed', collapsed.value ? '1' : '0') } catch (e) {}
}

// SVG 图标(Lucide 风格) — 和运维平台保持一致的视觉风格
const iconSvgs = {
  dashboard: '<rect x="3" y="3" width="7" height="7" rx="1"/><rect x="14" y="3" width="7" height="7" rx="1"/><rect x="14" y="14" width="7" height="7" rx="1"/><rect x="3" y="14" width="7" height="7" rx="1"/>',
  deploy: '<circle cx="12" cy="12" r="10"/><polygon points="10 8 16 12 10 16 10 8"/>',
  config: '<line x1="4" y1="21" x2="4" y2="14"/><line x1="4" y1="10" x2="4" y2="3"/><line x1="12" y1="21" x2="12" y2="12"/><line x1="12" y1="8" x2="12" y2="3"/><line x1="20" y1="21" x2="20" y2="16"/><line x1="20" y1="12" x2="20" y2="3"/><line x1="1" y1="14" x2="7" y2="14"/><line x1="9" y1="8" x2="15" y2="8"/><line x1="17" y1="16" x2="23" y2="16"/>',
  notify: '<path d="M18 8A6 6 0 0 0 6 8c0 7-3 9-3 9h18s-3-2-3-9"/><path d="M13.73 21a2 2 0 0 1-3.46 0"/>',
  system: '<path d="M12.22 2h-.44a2 2 0 0 0-2 2v.18a2 2 0 0 1-1 1.73l-.43.25a2 2 0 0 1-2 0l-.15-.08a2 2 0 0 0-2.73.73l-.22.38a2 2 0 0 0 .73 2.73l.15.1a2 2 0 0 1 1 1.72v.51a2 2 0 0 1-1 1.74l-.15.09a2 2 0 0 0-.73 2.73l.22.38a2 2 0 0 0 2.73.73l.15-.08a2 2 0 0 1 2 0l.43.25a2 2 0 0 1 1 1.73V20a2 2 0 0 0 2 2h.44a2 2 0 0 0 2-2v-.18a2 2 0 0 1 1-1.73l.43-.25a2 2 0 0 1 2 0l.15.08a2 2 0 0 0 2.73-.73l.22-.39a2 2 0 0 0-.73-2.73l-.15-.08a2 2 0 0 1-1-1.74v-.5a2 2 0 0 1 1-1.74l.15-.09a2 2 0 0 0 .73-2.73l-.22-.38a2 2 0 0 0-2.73-.73l-.15.08a2 2 0 0 1-2 0l-.43-.25a2 2 0 0 1-1-1.73V4a2 2 0 0 0-2-2z"/><circle cx="12" cy="12" r="3"/>'
}

const menuGroups = [
  {
    key: 'home',
    name: '仪表盘',
    icon: 'dashboard',
    items: [
      { key: 'dashboard', name: '概览', path: '/dashboard' }
    ]
  },
  {
    key: 'deploy',
    name: '部署管理',
    icon: 'deploy',
    items: [
      { key: 'projects', name: '应用管理', path: '/projects' },
      { key: 'secrets', name: 'Secret 管理', path: '/secrets' },
      { key: 'deployments', name: '发布历史', path: '/deployments' }
    ]
  },
  {
    key: 'config',
    name: '配置',
    icon: 'config',
    items: [
      { key: 'chart-templates', name: 'Chart 模板', path: '/chart-templates' },
      { key: 'env-templates', name: '环境变量模板', path: '/env-templates' },
      { key: 'environments', name: '环境字典', path: '/environments' }
    ]
  },
  {
    key: 'notify',
    name: '通知',
    icon: 'notify',
    items: [
      { key: 'contacts', name: '通知人', path: '/contacts' },
      { key: 'lark-configs', name: 'Lark 群', path: '/lark-configs' }
    ]
  },
  {
    key: 'system',
    name: '系统',
    icon: 'system',
    items: [
      { key: 'global-config', name: '全局配置', path: '/global-config' }
    ]
  }
]

const expandedGroups = ref({ home: true, deploy: true, config: false, notify: false, system: false })

// 根据当前路由自动展开对应分组
function autoExpandCurrent() {
  const path = route.path
  for (const group of menuGroups) {
    if (group.items.some(item => path === item.path || path.startsWith(item.path + '/') ||
        (item.path === '/projects' && path.startsWith('/modules')))) {
      expandedGroups.value[group.key] = true
      break
    }
  }
}
autoExpandCurrent()

function toggleGroup(key) {
  if (collapsed.value) {
    // 折叠状态下点击分组展开 sidebar
    collapsed.value = false
    try { localStorage.setItem('deploy_sidebar_collapsed', '0') } catch (e) {}
    expandedGroups.value[key] = true
    return
  }
  expandedGroups.value[key] = !expandedGroups.value[key]
}

function isActive(item) {
  const path = route.path
  if (path === item.path) return true
  if (item.path === '/projects' && (path.startsWith('/projects/') || path.startsWith('/modules'))) return true
  return false
}

function navigateTo(path) {
  router.push(path)
}
</script>

<template>
  <aside class="sidebar" :class="{ collapsed }">
    <div class="sidebar-header">
      <div class="sidebar-logo">
        <svg class="logo-icon" viewBox="0 0 120 32" fill="none" xmlns="http://www.w3.org/2000/svg">
          <defs>
            <linearGradient id="deployLogoGrad" x1="0%" y1="0%" x2="100%" y2="100%">
              <stop offset="0%" style="stop-color:#3b82f6"/>
              <stop offset="50%" style="stop-color:#8b5cf6"/>
              <stop offset="100%" style="stop-color:#06b6d4"/>
            </linearGradient>
            <linearGradient id="deployLogoGrad2" x1="0%" y1="0%" x2="100%" y2="0%">
              <stop offset="0%" style="stop-color:#3b82f6"/>
              <stop offset="100%" style="stop-color:#8b5cf6"/>
            </linearGradient>
          </defs>
          <path d="M8 20c-2.2 0-4-1.8-4-4 0-1.9 1.3-3.4 3-3.9C7.2 9.8 9.4 8 12 8c2.2 0 4.1 1.2 5.2 3 .3 0 .5-.1.8-.1 2.2 0 4 1.8 4 4s-1.8 4-4 4H8z" fill="url(#deployLogoGrad)" opacity="0.9"/>
          <g transform="translate(12, 10)">
            <polygon points="0,3 8,3 5,0 5,6" fill="white" opacity="0.95"/>
            <rect x="0" y="7" width="10" height="1.5" fill="white" opacity="0.85"/>
          </g>
          <text v-if="!collapsed" x="30" y="22" font-family="system-ui, -apple-system, sans-serif" font-size="14" font-weight="700" fill="url(#deployLogoGrad2)">Deploy Center</text>
        </svg>
      </div>
      <button class="collapse-btn" @click="toggleCollapse" :title="collapsed ? '展开' : '折叠'">
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
          <path v-if="collapsed" d="M9 18l6-6-6-6"/>
          <path v-else d="M15 18l-6-6 6-6"/>
        </svg>
      </button>
    </div>

    <nav class="sidebar-nav">
      <div class="nav-group" v-for="group in menuGroups" :key="group.key">
        <button
          class="nav-group-header"
          :class="{ expanded: expandedGroups[group.key] && !collapsed, 'group-active': group.items.some(isActive) }"
          @click="toggleGroup(group.key)"
          :title="collapsed ? group.name : ''"
        >
          <span class="nav-group-icon">
            <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round" v-html="iconSvgs[group.icon] || ''"></svg>
          </span>
          <span v-if="!collapsed" class="nav-group-title">{{ group.name }}</span>
          <span v-if="!collapsed" class="nav-group-arrow" :class="{ expanded: expandedGroups[group.key] }">
            <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
              <path d="M9 5l7 7-7 7"/>
            </svg>
          </span>
        </button>

        <div class="nav-sub-menu" v-show="!collapsed && expandedGroups[group.key]">
          <button
            v-for="item in group.items"
            :key="item.key"
            class="nav-item sub"
            :class="{ active: isActive(item) }"
            @click="navigateTo(item.path)"
          >
            {{ item.name }}
          </button>
        </div>
      </div>
    </nav>
  </aside>
</template>

<style scoped>
.sidebar {
  width: 200px;
  height: 100vh;
  background: linear-gradient(180deg, #1e293b 0%, #0f172a 100%);
  display: flex;
  flex-direction: column;
  position: fixed;
  left: 0;
  top: 0;
  z-index: 100;
  overflow-y: auto;
  transition: width 0.2s ease;
}
.sidebar.collapsed { width: 64px; }

.sidebar::-webkit-scrollbar { width: 4px; }
.sidebar::-webkit-scrollbar-thumb { background: rgba(255,255,255,0.1); border-radius: 2px; }

.sidebar-header {
  padding: 16px;
  border-bottom: 1px solid rgba(255, 255, 255, 0.06);
  flex-shrink: 0;
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
}
.sidebar.collapsed .sidebar-header { padding: 16px 8px; justify-content: center; }

.sidebar-logo { display: flex; align-items: center; flex: 1; min-width: 0; overflow: hidden; }
.logo-icon { width: auto; height: 28px; flex-shrink: 0; }

.collapse-btn {
  width: 22px;
  height: 22px;
  background: transparent;
  border: none;
  color: rgba(255,255,255,0.55);
  cursor: pointer;
  padding: 0;
  display: flex;
  align-items: center;
  justify-content: center;
  border-radius: 4px;
  flex-shrink: 0;
}
.collapse-btn:hover { color: #fff; background: rgba(255,255,255,0.08); }
.collapse-btn svg { width: 14px; height: 14px; }

.sidebar-nav { flex: 1; padding: 8px 0; }
.nav-group { margin-bottom: 2px; }

.nav-group-header {
  width: 100%;
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 10px 16px;
  background: transparent;
  border: none;
  color: rgba(255, 255, 255, 0.7);
  font-size: 13px;
  font-weight: 500;
  cursor: pointer;
  transition: all 0.15s;
  text-align: left;
}
.sidebar.collapsed .nav-group-header { justify-content: center; padding: 12px 0; }

.nav-group-header:hover { color: #fff; background: rgba(255, 255, 255, 0.05); }
.nav-group-header.group-active { color: #fff; }

.nav-group-icon {
  width: 20px; height: 20px;
  display: flex; align-items: center; justify-content: center;
  flex-shrink: 0;
}
.nav-group-icon svg { width: 18px; height: 18px; }

.nav-group-title { flex: 1; }

.nav-group-arrow {
  width: 16px; height: 16px;
  transition: transform 0.2s;
  opacity: 0.5;
}
.nav-group-arrow svg { width: 12px; height: 12px; }
.nav-group-arrow.expanded { transform: rotate(90deg); }

.nav-sub-menu { padding-left: 16px; }

.nav-item.sub {
  width: 100%;
  display: block;
  padding: 9px 16px 9px 30px;
  background: transparent;
  border: none;
  color: rgba(255, 255, 255, 0.55);
  font-size: 13px;
  cursor: pointer;
  transition: all 0.15s;
  text-align: left;
  border-radius: 0;
  position: relative;
}
.nav-item.sub:hover { color: rgba(255, 255, 255, 0.9); background: rgba(59, 130, 246, 0.08); }
.nav-item.sub.active {
  color: #fff;
  background: linear-gradient(90deg, #3b82f6 0%, #2563eb 100%);
  border-radius: 6px 0 0 6px;
  margin-right: 0;
  font-weight: 500;
}
</style>
