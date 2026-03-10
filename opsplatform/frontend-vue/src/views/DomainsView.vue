<script setup>
import { ref, computed, onMounted } from 'vue'
import { useAppStore, useAuthStore } from '@/stores'
import api from '@/api'

const appStore = useAppStore()
const authStore = useAuthStore()

// 权限检查
const canAdd = computed(() => authStore.hasPermission('domain:create'))
const canEdit = computed(() => authStore.hasPermission('domain:update'))
const canDelete = computed(() => authStore.hasPermission('domain:delete'))
const canExport = computed(() => authStore.hasPermission('domain:export'))
const canBatchAdd = computed(() => authStore.hasPermission('domain:batch_add'))
const canRefresh = computed(() => authStore.hasPermission('domain:refresh'))

const domains = ref([])
const loading = ref(false)
const currentPage = ref(1)
const pageSize = ref(10)

// 临时筛选条件
const tempSearchQuery = ref('')
const tempProjectFilter = ref('')
const tempCdnFilter = ref('')
const tempEnvFilter = ref('')
const tempStatusFilter = ref('')
const tempExpireFilter = ref('')
const tempDuplicateFilter = ref('')

// 实际筛选条件
const searchQuery = ref('')
const projectFilter = ref('')
const cdnFilter = ref('')
const envFilter = ref('')
const statusFilter = ref('')
const expireFilter = ref('')
const duplicateFilter = ref('')

const showModal = ref(false)
const editingDomain = ref(null)
const formData = ref({
  id: '',
  project: '',
  module: '',
  domain_name: '',
  origin: '',
  origin_ip: '',
  cdn_provider: '',
  env: 'PROD',
  expire_time: '',
  cert_expire_time: '',
  status: 'active',
  remark: ''
})

const checkingDomain = ref(null)
const selectedDomains = ref([])
const batchRefreshing = ref(false)
const batchRefreshProgress = ref('')

// 预览弹窗
const showPreview = ref(false)
const previewDomain = ref(null)

function openPreview(d) {
  previewDomain.value = d
  showPreview.value = true
}
function closePreview() {
  showPreview.value = false
  previewDomain.value = null
}

// 批量添加域名相关
const showBatchModal = ref(false)
const batchDomainText = ref('')
const batchDomainRecords = ref([])
const batchDomainError = ref('')
const batchDomainProject = ref('')
const batchDomainEnv = ref('PROD')
const batchDomainFetchExpiry = ref('skip')
const batchDomainLoading = ref(false)

// 计算属性：项目列表
const projectList = computed(() => {
  const projects = [...new Set((domains.value || []).map(d => d.project).filter(Boolean))]
  return projects.sort((a, b) => a.localeCompare(b, 'zh-CN'))
})

// 计算属性：CDN列表
const cdnList = computed(() => {
  const cdns = [...new Set((domains.value || []).map(d => d.cdn_provider).filter(Boolean))]
  return cdns.sort((a, b) => a.localeCompare(b, 'zh-CN'))
})

// 过滤后的域名
const filteredDomains = computed(() => {
  let d = domains.value || []
  if (projectFilter.value) d = d.filter(x => x.project === projectFilter.value)
  if (cdnFilter.value) d = d.filter(x => x.cdn_provider === cdnFilter.value)
  if (envFilter.value) d = d.filter(x => (x.env || 'PROD') === envFilter.value)
  if (statusFilter.value) d = d.filter(x => x.status === statusFilter.value)
  
  // 重复域名筛选
  if (duplicateFilter.value) {
    const domainCounts = {}
    ;(domains.value || []).forEach(x => {
      domainCounts[x.domain_name] = (domainCounts[x.domain_name] || 0) + 1
    })
    if (duplicateFilter.value === 'duplicate') {
      d = d.filter(x => domainCounts[x.domain_name] > 1)
    } else if (duplicateFilter.value === 'unique') {
      d = d.filter(x => domainCounts[x.domain_name] === 1)
    }
  }
  
  // 到期时间筛选
  if (expireFilter.value) {
    d = d.filter(x => {
      if (!x.expire_time) return false
      const expireDate = new Date(x.expire_time)
      const now = new Date()
      const diffDays = Math.ceil((expireDate - now) / (1000 * 60 * 60 * 24))
      if (expireFilter.value === '90+') return diffDays > 90
      const days = parseInt(expireFilter.value)
      return diffDays >= 0 && diffDays <= days
    })
  }
  
  // 搜索过滤
  if (searchQuery.value) {
    const q = searchQuery.value.toLowerCase()
    d = d.filter(x =>
      (x.domain_name && x.domain_name.toLowerCase().includes(q)) ||
      (x.project && x.project.toLowerCase().includes(q)) ||
      (x.module && x.module.toLowerCase().includes(q)) ||
      (x.origin && x.origin.toLowerCase().includes(q))
    )
  }
  return d
})

const pagedDomains = computed(() => {
  const start = (currentPage.value - 1) * pageSize.value
  return filteredDomains.value.slice(start, start + pageSize.value)
})

const totalPages = computed(() => Math.max(1, Math.ceil(filteredDomains.value.length / pageSize.value)))
const totalCount = computed(() => filteredDomains.value.length)

// 全选状态
const isAllSelected = computed(() => {
  return pagedDomains.value.length > 0 && pagedDomains.value.every(d => selectedDomains.value.includes(d.id))
})

onMounted(() => {
  loadDomains()
  loadCdnProviders()
})

async function loadDomains() {
  loading.value = true
  try {
    const res = await api.get('/api/domains')
    domains.value = res.data?.domains || res.data || []
  } catch (e) {
    appStore.showToast('加载域名失败', 'error')
  } finally {
    loading.value = false
  }
}

function applyFilter() {
  searchQuery.value = tempSearchQuery.value
  projectFilter.value = tempProjectFilter.value
  cdnFilter.value = tempCdnFilter.value
  envFilter.value = tempEnvFilter.value
  statusFilter.value = tempStatusFilter.value
  expireFilter.value = tempExpireFilter.value
  duplicateFilter.value = tempDuplicateFilter.value
  currentPage.value = 1
}

function resetFilter() {
  tempSearchQuery.value = ''
  tempProjectFilter.value = ''
  tempCdnFilter.value = ''
  tempEnvFilter.value = ''
  tempStatusFilter.value = ''
  tempExpireFilter.value = ''
  tempDuplicateFilter.value = ''
  searchQuery.value = ''
  projectFilter.value = ''
  cdnFilter.value = ''
  envFilter.value = ''
  statusFilter.value = ''
  expireFilter.value = ''
  duplicateFilter.value = ''
  currentPage.value = 1
}

function openModal(domain = null) {
  editingDomain.value = domain
  if (domain) {
    formData.value = {
      id: domain.id,
      project: domain.project || '',
      module: domain.module || '',
      domain_name: domain.domain_name || '',
      origin: domain.origin || '',
      origin_ip: domain.origin_ip || '',
      cdn_provider: domain.cdn_provider || '',
      env: domain.env || 'PROD',
      expire_time: formatDateForInput(domain.expire_time),
      cert_expire_time: formatDateForInput(domain.cert_expire_time),
      status: domain.status || 'active',
      remark: domain.remark || ''
    }
  } else {
    formData.value = {
      id: '',
      project: '',
      module: '',
      domain_name: '',
      origin: '',
      origin_ip: '',
      cdn_provider: '',
      env: 'PROD',
      expire_time: '',
      cert_expire_time: '',
      status: 'active',
      remark: ''
    }
  }
  showModal.value = true
}

function formatDateForInput(dateStr) {
  if (!dateStr) return ''
  return dateStr.split('T')[0]
}

function formatDate(dateStr) {
  if (!dateStr) return '-'
  return dateStr.split('T')[0]
}

