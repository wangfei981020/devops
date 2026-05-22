<template>
  <div>
    <div class="card">
      <div class="card-header">
        <div class="card-title">日志查询</div>
        <button v-if="result" class="btn btn-primary btn-sm" @click="createRuleFromQuery">
          <Plus :size="14" /> 基于此查询创建告警规则
        </button>
      </div>

      <!-- Data Source Toggle -->
      <div class="flex gap-2 mb-4">
        <button class="btn btn-sm" :class="dataSource === 'es' ? 'btn-primary' : 'btn-outline'" @click="dataSource = 'es'">Elasticsearch</button>
        <button class="btn btn-sm" :class="dataSource === 'loki' ? 'btn-primary' : 'btn-outline'" @click="dataSource = 'loki'">Loki</button>
      </div>

      <!-- ES Form -->
      <template v-if="dataSource === 'es'">
        <div class="form-row">
          <div class="form-group">
            <label class="form-label">ES 连接</label>
            <select v-model="form.es_connection_id" class="form-select">
              <option :value="0">请选择 ES 连接</option>
              <option v-for="c in esConnections" :key="c.id" :value="c.id">
                {{ c.name }} ({{ c.version }}.x)
              </option>
            </select>
          </div>
          <div class="form-group">
            <label class="form-label">ES 索引</label>
            <IndexSelector v-model="form.es_index" :es-connection-id="form.es_connection_id" />
          </div>
        </div>
        <div class="form-group">
          <label class="form-label">搜索关键词</label>
          <input v-model="form.keyword" class="form-input" placeholder="支持 Lucene 语法，AND/OR/NOT" @keyup.enter="search" />
        </div>
        <div class="form-group">
          <label class="form-label">过滤字段 (JSON, 可选)</label>
          <input v-model="form.filter_fields" class="form-input"
            placeholder='[{"field":"kubernetes.namespace","value":"g32-uat","op":"match"}]' />
        </div>
      </template>

      <!-- Loki Form -->
      <template v-else>
        <div class="form-group">
          <label class="form-label">Loki 连接</label>
          <select v-model="form.loki_connection_id" class="form-select" @change="loadLabels">
            <option :value="0">请选择 Loki 连接</option>
            <option v-for="c in lokiConnections" :key="c.id" :value="c.id">{{ c.name }}</option>
          </select>
        </div>

        <!-- Query Builder -->
        <div v-if="form.loki_connection_id" class="card" style="padding: 12px; margin-bottom: 12px; background: #f8fafc;">
          <div class="flex justify-between items-center" style="margin-bottom: 8px;">
            <span style="font-weight: 600; font-size: 13px;">标签选择器</span>
            <button class="btn btn-sm btn-outline" @click="addLabelFilter">+ 添加标签</button>
          </div>
          <div v-for="(filter, idx) in labelFilters" :key="idx" class="flex gap-2 items-center" style="margin-bottom: 6px;">
            <select v-model="filter.label" class="form-select" style="width: 180px;" @change="loadLabelValues(filter)">
              <option value="">选择标签</option>
              <option v-for="l in lokiLabels" :key="l" :value="l">{{ l }}</option>
            </select>
            <select v-model="filter.op" class="form-select" style="width: 80px;" @change="buildLogQL">
              <option value="=">=</option>
              <option value="!=">!=</option>
              <option value="=~">=~</option>
              <option value="!~">!~</option>
            </select>
            <select v-if="filter.values.length > 0" v-model="filter.value" class="form-select" style="width: 220px;" @change="buildLogQL">
              <option value="">选择值</option>
              <option v-for="v in filter.values" :key="v" :value="v">{{ v }}</option>
            </select>
            <input v-else v-model="filter.value" class="form-input" style="width: 220px;" placeholder="输入值或正则" @input="buildLogQL" />
            <button class="btn-icon" @click="labelFilters.splice(idx, 1); buildLogQL()" style="color: var(--danger);">
              <X :size="16" />
            </button>
          </div>

          <div class="form-group" style="margin-top: 8px; margin-bottom: 0;">
            <label class="form-label" style="font-size: 12px;">日志过滤 (可选)</label>
            <div class="flex gap-2">
              <select v-model="logFilter.op" class="form-select" style="width: 100px;">
                <option value="|=">包含 |=</option>
                <option value="!=">不包含 !=</option>
                <option value="|~">正则 |~</option>
                <option value="!~">排除正则 !~</option>
              </select>
              <input v-model="logFilter.value" class="form-input" placeholder="过滤关键词，如 error" @input="buildLogQL" />
            </div>
          </div>
        </div>

        <div class="form-group">
          <label class="form-label">LogQL 查询</label>
          <input v-model="form.logql" class="form-input" placeholder='{namespace="default"} |= "error"' @keyup.enter="search" />
          <div class="form-hint">可手动编辑，或通过上方标签选择器自动生成</div>
        </div>
      </template>

      <!-- Shared: Time range -->
      <div class="form-group">
        <label class="form-label">时间范围</label>
        <div class="flex gap-2 items-center">
          <div class="time-quick-btns">
            <button v-for="t in timeOptions" :key="t.value" class="btn btn-sm"
              :class="form.time_range === t.value ? 'btn-primary' : 'btn-outline'"
              @click="form.time_range = t.value">{{ t.label }}</button>
          </div>
        </div>
      </div>
      <div v-if="form.time_range === 'custom'" class="form-row">
        <div class="form-group">
          <label class="form-label">开始时间</label>
          <input type="datetime-local" v-model="form.start_time" class="form-input" />
        </div>
        <div class="form-group">
          <label class="form-label">结束时间</label>
          <input type="datetime-local" v-model="form.end_time" class="form-input" />
        </div>
      </div>

      <div class="flex gap-2 items-center" style="margin-top: 8px;">
        <button class="btn btn-primary" @click="search" :disabled="searching">
          <Search :size="16" /> {{ searching ? '查询中...' : '查询' }}
        </button>
        <button class="btn btn-outline" @click="resetForm">重置</button>
        <div class="flex items-center gap-2">
          <span class="text-sm text-secondary">返回条数:</span>
          <select v-model.number="form.size" class="form-select" style="width: 100px;">
            <option :value="20">20</option>
            <option :value="50">50</option>
            <option :value="100">100</option>
            <option :value="200">200</option>
            <option :value="500">500</option>
            <option :value="1000">1000</option>
          </select>
        </div>
        <span v-if="result" class="text-sm text-secondary" style="line-height: 36px;">
          共 {{ result.total }} 条，显示 {{ result.count }} 条
        </span>
      </div>
    </div>

    <!-- Results -->
    <div v-if="result" class="card" style="margin-top: 16px;">
      <div class="card-header">
        <div class="card-title">查询结果 ({{ result.count }} / {{ result.total }})</div>
        <div class="flex gap-2">
          <select v-model="displayMode" class="form-select" style="width: 130px;">
            <option value="table">表格模式</option>
            <option value="json">JSON 模式</option>
          </select>
        </div>
      </div>

      <div v-if="displayMode === 'table'" class="table-wrapper">
        <table>
          <thead>
            <tr>
              <th style="width: 40px;">#</th>
              <th style="width: 180px;">时间</th>
              <th>消息 / 内容</th>
              <th style="width: 60px;">详情</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="(hit, idx) in result.hits" :key="idx">
              <td class="text-sm text-secondary">{{ idx + 1 }}</td>
              <td class="text-sm" style="white-space: nowrap;">{{ formatTimestamp(hit['@timestamp'] || hit.timestamp) }}</td>
              <td>
                <div style="word-break: break-all; font-size: 13px; max-height: 60px; overflow: hidden;" :title="getMessage(hit)">{{ getMessage(hit) }}</div>
              </td>
              <td>
                <button class="btn btn-sm btn-outline" @click="expandedHit = expandedHit === idx ? -1 : idx">
                  {{ expandedHit === idx ? '收起' : '展开' }}
                </button>
              </td>
            </tr>
          </tbody>
        </table>
        <div v-if="expandedHit >= 0 && result.hits[expandedHit]" class="card" style="margin: 8px 0; padding: 12px; background: #f8fafc;">
          <div style="max-height: 400px; overflow-y: auto;">
            <JsonFilterable :data="cleanObj(result.hits[expandedHit])" @filter-add="onFilterAdd" />
          </div>
        </div>
      </div>

      <div v-else>
        <div v-for="(hit, idx) in result.hits" :key="idx" class="card" style="margin-bottom: 8px; padding: 12px;">
          <div class="flex justify-between items-center" style="margin-bottom: 6px;">
            <span class="badge badge-info">#{{ idx + 1 }} — {{ formatTimestamp(hit['@timestamp'] || hit.timestamp) }}</span>
          </div>
          <div style="max-height: 200px; overflow-y: auto; background: #f1f5f9; padding: 10px; border-radius: 6px;">
            <JsonFilterable :data="cleanObj(hit)" @filter-add="onFilterAdd" />
          </div>
        </div>
      </div>
    </div>

    <div v-else-if="searched && !searching" class="card" style="margin-top: 16px;">
      <div class="empty-state">
        <Search :size="48" style="opacity: 0.4;" />
        <p class="mt-4">未搜到匹配结果</p>
      </div>
    </div>
  </div>
