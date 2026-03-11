<template>
  <div>
    <div class="tabs">
      <button :class="['tab', { active: activeTab === 'connections' }]" @click="activeTab = 'connections'">服务连接</button>
      <button :class="['tab', { active: activeTab === 'general' }]" @click="activeTab = 'general'">通用配置</button>
      <button :class="['tab', { active: activeTab === 'users' }]" @click="activeTab = 'users'">用户管理</button>
      <button :class="['tab', { active: activeTab === 'audit' }]" @click="activeTab = 'audit'">审计日志</button>
    </div>

    <!-- 服务连接管理 -->
    <div v-if="activeTab === 'connections'">
      <!-- Confluence 连接 -->
      <div class="conn-section">
        <div class="section-header">
          <div class="section-label">
            <span class="type-icon confluence">C</span>
            <h3>Confluence 连接</h3>
          </div>
          <button class="btn btn-sm btn-primary" @click="openAddConn('confluence')">+ 添加</button>
        </div>
        <div class="conn-grid">
          <div class="conn-card" v-for="c in confluenceConns" :key="c.id" :class="{ default: c.is_default }">
            <div class="conn-card-header">
              <div class="conn-name">{{ c.name }}</div>
              <span v-if="c.is_default" class="default-badge">默认</span>
            </div>
            <div class="conn-url">{{ c.url }}</div>
            <div class="conn-meta">
              <span v-if="c.username">{{ c.username }}</span>
              <span v-if="c.config?.space_key" class="meta-tag">空间: {{ c.config.space_key }}</span>
              <span v-if="c.config?.root_page" class="meta-tag">根页面: {{ c.config.root_page }}</span>
            </div>
            <div class="conn-actions">
              <button class="btn btn-xs" @click="testConn(c)">{{ c._testing ? '测试中...' : '测试' }}</button>
              <button class="btn btn-xs" @click="openEditConn(c)">编辑</button>
              <button class="btn btn-xs danger" @click="deleteConn(c)">删除</button>
            </div>
            <div v-if="c._testResult" :class="['test-msg', c._testResult.ok ? 'ok' : 'fail']">{{ c._testResult.message }}</div>
          </div>
          <div class="conn-card empty" v-if="!confluenceConns.length" @click="openAddConn('confluence')">
            <span class="empty-icon">+</span>
            <span>添加 Confluence 连接</span>
          </div>
        </div>
      </div>

      <!-- Jira 连接 -->
      <div class="conn-section">
        <div class="section-header">
          <div class="section-label">
            <span class="type-icon jira">J</span>
            <h3>Jira 连接</h3>
          </div>
          <button class="btn btn-sm btn-primary" @click="openAddConn('jira')">+ 添加</button>
        </div>
        <div class="conn-grid">
          <div class="conn-card" v-for="c in jiraConns" :key="c.id" :class="{ default: c.is_default }">
            <div class="conn-card-header">
              <div class="conn-name">{{ c.name }}</div>
              <span v-if="c.is_default" class="default-badge">默认</span>
            </div>
            <div class="conn-url">{{ c.url }}</div>
            <div class="conn-meta">
              <span v-if="c.username">{{ c.username }}</span>
              <span v-if="c.config?.fault_issuetype" class="meta-tag">故障类型: {{ c.config.fault_issuetype }}</span>
            </div>
            <div class="conn-actions">
              <button class="btn btn-xs" @click="testConn(c)">{{ c._testing ? '测试中...' : '测试' }}</button>
              <button class="btn btn-xs" @click="openEditConn(c)">编辑</button>
              <button class="btn btn-xs danger" @click="deleteConn(c)">删除</button>
            </div>
            <div v-if="c._testResult" :class="['test-msg', c._testResult.ok ? 'ok' : 'fail']">{{ c._testResult.message }}</div>
          </div>
          <div class="conn-card empty" v-if="!jiraConns.length" @click="openAddConn('jira')">
            <span class="empty-icon">+</span>
            <span>添加 Jira 连接</span>
          </div>
        </div>
      </div>
    </div>

    <!-- 通用配置 -->
    <div v-if="activeTab === 'general'" class="card">
      <h3>报告项目列表</h3>
      <p class="field-desc">配置可选的项目代号，用于生成报告时按项目筛选。Confluence 和 Jira 报告共用此列表。多个项目用英文逗号分隔。</p>
      <div class="form-grid">
        <div class="field">
          <label>项目列表</label>
          <input class="input" v-model="generalConfig.confluence_projects" placeholder="G01,G32,G33（逗号分隔）" />
        </div>
      </div>
      <div class="form-actions">
        <button class="btn btn-primary" @click="saveGeneral" :disabled="saving">保存</button>
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
          <thead><tr><th>用户名</th><th>显示名</th><th>角色</th><th>来源</th><th>状态</th><th>创建时间</th></tr></thead>
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
              <td class="date-cell">{{ l.created_at }}</td><td>{{ l.username }}</td><td>{{ l.action }}</td><td>{{ l.detail }}</td><td>{{ l.ip }}</td>
            </tr>
          </tbody>
        </table>
      </div>
    </div>

    <!-- 连接编辑弹窗 -->
    <div v-if="showConnModal" class="modal-overlay" @click.self="showConnModal = false">
      <div class="modal card conn-modal">
        <h3>{{ editingConn.id ? '编辑连接' : '添加连接' }}</h3>
        <div class="field">
          <label>连接名称</label>
          <input class="input" v-model="editingConn.name" :placeholder="editingConn.type === 'confluence' ? 'Confluence 生产' : 'Jira 生产'" />
        </div>
        <div class="form-grid">
          <div class="field">
            <label>{{ editingConn.type === 'confluence' ? 'Confluence' : 'Jira' }} URL</label>
            <input class="input" v-model="editingConn.url" placeholder="http://localhost:8091" />
          </div>
          <div class="field">
            <label>用户名</label>
            <input class="input" v-model="editingConn.username" placeholder="admin" />
          </div>
        </div>
        <div class="field">
          <label>密码 / 令牌</label>
          <input class="input" type="password" v-model="editingConn.password" placeholder="密码或 Personal Access Token" />
        </div>

        <!-- Confluence 额外配置 -->
        <template v-if="editingConn.type === 'confluence'">
          <div class="divider"></div>
          <div class="form-grid">
            <div class="field">
              <label>空间 Key</label>
              <input class="input" v-model="editingConn.config.space_key" placeholder="DEV,TEAM（逗号分隔）" />
            </div>
            <div class="field">
              <label>根目录页面标题</label>
              <input class="input" v-model="editingConn.config.root_page" placeholder="发布说明" />
            </div>
          </div>
        </template>

        <!-- Jira 额外配置 -->
        <template v-if="editingConn.type === 'jira'">
          <div class="divider"></div>
          <div class="form-grid">
            <div class="field">
              <label>Jira 项目 Key</label>
              <input class="input" v-model="editingConn.config.projects" placeholder="YX,OPS（逗号分隔）" />
            </div>
            <div class="field">
              <label>故障工单类型</label>
              <input class="input" v-model="editingConn.config.fault_issuetype" placeholder="故障" />
            </div>
          </div>
        </template>

        <div class="field checkbox-field">
          <label><input type="checkbox" v-model="editingConn.is_default" /> 设为默认连接</label>
        </div>

        <div class="form-actions">
          <button class="btn" @click="showConnModal = false">取消</button>
          <button class="btn btn-primary" @click="saveConn" :disabled="savingConn">{{ savingConn ? '保存中...' : '保存' }}</button>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup>
