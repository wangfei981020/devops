<template>
  <div class="page">
    <div class="page-head">
      <span class="page-title">K8s 网络</span>
      <span class="muted" style="margin-left:10px">Service / Istio VirtualService / Ingress / Gateway API（入口用 Istio 看 VirtualService）</span>
    </div>
    <el-card shadow="never">
      <div class="bar">
        <el-select v-model="clusterId" placeholder="选集群" style="width:200px" @change="onCluster">
          <el-option v-for="c in clusters" :key="c.id" :label="(c.display_name||c.name)+' · '+c.environment" :value="c.id" />
        </el-select>
        <el-select v-model="ns" clearable filterable placeholder="命名空间" style="width:180px" @change="load">
          <el-option v-for="n in namespaces" :key="n.name" :label="n.name" :value="n.name" />
        </el-select>
        <el-input v-model="q" clearable placeholder="搜名称" style="width:200px" @keyup.enter="load" @clear="load" />
        <el-button :icon="Search" @click="load">查询</el-button>
      </div>
      <el-tabs v-model="tab" @tab-change="load">
        <el-tab-pane label="Service" name="svc">
          <el-table :data="svcPaged" size="small" v-loading="loading">
            <el-table-column prop="namespace" label="命名空间" width="150" />
            <el-table-column prop="name" label="名称" min-width="220" />
            <el-table-column prop="type" label="类型" width="130" />
            <el-table-column prop="cluster_ip" label="ClusterIP" width="140" />
            <el-table-column prop="ports" label="端口" min-width="160" />
          </el-table>
          <Pager :total="services.length" v-model:page="svcPage" v-model:page-size="svcSize" />
          <el-empty v-if="!loading && !services.length" description="无数据，先去集群管理点「同步」" />
        </el-tab-pane>
        <el-tab-pane label="VirtualService (Istio)" name="vs">
          <el-table :data="vsPaged" size="small" v-loading="loading">
            <el-table-column prop="namespace" label="命名空间" width="150" />
            <el-table-column prop="name" label="名称" min-width="180" />
            <el-table-column prop="hosts" label="Hosts" min-width="260"><template #default="{ row }">{{ row.hosts || '—' }}</template></el-table-column>
            <el-table-column prop="gateways" label="挂载 Gateway" min-width="220"><template #default="{ row }">{{ row.gateways || '—' }}</template></el-table-column>
            <el-table-column prop="backends" label="后端(destination)" min-width="180"><template #default="{ row }">{{ row.backends || '—' }}</template></el-table-column>
          </el-table>
          <Pager :total="virtualservices.length" v-model:page="vsPage" v-model:page-size="vsSize" />
          <el-empty v-if="!loading && !virtualservices.length" description="无 VirtualService（集群未装 Istio 则为空）" />
        </el-tab-pane>
        <el-tab-pane label="Ingress" name="ing">
          <el-table :data="ingPaged" size="small" v-loading="loading">
            <el-table-column prop="namespace" label="命名空间" width="150" />
            <el-table-column prop="name" label="名称" min-width="180" />
            <el-table-column prop="hosts" label="Host" min-width="240"><template #default="{ row }">{{ row.hosts || '—' }}</template></el-table-column>
            <el-table-column prop="svc_names" label="后端 Service" min-width="180"><template #default="{ row }">{{ row.svc_names || '—' }}</template></el-table-column>
            <el-table-column prop="tls" label="TLS Secret" min-width="160"><template #default="{ row }">{{ row.tls || '—' }}</template></el-table-column>
          </el-table>
          <Pager :total="ingresses.length" v-model:page="ingPage" v-model:page-size="ingSize" />
          <el-empty v-if="!loading && !ingresses.length" description="无标准 Ingress（你用 Istio 请看 VirtualService）" />
        </el-tab-pane>
        <el-tab-pane label="Gateway" name="gw">
          <el-table :data="gwPaged" size="small" v-loading="loading">
            <el-table-column prop="namespace" label="命名空间" width="150" />
            <el-table-column prop="name" label="名称" min-width="180" />
            <el-table-column prop="gateway_class" label="GatewayClass" width="160"><template #default="{ row }">{{ row.gateway_class || '—' }}</template></el-table-column>
            <el-table-column prop="listeners" label="Listeners" min-width="240"><template #default="{ row }">{{ row.listeners || '—' }}</template></el-table-column>
            <el-table-column prop="addresses" label="地址" min-width="160"><template #default="{ row }">{{ row.addresses || '—' }}</template></el-table-column>
          </el-table>
          <Pager :total="gateways.length" v-model:page="gwPage" v-model:page-size="gwSize" />
          <el-empty v-if="!loading && !gateways.length" description="无 Gateway（集群未装 Gateway API CRD 则为空）" />
        </el-tab-pane>
        <el-tab-pane label="HTTPRoute" name="route">
          <el-table :data="routePaged" size="small" v-loading="loading">
            <el-table-column prop="namespace" label="命名空间" width="150" />
            <el-table-column prop="name" label="名称" min-width="180" />
            <el-table-column prop="hostnames" label="Hostnames" min-width="240"><template #default="{ row }">{{ row.hostnames || '—' }}</template></el-table-column>
            <el-table-column prop="parents" label="挂载 Gateway" min-width="160"><template #default="{ row }">{{ row.parents || '—' }}</template></el-table-column>
            <el-table-column prop="backends" label="后端 Service" min-width="160"><template #default="{ row }">{{ row.backends || '—' }}</template></el-table-column>
          </el-table>
          <Pager :total="httproutes.length" v-model:page="routePage" v-model:page-size="routeSize" />
          <el-empty v-if="!loading && !httproutes.length" description="无 HTTPRoute" />
        </el-tab-pane>
        <el-tab-pane label="暴露面" name="expose">
          <!-- 这个 tab 回答的不是「有哪些资源」而是「谁能从外面访问到什么」：
               把 VS/Ingress/LB/NodePort 拉平成一张入口表，并给出内外网判定依据。
               判定依据必须显示——同样一个 external，依据是云 LB scheme 还是节点有公网 IP，
               可信程度差很多。 -->
          <div class="filters" style="margin-bottom:10px">
            <el-radio-group v-model="exposeOnly" size="small" @change="load">
              <el-radio-button value="">全部</el-radio-button>
              <el-radio-button value="external">只看外网</el-radio-button>
              <el-radio-button value="risky">只看有风险</el-radio-button>
            </el-radio-group>
            <span v-if="exposeSummary" class="muted">
              共 {{ exposeSummary.total }} · 外网 {{ exposeSummary.external }} · 内网 {{ exposeSummary.internal }}
              · 未判定 {{ exposeSummary.unknown }} · 高危 {{ exposeSummary.high }}
            </span>
          </div>
          <el-table :data="exposeItems" size="small" v-loading="loading" max-height="560">
            <el-table-column label="等级" width="90"><template #default="{ row }">
              <el-tag size="small" :type="row.severity === 'high' ? 'danger' : row.severity === 'medium' ? 'warning' : 'info'">
                {{ row.severity || '—' }}</el-tag>
            </template></el-table-column>
            <el-table-column prop="entry" label="入口" min-width="230" show-overflow-tooltip />
            <el-table-column prop="kind" label="类型" width="130" />
            <el-table-column label="内外网" width="100"><template #default="{ row }">
              <el-tag size="small" :type="row.exposure === 'external' ? 'danger' : row.exposure === 'internal' ? 'success' : 'info'">
                {{ { external: '外网', internal: '内网', unknown: '未判定' }[row.exposure] || row.exposure }}</el-tag>
            </template></el-table-column>
            <el-table-column prop="exposure_basis" label="判定依据" min-width="230" show-overflow-tooltip />
            <el-table-column label="TLS" width="80"><template #default="{ row }">
              <el-tag v-if="row.tls === 'no'" size="small" type="danger">无</el-tag>
              <el-tag v-else-if="row.tls === 'yes'" size="small" type="success">有</el-tag>
              <span v-else class="muted">—</span>
            </template></el-table-column>
            <el-table-column label="后端存活" width="100"><template #default="{ row }">
              <el-tag v-if="row.backend_alive === 'no'" size="small" type="danger">无实例</el-tag>
              <el-tag v-else-if="row.backend_alive === 'yes'" size="small" type="success">正常</el-tag>
              <span v-else class="muted">未知</span>
            </template></el-table-column>
            <el-table-column label="风险" min-width="280"><template #default="{ row }">
              <div v-for="(r, i) in row.risks" :key="i" class="risk-line">{{ r }}</div>
            </template></el-table-column>
          </el-table>
          <el-empty v-if="!loading && !exposeItems.length" description="没有对外入口，或该筛选条件下无结果" :image-size="60" />
        </el-tab-pane>
      </el-tabs>
    </el-card>
  </div>
