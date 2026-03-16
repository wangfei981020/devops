<template>
  <div>
    <ConnectionSelector type="jira" v-model="connId" @update:modelValue="onConnChange" />

    <!-- 项目标题 + 返回 -->
    <div class="card" style="margin-bottom:16px;padding:16px">
      <div style="display:flex;align-items:center;gap:12px;flex-wrap:wrap">
        <button class="btn btn-sm" @click="$router.push('/jira')" style="flex-shrink:0">← 返回项目</button>
        <div style="flex:1;min-width:200px">
          <h3 style="margin:0;font-size:16px;color:var(--text-primary)">
            {{ projectInfo.name || projectKey }}
            <span style="font-family:var(--font-mono);font-size:13px;color:var(--primary-light);margin-left:8px">{{ projectKey }}</span>
            <span v-if="!isFault" class="badge" style="margin-left:8px;background:rgba(76,154,255,0.15);color:var(--primary-light);font-size:11px">变更项目</span>
          </h3>
        </div>
        <span style="font-size:13px;color:var(--text-muted)">共 {{ totalCount }} 个工单</span>
      </div>
    </div>

    <!-- 筛选栏 -->
    <div class="card" style="margin-bottom:16px;padding:16px">
      <div class="filter-row">
        <div class="filter-group">
          <label>状态</label>
          <select v-model="filterStatus" class="input" @change="resetAndSearch">
            <option value="">全部</option>
            <template v-if="statusOptions.length">
              <option v-for="s in statusOptions" :key="s" :value="s">{{ s }}</option>
            </template>
            <template v-else>
              <option value="Open">Open</option>
              <option value="In Progress">进行中</option>
              <option value="Resolved">已解决</option>
              <option value="Closed">已关闭</option>
              <option value="Done">完成</option>
            </template>
          </select>
        </div>
        <template v-if="isFault">
          <div class="filter-group">
            <label>故障等级</label>
            <select v-model="filterFaultLevel" class="input" @change="resetAndSearch">
              <option value="">全部</option>
              <option value="p0">P0</option>
              <option value="p1">P1</option>
              <option value="p2">P2</option>
              <option value="p3">P3</option>
              <option value="p4">P4</option>
            </select>
          </div>
          <div class="filter-group">
            <label>故障项目</label>
            <input v-model="filterFaultProject" class="input" placeholder="如 G33" @keyup.enter="resetAndSearch" style="min-width:80px" />
          </div>
        </template>
        <template v-else>
          <div class="filter-group">
            <label>业务项目</label>
            <input v-model="filterBizProject" class="input" placeholder="业务项目名称" @keyup.enter="resetAndSearch" style="min-width:100px" />
          </div>
        </template>
        <div class="filter-group">
          <label>经办人</label>
          <input v-model="filterAssignee" class="input" placeholder="经办人" @keyup.enter="resetAndSearch" style="min-width:80px" />
        </div>
        <div class="filter-group">
          <label>开始日期</label>
          <input v-model="filterDateFrom" type="date" class="input" @change="resetAndSearch" style="min-width:130px" />
        </div>
        <div class="filter-group">
          <label>结束日期</label>
          <input v-model="filterDateTo" type="date" class="input" @change="resetAndSearch" style="min-width:130px" />
        </div>
        <div class="filter-group" style="flex:1;min-width:200px">
          <label>搜索</label>
          <div style="display:flex;gap:8px">
            <input v-model="searchText" class="input" placeholder="关键词搜索..." @keyup.enter="resetAndSearch" />
            <button class="btn btn-primary btn-sm" @click="resetAndSearch">搜索</button>
          </div>
        </div>
      </div>
    </div>

    <!-- 工单列表 -->
    <div v-if="loading && issues.length === 0" class="loading"><div class="spinner"></div>加载中...</div>
    <div v-else-if="error" class="card" style="text-align:center;padding:40px;color:var(--text-muted)">{{ error }}</div>
    <div v-else class="card">
      <div class="table-wrapper">
        <table>
          <thead>
            <tr>
              <th style="width:120px">KEY</th>
              <th>标题</th>
              <th style="width:100px">类型</th>
              <th style="width:90px">优先级</th>
              <template v-if="isFault">
                <th style="width:70px">故障等级</th>
                <th style="width:80px">故障项目</th>
              </template>
              <template v-else>
                <th style="width:80px">业务项目</th>
                <th style="width:90px">变更类型</th>
              </template>
              <th style="width:100px">状态</th>
              <th style="width:120px">经办人</th>
              <th style="width:110px">更新时间</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="issue in issues" :key="issue.key" @click="$router.push('/jira/issue/' + issue.key)" style="cursor:pointer">
              <td><span class="issue-key">{{ issue.key }}</span></td>
              <td class="title-cell">{{ issue.fields?.summary }}</td>
              <td>
                <span class="badge type-badge">{{ issue.fields?.issuetype?.name }}</span>
              </td>
              <td>
                <span class="badge" :class="'priority-' + (issue.fields?.priority?.name || '').toLowerCase()">
                  {{ issue.fields?.priority?.name || '-' }}
                </span>
              </td>
              <template v-if="isFault">
                <td>{{ getCF(issue, '故障等级') }}</td>
                <td>{{ getCF(issue, '故障项目') }}</td>
              </template>
              <template v-else>
                <td>{{ getCF(issue, '业务运维项目') }}</td>
                <td>{{ getCF(issue, '变更类型') }}</td>
              </template>
              <td>
                <span class="badge status-badge" :class="getStatusClass(issue.fields?.status?.statusCategory?.key)">
                  {{ issue.fields?.status?.name }}
                </span>
              </td>
              <td>{{ issue.fields?.assignee?.displayName || '-' }}</td>
              <td style="font-size:12px;color:var(--text-muted)">{{ formatDate(issue.fields?.updated) }}</td>
            </tr>
          </tbody>
        </table>
      </div>

      <div v-if="issues.length === 0" style="text-align:center;padding:40px;color:var(--text-muted)">
        没有匹配的工单
      </div>

      <!-- 分页 -->
      <div v-if="totalCount > 0" class="pagination-bar">
        <div class="page-size-select">
          <span>每页</span>
          <select v-model="pageSize" class="input input-sm" @change="onPageSizeChange">
            <option v-for="s in pageSizeOptions" :key="s" :value="s">{{ s }}</option>
          </select>
          <span>条</span>
        </div>
        <button class="btn btn-sm" :disabled="currentPage <= 1" @click="goPage(currentPage - 1)">上一页</button>
        <span class="page-info">第 {{ currentPage }} / {{ totalPages }} 页 (共 {{ totalCount }} 条)</span>
        <button class="btn btn-sm" :disabled="currentPage >= totalPages" @click="goPage(currentPage + 1)">下一页</button>
      </div>
    </div>
  </div>
