<script setup>
import { ref, computed, onMounted } from 'vue'
import { useAppStore } from '@/stores'
import api from '@/api'

const appStore = useAppStore()

const roles = ref([])
const allPermissions = ref([])
const loading = ref(false)

const tempRoleSearchQuery = ref('')
const tempRoleTypeFilter = ref('')
const tempRoleStatusFilter = ref('')

const roleSearchQuery = ref('')
const roleTypeFilter = ref('')
const roleStatusFilter = ref('')

const roleCurrentPage = ref(1)
const rolePageSize = ref(10)

const showRoleModal = ref(false)
const editRoleMode = ref(false)
const newRole = ref({ code: '', name: '', description: '', status: 'active' })

const showRolePermissionModal = ref(false)
const selectedRole = ref(null)
const selectedRolePermissions = ref({})

// 发布中心环境权限（与菜单/按钮/数据/API 权限并列的第 5 类）
const deployEnvs = ref([])              // [{id, name, env_type}]
const selectedDeployEnvs = ref(new Set()) // Set<envName>
const deployEnvsLoadFailed = ref(false)

const stats = computed(() => ({
  total: roles.value.length,
  system: roles.value.filter(r => r.is_system).length,
  custom: roles.value.filter(r => !r.is_system).length,
  userCount: roles.value.reduce((sum, r) => sum + (r.user_count || 0), 0)
}))

const filteredRoles = computed(() => {
  let list = roles.value
  if (roleSearchQuery.value) {
    const q = roleSearchQuery.value.toLowerCase()
    list = list.filter(r => r.code?.toLowerCase().includes(q) || r.name?.toLowerCase().includes(q))
  }
  if (roleTypeFilter.value === 'system') list = list.filter(r => r.is_system)
  else if (roleTypeFilter.value === 'custom') list = list.filter(r => !r.is_system)
  if (roleStatusFilter.value) list = list.filter(r => r.status === roleStatusFilter.value)
  return list
})

const roleTotalPages = computed(() => Math.ceil(filteredRoles.value.length / rolePageSize.value) || 1)
const paginatedRoles = computed(() => {
  const start = (roleCurrentPage.value - 1) * rolePageSize.value
  return filteredRoles.value.slice(start, start + rolePageSize.value)
})

onMounted(() => { loadRoles(); loadAllPermissions() })

async function loadRoles() {
  loading.value = true
  try {
    const res = await api.get('/api/roles')
    roles.value = res.data || []
  } catch (e) { appStore.showToast('加载角色失败', 'error') }
  finally { loading.value = false }
}

function applyFilter() {
  roleSearchQuery.value = tempRoleSearchQuery.value
  roleTypeFilter.value = tempRoleTypeFilter.value
  roleStatusFilter.value = tempRoleStatusFilter.value
  roleCurrentPage.value = 1
}

function resetFilter() {
  tempRoleSearchQuery.value = ''
  tempRoleTypeFilter.value = ''
  tempRoleStatusFilter.value = ''
  roleSearchQuery.value = ''
  roleTypeFilter.value = ''
  roleStatusFilter.value = ''
  roleCurrentPage.value = 1
}

async function loadAllPermissions() {
  try {
    const res = await api.get('/api/permissions')
    const data = res.data || []
    allPermissions.value = buildPermissionTree(data)
    console.log('加载权限完成，共', allPermissions.value.length, '条')
  } catch (e) { console.error(e) }
}

function buildPermissionTree(permissions) {
  const permMap = {}
  const roots = []
  permissions.forEach(perm => {
    perm.children = []
    permMap[perm.id] = perm
  })
  permissions.forEach(perm => {
    if (perm.parent_id && permMap[perm.parent_id]) {
      permMap[perm.parent_id].children.push(perm)
    } else {
      roots.push(perm)
    }
  })
  return roots
}

function getAllPermissionsFlat() {
  const result = []
  const flatten = (perms) => {
    for (const perm of perms) {
      result.push(perm)
      if (perm.children && perm.children.length > 0) {
        flatten(perm.children)
      }
    }
  }
  flatten(allPermissions.value)
  return result
}

function getGroupedButtonPermissions() {
  const buttons = getAllPermissionsFlat().filter(p => p.type === 'button')
  const groups = {}
  
  for (const perm of buttons) {
    // 从权限名称中提取页面名称，格式如 "[排班管理] 添加员工"
    const match = perm.name.match(/^\[(.+?)\]\s*(.+)$/)
    if (match) {
      const pageName = match[1]
      const actionName = match[2]
      if (!groups[pageName]) {
        groups[pageName] = []
      }
      groups[pageName].push({ ...perm, displayName: actionName })
    } else {
      // 兼容旧格式：尝试从 code 推断页面
      const codePrefix = perm.code.split(':')[0]
      const pageNameMap = {
        'user': '用户管理',
        'role': '角色管理',
        'asset': '资产管理',
        'domain': '域名管理',
        'vault': '密码库',
        'schedule': '排班管理',
        'merchant': '商户管理',
        'network': '网络管理'
      }
      const pageName = pageNameMap[codePrefix] || '其他'
      if (!groups[pageName]) {
        groups[pageName] = []
      }
      groups[pageName].push({ ...perm, displayName: perm.name })
    }
  }
  
  // 按页面名称排序
  const sortedGroups = {}
  const sortOrder = ['用户管理', '角色管理', '资产管理', '域名管理', '商户管理', '网络管理', '排班管理', '密码库', '其他']
  for (const name of sortOrder) {
    if (groups[name]) {
      sortedGroups[name] = groups[name]
    }
  }
  // 添加未在排序列表中的页面
  for (const name of Object.keys(groups)) {
    if (!sortedGroups[name]) {
      sortedGroups[name] = groups[name]
    }
  }
  
  return sortedGroups
}

