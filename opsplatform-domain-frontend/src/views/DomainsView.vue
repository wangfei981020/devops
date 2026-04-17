<template>
  <div class="page-wrap">
    <div class="main-content">
      <!-- 页面标题 + 操作按钮 -->
      <div class="page-header">
        <h2 class="page-title-inner">域名列表</h2>
        <div class="header-actions">
          <button class="btn-action" @click="fetchDomains">
            <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><polyline points="23 4 23 10 17 10"/><path d="M20.49 15a9 9 0 11-2.12-9.36L23 10"/></svg>
            刷新
          </button>
          <button class="btn-action" @click="syncAllRegistrars" :disabled="syncingAll">
            <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M21 2v6h-6"/><path d="M3 12a9 9 0 0115-6.7L21 8"/><path d="M3 22v-6h6"/><path d="M21 12a9 9 0 01-15 6.7L3 16"/></svg>
            {{ syncingAll ? '同步中...' : '同步域名到期' }}
          </button>
          <button class="btn-action" @click="batchCheckCerts" :disabled="batchCheckingCert">
            <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><rect x="3" y="11" width="18" height="11" rx="2" ry="2"/><path d="M7 11V7a5 5 0 0110 0v4"/></svg>
            {{ batchCheckingCert ? `检测中 ${batchCertProgress}/${batchCertTotal}...` : '刷新证书到期' }}
          </button>
          <button class="btn-primary-action" @click="openAddModal">
            <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5"><line x1="12" y1="5" x2="12" y2="19"/><line x1="5" y1="12" x2="19" y2="12"/></svg>
            添加
          </button>
        </div>
      </div>

      <!-- 统计卡片 -->
      <div class="summary-cards">
        <div class="card card-total"><div class="card-label">域名总数</div><div class="card-value">{{ totalCount }}</div></div>
        <div class="card card-active"><div class="card-label">启用域名</div><div class="card-value">{{ activeCount }}</div></div>
        <div class="card card-warn"><div class="card-label">30天内到期</div><div class="card-value">{{ expiring30Count }}</div></div>
        <div class="card card-cert"><div class="card-label">证书30天内到期</div><div class="card-value">{{ certExpiring30Count }}</div></div>
      </div>

      <!-- 注册商同步栏 -->
      <div class="sync-bar" v-if="registrars.length > 0">
        <span class="sync-label">注册商同步:</span>
        <div class="sync-items">
          <div v-for="reg in registrars" :key="reg.id" class="sync-item">
            <span class="sync-name">{{ reg.name }}</span>
            <span class="sync-time" v-if="reg.last_sync">{{ reg.last_sync }}</span>
            <button class="btn-sync" @click="syncConfig(reg)" :disabled="syncingId === reg.id">
              {{ syncingId === reg.id ? '同步中...' : '同步' }}
            </button>
          </div>
        </div>
      </div>

      <!-- 过滤栏 -->
      <div class="filter-bar">
        <input v-model="filters.search" type="text" placeholder="搜索域名、项目、模块..." class="filter-input filter-search" @input="debouncedFetch" />
        <select v-model="filters.project" @change="fetchDomains" class="filter-select">
          <option value="">全部项目</option>
          <option v-for="p in options.projects" :key="p" :value="p">{{ p }}</option>
        </select>
        <select v-model="filters.env" @change="fetchDomains" class="filter-select">
          <option value="">全部环境</option>
          <option v-for="e in options.envs" :key="e" :value="e">{{ e }}</option>
        </select>
        <select v-model="filters.module" @change="fetchDomains" class="filter-select">
          <option value="">全部模块</option>
          <option v-for="m in options.modules" :key="m" :value="m">{{ m }}</option>
        </select>
        <select v-model="filters.cdn_provider" @change="fetchDomains" class="filter-select">
          <option value="">全部CDN</option>
          <option v-for="c in options.cdn_providers" :key="c" :value="c">{{ c }}</option>
        </select>
        <select v-model="filters.config_id" @change="fetchDomains" class="filter-select">
          <option value="">全部注册商</option>
          <option v-for="r in registrars" :key="r.id" :value="r.id">{{ r.name }}</option>
        </select>
        <select v-model="filters.status" @change="fetchDomains" class="filter-select">
          <option value="">全部状态</option>
          <option value="active">启用</option>
          <option value="standby">备用</option>
          <option value="inactive">禁用</option>
        </select>
        <button class="btn-reset" @click="resetFilters">重置</button>
      </div>

      <!-- 表格 -->
      <div class="table-container">
        <table class="domain-table">
          <thead>
            <tr>
              <th>项目</th>
              <th>环境</th>
              <th>模块</th>
              <th>全域名</th>
              <th>主机头</th>
              <th>根域名</th>
              <th>类型</th>
              <th>解析值</th>
              <th>回源</th>
              <th>源站IP</th>
              <th>CDN厂商</th>
              <th>域名到期</th>
              <th>证书到期</th>
              <th>状态</th>
              <th>来源</th>
              <th>备注</th>
              <th>创建时间</th>
              <th>更新时间</th>
              <th>操作</th>
            </tr>
          </thead>
          <tbody>
            <tr v-if="loading"><td colspan="19" class="empty-cell">加载中...</td></tr>
            <tr v-else-if="pagedDomains.length === 0"><td colspan="19" class="empty-cell">暂无数据</td></tr>
            <tr v-for="d in pagedDomains" :key="d.id" class="domain-row">
              <td>{{ d.project || '-' }}</td>
              <td><span v-if="d.env" class="env-badge" :class="'env-'+d.env.toLowerCase()">{{ d.env }}</span><span v-else>-</span></td>
              <td>{{ d.module || '-' }}</td>
              <td class="td-full-domain"><span class="full-domain-text">{{ d.full_domain }}</span></td>
              <td>{{ d.host || '@' }}</td>
              <td>{{ d.root_domain || '-' }}</td>
              <td><span class="badge" :class="recordTypeBadge(d.record_type)">{{ d.record_type || '-' }}</span></td>
              <td class="td-value"><span class="value-truncate" :title="d.record_value">{{ d.record_value || '-' }}</span></td>
              <td>{{ d.origin || '-' }}</td>
              <td>{{ d.origin_ip || '-' }}</td>
              <td>{{ d.cdn_provider || '-' }}</td>
              <td @click.stop>
                <span :class="dateColorClass(d.expire_time)">{{ d.expire_time || '-' }}</span>
                <button class="btn-check-cert" @click.stop="checkExpiry(d)" :disabled="checkingExpiryId === d.id" title="WHOIS查询域名到期">{{ checkingExpiryId === d.id ? '...' : '检测' }}</button>
              </td>
              <td @click.stop>
                <span :class="dateColorClass(d.cert_expire_time)">{{ d.cert_expire_time || '-' }}</span>
                <button class="btn-check-cert" @click.stop="checkCert(d)" :disabled="checkingCertId === d.id" title="检测证书">{{ checkingCertId === d.id ? '...' : '检测' }}</button>
              </td>
              <td @click.stop>
                <span class="status-badge" :class="statusBadgeClass(d.status)" @click.stop="cycleStatus(d)" style="cursor:pointer" :title="'点击切换状态'">{{ statusLabel(d.status) }}</span>
              </td>
              <td><span class="source-badge" :class="'source-'+d.source">{{ sourceLabel(d.source) }}</span></td>
              <td class="td-remark" :title="d.remark">{{ d.remark || '-' }}</td>
              <td class="td-time">{{ d.created_at || '-' }}</td>
              <td class="td-time">{{ d.updated_at || '-' }}</td>
              <td @click.stop style="white-space:nowrap">
                <button class="btn-edit" @click.stop="openEditModal(d)">编辑</button>
                <button class="btn-delete" @click.stop="confirmDelete(d)">删除</button>
              </td>
            </tr>
          </tbody>
        </table>
      </div>

      <!-- 分页 -->
      <div class="pagination" v-if="filteredDomains.length > 0">
        <span class="page-info">共 {{ filteredDomains.length }} 条</span>
        <div class="page-buttons">
          <button class="page-btn" @click="currentPage=1" :disabled="currentPage===1">首页</button>
          <button class="page-btn" @click="currentPage--" :disabled="currentPage===1">上一页</button>
          <span class="page-current">{{ currentPage }} / {{ totalPages }}</span>
          <button class="page-btn" @click="currentPage++" :disabled="currentPage===totalPages">下一页</button>
          <button class="page-btn" @click="currentPage=totalPages" :disabled="currentPage===totalPages">末页</button>
          <select class="page-size-select" v-model="pageSize" @change="currentPage=1">
            <option :value="10">10条/页</option>
            <option :value="20">20条/页</option>
            <option :value="50">50条/页</option>
            <option :value="100">100条/页</option>
          </select>
        </div>
      </div>
    </div>

    <!-- 添加/编辑 弹窗 -->
    <div v-if="showEditModal" class="modal-overlay">
      <div class="modal-box">
        <div class="modal-header">
          <h3>{{ isAdding ? '添加域名' : '编辑域名信息' }}</h3>
          <button class="modal-close" @click="showEditModal=false">&times;</button>
        </div>
        <div class="modal-body">
          <div v-if="!isAdding" class="modal-domain-info">
            <strong>{{ editForm.full_domain }}</strong>
            <span class="badge" :class="recordTypeBadge(editForm.record_type)" style="margin-left:8px">{{ editForm.record_type }}</span>
          </div>
          <div class="form-grid">
            <div class="form-group" v-if="isAdding || editForm.source === 'manual'">
              <label>全域名 *</label>
              <input v-model="editForm.full_domain" type="text" placeholder="如: www.example.com" />
            </div>
            <div class="form-group" v-if="isAdding || editForm.source === 'manual'">
              <label>根域名</label>
              <input v-model="editForm.root_domain" type="text" placeholder="如: example.com" />
            </div>
            <div class="form-group" v-if="isAdding || editForm.source === 'manual'">
              <label>主机头</label>
              <input v-model="editForm.host" type="text" placeholder="如: www 或 @" />
            </div>
            <div class="form-group" v-if="isAdding || editForm.source === 'manual'">
              <label>记录类型</label>
              <select v-model="editForm.record_type">
                <option value="A">A</option>
                <option value="CNAME">CNAME</option>
                <option value="AAAA">AAAA</option>
              </select>
            </div>
            <div class="form-group">
              <label>项目</label>
              <input v-model="editForm.project" type="text" placeholder="如: 官网、后台" />
            </div>
            <div class="form-group">
              <label>环境</label>
              <input v-model="editForm.env" type="text" placeholder="如: PROD、UAT、DEV" />
            </div>
            <div class="form-group">
              <label>模块名</label>
              <input v-model="editForm.module" type="text" placeholder="如: 前端、API" />
            </div>
            <div class="form-group">
              <label>CDN厂商</label>
              <input v-model="editForm.cdn_provider" type="text" placeholder="如: Cloudflare、阿里云" />
            </div>
            <div class="form-group">
              <label>回源地址</label>
              <input v-model="editForm.origin" type="text" placeholder="如: origin.example.com" />
            </div>
            <div class="form-group">
              <label>源站IP</label>
              <input v-model="editForm.origin_ip" type="text" placeholder="如: 1.2.3.4" />
            </div>
            <div class="form-group">
              <label>域名到期时间</label>
              <div class="input-with-btn">
                <input v-model="editForm.expire_time" type="date" />
                <button class="btn-query" type="button" @click="queryExpiry" :disabled="checkingExpiry">{{ checkingExpiry ? '查询中...' : '自动获取' }}</button>
              </div>
            </div>
            <div class="form-group">
              <label>证书到期时间</label>
              <input v-model="editForm.cert_expire_time" type="date" />
            </div>
            <div class="form-group">
              <label>状态</label>
              <select v-model="editForm.status">
                <option value="active">启用</option>
                <option value="standby">备用</option>
                <option value="inactive">禁用</option>
              </select>
            </div>
          </div>
          <div class="form-group" style="margin-top:12px">
            <label>备注</label>
            <textarea v-model="editForm.remark" rows="2" placeholder="备注信息..."></textarea>
          </div>
          <div v-if="editError" class="error-msg">{{ editError }}</div>
        </div>
        <div class="modal-footer">
          <button class="btn-cancel" @click="showEditModal=false">取消</button>
          <button class="btn-save" @click="saveDomain" :disabled="saving">{{ saving ? '保存中...' : '保存' }}</button>
        </div>
      </div>
    </div>

    <!-- 删除确认 -->
    <div v-if="showDeleteConfirm" class="modal-overlay">
      <div class="modal-box modal-small">
        <div class="modal-header">
          <h3>确认删除</h3>
          <button class="modal-close" @click="showDeleteConfirm=false">&times;</button>
        </div>
        <div class="modal-body">
          <p>确定要删除域名 <strong>{{ deletingDomain?.full_domain }}</strong> 吗？</p>
          <p style="color:#ef4444;margin-top:8px;font-size:13px">此操作不可恢复</p>
        </div>
        <div class="modal-footer">
          <button class="btn-cancel" @click="showDeleteConfirm=false">取消</button>
          <button class="btn-danger" @click="deleteDomain" :disabled="deleting">{{ deleting ? '删除中...' : '确认删除' }}</button>
        </div>
      </div>
    </div>

    <!-- Toast -->
    <div v-if="toast.show" class="toast" :class="'toast-'+toast.type">{{ toast.message }}</div>
  </div>