</template>

<script setup>
import { ref, computed, onMounted } from 'vue'
import { useRoute } from 'vue-router'
import api from '@/api'
import ConnectionSelector from '@/components/ConnectionSelector.vue'

const route = useRoute()
const projectKey = route.params.key

const loading = ref(true)
const error = ref('')
const issues = ref([])
const projectInfo = ref({})
const totalCount = ref(0)
const pageSize = ref(10)
const pageSizeOptions = [10, 20, 50, 100]
const currentPage = ref(1)
const connId = ref('')

// 项目类型：fault(故障) 或 change(变更)，根据连接配置自动判断
const projectCategory = ref('fault')
// 自定义字段 ID 映射（从 Jira fields API 获取）
const customFields = ref({})

const filterStatus = ref('')
const filterFaultLevel = ref('')
const filterFaultProject = ref('')
const filterBizProject = ref('')
const filterAssignee = ref('')
const filterDateFrom = ref('')
const filterDateTo = ref('')
const searchText = ref('')
const statusOptions = ref([])

const totalPages = computed(() => Math.max(1, Math.ceil(totalCount.value / pageSize.value)))

const isFault = computed(() => projectCategory.value === 'fault')

function buildJQL() {
  const parts = [`project = "${projectKey}"`]
  if (filterStatus.value) parts.push(`status = "${filterStatus.value}"`)
  if (isFault.value) {
    if (filterFaultLevel.value) parts.push(`"故障等级" = "${filterFaultLevel.value}"`)
    if (filterFaultProject.value.trim()) parts.push(`"故障项目" = "${filterFaultProject.value.trim()}"`)
  } else {
    if (filterBizProject.value.trim()) parts.push(`"业务运维项目" ~ "${filterBizProject.value.trim()}"`)
  }
  if (filterAssignee.value.trim()) parts.push(`assignee = "${filterAssignee.value.trim()}"`)
  if (filterDateFrom.value) parts.push(`updated >= "${filterDateFrom.value}"`)
  if (filterDateTo.value) parts.push(`updated <= "${filterDateTo.value}"`)
  if (searchText.value.trim()) parts.push(`summary ~ "${searchText.value.trim()}"`)
  return parts.join(' AND ') + ' ORDER BY updated DESC'
}

function onConnChange() {
  currentPage.value = 1
  fetchIssues()
}

async function fetchIssues() {
  loading.value = true
  error.value = ''
  try {
    const jql = buildJQL()
    const startAt = (currentPage.value - 1) * pageSize.value
    const params = { jql, startAt, maxResults: pageSize.value, fields: '*all' }
    if (connId.value) params.conn_id = connId.value
    const res = await api.get('/api/jira/search', { params })
    const data = res.data.data || {}
    issues.value = data.issues || []
    totalCount.value = data.total || 0
  } catch (e) {
    error.value = e.response?.data?.error || '加载工单失败'
  }
  loading.value = false
}

function resetAndSearch() {
  currentPage.value = 1
  fetchIssues()
}

function goPage(p) {
  currentPage.value = p
  fetchIssues()
}

function onPageSizeChange() {
  currentPage.value = 1
  fetchIssues()
}

function getStatusClass(categoryKey) {
  if (categoryKey === 'done') return 'status-done'
  if (categoryKey === 'indeterminate') return 'status-progress'
  return 'status-open'
}

