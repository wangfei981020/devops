<template>
  <div>
    <div class="tabs">
      <button v-if="authStore.hasPermission('jira:config_connection')" :class="['tab', { active: activeTab === 'jira' }]" @click="activeTab = 'jira'">Jira 连接</button>
      <button v-if="authStore.hasPermission('jira:config_sso')" :class="['tab', { active: activeTab === 'sso' }]" @click="activeTab = 'sso'">SSO 配置</button>
      <button v-if="authStore.hasPermission('jira:manage_users')" :class="['tab', { active: activeTab === 'users' }]" @click="activeTab = 'users'">用户管理</button>
      <button v-if="authStore.hasPermission('jira:view_audit')" :class="['tab', { active: activeTab === 'audit' }]" @click="activeTab = 'audit'">审计日志</button>
    </div>

    <!-- Jira 连接配置 -->
    <div v-if="activeTab === 'jira'" class="card">
      <h3>Jira 服务器连接</h3>
      <div class="form-grid">
        <div class="field">
          <label>Jira URL</label>
          <input class="input" v-model="jiraConfig.jira_url" placeholder="http://localhost:8090" />
        </div>
        <div class="field">
          <label>用户名 <span class="hint">（使用个人令牌时留空）</span></label>
          <input class="input" v-model="jiraConfig.jira_username" placeholder="admin" />
        </div>
        <div class="field">
          <label>密码 / 个人访问令牌 (PAT)</label>
          <input class="input" type="password" v-model="jiraConfig.jira_password" placeholder="密码或个人访问令牌" />
        </div>
      </div>
      <div class="auth-hint">认证方式：填写用户名+密码使用 Basic Auth；只填令牌（用户名留空）使用 Bearer Token</div>
      <div class="form-actions">
        <button class="btn" @click="testJira" :disabled="testing">{{ testing ? '测试中...' : '测试连接' }}</button>
        <button class="btn btn-primary" @click="saveJira" :disabled="saving">保存</button>
      </div>
      <div v-if="testResult" :class="['test-result', testResult.ok ? 'success' : 'error']">
        {{ testResult.message }}
      </div>
    </div>

    <!-- SSO 配置 -->
    <div v-if="activeTab === 'sso'" class="card">
      <h3>OIDC / SSO 配置</h3>
      <div class="form-grid">
        <div class="field checkbox-field">
          <label><input type="checkbox" v-model="oidcConfig.enabled" /> 启用 SSO 登录</label>
        </div>
        <div class="field">
          <label>显示名称</label>
          <input class="input" v-model="oidcConfig.provider_name" placeholder="统一门户" />
        </div>
        <div class="field">
          <label>Client ID</label>
          <input class="input" v-model="oidcConfig.client_id" placeholder="opsplatform-jira" />
        </div>
        <div class="field">
          <label>Client Secret</label>
          <input class="input" type="password" v-model="oidcConfig.client_secret" />
        </div>
        <div class="field">
          <label>Issuer URL</label>
          <input class="input" v-model="oidcConfig.issuer_url" placeholder="http://localhost:30900" />
        </div>
        <div class="field">
          <label>Redirect URI</label>
          <input class="input" v-model="oidcConfig.redirect_uri" placeholder="http://localhost:3001/api/oidc/callback" />
        </div>
        <div class="field">
          <label>Scopes</label>
          <input class="input" v-model="oidcConfig.scopes" placeholder="openid profile email" />
        </div>
        <div class="field checkbox-field">
          <label><input type="checkbox" v-model="oidcConfig.auto_register" /> 自动注册新用户</label>
        </div>
        <div class="field">
          <label>默认角色</label>
          <select class="input" v-model="oidcConfig.default_role">
            <option value="user">user</option>
            <option value="admin">admin</option>
          </select>
        </div>
      </div>
      <div class="form-actions">
        <button class="btn btn-primary" @click="saveOIDC" :disabled="saving">保存</button>
      </div>
    </div>

    <!-- 用户管理 -->
    <div v-if="activeTab === 'users'" class="card">
      <div class="section-header">
        <h3>用户列表</h3>
        <button class="btn btn-primary btn-sm" @click="showAddUser = true">添加用户</button>
      </div>
      <div class="table-wrapper">
        <table>
          <thead>
            <tr>
              <th>用户名</th>
              <th>显示名</th>
              <th>角色</th>
              <th>来源</th>
              <th>状态</th>
              <th>创建时间</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="u in users" :key="u.id" style="cursor:default">
              <td>{{ u.username }}</td>
              <td>{{ u.display_name }}</td>
              <td><span class="badge" :class="u.role === 'admin' ? 'admin' : ''">{{ u.role }}</span></td>
              <td>{{ u.auth_source }}</td>
              <td><span :class="['status-dot', u.status]"></span>{{ u.status }}</td>
              <td class="date-cell">{{ u.created_at }}</td>
            </tr>
          </tbody>
        </table>
      </div>

      <!-- 添加用户弹窗 -->
      <div v-if="showAddUser" class="modal-overlay" @click.self="showAddUser = false">
        <div class="modal card">
          <h3>添加用户</h3>
          <div class="field"><label>用户名</label><input class="input" v-model="newUser.username" /></div>
          <div class="field"><label>密码</label><input class="input" type="password" v-model="newUser.password" /></div>
          <div class="field"><label>显示名</label><input class="input" v-model="newUser.display_name" /></div>
          <div class="field"><label>角色</label>
            <select class="input" v-model="newUser.role"><option value="user">user</option><option value="admin">admin</option></select>
          </div>
          <div class="form-actions">
            <button class="btn" @click="showAddUser = false">取消</button>
            <button class="btn btn-primary" @click="addUser">创建</button>
          </div>
        </div>
      </div>
    </div>

    <!-- 审计日志 -->
    <div v-if="activeTab === 'audit'" class="card">
      <h3>审计日志</h3>
      <div class="table-wrapper">
        <table>
          <thead><tr><th>时间</th><th>用户</th><th>操作</th><th>详情</th><th>IP</th></tr></thead>
          <tbody>
            <tr v-for="l in auditLogs" :key="l.id" style="cursor:default">
              <td class="date-cell">{{ l.created_at }}</td>
              <td>{{ l.username }}</td>
              <td>{{ l.action }}</td>
              <td>{{ l.detail }}</td>
              <td>{{ l.ip }}</td>
            </tr>
          </tbody>
        </table>
      </div>
    </div>
  </div>
