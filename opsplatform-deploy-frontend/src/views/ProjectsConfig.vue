<template>
  <div class="pc">
    <div class="tree-panel">
      <div class="tree-head">
        <span class="title">项目 · <b>{{ projectCount }}</b> <span class="sub">/ 环境 · <b>{{ envs.length }}</b></span></span>
        <button class="add-btn" @click="openCreate">
          <el-icon><Plus /></el-icon>新增
        </button>
      </div>

      <div class="tree-search">
        <el-icon class="search-ico"><Search /></el-icon>
        <input v-model="q" placeholder="搜索项目或环境..." />
      </div>

      <div class="tree-body">
        <div v-for="proj in filteredGroups" :key="proj.name" class="proj-group">
          <div class="proj-row" @click="toggle(proj.name)">
            <el-icon class="chev" :class="{opened: opened[proj.name]}"><ArrowRight /></el-icon>
            <el-icon class="ico"><Folder /></el-icon>
            <span class="proj-name">{{ proj.name }}</span>
            <span class="count">{{ proj.envs.length }}</span>
            <button class="row-btn" title="在此项目下新增环境" @click.stop="openCreate(proj.name)">
              <el-icon><Plus /></el-icon>
            </button>
          </div>
          <div v-if="opened[proj.name]" class="env-list">
            <div v-for="e in proj.envs" :key="e.id" class="env-row" @click="openEdit(e)">
              <span :class="'env-badge ' + e.env_type">{{ e.env_type }}</span>
              <span class="env-branch mono">{{ e.git_branch }}</span>
              <span class="env-sync">
                <span class="dot" :class="e.auto_sync ? 'ok' : 'off'"></span>
                <span class="sync-text">auto-sync {{ e.auto_sync ? 'on' : 'off' }}</span>
              </span>
              <span class="env-repo mono" :title="e.git_repo">{{ repoShort(e.git_repo) }}</span>
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
      :close-on-press-escape="true"
      custom-class="env-dialog">

      <div class="dlg-body">
        <div class="sec-lbl">基本信息</div>
        <div class="row2">
          <div class="field">
            <label>项目名称 <span class="req">*</span></label>
            <input v-if="isEdit"
              :value="form.project_name"
              class="inp mono"
              disabled />
            <div v-else class="combo">
              <input
                v-model="form.project_name"
                class="inp mono"
                placeholder="如: g33"
                list="proj-options" />
              <datalist id="proj-options">
                <option v-for="p in projects" :key="p" :value="p" />
              </datalist>
            </div>
          </div>
          <div class="field">
            <label>环境类型 <span class="req">*</span></label>
            <div class="rb-grp">
              <button :class="['rb', 'uat', {active: form.env_type === 'uat'}]"
                :disabled="isEdit"
                @click="setEnvType('uat')">UAT</button>
              <button :class="['rb', 'prod', {active: form.env_type === 'prod'}]"
                :disabled="isEdit"
                @click="setEnvType('prod')">PROD</button>
            </div>
          </div>
        </div>
        <div class="field">
          <label>显示名 <span class="hint">可选</span></label>
          <input v-model="form.display_name" class="inp" />
        </div>
        <div class="field full preview-name" v-if="!isEdit && form.project_name && form.env_type">
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
import { Plus, Search, Folder, ArrowRight, EditPen, Delete } from '@element-plus/icons-vue'
import {
  listProjectEnvs, createProjectEnv, updateProjectEnv, deleteProjectEnv,
  testProjectEnvGit, testProjectEnvArgocd
} from '../api'

const envs = ref([])
const q = ref('')
const opened = reactive({})
const dialogVis = ref(false)
const isEdit = ref(false)
const editingID = ref(null)
const saving = ref(false)
const testing = reactive({ git: false, argo: false })

const blank = () => ({
  project_name: '',       // 仅在前端使用，提交时拼成 name
  display_name: '',
  env_type: 'uat',
  git_repo: '',
  git_branch: 'main',
  chart_base_path: '',
  namespace: '',
  argocd_url: '',
  argocd_token: '',
  lark_webhook: '',
  auto_sync: 1
})
const form = reactive(blank())

function projectOf(e) {
  const suffix = '-' + e.env_type
  return e.name.endsWith(suffix) ? e.name.slice(0, -suffix.length) : e.name
}
function repoShort(url) {
  if (!url) return '—'
  const last = url.split('/').pop() || ''
  return last.replace(/\.git$/, '')
}

const projects = computed(() => {
  const s = new Set()
  envs.value.forEach(e => s.add(projectOf(e)))
  return [...s].sort()
})
const projectCount = computed(() => projects.value.length)