</template>

<script setup>
import { ref, computed, onMounted, watch } from 'vue'
import api from '../api/index.js'

const domains = ref([])
const registrars = ref([])
const loading = ref(false)
const syncingId = ref(null)
const syncingAll = ref(false)
const checkingCertId = ref(null)
const checkingExpiryId = ref(null)
const batchCheckingCert = ref(false)
const batchCertProgress = ref(0)
const batchCertTotal = ref(0)
const currentPage = ref(1)
const pageSize = ref(10)

const options = ref({ projects: [], envs: [], modules: [], cdn_providers: [] })

const filters = ref({ search: '', project: '', env: '', module: '', cdn_provider: '', config_id: '', status: '' })

const showEditModal = ref(false)
const isAdding = ref(false)
const editForm = ref({})
const editError = ref('')
const saving = ref(false)
const checkingExpiry = ref(false)

const showDeleteConfirm = ref(false)
const deletingDomain = ref(null)
const deleting = ref(false)

const toast = ref({ show: false, type: 'success', message: '' })

const filteredDomains = computed(() => domains.value)
const totalPages = computed(() => Math.max(1, Math.ceil(filteredDomains.value.length / pageSize.value)))
const pagedDomains = computed(() => {
  const start = (currentPage.value - 1) * pageSize.value
  return filteredDomains.value.slice(start, start + pageSize.value)
})
const totalCount = computed(() => domains.value.length)
const activeCount = computed(() => domains.value.filter(d => d.status === 'active').length)
const expiring30Count = computed(() => {
  const now = new Date(), limit = new Date(now.getTime() + 30*86400000)
  return domains.value.filter(d => { if (!d.expire_time) return false; const e=new Date(d.expire_time); return e<=limit&&e>=now }).length
})
const certExpiring30Count = computed(() => {
  const now = new Date(), limit = new Date(now.getTime() + 30*86400000)
  return domains.value.filter(d => { if (!d.cert_expire_time) return false; const e=new Date(d.cert_expire_time); return e<=limit&&e>=now }).length
})

