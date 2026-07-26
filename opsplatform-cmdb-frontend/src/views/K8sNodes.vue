<template>
  <div class="page">
    <div class="page-head">
      <span class="page-title">K8s 节点</span>
      <span class="muted" style="margin-left:10px">节点池 / 状态 / 容量 · 卡死(Ready=Unknown 或心跳超时)红标</span>
    </div>
    <el-card shadow="never">
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
        <el-switch v-model="onlyBad" active-text="只看异常" style="margin-left:6px" />
        <span class="muted" style="margin-left:auto">
          共 {{ rows.length }} · <b v-if="badCount" style="color:#f56c6c">异常 {{ badCount }}</b><span v-else>全部健康</span>
        </span>
      </div>
      <el-table :data="display" size="small" v-loading="loading" :row-class-name="rowClass">
        <el-table-column v-if="!clusterId" label="集群" width="150"><template #default="{ row }">{{ clusterName(row.cluster_id) }}</template></el-table-column>
        <el-table-column prop="name" label="节点" min-width="200" />
        <el-table-column prop="pool" label="节点池" width="120" />
        <el-table-column label="状态" width="140"><template #default="{ row }">
          <el-tag size="small" :type="row.ready_status==='Ready'?'success':(row.ready_status==='Unknown'?'danger':'warning')">{{ row.ready_status }}</el-tag>
          <el-tag v-if="Number(row.stuck)" size="small" type="danger" effect="dark" style="margin-left:4px">卡死</el-tag>
        </template></el-table-column>
        <el-table-column prop="internal_ip" label="内网IP" width="130" />
        <el-table-column label="容量" width="150"><template #default="{ row }">{{ row.cpu_cap }}核 / {{ memGi(row.mem_cap) }}</template></el-table-column>
        <el-table-column prop="pod_count" label="Pod" width="70" />
        <el-table-column prop="kubelet_version" label="版本" width="110" />
        <el-table-column label="压力" width="130"><template #default="{ row }">
          <span v-if="row.conditions" style="color:#e6a23c">{{ row.conditions }}</span><span v-else class="muted">—</span>
        </template></el-table-column>
        <el-table-column label="最后心跳" min-width="170"><template #default="{ row }">
          <span :class="{bad: staleHb(row)}">{{ row.last_heartbeat || '—' }}</span>
          <span v-if="row.last_heartbeat" class="muted"> ({{ ago(row.last_heartbeat) }})</span>
        </template></el-table-column>
      </el-table>
      <el-empty v-if="!loading && !display.length" :description="onlyBad ? '无异常节点 🎉' : '无数据，先去集群管理点「同步」'" />
    </el-card>
  </div>
</template>

<script setup>
import { ref, computed, onMounted } from 'vue'
import { ElMessage } from 'element-plus'
import { Search } from '@element-plus/icons-vue'
import { listK8sClusters, listK8sNodes, listK8sNodePools } from '../api/cmdb'

const clusters = ref([]); const pools = ref([]); const rows = ref([])
const clusterId = ref(''); const pool = ref(''); const q = ref(''); const onlyBad = ref(false); const loading = ref(false)

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

async function onCluster() { pool.value = ''; await load() }

async function load() {
  loading.value = true
  try {
    const p = {}
    if (clusterId.value) { p.cluster_id = clusterId.value; pools.value = await listK8sNodePools(p) } else { pools.value = [] }
    if (pool.value) p.pool = pool.value
    if (q.value) p.q = q.value
    rows.value = await listK8sNodes(p)
  } catch (e) { ElMessage.error('加载失败') } finally { loading.value = false }
}

onMounted(async () => {
  try {
    clusters.value = await listK8sClusters()
    if (clusters.value.length) clusterId.value = clusters.value[0].id
    load()
  } catch (e) { ElMessage.error('加载集群失败') }
})
</script>

<style scoped>
.page-head { margin-bottom: 14px; }
.page-title { font-size: 18px; font-weight: 600; }
.muted { color: #909399; font-size: 12px; }
.bad { color: #f56c6c; font-weight: 600; }
.bar { display: flex; gap: 10px; align-items: center; margin-bottom: 12px; }
:deep(.bad-row) { background: #fef0f0; }
</style>
