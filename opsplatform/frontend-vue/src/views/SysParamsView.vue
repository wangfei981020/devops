<script setup>
import { ref, computed } from 'vue'
import { useRouter } from 'vue-router'
import { useAppStore } from '@/stores'

const router = useRouter()
const appStore = useAppStore()

const searchQuery = ref('')

const settingSections = [
  {
    key: 'basic',
    title: '基础设置',
    icon: 'settings',
    items: [
      { key: 'logo-brand', title: 'Logo 与品牌', desc: '平台 Logo、名称、主题色彩', icon: 'image', color: 'blue', status: 'dev' },
      { key: 'session', title: '会话设置', desc: '登录超时、会话刷新策略', icon: 'clock', color: 'orange', status: 'dev' },
      { key: 'locale', title: '语言与时区', desc: '系统语言、默认时区配置', icon: 'globe', color: 'teal', status: 'dev' }
    ]
  },
  {
    key: 'auth',
    title: '认证与集成',
    icon: 'login',
    items: [
      { key: 'sso', title: 'SSO 单点登录', desc: 'OIDC/OAuth2.0 身份提供商', icon: 'login', color: 'purple', status: 'ready', path: '/sso-config' },
      { key: 'ldap', title: 'LDAP 集成', desc: '企业目录服务、用户同步', icon: 'users', color: 'cyan', status: 'dev' },
      { key: 'mfa', title: 'MFA 多因素认证', desc: 'TOTP、短信验证码配置', icon: 'shield', color: 'indigo', status: 'ready' },
      { key: 'webhook', title: 'Webhook 配置', desc: '事件回调、第三方集成', icon: 'link', color: 'pink', status: 'dev' }
    ]
  },
  {
    key: 'notify',
    title: '通知与告警',
    icon: 'bell',
    items: [
      { key: 'email', title: '邮件服务', desc: 'SMTP 服务器、发件配置', icon: 'mail', color: 'green', status: 'dev' },
      { key: 'lark', title: 'Lark 飞书', desc: '飞书机器人通知推送', icon: 'message', color: 'blue', status: 'dev' },
      { key: 'telegram', title: 'Telegram', desc: 'Telegram Bot 告警推送', icon: 'send', color: 'sky', status: 'dev' },
      { key: 'alert-rules', title: '告警规则', desc: '全局告警阈值、降噪策略', icon: 'alert', color: 'amber', status: 'dev' }
    ]
  },
  {
    key: 'datasource',
    title: '数据源与存储',
    icon: 'database',
    items: [
      { key: 'datasources', title: '数据源配置', desc: 'MySQL、Redis、Prometheus', icon: 'database', color: 'blue', status: 'ready', path: '/datasources' },
      { key: 'object-storage', title: '对象存储', desc: 'S3/OSS/MinIO 配置', icon: 'box', color: 'rose', status: 'dev' },
      { key: 'log-storage', title: '日志存储', desc: 'ES/Loki 索引配置', icon: 'file', color: 'slate', status: 'dev' }
    ]
  },
  {
    key: 'k8s',
    title: 'K8S与容器',
    icon: 'layers',
    items: [
      { key: 'clusters', title: '集群管理', desc: 'K8S 集群连接、kubeconfig', icon: 'layers', color: 'cyan', status: 'ready' },
      { key: 'registry', title: '镜像仓库', desc: 'Harbor/Docker Registry', icon: 'package', color: 'purple', status: 'dev' },
      { key: 'cicd', title: 'CI/CD 配置', desc: 'Jenkins/GitLab Runner', icon: 'code', color: 'green', status: 'dev' }
    ]
  },
  {
    key: 'security',
    title: '安全与审计',
    icon: 'shield',
    items: [
      { key: 'password-policy', title: '密码策略', desc: '复杂度、过期时间、历史限制', icon: 'lock', color: 'red', status: 'dev' },
      { key: 'ip-whitelist', title: 'IP 白名单', desc: '登录 IP 限制、访问控制', icon: 'shield-check', color: 'orange', status: 'dev' },
      { key: 'audit-settings', title: '审计设置', desc: '日志保留、敏感操作记录', icon: 'file-text', color: 'indigo', status: 'ready' }
    ]
  },
  {
    key: 'backup',
    title: '备份与恢复',
    icon: 'archive',
    items: [
      { key: 'auto-backup', title: '自动备份', desc: '定时备份策略、保留周期', icon: 'archive', color: 'blue', status: 'dev' },
      { key: 'restore', title: '数据恢复', desc: '快速恢复、备份历史', icon: 'refresh', color: 'teal', status: 'dev' },
      { key: 'system-monitor', title: '系统监控', desc: '资源使用率、健康检查', icon: 'monitor', color: 'cyan', status: 'dev' }
    ]
  }
]

