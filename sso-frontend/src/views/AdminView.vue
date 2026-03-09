<template>
  <div class="admin-page">
    <header class="admin-header">
      <div class="header-left">
        <h1>SSO 管理后台</h1>
      </div>
      <div class="header-right">
        <button class="back-btn" @click="$router.push('/portal')">返回门户</button>
        <span class="user-info">{{ authStore.displayName }}</span>
        <button class="logout-btn" @click="handleLogout">退出</button>
      </div>
    </header>

    <div class="admin-body">
      <nav class="admin-tabs">
        <button v-for="tab in tabs" :key="tab.key" :class="['tab-btn', { active: activeTab === tab.key }]" @click="activeTab = tab.key">
          {{ tab.label }}
        </button>
      </nav>

      <main class="admin-content">
        <!-- Users Tab -->
        <div v-if="activeTab === 'users'">
          <div class="toolbar">
            <button class="primary-btn" @click="showCreateUser = true">新建用户</button>
          </div>
          <table class="data-table">
            <thead>
              <tr>
                <th>用户名</th>
                <th>显示名</th>
                <th>邮箱</th>
                <th>电话</th>
                <th>状态</th>
                <th>认证方式</th>
                <th>最后登录</th>
                <th>操作</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="u in users" :key="u.id">
                <td>{{ u.username }}</td>
                <td>{{ u.display_name }}</td>
                <td>{{ u.email || '-' }}</td>
                <td>{{ u.phone || '-' }}</td>
                <td><span :class="['status-badge', u.status]">{{ u.status === 'active' ? '正常' : u.status === 'disabled' ? '禁用' : u.status }}</span></td>
                <td>{{ u.auth_source }}</td>
                <td>{{ u.last_login_at || '-' }}</td>
                <td class="actions">
                  <button class="sm-btn" @click="editUser(u)">编辑</button>
                  <button class="sm-btn" @click="editUserRoles(u)">角色</button>
                  <button class="sm-btn" @click="editUserClients(u)">应用</button>
                  <button class="sm-btn danger" @click="deleteUser(u)">删除</button>
                </td>
              </tr>
            </tbody>
          </table>
        </div>

        <!-- Clients Tab -->
        <div v-if="activeTab === 'clients'">
          <div class="toolbar">
            <button class="primary-btn" @click="showCreateClient = true">新建应用</button>
          </div>
          <table class="data-table">
            <thead>
              <tr>
                <th>Client ID</th>
                <th>应用名称</th>
                <th>环境</th>
                <th>描述</th>
                <th>图标</th>
                <th>首页地址</th>
                <th>状态</th>
                <th>操作</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="c in clients" :key="c.client_id">
                <td><code>{{ c.client_id }}</code></td>
                <td>{{ c.client_name }}</td>
                <td><span v-if="c.environment" class="env-tag" :style="{ background: getEnvColor(c.environment) + '20', color: getEnvColor(c.environment), borderColor: getEnvColor(c.environment) + '40' }">{{ getEnvLabel(c.environment) }}</span><span v-else style="color:#555">-</span></td>
                <td>{{ c.description || '-' }}</td>
                <td><img v-if="isImageUrl(c.icon)" :src="c.icon" class="table-logo" /><span v-else>{{ c.icon || '-' }}</span></td>
                <td>{{ c.home_url || '-' }}</td>
                <td><span :class="['status-badge', c.status]">{{ c.status === 'active' ? '正常' : '禁用' }}</span></td>
                <td class="actions">
                  <button class="sm-btn" @click="editClient(c)">编辑</button>
                  <button class="sm-btn danger" @click="deleteClient(c)">删除</button>
                </td>
              </tr>
            </tbody>
          </table>
        </div>

        <!-- Environments Tab -->
        <div v-if="activeTab === 'environments'">
          <div class="toolbar">
            <button class="primary-btn" @click="showCreateEnv = true">新建环境</button>
          </div>
          <table class="data-table">
            <thead>
              <tr>
                <th>环境名称</th>
                <th>显示标签</th>
                <th>颜色</th>
                <th>排序</th>
                <th>操作</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="e in environments" :key="e.id">
                <td><code>{{ e.name }}</code></td>
                <td><span class="env-tag" :style="{ background: e.color + '20', color: e.color, borderColor: e.color + '40' }">{{ e.label }}</span></td>
                <td><span class="color-preview" :style="{ background: e.color }"></span> {{ e.color }}</td>
                <td>{{ e.sort_order }}</td>
                <td class="actions">
                  <button class="sm-btn" @click="editEnv(e)">编辑</button>
                  <button class="sm-btn danger" @click="deleteEnv(e)">删除</button>
                </td>
              </tr>
            </tbody>
          </table>
        </div>

        <!-- Roles Tab -->
        <div v-if="activeTab === 'roles'">
          <table class="data-table">
            <thead>
              <tr>
                <th>角色代码</th>
                <th>角色名称</th>
                <th>描述</th>
                <th>系统角色</th>
                <th>状态</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="r in roles" :key="r.id">
                <td><code>{{ r.code }}</code></td>
                <td>{{ r.name }}</td>
                <td>{{ r.description || '-' }}</td>
                <td>{{ r.is_system ? '是' : '否' }}</td>
                <td><span :class="['status-badge', r.status]">{{ r.status === 'active' ? '正常' : '禁用' }}</span></td>
              </tr>
            </tbody>
          </table>
        </div>

        <!-- Audit Logs Tab -->
        <div v-if="activeTab === 'audit'">
          <table class="data-table">
            <thead>
              <tr>
                <th>时间</th>
                <th>用户</th>
                <th>操作</th>
                <th>目标</th>
                <th>详情</th>
                <th>IP</th>
                <th>结果</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="l in auditLogs" :key="l.id">
                <td>{{ l.created_at }}</td>
                <td>{{ l.username || l.user_id || '-' }}</td>
                <td>{{ l.action }}</td>
                <td>{{ l.target_type }}{{ l.target_id ? '/' + l.target_id : '' }}</td>
                <td>{{ l.detail || '-' }}</td>
                <td>{{ l.ip || '-' }}</td>
                <td><span :class="['status-badge', l.status === 'success' ? 'active' : 'disabled']">{{ l.status }}</span></td>
              </tr>
            </tbody>
          </table>
        </div>
      </main>
    </div>

    <!-- Create/Edit User Modal -->
    <div v-if="showCreateUser || editingUser" class="modal-overlay" @click.self="closeUserModal">
      <div class="modal">
        <h3>{{ editingUser ? '编辑用户' : '新建用户' }}</h3>
        <div class="form-group">
          <label>用户名</label>
          <input v-model="userForm.username" :disabled="!!editingUser" placeholder="username" />
        </div>
        <div class="form-group">
          <label>{{ editingUser ? '新密码（留空不修改）' : '密码' }}</label>
          <input v-model="userForm.password" type="password" placeholder="password" />
        </div>
        <div class="form-group">
          <label>显示名</label>
          <input v-model="userForm.display_name" placeholder="显示名称" />
        </div>
        <div class="form-group">
          <label>邮箱</label>
          <input v-model="userForm.email" placeholder="email" />
        </div>
        <div class="form-group">
          <label>电话</label>
          <input v-model="userForm.phone" placeholder="phone" />
        </div>
        <div class="form-group">
          <label>状态</label>
          <select v-model="userForm.status">
            <option value="active">正常</option>
            <option value="disabled">禁用</option>
          </select>
        </div>
        <div class="modal-actions">
          <button class="primary-btn" @click="saveUser">保存</button>
          <button class="cancel-btn" @click="closeUserModal">取消</button>
        </div>
      </div>
    </div>

    <!-- Create/Edit Client Modal -->
    <div v-if="showCreateClient || editingClient" class="modal-overlay" @click.self="closeClientModal">
      <div class="modal">
        <h3>{{ editingClient ? '编辑应用' : '新建应用' }}</h3>
        <div class="form-group">
          <label>Client ID</label>
          <input v-model="clientForm.client_id" :disabled="!!editingClient" placeholder="client-id" />
        </div>
        <div class="form-group">
          <label>{{ editingClient ? 'Client Secret（留空不修改）' : 'Client Secret' }}</label>
          <input v-model="clientForm.client_secret" placeholder="secret" />
        </div>
        <div class="form-group">
          <label>应用名称</label>
          <input v-model="clientForm.client_name" placeholder="应用名称" />
        </div>
        <div class="form-group">
          <label>描述</label>
          <input v-model="clientForm.description" placeholder="描述" />
        </div>
        <div class="form-group">
          <label>应用图标</label>
          <div class="logo-upload-area">
            <div v-if="clientForm.icon && isImageUrl(clientForm.icon)" class="logo-preview">
              <img :src="clientForm.icon" alt="Logo" />
              <button type="button" class="sm-btn danger" @click="clientForm.icon = ''">移除</button>
            </div>
            <div class="upload-dropzone" @click="$refs.fileInput.click()" @dragover.prevent @drop.prevent="handleDrop">
              <input ref="fileInput" type="file" accept="image/*" @change="handleFileSelect" style="display:none" />
              <div v-if="uploadingLogo" class="upload-placeholder">上传中...</div>
              <div v-else class="upload-placeholder">
                <span class="upload-icon">+</span>
                <span>点击或拖拽上传图标</span>
                <span class="upload-hint">PNG, JPG, SVG, WebP (最大 2MB)</span>
              </div>
            </div>
            <input v-model="clientForm.icon" placeholder="或输入图标 URL / 名称 (settings, shield...)" />
          </div>
        </div>
        <div class="form-group">
          <label>所属环境</label>
          <select v-model="clientForm.environment">
            <option value="">未分类</option>
            <option v-for="env in environments" :key="env.name" :value="env.name">{{ env.label }} ({{ env.name }})</option>
          </select>
        </div>
        <div class="form-group">
          <label>首页地址</label>
          <input v-model="clientForm.home_url" placeholder="http://..." />
        </div>
        <div class="form-group">
          <label>Redirect URIs (JSON数组)</label>
          <textarea v-model="clientForm.redirect_uris" rows="3" placeholder='["http://localhost/callback"]'></textarea>
        </div>
        <div v-if="editingClient" class="form-group">
          <label>状态</label>
          <select v-model="clientForm.status">
            <option value="active">正常</option>
            <option value="disabled">禁用</option>
          </select>
        </div>
        <div class="modal-actions">
          <button class="primary-btn" @click="saveClient">保存</button>
          <button class="cancel-btn" @click="closeClientModal">取消</button>
        </div>
      </div>
    </div>

    <!-- Assign Roles Modal -->
    <div v-if="assigningRolesUser" class="modal-overlay" @click.self="assigningRolesUser = null">
      <div class="modal">
        <h3>分配角色 - {{ assigningRolesUser.username }}</h3>
        <div v-for="r in roles" :key="r.id" class="checkbox-row">
          <label>
            <input type="checkbox" :value="r.id" v-model="selectedRoleIDs" />
            {{ r.name }} ({{ r.code }})
          </label>
        </div>
        <div class="modal-actions">
          <button class="primary-btn" @click="saveUserRoles">保存</button>
          <button class="cancel-btn" @click="assigningRolesUser = null">取消</button>
        </div>
      </div>
    </div>

    <!-- Assign Clients Modal -->
    <div v-if="assigningClientsUser" class="modal-overlay" @click.self="assigningClientsUser = null">
      <div class="modal">
        <h3>分配应用 - {{ assigningClientsUser.username }}</h3>
        <div v-for="c in clients" :key="c.client_id" class="checkbox-row">
          <label>
            <input type="checkbox" :value="c.client_id" v-model="selectedClientIDs" />
            {{ c.client_name }} ({{ c.client_id }})
          </label>
        </div>
        <div class="modal-actions">
          <button class="primary-btn" @click="saveUserClients">保存</button>
          <button class="cancel-btn" @click="assigningClientsUser = null">取消</button>
        </div>
      </div>
    </div>

    <!-- Create/Edit Environment Modal -->
    <div v-if="showCreateEnv || editingEnv" class="modal-overlay" @click.self="closeEnvModal">
      <div class="modal">
        <h3>{{ editingEnv ? '编辑环境' : '新建环境' }}</h3>
        <div class="form-group">
          <label>环境名称 (英文标识)</label>
          <input v-model="envForm.name" placeholder="如: prod / uat / dev / staging" />
        </div>
        <div class="form-group">
          <label>显示标签</label>
          <input v-model="envForm.label" placeholder="如: 生产环境 / UAT环境 / 开发环境" />
        </div>
        <div class="form-group">
          <label>颜色</label>
          <div style="display:flex;gap:8px;align-items:center">
            <input type="color" v-model="envForm.color" style="width:40px;height:32px;padding:2px;border-radius:4px;border:1px solid #444;background:#0f0f23;cursor:pointer" />
            <input v-model="envForm.color" placeholder="#ef4444" style="flex:1" />
            <div class="color-presets">
              <span v-for="c in ['#ef4444','#f59e0b','#3b82f6','#22c55e','#8b5cf6','#ec4899']" :key="c" class="color-dot" :style="{ background: c }" @click="envForm.color = c"></span>
            </div>
          </div>
        </div>
        <div class="form-group">
          <label>排序 (数字越小越靠前)</label>
          <input type="number" v-model.number="envForm.sort_order" />
        </div>
        <div class="modal-actions">
          <button class="primary-btn" @click="saveEnv">保存</button>
          <button class="cancel-btn" @click="closeEnvModal">取消</button>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup>
