<template>
  <div>
    <!-- 配置栏 -->
    <div class="card filters">
      <div class="filter-row">
        <input class="input report-title-input" v-model="reportTitle" placeholder="报告标题（如：运维月报）" />
        <select class="input filter-select" v-model="reportType" @change="onReportTypeChange">
          <option value="monthly">月报</option>
          <option value="weekly">周报</option>
          <option value="custom">自定义</option>
        </select>
        <input type="date" class="input filter-date" v-model="dateFrom" />
        <span class="date-sep">至</span>
        <input type="date" class="input filter-date" v-model="dateTo" />
        <button class="btn btn-primary" @click="generateReport" :disabled="!canGenerate || generating">
          {{ generating ? '生成中...' : '生成报告' }}
        </button>
        <template v-if="reportData">
          <button class="btn btn-export" @click="exportWord">导出 Word</button>
          <button class="btn btn-export" @click="exportPDF">导出 PDF</button>
        </template>
        <button class="btn btn-icon" @click="showConfig = !showConfig" title="字段映射配置">
          <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><circle cx="12" cy="12" r="3"/><path d="M19.4 15a1.65 1.65 0 00.33 1.82l.06.06a2 2 0 010 2.83 2 2 0 01-2.83 0l-.06-.06a1.65 1.65 0 00-1.82-.33 1.65 1.65 0 00-1 1.51V21a2 2 0 01-2 2 2 2 0 01-2-2v-.09A1.65 1.65 0 009 19.4a1.65 1.65 0 00-1.82.33l-.06.06a2 2 0 01-2.83 0 2 2 0 010-2.83l.06-.06A1.65 1.65 0 004.68 15a1.65 1.65 0 00-1.51-1H3a2 2 0 01-2-2 2 2 0 012-2h.09A1.65 1.65 0 004.6 9a1.65 1.65 0 00-.33-1.82l-.06-.06a2 2 0 010-2.83 2 2 0 012.83 0l.06.06A1.65 1.65 0 009 4.68a1.65 1.65 0 001-1.51V3a2 2 0 012-2 2 2 0 012 2v.09a1.65 1.65 0 001 1.51 1.65 1.65 0 001.82-.33l.06-.06a2 2 0 012.83 0 2 2 0 010 2.83l-.06.06A1.65 1.65 0 0019.32 9a1.65 1.65 0 001.51 1H21a2 2 0 012 2 2 2 0 01-2 2h-.09a1.65 1.65 0 00-1.51 1z"/></svg>
        </button>
      </div>
    </div>

    <!-- 章节项目配置（始终可见） -->
    <div class="card section-projects">
      <div class="section-projects-header">
        <h4>章节数据来源</h4>
        <button class="btn btn-sm" @click="addSection">+ 添加章节</button>
      </div>
      <div class="section-projects-grid">
        <div v-for="(sec, sidx) in config.sections" :key="sec.id" class="section-project-item">
          <div class="sp-left">
            <input type="checkbox" v-model="sec.enabled" />
            <input class="input sp-title" v-model="sec.title" placeholder="章节标题" />
          </div>
          <div class="sp-middle">
            <select class="input sp-project" v-model="sec.project">
              <option value="">-- 选择项目 --</option>
              <option v-for="p in projects" :key="p.key" :value="p.key">{{ p.name }} ({{ p.key }})</option>
            </select>
            <input class="input sp-issuetype" v-model="sec.issuetype" placeholder="工单类型(可选)" />
          </div>
          <button class="btn btn-sm btn-danger" @click="removeSection(sidx)" title="删除">×</button>
        </div>
      </div>
      <div class="section-projects-actions">
        <button class="btn btn-sm btn-primary" @click="saveConfig">保存配置</button>
      </div>
    </div>

    <!-- 字段映射配置面板（高级） -->
    <div v-if="showConfig" class="card config-panel">
      <div class="config-header">
        <h4>字段映射配置</h4>
      </div>
      <div class="config-sections">
        <div v-for="sec in config.sections" :key="sec.id" class="config-section">
          <div class="section-header">
            <span class="section-label">{{ sec.title }} <span class="hint" v-if="sec.project">({{ sec.project }})</span></span>
          </div>
          <div class="config-fields-row" v-if="sec.filter !== undefined">
            <div class="config-field" style="flex:1">
              <label>额外 JQL 条件 <span class="hint">(可选)</span></label>
              <input class="input" v-model="sec.filter" placeholder="如: priority = High AND labels = production" />
            </div>
          </div>
          <div class="config-mappings">
            <div class="mapping-title">字段映射 <span class="hint">（报告列名 → Jira 字段）</span></div>
            <div v-for="(map, midx) in (config.field_mappings[sec.id] || [])" :key="midx" class="mapping-row">
              <input class="input mapping-col" v-model="map.column" placeholder="报告列名" />
              <span class="mapping-arrow">→</span>
              <select class="input mapping-field" v-model="map.field_id">
                <option value="">不映射</option>
                <option value="_key">工单 Key</option>
                <option value="_duration">时长(自动计算)</option>
                <optgroup label="标准字段">
                  <option v-for="f in fieldsStore.standardFields" :key="f.id" :value="f.id">{{ fieldsStore.getFieldLabel(f.id) }}</option>
                </optgroup>
                <optgroup label="自定义字段" v-if="fieldsStore.customFields.length">
                  <option v-for="f in fieldsStore.customFields" :key="f.id" :value="f.id">{{ f.name }}</option>
                </optgroup>
              </select>
              <button class="btn btn-sm btn-danger" @click="removeMapping(sec.id, midx)">×</button>
            </div>
            <button class="btn btn-sm" @click="addMapping(sec.id)">+ 添加列</button>
          </div>
        </div>
      </div>
      <div class="config-actions">
        <button class="btn btn-primary" @click="saveConfig">保存配置</button>
        <button class="btn" @click="showConfig = false">关闭</button>
      </div>
    </div>

    <!-- 加载状态 -->
    <div v-if="generating" class="loading"><div class="spinner"></div>正在从 Jira 获取数据...</div>

    <!-- 报告预览 -->
    <div v-if="reportData && !generating" class="report-wrapper">
      <div class="report-document" ref="reportRef">
        <div class="report-header">
          <h1>{{ reportTitle || '运维报告' }}</h1>
          <p class="report-period">报告周期: {{ dateFrom }} ~ {{ dateTo }}</p>
        </div>

        <!-- 各章节（每个章节统一显示：条数 + 数据表格） -->
        <template v-for="sec in config.sections" :key="sec.id">
          <div v-if="sec.enabled" class="report-section">
            <h2>{{ sectionIndex(sec) }}、{{ sec.title }}</h2>

            <div class="report-summary-text">
              <p>- 共 {{ sectionIssues(sec.id).length }} 条记录</p>
            </div>

            <!-- 数据表格 -->
            <table v-if="getSectionMappings(sec.id).length" class="report-table">
              <thead>
                <tr>
                  <th v-for="map in getSectionMappings(sec.id)" :key="map.column">{{ map.column }}</th>
                </tr>
              </thead>
              <tbody>
                <tr v-for="issue in sectionIssues(sec.id)" :key="issue.key">
                  <td v-for="map in getSectionMappings(sec.id)" :key="map.column">
                    {{ getCellValue(issue, map) }}
                  </td>
                </tr>
                <tr v-if="sectionIssues(sec.id).length === 0">
                  <td :colspan="getSectionMappings(sec.id).length" class="empty-cell">暂无数据</td>
                </tr>
              </tbody>
            </table>
          </div>
        </template>

        <!-- 汇总统计（固定在报告底部，聚合所有项目） -->
        <div v-if="allIssuesCount > 0" class="report-section">
          <h2>{{ enabledSectionCount + 1 }}、工单汇总统计</h2>
          <div class="report-summary-text">
            <p>- 新建工单: {{ allIssuesCount }}</p>
            <p>- 已解决: {{ resolvedCount }}</p>
            <p>- 解决率: {{ resolveRate }}%</p>
          </div>

          <h3>按经办人统计</h3>
          <table class="report-table">
            <thead>
              <tr><th>经办人</th><th>工单数</th><th>已解决</th></tr>
            </thead>
            <tbody>
              <tr v-for="row in assigneeStats" :key="row.name">
                <td>{{ row.name }}</td>
                <td>{{ row.total }}</td>
                <td>{{ row.resolved }}</td>
              </tr>
            </tbody>
          </table>

          <h3 style="margin-top:16px">按优先级统计</h3>
          <table class="report-table">
            <thead>
              <tr><th>优先级</th><th>工单数</th></tr>
            </thead>
            <tbody>
              <tr v-for="row in priorityStats" :key="row.name">
                <td>{{ row.name }}</td>
                <td>{{ row.count }}</td>
              </tr>
            </tbody>
          </table>
        </div>
      </div>
    </div>

    <!-- 空状态 -->
    <div v-if="!reportData && !generating" class="empty-state card">
      <p>配置章节数据来源后，点击「生成报告」</p>
      <p class="hint">每个章节可选择不同的 Jira 项目，报告会自动聚合多个项目的数据</p>
    </div>
  </div>
