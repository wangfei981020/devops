<script setup>
import { ref, computed, onMounted } from 'vue'
import api from '@/api'
import { useAppStore, useAuthStore } from '@/stores'

const appStore = useAppStore()
const authStore = useAuthStore()
const t = (k, p) => appStore.t(k, p)
const isSuperAdmin = computed(() => authStore.isSuperAdmin())
const canSourceCreate = computed(() => isSuperAdmin.value || authStore.hasPermission('table_management:source_create'))
const canSourceUpdate = computed(() => isSuperAdmin.value || authStore.hasPermission('table_management:source_update'))
const canSourceDelete = computed(() => isSuperAdmin.value || authStore.hasPermission('table_management:source_delete'))
const canSync = computed(() => isSuperAdmin.value || authStore.hasPermission('table_management:sync'))
const canAliasUpdate = computed(() => isSuperAdmin.value || authStore.hasPermission('table_management:alias_update'))

const activeTab = ref('sources')   // sources / rooms / aliases

// ===== 全局开关：维护记录数据源 =====
// system_settings.table_maint_data_source = 'hierarchy' (默认) / 'external'
const dataSourceSwitch = ref('hierarchy')
const switchSaving = ref(false)

async function loadDataSourceSwitch() {
  try {
    const res = await api.get('/api/settings?key=table_maint_data_source')
    const v = res.data?.table_maint_data_source || 'hierarchy'
    dataSourceSwitch.value = (v === 'external') ? 'external' : 'hierarchy'
  } catch (e) {
    dataSourceSwitch.value = 'hierarchy'
  }
}

async function toggleDataSource() {
  if (switchSaving.value) return
  const newVal = dataSourceSwitch.value === 'external' ? 'hierarchy' : 'external'

  const confirmed = await appStore.showConfirm({
    type: 'warning',
    title: newVal === 'external' ? '切换到「桌台管理」数据源' : '切回「桌台配置」数据源',
    message: newVal === 'external'
      ? '切换后：桌台维护记录的桌台/现场/项目下拉将从「桌台管理」这里同步过来的数据拿。\n\n请先确认：\n  1. 已经配好至少一个数据源并同步过\n  2. 同步过来的桌台数据没问题'
      : '切换回老的「桌台配置」作为维护记录的数据源吗？',
    okText: '确定切换',
    cancelText: '取消'
  })
  if (!confirmed) return

  switchSaving.value = true
  try {
    await api.post('/api/settings', { table_maint_data_source: newVal })
    dataSourceSwitch.value = newVal
    appStore.showToast(
      newVal === 'external'
        ? '已切换到「桌台管理」数据源。打开桌台维护记录页刷新生效。'
        : '已切回「桌台配置」数据源。',
      'success'
    )
  } catch (e) {
    appStore.showToast('切换失败: ' + (e.response?.data?.error || e.message), 'error')
  } finally {
    switchSaving.value = false
  }
}

// ===== 数据源 =====
const sources = ref([])
const sourcesLoading = ref(false)
const showSourceForm = ref(false)
const editingSource = ref(null)
// 内部字段名清单（field_map 的 key 固定为这些）
const INTERNAL_FIELDS = [
  { key: 'platform_id',      label: '现场 ID',     hint: '外部主键，唯一标识现场' },
  { key: 'platform_name',    label: '现场 code',   hint: '外部英文代号/拼音' },
  { key: 'platform_name_zh', label: '现场中文名',  hint: '外部直接返回的中文名' },
  { key: 'room_id',          label: '桌台号',      hint: '桌台唯一 ID' },
  { key: 'game_type',        label: '游戏类型 code', hint: '英文代号，如 BAC / DT' },
  { key: 'game_type_name',   label: '游戏类型中文', hint: '可选；外部直接给中文名时填，自动注册到别名' },
  { key: 'room_status',      label: '状态字段',    hint: '原始状态值，按 status_map 映射' }
]

// 默认 field_map / status_map（项目 A "platformId" 那种风格）
function defaultFieldMap() {
  return {
    platform_id: 'platformId',
    platform_name: 'platformName',
    platform_name_zh: 'platformNameZh',
    room_id: 'roomId',
    game_type: 'gameType',
    game_type_name: '',
    room_status: 'roomStatus'
  }
}
function defaultStatusMap() {
  return {
    enabled: ['0', '2', 'Enable', 'enable'],
    disabled: ['1', 'Disable', 'disable']
  }
}