import { ref, onMounted, watch } from 'vue'
import { useRouter } from 'vue-router'
import { useAuthStore } from '../stores/auth'
import api from '../api'

const router = useRouter()
const authStore = useAuthStore()

const tabs = [
  { key: 'users', label: '用户管理' },
  { key: 'clients', label: '应用管理' },
  { key: 'environments', label: '环境管理' },
  { key: 'roles', label: '角色管理' },
  { key: 'audit', label: '审计日志' }
]
const activeTab = ref('users')

const users = ref([])
const clients = ref([])
const roles = ref([])
const auditLogs = ref([])
const environments = ref([])

// User modal
const showCreateUser = ref(false)
const editingUser = ref(null)
const userForm = ref({ username: '', password: '', display_name: '', email: '', phone: '', status: 'active' })

// Client modal
const showCreateClient = ref(false)
const editingClient = ref(null)
const clientForm = ref({ client_id: '', client_secret: '', client_name: '', description: '', icon: '', home_url: '', redirect_uris: '[]', status: 'active', environment: '' })
const uploadingLogo = ref(false)

// Environment modal
const showCreateEnv = ref(false)
const editingEnv = ref(null)
const envForm = ref({ name: '', label: '', color: '#6b7280', sort_order: 0 })

function isImageUrl(icon) {
  return icon && (icon.startsWith('/') || icon.startsWith('http'))
}