async function fetchRegistrars() {
  try {
    const res = await api.get('/registrars')
    if (res.data.success) registrars.value = res.data.data || []
  } catch {}
}

async function fetchOptions() {
  try {
    const res = await api.get('/domains/options')
    if (res.data.success) options.value = res.data
  } catch {}
}

async function fetchDomains() {
  loading.value = true
  currentPage.value = 1
  try {
    const params = {}
    if (filters.value.search) params.search = filters.value.search
    if (filters.value.project) params.project = filters.value.project
    if (filters.value.env) params.env = filters.value.env
    if (filters.value.module) params.module = filters.value.module
    if (filters.value.cdn_provider) params.cdn_provider = filters.value.cdn_provider
    if (filters.value.config_id) params.config_id = filters.value.config_id
    if (filters.value.status) params.status = filters.value.status
    const res = await api.get('/domains', { params })
    if (res.data.success) domains.value = res.data.data || []
  } catch { showToast('error', '获取域名列表失败') }
  finally { loading.value = false }
}

function resetFilters() {
  filters.value = { search: '', project: '', env: '', module: '', cdn_provider: '', config_id: '', status: '' }
  fetchDomains()
}

let searchTimer = null
function debouncedFetch() {
  if (searchTimer) clearTimeout(searchTimer)
  searchTimer = setTimeout(fetchDomains, 400)
}

