<script setup>
import { ref, computed, onMounted } from 'vue'
import { useAppStore } from '@/stores'
import api from '@/api'

const appStore = useAppStore()

const allPermissions = ref([])
const loading = ref(false)
const permSearchQuery = ref('')
const permTypeFilter = ref('')

const permCurrentPage = ref(1)
const permPageSize = ref(10)

const showPermModal = ref(false)
const editPermMode = ref(false)
const newPerm = ref({ code: '', name: '', type: 'button', resource: '', description: '' })

const stats = computed(() => ({
  total: allPermissions.value.length,
  menu: allPermissions.value.filter(p => p.type === 'menu').length,
  button: allPermissions.value.filter(p => p.type === 'button').length,
  data: allPermissions.value.filter(p => p.type === 'data').length,
  api: allPermissions.value.filter(p => p.type === 'api').length
}))

const filteredPermissions = computed(() => {
  let list = allPermissions.value
  if (permSearchQuery.value) {
    const q = permSearchQuery.value.toLowerCase()
    list = list.filter(p => p.code?.toLowerCase().includes(q) || p.name?.toLowerCase().includes(q))
  }
  if (permTypeFilter.value) list = list.filter(p => p.type === permTypeFilter.value)
  return list
})

const permTotalPages = computed(() => Math.ceil(filteredPermissions.value.length / permPageSize.value) || 1)
const paginatedPermissions = computed(() => {
  const start = (permCurrentPage.value - 1) * permPageSize.value
  return filteredPermissions.value.slice(start, start + permPageSize.value)
})

onMounted(() => { loadPermissions() })

async function loadPermissions() {
  loading.value = true
  try {
    const res = await api.get('/api/permissions')
    allPermissions.value = res.data || []
  } catch (e) { appStore.showToast('加载权限失败', 'error') }
  finally { loading.value = false }
}

function openPermissionModal(perm = null) {
  if (perm) {
    editPermMode.value = true
    newPerm.value = { ...perm }
  } else {
    editPermMode.value = false
    newPerm.value = { code: '', name: '', type: 'button', resource: '', description: '' }
  }
  showPermModal.value = true
}

async function savePerm() {
  if (!newPerm.value.code || !newPerm.value.name) {
    appStore.showToast('请填写权限代码和名称', 'error')
    return
  }
  try {
    if (editPermMode.value) {
      await api.put('/api/permissions/' + newPerm.value.id, newPerm.value)
      appStore.showToast('权限更新成功', 'success')
    } else {
      await api.post('/api/permissions', newPerm.value)
      appStore.showToast('权限创建成功', 'success')
    }
    showPermModal.value = false
    loadPermissions()
  } catch (e) { appStore.showToast('保存失败: ' + (e.response?.data || e.message), 'error') }
}

async function deletePerm(perm) {
  const confirmed = await appStore.showConfirm({
    type: 'danger',
    title: '删除权限',
    message: `确定要删除权限 "${perm.name}" 吗？\n此操作不可恢复。`,
    okText: '删除',
    cancelText: '取消'
  })
  if (!confirmed) return
  try {
    await api.delete('/api/permissions/' + perm.id)
    appStore.showToast('删除成功', 'success')
    loadPermissions()
  } catch (e) { appStore.showToast('删除失败', 'error') }
}

function getPermissionTypeLabel(type) {
  const map = { menu: '菜单', button: '按钮', data: '数据', api: 'API' }
  return map[type] || type
}
</script>

