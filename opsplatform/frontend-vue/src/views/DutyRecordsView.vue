<script setup>
import { ref, computed, onMounted, watch, nextTick } from 'vue'
import { useAppStore, useAuthStore } from '@/stores'
import api from '@/api'

const appStore = useAppStore()
const authStore = useAuthStore()

const isSuperAdmin = computed(() => authStore.isSuperAdmin())
const canCreate = computed(() => isSuperAdmin.value || authStore.hasPermission('duty:create'))
const canUpdate = computed(() => isSuperAdmin.value || authStore.hasPermission('duty:update'))
const canEditPlannedFixTime = computed(() => isSuperAdmin.value || authStore.hasPermission('duty:edit_planned_fix_time'))

// 在编辑表单里是否可以编辑计划修复时间
// 规则：
// 1. 如果是新增模式，可以编辑
// 2. 如果planned_fix_time_edited为false（首次编辑），且状态不是"检测正常"或"已解决"，可以编辑
// 3. 如果有duty:edit_planned_fix_time权限，可以编辑
const canEditPlannedFixTimeInForm = computed(() => {
  // 新增模式，始终可以编辑
  if (modalMode.value === 'add') return true
  // 有权限，始终可以编辑
  if (canEditPlannedFixTime.value) return true
  // 编辑模式下检查是否首次编辑
  if (modalMode.value === 'edit') {
    // 如果已经被编辑过，需要权限
    if (form.value.planned_fix_time_edited) return false
    // 首次编辑，状态不是"检测正常"或"已解决"，允许编辑
    const restrictedStatuses = ['normal', 'resolved']
    if (!restrictedStatuses.includes(form.value.status)) return true
  }
  return false
})
const canDelete = computed(() => isSuperAdmin.value || authStore.hasPermission('duty:delete'))
const canExport = computed(() => isSuperAdmin.value || authStore.hasPermission('duty:export'))
const canUpload = computed(() => isSuperAdmin.value || authStore.hasPermission('duty:upload'))
const canProjectCreate = computed(() => isSuperAdmin.value || authStore.hasPermission('duty_project:create'))
const canProjectUpdate = computed(() => isSuperAdmin.value || authStore.hasPermission('duty_project:update'))
const canProjectDelete = computed(() => isSuperAdmin.value || authStore.hasPermission('duty_project:delete'))
const canManageProject = computed(() => canProjectCreate.value || canProjectUpdate.value || canProjectDelete.value)
const canOpenProjectConfig = computed(() => isSuperAdmin.value || authStore.hasPermission('menu:duty_projects') || canManageProject.value)

const records = ref([])
const projects = ref([])
const allProjects = ref([])
const loading = ref(false)
const stats = ref({})

const filters = ref({
  status: '',
  project_id: '',
  handler: '',
  duty_person: '',
  start_date: '',
  end_date: '',
  is_overdue: false,
  event_type: '',
  response_time_range: ''
})

const showModal = ref(false)
const modalMode = ref('add')
const form = ref(getEmptyForm())
const showPlannedFixModal = ref(false)
const plannedFixForm = ref({ id: '', planned_fix_time: '', duty_person: '', project_name: '' })

const showStatsPanel = ref(true)
const activeTab = ref('list') // 'list' 或 'stats'

// 统计分析数据
const statsData = ref({
  overview: {},
  byHandler: [],
  byDutyPerson: [],
  byProject: [],
  byStatus: [],
  byEventType: [],
  byFeedback: [],
  callDetails: [],
  trend: [],
  responseTime: [],
  byEscalate: [],
  notEscalated: 0,
  callDistribution: [],
  callStats: {}
})
const statsLoading = ref(false)
const statsFilters = ref({
  project_id: '',
  handler: '',
  duty_person: '',
  event_type: '',
  start_date: '',
  end_date: ''
})
const timeRangePreset = ref('month') // 'today', 'week', 'month', 'quarter', 'all'
const chartInstances = ref({}) // 存储Chart.js实例
const chartTypes = ref({
  status: 'pie',       // 处理结果: bar, pie, doughnut
  project: 'bar',      // 项目: bar, pie, doughnut
  handler: 'bar',      // 处理人: bar, pie, horizontalBar
  eventType: 'doughnut', // 事件类型: bar, pie, doughnut
  dutyPerson: 'bar',   // 值班人: bar, pie, horizontalBar
  feedback: 'pie',     // 反馈类型: bar, pie, doughnut
  responseTime: 'bar', // 响应时长: bar, line
  trend: 'line',       // 趋势: line, bar
  callDist: 'bar',     // 拨打次数: bar, horizontalBar
  escalate: 'doughnut' // 上报: bar, pie, doughnut
})
const pieDisplayMode = ref({
  status: 'percent',    // 'percent' or 'count'
  project: 'percent',
  handler: 'percent',
  eventType: 'percent',
  dutyPerson: 'percent',
  feedback: 'percent',
  callDist: 'percent',
  escalate: 'percent'
})
const showImagePreview = ref(false)
const previewImages = ref([])
const previewIndex = ref(0)

const uploading = ref(false)

// 预签名URL缓存
const presignedUrlCache = ref({})
const presignPending = ref(new Set())

// 分享弹窗
const showShareModal = ref(false)
const shareForm = ref({ filePath: '', fileName: '', expiresIn: '7d' })
const shareResult = ref(null)

// 批量操作相关
const selectedRecords = ref([])
const showBatchStatusModal = ref(false)
const batchStatus = ref('')
const dragOver = ref(false)
const fileInput = ref(null)

// 复制/粘贴功能
const copiedRecord = ref(null)
const showPasteModal = ref(false)
const pasteCount = ref(1)

// 自动计算响应时间 (接听时间 - 首次拨打时间)
const calculatedResponseTime = computed(() => {
  if (!form.value.first_call_time || !form.value.answer_time) return 0
  const first = new Date(form.value.first_call_time)
  const answer = new Date(form.value.answer_time)
  if (isNaN(first.getTime()) || isNaN(answer.getTime())) return 0
  const diffMs = answer.getTime() - first.getTime()
  if (diffMs < 0) return 0
  return Math.round(diffMs / 60000) // 转换为分钟
})

// 监听计算结果，自动更新表单
watch(calculatedResponseTime, (val) => {
  form.value.response_time = val
})

// 接听时间变化时自动更新是否接听状态
function onAnswerTimeChange() {
  if (form.value.answer_time) {
    form.value.is_answered = '已接听'
  }
}

const showProjectModal = ref(false)
const projectForm = ref({ id: '', name: '', code: '', description: '', status: 'active', sort_order: 0 })


// 分页
const currentPage = ref(1)
const pageSize = ref(10)

const totalPages = computed(() => Math.ceil(records.value.length / pageSize.value) || 1)

const paginatedRecords = computed(() => {
  const start = (currentPage.value - 1) * pageSize.value
  return records.value.slice(start, start + pageSize.value)
})

// 待处理记录（排除已解决和检测正常，按更新时间倒序）
const recentHandovers = computed(() => {
  return records.value
    .filter(r => !['resolved', 'normal'].includes(r.status))
    .sort((a, b) => new Date(b.updated_at) - new Date(a.updated_at))
})

// 格式化交接时间
function formatHandoverTime(dateStr) {
  if (!dateStr) return '-'
  const date = new Date(dateStr)
  if (isNaN(date.getTime())) return dateStr
  const now = new Date()
  const diffMs = now - date
  const diffMin = Math.floor(diffMs / 60000)
  const diffHour = Math.floor(diffMs / 3600000)
  const diffDay = Math.floor(diffMs / 86400000)
  
  if (diffMin < 1) return '刚刚'
  if (diffMin < 60) return `${diffMin}分钟前`
  if (diffHour < 24) return `${diffHour}小时前`
  if (diffDay < 7) return `${diffDay}天前`
  
  const pad = n => n.toString().padStart(2, '0')
  return `${date.getMonth()+1}/${date.getDate()} ${pad(date.getHours())}:${pad(date.getMinutes())}`
}

// 全选功能
const selectAll = computed({
  get: () => paginatedRecords.value.length > 0 && selectedRecords.value.length === paginatedRecords.value.length,
  set: (val) => {
    if (val) {
      selectedRecords.value = paginatedRecords.value.map(r => r.id)
    } else {
      selectedRecords.value = []
    }
  }
})

const visiblePages = computed(() => {
  const pages = []
  const total = totalPages.value
  const current = currentPage.value
  let start = Math.max(1, current - 2)
  let end = Math.min(total, current + 2)
  if (end - start < 4) {
    if (start === 1) end = Math.min(total, 5)
    else start = Math.max(1, total - 4)
  }
  for (let i = start; i <= end; i++) pages.push(i)
  return pages
})
const projectModalMode = ref('add')

const showDetailModal = ref(false)
const detailRecord = ref(null)
const detailPendingStatus = ref(null)

function getEmptyForm() {
  const now = new Date()
  const pad = n => n.toString().padStart(2, '0')
  const defaultDateTime = `${now.getFullYear()}-${pad(now.getMonth()+1)}-${pad(now.getDate())}T${pad(now.getHours())}:${pad(now.getMinutes())}`
  return {
    id: '',
    duty_date: defaultDateTime,
    duty_person: '',
    project_id: '',
    task_desc: '',
    feedback_type: 'customer',
    event_type: 'customer_feedback',
    handler: '',
    handle_result: '',
    solution: '',
    problem_desc: '',
    first_call_time: '',
    answer_time: '',
    call_count: 0,
    is_answered: '无',
    response_time: 0,
    is_escalated: false,
    escalate_to: [],
    has_handover: false,
    handover_person: '',
    handover_content: '',
    status: 'pending',
    planned_fix_time: '',
    planned_fix_time_edited: false,
    is_overdue: false,
    overdue_reason: '',
    attachments: []
  }
}

// 翻译函数
const t = (key, params) => appStore.t(key, params)

const statusOptions = computed(() => [
  { value: 'normal', label: t('dutyRecords.statusOptions.normal'), color: '#10b981' },
  { value: 'pending', label: t('dutyRecords.statusOptions.pending'), color: '#f59e0b' },
  { value: 'in_progress', label: t('dutyRecords.statusOptions.inProgress'), color: '#8b5cf6' },
  { value: 'resolved', label: t('dutyRecords.statusOptions.resolved'), color: '#22c55e' },
  { value: 'temporary', label: t('dutyRecords.statusOptions.temporary'), color: '#3b82f6' }
])

const feedbackTypeOptions = computed(() => [
  { value: 'proactive', label: t('dutyRecords.feedbackTypeOptions.proactive') },
  { value: 'customer', label: t('dutyRecords.feedbackTypeOptions.customer') }
])

const eventTypeOptions = computed(() => [
  { value: 'inspection', label: t('dutyRecords.eventTypeOptions.inspection') },
  { value: 'alert', label: t('dutyRecords.eventTypeOptions.alert') },
  { value: 'customer_feedback', label: t('dutyRecords.eventTypeOptions.customerFeedback') },
  { value: 'proactive_check', label: t('dutyRecords.eventTypeOptions.proactiveCheck') }
])

// 从记录中提取唯一的处理人和值班人列表
const uniqueHandlers = computed(() => {
  const handlers = new Set()
  records.value.forEach(r => {
    if (r.handler) handlers.add(r.handler)
  })
  return Array.from(handlers).sort()
})

const uniqueDutyPersons = computed(() => {
  const persons = new Set()
  records.value.forEach(r => {
    if (r.duty_person) persons.add(r.duty_person)
  })
  return Array.from(persons).sort()
})

const escalateOptions = computed(() => [
  { value: 'leader', label: t('dutyRecords.escalateOptions.leader') },
  { value: 'hod', label: t('dutyRecords.escalateOptions.hod') }
])

onMounted(() => {
  loadProjects()
  loadRecords()
  loadStats()
})

async function loadProjects() {
  try {
    const res = await api.get('/api/duty-projects')
    allProjects.value = res.data || []
    projects.value = allProjects.value.filter(p => p.status === 'active')
  } catch (e) {
    console.error('加载项目失败', e)
  }
}

async function loadRecords() {
  loading.value = true
  try {
    const params = new URLSearchParams()
    if (filters.value.status) params.append('status', filters.value.status)
    if (filters.value.project_id) params.append('project_id', filters.value.project_id)
    if (filters.value.handler) params.append('handler', filters.value.handler)
    if (filters.value.duty_person) params.append('duty_person', filters.value.duty_person)
    if (filters.value.start_date) params.append('start_date', filters.value.start_date)
    if (filters.value.end_date) params.append('end_date', filters.value.end_date)
    if (filters.value.is_overdue) params.append('is_overdue', '1')
    if (filters.value.event_type) params.append('event_type', filters.value.event_type)
    if (filters.value.response_time_range) {
      const range = filters.value.response_time_range
      if (range === '2min') {
        params.append('response_time_max', '2')
      } else if (range === '5min') {
        params.append('response_time_min', '2')
        params.append('response_time_max', '5')
      } else if (range === '10min') {
        params.append('response_time_min', '5')
        params.append('response_time_max', '10')
      } else if (range === '10min+') {
        params.append('response_time_min', '10')
      }
    }

    const res = await api.get(`/api/duty-records?${params.toString()}`)
    records.value = res.data || []
    
    // 批量获取附件的预签名URL
    const allAttachments = records.value.flatMap(r => r.attachments || [])
    if (allAttachments.length > 0) {
      batchPresignUrls(allAttachments)
    }
  } catch (e) {
    console.error('加载记录失败', e)
    appStore.showToast(t('dutyRecords.actions.loadFailed'), 'error')
  } finally {
    loading.value = false
  }
}

async function loadStats() {
  try {
    const res = await api.get('/api/duty-records/stats')
    stats.value = res.data || {}
  } catch (e) {
    console.error('加载统计失败', e)
  }
}

// 获取单个文件的预签名URL
async function getPresignedUrl(path) {
  if (!path || !path.startsWith('/storage/')) return path
  if (presignedUrlCache.value[path]) return presignedUrlCache.value[path]
  if (presignPending.value.has(path)) return path
  
  presignPending.value.add(path)
  try {
    const res = await api.get(`/api/storage/presign?path=${encodeURIComponent(path)}`)
    if (res.data?.url) {
      presignedUrlCache.value[path] = res.data.url
      return res.data.url
    }
  } catch (e) {
    console.error('获取预签名URL失败', path, e)
  } finally {
    presignPending.value.delete(path)
  }
  return path
}

// 批量获取预签名URL
async function batchPresignUrls(paths) {
  const uncached = paths.filter(p => p && p.startsWith('/storage/') && !presignedUrlCache.value[p])
  if (uncached.length === 0) return

  const BATCH_SIZE = 50
  try {
    const batches = []
    for (let i = 0; i < uncached.length; i += BATCH_SIZE) {
      batches.push(uncached.slice(i, i + BATCH_SIZE))
    }
    const results = await Promise.all(batches.map(batch => api.post('/api/storage/presign/batch', { paths: batch })))
    results.forEach(res => {
      if (res.data?.urls) Object.assign(presignedUrlCache.value, res.data.urls)
    })
  } catch (e) {
    console.error('批量获取预签名URL失败', e)
  }
}

// 获取显示用的URL（优先使用缓存的预签名URL）
function getDisplayUrl(path) {
  if (!path) return ''
  if (presignedUrlCache.value[path]) return presignedUrlCache.value[path]
  if (path.startsWith('/storage/')) {
    getPresignedUrl(path)
    return ''
  }
  return path
}

// 打开分享弹窗
function openShareModal(filePath, fileName = '') {
  shareForm.value = { filePath, fileName: fileName || filePath.split('/').pop(), expiresIn: '7d' }
  shareResult.value = null
  showShareModal.value = true
}

// 创建分享链接
async function createShare() {
  try {
    const res = await api.post('/api/storage/shares', {
      file_path: shareForm.value.filePath,
      file_name: shareForm.value.fileName,
      expires_in: shareForm.value.expiresIn
    })
    shareResult.value = {
      code: res.data.code,
      shareUrl: window.location.origin + '/api/share/' + res.data.code,
      expiresAt: res.data.expires_at
    }
    appStore.showToast(t('dutyRecords.actions.shareSuccess'), 'success')
  } catch (e) {
    appStore.showToast(t('dutyRecords.actions.shareFailed') + ': ' + (e.response?.data?.error || e.message), 'error')
  }
}

// 复制分享链接
function copyShareUrl() {
  if (!shareResult.value?.shareUrl) return
  navigator.clipboard.writeText(shareResult.value.shareUrl)
  appStore.showToast(t('dutyRecords.actions.linkCopied'), 'success')
}

// 加载详细统计数据
async function loadDetailedStats() {
  statsLoading.value = true
  try {
    const params = new URLSearchParams()
    if (statsFilters.value.project_id) params.append('project_id', statsFilters.value.project_id)
    if (statsFilters.value.handler) params.append('handler', statsFilters.value.handler)
    if (statsFilters.value.duty_person) params.append('duty_person', statsFilters.value.duty_person)
    if (statsFilters.value.event_type) params.append('event_type', statsFilters.value.event_type)
    if (statsFilters.value.start_date) params.append('start_date', statsFilters.value.start_date)
    if (statsFilters.value.end_date) params.append('end_date', statsFilters.value.end_date)

    const res = await api.get(`/api/duty-records/stats/detail?${params.toString()}`)
    const data = res.data || {}
    statsData.value = {
      overview: data.overview || {},
      byHandler: data.by_handler || [],
      byDutyPerson: data.by_duty_person || [],
      byProject: data.by_project || [],
      byStatus: data.by_status || [],
      byEventType: data.by_event_type || [],
      byFeedback: data.by_feedback || [],
      callDetails: data.call_details || [],
      trend: data.trend || [],
      responseTime: data.response_time || [],
      byEscalate: data.by_escalate || [],
      notEscalated: data.not_escalated || 0,
      callDistribution: data.call_distribution || [],
      callStats: data.call_stats || {}
    }
    // 数据加载完成后渲染图表
    setTimeout(() => renderAllCharts(), 100)
  } catch (e) {
    console.error('加载详细统计失败', e)
    appStore.showToast(t('dutyRecords.actions.loadStatsFailed'), 'error')
  } finally {
    statsLoading.value = false
  }
}

function clearStatsFilters() {
  statsFilters.value = {
    project_id: '',
    handler: '',
    duty_person: '',
    event_type: '',
    start_date: '',
    end_date: ''
  }
  timeRangePreset.value = 'all'
  loadDetailedStats()
}

function exportStats() {
  appStore.showToast(t('dutyRecords.actions.exportDeveloping'), 'info')
}

function getPieOffset(idx) {
  let offset = 0
  for (let i = 0; i < idx; i++) {
    offset += (statsData.value.byStatus[i]?.count || 0) / (statsData.value.overview?.total || 1) * 100
  }
  return offset
}

// 时间范围预设
function setTimeRange(preset) {
  timeRangePreset.value = preset
  const now = new Date()
  let start = '', end = ''
  
  switch (preset) {
    case 'today':
      start = end = now.toISOString().split('T')[0]
      break
    case 'week':
      const weekStart = new Date(now)
      weekStart.setDate(now.getDate() - now.getDay())
      start = weekStart.toISOString().split('T')[0]
      end = now.toISOString().split('T')[0]
      break
    case 'month':
      start = new Date(now.getFullYear(), now.getMonth(), 1).toISOString().split('T')[0]
      end = now.toISOString().split('T')[0]
      break
    case 'quarter':
      const quarterMonth = Math.floor(now.getMonth() / 3) * 3
      start = new Date(now.getFullYear(), quarterMonth, 1).toISOString().split('T')[0]
      end = now.toISOString().split('T')[0]
      break
    case 'all':
      start = ''
      end = ''
      break
  }
  
  statsFilters.value.start_date = start
  statsFilters.value.end_date = end
  loadDetailedStats()
}

// Chart.js 渲染函数 - 支持动态图表类型切换
let ChartJS = null

async function loadChartJS() {
  if (!ChartJS) {
    ChartJS = (await import('chart.js/auto')).default
  }
  return ChartJS
}

