<template>
  <div>
    <!-- 筛选栏 -->
    <div class="filters card">
      <div class="filter-row">
        <select class="input filter-select" v-model="filters.project">
          <option value="">所有项目</option>
          <option v-for="p in projects" :key="p.key" :value="p.key">{{ p.name }} ({{ p.key }})</option>
        </select>
        <select class="input filter-select" v-model="filters.status">
          <option value="">所有状态</option>
          <option v-for="s in statuses" :key="s.name" :value="s.name">{{ s.name }}</option>
        </select>
        <select class="input filter-select" v-model="filters.priority">
          <option value="">所有优先级</option>
          <option v-for="p in priorities" :key="p.name" :value="p.name">{{ p.name }}</option>
        </select>
        <input class="input filter-input" v-model="filters.assignee" placeholder="负责人" />
        <input class="input filter-input" v-model="filters.keyword" placeholder="关键词搜索" @keyup.enter="search" />
        <button class="btn btn-primary" @click="search">搜索</button>
        <button class="btn btn-icon" @click="showColumnPicker = !showColumnPicker" title="选择显示列">
          <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M12 3h7a2 2 0 012 2v14a2 2 0 01-2 2h-7m0-18H5a2 2 0 00-2 2v14a2 2 0 002 2h7m0-18v18"/></svg>
        </button>
      </div>
      <div class="filter-row jql-row">
        <input class="input" v-model="customJQL" placeholder="自定义 JQL 查询（优先级高于上方筛选）" @keyup.enter="searchByJQL" />
        <button class="btn" @click="searchByJQL">JQL 查询</button>
      </div>
    </div>

    <!-- 列选择器 -->
    <div v-if="showColumnPicker" class="card column-picker">
      <div class="column-picker-header">
        <h4>选择显示列</h4>
        <button class="btn btn-sm" @click="resetColumns">恢复默认</button>
      </div>
      <div class="column-groups">
        <div class="column-group" v-if="fieldsStore.standardFields.length">
          <div class="column-group-title">标准字段</div>
          <div class="column-options">
            <label v-for="f in fieldsStore.standardFields" :key="f.id" class="column-option">
              <input type="checkbox" :value="f.id" v-model="selectedColumns" @change="onColumnsChange" />
              <span>{{ fieldsStore.getFieldLabel(f.id) }}</span>
            </label>
          </div>
        </div>
        <div class="column-group" v-if="fieldsStore.customFields.length">
          <div class="column-group-title">自定义字段</div>
          <div class="column-options">
            <label v-for="f in fieldsStore.customFields" :key="f.id" class="column-option">
              <input type="checkbox" :value="f.id" v-model="selectedColumns" @change="onColumnsChange" />
              <span>{{ f.name }}</span>
            </label>
          </div>
        </div>
      </div>
    </div>

    <!-- 工单列表 -->
    <div v-if="loading" class="loading"><div class="spinner"></div>搜索中...</div>
    <div v-else>
      <div class="result-info">共 {{ total }} 条结果</div>
      <div class="table-wrapper">
        <table>
          <thead>
            <tr>
              <th>Key</th>
              <th v-for="col in selectedColumns" :key="col">{{ fieldsStore.getFieldLabel(col) }}</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="issue in issues" :key="issue.key" @click="viewIssue(issue.key)">
              <td><span class="issue-key">{{ issue.key }}</span></td>
              <td v-for="col in selectedColumns" :key="col" :class="getCellClass(col)">
                <template v-if="col === 'status'">
                  <span class="badge" :style="statusStyle(issue.fields?.status)">{{ issue.fields?.status?.name || '-' }}</span>
                </template>
                <template v-else-if="col === 'labels' || col === 'components' || col === 'fixVersions' || isArrayField(col)">
                  <span class="tags-cell">
                    <span class="label-tag" v-for="v in getArrayValues(issue, col)" :key="v">{{ v }}</span>
                    <span v-if="!getArrayValues(issue, col).length">-</span>
                  </span>
                </template>
                <template v-else>
                  {{ fieldsStore.getFieldDisplayValue(issue, col) }}
                </template>
              </td>
            </tr>
          </tbody>
        </table>
      </div>

      <div v-if="!issues.length && !loading" class="loading">暂无工单数据</div>

      <!-- 分页 -->
      <div class="pagination" v-if="total > pageSize">
        <button :disabled="page <= 0" @click="page -= pageSize; search()">上一页</button>
        <span class="page-info">{{ Math.floor(page / pageSize) + 1 }} / {{ Math.ceil(total / pageSize) }}</span>
        <button :disabled="page + pageSize >= total" @click="page += pageSize; search()">下一页</button>
      </div>
    </div>
  </div>
</template>

<script setup>
import { ref, onMounted, watch } from 'vue'
import { useRouter, useRoute } from 'vue-router'
import api from '@/api'
import { useFieldsStore } from '@/stores/fields'

const router = useRouter()
const route = useRoute()
const fieldsStore = useFieldsStore()
const loading = ref(false)
const issues = ref([])
const total = ref(0)
const page = ref(0)
const pageSize = 50
const showColumnPicker = ref(false)

const projects = ref([])
const statuses = ref([])
const priorities = ref([])
const customJQL = ref('')

const selectedColumns = ref([])

const filters = ref({
  project: route.query.project || '',
  status: '',
  priority: '',
  assignee: '',
  keyword: ''
})

