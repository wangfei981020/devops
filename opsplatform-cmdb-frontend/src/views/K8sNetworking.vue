<template>
  <div class="page">
    <div class="page-head">
      <span class="page-title">K8s 网络</span>
      <span class="muted" style="margin-left:10px">Service / Ingress（Ingress 关联域名+证书，阶段4 建全链路）</span>
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
          <el-table :data="services" size="small" v-loading="loading">
            <el-table-column prop="namespace" label="命名空间" width="150" />
            <el-table-column prop="name" label="名称" min-width="220" />
            <el-table-column prop="type" label="类型" width="130" />
            <el-table-column prop="cluster_ip" label="ClusterIP" width="140" />
            <el-table-column prop="ports" label="端口" min-width="160" />
          </el-table>
          <el-empty v-if="!loading && !services.length" description="无数据，先去集群管理点「同步」" />
        </el-tab-pane>
        <el-tab-pane label="Ingress" name="ing">
          <el-table :data="ingresses" size="small" v-loading="loading">
            <el-table-column prop="namespace" label="命名空间" width="150" />
            <el-table-column prop="name" label="名称" min-width="180" />
            <el-table-column prop="hosts" label="Host" min-width="240"><template #default="{ row }">{{ row.hosts || '—' }}</template></el-table-column>
            <el-table-column prop="svc_names" label="后端 Service" min-width="180"><template #default="{ row }">{{ row.svc_names || '—' }}</template></el-table-column>
            <el-table-column prop="tls" label="TLS Secret" min-width="160"><template #default="{ row }">{{ row.tls || '—' }}</template></el-table-column>
          </el-table>
          <el-empty v-if="!loading && !ingresses.length" description="无 Ingress" />
        </el-tab-pane>
        <el-tab-pane label="Gateway" name="gw">
          <el-table :data="gateways" size="small" v-loading="loading">
            <el-table-column prop="namespace" label="命名空间" width="150" />
            <el-table-column prop="name" label="名称" min-width="180" />
            <el-table-column prop="gateway_class" label="GatewayClass" width="160"><template #default="{ row }">{{ row.gateway_class || '—' }}</template></el-table-column>
            <el-table-column prop="listeners" label="Listeners" min-width="240"><template #default="{ row }">{{ row.listeners || '—' }}</template></el-table-column>
            <el-table-column prop="addresses" label="地址" min-width="160"><template #default="{ row }">{{ row.addresses || '—' }}</template></el-table-column>
          </el-table>
          <el-empty v-if="!loading && !gateways.length" description="无 Gateway（集群未装 Gateway API CRD 则为空）" />
        </el-tab-pane>
        <el-tab-pane label="HTTPRoute" name="route">
          <el-table :data="httproutes" size="small" v-loading="loading">
            <el-table-column prop="namespace" label="命名空间" width="150" />
            <el-table-column prop="name" label="名称" min-width="180" />
            <el-table-column prop="hostnames" label="Hostnames" min-width="240"><template #default="{ row }">{{ row.hostnames || '—' }}</template></el-table-column>
            <el-table-column prop="parents" label="挂载 Gateway" min-width="160"><template #default="{ row }">{{ row.parents || '—' }}</template></el-table-column>
            <el-table-column prop="backends" label="后端 Service" min-width="160"><template #default="{ row }">{{ row.backends || '—' }}</template></el-table-column>
          </el-table>
          <el-empty v-if="!loading && !httproutes.length" description="无 HTTPRoute" />
        </el-tab-pane>
      </el-tabs>
    </el-card>
  </div>
</template>

<script setup>
import { ref, onMounted } from 'vue'
import { ElMessage } from 'element-plus'
import { Search } from '@element-plus/icons-vue'
import { listK8sClusters, listK8sServices, listK8sIngresses, listK8sGateways, listK8sHTTPRoutes, listK8sNamespaces } from '../api/cmdb'

const clusters = ref([]); const namespaces = ref([])
const services = ref([]); const ingresses = ref([]); const gateways = ref([]); const httproutes = ref([])
const clusterId = ref(null); const ns = ref(''); const q = ref(''); const tab = ref('svc'); const loading = ref(false)

async function onCluster() { ns.value = ''; namespaces.value = await listK8sNamespaces({ cluster_id: clusterId.value }); load() }

async function load() {
  if (!clusterId.value) return
  loading.value = true
  try {
    const p = { cluster_id: clusterId.value }
    if (ns.value) p.namespace = ns.value
    if (q.value) p.q = q.value
    if (tab.value === 'svc') services.value = await listK8sServices(p)
    else if (tab.value === 'ing') ingresses.value = await listK8sIngresses(p)
    else if (tab.value === 'gw') gateways.value = await listK8sGateways(p)
    else httproutes.value = await listK8sHTTPRoutes(p)
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
</style>
