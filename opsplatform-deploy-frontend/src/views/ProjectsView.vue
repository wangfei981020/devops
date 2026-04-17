<template>
  <div>
    <!-- KPI -->
    <div class="kpi-bar">
      <div class="kpi kpi-blue">
        <div class="kpi-icon"><Box :size="18" /></div>
        <div class="kpi-body">
          <div class="kpi-label">应用总数</div>
          <div class="kpi-value">{{ list.length }}</div>
          <div class="kpi-foot">{{ filteredList.length }} 已显示</div>
        </div>
      </div>
      <div class="kpi kpi-purple">
        <div class="kpi-icon"><Layers :size="18" /></div>
        <div class="kpi-body">
          <div class="kpi-label">项目-环境实例</div>
          <div class="kpi-value">{{ projectEnvs.length }}</div>
          <div class="kpi-foot">跨所有环境</div>
        </div>
      </div>
      <div class="kpi kpi-green">
        <div class="kpi-icon"><Package :size="18" /></div>
        <div class="kpi-body">
          <div class="kpi-label">模块总数</div>
          <div class="kpi-value">{{ modules.length }}</div>
          <div class="kpi-foot">{{ activeModuleCount }} 活跃</div>
        </div>
      </div>
      <div class="kpi kpi-amber">
        <div class="kpi-icon"><Clock :size="18" /></div>
        <div class="kpi-body">
          <div class="kpi-label">最近更新</div>
          <div class="kpi-value" style="font-size:16px">{{ lastUpdated }}</div>
          <div class="kpi-foot">应用配置</div>
        </div>
      </div>
    </div>

    <!-- Toolbar -->
    <div class="toolbar">
      <div class="toolbar-left">
        <span class="toolbar-title">应用管理</span>
        <span class="text-gray text-sm">共 {{ list.length }} 个应用</span>
      </div>
      <div class="toolbar-right">
        <div class="search-input">
          <Search :size="13" />
          <input v-model="keyword" placeholder="搜索项目..." />
        </div>
        <button class="btn btn-primary" @click="openCreate"><Plus :size="13" /> 新建项目</button>
      </div>
    </div>

    <!-- Table -->
    <div class="card" style="padding: 0; overflow: hidden">
      <GhostEmpty v-if="!filteredList.length"
        :headers="[{label:'项目',width:'220px'},{label:'代号',width:'100px'},{label:'描述'},{label:'环境',width:'180px'},{label:'模块',width:'80px'},{label:'更新时间',width:'140px'},{label:'操作',width:'220px'}]"
        :icon="Box"
        :title="keyword ? '没有匹配的项目' : '暂无应用'"
        :description="keyword ? '换个关键词试试' : '点击「新建项目」添加你的第一个应用'"
        :cta-label="keyword ? '' : '新建项目'"
        @cta="openCreate" />
      <table v-else class="table">
        <thead>
          <tr>
            <th style="width: 220px">项目</th>
            <th style="width: 100px">代号</th>
            <th>描述</th>
            <th style="width: 180px">环境</th>
            <th style="width: 80px; text-align: center">模块</th>
            <th style="width: 140px">更新时间</th>
            <th style="width: 220px; text-align: right">操作</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="p in filteredList" :key="p.id">
            <td>
              <div class="proj-cell">
                <div class="proj-avatar" :style="{ background: avatarColor(p.name) }">
                  {{ (p.display_name || p.name).charAt(0).toUpperCase() }}
                </div>
                <router-link :to="`/projects/${p.id}`" class="text-bold text-primary">
                  {{ p.display_name || p.name }}
                </router-link>
              </div>
            </td>
            <td><code class="mono text-xs">{{ p.name }}</code></td>
            <td class="text-sm" style="max-width: 260px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap">
              {{ p.description || '—' }}
            </td>
            <td>
              <div class="env-chips">
                <span v-for="pe in envsByProject[p.id] || []" :key="pe.id"
                      class="chip" :class="envChipClass(pe.env_name)" :title="pe.git_repo">
                  <span class="dot" :style="{ background: envColor(pe.env_name) }"></span>
                  {{ pe.env_name }}
                </span>
                <span v-if="!(envsByProject[p.id] || []).length" class="text-muted text-xs">未配置</span>
              </div>
            </td>
            <td class="text-center mono">
              <span class="chip chip-gray">{{ countModulesForProject(p.id) }}</span>
            </td>
            <td class="text-xs text-gray mono">{{ formatTime(p.updated_at) }}</td>
            <td style="text-align: right">
              <div class="actions" style="justify-content: flex-end">
                <router-link :to="`/projects/${p.id}`" class="btn btn-sm btn-outline">详情</router-link>
                <button class="btn btn-sm btn-outline" @click="openEdit(p)">编辑</button>
                <button class="btn btn-sm btn-danger-light" @click="onDelete(p)">删除</button>
              </div>
            </td>
          </tr>
        </tbody>
      </table>
    </div>

    <!-- Dialog -->
    <div v-if="dialogOpen" class="dialog-mask" @click.self="dialogOpen=false">
      <div class="dialog">
        <div class="dialog-title">{{ form.id ? '编辑项目' : '新建项目' }}</div>
        <div class="dialog-content">
          <div class="form-group">
            <label class="form-label">项目代号 <span class="text-danger">*</span></label>
            <input v-model="form.name" class="form-input" :disabled="!!form.id" placeholder="如 g50" />
            <div class="form-help">英文/数字/中划线, 创建后不可修改</div>
          </div>
          <div class="form-group">
            <label class="form-label">显示名</label>
            <input v-model="form.display_name" class="form-input" placeholder="如 G50 业务线" />
          </div>
          <div class="form-group">
            <label class="form-label">描述</label>
            <textarea v-model="form.description" class="form-textarea" rows="3"></textarea>
          </div>
        </div>
        <div class="dialog-actions">
          <button class="btn btn-outline" @click="dialogOpen=false">取消</button>
          <button class="btn btn-primary" @click="onSave">保存</button>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup>