const sourceForm = ref({
  project: '',
  url: '',
  method: 'POST',
  request_body: '{\n  "operator": "opsplatform",\n  "status": 0,\n  "roomIds": []\n}',
  data_path: 'data.data',
  field_map: defaultFieldMap(),
  status_map: defaultStatusMap()
})
// status_map UI 编辑用：把数组转成逗号分隔字符串
const statusEnabledStr = ref('')
const statusDisabledStr = ref('')
function syncStatusMapFromForm() {
  statusEnabledStr.value = (sourceForm.value.status_map.enabled || []).join(', ')
  statusDisabledStr.value = (sourceForm.value.status_map.disabled || []).join(', ')
}
function syncStatusMapToForm() {
  sourceForm.value.status_map = {
    enabled: statusEnabledStr.value.split(',').map(s => s.trim()).filter(Boolean),
    disabled: statusDisabledStr.value.split(',').map(s => s.trim()).filter(Boolean)
  }
}

const testing = ref(false)
const testResult = ref(null)

async function loadSources() {
  sourcesLoading.value = true
  try {
    const res = await api.get('/api/external-data-sources')
    sources.value = Array.isArray(res.data) ? res.data : []
  } catch (e) {
    appStore.showToast('加载数据源失败: ' + (e.response?.data?.error || e.message), 'error')
  } finally {
    sourcesLoading.value = false
  }
}

function openCreateSource() {
  editingSource.value = null
  sourceForm.value = {
    project: '',
    url: '',
    method: 'POST',
    request_body: '{\n  "operator": "opsplatform",\n  "status": 0,\n  "roomIds": []\n}',
    data_path: 'data.data',
    field_map: defaultFieldMap(),
    status_map: defaultStatusMap()
  }
  syncStatusMapFromForm()
  testResult.value = null
  showSourceForm.value = true
}

function openEditSource(s) {
  editingSource.value = s
  sourceForm.value = {
    project: s.project,
    url: s.url,
    method: s.method || 'POST',
    request_body: s.request_body || '',
    data_path: s.data_path || 'data.data',
    field_map: { ...(s.field_map || defaultFieldMap()) },
    status_map: { enabled: [...(s.status_map?.enabled || [])], disabled: [...(s.status_map?.disabled || [])] }
  }
  // 确保所有 INTERNAL_FIELDS 都有 key（兼容老数据）
  for (const f of INTERNAL_FIELDS) {
    if (!(f.key in sourceForm.value.field_map)) sourceForm.value.field_map[f.key] = ''
  }
  syncStatusMapFromForm()
  testResult.value = null
  showSourceForm.value = true
}

async function testConnection() {
  if (!sourceForm.value.url) {
    appStore.showToast('请先填 URL', 'warning')
    return
  }
  syncStatusMapToForm()
  testing.value = true
  testResult.value = null
  try {
    const res = await api.post('/api/external-data-sources/test', {
      url: sourceForm.value.url,
      method: sourceForm.value.method,
      request_body: sourceForm.value.request_body,
      data_path: sourceForm.value.data_path,
      field_map: sourceForm.value.field_map,
      status_map: sourceForm.value.status_map
    })
    testResult.value = res.data
  } catch (e) {
    testResult.value = { success: false, error: e.response?.data?.error || e.message }
  } finally {
    testing.value = false
  }
}

async function saveSource() {
  if (!sourceForm.value.project || !sourceForm.value.url) {
    appStore.showToast('项目和 URL 必填', 'warning')
    return
  }
  syncStatusMapToForm()
  try {
    if (editingSource.value) {
      await api.put(`/api/external-data-sources/${editingSource.value.id}`, {
        url: sourceForm.value.url,
        method: sourceForm.value.method,
        request_body: sourceForm.value.request_body,
        data_path: sourceForm.value.data_path,
        field_map: sourceForm.value.field_map,
        status_map: sourceForm.value.status_map
      })
    } else {
      await api.post('/api/external-data-sources', sourceForm.value)
    }
    appStore.showToast('保存成功', 'success')
    showSourceForm.value = false
    await loadSources()
  } catch (e) {
    appStore.showToast('保存失败: ' + (e.response?.data?.error || e.message), 'error')
  }
}

async function deleteSource(s) {
  const confirmed = await appStore.showConfirm({
    type: 'danger',
    title: '删除数据源',
    message: `确定删除项目 "${s.project}" 的数据源吗？\n\n该项目下的同步桌台也会一并清除。`,
    okText: '删除',
    cancelText: '取消'
  })
  if (!confirmed) return
  try {
    await api.delete(`/api/external-data-sources/${s.id}`)
    appStore.showToast('删除成功', 'success')
    await loadSources()
    await loadRooms()
  } catch (e) {
    appStore.showToast('删除失败: ' + (e.response?.data?.error || e.message), 'error')
  }
}

async function syncSource(s) {
  try {
    const res = await api.post(`/api/external-data-sources/${s.id}/sync`)
    if (res.data?.success) {
      const { added, updated, deleted } = res.data
      appStore.showToast(`同步成功：新增 ${added}，更新 ${updated}，下线 ${deleted}`, 'success')
      await loadSources()
      if (activeTab.value === 'rooms') await loadRooms()
    } else {
      appStore.showToast('同步失败: ' + (res.data?.error || '未知错误'), 'error')
    }
  } catch (e) {
    appStore.showToast('同步失败: ' + (e.response?.data?.error || e.message), 'error')
  }
}