async function uploadLogo(file) {
  if (file.size > 2 * 1024 * 1024) { alert('文件大小不能超过 2MB'); return }
  uploadingLogo.value = true
  try {
    const formData = new FormData()
    formData.append('file', file)
    const res = await api.post('/api/admin/upload', formData, { headers: { 'Content-Type': 'multipart/form-data' } })
    clientForm.value.icon = res.data.data.url
  } catch (e) {
    alert('上传失败: ' + (e.response?.data?.message || e.message))
  } finally {
    uploadingLogo.value = false
  }
}

function handleFileSelect(e) {
  const file = e.target.files[0]
  if (file) uploadLogo(file)
  e.target.value = ''
}

function handleDrop(e) {
  const file = e.dataTransfer.files[0]
  if (file && file.type.startsWith('image/')) uploadLogo(file)
}

// Role assignment
const assigningRolesUser = ref(null)
const selectedRoleIDs = ref([])

// Client assignment
const assigningClientsUser = ref(null)
const selectedClientIDs = ref([])

async function loadUsers() {
  try {
    const res = await api.get('/api/admin/users')
    users.value = res.data.data || []
  } catch (e) { console.error(e) }
}

async function loadClients() {
  try {
    const res = await api.get('/api/admin/clients')
    clients.value = res.data.data || []
  } catch (e) { console.error(e) }
}

