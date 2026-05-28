<script setup>
import { ref, onMounted, watch, nextTick } from 'vue'
import SwaggerUIBundle from 'swagger-ui-dist/swagger-ui-bundle.js'
import 'swagger-ui-dist/swagger-ui.css'
import api from '@/api'
import { useAppStore } from '@/stores'

const appStore = useAppStore()
const domains = ref([])
const currentDomain = ref('')
const loading = ref(true)
const tryItWarned = ref(false)
let swaggerInstance = null

onMounted(async () => {
  loading.value = true
  try {
    const res = await api.get('/api/api-docs/domains')
    domains.value = res.data || []
    if (domains.value.length) {
      currentDomain.value = domains.value[0].code
    }
  } catch (e) {
    appStore.showToast('加载业务域列表失败', 'error')
  } finally {
    loading.value = false
  }
})

watch(currentDomain, async (newDomain) => {
  if (!newDomain) return
  await nextTick()
  const dom = domains.value.find(d => d.code === newDomain)
  const canTry = dom?.can_try_it ?? false

  // 清掉旧实例
  const container = document.getElementById('swagger-ui-container')
  if (container) container.innerHTML = ''

  swaggerInstance = SwaggerUIBundle({
    url: `/api/api-docs/spec?domain=${encodeURIComponent(newDomain)}`,
    dom_id: '#swagger-ui-container',
    deepLinking: false,
    docExpansion: 'list',
    defaultModelsExpandDepth: 1,
    tryItOutEnabled: canTry,
    supportedSubmitMethods: canTry ? ['get', 'post', 'put', 'delete', 'patch'] : [],
    requestInterceptor: async (req) => {
      if (!tryItWarned.value) {
        const ok = await appStore.showConfirm({
          type: 'warning',
          title: '调试警告',
          message: '调试请求会真实写入数据库（创建/修改/删除）。是否继续？',
          okText: '我知道了，继续',
          cancelText: '取消'
        })
        if (!ok) {
          throw new Error('用户取消了调试请求')
        }
        tryItWarned.value = true
      }
      return req
    }
  })
}, { immediate: false })
</script>

<template>
  <div class="api-docs-page">
    <div class="page-header">
      <h2>接口文档</h2>
      <p class="page-desc">供内部开发对接的 API Key 接口。点击右上角 "Authorize" 按钮粘贴 API Key 后可以在线调试。</p>
    </div>

    <div v-if="loading" class="empty">加载中...</div>
    <div v-else-if="!domains.length" class="empty">
      您当前没有可查看的接口文档权限，请联系管理员授予 <code>menu:api_docs:&lt;域&gt;</code> 权限。
    </div>
    <div v-else>
      <div class="domain-tabs">
        <button
          v-for="d in domains"
          :key="d.code"
          :class="{ active: currentDomain === d.code }"
          @click="currentDomain = d.code">
          {{ d.name }}
          <span v-if="!d.can_try_it" class="readonly-tag" title="无调试权限，仅查看">只读</span>
        </button>
      </div>
      <div id="swagger-ui-container"></div>
    </div>
  </div>
</template>

<style scoped>
.api-docs-page { padding: 20px; color: var(--text-primary); }
.page-header { margin-bottom: 16px; }
.page-header h2 { margin: 0 0 6px 0; font-size: 20px; }
.page-desc { margin: 0; color: var(--text-secondary); font-size: 13px; }
.empty { text-align: center; padding: 48px 24px; color: var(--text-muted); font-size: 14px; }
.empty code { background: var(--bg-input); padding: 2px 6px; border-radius: 3px; color: var(--text-primary); font-family: Consolas, monospace; }

.domain-tabs { display: flex; gap: 8px; border-bottom: 1px solid var(--border-color); margin-bottom: 16px; }
.domain-tabs button {
  background: none; border: none; padding: 10px 16px; font-size: 14px;
  color: var(--text-secondary); cursor: pointer; position: relative; display: flex; align-items: center; gap: 6px;
}
.domain-tabs button:hover { color: var(--text-primary); }
.domain-tabs button.active { color: var(--primary); font-weight: 600; }
.domain-tabs button.active::after {
  content: ''; position: absolute; left: 0; right: 0; bottom: -1px; height: 2px; background: var(--primary);
}
.readonly-tag {
  font-size: 10px; padding: 1px 6px; border-radius: 3px;
  background: var(--bg-hover); color: var(--text-muted);
}

#swagger-ui-container { background: white; border-radius: 6px; min-height: 400px; }
/* 暗黑模式下 Swagger UI 默认主题对比度低，加白底兜底 */
:deep(.swagger-ui) { color: #3b4151; }
</style>
