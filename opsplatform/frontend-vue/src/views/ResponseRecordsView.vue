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

// v586: 日期范围筛选 (替代之前的"按月")
function todayStr() {
  const d = new Date()
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}`
}
function monthStartStr(date) {
  const d = date || new Date()
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-01`
}
const startDate = ref(monthStartStr())
const endDate = ref(todayStr())
const filterResponder = ref('')
const filterSource = ref('')
const filterOnlyIncident = ref(false)
const keyword = ref('')

// 快捷按钮
function setRange(preset) {
  const now = new Date()
  const pad = n => String(n).padStart(2, '0')
  const fmt = d => `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}`
  if (preset === 'today') {
    startDate.value = endDate.value = fmt(now)
  } else if (preset === 'week') {
    const day = now.getDay() || 7  // 周一为 1
    const monday = new Date(now); monday.setDate(now.getDate() - (day - 1))
    startDate.value = fmt(monday); endDate.value = fmt(now)
  } else if (preset === 'month') {
    startDate.value = monthStartStr(now); endDate.value = fmt(now)
  } else if (preset === 'last_month') {
    const last = new Date(now.getFullYear(), now.getMonth() - 1, 1)
    const lastEnd = new Date(now.getFullYear(), now.getMonth(), 0)
    startDate.value = fmt(last); endDate.value = fmt(lastEnd)
  } else if (preset === '7d') {
    const d = new Date(now); d.setDate(now.getDate() - 6)
    startDate.value = fmt(d); endDate.value = fmt(now)
  } else if (preset === '30d') {
    const d = new Date(now); d.setDate(now.getDate() - 29)
    startDate.value = fmt(d); endDate.value = fmt(now)
  }
}

async function loadEmployees() {
  try {
    // 员工列表用"今天"的月份（不强求跟筛选范围一致，员工不太变）
    const now = new Date()
    const res = await api.get(`/api/schedule?year=${now.getFullYear()}&month=${now.getMonth() + 1}`)
    employees.value = (res.data || []).map(e => ({ id: e.id, name: e.name, group_name: e.group_name }))
  } catch (e) { console.error(e) }
}

