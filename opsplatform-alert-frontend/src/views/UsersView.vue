<template>
  <div>
    <div class="card">
      <div class="card-header">
        <div class="card-title">账号管理</div>
        <button class="btn btn-primary" @click="showModal = true; resetForm()">
          <UserPlus :size="16" /> 添加用户
        </button>
      </div>

      <div v-if="loading" class="loading"><div class="spinner"></div></div>

      <div v-else class="table-wrapper">
        <table>
          <thead>
            <tr>
              <th>用户名</th>
              <th>显示名</th>
              <th>来源</th>
              <th>角色</th>
              <th>状态</th>
              <th>创建时间</th>
              <th>操作</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="user in users" :key="user.id">
              <td style="font-weight: 500;">{{ user.username || '-' }}</td>
              <td>{{ user.display_name || '-' }}</td>
              <td>
                <span class="badge" :class="user.auth_source === 'portal' ? 'badge-success' : 'badge-info'">
                  {{ user.auth_source === 'portal' ? 'SSO' : '本地' }}
                </span>
              </td>
              <td>
                <span class="badge" :class="user.role === 'admin' ? 'badge-warning' : 'badge-info'">
                  {{ user.role === 'admin' ? '管理员' : '普通用户' }}
                </span>
              </td>
              <td>
                <span class="badge" :class="user.status === 1 ? 'badge-success' : 'badge-danger'">
                  {{ user.status === 1 ? '启用' : '禁用' }}
                </span>
              </td>
              <td class="text-sm">{{ formatTime(user.created_at) }}</td>
              <td>
                <div class="actions">
                  <button class="btn btn-sm btn-outline" @click="editUser(user)" title="编辑">
                    <Pencil :size="14" />
                  </button>
                  <button class="btn btn-sm btn-outline" @click="resetPassword(user)" title="重置密码">
                    <KeyRound :size="14" />
                  </button>
                  <button class="btn btn-sm btn-outline" @click="toggleUser(user)"
                    :title="user.status === 1 ? '禁用' : '启用'">
                    <ShieldOff v-if="user.status === 1" :size="14" />
                    <ShieldCheck v-else :size="14" />
                  </button>
                  <button class="btn btn-sm btn-outline" style="color: var(--danger);" @click="deleteUser(user)" title="删除">
                    <Trash2 :size="14" />
                  </button>
                </div>
              </td>
            </tr>
          </tbody>
        </table>
      </div>
    </div>

    <!-- Add/Edit User Modal -->
    <div v-if="showModal" class="modal-overlay" @click.self="showModal = false">
      <div class="modal">
        <div class="modal-header">
          <div class="modal-title">{{ editId ? '编辑用户' : '添加用户' }}</div>
          <button class="btn-icon" @click="showModal = false"><X :size="18" /></button>
        </div>
        <form @submit.prevent="handleSubmit">
          <div class="form-group">
            <label class="form-label">用户名 *</label>
            <input v-model="form.username" class="form-input" :disabled="!!editId" required />
          </div>
          <div v-if="!editId" class="form-group">
            <label class="form-label">密码 *</label>
            <input v-model="form.password" type="password" class="form-input" required />
          </div>
          <div class="form-group">
            <label class="form-label">显示名</label>
            <input v-model="form.display_name" class="form-input" />
          </div>
          <div class="form-group">
            <label class="form-label">角色</label>
            <select v-model="form.role" class="form-select">
              <option value="user">普通用户</option>
              <option value="admin">管理员</option>
            </select>
          </div>
          <div class="modal-footer">
            <button type="button" class="btn btn-outline" @click="showModal = false">取消</button>
            <button type="submit" class="btn btn-primary" :disabled="submitting">
              {{ submitting ? '保存中...' : '保存' }}
            </button>
          </div>
        </form>
      </div>
    </div>

    <!-- Reset Password Modal -->
    <div v-if="showResetPwd" class="modal-overlay" @click.self="showResetPwd = false">
      <div class="modal" style="min-width: 400px;">
        <div class="modal-header">
          <div class="modal-title">重置密码 — {{ resetPwdUser }}</div>
          <button class="btn-icon" @click="showResetPwd = false"><X :size="18" /></button>
        </div>
        <form @submit.prevent="handleResetPassword">
          <div class="form-group">
            <label class="form-label">新密码</label>
            <input v-model="newPassword" type="password" class="form-input" placeholder="请输入新密码" required />
          </div>
          <div class="modal-footer">
            <button type="button" class="btn btn-outline" @click="showResetPwd = false">取消</button>
            <button type="submit" class="btn btn-primary">确认重置</button>
          </div>
        </form>
      </div>
    </div>
  </div>
