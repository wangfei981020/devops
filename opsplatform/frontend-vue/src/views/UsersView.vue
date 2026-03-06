<script setup>
import { ref, computed, onMounted } from 'vue'
import { useAppStore } from '@/stores'
import api from '@/api'

const appStore = useAppStore()

const users = ref([])
const roles = ref([])
const loading = ref(false)

const tempSearchQuery = ref('')
const tempFilterRole = ref('')
const tempFilterStatus = ref('')
const tempFilterMfa = ref('')

const searchQuery = ref('')
const filterRole = ref('')
const filterStatus = ref('')
const filterMfa = ref('')

const showUserModal = ref(false)
const userModalMode = ref('add')
const userEditTab = ref('basic')
const showUserPassword = ref(false)
const userForm = ref({
  id: '',
  username: '',
  password: '',
  display_name: '',
  role: 'user',
  status: 'active',
  permissions: [],
  mfa_enabled: false,
  mfa_bound: false,
  phone: '',
  email: '',
  description: '',
  session_timeout: 60,
  language: 'zh-CN'
})

const showPasswordModal = ref(false)
const showNewPassword = ref(false)
const passwordForm = ref({
  userId: '',
  username: '',
  newPassword: ''
})

const stats = computed(() => ({
  total: users.value.length,
  active: users.value.filter(u => u.status === 'active').length,
  mfaEnabled: users.value.filter(u => u.mfa_enabled).length,
  mfaBound: users.value.filter(u => u.mfa_bound).length
}))

const mfaRate = computed(() => {
  if (stats.value.total === 0) return 0
  return Math.round(stats.value.mfaBound / stats.value.total * 100)
})

const filteredUsers = computed(() => {
  let list = users.value
  if (searchQuery.value) {
    const q = searchQuery.value.toLowerCase()
    list = list.filter(u => u.username?.toLowerCase().includes(q) || u.email?.toLowerCase().includes(q))
  }
  if (filterRole.value) list = list.filter(u => u.role === filterRole.value)
  if (filterStatus.value) list = list.filter(u => u.status === filterStatus.value)
  if (filterMfa.value === 'bound') list = list.filter(u => u.mfa_bound)
  else if (filterMfa.value === 'unbound') list = list.filter(u => !u.mfa_bound)
  return list
})

onMounted(() => { loadUsers(); loadRoles() })

async function loadUsers() {
  loading.value = true
  try {
    const res = await api.get('/api/users')
    users.value = res.data || []
  } catch (e) { appStore.showToast('加载用户失败', 'error') }
  finally { loading.value = false }
}

function applyFilter() {
  searchQuery.value = tempSearchQuery.value
  filterRole.value = tempFilterRole.value
  filterStatus.value = tempFilterStatus.value
  filterMfa.value = tempFilterMfa.value
}

function resetFilter() {
  tempSearchQuery.value = ''
  tempFilterRole.value = ''
  tempFilterStatus.value = ''
  tempFilterMfa.value = ''
  searchQuery.value = ''
  filterRole.value = ''
  filterStatus.value = ''
  filterMfa.value = ''
}

async function loadRoles() {
  try { const res = await api.get('/api/roles'); roles.value = res.data || [] }
  catch (e) { console.error(e) }
}

async function openUserModal(mode, user = null) {
  if (roles.value.length === 0) await loadRoles()
  userModalMode.value = mode
  userEditTab.value = 'basic'
  showUserPassword.value = false
  if (user) {
    const perms = (user.permissions || '').split(',').map(p => p.trim()).filter(p => p)
    userForm.value = {
      id: user.id,
      username: user.username,
      password: '',
      display_name: user.display_name || '',
      role: user.role || 'user',
      status: user.status || 'active',
      permissions: perms,
      mfa_enabled: user.mfa_enabled || false,
      mfa_bound: user.mfa_bound || false,
      phone: user.phone || '',
      email: user.email || '',
      description: user.description || '',
      session_timeout: user.session_timeout || 60,
      language: user.language || 'zh-CN'
    }
  } else {
    userForm.value = { id: '', username: '', password: '', display_name: '', role: 'user', status: 'active', permissions: [], mfa_enabled: false, mfa_bound: false, phone: '', email: '', description: '', session_timeout: 60, language: 'zh-CN' }
  }
  showUserModal.value = true
}

async function saveUser() {
  if (!userForm.value.username) { appStore.showToast('请输入用户名', 'error'); return }
  if (userModalMode.value === 'add' && !userForm.value.password) { appStore.showToast('请输入密码', 'error'); return }
  try {
    const userData = {
      ...userForm.value,
      permissions: userForm.value.permissions.join(','),
      mfa_enabled: userForm.value.mfa_enabled || false,
      status: userForm.value.status || 'active'
    }
    if (!userData.password) delete userData.password
    if (userModalMode.value === 'edit') {
      await api.put('/api/users/' + userForm.value.id, userData)
      appStore.showToast('更新成功', 'success')
    } else {
      await api.post('/api/users', userData)
      appStore.showToast('创建成功', 'success')
    }
    showUserModal.value = false
    loadUsers()
  } catch (e) { appStore.showToast('保存失败: ' + (e.response?.data || e.message), 'error') }
}

async function deleteUser(user) {
  const confirmed = await appStore.showConfirm({
    type: 'danger',
    title: '删除用户',
    message: `确定要删除用户 "${user.username}" 吗？\n此操作不可恢复。`,
    okText: '删除',
    cancelText: '取消'
  })
  if (!confirmed) return
  try { await api.delete('/api/users/' + user.id); appStore.showToast('删除成功', 'success'); loadUsers() }
  catch (e) { appStore.showToast('删除失败', 'error') }
}

async function toggleUserMFA(user) {
  try {
    const newStatus = !user.mfa_enabled
    await api.put('/api/users/' + user.id, { ...user, mfa_enabled: newStatus })
    appStore.showToast(newStatus ? 'MFA 已启用' : 'MFA 已禁用', 'success')
    loadUsers()
  } catch (e) { appStore.showToast('操作失败: ' + (e.response?.data || e.message), 'error') }
}

