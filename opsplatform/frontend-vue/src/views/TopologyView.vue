<script setup>
import { ref, computed, onMounted, watch, nextTick } from 'vue'
import { useAppStore } from '@/stores'
import api from '@/api'

const appStore = useAppStore()

const services = ref([])
const loading = ref(false)
const filterProject = ref('')
const filterEnv = ref('prod')
const projects = ref([])
const canvasRef = ref(null)
const svgContainer = ref(null)
const selectedNode = ref(null)

const envList = [
  { value: 'prod', label: 'PROD', color: '#ea3636' },
  { value: 'uat', label: 'UAT', color: '#ff9c01' },
  { value: 'dev', label: 'DEV', color: '#3a84ff' }
]

const serviceTypes = {
  web: { label: '前端/Web', color: '#3a84ff', icon: 'W' },
  gateway: { label: '网关/Nginx', color: '#009639', icon: 'N' },
  backend: { label: '后端服务', color: '#6366f1', icon: 'S' },
  middleware: { label: '中间件', color: '#f59e0b', icon: 'M' },
  database: { label: '数据库', color: '#ef4444', icon: 'D' },
  cache: { label: '缓存', color: '#dc382d', icon: 'C' },
  mq: { label: '消息队列', color: '#8b5cf6', icon: 'Q' },
  third_party: { label: '第三方', color: '#6b7280', icon: 'T' }
}

const depTypes = {
  nginx: { label: 'Nginx', color: '#009639', icon: 'N' },
  mysql: { label: 'MySQL', color: '#4479a1', icon: 'M' },
  oracle: { label: 'Oracle', color: '#f80000', icon: 'O' },
  tidb: { label: 'TiDB', color: '#4a89dc', icon: 'Ti' },
  postgresql: { label: 'PostgreSQL', color: '#336791', icon: 'PG' },
  mongodb: { label: 'MongoDB', color: '#47a248', icon: 'Mo' },
  redis: { label: 'Redis', color: '#dc382d', icon: 'R' },
  elasticsearch: { label: 'ES', color: '#fec514', icon: 'ES' },
  kafka: { label: 'Kafka', color: '#231f20', icon: 'K' },
  rabbitmq: { label: 'RabbitMQ', color: '#ff6600', icon: 'RQ' },
  rocketmq: { label: 'RocketMQ', color: '#d77310', icon: 'RK' },
  nacos: { label: 'Nacos', color: '#1890ff', icon: 'Na' },
  apollo: { label: 'Apollo', color: '#ff5722', icon: 'Ap' },
  consul: { label: 'Consul', color: '#ca2171', icon: 'Co' },
  etcd: { label: 'Etcd', color: '#419eda', icon: 'Et' },
  zookeeper: { label: 'Zookeeper', color: '#c4a000', icon: 'ZK' },
  api: { label: 'API服务', color: '#6366f1', icon: 'API' },
  grpc: { label: 'gRPC', color: '#244c5a', icon: 'gR' },
  other: { label: '其他', color: '#6b7280', icon: '?' }
}

const filteredServices = computed(() => {
  let list = services.value
  if (filterProject.value) list = list.filter(s => s.project === filterProject.value)
  if (filterEnv.value) list = list.filter(s => s.env === filterEnv.value)
  return list
})

