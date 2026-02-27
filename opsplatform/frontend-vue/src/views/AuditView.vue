<script setup>
import { ref, computed, onMounted } from 'vue'
import { useAppStore, useAuthStore } from '@/stores'
import api from '@/api'

const appStore = useAppStore()
const authStore = useAuthStore()

const auditLogs = ref([])
const loading = ref(false)
const currentPage = ref(1)
const pageSize = ref(20)
const selectedIds = ref([])

const tempSearchQuery = ref('')
const tempFilterAction = ref('')
const tempFilterTargetType = ref('')
const tempFilterStatusCode = ref('')
const tempStartTime = ref('')
const tempEndTime = ref('')

const searchQuery = ref('')
const filterAction = ref('')
const filterTargetType = ref('')
const filterStatusCode = ref('')
const startTime = ref('')
const endTime = ref('')
const showDetailModal = ref(false)
const detailLog = ref(null)

const isSuperAdmin = computed(() => authStore.isSuperAdmin())

const filteredLogs = computed(() => {
  let logs = auditLogs.value
  
  if (filterAction.value) {
    logs = logs.filter(l => l.action === filterAction.value)
  }
  
  if (filterTargetType.value) {
    logs = logs.filter(l => l.target_type === filterTargetType.value)
  }
  
  if (filterStatusCode.value) {
    if (filterStatusCode.value === 'success') {
      logs = logs.filter(l => l.status_code >= 200 && l.status_code < 300)
    } else if (filterStatusCode.value === 'error') {
      logs = logs.filter(l => l.status_code >= 400)
    }
  }
  
  if (startTime.value) {
    logs = logs.filter(l => new Date(l.created_at) >= new Date(startTime.value))
  }
  
  if (endTime.value) {
    logs = logs.filter(l => new Date(l.created_at) <= new Date(endTime.value + 'T23:59:59'))
  }
  
  if (searchQuery.value) {
    const q = searchQuery.value.toLowerCase()
    logs = logs.filter(l => 
      l.path?.toLowerCase().includes(q) ||
      l.trace_id?.toLowerCase().includes(q) ||
      l.operator?.toLowerCase().includes(q) ||
      l.ip?.includes(q)
    )
  }
  
  return logs
})

const pagedLogs = computed(() => {
  const start = (currentPage.value - 1) * pageSize.value
  return filteredLogs.value.slice(start, start + pageSize.value)
})

const totalPages = computed(() => Math.ceil(filteredLogs.value.length / pageSize.value))

const actionTypes = computed(() => {
  const types = new Set(auditLogs.value.map(l => l.action).filter(Boolean))
  return Array.from(types).sort()
})

const targetTypes = computed(() => {
  const types = new Set(auditLogs.value.map(l => l.target_type).filter(Boolean))
  return Array.from(types).sort()
})

const stats = computed(() => {
  const today = new Date().toISOString().split('T')[0]
  const todayLogs = auditLogs.value.filter(l => l.created_at?.startsWith(today))
  const errorLogs = auditLogs.value.filter(l => l.status_code >= 400)
  const avgDuration = auditLogs.value.length > 0 
    ? Math.round(auditLogs.value.reduce((sum, l) => sum + (l.duration || 0), 0) / auditLogs.value.length)
    : 0
  
  return {
    total: auditLogs.value.length,
    today: todayLogs.length,
    errors: errorLogs.length,
    avgDuration
  }
})

const hasEmptyOperators = computed(() => {
  return auditLogs.value.some(l => !l.operator || l.operator === '-' || l.operator === 'unknown')
})

const isAllSelected = computed(() => {
  return pagedLogs.value.length > 0 && pagedLogs.value.every(l => selectedIds.value.includes(l.id))
})

onMounted(() => {
  loadAuditLogs()
})

async function loadAuditLogs() {
  loading.value = true
  try {
    const res = await api.get('/api/audit-logs')
    auditLogs.value = res.data || []
    selectedIds.value = []
  } catch (e) {
    appStore.showToast('加载审计日志失败', 'error')
  } finally {
    loading.value = false
  }
}

function applyFilter() {
  searchQuery.value = tempSearchQuery.value
  filterAction.value = tempFilterAction.value
  filterTargetType.value = tempFilterTargetType.value
  filterStatusCode.value = tempFilterStatusCode.value
  startTime.value = tempStartTime.value
  endTime.value = tempEndTime.value
  currentPage.value = 1
}

