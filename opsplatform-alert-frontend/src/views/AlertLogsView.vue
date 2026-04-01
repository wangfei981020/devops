<template>
  <div>
    <div class="card">
      <div class="card-header">
        <div class="flex items-center gap-2" style="flex-wrap: wrap;">
          <select v-model="filters.status" class="form-select" style="width: 120px;" @change="page = 1; loadLogs()">
            <option value="">全部状态</option>
            <option value="success">成功</option>
            <option value="failed">失败</option>
          </select>
          <select v-model="filters.severity" class="form-select" style="width: 120px;" @change="page = 1; loadLogs()">
            <option value="">全部级别</option>
            <option value="S1">S1 灾难</option>
            <option value="S2">S2 严重</option>
            <option value="S3">S3 警告</option>
          </select>
          <div class="flex items-center gap-2">
            <input type="date" v-model="filters.start_date" class="form-input" style="width: 150px;" @change="page = 1; loadLogs()" />
            <span class="text-secondary">至</span>
            <input type="date" v-model="filters.end_date" class="form-input" style="width: 150px;" @change="page = 1; loadLogs()" />
          </div>
          <span v-if="filters.rule_id" class="badge badge-info">
            规则 ID: {{ filters.rule_id }}
            <button class="btn-icon" style="display: inline; padding: 0 4px;" @click="filters.rule_id = ''; loadLogs()">x</button>
          </span>
          <button v-if="hasFilters" class="btn btn-sm btn-outline" @click="clearFilters">清除筛选</button>
        </div>
        <button class="btn btn-danger btn-sm" @click="cleanLogs">清理旧日志</button>
      </div>

      <div v-if="loading" class="loading"><div class="spinner"></div></div>

      <div v-else-if="logs.length === 0" class="empty-state">
        <FileText :size="48" style="opacity: 0.4;" />
        <p class="mt-4">暂无告警日志</p>
      </div>

      <div v-else class="table-wrapper">
        <table>
          <thead>
            <tr>
              <th>时间</th>
              <th>规则</th>
              <th>级别</th>
              <th>状态</th>
              <th>消息内容</th>
              <th>操作</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="log in logs" :key="log.id">
              <td class="text-sm" style="white-space: nowrap;">{{ formatTime(log.created_at) }}</td>
              <td>
                <span style="font-weight: 500;">{{ log.rule_name }}</span>
                <div class="text-sm text-secondary">ID: {{ log.rule_id }}</div>
              </td>
              <td>
                <span class="badge" :class="severityClass(log.severity)">{{ severityLabel(log.severity) }}</span>
              </td>
              <td>
                <span class="badge" :class="log.status === 'success' ? 'badge-success' : 'badge-danger'">
                  {{ log.status === 'success' ? '成功' : '失败' }}
                </span>
              </td>
              <td>
                <div class="truncate" style="max-width: 400px;" :title="log.message">{{ log.message }}</div>
                <div v-if="log.error_msg" class="text-sm" style="color: var(--danger);">{{ log.error_msg }}</div>
              </td>
              <td>
                <div class="actions">
                  <button class="btn btn-sm btn-outline" @click="showDetail(log)">详情</button>
                </div>
              </td>
            </tr>
          </tbody>
        </table>
      </div>

      <div v-if="total > limit" class="pagination">
        <span>共 {{ total }} 条</span>
        <div class="pagination-btns">
          <button class="btn btn-sm btn-outline" :disabled="page <= 1" @click="page--; loadLogs()">上一页</button>
          <span style="padding: 4px 12px;">{{ page }} / {{ Math.ceil(total / limit) }}</span>
          <button class="btn btn-sm btn-outline" :disabled="page >= Math.ceil(total / limit)" @click="page++; loadLogs()">下一页</button>
        </div>
      </div>
    </div>

    <!-- Detail Modal -->
    <div v-if="detailLog" class="modal-overlay" @click.self="detailLog = null">
      <div class="modal" style="min-width: 700px;">
        <div class="modal-header">
          <div class="modal-title">告警详情 #{{ detailLog.id }}</div>
          <button class="btn-icon" @click="detailLog = null"><X :size="18" /></button>
        </div>
        <div style="display: grid; grid-template-columns: 100px 1fr; gap: 8px; font-size: 14px;">
          <div class="text-secondary">规则:</div><div>{{ detailLog.rule_name }} (ID: {{ detailLog.rule_id }})</div>
          <div class="text-secondary">级别:</div><div><span class="badge" :class="severityClass(detailLog.severity)">{{ severityLabel(detailLog.severity) }}</span></div>
          <div class="text-secondary">状态:</div><div><span class="badge" :class="detailLog.status === 'success' ? 'badge-success' : 'badge-danger'">{{ detailLog.status }}</span></div>
          <div class="text-secondary">时间:</div><div>{{ detailLog.created_at }}</div>
        </div>

        <div class="form-group mt-4">
          <label class="form-label">消息内容</label>
          <pre style="background: #f1f5f9; padding: 12px; border-radius: 6px; font-size: 13px; white-space: pre-wrap; max-height: 200px; overflow-y: auto;">{{ detailLog.message }}</pre>
        </div>

        <div v-if="detailLog.error_msg" class="form-group">
          <label class="form-label" style="color: var(--danger);">错误信息</label>
          <pre style="background: #fef2f2; padding: 12px; border-radius: 6px; font-size: 13px; white-space: pre-wrap;">{{ detailLog.error_msg }}</pre>
        </div>

        <div v-if="detailLog.es_raw" class="form-group">
          <label class="form-label">ES 原始数据</label>
          <pre style="background: #f1f5f9; padding: 12px; border-radius: 6px; font-size: 12px; white-space: pre-wrap; max-height: 300px; overflow-y: auto;">{{ formatJSON(detailLog.es_raw) }}</pre>
        </div>

        <div v-if="detailLog.lark_response" class="form-group">
          <label class="form-label">Lark 响应</label>
          <pre style="background: #f1f5f9; padding: 12px; border-radius: 6px; font-size: 12px; white-space: pre-wrap;">{{ detailLog.lark_response }}</pre>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup>