</template>

<script setup>
import { ref, computed, onMounted, inject } from 'vue'
import api from '@/api'
import { useFieldsStore } from '@/stores/fields'
import { Document, Packer, Paragraph, Table, TableRow, TableCell, TextRun, HeadingLevel, WidthType, AlignmentType, BorderStyle } from 'docx'
import { saveAs } from 'file-saver'

const toast = inject('toast')
const fieldsStore = useFieldsStore()

const projects = ref([])
const reportTitle = ref('运维月报')
const reportType = ref('monthly')
const dateFrom = ref('')
const dateTo = ref('')
const generating = ref(false)
const reportData = ref(null)
const showConfig = ref(false)
const reportRef = ref(null)

const config = ref({
  sections: [],
  field_mappings: {}
})

const reportTypeLabel = computed(() => {
  if (reportType.value === 'monthly') return '月报'
  if (reportType.value === 'weekly') return '周报'
  return '报告'
})

// 至少一个章节配置了项目
const canGenerate = computed(() => {
  return config.value.sections.some(s => s.enabled && s.project)
})

function toLocalDate(d) {
  return d.getFullYear() + '-' + String(d.getMonth() + 1).padStart(2, '0') + '-' + String(d.getDate()).padStart(2, '0')
}

function setMonthRange() {
  const now = new Date()
  dateFrom.value = toLocalDate(new Date(now.getFullYear(), now.getMonth(), 1))
  dateTo.value = toLocalDate(new Date(now.getFullYear(), now.getMonth() + 1, 0))
}

