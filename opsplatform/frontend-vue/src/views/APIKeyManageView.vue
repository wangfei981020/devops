<script setup>
import { ref, computed, onMounted } from 'vue'
import api from '@/api'
import { useAppStore, useAuthStore } from '@/stores'

const appStore = useAppStore()
const authStore = useAuthStore()
const isSuperAdmin = computed(() => authStore.isSuperAdmin())
const canCreate = computed(() => isSuperAdmin.value || authStore.hasPermission('api_key:create'))
const canUpdate = computed(() => isSuperAdmin.value || authStore.hasPermission('api_key:update'))
const canDelete = computed(() => isSuperAdmin.value || authStore.hasPermission('api_key:delete'))

const keys = ref([])
const tables = ref([])  // 自定义表列表，用于 allowed_table_ids picker
const loading = ref(false)

// 业务域 → 可选权限码后缀
const DOMAIN_SCOPES = {
  table_maintenance: [
    { suffix: 'read',   label: '查看记录' },
    { suffix: 'create', label: '新增记录' },
    { suffix: 'update', label: '编辑记录' },
    { suffix: 'delete', label: '删除记录' },
    { suffix: 'upload', label: '上传附件' }
  ]
  // 以后加 duty / schedule 域的 scope 就在这里扩
}

const showFormDialog = ref(false)
const showGeneratedDialog = ref(false)
const editingKey = ref(null)
const generatedKey = ref(null)

const form = ref({
  name: '',
  description: '',
  domain: 'table_maintenance',
  scopes: [],
  allowed_table_ids: [],
  expiresMode: 'permanent',  // permanent | 7d | 30d | 90d | 180d | 365d | custom
  expiresCustom: ''
})

const availableScopes = computed(() => DOMAIN_SCOPES[form.value.domain] || [])

onMounted(() => {
  loadKeys()
  loadTables()
})

async function loadKeys() {
  loading.value = true
  try {
    const res = await api.get('/api/api-keys')
    keys.value = Array.isArray(res.data) ? res.data : []
  } catch (e) {
    appStore.showToast('加载失败: ' + (e.response?.data?.error || e.message), 'error')
  } finally {
    loading.value = false
  }
}

async function loadTables() {
  try {
    const res = await api.get('/api/custom-tables')
    tables.value = Array.isArray(res.data) ? res.data : []
  } catch (e) {
    tables.value = []
  }
}

function openCreate() {
  editingKey.value = null
  form.value = {
    name: '',
    description: '',
    domain: 'table_maintenance',
    scopes: ['table_maintenance:create', 'table_maintenance:update', 'table_maintenance:upload', 'table_maintenance:read'],
    allowed_table_ids: [],
    expiresMode: 'permanent',
    expiresCustom: ''
  }
  showFormDialog.value = true
}

function openEdit(k) {
  editingKey.value = k
  form.value = {
    name: k.name,
    description: k.description || '',
    domain: k.domain,
    scopes: [...(k.scopes || [])],
    allowed_table_ids: [...(k.allowed_table_ids || [])],
    expiresMode: k.expires_at ? 'custom' : 'permanent',
    expiresCustom: k.expires_at ? formatForInput(k.expires_at) : ''
  }
  showFormDialog.value = true
}

