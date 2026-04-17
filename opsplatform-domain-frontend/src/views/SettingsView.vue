<template>
  <div class="page-wrap">
    <div class="main-content">
      <div class="page-header">
        <h2>系统设置</h2>
      </div>

      <!-- Tabs -->
      <div class="tabs">
        <button
          v-for="tab in tabs"
          :key="tab.key"
          class="tab-btn"
          :class="{ active: activeTab === tab.key }"
          @click="activeTab = tab.key"
        >
          {{ tab.label }}
        </button>
      </div>

      <!-- Tab: Registrar Configs -->
      <div v-if="activeTab === 'registrars'" class="tab-content">
        <div class="section-header">
          <h3>注册商配置</h3>
          <button class="btn-primary" @click="openAddRegistrar">+ 添加注册商</button>
        </div>

        <div v-if="registrars.length === 0" class="empty-state">
          <p>暂无注册商配置，点击右上角添加</p>
        </div>

        <div class="registrar-grid">
          <div v-for="reg in registrars" :key="reg.id" class="registrar-card">
            <div class="reg-card-header">
              <div class="reg-info">
                <span class="provider-badge" :class="'provider-' + reg.provider">
                  {{ providerLabel(reg.provider) }}
                </span>
                <span class="reg-name">{{ reg.name }}</span>
              </div>
              <span class="status-dot" :class="reg.status === 'active' ? 'dot-active' : 'dot-inactive'"></span>
            </div>
            <div class="reg-card-body">
              <div class="reg-detail" v-if="reg.last_sync">
                <span class="detail-label">最近同步:</span>
                <span class="detail-value">{{ reg.last_sync }}</span>
              </div>
              <div class="reg-detail" v-else>
                <span class="detail-label">最近同步:</span>
                <span class="detail-value muted">从未同步</span>
              </div>
            </div>
            <div class="reg-card-actions">
              <button class="btn-sm btn-outline" @click="testRegistrar(reg)" :disabled="testingId === reg.id">
                {{ testingId === reg.id ? '测试中...' : '测试连接' }}
              </button>
              <button class="btn-sm btn-outline" @click="syncRegistrar(reg)" :disabled="syncingId === reg.id">
                {{ syncingId === reg.id ? '同步中...' : '手动同步' }}
              </button>
              <button class="btn-sm btn-blue" @click="editRegistrar(reg)">编辑</button>
              <button class="btn-sm btn-red" @click="confirmDeleteRegistrar(reg)">删除</button>
            </div>
          </div>
        </div>
      </div>

      <!-- Tab: Global Filters -->
      <div v-if="activeTab === 'filters'" class="tab-content">
        <div class="section-header">
          <h3>全局过滤配置</h3>
        </div>
        <div class="setting-card">
          <p class="setting-desc">配置后，所有域名同步时将跳过这些主机头（如 www、mail 等）</p>

          <div class="host-list">
            <div v-for="(host, idx) in excludeHosts" :key="idx" class="host-item">
              <span class="host-tag">{{ host }}</span>
              <button class="btn-remove" @click="excludeHosts.splice(idx, 1)">×</button>
            </div>
            <div v-if="excludeHosts.length === 0" class="host-empty">暂无过滤规则</div>
          </div>

          <div class="add-host-row">
            <input
              v-model="newHost"
              type="text"
              placeholder="输入主机头，如: www, mail, ftp"
              class="host-input"
              @keyup.enter="addHost"
            />
            <button class="btn-primary" @click="addHost">添加</button>
          </div>

          <div class="save-row">
            <button class="btn-primary" @click="saveFilters" :disabled="savingFilters">
              {{ savingFilters ? '保存中...' : '保存配置' }}
            </button>
          </div>
        </div>
      </div>

      <!-- Tab: Auto Sync -->
      <div v-if="activeTab === 'autosync'" class="tab-content">
        <div class="section-header">
          <h3>自动同步配置</h3>
        </div>
        <div class="setting-card">
          <div class="setting-row">
            <div class="setting-label">
              <span class="label-main">启用自动同步</span>
              <span class="label-desc">开启后将在指定时间自动同步所有活跃注册商的域名</span>
            </div>
            <div class="toggle-wrap">
              <label class="toggle">
                <input type="checkbox" v-model="autoSyncEnabled" />
                <span class="toggle-slider"></span>
              </label>
              <span class="toggle-text">{{ autoSyncEnabled ? '已启用' : '已禁用' }}</span>
            </div>
          </div>

          <div class="setting-row" :class="{ disabled: !autoSyncEnabled }">
            <div class="setting-label">
              <span class="label-main">同步时间</span>
              <span class="label-desc">每天执行同步的时间点（24小时制）</span>
            </div>
            <div>
              <input
                type="time"
                v-model="autoSyncTime"
                :disabled="!autoSyncEnabled"
                class="time-input"
              />
            </div>
          </div>

          <div class="sync-hint" v-if="autoSyncEnabled">
            <svg width="16" height="16" viewBox="0 0 16 16" fill="none">
              <circle cx="8" cy="8" r="7" stroke="#3b82f6" stroke-width="1.5"/>
              <path d="M8 5v4M8 11v1" stroke="#3b82f6" stroke-width="1.5" stroke-linecap="round"/>
            </svg>
            下次同步将在 <strong>{{ autoSyncTime || '02:00' }}</strong> 执行
          </div>

          <div class="save-row">
            <button class="btn-primary" @click="saveAutoSync" :disabled="savingAutoSync">
              {{ savingAutoSync ? '保存中...' : '保存配置' }}
            </button>
          </div>
        </div>
      </div>

      <!-- Tab: Domain Overrides -->
      <div v-if="activeTab === 'overrides'" class="tab-content">
        <div class="section-header">
          <h3>域名级过滤配置</h3>
          <button class="btn-primary" @click="openAddOverride">+ 添加配置</button>
        </div>
        <div class="setting-card">
          <p class="setting-desc">对特定根域名配置额外的主机头过滤规则（优先于全局配置）</p>

          <table class="override-table">
            <thead>
              <tr>
                <th>根域名</th>
                <th>排除主机头</th>
                <th>操作</th>
              </tr>
            </thead>
            <tbody>
              <tr v-if="overrides.length === 0">
                <td colspan="3" class="empty-cell">暂无配置</td>
              </tr>
              <tr v-for="ov in overrides" :key="ov.id">
                <td><strong>{{ ov.root_domain }}</strong></td>
                <td>
                  <div class="host-tags">
                    <span v-for="h in parseHosts(ov.exclude_hosts)" :key="h" class="host-tag-sm">{{ h }}</span>
                  </div>
                </td>
                <td>
                  <button class="btn-sm btn-blue" @click="editOverride(ov)">编辑</button>
                  <button class="btn-sm btn-red" @click="deleteOverride(ov.id)">删除</button>
                </td>
              </tr>
            </tbody>
          </table>
        </div>
      </div>
    </div>

    <!-- Registrar Modal -->
    <div v-if="showRegistrarModal" class="modal-overlay" @click.self="showRegistrarModal = false">
      <div class="modal-box">
        <div class="modal-header">
          <h3>{{ editingRegistrar?.id ? '编辑注册商' : '添加注册商' }}</h3>
          <button class="modal-close" @click="showRegistrarModal = false">&times;</button>
        </div>
        <div class="modal-body">
          <div class="form-grid">
            <div class="form-group full-width">
              <label>配置名称 *</label>
              <input v-model="regForm.name" type="text" placeholder="如: 主账号-GoDaddy" />
            </div>
            <div class="form-group full-width">
              <label>注册商类型 *</label>
              <select v-model="regForm.provider">
                <option value="">请选择</option>
                <option value="godaddy">GoDaddy</option>
                <option value="namecom">Name.com</option>
              </select>
            </div>

            <!-- GoDaddy fields -->
            <template v-if="regForm.provider === 'godaddy'">
              <div class="form-group">
                <label>API Key *</label>
                <input v-model="regForm.api_key" type="text" placeholder="GoDaddy API Key" />
              </div>
              <div class="form-group">
                <label>API Secret *</label>
                <input v-model="regForm.api_secret" type="password" placeholder="GoDaddy API Secret" />
              </div>
            </template>

            <!-- Name.com fields -->
            <template v-if="regForm.provider === 'namecom'">
              <div class="form-group">
                <label>API User *</label>
                <input v-model="regForm.api_user" type="text" placeholder="Name.com 用户名" />
              </div>
              <div class="form-group">
                <label>API Token *</label>
                <input v-model="regForm.api_token" type="password" placeholder="Name.com API Token" />
              </div>
            </template>

            <div class="form-group full-width">
              <label>自定义API地址（可选）</label>
              <input v-model="regForm.base_url" type="text" placeholder="留空使用默认，如: https://api.ote-godaddy.com" />
            </div>
            <div class="form-group">
              <label>状态</label>
              <select v-model="regForm.status">
                <option value="active">启用</option>
                <option value="inactive">禁用</option>
              </select>
            </div>
          </div>
          <div v-if="regFormError" class="error-msg" style="margin-top:12px">{{ regFormError }}</div>
        </div>
        <div class="modal-footer">
          <button class="btn-cancel" @click="showRegistrarModal = false">取消</button>
          <button class="btn-save" @click="saveRegistrar" :disabled="savingReg">
            {{ savingReg ? '保存中...' : '保存' }}
          </button>
        </div>
      </div>
    </div>

    <!-- Delete Registrar Confirm -->
    <div v-if="showDeleteRegConfirm" class="modal-overlay" @click.self="showDeleteRegConfirm = false">
      <div class="modal-box modal-small">
        <div class="modal-header">
          <h3>确认删除</h3>
          <button class="modal-close" @click="showDeleteRegConfirm = false">&times;</button>
        </div>
        <div class="modal-body">
          <p>确定要删除注册商 <strong>{{ deletingReg?.name }}</strong> 吗？</p>
          <p style="color:#ef4444;margin-top:8px;font-size:13px">注意：删除注册商不会自动删除已同步的域名记录</p>
        </div>
        <div class="modal-footer">
          <button class="btn-cancel" @click="showDeleteRegConfirm = false">取消</button>
          <button class="btn-danger" @click="deleteRegistrar" :disabled="deletingRegLoading">
            {{ deletingRegLoading ? '删除中...' : '确认删除' }}
          </button>
        </div>
      </div>
    </div>

    <!-- Override Modal -->
    <div v-if="showOverrideModal" class="modal-overlay" @click.self="showOverrideModal = false">
      <div class="modal-box">
        <div class="modal-header">
          <h3>{{ editingOverride?.id ? '编辑域名过滤' : '添加域名过滤' }}</h3>
          <button class="modal-close" @click="showOverrideModal = false">&times;</button>
        </div>
        <div class="modal-body">
          <div class="form-group" style="margin-bottom:16px">
            <label>根域名 *</label>
            <input v-model="overrideForm.root_domain" type="text" placeholder="如: example.com" :disabled="!!editingOverride?.id" />
          </div>
          <div class="form-group">
            <label>排除主机头</label>
            <div class="host-list">
              <div v-for="(h, i) in overrideForm.exclude_hosts" :key="i" class="host-item">
                <span class="host-tag">{{ h }}</span>
                <button class="btn-remove" @click="overrideForm.exclude_hosts.splice(i,1)">×</button>
              </div>
            </div>
            <div class="add-host-row" style="margin-top:8px">
              <input v-model="newOverrideHost" type="text" placeholder="输入主机头" class="host-input" @keyup.enter="addOverrideHost" />
              <button class="btn-primary" @click="addOverrideHost">添加</button>
            </div>
          </div>
          <div v-if="overrideFormError" class="error-msg" style="margin-top:12px">{{ overrideFormError }}</div>
        </div>
        <div class="modal-footer">
          <button class="btn-cancel" @click="showOverrideModal = false">取消</button>
          <button class="btn-save" @click="saveOverride" :disabled="savingOverride">
            {{ savingOverride ? '保存中...' : '保存' }}
          </button>
        </div>
      </div>
    </div>

    <!-- Toast -->
    <div v-if="toast.show" class="toast" :class="'toast-' + toast.type">
      {{ toast.message }}
    </div>
  </div>