</template>

<script setup>
import { ref, onMounted, watch, inject } from 'vue'
import api from '@/api'
import { useAuthStore } from '@/stores/auth'

const authStore = useAuthStore()

const toast = inject('toast')
const activeTab = ref('jira')
const saving = ref(false)
const testing = ref(false)
const testResult = ref(null)

const jiraConfig = ref({ jira_url: '', jira_username: '', jira_password: '' })
const oidcConfig = ref({ enabled: false, provider_name: '', client_id: '', client_secret: '', issuer_url: '', redirect_uri: '', scopes: 'openid profile email', auto_register: true, default_role: 'user' })
const users = ref([])
const auditLogs = ref([])
const showAddUser = ref(false)
const newUser = ref({ username: '', password: '', display_name: '', role: 'user' })

onMounted(() => loadTabData())
watch(activeTab, () => loadTabData())

async function loadTabData() {
  if (activeTab.value === 'jira') {
    try {
      const res = await api.get('/api/settings')
      jiraConfig.value = res.data.data || {}
    } catch (e) { /* ignore */ }
  } else if (activeTab.value === 'sso') {
    try {
      const res = await api.get('/api/oidc/admin-config')
      oidcConfig.value = res.data
    } catch (e) { /* ignore */ }
  } else if (activeTab.value === 'users') {
    try {
      const res = await api.get('/api/users')
      users.value = res.data.data || []
    } catch (e) { /* ignore */ }
  } else if (activeTab.value === 'audit') {
    try {
      const res = await api.get('/api/audit-logs')
      auditLogs.value = res.data.data || []
    } catch (e) { /* ignore */ }
  }
}

