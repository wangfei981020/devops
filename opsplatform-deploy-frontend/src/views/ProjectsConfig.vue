<template>
  <div class="pc">
    <!-- 左侧：树形项目 → 环境 -->
    <aside class="tree-panel">
      <div class="tree-head">
        <span class="title">项目 · <b>{{ envs.length }}</b></span>
        <button class="add-btn" @click="onAdd">
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
          </div>
          <div v-if="opened[proj.name]" class="env-list">
            <div
              v-for="e in proj.envs"
              :key="e.id"
              :class="['env-row', {active: selected?.id === e.id}]"
              @click="onPick(e)"
            >
              <span :class="'env-badge ' + e.env_type">{{ e.env_type }}</span>
              <span class="env-meta">
                <span v-if="e.auto_sync" class="dot ok" title="auto-sync ON"></span>
                <span v-else class="dot off" title="auto-sync OFF"></span>
                {{ e.git_branch }}
              </span>
            </div>
          </div>
        </div>
        <div v-if="!filteredGroups.length" class="empty">没有匹配的项目</div>
      </div>
    </aside>

    <!-- 右侧：详情 -->
    <section class="detail" v-if="selected">
      <header class="detail-hd">
        <div>
          <div class="hd-sub">PROJECT ENV</div>
          <h2 class="hd-name">
            <span class="mono">{{ form.name || '(新增)' }}</span>
            <span v-if="form.env_type" :class="'env-badge ' + form.env_type" style="margin-left:10px">{{ form.env_type }}</span>
          </h2>
        </div>
      </header>

      <div class="form">
        <div class="sec">
          <div class="sec-title">基本信息</div>
          <div class="sec-body">
            <div class="field">
              <label>唯一名称 <span class="req">*</span></label>
              <input v-model="form.name" :disabled="!!selected.id" class="input mono" placeholder="e.g. g32-uat" />
            </div>
            <div class="field">
              <label>显示名</label>
              <input v-model="form.display_name" class="input" />
            </div>
            <div class="field">
              <label>环境类型</label>
              <div class="radio-group">
                <button :class="['rb', 'uat', {active: form.env_type === 'uat'}]" @click="setEnvType('uat')">UAT</button>
                <button :class="['rb', 'prod', {active: form.env_type === 'prod'}]" @click="setEnvType('prod')">PROD</button>
              </div>
            </div>
          </div>
        </div>

        <div class="sec">
          <div class="sec-title">GitLab 仓库</div>
          <div class="sec-body">
            <div class="field full">
              <label>Git Repo URL <span class="req">*</span></label>
              <input v-model="form.git_repo" class="input mono" placeholder="http://gitlab.xx/ops/..." />
            </div>
            <div class="field">
              <label>分支</label>
              <input v-model="form.git_branch" class="input mono" />
            </div>
            <div class="field">
              <label>Chart 基础路径</label>
              <input v-model="form.chart_base_path" class="input mono" placeholder="argocd-apps/charts/g32-uat" />
            </div>
          </div>
        </div>

        <div class="sec">
          <div class="sec-title">Kubernetes · ArgoCD</div>
          <div class="sec-body">
            <div class="field">
              <label>K8s Namespace</label>
              <input v-model="form.namespace" class="input mono" />
            </div>
            <div class="field">
              <label>ArgoCD URL</label>
              <input v-model="form.argocd_url" class="input mono" />
            </div>
            <div class="field full">
              <label>ArgoCD Token <span class="hint">留空则不更新</span></label>
              <input v-model="form.argocd_token" type="password" class="input" placeholder="新建时必填；修改时留空则不变" />
            </div>
            <div class="field full">
              <label>Auto Sync</label>
              <div class="switch-row">
                <el-switch v-model="form.auto_sync" :active-value="1" :inactive-value="0" :disabled="form.env_type === 'prod'" />
                <span class="switch-hint">PROD 强制关闭</span>
              </div>
            </div>
          </div>
        </div>

        <div class="sec">
          <div class="sec-title">通知</div>
          <div class="sec-body">
            <div class="field full">
              <label>Lark Webhook <span class="hint">可空，留空用系统默认</span></label>
              <input v-model="form.lark_webhook" class="input mono" />
            </div>
          </div>
        </div>
      </div>

      <footer class="detail-ft">
        <div class="ft-left">
          <button class="btn" @click="onTestGit" :disabled="testing.git">测试 Git 连通</button>
          <button class="btn" @click="onTestArgo" :disabled="testing.argo">测试 ArgoCD 连通</button>
          <button v-if="selected.id" class="btn danger" @click="onDelete">删除</button>
        </div>
        <button class="cta" @click="onSave" :disabled="saving">
          <span v-if="saving">保存中...</span>
          <span v-else>保存</span>
        </button>
      </footer>
    </section>

    <section v-else class="empty-state">
      <div class="es-t">选择一个环境查看 / 编辑</div>
      <div class="es-d">点左侧项目展开，选一个 UAT/PROD，或「+ 新增」创建新的</div>
    </section>
  </div>