async function loadRecords() {
  loading.value = true
  try {
    const params = new URLSearchParams()
    if (startDate.value) params.append('start_date', startDate.value)
    if (endDate.value) params.append('end_date', endDate.value)
    if (filterResponder.value) params.append('responder', filterResponder.value)
    if (filterSource.value) params.append('source', filterSource.value)
    if (filterOnlyIncident.value) params.append('has_incident', '1')
    if (keyword.value.trim()) params.append('keyword', keyword.value.trim())
    const res = await api.get('/api/response-records?' + params.toString())
    records.value = (res.data || []).map(r => ({
      ...r,
      attachments: parseAttachments(r.attachments)
    }))
    // v587: 批量预热签名 URL 缓存
    const allAttachments = records.value.flatMap(r => r.attachments || [])
    await loadPresignedUrls(allAttachments)
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

watch([startDate, endDate], () => loadRecords())
watch([filterResponder, filterSource, filterOnlyIncident], () => loadRecords())

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

// v584: 拿 record 的 responders 数组（v743: 每行 mentioned_at 兜底用主表）
function recResponders(r) {
  const list = (r.responders && r.responders.length)
    ? r.responders
    : [{ responder: r.responder, responded_at: r.responded_at, completed_at: r.completed_at || '' }]
  return list.map(x => ({
    responder: x.responder,
    mentioned_at: x.mentioned_at || r.mentioned_at,  // v743: 缺就用主表
    responded_at: x.responded_at || '',
    completed_at: x.completed_at || ''
  }))
}

// v743: 每个响应人的状态自动算（4 种 + 晚到提示）
// 返回 { code, label, emoji, color }
function responderState(rr, record) {
  if (!rr.responded_at) {
    return { code: 'no_reply', label: '未响应', emoji: '🔴', color: '#ea3636' }
  }
  if (rr.completed_at) {
    return { code: 'resolved', label: '已解决', emoji: '🟢', color: '#10b981' }
  }
  // responded 但 completed 空：看任务整体状态
  if (record && record.status === 'processing') {
    return { code: 'in_progress', label: '处理中', emoji: '🟡', color: '#f97316' }
  }
  return { code: 'reply_only', label: '仅响应', emoji: '⚪', color: '#94a3b8' }
}

// v743: "晚到响应"判定 — 此人响应时间晚于任务里最早的 completed_at
function isLateResponse(rr, record) {
  if (!rr.responded_at) return false
  const earliestDone = recResponders(record).map(x => x.completed_at).filter(Boolean).sort()[0]
  return earliestDone && rr.responded_at > earliestDone
}

// ========= 统计：按 (record × responder) explode =========
// v743 新版：用每人 mentioned_at 算响应时长；增加 noReply、resolved 计数
const stats = computed(() => {
  const list = records.value
  let respDurs = [], procDurs = []
  let processing = 0, completed = 0
  let totalResponders = 0, noReplyCount = 0, resolvedCount = 0
  list.forEach(r => {
    recResponders(r).forEach(rr => {
      totalResponders++
      if (!rr.responded_at) { noReplyCount++; return }
      // v743: 用 rr 自己的 mentioned_at（兜底主表）
      const rd = diffMinutes(rr.responded_at, rr.mentioned_at)
      if (rd != null && rd >= 0) respDurs.push(rd)
      if (rr.completed_at) {
        resolvedCount++
        const pd = diffMinutes(rr.completed_at, rr.responded_at)
        if (pd != null && pd >= 0) procDurs.push(pd)
      }
    })
    if (r.status === 'processing') processing++; else completed++
  })
  const incidents = list.filter(r => r.has_incident).length
  const avgResp = respDurs.length ? Math.round(respDurs.reduce((a, b) => a + b, 0) / respDurs.length) : 0
  const avgProc = procDurs.length ? Math.round(procDurs.reduce((a, b) => a + b, 0) / procDurs.length) : 0
  const replyRate = totalResponders ? Math.round(((totalResponders - noReplyCount) / totalResponders) * 100) : 0
  const resolverRate = totalResponders ? Math.round((resolvedCount / totalResponders) * 100) : 0
  return { total: list.length, avgResp, avgProc, incidents, processing, completed, replyRate, resolverRate, totalResponders, noReplyCount, resolvedCount }
})

// 按响应人聚合
const employeeStats = computed(() => {
  const map = {}
  records.value.forEach(r => {
    recResponders(r).forEach(rr => {
      if (!rr.responder) return
      if (!map[rr.responder]) map[rr.responder] = { responder: rr.responder, count: 0, replyN: 0, respSum: 0, respN: 0, procSum: 0, procN: 0, incidents: 0 }
      const s = map[rr.responder]
      s.count++
      if (rr.responded_at) {
        s.replyN++
        const rd = diffMinutes(rr.responded_at, rr.mentioned_at)
        if (rd != null && rd >= 0) { s.respSum += rd; s.respN++ }
      }
      if (rr.completed_at) {
        const pd = diffMinutes(rr.completed_at, rr.responded_at)
        if (pd != null && pd >= 0) { s.procSum += pd; s.procN++ }
      }
      if (r.has_incident) s.incidents++
    })
  })
  return Object.values(map)
    .map(s => ({
      ...s,
      avgResp: s.respN ? Math.round(s.respSum / s.respN) : 0,
      avgProc: s.procN ? Math.round(s.procSum / s.procN) : 0,
      replyRate: s.count ? Math.round((s.replyN / s.count) * 100) : 0,
      resolverRate: s.count ? Math.round((s.procN / s.count) * 100) : 0,
      incidentRate: s.count ? Math.round((s.incidents / s.count) * 100) : 0
    }))
    .sort((a, b) => b.count - a.count)
})

// 列表"处理人"列：completed_at 非空的人
function resolvers(r) {
  return recResponders(r).filter(rr => rr.completed_at).map(rr => rr.responder)
}

// 列表行用：首响应时间 / 末完成时间
function firstRespondedAt(r) {
  const arr = recResponders(r).map(x => x.responded_at).filter(Boolean).sort()
  return arr[0] || ''
}
function lastCompletedAt(r) {
  if (recResponders(r).some(x => !x.completed_at)) return ''   // 任一未完成 → 整条进行中
  const arr = recResponders(r).map(x => x.completed_at).filter(Boolean).sort()
  return arr[arr.length - 1] || ''
}

const sourceDistribution = computed(() => {
  // v584 fix: SOURCES 在 v581 改成了 ref（之前是常量数组），这里漏了 .value 导致统计页白屏
  // 字段也变了：用 s.code 代替老的 s.value
  const total = records.value.length || 1
  return SOURCES.value.map(s => ({
    ...s,
    count: records.value.filter(r => r.message_source === s.code).length
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
    // v584: 多响应人；v743: 每人有自己的 mentioned_at（默认 = 主表）
    responders: [{ responder: '', mentioned_at: now, responded_at: '', completed_at: '' }],
    message_source: SOURCES.value[0]?.code || 'lark',
    message_content: '',
    mentioned_at: now,
    has_incident: 0,
    incident_ticket: '',
    handle_result: '',
    remark: '',
    attachments: []
  }
}
function addResponderRow() {
  // 新行默认 mentioned_at = 主表当前值（用户可改）
  form.value.responders.push({
    responder: '',
    mentioned_at: form.value.mentioned_at || formatNow(),
    responded_at: '',
    completed_at: ''
  })
}
function removeResponderRow(idx) {
  if (form.value.responders.length <= 1) return
  form.value.responders.splice(idx, 1)
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
  // v584/v743: 每人有自己的 mentioned_at；老数据缺就用主表
  const responders = (r.responders && r.responders.length)
    ? r.responders.map(x => ({
        responder: x.responder,
        mentioned_at: x.mentioned_at || r.mentioned_at,
        responded_at: x.responded_at || '',
        completed_at: x.completed_at || ''
      }))
    : [{ responder: r.responder, mentioned_at: r.mentioned_at, responded_at: r.responded_at, completed_at: r.completed_at || '' }]
  form.value = {
    id: r.id,
    responders,
    message_source: r.message_source,
    message_content: r.message_content,
    mentioned_at: r.mentioned_at,
    has_incident: r.has_incident,
    incident_ticket: r.incident_ticket || '',
    handle_result: r.handle_result || '',
    remark: r.remark || '',
    attachments: r.attachments || []
  }
  showModal.value = true
}

// v743: 每行用自己的 mentioned_at 算时长
function rowRespDur(row) { return row.responded_at ? diffMinutes(row.responded_at, row.mentioned_at) : null }
function rowProcDur(row) { return (row.responded_at && row.completed_at) ? diffMinutes(row.completed_at, row.responded_at) : null }

async function saveRecord() {
  if (!form.value.message_content) { appStore.showToast('请填消息内容', 'error'); return }
  if (!form.value.mentioned_at) { appStore.showToast('艾特时间必填', 'error'); return }
  if (!form.value.responders || form.value.responders.length === 0) { appStore.showToast('至少 1 个响应人', 'error'); return }
  for (const [i, r] of form.value.responders.entries()) {
    if (!r.responder) { appStore.showToast(`第 ${i + 1} 行响应人未选`, 'error'); return }
    if (!r.mentioned_at) { appStore.showToast(`第 ${i + 1} 行艾特时间必填`, 'error'); return }
    // v743: responded_at / completed_at 都可空（空 = 未响应 / 未解决）
  }
  if (form.value.has_incident && !form.value.incident_ticket) { appStore.showToast('勾选故障后请填故障单号', 'error'); return }

  const cleanAttachments = (form.value.attachments || []).map(a => ({
    name: a.name, size: a.size, path: a.path
  }))
  const payload = {
    responders: form.value.responders.map(r => ({
      responder: r.responder,
      mentioned_at: r.mentioned_at || form.value.mentioned_at,
      responded_at: r.responded_at || '',
      completed_at: r.completed_at || ''
    })),
    message_source: form.value.message_source,
    message_content: form.value.message_content,
    mentioned_at: form.value.mentioned_at,
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
        const att = {
          name: file.name || res.data.name || 'screenshot.png',
          size: file.size,
          path: res.data.path,
          preview: URL.createObjectURL(file)
        }
        form.value.attachments.push(att)
        // v587: 单独给这张图取签名 URL，保存后切回列表能立即显示
        await loadPresignedUrls([att])
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
// v587: 抄桌台维护的 presigned URL 模式 — 浏览器不靠 bucket 公开读，
// 而是用后端签发的临时签名 URL 访问 MinIO。生产 bucket 保持私有。
const presignedUrlCache = ref({})

async function loadPresignedUrls(attachments) {
  if (!attachments) return
  const paths = attachments
    .filter(a => a && a.path && !presignedUrlCache.value[a.path])
    .map(a => a.path)
  if (paths.length === 0) return
  const BATCH_SIZE = 50
  try {
    const batches = []
    for (let i = 0; i < paths.length; i += BATCH_SIZE) {
      batches.push(paths.slice(i, i + BATCH_SIZE))
    }
    const results = await Promise.all(
      batches.map(batch => api.post('/api/storage/presign/batch', { paths: batch }))
    )
    results.forEach(res => {
      const urls = res.data?.urls || {}
      Object.entries(urls).forEach(([path, url]) => {
        presignedUrlCache.value[path] = url
      })
    })
  } catch (e) {
    console.error('获取预签名 URL 失败', e)
  }
}

function getPresignedUrl(path) {
  return presignedUrlCache.value[path] || ''
}

function attachmentURL(a) {
  // 优先用签名 URL（生产合规）；fallback 1：path（本地公开读时直接通）；fallback 2：preview blob
  return getPresignedUrl(a.path) || a.path || a.preview || ''
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
  a.download = `响应记录_${startDate.value}_至_${endDate.value}.csv`
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
      <!-- v586: 日期范围选择器 + 快捷按钮 -->
      <div class="date-range">
        <input type="date" v-model="startDate" class="input dt-input">
        <span class="dash">至</span>
        <input type="date" v-model="endDate" class="input dt-input">
      </div>
      <div class="quick-range">
        <button class="btn-pill" @click="setRange('today')">今天</button>
        <button class="btn-pill" @click="setRange('week')">本周</button>
        <button class="btn-pill" @click="setRange('month')">本月</button>
        <button class="btn-pill" @click="setRange('last_month')">上月</button>
        <button class="btn-pill" @click="setRange('7d')">近7天</button>
        <button class="btn-pill" @click="setRange('30d')">近30天</button>
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
      <div class="ov-card"><div class="ov-num">{{ stats.total }}</div><div class="ov-lbl">总任务数</div></div>
      <div class="ov-card"><div class="ov-num">{{ fmtDuration(stats.avgResp) }}</div><div class="ov-lbl">平均响应时长</div></div>
      <div class="ov-card"><div class="ov-num">{{ fmtDuration(stats.avgProc) }}</div><div class="ov-lbl">平均处理时长</div></div>
      <div class="ov-card"><div class="ov-num ov-good">{{ stats.replyRate }}%</div><div class="ov-lbl">响应率</div></div>
      <div class="ov-card"><div class="ov-num">{{ stats.resolverRate }}%</div><div class="ov-lbl">实干率</div></div>
      <div class="ov-card"><div class="ov-num ov-bad">{{ stats.incidents }}</div><div class="ov-lbl">故障次数</div></div>
    </div>

    <!-- 列表 -->
    <div v-if="activeTab === 'list'" class="table-wrap">
      <table class="data-table">
        <thead>
          <tr>
            <th>#</th>
            <th>响应人</th>
            <th>处理人</th>
            <th>来源</th>
            <th>消息内容</th>
            <th>艾特时间</th>
            <th>首响应</th>
            <th>末完成</th>
            <th>响应明细</th>
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
            <td>
              <div class="resp-badges">
                <span v-for="(rr, i) in recResponders(r)" :key="i" class="resp-badge"
                      :style="{ background: responderState(rr, r).color + '22', color: responderState(rr, r).color }"
                      :title="`${responderState(rr, r).label}｜艾特 ${rr.mentioned_at}｜响应 ${rr.responded_at || '-'}｜完成 ${rr.completed_at || '-'}`">
                  {{ responderState(rr, r).emoji }} {{ rr.responder }}
                </span>
              </div>
            </td>
            <td>
              <div v-if="resolvers(r).length" class="resolver-list">
                <span v-for="(name, i) in resolvers(r)" :key="i" class="resolver-tag">{{ name }}</span>
              </div>
              <span v-else class="muted">-</span>
            </td>
            <td><span class="source-badge" :style="{ background: sourceColor(r.message_source) }">{{ sourceLabel(r.message_source) }}</span></td>
            <td class="msg">
              <div>{{ r.message_content }}</div>
              <div v-if="r.has_incident" class="incident-tag">⚠ 故障单 {{ r.incident_ticket }}</div>
            </td>
            <td class="ts">{{ r.mentioned_at }}</td>
            <td class="ts">{{ firstRespondedAt(r) || '-' }}</td>
            <td class="ts">{{ lastCompletedAt(r) || '-' }}</td>
            <td class="resp-detail">
              <div v-for="(rr, i) in recResponders(r)" :key="i" class="resp-line">
                <span class="resp-state" :style="{ color: responderState(rr, r).color }">{{ responderState(rr, r).emoji }}</span>
                <span class="resp-name">{{ rr.responder }}</span>
                <span v-if="rr.responded_at" :class="durationClass(diffMinutes(rr.responded_at, rr.mentioned_at), 5)">
                  响 {{ fmtDuration(diffMinutes(rr.responded_at, rr.mentioned_at)) }}
                  <span v-if="isLateResponse(rr, r)" class="late-tag" title="晚于第一个解决人">⚠</span>
                </span>
                <span v-if="rr.completed_at" :class="durationClass(diffMinutes(rr.completed_at, rr.responded_at), 60)">
                  处 {{ fmtDuration(diffMinutes(rr.completed_at, rr.responded_at)) }}
                </span>
              </div>
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
            <th>#</th><th>员工</th><th>参与次数</th><th>响应率</th><th>实干率</th><th>平均响应</th><th>平均处理</th><th>故障次数</th>
          </tr>
        </thead>
        <tbody>
          <tr v-if="employeeStats.length === 0"><td colspan="8" class="empty-cell">暂无数据</td></tr>
          <tr v-else v-for="(s, idx) in employeeStats" :key="s.responder">
            <td>{{ idx + 1 }}</td>
            <td>{{ s.responder }}</td>
            <td>{{ s.count }}</td>
            <td :title="`${s.replyN}/${s.count}`">{{ s.replyRate }}%</td>
            <td :title="`${s.procN}/${s.count}`">{{ s.resolverRate }}%</td>
            <td>{{ fmtDuration(s.avgResp) }}</td>
            <td>{{ fmtDuration(s.avgProc) }}</td>
            <td>{{ s.incidents }}</td>
          </tr>
        </tbody>
      </table>

      <h3 style="margin-top:24px">来源分布</h3>
      <div class="src-dist">
        <div v-for="s in sourceDistribution" :key="s.code" class="src-item">
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
            <input type="datetime-local" :value="toDtLocal(form.mentioned_at)" @input="form.mentioned_at = fromDtLocal($event.target.value, form.mentioned_at)" class="input dt-input">
            <input v-model="form.mentioned_at" class="input sec-input" title="可在此手动编辑到秒" placeholder="YYYY-MM-DD HH:MM:SS">
          </div>

          <!-- v584/v743: 多响应人动态行（每行自己的艾特时间） -->
          <div class="form-row resp-row-head">
            <label>响应人 *</label>
            <div class="resp-rows">
              <div v-for="(rr, idx) in form.responders" :key="idx" class="resp-row">
                <div class="resp-row-top">
                  <select v-model="rr.responder" class="select resp-select">
                    <option value="">请选择</option>
                    <option v-for="e in employees" :key="e.id" :value="e.name">{{ e.name }} ({{ e.group_name || '-' }})</option>
                  </select>
                  <span class="state-pill" :style="{ background: responderState(rr).color }">{{ responderState(rr).emoji }} {{ responderState(rr).label }}</span>
                  <button class="btn-link danger sm" @click.stop="removeResponderRow(idx)" :disabled="form.responders.length <= 1">删除</button>
                </div>
                <div class="resp-row-times">
                  <div class="resp-time-block">
                    <div class="resp-time-label">艾特</div>
                    <input type="datetime-local" :value="toDtLocal(rr.mentioned_at)" @input="rr.mentioned_at = fromDtLocal($event.target.value, rr.mentioned_at)" class="input dt-input">
                    <input v-model="rr.mentioned_at" class="input sec-input" placeholder="HH:MM:SS">
                  </div>
                  <div class="resp-time-block">
                    <div class="resp-time-label">响应</div>
                    <input type="datetime-local" :value="toDtLocal(rr.responded_at)" @input="rr.responded_at = fromDtLocal($event.target.value, rr.responded_at)" class="input dt-input">
                    <input v-model="rr.responded_at" class="input sec-input" placeholder="留空=未响应">
                    <span v-if="rowRespDur(rr) != null" class="dur-mini" :class="durationClass(rowRespDur(rr), 5)">{{ fmtDuration(rowRespDur(rr)) }}</span>
                  </div>
                  <div class="resp-time-block">
                    <div class="resp-time-label">完成</div>
                    <input type="datetime-local" :value="toDtLocal(rr.completed_at)" @input="rr.completed_at = fromDtLocal($event.target.value, rr.completed_at)" class="input dt-input" :disabled="!rr.responded_at">
                    <input v-model="rr.completed_at" class="input sec-input" placeholder="留空=未解决" :disabled="!rr.responded_at">
                    <span v-if="rowProcDur(rr) != null" class="dur-mini" :class="durationClass(rowProcDur(rr), 60)">{{ fmtDuration(rowProcDur(rr)) }}</span>
                  </div>
                </div>
              </div>
              <button class="btn btn-secondary sm" @click.stop="addResponderRow">+ 添加响应人</button>
            </div>
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
/* v586: 日期范围 */
.date-range { display: flex; align-items: center; gap: 8px; }
.date-range .dt-input { min-width: 140px; max-width: 150px; }
.dash { color: var(--text-secondary); font-size: 13px; }
.quick-range { display: flex; gap: 4px; flex-wrap: wrap; }
.btn-pill { padding: 4px 10px; background: var(--bg-hover); border: 1px solid var(--border-color); border-radius: 14px; font-size: 12px; cursor: pointer; color: var(--text-color); }
.btn-pill:hover { background: var(--primary); color: #fff; border-color: var(--primary); }
.btn-link { background: none; border: none; color: var(--primary); cursor: pointer; font-size: 13px; }
.btn-link.danger { color: #ea3636; }
.filter-group { display: flex; align-items: center; gap: 6px; font-size: 13px; }
.select, .input { padding: 6px 10px; border: 1px solid var(--border-color); background: var(--bg-card); color: var(--text-color); border-radius: 6px; font-size: 13px; }
.select { max-width: 220px; }
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
.dt-input { max-width: 220px; flex: 0 0 auto; }
.sec-input { max-width: 180px; font-family: monospace; font-size: 12px; flex: 0 0 auto; }
.big-check { width: 18px; height: 18px; cursor: pointer; }

/* v584: 响应人多行表单 + 列表响应人徽章 + 明细行 */
.resp-row-head { align-items: flex-start; }
.resp-rows { display: flex; flex-direction: column; gap: 8px; flex: 1; min-width: 0; }
.resp-row { display: flex; flex-direction: column; gap: 6px; padding: 10px; background: var(--bg-hover); border-radius: 8px; }
.resp-row-top { display: flex; align-items: center; gap: 8px; }
.resp-row-times { display: flex; flex-wrap: wrap; gap: 12px; }
.state-pill { padding: 2px 10px; border-radius: 12px; color: #fff; font-size: 11px; font-weight: 600; flex: 0 0 auto; }
.resolver-list { display: flex; flex-wrap: wrap; gap: 3px; }
.resolver-tag { padding: 2px 8px; background: rgba(16, 185, 129, 0.15); color: #10b981; border-radius: 10px; font-size: 11px; font-weight: 600; }
.resp-state { font-size: 13px; flex: 0 0 auto; }
.late-tag { color: #f59e0b; font-weight: 600; cursor: help; margin-left: 2px; }
.resp-select { max-width: 200px; flex: 0 0 auto; }
.resp-time-block { display: flex; align-items: center; gap: 4px; }
.resp-time-label { font-size: 11px; color: var(--text-secondary); width: 24px; }
.dur-mini { font-size: 11px; padding: 2px 6px; border-radius: 4px; background: rgba(16, 185, 129, 0.1); font-weight: 600; min-width: 32px; text-align: center; }
.dur-mini.dur-bad { background: rgba(239, 68, 68, 0.15); }
.dur-mini.dur-warn { background: rgba(249, 115, 22, 0.15); }
.resp-badges { display: flex; flex-wrap: wrap; gap: 3px; max-width: 160px; }
.resp-badge { padding: 2px 8px; background: rgba(59, 130, 246, 0.12); color: var(--primary); border-radius: 10px; font-size: 11px; font-weight: 600; cursor: help; }
.resp-detail { font-size: 11px; max-width: 240px; }
.resp-line { display: flex; gap: 6px; align-items: center; padding: 2px 0; border-bottom: 1px dashed rgba(148, 163, 184, 0.15); }
.resp-line:last-child { border-bottom: none; }
.resp-name { font-weight: 600; min-width: 40px; }

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
