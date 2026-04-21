<template>
  <div class="pc">
    <!-- KPI 行 -->
    <div class="kpis">
      <div class="kpi">
        <div class="k-lbl">项目总数</div>
        <div class="k-val mono">{{ projectCount }}</div>
        <div class="k-d">{{ envs.length }} 个环境</div>
      </div>
      <div class="kpi">
        <div class="k-lbl">UAT 环境</div>
        <div class="k-val mono" style="color:var(--success-dark)">{{ uatCount }}</div>
        <div class="k-d">auto-sync ON · {{ uatSyncedCount }}</div>
      </div>
      <div class="kpi">
        <div class="k-lbl">PROD 环境</div>
        <div class="k-val mono" style="color:var(--danger-dark)">{{ prodCount }}</div>
        <div class="k-d">需人工 sync</div>
      </div>
      <div class="kpi">
        <div class="k-lbl">模块总数</div>
        <div class="k-val mono">{{ totalModules }}</div>
        <div class="k-d">跨所有环境</div>
      </div>
      <div class="kpi">
        <div class="k-lbl">最近 24h 发布</div>
        <div class="k-val mono">{{ recent24hCount }}</div>
        <div class="k-d"><span class="ok">{{ recent24hOkCount }}</span> 成功</div>
      </div>
    </div>

    <!-- 主面板 -->
    <div class="tree-panel">
      <div class="tree-head">
        <div class="head-l">
          <span class="title">项目环境列表</span>
          <span class="sub">项目 <b>{{ projectCount }}</b> · 环境 <b>{{ envs.length }}</b></span>
        </div>
        <div class="head-r">
          <div class="search-box">
            <el-icon class="search-ico"><Search /></el-icon>
            <input v-model="q" placeholder="搜索项目或环境..." />
          </div>
          <button class="btn-refresh" @click="load" :disabled="loading" title="刷新">
            <el-icon :class="{spin: loading}"><RefreshRight /></el-icon>
          </button>
          <button class="add-btn" @click="openCreate()">
            <el-icon><Plus /></el-icon>新增
          </button>
        </div>
      </div>

      <div class="tree-body">
        <div v-for="proj in filteredGroups" :key="proj.name" class="proj-group">
          <div class="proj-row" @click="toggle(proj.name)">
            <el-icon class="chev" :class="{opened: opened[proj.name]}"><ArrowRight /></el-icon>
            <el-icon class="ico"><Folder /></el-icon>
            <span class="proj-name">{{ proj.name }}</span>
            <span class="count">{{ proj.envs.length }}</span>
            <div class="proj-stats">
              <span class="p-stat" v-if="proj.totalModules">
                <el-icon><Box /></el-icon>{{ proj.totalModules }} 模块
              </span>
              <span class="p-stat" v-if="proj.lastDeployAt">
                <el-icon><Clock /></el-icon>{{ fromNow(proj.lastDeployAt) }}
              </span>
            </div>
            <button class="row-btn plus" title="在此项目下新增环境" @click.stop="openCreate(proj.name)">
              <el-icon><Plus /></el-icon>
            </button>
          </div>
          <div v-if="opened[proj.name]" class="env-list">
            <div class="env-head">
              <span>环境</span>
              <span>分支</span>
              <span>Namespace</span>
              <span>Auto-sync</span>
              <span>模块</span>
              <span>最近发布</span>
              <span>状态</span>
              <span></span>
            </div>
            <div v-for="e in proj.envs" :key="e.id" class="env-row" @click="openEdit(e)">
              <span :class="'env-badge ' + e.env_type">{{ e.env_type }}</span>
              <span class="env-branch mono">{{ e.git_branch }}</span>
              <span class="env-ns mono">{{ e.namespace || '—' }}</span>
              <span class="env-sync">
                <span class="dot" :class="e.auto_sync ? 'ok' : 'off'"></span>
                {{ e.auto_sync ? 'ON' : 'OFF' }}
              </span>
              <span class="env-mods">
                <b v-if="envStats[e.id]?.moduleCount != null">{{ envStats[e.id].moduleCount }}</b>
                <span v-else class="muted">—</span>
              </span>
              <span class="env-last">
                <span v-if="envStats[e.id]?.lastDeployAt" :title="envStats[e.id].lastDeployAt">
                  {{ fromNow(envStats[e.id].lastDeployAt) }}
                </span>
                <span v-else class="muted">—</span>
              </span>
              <span class="env-status">
                <span v-if="envStats[e.id]?.lastStatus" :class="'st-pill ' + envStats[e.id].lastStatus">
                  {{ statusLabel(envStats[e.id].lastStatus) }}
                </span>
                <span v-else class="muted">—</span>
              </span>
              <button class="row-btn edit" title="编辑" @click.stop="openEdit(e)">
                <el-icon><EditPen /></el-icon>
              </button>
            </div>
          </div>
        </div>
        <div v-if="!envs.length" class="empty-tree">
          <div class="et-t">还没有项目环境</div>
          <div class="et-d">点击右上角「+ 新增」创建第一个</div>
        </div>
        <div v-else-if="!filteredGroups.length" class="empty-tree">
          <div class="et-t">没有匹配的项目</div>
        </div>
      </div>
    </div>

    <!-- 新增 / 编辑 弹窗 -->
    <el-dialog
      v-model="dialogVis"
      :title="isEdit ? '编辑项目环境' : '新增项目环境'"
      width="680px"
      :close-on-click-modal="false"
      custom-class="env-dialog">

      <div class="dlg-body">
        <div class="sec-lbl">基本信息</div>
        <div class="row2">
          <div class="field">
            <label>项目名称 <span class="req">*</span></label>
            <input v-if="isEdit" :value="form.project_name" class="inp mono" disabled />
            <input v-else
              v-model="form.project_name"
              class="inp mono"
              placeholder="如: g33"
              list="proj-options" />
            <datalist id="proj-options">
              <option v-for="p in projects" :key="p" :value="p" />
            </datalist>
          </div>
          <div class="field">
            <label>环境类型 <span class="req">*</span></label>
            <div class="rb-grp">
              <button :class="['rb', 'uat', {active: form.env_type === 'uat'}]"
                :disabled="isEdit" @click="setEnvType('uat')">UAT</button>
              <button :class="['rb', 'prod', {active: form.env_type === 'prod'}]"
                :disabled="isEdit" @click="setEnvType('prod')">PROD</button>
            </div>
          </div>
        </div>
        <div class="field">
          <label>显示名 <span class="hint">可选</span></label>
          <input v-model="form.display_name" class="inp" />
        </div>
        <div class="field preview-name" v-if="!isEdit && form.project_name && form.env_type">
          将创建：<code class="mono">{{ form.project_name }}-{{ form.env_type }}</code>
        </div>

        <div class="sec-lbl">GitLab 仓库</div>
        <div class="field">
          <label>Git Repo URL <span class="req">*</span></label>
          <input v-model="form.git_repo" class="inp mono" placeholder="http://gitlab.xx/ops/..." />
        </div>
        <div class="row2">
          <div class="field">
            <label>分支</label>
            <input v-model="form.git_branch" class="inp mono" />
          </div>
          <div class="field">
            <label>Chart 基础路径</label>
            <input v-model="form.chart_base_path" class="inp mono" placeholder="argocd-apps/charts/g33-uat" />
          </div>
        </div>

        <div class="sec-lbl">Kubernetes · ArgoCD</div>
        <div class="row2">
          <div class="field">
            <label>K8s Namespace</label>
            <input v-model="form.namespace" class="inp mono" />
          </div>
          <div class="field">
            <label>ArgoCD URL</label>
            <input v-model="form.argocd_url" class="inp mono" />
          </div>
        </div>
        <div class="field">
          <label>ArgoCD Token <span class="hint">{{ isEdit ? '留空则不更新' : '必填' }}</span></label>
          <input v-model="form.argocd_token" type="password" class="inp" :placeholder="isEdit ? '••••••••' : '粘贴 ArgoCD token'" />
        </div>
        <div class="field">
          <label>Auto Sync</label>
          <div class="switch-line">
            <el-switch v-model="form.auto_sync" :active-value="1" :inactive-value="0" :disabled="form.env_type === 'prod'" />
            <span class="switch-hint">PROD 强制关闭</span>
          </div>
        </div>

        <div class="sec-lbl">通知</div>
        <div class="field">
          <label>Lark Webhook <span class="hint">可空 · 留空使用系统默认</span></label>
          <input v-model="form.lark_webhook" class="inp mono" />
        </div>
      </div>

      <template #footer>
        <div class="dlg-foot">
          <div class="foot-l">
            <button v-if="isEdit" class="btn-danger" @click="onDelete">
              <el-icon><Delete /></el-icon>删除
            </button>
            <button v-if="isEdit" class="btn-ghost" @click="onTestGit" :disabled="testing.git">测试 Git</button>
            <button v-if="isEdit" class="btn-ghost" @click="onTestArgo" :disabled="testing.argo">测试 ArgoCD</button>
          </div>
          <div class="foot-r">
            <button class="btn-ghost" @click="dialogVis = false">取消</button>
            <button class="btn-primary" @click="onSave" :disabled="saving">{{ saving ? '保存中...' : '保存' }}</button>
          </div>
        </div>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { ref, reactive, computed, onMounted } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Plus, Search, Folder, ArrowRight, EditPen, Delete, Box, Clock, RefreshRight } from '@element-plus/icons-vue'
