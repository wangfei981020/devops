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

const loading = ref(false)
const searchQuery = ref('')

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
const formData = ref({ name: '', name_zh: '', name_en: '', project: '', site: '', gameTypes: [] })

// 批量添加弹窗
const showBatchModal = ref(false)
const batchMode = ref('project')
const batchData = ref({ names: '', project: '', site: '', gameTypes: [] })

// 添加菜单下拉
const showAddMenu = ref(false)


// 展开的项目
const expandedProjects = ref({})

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

// 获取默认游戏类型（首次加载时）
function getDefaultGameTypes() {
  return [
    { name_zh: '百家乐', name_en: 'Baccarat' },
    { name_zh: '龙虎', name_en: 'Dragon Tiger' },
    { name_zh: '轮盘', name_en: 'Roulette' },
    { name_zh: '骰宝', name_en: 'Sic Bo' },
    { name_zh: '其他', name_en: 'Other' }
  ]
}

function resetTemp() {
  tempProjects.value = [...projectOptions.value]
  tempSites.value = JSON.parse(JSON.stringify(siteOptions.value))
  tempTables.value = JSON.parse(JSON.stringify(tableOptions.value))
  tempGameTypes.value = JSON.parse(JSON.stringify(gameTypeData.value))
}

async function saveConfig() {
  loading.value = true
  try {
    const config = {
      projects: tempProjects.value,
      sites: tempSites.value,
      tables: tempTables.value,
      gameTypes: tempGameTypes.value,
      halls: [],
      maintTypes: ['无', '紧急维护', '临时维护', '例行维护']
    }
    await saveTableHierarchy(config)
    projectOptions.value = [...tempProjects.value]
    siteOptions.value = JSON.parse(JSON.stringify(tempSites.value))
    tableOptions.value = JSON.parse(JSON.stringify(tempTables.value))
    gameTypeData.value = JSON.parse(JSON.stringify(tempGameTypes.value))
    appStore.showToast(t('tableHierarchy.messages.saveSuccess'), 'success')
  } catch (e) {
    appStore.showToast(t('tableHierarchy.messages.saveFailed'), 'error')
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

function getSitesByFormProject() {
  const project = formData.value.project
  if (!project) return []
  return tempSites.value.filter(s => s.project === project)
}

function getSitesByBatchProject() {
  const project = batchData.value.project
  if (!project) return []
  return tempSites.value.filter(s => s.project === project)
}

function getSiteCountByProject(projectName) {
  return tempSites.value.filter(s => s.project === projectName).length
}

function getTableCountByProject(projectName) {
  return tempTables.value.filter(t => t.project === projectName).length
}

function getTableCountBySite(projectName, siteName) {
  return tempTables.value.filter(t => t.project === projectName && t.site === siteName).length
}

function getSitesByProject(projectName) {
  return tempSites.value.filter(s => s.project === projectName)
}

function getTablesBySite(projectName, siteName) {
  return tempTables.value.filter(t => t.project === projectName && t.site === siteName)
}

const filteredProjects = computed(() => {
  if (!searchQuery.value.trim()) return tempProjects.value
  const q = searchQuery.value.toLowerCase()
  return tempProjects.value.filter(p => {
    if (p.toLowerCase().includes(q)) return true
    const sites = getSitesByProject(p)
    for (const s of sites) {
      if (getSiteName(s).toLowerCase().includes(q)) return true
      const tables = getTablesBySite(s.project, getSiteKey(s))
      for (const t of tables) {
        if (t.name.toLowerCase().includes(q)) return true
      }
    }
    return false
  })
})

const stats = computed(() => {
  // 现场去重统计（按名称）
  const uniqueSiteNames = new Set(tempSites.value.map(s => getSiteName(s)))
  return {
    projects: tempProjects.value.length,
    sites: uniqueSiteNames.size,
    tables: tempTables.value.length
  }
})

function toggleProject(projectName) {
  expandedProjects.value[projectName] = !expandedProjects.value[projectName]
}

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
  formData.value = { 
    name: '', 
    name_zh: '',
    name_en: '',
    project: parentProject || tempProjects.value[0] || '',
    site: parentSite || (tempSites.value[0] ? getSiteKey(tempSites.value[0]) : ''),
    gameTypes: []
  }
  showFormModal.value = true
}

// 打开批量添加弹窗
function openBatchModal(mode) {
  showAddMenu.value = false
  batchMode.value = mode
  batchData.value = { 
    names: '', 
    project: tempProjects.value[0] || '',
    site: tempSites.value[0] ? getSiteKey(tempSites.value[0]) : '',
    gameTypes: []
  }
  showBatchModal.value = true
}

// 打开编辑弹窗
function openEditModal(mode, idx, item) {
  formMode.value = mode
  editingIdx.value = idx
  if (mode === 'project') {
    formData.value = { name: item, name_zh: '', name_en: '', project: '', site: '', gameTypes: [] }
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
    formData.value = { name: item.name, name_zh: '', name_en: '', project: item.project || '', site: item.site, gameTypes: item.gameTypes || [] }
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

// 保存单个表单 - 直接保存到后端
async function saveForm() {
  const { name, project, site } = formData.value
  
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
      const oldKey = getGameTypeKey(tempGameTypes.value[editingIdx.value])
      // 更新所有桌台中使用的旧游戏类型键
      tempTables.value.forEach(table => {
        if (table.gameTypes?.includes(oldKey)) {
          const idx = table.gameTypes.indexOf(oldKey)
          table.gameTypes[idx] = name_zh
        }
      })
      tempGameTypes.value[editingIdx.value] = newItem
    } else {
      const exists = tempGameTypes.value.some(gt => getGameTypeKey(gt) === name_zh)
      if (exists) {
        appStore.showToast(t('common.error'), 'warning')
        return
      }
      tempGameTypes.value.push(newItem)
    }
    showFormModal.value = false
    await saveConfig()
    return
  }
  
  if (!name.trim()) {
    appStore.showToast(t('common.error'), 'warning')
    return
  }

  if (formMode.value === 'project') {
    if (editingIdx.value >= 0) {
      const oldName = tempProjects.value[editingIdx.value]
      tempSites.value.forEach(s => {
        if (s.project === oldName) s.project = name.trim()
      })
      tempProjects.value[editingIdx.value] = name.trim()
    } else {
      if (tempProjects.value.includes(name.trim())) {
        appStore.showToast(t('common.error'), 'warning')
        return
      }
      tempProjects.value.push(name.trim())
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
      const oldKey = getSiteKey(tempSites.value[editingIdx.value])
      tempTables.value.forEach(t => {
        if (t.site === oldKey) t.site = name_zh
      })
      tempSites.value[editingIdx.value] = newItem
    } else {
      const exists = tempSites.value.some(s => getSiteKey(s) === name_zh && s.project === project)
      if (exists) {
        appStore.showToast(t('common.error'), 'warning')
        return
      }
      tempSites.value.push(newItem)
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
    const newItem = { name: name.trim(), site, project, gameTypes }
    if (editingIdx.value >= 0) {
      tempTables.value[editingIdx.value] = newItem
    } else {
      const exists = tempTables.value.some(t => t.name === name.trim() && t.site === site && t.project === project)
      if (exists) {
        appStore.showToast(t('common.error'), 'warning')
        return
      }
      tempTables.value.push(newItem)
    }
  }

  showFormModal.value = false
  // 直接保存到后端
  await saveConfig()
}

// 保存批量添加 - 直接保存到后端
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

  let addedCount = 0
  let skippedCount = 0

  if (batchMode.value === 'project') {
    for (const name of nameList) {
      if (!tempProjects.value.includes(name)) {
        tempProjects.value.push(name)
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
    for (const name of nameList) {
      const exists = tempSites.value.some(s => getSiteName(s) === name && s.project === project)
      if (!exists) {
        tempSites.value.push({ name, project })
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
      const exists = tempTables.value.some(t => t.name === name && t.site === site && t.project === project)
      if (!exists) {
        tempTables.value.push({ name, site, project, gameTypes: [...gameTypes] })
        addedCount++
      } else {
        skippedCount++
      }
    }
  }

  showBatchModal.value = false
  // 直接保存到后端
  await saveConfig()
  appStore.showToast(t('tableHierarchy.messages.saveSuccess'), 'success')
}

// 删除操作 - 直接保存到后端
// 确认删除
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
  
  // 执行删除
  if (type === 'project') {
    const projectName = tempProjects.value[idx]
    const projectSiteKeys = tempSites.value.filter(s => s.project === projectName).map(s => getSiteKey(s))
    tempTables.value = tempTables.value.filter(t => !projectSiteKeys.includes(t.site))
    tempSites.value = tempSites.value.filter(s => s.project !== projectName)
    tempProjects.value.splice(idx, 1)
  } else if (type === 'site') {
    const siteKey = getSiteKey(tempSites.value[idx])
    tempTables.value = tempTables.value.filter(t => t.site !== siteKey)
    tempSites.value.splice(idx, 1)
  } else if (type === 'table') {
    tempTables.value.splice(idx, 1)
  }
  
  await saveConfig()
  appStore.showToast(t('tableHierarchy.messages.deleteSuccess'), 'success')
}

function getSiteIndex(site) {
  return tempSites.value.findIndex(s => getSiteKey(s) === getSiteKey(site) && s.project === site.project)
}

function getTableIndex(table) {
  return tempTables.value.findIndex(t => t.name === table.name && t.site === table.site)
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
  } else {
    return t('tableHierarchy.form.batchAddTable')
  }
})
</script>

<template>
  <div class="hierarchy-config-page" :key="currentLanguage">
    <!-- 页面头部 -->
    <div class="page-header">
      <div class="header-left">
        <h2>{{ t('tableHierarchy.pageTitle') }}</h2>
        <span class="subtitle">{{ t('tableHierarchy.subtitle') }}</span>
      </div>
    </div>

    <!-- 统计卡片 + 搜索 -->
    <div class="toolbar-row">
      <div class="stats-mini">
        <div class="stat-item">
          <span class="stat-num">{{ stats.projects }}</span>
          <span class="stat-text">{{ t('tableHierarchy.stats.projects') }}</span>
        </div>
        <div class="stat-divider"></div>
        <div class="stat-item">
          <span class="stat-num">{{ stats.sites }}</span>
          <span class="stat-text">{{ t('tableHierarchy.stats.sites') }}</span>
        </div>
        <div class="stat-divider"></div>
        <div class="stat-item">
          <span class="stat-num">{{ stats.tables }}</span>
          <span class="stat-text">{{ t('tableHierarchy.stats.tables') }}</span>
        </div>
      </div>
      <div class="toolbar-right">
        <div class="search-box">
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><circle cx="11" cy="11" r="8"/><path d="M21 21l-4.35-4.35"/></svg>
          <input type="text" v-model="searchQuery" :placeholder="t('tableHierarchy.search.placeholder')">
        </div>
        <!-- 添加按钮下拉菜单 -->
        <div v-if="canCreate" class="add-dropdown">
          <button class="btn btn-primary btn-sm" @click.stop="toggleAddMenu">
            <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M12 5v14M5 12h14"/></svg>
            {{ t('tableHierarchy.add.button') }}
            <svg class="chevron" :class="{ rotate: showAddMenu }" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><polyline points="6 9 12 15 18 9"/></svg>
          </button>
          <div class="dropdown-menu" :class="{ show: showAddMenu }">
            <div class="dropdown-section">
              <div class="dropdown-title">{{ t('tableHierarchy.add.single') }}</div>
              <button class="dropdown-item" @click="openAddModal('project')">
                <div class="item-icon project"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M2 3h6a4 4 0 0 1 4 4v14a3 3 0 0 0-3-3H2z"/><path d="M22 3h-6a4 4 0 0 0-4 4v14a3 3 0 0 1 3-3h7z"/></svg></div>
                <span>{{ t('tableHierarchy.add.addProject') }}</span>
              </button>
              <button class="dropdown-item" @click="openAddModal('site')">
                <div class="item-icon site"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><rect x="3" y="3" width="18" height="18" rx="2"/><path d="M3 9h18"/><path d="M9 21V9"/></svg></div>
                <span>{{ t('tableHierarchy.add.addSite') }}</span>
              </button>
              <button class="dropdown-item" @click="openAddModal('table')">
                <div class="item-icon table"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><rect x="3" y="4" width="18" height="12" rx="2"/><path d="M7 20h10"/><path d="M9 16v4"/><path d="M15 16v4"/></svg></div>
                <span>{{ t('tableHierarchy.add.addTable') }}</span>
              </button>
              <button class="dropdown-item" @click="openAddModal('gameType')">
                <div class="item-icon game-type"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><circle cx="12" cy="12" r="10"/><path d="M8 14s1.5 2 4 2 4-2 4-2"/><line x1="9" y1="9" x2="9.01" y2="9"/><line x1="15" y1="9" x2="15.01" y2="9"/></svg></div>
                <span>{{ t('tableHierarchy.add.addGameType') }}</span>
              </button>
            </div>
            <div class="dropdown-divider"></div>
            <div class="dropdown-section">
              <div class="dropdown-title">{{ t('tableHierarchy.add.batch') }}</div>
              <button class="dropdown-item" @click="openBatchModal('project')">
                <div class="item-icon project"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M8 6h13M8 12h13M8 18h13M3 6h.01M3 12h.01M3 18h.01"/></svg></div>
                <span>{{ t('tableHierarchy.add.batchProject') }}</span>
              </button>
              <button class="dropdown-item" @click="openBatchModal('site')">
                <div class="item-icon site"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M8 6h13M8 12h13M8 18h13M3 6h.01M3 12h.01M3 18h.01"/></svg></div>
                <span>{{ t('tableHierarchy.add.batchSite') }}</span>
              </button>
              <button class="dropdown-item" @click="openBatchModal('table')">
                <div class="item-icon table"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M8 6h13M8 12h13M8 18h13M3 6h.01M3 12h.01M3 18h.01"/></svg></div>
                <span>{{ t('tableHierarchy.add.batchTable') }}</span>
              </button>
            </div>
          </div>
        </div>
      </div>
    </div>

    <!-- 表格内容 -->
    <div class="main-content">
      <div class="table-wrapper" v-if="!loading">
        <table class="hierarchy-table">
          <thead>
            <tr>
              <th style="width: 40px;"></th>
              <th style="width: 180px;">{{ t('tableHierarchy.columns.project') }}</th>
              <th style="width: 120px;">{{ t('tableHierarchy.columns.site') }}</th>
              <th style="width: 100px;">{{ t('tableHierarchy.columns.table') }}</th>
              <th style="width: 150px;">{{ t('tableHierarchy.columns.gameType') }}</th>
              <th style="width: 80px;">{{ t('tableHierarchy.columns.siteCount') }}</th>
              <th style="width: 80px;">{{ t('tableHierarchy.columns.tableCount') }}</th>
              <th style="width: 120px;">{{ t('tableHierarchy.columns.actions') }}</th>
            </tr>
          </thead>
          <tbody>
            <template v-if="filteredProjects.length">
              <template v-for="(proj, pIdx) in filteredProjects" :key="proj">
                <tr class="project-row" @click="toggleProject(proj)">
                  <td class="expand-cell">
                    <svg :class="{ expanded: expandedProjects[proj] }" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><polyline points="9 18 15 12 9 6"/></svg>
                  </td>
                  <td>
                    <div class="cell-content project-name">
                      <div class="type-badge project">{{ t('tableHierarchy.types.project') }}</div>
                      <span>{{ proj }}</span>
                    </div>
                  </td>
                  <td>-</td>
                  <td>-</td>
                  <td>-</td>
                  <td><span class="count-badge site-count">{{ getSiteCountByProject(proj) }}</span></td>
                  <td><span class="count-badge table-count">{{ getTableCountByProject(proj) }}</span></td>
                  <td>
                    <div class="action-btns" @click.stop>
                      <button v-if="canCreate" class="action-btn add" @click="openAddModal('site', proj)" :title="t('tableHierarchy.actions.addSite')">
                        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M12 5v14M5 12h14"/></svg>
                      </button>
                      <button v-if="canUpdate" class="action-btn edit" @click="openEditModal('project', tempProjects.indexOf(proj), proj)" :title="t('tableHierarchy.actions.edit')">
                        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M11 4H4a2 2 0 00-2 2v14a2 2 0 002 2h14a2 2 0 002-2v-7"/><path d="M18.5 2.5a2.121 2.121 0 013 3L12 15l-4 1 1-4 9.5-9.5z"/></svg>
                      </button>
                      <button v-if="canDelete" class="action-btn danger" @click="confirmDelete('project', tempProjects.indexOf(proj))" :title="t('tableHierarchy.actions.delete')">
                        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><polyline points="3 6 5 6 21 6"/><path d="M19 6v14a2 2 0 01-2 2H7a2 2 0 01-2-2V6m3 0V4a2 2 0 012-2h4a2 2 0 012 2v2"/></svg>
                      </button>
                    </div>
                  </td>
                </tr>
                <template v-if="expandedProjects[proj]">
                  <template v-for="site in getSitesByProject(proj)" :key="getSiteName(site)">
                    <tr class="site-row">
                      <td></td>
                      <td></td>
                      <td>
                        <div class="cell-content site-name">
                          <div class="type-badge site">{{ t('tableHierarchy.types.site') }}</div>
                          <span>{{ getSiteName(site) }}</span>
                        </div>
                      </td>
                      <td>-</td>
                      <td>-</td>
                      <td>-</td>
                      <td><span class="count-badge table-count">{{ getTableCountBySite(site.project, getSiteKey(site)) }}</span></td>
                      <td>
                        <div class="action-btns">
                          <button v-if="canCreate" class="action-btn add" @click="openAddModal('table', site.project, getSiteKey(site))" :title="t('tableHierarchy.actions.addTable')">
                            <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M12 5v14M5 12h14"/></svg>
                          </button>
                          <button v-if="canUpdate" class="action-btn edit" @click="openEditModal('site', getSiteIndex(site), site)" :title="t('tableHierarchy.actions.edit')">
                            <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M11 4H4a2 2 0 00-2 2v14a2 2 0 002 2h14a2 2 0 002-2v-7"/><path d="M18.5 2.5a2.121 2.121 0 013 3L12 15l-4 1 1-4 9.5-9.5z"/></svg>
                          </button>
                          <button v-if="canDelete" class="action-btn danger" @click="confirmDelete('site', getSiteIndex(site))" :title="t('tableHierarchy.actions.delete')">
                            <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><polyline points="3 6 5 6 21 6"/><path d="M19 6v14a2 2 0 01-2 2H7a2 2 0 01-2-2V6m3 0V4a2 2 0 012-2h4a2 2 0 012 2v2"/></svg>
                          </button>
                        </div>
                      </td>
                    </tr>
                    <tr v-for="table in getTablesBySite(site.project, getSiteKey(site))" :key="table.name" class="table-row">
                      <td></td>
                      <td></td>
                      <td></td>
                      <td>
                        <div class="cell-content table-name">
                          <div class="type-badge table">{{ t('tableHierarchy.types.table') }}</div>
                          <span>{{ table.name }}</span>
                        </div>
                      </td>
                      <td>
                        <div class="game-type-tags" v-if="table.gameTypes?.length">
                          <span v-for="gt in table.gameTypes" :key="gt" class="game-type-tag">{{ translateGameType(gt) }}</span>
                        </div>
                        <span v-else class="cell-muted">-</span>
                      </td>
                      <td>-</td>
                      <td>-</td>
                      <td>
                        <div class="action-btns">
                          <button v-if="canUpdate" class="action-btn edit" @click="openEditModal('table', getTableIndex(table), table)" :title="t('tableHierarchy.actions.edit')">
                            <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M11 4H4a2 2 0 00-2 2v14a2 2 0 002 2h14a2 2 0 002-2v-7"/><path d="M18.5 2.5a2.121 2.121 0 013 3L12 15l-4 1 1-4 9.5-9.5z"/></svg>
                          </button>
                          <button v-if="canDelete" class="action-btn danger" @click="confirmDelete('table', getTableIndex(table))" :title="t('tableHierarchy.actions.delete')">
                            <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><polyline points="3 6 5 6 21 6"/><path d="M19 6v14a2 2 0 01-2 2H7a2 2 0 01-2-2V6m3 0V4a2 2 0 012-2h4a2 2 0 012 2v2"/></svg>
                          </button>
                        </div>
                      </td>
                    </tr>
                  </template>
                </template>
              </template>
            </template>
            <tr v-else>
              <td colspan="8" class="empty-cell">
                <div class="empty-state">
                  <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5"><path d="M2 3h6a4 4 0 0 1 4 4v14a3 3 0 0 0-3-3H2z"/><path d="M22 3h-6a4 4 0 0 0-4 4v14a3 3 0 0 1 3-3h7z"/></svg>
                  <p>{{ searchQuery ? t('tableHierarchy.empty.noMatch') : t('tableHierarchy.empty.noData') }}</p>
                  <span>{{ searchQuery ? t('tableHierarchy.empty.noMatchHint') : t('tableHierarchy.empty.noDataHint') }}</span>
                </div>
              </td>
            </tr>
          </tbody>
        </table>
      </div>

      <div v-if="loading" class="loading-state">
        <div class="spinner"></div>
        <span>{{ t('tableHierarchy.loading') }}</span>
      </div>
    </div>

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
                <option v-for="p in tempProjects" :key="p" :value="p">{{ p }}</option>
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
            <!-- 项目或桌台名称 -->
            <div class="form-group" v-if="formMode === 'project' || formMode === 'table'">
              <label class="form-label required">{{ formMode === 'project' ? t('tableHierarchy.form.projectName') : t('tableHierarchy.form.tableNumber') }}</label>
              <input type="text" v-model="formData.name" :placeholder="formMode === 'project' ? t('tableHierarchy.form.projectPlaceholder') : t('tableHierarchy.form.tablePlaceholder')" class="form-input">
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
                <option v-for="p in tempProjects" :key="p" :value="p">{{ p }}</option>
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
            <div class="form-group">
              <label class="form-label required">
                {{ batchMode === 'project' ? t('tableHierarchy.form.projectName') : batchMode === 'site' ? t('tableHierarchy.form.siteName') : t('tableHierarchy.form.tableNumber') }}
                <span class="label-hint">{{ t('tableHierarchy.form.batchHint') }}</span>
              </label>
              <textarea v-model="batchData.names" class="form-textarea" :placeholder="batchMode === 'project' ? t('tableHierarchy.form.batchProjectPlaceholder') : batchMode === 'site' ? t('tableHierarchy.form.batchSitePlaceholder') : t('tableHierarchy.form.batchTablePlaceholder')" rows="8"></textarea>
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
.hierarchy-config-page {
  padding: 24px;
  max-width: 1400px;
  margin: 0 auto;
}

.page-header {
  display: flex;
  justify-content: space-between;
  align-items: flex-start;
  margin-bottom: 20px;
}

.header-left h2 {
  margin: 0 0 6px;
  font-size: 22px;
  font-weight: 600;
  color: var(--text-primary);
}

.subtitle {
  font-size: 14px;
  color: var(--text-secondary);
}

.header-actions {
  display: flex;
  gap: 12px;
}

.btn {
  display: inline-flex;
  align-items: center;
  gap: 8px;
  padding: 10px 18px;
  border: none;
  border-radius: 10px;
  font-size: 14px;
  font-weight: 500;
  cursor: pointer;
  transition: all 0.2s;
}

.btn svg {
  width: 18px;
  height: 18px;
}

.btn-primary {
  background: linear-gradient(135deg, #3a84ff, #2563eb);
  color: white;
  box-shadow: 0 2px 8px rgba(58, 132, 255, 0.3);
}

.btn-primary:hover {
  background: linear-gradient(135deg, #2b6fd9, #1d4ed8);
  transform: translateY(-1px);
}

.btn-primary:disabled {
  opacity: 0.6;
  cursor: not-allowed;
  transform: none;
}

.btn-secondary {
  background: var(--bg-secondary);
  color: var(--text-primary);
  border: 1px solid var(--border-color);
}

.btn-secondary:hover {
  background: var(--bg-hover);
}

.btn-sm {
  padding: 8px 14px;
  font-size: 13px;
}

.btn-sm svg {
  width: 16px;
  height: 16px;
}

.btn-sm .chevron {
  width: 14px;
  height: 14px;
  margin-left: 2px;
  transition: transform 0.2s;
}

.btn-sm .chevron.rotate {
  transform: rotate(180deg);
}

/* Toolbar */
.toolbar-row {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 16px;
  gap: 16px;
  flex-wrap: wrap;
}

.stats-mini {
  display: flex;
  align-items: center;
  gap: 16px;
  background: var(--bg-card);
  border: 1px solid var(--border-color);
  border-radius: 10px;
  padding: 12px 20px;
}

.stat-item {
  display: flex;
  align-items: baseline;
  gap: 4px;
}

.stat-num {
  font-size: 20px;
  font-weight: 700;
  color: #3a84ff;
}

.stat-text {
  font-size: 13px;
  color: var(--text-secondary);
}

.stat-divider {
  width: 1px;
  height: 24px;
  background: var(--border-color);
}

.toolbar-right {
  display: flex;
  align-items: center;
  gap: 12px;
}

.search-box {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 8px 14px;
  background: var(--bg-secondary);
  border: 1px solid var(--border-color);
  border-radius: 10px;
  width: 280px;
}

.search-box svg {
  width: 18px;
  height: 18px;
  color: var(--text-muted);
  flex-shrink: 0;
}

.search-box input {
  flex: 1;
  border: none;
  background: transparent;
  font-size: 14px;
  color: var(--text-primary);
  outline: none;
}

.search-box input::placeholder {
  color: var(--text-muted);
}

/* 添加下拉菜单 */
.add-dropdown {
  position: relative;
}

.dropdown-menu {
  position: absolute;
  top: calc(100% + 8px);
  right: 0;
  width: 220px;
  background: var(--bg-card);
  border: 1px solid var(--border-color);
  border-radius: 12px;
  box-shadow: 0 10px 40px rgba(0, 0, 0, 0.15);
  opacity: 0;
  visibility: hidden;
  transform: translateY(-8px);
  transition: all 0.2s;
  z-index: 100;
  overflow: hidden;
}

.dropdown-menu.show {
  opacity: 1;
  visibility: visible;
  transform: translateY(0);
}

.dropdown-section {
  padding: 8px;
}

.dropdown-title {
  padding: 8px 12px 6px;
  font-size: 11px;
  font-weight: 600;
  color: var(--text-muted);
  text-transform: uppercase;
  letter-spacing: 0.5px;
}

.dropdown-divider {
  height: 1px;
  background: var(--border-color);
  margin: 0;
}

.dropdown-item {
  display: flex;
  align-items: center;
  gap: 10px;
  width: 100%;
  padding: 10px 12px;
  border: none;
  background: transparent;
  font-size: 14px;
  color: var(--text-primary);
  cursor: pointer;
  border-radius: 8px;
  transition: background 0.15s;
  text-align: left;
}

.dropdown-item:hover {
  background: var(--bg-hover);
}

.dropdown-item .item-icon {
  width: 28px;
  height: 28px;
  display: flex;
  align-items: center;
  justify-content: center;
  border-radius: 6px;
  flex-shrink: 0;
}

.dropdown-item .item-icon svg {
  width: 16px;
  height: 16px;
}

.dropdown-item .item-icon.project {
  background: rgba(58, 132, 255, 0.12);
  color: #3a84ff;
}

.dropdown-item .item-icon.site {
  background: rgba(34, 197, 94, 0.12);
  color: #16a34a;
}

.dropdown-item .item-icon.table {
  background: rgba(168, 85, 247, 0.12);
  color: #9333ea;
}

.dropdown-item .item-icon.game-type {
  background: rgba(245, 158, 11, 0.12);
  color: #f59e0b;
}

/* 主内容 */
.main-content {
  background: var(--bg-card);
  border: 1px solid var(--border-color);
  border-radius: 14px;
  overflow: hidden;
}

.table-wrapper {
  overflow-x: auto;
}

.hierarchy-table {
  width: 100%;
  border-collapse: collapse;
  font-size: 14px;
}

.hierarchy-table th {
  background: var(--bg-secondary);
  padding: 14px 12px;
  text-align: left;
  font-weight: 600;
  color: var(--text-secondary);
  border-bottom: 1px solid var(--border-color);
  white-space: nowrap;
}

.hierarchy-table td {
  padding: 12px;
  border-bottom: 1px solid var(--border-color);
  vertical-align: middle;
}

.hierarchy-table tr:last-child td {
  border-bottom: none;
}

.project-row {
  cursor: pointer;
  background: var(--bg-card);
  transition: background 0.15s;
}

.project-row:hover {
  background: var(--bg-hover);
}

.site-row {
  background: rgba(34, 197, 94, 0.03);
}

.site-row:hover {
  background: rgba(34, 197, 94, 0.06);
}

.table-row {
  background: rgba(168, 85, 247, 0.03);
}

.table-row:hover {
  background: rgba(168, 85, 247, 0.06);
}

.expand-cell {
  width: 40px;
  text-align: center;
}

.expand-cell svg {
  width: 16px;
  height: 16px;
  color: var(--text-muted);
  transition: transform 0.2s;
}

.expand-cell svg.expanded {
  transform: rotate(90deg);
}

.cell-content {
  display: flex;
  align-items: center;
  gap: 8px;
}

.type-badge {
  padding: 3px 8px;
  border-radius: 6px;
  font-size: 11px;
  font-weight: 600;
  flex-shrink: 0;
}

.type-badge.project {
  background: rgba(58, 132, 255, 0.12);
  color: #3a84ff;
}

.type-badge.site {
  background: rgba(34, 197, 94, 0.12);
  color: #16a34a;
}

.type-badge.table {
  background: rgba(168, 85, 247, 0.12);
  color: #9333ea;
}

.game-type-tags {
  display: flex;
  flex-wrap: wrap;
  gap: 4px;
}
.game-type-tag {
  display: inline-block;
  padding: 2px 8px;
  font-size: 11px;
  font-weight: 500;
  background: rgba(16, 185, 129, 0.12);
  color: #059669;
  border-radius: 4px;
}
.cell-muted {
  color: var(--text-muted);
  font-size: 13px;
}

.game-type-checkboxes {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
}
.game-type-checkbox {
  display: flex;
  align-items: center;
  gap: 6px;
  padding: 6px 12px;
  border: 1px solid var(--border-color);
  border-radius: 6px;
  cursor: pointer;
  transition: all 0.15s;
  font-size: 13px;
}
.game-type-checkbox:hover {
  border-color: var(--primary);
  background: rgba(58, 132, 255, 0.05);
}
.game-type-checkbox.checked {
  border-color: var(--primary);
  background: rgba(58, 132, 255, 0.1);
}
.game-type-checkbox .checkbox-icon {
  width: 16px;
  height: 16px;
  border: 2px solid var(--border-color);
  border-radius: 4px;
  display: flex;
  align-items: center;
  justify-content: center;
  transition: all 0.15s;
}
.game-type-checkbox.checked .checkbox-icon {
  background: var(--primary);
  border-color: var(--primary);
}
.game-type-checkbox .checkbox-icon svg {
  width: 10px;
  height: 10px;
}

.count-badge {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  min-width: 28px;
  height: 24px;
  padding: 0 8px;
  border-radius: 12px;
  font-size: 12px;
  font-weight: 600;
}

.count-badge.site-count {
  background: rgba(34, 197, 94, 0.12);
  color: #16a34a;
}

.count-badge.table-count {
  background: rgba(168, 85, 247, 0.12);
  color: #9333ea;
}

.project-name span,
.site-name span,
.table-name span {
  font-weight: 500;
  color: var(--text-primary);
}

.action-btns {
  display: flex;
  gap: 6px;
}

.action-btn {
  width: 30px;
  height: 30px;
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

.action-btn svg {
  width: 14px;
  height: 14px;
}

.action-btn.add:hover {
  border-color: #22c55e;
  color: #22c55e;
  background: rgba(34, 197, 94, 0.08);
}

.action-btn.edit:hover {
  border-color: #3a84ff;
  color: #3a84ff;
  background: rgba(58, 132, 255, 0.08);
}

.action-btn.danger:hover {
  border-color: #ef4444;
  color: #ef4444;
  background: rgba(239, 68, 68, 0.08);
}

.empty-cell {
  padding: 60px 24px !important;
}

.empty-state {
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  color: var(--text-muted);
}

.empty-state svg {
  width: 64px;
  height: 64px;
  opacity: 0.3;
  margin-bottom: 16px;
}

.empty-state p {
  margin: 0 0 8px;
  font-size: 16px;
  font-weight: 500;
  color: var(--text-secondary);
}

.empty-state span {
  font-size: 14px;
}

.loading-state {
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  padding: 60px 24px;
  color: var(--text-muted);
}

.spinner {
  width: 40px;
  height: 40px;
  border: 3px solid var(--border-color);
  border-top-color: #3a84ff;
  border-radius: 50%;
  animation: spin 0.8s linear infinite;
  margin-bottom: 16px;
}

@keyframes spin {
  to { transform: rotate(360deg); }
}

/* 弹窗样式 */
.modal-overlay {
  position: fixed;
  inset: 0;
  background: rgba(0, 0, 0, 0.5);
  display: flex;
  align-items: center;
  justify-content: center;
  z-index: 1000;
  opacity: 0;
  pointer-events: none;
  transition: opacity 0.2s;
}

.modal-overlay.active {
  opacity: 1;
  pointer-events: auto;
}

.modal {
  background: var(--bg-card);
  border-radius: 16px;
  box-shadow: 0 20px 50px rgba(0, 0, 0, 0.2);
  max-width: 90vw;
  transform: scale(0.95);
  transition: transform 0.2s;
}

.modal-overlay.active .modal {
  transform: scale(1);
}

.form-modal {
  width: 480px;
}

.batch-modal {
  width: 520px;
}

.modal-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 20px 24px;
  border-bottom: 1px solid var(--border-color);
}

.modal-title {
  font-size: 18px;
  font-weight: 600;
  color: var(--text-primary);
}

.modal-close {
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
  transition: all 0.15s;
}

.modal-close:hover {
  background: var(--bg-secondary);
  color: var(--text-primary);
}

.modal-close svg {
  width: 20px;
  height: 20px;
}

.modal-body {
  padding: 24px;
}

.form-group {
  margin-bottom: 20px;
}

.form-group:last-child {
  margin-bottom: 0;
}

.form-label {
  display: block;
  margin-bottom: 8px;
  font-size: 14px;
  font-weight: 500;
  color: var(--text-primary);
}

.form-label.required::after {
  content: ' *';
  color: #ef4444;
}

.label-hint {
  font-weight: 400;
  color: var(--text-muted);
  font-size: 12px;
}

.form-input {
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

.form-input:focus {
  outline: none;
  border-color: #3a84ff;
  box-shadow: 0 0 0 3px rgba(58, 132, 255, 0.12);
}

.form-input::placeholder {
  color: var(--text-muted);
}

.form-textarea {
  width: 100%;
  padding: 12px 16px;
  border: 1px solid var(--border-color);
  border-radius: 10px;
  font-size: 14px;
  font-family: inherit;
  background: var(--bg-secondary);
  color: var(--text-primary);
  transition: all 0.2s;
  box-sizing: border-box;
  resize: vertical;
  min-height: 120px;
}

.form-textarea:focus {
  outline: none;
  border-color: #3a84ff;
  box-shadow: 0 0 0 3px rgba(58, 132, 255, 0.12);
}

.form-textarea::placeholder {
  color: var(--text-muted);
}

.modal-footer {
  display: flex;
  justify-content: flex-end;
  align-items: center;
  gap: 12px;
  padding: 16px 24px;
  border-top: 1px solid var(--border-color);
  background: var(--bg-secondary);
  border-radius: 0 0 16px 16px;
}


@media (max-width: 768px) {
  .toolbar-row {
    flex-direction: column;
    align-items: stretch;
  }
  
  .toolbar-right {
    flex-direction: column;
  }
  
  .search-box {
    width: 100%;
  }
  
  .page-header {
    flex-direction: column;
    gap: 16px;
  }
  
  .header-actions {
    width: 100%;
  }
  
  .header-actions .btn {
    flex: 1;
  }
}
</style>