onMounted(async () => {
  // 并行加载字段列表和筛选选项
  await fieldsStore.fetchFields()
  selectedColumns.value = fieldsStore.getSavedColumns()

  const [projectsRes, statusesRes, prioritiesRes] = await Promise.allSettled([
    api.get('/api/jira/projects'),
    api.get('/api/jira/statuses'),
    api.get('/api/jira/priorities')
  ])
  if (projectsRes.status === 'fulfilled') projects.value = projectsRes.value.data.data || []
  if (statusesRes.status === 'fulfilled') statuses.value = statusesRes.value.data.data || []
  if (prioritiesRes.status === 'fulfilled') priorities.value = prioritiesRes.value.data.data || []

  search()
})

watch(() => route.query.project, (v) => {
  if (v) { filters.value.project = v; search() }
})

function onColumnsChange() {
  fieldsStore.saveColumns(selectedColumns.value)
}

function resetColumns() {
  selectedColumns.value = [...fieldsStore.DEFAULT_COLUMNS]
  fieldsStore.saveColumns(selectedColumns.value)
}

function isArrayField(fieldId) {
  const f = fieldsStore.allFields.find(field => field.id === fieldId)
  return f?.schema?.type === 'array'
}

function getArrayValues(issue, fieldId) {
  const val = issue.fields?.[fieldId]
  if (!Array.isArray(val)) return []
  return val.map(v => {
    if (typeof v === 'string') return v
    return v.displayName || v.name || v.value || String(v)
  })
}

function getCellClass(col) {
  if (['created', 'updated', 'resolutiondate', 'duedate'].includes(col)) return 'date-cell'
  if (col === 'summary') return 'summary-cell'
  if (['labels', 'components', 'fixVersions'].includes(col) || isArrayField(col)) return 'tags-cell-td'
  return ''
}

function buildJQL() {
  const parts = []
  if (filters.value.project) parts.push(`project = ${filters.value.project}`)
  if (filters.value.status) parts.push(`status = "${filters.value.status}"`)
  if (filters.value.priority) parts.push(`priority = "${filters.value.priority}"`)
  if (filters.value.assignee) parts.push(`assignee = "${filters.value.assignee}"`)
  if (filters.value.keyword) parts.push(`summary ~ "${filters.value.keyword}"`)
  let jql = parts.join(' AND ')
  jql += ' ORDER BY created DESC'
  return jql
}

async function search() {
  loading.value = true
  try {
    const jql = encodeURIComponent(customJQL.value || buildJQL())
    const res = await api.get(`/api/jira/issues?jql=${jql}&startAt=${page.value}&maxResults=${pageSize}`)
    const d = res.data.data
    issues.value = d.issues || []
    total.value = d.total || 0
  } catch (e) {
    console.error('搜索工单失败:', e)
  } finally {
    loading.value = false
  }
}

function searchByJQL() {
  page.value = 0
  search()
}

function viewIssue(key) {
  router.push(`/issues/${key}`)
}

function statusStyle(status) {
  if (!status) return {}
  const cat = status.statusCategory?.key
  const colors = { done: '#10B981', new: '#3B82F6', indeterminate: '#F59E0B' }
  const bg = colors[cat] || '#94A3B8'
  return { background: bg + '20', color: bg, borderColor: bg + '40' }
}
</script>

<style scoped>
.filters { margin-bottom: 16px; }
.filter-row { display: flex; gap: 8px; flex-wrap: wrap; align-items: center; }
.filter-select { max-width: 180px; }
.filter-input { max-width: 160px; }
.jql-row { margin-top: 8px; }
.jql-row .input { flex: 1; font-family: var(--font-mono); font-size: 13px; }

.btn-icon {
  display: flex; align-items: center; justify-content: center;
  width: 36px; height: 36px; padding: 0;
  background: var(--card-bg); border: 1px solid var(--border); border-radius: var(--radius);
  color: var(--text-secondary); cursor: pointer; transition: all var(--transition);
}
.btn-icon:hover { color: var(--primary-light); border-color: var(--primary-light); }

.column-picker { margin-bottom: 16px; max-height: 400px; overflow-y: auto; }
.column-picker-header { display: flex; justify-content: space-between; align-items: center; margin-bottom: 12px; }
.column-picker-header h4 { font-size: 14px; margin: 0; }
.column-groups { display: flex; flex-direction: column; gap: 12px; }
.column-group-title { font-size: 12px; font-weight: 600; color: var(--text-muted); text-transform: uppercase; letter-spacing: 0.5px; margin-bottom: 6px; }
.column-options { display: flex; flex-wrap: wrap; gap: 4px 16px; }
.column-option { display: flex; align-items: center; gap: 4px; font-size: 13px; color: var(--text-secondary); cursor: pointer; padding: 2px 0; }
.column-option input { cursor: pointer; }

.result-info { font-size: 13px; color: var(--text-muted); margin-bottom: 8px; }

.issue-key {
  font-family: var(--font-mono);
  font-size: 13px;
  font-weight: 600;
  color: var(--primary-light);
  white-space: nowrap;
}

.summary-cell { max-width: 300px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.date-cell { white-space: nowrap; font-size: 13px; color: var(--text-secondary); }

.badge {
  border: 1px solid;
  padding: 2px 10px;
  white-space: nowrap;
}

.tags-cell-td { max-width: 180px; }
.tags-cell { display: inline; }
.label-tag {
  display: inline-block;
  font-size: 11px;
  padding: 1px 6px;
  background: rgba(59,130,246,0.1);
  color: var(--primary-light);
  border-radius: 9999px;
  margin: 1px 2px;
  white-space: nowrap;
}

.page-info { font-size: 13px; color: var(--text-secondary); padding: 0 12px; }

.table-wrapper { overflow-x: auto; }
table { min-width: 900px; }
</style>
