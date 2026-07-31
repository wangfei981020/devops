<template>
  <div class="page">
    <div class="page-head"><span class="page-title">关系图谱</span><el-button :icon="Refresh" @click="load">刷新</el-button></div>
    <el-card shadow="never" v-loading="loading">
      <!-- 出错必须显式说出来。之前 load() 无 try/catch 且 edges 可能是非数组，
           模板里 edges.length 直接抛 TypeError，整个 card 渲染失败，
           页面只剩 card 外的标题和刷新按钮，看上去像「功能是空的」。 -->
      <el-alert v-if="error" type="error" :closable="false" show-icon style="margin-bottom:10px">
        <template #title>关系图加载失败</template>
        {{ error }}
      </el-alert>
      <div v-show="!error && edges.length" ref="chartEl" style="width:100%;height:560px"></div>
      <el-empty v-if="!loading && !error && !edges.length" :image-size="70"
        description="暂无关系数据" >
        <div class="muted" style="max-width:520px;line-height:1.7">
          当前只建立「证书 → 域名」一种关系，申请/绑定证书时自动生成。
          物理机→虚拟机→容器→应用的全链路关系尚未接入，所以这里为空属正常，不是故障。
        </div>
      </el-empty>
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
const loading = ref(false)
const error = ref('')
let chart = null

async function load() {
  loading.value = true
  error.value = ''
  try {
    const r = await listRelations()
    // 接口可能返回 null / 对象；不归一成数组的话 edges.length 会在模板里抛错，整页白掉
    edges.value = Array.isArray(r) ? r : (Array.isArray(r?.data) ? r.data : [])
    await nextTick()
    if (edges.value.length) render()
  } catch (e) {
    error.value = e?.response?.data?.error || e.message || String(e)
    edges.value = []
  } finally {
    loading.value = false
  }
}
function render() {
  try {
    renderInner()
  } catch (e) {
    error.value = '图表渲染失败：' + (e.message || String(e))
  }
}
function renderInner() {
  if (!chartEl.value) return
  if (!chart) chart = echarts.init(chartEl.value)
  // 按 类型|名称 合并同名节点（多张同名证书合并成一个，去掉堆叠）
  const nodeMap = {}   // key -> node，key=type|name
  const degree = {}    // key -> 连接数
  const linkSet = new Set()
  const links = []
  const keyOf = (type, name) => type + '|' + name
  for (const e of (edges.value || [])) {
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
