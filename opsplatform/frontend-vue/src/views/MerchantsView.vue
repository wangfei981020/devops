<script setup>
import { ref, computed, onMounted, nextTick } from 'vue'
import { useAppStore, useAuthStore } from '@/stores'
import api from '@/api'

const appStore = useAppStore()
const authStore = useAuthStore()

// v604: 本地时区今天日期（避免 UTC 跨天）
function todayLocal() {
  const n = new Date()
  const p = x => String(x).padStart(2, '0')
  return `${n.getFullYear()}-${p(n.getMonth() + 1)}-${p(n.getDate())}`
}

const merchants = ref([])
const loading = ref(false)

const tempSearchQuery = ref('')
const tempSelectedProject = ref('')
const tempSelectedEnv = ref('')

const searchQuery = ref('')
const selectedProject = ref('')
const selectedEnv = ref('')

const currentPage = ref(1)
const pageSize = ref(20)
const selectedIds = ref([])

const showModal = ref(false)
const modalMode = ref('add')
const editingMerchant = ref(null)
const showBatchModal = ref(false)
const batchText = ref('')
const batchResult = ref(null)
const batchLoading = ref(false)
const showColumnSettings = ref(false)
const showMoreModal = ref(false)
const moreModalTitle = ref('')
const moreModalItems = ref([])

const formData = ref({
  project: '',
  env: 'PROD',
  website_name: '',
  contact_emails: '',
  website_urls: '',
  player_regions: '',
  estimated_players: '',
  game_types: '',
  handicaps: '',
  languages: '',
  currencies: '',
  supported_ports: '',
  wallet_types: '',
  callback_domains: '',
  whitelist_ips: '',
  hall_domains: '',
  site_domains: '',
  site_accounts: '',
  app_keys: '',
  game_domains: '',
  redirect_domains: '',
  remark: '',
  status: 'active'
})

const defaultColumns = [
  { key: 'checkbox', title: '', width: 40, type: 'checkbox', visible: true, fixed: 'left' },
  { key: 'project', title: '项目', width: 90, type: 'tag-project', visible: true },
  { key: 'env', title: '环境', width: 60, type: 'tag-env', visible: true },
  { key: 'website_name', title: '网站方', width: 100, type: 'tag-website', visible: true },
  { key: 'contact_emails', title: '对接邮箱', width: 180, type: 'multi-email', visible: true },
  { key: 'website_urls', title: '网站网址', width: 200, type: 'multi-link', visible: true },
  { key: 'player_regions', title: '玩家地区', width: 120, type: 'tags', visible: true },
  { key: 'estimated_players', title: '在线玩家', width: 100, type: 'text', visible: true },
  { key: 'game_types', title: '游戏种类', width: 140, type: 'tags', visible: true },
  { key: 'handicaps', title: '跟单', width: 100, type: 'tags', visible: true },
  { key: 'languages', title: '语言', width: 120, type: 'tags', visible: true },
  { key: 'currencies', title: '币种', width: 120, type: 'tags', visible: true },
  { key: 'supported_ports', title: '支持端口', width: 100, type: 'tags', visible: true },
  { key: 'wallet_types', title: '钱包类型', width: 100, type: 'tags', visible: true },
  { key: 'callback_domains', title: '三方回调域名', width: 220, type: 'multi-link', visible: true },
  { key: 'whitelist_ips', title: '三方白名单', width: 180, type: 'multi-mono', visible: true },
  { key: 'hall_domains', title: '厅房域名', width: 220, type: 'multi-link', visible: true },
  { key: 'site_domains', title: '站点域名', width: 220, type: 'multi-link', visible: true },
  { key: 'site_accounts', title: '站点账号', width: 160, type: 'multi-account', visible: true },
  { key: 'app_keys', title: 'AppKey', width: 180, type: 'multi-key', visible: true },
  { key: 'game_domains', title: '游戏域名', width: 220, type: 'multi-link', visible: true },
  { key: 'redirect_domains', title: '301域名', width: 220, type: 'multi-link', visible: true },
  { key: 'status', title: '状态', width: 80, type: 'status', visible: true },
  { key: 'actions', title: '操作', width: 90, type: 'actions', visible: true, fixed: 'right' }
]

const columnConfig = ref(JSON.parse(JSON.stringify(defaultColumns)))
const tempColumnConfig = ref([])
const draggedIndex = ref(null)

const isAdmin = computed(() => {
  return authStore.isSuperAdmin()
})

const projectOptions = computed(() => {
  const projects = [...new Set(merchants.value.map(m => m.project).filter(Boolean))]
  return projects.sort()
})

const filteredMerchants = computed(() => {
  let list = merchants.value
  if (searchQuery.value) {
    const q = searchQuery.value.toLowerCase()
    list = list.filter(m => 
      m.website_name?.toLowerCase().includes(q) ||
      m.project?.toLowerCase().includes(q)
    )
  }
  if (selectedProject.value) {
    list = list.filter(m => m.project === selectedProject.value)
  }
  if (selectedEnv.value) {
    list = list.filter(m => m.env?.toUpperCase() === selectedEnv.value)
  }
  return list
})

const pagedMerchants = computed(() => {
  const start = (currentPage.value - 1) * pageSize.value
  return filteredMerchants.value.slice(start, start + pageSize.value)
})

const totalPages = computed(() => Math.ceil(filteredMerchants.value.length / pageSize.value) || 1)

const visibleColumns = computed(() => columnConfig.value.filter(c => c.visible !== false))
const editableColumns = computed(() => tempColumnConfig.value.filter(c => c.type !== 'checkbox' && c.type !== 'actions'))

const allSelected = computed({
  get: () => pagedMerchants.value.length > 0 && pagedMerchants.value.every(m => selectedIds.value.includes(m.id)),
  set: (val) => {
    if (val) {
      selectedIds.value = [...new Set([...selectedIds.value, ...pagedMerchants.value.map(m => m.id)])]
    } else {
      const pageIds = pagedMerchants.value.map(m => m.id)
      selectedIds.value = selectedIds.value.filter(id => !pageIds.includes(id))
    }
  }
})

const selectedCount = computed(() => selectedIds.value.length)

const parsedBatchCount = computed(() => {
  const lines = batchText.value.split('\n').filter(l => l.trim() && !l.startsWith('#'))
  return lines.length
})

onMounted(() => {
  loadMerchants()
  loadColumnConfig()
})

async function loadMerchants() {
  loading.value = true
  try {
    const res = await api.get('/api/merchants')
    merchants.value = res.data || []
  } catch (e) {
    appStore.showToast('加载商户失败: ' + (e.response?.data?.error || e.message), 'error')
  } finally {
    loading.value = false
  }
}

async function loadColumnConfig() {
  try {
    const res = await api.get('/api/user-settings?key=merchant_column_config')
    if (res.data?.value && Array.isArray(res.data.value)) {
      columnConfig.value = res.data.value
    }
  } catch (e) {
    console.warn('列配置加载失败，使用默认配置')
  }
}

function openColumnSettings() {
  tempColumnConfig.value = JSON.parse(JSON.stringify(columnConfig.value))
  showColumnSettings.value = true
}

function cancelColumnSettings() {
  showColumnSettings.value = false
}

async function saveColumnConfig() {
  try {
    columnConfig.value = JSON.parse(JSON.stringify(tempColumnConfig.value))
    await api.post('/api/user-settings', { key: 'merchant_column_config', value: columnConfig.value })
    appStore.showToast('列设置已保存', 'success')
    showColumnSettings.value = false
  } catch (e) {
    appStore.showToast('保存失败', 'error')
  }
}

function resetColumnConfig() {
  tempColumnConfig.value = JSON.parse(JSON.stringify(defaultColumns))
  appStore.showToast('已恢复默认设置，请点击保存生效', 'success')
}

function addNewColumn() {
  const newKey = 'custom_' + Date.now()
  const insertIndex = tempColumnConfig.value.length - 1
  tempColumnConfig.value.splice(insertIndex, 0, {
    key: newKey,
    title: '新列',
    width: 120,
    type: 'text',
    visible: true,
    custom: true
  })
}

function deleteColumn(editableIndex) {
  const idx = getTempIndex(editableIndex)
  if (idx !== -1) {
    const col = tempColumnConfig.value[idx]
    if (col.custom) {
      tempColumnConfig.value.splice(idx, 1)
      appStore.showToast('列已标记删除，请点击保存生效', 'success')
    } else {
      appStore.showToast('系统默认列不能删除，只能隐藏', 'warning')
    }
  }
}

function handleDragStart(index) {
  draggedIndex.value = index
}

