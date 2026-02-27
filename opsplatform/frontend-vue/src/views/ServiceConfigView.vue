<script setup>
import { ref, computed, onMounted } from 'vue'
import { useAppStore, useAuthStore } from '@/stores'
import api from '@/api'

const appStore = useAppStore()
const authStore = useAuthStore()

const services = ref([])
const loading = ref(false)
const searchQuery = ref('')
const filterProject = ref('')
const filterEnv = ref('')
const filterType = ref('')
const projects = ref([])

const showServiceModal = ref(false)
const serviceModalMode = ref('add')
const serviceForm = ref({})

const showDepModal = ref(false)
const depModalMode = ref('add')
const depForm = ref({})
const currentService = ref(null)

const expandedServices = ref([])

const serviceTypes = [
  { value: 'web', label: '前端/Web' },
  { value: 'gateway', label: '网关/Nginx' },
  { value: 'backend', label: '后端服务' },
  { value: 'middleware', label: '中间件' },
  { value: 'database', label: '数据库' },
  { value: 'cache', label: '缓存' },
  { value: 'mq', label: '消息队列' },
  { value: 'third_party', label: '第三方服务' }
]

const depTypes = [
  { value: 'nginx', label: 'Nginx' },
  { value: 'mysql', label: 'MySQL' },
  { value: 'oracle', label: 'Oracle' },
  { value: 'tidb', label: 'TiDB' },
  { value: 'postgresql', label: 'PostgreSQL' },
  { value: 'mongodb', label: 'MongoDB' },
  { value: 'redis', label: 'Redis' },
  { value: 'elasticsearch', label: 'Elasticsearch' },
  { value: 'kafka', label: 'Kafka' },
  { value: 'rabbitmq', label: 'RabbitMQ' },
  { value: 'rocketmq', label: 'RocketMQ' },
  { value: 'nacos', label: 'Nacos' },
  { value: 'apollo', label: 'Apollo' },
  { value: 'consul', label: 'Consul' },
  { value: 'etcd', label: 'Etcd' },
  { value: 'zookeeper', label: 'Zookeeper' },
  { value: 'api', label: 'API服务' },
  { value: 'grpc', label: 'gRPC服务' },
  { value: 'other', label: '其他' }
]

const envList = [
  { value: 'prod', label: 'PROD', color: '#ea3636' },
  { value: 'uat', label: 'UAT', color: '#ff9c01' },
  { value: 'dev', label: 'DEV', color: '#3a84ff' }
]

const filteredServices = computed(() => {
  let list = services.value
  if (filterProject.value) list = list.filter(s => s.project === filterProject.value)
  if (filterEnv.value) list = list.filter(s => s.env === filterEnv.value)
  if (filterType.value) list = list.filter(s => s.service_type === filterType.value)
  if (searchQuery.value) {
    const q = searchQuery.value.toLowerCase()
    list = list.filter(s => 
      s.service_name?.toLowerCase().includes(q) ||
      s.project?.toLowerCase().includes(q) ||
      s.domain?.toLowerCase().includes(q)
    )
  }
  return list
})

const stats = computed(() => {
  const total = services.value.length
  const envCounts = { prod: 0, uat: 0, dev: 0 }
  services.value.forEach(s => { if (envCounts[s.env] !== undefined) envCounts[s.env]++ })
  const depCount = services.value.reduce((sum, s) => sum + (s.dependencies?.length || 0), 0)
  return { total, ...envCounts, depCount }
})

onMounted(() => {
  loadServices()
  loadProjects()
})

async function loadServices() {
  loading.value = true
  try {
    const res = await api.get('/api/service-configs')
    services.value = res.data || []
  } catch (e) {
    appStore.showToast('加载服务配置失败', 'error')
  } finally {
    loading.value = false
  }
}

async function loadProjects() {
  try {
    const res = await api.get('/api/service-configs/projects')
    projects.value = res.data || []
  } catch (e) { console.error(e) }
}

function getServiceTypeLabel(type) {
  return serviceTypes.find(t => t.value === type)?.label || type
}

function getDepTypeLabel(type) {
  return depTypes.find(t => t.value === type)?.label || type
}

function getEnvInfo(env) {
  return envList.find(e => e.value === env) || { label: env?.toUpperCase(), color: '#6b7280' }
}

