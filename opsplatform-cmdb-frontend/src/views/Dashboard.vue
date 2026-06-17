<template>
  <div class="board">
    <div class="board-head">
      <span class="bt">CMDB 资产展示台</span>
      <span class="bclock">{{ now }}</span>
    </div>
    <div class="kpis">
      <div class="kpi" v-for="k in kpis" :key="k.label"><div class="kn" :style="{color:k.color}">{{ k.num }}</div><div class="kl">{{ k.label }}</div></div>
    </div>
    <div class="charts">
      <div class="panel"><div class="pt">按环境分布</div><div ref="envEl" class="chart"></div></div>
      <div class="panel"><div class="pt">30 天内到期倒计时</div><div ref="expEl" class="chart"></div></div>
    </div>
  </div>
</template>

<script setup>
import { ref, onMounted, onBeforeUnmount, nextTick, computed } from 'vue'
import * as echarts from 'echarts/core'
import { PieChart, BarChart } from 'echarts/charts'
import { TooltipComponent, GridComponent, LegendComponent } from 'echarts/components'
import { CanvasRenderer } from 'echarts/renderers'
import { dashboard } from '../api/cmdb'
echarts.use([PieChart, BarChart, TooltipComponent, GridComponent, LegendComponent, CanvasRenderer])

const data = ref({})
const now = ref('')
const envEl = ref(null), expEl = ref(null)
let envChart = null, expChart = null, timer = null

const kpis = computed(() => [
  { label: '配置项', num: data.value.ci_total || 0, color: '#60a5fa' },
  { label: '域名', num: data.value.domain_total || 0, color: '#34d399' },
  { label: '证书', num: data.value.cert_total || 0, color: '#fbbf24' },
  { label: '有效证书', num: data.value.cert_active || 0, color: '#a78bfa' },
  { label: '已过期', num: data.value.cert_expired || 0, color: '#f87171' },
])

function render() {
  const dark = { backgroundColor: 'transparent', textStyle: { color: '#cbd5e1' } }
  if (envEl.value) {
    if (!envChart) envChart = echarts.init(envEl.value)
    const byEnv = data.value.by_env || {}
    envChart.setOption({
      ...dark, tooltip: { trigger: 'item' }, legend: { bottom: 0, textStyle: { color: '#94a3b8' } },
      series: [{ type: 'pie', radius: ['38%', '66%'], data: Object.entries(byEnv).map(([k, v]) => ({ name: k, value: v })), label: { color: '#cbd5e1' } }],
    })
    envChart.resize()
  }
  if (expEl.value) {
    if (!expChart) expChart = echarts.init(expEl.value)
    const exp = (data.value.expiring || []).slice(0, 12)
    expChart.setOption({
      ...dark, tooltip: {}, grid: { left: 120, right: 30, top: 16, bottom: 24 },
      xAxis: { type: 'value', axisLabel: { color: '#94a3b8' } },
      yAxis: { type: 'category', inverse: true, data: exp.map(e => e.name), axisLabel: { color: '#cbd5e1' } },
      series: [{ type: 'bar', data: exp.map(e => ({ value: e.days, itemStyle: { color: e.days <= 7 ? '#f87171' : '#fbbf24' } })), label: { show: true, position: 'right', formatter: '{c} 天', color: '#cbd5e1' } }],
    })
    expChart.resize()
  }
}
async function load() { data.value = await dashboard(); await nextTick(); render() }
function tick() { now.value = new Date().toLocaleString('zh-CN') }
function onResize() { envChart?.resize(); expChart?.resize() }
onMounted(() => {
  tick(); timer = setInterval(tick, 1000)
  window.addEventListener('resize', onResize)
  load()
})
onBeforeUnmount(() => { clearInterval(timer); window.removeEventListener('resize', onResize); envChart?.dispose(); expChart?.dispose() })
</script>

<style scoped>
.board { min-height: calc(100vh - 52px); background: #0f172a; padding: 22px; }
.board-head { display: flex; justify-content: space-between; align-items: center; margin-bottom: 18px; }
.bt { color: #e2e8f0; font-size: 22px; font-weight: 700; letter-spacing: 2px; }
.bclock { color: #64748b; font-family: Consolas, monospace; }
.kpis { display: grid; grid-template-columns: repeat(5, 1fr); gap: 14px; margin-bottom: 18px; }
.kpi { background: #1e293b; border-radius: 10px; padding: 18px; text-align: center; }
.kn { font-size: 34px; font-weight: 800; }
.kl { color: #94a3b8; font-size: 13px; margin-top: 6px; }
.charts { display: grid; grid-template-columns: 1fr 1fr; gap: 14px; }
.panel { background: #1e293b; border-radius: 10px; padding: 14px; }
.pt { color: #cbd5e1; font-size: 14px; margin-bottom: 8px; }
.chart { width: 100%; height: 300px; }
</style>