const groups = computed(() => {
  const map = {}
  envs.value.forEach(e => {
    const p = projectOf(e)
    if (!map[p]) map[p] = []
    map[p].push(e)
  })
  return Object.entries(map)
    .map(([name, envList]) => ({
      name,
      envs: envList.sort((a, b) => (a.env_type === 'uat' ? -1 : 1))
    }))
    .sort((a, b) => a.name.localeCompare(b.name))
})

const filteredGroups = computed(() => {
  const qq = q.value.trim().toLowerCase()
  if (!qq) return groups.value
  return groups.value
    .map(g => ({
      name: g.name,
      envs: g.name.toLowerCase().includes(qq)
        ? g.envs
        : g.envs.filter(e => e.name.toLowerCase().includes(qq) || e.env_type.includes(qq))
    }))
    .filter(g => g.envs.length > 0)
})

function toggle(pn) { opened[pn] = !opened[pn] }

async function load() {
  envs.value = (await listProjectEnvs()) || []
  groups.value.forEach(g => { if (opened[g.name] === undefined) opened[g.name] = true })
}

// 新增：prefix 可空，或传入项目名预填
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
    argocd_token: '', // 不显示原值，留空
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
  try {
    await testProjectEnvGit(editingID.value)
    ElMessage.success('Git 连通 OK')
  } finally { testing.git = false }
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
.pc {
  max-width: 820px;
  margin: 0 auto;
}

/* ===== 树面板 ===== */
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
}
.tree-head .title { font-weight: 600; font-size: 13.5px; color: var(--text); }
.tree-head .title b { color: var(--primary); font-family: var(--mono); }
.tree-head .sub { color: var(--text-3); font-weight: 400; margin-left: 2px; }
.tree-head .sub b { color: var(--text-2); font-family: var(--mono); }

.add-btn {
  display: flex; align-items: center; gap: 4px;
  background: var(--primary); color: #fff; border: none;
  padding: 6px 14px; border-radius: 5px;
  font: 500 12.5px var(--body); cursor: pointer;
}
.add-btn:hover { background: var(--primary-dark); }
.add-btn .el-icon { font-size: 14px; }

.tree-search { padding: 10px 14px; border-bottom: 1px solid var(--border-soft); position: relative; }
.tree-search .search-ico { position: absolute; left: 24px; top: 50%; transform: translateY(-50%); color: var(--text-3); font-size: 14px; }
.tree-search input {
  width: 100%; padding: 7px 10px 7px 32px;
  background: var(--bg-input); border: 1px solid var(--border); border-radius: 5px;
  font: 500 12.5px var(--body); color: var(--text);
}
.tree-search input:focus { outline: none; border-color: var(--primary); }

.tree-body { padding: 6px 0; }

.proj-group {}
.proj-row {
  display: flex; align-items: center; gap: 8px;
  padding: 9px 14px;
  cursor: pointer;
  font-size: 13px;
  color: var(--text);
  transition: background .1s;
}
.proj-row:hover { background: var(--bg-hover); }
.proj-row .chev { font-size: 10px; color: var(--text-3); transition: transform .15s; }
.proj-row .chev.opened { transform: rotate(90deg); }
.proj-row .ico { color: var(--text-3); font-size: 15px; }
.proj-row .proj-name { flex: 1; font-family: var(--mono); font-weight: 600; font-size: 14px; }
.proj-row .count {
  font: 600 11px var(--mono);
  color: var(--text-3);
  background: var(--bg-hover);
  padding: 1px 8px;
  border-radius: 10px;
}

.row-btn {
  background: transparent;
  border: none;
  color: var(--text-3);
  padding: 4px 6px;
  border-radius: 4px;
  cursor: pointer;
  display: flex; align-items: center; justify-content: center;
  transition: all .12s;
}
.row-btn:hover { background: var(--primary-bg); color: var(--primary); }
.row-btn .el-icon { font-size: 14px; }
.row-btn.edit { color: var(--text-3); }

.env-list { padding: 2px 0 6px; }
.env-row {
  display: grid;
  grid-template-columns: 60px 70px 130px 1fr 32px;
  align-items: center;
  gap: 14px;
  padding: 9px 14px 9px 44px;
  cursor: pointer;
  border-left: 2px solid transparent;
  transition: all .1s;
}
.env-row:hover {
  background: var(--bg-hover);
  border-left-color: var(--border);
}

.env-badge {
  font-family: var(--mono);
  font-size: 10.5px;
  font-weight: 600;
  padding: 3px 8px;
  border-radius: 3px;
  letter-spacing: .5px;
  text-transform: uppercase;
  text-align: center;
}
.env-badge.uat { background: var(--success-bg); color: var(--success-dark); }
.env-badge.prod { background: var(--danger-bg); color: var(--danger-dark); }