function toggleExpand(id) {
  const idx = expandedServices.value.indexOf(id)
  if (idx === -1) expandedServices.value.push(id)
  else expandedServices.value.splice(idx, 1)
}

function openServiceModal(mode, service = null) {
  serviceModalMode.value = mode
  if (mode === 'edit' && service) {
    serviceForm.value = { ...service }
  } else {
    serviceForm.value = { project: '', service_name: '', service_type: 'backend', domain: '', port: '', env: 'prod', namespace: '', replicas: 1, image: '', remark: '', status: 'active' }
  }
  showServiceModal.value = true
}

async function saveService() {
  if (!serviceForm.value.service_name) {
    appStore.showToast('请输入服务名称', 'error')
    return
  }
  try {
    if (serviceModalMode.value === 'edit') {
      await api.put(`/api/service-configs/${serviceForm.value.id}`, serviceForm.value)
      appStore.showToast('更新成功', 'success')
    } else {
      await api.post('/api/service-configs', serviceForm.value)
      appStore.showToast('添加成功', 'success')
    }
    showServiceModal.value = false
    loadServices()
    loadProjects()
  } catch (e) {
    appStore.showToast(e.response?.data || '保存失败', 'error')
  }
}

async function deleteService(service) {
  const confirmed = await appStore.showConfirm({ type: 'danger', title: '确认删除', message: `确定要删除服务 "${service.service_name}" 及其所有依赖吗？`, confirmText: '删除', cancelText: '取消' })
  if (!confirmed) return
  try {
    await api.delete(`/api/service-configs/${service.id}`)
    appStore.showToast('删除成功', 'success')
    loadServices()
  } catch (e) {
    appStore.showToast('删除失败', 'error')
  }
}

function openDepModal(mode, service, dep = null) {
  currentService.value = service
  depModalMode.value = mode
  if (mode === 'edit' && dep) {
    depForm.value = { ...dep }
  } else {
    depForm.value = { dependency_type: 'mysql', dependency_name: '', host: '', port: '', database: '', username: '', password: '', conn_string: '', remark: '', status: 'active' }
  }
  showDepModal.value = true
}

async function saveDep() {
  if (!depForm.value.dependency_name) {
    appStore.showToast('请输入依赖名称', 'error')
    return
  }
  try {
    if (depModalMode.value === 'edit') {
      await api.put(`/api/service-configs/${currentService.value.id}/deps/${depForm.value.id}`, depForm.value)
      appStore.showToast('更新成功', 'success')
    } else {
      await api.post(`/api/service-configs/${currentService.value.id}/deps`, depForm.value)
      appStore.showToast('添加成功', 'success')
    }
    showDepModal.value = false
    loadServices()
  } catch (e) {
    appStore.showToast(e.response?.data || '保存失败', 'error')
  }
}

async function deleteDep(service, dep) {
  const confirmed = await appStore.showConfirm({ type: 'danger', title: '确认删除', message: `确定要删除依赖 "${dep.dependency_name}" 吗？`, confirmText: '删除', cancelText: '取消' })
  if (!confirmed) return
  try {
    await api.delete(`/api/service-configs/${service.id}/deps/${dep.id}`)
    appStore.showToast('删除成功', 'success')
    loadServices()
  } catch (e) {
    appStore.showToast('删除失败', 'error')
  }
}

async function viewPassword(service, dep) {
  try {
    const res = await api.get(`/api/service-configs/${service.id}/deps/${dep.id}/password`)
    appStore.showConfirm({ type: 'info', title: '密码', message: res.data.password || '(无密码)', confirmText: '关闭', showCancel: false })
  } catch (e) {
    appStore.showToast('获取密码失败', 'error')
  }
}

function clearFilters() {
  searchQuery.value = ''
  filterProject.value = ''
  filterEnv.value = ''
  filterType.value = ''
}

function getDepTypeIcon(type) {
  const icons = {
    nginx: 'N', mysql: 'M', oracle: 'O', tidb: 'T', postgresql: 'P', mongodb: 'Mo', redis: 'R',
    elasticsearch: 'ES', kafka: 'K', rabbitmq: 'RQ', rocketmq: 'RK', nacos: 'Na', apollo: 'Ap',
    consul: 'Co', etcd: 'Et', zookeeper: 'ZK', api: 'API', grpc: 'gR', other: '?'
  }
  return icons[type] || '?'
}

