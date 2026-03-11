<template>
  <div>
    <ConnectionSelector type="confluence" v-model="connId" @connection-change="onConnChange" />

    <div v-if="loading" class="loading"><div class="spinner"></div>加载中...</div>

    <template v-else>
      <div v-if="error" class="card" style="text-align:center;padding:40px;color:var(--text-muted)">{{ error }}</div>

      <div class="card" v-else>
        <div class="table-wrapper">
          <table>
            <thead>
              <tr>
                <th>Key</th>
                <th>名称</th>
                <th>类型</th>
                <th>描述</th>
                <th>操作</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="space in spaces" :key="space.key" @click="$router.push('/spaces/' + space.key)">
                <td><span class="space-key">{{ space.key }}</span></td>
                <td>{{ space.name }}</td>
                <td><span class="badge" :class="space.type">{{ space.type === 'global' ? '全局' : '个人' }}</span></td>
                <td class="desc-cell">{{ space.description?.plain?.value || '-' }}</td>
                <td>
                  <button class="btn btn-sm" @click.stop="$router.push('/spaces/' + space.key)">查看内容</button>
                </td>
              </tr>
            </tbody>
          </table>
        </div>
        <div v-if="!allowedKeys.length && hasMore" class="pagination">
          <button @click="loadMore" :disabled="loadingMore">{{ loadingMore ? '加载中...' : '加载更多' }}</button>
        </div>
      </div>
    </template>
  </div>
</template>

<script setup>
import { ref, onMounted } from 'vue'
import api from '@/api'
import ConnectionSelector from '@/components/ConnectionSelector.vue'

const loading = ref(true)
const loadingMore = ref(false)
const error = ref('')
const spaces = ref([])
const start = ref(0)
const hasMore = ref(false)
const allowedKeys = ref([])
const connId = ref('')

function connParam() {
  return connId.value ? `conn_id=${connId.value}&` : ''
}

onMounted(async () => {
  // 初始加载由 ConnectionSelector 的 connection-change 触发
})

async function loadSpaces() {
  loading.value = true
  error.value = ''
  spaces.value = []
  start.value = 0

  if (allowedKeys.value.length > 0) {
    await fetchAllowedSpaces()
  } else {
    await fetchSpaces()
  }
  loading.value = false
}

function onConnChange(conn) {
  // 从连接的 config 中读取 space_key
  if (conn && conn.config) {
    const cfg = typeof conn.config === 'string' ? JSON.parse(conn.config) : conn.config
    const spaceKey = cfg.space_key || ''
    allowedKeys.value = spaceKey.split(',').map(s => s.trim()).filter(Boolean)
  } else {
    allowedKeys.value = []
  }
  loadSpaces()
}

async function fetchAllowedSpaces() {
  const results = []
  for (const key of allowedKeys.value) {
    try {
      const res = await api.get(`/api/confluence/spaces/${key}?${connParam()}`)
      results.push(res.data.data)
    } catch (e) { /* skip unavailable spaces */ }
  }
  spaces.value = results
}

async function fetchSpaces() {
  try {
    const res = await api.get(`/api/confluence/spaces?${connParam()}start=${start.value}&limit=25`)
    const data = res.data.data
    spaces.value = data.results || []
    hasMore.value = (data.start + data.size) < data.totalSize
    start.value = data.start + data.size
  } catch (e) {
    error.value = e.response?.data?.error || '加载空间列表失败'
  }
}

async function loadMore() {
  loadingMore.value = true
  try {
    const res = await api.get(`/api/confluence/spaces?${connParam()}start=${start.value}&limit=25`)
    const data = res.data.data
    spaces.value.push(...(data.results || []))
    hasMore.value = (data.start + data.size) < data.totalSize
    start.value = data.start + data.size
  } catch (e) { /* ignore */ }
  finally { loadingMore.value = false }
}
</script>

<style scoped>
.space-key {
  font-family: var(--font-mono);
  font-size: 13px;
  color: var(--primary-light);
  font-weight: 500;
}
.badge.global { background: rgba(76,154,255,0.15); color: #4C9AFF; }
.badge.personal { background: rgba(139,92,246,0.15); color: #8B5CF6; }
.desc-cell { max-width: 300px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
</style>