function formatForInput(iso) {
  if (!iso) return ''
  const d = new Date(iso)
  const pad = n => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${pad(d.getMonth()+1)}-${pad(d.getDate())}T${pad(d.getHours())}:${pad(d.getMinutes())}`
}

function computeExpiresAt() {
  const m = form.value.expiresMode
  if (m === 'permanent') return ''
  if (m === 'custom') {
    if (!form.value.expiresCustom) return ''
    return form.value.expiresCustom.replace('T', ' ') + ':00'
  }
  const days = { '7d': 7, '30d': 30, '90d': 90, '180d': 180, '365d': 365 }[m]
  if (!days) return ''
  const t = new Date()
  t.setDate(t.getDate() + days)
  const pad = n => String(n).padStart(2, '0')
  return `${t.getFullYear()}-${pad(t.getMonth()+1)}-${pad(t.getDate())} ${pad(t.getHours())}:${pad(t.getMinutes())}:${pad(t.getSeconds())}`
}

async function saveKey() {
  if (!form.value.name.trim()) { appStore.showToast('请填写名称', 'error'); return }
  if (form.value.scopes.length === 0) { appStore.showToast('至少勾选一个权限', 'error'); return }
  if (form.value.allowed_table_ids.length === 0) { appStore.showToast('必须至少勾选一张允许访问的表', 'error'); return }

  const expires_at = computeExpiresAt()

  try {
    if (editingKey.value) {
      await api.put('/api/api-keys/' + editingKey.value.id, {
        name: form.value.name,
        description: form.value.description,
        scopes: form.value.scopes,
        allowed_table_ids: form.value.allowed_table_ids,
        expires_at,
        clear_expires: form.value.expiresMode === 'permanent'
      })
      appStore.showToast('已更新', 'success')
      showFormDialog.value = false
      loadKeys()
    } else {
      const res = await api.post('/api/api-keys', {
        name: form.value.name,
        description: form.value.description,
        domain: form.value.domain,
        scopes: form.value.scopes,
        allowed_table_ids: form.value.allowed_table_ids,
        expires_at
      })
      showFormDialog.value = false
      generatedKey.value = res.data
      showGeneratedDialog.value = true
      loadKeys()
    }
  } catch (e) {
    appStore.showToast('保存失败: ' + (e.response?.data?.error || e.message), 'error')
  }
}

async function toggleEnabled(k) {
  try {
    await api.put('/api/api-keys/' + k.id, { enabled: !k.enabled })
    loadKeys()
  } catch (e) {
    appStore.showToast('操作失败', 'error')
  }
}

async function deleteKey(k) {
  const ok = await appStore.showConfirm({
    type: 'danger',
    title: '删除 API Key',
    message: `确定删除 "${k.name}"？删除后调用方将立即失败。`,
    okText: '删除',
    cancelText: '取消'
  })
  if (!ok) return
  try {
    await api.delete('/api/api-keys/' + k.id)
    appStore.showToast('已删除', 'success')
    loadKeys()
  } catch (e) {
    appStore.showToast('删除失败', 'error')
  }
}

async function copyText(text) {
  try {
    await navigator.clipboard.writeText(text)
    appStore.showToast('已复制', 'success')
  } catch {
    appStore.showToast('复制失败', 'error')
  }
}

function displayKey(k) {
  return `${k.key_prefix}****${k.key_suffix}`
}

function fmtTime(t) {
  if (!t) return '-'
  const d = new Date(t)
  const pad = n => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${pad(d.getMonth()+1)}-${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}`
}

function expiryState(k) {
  if (!k.expires_at) return { text: '永久', class: 'tag-gray' }
  const now = Date.now()
  const t = new Date(k.expires_at).getTime()
  if (t < now) return { text: '已过期', class: 'tag-red' }
  const days = Math.floor((t - now) / 86400000)
  if (days < 7) return { text: `${days}天后`, class: 'tag-orange' }
  return { text: fmtTime(k.expires_at), class: 'tag-gray' }
}
</script>

