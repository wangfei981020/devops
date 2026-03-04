<script setup>
import { ref, computed } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useAuthStore, useAppStore } from '@/stores'

const route = useRoute()
const router = useRouter()
const authStore = useAuthStore()
const appStore = useAppStore()
const t = (key, params) => appStore.t(key, params)

function getGroupName(groupKey) {
  return t('menu.groups.' + groupKey) || groupKey
}

function getItemName(itemKey) {
  const keyMap = {
    'table-maintenance': 'tableMaintenance',
    'table-hierarchy-config': 'tableHierarchyConfig',
    'alertrules': 'alertRules',
    'alertnotify': 'alertNotify',
    'dashboard': 'dashboardScreen',
    'tickettemplate': 'ticketTemplate',
    'selfhealing': 'selfHealing',
    'rootcause': 'rootCause',
    'smartalert': 'smartAlert',
    'logquery': 'logQuery',
    'loganalysis': 'logAnalysis',
    'logalert': 'logAlert',
    'sysparams': 'sysParams',
    'sso-config': 'ssoConfig'
  }
  const key = keyMap[itemKey] || itemKey
  return t('menu.items.' + key) || itemKey
}

const currentUser = computed(() => authStore.user || JSON.parse(localStorage.getItem('currentUser') || '{}'))

// 响应式权限数据，当 myPermissions 变化时会触发重新计算
const permissions = computed(() => authStore.myPermissions || {})

const isAdmin = computed(() => {
  const user = currentUser.value
  return user?.role === 'admin' || user?.role === 'super_admin' || user?.role === '超级管理员' || authStore.isSuperAdmin()
})

// 使用 computed 依赖 permissions，确保权限变化时菜单更新
function hasMenu(menuCode) {
  if (menuCode === 'welcome') return true
  if (isAdmin.value) return true
  return permissions.value['menu:' + menuCode] === true
}

function hasMenuGroup(groupCode) {
  if (isAdmin.value) return true
  const menuGroupMap = {
    'system': ['welcome', 'users', 'roles', 'permissions', 'audit', 'api-manage', 'schedule', 'task-pool', 'incidents', 'duty', 'table_maintenance', 'table_hierarchy_config'],
    'resource': ['assets', 'domains', 'merchants', 'network', 'service-config', 'topology'],
    'monitor': ['metrics', 'alerts', 'alert-rules', 'alert-notify', 'dashboard-screen'],
    'k8s': ['clusters', 'workloads', 'configmaps', 'storage', 'terminal'],
    'ticket': ['tickets', 'workflow', 'sla', 'ticket-template'],
    'automation': ['jobs', 'crontab', 'inspection', 'self-healing'],
    'aiops': ['anomaly', 'root-cause', 'predict', 'smart-alert', 'capacity'],
    'release': ['deploy', 'change', 'rollback'],
    'logs': ['log-query', 'log-analysis', 'log-alert'],
    'security': ['vault', 'secrets', 'certificates'],
    'settings': ['datasources', 'sys-params', 'settings', 'sso-config']
  }
  const subMenus = menuGroupMap[groupCode] || []
  for (const sub of subMenus) {
    if (sub === 'welcome') return true
    if (permissions.value['menu:' + sub] === true) return true
  }
  return permissions.value['menu:' + groupCode] === true
}

function canShowItem(item) {
  if (item.adminOnly && !isAdmin.value) return false
  return hasMenu(item.perm)
}

const expandedGroups = ref({
  system: true,
  resource: true,
  monitor: false,
  k8s: false,
  ticket: false,
  automation: false,
  aiops: false,
  release: false,
  logs: false,
  security: false,
  settings: false
})