<template>
  <div class="permissions-page">
    <div class="page-header-card">
      <div class="page-title-badge">
        <h1 class="page-title">权限配置</h1>
        <span class="badge-count">{{ stats.total }} 项权限</span>
      </div>
      <div class="header-actions">
        <button class="btn btn-primary" @click="openPermissionModal()">
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" width="16" height="16">
            <line x1="12" y1="5" x2="12" y2="19"></line>
            <line x1="5" y1="12" x2="19" y2="12"></line>
          </svg>
          添加权限
        </button>
      </div>
    </div>

    <div class="perm-stats-grid">
      <div class="perm-stat-card menu">
        <div class="perm-stat-header">
          <div class="perm-stat-icon menu">
            <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
              <rect x="3" y="3" width="7" height="7"/>
              <rect x="14" y="3" width="7" height="7"/>
              <rect x="14" y="14" width="7" height="7"/>
              <rect x="3" y="14" width="7" height="7"/>
            </svg>
          </div>
          <span class="perm-stat-label">菜单权限</span>
        </div>
        <div class="perm-stat-value">{{ stats.menu }}</div>
      </div>
      <div class="perm-stat-card button">
        <div class="perm-stat-header">
          <div class="perm-stat-icon button">
            <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
              <circle cx="12" cy="12" r="10"/>
              <path d="M8 12h8"/>
            </svg>
          </div>
          <span class="perm-stat-label">按钮权限</span>
        </div>
        <div class="perm-stat-value">{{ stats.button }}</div>
      </div>
      <div class="perm-stat-card data">
        <div class="perm-stat-header">
          <div class="perm-stat-icon data">
            <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
              <ellipse cx="12" cy="5" rx="9" ry="3"/>
              <path d="M21 12c0 1.66-4 3-9 3s-9-1.34-9-3"/>
              <path d="M3 5v14c0 1.66 4 3 9 3s9-1.34 9-3V5"/>
            </svg>
          </div>
          <span class="perm-stat-label">数据权限</span>
        </div>
        <div class="perm-stat-value">{{ stats.data }}</div>
      </div>
      <div class="perm-stat-card api">
        <div class="perm-stat-header">
          <div class="perm-stat-icon api">
            <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
              <path d="M10 13a5 5 0 0 0 7.54.54l3-3a5 5 0 0 0-7.07-7.07l-1.72 1.71"/>
              <path d="M14 11a5 5 0 0 0-7.54-.54l-3 3a5 5 0 0 0 7.07 7.07l1.71-1.71"/>
            </svg>
          </div>
          <span class="perm-stat-label">API 权限</span>
        </div>
        <div class="perm-stat-value">{{ stats.api }}</div>
      </div>
    </div>

    <div class="filter-bar">
      <div class="filter-search">
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
          <circle cx="11" cy="11" r="8"/>
          <path d="M21 21l-4.35-4.35"/>
        </svg>
        <input type="text" v-model="permSearchQuery" placeholder="搜索权限代码或名称...">
      </div>
      <select class="filter-select-new" v-model="permTypeFilter">
        <option value="">全部类型</option>
        <option value="menu">菜单权限</option>
        <option value="button">按钮权限</option>
        <option value="data">数据权限</option>
        <option value="api">API 权限</option>
      </select>
    </div>

    <div class="table-card-new">
      <table>
        <thead>
          <tr>
            <th>权限代码</th>
            <th>权限名称</th>
            <th>类型</th>
            <th>资源路径</th>
            <th>描述</th>
            <th>操作</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="perm in paginatedPermissions" :key="perm.id">
            <td><span class="perm-code">{{ perm.code }}</span></td>
            <td>{{ perm.name }}</td>
            <td>
              <span class="type-badge-new" :class="perm.type">
                {{ getPermissionTypeLabel(perm.type) }}
              </span>
            </td>
            <td>
              <span v-if="perm.resource" class="resource-path">{{ perm.resource }}</span>
              <span v-else class="no-data">-</span>
            </td>
            <td class="desc-cell">{{ perm.description || '-' }}</td>
            <td>
              <div class="action-buttons">
                <button class="action-btn edit" @click="openPermissionModal(perm)" title="编辑">
                  <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                    <path d="M11 4H4a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2h14a2 2 0 0 0 2-2v-7"/>
                    <path d="M18.5 2.5a2.121 2.121 0 0 1 3 3L12 15l-4 1 1-4 9.5-9.5z"/>
                  </svg>
                </button>
                <button class="action-btn delete" @click="deletePerm(perm)" title="删除">
                  <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                    <path d="M3 6h18M19 6v14a2 2 0 0 1-2 2H7a2 2 0 0 1-2-2V6m3 0V4a2 2 0 0 1 2-2h4a2 2 0 0 1 2 2v2"/>
                  </svg>
                </button>
              </div>
            </td>
          </tr>
          <tr v-if="paginatedPermissions.length === 0">
            <td colspan="6" class="empty-row">暂无权限数据</td>
          </tr>
        </tbody>
      </table>
      <div class="table-footer-new">
        <div class="page-info">
          显示 {{ Math.min((permCurrentPage - 1) * permPageSize + 1, filteredPermissions.length) }}-{{ Math.min(permCurrentPage * permPageSize, filteredPermissions.length) }} 条，共 {{ filteredPermissions.length }} 条
        </div>
        <div class="pagination-controls">
          <select class="page-size-select" v-model="permPageSize" @change="permCurrentPage = 1">
            <option :value="10">10 条/页</option>
            <option :value="20">20 条/页</option>
            <option :value="50">50 条/页</option>
          </select>
          <div class="pagination-btns">
            <button class="page-btn" @click="permCurrentPage = Math.max(1, permCurrentPage - 1)" :disabled="permCurrentPage <= 1">
              <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" width="14" height="14"><path d="M15 18l-6-6 6-6"/></svg>
            </button>
            <span class="page-number">{{ permCurrentPage }} / {{ permTotalPages }}</span>
            <button class="page-btn" @click="permCurrentPage = Math.min(permTotalPages, permCurrentPage + 1)" :disabled="permCurrentPage >= permTotalPages">
              <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" width="14" height="14"><path d="M9 18l6-6-6-6"/></svg>
            </button>
          </div>
        </div>
      </div>
    </div>

    <Teleport to="body">
      <div v-if="showPermModal" class="perm-modal-overlay show">
        <div class="perm-modal">
          <div class="modal-header">
            <h3>{{ editPermMode ? '编辑权限' : '添加权限' }}</h3>
            <button class="modal-close" @click="showPermModal = false">×</button>
          </div>
          <div class="modal-body">
            <div class="form-group">
              <label>权限代码 <span class="required">*</span></label>
              <input type="text" v-model="newPerm.code" class="form-input" placeholder="如：user:create">
            </div>
            <div class="form-group">
              <label>权限名称 <span class="required">*</span></label>
              <input type="text" v-model="newPerm.name" class="form-input" placeholder="如：创建用户">
            </div>
            <div class="form-group">
              <label>类型</label>
              <select v-model="newPerm.type" class="form-select">
                <option value="menu">菜单权限</option>
                <option value="button">按钮权限</option>
                <option value="data">数据权限</option>
                <option value="api">API 权限</option>
              </select>
            </div>
            <div class="form-group">
              <label>资源路径</label>
              <input type="text" v-model="newPerm.resource" class="form-input" placeholder="如：/api/users">
            </div>
            <div class="form-group">
              <label>描述</label>
              <textarea v-model="newPerm.description" class="form-input" rows="3" placeholder="权限描述信息"></textarea>
            </div>
          </div>
          <div class="modal-footer">
            <button class="btn btn-default" @click="showPermModal = false">取消</button>
            <button class="btn btn-primary" @click="savePerm">保存</button>
          </div>
        </div>
      </div>
    </Teleport>
  </div>