function handleDragOver(e) {
  e.preventDefault()
}

function handleDrop(dropIndex) {
  if (draggedIndex.value !== null && draggedIndex.value !== dropIndex) {
    const dragIdx = getTempIndex(draggedIndex.value)
    const dropIdx = getTempIndex(dropIndex)
    
    if (dragIdx !== -1 && dropIdx !== -1) {
      const item = tempColumnConfig.value.splice(dragIdx, 1)[0]
      tempColumnConfig.value.splice(dropIdx, 0, item)
    }
  }
  draggedIndex.value = null
}

function getTempIndex(editableIndex) {
  let count = 0
  for (let i = 0; i < tempColumnConfig.value.length; i++) {
    if (tempColumnConfig.value[i].type !== 'checkbox' && tempColumnConfig.value[i].type !== 'actions') {
      if (count === editableIndex) return i
      count++
    }
  }
  return -1
}

function getOriginalIndex(editableIndex) {
  let count = 0
  for (let i = 0; i < columnConfig.value.length; i++) {
    if (columnConfig.value[i].type !== 'checkbox' && columnConfig.value[i].type !== 'actions') {
      if (count === editableIndex) return i
      count++
    }
  }
  return -1
}

function handleDragEnd() {
  draggedIndex.value = null
}

function toggleColumnVisible(editableIndex) {
  const idx = getTempIndex(editableIndex)
  if (idx !== -1) {
    tempColumnConfig.value[idx].visible = tempColumnConfig.value[idx].visible === false ? true : false
  }
}

function updateColumnTitle(editableIndex, value) {
  const idx = getTempIndex(editableIndex)
  if (idx !== -1) tempColumnConfig.value[idx].title = value
}

function updateColumnWidth(editableIndex, value) {
  const idx = getTempIndex(editableIndex)
  if (idx !== -1) tempColumnConfig.value[idx].width = parseInt(value) || 100
}

function toggleSelect(id) {
  const idx = selectedIds.value.indexOf(id)
  if (idx > -1) selectedIds.value.splice(idx, 1)
  else selectedIds.value.push(id)
}

function parseJsonArray(str) {
  if (!str) return []
  try {
    const arr = JSON.parse(str)
    return Array.isArray(arr) ? arr : []
  } catch { return [] }
}

function parseJsonArrayToString(str) {
  return parseJsonArray(str).join('\n')
}

function toJsonArray(str) {
  if (!str) return '[]'
  const lines = str.split('\n').map(s => s.trim()).filter(s => s)
  return JSON.stringify(lines)
}

function displayMulti(str, max = 2) {
  const arr = parseJsonArray(str)
  if (arr.length === 0) return { items: [], more: 0, total: 0 }
  return { items: arr.slice(0, max), more: arr.length > max ? arr.length - max : 0, total: arr.length }
}

function openMoreModal(title, str) {
  const items = parseJsonArray(str)
  if (items.length === 0) return
  moreModalTitle.value = title || '详情'
  moreModalItems.value = items
  showMoreModal.value = true
}

function closeMoreModal() {
  showMoreModal.value = false
  moreModalItems.value = []
  moreModalTitle.value = ''
}

function openAddModal() {
  modalMode.value = 'add'
  editingMerchant.value = null
  formData.value = { project: '', env: 'PROD', website_name: '', contact_emails: '', website_urls: '', player_regions: '', estimated_players: '', game_types: '', handicaps: '', languages: '', currencies: '', supported_ports: '', wallet_types: '', callback_domains: '', whitelist_ips: '', hall_domains: '', site_domains: '', site_accounts: '', app_keys: '', game_domains: '', redirect_domains: '', remark: '', status: 'active' }
  showModal.value = true
}

function openEditModal(merchant) {
  modalMode.value = 'edit'
  editingMerchant.value = merchant
  formData.value = {
    project: merchant.project || '',
    env: merchant.env?.toUpperCase() || 'PROD',
    website_name: merchant.website_name || '',
    contact_emails: parseJsonArrayToString(merchant.contact_emails),
    website_urls: parseJsonArrayToString(merchant.website_urls),
    player_regions: parseJsonArrayToString(merchant.player_regions),
    estimated_players: merchant.estimated_players || '',
    game_types: parseJsonArrayToString(merchant.game_types),
    handicaps: parseJsonArrayToString(merchant.handicaps),
    languages: parseJsonArrayToString(merchant.languages),
    currencies: parseJsonArrayToString(merchant.currencies),
    supported_ports: parseJsonArrayToString(merchant.supported_ports),
    wallet_types: parseJsonArrayToString(merchant.wallet_types),
    callback_domains: parseJsonArrayToString(merchant.callback_domains),
    whitelist_ips: parseJsonArrayToString(merchant.whitelist_ips),
    hall_domains: parseJsonArrayToString(merchant.hall_domains),
    site_domains: parseJsonArrayToString(merchant.site_domains),
    site_accounts: parseJsonArrayToString(merchant.site_accounts),
    app_keys: parseJsonArrayToString(merchant.app_keys),
    game_domains: parseJsonArrayToString(merchant.game_domains),
    redirect_domains: parseJsonArrayToString(merchant.redirect_domains),
    remark: merchant.remark || '',
    status: merchant.status || 'active'
  }
  showModal.value = true
}

async function saveMerchant() {
  if (!formData.value.website_name) {
    appStore.showToast('请输入网站方名称', 'error')
    return
  }
  const currentUser = authStore.user?.username || ''
  const payload = {
    project: formData.value.project,
    env: formData.value.env,
    website_name: formData.value.website_name,
    contact_emails: toJsonArray(formData.value.contact_emails),
    website_urls: toJsonArray(formData.value.website_urls),
    player_regions: toJsonArray(formData.value.player_regions),
    estimated_players: formData.value.estimated_players,
    game_types: toJsonArray(formData.value.game_types),
    handicaps: toJsonArray(formData.value.handicaps),
    languages: toJsonArray(formData.value.languages),
    currencies: toJsonArray(formData.value.currencies),
    supported_ports: toJsonArray(formData.value.supported_ports),
    wallet_types: toJsonArray(formData.value.wallet_types),
    callback_domains: toJsonArray(formData.value.callback_domains),
    whitelist_ips: toJsonArray(formData.value.whitelist_ips),
    hall_domains: toJsonArray(formData.value.hall_domains),
    site_domains: toJsonArray(formData.value.site_domains),
    site_accounts: toJsonArray(formData.value.site_accounts),
    app_keys: toJsonArray(formData.value.app_keys),
    game_domains: toJsonArray(formData.value.game_domains),
    redirect_domains: toJsonArray(formData.value.redirect_domains),
    remark: formData.value.remark,
    status: formData.value.status,
    updated_by: currentUser
  }
  try {
    if (modalMode.value === 'add') {
      payload.created_by = currentUser
      await api.post('/api/merchants', payload)
      appStore.showToast('商户创建成功', 'success')
    } else {
      await api.put('/api/merchants/' + editingMerchant.value.id, payload)
      appStore.showToast('商户更新成功', 'success')
    }
    showModal.value = false
    loadMerchants()
  } catch (e) {
    appStore.showToast('保存失败: ' + (e.response?.data?.error || e.message), 'error')
  }
}

async function deleteMerchant(merchant) {
  const confirmed = await appStore.showConfirm({ type: 'danger', title: '删除商户', message: `确定要删除商户 "${merchant.website_name}" 吗？`, okText: '确认删除', cancelText: '取消' })
  if (!confirmed) return
  try {
    await api.delete('/api/merchants/' + merchant.id)
    appStore.showToast('删除成功', 'success')
    loadMerchants()
  } catch (e) {
    appStore.showToast('删除失败: ' + (e.response?.data?.error || e.message), 'error')
  }
}

function applyFilter() {
  searchQuery.value = tempSearchQuery.value
  selectedProject.value = tempSelectedProject.value
  selectedEnv.value = tempSelectedEnv.value
  currentPage.value = 1
}

function resetFilter() {
  tempSearchQuery.value = ''
  tempSelectedProject.value = ''
  tempSelectedEnv.value = ''
  searchQuery.value = ''
  selectedProject.value = ''
  selectedEnv.value = ''
  currentPage.value = 1
}

async function deleteSelected() {
  if (selectedIds.value.length === 0) return
  const confirmed = await appStore.showConfirm({ type: 'danger', title: '批量删除', message: `确定要删除选中的 ${selectedIds.value.length} 个商户吗？`, okText: '确认删除', cancelText: '取消' })
  if (!confirmed) return
  let success = 0
  for (const id of selectedIds.value) {
    try { await api.delete('/api/merchants/' + id); success++ } catch {}
  }
  appStore.showToast(`删除成功 ${success} 个`, 'success')
  selectedIds.value = []
  loadMerchants()
}

