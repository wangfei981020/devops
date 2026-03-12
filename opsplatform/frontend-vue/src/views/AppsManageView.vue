<script setup>
import { ref, onMounted } from 'vue'
import api from '@/api'

const apps = ref([])
const roles = ref([])
const loading = ref(false)
const showForm = ref(false)
const showRoleDialog = ref(false)
const editingApp = ref(null)

const form = ref({
  app_key: '',
  name: '',
  url: '',
  icon_svg: '',
  group_name: '',
  sort_order: 0,
  status: 'active'
})

const roleForm = ref({
  appKey: '',
  appName: '',
  selectedRoles: []
})

onMounted(() => {
  loadApps()
  loadRoles()
})

async function loadApps() {
  loading.value = true
  try {
    const res = await api.get('/api/external-apps')
    const d = res.data?.data ?? res.data
    apps.value = Array.isArray(d) ? d : []
  } catch (e) {
    console.log('加载失败:', e)
  } finally {
    loading.value = false
  }
}

async function loadRoles() {
  try {
    const res = await api.get('/api/roles')
    const d = res.data?.data ?? res.data
    roles.value = Array.isArray(d) ? d : []
  } catch (e) {
    console.log('加载角色失败:', e)
  }
}

function openCreate() {
  editingApp.value = null
  form.value = { app_key: '', name: '', url: '', icon_svg: '', group_name: '', sort_order: 0, status: 'active' }
  showForm.value = true
}

function openEdit(app) {
  editingApp.value = app
  form.value = { ...app }
  showForm.value = true
}

async function saveApp() {
  try {
    if (editingApp.value) {
      await api.put('/api/external-apps/' + editingApp.value.id, form.value)
    } else {
      await api.post('/api/external-apps', form.value)
    }
    showForm.value = false
    loadApps()
  } catch (e) {
    alert('保存失败: ' + (e.response?.data?.error || e.message))
  }
}

async function deleteApp(app) {
  if (!confirm('确定删除应用 "' + app.name + '"？')) return
  try {
    await api.delete('/api/external-apps/' + app.id)
    loadApps()
  } catch (e) {
    alert('删除失败')
  }
}

async function openRoleConfig(app) {
  roleForm.value.appKey = app.app_key
  roleForm.value.appName = app.name
  try {
    const res = await api.get('/api/external-apps/' + app.app_key + '/roles')
    const d = res.data?.data ?? res.data
    roleForm.value.selectedRoles = Array.isArray(d) ? d : []
  } catch (e) {
    roleForm.value.selectedRoles = []
  }
  showRoleDialog.value = true
}

function toggleRole(roleId) {
  const idx = roleForm.value.selectedRoles.indexOf(roleId)
  if (idx >= 0) roleForm.value.selectedRoles.splice(idx, 1)
  else roleForm.value.selectedRoles.push(roleId)
}

async function saveRoles() {
  try {
    await api.put('/api/external-apps/' + roleForm.value.appKey + '/roles', {
      role_ids: roleForm.value.selectedRoles
    })
    showRoleDialog.value = false
  } catch (e) {
    alert('保存失败')
  }
}

const testResult = ref(null)
const testing = ref(false)

async function testURL() {
  if (!form.value.url) return
  testing.value = true
  testResult.value = null
  try {
    const res = await api.post('/api/external-apps/test-url', { url: form.value.url })
    const d = res.data?.data ?? res.data
    testResult.value = d
  } catch (e) {
    testResult.value = { reachable: false, message: '请求失败' }
  } finally {
    testing.value = false
  }
}

const presetIcons = {
  'bug': '<circle cx="12" cy="8" r="2"/><path d="M12 10v6"/><path d="M8 14h8"/><path d="M7 8l-3-1"/><path d="M17 8l3-1"/><path d="M7 14l-3 1"/><path d="M17 14l3 1"/><path d="M9 18l-1 2"/><path d="M15 18l1 2"/>',
  'monitor': '<polyline points="22 12 18 12 15 21 9 3 6 12 2 12"/>',
  'database': '<ellipse cx="12" cy="5" rx="9" ry="3"/><path d="M21 12c0 1.66-4 3-9 3s-9-1.34-9-3"/><path d="M3 5v14c0 1.66 4 3 9 3s9-1.34 9-3V5"/>',
  'shield': '<path d="M12 22s8-4 8-10V5l-8-3-8 3v7c0 6 8 10 8 10z"/>',
  'code': '<polyline points="16 18 22 12 16 6"/><polyline points="8 6 2 12 8 18"/>',
  'globe': '<circle cx="12" cy="12" r="10"/><line x1="2" y1="12" x2="22" y2="12"/><path d="M12 2a15.3 15.3 0 0 1 4 10 15.3 15.3 0 0 1-4 10 15.3 15.3 0 0 1-4-10 15.3 15.3 0 0 1 4-10z"/>',
  'terminal': '<polyline points="4 17 10 11 4 5"/><line x1="12" y1="19" x2="20" y2="19"/>',
  'chart': '<line x1="18" y1="20" x2="18" y2="10"/><line x1="12" y1="20" x2="12" y2="4"/><line x1="6" y1="20" x2="6" y2="14"/>'
}
</script>