async function syncAll() {
  if (!sources.value.length) {
    appStore.showToast('暂无数据源', 'warning')
    return
  }
  let ok = 0, fail = 0
  for (const s of sources.value) {
    if (!s.enabled) continue
    try {
      const res = await api.post(`/api/external-data-sources/${s.id}/sync`)
      if (res.data?.success) ok++; else fail++
    } catch { fail++ }
  }
  appStore.showToast(`全部同步完成：成功 ${ok}，失败 ${fail}`, ok > 0 ? 'success' : 'error')
  await loadSources()
  if (activeTab.value === 'rooms') await loadRooms()
}

// ===== 自动桌台清单 =====
const rooms = ref([])
const aliases = ref([])
const roomsLoading = ref(false)
const filterProject = ref('')
const filterStatus = ref('')
const filterRoomId = ref('')

async function loadRooms() {
  roomsLoading.value = true
  try {
    const params = new URLSearchParams()
    if (filterProject.value) params.set('project', filterProject.value)
    const res = await api.get(`/api/external-rooms?${params}`)
    rooms.value = Array.isArray(res.data) ? res.data : []
  } catch (e) {
    rooms.value = []
  } finally {
    roomsLoading.value = false
  }
}

const filteredRooms = computed(() => {
  let list = rooms.value
  if (filterStatus.value) list = list.filter(r => r.status === filterStatus.value)
  if (filterRoomId.value) {
    const q = filterRoomId.value.toLowerCase()
    list = list.filter(r => r.room_id.toLowerCase().includes(q))
  }
  return list
})

function platformDisplay(r) {
  // 优先 alias.name_zh，再 platform_name_zh，再 platform_name code
  const a = aliases.value.find(x => x.alias_type === 'platform' && x.code === r.platform_name)
  if (a && a.name_zh) return a.name_zh
  return r.platform_name_zh || r.platform_name
}

function gameTypeDisplay(code) {
  const a = aliases.value.find(x => x.alias_type === 'gameType' && x.code === code)
  return a && a.name_zh ? a.name_zh : code
}

const roomsStats = computed(() => {
  const total = filteredRooms.value.length
  const enabled = filteredRooms.value.filter(r => r.status === 'enabled').length
  const disabled = filteredRooms.value.filter(r => r.status === 'disabled').length
  return { total, enabled, disabled }
})

// ===== 别名 =====
const aliasesLoading = ref(false)
const gameTypeAliasEdits = ref({})   // code -> 编辑中的 name_zh

async function loadAliases() {
  aliasesLoading.value = true
  try {
    const res = await api.get('/api/external-aliases')
    aliases.value = Array.isArray(res.data) ? res.data : []
  } catch (e) {
    aliases.value = []
  } finally {
    aliasesLoading.value = false
  }
}

// 从 rooms 里抽出去重的 gameType 列表（用于别名编辑）
const gameTypeCodes = computed(() => {
  const set = new Set()
  rooms.value.forEach(r => { if (r.game_type) set.add(r.game_type) })
  return Array.from(set).sort()
})

const platformCodes = computed(() => {
  const map = new Map()  // code → defaultNameZh（外部返回的中文，作为默认提示）
  rooms.value.forEach(r => {
    if (r.platform_name && !map.has(r.platform_name)) {
      map.set(r.platform_name, r.platform_name_zh || '')
    }
  })
  return Array.from(map.entries()).sort((a, b) => a[0].localeCompare(b[0]))
})

function getAliasValue(type, code) {
  const a = aliases.value.find(x => x.alias_type === type && x.code === code)
  return a ? a.name_zh : ''
}

const platformAliasEdits = ref({})

async function saveAlias(type, code, name_zh) {
  try {
    await api.put('/api/external-aliases', { alias_type: type, code, name_zh })
    appStore.showToast('已保存', 'success')
    await loadAliases()
  } catch (e) {
    appStore.showToast('保存失败: ' + (e.response?.data?.error || e.message), 'error')
  }
}

function statusTag(status) {
  if (status === 'enabled') return { label: '启用', cls: 'tag-enabled' }
  if (status === 'disabled') return { label: '关闭', cls: 'tag-disabled' }
  return { label: status, cls: 'tag-disabled' }
}

function fmt(dt) {
  if (!dt) return '-'
  return String(dt).replace('T', ' ').slice(0, 19)
}

onMounted(async () => {
  await loadDataSourceSwitch()
  await loadAliases()
  await loadSources()
  await loadRooms()
})
</script>