</template>

<style scoped>
.permissions-page { padding: 0; }

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

.perm-stats-grid { display: grid; grid-template-columns: repeat(4, 1fr); gap: 16px; margin-bottom: 20px; }
.perm-stat-card {
  background: var(--bg-primary);
  border-radius: 12px;
  padding: 20px 24px;
  border: 1px solid var(--border-color);
}
.perm-stat-header { display: flex; align-items: center; gap: 10px; margin-bottom: 12px; }
.perm-stat-icon {
  width: 36px;
  height: 36px;
  border-radius: 8px;
  display: flex;
  align-items: center;
  justify-content: center;
}
.perm-stat-icon svg { width: 20px; height: 20px; }
.perm-stat-icon.menu { background: rgba(59, 130, 246, 0.1); color: #3b82f6; }
.perm-stat-icon.button { background: rgba(236, 72, 153, 0.1); color: #ec4899; }
.perm-stat-icon.data { background: rgba(34, 197, 94, 0.1); color: #22c55e; }
.perm-stat-icon.api { background: rgba(139, 92, 246, 0.1); color: #8b5cf6; }
.perm-stat-label { font-size: 0.875rem; color: var(--text-secondary); }
.perm-stat-value { font-size: 2.5rem; font-weight: 700; color: var(--text-primary); line-height: 1; }

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
.desc-cell { color: var(--text-secondary); max-width: 200px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.empty-row { text-align: center; color: var(--text-muted); padding: 40px !important; }

.perm-code {
  font-family: 'Consolas', 'Monaco', monospace;
  background: rgba(59, 130, 246, 0.1);
  color: #3b82f6;
  padding: 4px 8px;
  border-radius: 4px;
  font-size: 0.8125rem;
}
.resource-path {
  font-family: 'Consolas', 'Monaco', monospace;
  font-size: 0.8125rem;
  color: var(--text-secondary);
}
.no-data { color: var(--text-muted); }

.type-badge-new {
  display: inline-block;
  padding: 4px 10px;
  border-radius: 20px;
  font-size: 0.75rem;
  font-weight: 500;
}
.type-badge-new.menu { background: rgba(59, 130, 246, 0.1); color: #3b82f6; }
.type-badge-new.button { background: rgba(236, 72, 153, 0.1); color: #ec4899; }
.type-badge-new.data { background: rgba(34, 197, 94, 0.1); color: #22c55e; }
.type-badge-new.api { background: rgba(139, 92, 246, 0.1); color: #8b5cf6; }

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
.action-btn.edit { color: #f59e0b; }
.action-btn.edit:hover { background: rgba(245, 158, 11, 0.1); border-color: #f59e0b; }
.action-btn.delete { color: #ef4444; }
.action-btn.delete:hover { background: rgba(239, 68, 68, 0.1); border-color: #ef4444; }

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
.perm-modal-overlay {
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
.perm-modal-overlay.show { opacity: 1; visibility: visible; }
.perm-modal {
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
.perm-modal .modal-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  padding: 20px 24px;
  border-bottom: 1px solid #e2e8f0;
  background: #fff;
}
.perm-modal .modal-header h3 { margin: 0; font-size: 1.125rem; font-weight: 600; color: #1e293b; }
.perm-modal .modal-close { background: none; border: none; font-size: 1.5rem; cursor: pointer; color: #94a3b8; line-height: 1; padding: 0; width: 32px; height: 32px; display: flex; align-items: center; justify-content: center; border-radius: 8px; transition: all 0.2s; }
.perm-modal .modal-close:hover { background: #f1f5f9; color: #475569; }
.perm-modal .modal-body { padding: 24px; overflow-y: auto; flex: 1; background: #fff; }
.perm-modal .modal-footer { display: flex; justify-content: flex-end; gap: 12px; padding: 16px 24px; border-top: 1px solid #e2e8f0; background: #f8fafc; }

.perm-modal .form-group { margin-bottom: 20px; }
.perm-modal .form-group label { display: block; margin-bottom: 8px; font-weight: 500; font-size: 0.875rem; color: #1e293b; }
.perm-modal .form-group .required { color: #ef4444; }
.perm-modal .form-input, .perm-modal .form-select {
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
.perm-modal .form-input:focus, .perm-modal .form-select:focus { outline: none; border-color: #3b82f6; box-shadow: 0 0 0 3px rgba(59, 130, 246, 0.1); }
.perm-modal textarea.form-input { resize: vertical; min-height: 80px; }
.perm-modal .btn { display: inline-flex; align-items: center; gap: 6px; padding: 10px 18px; border-radius: 8px; font-size: 0.875rem; font-weight: 500; cursor: pointer; border: none; transition: all 0.2s; }
.perm-modal .btn-primary { background: linear-gradient(135deg, #3b82f6 0%, #2563eb 100%); color: #fff; }
.perm-modal .btn-primary:hover { transform: translateY(-1px); box-shadow: 0 4px 12px rgba(59, 130, 246, 0.4); }
.perm-modal .btn-default { background: #f1f5f9; color: #475569; border: 1px solid #e2e8f0; }
.perm-modal .btn-default:hover { background: #e2e8f0; }
</style>