</template>

<script setup>
import { ref, reactive, computed, onMounted } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Plus, Search, Folder, ArrowRight } from '@element-plus/icons-vue'
import {
  listProjectEnvs, createProjectEnv, updateProjectEnv, deleteProjectEnv,
  testProjectEnvGit, testProjectEnvArgocd
} from '../api'

const envs = ref([])
const q = ref('')
const selected = ref(null)
const opened = reactive({})
const saving = ref(false)
const testing = reactive({ git: false, argo: false })

const blank = () => ({
  name: '', display_name: '', env_type: 'uat',
  git_repo: '', git_branch: 'main', chart_base_path: '',
  namespace: '', argocd_url: '', argocd_token: '', lark_webhook: '',
  auto_sync: 1
})
const form = reactive(blank())

function projectOf(e) {
  const suffix = '-' + e.env_type
  return e.name.endsWith(suffix) ? e.name.slice(0, -suffix.length) : e.name
}

const groups = computed(() => {
  const map = {}
  envs.value.forEach(e => {
    const p = projectOf(e)
    if (!map[p]) map[p] = []
    map[p].push(e)
  })
  return Object.entries(map).map(([name, envs]) => ({
    name,
    envs: envs.sort((a, b) => a.env_type === 'uat' ? -1 : 1)
  })).sort((a, b) => a.name.localeCompare(b.name))
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

function toggle(projName) {
  opened[projName] = !opened[projName]
}
function onPick(e) {
  selected.value = e
  Object.assign(form, e, { argocd_token: '' })
}
function onAdd() {
  selected.value = { id: null }
  Object.assign(form, blank())
}
function setEnvType(t) {
  form.env_type = t
  if (t === 'prod') form.auto_sync = 0
}

async function load() {
  envs.value = (await listProjectEnvs()) || []
  // 默认全部展开
  groups.value.forEach(g => { if (opened[g.name] === undefined) opened[g.name] = true })
}

async function onSave() {
  saving.value = true
  try {
    if (selected.value?.id) {
      await updateProjectEnv(selected.value.id, form)
    } else {
      await createProjectEnv(form)
    }
    ElMessage.success('保存成功')
    await load()
  } finally { saving.value = false }
}

async function onDelete() {
  try { await ElMessageBox.confirm(`删除 ${selected.value.name}？`, '确认') } catch (_) { return }
  await deleteProjectEnv(selected.value.id)
  ElMessage.success('已删除')
  selected.value = null
  await load()
}

async function onTestGit() {
  if (!selected.value?.id) { ElMessage.warning('先保存再测试'); return }
  testing.git = true
  try { await testProjectEnvGit(selected.value.id); ElMessage.success('Git 连通 OK') }
  finally { testing.git = false }
}

async function onTestArgo() {
  if (!selected.value?.id) { ElMessage.warning('先保存再测试'); return }
  testing.argo = true
  try {
    const r = await testProjectEnvArgocd(selected.value.id)
    ElMessage.success(`ArgoCD OK (${r.version})`)
  } finally { testing.argo = false }
}

onMounted(load)
</script>

<style scoped>
.pc {
  display: grid;
  grid-template-columns: 300px 1fr;
  gap: 14px;
  height: calc(100vh - 100px);
}

/* ===== 左侧树形面板 ===== */
.tree-panel {
  background: var(--bg-card);
  border: 1px solid var(--border);
  border-radius: var(--radius);
  display: flex;
  flex-direction: column;
  overflow: hidden;
}
.tree-head {
  padding: 14px 16px;
  border-bottom: 1px solid var(--border-soft);
  display: flex;
  align-items: center;
  justify-content: space-between;
}
.tree-head .title { font-weight: 600; font-size: 13.5px; color: var(--text); }
.tree-head .title b { color: var(--primary); font-family: var(--mono); margin-left: 2px; }
.add-btn {
  display: flex; align-items: center; gap: 4px;
  background: var(--primary); color: #fff; border: none;
  padding: 5px 12px; border-radius: 5px;
  font: 500 12.5px var(--body); cursor: pointer;
}
.add-btn:hover { background: var(--primary-dark); }
.add-btn .el-icon { font-size: 14px; }

.tree-search { padding: 10px 12px; border-bottom: 1px solid var(--border-soft); position: relative; }
.tree-search .search-ico {
  position: absolute; left: 22px; top: 50%; transform: translateY(-50%);
  color: var(--text-3); font-size: 14px;
}
.tree-search input {
  width: 100%; padding: 7px 10px 7px 32px;
  background: var(--bg-input);
  border: 1px solid var(--border);
  border-radius: 5px;
  font: 500 12.5px var(--body);
  color: var(--text);
}
.tree-search input:focus { outline: none; border-color: var(--primary); }

.tree-body { flex: 1; overflow-y: auto; padding: 6px 0; }

.proj-group { margin-bottom: 2px; }

.proj-row {
  display: flex; align-items: center; gap: 6px;
  padding: 7px 14px;
  cursor: pointer;
  font-size: 13px;
  color: var(--text);
  transition: background .1s;
}
.proj-row:hover { background: var(--bg-hover); }
.proj-row .chev {
  font-size: 10px; color: var(--text-3);
  transition: transform .15s;
}
.proj-row .chev.opened { transform: rotate(90deg); }
.proj-row .ico { color: var(--text-3); font-size: 14px; }
.proj-row .proj-name {
  flex: 1; font-family: var(--mono);
  font-weight: 600; font-size: 13px;
  color: var(--text);
}
.proj-row .count {
  font: 500 11px var(--mono);
  color: var(--text-3);
  background: var(--bg-hover);
  padding: 1px 7px;
  border-radius: 10px;
}

.env-list { padding: 2px 0 4px; }
.env-row {
  display: flex; align-items: center; gap: 8px;
  padding: 7px 14px 7px 40px;
  cursor: pointer;
  border-left: 2px solid transparent;
  transition: all .1s;
}
.env-row:hover { background: var(--bg-hover); }
.env-row.active {
  background: var(--primary-bg);
  border-left-color: var(--primary);
}
.env-row.active .env-badge { font-weight: 700; }

.env-badge {
  font-family: var(--mono);
  font-size: 10.5px;
  font-weight: 600;
  padding: 2px 7px;
  border-radius: 3px;
  letter-spacing: .5px;
  text-transform: uppercase;
  min-width: 38px;
  text-align: center;
}
.env-badge.uat { background: var(--success-bg); color: var(--success-dark); }
.env-badge.prod { background: var(--danger-bg); color: var(--danger-dark); }

.env-meta {
  font: 500 11px var(--mono);
  color: var(--text-3);
  display: flex; align-items: center; gap: 5px;
}
.dot { width: 5px; height: 5px; border-radius: 50%; }
.dot.ok { background: var(--success); }
.dot.off { background: var(--text-3); }

.empty { padding: 40px 20px; text-align: center; color: var(--text-3); font-size: 12.5px; }

/* ===== 右侧详情 ===== */
.detail {
  background: var(--bg-card);
  border: 1px solid var(--border);
  border-radius: var(--radius);
  display: flex;
  flex-direction: column;
  overflow: hidden;
}
.detail-hd {
  padding: 16px 22px;
  border-bottom: 1px solid var(--border-soft);
}
.hd-sub {
  font-size: 10.5px;
  color: var(--text-3);
  text-transform: uppercase;
  letter-spacing: 1px;
  font-weight: 600;
  margin-bottom: 2px;
}
.hd-name {
  font-size: 18px;
  font-weight: 700;
  color: var(--text);
  letter-spacing: -.2px;
  display: flex;
  align-items: center;
}

.form { flex: 1; overflow-y: auto; padding: 8px 0; }

.sec { padding: 14px 22px; border-bottom: 1px solid var(--border-soft); }
.sec:last-child { border-bottom: none; }
.sec-title {
  font-size: 10.5px;
  color: var(--text-3);
  text-transform: uppercase;
  letter-spacing: .8px;
  font-weight: 600;
  margin-bottom: 10px;
}
.sec-body {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 14px;
}
.field { display: flex; flex-direction: column; gap: 5px; }
.field.full { grid-column: span 2; }
.field label {
  font-size: 11.5px;
  color: var(--text-2);
  font-weight: 500;
}
.field label .req { color: var(--danger); margin-left: 2px; }
.field label .hint { color: var(--text-3); font-weight: 400; font-size: 10.5px; margin-left: 6px; }

.input {
  padding: 7px 10px;
  border: 1px solid var(--border);
  border-radius: 5px;
  font: 500 13px var(--body);
  color: var(--text);
  background: #fff;
  transition: all .15s;
}
.input:focus { outline: none; border-color: var(--primary); box-shadow: 0 0 0 3px rgba(59,130,246,.12); }
.input.mono { font-family: var(--mono); font-size: 12.5px; }
.input:disabled { background: var(--bg-hover); color: var(--text-3); cursor: not-allowed; }

.radio-group { display: flex; gap: 6px; }
.rb {
  background: #fff;
  border: 1px solid var(--border);
  color: var(--text-2);
  padding: 7px 18px;
  border-radius: 5px;
  font: 600 12px var(--mono);
  text-transform: uppercase;
  letter-spacing: .6px;
  cursor: pointer;
  transition: all .12s;
}
.rb:hover { border-color: var(--text-2); }
.rb.uat.active { background: var(--success); border-color: var(--success); color: #fff; }
.rb.prod.active { background: var(--danger); border-color: var(--danger); color: #fff; }

.switch-row { display: flex; align-items: center; gap: 10px; padding: 4px 0; }
.switch-hint { color: var(--text-3); font-size: 11.5px; }

.detail-ft {
  padding: 14px 22px;
  border-top: 1px solid var(--border-soft);
  background: var(--bg-input);
  display: flex;
  justify-content: space-between;
  align-items: center;
}
.ft-left { display: flex; gap: 8px; }
.btn {
  background: #fff;
  border: 1px solid var(--border);
  color: var(--text);
  padding: 7px 14px;
  border-radius: 5px;
  font: 500 12.5px var(--body);
  cursor: pointer;
  transition: all .12s;
}
.btn:hover:not(:disabled) { border-color: var(--primary); color: var(--primary); }
.btn:disabled { opacity: .5; cursor: not-allowed; }
.btn.danger { color: var(--danger); }
.btn.danger:hover { border-color: var(--danger); background: var(--danger-bg); }

.cta {
  background: var(--primary);
  color: #fff;
  border: none;
  padding: 8px 22px;
  border-radius: 5px;
  font: 600 13px var(--body);
  cursor: pointer;
}
.cta:hover:not(:disabled) { background: var(--primary-dark); }
.cta:disabled { opacity: .5; cursor: not-allowed; }

.empty-state {
  background: var(--bg-card);
  border: 1px solid var(--border);
  border-radius: var(--radius);
  padding: 80px 40px;
  text-align: center;
}
.es-t { color: var(--text-2); font-size: 14px; font-weight: 500; }
.es-d { color: var(--text-3); font-size: 12.5px; margin-top: 6px; }
</style>