</template>

<script setup>
import { ref, onMounted } from 'vue'
import api from '../api/index.js'

const tabs = [
  { key: 'registrars', label: '注册商配置' },
  { key: 'filters', label: '全局过滤配置' },
  { key: 'autosync', label: '自动同步配置' },
  { key: 'overrides', label: '域名级过滤' }
]
const activeTab = ref('registrars')

// Registrars
const registrars = ref([])
const testingId = ref(null)
const syncingId = ref(null)
const showRegistrarModal = ref(false)
const editingRegistrar = ref(null)
const regForm = ref({ name: '', provider: '', api_key: '', api_secret: '', api_user: '', api_token: '', base_url: '', status: 'active' })
const regFormError = ref('')
const savingReg = ref(false)
const showDeleteRegConfirm = ref(false)
const deletingReg = ref(null)
const deletingRegLoading = ref(false)

// Filters
const excludeHosts = ref([])
const newHost = ref('')
const savingFilters = ref(false)

// Auto sync
const autoSyncEnabled = ref(false)
const autoSyncTime = ref('02:00')
const savingAutoSync = ref(false)

// Overrides
const overrides = ref([])
const showOverrideModal = ref(false)
const editingOverride = ref(null)
const overrideForm = ref({ root_domain: '', exclude_hosts: [] })
const newOverrideHost = ref('')
const overrideFormError = ref('')
const savingOverride = ref(false)

