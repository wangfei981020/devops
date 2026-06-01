<script setup>
import { ref, computed, onMounted, onUnmounted, watch } from 'vue'
import { useRouter } from 'vue-router'
import { useAppStore, useAuthStore } from '@/stores'
import api from '@/api'
import { getTableHierarchy, clearHierarchyCache } from '@/api/tableHierarchy'

const appStore = useAppStore()
const authStore = useAuthStore()
const router = useRouter()

// 翻译函数 - 包装以确保响应式
const t = (key, params) => appStore.t(key, params)

// 当前语言（用于触发响应式更新）
const currentLanguage = computed(() => appStore.language)

const isSuperAdmin = computed(() => authStore.isSuperAdmin())
const canCreate = computed(() => isSuperAdmin.value || authStore.hasPermission('table_maintenance:create'))
const canExport = computed(() => isSuperAdmin.value || authStore.hasPermission('table_maintenance:export'))
const canUpload = computed(() => isSuperAdmin.value || authStore.hasPermission('table_maintenance:upload'))

// 行级权限：API Key 创建的记录需要专属权限码才能改/删
function rowEditable(row) {
  if (isSuperAdmin.value) return true
  if (row && row.source_api_key_id) {
    return authStore.hasPermission('table_maintenance:edit_api_record')
  }
  return authStore.hasPermission('table_maintenance:update')
}
function rowDeletable(row) {
  if (isSuperAdmin.value) return true
  if (row && row.source_api_key_id) {
    return authStore.hasPermission('table_maintenance:delete_api_record')
  }
  return authStore.hasPermission('table_maintenance:delete')
}
function rowLocked(row) {
  return !!(row && row.source_api_key_id) && !rowEditable(row)
}

// 配置选项（项目、现场、桌台、维护类型）- 从全局配置加载
const projectOptions = ref([])  // 项目列表
const siteOptions = ref([])  // 现场列表
const tableOptions = ref([]) // 桌台列表，每个包含 { name, site }
const gameTypeData = ref([])  // 游戏类型数据 { name_zh, name_en }

// 维护类型 - 使用翻译
const maintTypeKeys = ['none', 'emergency', 'temporary', 'routine']
const maintTypeOptions = computed(() => maintTypeKeys.map(k => t(`tableMaintenance.options.maintenanceType.${k}`)))

// 预定义的游戏类型（用于兼容旧数据）
const predefinedGameTypeKeys = ['baccarat', 'dragonTiger', 'roulette', 'sicBo', 'other']
const predefinedGameTypeMap = {
  '百家乐': 'baccarat',
  '龙虎': 'dragonTiger',
  '轮盘': 'roulette',
  '骰宝': 'sicBo',
  '其他': 'other'
}

// 获取游戏类型的 key
function getGameTypeKey(gt) {
  if (!gt) return ''
  if (typeof gt === 'string') return gt
  return gt.name_zh || gt.name || ''
}

// 根据当前语言获取游戏类型显示名称
function getGameTypeName(gt) {
  if (!gt) return ''
  if (typeof gt === 'string') {
    // 检查是否是预定义的键
    if (predefinedGameTypeKeys.includes(gt)) {
      return t(`tableHierarchy.gameTypes.${gt}`)
    }
    // 检查是否是旧的中文值
    const mappedKey = predefinedGameTypeMap[gt]
    if (mappedKey) {
      return t(`tableHierarchy.gameTypes.${mappedKey}`)
    }
    return gt
  }
  // 对象格式 { name_zh, name_en }
  const lang = appStore.language
  if (lang === 'en-US') {
    return gt.name_en || gt.name_zh || gt.name || ''
  } else {
    return gt.name_zh || gt.name || gt.name_en || ''
  }
}

// 翻译游戏类型（支持键或中文值）
function translateGameType(gameType) {
  // 先在自定义游戏类型中查找
  const customGt = gameTypeData.value.find(gt => getGameTypeKey(gt) === gameType)
  if (customGt) {
    return getGameTypeName(customGt)
  }
  // 检查是否是预定义的键
  if (predefinedGameTypeKeys.includes(gameType)) {
    return t(`tableHierarchy.gameTypes.${gameType}`)
  }
  // 检查是否是旧的中文值
  const mappedKey = predefinedGameTypeMap[gameType]
  if (mappedKey) {
    return t(`tableHierarchy.gameTypes.${mappedKey}`)
  }
  // 未知值，原样返回
  return gameType
}

// v604: 本地时区今天日期（避免 toISOString 在凌晨 UTC 早 8 小时跨天）
function todayLocal() {
  const n = new Date()
  const p = x => String(x).padStart(2, '0')
  return `${n.getFullYear()}-${p(n.getMonth() + 1)}-${p(n.getDate())}`
}

const columns = computed(() => [
  { key: 'checkbox', title: '', width: 40, type: 'checkbox' },
  { key: 'date', title: t('tableMaintenance.columns.date'), width: 110, type: 'date' },
  { key: 'start_time', title: t('tableMaintenance.columns.startTime'), width: 150, type: 'datetime' },
  { key: 'notify_start_screenshot', title: t('tableMaintenance.columns.notifyStartScreenshot'), width: 120, type: 'attachments' },
  { key: 'start_duration', title: t('tableMaintenance.columns.startDuration'), width: 130, type: 'duration-select', required: true, options: [t('tableMaintenance.options.duration.twoMin'), t('tableMaintenance.options.duration.fiveMin'), t('tableMaintenance.options.duration.tenMin'), t('tableMaintenance.options.duration.overTenMin')] },
  { key: 'end_time', title: t('tableMaintenance.columns.endTime'), width: 150, type: 'datetime' },
  { key: 'notify_end_screenshot', title: t('tableMaintenance.columns.notifyEndScreenshot'), width: 100, type: 'attachments' },
  { key: 'close_duration', title: t('tableMaintenance.columns.closeDuration'), width: 130, type: 'duration-select', options: [t('tableMaintenance.options.duration.twoMin'), t('tableMaintenance.options.duration.fiveMin'), t('tableMaintenance.options.duration.tenMin'), t('tableMaintenance.options.duration.overTenMin')] },
  { key: 'affected_projects', title: t('tableMaintenance.columns.affectedProjects'), width: 150, type: 'multi-select-projects', required: true },
  { key: 'affected_sites', title: t('tableMaintenance.columns.site'), width: 120, type: 'multi-select-sites' },
  { key: 'affected_tables', title: t('tableMaintenance.columns.table'), width: 120, type: 'multi-select-tables' },
  { key: 'table_status', title: t('tableMaintenance.columns.tableStatus'), width: 100, type: 'table-status-display' },
  { key: 'game_types', title: t('tableMaintenance.columns.gameType'), width: 120, type: 'multi-select-game-types' },
  { key: 'maintenance_type', title: t('tableMaintenance.columns.maintenanceType'), width: 90, type: 'maint-type-select' },
  { key: 'reason', title: t('tableMaintenance.columns.reason'), width: 150, type: 'textarea' },
  { key: 'affect_settlement', title: t('tableMaintenance.columns.affectSettlement'), width: 80, type: 'select', options: [t('tableMaintenance.options.yesNo.yes'), t('tableMaintenance.options.yesNo.no')], default: t('tableMaintenance.options.yesNo.no') },
  { key: 'affected_round_ids', title: t('tableMaintenance.columns.affectedRoundIds'), width: 120, type: 'text' },
  { key: 'operation', title: t('tableMaintenance.columns.operation'), width: 100, type: 'select', required: true, options: [t('tableMaintenance.options.operation.maintenance'), t('tableMaintenance.options.operation.cancel'), t('tableMaintenance.options.operation.recalculate'), t('tableMaintenance.options.operation.repayout'), t('tableMaintenance.options.operation.vipTable'), t('tableMaintenance.options.operation.missed'), t('tableMaintenance.options.operation.missedScreenshot')] },
  { key: 'operator', title: t('tableMaintenance.columns.operator'), width: 90, type: 'text', required: true },
  { key: 'inspector', title: t('tableMaintenance.columns.inspector'), width: 80, type: 'text' },
  { key: 'qc_status', title: t('tableMaintenance.columns.qcStatus'), width: 90, type: 'qc-status', options: [t('tableMaintenance.options.qcStatus.normal'), t('tableMaintenance.options.qcStatus.abnormal')] },
  { key: 'remark', title: t('tableMaintenance.columns.remark'), width: 150, type: 'textarea' },
  { key: 'created_by', title: t('tableMaintenance.columns.createdBy'), width: 80, type: 'readonly' },
  { key: 'created_at', title: t('tableMaintenance.columns.createdAt'), width: 150, type: 'datetime-readonly' },
  { key: 'updated_at', title: t('tableMaintenance.columns.updatedAt'), width: 150, type: 'datetime-readonly' },
  { key: 'actions', title: t('tableMaintenance.columns.actions'), width: 100, type: 'actions' }
])

const visibleColumns = computed(() => columns.value)

// i18n 感知的比较辅助（数据库可能存中文或英文值，统计时两种都要匹配）
const OP_LABELS = {
  maintenance: ['维护', 'Maintenance'],
  cancel: ['取消', 'Cancel'],
  recalculate: ['重算', 'Recalculate'],
  repayout: ['重派彩', 'Re-payout'],
  vipTable: ['包桌T人', 'VIP Table'],
  missed: ['漏操作', 'Missed Op'],
  missedScreenshot: ['漏截图', 'Missed Screenshot'],
}
const DUR_LABELS = {
  twoMin: ['两分钟内', 'Within 2 min'],
  fiveMin: ['五分钟内', 'Within 5 min'],
  tenMin: ['十分钟内', 'Within 10 min'],
  overTenMin: ['十分钟以上', 'Over 10 min'],
}
const MAINT_LABELS = {
  none: ['无', 'None'],
  emergency: ['紧急维护', 'Emergency'],
  temporary: ['临时维护', 'Temporary'],
  routine: ['例行维护', 'Routine'],
}
const QC_LABELS = {
  normal: ['正常', 'Normal'],
  abnormal: ['异常', 'Abnormal'],
}

function isQc(val, key) { return QC_LABELS[key]?.includes(val) }
function isOp(val, key) { return OP_LABELS[key]?.includes(val) }
function isDur(val, key) { return DUR_LABELS[key]?.includes(val) }
function isMaintType(val, key) { return MAINT_LABELS[key]?.includes(val) }

// 显示归一化：无论存的是中文还是英文，都按当前界面语言展示对应 label
function _normalizeBy(labels, prefix, val) {
  if (!val) return val || ''
  for (const key of Object.keys(labels)) {
    if (labels[key].includes(val)) return t(`${prefix}.${key}`)
  }
  return val // 值不在枚举里就原样返回
}
function displayOp(val) { return _normalizeBy(OP_LABELS, 'tableMaintenance.options.operation', val) }
function displayDur(val) { return _normalizeBy(DUR_LABELS, 'tableMaintenance.options.duration', val) }
function displayMaintType(val) { return _normalizeBy(MAINT_LABELS, 'tableMaintenance.options.maintenanceType', val) }
function displayQc(val) { return _normalizeBy(QC_LABELS, 'tableMaintenance.options.qcStatus', val) }
// 获取时长排序索引（兼容中英文）
function getDurIndex(val) {
  const keys = ['twoMin', 'fiveMin', 'tenMin', 'overTenMin']
  for (let i = 0; i < keys.length; i++) {
    if (isDur(val, keys[i])) return i
  }
  return -1
}
// 获取时长的固定 key（用于统计 map）
function getDurKey(val) {
  const keys = ['twoMin', 'fiveMin', 'tenMin', 'overTenMin']
  for (const k of keys) {
    if (isDur(val, k)) return k
  }
  return null
}

const records = ref([])
const loading = ref(false)
const stats = ref({ total_rows: 0, today_rows: 0, week_rows: 0, month_rows: 0 })
const tableId = ref('')

// 分页相关
const currentPage = ref(1)
const savedPageSize = localStorage.getItem('table_maintenance_page_size')
const pageSize = ref(savedPageSize ? parseInt(savedPageSize) : 10)
const pageSizeOptions = [10, 20, 50, 100]

// 监听 pageSize 变化，保存到 localStorage
function onPageSizeChange(newSize) {
  pageSize.value = newSize
  currentPage.value = 1
  localStorage.setItem('table_maintenance_page_size', String(newSize))
}

// 搜索输入（用户输入但未应用）
const filterInput = ref({ search: '', status: '', dateStart: '', dateEnd: '', project: '', duration: '', operator: '', operation: '', tableStatus: '', maintType: '' })
// 已应用的搜索条件（点击搜索按钮后生效）
const searchQuery = ref('')
const selectedStatus = ref('')
const dateRange = ref({ start: '', end: '' })
const selectedProject = ref('')
const selectedDuration = ref('')
const selectedOperator = ref('')
const selectedOperation = ref('')
const selectedTableStatus = ref('')
const selectedMaintType = ref('')

const selectedIds = ref([])

const showModal = ref(false)
const modalMode = ref('add')
const formData = ref({})
const currentAttachments = ref([])
const uploading = ref(false)
const fileInput = ref(null)
const dragOver = ref(false)
const attachmentInputRefs = ref({})

const showDetailModal = ref(false)
const detailRecord = ref(null)

const showImagePreview = ref(false)
const previewImages = ref([])
const previewIndex = ref(0)

const presignedUrlCache = ref({})

const activeTab = ref('list') // 'list' 或 'stats'

// 统计面板的筛选条件（输入值）
const statsDateInput = ref({ start: '', end: '' })
const statsProjectInput = ref('')  // 项目筛选
const statsDurationInput = ref('')  // 时长筛选
const statsOperatorInput = ref('')  // 操作人筛选
const statsOperationInput = ref('')  // 操作类型筛选
const statsRoundIdInput = ref('')  // 局号筛选
const statsMaintTypeInput = ref('')  // 维护类型筛选
// 统计面板的筛选条件（已应用的值）
const statsDateRange = ref({ start: '', end: '' })
const statsProjectFilter = ref('')
const statsDurationFilter = ref('')
const statsOperatorFilter = ref('')
const statsOperationFilter = ref('')
const statsRoundIdFilter = ref('')
const statsMaintTypeFilter = ref('')
// 忽略创建人（统计 / 统计导出生效，不影响列表）
const statsExcludeCreatorsInput = ref([])
const statsExcludeApiKeyInput = ref(false)
const statsExcludeCreatorsFilter = ref([])
const statsExcludeApiKeyFilter = ref(false)
const showExcludeCreatorsDropdown = ref(false)
// 桌台状态筛选（启用/关闭/未接入），跟列表页 tableStatus 筛选语义一致
const statsTableStatusInput = ref('')
const statsTableStatusFilter = ref('')

// 取记录的有效日期：优先 date，否则从 start_time 提取日期部分
function getEffectiveDate(r) {
  if (r.date) return r.date
  if (r.start_time) return r.start_time.split(/[ T]/)[0] || ''
  return ''
}

// 计算记录的"代表时长"索引：维护取 max(start,close)，其他操作取 start
function getRecordDurationIndex(r) {
  if (isOp(r.operation, 'maintenance')) {
    return Math.max(getDurIndex(r.start_duration), getDurIndex(r.close_duration))
  }
  return getDurIndex(r.start_duration)
}

// 根据筛选条件过滤的记录
const statsFilteredRecords = computed(() => {
  let data = records.value
  if (statsDateRange.value.start) {
    data = data.filter(r => {
      const d = getEffectiveDate(r)
      return d && d >= statsDateRange.value.start
    })
  }
  if (statsDateRange.value.end) {
    data = data.filter(r => {
      const d = getEffectiveDate(r)
      return d && d <= statsDateRange.value.end
    })
  }
  // 按项目筛选
  if (statsProjectFilter.value) {
    data = data.filter(r => {
      const projects = parseMultiSelect(r.affected_projects)
      return projects.includes(statsProjectFilter.value)
    })
  }
  // 按时长筛选（值是 key，对所有操作生效：维护取 max，其他取 start_duration）
  if (statsDurationFilter.value) {
    data = data.filter(r => {
      const idx = getRecordDurationIndex(r)
      if (idx < 0) return false
      return ['twoMin', 'fiveMin', 'tenMin', 'overTenMin'][idx] === statsDurationFilter.value
    })
  }
  // 按操作人筛选
  if (statsOperatorFilter.value) {
    data = data.filter(r => r.operator === statsOperatorFilter.value)
  }
  // 按操作类型筛选（值是 key，走 isOp 匹配中英文）
  if (statsOperationFilter.value) {
    data = data.filter(r => isOp(r.operation, statsOperationFilter.value))
  }
  // 按局号筛选
  if (statsRoundIdFilter.value) {
    const q = statsRoundIdFilter.value.toLowerCase()
    data = data.filter(r => r.affected_round_ids?.toLowerCase().includes(q))
  }
  // 按维护类型筛选（值是 key，走 isMaintType 兼容中英文）
  if (statsMaintTypeFilter.value) {
    data = data.filter(r => isMaintType(r.maintenance_type, statsMaintTypeFilter.value))
  }
  // 忽略指定创建人（统计页生效，列表页不受影响）
  if (statsExcludeApiKeyFilter.value) {
    data = data.filter(r => !(r.created_by || '').startsWith('apikey:'))
  }
  if (statsExcludeCreatorsFilter.value.length) {
    data = data.filter(r => !statsExcludeCreatorsFilter.value.includes(r.created_by))
  }
  // 按桌台状态筛选：记录里 affected_tables 任一桌台 status === 选中值即保留
  if (statsTableStatusFilter.value) {
    data = data.filter(r => {
      const tables = parseMultiSelect(r.affected_tables)
      return tables.some(t => getTableStatus(t) === statsTableStatusFilter.value)
    })
  }
  return data
})

function applyStatsFilter() {
  statsDateRange.value = { ...statsDateInput.value }
  statsProjectFilter.value = statsProjectInput.value
  statsDurationFilter.value = statsDurationInput.value
  statsOperatorFilter.value = statsOperatorInput.value
  statsOperationFilter.value = statsOperationInput.value
  statsRoundIdFilter.value = statsRoundIdInput.value
  statsMaintTypeFilter.value = statsMaintTypeInput.value
  statsExcludeCreatorsFilter.value = [...statsExcludeCreatorsInput.value]
  statsExcludeApiKeyFilter.value = statsExcludeApiKeyInput.value
  statsTableStatusFilter.value = statsTableStatusInput.value
  saveStatsExcludeConfig()
}

// 候选创建人（从已加载记录里去重，按字母排序）
const creatorOptions = computed(() => {
  const s = new Set()
  records.value.forEach(r => {
    if (r.created_by && r.created_by !== '-') s.add(r.created_by)
  })
  return Array.from(s).sort()
})

// 忽略按钮的标签：未选 = "忽略创建人"；有选 = "忽略创建人: apikey:* +N"
const excludeBtnLabel = computed(() => {
  const n = statsExcludeCreatorsInput.value.length
  const apiKey = statsExcludeApiKeyInput.value
  const base = t('tableMaintenance.statsPanel.excludeCreators')
  if (!n && !apiKey) return base
  const parts = []
  if (apiKey) parts.push('apikey:*')
  if (n) parts.push('+' + n)
  return base + ': ' + parts.join(' ')
})

async function loadStatsExcludeConfig() {
  if (!tableId.value) return
  try {
    const res = await api.get('/api/user-settings?key=table_maint_stats_exclude_' + tableId.value)
    const v = res.data?.value
    if (v && typeof v === 'object') {
      const creators = Array.isArray(v.creators) ? v.creators : []
      const excludeApiKey = !!v.excludeApiKey
      statsExcludeCreatorsInput.value = [...creators]
      statsExcludeApiKeyInput.value = excludeApiKey
      statsExcludeCreatorsFilter.value = [...creators]
      statsExcludeApiKeyFilter.value = excludeApiKey
    }
  } catch (e) {
    // 没配置就用默认值，安静失败
  }
}

async function saveStatsExcludeConfig() {
  if (!tableId.value) return
  try {
    await api.post('/api/user-settings', {
      key: 'table_maint_stats_exclude_' + tableId.value,
      value: {
        creators: statsExcludeCreatorsFilter.value,
        excludeApiKey: statsExcludeApiKeyFilter.value
      }
    })
  } catch (e) {
    // 保存失败不打扰用户，下次刷新会重新展示当前选择
  }
}

// 统计分析数据
const statsAnalysis = computed(() => {
  const data = statsFilteredRecords.value
  if (!data.length) return {}

  // 辅助函数：按桌台数计数（一条记录影响多少个桌台，就算多少次）
  function getTableCount(r) {
    const n = parseMultiSelect(r.affected_tables).length
    return n > 0 ? n : 1
  }

  // 按游戏类型统计（支持多选，按桌台数计数）
  const gameTypeMap = {}
  data.forEach(r => {
    const tc = getTableCount(r)
    const types = parseMultiSelect(r.game_types)
    if (types.length) {
      types.forEach(gt => {
        gameTypeMap[gt] = (gameTypeMap[gt] || 0) + tc
      })
    } else {
      gameTypeMap[t('tableMaintenance.statsPanel.unknown')] = (gameTypeMap[t('tableMaintenance.statsPanel.unknown')] || 0) + tc
    }
  })
  const byGameType = Object.entries(gameTypeMap).map(([name, count]) => ({ name, count })).sort((a, b) => b.count - a.count)
  const maxGameType = Math.max(...byGameType.map(i => i.count), 1)

  // 按现场统计（affected_sites 是多选数组，按桌台数计数）
  const siteMap = {}
  data.forEach(r => {
    const tc = getTableCount(r)
    const sites = parseMultiSelect(r.affected_sites)
    if (sites.length) {
      sites.forEach(s => {
        siteMap[s] = (siteMap[s] || 0) + tc
      })
    } else {
      siteMap[t('tableMaintenance.statsPanel.unknown')] = (siteMap[t('tableMaintenance.statsPanel.unknown')] || 0) + tc
    }
  })
  const bySite = Object.entries(siteMap).map(([name, count]) => ({ name, count })).sort((a, b) => b.count - a.count)
  const maxSite = Math.max(...bySite.map(i => i.count), 1)

  // 按操作类型统计（维护/取消/重算/重派彩/包桌T人，按桌台数计数）
  const opKeys = ['maintenance', 'cancel', 'recalculate', 'repayout', 'vipTable']
  const operationCounts = {}
  opKeys.forEach(k => { operationCounts[k] = 0 })
  data.forEach(r => {
    const op = r.operation || ''
    for (const k of opKeys) {
      if (isOp(op, k)) {
        if (k === 'maintenance' && (!r.maintenance_type || isMaintType(r.maintenance_type, 'none'))) return
        operationCounts[k] += getTableCount(r)
        return
      }
    }
  })
  const byOperation = opKeys.map(k => ({ name: t(`tableMaintenance.options.operation.${k}`), count: operationCounts[k] }))

  // 时长 keys
  const durKeys = ['twoMin', 'fiveMin', 'tenMin', 'overTenMin']

  // 维护操作的数据（排除 maintenance_type 为"无"的记录）
  const maintData = data.filter(r => isOp(r.operation, 'maintenance') && r.maintenance_type && !isMaintType(r.maintenance_type, 'none'))
  let maintTotalByTable = 0
  maintData.forEach(r => { maintTotalByTable += getTableCount(r) })

  // 维护-开始时长统计（按桌台数计数）
  const maintStartDurMap = {}
  durKeys.forEach(k => { maintStartDurMap[k] = 0 })
  maintData.forEach(r => {
    const tc = getTableCount(r)
    const dk = getDurKey(r.start_duration)
    if (dk) maintStartDurMap[dk] += tc
  })
  const byMaintStartDuration = durKeys.map(k => ({ name: t(`tableMaintenance.options.duration.${k}`), count: maintStartDurMap[k] }))

  // 维护-关闭时长统计（按桌台数计数）
  const closeDurMap = {}
  durKeys.forEach(k => { closeDurMap[k] = 0 })
  maintData.forEach(r => {
    const tc = getTableCount(r)
    const dk = getDurKey(r.close_duration)
    if (dk) closeDurMap[dk] += tc
  })
  const byCloseDuration = durKeys.map(k => ({ name: t(`tableMaintenance.options.duration.${k}`), count: closeDurMap[k] }))

  // 维护总时长统计（取开始时长和关闭时长中较大的那个，按桌台数计数）
  const totalDurMap = {}
  durKeys.forEach(k => { totalDurMap[k] = 0 })
  maintData.forEach(r => {
    const tc = getTableCount(r)
    const startIdx = getDurIndex(r.start_duration)
    const closeIdx = getDurIndex(r.close_duration)
    const maxIdx = Math.max(startIdx, closeIdx)
    if (maxIdx >= 0) {
      totalDurMap[durKeys[maxIdx]] += tc
    }
  })
  const byTotalDuration = durKeys.map(k => ({ name: t(`tableMaintenance.options.duration.${k}`), count: totalDurMap[k] }))

  // 维护时长明细列表（每条记录的日期、桌号、开始时长、关闭时长、总时长）
  const maintDurationList = maintData.map(r => {
    const startIdx = getDurIndex(r.start_duration)
    const closeIdx = getDurIndex(r.close_duration)
    const maxIdx = Math.max(startIdx, closeIdx)
    const totalDuration = maxIdx >= 0 ? t(`tableMaintenance.options.duration.${durKeys[maxIdx]}`) : '-'
    // 日期：优先使用 date 字段，其次从 start_time 提取
    let dateStr = r.date
    if (!dateStr && r.start_time) {
      dateStr = r.start_time.split(' ')[0] || r.start_time.split('T')[0]
    }
    return {
      date: dateStr || '-',
      tableNo: parseMultiSelect(r.affected_tables).join(', ') || '-',
      startDuration: displayDur(r.start_duration) || '-',
      closeDuration: displayDur(r.close_duration) || '-',
      totalDuration,
      maintType: displayMaintType(r.maintenance_type) || '-',
      roundIds: r.affected_round_ids || '-'
    }
  }).sort((a, b) => (b.date || '').localeCompare(a.date || ''))

  // 取消操作明细列表
  const cancelDetailList = data.filter(r => isOp(r.operation, 'cancel')).map(r => {
    let dateStr = r.date
    if (!dateStr && r.start_time) {
      dateStr = r.start_time.split(' ')[0] || r.start_time.split('T')[0]
    }
    return {
      date: dateStr || '-',
      projects: parseMultiSelect(r.affected_projects).join(', ') || '-',
      roundIds: r.affected_round_ids || '-',
      startDuration: displayDur(r.start_duration) || '-',
      operator: r.operator || '-'
    }
  }).sort((a, b) => (b.date || '').localeCompare(a.date || ''))

  // 重算操作明细列表
  const recalcDetailList = data.filter(r => isOp(r.operation, 'recalculate')).map(r => {
    let dateStr = r.date
    if (!dateStr && r.start_time) {
      dateStr = r.start_time.split(' ')[0] || r.start_time.split('T')[0]
    }
    return {
      date: dateStr || '-',
      projects: parseMultiSelect(r.affected_projects).join(', ') || '-',
      roundIds: r.affected_round_ids || '-',
      startDuration: displayDur(r.start_duration) || '-',
      operator: r.operator || '-'
    }
  }).sort((a, b) => (b.date || '').localeCompare(a.date || ''))

  // 重派彩操作明细列表
  const repayoutDetailList = data.filter(r => isOp(r.operation, 'repayout')).map(r => {
    let dateStr = r.date
    if (!dateStr && r.start_time) {
      dateStr = r.start_time.split(' ')[0] || r.start_time.split('T')[0]
    }
    return {
      date: dateStr || '-',
      projects: parseMultiSelect(r.affected_projects).join(', ') || '-',
      roundIds: r.affected_round_ids || '-',
      startDuration: displayDur(r.start_duration) || '-',
      operator: r.operator || '-'
    }
  }).sort((a, b) => (b.date || '').localeCompare(a.date || ''))

  // 包桌T人明细列表
  const vipTableDetailList = data.filter(r => isOp(r.operation, 'vipTable')).map(r => {
    let dateStr = r.date
    if (!dateStr && r.start_time) {
      dateStr = r.start_time.split(' ')[0] || r.start_time.split('T')[0]
    }
    return {
      date: dateStr || '-',
      projects: parseMultiSelect(r.affected_projects).join(', ') || '-',
      roundIds: r.affected_round_ids || '-',
      startDuration: displayDur(r.start_duration) || '-',
      operator: r.operator || '-'
    }
  }).sort((a, b) => (b.date || '').localeCompare(a.date || ''))

  // 漏操作明细列表
  const missedDetailList = data.filter(r => isOp(r.operation, 'missed')).map(r => {
    let dateStr = r.date
    if (!dateStr && r.start_time) {
      dateStr = r.start_time.split(' ')[0] || r.start_time.split('T')[0]
    }
    return {
      date: dateStr || '-',
      projects: parseMultiSelect(r.affected_projects).join(', ') || '-',
      roundIds: r.affected_round_ids || '-',
      startDuration: displayDur(r.start_duration) || '-',
      operator: r.operator || '-',
      remark: r.remark || '-'
    }
  }).sort((a, b) => (b.date || '').localeCompare(a.date || ''))

  // 漏截图明细列表
  const missedScreenshotDetailList = data.filter(r => isOp(r.operation, 'missedScreenshot')).map(r => {
    let dateStr = r.date
    if (!dateStr && r.start_time) {
      dateStr = r.start_time.split(' ')[0] || r.start_time.split('T')[0]
    }
    return {
      date: dateStr || '-',
      projects: parseMultiSelect(r.affected_projects).join(', ') || '-',
      roundIds: r.affected_round_ids || '-',
      operator: r.operator || '-',
      remark: r.remark || '-'
    }
  }).sort((a, b) => (b.date || '').localeCompare(a.date || ''))

  // 操作人统计（按桌台数计数）
  const operatorMap = {}
  data.forEach(r => {
    const tc = getTableCount(r)
    const key = r.operator || t('tableMaintenance.statsPanel.notFilled')
    operatorMap[key] = (operatorMap[key] || 0) + tc
  })
  const byOperator = Object.entries(operatorMap).map(([name, count]) => ({ name, count })).sort((a, b) => b.count - a.count)
  const maxOperator = Math.max(...byOperator.map(i => i.count), 1)

  // 质检人统计（按桌台数计数）
  const inspectorMap = {}
  data.forEach(r => {
    const tc = getTableCount(r)
    const key = r.inspector || t('tableMaintenance.statsPanel.notFilled')
    inspectorMap[key] = (inspectorMap[key] || 0) + tc
  })
  const byInspector = Object.entries(inspectorMap).map(([name, count]) => ({ name, count })).sort((a, b) => b.count - a.count)
  const maxInspector = Math.max(...byInspector.map(i => i.count), 1)
  
  // 影响项目统计（按桌台数计数）
  const affectProjectMap = {}
  let noAffectCount = 0
  data.forEach(r => {
    const tc = getTableCount(r)
    const projects = parseMultiSelectSync(r.affected_projects)
    if (projects.length) {
      projects.forEach(p => {
        affectProjectMap[p] = (affectProjectMap[p] || 0) + tc
      })
    } else {
      noAffectCount += tc
    }
  })
  const byAffectProject = Object.entries(affectProjectMap).map(([name, count]) => ({ name, count })).sort((a, b) => b.count - a.count)
  const maxAffectProject = Math.max(...byAffectProject.map(i => i.count), 1)

  // 质检状态统计（按桌台数计数）
  let qcNormal = 0, qcAbnormal = 0, qcPending = 0
  data.forEach(r => {
    const tc = getTableCount(r)
    if (isQc(r.qc_status, 'normal')) qcNormal += tc
    else if (isQc(r.qc_status, 'abnormal')) qcAbnormal += tc
    else qcPending += tc
  })
  
  // 按桌台统计（使用层级配置精确归属每个桌台的项目、现场、游戏类型）
  const tableNoMap = {}
  data.forEach(r => {
    const tables = parseMultiSelect(r.affected_tables)
    const rProjects = parseMultiSelect(r.affected_projects)
    const rSites = parseMultiSelect(r.affected_sites)
    if (tables.length) {
      tables.forEach(tName => {
        // 从层级配置中查找此桌台的所有配置项
        let matches = tableOptions.value.filter(t => t.name === tName)
        if (matches.length > 0) {
          // 用记录的项目缩小匹配
          if (rProjects.length) {
            const f = matches.filter(t => rProjects.includes(t.project))
            if (f.length) matches = f
          }
          // 用记录的现场缩小匹配
          if (rSites.length) {
            const f = matches.filter(t => rSites.includes(t.site))
            if (f.length) matches = f
          }
          // 对每个匹配的配置单独计数
          matches.forEach(ti => {
            const key = `${tName}|${ti.project || ''}|${ti.site || ''}`
            if (!tableNoMap[key]) {
              tableNoMap[key] = {
                tableNo: tName,
                project: ti.project || '-',
                site: ti.site || '-',
                gameType: ti.gameTypes?.join(', ') || '-',
                count: 0
              }
            }
            tableNoMap[key].count++
          })
        } else {
          // 层级配置中找不到，回退到记录中的信息
          const key = `${tName}|?|?`
          if (!tableNoMap[key]) {
            tableNoMap[key] = {
              tableNo: tName,
              project: rProjects.join(', ') || '-',
              site: rSites.join(', ') || '-',
              gameType: parseMultiSelect(r.game_types).join(', ') || '-',
              count: 0
            }
          }
          tableNoMap[key].count++
        }
      })
    } else {
      const key = '未填写|-|-'
      if (!tableNoMap[key]) {
        tableNoMap[key] = { tableNo: t('tableMaintenance.statsPanel.notFilled'), project: '-', site: '-', gameType: '-', count: 0 }
      }
      tableNoMap[key].count++
    }
  })
  const byTableNo = Object.values(tableNoMap).map(item => {
    const tableConfig = tableOptions.value.find(t => t.name === item.tableNo)
    return {
      tableNo: item.tableNo,
      project: item.project,
      gameType: item.gameType,
      site: item.site,
      status: tableConfig ? (tableConfig.status || 'enabled') : 'unconfigured',
      count: item.count
    }
  }).sort((a, b) => b.count - a.count)
  const maxTableNo = Math.max(...byTableNo.map(i => i.count), 1)

  // 按桌台汇总统计（不区分项目，同名桌台合并计数）
  const tableSummaryMap = {}
  byTableNo.forEach(item => {
    if (!tableSummaryMap[item.tableNo]) {
      tableSummaryMap[item.tableNo] = { tableNo: item.tableNo, projects: new Set(), sites: new Set(), gameTypes: new Set(), count: 0 }
    }
    const s = tableSummaryMap[item.tableNo]
    s.count += item.count
    if (item.project && item.project !== '-') s.projects.add(item.project)
    if (item.site && item.site !== '-') s.sites.add(item.site)
    if (item.gameType && item.gameType !== '-') item.gameType.split(', ').forEach(g => s.gameTypes.add(g))
  })
  const byTableSummary = Object.values(tableSummaryMap).map(item => {
    const tableConfig = tableOptions.value.find(t => t.name === item.tableNo)
    return {
      tableNo: item.tableNo,
      projects: Array.from(item.projects).join(', ') || '-',
      sites: Array.from(item.sites).join(', ') || '-',
      gameTypes: Array.from(item.gameTypes).join(', ') || '-',
      status: tableConfig ? (tableConfig.status || 'enabled') : 'unconfigured',
      count: item.count
    }
  }).sort((a, b) => b.count - a.count)
  const maxTableSummary = Math.max(...byTableSummary.map(i => i.count), 1)
  
  // 按维护类型统计各时长分布（仅 operation=维护 的记录，不统计"无"，按桌台数计数）
  const maintTypeKeys = ['routine', 'temporary', 'emergency']
  const byMaintType = maintTypeKeys.map(mtKey => {
    const items = data.filter(r => isOp(r.operation, 'maintenance') && isMaintType(r.maintenance_type, mtKey))
    let total = 0
    const durations = {}
    durKeys.forEach(k => { durations[k] = 0 })
    let noDurationCount = 0
    items.forEach(r => {
      const tc = getTableCount(r)
      total += tc
      const startIdx = getDurIndex(r.start_duration)
      const closeIdx = getDurIndex(r.close_duration)
      const maxIdx = Math.max(startIdx, closeIdx)
      if (maxIdx >= 0) durations[durKeys[maxIdx]] += tc
      else noDurationCount += tc
    })
    const displayDurations = {}
    durKeys.forEach(k => { displayDurations[t(`tableMaintenance.options.duration.${k}`)] = durations[k] })
    if (noDurationCount > 0) displayDurations[t('tableMaintenance.statsPanel.notFilled')] = noDurationCount
    return { name: t(`tableMaintenance.options.maintenanceType.${mtKey}`), total, durations: displayDurations }
  })

  // 按操作类型统计（维护操作取最大时长，其他操作用开始时长，按桌台数计数）
  const byOpType = opKeys.map(opKey => {
    let items = data.filter(r => isOp(r.operation, opKey))
    if (opKey === 'maintenance') {
      items = items.filter(r => r.maintenance_type && !isMaintType(r.maintenance_type, 'none'))
    }
    let total = 0
    const durations = {}
    durKeys.forEach(k => { durations[k] = 0 })
    let noDurationCount = 0
    items.forEach(r => {
      const tc = getTableCount(r)
      total += tc
      if (opKey === 'maintenance') {
        const startIdx = getDurIndex(r.start_duration)
        const closeIdx = getDurIndex(r.close_duration)
        const maxIdx = Math.max(startIdx, closeIdx)
        if (maxIdx >= 0) durations[durKeys[maxIdx]] += tc
        else noDurationCount += tc
      } else {
        const dk = getDurKey(r.start_duration)
        if (dk) durations[dk] += tc
        else noDurationCount += tc
      }
    })
    const displayDurations = {}
    durKeys.forEach(k => { displayDurations[t(`tableMaintenance.options.duration.${k}`)] = durations[k] })
    if (noDurationCount > 0) displayDurations[t('tableMaintenance.statsPanel.notFilled')] = noDurationCount
    return { name: t(`tableMaintenance.options.operation.${opKey}`), key: opKey, total, durations: displayDurations, hasDuration: true }
  })
  
  // 按项目 × 操作类型矩阵（每条记录按 affected_projects 展开，每个项目下 5 种操作各自累加桌台数）
  const projectOpMap = {}
  data.forEach(r => {
    const tc = getTableCount(r)
    const projects = parseMultiSelect(r.affected_projects)
    if (!projects.length) return
    let opKey = null
    for (const k of opKeys) {
      if (isOp(r.operation, k)) {
        // 维护操作需排除 maintenance_type 为"无"的记录
        if (k === 'maintenance' && (!r.maintenance_type || isMaintType(r.maintenance_type, 'none'))) return
        opKey = k
        break
      }
    }
    if (!opKey) return
    projects.forEach(p => {
      if (!projectOpMap[p]) {
        projectOpMap[p] = { project: p, total: 0, ops: {} }
        opKeys.forEach(k => { projectOpMap[p].ops[k] = 0 })
      }
      projectOpMap[p].ops[opKey] += tc
      projectOpMap[p].total += tc
    })
  })
  const byProjectOperation = Object.values(projectOpMap).sort((a, b) => b.total - a.total)

  // 总计（按桌台数）
  let totalByTable = 0
  data.forEach(r => { totalByTable += getTableCount(r) })

  // 未接入桌台维护汇总：从 byTableSummary 取 status='pending' 的桌台
  // 卡片显示用 pendingMaintCount = 涉及次数总和（多桌联合记录按桌台数累加，跟其他卡片口径一致）
  const pendingTables = byTableSummary.filter(it => it.status === 'pending')
  const pendingMaintCount = pendingTables.reduce((s, it) => s + it.count, 0)
  const pendingTableCount = pendingTables.length

  return {
    total: totalByTable,
    totalRecords: data.length,
    byGameType, maxGameType,
    bySite, maxSite,
    byOperation,
    byMaintStartDuration, byCloseDuration, byTotalDuration, maintDurationList,
    cancelDetailList, recalcDetailList, repayoutDetailList, vipTableDetailList, missedDetailList, missedScreenshotDetailList,
    byOperator, maxOperator,
    byInspector, maxInspector,
    byTableSummary, maxTableSummary,
    byTableNo, maxTableNo,
    byMaintType, byOpType, byProjectOperation, opKeys,
    byAffectProject, maxAffectProject, noAffectCount,
    qcNormal, qcAbnormal, qcPending,
    pendingMaintCount, pendingTableCount
  }
})