import dayjs from 'dayjs'
import {
  listProjectEnvs, createProjectEnv, updateProjectEnv, deleteProjectEnv,
  testProjectEnvGit, testProjectEnvArgocd,
  listModules, listDeployments
} from '../api'

const envs = ref([])
const envStats = ref({})  // { [envId]: { moduleCount, lastDeployAt, lastStatus } }
const q = ref('')
const opened = reactive({})
const dialogVis = ref(false)
const isEdit = ref(false)
const editingID = ref(null)
const saving = ref(false)
const loading = ref(false)
const testing = reactive({ git: false, argo: false })

const blank = () => ({
  project_name: '', display_name: '', env_type: 'uat',
  git_repo: '', git_branch: 'main', chart_base_path: '',
  namespace: '', argocd_url: '', argocd_token: '',
  lark_webhook: '', auto_sync: 1
})
const form = reactive(blank())

function projectOf(e) {
  const suffix = '-' + e.env_type
  return e.name.endsWith(suffix) ? e.name.slice(0, -suffix.length) : e.name
}

function fromNow(s) {
  if (!s) return ''
  const d = dayjs(s)
  const diff = dayjs().diff(d, 'minute')
  if (diff < 1) return '刚刚'
  if (diff < 60) return diff + ' 分钟前'
  if (diff < 60 * 24) return Math.floor(diff / 60) + ' 小时前'
  const days = Math.floor(diff / (60 * 24))
  if (days < 30) return days + ' 天前'
  return d.format('YYYY-MM-DD')
}