async function confirmResetUserMFA(userId) {
  const confirmed = await appStore.showConfirm({
    type: 'warning',
    title: '重置 MFA',
    message: '确定要重置该用户的 MFA 吗？\n用户需要重新绑定认证器。',
    okText: '重置',
    cancelText: '取消'
  })
  if (!confirmed) return
  try {
    await api.post('/api/mfa/reset', { user_id: userId })
    appStore.showToast('MFA 已重置', 'success')
    loadUsers()
  } catch (e) { appStore.showToast('重置失败: ' + (e.response?.data || e.message), 'error') }
}

function handleMfaAction(user) {
  if (user.mfa_bound) confirmResetUserMFA(user.id)
  else toggleUserMFA(user)
}

async function forceEnableMFA() {
  const confirmed = await appStore.showConfirm({
    type: 'warning',
    title: '强制启用 MFA',
    message: '确定要强制所有用户启用 MFA 吗？\n未绑定的用户下次登录时将被要求绑定。',
    okText: '确定',
    cancelText: '取消'
  })
  if (!confirmed) return
  try {
    for (const user of users.value) {
      if (!user.mfa_enabled) await api.put('/api/users/' + user.id, { ...user, mfa_enabled: true })
    }
    appStore.showToast('已强制启用所有用户 MFA', 'success')
    loadUsers()
  } catch (e) { appStore.showToast('操作失败', 'error') }
}

function openPasswordModal(user) {
  passwordForm.value = {
    userId: user.id,
    username: user.username,
    newPassword: ''
  }
  showNewPassword.value = false
  showPasswordModal.value = true
}

function generatePassword() {
  const chars = 'ABCDEFGHJKLMNPQRSTUVWXYZabcdefghjkmnpqrstuvwxyz23456789!@#$%'
  let pwd = ''
  for (let i = 0; i < 12; i++) pwd += chars.charAt(Math.floor(Math.random() * chars.length))
  passwordForm.value.newPassword = pwd
  showNewPassword.value = true
  appStore.showToast('已生成随机密码', 'success')
}

function copyPassword() {
  if (!passwordForm.value.newPassword) { appStore.showToast('请先生成或输入密码', 'warning'); return }
  navigator.clipboard.writeText(passwordForm.value.newPassword)
  appStore.showToast('密码已复制到剪贴板', 'success')
}

async function submitPasswordChange() {
  if (!passwordForm.value.newPassword) { appStore.showToast('请输入新密码', 'error'); return }
  if (passwordForm.value.newPassword.length < 6) { appStore.showToast('密码长度至少6位', 'error'); return }
  try {
    await api.put('/api/users/' + passwordForm.value.userId + '/password', { password: passwordForm.value.newPassword })
    appStore.showToast('密码修改成功', 'success')
    showPasswordModal.value = false
  } catch (e) { appStore.showToast('密码修改失败: ' + (e.response?.data || e.message), 'error') }
}

function generateUserPassword() {
  const chars = 'ABCDEFGHJKLMNPQRSTUVWXYZabcdefghjkmnpqrstuvwxyz23456789!@#$%'
  let pwd = ''
  for (let i = 0; i < 12; i++) pwd += chars.charAt(Math.floor(Math.random() * chars.length))
  userForm.value.password = pwd
}

function copyUserPassword() {
  if (!userForm.value.password) return
  navigator.clipboard.writeText(userForm.value.password)
  appStore.showToast('密码已复制', 'success')
}

function getRoleName(code) {
  if (!code) return '普通用户'
  if (code === 'super_admin') return '超级管理员'
  if (code === 'admin') return '管理员'
  const r = roles.value.find(x => x.code === code)
  return r?.name || code
}