function setWeekRange() {
  const now = new Date()
  const day = now.getDay() || 7
  const monday = new Date(now)
  monday.setDate(now.getDate() - day + 1)
  const sunday = new Date(monday)
  sunday.setDate(monday.getDate() + 6)
  dateFrom.value = toLocalDate(monday)
  dateTo.value = toLocalDate(sunday)
}

function onReportTypeChange() {
  if (reportType.value === 'monthly') setMonthRange()
  else if (reportType.value === 'weekly') setWeekRange()
}

onMounted(async () => {
  await fieldsStore.fetchFields()
  setMonthRange()

  const [projectsRes, configRes] = await Promise.allSettled([
    api.get('/api/jira/projects'),
    api.get('/api/report-config')
  ])
  if (projectsRes.status === 'fulfilled') projects.value = projectsRes.value.data.data || []
  if (configRes.status === 'fulfilled' && configRes.value.data.data) {
    config.value = configRes.value.data.data
  }
})

function addSection() {
  const id = 'section_' + Date.now()
  config.value.sections.push({ id, title: '新章节', enabled: true, project: '', issuetype: '', filter: '' })
  config.value.field_mappings[id] = [
    { column: '工单Key', field_id: '_key' },
    { column: '标题', field_id: 'summary' },
    { column: '状态', field_id: 'status' },
    { column: '负责人', field_id: 'assignee' },
  ]
}

function removeSection(idx) {
  const sec = config.value.sections[idx]
  if (sec) {
    delete config.value.field_mappings[sec.id]
    config.value.sections.splice(idx, 1)
  }
}

function addMapping(sectionId) {
  if (!config.value.field_mappings[sectionId]) config.value.field_mappings[sectionId] = []
  config.value.field_mappings[sectionId].push({ column: '', field_id: '' })
}

function removeMapping(sectionId, idx) {
  config.value.field_mappings[sectionId].splice(idx, 1)
}

