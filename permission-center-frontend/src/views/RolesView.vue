<script setup>
import { ref, onMounted } from 'vue'
import api from '@/api'
import { useAppStore } from '@/stores/app'

const appStore = useAppStore()
const roles = ref([])
const permissions = ref([])
const loading = ref(false)

// Create/Edit dialog
const showDialog = ref(false)
const editMode = ref(false)
const form = ref({ code: '', name: '', description: '' })

// Permission assignment dialog
const showPermDialog = ref(false)
const selectedRole = ref(null)
const selectedPermIds = ref({})

async function loadRoles() {
  loading.value = true
  try {
    const res = await api.get('/api/admin/roles')
    roles.value = res.data?.data || []
  } catch (e) {
    appStore.showToast('加载失败', 'error')
  } finally {
    loading.value = false
  }
}

async function loadPermissions() {
  try {
    const res = await api.get('/api/admin/permissions')
    permissions.value = res.data?.data || []
  } catch (e) {}
}

function openCreate() {
  editMode.value = false
  form.value = { code: '', name: '', description: '' }
  showDialog.value = true
}

function openEdit(role) {
  editMode.value = true
  form.value = { id: role.id, code: role.code, name: role.name, description: role.description, status: role.status }
  showDialog.value = true
}

async function saveRole() {
  try {
    if (editMode.value) {
      await api.put(`/api/admin/roles/${form.value.id}`, form.value)
    } else {
      await api.post('/api/admin/roles', form.value)
    }
    appStore.showToast('保存成功', 'success')
    showDialog.value = false
    loadRoles()
  } catch (e) {
    appStore.showToast(e.response?.data?.message || '保存失败', 'error')
  }
}

async function deleteRole(role) {
  if (!confirm(`确定删除角色 ${role.name}？`)) return
  try {
    await api.delete(`/api/admin/roles/${role.id}`)
    appStore.showToast('删除成功', 'success')
    loadRoles()
  } catch (e) {
    appStore.showToast(e.response?.data?.message || '删除失败', 'error')
  }
}

async function openPermDialog(role) {
  selectedRole.value = role
  try {
    const res = await api.get(`/api/admin/roles/${role.id}/permissions`)
    selectedPermIds.value = res.data?.data || {}
  } catch (e) {
    selectedPermIds.value = {}
  }
  showPermDialog.value = true
}

function togglePerm(permId) {
  if (selectedPermIds.value[permId]) {
    delete selectedPermIds.value[permId]
  } else {
    selectedPermIds.value[permId] = true
  }
}

async function savePermissions() {
  const ids = Object.keys(selectedPermIds.value).filter(k => selectedPermIds.value[k])
  try {
    await api.put(`/api/admin/roles/${selectedRole.value.id}/permissions`, ids)
    appStore.showToast('权限更新成功', 'success')
    showPermDialog.value = false
  } catch (e) {
    appStore.showToast(e.response?.data?.message || '更新失败', 'error')
  }
}

// Group permissions by service_code
function groupedPermissions() {
  const groups = {}
  for (const p of permissions.value) {
    const svc = p.service_code || '未分组'
    if (!groups[svc]) groups[svc] = []
    groups[svc].push(p)
  }
  return groups
}

onMounted(() => {
  loadRoles()
  loadPermissions()
})
</script>

<template>
  <div class="page">
    <div class="toolbar">
      <button class="btn btn-primary" @click="openCreate">+ 添加角色</button>
    </div>

    <div class="table-wrap">
      <table>
        <thead>
          <tr>
            <th>角色代码</th>
            <th>名称</th>
            <th>描述</th>
            <th>系统角色</th>
            <th>用户数</th>
            <th>状态</th>
            <th>操作</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="role in roles" :key="role.id">
            <td><code>{{ role.code }}</code></td>
            <td>{{ role.name }}</td>
            <td>{{ role.description || '-' }}</td>
            <td>{{ role.is_system ? '是' : '否' }}</td>
            <td>{{ role.user_count }}</td>
            <td><span class="status" :class="role.status">{{ role.status }}</span></td>
            <td>
              <button class="btn-sm" @click="openPermDialog(role)">分配权限</button>
              <button class="btn-sm" @click="openEdit(role)" v-if="!role.is_system">编辑</button>
              <button class="btn-sm btn-danger" @click="deleteRole(role)" v-if="!role.is_system">删除</button>
            </td>
          </tr>
          <tr v-if="roles.length === 0">
            <td colspan="7" class="empty">{{ loading ? '加载中...' : '暂无数据' }}</td>
          </tr>
        </tbody>
      </table>
    </div>

    <!-- Create/Edit Dialog -->
    <div class="modal-overlay" v-if="showDialog" @click="showDialog = false">
      <div class="modal" @click.stop>
        <h3>{{ editMode ? '编辑角色' : '添加角色' }}</h3>
        <div class="form-group" v-if="!editMode">
          <label>角色代码</label>
          <input v-model="form.code" placeholder="如 editor" />
        </div>
        <div class="form-group">
          <label>名称</label>
          <input v-model="form.name" placeholder="如 编辑员" />
        </div>
        <div class="form-group">
          <label>描述</label>
          <input v-model="form.description" placeholder="角色描述" />
        </div>
        <div class="modal-actions">
          <button class="btn" @click="showDialog = false">取消</button>
          <button class="btn btn-primary" @click="saveRole">保存</button>
        </div>
      </div>
    </div>

    <!-- Permission Assignment Dialog -->
    <div class="modal-overlay" v-if="showPermDialog" @click="showPermDialog = false">
      <div class="modal modal-lg" @click.stop>
        <h3>分配权限 — {{ selectedRole?.name }}</h3>
        <div class="perm-groups">
          <div v-for="(perms, svc) in groupedPermissions()" :key="svc" class="perm-group">
            <h4>{{ svc }}</h4>
            <div class="perm-list">
              <label v-for="p in perms" :key="p.id" class="perm-item">
                <input type="checkbox" :checked="selectedPermIds[p.id]" @change="togglePerm(p.id)" />
                <span>{{ p.name }}</span>
                <small>{{ p.code }}</small>
              </label>
            </div>
          </div>
        </div>
        <div class="modal-actions">
          <button class="btn" @click="showPermDialog = false">取消</button>
          <button class="btn btn-primary" @click="savePermissions">保存</button>
        </div>
      </div>
    </div>
  </div>