const topologyData = computed(() => {
  const nodes = []
  const edges = []
  const nodeMap = new Map()

  const svcs = filteredServices.value
  
  const layers = { client: [], web: [], gateway: [], backend: [], middleware: [], infra: [] }
  
  svcs.forEach(s => {
    const nodeId = `svc-${s.id}`
    const typeInfo = serviceTypes[s.service_type] || serviceTypes.backend
    const node = {
      id: nodeId,
      type: 'service',
      data: s,
      label: s.service_name,
      sublabel: s.domain || '',
      color: typeInfo.color,
      icon: typeInfo.icon
    }
    nodes.push(node)
    nodeMap.set(nodeId, node)

    if (s.service_type === 'web') layers.web.push(node)
    else if (s.service_type === 'gateway') layers.gateway.push(node)
    else if (s.service_type === 'backend') layers.backend.push(node)
    else if (['middleware', 'mq'].includes(s.service_type)) layers.middleware.push(node)
    else if (['database', 'cache'].includes(s.service_type)) layers.infra.push(node)
    else layers.backend.push(node)

    if (s.dependencies?.length) {
      s.dependencies.forEach(dep => {
        const depKey = `${dep.dependency_type}-${dep.host}-${dep.port}`
        let depNodeId = `dep-${depKey}`
        
        if (!nodeMap.has(depNodeId)) {
          const depTypeInfo = depTypes[dep.dependency_type] || depTypes.other
          const depNode = {
            id: depNodeId,
            type: 'dependency',
            data: dep,
            label: dep.dependency_name,
            sublabel: dep.host ? `${dep.host}${dep.port ? ':' + dep.port : ''}` : '',
            color: depTypeInfo.color,
            icon: depTypeInfo.icon
          }
          nodes.push(depNode)
          nodeMap.set(depNodeId, depNode)

          if (['mysql', 'oracle', 'tidb', 'postgresql', 'mongodb', 'redis'].includes(dep.dependency_type)) {
            layers.infra.push(depNode)
          } else if (['nacos', 'apollo', 'consul', 'etcd', 'zookeeper', 'kafka', 'rabbitmq', 'rocketmq'].includes(dep.dependency_type)) {
            layers.middleware.push(depNode)
          } else if (dep.dependency_type === 'nginx') {
            layers.gateway.push(depNode)
          } else {
            layers.infra.push(depNode)
          }
        }

        edges.push({
          source: nodeId,
          target: depNodeId,
          label: dep.database || ''
        })
      })
    }
  })

  if (svcs.length > 0 && layers.web.length > 0) {
    const clientNode = { id: 'client', type: 'client', label: '客户端', sublabel: '浏览器/APP', color: '#10b981', icon: 'U' }
    nodes.push(clientNode)
    layers.client.push(clientNode)
    
    layers.web.forEach(webNode => {
      edges.push({ source: 'client', target: webNode.id, label: '' })
    })
  }

  return { nodes, edges, layers }
})

onMounted(() => {
  loadServices()
  loadProjects()
})

watch([filterProject, filterEnv], () => {
  nextTick(() => renderTopology())
})

