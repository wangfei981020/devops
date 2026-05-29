<script setup>
import { ref, computed, onMounted, watch, nextTick } from 'vue'
import SwaggerUIBundle from 'swagger-ui-dist/swagger-ui-bundle.js'
import 'swagger-ui-dist/swagger-ui.css'
import api from '@/api'
import { useAppStore } from '@/stores'

const appStore = useAppStore()
const domains = ref([])
const currentDomain = ref('')
const loading = ref(true)
// 调试开关按域独立存：{ domainCode: bool }
const debugEnabledByDomain = ref({})
let swaggerInstance = null

// 按 category 分组（保留 sort_order 内部排序）
const groupedDomains = computed(() => {
  const groups = {}
  for (const d of domains.value) {
    const cat = d.category || '其他'
    if (!groups[cat]) groups[cat] = []
    groups[cat].push(d)
  }
  for (const cat in groups) {
    groups[cat].sort((a, b) => (a.sort_order || 0) - (b.sort_order || 0))
  }
  return Object.entries(groups).map(([category, items]) => ({ category, items }))
})

const currentDomainObj = computed(() =>
  domains.value.find(d => d.code === currentDomain.value) || null
)

const canTryCurrent = computed(() => currentDomainObj.value?.can_try_it ?? false)

const debugOn = computed(() => !!debugEnabledByDomain.value[currentDomain.value])

const toggleLabel = computed(() => {
  if (!canTryCurrent.value) return '调试: 无权限'
  return debugOn.value ? '调试: 已开启' : '调试: 关闭'
})

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

async function toggleDebug() {
  if (!canTryCurrent.value) return
  if (debugOn.value) {
    // 关 → 直接关，不弹
    debugEnabledByDomain.value = { ...debugEnabledByDomain.value, [currentDomain.value]: false }
    return
  }
  // 开 → 先弹警告
  const ok = await appStore.showConfirm({
    type: 'warning',
    title: '开启调试',
    message: `开启后，"${currentDomainObj.value?.name}" 的所有 Execute 请求会真实写入数据库（创建/修改/删除）。是否继续？`,
    okText: '我知道了，开启',
    cancelText: '取消'
  })
  if (!ok) return
  debugEnabledByDomain.value = { ...debugEnabledByDomain.value, [currentDomain.value]: true }
}

// 切域或开关变化时重新渲染 Swagger UI
watch([currentDomain, debugOn], async ([newDomain]) => {
  if (!newDomain) return
  await nextTick()
  const container = document.getElementById('swagger-ui-container')
  if (container) container.innerHTML = ''
  swaggerInstance = SwaggerUIBundle({
    url: `/api/api-docs/spec?domain=${encodeURIComponent(newDomain)}`,
    dom_id: '#swagger-ui-container',
    deepLinking: false,
    docExpansion: 'list',
    defaultModelsExpandDepth: 1,
    tryItOutEnabled: debugOn.value,
    supportedSubmitMethods: debugOn.value ? ['get', 'post', 'put', 'delete', 'patch'] : []
  })
}, { immediate: false })
</script>

<template>
  <div class="api-docs-page">
    <div class="page-header">
      <h2>接口文档</h2>
      <p class="page-desc">供内部开发对接的 API Key 接口。从左侧选择业务域，主区右上角 Authorize 粘贴 API Key 后可在线调试。</p>
    </div>

    <div v-if="loading" class="empty">加载中...</div>
    <div v-else-if="!domains.length" class="empty">
      您当前没有可查看的接口文档权限，请联系管理员授予 <code>menu:api_docs:&lt;域&gt;</code> 权限。
    </div>
    <div v-else class="docs-layout">
      <!-- 左栏：业务域列表 -->
      <aside class="domain-sidebar">
        <div v-for="group in groupedDomains" :key="group.category" class="domain-group">
          <div class="group-title">
            <span>{{ group.category }}</span>
            <span class="group-count">{{ group.items.length }}</span>
          </div>
          <ul class="domain-list">
            <li
              v-for="d in group.items"
              :key="d.code"
              :class="{ active: currentDomain === d.code }"
              :title="d.description"
              @click="currentDomain = d.code">
              <span class="domain-name">{{ d.name }}</span>
              <span v-if="!d.can_try_it" class="readonly-tag" title="无调试权限，仅查看">只读</span>
            </li>
          </ul>
        </div>
      </aside>

      <!-- 右侧主区 -->
      <main class="swagger-main">
        <div class="main-toolbar">
          <div class="domain-title">
            <strong>{{ currentDomainObj?.name || '' }}</strong>
            <span v-if="currentDomainObj?.description" class="domain-desc">{{ currentDomainObj.description }}</span>
          </div>
          <button
            class="debug-toggle"
            :class="{ on: debugOn, off: !debugOn && canTryCurrent, locked: !canTryCurrent }"
            :disabled="!canTryCurrent"
            :title="canTryCurrent ? '点击切换调试状态' : '当前业务域无调试权限'"
            @click="toggleDebug">
            <span class="dot"></span>
            <span class="label">{{ toggleLabel }}</span>
          </button>
        </div>
        <div id="swagger-ui-container"></div>
      </main>
    </div>
  </div>