function getAvatarColor(name) {
  const colors = ['#3b82f6', '#8b5cf6', '#ec4899', '#f59e0b', '#10b981', '#6366f1']
  let hash = 0
  for (let i = 0; i < (name || 'U').length; i++) hash = name.charCodeAt(i) + ((hash << 5) - hash)
  return colors[Math.abs(hash) % colors.length]
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
  <div class="users-page">
    <div class="page-header">
      <div class="header-left">
        <h1 class="page-title">用户管理</h1>
        <span class="count-badge">{{ stats.total }}位用户</span>
      </div>
      <div class="header-right">
        <button class="btn-export">
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" width="16" height="16"><path d="M4 16v1a3 3 0 003 3h10a3 3 0 003-3v-1m-4-8l-4-4m0 0L8 8m4-4v12"/></svg>
          导出
        </button>
        <button class="btn-primary" @click="openUserModal('add')">
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" width="16" height="16"><path d="M12 6v6m0 0v6m0-6h6m-6 0H6"/></svg>
          新建用户
        </button>
      </div>
    </div>

    <div class="stats-inline">
      <div class="stat-item"><span class="stat-num">{{ stats.total }}</span><span class="stat-text">用户</span></div>
      <span class="stat-sep"></span>
      <div class="stat-item"><span class="stat-num">{{ stats.active }}</span><span class="stat-text">活跃</span></div>
      <span class="stat-sep"></span>
      <div class="stat-item"><span class="stat-num">{{ stats.mfaEnabled }}</span><span class="stat-text">已启用MFA</span></div>
    </div>

      <div class="list-panel">
        <div class="panel-header">
          <div class="panel-title"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" width="18" height="18"><path d="M20 21v-2a4 4 0 0 0-4-4H8a4 4 0 0 0-4 4v2"/><circle cx="12" cy="7" r="4"/></svg> 用户列表</div>
          <button class="refresh-btn" @click="loadUsers"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" width="16" height="16"><path d="M23 4v6h-6M1 20v-6h6"/><path d="M3.51 9a9 9 0 0114.85-3.36L23 10M1 14l4.64 4.36A9 9 0 0020.49 15"/></svg> 刷新</button>
        </div>
        <div class="filter-row">
          <div class="search-box"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" width="16" height="16"><circle cx="11" cy="11" r="8"/><path d="M21 21l-4.35-4.35"/></svg><input v-model="tempSearchQuery" placeholder="搜索用户名、邮箱..." @keyup.enter="applyFilter"></div>
          <select v-model="tempFilterRole" class="filter-select"><option value="">全部角色</option><option v-for="r in roles" :key="r.id" :value="r.code">{{ r.name }}</option></select>
          <select v-model="tempFilterStatus" class="filter-select"><option value="">全部状态</option><option value="active">活跃</option><option value="inactive">禁用</option></select>
          <select v-model="tempFilterMfa" class="filter-select"><option value="">MFA 状态</option><option value="bound">已绑定</option><option value="unbound">未绑定</option></select>
          <button class="btn-search" @click="applyFilter">搜 索</button>
          <button class="btn-reset" @click="resetFilter">重 置</button>
        </div>
        <table class="user-table">
          <thead><tr><th>用户信息</th><th>角色</th><th>状态</th><th>MFA</th><th>最后登录</th><th>操作</th></tr></thead>
          <tbody>
            <tr v-for="u in filteredUsers" :key="u.id">
              <td><div class="user-info"><span class="name">{{ u.display_name || u.username }}<span v-if="u.auth_source === 'sso'" class="auth-badge sso" title="SSO 账号">SSO</span><span v-else class="auth-badge local" title="本地账号">本地</span></span><span class="email">{{ u.email || u.username }}</span></div></td>
              <td><span class="role-badge" :class="u.role === 'super_admin' || u.role === 'admin' ? 'admin' : ''">{{ getRoleName(u.role) }}</span></td>
              <td><span class="status-cell"><span class="status-dot" :class="u.status"></span>{{ u.status === 'active' ? '活跃' : '禁用' }}</span></td>
              <td><span class="mfa-badge" :class="u.mfa_bound ? 'bound' : 'unbound'"><svg v-if="u.mfa_bound" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" width="14" height="14"><path d="M22 11.08V12a10 10 0 1 1-5.93-9.14"/><polyline points="22 4 12 14.01 9 11.01"/></svg><svg v-else viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" width="14" height="14"><rect x="3" y="11" width="18" height="11" rx="2"/><path d="M7 11V7a5 5 0 0 1 10 0v4"/></svg>{{ u.mfa_bound ? '已绑定' : '未启用' }}</span></td>
              <td class="date">{{ formatDate(u.updated_at || u.created_at) }}</td>
              <td>
                <div class="actions">
                  <button class="act-icon" @click="openUserModal('edit', u)" title="编辑"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" width="16" height="16"><path d="M11 4H4a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2h14a2 2 0 0 0 2-2v-7"/><path d="M18.5 2.5a2.121 2.121 0 0 1 3 3L12 15l-4 1 1-4 9.5-9.5z"/></svg></button>
                  <button class="act-icon" @click="handleMfaAction(u)" :title="u.mfa_bound ? '重置 MFA' : '启用 MFA'"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" width="16" height="16"><path d="M12 22s8-4 8-10V5l-8-3-8 3v7c0 6 8 10 8 10z"/></svg></button>
                  <button class="act-icon" @click="openPasswordModal(u)" title="修改密码"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" width="16" height="16"><path d="M21 2l-2 2m-7.61 7.61a5.5 5.5 0 1 1-7.778 7.778 5.5 5.5 0 0 1 7.777-7.777zm0 0L15.5 7.5m0 0l3 3L22 7l-3-3m-3.5 3.5L19 4"/></svg></button>
                  <button class="act-icon" @click="deleteUser(u)" title="删除" v-if="u.role !== 'super_admin' && u.role !== 'admin'"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" width="16" height="16"><path d="M3 6h18M19 6v14a2 2 0 0 1-2 2H7a2 2 0 0 1-2-2V6m3 0V4a2 2 0 0 1 2-2h4a2 2 0 0 1 2 2v2"/></svg></button>
                </div>
              </td>
            </tr>
          </tbody>
        </table>
        <div class="pagination"><span>每页</span><select><option>10</option><option>20</option></select><span>条，共 {{ filteredUsers.length }} 条</span><div class="page-btns"><button>首页</button><button>上一页</button><span class="page-num">1</span><button>下一页</button><button>尾页</button></div></div>
      </div>

    <!-- 用户编辑弹窗 -->
    <Teleport to="body">
      <div class="user-modal-overlay" :class="{ show: showUserModal }">
        <div class="modal user-edit-modal">
          <form @submit.prevent="saveUser" class="user-edit-form">
            <div class="user-edit-sidebar">
              <div class="sidebar-header">
                <div class="sidebar-title">{{ userModalMode === 'add' ? '添加用户' : '编辑用户' }}</div>
                <div class="user-card">
                  <div class="user-avatar" :style="{ background: getAvatarColor(userForm.username || 'U') }">{{ (userForm.display_name || userForm.username || 'U').charAt(0).toUpperCase() }}</div>
                  <div class="user-card-info"><div class="user-card-name">{{ userForm.display_name || userForm.username || '新用户' }}</div><div class="user-card-role">{{ getRoleName(userForm.role) }}</div></div>
                </div>
              </div>
              <ul class="nav-list">
                <li class="nav-item" :class="{ active: userEditTab === 'basic' }" @click="userEditTab = 'basic'"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" width="18" height="18"><path d="M20 21v-2a4 4 0 0 0-4-4H8a4 4 0 0 0-4 4v2"/><circle cx="12" cy="7" r="4"/></svg><span>基本信息</span></li>
                <li class="nav-item" :class="{ active: userEditTab === 'security' }" @click="userEditTab = 'security'"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" width="18" height="18"><rect x="3" y="11" width="18" height="11" rx="2"/><path d="M7 11V7a5 5 0 0 1 10 0v4"/></svg><span>安全设置</span></li>
                <li class="nav-item" :class="{ active: userEditTab === 'permissions' }" @click="userEditTab = 'permissions'"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" width="18" height="18"><path d="M12 22s8-4 8-10V5l-8-3-8 3v7c0 6 8 10 8 10z"/></svg><span>权限管理</span></li>
                <li class="nav-item" :class="{ active: userEditTab === 'history' }" @click="userEditTab = 'history'"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" width="18" height="18"><circle cx="12" cy="12" r="10"/><polyline points="12 6 12 12 16 14"/></svg><span>登录历史</span></li>
              </ul>
            </div>
            <div class="user-edit-content">
              <div class="content-header">
                <div><div class="content-title">{{ userEditTab === 'basic' ? '基本信息' : userEditTab === 'security' ? '安全设置' : userEditTab === 'permissions' ? '权限管理' : '登录历史' }}</div><div class="content-subtitle">{{ userEditTab === 'basic' ? '更新用户的基本资料' : userEditTab === 'security' ? '配置密码和多因素认证' : userEditTab === 'permissions' ? '管理用户的系统权限' : '查看用户的登录记录' }}</div></div>
                <button type="button" class="close-btn" @click="showUserModal = false"><svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M18 6L6 18M6 6l12 12"/></svg></button>
              </div>
              <div class="content-body">
                <!-- 基本信息 -->
                <div v-show="userEditTab === 'basic'" class="tab-content">
                  <div class="form-row">
                    <div class="form-group"><label><span class="required">*</span> 用户名</label><input type="text" v-model="userForm.username" :disabled="userModalMode === 'edit'" placeholder="请输入用户名"></div>
                    <div class="form-group"><label><span class="required">*</span> 真实姓名</label><input type="text" v-model="userForm.display_name" placeholder="请输入真实姓名"></div>
                  </div>
                  <div class="form-row">
                    <div class="form-group"><label>手机号码</label><input type="tel" v-model="userForm.phone" placeholder="请输入手机号"></div>
                    <div class="form-group"><label>电子邮箱</label><input type="email" v-model="userForm.email" placeholder="请输入邮箱"></div>
                  </div>
                  <div class="section-divider">账号状态</div>
                  <div class="status-cards">
                    <div class="status-card" :class="{ selected: userForm.status === 'active' }" @click="userForm.status = 'active'">
                      <div class="status-card-header"><span class="status-card-title">正常启用</span><span class="status-indicator"></span></div>
                      <div class="status-card-desc">账号可正常登录和使用系统</div>
                    </div>
                    <div class="status-card" :class="{ selected: userForm.status === 'inactive' }" @click="userForm.status = 'inactive'">
                      <div class="status-card-header"><span class="status-card-title">暂时禁用</span><span class="status-indicator"></span></div>
                      <div class="status-card-desc">账号暂停使用，无法登录</div>
                    </div>
                  </div>
                  <div class="section-divider">角色分配</div>
                  <div class="form-row">
                    <div class="form-group"><label>用户角色</label><select v-model="userForm.role"><option v-for="role in roles" :key="role.id" :value="role.code">{{ role.name }}</option><option v-if="roles.length === 0" value="user">普通用户</option></select></div>
                    <div class="form-group"><label>界面语言</label><select v-model="userForm.language"><option value="zh-CN">简体中文</option><option value="en-US">English</option></select></div>
                  </div>
                </div>
                <!-- 安全设置 -->
                <div v-show="userEditTab === 'security'" class="tab-content">
                  <div class="section-divider">{{ userModalMode === 'add' ? '设置密码' : '修改密码' }}</div>
                  <div class="form-row">
                    <div class="form-group full-width">
                      <label v-if="userModalMode === 'add'"><span class="required">*</span> 登录密码</label>
                      <label v-else>新密码 <span class="hint">(留空则不修改)</span></label>
                      <div class="password-field">
                        <input :type="showUserPassword ? 'text' : 'password'" v-model="userForm.password" :placeholder="userModalMode === 'add' ? '请设置登录密码' : '输入新密码'">
                        <button type="button" class="password-eye" @click="showUserPassword = !showUserPassword">
                          <svg v-if="showUserPassword" width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M1 12s4-8 11-8 11 8 11 8-4 8-11 8-11-8-11-8z"/><circle cx="12" cy="12" r="3"/></svg>
                          <svg v-else width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M17.94 17.94A10.07 10.07 0 0 1 12 20c-7 0-11-8-11-8a18.45 18.45 0 0 1 5.06-5.94M9.9 4.24A9.12 9.12 0 0 1 12 4c7 0 11 8 11 8a18.5 18.5 0 0 1-2.16 3.19m-6.72-1.07a3 3 0 1 1-4.24-4.24"/><line x1="1" y1="1" x2="23" y2="23"/></svg>
                        </button>
                      </div>
                    </div>
                  </div>
                  <div class="password-actions">
                    <button type="button" class="btn-ghost" @click="generateUserPassword"><svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><rect x="3" y="11" width="18" height="11" rx="2"/><path d="M7 11V7a5 5 0 0 1 10 0v4"/></svg> 自动生成</button>
                    <button type="button" class="btn-ghost" @click="copyUserPassword" :disabled="!userForm.password"><svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><rect x="9" y="9" width="13" height="13" rx="2"/><path d="M5 15H4a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2h9a2 2 0 0 1 2 2v1"/></svg> 一键复制</button>
                  </div>
                  <div class="section-divider">多因素认证</div>
                  <div class="switch-group">
                    <div class="switch-item">
                      <div class="switch-info">
                        <div class="switch-icon"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" width="20" height="20"><path d="M12 22s8-4 8-10V5l-8-3-8 3v7c0 6 8 10 8 10z"/></svg></div>
                        <div><div class="switch-label">启用 MFA 认证</div><div class="switch-desc">{{ userForm.mfa_enabled ? (userForm.mfa_bound ? '已绑定认证器' : '待绑定认证器') : '登录时需要动态验证码' }}</div></div>
                      </div>
                      <label class="switch"><input type="checkbox" v-model="userForm.mfa_enabled"><span class="slider"></span></label>
                    </div>
                  </div>
                  <div class="mfa-reset-action" v-if="userModalMode === 'edit' && userForm.mfa_bound">
                    <button type="button" class="btn-danger-outline" @click="confirmResetUserMFA(userForm.id)"><svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M3 6h18M19 6v14a2 2 0 0 1-2 2H7a2 2 0 0 1-2-2V6m3 0V4a2 2 0 0 1 2-2h4a2 2 0 0 1 2 2v2"/></svg> 重置 MFA 绑定</button>
                  </div>
                  <div class="section-divider">会话管理</div>
                  <div class="form-row">
                    <div class="form-group"><label>会话超时时间</label><select v-model="userForm.session_timeout"><option :value="30">30 分钟</option><option :value="60">1 小时</option><option :value="180">3 小时</option><option :value="1440">24 小时</option></select><div class="form-hint">无操作自动退出登录的时间</div></div>
                  </div>
                </div>
                <!-- 权限管理 -->
                <div v-show="userEditTab === 'permissions'" class="tab-content">
                  <div class="section-divider">角色权限</div>
                  <div class="permission-info-card">
                    <div class="permission-icon"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" width="24" height="24"><path d="M12 22s8-4 8-10V5l-8-3-8 3v7c0 6 8 10 8 10z"/></svg></div>
                    <div class="permission-content"><div class="permission-title">当前角色: {{ getRoleName(userForm.role) }}</div><div class="permission-desc">用户权限由所属角色决定，如需修改权限请在基本信息中调整角色</div></div>
                  </div>
                  <div class="section-divider">功能权限</div>
                  <div class="permission-list">
                    <div class="permission-item"><span class="permission-name">用户管理</span><span class="permission-badge" :class="userForm.role === 'admin' || userForm.role === 'super_admin' ? 'granted' : 'denied'">{{ userForm.role === 'admin' || userForm.role === 'super_admin' ? '已授权' : '未授权' }}</span></div>
                    <div class="permission-item"><span class="permission-name">角色管理</span><span class="permission-badge" :class="userForm.role === 'admin' || userForm.role === 'super_admin' ? 'granted' : 'denied'">{{ userForm.role === 'admin' || userForm.role === 'super_admin' ? '已授权' : '未授权' }}</span></div>
                    <div class="permission-item"><span class="permission-name">系统设置</span><span class="permission-badge" :class="userForm.role === 'admin' || userForm.role === 'super_admin' ? 'granted' : 'denied'">{{ userForm.role === 'admin' || userForm.role === 'super_admin' ? '已授权' : '未授权' }}</span></div>
                    <div class="permission-item"><span class="permission-name">数据查看</span><span class="permission-badge granted">已授权</span></div>
                  </div>
                </div>
                <!-- 登录历史 -->
                <div v-show="userEditTab === 'history'" class="tab-content">
                  <div class="section-divider">最近登录</div>
                  <div class="login-history-list">
                    <div class="history-item"><div class="history-icon success"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" width="16" height="16"><polyline points="20 6 9 17 4 12"/></svg></div><div class="history-content"><div class="history-title">登录成功</div><div class="history-meta"><span>IP: 192.168.1.100</span><span>Chrome / Windows</span></div></div><div class="history-time">2 小时前</div></div>
                    <div class="history-item"><div class="history-icon success"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" width="16" height="16"><polyline points="20 6 9 17 4 12"/></svg></div><div class="history-content"><div class="history-title">登录成功</div><div class="history-meta"><span>IP: 192.168.1.100</span><span>Chrome / Windows</span></div></div><div class="history-time">昨天 14:30</div></div>
                  </div>
                </div>
              </div>
              <div class="content-footer">
                <span class="footer-hint" v-if="userModalMode === 'edit'">ID: {{ userForm.id }}</span>
                <span class="footer-hint" v-else></span>
                <div class="btn-group"><button type="button" class="btn-cancel" @click="showUserModal = false">取消</button><button type="submit" class="btn-submit">{{ userModalMode === 'add' ? '创建用户' : '保存更改' }}</button></div>
              </div>
            </div>
          </form>
        </div>
      </div>
    </Teleport>

    <!-- 修改密码弹窗 -->
    <Teleport to="body">
      <div class="user-modal-overlay" :class="{ show: showPasswordModal }">
        <div class="user-password-modal">
          <div class="modal-header"><h2>修改密码</h2><button class="modal-close" @click="showPasswordModal = false">&times;</button></div>
          <div class="modal-body">
            <div class="form-group"><label>用户名</label><input type="text" :value="passwordForm.username" disabled class="disabled-input"></div>
            <div class="form-group">
              <label><span class="required">*</span> 新密码</label>
              <div class="password-field">
                <input :type="showNewPassword ? 'text' : 'password'" v-model="passwordForm.newPassword" placeholder="请输入新密码">
                <button type="button" class="password-eye" @click="showNewPassword = !showNewPassword">
                  <svg v-if="showNewPassword" width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M1 12s4-8 11-8 11 8 11 8-4 8-11 8-11-8-11-8z"/><circle cx="12" cy="12" r="3"/></svg>
                  <svg v-else width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M17.94 17.94A10.07 10.07 0 0 1 12 20c-7 0-11-8-11-8a18.45 18.45 0 0 1 5.06-5.94"/><line x1="1" y1="1" x2="23" y2="23"/></svg>
                </button>
              </div>
            </div>
            <div class="password-actions">
              <button type="button" class="btn-ghost" @click="generatePassword"><svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><rect x="3" y="11" width="18" height="11" rx="2"/><path d="M7 11V7a5 5 0 0 1 10 0v4"/></svg> 自动生成密码</button>
              <button type="button" class="btn-ghost" @click="copyPassword" :disabled="!passwordForm.newPassword"><svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><rect x="9" y="9" width="13" height="13" rx="2"/><path d="M5 15H4a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2h9a2 2 0 0 1 2 2v1"/></svg> 一键复制</button>
            </div>
          </div>
          <div class="modal-footer">
            <button class="btn-cancel" @click="showPasswordModal = false">取 消</button>
            <button class="btn-submit" @click="submitPasswordChange">确 定</button>
          </div>
        </div>
      </div>
    </Teleport>
  </div>
</template>

<style scoped>
.users-page { padding: 0; }
.page-header { display: flex; justify-content: space-between; align-items: center; margin-bottom: 20px; }
.header-left { display: flex; align-items: center; gap: 12px; }
.page-title { font-size: 1.5rem; font-weight: 600; margin: 0; }
.count-badge { background: #3b82f6; color: #fff; padding: 4px 12px; border-radius: 20px; font-size: 0.75rem; }
.header-right { display: flex; gap: 8px; }
.btn-export, .btn-primary { display: flex; align-items: center; gap: 6px; padding: 8px 16px; border-radius: 8px; font-size: 0.875rem; cursor: pointer; border: none; }
.btn-export { background: var(--bg-secondary); color: var(--text-primary); border: 1px solid var(--border-color); }
.btn-primary { background: linear-gradient(135deg, #3b82f6, #2563eb); color: #fff; }

.stats-inline { display: flex; align-items: baseline; gap: 20px; margin-bottom: 20px; padding: 12px 0; border-bottom: 1px solid var(--border-color); }
.stat-item { display: flex; align-items: baseline; gap: 5px; }
.stat-num { font-size: 1.25rem; font-weight: 600; color: var(--text-primary); }
.stat-text { font-size: 0.8125rem; color: var(--text-secondary); }
.stat-sep { width: 1px; height: 16px; background: var(--border-color); align-self: center; }

.list-panel { background: var(--bg-primary); border-radius: 12px; border: 1px solid var(--border-color); padding: 20px; }
.panel-header { display: flex; justify-content: space-between; align-items: center; margin-bottom: 16px; }
.panel-title { display: flex; align-items: center; gap: 8px; font-weight: 600; }
.refresh-btn { display: flex; align-items: center; gap: 4px; background: none; border: 1px solid var(--border-color); padding: 6px 12px; border-radius: 6px; cursor: pointer; font-size: 0.8125rem; }
.filter-row { display: flex; gap: 12px; margin-bottom: 16px; }
.search-box { flex: 1; display: flex; align-items: center; gap: 8px; background: var(--bg-secondary); padding: 0 12px; border-radius: 8px; border: 1px solid var(--border-color); }
.search-box input { flex: 1; border: none; background: transparent; padding: 10px 0; outline: none; font-size: 0.875rem; }
.filter-select { padding: 10px 12px; border-radius: 8px; border: 1px solid var(--border-color); background: var(--bg-secondary); font-size: 0.875rem; min-width: 100px; }
.btn-search { padding: 10px 20px; border-radius: 8px; border: none; background: #3a84ff; color: #fff; font-size: 14px; font-weight: 500; cursor: pointer; transition: all 0.2s; }
.btn-search:hover { background: #2970e6; }
.btn-reset { padding: 10px 20px; border-radius: 8px; border: 1px solid var(--border-color); background: var(--bg-secondary); color: var(--text-primary); font-size: 14px; cursor: pointer; transition: all 0.2s; }
.btn-reset:hover { background: var(--bg-hover); }

.user-table { width: 100%; border-collapse: collapse; }
.user-table th, .user-table td { padding: 12px; text-align: left; border-bottom: 1px solid var(--border-color); }
.user-table th { font-size: 0.75rem; color: var(--text-secondary); text-transform: uppercase; }
.user-info { display: flex; flex-direction: column; }
.user-info .name { font-weight: 500; display: flex; align-items: center; gap: 6px; }
.user-info .email { font-size: 0.75rem; color: var(--text-secondary); }
.auth-badge { font-size: 0.6rem; padding: 1px 5px; border-radius: 3px; font-weight: 500; text-transform: uppercase; }
.auth-badge.local { background: rgba(34, 197, 94, 0.12); color: #16a34a; }
.auth-badge.sso { background: rgba(59, 130, 246, 0.12); color: #2563eb; }
.role-badge { display: inline-block; padding: 4px 10px; border-radius: 20px; font-size: 0.75rem; background: rgba(59, 130, 246, 0.1); color: #3b82f6; }
.role-badge.admin { background: rgba(139, 92, 246, 0.1); color: #8b5cf6; }
.status-cell { display: flex; align-items: center; gap: 6px; }
.status-dot { width: 8px; height: 8px; border-radius: 50%; }
.status-dot.active { background: #22c55e; }
.status-dot.inactive { background: #ef4444; }
.mfa-badge { display: inline-flex; align-items: center; gap: 4px; padding: 4px 10px; border-radius: 20px; font-size: 0.75rem; }
.mfa-badge.bound { background: rgba(34, 197, 94, 0.1); color: #22c55e; }
.mfa-badge.unbound { background: rgba(239, 68, 68, 0.1); color: #ef4444; }
.date { font-size: 0.8125rem; color: var(--text-secondary); }
.actions { display: flex; align-items: center; gap: 6px; }
.act-icon { background: none; border: none; padding: 4px; cursor: pointer; color: #94a3b8; display: flex; align-items: center; justify-content: center; border-radius: 4px; }
.act-icon:hover { color: #475569; background: var(--bg-hover, #f1f5f9); }
.pagination { display: flex; align-items: center; gap: 8px; padding: 16px 0 0; font-size: 0.8125rem; color: var(--text-secondary); }
.pagination select { padding: 4px 8px; border-radius: 4px; border: 1px solid var(--border-color); }
.page-btns { margin-left: auto; display: flex; gap: 4px; }
.page-btns button { padding: 6px 12px; border: 1px solid var(--border-color); background: var(--bg-primary); border-radius: 4px; cursor: pointer; font-size: 0.75rem; }
.page-num { padding: 6px 12px; background: #3b82f6; color: #fff; border-radius: 4px; }


</style>

<!-- 弹窗样式 - 全局样式用于 Teleport -->
<style>
.user-modal-overlay { position: fixed; inset: 0; background: rgba(0,0,0,0.5); display: flex; align-items: center; justify-content: center; z-index: 9999; opacity: 0; visibility: hidden; transition: all 0.2s; }
.user-modal-overlay.show { opacity: 1; visibility: visible; }
.user-edit-modal { background: #fff; border-radius: 16px; width: 90%; max-width: 900px; max-height: 90vh; overflow: hidden; box-shadow: 0 25px 50px -12px rgba(0,0,0,0.25); }
.user-edit-form { display: flex; height: 580px; }
.user-edit-sidebar { width: 220px; background: #f8fafc; border-right: 1px solid #e2e8f0; display: flex; flex-direction: column; flex-shrink: 0; }
.user-edit-sidebar .sidebar-header { padding: 20px; border-bottom: 1px solid #e2e8f0; }
.user-edit-sidebar .sidebar-title { font-size: 0.875rem; color: #64748b; margin-bottom: 16px; }
.user-edit-sidebar .user-card { display: flex; align-items: center; gap: 12px; }
.user-edit-sidebar .user-card .user-avatar { width: 48px; height: 48px; border-radius: 50%; display: flex; align-items: center; justify-content: center; color: #fff; font-weight: 600; font-size: 1.25rem; flex-shrink: 0; }
.user-edit-sidebar .user-card-info { display: flex; flex-direction: column; min-width: 0; }
.user-edit-sidebar .user-card-name { font-weight: 600; color: #1e293b; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
.user-edit-sidebar .user-card-role { font-size: 0.75rem; color: #64748b; }
.user-edit-sidebar .nav-list { list-style: none; padding: 12px; margin: 0; flex: 1; }
.user-edit-sidebar .nav-item { display: flex; align-items: center; gap: 10px; padding: 12px 16px; border-radius: 8px; cursor: pointer; font-size: 0.875rem; color: #64748b; transition: all 0.2s; }
.user-edit-sidebar .nav-item:hover { background: rgba(59, 130, 246, 0.05); }
.user-edit-sidebar .nav-item.active { background: rgba(59, 130, 246, 0.1); color: #3b82f6; }
.user-edit-content { flex: 1; display: flex; flex-direction: column; overflow: hidden; min-width: 0; background: #fff; }
.user-edit-content .content-header { display: flex; justify-content: space-between; align-items: flex-start; padding: 24px 24px 0; flex-shrink: 0; }
.user-edit-content .content-title { font-size: 1.25rem; font-weight: 600; color: #1e293b; }
.user-edit-content .content-subtitle { font-size: 0.875rem; color: #64748b; margin-top: 4px; }
.user-edit-content .close-btn { background: none; border: none; cursor: pointer; padding: 8px; border-radius: 6px; color: #94a3b8; }
.user-edit-content .close-btn:hover { background: #f1f5f9; }
.user-edit-content .content-body { flex: 1; padding: 24px; overflow-y: auto; }
.user-edit-content .tab-content { display: flex; flex-direction: column; }
.user-edit-content .form-row { display: grid; grid-template-columns: 1fr 1fr; gap: 16px; margin-bottom: 16px; }
.user-edit-content .form-group { display: flex; flex-direction: column; gap: 8px; }
.user-edit-content .form-group.full-width { grid-column: span 2; }
.user-edit-content .form-group label { font-size: 0.875rem; color: #64748b; }
.user-edit-content .form-group label .required { color: #ef4444; }
.user-edit-content .form-group label .hint { font-size: 0.75rem; color: #94a3b8; }
.user-edit-content .form-group input, .user-edit-content .form-group select { padding: 10px 14px; border: 1px solid #e2e8f0; border-radius: 8px; font-size: 0.875rem; background: #fff; color: #1e293b; }
.user-edit-content .form-group input:disabled { background: #f1f5f9; opacity: 0.7; }
.user-edit-content .form-group input:focus, .user-edit-content .form-group select:focus { outline: none; border-color: #3b82f6; }
.user-edit-content .form-hint { font-size: 0.75rem; color: #94a3b8; margin-top: 4px; }
.user-edit-content .section-divider { font-size: 0.8125rem; font-weight: 600; color: #64748b; margin: 20px 0 12px; padding-bottom: 8px; border-bottom: 1px solid #e2e8f0; }
.user-edit-content .status-cards { display: grid; grid-template-columns: 1fr 1fr; gap: 12px; margin-bottom: 8px; }
.user-edit-content .status-card { padding: 16px; border: 2px solid #e2e8f0; border-radius: 12px; cursor: pointer; transition: all 0.2s; background: #fff; }
.user-edit-content .status-card:hover { border-color: #3b82f6; }
.user-edit-content .status-card.selected { border-color: #3b82f6; background: rgba(59, 130, 246, 0.05); }
.user-edit-content .status-card-header { display: flex; justify-content: space-between; align-items: center; margin-bottom: 8px; }
.user-edit-content .status-card-title { font-weight: 500; color: #1e293b; }
.user-edit-content .status-indicator { width: 18px; height: 18px; border: 2px solid #e2e8f0; border-radius: 50%; }
.user-edit-content .status-card.selected .status-indicator { border-color: #3b82f6; background: #3b82f6; }
.user-edit-content .status-card-desc { font-size: 0.75rem; color: #64748b; }
.user-edit-content .password-field { display: flex; align-items: center; border: 1px solid #e2e8f0; border-radius: 8px; overflow: hidden; background: #fff; }
.user-edit-content .password-field input { flex: 1; border: none; padding: 10px 14px; font-size: 0.875rem; outline: none; background: transparent; color: #1e293b; }
.user-edit-content .password-eye { padding: 10px; background: none; border: none; cursor: pointer; color: #94a3b8; }
.user-edit-content .password-actions { display: flex; gap: 8px; margin-top: 12px; margin-bottom: 8px; }
.user-edit-content .btn-ghost { display: flex; align-items: center; gap: 6px; padding: 8px 12px; border: 1px solid #e2e8f0; border-radius: 6px; background: transparent; cursor: pointer; font-size: 0.8125rem; color: #1e293b; }
.user-edit-content .btn-ghost:hover { background: #f1f5f9; }
.user-edit-content .btn-ghost:disabled { opacity: 0.5; cursor: not-allowed; }
.user-edit-content .switch-group { margin-bottom: 8px; }
.user-edit-content .switch-item { display: flex; justify-content: space-between; align-items: center; padding: 16px; border: 1px solid #e2e8f0; border-radius: 12px; background: #fff; }
.user-edit-content .switch-info { display: flex; align-items: center; gap: 12px; }
.user-edit-content .switch-icon { width: 40px; height: 40px; background: rgba(59, 130, 246, 0.1); border-radius: 8px; display: flex; align-items: center; justify-content: center; color: #3b82f6; flex-shrink: 0; }
.user-edit-content .switch-label { font-weight: 500; color: #1e293b; }
.user-edit-content .switch-desc { font-size: 0.75rem; color: #64748b; }
.user-edit-content .switch { position: relative; width: 36px; height: 20px; }
.user-edit-content .switch input { opacity: 0; width: 0; height: 0; }
.user-edit-content .switch .slider { position: absolute; cursor: pointer; inset: 0; background: #cbd5e1; border-radius: 20px; transition: 0.3s; }
.user-edit-content .switch .slider:before { content: ''; position: absolute; width: 16px; height: 16px; left: 2px; bottom: 2px; background: #fff; border-radius: 50%; transition: 0.3s; }
.user-edit-content .switch input:checked + .slider { background: #3b82f6; }
.user-edit-content .switch input:checked + .slider:before { transform: translateX(16px); }
.user-edit-content .mfa-reset-action { margin-top: 12px; }
.user-edit-content .btn-danger-outline { display: flex; align-items: center; gap: 6px; padding: 8px 12px; border: 1px solid #ef4444; border-radius: 6px; background: transparent; cursor: pointer; font-size: 0.8125rem; color: #ef4444; }
.user-edit-content .btn-danger-outline:hover { background: rgba(239, 68, 68, 0.1); }
.user-edit-content .permission-info-card { display: flex; align-items: center; gap: 16px; padding: 16px; background: rgba(59, 130, 246, 0.05); border-radius: 12px; margin-bottom: 8px; }
.user-edit-content .permission-icon { width: 48px; height: 48px; background: rgba(59, 130, 246, 0.1); border-radius: 12px; display: flex; align-items: center; justify-content: center; color: #3b82f6; flex-shrink: 0; }
.user-edit-content .permission-content { }
.user-edit-content .permission-title { font-weight: 600; margin-bottom: 4px; color: #1e293b; }
.user-edit-content .permission-desc { font-size: 0.8125rem; color: #64748b; }
.user-edit-content .permission-list { display: flex; flex-direction: column; gap: 8px; }
.user-edit-content .permission-item { display: flex; justify-content: space-between; align-items: center; padding: 12px 16px; border: 1px solid #e2e8f0; border-radius: 8px; background: #fff; }
.user-edit-content .permission-name { font-size: 0.875rem; color: #1e293b; }
.user-edit-content .permission-badge { padding: 4px 10px; border-radius: 20px; font-size: 0.75rem; }
.user-edit-content .permission-badge.granted { background: rgba(34, 197, 94, 0.1); color: #22c55e; }
.user-edit-content .permission-badge.denied { background: rgba(239, 68, 68, 0.1); color: #ef4444; }
.user-edit-content .login-history-list { display: flex; flex-direction: column; gap: 12px; }
.user-edit-content .history-item { display: flex; align-items: center; gap: 12px; padding: 12px; border: 1px solid #e2e8f0; border-radius: 8px; background: #fff; }
.user-edit-content .history-icon { width: 32px; height: 32px; border-radius: 50%; display: flex; align-items: center; justify-content: center; flex-shrink: 0; }
.user-edit-content .history-icon.success { background: rgba(34, 197, 94, 0.1); color: #22c55e; }
.user-edit-content .history-content { flex: 1; min-width: 0; }
.user-edit-content .history-title { font-weight: 500; font-size: 0.875rem; color: #1e293b; }
.user-edit-content .history-meta { display: flex; gap: 16px; font-size: 0.75rem; color: #64748b; }
.user-edit-content .history-time { font-size: 0.75rem; color: #94a3b8; flex-shrink: 0; }
.user-edit-content .content-footer { display: flex; justify-content: space-between; align-items: center; padding: 16px 24px; border-top: 1px solid #e2e8f0; flex-shrink: 0; background: #fff; }
.user-edit-content .footer-hint { font-size: 0.75rem; color: #94a3b8; }
.user-edit-content .btn-group { display: flex; gap: 8px; }
.user-edit-content .btn-cancel { padding: 10px 20px; border: 1px solid #e2e8f0; border-radius: 8px; background: #fff; cursor: pointer; font-size: 0.875rem; color: #1e293b; }
.user-edit-content .btn-submit { padding: 10px 20px; border: none; border-radius: 8px; background: linear-gradient(135deg, #3b82f6, #2563eb); color: #fff; cursor: pointer; font-size: 0.875rem; }

/* 修改密码弹窗 */
.user-password-modal { background: #fff; border-radius: 12px; width: 90%; max-width: 480px; box-shadow: 0 25px 50px -12px rgba(0,0,0,0.25); }
.user-password-modal .modal-header { display: flex; justify-content: space-between; align-items: center; padding: 16px 20px; border-bottom: 1px solid #e2e8f0; }
.user-password-modal .modal-header h2 { margin: 0; font-size: 1.125rem; color: #1e293b; }
.user-password-modal .modal-close { background: none; border: none; font-size: 1.5rem; cursor: pointer; color: #94a3b8; line-height: 1; }
.user-password-modal .modal-body { padding: 20px; }
.user-password-modal .form-group { display: flex; flex-direction: column; gap: 8px; margin-bottom: 16px; }
.user-password-modal .form-group label { font-size: 0.875rem; color: #64748b; }
.user-password-modal .form-group label .required { color: #ef4444; }
.user-password-modal .form-group input { padding: 10px 14px; border: 1px solid #e2e8f0; border-radius: 8px; font-size: 0.875rem; background: #fff; color: #1e293b; }
.user-password-modal .disabled-input { background: #f1f5f9 !important; opacity: 0.7; }
.user-password-modal .password-field { display: flex; align-items: center; border: 1px solid #e2e8f0; border-radius: 8px; overflow: hidden; background: #fff; }
.user-password-modal .password-field input { flex: 1; border: none; padding: 10px 14px; font-size: 0.875rem; outline: none; background: transparent; color: #1e293b; }
.user-password-modal .password-eye { padding: 10px; background: none; border: none; cursor: pointer; color: #94a3b8; }
.user-password-modal .password-actions { display: flex; gap: 8px; margin-top: 12px; }
.user-password-modal .btn-ghost { display: flex; align-items: center; gap: 6px; padding: 8px 12px; border: 1px solid #e2e8f0; border-radius: 6px; background: transparent; cursor: pointer; font-size: 0.8125rem; color: #1e293b; }
.user-password-modal .btn-ghost:hover { background: #f1f5f9; }
.user-password-modal .btn-ghost:disabled { opacity: 0.5; cursor: not-allowed; }
.user-password-modal .modal-footer { display: flex; justify-content: flex-end; gap: 8px; padding: 16px 20px; border-top: 1px solid #e2e8f0; }
.user-password-modal .btn-cancel { padding: 10px 20px; border: 1px solid #e2e8f0; border-radius: 8px; background: #fff; cursor: pointer; font-size: 0.875rem; color: #1e293b; }
.user-password-modal .btn-submit { padding: 10px 20px; border: none; border-radius: 8px; background: linear-gradient(135deg, #3b82f6, #2563eb); color: #fff; cursor: pointer; font-size: 0.875rem; }
</style>
