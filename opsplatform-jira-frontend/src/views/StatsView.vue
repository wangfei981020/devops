<template>
  <div>
    <!-- 筛选栏 -->
    <div class="card filters">
      <div class="filter-row">
        <select class="input filter-select" v-model="project" @change="onProjectChange">
          <option value="">所有项目</option>
          <option v-for="p in projects" :key="p.key" :value="p.key">{{ p.name }} ({{ p.key }})</option>
        </select>
        <select class="input filter-select" v-model="dateMode">
          <option value="relative">相对时间</option>
          <option value="absolute">指定日期</option>
        </select>
        <template v-if="dateMode === 'relative'">
          <select class="input filter-select" v-model="days">
            <option value="7">近 7 天</option>
            <option value="14">近 14 天</option>
            <option value="30">近 30 天</option>
            <option value="90">近 90 天</option>
            <option value="180">近 180 天</option>
            <option value="365">近 1 年</option>
          </select>
        </template>
        <template v-else>
          <input type="date" class="input filter-date" v-model="dateFrom" />
          <span class="date-sep">至</span>
          <input type="date" class="input filter-date" v-model="dateTo" />
        </template>
        <button class="btn btn-primary" @click="loadStats">查询</button>
      </div>
      <div class="filter-row" style="margin-top:8px">
        <label class="filter-label">图表维度：</label>
        <label v-for="f in availableStatFields" :key="f.id" class="checkbox-inline">
          <input type="checkbox" v-model="selectedFields" :value="f.id" @change="onFieldsChange" />
          {{ f.label }}
        </label>
      </div>
    </div>

    <div v-if="loading" class="loading"><div class="spinner"></div>加载中...</div>

    <template v-else>
      <!-- 概览卡片 -->
      <div class="summary-cards">
        <div class="summary-card">
          <div class="summary-value">{{ summary.created }}</div>
          <div class="summary-label">创建工单</div>
        </div>
        <div class="summary-card">
          <div class="summary-value">{{ summary.resolved }}</div>
          <div class="summary-label">已解决</div>
        </div>
        <div class="summary-card">
          <div class="summary-value">{{ summary.open }}</div>
          <div class="summary-label">未解决</div>
        </div>
        <div class="summary-card">
          <div class="summary-value">{{ summary.resolveRate }}%</div>
          <div class="summary-label">解决率</div>
        </div>
      </div>

      <div class="charts-grid">
        <!-- 创建/解决趋势 (固定) -->
        <div class="card chart-card wide">
          <h3>创建 / 解决趋势</h3>
          <v-chart class="chart" :option="trendChartOption" autoresize />
        </div>

        <!-- 动态维度图表 -->
        <template v-for="fieldId in selectedFields" :key="fieldId">
          <!-- 负责人特殊：柱状图 + 矩阵 -->
          <template v-if="fieldId === 'assignee'">
            <div class="card chart-card">
              <h3>{{ getStatFieldLabel(fieldId) }}工单数</h3>
              <v-chart class="chart" :option="makeBarOption(fieldId)" autoresize />
            </div>
            <div class="card chart-card wide">
              <h3>{{ getStatFieldLabel(fieldId) }}× 状态矩阵</h3>
              <v-chart class="chart chart-tall" :option="makeMatrixOption(fieldId)" autoresize />
            </div>
          </template>
          <!-- 其他字段：饼图 -->
          <div v-else class="card chart-card">
            <h3>{{ getStatFieldLabel(fieldId) }}分布</h3>
            <v-chart class="chart" :option="makePieOption(fieldId)" autoresize />
          </div>
        </template>
      </div>
    </template>
  </div>
</template>

<script setup>
import { ref, computed, onMounted } from 'vue'
import { use } from 'echarts/core'
import { PieChart, BarChart, LineChart } from 'echarts/charts'
import { TitleComponent, TooltipComponent, LegendComponent, GridComponent, DatasetComponent } from 'echarts/components'
import { CanvasRenderer } from 'echarts/renderers'
import VChart from 'vue-echarts'
import api from '@/api'
import { useFieldsStore } from '@/stores/fields'

use([PieChart, BarChart, LineChart, TitleComponent, TooltipComponent, LegendComponent, GridComponent, DatasetComponent, CanvasRenderer])

const fieldsStore = useFieldsStore()
const loading = ref(true)
const projects = ref([])
const project = ref('')
const dateMode = ref('relative')
const days = ref('30')
const dateFrom = ref('')
const dateTo = ref('')

const selectedFields = ref([])

// 可用的统计维度（标准 + 自定义）
const availableStatFields = computed(() => {
  if (!fieldsStore.loaded) return []
  return fieldsStore.statFields.map(f => ({
    id: f.id,
    label: fieldsStore.getFieldLabel(f.id),
  }))
})

