<script setup>
import { ref, computed, onMounted } from 'vue'
import { useRouter, useRoute } from 'vue-router'
import { useAuthStore, useAppStore } from '@/stores'
import api from '@/api'

const router = useRouter()
const route = useRoute()
const authStore = useAuthStore()
const appStore = useAppStore()
const t = (key, params) => appStore.t(key, params)

const stats = ref({
  domains: 0,
  expiringDomains: 0,
  merchants: 0,
  activeMerchants: 0,
  users: 0,
  activeUsers: 0
})

const showSSONotice = ref(false)

const currentDate = computed(() => {
  const now = new Date()
  const isEn = appStore.language === 'en-US'
  if (isEn) {
    const weekdays = ['Sunday', 'Monday', 'Tuesday', 'Wednesday', 'Thursday', 'Friday', 'Saturday']
    const months = ['January', 'February', 'March', 'April', 'May', 'June', 'July', 'August', 'September', 'October', 'November', 'December']
    return `${weekdays[now.getDay()]}, ${months[now.getMonth()]} ${now.getDate()}, ${now.getFullYear()}`
  } else {
    const weekdays = ['星期日', '星期一', '星期二', '星期三', '星期四', '星期五', '星期六']
    return `${now.getFullYear()}年${now.getMonth() + 1}月${now.getDate()}日${weekdays[now.getDay()]}`
  }
})

const username = computed(() => {
  if (authStore.user?.display_name || authStore.user?.username) {
    return authStore.user.display_name || authStore.user.username
  }
  const cached = localStorage.getItem('currentUser')
  if (cached) {
    try {
      const user = JSON.parse(cached)
      return user.display_name || user.username || '用户'
    } catch (e) {}
  }
  return '用户'
})

onMounted(async () => {
  // 检查是否通过 SSO 登录
  if (route.query.sso_login === '1') {
    showSSONotice.value = true
    // 清除 URL 参数
    router.replace({ path: '/welcome' })
    // 5秒后自动隐藏
    setTimeout(() => {
      showSSONotice.value = false
    }, 5000)
  }

  try {
    const [domainsRes, merchantsRes, usersRes] = await Promise.all([
      api.get('/api/domains').catch(() => ({ data: [] })),
      api.get('/api/merchants').catch(() => ({ data: [] })),
      api.get('/api/users').catch(() => ({ data: [] }))
    ])
    const domains = Array.isArray(domainsRes.data) ? domainsRes.data : []
    const merchants = Array.isArray(merchantsRes.data) ? merchantsRes.data : []
    const users = Array.isArray(usersRes.data) ? usersRes.data : []
    
    const now = new Date()
    const thirtyDaysLater = new Date(now.getTime() + 30 * 24 * 60 * 60 * 1000)
    
    stats.value = {
      domains: domains.length,
      expiringDomains: domains.filter(d => d.expire_date && new Date(d.expire_date) <= thirtyDaysLater).length,
      merchants: merchants.length,
      activeMerchants: merchants.filter(m => m.status === 'active' || m.status === '运营中').length,
      users: users.length,
      activeUsers: users.filter(u => u.status === 'active').length
    }
  } catch (e) { console.error(e) }
})

function navigateTo(path) {
  router.push(path)
}

function hasPermission(menuCode) {
  return authStore.hasMenu(menuCode)
}

const isAdmin = computed(() => {
  const user = authStore.user
  return user?.role === 'admin' || user?.role === 'super_admin' || authStore.isSuperAdmin()
})