</template>

<style scoped>
.page { max-width: 1200px; }
.toolbar { display: flex; gap: 8px; margin-bottom: 16px; }
.table-wrap { background: #1e293b; border-radius: 10px; border: 1px solid rgba(255,255,255,0.06); overflow-x: auto; }
table { width: 100%; border-collapse: collapse; }
th { padding: 12px 16px; text-align: left; font-size: 12px; color: #64748b; font-weight: 600; text-transform: uppercase; border-bottom: 1px solid rgba(255,255,255,0.06); }
td { padding: 12px 16px; font-size: 14px; color: #cbd5e1; border-bottom: 1px solid rgba(255,255,255,0.04); }
tr:hover td { background: rgba(255,255,255,0.02); }
.empty { text-align: center; color: #64748b; padding: 40px !important; }
code { padding: 2px 6px; background: rgba(255,255,255,0.06); border-radius: 3px; font-size: 13px; }
.status { padding: 2px 8px; border-radius: 4px; font-size: 12px; }
.status.active { background: rgba(16,185,129,0.15); color: #34d399; }

.btn { padding: 8px 16px; border: 1px solid rgba(255,255,255,0.1); background: #1e293b; color: #cbd5e1; border-radius: 6px; cursor: pointer; font-size: 13px; }
.btn:hover { background: #334155; }
.btn-primary { background: #3b82f6; border-color: #3b82f6; color: white; }
.btn-primary:hover { background: #2563eb; }
.btn-sm { padding: 4px 10px; font-size: 12px; background: transparent; border: 1px solid rgba(255,255,255,0.1); color: #94a3b8; border-radius: 4px; cursor: pointer; margin-right: 4px; }
.btn-sm:hover { background: rgba(255,255,255,0.06); }
.btn-danger { color: #f87171; border-color: rgba(239,68,68,0.3); }
.btn-danger:hover { background: rgba(239,68,68,0.1); }

.modal-overlay { position: fixed; inset: 0; background: rgba(0,0,0,0.6); display: flex; align-items: center; justify-content: center; z-index: 1000; }
.modal { background: #1e293b; border: 1px solid rgba(255,255,255,0.1); border-radius: 12px; padding: 24px; min-width: 400px; }
.modal-lg { min-width: 600px; max-height: 80vh; overflow-y: auto; }
.modal h3 { margin: 0 0 16px; font-size: 16px; color: #f1f5f9; }
.modal-actions { display: flex; justify-content: flex-end; gap: 8px; margin-top: 20px; }

.form-group { margin-bottom: 12px; }
.form-group label { display: block; font-size: 13px; color: #94a3b8; margin-bottom: 4px; }
.form-group input { width: 100%; padding: 8px 12px; background: #0f172a; border: 1px solid rgba(255,255,255,0.1); border-radius: 6px; color: #e2e8f0; font-size: 14px; outline: none; box-sizing: border-box; }
.form-group input:focus { border-color: #3b82f6; }

.perm-groups { display: flex; flex-direction: column; gap: 16px; }
.perm-group h4 { margin: 0 0 8px; font-size: 14px; color: #3b82f6; }
.perm-list { display: flex; flex-direction: column; gap: 4px; }
.perm-item { display: flex; align-items: center; gap: 8px; padding: 6px 10px; border-radius: 4px; cursor: pointer; }
.perm-item:hover { background: rgba(255,255,255,0.04); }
.perm-item input { accent-color: #3b82f6; }
.perm-item span { font-size: 13px; color: #e2e8f0; }
.perm-item small { font-size: 11px; color: #64748b; margin-left: auto; }
</style>
