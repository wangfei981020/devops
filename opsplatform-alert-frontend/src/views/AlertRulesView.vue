<template>
  <div class="rules-layout">
    <!-- Left: Project Tree -->
    <aside class="project-sidebar">
      <div class="sidebar-header">
        <span class="sidebar-title">项目</span>
        <button class="btn-icon" @click="openCreateProject(0)" title="添加项目">
          <Plus :size="16" />
        </button>
      </div>

      <div class="project-list">
        <div class="project-item" :class="{ active: selectedProjectId === null }" @click="selectProject(null)">
          全部规则 <span class="count">{{ allTotal }}</span>
        </div>
        <div class="project-item" :class="{ active: selectedProjectId === -1 }" @click="selectProject(-1)">
          未分类 <span class="count">{{ unassignedCount }}</span>
        </div>

        <template v-for="p in topProjects" :key="p.id">
          <!-- Parent -->
          <div class="project-item parent" :class="{ active: selectedProjectId === p.id }">
            <button class="expand-btn" @click.stop="toggleExpand(p.id)" v-if="getChildren(p.id).length > 0">
              <ChevronRight :size="14" :class="{ rotated: expanded[p.id] }" />
            </button>
            <span class="expand-btn" v-else></span>
            <Folder :size="14" />
            <span class="project-name" @click="selectProject(p.id)">{{ p.name }}</span>
            <div class="project-actions">
              <button class="btn-icon-sm" @click.stop="openCreateProject(p.id)" title="添加子项目">
                <Plus :size="12" />
              </button>
              <button class="btn-icon-sm" @click.stop="editProject(p)" title="编辑">
                <Pencil :size="12" />
              </button>
            </div>
          </div>
          <!-- Children -->
          <template v-if="expanded[p.id]">
            <div v-for="child in getChildren(p.id)" :key="child.id"
              class="project-item child" :class="{ active: selectedProjectId === child.id }">
              <span class="project-name" @click="selectProject(child.id)">{{ child.name }}</span>
              <button class="btn-icon-sm" @click.stop="editProject(child)" title="编辑">
                <Pencil :size="12" />
              </button>
            </div>
          </template>
        </template>
      </div>
    </aside>

    <!-- Right: Rules List -->
    <main class="rules-main">
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
          <router-link :to="createRuleLink" class="btn btn-primary">
            <Plus :size="16" /> 新建规则
          </router-link>
        </div>

        <div v-if="loading" class="loading"><div class="spinner"></div></div>

        <div v-else-if="rules.length === 0" class="empty-state">
          <Bell :size="48" style="opacity: 0.4;" />
          <p>暂无告警规则</p>
          <router-link :to="createRuleLink" class="btn btn-primary mt-4">创建第一个规则</router-link>
        </div>

        <div v-else class="table-wrapper">
          <table>
            <thead>
              <tr>
                <th>状态</th>
                <th>规则名称</th>
                <th>项目</th>
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
                <td class="text-sm text-secondary">{{ getProjectName(rule.project_id) }}</td>
                <td>
                  <span v-if="(rule.data_source_type || 'es') === 'loki'" class="badge badge-warning">Loki</span>
                  <span v-else class="badge badge-info">ES: {{ rule.es_connection_name }}</span>
                </td>
                <td><span class="badge badge-success">{{ rule.lark_config_name }}</span></td>
                <td class="text-sm">{{ rule.schedule }}</td>
                <td><span class="badge" :class="severityClass(rule.severity)">{{ severityLabel(rule.severity) }}</span></td>
                <td class="text-sm">
                  <template v-if="rule.last_run_at">
                    {{ formatTime(rule.last_run_at) }}
                    <div v-if="rule.last_error" class="text-sm" style="color: var(--danger);" :title="rule.last_error">Error</div>
                  </template>
                  <span v-else class="text-secondary">-</span>
                </td>
                <td>
                  <div class="actions">
                    <button class="btn btn-sm btn-outline" @click="runRule(rule)" title="立即执行"><Play :size="14" /></button>
                    <router-link :to="`/alert-rules/${rule.id}/edit`" class="btn btn-sm btn-outline" title="编辑"><Pencil :size="14" /></router-link>
                    <button class="btn btn-sm btn-outline" @click="viewLogs(rule)" title="查看日志"><FileText :size="14" /></button>
                    <button class="btn btn-sm btn-outline" style="color: var(--danger);" @click="deleteRule(rule)" title="删除"><Trash2 :size="14" /></button>
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
    </main>

    <!-- Project Modal -->
    <div v-if="showProjectModal" class="modal-overlay" @click.self="showProjectModal = false">
      <div class="modal" style="min-width: 400px;">
        <div class="modal-header">
          <div class="modal-title">{{ editProjectId ? '编辑项目' : '添加项目' }}</div>
          <button class="btn-icon" @click="showProjectModal = false"><X :size="18" /></button>
        </div>
        <form @submit.prevent="handleProjectSubmit">
          <div class="form-group">
            <label class="form-label">项目名称 *</label>
            <input v-model="projectForm.name" class="form-input" placeholder="如: G32" required />
          </div>
          <div class="form-group">
            <label class="form-label">父项目</label>
            <select v-model.number="projectForm.parent_id" class="form-select">
              <option :value="0">无（顶级项目）</option>
              <option v-for="p in topProjects" :key="p.id" :value="p.id">{{ p.name }}</option>
            </select>
          </div>
          <div class="modal-footer">
            <button v-if="editProjectId" type="button" class="btn btn-outline" style="color: var(--danger); margin-right: auto;" @click="deleteProject">删除项目</button>
            <button type="button" class="btn btn-outline" @click="showProjectModal = false">取消</button>
            <button type="submit" class="btn btn-primary">保存</button>
          </div>
        </form>
      </div>
    </div>
  </div>