async function saveConfig() {
  try {
    await api.post('/api/report-config', config.value)
    toast?.('配置已保存', 'success')
  } catch (e) {
    toast?.('保存失败', 'error')
  }
}

async function generateReport() {
  generating.value = true
  reportData.value = null
  try {
    const params = new URLSearchParams({
      date_from: dateFrom.value,
      date_to: dateTo.value
    })
    const res = await api.get(`/api/jira/report?${params}`)
    reportData.value = res.data.data
  } catch (e) {
    if (e.response?.status !== 401) {
      toast?.('生成报告失败: ' + (e.response?.data?.error || e.message), 'error')
    }
  } finally {
    generating.value = false
  }
}

const defaultMappings = [
  { column: '工单Key', field_id: '_key' },
  { column: '标题', field_id: 'summary' },
  { column: '优先级', field_id: 'priority' },
  { column: '状态', field_id: 'status' },
  { column: '创建时间', field_id: 'created' },
  { column: '负责人', field_id: 'assignee' },
]

function getSectionMappings(sectionId) {
  const mappings = config.value.field_mappings[sectionId]
  if (mappings && mappings.length > 0) return mappings
  return defaultMappings
}

function sectionIssues(sectionId) {
  if (!reportData.value) return []
  const sec = reportData.value.sections?.find(s => s.id === sectionId)
  return sec?.issues || []
}

const enabledSectionCount = computed(() => {
  return config.value.sections.filter(s => s.enabled).length
})

function sectionIndex(sec) {
  const enabled = config.value.sections.filter(s => s.enabled)
  return enabled.indexOf(sec) + 1
}

// 格式化日期为 M.DD HH:mm
function formatShortDate(dateStr) {
  if (!dateStr) return '-'
  const d = new Date(dateStr)
  if (isNaN(d.getTime())) return '-'
  const m = d.getMonth() + 1
  const dd = String(d.getDate()).padStart(2, '0')
  const hh = String(d.getHours()).padStart(2, '0')
  const mm = String(d.getMinutes()).padStart(2, '0')
  return `${m}.${dd} ${hh}:${mm}`
}

// 从工单中提取单元格值
function getCellValue(issue, map) {
  if (!map.field_id) return '-'
  if (map.field_id === '_key') return issue.key
  if (map.field_id === '_duration') return calcDuration(issue)

  const val = issue.fields?.[map.field_id]

  if (['created', 'updated', 'resolutiondate', 'duedate'].includes(map.field_id) ||
      (typeof val === 'string' && /^\d{4}-\d{2}-\d{2}T/.test(val))) {
    return formatShortDate(val)
  }

  if (map.field_id === 'description') {
    if (!val) return '-'
    const text = String(val).replace(/<[^>]*>/g, '').replace(/\s+/g, ' ').trim()
    return text.length > 40 ? text.slice(0, 40) + '...' : text
  }

  return fieldsStore.getFieldDisplayValue(issue, map.field_id)
}

function calcDuration(issue) {
  const created = issue.fields?.created
  const resolved = issue.fields?.resolutiondate
  if (!created || !resolved) return '-'
  const ms = new Date(resolved) - new Date(created)
  if (isNaN(ms) || ms < 0) return '-'
  const totalMins = Math.round(ms / 60000)
  if (totalMins < 60) return totalMins + 'm'
  const hrs = Math.floor(totalMins / 60)
  if (hrs < 24) {
    const remainMins = totalMins % 60
    return hrs + 'h' + (remainMins > 0 ? remainMins + 'm' : '')
  }
  const days = (totalMins / 1440).toFixed(1)
  return days + 'd'
}

function totalDuration(sectionId) {
  const issues = sectionIssues(sectionId)
  let totalMins = 0
  for (const issue of issues) {
    const created = issue.fields?.created
    const resolved = issue.fields?.resolutiondate
    if (created && resolved) {
      totalMins += Math.round((new Date(resolved) - new Date(created)) / 60000)
    }
  }
  if (totalMins < 60) return totalMins + ' 分钟'
  return Math.floor(totalMins / 60) + ' 小时 ' + (totalMins % 60) + ' 分钟'
}

// 统计数据
const allIssuesCount = computed(() => {
  if (!reportData.value?.all_issues) return 0
  try {
    const d = typeof reportData.value.all_issues === 'string' ? JSON.parse(reportData.value.all_issues) : reportData.value.all_issues
    return d?.total || d?.issues?.length || 0
  } catch { return 0 }
})