async function syncAllRegistrars() {
  if (registrars.value.length === 0) { showToast('error', '没有可用的注册商配置'); return }
  syncingAll.value = true
  let ok = 0, fail = 0
  for (const reg of registrars.value) {
    try {
      const res = await api.post(`/registrars/${reg.id}/sync`)
      if (res.data.success) { ok++ } else { fail++ }
    } catch { fail++ }
  }
  syncingAll.value = false
  showToast(fail === 0 ? 'success' : 'error', `同步完成: ${ok} 成功, ${fail} 失败`)
  await fetchRegistrars()
  await fetchDomains()
  await fetchOptions()
}

async function batchCheckCerts() {
  const list = domains.value.filter(d => d.full_domain)
  if (list.length === 0) { showToast('error', '没有可检测的域名'); return }
  batchCheckingCert.value = true
  batchCertProgress.value = 0
  batchCertTotal.value = list.length
  let updated = 0
  const CONCURRENCY = 5
  const queue = [...list]
  async function worker() {
    while (queue.length > 0) {
      const d = queue.shift()
      try {
        const res = await api.get('/domains/check-cert', { params: { domain: d.full_domain } })
        if (res.data.success) {
          await api.put(`/domains/${d.id}`, { ...d, cert_expire_time: res.data.cert_expire_time })
          d.cert_expire_time = res.data.cert_expire_time
          updated++
        }
      } catch {}
      batchCertProgress.value++
    }
  }
  await Promise.all(Array.from({ length: CONCURRENCY }, worker))
  batchCheckingCert.value = false
  showToast('success', `证书检测完成: ${updated}/${list.length} 条已更新`)
}