function resetStatsFilter() {
  statsDateInput.value = { start: '', end: '' }
  statsDateRange.value = { start: '', end: '' }
  statsProjectInput.value = ''
  statsProjectFilter.value = ''
  statsDurationInput.value = ''
  statsDurationFilter.value = ''
  statsOperatorInput.value = ''
  statsOperatorFilter.value = ''
  statsOperationInput.value = ''
  statsOperationFilter.value = ''
  statsRoundIdInput.value = ''
  statsRoundIdFilter.value = ''
  statsMaintTypeInput.value = ''
  statsMaintTypeFilter.value = ''
  statsExcludeCreatorsInput.value = []
  statsExcludeApiKeyInput.value = false
  statsExcludeCreatorsFilter.value = []
  statsExcludeApiKeyFilter.value = false
  statsTableStatusInput.value = ''
  statsTableStatusFilter.value = ''
  saveStatsExcludeConfig()
}

function exportStatsToExcel() {
  const stats = statsAnalysis.value
  if (!stats.total) {
    appStore.showToast(t('common.warning'), 'warning')
    return
  }
  
  const lines = []
  const BOM = '\uFEFF'
  
  // 时间范围
  const noLimit = t('tableMaintenance.statsPanel.csvNoLimit')
  const dateInfo = statsDateRange.value.start || statsDateRange.value.end
    ? `${statsDateRange.value.start || noLimit} ~ ${statsDateRange.value.end || noLimit}`
    : t('tableMaintenance.statsPanel.csvAllTime')
  const durTwoMin = t('tableMaintenance.options.duration.twoMin')
  const durFiveMin = t('tableMaintenance.options.duration.fiveMin')
  const durTenMin = t('tableMaintenance.options.duration.tenMin')
  const durOverTenMin = t('tableMaintenance.options.duration.overTenMin')

  lines.push(`${t('tableMaintenance.statsPanel.csvDateRange')},${dateInfo}`)
  lines.push(`${t('tableMaintenance.statsPanel.csvTotalRecords')},${stats.totalRecords}`)
  lines.push(`${t('tableMaintenance.statsPanel.csvTotalTables')},${stats.total}`)
  lines.push('')

  // 按维护类型统计（含时长分布）
  lines.push(t('tableMaintenance.statsPanel.csvByMaintType'))
  lines.push(`${t('tableMaintenance.statsPanel.csvMaintType')},${t('tableMaintenance.statsPanel.csvTotal')},${durTwoMin},${durFiveMin},${durTenMin},${durOverTenMin}`)
  stats.byMaintType?.forEach(item => {
    lines.push(`${item.name},${item.total},${item.durations[durTwoMin] || 0},${item.durations[durFiveMin] || 0},${item.durations[durTenMin] || 0},${item.durations[durOverTenMin] || 0}`)
  })
  lines.push('')

  // 按操作类型统计（含开始时长分布）
  lines.push(t('tableMaintenance.statsPanel.csvByOpType'))
  lines.push(`${t('tableMaintenance.statsPanel.csvOpType')},${t('tableMaintenance.statsPanel.csvTotal')},${durTwoMin},${durFiveMin},${durTenMin},${durOverTenMin}`)
  stats.byOpType?.forEach(item => {
    lines.push(`${item.name},${item.total},${item.durations[durTwoMin] || 0},${item.durations[durFiveMin] || 0},${item.durations[durTenMin] || 0},${item.durations[durOverTenMin] || 0}`)
  })
  lines.push('')

  // 按项目 × 操作类型矩阵
  if (stats.byProjectOperation?.length) {
    lines.push(t('tableMaintenance.statsPanel.csvByProjectOperation'))
    const opHeaders = (stats.opKeys || []).map(k => t(`tableMaintenance.options.operation.${k}`))
    lines.push(`${t('tableMaintenance.statsPanel.csvProject')},${t('tableMaintenance.statsPanel.csvTotal')},${opHeaders.join(',')}`)
    stats.byProjectOperation.forEach(item => {
      const opCounts = (stats.opKeys || []).map(k => item.ops[k] || 0).join(',')
      lines.push(`${item.project},${item.total},${opCounts}`)
    })
    lines.push('')
  }

  // 维护-开始时长
  lines.push(t('tableMaintenance.statsPanel.csvMaintStartDuration'))
  lines.push(`${t('tableMaintenance.statsPanel.csvDuration')},${t('tableMaintenance.statsPanel.csvCount')}`)
  stats.byMaintStartDuration?.forEach(item => lines.push(`${item.name},${item.count}`))
  lines.push('')

  // 维护-关闭时长
  lines.push(t('tableMaintenance.statsPanel.csvMaintCloseDuration'))
  lines.push(`${t('tableMaintenance.statsPanel.csvDuration')},${t('tableMaintenance.statsPanel.csvCount')}`)
  stats.byCloseDuration?.forEach(item => lines.push(`${item.name},${item.count}`))
  lines.push('')

  // 维护-总时长
  lines.push(t('tableMaintenance.statsPanel.csvMaintTotalDuration'))
  lines.push(`${t('tableMaintenance.statsPanel.csvDuration')},${t('tableMaintenance.statsPanel.csvCount')}`)
  stats.byTotalDuration?.forEach(item => lines.push(`${item.name},${item.count}`))
  lines.push('')

  // 操作人统计
  lines.push(t('tableMaintenance.statsPanel.csvByOperator'))
  lines.push(`${t('tableMaintenance.statsPanel.csvOperator')},${t('tableMaintenance.statsPanel.csvCount')}`)
  stats.byOperator?.forEach(item => lines.push(`${item.name},${item.count}`))
  lines.push('')

  // 质检人统计
  lines.push(t('tableMaintenance.statsPanel.csvByInspector'))
  lines.push(`${t('tableMaintenance.statsPanel.csvInspector')},${t('tableMaintenance.statsPanel.csvCount')}`)
  stats.byInspector?.forEach(item => lines.push(`${item.name},${item.count}`))
  lines.push('')

  // 质检状态统计
  lines.push(t('tableMaintenance.statsPanel.csvQcStatus'))
  lines.push(`${t('tableMaintenance.statsPanel.csvStatus')},${t('tableMaintenance.statsPanel.csvCount')}`)
  lines.push(`${t('tableMaintenance.statsPanel.csvQcNormal')},${stats.qcNormal}`)
  lines.push(`${t('tableMaintenance.statsPanel.csvQcAbnormal')},${stats.qcAbnormal}`)
  lines.push(`${t('tableMaintenance.statsPanel.csvQcPending')},${stats.qcPending}`)
  lines.push('')

  // 影响项目统计
  lines.push(t('tableMaintenance.statsPanel.csvByAffectedProject'))
  lines.push(`${t('tableMaintenance.statsPanel.csvProject')},${t('tableMaintenance.statsPanel.csvCount')}`)
  stats.byAffectProject?.forEach(item => lines.push(`${item.name},${item.count}`))
  if (stats.noAffectCount > 0) {
    lines.push(`${t('tableMaintenance.statsPanel.csvNoAffect')},${stats.noAffectCount}`)
  }
  lines.push('')

  // 游戏类型统计
  lines.push(t('tableMaintenance.statsPanel.csvByGameType'))
  lines.push(`${t('tableMaintenance.statsPanel.csvGameType')},${t('tableMaintenance.statsPanel.csvCount')}`)
  stats.byGameType?.forEach(item => lines.push(`${item.name},${item.count}`))
  lines.push('')

  // 现场统计
  lines.push(t('tableMaintenance.statsPanel.csvBySite'))
  lines.push(`${t('tableMaintenance.statsPanel.csvSite')},${t('tableMaintenance.statsPanel.csvCount')}`)
  stats.bySite?.forEach(item => lines.push(`${item.name},${item.count}`))
  lines.push('')

  // 桌台汇总
  lines.push(t('tableMaintenance.statsPanel.csvTableSummary'))
  lines.push(`${t('tableMaintenance.statsPanel.csvTableNo')},${t('tableMaintenance.statsPanel.csvMaintCount')},${t('tableMaintenance.statsPanel.csvProjects')},${t('tableMaintenance.statsPanel.csvSites')},${t('tableMaintenance.statsPanel.csvGameTypes')},${t('tableMaintenance.statsPanel.csvStatus')}`)
  stats.byTableSummary?.forEach(item => lines.push(`${item.tableNo},${item.count},"${item.projects}","${item.sites}","${item.gameTypes}",${t('tableHierarchy.status.' + (item.status || 'enabled'))}`))
  lines.push('')

  // 桌台明细（按项目）
  lines.push(t('tableMaintenance.statsPanel.csvTableDetail'))
  lines.push(`${t('tableMaintenance.statsPanel.csvTableNo')},${t('tableMaintenance.statsPanel.csvProject')},${t('tableMaintenance.statsPanel.csvSite')},${t('tableMaintenance.statsPanel.csvGameType')},${t('tableMaintenance.statsPanel.csvMaintCount')}`)
  stats.byTableNo?.forEach(item => lines.push(`${item.tableNo},"${item.project}",${item.site},${item.gameType},${item.count}`))

  const csvContent = BOM + lines.join('\n')
  const blob = new Blob([csvContent], { type: 'text/csv;charset=utf-8;' })
  const link = document.createElement('a')
  link.href = URL.createObjectURL(blob)
  link.download = `${t('tableMaintenance.statsPanel.csvFileName')}_${todayLocal()}.csv`
  link.click()
  URL.revokeObjectURL(link.href)
  
  appStore.showToast(t('tableMaintenance.actions.exportSuccess'), 'success')
}

function getBarWidth(count, max) {
  return Math.round((count / max) * 100) + '%'
}

const statusOptions = ['待处理', '处理中', '已解决']
const issueTypeOptions = ['设备故障', '清洁问题', '物品损坏', '桌面磨损', '配件缺失', '其他']

// 根据 key 获取列定义
function getCol(key) {
  return columns.value.find(c => c.key === key)
}

const formColumns = computed(() => {
  return columns.value.filter(c => {
    if (['checkbox', 'actions', 'readonly', 'datetime-readonly', 'table-status-display'].includes(c.type)) return false
    if (['checkbox', 'actions', 'created_by', 'created_at', 'updated_at'].includes(c.key)) return false
    if (c.visible === false) return false
    // 只有"维护"操作才显示结束时间、结束截图、关闭维护时长
    if (['end_time', 'notify_end_screenshot', 'close_duration'].includes(c.key)) {
      return isOp(formData.value.operation, 'maintenance')
    }
    return true
  })
})

const attachmentColumns = computed(() => {
  return columns.value.filter(c => c.type === 'attachments')
})

// 详情页面的列（根据当前记录的operation动态显示）
const detailColumns = computed(() => {
  return columns.value.filter(c => {
    if (['checkbox', 'actions', 'readonly', 'datetime-readonly', 'table-status-display'].includes(c.type)) return false
    if (['checkbox', 'actions', 'created_by', 'created_at', 'updated_at'].includes(c.key)) return false
    if (c.visible === false) return false
    // 只有"维护"操作才显示结束时间、结束截图、关闭维护时长
    if (['end_time', 'notify_end_screenshot', 'close_duration'].includes(c.key)) {
      return isOp(detailRecord.value?.operation, 'maintenance')
    }
    return true
  })
})

const formAttachments = ref({})

function getEmptyForm() {
  const form = { id: '' }
  columns.value.forEach(col => {
    if (['checkbox', 'actions', 'created_by'].includes(col.key)) return
    if (col.type === 'attachments') {
      form[col.key] = []
    } else if (col.type === 'multi-select-projects' || col.type === 'multi-select-sites' || col.type === 'multi-select-tables' || col.type === 'multi-select-game-types') {
      form[col.key] = []
    } else if (col.type === 'date') {
      form[col.key] = todayLocal()
    } else if (col.type === 'datetime') {
      // 开始时间自动填充当前时间
      if (col.key === 'start_time') {
        const now = new Date()
        const pad = n => String(n).padStart(2, '0')
        form[col.key] = `${now.getFullYear()}-${pad(now.getMonth() + 1)}-${pad(now.getDate())} ${pad(now.getHours())}:${pad(now.getMinutes())}`
      } else {
        form[col.key] = ''
      }
    } else if (col.type === 'status') {
      form[col.key] = statusOptions[0] || ''
    } else if (col.type === 'select' || col.type === 'tag-type') {
      form[col.key] = col.default || col.options?.[0] || ''
    } else if (col.type === 'maint-type-select') {
      form[col.key] = t('tableMaintenance.options.maintenanceType.none')
    } else if (col.key === 'operator') {
      form[col.key] = authStore.user?.username || ''
    } else {
      form[col.key] = ''
    }
  })
  return form
}

function initFormAttachments() {
  const atts = {}
  attachmentColumns.value.forEach(col => {
    atts[col.key] = []
  })
  formAttachments.value = atts
}

function getColumnOptions(col) {
  if (col.options && Array.isArray(col.options) && col.options.length > 0) return col.options
  if (col.key === 'status' || col.type === 'status') return statusOptions
  if (col.key === 'issue_type') return issueTypeOptions
  return []
}

function getDurationClass(opt) {
  if (isDur(opt, 'twoMin')) return 'duration-green'
  if (isDur(opt, 'fiveMin')) return 'duration-blue'
  if (isDur(opt, 'tenMin')) return 'duration-orange'
  if (isDur(opt, 'overTenMin')) return 'duration-red'
  return ''
}

// 过滤后的全部记录（用于统计和导出）
const allFilteredRecords = computed(() => {
  let list = records.value
  if (searchQuery.value) {
    const q = searchQuery.value.toLowerCase()
    list = list.filter(r =>
      parseMultiSelect(r.affected_tables).join(',').toLowerCase().includes(q) ||
      r.description?.toLowerCase().includes(q) ||
      r.handler?.toLowerCase().includes(q) ||
      r.affected_round_ids?.toLowerCase().includes(q) ||
      r.operator?.toLowerCase().includes(q) ||
      r.reason?.toLowerCase().includes(q)
    )
  }
  if (selectedStatus.value) {
    if (selectedStatus.value === 'pending') {
      list = list.filter(r => !r.qc_status)
    } else {
      list = list.filter(r => isQc(r.qc_status, selectedStatus.value))
    }
  }
  if (dateRange.value.start) {
    list = list.filter(r => r.date >= dateRange.value.start)
  }
  if (dateRange.value.end) {
    list = list.filter(r => r.date <= dateRange.value.end)
  }
  // 按项目筛选
  if (selectedProject.value) {
    list = list.filter(r => {
      const projects = parseMultiSelect(r.affected_projects)
      return projects.includes(selectedProject.value)
    })
  }
  // 按时长筛选（对所有操作生效：维护取 max(start,close)，其他取 start_duration）
  if (selectedDuration.value) {
    list = list.filter(r => {
      const idx = isOp(r.operation, 'maintenance')
        ? Math.max(getDurIndex(r.start_duration), getDurIndex(r.close_duration))
        : getDurIndex(r.start_duration)
      if (idx < 0) return false
      const key = ['twoMin', 'fiveMin', 'tenMin', 'overTenMin'][idx]
      return isDur(selectedDuration.value, key)
    })
  }
  // 按操作人筛选
  if (selectedOperator.value) {
    list = list.filter(r => r.operator === selectedOperator.value)
  }
  // 按操作类型筛选（值是 key，走 isOp 兼容中英文）
  if (selectedOperation.value) {
    list = list.filter(r => isOp(r.operation, selectedOperation.value))
  }
  // 按桌台状态筛选
  if (selectedTableStatus.value) {
    list = list.filter(r => {
      const tables = parseMultiSelect(r.affected_tables)
      return tables.some(t => getTableStatus(t) === selectedTableStatus.value)
    })
  }
  // 按维护类型筛选（值是 key，走 isMaintType 兼容中英文）
  if (selectedMaintType.value) {
    list = list.filter(r => isMaintType(r.maintenance_type, selectedMaintType.value))
  }
  // 按 start_time > date > created_at 降序排序（最新在最上面）
  // 归一化 'T' → 空格，兼容历史数据：UI 写入是 "YYYY-MM-DDTHH:MM"、脚本写入是 "YYYY-MM-DD HH:MM"
  const normTs = s => String(s || '').replace('T', ' ')
  return [...list].sort((a, b) => {
    const at = normTs(a.start_time || a.date || a.created_at)
    const bt = normTs(b.start_time || b.date || b.created_at)
    return bt.localeCompare(at)
  })
})

// 总页数
const totalPages = computed(() => {
  return Math.ceil(allFilteredRecords.value.length / pageSize.value) || 1
})

// 当前页的记录（用于表格展示）
const filteredRecords = computed(() => {
  const start = (currentPage.value - 1) * pageSize.value
  const end = start + pageSize.value
  return allFilteredRecords.value.slice(start, end)
})

// 切换页码
function goToPage(page) {
  if (page >= 1 && page <= totalPages.value) {
    currentPage.value = page
  }
}


const allSelected = computed({
  get: () => filteredRecords.value.length > 0 && filteredRecords.value.every(r => selectedIds.value.includes(r.id)),
  set: (val) => {
    if (val) {
      filteredRecords.value.forEach(r => { if (!selectedIds.value.includes(r.id)) selectedIds.value.push(r.id) })
    } else {
      const pageIds = filteredRecords.value.map(r => r.id)
      selectedIds.value = selectedIds.value.filter(id => !pageIds.includes(id))
    }
  }
})

const editableColumns = computed(() => tempColumnConfig.value.filter(c => c.type !== 'checkbox' && c.type !== 'actions'))

onMounted(async () => {
  formData.value = getEmptyForm()
  await loadHierarchyOptions()
  await initTable()
})

// 加载层级配置选项（项目、现场、桌台、游戏类型）
async function loadHierarchyOptions() {
  try {
    // 读全局开关：'external' = 用桌台管理同步数据；其他 = 用桌台层级配置
    let useExternal = false
    try {
      const sres = await api.get('/api/settings?key=table_maint_data_source')
      useExternal = (sres.data?.table_maint_data_source === 'external')
    } catch (_) { useExternal = false }

    if (useExternal) {
      // 从桌台管理拉自动同步数据（默认只拿 PROD 环境），转成 hierarchy 风格塞进 *Options
      const [roomsRes, aliasRes] = await Promise.all([
        api.get('/api/external-rooms?env=PROD'),
        api.get('/api/external-aliases')
      ])
      const rooms = Array.isArray(roomsRes.data) ? roomsRes.data : []
      const aliases = Array.isArray(aliasRes.data) ? aliasRes.data : []
      const aliasMap = { platform: {}, gameType: {} }
      aliases.forEach(a => {
        if (a.alias_type === 'platform' || a.alias_type === 'gameType') {
          aliasMap[a.alias_type][a.code] = a.name_zh || a.code
        }
      })
      const transformed = transformExternalToHierarchy(rooms, aliasMap)
      projectOptions.value = transformed.projects
      siteOptions.value = transformed.sites
      tableOptions.value = transformed.tables
      gameTypeData.value = transformed.gameTypes
    } else {
      const config = await getTableHierarchy(true)
      projectOptions.value = config.projects || []
      siteOptions.value = config.sites || []
      tableOptions.value = config.tables || []
      gameTypeData.value = config.gameTypes || []
    }
  } catch (e) {
    console.error('加载层级配置失败', e)
  }
}

// 把 external_rooms 列表转成 hierarchy 风格的 { projects, sites, gameTypes, tables }
// rooms: [{ project, platform_id, platform_name, platform_name_zh, room_id, game_type, status }]
function transformExternalToHierarchy(rooms, aliasMap) {
  const projectSet = new Set()
  const siteMap = new Map()       // key="siteName|project" → { name_zh, name_en, project }
  const gameTypeSet = new Set()
  const tableMap = new Map()      // key="roomId|siteName|project" → table object

  const platformAlias = (code, defaultZh) => {
    if (aliasMap?.platform?.[code]) return aliasMap.platform[code]
    return defaultZh || code
  }
  const gameTypeAlias = (code) => aliasMap?.gameType?.[code] || code

  rooms.forEach(r => {
    const project = r.project || ''
    if (!project) return
    projectSet.add(project)

    const siteName = platformAlias(r.platform_name || r.platform_id, r.platform_name_zh)
    const siteKey = siteName + '|' + project
    if (!siteMap.has(siteKey)) {
      siteMap.set(siteKey, { name_zh: siteName, name_en: r.platform_name || '', project })
    }

    const gtName = gameTypeAlias(r.game_type)
    if (gtName) gameTypeSet.add(gtName)

    const tableKey = r.room_id + '|' + siteName + '|' + project
    if (!tableMap.has(tableKey)) {
      tableMap.set(tableKey, {
        name: r.room_id,
        project,
        site: siteName,
        gameTypes: gtName ? [gtName] : [],
        status: r.status || 'enabled'
      })
    } else {
      const existing = tableMap.get(tableKey)
      if (gtName && !existing.gameTypes.includes(gtName)) existing.gameTypes.push(gtName)
    }
  })

  return {
    projects: Array.from(projectSet).sort().map(p => ({ name_zh: p, name: p })),
    sites: Array.from(siteMap.values()),
    gameTypes: Array.from(gameTypeSet).sort().map(g => ({ name_zh: g })),
    tables: Array.from(tableMap.values())
  }
}

// 跳转到层级配置页面
function goToHierarchyConfig() {
  router.push('/table-hierarchy-config')
}


// 根据当前语言获取现场显示名称
function getSiteName(site) {
  if (!site) return ''
  if (typeof site === 'string') return site
  
  // 优先使用当前语言的名称，如果没有则回退
  const lang = appStore.language
  if (lang === 'en-US') {
    return site.name_en || site.name_zh || site.name || ''
  } else {
    return site.name_zh || site.name || site.name_en || ''
  }
}

// 获取现场的内部标识（用于数据关联，优先用 name_zh）
function getSiteKey(site) {
  if (!site) return ''
  if (typeof site === 'string') return site
  return site.name_zh || site.name || ''
}

// 多选下拉框展开状态
const projectSelectOpen = ref(false)
const siteSelectOpen = ref(false)
const tableSelectOpen = ref(false)
const tableSearchQuery = ref('')
const gameTypeSelectOpen = ref(false)

// 复制/粘贴功能
const copiedRecord = ref(null)
const showPasteModal = ref(false)
const pasteCount = ref(1)

function copyRecord(record) {
  // 复制时保留所有业务字段，排除系统字段
  const copy = { ...record }
  delete copy.id
  delete copy.created_by
  delete copy.created_at
  delete copy.updated_at
  copiedRecord.value = copy
  appStore.showToast(t('tableMaintenance.actions.copySuccess'), 'success')
}

function openPasteModal() {
  if (!copiedRecord.value) {
    appStore.showToast(t('common.warning'), 'warning')
    return
  }
  pasteCount.value = 1
  showPasteModal.value = true
}

