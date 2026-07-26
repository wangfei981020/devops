<template>
  <div class="page">
    <div class="page-head">
      <span class="page-title">资源使用率</span>
      <span class="muted" style="margin-left:10px">实时查 Prometheus（数据源接入配置），CPU/内存 时序曲线</span>
    </div>
    <el-card shadow="never">
      <div class="bar">
        <el-select v-model="clusterId" placeholder="集群" style="width:200px" @change="onCluster">
          <el-option v-for="c in clusters" :key="c.id" :label="(c.display_name||c.name)+' · '+c.environment" :value="c.id" />
        </el-select>
        <el-radio-group v-model="target" @change="onTarget">
          <el-radio-button value="pod">Pod</el-radio-button>
          <el-radio-button value="workload">工作负载</el-radio-button>
          <el-radio-button value="node">节点</el-radio-button>
        </el-radio-group>
        <el-select v-if="target!=='node'" v-model="ns" clearable filterable placeholder="命名空间" style="width:170px">
          <el-option v-for="n in namespaces" :key="n.name" :label="n.name" :value="n.name" />
        </el-select>
        <el-input v-model="name" clearable :placeholder="target==='node'?'节点名':(target==='workload'?'工作负载名':'Pod 名')" style="width:220px" @keyup.enter="query" />
        <el-radio-group v-model="metric">
          <el-radio-button value="cpu">CPU</el-radio-button>
          <el-radio-button value="mem">内存</el-radio-button>
        </el-radio-group>
        <el-select v-model="minutes" style="width:110px">
          <el-option :value="30" label="近30分" /><el-option :value="60" label="近1小时" />
          <el-option :value="180" label="近3小时" /><el-option :value="360" label="近6小时" />
        </el-select>
        <el-button type="primary" :icon="Search" :loading="loading" @click="query">查询</el-button>
      </div>
      <div v-if="err" class="err">{{ err }}</div>
      <div ref="chartEl" style="height:440px;width:100%"></div>
      <el-empty v-if="!loading && !hasData && !err" description="选目标后点查询（Pod/工作负载需填名称；节点填节点名）" />
    </el-card>
  </div>
</template>

<script setup>
import { ref, onMounted, nextTick } from 'vue'
import { ElMessage } from 'element-plus'
import { Search } from '@element-plus/icons-vue'
import * as echarts from 'echarts'
import { listK8sClusters, listK8sNamespaces, obsUsage } from '../api/cmdb'

const clusters = ref([]); const namespaces = ref([])
const clusterId = ref(null); const target = ref('pod'); const ns = ref(''); const name = ref('')
const metric = ref('cpu'); const minutes = ref(60); const loading = ref(false); const err = ref(''); const hasData = ref(false)
const chartEl = ref(null); let chart = null

async function onCluster() { ns.value = ''; namespaces.value = await listK8sNamespaces({ cluster_id: clusterId.value }) }
function onTarget() { if (target.value === 'node') ns.value = '' }

function fmtY(v) {
  if (metric.value === 'mem') { return (v / 1048576 >= 1024) ? (v / 1073741824).toFixed(2) + 'Gi' : (v / 1048576).toFixed(0) + 'Mi' }
  return (v * 1000).toFixed(0) + 'm' // cpu 核 → 毫核
}

async function query() {
  if (!clusterId.value) { ElMessage.warning('选集群'); return }
  if (!name.value) { ElMessage.warning('填名称'); return }
  loading.value = true; err.value = ''; hasData.value = false
  try {
    const r = await obsUsage({ cluster_id: clusterId.value, target: target.value, namespace: ns.value, name: name.value, metric: metric.value, minutes: minutes.value })
    if (!r.ok) { err.value = '查询失败：' + (r.error || JSON.stringify(r.data)); return }
    const result = r.data?.data?.result || []
    if (!result.length) { err.value = '无数据（检查名称/命名空间是否正确，或 Prometheus 中是否有该指标）'; return }
    const series = result.map((s, i) => ({
      name: seriesName(s.metric, i), type: 'line', smooth: true, showSymbol: false, areaStyle: { opacity: 0.08 },
      data: s.values.map(([ts, v]) => [ts * 1000, parseFloat(v)])
    }))
    hasData.value = true
    nextTick(renderChart.bind(null, series))
  } catch (e) { err.value = '请求失败：' + e.message } finally { loading.value = false }
}
function seriesName(m, i) { return m.pod || m.node || m.instance || (metric.value.toUpperCase() + ' ' + (i + 1)) }

function renderChart(series) {
  if (!chart) chart = echarts.init(chartEl.value)
  chart.setOption({
    tooltip: { trigger: 'axis', valueFormatter: fmtY },
    grid: { left: 60, right: 20, top: 30, bottom: 40 },
    legend: { type: 'scroll', top: 0 },
    xAxis: { type: 'time' },
    yAxis: { type: 'value', axisLabel: { formatter: fmtY } },
    series
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