const quickLinks = computed(() => {
  const allLinks = [
    { path: '/domains', perm: 'domains', icon: 'globe', color: 'blue', titleKey: 'welcome.quickLinks.domains', descKey: 'welcome.quickLinks.domainsDesc' },
    { path: '/merchants', perm: 'merchants', icon: 'store', color: 'indigo', titleKey: 'welcome.quickLinks.merchants', descKey: 'welcome.quickLinks.merchantsDesc' },
    { path: '/network', perm: 'network', icon: 'network', color: 'cyan', titleKey: 'welcome.quickLinks.network', descKey: 'welcome.quickLinks.networkDesc' },
    { path: '/users', perm: 'users', icon: 'users', color: 'purple', titleKey: 'welcome.quickLinks.users', descKey: 'welcome.quickLinks.usersDesc' },
    { path: '/audit', perm: 'audit', icon: 'audit', color: 'slate', titleKey: 'welcome.quickLinks.audit', descKey: 'welcome.quickLinks.auditDesc' },
    { path: '/duty-records', perm: 'duty', icon: 'duty', color: 'green', titleKey: 'welcome.quickLinks.duty', descKey: 'welcome.quickLinks.dutyDesc' }
  ]
  if (isAdmin.value) return allLinks
  return allLinks.filter(link => hasPermission(link.perm))
})
</script>

<template>
  <div class="welcome-page">
    <!-- SSO 登录提示 -->
    <transition name="fade">
      <div v-if="showSSONotice" class="sso-notice">
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" width="18" height="18">
          <path d="M12 22s8-4 8-10V5l-8-3-8 3v7c0 6 8 10 8 10z"/>
          <polyline points="9 12 11 14 15 10"/>
        </svg>
        <span>{{ t('welcome.ssoNotice') }}</span>
        <button class="sso-close" @click="showSSONotice = false">
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" width="14" height="14">
            <line x1="18" y1="6" x2="6" y2="18"/><line x1="6" y1="6" x2="18" y2="18"/>
          </svg>
        </button>
      </div>
    </transition>

    <!-- 欢迎卡片 -->
    <div class="welcome-banner">
      <h1 class="welcome-title">{{ t('welcome.greeting', { name: username }) }}</h1>
      <p class="welcome-date">{{ t('welcome.today', { date: currentDate }) }}</p>
    </div>

    <!-- 统计卡片 -->
    <div class="stats-row">
      <div class="stat-card">
        <div class="stat-icon teal">
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
            <circle cx="12" cy="12" r="10"/>
            <line x1="2" y1="12" x2="22" y2="12"/>
            <path d="M12 2a15.3 15.3 0 0 1 4 10 15.3 15.3 0 0 1-4 10 15.3 15.3 0 0 1-4-10 15.3 15.3 0 0 1 4-10z"/>
          </svg>
        </div>
        <div class="stat-content">
          <div class="stat-label">{{ t('welcome.stats.domains') }}</div>
          <div class="stat-value">{{ stats.domains }}</div>
          <div class="stat-sub">{{ t('welcome.stats.expiring') }} {{ stats.expiringDomains }}</div>
        </div>
      </div>

      <div class="stat-card">
        <div class="stat-icon orange">
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
            <path d="M3 9l9-7 9 7v11a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2z"/>
            <polyline points="9 22 9 12 15 12 15 22"/>
          </svg>
        </div>
        <div class="stat-content">
          <div class="stat-label">{{ t('welcome.stats.merchants') }}</div>
          <div class="stat-value">{{ stats.merchants }}</div>
          <div class="stat-sub">{{ t('welcome.stats.active') }} {{ stats.activeMerchants }}</div>
        </div>
      </div>

      <div class="stat-card">
        <div class="stat-icon blue">
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
            <path d="M17 21v-2a4 4 0 0 0-4-4H5a4 4 0 0 0-4 4v2"/>
            <circle cx="9" cy="7" r="4"/>
            <path d="M23 21v-2a4 4 0 0 0-3-3.87"/>
            <path d="M16 3.13a4 4 0 0 1 0 7.75"/>
          </svg>
        </div>
        <div class="stat-content">
          <div class="stat-label">{{ t('welcome.stats.users') }}</div>
          <div class="stat-value">{{ stats.users }}</div>
          <div class="stat-sub">{{ t('welcome.stats.activeUsers') }}</div>
        </div>
      </div>
    </div>

    <!-- 快捷入口 -->
    <div class="quick-section" v-if="quickLinks.length > 0">
      <div class="section-header">
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
          <rect x="3" y="3" width="7" height="7"/>
          <rect x="14" y="3" width="7" height="7"/>
          <rect x="14" y="14" width="7" height="7"/>
          <rect x="3" y="14" width="7" height="7"/>
        </svg>
        <span>{{ t('welcome.quickEntry') }}</span>
      </div>

      <div class="quick-grid">
        <div v-for="link in quickLinks" :key="link.path" class="quick-card" @click="navigateTo(link.path)">
          <div class="quick-icon" :class="link.color">
            <svg v-if="link.icon === 'globe'" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
              <circle cx="12" cy="12" r="10"/><line x1="2" y1="12" x2="22" y2="12"/>
              <path d="M12 2a15.3 15.3 0 0 1 4 10 15.3 15.3 0 0 1-4 10 15.3 15.3 0 0 1-4-10 15.3 15.3 0 0 1 4-10z"/>
            </svg>
            <svg v-else-if="link.icon === 'store'" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
              <path d="M3 9l9-7 9 7v11a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2z"/><polyline points="9 22 9 12 15 12 15 22"/>
            </svg>
            <svg v-else-if="link.icon === 'network'" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
              <rect x="2" y="3" width="20" height="14" rx="2" ry="2"/><line x1="8" y1="21" x2="16" y2="21"/><line x1="12" y1="17" x2="12" y2="21"/>
            </svg>
            <svg v-else-if="link.icon === 'users'" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
              <path d="M20 21v-2a4 4 0 0 0-4-4H8a4 4 0 0 0-4 4v2"/><circle cx="12" cy="7" r="4"/>
            </svg>
            <svg v-else-if="link.icon === 'audit'" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
              <path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8z"/><polyline points="14 2 14 8 20 8"/>
              <line x1="16" y1="13" x2="8" y2="13"/><line x1="16" y1="17" x2="8" y2="17"/>
            </svg>
            <svg v-else-if="link.icon === 'duty'" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
              <rect x="3" y="4" width="18" height="18" rx="2" ry="2"/><line x1="16" y1="2" x2="16" y2="6"/><line x1="8" y1="2" x2="8" y2="6"/>
              <line x1="3" y1="10" x2="21" y2="10"/><path d="M9 16l2 2 4-4"/>
            </svg>
          </div>
          <div class="quick-title">{{ t(link.titleKey) }}</div>
          <div class="quick-desc">{{ t(link.descKey) }}</div>
        </div>
      </div>
    </div>
  </div>