<template>
  <div class="apikey-page">
    <div class="page-header">
      <div>
        <h2>API Key 管理</h2>
        <p class="page-desc">生成 API Key 供外部系统调用维护记录接口。权限基于现有权限码体系，可限定到具体业务域和表。</p>
      </div>
      <button v-if="canCreate" class="btn btn-primary" @click="openCreate">+ 新建 Key</button>
    </div>

    <div class="table-wrap">
      <table class="data-table">
        <thead>
          <tr>
            <th>名称</th>
            <th>Key</th>
            <th>业务域</th>
            <th>权限</th>
            <th>允许的表</th>
            <th>过期时间</th>
            <th>最后使用</th>
            <th>状态</th>
            <th>操作</th>
          </tr>
        </thead>
        <tbody>
          <tr v-if="loading"><td colspan="9" class="cell-muted">加载中...</td></tr>
          <tr v-else-if="!keys.length"><td colspan="9" class="cell-muted">暂无 API Key，点右上角新建</td></tr>
          <tr v-for="k in keys" :key="k.id">
            <td>
              <div class="name-cell">
                <strong>{{ k.name }}</strong>
                <div v-if="k.description" class="desc">{{ k.description }}</div>
              </div>
            </td>
            <td class="mono">
              {{ displayKey(k) }}
              <button class="icon-btn" @click="copyText(k.key_prefix)" title="复制前缀">⎘</button>
            </td>
            <td><span class="tag tag-blue">{{ k.domain }}</span></td>
            <td>
              <span v-for="s in k.scopes" :key="s" class="tag tag-gray small">{{ s.split(':')[1] }}</span>
            </td>
            <td>
              <span v-if="!k.allowed_table_ids?.length" class="cell-muted">全部</span>
              <span v-else>{{ k.allowed_table_ids.length }} 张</span>
            </td>
            <td>
              <span :class="'tag small ' + expiryState(k).class">{{ expiryState(k).text }}</span>
            </td>
            <td>{{ fmtTime(k.last_used_at) }}</td>
            <td>
              <label class="switch">
                <input type="checkbox" :checked="k.enabled" :disabled="!canUpdate" @change="toggleEnabled(k)">
                <span class="slider"></span>
              </label>
            </td>
            <td class="action-cell">
              <button v-if="canUpdate" class="action-btn" @click="openEdit(k)" title="编辑">✎</button>
              <button v-if="canDelete" class="action-btn danger" @click="deleteKey(k)" title="删除">🗑</button>
            </td>
          </tr>
        </tbody>
      </table>
    </div>

    <!-- 创建/编辑弹窗 -->
    <div v-if="showFormDialog" class="modal-backdrop" @click.self="() => {}">
      <div class="modal">
        <div class="modal-header">
          <h3>{{ editingKey ? '编辑 API Key' : '新建 API Key' }}</h3>
          <button class="close-btn" @click="showFormDialog = false">×</button>
        </div>
        <div class="modal-body">
          <div class="form-row">
            <label>名称 <span class="required">*</span></label>
            <input v-model="form.name" type="text" placeholder="如：监控系统对接" maxlength="100">
          </div>
          <div class="form-row">
            <label>描述</label>
            <input v-model="form.description" type="text" placeholder="说明用途（可选）" maxlength="500">
          </div>
          <div class="form-row">
            <label>业务域 <span class="required">*</span></label>
            <select v-model="form.domain" :disabled="!!editingKey">
              <option value="table_maintenance">桌台维护记录 (table_maintenance)</option>
            </select>
            <small v-if="editingKey" class="hint">业务域创建后不可更改</small>
          </div>
          <div class="form-row">
            <label>权限 <span class="required">*</span></label>
            <div class="scope-grid">
              <label v-for="s in availableScopes" :key="s.suffix" class="check-item">
                <input type="checkbox" :value="form.domain + ':' + s.suffix" v-model="form.scopes">
                <span>{{ s.label }} <code>{{ form.domain }}:{{ s.suffix }}</code></span>
              </label>
            </div>
          </div>
          <div class="form-row">
            <label>限定表格 <span class="required">*</span></label>
            <div class="table-picker">
              <label v-for="t in tables" :key="t.id" class="check-item with-copy">
                <input type="checkbox" :value="t.id" v-model="form.allowed_table_ids">
                <span class="table-name">{{ t.name }}</span>
                <button type="button" class="copy-id-btn" @click.stop.prevent="copyText(t.id)" :title="`复制 tableID: ${t.id}`">📋 ID</button>
              </label>
              <div v-if="!tables.length" class="cell-muted">加载中...</div>
            </div>
            <small class="hint">必须至少勾选一张。此 key 只能访问勾选的表，不勾则无任何表访问权限</small>
          </div>
          <div class="form-row">
            <label>过期时间</label>
            <div class="expires-row">
              <select v-model="form.expiresMode">
                <option value="permanent">永久有效</option>
                <option value="7d">7 天后</option>
                <option value="30d">30 天后</option>
                <option value="90d">90 天后</option>
                <option value="180d">180 天后</option>
                <option value="365d">1 年后</option>
                <option value="custom">自定义时间</option>
              </select>
              <input v-if="form.expiresMode === 'custom'" type="datetime-local" v-model="form.expiresCustom">
            </div>
          </div>
        </div>
        <div class="modal-footer">
          <button class="btn btn-default" @click="showFormDialog = false">取消</button>
          <button class="btn btn-primary" @click="saveKey">保存</button>
        </div>
      </div>
    </div>

    <!-- 生成成功弹窗 -->
    <div v-if="showGeneratedDialog && generatedKey" class="modal-backdrop" @click.self="() => {}">
      <div class="modal small">
        <div class="modal-header">
          <h3>API Key 已生成</h3>
        </div>
        <div class="modal-body">
          <div class="warn-banner">
            ⚠ 这是您唯一一次看到完整 Key 的机会。请立即复制并妥善保存，关闭后只能看到前缀和后 6 位。
          </div>
          <div class="form-row">
            <label>名称</label>
            <div>{{ generatedKey.name }}</div>
          </div>
          <div class="form-row">
            <label>完整 Key</label>
            <div class="key-box">
              <code class="mono">{{ generatedKey.key }}</code>
              <button class="btn btn-primary small" @click="copyText(generatedKey.key)">复制</button>
            </div>
          </div>
        </div>
        <div class="modal-footer">
          <button class="btn btn-primary" @click="() => { showGeneratedDialog = false; generatedKey = null }">我已保存，关闭</button>
        </div>
      </div>
    </div>
  </div>