async function loadServices() {
  loading.value = true
  try {
    const res = await api.get('/api/service-configs')
    services.value = res.data || []
    nextTick(() => renderTopology())
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

function renderTopology() {
  if (!svgContainer.value) return
  
  const { nodes, edges, layers } = topologyData.value
  if (nodes.length === 0) {
    svgContainer.value.innerHTML = ''
    return
  }

  const width = svgContainer.value.clientWidth || 1200
  const layerNames = ['client', 'web', 'gateway', 'backend', 'middleware', 'infra']
  const activeLayers = layerNames.filter(l => layers[l].length > 0)
  const layerHeight = 120
  const totalHeight = Math.max(400, activeLayers.length * layerHeight + 100)
  const nodeWidth = 140
  const nodeHeight = 60

  const nodePositions = new Map()
  let y = 50
  
  activeLayers.forEach(layerName => {
    const layerNodes = layers[layerName]
    const totalWidth = layerNodes.length * nodeWidth + (layerNodes.length - 1) * 40
    let x = (width - totalWidth) / 2
    
    layerNodes.forEach(node => {
      nodePositions.set(node.id, { x: x + nodeWidth / 2, y: y + nodeHeight / 2 })
      x += nodeWidth + 40
    })
    
    y += layerHeight
  })

  let svg = `<svg width="${width}" height="${totalHeight}" xmlns="http://www.w3.org/2000/svg">`
  
  svg += `<defs>
    <marker id="arrowhead" markerWidth="10" markerHeight="7" refX="10" refY="3.5" orient="auto">
      <polygon points="0 0, 10 3.5, 0 7" fill="#64748b"/>
    </marker>
    <filter id="shadow" x="-20%" y="-20%" width="140%" height="140%">
      <feDropShadow dx="0" dy="2" stdDeviation="3" flood-opacity="0.2"/>
    </filter>
  </defs>`

  edges.forEach(edge => {
    const source = nodePositions.get(edge.source)
    const target = nodePositions.get(edge.target)
    if (source && target) {
      const dx = target.x - source.x
      const dy = target.y - source.y
      const len = Math.sqrt(dx * dx + dy * dy)
      const offsetX = (dx / len) * 35
      const offsetY = (dy / len) * 30
      
      svg += `<line 
        x1="${source.x + offsetX}" y1="${source.y + offsetY}" 
        x2="${target.x - offsetX}" y2="${target.y - offsetY}" 
        stroke="#64748b" stroke-width="1.5" marker-end="url(#arrowhead)" opacity="0.6"/>`
      
      if (edge.label) {
        const midX = (source.x + target.x) / 2
        const midY = (source.y + target.y) / 2
        svg += `<text x="${midX}" y="${midY - 5}" text-anchor="middle" fill="#94a3b8" font-size="10">${edge.label}</text>`
      }
    }
  })

  nodes.forEach(node => {
    const pos = nodePositions.get(node.id)
    if (!pos) return
    
    const x = pos.x - nodeWidth / 2
    const y = pos.y - nodeHeight / 2
    
    svg += `<g class="topology-node" data-id="${node.id}" style="cursor: pointer;">
      <rect x="${x}" y="${y}" width="${nodeWidth}" height="${nodeHeight}" rx="10" fill="${node.color}" filter="url(#shadow)" opacity="0.95"/>
      <circle cx="${x + 25}" cy="${y + nodeHeight / 2}" r="16" fill="rgba(255,255,255,0.2)"/>
      <text x="${x + 25}" y="${y + nodeHeight / 2 + 4}" text-anchor="middle" fill="white" font-size="11" font-weight="bold">${node.icon}</text>
      <text x="${x + 50}" y="${y + 22}" fill="white" font-size="12" font-weight="600">${truncate(node.label, 10)}</text>
      <text x="${x + 50}" y="${y + 40}" fill="rgba(255,255,255,0.7)" font-size="10">${truncate(node.sublabel, 12)}</text>
    </g>`
  })

  let legendY = 20
  svg += `<g transform="translate(${width - 160}, ${legendY})">`
  const legendItems = [
    { label: '客户端', color: '#10b981' },
    { label: '前端/Web', color: '#3a84ff' },
    { label: '网关', color: '#009639' },
    { label: '后端服务', color: '#6366f1' },
    { label: '数据库/缓存', color: '#ef4444' }
  ]
  legendItems.forEach((item, i) => {
    svg += `<rect x="0" y="${i * 22}" width="14" height="14" rx="3" fill="${item.color}"/>`
    svg += `<text x="20" y="${i * 22 + 11}" fill="#94a3b8" font-size="11">${item.label}</text>`
  })
  svg += `</g>`

  svg += '</svg>'
  svgContainer.value.innerHTML = svg

  svgContainer.value.querySelectorAll('.topology-node').forEach(el => {
    el.addEventListener('click', () => {
      const nodeId = el.getAttribute('data-id')
      const node = nodes.find(n => n.id === nodeId)
      if (node) selectedNode.value = node
    })
  })
}

function truncate(str, len) {
  if (!str) return ''
  return str.length > len ? str.slice(0, len) + '...' : str
}

function closeNodeDetail() {
  selectedNode.value = null
}

function getEnvInfo(env) {
  return envList.find(e => e.value === env) || { label: env?.toUpperCase(), color: '#6b7280' }
}
</script>

<template>
  <div class="topology-page">
    <div class="page-header">
      <h2>网络拓扑</h2>
      <div class="header-controls">
        <select v-model="filterProject" class="filter-select">
          <option value="">所有项目</option>
          <option v-for="p in projects" :key="p" :value="p">{{ p }}</option>
        </select>
        <div class="env-tabs">
          <button v-for="e in envList" :key="e.value" class="env-tab" :class="{ active: filterEnv === e.value }" :style="{ '--env-color': e.color }" @click="filterEnv = e.value">{{ e.label }}</button>
        </div>
      </div>
    </div>

    <div class="topology-container">
      <div v-if="loading" class="loading-state">加载中...</div>
      <div v-else-if="filteredServices.length === 0" class="empty-state">
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5"><circle cx="12" cy="12" r="10"/><path d="M12 6v6l4 2"/></svg>
        <div>当前环境暂无服务配置</div>
        <p>请先在"服务配置"页面添加服务</p>
      </div>
      <div v-else class="topology-canvas" ref="svgContainer"></div>
    </div>

    <!-- 节点详情面板 -->
    <Teleport to="body">
      <div class="node-detail-overlay" :class="{ active: selectedNode }" @click.self="closeNodeDetail">
        <div class="node-detail-panel" v-if="selectedNode">
          <div class="panel-header" :style="{ background: selectedNode.color }">
            <div class="panel-icon">{{ selectedNode.icon }}</div>
            <div class="panel-title">
              <h3>{{ selectedNode.label }}</h3>
              <span>{{ selectedNode.sublabel }}</span>
            </div>
            <button class="panel-close" @click="closeNodeDetail">
              <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><line x1="18" y1="6" x2="6" y2="18"/><line x1="6" y1="6" x2="18" y2="18"/></svg>
            </button>
          </div>
          <div class="panel-body">
            <template v-if="selectedNode.type === 'service'">
              <div class="detail-section">
                <h4>服务信息</h4>
                <div class="detail-grid">
                  <div class="detail-item"><span class="label">项目</span><span class="value">{{ selectedNode.data.project || '-' }}</span></div>
                  <div class="detail-item"><span class="label">环境</span><span class="env-badge" :style="{ background: getEnvInfo(selectedNode.data.env).color }">{{ getEnvInfo(selectedNode.data.env).label }}</span></div>
                  <div class="detail-item"><span class="label">类型</span><span class="value">{{ serviceTypes[selectedNode.data.service_type]?.label }}</span></div>
                  <div class="detail-item"><span class="label">端口</span><span class="value">{{ selectedNode.data.port || '-' }}</span></div>
                  <div class="detail-item"><span class="label">命名空间</span><span class="value">{{ selectedNode.data.namespace || '-' }}</span></div>
                  <div class="detail-item"><span class="label">副本数</span><span class="value">{{ selectedNode.data.replicas }}</span></div>
                </div>
                <div v-if="selectedNode.data.image" class="detail-row"><span class="label">镜像</span><code>{{ selectedNode.data.image }}</code></div>
              </div>
              <div class="detail-section" v-if="selectedNode.data.dependencies?.length">
                <h4>依赖服务 ({{ selectedNode.data.dependencies.length }})</h4>
                <div class="deps-list">
                  <div v-for="dep in selectedNode.data.dependencies" :key="dep.id" class="dep-item" :style="{ borderLeftColor: depTypes[dep.dependency_type]?.color }">
                    <span class="dep-type">{{ depTypes[dep.dependency_type]?.label }}</span>
                    <span class="dep-name">{{ dep.dependency_name }}</span>
                    <span class="dep-host" v-if="dep.host">{{ dep.host }}{{ dep.port ? ':' + dep.port : '' }}</span>
                    <span class="dep-db" v-if="dep.database">{{ dep.database }}</span>
                  </div>
                </div>
              </div>
            </template>
            <template v-else-if="selectedNode.type === 'dependency'">
              <div class="detail-section">
                <h4>依赖信息</h4>
                <div class="detail-grid">
                  <div class="detail-item"><span class="label">类型</span><span class="value">{{ depTypes[selectedNode.data.dependency_type]?.label }}</span></div>
                  <div class="detail-item"><span class="label">地址</span><span class="value">{{ selectedNode.data.host || '-' }}</span></div>
                  <div class="detail-item"><span class="label">端口</span><span class="value">{{ selectedNode.data.port || '-' }}</span></div>
                  <div class="detail-item"><span class="label">数据库/ID</span><span class="value">{{ selectedNode.data.database || '-' }}</span></div>
                  <div class="detail-item"><span class="label">用户名</span><span class="value">{{ selectedNode.data.username || '-' }}</span></div>
                </div>
                <div v-if="selectedNode.data.remark" class="detail-row"><span class="label">备注</span><span>{{ selectedNode.data.remark }}</span></div>
              </div>
            </template>
            <template v-else>
              <div class="detail-section">
                <h4>客户端</h4>
                <p>代表访问系统的用户端，包括浏览器、移动APP等</p>
              </div>
            </template>
          </div>
        </div>
      </div>
    </Teleport>
  </div>
</template>

<style scoped>
.topology-page { display: flex; flex-direction: column; gap: 20px; height: 100%; }
.page-header { display: flex; justify-content: space-between; align-items: center; flex-wrap: wrap; gap: 16px; }
.page-header h2 { font-size: 20px; font-weight: 600; margin: 0; }
.header-controls { display: flex; gap: 16px; align-items: center; }

.filter-select { padding: 10px 14px; border: 1px solid var(--border-color); border-radius: 8px; background: var(--bg-card); color: var(--text-primary); font-size: 14px; cursor: pointer; }

.env-tabs { display: flex; gap: 4px; background: var(--bg-card); border-radius: 10px; padding: 4px; border: 1px solid var(--border-color); }
.env-tab { padding: 8px 16px; border: none; border-radius: 6px; background: transparent; color: var(--text-secondary); font-size: 13px; font-weight: 600; cursor: pointer; transition: all 0.2s; }
.env-tab.active { background: var(--env-color); color: white; }
.env-tab:hover:not(.active) { background: var(--bg-hover); }

.topology-container { flex: 1; background: var(--bg-card); border: 1px solid var(--border-color); border-radius: 16px; overflow: hidden; min-height: 500px; }
.loading-state, .empty-state { display: flex; flex-direction: column; align-items: center; justify-content: center; height: 100%; color: var(--text-muted); }
.empty-state svg { width: 64px; height: 64px; margin-bottom: 16px; opacity: 0.4; }
.empty-state p { font-size: 13px; margin-top: 8px; }

.topology-canvas { width: 100%; height: 100%; min-height: 500px; overflow: auto; }
</style>

<style>
.node-detail-overlay { position: fixed; top: 0; right: 0; bottom: 0; left: 0; background: rgba(0, 0, 0, 0.4); backdrop-filter: blur(2px); z-index: 10000; display: none; justify-content: flex-end; }
.node-detail-overlay.active { display: flex; }

.node-detail-panel { width: 400px; max-width: 100%; height: 100%; background: var(--bg-page); display: flex; flex-direction: column; box-shadow: -4px 0 20px rgba(0, 0, 0, 0.2); }
.panel-header { display: flex; align-items: center; gap: 14px; padding: 20px; color: white; }
.panel-icon { width: 48px; height: 48px; background: rgba(255, 255, 255, 0.2); border-radius: 12px; display: flex; align-items: center; justify-content: center; font-size: 18px; font-weight: bold; }
.panel-title { flex: 1; }
.panel-title h3 { margin: 0; font-size: 18px; font-weight: 600; }
.panel-title span { font-size: 13px; opacity: 0.8; }
.panel-close { width: 36px; height: 36px; border-radius: 8px; border: none; background: rgba(255, 255, 255, 0.2); color: white; cursor: pointer; display: flex; align-items: center; justify-content: center; }
.panel-close:hover { background: rgba(255, 255, 255, 0.3); }
.panel-close svg { width: 18px; height: 18px; }

.panel-body { flex: 1; overflow-y: auto; padding: 20px; }
.detail-section { margin-bottom: 24px; }
.detail-section h4 { font-size: 14px; font-weight: 600; color: var(--text-primary); margin: 0 0 14px 0; padding-bottom: 8px; border-bottom: 1px solid var(--border-color); }
.detail-grid { display: grid; grid-template-columns: 1fr 1fr; gap: 12px; }
.detail-item { display: flex; flex-direction: column; gap: 4px; }
.detail-item .label { font-size: 12px; color: var(--text-muted); }
.detail-item .value { font-size: 14px; color: var(--text-primary); }
.detail-row { display: flex; flex-direction: column; gap: 6px; margin-top: 12px; }
.detail-row .label { font-size: 12px; color: var(--text-muted); }
.detail-row code { font-size: 12px; padding: 8px 12px; background: var(--bg-hover); border-radius: 6px; word-break: break-all; }

.env-badge { padding: 3px 8px; border-radius: 4px; font-size: 11px; font-weight: 700; color: white; display: inline-block; }

.deps-list { display: flex; flex-direction: column; gap: 8px; }
.dep-item { display: flex; align-items: center; gap: 10px; padding: 10px 12px; background: var(--bg-card); border-radius: 8px; border-left: 3px solid; }
.dep-type { font-size: 11px; color: var(--text-muted); background: var(--bg-hover); padding: 2px 6px; border-radius: 4px; }
.dep-name { font-size: 13px; font-weight: 500; color: var(--text-primary); flex: 1; }
.dep-host { font-size: 11px; color: var(--text-secondary); font-family: monospace; }
.dep-db { font-size: 11px; color: var(--primary); }
</style>