async function pasteRecords() {
  if (!copiedRecord.value || pasteCount.value < 1 || !tableId.value) return
  
  loading.value = true
  try {
    for (let i = 0; i < pasteCount.value; i++) {
      // 构建数据对象，与 saveRecord 格式一致
      const data = {}
      columns.value.forEach(col => {
        if (['checkbox', 'actions', 'created_by'].includes(col.key)) return
        if (col.type === 'date') {
          // 日期使用当前日期
          data[col.key] = todayLocal()
        } else if (col.type === 'attachments') {
          // 附件列（截图等）也要复制
          data[col.key] = copiedRecord.value[col.key] || []
        } else {
          data[col.key] = copiedRecord.value[col.key] || ''
        }
      })
      
      // 构建 API payload
      const payload = {
        data: JSON.stringify(data),
        attachments: JSON.stringify(copiedRecord.value.attachments || [])
      }
      
      await api.post(`/api/custom-tables/${tableId.value}/rows`, payload)
    }
    appStore.showToast(t('tableMaintenance.actions.pasteSuccess', { count: pasteCount.value }), 'success')
    showPasteModal.value = false
    await loadRecords()
    await loadStats()
  } catch (err) {
    appStore.showToast(t('tableMaintenance.actions.saveFailed') + ': ' + (err.response?.data?.error || err.message), 'error')
  } finally {
    loading.value = false
  }
}

// 项目多选
function toggleProjectOption(optionName) {
  if (!formData.value.affected_projects) {
    formData.value.affected_projects = []
  }
  const idx = formData.value.affected_projects.indexOf(optionName)
  if (idx >= 0) {
    formData.value.affected_projects.splice(idx, 1)
  } else {
    formData.value.affected_projects.push(optionName)
  }
  // 项目改变后，清理无效的现场和桌台
  cleanupInvalidSelections()
}

function isProjectSelected(optionName) {
  return formData.value.affected_projects?.includes(optionName) || false
}

// 全选/取消全选项目
function selectAllProjects() {
  if (isAllProjectsSelected()) {
    formData.value.affected_projects = []
  } else {
    formData.value.affected_projects = projectOptions.value.map(p => getProjectKey(p))
  }
  cleanupInvalidSelections()
}

function isAllProjectsSelected() {
  return projectOptions.value.length > 0 && formData.value.affected_projects?.length === projectOptions.value.length
}

// 清理无效的现场和桌台（当项目选择改变时调用）
function cleanupInvalidSelections() {
  const validSites = getFilteredSites()
  const validTables = getFilteredTables()
  
  // 清理已选但不再有效的现场
  if (formData.value.affected_sites?.length) {
    formData.value.affected_sites = formData.value.affected_sites.filter(s => validSites.includes(s))
  }
  
  // 清理已选但不再有效的桌台
  if (formData.value.affected_tables?.length) {
    formData.value.affected_tables = formData.value.affected_tables.filter(t => validTables.includes(t))
  }
}

// 现场多选
function toggleSiteOption(optionName) {
  if (!formData.value.affected_sites) {
    formData.value.affected_sites = []
  }
  const idx = formData.value.affected_sites.indexOf(optionName)
  if (idx >= 0) {
    formData.value.affected_sites.splice(idx, 1)
  } else {
    formData.value.affected_sites.push(optionName)
  }
  // 现场改变后，清理无效的桌台
  cleanupInvalidTables()
}

// 清理无效的桌台（当现场选择改变时调用）
function cleanupInvalidTables() {
  const validTables = getFilteredTables()
  if (formData.value.affected_tables?.length) {
    formData.value.affected_tables = formData.value.affected_tables.filter(t => validTables.includes(t))
  }
}

function isSiteSelected(optionName) {
  return formData.value.affected_sites?.includes(optionName) || false
}

// 全选/取消全选现场
function selectAllSites() {
  const sites = getFilteredSites()
  if (isAllSitesSelected()) {
    formData.value.affected_sites = []
  } else {
    formData.value.affected_sites = [...sites]
  }
  cleanupInvalidTables()
}

function isAllSitesSelected() {
  const sites = getFilteredSites()
  return sites.length > 0 && formData.value.affected_sites?.length === sites.length
}

// 桌台多选
function toggleTableOption(optionName) {
  if (!formData.value.affected_tables) {
    formData.value.affected_tables = []
  }
  const idx = formData.value.affected_tables.indexOf(optionName)
  if (idx >= 0) {
    formData.value.affected_tables.splice(idx, 1)
  } else {
    formData.value.affected_tables.push(optionName)
  }
  syncSitesAndGameTypes()
}

// 根据选中的桌台自动勾选现场和游戏类型
function syncSitesAndGameTypes() {
  const selectedTables = formData.value.affected_tables || []
  if (selectedTables.length === 0) {
    formData.value.affected_sites = []
    formData.value.game_types = []
    return
  }
  const sitesSet = new Set()
  const gameTypesSet = new Set()
  tableOptions.value.forEach(t => {
    if (selectedTables.includes(t.name)) {
      if (t.site) sitesSet.add(t.site)
      if (t.gameTypes?.length) {
        t.gameTypes.forEach(gt => gameTypesSet.add(gt))
      }
    }
  })
  formData.value.affected_sites = [...sitesSet]
  formData.value.game_types = [...gameTypesSet]
}

function isTableSelected(optionName) {
  return formData.value.affected_tables?.includes(optionName) || false
}

// 全选/取消全选桌台
function selectAllTables() {
  const tables = getFilteredTables()
  if (isAllTablesSelected()) {
    formData.value.affected_tables = []
  } else {
    formData.value.affected_tables = [...tables]
  }
  syncSitesAndGameTypes()
}

function isAllTablesSelected() {
  const tables = getFilteredTables()
  return tables.length > 0 && formData.value.affected_tables?.length === tables.length
}

// 获取现场列表 - 根据选择的受影响项目过滤
// 返回 { key, label } 对象数组，key 用于存储，label 用于显示
function getFilteredSitesWithLabels() {
  const selectedProjects = formData.value.affected_projects || []
  let filteredSites = siteOptions.value
  if (selectedProjects.length > 0) {
    filteredSites = siteOptions.value.filter(s => selectedProjects.includes(s.project))
  }
  // 去重并返回 { key, label } 对象
  const siteMap = new Map()
  for (const s of filteredSites) {
    const key = getSiteKey(s)
    if (!siteMap.has(key)) {
      siteMap.set(key, { key, label: getSiteName(s) })
    }
  }
  return Array.from(siteMap.values())
}

// 兼容旧代码的简化版本
function getFilteredSites() {
  return getFilteredSitesWithLabels().map(s => s.key)
}

// 根据 key 获取现场显示名称
function getSiteDisplayName(key) {
  const site = siteOptions.value.find(s => getSiteKey(s) === key)
  return site ? getSiteName(site) : key
}

// 获取桌台列表 - 只根据选择的项目过滤（现场和游戏类型由桌台自动联动）
// pending（未接入）桌台允许出现：v552 起未接入桌台已挂在"未接入"项目下，
// 是合法的可维护实体，下拉里通过紫色小标签提示状态
function getFilteredTables() {
  const selectedProjects = formData.value.affected_projects || []

  let filteredTables = tableOptions.value

  // 根据选中的项目过滤
  if (selectedProjects.length > 0) {
    filteredTables = filteredTables.filter(t => selectedProjects.includes(t.project))
  }

  const names = filteredTables.map(t => t.name)
  return [...new Set(names)]
}

// 获取搜索过滤后的桌台列表（用于下拉显示）
function getSearchFilteredTables() {
  const tables = getFilteredTables()
  const query = tableSearchQuery.value.trim().toLowerCase()
  if (!query) return tables
  return tables.filter(t => t.toLowerCase().includes(query))
}

// 获取桌台状态
// hierarchy 里找不到 → 'unconfigured'（API 误录、桌台名不在配置里的脏数据用第四态显示）
// hierarchy 里找到但 status 字段为空 → fallback 'enabled'（兼容旧数据）
function getTableStatus(tableName) {
  const table = tableOptions.value.find(t => t.name === tableName)
  if (!table) return 'unconfigured'
  return table.status || 'enabled'
}

// 清除桌台搜索
function clearTableSearch() {
  tableSearchQuery.value = ''
}

// 游戏类型多选
function toggleGameTypeOption(gameType) {
  if (!formData.value.game_types) {
    formData.value.game_types = []
  }
  const idx = formData.value.game_types.indexOf(gameType)
  if (idx >= 0) {
    formData.value.game_types.splice(idx, 1)
  } else {
    formData.value.game_types.push(gameType)
  }
}

function isGameTypeSelected(gameType) {
  return formData.value.game_types?.includes(gameType) || false
}

// 全选/取消全选游戏类型
function selectAllGameTypes() {
  const gameTypes = getFilteredGameTypes()
  if (isAllGameTypesSelected()) {
    formData.value.game_types = []
  } else {
    formData.value.game_types = [...gameTypes]
  }
}

function isAllGameTypesSelected() {
  const gameTypes = getFilteredGameTypes()
  return gameTypes.length > 0 && formData.value.game_types?.length === gameTypes.length
}

// 获取选中桌台的游戏类型（根据选中的桌台自动筛选）
function getFilteredGameTypes() {
  const selectedTables = formData.value.affected_tables || []
  if (selectedTables.length === 0) {
    // 没有选择桌台时返回空数组（或可返回所有游戏类型）
    return []
  }
  
  // 获取选中桌台对应的所有游戏类型
  const gameTypesSet = new Set()
  tableOptions.value.forEach(t => {
    if (selectedTables.includes(t.name) && t.gameTypes?.length) {
      t.gameTypes.forEach(gt => gameTypesSet.add(gt))
    }
  })
  return [...gameTypesSet]
}

// 点击外部关闭多选下拉框
function handleClickOutside(e) {
  const closeIfOutside = (openRef, className) => {
    if (openRef.value) {
      const wrapper = document.querySelector(className)
      if (wrapper && !wrapper.contains(e.target)) {
        openRef.value = false
      }
    }
  }
  closeIfOutside(projectSelectOpen, '.project-select-wrapper')
  closeIfOutside(siteSelectOpen, '.site-select-wrapper')
  closeIfOutside(tableSelectOpen, '.table-select-wrapper')
  closeIfOutside(gameTypeSelectOpen, '.game-type-select-wrapper')
}

onMounted(() => {
  document.addEventListener('click', handleClickOutside)
})

onUnmounted(() => {
  document.removeEventListener('click', handleClickOutside)
})

function getProjectName(proj) {
  if (!proj) return ''
  if (typeof proj === 'string') return proj
  // 支持中英文名称
  const lang = appStore.language
  if (lang === 'en-US') {
    return proj.name_en || proj.name_zh || proj.name || ''
  } else {
    return proj.name_zh || proj.name || proj.name_en || ''
  }
}

function getProjectKey(proj) {
  if (!proj) return ''
  if (typeof proj === 'string') return proj
  return proj.name_zh || proj.name || ''
}

function getProjectCode(proj) {
  return typeof proj === 'string' ? proj.toLowerCase().replace(/\s+/g, '_') : (proj.code || getProjectKey(proj))
}

function getProjectDesc(proj) {
  return typeof proj === 'string' ? '' : (proj.description || '暂无描述')
}

function isProjectEnabled(proj) {
  return typeof proj === 'string' ? true : (proj.enabled !== false)
}

// 影响项目选项直接使用全局配置的项目列表
const enabledProjectOptions = computed(() => {
  return projectOptions.value || []
})

// 获取所有操作人列表（从记录中提取）
const operatorOptions = computed(() => {
  const operators = new Set()
  records.value.forEach(r => {
    if (r.operator) operators.add(r.operator)
  })
  return Array.from(operators).sort()
})

// 质检人候选（从已有记录中收集）
const inspectorOptions = computed(() => {
  const s = new Set()
  records.value.forEach(r => { if (r.inspector) s.add(r.inspector) })
  return Array.from(s).sort()
})

// 批量修改质检人
const showBatchInspectorModal = ref(false)
const batchInspectorValue = ref('')
const batchInspectorSaving = ref(false)

function openBatchInspectorModal() {
  if (selectedIds.value.length === 0) {
    appStore.showToast(t('common.warning'), 'warning')
    return
  }
  batchInspectorValue.value = ''
  showBatchInspectorModal.value = true
}

async function confirmBatchInspector() {
  const inspector = (batchInspectorValue.value || '').trim()
  if (!inspector) {
    appStore.showToast(t('common.warning'), 'warning')
    return
  }
  batchInspectorSaving.value = true
  let ok = 0, fail = 0
  try {
    for (const id of selectedIds.value) {
      const r = records.value.find(x => x.id === id)
      if (!r) { fail++; continue }
      // 构造 data：保留所有业务字段，覆盖 inspector
      const data = {}
      columns.value.forEach(col => {
        if (['checkbox', 'actions', 'created_by'].includes(col.key)) return
        if (col.type === 'attachments') {
          data[col.key] = r[col.key] || []
        } else if (col.key === 'inspector') {
          data[col.key] = inspector
        } else {
          data[col.key] = r[col.key] ?? ''
        }
      })
      const payload = {
        data: JSON.stringify(data),
        attachments: JSON.stringify(r.attachments || []),
      }
      try {
        await api.put(`/api/custom-tables/${tableId.value}/rows/${id}`, payload)
        ok++
      } catch (e) {
        fail++
      }
    }
    showBatchInspectorModal.value = false
    selectedIds.value = []
    appStore.showToast(`${t('tableMaintenance.actions.saveSuccess')} (${ok}/${ok + fail})`, ok && !fail ? 'success' : 'warning')
    loadRecords()
    loadStats()
  } finally {
    batchInspectorSaving.value = false
  }
}

// 获取所有操作类型列表
const operationOptions = computed(() => {
  // 返回 { key, label } 对：key 用于筛选匹配（走 isOp 兼容中英文），label 仅作展示
  return ['maintenance', 'cancel', 'recalculate', 'repayout', 'vipTable', 'missed', 'missedScreenshot']
    .map(k => ({ key: k, label: t(`tableMaintenance.options.operation.${k}`) }))
})

// 项目颜色列表
const projectColors = [
  '#3a84ff', '#22c55e', '#f97316', '#ef4444', '#8b5cf6', 
  '#ec4899', '#14b8a6', '#f59e0b', '#6366f1', '#10b981',
  '#f43f5e', '#0ea5e9', '#84cc16', '#a855f7', '#06b6d4'
]

function getProjectColor(idx) {
  return projectColors[idx % projectColors.length]
}

function getProjectColorByName(name) {
  if (!enabledProjectOptions.value) return '#6b7280'
  const idx = enabledProjectOptions.value.findIndex(p => getProjectKey(p) === name || getProjectName(p) === name)
  return idx >= 0 ? projectColors[idx % projectColors.length] : '#6b7280'
}

async function initTable() {
  try {
    const res = await api.get('/api/custom-tables')
    let table = (res.data || []).find(t => t.name === '桌台维护记录')
    if (!table) {
      const createRes = await api.post('/api/custom-tables', {
        name: '桌台维护记录',
        description: '桌台设备维护与问题记录',
        icon: 'table'
      })
      tableId.value = createRes.data?.id || ''
    } else {
      tableId.value = table.id
    }
    await loadStatsExcludeConfig()
    await loadRecords()
    loadStats()
  } catch (e) {
    console.error('初始化失败', e)
  }
}

async function loadRecords() {
  if (!tableId.value) return
  loading.value = true
  try {
    const res = await api.get(`/api/custom-tables/${tableId.value}`)
    const rows = res.data?.rows || []
    records.value = rows.map(r => ({
      id: r.id,
      ...parseData(r.data),
      attachments: parseAttachments(r.attachments),
      source_api_key_id: r.source_api_key_id,
      created_by: r.created_by || '-',
      created_at: r.created_at,
      updated_at: r.updated_at
    }))
    // v605: 只 presign 当前可见页（watch filteredRecords 接管），
    // 之前全量 presign 1639 条的 3000+ 张图导致 429。
  } catch (e) {
    records.value = []
  } finally {
    loading.value = false
  }
}

// v605: 监听当前可见页，按需 presign，缓存命中跳过
watch(() => filteredRecords.value, async (rows) => {
  if (!rows?.length) return
  const visibleAttachments = []
  for (const r of rows) {
    if (r.attachments?.length) visibleAttachments.push(...r.attachments)
    attachmentColumns.value.forEach(col => {
      const atts = getAttachmentsByKey(r, col.key)
      if (atts?.length) visibleAttachments.push(...atts)
    })
  }
  if (visibleAttachments.length > 0) {
    await loadPresignedUrls(visibleAttachments)
  }
}, { immediate: false, flush: 'post' })

function parseData(data) {
  if (!data) return {}
  if (typeof data === 'string') {
    try { return JSON.parse(data) } catch { return {} }
  }
  return data
}

function parseAttachments(att) {
  if (!att) return []
  if (typeof att === 'string') {
    try { return JSON.parse(att) } catch { return [] }
  }
  return Array.isArray(att) ? att : []
}

function parseMultiSelect(val) {
  if (!val) return []
  if (Array.isArray(val)) return val
  if (typeof val === 'string') {
    try { return JSON.parse(val) } catch { return val.split(',').map(s => s.trim()).filter(Boolean) }
  }
  return []
}

function parseMultiSelectSync(val) {
  if (!val) return []
  if (Array.isArray(val)) return val
  if (typeof val === 'string') {
    try { return JSON.parse(val) } catch { return val.split(',').map(s => s.trim()).filter(Boolean) }
  }
  return []
}

async function loadStats() {
  if (!tableId.value) return
  try {
    const res = await api.get(`/api/custom-tables/${tableId.value}/stats`)
    stats.value = res.data || { total_rows: 0, today_rows: 0, week_rows: 0, month_rows: 0 }
  } catch (e) {
    stats.value = { total_rows: 0, today_rows: 0, week_rows: 0, month_rows: 0 }
  }
}

async function loadPresignedUrls(attachments) {
  // v605: 串行 + 大 batch（50→500），避免 Promise.all 并发触发后端 rate limit 429
  // BATCH_SIZE 受后端 v751 file_share.go HandleBatchPresignedURL 硬限制 ≤ 500
  const paths = attachments.filter(a => a.path && !presignedUrlCache.value[a.path]).map(a => a.path)
  if (paths.length === 0) return
  const BATCH_SIZE = 500
  for (let i = 0; i < paths.length; i += BATCH_SIZE) {
    const batch = paths.slice(i, i + BATCH_SIZE)
    try {
      const res = await api.post('/api/storage/presign/batch', { paths: batch })
      const urls = res.data?.urls || {}
      Object.entries(urls).forEach(([path, url]) => {
        presignedUrlCache.value[path] = url
      })
    } catch (e) {
      console.error('获取预签名URL失败', e)
    }
  }
}

function getPresignedUrl(path) {
  return presignedUrlCache.value[path] || ''
}

function openAddModal() {
  modalMode.value = 'add'
  formData.value = getEmptyForm()
  currentAttachments.value = []
  initFormAttachments()
  showModal.value = true
}

function openEditModal(record) {
  modalMode.value = 'edit'
  const form = { id: record.id }
  columns.value.forEach(col => {
    if (['checkbox', 'actions', 'created_by'].includes(col.key)) return
    if (col.type === 'attachments') {
      form[col.key] = getAttachmentsByKey(record, col.key)
    } else {
      form[col.key] = record[col.key] || ''
    }
  })
  formData.value = form
  currentAttachments.value = record.attachments || []
  const atts = {}
  attachmentColumns.value.forEach(col => {
    atts[col.key] = getAttachmentsByKey(record, col.key)
  })
  formAttachments.value = atts
  showModal.value = true
}

function openDetail(record) {
  detailRecord.value = record
  showDetailModal.value = true
}

async function saveRecord() {
  const requiredCols = columns.value.filter(c => c.required && !['checkbox', 'actions', 'attachments'].includes(c.key) && c.type !== 'attachments')
  for (const col of requiredCols) {
    const val = formData.value[col.key]
    if (!val || (Array.isArray(val) && val.length === 0)) {
      appStore.showToast(`请填写${col.title}`, 'error')
      return
    }
  }
  // 质检相关验证：填了质检人则必须填质检状态
  if (formData.value.inspector && !formData.value.qc_status) {
    appStore.showToast('填写了质检人，必须选择质检状态', 'error')
    return
  }
  // 质检状态为异常时必须填写备注
  if (isQc(formData.value.qc_status, 'abnormal') && !formData.value.remark?.trim()) {
    appStore.showToast(t('tableMaintenance.placeholders.remarkRequired'), 'error')
    return
  }
  // 操作为维护时，维护类型必填且不能为「无」
  if (isOp(formData.value.operation, 'maintenance')) {
    const mt = formData.value.maintenance_type
    if (!mt || isMaintType(mt, 'none')) {
      appStore.showToast(t('tableMaintenance.form.maintTypeRequired'), 'error')
      return
    }
  }
  const data = {}
  columns.value.forEach(col => {
    if (['checkbox', 'actions', 'created_by'].includes(col.key)) return
    if (col.type === 'attachments') {
      data[col.key] = formAttachments.value[col.key] || []
    } else if (col.type === 'datetime') {
      // datetime-local 控件返回 "YYYY-MM-DDTHH:MM"，统一归一为空格分隔，避免和脚本/旧数据混排
      const v = formData.value[col.key] || ''
      data[col.key] = typeof v === 'string' ? v.replace('T', ' ') : v
    } else {
      data[col.key] = formData.value[col.key] || ''
    }
  })
  const payload = {
    data: JSON.stringify(data),
    attachments: JSON.stringify(currentAttachments.value)
  }
  try {
    if (modalMode.value === 'add') {
      await api.post(`/api/custom-tables/${tableId.value}/rows`, payload)
      appStore.showToast(t('tableMaintenance.actions.saveSuccess'), 'success')
    } else {
      await api.put(`/api/custom-tables/${tableId.value}/rows/${formData.value.id}`, payload)
      appStore.showToast(t('tableMaintenance.actions.saveSuccess'), 'success')
    }
    showModal.value = false
    loadRecords()
    loadStats()
  } catch (e) {
    appStore.showToast(t('tableMaintenance.actions.saveFailed'), 'error')
  }
}

async function deleteRecord(record) {
  const confirmed = await appStore.showConfirm({
    type: 'warning',
    title: t('common.confirm'),
    message: t('tableMaintenance.actions.confirmDelete'),
    okText: t('common.delete'),
    cancelText: t('common.cancel')
  })
  if (!confirmed) return
  try {
    await api.delete(`/api/custom-tables/${tableId.value}/rows/${record.id}`)
    appStore.showToast(t('tableMaintenance.actions.deleteSuccess'), 'success')
    loadRecords()
    loadStats()
  } catch (e) {
    appStore.showToast(t('tableMaintenance.actions.deleteFailed'), 'error')
  }
}

async function batchDelete() {
  if (selectedIds.value.length === 0) {
    appStore.showToast(t('common.warning'), 'warning')
    return
  }
  const confirmed = await appStore.showConfirm({
    type: 'warning',
    title: t('common.confirm'),
    message: t('tableMaintenance.actions.confirmBatchDelete', { count: selectedIds.value.length }),
    okText: t('common.delete'),
    cancelText: t('common.cancel')
  })
  if (!confirmed) return
  try {
    for (const id of selectedIds.value) {
      await api.delete(`/api/custom-tables/${tableId.value}/rows/${id}`)
    }
    appStore.showToast(t('tableMaintenance.actions.deleteSuccess'), 'success')
    selectedIds.value = []
    loadRecords()
    loadStats()
  } catch (e) {
    appStore.showToast(t('tableMaintenance.actions.deleteFailed'), 'error')
  }
}

function triggerFileInput() {
  fileInput.value?.click()
}

async function handleFileSelect(e) {
  const files = e.target?.files || e.dataTransfer?.files
  if (!files?.length) return
  await uploadFiles(Array.from(files))
  if (fileInput.value) fileInput.value.value = ''
}

function handleDragOver(e) {
  e.preventDefault()
  dragOver.value = true
}

function handleDragLeave() {
  dragOver.value = false
}

function handleDrop(e) {
  e.preventDefault()
  dragOver.value = false
  handleFileSelect(e)
}

async function uploadFiles(files) {
  if (!files.length) return
  const validFiles = files.filter(file => {
    if (file.size === 0) {
      appStore.showToast(t('tableMaintenance.upload.empty', { name: file.name || '' }), 'error')
      return false
    }
    return true
  })
  if (!validFiles.length) return
  uploading.value = true
  const fd = new FormData()
  validFiles.forEach(file => fd.append('files', file))
  try {
    const res = await api.post('/api/storage/upload', fd, {
      headers: { 'Content-Type': 'multipart/form-data' }
    })
    const uploaded = res.data?.files || []
    uploaded.forEach(f => {
      currentAttachments.value.push({
        name: f.original_name || f.filename,
        path: f.path,
        size: f.size,
        type: f.content_type
      })
    })
    appStore.showToast(t('common.success'), 'success')
  } catch (e) {
    appStore.showToast(t('common.failed'), 'error')
  } finally {
    uploading.value = false
  }
}

function removeAttachment(index) {
  currentAttachments.value.splice(index, 1)
}

function triggerAttachmentInput(key) {
  const input = attachmentInputRefs.value[key]
  if (input) input.click()
}

async function handleAttachmentSelect(e, key) {
  const files = Array.from(e.target.files || [])
  if (files.length === 0) return
  await uploadAttachmentsForKey(files, key)
  e.target.value = ''
}

async function handleAttachmentDrop(e, key) {
  e.currentTarget.classList.remove('dragover')
  const files = Array.from(e.dataTransfer?.files || [])
  if (files.length === 0) return
  await uploadAttachmentsForKey(files, key)
}

async function handleAttachmentPaste(e, key) {
  const items = e.clipboardData?.items
  if (!items) return

  const files = []
  for (let i = 0; i < items.length; i++) {
    if (items[i].type.startsWith('image/')) {
      const file = items[i].getAsFile()
      if (file) files.push(file)
    }
  }

  if (files.length > 0) {
    e.preventDefault()
    await uploadAttachmentsForKey(files, key)
  }
}

async function uploadAttachmentsForKey(files, key) {
  uploading.value = true
  try {
    for (const file of files) {
      if (file.size === 0) {
        appStore.showToast(t('tableMaintenance.upload.empty', { name: file.name || '' }), 'error')
        continue
      }
      const fd = new FormData()
      fd.append('file', file)
      const res = await api.post('/api/storage/upload', fd, {
        headers: { 'Content-Type': 'multipart/form-data' }
      })
      if (res.data?.path) {
        if (!formAttachments.value[key]) formAttachments.value[key] = []
        formAttachments.value[key].push({
          name: file.name,
          size: file.size,
          path: res.data.path,
          preview: URL.createObjectURL(file)
        })
      }
    }
  } catch (err) {
    console.error('Upload error:', err)
    appStore.showToast(t('common.failed'), 'error')
  } finally {
    uploading.value = false
  }
}

function removeFormAttachment(key, index) {
  if (formAttachments.value[key]) {
    formAttachments.value[key].splice(index, 1)
  }
}

function previewImage(attachments, index) {
  const images = attachments.filter(a => isImageFile(a.name || a.path))
  if (images.length === 0) return
  previewImages.value = images.map(a => getPresignedUrl(a.path))
  previewIndex.value = Math.min(index, images.length - 1)
  showImagePreview.value = true
}

function previewFormImage(key, index) {
  const attachments = formAttachments.value[key] || []
  const images = attachments.filter(a => isImageFile(a.name))
  if (images.length === 0) return
  previewImages.value = images.map(a => getPresignedUrl(a.path) || a.preview)
  previewIndex.value = Math.min(index, images.length - 1)
  showImagePreview.value = true
}

function previewAttachments(attachments, startIndex = 0) {
  if (!attachments || attachments.length === 0) return
  const images = attachments.filter(a => isImageFile(a.name || a.path))
  if (images.length === 0) return
  previewImages.value = images.map(a => getPresignedUrl(a.path))
  previewIndex.value = Math.min(startIndex, images.length - 1)
  showImagePreview.value = true
}

function isImageFile(name) {
  return /\.(jpg|jpeg|png|gif|webp|bmp)$/i.test(name)
}

function formatFileSize(bytes) {
  if (!bytes) return '-'
  if (bytes < 1024) return bytes + ' B'
  if (bytes < 1024 * 1024) return (bytes / 1024).toFixed(1) + ' KB'
  return (bytes / 1024 / 1024).toFixed(2) + ' MB'
}

function formatDate(dateStr) {
  if (!dateStr) return '-'
  return dateStr.slice(0, 10)
}

function formatDateTime(dateStr) {
  if (!dateStr) return '-'
  if (dateStr.length > 16) return dateStr.slice(0, 16).replace('T', ' ')
  return dateStr.replace('T', ' ')
}

function getAttachmentsByKey(record, key) {
  if (key === 'attachments') return record.attachments || []
  const data = record[key]
  if (!data) return []
  if (Array.isArray(data)) return data
  if (typeof data === 'string') {
    try { return JSON.parse(data) } catch { return [] }
  }
  return []
}

function getStatusClass(status) {
  const map = { '待处理': 'pending', '处理中': 'processing', '已解决': 'resolved' }
  return map[status] || ''
}

function getIssueTypeClass(type) {
  const map = { '设备故障': 'device', '清洁问题': 'clean', '物品损坏': 'damage', '桌面磨损': 'wear', '配件缺失': 'missing', '其他': 'other' }
  return map[type] || 'other'
}

function applyFilters() {
  searchQuery.value = filterInput.value.search
  selectedStatus.value = filterInput.value.status
  dateRange.value = { start: filterInput.value.dateStart, end: filterInput.value.dateEnd }
  selectedProject.value = filterInput.value.project
  selectedDuration.value = filterInput.value.duration
  selectedOperator.value = filterInput.value.operator
  selectedOperation.value = filterInput.value.operation
  selectedTableStatus.value = filterInput.value.tableStatus
  selectedMaintType.value = filterInput.value.maintType
  currentPage.value = 1
}

function resetFilters() {
  filterInput.value = { search: '', status: '', dateStart: '', dateEnd: '', project: '', duration: '', operator: '', operation: '', tableStatus: '', maintType: '' }
  searchQuery.value = ''
  selectedStatus.value = ''
  dateRange.value = { start: '', end: '' }
  selectedProject.value = ''
  selectedDuration.value = ''
  selectedOperator.value = ''
  selectedOperation.value = ''
  selectedTableStatus.value = ''
  selectedMaintType.value = ''
  currentPage.value = 1
}

async function exportToExcel() {
  const data = allFilteredRecords.value
  if (!data.length) {
    appStore.showToast(t('common.warning'), 'warning')
    return
  }

  appStore.showToast('正在准备导出...', 'info')

  // 动态导入xlsx库
  const XLSX = await import('xlsx')

  // 按字段顺序导出，排除 checkbox、actions 和 attachments（不导出图片）
  const exportCols = columns.value.filter(c => !['checkbox', 'actions'].includes(c.key) && c.type !== 'checkbox' && c.type !== 'actions' && c.type !== 'attachments')
  const headers = exportCols.map(c => c.title)
  
  // 构建数据行
  const rows = data.map(r => {
    const row = {}
    exportCols.forEach(c => {
      const val = r[c.key]
      
      // 处理数组类型（多选字段）
      if (Array.isArray(val)) {
        row[c.title] = val.join(', ')
        return
      }
      
      row[c.title] = val ?? ''
    })
    return row
  })

  // 创建工作簿和工作表
  const ws = XLSX.utils.json_to_sheet(rows, { header: headers })
  
  // 设置列宽
  const colWidths = exportCols.map(c => {
    if (c.type === 'textarea') return { wch: 30 }
    return { wch: 15 }
  })
  ws['!cols'] = colWidths

  const wb = XLSX.utils.book_new()
  XLSX.utils.book_append_sheet(wb, ws, t('tableMaintenance.statsPanel.csvFileName'))

  // 导出为xlsx文件
  XLSX.writeFile(wb, `${t('tableMaintenance.statsPanel.csvFileName')}_${todayLocal()}.xlsx`)

  appStore.showToast(t('tableMaintenance.actions.exportSuccess'), 'success')
}
</script>

