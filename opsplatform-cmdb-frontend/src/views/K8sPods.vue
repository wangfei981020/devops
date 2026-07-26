<template>
  <div class="page">
    <div class="page-head">
      <span class="page-title">K8s Pod</span>
      <span class="muted" style="margin-left:10px">按命名空间 / 节点筛选</span>
    </div>
    <el-card shadow="never">
      <div class="bar">
        <el-select v-model="clusterId" placeholder="选集群" style="width:200px" @change="onCluster">
          <el-option v-for="c in clusters" :key="c.id" :label="(c.display_name||c.name)+' · '+c.environment" :value="c.id" />
        </el-select>
        <el-select v-model="ns" clearable filterable placeholder="命名空间" style="width:180px" @change="load">
          <el-option v-for="n in namespaces" :key="n.name" :label="n.name" :value="n.name" />
        </el-select>
        <el-input v-model="node" clearable placeholder="节点名" style="width:180px" @keyup.enter="load" @clear="load" />
        <el-input v-model="q" clearable placeholder="搜 Pod名/IP/工作负载" style="width:220px" @keyup.enter="load" @clear="load" />
        <el-button :icon="Search" @click="load">查询</el-button>
        <span class="muted" style="margin-left:auto">共 {{ rows.length }}</span>
      </div>
      <el-table :data="rows" size="small" v-loading="loading">
        <el-table-column prop="namespace" label="命名空间" width="140" />
        <el-table-column prop="name" label="Pod" min-width="240" />
        <el-table-column prop="workload" label="工作负载" min-width="160"><template #default="{ row }">{{ row.workload || '—' }}</template></el-table-column>
        <el-table-column prop="node_name" label="节点" min-width="160" />
        <el-table-column label="状态" width="100"><template #default="{ row }">
          <el-tag size="small" :type="row.phase==='Running'||row.phase==='Succeeded'?'success':(row.phase==='Failed'?'danger':'warning')">{{ row.phase }}</el-tag>
        </template></el-table-column>
        <el-table-column label="CPU req/lim" width="130"><template #default="{ row }">
          {{ cpu(row.cpu_req_m) }} / {{ cpu(row.cpu_lim_m) }}
        </template></el-table-column>
        <el-table-column label="内存 req/lim" width="140"><template #default="{ row }">
          {{ mem(row.mem_req_mi) }} / {{ mem(row.mem_lim_mi) }}
        </template></el-table-column>
        <el-table-column label="重启" width="70"><template #default="{ row }">
          <span :class="{bad: row.restarts>5}">{{ row.restarts }}</span>
        </template></el-table-column>
        <el-table-column prop="pod_ip" label="IP" width="120" />
        <el-table-column prop="start_time" label="启动时间" width="150" />
        <el-table-column label="操作" width="180" fixed="right"><template #default="{ row }">
          <el-button link type="primary" size="small" @click="openLogs(row)">日志</el-button>
          <el-button link type="primary" size="small" @click="openEvents(row)">事件</el-button>
          <el-button link type="warning" size="small" @click="openDiag(row)">诊断</el-button>
        </template></el-table-column>
      </el-table>

      <!-- 日志/事件/诊断 弹窗 -->
      <el-dialog :close-on-click-modal="false" v-model="dlg" :title="dlgTitle" width="820px" top="6vh">
        <div v-loading="dlgLoading">
          <pre v-if="dlgMode==='logs'" class="logbox">{{ logText || '（无日志）' }}</pre>
          <el-table v-else-if="dlgMode==='events'" :data="events" size="small" max-height="440">
            <el-table-column label="类型" width="90"><template #default="{row}">
              <el-tag size="small" :type="row.type==='Warning'?'danger':'info'">{{ row.type }}</el-tag>
            </template></el-table-column>
            <el-table-column prop="reason" label="原因" width="150" />
            <el-table-column prop="message" label="消息" min-width="320" />
            <el-table-column prop="count" label="次数" width="70" />
            <el-table-column prop="last_seen" label="最近" width="160" />
          </el-table>
          <div v-else-if="dlgMode==='diag'">
            <el-alert :type="diag.result?.matched?'warning':'info'" :closable="false"
              :title="diag.result?.matched ? ('根因：'+diag.result.root_cause+'（'+conf(diag.result.confidence)+'）') : '未命中已知故障模式（可看下方证据/日志人工判断）'" />
            <div class="mini-t" style="margin-top:12px">证据</div>
            <ul class="ev"><li v-for="(e,i) in diag.result?.evidence||[]" :key="i">{{ e }}</li></ul>
            <div class="mini-t">处置建议（手动执行）</div>
            <ol class="sol"><li v-for="(s,i) in diag.result?.solutions||[]" :key="i">{{ s.text }}</li></ol>
            <el-empty v-if="!(diag.result?.solutions||[]).length" description="无建议" :image-size="60" />
          </div>
        </div>
      </el-dialog>
      <el-empty v-if="!loading && !rows.length" description="无数据，先去集群管理点「同步」" />
    </el-card>
  </div>