</template>

<script setup>
import { ref, onMounted } from 'vue'
import { ElMessage } from 'element-plus'
import { Search } from '@element-plus/icons-vue'
import { listK8sClusters, listK8sServices, listK8sIngresses, listK8sGateways, listK8sHTTPRoutes, listK8sVirtualServices, listK8sNamespaces, exposeSurface } from '../api/cmdb'
import { usePager } from '../composables/usePager'
import Pager from '../components/Pager.vue'

const clusters = ref([]); const namespaces = ref([])
const services = ref([]); const virtualservices = ref([]); const ingresses = ref([]); const gateways = ref([]); const httproutes = ref([])
const { page: svcPage, pageSize: svcSize, paged: svcPaged } = usePager(services)
const { page: vsPage, pageSize: vsSize, paged: vsPaged } = usePager(virtualservices)
const { page: ingPage, pageSize: ingSize, paged: ingPaged } = usePager(ingresses)
const { page: gwPage, pageSize: gwSize, paged: gwPaged } = usePager(gateways)
const { page: routePage, pageSize: routeSize, paged: routePaged } = usePager(httproutes)
const clusterId = ref(null); const ns = ref(''); const q = ref(''); const tab = ref('svc'); const loading = ref(false)
const exposeItems = ref([]); const exposeSummary = ref(null); const exposeOnly = ref('')

