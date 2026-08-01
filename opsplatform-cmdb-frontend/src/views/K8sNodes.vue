<template>
  <div class="page">
    <div class="page-head">
      <span class="page-title">K8s 节点</span>
      <span class="muted" style="margin-left:10px">节点池 / 状态 / 容量 · 卡死(Ready=Unknown 或心跳超时)红标</span>
    </div>
    <el-card shadow="never">
      <LoadError :error="error" @retry="load" />
      <div class="bar">
        <el-select v-model="clusterId" placeholder="集群" style="width:220px" @change="onCluster">
          <el-option label="全部集群" :value="''" />
          <el-option v-for="c in clusters" :key="c.id" :label="(c.display_name||c.name)+' · '+c.environment" :value="c.id" />
        </el-select>
        <el-select v-model="pool" clearable placeholder="节点池" style="width:170px" @change="load">
          <el-option v-for="p in pools" :key="p.name" :label="p.name+' ('+p.node_count+')'" :value="p.name" />
        </el-select>
        <el-input v-model="q" clearable placeholder="搜节点名/IP" style="width:200px" @keyup.enter="load" @clear="load" />
        <el-button :icon="Search" @click="load">查询</el-button>
        <el-switch v-model="onlyBad" active-text="只看异常" style="margin-left:6px" @change="page=1" />
        <span class="muted" style="margin-left:auto">
          <!-- 「全部健康」是一句断言，只有在数据真的取到时才允许说出口（CMDB-013） -->
          <template v-if="error">共 — · <b style="color:#f56c6c">数据未加载，状态未知</b></template>
          <template v-else>共 {{ rows.length }} · <b v-if="badCount" style="color:#f56c6c">异常 {{ badCount }}</b><span v-else>全部健康</span></template>
        </span>
      </div>
      <el-table :data="paged" size="small" v-loading="loading" :row-class-name="rowClass">
        <el-table-column v-if="!clusterId" label="集群" width="150"><template #default="{ row }">{{ clusterName(row.cluster_id) }}</template></el-table-column>
        <el-table-column label="节点" min-width="220"><template #default="{ row }">
          {{ row.name }}
          <el-tooltip v-if="Number(row.host_ci_id)" content="关联的云主机(GCE)——点看机型/IP/项目">
            <el-link type="primary" underline="never" style="margin-left:6px;font-size:12px" @click="openHost(row)">🖥主机</el-link>
          </el-tooltip>
        </template></el-table-column>
        <el-table-column prop="pool" label="节点池" width="120" />
        <el-table-column label="状态" width="140"><template #default="{ row }">
          <el-tag size="small" :type="row.ready_status==='Ready'?'success':(row.ready_status==='Unknown'?'danger':'warning')">{{ row.ready_status }}</el-tag>
          <el-tag v-if="Number(row.stuck)" size="small" type="danger" effect="dark" style="margin-left:4px">卡死</el-tag>
        </template></el-table-column>
        <el-table-column prop="internal_ip" label="内网IP" width="130" />
        <el-table-column label="容量" width="140"><template #default="{ row }">{{ row.cpu_cap }}核 / {{ memGi(row.mem_cap) }}</template></el-table-column>
        <el-table-column label="CPU用量" width="120"><template #default="{ row }">
          <template v-if="nu(row,'cpu_pct')"><el-progress :percentage="Math.min(100,Math.round(nuVal(row,'cpu_pct')))" :stroke-width="10" :color="barColor" text-inside /></template>
          <span v-else class="muted">—</span>
        </template></el-table-column>
        <el-table-column label="内存用量" width="120"><template #default="{ row }">
          <template v-if="nu(row,'mem_pct')"><el-progress :percentage="Math.min(100,Math.round(nuVal(row,'mem_pct')))" :stroke-width="10" :color="barColor" text-inside /></template>
          <span v-else class="muted">—</span>
        </template></el-table-column>
        <el-table-column label="CPU请求装箱" width="150"><template #default="{ row }">
          <el-tooltip :content="`已request ${cp(row,'req_cpu_m')}m / 可分配 ${cp(row,'alloc_cpu_m')}m · limit ${cp(row,'lim_cpu_m')}m`">
            <div><el-progress :percentage="Math.min(150,Math.round(cp(row,'cpu_req_pct')))" :stroke-width="10" :color="boxColor" text-inside /></div>
          </el-tooltip>
        </template></el-table-column>
        <el-table-column label="内存请求装箱" width="150"><template #default="{ row }">
          <el-tooltip :content="`已request ${cp(row,'req_mem_mi')}Mi / 可分配 ${cp(row,'alloc_mem_mi')}Mi · limit ${cp(row,'lim_mem_mi')}Mi`">
            <div><el-progress :percentage="Math.min(150,Math.round(cp(row,'mem_req_pct')))" :stroke-width="10" :color="boxColor" text-inside /></div>
          </el-tooltip>
        </template></el-table-column>
        <el-table-column prop="pod_count" label="Pod" width="70" />
        <el-table-column prop="kubelet_version" label="版本" width="110" />
        <el-table-column label="压力/状况" width="150"><template #default="{ row }">
          <el-popover placement="left" width="420" trigger="click">
            <template #reference>
              <span style="cursor:pointer">
                <span v-if="row.conditions" style="color:#f56c6c">⚠ {{ row.conditions }}</span>
                <span v-else style="color:#67c23a">正常</span>
                <el-link type="primary" underline="never" style="margin-left:6px;font-size:12px">详情</el-link>
              </span>
            </template>
            <div style="font-size:12px">
              <div style="font-weight:600;margin-bottom:6px">{{ row.name }} · 节点状况</div>
              <el-table :data="conds(row)" size="small">
                <el-table-column prop="type" label="类型" width="150" />
                <el-table-column label="状态" width="70"><template #default="{ row: c }"><el-tag size="small" :type="c.real?'danger':(c.status==='True'?'info':'success')">{{ c.status }}</el-tag></template></el-table-column>
                <el-table-column label="原因/说明"><template #default="{ row: c }"><span :style="c.real?'color:#f56c6c':''">{{ c.reason }}<span v-if="c.message" class="muted"> · {{ c.message }}</span></span></template></el-table-column>
              </el-table>
              <div class="muted" style="margin-top:6px">红=真压力(内存/磁盘/PID/网络)；SysctlChanged 等为信息性，不计异常</div>
            </div>
          </el-popover>
        </template></el-table-column>
        <el-table-column label="最后心跳" min-width="170"><template #default="{ row }">
          <span :class="{bad: staleHb(row)}">{{ row.last_heartbeat || '—' }}</span>
          <span v-if="row.last_heartbeat" class="muted"> ({{ ago(row.last_heartbeat) }})</span>
        </template></el-table-column>
      </el-table>
      <div class="pager" v-if="display.length > pageSize">
        <el-pagination layout="total, sizes, prev, pager, next" :total="display.length"
          v-model:current-page="page" v-model:page-size="pageSize" :page-sizes="[20,50,100]" size="small" background />
      </div>
      <el-empty v-if="!loading && !error && !display.length" :description="onlyBad ? '无异常节点 🎉' : '无数据，先去集群管理点「同步」'" />
    </el-card>
    <el-dialog :close-on-click-modal="false" v-model="hostDlg" title="关联云主机" width="520px">
      <el-descriptions v-if="host" :column="1" border size="small">
        <el-descriptions-item label="主机名">{{ host.name }}</el-descriptions-item>
        <el-descriptions-item label="机型">{{ host.machine_type || '—' }} · {{ host.vcpu }}核 / {{ host.mem_mb ? (host.mem_mb/1024).toFixed(1)+'G':'—' }}</el-descriptions-item>
        <el-descriptions-item label="内网IP">{{ host.internal_ip || '—' }}</el-descriptions-item>
        <el-descriptions-item label="外网IP">{{ host.external_ip || '—' }}</el-descriptions-item>
        <el-descriptions-item label="区域">{{ host.zone || host.region || '—' }}</el-descriptions-item>
        <el-descriptions-item label="项目">{{ host.project_name || host.project || '—' }}</el-descriptions-item>
        <el-descriptions-item label="状态">{{ host.status || '—' }}</el-descriptions-item>
        <el-descriptions-item label="类型"><el-tag size="small" type="success" v-if="host.is_k8s_node">☸ K8s节点{{ host.k8s_pool?'/'+host.k8s_pool:'' }}</el-tag></el-descriptions-item>
      </el-descriptions>
      <el-empty v-else description="未取到主机" :image-size="50" />
    </el-dialog>
  </div>