function formatDateTime(dateStr) {
  if (!dateStr) return '-'
  const d = new Date(dateStr)
  if (isNaN(d.getTime())) return '-'
  const pad = n => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}:${pad(d.getSeconds())}`
}

async function saveDomain() {
  if (!formData.value.domain_name) {
    appStore.showToast('请输入域名', 'error')
    return
  }

  const currentUser = authStore.user?.username || ''
  const payload = { 
    ...formData.value,
    updated_by: currentUser,
    created_by: editingDomain.value ? formData.value.created_by : currentUser
  }
  delete payload.id

  try {
    if (editingDomain.value) {
      await api.put('/api/domains/' + editingDomain.value.id, payload)
      appStore.showToast('域名更新成功', 'success')
    } else {
      await api.post('/api/domains', payload)
      appStore.showToast('域名添加成功', 'success')
    }
    showModal.value = false
    loadDomains()
  } catch (e) {
    appStore.showToast('保存失败: ' + (e.response?.data?.error || e.message), 'error')
  }
}

async function deleteDomain(domain) {
  const confirmed = await appStore.showConfirm({
    type: 'danger',
    title: '删除域名',
    message: `确定要删除域名 "${domain.domain_name}" 吗？`,
    okText: '删除',
    cancelText: '取消'
  })
  if (!confirmed) return

  try {
    await api.delete('/api/domains/' + domain.id)
    appStore.showToast('删除成功', 'success')
    loadDomains()
  } catch (e) {
    appStore.showToast('删除失败', 'error')
  }
}

async function refreshDomainExpire(domain) {
  checkingDomain.value = domain.id
  try {
    const res = await api.get('/api/domains/check-cert', { params: { domain: domain.domain_name } })
    const results = []
    const errors = []
    
    if (res.data.cert_expire_time) {
      results.push('证书到期: ' + res.data.cert_expire_time.split('T')[0])
    } else if (res.data.cert_error) {
      errors.push('证书检测失败')
    }
    
    if (res.data.expire_time) {
      results.push('域名到期: ' + res.data.expire_time.split('T')[0])
    } else if (res.data.domain_error) {
      errors.push('域名检测失败')
    }
    
    // 只要有任何结果就更新数据库
    if (res.data.cert_expire_time || res.data.expire_time) {
      const currentUser = authStore.user?.username || ''
      await api.put('/api/domains/' + domain.id, {
        ...domain,
        cert_expire_time: res.data.cert_expire_time || domain.cert_expire_time,
        expire_time: res.data.expire_time || domain.expire_time,
        updated_at: new Date().toISOString(),
        updated_by: currentUser
      })
      loadDomains()
    }
    
    if (results.length > 0 && errors.length > 0) {
      appStore.showToast(results.join(', ') + ' | ' + errors.join(', '), 'warning')
    } else if (results.length > 0) {
      appStore.showToast(results.join(', '), 'success')
    } else if (errors.length > 0) {
      appStore.showToast(errors.join(', '), 'error')
    } else {
      appStore.showToast('未能获取域名信息', 'warning')
    }
  } catch (e) {
    appStore.showToast('检测失败: ' + (e.response?.data?.error || e.message), 'error')
  } finally {
    checkingDomain.value = null
  }
}

// 自动检测域名证书
const checkingCert = ref(false)
async function checkDomainCert() {
  if (!formData.value.domain_name) {
    appStore.showToast('请先输入域名', 'warning')
    return
  }
  checkingCert.value = true
  try {
    const res = await api.get('/api/domains/check-cert', { params: { domain: formData.value.domain_name } })
    const results = []
    const errors = []
    
    if (res.data.cert_expire_time) {
      formData.value.cert_expire_time = formatDateForInput(res.data.cert_expire_time)
      results.push('证书到期: ' + res.data.cert_expire_time.split('T')[0])
    } else if (res.data.cert_error) {
      errors.push('证书检测失败')
    }
    
    if (res.data.expire_time) {
      formData.value.expire_time = formatDateForInput(res.data.expire_time)
      results.push('域名到期: ' + res.data.expire_time.split('T')[0])
    } else if (res.data.domain_error) {
      errors.push('域名检测失败')
    }
    
    if (results.length > 0 && errors.length > 0) {
      appStore.showToast(results.join(', ') + ' | ' + errors.join(', '), 'warning')
    } else if (results.length > 0) {
      appStore.showToast(results.join(', '), 'success')
    } else if (errors.length > 0) {
      appStore.showToast(errors.join(', '), 'error')
    } else {
      appStore.showToast('未能获取域名信息', 'warning')
    }
  } catch (e) {
    const errMsg = e.response?.data?.error || e.message || '检测失败，请检查域名是否正确'
    appStore.showToast(errMsg, 'error')
  } finally {
    checkingCert.value = false
  }
}

// 批量刷新到期时间（分批处理，每批10个）
async function batchRefreshExpire() {
  const activeDomains = domains.value.filter(d => d.status === 'active')
  if (activeDomains.length === 0) {
    appStore.showToast('没有启用的域名需要刷新', 'warning')
    return
  }

  batchRefreshing.value = true
  const total = activeDomains.length
  let success = 0
  let failed = 0
  const batchSize = 10
  const currentUser = authStore.user?.username || ''

  for (let i = 0; i < total; i += batchSize) {
    const batch = activeDomains.slice(i, i + batchSize)
    batchRefreshProgress.value = `${Math.min(i + batchSize, total)}/${total}`

    // 并发处理当前批次
    const promises = batch.map(async (domain) => {
      try {
        const res = await api.get('/api/domains/check-cert', { params: { domain: domain.domain_name } })
        if (res.data.cert_expire_time || res.data.expire_time) {
          await api.put('/api/domains/' + domain.id, {
            ...domain,
            cert_expire_time: res.data.cert_expire_time || domain.cert_expire_time,
            expire_time: res.data.expire_time || domain.expire_time,
            updated_at: new Date().toISOString(),
            updated_by: currentUser
          })
          success++
        }
      } catch (e) {
        failed++
      }
    })
    
    await Promise.all(promises)
    
    // 每批之间稍作延迟，避免过快请求
    if (i + batchSize < total) {
      await new Promise(r => setTimeout(r, 500))
    }
  }
  
  batchRefreshing.value = false
  batchRefreshProgress.value = ''
  appStore.showToast(`刷新完成: ${success} 成功, ${failed} 失败`, success > 0 ? 'success' : 'warning')
  loadDomains()
}

function getDaysLeft(expireTime) {
  if (!expireTime) return null
  const now = new Date()
  const expire = new Date(expireTime)
  return Math.ceil((expire - now) / (1000 * 60 * 60 * 24))
}

function getExpiryBadgeClass(expireTime) {
  const days = getDaysLeft(expireTime)
  if (days === null) return 'expiry-none'
  if (days <= 0) return 'expiry-expired'
  if (days <= 7) return 'expiry-danger'
  if (days <= 15) return 'expiry-warning'
  if (days <= 30) return 'expiry-notice'
  return 'expiry-safe'
}

function getEnvClass(env) {
  const map = { 'PROD': 'env-prod', 'UAT': 'env-uat', 'DEV': 'env-dev' }
  return map[env] || 'env-prod'
}

// 选择相关
function toggleSelectAll() {
  if (isAllSelected.value) {
    selectedDomains.value = selectedDomains.value.filter(id => !pagedDomains.value.find(d => d.id === id))
  } else {
    pagedDomains.value.forEach(d => {
      if (!selectedDomains.value.includes(d.id)) {
        selectedDomains.value.push(d.id)
      }
    })
  }
}

function toggleSelect(domain) {
  const idx = selectedDomains.value.indexOf(domain.id)
  if (idx >= 0) {
    selectedDomains.value.splice(idx, 1)
  } else {
    selectedDomains.value.push(domain.id)
  }
}

// 批量操作
async function batchAction(action) {
  if (selectedDomains.value.length === 0) return
  
  if (action === 'delete') {
    const confirmed = await appStore.showConfirm({
      type: 'danger',
      title: '批量删除',
      message: `确定要删除选中的 ${selectedDomains.value.length} 条域名吗？`,
      okText: '删除',
      cancelText: '取消'
    })
    if (!confirmed) return
  }
  
  try {
    await api.post('/api/domains/batch-action', {
      ids: selectedDomains.value,
      action: action
    })
    appStore.showToast('操作成功', 'success')
    selectedDomains.value = []
    loadDomains()
  } catch (e) {
    appStore.showToast('操作失败', 'error')
  }
}

// 导出
async function exportDomains() {
  try {
    const res = await api.get('/api/domains/export', { responseType: 'blob' })
    const url = window.URL.createObjectURL(new Blob([res.data]))
    const link = document.createElement('a')
    link.href = url
    link.setAttribute('download', `domains_${new Date().toISOString().slice(0, 10)}.xlsx`)
    document.body.appendChild(link)
    link.click()
    link.remove()
    appStore.showToast('导出成功', 'success')
  } catch (e) {
    appStore.showToast('导出失败', 'error')
  }
}

// 打开批量添加弹窗
function openBatchModal() {
  batchDomainText.value = `example.com
