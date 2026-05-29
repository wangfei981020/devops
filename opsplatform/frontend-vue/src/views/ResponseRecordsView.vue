<script setup>
import { ref, computed, onMounted, watch } from 'vue'
import { useAppStore, useAuthStore } from '@/stores'
import api from '@/api'

const appStore = useAppStore()
const authStore = useAuthStore()

const canCreate = computed(() => authStore.hasPermission('response_record:create'))
const canUpdate = computed(() => authStore.hasPermission('response_record:update'))
const canDelete = computed(() => authStore.hasPermission('response_record:delete'))
const canExport = computed(() => authStore.hasPermission('response_record:export'))
const canManageSources = computed(() => authStore.hasPermission('response_source:manage'))

// v581: 消息来源从后端 API 拉，admin 可在「来源管理」里增删改
const SOURCES = ref([])
const sourceLabel = code => SOURCES.value.find(s => s.code === code)?.label || code
const sourceColor = code => SOURCES.value.find(s => s.code === code)?.color || '#94a3b8'

async function loadSources() {
  try {
    const res = await api.get('/api/response-record-sources')
    SOURCES.value = res.data || []
  } catch (e) { console.error(e) }
}

const records = ref([])
const employees = ref([])
const loading = ref(false)

const activeTab = ref('list')

const today = new Date()
const currentYear = ref(today.getFullYear())
const currentMonth = ref(today.getMonth() + 1)
const filterResponder = ref('')
const filterSource = ref('')
const filterOnlyIncident = ref(false)
const keyword = ref('')

async function loadEmployees() {
  try {
    const res = await api.get(`/api/schedule?year=${currentYear.value}&month=${currentMonth.value}`)
    employees.value = (res.data || []).map(e => ({ id: e.id, name: e.name, group_name: e.group_name }))
  } catch (e) { console.error(e) }
}

async function loadRecords() {
  loading.value = true
  try {
    const params = new URLSearchParams()
    params.append('year', currentYear.value)
    params.append('month', currentMonth.value)
    if (filterResponder.value) params.append('responder', filterResponder.value)
    if (filterSource.value) params.append('source', filterSource.value)
    if (filterOnlyIncident.value) params.append('has_incident', '1')
    if (keyword.value.trim()) params.append('keyword', keyword.value.trim())
    const res = await api.get('/api/response-records?' + params.toString())
    records.value = (res.data || []).map(r => ({
      ...r,
      attachments: parseAttachments(r.attachments)
    }))
  } catch (e) {
    appStore.showToast('加载失败: ' + (e.response?.data || e.message), 'error')
  } finally {
    loading.value = false
  }
}

function parseAttachments(s) {
  if (!s) return []
  try { return JSON.parse(s) || [] } catch { return [] }
}

onMounted(async () => {
  await loadSources()
  await loadEmployees()
  await loadRecords()
  // v582: 全局键盘监听 — lightbox 显示时左右键切换、Esc 关闭
  window.addEventListener('keydown', onPreviewKey)
})

watch([currentYear, currentMonth], async () => { await loadEmployees(); await loadRecords() })
watch([filterResponder, filterSource, filterOnlyIncident], () => loadRecords())

function prevMonth() {
  if (currentMonth.value === 1) { currentYear.value--; currentMonth.value = 12 }
  else currentMonth.value--
}
function nextMonth() {
  if (currentMonth.value === 12) { currentYear.value++; currentMonth.value = 1 }
  else currentMonth.value++
}
function toToday() {
  currentYear.value = new Date().getFullYear()
  currentMonth.value = new Date().getMonth() + 1
}

// ========= 时长计算 =========
function diffMinutes(t1, t0) {
  if (!t1 || !t0) return null
  const d1 = new Date(t1.replace(' ', 'T'))
  const d0 = new Date(t0.replace(' ', 'T'))
  const m = Math.round((d1.getTime() - d0.getTime()) / 60000)
  return isNaN(m) ? null : m
}
function fmtDuration(min) {
  if (min == null) return '-'
  if (min < 0) return '异常'
  if (min < 60) return min + 'm'
  const h = Math.floor(min / 60), m = min % 60
  return m === 0 ? `${h}h` : `${h}h${m}m`
}
function durationClass(min, threshold) {
  if (min == null) return ''
  if (min > threshold) return 'dur-bad'
  if (min > threshold * 0.7) return 'dur-warn'
  return 'dur-good'
}

// ========= 统计 =========
const stats = computed(() => {
  const list = records.value
  const respDurs = list.map(r => diffMinutes(r.responded_at, r.mentioned_at)).filter(v => v != null && v >= 0)
  const procDurs = list.map(r => r.completed_at ? diffMinutes(r.completed_at, r.responded_at) : null).filter(v => v != null && v >= 0)
  const incidents = list.filter(r => r.has_incident).length
  const processing = list.filter(r => r.status === 'processing').length
  const completed = list.filter(r => r.status === 'completed').length
  const avgResp = respDurs.length ? Math.round(respDurs.reduce((a, b) => a + b, 0) / respDurs.length) : 0
  const avgProc = procDurs.length ? Math.round(procDurs.reduce((a, b) => a + b, 0) / procDurs.length) : 0
  return { total: list.length, avgResp, avgProc, incidents, processing, completed }
})

