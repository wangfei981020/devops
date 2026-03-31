<template>
  <div>
    <div class="card">
      <div class="card-header">
        <div class="flex items-center gap-2">
          <input v-model="search" class="form-input" style="width: 250px;" placeholder="搜索规则名称/关键词..." @input="debounceLoad" />
          <select v-model="statusFilter" class="form-select" style="width: 120px;" @change="loadRules">
            <option value="">全部状态</option>
            <option value="1">已启用</option>
            <option value="0">已禁用</option>
          </select>
        </div>
        <router-link to="/alert-rules/create" class="btn btn-primary">
          <Plus :size="16" /> 新建规则
        </router-link>
      </div>

      <div v-if="loading" class="loading"><div class="spinner"></div></div>

      <div v-else-if="rules.length === 0" class="empty-state">
        <div class="icon">
          <Bell :size="48" />
        </div>
        <p>暂无告警规则</p>
        <router-link to="/alert-rules/create" class="btn btn-primary mt-4">创建第一个规则</router-link>
      </div>

      <div v-else class="table-wrapper">
        <table>
          <thead>
            <tr>
              <th>状态</th>
              <th>规则名称</th>
              <th>数据源</th>
              <th>Lark 配置</th>
              <th>执行周期</th>
              <th>级别</th>
              <th>上次执行</th>
              <th>操作</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="rule in rules" :key="rule.id">
              <td>
                <label class="switch">
                  <input type="checkbox" :checked="rule.status === 1" @change="toggleRule(rule)">
                  <span class="slider"></span>
                </label>
              </td>
              <td style="white-space: nowrap;">
                <span style="font-weight: 500;">{{ rule.name }}</span>
                <span v-if="rule.alert_mode === 'not_found'" class="badge badge-warning" style="margin-left: 4px; font-size: 10px;">反向</span>
              </td>
              <td>
                <span v-if="(rule.data_source_type || 'es') === 'loki'" class="badge badge-warning">Loki</span>
                <span v-else class="badge badge-info">ES: {{ rule.es_connection_name }}</span>
              </td>
              <td>
                <span class="badge badge-success">{{ rule.lark_config_name }}</span>
              </td>
              <td class="text-sm">{{ rule.schedule }}</td>
              <td>
                <span class="badge" :class="severityClass(rule.severity)">{{ severityLabel(rule.severity) }}</span>
              </td>
              <td class="text-sm">
                <template v-if="rule.last_run_at">
                  {{ formatTime(rule.last_run_at) }}
                  <div v-if="rule.last_error" class="text-sm" style="color: var(--danger);" :title="rule.last_error">
                    Error
                  </div>
                </template>
                <span v-else class="text-secondary">-</span>
              </td>
              <td>
                <div class="actions">
                  <button class="btn btn-sm btn-outline" @click="runRule(rule)" title="立即执行">
                    <Play :size="14" />
                  </button>
                  <router-link :to="`/alert-rules/${rule.id}/edit`" class="btn btn-sm btn-outline" title="编辑">
                    <Pencil :size="14" />
                  </router-link>
                  <button class="btn btn-sm btn-outline" @click="viewLogs(rule)" title="查看日志">
                    <FileText :size="14" />
                  </button>
                  <button class="btn btn-sm btn-outline" style="color: var(--danger);" @click="deleteRule(rule)" title="删除">
                    <Trash2 :size="14" />
                  </button>
                </div>
              </td>
            </tr>
          </tbody>
        </table>
      </div>

      <div v-if="total > limit" class="pagination">
        <span>共 {{ total }} 条</span>
        <div class="pagination-btns">
          <button class="btn btn-sm btn-outline" :disabled="page <= 1" @click="page--; loadRules()">上一页</button>
          <span style="padding: 4px 12px;">{{ page }} / {{ Math.ceil(total / limit) }}</span>
          <button class="btn btn-sm btn-outline" :disabled="page >= Math.ceil(total / limit)" @click="page++; loadRules()">下一页</button>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup>
import { ref, onMounted } from 'vue'
import { useRouter } from 'vue-router'
import api from '../api'
import { useToast, useConfirm } from '../stores/ui'
import { Plus, Bell, Play, Pencil, FileText, Trash2 } from 'lucide-vue-next'

const toast = useToast()
const dialog = useConfirm()

const router = useRouter()
const rules = ref([])
const loading = ref(false)
const search = ref('')
const statusFilter = ref('')
const page = ref(1)
const limit = ref(20)
const total = ref(0)

let debounceTimer = null

function debounceLoad() {
  clearTimeout(debounceTimer)
  debounceTimer = setTimeout(() => { page.value = 1; loadRules() }, 300)
}

async function loadRules() {
  loading.value = true
  try {
    const params = { page: page.value, limit: limit.value }
    if (search.value) params.search = search.value
    if (statusFilter.value !== '') params.status = statusFilter.value
    const res = await api.get('/alert-rules', { params })
    if (res.code === 0) {
      rules.value = res.data
      total.value = res.total
    }
  } catch (e) { /* ignore */ }
  loading.value = false
}

async function toggleRule(rule) {
  try {
    await api.put(`/alert-rules/${rule.id}/toggle`)
    rule.status = rule.status === 1 ? 0 : 1
  } catch (e) { /* ignore */ }
}

async function runRule(rule) {
  const ok = await dialog.confirm({ title: '执行确认', message: `确认立即执行规则「${rule.name}」？` })
  if (!ok) return
  try {
    await api.post(`/alert-rules/${rule.id}/run`)
    toast.success('规则已触发执行')
  } catch (e) {
    toast.error('执行失败: ' + (e.response?.data?.message || e.message))
  }
}

async function deleteRule(rule) {
  const ok = await dialog.danger({ title: '删除规则', message: `确认删除规则「${rule.name}」？此操作不可恢复。` })
  if (!ok) return
  try {
    await api.delete(`/alert-rules/${rule.id}`)
    loadRules()
  } catch (e) { /* ignore */ }
}

function viewLogs(rule) {
  router.push({ path: '/alert-logs', query: { rule_id: rule.id } })
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

onMounted(loadRules)
</script>