// Toast
const toast = ref({ show: false, type: 'success', message: '' })
let toastTimer = null


function showToast(type, message) {
  toast.value = { show: true, type, message }
  if (toastTimer) clearTimeout(toastTimer)
  toastTimer = setTimeout(() => { toast.value.show = false }, 3500)
}

// Registrars
async function fetchRegistrars() {
  try {
    const res = await api.get('/registrars')
    if (res.data.success) registrars.value = res.data.data || []
  } catch {}
}

function openAddRegistrar() {
  editingRegistrar.value = null
  regForm.value = { name: '', provider: '', api_key: '', api_secret: '', api_user: '', api_token: '', base_url: '', status: 'active' }
  regFormError.value = ''
  showRegistrarModal.value = true
}

function editRegistrar(reg) {
  editingRegistrar.value = reg
  regForm.value = { ...reg }
  regFormError.value = ''
  showRegistrarModal.value = true
}

async function saveRegistrar() {
  regFormError.value = ''
  if (!regForm.value.name || !regForm.value.provider) {
    regFormError.value = '请填写配置名称和注册商类型'
    return
  }
  savingReg.value = true
  try {
    let res
    if (editingRegistrar.value?.id) {
      res = await api.put(`/registrars/${editingRegistrar.value.id}`, regForm.value)
    } else {
      res = await api.post('/registrars', regForm.value)
    }
    if (res.data.success) {
      showToast('success', editingRegistrar.value?.id ? '更新成功' : '添加成功')
      showRegistrarModal.value = false
      await fetchRegistrars()
    } else {
      regFormError.value = res.data.message || '保存失败'
    }
  } catch (err) {
    regFormError.value = err.response?.data?.message || '保存失败'
  } finally {
    savingReg.value = false
  }
}