import { ref, onMounted, computed } from 'vue'
import { Plus, Box, Layers, Package, Clock, Search } from 'lucide-vue-next'
import { projectsApi, projectEnvsApi, modulesApi } from '../api'
import { success, error, confirm } from '../stores/ui'
import GhostEmpty from '../components/GhostEmpty.vue'

const list = ref([])
const projectEnvs = ref([])
const modules = ref([])
const dialogOpen = ref(false)
const keyword = ref('')
const form = ref({ id: null, name: '', display_name: '', description: '' })

const envsByProject = computed(() => {
  const map = {}
  for (const pe of projectEnvs.value) {
    if (!map[pe.project_id]) map[pe.project_id] = []
    map[pe.project_id].push(pe)
  }
  return map
})
const activeModuleCount = computed(() => modules.value.filter(m => m.status === 'active').length)
const lastUpdated = computed(() => {
  const times = list.value.map(p => p.updated_at).filter(Boolean).sort().reverse()
  if (!times.length) return '—'
  const d = new Date(times[0]); const diff = (Date.now() - d.getTime()) / 1000
  if (diff < 60) return '刚刚'
  if (diff < 3600) return Math.floor(diff / 60) + '分钟前'
  if (diff < 86400) return Math.floor(diff / 3600) + '小时前'
  return Math.floor(diff / 86400) + '天前'
})

const filteredList = computed(() => {
  if (!keyword.value) return list.value
  const k = keyword.value.toLowerCase()
  return list.value.filter(p => p.name.toLowerCase().includes(k) || (p.display_name || '').toLowerCase().includes(k))
})

function countModulesForProject(pid) {
  const peIds = (envsByProject.value[pid] || []).map(pe => pe.id)
  return modules.value.filter(m => peIds.includes(m.project_env_id)).length
}

function envColor(name) {
  return { dev: '#10b981', test: '#06b6d4', uat: '#f59e0b', prod: '#ef4444' }[name] || '#64748b'
}
function envChipClass(name) {
  return { dev: 'chip-green', test: 'chip-cyan', uat: 'chip-amber', prod: 'chip-red' }[name] || 'chip-gray'
}

function avatarColor(name) {
  const colors = ['#1e40af', '#6d28d9', '#db2777', '#b45309', '#15803d', '#0e7490', '#4338ca', '#b91c1c']
  let hash = 0
  for (let i = 0; i < (name || 'U').length; i++) hash = name.charCodeAt(i) + ((hash << 5) - hash)
  return colors[Math.abs(hash) % colors.length]
}

function formatTime(t) { return t ? t.slice(0, 16).replace('T', ' ') : '-' }

async function load() {
  try {
    const [p, pe, m] = await Promise.all([projectsApi.list(), projectEnvsApi.list(), modulesApi.list()])
    list.value = p.data || []
    projectEnvs.value = pe.data || []
    modules.value = m.data || []
  } catch (e) { error('加载失败: ' + e.message) }
}

function openCreate() { form.value = { id: null, name: '', display_name: '', description: '' }; dialogOpen.value = true }
function openEdit(p) { form.value = { ...p }; dialogOpen.value = true }

async function onSave() {
  if (!form.value.name) return error('项目代号不能为空')
  try {
    if (form.value.id) await projectsApi.update(form.value.id, form.value)
    else await projectsApi.create(form.value)
    success('保存成功'); dialogOpen.value = false; load()
  } catch (e) { error('保存失败: ' + (e.response?.data?.message || e.message)) }
}

async function onDelete(p) {
  if (!await confirm({ title: '删除项目', message: `确认删除 "${p.display_name || p.name}"?`, danger: true })) return
  try { await projectsApi.delete(p.id); success('已删除'); load() }
  catch (e) { error('删除失败: ' + (e.response?.data?.message || e.message)) }
}

onMounted(load)
</script>

<style scoped>
.proj-cell { display: flex; align-items: center; gap: 8px; }
.proj-avatar {
  width: 26px; height: 26px; border-radius: 5px;
  display: flex; align-items: center; justify-content: center;
  color: white; font-weight: 700; font-size: 12px;
  flex-shrink: 0;
  font-family: 'Fira Code', monospace;
}
.env-chips { display: flex; gap: 4px; flex-wrap: wrap; }
.text-center { text-align: center; }
</style>