const stats = computed(() => {
  let total = 0, ready = 0, dev = 0
  settingSections.forEach(s => {
    s.items.forEach(item => {
      total++
      if (item.status === 'ready') ready++
      else dev++
    })
  })
  return { total, ready, dev }
})

const filteredSections = computed(() => {
  if (!searchQuery.value.trim()) return settingSections
  const q = searchQuery.value.toLowerCase()
  return settingSections.map(section => ({
    ...section,
    items: section.items.filter(item => 
      item.title.toLowerCase().includes(q) || 
      item.desc.toLowerCase().includes(q)
    )
  })).filter(s => s.items.length > 0)
})

function handleCardClick(item) {
  if (item.path) {
    router.push(item.path)
  } else if (item.status === 'ready') {
    appStore.showToast('功能开发中', 'info')
  } else {
    appStore.showToast('功能开发中，敬请期待', 'info')
  }
}

function getIconSvg(name) {
  const icons = {
    'settings': '<circle cx="12" cy="12" r="3"/><path d="M19.4 15a1.65 1.65 0 0 0 .33 1.82l.06.06a2 2 0 0 1 0 2.83 2 2 0 0 1-2.83 0l-.06-.06a1.65 1.65 0 0 0-1.82-.33 1.65 1.65 0 0 0-1 1.51V21a2 2 0 0 1-2 2 2 2 0 0 1-2-2v-.09A1.65 1.65 0 0 0 9 19.4a1.65 1.65 0 0 0-1.82.33l-.06.06a2 2 0 0 1-2.83 0 2 2 0 0 1 0-2.83l.06-.06a1.65 1.65 0 0 0 .33-1.82 1.65 1.65 0 0 0-1.51-1H3a2 2 0 0 1-2-2 2 2 0 0 1 2-2h.09A1.65 1.65 0 0 0 4.6 9a1.65 1.65 0 0 0-.33-1.82l-.06-.06a2 2 0 0 1 0-2.83 2 2 0 0 1 2.83 0l.06.06a1.65 1.65 0 0 0 1.82.33H9a1.65 1.65 0 0 0 1-1.51V3a2 2 0 0 1 2-2 2 2 0 0 1 2 2v.09a1.65 1.65 0 0 0 1 1.51 1.65 1.65 0 0 0 1.82-.33l.06-.06a2 2 0 0 1 2.83 0 2 2 0 0 1 0 2.83l-.06.06a1.65 1.65 0 0 0-.33 1.82V9a1.65 1.65 0 0 0 1.51 1H21a2 2 0 0 1 2 2 2 2 0 0 1-2 2h-.09a1.65 1.65 0 0 0-1.51 1z"/>',
    'image': '<rect x="3" y="3" width="18" height="18" rx="2"/><circle cx="8.5" cy="8.5" r="1.5"/><path d="M21 15l-5-5L5 21"/>',
    'clock': '<circle cx="12" cy="12" r="10"/><polyline points="12 6 12 12 16 14"/>',
    'globe': '<circle cx="12" cy="12" r="10"/><line x1="2" y1="12" x2="22" y2="12"/><path d="M12 2a15.3 15.3 0 0 1 4 10 15.3 15.3 0 0 1-4 10 15.3 15.3 0 0 1-4-10 15.3 15.3 0 0 1 4-10z"/>',
    'login': '<path d="M15 3h4a2 2 0 0 1 2 2v14a2 2 0 0 1-2 2h-4"/><polyline points="10 17 15 12 10 7"/><line x1="15" y1="12" x2="3" y2="12"/>',
    'users': '<path d="M17 21v-2a4 4 0 0 0-4-4H5a4 4 0 0 0-4 4v2"/><circle cx="9" cy="7" r="4"/><path d="M23 21v-2a4 4 0 0 0-3-3.87"/><path d="M16 3.13a4 4 0 0 1 0 7.75"/>',
    'shield': '<path d="M12 22s8-4 8-10V5l-8-3-8 3v7c0 6 8 10 8 10z"/>',
    'shield-check': '<path d="M12 22s8-4 8-10V5l-8-3-8 3v7c0 6 8 10 8 10z"/><path d="M9 12l2 2 4-4"/>',
    'link': '<path d="M10 13a5 5 0 0 0 7.54.54l3-3a5 5 0 0 0-7.07-7.07l-1.72 1.71"/><path d="M14 11a5 5 0 0 0-7.54-.54l-3 3a5 5 0 0 0 7.07 7.07l1.71-1.71"/>',
    'bell': '<path d="M18 8A6 6 0 0 0 6 8c0 7-3 9-3 9h18s-3-2-3-9"/><path d="M13.73 21a2 2 0 0 1-3.46 0"/>',
    'mail': '<path d="M4 4h16c1.1 0 2 .9 2 2v12c0 1.1-.9 2-2 2H4c-1.1 0-2-.9-2-2V6c0-1.1.9-2 2-2z"/><polyline points="22,6 12,13 2,6"/>',
    'message': '<path d="M21 11.5a8.38 8.38 0 0 1-.9 3.8 8.5 8.5 0 0 1-7.6 4.7 8.38 8.38 0 0 1-3.8-.9L3 21l1.9-5.7a8.38 8.38 0 0 1-.9-3.8 8.5 8.5 0 0 1 4.7-7.6 8.38 8.38 0 0 1 3.8-.9h.5a8.48 8.48 0 0 1 8 8v.5z"/>',
    'send': '<line x1="22" y1="2" x2="11" y2="13"/><polygon points="22 2 15 22 11 13 2 9 22 2"/>',
    'alert': '<circle cx="12" cy="12" r="10"/><line x1="12" y1="8" x2="12" y2="12"/><line x1="12" y1="16" x2="12.01" y2="16"/>',
    'database': '<ellipse cx="12" cy="5" rx="9" ry="3"/><path d="M21 12c0 1.66-4 3-9 3s-9-1.34-9-3"/><path d="M3 5v14c0 1.66 4 3 9 3s9-1.34 9-3V5"/>',
    'box': '<path d="M21 16V8a2 2 0 0 0-1-1.73l-7-4a2 2 0 0 0-2 0l-7 4A2 2 0 0 0 3 8v8a2 2 0 0 0 1 1.73l7 4a2 2 0 0 0 2 0l7-4A2 2 0 0 0 21 16z"/><polyline points="3.27 6.96 12 12.01 20.73 6.96"/><line x1="12" y1="22.08" x2="12" y2="12"/>',
    'file': '<path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8z"/><polyline points="14 2 14 8 20 8"/><line x1="16" y1="13" x2="8" y2="13"/><line x1="16" y1="17" x2="8" y2="17"/>',
    'file-text': '<path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8z"/><polyline points="14 2 14 8 20 8"/><line x1="16" y1="13" x2="8" y2="13"/><line x1="16" y1="17" x2="8" y2="17"/><polyline points="10 9 9 9 8 9"/>',
    'layers': '<polygon points="12 2 2 7 12 12 22 7 12 2"/><polyline points="2 17 12 22 22 17"/><polyline points="2 12 12 17 22 12"/>',
    'package': '<line x1="16.5" y1="9.4" x2="7.5" y2="4.21"/><path d="M21 16V8a2 2 0 0 0-1-1.73l-7-4a2 2 0 0 0-2 0l-7 4A2 2 0 0 0 3 8v8a2 2 0 0 0 1 1.73l7 4a2 2 0 0 0 2 0l7-4A2 2 0 0 0 21 16z"/><polyline points="3.27 6.96 12 12.01 20.73 6.96"/><line x1="12" y1="22.08" x2="12" y2="12"/>',
    'code': '<polyline points="16 18 22 12 16 6"/><polyline points="8 6 2 12 8 18"/>',
    'lock': '<rect x="3" y="11" width="18" height="11" rx="2" ry="2"/><path d="M7 11V7a5 5 0 0 1 10 0v4"/>',
    'archive': '<polyline points="21 8 21 21 3 21 3 8"/><rect x="1" y="3" width="22" height="5"/><line x1="10" y1="12" x2="14" y2="12"/>',
    'refresh': '<path d="M3 12a9 9 0 0 1 9-9 9.75 9.75 0 0 1 6.74 2.74L21 8"/><path d="M21 3v5h-5"/><path d="M21 12a9 9 0 0 1-9 9 9.75 9.75 0 0 1-6.74-2.74L3 16"/><path d="M8 16H3v5"/>',
    'monitor': '<rect x="2" y="3" width="20" height="14" rx="2" ry="2"/><line x1="8" y1="21" x2="16" y2="21"/><line x1="12" y1="17" x2="12" y2="21"/>'
  }
  return icons[name] || icons['settings']
}
</script>