function resetFilter() {
  tempSearchQuery.value = ''
  tempFilterAction.value = ''
  tempFilterTargetType.value = ''
  tempFilterStatusCode.value = ''
  tempStartTime.value = ''
  tempEndTime.value = ''
  searchQuery.value = ''
  filterAction.value = ''
  filterTargetType.value = ''
  filterStatusCode.value = ''
  startTime.value = ''
  endTime.value = ''
  currentPage.value = 1
}

function formatTime(timeStr) {
  if (!timeStr) return '-'
  const d = new Date(timeStr)
  return d.toLocaleString('zh-CN', { 
    year: 'numeric', month: '2-digit', day: '2-digit',
    hour: '2-digit', minute: '2-digit', second: '2-digit',
    hour12: false 
  })
}

function getActionLabel(action) {
  const labels = {
    CREATE: '创建', INSERT: '新增', UPDATE: '更新', DELETE: '删除',
    LOGIN: '登录', LOGOUT: '退出', VIEW: '查看', EXPORT: '导出',
    create: '创建', insert: '新增', update: '更新', delete: '删除',
    login: '登录', logout: '退出', view: '查看', export: '导出'
  }
  return labels[action] || action
}

function getActionClass(action) {
  const act = action?.toUpperCase()
  if (act === 'CREATE' || act === 'INSERT') return 'success'
  if (act === 'UPDATE') return 'warning'
  if (act === 'DELETE') return 'danger'
  if (act === 'LOGIN') return 'info'
  return 'secondary'
}

function getStatusClass(code) {
  if (code >= 200 && code < 300) return 'success'
  if (code >= 400 && code < 500) return 'warning'
  if (code >= 500) return 'danger'
  return 'secondary'
}


function viewDetails(log) {
  detailLog.value = log
  showDetailModal.value = true
}

function parseChanges(log) {
  if (!log.changes) return null
  try {
    const changes = JSON.parse(log.changes)
    return Object.keys(changes).length > 0 ? changes : null
  } catch { return null }
}

function parseSnapshot(snapshot, type) {
  if (!snapshot) return null
  try {
    const data = JSON.parse(snapshot)
    return data[type] || null
  } catch { return null }
}

function isJsonObject(str) {
  if (!str || typeof str !== 'string') return false
  try {
    const parsed = JSON.parse(str)
    return typeof parsed === 'object' && parsed !== null
  } catch { return false }
}

function formatJsonData(data) {
  if (!data) return '-'
  try {
    const parsed = JSON.parse(data)
    return JSON.stringify(parsed, null, 2)
  } catch { return data }
}

function toggleSelect(id) {
  const idx = selectedIds.value.indexOf(id)
  if (idx === -1) {
    selectedIds.value.push(id)
  } else {
    selectedIds.value.splice(idx, 1)
  }
}

function toggleSelectAll() {
  if (isAllSelected.value) {
    selectedIds.value = selectedIds.value.filter(id => !pagedLogs.value.find(l => l.id === id))
  } else {
    pagedLogs.value.forEach(l => {
      if (!selectedIds.value.includes(l.id)) {
        selectedIds.value.push(l.id)
      }
    })
  }
}

async function deleteLog(log) {
  const confirmed = await appStore.showConfirm({
    type: 'danger',
    title: '确认删除',
    message: `确定要删除此审计日志吗？\nTrace ID: ${log.trace_id}`,
    confirmText: '删除',
    cancelText: '取消'
  })
  
  if (!confirmed) return
  
  try {
    await api.delete('/api/audit-logs/' + log.id)
    appStore.showToast('删除成功', 'success')
    loadAuditLogs()
  } catch (e) {
    appStore.showToast(e.response?.data?.error || '删除失败', 'error')
  }
}

async function fixEmptyOperators() {
  const confirmed = await appStore.showConfirm({
    type: 'warning',
    title: '修复历史数据',
    message: '将把所有空的用户字段更新为 "admin"。确定要执行吗？',
    confirmText: '执行修复',
    cancelText: '取消'
  })
  
  if (!confirmed) return
  
  try {
    const res = await api.post('/api/audit-logs/fix-operators')
    appStore.showToast(`修复完成，共更新 ${res.data.affected} 条记录`, 'success')
    loadAuditLogs()
  } catch (e) {
    appStore.showToast(e.response?.data?.error || '修复失败', 'error')
  }
}