</template>

<script setup>
import { ref, onMounted } from 'vue'
import { useRouter } from 'vue-router'
import api from '../api'
import { useToast } from '../stores/ui'
import { Search, Plus, X } from 'lucide-vue-next'
import IndexSelector from '../components/IndexSelector.vue'
import JsonFilterable from '../components/JsonFilterable.vue'

const router = useRouter()
const toast = useToast()

const dataSource = ref('es')
const esConnections = ref([])
const lokiConnections = ref([])
const lokiLabels = ref([])
const labelFilters = ref([])
const logFilter = ref({ op: '|=', value: '' })
const searching = ref(false)
const searched = ref(false)
const result = ref(null)
const displayMode = ref('table')
const expandedHit = ref(-1)

const timeOptions = [
  { label: '5分钟', value: '5m' },
  { label: '15分钟', value: '15m' },
  { label: '30分钟', value: '30m' },
  { label: '1小时', value: '1h' },
  { label: '6小时', value: '6h' },
  { label: '24小时', value: '24h' },
  { label: '7天', value: '7d' },
  { label: '自定义', value: 'custom' },
]

const form = ref({
  es_connection_id: 0,
  loki_connection_id: 0,
  es_index: '*',
  keyword: '',
  logql: '',
  filter_fields: '',
  time_range: '15m',
  start_time: '',
  end_time: '',
  size: 20
})