<template>
  <div class="table-mgmt-page">
    <div class="page-header">
      <h2>{{ t('tableManagement.title') }}</h2>
      <div class="header-actions">
        <button v-if="canSync" class="btn btn-secondary" @click="syncAll">
          🔄 {{ t('tableManagement.syncAll') }}
        </button>
      </div>
    </div>

    <!-- 维护记录数据源开关 -->
    <div class="data-source-switch" :class="{ 'using-external': dataSourceSwitch === 'external' }">
      <div class="dss-left">
        <div class="dss-icon">⚙️</div>
        <div>
          <div class="dss-title">桌台维护记录数据源</div>
          <div class="dss-hint">
            <template v-if="dataSourceSwitch === 'external'">
              <strong>当前：桌台管理</strong>（用本菜单同步过来的桌台作为维护记录下拉来源）
            </template>
            <template v-else>
              <strong>当前：桌台配置</strong>（老菜单的手动配置）—— 验收 OK 后切换到本菜单
            </template>
          </div>
        </div>
      </div>
      <button class="btn-switch" :class="{ on: dataSourceSwitch === 'external' }" @click="toggleDataSource" :disabled="switchSaving">
        <span class="switch-track"><span class="switch-thumb"></span></span>
        <span class="switch-label">{{ dataSourceSwitch === 'external' ? '✓ 已切换' : '切换到桌台管理' }}</span>
      </button>
    </div>

    <!-- Tabs -->
    <div class="tabs">
      <button class="tab" :class="{ active: activeTab === 'sources' }" @click="activeTab = 'sources'">
        📦 {{ t('tableManagement.tabSources') }} ({{ sources.length }})
      </button>
      <button class="tab" :class="{ active: activeTab === 'rooms' }" @click="activeTab = 'rooms'">
        🏠 {{ t('tableManagement.tabRooms') }} ({{ rooms.length }})
      </button>
      <button class="tab" :class="{ active: activeTab === 'aliases' }" @click="activeTab = 'aliases'">
        🏷️ {{ t('tableManagement.tabAliases') }}
      </button>
    </div>

    <!-- Tab: 数据源 -->
    <div v-if="activeTab === 'sources'" class="tab-content">
      <div class="action-bar">
        <button v-if="canSourceCreate" class="btn btn-primary" @click="openCreateSource">
          + {{ t('tableManagement.addSource') }}
        </button>
        <span class="hint">{{ t('tableManagement.scheduleHint') }}</span>
      </div>

      <div class="data-table-wrap">
        <table class="data-table">
          <thead>
            <tr>
              <th>项目</th>
              <th>URL</th>
              <th>启用</th>
              <th>上次同步</th>
              <th>状态</th>
              <th>桌台数</th>
              <th>操作</th>
            </tr>
          </thead>
          <tbody>
            <tr v-if="!sourcesLoading && !sources.length"><td colspan="7" class="empty">暂无数据源，点击上方"+ 添加数据源"开始配置</td></tr>
            <tr v-for="s in sources" :key="s.id">
              <td><strong>{{ s.project }}</strong></td>
              <td class="url-cell" :title="s.url">{{ s.url }}</td>
              <td>
                <span :class="s.enabled ? 'tag-enabled' : 'tag-disabled'">
                  {{ s.enabled ? '已启用' : '已禁用' }}
                </span>
              </td>
              <td>{{ fmt(s.last_synced_at) }}</td>
              <td>
                <span v-if="s.last_sync_status === 'success'" class="tag-enabled">成功</span>
                <span v-else-if="s.last_sync_status === 'failed'" class="tag-failed" :title="s.last_sync_error">失败</span>
                <span v-else class="tag-disabled">未同步</span>
              </td>
              <td><strong>{{ s.last_sync_count }}</strong></td>
              <td class="actions">
                <button v-if="canSync" class="btn-mini" @click="syncSource(s)">立即同步</button>
                <button v-if="canSourceUpdate" class="btn-mini" @click="openEditSource(s)">编辑</button>
                <button v-if="canSourceDelete" class="btn-mini danger" @click="deleteSource(s)">删除</button>
              </td>
            </tr>
          </tbody>
        </table>
      </div>
    </div>

    <!-- Tab: 自动桌台清单 -->
    <div v-if="activeTab === 'rooms'" class="tab-content">
      <div class="filter-bar">
        <label>项目: <select v-model="filterProject" @change="loadRooms">
          <option value="">全部</option>
          <option v-for="s in sources" :key="s.project" :value="s.project">{{ s.project }}</option>
        </select></label>
        <label>状态: <select v-model="filterStatus">
          <option value="">全部</option>
          <option value="enabled">启用</option>
          <option value="disabled">关闭</option>
        </select></label>
        <label>桌台号: <input v-model="filterRoomId" placeholder="搜索..."></label>
        <span class="stats">共 <strong>{{ roomsStats.total }}</strong> 张 · 启用 {{ roomsStats.enabled }} / 关闭 {{ roomsStats.disabled }}</span>
        <button class="btn-mini" @click="loadRooms">🔄 刷新</button>
      </div>

      <div class="data-table-wrap">
        <table class="data-table">
          <thead>
            <tr>
              <th>项目</th>
              <th>现场 (code)</th>
              <th>现场 (中文)</th>
              <th>桌台号</th>
              <th>游戏类型</th>
              <th>状态</th>
              <th>上次同步</th>
            </tr>
          </thead>
          <tbody>
            <tr v-if="!roomsLoading && !filteredRooms.length"><td colspan="7" class="empty">暂无桌台数据。先配置数据源并同步。</td></tr>
            <tr v-for="r in filteredRooms" :key="r.id">
              <td>{{ r.project }}</td>
              <td><code class="code-cell">{{ r.platform_name }}</code></td>
              <td>{{ platformDisplay(r) }}</td>
              <td><strong>{{ r.room_id }}</strong></td>
              <td>
                <span class="gametype-tag">
                  <code class="code-cell">{{ r.game_type }}</code>
                  <span v-if="gameTypeDisplay(r.game_type) !== r.game_type" class="zh-name">{{ gameTypeDisplay(r.game_type) }}</span>
                </span>
              </td>
              <td>
                <span :class="statusTag(r.status).cls">{{ statusTag(r.status).label }}</span>
                <span v-if="r.room_status === 2" class="maintenance-hint" title="外部 roomStatus=2 维护中，归类为启用">·维护中</span>
              </td>
              <td>{{ fmt(r.synced_at) }}</td>
            </tr>
          </tbody>
        </table>
      </div>
    </div>

    <!-- Tab: 别名 -->
    <div v-if="activeTab === 'aliases'" class="tab-content">
      <div class="alias-section">
        <h3>🎮 游戏类型别名</h3>
        <p class="hint">编辑下面的中文名后，所有项目下的同名游戏类型自动显示中文。留空 = 显示原 code。</p>
        <table class="data-table">
          <thead>
            <tr><th>英文代号</th><th>中文别名</th><th>使用次数</th><th>操作</th></tr>
          </thead>
          <tbody>
            <tr v-if="!gameTypeCodes.length"><td colspan="4" class="empty">同步桌台后，这里会列出所有出现的游戏类型代号</td></tr>
            <tr v-for="code in gameTypeCodes" :key="code">
              <td><code class="code-cell">{{ code }}</code></td>
              <td>
                <input v-model="gameTypeAliasEdits[code]" :placeholder="getAliasValue('gameType', code) || code" :disabled="!canAliasUpdate">
              </td>
              <td>{{ rooms.filter(r => r.game_type === code).length }}</td>
              <td>
                <button v-if="canAliasUpdate" class="btn-mini" @click="saveAlias('gameType', code, gameTypeAliasEdits[code] || ''); gameTypeAliasEdits[code] = ''">保存</button>
                <span v-if="getAliasValue('gameType', code)" class="current-alias">→ {{ getAliasValue('gameType', code) }}</span>
              </td>
            </tr>
          </tbody>
        </table>
      </div>

      <div class="alias-section">
        <h3>🏛️ 现场别名（可选，覆盖外部返回的中文）</h3>
        <p class="hint">外部 API 已经返回 platformNameZh 时，这里可以再覆盖一层。一般不需要改。</p>
        <table class="data-table">
          <thead>
            <tr><th>英文代号</th><th>外部中文</th><th>覆盖别名</th><th>操作</th></tr>
          </thead>
          <tbody>
            <tr v-if="!platformCodes.length"><td colspan="4" class="empty">同步桌台后这里会列出所有现场代号</td></tr>
            <tr v-for="[code, defaultZh] in platformCodes" :key="code">
              <td><code class="code-cell">{{ code }}</code></td>
              <td>{{ defaultZh || '-' }}</td>
              <td>
                <input v-model="platformAliasEdits[code]" :placeholder="getAliasValue('platform', code) || ''" :disabled="!canAliasUpdate">
              </td>
              <td>
                <button v-if="canAliasUpdate" class="btn-mini" @click="saveAlias('platform', code, platformAliasEdits[code] || ''); platformAliasEdits[code] = ''">保存</button>
                <span v-if="getAliasValue('platform', code)" class="current-alias">→ {{ getAliasValue('platform', code) }}</span>
              </td>
            </tr>
          </tbody>
        </table>
      </div>
    </div>

    <!-- 添加/编辑数据源弹窗 -->
    <Teleport to="body">
      <div class="modal-overlay" :class="{ active: showSourceForm }">
        <div class="modal modal-wide">
          <div class="modal-header">
            <div class="modal-title">{{ editingSource ? '编辑数据源' : '添加数据源' }}</div>
            <button class="modal-close" @click="showSourceForm = false">×</button>
          </div>
          <div class="modal-body">

            <!-- 基础配置 -->
            <div class="form-row">
              <div class="form-group">
                <label class="form-label required">项目</label>
                <input v-model="sourceForm.project" :disabled="!!editingSource" placeholder="例：G01" class="form-input">
                <span v-if="editingSource" class="form-hint">项目创建后不可修改</span>
              </div>
              <div class="form-group">
                <label class="form-label required">HTTP 方法</label>
                <select v-model="sourceForm.method" class="form-input">
                  <option value="POST">POST</option>
                  <option value="GET">GET</option>
                </select>
              </div>
            </div>
            <div class="form-group">
              <label class="form-label required">API 接口地址</label>
              <input v-model="sourceForm.url" placeholder="https://op-office-inner.g01-uat.com/openapi/room/list" class="form-input">
            </div>
            <div class="form-group" v-if="sourceForm.method === 'POST'">
              <label class="form-label">请求 Body (JSON)</label>
              <textarea v-model="sourceForm.request_body" rows="6" class="form-textarea code-area" placeholder='{"operator":"opsplatform","status":0,"roomIds":[]}'></textarea>
            </div>
            <div class="form-group">
              <label class="form-label required">响应数组路径 data_path</label>
              <input v-model="sourceForm.data_path" placeholder="如 data / data.data / data.list" class="form-input">
              <span class="form-hint">点分路径，从 JSON 根开始找到桌台数组所在</span>
            </div>

            <!-- 字段映射 -->
            <div class="section-title">📋 字段映射（外部字段名 → 内部字段）</div>
            <div class="field-map-grid">
              <div v-for="f in INTERNAL_FIELDS" :key="f.key" class="field-map-row">
                <label>
                  <div class="fm-label">{{ f.label }}</div>
                  <div class="fm-hint">{{ f.hint }}</div>
                </label>
                <span class="fm-arrow">←</span>
                <input v-model="sourceForm.field_map[f.key]" :placeholder="'外部字段名，如 ' + (f.key === 'platform_id' ? 'platformId / gamePlatformCode' : '')" class="form-input fm-input">
              </div>
            </div>

            <!-- 状态值映射 -->
            <div class="section-title">🚦 状态值映射（外部 status 原值 → 内部状态）</div>
            <div class="form-row">
              <div class="form-group">
                <label class="form-label">enabled（启用）<span class="form-hint">逗号分隔</span></label>
                <input v-model="statusEnabledStr" placeholder='0, 2, Enable, enable' class="form-input">
              </div>
              <div class="form-group">
                <label class="form-label">disabled（关闭）<span class="form-hint">逗号分隔</span></label>
                <input v-model="statusDisabledStr" placeholder='1, Disable, disable' class="form-input">
              </div>
            </div>

            <!-- 测试连接 -->
            <div class="form-group">
              <button class="btn btn-secondary" @click="testConnection" :disabled="testing">
                {{ testing ? '测试中...' : '🔌 测试连接 + 验证字段映射' }}
              </button>

              <div v-if="testResult" class="test-result" :class="testResult.success ? 'ok' : 'fail'">
                <div v-if="testResult.success">
                  ✅ 成功！返回 <strong>{{ testResult.total }}</strong> 条桌台。下面是前 5 条的双面对比，看右侧解析是否符合预期：
                  <div class="preview-grid">
                    <div class="preview-col">
                      <div class="preview-title">外部原始 JSON</div>
                      <pre class="preview-content">{{ JSON.stringify(testResult.raw, null, 2) }}</pre>
                    </div>
                    <div class="preview-col">
                      <div class="preview-title">按字段映射解析后</div>
                      <pre class="preview-content">{{ JSON.stringify(testResult.parsed, null, 2) }}</pre>
                    </div>
                  </div>
                </div>
                <div v-else>
                  ❌ 失败：{{ testResult.error }}
                  <details v-if="testResult.root_sample" style="margin-top:8px;">
                    <summary style="cursor:pointer;font-size:12px;">查看返回的原始 JSON（前 1000 字节）</summary>
                    <pre class="preview-content">{{ JSON.stringify(testResult.root_sample, null, 2).slice(0, 1000) }}</pre>
                  </details>
                </div>
              </div>
            </div>
          </div>
          <div class="modal-footer">
            <button class="btn btn-secondary" @click="showSourceForm = false">取消</button>
            <button class="btn btn-primary" @click="saveSource">保存</button>
          </div>
        </div>
      </div>
    </Teleport>
  </div>