function statusLabel(s) {
  return { success: '成功', partial: '部分', failed: '失败', pending: '进行中', no_change: '无变化' }[s] || s
}

const projects = computed(() => {
  const s = new Set()
  envs.value.forEach(e => s.add(projectOf(e)))
  return [...s].sort()
})
const projectCount = computed(() => projects.value.length)
const uatCount = computed(() => envs.value.filter(e => e.env_type === 'uat').length)
const prodCount = computed(() => envs.value.filter(e => e.env_type === 'prod').length)
const uatSyncedCount = computed(() => envs.value.filter(e => e.env_type === 'uat' && e.auto_sync).length)
const totalModules = computed(() => Object.values(envStats.value).reduce((a, s) => a + (s?.moduleCount || 0), 0))
const recent24hCount = computed(() => {
  const cutoff = dayjs().subtract(24, 'hour')
  return Object.values(envStats.value).filter(s => s?.lastDeployAt && dayjs(s.lastDeployAt).isAfter(cutoff)).length
})
const recent24hOkCount = computed(() => {
  const cutoff = dayjs().subtract(24, 'hour')
  return Object.values(envStats.value).filter(s =>
    s?.lastDeployAt && s.lastStatus === 'success' && dayjs(s.lastDeployAt).isAfter(cutoff)
  ).length
})