</template>

<script setup>
import { ref, computed, onMounted, reactive } from 'vue'
import { useRouter } from 'vue-router'
import api from '../api'
import { useToast, useConfirm } from '../stores/ui'
import { Plus, Bell, Play, Pencil, FileText, Trash2, Folder, ChevronRight, X } from 'lucide-vue-next'

const toast = useToast()
const dialog = useConfirm()
const router = useRouter()

// Projects
const projects = ref([])
const selectedProjectId = ref(null)
const showProjectModal = ref(false)
const editProjectId = ref(null)
const projectForm = ref({ name: '', parent_id: 0 })
const allTotal = ref(0)
const unassignedCount = ref(0)
const expanded = reactive({})

const topProjects = computed(() => projects.value.filter(p => p.parent_id === 0))
const getChildren = (parentId) => projects.value.filter(p => p.parent_id === parentId)

function toggleExpand(pid) {
  expanded[pid] = !expanded[pid]
}

function getProjectName(pid) {
  if (!pid) return '-'
  const p = projects.value.find(x => x.id === pid)
  return p ? p.name : '-'
}

const createRuleLink = computed(() => {
  if (selectedProjectId.value && selectedProjectId.value > 0) {
    return `/alert-rules/create?project_id=${selectedProjectId.value}`
  }
  return '/alert-rules/create'
})

async function loadProjects() {
  try {
    const res = await api.get('/projects')
    if (res.code === 0) {
      projects.value = res.data
      // Auto expand all top-level projects
      for (const p of res.data.filter(x => x.parent_id === 0)) {
        if (expanded[p.id] === undefined) expanded[p.id] = true
      }
    }
  } catch (e) { /* ignore */ }
}

function selectProject(pid) {
  selectedProjectId.value = pid
  page.value = 1
  loadRules()
}

function openCreateProject(parentId) {
  editProjectId.value = null
  projectForm.value = { name: '', parent_id: parentId }
  showProjectModal.value = true
}

function editProject(p) {
  editProjectId.value = p.id
  projectForm.value = { name: p.name, parent_id: p.parent_id }
  showProjectModal.value = true
}

async function handleProjectSubmit() {
  if (!projectForm.value.name) return
  try {
    if (editProjectId.value) {
      await api.put(`/projects/${editProjectId.value}`, projectForm.value)
      toast.success('项目已更新')
    } else {
      await api.post('/projects', projectForm.value)
      toast.success('项目已创建')
    }
    showProjectModal.value = false
    loadProjects()
  } catch (e) {
    toast.error(e.response?.data?.message || '操作失败')
  }
}

