import { defineStore } from 'pinia'
import { ref, computed } from 'vue'
import api from '@/api'

// 标准字段的显示名映射
const BUILTIN_LABELS = {
  summary: '摘要',
  status: '状态',
  priority: '优先级',
  assignee: '负责人',
  reporter: '报告人',
  creator: '创建人',
  issuetype: '工单类型',
  project: '项目',
  resolution: '解决方案',
  created: '创建时间',
  updated: '更新时间',
  resolutiondate: '解决时间',
  duedate: '截止日期',
  labels: '标签',
  components: '组件',
  fixVersions: '修复版本',
  description: '描述',
  environment: '环境',
  attachment: '附件',
  comment: '评论',
  votes: '投票',
  watches: '关注',
  worklog: '工时',
  timetracking: '时间追踪',
  subtasks: '子任务',
  issuelinks: '关联',
  parent: '父工单',
}

// 默认显示的列（工单列表）
const DEFAULT_COLUMNS = ['issuetype', 'summary', 'status', 'priority', 'assignee', 'reporter', 'created', 'updated']

// 默认统计维度
const DEFAULT_STAT_FIELDS = ['status', 'priority', 'assignee']

// 不适合显示为列或统计的字段
const EXCLUDED_FIELDS = new Set([
  'description', 'comment', 'attachment', 'worklog', 'timetracking',
  'subtasks', 'issuelinks', 'parent', 'thumbnail', 'watches', 'votes',
  'progress', 'aggregateprogress', 'workratio', 'lastViewed',
])

export const useFieldsStore = defineStore('fields', () => {
  const allFields = ref([])
  const loaded = ref(false)
  const loading = ref(false)

  // 标准字段
  const standardFields = computed(() =>
    allFields.value.filter(f => !f.custom && !EXCLUDED_FIELDS.has(f.id))
  )

  // 自定义字段
  const customFields = computed(() =>
    allFields.value.filter(f => f.custom)
  )

  // 可作为列显示的字段（标准+自定义，排除不适合的）
  const columnFields = computed(() =>
    allFields.value.filter(f => !EXCLUDED_FIELDS.has(f.id))
  )

  // 可作为统计维度的字段（需要是可聚合的类型）
  const statFields = computed(() => {
    const aggregatable = new Set(['option', 'user', 'string', 'array', 'resolution', 'status', 'priority', 'issuetype', 'project', 'component', 'version'])
    return allFields.value.filter(f => {
      if (EXCLUDED_FIELDS.has(f.id)) return false
      const schemaType = f.schema?.type
      const schemaSystem = f.schema?.system
      // 标准可聚合字段
      if (schemaSystem && ['status', 'priority', 'assignee', 'reporter', 'issuetype', 'resolution', 'components', 'labels', 'fixVersions', 'project', 'creator'].includes(schemaSystem)) return true
      // 自定义字段：select, multiselect, user, radio 等
      if (f.custom) {
        const ct = f.schema?.custom || ''
        if (ct.includes('select') || ct.includes('radio') || ct.includes('user') || ct.includes('multiselect') || ct.includes('labels') || ct.includes('cascadingselect')) return true
        if (schemaType === 'option' || schemaType === 'array' || schemaType === 'user') return true
      }
      return false
    })
  })

  // 获取字段显示名
  function getFieldLabel(fieldId) {
    if (BUILTIN_LABELS[fieldId]) return BUILTIN_LABELS[fieldId]
    const field = allFields.value.find(f => f.id === fieldId)
    return field?.name || fieldId
  }

  // 从工单 fields 中提取字段的显示值
  function getFieldDisplayValue(issue, fieldId) {
    const val = issue.fields?.[fieldId]
    if (val === null || val === undefined) return '-'

    // 对象类型（有 name 或 displayName）
    if (typeof val === 'object' && !Array.isArray(val)) {
      return val.displayName || val.name || val.value || JSON.stringify(val)
    }

    // 数组类型
    if (Array.isArray(val)) {
      if (val.length === 0) return '-'
      return val.map(v => {
        if (typeof v === 'string') return v
        return v.displayName || v.name || v.value || String(v)
      }).join(', ')
    }

    // 日期字段
    if (typeof val === 'string' && /^\d{4}-\d{2}-\d{2}/.test(val)) {
      return new Date(val).toLocaleDateString('zh-CN')
    }

    return String(val)
  }

  // 从工单 fields 中提取字段值用于统计聚合
  function getFieldGroupValue(issue, fieldId) {
    const val = issue.fields?.[fieldId]
    if (val === null || val === undefined) return null

    // 对象类型
    if (typeof val === 'object' && !Array.isArray(val)) {
      return val.displayName || val.name || val.value || null
    }

    // 数组类型 → 返回数组（多值字段，每个值都计一次）
    if (Array.isArray(val)) {
      if (val.length === 0) return null
      return val.map(v => {
        if (typeof v === 'string') return v
        return v.displayName || v.name || v.value || String(v)
      })
    }

    return String(val)
  }

  async function fetchFields() {
    if (loaded.value || loading.value) return
    loading.value = true
    try {
      const res = await api.get('/api/jira/fields')
      const raw = res.data.data || []
      allFields.value = raw.map(f => ({
        id: f.id,
        name: f.name,
        custom: f.custom || false,
        schema: f.schema || null,
        clauseNames: f.clauseNames || [],
      }))
      loaded.value = true
    } catch (e) {
      console.error('获取字段列表失败:', e)
    } finally {
      loading.value = false
    }
  }

  // 获取用户保存的列配置，或返回默认值
  function getSavedColumns() {
    try {
      const saved = localStorage.getItem('jira-center-columns')
      if (saved) return JSON.parse(saved)
    } catch (e) { /* ignore */ }
    return [...DEFAULT_COLUMNS]
  }

  function saveColumns(cols) {
    localStorage.setItem('jira-center-columns', JSON.stringify(cols))
  }

  function getSavedStatFields() {
    try {
      const saved = localStorage.getItem('jira-center-stat-fields')
      if (saved) return JSON.parse(saved)
    } catch (e) { /* ignore */ }
    return [...DEFAULT_STAT_FIELDS]
  }

  function saveStatFields(fields) {
    localStorage.setItem('jira-center-stat-fields', JSON.stringify(fields))
  }

  return {
    allFields, loaded, loading,
    standardFields, customFields, columnFields, statFields,
    getFieldLabel, getFieldDisplayValue, getFieldGroupValue,
    fetchFields,
    getSavedColumns, saveColumns,
    getSavedStatFields, saveStatFields,
    DEFAULT_COLUMNS, DEFAULT_STAT_FIELDS,
  }
})