import { ref, computed, onMounted, watch, inject } from 'vue'
import api from '@/api'

const toast = inject('toast')
const confirm = inject('confirm')
const activeTab = ref('connections')
const saving = ref(false)
const savingConn = ref(false)

// 连接数据
const connections = ref([])
const confluenceConns = computed(() => connections.value.filter(c => c.type === 'confluence'))
const jiraConns = computed(() => connections.value.filter(c => c.type === 'jira'))

// 连接编辑弹窗
const showConnModal = ref(false)
const editingConn = ref({ type: '', name: '', url: '', username: '', password: '', config: {}, is_default: false })

// 通用配置
const generalConfig = ref({ confluence_projects: '' })

// 用户
const users = ref([])
const auditLogs = ref([])
const showAddUser = ref(false)
const newUser = ref({ username: '', password: '', display_name: '', role: 'user' })

onMounted(() => loadTabData())
watch(activeTab, () => loadTabData())

async function loadTabData() {
  if (activeTab.value === 'connections') {
    try {
      const res = await api.get('/api/connections')
      connections.value = (res.data.data || []).map(c => {
        if (typeof c.config === 'string') {
          try { c.config = JSON.parse(c.config) } catch { c.config = {} }
        }
        if (!c.config) c.config = {}
        return c
      })
    } catch (e) { /* ignore */ }
  } else if (activeTab.value === 'general') {
    try {
      const res = await api.get('/api/settings')
      generalConfig.value = res.data.data || {}
    } catch (e) { /* ignore */ }
  } else if (activeTab.value === 'users') {
    try { const res = await api.get('/api/users'); users.value = res.data.data || [] } catch (e) { /* ignore */ }
  } else if (activeTab.value === 'audit') {
    try { const res = await api.get('/api/audit-logs'); auditLogs.value = res.data.data || [] } catch (e) { /* ignore */ }
  }
}