import { ref, computed, onMounted } from 'vue'
import { useRoute } from 'vue-router'
import api from '../api'
import { useToast, useConfirm } from '../stores/ui'
import { FileText, X } from 'lucide-vue-next'

const toast = useToast()
const dialog = useConfirm()
const route = useRoute()
const logs = ref([])
const loading = ref(false)
const page = ref(1)
const limit = ref(20)
const total = ref(0)
const detailLog = ref(null)

const filters = ref({
  status: '',
  severity: '',
  rule_id: route.query.rule_id || '',
  start_date: '',
  end_date: ''
})

async function muteFromLog(log, event) {
  const duration = event.target.value
  event.target.value = ''
  if (!duration || !log.rule_id) return

  // Extract group_key: try rule_name [xxx], then message **Container**: xxx
  let groupKey = ''
  const match = log.rule_name?.match(/\[(.+?)\]/)
  if (match) {
    groupKey = match[1]
  }
  if (!groupKey) {
    const containerMatch = log.message?.match(/\*\*Container\*\*:\s*(\S+)/)
    if (containerMatch) groupKey = containerMatch[1]
  }
  if (!groupKey) {
    const msgMatch = log.message?.match(/\*\*_group_key:\*\*\s*(\S+)/)
    if (msgMatch) groupKey = msgMatch[1]
  }
  if (!groupKey) {
    toast.error('无法识别容器名，请在规则编辑页屏蔽')
    return
  }

  try {
    const res = await api.post('/alert-mutes', {
      rule_id: log.rule_id,
      group_key: groupKey,
      duration: duration,
      reason: '告警日志页屏蔽'
    })
    if (res.code === 0) {
      toast.success(`${groupKey} 已屏蔽 ${duration}`)
    } else {
      toast.error(res.message)
    }
  } catch (e) {
    toast.error('屏蔽失败')
  }
}

const hasFilters = computed(() => {
  return filters.value.status || filters.value.severity || filters.value.rule_id || filters.value.start_date || filters.value.end_date
})

function clearFilters() {
  filters.value = { status: '', severity: '', rule_id: '', start_date: '', end_date: '' }
  page.value = 1
  loadLogs()
}

async function loadLogs() {
  loading.value = true
  try {
    const params = { page: page.value, limit: limit.value }
    if (filters.value.status) params.status = filters.value.status
    if (filters.value.severity) params.severity = filters.value.severity
    if (filters.value.rule_id) params.rule_id = filters.value.rule_id
    if (filters.value.start_date) params.start_date = filters.value.start_date
    if (filters.value.end_date) params.end_date = filters.value.end_date
    const res = await api.get('/alert-logs', { params })
    if (res.code === 0) {
      logs.value = res.data
      total.value = res.total
    }
  } catch (e) { /* ignore */ }
  loading.value = false
}

function showDetail(log) {
  detailLog.value = log
}

async function cleanLogs() {
  const days = await dialog.prompt({ title: '清理旧日志', message: '清理多少天前的日志？', defaultValue: '30', placeholder: '天数' })
  if (days === null) return
  try {
    const res = await api.delete('/alert-logs/clean', { params: { days } })
    if (res.code === 0) {
      toast.success(res.data.message)
      loadLogs()
    }
  } catch (e) { /* ignore */ }
}

function severityClass(s) {
  return { S1: 'badge-danger', S2: 'badge-warning', S3: 'badge-info', info: 'badge-info', warning: 'badge-warning', critical: 'badge-danger' }[s] || 'badge-gray'
}
function severityLabel(s) {
  return { S1: 'S1 灾难', S2: 'S2 严重', S3: 'S3 警告', info: '信息', warning: '警告', critical: '严重' }[s] || s
}
function formatTime(t) {
  if (!t) return '-'
  return new Date(t).toLocaleString('zh-CN')
}
function formatJSON(s) {
  try { return JSON.stringify(JSON.parse(s), null, 2) } catch { return s }
}

onMounted(loadLogs)
</script>