function addLabelFilter() {
  labelFilters.value.push({ label: '', op: '=', value: '', values: [] })
}

async function loadLabelValues(filter) {
  filter.values = []
  filter.value = ''
  if (!filter.label || !form.value.loki_connection_id) return
  try {
    const res = await api.get('/loki-label-values', {
      params: { loki_connection_id: form.value.loki_connection_id, label: filter.label }
    })
    if (res.code === 0) filter.values = res.data || []
  } catch (e) { /* ignore */ }
  buildLogQL()
}

function buildLogQL() {
  const parts = labelFilters.value
    .filter(f => f.label && f.value)
    .map(f => `${f.label}${f.op}"${f.value}"`)
  if (parts.length === 0) { form.value.logql = ''; return }
  let q = `{${parts.join(', ')}}`
  if (logFilter.value.value) {
    q += ` ${logFilter.value.op} "${logFilter.value.value}"`
  }
  form.value.logql = q
}

async function loadOptions() {
  try {
    const [esRes, lokiRes] = await Promise.all([
      api.get('/es-connections'),
      api.get('/loki-connections')
    ])
    if (esRes.code === 0) esConnections.value = esRes.data
    if (lokiRes.code === 0) lokiConnections.value = lokiRes.data
  } catch (e) { /* ignore */ }
}

async function loadLabels() {
  lokiLabels.value = []
  if (!form.value.loki_connection_id) return
  try {
    const res = await api.get('/loki-labels', { params: { loki_connection_id: form.value.loki_connection_id } })
    if (res.code === 0) lokiLabels.value = res.data || []
  } catch (e) { /* ignore */ }
}