function getChartConfig(chartKey, chartType) {
  const colors = {
    primary: ['#3b82f6', '#2563eb'],
    success: ['#22c55e', '#16a34a'],
    warning: ['#f59e0b', '#d97706'],
    danger: ['#ef4444', '#dc2626'],
    purple: ['#8b5cf6', '#7c3aed'],
    teal: ['#10b981', '#059669'],
    indigo: ['#6366f1', '#4f46e5'],
    gray: ['#94a3b8', '#64748b']
  }
  const palette = ['#22c55e', '#f59e0b', '#8b5cf6', '#3b82f6', '#10b981', '#ef4444', '#6366f1', '#94a3b8']

  const isPieType = ['pie', 'doughnut'].includes(chartType)
  const legendPosition = isPieType ? 'right' : 'top'

  // 英文转中文映射表 - 根据实际定义的字段
  const eventTypeMap = {
    'inspection': '巡检发现',
    'alert': '监控告警',
    'customer_feedback': '客户反馈',
    'proactive_check': '值班人员主动排查'
  }
  const feedbackTypeMap = {
    'proactive': '主动发现',
    'customer': '客户反馈'
  }
  const escalateToMap = {
    'leader': '组长',
    'hod': 'HOD'
  }
  const translateEventType = (type) => eventTypeMap[type] || type || '未知'
  const translateFeedbackType = (type) => feedbackTypeMap[type] || type || '未知'
  const translateEscalateTo = (type) => escalateToMap[type] || type || '未上报'

  // 格式化日期显示 (将 2025-02-18 格式化为 02-18)
  const formatTrendDate = (dateStr) => {
    if (!dateStr) return ''
    // 如果包含T（ISO格式），取日期部分
    const datePart = dateStr.includes('T') ? dateStr.split('T')[0] : dateStr
    const parts = datePart.split('-')
    if (parts.length >= 3) {
      return `${parts[1]}-${parts[2]}`
    }
    return datePart.slice(-5)
  }

  const configs = {
    status: () => {
      const statusData = statsData.value.byStatus || []
      if (!statusData.length) return { labels: [], datasets: [] }
      return {
        labels: statusData.map(s => s.label),
        datasets: isPieType ? [{
          data: statusData.map(s => s.count),
          backgroundColor: palette,
          borderWidth: 0
        }] : [{
          label: '数量',
          data: statusData.map(s => s.count),
          backgroundColor: palette,
          borderRadius: 4
        }]
      }
    },
    project: () => {
      if (isPieType) {
        return {
          labels: statsData.value.byProject.map(p => p.project),
          datasets: [{ data: statsData.value.byProject.map(p => p.total), backgroundColor: palette, borderWidth: 0 }]
        }
      }
      return {
        labels: statsData.value.byProject.map(p => p.project),
        datasets: [
          { label: '总记录', data: statsData.value.byProject.map(p => p.total), backgroundColor: '#3b82f6', borderRadius: 4 },
          { label: '已解决', data: statsData.value.byProject.map(p => p.resolved), backgroundColor: '#22c55e', borderRadius: 4 },
          { label: '逾期', data: statsData.value.byProject.map(p => p.overdue), backgroundColor: '#ef4444', borderRadius: 4 }
        ]
      }
    },
    handler: () => {
      if (isPieType) {
        return {
          labels: statsData.value.byHandler.map(h => h.handler || '未分配'),
          datasets: [{ data: statsData.value.byHandler.map(h => h.total), backgroundColor: palette, borderWidth: 0 }]
        }
      }
      return {
        labels: statsData.value.byHandler.map(h => h.handler || '未分配'),
        datasets: [
          { label: '已解决', data: statsData.value.byHandler.map(h => h.resolved), backgroundColor: '#22c55e', borderRadius: 4 },
          { label: '检测正常', data: statsData.value.byHandler.map(h => h.normal), backgroundColor: '#10b981', borderRadius: 4 },
          { label: '待解决', data: statsData.value.byHandler.map(h => h.pending), backgroundColor: '#f59e0b', borderRadius: 4 },
          { label: '正在解决', data: statsData.value.byHandler.map(h => h.in_progress), backgroundColor: '#8b5cf6', borderRadius: 4 },
          { label: '逾期', data: statsData.value.byHandler.map(h => h.overdue), backgroundColor: '#ef4444', borderRadius: 4 }
        ]
      }
    },
    eventType: () => ({
      labels: statsData.value.byEventType.map(e => translateEventType(e.event_type)),
      datasets: isPieType ? [{
        data: statsData.value.byEventType.map(e => e.count),
        backgroundColor: palette,
        borderWidth: 0
      }] : [{
        label: '数量',
        data: statsData.value.byEventType.map(e => e.count),
        backgroundColor: palette,
        borderRadius: 4
      }]
    }),
    dutyPerson: () => {
      if (isPieType) {
        return {
          labels: statsData.value.byDutyPerson.map(d => d.duty_person),
          datasets: [{ data: statsData.value.byDutyPerson.map(d => d.total), backgroundColor: palette, borderWidth: 0 }]
        }
      }
      return {
        labels: statsData.value.byDutyPerson.map(d => d.duty_person),
        datasets: [
          { label: '记录问题数', data: statsData.value.byDutyPerson.map(d => d.total), backgroundColor: '#6366f1', borderRadius: 4 },
          { label: '已解决', data: statsData.value.byDutyPerson.map(d => d.resolved), backgroundColor: '#22c55e', borderRadius: 4 },
          { label: '检测正常', data: statsData.value.byDutyPerson.map(d => d.normal), backgroundColor: '#10b981', borderRadius: 4 },
          { label: '逾期', data: statsData.value.byDutyPerson.map(d => d.overdue), backgroundColor: '#ef4444', borderRadius: 4 }
        ]
      }
    },
    feedback: () => ({
      labels: statsData.value.byFeedback.map(f => translateFeedbackType(f.feedback_type)),
      datasets: isPieType ? [{
        data: statsData.value.byFeedback.map(f => f.count),
        backgroundColor: ['#3b82f6', '#f59e0b', '#22c55e', '#8b5cf6'],
        borderWidth: 0
      }] : [{
        label: '数量',
        data: statsData.value.byFeedback.map(f => f.count),
        backgroundColor: ['#3b82f6', '#f59e0b', '#22c55e', '#8b5cf6'],
        borderRadius: 4
      }]
    }),
    responseTime: () => ({
      labels: statsData.value.responseTime.map(r => r.range),
      datasets: chartType === 'line' ? [{
        label: '记录数',
        data: statsData.value.responseTime.map(r => r.count),
        borderColor: '#3b82f6',
        backgroundColor: 'rgba(59, 130, 246, 0.1)',
        fill: true,
        tension: 0.4
      }] : [{
        label: '记录数',
        data: statsData.value.responseTime.map(r => r.count),
        backgroundColor: ['#22c55e', '#10b981', '#3b82f6', '#f59e0b', '#ef4444'],
        borderRadius: 4
      }]
    }),
    trend: () => {
      const trendData = [...statsData.value.trend].reverse()
      if (chartType === 'bar') {
        return {
          labels: trendData.map(t => formatTrendDate(t.date)),
          datasets: [
            { label: '总记录', data: trendData.map(t => t.total), backgroundColor: '#3b82f6', borderRadius: 4 },
            { label: '逾期', data: trendData.map(t => t.overdue), backgroundColor: '#ef4444', borderRadius: 4 }
          ]
        }
      }
      return {
        labels: trendData.map(t => formatTrendDate(t.date)),
        datasets: [
          { label: '总记录', data: trendData.map(t => t.total), borderColor: '#3b82f6', backgroundColor: 'rgba(59, 130, 246, 0.1)', fill: true, tension: 0.4 },
          { label: '逾期', data: trendData.map(t => t.overdue), borderColor: '#ef4444', backgroundColor: 'rgba(239, 68, 68, 0.1)', fill: true, tension: 0.4 }
        ]
      }
    },
    callDist: () => {
      if (isPieType) {
        const total = statsData.value.callDetails.reduce((a, c) => a + c.answered + c.not_answered, 0)
        return {
          labels: ['接通', '未接通'],
          datasets: [{
            data: [
              statsData.value.callDetails.reduce((a, c) => a + c.answered, 0),
              statsData.value.callDetails.reduce((a, c) => a + c.not_answered, 0)
            ],
            backgroundColor: ['#22c55e', '#ef4444'],
            borderWidth: 0
          }]
        }
      }
      return {
        labels: statsData.value.callDetails.map(c => c.handler || '未知'),
        datasets: [
          { label: '接通', data: statsData.value.callDetails.map(c => c.answered), backgroundColor: '#22c55e', borderRadius: 4 },
          { label: '未接通', data: statsData.value.callDetails.map(c => c.not_answered), backgroundColor: '#ef4444', borderRadius: 4 }
        ]
      }
    },
    escalate: () => {
      const escalateData = [...statsData.value.byEscalate]
      if (statsData.value.notEscalated > 0) {
        escalateData.unshift({ escalate_to: '', count: statsData.value.notEscalated })
      }
      return {
        labels: escalateData.map(e => translateEscalateTo(e.escalate_to)),
        datasets: isPieType ? [{
          data: escalateData.map(e => e.count),
          backgroundColor: ['#94a3b8', '#f59e0b', '#ef4444', '#8b5cf6', '#3b82f6'],
          borderWidth: 0
        }] : [{
          label: '数量',
          data: escalateData.map(e => e.count),
          backgroundColor: ['#94a3b8', '#f59e0b', '#ef4444', '#8b5cf6', '#3b82f6'],
          borderRadius: 4
        }]
      }
    }
  }
  
  return configs[chartKey] ? configs[chartKey]() : null
}

function getChartOptions(chartKey, chartType) {
  const isPieType = ['pie', 'doughnut'].includes(chartType)
  const isHorizontal = chartType === 'horizontalBar' || chartKey === 'handler'
  const displayMode = pieDisplayMode.value[chartKey] || 'percent'

  const baseOptions = {
    responsive: true,
    maintainAspectRatio: false
  }

  if (isPieType) {
    return {
      ...baseOptions,
      plugins: { 
        legend: { position: 'right', labels: { usePointStyle: true, padding: 15 } },
        tooltip: {
          callbacks: {
            label: function(context) {
              const label = context.label || ''
              const value = context.parsed
              const total = context.dataset.data.reduce((a, b) => a + b, 0)
              const percent = ((value / total) * 100).toFixed(1)
              if (displayMode === 'percent') {
                return `${label}: ${percent}%`
              }
              return `${label}: ${value} (${percent}%)`
            }
          }
        },
        datalabels: displayMode === 'percent' ? {
          formatter: (value, ctx) => {
            const total = ctx.dataset.data.reduce((a, b) => a + b, 0)
            const percent = ((value / total) * 100).toFixed(0)
            return percent > 5 ? `${percent}%` : ''
          },
          color: '#fff',
          font: { weight: 'bold', size: 11 }
        } : {
          formatter: (value) => value > 0 ? value : '',
          color: '#fff',
          font: { weight: 'bold', size: 11 }
        }
      }
    }
  }
  
  if (chartKey === 'handler' && chartType === 'bar') {
    return {
      ...baseOptions,
      indexAxis: 'y',
      plugins: { legend: { position: 'top', labels: { usePointStyle: true } } },
      scales: { x: { stacked: true }, y: { stacked: true } }
    }
  }
  
  if (chartKey === 'callDist' && chartType === 'bar') {
    return {
      ...baseOptions,
      plugins: { legend: { position: 'top', labels: { usePointStyle: true } } },
      scales: { x: { stacked: true }, y: { stacked: true, beginAtZero: true } }
    }
  }
  
  if (chartKey === 'responseTime' && chartType === 'bar') {
    return {
      ...baseOptions,
      plugins: { legend: { display: false } },
      scales: { y: { beginAtZero: true } }
    }
  }
  
  return {
    ...baseOptions,
    plugins: { legend: { position: 'top', labels: { usePointStyle: true } } },
    scales: { y: { beginAtZero: true } }
  }
}

async function renderChart(chartKey) {
  const ChartModule = await loadChartJS()
  const canvasIdMap = {
    status: 'statsStatusChart',
    project: 'statsProjectChart',
    handler: 'statsHandlerChart',
    eventType: 'statsEventTypeChart',
    dutyPerson: 'statsDutyPersonChart',
    feedback: 'statsFeedbackChart',
    responseTime: 'statsResponseTimeChart',
    trend: 'statsTrendChart',
    callDist: 'statsCallDistChart',
    escalate: 'statsEscalateChart'
  }
  const canvasId = canvasIdMap[chartKey]
  if (!canvasId) return

  const canvas = document.getElementById(canvasId)
  if (!canvas) return

  // 使用 Chart.js 的 getChart 方法获取并销毁已存在的图表
  const existingChart = ChartModule.getChart(canvas)
  if (existingChart) {
    existingChart.destroy()
  }
  chartInstances.value[chartKey] = null

  const chartType = chartTypes.value[chartKey]
  const config = getChartConfig(chartKey, chartType)
  const options = getChartOptions(chartKey, chartType)

  if (!config || !config.labels || config.labels.length === 0) {
    return
  }

  const type = chartType === 'horizontalBar' ? 'bar' : chartType
  try {
    chartInstances.value[chartKey] = new ChartModule(canvas, { 
      type, 
      data: config, 
      options 
    })
  } catch (e) {
    console.error(`Chart error ${chartKey}:`, e)
  }
}

async function changeChartType(chartKey, newType) {
  chartTypes.value[chartKey] = newType
  await nextTick()
  await renderChart(chartKey)
}

async function togglePieDisplayMode(chartKey) {
  pieDisplayMode.value[chartKey] = pieDisplayMode.value[chartKey] === 'percent' ? 'count' : 'percent'
  await nextTick()
  await renderChart(chartKey)
}

async function renderAllCharts() {
  const ChartModule = await loadChartJS()

  // 清理所有已存在的图表
  const canvasIds = ['statsStatusChart', 'statsProjectChart', 'statsHandlerChart', 'statsEventTypeChart', 
    'statsDutyPersonChart', 'statsFeedbackChart', 'statsResponseTimeChart', 'statsTrendChart', 
    'statsCallDistChart', 'statsEscalateChart']
  
  canvasIds.forEach(id => {
    const canvas = document.getElementById(id)
    if (canvas) {
      const existingChart = ChartModule.getChart(canvas)
      if (existingChart) {
        existingChart.destroy()
      }
    }
  })
  chartInstances.value = {}

  const chartKeys = ['status', 'project', 'handler', 'eventType', 'dutyPerson', 'feedback', 'responseTime', 'trend', 'callDist', 'escalate']

  for (const key of chartKeys) {
    await renderChart(key)
  }
}

// Tab 切换时加载数据
watch(activeTab, (val) => {
  if (val === 'stats') {
    loadDetailedStats()
  }
})

function openModal(mode, record = null) {
  modalMode.value = mode
  if (mode === 'edit' && record) {
    form.value = { 
      ...record,
      escalate_to: record.escalate_to ? record.escalate_to.split(',').filter(s => s) : []
    }
  } else {
    form.value = getEmptyForm()
  }
  showModal.value = true
}

function openPlannedFixModal(record) {
  plannedFixForm.value = {
    id: record.id,
    planned_fix_time: record.planned_fix_time ? record.planned_fix_time.slice(0, 16) : '',
    duty_person: record.duty_person || '',
    project_name: record.project_name || ''
  }
  showPlannedFixModal.value = true
}

async function savePlannedFixTime() {
  try {
    await api.put(`/api/duty-records/${plannedFixForm.value.id}/planned-fix-time`, {
      planned_fix_time: plannedFixForm.value.planned_fix_time || ''
    })
    appStore.showToast(t('dutyRecords.actions.plannedFixTimeUpdated'), 'success')
    showPlannedFixModal.value = false
    loadRecords()
    loadStats()
  } catch (e) {
    const errMsg = typeof e.response?.data === 'object'
      ? (e.response.data.error || JSON.stringify(e.response.data))
      : (e.response?.data || e.message)
    appStore.showToast(t('dutyRecords.actions.updateFailed') + ': ' + errMsg, 'error')
  }
}

function viewDetail(record) {
  detailRecord.value = record
  detailPendingStatus.value = record.status
  showDetailModal.value = true
}

async function saveRecord() {
  if (!form.value.duty_date || !form.value.duty_person || !form.value.project_id) {
    appStore.showToast(t('dutyRecords.actions.fillRequired'), 'warning')
    return
  }
  if (!form.value.status) {
    appStore.showToast(t('dutyRecords.actions.selectStatus'), 'warning')
    return
  }

  const payload = {
    ...form.value,
    escalate_to: Array.isArray(form.value.escalate_to) ? form.value.escalate_to.join(',') : form.value.escalate_to
  }

  try {
    if (modalMode.value === 'add') {
      await api.post('/api/duty-records', payload)
      appStore.showToast(t('dutyRecords.actions.createSuccess'), 'success')
    } else {
      await api.put(`/api/duty-records/${form.value.id}`, payload)
      appStore.showToast(t('dutyRecords.actions.updateSuccess'), 'success')
    }
    showModal.value = false
    loadRecords()
    loadStats()
  } catch (e) {
    console.error('保存失败', e)
    const errMsg = typeof e.response?.data === 'object' ? (e.response.data.error || JSON.stringify(e.response.data)) : (e.response?.data || e.message)
    appStore.showToast(t('dutyRecords.actions.saveFailed') + ': ' + errMsg, 'error')
  }
}

async function quickUpdateStatus(record, newStatus) {
  try {
    const payload = { ...record, status: newStatus }
    if (Array.isArray(payload.escalate_to)) {
      payload.escalate_to = payload.escalate_to.join(',')
    }
    await api.put(`/api/duty-records/${record.id}`, payload)
    record.status = newStatus
    detailPendingStatus.value = newStatus
    appStore.showToast('状态更新成功', 'success')
    showDetailModal.value = false
    loadRecords()
    loadStats()
  } catch (e) {
    console.error('状态更新失败', e)
    detailPendingStatus.value = record.status
    appStore.showToast('状态更新失败', 'error')
  }
}

async function deleteRecord(record) {
  const confirmed = await appStore.showConfirm({
    type: 'warning',
    title: '确认删除',
    message: `确定要删除这条值班记录吗？`,
    okText: '确定删除',
    cancelText: '取消'
  })
  if (!confirmed) return

  try {
    await api.delete(`/api/duty-records/${record.id}`)
    appStore.showToast(t('dutyRecords.actions.deleteSuccess'), 'success')
    loadRecords()
    loadStats()
  } catch (e) {
    appStore.showToast(t('dutyRecords.actions.deleteFailed'), 'error')
  }
}

// 复制记录
function copyRecord(record) {
  const copy = { ...record }
  delete copy.id
  delete copy.created_at
  delete copy.updated_at
  copiedRecord.value = copy
  appStore.showToast('已复制记录', 'success')
}

// 打开粘贴弹窗
function openPasteModal() {
  console.log('[DEBUG] openPasteModal called, copiedRecord:', copiedRecord.value)
  if (!copiedRecord.value) {
    appStore.showToast('请先复制一条记录', 'warning')
    return
  }
  pasteCount.value = 1
  showPasteModal.value = true
  console.log('[DEBUG] showPasteModal set to:', showPasteModal.value)
}

// 粘贴记录
async function pasteRecords() {
  if (!copiedRecord.value || pasteCount.value < 1) return
  
  try {
    for (let i = 0; i < pasteCount.value; i++) {
      const newRecord = { ...copiedRecord.value }
      // 更新日期为当前日期时间
      const now = new Date()
      const pad = n => n.toString().padStart(2, '0')
      newRecord.duty_date = `${now.getFullYear()}-${pad(now.getMonth()+1)}-${pad(now.getDate())}T${pad(now.getHours())}:${pad(now.getMinutes())}`
      // 转换 escalate_to 数组为字符串
      const payload = {
        ...newRecord,
        escalate_to: Array.isArray(newRecord.escalate_to) ? newRecord.escalate_to.join(',') : newRecord.escalate_to
      }
      await api.post('/api/duty-records', payload)
    }
    appStore.showToast(`成功粘贴 ${pasteCount.value} 条记录`, 'success')
    showPasteModal.value = false
    await loadRecords()
    await loadStats()
  } catch (err) {
    appStore.showToast('粘贴失败: ' + (err.response?.data?.error || err.message), 'error')
  }
}

// 批量修改处理结果
function openBatchStatusModal() {
  if (selectedRecords.value.length === 0) {
    appStore.showToast(t('dutyRecords.actions.selectRecordsFirst'), 'warning')
    return
  }
  batchStatus.value = ''
  showBatchStatusModal.value = true
}

async function submitBatchStatus() {
  if (!batchStatus.value) {
    appStore.showToast(t('dutyRecords.actions.selectStatus'), 'warning')
    return
  }
  try {
    await api.put('/api/duty-records/batch/status', {
      ids: selectedRecords.value,
      status: batchStatus.value
    })
    appStore.showToast(`已批量修改 ${selectedRecords.value.length} 条记录`, 'success')
    showBatchStatusModal.value = false
    selectedRecords.value = []
    loadRecords()
    loadStats()
  } catch (e) {
    appStore.showToast(t('dutyRecords.actions.batchModifyFailed'), 'error')
  }
}

// 批量删除
async function batchDelete() {
  if (selectedRecords.value.length === 0) {
    appStore.showToast(t('dutyRecords.actions.selectDeleteFirst'), 'warning')
    return
  }
  const confirmed = await appStore.showConfirm({
    type: 'warning',
    title: '确认批量删除',
    message: `确定要删除选中的 ${selectedRecords.value.length} 条记录吗？此操作不可恢复！`,
    okText: '确定删除',
    cancelText: '取消'
  })
  if (!confirmed) return

  try {
    await api.delete('/api/duty-records/batch', {
      data: { ids: selectedRecords.value }
    })
    appStore.showToast(`已删除 ${selectedRecords.value.length} 条记录`, 'success')
    selectedRecords.value = []
    loadRecords()
    loadStats()
  } catch (e) {
    appStore.showToast(t('dutyRecords.actions.batchDeleteFailed'), 'error')
  }
}

// 切换单行选中状态
function toggleSelect(id) {
  const idx = selectedRecords.value.indexOf(id)
  if (idx > -1) {
    selectedRecords.value.splice(idx, 1)
  } else {
    selectedRecords.value.push(id)
  }
}

async function uploadFiles(files) {
  if (!files || files.length === 0) return
  
  const imageFiles = Array.from(files).filter(f => f.type.startsWith('image/'))
  if (imageFiles.length === 0) {
    appStore.showToast(t('dutyRecords.actions.selectImageFile'), 'warning')
    return
  }

  uploading.value = true
  const formData = new FormData()
  imageFiles.forEach(file => formData.append('files', file))

  try {
    const res = await api.post('/api/duty-records/upload', formData, {
      headers: { 'Content-Type': 'multipart/form-data' }
    })
    if (res.data.urls) {
      form.value.attachments = [...(form.value.attachments || []), ...res.data.urls]
      appStore.showToast(`成功上传 ${res.data.urls.length} 个文件`, 'success')
    }
  } catch (e) {
    console.error('上传失败', e)
    appStore.showToast(t('dutyRecords.actions.uploadFailed') + ': ' + (e.response?.data || e.message), 'error')
  } finally {
    uploading.value = false
  }
}

async function handleFileUpload(event) {
  await uploadFiles(event.target.files)
  event.target.value = ''
}

async function handlePaste(event) {
  const items = event.clipboardData?.items
  if (!items) return
  
  const files = []
  for (let i = 0; i < items.length; i++) {
    if (items[i].type.startsWith('image/')) {
      const file = items[i].getAsFile()
      if (file) files.push(file)
    }
  }
  
  if (files.length > 0) {
    event.preventDefault()
    await uploadFiles(files)
  }
}

async function handleDrop(event) {
  dragOver.value = false
  const files = event.dataTransfer?.files
  if (files) await uploadFiles(files)
}

function removeAttachment(index) {
  form.value.attachments.splice(index, 1)
}

async function openImagePreview(images, index) {
  // 批量获取预签名URL
  await batchPresignUrls(images)
  previewImages.value = images.map(img => getDisplayUrl(img))
  previewIndex.value = index
  showImagePreview.value = true
}