<template>
  <div class="table-maintenance-page" :key="currentLanguage">
    <div class="page-header">
      <div class="header-left">
        <h2>{{ t('tableMaintenance.pageTitle') }}</h2>
      </div>
      <div class="header-actions">
        <button v-if="selectedIds.length > 0" class="btn btn-secondary" @click="openBatchInspectorModal" style="margin-right:8px;">
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" style="width:16px;height:16px;margin-right:4px;"><path d="M20 21v-2a4 4 0 0 0-4-4H8a4 4 0 0 0-4 4v2"/><circle cx="12" cy="7" r="4"/></svg>
          {{ t('tableMaintenance.actions.batchModifyInspector') }} ({{ selectedIds.length }})
        </button>
        <button v-if="selectedIds.length > 0" class="btn btn-danger" @click="batchDelete">
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><polyline points="3,6 5,6 21,6"/><path d="M19,6v14a2,2,0,0,1-2,2H7a2,2,0,0,1-2-2V6M8,6V4a2,2,0,0,1,2-2h4a2,2,0,0,1,2,2v2"/></svg>
          {{ t('tableMaintenance.deleteSelected') }} ({{ selectedIds.length }})
        </button>
        <button v-if="canExport" class="btn btn-secondary" @click="exportToExcel">
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M21 15v4a2 2 0 01-2 2H5a2 2 0 01-2-2v-4M7 10l5 5 5-5M12 15V3"/></svg>
          {{ t('tableMaintenance.exportExcel') }}
        </button>
        <button v-if="isSuperAdmin || authStore.hasPermission('table_hierarchy:create') || authStore.hasPermission('table_hierarchy:update')" class="btn btn-secondary" @click="goToHierarchyConfig">
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M3 3h18v18H3z"/><path d="M3 9h18"/><path d="M9 21V9"/></svg>
          {{ t('tableMaintenance.hierarchyConfig') }}
        </button>
        <button v-if="canCreate" class="btn btn-primary" @click="openAddModal">
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M12 5v14M5 12h14"/></svg>
          {{ t('tableMaintenance.addRecord') }}
        </button>
        <button v-if="canCreate && copiedRecord" class="btn btn-secondary" @click="openPasteModal">
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M16 4h2a2 2 0 0 1 2 2v14a2 2 0 0 1-2 2H6a2 2 0 0 1-2-2V6a2 2 0 0 1 2-2h2"/><rect x="8" y="2" width="8" height="4" rx="1" ry="1"/></svg>
          {{ t('tableMaintenance.pasteRecord') }}
        </button>
      </div>
    </div>

    <!-- Tab 切换 -->
    <div class="tab-nav">
      <button class="tab-btn" :class="{ active: activeTab === 'list' }" @click="activeTab = 'list'">
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M8 6h13M8 12h13M8 18h13M3 6h.01M3 12h.01M3 18h.01"/></svg>
        {{ t('tableMaintenance.tabs.list') }}
      </button>
      <button class="tab-btn" :class="{ active: activeTab === 'stats' }" @click="activeTab = 'stats'">
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M18 20V10M12 20V4M6 20v-6"/></svg>
        {{ t('tableMaintenance.tabs.stats') }}
      </button>
    </div>

    <!-- 记录列表 Tab -->
    <div v-if="activeTab === 'list'" class="tab-content">
    <div class="stats-cards">
      <div class="stat-card">
        <div class="stat-icon total"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M3 3h18v18H3z"/><path d="M9 9h6v6H9z"/></svg></div>
        <div class="stat-info"><span class="stat-label">{{ t('tableMaintenance.stats.total') }}</span><span class="stat-value">{{ stats.total_rows }}</span></div>
      </div>
      <div class="stat-card">
        <div class="stat-icon today"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><circle cx="12" cy="12" r="10"/><path d="M12 6v6l4 2"/></svg></div>
        <div class="stat-info"><span class="stat-label">{{ t('tableMaintenance.stats.today') }}</span><span class="stat-value">{{ stats.today_rows }}</span></div>
      </div>
      <div class="stat-card">
        <div class="stat-icon week"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><rect x="3" y="4" width="18" height="18" rx="2"/><path d="M16 2v4M8 2v4M3 10h18"/></svg></div>
        <div class="stat-info"><span class="stat-label">{{ t('tableMaintenance.stats.week') }}</span><span class="stat-value">{{ stats.week_rows }}</span></div>
      </div>
      <div class="stat-card">
        <div class="stat-icon month"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M18 20V10M12 20V4M6 20v-6"/></svg></div>
        <div class="stat-info"><span class="stat-label">{{ t('tableMaintenance.stats.month') }}</span><span class="stat-value">{{ stats.month_rows }}</span></div>
      </div>
    </div>

    <!-- 卡片分组筛选器 -->
    <div class="filter-container">
      <!-- 搜索卡片 -->
      <div class="search-card">
        <div class="search-main">
          <div class="search-input-wrapper">
            <svg class="search-icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
              <circle cx="11" cy="11" r="8"/><path d="m21 21-4.35-4.35"/>
            </svg>
            <input type="text" v-model="filterInput.search" :placeholder="t('tableMaintenance.filters.searchAllPlaceholder')" @keyup.enter="applyFilters">
          </div>
          <button class="btn-search" @click="applyFilters">
            <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
              <circle cx="11" cy="11" r="8"/><path d="m21 21-4.35-4.35"/>
            </svg>
            {{ t('tableMaintenance.filters.search') }}
          </button>
        </div>
      </div>
      
      <!-- 筛选条件卡片 -->
      <div class="filters-card">
        <div class="filters-header">
          <span class="filters-title">
            <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
              <polygon points="22 3 2 3 10 12.46 10 19 14 21 14 12.46 22 3"/>
            </svg>
            {{ t('tableMaintenance.filters.filterConditions') }}
          </span>
          <button class="btn-reset" @click="resetFilters">{{ t('tableMaintenance.filters.reset') }}</button>
        </div>
        <div class="filters-grid">
          <div class="filter-field">
            <label>{{ t('tableMaintenance.filters.qcStatusLabel') }}</label>
            <select v-model="filterInput.status">
              <option value="">{{ t('tableMaintenance.filters.allQcStatus') }}</option>
              <option value="normal">{{ t('tableMaintenance.filters.normal') }}</option>
              <option value="abnormal">{{ t('tableMaintenance.filters.abnormal') }}</option>
              <option value="pending">{{ t('tableMaintenance.filters.notInspected') }}</option>
            </select>
          </div>
          <div class="filter-field">
            <label>{{ t('tableMaintenance.filters.projectLabel') }}</label>
            <select v-model="filterInput.project">
              <option value="">{{ t('tableMaintenance.filters.allProjects') }}</option>
              <option v-for="proj in projectOptions" :key="getProjectKey(proj)" :value="getProjectKey(proj)">{{ getProjectName(proj) }}</option>
            </select>
          </div>
          <div class="filter-field">
            <label>{{ t('tableMaintenance.filters.durationLabel') }}</label>
            <select v-model="filterInput.duration">
              <option value="">{{ t('tableMaintenance.filters.allDurations') }}</option>
              <option :value="t('tableMaintenance.options.duration.twoMin')">{{ t('tableMaintenance.options.duration.twoMin') }}</option>
              <option :value="t('tableMaintenance.options.duration.fiveMin')">{{ t('tableMaintenance.options.duration.fiveMin') }}</option>
              <option :value="t('tableMaintenance.options.duration.tenMin')">{{ t('tableMaintenance.options.duration.tenMin') }}</option>
              <option :value="t('tableMaintenance.options.duration.overTenMin')">{{ t('tableMaintenance.options.duration.overTenMin') }}</option>
            </select>
          </div>
          <div class="filter-field">
            <label>{{ t('tableMaintenance.filters.operatorLabel') }}</label>
            <select v-model="filterInput.operator">
              <option value="">{{ t('tableMaintenance.filters.allOperators') }}</option>
              <option v-for="op in operatorOptions" :key="op" :value="op">{{ op }}</option>
            </select>
          </div>
          <div class="filter-field">
            <label>{{ t('tableMaintenance.filters.operationLabel') }}</label>
            <select v-model="filterInput.operation">
              <option value="">{{ t('tableMaintenance.filters.allOperations') }}</option>
              <option v-for="op in operationOptions" :key="op.key" :value="op.key">{{ op.label }}</option>
            </select>
          </div>
          <div class="filter-field">
            <label>{{ t('tableMaintenance.filters.tableStatusLabel') }}</label>
            <select v-model="filterInput.tableStatus">
              <option value="">{{ t('tableMaintenance.filters.allTableStatus') }}</option>
              <option value="enabled">{{ t('tableHierarchy.status.enabled') }}</option>
              <option value="disabled">{{ t('tableHierarchy.status.disabled') }}</option>
              <option value="pending">{{ t('tableHierarchy.status.pending') }}</option>
              <option value="unconfigured">{{ t('tableHierarchy.status.unconfigured') }}</option>
            </select>
          </div>
          <div class="filter-field">
            <label>{{ t('tableMaintenance.columns.maintenanceType') }}</label>
            <select v-model="filterInput.maintType">
              <option value="">{{ t('common.all') }}</option>
              <option value="routine">{{ t('tableMaintenance.options.maintenanceType.routine') }}</option>
              <option value="temporary">{{ t('tableMaintenance.options.maintenanceType.temporary') }}</option>
              <option value="emergency">{{ t('tableMaintenance.options.maintenanceType.emergency') }}</option>
            </select>
          </div>
          <div class="filter-field date-field">
            <label>{{ t('tableMaintenance.filters.dateRangeLabel') }}</label>
            <div class="date-inputs">
              <input type="date" v-model="filterInput.dateStart">
              <span class="date-separator">{{ t('tableMaintenance.filters.dateTo') }}</span>
              <input type="date" v-model="filterInput.dateEnd">
            </div>
          </div>
        </div>
      </div>
    </div>

    <div class="table-wrapper">
      <div class="table-container">
        <table class="data-table">
          <thead>
            <tr>
              <th v-for="col in visibleColumns" :key="col.key" :style="{ width: col.width + 'px', minWidth: col.width + 'px' }" :class="{ 'sticky-col': col.key === 'actions' }">
                <template v-if="col.type === 'checkbox'">
                  <input type="checkbox" v-model="allSelected">
                </template>
                <template v-else>{{ col.title }}</template>
              </th>
            </tr>
          </thead>
          <tbody>
            <tr v-if="loading"><td :colspan="visibleColumns.length" class="empty">{{ t('tableMaintenance.table.loading') }}</td></tr>
            <tr v-else-if="filteredRecords.length === 0"><td :colspan="visibleColumns.length" class="empty">{{ t('tableMaintenance.table.empty') }}</td></tr>
            <tr v-for="r in filteredRecords" :key="r.id" :class="{ selected: selectedIds.includes(r.id), 'row-locked': rowLocked(r) }">
              <template v-for="col in visibleColumns" :key="col.key">
                <td v-if="col.type === 'checkbox'">
                  <input type="checkbox" :checked="selectedIds.includes(r.id)" :disabled="rowLocked(r)" @change="e => { if (e.target.checked) selectedIds.push(r.id); else selectedIds = selectedIds.filter(i => i !== r.id) }">
                </td>
                <td v-else-if="col.type === 'date'">
                  <span class="cell-date">{{ formatDate(r[col.key]) }}</span>
                </td>
                <td v-else-if="col.type === 'datetime'">
                  <span class="cell-date">{{ formatDateTime(r[col.key]) }}</span>
                </td>
                <td v-else-if="col.type === 'tag'">
                  <span class="tag-table">{{ r[col.key] || '-' }}</span>
                </td>
                <td v-else-if="col.type === 'tag-type'">
                  <span class="tag-issue" :class="'tag-issue-' + getIssueTypeClass(r[col.key])">{{ r[col.key] || '-' }}</span>
                </td>
                <td v-else-if="col.type === 'status'">
                  <span class="status-tag" :class="'status-' + getStatusClass(r[col.key])">{{ r[col.key] || '-' }}</span>
                </td>
                <td v-else-if="col.type === 'select'">
                  <span class="cell-select">{{ col.key === 'operation' ? (displayOp(r[col.key]) || '-') : (r[col.key] || '-') }}</span>
                </td>
                <td v-else-if="col.type === 'duration-select'">
                  <span class="duration-tag" :class="getDurationClass(r[col.key])">{{ displayDur(r[col.key]) || '-' }}</span>
                </td>
                <td v-else-if="col.type === 'maint-type-select'">
                  <span class="cell-select">{{ displayMaintType(r[col.key]) || '-' }}</span>
                </td>
                <td v-else-if="col.type === 'yes-no'">
                  <span class="yes-no-tag" :class="r[col.key] === '是' ? 'tag-yes' : 'tag-no'">{{ r[col.key] || '-' }}</span>
                </td>
                <td v-else-if="col.type === 'multi-select-projects'">
                  <div class="multi-select-tags" v-if="parseMultiSelect(r[col.key])?.length">
                    <span v-for="(item, idx) in parseMultiSelect(r[col.key])" :key="idx" class="multi-tag" :style="{ backgroundColor: getProjectColorByName(item) + '18', color: getProjectColorByName(item), borderColor: getProjectColorByName(item) + '40' }">{{ item }}</span>
                  </div>
                  <span v-else class="cell-muted">-</span>
                </td>
                <td v-else-if="col.type === 'multi-select-tables'">
                  <div class="multi-select-tags" v-if="parseMultiSelect(r[col.key])?.length">
                    <span v-for="(item, idx) in parseMultiSelect(r[col.key])" :key="idx" class="multi-tag" :class="{ 'table-disabled': getTableStatus(item) === 'disabled' }" :style="{ backgroundColor: getProjectColorByName(item) + '18', color: getProjectColorByName(item), borderColor: getProjectColorByName(item) + '40' }">{{ item }}<span v-if="getTableStatus(item) === 'disabled'" class="status-tag-disabled-inline">{{ t('tableHierarchy.status.disabled') }}</span><span v-else-if="getTableStatus(item) === 'pending'" class="status-tag-pending-inline">{{ t('tableHierarchy.status.pending') }}</span><span v-else-if="getTableStatus(item) === 'unconfigured'" class="status-tag-unconfigured-inline">{{ t('tableHierarchy.status.unconfigured') }}</span></span>
                  </div>
                  <span v-else class="cell-muted">-</span>
                </td>
                <td v-else-if="col.type === 'table-status-display'">
                  <div class="multi-select-tags" v-if="parseMultiSelect(r.affected_tables)?.length">
                    <span v-for="(item, idx) in parseMultiSelect(r.affected_tables)" :key="idx" :class="['status-tag', 'status-tag-' + getTableStatus(item)]">{{ item }}: {{ t('tableHierarchy.status.' + getTableStatus(item)) }}</span>
                  </div>
                  <span v-else class="cell-muted">-</span>
                </td>
                <td v-else-if="col.type === 'multi-select-sites'">
                  <div class="multi-select-tags" v-if="parseMultiSelect(r[col.key])?.length">
                    <span v-for="(item, idx) in parseMultiSelect(r[col.key])" :key="idx" class="multi-tag" :style="{ backgroundColor: getProjectColorByName(item) + '18', color: getProjectColorByName(item), borderColor: getProjectColorByName(item) + '40' }">{{ getSiteDisplayName(item) }}</span>
                  </div>
                  <span v-else class="cell-muted">-</span>
                </td>
                <td v-else-if="col.type === 'multi-select-game-types'">
                  <div class="multi-select-tags" v-if="parseMultiSelect(r[col.key])?.length">
                    <span v-for="(item, idx) in parseMultiSelect(r[col.key])" :key="idx" class="multi-tag game-type-multi-tag">{{ item }}</span>
                  </div>
                  <span v-else class="cell-muted">-</span>
                </td>
                <td v-else-if="col.type === 'qc-status'">
                  <span class="qc-status-tag" :class="isQc(r[col.key], 'normal') ? 'qc-normal' : isQc(r[col.key], 'abnormal') ? 'qc-abnormal' : ''">{{ displayQc(r[col.key]) || '-' }}</span>
                </td>
                <td v-else-if="col.type === 'readonly'" class="cell-muted">
                  {{ r[col.key] || '-' }}
                </td>
                <td v-else-if="col.type === 'datetime-readonly'" class="cell-datetime">
                  {{ formatDateTime(r[col.key]) }}
                </td>
                <td v-else-if="col.type === 'attachments'" @click.stop>
