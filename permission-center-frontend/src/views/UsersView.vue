<script setup>
import { ref, onMounted } from 'vue'
import api from '@/api'
import { useAppStore } from '@/stores/app'

const appStore = useAppStore()
const users = ref([])
const roles = ref([])
const loading = ref(false)
const keyword = ref('')

// Role assignment dialog
const showRoleDialog = ref(false)
const selectedUser = ref(null)
const selectedRoleIds = ref([])

async function loadUsers() {
  loading.value = true
  try {
    const params = keyword.value ? `?keyword=${keyword.value}` : ''
    const res = await api.get('/api/admin/users' + params)
    users.value = res.data?.data || []
  } catch (e) {
    appStore.showToast('加载用户失败', 'error')
  } finally {
    loading.value = false
  }
}

async function loadRoles() {
  try {
    const res = await api.get('/api/admin/roles')
    roles.value = res.data?.data || []
  } catch (e) {}
}

function openRoleDialog(user) {
  selectedUser.value = user
  // Parse current roles
  const roleNames = (user.role_names || '').split(',').filter(Boolean)
  selectedRoleIds.value = roles.value
    .filter(r => roleNames.includes(r.name))
    .map(r => r.id)
  showRoleDialog.value = true

  // Load actual role IDs
  api.get(`/api/admin/users/${user.id}/roles`).then(res => {
    const data = res.data?.data || []
    selectedRoleIds.value = data.map(r => r.role_id)
  })
}

async function saveRoles() {
  try {
    await api.put(`/api/admin/users/${selectedUser.value.id}/roles`, selectedRoleIds.value)
    appStore.showToast('角色更新成功', 'success')
    showRoleDialog.value = false
    loadUsers()
  } catch (e) {
    appStore.showToast('更新失败', 'error')
  }
}

function toggleRole(roleId) {
  const idx = selectedRoleIds.value.indexOf(roleId)
  if (idx >= 0) {
    selectedRoleIds.value.splice(idx, 1)
  } else {
    selectedRoleIds.value.push(roleId)
  }
}

// Password reset dialog
const showPasswordDialog = ref(false)
const passwordUser = ref(null)
const newPassword = ref('')

function openPasswordDialog(user) {
  passwordUser.value = user
  newPassword.value = ''
  showPasswordDialog.value = true
}

async function resetPassword() {
  if (!newPassword.value || newPassword.value.length < 6) {
    appStore.showToast('密码至少6位', 'error')
    return
  }
  try {
    await api.post(`/api/admin/users/${passwordUser.value.id}/reset-password`, {
      password: newPassword.value
    })
    appStore.showToast('密码重置成功', 'success')
    showPasswordDialog.value = false
  } catch (e) {
    appStore.showToast(e.response?.data?.message || '重置失败', 'error')
  }
}

async function deleteUser(user) {
  if (!confirm(`确定删除用户 ${user.username}？`)) return
  try {
    await api.delete(`/api/admin/users/${user.id}`)
    appStore.showToast('删除成功', 'success')
    loadUsers()
  } catch (e) {
    appStore.showToast(e.response?.data?.message || '删除失败', 'error')
  }
}

onMounted(() => {
  loadUsers()
  loadRoles()
})
</script>

<template>
  <div class="page">
    <!-- Toolbar -->
    <div class="toolbar">
      <input v-model="keyword" placeholder="搜索用户名/显示名..." @keyup.enter="loadUsers" class="search-input" />
      <button class="btn btn-primary" @click="loadUsers">搜索</button>
    </div>

    <!-- Table -->
    <div class="table-wrap">
      <table>
        <thead>
          <tr>
            <th>用户名</th>
            <th>显示名</th>
            <th>邮箱</th>
            <th>来源</th>
            <th>角色</th>
            <th>状态</th>
            <th>创建时间</th>
            <th>操作</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="user in users" :key="user.id">
            <td>{{ user.username }}</td>
            <td>{{ user.display_name }}</td>
            <td>{{ user.email || '-' }}</td>
            <td><span class="badge">{{ user.source }}</span></td>
            <td>{{ user.role_names || '-' }}</td>
            <td><span class="status" :class="user.status">{{ user.status }}</span></td>
            <td>{{ user.created_at }}</td>
            <td>
              <button class="btn-sm" @click="openRoleDialog(user)">分配角色</button>
              <button class="btn-sm" @click="openPasswordDialog(user)" v-if="user.source === 'local'">重置密码</button>
              <button class="btn-sm btn-danger" @click="deleteUser(user)" v-if="user.username !== 'admin'">删除</button>
            </td>
          </tr>
          <tr v-if="users.length === 0">
            <td colspan="8" class="empty">{{ loading ? '加载中...' : '暂无数据' }}</td>
          </tr>
        </tbody>
      </table>
    </div>

    <!-- Role Assignment Dialog -->
    <div class="modal-overlay" v-if="showRoleDialog" @click="showRoleDialog = false">
      <div class="modal" @click.stop>
        <h3>分配角色 — {{ selectedUser?.username }}</h3>
        <div class="role-list">
          <label v-for="role in roles" :key="role.id" class="role-item">
            <input type="checkbox" :checked="selectedRoleIds.includes(role.id)" @change="toggleRole(role.id)" />
            <span>{{ role.name }}</span>
            <small>{{ role.code }}</small>
          </label>
        </div>
        <div class="modal-actions">
          <button class="btn" @click="showRoleDialog = false">取消</button>
          <button class="btn btn-primary" @click="saveRoles">保存</button>
        </div>
      </div>
    </div>

    <!-- Password Reset Dialog -->
    <div class="modal-overlay" v-if="showPasswordDialog" @click="showPasswordDialog = false">
      <div class="modal" @click.stop>
        <h3>重置密码 — {{ passwordUser?.username }}</h3>
        <div class="form-group">
          <label>新密码</label>
          <input v-model="newPassword" type="password" placeholder="新密码（至少6位）" @keyup.enter="resetPassword" />
        </div>
        <div class="modal-actions">
          <button class="btn" @click="showPasswordDialog = false">取消</button>
          <button class="btn btn-primary" @click="resetPassword">确定</button>
        </div>
      </div>
    </div>
  </div>