async function loadRoles() {
  try {
    const res = await api.get('/api/admin/roles')
    roles.value = res.data.data || []
  } catch (e) { console.error(e) }
}

async function loadAuditLogs() {
  try {
    const res = await api.get('/api/admin/audit-logs')
    auditLogs.value = res.data.data || []
  } catch (e) { console.error(e) }
}

async function loadEnvironments() {
  try {
    const res = await api.get('/api/admin/environments')
    environments.value = res.data.data || []
  } catch (e) { console.error(e) }
}

watch(activeTab, (tab) => {
  if (tab === 'users') loadUsers()
  else if (tab === 'clients') { loadClients(); loadEnvironments() }
  else if (tab === 'environments') loadEnvironments()
  else if (tab === 'roles') loadRoles()
  else if (tab === 'audit') loadAuditLogs()
})

// User CRUD
function editUser(u) {
  editingUser.value = u
  userForm.value = { username: u.username, password: '', display_name: u.display_name, email: u.email, phone: u.phone, status: u.status }
}

function closeUserModal() {
  showCreateUser.value = false
  editingUser.value = null
  userForm.value = { username: '', password: '', display_name: '', email: '', phone: '', status: 'active' }
}

async function saveUser() {
  try {
    if (editingUser.value) {
      await api.put(`/api/admin/users/${editingUser.value.id}`, userForm.value)
    } else {
      await api.post('/api/admin/users', userForm.value)
    }
    closeUserModal()
    loadUsers()
  } catch (e) {
    alert(e.response?.data?.message || '操作失败')
  }
}