function openRoleModal(role = null) {
  if (role) {
    editRoleMode.value = true
    newRole.value = { ...role }
  } else {
    editRoleMode.value = false
    newRole.value = { code: '', name: '', description: '', status: 'active' }
  }
  showRoleModal.value = true
}

async function saveRole() {
  if (!newRole.value.code || !newRole.value.name) {
    appStore.showToast('请填写角色代码和名称', 'error')
    return
  }
  try {
    if (editRoleMode.value) {
      await api.put('/api/roles/' + newRole.value.id, newRole.value)
      appStore.showToast('角色更新成功', 'success')
    } else {
      await api.post('/api/roles', newRole.value)
      appStore.showToast('角色创建成功', 'success')
    }
    showRoleModal.value = false
    loadRoles()
  } catch (e) { appStore.showToast('保存失败: ' + (e.response?.data || e.message), 'error') }
}

async function deleteRole(role) {
  if (role.is_system) { appStore.showToast('系统角色不能删除', 'error'); return }
  const confirmed = await appStore.showConfirm({
    type: 'danger',
    title: '删除角色',
    message: `确定要删除角色 "${role.name}" 吗？\n删除后，使用此角色的用户将失去相关权限。`,
    okText: '删除',
    cancelText: '取消'
  })
  if (!confirmed) return
  try {
    await api.delete('/api/roles/' + role.id)
    appStore.showToast('删除成功', 'success')
    loadRoles()
  } catch (e) { appStore.showToast('删除失败', 'error') }
}

async function openRolePermissionModal(role) {
  selectedRole.value = role
  selectedRolePermissions.value = {}
  selectedDeployEnvs.value = new Set()
  deployEnvsLoadFailed.value = false

  // 加载已有权限和全部权限
  await loadAllPermissions()

  // 从后端获取角色已分配的权限
  try {
    const res = await api.get('/api/roles/' + role.id + '/permissions')
    const permMap = res.data || {}
    selectedRolePermissions.value = { ...permMap }
  } catch (e) {
    console.error('加载角色权限失败:', e)
  }

  // 加载发布中心环境列表 + 当前角色已勾的 env
  try {
    const [envsRes, roleEnvsRes] = await Promise.all([
      api.get('/api/admin/deploy-center-envs'),
      api.get('/api/admin/role-deploy-envs/' + role.id)
    ])
    const rawEnvs = envsRes?.data?.data ?? envsRes?.data ?? []
    deployEnvs.value = Array.isArray(rawEnvs) ? rawEnvs : []
    const rawRoleEnvs = roleEnvsRes?.data?.data ?? roleEnvsRes?.data ?? []
    selectedDeployEnvs.value = new Set(Array.isArray(rawRoleEnvs) ? rawRoleEnvs : [])
  } catch (e) {
    console.error('加载发布中心环境失败:', e)
    deployEnvs.value = []
    deployEnvsLoadFailed.value = true
  }

  showRolePermissionModal.value = true
}

function togglePermission(permId) {
  selectedRolePermissions.value[permId] = !selectedRolePermissions.value[permId]
}

function toggleDeployEnv(envName, on) {
  const s = new Set(selectedDeployEnvs.value)
  if (on) s.add(envName)
  else s.delete(envName)
  selectedDeployEnvs.value = s
}

function toggleAllDeployEnvs(on) {
  selectedDeployEnvs.value = on
    ? new Set(deployEnvs.value.map(e => e.name))
    : new Set()
}

async function saveRolePermissions() {
  const permIds = Object.keys(selectedRolePermissions.value).filter(k => selectedRolePermissions.value[k])
  try {
    await api.put('/api/roles/' + selectedRole.value.id + '/permissions', permIds)
    // 同时保存发布中心 env 权限（拉取失败时跳过，避免误清空）
    if (!deployEnvsLoadFailed.value) {
      const env_names = Array.from(selectedDeployEnvs.value)
      await api.put('/api/admin/role-deploy-envs/' + selectedRole.value.id, { env_names })
    }
    appStore.showToast('权限保存成功', 'success')
    showRolePermissionModal.value = false
    loadRoles()
  } catch (e) {
    console.error('保存权限失败:', e)
    appStore.showToast(e.response?.data || '保存失败', 'error')
  }
}