<template>
  <div class="apps-manage">
    <div class="page-header">
      <h2>应用管理</h2>
      <button class="btn-primary" @click="openCreate">注册应用</button>
    </div>

    <div class="apps-table-wrap">
      <table class="data-table">
        <thead>
          <tr>
            <th>应用标识</th>
            <th>名称</th>
            <th>分组</th>
            <th>URL</th>
            <th>图标</th>
            <th>排序</th>
            <th>状态</th>
            <th>操作</th>
          </tr>
        </thead>
        <tbody>
          <tr v-if="loading">
            <td colspan="8" class="empty-cell">加载中...</td>
          </tr>
          <tr v-else-if="apps.length === 0">
            <td colspan="8" class="empty-cell">暂无注册应用</td>
          </tr>
          <tr v-for="app in apps" :key="app.id">
            <td><code>{{ app.app_key }}</code></td>
            <td>{{ app.name }}</td>
            <td>{{ app.group_name || '-' }}</td>
            <td class="url-cell" :title="app.url">{{ app.url }}</td>
            <td>
              <svg v-if="app.icon_svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round" class="app-icon-preview" v-html="app.icon_svg"></svg>
              <span v-else class="no-icon">-</span>
            </td>
            <td>{{ app.sort_order }}</td>
            <td>
              <span :class="['status-tag', app.status === 'active' ? 'active' : 'inactive']">
                {{ app.status === 'active' ? '启用' : '停用' }}
              </span>
            </td>
            <td class="action-cell">
              <button class="btn-sm" @click="openRoleConfig(app)">权限</button>
              <button class="btn-sm" @click="openEdit(app)">编辑</button>
              <button class="btn-sm btn-danger" @click="deleteApp(app)">删除</button>
            </td>
          </tr>
        </tbody>
      </table>
    </div>

    <!-- 创建/编辑弹窗 -->
    <Teleport to="body">
    <div class="am-modal-overlay" v-if="showForm">
      <div class="am-modal-box">
        <h3>{{ editingApp ? '编辑应用' : '注册应用' }}</h3>
        <div class="form-group">
          <label>应用标识 (app_key)</label>
          <input v-model="form.app_key" :disabled="!!editingApp" placeholder="如 jira-center">
        </div>
        <div class="form-group">
          <label>名称</label>
          <input v-model="form.name" placeholder="如 Jira工单中心">
        </div>
        <div class="form-group">
          <label>URL</label>
          <div class="url-input-row">
            <input v-model="form.url" placeholder="如 http://jira-ops.example.com" @input="testResult = null">
            <button class="btn-test" :disabled="!form.url || testing" @click="testURL">
              {{ testing ? '测试中...' : '测试连通' }}
            </button>
          </div>
          <div v-if="testResult" class="test-result" :class="testResult.reachable ? 'success' : 'fail'">
            {{ testResult.message }}
          </div>
        </div>
        <div class="form-group">
          <label>分组名称</label>
          <input v-model="form.group_name" placeholder="如 运维工具">
        </div>
        <div class="form-group">
          <label>排序</label>
          <input v-model.number="form.sort_order" type="number">
        </div>
        <div class="form-group">
          <label>状态</label>
          <select v-model="form.status">
            <option value="active">启用</option>
            <option value="inactive">停用</option>
          </select>
        </div>
        <div class="form-group">
          <label>图标 SVG (选择预设或自定义)</label>
          <div class="icon-presets">
            <button
              v-for="(svg, key) in presetIcons"
              :key="key"
              class="icon-preset-btn"
              :class="{ selected: form.icon_svg === svg }"
              @click="form.icon_svg = svg"
              :title="key"
            >
              <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round" v-html="svg"></svg>
            </button>
          </div>
          <textarea v-model="form.icon_svg" rows="2" placeholder="SVG path 内容（viewBox 24x24）" class="icon-textarea"></textarea>
        </div>
        <div class="modal-actions">
          <button class="btn-secondary" @click="showForm = false">取消</button>
          <button class="btn-primary" @click="saveApp">保存</button>
        </div>
      </div>
    </div>
    </Teleport>

    <!-- 权限配置弹窗 -->
    <Teleport to="body">
    <div class="am-modal-overlay" v-if="showRoleDialog">
      <div class="am-modal-box am-modal-sm">
        <h3>{{ roleForm.appName }} - 角色权限</h3>
        <p class="hint">勾选的角色可以访问此应用</p>
        <div class="role-list">
          <label v-for="role in roles" :key="role.id" class="role-item">
            <input
              type="checkbox"
              :checked="roleForm.selectedRoles.includes(role.id)"
              @change="toggleRole(role.id)"
            >
            <span>{{ role.name || role.id }}</span>
          </label>
        </div>
        <div class="modal-actions">
          <button class="btn-secondary" @click="showRoleDialog = false">取消</button>
          <button class="btn-primary" @click="saveRoles">保存</button>
        </div>
      </div>
    </div>
    </Teleport>
  </div>