const groups = computed(() => {
  const map = {}
  envs.value.forEach(e => {
    const p = projectOf(e)
    if (!map[p]) map[p] = []
    map[p].push(e)
  })
  return Object.entries(map)
    .map(([name, envList]) => {
      const sorted = envList.sort((a, b) => (a.env_type === 'uat' ? -1 : 1))
      const totalMods = sorted.reduce((a, e) => a + (envStats.value[e.id]?.moduleCount || 0), 0)
      const lastDeploys = sorted
        .map(e => envStats.value[e.id]?.lastDeployAt)
        .filter(Boolean)
        .sort((a, b) => dayjs(b).valueOf() - dayjs(a).valueOf())
      return { name, envs: sorted, totalModules: totalMods, lastDeployAt: lastDeploys[0] || null }
    })
    .sort((a, b) => a.name.localeCompare(b.name))
})

const filteredGroups = computed(() => {
  const qq = q.value.trim().toLowerCase()
  if (!qq) return groups.value
  return groups.value
    .map(g => ({
      ...g,
      envs: g.name.toLowerCase().includes(qq)
        ? g.envs
        : g.envs.filter(e => e.name.toLowerCase().includes(qq) || e.env_type.includes(qq))
    }))
    .filter(g => g.envs.length > 0)
})

function toggle(pn) { opened[pn] = !opened[pn] }

async function load() {
  loading.value = true
  try {
    envs.value = (await listProjectEnvs()) || []
    groups.value.forEach(g => { if (opened[g.name] === undefined) opened[g.name] = true })
    // 并行加载每个 env 的模块数 + 最近发布
    const statsResults = await Promise.all(envs.value.map(async (e) => {
      try {
        const [mods, deps] = await Promise.all([
          listModules(e.id).catch(() => []),
          listDeployments({ project_env_id: e.id, page_size: 1 }).catch(() => ({ list: [] }))
        ])
        const last = deps.list?.[0]
        return {
          id: e.id,
          moduleCount: (mods || []).length,
          lastDeployAt: last?.created_at || null,
          lastStatus: last?.status || null
        }
      } catch {
        return { id: e.id, moduleCount: 0, lastDeployAt: null, lastStatus: null }
      }
    }))
    const map = {}
    statsResults.forEach(s => { map[s.id] = s })
    envStats.value = map
  } finally { loading.value = false }
}

function openCreate(prefillProject) {
  isEdit.value = false
  editingID.value = null
  Object.assign(form, blank())
  if (typeof prefillProject === 'string') form.project_name = prefillProject
  dialogVis.value = true
}

function openEdit(e) {
  isEdit.value = true
  editingID.value = e.id
  Object.assign(form, {
    project_name: projectOf(e),
    display_name: e.display_name || '',
    env_type: e.env_type,
    git_repo: e.git_repo || '',
    git_branch: e.git_branch || 'main',
    chart_base_path: e.chart_base_path || '',
    namespace: e.namespace || '',
    argocd_url: e.argocd_url || '',
    argocd_token: '',
    lark_webhook: e.lark_webhook || '',
    auto_sync: e.auto_sync
  })
  dialogVis.value = true
}

function setEnvType(t) {
  if (isEdit.value) return
  form.env_type = t
  if (t === 'prod') form.auto_sync = 0
}

async function onSave() {
  if (!form.project_name.trim()) { ElMessage.warning('请填写项目名称'); return }
  if (!form.env_type) { ElMessage.warning('请选择环境类型'); return }
  if (!form.git_repo.trim()) { ElMessage.warning('请填写 Git Repo URL'); return }

  const payload = {
    name: form.project_name.trim() + '-' + form.env_type,
    display_name: form.display_name.trim(),
    env_type: form.env_type,
    git_repo: form.git_repo.trim(),
    git_branch: form.git_branch.trim() || 'main',
    chart_base_path: form.chart_base_path.trim(),
    namespace: form.namespace.trim(),
    argocd_url: form.argocd_url.trim(),
    argocd_token: form.argocd_token,
    lark_webhook: form.lark_webhook.trim(),
    auto_sync: form.auto_sync
  }
  saving.value = true
  try {
    if (isEdit.value) {
      await updateProjectEnv(editingID.value, payload)
      ElMessage.success('已保存')
    } else {
      await createProjectEnv(payload)
      ElMessage.success('已创建')
    }
    dialogVis.value = false
    await load()
  } finally { saving.value = false }
}