</template>

<script setup>
import { ref, computed, onMounted } from 'vue'
import { Search } from '@element-plus/icons-vue'
import { listK8sClusters, listK8sNodes, listK8sNodePools, k8sNodeUsage, k8sNodeCapacity, getHost } from '../api/cmdb'
import { useLoadState } from '../composables/useLoadState'
import LoadError from '../components/LoadError.vue'

const { loading, error, run } = useLoadState()

const clusters = ref([]); const pools = ref([]); const rows = ref([]); const usage = ref({}); const cap = ref({})
function cp(row, k) { const m = cap.value[row.name]; return (m && m[k] != null) ? m[k] : 0 }
const clusterId = ref(''); const pool = ref(''); const q = ref(''); const onlyBad = ref(false)
function nu(row, k) { const m = usage.value[row.name]; return (m && m[k] != null) ? m[k].toFixed(0) + '%' : null }
function nuVal(row, k) { const m = usage.value[row.name]; return (m && m[k] != null) ? m[k] : 0 }
const hostDlg = ref(false); const host = ref(null)
async function openHost(row) {
  hostDlg.value = true; host.value = null
  try { host.value = await getHost(row.host_ci_id) } catch (e) { host.value = null }
}
function barColor(p) { return p >= 85 ? '#f56c6c' : (p >= 60 ? '#e6a23c' : '#67c23a') }
function boxColor(p) { return p >= 100 ? '#f56c6c' : (p >= 80 ? '#e6a23c' : '#67c23a') }