async function batchUpdateStatus(status) {
  if (selectedIds.value.length === 0) return
  const statusText = { active: '启用', inactive: '停用' }[status] || status
  const confirmed = await appStore.showConfirm({ type: 'warning', title: '批量修改状态', message: `确定要将选中的 ${selectedIds.value.length} 个商户状态改为"${statusText}"吗？`, okText: '确认', cancelText: '取消' })
  if (!confirmed) return
  let success = 0
  for (const id of selectedIds.value) {
    try {
      await api.put('/api/merchants/' + id, { status })
      success++
    } catch {}
  }
  appStore.showToast(`修改成功 ${success} 个`, 'success')
  selectedIds.value = []
  loadMerchants()
}

function openBatchModal() {
  batchText.value = ''
  batchResult.value = null
  showBatchModal.value = true
}

function fillBatchExample() {
  batchText.value = `# 项目, 环境(PROD/UAT/DEV), 网站方, 对接邮箱(;分隔), 网站方网址(;分隔), 玩家地区(;分隔), 预计玩家, 游戏种类(;分隔), 盘口(;分隔), 语言(;分隔), 币种(;分隔), 支持端口(;分隔), 钱包类型(;分隔)...
星辰项目, PROD, 星辰娱乐, contact@star.com;tech@star.com, www.star.com, 中国;菲律宾, 5000, 真人;体育;电竞, A盘;B盘, 中文;英语, CNY;USDT, H5;APP;PC, 转账钱包`
}

async function submitBatchAdd() {
  const lines = batchText.value.split('\n').filter(l => l.trim() && !l.startsWith('#'))
  if (lines.length === 0) { appStore.showToast('请输入有效数据', 'warning'); return }
  const currentUser = authStore.user?.username || ''
  batchLoading.value = true
  let success = 0, failed = 0
  const skipDetails = []
  
  for (const line of lines) {
    const parts = line.split(/[,\t|]/).map(s => s.trim())
    if (parts.length < 3 || !parts[2]) {
      failed++
      skipDetails.push(`行 "${line.slice(0, 30)}..." - 缺少必填字段(网站方)`)
      continue
    }
    const payload = {
      project: parts[0] || '',
      env: parts[1]?.toUpperCase() || 'PROD',
      website_name: parts[2] || '',
      contact_emails: JSON.stringify(parts[3] ? parts[3].split(';').map(s => s.trim()).filter(s => s) : []),
      website_urls: JSON.stringify(parts[4] ? parts[4].split(';').map(s => s.trim()).filter(s => s) : []),
      player_regions: JSON.stringify(parts[5] ? parts[5].split(';').map(s => s.trim()).filter(s => s) : []),
      estimated_players: parts[6] || '',
      game_types: JSON.stringify(parts[7] ? parts[7].split(';').map(s => s.trim()).filter(s => s) : []),
      handicaps: JSON.stringify(parts[8] ? parts[8].split(';').map(s => s.trim()).filter(s => s) : []),
      languages: JSON.stringify(parts[9] ? parts[9].split(';').map(s => s.trim()).filter(s => s) : []),
      currencies: JSON.stringify(parts[10] ? parts[10].split(';').map(s => s.trim()).filter(s => s) : []),
      supported_ports: JSON.stringify(parts[11] ? parts[11].split(';').map(s => s.trim()).filter(s => s) : []),
      wallet_types: JSON.stringify(parts[12] ? parts[12].split(';').map(s => s.trim()).filter(s => s) : []),
      callback_domains: JSON.stringify(parts[13] ? parts[13].split(';').map(s => s.trim()).filter(s => s) : []),
      whitelist_ips: JSON.stringify(parts[14] ? parts[14].split(';').map(s => s.trim()).filter(s => s) : []),
      hall_domains: JSON.stringify(parts[15] ? parts[15].split(';').map(s => s.trim()).filter(s => s) : []),
      site_domains: JSON.stringify(parts[16] ? parts[16].split(';').map(s => s.trim()).filter(s => s) : []),
      site_accounts: JSON.stringify(parts[17] ? parts[17].split(';').map(s => s.trim()).filter(s => s) : []),
      app_keys: JSON.stringify(parts[18] ? parts[18].split(';').map(s => s.trim()).filter(s => s) : []),
      game_domains: JSON.stringify(parts[19] ? parts[19].split(';').map(s => s.trim()).filter(s => s) : []),
      redirect_domains: JSON.stringify(parts[20] ? parts[20].split(';').map(s => s.trim()).filter(s => s) : []),
      status: 'active',
      created_by: currentUser,
      updated_by: currentUser
    }
    try {
      await api.post('/api/merchants', payload)
      success++
    } catch (e) {
      failed++
      skipDetails.push(`${parts[2]} - ${e.response?.data?.error || e.message}`)
    }
  }
  
  batchResult.value = {
    message: `批量添加完成：成功 ${success} 条，失败 ${failed} 条`,
    fail_count: failed,
    skip_details: skipDetails
  }
  batchLoading.value = false
  if (success > 0) loadMerchants()
}

function exportCSV() {
  const headers = ['项目', '环境', '网站方', '对接邮箱', '网站网址', '玩家地区', '在线玩家', '游戏种类', '跟单', '语言', '币种', '支持端口', '钱包类型', '状态', '备注']
  const rows = filteredMerchants.value.map(m => [
    m.project || '', m.env || '', m.website_name || '',
    parseJsonArray(m.contact_emails).join(';'),
    parseJsonArray(m.website_urls).join(';'),
    parseJsonArray(m.player_regions).join(';'),
    m.estimated_players || '',
    parseJsonArray(m.game_types).join(';'),
    parseJsonArray(m.handicaps).join(';'),
    parseJsonArray(m.languages).join(';'),
    parseJsonArray(m.currencies).join(';'),
    parseJsonArray(m.supported_ports).join(';'),
    parseJsonArray(m.wallet_types).join(';'),
    m.status === 'active' ? '运营中' : '停用',
    m.remark || ''
  ])
  const csv = [headers.join(','), ...rows.map(r => r.map(c => `"${c}"`).join(','))].join('\n')
  const blob = new Blob(['\uFEFF' + csv], { type: 'text/csv;charset=utf-8' })
  const url = URL.createObjectURL(blob)
  const a = document.createElement('a')
  a.href = url; a.download = `商户管理_${todayLocal()}.csv`; a.click()
  URL.revokeObjectURL(url)
}

function getProjectColor(project) {
  if (!project) return 'blue'
  const colors = ['blue', 'green', 'purple', 'orange']
  let hash = 0
  for (let i = 0; i < project.length; i++) hash = project.charCodeAt(i) + ((hash << 5) - hash)
  return colors[Math.abs(hash) % colors.length]
}

function getWebsiteStyle(name) {
  if (!name) return 1
  let hash = 0
  for (let i = 0; i < name.length; i++) hash = name.charCodeAt(i) + ((hash << 5) - hash)
  return (Math.abs(hash) % 4) + 1
}
</script>