<template>
  <div class="sys-params-page">
    <div class="page-header">
      <h1>系统参数</h1>
      <p class="page-desc">管理平台的全局配置和系统设置</p>
    </div>

    <!-- 搜索框 -->
    <div class="search-bar">
      <div class="search-wrapper">
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
          <circle cx="11" cy="11" r="8"/><path d="m21 21-4.3-4.3"/>
        </svg>
        <input 
          type="text" 
          class="search-input" 
          v-model="searchQuery"
          placeholder="搜索设置项..."
        >
      </div>
    </div>

    <!-- 统计 -->
    <div class="stats-bar">
      <div class="stat-item">
        <div class="stat-icon blue">
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
            <polygon points="12 2 2 7 12 12 22 7 12 2"/><polyline points="2 17 12 22 22 17"/><polyline points="2 12 12 17 22 12"/>
          </svg>
        </div>
        <div class="stat-info">
          <h4>{{ stats.total }}</h4>
          <span>配置模块</span>
        </div>
      </div>
      <div class="stat-item">
        <div class="stat-icon green">
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
            <path d="M22 11.08V12a10 10 0 1 1-5.93-9.14"/><polyline points="22 4 12 14.01 9 11.01"/>
          </svg>
        </div>
        <div class="stat-info">
          <h4>{{ stats.ready }}</h4>
          <span>已配置</span>
        </div>
      </div>
      <div class="stat-item">
        <div class="stat-icon orange">
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
            <circle cx="12" cy="12" r="10"/><line x1="12" y1="8" x2="12" y2="12"/><line x1="12" y1="16" x2="12.01" y2="16"/>
          </svg>
        </div>
        <div class="stat-info">
          <h4>{{ stats.dev }}</h4>
          <span>待配置</span>
        </div>
      </div>
    </div>

    <!-- 设置分区 -->
    <div 
      v-for="section in filteredSections" 
      :key="section.key" 
      class="settings-section"
    >
      <div class="section-title">
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" v-html="getIconSvg(section.icon)"></svg>
        {{ section.title }}
      </div>
      <div class="settings-grid">
        <div 
          v-for="item in section.items" 
          :key="item.key"
          class="setting-card"
          :class="{ highlight: item.status === 'ready' }"
          @click="handleCardClick(item)"
        >
          <span v-if="item.status === 'ready'" class="card-badge ready">已配置</span>
          <span v-else class="card-badge dev">开发中</span>
          <div class="card-icon" :class="item.color">
            <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" v-html="getIconSvg(item.icon)"></svg>
          </div>
          <div class="card-content">
            <div class="card-title">{{ item.title }}</div>
            <div class="card-desc">{{ item.desc }}</div>
          </div>
        </div>
      </div>
    </div>

    <div v-if="filteredSections.length === 0" class="empty-state">
      <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
        <circle cx="11" cy="11" r="8"/><path d="m21 21-4.3-4.3"/>
      </svg>
      <p>没有找到匹配的设置项</p>
    </div>
  </div>