async function syncConfig(reg) {
  syncingId.value = reg.id
  try {
    const res = await api.post(`/registrars/${reg.id}/sync`)
    if (res.data.success) {
      showToast('success', res.data.message || '同步成功')
      await fetchRegistrars()
      await fetchDomains()
      await fetchOptions()
    } else showToast('error', res.data.message || '同步失败')
  } catch (err) { showToast('error', err.response?.data?.message || '同步失败') }
  finally { syncingId.value = null }
}

async function checkExpiry(domain) {
  checkingExpiryId.value = domain.id
  try {
    const res = await api.get('/domains/check-expiry', { params: { domain: domain.full_domain } })
    if (res.data.success) {
      await api.put(`/domains/${domain.id}`, { ...domain, expire_time: res.data.expire_time })
      domain.expire_time = res.data.expire_time
      showToast('success', res.data.message)
    } else showToast('error', res.data.message)
  } catch { showToast('error', '域名到期查询失败') }
  finally { checkingExpiryId.value = null }
}

async function checkCert(domain) {
  checkingCertId.value = domain.id
  try {
    const res = await api.get('/domains/check-cert', { params: { domain: domain.full_domain } })
    if (res.data.success) {
      await api.put(`/domains/${domain.id}`, { ...domain, cert_expire_time: res.data.cert_expire_time })
      domain.cert_expire_time = res.data.cert_expire_time
      showToast('success', res.data.message)
    } else showToast('error', res.data.message)
  } catch { showToast('error', '证书检测失败') }
  finally { checkingCertId.value = null }
}

async function cycleStatus(domain) {
  const cycle = { active: 'standby', standby: 'inactive', inactive: 'active' }
  const newStatus = cycle[domain.status] || 'active'
  try {
    const res = await api.put(`/domains/${domain.id}`, { ...domain, status: newStatus })
    if (res.data.success) domain.status = newStatus
  } catch {}
}

function openAddModal() {
  isAdding.value = true
  editForm.value = { full_domain: '', root_domain: '', host: '', record_type: 'A', project: '', env: 'PROD', module: '', origin: '', origin_ip: '', cdn_provider: '', expire_time: '', cert_expire_time: '', status: 'active', remark: '' }
  editError.value = ''
  showEditModal.value = true
}

function openEditModal(domain) {
  isAdding.value = false
  editForm.value = { ...domain }
  editError.value = ''
  showEditModal.value = true
}

async function queryExpiry() {
  const domain = editForm.value.full_domain || editForm.value.root_domain
  if (!domain) { editError.value = '请先填写域名'; return }
  checkingExpiry.value = true
  editError.value = ''
  try {
    const res = await api.get('/domains/check-expiry', { params: { domain } })
    if (res.data.success) {
      editForm.value.expire_time = res.data.expire_time
      showToast('success', res.data.message)
    } else {
      editError.value = res.data.message || 'WHOIS查询失败'
    }
  } catch { editError.value = 'WHOIS查询失败' }
  finally { checkingExpiry.value = false }
}

async function saveDomain() {
  editError.value = ''
  saving.value = true
  try {
    let res
    if (isAdding.value) {
      if (!editForm.value.full_domain) { editError.value = '域名不能为空'; saving.value = false; return }
      res = await api.post('/domains', editForm.value)
    } else {
      res = await api.put(`/domains/${editForm.value.id}`, editForm.value)
    }
    if (res.data.success) {
      showToast('success', isAdding.value ? '添加成功' : '保存成功')
      showEditModal.value = false
      await fetchDomains()
      await fetchOptions()
    } else editError.value = res.data.message || '操作失败'
  } catch (err) { editError.value = err.response?.data?.message || '操作失败' }
  finally { saving.value = false }
}

function confirmDelete(domain) { deletingDomain.value = domain; showDeleteConfirm.value = true }

async function deleteDomain() {
  if (!deletingDomain.value) return
  deleting.value = true
  try {
    const res = await api.delete(`/domains/${deletingDomain.value.id}`)
    if (res.data.success) {
      domains.value = domains.value.filter(d => d.id !== deletingDomain.value.id)
      showToast('success', '删除成功')
      showDeleteConfirm.value = false
      await fetchOptions()
    } else showToast('error', res.data.message || '删除失败')
  } catch { showToast('error', '删除失败') }
  finally { deleting.value = false }
}