</template>

<style scoped>
.api-docs-page { padding: 20px; color: var(--text-primary); display: flex; flex-direction: column; height: 100%; }
.page-header { margin-bottom: 12px; flex-shrink: 0; }
.page-header h2 { margin: 0 0 6px 0; font-size: 20px; }
.page-desc { margin: 0; color: var(--text-secondary); font-size: 13px; }
.empty { text-align: center; padding: 48px 24px; color: var(--text-muted); font-size: 14px; }
.empty code { background: var(--bg-input); padding: 2px 6px; border-radius: 3px; color: var(--text-primary); font-family: Consolas, monospace; }

.docs-layout {
  display: flex;
  flex: 1;
  min-height: 0;
  gap: 12px;
  border-top: 1px solid var(--border-color);
  padding-top: 12px;
}

/* 左栏 */
.domain-sidebar {
  width: 200px;
  min-width: 200px;
  flex-shrink: 0;
  overflow-y: auto;
  padding-right: 8px;
  border-right: 1px solid var(--border-color);
}
.domain-group { margin-bottom: 16px; }
.group-title {
  display: flex;
  justify-content: space-between;
  align-items: center;
  padding: 6px 10px;
  font-size: 11px;
  font-weight: 600;
  color: var(--text-muted);
  text-transform: uppercase;
  letter-spacing: 0.5px;
}
.group-count {
  font-size: 11px;
  font-weight: normal;
  color: var(--text-muted);
  background: var(--bg-input);
  padding: 0 6px;
  border-radius: 8px;
  min-width: 18px;
  text-align: center;
}
.domain-list { list-style: none; margin: 0; padding: 0; }
.domain-list li {
  display: flex;
  justify-content: space-between;
  align-items: center;
  padding: 8px 12px;
  font-size: 13px;
  color: var(--text-primary);
  cursor: pointer;
  border-radius: 4px;
  margin-bottom: 2px;
  position: relative;
}
.domain-list li:hover { background: var(--bg-hover); }
.domain-list li.active {
  background: var(--primary);
  color: white;
  font-weight: 500;
}
.domain-name { flex: 1; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.readonly-tag {
  font-size: 10px;
  padding: 1px 6px;
  border-radius: 3px;
  background: var(--bg-input);
  color: var(--text-muted);
  margin-left: 6px;
}
.domain-list li.active .readonly-tag {
  background: rgba(255, 255, 255, 0.2);
  color: rgba(255, 255, 255, 0.85);
}

/* 右侧主区 */
.swagger-main {
  flex: 1;
  min-width: 0;
  overflow-y: auto;
  display: flex;
  flex-direction: column;
}

/* 主区顶部工具栏 */
.main-toolbar {
  display: flex;
  justify-content: space-between;
  align-items: center;
  padding: 10px 16px;
  background: var(--bg-card, white);
  border: 1px solid var(--border-color);
  border-radius: 6px 6px 0 0;
  border-bottom: none;
  flex-shrink: 0;
}
.domain-title { display: flex; align-items: baseline; gap: 12px; min-width: 0; }
.domain-title strong { font-size: 15px; color: var(--text-primary); white-space: nowrap; }
.domain-desc { font-size: 12px; color: var(--text-muted); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }

/* 调试开关按钮 */
.debug-toggle {
  display: inline-flex;
  align-items: center;
  gap: 8px;
  padding: 6px 14px;
  border-radius: 16px;
  font-size: 13px;
  border: 1px solid;
  cursor: pointer;
  user-select: none;
  white-space: nowrap;
  transition: all 0.15s ease;
}
.debug-toggle .dot {
  width: 8px; height: 8px;
  border-radius: 50%;
  display: inline-block;
}
.debug-toggle.off {
  background: var(--bg-input);
  border-color: var(--border-color);
  color: var(--text-secondary);
}
.debug-toggle.off .dot { background: #9ca3af; }
.debug-toggle.off:hover { background: var(--bg-hover); }
.debug-toggle.on {
  background: rgba(34, 197, 94, 0.12);
  border-color: #22c55e;
  color: #16a34a;
  font-weight: 500;
}
.debug-toggle.on .dot { background: #22c55e; box-shadow: 0 0 6px rgba(34, 197, 94, 0.6); }
.debug-toggle.on:hover { background: rgba(34, 197, 94, 0.2); }
.debug-toggle.locked {
  background: var(--bg-input);
  border-color: var(--border-color);
  color: var(--text-muted);
  cursor: not-allowed;
}
.debug-toggle.locked .dot { background: #ef4444; }

#swagger-ui-container {
  background: white;
  border: 1px solid var(--border-color);
  border-radius: 0 0 6px 6px;
  min-height: 400px;
  flex: 1;
}
:deep(.swagger-ui) { color: #3b4151; }
</style>