</template>

<style scoped>
.welcome-page {
  display: flex;
  flex-direction: column;
  gap: 24px;
}

.sso-notice {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 14px 18px;
  background: linear-gradient(135deg, rgba(16, 185, 129, 0.12) 0%, rgba(6, 182, 212, 0.12) 100%);
  border: 1px solid rgba(16, 185, 129, 0.3);
  border-radius: 10px;
  color: #059669;
  font-size: 14px;
  font-weight: 500;
}

.sso-notice svg:first-child {
  flex-shrink: 0;
  color: #10b981;
}

.sso-notice span {
  flex: 1;
}

.sso-close {
  background: none;
  border: none;
  padding: 4px;
  cursor: pointer;
  color: #6b7280;
  border-radius: 4px;
  display: flex;
  align-items: center;
  justify-content: center;
  transition: all 0.15s;
}

.sso-close:hover {
  background: rgba(0, 0, 0, 0.08);
  color: #374151;
}

.fade-enter-active,
.fade-leave-active {
  transition: opacity 0.3s ease, transform 0.3s ease;
}

.fade-enter-from,
.fade-leave-to {
  opacity: 0;
  transform: translateY(-8px);
}

.welcome-banner {
  background: linear-gradient(135deg, #3b82f6 0%, #1d4ed8 100%);
  border-radius: 12px;
  padding: 32px 40px;
  color: #fff;
}

.welcome-title {
  margin: 0 0 8px 0;
  font-size: 26px;
  font-weight: 600;
}

.welcome-date {
  margin: 0;
  font-size: 14px;
  opacity: 0.85;
}

.stats-row {
  display: grid;
  grid-template-columns: repeat(3, 1fr);
  gap: 20px;
}

.stat-card {
  background: var(--bg-card);
  border: 1px solid var(--border-color);
  border-radius: 12px;
  padding: 24px;
  display: flex;
  align-items: flex-start;
  gap: 16px;
}

.stat-icon {
  width: 48px;
  height: 48px;
  border-radius: 10px;
  display: flex;
  align-items: center;
  justify-content: center;
  flex-shrink: 0;
}

.stat-icon svg {
  width: 24px;
  height: 24px;
}

.stat-icon.teal {
  background: rgba(20, 184, 166, 0.1);
  color: #14b8a6;
}

.stat-icon.orange {
  background: rgba(251, 146, 60, 0.1);
  color: #fb923c;
}

.stat-icon.blue {
  background: rgba(59, 130, 246, 0.1);
  color: #3b82f6;
}

.stat-content {
  flex: 1;
}

.stat-label {
  font-size: 13px;
  color: var(--text-muted);
  margin-bottom: 4px;
}

.stat-value {
  font-size: 32px;
  font-weight: 700;
  color: var(--text-primary);
  line-height: 1.1;
  margin-bottom: 4px;
}

.stat-sub {
  font-size: 12px;
  color: var(--text-muted);
}

.quick-section {
  background: var(--bg-card);
  border: 1px solid var(--border-color);
  border-radius: 12px;
  padding: 24px;
}

.section-header {
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: 15px;
  font-weight: 600;
  color: var(--text-primary);
  margin-bottom: 20px;
}

.section-header svg {
  width: 18px;
  height: 18px;
  color: var(--text-muted);
}

.quick-grid {
  display: grid;
  grid-template-columns: repeat(5, 1fr);
  gap: 16px;
}

.quick-card {
  background: var(--bg-hover);
  border: 1px solid var(--border-color);
  border-radius: 10px;
  padding: 24px 16px;
  text-align: center;
  cursor: pointer;
  transition: all 0.2s;
}

.quick-card:hover {
  border-color: #3b82f6;
  box-shadow: 0 4px 12px rgba(59, 130, 246, 0.15);
  transform: translateY(-2px);
}

.quick-icon {
  width: 48px;
  height: 48px;
  border-radius: 10px;
  display: flex;
  align-items: center;
  justify-content: center;
  margin: 0 auto 14px;
}

.quick-icon svg {
  width: 24px;
  height: 24px;
}

.quick-icon.blue {
  background: rgba(59, 130, 246, 0.1);
  color: #3b82f6;
}

.quick-icon.indigo {
  background: rgba(99, 102, 241, 0.1);
  color: #6366f1;
}

.quick-icon.cyan {
  background: rgba(6, 182, 212, 0.1);
  color: #06b6d4;
}

.quick-icon.purple {
  background: rgba(139, 92, 246, 0.1);
  color: #8b5cf6;
}

.quick-icon.slate {
  background: rgba(100, 116, 139, 0.1);
  color: #64748b;
}

.quick-title {
  font-size: 14px;
  font-weight: 600;
  color: var(--text-primary);
  margin-bottom: 4px;
}

.quick-desc {
  font-size: 12px;
  color: var(--text-muted);
}

@media (max-width: 1200px) {
  .quick-grid {
    grid-template-columns: repeat(3, 1fr);
  }
}

@media (max-width: 768px) {
  .stats-row {
    grid-template-columns: 1fr;
  }
  .quick-grid {
    grid-template-columns: repeat(2, 1fr);
  }
}
</style>