function statusLabel(s) {
  return { active: '启用', standby: '备用', inactive: '禁用' }[s] || s || '-'
}
function statusBadgeClass(s) {
  return { active: 'status-active', standby: 'status-standby', inactive: 'status-inactive' }[s] || 'status-inactive'
}
function recordTypeBadge(type) {
  return { A: 'badge-blue', AAAA: 'badge-purple', CNAME: 'badge-green' }[type] || 'badge-gray'
}
function dateColorClass(dateStr) {
  if (!dateStr) return 'date-normal'
  const diff = (new Date(dateStr) - new Date()) / 86400000
  if (diff < 30) return 'date-danger'
  if (diff < 90) return 'date-warn'
  return 'date-normal'
}
function sourceLabel(s) {
  return { manual: '手动', godaddy: 'GoDaddy', namecom: 'Name.com' }[s] || s || '-'
}

let toastTimer = null
function showToast(type, message) {
  toast.value = { show: true, type, message }
  if (toastTimer) clearTimeout(toastTimer)
  toastTimer = setTimeout(() => { toast.value.show = false }, 3500)
}

watch(() => [filters.value.project, filters.value.env, filters.value.status, filters.value.config_id], () => { currentPage.value = 1 })

onMounted(async () => {
  await Promise.all([fetchRegistrars(), fetchOptions()])
  await fetchDomains()
})
</script>

<style scoped>
.page-wrap { min-height: 100vh; overflow-x: hidden; }
.main-content { padding: 24px 28px; min-width: 0; }