function openAddConn(type) {
  editingConn.value = {
    type,
    name: '',
    url: '',
    username: '',
    password: '',
    config: type === 'confluence' ? { space_key: '', root_page: '' } : { projects: '', fault_issuetype: '故障' },
    is_default: connections.value.filter(c => c.type === type).length === 0, // 第一个自动设为默认
  }
  showConnModal.value = true
}

function openEditConn(conn) {
  editingConn.value = {
    id: conn.id,
    type: conn.type,
    name: conn.name,
    url: conn.url,
    username: conn.username,
    password: conn.password || '',
    config: { ...conn.config },
    is_default: conn.is_default,
  }
  showConnModal.value = true
}

async function saveConn() {
  savingConn.value = true
  try {
    const data = {
      type: editingConn.value.type,
      name: editingConn.value.name,
      url: editingConn.value.url,
      username: editingConn.value.username,
      password: editingConn.value.password,
      config: editingConn.value.config,
      is_default: editingConn.value.is_default,
    }
    if (editingConn.value.id) {
      await api.put(`/api/connections/${editingConn.value.id}`, data)
    } else {
      await api.post('/api/connections', data)
    }
    toast('保存成功', 'success')
    showConnModal.value = false
    loadTabData()
  } catch (e) {
    toast(e.response?.data?.error || '保存失败', 'error')
  } finally { savingConn.value = false }
}

async function deleteConn(conn) {
  const ok = await confirm({ title: '确认删除', message: `删除连接「${conn.name}」？`, type: 'warning', okText: '删除' })
  if (!ok) return
  try {
    await api.delete(`/api/connections/${conn.id}`)
    toast('已删除', 'success')
    loadTabData()
  } catch (e) {
    toast('删除失败', 'error')
  }
}

async function testConn(conn) {
  conn._testing = true
  conn._testResult = null
  try {
    const res = await api.post('/api/connections/test', {
      id: conn.id, type: conn.type, url: conn.url, username: conn.username, password: conn.password,
    })
    conn._testResult = { ok: true, message: `连接成功！用户: ${res.data.data?.user || ''}` }
  } catch (e) {
    conn._testResult = { ok: false, message: e.response?.data?.error || '连接失败' }
  } finally { conn._testing = false }
}

async function saveGeneral() {
  saving.value = true
  try {
    await api.post('/api/settings', generalConfig.value)
    toast('配置已保存', 'success')
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
  } catch (e) { toast(e.response?.data?.error || '创建失败', 'error') }
}
</script>

<style scoped>
.tabs { display: flex; gap: 0; margin-bottom: 20px; border-bottom: 1px solid var(--border); }
.tab {
  padding: 10px 20px; background: none; border: none;
  border-bottom: 2px solid transparent; color: var(--text-secondary);
  font-size: 14px; cursor: pointer; transition: all var(--transition); font-family: var(--font-sans);
}
.tab:hover { color: var(--text-primary); }
.tab.active { color: var(--primary-light); border-bottom-color: var(--primary-light); }