async function exportRecords() {
  const data = records.value
  if (!data.length) {
    appStore.showToast('没有数据可导出', 'warning')
    return
  }

  appStore.showToast('正在准备导出...', 'info')

  // 动态导入xlsx库
  const XLSX = await import('xlsx')

  // 定义导出字段顺序（不包含附件列）
  const exportFields = [
    { key: 'duty_date', title: '值班时间', width: 18 },
    { key: 'duty_person', title: '值班人员', width: 12 },
    { key: 'project_name', title: '项目', width: 15 },
    { key: 'feedback_type', title: '反馈类型', width: 12 },
    { key: 'event_type', title: '事件类型', width: 12 },
    { key: 'problem_desc', title: '问题描述', width: 30 },
    { key: 'handler', title: '处理人', width: 12 },
    { key: 'handle_result', title: '处理结果', width: 20 },
    { key: 'solution', title: '解决方案', width: 30 },
    { key: 'first_call_time', title: '首次拨打时间', width: 18 },
    { key: 'answer_time', title: '接听时间', width: 18 },
    { key: 'call_count', title: '拨打次数', width: 10 },
    { key: 'is_answered', title: '是否接听', width: 10 },
    { key: 'response_time', title: '响应时间(分钟)', width: 14 },
    { key: 'is_escalated', title: '是否上报', width: 10 },
    { key: 'escalate_to', title: '上报至', width: 15 },
    { key: 'has_handover', title: '是否交接', width: 10 },
    { key: 'handover_person', title: '交接人', width: 12 },
    { key: 'handover_content', title: '交接内容', width: 20 },
    { key: 'status', title: '状态', width: 12 },
    { key: 'planned_fix_time', title: '计划修复时间', width: 18 },
    { key: 'is_overdue', title: '是否超期', width: 10 },
    { key: 'overdue_reason', title: '超期原因', width: 20 },
    { key: 'created_at', title: '创建时间', width: 18 },
    { key: 'updated_at', title: '更新时间', width: 18 }
  ]

  const headers = exportFields.map(f => f.title)
  
  // 构建数据行
  const rows = data.map(r => {
    const row = {}
    exportFields.forEach(f => {
      let val = r[f.key]
      
      // 处理项目名称
      if (f.key === 'project_name') {
        row[f.title] = projects.value.find(p => p.id == r.project_id)?.name || r.project_name || ''
        return
      }
      
      // 处理布尔值
      if (f.key === 'is_escalated' || f.key === 'has_handover' || f.key === 'is_overdue') {
        row[f.title] = val ? '是' : '否'
        return
      }
      
      // 处理是否接听
      if (f.key === 'is_answered') {
        if (val === true || val === '已接听') row[f.title] = '已接听'
        else if (val === false || val === '未接听') row[f.title] = '未接听'
        else row[f.title] = val || '无'
        return
      }
      
      // 处理状态
      if (f.key === 'status') {
        row[f.title] = getStatusLabel(val)
        return
      }
      
      // 处理反馈类型
      if (f.key === 'feedback_type') {
        row[f.title] = getFeedbackLabel(val)
        return
      }
      
      // 处理事件类型
      if (f.key === 'event_type') {
        row[f.title] = getEventTypeLabel(val)
        return
      }
      
      // 处理日期时间
      if (f.key === 'duty_date' || f.key === 'first_call_time' || f.key === 'answer_time' || 
          f.key === 'planned_fix_time' || f.key === 'created_at' || f.key === 'updated_at') {
        row[f.title] = formatTimeDisplay(val)
        return
      }
      
      row[f.title] = val ?? ''
    })
    return row
  })

  // 创建工作簿和工作表
  const ws = XLSX.utils.json_to_sheet(rows, { header: headers })
  
  // 设置列宽
  ws['!cols'] = exportFields.map(f => ({ wch: f.width }))

  const wb = XLSX.utils.book_new()
  XLSX.utils.book_append_sheet(wb, ws, '值班记录')
  
  // 导出为xlsx文件
  XLSX.writeFile(wb, `值班记录_${new Date().toISOString().slice(0, 10)}.xlsx`)

  appStore.showToast('导出成功', 'success')
}

function getStatusLabel(status) {
  return statusOptions.value.find(s => s.value === status)?.label || status
}

function formatDate(dateStr) {
  if (!dateStr) return t('common.none')
  return dateStr.replace('T', ' ').slice(0, 16)
}

function formatDateTime(dateStr) {
  if (!dateStr) return ''
  return dateStr.replace('T', ' ').slice(0, 16)
}

function formatDateOnly(dateStr) {
  if (!dateStr) return ''
  return dateStr.slice(0, 10)
}

function formatTimeDisplay(timeStr) {
  if (!timeStr) return t('common.none')
  // 去掉时区后缀如 +08:00 或 Z
  return timeStr.replace(/[+-]\d{2}:\d{2}$/, '').replace('Z', '').replace('T', ' ')
}

function getStatusColor(status) {
  return statusOptions.value.find(s => s.value === status)?.color || '#6b7280'
}

function getFeedbackLabel(type) {
  return feedbackTypeOptions.value.find(t => t.value === type)?.label || type
}

function getEscalateLabel(type) {
  if (!type) return t('common.none')
  const types = type.split(',').filter(s => s)
  return types.map(tp => escalateOptions.value.find(o => o.value === tp)?.label || tp).join(', ')
}

function getEventTypeLabel(type) {
  return eventTypeOptions.value.find(tp => tp.value === type)?.label || type || t('common.none')
}

function isRecordOverdue(record) {
  if (record.is_overdue) return true
  if (!record.planned_fix_time) return false
  if (record.status === 'resolved' || record.status === 'normal') return false
  const planned = new Date(record.planned_fix_time + 'T23:59:59')
  return new Date() > planned
}

function clearFilters() {
  filters.value = {
    status: '',
    project_id: '',
    handler: '',
    duty_person: '',
    start_date: '',
    end_date: '',
    is_overdue: false,
    event_type: '',
    response_time_range: ''
  }
  loadRecords()
}

function handleProjectConfig() {
  openProjectModal('list')
}

function handleAddRecord() {
  openModal('add')
}

function toggleStats() {
  showStatsPanel.value = !showStatsPanel.value
}

function openProjectModal(mode, project = null) {
  if (mode === 'add' && !canProjectCreate.value) {
    appStore.showToast(t('dutyRecords.actions.noPermissionAdd'), 'warning')
    return
  }
  if (mode === 'edit' && !canProjectUpdate.value) {
    appStore.showToast(t('dutyRecords.actions.noPermissionEdit'), 'warning')
    return
  }
  projectModalMode.value = mode
  if (mode === 'edit' && project) {
    projectForm.value = { ...project }
  } else {
    projectForm.value = { id: '', name: '', code: '', description: '', status: 'active', sort_order: 0 }
  }
  showProjectModal.value = true
}

async function saveProject() {
  if (projectModalMode.value === 'add' && !canProjectCreate.value) {
    appStore.showToast(t('dutyRecords.actions.noPermissionAdd'), 'warning')
    return
  }
  if (projectModalMode.value === 'edit' && !canProjectUpdate.value) {
    appStore.showToast(t('dutyRecords.actions.noPermissionEdit'), 'warning')
    return
  }
  if (!projectForm.value.name || !projectForm.value.code) {
    appStore.showToast(t('dutyRecords.actions.fillProjectNameCode'), 'warning')
    return
  }
  try {
    if (projectModalMode.value === 'add') {
      await api.post('/api/duty-projects', projectForm.value)
      appStore.showToast(t('dutyRecords.actions.createSuccess'), 'success')
    } else {
      await api.put(`/api/duty-projects/${projectForm.value.id}`, projectForm.value)
      appStore.showToast(t('dutyRecords.actions.updateSuccess'), 'success')
    }
    showProjectModal.value = false
    loadProjects()
  } catch (e) {
    appStore.showToast(t('dutyRecords.actions.saveFailed') + ': ' + (e.response?.data || e.message), 'error')
  }
}

async function deleteProject(project) {
  if (!canProjectDelete.value) {
    appStore.showToast(t('dutyRecords.actions.noPermissionDelete'), 'warning')
    return
  }
  const confirmed = await appStore.showConfirm({
    type: 'warning',
    title: '确认删除',
    message: `确定要删除项目「${project.name}」吗？`,
    okText: '确定删除',
    cancelText: '取消'
  })
  if (!confirmed) return
  try {
    await api.delete(`/api/duty-projects/${project.id}`)
    appStore.showToast(t('dutyRecords.actions.deleteSuccess'), 'success')
    loadProjects()
  } catch (e) {
    appStore.showToast(t('dutyRecords.actions.deleteFailed') + ': ' + (e.response?.data || e.message), 'error')
  }
}

</script>