/* 页面标题 */
.page-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-bottom: 20px;
}
.page-title-inner { font-size: 18px; font-weight: 700; color: #1e293b; }
.header-actions { display: flex; gap: 10px; }

.btn-action {
  display: flex; align-items: center; gap: 6px;
  background: #f1f5f9; border: 1.5px solid #e2e8f0; color: #475569;
  padding: 8px 16px; border-radius: 7px; font-size: 13px; cursor: pointer; transition: all 0.2s;
}
.btn-action:hover { background: #e2e8f0; }

.btn-primary-action {
  display: flex; align-items: center; gap: 6px;
  background: #3b82f6; border: none; color: white;
  padding: 8px 18px; border-radius: 7px; font-size: 13px; font-weight: 600; cursor: pointer; transition: opacity 0.2s;
}
.btn-primary-action:hover { opacity: 0.88; }

/* 统计卡片 */
.summary-cards { display: grid; grid-template-columns: repeat(4,1fr); gap: 16px; margin-bottom: 20px; }
.card { background: white; border-radius: 10px; padding: 18px 20px; box-shadow: 0 1px 4px rgba(0,0,0,0.08); border-left: 4px solid transparent; }
.card-total { border-left-color: #3b82f6; }
.card-active { border-left-color: #10b981; }
.card-warn { border-left-color: #f59e0b; }
.card-cert { border-left-color: #ef4444; }
.card-label { font-size: 12px; color: #64748b; margin-bottom: 6px; }
.card-value { font-size: 28px; font-weight: 700; color: #1e293b; }

/* 同步栏 */
.sync-bar {
  background: white; border-radius: 10px; padding: 12px 18px; margin-bottom: 16px;
  box-shadow: 0 1px 4px rgba(0,0,0,0.08); display: flex; align-items: center; gap: 14px; flex-wrap: wrap;
}
.sync-label { font-size: 12px; color: #64748b; font-weight: 500; flex-shrink: 0; }
.sync-items { display: flex; gap: 10px; flex-wrap: wrap; }
.sync-item { display: flex; align-items: center; gap: 8px; background: #f8fafc; border: 1px solid #e2e8f0; padding: 5px 10px; border-radius: 8px; }
.sync-name { font-size: 12px; font-weight: 500; color: #334155; }
.sync-time { font-size: 11px; color: #94a3b8; }
.btn-sync { background: #3b82f6; color: white; border: none; padding: 3px 10px; border-radius: 5px; font-size: 12px; cursor: pointer; transition: opacity 0.2s; }
.btn-sync:hover:not(:disabled) { opacity: 0.85; }
.btn-sync:disabled { opacity: 0.5; cursor: not-allowed; }

/* 过滤栏 */
.filter-bar {
  background: white; border-radius: 10px; padding: 14px 18px; margin-bottom: 16px;
  box-shadow: 0 1px 4px rgba(0,0,0,0.08); display: flex; align-items: center; gap: 10px; flex-wrap: wrap;
}
.filter-search { flex: 1; min-width: 200px; }
.filter-input, .filter-select {
  padding: 7px 11px; border: 1.5px solid #e2e8f0; border-radius: 7px; font-size: 13px;
  outline: none; transition: border-color 0.2s; background: white; color: #334155;
}
.filter-input:focus, .filter-select:focus { border-color: #3b82f6; }
.btn-reset {
  background: #f1f5f9; border: 1.5px solid #e2e8f0; color: #64748b;
  padding: 7px 14px; border-radius: 7px; font-size: 13px; cursor: pointer; transition: all 0.2s; white-space: nowrap;
}
.btn-reset:hover { background: #e2e8f0; }

/* 表格 */
.table-container { background: white; border-radius: 10px; box-shadow: 0 1px 4px rgba(0,0,0,0.08); overflow-x: auto; margin-bottom: 16px; }
.domain-table { width: 100%; border-collapse: collapse; font-size: 13px; }
.domain-table th { background: #f8fafc; padding: 11px 10px; text-align: left; font-weight: 600; color: #475569; border-bottom: 2px solid #e2e8f0; white-space: nowrap; }
.domain-table td { padding: 9px 10px; border-bottom: 1px solid #f1f5f9; vertical-align: middle; white-space: nowrap; }
.domain-row { transition: background 0.15s; }
.domain-row:hover { background: #f0f7ff; }

.td-full-domain { min-width: 160px; }
.td-value { max-width: 130px; }
.td-time { min-width: 140px; white-space: nowrap; color: #64748b; font-size: 12px; }
.td-remark { max-width: 140px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; color: #64748b; font-size: 12px; cursor: default; }
.full-domain-text { font-weight: 600; color: #1e40af; }
.value-truncate { display: inline-block; max-width: 120px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; vertical-align: middle; }

/* 环境 badge */
.env-badge { display: inline-block; padding: 2px 8px; border-radius: 4px; font-size: 11px; font-weight: 700; }
.env-prod { background: #dbeafe; color: #1d4ed8; }
.env-uat { background: #fef3c7; color: #92400e; }
.env-dev { background: #d1fae5; color: #065f46; }

/* 记录类型 badge */
.badge { display: inline-block; padding: 2px 7px; border-radius: 4px; font-size: 11px; font-weight: 600; }
.badge-blue { background: #dbeafe; color: #1d4ed8; }
.badge-green { background: #d1fae5; color: #065f46; }
.badge-purple { background: #ede9fe; color: #5b21b6; }
.badge-gray { background: #f1f5f9; color: #475569; }

/* 状态 */
.status-badge { display: inline-block; padding: 3px 10px; border-radius: 12px; font-size: 11px; font-weight: 600; white-space: nowrap; }
.status-active { background: #d1fae5; color: #065f46; }
.status-standby { background: #fef3c7; color: #92400e; }
.status-inactive { background: #fee2e2; color: #991b1b; }

/* 来源 */
.source-badge { display: inline-block; padding: 2px 7px; border-radius: 4px; font-size: 11px; font-weight: 600; white-space: nowrap; }
.source-manual { background: #f1f5f9; color: #475569; }
.source-godaddy { background: #fef3c7; color: #92400e; }
.source-namecom { background: #e0e7ff; color: #3730a3; }

/* 日期颜色 */
.date-danger { color: #dc2626; font-weight: 600; }
.date-warn { color: #d97706; font-weight: 500; }
.date-normal { color: #374151; }

/* 操作按钮 */
.btn-check-cert { background: transparent; border: 1px solid #3b82f6; color: #3b82f6; padding: 2px 7px; border-radius: 4px; font-size: 11px; cursor: pointer; margin-left: 5px; transition: all 0.2s; }
.btn-check-cert:hover:not(:disabled) { background: #3b82f6; color: white; }
.btn-check-cert:disabled { opacity: 0.5; cursor: not-allowed; }
.btn-edit { background: #eff6ff; border: 1px solid #bfdbfe; color: #1d4ed8; padding: 4px 9px; border-radius: 5px; font-size: 12px; cursor: pointer; margin-right: 5px; transition: all 0.2s; white-space: nowrap; }
.btn-edit:hover { background: #dbeafe; }
.btn-delete { background: #fef2f2; border: 1px solid #fecaca; color: #dc2626; padding: 4px 9px; border-radius: 5px; font-size: 12px; cursor: pointer; transition: all 0.2s; white-space: nowrap; }
.btn-delete:hover { background: #fee2e2; }
.empty-cell { text-align: center; padding: 48px; color: #94a3b8; font-size: 14px; }

/* 分页 */
.pagination { display: flex; align-items: center; justify-content: space-between; background: white; border-radius: 10px; padding: 13px 18px; box-shadow: 0 1px 4px rgba(0,0,0,0.08); }
.page-info { font-size: 13px; color: #64748b; }
.page-buttons { display: flex; align-items: center; gap: 7px; }
.page-btn { background: #f1f5f9; border: 1.5px solid #e2e8f0; color: #475569; padding: 5px 11px; border-radius: 6px; font-size: 13px; cursor: pointer; transition: all 0.2s; }
.page-btn:hover:not(:disabled) { background: #e2e8f0; }
.page-btn:disabled { opacity: 0.4; cursor: not-allowed; }
.page-current { font-size: 13px; color: #334155; font-weight: 500; padding: 0 6px; }
.page-size-select { padding: 5px 8px; border: 1.5px solid #e2e8f0; border-radius: 6px; font-size: 13px; outline: none; background: white; color: #334155; cursor: pointer; margin-left: 6px; }

/* 弹窗 */
.modal-overlay { position: fixed; inset: 0; background: rgba(0,0,0,0.45); display: flex; align-items: center; justify-content: center; z-index: 1000; padding: 20px; }
.modal-box { background: white; border-radius: 12px; width: 100%; max-width: 680px; box-shadow: 0 20px 50px rgba(0,0,0,0.25); max-height: 90vh; overflow-y: auto; }
.modal-small { max-width: 400px; }
.modal-header { display: flex; align-items: center; justify-content: space-between; padding: 18px 22px; border-bottom: 1px solid #e2e8f0; }
.modal-header h3 { font-size: 15px; font-weight: 600; color: #1e293b; }
.modal-close { background: transparent; border: none; font-size: 22px; color: #94a3b8; cursor: pointer; }
.modal-close:hover { color: #475569; }
.modal-body { padding: 18px 22px; }
.modal-domain-info { margin-bottom: 14px; padding-bottom: 14px; border-bottom: 1px solid #f1f5f9; font-size: 14px; color: #475569; }
.form-grid { display: grid; grid-template-columns: 1fr 1fr; gap: 12px; }
.form-group { display: flex; flex-direction: column; gap: 4px; }
.form-group label { font-size: 11px; font-weight: 600; color: #64748b; text-transform: uppercase; }
.label-tip { font-size: 10px; font-weight: 400; color: #f59e0b; background: #fef3c7; padding: 1px 5px; border-radius: 3px; text-transform: none; margin-left: 4px; }
.input-with-btn { display: flex; gap: 6px; align-items: center; }
.input-with-btn input { flex: 1; }
.btn-query { padding: 8px 12px; background: #f1f5f9; border: 1.5px solid #e2e8f0; color: #475569; border-radius: 7px; font-size: 12px; cursor: pointer; white-space: nowrap; transition: all 0.2s; }
.btn-query:hover:not(:disabled) { background: #e2e8f0; }
.btn-query:disabled { opacity: 0.5; cursor: not-allowed; }
.form-group input, .form-group select, .form-group textarea { padding: 8px 11px; border: 1.5px solid #e2e8f0; border-radius: 7px; font-size: 13px; outline: none; transition: border-color 0.2s; font-family: inherit; }
.form-group input:focus, .form-group select:focus, .form-group textarea:focus { border-color: #3b82f6; }
.form-group textarea { resize: vertical; }
.error-msg { background: #fef2f2; border: 1px solid #fecaca; color: #dc2626; padding: 9px 13px; border-radius: 7px; font-size: 13px; margin-top: 10px; }
.modal-footer { display: flex; justify-content: flex-end; gap: 10px; padding: 14px 22px; border-top: 1px solid #e2e8f0; }
.btn-cancel { background: #f1f5f9; border: 1.5px solid #e2e8f0; color: #475569; padding: 8px 18px; border-radius: 7px; font-size: 13px; cursor: pointer; }
.btn-cancel:hover { background: #e2e8f0; }
.btn-save { background: #3b82f6; border: none; color: white; padding: 8px 22px; border-radius: 7px; font-size: 13px; font-weight: 600; cursor: pointer; transition: opacity 0.2s; }
.btn-save:hover:not(:disabled) { opacity: 0.88; }
.btn-save:disabled { opacity: 0.5; cursor: not-allowed; }
.btn-danger { background: #ef4444; border: none; color: white; padding: 8px 22px; border-radius: 7px; font-size: 13px; font-weight: 600; cursor: pointer; }
.btn-danger:hover:not(:disabled) { opacity: 0.88; }
.btn-danger:disabled { opacity: 0.5; cursor: not-allowed; }

/* Toast */
.toast { position: fixed; bottom: 28px; right: 28px; padding: 13px 22px; border-radius: 10px; font-size: 14px; font-weight: 500; box-shadow: 0 8px 24px rgba(0,0,0,0.18); z-index: 2000; animation: slideIn 0.3s ease; }
.toast-success { background: #10b981; color: white; }
.toast-error { background: #ef4444; color: white; }
@keyframes slideIn { from { transform: translateX(100px); opacity: 0; } to { transform: translateX(0); opacity: 1; } }

@media (max-width: 768px) {
  .summary-cards { grid-template-columns: repeat(2,1fr); }
  .form-grid { grid-template-columns: 1fr; }
}
</style>