function getDepTypeColor(type) {
  const colors = {
    nginx: '#009639', mysql: '#4479a1', oracle: '#f80000', tidb: '#4a89dc', postgresql: '#336791',
    mongodb: '#47a248', redis: '#dc382d', elasticsearch: '#fec514', kafka: '#231f20', rabbitmq: '#ff6600',
    rocketmq: '#d77310', nacos: '#1890ff', apollo: '#ff5722', consul: '#ca2171', etcd: '#419eda',
    zookeeper: '#c4a000', api: '#6366f1', grpc: '#244c5a', other: '#6b7280'
  }
  return colors[type] || '#6b7280'
}
</script>

<template>
  <div class="service-config-page">
    <div class="page-header">
      <h2>服务配置</h2>
      <div class="header-actions">
        <button class="btn btn-primary" @click="openServiceModal('add')">
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><line x1="12" y1="5" x2="12" y2="19"/><line x1="5" y1="12" x2="19" y2="12"/></svg>
          添加服务
        </button>
      </div>
    </div>

    <div class="stats-cards">
      <div class="stat-card"><div class="stat-value">{{ stats.total }}</div><div class="stat-label">服务总数</div></div>
      <div class="stat-card prod"><div class="stat-value">{{ stats.prod }}</div><div class="stat-label">生产环境</div></div>
      <div class="stat-card uat"><div class="stat-value">{{ stats.uat }}</div><div class="stat-label">测试环境</div></div>
      <div class="stat-card dev"><div class="stat-value">{{ stats.dev }}</div><div class="stat-label">开发环境</div></div>
      <div class="stat-card deps"><div class="stat-value">{{ stats.depCount }}</div><div class="stat-label">依赖关系</div></div>
    </div>

    <div class="filter-bar">
      <input type="text" v-model="searchQuery" placeholder="搜索服务名、项目、域名..." class="search-input" />
      <select v-model="filterProject" class="filter-select">
        <option value="">所有项目</option>
        <option v-for="p in projects" :key="p" :value="p">{{ p }}</option>
      </select>
      <select v-model="filterEnv" class="filter-select">
        <option value="">所有环境</option>
        <option v-for="e in envList" :key="e.value" :value="e.value">{{ e.label }}</option>
      </select>
      <select v-model="filterType" class="filter-select">
        <option value="">所有类型</option>
        <option v-for="t in serviceTypes" :key="t.value" :value="t.value">{{ t.label }}</option>
      </select>
      <button class="btn btn-text" @click="clearFilters">重置</button>
    </div>

    <div class="services-list">
      <div v-if="loading" class="loading-state">加载中...</div>
      <div v-else-if="filteredServices.length === 0" class="empty-state">
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5"><path d="M9 5H7a2 2 0 00-2 2v12a2 2 0 002 2h10a2 2 0 002-2V7a2 2 0 00-2-2h-2M9 5a2 2 0 002 2h2a2 2 0 002-2M9 5a2 2 0 012-2h2a2 2 0 012 2"/></svg>
        <div>暂无服务配置</div>
      </div>
      <div v-else class="service-cards">
        <div v-for="service in filteredServices" :key="service.id" class="service-card" :class="{ expanded: expandedServices.includes(service.id) }">
          <div class="service-header" @click="toggleExpand(service.id)">
            <div class="service-main">
              <span class="service-type-badge" :style="{ background: getDepTypeColor(service.service_type) }">{{ getServiceTypeLabel(service.service_type) }}</span>
              <span class="service-name">{{ service.service_name }}</span>
              <span class="env-badge" :style="{ background: getEnvInfo(service.env).color }">{{ getEnvInfo(service.env).label }}</span>
            </div>
            <div class="service-info">
              <span class="info-item" v-if="service.project"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M3 7v10a2 2 0 002 2h14a2 2 0 002-2V9a2 2 0 00-2-2h-6l-2-2H5a2 2 0 00-2 2z"/></svg>{{ service.project }}</span>
              <span class="info-item" v-if="service.domain"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><circle cx="12" cy="12" r="10"/><line x1="2" y1="12" x2="22" y2="12"/><path d="M12 2a15.3 15.3 0 0 1 4 10 15.3 15.3 0 0 1-4 10 15.3 15.3 0 0 1-4-10 15.3 15.3 0 0 1 4-10z"/></svg>{{ service.domain }}</span>
              <span class="info-item" v-if="service.port"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><rect x="2" y="3" width="20" height="14" rx="2" ry="2"/><line x1="8" y1="21" x2="16" y2="21"/><line x1="12" y1="17" x2="12" y2="21"/></svg>:{{ service.port }}</span>
              <span class="dep-count" v-if="service.dependencies?.length">{{ service.dependencies.length }} 个依赖</span>
            </div>
            <div class="service-actions" @click.stop>
              <button class="action-btn" @click="openDepModal('add', service)" title="添加依赖"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><line x1="12" y1="5" x2="12" y2="19"/><line x1="5" y1="12" x2="19" y2="12"/></svg></button>
              <button class="action-btn" @click="openServiceModal('edit', service)" title="编辑"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M11 5H6a2 2 0 00-2 2v11a2 2 0 002 2h11a2 2 0 002-2v-5m-1.414-9.414a2 2 0 112.828 2.828L11.828 15H9v-2.828l8.586-8.586z"/></svg></button>
              <button class="action-btn danger" @click="deleteService(service)" title="删除"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M19 7l-.867 12.142A2 2 0 0116.138 21H7.862a2 2 0 01-1.995-1.858L5 7m5 4v6m4-6v6m1-10V4a1 1 0 00-1-1h-4a1 1 0 00-1 1v3M4 7h16"/></svg></button>
            </div>
            <svg class="expand-icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><polyline points="6 9 12 15 18 9"/></svg>
          </div>
          <div class="service-deps" v-if="expandedServices.includes(service.id)">
            <div v-if="!service.dependencies?.length" class="no-deps">暂无依赖配置，点击"+"添加</div>
            <div v-else class="deps-grid">
              <div v-for="dep in service.dependencies" :key="dep.id" class="dep-card">
                <div class="dep-type-icon" :style="{ background: getDepTypeColor(dep.dependency_type) }">{{ getDepTypeIcon(dep.dependency_type) }}</div>
                <div class="dep-content">
                  <div class="dep-name">{{ dep.dependency_name }}</div>
                  <div class="dep-details">
                    <span class="dep-type">{{ getDepTypeLabel(dep.dependency_type) }}</span>
                    <span v-if="dep.host" class="dep-host">{{ dep.host }}<span v-if="dep.port">:{{ dep.port }}</span></span>
                    <span v-if="dep.database" class="dep-db">DB: {{ dep.database }}</span>
                    <span v-if="dep.username" class="dep-user">用户: {{ dep.username }}</span>
                  </div>
                  <div class="dep-remark" v-if="dep.remark">{{ dep.remark }}</div>
                </div>
                <div class="dep-actions">
                  <button class="dep-btn" @click="viewPassword(service, dep)" title="查看密码"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><rect x="3" y="11" width="18" height="11" rx="2" ry="2"/><path d="M7 11V7a5 5 0 0 1 10 0v4"/></svg></button>
                  <button class="dep-btn" @click="openDepModal('edit', service, dep)" title="编辑"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M11 5H6a2 2 0 00-2 2v11a2 2 0 002 2h11a2 2 0 002-2v-5m-1.414-9.414a2 2 0 112.828 2.828L11.828 15H9v-2.828l8.586-8.586z"/></svg></button>
                  <button class="dep-btn danger" @click="deleteDep(service, dep)" title="删除"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M19 7l-.867 12.142A2 2 0 0116.138 21H7.862a2 2 0 01-1.995-1.858L5 7m5 4v6m4-6v6m1-10V4a1 1 0 00-1-1h-4a1 1 0 00-1 1v3M4 7h16"/></svg></button>
                </div>
              </div>
            </div>
          </div>
        </div>
      </div>
    </div>

    <!-- 服务配置弹窗 -->
    <Teleport to="body">
      <div class="modal-overlay" :class="{ active: showServiceModal }">
        <div class="modal service-modal">
          <div class="modal-header">
            <h2>{{ serviceModalMode === 'edit' ? '编辑服务' : '添加服务' }}</h2>
            <button class="modal-close" @click="showServiceModal = false"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><line x1="18" y1="6" x2="6" y2="18"/><line x1="6" y1="6" x2="18" y2="18"/></svg></button>
          </div>
          <form class="modal-form" @submit.prevent="saveService">
            <div class="modal-body">
              <div class="form-row">
                <div class="form-group"><label>项目</label><input type="text" class="form-input" v-model="serviceForm.project" placeholder="项目名称" /></div>
                <div class="form-group"><label>服务名称 *</label><input type="text" class="form-input" v-model="serviceForm.service_name" placeholder="如: user-service" required /></div>
              </div>
              <div class="form-row">
                <div class="form-group"><label>服务类型</label>
                  <select class="form-select" v-model="serviceForm.service_type">
                    <option v-for="t in serviceTypes" :key="t.value" :value="t.value">{{ t.label }}</option>
                  </select>
                </div>
                <div class="form-group"><label>环境</label>
                  <select class="form-select" v-model="serviceForm.env">
                    <option v-for="e in envList" :key="e.value" :value="e.value">{{ e.label }}</option>
                  </select>
                </div>
              </div>
              <div class="form-row">
                <div class="form-group"><label>域名</label><input type="text" class="form-input" v-model="serviceForm.domain" placeholder="域名或IP" /></div>
                <div class="form-group"><label>端口</label><input type="text" class="form-input" v-model="serviceForm.port" placeholder="端口" /></div>
              </div>
              <div class="form-row">
                <div class="form-group"><label>命名空间</label><input type="text" class="form-input" v-model="serviceForm.namespace" placeholder="K8s命名空间" /></div>
                <div class="form-group"><label>副本数</label><input type="number" class="form-input" v-model.number="serviceForm.replicas" min="0" /></div>
              </div>
              <div class="form-group"><label>镜像地址</label><input type="text" class="form-input" v-model="serviceForm.image" placeholder="Docker镜像地址" /></div>
              <div class="form-group"><label>备注</label><textarea class="form-input" v-model="serviceForm.remark" rows="2" placeholder="备注信息"></textarea></div>
            </div>
            <div class="modal-footer">
              <button type="button" class="btn btn-secondary" @click="showServiceModal = false">取消</button>
              <button type="submit" class="btn btn-primary">保存</button>
            </div>
          </form>
        </div>
      </div>
    </Teleport>

    <!-- 依赖配置弹窗 -->
    <Teleport to="body">
      <div class="modal-overlay" :class="{ active: showDepModal }">
        <div class="modal dep-modal">
          <div class="modal-header">
            <h2>{{ depModalMode === 'edit' ? '编辑依赖' : '添加依赖' }} - {{ currentService?.service_name }}</h2>
            <button class="modal-close" @click="showDepModal = false"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><line x1="18" y1="6" x2="6" y2="18"/><line x1="6" y1="6" x2="18" y2="18"/></svg></button>
          </div>
          <form class="modal-form" @submit.prevent="saveDep">
            <div class="modal-body">
              <div class="form-row">
                <div class="form-group"><label>依赖类型</label>
                  <select class="form-select" v-model="depForm.dependency_type">
                    <option v-for="t in depTypes" :key="t.value" :value="t.value">{{ t.label }}</option>
                  </select>
                </div>
                <div class="form-group"><label>依赖名称 *</label><input type="text" class="form-input" v-model="depForm.dependency_name" placeholder="如: 主数据库、用户缓存" required /></div>
              </div>
              <div class="form-row">
                <div class="form-group"><label>地址</label><input type="text" class="form-input" v-model="depForm.host" placeholder="IP或域名" /></div>
                <div class="form-group"><label>端口</label><input type="text" class="form-input" v-model="depForm.port" placeholder="端口" /></div>
              </div>
              <div class="form-row">
                <div class="form-group"><label>数据库名/ID</label><input type="text" class="form-input" v-model="depForm.database" placeholder="数据库名、Nacos ID、Apollo AppId等" /></div>
                <div class="form-group"><label>用户名</label><input type="text" class="form-input" v-model="depForm.username" placeholder="用户名" /></div>
              </div>
              <div class="form-group"><label>密码</label><input type="password" class="form-input" v-model="depForm.password" :placeholder="depModalMode === 'edit' ? '留空则不修改' : '密码'" /></div>
              <div class="form-group"><label>连接串</label><input type="text" class="form-input" v-model="depForm.conn_string" placeholder="完整连接串（可选）" /></div>
              <div class="form-group"><label>备注</label><textarea class="form-input" v-model="depForm.remark" rows="2" placeholder="备注"></textarea></div>
            </div>
            <div class="modal-footer">
              <button type="button" class="btn btn-secondary" @click="showDepModal = false">取消</button>
              <button type="submit" class="btn btn-primary">保存</button>
            </div>
          </form>
        </div>
      </div>
    </Teleport>
  </div>