<template>
  <div class="merchants-page">
    <!-- 页面头部 -->
    <div class="page-header">
      <div class="page-title-section">
        <div>
          <h1 class="page-title">商户管理</h1>
          <p class="page-subtitle">管理商户信息、域名配置、接入参数</p>
        </div>
      </div>
      <div class="header-actions">
        <button class="btn btn-secondary" @click="openColumnSettings">
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M12 3v1m0 16v1m9-9h-1M4 12H3m15.364 6.364l-.707-.707M6.343 6.343l-.707-.707m12.728 0l-.707.707M6.343 17.657l-.707.707"/><circle cx="12" cy="12" r="4"/></svg>
          列设置
        </button>
        <button class="btn btn-secondary" @click="loadMerchants" :disabled="loading">
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M4 4v5h.582m15.356 2A8.001 8.001 0 004.582 9m0 0H9m11 11v-5h-.581m0 0a8.003 8.003 0 01-15.357-2m15.357 2H15"/></svg>
          刷新
        </button>
        <button class="btn btn-secondary" @click="exportCSV">
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4M7 10l5 5 5-5M12 15V3"/></svg>
          导出
        </button>
        <button class="btn btn-primary" @click="openAddModal">
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><line x1="12" y1="5" x2="12" y2="19"/><line x1="5" y1="12" x2="19" y2="12"/></svg>
          添加商户
        </button>
        <button class="btn btn-primary" @click="openBatchModal">
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><rect x="3" y="3" width="7" height="7"/><rect x="14" y="3" width="7" height="7"/><rect x="14" y="14" width="7" height="7"/><rect x="3" y="14" width="7" height="7"/></svg>
          批量添加
        </button>
      </div>
    </div>

    <!-- 筛选栏 -->
    <div class="filter-bar">
      <div class="search-box">
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><circle cx="11" cy="11" r="8"/><path d="M21 21l-4.35-4.35"/></svg>
        <input type="text" v-model="tempSearchQuery" placeholder="搜索网站方、项目..." class="search-input" @keyup.enter="applyFilter" />
      </div>
      <select v-model="tempSelectedProject" class="filter-select">
        <option value="">所有项目</option>
        <option v-for="p in projectOptions" :key="p" :value="p">{{ p }}</option>
      </select>
      <select v-model="tempSelectedEnv" class="filter-select">
        <option value="">所有环境</option>
        <option value="PROD">PROD</option>
        <option value="UAT">UAT</option>
        <option value="DEV">DEV</option>
      </select>
      <div class="filter-actions">
        <button class="btn-search" @click="applyFilter">搜 索</button>
        <button class="btn-reset" @click="resetFilter">重 置</button>
      </div>
    </div>

    <!-- 批量操作栏 -->
    <div class="batch-action-bar" v-if="selectedIds.length > 0">
      <span class="batch-info">已选择 {{ selectedIds.length }} 项</span>
      <button class="btn btn-success btn-sm" @click="batchUpdateStatus('active')">批量启用</button>
      <button class="btn btn-warning btn-sm" @click="batchUpdateStatus('inactive')">批量停用</button>
      <button class="btn btn-danger btn-sm" @click="deleteSelected">批量删除</button>
    </div>
    
    <!-- 表格 -->
    <div class="table-container">
      <div class="table-scroll">
        <table class="data-table">
          <thead>
            <tr>
              <template v-for="col in visibleColumns" :key="col.key">
                <th v-if="col.type === 'checkbox'" :style="{ minWidth: col.width + 'px' }">
                  <div class="checkbox" :class="{ checked: allSelected }" @click="allSelected = !allSelected">
                    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="3"><polyline points="20 6 9 17 4 12"/></svg>
                  </div>
                </th>
                <th v-else-if="col.fixed === 'right'" class="th-action-fixed" :style="{ minWidth: col.width + 'px' }">{{ col.title }}</th>
                <th v-else :style="{ minWidth: col.width + 'px' }">{{ col.title }}</th>
              </template>
            </tr>
          </thead>
          <tbody>
            <tr v-for="m in pagedMerchants" :key="m.id" :class="{ selected: selectedIds.includes(m.id) }">
              <template v-for="col in visibleColumns" :key="col.key">
                <td v-if="col.type === 'checkbox'">
                  <div class="checkbox" :class="{ checked: selectedIds.includes(m.id) }" @click="toggleSelect(m.id)">
                    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="3"><polyline points="20 6 9 17 4 12"/></svg>
                  </div>
                </td>
                <td v-else-if="col.type === 'tag-project'">
                  <span class="tag-project" :class="'tag-project-' + getProjectColor(m.project)">{{ m.project || '-' }}</span>
                </td>
                <td v-else-if="col.type === 'tag-env'">
                  <span class="tag-env" :class="'tag-env-' + (m.env?.toLowerCase() || 'dev')">{{ m.env?.toUpperCase() || '-' }}</span>
                </td>
                <td v-else-if="col.type === 'tag-website'">
                  <span class="tag-website" :class="'tag-website-' + getWebsiteStyle(m.website_name)">{{ m.website_name || '-' }}</span>
                </td>
                <td v-else-if="col.type === 'multi-email'">
                  <div class="multi-list" v-if="displayMulti(m[col.key]).items?.length">
                    <span class="multi-item-email" v-for="(item, i) in displayMulti(m[col.key]).items" :key="i">{{ item }}</span>
                    <span class="multi-more" v-if="displayMulti(m[col.key]).more > 0" @click.stop="openMoreModal(col.title, m[col.key])">+{{ displayMulti(m[col.key]).more }} 更多 <span class="multi-count">{{ displayMulti(m[col.key]).total }}</span></span>
                  </div>
                  <span class="cell-single" v-else>-</span>
                </td>
                <td v-else-if="col.type === 'multi-link'">
                  <div class="multi-list" v-if="displayMulti(m[col.key]).items?.length">
                    <span class="multi-item-link" v-for="(item, i) in displayMulti(m[col.key]).items" :key="i">{{ item }}</span>
                    <span class="multi-more" v-if="displayMulti(m[col.key]).more > 0" @click.stop="openMoreModal(col.title, m[col.key])">+{{ displayMulti(m[col.key]).more }} 更多 <span class="multi-count">{{ displayMulti(m[col.key]).total }}</span></span>
                  </div>
                  <span class="cell-single" v-else>-</span>
                </td>
                <td v-else-if="col.type === 'multi-mono'">
                  <div class="multi-list" v-if="displayMulti(m[col.key]).items?.length">
                    <span class="multi-item-mono" v-for="(item, i) in displayMulti(m[col.key]).items" :key="i">{{ item }}</span>
                    <span class="multi-more" v-if="displayMulti(m[col.key]).more > 0" @click.stop="openMoreModal(col.title, m[col.key])">+{{ displayMulti(m[col.key]).more }} 更多 <span class="multi-count">{{ displayMulti(m[col.key]).total }}</span></span>
                  </div>
                  <span class="cell-single" v-else>-</span>
                </td>
                <td v-else-if="col.type === 'multi-account'">
                  <div class="multi-list" v-if="displayMulti(m[col.key]).items?.length">
                    <span class="multi-item-account" v-for="(item, i) in displayMulti(m[col.key]).items" :key="i">{{ item }}</span>
                    <span class="multi-more" v-if="displayMulti(m[col.key]).more > 0" @click.stop="openMoreModal(col.title, m[col.key])">+{{ displayMulti(m[col.key]).more }} 更多 <span class="multi-count">{{ displayMulti(m[col.key]).total }}</span></span>
                  </div>
                  <span class="cell-single" v-else>-</span>
                </td>
                <td v-else-if="col.type === 'multi-key'">
                  <div class="multi-list" v-if="displayMulti(m[col.key]).items?.length">
                    <span class="multi-item-key" v-for="(item, i) in displayMulti(m[col.key]).items" :key="i">{{ item }}</span>
                    <span class="multi-more" v-if="displayMulti(m[col.key]).more > 0" @click.stop="openMoreModal(col.title, m[col.key])">+{{ displayMulti(m[col.key]).more }} 更多 <span class="multi-count">{{ displayMulti(m[col.key]).total }}</span></span>
                  </div>
                  <span class="cell-single" v-else>-</span>
                </td>
                <td v-else-if="col.type === 'tags'">
                  <div class="cell-tags" v-if="parseJsonArray(m[col.key]).length">
                    <span class="mini-tag" v-for="(tag, i) in parseJsonArray(m[col.key]).slice(0, 4)" :key="i">{{ tag }}</span>
                    <span class="mini-tag more" v-if="parseJsonArray(m[col.key]).length > 4" @click.stop="openMoreModal(col.title, m[col.key])">+{{ parseJsonArray(m[col.key]).length - 4 }}</span>
                  </div>
                  <span class="cell-single" v-else>-</span>
                </td>
                <td v-else-if="col.type === 'status'">
                  <span class="status-tag" :class="m.status === 'active' ? 'status-active' : 'status-inactive'">{{ m.status === 'active' ? '运营中' : '停用' }}</span>
                </td>
                <td v-else-if="col.type === 'text'">
                  <span class="cell-single">{{ m[col.key] || '-' }}</span>
                </td>
                <td v-else-if="col.type === 'actions'" class="action-cell td-action-fixed">
                  <div class="action-btns">
                    <button class="action-btn" @click="openEditModal(m)" title="编辑">
                      <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M11 4H4a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2h14a2 2 0 0 0 2-2v-7"/><path d="M18.5 2.5a2.121 2.121 0 0 1 3 3L12 15l-4 1 1-4 9.5-9.5z"/></svg>
                    </button>
                    <button class="action-btn danger" @click="deleteMerchant(m)" title="删除">
                      <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><polyline points="3 6 5 6 21 6"/><path d="M19 6v14a2 2 0 0 1-2 2H7a2 2 0 0 1-2-2V6m3 0V4a2 2 0 0 1 2-2h4a2 2 0 0 1 2 2v2"/></svg>
                    </button>
                  </div>
                </td>
                <td v-else><span class="cell-single">{{ m[col.key] || '-' }}</span></td>
              </template>
            </tr>
            <tr v-if="pagedMerchants.length === 0 && !loading"><td :colspan="visibleColumns.length" class="empty">暂无数据</td></tr>
            <tr v-if="loading"><td :colspan="visibleColumns.length" class="empty">加载中...</td></tr>
          </tbody>
        </table>
      </div>
      <div class="pagination">
        <div class="pagination-info">共 {{ filteredMerchants.length }} 个商户，第 {{ currentPage }}/{{ totalPages }} 页</div>
        <div class="pagination-controls">
          <button class="page-btn" @click="currentPage = 1" :disabled="currentPage <= 1">首页</button>
          <button class="page-btn" @click="currentPage--" :disabled="currentPage <= 1">上一页</button>
          <button class="page-btn active">{{ currentPage }}</button>
          <button class="page-btn" @click="currentPage++" :disabled="currentPage >= totalPages">下一页</button>
          <button class="page-btn" @click="currentPage = totalPages" :disabled="currentPage >= totalPages">尾页</button>
        </div>
      </div>
    </div>

    <!-- 新增/编辑弹窗 -->
    <Teleport to="body">
      <div class="modal-overlay" :class="{ active: showModal }">
        <div class="modal merchant-form-modal">
          <div class="modal-header">
            <div class="modal-title">
              <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M3 9l9-7 9 7v11a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2z"/></svg>
              {{ modalMode === 'add' ? '添加商户' : '编辑商户' }}
            </div>
            <button class="modal-close" @click="showModal = false"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><line x1="18" y1="6" x2="6" y2="18"/><line x1="6" y1="6" x2="18" y2="18"/></svg></button>
          </div>
          <div class="modal-body">
            <div class="form-section">
              <div class="section-title">基本信息</div>
              <div class="form-row"><div class="form-group"><label>项目名称</label><input type="text" v-model="formData.project" placeholder="如：甲方1"></div><div class="form-group"><label>环境</label><select v-model="formData.env"><option value="PROD">PROD</option><option value="UAT">UAT</option><option value="DEV">DEV</option></select></div></div>
              <div class="form-group"><label>网站方名称 <span class="required">*</span></label><input type="text" v-model="formData.website_name" placeholder="网站方名称"></div>
              <div class="form-row"><div class="form-group"><label>对接邮箱（每行一个）</label><textarea v-model="formData.contact_emails" rows="2" placeholder="每行一个邮箱"></textarea></div><div class="form-group"><label>网站网址（每行一个）</label><textarea v-model="formData.website_urls" rows="2" placeholder="每行一个网址"></textarea></div></div>
            </div>
            <div class="form-section">
              <div class="section-title">业务信息</div>
              <div class="form-row"><div class="form-group"><label>玩家地区（每行一个）</label><textarea v-model="formData.player_regions" rows="2" placeholder="如：中国、东南亚"></textarea></div><div class="form-group"><label>预计在线玩家</label><input type="text" v-model="formData.estimated_players" placeholder="如：5000"></div></div>
              <div class="form-row"><div class="form-group"><label>游戏种类（每行一个）</label><textarea v-model="formData.game_types" rows="2" placeholder="如：真人、体育"></textarea></div><div class="form-group"><label>跟单/盘口（每行一个）</label><textarea v-model="formData.handicaps" rows="2" placeholder="如：A盘、C盘"></textarea></div></div>
              <div class="form-row"><div class="form-group"><label>语言（每行一个）</label><textarea v-model="formData.languages" rows="2" placeholder="如：中文、英文"></textarea></div><div class="form-group"><label>币种（每行一个）</label><textarea v-model="formData.currencies" rows="2" placeholder="如：CNY、USD"></textarea></div></div>
              <div class="form-row"><div class="form-group"><label>支持端口（每行一个）</label><textarea v-model="formData.supported_ports" rows="2" placeholder="如：PC、H5、APP"></textarea></div><div class="form-group"><label>钱包类型（每行一个）</label><textarea v-model="formData.wallet_types" rows="2" placeholder="如：转账、单钱包"></textarea></div></div>
            </div>
            <div class="form-section">
              <div class="section-title">技术配置</div>
              <div class="form-row"><div class="form-group"><label>三方回调域名（每行一个）</label><textarea v-model="formData.callback_domains" rows="2" placeholder="回调域名"></textarea></div><div class="form-group"><label>三方白名单IP（每行一个）</label><textarea v-model="formData.whitelist_ips" rows="2" placeholder="白名单IP"></textarea></div></div>
              <div class="form-row"><div class="form-group"><label>厅房域名（每行一个）</label><textarea v-model="formData.hall_domains" rows="2" placeholder="厅房域名"></textarea></div><div class="form-group"><label>站点域名（每行一个）</label><textarea v-model="formData.site_domains" rows="2" placeholder="站点域名"></textarea></div></div>
              <div class="form-row"><div class="form-group"><label>站点账号（每行一个）</label><textarea v-model="formData.site_accounts" rows="2" placeholder="站点账号"></textarea></div><div class="form-group"><label>AppKey（每行一个）</label><textarea v-model="formData.app_keys" rows="2" placeholder="AppKey"></textarea></div></div>
              <div class="form-row"><div class="form-group"><label>游戏域名（每行一个）</label><textarea v-model="formData.game_domains" rows="2" placeholder="游戏域名"></textarea></div><div class="form-group"><label>301域名（每行一个）</label><textarea v-model="formData.redirect_domains" rows="2" placeholder="301跳转域名"></textarea></div></div>
            </div>
            <div class="form-section">
              <div class="section-title">其他</div>
              <div class="form-group"><label>状态</label><select v-model="formData.status"><option value="active">运营中</option><option value="inactive">停用</option></select></div>
              <div class="form-group"><label>备注</label><textarea v-model="formData.remark" rows="2" placeholder="备注信息"></textarea></div>
            </div>
          </div>
          <div class="modal-footer">
            <button class="btn btn-secondary" @click="showModal = false">取消</button>
            <button class="btn btn-primary" @click="saveMerchant"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M19 21H5a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h11l5 5v11a2 2 0 0 1-2 2z"/></svg>保存</button>
          </div>
        </div>
      </div>
    </Teleport>

    <!-- 批量添加弹窗 -->
    <Teleport to="body">
      <div class="modal-overlay" :class="{ active: showBatchModal }">
        <div class="modal batch-modal">
          <div class="modal-header">
            <div class="modal-title"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><rect x="3" y="3" width="7" height="7"/><rect x="14" y="3" width="7" height="7"/><rect x="14" y="14" width="7" height="7"/><rect x="3" y="14" width="7" height="7"/></svg>批量添加商户</div>
            <button class="modal-close" @click="showBatchModal = false"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><line x1="18" y1="6" x2="6" y2="18"/><line x1="6" y1="6" x2="18" y2="18"/></svg></button>
          </div>
          <div class="modal-body">
            <div class="batch-help">
              <h4>格式说明</h4>
              <p>每行一个商户，支持 <code>,</code> 逗号、<code>Tab</code>、<code>|</code> 分隔，<code>#</code> 开头忽略。</p>
              <p><strong>字段顺序（共21个字段）:</strong></p>
              <p><span class="field-required">1.项目, 2.环境(PROD/UAT/DEV), 3.网站方</span>, 4.对接邮箱, 5.网站方网址, 6.玩家地区, 7.预计玩家, 8.游戏种类, 9.盘口, 10.语言, 11.币种, 12.支持端口, 13.钱包类型, 14.三方回调域名, 15.三方白名单, 16.厅房域名, 17.站点系统域名, 18.站点账号, 19.AppKey, 20.游戏域名, 21.301域名</p>
              <p><strong>多个值用 <code>;</code> 分号分隔</strong>，如: <code class="example-code">中文;英语;泰语</code></p>
              <p><strong>空值处理:</strong> 直接留空（连续逗号），如: <code class="example-code">项目,PROD,网站方,邮箱,,,,</code> 表示第5-8字段为空</p>
            </div>
            <div class="batch-list-header">
              <label>商户列表（{{ parsedBatchCount }} 条）</label>
              <button class="btn btn-secondary btn-xs" @click="fillBatchExample">填入示例</button>
            </div>
            <textarea v-model="batchText" rows="14" class="batch-textarea" placeholder="# 项目, 环境(PROD/UAT/DEV), 网站方, 对接邮箱(;分隔), 网站方网址(;分隔), 玩家地区(;分隔), 预计玩家, 游戏种类(;分隔), 盘口(;分隔), 语言(;分隔), 币种(;分隔), 支持端口(;分隔), 钱包类型(;分隔)...