</template>

<style scoped>
.apikey-page { padding: 20px; color: var(--text-primary); }
.page-header { display: flex; justify-content: space-between; align-items: start; margin-bottom: 20px; }
.page-header h2 { margin: 0 0 6px 0; font-size: 20px; color: var(--text-primary); }
.page-desc { margin: 0; color: var(--text-secondary); font-size: 13px; }

.btn { padding: 6px 14px; border-radius: 4px; border: 1px solid transparent; cursor: pointer; font-size: 13px; }
.btn.small { padding: 4px 10px; font-size: 12px; }
.btn-primary { background: var(--primary); color: white; border-color: var(--primary); }
.btn-primary:hover { background: var(--primary-dark); }
.btn-default { background: var(--bg-input); color: var(--text-primary); border-color: var(--border-color); }
.btn-default:hover { background: var(--bg-hover); }

.table-wrap { background: var(--bg-card); border-radius: 6px; box-shadow: var(--shadow-sm); border: 1px solid var(--border-color); overflow-x: auto; }
.data-table { width: 100%; border-collapse: collapse; font-size: 13px; color: var(--text-primary); }
.data-table th, .data-table td { padding: 10px 12px; text-align: left; border-bottom: 1px solid var(--border-color); vertical-align: middle; }
.data-table th { background: var(--bg-input); font-weight: 600; color: var(--text-primary); white-space: nowrap; }
.cell-muted { color: var(--text-muted); text-align: center; padding: 24px; }
.name-cell strong { color: var(--text-primary); }
.name-cell .desc { color: var(--text-muted); font-size: 11px; margin-top: 2px; }
.mono { font-family: Consolas, Monaco, monospace; font-size: 12px; color: var(--text-secondary); }

.tag { display: inline-block; padding: 2px 8px; border-radius: 3px; font-size: 11px; margin-right: 4px; margin-bottom: 2px; }
.tag.small { font-size: 10px; padding: 1px 6px; }
.tag-blue { background: rgba(59, 130, 246, 0.15); color: var(--primary); }
.tag-gray { background: var(--bg-hover); color: var(--text-secondary); }
.tag-orange { background: rgba(245, 158, 11, 0.15); color: var(--warning); }
.tag-red { background: rgba(239, 68, 68, 0.15); color: var(--danger); }

.icon-btn { background: none; border: none; cursor: pointer; color: var(--text-muted); padding: 2px 6px; }
.icon-btn:hover { color: var(--primary); }
.action-cell { white-space: nowrap; }
.action-btn { background: none; border: 1px solid transparent; padding: 3px 8px; margin-right: 4px; cursor: pointer; border-radius: 3px; color: var(--text-secondary); }
.action-btn:hover { background: var(--bg-hover); color: var(--text-primary); }
.action-btn.danger:hover { background: rgba(239, 68, 68, 0.1); color: var(--danger); }