function getStatFieldLabel(fieldId) {
  return fieldsStore.getFieldLabel(fieldId)
}

const rawCreated = ref([])
const rawResolved = ref([])
const rawOpen = ref([])
const rawAll = ref([])

const chartColors = ['#3B82F6', '#F59E0B', '#10B981', '#EF4444', '#8B5CF6', '#EC4899', '#06B6D4', '#84CC16', '#F97316', '#6366F1']
const textColor = '#94A3B8'
const gridColor = '#334155'

onMounted(async () => {
  const now = new Date()
  dateTo.value = now.toISOString().slice(0, 10)
  dateFrom.value = new Date(now - 30 * 86400000).toISOString().slice(0, 10)

  await fieldsStore.fetchFields()
  selectedFields.value = fieldsStore.getSavedStatFields()

  try {
    const res = await api.get('/api/jira/projects')
    projects.value = res.data.data || []
  } catch (e) { /* ignore */ }
  loadStats()
})

function onProjectChange() { loadStats() }

function onFieldsChange() {
  fieldsStore.saveStatFields(selectedFields.value)
}

async function loadStats() {
  loading.value = true
  try {
    const params = new URLSearchParams()
    if (project.value) params.set('project', project.value)
    if (dateMode.value === 'absolute' && dateFrom.value && dateTo.value) {
      params.set('date_from', dateFrom.value)
      params.set('date_to', dateTo.value)
    } else {
      params.set('days', days.value)
    }

    const res = await api.get(`/api/jira/stats?${params}`)
    const data = res.data.data
    const queries = data?.queries || []

    rawCreated.value = parseIssues(queries.find(r => r.label === 'created_trend')?.data)
    rawResolved.value = parseIssues(queries.find(r => r.label === 'resolved_trend')?.data)
    rawOpen.value = parseIssues(queries.find(r => r.label === 'open_issues')?.data)
    rawAll.value = parseIssues(queries.find(r => r.label === 'all_in_range')?.data)
  } catch (e) {
    console.error('加载统计失败:', e)
  } finally {
    loading.value = false
  }
}

function parseIssues(raw) {
  if (!raw?.issues) return []
  return raw.issues
}

// 概览
const summary = computed(() => {
  const created = rawCreated.value.length
  const resolved = rawResolved.value.length
  const open = rawOpen.value.length
  const rate = created > 0 ? Math.round((resolved / created) * 100) : 0
  return { created, resolved, open, resolveRate: rate }
})

// 按日期分组
function groupByDate(issues, field) {
  const map = {}
  for (const i of issues) {
    const d = i.fields?.[field]
    if (!d) continue
    const day = d.substring(0, 10)
    map[day] = (map[day] || 0) + 1
  }
  return Object.entries(map).sort().map(([date, count]) => ({ date, count }))
}

// 通用按字段分组（使用 fieldsStore 的方法）
function groupByField(issues, fieldId) {
  const map = {}
  for (const i of issues) {
    const val = fieldsStore.getFieldGroupValue(i, fieldId)
    if (val === null) {
      const fallback = fieldId === 'resolution' ? '未解决' : fieldId === 'assignee' ? '未分配' : '未知'
      map[fallback] = (map[fallback] || 0) + 1
      continue
    }
    // 多值字段
    if (Array.isArray(val)) {
      for (const v of val) { map[v] = (map[v] || 0) + 1 }
    } else {
      map[val] = (map[val] || 0) + 1
    }
  }
  return Object.entries(map).sort((a, b) => b[1] - a[1])
}

// 趋势图
const trendChartOption = computed(() => {
  const created = groupByDate(rawCreated.value, 'created')
  const resolved = groupByDate(rawResolved.value, 'resolved')
  const allDates = [...new Set([...created.map(d => d.date), ...resolved.map(d => d.date)])].sort()

  return {
    tooltip: { trigger: 'axis' },
    legend: { data: ['创建', '解决'], textStyle: { color: textColor } },
    grid: { left: 50, right: 30, top: 40, bottom: 30 },
    xAxis: { type: 'category', data: allDates, axisLabel: { color: textColor, fontSize: 11 } },
    yAxis: { type: 'value', splitLine: { lineStyle: { color: gridColor } }, axisLabel: { color: textColor }, minInterval: 1 },
    series: [
      { name: '创建', type: 'line', data: allDates.map(d => created.find(c => c.date === d)?.count || 0), smooth: true, itemStyle: { color: '#3B82F6' }, areaStyle: { color: 'rgba(59,130,246,0.1)' } },
      { name: '解决', type: 'line', data: allDates.map(d => resolved.find(c => c.date === d)?.count || 0), smooth: true, itemStyle: { color: '#10B981' }, areaStyle: { color: 'rgba(16,185,129,0.1)' } },
    ]
  }
})

