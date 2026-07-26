<template>
  <div class="page">
    <div class="page-head">
      <span class="page-title">资源使用率</span>
      <span class="muted" style="margin-left:10px">实时查 Prometheus，覆盖 K8s(Pod/工作负载/节点) 与 传统主机；CPU/内存 时序曲线</span>
    </div>
    <el-card shadow="never">
      <div class="bar">
        <el-select v-model="clusterId" placeholder="数据源(集群/环境)" style="width:200px" @change="onCluster">
          <el-option v-for="c in clusters" :key="c.id" :label="(c.display_name||c.name)+' · '+c.environment" :value="c.id" />
        </el-select>
        <el-radio-group v-model="target" @change="onTarget">
          <el-radio-button value="pod">Pod</el-radio-button>
          <el-radio-button value="workload">工作负载</el-radio-button>
          <el-radio-button value="node">节点</el-radio-button>
          <el-radio-button value="host">主机(传统)</el-radio-button>
        </el-radio-group>
        <el-select v-if="target==='pod'||target==='workload'" v-model="ns" clearable filterable placeholder="命名空间" style="width:160px" @change="loadCandidates">
          <el-option v-for="n in namespaces" :key="n.name" :label="n.name" :value="n.name" />
        </el-select>
        <el-select v-model="name" filterable clearable :placeholder="targetLabel+'（可搜）'" style="width:280px" :loading="candLoading">
          <el-option v-for="c in candidates" :key="c.value+c.label" :label="c.label" :value="c.value" />
        </el-select>
        <el-radio-group v-model="metric"><el-radio-button value="cpu">CPU</el-radio-button><el-radio-button value="mem">内存</el-radio-button></el-radio-group>
        <el-select v-if="target==='pod'||target==='workload'" v-model="unit" style="width:130px">
          <el-option value="abs" label="绝对值" /><el-option value="req" label="占Request%" /><el-option value="lim" label="占Limit%" />
        </el-select>
        <el-select v-model="rangeSel" style="width:120px" @change="onRange">
          <el-option :value="30" label="近30分" /><el-option :value="60" label="近1小时" />
          <el-option :value="180" label="近3小时" /><el-option :value="360" label="近6小时" />
          <el-option :value="1440" label="近24小时" /><el-option value="custom" label="自定义…" />
        </el-select>
        <el-date-picker v-if="rangeSel==='custom'" v-model="customRange" type="datetimerange" style="width:340px"
          start-placeholder="开始" end-placeholder="结束" value-format="x" />
        <el-button type="primary" :icon="Search" :loading="loading" @click="query">查询</el-button>
      </div>
      <div v-if="err" class="err">{{ err }}</div>
      <div ref="chartEl" style="height:440px;width:100%"></div>
      <el-empty v-if="!loading && !hasData && !err" description="选目标+名称后点查询" />
    </el-card>
  </div>
</template>

<script setup>
import { ref, computed, onMounted, nextTick } from 'vue'
import { ElMessage } from 'element-plus'
import { Search } from '@element-plus/icons-vue'
import * as echarts from 'echarts'
import { listK8sClusters, listK8sNamespaces, listK8sPods, listK8sWorkloads, listK8sNodes, listHosts, obsUsage } from '../api/cmdb'

const clusters = ref([]); const namespaces = ref([])
const clusterId = ref(null); const target = ref('pod'); const ns = ref(''); const name = ref('')
const metric = ref('cpu'); const unit = ref('abs'); const pctMode = ref(false); const loading = ref(false); const err = ref(''); const hasData = ref(false)
const candidates = ref([]); const candLoading = ref(false)
const rangeSel = ref(60); const customRange = ref(null)
const chartEl = ref(null); let chart = null

const targetLabel = computed(() => ({ pod: 'Pod', workload: '工作负载', node: '节点', host: '主机' }[target.value]))

async function onCluster() { ns.value = ''; namespaces.value = await listK8sNamespaces({ cluster_id: clusterId.value }); loadCandidates() }
function onTarget() { name.value = ''; if (target.value === 'node' || target.value === 'host') ns.value = ''; loadCandidates() }
function onRange() { if (rangeSel.value !== 'custom') customRange.value = null }

// 按目标从 CMDB 已有数据列出候选（可搜下拉，免手输）
async function loadCandidates() {
  candLoading.value = true; candidates.value = []
  try {
    if (target.value === 'pod') {
      const r = await listK8sPods({ cluster_id: clusterId.value, namespace: ns.value })
      candidates.value = r.map(p => ({ value: p.name, label: p.namespace + '/' + p.name, ns: p.namespace }))
    } else if (target.value === 'workload') {
      const r = await listK8sWorkloads({ cluster_id: clusterId.value, namespace: ns.value })
      candidates.value = r.map(w => ({ value: w.name, label: w.namespace + '/' + w.name + ' (' + w.kind + ')', ns: w.namespace }))
    } else if (target.value === 'node') {
      const r = await listK8sNodes({ cluster_id: clusterId.value })
      candidates.value = r.map(n => ({ value: n.name, label: n.name + (n.pool ? ' · ' + n.pool : '') }))
    } else {
      const r = await listHosts()
      candidates.value = r.filter(h => h.internal_ip).map(h => ({ value: h.internal_ip, label: (h.name || h.internal_ip) + ' · ' + h.internal_ip }))
    }
  } catch (e) { /* 静默 */ } finally { candLoading.value = false }
}