.env-branch { font-size: 12px; color: var(--text-2); }

.env-sync { display: flex; align-items: center; gap: 6px; }
.dot { width: 6px; height: 6px; border-radius: 50%; }
.dot.ok { background: var(--success); }
.dot.off { background: var(--text-3); }
.sync-text { font: 500 11.5px var(--mono); color: var(--text-3); }

.env-repo {
  font-size: 12px;
  color: var(--text-3);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.empty-tree { padding: 60px 20px; text-align: center; }
.et-t { color: var(--text-2); font-size: 14px; font-weight: 500; margin-bottom: 4px; }
.et-d { color: var(--text-3); font-size: 12.5px; }

/* ===== 弹窗 ===== */
:deep(.env-dialog) { border-radius: 8px; }
:deep(.env-dialog .el-dialog__header) {
  padding: 16px 22px; margin: 0;
  border-bottom: 1px solid var(--border-soft);
  background: var(--bg-card);
  border-radius: 8px 8px 0 0;
}
:deep(.env-dialog .el-dialog__title) { font-weight: 600; font-size: 15px; color: var(--text); }
:deep(.env-dialog .el-dialog__body) { padding: 0; }
:deep(.env-dialog .el-dialog__footer) {
  padding: 14px 22px;
  border-top: 1px solid var(--border-soft);
  background: var(--bg-input);
  border-radius: 0 0 8px 8px;
}

.dlg-body { padding: 18px 22px 22px; max-height: 60vh; overflow-y: auto; }
.sec-lbl {
  font-size: 10.5px; color: var(--text-3);
  text-transform: uppercase; letter-spacing: .8px; font-weight: 600;
  margin: 18px 0 10px;
  padding-bottom: 6px;
  border-bottom: 1px solid var(--border-soft);
}
.sec-lbl:first-child { margin-top: 0; }

.row2 { display: grid; grid-template-columns: 1fr 1fr; gap: 14px; }
.field { display: flex; flex-direction: column; gap: 5px; margin-bottom: 12px; }
.field label {
  font-size: 11.5px; color: var(--text-2); font-weight: 500;
}
.field label .req { color: var(--danger); margin-left: 2px; }
.field label .hint { color: var(--text-3); font-weight: 400; font-size: 11px; margin-left: 6px; }

.inp {
  padding: 8px 10px;
  border: 1px solid var(--border);
  border-radius: 5px;
  font: 500 13px var(--body);
  color: var(--text);
  background: #fff;
  transition: all .15s;
  width: 100%;
}
.inp:focus { outline: none; border-color: var(--primary); box-shadow: 0 0 0 3px rgba(59,130,246,.12); }
.inp.mono { font-family: var(--mono); font-size: 12.5px; }
.inp:disabled { background: var(--bg-hover); color: var(--text-3); cursor: not-allowed; }

.combo { position: relative; }

.rb-grp { display: flex; gap: 6px; }
.rb {
  flex: 1;
  background: #fff; border: 1px solid var(--border); color: var(--text-2);
  padding: 7px 14px; border-radius: 5px;
  font: 600 12px var(--mono);
  text-transform: uppercase; letter-spacing: .6px;
  cursor: pointer; transition: all .12s;
}
.rb:hover:not(:disabled) { border-color: var(--text-2); }
.rb:disabled { opacity: .7; cursor: not-allowed; }
.rb.uat.active { background: var(--success); border-color: var(--success); color: #fff; }
.rb.prod.active { background: var(--danger); border-color: var(--danger); color: #fff; }

.preview-name {
  padding: 8px 12px;
  background: var(--primary-bg);
  border-radius: 4px;
  font-size: 12px;
  color: var(--text-2);
  margin-bottom: 0;
}
.preview-name code {
  background: #fff;
  padding: 2px 8px;
  border-radius: 3px;
  color: var(--primary);
  font-weight: 600;
  margin-left: 4px;
}

.switch-line { display: flex; align-items: center; gap: 10px; padding-top: 2px; }
.switch-hint { color: var(--text-3); font-size: 11.5px; }

.dlg-foot {
  display: flex;
  justify-content: space-between;
  align-items: center;
  width: 100%;
}
.foot-l { display: flex; gap: 8px; }
.foot-r { display: flex; gap: 8px; }

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
.btn-danger {
  background: #fff; border: 1px solid var(--danger); color: var(--danger);
}
.btn-danger:hover { background: var(--danger-bg); }
.btn-danger .el-icon { font-size: 13px; }
</style>