<div v-if="getAttachmentsByKey(r, col.key)?.length" class="attachments-preview">
                    <img v-for="(att, idx) in getAttachmentsByKey(r, col.key).slice(0, 2)"
                         :key="idx"
                         :src="getPresignedUrl(att.path)"
                         @click="previewAttachments(getAttachmentsByKey(r, col.key), idx)"
                         class="thumb"
                         :title="att.name">
                    <span v-if="getAttachmentsByKey(r, col.key).length > 2"
                          class="more-count"
                          @click="previewAttachments(getAttachmentsByKey(r, col.key), 2)">
                      +{{ getAttachmentsByKey(r, col.key).length - 2 }}
                    </span>
                  </div>
                  <span v-else class="cell-muted">-</span>
                </td>
                <td v-else-if="col.type === 'actions'" class="action-cell sticky-col">
                  <div class="action-btns">
                    <button class="action-btn" @click="openDetail(r)" :title="t('tableMaintenance.actions.view')">
                      <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M1 12s4-8 11-8 11 8 11 8-4 8-11 8-11-8-11-8z"/><circle cx="12" cy="12" r="3"/></svg>
                    </button>
                    <button v-if="canCreate" class="action-btn copy-btn" @click="copyRecord(r)" :title="t('tableMaintenance.actions.copy')">
                      <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><rect x="9" y="9" width="13" height="13" rx="2" ry="2"/><path d="M5 15H4a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2h9a2 2 0 0 1 2 2v1"/></svg>
                    </button>
                    <button v-if="rowEditable(r)" class="action-btn" @click="openEditModal(r)" :title="t('tableMaintenance.actions.edit')">
                      <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M11 4H4a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2h14a2 2 0 0 0 2-2v-7"/><path d="M18.5 2.5a2.121 2.121 0 0 1 3 3L12 15l-4 1 1-4 9.5-9.5z"/></svg>
                    </button>
                    <button v-if="rowDeletable(r)" class="action-btn danger" @click="deleteRecord(r)" :title="t('tableMaintenance.actions.delete')">
                      <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><polyline points="3,6 5,6 21,6"/><path d="M19,6v14a2,2,0,0,1-2,2H7a2,2,0,0,1-2-2V6M8,6V4a2,2,0,0,1,2-2h4a2,2,0,0,1,2,2v2"/></svg>
                    </button>
                    <span v-if="rowLocked(r)" class="lock-icon" title="此记录由 API Key 创建，需要权限：编辑/删除 API Key 创建的记录">🔒</span>
                  </div>
                </td>
                <td v-else-if="col.type === 'textarea'"><span class="cell-text cell-ellipsis" :title="r[col.key]">{{ r[col.key] || '-' }}</span></td>
                <td v-else><span class="cell-text">{{ r[col.key] || '-' }}</span></td>
              </template>
            </tr>
          </tbody>
        </table>
      </div>
    </div>

    <div class="pagination">
      <div class="pagination-info">
        {{ t('tableMaintenance.table.pagination.total') }} <strong>{{ allFilteredRecords.length }}</strong> {{ t('tableMaintenance.table.pagination.records') }}
      </div>
      <div class="pagination-size">
        <span>{{ t('tableMaintenance.table.pagination.perPage') }}</span>
        <select :value="pageSize" @change="onPageSizeChange(Number($event.target.value))" class="page-size-select">
          <option v-for="size in pageSizeOptions" :key="size" :value="size">{{ size }}</option>
        </select>
        <span>{{ t('tableMaintenance.table.pagination.items') }}</span>
      </div>
      <div class="pagination-controls">
        <button class="page-btn" @click="goToPage(1)" :disabled="currentPage <= 1">
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><polyline points="11 17 6 12 11 7"/><polyline points="18 17 13 12 18 7"/></svg>
        </button>
        <button class="page-btn" @click="goToPage(currentPage - 1)" :disabled="currentPage <= 1">
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><polyline points="15 18 9 12 15 6"/></svg>
        </button>
        <span class="page-indicator">{{ currentPage }} / {{ totalPages }}</span>
        <button class="page-btn" @click="goToPage(currentPage + 1)" :disabled="currentPage >= totalPages">
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><polyline points="9 18 15 12 9 6"/></svg>
        </button>
        <button class="page-btn" @click="goToPage(totalPages)" :disabled="currentPage >= totalPages">
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><polyline points="13 17 18 12 13 7"/><polyline points="6 17 11 12 6 7"/></svg>
        </button>
      </div>
    </div>
    </div><!-- 列表 Tab 结束 -->

    <!-- 统计分析 Tab -->
    <div v-if="activeTab === 'stats'" class="tab-content stats-tab">
      <!-- 筛选器 -->
      <div class="stats-filter">
        <div class="filter-group">
          <div class="filter-label">{{ t('tableMaintenance.statsPanel.dateRange') }}:</div>
          <input type="date" v-model="statsDateInput.start" class="stats-date-input">
          <span class="date-separator">-</span>
          <input type="date" v-model="statsDateInput.end" class="stats-date-input">
        </div>
        <div class="filter-group">
          <div class="filter-label">{{ t('tableMaintenance.statsPanel.filterProject') }}:</div>
          <select v-model="statsProjectInput" class="stats-select-input">
            <option value="">{{ t('common.all') }}</option>
            <option v-for="proj in projectOptions" :key="getProjectKey(proj)" :value="getProjectKey(proj)">{{ getProjectName(proj) }}</option>
          </select>
        </div>
        <div class="filter-group">
          <div class="filter-label">{{ t('tableMaintenance.statsPanel.filterDuration') }}:</div>
          <select v-model="statsDurationInput" class="stats-select-input">
            <option value="">{{ t('common.all') }}</option>
            <option value="twoMin">{{ t('tableMaintenance.options.duration.twoMin') }}</option>
            <option value="fiveMin">{{ t('tableMaintenance.options.duration.fiveMin') }}</option>
            <option value="tenMin">{{ t('tableMaintenance.options.duration.tenMin') }}</option>
            <option value="overTenMin">{{ t('tableMaintenance.options.duration.overTenMin') }}</option>
          </select>
        </div>
        <div class="filter-group">
          <div class="filter-label">{{ t('tableMaintenance.statsPanel.filterOperator') }}:</div>
          <select v-model="statsOperatorInput" class="stats-select-input">
            <option value="">{{ t('common.all') }}</option>
            <option v-for="op in operatorOptions" :key="op" :value="op">{{ op }}</option>
          </select>
        </div>
        <div class="filter-group">
          <div class="filter-label">{{ t('tableMaintenance.filters.operationLabel') }}:</div>
          <select v-model="statsOperationInput" class="stats-select-input">
            <option value="">{{ t('tableMaintenance.filters.allOperations') }}</option>
            <option v-for="op in operationOptions" :key="op.key" :value="op.key">{{ op.label }}</option>
          </select>
        </div>
        <div class="filter-group">
          <div class="filter-label">{{ t('tableMaintenance.columns.maintenanceType') }}:</div>
          <select v-model="statsMaintTypeInput" class="stats-select-input">
            <option value="">{{ t('common.all') }}</option>
            <option value="routine">{{ t('tableMaintenance.options.maintenanceType.routine') }}</option>
            <option value="temporary">{{ t('tableMaintenance.options.maintenanceType.temporary') }}</option>
            <option value="emergency">{{ t('tableMaintenance.options.maintenanceType.emergency') }}</option>
          </select>
        </div>
        <div class="filter-group">
          <div class="filter-label">{{ t('tableMaintenance.statsPanel.filterRoundId') }}:</div>
          <input type="text" v-model="statsRoundIdInput" class="stats-text-input" :placeholder="t('tableMaintenance.filters.roundIdPlaceholder')">
        </div>
        <div class="filter-group">
          <div class="filter-label">{{ t('tableMaintenance.statsPanel.filterTableStatus') }}:</div>
          <select v-model="statsTableStatusInput" class="stats-select-input">
            <option value="">{{ t('tableMaintenance.statsPanel.allTableStatus') }}</option>
            <option value="enabled">{{ t('tableHierarchy.status.enabled') }}</option>
            <option value="disabled">{{ t('tableHierarchy.status.disabled') }}</option>
            <option value="pending">{{ t('tableHierarchy.status.pending') }}</option>
            <option value="unconfigured">{{ t('tableHierarchy.status.unconfigured') }}</option>
          </select>
        </div>
        <div class="filter-group exclude-creator-group">
          <div class="filter-label">{{ t('tableMaintenance.statsPanel.excludeFilterLabel') }}:</div>
          <button type="button" class="stats-select-input exclude-trigger"
                  :class="{ 'has-exclude': statsExcludeApiKeyInput || statsExcludeCreatorsInput.length }"
                  @click="showExcludeCreatorsDropdown = !showExcludeCreatorsDropdown">
            <span>{{ excludeBtnLabel }}</span>
            <svg class="exclude-caret" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><polyline points="6 9 12 15 18 9"/></svg>
          </button>
          <div v-if="showExcludeCreatorsDropdown" class="exclude-backdrop" @click="showExcludeCreatorsDropdown = false"></div>
          <div v-if="showExcludeCreatorsDropdown" class="exclude-dropdown" @click.stop>
            <label class="exclude-row exclude-apikey-row">
              <input type="checkbox" v-model="statsExcludeApiKeyInput">
              <span>{{ t('tableMaintenance.statsPanel.excludeApiKey') }}</span>
            </label>
            <div class="exclude-divider"></div>
            <div class="exclude-list">
              <label v-for="c in creatorOptions" :key="c" class="exclude-row">
                <input type="checkbox" :value="c" v-model="statsExcludeCreatorsInput">
                <span :title="c">{{ c }}</span>
              </label>
              <div v-if="!creatorOptions.length" class="exclude-empty">{{ t('tableMaintenance.statsPanel.excludeNoCreators') }}</div>
            </div>
            <div class="exclude-actions-bar">
              <button type="button" class="exclude-link" @click="statsExcludeCreatorsInput = [...creatorOptions]">{{ t('common.selectAll') }}</button>
              <button type="button" class="exclude-link" @click="statsExcludeCreatorsInput = []">{{ t('tableMaintenance.statsPanel.excludeClear') }}</button>
              <span class="exclude-hint">{{ t('tableMaintenance.statsPanel.excludeApplyHint') }}</span>
            </div>
          </div>
        </div>
        <div class="filter-actions">
          <button class="btn btn-primary" @click="applyStatsFilter">
            <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" style="width:16px;height:16px;margin-right:4px;"><circle cx="11" cy="11" r="8"/><path d="m21 21-4.35-4.35"/></svg>
            {{ t('tableMaintenance.statsPanel.searchBtn') }}
          </button>
          <button class="btn btn-text" @click="resetStatsFilter">{{ t('tableMaintenance.filters.reset') }}</button>
        </div>
        <button class="btn btn-secondary" @click="exportStatsToExcel">
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" style="width:16px;height:16px;margin-right:4px;"><path d="M21 15v4a2 2 0 01-2 2H5a2 2 0 01-2-2v-4M7 10l5 5 5-5M12 15V3"/></svg>
          {{ t('tableMaintenance.statsPanel.export') }}
        </button>
        <div class="filter-result">
          {{ t('tableMaintenance.table.pagination.total') }}: <strong>{{ statsAnalysis.totalRecords || 0 }}</strong> {{ t('tableMaintenance.table.pagination.records') }}{{ t('tableMaintenance.statsPanel.involves') }} <strong>{{ statsAnalysis.total || 0 }}</strong> {{ t('tableMaintenance.statsPanel.tableTimes') }}
          <span class="filter-pending-card" v-if="statsAnalysis.pendingMaintCount">
            · {{ t('tableMaintenance.statsPanel.pendingMaintCard') }}:
            <strong>{{ statsAnalysis.pendingMaintCount }}</strong> {{ t('tableMaintenance.statsPanel.tableTimes') }}
            ({{ statsAnalysis.pendingTableCount }})
          </span>
        </div>
      </div>

      <div class="stats-overview">
        <!-- 按维护类型统计时长分布 -->
        <div class="stats-row single">
          <div class="stats-section">
            <h4>{{ t('tableMaintenance.statsPanel.byMaintenanceType') }}</h4>
            <div class="type-duration-grid" v-if="statsAnalysis.byMaintType?.length">
              <div class="type-duration-card" v-for="item in statsAnalysis.byMaintType" :key="item.name">
                <div class="tdc-header">
                  <span class="tdc-name">{{ item.name }}</span>
                  <span class="tdc-total">{{ item.total }}{{ t('tableMaintenance.statsPanel.tableTimes') }}</span>
                </div>
                <div class="tdc-durations">
                  <div class="tdc-dur" :class="getDurationClass(t('tableMaintenance.options.duration.twoMin'))">
                    <span class="tdc-dur-label">{{ t('tableMaintenance.options.duration.twoMin') }}</span>
                    <span class="tdc-dur-count">{{ item.durations[t('tableMaintenance.options.duration.twoMin')] || 0 }}</span>
                  </div>
                  <div class="tdc-dur" :class="getDurationClass(t('tableMaintenance.options.duration.fiveMin'))">
                    <span class="tdc-dur-label">{{ t('tableMaintenance.options.duration.fiveMin') }}</span>
                    <span class="tdc-dur-count">{{ item.durations[t('tableMaintenance.options.duration.fiveMin')] || 0 }}</span>
                  </div>
                  <div class="tdc-dur" :class="getDurationClass(t('tableMaintenance.options.duration.tenMin'))">
                    <span class="tdc-dur-label">{{ t('tableMaintenance.options.duration.tenMin') }}</span>
                    <span class="tdc-dur-count">{{ item.durations[t('tableMaintenance.options.duration.tenMin')] || 0 }}</span>
                  </div>
                  <div class="tdc-dur" :class="getDurationClass(t('tableMaintenance.options.duration.overTenMin'))">
                    <span class="tdc-dur-label">{{ t('tableMaintenance.options.duration.overTenMin') }}</span>
                    <span class="tdc-dur-count">{{ item.durations[t('tableMaintenance.options.duration.overTenMin')] || 0 }}</span>
                  </div>
                </div>
              </div>
            </div>
            <div v-else class="empty-stats">{{ t('tableMaintenance.table.empty') }}</div>
          </div>
        </div>

        <!-- 按操作类型统计 -->
        <div class="stats-row single">
          <div class="stats-section">
            <h4>{{ t('tableMaintenance.statsPanel.byOperation') }}</h4>
            <div class="type-duration-grid" v-if="statsAnalysis.byOpType?.length">
              <div class="type-duration-card op-card" v-for="item in statsAnalysis.byOpType" :key="item.key" :class="'op-' + item.key">
                <div class="tdc-header">
                  <span class="tdc-name">{{ item.name }}</span>
                  <span class="tdc-total">{{ item.total }}{{ t('tableMaintenance.statsPanel.tableTimes') }}</span>
                </div>
                <!-- 只有维护操作才显示时长分布 -->
                <div class="tdc-durations" v-if="item.hasDuration">
                  <div class="tdc-dur" :class="getDurationClass(t('tableMaintenance.options.duration.twoMin'))">
                    <span class="tdc-dur-label">{{ t('tableMaintenance.options.duration.twoMin') }}</span>
                    <span class="tdc-dur-count">{{ item.durations[t('tableMaintenance.options.duration.twoMin')] || 0 }}</span>
                  </div>
                  <div class="tdc-dur" :class="getDurationClass(t('tableMaintenance.options.duration.fiveMin'))">
                    <span class="tdc-dur-label">{{ t('tableMaintenance.options.duration.fiveMin') }}</span>
                    <span class="tdc-dur-count">{{ item.durations[t('tableMaintenance.options.duration.fiveMin')] || 0 }}</span>
                  </div>
                  <div class="tdc-dur" :class="getDurationClass(t('tableMaintenance.options.duration.tenMin'))">
                    <span class="tdc-dur-label">{{ t('tableMaintenance.options.duration.tenMin') }}</span>
                    <span class="tdc-dur-count">{{ item.durations[t('tableMaintenance.options.duration.tenMin')] || 0 }}</span>
                  </div>
                  <div class="tdc-dur" :class="getDurationClass(t('tableMaintenance.options.duration.overTenMin'))">
                    <span class="tdc-dur-label">{{ t('tableMaintenance.options.duration.overTenMin') }}</span>
                    <span class="tdc-dur-count">{{ item.durations[t('tableMaintenance.options.duration.overTenMin')] || 0 }}</span>
                  </div>
                </div>
              </div>
            </div>
            <div v-else class="empty-stats">{{ t('tableMaintenance.table.empty') }}</div>
          </div>
        </div>

        <!-- 维护时长明细列表 -->
        <div class="stats-row single">
          <div class="stats-section">
            <h4>{{ t('tableMaintenance.statsPanel.maintDurationDetail') }}</h4>
            <div class="duration-detail-table" v-if="statsAnalysis.maintDurationList?.length">
              <table>
                <thead>
                  <tr>
                    <th>{{ t('tableMaintenance.statsPanel.detailDate') }}</th>
                    <th>{{ t('tableMaintenance.statsPanel.detailTable') }}</th>
                    <th>{{ t('tableMaintenance.statsPanel.detailMaintType') }}</th>
                    <th>{{ t('tableMaintenance.statsPanel.detailStartDuration') }}</th>
                    <th>{{ t('tableMaintenance.statsPanel.detailCloseDuration') }}</th>
                    <th>{{ t('tableMaintenance.statsPanel.detailTotalDuration') }}</th>
                  </tr>
                </thead>
                <tbody>
                  <tr v-for="(item, idx) in statsAnalysis.maintDurationList" :key="idx">
                    <td>{{ item.date }}</td>
                    <td>{{ item.tableNo }}</td>
                    <td>{{ item.maintType }}</td>
                    <td><span class="dur-badge" :class="getDurationClass(item.startDuration)">{{ item.startDuration }}</span></td>
                    <td><span class="dur-badge" :class="getDurationClass(item.closeDuration)">{{ item.closeDuration }}</span></td>
                    <td><span class="dur-badge total" :class="getDurationClass(item.totalDuration)">{{ item.totalDuration }}</span></td>
                  </tr>
                </tbody>
              </table>
            </div>
            <div v-else class="empty-stats">{{ t('tableMaintenance.table.empty') }}</div>
          </div>
        </div>

        <!-- 取消操作明细 -->
        <div class="stats-row single" v-if="statsAnalysis.cancelDetailList?.length">
          <div class="stats-section">
            <h4>{{ t('tableMaintenance.statsPanel.cancelDetail') }} ({{ statsAnalysis.cancelDetailList.length }})</h4>
            <div class="duration-detail-table">
              <table>
                <thead>
                  <tr>
                    <th>{{ t('tableMaintenance.statsPanel.detailDate') }}</th>
                    <th>{{ t('tableMaintenance.statsPanel.detailProject') }}</th>
                    <th>{{ t('tableMaintenance.statsPanel.detailRoundId') }}</th>
                    <th>{{ t('tableMaintenance.statsPanel.detailDuration') }}</th>
                    <th>{{ t('tableMaintenance.statsPanel.detailOperator') }}</th>
                  </tr>
                </thead>
                <tbody>
                  <tr v-for="(item, idx) in statsAnalysis.cancelDetailList" :key="idx">
                    <td>{{ item.date }}</td>
                    <td class="projects-cell">{{ item.projects }}</td>
                    <td class="round-id-cell">{{ item.roundIds }}</td>
                    <td><span class="dur-badge" :class="getDurationClass(item.startDuration)">{{ item.startDuration }}</span></td>
                    <td>{{ item.operator }}</td>
                  </tr>
                </tbody>
              </table>
            </div>
          </div>
        </div>

        <!-- 重算操作明细 -->
        <div class="stats-row single" v-if="statsAnalysis.recalcDetailList?.length">
          <div class="stats-section">
            <h4>{{ t('tableMaintenance.statsPanel.recalcDetail') }} ({{ statsAnalysis.recalcDetailList.length }})</h4>
            <div class="duration-detail-table">
              <table>
                <thead>
                  <tr>
                    <th>{{ t('tableMaintenance.statsPanel.detailDate') }}</th>
                    <th>{{ t('tableMaintenance.statsPanel.detailProject') }}</th>
                    <th>{{ t('tableMaintenance.statsPanel.detailRoundId') }}</th>
                    <th>{{ t('tableMaintenance.statsPanel.detailDuration') }}</th>
                    <th>{{ t('tableMaintenance.statsPanel.detailOperator') }}</th>
                  </tr>
                </thead>
                <tbody>
                  <tr v-for="(item, idx) in statsAnalysis.recalcDetailList" :key="idx">
                    <td>{{ item.date }}</td>
                    <td class="projects-cell">{{ item.projects }}</td>
                    <td class="round-id-cell">{{ item.roundIds }}</td>
                    <td><span class="dur-badge" :class="getDurationClass(item.startDuration)">{{ item.startDuration }}</span></td>
                    <td>{{ item.operator }}</td>
                  </tr>
                </tbody>
              </table>
            </div>
          </div>
        </div>

        <!-- 重派彩操作明细 -->
        <div class="stats-row single" v-if="statsAnalysis.repayoutDetailList?.length">
          <div class="stats-section">
            <h4>{{ t('tableMaintenance.statsPanel.repayoutDetail') }} ({{ statsAnalysis.repayoutDetailList.length }})</h4>
            <div class="duration-detail-table">
              <table>
                <thead>
                  <tr>
                    <th>{{ t('tableMaintenance.statsPanel.detailDate') }}</th>
                    <th>{{ t('tableMaintenance.statsPanel.detailProject') }}</th>
                    <th>{{ t('tableMaintenance.statsPanel.detailRoundId') }}</th>
                    <th>{{ t('tableMaintenance.statsPanel.detailDuration') }}</th>
                    <th>{{ t('tableMaintenance.statsPanel.detailOperator') }}</th>
                  </tr>
                </thead>
                <tbody>
                  <tr v-for="(item, idx) in statsAnalysis.repayoutDetailList" :key="idx">
                    <td>{{ item.date }}</td>
                    <td class="projects-cell">{{ item.projects }}</td>
                    <td class="round-id-cell">{{ item.roundIds }}</td>
                    <td><span class="dur-badge" :class="getDurationClass(item.startDuration)">{{ item.startDuration }}</span></td>
                    <td>{{ item.operator }}</td>
                  </tr>
                </tbody>
              </table>
            </div>
          </div>
        </div>

        <!-- 包桌T人明细 -->
        <div class="stats-row single" v-if="statsAnalysis.vipTableDetailList?.length">
          <div class="stats-section">
            <h4>{{ t('tableMaintenance.statsPanel.vipTableDetail') }} ({{ statsAnalysis.vipTableDetailList.length }})</h4>
            <div class="duration-detail-table">
              <table>
                <thead>
                  <tr>
                    <th>{{ t('tableMaintenance.statsPanel.detailDate') }}</th>
                    <th>{{ t('tableMaintenance.statsPanel.detailProject') }}</th>
                    <th>{{ t('tableMaintenance.statsPanel.detailRoundId') }}</th>
                    <th>{{ t('tableMaintenance.statsPanel.detailDuration') }}</th>
                    <th>{{ t('tableMaintenance.statsPanel.detailOperator') }}</th>
                  </tr>
                </thead>
                <tbody>
                  <tr v-for="(item, idx) in statsAnalysis.vipTableDetailList" :key="idx">
                    <td>{{ item.date }}</td>
                    <td class="projects-cell">{{ item.projects }}</td>
                    <td class="round-id-cell">{{ item.roundIds }}</td>
                    <td><span class="dur-badge" :class="getDurationClass(item.startDuration)">{{ item.startDuration }}</span></td>
                    <td>{{ item.operator }}</td>
                  </tr>
                </tbody>
              </table>
            </div>
          </div>
        </div>

        <!-- 按项目 × 操作类型矩阵 -->
        <div class="stats-row single" v-if="statsAnalysis.byProjectOperation?.length">
          <div class="stats-section">
            <h4>{{ t('tableMaintenance.statsPanel.byProjectOperation') }}</h4>
            <div class="project-op-grid">
              <div class="project-op-card" v-for="item in statsAnalysis.byProjectOperation" :key="item.project">
                <div class="pop-header">
                  <span class="pop-project">{{ item.project }}</span>
                  <span class="pop-total">{{ item.total }}{{ t('tableMaintenance.statsPanel.tableTimes') }}</span>
                </div>
                <div class="pop-ops">
                  <div class="pop-op" v-for="k in statsAnalysis.opKeys" :key="k" :class="'op-' + k">
                    <span class="pop-op-name">{{ t('tableMaintenance.options.operation.' + k) }}</span>
                    <span class="pop-op-count">{{ item.ops[k] || 0 }}</span>
                  </div>
                </div>
              </div>
            </div>
          </div>
        </div>

        <!-- 漏操作明细 -->
        <div class="stats-row single" v-if="statsAnalysis.missedDetailList?.length">
          <div class="stats-section">
            <h4>{{ t('tableMaintenance.statsPanel.missedDetail') }} ({{ statsAnalysis.missedDetailList.length }})</h4>
            <div class="duration-detail-table">
              <table>
                <thead>
                  <tr>
                    <th>{{ t('tableMaintenance.statsPanel.detailDate') }}</th>
                    <th>{{ t('tableMaintenance.statsPanel.detailProject') }}</th>
                    <th>{{ t('tableMaintenance.statsPanel.detailRoundId') }}</th>
                    <th>{{ t('tableMaintenance.statsPanel.detailDuration') }}</th>
                    <th>{{ t('tableMaintenance.statsPanel.detailOperator') }}</th>
                    <th>{{ t('tableMaintenance.columns.remark') }}</th>
                  </tr>
                </thead>
                <tbody>
                  <tr v-for="(item, idx) in statsAnalysis.missedDetailList" :key="idx">
                    <td>{{ item.date }}</td>
                    <td class="projects-cell">{{ item.projects }}</td>
                    <td class="round-id-cell">{{ item.roundIds }}</td>
                    <td><span class="dur-badge" :class="getDurationClass(item.startDuration)">{{ item.startDuration }}</span></td>
                    <td>{{ item.operator }}</td>
                    <td class="remark-cell">{{ item.remark }}</td>
                  </tr>
                </tbody>
              </table>
            </div>
          </div>
        </div>

        <!-- 漏截图明细 -->
        <div class="stats-row single" v-if="statsAnalysis.missedScreenshotDetailList?.length">
          <div class="stats-section">
            <h4>{{ t('tableMaintenance.statsPanel.missedScreenshotDetail') }} ({{ statsAnalysis.missedScreenshotDetailList.length }})</h4>
            <div class="duration-detail-table">
              <table>
                <thead>
                  <tr>
                    <th>{{ t('tableMaintenance.statsPanel.detailDate') }}</th>
                    <th>{{ t('tableMaintenance.statsPanel.detailProject') }}</th>
                    <th>{{ t('tableMaintenance.statsPanel.detailRoundId') }}</th>
                    <th>{{ t('tableMaintenance.statsPanel.detailOperator') }}</th>
                    <th>{{ t('tableMaintenance.columns.remark') }}</th>
                  </tr>
                </thead>
                <tbody>
                  <tr v-for="(item, idx) in statsAnalysis.missedScreenshotDetailList" :key="idx">
                    <td>{{ item.date }}</td>
                    <td class="projects-cell">{{ item.projects }}</td>
                    <td class="round-id-cell">{{ item.roundIds }}</td>
                    <td>{{ item.operator }}</td>
                    <td class="remark-cell">{{ item.remark }}</td>
                  </tr>
                </tbody>
              </table>
            </div>
          </div>
        </div>

        <div class="stats-row">
          <div class="stats-section">
            <h4>{{ t('tableMaintenance.statsPanel.byOperator') }}</h4>
            <div class="stats-list scrollable" v-if="statsAnalysis.byOperator?.length">
              <div class="stats-item" v-for="item in statsAnalysis.byOperator" :key="item.name">
                <span class="item-name">{{ item.name }}</span>
                <div class="item-bar"><div class="bar-fill operator" :style="{ width: getBarWidth(item.count, statsAnalysis.maxOperator) }"></div></div>
                <span class="item-count">{{ item.count }}</span>
              </div>
            </div>
            <div v-else class="empty-stats">{{ t('tableMaintenance.table.empty') }}</div>
          </div>
          <div class="stats-section">
            <h4>{{ t('tableMaintenance.statsPanel.byInspector') }}</h4>
            <div class="stats-list scrollable" v-if="statsAnalysis.byInspector?.length">
              <div class="stats-item" v-for="item in statsAnalysis.byInspector" :key="item.name">
                <span class="item-name">{{ item.name }}</span>
                <div class="item-bar"><div class="bar-fill inspector" :style="{ width: getBarWidth(item.count, statsAnalysis.maxInspector) }"></div></div>
                <span class="item-count">{{ item.count }}</span>
              </div>
            </div>
            <div v-else class="empty-stats">{{ t('tableMaintenance.table.empty') }}</div>
          </div>
        </div>

        <div class="stats-row">
          <div class="stats-section">
            <h4>{{ t('tableMaintenance.statsPanel.byQcStatus') }}</h4>
            <div class="qc-stats" v-if="statsAnalysis.qcNormal !== undefined">
              <div class="qc-item normal"><span class="qc-label">{{ t('tableMaintenance.filters.normal') }}</span><span class="qc-value">{{ statsAnalysis.qcNormal }}</span></div>
              <div class="qc-item abnormal"><span class="qc-label">{{ t('tableMaintenance.filters.abnormal') }}</span><span class="qc-value">{{ statsAnalysis.qcAbnormal }}</span></div>
              <div class="qc-item pending"><span class="qc-label">{{ t('tableMaintenance.statsPanel.qcPending') }}</span><span class="qc-value">{{ statsAnalysis.qcPending }}</span></div>
            </div>
            <div v-else class="empty-stats">{{ t('tableMaintenance.table.empty') }}</div>
          </div>
          <div class="stats-section">
            <h4>{{ t('tableMaintenance.statsPanel.byAffectedProject') }}</h4>
            <div class="stats-list" v-if="statsAnalysis.byAffectProject?.length">
              <div class="stats-item" v-for="item in statsAnalysis.byAffectProject" :key="item.name">
                <span class="item-name">{{ item.name }}</span>
                <div class="item-bar"><div class="bar-fill affect-project" :style="{ width: getBarWidth(item.count, statsAnalysis.maxAffectProject) }"></div></div>
                <span class="item-count">{{ item.count }}</span>
              </div>
              <div class="stats-item" v-if="statsAnalysis.noAffectCount > 0">
                <span class="item-name text-muted">{{ t('common.none') }}</span>
                <div class="item-bar"><div class="bar-fill no-affect" :style="{ width: getBarWidth(statsAnalysis.noAffectCount, statsAnalysis.maxAffectProject) }"></div></div>
                <span class="item-count text-muted">{{ statsAnalysis.noAffectCount }}</span>
              </div>
            </div>
            <div v-else class="empty-stats">{{ t('tableMaintenance.table.empty') }}</div>
          </div>
        </div>

        <div class="stats-row">
          <div class="stats-section">
            <h4>{{ t('tableMaintenance.statsPanel.byGameType') }}</h4>
            <div class="stats-list" v-if="statsAnalysis.byGameType?.length">
              <div class="stats-item" v-for="item in statsAnalysis.byGameType" :key="item.name">
                <span class="item-name">{{ item.name }}</span>
                <div class="item-bar"><div class="bar-fill" :style="{ width: getBarWidth(item.count, statsAnalysis.maxGameType) }"></div></div>
                <span class="item-count">{{ item.count }}</span>
              </div>
            </div>
            <div v-else class="empty-stats">{{ t('tableMaintenance.table.empty') }}</div>
          </div>
          <div class="stats-section">
            <h4>{{ t('tableMaintenance.statsPanel.bySite') }}</h4>
            <div class="stats-list" v-if="statsAnalysis.bySite?.length">
              <div class="stats-item" v-for="item in statsAnalysis.bySite" :key="item.name">
                <span class="item-name">{{ item.name }}</span>
                <div class="item-bar"><div class="bar-fill site" :style="{ width: getBarWidth(item.count, statsAnalysis.maxSite) }"></div></div>
                <span class="item-count">{{ item.count }}</span>
              </div>
            </div>
            <div v-else class="empty-stats">{{ t('tableMaintenance.table.empty') }}</div>
          </div>
        </div>

        <div class="stats-row single">
          <div class="stats-section">
            <h4>{{ t('tableMaintenance.statsPanel.byTableSummary') }}</h4>
            <div class="table-stats-table" v-if="statsAnalysis.byTableSummary?.length">
              <table>
                <thead>
                  <tr>
                    <th>{{ t('tableMaintenance.statsPanel.tableColumns.tableNo') }}</th>
                    <th>{{ t('tableMaintenance.statsPanel.tableColumns.maintCount') }}</th>
                    <th>{{ t('tableMaintenance.statsPanel.tableColumns.projects') }}</th>
                    <th>{{ t('tableMaintenance.statsPanel.tableColumns.site') }}</th>
                    <th>{{ t('tableMaintenance.statsPanel.tableColumns.gameType') }}</th>
                    <th>{{ t('tableMaintenance.statsPanel.tableColumns.status') }}</th>
                  </tr>
                </thead>
                <tbody>
                  <tr v-for="(item, idx) in statsAnalysis.byTableSummary" :key="idx">
                    <td><span class="table-no-tag">{{ item.tableNo }}</span></td>
                    <td><span class="count-badge">{{ item.count }}</span></td>
                    <td class="projects-cell">{{ item.projects }}</td>
                    <td>{{ item.sites }}</td>
                    <td>{{ item.gameTypes }}</td>
                    <td><span :class="['status-tag', 'status-tag-' + (item.status || 'enabled')]">{{ t('tableHierarchy.status.' + (item.status || 'enabled')) }}</span></td>
                  </tr>
                </tbody>
              </table>
            </div>
            <div v-else class="empty-stats">{{ t('tableMaintenance.table.empty') }}</div>
          </div>
        </div>

        <div class="stats-row single">
          <div class="stats-section">
            <h4>{{ t('tableMaintenance.statsPanel.byTable') }}</h4>
            <div class="table-stats-table" v-if="statsAnalysis.byTableNo?.length">
              <table>
                <thead>
                  <tr>
                    <th>{{ t('tableMaintenance.statsPanel.tableColumns.tableNo') }}</th>
                    <th>{{ t('tableMaintenance.statsPanel.tableColumns.projects') }}</th>
                    <th>{{ t('tableMaintenance.statsPanel.tableColumns.site') }}</th>
                    <th>{{ t('tableMaintenance.statsPanel.tableColumns.gameType') }}</th>
                    <th>{{ t('tableMaintenance.statsPanel.tableColumns.status') }}</th>
                    <th>{{ t('tableMaintenance.statsPanel.tableColumns.maintCount') }}</th>
                  </tr>
                </thead>
                <tbody>
                  <tr v-for="(item, idx) in statsAnalysis.byTableNo" :key="idx">
                    <td><span class="table-no-tag">{{ item.tableNo }}</span></td>
                    <td class="projects-cell">{{ item.project }}</td>
                    <td>{{ item.site }}</td>
                    <td>{{ item.gameType }}</td>
                    <td><span :class="['status-tag', 'status-tag-' + (item.status || 'enabled')]">{{ t('tableHierarchy.status.' + (item.status || 'enabled')) }}</span></td>
                    <td><span class="count-badge">{{ item.count }}</span></td>
                  </tr>
                </tbody>
              </table>
            </div>
            <div v-else class="empty-stats">{{ t('tableMaintenance.table.empty') }}</div>
          </div>
        </div>
      </div>
    </div>

    <!-- 新增/编辑弹窗 -->
    <Teleport to="body">
      <div class="modal-overlay" :class="{ active: showModal }">
        <div class="modal record-form-modal">
          <div class="modal-header">
            <div class="modal-title">
              <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><rect x="3" y="3" width="18" height="18" rx="2"/><path d="M9 9h6v6H9z"/></svg>
              {{ modalMode === 'add' ? t('tableMaintenance.form.addTitle') : t('tableMaintenance.form.editTitle') }}
            </div>
            <button class="modal-close" @click="showModal = false"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><line x1="18" y1="6" x2="6" y2="18"/><line x1="6" y1="6" x2="18" y2="18"/></svg></button>
          </div>
          <div class="modal-body">
            <!-- ▎操作信息 -->
            <div class="form-section">
              <div class="section-title">{{ t('tableMaintenance.form.sectionOperation') }}</div>
              <div class="dynamic-form-grid">
                <!-- 日期 -->
                <div class="form-group">
                  <label>{{ getCol('date')?.title }}</label>
                  <input type="date" v-model="formData.date">
                </div>
                <!-- 受影响项目 -->
                <div class="form-group">
                  <label>{{ getCol('affected_projects')?.title }} <span class="required">*</span></label>
                  <div class="multi-select-dropdown-wrapper project-select-wrapper">
                    <div class="multi-select-trigger" @click.stop="projectSelectOpen = !projectSelectOpen" v-if="projectOptions.length">
                      <div class="multi-select-selected">
                        <span v-if="!formData.affected_projects?.length" class="placeholder">{{ t('tableMaintenance.placeholders.select', { field: getCol('affected_projects')?.title }) }}</span>
                        <span v-for="(item, idx) in (formData.affected_projects || []).slice(0, 3)" :key="idx" class="selected-tag" :style="{ backgroundColor: getProjectColorByName(item) + '18', color: getProjectColorByName(item), borderColor: getProjectColorByName(item) + '40' }">{{ item }}</span>
                        <span v-if="(formData.affected_projects?.length || 0) > 3" class="more-tag">+{{ formData.affected_projects.length - 3 }}</span>
                      </div>
                      <svg class="dropdown-arrow" :class="{ open: projectSelectOpen }" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><polyline points="6 9 12 15 18 9"/></svg>
                    </div>
                    <div class="multi-select-panel" v-if="projectSelectOpen && projectOptions.length">
                      <div class="select-all-row" @click.stop="selectAllProjects()">
                        <span class="checkbox-icon select-all-checkbox" :class="{ checked: isAllProjectsSelected() }"><svg v-if="isAllProjectsSelected()" viewBox="0 0 24 24" fill="none" stroke="white" stroke-width="3"><polyline points="20 6 9 17 4 12"/></svg></span>
                        <span class="select-all-label">{{ t('tableMaintenance.selectAll') }}</span>
                      </div>
                      <label v-for="(opt, idx) in projectOptions" :key="getProjectKey(opt)" class="multi-select-option" :class="{ checked: isProjectSelected(getProjectKey(opt)) }" @click.stop="toggleProjectOption(getProjectKey(opt))">
                        <span class="checkbox-icon" :style="isProjectSelected(getProjectKey(opt)) ? { backgroundColor: getProjectColor(idx), borderColor: getProjectColor(idx) } : {}"><svg v-if="isProjectSelected(getProjectKey(opt))" viewBox="0 0 24 24" fill="none" stroke="white" stroke-width="3"><polyline points="20 6 9 17 4 12"/></svg></span>
                        <span class="option-label" :style="{ color: getProjectColor(idx) }">{{ getProjectName(opt) }}</span>
                      </label>
                    </div>
                    <div v-if="!projectOptions.length" class="empty-options">
                      <span>暂无配置的项目</span>
                      <button v-if="isSuperAdmin" class="btn-link" @click="$router.push('/table-hierarchy-config')">去配置</button>
                    </div>
                  </div>
                </div>
                <!-- 操作 -->
                <div class="form-group">
                  <label>{{ getCol('operation')?.title }} <span class="required">*</span></label>
                  <select v-model="formData.operation">
                    <option v-for="opt in getColumnOptions(getCol('operation'))" :key="opt" :value="opt">{{ opt }}</option>
                  </select>
                </div>
                <!-- 维护类型（维护时必填） -->
                <div class="form-group" v-if="isOp(formData.operation, 'maintenance')">
                  <label>{{ getCol('maintenance_type')?.title }} <span class="required">*</span></label>
                  <select v-model="formData.maintenance_type" class="form-select">
                    <option value="">{{ t('tableMaintenance.placeholders.select', { field: getCol('maintenance_type')?.title }) }}</option>
                    <option v-for="opt in maintTypeOptions.filter(o => o !== t('tableMaintenance.options.maintenanceType.none'))" :key="opt" :value="opt">{{ opt }}</option>
                  </select>
                </div>
                <!-- 桌台 -->
                <div class="form-group">
                  <label>{{ getCol('affected_tables')?.title }} <span class="required">*</span></label>
                  <div class="multi-select-dropdown-wrapper table-select-wrapper">
                    <div class="multi-select-trigger" @click.stop="tableSelectOpen = !tableSelectOpen; if(tableSelectOpen) tableSearchQuery = ''" v-if="getFilteredTables().length">
                      <div class="multi-select-selected">
                        <span v-if="!formData.affected_tables?.length" class="placeholder">{{ t('tableMaintenance.placeholders.select', { field: getCol('affected_tables')?.title }) }}</span>
                        <span v-for="(item, idx) in (formData.affected_tables || []).slice(0, 3)" :key="idx" class="selected-tag" :style="{ backgroundColor: getProjectColorByName(item) + '18', color: getProjectColorByName(item), borderColor: getProjectColorByName(item) + '40' }">{{ item }}</span>
                        <span v-if="(formData.affected_tables?.length || 0) > 3" class="more-tag">+{{ formData.affected_tables.length - 3 }}</span>
                      </div>
                      <svg class="dropdown-arrow" :class="{ open: tableSelectOpen }" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><polyline points="6 9 12 15 18 9"/></svg>
                    </div>
                    <div class="multi-select-panel with-search" v-if="tableSelectOpen && getFilteredTables().length">
                      <div class="select-all-row" @click.stop="selectAllTables()">
                        <span class="checkbox-icon select-all-checkbox" :class="{ checked: isAllTablesSelected() }"><svg v-if="isAllTablesSelected()" viewBox="0 0 24 24" fill="none" stroke="white" stroke-width="3"><polyline points="20 6 9 17 4 12"/></svg></span>
                        <span class="select-all-label">{{ isAllTablesSelected() ? '取消全选' : '全选' }} ({{ getFilteredTables().length }})</span>
                      </div>
                      <div class="panel-search-box" @click.stop>
                        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><circle cx="11" cy="11" r="8"/><path d="M21 21l-4.35-4.35"/></svg>
                        <input type="text" v-model="tableSearchQuery" placeholder="搜索桌台..." class="panel-search-input" @keyup.enter.stop="getSearchFilteredTables().length === 1 && toggleTableOption(getSearchFilteredTables()[0])">
                        <button v-if="tableSearchQuery" class="search-clear" @click.stop="tableSearchQuery = ''"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><line x1="18" y1="6" x2="6" y2="18"/><line x1="6" y1="6" x2="18" y2="18"/></svg></button>
                      </div>
                      <div class="panel-options-list">
                        <label v-for="(opt, idx) in getSearchFilteredTables()" :key="opt" class="multi-select-option" :class="{ checked: isTableSelected(opt) }" @click.stop="toggleTableOption(opt)">
                          <span class="checkbox-icon" :style="isTableSelected(opt) ? { backgroundColor: getProjectColor(idx), borderColor: getProjectColor(idx) } : {}"><svg v-if="isTableSelected(opt)" viewBox="0 0 24 24" fill="none" stroke="white" stroke-width="3"><polyline points="20 6 9 17 4 12"/></svg></span>
                          <span class="option-label" :style="{ color: getProjectColor(idx) }">{{ opt }}</span>
                          <span v-if="getTableStatus(opt) === 'disabled'" class="status-tag-disabled-sm">{{ t('tableHierarchy.status.disabled') }}</span>
                          <span v-else-if="getTableStatus(opt) === 'pending'" class="status-tag-pending-sm">{{ t('tableHierarchy.status.pending') }}</span>
                        </label>
                        <div v-if="tableSearchQuery && !getSearchFilteredTables().length" class="no-match"><span>没有找到匹配的桌台</span></div>
                      </div>
                    </div>
                    <div v-if="!getFilteredTables().length" class="empty-options">
                      <span>暂无配置的桌台</span>
                      <button v-if="isSuperAdmin" class="btn-link" @click="$router.push('/table-hierarchy-config')">去配置</button>
                    </div>
                  </div>
                </div>
                <!-- 现场（自动，只读展示） -->
                <div class="form-group">
                  <label>{{ getCol('affected_sites')?.title }} <span class="label-hint">（{{ t('tableMaintenance.form.auto') }}）</span></label>
                  <div class="auto-tags">
                    <span v-for="(item, idx) in (formData.affected_sites || [])" :key="idx" class="selected-tag" :style="{ backgroundColor: getProjectColorByName(item) + '18', color: getProjectColorByName(item), borderColor: getProjectColorByName(item) + '40' }">{{ getSiteDisplayName(item) }}</span>
                    <span v-if="!formData.affected_sites?.length" class="cell-muted">{{ t('tableMaintenance.hints.selectTableFirst') }}</span>
                  </div>
                </div>
                <!-- 游戏类型（自动，只读展示） -->
                <div class="form-group">
                  <label>{{ getCol('game_types')?.title }} <span class="label-hint">（{{ t('tableMaintenance.form.auto') }}）</span></label>
                  <div class="auto-tags">
                    <span v-for="(item, idx) in (formData.game_types || [])" :key="idx" class="selected-tag game-type-tag-selected">{{ translateGameType(item) }}</span>
                    <span v-if="!formData.game_types?.length" class="cell-muted">{{ t('tableMaintenance.hints.selectTableFirst') }}</span>
                  </div>
                </div>
                <!-- 原因 -->
                <div class="form-group full-width">
                  <label>{{ getCol('reason')?.title }}</label>
                  <textarea v-model="formData.reason" rows="3" :placeholder="getCol('reason')?.title"></textarea>
                </div>
                <!-- 影响结算 -->
                <div class="form-group">
                  <label>{{ getCol('affect_settlement')?.title }}</label>
                  <select v-model="formData.affect_settlement">
                    <option v-for="opt in getColumnOptions(getCol('affect_settlement'))" :key="opt" :value="opt">{{ opt }}</option>
                  </select>
                </div>
                <!-- 影响局号 -->
                <div class="form-group">
                  <label>{{ getCol('affected_round_ids')?.title }}</label>
                  <input type="text" v-model="formData.affected_round_ids" :placeholder="getCol('affected_round_ids')?.title">
                </div>
                <!-- 实际操作人 -->
                <div class="form-group">
                  <label>{{ getCol('operator')?.title }} <span class="required">*</span></label>
                  <input type="text" v-model="formData.operator" :placeholder="getCol('operator')?.title">
                </div>
                <!-- 质检人 -->
                <div class="form-group">
                  <label>{{ getCol('inspector')?.title }}</label>
                  <input type="text" v-model="formData.inspector" :placeholder="getCol('inspector')?.title">
                </div>
                <!-- 质检状态 -->
                <div class="form-group">
                  <label>{{ getCol('qc_status')?.title }}</label>
                  <select v-model="formData.qc_status" class="form-select qc-status-select">
                    <option value="">{{ t('tableMaintenance.placeholders.select', { field: getCol('qc_status')?.title }) }}</option>
                    <option v-for="opt in getCol('qc_status')?.options" :key="opt" :value="opt">{{ opt }}</option>
                  </select>
                </div>
                <!-- 备注 -->
                <div class="form-group full-width">
                  <label>{{ getCol('remark')?.title }} <span v-if="isQc(formData.qc_status, 'abnormal')" class="required">*</span></label>
                  <textarea v-model="formData.remark" rows="3" :placeholder="isQc(formData.qc_status, 'abnormal') ? t('tableMaintenance.placeholders.remarkRequired') : getCol('remark')?.title" :class="{ 'required-field': isQc(formData.qc_status, 'abnormal') }"></textarea>
                </div>
              </div>
            </div>
            <!-- ▎时间与截图 -->
            <div class="form-section">
              <div class="section-title">{{ t('tableMaintenance.form.sectionTimeScreenshot') }}</div>
              <div class="dynamic-form-grid">
                <!-- 开始时间 -->
                <div class="form-group">
                  <label>{{ getCol('start_time')?.title }}</label>
                  <input type="datetime-local" v-model="formData.start_time">
                </div>
                <!-- 开始维护时长 -->
                <div class="form-group">
                  <label>{{ getCol('start_duration')?.title }} <span class="required">*</span></label>
                  <select v-model="formData.start_duration" class="form-select duration-dropdown">
                    <option value="">{{ t('tableMaintenance.placeholders.select', { field: getCol('start_duration')?.title }) }}</option>
                    <option v-for="opt in getCol('start_duration')?.options" :key="opt" :value="opt">{{ opt }}</option>
                  </select>
                </div>
                <!-- 通知开始截图 -->
                <div class="form-group full-width attachment-field">
                  <label>{{ getCol('notify_start_screenshot')?.title }}</label>
                  <div class="upload-zone" @dragover.prevent="e => e.currentTarget.classList.add('drag-over')" @dragleave.prevent="e => e.currentTarget.classList.remove('drag-over')" @drop.prevent="e => handleAttachmentDrop(e, 'notify_start_screenshot')" @paste="e => handleAttachmentPaste(e, 'notify_start_screenshot')" tabindex="0">
                    <input :ref="el => attachmentInputRefs['notify_start_screenshot'] = el" type="file" multiple hidden @change="e => handleAttachmentSelect(e, 'notify_start_screenshot')" accept="image/*">
                    <div class="upload-content">
                      <svg class="upload-icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M21 15v4a2 2 0 01-2 2H5a2 2 0 01-2-2v-4M17 8l-5-5-5 5M12 3v12"/></svg>
                      <p v-if="uploading" class="upload-text">{{ t('common.loading') }}</p>
                      <p v-else class="upload-text"><span class="primary-action" @click.stop="triggerAttachmentInput('notify_start_screenshot')">{{ t('tableMaintenance.upload.click') }}</span></p>
                      <p class="upload-hint">{{ t('tableMaintenance.upload.hint') }}</p>
                    </div>
                  </div>
                  <div class="mini-uploaded-files" v-if="formAttachments['notify_start_screenshot']?.length">
                    <div v-for="(att, idx) in formAttachments['notify_start_screenshot']" :key="idx" class="mini-file-item">
                      <img v-if="isImageFile(att.name)" :src="getPresignedUrl(att.path) || att.preview" class="mini-thumb clickable" @click="previewFormImage('notify_start_screenshot', idx)" title="点击预览">
                      <span class="mini-file-name">{{ att.name }}</span>
                      <button class="mini-remove-btn" @click="removeFormAttachment('notify_start_screenshot', idx)">&times;</button>
                    </div>
                  </div>
                </div>
                <!-- 结束时间 -->
                <div class="form-group" v-if="isOp(formData.operation, 'maintenance')">
                  <label>{{ getCol('end_time')?.title }}</label>
                  <input type="datetime-local" v-model="formData.end_time">
                </div>
                <!-- 关闭维护时长 -->
                <div class="form-group" v-if="isOp(formData.operation, 'maintenance')">
                  <label>{{ getCol('close_duration')?.title }}</label>
                  <select v-model="formData.close_duration" class="form-select duration-dropdown">
                    <option value="">{{ t('tableMaintenance.placeholders.select', { field: getCol('close_duration')?.title }) }}</option>
                    <option v-for="opt in getCol('close_duration')?.options" :key="opt" :value="opt">{{ opt }}</option>
                  </select>
                </div>
                <!-- 结束截图 -->
                <div class="form-group full-width attachment-field" v-if="isOp(formData.operation, 'maintenance')">
                  <label>{{ getCol('notify_end_screenshot')?.title }}</label>
                  <div class="upload-zone" @dragover.prevent="e => e.currentTarget.classList.add('drag-over')" @dragleave.prevent="e => e.currentTarget.classList.remove('drag-over')" @drop.prevent="e => handleAttachmentDrop(e, 'notify_end_screenshot')" @paste="e => handleAttachmentPaste(e, 'notify_end_screenshot')" tabindex="0">
                    <input :ref="el => attachmentInputRefs['notify_end_screenshot'] = el" type="file" multiple hidden @change="e => handleAttachmentSelect(e, 'notify_end_screenshot')" accept="image/*">
                    <div class="upload-content">
                      <svg class="upload-icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M21 15v4a2 2 0 01-2 2H5a2 2 0 01-2-2v-4M17 8l-5-5-5 5M12 3v12"/></svg>
                      <p v-if="uploading" class="upload-text">{{ t('common.loading') }}</p>
                      <p v-else class="upload-text"><span class="primary-action" @click.stop="triggerAttachmentInput('notify_end_screenshot')">{{ t('tableMaintenance.upload.click') }}</span></p>
                      <p class="upload-hint">{{ t('tableMaintenance.upload.hint') }}</p>
                    </div>
                  </div>
                  <div class="mini-uploaded-files" v-if="formAttachments['notify_end_screenshot']?.length">
                    <div v-for="(att, idx) in formAttachments['notify_end_screenshot']" :key="idx" class="mini-file-item">
                      <img v-if="isImageFile(att.name)" :src="getPresignedUrl(att.path) || att.preview" class="mini-thumb clickable" @click="previewFormImage('notify_end_screenshot', idx)" title="点击预览">
                      <span class="mini-file-name">{{ att.name }}</span>
                      <button class="mini-remove-btn" @click="removeFormAttachment('notify_end_screenshot', idx)">&times;</button>
                    </div>
                  </div>
                </div>
              </div>
            </div>
          </div>
          <div class="modal-footer">
            <button class="btn btn-secondary" @click="showModal = false">取消</button>
            <button class="btn btn-primary" @click="saveRecord" :disabled="uploading">
              <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M19 21H5a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h11l5 5v11a2 2 0 0 1-2 2z"/></svg>
              保存
            </button>
          </div>
        </div>
      </div>
    </Teleport>

    <!-- 详情弹窗 -->
    <Teleport to="body">
      <div class="modal-overlay" :class="{ active: showDetailModal && detailRecord }">
        <div class="modal detail-modal" v-if="showDetailModal && detailRecord">
          <div class="modal-header">
            <div class="modal-title">
              <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M1 12s4-8 11-8 11 8 11 8-4 8-11 8-11-8-11-8z"/><circle cx="12" cy="12" r="3"/></svg>
              记录详情
            </div>
            <button class="modal-close" @click="showDetailModal = false"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><line x1="18" y1="6" x2="6" y2="18"/><line x1="6" y1="6" x2="18" y2="18"/></svg></button>
          </div>
          <div class="modal-body">
            <div class="detail-grid">
              <template v-for="col in detailColumns" :key="col.key">
                <div class="detail-item" :class="{ full: col.type === 'textarea' || col.type === 'attachments' }">
                  <label>{{ col.title }}</label>
                  <span v-if="col.type === 'date'">{{ formatDate(detailRecord[col.key]) }}</span>
                  <span v-else-if="col.type === 'datetime'">{{ formatDateTime(detailRecord[col.key]) }}</span>
                  <span v-else-if="col.key === 'table_no'" class="tag-table">{{ detailRecord[col.key] || '-' }}</span>
                  <span v-else-if="col.type === 'tag-type'" class="tag-issue" :class="'tag-issue-' + getIssueTypeClass(detailRecord[col.key])">{{ detailRecord[col.key] || '-' }}</span>
                  <span v-else-if="col.type === 'status'" class="status-tag" :class="'status-' + getStatusClass(detailRecord[col.key])">{{ detailRecord[col.key] || '-' }}</span>
                  <span v-else-if="col.type === 'duration-select'" class="duration-tag" :class="getDurationClass(detailRecord[col.key])">{{ displayDur(detailRecord[col.key]) || '-' }}</span>
                  <span v-else-if="col.type === 'maint-type-select'" class="cell-select">{{ displayMaintType(detailRecord[col.key]) || '-' }}</span>
                  <span v-else-if="col.type === 'select' && col.key === 'operation'" class="cell-select">{{ displayOp(detailRecord[col.key]) || '-' }}</span>
                  <span v-else-if="col.type === 'yes-no'" class="yes-no-tag" :class="detailRecord[col.key] === '是' ? 'tag-yes' : 'tag-no'">{{ detailRecord[col.key] || '-' }}</span>
                  <div v-else-if="col.type === 'multi-select-projects'" class="multi-select-tags">
                    <span v-for="(item, idx) in parseMultiSelect(detailRecord[col.key])" :key="idx" class="multi-tag" :style="{ backgroundColor: getProjectColorByName(item) + '18', color: getProjectColorByName(item), borderColor: getProjectColorByName(item) + '40' }">{{ item }}</span>
                    <span v-if="!parseMultiSelect(detailRecord[col.key])?.length" class="cell-muted">-</span>
                  </div>
                  <div v-else-if="col.type === 'multi-select-tables'" class="multi-select-tags">
                    <span v-for="(item, idx) in parseMultiSelect(detailRecord[col.key])" :key="idx" class="multi-tag" :class="{ 'table-disabled': getTableStatus(item) === 'disabled' }" :style="{ backgroundColor: getProjectColorByName(item) + '18', color: getProjectColorByName(item), borderColor: getProjectColorByName(item) + '40' }">{{ item }}<span v-if="getTableStatus(item) === 'disabled'" class="status-tag-disabled-inline">{{ t('tableHierarchy.status.disabled') }}</span><span v-else-if="getTableStatus(item) === 'pending'" class="status-tag-pending-inline">{{ t('tableHierarchy.status.pending') }}</span><span v-else-if="getTableStatus(item) === 'unconfigured'" class="status-tag-unconfigured-inline">{{ t('tableHierarchy.status.unconfigured') }}</span></span>
                    <span v-if="!parseMultiSelect(detailRecord[col.key])?.length" class="cell-muted">-</span>
                  </div>
                  <div v-else-if="col.type === 'multi-select-sites'" class="multi-select-tags">
                    <span v-for="(item, idx) in parseMultiSelect(detailRecord[col.key])" :key="idx" class="multi-tag" :style="{ backgroundColor: getProjectColorByName(item) + '18', color: getProjectColorByName(item), borderColor: getProjectColorByName(item) + '40' }">{{ getSiteDisplayName(item) }}</span>
                    <span v-if="!parseMultiSelect(detailRecord[col.key])?.length" class="cell-muted">-</span>
                  </div>
                  <div v-else-if="col.type === 'multi-select-game-types'" class="multi-select-tags">
                    <span v-for="(item, idx) in parseMultiSelect(detailRecord[col.key])" :key="idx" class="multi-tag game-type-multi-tag">{{ item }}</span>
                    <span v-if="!parseMultiSelect(detailRecord[col.key])?.length" class="cell-muted">-</span>
                  </div>
                  <span v-else-if="col.type === 'qc-status'" class="qc-status-tag" :class="isQc(detailRecord[col.key], 'normal') ? 'qc-normal' : 'qc-abnormal'">{{ displayQc(detailRecord[col.key]) || '-' }}</span>
                  <div v-else-if="col.type === 'attachments'" class="detail-attachments">
                    <div v-if="getAttachmentsByKey(detailRecord, col.key)?.length" class="detail-attachment-list">
                      <img v-for="(att, idx) in getAttachmentsByKey(detailRecord, col.key)" :key="idx"
                           :src="getPresignedUrl(att.path)" :alt="att.name"
                           class="detail-thumb" @click="previewAttachments(getAttachmentsByKey(detailRecord, col.key), idx)">
                    </div>
                    <span v-else class="text-muted">-</span>
                  </div>
                  <p v-else-if="col.type === 'textarea'" class="detail-text">{{ detailRecord[col.key] || '-' }}</p>
                  <span v-else>{{ detailRecord[col.key] || '-' }}</span>
                </div>
              </template>
              <div class="detail-item">
                <label>创建人</label>
                <span>{{ detailRecord.created_by || '-' }}</span>
              </div>
            </div>
            <div class="attachments-section" v-if="detailRecord.attachments?.length">
              <h4>附件列表 ({{ detailRecord.attachments.length }})</h4>
              <div class="attachment-list">
                <div v-for="(att, idx) in detailRecord.attachments" :key="idx" class="attachment-item">
                  <div class="attachment-preview" v-if="isImageFile(att.name || att.path)">
                    <img :src="getPresignedUrl(att.path)" :alt="att.name" @click="previewImage(detailRecord.attachments, idx)">
                  </div>
                  <div v-else class="attachment-icon">
                    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M14 2H6a2 2 0 00-2 2v16a2 2 0 002 2h12a2 2 0 002-2V8z"/><polyline points="14,2 14,8 20,8"/></svg>
                  </div>
                  <div class="attachment-info">
                    <span class="att-name">{{ att.name || '未命名' }}</span>
                    <span class="att-size">{{ formatFileSize(att.size) }}</span>
                  </div>
                  <a :href="getPresignedUrl(att.path)" target="_blank" class="download-btn" title="下载">
                    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M21 15v4a2 2 0 01-2 2H5a2 2 0 01-2-2v-4M7 10l5 5 5-5M12 15V3"/></svg>
                  </a>
                </div>
              </div>
            </div>
          </div>
          <div class="modal-footer">
            <button class="btn btn-secondary" @click="showDetailModal = false">关闭</button>
            <button v-if="detailRecord && rowEditable(detailRecord)" class="btn btn-primary" @click="showDetailModal = false; openEditModal(detailRecord)">编辑</button>
          </div>
        </div>
      </div>
    </Teleport>

    <!-- 粘贴记录弹窗 -->
    <Teleport to="body">
      <div class="modal-overlay" :class="{ active: showPasteModal }">
        <div class="modal paste-modal" v-if="showPasteModal">
          <div class="modal-header">
            <div class="modal-title">
              <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M16 4h2a2 2 0 0 1 2 2v14a2 2 0 0 1-2 2H6a2 2 0 0 1-2-2V6a2 2 0 0 1 2-2h2"/><rect x="8" y="2" width="8" height="4" rx="1" ry="1"/></svg>
              粘贴记录
            </div>
            <button class="modal-close" @click="showPasteModal = false"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><line x1="18" y1="6" x2="6" y2="18"/><line x1="6" y1="6" x2="18" y2="18"/></svg></button>
          </div>
          <div class="modal-body">
            <div class="paste-info">
              <p class="paste-hint">已复制的记录将以当前日期创建新记录</p>
              <div class="paste-preview" v-if="copiedRecord">
                <div class="preview-item" v-if="copiedRecord.affected_tables">
                  <label>桌台:</label>
                  <span>{{ parseMultiSelect(copiedRecord.affected_tables).join(', ') || '-' }}</span>
                </div>
                <div class="preview-item" v-if="copiedRecord.operation">
                  <label>操作:</label>
                  <span>{{ copiedRecord.operation }}</span>
                </div>
                <div class="preview-item" v-if="copiedRecord.affected_projects">
                  <label>受影响项目:</label>
                  <span>{{ parseMultiSelect(copiedRecord.affected_projects).join(', ') || '-' }}</span>
                </div>
              </div>
            </div>
            <div class="form-group paste-count-group">
              <label>粘贴数量</label>
              <div class="paste-count-input">
                <button class="count-btn" @click="pasteCount = Math.max(1, pasteCount - 1)">-</button>
                <input type="number" v-model.number="pasteCount" min="1" max="50" class="count-input">
                <button class="count-btn" @click="pasteCount = Math.min(50, pasteCount + 1)">+</button>
              </div>
              <span class="count-hint">最多可粘贴 50 条</span>
            </div>
            <div class="paste-quick-btns">
              <button class="quick-btn" @click="pasteCount = 1">1条</button>
              <button class="quick-btn" @click="pasteCount = 5">5条</button>
              <button class="quick-btn" @click="pasteCount = 10">10条</button>
              <button class="quick-btn" @click="pasteCount = 20">20条</button>
            </div>
          </div>
          <div class="modal-footer">
            <button class="btn btn-secondary" @click="showPasteModal = false">取消</button>
            <button class="btn btn-primary" @click="pasteRecords" :disabled="loading">
              <span v-if="loading">粘贴中...</span>
              <span v-else>粘贴 {{ pasteCount }} 条记录</span>
            </button>
          </div>
        </div>
      </div>
    </Teleport>

    <!-- 批量修改质检人 -->
    <Teleport to="body">
      <div class="modal-overlay" :class="{ active: showBatchInspectorModal }">
        <div class="modal paste-modal" v-if="showBatchInspectorModal" style="max-width:440px;">
          <div class="modal-header">
            <div class="modal-title">
              <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" style="width:20px;height:20px;"><path d="M20 21v-2a4 4 0 0 0-4-4H8a4 4 0 0 0-4 4v2"/><circle cx="12" cy="7" r="4"/></svg>
              {{ t('tableMaintenance.actions.batchModifyInspectorTitle') }}
            </div>
            <button class="modal-close" @click="showBatchInspectorModal = false"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><line x1="18" y1="6" x2="6" y2="18"/><line x1="6" y1="6" x2="18" y2="18"/></svg></button>
          </div>
          <div class="modal-body">
            <p class="paste-hint" style="margin-bottom:16px;">{{ t('tableMaintenance.actions.batchModifyInspectorHint', { count: selectedIds.length }) }}</p>
            <div class="form-group">
              <label>{{ t('tableMaintenance.columns.inspector') }}</label>
              <input type="text" v-model="batchInspectorValue" list="batch-inspector-options"
                     :placeholder="t('tableMaintenance.actions.inspectorPlaceholder')"
                     class="form-input" style="width:100%;">
              <datalist id="batch-inspector-options">
                <option v-for="op in inspectorOptions" :key="op" :value="op" />
              </datalist>
            </div>
          </div>
          <div class="modal-footer">
            <button class="btn btn-secondary" @click="showBatchInspectorModal = false" :disabled="batchInspectorSaving">{{ t('common.cancel') }}</button>
            <button class="btn btn-primary" @click="confirmBatchInspector" :disabled="batchInspectorSaving || !batchInspectorValue.trim()">
              <span v-if="batchInspectorSaving">{{ t('common.loading') }}</span>
              <span v-else>{{ t('common.confirm') }}</span>
            </button>
          </div>
        </div>
      </div>
    </Teleport>

    <!-- 图片预览 -->
    <Teleport to="body">
      <div class="image-preview-modal" v-if="showImagePreview" @click="showImagePreview = false">
        <button class="preview-close">&times;</button>
        <button class="preview-nav prev" @click.stop="previewIndex = (previewIndex - 1 + previewImages.length) % previewImages.length">&lt;</button>
        <img :src="previewImages[previewIndex]" @click.stop>
        <button class="preview-nav next" @click.stop="previewIndex = (previewIndex + 1) % previewImages.length">&gt;</button>
        <div class="preview-counter">{{ previewIndex + 1 }} / {{ previewImages.length }}</div>
      </div>
    </Teleport>

      </div>