async function search() {
  if (dataSource.value === 'es' && !form.value.es_connection_id) {
    toast.error('请选择 ES 连接'); return
  }
  if (dataSource.value === 'loki' && !form.value.loki_connection_id) {
    toast.error('请选择 Loki 连接'); return
  }
  if (dataSource.value === 'loki' && !form.value.logql) {
    toast.error('请输入 LogQL 查询'); return
  }

  searching.value = true
  searched.value = true
  expandedHit.value = -1
  result.value = null

  try {
    let res
    if (dataSource.value === 'es') {
      const payload = { ...form.value }
      if (payload.time_range === 'custom') { payload.time_range = '' } else { payload.start_time = ''; payload.end_time = '' }
      res = await api.post('/es-explore', payload)
    } else {
      const payload = {
        loki_connection_id: form.value.loki_connection_id,
        logql: form.value.logql,
        time_range: form.value.time_range === 'custom' ? '' : form.value.time_range,
        start_time: form.value.time_range === 'custom' ? form.value.start_time : '',
        end_time: form.value.time_range === 'custom' ? form.value.end_time : '',
        size: form.value.size
      }
      res = await api.post('/loki-explore', payload)
    }
    if (res.code === 0) { result.value = res.data } else { toast.error(res.message) }
  } catch (e) { toast.error('查询失败: ' + (e.response?.data?.message || e.message)) }
  searching.value = false
}

function resetForm() {
  form.value = { es_connection_id: form.value.es_connection_id, loki_connection_id: form.value.loki_connection_id, es_index: '*', keyword: '', logql: '', filter_fields: '', time_range: '15m', start_time: '', end_time: '', size: 20 }
  result.value = null
  searched.value = false
}

function createRuleFromQuery() {
  const query = { time_range: form.value.time_range === 'custom' ? '5m' : form.value.time_range }
  if (dataSource.value === 'es') {
    query.es_connection_id = form.value.es_connection_id
    query.es_index = form.value.es_index
    query.keyword = form.value.keyword
    query.filter_fields = form.value.filter_fields
  }
  router.push({ path: '/alert-rules/create', query })
}

// Strip ANSI escape codes and control characters
function cleanAnsi(str) {
  if (!str || typeof str !== 'string') return str
  return str
    .replace(/\u001b\[[\d;]*[a-zA-Z]/g, '')   // real ANSI: \x1b[...
    .replace(/\\u001b\[[\d;]*[a-zA-Z]/g, '')   // escaped in JSON: \\u001b[...
    .replace(/\x1b\[[\d;]*[a-zA-Z]/g, '')      // hex variant
}

function getMessage(hit) {
  const msg = hit.message || hit.line || hit.log || hit.msg || JSON.stringify(hit).substring(0, 200)
  return cleanAnsi(msg)
}
function formatTimestamp(ts) {
  if (!ts) return '-'
  // Loki timestamp is nanoseconds string
  if (typeof ts === 'string' && ts.length > 13) {
    return new Date(parseInt(ts) / 1000000).toLocaleString('zh-CN')
  }
  return new Date(ts).toLocaleString('zh-CN')
}
function cleanObj(obj) {
  if (typeof obj === 'string') return cleanAnsi(obj)
  if (Array.isArray(obj)) return obj.map(cleanObj)
  if (obj && typeof obj === 'object') {
    const out = {}
    for (const [k, v] of Object.entries(obj)) out[k] = cleanObj(v)
    return out
  }
  return obj
}

function formatJSON(obj) {
  try { return JSON.stringify(cleanObj(obj), null, 2) } catch { return String(obj) }
}

function onFilterAdd(payload) {
  // Parse current filter_fields (JSON string), append, re-stringify
  let current = []
  try {
    if (form.value.filter_fields && form.value.filter_fields.trim()) {
      current = JSON.parse(form.value.filter_fields)
      if (!Array.isArray(current)) current = []
    }
  } catch { current = [] }
  current.push(payload)
  form.value.filter_fields = JSON.stringify(current)
  // Auto re-query
  search()
}

onMounted(loadOptions)
</script>

<style scoped>
.time-quick-btns { display: flex; gap: 4px; flex-wrap: wrap; }
.time-quick-btns .btn { padding: 4px 10px; font-size: 12px; }
</style>