</template>

<style scoped>
.sys-params-page {
  padding: 0;
}

.page-header {
  margin-bottom: 20px;
}
.page-header h1 {
  font-size: 22px;
  font-weight: 600;
  margin: 0 0 6px;
  color: var(--text-primary);
}
.page-desc {
  margin: 0;
  color: var(--text-muted);
  font-size: 13px;
}

/* 搜索框 */
.search-bar {
  margin-bottom: 20px;
}
.search-wrapper {
  position: relative;
  max-width: 400px;
}
.search-wrapper svg {
  position: absolute;
  left: 12px;
  top: 50%;
  transform: translateY(-50%);
  width: 16px;
  height: 16px;
  color: var(--text-muted);
}
.search-input {
  width: 100%;
  padding: 10px 16px 10px 38px;
  background: var(--bg-card);
  border: 1px solid var(--border-color);
  border-radius: 8px;
  color: var(--text-primary);
  font-size: 13px;
}
.search-input:focus {
  outline: none;
  border-color: var(--primary);
}
.search-input::placeholder {
  color: var(--text-muted);
}

/* 统计 */
.stats-bar {
  display: flex;
  gap: 20px;
  padding: 14px 18px;
  background: var(--bg-card);
  border-radius: 10px;
  border: 1px solid var(--border-color);
  margin-bottom: 24px;
}
.stat-item {
  display: flex;
  align-items: center;
  gap: 10px;
}
.stat-icon {
  width: 34px;
  height: 34px;
  border-radius: 8px;
  display: flex;
  align-items: center;
  justify-content: center;
}
.stat-icon svg { width: 16px; height: 16px; }
.stat-icon.blue { background: rgba(58, 132, 255, 0.15); color: var(--primary); }
.stat-icon.green { background: rgba(45, 204, 164, 0.15); color: #2dcca4; }
.stat-icon.orange { background: rgba(255, 156, 1, 0.15); color: #ff9c01; }
.stat-info h4 { font-size: 16px; font-weight: 600; margin: 0; color: var(--text-primary); }
.stat-info span { font-size: 11px; color: var(--text-muted); }

/* 设置分区 */
.settings-section {
  margin-bottom: 28px;
}
.section-title {
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: 14px;
  font-weight: 500;
  color: var(--text-secondary);
  margin-bottom: 14px;
  padding-left: 2px;
}
.section-title svg {
  width: 16px;
  height: 16px;
  opacity: 0.7;
}

.settings-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(280px, 1fr));
  gap: 14px;
}

