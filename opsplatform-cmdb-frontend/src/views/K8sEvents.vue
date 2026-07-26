<template>
  <div class="page">
    <div class="page-head">
      <span class="page-title">K8s 事件</span>
      <span class="muted" style="margin-left:10px">全集群统一事件（含 Node）· 实时</span>
    </div>
    <el-card shadow="never">
      <div class="bar">
        <el-select v-model="clusterId" placeholder="选集群" style="width:200px" @change="load">
          <el-option v-for="c in clusters" :key="c.id" :label="(c.display_name||c.name)+' · '+c.environment" :value="c.id" />
        </el-select>
        <el-select v-model="kind" clearable placeholder="对象类型" style="width:140px" @change="load">
          <el-option v-for="k in kinds" :key="k" :label="k" :value="k" />
        </el-select>
        <el-select v-model="type" clearable placeholder="级别" style="width:120px" @change="load">
          <el-option label="Warning" value="Warning" /><el-option label="Normal" value="Normal" />
        </el-select>
        <el-input v-model="ns" clearable placeholder="命名空间" style="width:150px" @keyup.enter="load" @clear="load" />
        <el-button :icon="Refresh" @click="load">刷新</el-button>
        <el-switch v-model="auto" active-text="自动刷新" @change="toggleAuto" />
        <span class="muted" style="margin-left:auto">共 {{ rows.length }}</span>
      </div>
      <el-table :data="rows" size="small" v-loading="loading" max-height="600">
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
      <el-empty v-if="!loading && !rows.length" description="无事件" />
    </el-card>
  </div>
</template>

<script setup>
import { ref, onMounted, onUnmounted } from 'vue'
import { ElMessage } from 'element-plus'
import { Refresh } from '@element-plus/icons-vue'
import { listK8sClusters, k8sEvents } from '../api/cmdb'

const kinds = ['Node', 'Pod', 'Deployment', 'StatefulSet', 'DaemonSet', 'ReplicaSet', 'Service', 'Ingress', 'HorizontalPodAutoscaler']
const clusters = ref([]); const rows = ref([])
const clusterId = ref(null); const kind = ref(''); const type = ref(''); const ns = ref(''); const loading = ref(false)
const auto = ref(false); let timer = null

async function load() {
  if (!clusterId.value) return
  loading.value = true
  try {
    const p = { cluster_id: clusterId.value }
    if (kind.value) p.kind = kind.value
    if (type.value) p.type = type.value
    if (ns.value) p.namespace = ns.value
    rows.value = await k8sEvents(p)
  } catch (e) { ElMessage.error('加载失败') } finally { loading.value = false }
}
function toggleAuto(v) {
  if (v) { timer = setInterval(load, 15000) } else if (timer) { clearInterval(timer); timer = null }
}
onUnmounted(() => { if (timer) clearInterval(timer) })
onMounted(async () => {
  try { clusters.value = await listK8sClusters(); if (clusters.value.length) { clusterId.value = clusters.value[0].id; load() } }
  catch (e) { ElMessage.error('加载集群失败') }
})
</script>

<style scoped>
.page-head { margin-bottom: 14px; }
.page-title { font-size: 18px; font-weight: 600; }
.muted { color: #909399; font-size: 12px; }
.bar { display: flex; gap: 10px; align-items: center; margin-bottom: 12px; }
</style>