async function saveJira() {
  saving.value = true
  try {
    await api.post('/api/settings', jiraConfig.value)
    toast('Jira 配置已保存', 'success')
  } catch (e) {
    toast('保存失败', 'error')
  } finally { saving.value = false }
}

async function testJira() {
  testing.value = true
  testResult.value = null
  try {
    const res = await api.post('/api/settings/test-jira', jiraConfig.value)
    testResult.value = { ok: true, message: `连接成功！用户: ${res.data.data?.user || ''}` }
  } catch (e) {
    testResult.value = { ok: false, message: e.response?.data?.error || '连接失败' }
  } finally { testing.value = false }
}

async function saveOIDC() {
  saving.value = true
  try {
    await api.post('/api/oidc/admin-config', oidcConfig.value)
    toast('SSO 配置已保存', 'success')
  } catch (e) {
    toast('保存失败', 'error')
  } finally { saving.value = false }
}

async function addUser() {
  try {
    await api.post('/api/users', newUser.value)
    toast('用户已创建', 'success')
    showAddUser.value = false
    newUser.value = { username: '', password: '', display_name: '', role: 'user' }
    loadTabData()
  } catch (e) {
    toast(e.response?.data?.error || '创建失败', 'error')
  }
}
</script>

<style scoped>
.tabs { display: flex; gap: 0; margin-bottom: 16px; border-bottom: 1px solid var(--border); }
.tab {
  padding: 10px 20px;
  background: none;
  border: none;
  border-bottom: 2px solid transparent;
  color: var(--text-secondary);
  font-size: 14px;
  cursor: pointer;
  transition: all var(--transition);
  font-family: var(--font-sans);
}
.tab:hover { color: var(--text-primary); }
.tab.active { color: var(--primary-light); border-bottom-color: var(--primary-light); }

h3 { font-size: 16px; margin-bottom: 16px; }

.form-grid { display: grid; grid-template-columns: 1fr 1fr; gap: 16px; }
.field { display: flex; flex-direction: column; gap: 4px; }
.field label { font-size: 13px; color: var(--text-secondary); font-weight: 500; }
.checkbox-field { flex-direction: row; align-items: center; }
.checkbox-field label { display: flex; align-items: center; gap: 8px; cursor: pointer; }

.hint { font-weight: 400; color: var(--text-muted, #888); font-size: 12px; }
.auth-hint { font-size: 12px; color: var(--text-secondary); margin-top: 8px; padding: 8px 12px; background: rgba(59,130,246,0.06); border-radius: var(--radius); }
.form-actions { display: flex; gap: 8px; margin-top: 20px; }

.test-result { margin-top: 12px; padding: 10px 14px; border-radius: var(--radius); font-size: 14px; }
.test-result.success { background: rgba(16,185,129,0.1); color: var(--success); }
.test-result.error { background: rgba(239,68,68,0.1); color: var(--danger); }

.section-header { display: flex; justify-content: space-between; align-items: center; margin-bottom: 12px; }

.badge.admin { background: rgba(59,130,246,0.15); color: var(--primary-light); }
.status-dot { display: inline-block; width: 6px; height: 6px; border-radius: 50%; margin-right: 6px; }
.status-dot.active { background: var(--success); }
.status-dot.disabled { background: var(--danger); }
.date-cell { font-size: 13px; color: var(--text-secondary); white-space: nowrap; }

.modal-overlay {
  position: fixed;
  inset: 0;
  background: rgba(0,0,0,0.5);
  display: flex;
  align-items: center;
  justify-content: center;
  z-index: 1000;
}
.modal { width: 400px; display: flex; flex-direction: column; gap: 12px; }
</style>