const menuGroups = [
  {
    key: 'system',
    name: '系统管理',
    icon: '🏠',
    items: [
      { key: 'welcome', perm: 'welcome', name: '欢迎页', path: '/welcome' },
      { key: 'users', perm: 'users', name: '用户管理', path: '/users' },
      { key: 'roles', perm: 'roles', name: '角色管理', path: '/roles' },
      { key: 'permissions', perm: 'permissions', name: '权限配置', path: '/permissions' },
      { key: 'audit', perm: 'audit', name: '审计日志', path: '/audit' },
      { key: 'api', perm: 'api-manage', name: '接口管理', path: '/api-manage' },
      { key: 'schedule', perm: 'schedule', name: '排班管理', path: '/schedule' },
      { key: 'taskpool', perm: 'task-pool', name: '任务池', path: '/task-pool' },
      { key: 'incidents', perm: 'incidents', name: '失误记录', path: '/incidents' },
      { key: 'duty', perm: 'duty', name: '值班记录', path: '/duty-records' },
      { key: 'table-maintenance', perm: 'table_maintenance', name: '桌台维护记录', path: '/table-maintenance' },
      { key: 'table-hierarchy-config', perm: 'table_hierarchy_config', name: '桌台配置', path: '/table-hierarchy-config' }
    ]
  },
  {
    key: 'resource',
    name: '资源管理',
    icon: '📦',
    items: [
      { key: 'assets', perm: 'assets', name: '资产管理', path: '/assets' },
      { key: 'domains', perm: 'domains', name: '域名管理', path: '/domains' },
      { key: 'merchants', perm: 'merchants', name: '商户管理', path: '/merchants' },
      { key: 'network', perm: 'network', name: '网络管理', path: '/network' },
      { key: 'serviceconfig', perm: 'service-config', name: '服务配置', path: '/service-config' },
      { key: 'topology', perm: 'topology', name: '网络拓扑', path: '/topology' }
    ]
  },
  {
    key: 'monitor',
    name: '监控告警',
    icon: '📊',
    items: [
      { key: 'metrics', perm: 'metrics', name: '指标监控', path: '/metrics' },
      { key: 'alerts', perm: 'alerts', name: '告警管理', path: '/alerts' },
      { key: 'alertrules', perm: 'alert-rules', name: '告警规则', path: '/alert-rules' },
      { key: 'alertnotify', perm: 'alert-notify', name: '通知配置', path: '/alert-notify' },
      { key: 'dashboard', perm: 'dashboard-screen', name: '大屏展示', path: '/dashboard-screen' }
    ]
  },
  {
    key: 'k8s',
    name: 'K8S运维',
    icon: '☸️',
    items: [
      { key: 'clusters', perm: 'clusters', name: '集群管理', path: '/clusters' },
      { key: 'workloads', perm: 'workloads', name: '工作负载', path: '/workloads' },
      { key: 'configmaps', perm: 'configmaps', name: '配置管理', path: '/configmaps' },
      { key: 'storage', perm: 'storage', name: '存储管理', path: '/storage' },
      { key: 'terminal', perm: 'terminal', name: '容器终端', path: '/terminal' }
    ]
  },
  {
    key: 'ticket',
    name: '工单系统',
    icon: '🎫',
    items: [
      { key: 'tickets', perm: 'tickets', name: '工单管理', path: '/tickets' },
      { key: 'workflow', perm: 'workflow', name: '流程引擎', path: '/workflow' },
      { key: 'sla', perm: 'sla', name: 'SLA管理', path: '/sla' },
      { key: 'tickettemplate', perm: 'ticket-template', name: '工单模板', path: '/ticket-template' }
    ]
  },
  {
    key: 'automation',
    name: '自动化运维',
    icon: '🤖',
    items: [
      { key: 'jobs', perm: 'jobs', name: '作业平台', path: '/jobs' },
      { key: 'crontab', perm: 'crontab', name: '定时任务', path: '/crontab' },
      { key: 'inspection', perm: 'inspection', name: '自动巡检', path: '/inspection' },
      { key: 'selfhealing', perm: 'self-healing', name: '自愈策略', path: '/self-healing' }
    ]
  },
  {
    key: 'aiops',
    name: '智能运维',
    icon: '🧠',
    items: [
      { key: 'anomaly', perm: 'anomaly', name: '异常检测', path: '/anomaly' },
      { key: 'rootcause', perm: 'root-cause', name: '根因分析', path: '/root-cause' },
      { key: 'predict', perm: 'predict', name: '故障预测', path: '/predict' },
      { key: 'smartalert', perm: 'smart-alert', name: '智能告警', path: '/smart-alert' },
      { key: 'capacity', perm: 'capacity', name: '容量预测', path: '/capacity' }
    ]
  },
  {
    key: 'release',
    name: '变更发布',
    icon: '🚀',
    items: [
      { key: 'deploy', perm: 'deploy', name: '发布管理', path: '/deploy' },
      { key: 'change', perm: 'change', name: '变更管理', path: '/change' },
      { key: 'rollback', perm: 'rollback', name: '回滚管理', path: '/rollback' }
    ]
  },
  {
    key: 'logs',
    name: '日志服务',
    icon: '📝',
    items: [
      { key: 'logquery', perm: 'log-query', name: '日志查询', path: '/log-query' },
      { key: 'loganalysis', perm: 'log-analysis', name: '日志分析', path: '/log-analysis' },
      { key: 'logalert', perm: 'log-alert', name: '日志告警', path: '/log-alert' }
    ]
  },
  {
    key: 'security',
    name: '安全工具',
    icon: '🔐',
    items: [
      { key: 'vault', perm: 'vault', name: '密码库', path: '/vault' },
      { key: 'secrets', perm: 'secrets', name: '密钥管理', path: '/secrets' },
      { key: 'certificates', perm: 'certificates', name: '证书管理', path: '/certificates' }
    ]
  },
  {
    key: 'settings',
    name: '系统设置',
    icon: '⚙️',
    items: [
      { key: 'datasources', perm: 'datasources', name: '数据源配置', path: '/datasources' },
      { key: 'sysparams', perm: 'sys-params', name: '系统参数', path: '/sys-params' },
      { key: 'sso-config', perm: 'sso-config', name: 'SSO配置', path: '/sso-config', adminOnly: true }
    ]
  }
]

// 权限变化计数，用于强制触发重新计算
const permissionsKey = computed(() => Object.keys(permissions.value).length)