async function batchDelete() {
  if (selectedIds.value.length === 0) {
    appStore.showToast('请先选择要删除的日志', 'warning')
    return
  }

  const confirmed = await appStore.showConfirm({
    type: 'danger',
    title: '确认批量删除',
    message: `确定要删除选中的 ${selectedIds.value.length} 条审计日志吗？此操作不可恢复。`,
    confirmText: '删除',
    cancelText: '取消'
  })
  
  if (!confirmed) return
  
  try {
    await api.post('/api/audit-logs/batch-delete', { ids: selectedIds.value })
    appStore.showToast('批量删除成功', 'success')
    loadAuditLogs()
  } catch (e) {
    appStore.showToast(e.response?.data?.error || '批量删除失败', 'error')
  }
}

async function exportLogs() {
  const csvContent = [
    ['时间', '用户', 'Trace ID', '操作类型', '目标类型', '请求路径', '状态码', '耗时(ms)', 'IP地址'].join(','),
    ...filteredLogs.value.map(log => [
      formatTime(log.created_at),
      log.operator || '-',
      log.trace_id || '-',
      log.action || '-',
      log.target_type || '-',
      log.path || '-',
      log.status_code || '-',
      log.duration || 0,
      log.ip || '-'
    ].join(','))
  ].join('\n')
  
  const blob = new Blob(['\ufeff' + csvContent], { type: 'text/csv;charset=utf-8;' })
  const link = document.createElement('a')
  link.href = URL.createObjectURL(blob)
  link.download = `audit-logs-${new Date().toISOString().split('T')[0]}.csv`
  link.click()
  appStore.showToast('导出成功', 'success')
}
</script>

