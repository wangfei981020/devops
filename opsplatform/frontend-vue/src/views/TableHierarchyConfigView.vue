<script setup>
import { ref, computed, onMounted, onUnmounted } from 'vue'
import { useAppStore, useAuthStore } from '@/stores'
import { getTableHierarchy, saveTableHierarchy } from '@/api/tableHierarchy'

const appStore = useAppStore()
const authStore = useAuthStore()

// 翻译函数 - 包装以确保响应式
const t = (key, params) => appStore.t(key, params)

// 当前语言（用于触发响应式更新）
const currentLanguage = computed(() => appStore.language)

const isSuperAdmin = computed(() => authStore.isSuperAdmin())
const canCreate = computed(() => isSuperAdmin.value || authStore.hasPermission('table_hierarchy:create'))
const canUpdate = computed(() => isSuperAdmin.value || authStore.hasPermission('table_hierarchy:update'))
const canDelete = computed(() => isSuperAdmin.value || authStore.hasPermission('table_hierarchy:delete'))
const canManageProject = computed(() => isSuperAdmin.value || authStore.hasPermission('table_hierarchy:manage_project'))
const canManageSite = computed(() => isSuperAdmin.value || authStore.hasPermission('table_hierarchy:manage_site'))
const canManageGameType = computed(() => isSuperAdmin.value || authStore.hasPermission('table_hierarchy:manage_gametype'))
const canManageTable = computed(() => isSuperAdmin.value || authStore.hasPermission('table_hierarchy:manage_table'))

const loading = ref(false)
const searchQuery = ref('')

// 筛选条件输入
const filterInput = ref({ search: '', project: '', site: '', gameType: '', tableNo: '' })
// 已应用的筛选
const appliedFilter = ref({ search: '', project: '', site: '', gameType: '', tableNo: '' })

// 分页
const currentPage = ref(1)
const pageSize = ref(10)
const pageSizeOptions = [10, 20, 50, 100]

function onPageSizeChange(newSize) {
  pageSize.value = newSize
  currentPage.value = 1
}

// 层级数据
const projectOptions = ref([])
const siteOptions = ref([])
const tableOptions = ref([])
const gameTypeData = ref([]) // 游戏类型数据 { name_zh, name_en }

// 临时编辑副本
const tempProjects = ref([])
const tempSites = ref([])
const tempTables = ref([])
const tempGameTypes = ref([]) // 游戏类型临时副本

// 预定义的游戏类型（用于兼容旧数据）
const predefinedGameTypes = ['baccarat', 'dragonTiger', 'roulette', 'sicBo', 'other']
const predefinedGameTypeMap = {
  '百家乐': 'baccarat',
  '龙虎': 'dragonTiger',
  '轮盘': 'roulette',
  '骰宝': 'sicBo',
  '其他': 'other'
}

// 返回 { key, label } 对象数组，用于表单选择
const gameTypeOptions = computed(() => {
  return tempGameTypes.value.map(gt => ({
    key: getGameTypeKey(gt),
    label: getGameTypeName(gt)
  }))
})