const filteredMenuGroups = computed(() => {
  // 依赖 permissionsKey 确保权限变化时重新计算
  const _key = permissionsKey.value
  console.log('[Sidebar] 重新计算菜单, 权限数量:', _key)
  return menuGroups.map(group => ({
    ...group,
    items: group.items.filter(item => canShowItem(item))
  })).filter(group => group.items.length > 0 && hasMenuGroup(group.key))
})

function toggleGroup(key) {
  expandedGroups.value[key] = !expandedGroups.value[key]
}

function isActive(path) {
  return route.path === path
}

function navigateTo(path) {
  router.push(path)
}
</script>

<template>
  <aside class="sidebar">
    <div class="sidebar-header">
      <div class="sidebar-logo">
        <svg class="logo-icon" viewBox="0 0 120 32" fill="none" xmlns="http://www.w3.org/2000/svg">
          <defs>
            <linearGradient id="logoGrad" x1="0%" y1="0%" x2="100%" y2="100%">
              <stop offset="0%" style="stop-color:#3b82f6"/>
              <stop offset="50%" style="stop-color:#8b5cf6"/>
              <stop offset="100%" style="stop-color:#06b6d4"/>
            </linearGradient>
            <linearGradient id="logoGrad2" x1="0%" y1="0%" x2="100%" y2="0%">
              <stop offset="0%" style="stop-color:#3b82f6"/>
              <stop offset="100%" style="stop-color:#8b5cf6"/>
            </linearGradient>
          </defs>
          <path d="M8 20c-2.2 0-4-1.8-4-4 0-1.9 1.3-3.4 3-3.9C7.2 9.8 9.4 8 12 8c2.2 0 4.1 1.2 5.2 3 .3 0 .5-.1.8-.1 2.2 0 4 1.8 4 4s-1.8 4-4 4H8z" fill="url(#logoGrad)" opacity="0.9"/>
          <g transform="translate(14, 10)">
            <circle cx="6" cy="6" r="2.5" fill="none" stroke="white" stroke-width="1.5"/>
            <path d="M6 0v2M6 10v2M0 6h2M10 6h2M1.8 1.8l1.4 1.4M8.8 8.8l1.4 1.4M1.8 10.2l1.4-1.4M8.8 3.2l1.4-1.4" stroke="white" stroke-width="1.2" stroke-linecap="round"/>
          </g>
          <text x="30" y="22" font-family="system-ui, -apple-system, sans-serif" font-size="14" font-weight="700" fill="url(#logoGrad2)">AI-CloudOps</text>
        </svg>
      </div>
    </div>

    <nav class="sidebar-nav">
      <div class="nav-group" v-for="group in filteredMenuGroups" :key="group.key">
        <button 
          class="nav-group-header" 
          :class="{ expanded: expandedGroups[group.key] }"
          @click="toggleGroup(group.key)"
        >
          <span class="nav-group-icon">{{ group.icon }}</span>
          <span class="nav-group-title">{{ getGroupName(group.key) }}</span>
          <span class="nav-group-arrow" :class="{ expanded: expandedGroups[group.key] }">
            <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
              <path d="M9 5l7 7-7 7"/>
            </svg>
          </span>
        </button>
        
        <div class="nav-sub-menu" v-show="expandedGroups[group.key]">
          <button 
            v-for="item in group.items" 
            :key="item.key"
            class="nav-item sub" 
            :class="{ active: isActive(item.path) }"
            @click="navigateTo(item.path)"
          >
            {{ getItemName(item.key) }}
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
}

.sidebar::-webkit-scrollbar {
  width: 4px;
}

.sidebar::-webkit-scrollbar-thumb {
  background: rgba(255,255,255,0.1);
  border-radius: 2px;
}

.sidebar-header {
  padding: 16px;
  border-bottom: 1px solid rgba(255, 255, 255, 0.06);
  flex-shrink: 0;
}

.sidebar-logo {
  display: flex;
  align-items: center;
}

.logo-icon {
  width: auto;
  height: 28px;
}

.sidebar-nav {
  flex: 1;
  padding: 8px 0;
}

.nav-group {
  margin-bottom: 2px;
}

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

.nav-group-header:hover {
  color: #fff;
  background: rgba(255, 255, 255, 0.05);
}

.nav-group-icon {
  font-size: 15px;
  width: 20px;
  text-align: center;
  flex-shrink: 0;
}

.nav-group-title {
  flex: 1;
}

.nav-group-arrow {
  width: 16px;
  height: 16px;
  transition: transform 0.2s;
  opacity: 0.5;
}

.nav-group-arrow svg {
  width: 12px;
  height: 12px;
}

.nav-group-arrow.expanded {
  transform: rotate(90deg);
}

.nav-sub-menu {
  padding-left: 16px;
}

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

.nav-item.sub:hover {
  color: rgba(255, 255, 255, 0.9);
  background: rgba(59, 130, 246, 0.08);
}

.nav-item.sub.active {
  color: #fff;
  background: linear-gradient(90deg, #3b82f6 0%, #2563eb 100%);
  border-radius: 6px 0 0 6px;
  margin-right: 0;
  font-weight: 500;
}
</style>