async function testRegistrar(reg) {
  testingId.value = reg.id
  try {
    const res = await api.post(`/registrars/${reg.id}/test`)
    showToast(res.data.success ? 'success' : 'error', res.data.message)
  } catch (err) {
    showToast('error', err.response?.data?.message || '测试失败')
  } finally {
    testingId.value = null
  }
}

async function syncRegistrar(reg) {
  syncingId.value = reg.id
  try {
    const res = await api.post(`/registrars/${reg.id}/sync`)
    showToast(res.data.success ? 'success' : 'error', res.data.message)
    if (res.data.success) await fetchRegistrars()
  } catch (err) {
    showToast('error', err.response?.data?.message || '同步失败')
  } finally {
    syncingId.value = null
  }
}

function confirmDeleteRegistrar(reg) {
  deletingReg.value = reg
  showDeleteRegConfirm.value = true
}

async function deleteRegistrar() {
  if (!deletingReg.value) return
  deletingRegLoading.value = true
  try {
    const res = await api.delete(`/registrars/${deletingReg.value.id}`)
    if (res.data.success) {
      showToast('success', '删除成功')
      showDeleteRegConfirm.value = false
      await fetchRegistrars()
    } else {
      showToast('error', res.data.message)
    }
  } catch {
    showToast('error', '删除失败')
  } finally {
    deletingRegLoading.value = false
  }
}