API模块,api.example.com,origin.com,192.168.1.1,阿里云CDN,2025-12-31,生产环境
Web模块,www.example.com`
  batchDomainRecords.value = []
  batchDomainError.value = ''
  batchDomainProject.value = ''
  batchDomainEnv.value = 'PROD'
  batchDomainFetchExpiry.value = 'skip'
  showBatchModal.value = true
  // 延迟解析示例数据
  setTimeout(() => {
    if (batchDomainProject.value) {
      parseBatchDomains()
    }
  }, 100)
}

// 解析批量域名
function parseBatchDomains() {
  batchDomainError.value = ''
  batchDomainRecords.value = []

  if (!batchDomainText.value.trim()) return
  if (!batchDomainProject.value.trim()) {
    batchDomainError.value = '请先填写项目名称'
    return
  }

  const lines = batchDomainText.value.trim().split('\n')
  const records = []

  for (const line of lines) {
    const trimmed = line.trim()
    if (!trimmed) continue

    const parts = trimmed.split(',').map(p => p.trim())
    const domainName = parts[0]

    if (!domainName || !/^[\w.-]+\.[a-z]{2,}$/i.test(domainName)) continue

    if (parts.length >= 2) {
      records.push({
        domain_name: domainName,
        module: parts[1] || '',
        origin: parts[2] || '',
        origin_ip: parts[3] || '',
        cdn_provider: parts[4] || '',
        expire_time: parts[5] || '',
        remark: parts[6] || '',
        project: batchDomainProject.value,
        env: batchDomainEnv.value,
        status: 'active'
      })
    } else {
      records.push({
        domain_name: domainName,
        module: '',
        origin: '',
        origin_ip: '',
        cdn_provider: '',
        expire_time: '',
        remark: '',
        project: batchDomainProject.value,
        env: batchDomainEnv.value,
        status: 'active'
      })
    }
  }
  batchDomainRecords.value = records
}

// 提交批量添加
async function submitBatchDomains() {
  if (batchDomainRecords.value.length === 0) {
    appStore.showToast('请先输入有效的域名', 'error')
    return
  }

  batchDomainLoading.value = true
  try {
    const res = await api.post('/api/domains/batch-add', {
      domains: batchDomainRecords.value,
      created_by: appStore.currentUser?.username,
      fetch_expiry: batchDomainFetchExpiry.value === 'fetch'
    })
    if (res.data.success) {
      appStore.showToast(`成功添加 ${res.data.added || batchDomainRecords.value.length} 个域名`, 'success')
      showBatchModal.value = false
      loadDomains()
    } else {
      appStore.showToast(res.data.message || '批量添加失败', 'error')
    }
  } catch (e) {
    appStore.showToast('批量添加失败: ' + (e.response?.data?.error || e.message), 'error')
  } finally {
    batchDomainLoading.value = false
  }
}

// CDN 选项（从后端加载 + 默认值）
const DEFAULT_CDN_OPTIONS = [
  { value: '', label: '无' },
  { value: '阿里云CDN', label: '阿里云' },
  { value: '腾讯云CDN', label: '腾讯云' },
  { value: 'Cloudflare', label: 'Cloudflare' },
  { value: '华为云CDN', label: '华为云' },
  { value: '七牛云CDN', label: '七牛云' },
  { value: '网宿CDN', label: '网宿' },
  { value: '其他', label: '其他' }
]
const customCdnProviders = ref([])
const cdnOptions = computed(() => {
  if (customCdnProviders.value.length > 0) {
    return [{ value: '', label: '无' }, ...customCdnProviders.value]
  }
  return DEFAULT_CDN_OPTIONS
})

// CDN 管理弹窗
const showCdnManager = ref(false)
const cdnManagerList = ref([])
const newCdnValue = ref('')
const newCdnLabel = ref('')

async function loadCdnProviders() {
  try {
    const res = await api.get('/api/domains/cdn-providers')
    const data = res.data
    if (Array.isArray(data) && data.length > 0) {
      customCdnProviders.value = data
    }
  } catch (e) { /* use defaults */ }
}

function openCdnManager() {
  cdnManagerList.value = cdnOptions.value.filter(o => o.value !== '').map(o => ({ ...o }))
  newCdnValue.value = ''
  newCdnLabel.value = ''
  showCdnManager.value = true
}

function addCdnProvider() {
  const val = newCdnValue.value.trim()
  const lbl = newCdnLabel.value.trim()
  if (!val) return
  if (cdnManagerList.value.some(o => o.value === val)) {
    appStore.showToast('该CDN已存在', 'warning')
    return
  }
  cdnManagerList.value.push({ value: val, label: lbl || val })
  newCdnValue.value = ''
  newCdnLabel.value = ''
}

function removeCdnProvider(idx) {
  cdnManagerList.value.splice(idx, 1)
}

async function saveCdnProviders() {
  try {
    await api.post('/api/domains/cdn-providers', { providers: cdnManagerList.value })
    customCdnProviders.value = [...cdnManagerList.value]
    showCdnManager.value = false
    appStore.showToast('CDN厂商配置已保存', 'success')
  } catch (e) {
    appStore.showToast('保存失败', 'error')
  }
}

// 批量修改弹窗
const showBatchEditModal = ref(false)
const batchEditData = ref({ origin: '', origin_ip: '', cdn_provider: '', status: '' })
const batchEditFields = ref({ origin: false, origin_ip: false, cdn_provider: false, status: false })
const batchEditLoading = ref(false)

function openBatchEditModal() {
  if (selectedDomains.value.length === 0) {
    appStore.showToast('请先选择域名', 'warning')
    return
  }
  batchEditData.value = { origin: '', origin_ip: '', cdn_provider: '', status: '' }
  batchEditFields.value = { origin: false, origin_ip: false, cdn_provider: false, status: false }
  showBatchEditModal.value = true
}

async function submitBatchEdit() {
  const payload = { ids: selectedDomains.value, operator: authStore.user?.username || '' }
  if (batchEditFields.value.origin) payload.origin = batchEditData.value.origin
  if (batchEditFields.value.origin_ip) payload.origin_ip = batchEditData.value.origin_ip
  if (batchEditFields.value.cdn_provider) payload.cdn_provider = batchEditData.value.cdn_provider
  if (batchEditFields.value.status) payload.status = batchEditData.value.status

  {
    if (!batchEditFields.value.origin && !batchEditFields.value.origin_ip && !batchEditFields.value.cdn_provider && !batchEditFields.value.status) {
      appStore.showToast('请至少勾选一个修改项', 'warning')
      return
    }
  }

  batchEditLoading.value = true
  try {
    const res = await api.post('/api/domains/batch-update', payload)
    appStore.showToast(res.data.message || '批量修改成功', 'success')
    showBatchEditModal.value = false
    selectedDomains.value = []
    loadDomains()
  } catch (e) {
    appStore.showToast('批量修改失败: ' + (e.response?.data?.error || e.message), 'error')
  } finally {
    batchEditLoading.value = false
  }
}
</script>

<template>
  <div class="domains-page">
    <!-- 页面头部 -->
    <div class="page-header-card">
      <h1 class="page-title">域名管理</h1>
      <div class="header-actions">
        <button class="btn btn-default" @click="loadDomains" :disabled="loading">
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M4 4v5h.582m15.356 2A8.001 8.001 0 004.582 9m0 0H9m11 11v-5h-.581m0 0a8.003 8.003 0 01-15.357-2m15.357 2H15"/></svg>
          刷新
        </button>
        <button v-if="canRefresh" class="btn btn-warning" @click="batchRefreshExpire" :disabled="batchRefreshing">
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><circle cx="12" cy="12" r="10"/><polyline points="12 6 12 12 16 14"/></svg>
          {{ batchRefreshing ? '刷新中...' + batchRefreshProgress : '批量刷新到期时间' }}
        </button>
        <button v-if="canExport" class="btn btn-default" @click="exportDomains">
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4"/><polyline points="7 10 12 15 17 10"/><line x1="12" y1="15" x2="12" y2="3"/></svg>
          导出
        </button>
        <button v-if="canBatchAdd" class="btn btn-default" @click="openBatchModal">
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M12 2L2 7l10 5 10-5-10-5z"/><path d="M2 17l10 5 10-5"/><path d="M2 12l10 5 10-5"/></svg>
          批量添加
        </button>
        <button v-if="canAdd" class="btn btn-primary" @click="openModal()">
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><line x1="12" y1="5" x2="12" y2="19"/><line x1="5" y1="12" x2="19" y2="12"/></svg>
          添加
        </button>
        <button class="btn btn-default" @click="openCdnManager">
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M12 2L2 7l10 5 10-5-10-5z"/><path d="M2 17l10 5 10-5"/><path d="M2 12l10 5 10-5"/></svg>
          CDN配置
        </button>
      </div>
    </div>

    <!-- 筛选栏 -->
    <div class="filter-bar">
      <div class="search-box">
        <svg class="search-icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><circle cx="11" cy="11" r="8"/><path d="m21 21-4.35-4.35"/></svg>
        <input type="text" v-model="tempSearchQuery" placeholder="搜索域名、项目、模块..." class="search-input" @keyup.enter="applyFilter">
      </div>
      <select v-model="tempProjectFilter" class="filter-select">
        <option value="">所有项目</option>
        <option v-for="p in projectList" :key="p" :value="p">{{ p }}</option>
      </select>
      <select v-model="tempCdnFilter" class="filter-select">
        <option value="">所有CDN厂商</option>
        <option v-for="c in cdnList" :key="c" :value="c">{{ c }}</option>
      </select>
      <select v-model="tempEnvFilter" class="filter-select">
        <option value="">所有环境</option>
        <option value="PROD">PROD</option>
        <option value="UAT">UAT</option>
        <option value="DEV">DEV</option>
      </select>
      <select v-model="tempStatusFilter" class="filter-select">
        <option value="">所有状态</option>
        <option value="active">启用</option>
        <option value="inactive">停用</option>
      </select>
      <select v-model="tempDuplicateFilter" class="filter-select">
        <option value="">所有域名</option>
        <option value="duplicate">重复域名</option>
        <option value="unique">唯一域名</option>
      </select>
      <select v-model="tempExpireFilter" class="filter-select">
        <option value="">所有到期时间</option>
        <option value="7">7天内到期</option>
        <option value="15">15天内到期</option>
        <option value="30">30天内到期</option>
        <option value="60">60天内到期</option>
        <option value="90">90天内到期</option>
        <option value="90+">90天以上到期</option>
      </select>
      <div class="filter-actions">
        <button class="btn-search" @click="applyFilter">搜 索</button>
        <button class="btn-reset" @click="resetFilter">重 置</button>
      </div>
    </div>

    <!-- 批量操作栏 -->
    <div class="batch-action-bar" v-if="selectedDomains.length > 0">
      <span class="batch-info">已选择 {{ selectedDomains.length }} 项</span>
      <button class="btn btn-primary btn-sm" @click="openBatchEditModal">批量修改</button>
      <button class="btn btn-success btn-sm" @click="batchAction('enable')">批量启用</button>
      <button class="btn btn-warning btn-sm" @click="batchAction('disable')">批量停用</button>
      <button class="btn btn-danger btn-sm" @click="batchAction('delete')">批量删除</button>
    </div>
    
    <!-- 域名表格 -->
    <div class="table-container">
      <table class="data-table table-fixed-action">
        <thead>
          <tr>
            <th class="checkbox-col" style="width:40px;">
              <input type="checkbox" :checked="isAllSelected" @change="toggleSelectAll">
            </th>
            <th style="width:60px;">项目</th>
            <th style="width:50px;">环境</th>
            <th style="width:70px;">模块</th>
            <th style="width:160px;">域名</th>
            <th style="width:140px;">回源</th>
            <th style="width:100px;">源站IP</th>
            <th style="width:70px;">CDN厂商</th>
            <th style="width:90px;">域名到期</th>
            <th style="width:90px;">证书到期</th>
            <th style="width:60px;">状态</th>
            <th style="width:130px;">创建时间</th>
            <th style="width:130px;">更新时间</th>
            <th style="width:80px;">备注</th>
            <th class="th-action-fixed" style="width:160px;">操作</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="d in pagedDomains" :key="d.id" :class="{ 'row-selected': selectedDomains.includes(d.id) }">
            <td class="checkbox-col">
              <input type="checkbox" :checked="selectedDomains.includes(d.id)" @change="toggleSelect(d)">
            </td>
            <td class="cell-truncate" :title="d.project">{{ d.project || '-' }}</td>
            <td>
              <span class="env-badge" :class="getEnvClass(d.env)">{{ d.env || 'PROD' }}</span>
            </td>
            <td class="cell-truncate" :title="d.module">{{ d.module || '-' }}</td>
            <td class="cell-truncate" :title="d.domain_name">
              <a :href="'https://' + d.domain_name" target="_blank" class="domain-link">{{ d.domain_name }}</a>
            </td>
            <td class="cell-truncate" :title="d.origin">{{ d.origin || '-' }}</td>
            <td class="cell-truncate" :title="d.origin_ip">{{ d.origin_ip || '-' }}</td>
            <td class="cell-truncate" :title="d.cdn_provider">{{ d.cdn_provider || '-' }}</td>
            <td>
              <div v-if="d.expire_time" class="expiry-badge" :class="getExpiryBadgeClass(d.expire_time)">
                <span class="expiry-date">{{ formatDate(d.expire_time) }}</span>
                <span class="expiry-days">{{ getDaysLeft(d.expire_time) > 0 ? getDaysLeft(d.expire_time) + '天' : '已过期' }}</span>
              </div>
              <span v-else class="text-muted">-</span>
            </td>
            <td>
              <div v-if="d.cert_expire_time" class="expiry-badge" :class="getExpiryBadgeClass(d.cert_expire_time)">
                <span class="expiry-date">{{ formatDate(d.cert_expire_time) }}</span>
                <span class="expiry-days">{{ getDaysLeft(d.cert_expire_time) > 0 ? getDaysLeft(d.cert_expire_time) + '天' : '已过期' }}</span>
              </div>
              <span v-else class="text-muted">-</span>
            </td>
            <td>
              <span class="status-badge" :class="d.status === 'active' ? 'status-active' : 'status-inactive'">
                {{ d.status === 'active' ? '启用' : '停用' }}
              </span>
            </td>
            <td class="cell-truncate time-cell" :title="formatDateTime(d.created_at)">{{ formatDateTime(d.created_at) }}</td>
            <td class="cell-truncate time-cell" :title="formatDateTime(d.updated_at)">{{ formatDateTime(d.updated_at) }}</td>
            <td class="cell-truncate" :title="d.remark">{{ d.remark || '-' }}</td>
            <td class="action-cell td-action-fixed">
              <button v-if="canRefresh" class="btn-icon refresh" @click="refreshDomainExpire(d)" title="刷新" :disabled="checkingDomain === d.id">
                <svg v-if="checkingDomain !== d.id" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M4 4v5h.582m15.356 2A8.001 8.001 0 004.582 9m0 0H9m11 11v-5h-.581m0 0a8.003 8.003 0 01-15.357-2m15.357 2H15"/></svg>
                <svg v-else class="spin" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><line x1="12" y1="2" x2="12" y2="6"/><line x1="12" y1="18" x2="12" y2="22"/></svg>
              </button>
              <button class="btn-icon preview" @click="openPreview(d)" title="预览">
                <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M1 12s4-8 11-8 11 8 11 8-4 8-11 8-11-8-11-8z"/><circle cx="12" cy="12" r="3"/></svg>
              </button>
              <button v-if="canEdit" class="btn-icon edit" @click="openModal(d)" title="编辑">
                <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M11 5H6a2 2 0 00-2 2v11a2 2 0 002 2h11a2 2 0 002-2v-5m-1.414-9.414a2 2 0 112.828 2.828L11.828 15H9v-2.828l8.586-8.586z"/></svg>
              </button>
              <button v-if="canDelete" class="btn-icon danger" @click="deleteDomain(d)" title="删除">
                <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M19 7l-.867 12.142A2 2 0 0116.138 21H7.862a2 2 0 01-1.995-1.858L5 7m5 4v6m4-6v6m1-10V4a1 1 0 00-1-1h-4a1 1 0 00-1 1v3M4 7h16"/></svg>
              </button>
            </td>
          </tr>
          <tr v-if="pagedDomains.length === 0 && !loading">
            <td colspan="12" class="empty">暂无数据</td>
          </tr>
        </tbody>
      </table>
    </div>
    
    <!-- 分页 -->
    <div class="pagination-bar">
      <div class="pagination-info">
        共 {{ totalCount }} 条，每页
        <select class="page-size-select" v-model="pageSize" @change="currentPage = 1">
          <option :value="10">10</option>
          <option :value="20">20</option>
          <option :value="50">50</option>
          <option :value="100">100</option>
        </select>
        条
      </div>
      <div class="pagination">
        <button @click="currentPage = 1" :disabled="currentPage <= 1">首页</button>
        <button @click="currentPage--" :disabled="currentPage <= 1">上一页</button>
        <span class="page-info">第 {{ currentPage }} / {{ totalPages }} 页</span>
        <button @click="currentPage++" :disabled="currentPage >= totalPages">下一页</button>
        <button @click="currentPage = totalPages" :disabled="currentPage >= totalPages">末页</button>
      </div>
    </div>

    <!-- 新增/编辑弹窗 -->
    <Teleport to="body">
      <div class="modal-overlay" :class="{ show: showModal }">
        <div class="domain-modal">
          <div class="domain-modal-header">
            <div class="domain-modal-title">
              <svg class="domain-modal-icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                <circle cx="12" cy="12" r="10"/>
                <path d="M2 12h20"/>
                <path d="M12 2a15.3 15.3 0 0 1 4 10 15.3 15.3 0 0 1-4 10 15.3 15.3 0 0 1-4-10 15.3 15.3 0 0 1 4-10z"/>
              </svg>
              {{ editingDomain ? '编辑域名' : '添加域名' }}
            </div>
            <button class="domain-modal-close" @click="showModal = false">
              <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M18 6L6 18M6 6l12 12"/></svg>
            </button>
          </div>

          <form @submit.prevent="saveDomain">
            <div class="domain-modal-body">
              <div class="domain-form-grid">
                <!-- 基本信息 -->
                <div class="domain-form-group">
                  <label class="domain-form-label">项目 <span class="required">*</span></label>
                  <input type="text" class="domain-form-input" v-model="formData.project" placeholder="请输入项目名称" required>
                </div>
                <div class="domain-form-group">
                  <label class="domain-form-label">模块</label>
                  <input type="text" class="domain-form-input" v-model="formData.module" placeholder="请输入模块名称">
                </div>

                <div class="domain-form-group domain-full-width">
                  <label class="domain-form-label">域名 <span class="required">*</span></label>
                  <div class="domain-input-with-btn">
                    <input type="text" class="domain-form-input" v-model="formData.domain_name" placeholder="请输入域名" required :disabled="!!editingDomain">
                    <button type="button" class="domain-input-btn" @click="checkDomainCert">
                      <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M21 21l-6-6m2-5a7 7 0 11-14 0 7 7 0 0114 0z"/></svg>
                      自动检测
                    </button>
                  </div>
                </div>

                <!-- 分割线 -->
                <div class="domain-section-divider">
                  <span class="domain-section-title">网络配置</span>
                </div>

                <div class="domain-form-group">
                  <label class="domain-form-label">回源地址</label>
                  <input type="text" class="domain-form-input" v-model="formData.origin" placeholder="CNAME 或 IP 地址">
                </div>
                <div class="domain-form-group">
                  <label class="domain-form-label">源站 IP</label>
                  <input type="text" class="domain-form-input" v-model="formData.origin_ip" placeholder="源站服务器 IP">
                </div>

                <div class="domain-form-group domain-full-width">
                  <label class="domain-form-label">CDN 厂商</label>
                  <div class="domain-cdn-grid">
                    <div 
                      v-for="opt in cdnOptions" 
                      :key="opt.value" 
                      class="domain-cdn-option" 
                      :class="{ active: formData.cdn_provider === opt.value }" 
                      @click="formData.cdn_provider = opt.value"
                    >{{ opt.label }}</div>
                  </div>
                </div>

                <!-- 分割线 -->
                <div class="domain-section-divider">
                  <span class="domain-section-title">有效期</span>
                </div>

                <div class="domain-form-group">
                  <label class="domain-form-label">域名到期时间</label>
                  <input type="date" class="domain-form-input" v-model="formData.expire_time">
                </div>
                <div class="domain-form-group">
                  <label class="domain-form-label">证书到期时间</label>
                  <input type="date" class="domain-form-input" v-model="formData.cert_expire_time">
                </div>

                <!-- 分割线 -->
                <div class="domain-section-divider">
                  <span class="domain-section-title">环境与状态</span>
                </div>

                <div class="domain-form-group">
                  <label class="domain-form-label">环境</label>
                  <div class="domain-env-tags">
                    <div class="domain-env-tag dev" :class="{ active: formData.env === 'DEV' }" @click="formData.env = 'DEV'">DEV</div>
                    <div class="domain-env-tag uat" :class="{ active: formData.env === 'UAT' }" @click="formData.env = 'UAT'">UAT</div>
                    <div class="domain-env-tag prod" :class="{ active: formData.env === 'PROD' }" @click="formData.env = 'PROD'">PROD</div>
                  </div>
                </div>
                <div class="domain-form-group">
                  <label class="domain-form-label">状态</label>
                  <div class="domain-status-tags">
                    <div class="domain-status-tag" :class="{ active: formData.status === 'active' }" @click="formData.status = 'active'">启用</div>
                    <div class="domain-status-tag disabled" :class="{ active: formData.status === 'inactive' }" @click="formData.status = 'inactive'">停用</div>
                  </div>
                </div>

                <div class="domain-form-group domain-full-width">
                  <label class="domain-form-label">备注</label>
                  <textarea class="domain-form-textarea" v-model="formData.remark" placeholder="输入备注信息..."></textarea>
                </div>
              </div>
            </div>

            <div class="domain-modal-footer">
              <button type="button" class="btn btn-secondary" @click="showModal = false">取消</button>
              <button type="submit" class="btn btn-primary">
                <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M5 13l4 4L19 7"/></svg>
                保存
              </button>
            </div>
          </form>
        </div>
      </div>
    </Teleport>

    <!-- 批量添加弹窗 -->
    <Teleport to="body">
      <div class="modal-overlay" :class="{ show: showBatchModal }">
        <div class="batch-modal">
          <!-- 渐变头部 -->
          <div class="batch-modal-header">
            <div class="batch-header-content">
              <div class="batch-header-icon">
                <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" width="26" height="26">
                  <path d="M12 2L2 7l10 5 10-5-10-5z"></path>
                  <path d="M2 17l10 5 10-5"></path>
                  <path d="M2 12l10 5 10-5"></path>
                </svg>
              </div>
              <div class="batch-header-text">
                <h2>批量添加域名</h2>
                <p>快速导入多个域名到系统</p>
              </div>
            </div>
            <button class="batch-modal-close" @click="showBatchModal = false">&times;</button>
          </div>

          <div class="batch-modal-body">
            <!-- 步骤指示器 -->
            <div class="batch-steps">
              <div class="batch-step">
                <span class="batch-step-num active">1</span>
                <span class="batch-step-text active">设置项目信息</span>
              </div>
              <div class="batch-step-line"></div>
              <div class="batch-step">
                <span class="batch-step-num" :class="{ active: batchDomainProject }">2</span>
                <span class="batch-step-text" :class="{ active: batchDomainProject }">输入域名</span>
              </div>
              <div class="batch-step-line"></div>
              <div class="batch-step">
                <span class="batch-step-num" :class="{ active: batchDomainRecords.length > 0 }">3</span>
                <span class="batch-step-text" :class="{ active: batchDomainRecords.length > 0 }">预览确认</span>
              </div>
            </div>

            <!-- 项目和环境 -->
            <div class="batch-form-row">
              <div class="batch-form-group">
                <label class="batch-form-label">
                  <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" width="16" height="16">
                    <path d="M3 9l9-7 9 7v11a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2z"></path>
                    <polyline points="9 22 9 12 15 12 15 22"></polyline>
                  </svg>
                  项目名称 <span class="batch-required">*</span>
                </label>
                <input type="text" class="batch-form-input" v-model="batchDomainProject" placeholder="输入项目名称，如：商城项目">
              </div>
              <div class="batch-form-group">
                <label class="batch-form-label">
                  <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" width="16" height="16">
                    <rect x="2" y="3" width="20" height="14" rx="2" ry="2"></rect>
                    <line x1="8" y1="21" x2="16" y2="21"></line>
                    <line x1="12" y1="17" x2="12" y2="21"></line>
                  </svg>
                  环境 <span class="batch-required">*</span>
                </label>
                <div class="batch-env-cards">
                  <div class="batch-env-card" :class="{ active: batchDomainEnv === 'PROD' }" @click="batchDomainEnv = 'PROD'">
                    <span class="batch-env-badge prod">PROD</span>
                    <span class="batch-env-label">生产</span>
                  </div>
                  <div class="batch-env-card" :class="{ active: batchDomainEnv === 'UAT' }" @click="batchDomainEnv = 'UAT'">
                    <span class="batch-env-badge uat">UAT</span>
                    <span class="batch-env-label">测试</span>
                  </div>
                  <div class="batch-env-card" :class="{ active: batchDomainEnv === 'DEV' }" @click="batchDomainEnv = 'DEV'">
                    <span class="batch-env-badge dev">DEV</span>
                    <span class="batch-env-label">开发</span>
                  </div>
                </div>
              </div>
            </div>

            <!-- 域名输入 -->
            <div class="batch-form-group" style="margin-bottom: 24px;">
              <label class="batch-form-label" style="display: flex; justify-content: space-between;">
                <span style="display: flex; align-items: center; gap: 8px;">
                  <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" width="16" height="16">
                    <circle cx="12" cy="12" r="10"></circle>
                    <line x1="2" y1="12" x2="22" y2="12"></line>
                    <path d="M12 2a15.3 15.3 0 0 1 4 10 15.3 15.3 0 0 1-4 10 15.3 15.3 0 0 1-4-10 15.3 15.3 0 0 1 4-10z"></path>
                  </svg>
                  域名列表 <span class="batch-required">*</span>
                </span>
                <span class="batch-domain-count">已识别 <strong>{{ batchDomainRecords.length }}</strong> 个域名</span>
              </label>
              <div class="batch-format-hint">
                <div class="batch-format-title">支持格式：</div>
                <div class="batch-format-content">
                  <span>• 简单：<code>example.com</code></span>
                  <span>• 完整：<code>模块,域名,回源,源站IP,CDN厂商,到期日,备注</code></span>
                </div>
              </div>
              <textarea class="batch-textarea" v-model="batchDomainText" @input="parseBatchDomains" rows="6"
                placeholder="example.com&#10;API模块,api.example.com,origin.com,192.168.1.1,阿里云CDN,2025-12-31,生产环境&#10;Web模块,www.example.com"></textarea>
              <div v-if="batchDomainError" class="batch-error">{{ batchDomainError }}</div>
            </div>

            <!-- 到期时间获取方式 -->
            <div class="batch-form-group" style="margin-bottom: 24px;">
              <label class="batch-form-label">
                <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" width="16" height="16">
                  <circle cx="12" cy="12" r="10"></circle>
                  <polyline points="12 6 12 12 16 14"></polyline>
                </svg>
                到期时间获取方式
              </label>
              <div class="batch-option-cards">
                <div class="batch-option-card" :class="{ active: batchDomainFetchExpiry === 'skip' }" @click="batchDomainFetchExpiry = 'skip'">
                  <div class="batch-option-icon fast">
                    <svg viewBox="0 0 24 24" fill="none" stroke="white" stroke-width="2" width="20" height="20">
                      <polygon points="13 2 3 14 12 14 11 22 21 10 12 10 13 2"></polygon>
                    </svg>
                  </div>
                  <div class="batch-option-text">
                    <h4>快速添加</h4>
                    <p>跳过到期查询，后续手动刷新</p>
                    <span class="batch-option-tag">推荐</span>
                  </div>
                </div>
                <div class="batch-option-card" :class="{ active: batchDomainFetchExpiry === 'fetch' }" @click="batchDomainFetchExpiry = 'fetch'">
                  <div class="batch-option-icon auto">
                    <svg viewBox="0 0 24 24" fill="none" stroke="white" stroke-width="2" width="20" height="20">
                      <circle cx="11" cy="11" r="8"></circle>
                      <line x1="21" y1="21" x2="16.65" y2="16.65"></line>
                    </svg>
                  </div>
                  <div class="batch-option-text">
                    <h4>自动获取</h4>
                    <p>自动查询到期时间，约1-2秒/个</p>
                  </div>
                </div>
              </div>
            </div>

            <!-- 预览表格 -->
            <div class="batch-preview-section" v-if="batchDomainRecords.length > 0">
              <div class="batch-preview-header">
                <span class="batch-preview-title">
                  <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" width="16" height="16">
                    <path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8z"></path>
                    <polyline points="14 2 14 8 20 8"></polyline>
                  </svg>
                  预览数据
                </span>
                <span class="batch-preview-count">{{ batchDomainRecords.length }} 条</span>
              </div>
              <div class="batch-preview-table">
                <table>
                  <thead>
                    <tr>
                      <th>域名</th>
                      <th>环境</th>
                      <th>模块</th>
                      <th>回源</th>
                      <th>CDN</th>
                    </tr>
                  </thead>
                  <tbody>
                    <tr v-for="(record, idx) in batchDomainRecords.slice(0, 10)" :key="idx">
                      <td class="mono">{{ record.domain_name }}</td>
                      <td><span :class="['batch-env-badge', getEnvClass(record.env)]">{{ record.env }}</span></td>
                      <td>{{ record.module || '-' }}</td>
                      <td>{{ record.origin || '-' }}</td>
                      <td>{{ record.cdn_provider || '-' }}</td>
                    </tr>
                    <tr v-if="batchDomainRecords.length > 10">
                      <td colspan="5" class="text-muted" style="text-align: center;">
                        ... 还有 {{ batchDomainRecords.length - 10 }} 条记录
                      </td>
                    </tr>
                  </tbody>
                </table>
              </div>
            </div>
          </div>

          <!-- 底部 -->
          <div class="batch-modal-footer">
            <div class="batch-footer-info" v-if="batchDomainProject">
              将添加到 <strong>{{ batchDomainProject }}</strong> / <span :class="['batch-env-badge', getEnvClass(batchDomainEnv)]">{{ batchDomainEnv }}</span>
            </div>
            <div class="batch-footer-actions">
              <button class="btn btn-secondary" @click="showBatchModal = false">取消</button>
              <button class="btn btn-primary" @click="submitBatchDomains" :disabled="batchDomainRecords.length === 0 || batchDomainLoading">
                <svg v-if="batchDomainLoading" class="spin" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><circle cx="12" cy="12" r="10" stroke-dasharray="60" stroke-dashoffset="20"/></svg>
                <svg v-else viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M12 5v14M5 12h14"/></svg>
                添加 {{ batchDomainRecords.length }} 个域名
              </button>
            </div>
          </div>
        </div>
      </div>
    </Teleport>
    <!-- CDN 厂商管理弹窗 -->
    <Teleport to="body">
      <div class="modal-overlay" :class="{ show: showCdnManager }">
        <div class="domain-modal cdn-manager-modal">
          <div class="domain-modal-header">
            <div class="domain-modal-title">
              <svg class="domain-modal-icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M12 2L2 7l10 5 10-5-10-5z"/><path d="M2 17l10 5 10-5"/><path d="M2 12l10 5 10-5"/></svg>
              CDN 厂商配置
            </div>
            <button class="domain-modal-close" @click="showCdnManager = false">
              <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M18 6L6 18M6 6l12 12"/></svg>
            </button>
          </div>
          <div class="domain-modal-body">
            <!-- 添加区 -->
            <div class="cdn-add-row">
              <input type="text" class="cdn-add-input" v-model="newCdnValue" placeholder="CDN 标识，如：cf" @keyup.enter="addCdnProvider">
              <input type="text" class="cdn-add-input" v-model="newCdnLabel" placeholder="显示名，如：Cloudflare" @keyup.enter="addCdnProvider">
              <button type="button" class="btn btn-primary btn-sm" @click="addCdnProvider">
                <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" style="width:14px;height:14px;"><line x1="12" y1="5" x2="12" y2="19"/><line x1="5" y1="12" x2="19" y2="12"/></svg>
                添加
              </button>
            </div>
            <!-- 列表区 -->
            <div class="cdn-list">
              <div class="cdn-list-header">
                <span class="cdn-col-name">显示名</span>
                <span class="cdn-col-value">标识值</span>
                <span class="cdn-col-action">操作</span>
              </div>
              <div v-for="(item, idx) in cdnManagerList" :key="idx" class="cdn-list-item">
                <span class="cdn-col-name">{{ item.label || item.value }}</span>
                <span class="cdn-col-value"><code>{{ item.value }}</code></span>
                <span class="cdn-col-action">
                  <button type="button" class="btn-icon danger" @click="removeCdnProvider(idx)" title="删除" style="width:26px;height:26px;">
                    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" style="width:13px;height:13px;"><path d="M19 7l-.867 12.142A2 2 0 0116.138 21H7.862a2 2 0 01-1.995-1.858L5 7m5 4v6m4-6v6m1-10V4a1 1 0 00-1-1h-4a1 1 0 00-1 1v3M4 7h16"/></svg>
                  </button>
                </span>
              </div>
              <div v-if="cdnManagerList.length === 0" class="cdn-list-empty">
                暂无 CDN 厂商，请添加
              </div>
            </div>
          </div>
          <div class="domain-modal-footer">
            <span class="cdn-count">共 {{ cdnManagerList.length }} 个厂商</span>
            <div style="display:flex;gap:8px;">
              <button type="button" class="btn btn-secondary" @click="showCdnManager = false">取消</button>
              <button type="button" class="btn btn-primary" @click="saveCdnProviders">
                <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M5 13l4 4L19 7"/></svg>
                保存
              </button>
            </div>
          </div>
        </div>
      </div>
    </Teleport>

    <!-- 批量修改弹窗 -->
    <Teleport to="body">
      <div class="modal-overlay" :class="{ show: showBatchEditModal }">
        <div class="domain-modal" style="max-width:520px;">
          <div class="domain-modal-header">
            <div class="domain-modal-title">
              <svg class="domain-modal-icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M11 5H6a2 2 0 00-2 2v11a2 2 0 002 2h11a2 2 0 002-2v-5m-1.414-9.414a2 2 0 112.828 2.828L11.828 15H9v-2.828l8.586-8.586z"/></svg>
              批量修改 ({{ selectedDomains.length }} 项)
            </div>
            <button class="domain-modal-close" @click="showBatchEditModal = false">
              <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M18 6L6 18M6 6l12 12"/></svg>
            </button>
          </div>
          <div class="domain-modal-body">
            <p style="font-size:13px;color:var(--text-muted);margin-bottom:16px;">勾选需要修改的字段，未勾选的字段将保持原值不变。</p>

            <div class="batch-edit-field">
              <label class="batch-edit-check">
                <input type="checkbox" v-model="batchEditFields.origin">
                <span>回源地址</span>
              </label>
              <input type="text" class="domain-form-input" v-model="batchEditData.origin" placeholder="新的回源地址" :disabled="!batchEditFields.origin">
            </div>

            <div class="batch-edit-field">
              <label class="batch-edit-check">
                <input type="checkbox" v-model="batchEditFields.origin_ip">
                <span>源站 IP</span>
              </label>
              <input type="text" class="domain-form-input" v-model="batchEditData.origin_ip" placeholder="新的源站IP" :disabled="!batchEditFields.origin_ip">
            </div>

            <div class="batch-edit-field">
              <label class="batch-edit-check">
                <input type="checkbox" v-model="batchEditFields.cdn_provider">
                <span>CDN 厂商</span>
              </label>
              <div class="domain-cdn-grid" :class="{ disabled: !batchEditFields.cdn_provider }">
                <div
                  v-for="opt in cdnOptions"
                  :key="opt.value"
                  class="domain-cdn-option"
                  :class="{ active: batchEditData.cdn_provider === opt.value, disabled: !batchEditFields.cdn_provider }"
                  @click="batchEditFields.cdn_provider && (batchEditData.cdn_provider = opt.value)"
                >{{ opt.label }}</div>
              </div>
            </div>

            <div class="batch-edit-field">
              <label class="batch-edit-check">
                <input type="checkbox" v-model="batchEditFields.status">
                <span>状态</span>
              </label>
              <div class="domain-cdn-grid" :class="{ disabled: !batchEditFields.status }">
                <div class="domain-cdn-option" :class="{ active: batchEditData.status === 'active', disabled: !batchEditFields.status }" @click="batchEditFields.status && (batchEditData.status = 'active')">启用</div>
                <div class="domain-cdn-option" :class="{ active: batchEditData.status === 'inactive', disabled: !batchEditFields.status }" @click="batchEditFields.status && (batchEditData.status = 'inactive')">停用</div>
              </div>
            </div>
          </div>
          <div class="domain-modal-footer">
            <button type="button" class="btn btn-secondary" @click="showBatchEditModal = false">取消</button>
            <button type="button" class="btn btn-primary" @click="submitBatchEdit" :disabled="batchEditLoading">
              <svg v-if="batchEditLoading" class="spin" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><circle cx="12" cy="12" r="10" stroke-dasharray="60" stroke-dashoffset="20"/></svg>
              <svg v-else viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M5 13l4 4L19 7"/></svg>
              确认修改
            </button>
          </div>
        </div>
      </div>
    </Teleport>

    <!-- 预览弹窗 -->
    <Teleport to="body">
      <div class="modal-overlay" :class="{ show: showPreview }" @click.self="closePreview">
        <div v-if="previewDomain" class="preview-dialog">
          <div class="preview-header">
            <h3 class="preview-title">域名详情</h3>
            <button class="domain-modal-close" @click="closePreview">
              <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><line x1="18" y1="6" x2="6" y2="18"/><line x1="6" y1="6" x2="18" y2="18"/></svg>
            </button>
          </div>
          <div class="preview-body">
            <div class="preview-grid">
              <div class="preview-item">
                <span class="preview-label">项目</span>
                <span class="preview-value">{{ previewDomain.project || '-' }}</span>
              </div>
              <div class="preview-item">
                <span class="preview-label">环境</span>
                <span class="preview-value"><span class="env-badge" :class="getEnvClass(previewDomain.env)">{{ previewDomain.env || 'PROD' }}</span></span>
              </div>
              <div class="preview-item">
                <span class="preview-label">模块</span>
                <span class="preview-value">{{ previewDomain.module || '-' }}</span>
              </div>
              <div class="preview-item">
                <span class="preview-label">状态</span>
                <span class="preview-value">
                  <span class="status-badge" :class="previewDomain.status === 'active' ? 'status-active' : 'status-inactive'">
                    {{ previewDomain.status === 'active' ? '启用' : '停用' }}
                  </span>
                </span>
              </div>
              <div class="preview-item full">
                <span class="preview-label">域名</span>
                <span class="preview-value mono">{{ previewDomain.domain_name || '-' }}</span>
              </div>
              <div class="preview-item full">
                <span class="preview-label">回源</span>
                <span class="preview-value mono">{{ previewDomain.origin || '-' }}</span>
              </div>
              <div class="preview-item">
                <span class="preview-label">源站IP</span>
                <span class="preview-value mono">{{ previewDomain.origin_ip || '-' }}</span>
              </div>
              <div class="preview-item">
                <span class="preview-label">CDN厂商</span>
                <span class="preview-value">{{ previewDomain.cdn_provider || '-' }}</span>
              </div>
              <div class="preview-item">
                <span class="preview-label">域名到期</span>
                <span class="preview-value">
                  <template v-if="previewDomain.expire_time">
                    <span class="expiry-badge" :class="getExpiryBadgeClass(previewDomain.expire_time)">
                      {{ formatDate(previewDomain.expire_time) }}
                      <span class="expiry-days">{{ getDaysLeft(previewDomain.expire_time) > 0 ? getDaysLeft(previewDomain.expire_time) + '天' : '已过期' }}</span>
                    </span>
                  </template>
                  <template v-else>-</template>
                </span>
              </div>
              <div class="preview-item">
                <span class="preview-label">证书到期</span>
                <span class="preview-value">
                  <template v-if="previewDomain.cert_expire_time">
                    <span class="expiry-badge" :class="getExpiryBadgeClass(previewDomain.cert_expire_time)">
                      {{ formatDate(previewDomain.cert_expire_time) }}
                      <span class="expiry-days">{{ getDaysLeft(previewDomain.cert_expire_time) > 0 ? getDaysLeft(previewDomain.cert_expire_time) + '天' : '已过期' }}</span>
                    </span>
                  </template>
                  <template v-else>-</template>
                </span>
              </div>
              <div class="preview-item">
                <span class="preview-label">创建时间</span>
                <span class="preview-value">{{ formatDateTime(previewDomain.created_at) }}</span>
              </div>
              <div class="preview-item">
                <span class="preview-label">更新时间</span>
                <span class="preview-value">{{ formatDateTime(previewDomain.updated_at) }}</span>
              </div>
              <div class="preview-item full">
                <span class="preview-label">备注</span>
                <span class="preview-value">{{ previewDomain.remark || '-' }}</span>
              </div>
            </div>
          </div>
          <div class="preview-footer">
            <button v-if="canEdit" class="preview-btn preview-btn-edit" @click="closePreview(); openModal(previewDomain)">
              <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" style="width:14px;height:14px;"><path d="M11 5H6a2 2 0 00-2 2v11a2 2 0 002 2h11a2 2 0 002-2v-5m-1.414-9.414a2 2 0 112.828 2.828L11.828 15H9v-2.828l8.586-8.586z"/></svg>
              编辑
            </button>
            <button class="preview-btn preview-btn-close" @click="closePreview">关闭</button>
          </div>
        </div>
      </div>
    </Teleport>
  </div>
</template>

<style scoped>
.domains-page {
  display: flex;
  flex-direction: column;
  gap: 16px;
  width: 100%;
  max-width: 100%;
}

/* 页面头部 */
.page-header-card {
  display: flex;
  justify-content: space-between;
  align-items: center;
  background: var(--bg-card);
  padding: 16px 20px;
  border-radius: 12px;
  border: 1px solid var(--border-color);
}

.page-title {
  font-size: 16px;
  font-weight: 600;
  color: var(--text-primary);
  margin: 0;
}

.header-actions {
  display: flex;
  gap: 10px;
  flex-wrap: wrap;
}

/* 筛选栏 */
.filter-bar {
  display: flex;
  gap: 10px;
  flex-wrap: wrap;
  align-items: center;
  background: var(--bg-card);
  padding: 12px 16px;
  border-radius: 10px;
  border: 1px solid var(--border-color);
}

.search-box {
  position: relative;
  display: flex;
  align-items: center;
}

.search-icon {
  position: absolute;
  left: 12px;
  width: 16px;
  height: 16px;
  color: var(--text-muted);
  pointer-events: none;
}

.search-input {
  padding: 7px 10px 7px 34px;
  width: 200px;
  background: var(--bg-input);
  border: 1px solid var(--border-color);
  border-radius: 6px;
  color: var(--text-primary);
  font-size: 13px;
}

.filter-select {
  padding: 7px 10px;
  background: var(--bg-input);
  border: 1px solid var(--border-color);
  border-radius: 6px;
  color: var(--text-primary);
  font-size: 13px;
  min-width: 90px;
}

.filter-actions { display: flex; gap: 8px; }
.btn-search { padding: 7px 16px; border-radius: 6px; border: none; background: #3a84ff; color: #fff; font-size: 13px; font-weight: 500; cursor: pointer; transition: all 0.2s; }
.btn-search:hover { background: #2970e6; }
.btn-reset { padding: 7px 16px; border-radius: 6px; border: 1px solid var(--border-color); background: var(--bg-input); color: var(--text-primary); font-size: 13px; cursor: pointer; transition: all 0.2s; }
.btn-reset:hover { background: var(--bg-hover); }

/* 批量操作栏 */
.batch-action-bar {
  display: flex;
  gap: 10px;
  align-items: center;
  background: rgba(59, 130, 246, 0.1);
  padding: 10px 16px;
  border-radius: 8px;
  border: 1px solid rgba(59, 130, 246, 0.3);
}

.batch-info {
  font-size: 13px;
  color: var(--primary);
  font-weight: 500;
}

/* 按钮 */
.btn {
  display: inline-flex;
  align-items: center;
  gap: 5px;
  padding: 7px 12px;
  border: none;
  border-radius: 6px;
  cursor: pointer;
  font-size: 13px;
  font-weight: 500;
  transition: all 0.2s;
  white-space: nowrap;
}

.btn svg { width: 16px; height: 16px; }
.btn-primary { background: var(--primary); color: #fff; }
.btn-primary:hover { background: var(--primary-dark); }
.btn-default { background: var(--bg-hover); color: var(--text-primary); border: 1px solid var(--border-color); }
.btn-default:hover { border-color: var(--primary); color: var(--primary); }
.btn-warning { background: #f59e0b; color: #fff; }
.btn-warning:hover { background: #d97706; }
.btn-warning:disabled { opacity: 0.6; cursor: not-allowed; }
.btn-success { background: #10b981; color: #fff; }
.btn-danger { background: #ef4444; color: #fff; }
.btn-sm { padding: 6px 12px; font-size: 13px; }
.btn-secondary { background: #f1f5f9; color: #475569; border: 1px solid #e2e8f0; }

/* 表格 */
.table-container {
  background: var(--bg-card);
  border: 1px solid var(--border-color);
  border-radius: 12px;
  overflow-x: auto;
  width: 100%;
  position: relative;
}

.data-table {
  width: 100%;
  border-collapse: collapse;
  min-width: 1400px;
  table-layout: fixed;
}

.data-table.table-fixed-action {
  border: none;
}

/* 固定操作列 - 必须使用完全不透明的实心背景色 */
.th-action-fixed,
.td-action-fixed {
  position: sticky !important;
  right: 0 !important;
  min-width: 160px;
  width: 160px;
  text-align: center !important;
  white-space: nowrap;
  padding: 8px 10px !important;
}

/* 深色模式 - 使用实心颜色 */
.th-action-fixed {
  z-index: 100 !important;
  background: #252d3d !important;
  box-shadow: -8px 0 12px -4px rgba(0, 0, 0, 0.3);
}

.td-action-fixed {
  z-index: 50 !important;
  background: #1a1f2e !important;
  box-shadow: -8px 0 12px -4px rgba(0, 0, 0, 0.2);
}

.data-table tr:hover .td-action-fixed {
  background: #252d3d !important;
}

.row-selected .td-action-fixed {
  background: #1e3a5f !important;
}

/* 亮色模式 */
.light-mode .th-action-fixed {
  background: #f0f2f5 !important;
}

.light-mode .td-action-fixed {
  background: #ffffff !important;
}

.light-mode .data-table tr:hover .td-action-fixed {
  background: #f0f2f5 !important;
}

.light-mode .row-selected .td-action-fixed {
  background: #dbeafe !important;
}

.data-table th, .data-table td {
  padding: 10px 12px;
  text-align: left;
  border-bottom: 1px solid var(--border-color);
  font-size: 0.8125rem;
  overflow: hidden;
  text-overflow: ellipsis;
  max-width: 200px;
  position: relative;
  z-index: 1;
}

.data-table th {
  background: var(--bg-hover);
  font-weight: 600;
  color: var(--text-muted);
  font-size: 0.7rem;
  text-transform: uppercase;
  letter-spacing: 0.03em;
  white-space: nowrap;
}

.data-table tr:hover { background: var(--bg-hover); }
.data-table tr.row-selected { background: rgba(59, 130, 246, 0.08); }

.checkbox-col { width: 40px; text-align: center !important; }

.text-ellipsis {
  max-width: 200px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

/* 统一截断 + hover 气泡提示 */
.cell-truncate {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  position: relative;
}

.cell-truncate:hover {
  overflow: visible;
  z-index: 200;
}

.cell-truncate:hover::after {
  content: attr(title);
  position: absolute;
  left: 0;
  top: 100%;
  margin-top: 4px;
  padding: 6px 10px;
  background: #1E293B;
  color: #F1F5F9;
  border: 1px solid #475569;
  border-radius: 6px;
  font-size: 12px;
  line-height: 1.5;
  white-space: normal;
  word-break: break-all;
  max-width: 360px;
  min-width: 120px;
  box-shadow: 0 8px 24px rgba(0,0,0,0.3);
  pointer-events: none;
  z-index: 9999;
}

.light-mode .cell-truncate:hover::after {
  background: #FFFFFF;
  color: #0F172A;
  border-color: #E2E8F0;
  box-shadow: 0 8px 24px rgba(0,0,0,0.1);
}

/* 隐藏空 title 的气泡 */
.cell-truncate[title=""]::after,
.cell-truncate[title="null"]::after,
.cell-truncate[title="undefined"]::after,
.cell-truncate:not([title])::after {
  display: none;
}

.domain-link { color: var(--primary); text-decoration: none; }
.domain-link:hover { text-decoration: underline; }

.time-cell {
  font-size: 12px;
  color: var(--text-secondary);
  white-space: nowrap;
}

.remark-cell {
  max-width: 120px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  font-size: 12px;
  color: var(--text-muted);
}

/* 环境标签 */
.env-badge {
  display: inline-flex;
  padding: 2px 8px;
  border-radius: 4px;
  font-size: 11px;
  font-weight: 600;
}

.env-prod { background: #dcfce7; color: #16a34a; }
.env-uat { background: #fef3c7; color: #d97706; }
.env-dev { background: #dbeafe; color: #2563eb; }

/* 到期徽章 */
.expiry-badge {
  display: inline-flex;
  flex-direction: column;
  align-items: center;
  gap: 2px;
  padding: 6px 10px;
  border-radius: 6px;
  font-size: 12px;
  min-width: 80px;
  text-align: center;
}

.expiry-date { 
  font-weight: 600; 
  white-space: nowrap;
  display: block;
}

.expiry-days { 
  font-size: 11px; 
  opacity: 0.75; 
  white-space: nowrap;
  display: block;
}

.expiry-safe { background: rgba(16, 185, 129, 0.1); color: #10b981; }
.expiry-notice { background: rgba(59, 130, 246, 0.1); color: #3b82f6; }
.expiry-warning { background: rgba(245, 158, 11, 0.1); color: #f59e0b; }
.expiry-danger { background: rgba(239, 68, 68, 0.15); color: #ef4444; }
.expiry-expired { background: rgba(239, 68, 68, 0.2); color: #dc2626; }

/* 状态徽章 */
.status-badge {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  padding: 3px 10px;
  border-radius: 20px;
  font-size: 12px;
  font-weight: 500;
  white-space: nowrap;
}

.status-badge::before { content: ''; width: 6px; height: 6px; border-radius: 50%; }
.status-active { background: rgba(16, 185, 129, 0.1); color: #10b981; }
.status-active::before { background: #10b981; }
.status-inactive { background: rgba(107, 114, 128, 0.1); color: #6b7280; }
.status-inactive::before { background: #6b7280; }

.text-muted { color: var(--text-muted); }
.empty { text-align: center; color: var(--text-muted); padding: 40px !important; }

/* 操作列 */
.actions-cell { 
  white-space: nowrap;
  display: flex;
  gap: 4px;
  align-items: center;
  justify-content: center;
}

.btn-icon {
  width: 28px;
  height: 28px;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  border: none;
  border-radius: 6px;
  cursor: pointer;
  transition: all 0.2s;
}

/* 刷新按钮 - 蓝色 */
.btn-icon.refresh {
  background: rgba(59, 130, 246, 0.15);
  color: #3b82f6;
}
.btn-icon.refresh:hover {
  background: rgba(59, 130, 246, 0.25);
  color: #2563eb;
}

/* 预览按钮 - 紫色 */
.btn-icon.preview {
  background: rgba(139, 92, 246, 0.15);
  color: #8b5cf6;
}
.btn-icon.preview:hover {
  background: rgba(139, 92, 246, 0.25);
  color: #7c3aed;
}

/* 编辑按钮 - 绿色 */
.btn-icon.edit {
  background: rgba(16, 185, 129, 0.15);
  color: #10b981;
}
.btn-icon.edit:hover {
  background: rgba(16, 185, 129, 0.25);
  color: #059669;
}

/* 删除按钮 - 红色 */
.btn-icon.danger {
  background: rgba(239, 68, 68, 0.15);
  color: #ef4444;
}
.btn-icon.danger:hover {
  background: rgba(239, 68, 68, 0.25);
  color: #dc2626;
}

.btn-icon svg { width: 15px; height: 15px; }
.btn-icon:disabled { opacity: 0.4; cursor: not-allowed; }

/* 深色模式按钮样式（默认） */
.btn-icon {
  color: #9CA3AF;
}

.btn-icon.refresh {
  background: rgba(96, 165, 250, 0.15);
  color: #60A5FA;
}
.btn-icon.refresh:hover {
  background: #374151;
  color: #93C5FD;
}

.btn-icon.preview {
  background: rgba(167, 139, 250, 0.15);
  color: #A78BFA;
}
.btn-icon.preview:hover {
  background: #374151;
  color: #C4B5FD;
}

.btn-icon.edit {
  background: rgba(52, 211, 153, 0.15);
  color: #34D399;
}
.btn-icon.edit:hover {
  background: #374151;
  color: #6EE7B7;
}

.btn-icon.danger {
  background: rgba(248, 113, 113, 0.15);
  color: #F87171;
}
.btn-icon.danger:hover {
  background: #7F1D1D;
  color: #FCA5A5;
}

/* 亮色模式按钮样式 */
.light-mode .btn-icon.refresh {
  background: rgba(59, 130, 246, 0.15);
  color: #3b82f6;
}
.light-mode .btn-icon.refresh:hover {
  background: rgba(59, 130, 246, 0.25);
  color: #2563eb;
}

.light-mode .btn-icon.preview {
  background: rgba(139, 92, 246, 0.15);
  color: #8b5cf6;
}
.light-mode .btn-icon.preview:hover {
  background: rgba(139, 92, 246, 0.25);
  color: #7c3aed;
}

.light-mode .btn-icon.edit {
  background: rgba(16, 185, 129, 0.15);
  color: #10b981;
}
.light-mode .btn-icon.edit:hover {
  background: rgba(16, 185, 129, 0.25);
  color: #059669;
}

.light-mode .btn-icon.danger {
  background: rgba(239, 68, 68, 0.15);
  color: #ef4444;
}
.light-mode .btn-icon.danger:hover {
  background: rgba(239, 68, 68, 0.25);
  color: #dc2626;
}

.spin { animation: spin 1s linear infinite; }
@keyframes spin { to { transform: rotate(360deg); } }

/* 分页 */
.pagination-bar {
  display: flex;
  justify-content: space-between;
  align-items: center;
  flex-wrap: wrap;
  gap: 12px;
}

.pagination-info {
  color: var(--text-muted);
  font-size: 14px;
  display: flex;
  align-items: center;
  gap: 6px;
}

.page-size-select {
  padding: 4px 8px;
  border: 1px solid var(--border-color);
  border-radius: 6px;
  background: var(--bg-card);
  color: var(--text-primary);
  font-size: 13px;
  cursor: pointer;
}

.pagination {
  display: flex;
  align-items: center;
  gap: 8px;
}

.pagination button {
  padding: 6px 12px;
  background: var(--bg-card);
  border: 1px solid var(--border-color);
  border-radius: 6px;
  color: var(--text-primary);
  cursor: pointer;
  font-size: 13px;
}

.pagination button:hover:not(:disabled) { border-color: var(--primary); color: var(--primary); }
.pagination button:disabled { opacity: 0.5; cursor: not-allowed; }
.page-info { color: var(--text-muted); font-size: 13px; }
</style>

<!-- 弹窗样式 - 基础样式由全局 base.css 控制 -->
<style>
.modal-overlay.show {
  display: flex !important;
}

.domain-modal {
  background: #fff;
  border-radius: 16px;
  width: 100%;
  max-width: 680px;
  max-height: 90vh;
  overflow: hidden;
  display: flex;
  flex-direction: column;
  box-shadow: 0 20px 50px rgba(0, 0, 0, 0.3);
}

.domain-modal form {
  display: flex;
  flex-direction: column;
  overflow: hidden;
  flex: 1;
  min-height: 0;
}

.domain-modal-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 18px 24px;
  border-bottom: 1px solid #e2e8f0;
  background: linear-gradient(135deg, #f8fafc 0%, #fff 100%);
}

.domain-modal-title {
  display: flex;
  align-items: center;
  gap: 10px;
  font-size: 18px;
  font-weight: 600;
  color: #1e293b;
}

.domain-modal-icon {
  width: 24px;
  height: 24px;
  color: #3b82f6;
}

.domain-modal-close {
  width: 32px;
  height: 32px;
  display: flex;
  align-items: center;
  justify-content: center;
  background: transparent;
  border: none;
  color: #94a3b8;
  cursor: pointer;
  border-radius: 6px;
  transition: all 0.2s;
}

.domain-modal-close:hover { background: #f1f5f9; color: #475569; }
.domain-modal-close svg { width: 18px; height: 18px; }

.domain-modal-body {
  padding: 24px;
  overflow-y: auto;
  background: #fff;
  flex: 1;
  min-height: 0;
}

.domain-form-grid {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 16px;
}

.domain-form-group {
  display: flex;
  flex-direction: column;
  gap: 6px;
}

.domain-full-width {
  grid-column: 1 / -1;
}

.domain-form-label {
  font-size: 13px;
  font-weight: 500;
  color: #64748b;
}

.domain-form-label .required { color: #ef4444; }

.domain-form-input {
  padding: 10px 14px;
  background: #fff;
  border: 1px solid #e2e8f0;
  border-radius: 8px;
  color: #1e293b;
  font-size: 14px;
  transition: all 0.2s;
}

.domain-form-input:focus {
  outline: none;
  border-color: #3b82f6;
  box-shadow: 0 0 0 3px rgba(59, 130, 246, 0.1);
}

.domain-form-input:disabled {
  background: #f8fafc;
  color: #94a3b8;
}

.domain-input-with-btn {
  display: flex;
  gap: 10px;
}

.domain-input-with-btn .domain-form-input {
  flex: 1;
}

.domain-input-btn {
  display: flex;
  align-items: center;
  gap: 6px;
  padding: 0 14px;
  background: linear-gradient(135deg, #3b82f6 0%, #2563eb 100%);
  color: #fff;
  border: none;
  border-radius: 8px;
  font-size: 13px;
  font-weight: 500;
  cursor: pointer;
  white-space: nowrap;
  transition: all 0.2s;
}

.domain-input-btn:hover {
  transform: translateY(-1px);
  box-shadow: 0 4px 12px rgba(59, 130, 246, 0.3);
}

.domain-input-btn svg { width: 14px; height: 14px; }

.domain-section-divider {
  grid-column: 1 / -1;
  display: flex;
  align-items: center;
  gap: 12px;
  margin: 8px 0;
}

.domain-section-divider::before,
.domain-section-divider::after {
  content: '';
  flex: 1;
  height: 1px;
  background: #e2e8f0;
}

.domain-section-title {
  font-size: 12px;
  font-weight: 600;
  color: #94a3b8;
  text-transform: uppercase;
  letter-spacing: 0.5px;
}

.domain-cdn-grid {
  display: grid;
  grid-template-columns: repeat(4, 1fr);
  gap: 8px;
}

.domain-cdn-option {
  padding: 10px;
  text-align: center;
  border: 1px solid #e2e8f0;
  border-radius: 8px;
  font-size: 13px;
  color: #64748b;
  cursor: pointer;
  transition: all 0.2s;
}

.domain-cdn-option:hover {
  border-color: #3b82f6;
  color: #3b82f6;
}

.domain-cdn-option.active {
  background: linear-gradient(135deg, #3b82f6 0%, #2563eb 100%);
  border-color: transparent;
  color: #fff;
}

.domain-env-tags,
.domain-status-tags {
  display: flex;
  gap: 8px;
}

.domain-env-tag,
.domain-status-tag {
  padding: 8px 16px;
  border: 1px solid #e2e8f0;
  border-radius: 8px;
  font-size: 13px;
  font-weight: 500;
  cursor: pointer;
  transition: all 0.2s;
}

.domain-env-tag.dev { color: #2563eb; border-color: #dbeafe; background: #f0f9ff; }
.domain-env-tag.dev.active { background: #2563eb; border-color: #2563eb; color: #fff; }

.domain-env-tag.uat { color: #d97706; border-color: #fef3c7; background: #fffbeb; }
.domain-env-tag.uat.active { background: #d97706; border-color: #d97706; color: #fff; }

.domain-env-tag.prod { color: #16a34a; border-color: #dcfce7; background: #f0fdf4; }
.domain-env-tag.prod.active { background: #16a34a; border-color: #16a34a; color: #fff; }

.domain-status-tag { color: #10b981; border-color: #d1fae5; background: #ecfdf5; }
.domain-status-tag.active { background: #10b981; border-color: #10b981; color: #fff; }

.domain-status-tag.disabled { color: #6b7280; border-color: #e5e7eb; background: #f9fafb; }
.domain-status-tag.disabled.active { background: #6b7280; border-color: #6b7280; color: #fff; }

.domain-form-textarea {
  padding: 10px 14px;
  background: #fff;
  border: 1px solid #e2e8f0;
  border-radius: 8px;
  color: #1e293b;
  font-size: 14px;
  font-family: inherit;
  resize: vertical;
  min-height: 80px;
  transition: all 0.2s;
}

.domain-form-textarea:focus {
  outline: none;
  border-color: #3b82f6;
  box-shadow: 0 0 0 3px rgba(59, 130, 246, 0.1);
}

.domain-modal-footer {
  display: flex;
  justify-content: flex-end;
  gap: 12px;
  padding: 16px 24px;
  border-top: 1px solid #e2e8f0;
  background: #f8fafc;
}

.domain-modal-footer .btn {
  padding: 10px 20px;
}

.domain-modal-footer .btn-primary {
  background: linear-gradient(135deg, #3b82f6 0%, #2563eb 100%);
}

.domain-modal-footer .btn-primary:hover {
  transform: translateY(-1px);
  box-shadow: 0 4px 12px rgba(59, 130, 246, 0.4);
}

/* 批量添加弹窗样式 */
.batch-modal {
  background: #fff;
  border-radius: 16px;
  width: 720px;
  max-width: 95vw;
  max-height: 90vh;
  overflow: hidden;
  display: flex;
  flex-direction: column;
  box-shadow: 0 25px 50px -12px rgba(0, 0, 0, 0.25);
}

.batch-modal-header {
  background: linear-gradient(135deg, #667eea 0%, #764ba2 100%);
  color: white;
  padding: 24px 28px;
  display: flex;
  align-items: center;
  justify-content: space-between;
}

.batch-header-content {
  display: flex;
  align-items: center;
  gap: 14px;
}

.batch-header-icon {
  width: 48px;
  height: 48px;
  background: rgba(255,255,255,0.2);
  border-radius: 12px;
  display: flex;
  align-items: center;
  justify-content: center;
  color: white;
}

.batch-header-text h2 {
  font-size: 20px;
  font-weight: 600;
  margin: 0;
}

.batch-header-text p {
  font-size: 14px;
  opacity: 0.85;
  margin: 4px 0 0 0;
}

.batch-modal-close {
  width: 36px;
  height: 36px;
  background: rgba(255,255,255,0.15);
  border: none;
  border-radius: 8px;
  color: white;
  font-size: 24px;
  cursor: pointer;
  display: flex;
  align-items: center;
  justify-content: center;
  transition: background 0.2s;
}

.batch-modal-close:hover {
  background: rgba(255,255,255,0.25);
}

.batch-modal-body {
  padding: 28px;
  overflow-y: auto;
  flex: 1;
}

.batch-steps {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-bottom: 28px;
  padding: 14px 18px;
  background: #f1f5f9;
  border-radius: 10px;
}

.batch-step {
  display: flex;
  align-items: center;
  gap: 8px;
}

.batch-step-num {
  width: 28px;
  height: 28px;
  border-radius: 50%;
  display: flex;
  align-items: center;
  justify-content: center;
  font-size: 13px;
  font-weight: 600;
  background: #e2e8f0;
  color: #64748b;
}

.batch-step-num.active {
  background: linear-gradient(135deg, #667eea 0%, #764ba2 100%);
  color: white;
}

.batch-step-text {
  font-size: 13px;
  color: #64748b;
}

.batch-step-text.active {
  color: #1e293b;
  font-weight: 500;
}

.batch-step-line {
  flex: 1;
  height: 2px;
  background: #e2e8f0;
}

.batch-form-row {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 20px;
  margin-bottom: 24px;
}

.batch-form-group {
  margin-bottom: 0;
}

.batch-form-label {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-bottom: 10px;
  font-size: 14px;
  font-weight: 500;
  color: #1e293b;
}

.batch-form-label svg {
  color: #667eea;
}

.batch-required {
  color: #ef4444;
}

.batch-form-input {
  width: 100%;
  height: 44px;
  padding: 0 14px;
  border: 1px solid #e2e8f0;
  border-radius: 10px;
  font-size: 14px;
  color: #1e293b;
  background: #fff;
  transition: all 0.2s;
}

.batch-form-input:focus {
  outline: none;
  border-color: #667eea;
  box-shadow: 0 0 0 3px rgba(102, 126, 234, 0.15);
}

.batch-env-cards {
  display: flex;
  gap: 10px;
}

.batch-env-card {
  flex: 1;
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 6px;
  padding: 14px 10px;
  border: 2px solid #e2e8f0;
  border-radius: 12px;
  cursor: pointer;
  transition: all 0.2s ease;
  background: #fff;
}

.batch-env-card:hover {
  border-color: #667eea;
  background: rgba(102, 126, 234, 0.05);
}

.batch-env-card.active {
  border-color: #667eea;
  background: linear-gradient(135deg, rgba(102, 126, 234, 0.1) 0%, rgba(118, 75, 162, 0.1) 100%);
  box-shadow: 0 0 0 3px rgba(102, 126, 234, 0.15);
}

.batch-env-badge {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  padding: 4px 10px;
  border-radius: 6px;
  font-size: 12px;
  font-weight: 700;
  letter-spacing: 0.5px;
}

.batch-env-badge.prod, .batch-env-badge.env-prod {
  background: linear-gradient(135deg, #10b981 0%, #059669 100%);
  color: white;
}

.batch-env-badge.uat, .batch-env-badge.env-uat {
  background: linear-gradient(135deg, #f59e0b 0%, #d97706 100%);
  color: white;
}

.batch-env-badge.dev, .batch-env-badge.env-dev {
  background: linear-gradient(135deg, #3b82f6 0%, #2563eb 100%);
  color: white;
}

.batch-env-label {
  font-size: 12px;
  color: #64748b;
}

.batch-format-hint {
  background: linear-gradient(135deg, rgba(102, 126, 234, 0.08) 0%, rgba(118, 75, 162, 0.08) 100%);
  border: 1px solid rgba(102, 126, 234, 0.2);
  border-radius: 10px;
  padding: 14px 16px;
  margin-bottom: 14px;
}

.batch-format-title {
  font-size: 13px;
  font-weight: 600;
  color: #1e293b;
  margin-bottom: 8px;
}

.batch-format-content {
  display: flex;
  gap: 20px;
  flex-wrap: wrap;
  font-size: 12px;
  color: #64748b;
}

.batch-format-content code {
  background: #f1f5f9;
  padding: 3px 8px;
  border-radius: 5px;
  font-size: 11px;
  font-family: 'Monaco', 'Menlo', monospace;
  color: #f59e0b;
}

.batch-textarea {
  width: 100%;
  padding: 14px;
  border: 1px solid #e2e8f0;
  border-radius: 10px;
  font-size: 14px;
  color: #1e293b;
  background: #fff;
  font-family: 'Monaco', 'Menlo', 'Consolas', monospace;
  line-height: 1.6;
  resize: vertical;
  transition: all 0.2s;
}

.batch-textarea:focus {
  outline: none;
  border-color: #667eea;
  box-shadow: 0 0 0 3px rgba(102, 126, 234, 0.15);
}

.batch-domain-count {
  font-size: 12px;
  color: #64748b;
}

.batch-domain-count strong {
  color: #667eea;
  font-weight: 600;
}

.batch-error {
  margin-top: 8px;
  padding: 10px 14px;
  background: rgba(239, 68, 68, 0.1);
  border: 1px solid rgba(239, 68, 68, 0.3);
  border-radius: 8px;
  color: #ef4444;
  font-size: 13px;
}

.batch-preview-section {
  border: 1px solid #e2e8f0;
  border-radius: 12px;
  overflow: hidden;
  margin-bottom: 20px;
}

.batch-preview-header {
  background: #f1f5f9;
  padding: 14px 18px;
  border-bottom: 1px solid #e2e8f0;
  display: flex;
  align-items: center;
  justify-content: space-between;
}

.batch-preview-title {
  font-weight: 600;
  font-size: 14px;
  color: #1e293b;
  display: flex;
  align-items: center;
  gap: 10px;
}

.batch-preview-title svg {
  color: #667eea;
}

.batch-preview-count {
  font-size: 12px;
  padding: 5px 12px;
  background: linear-gradient(135deg, #667eea 0%, #764ba2 100%);
  color: white;
  border-radius: 14px;
  font-weight: 600;
}

.batch-preview-table {
  max-height: 220px;
  overflow-y: auto;
}

.batch-preview-table table {
  width: 100%;
  border-collapse: collapse;
  font-size: 13px;
}

.batch-preview-table th {
  position: sticky;
  top: 0;
  background: #fff;
  padding: 12px 14px;
  text-align: left;
  font-weight: 600;
  color: #64748b;
  border-bottom: 1px solid #e2e8f0;
  z-index: 1;
}

.batch-preview-table td {
  padding: 12px 14px;
  border-bottom: 1px solid #e2e8f0;
  color: #1e293b;
}

.batch-preview-table td.mono {
  font-family: 'Monaco', 'Menlo', monospace;
  font-size: 12px;
}

.batch-preview-table tr:last-child td {
  border-bottom: none;
}

.batch-preview-table tr:hover {
  background: #f8fafc;
}

/* 选项卡片 */
.batch-option-cards {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 14px;
}

.batch-option-card {
  padding: 18px;
  border: 2px solid #e2e8f0;
  border-radius: 14px;
  cursor: pointer;
  transition: all 0.2s ease;
  background: #fff;
  display: flex;
  align-items: flex-start;
  gap: 14px;
}

.batch-option-card:hover {
  border-color: #667eea;
  background: rgba(102, 126, 234, 0.03);
}

.batch-option-card.active {
  border-color: #667eea;
  background: linear-gradient(135deg, rgba(102, 126, 234, 0.08) 0%, rgba(118, 75, 162, 0.08) 100%);
  box-shadow: 0 0 0 3px rgba(102, 126, 234, 0.12);
}

.batch-option-icon {
  width: 42px;
  height: 42px;
  border-radius: 10px;
  display: flex;
  align-items: center;
  justify-content: center;
  flex-shrink: 0;
}

.batch-option-icon.fast {
  background: linear-gradient(135deg, #10b981 0%, #059669 100%);
}

.batch-option-icon.auto {
  background: linear-gradient(135deg, #3b82f6 0%, #1d4ed8 100%);
}

.batch-option-text h4 {
  font-size: 15px;
  font-weight: 600;
  color: #1e293b;
  margin: 0 0 4px 0;
}

.batch-option-text p {
  font-size: 12px;
  color: #64748b;
  margin: 0;
}

.batch-option-tag {
  display: inline-block;
  margin-top: 8px;
  padding: 3px 10px;
  background: #10b981;
  color: white;
  border-radius: 5px;
  font-size: 11px;
  font-weight: 600;
}

.batch-modal-footer {
  border-top: 1px solid #e2e8f0;
  padding: 18px 28px;
  display: flex;
  justify-content: space-between;
  align-items: center;
  background: #f8fafc;
}

.batch-footer-info {
  font-size: 13px;
  color: #64748b;
}

.batch-footer-info strong {
  color: #667eea;
}

.batch-footer-actions {
  display: flex;
  gap: 12px;
}

.batch-footer-actions .btn {
  padding: 12px 24px;
  border-radius: 10px;
}

.batch-footer-actions .btn-primary {
  background: linear-gradient(135deg, #667eea 0%, #764ba2 100%);
  min-width: 160px;
  justify-content: center;
}

.batch-footer-actions .btn-primary:hover {
  transform: translateY(-1px);
  box-shadow: 0 4px 12px rgba(102, 126, 234, 0.4);
}

.batch-footer-actions .btn-primary:disabled {
  opacity: 0.6;
  cursor: not-allowed;
  transform: none;
  box-shadow: none;
}

/* 批量修改弹窗 */
.batch-edit-field {
  margin-bottom: 16px;
}
.batch-edit-check {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-bottom: 8px;
  font-size: 14px;
  font-weight: 500;
  color: #334155;
  cursor: pointer;
}
.batch-edit-check input[type="checkbox"] {
  width: 16px;
  height: 16px;
  accent-color: #3B82F6;
}
.batch-edit-field .domain-form-input:disabled {
  opacity: 0.4;
  cursor: not-allowed;
}
.batch-edit-field .domain-cdn-grid.disabled {
  opacity: 0.4;
  pointer-events: none;
}
.batch-edit-field .domain-cdn-option.disabled {
  cursor: not-allowed;
}

/* 预览弹窗 */
.preview-dialog {
  background: var(--bg-card, #1E293B);
  border: 1px solid var(--border-color, #334155);
  border-radius: 12px;
  width: 560px;
  max-width: 92vw;
  max-height: 85vh;
  overflow: hidden;
  display: flex;
  flex-direction: column;
  box-shadow: 0 20px 60px rgba(0, 0, 0, 0.4);
}

.preview-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 16px 20px;
  border-bottom: 1px solid var(--border-color);
}

.preview-title {
  font-size: 15px;
  font-weight: 600;
  color: var(--text-primary);
  margin: 0;
}

.preview-body {
  padding: 20px;
  overflow-y: auto;
  flex: 1;
}

.preview-grid {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 16px;
}

.preview-item {
  display: flex;
  flex-direction: column;
  gap: 4px;
}

.preview-item.full {
  grid-column: 1 / -1;
}

.preview-label {
  font-size: 0.7rem;
  font-weight: 600;
  color: var(--text-muted);
  text-transform: uppercase;
  letter-spacing: 0.03em;
}

.preview-value {
  font-size: 0.8125rem;
  color: var(--text-primary);
  word-break: break-all;
  line-height: 1.5;
}

.preview-value.mono {
  font-family: 'Fira Code', 'Consolas', monospace;
  font-size: 0.75rem;
  background: var(--bg-hover);
  padding: 6px 10px;
  border-radius: 6px;
  border: 1px solid var(--border-color);
}

.preview-footer {
  display: flex;
  justify-content: flex-end;
  gap: 8px;
  padding: 12px 20px;
  border-top: 1px solid var(--border-color);
}

.preview-btn {
  display: inline-flex;
  align-items: center;
  gap: 5px;
  padding: 7px 16px;
  border-radius: 6px;
  border: none;
  font-size: 13px;
  font-weight: 500;
  cursor: pointer;
  transition: all 0.2s;
}

.preview-btn-edit {
  background: var(--primary, #1E40AF);
  color: #fff;
}
.preview-btn-edit:hover {
  background: var(--primary-dark, #1E3A8A);
}

.preview-btn-close {
  background: var(--bg-tertiary, #334155);
  color: var(--text-secondary, #94A3B8);
  border: 1px solid var(--border-color, #334155);
}
.preview-btn-close:hover {
  background: var(--bg-hover, #475569);
  color: var(--text-primary, #F1F5F9);
}

.btn-secondary {
  padding: 7px 18px;
  border-radius: 8px;
  border: 1px solid var(--border-color);
  background: var(--bg-input);
  color: var(--text-secondary);
  font-size: 13px;
  cursor: pointer;
  transition: all 0.2s;
}
.btn-secondary:hover {
  background: var(--bg-hover);
  color: var(--text-primary);
}

/* CDN 管理弹窗 */
.cdn-manager-modal {
  max-width: 480px;
}

.cdn-add-row {
  display: flex;
  gap: 8px;
  margin-bottom: 16px;
}

.cdn-add-input {
  flex: 1;
  padding: 7px 10px;
  background: var(--bg-input, #1E293B);
  border: 1px solid var(--border-color, #334155);
  border-radius: 6px;
  color: var(--text-primary);
  font-size: 12px;
}
.cdn-add-input::placeholder {
  color: var(--text-muted);
}

.cdn-list {
  border: 1px solid var(--border-color, #334155);
  border-radius: 8px;
  overflow: hidden;
}

.cdn-list-header {
  display: flex;
  align-items: center;
  padding: 8px 12px;
  background: var(--bg-hover, #334155);
  font-size: 0.7rem;
  font-weight: 600;
  color: var(--text-muted);
  text-transform: uppercase;
  letter-spacing: 0.03em;
  border-bottom: 1px solid var(--border-color);
}

.cdn-list-item {
  display: flex;
  align-items: center;
  padding: 10px 12px;
  border-bottom: 1px solid var(--border-color, #334155);
  transition: background 0.15s;
}
.cdn-list-item:last-child {
  border-bottom: none;
}
.cdn-list-item:hover {
  background: var(--bg-hover, #334155);
}

.cdn-col-name {
  flex: 1;
  font-size: 13px;
  color: var(--text-primary);
  font-weight: 500;
}

.cdn-col-value {
  flex: 1;
  font-size: 12px;
  color: var(--text-secondary);
}
.cdn-col-value code {
  background: var(--bg-hover, #334155);
  padding: 2px 6px;
  border-radius: 4px;
  font-family: 'Fira Code', 'Consolas', monospace;
  font-size: 11px;
}

.cdn-col-action {
  width: 50px;
  text-align: center;
}

.cdn-list-empty {
  text-align: center;
  color: var(--text-muted);
  padding: 24px;
  font-size: 13px;
}

.cdn-count {
  font-size: 12px;
  color: var(--text-muted);
}
</style>