<template>
  <div class="audit-page">
    <div class="page-title-bar">
      <h2>审计日志</h2>
      <div class="title-actions">
        <button class="btn btn-warning" @click="fixEmptyOperators" v-if="isSuperAdmin && hasEmptyOperators">
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
            <path d="M14.7 6.3a1 1 0 0 0 0 1.4l1.6 1.6a1 1 0 0 0 1.4 0l3.77-3.77a6 6 0 0 1-7.94 7.94l-6.91 6.91a2.12 2.12 0 0 1-3-3l6.91-6.91a6 6 0 0 1 7.94-7.94l-3.76 3.76z"></path>
          </svg>
          修复历史数据
        </button>
        <button class="btn btn-danger" @click="batchDelete" v-if="isSuperAdmin && selectedIds.length > 0">
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
            <polyline points="3 6 5 6 21 6"></polyline>
            <path d="M19 6v14a2 2 0 0 1-2 2H7a2 2 0 0 1-2-2V6m3 0V4a2 2 0 0 1 2-2h4a2 2 0 0 1 2 2v2"></path>
          </svg>
          删除选中 ({{ selectedIds.length }})
        </button>
        <button class="btn btn-secondary" @click="loadAuditLogs" :disabled="loading">
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
            <polyline points="23 4 23 10 17 10"></polyline>
            <path d="M20.49 15a9 9 0 1 1-2.12-9.36L23 10"></path>
          </svg>
          刷新
        </button>
        <button class="btn btn-secondary" @click="exportLogs">
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
            <path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4"></path>
            <polyline points="7 10 12 15 17 10"></polyline>
            <line x1="12" y1="15" x2="12" y2="3"></line>
          </svg>
          导出
        </button>
      </div>
    </div>

    <!-- 统计卡片 -->
    <div class="stats-cards">
      <div class="stat-card orange">
        <div class="stat-value">{{ stats.total }}</div>
        <div class="stat-label">总日志数</div>
      </div>
      <div class="stat-card green">
        <div class="stat-value">{{ stats.today }}</div>
        <div class="stat-label">今日新增</div>
      </div>
      <div class="stat-card gray">
        <div class="stat-value">{{ stats.errors }}</div>
        <div class="stat-label">错误日志</div>
      </div>
      <div class="stat-card blue">
        <div class="stat-value">{{ stats.avgDuration }}ms</div>
        <div class="stat-label">平均耗时</div>
      </div>
    </div>

    <!-- 筛选栏 -->
    <div class="filter-bar">
      <input
        type="text"
        v-model="tempSearchQuery"
        placeholder="搜索路径、Trace ID、操作者..."
        class="search-input"
        @keyup.enter="applyFilter"
      />
      <select v-model="tempFilterAction" class="filter-select">
        <option value="">操作类型</option>
        <option v-for="action in actionTypes" :key="action" :value="action">
          {{ getActionLabel(action) }}
        </option>
      </select>
      <select v-model="tempFilterTargetType" class="filter-select">
        <option value="">目标类型</option>
        <option v-for="t in targetTypes" :key="t" :value="t">{{ t }}</option>
      </select>
      <select v-model="tempFilterStatusCode" class="filter-select">
        <option value="">状态码</option>
        <option value="success">成功 (2xx)</option>
        <option value="error">错误 (4xx/5xx)</option>
      </select>
      <input type="date" v-model="tempStartTime" class="filter-input" placeholder="开始时间" />
      <input type="date" v-model="tempEndTime" class="filter-input" placeholder="结束时间" />
      <button class="btn-search" @click="applyFilter">搜 索</button>
      <button class="btn-reset" @click="resetFilter">重 置</button>
    </div>
    
    <!-- 表格 -->
    <div class="table-container">
      <table class="data-table">
        <thead>
          <tr>
            <th width="40" v-if="isSuperAdmin">
              <input type="checkbox" :checked="isAllSelected" @change="toggleSelectAll" class="checkbox" />
            </th>
            <th width="160">时间</th>
            <th width="80">用户</th>
            <th width="160">Trace ID</th>
            <th width="80">操作类型</th>
            <th class="request-th">请求信息</th>
            <th width="70">状态码</th>
            <th width="70">耗时</th>
            <th width="120">IP地址</th>
            <th width="100">操作</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="log in pagedLogs" :key="log.id" :class="{ 'row-selected': selectedIds.includes(log.id) }">
            <td v-if="isSuperAdmin">
              <input type="checkbox" :checked="selectedIds.includes(log.id)" @change="toggleSelect(log.id)" class="checkbox" />
            </td>
            <td class="time-cell">{{ formatTime(log.created_at) }}</td>
            <td>
              <span class="user-badge" :class="{ system: log.operator === 'system' }">{{ log.operator || '-' }}</span>
            </td>
            <td class="trace-cell" :title="log.trace_id">{{ log.trace_id || '-' }}</td>
            <td>
              <span class="action-badge" :class="getActionClass(log.action)">
                {{ getActionLabel(log.action) }}
              </span>
            </td>
            <td class="request-cell" :title="(log.method || '') + ' ' + (log.path || '')">
              <span class="method-badge">{{ log.method || '-' }}</span>
              <span class="path-text">{{ log.path || '-' }}</span>
            </td>
            <td>
              <span class="status-badge" :class="getStatusClass(log.status_code)">
                {{ log.status_code || '-' }}
              </span>
            </td>
            <td class="duration-cell">{{ log.duration || 0 }}ms</td>
            <td class="ip-cell">{{ log.ip || '-' }}</td>
            <td class="actions-cell">
              <button class="btn-icon view" @click="viewDetails(log)" title="查看详情">
                <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                  <path d="M1 12s4-8 11-8 11 8 11 8-4 8-11 8-11-8-11-8z"></path>
                  <circle cx="12" cy="12" r="3"></circle>
                </svg>
              </button>
              <button class="btn-icon danger" @click="deleteLog(log)" title="删除" v-if="isSuperAdmin">
                <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                  <polyline points="3 6 5 6 21 6"></polyline>
                  <path d="M19 6v14a2 2 0 0 1-2 2H7a2 2 0 0 1-2-2V6m3 0V4a2 2 0 0 1 2-2h4a2 2 0 0 1 2 2v2"></path>
                </svg>
              </button>
            </td>
          </tr>
          <tr v-if="pagedLogs.length === 0 && !loading">
            <td :colspan="isSuperAdmin ? 10 : 9" class="empty">暂无审计记录</td>
          </tr>
          <tr v-if="loading">
            <td :colspan="isSuperAdmin ? 10 : 9" class="empty">加载中...</td>
          </tr>
        </tbody>
      </table>
    </div>
    
    <!-- 分页 -->
    <div class="pagination-bar" v-if="totalPages >= 1">
      <div class="pagination-info">
        共 {{ filteredLogs.length }} 条，每页
        <select class="page-size-select" v-model="pageSize" @change="currentPage = 1">
          <option :value="10">10</option>
          <option :value="20">20</option>
          <option :value="50">50</option>
          <option :value="100">100</option>
        </select>
        条
      </div>
      <div class="pagination">
        <button @click="currentPage = 1" :disabled="currentPage <= 1">首页</button>
        <button @click="currentPage--" :disabled="currentPage <= 1">上一页</button>
        <span class="page-info">第 {{ currentPage }} / {{ totalPages }} 页</span>
        <button @click="currentPage++" :disabled="currentPage >= totalPages">下一页</button>
        <button @click="currentPage = totalPages" :disabled="currentPage >= totalPages">末页</button>
      </div>
    </div>

    <!-- 审计日志详情弹窗 -->
    <Teleport to="body">
      <div class="modal-overlay" :class="{ active: showDetailModal }">
        <div class="modal detail-modal">
          <div class="modal-header">
            <h2>审计日志详情</h2>
            <button class="modal-close" @click="showDetailModal = false">
              <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><line x1="18" y1="6" x2="6" y2="18"/><line x1="6" y1="6" x2="18" y2="18"/></svg>
            </button>
          </div>
          <div class="modal-body" v-if="detailLog">
            <!-- 基本信息 -->
            <div class="detail-section">
              <h3 class="section-title">基本信息</h3>
              <div class="info-grid">
                <div class="info-item">
                  <div class="info-label">日志ID</div>
                  <div class="info-value code">{{ detailLog.id }}</div>
                </div>
                <div class="info-item">
                  <div class="info-label">用户</div>
                  <div class="info-value"><span class="user-badge" :class="{ system: detailLog.operator === 'system' }">{{ detailLog.operator || '-' }}</span></div>
                </div>
                <div class="info-item">
                  <div class="info-label">Trace ID</div>
                  <div class="info-value code">{{ detailLog.trace_id || '-' }}</div>
                </div>
                <div class="info-item">
                  <div class="info-label">IP地址</div>
                  <div class="info-value code">{{ detailLog.ip || '-' }}</div>
                </div>
                <div class="info-item">
                  <div class="info-label">操作类型</div>
                  <div class="info-value"><span class="action-badge" :class="getActionClass(detailLog.action)">{{ detailLog.action?.toUpperCase() }}</span></div>
                </div>
                <div class="info-item">
                  <div class="info-label">目标类型</div>
                  <div class="info-value">{{ detailLog.target_type || '-' }}</div>
                </div>
                <div class="info-item">
                  <div class="info-label">目标ID</div>
                  <div class="info-value code">{{ detailLog.target_id || '-' }}</div>
                </div>
                <div class="info-item">
                  <div class="info-label">操作时间</div>
                  <div class="info-value">{{ formatTime(detailLog.created_at) }}</div>
                </div>
              </div>
            </div>

            <!-- 请求信息 -->
            <div class="detail-section">
              <h3 class="section-title">请求信息</h3>
              <div class="info-grid">
                <div class="info-item">
                  <div class="info-label">请求方法</div>
                  <div class="info-value"><span class="method-badge">{{ detailLog.method || '-' }}</span></div>
                </div>
                <div class="info-item full">
                  <div class="info-label">请求路径</div>
                  <div class="info-value code path">{{ detailLog.path || '-' }}</div>
                </div>
                <div class="info-item">
                  <div class="info-label">状态码</div>
                  <div class="info-value"><span class="status-badge" :class="getStatusClass(detailLog.status_code)">{{ detailLog.status_code || '-' }}</span></div>
                </div>
                <div class="info-item">
                  <div class="info-label">响应时间</div>
                  <div class="info-value"><span class="duration-badge">{{ detailLog.duration || 0 }}ms</span></div>
                </div>
              </div>
            </div>

            <!-- 变更详情 (字符串类型的变更描述) -->
            <div class="detail-section" v-if="detailLog.changes && !isJsonObject(detailLog.changes)">
              <h3 class="section-title">变更说明</h3>
              <div class="change-desc">{{ detailLog.changes }}</div>
            </div>

            <!-- 变更详情 (JSON格式的字段对比) -->
            <div class="detail-section" v-if="parseChanges(detailLog)">
              <h3 class="section-title">变更详情</h3>
              <div class="changes-list">
                <div v-for="(val, key) in parseChanges(detailLog)" :key="key" class="change-row">
                  <span class="change-field">{{ key }}:</span>
                  <span class="change-old">{{ val.old || '-' }}</span>
                  <span class="change-arrow">→</span>
                  <span class="change-new">{{ val.new || '-' }}</span>
                </div>
              </div>
            </div>

            <!-- 数据快照 (操作前后对比) -->
            <div class="detail-section" v-if="detailLog.old_data || detailLog.new_data">
              <h3 class="section-title">数据快照</h3>
              <div class="snapshot-tabs">
                <div class="snapshot-block" v-if="detailLog.old_data">
                  <div class="snapshot-label before">操作前</div>
                  <pre class="snapshot-code">{{ formatJsonData(detailLog.old_data) }}</pre>
                </div>
                <div class="snapshot-block" v-if="detailLog.new_data">
                  <div class="snapshot-label after">操作后</div>
                  <pre class="snapshot-code">{{ formatJsonData(detailLog.new_data) }}</pre>
                </div>
              </div>
            </div>
          </div>
          <div class="modal-footer">
            <button class="btn btn-secondary" @click="showDetailModal = false">关闭</button>
          </div>
        </div>
      </div>
    </Teleport>
  </div>