const resolvedCount = computed(() => {
  if (!reportData.value?.resolved) return 0
  try {
    const d = typeof reportData.value.resolved === 'string' ? JSON.parse(reportData.value.resolved) : reportData.value.resolved
    return d?.total || d?.issues?.length || 0
  } catch { return 0 }
})

const resolveRate = computed(() => {
  if (allIssuesCount.value === 0) return 0
  return Math.round((resolvedCount.value / allIssuesCount.value) * 100)
})

function getAllIssues() {
  if (!reportData.value?.all_issues) return []
  try {
    const d = typeof reportData.value.all_issues === 'string' ? JSON.parse(reportData.value.all_issues) : reportData.value.all_issues
    return d?.issues || []
  } catch { return [] }
}

function getResolvedIssues() {
  if (!reportData.value?.resolved) return []
  try {
    const d = typeof reportData.value.resolved === 'string' ? JSON.parse(reportData.value.resolved) : reportData.value.resolved
    return d?.issues || []
  } catch { return [] }
}

const assigneeStats = computed(() => {
  const all = getAllIssues()
  const resolved = getResolvedIssues()
  const map = {}
  for (const i of all) {
    const name = i.fields?.assignee?.displayName || '未分配'
    if (!map[name]) map[name] = { name, total: 0, resolved: 0 }
    map[name].total++
  }
  for (const i of resolved) {
    const name = i.fields?.assignee?.displayName || '未分配'
    if (!map[name]) map[name] = { name, total: 0, resolved: 0 }
    map[name].resolved++
  }
  return Object.values(map).sort((a, b) => b.total - a.total)
})

const priorityStats = computed(() => {
  const all = getAllIssues()
  const map = {}
  for (const i of all) {
    const name = i.fields?.priority?.name || '未知'
    map[name] = (map[name] || 0) + 1
  }
  return Object.entries(map).sort((a, b) => b[1] - a[1]).map(([name, count]) => ({ name, count }))
})

// ===== Word 导出 =====
async function exportWord() {
  const children = []

  children.push(new Paragraph({
    text: reportTitle.value || '运维报告',
    heading: HeadingLevel.HEADING_1,
    alignment: AlignmentType.CENTER,
  }))
  children.push(new Paragraph({
    text: `报告周期: ${dateFrom.value} ~ ${dateTo.value}`,
    alignment: AlignmentType.CENTER,
    spacing: { after: 300 },
  }))

  let secIdx = 0
  for (const sec of config.value.sections) {
    if (!sec.enabled) continue
    const issues = sectionIssues(sec.id)
    secIdx++

    children.push(new Paragraph({
      text: `${secIdx}、${sec.title}`,
      heading: HeadingLevel.HEADING_2,
      spacing: { before: 200 },
    }))

    children.push(new Paragraph({ text: `- 共 ${issues.length} 条记录` }))

    const mappings = getSectionMappings(sec.id)
    if (mappings.length > 0) {
      const borderConfig = {
        top: { style: BorderStyle.SINGLE, size: 1 },
        bottom: { style: BorderStyle.SINGLE, size: 1 },
        left: { style: BorderStyle.SINGLE, size: 1 },
        right: { style: BorderStyle.SINGLE, size: 1 },
      }
      const headerRow = new TableRow({
        children: mappings.map(m => new TableCell({
          children: [new Paragraph({ children: [new TextRun({ text: m.column, bold: true, size: 20 })] })],
          borders: borderConfig,
          width: { size: Math.floor(9000 / mappings.length), type: WidthType.DXA },
        }))
      })
      const dataRows = issues.length > 0
        ? issues.map(issue => new TableRow({
            children: mappings.map(m => new TableCell({
              children: [new Paragraph({ children: [new TextRun({ text: getCellValue(issue, m), size: 20 })] })],
              borders: borderConfig,
            }))
          }))
        : [new TableRow({
            children: [new TableCell({
              children: [new Paragraph({ children: [new TextRun({ text: '暂无数据', size: 20 })] })],
              borders: borderConfig,
              columnSpan: mappings.length,
            })]
          })]
      children.push(new Paragraph({ spacing: { before: 100 } }))
      children.push(new Table({
        rows: [headerRow, ...dataRows],
        width: { size: 9000, type: WidthType.DXA },
      }))
    }
  }

  // 汇总统计
  if (allIssuesCount.value > 0) {
    secIdx++
    children.push(new Paragraph({
      text: `${secIdx}、工单汇总统计`,
      heading: HeadingLevel.HEADING_2,
      spacing: { before: 200 },
    }))
    children.push(new Paragraph({ text: `- 新建工单: ${allIssuesCount.value}` }))
    children.push(new Paragraph({ text: `- 已解决: ${resolvedCount.value}` }))
    children.push(new Paragraph({ text: `- 解决率: ${resolveRate.value}%` }))

    children.push(new Paragraph({
      text: '按经办人统计',
      heading: HeadingLevel.HEADING_3,
      spacing: { before: 200 },
    }))
    const borderConfig = {
      top: { style: BorderStyle.SINGLE, size: 1 },
      bottom: { style: BorderStyle.SINGLE, size: 1 },
      left: { style: BorderStyle.SINGLE, size: 1 },
      right: { style: BorderStyle.SINGLE, size: 1 },
    }
    const hRow = new TableRow({
      children: ['经办人', '工单数', '已解决'].map(t => new TableCell({
        children: [new Paragraph({ children: [new TextRun({ text: t, bold: true, size: 20 })] })],
        borders: borderConfig,
        width: { size: 3000, type: WidthType.DXA },
      }))
    })
    const dRows = assigneeStats.value.map(r => new TableRow({
      children: [r.name, String(r.total), String(r.resolved)].map(t => new TableCell({
        children: [new Paragraph({ children: [new TextRun({ text: t, size: 20 })] })],
        borders: borderConfig,
      }))
    }))
    children.push(new Table({ rows: [hRow, ...dRows], width: { size: 9000, type: WidthType.DXA } }))
  }

  const doc = new Document({
    sections: [{ children }]
  })

  const blob = await Packer.toBlob(doc)
  saveAs(blob, `${reportTitle.value || '运维报告'}_${dateFrom.value}.docx`)
  toast?.('Word 文档已导出', 'success')
}

