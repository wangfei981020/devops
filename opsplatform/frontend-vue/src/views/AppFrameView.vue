<script setup>
import { ref, computed, onMounted, watch } from 'vue'
import { useRoute } from 'vue-router'
import api from '@/api'

const route = useRoute()
const apps = ref([])
const loading = ref(true)
const portalToken = ref('')

const currentApp = computed(() => {
  const key = route.params.appKey
  return apps.value.find(a => a.app_key === key)
})

const iframeSrc = computed(() => {
  const url = currentApp.value?.url
  if (!url) return ''
  if (!portalToken.value) return url
  const sep = url.includes('?') ? '&' : '?'
  return url + sep + 'portal_token=' + encodeURIComponent(portalToken.value)
})

onMounted(async () => {
  try {
    // 获取真实 JWT token（SSO 用户 localStorage 中只有占位符）
    const tokenRes = await api.get('/api/portal-token')
    portalToken.value = tokenRes.data?.token || ''
  } catch (e) {}

  try {
    const res = await api.get('/api/external-apps/my')
    if (res.data?.data) apps.value = res.data.data
    else if (Array.isArray(res.data)) apps.value = res.data
  } catch (e) {
    console.log('加载应用列表失败:', e)
  } finally {
    loading.value = false
  }
})
</script>

<template>
  <div class="app-frame-container">
    <div v-if="loading" class="frame-loading">
      <div class="loading-spinner"></div>
      <span>加载中...</span>
    </div>
    <div v-else-if="!currentApp" class="frame-empty">
      <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" class="empty-icon">
        <rect x="2" y="3" width="20" height="14" rx="2"/>
        <line x1="8" y1="21" x2="16" y2="21"/>
        <line x1="12" y1="17" x2="12" y2="21"/>
      </svg>
      <p>应用不存在或无访问权限</p>
    </div>
    <iframe
      v-else
      :src="iframeSrc"
      class="app-iframe"
      frameborder="0"
      allow="clipboard-read; clipboard-write"
    ></iframe>
  </div>
</template>

<style scoped>
.app-frame-container {
  width: 100%;
  height: calc(100vh - 0px);
  display: flex;
  flex-direction: column;
  background: var(--bg-primary, #0f172a);
}

.app-iframe {
  flex: 1;
  width: 100%;
  height: 100%;
  border: none;
}

.frame-loading,
.frame-empty {
  flex: 1;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 12px;
  color: rgba(255, 255, 255, 0.5);
}

.empty-icon {
  width: 48px;
  height: 48px;
  opacity: 0.3;
}

.loading-spinner {
  width: 32px;
  height: 32px;
  border: 3px solid rgba(255, 255, 255, 0.1);
  border-top-color: #3b82f6;
  border-radius: 50%;
  animation: spin 0.8s linear infinite;
}

@keyframes spin {
  to { transform: rotate(360deg); }
}
</style>