function memGi(ki) { const n = parseInt(ki); return isNaN(n) ? (ki || '—') : (n / 1024 / 1024).toFixed(1) + 'Gi' }
function clusterName(id) { const c = clusters.value.find(x => x.id === id); return c ? (c.display_name || c.name) : id }
function isBad(row) { return Number(row.stuck) === 1 || row.ready_status !== 'Ready' || !!row.conditions }
function staleHb(row) { return Number(row.stuck) === 1 }
function ago(ts) {
  if (!ts) return ''
  // DB 返回的是本地墙钟时间(与浏览器同时区)，按本地解析，不加 Z
  let s = Math.floor((Date.now() - new Date(ts.replace(' ', 'T')).getTime()) / 1000)
  if (isNaN(s)) return ''
  if (s < 0) s = 0
  if (s < 60) return s + '秒前'
  if (s < 3600) return Math.floor(s / 60) + '分前'
  if (s < 86400) return Math.floor(s / 3600) + '小时前'
  return Math.floor(s / 86400) + '天前'
}
function rowClass({ row }) { return isBad(row) ? 'bad-row' : '' }

const display = computed(() => onlyBad.value ? rows.value.filter(isBad) : rows.value)
const badCount = computed(() => rows.value.filter(isBad).length)
const page = ref(1); const pageSize = ref(20)
const paged = computed(() => display.value.slice((page.value - 1) * pageSize.value, page.value * pageSize.value))
function conds(row) { try { return JSON.parse(row.conditions_json || '[]') } catch (e) { return [] } }

async function onCluster() { pool.value = ''; await load() }

async function loadUsage() {
  usage.value = {}; cap.value = {}
  if (!clusterId.value) return
  try { const r = await k8sNodeUsage({ cluster_id: clusterId.value }); if (r.ok) usage.value = r.usage || {} } catch (e) { /* 静默 */ }
  try { const r = await k8sNodeCapacity({ cluster_id: clusterId.value }); if (Array.isArray(r)) { const m = {}; r.forEach(x => m[x.node] = x); cap.value = m } } catch (e) { /* 静默 */ }
}

async function load() {
  // 失败时把 rows 清空：留着上一次的数据配上这次的错误横幅，会让人分不清看的是哪一时刻的状态
  await run(async () => {
    const p = {}
    if (clusterId.value) { p.cluster_id = clusterId.value; pools.value = await listK8sNodePools(p) } else { pools.value = [] }
    if (pool.value) p.pool = pool.value
    if (q.value) p.q = q.value
    const r = await listK8sNodes(p)
    rows.value = r
    page.value = 1
    loadUsage()
  })
  if (error.value) { rows.value = []; pools.value = [] }
}

onMounted(async () => {
  const ok = await run(async () => {
    clusters.value = await listK8sClusters()
    return true
  })
  if (!ok) return
  if (clusters.value.length) clusterId.value = clusters.value[0].id
  load()
})
</script>

<style scoped>
.page-head { margin-bottom: 14px; }
.page-title { font-size: 18px; font-weight: 600; }
.muted { color: #909399; font-size: 12px; }
.bad { color: #f56c6c; font-weight: 600; }
.bar { display: flex; gap: 10px; align-items: center; margin-bottom: 12px; }
.pager { display: flex; justify-content: flex-end; margin-top: 12px; }
:deep(.bad-row) { background: #fef0f0; }
</style>