星辰项目, PROD, 星辰娱乐, contact@star.com;tech@star.com, www.star.com, 中国;菲律宾, 5000, 真人;体育;电竞, A盘;B盘, 中文;英语, CNY;USDT, H5;APP;PC, 转账钱包"></textarea>
            <div v-if="batchResult" class="batch-result" :class="batchResult.fail_count === 0 ? 'success' : 'warning'">
              <strong>{{ batchResult.message }}</strong>
              <div v-if="batchResult.skip_details && batchResult.skip_details.length > 0" class="batch-errors">
                <div class="batch-errors-title">失败原因：</div>
                <ul>
                  <li v-for="(detail, idx) in batchResult.skip_details" :key="idx">{{ detail }}</li>
                </ul>
              </div>
            </div>
          </div>
          <div class="modal-footer">
            <button class="btn btn-secondary" @click="showBatchModal = false">取消</button>
            <button class="btn btn-primary" @click="submitBatchAdd" :disabled="batchLoading">{{ batchLoading ? '添加中...' : '确认添加' }}</button>
          </div>
        </div>
      </div>
    </Teleport>

    <!-- 更多详情弹窗 -->
    <Teleport to="body">
      <div class="modal-overlay" :class="{ active: showMoreModal }" @click.self="closeMoreModal">
        <div class="modal more-modal">
          <div class="modal-header">
            <div class="modal-title">{{ moreModalTitle }}（{{ moreModalItems.length }}）</div>
            <button class="modal-close" @click="closeMoreModal"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><line x1="18" y1="6" x2="6" y2="18"/><line x1="6" y1="6" x2="18" y2="18"/></svg></button>
          </div>
          <div class="modal-body">
            <div class="more-list">
              <div v-for="(item, idx) in moreModalItems" :key="idx" class="more-item">{{ item }}</div>
            </div>
          </div>
          <div class="modal-footer">
            <button class="btn btn-primary" @click="closeMoreModal">关闭</button>
          </div>
        </div>
      </div>
    </Teleport>

    <!-- 列设置弹窗 -->
    <Teleport to="body">
      <div class="modal-overlay" :class="{ active: showColumnSettings }" @click.self="cancelColumnSettings">
        <div class="modal column-modal">
          <div class="modal-header">
            <div class="modal-title">列设置</div>
            <button class="modal-close" @click="cancelColumnSettings"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><line x1="18" y1="6" x2="6" y2="18"/><line x1="6" y1="6" x2="18" y2="18"/></svg></button>
          </div>
          <div class="modal-body">
            <div class="column-header">
              <span class="column-hint">拖拽调整顺序，点击复选框显示/隐藏列</span>
              <button class="btn btn-primary btn-sm" @click="addNewColumn">
                <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><line x1="12" y1="5" x2="12" y2="19"/><line x1="5" y1="12" x2="19" y2="12"/></svg>
                新增列
              </button>
            </div>
            <div class="column-list">
              <div 
                v-for="(col, idx) in editableColumns" 
                :key="col.key" 
                class="column-item"
                :class="{ dragging: draggedIndex === idx }"
                draggable="true"
                @dragstart="handleDragStart(idx)"
                @dragover="handleDragOver"
                @drop="handleDrop(idx)"
                @dragend="handleDragEnd"
              >
                <div class="column-drag">
                  <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><circle cx="9" cy="5" r="1"/><circle cx="9" cy="12" r="1"/><circle cx="9" cy="19" r="1"/><circle cx="15" cy="5" r="1"/><circle cx="15" cy="12" r="1"/><circle cx="15" cy="19" r="1"/></svg>
                </div>
                <div class="column-checkbox" :class="{ checked: col.visible !== false }" @click="toggleColumnVisible(idx)">
                  <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="3"><polyline points="20 6 9 17 4 12"/></svg>
                </div>
                <div class="column-key-box">{{ col.key }}</div>
                <input type="text" class="column-title-input" :value="col.title" @input="updateColumnTitle(idx, $event.target.value)" placeholder="列标题">
                <input type="text" class="column-width-input" :value="col.width + 'px'" @change="updateColumnWidth(idx, $event.target.value.replace('px', ''))" placeholder="宽度">
                <button class="column-hide-btn" @click="toggleColumnVisible(idx)" :title="col.visible !== false ? '隐藏' : '显示'">
                  <svg v-if="col.visible !== false" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M1 12s4-8 11-8 11 8 11 8-4 8-11 8-11-8-11-8z"/><circle cx="12" cy="12" r="3"/></svg>
                  <svg v-else viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M17.94 17.94A10.07 10.07 0 0 1 12 20c-7 0-11-8-11-8a18.45 18.45 0 0 1 5.06-5.94M9.9 4.24A9.12 9.12 0 0 1 12 4c7 0 11 8 11 8a18.5 18.5 0 0 1-2.16 3.19m-6.72-1.07a3 3 0 1 1-4.24-4.24"/><line x1="1" y1="1" x2="23" y2="23"/></svg>
                </button>
                <button v-if="isAdmin" class="column-delete-btn" @click="deleteColumn(idx)" title="删除列">
                  <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><polyline points="3 6 5 6 21 6"/><path d="M19 6v14a2 2 0 0 1-2 2H7a2 2 0 0 1-2-2V6m3 0V4a2 2 0 0 1 2-2h4a2 2 0 0 1 2 2v2"/></svg>
                </button>
              </div>
            </div>
          </div>
          <div class="modal-footer">
            <button class="btn btn-secondary" @click="resetColumnConfig">恢复默认</button>
            <button class="btn btn-secondary" @click="cancelColumnSettings">取消</button>
            <button class="btn btn-primary" @click="saveColumnConfig">保存设置</button>
          </div>
        </div>
      </div>
    </Teleport>
  </div>