<template>
  <div class="duty-records-page">
    <div class="page-header">
      <div class="header-left">
        <h2>{{ t('dutyRecords.pageTitle') }}</h2>
        <span class="record-count" v-if="activeTab === 'list'">{{ t('dutyRecords.recordCount', { count: records.length }) }}</span>
      </div>
      <div class="header-actions">
        <button v-if="canOpenProjectConfig" class="btn btn-secondary" @click="openProjectModal('list')" type="button">
          <svg class="icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M12 6V4m0 2a2 2 0 100 4m0-4a2 2 0 110 4m-6 8a2 2 0 100-4m0 4a2 2 0 110-4m0 4v2m0-6V4m6 6v10m6-2a2 2 0 100-4m0 4a2 2 0 110-4m0 4v2m0-6V4"/></svg>
          {{ t('dutyRecords.projectConfig') }}
        </button>
        <button v-if="activeTab === 'list' && canExport" class="btn btn-default" @click="exportRecords" type="button">
          <svg class="icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M21 15v4a2 2 0 01-2 2H5a2 2 0 01-2-2v-4M7 10l5 5 5-5M12 15V3"/></svg>
          {{ t('common.export') }}
        </button>
        <button v-if="activeTab === 'list'" class="btn btn-secondary" @click="showStatsPanel = !showStatsPanel" type="button">
          <svg class="icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M18 20V10M12 20V4M6 20v-6"/></svg>
          {{ showStatsPanel ? t('dutyRecords.hideStats') : t('dutyRecords.showStats') }}
        </button>
        <button v-if="activeTab === 'list' && canCreate" class="btn btn-primary" @click="openModal('add')" type="button">
          <svg class="icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M12 5v14M5 12h14"/></svg>
          {{ t('dutyRecords.addRecord') }}
        </button>
        <button v-if="activeTab === 'list' && canCreate && copiedRecord" class="btn btn-secondary" @click="openPasteModal" type="button">
          <svg class="icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M16 4h2a2 2 0 0 1 2 2v14a2 2 0 0 1-2 2H6a2 2 0 0 1-2-2V6a2 2 0 0 1 2-2h2"/><rect x="8" y="2" width="8" height="4" rx="1" ry="1"/></svg>
          粘贴记录
        </button>
      </div>
    </div>
    
    <!-- Tab 切换 -->
    <div class="tab-nav">
      <button 
        class="tab-btn" 
        :class="{ active: activeTab === 'list' }"
        @click="activeTab = 'list'"
      >
        <svg class="icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M8 6h13M8 12h13M8 18h13M3 6h.01M3 12h.01M3 18h.01"/></svg>
        {{ t('dutyRecords.tabs.list') }}
      </button>
      <button 
        class="tab-btn" 
        :class="{ active: activeTab === 'stats' }"
        @click="activeTab = 'stats'"
      >
        <svg class="icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M18 20V10M12 20V4M6 20v-6"/></svg>
        {{ t('dutyRecords.tabs.stats') }}
      </button>
    </div>

    <!-- 记录列表 Tab 内容 -->
    <div v-if="activeTab === 'list'" class="tab-content">
    
    <!-- 统计面板 -->
    <div v-if="showStatsPanel" class="stats-panel">
      <div class="stats-cards">
        <div class="stat-card">
          <div class="stat-value">{{ stats.total || 0 }}</div>
          <div class="stat-label">{{ t('dutyRecords.stats.total') }}</div>
        </div>
        <div class="stat-card normal">
          <div class="stat-value">{{ stats.normal || 0 }}</div>
          <div class="stat-label">{{ t('dutyRecords.stats.normal') }}</div>
        </div>
        <div class="stat-card resolved">
          <div class="stat-value">{{ stats.resolved || 0 }}</div>
          <div class="stat-label">{{ t('dutyRecords.stats.resolved') }}</div>
        </div>
        <div class="stat-card pending">
          <div class="stat-value">{{ stats.pending || 0 }}</div>
          <div class="stat-label">{{ t('dutyRecords.stats.pending') }}</div>
        </div>
        <div class="stat-card in-progress">
          <div class="stat-value">{{ stats.in_progress || 0 }}</div>
          <div class="stat-label">{{ t('dutyRecords.stats.inProgress') }}</div>
        </div>
        <div class="stat-card temporary">
          <div class="stat-value">{{ stats.temporary || 0 }}</div>
          <div class="stat-label">{{ t('dutyRecords.stats.temporary') }}</div>
        </div>
        <div class="stat-card overdue">
          <div class="stat-value">{{ stats.overdue || 0 }}</div>
          <div class="stat-label">{{ t('dutyRecords.stats.overdue') }}</div>
        </div>
        <div class="stat-card month">
          <div class="stat-value">{{ stats.this_month || 0 }}</div>
          <div class="stat-label">{{ t('dutyRecords.stats.thisMonth') }}</div>
        </div>
      </div>

      <div class="stats-details">
        <div class="stats-section handover-section">
          <h4>
            {{ t('dutyRecords.stats.pendingHandover') }}
            <span class="count">{{ t('dutyRecords.stats.pendingCount', { count: recentHandovers.length }) }}</span>
          </h4>
          <div class="handover-list" v-if="recentHandovers.length">
            <div v-for="h in recentHandovers" :key="h.id" class="handover-item" @click="viewDetail(h)" style="cursor: pointer;">
              <!-- 顶部标签行：项目、反馈类型、事件类型、交接人、状态 -->
              <div class="handover-tags-row">
                <span class="tag-inline project"><b>{{ t('dutyRecords.handover.project') }}:</b> {{ h.project_name || t('dutyRecords.handover.unknownProject') }}</span>
                <span class="tag-inline" :class="['feedback', h.feedback_type]"><b>{{ t('dutyRecords.handover.feedbackType') }}:</b> {{ getFeedbackLabel(h.feedback_type) }}</span>
                <span class="tag-inline event"><b>{{ t('dutyRecords.handover.eventType') }}:</b> {{ getEventTypeLabel(h.event_type) }}</span>
                <span class="tag-inline handover-person"><b>{{ t('dutyRecords.handover.handoverPerson') }}:</b> {{ h.handover_person || '-' }}</span>
                <span :class="['tag-inline', 'status', h.status]"><b>{{ t('dutyRecords.handover.status') }}:</b> {{ getStatusLabel(h.status) }}</span>
              </div>
              
              <!-- 字段信息：按表格顺序 -->
              <div class="handover-fields">
                <div class="field-item">
                  <span class="field-label">{{ t('dutyRecords.handover.dutyPerson') }}</span>
                  <span class="field-value">{{ h.duty_person || '-' }}</span>
                </div>
                <div class="field-item">
                  <span class="field-label">{{ t('dutyRecords.handover.handler') }}</span>
                  <span class="field-value">{{ h.handler || '-' }}</span>
                </div>
                <div class="field-item">
                  <span class="field-label">{{ t('dutyRecords.handover.handoverTime') }}</span>
                  <span class="field-value time">{{ h.updated_at || '-' }}</span>
                </div>
              </div>
              
              <!-- 交接内容 -->
              <div class="handover-content-section">
                <span class="content-label">{{ t('dutyRecords.handover.handoverContent') }}</span>
                <div class="content-text">{{ h.handover_content || t('dutyRecords.handover.noContent') }}</div>
              </div>
            </div>
          </div>
          <div v-else class="empty-stats">{{ t('dutyRecords.stats.noHandover') }}</div>
        </div>
      </div>
    </div>

    <!-- 筛选条件 -->
    <div class="filters">
      <div class="filter-row">
        <div class="filter-group">
          <label>{{ t('dutyRecords.filters.status') }}</label>
          <select v-model="filters.status">
            <option value="">{{ t('common.all') }}</option>
            <option v-for="s in statusOptions" :key="s.value" :value="s.value">{{ s.label }}</option>
          </select>
        </div>
        <div class="filter-group">
          <label>{{ t('dutyRecords.filters.eventType') }}</label>
          <select v-model="filters.event_type">
            <option value="">{{ t('common.all') }}</option>
            <option v-for="e in eventTypeOptions" :key="e.value" :value="e.value">{{ e.label }}</option>
          </select>
        </div>
        <div class="filter-group">
          <label>{{ t('dutyRecords.filters.project') }}</label>
          <select v-model="filters.project_id">
            <option value="">{{ t('common.all') }}</option>
            <option v-for="p in projects" :key="p.id" :value="p.id">{{ p.name }}</option>
          </select>
        </div>
        <div class="filter-group">
          <label>{{ t('dutyRecords.filters.handler') }}</label>
          <input type="text" v-model="filters.handler" :placeholder="t('common.search')">
        </div>
        <div class="filter-group">
          <label>{{ t('dutyRecords.filters.dutyPerson') }}</label>
          <input type="text" v-model="filters.duty_person" :placeholder="t('common.search')">
        </div>
        <div class="filter-group">
          <label>{{ t('dutyRecords.filters.responseTime') }}</label>
          <select v-model="filters.response_time_range">
            <option value="">{{ t('common.all') }}</option>
            <option value="2min">{{ t('dutyRecords.filters.responseTimeOptions.within2min') }}</option>
            <option value="5min">{{ t('dutyRecords.filters.responseTimeOptions.within5min') }}</option>
            <option value="10min">{{ t('dutyRecords.filters.responseTimeOptions.within10min') }}</option>
            <option value="10min+">{{ t('dutyRecords.filters.responseTimeOptions.over10min') }}</option>
          </select>
        </div>
        <div class="filter-group">
          <label>{{ t('dutyRecords.filters.startDate') }}</label>
          <input type="date" v-model="filters.start_date">
        </div>
        <div class="filter-group">
          <label>{{ t('dutyRecords.filters.endDate') }}</label>
          <input type="date" v-model="filters.end_date">
        </div>
        <div class="filter-group checkbox-group">
          <label class="checkbox-label">
            <input type="checkbox" v-model="filters.is_overdue">
            <span>{{ t('dutyRecords.filters.overdueOnly') }}</span>
          </label>
        </div>
        <div class="filter-actions">
          <button class="btn btn-sm btn-primary" @click="loadRecords" type="button">
            <svg class="icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><circle cx="11" cy="11" r="8"/><path d="M21 21l-4.35-4.35"/></svg>
            {{ t('common.search') }}
          </button>
          <button class="btn btn-sm btn-default" @click="clearFilters" type="button">{{ t('common.reset') }}</button>
        </div>
      </div>
    </div>

    <!-- 批量操作栏 -->
    <div v-if="selectedRecords.length > 0 && activeTab === 'list'" class="batch-action-bar">
      <span class="batch-info">{{ t('common.selected') }} <strong>{{ selectedRecords.length }}</strong> {{ t('common.items') }}</span>
      <div class="batch-actions">
        <button type="button" class="batch-btn status-btn" @click.stop="openBatchStatusModal" v-if="canUpdate">
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M9 12l2 2 4-4m6 2a9 9 0 11-18 0 9 9 0 0118 0z"/></svg>
          {{ t('common.batchModifyStatus') }}
        </button>
        <button type="button" class="batch-btn delete-btn" @click.stop="batchDelete" v-if="canDelete">
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M19 7l-.867 12.142A2 2 0 0116.138 21H7.862a2 2 0 01-1.995-1.858L5 7m5 4v6m4-6v6m1-10V4a1 1 0 00-1-1h-4a1 1 0 00-1 1v3M4 7h16"/></svg>
          {{ t('dutyRecords.batch.batchDelete') }}
        </button>
        <button type="button" class="batch-btn cancel-btn" @click.stop="selectedRecords = []">
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M6 18L18 6M6 6l12 12"/></svg>
          {{ t('dutyRecords.batch.cancelSelect') }}
        </button>
      </div>
    </div>

    <!-- 数据表格 - 显示所有关键字段 -->
    <div class="table-wrapper">
      <table class="data-table">
        <thead>
          <tr>
            <th class="col-checkbox sticky-left">
              <input type="checkbox" v-model="selectAll" :title="t('common.selectAll')">
            </th>
            <th class="col-date">{{ t('dutyRecords.columns.date') }}</th>
            <th class="col-person">{{ t('dutyRecords.columns.dutyPerson') }}</th>
            <th class="col-project">{{ t('dutyRecords.columns.project') }}</th>
            <th class="col-task">{{ t('dutyRecords.columns.taskDesc') }}</th>
            <th class="col-feedback">{{ t('dutyRecords.columns.feedbackType') }}</th>
            <th class="col-event">{{ t('dutyRecords.columns.eventType') }}</th>
            <th class="col-desc">{{ t('dutyRecords.columns.problemDesc') }}</th>
            <th class="col-handler">{{ t('dutyRecords.columns.handler') }}</th>
            <th class="col-status">{{ t('dutyRecords.columns.status') }}</th>
            <th class="col-solution">{{ t('dutyRecords.columns.solution') }}</th>
            <th class="col-planned">{{ t('dutyRecords.columns.plannedFixTime') }}</th>
            <th class="col-overdue">{{ t('dutyRecords.columns.overdue') }}</th>
            <th class="col-time">{{ t('dutyRecords.columns.firstCallTime') }}</th>
            <th class="col-time">{{ t('dutyRecords.columns.answerTime') }}</th>
            <th class="col-call">{{ t('dutyRecords.columns.callCount') }}</th>
            <th class="col-answered">{{ t('dutyRecords.columns.answered') }}</th>
            <th class="col-resp">{{ t('dutyRecords.columns.responseTime') }}</th>
            <th class="col-escalate">{{ t('dutyRecords.columns.escalate') }}</th>
            <th class="col-handover">{{ t('dutyRecords.columns.handoverPerson') }}</th>
            <th class="col-handover-content">{{ t('dutyRecords.columns.handoverContent') }}</th>
            <th class="col-attach">{{ t('dutyRecords.columns.attachments') }}</th>
            <th class="col-creator">{{ t('dutyRecords.columns.operator') }}</th>
            <th class="col-time">{{ t('dutyRecords.columns.createdAt') }}</th>
            <th class="col-time">{{ t('dutyRecords.columns.updatedAt') }}</th>
            <th class="col-action sticky-right">{{ t('common.actions') }}</th>
          </tr>
        </thead>
        <tbody>
          <tr v-if="loading">
            <td colspan="26" class="loading-cell">
              <div class="loading-spinner"></div>
              加载中...
            </td>
          </tr>
          <tr v-else-if="paginatedRecords.length === 0">
            <td colspan="26" class="empty-cell">
              <svg class="empty-icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5"><path d="M9 5H7a2 2 0 00-2 2v12a2 2 0 002 2h10a2 2 0 002-2V7a2 2 0 00-2-2h-2M9 5a2 2 0 002 2h2a2 2 0 002-2M9 5a2 2 0 012-2h2a2 2 0 012 2"/></svg>
              <p>{{ t('dutyRecords.empty.noRecords') }}</p>
            </td>
          </tr>
          <tr v-for="r in paginatedRecords" :key="r.id" :class="{ overdue: r.is_overdue, selected: selectedRecords.includes(r.id) }">
            <td class="col-checkbox sticky-left" @click.stop>
              <input type="checkbox" :checked="selectedRecords.includes(r.id)" @change="toggleSelect(r.id)">
            </td>
            <td class="col-date nowrap">{{ formatDate(r.duty_date) }}</td>
            <td class="col-person nowrap">{{ r.duty_person }}</td>
            <td class="col-project nowrap">{{ r.project_name || t('common.none') }}</td>
            <td class="col-task" :title="r.task_desc">{{ r.task_desc || t('common.none') }}</td>
            <td class="col-feedback nowrap">
              <span class="feedback-badge" :class="r.feedback_type">{{ getFeedbackLabel(r.feedback_type) }}</span>
            </td>
            <td class="col-event nowrap">{{ getEventTypeLabel(r.event_type) }}</td>
            <td class="col-desc" :title="r.problem_desc">{{ r.problem_desc || t('common.none') }}</td>
            <td class="col-handler nowrap">{{ r.handler || t('common.none') }}</td>
            <td class="col-status nowrap">
              <span class="status-badge" :style="{ backgroundColor: getStatusColor(r.status) }">{{ getStatusLabel(r.status) }}</span>
            </td>
            <td class="col-solution" :title="r.solution">{{ r.solution || '-' }}</td>
            <td class="col-planned nowrap">
              <span>{{ formatDateTime(r.planned_fix_time) || t('common.none') }}</span>
              <button
                v-if="canEditPlannedFixTime"
                class="btn-mini-link"
                @click.stop="openPlannedFixModal(r)"
                :title="t('common.edit')"
              >
                {{ t('common.edit') }}
              </button>
            </td>
            <td class="col-overdue nowrap">
              <span v-if="isRecordOverdue(r)" class="overdue-badge" :title="r.overdue_reason">{{ t('dutyRecords.stats.overdue') }}</span>
              <span v-else>{{ t('common.none') }}</span>
            </td>
            <td class="col-time nowrap">{{ formatDateTime(r.first_call_time) || t('common.none') }}</td>
            <td class="col-time nowrap">{{ formatDateTime(r.answer_time) || t('common.none') }}</td>
            <td class="col-call nowrap">{{ r.call_count || 0 }}</td>
            <td class="col-answered nowrap">
              <span v-if="r.is_answered === '已接听' || r.is_answered === true || (r.answer_time && r.is_answered !== '未接听' && r.is_answered !== false)" class="answered-badge">已接听</span>
              <span v-else-if="r.is_answered === '未接听'" class="not-answered-badge">未接听</span>
              <span v-else class="none-badge">无</span>
            </td>
            <td class="col-resp nowrap">{{ r.response_time || t('common.none') }}</td>
            <td class="col-escalate nowrap">
              <span v-if="r.is_escalated" class="escalate-badge">{{ getEscalateLabel(r.escalate_to) }}</span>
              <span v-else>{{ t('common.none') }}</span>
            </td>
            <td class="col-handover nowrap">{{ r.has_handover ? (r.handover_person || t('common.yes')) : t('common.none') }}</td>
            <td class="col-handover-content" :title="r.handover_content">{{ r.has_handover ? (r.handover_content || t('common.none')) : t('common.none') }}</td>
            <td class="col-attach nowrap" @click.stop>
              <div v-if="r.attachments?.length" class="attachments-preview">
                <img v-for="(img, idx) in r.attachments.slice(0, 2)" :key="idx" :src="getDisplayUrl(img)" @click="openImagePreview(r.attachments, idx)" class="thumb">
                <span v-if="r.attachments.length > 2" class="more-count" @click="openImagePreview(r.attachments, 2)">+{{ r.attachments.length - 2 }}</span>
              </div>
              <span v-else>{{ t('common.none') }}</span>
            </td>
            <td class="col-creator nowrap">{{ r.updated_by || r.created_by || t('common.none') }}</td>
            <td class="col-time nowrap">{{ formatTimeDisplay(r.created_at) }}</td>
            <td class="col-time nowrap">{{ formatTimeDisplay(r.updated_at) }}</td>
            <td class="col-action nowrap sticky-right">
              <div class="action-buttons">
                <button class="btn-icon" @click="viewDetail(r)" :title="t('common.view')">
                  <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M1 12s4-8 11-8 11 8 11 8-4 8-11 8-11-8-11-8z"/><circle cx="12" cy="12" r="3"/></svg>
                </button>
                <button v-if="canCreate" class="btn-icon" @click="copyRecord(r)" title="复制">
                  <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><rect x="9" y="9" width="13" height="13" rx="2" ry="2"/><path d="M5 15H4a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2h9a2 2 0 0 1 2 2v1"/></svg>
                </button>
                <button v-if="canUpdate" class="btn-icon" @click="openModal('edit', r)" :title="t('common.edit')">
                  <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M11 4H4a2 2 0 00-2 2v14a2 2 0 002 2h14a2 2 0 002-2v-7M18.5 2.5a2.121 2.121 0 013 3L12 15l-4 1 1-4 9.5-9.5z"/></svg>
                </button>
                <button v-if="canEditPlannedFixTime" class="btn-icon warning" @click="openPlannedFixModal(r)" :title="t('dutyRecords.actions.editPlannedFixTime')">
                  <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><circle cx="12" cy="12" r="10"/><polyline points="12 6 12 12 16 14"/></svg>
                </button>
                <button v-if="canDelete" class="btn-icon danger" @click="deleteRecord(r)" :title="t('common.delete')">
                  <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M3 6h18M19 6v14a2 2 0 01-2 2H7a2 2 0 01-2-2V6m3 0V4a2 2 0 012-2h4a2 2 0 012 2v2"/></svg>
                </button>
              </div>
            </td>
          </tr>
        </tbody>
      </table>
    </div>

    <!-- 分页 -->
    <div class="pagination-wrapper" v-if="records.length > 0">
      <div class="pagination-info">
        {{ t('dutyRecords.pagination.total') }} {{ records.length }} {{ t('common.records') }}，{{ t('dutyRecords.pagination.page') }} {{ currentPage }} / {{ totalPages }}
      </div>
      <div class="pagination-controls">
        <select v-model="pageSize" @change="currentPage = 1" class="page-size-select">
          <option :value="10">10 {{ t('dutyRecords.pagination.perPage') }}</option>
          <option :value="20">20 {{ t('dutyRecords.pagination.perPage') }}</option>
          <option :value="50">50 {{ t('dutyRecords.pagination.perPage') }}</option>
          <option :value="100">100 {{ t('dutyRecords.pagination.perPage') }}</option>
        </select>
        <button class="page-btn" :disabled="currentPage === 1" @click="currentPage = 1">{{ t('dutyRecords.pagination.first') }}</button>
        <button class="page-btn" :disabled="currentPage === 1" @click="currentPage--">{{ t('dutyRecords.pagination.prev') }}</button>
        <span class="page-nums">
          <button v-for="p in visiblePages" :key="p" class="page-num" :class="{ active: p === currentPage }" @click="currentPage = p">{{ p }}</button>
        </span>
        <button class="page-btn" :disabled="currentPage === totalPages" @click="currentPage++">{{ t('dutyRecords.pagination.next') }}</button>
        <button class="page-btn" :disabled="currentPage === totalPages" @click="currentPage = totalPages">{{ t('dutyRecords.pagination.last') }}</button>
      </div>
    </div>
    
    </div><!-- 记录列表 Tab 结束 -->
    
    <!-- 统计分析 Tab 内容 -->
    <div v-if="activeTab === 'stats'" class="tab-content stats-tab">
      <!-- 页面头部 - 时间范围快捷选择 -->
      <div class="stats-page-header">
        <div class="time-range-selector">
          <button class="time-range-btn" :class="{ active: timeRangePreset === 'today' }" @click="setTimeRange('today')">{{ t('dutyRecords.statsPage.filters.today') }}</button>
          <button class="time-range-btn" :class="{ active: timeRangePreset === 'week' }" @click="setTimeRange('week')">{{ t('dutyRecords.statsPage.filters.thisWeek') }}</button>
          <button class="time-range-btn" :class="{ active: timeRangePreset === 'month' }" @click="setTimeRange('month')">{{ t('dutyRecords.statsPage.filters.thisMonth') }}</button>
          <button class="time-range-btn" :class="{ active: timeRangePreset === 'quarter' }" @click="setTimeRange('quarter')">{{ t('dutyRecords.statsPage.filters.thisQuarter') }}</button>
          <button class="time-range-btn" :class="{ active: timeRangePreset === 'all' }" @click="setTimeRange('all')">{{ t('dutyRecords.statsPage.filters.all') }}</button>
        </div>
        <button class="btn btn-sm btn-default" @click="exportStats" type="button">
          <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M21 15v4a2 2 0 01-2 2H5a2 2 0 01-2-2v-4M7 10l5 5 5-5M12 15V3"/></svg>
          {{ t('dutyRecords.statsPage.exportReport') }}
        </button>
      </div>
      
      <!-- 统计筛选条件 -->
      <div class="stats-filters">
        <div class="filter-row">
          <div class="filter-group">
            <label>{{ t('dutyRecords.statsPage.filters.project') }}</label>
            <select v-model="statsFilters.project_id">
              <option value="">{{ t('dutyRecords.statsPage.filters.allProjects') }}</option>
              <option v-for="p in projects" :key="p.id" :value="p.id">{{ p.name }}</option>
            </select>
          </div>
          <div class="filter-group">
            <label>{{ t('dutyRecords.statsPage.filters.handler') }}</label>
            <select v-model="statsFilters.handler">
              <option value="">{{ t('dutyRecords.statsPage.filters.allHandlers') }}</option>
              <option v-for="h in uniqueHandlers" :key="h" :value="h">{{ h }}</option>
            </select>
          </div>
          <div class="filter-group">
            <label>{{ t('dutyRecords.statsPage.filters.dutyPerson') }}</label>
            <select v-model="statsFilters.duty_person">
              <option value="">{{ t('dutyRecords.statsPage.filters.allDutyPersons') }}</option>
              <option v-for="d in uniqueDutyPersons" :key="d" :value="d">{{ d }}</option>
            </select>
          </div>
          <div class="filter-group">
            <label>{{ t('dutyRecords.statsPage.filters.eventType') }}</label>
            <select v-model="statsFilters.event_type">
              <option value="">{{ t('dutyRecords.statsPage.filters.allTypes') }}</option>
              <option v-for="e in eventTypeOptions" :key="e.value" :value="e.value">{{ e.label }}</option>
            </select>
          </div>
          <div class="filter-group">
            <label>{{ t('dutyRecords.statsPage.filters.startDate') }}</label>
            <input type="date" v-model="statsFilters.start_date">
          </div>
          <div class="filter-group">
            <label>{{ t('dutyRecords.statsPage.filters.endDate') }}</label>
            <input type="date" v-model="statsFilters.end_date">
          </div>
          <button class="btn btn-sm btn-primary" @click="loadDetailedStats" type="button">{{ t('common.apply') }}</button>
        </div>
      </div>
      
      <!-- 统计总览卡片 -->
      <div class="stats-overview-cards">
        <div class="overview-card total">
          <div class="card-value">{{ statsData.overview.total || 0 }}</div>
          <div class="card-label">{{ t('dutyRecords.stats.total') }}</div>
        </div>
        <div class="overview-card normal">
          <div class="card-value">{{ statsData.overview.normal || 0 }}</div>
          <div class="card-label">{{ t('dutyRecords.stats.normal') }}</div>
        </div>
        <div class="overview-card resolved">
          <div class="card-value">{{ statsData.overview.resolved || 0 }}</div>
          <div class="card-label">{{ t('dutyRecords.stats.resolved') }}</div>
        </div>
        <div class="overview-card pending">
          <div class="card-value">{{ statsData.overview.pending || 0 }}</div>
          <div class="card-label">{{ t('dutyRecords.stats.pending') }}</div>
        </div>
        <div class="overview-card in-progress">
          <div class="card-value">{{ statsData.overview.in_progress || 0 }}</div>
          <div class="card-label">{{ t('dutyRecords.stats.inProgress') }}</div>
        </div>
        <div class="overview-card temporary">
          <div class="card-value">{{ statsData.overview.temporary || 0 }}</div>
          <div class="card-label">{{ t('dutyRecords.stats.temporary') }}</div>
        </div>
        <div class="overview-card overdue">
          <div class="card-value">{{ statsData.overview.overdue || 0 }}</div>
          <div class="card-label">{{ t('dutyRecords.stats.overdue') }}</div>
        </div>
      </div>
      
      <!-- 加载中 -->
      <div v-if="statsLoading" class="stats-loading">
        <div class="loading-spinner"></div>
        <span>加载统计数据中...</span>
      </div>
      
      <!-- 统计图表区域 -->
      <div v-else class="stats-charts-grid">
        <!-- 按处理结果统计 -->
        <div class="chart-card">
          <div class="chart-header">
            <span class="chart-title">按处理结果统计</span>
            <div class="chart-type-selector">
              <button class="chart-type-btn" :class="{ active: chartTypes.status === 'bar' }" @click="changeChartType('status', 'bar')" title="条形图">
                <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><rect x="3" y="12" width="4" height="9"/><rect x="10" y="6" width="4" height="15"/><rect x="17" y="3" width="4" height="18"/></svg>
              </button>
              <button class="chart-type-btn" :class="{ active: chartTypes.status === 'pie' }" @click="changeChartType('status', 'pie')" title="饼图">
                <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M21.21 15.89A10 10 0 118 2.83"/><path d="M22 12A10 10 0 0012 2v10z"/></svg>
              </button>
              <button class="chart-type-btn" :class="{ active: chartTypes.status === 'doughnut' }" @click="changeChartType('status', 'doughnut')" title="环形图">
                <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><circle cx="12" cy="12" r="10"/><circle cx="12" cy="12" r="4"/></svg>
              </button>
              <span class="chart-type-divider" v-if="['pie', 'doughnut'].includes(chartTypes.status)">|</span>
              <button class="chart-type-btn display-mode-btn" v-if="['pie', 'doughnut'].includes(chartTypes.status)" :class="{ active: pieDisplayMode.status === 'percent' }" @click="togglePieDisplayMode('status')" :title="pieDisplayMode.status === 'percent' ? '显示数量' : '显示百分比'">
                <span class="mode-text">{{ pieDisplayMode.status === 'percent' ? '%' : '#' }}</span>
              </button>
            </div>
          </div>
          <div class="chart-body">
            <div class="chart-container">
              <canvas id="statsStatusChart"></canvas>
            </div>
            <div v-if="!statsData.byStatus.length" class="empty-chart">暂无数据</div>
          </div>
        </div>
        
        <!-- 按项目统计 -->
        <div class="chart-card">
          <div class="chart-header">
            <span class="chart-title">按项目统计</span>
            <div class="chart-type-selector">
              <button class="chart-type-btn" :class="{ active: chartTypes.project === 'bar' }" @click="changeChartType('project', 'bar')" title="条形图">
                <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><rect x="3" y="12" width="4" height="9"/><rect x="10" y="6" width="4" height="15"/><rect x="17" y="3" width="4" height="18"/></svg>
              </button>
              <button class="chart-type-btn" :class="{ active: chartTypes.project === 'pie' }" @click="changeChartType('project', 'pie')" title="饼图">
                <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M21.21 15.89A10 10 0 118 2.83"/><path d="M22 12A10 10 0 0012 2v10z"/></svg>
              </button>
              <button class="chart-type-btn" :class="{ active: chartTypes.project === 'doughnut' }" @click="changeChartType('project', 'doughnut')" title="环形图">
                <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><circle cx="12" cy="12" r="10"/><circle cx="12" cy="12" r="4"/></svg>
              </button>
              <span class="chart-type-divider" v-if="['pie', 'doughnut'].includes(chartTypes.project)">|</span>
              <button class="chart-type-btn display-mode-btn" v-if="['pie', 'doughnut'].includes(chartTypes.project)" :class="{ active: pieDisplayMode.project === 'percent' }" @click="togglePieDisplayMode('project')" :title="pieDisplayMode.project === 'percent' ? '显示数量' : '显示百分比'">
                <span class="mode-text">{{ pieDisplayMode.project === 'percent' ? '%' : '#' }}</span>
              </button>
            </div>
          </div>
          <div class="chart-body">
            <div class="chart-container">
              <canvas id="statsProjectChart"></canvas>
            </div>
            <div v-if="!statsData.byProject.length" class="empty-chart">暂无数据</div>
          </div>
        </div>
        
        <!-- 按处理人统计 -->
        <div class="chart-card">
          <div class="chart-header">
            <span class="chart-title">按处理人统计</span>
            <div class="chart-type-selector">
              <button class="chart-type-btn" :class="{ active: chartTypes.handler === 'bar' }" @click="changeChartType('handler', 'bar')" title="堆叠条形图">
                <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><rect x="3" y="3" width="18" height="6"/><rect x="3" y="11" width="18" height="6"/><rect x="3" y="19" width="12" height="2"/></svg>
              </button>
              <button class="chart-type-btn" :class="{ active: chartTypes.handler === 'pie' }" @click="changeChartType('handler', 'pie')" title="饼图">
                <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M21.21 15.89A10 10 0 118 2.83"/><path d="M22 12A10 10 0 0012 2v10z"/></svg>
              </button>
              <button class="chart-type-btn" :class="{ active: chartTypes.handler === 'doughnut' }" @click="changeChartType('handler', 'doughnut')" title="环形图">
                <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><circle cx="12" cy="12" r="10"/><circle cx="12" cy="12" r="4"/></svg>
              </button>
            </div>
          </div>
          <div class="chart-body">
            <div class="chart-container">
              <canvas id="statsHandlerChart"></canvas>
            </div>
            <div v-if="!statsData.byHandler.length" class="empty-chart">暂无数据</div>
          </div>
        </div>
        
        <!-- 按事件类型统计 -->
        <div class="chart-card">
          <div class="chart-header">
            <span class="chart-title">按事件类型统计</span>
            <div class="chart-type-selector">
              <button class="chart-type-btn" :class="{ active: chartTypes.eventType === 'bar' }" @click="changeChartType('eventType', 'bar')" title="条形图">
                <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><rect x="3" y="12" width="4" height="9"/><rect x="10" y="6" width="4" height="15"/><rect x="17" y="3" width="4" height="18"/></svg>
              </button>
              <button class="chart-type-btn" :class="{ active: chartTypes.eventType === 'pie' }" @click="changeChartType('eventType', 'pie')" title="饼图">
                <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M21.21 15.89A10 10 0 118 2.83"/><path d="M22 12A10 10 0 0012 2v10z"/></svg>
              </button>
              <button class="chart-type-btn" :class="{ active: chartTypes.eventType === 'doughnut' }" @click="changeChartType('eventType', 'doughnut')" title="环形图">
                <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><circle cx="12" cy="12" r="10"/><circle cx="12" cy="12" r="4"/></svg>
              </button>
              <span class="chart-type-divider" v-if="['pie', 'doughnut'].includes(chartTypes.eventType)">|</span>
              <button class="chart-type-btn display-mode-btn" v-if="['pie', 'doughnut'].includes(chartTypes.eventType)" :class="{ active: pieDisplayMode.eventType === 'percent' }" @click="togglePieDisplayMode('eventType')" :title="pieDisplayMode.eventType === 'percent' ? '显示数量' : '显示百分比'">
                <span class="mode-text">{{ pieDisplayMode.eventType === 'percent' ? '%' : '#' }}</span>
              </button>
            </div>
          </div>
          <div class="chart-body">
            <div class="chart-container">
              <canvas id="statsEventTypeChart"></canvas>
            </div>
            <div v-if="!statsData.byEventType.length" class="empty-chart">暂无数据</div>
          </div>
        </div>
        
        <!-- 按值班人统计 -->
        <div class="chart-card">
          <div class="chart-header">
            <span class="chart-title">按值班人统计（记录问题数）</span>
            <div class="chart-type-selector">
              <button class="chart-type-btn" :class="{ active: chartTypes.dutyPerson === 'bar' }" @click="changeChartType('dutyPerson', 'bar')" title="条形图">
                <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><rect x="3" y="12" width="4" height="9"/><rect x="10" y="6" width="4" height="15"/><rect x="17" y="3" width="4" height="18"/></svg>
              </button>
              <button class="chart-type-btn" :class="{ active: chartTypes.dutyPerson === 'pie' }" @click="changeChartType('dutyPerson', 'pie')" title="饼图">
                <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M21.21 15.89A10 10 0 118 2.83"/><path d="M22 12A10 10 0 0012 2v10z"/></svg>
              </button>
              <button class="chart-type-btn" :class="{ active: chartTypes.dutyPerson === 'doughnut' }" @click="changeChartType('dutyPerson', 'doughnut')" title="环形图">
                <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><circle cx="12" cy="12" r="10"/><circle cx="12" cy="12" r="4"/></svg>
              </button>
            </div>
          </div>
          <div class="chart-body">
            <div class="chart-container">
              <canvas id="statsDutyPersonChart"></canvas>
            </div>
            <div v-if="!statsData.byDutyPerson.length" class="empty-chart">暂无数据</div>
          </div>
        </div>
        
        <!-- 按反馈类型统计 -->
        <div class="chart-card">
          <div class="chart-header">
            <span class="chart-title">按反馈类型统计</span>
            <div class="chart-type-selector">
              <button class="chart-type-btn" :class="{ active: chartTypes.feedback === 'bar' }" @click="changeChartType('feedback', 'bar')" title="条形图">
                <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><rect x="3" y="12" width="4" height="9"/><rect x="10" y="6" width="4" height="15"/><rect x="17" y="3" width="4" height="18"/></svg>
              </button>
              <button class="chart-type-btn" :class="{ active: chartTypes.feedback === 'pie' }" @click="changeChartType('feedback', 'pie')" title="饼图">
                <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M21.21 15.89A10 10 0 118 2.83"/><path d="M22 12A10 10 0 0012 2v10z"/></svg>
              </button>
              <button class="chart-type-btn" :class="{ active: chartTypes.feedback === 'doughnut' }" @click="changeChartType('feedback', 'doughnut')" title="环形图">
                <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><circle cx="12" cy="12" r="10"/><circle cx="12" cy="12" r="4"/></svg>
              </button>
              <span class="chart-type-divider" v-if="['pie', 'doughnut'].includes(chartTypes.feedback)">|</span>
              <button class="chart-type-btn display-mode-btn" v-if="['pie', 'doughnut'].includes(chartTypes.feedback)" :class="{ active: pieDisplayMode.feedback === 'percent' }" @click="togglePieDisplayMode('feedback')" :title="pieDisplayMode.feedback === 'percent' ? '显示数量' : '显示百分比'">
                <span class="mode-text">{{ pieDisplayMode.feedback === 'percent' ? '%' : '#' }}</span>
              </button>
            </div>
          </div>
          <div class="chart-body">
            <div class="chart-container">
              <canvas id="statsFeedbackChart"></canvas>
            </div>
            <div v-if="!statsData.byFeedback.length" class="empty-chart">暂无数据</div>
          </div>
        </div>
        
        <!-- 响应时长分布 -->
        <div class="chart-card">
          <div class="chart-header">
            <span class="chart-title">响应时长分布</span>
            <div class="chart-type-selector">
              <button class="chart-type-btn" :class="{ active: chartTypes.responseTime === 'bar' }" @click="changeChartType('responseTime', 'bar')" title="条形图">
                <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><rect x="3" y="12" width="4" height="9"/><rect x="10" y="6" width="4" height="15"/><rect x="17" y="3" width="4" height="18"/></svg>
              </button>
              <button class="chart-type-btn" :class="{ active: chartTypes.responseTime === 'line' }" @click="changeChartType('responseTime', 'line')" title="折线图">
                <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><polyline points="22,6 13.5,14.5 8.5,9.5 2,16"/></svg>
              </button>
            </div>
          </div>
          <div class="chart-body">
            <div class="chart-container">
              <canvas id="statsResponseTimeChart"></canvas>
            </div>
            <div class="mini-stats" v-if="statsData.overview.avg_response">
              <div class="mini-stat">
                <div class="value">{{ statsData.overview.avg_response }}分钟</div>
                <div class="label">平均响应</div>
              </div>
              <div class="mini-stat">
                <div class="value">{{ statsData.overview.min_response || 0 }}分钟</div>
                <div class="label">最快响应</div>
              </div>
              <div class="mini-stat">
                <div class="value">{{ statsData.overview.max_response || 0 }}分钟</div>
                <div class="label">最慢响应</div>
              </div>
            </div>
            <div v-if="!statsData.responseTime.length" class="empty-chart">暂无数据</div>
          </div>
        </div>
        
        <!-- 每日记录趋势 (全宽) -->
        <div class="chart-card full-width">
          <div class="chart-header">
            <span class="chart-title">每日记录趋势（含逾期对比）</span>
            <div class="chart-type-selector">
              <button class="chart-type-btn" :class="{ active: chartTypes.trend === 'line' }" @click="changeChartType('trend', 'line')" title="折线图">
                <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><polyline points="22,6 13.5,14.5 8.5,9.5 2,16"/></svg>
              </button>
              <button class="chart-type-btn" :class="{ active: chartTypes.trend === 'bar' }" @click="changeChartType('trend', 'bar')" title="条形图">
                <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><rect x="3" y="12" width="4" height="9"/><rect x="10" y="6" width="4" height="15"/><rect x="17" y="3" width="4" height="18"/></svg>
              </button>
            </div>
          </div>
          <div class="chart-body chart-body-tall">
            <div class="chart-container chart-container-tall">
              <canvas id="statsTrendChart"></canvas>
            </div>
            <div v-if="!statsData.trend.length" class="empty-chart">暂无数据</div>
          </div>
        </div>
        
        <!-- 拨打次数分布 -->
        <div class="chart-card">
          <div class="chart-header">
            <span class="chart-title">拨打次数分布（按处理人）</span>
            <div class="chart-type-selector">
              <button class="chart-type-btn" :class="{ active: chartTypes.callDist === 'bar' }" @click="changeChartType('callDist', 'bar')" title="堆叠条形图">
                <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><rect x="3" y="12" width="4" height="9"/><rect x="10" y="6" width="4" height="15"/><rect x="17" y="3" width="4" height="18"/></svg>
              </button>
              <button class="chart-type-btn" :class="{ active: chartTypes.callDist === 'pie' }" @click="changeChartType('callDist', 'pie')" title="饼图">
                <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M21.21 15.89A10 10 0 118 2.83"/><path d="M22 12A10 10 0 0012 2v10z"/></svg>
              </button>
              <button class="chart-type-btn" :class="{ active: chartTypes.callDist === 'doughnut' }" @click="changeChartType('callDist', 'doughnut')" title="环形图">
                <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><circle cx="12" cy="12" r="10"/><circle cx="12" cy="12" r="4"/></svg>
              </button>
            </div>
          </div>
          <div class="chart-body">
            <div class="chart-container">
              <canvas id="statsCallDistChart"></canvas>
            </div>
            <div class="mini-stats" v-if="statsData.callStats.avg_call_count">
              <div class="mini-stat">
                <div class="value">{{ statsData.callStats.avg_call_count }}次</div>
                <div class="label">平均拨打</div>
              </div>
              <div class="mini-stat">
                <div class="value">{{ statsData.callStats.answer_rate || 0 }}%</div>
                <div class="label">接通率</div>
              </div>
              <div class="mini-stat">
                <div class="value">{{ statsData.overview.avg_response || 0 }}分钟</div>
                <div class="label">平均响应</div>
              </div>
            </div>
            <div v-if="!statsData.callDetails.length" class="empty-chart">暂无数据</div>
          </div>
        </div>
        
        <!-- 上报问题统计 -->
        <div class="chart-card">
          <div class="chart-header">
            <span class="chart-title">上报问题统计</span>
            <div class="chart-type-selector">
              <button class="chart-type-btn" :class="{ active: chartTypes.escalate === 'bar' }" @click="changeChartType('escalate', 'bar')" title="条形图">
                <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><rect x="3" y="12" width="4" height="9"/><rect x="10" y="6" width="4" height="15"/><rect x="17" y="3" width="4" height="18"/></svg>
              </button>
              <button class="chart-type-btn" :class="{ active: chartTypes.escalate === 'pie' }" @click="changeChartType('escalate', 'pie')" title="饼图">
                <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M21.21 15.89A10 10 0 118 2.83"/><path d="M22 12A10 10 0 0012 2v10z"/></svg>
              </button>
              <button class="chart-type-btn" :class="{ active: chartTypes.escalate === 'doughnut' }" @click="changeChartType('escalate', 'doughnut')" title="环形图">
                <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><circle cx="12" cy="12" r="10"/><circle cx="12" cy="12" r="4"/></svg>
              </button>
              <span class="chart-type-divider" v-if="['pie', 'doughnut'].includes(chartTypes.escalate)">|</span>
              <button class="chart-type-btn display-mode-btn" v-if="['pie', 'doughnut'].includes(chartTypes.escalate)" :class="{ active: pieDisplayMode.escalate === 'percent' }" @click="togglePieDisplayMode('escalate')" :title="pieDisplayMode.escalate === 'percent' ? '显示数量' : '显示百分比'">
                <span class="mode-text">{{ pieDisplayMode.escalate === 'percent' ? '%' : '#' }}</span>
              </button>
            </div>
          </div>
          <div class="chart-body">
            <div class="chart-container">
              <canvas id="statsEscalateChart"></canvas>
            </div>
            <div v-if="!statsData.byEscalate.length && !statsData.notEscalated" class="empty-chart">暂无数据</div>
          </div>
        </div>
      </div>
      
      <!-- 详细统计表格区域 -->
      <div v-if="!statsLoading" class="stats-tables-section">
        <!-- 处理人拨打详情表格 -->
        <div class="stats-table-card">
          <div class="table-header">
            <span class="table-title">处理人拨打详情（含响应时长）</span>
          </div>
          <table class="stats-table" v-if="statsData.callDetails.length">
            <thead>
              <tr>
                <th>处理人</th>
                <th>总拨打次数</th>
                <th>接通次数</th>
                <th>未接通次数</th>
                <th>接通率</th>
                <th>平均拨打次数</th>
                <th>首次响应(分)</th>
                <th>平均响应(分)</th>
                <th>最长响应(分)</th>
                <th>响应效率</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="c in statsData.callDetails" :key="c.handler">
                <td><strong>{{ c.handler || '未知' }}</strong></td>
                <td>{{ c.total_calls }}</td>
                <td><span class="badge badge-green">{{ c.answered }}</span></td>
                <td><span class="badge badge-red">{{ c.not_answered }}</span></td>
                <td>
                  <div class="progress-cell">
                    <div class="progress-bar" style="width: 80px;">
                      <div class="progress-fill" :class="c.answer_rate >= 80 ? 'green' : c.answer_rate >= 60 ? 'yellow' : 'red'" :style="{ width: c.answer_rate + '%' }"></div>
                    </div>
                    <span class="progress-text" :class="c.answer_rate >= 80 ? 'text-green' : c.answer_rate >= 60 ? 'text-yellow' : 'text-red'">{{ c.answer_rate }}%</span>
                  </div>
                </td>
                <td>{{ c.avg_call_count || '-' }}次</td>
                <td>{{ c.first_response || '-' }}</td>
                <td>{{ c.avg_response || '-' }}</td>
                <td>{{ c.max_response || '-' }}</td>
                <td>
                  <div class="progress-cell">
                    <div class="progress-bar" style="width: 80px;">
                      <div class="progress-fill blue" :style="{ width: Math.min(100 - (c.avg_response || 0) / 60 * 100, 100) + '%' }"></div>
                    </div>
                    <span class="progress-text text-blue">{{ Math.max(0, Math.round(100 - (c.avg_response || 0) / 60 * 100)) }}%</span>
                  </div>
                </td>
              </tr>
            </tbody>
          </table>
          <div v-else class="empty-table">暂无数据</div>
        </div>
        
        <!-- 处理人详细统计表格 -->
        <div class="stats-table-card">
          <div class="table-header">
            <span class="table-title">处理人详细统计</span>
          </div>
          <table class="stats-table" v-if="statsData.byHandler.length">
            <thead>
              <tr>
                <th>处理人</th>
                <th>总处理</th>
                <th>检测正常</th>
                <th>已解决</th>
                <th>待解决</th>
                <th>正在解决</th>
                <th>逾期</th>
                <th>平均响应(分)</th>
                <th>解决率</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="h in statsData.byHandler" :key="h.handler">
                <td><strong>{{ h.handler || '未分配' }}</strong></td>
                <td>{{ h.total }}</td>
                <td><span class="badge badge-green">{{ h.normal }}</span></td>
                <td><span class="badge badge-green">{{ h.resolved }}</span></td>
                <td><span class="badge badge-yellow">{{ h.pending }}</span></td>
                <td><span class="badge badge-purple">{{ h.in_progress }}</span></td>
                <td><span class="badge badge-red" v-if="h.overdue > 0">{{ h.overdue }}</span><span v-else>0</span></td>
                <td>{{ '-' }}</td>
                <td>
                  <div class="progress-cell">
                    <div class="progress-bar" style="width: 100px;">
                      <div class="progress-fill" :class="((h.resolved + h.normal) / (h.total || 1) * 100) >= 80 ? 'green' : 'yellow'" :style="{ width: ((h.resolved + h.normal) / (h.total || 1) * 100) + '%' }"></div>
                    </div>
                    <span class="progress-text" :class="((h.resolved + h.normal) / (h.total || 1) * 100) >= 80 ? 'text-green' : 'text-yellow'">{{ Math.round((h.resolved + h.normal) / (h.total || 1) * 100) }}%</span>
                  </div>
                </td>
              </tr>
            </tbody>
          </table>
          <div v-else class="empty-table">暂无数据</div>
        </div>
        
        <!-- 值班人详细统计表格 -->
        <div class="stats-table-card">
          <div class="table-header">
            <span class="table-title">值班人详细统计（记录问题数）</span>
          </div>
          <table class="stats-table" v-if="statsData.byDutyPerson.length">
            <thead>
              <tr>
                <th>值班人</th>
                <th>记录问题数</th>
                <th>检测正常</th>
                <th>已解决</th>
                <th>待解决</th>
                <th>正在解决</th>
                <th>逾期</th>
                <th>上报次数</th>
                <th>平均响应(分)</th>
                <th>问题发现率</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="d in statsData.byDutyPerson" :key="d.duty_person">
                <td><strong>{{ d.duty_person || '未知' }}</strong></td>
                <td><span class="badge badge-blue">{{ d.total }}</span></td>
                <td><span class="badge badge-green">{{ d.normal }}</span></td>
                <td><span class="badge badge-green">{{ d.resolved }}</span></td>
                <td><span class="badge badge-yellow">{{ d.pending }}</span></td>
                <td><span class="badge badge-purple">{{ 0 }}</span></td>
                <td><span class="badge badge-red" v-if="d.overdue > 0">{{ d.overdue }}</span><span v-else>0</span></td>
                <td>{{ d.escalated || 0 }}</td>
                <td>{{ d.avg_response || 0 }}</td>
                <td>
                  <div class="progress-cell">
                    <div class="progress-bar" style="width: 100px;">
                      <div class="progress-fill blue" :style="{ width: ((d.total - d.normal) / (statsData.overview.total || 1) * 100) + '%' }"></div>
                    </div>
                    <span class="progress-text text-blue">{{ Math.round((d.total - d.normal) / (statsData.overview.total || 1) * 100) }}%</span>
                  </div>
                </td>
              </tr>
            </tbody>
          </table>
          <div v-else class="empty-table">暂无数据</div>
        </div>
      </div>
    </div><!-- 统计分析 Tab 结束 -->

    <!-- 详情弹窗 -->
    <div class="modal-overlay" :class="{ show: showDetailModal && detailRecord }">
      <div v-if="showDetailModal && detailRecord" class="modal detail-modal">
        <div class="modal-header">
          <h3>值班记录详情</h3>
          <button class="close-btn" @click="showDetailModal = false">&times;</button>
        </div>
        <div class="modal-body">
          <div class="detail-grid">
            <div class="detail-section">
              <h4>基本信息</h4>
              <div class="detail-row"><label>值班日期</label><span>{{ detailRecord.duty_date }}</span></div>
              <div class="detail-row"><label>值班人</label><span>{{ detailRecord.duty_person }}</span></div>
              <div class="detail-row"><label>项目</label><span>{{ detailRecord.project_name }}</span></div>
              <div class="detail-row"><label>反馈类型</label><span>{{ getFeedbackLabel(detailRecord.feedback_type) }}</span></div>
              <div class="detail-row full"><label>{{ t('dutyRecords.form.taskDesc') }}</label><span>{{ detailRecord.task_desc || t('common.none') }}</span></div>
              <div class="detail-row full"><label>{{ t('dutyRecords.form.problemDesc') }}</label><span>{{ detailRecord.problem_desc || t('common.none') }}</span></div>
            </div>
            <div class="detail-section">
              <h4>处理信息</h4>
              <div class="detail-row"><label>{{ t('dutyRecords.form.handler') }}</label><span>{{ detailRecord.handler || t('common.none') }}</span></div>
              <div class="detail-row"><label>处理结果</label><select class="status-select" v-model="detailPendingStatus" :style="{ backgroundColor: '#fff', color: getStatusColor(detailPendingStatus), border: '1px solid ' + getStatusColor(detailPendingStatus), borderRadius: '4px', padding: '4px 8px', cursor: 'pointer', fontWeight: 600 }"><option v-for="opt in statusOptions" :key="opt.value" :value="opt.value" :style="{ color: opt.color }">{{ opt.label }}</option></select></div>
              <div class="detail-row"><label>{{ t('dutyRecords.form.plannedFixTime') }}</label><span>{{ detailRecord.planned_fix_time || t('common.none') }}</span></div>
              <div class="detail-row full"><label>{{ t('dutyRecords.columns.status') }}</label><span>{{ detailRecord.handle_result || t('common.none') }}</span></div>
              <div class="detail-row full"><label>{{ t('dutyRecords.columns.solution') }}</label><span>{{ detailRecord.solution || t('common.none') }}</span></div>
              <div class="detail-row" v-if="detailRecord.is_overdue"><label>逾期</label><span class="overdue-text">是 - {{ detailRecord.overdue_reason }}</span></div>
            </div>
            <div class="detail-section">
              <h4>通话信息</h4>
              <div class="detail-row"><label>{{ t('dutyRecords.form.firstCallTime') }}</label><span>{{ detailRecord.first_call_time || t('common.none') }}</span></div>
              <div class="detail-row"><label>{{ t('dutyRecords.form.answerTime') }}</label><span>{{ detailRecord.answer_time || t('common.none') }}</span></div>
              <div class="detail-row"><label>拨打次数</label><span>{{ detailRecord.call_count }} 次</span></div>
              <div class="detail-row"><label>是否接听</label><span>{{ detailRecord.is_answered || '无' }}</span></div>
              <div class="detail-row"><label>响应时间</label><span>{{ detailRecord.response_time }} 分钟</span></div>
            </div>
            <div class="detail-section">
              <h4>升级与交接</h4>
              <div class="detail-row"><label>是否升级</label><span>{{ detailRecord.is_escalated ? '是' : '否' }}</span></div>
              <div class="detail-row" v-if="detailRecord.is_escalated"><label>升级给</label><span>{{ getEscalateLabel(detailRecord.escalate_to) }}</span></div>
              <div class="detail-row"><label>是否交接</label><span>{{ detailRecord.has_handover ? '是' : '否' }}</span></div>
              <div class="detail-row" v-if="detailRecord.has_handover"><label>交接人</label><span>{{ detailRecord.handover_person }}</span></div>
              <div class="detail-row full" v-if="detailRecord.has_handover"><label>交接内容</label><span>{{ detailRecord.handover_content }}</span></div>
            </div>
            <div class="detail-section full-width" v-if="detailRecord.attachments?.length">
              <h4>附件 ({{ detailRecord.attachments.length }})</h4>
              <div class="detail-attachments">
                <div v-for="(img, idx) in detailRecord.attachments" :key="idx" class="detail-thumb-wrapper">
                  <img :src="getDisplayUrl(img)" @click="openImagePreview(detailRecord.attachments, idx)" class="detail-thumb">
                  <button class="share-btn" @click.stop="openShareModal(img)" title="生成分享链接">
                    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M4 12v8a2 2 0 002 2h12a2 2 0 002-2v-8"/><polyline points="16,6 12,2 8,6"/><line x1="12" y1="2" x2="12" y2="15"/></svg>
                  </button>
                </div>
              </div>
            </div>
          </div>
        </div>
        <div class="modal-footer">
          <button class="btn btn-default" @click="showDetailModal = false">关闭</button>
          <button v-if="canEditPlannedFixTime" class="btn btn-warning" @click="showDetailModal = false; openPlannedFixModal(detailRecord)">编辑计划修复时间</button>
          <button v-if="canUpdate" class="btn btn-primary" @click="showDetailModal = false; openModal('edit', detailRecord)">编辑</button>
          <button v-if="detailPendingStatus !== detailRecord.status" class="btn btn-success" @click="quickUpdateStatus(detailRecord, detailPendingStatus)">保存</button>
        </div>
      </div>
    </div>

    <!-- 单独编辑计划修复时间 -->
    <div class="modal-overlay" :class="{ show: showPlannedFixModal }">
      <div v-if="showPlannedFixModal" class="modal small-modal">
        <div class="modal-header">
          <h3>编辑计划修复时间</h3>
          <button class="close-btn" @click="showPlannedFixModal = false">&times;</button>
        </div>
        <div class="modal-body">
          <div class="form-group">
            <label>值班人</label>
            <input type="text" :value="plannedFixForm.duty_person" disabled>
          </div>
          <div class="form-group">
            <label>项目</label>
            <input type="text" :value="plannedFixForm.project_name || t('common.none')" disabled>
          </div>
          <div class="form-group">
            <label>计划修复时间</label>
            <input type="datetime-local" v-model="plannedFixForm.planned_fix_time">
            <span class="helper-text">留空表示清空计划修复时间</span>
          </div>
        </div>
        <div class="modal-footer">
          <button class="btn btn-default" @click="showPlannedFixModal = false">取消</button>
          <button class="btn btn-primary" @click="savePlannedFixTime">保存</button>
        </div>
      </div>
    </div>

    <!-- 添加/编辑弹窗 -->
    <div class="modal-overlay" :class="{ show: showModal }">
      <div v-if="showModal" class="modal duty-modal">
        <div class="modal-header">
          <h3>{{ modalMode === 'add' ? t('dutyRecords.form.addTitle') : t('dutyRecords.form.editTitle') }}</h3>
          <button class="close-btn" @click="showModal = false">&times;</button>
        </div>
        <div class="modal-body">
          <div class="form-sections">
            <!-- 基本信息 -->
            <div class="form-section">
              <h4>{{ t('dutyRecords.form.basicInfo') }}</h4>
              <div class="form-grid">
                <div class="form-group">
                  <label>{{ t('dutyRecords.form.dutyTime') }} <span class="required">*</span></label>
                  <input type="datetime-local" v-model="form.duty_date" required>
                </div>
                <div class="form-group">
                  <label>{{ t('dutyRecords.form.dutyPerson') }} <span class="required">*</span></label>
                  <input type="text" v-model="form.duty_person" required :placeholder="t('dutyRecords.form.dutyPersonPlaceholder')">
                </div>
                <div class="form-group">
                  <label>{{ t('dutyRecords.form.project') }} <span class="required">*</span></label>
                  <select v-model="form.project_id" required>
                    <option value="">{{ t('dutyRecords.form.selectProject') }}</option>
                    <option v-for="p in projects" :key="p.id" :value="p.id">{{ p.name }}</option>
                  </select>
                </div>
                <div class="form-group">
                  <label>{{ t('dutyRecords.form.feedbackType') }}</label>
                  <select v-model="form.feedback_type">
                    <option v-for="tp in feedbackTypeOptions" :key="tp.value" :value="tp.value">{{ tp.label }}</option>
                  </select>
                </div>
                <div class="form-group">
                  <label>{{ t('dutyRecords.form.eventType') }}</label>
                  <select v-model="form.event_type">
                    <option v-for="e in eventTypeOptions" :key="e.value" :value="e.value">{{ e.label }}</option>
                  </select>
                </div>
              </div>
              <div class="form-group full-width">
                <label>{{ t('dutyRecords.form.taskDesc') }}</label>
                <textarea v-model="form.task_desc" rows="2" :placeholder="t('dutyRecords.form.taskDescPlaceholder')"></textarea>
              </div>
              <div class="form-group full-width">
                <label>{{ t('dutyRecords.form.problemDesc') }}</label>
                <textarea v-model="form.problem_desc" rows="3" :placeholder="t('dutyRecords.form.problemDescPlaceholder')"></textarea>
              </div>
            </div>

            <!-- 处理信息 -->
            <div class="form-section">
              <h4>{{ t('dutyRecords.form.handleInfo') }}</h4>
              <div class="form-grid">
                <div class="form-group">
                  <label>{{ t('dutyRecords.form.handler') }}</label>
                  <input type="text" v-model="form.handler" :placeholder="t('dutyRecords.form.handlerPlaceholder')">
                </div>
                <div class="form-group">
                  <label>{{ t('dutyRecords.form.status') }} <span class="required">*</span></label>
                  <select v-model="form.status" required>
                    <option value="" disabled>{{ t('dutyRecords.form.selectStatus') }}</option>
                    <option v-for="s in statusOptions" :key="s.value" :value="s.value">{{ s.label }}</option>
                  </select>
                </div>
                <div class="form-group">
                  <label>{{ t('dutyRecords.form.plannedFixTime') }}</label>
                  <input type="datetime-local" v-model="form.planned_fix_time" :disabled="!canEditPlannedFixTimeInForm" :class="{ 'disabled-field': !canEditPlannedFixTimeInForm }">
                  <span v-if="!canEditPlannedFixTimeInForm" class="helper-text warning">{{ t('dutyRecords.form.plannedFixTimeEdited') }}</span>
                  <span v-else-if="modalMode === 'edit' && !form.planned_fix_time_edited" class="helper-text info">{{ t('dutyRecords.form.plannedFixTimeFirstEdit') }}</span>
                </div>
                <div class="form-group full-width">
                  <label>{{ t('dutyRecords.form.solution') }}</label>
                  <textarea v-model="form.solution" :placeholder="t('dutyRecords.form.solutionPlaceholder')" rows="2"></textarea>
                </div>
              </div>
              <div class="form-grid">
                <div class="form-group checkbox">
                  <label><input type="checkbox" v-model="form.is_overdue"><span>{{ t('dutyRecords.form.markOverdue') }}</span></label>
                </div>
                <div class="form-group" v-if="form.is_overdue">
                  <label>{{ t('dutyRecords.form.overdueReason') }}</label>
                  <input type="text" v-model="form.overdue_reason" :placeholder="t('dutyRecords.form.overdueReasonPlaceholder')">
                </div>
              </div>
            </div>

            <!-- 通话信息 -->
            <div class="form-section">
              <h4>{{ t('dutyRecords.form.callInfo') }}</h4>
              <div class="form-grid">
                <div class="form-group">
                  <label>{{ t('dutyRecords.form.firstCallTime') }}</label>
                  <input type="datetime-local" v-model="form.first_call_time">
                </div>
                <div class="form-group">
                  <label>{{ t('dutyRecords.form.answerTime') }}</label>
                  <input type="datetime-local" v-model="form.answer_time" @change="onAnswerTimeChange">
                </div>
                <div class="form-group">
                  <label>{{ t('dutyRecords.form.callCount') }}</label>
                  <input type="number" v-model.number="form.call_count" min="0">
                </div>
                <div class="form-group">
                  <label>{{ t('dutyRecords.form.answered') }}</label>
                  <select v-model="form.is_answered" class="form-select">
                    <option value="无">无</option>
                    <option value="已接听">已接听</option>
                    <option value="未接听">未接听</option>
                  </select>
                </div>
                <div class="form-group">
                  <label>{{ t('dutyRecords.form.responseTime') }}</label>
                  <div class="response-time-display">{{ calculatedResponseTime }} {{ t('dutyRecords.form.minutes') }}</div>
                  <span class="helper-text">{{ t('dutyRecords.form.autoCalculated') }}</span>
                </div>
              </div>
            </div>

            <!-- 升级与交接 -->
            <div class="form-section">
              <h4>{{ t('dutyRecords.form.escalateAndHandover') }}</h4>
              <div class="form-grid">
                <div class="form-group checkbox">
                  <label><input type="checkbox" v-model="form.is_escalated"><span>{{ t('dutyRecords.form.escalate') }}</span></label>
                </div>
                <div class="form-group escalate-options" v-if="form.is_escalated">
                  <label>{{ t('dutyRecords.form.escalateTo') }}</label>
                  <div class="checkbox-group">
                    <label v-for="e in escalateOptions" :key="e.value" class="checkbox-item">
                      <input type="checkbox" :value="e.value" v-model="form.escalate_to">
                      <span>{{ e.label }}</span>
                    </label>
                  </div>
                </div>
                <div class="form-group checkbox">
                  <label><input type="checkbox" v-model="form.has_handover"><span>{{ t('dutyRecords.form.hasHandover') }}</span></label>
                </div>
                <div class="form-group" v-if="form.has_handover">
                  <label>{{ t('dutyRecords.form.handoverPerson') }}</label>
                  <input type="text" v-model="form.handover_person" :placeholder="t('dutyRecords.form.handoverPersonPlaceholder')">
                </div>
              </div>
              <div class="form-group full-width" v-if="form.has_handover">
                <label>{{ t('dutyRecords.form.handoverContent') }}</label>
                <textarea v-model="form.handover_content" rows="2" :placeholder="t('dutyRecords.form.handoverContentPlaceholder')"></textarea>
              </div>
            </div>

            <!-- 附件 -->
            <div class="form-section">
              <h4>{{ t('dutyRecords.form.attachments') }} ({{ form.attachments?.length || 0 }})</h4>
              <div class="attachments-area">
                <div class="attachment-list" v-if="form.attachments?.length">
                  <div v-for="(img, idx) in form.attachments" :key="idx" class="attachment-item">
                    <img :src="getDisplayUrl(img)" @click="openImagePreview(form.attachments, idx)">
                    <button class="remove-btn" @click="removeAttachment(idx)" type="button">&times;</button>
                  </div>
                </div>
                <div v-if="canUpload" class="upload-zone" 
                     @paste="handlePaste" 
                     @dragover.prevent="dragOver = true" 
                     @dragleave="dragOver = false" 
                     @drop.prevent="handleDrop"
                     :class="{ 'drag-over': dragOver }"
                     tabindex="0">
                  <input type="file" multiple accept="image/*" @change="handleFileUpload" :disabled="uploading" id="file-upload" ref="fileInput">
                  <div class="upload-content">
                    <svg class="upload-icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M21 15v4a2 2 0 01-2 2H5a2 2 0 01-2-2v-4M17 8l-5-5-5 5M12 3v12"/></svg>
                    <p v-if="uploading" class="upload-text">{{ t('dutyRecords.form.uploadingFile') }}</p>
                    <p v-else class="upload-text">
                      <span class="primary-action" @click="$refs.fileInput.click()">{{ t('dutyRecords.form.clickToSelect') }}</span>{{ t('dutyRecords.form.dragOrPaste') }}
                    </p>
                    <p class="upload-hint">{{ t('dutyRecords.form.multipleImages') }}</p>
                  </div>
                </div>
              </div>
            </div>
          </div>
        </div>
        <div class="modal-footer">
          <button class="btn btn-default" @click="showModal = false">{{ t('common.cancel') }}</button>
          <button class="btn btn-primary" @click="saveRecord">{{ t('common.save') }}</button>
        </div>
      </div>
    </div>

    <!-- 项目配置弹窗 -->
    <div class="modal-overlay" :class="{ show: showProjectModal }">
      <div v-if="showProjectModal" class="modal project-modal">
        <div class="modal-header">
          <h3>{{ projectModalMode === 'list' ? '项目配置' : (projectModalMode === 'add' ? '添加项目' : '编辑项目') }}</h3>
          <button class="close-btn" @click="showProjectModal = false">&times;</button>
        </div>
        <div class="modal-body">
          <!-- 项目列表 -->
          <div v-if="projectModalMode === 'list'" class="project-list-container">
            <div class="project-list-header">
              <span>共 {{ allProjects.length }} 个项目</span>
              <button v-if="canProjectCreate" class="btn btn-sm btn-primary" @click="openProjectModal('add')">+ 添加项目</button>
            </div>
            <div class="project-grid">
              <div v-for="p in allProjects" :key="p.id" class="project-card" :class="{ disabled: p.status === 'disabled' }">
                <div class="project-info">
                  <div class="project-title">
                    <span class="name">{{ p.name }}</span>
                    <span class="code">{{ p.code }}</span>
                  </div>
                  <p class="desc">{{ p.description || '暂无描述' }}</p>
                  <span class="status-tag" :class="p.status">{{ p.status === 'active' ? '启用' : '禁用' }}</span>
                </div>
                <div class="project-actions">
                  <button v-if="canProjectUpdate" class="btn-icon" @click="openProjectModal('edit', p)" title="编辑">
                    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M11 4H4a2 2 0 00-2 2v14a2 2 0 002 2h14a2 2 0 002-2v-7M18.5 2.5a2.121 2.121 0 013 3L12 15l-4 1 1-4 9.5-9.5z"/></svg>
                  </button>
                  <button v-if="canProjectDelete" class="btn-icon danger" @click="deleteProject(p)" title="删除">
                    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M3 6h18M19 6v14a2 2 0 01-2 2H7a2 2 0 01-2-2V6m3 0V4a2 2 0 012-2h4a2 2 0 012 2v2"/></svg>
                  </button>
                </div>
              </div>
            </div>
          </div>
          <!-- 添加/编辑项目表单 -->
          <div v-else class="project-form">
            <button class="back-btn" @click="projectModalMode = 'list'">&larr; 返回列表</button>
            <div class="form-group">
              <label>项目名称 <span class="required">*</span></label>
              <input type="text" v-model="projectForm.name" placeholder="如: 游戏平台">
            </div>
            <div class="form-group">
              <label>项目代码 <span class="required">*</span></label>
              <input type="text" v-model="projectForm.code" placeholder="如: GAME">
            </div>
            <div class="form-group">
              <label>描述</label>
              <textarea v-model="projectForm.description" rows="3" placeholder="项目描述..."></textarea>
            </div>
            <div class="form-row">
              <div class="form-group">
                <label>状态</label>
                <select v-model="projectForm.status">
                  <option value="active">启用</option>
                  <option value="disabled">禁用</option>
                </select>
              </div>
              <div class="form-group">
                <label>排序</label>
                <input type="number" v-model.number="projectForm.sort_order" min="0">
              </div>
            </div>
            <div class="form-actions">
              <button class="btn btn-default" @click="projectModalMode = 'list'">取消</button>
              <button class="btn btn-primary" @click="saveProject">保存</button>
            </div>
          </div>
        </div>
      </div>
    </div>

    <!-- 图片预览 -->
    <div v-if="showImagePreview" class="image-preview-overlay" @click.self="showImagePreview = false">
      <div class="image-preview-container">
        <button class="preview-close" @click="showImagePreview = false">&times;</button>
        <button class="preview-nav prev" @click="previewIndex = (previewIndex - 1 + previewImages.length) % previewImages.length" v-if="previewImages.length > 1">&lt;</button>
        <img :src="previewImages[previewIndex]" class="preview-image">
        <button class="preview-nav next" @click="previewIndex = (previewIndex + 1) % previewImages.length" v-if="previewImages.length > 1">&gt;</button>
        <div class="preview-counter">{{ previewIndex + 1 }} / {{ previewImages.length }}</div>
      </div>
    </div>

  </div>
  
  <!-- 批量修改处理结果模态框 - 使用 Teleport 传送到 body -->
  <Teleport to="body">
    <div v-if="showBatchStatusModal" class="batch-modal-overlay" @click.self="showBatchStatusModal = false">
      <div class="batch-modal-content">
        <div class="batch-modal-header">
          <h3>批量修改处理结果</h3>
          <button class="batch-close-btn" @click="showBatchStatusModal = false">&times;</button>
        </div>
        <div class="batch-modal-body">
          <p class="batch-hint">将修改 <strong>{{ selectedRecords.length }}</strong> 条记录的处理结果</p>
          <div class="batch-form-group">
            <label>选择处理结果 <span class="required">*</span></label>
            <select v-model="batchStatus" class="batch-select">
              <option value="" disabled>请选择处理结果</option>
              <option v-for="s in statusOptions" :key="s.value" :value="s.value">{{ s.label }}</option>
            </select>
          </div>
        </div>
        <div class="batch-modal-footer">
          <button class="batch-btn-cancel" @click="showBatchStatusModal = false">取消</button>
          <button class="batch-btn-confirm" @click="submitBatchStatus" :disabled="!batchStatus">确认修改</button>
        </div>
      </div>
    </div>
  </Teleport>

  <!-- 分享链接弹窗 -->
  <Teleport to="body">
    <div v-if="showShareModal" class="batch-modal-overlay" @click.self="showShareModal = false">
      <div class="batch-modal-content">
        <div class="batch-modal-header">
          <h3>生成分享链接</h3>
          <button class="batch-close-btn" @click="showShareModal = false">&times;</button>
        </div>
        <div class="batch-modal-body">
          <div v-if="!shareResult">
            <p class="share-hint">为 <strong>{{ shareForm.fileName }}</strong> 生成分享链接</p>
            <div class="batch-form-group">
              <label>有效期</label>
              <select v-model="shareForm.expiresIn" class="batch-select">
                <option value="1d">1天</option>
                <option value="7d">7天</option>
                <option value="30d">30天</option>
                <option value="permanent">永久有效</option>
              </select>
            </div>
          </div>
          <div v-else class="share-result">
            <p class="share-success-hint">分享链接已生成</p>
            <div class="share-url-box">
              <input type="text" :value="shareResult.shareUrl" readonly class="share-url-input">
              <button class="copy-btn" @click="copyShareUrl">复制</button>
            </div>
            <p v-if="shareResult.expiresAt" class="share-expires">
              有效期至：{{ shareResult.expiresAt }}
            </p>
            <p v-else class="share-expires permanent">永久有效</p>
          </div>
        </div>
        <div class="batch-modal-footer">
          <button class="batch-btn-cancel" @click="showShareModal = false">关闭</button>
          <button v-if="!shareResult" class="batch-btn-confirm" @click="createShare">生成链接</button>
        </div>
      </div>
    </div>
  </Teleport>

  <!-- 粘贴记录弹窗 -->
  <Teleport to="body">
    <div v-if="showPasteModal" class="paste-modal-overlay" @click.self="showPasteModal = false">
      <div class="paste-modal">
        <div class="paste-modal-header">
          <h3>粘贴记录</h3>
          <button class="paste-close-btn" @click="showPasteModal = false">&times;</button>
        </div>
        <div class="paste-modal-body">
          <div class="paste-info">
            <p class="paste-hint">将复制的记录粘贴多条，日期将更新为当前时间</p>
            <div class="paste-preview" v-if="copiedRecord">
              <div class="preview-item">
                <span class="preview-label">值班人员：</span>
                <span class="preview-value">{{ copiedRecord.duty_person || '-' }}</span>
              </div>
              <div class="preview-item">
                <span class="preview-label">项目：</span>
                <span class="preview-value">{{ projects.find(p => p.id == copiedRecord.project_id)?.name || '-' }}</span>
              </div>
              <div class="preview-item">
                <span class="preview-label">问题描述：</span>
                <span class="preview-value">{{ copiedRecord.problem_desc?.substring(0, 50) || '-' }}{{ (copiedRecord.problem_desc?.length > 50) ? '...' : '' }}</span>
              </div>
            </div>
          </div>
          <div class="paste-count-group">
            <label>粘贴数量：</label>
            <div class="paste-count-input">
              <button class="count-btn" @click="pasteCount = Math.max(1, pasteCount - 1)">-</button>
              <input type="number" v-model.number="pasteCount" min="1" max="50" class="count-input">
              <button class="count-btn" @click="pasteCount = Math.min(50, pasteCount + 1)">+</button>
            </div>
            <span class="count-hint">最多50条</span>
          </div>
          <div class="paste-quick-btns">
            <button v-for="n in [1, 5, 10, 20]" :key="n" class="quick-btn" :class="{ active: pasteCount === n }" @click="pasteCount = n">{{ n }}</button>
          </div>
        </div>
        <div class="paste-modal-footer">
          <button class="btn btn-secondary" @click="showPasteModal = false">取消</button>
          <button class="btn btn-primary" @click="pasteRecords">粘贴 {{ pasteCount }} 条</button>
        </div>
      </div>
    </div>
  </Teleport>