</template>

<style scoped>
.table-maintenance-page { padding: 20px; max-width: 100%; }

.page-header { display: flex; justify-content: space-between; align-items: center; margin-bottom: 20px; }
.header-left { display: flex; align-items: center; gap: 12px; }
.header-left h2 { margin: 0; font-size: 20px; font-weight: 600; color: var(--text-primary); }
.record-count { font-size: 13px; color: var(--text-secondary); background: var(--bg-secondary); padding: 4px 10px; border-radius: 12px; }
.header-actions { display: flex; gap: 10px; }

.btn { display: inline-flex; align-items: center; gap: 6px; padding: 8px 16px; border: none; border-radius: 8px; font-size: 14px; font-weight: 500; cursor: pointer; transition: all 0.2s; }
.btn svg { width: 16px; height: 16px; }
.btn-primary { background: #3a84ff; color: white; }
.btn-primary:hover { background: #2b6fd9; }
.btn-secondary { background: var(--bg-secondary); color: var(--text-primary); border: 1px solid var(--border-color); }
.btn-secondary:hover { background: var(--bg-hover); }
.btn-danger { background: #ea3636; color: white; }
.btn-danger:hover { background: #c52d2d; }
.btn-text { background: transparent; color: var(--text-secondary); }
.btn-text:hover { color: var(--text-primary); }

/* Tab 导航 */
.tab-nav { display: flex; gap: 4px; margin-bottom: 16px; background: var(--bg-secondary); padding: 4px; border-radius: 10px; width: fit-content; }
.tab-btn { display: flex; align-items: center; gap: 6px; padding: 8px 16px; border: none; background: transparent; border-radius: 8px; font-size: 14px; font-weight: 500; color: var(--text-secondary); cursor: pointer; transition: all 0.2s; }
.tab-btn svg { width: 16px; height: 16px; }
.tab-btn:hover { color: var(--text-primary); }
.tab-btn.active { background: var(--bg-card); color: #3a84ff; box-shadow: 0 2px 8px rgba(0,0,0,0.08); }

.tab-content { animation: fadeIn 0.2s ease; }
@keyframes fadeIn { from { opacity: 0; } to { opacity: 1; } }

.stats-cards { display: grid; grid-template-columns: repeat(4, 1fr); gap: 16px; margin-bottom: 20px; }
.stat-card { display: flex; align-items: center; gap: 14px; padding: 16px 20px; background: var(--bg-card); border: 1px solid var(--border-color); border-radius: 12px; }
.stat-icon { width: 48px; height: 48px; border-radius: 12px; display: flex; align-items: center; justify-content: center; }
.stat-icon svg { width: 24px; height: 24px; stroke: white; }
.stat-icon.total { background: linear-gradient(135deg, #6366f1, #8b5cf6); }
.stat-icon.today { background: linear-gradient(135deg, #10b981, #34d399); }
.stat-icon.week { background: linear-gradient(135deg, #f59e0b, #fbbf24); }
.stat-icon.month { background: linear-gradient(135deg, #3b82f6, #60a5fa); }
.stat-info { display: flex; flex-direction: column; }
.stat-label { font-size: 13px; color: var(--text-secondary); }
.stat-value { font-size: 24px; font-weight: 700; color: var(--text-primary); }

/* 卡片分组筛选器样式 */
.filter-container {
  display: flex;
  flex-direction: column;
  gap: 16px;
  margin-bottom: 20px;
}

.search-card {
  background: var(--bg-card);
  border-radius: 16px;
  padding: 20px;
  box-shadow: 0 4px 12px rgba(0, 0, 0, 0.05);
  border: 1px solid var(--border-color);
}

body.light-mode .search-card {
  box-shadow: 0 4px 12px rgba(0, 0, 0, 0.08);
}

.search-main {
  display: flex;
  gap: 12px;
}

.search-input-wrapper {
  flex: 1;
  position: relative;
}

.search-input-wrapper .search-icon {
  position: absolute;
  left: 16px;
  top: 50%;
  transform: translateY(-50%);
  width: 20px;
  height: 20px;
  color: var(--text-muted);
  pointer-events: none;
}

.search-input-wrapper input {
  width: 100%;
  padding: 14px 16px 14px 48px;
  border: 2px solid var(--border-color);
  border-radius: 12px;
  font-size: 15px;
  background: var(--bg-secondary);
  color: var(--text-primary);
  transition: all 0.2s;
}

.search-input-wrapper input::placeholder {
  color: var(--text-muted);
}

.search-input-wrapper input:focus {
  outline: none;
  border-color: #3b82f6;
  background: var(--bg-primary);
  box-shadow: 0 0 0 3px rgba(59, 130, 246, 0.15);
}

.btn-search {
  padding: 14px 28px;
  background: linear-gradient(135deg, #3b82f6 0%, #2563eb 100%);
  color: #fff;
  border: none;
  border-radius: 12px;
  font-size: 15px;
  font-weight: 600;
  cursor: pointer;
  transition: all 0.2s;
  display: flex;
  align-items: center;
  gap: 8px;
  white-space: nowrap;
}

.btn-search:hover {
  transform: translateY(-1px);
  box-shadow: 0 4px 12px rgba(59, 130, 246, 0.4);
}

.btn-search:active {
  transform: translateY(0);
}

.filters-card {
  background: var(--bg-card);
  border-radius: 16px;
  padding: 20px;
  box-shadow: 0 4px 12px rgba(0, 0, 0, 0.05);
  border: 1px solid var(--border-color);
}

body.light-mode .filters-card {
  box-shadow: 0 4px 12px rgba(0, 0, 0, 0.08);
}

.filters-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-bottom: 16px;
  padding-bottom: 14px;
  border-bottom: 1px solid var(--border-color);
}

.filters-title {
  font-size: 14px;
  font-weight: 600;
  color: var(--text-secondary);
  display: flex;
  align-items: center;
  gap: 8px;
}

.filters-title svg {
  width: 16px;
  height: 16px;
  color: var(--text-muted);
}

.btn-reset {
  padding: 8px 16px;
  background: var(--bg-secondary);
  color: var(--text-secondary);
  border: 1px solid var(--border-color);
  border-radius: 8px;
  font-size: 13px;
  font-weight: 500;
  cursor: pointer;
  transition: all 0.2s;
}

.btn-reset:hover {
  background: var(--bg-hover);
  color: var(--text-primary);
  border-color: var(--text-muted);
}

.filters-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(180px, 1fr));
  gap: 16px;
}

.filter-field {
  display: flex;
  flex-direction: column;
  gap: 6px;
}

.filter-field label {
  font-size: 12px;
  font-weight: 600;
  color: var(--text-muted);
  text-transform: uppercase;
  letter-spacing: 0.3px;
}

.filter-field select,
.filter-field input {
  width: 100%;
  padding: 10px 12px;
  border: 1px solid var(--border-color);
  border-radius: 8px;
  font-size: 14px;
  background: var(--bg-secondary);
  color: var(--text-primary);
  transition: all 0.2s;
  appearance: none;
}

.filter-field select {
  background-image: url("data:image/svg+xml,%3Csvg xmlns='http://www.w3.org/2000/svg' fill='none' viewBox='0 0 24 24' stroke='%2394a3b8'%3E%3Cpath stroke-linecap='round' stroke-linejoin='round' stroke-width='2' d='M19 9l-7 7-7-7'/%3E%3C/svg%3E");
  background-repeat: no-repeat;
  background-position: right 10px center;
  background-size: 16px;
  padding-right: 36px;
  cursor: pointer;
}

.filter-field select:focus,
.filter-field input:focus {
  outline: none;
  border-color: #3b82f6;
  background: var(--bg-primary);
  box-shadow: 0 0 0 3px rgba(59, 130, 246, 0.1);
}

.filter-field.date-field {
  grid-column: span 2;
}

.date-inputs {
  display: flex;
  align-items: center;
  gap: 10px;
}

.date-inputs input {
  flex: 1;
  min-width: 0;
}

.date-separator {
  color: var(--text-muted);
  font-size: 13px;
  white-space: nowrap;
}

@media (max-width: 900px) {
  .filters-grid {
    grid-template-columns: repeat(2, 1fr);
  }
  .filter-field.date-field {
    grid-column: span 2;
  }
}

@media (max-width: 600px) {
  .search-main {
    flex-direction: column;
  }
  .btn-search {
    justify-content: center;
  }
  .filters-grid {
    grid-template-columns: 1fr;
  }
  .filter-field.date-field {
    grid-column: span 1;
  }
  .date-inputs {
    flex-direction: column;
    gap: 8px;
  }
  .date-separator {
    display: none;
  }
}

.table-wrapper { background: var(--bg-card); border: 1px solid var(--border-color); border-radius: 12px; overflow: hidden; position: relative; }
.table-container { overflow-x: auto; }
.data-table { width: 100%; border-collapse: collapse; table-layout: fixed; }
.data-table th, .data-table td { padding: 12px 14px; text-align: left; border-bottom: 1px solid var(--border-color); white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
.data-table th { background: var(--bg-secondary); font-size: 13px; font-weight: 600; color: var(--text-secondary); position: sticky; top: 0; }
.data-table td { font-size: 14px; color: var(--text-primary); }
.data-table tr:hover { background: var(--bg-hover); }
.data-table tr.selected { background: rgba(58, 132, 255, 0.08); }

/* 固定操作列到右侧 - 默认深色主题 */
.data-table th.sticky-col, .data-table td.sticky-col {
  position: sticky;
  right: 0;
  z-index: 2;
  background: #1a1f2e;
  box-shadow: -6px 0 12px rgba(0, 0, 0, 0.3);
}
.data-table th.sticky-col {
  background: #141824;
  z-index: 3;
}
.data-table tr:nth-child(odd) td.sticky-col {
  background: #1a1f2e;
}
.data-table tr:nth-child(even) td.sticky-col {
  background: #1e2433;
}
.data-table tr:hover td.sticky-col {
  background: #252d3d;
}
/* 浅色主题 */
body.light-mode .data-table td.sticky-col,
body.light-mode .data-table tr:nth-child(odd) td.sticky-col {
  background: #ffffff;
}
body.light-mode .data-table tr:nth-child(even) td.sticky-col {
  background: #fafbfc;
}
body.light-mode .data-table th.sticky-col {
  background: #f8fafc;
  box-shadow: -6px 0 12px rgba(0, 0, 0, 0.08);
}
body.light-mode .data-table tr:hover td.sticky-col {
  background: #f1f5f9;
}
.empty { text-align: center; padding: 40px !important; color: var(--text-muted); }

.cell-date { font-family: 'SF Mono', Monaco, monospace; font-size: 13px; color: var(--text-secondary); }
.cell-text { color: var(--text-primary); }
.cell-muted { color: var(--text-muted); }
.cell-datetime { font-family: 'SF Mono', Monaco, monospace; font-size: 12px; color: var(--text-secondary); white-space: nowrap; }

.tag-table { display: inline-block; padding: 4px 10px; background: linear-gradient(135deg, #6366f1, #8b5cf6); color: white; border-radius: 6px; font-size: 13px; font-weight: 600; }

.tag-issue { display: inline-block; padding: 4px 10px; border-radius: 6px; font-size: 12px; font-weight: 500; }
.tag-issue-device { background: #fee2e2; color: #dc2626; }
.tag-issue-clean { background: #dbeafe; color: #2563eb; }
.tag-issue-damage { background: #fef3c7; color: #d97706; }
.tag-issue-wear { background: #f3e8ff; color: #9333ea; }
.tag-issue-missing { background: #fce7f3; color: #db2777; }
.tag-issue-other { background: var(--bg-secondary); color: var(--text-secondary); }

.status-tag { display: inline-block; padding: 4px 10px; border-radius: 12px; font-size: 12px; font-weight: 500; }
.status-pending { background: #fef3c7; color: #d97706; }
.status-processing { background: #dbeafe; color: #2563eb; }
.status-resolved { background: #d1fae5; color: #059669; }

.attachments-preview { display: flex; align-items: center; gap: 3px; }
.attachments-preview .thumb { width: 28px; height: 28px; object-fit: cover; border-radius: 4px; cursor: pointer; transition: transform 0.15s; border: 1px solid var(--border-color); }
.attachments-preview .thumb:hover { transform: scale(1.1); box-shadow: 0 2px 8px rgba(0,0,0,0.15); }
.more-count { font-size: 0.7rem; color: var(--text-muted); cursor: pointer; padding: 2px 4px; border-radius: 4px; background: var(--bg-hover); }
.more-count:hover { background: var(--bg-input); color: var(--text-primary); }

.action-cell { white-space: nowrap; }
.action-btns { display: flex; gap: 6px; }
.action-btn { width: 30px; height: 30px; border: none; border-radius: 6px; background: var(--bg-secondary); cursor: pointer; display: flex; align-items: center; justify-content: center; transition: all 0.2s; }
.action-btn svg { width: 15px; height: 15px; stroke: var(--text-secondary); }
.action-btn:hover { background: #3a84ff; }
.action-btn:hover svg { stroke: white; }
.action-btn.danger:hover { background: #ea3636; }
.action-btn.copy-btn:hover { background: #10b981; }

/* 粘贴弹窗样式 */
.paste-modal { width: 480px; max-width: 95vw; }
.paste-info { margin-bottom: 20px; }
.paste-hint { color: var(--text-secondary); font-size: 14px; margin-bottom: 16px; }
.paste-preview { background: var(--bg-secondary); border-radius: 8px; padding: 16px; border: 1px solid var(--border-color); }
.preview-item { display: flex; gap: 12px; margin-bottom: 8px; font-size: 13px; }
.preview-item:last-child { margin-bottom: 0; }
.preview-item label { color: var(--text-muted); min-width: 80px; }
.preview-item span { color: var(--text-primary); font-weight: 500; }
.paste-count-group { margin-bottom: 16px; }
.paste-count-group label { display: block; margin-bottom: 8px; font-weight: 500; color: var(--text-primary); }
.paste-count-input { display: flex; align-items: center; gap: 0; width: fit-content; }
.count-btn { width: 40px; height: 40px; border: 1px solid var(--border-color); background: var(--bg-secondary); color: var(--text-primary); font-size: 18px; cursor: pointer; transition: all 0.15s; }
.count-btn:first-child { border-radius: 8px 0 0 8px; }
.count-btn:last-child { border-radius: 0 8px 8px 0; }
.count-btn:hover { background: #3a84ff; color: white; border-color: #3a84ff; }
.count-input { width: 80px; height: 40px; border: 1px solid var(--border-color); border-left: none; border-right: none; background: var(--bg-input); color: var(--text-primary); font-size: 16px; font-weight: 600; text-align: center; }
.count-input:focus { outline: none; }
.count-hint { display: block; margin-top: 8px; font-size: 12px; color: var(--text-muted); }
.paste-quick-btns { display: flex; gap: 10px; flex-wrap: wrap; }
.quick-btn { padding: 8px 20px; border: 1px solid var(--border-color); border-radius: 6px; background: var(--bg-secondary); color: var(--text-secondary); font-size: 13px; cursor: pointer; transition: all 0.15s; }
.quick-btn:hover { background: #3a84ff; color: white; border-color: #3a84ff; }

.pagination { display: flex; justify-content: space-between; align-items: center; margin-top: 16px; padding: 12px 20px; background: var(--bg-card); border: 1px solid var(--border-color); border-radius: 12px; gap: 20px; flex-wrap: wrap; }
.pagination-info { font-size: 13px; color: var(--text-secondary); }
.pagination-info strong { color: var(--text-primary); font-weight: 600; }
.pagination-size { display: flex; align-items: center; gap: 8px; font-size: 13px; color: var(--text-secondary); }
.page-size-select { padding: 6px 12px; border: 1px solid var(--border-color); background: var(--bg-card); border-radius: 6px; font-size: 13px; color: var(--text-primary); cursor: pointer; min-width: 70px; }
.page-size-select:focus { outline: none; border-color: #3a84ff; }
.pagination-controls { display: flex; align-items: center; gap: 6px; }
.page-btn { width: 32px; height: 32px; display: flex; align-items: center; justify-content: center; border: 1px solid var(--border-color); background: var(--bg-card); border-radius: 6px; cursor: pointer; transition: all 0.2s; color: var(--text-primary); }
.page-btn svg { width: 16px; height: 16px; }
.page-btn:hover:not(:disabled) { background: var(--bg-hover); border-color: #3a84ff; color: #3a84ff; }
.page-btn:disabled { opacity: 0.4; cursor: not-allowed; }
.page-indicator { padding: 0 12px; font-size: 13px; color: var(--text-secondary); white-space: nowrap; }

.modal-overlay { position: fixed; top: 0; left: 0; right: 0; bottom: 0; background: rgba(0,0,0,0.5); display: flex; align-items: center; justify-content: center; z-index: 1000; opacity: 0; visibility: hidden; transition: all 0.3s; }
.modal-overlay.active { opacity: 1; visibility: visible; }
.modal { background: var(--bg-card); border-radius: 16px; width: 90%; max-width: 700px; max-height: 90vh; display: flex; flex-direction: column; box-shadow: 0 20px 60px rgba(0,0,0,0.3); }
.modal-header { display: flex; justify-content: space-between; align-items: center; padding: 20px 24px; border-bottom: 1px solid var(--border-color); }
.modal-title { display: flex; align-items: center; gap: 10px; font-size: 18px; font-weight: 600; }
.modal-title svg { width: 22px; height: 22px; stroke: #3a84ff; }
.modal-close { width: 32px; height: 32px; border: none; background: var(--bg-secondary); border-radius: 8px; cursor: pointer; display: flex; align-items: center; justify-content: center; }
.modal-close svg { width: 18px; height: 18px; stroke: var(--text-secondary); }
.modal-body { flex: 1; overflow-y: auto; padding: 24px; }
.modal-footer { display: flex; justify-content: flex-end; gap: 12px; padding: 16px 24px; border-top: 1px solid var(--border-color); }

.form-section { margin-bottom: 20px; padding-bottom: 16px; border-bottom: 1px solid var(--border-color); }
.form-section:last-child { border-bottom: none; margin-bottom: 0; }
.section-title { font-size: 14px; font-weight: 600; color: var(--text-secondary); margin-bottom: 16px; display: flex; align-items: center; gap: 8px; }
.section-title::before { content: ''; width: 3px; height: 14px; background: #3a84ff; border-radius: 2px; }
.form-row { display: grid; grid-template-columns: 1fr 1fr; gap: 16px; }
.dynamic-form-grid { display: grid; grid-template-columns: 1fr 1fr; gap: 16px; }
.dynamic-form-grid .form-group.full-width { grid-column: 1 / -1; }
.attachment-field { margin-bottom: 16px; }
.upload-zone { border: 2px dashed var(--border-color); border-radius: 10px; padding: 24px; text-align: center; cursor: pointer; transition: all 0.2s; background: var(--bg-input, var(--bg-tertiary)); outline: none; }
.upload-zone:hover, .upload-zone:focus { border-color: #3b82f6; background: rgba(59, 130, 246, 0.1); }
.upload-zone.drag-over { border-color: #3b82f6; background: rgba(59, 130, 246, 0.15); transform: scale(1.01); }
.upload-zone input[type="file"] { display: none; }
.upload-content { display: flex; flex-direction: column; align-items: center; gap: 8px; }
.upload-icon { width: 36px; height: 36px; color: #3b82f6; }
.upload-text { margin: 0; font-size: 0.875rem; color: var(--text-secondary); }
.upload-text .primary-action { color: #3b82f6; font-weight: 500; cursor: pointer; }
.upload-text .primary-action:hover { text-decoration: underline; }
.upload-hint { margin: 0; font-size: 0.75rem; color: var(--text-muted); }
.mini-uploaded-files { display: flex; flex-wrap: wrap; gap: 10px; margin-top: 12px; }
.mini-file-item { display: flex; align-items: center; gap: 8px; padding: 8px 12px; background: var(--bg-secondary); border: 1px solid var(--border-color); border-radius: 8px; font-size: 12px; }
.mini-thumb { width: 48px; height: 48px; object-fit: cover; border-radius: 6px; }
.mini-thumb.clickable { cursor: pointer; transition: all 0.2s; border: 2px solid transparent; }
.mini-thumb.clickable:hover { border-color: #3b82f6; transform: scale(1.05); box-shadow: 0 2px 8px rgba(59, 130, 246, 0.3); }
.mini-file-icon svg { width: 18px; height: 18px; color: var(--text-muted); }
.mini-file-name { max-width: 120px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; color: var(--text-secondary); }
.mini-remove-btn { width: 20px; height: 20px; border: none; background: transparent; color: var(--text-muted); cursor: pointer; font-size: 16px; line-height: 1; }
.mini-remove-btn:hover { color: #ea3636; }
.cell-select { padding: 2px 8px; background: var(--bg-tertiary); border-radius: 4px; font-size: 12px; color: var(--text-secondary); }
.cell-ellipsis { max-width: 180px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; display: inline-block; }

/* 维护时长标签 */
.duration-tag { padding: 4px 10px; border-radius: 6px; font-size: 12px; font-weight: 500; }
.duration-tag.duration-green { background: rgba(34, 197, 94, 0.15); color: #16a34a; }
.duration-tag.duration-blue { background: rgba(59, 130, 246, 0.15); color: #2563eb; }
.duration-tag.duration-orange { background: rgba(249, 115, 22, 0.15); color: #ea580c; }
.duration-tag.duration-red { background: rgba(239, 68, 68, 0.15); color: #dc2626; }

/* 是/否标签 */
.yes-no-tag { padding: 4px 10px; border-radius: 6px; font-size: 12px; font-weight: 500; }
.yes-no-tag.tag-yes { background: rgba(34, 197, 94, 0.15); color: #16a34a; }
.yes-no-tag.tag-no { background: rgba(156, 163, 175, 0.15); color: #6b7280; }

/* 质检状态标签 */
.qc-status-tag { padding: 4px 10px; border-radius: 6px; font-size: 12px; font-weight: 500; }
.qc-status-tag.qc-normal { background: rgba(34, 197, 94, 0.15); color: #16a34a; }
.qc-status-tag.qc-abnormal { background: rgba(239, 68, 68, 0.15); color: #dc2626; }

/* 质检状态下拉框 */
.qc-status-select {
  width: 100%;
  padding: 10px 14px;
  border: 1px solid var(--border-color);
  border-radius: 8px;
  font-size: 14px;
  background: var(--bg-card);
  color: var(--text-primary);
  cursor: pointer;
  appearance: none;
  background-image: url("data:image/svg+xml,%3Csvg xmlns='http://www.w3.org/2000/svg' width='12' height='12' viewBox='0 0 24 24' fill='none' stroke='%236b7280' stroke-width='2'%3E%3Cpolyline points='6 9 12 15 18 9'/%3E%3C/svg%3E");
  background-repeat: no-repeat;
  background-position: right 12px center;
  padding-right: 36px;
}
.qc-status-select:focus {
  outline: none;
  border-color: #3a84ff;
  box-shadow: 0 0 0 3px rgba(58, 132, 255, 0.12);
}
.qc-status-select option[value="正常"] { color: #16a34a; }
.qc-status-select option[value="异常"] { color: #dc2626; }

.form-group { display: flex; flex-direction: column; gap: 8px; margin-bottom: 12px; }
.form-group label { font-size: 13px; font-weight: 500; color: var(--text-secondary); }
.form-group .required { color: #ea3636; }
.form-group input, .form-group select, .form-group textarea { padding: 10px 14px; background: var(--bg-input, var(--bg-primary)); border: 1px solid var(--border-color); border-radius: 8px; color: var(--text-primary); font-size: 14px; font-family: inherit; transition: border-color 0.2s; }
.form-group input:focus, .form-group select:focus, .form-group textarea:focus { outline: none; border-color: #3a84ff; }
.form-group textarea.required-field { border-color: #ea3636; background: rgba(234, 54, 54, 0.05); }
.form-group textarea.required-field::placeholder { color: #ea3636; opacity: 0.7; }
.remark-cell { max-width: 200px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.form-group textarea { resize: vertical; min-height: 70px; }

/* 下拉框通用样式 */
.form-select { 
  width: 100%; 
  padding: 10px 14px; 
  border: 1px solid var(--border-color); 
  border-radius: 8px; 
  font-size: 14px; 
  background: var(--bg-card); 
  color: var(--text-primary); 
  cursor: pointer; 
  transition: all 0.2s;
}
.form-select:focus { 
  outline: none; 
  border-color: #3a84ff; 
  box-shadow: 0 0 0 3px rgba(58, 132, 255, 0.12); 
}
.form-select:hover { 
  border-color: #3a84ff; 
}

/* 维护时长下拉框 */
.duration-dropdown { 
  background-image: url("data:image/svg+xml,%3Csvg xmlns='http://www.w3.org/2000/svg' viewBox='0 0 24 24' fill='none' stroke='%236b7280' stroke-width='2'%3E%3Cpolyline points='6 9 12 15 18 9'%3E%3C/polyline%3E%3C/svg%3E");
  background-repeat: no-repeat;
  background-position: right 12px center;
  background-size: 16px;
  padding-right: 36px;
  appearance: none;
}

/* 多选下拉框 */
.multi-select-dropdown-wrapper { 
  width: 100%; 
  position: relative;
}
.multi-select-trigger {
  display: flex;
  align-items: center;
  justify-content: space-between;
  min-height: 42px;
  padding: 8px 12px;
  border: 1px solid var(--border-color);
  border-radius: 8px;
  background: var(--bg-card);
  cursor: pointer;
  transition: all 0.2s;
}
.multi-select-trigger:hover {
  border-color: #3a84ff;
}
.multi-select-selected {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
  flex: 1;
}
.multi-select-selected .placeholder {
  color: var(--text-muted);
  font-size: 14px;
}
.multi-select-selected .selected-tag {
  display: inline-flex;
  align-items: center;
  padding: 2px 8px;
  border-radius: 4px;
  font-size: 12px;
  font-weight: 500;
  border: 1px solid;
}
.multi-select-selected .more-tag {
  display: inline-flex;
  align-items: center;
  padding: 2px 8px;
  border-radius: 4px;
  font-size: 12px;
  background: var(--bg-sidebar);
  color: var(--text-muted);
}
.dropdown-arrow {
  width: 18px;
  height: 18px;
  color: var(--text-muted);
  transition: transform 0.2s;
  flex-shrink: 0;
  margin-left: 8px;
}
.dropdown-arrow.open {
  transform: rotate(180deg);
}
.multi-select-panel {
  position: absolute;
  top: 100%;
  left: 0;
  right: 0;
  margin-top: 4px;
  max-height: 240px;
  overflow-y: auto;
  background: var(--bg-card);
  border: 1px solid var(--border-color);
  border-radius: 8px;
  box-shadow: 0 4px 12px rgba(0,0,0,0.15);
  z-index: 100;
  padding: 6px;
}
.multi-select-panel.with-search {
  max-height: 320px;
  overflow: hidden;
  display: flex;
  flex-direction: column;
}
.panel-search-box {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 8px 10px;
  border-bottom: 1px solid var(--border-color);
  background: var(--bg-sidebar);
  border-radius: 6px 6px 0 0;
  margin: -6px -6px 6px -6px;
  flex-shrink: 0;
}
.panel-search-box svg {
  width: 16px;
  height: 16px;
  color: var(--text-muted);
  flex-shrink: 0;
}
.panel-search-input {
  flex: 1;
  border: none;
  background: transparent;
  font-size: 13px;
  color: var(--text-primary);
  outline: none;
}
.panel-search-input::placeholder {
  color: var(--text-muted);
}
.search-clear {
  background: none;
  border: none;
  padding: 2px;
  cursor: pointer;
  color: var(--text-muted);
  display: flex;
  align-items: center;
  justify-content: center;
  border-radius: 4px;
  transition: all 0.15s;
}
.search-clear:hover {
  background: rgba(0,0,0,0.1);
  color: var(--text-primary);
}
.search-clear svg {
  width: 14px;
  height: 14px;
}
.panel-options-list {
  flex: 1;
  overflow-y: auto;
  max-height: 240px;
}
.no-match {
  padding: 16px;
  text-align: center;
  color: var(--text-muted);
  font-size: 13px;
}
/* 全选按钮样式 */
.select-all-row {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 10px 14px;
  cursor: pointer;
  border-bottom: 1px solid var(--border-color);
  background: var(--bg-hover);
  transition: background 0.15s;
  font-weight: 600;
}
.select-all-row:hover {
  background: rgba(58, 132, 255, 0.12);
}
.select-all-checkbox {
  width: 18px;
  height: 18px;
  border-radius: 4px;
  border: 2px solid var(--border-color);
  display: flex;
  align-items: center;
  justify-content: center;
  transition: all 0.15s;
}
.select-all-checkbox.checked {
  background: #3a84ff;
  border-color: #3a84ff;
}
.select-all-checkbox svg {
  width: 12px;
  height: 12px;
}
.select-all-label {
  font-size: 13px;
  color: #3a84ff;
}

.multi-select-option {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 10px 12px;
  border-radius: 6px;
  cursor: pointer;
  transition: background 0.15s;
}
.multi-select-option:hover {
  background: var(--bg-sidebar);
}
.multi-select-option.checked {
  background: rgba(58, 132, 255, 0.08);
}
.checkbox-icon {
  width: 18px;
  height: 18px;
  border: 2px solid var(--border-color);
  border-radius: 4px;
  display: flex;
  align-items: center;
  justify-content: center;
  transition: all 0.15s;
  flex-shrink: 0;
}
.checkbox-icon svg {
  width: 12px;
  height: 12px;
}
.option-label {
  font-size: 14px;
  font-weight: 500;
}
.multi-select-hint {
  font-size: 12px;
  color: var(--text-muted);
  margin-top: 6px;
}

/* 旧版维护时长选择组（保留兼容） */
.duration-select-group { display: flex; flex-wrap: wrap; gap: 8px; }
.duration-option { display: flex; align-items: center; padding: 8px 14px; border-radius: 8px; cursor: pointer; transition: all 0.2s; border: 2px solid transparent; font-size: 13px; font-weight: 500; }
.duration-option input { display: none; }
.duration-option.duration-green { background: rgba(34, 197, 94, 0.1); color: #16a34a; border-color: rgba(34, 197, 94, 0.3); }
.duration-option.duration-green.active { background: #22c55e; color: white; border-color: #22c55e; }
.duration-option.duration-blue { background: rgba(59, 130, 246, 0.1); color: #2563eb; border-color: rgba(59, 130, 246, 0.3); }
.duration-option.duration-blue.active { background: #3b82f6; color: white; border-color: #3b82f6; }
.duration-option.duration-orange { background: rgba(249, 115, 22, 0.1); color: #ea580c; border-color: rgba(249, 115, 22, 0.3); }
.duration-option.duration-orange.active { background: #f97316; color: white; border-color: #f97316; }
.duration-option.duration-red { background: rgba(239, 68, 68, 0.1); color: #dc2626; border-color: rgba(239, 68, 68, 0.3); }
.duration-option.duration-red.active { background: #ef4444; color: white; border-color: #ef4444; }
.duration-option:hover:not(.active) { filter: brightness(0.95); transform: translateY(-1px); }

/* 是/否选择组 */
.yes-no-group { display: flex; gap: 10px; }
.yes-no-option { display: flex; align-items: center; padding: 10px 20px; border-radius: 8px; cursor: pointer; transition: all 0.2s; border: 2px solid transparent; font-size: 14px; font-weight: 500; }
.yes-no-option input { display: none; }
.yes-no-option.opt-yes { background: rgba(34, 197, 94, 0.1); color: #16a34a; border-color: rgba(34, 197, 94, 0.3); }
.yes-no-option.opt-yes.active { background: #22c55e; color: white; border-color: #22c55e; }
.yes-no-option.opt-no { background: rgba(156, 163, 175, 0.1); color: #6b7280; border-color: rgba(156, 163, 175, 0.3); }
.yes-no-option.opt-no.active { background: #6b7280; color: white; border-color: #6b7280; }
.yes-no-option:hover:not(.active) { filter: brightness(0.95); }

.upload-area { border: 2px dashed var(--border-color); border-radius: 12px; padding: 32px; text-align: center; cursor: pointer; transition: all 0.2s; }
.upload-area:hover, .upload-area.dragover { border-color: #3a84ff; background: rgba(58, 132, 255, 0.05); }
.upload-area svg { width: 40px; height: 40px; stroke: var(--text-muted); margin-bottom: 8px; }
.upload-area p { margin: 0; color: var(--text-secondary); font-size: 14px; }

.uploaded-files { margin-top: 16px; display: flex; flex-direction: column; gap: 8px; }
.uploaded-file { display: flex; align-items: center; gap: 12px; padding: 10px 14px; background: var(--bg-secondary); border-radius: 8px; }
.file-icon { width: 32px; height: 32px; display: flex; align-items: center; justify-content: center; }
.file-icon svg { width: 20px; height: 20px; stroke: var(--text-secondary); }
.file-name { flex: 1; font-size: 14px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.file-size { font-size: 12px; color: var(--text-muted); }
.remove-btn { width: 24px; height: 24px; border: none; background: #ea3636; color: white; border-radius: 6px; cursor: pointer; font-size: 16px; display: flex; align-items: center; justify-content: center; }

.detail-modal { max-width: 800px; }
.detail-grid { display: grid; grid-template-columns: repeat(3, 1fr); gap: 16px; margin-bottom: 20px; }
.detail-item { display: flex; flex-direction: column; gap: 6px; }
.detail-item.full { grid-column: 1 / -1; }
.detail-item label { font-size: 12px; color: var(--text-secondary); }
.detail-item span { font-size: 14px; color: var(--text-primary); }
.detail-text { margin: 0; font-size: 14px; color: var(--text-primary); line-height: 1.6; background: var(--bg-secondary); padding: 12px; border-radius: 8px; }
.detail-attachments { margin-top: 4px; }
.detail-attachment-list { display: flex; flex-wrap: wrap; gap: 8px; }
.detail-thumb { width: 72px; height: 72px; object-fit: cover; border-radius: 8px; cursor: pointer; border: 1px solid var(--border-color); transition: transform 0.15s, box-shadow 0.15s; }
.detail-thumb:hover { transform: scale(1.05); box-shadow: 0 4px 12px rgba(0,0,0,0.15); }
.text-muted { color: var(--text-muted); }

.attachments-section { margin-top: 20px; padding-top: 20px; border-top: 1px solid var(--border-color); }
.attachments-section h4 { font-size: 15px; font-weight: 600; margin: 0 0 16px; }
.attachment-list { display: grid; grid-template-columns: repeat(auto-fill, minmax(200px, 1fr)); gap: 12px; }
.attachment-item { display: flex; align-items: center; gap: 12px; padding: 12px; background: var(--bg-secondary); border-radius: 10px; }
.attachment-preview { width: 48px; height: 48px; border-radius: 8px; overflow: hidden; flex-shrink: 0; cursor: pointer; }
.attachment-preview img { width: 100%; height: 100%; object-fit: cover; }
.attachment-icon { width: 48px; height: 48px; background: var(--bg-tertiary, var(--bg-hover)); border-radius: 8px; display: flex; align-items: center; justify-content: center; flex-shrink: 0; }
.attachment-icon svg { width: 24px; height: 24px; stroke: var(--text-secondary); }
.attachment-info { flex: 1; min-width: 0; }
.att-name { display: block; font-size: 13px; color: var(--text-primary); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.att-size { font-size: 12px; color: var(--text-muted); }
.download-btn { width: 32px; height: 32px; border-radius: 6px; background: #3a84ff; display: flex; align-items: center; justify-content: center; flex-shrink: 0; text-decoration: none; }
.download-btn svg { width: 16px; height: 16px; stroke: white; }

.column-modal { max-width: 750px; }
.column-config-header { display: flex; justify-content: space-between; align-items: center; margin-bottom: 16px; padding-bottom: 12px; border-bottom: 1px solid var(--border-color); }
.config-tip { font-size: 13px; color: var(--text-muted); }
.btn-sm { padding: 6px 12px; font-size: 13px; }
.btn-sm svg { width: 14px; height: 14px; }

.column-list { display: flex; flex-direction: column; gap: 8px; max-height: 400px; overflow-y: auto; }
.column-item { display: flex; align-items: center; gap: 12px; padding: 12px 14px; background: var(--bg-secondary); border-radius: 8px; cursor: grab; transition: all 0.2s; }
.column-item:hover { background: var(--bg-hover); }
.column-item.dragging { opacity: 0.5; background: rgba(58, 132, 255, 0.1); }
.column-item.system { border-left: 3px solid #3a84ff; }

.column-drag-handle { width: 20px; height: 20px; display: flex; align-items: center; justify-content: center; color: var(--text-muted); cursor: grab; flex-shrink: 0; }
.column-drag-handle svg { width: 16px; height: 16px; stroke: var(--text-muted); }

.column-checkbox { display: flex; align-items: center; gap: 8px; flex: 1; min-width: 0; }
.column-checkbox input { width: 16px; height: 16px; flex-shrink: 0; }
.col-title { font-size: 14px; font-weight: 500; color: var(--text-primary); }
.col-key { font-size: 12px; color: var(--text-muted); font-family: 'SF Mono', Monaco, monospace; }

.column-type { flex-shrink: 0; }
.column-type select { padding: 6px 10px; border: 1px solid var(--border-color); border-radius: 6px; font-size: 12px; background: var(--bg-primary); color: var(--text-primary); min-width: 90px; }
.column-type select:disabled { opacity: 0.6; cursor: not-allowed; }

.column-width { display: flex; align-items: center; gap: 4px; font-size: 12px; color: var(--text-secondary); flex-shrink: 0; }
.column-width input { width: 55px; padding: 6px 8px; border: 1px solid var(--border-color); border-radius: 6px; text-align: center; font-size: 12px; background: var(--bg-primary); color: var(--text-primary); }

.column-actions { display: flex; gap: 4px; flex-shrink: 0; }
.col-action-btn { width: 28px; height: 28px; border: none; border-radius: 6px; background: var(--bg-primary); cursor: pointer; display: flex; align-items: center; justify-content: center; transition: all 0.2s; }
.col-action-btn svg { width: 14px; height: 14px; stroke: var(--text-secondary); }
.col-action-btn:hover:not(:disabled) { background: #3a84ff; }
.col-action-btn:hover:not(:disabled) svg { stroke: white; }
.col-action-btn.danger:hover:not(:disabled) { background: #ea3636; }
.col-action-btn:disabled { opacity: 0.3; cursor: not-allowed; }

.add-column-modal { max-width: 500px; }
.form-hint { font-size: 12px; color: var(--text-muted); margin-top: 4px; }

.record-form-modal { max-width: 650px; }

/* 统计分析面板样式 */
.stats-tab { background: var(--bg-card); border: 1px solid var(--border-color); border-radius: 12px; padding: 24px; }
.stats-overview { display: flex; flex-direction: column; gap: 24px; }
.stats-row { display: grid; grid-template-columns: repeat(2, 1fr); gap: 24px; }
.stats-row.maint-duration-compare { grid-template-columns: repeat(3, 1fr); }
.stats-section { background: var(--bg-secondary); border-radius: 12px; padding: 20px; }
.stats-section.highlight-section { background: linear-gradient(135deg, rgba(99, 102, 241, 0.15), rgba(139, 92, 246, 0.15)); border: 2px solid rgba(99, 102, 241, 0.3); }
.stats-section h4 { margin: 0 0 16px; font-size: 15px; font-weight: 600; color: var(--text-primary); }

.overview-cards { display: grid; grid-template-columns: repeat(4, 1fr); gap: 12px; }
.overview-card { background: var(--bg-card); border: 1px solid var(--border-color); border-radius: 10px; padding: 16px; text-align: center; }
.overview-card .ov-value { display: block; font-size: 28px; font-weight: 700; color: var(--text-primary); }
.overview-card .ov-label { display: block; font-size: 12px; color: var(--text-muted); margin-top: 4px; }
.overview-card.today { background: rgba(16, 185, 129, 0.1); border-color: rgba(16, 185, 129, 0.3); }
.overview-card.today .ov-value { color: #10b981; }
.overview-card.week { background: rgba(245, 158, 11, 0.1); border-color: rgba(245, 158, 11, 0.3); }
.overview-card.week .ov-value { color: #f59e0b; }
.overview-card.month { background: rgba(59, 130, 246, 0.1); border-color: rgba(59, 130, 246, 0.3); }
.overview-card.month .ov-value { color: #3b82f6; }

.stats-list { display: flex; flex-direction: column; gap: 8px; }
.stats-list.scrollable { max-height: 300px; overflow-y: auto; padding-right: 8px; }
.stats-item { display: flex; align-items: center; gap: 12px; }
.stats-item .item-name { width: 80px; font-size: 13px; color: var(--text-primary); font-weight: 500; }
.stats-item .item-bar { flex: 1; height: 8px; background: var(--bg-hover); border-radius: 4px; overflow: hidden; }
.stats-item .bar-fill { height: 100%; background: linear-gradient(90deg, #6366f1, #8b5cf6); border-radius: 4px; transition: width 0.3s; }
.stats-item .bar-fill.site { background: linear-gradient(90deg, #10b981, #34d399); }
.stats-item .item-count { width: 40px; text-align: right; font-size: 14px; font-weight: 600; color: var(--text-primary); }

.duration-stats { display: flex; flex-wrap: wrap; gap: 10px; }
.duration-item { display: flex; flex-direction: column; align-items: center; padding: 12px 18px; border-radius: 10px; min-width: 90px; }
.duration-item .dur-name { font-size: 12px; color: inherit; margin-bottom: 4px; }
.duration-item .dur-count { font-size: 22px; font-weight: 700; }
.duration-item.duration-green { background: rgba(34, 197, 94, 0.15); color: #16a34a; }
.duration-item.duration-blue { background: rgba(59, 130, 246, 0.15); color: #3b82f6; }
.duration-item.duration-orange { background: rgba(245, 158, 11, 0.15); color: #f59e0b; }
.duration-item.duration-red { background: rgba(239, 68, 68, 0.15); color: #dc2626; }

/* 维护时长明细表格 */
.duration-detail-table { max-height: 400px; overflow-y: auto; border: 1px solid var(--border-color); border-radius: 8px; background: var(--bg-card); }
.duration-detail-table table { width: 100%; border-collapse: separate; border-spacing: 0; font-size: 13px; }
.duration-detail-table thead { position: sticky; top: 0; z-index: 10; }
.duration-detail-table th { background: #f1f5f9; padding: 10px 12px; text-align: left; font-weight: 600; color: var(--text-secondary); border-bottom: 2px solid var(--border-color); box-shadow: 0 1px 0 var(--border-color); }
[data-theme="dark"] .duration-detail-table th { background: #1e293b; }
.duration-detail-table td { padding: 10px 12px; border-bottom: 1px solid var(--border-color); color: var(--text-primary); background: var(--bg-card); }
.duration-detail-table tbody tr:hover td { background: var(--bg-hover); }
.duration-detail-table tbody tr:last-child td { border-bottom: none; }
.dur-badge { display: inline-block; padding: 4px 10px; border-radius: 6px; font-size: 12px; font-weight: 500; }
.dur-badge.duration-green { background: rgba(34, 197, 94, 0.15); color: #16a34a; }
.dur-badge.duration-blue { background: rgba(59, 130, 246, 0.15); color: #3b82f6; }
.dur-badge.duration-orange { background: rgba(245, 158, 11, 0.15); color: #f59e0b; }
.dur-badge.duration-red { background: rgba(239, 68, 68, 0.15); color: #dc2626; }
.dur-badge.total { font-weight: 700; }

.affect-stats, .qc-stats { display: flex; gap: 16px; }
.affect-item, .qc-item { flex: 1; display: flex; flex-direction: column; align-items: center; padding: 16px; border-radius: 10px; }
.affect-item .affect-label, .qc-item .qc-label { font-size: 13px; margin-bottom: 6px; }
.affect-item .affect-value, .qc-item .qc-value { font-size: 28px; font-weight: 700; }
.affect-item.yes { background: rgba(34, 197, 94, 0.15); color: #16a34a; }
.affect-item.no { background: rgba(156, 163, 175, 0.15); color: #6b7280; }
.qc-item.normal { background: rgba(34, 197, 94, 0.15); color: #16a34a; }
.qc-item.abnormal { background: rgba(239, 68, 68, 0.15); color: #dc2626; }
.qc-item.pending { background: rgba(156, 163, 175, 0.15); color: #6b7280; }

.empty-stats { text-align: center; color: var(--text-muted); font-size: 14px; padding: 20px; }

/* 统计面板筛选器 */
.stats-filter { display: flex; align-items: flex-start; gap: 16px; padding: 16px 20px; background: var(--bg-secondary); border-radius: 12px; margin-bottom: 20px; flex-wrap: wrap; }
.filter-group { display: flex; align-items: center; gap: 8px; }
.filter-label { font-size: 14px; color: var(--text-secondary); font-weight: 500; white-space: nowrap; }
.stats-date-input { padding: 8px 12px; border: 1px solid var(--border-color); border-radius: 8px; font-size: 14px; background: var(--bg-card); color: var(--text-primary); min-width: 140px; }
.stats-date-input:focus { outline: none; border-color: #3a84ff; }
.stats-select-input { padding: 8px 12px; border: 1px solid var(--border-color); border-radius: 8px; font-size: 14px; background: var(--bg-card); color: var(--text-primary); min-width: 120px; cursor: pointer; }
.stats-select-input:focus { outline: none; border-color: #3a84ff; }
.stats-text-input { padding: 8px 12px; border: 1px solid var(--border-color); border-radius: 8px; font-size: 14px; background: var(--bg-card); color: var(--text-primary); min-width: 120px; }
.stats-text-input:focus { outline: none; border-color: #3a84ff; }
.round-id-cell { max-width: 200px; word-break: break-all; }
.date-separator { color: var(--text-muted); font-size: 14px; }
.filter-actions { display: flex; align-items: center; gap: 8px; margin-left: auto; }
.filter-result { margin-left: auto; font-size: 14px; color: var(--text-secondary); }
.filter-result strong { color: #3a84ff; font-size: 18px; font-weight: 700; }

/* 忽略创建人 多选下拉 */
.exclude-creator-group { position: relative; }
.exclude-trigger { display: inline-flex; align-items: center; justify-content: space-between; gap: 6px; min-width: 180px; max-width: 260px; text-align: left; }
.exclude-trigger.has-exclude { border-color: #f59e0b; color: #b45309; background: rgba(245, 158, 11, 0.08); }
.exclude-trigger > span:first-child { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.exclude-caret { width: 14px; height: 14px; flex-shrink: 0; }
.exclude-backdrop { position: fixed; inset: 0; z-index: 40; background: transparent; }
.exclude-dropdown { position: absolute; top: calc(100% + 4px); left: 0; z-index: 41; min-width: 240px; max-width: 320px; background: var(--bg-card); border: 1px solid var(--border-color); border-radius: 8px; box-shadow: 0 8px 24px rgba(0,0,0,0.12); padding: 8px; }
.exclude-row { display: flex; align-items: center; gap: 8px; padding: 6px 8px; border-radius: 6px; cursor: pointer; font-size: 13px; color: var(--text-primary); }
.exclude-row:hover { background: var(--bg-secondary); }
.exclude-row input[type="checkbox"] { margin: 0; cursor: pointer; }
.exclude-row span { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.exclude-apikey-row { font-weight: 500; color: #b45309; }
.exclude-divider { height: 1px; background: var(--border-color); margin: 6px 0; }
.exclude-list { max-height: 220px; overflow-y: auto; }
.exclude-empty { padding: 12px 8px; text-align: center; font-size: 12px; color: var(--text-muted); }
.exclude-actions-bar { display: flex; align-items: center; gap: 8px; padding: 8px 8px 4px; border-top: 1px solid var(--border-color); margin-top: 4px; }
.exclude-link { background: none; border: none; padding: 2px 6px; font-size: 12px; color: #3a84ff; cursor: pointer; }
.exclude-link:hover { text-decoration: underline; }
.exclude-hint { margin-left: auto; font-size: 11px; color: var(--text-muted); }

/* 操作类型统计卡片 */
.stats-row.single { grid-template-columns: 1fr; }
.operation-section { background: linear-gradient(135deg, rgba(99, 102, 241, 0.1), rgba(139, 92, 246, 0.1)); }
.operation-stats { display: grid; grid-template-columns: repeat(4, 1fr); gap: 16px; }
.operation-card { display: flex; flex-direction: column; align-items: center; justify-content: center; padding: 24px 16px; border-radius: 12px; background: var(--bg-card); border: 2px solid var(--border-color); transition: all 0.2s; }
.operation-card:hover { transform: translateY(-2px); box-shadow: 0 4px 12px rgba(0,0,0,0.1); }
.operation-card .op-count { font-size: 36px; font-weight: 700; line-height: 1; }
.operation-card .op-name { font-size: 14px; font-weight: 500; margin-top: 8px; }
.operation-card.op-maintenance { border-color: rgba(59, 130, 246, 0.4); }
.operation-card.op-maintenance .op-count { color: #3b82f6; }
.operation-card.op-cancel { border-color: rgba(239, 68, 68, 0.4); }
.operation-card.op-cancel .op-count { color: #ef4444; }
.operation-card.op-recalculate { border-color: rgba(245, 158, 11, 0.4); }
.operation-card.op-recalculate .op-count { color: #f59e0b; }
.operation-card.op-repayout { border-color: rgba(16, 185, 129, 0.4); }
.operation-card.op-repayout .op-count { color: #10b981; }
.operation-card.op-vipTable { border-color: rgba(168, 85, 247, 0.4); }
.operation-card.op-vipTable .op-count { color: #a855f7; }

/* 操作人/质检人/影响项目柱状图颜色 */
.stats-item .bar-fill.operator { background: linear-gradient(90deg, #f59e0b, #fbbf24); }
.stats-item .bar-fill.inspector { background: linear-gradient(90deg, #8b5cf6, #a78bfa); }
.stats-item .bar-fill.affect-project { background: linear-gradient(90deg, #6366f1, #818cf8); }
.stats-item .bar-fill.no-affect { background: linear-gradient(90deg, #94a3b8, #cbd5e1); }
.text-muted { color: var(--text-muted) !important; }

/* 维护类型/操作类型时长分布卡片 */
.type-duration-grid { display: grid; grid-template-columns: repeat(4, 1fr); gap: 16px; }
.type-duration-card { background: var(--bg-card); border: 1px solid var(--border-color); border-radius: 12px; padding: 16px; }
.type-duration-card .tdc-header { display: flex; justify-content: space-between; align-items: center; margin-bottom: 12px; padding-bottom: 10px; border-bottom: 1px solid var(--border-color); }
.type-duration-card .tdc-name { font-size: 15px; font-weight: 600; color: var(--text-primary); }
.type-duration-card .tdc-total { font-size: 20px; font-weight: 700; color: #6366f1; }
.type-duration-card .tdc-durations { display: flex; flex-direction: column; gap: 8px; }
.type-duration-card .tdc-dur { display: flex; justify-content: space-between; align-items: center; padding: 6px 10px; border-radius: 6px; font-size: 13px; }
.type-duration-card .tdc-dur .tdc-dur-label { color: inherit; }
.type-duration-card .tdc-dur .tdc-dur-count { font-weight: 600; }
.type-duration-card .tdc-dur.duration-green { background: rgba(34, 197, 94, 0.1); color: #16a34a; }
.type-duration-card .tdc-dur.duration-blue { background: rgba(59, 130, 246, 0.1); color: #2563eb; }
.type-duration-card .tdc-dur.duration-orange { background: rgba(249, 115, 22, 0.1); color: #ea580c; }
.type-duration-card .tdc-dur.duration-red { background: rgba(239, 68, 68, 0.1); color: #dc2626; }

/* 操作类型卡片颜色 */
.type-duration-card.op-card.op-maintenance { border-left: 4px solid #3b82f6; }
.type-duration-card.op-card.op-maintenance .tdc-total { color: #3b82f6; }
.type-duration-card.op-card.op-cancel { border-left: 4px solid #ef4444; }
.type-duration-card.op-card.op-cancel .tdc-total { color: #ef4444; }
.type-duration-card.op-card.op-recalculate { border-left: 4px solid #f59e0b; }
.type-duration-card.op-card.op-recalculate .tdc-total { color: #f59e0b; }
.type-duration-card.op-card.op-repayout { border-left: 4px solid #10b981; }
.type-duration-card.op-card.op-repayout .tdc-total { color: #10b981; }
.type-duration-card.op-card.op-vipTable { border-left: 4px solid #a855f7; }
.type-duration-card.op-card.op-vipTable .tdc-total { color: #a855f7; }

/* 按项目 × 操作类型矩阵 */
.project-op-grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(320px, 1fr)); gap: 12px; }
.project-op-card { background: var(--bg-card); border: 1px solid var(--border-color); border-radius: 8px; padding: 12px; }
.pop-header { display: flex; justify-content: space-between; align-items: center; margin-bottom: 10px; padding-bottom: 8px; border-bottom: 1px solid var(--border-color); }
.pop-project { font-weight: 600; font-size: 14px; color: var(--text-primary); }
.pop-total { font-weight: 700; font-size: 14px; color: #6366f1; }
.pop-ops { display: grid; grid-template-columns: repeat(5, 1fr); gap: 6px; }
.pop-op { display: flex; flex-direction: column; align-items: center; padding: 6px 4px; border-radius: 6px; background: var(--bg-elevated, #f9fafb); font-size: 11px; }
.pop-op-name { color: var(--text-secondary); margin-bottom: 2px; }
.pop-op-count { font-weight: 700; font-size: 15px; color: var(--text-primary); }
.pop-op.op-maintenance { background: rgba(59, 130, 246, 0.08); }
.pop-op.op-maintenance .pop-op-count { color: #3b82f6; }
.pop-op.op-cancel { background: rgba(239, 68, 68, 0.08); }
.pop-op.op-cancel .pop-op-count { color: #ef4444; }
.pop-op.op-recalculate { background: rgba(245, 158, 11, 0.08); }
.pop-op.op-recalculate .pop-op-count { color: #f59e0b; }
.pop-op.op-repayout { background: rgba(16, 185, 129, 0.08); }
.pop-op.op-repayout .pop-op-count { color: #10b981; }
.pop-op.op-vipTable { background: rgba(168, 85, 247, 0.08); }
.pop-op.op-vipTable .pop-op-count { color: #a855f7; }

/* 桌台统计表格样式 */
.table-stats-table { max-height: 400px; overflow-y: auto; border: 1px solid var(--border-color); border-radius: 8px; background: var(--bg-card); }
.table-stats-table table { width: 100%; border-collapse: separate; border-spacing: 0; font-size: 13px; }
.table-stats-table thead { position: sticky; top: 0; z-index: 10; }
.table-stats-table th { background: #f1f5f9; padding: 10px 12px; text-align: left; font-weight: 600; color: var(--text-secondary); border-bottom: 2px solid var(--border-color); box-shadow: 0 1px 0 var(--border-color); }
[data-theme="dark"] .table-stats-table th { background: #1e293b; }
.table-stats-table td { padding: 10px 12px; border-bottom: 1px solid var(--border-color); color: var(--text-primary); background: var(--bg-card); }
.table-stats-table tbody tr:hover td { background: var(--bg-hover); }
.table-stats-table tbody tr:last-child td { border-bottom: none; }
.table-stats-table .projects-cell { max-width: 200px; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
.table-no-tag { display: inline-block; padding: 4px 10px; background: linear-gradient(135deg, #6366f1, #8b5cf6); color: white; border-radius: 6px; font-size: 13px; font-weight: 600; min-width: 50px; text-align: center; }
.count-badge { display: inline-block; padding: 4px 12px; background: rgba(99, 102, 241, 0.15); color: #6366f1; border-radius: 6px; font-weight: 700; }
.table-no-count { font-size: 14px; font-weight: 600; color: var(--text-primary); min-width: 40px; text-align: right; }

@media (max-width: 1200px) {
  .stats-cards { grid-template-columns: repeat(2, 1fr); }
  .stats-row { grid-template-columns: 1fr; }
  .stats-row.maint-duration-compare { grid-template-columns: 1fr; }
  .overview-cards { grid-template-columns: repeat(2, 1fr); }
  .operation-stats { grid-template-columns: repeat(2, 1fr); }
  .table-no-item { max-width: calc(50% - 6px); }
  .type-duration-grid { grid-template-columns: repeat(2, 1fr); }
}
@media (max-width: 768px) {
  .stats-cards { grid-template-columns: 1fr; }
  .detail-grid { grid-template-columns: 1fr 1fr; }
  .form-row { grid-template-columns: 1fr; }
  .overview-cards { grid-template-columns: 1fr 1fr; }
  .operation-stats { grid-template-columns: 1fr 1fr; }
  .stats-filter { flex-direction: column; align-items: stretch; }
  .filter-group { flex-wrap: wrap; }
  .filter-actions { margin-left: 0; margin-top: 8px; }
  .filter-result { margin-left: 0; margin-top: 8px; }
  .table-no-item { max-width: 100%; }
  .type-duration-grid { grid-template-columns: 1fr; }
}

/* 多选标签显示 */
.multi-select-tags { display: flex; flex-wrap: wrap; gap: 4px; }
.multi-tag { display: inline-block; padding: 3px 10px; border-radius: 6px; font-size: 12px; font-weight: 500; border: 1px solid; }
.multi-tag.game-type-multi-tag { background: rgba(16, 185, 129, 0.12); color: #059669; border-color: rgba(16, 185, 129, 0.3); }
.selected-tag.game-type-tag-selected { background: rgba(16, 185, 129, 0.12); color: #059669; border-color: rgba(16, 185, 129, 0.3); }
.game-type-label { color: #059669 !important; }

/* 多选编辑控件 */
.multi-select-group { width: 100%; }
.multi-select-checkboxes { display: flex; flex-wrap: wrap; gap: 8px; }
.multi-check-option { display: flex; align-items: center; gap: 6px; padding: 6px 12px; border: 1px solid var(--border-color); border-radius: 6px; cursor: pointer; transition: all 0.2s; font-size: 13px; }
.multi-check-option:hover { border-color: #6366f1; }
.multi-check-option.active { background: rgba(99, 102, 241, 0.1); border-color: #6366f1; color: #6366f1; }
.multi-check-option input { display: none; }
.empty-options { display: flex; align-items: center; gap: 8px; color: var(--text-muted); font-size: 13px; }
.btn-link { background: none; border: none; color: #3a84ff; cursor: pointer; font-size: 13px; }
.btn-link:hover { text-decoration: underline; }

.btn-sm { padding: 8px 16px; font-size: 13px; }

/* 自动填充标签（只读展示） */
.auto-tags { display: flex; flex-wrap: wrap; gap: 6px; padding: 8px 12px; background: var(--bg-secondary); border: 1px solid var(--border-color); border-radius: 8px; min-height: 38px; align-items: center; }
.label-hint { font-weight: 400; color: var(--text-muted); font-size: 11px; }

/* 桌台状态标签 四态 */
.status-tag-enabled { display: inline-block; padding: 2px 8px; border-radius: 10px; font-size: 12px; font-weight: 500; background: rgba(16, 185, 129, 0.12); color: #10b981; }
.status-tag-disabled { display: inline-block; padding: 2px 8px; border-radius: 10px; font-size: 12px; font-weight: 500; background: rgba(156, 163, 175, 0.15); color: #9ca3af; }
.status-tag-pending { display: inline-block; padding: 2px 8px; border-radius: 10px; font-size: 12px; font-weight: 500; background: rgba(99, 102, 241, 0.12); color: #6366f1; }
.status-tag-unconfigured { display: inline-block; padding: 2px 8px; border-radius: 10px; font-size: 12px; font-weight: 500; background: rgba(245, 158, 11, 0.12); color: #f59e0b; }
.status-tag-disabled-sm { display: inline-block; padding: 1px 6px; border-radius: 8px; font-size: 11px; font-weight: 500; background: rgba(156, 163, 175, 0.15); color: #9ca3af; margin-left: 4px; }
.status-tag-pending-sm { display: inline-block; padding: 1px 6px; border-radius: 8px; font-size: 11px; font-weight: 500; background: rgba(99, 102, 241, 0.15); color: #6366f1; margin-left: 4px; }
.status-tag-disabled-inline { font-size: 10px; margin-left: 3px; opacity: 0.7; }
.status-tag-pending-inline { font-size: 10px; margin-left: 3px; padding: 1px 5px; border-radius: 6px; background: rgba(99, 102, 241, 0.18); color: #6366f1; }
.status-tag-unconfigured-inline { font-size: 10px; margin-left: 3px; padding: 1px 5px; border-radius: 6px; background: rgba(245, 158, 11, 0.18); color: #f59e0b; }
.multi-tag.table-disabled { opacity: 0.6; }

/* 统计页顶部"未接入维护"独立卡片 */
.filter-pending-card { display: inline-flex; align-items: center; gap: 4px; margin-left: 8px; padding: 4px 10px; border-radius: 8px; background: rgba(99, 102, 241, 0.12); color: #6366f1; font-size: 13px; }
.filter-pending-card strong { color: #6366f1; font-size: 16px; font-weight: 700; margin: 0 2px; }
</style>

<style>
/* 图片预览弹窗 - 非 scoped 样式，用于 Teleport 到 body 的元素 */
.image-preview-modal { position: fixed; top: 0; left: 0; right: 0; bottom: 0; background: rgba(0,0,0,0.95); z-index: 99999; display: flex; align-items: center; justify-content: center; }
.image-preview-modal img { max-width: 90%; max-height: 90%; object-fit: contain; border-radius: 8px; box-shadow: 0 10px 50px rgba(0,0,0,0.5); }
.image-preview-modal .preview-close { position: absolute; top: 20px; right: 20px; width: 44px; height: 44px; border: none; background: rgba(255,255,255,0.15); color: white; font-size: 28px; border-radius: 50%; cursor: pointer; transition: all 0.2s; display: flex; align-items: center; justify-content: center; }
.image-preview-modal .preview-close:hover { background: rgba(255,255,255,0.3); }
.image-preview-modal .preview-nav { position: absolute; top: 50%; transform: translateY(-50%); width: 50px; height: 50px; border: none; background: rgba(255,255,255,0.15); color: white; font-size: 24px; border-radius: 50%; cursor: pointer; transition: all 0.2s; }
.image-preview-modal .preview-nav:hover { background: rgba(255,255,255,0.3); }
.image-preview-modal .preview-nav.prev { left: 20px; }
.image-preview-modal .preview-nav.next { right: 20px; }
.image-preview-modal .preview-counter { position: absolute; bottom: 20px; left: 50%; transform: translateX(-50%); color: white; font-size: 14px; background: rgba(0,0,0,0.6); padding: 8px 20px; border-radius: 20px; }

/* 配置弹窗 V2 - 非 scoped 样式，用于 Teleport 到 body 的元素 */
.config-modal-v2 { 
  width: 600px; 
  max-width: 92vw; 
  border-radius: 16px; 
  overflow: hidden; 
  background: var(--bg-primary); 
  box-shadow: 0 20px 60px rgba(0,0,0,0.3);
}

.config-modal-header { 
  display: flex; 
  align-items: center; 
  justify-content: space-between; 
  padding: 20px 24px; 
  border-bottom: 1px solid var(--border-color); 
  background: var(--bg-primary);
}
.config-modal-header h2 { 
  margin: 0; 
  font-size: 18px; 
  font-weight: 600; 
  color: var(--text-primary); 
}
.config-close-btn { 
  width: 32px; 
  height: 32px; 
  display: flex; 
  align-items: center; 
  justify-content: center; 
  border: none; 
  background: transparent; 
  color: var(--text-muted); 
  cursor: pointer; 
  border-radius: 8px; 
  transition: all 0.2s; 
}
.config-close-btn:hover { 
  background: var(--bg-secondary); 
  color: var(--text-primary); 
}
.config-close-btn svg { 
  width: 20px; 
  height: 20px; 
}

.config-modal-subheader { 
  display: flex; 
  align-items: center; 
  justify-content: space-between; 
  padding: 16px 24px; 
  background: var(--bg-secondary); 
}
.config-count { 
  font-size: 14px; 
  color: #3a84ff; 
  font-weight: 500; 
}
.config-add-btn-v2 { 
  display: flex; 
  align-items: center; 
  gap: 6px; 
  padding: 10px 18px; 
  background: #3a84ff; 
  border: none; 
  border-radius: 8px; 
  color: white; 
  font-size: 14px; 
  font-weight: 500; 
  cursor: pointer; 
  transition: all 0.2s; 
}
.config-add-btn-v2:hover { 
  background: #2b6ed9; 
}
.config-add-btn-v2 svg { 
  width: 16px; 
  height: 16px; 
}

.config-modal-body { 
  padding: 16px 24px; 
  max-height: 400px; 
  overflow-y: auto; 
  background: var(--bg-primary);
}

.config-project-list { 
  display: flex; 
  flex-direction: column; 
  gap: 12px; 
}

.config-project-card { 
  display: flex; 
  align-items: center; 
  justify-content: space-between; 
  padding: 16px 20px; 
  background: var(--bg-secondary); 
  border: 1px solid var(--border-color); 
  border-radius: 12px; 
  transition: all 0.2s; 
}
.config-project-card:hover { 
  border-color: rgba(58, 132, 255, 0.3); 
  box-shadow: 0 2px 8px rgba(0,0,0,0.06); 
}
.config-project-card.disabled { 
  opacity: 0.6; 
}
.config-project-card.disabled .project-name { 
  color: var(--text-muted); 
}

.project-card-left { 
  flex: 1; 
  min-width: 0; 
}
.project-card-main { 
  display: flex; 
  align-items: center; 
  gap: 10px; 
  margin-bottom: 6px; 
  flex-wrap: wrap; 
}
.project-name { 
  font-size: 15px; 
  font-weight: 600; 
  color: var(--text-primary); 
}
.project-code { 
  font-size: 12px; 
  padding: 3px 10px; 
  background: var(--bg-primary); 
  border: 1px solid var(--border-color); 
  border-radius: 6px; 
  color: var(--text-secondary); 
  font-family: 'Consolas', 'Monaco', monospace; 
}
.project-desc { 
  font-size: 13px; 
  color: var(--text-secondary); 
  margin-bottom: 8px; 
  white-space: nowrap; 
  overflow: hidden; 
  text-overflow: ellipsis; 
}
.project-status { 
  display: flex; 
  align-items: center; 
}
.status-badge { 
  display: inline-flex; 
  align-items: center; 
  gap: 4px; 
  padding: 4px 12px; 
  border-radius: 20px; 
  font-size: 12px; 
  font-weight: 500; 
}
.status-badge.enabled { 
  background: rgba(34, 197, 94, 0.12); 
  color: #16a34a; 
}
.status-badge.disabled { 
  background: rgba(107, 114, 128, 0.12); 
  color: #6b7280; 
}

.project-card-actions { 
  display: flex; 
  align-items: center; 
  gap: 8px; 
  margin-left: 16px; 
  flex-shrink: 0; 
}
.project-action-btn { 
  width: 36px; 
  height: 36px; 
  display: flex; 
  align-items: center; 
  justify-content: center; 
  border: 1px solid var(--border-color); 
  background: var(--bg-primary); 
  border-radius: 8px; 
  cursor: pointer; 
  transition: all 0.2s; 
  color: var(--text-muted); 
}
.project-action-btn:hover { 
  border-color: #3a84ff; 
  color: #3a84ff; 
  background: rgba(58, 132, 255, 0.08); 
}
.project-action-btn.delete:hover { 
  border-color: #ef4444; 
  color: #ef4444; 
  background: rgba(239, 68, 68, 0.08); 
}
.project-action-btn svg { 
  width: 16px; 
  height: 16px; 
}

.config-empty-state { 
  display: flex; 
  flex-direction: column; 
  align-items: center; 
  justify-content: center; 
  padding: 48px 24px; 
  color: var(--text-muted); 
}
.config-empty-state svg { 
  width: 56px; 
  height: 56px; 
  opacity: 0.3; 
  margin-bottom: 16px; 
}
.config-empty-state p { 
  margin: 0 0 8px; 
  font-size: 15px; 
  font-weight: 500; 
  color: var(--text-secondary); 
}
.config-empty-state span { 
  font-size: 13px; 
  color: var(--text-muted); 
}

.config-modal-footer { 
  display: flex; 
  align-items: center; 
  justify-content: flex-end; 
  gap: 12px; 
  padding: 16px 24px; 
  border-top: 1px solid var(--border-color); 
  background: var(--bg-secondary); 
}
.config-modal-footer .btn-primary { 
  display: inline-flex; 
  align-items: center; 
  gap: 8px; 
}
.config-modal-footer .btn-primary svg { 
  width: 16px; 
  height: 16px; 
}

/* 项目表单弹窗 */
.project-form-modal { 
  width: 480px; 
  max-width: 90vw; 
}
.project-form-modal .form-group { 
  margin-bottom: 20px; 
}
.project-form-modal .form-label { 
  display: block; 
  margin-bottom: 8px; 
  font-size: 14px; 
  font-weight: 500; 
  color: var(--text-primary); 
}
.project-form-modal .form-label.required::after { 
  content: ' *'; 
  color: #ef4444; 
}
.project-form-modal .form-input { 
  width: 100%; 
  padding: 12px 16px; 
  border: 1px solid var(--border-color); 
  border-radius: 10px; 
  font-size: 14px; 
  background: var(--bg-secondary); 
  color: var(--text-primary); 
  transition: all 0.2s; 
  box-sizing: border-box; 
}
.project-form-modal .form-input:focus { 
  outline: none; 
  border-color: #3a84ff; 
  box-shadow: 0 0 0 3px rgba(58, 132, 255, 0.12); 
}
.project-form-modal .form-input::placeholder { 
  color: var(--text-muted); 
}

.toggle-switch-row { 
  display: flex; 
  align-items: center; 
  gap: 12px; 
}
.toggle-switch { 
  position: relative; 
  width: 48px; 
  height: 26px; 
  background: #d1d5db; 
  border: none; 
  border-radius: 26px; 
  cursor: pointer; 
  transition: all 0.25s ease; 
  padding: 0; 
}
.toggle-switch.active { 
  background: #3a84ff; 
}
.toggle-slider { 
  position: absolute; 
  top: 3px; 
  left: 3px; 
  width: 20px; 
  height: 20px; 
  background: white; 
  border-radius: 50%; 
  transition: all 0.25s ease; 
  box-shadow: 0 1px 3px rgba(0,0,0,0.2); 
}
.toggle-switch.active .toggle-slider { 
  left: 25px; 
}
.toggle-label { 
  font-size: 14px; 
  color: var(--text-secondary); 
}

/* 层级配置弹窗 */
.hierarchy-config-modal {
  width: 680px;
  max-width: 95vw;
  max-height: 85vh;
  display: flex;
  flex-direction: column;
}
.hierarchy-tabs {
  display: flex;
  gap: 4px;
  padding: 0 24px;
  background: var(--bg-secondary);
  border-bottom: 1px solid var(--border-color);
}
.hierarchy-tab {
  padding: 14px 20px;
  border: none;
  background: transparent;
  font-size: 14px;
  font-weight: 500;
  color: var(--text-secondary);
  cursor: pointer;
  border-bottom: 2px solid transparent;
  transition: all 0.2s;
  margin-bottom: -1px;
}
.hierarchy-tab:hover {
  color: var(--text-primary);
  background: var(--bg-hover);
}
.hierarchy-tab.active {
  color: #3a84ff;
  border-bottom-color: #3a84ff;
  background: var(--bg-card);
}
.hierarchy-toolbar {
  display: flex;
  justify-content: flex-end;
  padding: 16px 24px;
  border-bottom: 1px solid var(--border-color);
}
.hierarchy-list {
  flex: 1;
  overflow-y: auto;
  padding: 16px 24px;
  max-height: 400px;
}
.hierarchy-item {
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 14px 16px;
  background: var(--bg-secondary);
  border: 1px solid var(--border-color);
  border-radius: 10px;
  margin-bottom: 10px;
  transition: all 0.15s;
}
.hierarchy-item:hover {
  border-color: rgba(58, 132, 255, 0.3);
  box-shadow: 0 2px 8px rgba(0,0,0,0.06);
}
.hierarchy-name {
  flex: 1;
  font-size: 14px;
  font-weight: 500;
  color: var(--text-primary);
}
.hierarchy-badge {
  padding: 4px 10px;
  background: var(--bg-primary);
  border: 1px solid var(--border-color);
  border-radius: 6px;
  font-size: 12px;
  color: var(--text-secondary);
}
.hierarchy-badge.site-badge {
  background: rgba(34, 197, 94, 0.1);
  border-color: rgba(34, 197, 94, 0.2);
  color: #16a34a;
}
.hierarchy-actions {
  display: flex;
  gap: 6px;
}
.btn-icon {
  width: 32px;
  height: 32px;
  display: flex;
  align-items: center;
  justify-content: center;
  border: 1px solid var(--border-color);
  background: var(--bg-card);
  border-radius: 6px;
  cursor: pointer;
  color: var(--text-muted);
  transition: all 0.15s;
}
.btn-icon:hover {
  border-color: #3a84ff;
  color: #3a84ff;
  background: rgba(58, 132, 255, 0.08);
}
.btn-icon.danger:hover {
  border-color: #ef4444;
  color: #ef4444;
  background: rgba(239, 68, 68, 0.08);
}
.btn-icon svg {
  width: 15px;
  height: 15px;
}
.hierarchy-empty {
  text-align: center;
  padding: 48px 24px;
  color: var(--text-muted);
  font-size: 14px;
}

/* API Key 创建的记录在无权限时锁定 */
.row-locked {
  background: var(--bg-hover, rgba(0, 0, 0, 0.03)) !important;
  opacity: 0.75;
  cursor: not-allowed;
}
.row-locked > td {
  pointer-events: none;
}
.row-locked > td.action-cell {
  pointer-events: auto;  /* 操作列保留 hover/tooltip */
}
.row-locked > td .action-btns > .lock-icon {
  pointer-events: auto;
}
.lock-icon {
  display: inline-flex;
  align-items: center;
  font-size: 14px;
  margin-left: 4px;
  color: var(--warning, #f59e0b);
  cursor: help;
}
</style>
