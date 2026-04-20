<template>
  <div class="dc">
    <!-- 刚完成发布后的快捷回滚条 -->
    <div v-if="recent" class="recent">
      <span class="recent-dot"></span>
      <span>刚完成 <b>#{{ recent.id }}</b>  ·  {{ recent.time }}</span>
      <span style="flex:1"></span>
      <button class="lnk warn" @click="openRollback(recent.id)">立即回滚</button>
      <button class="lnk mute" @click="recent = null">关闭</button>
    </div>

    <!-- 面包屑 + 模块数 -->
    <header class="dc-head">
      <div class="bc">
        <span v-if="currentEnv" class="bc-proj">{{ projectOf(currentEnv) }}</span>
        <span v-if="currentEnv" class="bc-sep">/</span>
        <span v-if="currentEnv" class="bc-env" :class="currentEnv.env_type">{{ currentEnv.env_type }}</span>
        <span class="bc-sep">/</span>
        <span class="bc-cur">{{ tab === 'update' ? '更新镜像' : '重启服务' }}</span>
      </div>
      <div class="head-right" v-if="currentEnv">
        <span class="kv"><span class="k">模块</span><span class="v mono">{{ modules.length }}</span></span>
        <span class="kv"><span class="k">auto-sync</span><span class="v mono" :class="currentEnv.auto_sync ? 'ok' : 'off'">{{ currentEnv.auto_sync ? 'ON' : 'OFF' }}</span></span>
        <button class="icon-btn" @click="onScan" :disabled="scanning" title="扫描 Git 重建模块列表">
          <svg :class="{spin: scanning}" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" width="14" height="14"><path d="M3 12a9 9 0 0 1 9-9 9.75 9.75 0 0 1 6.74 2.74L21 8"/><path d="M21 3v5h-5"/><path d="M21 12a9 9 0 0 1-9 9 9.75 9.75 0 0 1-6.74-2.74L3 16"/><path d="M3 21v-5h5"/></svg>
        </button>
      </div>
    </header>

    <!-- 项目 -->
    <div class="row">
      <span class="row-label">项目</span>
      <div class="chips">
        <button v-for="p in projects" :key="p"
          :class="['chip', {active: selectedProject === p}]"
          @click="pickProject(p)">
          {{ p }}
        </button>
        <span v-if="!projects.length" class="empty-hint">还没有项目，去「项目配置」添加</span>
      </div>
    </div>

    <!-- 环境 -->
    <div class="row">
      <span class="row-label">环境</span>
      <div class="chips">
        <button v-for="e in envsOfProject" :key="e.id"
          :class="['chip', 'env-chip', e.env_type, {active: selectedID === e.id}]"
          @click="pickEnv(e)">
          {{ e.env_type }}
        </button>
        <span v-if="selectedProject && !envsOfProject.length" class="empty-hint">该项目还没有环境</span>
      </div>
    </div>

    <!-- 动作 -->
    <div class="row">
      <span class="row-label">动作</span>
      <div class="seg">
        <button :class="{active: tab === 'update'}" @click="tab = 'update'">
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" width="14" height="14"><path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4"/><polyline points="17 8 12 3 7 8"/><line x1="12" x2="12" y1="3" y2="15"/></svg>
          更新镜像
        </button>
        <button :class="{active: tab === 'restart'}" @click="tab = 'restart'">
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" width="14" height="14"><path d="M3 12a9 9 0 1 0 9-9 9.75 9.75 0 0 0-6.74 2.74L3 8"/><path d="M3 3v5h5"/></svg>
          重启服务
        </button>
      </div>
    </div>

    <!-- 工作区 -->
    <section class="ws" v-if="currentEnv">
      <BatchUpdatePanel v-if="tab === 'update'" :project-env="currentEnv" :modules="modules" @done="handleDeployDone" />
      <RestartPanel v-else :project-env="currentEnv" :modules="modules" @done="handleRestartDone" />
    </section>
    <section v-else class="empty-state">
      <div class="empty-title">请选择一个项目和环境</div>
      <div class="empty-sub">没有项目？去「项目配置」添加。</div>
    </section>

    <RollbackDialog v-model="rbVis" :deployment-id="rbID" @done="onRollbackDone" />
  </div>
