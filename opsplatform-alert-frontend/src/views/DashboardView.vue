<template>
  <div>
    <transition name="fade">
      <div v-if="showPortalNotice" class="portal-notice">
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" width="18" height="18">
          <path d="M12 22s8-4 8-10V5l-8-3-8 3v7c0 6 8 10 8 10z"/>
          <polyline points="9 12 11 14 15 10"/>
        </svg>
        <span>SSO 登录成功，欢迎 {{ auth.user?.username }}</span>
        <button class="notice-close" @click="showPortalNotice = false">&times;</button>
      </div>
    </transition>

    <div class="stats-grid">
      <div v-if="auth.hasMenu('rules')" class="stat-card">
        <div class="label">告警规则</div>
        <div class="value primary">{{ stats.rules_total || 0 }}</div>
        <div class="text-sm text-secondary">启用: {{ stats.rules_enabled || 0 }}</div>
      </div>
      <div v-if="auth.hasMenu('logs')" class="stat-card">
        <div class="label">今日告警</div>
        <div class="value warning">{{ stats.today_alerts || 0 }}</div>
        <div class="text-sm text-secondary">
          成功: {{ stats.today_success || 0 }} / 失败: {{ stats.today_failed || 0 }}
        </div>
      </div>
      <div v-if="auth.hasMenu('connections')" class="stat-card">
        <div class="label">ES 连接</div>
        <div class="value info">{{ stats.es_connections || 0 }}</div>
        <div class="text-sm text-secondary">活跃: {{ stats.es_active || 0 }}</div>
      </div>
      <div v-if="auth.hasMenu('lark')" class="stat-card">
        <div class="label">Lark 配置</div>
        <div class="value success">{{ stats.lark_configs || 0 }}</div>
        <div class="text-sm text-secondary">活跃: {{ stats.lark_active || 0 }}</div>
      </div>
    </div>

    <div class="card" v-if="auth.hasMenu('rules') || auth.hasMenu('connections') || auth.hasMenu('lark') || auth.hasMenu('logs')">
      <div class="card-header">
        <div class="card-title">快速操作</div>
      </div>
      <div class="flex gap-4">
        <router-link v-if="auth.hasMenu('rules')" to="/alert-rules/create" class="btn btn-primary">
          <Plus :size="16" /> 新建告警规则
        </router-link>
        <router-link v-if="auth.hasMenu('connections')" to="/es-connections" class="btn btn-outline">
          <Database :size="16" /> 管理 ES 连接
        </router-link>
        <router-link v-if="auth.hasMenu('lark')" to="/lark-configs" class="btn btn-outline">
          <Send :size="16" /> 管理 Lark 配置
        </router-link>
        <router-link v-if="auth.hasMenu('logs')" to="/alert-logs" class="btn btn-outline">
          <FileText :size="16" /> 查看告警日志
        </router-link>
      </div>
    </div>
  </div>
</template>

<script setup>
import { ref, onMounted } from 'vue'
import api from '../api'
import { useAuthStore } from '../stores/auth'
import { Plus, Database, Send, FileText } from 'lucide-vue-next'

const auth = useAuthStore()
const stats = ref({})
const showPortalNotice = ref(auth.user?.auth_source === 'portal')

onMounted(async () => {
  // Auto-hide SSO notice after 5 seconds
  if (showPortalNotice.value) {
    setTimeout(() => { showPortalNotice.value = false }, 5000)
  }
  try {
    const res = await api.get('/stats')
    if (res.code === 0) stats.value = res.data
  } catch (e) { /* ignore */ }
})
</script>

<style scoped>
.portal-notice {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 10px 16px;
  margin-bottom: 16px;
  background: rgba(16, 185, 129, 0.1);
  border: 1px solid rgba(16, 185, 129, 0.3);
  border-radius: 8px;
  color: #10b981;
  font-size: 14px;
}
.notice-close {
  margin-left: auto;
  background: none;
  border: none;
  color: #10b981;
  cursor: pointer;
  font-size: 18px;
  line-height: 1;
  opacity: 0.6;
}
.notice-close:hover { opacity: 1; }
.fade-enter-active, .fade-leave-active { transition: opacity 0.3s; }
.fade-enter-from, .fade-leave-to { opacity: 0; }
</style>