// 按响应人聚合（统计 tab）
const employeeStats = computed(() => {
  const map = {}
  records.value.forEach(r => {
    if (!map[r.responder]) map[r.responder] = { responder: r.responder, count: 0, respSum: 0, respN: 0, procSum: 0, procN: 0, incidents: 0 }
    const s = map[r.responder]
    s.count++
    const rd = diffMinutes(r.responded_at, r.mentioned_at)
    if (rd != null && rd >= 0) { s.respSum += rd; s.respN++ }
    if (r.completed_at) {
      const pd = diffMinutes(r.completed_at, r.responded_at)
      if (pd != null && pd >= 0) { s.procSum += pd; s.procN++ }
    }
    if (r.has_incident) s.incidents++
  })
  return Object.values(map)
    .map(s => ({
      ...s,
      avgResp: s.respN ? Math.round(s.respSum / s.respN) : 0,
      avgProc: s.procN ? Math.round(s.procSum / s.procN) : 0,
      incidentRate: s.count ? Math.round((s.incidents / s.count) * 100) : 0
    }))
    .sort((a, b) => b.count - a.count)
})

const sourceDistribution = computed(() => {
  const total = records.value.length || 1
  return SOURCES.map(s => ({
    ...s,
    count: records.value.filter(r => r.message_source === s.value).length
  })).map(s => ({ ...s, pct: Math.round((s.count / total) * 100) })).filter(s => s.count > 0)
})

// ========= 表单 =========
const showModal = ref(false)
const modalMode = ref('add')
const form = ref(emptyForm())
const uploading = ref(false)