function formatDate(dt) {
  if (!dt) return '-'
  return dt.substring(0, 10)
}

// 从连接配置判断项目类型
async function detectProjectCategory() {
  try {
    const connRes = await api.get('/api/connections/public')
    const conns = (connRes.data.data || []).filter(c => c.type === 'jira')
    const conn = conns.find(c => c.is_default) || conns[0]
    if (conn) {
      connId.value = String(conn.id)
      const cfg = typeof conn.config === 'string' ? JSON.parse(conn.config) : (conn.config || {})
      const faultProjects = (cfg.fault_projects || cfg.projects || '').split(',').map(s => s.trim())
      const changeProjects = (cfg.change_projects || '').split(',').map(s => s.trim())
      if (changeProjects.includes(projectKey)) {
        projectCategory.value = 'change'
      } else {
        projectCategory.value = 'fault'
      }
    }
  } catch (e) { /* ignore */ }
}

// 获取自定义字段映射
async function fetchFieldMappings() {
  try {
    const params = connId.value ? { conn_id: connId.value } : {}
    const res = await api.get('/api/jira/fields', { params })
    const fields = res.data.data || []
    const map = {}
    for (const f of fields) {
      if (f.id?.startsWith('customfield_')) {
        // 同时用 name 和 description 作为 key，保留原始大小写
        const name = (f.name || '').trim()
        const desc = (f.description || '').trim()
        if (name) map[name] = f.id
        if (desc && desc !== name) map[desc] = f.id
        // 也存 lowercase 版本用于模糊匹配
        if (name) map[name.toLowerCase()] = f.id
        if (desc) map[desc.toLowerCase()] = f.id
      }
    }
    customFields.value = map
  } catch (e) { /* ignore */ }
}

// 获取自定义字段值
function getCF(issue, fieldName) {
  // 先精确匹配，再 lowercase 匹配，再模糊包含匹配
  let fieldId = customFields.value[fieldName] || customFields.value[fieldName.toLowerCase()]
  if (!fieldId) {
    // 模糊匹配：在所有 key 中查找包含 fieldName 的
    const needle = fieldName.toLowerCase()
    for (const [k, v] of Object.entries(customFields.value)) {
      if (k.includes(needle) || needle.includes(k)) {
        fieldId = v
        break
      }
    }
  }
  if (!fieldId) return '-'
  const val = issue.fields?.[fieldId]
  if (!val) return '-'
  if (typeof val === 'string') return val
  if (val.value) return val.value
  if (val.name) return val.name
  if (val.displayName) return val.displayName
  if (Array.isArray(val)) return val.map(v => v.value || v.name || v).join(', ')
  return String(val)
}

// 从工单中提取状态选项
function extractStatuses(issueList) {
  const set = new Set()
  for (const issue of issueList) {
    const name = issue.fields?.status?.name
    if (name) set.add(name)
  }
  return [...set]
}

onMounted(async () => {
  await detectProjectCategory()
  await fetchFieldMappings()
  const issuesPromise = fetchIssues()
  try {
    const projParams = connId.value ? { conn_id: connId.value } : {}
    const res = await api.get('/api/jira/projects', { params: projParams })
    const all = res.data.data || []
    projectInfo.value = all.find(p => p.key === projectKey) || {}
  } catch (e) { /* ignore */ }
  await issuesPromise
  if (issues.value.length > 0) {
    statusOptions.value = extractStatuses(issues.value)
  }
})
</script>

<style scoped>
.filter-row {
  display: flex;
  gap: 16px;
  align-items: flex-end;
  flex-wrap: wrap;
}
.filter-group {
  display: flex;
  flex-direction: column;
  gap: 4px;
}
.filter-group label {
  font-size: 12px;
  color: var(--text-muted);
  font-weight: 500;
}
.filter-group .input {
  min-width: 120px;
}
.issue-key {
  font-family: var(--font-mono);
  font-size: 13px;
  color: var(--primary-light);
  font-weight: 500;
}
.title-cell {
  max-width: 400px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.type-badge {
  background: rgba(76,154,255,0.1);
  color: var(--text-secondary);
}
.status-badge {
  font-size: 12px;
}
.status-done { background: rgba(16,185,129,0.15); color: var(--success); }
.status-progress { background: rgba(76,154,255,0.15); color: var(--primary-light); }
.status-open { background: rgba(100,116,139,0.15); color: var(--text-secondary); }

.priority-highest, .priority-high { color: var(--danger); background: rgba(239,68,68,0.1); }
.priority-medium { color: var(--warning); background: rgba(245,158,11,0.1); }
.priority-low, .priority-lowest { color: var(--success); background: rgba(16,185,129,0.1); }

.pagination-bar {
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 16px;
  padding: 16px;
  border-top: 1px solid var(--border);
}
.page-info {
  font-size: 13px;
  color: var(--text-muted);
}
.page-size-select {
  display: flex;
  align-items: center;
  gap: 6px;
  font-size: 13px;
  color: var(--text-muted);
  margin-right: auto;
}
.page-size-select select {
  width: 65px;
  padding: 2px 6px;
}
</style>