</template>

<script setup>
import { ref, computed, onMounted } from 'vue'
import { ElMessage } from 'element-plus'
import dayjs from 'dayjs'
import { listProjectEnvs, listModules, scanModules } from '../api'
import BatchUpdatePanel from '../components/BatchUpdatePanel.vue'
import RestartPanel from '../components/RestartPanel.vue'
import RollbackDialog from '../components/RollbackDialog.vue'

const envs = ref([])
const selectedID = ref(null)
const selectedProject = ref(null)
const modules = ref([])
const scanning = ref(false)
const tab = ref('update')
const currentEnv = computed(() => envs.value.find(e => e.id === selectedID.value))
const recent = ref(null)
const rbVis = ref(false)
const rbID = ref(null)

// project 名 = project_env.name 去掉 "-<env_type>" 后缀
function projectOf(e) {
  if (!e) return ''
  const suffix = '-' + e.env_type
  return e.name.endsWith(suffix) ? e.name.slice(0, -suffix.length) : e.name
}
const projects = computed(() => {
  const set = new Set()
  envs.value.forEach(e => set.add(projectOf(e)))
  return [...set].sort()
})
const envsOfProject = computed(() => {
  if (!selectedProject.value) return []
  return envs.value
    .filter(e => projectOf(e) === selectedProject.value)
    .sort((a, b) => (a.env_type === 'uat' ? -1 : 1))
})

function pickProject(p) {
  selectedProject.value = p
  // 自动选第一个环境（通常是 uat）
  const envs = envsOfProject.value
  if (envs.length) pickEnv(envs[0])
  else { selectedID.value = null; modules.value = [] }
}
function pickEnv(e) {
  selectedID.value = e.id
  loadModules()
}

async function loadEnvs() {
  envs.value = (await listProjectEnvs()) || []
  if (envs.value.length && !selectedProject.value) {
    pickProject(projects.value[0])
  }
}
async function loadModules() {
  if (!selectedID.value) return
  modules.value = (await listModules(selectedID.value)) || []
}
async function onScan() {
  if (!selectedID.value) return
  scanning.value = true
  try {
    const r = await scanModules(selectedID.value)
    ElMessage.success(`扫描到 ${r.count} 个模块`)
    await loadModules()
  } finally { scanning.value = false }
}

function handleDeployDone(depID) {
  recent.value = { id: depID, time: dayjs().format('HH:mm') }
  setTimeout(() => { if (recent.value?.id === depID) recent.value = null }, 5 * 60 * 1000)
  setTimeout(loadModules, 2000)
}
function handleRestartDone(depID) {
  recent.value = { id: depID, time: dayjs().format('HH:mm') }
  setTimeout(() => { if (recent.value?.id === depID) recent.value = null }, 5 * 60 * 1000)
}
function openRollback(id) { rbID.value = id; rbVis.value = true }
function onRollbackDone() { recent.value = null; loadModules() }

onMounted(loadEnvs)
</script>

<style scoped>
.dc { max-width: 1280px; margin: 0 auto; }