function emptyForm() {
  const now = formatNow()
  return {
    id: 0,
    responder: '',
    message_source: SOURCES.value[0]?.code || 'lark',
    message_content: '',
    mentioned_at: now,
    responded_at: now,
    completed_at: '',
    has_incident: 0,
    incident_ticket: '',
    handle_result: '',
    remark: '',
    attachments: [],
    is_processing: false
  }
}
function formatNow() {
  const d = new Date()
  const pad = n => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}:${pad(d.getSeconds())}`
}

// ========= v581: datetime-local <-> "YYYY-MM-DD HH:MM:SS" 互转 =========
// HTML5 datetime-local 用 "YYYY-MM-DDTHH:MM"（精度分钟），后端字段是 "YYYY-MM-DD HH:MM:SS"
// 用户可在输入框里直接 keyboard 编辑到秒，所以保留秒位
function toDtLocal(s) {
  if (!s) return ''
  // "2026-05-29 10:27:00" -> "2026-05-29T10:27"
  return s.replace(' ', 'T').slice(0, 16)
}
function fromDtLocal(s, original) {
  if (!s) return ''
  // datetime-local change 事件给的是 "2026-05-29T10:27" 或 "2026-05-29T10:27:00"
  let v = s.replace('T', ' ')
  if (v.length === 16) {
    // 没秒，保留原 record 的秒数（用户在 picker 里选只到分），默认补 :00
    const origSec = (original || '').slice(17, 19)
    v += ':' + (origSec.match(/\d{2}/) ? origSec : '00')
  }
  return v
}

function openAdd() {
  modalMode.value = 'add'
  form.value = emptyForm()
  showModal.value = true
}
function openEdit(r) {
  modalMode.value = 'edit'
  form.value = {
    id: r.id,
    responder: r.responder,
    message_source: r.message_source,
    message_content: r.message_content,
    mentioned_at: r.mentioned_at,
    responded_at: r.responded_at,
    completed_at: r.completed_at || '',
    has_incident: r.has_incident,
    incident_ticket: r.incident_ticket || '',
    handle_result: r.handle_result || '',
    remark: r.remark || '',
    attachments: r.attachments || [],
    is_processing: !r.completed_at
  }
  showModal.value = true
}

const formRespDur = computed(() => diffMinutes(form.value.responded_at, form.value.mentioned_at))
const formProcDur = computed(() => form.value.completed_at ? diffMinutes(form.value.completed_at, form.value.responded_at) : null)

async function saveRecord() {
  if (!form.value.responder) { appStore.showToast('请选择响应人', 'error'); return }
  if (!form.value.message_content) { appStore.showToast('请填消息内容', 'error'); return }
  if (!form.value.mentioned_at || !form.value.responded_at) { appStore.showToast('艾特时间和响应时间必填', 'error'); return }
  if (form.value.has_incident && !form.value.incident_ticket) { appStore.showToast('勾选故障后请填故障单号', 'error'); return }

  // v582: 入库前剥掉 preview (blob URL，下次会话失效)，只存 {name, size, path}
  const cleanAttachments = (form.value.attachments || []).map(a => ({
    name: a.name, size: a.size, path: a.path
  }))
  const payload = {
    responder: form.value.responder,
    message_source: form.value.message_source,
    message_content: form.value.message_content,
    mentioned_at: form.value.mentioned_at,
    responded_at: form.value.responded_at,
    completed_at: form.value.is_processing ? null : (form.value.completed_at || null),
    has_incident: form.value.has_incident ? 1 : 0,
    incident_ticket: form.value.incident_ticket,
    handle_result: form.value.handle_result,
    remark: form.value.remark,
    attachments: JSON.stringify(cleanAttachments)
  }

  try {
    if (modalMode.value === 'add') {
      await api.post('/api/response-records', payload)
      appStore.showToast('创建成功', 'success')
    } else {
      await api.put('/api/response-records/' + form.value.id, payload)
      appStore.showToast('更新成功', 'success')
    }
    showModal.value = false
    await loadRecords()
  } catch (e) {
    appStore.showToast('保存失败: ' + (e.response?.data || e.message), 'error')
  }
}

async function deleteRecord(r) {
  const ok = await appStore.showConfirm({
    type: 'danger', title: '删除响应记录',
    message: `确定删除 ${r.responder} 的 "${r.message_content.slice(0, 30)}..." 吗？`,
    okText: '删除', cancelText: '取消'
  })
  if (!ok) return
  try {
    await api.delete('/api/response-records/' + r.id)
    appStore.showToast('删除成功', 'success')
    await loadRecords()
  } catch (e) { appStore.showToast('删除失败', 'error') }
}

// ========= 附件（v581: 上传到独立 bucket response-records） =========
async function uploadFiles(files) {
  uploading.value = true
  try {
    for (const file of files) {
      if (file.size === 0) continue
      const fd = new FormData()
      fd.append('file', file)
      const res = await api.post('/api/response-records/upload', fd, { headers: { 'Content-Type': 'multipart/form-data' } })
      if (res.data?.path) {
        form.value.attachments.push({
          name: file.name || res.data.name || 'screenshot.png',
          size: file.size,
          path: res.data.path,
          preview: URL.createObjectURL(file)
        })
      }
    }
  } catch (e) {
    appStore.showToast('上传失败: ' + (e.response?.data || e.message), 'error')
  } finally { uploading.value = false }
}

// v581: 图片预览 lightbox
const previewURL = ref('')
function isImageAttachment(a) {
  const p = a.path || a.preview || ''
  return /\.(png|jpg|jpeg|gif|webp|bmp)(\?|$)/i.test(p) || (a.preview && a.preview.startsWith('blob:'))
}
function attachmentURL(a) {
  // path 是后端持久 URL (/storage/response-records/xxx.png)，永远有效；
  // preview 仅在"刚上传后的当前会话"内有效（blob: 协议），刷新就失效 → 别再优先用它
  if (a.path) return a.path
  return a.preview || ''
}
// 列表里只取图片类型的附件做缩略图
function imageAttachments(list) {
  return (list || []).filter(isImageAttachment)
}

// v582: lightbox 多图浏览（参考桌台维护：previewImages 数组 + index + 左右按钮 + 键盘）
const showPreview = ref(false)
const previewImages = ref([])
const previewIndex = ref(0)
function openPreviewList(attachments, startIdx = 0) {
  const imgs = imageAttachments(attachments).map(attachmentURL).filter(Boolean)
  if (imgs.length === 0) return
  previewImages.value = imgs
  previewIndex.value = Math.min(startIdx, imgs.length - 1)
  showPreview.value = true
}
function openPreview(a) {
  // 单图打开：转成 list 形式
  openPreviewList([a], 0)
}
function closePreview() { showPreview.value = false }
function prevImage() {
  if (previewImages.value.length <= 1) return
  previewIndex.value = (previewIndex.value - 1 + previewImages.value.length) % previewImages.value.length
}
function nextImage() {
  if (previewImages.value.length <= 1) return
  previewIndex.value = (previewIndex.value + 1) % previewImages.value.length
}
function onPreviewKey(e) {
  if (!showPreview.value) return
  if (e.key === 'ArrowLeft')      prevImage()
  else if (e.key === 'ArrowRight') nextImage()
  else if (e.key === 'Escape')     closePreview()
}
// 挂载时绑 keydown
// keydown 在上面 onMounted 里合并

function onFilePick(e) {
  const files = Array.from(e.target.files || [])
  if (files.length) uploadFiles(files)
  e.target.value = ''
}
function onDrop(e) {
  e.currentTarget.classList.remove('drag-over')
  const files = Array.from(e.dataTransfer?.files || [])
  if (files.length) uploadFiles(files)
}
function onPaste(e) {
  const items = e.clipboardData?.items
  if (!items) return
  const files = []
  for (let i = 0; i < items.length; i++) {
    if (items[i].type.startsWith('image/')) {
      const f = items[i].getAsFile()
      if (f) files.push(f)
    }
  }
  if (files.length) {
    e.preventDefault()
    uploadFiles(files)
  }
}
function removeAttachment(idx) { form.value.attachments.splice(idx, 1) }

// ========= v581: 来源管理 modal =========
const showSourceModal = ref(false)
const editingSource = ref(null)
const sourceForm = ref({ code: '', label: '', color: '#3a84ff', sort_order: 99 })

function openSourceManage() {
  showSourceModal.value = true
  editingSource.value = null
  resetSourceForm()
}
function resetSourceForm() {
  sourceForm.value = { code: '', label: '', color: '#3a84ff', sort_order: (SOURCES.value.length + 1) }
}
function editSource(s) {
  editingSource.value = s
  sourceForm.value = { code: s.code, label: s.label, color: s.color, sort_order: s.sort_order }
}
async function saveSource() {
  if (!sourceForm.value.code || !sourceForm.value.label) {
    appStore.showToast('code 和 显示名 必填', 'error'); return
  }
  try {
    if (editingSource.value) {
      await api.put('/api/response-record-sources/' + editingSource.value.id, sourceForm.value)
    } else {
      await api.post('/api/response-record-sources', sourceForm.value)
    }
    await loadSources()
    appStore.showToast('保存成功', 'success')
    editingSource.value = null
    resetSourceForm()
  } catch (e) {
    appStore.showToast('保存失败: ' + (e.response?.data || e.message), 'error')
  }
}
async function deleteSource(s) {
  const ok = await appStore.showConfirm({
    type: 'danger', title: '删除来源',
    message: `确定删除来源 "${s.label}" 吗？已有的历史记录不会受影响。`,
    okText: '删除', cancelText: '取消'
  })
  if (!ok) return
  try {
    await api.delete('/api/response-record-sources/' + s.id)
    await loadSources()
    appStore.showToast('已删除', 'success')
  } catch (e) { appStore.showToast('删除失败', 'error') }
}

// 导出
function exportExcel() {
  if (!records.value.length) { appStore.showToast('没有数据可导出', 'info'); return }
  const headers = ['ID', '响应人', '消息来源', '消息内容', '艾特时间', '响应时间', '完成时间', '响应时长(分)', '处理时长(分)', '是否故障', '故障单号', '处理结果', '状态']
  const rows = records.value.map(r => [
    r.id, r.responder, sourceLabel(r.message_source), r.message_content,
    r.mentioned_at, r.responded_at, r.completed_at || '',
    diffMinutes(r.responded_at, r.mentioned_at) ?? '',
    r.completed_at ? (diffMinutes(r.completed_at, r.responded_at) ?? '') : '',
    r.has_incident ? '是' : '否', r.incident_ticket, r.handle_result, r.status
  ])
  const csv = '﻿' + [headers, ...rows].map(row => row.map(c => `"${String(c ?? '').replace(/"/g, '""')}"`).join(',')).join('\n')
  const blob = new Blob([csv], { type: 'text/csv;charset=utf-8;' })
  const url = URL.createObjectURL(blob)
  const a = document.createElement('a')
  a.href = url
  a.download = `响应记录_${currentYear.value}年${currentMonth.value}月.csv`
  a.click()
  URL.revokeObjectURL(url)
}
</script>

