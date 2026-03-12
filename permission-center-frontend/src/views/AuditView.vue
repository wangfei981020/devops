<script setup>
import { ref, onMounted } from 'vue'
import api from '@/api'
import { useAppStore } from '@/stores/app'

const appStore = useAppStore()
const logs = ref([])
const loading = ref(false)
const total = ref(0)
const page = ref(1)
const pageSize = 20

// Filters
const filterAction = ref('')
const filterUsername = ref('')
const filterDateStart = ref('')
const filterDateEnd = ref('')

async function loadLogs() {
  loading.value = true
  try {
    const params = new URLSearchParams()
    params.set('page', page.value)
    params.set('page_size', pageSize)
    if (filterAction.value) params.set('action', filterAction.value)
    if (filterUsername.value) params.set('username', filterUsername.value)
    if (filterDateStart.value) params.set('start_date', filterDateStart.value)
    if (filterDateEnd.value) params.set('end_date', filterDateEnd.value)

    const res = await api.get('/api/admin/audit-logs?' + params.toString())
    const data = res.data?.data
    logs.value = data?.logs || []
    total.value = data?.total || 0
  } catch (e) {
    appStore.showToast('加载失败', 'error')
  } finally {
    loading.value = false
  }
}

function search() {
  page.value = 1
  loadLogs()
}

function resetFilters() {
  filterAction.value = ''
  filterUsername.value = ''
  filterDateStart.value = ''
  filterDateEnd.value = ''
  page.value = 1
  loadLogs()
}

const totalPages = () => Math.ceil(total.value / pageSize) || 1

function prevPage() {
  if (page.value > 1) { page.value--; loadLogs() }
}

function nextPage() {
  if (page.value < totalPages()) { page.value++; loadLogs() }
}

function formatTime(ts) {
  if (!ts) return '-'
  const d = new Date(ts)
  const pad = n => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${pad(d.getMonth()+1)}-${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}:${pad(d.getSeconds())}`
}

function actionColor(action) {
  if (action?.includes('delete')) return 'action-danger'
  if (action?.includes('create') || action?.includes('add')) return 'action-success'
  if (action?.includes('update') || action?.includes('edit')) return 'action-warn'
  return ''
}

onMounted(() => {
  loadLogs()
})
</script>

<template>
  <div class="page">
    <div class="toolbar">
      <input v-model="filterUsername" placeholder="用户名" class="filter-input" @keyup.enter="search" />
      <input v-model="filterAction" placeholder="操作类型" class="filter-input" @keyup.enter="search" />
      <input v-model="filterDateStart" type="date" class="filter-input" />
      <input v-model="filterDateEnd" type="date" class="filter-input" />
      <button class="btn btn-primary" @click="search">搜索</button>
      <button class="btn" @click="resetFilters">重置</button>
    </div>

    <div class="table-wrap">
      <table>
        <thead>
          <tr>
            <th>时间</th>
            <th>用户</th>
            <th>操作</th>
            <th>目标</th>
            <th>详情</th>
            <th>IP</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="log in logs" :key="log.id">
            <td class="time-cell">{{ formatTime(log.created_at) }}</td>
            <td>{{ log.username || log.user_id }}</td>
            <td><span class="action-tag" :class="actionColor(log.action)">{{ log.action }}</span></td>
            <td>{{ log.target_type }} <code v-if="log.target_id">{{ log.target_id }}</code></td>
            <td class="detail-cell">{{ log.detail || '-' }}</td>
            <td>{{ log.ip || '-' }}</td>
          </tr>
          <tr v-if="logs.length === 0">
            <td colspan="6" class="empty">{{ loading ? '加载中...' : '暂无数据' }}</td>
          </tr>
        </tbody>
      </table>
    </div>

    <!-- Pagination -->
    <div class="pagination" v-if="total > 0">
      <span class="page-info">共 {{ total }} 条，第 {{ page }}/{{ totalPages() }} 页</span>
      <div class="page-btns">
        <button class="btn-sm" @click="prevPage" :disabled="page <= 1">上一页</button>
        <button class="btn-sm" @click="nextPage" :disabled="page >= totalPages()">下一页</button>
      </div>
    </div>
  </div>
</template>

<style scoped>
.page { max-width: 1400px; }
.toolbar { display: flex; gap: 8px; margin-bottom: 16px; align-items: center; flex-wrap: wrap; }
.filter-input { padding: 8px 12px; background: #1e293b; border: 1px solid rgba(255,255,255,0.1); border-radius: 6px; color: #cbd5e1; font-size: 13px; outline: none; width: 140px; }
.filter-input:focus { border-color: #3b82f6; }
.table-wrap { background: #1e293b; border-radius: 10px; border: 1px solid rgba(255,255,255,0.06); overflow-x: auto; }
table { width: 100%; border-collapse: collapse; }
th { padding: 12px 16px; text-align: left; font-size: 12px; color: #64748b; font-weight: 600; text-transform: uppercase; border-bottom: 1px solid rgba(255,255,255,0.06); white-space: nowrap; }
td { padding: 10px 16px; font-size: 13px; color: #cbd5e1; border-bottom: 1px solid rgba(255,255,255,0.04); }
tr:hover td { background: rgba(255,255,255,0.02); }
.empty { text-align: center; color: #64748b; padding: 40px !important; }
.time-cell { white-space: nowrap; color: #94a3b8; font-size: 12px; }
.detail-cell { max-width: 300px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
code { padding: 2px 6px; background: rgba(255,255,255,0.06); border-radius: 3px; font-size: 12px; }

.action-tag { padding: 2px 8px; border-radius: 4px; font-size: 12px; }
.action-danger { background: rgba(239,68,68,0.15); color: #f87171; }
.action-success { background: rgba(16,185,129,0.15); color: #34d399; }
.action-warn { background: rgba(245,158,11,0.15); color: #fbbf24; }

.pagination { display: flex; justify-content: space-between; align-items: center; margin-top: 16px; padding: 0 4px; }
.page-info { font-size: 13px; color: #64748b; }
.page-btns { display: flex; gap: 8px; }

.btn { padding: 8px 16px; border: 1px solid rgba(255,255,255,0.1); background: #1e293b; color: #cbd5e1; border-radius: 6px; cursor: pointer; font-size: 13px; }
.btn:hover { background: #334155; }
.btn-primary { background: #3b82f6; border-color: #3b82f6; color: white; }
.btn-primary:hover { background: #2563eb; }
.btn-sm { padding: 4px 10px; font-size: 12px; background: transparent; border: 1px solid rgba(255,255,255,0.1); color: #94a3b8; border-radius: 4px; cursor: pointer; }
.btn-sm:hover { background: rgba(255,255,255,0.06); }
.btn-sm:disabled { opacity: 0.4; cursor: not-allowed; }
</style>
