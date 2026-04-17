<template>
  <div>
    <!-- KPI -->
    <div class="kpi-bar">
      <div class="kpi kpi-blue">
        <div class="kpi-icon"><FileCode :size="18" /></div>
        <div class="kpi-body">
          <div class="kpi-label">模板总数</div>
          <div class="kpi-value">{{ list.length }}</div>
          <div class="kpi-foot">全部可用</div>
        </div>
      </div>
      <div class="kpi kpi-cyan">
        <div class="kpi-icon"><Server :size="18" /></div>
        <div class="kpi-body">
          <div class="kpi-label">Backend</div>
          <div class="kpi-value">{{ backendCount }}</div>
          <div class="kpi-foot">后端服务模板</div>
        </div>
      </div>
      <div class="kpi kpi-amber">
        <div class="kpi-icon"><Monitor :size="18" /></div>
        <div class="kpi-body">
          <div class="kpi-label">Frontend</div>
          <div class="kpi-value">{{ frontendCount }}</div>
          <div class="kpi-foot">前端服务模板</div>
        </div>
      </div>
      <div class="kpi kpi-purple">
        <div class="kpi-icon"><Package :size="18" /></div>
        <div class="kpi-body">
          <div class="kpi-label">引用模块</div>
          <div class="kpi-value">{{ referencedCount }}</div>
          <div class="kpi-foot">模块基于这些模板创建</div>
        </div>
      </div>
    </div>

    <!-- Toolbar -->
    <div class="toolbar">
      <div class="toolbar-left">
        <span class="toolbar-title">Chart 模板</span>
        <div class="filter-tabs">
          <button class="filter-tab" :class="{ active: typeFilter==='' }" @click="typeFilter=''">全部 ({{ list.length }})</button>
          <button class="filter-tab" :class="{ active: typeFilter==='backend' }" @click="typeFilter='backend'">Backend ({{ backendCount }})</button>
          <button class="filter-tab" :class="{ active: typeFilter==='frontend' }" @click="typeFilter='frontend'">Frontend ({{ frontendCount }})</button>
        </div>
      </div>
      <div class="toolbar-right">
        <div class="search-input">
          <Search :size="13" />
          <input v-model="keyword" placeholder="搜索模板..." />
        </div>
        <button class="btn btn-primary" @click="openCreate"><Plus :size="13" /> 新增模板</button>
      </div>
    </div>

    <!-- Table -->
    <div class="card" style="padding: 0; overflow: hidden;">
      <GhostEmpty v-if="!filteredList.length"
        :headers="[{label:'名称',width:'180px'},{label:'类型',width:'100px'},{label:'版本',width:'70px'},{label:'描述'},{label:'Git 仓库/路径',width:'240px'},{label:'引用',width:'90px'},{label:'更新时间',width:'120px'},{label:'操作',width:'180px'}]"
        :icon="FileCode"
        :title="keyword || typeFilter ? '没有匹配的模板' : '暂无 Chart 模板'"
        description="新增 test1 / test2 / test3 等模板作为新建模块的脚手架"
        cta-label="新增模板"
        @cta="openCreate" />
      <table v-else class="table">
        <thead>
          <tr>
            <th style="width: 180px">名称</th>
            <th style="width: 100px">类型</th>
            <th style="width: 70px">版本</th>
            <th>描述</th>
            <th style="width: 240px">Git 仓库 / 路径</th>
            <th style="width: 90px; text-align: center">引用</th>
            <th style="width: 120px">更新时间</th>
            <th style="width: 180px; text-align: right">操作</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="t in filteredList" :key="t.id">
            <td>
              <div class="tpl-cell">
                <div class="tpl-avatar" :class="t.type === 'backend' ? 'tpl-avatar-blue' : 'tpl-avatar-amber'">
                  <Server v-if="t.type==='backend'" :size="14" />
                  <Monitor v-else :size="14" />
                </div>
                <strong>{{ t.name }}</strong>
              </div>
            </td>
            <td>
              <span class="badge" :class="t.type==='backend' ? 'badge-info' : 'badge-amber'">{{ t.type }}</span>
            </td>
            <td><code class="mono text-xs">{{ t.version }}</code></td>
            <td class="text-sm">{{ t.description || '—' }}</td>
            <td>
              <div v-if="t.git_repo" class="text-xs" style="color: #475569; word-break: break-all; line-height: 1.4">
                <div class="mono" style="font-size: 11px">{{ t.git_repo }}</div>
                <div v-if="t.chart_path" class="mono text-muted">{{ t.chart_path }}</div>
              </div>
              <span v-else class="text-muted text-xs">未配置</span>
            </td>
            <td class="text-center">
              <span class="chip chip-gray">{{ refCount(t.id) }}</span>
            </td>
            <td class="text-xs text-gray mono">{{ formatTime(t.updated_at) }}</td>
            <td style="text-align: right">
              <div class="actions" style="justify-content: flex-end">
                <button class="btn btn-sm btn-outline" @click="onPreview(t)">预览</button>
                <button class="btn btn-sm btn-outline" @click="openEdit(t)">编辑</button>
                <button class="btn btn-sm btn-danger-light" @click="onDelete(t)">删除</button>
              </div>
            </td>
          </tr>
        </tbody>
      </table>
    </div>

    <!-- Dialog -->
    <div v-if="dialogOpen" class="dialog-mask" @click.self="dialogOpen=false">
      <div class="dialog dialog-lg">
        <div class="dialog-title">{{ form.id ? '编辑 Chart 模板' : '新增 Chart 模板' }}</div>
        <div class="dialog-content">
          <div class="grid-2">
            <div class="form-group">
              <label class="form-label">模板名 <span class="text-danger">*</span></label>
              <input v-model="form.name" class="form-input" :disabled="!!form.id" placeholder="如 test1" />
            </div>
            <div class="form-group">
              <label class="form-label">类型 <span class="text-danger">*</span></label>
              <select v-model="form.type" class="form-select">
                <option value="backend">backend (后端)</option>
                <option value="frontend">frontend (前端)</option>
              </select>
            </div>
          </div>
          <div class="form-group">
            <label class="form-label">描述</label>
            <input v-model="form.description" class="form-input" placeholder="描述这个模板的用途" />
          </div>
          <div class="grid-2">
            <div class="form-group">
              <label class="form-label">Git 仓库</label>
              <input v-model="form.git_repo" class="form-input" placeholder="http://gitlab.xx/ops/chart-templates.git" />
            </div>
            <div class="form-group">
              <label class="form-label">Chart 路径</label>
              <input v-model="form.chart_path" class="form-input" placeholder="charts/test1" />
            </div>
          </div>
          <div class="form-group">
            <label class="form-label">默认 values.yaml</label>
            <textarea v-model="form.default_values" class="form-textarea" rows="10" placeholder="replicaCount: 1&#10;image:..."></textarea>
          </div>
          <div class="grid-2">
            <div class="form-group">
              <label class="form-label">默认探针 (JSON)</label>
              <textarea v-model="form.probe_config" class="form-textarea" rows="5" placeholder='{"liveness":...}'></textarea>
            </div>
            <div v-if="form.type==='frontend'" class="form-group">
              <label class="form-label">configmap_schema (JSON)</label>
              <textarea v-model="form.configmap_schema" class="form-textarea" rows="5"></textarea>
            </div>
            <div class="form-group">
              <label class="form-label">版本</label>
              <input v-model="form.version" class="form-input" placeholder="v1" />
            </div>
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
import { Plus, FileCode, Server, Monitor, Package, Search } from 'lucide-vue-next'
import { chartTemplatesApi, modulesApi } from '../api'
import { success, error, info, confirm } from '../stores/ui'
import GhostEmpty from '../components/GhostEmpty.vue'

