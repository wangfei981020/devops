<template>
  <div class="page">
    <div class="page-head"><span class="page-title">关系图谱</span><el-button :icon="Refresh" @click="load">刷新</el-button></div>
    <el-card shadow="never">
      <div ref="chartEl" style="width:100%;height:560px"></div>
      <el-empty v-if="!edges.length" description="暂无关系（申请证书时会自动建立 证书→域名 关系）" :image-size="70" />
    </el-card>
  </div>
</template>

<script setup>
import { ref, onMounted, onBeforeUnmount, nextTick } from 'vue'
import { Refresh } from '@element-plus/icons-vue'
import * as echarts from 'echarts/core'
import { GraphChart } from 'echarts/charts'
import { TooltipComponent, LegendComponent } from 'echarts/components'
import { CanvasRenderer } from 'echarts/renderers'
import { listRelations } from '../api/cmdb'
echarts.use([GraphChart, TooltipComponent, LegendComponent, CanvasRenderer])

const chartEl = ref(null)
const edges = ref([])
let chart = null

async function load() {
  edges.value = await listRelations()
  await nextTick()
  render()
}
function render() {
  if (!chartEl.value) return
  if (!chart) chart = echarts.init(chartEl.value)
  const nodeMap = {}
  const links = []
  for (const e of edges.value) {
    nodeMap[e.src_ci_id] = { id: String(e.src_ci_id), name: e.src_name, category: e.src_type }
    nodeMap[e.dst_ci_id] = { id: String(e.dst_ci_id), name: e.dst_name, category: e.dst_type }
    links.push({ source: String(e.src_ci_id), target: String(e.dst_ci_id), label: { show: true, formatter: e.rel_type } })
  }
  chart.setOption({
    tooltip: {},
    legend: [{ data: ['certificate', 'domain'], top: 8 }],
    series: [{
      type: 'graph', layout: 'force', roam: true, draggable: true,
      force: { repulsion: 220, edgeLength: 140 },
      label: { show: true, position: 'right' },
      categories: [{ name: 'certificate' }, { name: 'domain' }],
      data: Object.values(nodeMap).map(n => ({ ...n, symbolSize: 38, category: n.category === 'certificate' ? 0 : 1 })),
      links,
      lineStyle: { color: '#aaa', curveness: 0.1 },
    }],
  })
  chart.resize()
}
function onResize() { chart?.resize() }
onMounted(() => { window.addEventListener('resize', onResize); load() })
onBeforeUnmount(() => { window.removeEventListener('resize', onResize); chart?.dispose() })
</script>