/* 顶部面包屑 */
.dc-head {
  display: flex; align-items: center; justify-content: space-between;
  padding: 4px 0 20px; margin-bottom: 4px;
}
.bc { display: flex; align-items: center; gap: 10px; font-size: 14px; }
.bc-proj {
  font-weight: 600; color: #0f172a; font-family: var(--mono);
  font-size: 18px; letter-spacing: -0.2px;
}
.bc-sep { color: #cbd5e1; font-size: 18px; font-weight: 300; }
.bc-env {
  font-family: var(--mono); font-weight: 600; font-size: 11px;
  padding: 2px 7px; border-radius: 3px; text-transform: uppercase; letter-spacing: 0.6px;
}
.bc-env.uat { background: #ecfdf5; color: #059669; }
.bc-env.prod { background: #fef2f2; color: #dc2626; }
.bc-cur { color: #64748b; font-size: 14px; }

.head-right { display: flex; align-items: center; gap: 20px; }
.kv { display: flex; align-items: center; gap: 6px; font-size: 12px; }
.kv .k { color: #94a3b8; }
.kv .v { color: #334155; font-weight: 600; font-size: 12.5px; }
.kv .v.ok { color: #059669; }
.kv .v.off { color: #94a3b8; }
.icon-btn {
  background: none; border: 1px solid #e5e7eb; border-radius: 6px;
  padding: 5px 9px; cursor: pointer; color: #64748b;
  display: flex; align-items: center; justify-content: center; transition: all .15s;
}
.icon-btn:hover { border-color: #1890ff; color: #1890ff; }
.icon-btn:disabled { opacity: .4; cursor: not-allowed; }
.icon-btn .spin { animation: spin 1s linear infinite; }
@keyframes spin { to { transform: rotate(360deg); } }

/* 三行选择器 */
.row {
  display: flex; align-items: flex-start; gap: 16px;
  padding: 10px 0; border-top: 1px solid #eef1f4;
}
.row-label {
  color: #94a3b8; font-size: 11.5px;
  width: 52px; padding-top: 6px; flex-shrink: 0;
  text-transform: uppercase; letter-spacing: .8px; font-weight: 600;
}
.chips { display: flex; flex-wrap: wrap; gap: 6px; flex: 1; }
.chip {
  background: #fff; border: 1px solid #e5e7eb; border-radius: 5px;
  padding: 5px 12px; font-size: 12.5px; font-family: var(--mono);
  color: #475569; cursor: pointer; transition: all .12s;
}
.chip:hover { border-color: #cbd5e1; background: #fafbfc; }
.chip.active {
  background: #0f172a; border-color: #0f172a; color: #fff;
}
.chip.env-chip { text-transform: uppercase; letter-spacing: .6px; font-weight: 600; font-size: 11px; padding: 5px 10px; }
.chip.env-chip.uat.active { background: #059669; border-color: #059669; }
.chip.env-chip.prod.active { background: #dc2626; border-color: #dc2626; }
.empty-hint { color: #94a3b8; font-size: 12px; padding-top: 6px; }

/* 动作 segmented */
.seg { display: inline-flex; background: #f1f5f9; border-radius: 6px; padding: 3px; gap: 2px; }
.seg button {
  background: transparent; border: none;
  padding: 5px 14px; font-size: 12.5px; color: #64748b;
  display: flex; align-items: center; gap: 6px;
  cursor: pointer; border-radius: 4px; transition: all .15s;
  font-family: var(--body); font-weight: 500;
}
.seg button:hover { color: #0f172a; }
.seg button.active { background: #fff; color: #0f172a; box-shadow: 0 1px 2px rgba(0,0,0,.05); font-weight: 600; }

/* 工作区 */
.ws { margin-top: 20px; }

.empty-state { padding: 80px 20px; text-align: center; }
.empty-title { color: #475569; font-size: 14px; font-weight: 500; }
.empty-sub { color: #94a3b8; font-size: 12px; margin-top: 4px; }

/* 快捷回滚条 */
.recent {
  display: flex; align-items: center; gap: 10px;
  padding: 10px 14px; margin-bottom: 14px;
  background: #fffbeb; border: 1px solid #fde68a; border-radius: 6px;
  font-size: 12.5px; color: #78350f;
}
.recent-dot {
  width: 6px; height: 6px; border-radius: 50%; background: #f59e0b;
  animation: pulse 2s infinite;
}
@keyframes pulse { 0%, 100% { opacity: 1; } 50% { opacity: .4; } }
.recent b { font-family: var(--mono); color: #92400e; }
.lnk {
  background: none; border: none; cursor: pointer; font-size: 12.5px;
  padding: 4px 8px; border-radius: 4px; font-family: var(--body);
}
.lnk.warn { color: #b45309; font-weight: 600; }
.lnk.warn:hover { background: #fef3c7; }
.lnk.mute { color: #a16207; }
.lnk.mute:hover { background: #fef3c7; }
</style>