const list = ref([])
const modules = ref([])
const dialogOpen = ref(false)
const typeFilter = ref('')
const keyword = ref('')
const form = ref({ id: null, name: '', type: 'backend', description: '', git_repo: '', chart_path: '',
  default_values: '', probe_config: '', configmap_schema: '', version: 'v1' })

const backendCount = computed(() => list.value.filter(t => t.type === 'backend').length)
const frontendCount = computed(() => list.value.filter(t => t.type === 'frontend').length)
const referencedCount = computed(() => modules.value.filter(m => list.value.find(t => t.id === m.template_id)).length)

const filteredList = computed(() => {
  let arr = list.value
  if (typeFilter.value) arr = arr.filter(t => t.type === typeFilter.value)
  if (keyword.value) {
    const k = keyword.value.toLowerCase()
    arr = arr.filter(t => t.name.toLowerCase().includes(k) || (t.description || '').toLowerCase().includes(k))
  }
  return arr
})

function refCount(tid) { return modules.value.filter(m => m.template_id === tid).length }
function formatTime(t) { return t ? t.slice(0, 16).replace('T', ' ') : '-' }

async function load() {
  try {
    const [t, m] = await Promise.all([chartTemplatesApi.list(), modulesApi.list()])
    list.value = t.data || []
    modules.value = m.data || []
  } catch (e) { error('加载失败: ' + e.message) }
}

function openCreate() {
  form.value = { id: null, name: '', type: 'backend', description: '', git_repo: '', chart_path: '',
    default_values: '', probe_config: '', configmap_schema: '', version: 'v1' }
  dialogOpen.value = true
}
function openEdit(t) { form.value = { ...t }; dialogOpen.value = true }

async function onSave() {
  if (!form.value.name || !form.value.type) return error('模板名和类型必填')
  try {
    if (form.value.id) await chartTemplatesApi.update(form.value.id, form.value)
    else await chartTemplatesApi.create(form.value)
    success('保存成功'); dialogOpen.value = false; load()
  } catch (e) { error('保存失败: ' + (e.response?.data?.message || e.message)) }
}

async function onDelete(t) {
  if (!await confirm({ title: '删除模板', message: `确认删除 "${t.name}"?`, danger: true })) return
  try { await chartTemplatesApi.delete(t.id); success('已删除'); load() }
  catch (e) { error('删除失败: ' + (e.response?.data?.message || e.message)) }
}

async function onPreview(t) {
  try {
    const r = await chartTemplatesApi.preview(t.id, {})
    info(r.data?.msg || '预览渲染功能待阶段2实现')
  } catch (e) { error(e.message) }
}

onMounted(load)
</script>

<style scoped>
.tpl-cell { display: flex; align-items: center; gap: 8px; }
.tpl-avatar { width: 24px; height: 24px; border-radius: 5px; display: flex; align-items: center; justify-content: center; flex-shrink: 0; }
.tpl-avatar-blue { background: #dbeafe; color: #1e40af; }
.tpl-avatar-amber { background: #fef3c7; color: #b45309; }
.grid-2 { display: grid; grid-template-columns: 1fr 1fr; gap: 12px; }
.text-center { text-align: center; }
</style>