<template>
  <div class="page">
    <div class="page-header">
      <h2>响应记录</h2>
      <div class="header-actions">
        <button v-if="canManageSources" class="btn btn-secondary" @click="openSourceManage">⚙ 来源管理</button>
        <button v-if="canExport" class="btn btn-secondary" @click="exportExcel">⬇ 导出 Excel</button>
        <button v-if="canCreate" class="btn btn-primary" @click="openAdd">+ 新建响应</button>
      </div>
    </div>

    <div class="tab-nav">
      <button class="tab-btn" :class="{ active: activeTab === 'list' }" @click="activeTab = 'list'">📋 记录列表</button>
      <button class="tab-btn" :class="{ active: activeTab === 'stats' }" @click="activeTab = 'stats'">📊 统计分析</button>
    </div>

    <div class="toolbar">
      <div class="month-nav">
        <button class="nav-btn" @click="prevMonth">‹</button>
        <span class="month-label">{{ currentYear }}年{{ currentMonth }}月</span>
        <button class="nav-btn" @click="nextMonth">›</button>
        <button class="btn-link" @click="toToday">今天</button>
      </div>
      <div class="filter-group">
        <label>响应人:</label>
        <select v-model="filterResponder" class="select">
          <option value="">全部</option>
          <option v-for="e in employees" :key="e.id" :value="e.name">{{ e.name }}</option>
        </select>
      </div>
      <div class="filter-group">
        <label>来源:</label>
        <select v-model="filterSource" class="select">
          <option value="">全部</option>
          <option v-for="s in SOURCES" :key="s.code" :value="s.code">{{ s.label }}</option>
        </select>
      </div>
      <label class="check"><input type="checkbox" v-model="filterOnlyIncident"> 仅故障</label>
      <input v-model="keyword" class="input" placeholder="搜索内容 / 响应人 / 故障单号" @keyup.enter="loadRecords">
      <button class="btn btn-secondary sm" @click="loadRecords">🔍 搜索</button>
    </div>

    <!-- 概览 -->
    <div class="overview" v-if="activeTab === 'list'">
      <div class="ov-card"><div class="ov-num">{{ stats.total }}</div><div class="ov-lbl">总响应数</div></div>
      <div class="ov-card"><div class="ov-num">{{ fmtDuration(stats.avgResp) }}</div><div class="ov-lbl">平均响应时长</div></div>
      <div class="ov-card"><div class="ov-num">{{ fmtDuration(stats.avgProc) }}</div><div class="ov-lbl">平均处理时长</div></div>
      <div class="ov-card"><div class="ov-num ov-bad">{{ stats.incidents }}</div><div class="ov-lbl">故障次数</div></div>
      <div class="ov-card"><div class="ov-num ov-warn">{{ stats.processing }}</div><div class="ov-lbl">处理中</div></div>
      <div class="ov-card"><div class="ov-num ov-good">{{ stats.completed }}</div><div class="ov-lbl">已完成</div></div>
    </div>

    <!-- 列表 -->
    <div v-if="activeTab === 'list'" class="table-wrap">
      <table class="data-table">
        <thead>
          <tr>
            <th>#</th>
            <th>响应人</th>
            <th>来源</th>
            <th>消息内容</th>
            <th>艾特时间</th>
            <th>响应时间</th>
            <th>完成时间</th>
            <th>响应时长</th>
            <th>处理时长</th>
            <th>故障</th>
            <th>处理结果</th>
            <th>截图</th>
            <th>状态</th>
            <th class="op">操作</th>
          </tr>
        </thead>
        <tbody>
          <tr v-if="loading"><td colspan="14" class="empty-cell">加载中…</td></tr>
          <tr v-else-if="records.length === 0"><td colspan="14" class="empty-cell">暂无响应记录</td></tr>
          <tr v-else v-for="r in records" :key="r.id">
            <td>#{{ r.id }}</td>
            <td>{{ r.responder }}</td>
            <td><span class="source-badge" :style="{ background: sourceColor(r.message_source) }">{{ sourceLabel(r.message_source) }}</span></td>
            <td class="msg">
              <div>{{ r.message_content }}</div>
              <div v-if="r.has_incident" class="incident-tag">⚠ 故障单 {{ r.incident_ticket }}</div>
            </td>
            <td class="ts">{{ r.mentioned_at }}</td>
            <td class="ts">{{ r.responded_at }}</td>
            <td class="ts">{{ r.completed_at || '-' }}</td>
            <td :class="durationClass(diffMinutes(r.responded_at, r.mentioned_at), 5)">
              {{ fmtDuration(diffMinutes(r.responded_at, r.mentioned_at)) }}
            </td>
            <td :class="durationClass(r.completed_at ? diffMinutes(r.completed_at, r.responded_at) : null, 60)">
              {{ r.completed_at ? fmtDuration(diffMinutes(r.completed_at, r.responded_at)) : '进行中' }}
            </td>
            <td>{{ r.has_incident ? '⚠是' : '否' }}</td>
            <td class="result">{{ r.handle_result || '-' }}</td>
            <td class="shot-col">
              <div class="shot-list" v-if="imageAttachments(r.attachments).length">
                <img v-for="(a, i) in imageAttachments(r.attachments).slice(0, 3)" :key="i"
                     :src="attachmentURL(a)" class="shot-thumb"
                     :title="a.name + ' (点击放大，左右键切换)'"
                     @click="openPreviewList(r.attachments, i)"
                     @error="e => e.target.style.opacity='0.3'">
                <span v-if="imageAttachments(r.attachments).length > 3" class="shot-more"
                      :title="`共 ${imageAttachments(r.attachments).length} 张`"
                      @click="openPreviewList(r.attachments, 3)">+{{ imageAttachments(r.attachments).length - 3 }}</span>
              </div>
              <span v-else class="muted">-</span>
            </td>
            <td>
              <span v-if="r.status === 'completed'" class="status-completed">✅ 完成</span>
              <span v-else class="status-processing">🟡 处理中</span>
            </td>
            <td class="op">
              <button v-if="canUpdate" class="icon-btn" @click="openEdit(r)" title="编辑">✏️</button>
              <button v-if="canDelete" class="icon-btn danger" @click="deleteRecord(r)" title="删除">🗑️</button>
            </td>
          </tr>
        </tbody>
      </table>
    </div>

    <!-- 统计 -->
    <div v-if="activeTab === 'stats'" class="stats-panel">
      <h3>员工响应排名</h3>
      <table class="data-table">
        <thead>
          <tr>
            <th>#</th><th>员工</th><th>响应次数</th><th>平均响应</th><th>平均处理</th><th>故障次数</th><th>故障率</th>
          </tr>
        </thead>
        <tbody>
          <tr v-if="employeeStats.length === 0"><td colspan="7" class="empty-cell">暂无数据</td></tr>
          <tr v-else v-for="(s, idx) in employeeStats" :key="s.responder">
            <td>{{ idx + 1 }}</td>
            <td>{{ s.responder }}</td>
            <td>{{ s.count }}</td>
            <td>{{ fmtDuration(s.avgResp) }}</td>
            <td>{{ fmtDuration(s.avgProc) }}</td>
            <td>{{ s.incidents }}</td>
            <td>{{ s.incidentRate }}%</td>
          </tr>
        </tbody>
      </table>

      <h3 style="margin-top:24px">来源分布</h3>
      <div class="src-dist">
        <div v-for="s in sourceDistribution" :key="s.value" class="src-item">
          <span class="src-name" :style="{ color: s.color }">{{ s.label }}</span>
          <div class="src-bar"><div class="src-fill" :style="{ width: s.pct + '%', background: s.color }"></div></div>
          <span class="src-pct">{{ s.count }} ({{ s.pct }}%)</span>
        </div>
      </div>
    </div>

    <!-- 表单 Modal -->
    <div v-if="showModal" class="modal-mask" @click.self="showModal = false">
      <div class="modal-card">
        <div class="modal-header">
          <h3>{{ modalMode === 'add' ? '新建' : '编辑' }}响应记录</h3>
          <button class="close-btn" @click="showModal = false">×</button>
        </div>
        <div class="modal-body">
          <div class="form-row">
            <label>响应人 *</label>
            <select v-model="form.responder" class="select">
              <option value="">请选择</option>
              <option v-for="e in employees" :key="e.id" :value="e.name">{{ e.name }} ({{ e.group_name || '-' }})</option>
            </select>
          </div>
          <div class="form-row">
            <label>消息来源 *</label>
            <div class="radio-group">
              <label v-for="s in SOURCES" :key="s.code" class="radio-pill" :class="{ active: form.message_source === s.code }" :style="form.message_source === s.code ? { background: s.color } : {}">
                <input type="radio" :value="s.code" v-model="form.message_source"> {{ s.label }}
              </label>
            </div>
          </div>
          <div class="form-row">
            <label>消息内容 *</label>
            <textarea v-model="form.message_content" class="textarea" rows="2" placeholder="一句话说明这次任务"></textarea>
          </div>

          <div class="form-row">
            <label>艾特时间 *</label>
            <input type="datetime-local" :value="toDtLocal(form.mentioned_at)" @input="form.mentioned_at = fromDtLocal($event.target.value, form.mentioned_at)" class="input">
            <input v-model="form.mentioned_at" class="input sec-input" title="可在此手动编辑到秒" placeholder="YYYY-MM-DD HH:MM:SS">
          </div>
          <div class="form-row">
            <label>响应时间 *</label>
            <input type="datetime-local" :value="toDtLocal(form.responded_at)" @input="form.responded_at = fromDtLocal($event.target.value, form.responded_at)" class="input">
            <input v-model="form.responded_at" class="input sec-input" title="可在此手动编辑到秒" placeholder="YYYY-MM-DD HH:MM:SS">
            <span class="dur-hint" :class="durationClass(formRespDur, 5)">响应时长: {{ fmtDuration(formRespDur) }}</span>
          </div>
          <div class="form-row">
            <label>完成时间</label>
            <template v-if="!form.is_processing">
              <input type="datetime-local" :value="toDtLocal(form.completed_at)" @input="form.completed_at = fromDtLocal($event.target.value, form.completed_at)" class="input">
              <input v-model="form.completed_at" class="input sec-input" title="可在此手动编辑到秒" placeholder="YYYY-MM-DD HH:MM:SS">
            </template>
            <span v-else class="muted">处理中...</span>
            <label class="check"><input type="checkbox" v-model="form.is_processing"> 还在处理中</label>
            <span v-if="!form.is_processing" class="dur-hint" :class="durationClass(formProcDur, 60)">处理时长: {{ fmtDuration(formProcDur) }}</span>
          </div>

          <div class="form-row">
            <label>是否故障</label>
            <input type="checkbox" :checked="form.has_incident" @change="form.has_incident = $event.target.checked ? 1 : 0" class="big-check">
          </div>
          <div class="form-row" v-if="form.has_incident">
            <label>故障单号 *</label>
            <input v-model="form.incident_ticket" class="input" placeholder="如 INC-2304 / JIRA-XXX">
          </div>

          <div class="form-row">
            <label>处理结果</label>
            <textarea v-model="form.handle_result" class="textarea" rows="2"></textarea>
          </div>
          <div class="form-row">
            <label>备注</label>
            <textarea v-model="form.remark" class="textarea" rows="2"></textarea>
          </div>

          <div class="form-row">
            <label>附件 / 截图</label>
            <div class="upload-zone"
                 @dragover.prevent="e => e.currentTarget.classList.add('drag-over')"
                 @dragleave.prevent="e => e.currentTarget.classList.remove('drag-over')"
                 @drop.prevent="onDrop"
                 @paste="onPaste"
                 tabindex="0">
              <div class="upload-hint">
                📋 点击选文件 / 拖拽到此 / 直接 <kbd>Ctrl+V</kbd> 粘贴截图
                <input type="file" multiple class="file-input" @change="onFilePick" accept="image/*,.pdf,.doc,.docx,.xls,.xlsx,.txt,.log">
              </div>
              <div v-if="uploading" class="muted" style="padding:0 12px 8px">上传中...</div>
              <div v-if="form.attachments.length" class="att-list">
                <div v-for="(a, i) in form.attachments" :key="i" class="att-item">
                  <img v-if="isImageAttachment(a)" :src="attachmentURL(a)"
                       class="att-thumb thumb-clickable"
                       @click.stop="openPreviewList(form.attachments, imageAttachments(form.attachments).indexOf(a))"
                       @error="e => e.target.style.display='none'"
                       title="点击放大（左右键切换）">
                  <span v-else class="att-doc-icon">📄</span>
                  <span class="att-name">{{ a.name }}</span>
                  <span class="att-size">{{ Math.round(a.size / 1024) }} KB</span>
                  <button class="btn-link danger sm" @click.stop="removeAttachment(i)">删除</button>
                </div>
              </div>
            </div>
          </div>
        </div>
        <div class="modal-footer">
          <button class="btn btn-secondary" @click="showModal = false">取消</button>
          <button class="btn btn-primary" @click="saveRecord">💾 保存</button>
        </div>
      </div>
    </div>

    <!-- v582: 多图 lightbox（参考桌台维护：上下按钮 + 键盘左右键 + Esc 关闭 + 计数器） -->
    <div v-if="showPreview" class="lightbox" @click="closePreview">
      <button v-if="previewImages.length > 1" class="lightbox-nav prev" @click.stop="prevImage" title="上一张（←）">‹</button>
      <img :src="previewImages[previewIndex]" class="lightbox-img" @click.stop>
      <button v-if="previewImages.length > 1" class="lightbox-nav next" @click.stop="nextImage" title="下一张（→）">›</button>
      <div v-if="previewImages.length > 1" class="lightbox-counter">{{ previewIndex + 1 }} / {{ previewImages.length }}</div>
      <button class="lightbox-close" @click="closePreview" title="关闭（Esc）">×</button>
    </div>

    <!-- v581: 来源管理 modal -->
    <div v-if="showSourceModal" class="modal-mask" @click.self="showSourceModal = false">
      <div class="modal-card">
        <div class="modal-header">
          <h3>消息来源管理</h3>
          <button class="close-btn" @click="showSourceModal = false">×</button>
        </div>
        <div class="modal-body">
          <table class="data-table" style="margin-bottom: 16px">
            <thead><tr><th>code</th><th>显示名</th><th>颜色</th><th>排序</th><th>操作</th></tr></thead>
            <tbody>
              <tr v-for="s in SOURCES" :key="s.id">
                <td><code>{{ s.code }}</code></td>
                <td><span class="source-badge" :style="{ background: s.color }">{{ s.label }}</span></td>
                <td><span class="color-dot" :style="{ background: s.color }"></span> {{ s.color }}</td>
                <td>{{ s.sort_order }}</td>
                <td>
                  <button class="icon-btn" @click="editSource(s)" title="编辑">✏️</button>
                  <button class="icon-btn danger" @click="deleteSource(s)" title="删除">🗑️</button>
                </td>
              </tr>
            </tbody>
          </table>

          <h4>{{ editingSource ? '编辑 ' + editingSource.code : '新增来源' }}</h4>
          <div class="form-row">
            <label>code *</label>
            <input v-model="sourceForm.code" class="input" placeholder="如 wechat / sms / dingtalk" :disabled="!!editingSource">
          </div>
          <div class="form-row">
            <label>显示名 *</label>
            <input v-model="sourceForm.label" class="input" placeholder="如 企微 / 短信 / 钉钉">
          </div>
          <div class="form-row">
            <label>颜色</label>
            <input v-model="sourceForm.color" type="color" class="color-input">
            <input v-model="sourceForm.color" class="input" style="max-width: 120px" placeholder="#3a84ff">
          </div>
          <div class="form-row">
            <label>排序</label>
            <input v-model.number="sourceForm.sort_order" type="number" class="input" style="max-width: 100px">
          </div>
        </div>
        <div class="modal-footer">
          <button v-if="editingSource" class="btn btn-secondary" @click="editingSource = null; resetSourceForm()">取消编辑</button>
          <button class="btn btn-secondary" @click="showSourceModal = false">关闭</button>
          <button class="btn btn-primary" @click="saveSource">💾 {{ editingSource ? '保存' : '添加' }}</button>
        </div>
      </div>
    </div>
  </div>