async function onDelete() {
  try {
    await ElMessageBox.confirm(
      `确认删除 ${form.project_name}-${form.env_type}？该操作不可恢复。`,
      '删除确认',
      { type: 'warning', confirmButtonText: '确认删除', cancelButtonText: '取消' }
    )
  } catch (_) { return }
  await deleteProjectEnv(editingID.value)
  ElMessage.success('已删除')
  dialogVis.value = false
  await load()
}

async function onTestGit() {
  testing.git = true
  try { await testProjectEnvGit(editingID.value); ElMessage.success('Git 连通 OK') }
  finally { testing.git = false }
}
async function onTestArgo() {
  testing.argo = true
  try {
    const r = await testProjectEnvArgocd(editingID.value)
    ElMessage.success(`ArgoCD OK (${r.version})`)
  } finally { testing.argo = false }
}

onMounted(load)
</script>

<style scoped>
.pc { }

/* ===== KPI 行 ===== */
.kpis {
  display: grid;
  grid-template-columns: repeat(5, 1fr);
  gap: 14px;
  margin-bottom: 14px;
}
.kpi {
  background: var(--bg-card);
  border: 1px solid var(--border);
  border-radius: var(--radius);
  padding: 14px 18px;
}
.k-lbl { font-size: 11px; color: var(--text-3); text-transform: uppercase; letter-spacing: .6px; font-weight: 600; margin-bottom: 4px; }
.k-val { font-size: 24px; font-weight: 700; color: var(--text); letter-spacing: -.5px; line-height: 1.1; }
.k-d { font-size: 11.5px; color: var(--text-3); margin-top: 4px; }
.k-d .ok { color: var(--success-dark); font-weight: 600; }

/* ===== 主面板 ===== */
.tree-panel {
  background: var(--bg-card);
  border: 1px solid var(--border);
  border-radius: var(--radius);
  overflow: hidden;
}
.tree-head {
  padding: 14px 18px;
  border-bottom: 1px solid var(--border-soft);
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 14px;
}
.head-l { display: flex; align-items: baseline; gap: 10px; }
.head-l .title { font-weight: 600; font-size: 14px; color: var(--text); }
.head-l .sub { color: var(--text-3); font-size: 12px; }
.head-l .sub b { color: var(--text); font-family: var(--mono); font-weight: 600; }

.head-r { display: flex; align-items: center; gap: 8px; }