</template>

<script setup>
import { ref, onMounted } from 'vue'
import { ElMessage } from 'element-plus'
import { Search } from '@element-plus/icons-vue'
import { listK8sClusters, listK8sPods, listK8sNamespaces, k8sPodLogs, k8sPodEvents, k8sDiagnose } from '../api/cmdb'

const clusters = ref([]); const namespaces = ref([]); const rows = ref([])
const clusterId = ref(null); const ns = ref(''); const node = ref(''); const q = ref(''); const loading = ref(false)
const dlg = ref(false); const dlgMode = ref(''); const dlgTitle = ref(''); const dlgLoading = ref(false)
const logText = ref(''); const events = ref([]); const diag = ref({})

function cpu(m) { m = Number(m) || 0; return m === 0 ? '—' : (m >= 1000 ? (m / 1000) + '核' : m + 'm') }
function mem(mi) { mi = Number(mi) || 0; return mi === 0 ? '—' : (mi >= 1024 ? (mi / 1024).toFixed(1) + 'Gi' : mi + 'Mi') }
function conf(c) { return { high: '高置信', medium: '中', low: '低' }[c] || c }

async function openLogs(row) {
  dlgMode.value = 'logs'; dlgTitle.value = '日志 · ' + row.name; dlg.value = true; dlgLoading.value = true; logText.value = ''
  try { logText.value = await k8sPodLogs({ cluster_id: clusterId.value, namespace: row.namespace, pod: row.name, tail: 300 }) }
  catch (e) { logText.value = '取日志失败：' + (e.response?.data?.error || e.message) } finally { dlgLoading.value = false }
}
async function openEvents(row) {
  dlgMode.value = 'events'; dlgTitle.value = '事件 · ' + row.name; dlg.value = true; dlgLoading.value = true; events.value = []
  try { events.value = await k8sPodEvents({ cluster_id: clusterId.value, namespace: row.namespace, pod: row.name }) }
  catch (e) { ElMessage.error('取事件失败') } finally { dlgLoading.value = false }
}
async function openDiag(row) {
  dlgMode.value = 'diag'; dlgTitle.value = '诊断 · ' + row.name; dlg.value = true; dlgLoading.value = true; diag.value = {}
  try { diag.value = await k8sDiagnose({ cluster_id: clusterId.value, namespace: row.namespace, pod: row.name }) }
  catch (e) { ElMessage.error('诊断失败：' + (e.response?.data?.error || '')) } finally { dlgLoading.value = false }
}

async function onCluster() { ns.value = ''; namespaces.value = await listK8sNamespaces({ cluster_id: clusterId.value }); load() }

async function load() {
  if (!clusterId.value) return
  loading.value = true
  try {
    const p = { cluster_id: clusterId.value }
    if (ns.value) p.namespace = ns.value
    if (node.value) p.node = node.value
    if (q.value) p.q = q.value
    rows.value = await listK8sPods(p)
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
.bad { color: #f56c6c; font-weight: 600; }
.bar { display: flex; gap: 10px; align-items: center; margin-bottom: 12px; }
.logbox { background: #1e1e1e; color: #d4d4d4; padding: 12px; border-radius: 6px; max-height: 460px; overflow: auto; font-size: 12px; line-height: 1.5; white-space: pre-wrap; word-break: break-all; }
.mini-t { font-size: 13px; font-weight: 600; margin: 12px 0 6px; }
.ev { margin: 0; padding-left: 18px; font-size: 13px; color: #606266; }
.ev li, .sol li { margin: 4px 0; }
.sol { margin: 0; padding-left: 18px; font-size: 13px; }
</style>