</template>

<style scoped>
.page { padding: 20px; }
.page-header { display: flex; justify-content: space-between; align-items: center; margin-bottom: 16px; }
.page-header h2 { font-size: 20px; margin: 0; }
.header-actions { display: flex; gap: 10px; }

.tab-nav { display: flex; gap: 8px; margin-bottom: 16px; border-bottom: 1px solid var(--border-color); }
.tab-btn { padding: 10px 18px; border: none; background: transparent; cursor: pointer; font-size: 14px; color: var(--text-secondary); border-bottom: 2px solid transparent; }
.tab-btn.active { color: var(--primary); border-bottom-color: var(--primary); font-weight: 600; }

.toolbar { display: flex; align-items: center; gap: 12px; flex-wrap: wrap; margin-bottom: 16px; padding: 12px; background: var(--bg-card); border: 1px solid var(--border-color); border-radius: 10px; }
.month-nav { display: flex; align-items: center; gap: 8px; }
.nav-btn { width: 30px; height: 30px; border: 1px solid var(--border-color); background: transparent; cursor: pointer; border-radius: 6px; color: var(--text-color); }
.month-label { font-weight: 600; min-width: 100px; text-align: center; }
.btn-link { background: none; border: none; color: var(--primary); cursor: pointer; font-size: 13px; }
.btn-link.danger { color: #ea3636; }
.filter-group { display: flex; align-items: center; gap: 6px; font-size: 13px; }
.select, .input { padding: 6px 10px; border: 1px solid var(--border-color); background: var(--bg-card); color: var(--text-color); border-radius: 6px; font-size: 13px; }
.input { min-width: 200px; }
.check { display: flex; align-items: center; gap: 4px; font-size: 13px; cursor: pointer; }
.btn { padding: 8px 16px; border: none; border-radius: 8px; cursor: pointer; font-size: 14px; font-weight: 500; }
.btn.sm { padding: 6px 12px; font-size: 12px; }
.btn-primary { background: var(--primary); color: #fff; }
.btn-secondary { background: var(--bg-hover); color: var(--text-color); border: 1px solid var(--border-color); }

.overview { display: grid; grid-template-columns: repeat(6, 1fr); gap: 12px; margin-bottom: 16px; }
.ov-card { background: var(--bg-card); border: 1px solid var(--border-color); border-radius: 10px; padding: 14px; text-align: center; }
.ov-num { font-size: 22px; font-weight: 700; color: var(--text-color); }
.ov-num.ov-good { color: #10b981; }
.ov-num.ov-warn { color: #f97316; }
.ov-num.ov-bad { color: #ea3636; }
.ov-lbl { font-size: 12px; color: var(--text-secondary); margin-top: 4px; }

.table-wrap, .stats-panel { background: var(--bg-card); border: 1px solid var(--border-color); border-radius: 10px; padding: 12px; overflow-x: auto; }
.table-wrap .data-table { min-width: 1500px; }
.ts { white-space: nowrap; font-family: monospace; font-size: 12px; color: var(--text-secondary); }
.result { max-width: 200px; }
.shot-col { width: 140px; }
.shot-list { display: flex; gap: 4px; align-items: center; }
.shot-thumb { width: 32px; height: 32px; object-fit: cover; border-radius: 4px; cursor: zoom-in; border: 1px solid var(--border-color); transition: transform 0.15s; }
.shot-thumb:hover { transform: scale(1.5); position: relative; z-index: 5; box-shadow: 0 4px 12px rgba(0, 0, 0, 0.3); }
.shot-more { font-size: 11px; color: var(--text-secondary); }
.att-doc-icon { font-size: 28px; }
.data-table { width: 100%; border-collapse: collapse; font-size: 13px; }
.data-table th, .data-table td { padding: 10px 8px; text-align: left; border-bottom: 1px solid var(--border-color); }
.data-table th { font-weight: 600; color: var(--text-secondary); background: var(--bg-hover); }
.data-table .op { width: 80px; text-align: center; }
.empty-cell { text-align: center; padding: 40px; color: var(--text-secondary); }
.icon-btn { background: none; border: none; cursor: pointer; padding: 4px 6px; font-size: 14px; }
.icon-btn.danger { color: #ea3636; }
.source-badge { display: inline-block; padding: 2px 8px; border-radius: 4px; color: #fff; font-size: 11px; font-weight: 600; }
.msg { max-width: 320px; }
.incident-tag { color: #ea3636; font-size: 12px; margin-top: 4px; }
.att-row { margin-top: 4px; display: flex; flex-wrap: wrap; gap: 4px; }
.att-chip { font-size: 11px; padding: 1px 6px; background: var(--bg-hover); border-radius: 4px; color: var(--text-secondary); max-width: 120px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.status-completed { color: #10b981; font-weight: 600; }
.status-processing { color: #f97316; font-weight: 600; }
.dur-good { color: #10b981; }
.dur-warn { color: #f97316; }
.dur-bad { color: #ea3636; font-weight: 600; }

.stats-panel h3 { margin: 0 0 12px; font-size: 16px; }
.src-dist { display: flex; flex-direction: column; gap: 8px; }
.src-item { display: grid; grid-template-columns: 80px 1fr 100px; align-items: center; gap: 12px; }
.src-name { font-weight: 600; font-size: 13px; }
.src-bar { height: 12px; background: var(--bg-hover); border-radius: 6px; overflow: hidden; }
.src-fill { height: 100%; }
.src-pct { font-size: 12px; color: var(--text-secondary); }

.modal-mask { position: fixed; top: 0; left: 0; right: 0; bottom: 0; background: rgba(0, 0, 0, 0.5); z-index: 9999; display: flex; align-items: center; justify-content: center; }
.modal-card { background: var(--bg-card); border-radius: 12px; width: 720px; max-height: 90vh; display: flex; flex-direction: column; }
.modal-header { padding: 16px 20px; border-bottom: 1px solid var(--border-color); display: flex; justify-content: space-between; align-items: center; }
.modal-header h3 { margin: 0; font-size: 16px; }
.close-btn { background: none; border: none; font-size: 24px; cursor: pointer; color: var(--text-secondary); }
.modal-body { padding: 20px; overflow-y: auto; flex: 1; }
.modal-footer { padding: 14px 20px; border-top: 1px solid var(--border-color); display: flex; justify-content: flex-end; gap: 10px; }

.form-row { display: flex; align-items: flex-start; gap: 12px; margin-bottom: 14px; }
.form-row > label:first-child { width: 110px; flex-shrink: 0; padding-top: 6px; font-size: 13px; color: var(--text-color); }
.form-row .input, .form-row .select { flex: 1; min-width: 0; }
.textarea { flex: 1; padding: 8px 10px; border: 1px solid var(--border-color); background: var(--bg-card); color: var(--text-color); border-radius: 6px; font-size: 13px; font-family: inherit; resize: vertical; }
.radio-group { display: flex; gap: 8px; flex-wrap: wrap; flex: 1; }
.radio-pill { display: flex; align-items: center; gap: 4px; padding: 4px 12px; border: 1px solid var(--border-color); border-radius: 16px; cursor: pointer; font-size: 13px; }
.radio-pill input { display: none; }
.radio-pill.active { color: #fff; border-color: transparent; }
.dur-hint { font-size: 12px; padding-top: 8px; min-width: 100px; }
.muted { color: var(--text-secondary); font-size: 13px; }

.upload-zone { flex: 1; border: 2px dashed var(--border-color); border-radius: 8px; padding: 0; outline: none; }
.upload-zone:hover, .upload-zone:focus, .upload-zone.drag-over { border-color: var(--primary); background: rgba(59, 130, 246, 0.05); }
/* v581.1: input.file-input 只覆盖 hint 区域，不挡住下面的缩略图列表 */
.upload-hint { position: relative; font-size: 13px; color: var(--text-secondary); text-align: center; padding: 16px; cursor: pointer; }
.upload-hint kbd { padding: 1px 6px; background: var(--bg-hover); border-radius: 3px; font-family: monospace; }
.file-input { position: absolute; inset: 0; opacity: 0; cursor: pointer; }
.att-list { padding: 0 12px 12px; display: flex; flex-direction: column; gap: 6px; }
.att-item { display: flex; align-items: center; gap: 10px; padding: 6px; background: var(--bg-hover); border-radius: 6px; font-size: 12px; }
.att-thumb { width: 36px; height: 36px; object-fit: cover; border-radius: 4px; }
.thumb-clickable { cursor: zoom-in; transition: transform 0.15s; }
.thumb-clickable:hover { transform: scale(1.1); }
.att-icon { font-size: 18px; }
.att-name { flex: 1; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.att-size { color: var(--text-secondary); }
.att-chip.clickable { cursor: zoom-in; }
.att-chip.clickable:hover { background: var(--primary); color: #fff; }

/* v581: 时间字段：datetime-local + 秒位编辑 */
.sec-input { max-width: 180px; font-family: monospace; font-size: 12px; }
.big-check { width: 18px; height: 18px; cursor: pointer; }

/* v581: 图片预览 lightbox */
.lightbox { position: fixed; inset: 0; background: rgba(0, 0, 0, 0.85); z-index: 10000; display: flex; align-items: center; justify-content: center; cursor: zoom-out; }
.lightbox-img { max-width: 95vw; max-height: 95vh; object-fit: contain; box-shadow: 0 8px 40px rgba(0, 0, 0, 0.6); cursor: default; }
.lightbox-close { position: absolute; top: 20px; right: 30px; background: rgba(255, 255, 255, 0.2); border: none; color: #fff; font-size: 28px; width: 40px; height: 40px; border-radius: 50%; cursor: pointer; }
.lightbox-close:hover { background: rgba(255, 255, 255, 0.35); }
.lightbox-nav { position: absolute; top: 50%; transform: translateY(-50%); width: 50px; height: 50px; background: rgba(255, 255, 255, 0.15); border: none; color: #fff; font-size: 40px; line-height: 1; border-radius: 50%; cursor: pointer; display: flex; align-items: center; justify-content: center; }
.lightbox-nav:hover { background: rgba(255, 255, 255, 0.35); }
.lightbox-nav.prev { left: 30px; }
.lightbox-nav.next { right: 30px; }
.lightbox-counter { position: absolute; bottom: 24px; left: 50%; transform: translateX(-50%); padding: 6px 14px; background: rgba(255, 255, 255, 0.15); color: #fff; border-radius: 14px; font-size: 13px; font-weight: 600; }

/* v581: 来源管理 */
.color-dot { display: inline-block; width: 12px; height: 12px; border-radius: 2px; vertical-align: middle; margin-right: 4px; }
.color-input { width: 50px; height: 32px; padding: 0; border: 1px solid var(--border-color); border-radius: 4px; cursor: pointer; }
</style>
