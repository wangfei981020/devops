<template>
  <div class="page">
    <div class="page-head">
      <span class="page-title">K8s 存储 / 伸缩</span>
      <span class="muted" style="margin-left:10px">PVC 存储卷 / HPA 自动伸缩</span>
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
      <LoadError :error="error" title="存储/伸缩数据未加载" @retry="load" />
      <el-tabs v-model="tab" @tab-change="load">
        <el-tab-pane label="PVC" name="pvc">
          <el-table :data="pvcPaged" size="small" v-loading="loading">
            <el-table-column prop="namespace" label="命名空间" width="150" />
            <el-table-column prop="name" label="名称" min-width="220" />
            <el-table-column label="状态" width="100"><template #default="{ row }">
              <el-tag size="small" :type="row.status==='Bound'?'success':'warning'">{{ row.status }}</el-tag>
            </template></el-table-column>
            <el-table-column prop="capacity" label="容量" width="100" />
            <el-table-column label="使用率" width="200"><template #default="{ row }">
              <template v-if="pu(row)">
                <div style="display:flex;align-items:center;gap:8px">
                  <el-progress :percentage="Math.min(100,Math.round(pu(row).pct))" :stroke-width="8" :color="pvcColor" :show-text="false" style="flex:1" />
                  <span :style="{color:pvcColor(pu(row).pct),fontWeight:600,fontSize:'13px',minWidth:'38px'}">{{ Math.round(pu(row).pct) }}%</span>
                </div>
                <div class="muted" style="font-size:12px">{{ pu(row).used_gi.toFixed(1) }}Gi / {{ pu(row).cap_gi.toFixed(1) }}Gi</div>
              </template>
              <span v-else class="muted">—</span>
            </template></el-table-column>
            <el-table-column prop="storage_class" label="StorageClass" width="150"><template #default="{ row }">{{ row.storage_class || '—' }}</template></el-table-column>
            <el-table-column prop="volume_name" label="PV" min-width="200"><template #default="{ row }">{{ row.volume_name || '—' }}</template></el-table-column>
          </el-table>
          <Pager :total="pvcs.length" v-model:page="pvcPage" v-model:page-size="pvcSize" />
          <el-empty v-if="!loading && !error && !pvcs.length" description="无数据，先去集群管理点「同步」" />
        </el-tab-pane>
        <el-tab-pane label="HPA" name="hpa">
          <el-table :data="hpaPaged" size="small" v-loading="loading">
            <el-table-column prop="namespace" label="命名空间" width="150" />
            <el-table-column prop="name" label="名称" min-width="200" />
            <el-table-column label="目标工作负载" min-width="220"><template #default="{ row }">{{ row.target_kind }}/{{ row.target_name }}</template></el-table-column>
            <el-table-column label="副本区间" width="120"><template #default="{ row }">{{ row.min_replicas }} - {{ row.max_replicas }}</template></el-table-column>
            <el-table-column prop="current_replicas" label="当前副本" width="100" />
          </el-table>
          <Pager :total="hpas.length" v-model:page="hpaPage" v-model:page-size="hpaSize" />
          <el-empty v-if="!loading && !error && !hpas.length" description="无 HPA" />
        </el-tab-pane>
      </el-tabs>
    </el-card>
  </div>
</template>

<script setup>
import { ref, onMounted } from 'vue'
import { usePager } from '../composables/usePager'
import Pager from '../components/Pager.vue'
import { ElMessage } from 'element-plus'
import { Search } from '@element-plus/icons-vue'
import { listK8sClusters, listK8sPVCs, listK8sHPAs, listK8sNamespaces, k8sPvcUsage } from '../api/cmdb'
import { pickDefaultCluster } from '../composables/useClusterPick'
import { useLoadState } from '../composables/useLoadState'
import { normalizeError } from '../api/http'
import LoadError from '../components/LoadError.vue'

const clusters = ref([]); const namespaces = ref([]); const pvcs = ref([]); const hpas = ref([]); const pvcUse = ref({})
const { page: pvcPage, pageSize: pvcSize, paged: pvcPaged } = usePager(pvcs)
function pvcColor(p) { return p >= 90 ? '#f56c6c' : (p >= 75 ? '#e6a23c' : '#67c23a') }
const { page: hpaPage, pageSize: hpaSize, paged: hpaPaged } = usePager(hpas)
const clusterId = ref(null); const ns = ref(''); const q = ref(''); const tab = ref('pvc')
const { loading, error, run } = useLoadState()

async function onCluster() { ns.value = ''; namespaces.value = await listK8sNamespaces({ cluster_id: clusterId.value }); load() }

async function load() {
  if (!clusterId.value) return
  // 失败落到页面 error：否则「无数据，先去集群管理点同步」会把接口故障说成"这个集群没有存储卷"
  await run(async () => {
    const p = { cluster_id: clusterId.value }
    if (ns.value) p.namespace = ns.value
    if (q.value) p.q = q.value
    if (tab.value === 'pvc') { pvcs.value = await listK8sPVCs(p); loadPvcUsage() }
    else hpas.value = await listK8sHPAs(p)
  })
  if (error.value) { pvcs.value = []; hpas.value = [] }
}
async function loadPvcUsage() {
  pvcUse.value = {}
  try { const r = await k8sPvcUsage({ cluster_id: clusterId.value }); if (r.ok) pvcUse.value = r.usage || {} } catch (e) { /* 静默 */ }
}
function pu(row) {
  const m = pvcUse.value[row.namespace + '/' + row.name]
  return (m && m.pct != null) ? m : null
}

onMounted(async () => {
  try {
    clusters.value = await listK8sClusters()
    if (clusters.value.length) { clusterId.value = pickDefaultCluster(clusters.value); onCluster() }
  } catch (e) { ElMessage.error('加载集群失败') }
})
</script>

<style scoped>
.page-head { margin-bottom: 14px; }
.page-title { font-size: 18px; font-weight: 600; }
.muted { color: #909399; font-size: 12px; }
.bar { display: flex; gap: 10px; align-items: center; margin-bottom: 12px; }
</style>
