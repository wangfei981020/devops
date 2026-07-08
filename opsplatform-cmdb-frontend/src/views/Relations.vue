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
  // 按 类型|名称 合并同名节点（多张同名证书合并成一个，去掉堆叠）
  const nodeMap = {}   // key -> node，key=type|name
  const degree = {}    // key -> 连接数
  const linkSet = new Set()
  const links = []
  const keyOf = (type, name) => type + '|' + name
  for (const e of edges.value) {
    const sk = keyOf(e.src_type, e.src_name), dk = keyOf(e.dst_type, e.dst_name)
    if (!nodeMap[sk]) nodeMap[sk] = { id: sk, name: e.src_name, category: e.src_type, count: 0 }
    if (!nodeMap[dk]) nodeMap[dk] = { id: dk, name: e.dst_name, category: e.dst_type, count: 0 }
    nodeMap[sk].count++
    const lk = sk + '>' + dk + '>' + e.rel_type
    if (!linkSet.has(lk)) {
      linkSet.add(lk)
      links.push({ source: sk, target: dk, label: { show: true, formatter: e.rel_type } })
      degree[sk] = (degree[sk] || 0) + 1
      degree[dk] = (degree[dk] || 0) + 1
    }
  }
  const data = Object.values(nodeMap).map(n => ({
    id: n.id,
    name: n.count > 1 && n.category === 'certificate' ? `${n.name} ×${n.count}` : n.name, // 同名证书标数量
    category: n.category === 'certificate' ? 0 : 1,
    symbolSize: Math.min(64, 30 + (degree[n.id] || 1) * 7), // 连接越多越大
  }))
  chart.setOption({
    tooltip: {},
    legend: [{ data: ['certificate', 'domain'], top: 8 }],
    series: [{
      type: 'graph', layout: 'force', roam: true, draggable: true,
      force: { repulsion: 520, edgeLength: 190, gravity: 0.05, friction: 0.3 }, // 拉大间距不重叠
      label: { show: true, position: 'right', fontSize: 12 },
      labelLayout: { hideOverlap: true }, // 标签重叠时自动隐藏
      emphasis: { focus: 'adjacency', lineStyle: { width: 3 } },
      categories: [{ name: 'certificate' }, { name: 'domain' }],
      data,
      links,
      lineStyle: { color: '#bbb', curveness: 0.12 },
    }],
  })
  chart.resize()
}
function onResize() { chart?.resize() }
onMounted(() => { window.addEventListener('resize', onResize); load() })
onBeforeUnmount(() => { window.removeEventListener('resize', onResize); chart?.dispose() })
</script>