</template>

<style scoped>
.table-mgmt-page { padding: 20px; }
.page-header { display: flex; justify-content: space-between; align-items: center; margin-bottom: 16px; }
.page-header h2 { margin: 0; font-size: 20px; font-weight: 600; }

/* 数据源切换开关 */
.data-source-switch { display: flex; justify-content: space-between; align-items: center; padding: 12px 20px; margin-bottom: 16px; background: var(--bg-card); border: 1px solid var(--border-color); border-left: 4px solid #9ca3af; border-radius: 8px; }
.data-source-switch.using-external { border-left-color: #3a84ff; background: rgba(58, 132, 255, 0.04); }
.dss-left { display: flex; align-items: center; gap: 12px; }
.dss-icon { font-size: 20px; }
.dss-title { font-size: 14px; font-weight: 600; color: var(--text-primary); }
.dss-hint { font-size: 12px; color: var(--text-secondary); margin-top: 2px; }
.dss-hint strong { color: var(--text-primary); }
.btn-switch { display: inline-flex; align-items: center; gap: 8px; padding: 6px 14px; border: 1px solid var(--border-color); background: var(--bg-card); border-radius: 6px; cursor: pointer; font-size: 13px; color: var(--text-primary); }
.btn-switch:hover { border-color: #3a84ff; }
.btn-switch:disabled { opacity: 0.5; cursor: not-allowed; }
.btn-switch.on { background: #3a84ff; color: white; border-color: #3a84ff; }
.switch-track { width: 32px; height: 18px; background: #d1d5db; border-radius: 9px; position: relative; transition: background 0.2s; }
.btn-switch.on .switch-track { background: rgba(255,255,255,0.4); }
.switch-thumb { position: absolute; top: 2px; left: 2px; width: 14px; height: 14px; background: white; border-radius: 50%; transition: left 0.2s; }
.btn-switch.on .switch-thumb { left: 16px; background: white; }

.tabs { display: flex; border-bottom: 2px solid var(--border-color); margin-bottom: 20px; gap: 4px; }
.tab { padding: 10px 20px; border: none; background: transparent; cursor: pointer; font-size: 14px; color: var(--text-secondary); border-bottom: 2px solid transparent; margin-bottom: -2px; }
.tab.active { color: #3a84ff; border-bottom-color: #3a84ff; font-weight: 600; }
.tab:hover { background: var(--bg-secondary); }

.tab-content { background: var(--bg-card); border-radius: 12px; padding: 20px; }

.action-bar { display: flex; align-items: center; gap: 16px; margin-bottom: 16px; }
.action-bar .hint { color: var(--text-muted); font-size: 13px; }

.filter-bar { display: flex; align-items: center; gap: 16px; flex-wrap: wrap; margin-bottom: 16px; padding: 12px 16px; background: var(--bg-secondary); border-radius: 8px; }
.filter-bar label { display: flex; align-items: center; gap: 6px; font-size: 13px; color: var(--text-secondary); }
.filter-bar select, .filter-bar input { padding: 6px 10px; border: 1px solid var(--border-color); border-radius: 6px; background: var(--bg-card); }
.filter-bar .stats { margin-left: auto; font-size: 13px; color: var(--text-secondary); }
.filter-bar .stats strong { color: #3a84ff; font-size: 16px; }

.data-table-wrap { overflow-x: auto; }
.data-table { width: 100%; border-collapse: collapse; }
.data-table th, .data-table td { padding: 10px 12px; text-align: left; font-size: 14px; border-bottom: 1px solid var(--border-color); }
.data-table th { background: var(--bg-secondary); color: var(--text-secondary); font-weight: 600; font-size: 13px; }
.data-table tbody tr:hover { background: var(--bg-hover, rgba(0,0,0,0.02)); }
.data-table .empty { text-align: center; color: var(--text-muted); padding: 40px; }
.url-cell { max-width: 320px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; color: var(--text-secondary); font-family: monospace; font-size: 12px; }
.code-cell { display: inline-block; padding: 1px 6px; background: rgba(0,0,0,0.04); border-radius: 4px; font-family: monospace; font-size: 12px; color: var(--text-primary); }
.gametype-tag { display: inline-flex; align-items: center; gap: 6px; }
.zh-name { color: var(--text-primary); font-weight: 500; }
.maintenance-hint { font-size: 11px; color: #f59e0b; margin-left: 4px; }

.actions { display: flex; gap: 4px; }
.btn-mini { padding: 4px 10px; font-size: 12px; border: 1px solid var(--border-color); background: var(--bg-card); border-radius: 4px; cursor: pointer; color: var(--text-primary); }
.btn-mini:hover { background: var(--bg-secondary); border-color: #3a84ff; color: #3a84ff; }
.btn-mini.danger:hover { background: rgba(239,68,68,0.1); border-color: #ef4444; color: #ef4444; }

.tag-enabled { display: inline-block; padding: 2px 8px; border-radius: 10px; font-size: 12px; background: rgba(16,185,129,0.12); color: #10b981; }
.tag-disabled { display: inline-block; padding: 2px 8px; border-radius: 10px; font-size: 12px; background: rgba(156,163,175,0.15); color: #9ca3af; }
.tag-failed { display: inline-block; padding: 2px 8px; border-radius: 10px; font-size: 12px; background: rgba(239,68,68,0.12); color: #ef4444; cursor: help; }

.alias-section { margin-bottom: 32px; }
.alias-section h3 { margin: 0 0 8px; font-size: 16px; font-weight: 600; }
.alias-section .hint { margin: 0 0 12px; font-size: 13px; color: var(--text-muted); }
.current-alias { margin-left: 8px; font-size: 12px; color: #10b981; }

/* 弹窗 */
.modal-overlay { position: fixed; inset: 0; background: rgba(0,0,0,0.5); display: flex; align-items: center; justify-content: center; z-index: 1000; opacity: 0; visibility: hidden; transition: all 0.3s; }
.modal-overlay.active { opacity: 1; visibility: visible; }
.modal { background: var(--bg-card); border-radius: 12px; width: 90%; max-width: 600px; max-height: 90vh; display: flex; flex-direction: column; }
.modal-wide { max-width: 900px; }
.form-row { display: grid; grid-template-columns: 1fr 1fr; gap: 16px; }
.section-title { margin: 20px 0 10px; font-size: 14px; font-weight: 600; color: var(--text-primary); padding-bottom: 6px; border-bottom: 1px solid var(--border-color); }
.field-map-grid { display: flex; flex-direction: column; gap: 10px; margin-bottom: 16px; }
.field-map-row { display: grid; grid-template-columns: 200px 24px 1fr; align-items: center; gap: 8px; }
.fm-label { font-size: 13px; font-weight: 500; color: var(--text-primary); }
.fm-hint { font-size: 11px; color: var(--text-muted); margin-top: 2px; }
.fm-arrow { text-align: center; color: var(--text-muted); font-size: 16px; }
.fm-input { font-family: monospace; font-size: 13px; }

/* 双面预览 */
.preview-grid { display: grid; grid-template-columns: 1fr 1fr; gap: 12px; margin-top: 12px; }
.preview-col { display: flex; flex-direction: column; }
.preview-title { font-size: 12px; font-weight: 600; color: var(--text-secondary); margin-bottom: 4px; }
.preview-content { font-size: 11px; max-height: 280px; overflow: auto; margin: 0; background: rgba(0,0,0,0.04); padding: 8px; border-radius: 4px; font-family: monospace; }
.modal-header { padding: 16px 20px; border-bottom: 1px solid var(--border-color); display: flex; justify-content: space-between; align-items: center; }
.modal-title { font-weight: 600; font-size: 16px; }
.modal-close { background: var(--bg-secondary); border: none; width: 28px; height: 28px; border-radius: 6px; cursor: pointer; font-size: 18px; }
.modal-body { padding: 20px; overflow-y: auto; }
.modal-footer { padding: 12px 20px; border-top: 1px solid var(--border-color); display: flex; justify-content: flex-end; gap: 8px; }

.form-group { margin-bottom: 16px; }
.form-label { display: block; margin-bottom: 6px; font-size: 14px; font-weight: 500; }
.form-label.required::after { content: ' *'; color: #ef4444; }
.form-input, .form-textarea { width: 100%; padding: 8px 12px; border: 1px solid var(--border-color); border-radius: 6px; background: var(--bg-input, var(--bg-primary)); color: var(--text-primary); font-size: 14px; box-sizing: border-box; }
.form-input:focus, .form-textarea:focus { outline: none; border-color: #3a84ff; }
.code-area { font-family: monospace; font-size: 12px; resize: vertical; min-height: 100px; }
.form-hint { font-size: 12px; color: var(--text-muted); }

.test-result { margin-top: 12px; padding: 10px 14px; border-radius: 8px; font-size: 13px; }
.test-result.ok { background: rgba(16,185,129,0.08); color: #047857; border: 1px solid rgba(16,185,129,0.3); }
.test-result.fail { background: rgba(239,68,68,0.08); color: #b91c1c; border: 1px solid rgba(239,68,68,0.3); }
.test-result pre { font-size: 11px; max-height: 200px; overflow: auto; margin: 8px 0 0; background: rgba(0,0,0,0.04); padding: 8px; border-radius: 4px; }

.btn { padding: 8px 16px; border-radius: 6px; cursor: pointer; font-size: 14px; border: 1px solid var(--border-color); }
.btn-primary { background: #3a84ff; color: white; border-color: #3a84ff; }
.btn-secondary { background: var(--bg-secondary); color: var(--text-primary); }
.btn:hover { opacity: 0.9; }
</style>