</template>

<style scoped>
.apps-manage {
  padding: 24px;
  color: var(--text-primary);
}

.page-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-bottom: 20px;
}

.page-header h2 {
  font-size: 18px;
  font-weight: 600;
  margin: 0;
}

.apps-table-wrap {
  overflow-x: auto;
}

.data-table {
  width: 100%;
  border-collapse: collapse;
  font-size: 13px;
}

.data-table th,
.data-table td {
  padding: 10px 12px;
  text-align: left;
  border-bottom: 1px solid var(--border-color);
}

.data-table th {
  color: var(--text-muted);
  font-weight: 500;
  font-size: 12px;
  text-transform: uppercase;
  letter-spacing: 0.5px;
}

.data-table td code {
  background: rgba(59, 130, 246, 0.15);
  padding: 2px 6px;
  border-radius: 3px;
  font-size: 12px;
  color: var(--primary);
}

.url-cell {
  max-width: 200px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  color: var(--text-muted);
}

.app-icon-preview {
  width: 20px;
  height: 20px;
  color: var(--primary);
}

.no-icon {
  color: var(--text-muted);
}

.status-tag {
  padding: 2px 8px;
  border-radius: 10px;
  font-size: 12px;
}

.status-tag.active {
  background: rgba(34, 197, 94, 0.15);
  color: #4ade80;
}

.status-tag.inactive {
  background: rgba(239, 68, 68, 0.15);
  color: #f87171;
}

.action-cell {
  display: flex;
  gap: 6px;
}

.empty-cell {
  text-align: center !important;
  color: var(--text-muted);
  padding: 40px !important;
}

.btn-primary {
  background: linear-gradient(135deg, var(--primary), var(--primary-dark));
  color: white;
  border: none;
  padding: 8px 16px;
  border-radius: 6px;
  cursor: pointer;
  font-size: 13px;
  transition: opacity 0.15s;
}

.btn-primary:hover { opacity: 0.9; }

.btn-secondary {
  background: var(--bg-hover);
  color: var(--text-secondary);
  border: 1px solid var(--border-color);
  padding: 8px 16px;
  border-radius: 6px;
  cursor: pointer;
  font-size: 13px;
}

.btn-sm {
  padding: 4px 10px;
  font-size: 12px;
  border-radius: 4px;
  border: 1px solid var(--border-color);
  background: var(--bg-input);
  color: var(--text-secondary);
  cursor: pointer;
  transition: all 0.15s;
}

.btn-sm:hover {
  background: var(--bg-hover);
  color: var(--text-primary);
}

.btn-sm.btn-danger:hover {
  background: rgba(239, 68, 68, 0.2);
  color: #f87171;
  border-color: rgba(239, 68, 68, 0.3);
}

</style>

<style>
.am-modal-overlay {
  position: fixed;
  inset: 0;
  background: rgba(0, 0, 0, 0.6);
  display: flex;
  align-items: center;
  justify-content: center;
  z-index: 10000;
}