</template>

<style scoped>
.duty-records-page { padding: 20px; max-width: 100%; position: relative; }

/* Tab 导航 */
.tab-nav { display: flex; gap: 4px; margin-bottom: 16px; background: var(--bg-card, white); padding: 4px; border-radius: 10px; border: 1px solid var(--border-color, #e2e8f0); width: fit-content; }
.tab-btn { display: inline-flex; align-items: center; gap: 6px; padding: 8px 16px; border: none; background: transparent; color: var(--text-secondary, #64748b); font-size: 0.875rem; font-weight: 500; cursor: pointer; border-radius: 8px; transition: all 0.2s; }
.tab-btn .icon { width: 16px; height: 16px; }
.tab-btn:hover { color: var(--text-primary, #1e293b); background: var(--bg-hover, #f1f5f9); }
.tab-btn.active { background: linear-gradient(135deg, #3b82f6, #2563eb); color: white; box-shadow: 0 2px 8px rgba(59, 130, 246, 0.3); }
.tab-content { animation: fadeIn 0.2s ease; }
@keyframes fadeIn { from { opacity: 0; transform: translateY(-4px); } to { opacity: 1; transform: translateY(0); } }

/* 统计分析 Tab */
.stats-tab { background: transparent; }

/* 页面头部 - 时间范围选择器 */
.stats-page-header { display: flex; justify-content: space-between; align-items: center; margin-bottom: 16px; flex-wrap: wrap; gap: 12px; }
.time-range-selector { display: flex; gap: 4px; background: var(--bg-hover); padding: 4px; border-radius: 6px; }
.time-range-btn { padding: 6px 12px; border: none; background: transparent; border-radius: 4px; font-size: 0.75rem; cursor: pointer; color: var(--text-muted); transition: all 0.15s; }
.time-range-btn.active { background: var(--bg-card); color: var(--text-primary); box-shadow: 0 1px 2px rgba(0,0,0,0.15); }
.time-range-btn:hover:not(.active) { color: var(--text-primary); }

.stats-filters { background: var(--bg-card); border-radius: 10px; padding: 16px; margin-bottom: 20px; border: 1px solid var(--border-color); box-shadow: 0 1px 3px rgba(0,0,0,0.1); }
.stats-filters .filter-row { display: flex; flex-wrap: wrap; gap: 12px; align-items: flex-end; }
.stats-filters .filter-group { display: flex; flex-direction: column; gap: 4px; }
.stats-filters .filter-group label { font-size: 0.75rem; color: var(--text-muted); font-weight: 500; }
.stats-filters .filter-group select, .stats-filters .filter-group input { padding: 6px 10px; border: 1px solid var(--border-color); border-radius: 5px; font-size: 0.8125rem; min-width: 120px; background: var(--bg-input); color: var(--text-primary); }
.stats-filters .filter-group select:focus, .stats-filters .filter-group input:focus { outline: none; border-color: #3b82f6; }

.stats-overview-cards { display: grid; grid-template-columns: repeat(auto-fit, minmax(140px, 1fr)); gap: 12px; margin-bottom: 20px; }
.overview-card { background: var(--bg-card); border-radius: 10px; padding: 16px; text-align: center; border: 1px solid var(--border-color); transition: all 0.2s; box-shadow: 0 1px 3px rgba(0,0,0,0.1); }
.overview-card:hover { transform: translateY(-2px); box-shadow: 0 4px 12px rgba(0,0,0,0.15); }
.overview-card .card-value { font-size: 2rem; font-weight: 700; color: var(--text-primary); }
.overview-card .card-label { font-size: 0.75rem; color: var(--text-muted); margin-top: 4px; }
.overview-card.total { border-left: 4px solid #3b82f6; } .overview-card.total .card-value { color: #3b82f6; }
.overview-card.normal { border-left: 4px solid #10b981; } .overview-card.normal .card-value { color: #10b981; }
.overview-card.resolved { border-left: 4px solid #22c55e; } .overview-card.resolved .card-value { color: #22c55e; }
.overview-card.pending { border-left: 4px solid #f59e0b; } .overview-card.pending .card-value { color: #f59e0b; }
.overview-card.in-progress { border-left: 4px solid #8b5cf6; } .overview-card.in-progress .card-value { color: #8b5cf6; }
.overview-card.temporary { border-left: 4px solid #3b82f6; } .overview-card.temporary .card-value { color: #3b82f6; }
.overview-card.overdue { border-left: 4px solid #ef4444; } .overview-card.overdue .card-value { color: #ef4444; }

.stats-loading { display: flex; flex-direction: column; align-items: center; justify-content: center; padding: 60px; color: var(--text-muted); gap: 12px; }
.loading-spinner { width: 32px; height: 32px; border: 3px solid var(--border-color); border-top-color: #3b82f6; border-radius: 50%; animation: spin 0.8s linear infinite; }
@keyframes spin { to { transform: rotate(360deg); } }

.stats-charts-grid { display: grid; grid-template-columns: repeat(2, 1fr); gap: 16px; margin-bottom: 20px; }
@media (max-width: 1200px) { .stats-charts-grid { grid-template-columns: 1fr; } }
.chart-card { background: var(--bg-card); border-radius: 10px; border: 1px solid var(--border-color); overflow: hidden; box-shadow: 0 1px 3px rgba(0,0,0,0.1); }
.chart-card.full-width { grid-column: 1 / -1; }
.chart-header { display: flex; justify-content: space-between; align-items: center; padding: 12px 16px; border-bottom: 1px solid var(--border-color); }
.chart-title { font-size: 0.9375rem; font-weight: 600; color: var(--text-primary); }

/* 图表类型选择器 */
.chart-type-selector { display: flex; gap: 4px; }
.chart-type-btn { width: 28px; height: 28px; border: 1px solid var(--border-color); background: var(--bg-card); border-radius: 5px; cursor: pointer; display: flex; align-items: center; justify-content: center; transition: all 0.15s; padding: 0; }
.chart-type-btn:hover { background: var(--bg-hover); border-color: var(--text-muted); }
.chart-type-btn.active { background: #3b82f6; border-color: #3b82f6; }
.chart-type-btn.active svg { stroke: white; }
.chart-type-btn svg { width: 14px; height: 14px; stroke: var(--text-muted); }
.chart-type-divider { color: var(--border-color); font-size: 14px; margin: 0 2px; }
.display-mode-btn { font-size: 12px; font-weight: 600; }
.display-mode-btn .mode-text { color: var(--text-muted); }
.display-mode-btn.active .mode-text { color: white; }
.chart-body { padding: 16px; }
.chart-body.chart-body-tall { padding: 16px; }
.chart-container { height: 280px; position: relative; }
.chart-container-tall { height: 320px; }
.empty-chart { color: var(--text-muted); font-size: 0.875rem; text-align: center; padding: 40px; }

/* Mini 统计 */
.mini-stats { display: flex; gap: 20px; margin-top: 12px; padding-top: 12px; border-top: 1px solid var(--border-color); }
.mini-stat { text-align: center; flex: 1; }
.mini-stat .value { font-size: 1.25rem; font-weight: 600; color: var(--text-primary); }
.mini-stat .label { font-size: 0.7rem; color: var(--text-muted); }

/* 统计表格 */
.stats-table { width: 100%; border-collapse: collapse; font-size: 0.8125rem; }
.stats-table th, .stats-table td { padding: 10px 12px; text-align: left; border-bottom: 1px solid var(--border-color); color: var(--text-primary); }
.stats-table th { background: var(--bg-hover); font-weight: 600; color: var(--text-secondary); font-size: 0.75rem; white-space: nowrap; }
.stats-table tr:hover { background: var(--bg-hover); }
.stats-table .badge { display: inline-block; padding: 2px 8px; border-radius: 10px; font-size: 0.75rem; font-weight: 500; }
.badge-green { background: rgba(5, 150, 105, 0.2); color: #10b981; }
.badge-yellow { background: rgba(217, 119, 6, 0.2); color: #f59e0b; }
.badge-purple { background: rgba(124, 58, 237, 0.2); color: #8b5cf6; }
.badge-blue { background: rgba(37, 99, 235, 0.2); color: #3b82f6; }
.badge-red { background: rgba(220, 38, 38, 0.2); color: #ef4444; }

.progress-cell { display: flex; align-items: center; gap: 8px; }
.progress-bar { height: 6px; background: #e2e8f0; border-radius: 3px; overflow: hidden; }
.progress-fill { height: 100%; background: linear-gradient(90deg, #22c55e, #16a34a); border-radius: 3px; transition: width 0.3s; }
.progress-fill.green { background: linear-gradient(90deg, #22c55e, #16a34a); }
.progress-fill.yellow { background: linear-gradient(90deg, #f59e0b, #d97706); }
.progress-fill.red { background: linear-gradient(90deg, #ef4444, #dc2626); }
.progress-fill.blue { background: linear-gradient(90deg, #3b82f6, #2563eb); }
.progress-text { font-size: 0.75rem; color: var(--text-muted); min-width: 35px; }
.progress-text.text-green { color: #22c55e; }
.progress-text.text-yellow { color: #d97706; }
.progress-text.text-red { color: #ef4444; }
.progress-text.text-blue { color: #2563eb; }

/* 统计表格卡片 */
.stats-tables-section { margin-top: 20px; }
.stats-table-card { background: var(--bg-card); border-radius: 10px; padding: 16px; box-shadow: 0 1px 3px rgba(0,0,0,0.1); border: 1px solid var(--border-color); margin-bottom: 20px; }
.table-header { display: flex; justify-content: space-between; align-items: center; margin-bottom: 12px; padding-bottom: 10px; border-bottom: 1px solid var(--border-color); }
.table-title { font-size: 0.9375rem; font-weight: 600; color: var(--text-primary); }
.empty-table { color: var(--text-muted); font-size: 0.875rem; text-align: center; padding: 40px; }

/* 简单图表 */
.simple-chart { display: flex; flex-direction: column; gap: 10px; }
.chart-bar-item { display: flex; align-items: center; gap: 10px; }
.bar-label { font-size: 0.8125rem; color: var(--text-secondary); min-width: 80px; white-space: nowrap; }
.bar-container { flex: 1; height: 20px; background: var(--bg-hover); border-radius: 4px; overflow: hidden; }
.bar-fill { height: 100%; background: linear-gradient(90deg, #6366f1, #8b5cf6); border-radius: 4px; transition: width 0.3s; }
.bar-fill.feedback { background: linear-gradient(90deg, #3b82f6, #60a5fa); }
.bar-fill.response { background: linear-gradient(90deg, #10b981, #34d399); }
.bar-value { font-size: 0.8125rem; font-weight: 600; color: var(--text-primary); min-width: 30px; text-align: right; }

.response-summary { display: flex; gap: 20px; margin-bottom: 16px; padding-bottom: 12px; border-bottom: 1px solid var(--border-color); }
.summary-item { text-align: center; }
.summary-value { font-size: 1.25rem; font-weight: 700; color: var(--text-primary); }
.summary-label { font-size: 0.7rem; color: var(--text-muted); }

/* 处理结果统计（饼图样式） */
.status-chart { display: flex; gap: 24px; align-items: center; }
.status-pie-container { width: 120px; height: 120px; }
.status-legend { flex: 1; }
.legend-item { display: flex; align-items: center; gap: 8px; padding: 6px 0; border-bottom: 1px solid var(--border-color); }
.legend-color { width: 12px; height: 12px; border-radius: 3px; }
.legend-color.resolved { background: #22c55e; }
.legend-color.pending { background: #f59e0b; }
.legend-color.in_progress, .legend-color.in-progress { background: #8b5cf6; }
.legend-color.temporary { background: #3b82f6; }
.legend-color.normal { background: #10b981; }
.legend-label { flex: 1; font-size: 0.8125rem; color: var(--text-secondary); }
.legend-value { font-size: 0.875rem; font-weight: 600; color: var(--text-primary); }
.legend-percent { font-size: 0.75rem; color: var(--text-muted); min-width: 40px; text-align: right; }

/* 上报问题统计 */
.escalate-stats { }
.escalate-summary { display: flex; gap: 16px; margin-bottom: 16px; padding-bottom: 12px; border-bottom: 1px solid var(--border-color); }
.escalate-total { text-align: center; }
.escalate-total .value { font-size: 2rem; font-weight: 700; color: #ef4444; }
.escalate-total .label { font-size: 0.75rem; color: var(--text-muted); }
.bar-fill.escalate { background: linear-gradient(90deg, #ef4444, #f87171); }

/* 每日趋势 */
.trend-chart { }
.trend-bars { display: flex; gap: 4px; align-items: flex-end; height: 180px; padding: 10px 0; overflow-x: auto; }
.trend-bar-group { display: flex; flex-direction: column; align-items: center; min-width: 24px; }
.trend-bar-container { display: flex; flex-direction: column; align-items: center; height: 150px; position: relative; }
.trend-bar { width: 18px; border-radius: 3px 3px 0 0; position: absolute; bottom: 0; transition: height 0.3s; cursor: pointer; }
.trend-bar.total { background: linear-gradient(180deg, #3b82f6, #60a5fa); z-index: 1; }
.trend-bar.overdue { background: linear-gradient(180deg, #ef4444, #f87171); z-index: 2; }
.trend-bar:hover .trend-tooltip { display: block; }
.trend-tooltip { display: none; position: absolute; bottom: 100%; left: 50%; transform: translateX(-50%); background: #1e293b; color: white; padding: 6px 10px; border-radius: 6px; font-size: 0.75rem; white-space: nowrap; z-index: 100; }
.trend-date { font-size: 0.65rem; color: var(--text-muted); margin-top: 4px; white-space: nowrap; }
.trend-legend { display: flex; gap: 16px; justify-content: center; margin-top: 12px; padding-top: 12px; border-top: 1px solid var(--border-color); }
.trend-legend .legend-item { display: flex; align-items: center; gap: 6px; font-size: 0.75rem; color: var(--text-muted); }
.trend-legend .dot { width: 10px; height: 10px; border-radius: 2px; }
.trend-legend .dot.total { background: #3b82f6; }
.trend-legend .dot.overdue { background: #ef4444; }

@media (max-width: 1024px) {
  .stats-charts-grid { grid-template-columns: 1fr; }
  .chart-card.full-width { grid-column: 1; }
  .status-chart { flex-direction: column; }
}

.page-header { display: flex; justify-content: space-between; align-items: center; margin-bottom: 16px; flex-wrap: wrap; gap: 12px; position: relative; z-index: 10; }
.header-left { display: flex; align-items: center; gap: 12px; }
.header-left h2 { margin: 0; font-size: 1.375rem; font-weight: 600; color: var(--text-primary, #1e293b); }
.record-count { font-size: 0.8125rem; color: var(--text-secondary, #64748b); background: var(--bg-input, #f1f5f9); padding: 4px 10px; border-radius: 10px; }
.header-actions { display: flex; gap: 8px; flex-wrap: wrap; position: relative; z-index: 20; }

.btn { display: inline-flex; align-items: center; gap: 5px; padding: 7px 14px; border-radius: 6px; font-size: 0.8125rem; font-weight: 500; border: none; cursor: pointer; transition: all 0.15s; position: relative; z-index: 30; user-select: none; }
.btn .icon { width: 15px; height: 15px; pointer-events: none; }
.btn-primary { background: linear-gradient(135deg, #3b82f6, #2563eb); color: white; }
.btn-primary:hover { box-shadow: 0 3px 10px rgba(59, 130, 246, 0.35); }
.btn-secondary { background: var(--bg-input, #f1f5f9); color: var(--text-secondary, #475569); }
.btn-secondary:hover { background: var(--bg-hover, #e2e8f0); }
.btn-default { background: var(--bg-card, white); color: var(--text-secondary, #475569); border: 1px solid var(--border-color, #e2e8f0); }
.btn-default:hover { border-color: #cbd5e1; background: var(--bg-hover, #f8fafc); }
.btn-warning { background: #f59e0b; color: #fff; }
.btn-warning:hover { background: #d97706; }
.btn-sm { padding: 5px 10px; font-size: 0.75rem; }

/* 统计面板 */
.stats-panel { background: var(--bg-card); border-radius: 10px; padding: 16px; margin-bottom: 16px; border: 1px solid var(--border-color); }
.stats-cards { display: grid; grid-template-columns: repeat(auto-fit, minmax(100px, 1fr)); gap: 12px; margin-bottom: 16px; }
.stat-card { background: var(--bg-hover); border-radius: 8px; padding: 12px; text-align: center; }
.stat-card .stat-value { font-size: 1.5rem; font-weight: 700; color: var(--text-primary); }
.stat-card .stat-label { font-size: 0.7rem; color: var(--text-muted); margin-top: 2px; }
.stat-card.normal { background: rgba(16, 185, 129, 0.15); } .stat-card.normal .stat-value { color: #10b981; }
.stat-card.resolved { background: rgba(5, 150, 105, 0.15); } .stat-card.resolved .stat-value { color: #059669; }
.stat-card.pending { background: rgba(245, 158, 11, 0.15); } .stat-card.pending .stat-value { color: #f59e0b; }
.stat-card.in-progress { background: rgba(139, 92, 246, 0.15); } .stat-card.in-progress .stat-value { color: #8b5cf6; }
.stat-card.temporary { background: rgba(59, 130, 246, 0.15); } .stat-card.temporary .stat-value { color: #3b82f6; }
.stat-card.overdue { background: rgba(239, 68, 68, 0.15); border: 1px solid rgba(239, 68, 68, 0.3); } .stat-card.overdue .stat-value { color: #ef4444; }
.stat-card.month { background: rgba(5, 150, 105, 0.15); } .stat-card.month .stat-value { color: #059669; }

.stats-details { display: grid; grid-template-columns: repeat(auto-fit, minmax(280px, 1fr)); gap: 16px; }
.stats-section h4 { margin: 0 0 10px; font-size: 0.8125rem; color: var(--text-secondary); font-weight: 600; }
.handler-list, .project-list { display: flex; flex-direction: column; gap: 6px; max-height: 150px; overflow-y: auto; }
.handler-item, .project-item { display: flex; justify-content: space-between; align-items: center; padding: 6px 10px; background: var(--bg-hover); border-radius: 5px; font-size: 0.8125rem; }
.handler-name, .project-name { font-weight: 500; color: var(--text-primary); }
.handler-stats, .project-stats { display: flex; gap: 4px; font-size: 0.7rem; }
.badge { padding: 1px 6px; border-radius: 8px; font-weight: 500; }
.badge.resolved { background: #d1fae5; color: #059669; }
.badge.pending { background: #fef3c7; color: #d97706; }
.badge.in-progress { background: #ede9fe; color: #7c3aed; }
.badge.temporary { background: #dbeafe; color: #2563eb; }
.badge.overdue { background: #fca5a5; color: #7f1d1d; }
.project-stats .resolved { color: #10b981; }
.project-stats .overdue { color: #dc2626; }
.empty-stats { color: var(--text-muted); font-size: 0.8125rem; text-align: center; padding: 20px; }

/* 交接记录样式 */
.handover-section { grid-column: 1 / -1; }
.handover-section h4 { display: flex; align-items: center; justify-content: space-between; }
.handover-section h4 .count { font-size: 0.75rem; color: var(--text-muted); font-weight: 400; background: var(--bg-hover); padding: 2px 8px; border-radius: 10px; }
.handover-list { display: flex; flex-direction: column; gap: 10px; max-height: 320px; overflow-y: auto; padding-right: 8px; }
.handover-list::-webkit-scrollbar { width: 6px; }
.handover-list::-webkit-scrollbar-track { background: var(--bg-hover); border-radius: 3px; }
.handover-list::-webkit-scrollbar-thumb { background: var(--border-color); border-radius: 3px; }
.handover-list::-webkit-scrollbar-thumb:hover { background: var(--text-muted); }
.handover-item { padding: 12px 14px; background: var(--bg-hover); border-radius: 8px; border-left: 3px solid #3b82f6; transition: all 0.15s; overflow: visible; }
.handover-item:hover { background: var(--bg-card); box-shadow: 0 2px 8px rgba(0,0,0,0.08); }

/* 顶部标签行 */
.handover-tags-row { display: flex; flex-wrap: wrap; gap: 6px; margin-bottom: 10px; align-items: center; }
.tag-inline { font-size: 0.7rem; padding: 2px 8px; border-radius: 4px; font-weight: 500; white-space: nowrap; }
.tag-inline.project { background: rgba(59, 130, 246, 0.12); color: #1d4ed8; }
.tag-inline.feedback.proactive { background: rgba(16, 185, 129, 0.12); color: #059669; }
.tag-inline.feedback.customer { background: rgba(245, 158, 11, 0.12); color: #b45309; }
.tag-inline.event { background: rgba(139, 92, 246, 0.12); color: #7c3aed; }
.tag-inline.handover-person { background: rgba(236, 72, 153, 0.12); color: #db2777; }
.tag-inline.status { font-weight: 600; }
.tag-inline.status.resolved { background: #d1fae5; color: #059669; }
.tag-inline.status.pending { background: #fef3c7; color: #d97706; }
.tag-inline.status.in_progress { background: #ede9fe; color: #7c3aed; }
.tag-inline.status.normal { background: #e0f2fe; color: #0284c7; }

/* 字段信息行 */
.handover-fields { display: flex; gap: 20px; margin-bottom: 10px; flex-wrap: wrap; }
.field-item { display: flex; align-items: center; gap: 6px; }
.field-label { font-size: 0.7rem; color: var(--text-muted); font-weight: 500; }
.field-value { font-size: 0.8rem; color: var(--text-primary); font-weight: 500; }
.field-value.time { font-size: 0.75rem; color: var(--text-secondary); }

/* 交接内容 */
.handover-content-section { margin-top: 8px; overflow: visible; }
.content-label { display: block; font-size: 0.65rem; color: var(--text-muted); font-weight: 500; margin-bottom: 4px; }
.content-text { font-size: 0.8rem; color: var(--text-secondary); line-height: 1.6; background: var(--bg-card); padding: 10px 12px; border-radius: 5px; border: 1px solid var(--border-color); white-space: pre-wrap; word-break: break-word; overflow: visible; display: block; }

/* 筛选 */
.filters { background: var(--bg-card); border-radius: 8px; padding: 12px 16px; margin-bottom: 16px; border: 1px solid var(--border-color); }
.filter-row { display: flex; flex-wrap: wrap; gap: 12px; align-items: flex-end; }
.filter-group { display: flex; flex-direction: column; gap: 4px; }
.filter-group label { font-size: 0.7rem; color: var(--text-muted); font-weight: 500; }
.filter-group select, .filter-group input[type="text"], .filter-group input[type="date"], .filter-group input[type="number"] { padding: 5px 10px; border: 1px solid var(--border-color); border-radius: 5px; font-size: 0.8125rem; background: var(--bg-input); color: var(--text-primary); min-width: 100px; }
.range-inputs { display: flex; align-items: center; gap: 4px; }
.range-input { width: 60px !important; min-width: 60px !important; }
.range-sep { color: var(--text-muted); font-size: 0.75rem; }
.filter-group select:focus, .filter-group input:focus { outline: none; border-color: #3b82f6; }
.checkbox-group { flex-direction: row; align-items: center; }
.checkbox-label { display: flex; align-items: center; gap: 5px; font-size: 0.8125rem; color: var(--text-secondary); cursor: pointer; }
.filter-actions { display: flex; gap: 8px; align-items: center; margin-left: auto; }
.filter-actions .btn { display: inline-flex; align-items: center; gap: 4px; }
.filter-actions .btn .icon { width: 14px; height: 14px; }

/* 表格 */
.table-wrapper { background: var(--bg-card); border-radius: 10px; border: 1px solid var(--border-color); overflow-x: auto; }
.data-table { width: 100%; border-collapse: collapse; min-width: 1800px; }
.data-table th { background: var(--bg-hover); padding: 10px 12px; text-align: left; font-size: 0.7rem; font-weight: 600; color: var(--text-muted); text-transform: uppercase; letter-spacing: 0.03em; border-bottom: 1px solid var(--border-color); white-space: nowrap; }
.data-table td { padding: 10px 12px; font-size: 0.8125rem; color: var(--text-primary); border-bottom: 1px solid var(--border-color); }
.data-table tr { transition: background 0.15s; }
.data-table tbody tr:hover { background: var(--bg-hover); }
.data-table tr.overdue { background: #3d2020; }
.data-table tr.overdue:hover { background: #4a2525; }

/* 亮色模式逾期行 */
.light-mode .data-table tr.overdue { background: #fef2f2; }
.light-mode .data-table tr.overdue:hover { background: #fee2e2; }

.nowrap { white-space: nowrap; }
.col-date { width: 85px; }
.col-person, .col-handler, .col-creator { width: 60px; }
.col-project { width: 70px; }
.col-task { min-width: 120px; max-width: 200px; word-break: break-word; max-height: 80px; overflow: hidden; display: -webkit-box; -webkit-line-clamp: 3; -webkit-box-orient: vertical; }
.col-feedback { width: 65px; }
.col-event { width: 80px; }
.col-desc { min-width: 120px; max-width: 200px; word-break: break-word; max-height: 80px; overflow: hidden; display: -webkit-box; -webkit-line-clamp: 3; -webkit-box-orient: vertical; }
.col-time { width: 75px; font-size: 0.7rem; }
.col-call { width: 55px; text-align: center; }
.col-answered { width: 55px; }
.col-resp { width: 45px; text-align: center; }
.col-escalate { width: 65px; }
.col-handover { width: 55px; }
.col-handover-content { min-width: 120px; max-width: 200px; word-break: break-word; white-space: normal; max-height: 80px; overflow: hidden; display: -webkit-box; -webkit-line-clamp: 3; -webkit-box-orient: vertical; }
.col-status { width: 60px; }
.col-solution { width: 120px; max-width: 200px; white-space: normal; word-break: break-all; max-height: 80px; overflow: hidden; display: -webkit-box; -webkit-line-clamp: 3; -webkit-box-orient: vertical; }
.col-planned { width: 70px; }
.col-overdue { width: 40px; }
.col-attach { width: 55px; }
.col-action { width: 90px; max-width: 90px; min-width: 90px; padding: 8px 4px !important; }

/* 固定操作列 */
/* 暗黑模式固定列 - 使用实体颜色 */
.sticky-right { position: sticky; right: 0; background: #1a1f2e; box-shadow: -2px 0 4px rgba(0,0,0,0.3); z-index: 10; }
thead .sticky-right { background: #252d3d; }
tr:hover .sticky-right { background: #252d3d; }
tr.overdue .sticky-right { background: #3d2020; }
tr.overdue:hover .sticky-right { background: #4a2525; }

/* 固定复选框列 */
.sticky-left { position: sticky; left: 0; background: #1a1f2e; box-shadow: 2px 0 4px rgba(0,0,0,0.3); z-index: 10; }
thead .sticky-left { background: #252d3d; }
tr:hover .sticky-left { background: #252d3d; }
tr.overdue .sticky-left { background: #3d2020; }
tr.overdue:hover .sticky-left { background: #4a2525; }
tr.selected .sticky-left, tr.selected .sticky-right { background: #1e3a5f; }
tr.selected { background: #1e3a5f !important; }
tr.selected:hover { background: #254b75 !important; }

/* 亮色模式固定列 */
.light-mode .sticky-right, .light-mode .sticky-left { background: #ffffff !important; }
.light-mode thead .sticky-right, .light-mode thead .sticky-left { background: #f8fafc !important; }
.light-mode tr:hover .sticky-right, .light-mode tr:hover .sticky-left { background: #f1f5f9 !important; }
.light-mode tr.overdue .sticky-right, .light-mode tr.overdue .sticky-left { background: #fef2f2 !important; }
.light-mode tr.overdue:hover .sticky-right, .light-mode tr.overdue:hover .sticky-left { background: #fee2e2 !important; }
.light-mode tr.selected .sticky-left, .light-mode tr.selected .sticky-right { background: #dbeafe !important; }
.light-mode tr.selected { background: #dbeafe !important; }
.light-mode tr.selected:hover { background: #bfdbfe !important; }
.col-checkbox { width: 40px; text-align: center; padding: 8px !important; }
.col-checkbox input[type="checkbox"] { width: 16px; height: 16px; cursor: pointer; accent-color: #3b82f6; }

/* 批量操作栏 */
.batch-action-bar {
  display: flex; align-items: center; justify-content: space-between;
  padding: 12px 20px; background: linear-gradient(135deg, #3b82f6 0%, #1d4ed8 100%);
  border-radius: 10px; margin-bottom: 15px; color: white;
  position: relative; z-index: 100;
}
.batch-info { font-size: 0.95rem; }
.batch-info strong { font-weight: 600; font-size: 1.1rem; }
.batch-actions { display: flex; gap: 10px; }
.batch-btn {
  display: flex; align-items: center; gap: 6px;
  padding: 8px 16px; border-radius: 6px; border: none;
  font-size: 0.85rem; font-weight: 500; cursor: pointer;
  transition: all 0.2s;
}
.batch-btn svg { width: 16px; height: 16px; pointer-events: none; }
.batch-btn.status-btn { background: var(--bg-card); color: #3b82f6; }
.batch-btn.status-btn:hover { background: var(--bg-hover); }
.batch-btn.delete-btn { background: #ef4444; color: white; }
.batch-btn.delete-btn:hover { background: #dc2626; }
.batch-btn.cancel-btn { background: rgba(255,255,255,0.2); color: white; }
.batch-btn.cancel-btn:hover { background: rgba(255,255,255,0.3); }

/* 批量修改模态框 */
.batch-modal { max-width: 400px; }
.batch-hint { color: var(--text-muted); margin-bottom: 20px; text-align: center; }
.batch-hint strong { color: #3b82f6; }
.batch-select {
  width: 100%; padding: 12px; border: 1px solid var(--border-color); border-radius: 8px;
  font-size: 1rem; background: var(--bg-input); color: var(--text-primary); cursor: pointer;
}
.batch-select:focus { border-color: #3b82f6; outline: none; box-shadow: 0 0 0 3px rgba(59,130,246,0.1); }

.loading-cell, .empty-cell { text-align: center; color: var(--text-muted); padding: 50px 20px !important; }
.loading-cell { display: flex; align-items: center; justify-content: center; gap: 10px; }
.loading-spinner { width: 20px; height: 20px; border: 2px solid var(--border-color); border-top-color: #3b82f6; border-radius: 50%; animation: spin 0.8s linear infinite; }
@keyframes spin { to { transform: rotate(360deg); } }
.empty-cell { flex-direction: column; }
.empty-icon { width: 48px; height: 48px; margin-bottom: 10px; opacity: 0.4; }

.feedback-badge { font-size: 0.7rem; padding: 2px 6px; border-radius: 4px; }
.feedback-badge.proactive { background: #dbeafe; color: #1d4ed8; }
.feedback-badge.customer { background: #fef3c7; color: #b45309; }

.call-info { display: flex; flex-direction: column; gap: 2px; font-size: 0.75rem; }
.call-count { font-weight: 500; }
.call-count.answered { color: #10b981; }
.answered-badge { color: #10b981; font-size: 0.7rem; font-weight: 500; }
.not-answered-badge { color: #ef4444; font-size: 0.7rem; font-weight: 500; }
.none-badge { color: var(--text-muted); font-size: 0.7rem; }

.escalate-badge { background: #fef3c7; color: #b45309; padding: 2px 6px; border-radius: 4px; font-size: 0.7rem; }
.handover-badge { background: #dbeafe; color: #1d4ed8; padding: 2px 6px; border-radius: 4px; font-size: 0.7rem; }

.status-badge { display: inline-block; padding: 3px 8px; border-radius: 10px; font-size: 0.7rem; font-weight: 500; color: white; }
.overdue-badge { background: #fca5a5; color: #7f1d1d; padding: 2px 6px; border-radius: 8px; font-size: 0.65rem; font-weight: 600; }

.attachments-preview { display: flex; align-items: center; gap: 3px; }
.attachments-preview .thumb { width: 28px; height: 28px; object-fit: cover; border-radius: 4px; cursor: pointer; transition: transform 0.15s; }
.attachments-preview .thumb:hover { transform: scale(1.1); }
.more-count { font-size: 0.7rem; color: var(--text-muted); cursor: pointer; padding: 2px 4px; border-radius: 4px; background: var(--bg-hover); }
.more-count:hover { background: var(--bg-input); color: var(--text-primary); }

.action-buttons { display: flex; gap: 4px; }
.btn-icon { width: 28px; height: 28px; border: none; background: var(--bg-hover); border-radius: 5px; cursor: pointer; display: flex; align-items: center; justify-content: center; transition: all 0.15s; }
.btn-icon svg { width: 14px; height: 14px; color: var(--text-muted); }
.btn-icon:hover { background: var(--bg-input); }
.btn-icon:hover svg { color: var(--text-primary); }
.btn-icon.warning:hover { background: rgba(245, 158, 11, 0.15); }
.btn-icon.warning:hover svg { color: #d97706; }
.btn-icon.danger:hover { background: rgba(239, 68, 68, 0.15); }
.btn-icon.danger:hover svg { color: #dc2626; }
.btn-mini-link {
  margin-left: 6px;
  border: none;
  background: transparent;
  color: #3b82f6;
  font-size: 12px;
  cursor: pointer;
  padding: 0;
}
.btn-mini-link:hover { color: #2563eb; text-decoration: underline; }

/* 弹窗通用 */
.modal-overlay { position: fixed; inset: 0; background: rgba(0, 0, 0, 0.6); display: flex; align-items: center; justify-content: center; z-index: 1000; padding: 20px; }
.modal { background: var(--bg-card); border-radius: 12px; width: 100%; max-height: 90vh; display: flex; flex-direction: column; box-shadow: 0 20px 40px rgba(0, 0, 0, 0.3); }
.modal-header { display: flex; justify-content: space-between; align-items: center; padding: 16px 20px; border-bottom: 1px solid var(--border-color); }
.modal-header h3 { margin: 0; font-size: 1.125rem; font-weight: 600; color: var(--text-primary); }
.close-btn { width: 28px; height: 28px; border: none; background: transparent; font-size: 22px; color: var(--text-muted); cursor: pointer; border-radius: 5px; }
.close-btn:hover { background: var(--bg-hover); color: var(--text-secondary); }
.modal-body { padding: 20px; overflow-y: auto; flex: 1; }
.modal-footer { display: flex; justify-content: flex-end; gap: 10px; padding: 14px 20px; border-top: 1px solid var(--border-color); }

/* 详情弹窗 */
.detail-modal { max-width: 800px; }
.detail-grid { display: flex; flex-direction: column; gap: 20px; }
.detail-section { background: var(--bg-hover); border-radius: 8px; padding: 14px; }
.detail-section.full-width { grid-column: 1 / -1; }
.detail-section h4 { margin: 0 0 12px; font-size: 0.8125rem; font-weight: 600; color: #3b82f6; border-bottom: 1px solid var(--border-color); padding-bottom: 8px; }
.detail-row { display: flex; margin-bottom: 8px; font-size: 0.8125rem; }
.detail-row label { width: 100px; color: var(--text-muted); flex-shrink: 0; }
.detail-row span { color: var(--text-primary); flex: 1; }
.detail-row.full { flex-direction: column; gap: 4px; }
.detail-row.full label { width: auto; }
.overdue-text { color: #dc2626; font-weight: 500; }
.detail-attachments { display: flex; flex-wrap: wrap; gap: 10px; }
.detail-thumb-wrapper { position: relative; }
.detail-thumb { width: 80px; height: 80px; object-fit: cover; border-radius: 6px; cursor: pointer; transition: transform 0.15s; }
.detail-thumb:hover { transform: scale(1.05); }
.detail-thumb-wrapper .share-btn { position: absolute; bottom: 4px; right: 4px; width: 24px; height: 24px; border: none; background: rgba(59, 130, 246, 0.9); color: white; border-radius: 4px; cursor: pointer; display: flex; align-items: center; justify-content: center; opacity: 0; transition: opacity 0.2s; }
.detail-thumb-wrapper:hover .share-btn { opacity: 1; }
.detail-thumb-wrapper .share-btn svg { width: 14px; height: 14px; }
.detail-thumb-wrapper .share-btn:hover { background: #2563eb; }

/* 分享弹窗 */
.share-hint { color: var(--text-secondary); margin: 0 0 16px; text-align: center; }
.share-hint strong { color: #3b82f6; }
.share-result { text-align: center; }
.share-success-hint { color: #10b981; font-weight: 500; margin: 0 0 12px; }
.share-url-box { display: flex; gap: 8px; margin-bottom: 12px; }
.share-url-input { flex: 1; padding: 10px 12px; border: 1px solid var(--border-color); border-radius: 6px; font-size: 0.875rem; background: var(--bg-input); color: var(--text-primary); }
.copy-btn { padding: 10px 16px; border: none; background: #3b82f6; color: white; border-radius: 6px; cursor: pointer; font-weight: 500; }
.copy-btn:hover { background: #2563eb; }
.share-expires { color: var(--text-muted); font-size: 0.8125rem; margin: 0; }
.share-expires.permanent { color: #10b981; }

/* 添加/编辑弹窗 */
.duty-modal { max-width: 900px; }
.small-modal { max-width: 520px; }
.form-sections { display: flex; flex-direction: column; gap: 16px; }
.form-section { background: var(--bg-hover); border-radius: 8px; padding: 14px; }
.form-section h4 { margin: 0 0 12px; font-size: 0.8125rem; font-weight: 600; color: #3b82f6; }
.form-grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(180px, 1fr)); gap: 12px; }
.form-group { display: flex; flex-direction: column; gap: 4px; }
.form-group.full-width { grid-column: 1 / -1; }
.form-group.checkbox { flex-direction: row; align-items: center; }
.form-group.checkbox label { display: flex; align-items: center; gap: 6px; cursor: pointer; }
.checkbox-group { display: flex; gap: 16px; flex-wrap: wrap; }
.checkbox-item { display: flex; align-items: center; gap: 6px; cursor: pointer; font-size: 0.8125rem; color: var(--text-primary); }
.checkbox-item input[type="checkbox"] { width: 16px; height: 16px; accent-color: #3b82f6; cursor: pointer; }
.form-group.escalate-options { flex-direction: column; align-items: flex-start; }
.form-group label { font-size: 0.75rem; font-weight: 500; color: var(--text-secondary); }
.required { color: #ef4444; }
.form-group input, .form-group select, .form-group textarea { padding: 7px 10px; border: 1px solid var(--border-color); border-radius: 5px; font-size: 0.8125rem; background: var(--bg-input); color: var(--text-primary); }
.form-group input:focus, .form-group select:focus, .form-group textarea:focus { outline: none; border-color: #3b82f6; box-shadow: 0 0 0 2px rgba(59, 130, 246, 0.1); }
.form-group textarea { resize: vertical; }
.response-time-display { padding: 7px 10px; border: 1px solid var(--border-color); border-radius: 5px; font-size: 0.875rem; background: var(--bg-hover); color: var(--text-primary); font-weight: 500; }
.helper-text { font-size: 0.7rem; color: var(--text-muted); margin-top: 4px; display: block; }
.helper-text.warning { color: #f59e0b; }
.helper-text.info { color: #3b82f6; }
.disabled-field { background: var(--bg-hover) !important; cursor: not-allowed !important; opacity: 0.7; }

.attachments-area { display: flex; flex-direction: column; gap: 12px; }
.attachment-list { display: flex; flex-wrap: wrap; gap: 10px; }
.attachment-item { position: relative; width: 80px; height: 80px; }
.attachment-item img { width: 100%; height: 100%; object-fit: cover; border-radius: 8px; cursor: pointer; border: 1px solid var(--border-color); transition: transform 0.15s; }
.attachment-item img:hover { transform: scale(1.05); }
.attachment-item .remove-btn { position: absolute; top: -6px; right: -6px; width: 20px; height: 20px; border: none; background: #ef4444; color: white; border-radius: 50%; cursor: pointer; font-size: 14px; line-height: 1; display: flex; align-items: center; justify-content: center; box-shadow: 0 1px 3px rgba(0,0,0,0.2); }
.attachment-item .remove-btn:hover { background: #dc2626; }
.upload-zone { border: 2px dashed var(--border-color); border-radius: 10px; padding: 24px; text-align: center; cursor: pointer; transition: all 0.2s; background: var(--bg-input); outline: none; }
.upload-zone:hover, .upload-zone:focus { border-color: #3b82f6; background: rgba(59, 130, 246, 0.1); }
.upload-zone.drag-over { border-color: #3b82f6; background: rgba(59, 130, 246, 0.15); transform: scale(1.01); }
.upload-zone input[type="file"] { display: none; }
.upload-content { display: flex; flex-direction: column; align-items: center; gap: 8px; }
.upload-icon { width: 36px; height: 36px; color: #3b82f6; }
.upload-text { margin: 0; font-size: 0.875rem; color: var(--text-secondary); }
.upload-text .primary-action { color: #3b82f6; font-weight: 500; cursor: pointer; }
.upload-text .primary-action:hover { text-decoration: underline; }
.upload-text kbd { display: inline-block; padding: 2px 6px; font-size: 0.75rem; font-family: inherit; background: var(--bg-hover); border-radius: 4px; border: 1px solid var(--border-color); }
.upload-hint { margin: 0; font-size: 0.75rem; color: var(--text-muted); }

/* 项目配置弹窗 */
.project-modal { max-width: 700px; }
.project-list-container { }
.project-list-header { display: flex; justify-content: space-between; align-items: center; margin-bottom: 16px; }
.project-list-header span { font-size: 0.875rem; color: var(--text-muted); }
.project-grid { display: flex; flex-direction: column; gap: 10px; max-height: 400px; overflow-y: auto; }
.project-card { display: flex; justify-content: space-between; align-items: center; padding: 12px 14px; background: var(--bg-hover); border-radius: 8px; border: 1px solid var(--border-color); }
.project-card.disabled { opacity: 0.6; }
.project-info { flex: 1; }
.project-title { display: flex; align-items: center; gap: 8px; margin-bottom: 4px; }
.project-title .name { font-weight: 600; color: var(--text-primary); }
.project-title .code { font-size: 0.7rem; color: var(--text-muted); background: var(--bg-hover); padding: 1px 5px; border-radius: 3px; font-family: monospace; }
.project-info .desc { font-size: 0.8125rem; color: var(--text-muted); margin: 0 0 6px; }
.status-tag { font-size: 0.7rem; padding: 2px 8px; border-radius: 10px; }
.status-tag.active { background: rgba(5, 150, 105, 0.2); color: #10b981; }
.status-tag.disabled { background: var(--bg-hover); color: var(--text-muted); }
.project-actions { display: flex; gap: 6px; }

.project-form { }
.back-btn { background: none; border: none; color: #3b82f6; font-size: 0.8125rem; cursor: pointer; margin-bottom: 16px; padding: 0; }
.back-btn:hover { text-decoration: underline; }
.form-row { display: grid; grid-template-columns: 1fr 1fr; gap: 12px; }
.form-actions { display: flex; justify-content: flex-end; gap: 10px; margin-top: 16px; }

/* 分页 */
.pagination-wrapper { display: flex; justify-content: space-between; align-items: center; padding: 12px 16px; background: var(--bg-card); border-radius: 0 0 10px 10px; border: 1px solid var(--border-color); border-top: none; margin-top: -1px; }
.pagination-info { font-size: 0.8125rem; color: var(--text-muted); }
.pagination-controls { display: flex; align-items: center; gap: 6px; }
.page-size-select { padding: 5px 8px; border: 1px solid var(--border-color); border-radius: 5px; font-size: 0.8125rem; background: var(--bg-input); color: var(--text-primary); cursor: pointer; }
.page-btn { padding: 5px 10px; border: 1px solid var(--border-color); border-radius: 5px; background: var(--bg-card); color: var(--text-primary); font-size: 0.75rem; cursor: pointer; transition: all 0.15s; }
.page-btn:hover:not(:disabled) { background: var(--bg-hover); border-color: var(--text-muted); }
.page-btn:disabled { opacity: 0.5; cursor: not-allowed; }
.page-nums { display: flex; gap: 4px; }
.page-num { width: 28px; height: 28px; border: 1px solid var(--border-color); border-radius: 5px; background: var(--bg-card); color: var(--text-primary); font-size: 0.8125rem; cursor: pointer; transition: all 0.15s; }
.page-num:hover { background: var(--bg-hover); }
.page-num.active { background: #3b82f6; color: white; border-color: #3b82f6; }
</style>

<style>
/* 图片预览 - 全局样式，必须高于 base.css 的 modal-overlay (z-index: 10000 !important) */
.image-preview-overlay { 
  position: fixed !important; 
  inset: 0 !important; 
  background: rgba(0, 0, 0, 0.95) !important; 
  display: flex !important; 
  align-items: center !important; 
  justify-content: center !important; 
  z-index: 999999 !important; 
}
.image-preview-container { position: relative; max-width: 90vw; max-height: 90vh; z-index: 999999; }
.preview-image { max-width: 100%; max-height: 85vh; object-fit: contain; border-radius: 6px; }
.preview-close { position: absolute; top: -35px; right: 0; width: 32px; height: 32px; border: none; background: rgba(255, 255, 255, 0.2); color: white; font-size: 22px; border-radius: 50%; cursor: pointer; z-index: 999999; }
.preview-nav { position: absolute; top: 50%; transform: translateY(-50%); width: 40px; height: 40px; border: none; background: rgba(255, 255, 255, 0.2); color: white; font-size: 20px; border-radius: 50%; cursor: pointer; }
.preview-nav.prev { left: -50px; }
.preview-nav.next { right: -50px; }
.preview-counter { position: absolute; bottom: -25px; left: 50%; transform: translateX(-50%); color: white; font-size: 0.8125rem; }
</style>

<!-- 全局样式 - 用于 Teleport 的模态框 -->
<style>
.batch-modal-overlay {
  position: fixed;
  inset: 0;
  background: rgba(0, 0, 0, 0.5);
  display: flex;
  align-items: center;
  justify-content: center;
  z-index: 99999;
  padding: 20px;
}
.batch-modal-content {
  background: var(--bg-card);
  border-radius: 12px;
  width: 100%;
  max-width: 400px;
  box-shadow: 0 20px 40px rgba(0, 0, 0, 0.3);
  animation: batchModalIn 0.2s ease;
}
@keyframes batchModalIn {
  from { opacity: 0; transform: scale(0.95); }
  to { opacity: 1; transform: scale(1); }
}
.batch-modal-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  padding: 16px 20px;
  border-bottom: 1px solid var(--border-color);
}
.batch-modal-header h3 {
  margin: 0;
  font-size: 1.125rem;
  font-weight: 600;
  color: var(--text-primary);
}
.batch-close-btn {
  width: 28px;
  height: 28px;
  border: none;
  background: transparent;
  font-size: 22px;
  color: var(--text-muted);
  cursor: pointer;
  border-radius: 5px;
}
.batch-close-btn:hover {
  background: var(--bg-hover);
  color: var(--text-secondary);
}
.batch-modal-body {
  padding: 20px;
}
.batch-hint {
  color: var(--text-muted);
  margin: 0 0 20px;
  text-align: center;
}
.batch-hint strong {
  color: #3b82f6;
}
.batch-form-group {
  margin-bottom: 16px;
}
.batch-form-group label {
  display: block;
  margin-bottom: 6px;
  font-size: 0.875rem;
  font-weight: 500;
  color: var(--text-secondary);
}
.batch-form-group .required {
  color: #ef4444;
}
.batch-select {
  width: 100%;
  padding: 12px;
  border: 1px solid var(--border-color);
  border-radius: 8px;
  font-size: 1rem;
  background: var(--bg-input);
  color: var(--text-primary);
  cursor: pointer;
}
.batch-select:focus {
  border-color: #3b82f6;
  outline: none;
  box-shadow: 0 0 0 3px rgba(59, 130, 246, 0.1);
}
.batch-modal-footer {
  display: flex;
  justify-content: flex-end;
  gap: 10px;
  padding: 14px 20px;
  border-top: 1px solid var(--border-color);
}
.batch-btn-cancel {
  padding: 10px 20px;
  border: 1px solid var(--border-color);
  background: var(--bg-hover);
  color: var(--text-secondary);
  border-radius: 8px;
  font-size: 0.875rem;
  font-weight: 500;
  cursor: pointer;
  transition: all 0.2s;
}
.batch-btn-cancel:hover {
  background: var(--bg-card);
  border-color: var(--text-muted);
}
.batch-btn-confirm {
  padding: 10px 20px;
  border: none;
  background: linear-gradient(135deg, #3b82f6, #2563eb);
  color: white;
  border-radius: 8px;
  font-size: 0.875rem;
  font-weight: 500;
  cursor: pointer;
  transition: all 0.2s;
}
.batch-btn-confirm:hover {
  background: linear-gradient(135deg, #2563eb, #1d4ed8);
  box-shadow: 0 4px 12px rgba(37, 99, 235, 0.3);
}
.batch-btn-confirm:disabled {
  opacity: 0.5;
  cursor: not-allowed;
}

/* 粘贴弹窗样式已移至非 scoped style 块 */
</style>

<style>
/* 粘贴弹窗 - 非 scoped，用于 Teleport to body */
.paste-modal-overlay {
  position: fixed;
  inset: 0;
  background: rgba(0, 0, 0, 0.6);
  display: flex;
  align-items: center;
  justify-content: center;
  z-index: 99999;
  padding: 20px;
}
.paste-modal {
  background: var(--bg-card);
  border-radius: 16px;
  width: 420px;
  max-width: 95vw;
  box-shadow: 0 20px 60px rgba(0, 0, 0, 0.4);
  border: 1px solid var(--border-color);
}
.paste-modal-header {
  padding: 20px 24px;
  border-bottom: 1px solid var(--border-color);
  display: flex;
  justify-content: space-between;
  align-items: center;
}
.paste-modal-header h3 {
  margin: 0;
  font-size: 1.1rem;
  font-weight: 600;
  color: var(--text-primary);
}
.paste-close-btn {
  background: none;
  border: none;
  font-size: 1.5rem;
  color: var(--text-muted);
  cursor: pointer;
  line-height: 1;
}
.paste-close-btn:hover {
  color: var(--text-primary);
}
.paste-modal-body {
  padding: 24px;
}
.paste-info { margin-bottom: 20px; }
.paste-hint { font-size: 0.875rem; color: var(--text-muted); margin-bottom: 12px; }
.paste-preview { background: var(--bg-hover); border-radius: 8px; padding: 12px; }
.paste-preview .preview-item { display: flex; gap: 8px; font-size: 0.875rem; margin-bottom: 6px; }
.paste-preview .preview-item:last-child { margin-bottom: 0; }
.paste-preview .preview-label { color: var(--text-muted); flex-shrink: 0; }
.paste-preview .preview-value { color: var(--text-primary); word-break: break-all; }
.paste-count-group { display: flex; align-items: center; gap: 12px; margin-bottom: 16px; }
.paste-count-group label { font-size: 0.875rem; color: var(--text-primary); font-weight: 500; }
.paste-count-input { display: flex; align-items: center; gap: 0; border: 1px solid var(--border-color); border-radius: 8px; overflow: hidden; }
.paste-count-input .count-btn { width: 36px; height: 36px; border: none; background: var(--bg-hover); color: var(--text-primary); font-size: 1.2rem; cursor: pointer; transition: all 0.15s; }
.paste-count-input .count-btn:hover { background: var(--bg-input); }
.paste-count-input .count-input { width: 60px; height: 36px; border: none; background: var(--bg-card); color: var(--text-primary); text-align: center; font-size: 1rem; font-weight: 600; -moz-appearance: textfield; }
.paste-count-input .count-input::-webkit-outer-spin-button,
.paste-count-input .count-input::-webkit-inner-spin-button { -webkit-appearance: none; margin: 0; }
.paste-count-group .count-hint { font-size: 0.75rem; color: var(--text-muted); }
.paste-quick-btns { display: flex; gap: 8px; }
.paste-quick-btns .quick-btn { padding: 8px 16px; border: 1px solid var(--border-color); background: var(--bg-card); color: var(--text-primary); border-radius: 6px; font-size: 0.875rem; cursor: pointer; transition: all 0.15s; }
.paste-quick-btns .quick-btn:hover { background: var(--bg-hover); border-color: var(--text-muted); }
.paste-quick-btns .quick-btn.active { background: linear-gradient(135deg, #3b82f6, #2563eb); color: white; border-color: transparent; }
.paste-modal-footer { padding: 16px 24px; border-top: 1px solid var(--border-color); display: flex; justify-content: flex-end; gap: 12px; }
</style>
