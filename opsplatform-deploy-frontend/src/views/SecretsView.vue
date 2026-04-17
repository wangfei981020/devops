<template>
  <div class="secret-layout">
    <!-- 左侧项目-环境 树 -->
    <aside class="secret-side">
      <div class="side-header">
        <div class="side-title">项目 - 环境</div>
        <div class="text-xs text-muted">共 {{ totalSecrets }} 个 Secret</div>
      </div>
      <div class="side-body">
        <div v-if="!projects.length" class="empty-state" style="padding: 20px">
          <div class="text-sm text-muted">暂无项目</div>
        </div>
        <div v-for="proj in projects" :key="proj.id" class="proj-node">
          <button class="proj-node-head" @click="toggleProj(proj.id)">
            <ChevronRight :size="11" :class="{ expanded: projExpanded[proj.id] }" class="chev" />
            <span class="proj-node-name">{{ proj.display_name || proj.name }}</span>
            <span class="text-xs text-muted">{{ (envsByProject[proj.id] || []).length }}</span>
          </button>
          <div v-if="projExpanded[proj.id]" class="pe-list">
            <button v-for="pe in envsByProject[proj.id] || []" :key="pe.id"
                    class="pe-item" :class="{ active: selectedPe === pe.id }"
                    @click="selectPe(pe)">
              <span class="dot" :style="{ background: envColor(pe.env_name) }"></span>
              <span>{{ pe.env_name }}</span>
              <span class="text-xs text-muted" style="margin-left: auto">{{ countForPe(pe.id) }}</span>
            </button>
          </div>
        </div>
      </div>
    </aside>

    <!-- 右侧 -->
    <div class="secret-main">
      <!-- 未选 -->
      <div v-if="!selectedPe" class="card" style="height: 100%; display: flex; align-items: center; justify-content: center; min-height: 400px">
        <div class="empty-state">
          <div class="empty-icon"><KeyRound :size="28" /></div>
          <div class="empty-title">选择项目-环境以查看 Secret</div>
          <div class="text-sm text-muted">左侧树展开项目后点击环境</div>
        </div>
      </div>

      <!-- 已选 -->
      <div v-else>
        <!-- KPI mini -->
        <div class="kpi-bar">
          <div class="kpi kpi-amber">
            <div class="kpi-icon"><KeyRound :size="18" /></div>
            <div class="kpi-body">
              <div class="kpi-label">Secret 数量</div>
              <div class="kpi-value">{{ list.length }}</div>
              <div class="kpi-foot">当前环境</div>
            </div>
          </div>
          <div class="kpi kpi-green">
            <div class="kpi-icon"><Check :size="18" /></div>
            <div class="kpi-body">
              <div class="kpi-label">已同步</div>
              <div class="kpi-value">{{ syncedCount }}</div>
              <div class="kpi-foot">写入 z-kv-secrets</div>
            </div>
          </div>
          <div class="kpi kpi-red">
            <div class="kpi-icon"><AlertTriangle :size="18" /></div>
            <div class="kpi-body">
              <div class="kpi-label">待同步/失败</div>
              <div class="kpi-value">{{ pendingCount + failedCount }}</div>
              <div class="kpi-foot">{{ failedCount }} 失败</div>
            </div>
          </div>
        </div>

        <!-- Toolbar -->
        <div class="toolbar">
          <div class="toolbar-left">
            <span class="toolbar-title">
              <strong>{{ selectedPeInfo?.project_name }}</strong>
              <span class="text-muted" style="margin: 0 6px">/</span>
              <span class="chip" :class="envChipClass(selectedPeInfo?.env_name)">
                <span class="dot" :style="{ background: envColor(selectedPeInfo?.env_name) }"></span>
                {{ selectedPeInfo?.env_name }}
              </span>
            </span>
            <span class="text-muted text-xs">→ z-kv-secrets chart</span>
          </div>
          <div class="toolbar-right">
            <div class="search-input">
              <Search :size="13" />
              <input v-model="keyword" placeholder="搜索 Secret..." />
            </div>
            <button class="btn btn-primary" @click="openCreate"><Plus :size="13" /> 新建 Secret</button>
          </div>
        </div>

        <!-- Table -->
        <div class="card" style="padding: 0; overflow: hidden">
          <GhostEmpty v-if="!filteredList.length"
            :headers="[{label:'名称',width:'260px'},{label:'类型',width:'90px'},{label:'同步状态',width:'120px'},{label:'同步时间',width:'150px'},{label:'描述'},{label:'操作',width:'220px'}]"
            :icon="KeyRound"
            :title="keyword ? '没有匹配的 Secret' : '该环境暂无 Secret'"
            description="Secret 会加密存储并同步到 z-kv-secrets chart"
            :cta-label="keyword ? '' : '新建 Secret'"
            @cta="openCreate" />
          <table v-else class="table">
            <thead>
              <tr>
                <th style="width: 260px">名称</th>
                <th style="width: 90px">类型</th>
                <th style="width: 120px">同步状态</th>
                <th style="width: 150px">同步时间</th>
                <th>描述</th>
                <th style="width: 220px; text-align: right">操作</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="s in filteredList" :key="s.id">
                <td>
                  <div class="secret-cell">
                    <div class="secret-avatar"><KeyRound :size="12" /></div>
                    <strong class="mono" style="font-size: 12px; word-break: break-all">{{ s.name }}</strong>
                  </div>
                </td>
                <td><code class="mono text-xs">{{ s.type }}</code></td>
                <td>
                  <span class="chip" :class="syncChip(s.sync_status)">
                    <span class="dot" :class="syncDot(s.sync_status)"></span>
                    {{ s.sync_status }}
                  </span>
                </td>
                <td class="text-xs text-gray mono">{{ s.synced_at ? s.synced_at.slice(0,16).replace('T',' ') : '—' }}</td>
                <td class="text-sm">{{ s.description || '—' }}</td>
                <td style="text-align: right">
                  <div class="actions" style="justify-content: flex-end">
                    <button class="btn btn-sm btn-outline" @click="onView(s)">查看</button>
                    <button class="btn btn-sm btn-outline" @click="onEdit(s)">编辑</button>
                    <button class="btn btn-sm btn-outline" @click="onRefs(s)">引用</button>
                    <button class="btn btn-sm btn-danger-light" @click="onDelete(s)">删除</button>
                  </div>
                </td>
              </tr>
            </tbody>
          </table>
        </div>
      </div>
    </div>

    <!-- Dialog -->
    <div v-if="dialogOpen" class="dialog-mask" @click.self="dialogOpen=false">
      <div class="dialog">
        <div class="dialog-title">{{ form.id ? '编辑 Secret' : '新建 Secret' }}</div>
        <div class="dialog-content">
          <div class="form-group">
            <label class="form-label">Secret 名 <span class="text-danger">*</span></label>
            <input v-model="form.name" class="form-input" :disabled="!!form.id" placeholder="如 g50-nacos-secret" />
          </div>
          <div class="form-group">
            <label class="form-label">类型</label>
            <input v-model="form.type" class="form-input" placeholder="Opaque" />
          </div>
          <div class="form-group">
            <label class="form-label">data (JSON) <span class="text-danger">*</span></label>
            <textarea v-model="form.data" class="form-textarea" rows="10" placeholder='{"username":"admin","password":"xxx"}'></textarea>
            <div class="form-help">value 会被 AES 加密存储</div>
          </div>
          <div class="form-group">
            <label class="form-label">描述</label>
            <input v-model="form.description" class="form-input" />
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
import { Plus, KeyRound, Check, AlertTriangle, Search, ChevronRight } from 'lucide-vue-next'
import { secretsApi, projectEnvsApi, projectsApi } from '../api'
import { success, error, info, confirm } from '../stores/ui'
import GhostEmpty from '../components/GhostEmpty.vue'