function formatDate(str) {
  if (!str) return '-'
  const d = new Date(str)
  if (isNaN(d.getTime())) return '-'
  const pad = n => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}:${pad(d.getSeconds())}`
}
</script>

<template>
  <div class="roles-page">
    <div class="page-header-card">
      <div class="page-title-badge">
        <h1 class="page-title">角色管理</h1>
        <span class="badge-count">RBAC</span>
      </div>
      <div class="header-actions">
        <button class="btn btn-primary" @click="openRoleModal()">
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" width="16" height="16">
            <line x1="12" y1="5" x2="12" y2="19"></line>
            <line x1="5" y1="12" x2="19" y2="12"></line>
          </svg>
          创建角色
        </button>
      </div>
    </div>

    <div class="role-stats-grid">
      <div class="role-stat-card">
        <div class="role-stat-icon blue">
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
            <path d="M17 21v-2a4 4 0 0 0-4-4H5a4 4 0 0 0-4 4v2"/>
            <circle cx="9" cy="7" r="4"/>
            <path d="M23 21v-2a4 4 0 0 0-3-3.87"/>
            <path d="M16 3.13a4 4 0 0 1 0 7.75"/>
          </svg>
        </div>
        <div class="role-stat-info">
          <h3>{{ stats.total }}</h3>
          <p>总角色数</p>
        </div>
      </div>
      <div class="role-stat-card">
        <div class="role-stat-icon purple">
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
            <path d="M12 22s8-4 8-10V5l-8-3-8 3v7c0 6 8 10 8 10z"/>
          </svg>
        </div>
        <div class="role-stat-info">
          <h3>{{ stats.system }}</h3>
          <p>系统角色</p>
        </div>
      </div>
      <div class="role-stat-card">
        <div class="role-stat-icon green">
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
            <rect x="3" y="3" width="18" height="18" rx="2" ry="2"/>
            <path d="M9 12l2 2 4-4"/>
          </svg>
        </div>
        <div class="role-stat-info">
          <h3>{{ stats.custom }}</h3>
          <p>自定义角色</p>
        </div>
      </div>
      <div class="role-stat-card">
        <div class="role-stat-icon orange">
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
            <path d="M16 21v-2a4 4 0 0 0-4-4H6a4 4 0 0 0-4 4v2"/>
            <circle cx="9" cy="7" r="4"/>
            <path d="M22 21v-2a4 4 0 0 0-3-3.87"/>
            <path d="M16 3.13a4 4 0 0 1 0 7.75"/>
          </svg>
        </div>
        <div class="role-stat-info">
          <h3>{{ stats.userCount }}</h3>
          <p>关联用户</p>
        </div>
      </div>
    </div>

    <div class="filter-bar">
      <div class="filter-search">
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
          <circle cx="11" cy="11" r="8"/>
          <path d="M21 21l-4.35-4.35"/>
        </svg>
        <input type="text" v-model="tempRoleSearchQuery" placeholder="搜索角色代码或名称..." @keyup.enter="applyFilter">
      </div>
      <select class="filter-select-new" v-model="tempRoleTypeFilter">
        <option value="">全部类型</option>
        <option value="system">系统角色</option>
        <option value="custom">自定义角色</option>
      </select>
      <select class="filter-select-new" v-model="tempRoleStatusFilter">
        <option value="">全部状态</option>
        <option value="active">启用</option>
        <option value="disabled">禁用</option>
      </select>
      <button class="btn-search" @click="applyFilter">搜 索</button>
      <button class="btn-reset" @click="resetFilter">重 置</button>
    </div>

    <div class="table-card-new">
      <table>
        <thead>
          <tr>
            <th>角色代码</th>
            <th>角色名称</th>
            <th>描述</th>
            <th>用户数</th>
            <th>类型</th>
            <th>状态</th>
            <th>创建时间</th>
            <th>操作</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="role in paginatedRoles" :key="role.id">
            <td><span class="role-code">{{ role.code }}</span></td>
            <td>{{ role.name }}</td>
            <td class="desc-cell">{{ role.description || '-' }}</td>
            <td>
              <span class="user-count-badge">
                <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                  <path d="M20 21v-2a4 4 0 0 0-4-4H8a4 4 0 0 0-4 4v2"/>
                  <circle cx="12" cy="7" r="4"/>
                </svg>
                {{ role.user_count || 0 }}
              </span>
            </td>
            <td>
              <span class="type-badge-new" :class="role.is_system ? 'system' : 'custom'">
                {{ role.is_system ? '系统' : '自定义' }}
              </span>
            </td>
            <td>
              <span class="status-badge-new" :class="role.status === 'active' ? 'enabled' : 'disabled'">
                {{ role.status === 'active' ? '启用' : '禁用' }}
              </span>
            </td>
            <td class="date-cell">{{ formatDate(role.created_at) }}</td>
            <td>
              <div class="action-buttons">
                <button class="action-btn view" @click="openRolePermissionModal(role)" title="查看权限">
                  <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                    <path d="M1 12s4-8 11-8 11 8 11 8-4 8-11 8-11-8-11-8z"/>
                    <circle cx="12" cy="12" r="3"/>
                  </svg>
                </button>
                <button class="action-btn edit" @click="openRoleModal(role)" :disabled="role.is_system" title="编辑">
                  <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                    <path d="M11 4H4a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2h14a2 2 0 0 0 2-2v-7"/>
                    <path d="M18.5 2.5a2.121 2.121 0 0 1 3 3L12 15l-4 1 1-4 9.5-9.5z"/>
                  </svg>
                </button>
                <button class="action-btn delete" @click="deleteRole(role)" :disabled="role.is_system" title="删除">
                  <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                    <path d="M3 6h18M19 6v14a2 2 0 0 1-2 2H7a2 2 0 0 1-2-2V6m3 0V4a2 2 0 0 1 2-2h4a2 2 0 0 1 2 2v2"/>
                  </svg>
                </button>
              </div>
            </td>
          </tr>
          <tr v-if="paginatedRoles.length === 0">
            <td colspan="8" class="empty-row">暂无角色数据</td>
          </tr>
        </tbody>
      </table>
      <div class="table-footer-new">
        <div class="page-info">
          显示 {{ Math.min((roleCurrentPage - 1) * rolePageSize + 1, filteredRoles.length) }}-{{ Math.min(roleCurrentPage * rolePageSize, filteredRoles.length) }} 条，共 {{ filteredRoles.length }} 条
        </div>
        <div class="pagination-controls">
          <select class="page-size-select" v-model="rolePageSize" @change="roleCurrentPage = 1">
            <option :value="10">10 条/页</option>
            <option :value="20">20 条/页</option>
            <option :value="50">50 条/页</option>
          </select>
          <div class="pagination-btns">
            <button class="page-btn" @click="roleCurrentPage = Math.max(1, roleCurrentPage - 1)" :disabled="roleCurrentPage <= 1">
              <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" width="14" height="14"><path d="M15 18l-6-6 6-6"/></svg>
            </button>
            <span class="page-number">{{ roleCurrentPage }} / {{ roleTotalPages }}</span>
            <button class="page-btn" @click="roleCurrentPage = Math.min(roleTotalPages, roleCurrentPage + 1)" :disabled="roleCurrentPage >= roleTotalPages">
              <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" width="14" height="14"><path d="M9 18l6-6-6-6"/></svg>
            </button>
          </div>
        </div>
      </div>
    </div>

    <Teleport to="body">
      <div v-if="showRoleModal" class="roles-modal-overlay show">
        <div class="roles-modal">
          <div class="modal-header">
            <h3>{{ editRoleMode ? '编辑角色' : '创建角色' }}</h3>
            <button class="modal-close" @click="showRoleModal = false">×</button>
          </div>
          <div class="modal-body">
            <div class="form-group">
              <label>角色代码 <span class="required">*</span></label>
              <input type="text" v-model="newRole.code" class="form-input" placeholder="如：operator" :disabled="editRoleMode">
              <small class="form-hint">角色的唯一标识，创建后不可修改</small>
            </div>
            <div class="form-group">
              <label>角色名称 <span class="required">*</span></label>
              <input type="text" v-model="newRole.name" class="form-input" placeholder="如：运维人员">
            </div>
            <div class="form-group">
              <label>描述</label>
              <textarea v-model="newRole.description" class="form-input" rows="3" placeholder="角色描述信息"></textarea>
            </div>
            <div class="form-group">
              <label>状态</label>
              <select v-model="newRole.status" class="form-select">
                <option value="active">启用</option>
                <option value="disabled">禁用</option>
              </select>
            </div>
          </div>
          <div class="modal-footer">
            <button class="btn btn-default" @click="showRoleModal = false">取消</button>
            <button class="btn btn-primary" @click="saveRole">保存</button>
          </div>
        </div>
      </div>

      <div v-if="showRolePermissionModal" class="roles-modal-overlay show">
        <div class="roles-modal modal-lg">
          <div class="modal-header">
            <h3>配置权限 - {{ selectedRole?.name }}</h3>
            <button class="modal-close" @click="showRolePermissionModal = false">×</button>
          </div>
          <div class="modal-body permission-config-body">
            <div class="permission-sections">
              <!-- 菜单权限 - 使用树形结构 -->
              <div class="permission-section">
                <h4 class="section-title">菜单权限</h4>
                <div class="permission-tree">
                  <template v-for="perm in allPermissions.filter(p => p.type === 'menu')" :key="perm.id">
                    <div class="permission-item parent">
                      <label class="checkbox-label">
                        <input type="checkbox" :checked="selectedRolePermissions[perm.id]" @change="togglePermission(perm.id)">
                        <span>{{ perm.name }}</span>
                      </label>
                    </div>
                    <div v-if="perm.children && perm.children.length" class="permission-children">
                      <div v-for="child in perm.children" :key="child.id" class="permission-item child">
                        <label class="checkbox-label">
                          <input type="checkbox" :checked="selectedRolePermissions[child.id]" @change="togglePermission(child.id)">
                          <span>{{ child.name }}</span>
                        </label>
                      </div>
                    </div>
                  </template>
                </div>
              </div>

              <!-- 操作权限 - 按页面分组显示 -->
              <div class="permission-section permission-section-full">
                <h4 class="section-title">操作权限</h4>
                <div class="permission-groups">
                  <div v-for="(perms, pageName) in getGroupedButtonPermissions()" :key="pageName" class="permission-group">
                    <div class="group-header">
                      <span class="group-name">{{ pageName }}</span>
                      <span class="group-count">{{ perms.length }}项</span>
                    </div>
                    <div class="permission-grid">
                      <div v-for="perm in perms" :key="perm.id" class="permission-item">
                        <label class="checkbox-label">
                          <input type="checkbox" :checked="selectedRolePermissions[perm.id]" @change="togglePermission(perm.id)">
                          <span>{{ perm.displayName }}</span>
                        </label>
                      </div>
                    </div>
                  </div>
                </div>
              </div>

              <!-- 数据权限 -->
              <div class="permission-section">
                <h4 class="section-title">数据权限</h4>
                <div class="permission-grid">
                  <div v-for="perm in getAllPermissionsFlat().filter(p => p.type === 'data')" :key="perm.id" class="permission-item">
                    <label class="checkbox-label">
                      <input type="checkbox" :checked="selectedRolePermissions[perm.id]" @change="togglePermission(perm.id)">
                      <span>{{ perm.name }}</span>
                    </label>
                  </div>
                </div>
              </div>

              <!-- 发布中心环境权限（数据级 RBAC，仅对非 admin 角色生效） -->
              <div class="permission-section permission-section-full">
                <h4 class="section-title">
                  发布中心环境
                  <span class="section-hint">没勾的环境，该角色用户在发布中心完全看不到（admin / super_admin 自动绕过）</span>
                  <span v-if="deployEnvs.length > 0" class="section-counter">
                    已勾 {{ selectedDeployEnvs.size }} / {{ deployEnvs.length }}
                  </span>
                </h4>
                <div v-if="deployEnvsLoadFailed" class="env-load-failed">
                  无法连接发布中心，已暂时禁用此处的修改（保存时会跳过 env 部分以免误清空）。请检查 <code>DEPLOY_CENTER_INTERNAL_URL</code> / <code>DEPLOY_CENTER_INTERNAL_TOKEN</code>。
                </div>
                <div v-else-if="deployEnvs.length === 0" class="env-empty">
                  发布中心暂无项目环境。
                </div>
                <template v-else>
                  <div class="env-toolbar">
                    <button type="button" class="btn-mini" @click="toggleAllDeployEnvs(true)">全选</button>
                    <button type="button" class="btn-mini" @click="toggleAllDeployEnvs(false)">清空</button>
                  </div>
                  <div class="deploy-env-grid">
                    <label v-for="e in deployEnvs" :key="e.name"
                      :class="['deploy-env-chip', e.env_type, { on: selectedDeployEnvs.has(e.name) }]">
                      <input type="checkbox"
                        :checked="selectedDeployEnvs.has(e.name)"
                        @change="toggleDeployEnv(e.name, $event.target.checked)" />
                      <span class="deploy-env-name">{{ e.name }}</span>
                      <span :class="'deploy-env-badge ' + e.env_type">{{ e.env_type.toUpperCase() }}</span>
                    </label>
                  </div>
                </template>
              </div>

              <!-- API 权限 -->
              <div class="permission-section">
                <h4 class="section-title">API 权限</h4>
                <div class="permission-grid">
                  <div v-for="perm in getAllPermissionsFlat().filter(p => p.type === 'api')" :key="perm.id" class="permission-item">
                    <label class="checkbox-label">
                      <input type="checkbox" :checked="selectedRolePermissions[perm.id]" @change="togglePermission(perm.id)">
                      <span>{{ perm.name }}</span>
                    </label>
                  </div>
                </div>
              </div>
            </div>
          </div>
          <div class="modal-footer">
            <button class="btn btn-default" @click="showRolePermissionModal = false">取消</button>
            <button class="btn btn-primary" @click="saveRolePermissions">保存权限</button>
          </div>
        </div>
      </div>
    </Teleport>
  </div>
</template>

<style scoped>
.roles-page { padding: 0; }

.page-header-card {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 20px;
}
.page-title-badge { display: flex; align-items: center; gap: 12px; }
.page-title { font-size: 1.5rem; font-weight: 600; color: var(--text-primary); margin: 0; }
.badge-count {
  background: linear-gradient(135deg, #3b82f6 0%, #2563eb 100%);
  color: #fff;
  padding: 4px 12px;
  border-radius: 20px;
  font-size: 0.75rem;
  font-weight: 500;
}
.header-actions { display: flex; gap: 8px; }
.btn { display: inline-flex; align-items: center; gap: 6px; padding: 8px 16px; border-radius: 8px; font-size: 0.875rem; font-weight: 500; cursor: pointer; border: none; transition: all 0.2s; }
.btn-primary { background: linear-gradient(135deg, #3b82f6 0%, #2563eb 100%); color: #fff; }
.btn-primary:hover { transform: translateY(-1px); box-shadow: 0 4px 12px rgba(59, 130, 246, 0.4); }
.btn-default { background: var(--bg-secondary); color: var(--text-primary); border: 1px solid var(--border-color); }

.role-stats-grid { display: grid; grid-template-columns: repeat(4, 1fr); gap: 16px; margin-bottom: 20px; }
.role-stat-card {
  background: var(--bg-primary);
  border-radius: 12px;
  padding: 20px;
  display: flex;
  align-items: center;
  gap: 16px;
  border: 1px solid var(--border-color);
}
.role-stat-icon {
  width: 48px;
  height: 48px;
  border-radius: 12px;
  display: flex;
  align-items: center;
  justify-content: center;
}
.role-stat-icon svg { width: 24px; height: 24px; }
.role-stat-icon.blue { background: rgba(59, 130, 246, 0.1); color: #3b82f6; }
.role-stat-icon.purple { background: rgba(139, 92, 246, 0.1); color: #8b5cf6; }
.role-stat-icon.green { background: rgba(34, 197, 94, 0.1); color: #22c55e; }
.role-stat-icon.orange { background: rgba(249, 115, 22, 0.1); color: #f97316; }
.role-stat-info h3 { font-size: 1.75rem; font-weight: 700; margin: 0; color: var(--text-primary); }
.role-stat-info p { font-size: 0.875rem; color: var(--text-secondary); margin: 4px 0 0; }

.filter-bar {
  display: flex;
  gap: 12px;
  margin-bottom: 16px;
  background: var(--bg-primary);
  padding: 16px;
  border-radius: 12px;
  border: 1px solid var(--border-color);
}
.filter-search {
  flex: 1;
  display: flex;
  align-items: center;
  gap: 8px;
  background: var(--bg-secondary);
  padding: 0 12px;
  border-radius: 8px;
  border: 1px solid var(--border-color);
}
.filter-search svg { width: 18px; height: 18px; color: var(--text-muted); }
.filter-search input {
  flex: 1;
  border: none;
  background: transparent;
  padding: 10px 0;
  font-size: 0.875rem;
  color: var(--text-primary);
  outline: none;
}
.filter-select-new {
  padding: 10px 12px;
  border-radius: 8px;
  border: 1px solid var(--border-color);
  background: var(--bg-secondary);
  color: var(--text-primary);
  font-size: 0.875rem;
}
.btn-search { padding: 10px 20px; border-radius: 8px; border: none; background: #3a84ff; color: #fff; font-size: 14px; font-weight: 500; cursor: pointer; transition: all 0.2s; }
.btn-search:hover { background: #2970e6; }
.btn-reset { padding: 10px 20px; border-radius: 8px; border: 1px solid var(--border-color); background: var(--bg-secondary); color: var(--text-primary); font-size: 14px; cursor: pointer; transition: all 0.2s; }
.btn-reset:hover { background: var(--bg-hover); }

.filter-select-new {
  min-width: 120px;
  cursor: pointer;
}

.table-card-new {
  background: var(--bg-primary);
  border-radius: 12px;
  border: 1px solid var(--border-color);
  overflow: hidden;
}
table { width: 100%; border-collapse: collapse; }
th, td { padding: 14px 16px; text-align: left; border-bottom: 1px solid var(--border-color); }
th { background: var(--bg-secondary); font-weight: 600; font-size: 0.8125rem; color: var(--text-secondary); text-transform: uppercase; letter-spacing: 0.5px; }
td { font-size: 0.875rem; color: var(--text-primary); }
.desc-cell { color: var(--text-secondary); max-width: 280px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.date-cell { color: var(--text-secondary); font-size: 0.8125rem; }
.empty-row { text-align: center; color: var(--text-muted); padding: 40px !important; }

.role-code {
  font-family: 'Consolas', 'Monaco', monospace;
  background: rgba(59, 130, 246, 0.1);
  color: #3b82f6;
  padding: 4px 8px;
  border-radius: 4px;
  font-size: 0.8125rem;
}
.user-count-badge {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  background: var(--bg-secondary);
  padding: 4px 10px;
  border-radius: 20px;
  font-size: 0.8125rem;
}
.user-count-badge svg { width: 14px; height: 14px; }
.type-badge-new {
  display: inline-block;
  padding: 4px 10px;
  border-radius: 20px;
  font-size: 0.75rem;
  font-weight: 500;
}
.type-badge-new.system { background: rgba(139, 92, 246, 0.1); color: #8b5cf6; }
.type-badge-new.custom { background: rgba(59, 130, 246, 0.1); color: #3b82f6; }
.status-badge-new {
  display: inline-block;
  padding: 4px 10px;
  border-radius: 20px;
  font-size: 0.75rem;
  font-weight: 500;
}
.status-badge-new.enabled { background: rgba(34, 197, 94, 0.1); color: #22c55e; }
.status-badge-new.disabled { background: rgba(239, 68, 68, 0.1); color: #ef4444; }

.action-buttons { display: flex; gap: 8px; }
.action-btn {
  width: 32px;
  height: 32px;
  border-radius: 6px;
  border: 1px solid var(--border-color);
  background: var(--bg-primary);
  cursor: pointer;
  display: flex;
  align-items: center;
  justify-content: center;
  transition: all 0.2s;
}
.action-btn svg { width: 16px; height: 16px; }
.action-btn.view { color: #3b82f6; }
.action-btn.view:hover { background: rgba(59, 130, 246, 0.1); border-color: #3b82f6; }
.action-btn.edit { color: #f59e0b; }
.action-btn.edit:hover { background: rgba(245, 158, 11, 0.1); border-color: #f59e0b; }
.action-btn.delete { color: #ef4444; }
.action-btn.delete:hover { background: rgba(239, 68, 68, 0.1); border-color: #ef4444; }
.action-btn:disabled { opacity: 0.4; cursor: not-allowed; }

.table-footer-new {
  display: flex;
  justify-content: space-between;
  align-items: center;
  padding: 12px 16px;
  background: var(--bg-secondary);
  border-top: 1px solid var(--border-color);
}
.page-info { font-size: 0.8125rem; color: var(--text-secondary); }
.pagination-controls { display: flex; align-items: center; gap: 12px; }
.page-size-select { padding: 6px 10px; border-radius: 6px; border: 1px solid var(--border-color); background: var(--bg-primary); font-size: 0.8125rem; }
.pagination-btns { display: flex; align-items: center; gap: 8px; }
.page-btn {
  width: 32px;
  height: 32px;
  border-radius: 6px;
  border: 1px solid var(--border-color);
  background: var(--bg-primary);
  cursor: pointer;
  display: flex;
  align-items: center;
  justify-content: center;
}
.page-btn:hover:not(:disabled) { background: var(--bg-secondary); }
.page-btn:disabled { opacity: 0.4; cursor: not-allowed; }
.page-number { font-size: 0.875rem; color: var(--text-primary); min-width: 60px; text-align: center; }

</style>

<!-- 弹窗样式 - 全局样式用于 Teleport -->
<style>
.roles-modal-overlay {
  position: fixed;
  inset: 0;
  background: rgba(0, 0, 0, 0.6);
  backdrop-filter: blur(4px);
  display: flex;
  align-items: center;
  justify-content: center;
  z-index: 10000;
  opacity: 0;
  visibility: hidden;
  transition: all 0.2s;
}
.roles-modal-overlay.show { opacity: 1; visibility: visible; }
.roles-modal {
  background: #fff;
  border-radius: 16px;
  width: 90%;
  max-width: 500px;
  max-height: 90vh;
  overflow: hidden;
  display: flex;
  flex-direction: column;
  box-shadow: 0 20px 50px rgba(0, 0, 0, 0.3);
}
.roles-modal.modal-lg { max-width: 800px; }
.roles-modal .modal-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  padding: 20px 24px;
  border-bottom: 1px solid #e2e8f0;
  background: #fff;
}
.roles-modal .modal-header h3 { margin: 0; font-size: 1.125rem; font-weight: 600; color: #1e293b; }
.roles-modal .modal-close { background: none; border: none; font-size: 1.5rem; cursor: pointer; color: #94a3b8; line-height: 1; padding: 0; width: 32px; height: 32px; display: flex; align-items: center; justify-content: center; border-radius: 8px; transition: all 0.2s; }
.roles-modal .modal-close:hover { background: #f1f5f9; color: #475569; }
.roles-modal .modal-body { padding: 24px; overflow-y: auto; flex: 1; background: #fff; }
.roles-modal .modal-footer { display: flex; justify-content: flex-end; gap: 12px; padding: 16px 24px; border-top: 1px solid #e2e8f0; background: #f8fafc; }

.roles-modal .form-group { margin-bottom: 20px; }
.roles-modal .form-group label { display: block; margin-bottom: 8px; font-weight: 500; font-size: 0.875rem; color: #1e293b; }
.roles-modal .form-group .required { color: #ef4444; }
.roles-modal .form-input, .roles-modal .form-select {
  width: 100%;
  padding: 10px 14px;
  border: 1px solid #e2e8f0;
  border-radius: 8px;
  font-size: 0.875rem;
  background: #fff;
  color: #1e293b;
  transition: border-color 0.2s, box-shadow 0.2s;
  box-sizing: border-box;
}
.roles-modal .form-input:focus, .roles-modal .form-select:focus { outline: none; border-color: #3b82f6; box-shadow: 0 0 0 3px rgba(59, 130, 246, 0.1); }
.roles-modal .form-input:disabled { background: #f1f5f9; opacity: 0.7; cursor: not-allowed; }
.roles-modal .form-hint { display: block; margin-top: 6px; font-size: 0.75rem; color: #94a3b8; }
.roles-modal textarea.form-input { resize: vertical; min-height: 80px; }

.roles-modal .permission-config-body { max-height: 60vh; overflow-y: auto; }
.roles-modal .permission-sections { display: grid; grid-template-columns: repeat(2, 1fr); gap: 24px; }
.roles-modal .permission-section { background: #f8fafc; border-radius: 12px; padding: 16px; }
.roles-modal .permission-section-full { grid-column: 1 / -1; }
.roles-modal .section-title { font-size: 0.875rem; font-weight: 600; margin-bottom: 12px; color: #1e293b; padding-bottom: 8px; border-bottom: 1px solid #e2e8f0; margin-top: 0; }
.roles-modal .permission-tree { display: flex; flex-direction: column; gap: 8px; max-height: 200px; overflow-y: auto; }
.roles-modal .permission-item { padding: 8px 12px; background: #fff; border-radius: 6px; border: 1px solid #e2e8f0; transition: all 0.2s; }
.roles-modal .permission-item:hover { border-color: #3b82f6; }
.roles-modal .permission-groups { display: grid; grid-template-columns: repeat(auto-fill, minmax(280px, 1fr)); gap: 16px; }
.roles-modal .permission-group { background: #fff; border-radius: 8px; padding: 12px; border: 1px solid #e2e8f0; }
.roles-modal .group-header { display: flex; justify-content: space-between; align-items: center; margin-bottom: 10px; padding-bottom: 8px; border-bottom: 1px solid #f1f5f9; }
.roles-modal .group-name { font-size: 0.8125rem; font-weight: 600; color: #3b82f6; }
.roles-modal .group-count { font-size: 0.75rem; color: #94a3b8; background: #f1f5f9; padding: 2px 8px; border-radius: 10px; }
.roles-modal .permission-group .permission-grid { display: flex; flex-direction: column; gap: 6px; }
.roles-modal .permission-group .permission-item { padding: 6px 10px; font-size: 0.8125rem; }
.roles-modal .checkbox-label {
  display: flex;
  align-items: center;
  gap: 10px;
  cursor: pointer;
  font-size: 0.875rem;
  color: #1e293b;
}
.roles-modal .checkbox-label input[type="checkbox"] { width: 18px; height: 18px; accent-color: #3b82f6; cursor: pointer; }
.roles-modal .btn { display: inline-flex; align-items: center; gap: 6px; padding: 10px 18px; border-radius: 8px; font-size: 0.875rem; font-weight: 500; cursor: pointer; border: none; transition: all 0.2s; }
.roles-modal .btn-default { background: #f1f5f9; color: #475569; border: 1px solid #e2e8f0; }
.roles-modal .btn-default:hover { background: #e2e8f0; }
.roles-modal .btn-primary { background: linear-gradient(135deg, #3b82f6 0%, #2563eb 100%); color: #fff; }
.roles-modal .btn-primary:hover { transform: translateY(-1px); box-shadow: 0 4px 12px rgba(59, 130, 246, 0.4); }

/* 发布中心环境权限 section */
.roles-modal .section-title .section-hint {
  font-weight: 400; font-size: 12px; color: #94a3b8; margin-left: 8px;
}
.roles-modal .section-title .section-counter {
  float: right; font-weight: 500; font-size: 12px; color: #3b82f6;
  font-family: 'Consolas', monospace;
}
.roles-modal .env-load-failed {
  padding: 12px; background: #fef3c7; border: 1px solid #fde68a; border-radius: 6px;
  font-size: 12.5px; color: #78350f; line-height: 1.6;
}
.roles-modal .env-load-failed code {
  background: rgba(0,0,0,.06); padding: 1px 5px; border-radius: 3px; font-family: monospace;
}
.roles-modal .env-empty {
  padding: 16px; text-align: center; color: #94a3b8; font-size: 13px;
  background: #fff; border: 1px dashed #e2e8f0; border-radius: 6px;
}
.roles-modal .env-toolbar { display: flex; gap: 6px; margin-bottom: 10px; }
.roles-modal .btn-mini {
  padding: 4px 10px; font-size: 12px; border: 1px solid #e2e8f0;
  background: #fff; border-radius: 4px; cursor: pointer; color: #475569;
}
.roles-modal .btn-mini:hover { border-color: #3b82f6; color: #3b82f6; }
.roles-modal .deploy-env-grid {
  display: grid; grid-template-columns: repeat(auto-fill, minmax(220px, 1fr)); gap: 8px;
}
.roles-modal .deploy-env-chip {
  display: flex; align-items: center; gap: 8px;
  padding: 8px 12px; border: 1px solid #e2e8f0; border-radius: 6px;
  background: #fff; cursor: pointer; transition: all .12s;
}
.roles-modal .deploy-env-chip:hover { border-color: #3b82f6; background: #fbfdff; }
.roles-modal .deploy-env-chip.on { border-color: #10b981; background: #ecfdf5; }
.roles-modal .deploy-env-chip.on.prod { border-color: #ef4444; background: #fef2f2; }
.roles-modal .deploy-env-chip input { margin: 0; cursor: pointer; accent-color: #3b82f6; width: 16px; height: 16px; }
.roles-modal .deploy-env-chip.prod input { accent-color: #ef4444; }
.roles-modal .deploy-env-name { flex: 1; font-family: 'Consolas', monospace; font-size: 12.5px; color: #1e293b; }
.roles-modal .deploy-env-badge {
  font: 700 10px 'Consolas', monospace; padding: 2px 6px; border-radius: 3px; letter-spacing: .4px;
}
.roles-modal .deploy-env-badge.uat { background: #ecfdf5; color: #059669; }
.roles-modal .deploy-env-badge.prod { background: #fef2f2; color: #dc2626; }
</style>