.search-box { position: relative; }
.search-box .search-ico {
  position: absolute; left: 10px; top: 50%; transform: translateY(-50%);
  color: var(--text-3); font-size: 13px;
}
.search-box input {
  width: 260px; padding: 7px 10px 7px 32px;
  background: var(--bg-input); border: 1px solid var(--border); border-radius: 5px;
  font: 500 12.5px var(--body); color: var(--text);
}
.search-box input:focus { outline: none; border-color: var(--primary); background: #fff; }

.btn-refresh {
  background: #fff; border: 1px solid var(--border); color: var(--text-2);
  padding: 6px 10px; border-radius: 5px; cursor: pointer;
  display: flex; align-items: center; justify-content: center;
}
.btn-refresh:hover { border-color: var(--primary); color: var(--primary); }
.btn-refresh .spin { animation: spin 1s linear infinite; }
@keyframes spin { to { transform: rotate(360deg); } }

.add-btn {
  display: flex; align-items: center; gap: 4px;
  background: var(--primary); color: #fff; border: none;
  padding: 7px 14px; border-radius: 5px;
  font: 500 12.5px var(--body); cursor: pointer;
}
.add-btn:hover { background: var(--primary-dark); }
.add-btn .el-icon { font-size: 14px; }

.tree-body { padding: 4px 0 10px; }

.proj-group { border-bottom: 1px solid var(--border-soft); }
.proj-group:last-child { border-bottom: none; }

.proj-row {
  display: flex; align-items: center; gap: 8px;
  padding: 12px 18px;
  cursor: pointer;
  font-size: 13px;
  color: var(--text);
  transition: background .1s;
}
.proj-row:hover { background: var(--bg-hover); }
.proj-row .chev { font-size: 10px; color: var(--text-3); transition: transform .15s; }
.proj-row .chev.opened { transform: rotate(90deg); }
.proj-row .ico { color: var(--text-3); font-size: 15px; }
.proj-row .proj-name { font-family: var(--mono); font-weight: 600; font-size: 14px; }
.proj-row .count {
  font: 600 11px var(--mono); color: var(--text-3);
  background: var(--bg-hover);
  padding: 1px 8px; border-radius: 10px;
}

.proj-stats { margin-left: auto; display: flex; gap: 14px; color: var(--text-3); font-size: 12px; }
.p-stat { display: inline-flex; align-items: center; gap: 4px; }
.p-stat .el-icon { font-size: 13px; }

.row-btn {
  background: transparent; border: none; color: var(--text-3);
  padding: 4px 6px; border-radius: 4px; cursor: pointer;
  display: flex; align-items: center; justify-content: center; transition: all .12s;
}
.row-btn:hover { background: var(--primary-bg); color: var(--primary); }
.row-btn .el-icon { font-size: 14px; }

.env-list { background: var(--bg-input); padding: 6px 0 4px; }

.env-head {
  display: grid;
  grid-template-columns: 64px 88px 140px 100px 70px 120px 90px 36px;
  gap: 18px;
  padding: 6px 18px 6px 48px;
  font-size: 10.5px;
  color: var(--text-3);
  text-transform: uppercase;
  letter-spacing: .6px;
  font-weight: 600;
}

.env-row {
  display: grid;
  grid-template-columns: 64px 88px 140px 100px 70px 120px 90px 36px;
  gap: 18px;
  align-items: center;
  padding: 10px 18px 10px 48px;
  cursor: pointer;
  transition: background .1s;
  font-size: 12.5px;
  border-left: 2px solid transparent;
}
.env-row:hover { background: #fff; border-left-color: var(--primary); }

.env-badge {
  font-family: var(--mono); font-size: 10.5px; font-weight: 600;
  padding: 3px 8px; border-radius: 3px;
  letter-spacing: .5px; text-transform: uppercase;
  text-align: center;
}
.env-badge.uat { background: var(--success-bg); color: var(--success-dark); }
.env-badge.prod { background: var(--danger-bg); color: var(--danger-dark); }

.env-branch, .env-ns { color: var(--text-2); font-size: 12px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.env-sync { font: 500 11.5px var(--mono); color: var(--text-2); display: flex; align-items: center; gap: 6px; }
.dot { width: 6px; height: 6px; border-radius: 50%; }
.dot.ok { background: var(--success); }
.dot.off { background: var(--text-3); }

.env-mods b { font-family: var(--mono); font-weight: 600; font-size: 13px; color: var(--text); }
.env-last { color: var(--text-2); font-size: 12px; }
.muted { color: var(--text-3); }

.st-pill {
  display: inline-block;
  padding: 1px 7px; border-radius: 10px;
  font-size: 10.5px; font-weight: 500;
}
.st-pill.success { background: var(--success-bg); color: var(--success-dark); }
.st-pill.partial { background: #fef3c7; color: #92400e; }
.st-pill.failed { background: var(--danger-bg); color: var(--danger-dark); }
.st-pill.pending { background: #eff6ff; color: #1e40af; }
.st-pill.no_change { background: var(--bg-hover); color: var(--text-2); }

.empty-tree { padding: 60px 20px; text-align: center; }
.et-t { color: var(--text-2); font-size: 14px; font-weight: 500; margin-bottom: 4px; }
.et-d { color: var(--text-3); font-size: 12.5px; }

/* ===== 弹窗 ===== */
:deep(.env-dialog) { border-radius: 8px; }
:deep(.env-dialog .el-dialog__header) {
  padding: 16px 22px; margin: 0;
  border-bottom: 1px solid var(--border-soft);
  background: var(--bg-card); border-radius: 8px 8px 0 0;
}
:deep(.env-dialog .el-dialog__title) { font-weight: 600; font-size: 15px; color: var(--text); }
:deep(.env-dialog .el-dialog__body) { padding: 0; }
:deep(.env-dialog .el-dialog__footer) {
  padding: 14px 22px; border-top: 1px solid var(--border-soft);
  background: var(--bg-input); border-radius: 0 0 8px 8px;
}

.dlg-body { padding: 18px 22px 22px; max-height: 60vh; overflow-y: auto; }
.sec-lbl {
  font-size: 10.5px; color: var(--text-3);
  text-transform: uppercase; letter-spacing: .8px; font-weight: 600;
  margin: 18px 0 10px; padding-bottom: 6px;
  border-bottom: 1px solid var(--border-soft);
}
.sec-lbl:first-child { margin-top: 0; }

.row2 { display: grid; grid-template-columns: 1fr 1fr; gap: 14px; }
.field { display: flex; flex-direction: column; gap: 5px; margin-bottom: 12px; }
.field label { font-size: 11.5px; color: var(--text-2); font-weight: 500; }
.field label .req { color: var(--danger); margin-left: 2px; }
.field label .hint { color: var(--text-3); font-weight: 400; font-size: 11px; margin-left: 6px; }

.inp {
  padding: 8px 10px; border: 1px solid var(--border); border-radius: 5px;
  font: 500 13px var(--body); color: var(--text); background: #fff;
  transition: all .15s; width: 100%;
}
.inp:focus { outline: none; border-color: var(--primary); box-shadow: 0 0 0 3px rgba(59,130,246,.12); }
.inp.mono { font-family: var(--mono); font-size: 12.5px; }
.inp:disabled { background: var(--bg-hover); color: var(--text-3); cursor: not-allowed; }

.rb-grp { display: flex; gap: 6px; }
.rb {
  flex: 1; background: #fff; border: 1px solid var(--border); color: var(--text-2);
  padding: 7px 14px; border-radius: 5px;
  font: 600 12px var(--mono); text-transform: uppercase; letter-spacing: .6px;
  cursor: pointer; transition: all .12s;
}
.rb:hover:not(:disabled) { border-color: var(--text-2); }
.rb:disabled { opacity: .7; cursor: not-allowed; }
.rb.uat.active { background: var(--success); border-color: var(--success); color: #fff; }
.rb.prod.active { background: var(--danger); border-color: var(--danger); color: #fff; }

.preview-name {
  padding: 8px 12px; background: var(--primary-bg); border-radius: 4px;
  font-size: 12px; color: var(--text-2); margin-bottom: 0;
}
.preview-name code {
  background: #fff; padding: 2px 8px; border-radius: 3px;
  color: var(--primary); font-weight: 600; margin-left: 4px;
}

.switch-line { display: flex; align-items: center; gap: 10px; padding-top: 2px; }
.switch-hint { color: var(--text-3); font-size: 11.5px; }

.dlg-foot { display: flex; justify-content: space-between; align-items: center; width: 100%; }
.foot-l, .foot-r { display: flex; gap: 8px; }

.btn-primary, .btn-ghost, .btn-danger {
  padding: 7px 16px; border-radius: 5px;
  font: 500 12.5px var(--body); cursor: pointer;
  display: inline-flex; align-items: center; gap: 6px;
}
.btn-primary { background: var(--primary); color: #fff; border: 1px solid var(--primary); }
.btn-primary:hover:not(:disabled) { background: var(--primary-dark); }
.btn-primary:disabled { opacity: .5; cursor: not-allowed; }
.btn-ghost { background: #fff; border: 1px solid var(--border); color: var(--text); }
.btn-ghost:hover:not(:disabled) { border-color: var(--primary); color: var(--primary); }
.btn-ghost:disabled { opacity: .5; cursor: not-allowed; }
.btn-danger { background: #fff; border: 1px solid var(--danger); color: var(--danger); }
.btn-danger:hover { background: var(--danger-bg); }
.btn-danger .el-icon { font-size: 13px; }
</style>