.setting-card {
  background: var(--bg-card);
  border: 1px solid var(--border-color);
  border-radius: 10px;
  padding: 16px;
  cursor: pointer;
  transition: all 0.2s;
  display: flex;
  gap: 14px;
  position: relative;
  overflow: hidden;
}
.setting-card:hover {
  border-color: var(--primary);
  background: var(--bg-card-hover);
  transform: translateY(-1px);
  box-shadow: 0 6px 20px var(--shadow-color);
}
.setting-card::after {
  content: '›';
  position: absolute;
  right: 14px;
  top: 50%;
  transform: translateY(-50%);
  font-size: 18px;
  color: var(--text-muted);
  transition: all 0.2s;
}
.setting-card:hover::after {
  color: var(--primary);
  right: 10px;
}

.card-icon {
  width: 42px;
  height: 42px;
  border-radius: 10px;
  display: flex;
  align-items: center;
  justify-content: center;
  flex-shrink: 0;
}
.card-icon svg {
  width: 20px;
  height: 20px;
  color: white;
}
.card-icon.blue { background: linear-gradient(135deg, #3a84ff, #2563eb); }
.card-icon.orange { background: linear-gradient(135deg, #ff9c01, #f97316); }
.card-icon.green { background: linear-gradient(135deg, #2dcca4, #10b981); }
.card-icon.purple { background: linear-gradient(135deg, #8b5cf6, #7c3aed); }
.card-icon.pink { background: linear-gradient(135deg, #ec4899, #db2777); }
.card-icon.cyan { background: linear-gradient(135deg, #06b6d4, #0891b2); }
.card-icon.red { background: linear-gradient(135deg, #ea3636, #dc2626); }
.card-icon.amber { background: linear-gradient(135deg, #fbbf24, #f59e0b); }
.card-icon.indigo { background: linear-gradient(135deg, #6366f1, #4f46e5); }
.card-icon.teal { background: linear-gradient(135deg, #14b8a6, #0d9488); }
.card-icon.rose { background: linear-gradient(135deg, #f43f5e, #e11d48); }
.card-icon.sky { background: linear-gradient(135deg, #0ea5e9, #0284c7); }
.card-icon.slate { background: linear-gradient(135deg, #64748b, #475569); }

.card-content {
  flex: 1;
  min-width: 0;
}
.card-title {
  font-size: 14px;
  font-weight: 600;
  margin-bottom: 4px;
  color: var(--text-primary);
}
.card-desc {
  font-size: 12px;
  color: var(--text-muted);
  line-height: 1.4;
}

.card-badge {
  position: absolute;
  top: 10px;
  right: 10px;
  padding: 2px 6px;
  border-radius: 4px;
  font-size: 10px;
  font-weight: 500;
}
.card-badge.dev {
  background: rgba(251, 191, 36, 0.15);
  color: #fbbf24;
}
.card-badge.ready {
  background: rgba(45, 204, 164, 0.15);
  color: #2dcca4;
}

/* 高亮卡片 */
.setting-card.highlight {
  border-color: var(--primary);
  background: var(--bg-card-highlight);
}
.setting-card.highlight .card-icon {
  box-shadow: 0 4px 12px rgba(58, 132, 255, 0.3);
}

/* 空状态 */
.empty-state {
  text-align: center;
  padding: 60px 20px;
  color: var(--text-muted);
}
.empty-state svg {
  width: 48px;
  height: 48px;
  opacity: 0.5;
  margin-bottom: 16px;
}
.empty-state p {
  margin: 0;
  font-size: 14px;
}

@media (max-width: 768px) {
  .settings-grid {
    grid-template-columns: 1fr;
  }
  .stats-bar {
    flex-wrap: wrap;
  }
}
</style>
