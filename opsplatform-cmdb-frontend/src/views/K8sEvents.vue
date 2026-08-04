<template>
  <div class="page">
    <div class="page-head">
      <span class="page-title">K8s 事件</span>
      <span class="muted" style="margin-left:10px">全集群统一事件（含 Node）· 实时</span>
    </div>
    <el-card shadow="never">
      <div class="bar">
        <el-select v-model="clusterId" placeholder="选集群" style="width:200px" @change="onCluster">
          <el-option v-for="c in clusters" :key="c.id" :label="(c.display_name||c.name)+' · '+c.environment" :value="c.id" />
        </el-select>
        <el-select v-model="kind" clearable placeholder="对象类型" style="width:140px" @change="load">
          <el-option v-for="k in kinds" :key="k" :label="k" :value="k" />
        </el-select>
        <el-select v-model="type" clearable placeholder="级别" style="width:120px" @change="load">
          <el-option label="Warning" value="Warning" /><el-option label="Normal" value="Normal" />
        </el-select>
        <el-select v-model="ns" clearable filterable allow-create default-first-option placeholder="命名空间(可搜/自输)" style="width:170px" @change="load">
          <el-option v-for="n in namespaces" :key="n.name" :label="n.name" :value="n.name" />
        </el-select>
        <el-button :icon="Refresh" @click="load">刷新</el-button>
        <el-switch v-model="auto" active-text="自动刷新" @change="toggleAuto" />
        <!-- 失败时不能报「共 0」：0 是"这个集群没有事件"的断言（CMDB-013） -->
        <span class="muted" style="margin-left:auto">共 {{ error ? '—' : rows.length }}</span>
      </div>
      <LoadError :error="error" title="事件未加载" @retry="load" />
      <el-table :data="paged" size="small" v-loading="loading" max-height="600">
        <el-table-column label="级别" width="90"><template #default="{row}">
          <el-tag size="small" :type="row.type==='Warning'?'danger':'info'">{{ row.type }}</el-tag>
        </template></el-table-column>
        <el-table-column prop="kind" label="对象" width="100" />
        <el-table-column label="名称" min-width="200"><template #default="{row}">{{ row.namespace?row.namespace+'/':'' }}{{ row.object }}</template></el-table-column>
        <el-table-column prop="reason" label="原因" width="160" />
        <el-table-column prop="message" label="消息" min-width="320" />
        <el-table-column prop="count" label="次数" width="70" />
        <el-table-column prop="last_seen" label="最近" width="160" />
      </el-table>
      <Pager :total="rows.length" v-model:page="page" v-model:page-size="pageSize" />
      <!-- 「无事件」是结论，加载失败时不能这么说。这一页的原始 bug 就是接口 502
           被渲染成「共 0 · 无事件」——集群连不上，页面却告诉运维这个集群很干净。 -->
      <el-empty v-if="!loading && !error && !rows.length" description="无事件" />
    </el-card>
  </div>
</template>

<script setup>
import { ref, onMounted, onUnmounted } from 'vue'
import { Refresh } from '@element-plus/icons-vue'
import { listK8sClusters, k8sEvents, listK8sNamespaces } from '../api/cmdb'
import { normalizeError } from '../api/http'
import { pickDefaultCluster } from '../composables/useClusterPick'
import { usePager } from '../composables/usePager'
import { useLoadState } from '../composables/useLoadState'
import Pager from '../components/Pager.vue'
import LoadError from '../components/LoadError.vue'

const kinds = ['Node', 'Pod', 'Deployment', 'StatefulSet', 'DaemonSet', 'ReplicaSet', 'Service', 'Ingress', 'HorizontalPodAutoscaler']
const clusters = ref([]); const rows = ref([]); const namespaces = ref([])
const { page, pageSize, paged } = usePager(rows)
const clusterId = ref(null); const kind = ref(''); const type = ref(''); const ns = ref('')
const { loading, error, run } = useLoadState()
const auto = ref(false); let timer = null

async function onCluster() {
  ns.value = ''
  try { namespaces.value = await listK8sNamespaces({ cluster_id: clusterId.value }) } catch (e) { namespaces.value = [] }
  load()
}

async function load() {
  if (!clusterId.value) return
  // 失败必须落到页面上的 error（红条 + 计数变 —），不能只弹一个 3 秒就消失的 toast：
  // toast 消失后表格照常显示「无事件」，与"这个集群真的没有事件"完全无法区分。
  const r = await run(() => {
    const p = { cluster_id: clusterId.value }
    if (kind.value) p.kind = kind.value
    if (type.value) p.type = type.value
    if (ns.value) p.namespace = ns.value
    return k8sEvents(p)
  })
  rows.value = error.value ? [] : (r || [])
}
function toggleAuto(v) {
  if (v) { timer = setInterval(load, 15000) } else if (timer) { clearInterval(timer); timer = null }
}
onUnmounted(() => { if (timer) clearInterval(timer) })
onMounted(async () => {
  // 集群列表拉不到 = 整页无从选择，同样要落到页面 error 而不是一闪而过的 toast
  try {
    clusters.value = await listK8sClusters()
    if (clusters.value.length) { clusterId.value = pickDefaultCluster(clusters.value); onCluster() }
  } catch (e) {
    clusters.value = []
    error.value = '加载集群列表失败：' + normalizeError(e).message
  }
})
</script>

<style scoped>
.page-head { margin-bottom: 14px; }
.page-title { font-size: 18px; font-weight: 600; }
.muted { color: #909399; font-size: 12px; }
.bar { display: flex; gap: 10px; align-items: center; margin-bottom: 12px; }
</style>
