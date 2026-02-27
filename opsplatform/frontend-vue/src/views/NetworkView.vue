<script setup>
import { ref, computed, onMounted } from 'vue'
import { useAppStore, useAuthStore } from '@/stores'
import api from '@/api'

const appStore = useAppStore()
const authStore = useAuthStore()

const records = ref([])
const loading = ref(false)

const tempSearchQuery = ref('')
const tempProjectFilter = ref('')
const tempEnvFilter = ref('')
const tempStatusFilter = ref('')
const tempDuplicateFilter = ref('')

const searchQuery = ref('')
const projectFilter = ref('')
const envFilter = ref('')
const statusFilter = ref('')
const duplicateFilter = ref('')

const currentPage = ref(1)
const pageSize = ref(20)
const jumpPage = ref(1)
const selectedRecords = ref([])
const sortField = ref('')
const sortOrder = ref('asc')

const showRecordModal = ref(false)
const recordModalMode = ref('add')
const recordForm = ref({
  id: '', connection_id: '', project: '', env: 'uat', module: '', vid: '',
  src_ip: '', src_port: '', dest_ip: '', dest_port: '', status: 'active'
})

const showBatchModal = ref(false)
const batchText = ref('')
const batchRecords = ref([])
const batchError = ref('')
const batchEnv = ref('uat')
const batchStatus = ref('active')
const batchLoading = ref(false)

const showBatchCheckModal = ref(false)
const batchCheckText = ref('')
const batchCheckRecords = ref([])
const batchCheckError = ref('')
const batchCheckLoading = ref(false)
const batchCheckResult = ref(null)

const showHistoryModal = ref(false)
const historyRecord = ref(null)
const recordHistories = ref([])
const showPreviewModal = ref(false)
const previewData = ref(null)
const previewHistory = ref(null)
const previewVersion = ref(0)

const activePopconfirm = ref(null)

const projectList = computed(() => {
  const projects = [...new Set((records.value || []).map(r => r.project).filter(Boolean))]
  return projects.sort((a, b) => a.localeCompare(b, 'zh-CN'))
})

const filteredRecords = computed(() => {
  let r = records.value || []
  if (projectFilter.value) r = r.filter(x => x.project === projectFilter.value)
  if (envFilter.value) r = r.filter(x => x.env === envFilter.value)
  if (statusFilter.value) r = r.filter(x => x.status === statusFilter.value)
  
  if (duplicateFilter.value) {
    const connIdCount = {}
    records.value.forEach(rec => {
      if (rec.connection_id) connIdCount[rec.connection_id] = (connIdCount[rec.connection_id] || 0) + 1
    })
    if (duplicateFilter.value === 'duplicate') r = r.filter(x => connIdCount[x.connection_id] > 1)
    else if (duplicateFilter.value === 'unique') r = r.filter(x => connIdCount[x.connection_id] === 1)
  }
  
  if (searchQuery.value) {
    const q = searchQuery.value.toLowerCase()
    r = r.filter(x => [x.connection_id, x.vid, x.src_ip, x.dest_ip, x.src_port, x.dest_port].some(v => v && String(v).toLowerCase().includes(q)))
  }
  
  if (sortField.value) {
    r = [...r].sort((a, b) => {
      const va = a[sortField.value] || '', vb = b[sortField.value] || ''
      const cmp = va.localeCompare ? va.localeCompare(vb, 'zh-CN') : (va > vb ? 1 : -1)
      return sortOrder.value === 'desc' ? -cmp : cmp
    })
  }
  return r
})

const paginatedRecords = computed(() => {
  const start = (currentPage.value - 1) * pageSize.value
  return filteredRecords.value.slice(start, start + pageSize.value)
})

const totalPages = computed(() => Math.max(1, Math.ceil(filteredRecords.value.length / pageSize.value)))

const displayedPages = computed(() => {
  const total = totalPages.value, current = currentPage.value, pages = []
  if (total <= 7) { for (let i = 1; i <= total; i++) pages.push(i) }
  else {
    if (current <= 4) { for (let i = 1; i <= 5; i++) pages.push(i); pages.push('...'); pages.push(total) }
    else if (current >= total - 3) { pages.push(1); pages.push('...'); for (let i = total - 4; i <= total; i++) pages.push(i) }
    else { pages.push(1); pages.push('...'); for (let i = current - 1; i <= current + 1; i++) pages.push(i); pages.push('...'); pages.push(total) }
  }
  return pages
})

const isAllSelected = computed(() => {
  const filtered = paginatedRecords.value || []
  return filtered.length > 0 && filtered.every(r => selectedRecords.value.includes(r.id))
})

const statsData = computed(() => ({
  total: records.value.length,
  active: records.value.filter(r => r.status === 'active').length,
  pending: records.value.filter(r => r.status === 'pending').length,
  inactive: records.value.filter(r => r.status === 'inactive').length
}))

onMounted(() => { loadRecords() })

async function loadRecords() {
  loading.value = true
  try {
    const res = await api.get('/api/records')
    records.value = res.data || []
  } catch (e) {
    appStore.showToast('加载记录失败', 'error')
  } finally { loading.value = false }
}

function toggleSort(field) {
  if (sortField.value === field) sortOrder.value = sortOrder.value === 'asc' ? 'desc' : 'asc'
  else { sortField.value = field; sortOrder.value = 'asc' }
}

function getSortIcon(field) {
  if (sortField.value !== field) return '⇅'
  return sortOrder.value === 'asc' ? '↑' : '↓'
}

function toggleSelectAll(e) {
  const pageIds = paginatedRecords.value.map(r => r.id)
  if (e.target.checked) selectedRecords.value = [...new Set([...selectedRecords.value, ...pageIds])]
  else selectedRecords.value = selectedRecords.value.filter(id => !pageIds.includes(id))
}

function applyFilter() {
  searchQuery.value = tempSearchQuery.value
  projectFilter.value = tempProjectFilter.value
  envFilter.value = tempEnvFilter.value
  statusFilter.value = tempStatusFilter.value
  duplicateFilter.value = tempDuplicateFilter.value
  currentPage.value = 1
}

function resetRecordFilter() {
  tempSearchQuery.value = ''
  tempProjectFilter.value = ''
  tempEnvFilter.value = ''
  tempStatusFilter.value = ''
  tempDuplicateFilter.value = ''
  searchQuery.value = ''
  projectFilter.value = ''
  envFilter.value = ''
  statusFilter.value = ''
  duplicateFilter.value = ''
  currentPage.value = 1
}

function goToPage() {
  const page = parseInt(jumpPage.value) || 1
  if (page >= 1 && page <= totalPages.value) currentPage.value = page
  jumpPage.value = currentPage.value
}