function fmtY(v) {
  if (pctMode.value) { // 占比模式:值已是百分比，小值给精度
    if (v >= 10) return v.toFixed(0) + '%'
    if (v >= 1) return v.toFixed(1) + '%'
    return v.toFixed(2) + '%'
  }
  if (metric.value === 'mem') return (v / 1073741824 >= 1) ? (v / 1073741824).toFixed(2) + 'Gi' : (v / 1048576).toFixed(0) + 'Mi'
  const m = v * 1000 // 核 → 毫核；小值给精度，避免全显 0m
  if (m >= 10) return m.toFixed(0) + 'm'
  if (m >= 1) return m.toFixed(1) + 'm'
  return m.toFixed(2) + 'm'
}

async function query() {
  if (!clusterId.value) { ElMessage.warning('选数据源'); return }
  if (!name.value) { ElMessage.warning('选' + targetLabel.value); return }
  loading.value = true; err.value = ''; hasData.value = false
  try {
    const sel = candidates.value.find(c => c.value === name.value)
    const p = { cluster_id: clusterId.value, target: target.value, name: name.value, metric: metric.value }
    if (target.value === 'pod' || target.value === 'workload') p.namespace = sel?.ns || ns.value
    if (rangeSel.value === 'custom' && customRange.value?.length === 2) {
      p.start = Math.floor(customRange.value[0] / 1000); p.end = Math.floor(customRange.value[1] / 1000)
    } else { p.minutes = rangeSel.value }
    const r = await obsUsage(p)
    if (!r.ok) { err.value = '查询失败：' + (r.error || JSON.stringify(r.data)); return }
    const result = r.data?.data?.result || []
    if (!result.length) { err.value = '无数据（该时间段/名称在 Prometheus 中无该指标；主机需装 node-exporter）'; return }
    // 占比模式:用 CMDB 里各 Pod 的 request/limit 把绝对用量换成百分比
    let reqMap = {}
    if (unit.value !== 'abs' && (target.value === 'pod' || target.value === 'workload')) {
      try {
        const pods = await listK8sPods({ cluster_id: clusterId.value, namespace: sel?.ns || ns.value })
        pods.forEach(pd => { reqMap[pd.name] = pd })
      } catch (e) { /* 拿不到就退回绝对 */ }
    }
    const divisor = (podName) => {
      const pd = reqMap[podName]
      if (!pd) return 0
      if (metric.value === 'cpu') return (unit.value === 'lim' ? pd.cpu_lim_m : pd.cpu_req_m) / 1000 // 毫核→核(与绝对同单位)
      return (unit.value === 'lim' ? pd.mem_lim_mi : pd.mem_req_mi) * 1048576 // Mi→字节
    }
    const series = result.map((s, i) => {
      const podName = s.metric.pod || s.metric.node || s.metric.instance || ''
      const d = unit.value !== 'abs' ? divisor(podName) : 0
      return {
        name: podName || (metric.value.toUpperCase() + ' ' + (i + 1)),
        type: 'line', smooth: true, showSymbol: false, areaStyle: { opacity: 0.08 },
        data: s.values.map(([ts, v]) => [ts * 1000, d > 0 ? parseFloat(v) / d * 100 : parseFloat(v)])
      }
    })
    pctMode.value = unit.value !== 'abs' && Object.keys(reqMap).length > 0
    if (unit.value !== 'abs' && !pctMode.value) err.value = '注:未取到 request/limit，暂显绝对值'
    hasData.value = true
    nextTick(() => renderChart(series))
  } catch (e) { err.value = '请求失败：' + e.message } finally { loading.value = false }
}

function renderChart(series) {
  if (!chart) chart = echarts.init(chartEl.value)
  chart.setOption({
    tooltip: { trigger: 'axis', valueFormatter: fmtY }, grid: { left: 60, right: 20, top: 30, bottom: 40 },
    legend: { type: 'scroll', top: 0 }, xAxis: { type: 'time' },
    yAxis: { type: 'value', axisLabel: { formatter: fmtY } }, series
  }, true)
}

onMounted(async () => {
  try {
    clusters.value = await listK8sClusters()
    if (clusters.value.length) { clusterId.value = clusters.value[0].id; onCluster() }
  } catch (e) { ElMessage.error('加载集群失败') }
  window.addEventListener('resize', () => chart && chart.resize())
})
</script>

<style scoped>
.page-head { margin-bottom: 14px; }
.page-title { font-size: 18px; font-weight: 600; }
.muted { color: #909399; font-size: 12px; }
.bar { display: flex; gap: 10px; align-items: center; margin-bottom: 14px; flex-wrap: wrap; }
.err { color: #f56c6c; background: #fef0f0; border: 1px solid #fde2e2; padding: 8px 12px; border-radius: 6px; margin-bottom: 10px; font-size: 13px; }
</style>