async function deleteUser(u) {
  if (!confirm(`确定删除用户 "${u.username}" 吗？`)) return
  try {
    await api.delete(`/api/admin/users/${u.id}`)
    loadUsers()
  } catch (e) {
    alert(e.response?.data?.message || '删除失败')
  }
}

// Role assignment
async function editUserRoles(u) {
  assigningRolesUser.value = u
  try {
    const res = await api.get(`/api/admin/users/${u.id}/roles`)
    selectedRoleIDs.value = res.data.data || []
  } catch (e) {
    selectedRoleIDs.value = []
  }
  if (roles.value.length === 0) await loadRoles()
}

async function saveUserRoles() {
  try {
    await api.put(`/api/admin/users/${assigningRolesUser.value.id}/roles`, { role_ids: selectedRoleIDs.value })
    assigningRolesUser.value = null
  } catch (e) {
    alert(e.response?.data?.message || '分配失败')
  }
}

// Client assignment
async function editUserClients(u) {
  assigningClientsUser.value = u
  try {
    const res = await api.get(`/api/admin/users/${u.id}/clients`)
    selectedClientIDs.value = res.data.data || []
  } catch (e) {
    selectedClientIDs.value = []
  }
  if (clients.value.length === 0) await loadClients()
}

async function saveUserClients() {
  try {
    await api.put(`/api/admin/users/${assigningClientsUser.value.id}/clients`, { client_ids: selectedClientIDs.value })
    assigningClientsUser.value = null
  } catch (e) {
    alert(e.response?.data?.message || '分配失败')
  }
}

// Client CRUD
function editClient(c) {
  editingClient.value = c
  clientForm.value = { client_id: c.client_id, client_secret: '', client_name: c.client_name, description: c.description, icon: c.icon, home_url: c.home_url, redirect_uris: c.redirect_uris, status: c.status, environment: c.environment || '' }
}

function closeClientModal() {
  showCreateClient.value = false
  editingClient.value = null
  clientForm.value = { client_id: '', client_secret: '', client_name: '', description: '', icon: '', home_url: '', redirect_uris: '[]', status: 'active', environment: '' }
}

async function saveClient() {
  try {
    if (editingClient.value) {
      await api.put(`/api/admin/clients/${editingClient.value.client_id}`, clientForm.value)
    } else {
      await api.post('/api/admin/clients', clientForm.value)
    }
    closeClientModal()
    loadClients()
  } catch (e) {
    alert(e.response?.data?.message || '操作失败')
  }
}

