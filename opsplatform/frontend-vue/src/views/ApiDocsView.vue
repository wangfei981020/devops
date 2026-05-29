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
const tryItWarned = ref(false)
let swaggerInstance = null

// 按 category 分组（保留 sort_order 内部排序）
const groupedDomains = computed(() => {
  const groups = {}
  for (const d of domains.value) {
    const cat = d.category || '其他'
    if (!groups[cat]) groups[cat] = []
    groups[cat].push(d)
  }
  // 每组内按 sort_order 排序
  for (const cat in groups) {
    groups[cat].sort((a, b) => (a.sort_order || 0) - (b.sort_order || 0))
  }
  // 转成数组保留出现顺序
  return Object.entries(groups).map(([category, items]) => ({ category, items }))
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
      <p class="page-desc">供内部开发对接的 API Key 接口。从左侧选择业务域，右上角 Authorize 粘贴 API Key 后可在线调试。</p>
    </div>

    <div v-if="loading" class="empty">加载中...</div>
    <div v-else-if="!domains.length" class="empty">
      您当前没有可查看的接口文档权限，请联系管理员授予 <code>menu:api_docs:&lt;域&gt;</code> 权限。
    </div>
    <div v-else class="docs-layout">
      <!-- 左栏：业务域列表，按 category 分组 -->
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

      <!-- 右侧：Swagger UI -->
      <main class="swagger-main">
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

/* 整体两栏布局 */
.docs-layout {
  display: flex;
  flex: 1;
  min-height: 0;  /* 让 flex 子项可以收缩 */
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
.domain-list li.active::before {
  content: '';
  position: absolute;
  left: -8px;
  top: 50%;
  transform: translateY(-50%);
  width: 3px;
  height: 60%;
  background: var(--primary);
  border-radius: 2px;
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
  background: white;
  border-radius: 6px;
}
#swagger-ui-container { background: white; border-radius: 6px; min-height: 400px; }
/* 暗黑模式下 Swagger UI 默认主题对比度低，加白底兜底 */
:deep(.swagger-ui) { color: #3b4151; }
</style>