// ===== PDF 导出 =====
async function exportPDF() {
  const html2pdf = (await import('html2pdf.js')).default
  const element = reportRef.value
  if (!element) return

  html2pdf().set({
    margin: 10,
    filename: `${reportTitle.value || '运维报告'}_${dateFrom.value}.pdf`,
    image: { type: 'jpeg', quality: 0.98 },
    html2canvas: { scale: 2, useCORS: true },
    jsPDF: { unit: 'mm', format: 'a4', orientation: 'landscape' }
  }).from(element).save()

  toast?.('PDF 已导出', 'success')
}
</script>

<style scoped>
.filters { margin-bottom: 12px; }
.filter-row { display: flex; gap: 8px; align-items: center; flex-wrap: wrap; }
.report-title-input { width: 200px; font-weight: 600; }
.filter-select { max-width: 120px; }
.filter-date { max-width: 160px; }
.date-sep { color: var(--text-muted); font-size: 13px; }

.btn-export {
  background: rgba(16,185,129,0.12);
  color: #10B981;
  border: 1px solid rgba(16,185,129,0.3);
}
.btn-export:hover { background: rgba(16,185,129,0.2); }

.btn-icon {
  display: flex; align-items: center; justify-content: center;
  width: 36px; height: 36px; padding: 0;
  background: var(--card-bg); border: 1px solid var(--border); border-radius: var(--radius);
  color: var(--text-secondary); cursor: pointer;
}
.btn-icon:hover { color: var(--primary-light); border-color: var(--primary-light); }

/* 章节项目配置 */
.section-projects { margin-bottom: 12px; }
.section-projects-header { display: flex; align-items: center; justify-content: space-between; margin-bottom: 10px; }
.section-projects-header h4 { font-size: 14px; margin: 0; color: var(--text-primary); }
.section-projects-grid { display: flex; flex-direction: column; gap: 6px; }
.section-project-item {
  display: flex; align-items: center; gap: 8px;
  padding: 6px 8px; background: rgba(0,0,0,0.1); border-radius: var(--radius);
}
.sp-left { display: flex; align-items: center; gap: 6px; min-width: 200px; }
.sp-title { width: 160px; font-size: 13px; font-weight: 600; }
.sp-middle { display: flex; align-items: center; gap: 6px; flex: 1; }
.sp-project { min-width: 200px; font-size: 13px; }
.sp-issuetype { width: 140px; font-size: 13px; }
.section-projects-actions { margin-top: 8px; }