async function onCluster() { ns.value = ''; namespaces.value = await listK8sNamespaces({ cluster_id: clusterId.value }); load() }

async function load() {
  if (!clusterId.value) return
  loading.value = true
  try {
    const p = { cluster_id: clusterId.value }
    if (ns.value) p.namespace = ns.value
    if (q.value) p.q = q.value
    if (tab.value === 'svc') services.value = await listK8sServices(p)
    else if (tab.value === 'vs') virtualservices.value = await listK8sVirtualServices(p)
    else if (tab.value === 'ing') ingresses.value = await listK8sIngresses(p)
    else if (tab.value === 'gw') gateways.value = await listK8sGateways(p)
    else if (tab.value === 'route') httproutes.value = await listK8sHTTPRoutes(p)
    else if (tab.value === 'expose') {
      // 暴露面是全集群视角，不吃命名空间/关键词筛选——入口的内外网属性由集群和云侧决定
      const r = await exposeSurface({ cluster_id: clusterId.value, only: exposeOnly.value || undefined })
      exposeItems.value = r.items || []
      exposeSummary.value = r.summary || null
    }
  } catch (e) { ElMessage.error('加载失败') } finally { loading.value = false }
}

onMounted(async () => {
  try {
    clusters.value = await listK8sClusters()
    if (clusters.value.length) { clusterId.value = clusters.value[0].id; onCluster() }
  } catch (e) { ElMessage.error('加载集群失败') }
})
</script>

<style scoped>
.page-head { margin-bottom: 14px; }
.page-title { font-size: 18px; font-weight: 600; }
.muted { color: #909399; font-size: 12px; }
.bar { display: flex; gap: 10px; align-items: center; margin-bottom: 12px; }
.risk-line { font-size: 12px; line-height: 1.6; }
.filters { display: flex; gap: 12px; align-items: center; flex-wrap: wrap; }
.muted { color: #909399; font-size: 12px; }
</style>