</template>

<style scoped>
.audit-page { display: flex; flex-direction: column; gap: 20px; }

.page-title-bar {
  display: flex;
  justify-content: space-between;
  align-items: center;
}

.page-title-bar h2 {
  font-size: 20px;
  font-weight: 600;
  margin: 0;
}

.title-actions {
  display: flex;
  gap: 12px;
}

/* 统计卡片 */
.stats-cards {
  display: grid;
  grid-template-columns: repeat(4, 1fr);
  gap: 16px;
}

.stat-card {
  padding: 24px;
  border-radius: 12px;
  text-align: center;
  border: 1px solid var(--border-color);
}

.stat-card .stat-value {
  font-size: 32px;
  font-weight: 700;
  margin-bottom: 6px;
}

.stat-card .stat-label {
  font-size: 13px;
  color: var(--text-muted);
}

.stat-card.orange {
  background: linear-gradient(135deg, rgba(249, 115, 22, 0.1), rgba(249, 115, 22, 0.05));
  border-color: rgba(249, 115, 22, 0.3);
}
.stat-card.orange .stat-value { color: #f97316; }

.stat-card.green {
  background: linear-gradient(135deg, rgba(16, 185, 129, 0.1), rgba(16, 185, 129, 0.05));
  border-color: rgba(16, 185, 129, 0.3);
}
.stat-card.green .stat-value { color: #10b981; }

.stat-card.gray {
  background: linear-gradient(135deg, rgba(107, 114, 128, 0.1), rgba(107, 114, 128, 0.05));
  border-color: rgba(107, 114, 128, 0.3);
}
.stat-card.gray .stat-value { color: #6b7280; }

.stat-card.blue {
  background: linear-gradient(135deg, rgba(59, 130, 246, 0.1), rgba(59, 130, 246, 0.05));
  border-color: rgba(59, 130, 246, 0.3);
}
.stat-card.blue .stat-value { color: #3b82f6; }

/* 筛选栏 */
.filter-bar {
  display: flex;
  gap: 12px;
  flex-wrap: wrap;
  align-items: center;
  padding: 16px;
  background: var(--bg-card);
  border: 1px solid var(--border-color);
  border-radius: 12px;
}

.search-input {
  padding: 8px 14px;
  flex: 1;
  min-width: 200px;
  background: var(--bg-input);
  border: 1px solid var(--border-color);
  border-radius: 8px;
  color: var(--text-primary);
  font-size: 14px;
}

.filter-select, .filter-input {
  padding: 8px 12px;
  background: var(--bg-input);
  border: 1px solid var(--border-color);
  border-radius: 8px;
  color: var(--text-primary);
  font-size: 13px;
  cursor: pointer;
}

.filter-input { width: 140px; }

.btn-search { padding: 8px 20px; border-radius: 8px; border: none; background: #3a84ff; color: #fff; font-size: 14px; font-weight: 500; cursor: pointer; transition: all 0.2s; }
.btn-search:hover { background: #2970e6; }
.btn-reset { padding: 8px 20px; border-radius: 8px; border: 1px solid var(--border-color); background: var(--bg-input); color: var(--text-primary); font-size: 14px; cursor: pointer; transition: all 0.2s; }
.btn-reset:hover { background: var(--bg-hover); }

.btn {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  padding: 8px 16px;
  border: none;
  border-radius: 8px;
  cursor: pointer;
  font-size: 14px;
  font-weight: 500;
  transition: all 0.2s;
}

.btn svg { width: 16px; height: 16px; }

.btn-primary { background: var(--primary); color: white; }
.btn-primary:hover { background: #2563eb; }

.btn-danger { background: #ef4444; color: white; }
.btn-danger:hover { background: #dc2626; }
.btn-warning { background: #f59e0b; color: white; }
.btn-warning:hover { background: #d97706; }

.btn-sm { padding: 8px 16px; font-size: 13px; }
.btn-secondary { background: var(--bg-hover); color: var(--text-primary); border: 1px solid var(--border-color); }
.btn-text { background: transparent; color: var(--text-muted); padding: 8px 12px; }
.btn-text:hover { color: var(--primary); }

/* 表格 */
.table-container {
  background: var(--bg-card);
  border: 1px solid var(--border-color);
  border-radius: 12px;
  overflow-x: auto;
}

.data-table {
  width: 100%;
  border-collapse: collapse;
  min-width: 1200px;
}

.data-table th, .data-table td {
  padding: 12px 14px;
  text-align: left;
  border-bottom: 1px solid var(--border-color);
  font-size: 13px;
}

.data-table th {
  background: var(--bg-hover);
  font-weight: 600;
  color: var(--text-secondary);
  white-space: nowrap;
}

.data-table tr:hover { background: var(--bg-hover); }
.row-selected { background: rgba(59, 130, 246, 0.1) !important; }

.checkbox {
  width: 16px;
  height: 16px;
  cursor: pointer;
}

.time-cell {
  font-size: 12px;
  color: var(--text-secondary);
  white-space: nowrap;
}

.trace-cell {
  font-family: 'Fira Code', monospace;
  font-size: 11px;
  color: var(--text-muted);
  max-width: 160px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.user-badge {
  padding: 3px 10px;
  border-radius: 6px;
  background: rgba(99, 102, 241, 0.15);
  color: #818cf8;
  font-size: 12px;
  font-weight: 500;
  white-space: nowrap;
}

.user-badge.system {
  background: rgba(156, 163, 175, 0.2);
  color: #9ca3af;
  font-style: italic;
}

.action-badge {
  display: inline-block;
  padding: 3px 10px;
  border-radius: 4px;
  font-size: 12px;
  font-weight: 500;
}

.action-badge.success { background: rgba(16, 185, 129, 0.15); color: #10b981; }
.action-badge.warning { background: rgba(245, 158, 11, 0.15); color: #f59e0b; }
.action-badge.danger { background: rgba(239, 68, 68, 0.15); color: #ef4444; }
.action-badge.info { background: rgba(6, 182, 212, 0.15); color: #06b6d4; }
.action-badge.secondary { background: var(--bg-hover); color: var(--text-muted); }

.request-th { min-width: 300px; }

.request-cell {
  display: flex;
  align-items: center;
  gap: 8px;
}

.method-badge {
  padding: 2px 8px;
  border-radius: 4px;
  background: rgba(139, 92, 246, 0.15);
  color: #a78bfa;
  font-size: 11px;
  font-weight: 600;
  flex-shrink: 0;
}

.path-text {
  font-family: 'Fira Code', monospace;
  font-size: 12px;
  color: var(--text-secondary);
  word-break: break-all;
}

.status-badge {
  display: inline-block;
  padding: 3px 10px;
  border-radius: 4px;
  font-size: 12px;
  font-weight: 600;
}

.status-badge.success { background: rgba(16, 185, 129, 0.15); color: #10b981; }
.status-badge.warning { background: rgba(245, 158, 11, 0.15); color: #f59e0b; }
.status-badge.danger { background: rgba(239, 68, 68, 0.15); color: #ef4444; }
.status-badge.secondary { background: var(--bg-hover); color: var(--text-muted); }

.duration-cell {
  font-family: 'Fira Code', monospace;
  font-size: 12px;
  color: var(--text-secondary);
  white-space: nowrap;
}

.ip-cell {
  font-family: 'Fira Code', monospace;
  font-size: 12px;
  color: var(--text-secondary);
  white-space: nowrap;
}

.actions-cell {
  white-space: nowrap;
  display: flex;
  gap: 4px;
}

.btn-icon {
  width: 28px;
  height: 28px;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  border: none;
  border-radius: 6px;
  cursor: pointer;
  transition: all 0.2s;
}

.btn-icon svg { width: 14px; height: 14px; }

.btn-icon.view { background: rgba(59, 130, 246, 0.15); color: #3b82f6; }
.btn-icon.view:hover { background: rgba(59, 130, 246, 0.25); }

.btn-icon.danger { background: rgba(239, 68, 68, 0.15); color: #ef4444; }
.btn-icon.danger:hover { background: rgba(239, 68, 68, 0.25); }

.text-muted { color: var(--text-muted); }
.empty { text-align: center; color: var(--text-muted); padding: 40px !important; }

/* 分页 */
.pagination-bar {
  display: flex;
  justify-content: space-between;
  align-items: center;
  flex-wrap: wrap;
  gap: 12px;
}

.pagination-info {
  color: var(--text-muted);
  font-size: 14px;
  display: flex;
  align-items: center;
  gap: 6px;
}

.page-size-select {
  padding: 4px 8px;
  border: 1px solid var(--border-color);
  border-radius: 6px;
  background: var(--bg-card);
  color: var(--text-primary);
  font-size: 13px;
  cursor: pointer;
}

.pagination {
  display: flex;
  align-items: center;
  gap: 8px;
}

.pagination button {
  padding: 8px 14px;
  background: var(--bg-card);
  border: 1px solid var(--border-color);
  border-radius: 6px;
  color: var(--text-primary);
  cursor: pointer;
  font-size: 13px;
}

.pagination button:hover:not(:disabled) { border-color: var(--primary); color: var(--primary); }
.pagination button:disabled { opacity: 0.5; cursor: not-allowed; }
.page-info { color: var(--text-muted); font-size: 13px; }

@media (max-width: 1024px) {
  .stats-cards { grid-template-columns: repeat(2, 1fr); }
}

@media (max-width: 640px) {
  .stats-cards { grid-template-columns: 1fr; }
}

/* 详情弹窗 - 基础样式由全局 base.css 控制 */
.modal-overlay.active { display: flex !important; }
.modal { background: var(--bg-card); border-radius: 16px; border: 1px solid var(--border-color); max-width: 100%; max-height: 100%; display: flex; flex-direction: column; box-shadow: 0 25px 50px rgba(0, 0, 0, 0.4); overflow: hidden; }
.detail-modal { width: 700px; }
.modal-header { display: flex; align-items: center; justify-content: space-between; padding: 20px 24px; border-bottom: 1px solid var(--border-color); flex-shrink: 0; }
.modal-header h2 { font-size: 18px; font-weight: 600; margin: 0; }
.modal-close { width: 32px; height: 32px; border-radius: 8px; border: none; background: var(--bg-hover); color: var(--text-secondary); cursor: pointer; display: flex; align-items: center; justify-content: center; }
.modal-close:hover { background: #ef4444; color: #fff; }
.modal-close svg { width: 16px; height: 16px; }
.modal-body { padding: 20px 24px; overflow-y: auto; flex: 1; }
.modal-footer { display: flex; justify-content: flex-end; gap: 12px; padding: 16px 24px; border-top: 1px solid var(--border-color); flex-shrink: 0; }

.detail-section { margin-bottom: 24px; }
.detail-section:last-child { margin-bottom: 0; }
.section-title { font-size: 15px; font-weight: 600; color: var(--text-primary); margin: 0 0 16px 0; padding-bottom: 8px; border-bottom: 1px solid var(--border-color); }

.info-grid { display: grid; grid-template-columns: 1fr 1fr; gap: 12px; }
.info-item { background: var(--bg-hover); border-radius: 10px; padding: 14px 16px; }
.info-item.full { grid-column: span 2; }
.info-label { font-size: 12px; color: var(--text-muted); margin-bottom: 6px; }
.info-value { font-size: 14px; color: var(--text-primary); font-weight: 500; }
.info-value.code { font-family: 'Monaco', 'Consolas', monospace; font-size: 13px; word-break: break-all; }
.info-value.path { color: #3b82f6; }

.duration-badge { display: inline-block; padding: 3px 10px; border-radius: 6px; background: rgba(16, 185, 129, 0.15); color: #10b981; font-size: 13px; font-weight: 500; }

.changes-list { display: flex; flex-direction: column; gap: 8px; }
.change-row { display: flex; align-items: center; gap: 10px; padding: 10px 14px; background: var(--bg-hover); border-radius: 8px; font-size: 13px; }
.change-field { color: var(--text-secondary); font-weight: 500; min-width: 80px; }
.change-old { color: #ef4444; text-decoration: line-through; opacity: 0.8; }
.change-arrow { color: var(--text-muted); }
.change-new { color: #10b981; font-weight: 500; }

.change-desc { padding: 12px 16px; background: var(--bg-hover); border-radius: 8px; font-size: 14px; color: var(--text-primary); }

.snapshot-tabs { display: flex; flex-direction: column; gap: 16px; }
.snapshot-block { }
.snapshot-label { display: inline-block; font-size: 12px; color: var(--text-muted); margin-bottom: 8px; font-weight: 600; padding: 4px 10px; border-radius: 4px; }
.snapshot-label.before { background: rgba(239, 68, 68, 0.15); color: #ef4444; }
.snapshot-label.after { background: rgba(16, 185, 129, 0.15); color: #10b981; }
.snapshot-code { background: #1a1f2e; border-radius: 10px; padding: 16px; font-family: 'Monaco', 'Consolas', monospace; font-size: 12px; color: #e2e8f0; overflow-x: auto; white-space: pre-wrap; word-break: break-all; max-height: 250px; margin: 0; overflow-y: auto; }
</style>