.am-modal-box {
  background: var(--bg-card, #1e293b);
  border: 1px solid var(--border-color, rgba(255, 255, 255, 0.08));
  border-radius: 12px;
  padding: 24px;
  width: 480px;
  max-height: 80vh;
  overflow-y: auto;
  color: var(--text-primary, rgba(255, 255, 255, 0.9));
  box-shadow: 0 20px 40px rgba(0, 0, 0, 0.3);
}

.am-modal-sm { width: 360px; }

.am-modal-box h3 {
  margin: 0 0 16px;
  font-size: 16px;
  color: var(--text-primary, #fff);
}

.am-modal-box .form-group { margin-bottom: 14px; }

.am-modal-box .form-group label {
  display: block;
  font-size: 12px;
  color: var(--text-muted, rgba(255, 255, 255, 0.5));
  margin-bottom: 4px;
}

.am-modal-box .form-group input,
.am-modal-box .form-group select,
.am-modal-box .form-group textarea {
  width: 100%;
  padding: 8px 10px;
  background: var(--bg-input, rgba(0, 0, 0, 0.3));
  border: 1px solid var(--border-color, rgba(255, 255, 255, 0.1));
  border-radius: 6px;
  color: var(--text-primary, #fff);
  font-size: 13px;
  box-sizing: border-box;
}

.am-modal-box .form-group input:focus,
.am-modal-box .form-group select:focus,
.am-modal-box .form-group textarea:focus {
  outline: none;
  border-color: #3b82f6;
}

.am-modal-box .url-input-row {
  display: flex;
  gap: 8px;
}

.am-modal-box .url-input-row input { flex: 1; }

.am-modal-box .btn-test {
  padding: 8px 12px;
  font-size: 12px;
  white-space: nowrap;
  border-radius: 6px;
  border: 1px solid var(--border-color, #2d3748);
  background: var(--bg-input, #1e2433);
  color: var(--text-secondary, #a0aec0);
  cursor: pointer;
  transition: all 0.15s;
}

.am-modal-box .btn-test:hover:not(:disabled) {
  border-color: var(--primary, #3b82f6);
  color: var(--primary, #3b82f6);
}

.am-modal-box .btn-test:disabled {
  opacity: 0.5;
  cursor: not-allowed;
}

.am-modal-box .test-result {
  font-size: 12px;
  margin-top: 6px;
  padding: 4px 8px;
  border-radius: 4px;
}

.am-modal-box .test-result.success {
  color: #4ade80;
  background: rgba(34, 197, 94, 0.1);
}

.am-modal-box .test-result.fail {
  color: #f87171;
  background: rgba(239, 68, 68, 0.1);
}

.am-modal-box .icon-presets {
  display: flex;
  gap: 6px;
  margin-bottom: 8px;
  flex-wrap: wrap;
}

.am-modal-box .icon-preset-btn {
  width: 36px;
  height: 36px;
  padding: 6px;
  background: var(--bg-input, #1e2433);
  border: 1px solid var(--border-color, #2d3748);
  border-radius: 6px;
  cursor: pointer;
  color: var(--text-secondary, #a0aec0);
  transition: all 0.15s;
}

.am-modal-box .icon-preset-btn:hover {
  background: rgba(59, 130, 246, 0.15);
  color: var(--primary, #3b82f6);
}

.am-modal-box .icon-preset-btn.selected {
  background: rgba(59, 130, 246, 0.2);
  border-color: var(--primary, #3b82f6);
  color: var(--primary, #3b82f6);
}

.am-modal-box .icon-preset-btn svg { width: 100%; height: 100%; }

.am-modal-box .icon-textarea { font-family: monospace; font-size: 11px !important; }

.am-modal-box .modal-actions {
  display: flex;
  justify-content: flex-end;
  gap: 8px;
  margin-top: 20px;
}

.am-modal-box .btn-primary {
  background: linear-gradient(135deg, var(--primary, #3b82f6), var(--primary-dark, #2563eb));
  color: white;
  border: none;
  padding: 8px 16px;
  border-radius: 6px;
  cursor: pointer;
  font-size: 13px;
}

.am-modal-box .btn-secondary {
  background: var(--bg-hover, #252d3d);
  color: var(--text-secondary, #a0aec0);
  border: 1px solid var(--border-color, #2d3748);
  padding: 8px 16px;
  border-radius: 6px;
  cursor: pointer;
  font-size: 13px;
}

.am-modal-box .hint {
  font-size: 12px;
  color: var(--text-muted, rgba(255, 255, 255, 0.4));
  margin: 0 0 12px;
}

.am-modal-box .role-list {
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.am-modal-box .role-item {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 8px 10px;
  background: var(--bg-input, #1e2433);
  border-radius: 6px;
  cursor: pointer;
  font-size: 13px;
}

.am-modal-box .role-item:hover { background: var(--bg-hover, #252d3d); }
.am-modal-box .role-item input[type="checkbox"] { accent-color: var(--primary, #3b82f6); }
</style>