.switch { position: relative; display: inline-block; width: 36px; height: 20px; }
.switch input { opacity: 0; width: 0; height: 0; }
.slider { position: absolute; cursor: pointer; top: 0; left: 0; right: 0; bottom: 0; background: var(--text-muted); transition: .2s; border-radius: 20px; }
.slider:before { position: absolute; content: ""; height: 14px; width: 14px; left: 3px; bottom: 3px; background: white; transition: .2s; border-radius: 50%; }
.switch input:checked + .slider { background: var(--success); }
.switch input:checked + .slider:before { transform: translateX(16px); }
.switch input:disabled + .slider { opacity: 0.5; cursor: not-allowed; }

.modal-backdrop { position: fixed; inset: 0; background: rgba(0,0,0,0.5); display: flex; justify-content: center; align-items: center; z-index: 1000; }
.modal { background: var(--bg-card); color: var(--text-primary); border-radius: 6px; width: 600px; max-height: 85vh; display: flex; flex-direction: column; box-shadow: var(--shadow-lg); }
.modal.small { width: 520px; }
.modal-header { padding: 16px 20px; border-bottom: 1px solid var(--border-color); display: flex; justify-content: space-between; align-items: center; }
.modal-header h3 { margin: 0; font-size: 16px; color: var(--text-primary); }
.close-btn { background: none; border: none; font-size: 24px; color: var(--text-muted); cursor: pointer; line-height: 1; }
.close-btn:hover { color: var(--text-primary); }
.modal-body { padding: 20px; overflow-y: auto; }
.modal-footer { padding: 12px 20px; border-top: 1px solid var(--border-color); text-align: right; }
.modal-footer .btn { margin-left: 8px; }

.form-row { margin-bottom: 16px; }
.form-row label { display: block; font-size: 13px; color: var(--text-primary); margin-bottom: 6px; font-weight: 500; }
.required { color: var(--danger); }
.form-row input[type=text], .form-row input[type=datetime-local], .form-row select {
  width: 100%; padding: 6px 10px; border: 1px solid var(--border-color); border-radius: 4px;
  font-size: 13px; box-sizing: border-box; background: var(--bg-input); color: var(--text-primary);
}
.form-row input:focus, .form-row select:focus { outline: none; border-color: var(--primary); }
.hint { display: block; margin-top: 4px; color: var(--text-muted); font-size: 11px; }

.scope-grid, .table-picker {
  display: grid; grid-template-columns: 1fr 1fr; gap: 6px; padding: 8px;
  background: var(--bg-input); border-radius: 4px; max-height: 180px; overflow-y: auto;
}
.check-item { display: flex; align-items: center; gap: 6px; font-size: 12px; cursor: pointer; font-weight: normal !important; color: var(--text-primary); }
.check-item code { background: var(--bg-hover); padding: 1px 4px; border-radius: 2px; font-size: 11px; color: var(--text-secondary); }
.check-item code.small { font-size: 10px; }
.check-item.with-copy { justify-content: space-between; }
.check-item .table-name { flex: 1; }
.copy-id-btn { background: rgba(59, 130, 246, 0.15); border: 1px solid var(--primary); color: var(--primary); padding: 2px 8px; border-radius: 3px; font-size: 11px; cursor: pointer; }
.copy-id-btn:hover { background: rgba(59, 130, 246, 0.3); }

.expires-row { display: flex; gap: 8px; }
.expires-row select { width: 160px; }
.expires-row input[type=datetime-local] { flex: 1; }

.warn-banner { background: rgba(245, 158, 11, 0.1); border: 1px solid var(--warning); padding: 10px 14px; border-radius: 4px; font-size: 12px; color: var(--warning); margin-bottom: 16px; }
.key-box { display: flex; align-items: center; gap: 8px; background: var(--bg-input); padding: 10px; border-radius: 4px; }
.key-box code { flex: 1; word-break: break-all; font-size: 12px; color: var(--text-primary); }
</style>