// Filters
async function fetchSettings() {
  try {
    const res = await api.get('/settings')
    if (res.data.success) {
      const s = res.data.data || {}
      if (s.filter_exclude_hosts) {
        try { excludeHosts.value = JSON.parse(s.filter_exclude_hosts) } catch {}
      }
      if (s.auto_sync_enabled !== undefined) {
        autoSyncEnabled.value = s.auto_sync_enabled === 'true'
      }
      if (s.auto_sync_time) {
        autoSyncTime.value = s.auto_sync_time
      }
    }
  } catch {}
}

function addHost() {
  const h = newHost.value.trim()
  if (h && !excludeHosts.value.includes(h)) {
    excludeHosts.value.push(h)
  }
  newHost.value = ''
}

async function saveFilters() {
  savingFilters.value = true
  try {
    const res = await api.post('/settings', {
      settings: {
        filter_exclude_hosts: JSON.stringify(excludeHosts.value)
      }
    })
    showToast(res.data.success ? 'success' : 'error', res.data.success ? '过滤配置已保存' : res.data.message)
  } catch {
    showToast('error', '保存失败')
  } finally {
    savingFilters.value = false
  }
}

// Auto sync
async function saveAutoSync() {
  savingAutoSync.value = true
  try {
    const res = await api.post('/settings', {
      settings: {
        auto_sync_enabled: autoSyncEnabled.value ? 'true' : 'false',
        auto_sync_time: autoSyncTime.value
      }
    })
    showToast(res.data.success ? 'success' : 'error', res.data.success ? '自动同步配置已保存' : res.data.message)
  } catch {
    showToast('error', '保存失败')
  } finally {
    savingAutoSync.value = false
  }
}

// Overrides
async function fetchOverrides() {
  try {
    const res = await api.get('/overrides')
    if (res.data.success) overrides.value = res.data.data || []
  } catch {}
}

function parseHosts(hostsStr) {
  try { return JSON.parse(hostsStr) } catch { return [] }
}

function openAddOverride() {
  editingOverride.value = null
  overrideForm.value = { root_domain: '', exclude_hosts: [] }
  newOverrideHost.value = ''
  overrideFormError.value = ''
  showOverrideModal.value = true
}

function editOverride(ov) {
  editingOverride.value = ov
  overrideForm.value = { root_domain: ov.root_domain, exclude_hosts: parseHosts(ov.exclude_hosts) }
  newOverrideHost.value = ''
  overrideFormError.value = ''
  showOverrideModal.value = true
}

function addOverrideHost() {
  const h = newOverrideHost.value.trim()
  if (h && !overrideForm.value.exclude_hosts.includes(h)) {
    overrideForm.value.exclude_hosts.push(h)
  }
  newOverrideHost.value = ''
}

async function saveOverride() {
  overrideFormError.value = ''
  if (!overrideForm.value.root_domain) {
    overrideFormError.value = '请输入根域名'
    return
  }
  savingOverride.value = true
  try {
    const res = await api.post('/overrides', {
      root_domain: overrideForm.value.root_domain,
      exclude_hosts: overrideForm.value.exclude_hosts
    })
    if (res.data.success) {
      showToast('success', '保存成功')
      showOverrideModal.value = false
      await fetchOverrides()
    } else {
      overrideFormError.value = res.data.message || '保存失败'
    }
  } catch (err) {
    overrideFormError.value = err.response?.data?.message || '保存失败'
  } finally {
    savingOverride.value = false
  }
}

async function deleteOverride(id) {
  if (!confirm('确定要删除该配置吗？')) return
  try {
    const res = await api.delete(`/overrides/${id}`)
    if (res.data.success) {
      showToast('success', '删除成功')
      await fetchOverrides()
    }
  } catch { showToast('error', '删除失败') }
}

function providerLabel(provider) {
  return { godaddy: 'GoDaddy', namecom: 'Name.com' }[provider] || provider
}

onMounted(async () => {
  await Promise.all([fetchRegistrars(), fetchSettings(), fetchOverrides()])
})
</script>

<style scoped>
.page-wrap {
  min-height: 100vh;
}

.main-content {
  padding: 24px 28px;
  max-width: 1200px;
}

.page-header {
  margin-bottom: 20px;
}

.page-header h2 {
  font-size: 20px;
  font-weight: 700;
  color: #1e293b;
}