async function deleteClient(c) {
  if (!confirm(`确定删除应用 "${c.client_name}" 吗？`)) return
  try {
    await api.delete(`/api/admin/clients/${c.client_id}`)
    loadClients()
  } catch (e) {
    alert(e.response?.data?.message || '删除失败')
  }
}

// Environment CRUD
function editEnv(e) {
  editingEnv.value = e
  envForm.value = { name: e.name, label: e.label, color: e.color, sort_order: e.sort_order }
}

function closeEnvModal() {
  showCreateEnv.value = false
  editingEnv.value = null
  envForm.value = { name: '', label: '', color: '#6b7280', sort_order: 0 }
}

async function saveEnv() {
  try {
    if (editingEnv.value) {
      await api.put(`/api/admin/environments/${editingEnv.value.id}`, envForm.value)
    } else {
      await api.post('/api/admin/environments', envForm.value)
    }
    closeEnvModal()
    loadEnvironments()
  } catch (e) {
    alert(e.response?.data?.message || '操作失败')
  }
}

async function deleteEnv(e) {
  if (!confirm(`确定删除环境 "${e.label}" 吗？`)) return
  try {
    await api.delete(`/api/admin/environments/${e.id}`)
    loadEnvironments()
  } catch (e2) {
    alert(e2.response?.data?.message || '删除失败')
  }
}

function getEnvLabel(envName) {
  const e = environments.value.find(x => x.name === envName)
  return e ? e.label : envName || '-'
}

function getEnvColor(envName) {
  const e = environments.value.find(x => x.name === envName)
  return e ? e.color : '#6b7280'
}

async function handleLogout() {
  await authStore.logout()
  router.push('/login')
}

onMounted(() => {
  loadUsers()
  loadRoles()
  loadClients()
  loadEnvironments()
})
</script>

<style scoped>
.admin-page {
  min-height: 100vh;
  background: #0f0f23;
  color: #e0e0e0;
}

.admin-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 12px 24px;
  background: #1a1a2e;
  border-bottom: 1px solid #333;
}

.admin-header h1 {
  font-size: 18px;
  margin: 0;
  color: #fff;
}

.header-right {
  display: flex;
  align-items: center;
  gap: 10px;
}