</template>

<script setup>
import { ref, onMounted } from 'vue'
import api from '../api'
import { useToast, useConfirm } from '../stores/ui'
import { UserPlus, Pencil, KeyRound, ShieldOff, ShieldCheck, Trash2, X } from 'lucide-vue-next'

const toast = useToast()
const dialog = useConfirm()

const users = ref([])
const loading = ref(false)
const showModal = ref(false)
const editId = ref(null)
const submitting = ref(false)

const showResetPwd = ref(false)
const resetPwdId = ref(null)
const resetPwdUser = ref('')
const newPassword = ref('')

const form = ref({ username: '', password: '', display_name: '', role: 'user' })

function resetForm() {
  editId.value = null
  form.value = { username: '', password: '', display_name: '', role: 'user' }
}

async function loadUsers() {
  loading.value = true
  try {
    const res = await api.get('/users')
    if (res.code === 0) users.value = res.data
  } catch (e) { /* ignore */ }
  loading.value = false
}

function editUser(user) {
  editId.value = user.id
  form.value = { username: user.username, password: '', display_name: user.display_name, role: user.role }
  showModal.value = true
}

async function handleSubmit() {
  submitting.value = true
  try {
    let res
    if (editId.value) {
      res = await api.put(`/users/${editId.value}`, { display_name: form.value.display_name, role: form.value.role })
    } else {
      res = await api.post('/users', form.value)
    }
    if (res.code === 0) {
      showModal.value = false
      toast.success(editId.value ? '用户已更新' : '用户已创建')
      loadUsers()
    } else {
      toast.error(res.message)
    }
  } catch (e) {
    toast.error(e.response?.data?.message || '操作失败')
  }
  submitting.value = false
}

function resetPassword(user) {
  resetPwdId.value = user.id
  resetPwdUser.value = user.username
  newPassword.value = ''
  showResetPwd.value = true
}

async function handleResetPassword() {
  try {
    const res = await api.post(`/users/${resetPwdId.value}/reset-password`, { password: newPassword.value })
    if (res.code === 0) {
      showResetPwd.value = false
      toast.success('密码已重置')
    } else {
      toast.error(res.message)
    }
  } catch (e) {
    toast.error(e.response?.data?.message || '重置失败')
  }
}

async function toggleUser(user) {
  const action = user.status === 1 ? '禁用' : '启用'
  const ok = await dialog.confirm({ title: `${action}用户`, message: `确认${action}用户「${user.username}」？` })
  if (!ok) return
  try {
    const res = await api.put(`/users/${user.id}/toggle`)
    if (res.code === 0) {
      user.status = res.data.status
      toast.success(`用户已${action}`)
    } else {
      toast.error(res.message)
    }
  } catch (e) {
    toast.error(e.response?.data?.message || '操作失败')
  }
}

async function deleteUser(user) {
  const ok = await dialog.danger({ title: '删除用户', message: `确认删除用户「${user.username}」？此操作不可恢复。` })
  if (!ok) return
  try {
    const res = await api.delete(`/users/${user.id}`)
    if (res.code === 0) {
      toast.success('用户已删除')
      loadUsers()
    } else {
      toast.error(res.message)
    }
  } catch (e) {
    toast.error(e.response?.data?.message || '删除失败')
  }
}

function formatTime(t) {
  if (!t) return '-'
  return new Date(t).toLocaleString('zh-CN')
}

onMounted(loadUsers)
</script>