</template>

<style scoped>
.merchants-page { display: flex; flex-direction: column; gap: 16px; }

.page-header { display: flex; align-items: center; justify-content: space-between; padding: 20px 24px; background: var(--bg-card); border-radius: 12px; border: 1px solid var(--border-color); flex-wrap: wrap; gap: 16px; }
.page-title-section { display: flex; align-items: center; gap: 16px; }
.page-icon { width: 48px; height: 48px; border-radius: 12px; background: linear-gradient(135deg, #ff9c01 0%, #fbbf24 100%); display: flex; align-items: center; justify-content: center; }
.page-icon svg { width: 24px; height: 24px; color: #000; }
.page-title { font-size: 24px; font-weight: 600; color: var(--text-primary); margin: 0; }
.page-subtitle { font-size: 14px; color: var(--text-secondary); margin-top: 4px; }
.header-actions { display: flex; gap: 12px; flex-wrap: wrap; }

.btn { display: inline-flex; align-items: center; gap: 8px; padding: 10px 18px; border-radius: 8px; font-size: 14px; font-weight: 500; border: none; cursor: pointer; transition: all 0.2s; white-space: nowrap; position: relative; z-index: 1; }
.btn svg { width: 16px; height: 16px; pointer-events: none; }
.btn * { pointer-events: none; }
.btn-primary { background: #3a84ff; color: #fff; }
.btn-primary:hover { background: #2b6ee6; }
.btn-secondary { background: var(--bg-hover); color: var(--text-primary); border: 1px solid var(--border-color); }
.btn-secondary:hover { background: var(--bg-input); border-color: var(--primary); }
.btn-danger { background: #ea3636; color: #fff; margin-left: auto; }
.btn-danger:disabled { opacity: 0.5; cursor: not-allowed; }
.btn-sm { padding: 8px 14px; font-size: 13px; }
.btn-xs { padding: 6px 12px; font-size: 12px; }

.filter-bar { display: flex; gap: 12px; padding: 16px; background: var(--bg-card); border-radius: 12px; border: 1px solid var(--border-color); flex-wrap: wrap; align-items: center; }
.search-box { flex: 1; min-width: 240px; position: relative; }
.search-box svg { position: absolute; left: 14px; top: 50%; transform: translateY(-50%); width: 18px; height: 18px; color: var(--text-muted); }
.search-input { width: 100%; padding: 10px 14px 10px 42px; border-radius: 8px; border: 1px solid var(--border-color); background: var(--bg-input); color: var(--text-primary); font-size: 14px; }
.search-input:focus { outline: none; border-color: #3a84ff; }
.filter-select { padding: 10px 36px 10px 14px; border-radius: 8px; border: 1px solid var(--border-color); background: var(--bg-input); color: var(--text-primary); font-size: 14px; cursor: pointer; appearance: none; background-image: url("data:image/svg+xml,%3Csvg xmlns='http://www.w3.org/2000/svg' width='16' height='16' viewBox='0 0 24 24' fill='none' stroke='%238b949e' stroke-width='2'%3E%3Cpath d='M6 9l6 6 6-6'/%3E%3C/svg%3E"); background-repeat: no-repeat; background-position: right 10px center; }
.filter-select:focus { outline: none; border-color: #3a84ff; }

.filter-actions { display: flex; gap: 8px; }
.btn-search { padding: 10px 24px; border-radius: 8px; border: none; background: #3a84ff; color: #fff; font-size: 14px; font-weight: 500; cursor: pointer; transition: all 0.2s; }
.btn-search:hover { background: #2970e6; }
.btn-reset { padding: 10px 24px; border-radius: 8px; border: 1px solid var(--border-color); background: var(--bg-input); color: var(--text-primary); font-size: 14px; cursor: pointer; transition: all 0.2s; }
.btn-reset:hover { background: var(--bg-hover); }

.batch-action-bar { display: flex; align-items: center; gap: 12px; padding: 14px 20px; background: linear-gradient(135deg, rgba(59, 130, 246, 0.12) 0%, rgba(99, 102, 241, 0.12) 100%); border: 1px solid rgba(59, 130, 246, 0.3); border-radius: 10px; }
.batch-info { font-size: 14px; font-weight: 600; color: #3b82f6; margin-right: 8px; }

.table-container { background: var(--bg-card); border-radius: 12px; border: 1px solid var(--border-color); overflow: hidden; }
.table-scroll { overflow-x: auto; }
.data-table { width: 100%; border-collapse: collapse; font-size: 13px; }
.data-table th { padding: 14px 16px; text-align: left; font-weight: 500; color: var(--text-secondary); background: var(--bg-hover); border-bottom: 1px solid var(--border-color); white-space: nowrap; position: relative; z-index: 1; font-size: 13px; overflow: hidden; text-overflow: ellipsis; }
.data-table td { padding: 14px 16px; border-bottom: 1px solid var(--border-color); vertical-align: top; color: var(--text-primary); font-size: 13px; overflow: hidden; text-overflow: ellipsis; max-width: 220px; position: relative; z-index: 1; }
.data-table tbody tr { transition: background 0.15s; }
.data-table tbody tr:hover { background: var(--bg-hover); }
.data-table tbody tr.selected { background: rgba(58, 132, 255, 0.08); }

/* 固定操作列 - 必须使用完全不透明的实心背景色 */
.th-action-fixed,
.td-action-fixed {
  position: sticky !important;
  right: 0 !important;
  min-width: 90px;
  width: 90px;
  text-align: center !important;
  white-space: nowrap;
}

/* 深色模式 - 使用实心颜色 */
.th-action-fixed {
  z-index: 100 !important;
  background: #252d3d !important;
  box-shadow: -8px 0 12px -4px rgba(0, 0, 0, 0.3);
}

.td-action-fixed {
  z-index: 50 !important;
  background: #1a1f2e !important;
  box-shadow: -8px 0 12px -4px rgba(0, 0, 0, 0.2);
}

.data-table tbody tr:hover .td-action-fixed {
  background: #252d3d !important;
}

.data-table tbody tr.selected .td-action-fixed {
  background: #1e3a5f !important;
}

/* 亮色模式 */
.light-mode .th-action-fixed {
  background: #f0f2f5 !important;
}

.light-mode .td-action-fixed {
  background: #ffffff !important;
}

.light-mode .data-table tbody tr:hover .td-action-fixed {
  background: #f0f2f5 !important;
}

.light-mode .data-table tbody tr.selected .td-action-fixed {
  background: #dbeafe !important;
}

.action-cell { min-width: 90px; }

.checkbox { width: 18px; height: 18px; border-radius: 4px; border: 2px solid var(--border-color); background: var(--bg-input); cursor: pointer; display: flex; align-items: center; justify-content: center; transition: all 0.15s; }
.checkbox.checked { background: #3a84ff; border-color: #3a84ff; }
.checkbox svg { width: 12px; height: 12px; color: #fff; opacity: 0; }
.checkbox.checked svg { opacity: 1; }

.tag-project { display: inline-flex; padding: 5px 12px; border-radius: 4px; font-size: 12px; font-weight: 600; white-space: nowrap; }
.tag-project-blue { background: rgba(58, 132, 255, 0.15); color: #3a84ff; }
.tag-project-green { background: rgba(45, 203, 86, 0.15); color: #2dcb56; }
.tag-project-purple { background: rgba(168, 85, 247, 0.15); color: #a855f7; }
.tag-project-orange { background: rgba(255, 156, 1, 0.15); color: #ff9c01; }

.tag-env { display: inline-flex; padding: 5px 12px; border-radius: 4px; font-size: 12px; font-weight: 600; white-space: nowrap; }
.tag-env-prod { background: rgba(234, 54, 54, 0.15); color: #ea3636; }
.tag-env-uat { background: rgba(255, 156, 1, 0.15); color: #ff9c01; }
.tag-env-dev { background: rgba(45, 203, 86, 0.15); color: #2dcb56; }

.tag-website { display: inline-flex; padding: 5px 12px; border-radius: 4px; font-size: 12px; font-weight: 600; white-space: nowrap; }
.tag-website-1 { background: rgba(236, 72, 153, 0.15); color: #ec4899; }
.tag-website-2 { background: rgba(34, 211, 238, 0.15); color: #22d3ee; }
.tag-website-3 { background: rgba(163, 230, 53, 0.15); color: #84cc16; }
.tag-website-4 { background: rgba(251, 146, 60, 0.15); color: #fb923c; }

.status-tag { display: inline-flex; padding: 5px 12px; border-radius: 4px; font-size: 12px; font-weight: 600; white-space: nowrap; }
.status-active { background: rgba(45, 203, 86, 0.15); color: #2dcb56; }
.status-inactive { background: rgba(234, 54, 54, 0.15); color: #ea3636; }

.multi-list { display: flex; flex-direction: column; gap: 4px; max-width: 200px; }
.multi-item-email { font-size: 12px; color: #22d3ee; padding: 4px 10px; background: rgba(34, 211, 238, 0.1); border-radius: 4px; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
.multi-item-link { font-family: 'Monaco', 'Consolas', monospace; font-size: 11px; color: #3a84ff; padding: 4px 10px; background: rgba(58, 132, 255, 0.1); border-radius: 4px; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
.multi-item-mono { font-family: 'Monaco', 'Consolas', monospace; font-size: 11px; color: var(--text-muted); padding: 4px 10px; background: var(--bg-hover); border-radius: 4px; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
.multi-item-account { font-size: 12px; color: #ec4899; padding: 4px 10px; background: rgba(236, 72, 153, 0.1); border-radius: 4px; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
.multi-item-key { font-family: 'Monaco', 'Consolas', monospace; font-size: 10px; color: #84cc16; padding: 4px 10px; background: rgba(163, 230, 53, 0.1); border-radius: 4px; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
.multi-more { font-size: 11px; color: var(--text-muted); padding: 4px 10px; cursor: pointer; display: inline-flex; align-items: center; gap: 6px; }
.multi-more:hover { color: #3a84ff; }
.multi-count { display: inline-flex; align-items: center; justify-content: center; min-width: 18px; height: 18px; padding: 0 5px; background: #3a84ff; color: #fff; border-radius: 9px; font-size: 10px; font-weight: 600; }

.cell-tags { display: flex; flex-wrap: wrap; gap: 4px; max-width: 140px; }
.mini-tag { display: inline-flex; padding: 3px 8px; border-radius: 3px; font-size: 11px; background: var(--bg-hover); color: var(--text-secondary); border: 1px solid var(--border-color); white-space: nowrap; }
.mini-tag.more { background: rgba(58, 132, 255, 0.15); color: #3a84ff; border-color: transparent; }
.mini-tag.more { cursor: pointer; }

.cell-single { font-size: 13px; color: var(--text-secondary); }

.action-btns { display: flex; gap: 6px; }
.action-btn { width: 30px; height: 30px; border-radius: 6px; border: 1px solid var(--border-color); background: var(--bg-card); color: var(--text-secondary); display: flex; align-items: center; justify-content: center; cursor: pointer; transition: all 0.15s; }
.action-btn:hover { background: #3a84ff; border-color: #3a84ff; color: #fff; }
.action-btn.danger:hover { background: #ea3636; border-color: #ea3636; }
.action-btn svg { width: 14px; height: 14px; }

.empty { text-align: center; color: var(--text-muted); padding: 40px !important; }

.pagination { display: flex; align-items: center; justify-content: space-between; padding: 16px 20px; border-top: 1px solid var(--border-color); }
.pagination-info { font-size: 14px; color: var(--text-secondary); }
.pagination-controls { display: flex; align-items: center; gap: 8px; }
.page-btn { min-width: 36px; height: 36px; padding: 0 12px; border-radius: 6px; border: 1px solid var(--border-color); background: var(--bg-input); color: var(--text-primary); font-size: 14px; cursor: pointer; display: inline-flex; align-items: center; justify-content: center; transition: all 0.15s; white-space: nowrap; }
.page-btn:hover:not(:disabled) { border-color: #3a84ff; color: #3a84ff; }
.page-btn.active { background: #3a84ff; border-color: #3a84ff; color: #fff; }
.page-btn:disabled { opacity: 0.5; cursor: not-allowed; }
</style>

<style>
/* 弹窗基础样式由全局 base.css 控制 display 属性 */
.modal-overlay.active { display: flex !important; }
.modal { background: var(--bg-card); border-radius: 16px; border: 1px solid var(--border-color); width: 600px; max-width: 90vw; max-height: 85vh; display: flex; flex-direction: column; box-shadow: 0 20px 40px rgba(0, 0, 0, 0.3); }
.merchant-form-modal { width: 720px; }
.column-modal { width: 700px; }
.batch-modal { width: 900px; }
.more-modal { width: 520px; }
.modal-header { display: flex; align-items: center; justify-content: space-between; padding: 20px 24px; border-bottom: 1px solid var(--border-color); }
.modal-title { font-size: 18px; font-weight: 600; display: flex; align-items: center; gap: 10px; color: var(--text-primary); }
.modal-title svg { width: 22px; height: 22px; color: #3a84ff; }
.modal-close { width: 32px; height: 32px; border-radius: 8px; border: none; background: var(--bg-hover); color: var(--text-secondary); cursor: pointer; display: flex; align-items: center; justify-content: center; transition: all 0.15s; }
.modal-close:hover { background: #ea3636; color: #fff; }
.modal-close svg { width: 18px; height: 18px; }
.modal-body { padding: 20px 24px; overflow-y: auto; flex: 1; }
.modal-footer { display: flex; justify-content: flex-end; gap: 12px; padding: 16px 24px; border-top: 1px solid var(--border-color); }

.form-section { margin-bottom: 20px; padding-bottom: 16px; border-bottom: 1px solid var(--border-color); }
.form-section:last-child { border-bottom: none; margin-bottom: 0; }
.section-title { font-size: 14px; font-weight: 600; color: var(--text-secondary); margin-bottom: 16px; display: flex; align-items: center; gap: 8px; }
.section-title::before { content: ''; width: 3px; height: 14px; background: #3a84ff; border-radius: 2px; }
.form-row { display: grid; grid-template-columns: 1fr 1fr; gap: 16px; }
.form-group { display: flex; flex-direction: column; gap: 8px; margin-bottom: 12px; }
.form-group label { font-size: 13px; font-weight: 500; color: var(--text-secondary); }
.form-group .required { color: #ea3636; }
.form-group input, .form-group select, .form-group textarea { padding: 10px 14px; background: var(--bg-input); border: 1px solid var(--border-color); border-radius: 8px; color: var(--text-primary); font-size: 14px; font-family: inherit; transition: border-color 0.2s; }
.form-group input:focus, .form-group select:focus, .form-group textarea:focus { outline: none; border-color: #3a84ff; }
.form-group textarea { resize: vertical; min-height: 60px; }

.column-header { display: flex; align-items: center; justify-content: space-between; margin-bottom: 16px; }
.column-hint { font-size: 13px; color: var(--text-muted); }
.column-list { display: flex; flex-direction: column; gap: 8px; }
.column-item { display: flex; align-items: center; gap: 12px; padding: 12px 16px; background: var(--bg-hover); border-radius: 8px; border: 1px solid var(--border-color); transition: all 0.15s; cursor: grab; }
.column-item:hover { border-color: #3a84ff; }
.column-item.dragging { opacity: 0.5; border-style: dashed; }
.column-drag { cursor: grab; color: var(--text-muted); display: flex; align-items: center; }
.column-drag:active { cursor: grabbing; }
.column-drag svg { width: 16px; height: 16px; }
.column-checkbox { width: 20px; height: 20px; border-radius: 4px; border: 2px solid var(--border-color); background: var(--bg-input); cursor: pointer; display: flex; align-items: center; justify-content: center; transition: all 0.15s; flex-shrink: 0; }
.column-checkbox.checked { background: #3a84ff; border-color: #3a84ff; }
.column-checkbox svg { width: 12px; height: 12px; color: #fff; opacity: 0; }
.column-checkbox.checked svg { opacity: 1; }
.column-key-box { padding: 4px 10px; background: var(--bg-input); border: 1px solid var(--border-color); border-radius: 4px; font-size: 12px; color: var(--text-muted); font-family: monospace; min-width: 120px; }
.column-title-input { flex: 1; padding: 8px 12px; border-radius: 6px; border: 1px solid var(--border-color); background: var(--bg-input); color: var(--text-primary); font-size: 14px; }
.column-title-input:focus { outline: none; border-color: #3a84ff; }
.column-width-input { width: 80px; padding: 8px 12px; border-radius: 6px; border: 1px solid var(--border-color); background: var(--bg-input); color: var(--text-primary); font-size: 13px; text-align: center; }
.column-width-input:focus { outline: none; border-color: #3a84ff; }
.column-hide-btn { width: 32px; height: 32px; border-radius: 6px; border: none; background: transparent; color: var(--text-muted); cursor: pointer; display: flex; align-items: center; justify-content: center; transition: all 0.15s; }
.column-hide-btn:hover { background: var(--bg-input); color: var(--text-primary); }
.column-hide-btn svg { width: 16px; height: 16px; }
.column-delete-btn { width: 32px; height: 32px; border-radius: 6px; border: none; background: transparent; color: var(--text-muted); cursor: pointer; display: flex; align-items: center; justify-content: center; transition: all 0.15s; }
.column-delete-btn:hover { background: rgba(234, 54, 54, 0.1); color: #ea3636; }
.column-delete-btn svg { width: 16px; height: 16px; }

.batch-help { background: var(--bg-hover); border: 1px solid var(--border-color); border-radius: 8px; padding: 16px; margin-bottom: 16px; }
.batch-help h4 { margin: 0 0 12px 0; font-size: 14px; color: var(--text-primary); font-weight: 600; }
.batch-help p { margin: 8px 0; font-size: 13px; color: var(--text-secondary); line-height: 1.8; }
.batch-help code { background: #3a84ff; color: #fff; padding: 2px 6px; border-radius: 3px; font-size: 12px; margin: 0 2px; }
.batch-help .field-required { color: #3a84ff; font-weight: 600; }
.batch-help .example-code { background: #2dcb56; }
.batch-list-header { display: flex; align-items: center; justify-content: space-between; margin-bottom: 12px; }
.batch-list-header label { font-size: 14px; font-weight: 500; color: var(--text-primary); }
.batch-textarea { width: 100%; padding: 14px; background: var(--bg-input); border: 1px solid var(--border-color); border-radius: 8px; color: var(--text-primary); font-size: 12px; font-family: 'Monaco', 'Consolas', monospace; resize: vertical; line-height: 1.6; }
.batch-textarea:focus { outline: none; border-color: #3a84ff; }
.batch-result { margin-top: 16px; padding: 14px 18px; border-radius: 8px; font-size: 13px; }
.batch-result.success { background: rgba(45, 203, 86, 0.1); border: 1px solid rgba(45, 203, 86, 0.3); color: #2dcb56; }
.batch-result.warning { background: rgba(255, 156, 1, 0.1); border: 1px solid rgba(255, 156, 1, 0.3); color: #ff9c01; }
.batch-errors { margin-top: 10px; padding-top: 10px; border-top: 1px solid rgba(0, 0, 0, 0.1); }
.batch-errors-title { font-weight: 600; margin-bottom: 6px; color: #d97706; }
.batch-errors ul { margin: 0; padding-left: 20px; font-size: 12px; max-height: 100px; overflow-y: auto; }
.batch-errors li { margin-bottom: 4px; color: var(--text-secondary); }

.more-list { display: flex; flex-direction: column; gap: 8px; max-height: 420px; overflow-y: auto; }
.more-item { padding: 10px 12px; border-radius: 8px; background: var(--bg-input); border: 1px solid var(--border-color); color: var(--text-primary); font-size: 13px; word-break: break-all; }
</style>