// 通用饼图
function makePieOption(fieldId) {
  const entries = groupByField(rawAll.value, fieldId)
  return {
    tooltip: { trigger: 'item', formatter: '{b}: {c} ({d}%)' },
    color: chartColors,
    legend: { orient: 'vertical', right: 10, top: 'center', textStyle: { color: textColor, fontSize: 12 } },
    series: [{
      type: 'pie',
      radius: ['35%', '65%'],
      center: ['35%', '50%'],
      data: entries.map(([name, value]) => ({ name, value })),
      label: { show: false },
      emphasis: { label: { show: true, fontSize: 14, fontWeight: 'bold' } }
    }]
  }
}

// 通用柱状图
function makeBarOption(fieldId) {
  const entries = groupByField(rawAll.value, fieldId).slice(0, 15)
  return {
    tooltip: { trigger: 'axis' },
    grid: { left: 120, right: 30, top: 10, bottom: 20 },
    xAxis: { type: 'value', splitLine: { lineStyle: { color: gridColor } }, axisLabel: { color: textColor }, minInterval: 1 },
    yAxis: { type: 'category', data: entries.map(e => e[0]).reverse(), axisLabel: { color: textColor, fontSize: 12 } },
    series: [{ type: 'bar', data: entries.map(e => e[1]).reverse(), itemStyle: { color: '#F59E0B', borderRadius: [0, 4, 4, 0] }, barMaxWidth: 24 }]
  }
}

// 通用矩阵图（fieldId × status）
function makeMatrixOption(fieldId) {
  const issues = rawAll.value
  const groupMap = {}
  const statusSet = new Set()
  for (const i of issues) {
    const val = fieldsStore.getFieldGroupValue(i, fieldId)
    const groupName = val === null ? '未分配' : Array.isArray(val) ? val[0] : val
    const s = i.fields?.status?.name || '未知'
    statusSet.add(s)
    if (!groupMap[groupName]) groupMap[groupName] = {}
    groupMap[groupName][s] = (groupMap[groupName][s] || 0) + 1
  }
  const groups = Object.entries(groupMap).sort((a, b) => {
    const totalA = Object.values(a[1]).reduce((s, v) => s + v, 0)
    const totalB = Object.values(b[1]).reduce((s, v) => s + v, 0)
    return totalB - totalA
  }).slice(0, 15).map(e => e[0]).reverse()
  const statuses = [...statusSet]

  return {
    tooltip: { trigger: 'axis', axisPointer: { type: 'shadow' } },
    legend: { data: statuses, textStyle: { color: textColor, fontSize: 11 } },
    grid: { left: 120, right: 30, top: 30, bottom: 20 },
    xAxis: { type: 'value', splitLine: { lineStyle: { color: gridColor } }, axisLabel: { color: textColor }, minInterval: 1 },
    yAxis: { type: 'category', data: groups, axisLabel: { color: textColor, fontSize: 12 } },
    color: chartColors,
    series: statuses.map(s => ({
      name: s,
      type: 'bar',
      stack: 'total',
      data: groups.map(g => groupMap[g]?.[s] || 0),
      barMaxWidth: 22,
    }))
  }
}
</script>

<style scoped>
.filters { margin-bottom: 16px; }
.filter-row { display: flex; gap: 8px; align-items: center; flex-wrap: wrap; }
.filter-select { max-width: 200px; }
.filter-date { max-width: 160px; }
.date-sep { color: var(--text-muted); font-size: 13px; }
.filter-label { font-size: 13px; color: var(--text-secondary); font-weight: 500; margin-right: 4px; }
.checkbox-inline { display: flex; align-items: center; gap: 4px; font-size: 13px; color: var(--text-secondary); cursor: pointer; }
.checkbox-inline input { cursor: pointer; }

.summary-cards { display: grid; grid-template-columns: repeat(4, 1fr); gap: 12px; margin-bottom: 16px; }
.summary-card {
  background: var(--card-bg);
  border: 1px solid var(--border);
  border-radius: var(--radius);
  padding: 16px 20px;
  text-align: center;
}
.summary-value { font-size: 28px; font-weight: 700; color: var(--primary-light); }
.summary-label { font-size: 13px; color: var(--text-secondary); margin-top: 4px; }

.charts-grid { display: grid; grid-template-columns: 1fr 1fr; gap: 16px; }
.chart-card { min-height: 320px; }
.chart-card h3 { font-size: 14px; font-weight: 600; color: var(--text-secondary); margin-bottom: 12px; }
.chart-card.wide { grid-column: 1 / -1; }
.chart { width: 100%; height: 260px; }
.chart-tall { height: 320px; }

@media (max-width: 768px) {
  .summary-cards { grid-template-columns: repeat(2, 1fr); }
  .charts-grid { grid-template-columns: 1fr; }
}
</style>