function formatDate(dateStr) {
  if (!dateStr) return '-'
  const d = new Date(dateStr)
  if (isNaN(d.getTime())) return '-'
  const pad = n => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}:${pad(d.getSeconds())}`
}

function formatDateTime(dateStr) {
  if (!dateStr) return '-'
  return dateStr.replace('T', ' ').substring(0, 19)
}

function getStatusText(status) {
  return { active: '启用', pending: '待定', inactive: '停用' }[status] || status
}

function getActionText(action) {
  return { create: '创建', update: '修改', delete: '删除' }[action] || action
}

function openRecordModal(mode, record = null) {
  recordModalMode.value = mode
  recordForm.value = record ? { ...record } : { id: '', connection_id: '', project: '', env: 'uat', module: '', vid: '', src_ip: '', src_port: '', dest_ip: '', dest_port: '', status: 'active' }
  showRecordModal.value = true
}

async function saveRecord() {
  if (!recordForm.value.connection_id) { appStore.showToast('请输入连接ID', 'error'); return }
  const currentUser = authStore.user?.username || 'admin'
  try {
    if (recordModalMode.value === 'edit') {
      await api.put(`/api/records/${recordForm.value.id}`, { record: recordForm.value, operator: currentUser })
      appStore.showToast('更新成功', 'success')
    } else {
      await api.post('/api/records', { record: recordForm.value, operator: currentUser })
      appStore.showToast('添加成功', 'success')
    }
    showRecordModal.value = false
    loadRecords()
  } catch (e) {
    const msg = e.response?.data || e.message || '保存失败'
    appStore.showToast(typeof msg === 'string' ? msg : '保存失败', 'error')
    console.error('Save record error:', e)
  }
}

function togglePopconfirm(type, id) {
  const key = `${type}-${id}`
  activePopconfirm.value = activePopconfirm.value === key ? null : key
}

async function deleteRecord(record) {
  activePopconfirm.value = null
  try {
    await api.delete(`/api/records/${record.id}`, { data: { operator: authStore.user?.username || 'admin' } })
    appStore.showToast('删除成功', 'success')
    loadRecords()
  } catch (e) {
    const msg = e.response?.data || e.message || '删除失败'
    appStore.showToast(typeof msg === 'string' ? msg : '删除失败', 'error')
    console.error('Delete record error:', e)
  }
}

async function openRecordHistory(record) {
  historyRecord.value = record
  recordHistories.value = []
  showHistoryModal.value = true
  try {
    const res = await api.get(`/api/records/${record.id}/history`)
    recordHistories.value = res.data || []
  } catch (e) { appStore.showToast('加载历史记录失败', 'error') }
}

function parseChanges(changesStr) {
  if (!changesStr) return {}
  try { return JSON.parse(changesStr) } catch { return {} }
}

function getFieldLabel(field) {
  const map = { connection_id: '连接ID', project: '项目', env: '环境', module: '模块名', vid: 'VID', src_ip: '源IP', src_port: '源端口', dest_ip: '目标IP', dest_port: '目标端口', status: '状态' }
  return map[field] || field
}

function previewHistoryVersion(history, idx) {
  try {
    previewData.value = JSON.parse(history.snapshot)
    previewHistory.value = history
    previewVersion.value = recordHistories.value.length - idx
    showPreviewModal.value = true
  } catch (e) { appStore.showToast('解析快照失败', 'error') }
}

async function rollbackToVersion(history) {
  const confirmed = await appStore.showConfirm({ type: 'warning', title: '确认回滚', message: `确定要回滚到此版本吗？`, okText: '确认回滚', cancelText: '取消' })
  if (!confirmed) return
  const currentUser = authStore.user?.username || 'admin'
  try {
    await api.post(`/api/records/${historyRecord.value.id}/rollback`, { history_id: history.id, operator: currentUser })
    appStore.showToast('回滚成功', 'success')
    showHistoryModal.value = false
    loadRecords()
  } catch (e) { 
    const msg = e.response?.data || e.message || '回滚失败'
    appStore.showToast(typeof msg === 'string' ? msg : '回滚失败', 'error')
  }
}

function openBatchModal() {
  batchText.value = ''; batchRecords.value = []; batchError.value = ''; showBatchModal.value = true
}

function parseBatchText() {
  batchError.value = ''; batchRecords.value = []
  if (!batchText.value.trim()) return
  const lines = batchText.value.trim().split('\n'), seenConnIds = new Map(), duplicateErrors = []
  
  for (let i = 0; i < lines.length; i++) {
    const line = lines[i].trim()
    if (!line || line.startsWith('#')) continue
    let parts
    if (line.includes('\t')) parts = line.split('\t')
    else if (line.includes(',')) parts = line.split(',')
    else if (line.includes('|')) parts = line.split('|')
    else parts = line.split(/\s+/)
    parts = parts.map(p => p.trim()).filter(p => p)
    
    if (parts.length < 6) { batchError.value = `第 ${i + 1} 行: 需要6个字段`; return }
    
    const connId = parts[5]
    if (seenConnIds.has(connId)) duplicateErrors.push(`第 ${i + 1} 行连接ID重复: "${connId}"`)
    else seenConnIds.set(connId, { line: i + 1 })
    
    const srcAddr = parts[3].split(':'), destAddr = parts[4].split(':')
    if (srcAddr.length !== 2 || destAddr.length !== 2) { batchError.value = `第 ${i + 1} 行: 地址格式应为 IP:端口`; return }
    
    batchRecords.value.push({ project: parts[0], module: parts[1], vid: parts[2].replace(/;/g, '\n'), src_ip: srcAddr[0], src_port: srcAddr[1], dest_ip: destAddr[0], dest_port: destAddr[1], connection_id: parts[5], env: batchEnv.value, status: batchStatus.value })
  }
  
  if (duplicateErrors.length > 0) { batchError.value = duplicateErrors.join('\n'); batchRecords.value = [] }
}

async function submitBatch() {
  if (batchRecords.value.length === 0) return
  batchLoading.value = true
  try {
    const res = await api.post('/api/records/batch', { records: batchRecords.value, operator: authStore.user?.username })
    appStore.showToast(res.data.message || '批量添加成功', 'success')
    showBatchModal.value = false
    loadRecords()
  } catch (e) { appStore.showToast('批量添加失败', 'error') }
  finally { batchLoading.value = false }
}

function openBatchCheckModal() {
  batchCheckText.value = ''; batchCheckRecords.value = []; batchCheckError.value = ''; batchCheckResult.value = null; showBatchCheckModal.value = true
}

function parseBatchCheckText() {
  batchCheckError.value = ''; batchCheckRecords.value = []; batchCheckResult.value = null
  if (!batchCheckText.value.trim()) return
  const lines = batchCheckText.value.trim().split('\n')
  for (let i = 0; i < lines.length; i++) {
    const line = lines[i].trim()
    if (!line || line.startsWith('#')) continue
    let parts
    if (line.includes('\t')) parts = line.split('\t')
    else if (line.includes(',')) parts = line.split(',')
    else if (line.includes('|')) parts = line.split('|')
    else parts = line.split(/\s+/)
    parts = parts.map(p => p.trim()).filter(p => p)
    if (parts.length < 6) { batchCheckError.value = `第 ${i + 1} 行: 需要6个字段`; return }
    const srcAddr = parts[3].split(':'), destAddr = parts[4].split(':')
    if (srcAddr.length !== 2 || destAddr.length !== 2) { batchCheckError.value = `第 ${i + 1} 行: 地址格式应为 IP:端口`; return }
    batchCheckRecords.value.push({ project: parts[0], module: parts[1], vid: parts[2].replace(/;/g, '\n'), src_ip: srcAddr[0], src_port: srcAddr[1], dest_ip: destAddr[0], dest_port: destAddr[1], connection_id: parts[5] })
  }
}

async function doBatchCheck() {
  if (batchCheckRecords.value.length === 0) return
  batchCheckLoading.value = true
  try {
    const res = await api.post('/api/records/batch-check', { records: batchCheckRecords.value })
    batchCheckResult.value = res.data
    appStore.showToast(`检测完成：${res.data.new_count} 条可添加，${res.data.exists_count} 条已存在`, res.data.exists_count > 0 ? 'warning' : 'success')
  } catch (e) { appStore.showToast('检测失败', 'error') }
  finally { batchCheckLoading.value = false }
}

function copyCheckResult(type) {
  const items = type === 'exists' ? batchCheckResult.value?.exists : batchCheckResult.value?.new
  if (!items?.length) return
  const text = items.map(r => `${r.project}\t${r.module}\t${r.vid}\t${r.src_addr}\t${r.dest_addr}\t${r.connection_id}`).join('\n')
  navigator.clipboard.writeText(text)
  appStore.showToast('已复制到剪贴板', 'success')
}

async function exportRecords() {
  try {
    const params = new URLSearchParams()
    if (envFilter.value) params.append('env', envFilter.value)
    if (statusFilter.value) params.append('status', statusFilter.value)
    const res = await api.get(`/api/records/export?${params.toString()}`, { responseType: 'blob' })
    const blob = new Blob([res.data], { type: 'text/csv;charset=utf-8' })
    const url = URL.createObjectURL(blob), a = document.createElement('a')
    a.href = url; a.download = `网络管理_${new Date().toISOString().split('T')[0]}.csv`; a.click()
    URL.revokeObjectURL(url)
    appStore.showToast('导出成功', 'success')
  } catch (e) { appStore.showToast('导出失败', 'error') }
}

async function batchUpdateStatus(status) {
  if (selectedRecords.value.length === 0) return
  try {
    for (const id of selectedRecords.value) await api.put(`/api/records/${id}`, { record: { status }, operator: authStore.user?.username })
    appStore.showToast(`已将 ${selectedRecords.value.length} 条记录设为${getStatusText(status)}`, 'success')
    selectedRecords.value = []
    loadRecords()
  } catch (e) { appStore.showToast('批量更新失败', 'error') }
}

async function confirmBatchDelete() {
  if (selectedRecords.value.length === 0) return
  const confirmed = await appStore.showConfirm({ type: 'danger', title: '批量删除', message: `确定要删除选中的 ${selectedRecords.value.length} 条记录吗？`, okText: '确认删除', cancelText: '取消' })
  if (!confirmed) return
  try {
    await api.post('/api/records/batch-delete', { ids: selectedRecords.value, operator: authStore.user?.username })
    appStore.showToast('批量删除成功', 'success')
    selectedRecords.value = []
    loadRecords()
  } catch (e) { appStore.showToast('批量删除失败', 'error') }
}
</script>

<template>
  <div class="network-page">
    <!-- 页面头部 -->
    <div class="page-header">
      <div class="page-title-section">
        <h1 class="page-title">网络管理</h1>
        <p class="page-subtitle">管理网络连接记录、VID配置、源地址和目标地址映射</p>
      </div>
      <div class="header-actions">
        <button class="btn btn-secondary" @click="loadRecords" :disabled="loading">
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M4 4v5h.582m15.356 2A8.001 8.001 0 004.582 9m0 0H9m11 11v-5h-.581m0 0a8.003 8.003 0 01-15.357-2m15.357 2H15"/></svg>
          刷新
        </button>
        <button class="btn btn-secondary" @click="exportRecords">
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4M7 10l5 5 5-5M12 15V3"/></svg>
          导出
        </button>
        <button class="btn btn-primary" @click="openRecordModal('add')">
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><line x1="12" y1="5" x2="12" y2="19"/><line x1="5" y1="12" x2="19" y2="12"/></svg>
          添加
        </button>
        <button class="btn btn-secondary" @click="openBatchModal">
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><rect x="3" y="3" width="7" height="7"/><rect x="14" y="3" width="7" height="7"/><rect x="14" y="14" width="7" height="7"/><rect x="3" y="14" width="7" height="7"/></svg>
          批量添加
        </button>
        <button class="btn btn-secondary" @click="openBatchCheckModal">
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><circle cx="11" cy="11" r="8"/><path d="M21 21l-4.35-4.35"/></svg>
          批量检测
        </button>
      </div>
    </div>

    <!-- 统计卡片 -->
    <div class="stats-grid">
      <div class="stat-card primary"><div class="stat-value">{{ statsData.total }}</div><div class="stat-label">总记录数</div></div>
      <div class="stat-card success"><div class="stat-value">{{ statsData.active }}</div><div class="stat-label">启用</div></div>
      <div class="stat-card warning"><div class="stat-value">{{ statsData.pending }}</div><div class="stat-label">待定</div></div>
      <div class="stat-card danger"><div class="stat-value">{{ statsData.inactive }}</div><div class="stat-label">停用</div></div>
    </div>

    <!-- 筛选栏 -->
    <div class="filter-bar">
      <div class="search-box">
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><circle cx="11" cy="11" r="8"/><path d="M21 21l-4.35-4.35"/></svg>
        <input type="text" v-model="tempSearchQuery" placeholder="搜索VID、IP、端口..." class="search-input" @keyup.enter="applyFilter" />
      </div>
      <select v-model="tempProjectFilter" class="filter-select"><option value="">所有项目</option><option v-for="p in projectList" :key="p" :value="p">{{ p }}</option></select>
      <select v-model="tempEnvFilter" class="filter-select"><option value="">所有环境</option><option value="prod">PROD</option><option value="uat">UAT</option></select>
      <select v-model="tempStatusFilter" class="filter-select"><option value="">所有状态</option><option value="active">启用</option><option value="pending">待定</option><option value="inactive">停用</option></select>
      <select v-model="tempDuplicateFilter" class="filter-select"><option value="">连接ID</option><option value="duplicate">重复ID</option><option value="unique">不重复ID</option></select>
      <div class="filter-actions">
        <button class="btn-search" @click="applyFilter">搜 索</button>
        <button class="btn-reset" @click="resetRecordFilter">重 置</button>
      </div>
    </div>

    <!-- 批量操作栏 -->
    <div class="batch-action-bar" v-if="selectedRecords.length > 0">
      <span class="batch-info">已选择 {{ selectedRecords.length }} 条记录</span>
      <button class="btn btn-primary btn-sm" @click="batchUpdateStatus('active')">设为启用</button>
      <button class="btn btn-warning btn-sm" @click="batchUpdateStatus('pending')">设为待定</button>
      <button class="btn btn-secondary btn-sm" @click="batchUpdateStatus('inactive')">设为停用</button>
      <button class="btn btn-danger btn-sm" @click="confirmBatchDelete">删除选中</button>
    </div>

    <!-- 表格 -->
    <div class="table-container">
      <div class="table-scroll">
        <table class="data-table">
          <thead>
            <tr>
              <th style="width: 40px;"><input type="checkbox" class="table-checkbox" @change="toggleSelectAll" :checked="isAllSelected" /></th>
              <th class="sortable" @click="toggleSort('project')">项目 <span class="sort-icon">{{ getSortIcon('project') }}</span></th>
              <th class="sortable" @click="toggleSort('env')">环境 <span class="sort-icon">{{ getSortIcon('env') }}</span></th>
              <th class="sortable" @click="toggleSort('module')">模块名 <span class="sort-icon">{{ getSortIcon('module') }}</span></th>
              <th class="sortable" @click="toggleSort('vid')">VID <span class="sort-icon">{{ getSortIcon('vid') }}</span></th>
              <th class="sortable" @click="toggleSort('src_ip')">源地址 <span class="sort-icon">{{ getSortIcon('src_ip') }}</span></th>
              <th class="sortable" @click="toggleSort('dest_ip')">目标地址 <span class="sort-icon">{{ getSortIcon('dest_ip') }}</span></th>
              <th class="sortable" @click="toggleSort('connection_id')">连接ID <span class="sort-icon">{{ getSortIcon('connection_id') }}</span></th>
              <th class="sortable" @click="toggleSort('status')">状态 <span class="sort-icon">{{ getSortIcon('status') }}</span></th>
              <th>更新人</th>
              <th>更新时间</th>
              <th class="th-action-fixed">操作</th>
            </tr>
          </thead>
          <tbody>
            <tr v-if="loading"><td colspan="12" class="empty">加载中...</td></tr>
            <tr v-else-if="filteredRecords.length === 0"><td colspan="12" class="empty"><div class="empty-state"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" style="width:48px;height:48px;margin-bottom:8px;opacity:0.5;"><path d="M9 5H7a2 2 0 00-2 2v12a2 2 0 002 2h10a2 2 0 002-2V7a2 2 0 00-2-2h-2M9 5a2 2 0 002 2h2a2 2 0 002-2M9 5a2 2 0 012-2h2a2 2 0 012 2"/></svg><div>暂无数据</div></div></td></tr>
            <tr v-for="record in paginatedRecords" :key="record.id" :class="{ 'row-selected': selectedRecords.includes(record.id), 'popconfirm-active': activePopconfirm === 'record-' + record.id }">
              <td><input type="checkbox" class="table-checkbox" :value="record.id" v-model="selectedRecords" /></td>
              <td>{{ record.project }}</td>
              <td><span class="type-tag" :class="'env-' + record.env">{{ record.env?.toUpperCase() }}</span></td>
              <td>{{ record.module || '-' }}</td>
              <td class="vid-cell">{{ record.vid }}</td>
              <td class="ip-cell">{{ record.src_ip }}:{{ record.src_port }}</td>
              <td class="ip-cell">{{ record.dest_ip }}:{{ record.dest_port }}</td>
              <td class="conn-id">{{ record.connection_id }}</td>
              <td><span class="status-badge" :class="record.status">{{ getStatusText(record.status) }}</span></td>
              <td>{{ record.updated_by || '-' }}</td>
              <td>{{ formatDate(record.updated_at) }}</td>
              <td class="td-action-fixed">
                <div class="action-btns">
                  <button class="action-btn" @click="openRecordHistory(record)" title="历史"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M12 8v4l3 3m6-3a9 9 0 11-18 0 9 9 0 0118 0z"/></svg></button>
                  <button class="action-btn" @click="openRecordModal('edit', record)" title="编辑"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M11 5H6a2 2 0 00-2 2v11a2 2 0 002 2h11a2 2 0 002-2v-5m-1.414-9.414a2 2 0 112.828 2.828L11.828 15H9v-2.828l8.586-8.586z"/></svg></button>
                  <div class="popconfirm-wrapper">
                    <button class="action-btn danger" @click.stop="togglePopconfirm('record', record.id)" title="删除"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M19 7l-.867 12.142A2 2 0 0116.138 21H7.862a2 2 0 01-1.995-1.858L5 7m5 4v6m4-6v6m1-10V4a1 1 0 00-1-1h-4a1 1 0 00-1 1v3M4 7h16"/></svg></button>
                    <div class="popconfirm" :class="{ show: activePopconfirm === 'record-' + record.id }">
                      <div class="popconfirm-content"><span class="popconfirm-icon">!</span><span class="popconfirm-message">确定要删除吗?</span></div>
                      <div class="popconfirm-buttons"><button class="btn-cancel" @click.stop="activePopconfirm = null">取消</button><button class="btn-confirm" @click.stop="deleteRecord(record)">确定</button></div>
                    </div>
                  </div>
                </div>
              </td>
            </tr>
          </tbody>
        </table>
      </div>
      
      <!-- 分页 -->
      <div class="pagination" v-if="filteredRecords.length > 0">
        <div class="pagination-info"><span>每页</span><select class="page-size-select" v-model="pageSize" @change="currentPage = 1"><option :value="10">10</option><option :value="20">20</option><option :value="50">50</option><option :value="100">100</option></select><span>条，共 {{ filteredRecords.length }} 条</span></div>
        <div class="pagination-controls">
          <button class="page-btn" @click="currentPage = 1" :disabled="currentPage === 1">首页</button>
          <button class="page-btn" @click="currentPage--" :disabled="currentPage === 1">上一页</button>
          <div class="page-numbers"><button v-for="page in displayedPages" :key="page" class="page-number" :class="{ active: page === currentPage, ellipsis: page === '...' }" @click="page !== '...' && (currentPage = page)" :disabled="page === '...'">{{ page }}</button></div>
          <button class="page-btn" @click="currentPage++" :disabled="currentPage === totalPages">下一页</button>
          <button class="page-btn" @click="currentPage = totalPages" :disabled="currentPage === totalPages">尾页</button>
          <div class="page-jump"><span>跳至</span><input type="number" class="page-jump-input" v-model.number="jumpPage" @keyup.enter="goToPage" min="1" :max="totalPages" /><span>页</span></div>
        </div>
      </div>
    </div>

    <!-- 添加/编辑记录弹窗 -->
    <Teleport to="body">
      <div class="modal-overlay" :class="{ active: showRecordModal }">
        <div class="modal record-modal">
          <div class="modal-header"><h2>{{ recordModalMode === 'add' ? '添加记录' : '编辑记录' }}</h2><button class="modal-close" @click="showRecordModal = false"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><line x1="18" y1="6" x2="6" y2="18"/><line x1="6" y1="6" x2="18" y2="18"/></svg></button></div>
          <form class="modal-form" @submit.prevent="saveRecord">
            <div class="modal-body">
              <div class="form-group"><label>连接ID <span class="required">*</span></label><input type="text" class="form-input" v-model="recordForm.connection_id" placeholder="唯一标识，如：CONN-001" required /></div>
              <div class="form-row"><div class="form-group"><label>项目名称 <span class="required">*</span></label><input type="text" class="form-input" v-model="recordForm.project" required /></div><div class="form-group"><label>环境 <span class="required">*</span></label><select class="form-select" v-model="recordForm.env"><option value="uat">UAT</option><option value="prod">PROD</option></select></div></div>
              <div class="form-row"><div class="form-group"><label>模块名 <span class="required">*</span></label><input type="text" class="form-input" v-model="recordForm.module" placeholder="请输入模块名" required /></div><div class="form-group"><label>VID <span class="required">*</span></label><textarea class="form-input vid-textarea" v-model="recordForm.vid" placeholder="支持多行，如：&#10;VLAN100&#10;VLAN200" rows="2" required></textarea></div></div>
              <div class="form-row"><div class="form-group"><label>源地址 <span class="required">*</span></label><div class="address-input"><input type="text" class="form-input" v-model="recordForm.src_ip" placeholder="IP地址" required /><span class="address-sep">:</span><input type="text" class="form-input" v-model="recordForm.src_port" placeholder="端口" required /></div></div><div class="form-group"><label>目标地址 <span class="required">*</span></label><div class="address-input"><input type="text" class="form-input" v-model="recordForm.dest_ip" placeholder="IP地址" required /><span class="address-sep">:</span><input type="text" class="form-input" v-model="recordForm.dest_port" placeholder="端口" required /></div></div></div>
              <div class="form-group" style="max-width:50%"><label>状态</label><select class="form-select" v-model="recordForm.status"><option value="active">启用</option><option value="inactive">停用</option><option value="pending">待定</option></select></div>
            </div>
            <div class="modal-footer"><button type="button" class="btn btn-secondary" @click="showRecordModal = false">取消</button><button type="submit" class="btn btn-primary">保存</button></div>
          </form>
        </div>
      </div>
    </Teleport>

    <!-- 历史记录弹窗 - 现代蓝鲸时间线风格 -->
    <Teleport to="body">
      <div class="modal-overlay" :class="{ active: showHistoryModal }">
        <div class="modal history-modal">
          <div class="modal-header">
            <div class="modal-title-icon">
              <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M12 8v4l3 3m6-3a9 9 0 11-18 0 9 9 0 0118 0z"/></svg>
            </div>
            <h2>修改历史 - {{ historyRecord?.connection_id }}</h2>
            <button class="modal-close" @click="showHistoryModal = false"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><line x1="18" y1="6" x2="6" y2="18"/><line x1="6" y1="6" x2="18" y2="18"/></svg></button>
          </div>
          <div class="modal-body">
            <div v-if="recordHistories.length === 0" class="empty-state"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" style="width:48px;height:48px;opacity:0.5;"><path d="M12 8v4l3 3m6-3a9 9 0 11-18 0 9 9 0 0118 0z"/></svg><div>暂无修改历史</div></div>
            <div v-else class="history-timeline">
              <div v-for="(h, idx) in recordHistories" :key="h.id" class="history-item" :class="{ 'history-current': idx === 0 }">
                <div class="timeline-dot" :class="{ current: idx === 0 }"></div>
                <div class="timeline-line" v-if="idx < recordHistories.length - 1"></div>
                <div class="history-card">
                  <div class="history-header">
                    <span class="history-version">v{{ recordHistories.length - idx }}</span>
                    <span class="history-time">{{ formatDateTime(h.created_at) }}</span>
                    <span class="history-user">{{ h.created_by }}</span>
                    <span class="history-action" :class="'action-' + h.action">{{ getActionText(h.action) }}</span>
                  </div>
                  <div class="history-changes" v-if="h.changes && Object.keys(parseChanges(h.changes)).length > 0">
                    <div v-for="(change, field) in parseChanges(h.changes)" :key="field" class="change-item">
                      <span class="change-field">{{ getFieldLabel(field) }}:</span>
                      <span class="change-old">{{ change.old || '-' }}</span>
                      <span class="change-arrow">
                        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M5 12h14M12 5l7 7-7 7"/></svg>
                      </span>
                      <span class="change-new">{{ change.new || '-' }}</span>
                    </div>
                  </div>
                  <div class="history-actions" v-if="idx > 0">
                    <button class="btn btn-sm btn-secondary" @click="previewHistoryVersion(h, idx)">
                      <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M1 12s4-8 11-8 11 8 11 8-4 8-11 8-11-8-11-8z"/><circle cx="12" cy="12" r="3"/></svg>
                      预览
                    </button>
                    <button class="btn btn-sm btn-warning" @click="rollbackToVersion(h)">
                      <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M1 4v6h6M23 20v-6h-6"/><path d="M20.49 9A9 9 0 0 0 5.64 5.64L1 10m22 4l-4.64 4.36A9 9 0 0 1 3.51 15"/></svg>
                      回滚到此版本
                    </button>
                  </div>
                </div>
              </div>
            </div>
          </div>
          <div class="modal-footer"><button class="btn btn-secondary" @click="showHistoryModal = false">关闭</button></div>
        </div>
      </div>
    </Teleport>

    <!-- 历史版本预览弹窗 -->
    <Teleport to="body">
      <div class="modal-overlay" :class="{ active: showPreviewModal }">
        <div class="modal preview-modal">
          <div class="modal-header">
            <div class="modal-title-icon preview"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M1 12s4-8 11-8 11 8 11 8-4 8-11 8-11-8-11-8z"/><circle cx="12" cy="12" r="3"/></svg></div>
            <h2>版本预览 - v{{ previewVersion }}</h2>
            <button class="modal-close" @click="showPreviewModal = false"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><line x1="18" y1="6" x2="6" y2="18"/><line x1="6" y1="6" x2="18" y2="18"/></svg></button>
          </div>
          <div class="modal-body" v-if="previewData">
            <div class="preview-meta">
              <span class="preview-badge"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><rect x="3" y="4" width="18" height="18" rx="2" ry="2"/><line x1="16" y1="2" x2="16" y2="6"/><line x1="8" y1="2" x2="8" y2="6"/><line x1="3" y1="10" x2="21" y2="10"/></svg>{{ formatDateTime(previewHistory?.created_at) }}</span>
              <span class="preview-badge"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M20 21v-2a4 4 0 0 0-4-4H8a4 4 0 0 0-4 4v2"/><circle cx="12" cy="7" r="4"/></svg>{{ previewHistory?.created_by }}</span>
              <span class="preview-badge action" :class="'action-' + previewHistory?.action">{{ getActionText(previewHistory?.action) }}</span>
            </div>
            <div class="preview-table">
              <table>
                <tr><td class="field-label">连接ID</td><td><code>{{ previewData.connection_id }}</code></td></tr>
                <tr><td class="field-label">项目</td><td>{{ previewData.project }}</td></tr>
                <tr><td class="field-label">环境</td><td><span class="type-tag" :class="'env-' + previewData.env">{{ previewData.env?.toUpperCase() }}</span></td></tr>
                <tr><td class="field-label">模块名</td><td>{{ previewData.module || '-' }}</td></tr>
                <tr><td class="field-label">VID</td><td class="vid-cell">{{ previewData.vid }}</td></tr>
                <tr><td class="field-label">源地址</td><td class="ip-cell">{{ previewData.src_ip }}:{{ previewData.src_port }}</td></tr>
                <tr><td class="field-label">目标地址</td><td class="ip-cell">{{ previewData.dest_ip }}:{{ previewData.dest_port }}</td></tr>
                <tr><td class="field-label">状态</td><td><span class="status-badge" :class="previewData.status">{{ getStatusText(previewData.status) }}</span></td></tr>
              </table>
            </div>
          </div>
          <div class="modal-footer"><button class="btn btn-secondary" @click="showPreviewModal = false">关闭</button></div>
        </div>
      </div>
    </Teleport>

    <!-- 批量添加弹窗 -->
    <Teleport to="body">
      <div class="modal-overlay" :class="{ active: showBatchModal }">
        <div class="modal batch-modal">
          <div class="modal-header"><h2>批量添加记录</h2><button class="modal-close" @click="showBatchModal = false"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><line x1="18" y1="6" x2="6" y2="18"/><line x1="6" y1="6" x2="18" y2="18"/></svg></button></div>
          <div class="modal-body">
            <div class="batch-help">
              <h4>格式说明</h4>
              <p>每行一条记录，字段用 <code>Tab</code>、<code>逗号</code>、<code>|</code> 或 <code>空格</code> 分隔</p>
              <p>字段顺序: <code>项目</code> <code>模块名</code> <code>VID</code> <code>源地址(IP:端口)</code> <code>目标地址(IP:端口)</code> <code>连接ID</code></p>
              <p class="batch-tip">VID 支持多个值，用分号 <code>;</code> 分隔</p>
              <pre class="batch-example">订单系统,订单模块,VLAN100;VLAN200,192.168.1.10:8080,10.0.0.5:8080,CONN-001</pre>
            </div>
            <div class="batch-options"><div class="batch-option"><label>默认环境:</label><select v-model="batchEnv" @change="parseBatchText"><option value="uat">UAT</option><option value="prod">PROD</option></select></div><div class="batch-option"><label>默认状态:</label><select v-model="batchStatus" @change="parseBatchText"><option value="active">启用</option><option value="pending">待定</option><option value="inactive">停用</option></select></div></div>
            <div class="form-group"><label>粘贴数据</label><textarea class="batch-textarea" v-model="batchText" @input="parseBatchText" placeholder="粘贴数据..."></textarea></div>
            <div v-if="batchError" class="batch-error">{{ batchError }}</div>
            <div v-if="batchRecords.length > 0" class="batch-preview"><h4>预览 ({{ batchRecords.length }} 条)</h4><div class="batch-preview-table"><table><thead><tr><th>#</th><th>项目</th><th>模块名</th><th>VID</th><th>源地址</th><th>目标地址</th><th>连接ID</th></tr></thead><tbody><tr v-for="(r, i) in batchRecords" :key="i"><td>{{ i + 1 }}</td><td>{{ r.project }}</td><td>{{ r.module }}</td><td class="vid-cell">{{ r.vid }}</td><td>{{ r.src_ip }}:{{ r.src_port }}</td><td>{{ r.dest_ip }}:{{ r.dest_port }}</td><td>{{ r.connection_id }}</td></tr></tbody></table></div></div>
          </div>
          <div class="modal-footer"><button class="btn btn-secondary" @click="showBatchModal = false" :disabled="batchLoading">取消</button><button class="btn btn-primary" @click="submitBatch" :disabled="batchRecords.length === 0 || !!batchError || batchLoading">{{ batchLoading ? '添加中...' : `添加 ${batchRecords.length} 条记录` }}</button></div>
        </div>
      </div>
    </Teleport>

    <!-- 批量检测弹窗 -->
    <Teleport to="body">
      <div class="modal-overlay" :class="{ active: showBatchCheckModal }">
        <div class="modal batch-check-modal">
          <div class="modal-header"><h2>批量检测连接ID</h2><button class="modal-close" @click="showBatchCheckModal = false"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><line x1="18" y1="6" x2="6" y2="18"/><line x1="6" y1="6" x2="18" y2="18"/></svg></button></div>
          <div class="modal-body">
            <div class="batch-help"><h4>格式说明</h4><p>粘贴要检测的数据，系统将检测连接ID是否已存在于数据库中</p><p>字段顺序: <code>项目</code> <code>模块名</code> <code>VID</code> <code>源地址(IP:端口)</code> <code>目标地址(IP:端口)</code> <code>连接ID</code></p></div>
            <div class="form-group"><label>粘贴数据</label><textarea class="batch-textarea" v-model="batchCheckText" @input="parseBatchCheckText" placeholder="粘贴要检测的数据..."></textarea></div>
            <div v-if="batchCheckError" class="batch-error">{{ batchCheckError }}</div>
            <div v-if="batchCheckResult" class="batch-check-result">
              <div class="result-summary"><div class="result-card success"><div class="result-value">{{ batchCheckResult.new_count }}</div><div class="result-label">可添加（新记录）</div></div><div class="result-card danger"><div class="result-value">{{ batchCheckResult.exists_count }}</div><div class="result-label">已存在（需修改）</div></div></div>
              <div v-if="batchCheckResult.exists?.length > 0" class="result-section exists"><div class="result-header"><h4>已存在的连接ID（{{ batchCheckResult.exists.length }} 条）</h4><button class="btn btn-sm" @click="copyCheckResult('exists')">复制</button></div><div class="result-table"><table><thead><tr><th>项目</th><th>模块名</th><th>VID</th><th>源地址</th><th>目标地址</th><th>连接ID</th><th>数据库中的记录</th></tr></thead><tbody><tr v-for="r in batchCheckResult.exists" :key="r.connection_id"><td>{{ r.project }}</td><td>{{ r.module }}</td><td>{{ r.vid }}</td><td>{{ r.src_addr }}</td><td>{{ r.dest_addr }}</td><td class="conn-id danger">{{ r.connection_id }}</td><td class="existing-info">{{ r.existing_info }}</td></tr></tbody></table></div></div>
              <div v-if="batchCheckResult.new?.length > 0" class="result-section new"><div class="result-header"><h4>可添加的记录（{{ batchCheckResult.new.length }} 条）</h4><button class="btn btn-sm" @click="copyCheckResult('new')">复制</button></div><div class="result-table"><table><thead><tr><th>项目</th><th>模块名</th><th>VID</th><th>源地址</th><th>目标地址</th><th>连接ID</th></tr></thead><tbody><tr v-for="r in batchCheckResult.new" :key="r.connection_id"><td>{{ r.project }}</td><td>{{ r.module }}</td><td>{{ r.vid }}</td><td>{{ r.src_addr }}</td><td>{{ r.dest_addr }}</td><td class="conn-id success">{{ r.connection_id }}</td></tr></tbody></table></div></div>
            </div>
          </div>
          <div class="modal-footer"><button class="btn btn-secondary" @click="showBatchCheckModal = false">关闭</button><button class="btn btn-primary" @click="doBatchCheck" :disabled="batchCheckRecords.length === 0 || !!batchCheckError || batchCheckLoading">{{ batchCheckLoading ? '检测中...' : `开始检测 (${batchCheckRecords.length} 条)` }}</button></div>
        </div>
      </div>
    </Teleport>
  </div>
</template>

<style scoped>
.network-page { display: flex; flex-direction: column; gap: 16px; }

.page-header { display: flex; align-items: center; justify-content: space-between; padding: 24px 28px; background: var(--bg-card); border-radius: 16px; border: 1px solid var(--border-color); flex-wrap: wrap; gap: 16px; }
.page-title-section { display: flex; flex-direction: column; gap: 4px; }
.page-title { font-size: 26px; font-weight: 700; color: var(--text-primary); margin: 0; }
.page-subtitle { font-size: 14px; color: var(--text-secondary); margin: 0; }
.header-actions { display: flex; gap: 12px; flex-wrap: wrap; }

.btn { display: inline-flex; align-items: center; gap: 8px; padding: 12px 20px; border-radius: 10px; font-size: 14px; font-weight: 500; border: none; cursor: pointer; transition: all 0.2s; white-space: nowrap; }
.btn svg { width: 16px; height: 16px; }
.btn-primary { background: linear-gradient(135deg, #3a84ff, #6366f1); color: #fff; box-shadow: 0 4px 14px rgba(58, 132, 255, 0.35); }
.btn-primary:hover { transform: translateY(-2px); box-shadow: 0 6px 20px rgba(58, 132, 255, 0.45); }
.btn-secondary { background: var(--bg-hover); color: var(--text-primary); border: 1px solid var(--border-color); }
.btn-secondary:hover { border-color: var(--primary); color: var(--primary); }
.btn-warning { background: linear-gradient(135deg, #ff9c01, #f59e0b); color: #fff; }
.btn-danger { background: #ea3636; color: #fff; }
.btn-danger:disabled { opacity: 0.5; cursor: not-allowed; }
.btn-sm { padding: 8px 14px; font-size: 13px; }

.stats-grid { display: grid; grid-template-columns: repeat(4, 1fr); gap: 16px; }
.stat-card { background: var(--bg-card); border-radius: 16px; border: 1px solid var(--border-color); padding: 24px; text-align: center; position: relative; overflow: hidden; }
.stat-card::before { content: ''; position: absolute; top: 0; left: 0; width: 4px; height: 100%; border-radius: 16px 0 0 16px; }
.stat-card.primary::before { background: linear-gradient(to bottom, #3a84ff, #6366f1); }
.stat-card.success::before { background: linear-gradient(to bottom, #2dcb56, #10b981); }
.stat-card.warning::before { background: linear-gradient(to bottom, #ff9c01, #f59e0b); }
.stat-card.danger::before { background: linear-gradient(to bottom, #ea3636, #ef4444); }
.stat-value { font-size: 36px; font-weight: 700; line-height: 1.2; }
.stat-card.primary .stat-value { color: #3a84ff; }
.stat-card.success .stat-value { color: #2dcb56; }
.stat-card.warning .stat-value { color: #ff9c01; }
.stat-card.danger .stat-value { color: #ea3636; }
.stat-label { font-size: 14px; color: var(--text-secondary); margin-top: 8px; }

.filter-bar { display: flex; gap: 12px; padding: 20px; background: var(--bg-card); border-radius: 16px; border: 1px solid var(--border-color); flex-wrap: wrap; align-items: center; }
.search-box { flex: 1; min-width: 200px; position: relative; }
.search-box svg { position: absolute; left: 16px; top: 50%; transform: translateY(-50%); width: 18px; height: 18px; color: var(--text-muted); }
.search-input { width: 100%; padding: 12px 16px 12px 48px; border-radius: 10px; border: 1px solid var(--border-color); background: var(--bg-input); color: var(--text-primary); font-size: 14px; transition: all 0.2s; }
.search-input:focus { outline: none; border-color: #3a84ff; box-shadow: 0 0 0 3px rgba(58, 132, 255, 0.1); }
.filter-select { padding: 12px 40px 12px 16px; border-radius: 10px; border: 1px solid var(--border-color); background: var(--bg-input); color: var(--text-primary); font-size: 14px; cursor: pointer; appearance: none; min-width: 140px; background-image: url("data:image/svg+xml,%3Csvg xmlns='http://www.w3.org/2000/svg' width='16' height='16' viewBox='0 0 24 24' fill='none' stroke='%238b949e' stroke-width='2'%3E%3Cpath d='M6 9l6 6 6-6'/%3E%3C/svg%3E"); background-repeat: no-repeat; background-position: right 12px center; }
.filter-select:focus { outline: none; border-color: #3a84ff; }
.filter-actions { display: flex; gap: 8px; }
.btn-search { padding: 12px 28px; background: linear-gradient(135deg, #3a84ff, #6366f1); color: #fff; border: none; border-radius: 10px; cursor: pointer; font-size: 14px; font-weight: 500; }
.btn-search:hover { box-shadow: 0 4px 14px rgba(58, 132, 255, 0.35); }
.btn-reset { padding: 12px 28px; background: var(--bg-hover); color: var(--text-primary); border: 1px solid var(--border-color); border-radius: 10px; cursor: pointer; font-size: 14px; }

.batch-action-bar { display: flex; align-items: center; gap: 12px; padding: 14px 20px; background: rgba(58, 132, 255, 0.1); border: 1px solid rgba(58, 132, 255, 0.3); border-radius: 12px; }
.batch-info { font-size: 14px; color: #3a84ff; font-weight: 600; }

.table-container { background: var(--bg-card); border-radius: 16px; border: 1px solid var(--border-color); overflow: hidden; }
.table-scroll { overflow-x: auto; }
.data-table { width: 100%; border-collapse: collapse; font-size: 13px; }
.data-table th { padding: 16px 20px; text-align: left; font-weight: 600; font-size: 13px; color: var(--text-secondary); background: var(--bg-hover); border-bottom: 1px solid var(--border-color); white-space: nowrap; text-transform: uppercase; letter-spacing: 0.5px; }
.data-table th.sortable { cursor: pointer; user-select: none; }
.data-table th.sortable:hover { color: #3a84ff; }
.sort-icon { font-size: 12px; margin-left: 4px; opacity: 0.5; }
.data-table td { padding: 18px 20px; border-bottom: 1px solid var(--border-color); vertical-align: middle; color: var(--text-primary); }
.data-table tbody tr { transition: all 0.15s; }
.data-table tbody tr:hover { background: var(--bg-hover); }
.data-table tbody tr.row-selected { background: rgba(58, 132, 255, 0.08); }
.data-table tbody tr:last-child td { border-bottom: none; }

.table-checkbox { width: 16px; height: 16px; cursor: pointer; }

.type-tag { display: inline-flex; padding: 6px 14px; border-radius: 6px; font-size: 12px; font-weight: 600; }
.type-tag.env-prod, .type-tag.env-PROD { background: rgba(234, 54, 54, 0.15); color: #ea3636; }
.type-tag.env-uat, .type-tag.env-UAT { background: rgba(255, 156, 1, 0.15); color: #ff9c01; }

.vid-cell { font-family: 'Monaco', 'Consolas', monospace; font-size: 12px; color: #22d3ee; white-space: pre-line; }
.ip-cell { font-family: 'Monaco', 'Consolas', monospace; font-size: 13px; color: var(--text-primary); }
.conn-id { font-family: 'Monaco', 'Consolas', monospace; font-size: 13px; color: #a78bfa; font-weight: 500; }

.status-badge { display: inline-flex; align-items: center; justify-content: center; padding: 5px 12px; border-radius: 6px; font-size: 12px; font-weight: 500; min-width: 48px; text-align: center; }
.status-badge.active { background: rgba(45, 203, 86, 0.15); color: #2dcb56; }
.status-badge.pending { background: rgba(255, 156, 1, 0.15); color: #ff9c01; }
.status-badge.inactive { background: rgba(234, 54, 54, 0.15); color: #ea3636; }

.th-action-fixed, .td-action-fixed { position: sticky !important; right: 0 !important; min-width: 120px; width: 120px; text-align: center !important; white-space: nowrap; }
.th-action-fixed { z-index: 100 !important; background: #252d3d !important; box-shadow: -8px 0 12px -4px rgba(0, 0, 0, 0.3); }
.td-action-fixed { z-index: 50 !important; background: #1a1f2e !important; box-shadow: -8px 0 12px -4px rgba(0, 0, 0, 0.2); }
.data-table tbody tr:hover .td-action-fixed { background: #252d3d !important; }
.data-table tbody tr.row-selected .td-action-fixed { background: #1e3a5f !important; }
.data-table tbody tr.popconfirm-active { position: relative; z-index: 200; }
.data-table tbody tr.popconfirm-active .td-action-fixed { z-index: 201 !important; }
.light-mode .th-action-fixed { background: #f0f2f5 !important; }
.light-mode .td-action-fixed { background: #ffffff !important; }
.light-mode .data-table tbody tr:hover .td-action-fixed { background: #f0f2f5 !important; }
.light-mode .data-table tbody tr.row-selected .td-action-fixed { background: #dbeafe !important; }

.action-btns { display: flex; gap: 8px; justify-content: center; }
.action-btn { width: 34px; height: 34px; border-radius: 8px; border: none; background: var(--bg-hover); color: var(--text-secondary); cursor: pointer; display: flex; align-items: center; justify-content: center; transition: all 0.15s; }
.action-btn:hover { background: #3a84ff; color: #fff; }
.action-btn.danger:hover { background: #ea3636; }
.action-btn svg { width: 16px; height: 16px; }

.popconfirm-wrapper { position: relative; display: inline-block; }
.popconfirm { position: absolute; top: calc(100% + 8px); right: 0; background: var(--bg-card); border: 1px solid var(--border-color); border-radius: 10px; padding: 16px; box-shadow: 0 8px 24px rgba(0, 0, 0, 0.3); z-index: 9999; display: none; min-width: 200px; }
.popconfirm.show { display: block; }
.popconfirm-content { display: flex; align-items: center; gap: 10px; margin-bottom: 14px; }
.popconfirm-icon { width: 24px; height: 24px; background: #ff9c01; color: #fff; border-radius: 50%; display: flex; align-items: center; justify-content: center; font-size: 14px; font-weight: bold; flex-shrink: 0; }
.popconfirm-message { font-size: 14px; color: var(--text-primary); }
.popconfirm-buttons { display: flex; gap: 10px; justify-content: flex-end; }
.btn-cancel { padding: 8px 16px; background: var(--bg-hover); border: 1px solid var(--border-color); border-radius: 6px; cursor: pointer; font-size: 13px; color: var(--text-primary); transition: all 0.15s; }
.btn-cancel:hover { background: var(--bg-card); border-color: var(--text-secondary); }
.btn-confirm { padding: 8px 16px; background: #ea3636; border: none; border-radius: 6px; cursor: pointer; font-size: 13px; color: #fff; transition: all 0.15s; }
.btn-confirm:hover { background: #dc2626; }

.empty { text-align: center; color: var(--text-muted); padding: 60px 20px !important; }
.empty-state { display: flex; flex-direction: column; align-items: center; }

.pagination { display: flex; align-items: center; justify-content: space-between; padding: 18px 24px; border-top: 1px solid var(--border-color); flex-wrap: wrap; gap: 12px; }
.pagination-info { display: flex; align-items: center; gap: 8px; font-size: 14px; color: var(--text-secondary); }
.page-size-select { padding: 8px 12px; border-radius: 8px; border: 1px solid var(--border-color); background: var(--bg-input); color: var(--text-primary); }
.pagination-controls { display: flex; align-items: center; gap: 8px; }
.page-btn { min-width: 38px; height: 38px; padding: 0 14px; border-radius: 8px; border: 1px solid var(--border-color); background: var(--bg-input); color: var(--text-primary); font-size: 14px; cursor: pointer; transition: all 0.15s; white-space: nowrap; display: flex; align-items: center; justify-content: center; }
.page-btn:hover:not(:disabled) { border-color: #3a84ff; color: #3a84ff; }
.page-btn:disabled { opacity: 0.5; cursor: not-allowed; }
.page-numbers { display: flex; gap: 4px; }
.page-number { min-width: 38px; height: 38px; border-radius: 8px; border: 1px solid var(--border-color); background: var(--bg-input); color: var(--text-primary); font-size: 14px; cursor: pointer; display: flex; align-items: center; justify-content: center; }
.page-number:hover:not(:disabled) { border-color: #3a84ff; color: #3a84ff; }
.page-number.active { background: linear-gradient(135deg, #3a84ff, #6366f1); border-color: transparent; color: #fff; }
.page-number.ellipsis { border: none; background: none; cursor: default; }
.page-jump { display: flex; align-items: center; gap: 8px; margin-left: 12px; font-size: 14px; color: var(--text-secondary); }
.page-jump-input { width: 55px; padding: 8px 10px; border-radius: 8px; border: 1px solid var(--border-color); background: var(--bg-input); color: var(--text-primary); text-align: center; }
</style>

<style>
/* 弹窗基础样式由全局 base.css 控制 display 属性 */
.modal-overlay.active { display: flex !important; }
.modal { background: var(--bg-card); border-radius: 20px; border: 1px solid var(--border-color); width: 600px; max-width: 100%; max-height: 100%; display: flex; flex-direction: column; box-shadow: 0 25px 50px rgba(0, 0, 0, 0.4); overflow: hidden; }
.record-modal { width: 680px; }
.history-modal { width: 700px; max-height: 85vh; }
.history-modal .modal-body { padding-bottom: 30px; }
.preview-modal { width: 600px; }
.batch-modal { width: 900px; }
.batch-check-modal { width: 1000px; }
.modal-form { display: flex; flex-direction: column; flex: 1; min-height: 0; overflow: hidden; }
.modal-header { display: flex; align-items: center; gap: 12px; padding: 20px 24px; border-bottom: 1px solid var(--border-color); flex-shrink: 0; }
.modal-header h2 { flex: 1; font-size: 18px; font-weight: 600; color: var(--text-primary); margin: 0; }
.modal-title-icon { width: 40px; height: 40px; border-radius: 12px; background: linear-gradient(135deg, #3a84ff, #6366f1); display: flex; align-items: center; justify-content: center; flex-shrink: 0; }
.modal-title-icon svg { width: 20px; height: 20px; color: #fff; }
.modal-title-icon.preview { background: linear-gradient(135deg, #22d3ee, #06b6d4); }
.modal-close { width: 36px; height: 36px; border-radius: 10px; border: none; background: var(--bg-hover); color: var(--text-secondary); cursor: pointer; display: flex; align-items: center; justify-content: center; transition: all 0.15s; flex-shrink: 0; }
.modal-close:hover { background: #ea3636; color: #fff; }
.modal-close svg { width: 18px; height: 18px; }
.modal-body { padding: 20px 24px; overflow-y: auto; overflow-x: hidden; flex: 1; min-height: 0; }
.modal-footer { display: flex; justify-content: flex-end; gap: 12px; padding: 16px 24px; border-top: 1px solid var(--border-color); flex-shrink: 0; background: var(--bg-card); }

.form-row { display: grid; grid-template-columns: 1fr 1fr; gap: 16px; }
.form-group { display: flex; flex-direction: column; gap: 6px; margin-bottom: 14px; }
.form-group:last-child { margin-bottom: 0; }
.form-group label { font-size: 13px; font-weight: 500; color: var(--text-secondary); }
.form-group .required { color: #ea3636; }
.form-input, .form-select { padding: 10px 14px; background: var(--bg-input); border: 1px solid var(--border-color); border-radius: 8px; color: var(--text-primary); font-size: 14px; transition: all 0.2s; width: 100%; box-sizing: border-box; }
.form-input:focus, .form-select:focus { outline: none; border-color: #3a84ff; box-shadow: 0 0 0 3px rgba(58, 132, 255, 0.1); }
.vid-textarea { resize: vertical; min-height: 60px; font-family: inherit; }
.address-input { display: flex; align-items: center; gap: 6px; width: 100%; }
.address-input .form-input { min-width: 0; flex: 1; }
.address-input .form-input:first-child { flex: 2; }
.address-sep { color: var(--text-muted); font-size: 14px; font-weight: 500; flex-shrink: 0; }

/* 历史记录时间线样式 */
.history-timeline { position: relative; padding-left: 30px; }
.history-item { position: relative; margin-bottom: 24px; }
.history-item:last-child { margin-bottom: 10px; }
.timeline-dot { position: absolute; left: -30px; top: 20px; width: 14px; height: 14px; border-radius: 50%; background: var(--bg-hover); border: 3px solid var(--border-color); z-index: 2; }
.timeline-dot.current { background: #3a84ff; border-color: rgba(58, 132, 255, 0.3); box-shadow: 0 0 0 4px rgba(58, 132, 255, 0.15); }
.timeline-line { position: absolute; left: -24px; top: 38px; width: 2px; height: calc(100% + 10px); background: var(--border-color); }
.history-card { background: var(--bg-hover); border: 1px solid var(--border-color); border-radius: 14px; padding: 18px 22px; transition: all 0.2s; }
.history-item.history-current .history-card { border-color: rgba(58, 132, 255, 0.4); background: rgba(58, 132, 255, 0.05); }
.history-header { display: flex; align-items: center; gap: 14px; margin-bottom: 14px; flex-wrap: wrap; }
.history-version { display: inline-flex; align-items: center; justify-content: center; min-width: 36px; height: 28px; padding: 0 10px; background: linear-gradient(135deg, #3a84ff, #6366f1); color: #fff; border-radius: 6px; font-size: 12px; font-weight: 700; }
.history-time { font-size: 13px; color: var(--text-secondary); }
.history-user { font-size: 13px; color: #3a84ff; font-weight: 500; }
.history-action { padding: 4px 12px; border-radius: 6px; font-size: 12px; font-weight: 600; }
.history-action.action-create { background: rgba(45, 203, 86, 0.15); color: #2dcb56; }
.history-action.action-update { background: rgba(58, 132, 255, 0.15); color: #3a84ff; }
.history-action.action-delete { background: rgba(234, 54, 54, 0.15); color: #ea3636; }
.history-changes { margin-bottom: 14px; }
.change-item { display: flex; align-items: center; gap: 10px; padding: 10px 14px; background: var(--bg-card); border-radius: 8px; margin-bottom: 8px; font-size: 13px; }
.change-field { color: var(--text-secondary); font-weight: 500; min-width: 70px; }
.change-old { color: #ea3636; text-decoration: line-through; opacity: 0.7; }
.change-arrow { color: var(--text-muted); }
.change-arrow svg { width: 16px; height: 16px; }
.change-new { color: #2dcb56; font-weight: 500; }
.history-actions { display: flex; gap: 10px; }

/* 预览弹窗 */
.preview-meta { display: flex; gap: 12px; margin-bottom: 20px; flex-wrap: wrap; }
.preview-badge { display: inline-flex; align-items: center; gap: 8px; padding: 8px 14px; background: var(--bg-hover); border-radius: 8px; font-size: 13px; color: var(--text-secondary); }
.preview-badge svg { width: 16px; height: 16px; }
.preview-badge.action { font-weight: 600; }
.preview-badge.action.action-create { background: rgba(45, 203, 86, 0.15); color: #2dcb56; }
.preview-badge.action.action-update { background: rgba(58, 132, 255, 0.15); color: #3a84ff; }
.preview-table table { width: 100%; border-collapse: collapse; }
.preview-table tr { border-bottom: 1px solid var(--border-color); }
.preview-table tr:last-child { border-bottom: none; }
.preview-table td { padding: 14px 16px; font-size: 14px; }
.preview-table .field-label { width: 120px; color: var(--text-secondary); font-weight: 500; background: var(--bg-hover); }
.preview-table code { background: rgba(58, 132, 255, 0.15); color: #3a84ff; padding: 4px 10px; border-radius: 6px; font-family: 'Monaco', 'Consolas', monospace; font-size: 13px; }

.batch-help { background: var(--bg-hover); border: 1px solid var(--border-color); border-radius: 12px; padding: 18px; margin-bottom: 18px; }
.batch-help h4 { margin: 0 0 12px 0; font-size: 14px; color: var(--text-primary); font-weight: 600; }
.batch-help p { margin: 8px 0; font-size: 13px; color: var(--text-secondary); line-height: 1.6; }
.batch-help code { background: #3a84ff; color: #fff; padding: 2px 8px; border-radius: 4px; font-size: 12px; }
.batch-tip { color: #3a84ff !important; }
.batch-example { background: var(--bg-input); border: 1px solid var(--border-color); border-radius: 8px; padding: 14px; font-size: 12px; color: var(--text-primary); overflow-x: auto; margin: 14px 0 0; line-height: 1.6; font-family: 'Monaco', 'Consolas', monospace; }
.batch-options { display: flex; gap: 24px; margin-bottom: 18px; }
.batch-option { display: flex; align-items: center; gap: 10px; }
.batch-option label { font-size: 13px; color: var(--text-secondary); }
.batch-option select { padding: 10px 14px; border-radius: 8px; border: 1px solid var(--border-color); background: var(--bg-input); color: var(--text-primary); }
.batch-textarea { width: 100%; min-height: 150px; padding: 16px; background: var(--bg-input); border: 1px solid var(--border-color); border-radius: 12px; color: var(--text-primary); font-size: 12px; font-family: 'Monaco', 'Consolas', monospace; resize: vertical; }
.batch-textarea:focus { outline: none; border-color: #3a84ff; box-shadow: 0 0 0 3px rgba(58, 132, 255, 0.1); }
.batch-error { background: rgba(234, 54, 54, 0.1); border: 1px solid rgba(234, 54, 54, 0.3); color: #ea3636; padding: 14px 18px; border-radius: 10px; font-size: 13px; margin-bottom: 18px; white-space: pre-wrap; }
.batch-preview { margin-top: 18px; }
.batch-preview h4 { margin: 0 0 14px 0; font-size: 14px; color: var(--text-primary); }
.batch-preview-table { max-height: 200px; overflow: auto; border: 1px solid var(--border-color); border-radius: 10px; }
.batch-preview-table table { width: 100%; border-collapse: collapse; font-size: 12px; }
.batch-preview-table th, .batch-preview-table td { padding: 12px 14px; border-bottom: 1px solid var(--border-color); text-align: left; }
.batch-preview-table th { background: var(--bg-hover); font-weight: 500; color: var(--text-secondary); position: sticky; top: 0; }

.batch-check-result { margin-top: 22px; }
.result-summary { display: grid; grid-template-columns: 1fr 1fr; gap: 18px; margin-bottom: 22px; }
.result-card { padding: 20px; border-radius: 12px; text-align: center; }
.result-card.success { background: rgba(45, 203, 86, 0.1); border: 1px solid rgba(45, 203, 86, 0.3); }
.result-card.danger { background: rgba(234, 54, 54, 0.1); border: 1px solid rgba(234, 54, 54, 0.3); }
.result-value { font-size: 32px; font-weight: 700; }
.result-card.success .result-value { color: #2dcb56; }
.result-card.danger .result-value { color: #ea3636; }
.result-label { font-size: 13px; color: var(--text-secondary); margin-top: 6px; }
.result-section { margin-bottom: 18px; }
.result-header { display: flex; align-items: center; justify-content: space-between; margin-bottom: 14px; }
.result-header h4 { margin: 0; font-size: 14px; }
.result-section.exists .result-header h4 { color: #ea3636; }
.result-section.new .result-header h4 { color: #2dcb56; }
.result-table { max-height: 200px; overflow: auto; border: 1px solid var(--border-color); border-radius: 10px; }
.result-table table { width: 100%; border-collapse: collapse; font-size: 12px; }
.result-table th, .result-table td { padding: 12px 14px; border-bottom: 1px solid var(--border-color); text-align: left; }
.result-table th { background: var(--bg-hover); font-weight: 500; position: sticky; top: 0; }
.result-section.exists .result-table tr { background: rgba(234, 54, 54, 0.05); }
.result-section.new .result-table tr { background: rgba(45, 203, 86, 0.05); }
.conn-id.danger { color: #ea3636; font-weight: 600; }
.conn-id.success { color: #2dcb56; font-weight: 600; }
.existing-info { font-size: 11px; color: var(--text-muted); }
</style>