h3 { font-size: 16px; margin-bottom: 16px; }
.form-grid { display: grid; grid-template-columns: 1fr 1fr; gap: 16px; }
.field { display: flex; flex-direction: column; gap: 4px; }
.field label { font-size: 13px; color: var(--text-secondary); font-weight: 500; }
.checkbox-field label { flex-direction: row; gap: 8px; cursor: pointer; align-items: center; display: flex; }
.checkbox-field input[type="checkbox"] { width: 16px; height: 16px; accent-color: var(--primary-light); }
.form-actions { display: flex; gap: 8px; margin-top: 20px; }
.divider { height: 1px; background: var(--border); margin: 20px 0; }
.field-desc { font-size: 13px; color: var(--text-muted); margin-bottom: 12px; }

/* 连接管理 */
.conn-section { margin-bottom: 28px; }
.section-header { display: flex; justify-content: space-between; align-items: center; margin-bottom: 14px; }
.section-header h3 { margin: 0; }
.section-label { display: flex; align-items: center; gap: 10px; }
.type-icon {
  width: 32px; height: 32px; border-radius: 8px; display: flex; align-items: center; justify-content: center;
  font-weight: 700; font-size: 16px; color: #fff;
}
.type-icon.confluence { background: linear-gradient(135deg, #0052CC, #2684FF); }
.type-icon.jira { background: linear-gradient(135deg, #0065FF, #2684FF); }

.conn-grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(320px, 1fr)); gap: 14px; }

.conn-card {
  background: var(--bg-secondary); border: 1px solid var(--border); border-radius: 10px;
  padding: 18px; transition: all 0.2s; position: relative;
}
.conn-card:hover { border-color: var(--primary-light); }
.conn-card.default { border-color: rgba(76,154,255,0.3); background: rgba(76,154,255,0.04); }
.conn-card.empty {
  border-style: dashed; display: flex; flex-direction: column; align-items: center;
  justify-content: center; gap: 8px; min-height: 120px; cursor: pointer;
  color: var(--text-muted); font-size: 14px;
}
.conn-card.empty:hover { border-color: var(--primary-light); color: var(--primary-light); }
.empty-icon { font-size: 28px; font-weight: 300; }

.conn-card-header { display: flex; justify-content: space-between; align-items: center; margin-bottom: 8px; }
.conn-name { font-weight: 600; font-size: 15px; color: var(--text-primary); }
.default-badge {
  font-size: 11px; padding: 2px 8px; border-radius: 4px;
  background: rgba(76,154,255,0.15); color: var(--primary-light); font-weight: 500;
}
.conn-url { font-size: 13px; color: var(--text-secondary); font-family: var(--font-mono); margin-bottom: 8px; word-break: break-all; }
.conn-meta { display: flex; gap: 8px; flex-wrap: wrap; margin-bottom: 12px; }
.conn-meta span { font-size: 12px; color: var(--text-muted); }
.meta-tag { background: rgba(255,255,255,0.05); padding: 2px 8px; border-radius: 4px; }
.conn-actions { display: flex; gap: 6px; }

.btn-xs { padding: 4px 10px; font-size: 12px; }
.btn-xs.danger { color: var(--danger); border-color: var(--danger); }
.btn-xs.danger:hover { background: rgba(239,68,68,0.1); }

.test-msg { margin-top: 8px; font-size: 12px; padding: 6px 10px; border-radius: 6px; }
.test-msg.ok { background: rgba(16,185,129,0.1); color: var(--success); }
.test-msg.fail { background: rgba(239,68,68,0.1); color: var(--danger); }

/* 弹窗 */
.modal-overlay {
  position: fixed; inset: 0; background: rgba(0,0,0,0.5);
  display: flex; align-items: center; justify-content: center; z-index: 1000;
}
.modal { display: flex; flex-direction: column; gap: 12px; }
.conn-modal { width: 520px; max-height: 90vh; overflow-y: auto; }

.badge.admin { background: rgba(76,154,255,0.15); color: var(--primary-light); }
.status-dot { display: inline-block; width: 6px; height: 6px; border-radius: 50%; margin-right: 6px; }
.status-dot.active { background: var(--success); }
.status-dot.disabled { background: var(--danger); }
.date-cell { font-size: 13px; color: var(--text-secondary); white-space: nowrap; }

.test-result { margin-top: 12px; padding: 10px 14px; border-radius: var(--radius); font-size: 14px; }
.test-result.success { background: rgba(16,185,129,0.1); color: var(--success); }
.test-result.error { background: rgba(239,68,68,0.1); color: var(--danger); }
</style>