const projects = ref([])
const projectEnvs = ref([])
const allSecrets = ref([])
const list = ref([])
const projExpanded = ref({})
const selectedPe = ref(0)
const keyword = ref('')
const dialogOpen = ref(false)
const form = ref({ id: null, name: '', type: 'Opaque', data: '{}', description: '' })

const envsByProject = computed(() => {
  const map = {}
  for (const pe of projectEnvs.value) {
    if (!map[pe.project_id]) map[pe.project_id] = []
    map[pe.project_id].push(pe)
  }
  return map
})

const selectedPeInfo = computed(() => projectEnvs.value.find(pe => pe.id === selectedPe.value))
const filteredList = computed(() => {
  if (!keyword.value) return list.value
  const k = keyword.value.toLowerCase()
  return list.value.filter(s => s.name.toLowerCase().includes(k))
})

const totalSecrets = computed(() => allSecrets.value.length)
const syncedCount = computed(() => list.value.filter(s => s.sync_status === 'synced').length)
const pendingCount = computed(() => list.value.filter(s => s.sync_status === 'pending').length)
const failedCount = computed(() => list.value.filter(s => s.sync_status === 'failed').length)

function envColor(name) { return { dev: '#10b981', test: '#06b6d4', uat: '#f59e0b', prod: '#ef4444' }[name] || '#64748b' }
function envChipClass(name) { return { dev: 'chip-green', test: 'chip-cyan', uat: 'chip-amber', prod: 'chip-red' }[name] || 'chip-gray' }
function syncChip(s) { return { synced: 'chip-green', pending: 'chip-amber', failed: 'chip-red' }[s] || 'chip-gray' }
function syncDot(s) { return { synced: 'dot-success', pending: 'dot-warning', failed: 'dot-danger' }[s] || 'dot-gray' }