// 获取游戏类型的 key（用于存储）
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
    if (predefinedGameTypes.includes(gt)) {
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

// 翻译游戏类型键为显示文本
function translateGameType(key) {
  // 先在自定义游戏类型中查找
  const customGt = tempGameTypes.value.find(gt => getGameTypeKey(gt) === key)
  if (customGt) {
    return getGameTypeName(customGt)
  }
  // 检查是否是预定义的键
  if (predefinedGameTypes.includes(key)) {
    return t(`tableHierarchy.gameTypes.${key}`)
  }
  // 检查是否是旧的中文值
  const mappedKey = predefinedGameTypeMap[key]
  if (mappedKey) {
    return t(`tableHierarchy.gameTypes.${mappedKey}`)
  }
  return key
}

// 表单弹窗
const showFormModal = ref(false)
const formMode = ref('project')
const editingIdx = ref(-1)
// 支持多语言名称：name_zh, name_en
const formData = ref({ name: '', name_zh: '', name_en: '', project: '', site: '', gameTypes: [], status: 'enabled' })

// 批量添加弹窗
const showBatchModal = ref(false)
const batchMode = ref('project')
const batchData = ref({ names: '', project: '', site: '', gameTypes: [], status: 'enabled' })

// 添加菜单下拉
const showAddMenu = ref(false)

// 管理弹窗
const showProjectManager = ref(false)
const showSiteManager = ref(false)
const showGameTypeManager = ref(false)
const showTableManager = ref(false)

// 弹窗分页
const managerPage = ref({ project: 1, site: 1, gameType: 1, table: 1 })
const managerPageSize = ref(10)
const managerPageSizeOptions = [10, 20, 50, 100]

function onManagerPageSizeChange(newSize) {
  managerPageSize.value = newSize
  managerPage.value = { project: 1, site: 1, gameType: 1, table: 1 }
}

function getManagerPaged(list, type) {
  const page = managerPage.value[type] || 1
  const size = managerPageSize.value
  const start = (page - 1) * size
  return list.slice(start, start + size)
}

function getManagerTotalPages(list) {
  return Math.ceil(list.length / managerPageSize.value) || 1
}

// 删除保护：检查是否有桌台关联
function isProjectDeletable(projectKey) {
  return tempTables.value.filter(t => t.project === projectKey).length === 0
}
function isSiteDeletable(siteKey, projectKey) {
  return tempTables.value.filter(t => t.site === siteKey && t.project === projectKey).length === 0
}
function isGameTypeDeletable(gameTypeKey) {
  return !tempTables.value.some(t => t.gameTypes && t.gameTypes.includes(gameTypeKey))
}

// 加载数据
async function loadData() {
  loading.value = true
  try {
    const config = await getTableHierarchy(true)
    projectOptions.value = config.projects || []
    siteOptions.value = config.sites || []
    tableOptions.value = config.tables || []
    gameTypeData.value = config.gameTypes || getDefaultGameTypes()
    resetTemp()
  } catch (e) {
    appStore.showToast(t('tableHierarchy.messages.loadFailed'), 'error')
  } finally {
    loading.value = false
  }
}

// 获取默认游戏类型（首次加载时）- 返回空数组，用户手动添加
function getDefaultGameTypes() {
  return []
}

function resetTemp() {
  tempProjects.value = [...projectOptions.value]
  tempSites.value = JSON.parse(JSON.stringify(siteOptions.value))
  tempTables.value = JSON.parse(JSON.stringify(tableOptions.value))
  tempGameTypes.value = JSON.parse(JSON.stringify(gameTypeData.value))
}

async function saveConfig() {
  return await saveConfigWithData(tempProjects.value, tempSites.value, tempTables.value, tempGameTypes.value)
}

// 保存指定数据到后端，返回是否成功
async function saveConfigWithData(projects, sites, tables, gameTypes) {
  loading.value = true
  try {
    const config = {
      projects: projects,
      sites: sites,
      tables: tables,
      gameTypes: gameTypes,
      halls: [],
      maintTypes: ['无', '紧急维护', '临时维护', '例行维护']
    }
    await saveTableHierarchy(config)
    projectOptions.value = [...projects]
    siteOptions.value = JSON.parse(JSON.stringify(sites))
    tableOptions.value = JSON.parse(JSON.stringify(tables))
    gameTypeData.value = JSON.parse(JSON.stringify(gameTypes))
    appStore.showToast(t('tableHierarchy.messages.saveSuccess'), 'success')
    return true
  } catch (e) {
    // 显示更详细的错误信息
    console.error('[TableHierarchy] saveConfigWithData 保存失败:', e)
    console.error('[TableHierarchy] 错误详情:', {
      status: e.response?.status,
      data: e.response?.data,
      message: e.message
    })
    if (e.response?.status === 403) {
      const debugInfo = e.response?.data?.debug
      let errorMsg = t('tableHierarchy.messages.noPermission') || '无权限保存桌台配置'
      if (debugInfo) {
        console.error('[权限调试]', debugInfo)
        errorMsg += ` (user: ${debugInfo.username || debugInfo.x_operator || 'unknown'})`
      }
      appStore.showToast(errorMsg, 'error')
    } else {
      const errMsg = e.response?.data?.error || e.response?.data?.message || e.message || t('tableHierarchy.messages.saveFailed')
      appStore.showToast(errMsg, 'error')
    }
    return false
  } finally {
    loading.value = false
  }
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

// 根据当前语言获取项目显示名称
function getProjectName(project) {
  if (!project) return ''
  if (typeof project === 'string') return project
  
  const lang = appStore.language
  if (lang === 'en-US') {
    return project.name_en || project.name_zh || project.name || ''
  } else {
    return project.name_zh || project.name || project.name_en || ''
  }
}

// 获取项目的内部标识（用于数据关联，优先用 name_zh）
function getProjectKey(project) {
  if (!project) return ''
  if (typeof project === 'string') return project
  return project.name_zh || project.name || ''
}

function getSitesByFormProject() {
  const projectKey = formData.value.project
  if (!projectKey) return []
  return tempSites.value.filter(s => s.project === projectKey)
}

function getSitesByBatchProject() {
  const projectKey = batchData.value.project
  if (!projectKey) return []
  return tempSites.value.filter(s => s.project === projectKey)
}

function getSiteCountByProject(projectKey) {
  return tempSites.value.filter(s => s.project === projectKey).length
}

function getTableCountByProject(projectKey) {
  return tempTables.value.filter(t => t.project === projectKey).length
}

function getTableCountBySite(projectKey, siteName) {
  return tempTables.value.filter(t => t.project === projectKey && t.site === siteName).length
}

function getSitesByProject(projectKey) {
  return tempSites.value.filter(s => s.project === projectKey)
}

function getTablesBySite(projectKey, siteName) {
  return tempTables.value.filter(t => t.project === projectKey && t.site === siteName)
}

// 应用筛选
function applyFilters() {
  appliedFilter.value = { ...filterInput.value }
  searchQuery.value = filterInput.value.search
  currentPage.value = 1
}

function resetFilters() {
  filterInput.value = { search: '', project: '', site: '', gameType: '', tableNo: '' }
  appliedFilter.value = { search: '', project: '', site: '', gameType: '', tableNo: '' }
  searchQuery.value = ''
  currentPage.value = 1
}

// 筛选下拉的现场选项（联动已选项目）
const filteredSiteOptions = computed(() => {
  const selectedProject = filterInput.value.project
  let sites = tempSites.value
  if (selectedProject) {
    sites = sites.filter(s => s.project === selectedProject)
  }
  const names = new Set()
  return sites.filter(s => {
    const name = getSiteName(s)
    if (names.has(name)) return false
    names.add(name)
    return true
  })
})

// 扁平化桌台列表
const flatTableList = computed(() => {
  return tempTables.value.map(table => {
    const projObj = tempProjects.value.find(p => getProjectKey(p) === table.project)
    const siteObj = tempSites.value.find(s => getSiteKey(s) === table.site && s.project === table.project)
    return {
      ...table,
      status: table.status || 'enabled', // 兼容旧数据，默认启用
      projectName: projObj ? getProjectName(projObj) : table.project || '-',
      projectKey: table.project || '',
      siteName: siteObj ? getSiteName(siteObj) : table.site || '-',
      siteKey: table.site || '',
      gameTypeLabels: (table.gameTypes || []).map(gt => translateGameType(gt))
    }
  })
})

// 筛选后的扁平桌台列表
const allFilteredTables = computed(() => {
  const f = appliedFilter.value
  const q = f.search?.toLowerCase() || ''
  const fProject = f.project
  const fSite = f.site
  const fGameType = f.gameType
  const fTableNo = f.tableNo?.toLowerCase() || ''

  return flatTableList.value.filter(row => {
    if (fProject && row.projectKey !== fProject) return false
    if (fSite && row.siteKey !== fSite) return false
    if (fGameType && !(row.gameTypes || []).includes(fGameType)) return false
    if (fTableNo && !row.name.toLowerCase().includes(fTableNo)) return false
    if (q) {
      const match = row.projectName.toLowerCase().includes(q) ||
        row.siteName.toLowerCase().includes(q) ||
        row.name.toLowerCase().includes(q) ||
        row.gameTypeLabels.some(label => label.toLowerCase().includes(q))
      if (!match) return false
    }
    return true
  })
})

// 分页
const totalPages = computed(() => Math.ceil(allFilteredTables.value.length / pageSize.value) || 1)
const pagedTables = computed(() => {
  const start = (currentPage.value - 1) * pageSize.value
  return allFilteredTables.value.slice(start, start + pageSize.value)
})

const stats = computed(() => {
  const uniqueSiteNames = new Set(tempSites.value.map(s => getSiteName(s)))
  return {
    projects: tempProjects.value.length,
    sites: uniqueSiteNames.size,
    gameTypes: tempGameTypes.value.length,
    tables: tempTables.value.length
  }
})

// 打开添加菜单
function toggleAddMenu() {
  showAddMenu.value = !showAddMenu.value
}

// 点击其他地方关闭菜单
function handleClickOutside(e) {
  if (showAddMenu.value && !e.target.closest('.add-dropdown')) {
    showAddMenu.value = false
  }
}

onMounted(() => {
  loadData()
  document.addEventListener('click', handleClickOutside)
})

onUnmounted(() => {
  document.removeEventListener('click', handleClickOutside)
})

// 打开单个添加弹窗
function openAddModal(mode, parentProject = '', parentSite = '') {
  showAddMenu.value = false
  formMode.value = mode
  editingIdx.value = -1
  const firstProjectKey = tempProjects.value[0] ? getProjectKey(tempProjects.value[0]) : ''
  formData.value = {
    name: '',
    name_zh: '',
    name_en: '',
    project: parentProject || firstProjectKey,
    site: parentSite || (tempSites.value[0] ? getSiteKey(tempSites.value[0]) : ''),
    gameTypes: [],
    status: 'enabled'
  }
  showFormModal.value = true
}

// 打开批量添加弹窗
function openBatchModal(mode) {
  showAddMenu.value = false
  batchMode.value = mode
  const firstProjectKey = tempProjects.value[0] ? getProjectKey(tempProjects.value[0]) : ''
  batchData.value = {
    names: '',
    project: firstProjectKey,
    site: tempSites.value[0] ? getSiteKey(tempSites.value[0]) : '',
    gameTypes: [],
    status: 'enabled'
  }
  showBatchModal.value = true
}

// 打开编辑弹窗
function openEditModal(mode, idx, item) {
  formMode.value = mode
  editingIdx.value = idx
  if (mode === 'project') {
    // 项目现在也支持中英文名称
    const projectNameZh = typeof item === 'string' ? item : (item.name_zh || item.name || '')
    const projectNameEn = typeof item === 'string' ? '' : (item.name_en || '')
    formData.value = { name: '', name_zh: projectNameZh, name_en: projectNameEn, project: '', site: '', gameTypes: [] }
  } else if (mode === 'site') {
    // 支持多语言名称
    formData.value = { 
      name: getSiteName(item), 
      name_zh: item.name_zh || item.name || '', 
      name_en: item.name_en || '', 
      project: item.project || '', 
      site: '', 
      gameTypes: [] 
    }
  } else if (mode === 'table') {
    formData.value = { name: item.name, name_zh: '', name_en: '', project: item.project || '', site: item.site, gameTypes: item.gameTypes || [], status: item.status || 'enabled' }
  }
  showFormModal.value = true
}

// 切换游戏类型选择
function toggleGameType(gameType) {
  const idx = formData.value.gameTypes.indexOf(gameType)
  if (idx >= 0) {
    formData.value.gameTypes.splice(idx, 1)
  } else {
    formData.value.gameTypes.push(gameType)
  }
}

function toggleBatchGameType(gameType) {
  const idx = batchData.value.gameTypes.indexOf(gameType)
  if (idx >= 0) {
    batchData.value.gameTypes.splice(idx, 1)
  } else {
    batchData.value.gameTypes.push(gameType)
  }
}

// 保存单个表单 - 先保存到后端，成功后才更新本地数据
async function saveForm() {
  const { name, project, site } = formData.value
  
  // 创建临时副本用于保存
  let newProjects = [...tempProjects.value]
  let newSites = JSON.parse(JSON.stringify(tempSites.value))
  let newTables = JSON.parse(JSON.stringify(tempTables.value))
  let newGameTypes = JSON.parse(JSON.stringify(tempGameTypes.value))
  
  // 游戏类型使用 name_zh 和 name_en，不需要 name 字段
  if (formMode.value === 'gameType') {
    const name_zh = formData.value.name_zh?.trim() || ''
    const name_en = formData.value.name_en?.trim() || ''
    if (!name_zh) {
      appStore.showToast(t('tableHierarchy.form.gameTypeNameZhRequired'), 'warning')
      return
    }
    const newItem = { name_zh, name_en }
    if (editingIdx.value >= 0) {
      const oldKey = getGameTypeKey(newGameTypes[editingIdx.value])
      newTables.forEach(table => {
        if (table.gameTypes?.includes(oldKey)) {
          const idx = table.gameTypes.indexOf(oldKey)
          table.gameTypes[idx] = name_zh
        }
      })
      newGameTypes[editingIdx.value] = newItem
    } else {
      const exists = newGameTypes.some(gt => getGameTypeKey(gt) === name_zh)
      if (exists) {
        appStore.showToast(t('common.error'), 'warning')
        return
      }
      newGameTypes.push(newItem)
    }
    
    // 先保存到后端
    const success = await saveConfigWithData(newProjects, newSites, newTables, newGameTypes)
    if (success) {
      tempProjects.value = newProjects
      tempSites.value = newSites
      tempTables.value = newTables
      tempGameTypes.value = newGameTypes
      showFormModal.value = false
    }
    return
  }
  
  // 对于 table 模式，需要检查 name 字段
  // 对于 site 和 project 模式，使用 name_zh 字段
  if (formMode.value === 'table' && !name.trim()) {
    appStore.showToast(t('common.error'), 'warning')
    return
  }

  if (formMode.value === 'project') {
    // 项目现在支持中英文名称
    const name_zh = formData.value.name_zh?.trim() || ''
    const name_en = formData.value.name_en?.trim() || ''
    if (!name_zh) {
      appStore.showToast(t('tableHierarchy.form.projectNameZhRequired'), 'warning')
      return
    }
    const newItem = { name_zh, name_en }
    if (editingIdx.value >= 0) {
      const oldKey = getProjectKey(newProjects[editingIdx.value])
      // 更新关联的现场和桌台
      newSites.forEach(s => {
        if (s.project === oldKey) s.project = name_zh
      })
      newTables.forEach(t => {
        if (t.project === oldKey) t.project = name_zh
      })
      newProjects[editingIdx.value] = newItem
    } else {
      const exists = newProjects.some(p => getProjectKey(p) === name_zh)
      if (exists) {
        appStore.showToast(t('common.error'), 'warning')
        return
      }
      newProjects.push(newItem)
    }
  } else if (formMode.value === 'site') {
    if (!project) {
      appStore.showToast(t('tableHierarchy.form.selectProject'), 'warning')
      return
    }
    const name_zh = formData.value.name_zh?.trim() || name.trim()
    const name_en = formData.value.name_en?.trim() || ''
    if (!name_zh) {
      appStore.showToast(t('tableHierarchy.form.siteNameZhRequired'), 'warning')
      return
    }
    const newItem = { name_zh, name_en, project }
    if (editingIdx.value >= 0) {
      const oldKey = getSiteKey(newSites[editingIdx.value])
      newTables.forEach(t => {
        if (t.site === oldKey) t.site = name_zh
      })
      newSites[editingIdx.value] = newItem
    } else {
      const exists = newSites.some(s => getSiteKey(s) === name_zh && s.project === project)
      if (exists) {
        appStore.showToast(t('common.error'), 'warning')
        return
      }
      newSites.push(newItem)
    }
  } else if (formMode.value === 'table') {
    if (!project) {
      appStore.showToast(t('tableHierarchy.form.selectProject'), 'warning')
      return
    }
    if (!site) {
      appStore.showToast(t('tableHierarchy.form.selectSite'), 'warning')
      return
    }
    const gameTypes = formData.value.gameTypes || []
    const status = formData.value.status || 'enabled'
    const newItem = { name: name.trim(), site, project, gameTypes, status }
    if (editingIdx.value >= 0) {
      newTables[editingIdx.value] = newItem
    } else {
      const exists = newTables.some(t => t.name === name.trim() && t.site === site && t.project === project)
      if (exists) {
        appStore.showToast(t('common.error'), 'warning')
        return
      }
      newTables.push(newItem)
    }
  }

  // 先保存到后端，成功后才更新本地数据
  const success = await saveConfigWithData(newProjects, newSites, newTables, newGameTypes)
  if (success) {
    tempProjects.value = newProjects
    tempSites.value = newSites
    tempTables.value = newTables
    tempGameTypes.value = newGameTypes
    showFormModal.value = false
  }
}

// 保存批量添加 - 先保存到后端，成功后才更新本地数据
async function saveBatch() {
  const { names, project, site } = batchData.value
  if (!names.trim()) {
    appStore.showToast(t('common.error'), 'warning')
    return
  }

  const nameList = names.split('\n').map(n => n.trim()).filter(n => n)
  if (!nameList.length) {
    appStore.showToast(t('common.error'), 'warning')
    return
  }

  // 创建临时副本
  let newProjects = [...tempProjects.value]
  let newSites = JSON.parse(JSON.stringify(tempSites.value))
  let newTables = JSON.parse(JSON.stringify(tempTables.value))
  let newGameTypes = JSON.parse(JSON.stringify(tempGameTypes.value))

  let addedCount = 0
  let skippedCount = 0

  if (batchMode.value === 'project') {
    // 批量添加项目，格式：中文名称|英文名称（每行一个，英文名称可选）
    for (const line of nameList) {
      const parts = line.split('|').map(p => p.trim())
      const name_zh = parts[0] || ''
      const name_en = parts[1] || ''
      if (!name_zh) continue
      const exists = newProjects.some(p => getProjectKey(p) === name_zh)
      if (!exists) {
        newProjects.push({ name_zh, name_en })
        addedCount++
      } else {
        skippedCount++
      }
    }
  } else if (batchMode.value === 'site') {
    if (!project) {
      appStore.showToast(t('tableHierarchy.form.selectProject'), 'warning')
      return
    }
    // 批量添加现场，格式：中文名称|英文名称（每行一个，英文名称可选）
    for (const line of nameList) {
      const parts = line.split('|').map(p => p.trim())
      const name_zh = parts[0] || ''
      const name_en = parts[1] || ''
      if (!name_zh) continue
      const exists = newSites.some(s => getSiteKey(s) === name_zh && s.project === project)
      if (!exists) {
        newSites.push({ name_zh, name_en, project })
        addedCount++
      } else {
        skippedCount++
      }
    }
  } else if (batchMode.value === 'gameType') {
    // 批量添加游戏类型，格式：中文名称|英文名称（每行一个，英文名称可选）
    for (const line of nameList) {
      const parts = line.split('|').map(p => p.trim())
      const name_zh = parts[0] || ''
      const name_en = parts[1] || ''
      if (!name_zh) continue
      const exists = newGameTypes.some(gt => getGameTypeKey(gt) === name_zh)
      if (!exists) {
        newGameTypes.push({ name_zh, name_en })
        addedCount++
      } else {
        skippedCount++
      }
    }
  } else if (batchMode.value === 'table') {
    if (!project) {
      appStore.showToast(t('tableHierarchy.form.selectProject'), 'warning')
      return
    }
    if (!site) {
      appStore.showToast(t('tableHierarchy.form.selectSite'), 'warning')
      return
    }
    const gameTypes = batchData.value.gameTypes || []
    for (const name of nameList) {
      const exists = newTables.some(t => t.name === name && t.site === site && t.project === project)
      if (!exists) {
        newTables.push({ name, site, project, gameTypes: [...gameTypes], status: batchData.value.status || 'enabled' })
        addedCount++
      } else {
        skippedCount++
      }
    }
  }

  // 先保存到后端，成功后才更新本地数据
  const success = await saveConfigWithData(newProjects, newSites, newTables, newGameTypes)
  if (success) {
    tempProjects.value = newProjects
    tempSites.value = newSites
    tempTables.value = newTables
    tempGameTypes.value = newGameTypes
    showBatchModal.value = false
  }
}

// 删除操作 - 先保存到后端，成功后才更新本地数据
async function confirmDelete(type, idx) {
  let name = ''
  let extraMsg = ''
  
  if (type === 'project') {
    name = tempProjects.value[idx]
    extraMsg = t('tableHierarchy.messages.cascadeWarningProject')
  } else if (type === 'site') {
    name = getSiteName(tempSites.value[idx])
    extraMsg = t('tableHierarchy.messages.cascadeWarningSite')
  } else if (type === 'table') {
    name = tempTables.value[idx].name
  }
  
  const confirmMsgKey = type === 'project' ? 'confirmDeleteProject' : type === 'site' ? 'confirmDeleteSite' : 'confirmDeleteTable'
  let message = t(`tableHierarchy.messages.${confirmMsgKey}`, { name })
  if (extraMsg) message += '\n' + extraMsg
  
  const confirmed = await appStore.showConfirm({
    type: 'warning',
    title: t('common.confirm'),
    message,
    okText: t('common.delete'),
    cancelText: t('common.cancel')
  })
  
  if (!confirmed) return
  
  // 创建临时副本
  let newProjects = [...tempProjects.value]
  let newSites = JSON.parse(JSON.stringify(tempSites.value))
  let newTables = JSON.parse(JSON.stringify(tempTables.value))
  let newGameTypes = JSON.parse(JSON.stringify(tempGameTypes.value))
  
  // 在副本上执行删除
  if (type === 'project') {
    const projectKey = getProjectKey(newProjects[idx])
    const projectSiteKeys = newSites.filter(s => s.project === projectKey).map(s => getSiteKey(s))
    newTables = newTables.filter(t => !projectSiteKeys.includes(t.site))
    newSites = newSites.filter(s => s.project !== projectKey)
    newProjects.splice(idx, 1)
  } else if (type === 'site') {
    const siteKey = getSiteKey(newSites[idx])
    newTables = newTables.filter(t => t.site !== siteKey)
    newSites.splice(idx, 1)
  } else if (type === 'table') {
    newTables.splice(idx, 1)
  }
  
  // 先保存到后端，成功后才更新本地数据
  const success = await saveConfigWithData(newProjects, newSites, newTables, newGameTypes)
  if (success) {
    tempProjects.value = newProjects
    tempSites.value = newSites
    tempTables.value = newTables
    tempGameTypes.value = newGameTypes
    appStore.showToast(t('tableHierarchy.messages.deleteSuccess'), 'success')
  }
}

function getSiteIndex(site) {
  return tempSites.value.findIndex(s => getSiteKey(s) === getSiteKey(site) && s.project === site.project)
}

function getTableIndex(table) {
  return tempTables.value.findIndex(t => t.name === table.name && t.site === table.site)
}

// 打开编辑游戏类型弹窗
function openEditGameTypeModal(idx, gt) {
  formMode.value = 'gameType'
  editingIdx.value = idx
  formData.value = {
    name: '',
    name_zh: gt.name_zh || '',
    name_en: gt.name_en || '',
    project: '',
    site: '',
    gameTypes: []
  }
  showFormModal.value = true
}

// 删除游戏类型确认
async function confirmDeleteGameType(idx) {
  const gt = tempGameTypes.value[idx]
  const name = getGameTypeName(gt)
  
  const confirmed = await appStore.showConfirm({
    title: t('common.confirm'),
    message: t('tableHierarchy.messages.confirmDeleteGameType', { name }),
    okText: t('common.delete'),
    cancelText: t('common.cancel')
  })
  
  if (!confirmed) return
  
  let newProjects = JSON.parse(JSON.stringify(tempProjects.value))
  let newSites = JSON.parse(JSON.stringify(tempSites.value))
  let newTables = JSON.parse(JSON.stringify(tempTables.value))
  let newGameTypes = JSON.parse(JSON.stringify(tempGameTypes.value))
  
  const gameTypeKey = getGameTypeKey(newGameTypes[idx])
  newTables.forEach(table => {
    if (table.gameTypes?.includes(gameTypeKey)) {
      table.gameTypes = table.gameTypes.filter(g => g !== gameTypeKey)
    }
  })
  newGameTypes.splice(idx, 1)
  
  const success = await saveConfigWithData(newProjects, newSites, newTables, newGameTypes)
  if (success) {
    tempProjects.value = newProjects
    tempSites.value = newSites
    tempTables.value = newTables
    tempGameTypes.value = newGameTypes
    appStore.showToast(t('tableHierarchy.messages.deleteSuccess'), 'success')
  }
}

const formTitle = computed(() => {
  if (formMode.value === 'project') {
    return editingIdx.value >= 0 ? t('tableHierarchy.form.editProject') : t('tableHierarchy.form.addProject')
  } else if (formMode.value === 'site') {
    return editingIdx.value >= 0 ? t('tableHierarchy.form.editSite') : t('tableHierarchy.form.addSite')
  } else if (formMode.value === 'gameType') {
    return editingIdx.value >= 0 ? t('tableHierarchy.form.editGameType') : t('tableHierarchy.form.addGameType')
  } else {
    return editingIdx.value >= 0 ? t('tableHierarchy.form.editTable') : t('tableHierarchy.form.addTable')
  }
})

const batchTitle = computed(() => {
  if (batchMode.value === 'project') {
    return t('tableHierarchy.form.batchAddProject')
  } else if (batchMode.value === 'site') {
    return t('tableHierarchy.form.batchAddSite')
  } else if (batchMode.value === 'gameType') {
    return t('tableHierarchy.form.batchAddGameType')
  } else {
    return t('tableHierarchy.form.batchAddTable')
  }
})

const batchPlaceholder = computed(() => {
  if (batchMode.value === 'gameType') {
    return t('tableHierarchy.form.batchGameTypePlaceholder')
  } else if (batchMode.value === 'site') {
    return t('tableHierarchy.form.batchSitePlaceholder')
  } else if (batchMode.value === 'project') {
    return t('tableHierarchy.form.batchProjectPlaceholder')
  } else {
    return t('tableHierarchy.form.batchTablePlaceholder')
  }
})
</script>

<template>
  <div class="hierarchy-config-page" :key="currentLanguage">
    <!-- 页面头部 -->
    <div class="page-header">
      <div class="header-left">
        <h2>{{ t('tableHierarchy.pageTitle') }}</h2>
        <span class="record-count">{{ t('tableHierarchy.recordCount', { count: allFilteredTables.length }) }}</span>
      </div>
      <div class="header-actions">
        <button v-if="canManageProject" class="btn btn-config project" @click="showProjectManager = true">
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M2 3h6a4 4 0 0 1 4 4v14a3 3 0 0 0-3-3H2z"/><path d="M22 3h-6a4 4 0 0 0-4 4v14a3 3 0 0 1 3-3h7z"/></svg>
          {{ t('tableHierarchy.configBtn.project') }}
          <span class="btn-badge">{{ stats.projects }}</span>
        </button>
        <button v-if="canManageSite" class="btn btn-config site" @click="showSiteManager = true">
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><rect x="3" y="3" width="18" height="18" rx="2"/><path d="M3 9h18"/><path d="M9 21V9"/></svg>
          {{ t('tableHierarchy.configBtn.site') }}
          <span class="btn-badge">{{ stats.sites }}</span>
        </button>
        <button v-if="canManageGameType" class="btn btn-config gametype" @click="showGameTypeManager = true">
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><circle cx="12" cy="12" r="10"/><path d="M8 14s1.5 2 4 2 4-2 4-2"/><line x1="9" y1="9" x2="9.01" y2="9"/><line x1="15" y1="9" x2="15.01" y2="9"/></svg>
          {{ t('tableHierarchy.configBtn.gameType') }}
          <span class="btn-badge">{{ stats.gameTypes }}</span>
        </button>
        <button v-if="canManageTable" class="btn btn-config table" @click="showTableManager = true">
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><rect x="3" y="4" width="18" height="12" rx="2"/><path d="M7 20h10"/><path d="M9 16v4"/><path d="M15 16v4"/></svg>
          {{ t('tableHierarchy.configBtn.table') }}
          <span class="btn-badge">{{ stats.tables }}</span>
        </button>
      </div>
    </div>

    <!-- 统计卡片 -->
    <div class="stats-cards">
      <div class="stat-card">
        <div class="stat-icon project-icon"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M2 3h6a4 4 0 0 1 4 4v14a3 3 0 0 0-3-3H2z"/><path d="M22 3h-6a4 4 0 0 0-4 4v14a3 3 0 0 1 3-3h7z"/></svg></div>
        <div class="stat-info"><span class="stat-label">{{ t('tableHierarchy.stats.projects') }}</span><span class="stat-value">{{ stats.projects }}</span></div>
      </div>
      <div class="stat-card">
        <div class="stat-icon site-icon"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><rect x="3" y="3" width="18" height="18" rx="2"/><path d="M3 9h18"/><path d="M9 21V9"/></svg></div>
        <div class="stat-info"><span class="stat-label">{{ t('tableHierarchy.stats.sites') }}</span><span class="stat-value">{{ stats.sites }}</span></div>
      </div>
      <div class="stat-card">
        <div class="stat-icon gametype-icon"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><circle cx="12" cy="12" r="10"/><path d="M8 14s1.5 2 4 2 4-2 4-2"/><line x1="9" y1="9" x2="9.01" y2="9"/><line x1="15" y1="9" x2="15.01" y2="9"/></svg></div>
        <div class="stat-info"><span class="stat-label">{{ t('tableHierarchy.gameTypeSection.title') }}</span><span class="stat-value">{{ stats.gameTypes }}</span></div>
      </div>
      <div class="stat-card">
        <div class="stat-icon table-icon"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><rect x="3" y="4" width="18" height="12" rx="2"/><path d="M7 20h10"/><path d="M9 16v4"/><path d="M15 16v4"/></svg></div>
        <div class="stat-info"><span class="stat-label">{{ t('tableHierarchy.stats.tables') }}</span><span class="stat-value">{{ stats.tables }}</span></div>
      </div>
    </div>

    <!-- 搜索和筛选 -->
    <div class="filter-container">
      <div class="search-card">
        <div class="search-main">
          <div class="search-input-wrapper">
            <svg class="search-icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><circle cx="11" cy="11" r="8"/><path d="m21 21-4.35-4.35"/></svg>
            <input type="text" v-model="filterInput.search" :placeholder="t('tableHierarchy.search.placeholder')" @keyup.enter="applyFilters">
          </div>
          <button class="btn-search" @click="applyFilters">
            <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><circle cx="11" cy="11" r="8"/><path d="m21 21-4.35-4.35"/></svg>
            {{ t('tableHierarchy.search.searchBtn') }}
          </button>
        </div>
      </div>
      <div class="filters-card">
        <div class="filters-header">
          <span class="filters-title">
            <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><polygon points="22 3 2 3 10 12.46 10 19 14 21 14 12.46 22 3"/></svg>
            {{ t('tableHierarchy.filters.title') }}
          </span>
          <button class="btn-reset" @click="resetFilters">{{ t('tableHierarchy.search.reset') }}</button>
        </div>
        <div class="filters-grid">
          <div class="filter-field">
            <label>{{ t('tableHierarchy.filters.project') }}</label>
            <select v-model="filterInput.project" @change="applyFilters">
              <option value="">{{ t('tableHierarchy.filters.allProjects') }}</option>
              <option v-for="p in tempProjects" :key="getProjectKey(p)" :value="getProjectKey(p)">{{ getProjectName(p) }}</option>
            </select>
          </div>
          <div class="filter-field">
            <label>{{ t('tableHierarchy.filters.site') }}</label>
            <select v-model="filterInput.site" @change="applyFilters">
              <option value="">{{ t('tableHierarchy.filters.allSites') }}</option>
              <option v-for="s in filteredSiteOptions" :key="getSiteKey(s)" :value="getSiteKey(s)">{{ getSiteName(s) }}</option>
            </select>
          </div>
          <div class="filter-field">
            <label>{{ t('tableHierarchy.filters.gameType') }}</label>
            <select v-model="filterInput.gameType" @change="applyFilters">
              <option value="">{{ t('tableHierarchy.filters.allGameTypes') }}</option>
              <option v-for="gt in gameTypeOptions" :key="gt.key" :value="gt.key">{{ gt.label }}</option>
            </select>
          </div>
          <div class="filter-field">
            <label>{{ t('tableHierarchy.filters.tableNo') }}</label>
            <input type="text" v-model="filterInput.tableNo" :placeholder="t('tableHierarchy.filters.tableNoPlaceholder')" @keyup.enter="applyFilters">
          </div>
        </div>
      </div>
    </div>

    <!-- 管理弹窗移至 Teleport 区域 -->

    <!-- 桌台列表（扁平表格） -->
    <div class="table-wrapper" v-if="!loading">
      <div class="table-container">
        <table class="data-table">
          <thead>
            <tr>
              <th style="width: 60px;">#</th>
              <th style="width: 150px;">{{ t('tableHierarchy.columns.project') }}</th>
              <th style="width: 150px;">{{ t('tableHierarchy.columns.site') }}</th>
              <th style="width: 120px;">{{ t('tableHierarchy.columns.table') }}</th>
              <th style="width: 200px;">{{ t('tableHierarchy.columns.gameType') }}</th>
              <th style="width: 100px;">{{ t('tableHierarchy.columns.status') }}</th>
              <th style="width: 120px;" class="sticky-col">{{ t('tableHierarchy.columns.actions') }}</th>
            </tr>
          </thead>
          <tbody>
            <template v-if="pagedTables.length">
              <tr v-for="(row, idx) in pagedTables" :key="row.name + '-' + row.siteKey + '-' + row.projectKey">
                <td class="row-number">{{ (currentPage - 1) * pageSize + idx + 1 }}</td>
                <td><div class="cell-content"><div class="type-badge project">{{ t('tableHierarchy.types.project') }}</div><span>{{ row.projectName }}</span></div></td>
                <td><div class="cell-content"><div class="type-badge site">{{ t('tableHierarchy.types.site') }}</div><span>{{ row.siteName }}</span></div></td>
                <td><span class="table-number">{{ row.name }}</span></td>
                <td>
                  <div class="game-type-tags" v-if="row.gameTypes?.length">
                    <span v-for="gt in row.gameTypes" :key="gt" class="game-type-tag">{{ translateGameType(gt) }}</span>
                  </div>
                  <span v-else class="cell-muted">-</span>
                </td>
                <td>
                  <span :class="row.status === 'enabled' ? 'status-tag-on' : 'status-tag-off'">{{ row.status === 'enabled' ? t('tableHierarchy.status.enabled') : t('tableHierarchy.status.disabled') }}</span>
                </td>
                <td class="sticky-col">
                  <div class="action-btns">
                    <button v-if="canUpdate" class="action-btn" @click="openEditModal('table', getTableIndex(row), row)" :title="t('tableHierarchy.actions.edit')"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M11 4H4a2 2 0 00-2 2v14a2 2 0 002 2h14a2 2 0 002-2v-7"/><path d="M18.5 2.5a2.121 2.121 0 013 3L12 15l-4 1 1-4 9.5-9.5z"/></svg></button>
                    <button v-if="canDelete" class="action-btn danger" @click="confirmDelete('table', getTableIndex(row))" :title="t('tableHierarchy.actions.delete')"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><polyline points="3 6 5 6 21 6"/><path d="M19 6v14a2 2 0 01-2 2H7a2 2 0 01-2-2V6m3 0V4a2 2 0 012-2h4a2 2 0 012 2v2"/></svg></button>
                  </div>
                </td>
              </tr>
            </template>
            <tr v-else>
              <td colspan="7" class="empty-cell">
                <div class="empty-state">
                  <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5"><rect x="3" y="4" width="18" height="12" rx="2"/><path d="M7 20h10"/><path d="M9 16v4"/><path d="M15 16v4"/></svg>
                  <p>{{ searchQuery ? t('tableHierarchy.empty.noMatch') : t('tableHierarchy.empty.noData') }}</p>
                  <span>{{ searchQuery ? t('tableHierarchy.empty.noMatchHint') : t('tableHierarchy.empty.noDataHint') }}</span>
                </div>
              </td>
            </tr>
          </tbody>
        </table>
      </div>
    </div>

    <!-- 分页 -->
    <div class="pagination" v-if="!loading && allFilteredTables.length > 0">
      <div class="pagination-info">{{ t('tableHierarchy.pagination.total') }} <strong>{{ allFilteredTables.length }}</strong> {{ t('tableHierarchy.pagination.records') }}</div>
      <div class="pagination-size">
        <span>{{ t('tableHierarchy.pagination.perPage') }}</span>
        <select :value="pageSize" @change="onPageSizeChange(Number($event.target.value))" class="page-size-select">
          <option v-for="size in pageSizeOptions" :key="size" :value="size">{{ size }}</option>
        </select>
        <span>{{ t('tableHierarchy.pagination.items') }}</span>
      </div>
      <div class="pagination-controls">
        <button class="page-btn" @click="currentPage = 1" :disabled="currentPage <= 1"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><polyline points="11 17 6 12 11 7"/><polyline points="18 17 13 12 18 7"/></svg></button>
        <button class="page-btn" @click="currentPage--" :disabled="currentPage <= 1"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><polyline points="15 18 9 12 15 6"/></svg></button>
        <span class="page-indicator">{{ currentPage }} / {{ totalPages }}</span>
        <button class="page-btn" @click="currentPage++" :disabled="currentPage >= totalPages"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><polyline points="9 18 15 12 9 6"/></svg></button>
        <button class="page-btn" @click="currentPage = totalPages" :disabled="currentPage >= totalPages"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><polyline points="13 17 18 12 13 7"/><polyline points="6 17 11 12 6 7"/></svg></button>
      </div>
    </div>

    <div v-if="loading" class="loading-state">
      <div class="spinner"></div>
      <span>{{ t('tableHierarchy.loading') }}</span>
    </div>

    <!-- 项目管理弹窗 -->
    <Teleport to="body">
      <div class="modal-overlay" :class="{ active: showProjectManager }">
        <div class="modal manager-modal">
          <div class="modal-header">
            <div class="modal-title">{{ t('tableHierarchy.projectSection.title') }} <span class="modal-count">{{ tempProjects.length }}</span></div>
            <button class="modal-close" @click="showProjectManager = false"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><line x1="18" y1="6" x2="6" y2="18"/><line x1="6" y1="6" x2="18" y2="18"/></svg></button>
          </div>
          <div class="modal-body manager-body">
            <div class="manager-toolbar">
              <button v-if="canCreate" class="btn btn-primary btn-sm" @click="showProjectManager = false; openAddModal('project')"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M12 5v14M5 12h14"/></svg>{{ t('tableHierarchy.add.addProject') }}</button>
              <button v-if="canCreate" class="btn btn-secondary btn-sm" @click="showProjectManager = false; openBatchModal('project')">{{ t('tableHierarchy.add.batchProject') }}</button>
            </div>
            <div class="manager-table-scroll">
              <table class="data-table">
                <thead><tr><th style="width:50px">#</th><th>{{ t('tableHierarchy.form.projectNameZh') }}</th><th>{{ t('tableHierarchy.form.projectNameEn') }}</th><th style="width:80px">{{ t('tableHierarchy.columns.siteCount') }}</th><th style="width:80px">{{ t('tableHierarchy.columns.tableCount') }}</th><th style="width:90px">{{ t('tableHierarchy.columns.actions') }}</th></tr></thead>
                <tbody>
                  <tr v-for="(proj, localIdx) in getManagerPaged(tempProjects, 'project')" :key="getProjectKey(proj)">
                    <td class="row-number">{{ (managerPage.project - 1) * managerPageSize + localIdx + 1 }}</td>
                    <td>{{ typeof proj === 'string' ? proj : (proj.name_zh || '-') }}</td>
                    <td>{{ typeof proj === 'string' ? '-' : (proj.name_en || '-') }}</td>
                    <td><span class="count-badge site-count">{{ getSiteCountByProject(getProjectKey(proj)) }}</span></td>
                    <td><span class="count-badge table-count">{{ getTableCountByProject(getProjectKey(proj)) }}</span></td>
                    <td>
                      <div class="action-btns">
                        <button v-if="canUpdate" class="action-btn" @click="showProjectManager = false; openEditModal('project', (managerPage.project - 1) * managerPageSize + localIdx, proj)"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M11 4H4a2 2 0 00-2 2v14a2 2 0 002 2h14a2 2 0 002-2v-7"/><path d="M18.5 2.5a2.121 2.121 0 013 3L12 15l-4 1 1-4 9.5-9.5z"/></svg></button>
                        <button v-if="canDelete" class="action-btn danger" :disabled="!isProjectDeletable(getProjectKey(proj))" :title="isProjectDeletable(getProjectKey(proj)) ? t('tableHierarchy.actions.delete') : t('tableHierarchy.messages.hasTablesCannotDelete')" @click="isProjectDeletable(getProjectKey(proj)) && confirmDelete('project', (managerPage.project - 1) * managerPageSize + localIdx)"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><polyline points="3 6 5 6 21 6"/><path d="M19 6v14a2 2 0 01-2 2H7a2 2 0 01-2-2V6m3 0V4a2 2 0 012-2h4a2 2 0 012 2v2"/></svg></button>
                      </div>
                    </td>
                  </tr>
                  <tr v-if="tempProjects.length === 0"><td colspan="6" class="empty">{{ t('tableHierarchy.projectSection.empty') }}</td></tr>
                </tbody>
              </table>
            </div>
            <div class="manager-pagination">
              <div class="mp-left">
                <span class="mp-info">{{ t('tableHierarchy.pagination.total') }} <strong>{{ tempProjects.length }}</strong> {{ t('tableHierarchy.pagination.records') }}</span>
                <select class="mp-size-select" :value="managerPageSize" @change="onManagerPageSizeChange(Number($event.target.value))">
                  <option v-for="s in managerPageSizeOptions" :key="s" :value="s">{{ s }} {{ t('tableHierarchy.pagination.items') }}</option>
                </select>
              </div>
              <div class="mp-right">
                <span class="mp-page">{{ managerPage.project }} / {{ getManagerTotalPages(tempProjects) }}</span>
                <button class="page-btn" :disabled="managerPage.project <= 1" @click="managerPage.project--"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><polyline points="15 18 9 12 15 6"/></svg></button>
                <button class="page-btn" :disabled="managerPage.project >= getManagerTotalPages(tempProjects)" @click="managerPage.project++"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><polyline points="9 18 15 12 9 6"/></svg></button>
              </div>
            </div>
          </div>
        </div>
      </div>
    </Teleport>

    <!-- 现场管理弹窗 -->
    <Teleport to="body">
      <div class="modal-overlay" :class="{ active: showSiteManager }">
        <div class="modal manager-modal">
          <div class="modal-header">
            <div class="modal-title">{{ t('tableHierarchy.siteSection.title') }} <span class="modal-count">{{ tempSites.length }}</span></div>
            <button class="modal-close" @click="showSiteManager = false"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><line x1="18" y1="6" x2="6" y2="18"/><line x1="6" y1="6" x2="18" y2="18"/></svg></button>
          </div>
          <div class="modal-body manager-body">
            <div class="manager-toolbar">
              <button v-if="canCreate" class="btn btn-primary btn-sm" @click="showSiteManager = false; openAddModal('site')"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M12 5v14M5 12h14"/></svg>{{ t('tableHierarchy.add.addSite') }}</button>
              <button v-if="canCreate" class="btn btn-secondary btn-sm" @click="showSiteManager = false; openBatchModal('site')">{{ t('tableHierarchy.add.batchSite') }}</button>
            </div>
            <div class="manager-table-scroll">
              <table class="data-table">
                <thead><tr><th style="width:50px">#</th><th>{{ t('tableHierarchy.form.siteNameZh') }}</th><th>{{ t('tableHierarchy.form.siteNameEn') }}</th><th>{{ t('tableHierarchy.form.belongProject') }}</th><th style="width:80px">{{ t('tableHierarchy.columns.tableCount') }}</th><th style="width:90px">{{ t('tableHierarchy.columns.actions') }}</th></tr></thead>
                <tbody>
                  <tr v-for="(site, localIdx) in getManagerPaged(tempSites, 'site')" :key="getSiteKey(site) + '-' + site.project">
                    <td class="row-number">{{ (managerPage.site - 1) * managerPageSize + localIdx + 1 }}</td>
                    <td>{{ site.name_zh || '-' }}</td>
                    <td>{{ site.name_en || '-' }}</td>
                    <td><span class="project-ref-tag">{{ getProjectName(tempProjects.find(p => getProjectKey(p) === site.project) || site.project) }}</span></td>
                    <td><span class="count-badge table-count">{{ getTableCountBySite(site.project, getSiteKey(site)) }}</span></td>
                    <td>
                      <div class="action-btns">
                        <button v-if="canUpdate" class="action-btn" @click="showSiteManager = false; openEditModal('site', (managerPage.site - 1) * managerPageSize + localIdx, site)"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M11 4H4a2 2 0 00-2 2v14a2 2 0 002 2h14a2 2 0 002-2v-7"/><path d="M18.5 2.5a2.121 2.121 0 013 3L12 15l-4 1 1-4 9.5-9.5z"/></svg></button>
                        <button v-if="canDelete" class="action-btn danger" :disabled="!isSiteDeletable(getSiteKey(site), site.project)" :title="isSiteDeletable(getSiteKey(site), site.project) ? t('tableHierarchy.actions.delete') : t('tableHierarchy.messages.hasTablesCannotDelete')" @click="isSiteDeletable(getSiteKey(site), site.project) && confirmDelete('site', (managerPage.site - 1) * managerPageSize + localIdx)"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><polyline points="3 6 5 6 21 6"/><path d="M19 6v14a2 2 0 01-2 2H7a2 2 0 01-2-2V6m3 0V4a2 2 0 012-2h4a2 2 0 012 2v2"/></svg></button>
                      </div>
                    </td>
                  </tr>
                  <tr v-if="tempSites.length === 0"><td colspan="6" class="empty">{{ t('tableHierarchy.siteSection.empty') }}</td></tr>
                </tbody>
              </table>
            </div>
            <div class="manager-pagination">
              <div class="mp-left">
                <span class="mp-info">{{ t('tableHierarchy.pagination.total') }} <strong>{{ tempSites.length }}</strong> {{ t('tableHierarchy.pagination.records') }}</span>
                <select class="mp-size-select" :value="managerPageSize" @change="onManagerPageSizeChange(Number($event.target.value))">
                  <option v-for="s in managerPageSizeOptions" :key="s" :value="s">{{ s }} {{ t('tableHierarchy.pagination.items') }}</option>
                </select>
              </div>
              <div class="mp-right">
                <span class="mp-page">{{ managerPage.site }} / {{ getManagerTotalPages(tempSites) }}</span>
                <button class="page-btn" :disabled="managerPage.site <= 1" @click="managerPage.site--"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><polyline points="15 18 9 12 15 6"/></svg></button>
                <button class="page-btn" :disabled="managerPage.site >= getManagerTotalPages(tempSites)" @click="managerPage.site++"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><polyline points="9 18 15 12 9 6"/></svg></button>
              </div>
            </div>
          </div>
        </div>
      </div>
    </Teleport>

    <!-- 游戏类型管理弹窗 -->
    <Teleport to="body">
      <div class="modal-overlay" :class="{ active: showGameTypeManager }">
        <div class="modal manager-modal">
          <div class="modal-header">
            <div class="modal-title">{{ t('tableHierarchy.gameTypeSection.title') }} <span class="modal-count">{{ tempGameTypes.length }}</span></div>
            <button class="modal-close" @click="showGameTypeManager = false"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><line x1="18" y1="6" x2="6" y2="18"/><line x1="6" y1="6" x2="18" y2="18"/></svg></button>
          </div>
          <div class="modal-body manager-body">
            <div class="manager-toolbar">
              <button v-if="canCreate" class="btn btn-primary btn-sm" @click="showGameTypeManager = false; openAddModal('gameType')"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M12 5v14M5 12h14"/></svg>{{ t('tableHierarchy.add.addGameType') }}</button>
              <button v-if="canCreate" class="btn btn-secondary btn-sm" @click="showGameTypeManager = false; openBatchModal('gameType')">{{ t('tableHierarchy.add.batchGameType') }}</button>
            </div>
            <div class="manager-table-scroll">
              <table class="data-table">
                <thead><tr><th style="width:50px">#</th><th>{{ t('tableHierarchy.form.gameTypeNameZh') }}</th><th>{{ t('tableHierarchy.form.gameTypeNameEn') }}</th><th style="width:90px">{{ t('tableHierarchy.columns.actions') }}</th></tr></thead>
                <tbody>
                  <tr v-for="(gt, localIdx) in getManagerPaged(tempGameTypes, 'gameType')" :key="getGameTypeKey(gt)">
                    <td class="row-number">{{ (managerPage.gameType - 1) * managerPageSize + localIdx + 1 }}</td>
                    <td>{{ gt.name_zh || '-' }}</td>
                    <td>{{ gt.name_en || '-' }}</td>
                    <td>
                      <div class="action-btns">
                        <button v-if="canUpdate" class="action-btn" @click="showGameTypeManager = false; openEditGameTypeModal((managerPage.gameType - 1) * managerPageSize + localIdx, gt)"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M11 4H4a2 2 0 00-2 2v14a2 2 0 002 2h14a2 2 0 002-2v-7"/><path d="M18.5 2.5a2.121 2.121 0 013 3L12 15l-4 1 1-4 9.5-9.5z"/></svg></button>
                        <button v-if="canDelete" class="action-btn danger" :disabled="!isGameTypeDeletable(getGameTypeKey(gt))" :title="isGameTypeDeletable(getGameTypeKey(gt)) ? t('tableHierarchy.actions.delete') : t('tableHierarchy.messages.hasTablesCannotDelete')" @click="isGameTypeDeletable(getGameTypeKey(gt)) && confirmDeleteGameType((managerPage.gameType - 1) * managerPageSize + localIdx)"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><polyline points="3 6 5 6 21 6"/><path d="M19 6v14a2 2 0 01-2 2H7a2 2 0 01-2-2V6m3 0V4a2 2 0 012-2h4a2 2 0 012 2v2"/></svg></button>
                      </div>
                    </td>
                  </tr>
                  <tr v-if="tempGameTypes.length === 0"><td colspan="4" class="empty">{{ t('tableHierarchy.gameTypeSection.empty') }}</td></tr>
                </tbody>
              </table>
            </div>
            <div class="manager-pagination">
              <div class="mp-left">
                <span class="mp-info">{{ t('tableHierarchy.pagination.total') }} <strong>{{ tempGameTypes.length }}</strong> {{ t('tableHierarchy.pagination.records') }}</span>
                <select class="mp-size-select" :value="managerPageSize" @change="onManagerPageSizeChange(Number($event.target.value))">
                  <option v-for="s in managerPageSizeOptions" :key="s" :value="s">{{ s }} {{ t('tableHierarchy.pagination.items') }}</option>
                </select>
              </div>
              <div class="mp-right">
                <span class="mp-page">{{ managerPage.gameType }} / {{ getManagerTotalPages(tempGameTypes) }}</span>
                <button class="page-btn" :disabled="managerPage.gameType <= 1" @click="managerPage.gameType--"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><polyline points="15 18 9 12 15 6"/></svg></button>
                <button class="page-btn" :disabled="managerPage.gameType >= getManagerTotalPages(tempGameTypes)" @click="managerPage.gameType++"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><polyline points="9 18 15 12 9 6"/></svg></button>
              </div>
            </div>
          </div>
        </div>
      </div>
    </Teleport>

    <!-- 桌台管理弹窗 -->
    <Teleport to="body">
      <div class="modal-overlay" :class="{ active: showTableManager }">
        <div class="modal manager-modal manager-modal-wide">
          <div class="modal-header">
            <div class="modal-title">{{ t('tableHierarchy.configBtn.table') }} <span class="modal-count">{{ tempTables.length }}</span></div>
            <button class="modal-close" @click="showTableManager = false"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><line x1="18" y1="6" x2="6" y2="18"/><line x1="6" y1="6" x2="18" y2="18"/></svg></button>
          </div>
          <div class="modal-body manager-body">
            <div class="manager-toolbar">
              <button v-if="canCreate" class="btn btn-primary btn-sm" @click="showTableManager = false; openAddModal('table')"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M12 5v14M5 12h14"/></svg>{{ t('tableHierarchy.add.addTable') }}</button>
              <button v-if="canCreate" class="btn btn-secondary btn-sm" @click="showTableManager = false; openBatchModal('table')">{{ t('tableHierarchy.add.batchTable') }}</button>
            </div>
            <div class="manager-table-scroll">
              <table class="data-table">
                <thead><tr><th style="width:50px">#</th><th>{{ t('tableHierarchy.columns.project') }}</th><th>{{ t('tableHierarchy.columns.site') }}</th><th>{{ t('tableHierarchy.columns.table') }}</th><th>{{ t('tableHierarchy.columns.gameType') }}</th><th style="width:90px">{{ t('tableHierarchy.columns.actions') }}</th></tr></thead>
                <tbody>
                  <tr v-for="(table, localIdx) in getManagerPaged(tempTables, 'table')" :key="table.name + '-' + table.site + '-' + table.project">
                    <td class="row-number">{{ (managerPage.table - 1) * managerPageSize + localIdx + 1 }}</td>
                    <td>{{ getProjectName(tempProjects.find(p => getProjectKey(p) === table.project) || table.project) }}</td>
                    <td>{{ getSiteName(tempSites.find(s => getSiteKey(s) === table.site && s.project === table.project) || table.site) }}</td>
                    <td><span class="table-number">{{ table.name }}</span></td>
                    <td>
                      <div class="game-type-tags" v-if="table.gameTypes?.length"><span v-for="gt in table.gameTypes" :key="gt" class="game-type-tag">{{ translateGameType(gt) }}</span></div>
                      <span v-else class="cell-muted">-</span>
                    </td>
                    <td>
                      <div class="action-btns">
                        <button v-if="canUpdate" class="action-btn" @click="showTableManager = false; openEditModal('table', (managerPage.table - 1) * managerPageSize + localIdx, table)"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M11 4H4a2 2 0 00-2 2v14a2 2 0 002 2h14a2 2 0 002-2v-7"/><path d="M18.5 2.5a2.121 2.121 0 013 3L12 15l-4 1 1-4 9.5-9.5z"/></svg></button>
                        <button v-if="canDelete" class="action-btn danger" @click="confirmDelete('table', (managerPage.table - 1) * managerPageSize + localIdx)"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><polyline points="3 6 5 6 21 6"/><path d="M19 6v14a2 2 0 01-2 2H7a2 2 0 01-2-2V6m3 0V4a2 2 0 012-2h4a2 2 0 012 2v2"/></svg></button>
                      </div>
                    </td>
                  </tr>
                  <tr v-if="tempTables.length === 0"><td colspan="6" class="empty">{{ t('tableHierarchy.empty.noData') }}</td></tr>
                </tbody>
              </table>
            </div>
            <div class="manager-pagination">
              <div class="mp-left">
                <span class="mp-info">{{ t('tableHierarchy.pagination.total') }} <strong>{{ tempTables.length }}</strong> {{ t('tableHierarchy.pagination.records') }}</span>
                <select class="mp-size-select" :value="managerPageSize" @change="onManagerPageSizeChange(Number($event.target.value))">
                  <option v-for="s in managerPageSizeOptions" :key="s" :value="s">{{ s }} {{ t('tableHierarchy.pagination.items') }}</option>
                </select>
              </div>
              <div class="mp-right">
                <span class="mp-page">{{ managerPage.table }} / {{ getManagerTotalPages(tempTables) }}</span>
                <button class="page-btn" :disabled="managerPage.table <= 1" @click="managerPage.table--"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><polyline points="15 18 9 12 15 6"/></svg></button>
                <button class="page-btn" :disabled="managerPage.table >= getManagerTotalPages(tempTables)" @click="managerPage.table++"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><polyline points="9 18 15 12 9 6"/></svg></button>
              </div>
            </div>
          </div>
        </div>
      </div>
    </Teleport>

    <!-- 单个添加/编辑弹窗 -->
    <Teleport to="body">
      <div class="modal-overlay" :class="{ active: showFormModal }">
        <div class="modal form-modal">
          <div class="modal-header">
            <div class="modal-title">{{ formTitle }}</div>
            <button class="modal-close" @click="showFormModal = false">
              <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><line x1="18" y1="6" x2="6" y2="18"/><line x1="6" y1="6" x2="18" y2="18"/></svg>
            </button>
          </div>
          <div class="modal-body">
            <div class="form-group" v-if="formMode === 'site' || formMode === 'table'">
              <label class="form-label required">{{ t('tableHierarchy.form.belongProject') }}</label>
              <select v-model="formData.project" class="form-input" @change="formData.site = ''">
                <option value="">{{ t('tableHierarchy.form.selectProject') }}</option>
                <option v-for="p in tempProjects" :key="getProjectKey(p)" :value="getProjectKey(p)">{{ getProjectName(p) }}</option>
              </select>
            </div>
            <div class="form-group" v-if="formMode === 'table'">
              <label class="form-label required">{{ t('tableHierarchy.form.belongSite') }}</label>
              <select v-model="formData.site" class="form-input">
                <option value="">{{ t('tableHierarchy.form.selectSite') }}</option>
                <option v-for="s in getSitesByFormProject()" :key="getSiteKey(s)" :value="getSiteKey(s)">{{ getSiteName(s) }}</option>
              </select>
            </div>
            <!-- 游戏类型中英文名称 -->
            <div class="form-group" v-if="formMode === 'gameType'">
              <label class="form-label required">{{ t('tableHierarchy.form.gameTypeNameZh') }}</label>
              <input type="text" v-model="formData.name_zh" :placeholder="t('tableHierarchy.form.gameTypeNameZhPlaceholder')" class="form-input">
            </div>
            <div class="form-group" v-if="formMode === 'gameType'">
              <label class="form-label">{{ t('tableHierarchy.form.gameTypeNameEn') }} <span class="label-hint">{{ t('tableHierarchy.form.optional') }}</span></label>
              <input type="text" v-model="formData.name_en" :placeholder="t('tableHierarchy.form.gameTypeNameEnPlaceholder')" class="form-input">
            </div>
            <!-- 项目中英文名称 -->
            <div class="form-group" v-if="formMode === 'project'">
              <label class="form-label required">{{ t('tableHierarchy.form.projectNameZh') }}</label>
              <input type="text" v-model="formData.name_zh" :placeholder="t('tableHierarchy.form.projectNameZhPlaceholder')" class="form-input">
            </div>
            <div class="form-group" v-if="formMode === 'project'">
              <label class="form-label">{{ t('tableHierarchy.form.projectNameEn') }} <span class="label-hint">{{ t('tableHierarchy.form.optional') }}</span></label>
              <input type="text" v-model="formData.name_en" :placeholder="t('tableHierarchy.form.projectNameEnPlaceholder')" class="form-input">
            </div>
            <!-- 桌台名称 -->
            <div class="form-group" v-if="formMode === 'table'">
              <label class="form-label required">{{ t('tableHierarchy.form.tableNumber') }}</label>
              <input type="text" v-model="formData.name" :placeholder="t('tableHierarchy.form.tablePlaceholder')" class="form-input">
            </div>
            <!-- 现场中英文名称 -->
            <div class="form-group" v-if="formMode === 'site'">
              <label class="form-label required">{{ t('tableHierarchy.form.siteNameZh') }}</label>
              <input type="text" v-model="formData.name_zh" :placeholder="t('tableHierarchy.form.siteNameZhPlaceholder')" class="form-input">
            </div>
            <div class="form-group" v-if="formMode === 'site'">
              <label class="form-label">{{ t('tableHierarchy.form.siteNameEn') }} <span class="label-hint">{{ t('tableHierarchy.form.optional') }}</span></label>
              <input type="text" v-model="formData.name_en" :placeholder="t('tableHierarchy.form.siteNameEnPlaceholder')" class="form-input">
            </div>
            <div class="form-group" v-if="formMode === 'table'">
              <label class="form-label">{{ t('tableHierarchy.form.gameType') }} <span class="label-hint">{{ t('tableHierarchy.form.gameTypeHint') }}</span></label>
              <div class="game-type-checkboxes">
                <label v-for="gt in gameTypeOptions" :key="gt.key" class="game-type-checkbox" :class="{ checked: formData.gameTypes?.includes(gt.key) }" @click="toggleGameType(gt.key)">
                  <span class="checkbox-icon">
                    <svg v-if="formData.gameTypes?.includes(gt.key)" viewBox="0 0 24 24" fill="none" stroke="white" stroke-width="3"><polyline points="20 6 9 17 4 12"/></svg>
                  </span>
                  <span>{{ gt.label }}</span>
                </label>
              </div>
            </div>
            <div class="form-group" v-if="formMode === 'table'">
              <label class="form-label">{{ t('tableHierarchy.form.tableStatus') }}</label>
              <label class="status-toggle" @click.prevent="formData.status = formData.status === 'enabled' ? 'disabled' : 'enabled'">
                <span class="status-checkbox" :class="{ checked: formData.status === 'enabled' }">
                  <svg v-if="formData.status === 'enabled'" viewBox="0 0 24 24" fill="none" stroke="white" stroke-width="3"><polyline points="20 6 9 17 4 12"/></svg>
                </span>
                <span :class="formData.status === 'enabled' ? 'status-enabled' : 'status-disabled'">{{ formData.status === 'enabled' ? t('tableHierarchy.status.enabled') : t('tableHierarchy.status.disabled') }}</span>
              </label>
            </div>
          </div>
          <div class="modal-footer">
            <button class="btn btn-secondary" @click="showFormModal = false">{{ t('tableHierarchy.actions.cancel') }}</button>
            <button class="btn btn-primary" @click="saveForm" :disabled="loading">
              <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M19 21H5a2 2 0 01-2-2V5a2 2 0 012-2h11l5 5v11a2 2 0 01-2 2z"/><polyline points="17 21 17 13 7 13 7 21"/><polyline points="7 3 7 8 15 8"/></svg>
              {{ loading ? t('tableHierarchy.actions.saving') : t('tableHierarchy.actions.save') }}
            </button>
          </div>
        </div>
      </div>
    </Teleport>

    <!-- 批量添加弹窗 -->
    <Teleport to="body">
      <div class="modal-overlay" :class="{ active: showBatchModal }">
        <div class="modal batch-modal">
          <div class="modal-header">
            <div class="modal-title">{{ batchTitle }}</div>
            <button class="modal-close" @click="showBatchModal = false">
              <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><line x1="18" y1="6" x2="6" y2="18"/><line x1="6" y1="6" x2="18" y2="18"/></svg>
            </button>
          </div>
          <div class="modal-body">
            <div class="form-group" v-if="batchMode === 'site' || batchMode === 'table'">
              <label class="form-label required">{{ t('tableHierarchy.form.belongProject') }}</label>
              <select v-model="batchData.project" class="form-input" @change="batchData.site = ''">
                <option value="">{{ t('tableHierarchy.form.selectProject') }}</option>
                <option v-for="p in tempProjects" :key="getProjectKey(p)" :value="getProjectKey(p)">{{ getProjectName(p) }}</option>
              </select>
            </div>
            <div class="form-group" v-if="batchMode === 'table'">
              <label class="form-label required">{{ t('tableHierarchy.form.belongSite') }}</label>
              <select v-model="batchData.site" class="form-input">
                <option value="">{{ t('tableHierarchy.form.selectSite') }}</option>
                <option v-for="s in getSitesByBatchProject()" :key="getSiteKey(s)" :value="getSiteKey(s)">{{ getSiteName(s) }}</option>
              </select>
            </div>
            <div class="form-group" v-if="batchMode === 'table'">
              <label class="form-label">{{ t('tableHierarchy.form.gameType') }} <span class="label-hint">{{ t('tableHierarchy.form.batchGameTypeHint') }}</span></label>
              <div class="game-type-checkboxes">
                <label v-for="gt in gameTypeOptions" :key="gt.key" class="game-type-checkbox" :class="{ checked: batchData.gameTypes?.includes(gt.key) }" @click="toggleBatchGameType(gt.key)">
                  <span class="checkbox-icon">
                    <svg v-if="batchData.gameTypes?.includes(gt.key)" viewBox="0 0 24 24" fill="none" stroke="white" stroke-width="3"><polyline points="20 6 9 17 4 12"/></svg>
                  </span>
                  <span>{{ gt.label }}</span>
                </label>
              </div>
            </div>
            <div class="form-group" v-if="batchMode === 'table'">
              <label class="form-label">{{ t('tableHierarchy.form.tableStatus') }}</label>
              <label class="status-toggle" @click.prevent="batchData.status = batchData.status === 'enabled' ? 'disabled' : 'enabled'">
                <span class="status-checkbox" :class="{ checked: batchData.status === 'enabled' }">
                  <svg v-if="batchData.status === 'enabled'" viewBox="0 0 24 24" fill="none" stroke="white" stroke-width="3"><polyline points="20 6 9 17 4 12"/></svg>
                </span>
                <span :class="batchData.status === 'enabled' ? 'status-enabled' : 'status-disabled'">{{ batchData.status === 'enabled' ? t('tableHierarchy.status.enabled') : t('tableHierarchy.status.disabled') }}</span>
              </label>
            </div>
            <div class="form-group">
              <label class="form-label required">
                {{ batchMode === 'project' ? t('tableHierarchy.form.projectName') : batchMode === 'site' ? t('tableHierarchy.form.siteName') : batchMode === 'gameType' ? t('tableHierarchy.form.gameTypeName') : t('tableHierarchy.form.tableNumber') }}
                <span class="label-hint">{{ (batchMode === 'gameType' || batchMode === 'project' || batchMode === 'site') ? t('tableHierarchy.form.batchGameTypeHint') : t('tableHierarchy.form.batchHint') }}</span>
              </label>
              <textarea v-model="batchData.names" class="form-textarea" :placeholder="batchPlaceholder" rows="8"></textarea>
            </div>
          </div>
          <div class="modal-footer">
            <button class="btn btn-secondary" @click="showBatchModal = false">{{ t('tableHierarchy.actions.cancel') }}</button>
            <button class="btn btn-primary" @click="saveBatch" :disabled="loading">
              <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M19 21H5a2 2 0 01-2-2V5a2 2 0 012-2h11l5 5v11a2 2 0 01-2 2z"/><polyline points="17 21 17 13 7 13 7 21"/><polyline points="7 3 7 8 15 8"/></svg>
              {{ loading ? t('tableHierarchy.actions.saving') : t('tableHierarchy.actions.save') }}
            </button>
          </div>
        </div>
      </div>
    </Teleport>

  </div>
</template>

<style scoped>
.hierarchy-config-page { padding: 20px; max-width: 100%; }

.page-header { display: flex; justify-content: space-between; align-items: center; margin-bottom: 20px; }
.header-left { display: flex; align-items: center; gap: 12px; }
.header-left h2 { margin: 0; font-size: 20px; font-weight: 600; color: var(--text-primary); }
.record-count { font-size: 13px; color: var(--text-secondary); background: var(--bg-secondary); padding: 4px 10px; border-radius: 12px; }
.header-actions { display: flex; gap: 10px; }

.btn { display: inline-flex; align-items: center; gap: 6px; padding: 8px 16px; border: none; border-radius: 8px; font-size: 14px; font-weight: 500; cursor: pointer; transition: all 0.2s; }
.btn svg { width: 16px; height: 16px; }
.btn-primary { background: #3a84ff; color: white; }
.btn-primary:hover { background: #2b6fd9; }
.btn-primary:disabled { opacity: 0.6; cursor: not-allowed; }
.btn-secondary { background: var(--bg-secondary); color: var(--text-primary); border: 1px solid var(--border-color); }
.btn-secondary:hover { background: var(--bg-hover); }
.btn-sm { padding: 6px 12px; font-size: 13px; }
.btn-sm svg { width: 14px; height: 14px; }

/* 配置按钮 */
.btn-config { background: var(--bg-secondary); color: var(--text-primary); border: 1px solid var(--border-color); position: relative; }
.btn-config:hover { border-color: var(--text-muted); background: var(--bg-hover); }
.btn-config.project { border-left: 3px solid #6366f1; }
.btn-config.project:hover { background: rgba(99, 102, 241, 0.08); border-color: #6366f1; }
.btn-config.site { border-left: 3px solid #10b981; }
.btn-config.site:hover { background: rgba(16, 185, 129, 0.08); border-color: #10b981; }
.btn-config.gametype { border-left: 3px solid #f59e0b; }
.btn-config.gametype:hover { background: rgba(245, 158, 11, 0.08); border-color: #f59e0b; }
.btn-config.table { border-left: 3px solid #3b82f6; }
.btn-config.table:hover { background: rgba(59, 130, 246, 0.08); border-color: #3b82f6; }
.btn-badge { display: inline-flex; align-items: center; justify-content: center; min-width: 20px; height: 20px; padding: 0 6px; border-radius: 10px; font-size: 11px; font-weight: 700; background: rgba(59, 130, 246, 0.15); color: #3b82f6; margin-left: 2px; }
.btn-config.project .btn-badge { background: rgba(99, 102, 241, 0.15); color: #6366f1; }
.btn-config.site .btn-badge { background: rgba(16, 185, 129, 0.15); color: #10b981; }
.btn-config.gametype .btn-badge { background: rgba(245, 158, 11, 0.15); color: #f59e0b; }
.btn-config.table .btn-badge { background: rgba(59, 130, 246, 0.15); color: #3b82f6; }

.btn .chevron { width: 14px; height: 14px; margin-left: 2px; transition: transform 0.2s; }
.btn .chevron.rotate { transform: rotate(180deg); }

/* 统计卡片 - 和桌台维护记录一致 */
.stats-cards { display: grid; grid-template-columns: repeat(4, 1fr); gap: 16px; margin-bottom: 20px; }
.stat-card { display: flex; align-items: center; gap: 14px; padding: 16px 20px; background: var(--bg-card); border: 1px solid var(--border-color); border-radius: 12px; }
.stat-icon { width: 48px; height: 48px; border-radius: 12px; display: flex; align-items: center; justify-content: center; }
.stat-icon svg { width: 24px; height: 24px; stroke: white; }
.stat-icon.project-icon { background: linear-gradient(135deg, #6366f1, #8b5cf6); }
.stat-icon.site-icon { background: linear-gradient(135deg, #10b981, #34d399); }
.stat-icon.gametype-icon { background: linear-gradient(135deg, #f59e0b, #fbbf24); }
.stat-icon.table-icon { background: linear-gradient(135deg, #3b82f6, #60a5fa); }
.stat-info { display: flex; flex-direction: column; }
.stat-label { font-size: 13px; color: var(--text-secondary); }
.stat-value { font-size: 24px; font-weight: 700; color: var(--text-primary); }

/* 搜索和筛选 - 和桌台维护记录一致 */
.filter-container { display: flex; flex-direction: column; gap: 16px; margin-bottom: 20px; }

.search-card { background: var(--bg-card); border-radius: 16px; padding: 20px; box-shadow: 0 4px 12px rgba(0, 0, 0, 0.05); border: 1px solid var(--border-color); }
body.light-mode .search-card { box-shadow: 0 4px 12px rgba(0, 0, 0, 0.08); }
.search-main { display: flex; gap: 12px; }
.search-input-wrapper { flex: 1; position: relative; }
.search-input-wrapper .search-icon { position: absolute; left: 16px; top: 50%; transform: translateY(-50%); width: 20px; height: 20px; color: var(--text-muted); pointer-events: none; }
.search-input-wrapper input { width: 100%; padding: 14px 16px 14px 48px; border: 2px solid var(--border-color); border-radius: 12px; font-size: 15px; background: var(--bg-secondary); color: var(--text-primary); transition: all 0.2s; box-sizing: border-box; }
.search-input-wrapper input::placeholder { color: var(--text-muted); }
.search-input-wrapper input:focus { outline: none; border-color: #3b82f6; background: var(--bg-primary); box-shadow: 0 0 0 3px rgba(59, 130, 246, 0.15); }

.btn-search { padding: 14px 28px; background: linear-gradient(135deg, #3b82f6 0%, #2563eb 100%); color: #fff; border: none; border-radius: 12px; font-size: 15px; font-weight: 600; cursor: pointer; transition: all 0.2s; display: flex; align-items: center; gap: 8px; white-space: nowrap; }
.btn-search:hover { transform: translateY(-1px); box-shadow: 0 4px 12px rgba(59, 130, 246, 0.4); }
.btn-search:active { transform: translateY(0); }

.filters-card { background: var(--bg-card); border-radius: 16px; padding: 20px; box-shadow: 0 4px 12px rgba(0, 0, 0, 0.05); border: 1px solid var(--border-color); }
body.light-mode .filters-card { box-shadow: 0 4px 12px rgba(0, 0, 0, 0.08); }
.filters-header { display: flex; align-items: center; justify-content: space-between; margin-bottom: 16px; padding-bottom: 14px; border-bottom: 1px solid var(--border-color); }
.filters-title { font-size: 14px; font-weight: 600; color: var(--text-secondary); display: flex; align-items: center; gap: 8px; }
.filters-title svg { width: 16px; height: 16px; color: var(--text-muted); }

.btn-reset { padding: 8px 16px; background: var(--bg-secondary); color: var(--text-secondary); border: 1px solid var(--border-color); border-radius: 8px; font-size: 13px; font-weight: 500; cursor: pointer; transition: all 0.2s; }
.btn-reset:hover { background: var(--bg-hover); color: var(--text-primary); border-color: var(--text-muted); }

.filters-grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(180px, 1fr)); gap: 16px; }
.filter-field { display: flex; flex-direction: column; gap: 6px; }
.filter-field label { font-size: 12px; font-weight: 600; color: var(--text-muted); text-transform: uppercase; letter-spacing: 0.3px; }
.filter-field select, .filter-field input { width: 100%; padding: 10px 12px; border: 1px solid var(--border-color); border-radius: 8px; font-size: 14px; background: var(--bg-secondary); color: var(--text-primary); transition: all 0.2s; appearance: none; box-sizing: border-box; }
.filter-field select { background-image: url("data:image/svg+xml,%3Csvg xmlns='http://www.w3.org/2000/svg' fill='none' viewBox='0 0 24 24' stroke='%2394a3b8'%3E%3Cpath stroke-linecap='round' stroke-linejoin='round' stroke-width='2' d='M19 9l-7 7-7-7'/%3E%3C/svg%3E"); background-repeat: no-repeat; background-position: right 10px center; background-size: 16px; padding-right: 36px; cursor: pointer; }
.filter-field select:focus, .filter-field input:focus { outline: none; border-color: #3b82f6; background: var(--bg-primary); box-shadow: 0 0 0 3px rgba(59, 130, 246, 0.1); }

/* 游戏类型管理区域 */
.game-type-section { background: var(--bg-card); border: 1px solid var(--border-color); border-radius: 12px; margin-bottom: 20px; overflow: hidden; }
.section-header { display: flex; justify-content: space-between; align-items: center; cursor: pointer; padding: 16px 20px; transition: background 0.15s; }
.section-header:hover { background: var(--bg-hover); }
.section-title-row { display: flex; align-items: center; gap: 8px; font-size: 14px; font-weight: 600; color: var(--text-primary); }
.section-title-row .section-icon { width: 18px; height: 18px; color: #10b981; }
.expand-icon { width: 16px; height: 16px; color: var(--text-secondary); transition: transform 0.2s; }
.expand-icon.expanded { transform: rotate(90deg); }
.count-badge.gt-badge { background: #10b981; color: white; font-size: 11px; font-weight: 600; padding: 2px 8px; border-radius: 10px; }
.game-type-table-wrapper { overflow-x: auto; }

/* 添加下拉菜单 */
.add-dropdown { position: relative; }
.dropdown-menu { position: absolute; top: calc(100% + 8px); right: 0; width: 220px; background: var(--bg-card); border: 1px solid var(--border-color); border-radius: 12px; box-shadow: 0 10px 40px rgba(0, 0, 0, 0.15); opacity: 0; visibility: hidden; transform: translateY(-8px); transition: all 0.2s; z-index: 100; overflow: hidden; }
.dropdown-menu.show { opacity: 1; visibility: visible; transform: translateY(0); }
.dropdown-section { padding: 8px; }
.dropdown-title { padding: 8px 12px 6px; font-size: 11px; font-weight: 600; color: var(--text-muted); text-transform: uppercase; letter-spacing: 0.5px; }
.dropdown-divider { height: 1px; background: var(--border-color); margin: 0; }
.dropdown-item { display: flex; align-items: center; gap: 10px; width: 100%; padding: 10px 12px; border: none; background: transparent; font-size: 14px; color: var(--text-primary); cursor: pointer; border-radius: 8px; transition: background 0.15s; text-align: left; }
.dropdown-item:hover { background: var(--bg-hover); }
.dropdown-item .item-icon { width: 28px; height: 28px; display: flex; align-items: center; justify-content: center; border-radius: 6px; flex-shrink: 0; }
.dropdown-item .item-icon svg { width: 16px; height: 16px; }
.dropdown-item .item-icon.project { background: rgba(58, 132, 255, 0.12); color: #3a84ff; }
.dropdown-item .item-icon.site { background: rgba(34, 197, 94, 0.12); color: #16a34a; }
.dropdown-item .item-icon.table { background: rgba(168, 85, 247, 0.12); color: #9333ea; }
.dropdown-item .item-icon.game-type { background: rgba(245, 158, 11, 0.12); color: #f59e0b; }

/* 数据表格 - 和桌台维护记录一致 */
.table-wrapper { background: var(--bg-card); border: 1px solid var(--border-color); border-radius: 12px; overflow: hidden; position: relative; }
.table-container { overflow-x: auto; }
.data-table { width: 100%; border-collapse: collapse; table-layout: fixed; }
.data-table th, .data-table td { padding: 12px 14px; text-align: left; border-bottom: 1px solid var(--border-color); white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
.data-table th { background: var(--bg-secondary); font-size: 13px; font-weight: 600; color: var(--text-secondary); position: sticky; top: 0; }
.data-table td { font-size: 14px; color: var(--text-primary); }
.data-table tr:hover { background: var(--bg-hover); }
.empty { text-align: center; padding: 40px !important; color: var(--text-muted); }

/* 管理区域（项目/现场）通用样式 */
.management-section { background: var(--bg-card); border: 1px solid var(--border-color); border-radius: 12px; margin-bottom: 20px; overflow: hidden; }
.section-table-wrapper { overflow-x: auto; }
.count-badge.project-badge { background: #6366f1; color: white; font-size: 11px; font-weight: 600; padding: 2px 8px; border-radius: 10px; }
.count-badge.site-badge { background: #10b981; color: white; font-size: 11px; font-weight: 600; padding: 2px 8px; border-radius: 10px; }
.project-ref-tag { display: inline-block; padding: 2px 8px; font-size: 12px; font-weight: 500; background: rgba(99, 102, 241, 0.1); color: #6366f1; border-radius: 4px; }

/* 扁平表格行样式 */
.row-number { color: var(--text-muted); font-size: 13px; text-align: center; }
.table-number { font-weight: 600; color: var(--text-primary); }

/* 操作列固定右侧 */
.sticky-col { position: sticky; right: 0; background: var(--bg-card); box-shadow: -2px 0 8px rgba(0, 0, 0, 0.06); z-index: 1; }
thead .sticky-col { background: var(--bg-secondary); z-index: 2; }

.cell-content { display: flex; align-items: center; gap: 8px; }
.type-badge { padding: 3px 8px; border-radius: 6px; font-size: 11px; font-weight: 600; flex-shrink: 0; }
.type-badge.project { background: rgba(58, 132, 255, 0.12); color: #3a84ff; }
.type-badge.site { background: rgba(34, 197, 94, 0.12); color: #16a34a; }
.type-badge.table { background: rgba(168, 85, 247, 0.12); color: #9333ea; }
.type-badge.game-type { background: rgba(16, 185, 129, 0.1); color: #10b981; }

.game-type-tags { display: flex; flex-wrap: wrap; gap: 4px; }
.game-type-tag { display: inline-block; padding: 2px 8px; font-size: 11px; font-weight: 500; background: rgba(16, 185, 129, 0.12); color: #059669; border-radius: 4px; }
.cell-muted { color: var(--text-muted); font-size: 13px; }

.count-badge { display: inline-flex; align-items: center; justify-content: center; min-width: 28px; height: 24px; padding: 0 8px; border-radius: 12px; font-size: 12px; font-weight: 600; }
.count-badge.site-count { background: rgba(34, 197, 94, 0.12); color: #16a34a; }
.count-badge.table-count { background: rgba(168, 85, 247, 0.12); color: #9333ea; }

/* 分页 */
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

.action-btns { display: flex; gap: 6px; }
.action-btn { width: 30px; height: 30px; border: none; border-radius: 6px; background: var(--bg-secondary); cursor: pointer; display: flex; align-items: center; justify-content: center; transition: all 0.2s; }
.action-btn svg { width: 15px; height: 15px; stroke: var(--text-secondary); }
.action-btn:hover { background: #3a84ff; }
.action-btn:hover svg { stroke: white; }
.action-btn.danger:hover:not(:disabled) { background: #ea3636; }
.action-btn.add:hover { background: #10b981; }
.action-btn:disabled { opacity: 0.3; cursor: not-allowed; }
.action-btn:disabled:hover { background: var(--bg-secondary); }
.action-btn:disabled:hover svg { stroke: var(--text-secondary); }

/* 管理弹窗 */
.manager-modal { max-width: 800px; }
.manager-modal-wide { max-width: 960px; }
.manager-body { padding: 16px 24px 24px; }
.manager-toolbar { display: flex; gap: 8px; margin-bottom: 16px; }
.manager-table-scroll { border: 1px solid var(--border-color); border-radius: 8px; overflow: hidden; }
.manager-table-scroll .data-table { margin: 0; }
.modal-count { font-size: 14px; font-weight: 400; color: var(--text-muted); margin-left: 4px; }
.manager-pagination { display: flex; align-items: center; justify-content: space-between; gap: 12px; margin-top: 12px; padding-top: 12px; border-top: 1px solid var(--border-color); flex-wrap: wrap; }
.mp-left { display: flex; align-items: center; gap: 10px; }
.mp-right { display: flex; align-items: center; gap: 6px; }
.mp-info { font-size: 13px; color: var(--text-secondary); }
.mp-info strong { color: var(--text-primary); font-weight: 600; }
.mp-page { font-size: 13px; color: var(--text-secondary); padding: 0 8px; }
.mp-size-select { padding: 4px 8px; border: 1px solid var(--border-color); background: var(--bg-card); border-radius: 6px; font-size: 13px; color: var(--text-primary); cursor: pointer; }

.game-type-checkboxes { display: flex; flex-wrap: wrap; gap: 8px; }
.game-type-checkbox { display: flex; align-items: center; gap: 6px; padding: 6px 12px; border: 1px solid var(--border-color); border-radius: 6px; cursor: pointer; transition: all 0.15s; font-size: 13px; }
.game-type-checkbox:hover { border-color: var(--primary); background: rgba(58, 132, 255, 0.05); }
.game-type-checkbox.checked { border-color: var(--primary); background: rgba(58, 132, 255, 0.1); }
.game-type-checkbox .checkbox-icon { width: 16px; height: 16px; border: 2px solid var(--border-color); border-radius: 4px; display: flex; align-items: center; justify-content: center; transition: all 0.15s; }
.game-type-checkbox.checked .checkbox-icon { background: var(--primary); border-color: var(--primary); }
.game-type-checkbox .checkbox-icon svg { width: 10px; height: 10px; }

.empty-cell { padding: 60px 24px !important; }
.empty-state { display: flex; flex-direction: column; align-items: center; justify-content: center; color: var(--text-muted); }
.empty-state svg { width: 64px; height: 64px; opacity: 0.3; margin-bottom: 16px; }
.empty-state p { margin: 0 0 8px; font-size: 16px; font-weight: 500; color: var(--text-secondary); }
.empty-state span { font-size: 14px; }

.loading-state { display: flex; flex-direction: column; align-items: center; justify-content: center; padding: 60px 24px; color: var(--text-muted); }
.spinner { width: 40px; height: 40px; border: 3px solid var(--border-color); border-top-color: #3a84ff; border-radius: 50%; animation: spin 0.8s linear infinite; margin-bottom: 16px; }
@keyframes spin { to { transform: rotate(360deg); } }

/* 弹窗样式 */
.modal-overlay { position: fixed; top: 0; left: 0; right: 0; bottom: 0; background: rgba(0,0,0,0.5); display: flex; align-items: center; justify-content: center; z-index: 1000; opacity: 0; visibility: hidden; transition: all 0.3s; }
.modal-overlay.active { opacity: 1; visibility: visible; }
.modal { background: var(--bg-card); border-radius: 16px; width: 90%; max-width: 700px; max-height: 90vh; display: flex; flex-direction: column; box-shadow: 0 20px 60px rgba(0,0,0,0.3); }
.modal-overlay.active .modal { transform: scale(1); }
.form-modal { max-width: 480px; }
.batch-modal { max-width: 520px; }
.modal-header { display: flex; justify-content: space-between; align-items: center; padding: 20px 24px; border-bottom: 1px solid var(--border-color); }
.modal-title { display: flex; align-items: center; gap: 10px; font-size: 18px; font-weight: 600; }
.modal-close { width: 32px; height: 32px; border: none; background: var(--bg-secondary); border-radius: 8px; cursor: pointer; display: flex; align-items: center; justify-content: center; }
.modal-close svg { width: 18px; height: 18px; stroke: var(--text-secondary); }
.modal-close:hover { background: var(--bg-hover); }
.modal-body { flex: 1; overflow-y: auto; padding: 24px; }
.modal-footer { display: flex; justify-content: flex-end; gap: 12px; padding: 16px 24px; border-top: 1px solid var(--border-color); }

.form-group { margin-bottom: 20px; }
.form-group:last-child { margin-bottom: 0; }
.form-label { display: block; margin-bottom: 8px; font-size: 14px; font-weight: 500; color: var(--text-primary); }
.form-label.required::after { content: ' *'; color: #ef4444; }
.label-hint { font-weight: 400; color: var(--text-muted); font-size: 12px; }
.form-input { width: 100%; padding: 10px 14px; background: var(--bg-input, var(--bg-primary)); border: 1px solid var(--border-color); border-radius: 8px; color: var(--text-primary); font-size: 14px; font-family: inherit; transition: border-color 0.2s; box-sizing: border-box; }
.form-input:focus { outline: none; border-color: #3a84ff; }
.form-textarea { width: 100%; padding: 12px 16px; border: 1px solid var(--border-color); border-radius: 10px; font-size: 14px; font-family: inherit; background: var(--bg-secondary); color: var(--text-primary); transition: all 0.2s; box-sizing: border-box; resize: vertical; min-height: 120px; }
.form-textarea:focus { outline: none; border-color: #3a84ff; box-shadow: 0 0 0 3px rgba(58, 132, 255, 0.12); }
.form-textarea::placeholder { color: var(--text-muted); }

/* 状态标签（列表展示） */
.status-tag-on { display: inline-flex; align-items: center; gap: 4px; padding: 3px 10px; border-radius: 10px; font-size: 12px; font-weight: 500; background: rgba(16, 185, 129, 0.12); color: #10b981; }
.status-tag-on::before { content: ''; display: inline-block; width: 6px; height: 6px; border-radius: 50%; background: #10b981; }
.status-tag-off { display: inline-flex; align-items: center; gap: 4px; padding: 3px 10px; border-radius: 10px; font-size: 12px; font-weight: 500; background: rgba(156, 163, 175, 0.15); color: #9ca3af; }
.status-tag-off::before { content: ''; display: inline-block; width: 6px; height: 6px; border-radius: 50%; background: #9ca3af; }

/* 状态切换（编辑弹窗） */
.status-toggle { display: inline-flex; align-items: center; gap: 8px; cursor: pointer; user-select: none; }
.status-checkbox { display: inline-flex; align-items: center; justify-content: center; width: 20px; height: 20px; border-radius: 4px; border: 2px solid #d1d5db; background: white; transition: all 0.2s; flex-shrink: 0; }
.status-checkbox svg { width: 14px; height: 14px; }
.status-checkbox.checked { background: #10b981; border-color: #10b981; }
.status-enabled { color: #10b981; font-size: 13px; font-weight: 500; }
.status-disabled { color: #9ca3af; font-size: 13px; font-weight: 500; }

@media (max-width: 900px) {
  .filters-grid { grid-template-columns: repeat(2, 1fr); }
  .stats-cards { grid-template-columns: repeat(2, 1fr); }
}
@media (max-width: 600px) {
  .search-main { flex-direction: column; }
  .btn-search { justify-content: center; }
  .filters-grid { grid-template-columns: 1fr; }
  .stats-cards { grid-template-columns: 1fr; }
  .page-header { flex-direction: column; gap: 12px; }
}
</style>