/* Tabs */
.tabs {
  display: flex;
  gap: 4px;
  background: white;
  border-radius: 10px;
  padding: 6px;
  margin-bottom: 20px;
  box-shadow: 0 1px 4px rgba(0,0,0,0.08);
  width: fit-content;
}

.tab-btn {
  padding: 8px 20px;
  background: transparent;
  border: none;
  border-radius: 7px;
  font-size: 13px;
  color: #64748b;
  cursor: pointer;
  font-weight: 500;
  transition: all 0.2s;
}

.tab-btn.active {
  background: #1e40af;
  color: white;
}

.tab-btn:not(.active):hover {
  background: #f1f5f9;
  color: #334155;
}

/* Tab content */
.tab-content { animation: fadeIn 0.2s ease; }

@keyframes fadeIn {
  from { opacity: 0; transform: translateY(4px); }
  to { opacity: 1; transform: translateY(0); }
}

.section-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-bottom: 16px;
}

.section-header h3 {
  font-size: 16px;
  font-weight: 600;
  color: #1e293b;
}

/* Registrar cards */
.registrar-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(340px, 1fr));
  gap: 16px;
}

.registrar-card {
  background: white;
  border-radius: 10px;
  padding: 20px;
  box-shadow: 0 1px 4px rgba(0,0,0,0.08);
  border: 1px solid #e2e8f0;
  transition: box-shadow 0.2s;
}

.registrar-card:hover {
  box-shadow: 0 4px 12px rgba(0,0,0,0.12);
}

.reg-card-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-bottom: 12px;
}

.reg-info {
  display: flex;
  align-items: center;
  gap: 10px;
}

.reg-name {
  font-size: 15px;
  font-weight: 600;
  color: #1e293b;
}

.status-dot {
  width: 10px;
  height: 10px;
  border-radius: 50%;
  flex-shrink: 0;
}