/* 配置面板 */
.config-panel { margin-bottom: 16px; }
.config-header { display: flex; align-items: center; justify-content: space-between; margin-bottom: 12px; }
.config-header h4 { font-size: 15px; margin: 0; }
.config-sections { display: flex; flex-direction: column; gap: 16px; }
.config-section { padding: 12px; background: rgba(0,0,0,0.15); border-radius: var(--radius); }
.section-header { margin-bottom: 8px; }
.section-label { font-size: 14px; font-weight: 600; color: var(--text-primary); }
.section-label .hint { font-weight: 400; font-size: 12px; color: var(--text-muted); }
.config-fields-row { display: flex; gap: 12px; margin-bottom: 8px; }
.config-field { margin-bottom: 8px; }
.config-field label { font-size: 12px; color: var(--text-muted); display: block; margin-bottom: 4px; }
.config-field .hint { font-size: 11px; color: var(--text-muted); opacity: 0.7; }
.config-field .input { font-family: var(--font-mono); font-size: 12px; }
.config-mappings { margin-top: 8px; }
.mapping-title { font-size: 12px; color: var(--text-muted); margin-bottom: 6px; font-weight: 600; }
.mapping-title .hint { font-weight: 400; opacity: 0.7; }
.mapping-row { display: flex; gap: 6px; align-items: center; margin-bottom: 4px; }
.mapping-col { width: 140px; font-size: 13px; }
.mapping-arrow { color: var(--text-muted); font-size: 13px; }
.mapping-field { flex: 1; font-size: 13px; }
.btn-danger { background: rgba(239,68,68,0.15); color: #EF4444; border: 1px solid rgba(239,68,68,0.3); width: 28px; height: 28px; padding: 0; display: flex; align-items: center; justify-content: center; }
.config-actions { display: flex; gap: 8px; margin-top: 12px; }

/* 报告预览 */
.report-wrapper { display: flex; justify-content: center; }
.report-document {
  background: #ffffff;
  color: #1a1a1a;
  width: 290mm;
  min-height: 210mm;
  padding: 15mm 12mm;
  box-shadow: 0 4px 24px rgba(0,0,0,0.3);
  border-radius: 2px;
  font-family: 'SimSun', 'Songti SC', serif;
  font-size: 13px;
  line-height: 1.6;
  overflow: hidden;
}

.report-header { text-align: center; margin-bottom: 30px; border-bottom: 2px solid #333; padding-bottom: 16px; }
.report-header h1 { font-size: 22px; font-weight: 700; color: #000; margin-bottom: 8px; font-family: 'SimHei', 'Heiti SC', sans-serif; }
.report-period { font-size: 14px; color: #555; }

.report-section { margin-bottom: 24px; }
.report-section h2 { font-size: 16px; font-weight: 700; margin-bottom: 12px; color: #000; font-family: 'SimHei', 'Heiti SC', sans-serif; }
.report-section h3 { font-size: 14px; font-weight: 700; margin-bottom: 8px; color: #333; }

.report-summary-text { margin-bottom: 12px; }
.report-summary-text p { margin: 2px 0; }

.report-table {
  width: 100%;
  border-collapse: collapse;
  margin: 8px 0 16px;
  font-size: 12px;
  table-layout: auto;
}
.report-table th, .report-table td {
  border: 1px solid #333;
  padding: 6px 8px;
  text-align: left;
  vertical-align: top;
  word-break: break-all;
  color: #1a1a1a;
}
.report-table th {
  background: #f0f0f0;
  font-weight: 700;
  font-size: 12px;
  white-space: nowrap;
}
.report-table td { background: #fff; }

.empty-cell { text-align: center; color: #999; font-style: italic; padding: 16px 8px; }
.empty-state { text-align: center; padding: 60px 20px; color: var(--text-secondary); }
.empty-state .hint { font-size: 13px; color: var(--text-muted); margin-top: 8px; }
</style>