function countForPe(peId) { return allSecrets.value.filter(s => s.project_env_id === peId).length }
function toggleProj(id) { projExpanded.value[id] = !projExpanded.value[id] }

async function loadMeta() {
  try {
    const [p, pe] = await Promise.all([projectsApi.list(), projectEnvsApi.list()])
    projects.value = p.data || []
    projectEnvs.value = pe.data || []
    // 全量 secret 计数
    const all = []
    for (const peItem of projectEnvs.value) {
      try {
        const r = await secretsApi.list({ project_env_id: peItem.id })
        all.push(...(r.data || []))
      } catch (e) {}
    }
    allSecrets.value = all
    if (projects.value.length) projExpanded.value[projects.value[0].id] = true
  } catch (e) { error('加载失败: ' + e.message) }
}

async function selectPe(pe) {
  selectedPe.value = pe.id
  try { list.value = (await secretsApi.list({ project_env_id: pe.id })).data || [] }
  catch (e) { error('加载失败: ' + e.message) }
}

function openCreate() { form.value = { id: null, name: '', type: 'Opaque', data: '{}', description: '' }; dialogOpen.value = true }

async function onView(s) {
  try { await secretsApi.get(s.id, true); info('查看详情: ' + s.name + ' (解密数据功能待阶段3)') }
  catch (e) { error(e.message) }
}

async function onEdit(s) {
  try {
    const r = await secretsApi.get(s.id, true)
    form.value = { ...r.data, data: r.data.data || '{}' }
    dialogOpen.value = true
  } catch (e) { error(e.message) }
}

async function onSave() {
  if (!form.value.name) return error('Secret 名必填')
  try { JSON.parse(form.value.data || '{}') } catch { return error('data 不是合法 JSON') }
  try {
    const data = { ...form.value, project_env_id: selectedPe.value }
    if (form.value.id) await secretsApi.update(form.value.id, data)
    else await secretsApi.create(data)
    success('保存成功 (核心实现待阶段3)')
    dialogOpen.value = false
    selectPe({ id: selectedPe.value })
    loadMeta()
  } catch (e) { error('保存失败: ' + (e.response?.data?.message || e.message)) }
}

async function onDelete(s) {
  if (!await confirm({ title: '删除 Secret', message: `删除 "${s.name}"?`, danger: true })) return
  try { await secretsApi.delete(s.id); success('已删除'); selectPe({ id: selectedPe.value }); loadMeta() }
  catch (e) { error(e.message) }
}

async function onRefs(s) {
  try {
    const r = await secretsApi.referencedBy(s.id)
    const refs = r.data || []
    info(refs.length
      ? `被 ${refs.length} 个模块引用: ${refs.map(x => x.module_name).join(', ')}`
      : '没有模块引用此 Secret')
  } catch (e) { error(e.message) }
}

onMounted(loadMeta)
</script>

<style scoped>
.secret-layout { display: grid; grid-template-columns: 240px 1fr; gap: 10px; }

.secret-side {
  background: white; border: 1px solid #e2e8f0; border-radius: 6px;
  display: flex; flex-direction: column;
  max-height: calc(100vh - 100px); overflow: hidden;
  position: sticky; top: 62px;
}
.side-header { padding: 12px 14px; border-bottom: 1px solid #f1f5f9; }
.side-title { font-size: 13px; font-weight: 600; color: #0f172a; margin-bottom: 2px; }
.side-body { flex: 1; overflow-y: auto; padding: 4px 0; }

.proj-node-head {
  display: flex; align-items: center; gap: 6px;
  width: 100%; padding: 6px 12px;
  background: transparent; border: none;
  cursor: pointer;
  font-size: 12.5px; font-weight: 500; color: #334155;
  text-align: left;
}
.proj-node-head:hover { background: #f8fafc; }
.chev { transition: transform .15s; color: #94a3b8; flex-shrink: 0; }
.chev.expanded { transform: rotate(90deg); }
.proj-node-name { flex: 1; color: #0f172a; }

.pe-list { padding: 1px 0; }
.pe-item {
  display: flex; align-items: center; gap: 6px;
  width: 100%; padding: 5px 12px 5px 28px;
  background: transparent; border: none;
  cursor: pointer; font-size: 12px; color: #475569;
  text-align: left;
}
.pe-item:hover { background: #f8fafc; }
.pe-item.active { background: #eff6ff; color: #1e40af; font-weight: 600; border-right: 2px solid #1e40af; }

.secret-main { min-width: 0; }

.secret-cell { display: flex; align-items: center; gap: 6px; }
.secret-avatar {
  width: 22px; height: 22px; border-radius: 5px;
  background: #fef3c7; color: #b45309;
  display: flex; align-items: center; justify-content: center;
  flex-shrink: 0;
}
</style>