.dot-active { background: #10b981; }
.dot-inactive { background: #94a3b8; }

.reg-card-body {
  margin-bottom: 14px;
}

.reg-detail {
  display: flex;
  gap: 8px;
  font-size: 13px;
  margin-bottom: 4px;
}

.detail-label { color: #94a3b8; }
.detail-value { color: #334155; }
.muted { color: #cbd5e1 !important; }

.reg-card-actions {
  display: flex;
  gap: 8px;
  flex-wrap: wrap;
}

/* Buttons */
.btn-primary {
  background: #1e40af;
  color: white;
  border: none;
  padding: 9px 18px;
  border-radius: 7px;
  font-size: 13px;
  font-weight: 600;
  cursor: pointer;
  transition: opacity 0.2s;
}

.btn-primary:hover:not(:disabled) { opacity: 0.88; }
.btn-primary:disabled { opacity: 0.5; cursor: not-allowed; }

.btn-sm {
  padding: 5px 12px;
  border-radius: 5px;
  font-size: 12px;
  font-weight: 500;
  cursor: pointer;
  transition: opacity 0.2s;
}

.btn-outline {
  background: transparent;
  border: 1px solid #cbd5e1;
  color: #475569;
}

.btn-outline:hover:not(:disabled) { background: #f1f5f9; }
.btn-outline:disabled { opacity: 0.5; cursor: not-allowed; }

.btn-blue {
  background: #eff6ff;
  border: 1px solid #bfdbfe;
  color: #1d4ed8;
}

.btn-blue:hover { background: #dbeafe; }

.btn-red {
  background: #fef2f2;
  border: 1px solid #fecaca;
  color: #dc2626;
}

.btn-red:hover { background: #fee2e2; }

/* Provider badges */
.provider-badge {
  display: inline-block;
  padding: 2px 8px;
  border-radius: 4px;
  font-size: 11px;
  font-weight: 600;
}

.provider-godaddy { background: #fef3c7; color: #92400e; }
.provider-namecom { background: #e0e7ff; color: #3730a3; }

/* Setting cards */
.setting-card {
  background: white;
  border-radius: 10px;
  padding: 24px;
  box-shadow: 0 1px 4px rgba(0,0,0,0.08);
}

.setting-desc {
  color: #64748b;
  font-size: 13px;
  margin-bottom: 20px;
  padding: 12px;
  background: #f0f7ff;
  border-radius: 7px;
  border-left: 3px solid #3b82f6;
}

.host-list {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  min-height: 40px;
  padding: 12px;
  background: #f8fafc;
  border-radius: 8px;
  border: 1px solid #e2e8f0;
  margin-bottom: 12px;
}

.host-item {
  display: flex;
  align-items: center;
  gap: 4px;
  background: white;
  border: 1px solid #e2e8f0;
  border-radius: 4px;
  padding: 2px 4px 2px 10px;
}

.host-tag {
  font-size: 13px;
  color: #334155;
  font-family: monospace;
}

.host-tag-sm {
  background: #f1f5f9;
  border: 1px solid #e2e8f0;
  border-radius: 4px;
  padding: 2px 8px;
  font-size: 12px;
  font-family: monospace;
  color: #475569;
}

.host-empty {
  font-size: 13px;
  color: #94a3b8;
  align-self: center;
}

.btn-remove {
  background: transparent;
  border: none;
  color: #94a3b8;
  cursor: pointer;
  font-size: 16px;
  line-height: 1;
  padding: 0 2px;
  transition: color 0.2s;
}

.btn-remove:hover { color: #ef4444; }

.add-host-row {
  display: flex;
  gap: 10px;
  align-items: center;
  margin-bottom: 16px;
}

.host-input {
  flex: 1;
  padding: 9px 12px;
  border: 1.5px solid #e2e8f0;
  border-radius: 7px;
  font-size: 13px;
  outline: none;
}

.host-input:focus { border-color: #3b82f6; }

.save-row {
  margin-top: 20px;
  border-top: 1px solid #f1f5f9;
  padding-top: 16px;
}

/* Settings rows */
.setting-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 16px 0;
  border-bottom: 1px solid #f1f5f9;
  gap: 16px;
}

.setting-row.disabled { opacity: 0.5; }

.setting-label {
  display: flex;
  flex-direction: column;
  gap: 4px;
}

.label-main {
  font-size: 14px;
  font-weight: 500;
  color: #1e293b;
}

.label-desc {
  font-size: 12px;
  color: #94a3b8;
}

/* Toggle */
.toggle-wrap {
  display: flex;
  align-items: center;
  gap: 10px;
}

.toggle {
  position: relative;
  display: inline-block;
  width: 44px;
  height: 24px;
  flex-shrink: 0;
}

.toggle input { display: none; }

.toggle-slider {
  position: absolute;
  inset: 0;
  background: #cbd5e1;
  border-radius: 12px;
  cursor: pointer;
  transition: background 0.2s;
}

.toggle-slider::before {
  content: '';
  position: absolute;
  width: 18px;
  height: 18px;
  background: white;
  border-radius: 50%;
  left: 3px;
  top: 3px;
  transition: transform 0.2s;
  box-shadow: 0 1px 3px rgba(0,0,0,0.2);
}

.toggle input:checked + .toggle-slider { background: #3b82f6; }
.toggle input:checked + .toggle-slider::before { transform: translateX(20px); }

.toggle-text {
  font-size: 13px;
  color: #64748b;
  font-weight: 500;
  min-width: 40px;
}

.time-input {
  padding: 9px 12px;
  border: 1.5px solid #e2e8f0;
  border-radius: 7px;
  font-size: 14px;
  outline: none;
}

.time-input:focus { border-color: #3b82f6; }
.time-input:disabled { background: #f8fafc; cursor: not-allowed; }

.sync-hint {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 12px;
  background: #eff6ff;
  border-radius: 7px;
  font-size: 13px;
  color: #1e40af;
  margin-top: 16px;
}

/* Override table */
.override-table {
  width: 100%;
  border-collapse: collapse;
  font-size: 13px;
  margin-top: 16px;
}

.override-table th {
  background: #f8fafc;
  padding: 10px 12px;
  text-align: left;
  font-weight: 600;
  color: #475569;
  border-bottom: 2px solid #e2e8f0;
}

.override-table td {
  padding: 10px 12px;
  border-bottom: 1px solid #f1f5f9;
  vertical-align: middle;
}

.host-tags {
  display: flex;
  flex-wrap: wrap;
  gap: 4px;
}

.empty-cell {
  text-align: center;
  padding: 32px;
  color: #94a3b8;
}

.empty-state {
  background: white;
  border-radius: 10px;
  padding: 48px;
  text-align: center;
  color: #94a3b8;
  box-shadow: 0 1px 4px rgba(0,0,0,0.08);
}

/* Modal */
.modal-overlay {
  position: fixed;
  inset: 0;
  background: rgba(0,0,0,0.45);
  display: flex;
  align-items: center;
  justify-content: center;
  z-index: 1000;
  padding: 20px;
}

.modal-box {
  background: white;
  border-radius: 12px;
  width: 100%;
  max-width: 560px;
  box-shadow: 0 20px 50px rgba(0,0,0,0.25);
  max-height: 90vh;
  overflow-y: auto;
}

.modal-small { max-width: 400px; }

.modal-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 20px 24px;
  border-bottom: 1px solid #e2e8f0;
}

.modal-header h3 {
  font-size: 16px;
  font-weight: 600;
  color: #1e293b;
}

.modal-close {
  background: transparent;
  border: none;
  font-size: 22px;
  color: #94a3b8;
  cursor: pointer;
  padding: 0 4px;
}

.modal-close:hover { color: #475569; }

.modal-body { padding: 20px 24px; }

.form-grid {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 14px;
}

.form-group {
  display: flex;
  flex-direction: column;
  gap: 5px;
}

.form-group.full-width { grid-column: 1 / -1; }

.form-group label {
  font-size: 12px;
  font-weight: 500;
  color: #64748b;
  text-transform: uppercase;
  letter-spacing: 0.3px;
}

.form-group input,
.form-group select,
.form-group textarea {
  padding: 9px 12px;
  border: 1.5px solid #e2e8f0;
  border-radius: 7px;
  font-size: 13px;
  outline: none;
  font-family: inherit;
  transition: border-color 0.2s;
}

.form-group input:focus,
.form-group select:focus,
.form-group textarea:focus {
  border-color: #3b82f6;
  box-shadow: 0 0 0 3px rgba(59,130,246,0.1);
}

.form-group input:disabled {
  background: #f8fafc;
  color: #94a3b8;
  cursor: not-allowed;
}

.error-msg {
  background: #fef2f2;
  border: 1px solid #fecaca;
  color: #dc2626;
  padding: 10px 14px;
  border-radius: 8px;
  font-size: 13px;
}

.modal-footer {
  display: flex;
  justify-content: flex-end;
  gap: 10px;
  padding: 16px 24px;
  border-top: 1px solid #e2e8f0;
}

.btn-cancel {
  background: #f1f5f9;
  border: 1.5px solid #e2e8f0;
  color: #475569;
  padding: 9px 20px;
  border-radius: 7px;
  font-size: 13px;
  cursor: pointer;
  transition: all 0.2s;
}

.btn-cancel:hover { background: #e2e8f0; }

.btn-save {
  background: #1e40af;
  border: none;
  color: white;
  padding: 9px 24px;
  border-radius: 7px;
  font-size: 13px;
  font-weight: 600;
  cursor: pointer;
  transition: opacity 0.2s;
}

.btn-save:hover:not(:disabled) { opacity: 0.88; }
.btn-save:disabled { opacity: 0.5; cursor: not-allowed; }

.btn-danger {
  background: #ef4444;
  border: none;
  color: white;
  padding: 9px 24px;
  border-radius: 7px;
  font-size: 13px;
  font-weight: 600;
  cursor: pointer;
  transition: opacity 0.2s;
}

.btn-danger:hover:not(:disabled) { opacity: 0.88; }
.btn-danger:disabled { opacity: 0.5; cursor: not-allowed; }

/* Toast */
.toast {
  position: fixed;
  bottom: 32px;
  right: 32px;
  padding: 14px 24px;
  border-radius: 10px;
  font-size: 14px;
  font-weight: 500;
  box-shadow: 0 8px 24px rgba(0,0,0,0.18);
  z-index: 2000;
  animation: slideIn 0.3s ease;
}

.toast-success { background: #10b981; color: white; }
.toast-error { background: #ef4444; color: white; }

@keyframes slideIn {
  from { transform: translateX(100px); opacity: 0; }
  to { transform: translateX(0); opacity: 1; }
}

@media (max-width: 640px) {
  .form-grid { grid-template-columns: 1fr; }
  .tabs { flex-wrap: wrap; }
}
</style>