</template>

<style scoped>
.page { max-width: 1200px; }
.toolbar { display: flex; gap: 8px; margin-bottom: 16px; }
.search-input {
  padding: 8px 14px; background: #1e293b; border: 1px solid rgba(255,255,255,0.1);
  border-radius: 6px; color: #e2e8f0; font-size: 14px; width: 280px; outline: none;
}
.search-input:focus { border-color: #3b82f6; }

.table-wrap { background: #1e293b; border-radius: 10px; border: 1px solid rgba(255,255,255,0.06); overflow-x: auto; }
table { width: 100%; border-collapse: collapse; }
th { padding: 12px 16px; text-align: left; font-size: 12px; color: #64748b; font-weight: 600; text-transform: uppercase; border-bottom: 1px solid rgba(255,255,255,0.06); }
td { padding: 12px 16px; font-size: 14px; color: #cbd5e1; border-bottom: 1px solid rgba(255,255,255,0.04); }
tr:hover td { background: rgba(255,255,255,0.02); }
.empty { text-align: center; color: #64748b; padding: 40px !important; }

.badge { padding: 2px 8px; background: rgba(59,130,246,0.15); color: #60a5fa; border-radius: 4px; font-size: 12px; }
.status { padding: 2px 8px; border-radius: 4px; font-size: 12px; }
.status.active { background: rgba(16,185,129,0.15); color: #34d399; }
.status.disabled { background: rgba(239,68,68,0.15); color: #f87171; }

.btn { padding: 8px 16px; border: 1px solid rgba(255,255,255,0.1); background: #1e293b; color: #cbd5e1; border-radius: 6px; cursor: pointer; font-size: 13px; }
.btn:hover { background: #334155; }
.btn-primary { background: #3b82f6; border-color: #3b82f6; color: white; }
.btn-primary:hover { background: #2563eb; }
.btn-sm { padding: 4px 10px; font-size: 12px; background: transparent; border: 1px solid rgba(255,255,255,0.1); color: #94a3b8; border-radius: 4px; cursor: pointer; margin-right: 4px; }
.btn-sm:hover { background: rgba(255,255,255,0.06); }
.btn-danger { color: #f87171; border-color: rgba(239,68,68,0.3); }
.btn-danger:hover { background: rgba(239,68,68,0.1); }

.modal-overlay { position: fixed; inset: 0; background: rgba(0,0,0,0.6); display: flex; align-items: center; justify-content: center; z-index: 1000; }
.modal { background: #1e293b; border: 1px solid rgba(255,255,255,0.1); border-radius: 12px; padding: 24px; min-width: 400px; max-width: 500px; }
.modal h3 { margin: 0 0 16px; font-size: 16px; color: #f1f5f9; }
.modal-actions { display: flex; justify-content: flex-end; gap: 8px; margin-top: 20px; }

.form-group { margin-bottom: 12px; }
.form-group label { display: block; font-size: 13px; color: #94a3b8; margin-bottom: 4px; }
.form-group input { width: 100%; padding: 8px 12px; background: #0f172a; border: 1px solid rgba(255,255,255,0.1); border-radius: 6px; color: #e2e8f0; font-size: 14px; outline: none; box-sizing: border-box; }
.form-group input:focus { border-color: #3b82f6; }

.role-list { display: flex; flex-direction: column; gap: 8px; max-height: 300px; overflow-y: auto; }
.role-item { display: flex; align-items: center; gap: 8px; padding: 8px 12px; background: rgba(255,255,255,0.04); border-radius: 6px; cursor: pointer; }
.role-item input { accent-color: #3b82f6; }
.role-item span { font-size: 14px; color: #e2e8f0; }
.role-item small { font-size: 12px; color: #64748b; margin-left: auto; }
</style>