.user-info { color: #999; font-size: 13px; }

.back-btn, .logout-btn {
  padding: 5px 14px;
  border-radius: 6px;
  border: none;
  font-size: 13px;
  cursor: pointer;
}

.back-btn { background: #667eea; color: #fff; }
.back-btn:hover { background: #5a6fd6; }
.logout-btn { background: #333; color: #ccc; }
.logout-btn:hover { background: #444; }

.admin-body {
  max-width: 1400px;
  margin: 0 auto;
  padding: 20px 24px;
}

.admin-tabs {
  display: flex;
  gap: 4px;
  margin-bottom: 20px;
  border-bottom: 1px solid #333;
  padding-bottom: 0;
}

.tab-btn {
  padding: 8px 20px;
  background: transparent;
  color: #888;
  border: none;
  border-bottom: 2px solid transparent;
  cursor: pointer;
  font-size: 14px;
  transition: all 0.2s;
}

.tab-btn:hover { color: #ccc; }
.tab-btn.active { color: #667eea; border-bottom-color: #667eea; }

.toolbar {
  margin-bottom: 16px;
}

.primary-btn {
  padding: 7px 18px;
  background: #667eea;
  color: #fff;
  border: none;
  border-radius: 6px;
  cursor: pointer;
  font-size: 13px;
}

.primary-btn:hover { background: #5a6fd6; }

.data-table {
  width: 100%;
  border-collapse: collapse;
  font-size: 13px;
}

.data-table th {
  text-align: left;
  padding: 10px 12px;
  background: #1a1a2e;
  color: #999;
  font-weight: 500;
  border-bottom: 1px solid #333;
  white-space: nowrap;
}

.data-table td {
  padding: 10px 12px;
  border-bottom: 1px solid #222;
  max-width: 200px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.data-table tr:hover td { background: #1a1a2e; }

.data-table code {
  background: #2a2a3e;
  padding: 2px 6px;
  border-radius: 3px;
  font-size: 12px;
}

.actions {
  display: flex;
  gap: 6px;
  white-space: nowrap;
}

.sm-btn {
  padding: 3px 10px;
  background: #2a2a3e;
  color: #ccc;
  border: 1px solid #444;
  border-radius: 4px;
  cursor: pointer;
  font-size: 12px;
}

.sm-btn:hover { background: #3a3a4e; }
.sm-btn.danger { color: #f87171; border-color: #7f1d1d; }
.sm-btn.danger:hover { background: #7f1d1d; color: #fff; }

.status-badge {
  padding: 2px 8px;
  border-radius: 10px;
  font-size: 12px;
}

.status-badge.active { background: #065f46; color: #34d399; }
.status-badge.disabled { background: #7f1d1d; color: #f87171; }

/* Modal */
.modal-overlay {
  position: fixed;
  inset: 0;
  background: rgba(0, 0, 0, 0.6);
  display: flex;
  align-items: center;
  justify-content: center;
  z-index: 1000;
}

.modal {
  background: #1a1a2e;
  border: 1px solid #333;
  border-radius: 12px;
  padding: 24px;
  width: 480px;
  max-height: 80vh;
  overflow-y: auto;
}

.modal h3 {
  margin: 0 0 20px;
  font-size: 18px;
  color: #fff;
}

.form-group {
  margin-bottom: 14px;
}

.form-group label {
  display: block;
  margin-bottom: 4px;
  color: #999;
  font-size: 13px;
}

.form-group input, .form-group select, .form-group textarea {
  width: 100%;
  padding: 8px 12px;
  background: #0f0f23;
  border: 1px solid #444;
  border-radius: 6px;
  color: #e0e0e0;
  font-size: 13px;
  box-sizing: border-box;
}

.form-group input:focus, .form-group select:focus, .form-group textarea:focus {
  outline: none;
  border-color: #667eea;
}

.form-group textarea {
  font-family: monospace;
  resize: vertical;
}

.modal-actions {
  display: flex;
  gap: 10px;
  justify-content: flex-end;
  margin-top: 20px;
}

.cancel-btn {
  padding: 7px 18px;
  background: #333;
  color: #ccc;
  border: none;
  border-radius: 6px;
  cursor: pointer;
  font-size: 13px;
}

.cancel-btn:hover { background: #444; }

.checkbox-row {
  padding: 6px 0;
}

.checkbox-row label {
  display: flex;
  align-items: center;
  gap: 8px;
  color: #ccc;
  font-size: 13px;
  cursor: pointer;
}

.checkbox-row input[type="checkbox"] {
  accent-color: #667eea;
}

/* Logo upload */
.logo-upload-area { display: flex; flex-direction: column; gap: 8px; }
.logo-preview { display: flex; align-items: center; gap: 12px; }
.logo-preview img { width: 48px; height: 48px; object-fit: contain; border-radius: 8px; background: rgba(255,255,255,0.05); border: 1px solid #333; }
.upload-dropzone { border: 2px dashed #444; border-radius: 8px; padding: 16px; text-align: center; cursor: pointer; transition: border-color 0.2s; }
.upload-dropzone:hover { border-color: #667eea; }
.upload-placeholder { display: flex; flex-direction: column; align-items: center; gap: 4px; color: #666; font-size: 13px; }
.upload-icon { font-size: 24px; color: #555; }
.upload-hint { font-size: 11px; color: #555; }
.table-logo { width: 28px; height: 28px; object-fit: contain; border-radius: 4px; vertical-align: middle; }

/* Environment styles */
.env-tag { display: inline-block; padding: 2px 10px; border-radius: 10px; font-size: 12px; font-weight: 500; border: 1px solid; white-space: nowrap; }
.color-preview { display: inline-block; width: 14px; height: 14px; border-radius: 3px; vertical-align: middle; margin-right: 4px; }
.color-presets { display: flex; gap: 4px; }
.color-dot { width: 20px; height: 20px; border-radius: 4px; cursor: pointer; border: 2px solid transparent; transition: border-color 0.15s; }
.color-dot:hover { border-color: #fff; }
</style>