</template>

<style scoped>
.service-config-page { display: flex; flex-direction: column; gap: 20px; }
.page-header { display: flex; justify-content: space-between; align-items: center; }
.page-header h2 { font-size: 20px; font-weight: 600; margin: 0; }
.header-actions { display: flex; gap: 12px; }

.btn { display: inline-flex; align-items: center; gap: 6px; padding: 10px 18px; border-radius: 8px; border: none; cursor: pointer; font-size: 14px; font-weight: 500; transition: all 0.2s; }
.btn svg { width: 16px; height: 16px; }
.btn-primary { background: var(--primary); color: white; }
.btn-primary:hover { background: #2563eb; }
.btn-secondary { background: var(--bg-hover); color: var(--text-primary); border: 1px solid var(--border-color); }
.btn-text { background: transparent; color: var(--text-muted); }
.btn-text:hover { color: var(--primary); }

.stats-cards { display: grid; grid-template-columns: repeat(5, 1fr); gap: 16px; }
.stat-card { padding: 20px; border-radius: 12px; text-align: center; background: var(--bg-card); border: 1px solid var(--border-color); }
.stat-card .stat-value { font-size: 28px; font-weight: 700; color: var(--primary); }
.stat-card .stat-label { font-size: 13px; color: var(--text-muted); margin-top: 4px; }
.stat-card.prod .stat-value { color: #ea3636; }
.stat-card.uat .stat-value { color: #ff9c01; }
.stat-card.dev .stat-value { color: #3a84ff; }
.stat-card.deps .stat-value { color: #10b981; }

.filter-bar { display: flex; gap: 12px; flex-wrap: wrap; }
.search-input { flex: 1; min-width: 200px; padding: 10px 14px; border: 1px solid var(--border-color); border-radius: 8px; background: var(--bg-card); color: var(--text-primary); font-size: 14px; }
.filter-select { padding: 10px 14px; border: 1px solid var(--border-color); border-radius: 8px; background: var(--bg-card); color: var(--text-primary); font-size: 14px; cursor: pointer; }

.services-list { }
.loading-state, .empty-state { text-align: center; padding: 60px; color: var(--text-muted); }
.empty-state svg { width: 48px; height: 48px; margin-bottom: 12px; opacity: 0.5; }

.service-cards { display: flex; flex-direction: column; gap: 12px; }
.service-card { background: var(--bg-card); border: 1px solid var(--border-color); border-radius: 12px; overflow: hidden; transition: all 0.2s; }
.service-card:hover { border-color: var(--primary); }
.service-card.expanded { border-color: var(--primary); }

.service-header { display: flex; align-items: center; gap: 16px; padding: 16px 20px; cursor: pointer; }
.service-main { display: flex; align-items: center; gap: 10px; }
.service-type-badge { padding: 4px 10px; border-radius: 6px; font-size: 11px; font-weight: 600; color: white; }
.service-name { font-size: 15px; font-weight: 600; color: var(--text-primary); }
.env-badge { padding: 3px 8px; border-radius: 4px; font-size: 10px; font-weight: 700; color: white; }

.service-info { flex: 1; display: flex; align-items: center; gap: 16px; color: var(--text-secondary); font-size: 13px; }
.info-item { display: flex; align-items: center; gap: 4px; }
.info-item svg { width: 14px; height: 14px; opacity: 0.6; }
.dep-count { background: rgba(16, 185, 129, 0.15); color: #10b981; padding: 3px 8px; border-radius: 4px; font-size: 11px; font-weight: 500; }

.service-actions { display: flex; gap: 6px; }
.action-btn { width: 32px; height: 32px; border-radius: 6px; border: none; background: var(--bg-hover); color: var(--text-secondary); cursor: pointer; display: flex; align-items: center; justify-content: center; transition: all 0.15s; }
.action-btn:hover { background: var(--primary); color: white; }
.action-btn.danger:hover { background: #ea3636; }
.action-btn svg { width: 15px; height: 15px; }

.expand-icon { width: 20px; height: 20px; color: var(--text-muted); transition: transform 0.2s; }
.service-card.expanded .expand-icon { transform: rotate(180deg); }

.service-deps { padding: 0 20px 20px; border-top: 1px solid var(--border-color); background: var(--bg-hover); }
.no-deps { text-align: center; padding: 30px; color: var(--text-muted); font-size: 13px; }
.deps-grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(320px, 1fr)); gap: 12px; padding-top: 16px; }

.dep-card { display: flex; gap: 12px; padding: 14px; background: var(--bg-card); border: 1px solid var(--border-color); border-radius: 10px; }
.dep-type-icon { width: 40px; height: 40px; border-radius: 8px; display: flex; align-items: center; justify-content: center; font-size: 12px; font-weight: 700; color: white; flex-shrink: 0; }
.dep-content { flex: 1; min-width: 0; }
.dep-name { font-size: 14px; font-weight: 600; color: var(--text-primary); margin-bottom: 4px; }
.dep-details { display: flex; flex-wrap: wrap; gap: 8px; font-size: 12px; color: var(--text-secondary); }
.dep-type { background: var(--bg-hover); padding: 2px 6px; border-radius: 4px; }
.dep-host { font-family: monospace; }
.dep-db, .dep-user { color: var(--text-muted); }
.dep-remark { font-size: 12px; color: var(--text-muted); margin-top: 6px; }

.dep-actions { display: flex; flex-direction: column; gap: 4px; }
.dep-btn { width: 26px; height: 26px; border-radius: 5px; border: none; background: var(--bg-hover); color: var(--text-secondary); cursor: pointer; display: flex; align-items: center; justify-content: center; transition: all 0.15s; }
.dep-btn:hover { background: var(--primary); color: white; }
.dep-btn.danger:hover { background: #ea3636; }
.dep-btn svg { width: 13px; height: 13px; }

@media (max-width: 1024px) { .stats-cards { grid-template-columns: repeat(3, 1fr); } }
@media (max-width: 640px) { .stats-cards { grid-template-columns: repeat(2, 1fr); } }
</style>

<style>
/* 弹窗基础样式由全局 base.css 控制 display 属性 */
.modal-overlay.active { display: flex !important; }
.modal { background: var(--bg-card); border-radius: 16px; border: 1px solid var(--border-color); max-width: 100%; max-height: 90vh; display: flex; flex-direction: column; box-shadow: 0 25px 50px rgba(0, 0, 0, 0.4); overflow: hidden; }
.service-modal, .dep-modal { width: 600px; }
.modal-header { display: flex; align-items: center; justify-content: space-between; padding: 18px 24px; border-bottom: 1px solid var(--border-color); }
.modal-header h2 { font-size: 17px; font-weight: 600; margin: 0; }
.modal-close { width: 30px; height: 30px; border-radius: 6px; border: none; background: var(--bg-hover); color: var(--text-secondary); cursor: pointer; display: flex; align-items: center; justify-content: center; }
.modal-close:hover { background: #ea3636; color: white; }
.modal-close svg { width: 16px; height: 16px; }
.modal-form { display: flex; flex-direction: column; flex: 1; overflow: hidden; }
.modal-body { padding: 20px 24px; overflow-y: auto; flex: 1; }
.modal-footer { display: flex; justify-content: flex-end; gap: 12px; padding: 16px 24px; border-top: 1px solid var(--border-color); }

.form-row { display: grid; grid-template-columns: 1fr 1fr; gap: 16px; }
.form-group { display: flex; flex-direction: column; gap: 6px; margin-bottom: 14px; }
.form-group label { font-size: 13px; font-weight: 500; color: var(--text-secondary); }
.form-input, .form-select { padding: 10px 14px; border: 1px solid var(--border-color); border-radius: 8px; background: var(--bg-hover); color: var(--text-primary); font-size: 14px; width: 100%; box-sizing: border-box; }
.form-input:focus, .form-select:focus { border-color: var(--primary); outline: none; }
textarea.form-input { resize: vertical; min-height: 60px; }
</style>