async function deleteProject() {
  if (!editProjectId.value) return
  const ok = await dialog.danger({ title: '删除项目', message: '确认删除该项目？项目下有规则或子项目时无法删除。' })
  if (!ok) return
  try {
    const res = await api.delete(`/projects/${editProjectId.value}`)
    if (res.code === 0) {
      showProjectModal.value = false
      toast.success('项目已删除')
      if (selectedProjectId.value === editProjectId.value) selectedProjectId.value = null
      loadProjects()
      loadRules()
    } else {
      toast.error(res.message)
    }
  } catch (e) {
    toast.error(e.response?.data?.message || '删除失败')
  }
}

// Rules
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
    if (selectedProjectId.value !== null) params.project_id = selectedProjectId.value
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

async function loadCounts() {
  try {
    const [allRes, unRes] = await Promise.all([
      api.get('/alert-rules', { params: { limit: 1 } }),
      api.get('/alert-rules', { params: { project_id: -1, limit: 1 } })
    ])
    if (allRes.code === 0) allTotal.value = allRes.total
    if (unRes.code === 0) unassignedCount.value = unRes.total
  } catch (e) { /* ignore */ }
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
    loadCounts()
  } catch (e) { /* ignore */ }
}

function viewLogs(rule) {
  router.push({ path: '/alert-logs', query: { rule_id: rule.id } })
}

function severityClass(s) {
  return { S1: 'badge-danger', S2: 'badge-warning', S3: 'badge-info' }[s] || 'badge-gray'
}
function severityLabel(s) {
  return { S1: 'S1 灾难', S2: 'S2 严重', S3: 'S3 警告' }[s] || s
}
function formatTime(t) {
  if (!t) return '-'
  return new Date(t).toLocaleString('zh-CN')
}

onMounted(() => {
  loadProjects()
  loadRules()
  loadCounts()
})
</script>

<style scoped>
.rules-layout {
  display: flex;
  gap: 16px;
  height: 100%;
}

.project-sidebar {
  width: 240px;
  flex-shrink: 0;
  background: var(--bg-card, #fff);
  border-radius: 8px;
  border: 1px solid var(--border, #e2e8f0);
  overflow-y: auto;
}

.sidebar-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  padding: 12px 12px 8px;
  border-bottom: 1px solid var(--border, #e2e8f0);
}

.sidebar-title { font-weight: 600; font-size: 14px; }

.project-list { padding: 4px 0; }

.project-item {
  display: flex;
  align-items: center;
  gap: 4px;
  padding: 7px 12px;
  cursor: pointer;
  font-size: 13px;
  transition: background 0.15s;
  position: relative;
}

.project-item:hover { background: var(--bg-hover, #f1f5f9); }

.project-item.active {
  background: var(--primary-light, #eff6ff);
  color: var(--primary, #3b82f6);
  font-weight: 500;
}

.project-item.child { padding-left: 44px; }

.project-name {
  flex: 1;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.project-item .count {
  margin-left: auto;
  font-size: 11px;
  color: var(--text-secondary, #94a3b8);
  flex-shrink: 0;
}

.expand-btn {
  width: 18px;
  height: 18px;
  display: flex;
  align-items: center;
  justify-content: center;
  background: none;
  border: none;
  cursor: pointer;
  padding: 0;
  color: var(--text-secondary);
  flex-shrink: 0;
  transition: transform 0.15s;
}

.expand-btn .rotated { transform: rotate(90deg); }

.project-actions {
  display: flex;
  gap: 2px;
  opacity: 0;
  transition: opacity 0.15s;
  flex-shrink: 0;
}

.project-item:hover .project-actions { opacity: 1; }

.btn-icon-sm {
  background: none;
  border: none;
  cursor: pointer;
  padding: 2px;
  color: var(--text-secondary);
}
.btn-icon-sm:hover { color: var(--primary, #3b82f6); }

.rules-main { flex: 1; min-width: 0; }
</style>
