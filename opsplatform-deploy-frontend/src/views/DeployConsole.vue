<template>
  <div class="dc">
    <!-- 刚发布后的橙色快捷回滚条 -->
    <div v-if="recent" class="recent">
      <span class="dot"></span>
      <span>刚完成的发布 <b>#{{ recent.id }}</b> · {{ recent.time }}</span>
      <span style="flex:1"></span>
      <button class="ln-btn warn" @click="openRollback(recent.id)">立即回滚</button>
      <button class="ln-btn mute" @click="recent = null">关闭</button>
    </div>

    <!-- ===== Header card ===== -->
    <div class="hdr-card" v-if="currentEnv">
      <div class="hdr-l">
        <h1>
          <span class="proj">{{ projectOf(currentEnv) }}</span>
          <span class="slash">/</span>
          <span>{{ currentEnv.env_type.toUpperCase() }}</span>
          <span :class="'env-chip ' + currentEnv.env_type">● {{ currentEnv.env_type.toUpperCase() }}</span>
        </h1>
        <p>
          <b>{{ modules.length }}</b> modules ·
          auto-sync <b :style="{color: currentEnv.auto_sync ? 'var(--success)' : 'var(--text-3)'}">{{ currentEnv.auto_sync ? 'ON' : 'OFF' }}</b>
        </p>
      </div>
      <div class="hdr-r">
        <div class="stat"><div class="k">Modules</div><div class="v">{{ modules.length }}</div></div>
      </div>
    </div>
    <div v-else class="hdr-card empty">
      <div>请选择一个项目和环境</div>
    </div>

    <!-- ===== Target panel ===== -->
    <div class="panel">
      <div class="p-hd">
        <h3>
          <el-icon><Aim /></el-icon>
          目标选择
        </h3>
        <span class="sub">project · env</span>
      </div>
      <div class="p-bd">
        <div class="sel-row">
          <span class="sel-k">Project</span>
          <div class="chips">
            <button v-for="p in projects" :key="p"
              :class="['chip', {active: selectedProject === p}]"
              @click="pickProject(p)">{{ p }}</button>
            <span v-if="!projects.length" class="empty-hint">还没有项目，去「项目配置」添加</span>
          </div>
        </div>
        <div class="sel-row">
          <span class="sel-k">Env</span>
          <div class="chips">
            <button v-for="e in envsOfProject" :key="e.id"
              :class="['chip', 'env', e.env_type, {active: selectedID === e.id}]"
              @click="pickEnv(e)">{{ e.env_type.toUpperCase() }}</button>
            <span v-if="selectedProject && !envsOfProject.length" class="empty-hint">该项目没有环境</span>
          </div>
        </div>
      </div>
    </div>

    <!-- ===== Big action tabs ===== -->
    <div class="action-tabs" v-if="currentEnv">
      <button :class="['act-tab', {active: tab === 'update'}]" @click="tab = 'update'">
        <span class="act-icon"><el-icon><Upload /></el-icon></span>
        <span class="act-body">
          <span class="act-title">批量更新镜像</span>
          <span class="act-desc">写 values.yaml → git push → ArgoCD 同步</span>
        </span>
        <span class="act-badge">update-image</span>
      </button>
      <button :class="['act-tab', {active: tab === 'restart'}]" @click="tab = 'restart'">
        <span class="act-icon"><el-icon><RefreshRight /></el-icon></span>
        <span class="act-body">
          <span class="act-title">批量重启服务</span>
          <span class="act-desc">调用 ArgoCD Restart · 不动 git · 保持当前 tag</span>
        </span>
        <span class="act-badge">restart</span>
      </button>
    </div>

    <!-- ===== Workspace ===== -->
    <div class="panel" v-if="currentEnv">
      <BatchUpdatePanel v-if="tab === 'update'"
        :project-env="currentEnv" :modules="modules" @done="handleDeployDone" />
      <RestartPanel v-else
        :project-env="currentEnv" :modules="modules" @done="handleRestartDone" />
    </div>

    <RollbackDialog v-model="rbVis" :deployment-id="rbID" @done="onRollbackDone" />
  </div>
</template>

<script setup>
import { ref, computed, onMounted } from 'vue'
import { ElMessage } from 'element-plus'
import { Aim, Upload, RefreshRight } from '@element-plus/icons-vue'
import dayjs from 'dayjs'
import { listProjectEnvs, listModules, scanModules } from '../api'
import BatchUpdatePanel from '../components/BatchUpdatePanel.vue'
import RestartPanel from '../components/RestartPanel.vue'
import RollbackDialog from '../components/RollbackDialog.vue'

const envs = ref([])
const selectedID = ref(null)
const selectedProject = ref(null)
const modules = ref([])
const tab = ref('update')
const currentEnv = computed(() => envs.value.find(e => e.id === selectedID.value))
const recent = ref(null)
const rbVis = ref(false)
const rbID = ref(null)

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
  if (envs.value.length && !selectedProject.value) pickProject(projects.value[0])
}
async function loadModules() {
  if (!selectedID.value) return
  modules.value = (await listModules(selectedID.value)) || []
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
/* ===== Recent rollback bar ===== */
.recent {
  display: flex; align-items: center; gap: 10px;
  background: #fffbeb; border: 1px solid #fde68a; border-radius: var(--radius);
  padding: 10px 14px; margin-bottom: 14px;
  font-size: 12.5px; color: #78350f;
}
.dot { width: 6px; height: 6px; border-radius: 50%; background: #f59e0b; animation: pulse 2s infinite; }
@keyframes pulse { 0%, 100% { opacity: 1; } 50% { opacity: .4; } }
.recent b { font-family: var(--mono); color: #92400e; }
.ln-btn { background: none; border: none; cursor: pointer; font-size: 12.5px; padding: 4px 8px; border-radius: 4px; font-family: var(--body); }
.ln-btn.warn { color: #b45309; font-weight: 600; }
.ln-btn.warn:hover { background: #fef3c7; }
.ln-btn.mute { color: #a16207; }

/* ===== Header card ===== */
.hdr-card {
  background: var(--bg-card); border: 1px solid var(--border);
  border-radius: var(--radius); padding: 20px 24px; margin-bottom: 16px;
  display: flex; justify-content: space-between; align-items: center;
}
.hdr-card.empty { color: var(--text-3); justify-content: center; padding: 40px; }
.hdr-l h1 {
  font: 600 20px/1.2 var(--body); color: var(--text); letter-spacing: -.2px;
  margin-bottom: 4px;
  display: flex; align-items: center; gap: 12px;
}
.hdr-l .proj { color: var(--primary); font-family: var(--mono); font-weight: 700; }
.hdr-l .slash { color: var(--text-3); font-weight: 400; }
.hdr-l p { color: var(--text-2); font-size: 13px; }
.hdr-l p b { color: var(--text); font-family: var(--mono); font-weight: 500; }
.hdr-r { display: flex; gap: 24px; }
.stat { text-align: center; }
.stat .k { font-size: 10.5px; color: var(--text-3); text-transform: uppercase; letter-spacing: .6px; font-weight: 600; margin-bottom: 2px; }
.stat .v { font: 600 20px var(--mono); color: var(--text); }

/* ===== Panel ===== */
.panel { background: var(--bg-card); border: 1px solid var(--border); border-radius: var(--radius); margin-bottom: 16px; overflow: hidden; }
.p-hd { padding: 14px 20px; border-bottom: 1px solid var(--border-soft); display: flex; justify-content: space-between; align-items: center; }
.p-hd h3 { font: 600 14px/1 var(--body); color: var(--text); display: flex; align-items: center; gap: 8px; }
.p-hd h3 .el-icon { color: var(--primary); font-size: 16px; }
.p-hd .sub { font: 500 11.5px var(--mono); color: var(--text-3); }
.p-bd { padding: 6px 20px 16px; }

/* ===== Selector rows ===== */
.sel-row { display: grid; grid-template-columns: 84px 1fr; gap: 14px; padding: 10px 0; align-items: center; border-top: 1px solid var(--border-soft); }
.sel-row:first-child { border-top: none; padding-top: 8px; }
.sel-k { font-size: 11px; color: var(--text-3); text-transform: uppercase; letter-spacing: .8px; font-weight: 600; }

.chips { display: flex; flex-wrap: wrap; gap: 5px; }
.chip {
  background: #fff; border: 1px solid var(--border); color: var(--text-2);
  padding: 5px 12px; border-radius: 5px;
  font: 500 12.5px var(--mono); cursor: pointer; transition: all .12s;
  font-family: var(--mono);
}
.chip:hover { border-color: var(--primary); color: var(--primary); }
.chip.active { background: var(--primary); color: #fff; border-color: var(--primary); font-weight: 600; }
.chip.env { text-transform: uppercase; letter-spacing: .4px; font-weight: 600; font-size: 11px; }
.chip.env.uat.active { background: var(--success); border-color: var(--success); }
.chip.env.prod.active { background: var(--danger); border-color: var(--danger); }
.empty-hint { color: var(--text-3); font-size: 12px; padding: 6px 0; }

/* ===== Big action tabs (A1) ===== */
.action-tabs { display: grid; grid-template-columns: 1fr 1fr; gap: 10px; margin-bottom: 16px; }
.act-tab {
  background: var(--bg-card); border: 1.5px solid var(--border); border-radius: var(--radius);
  padding: 14px 18px; cursor: pointer; text-align: left;
  display: flex; align-items: center; gap: 12px;
  transition: all .15s; font-family: var(--body);
}
.act-tab:hover { border-color: var(--primary); background: #fbfdff; }
.act-tab.active {
  background: var(--primary); border-color: var(--primary); color: #fff;
  box-shadow: 0 4px 12px rgba(24, 144, 255, .22);
}
.act-icon {
  width: 40px; height: 40px; border-radius: 8px; flex-shrink: 0;
  background: var(--primary-bg); color: var(--primary);
  display: flex; align-items: center; justify-content: center;
  transition: all .15s;
}
.act-icon .el-icon { font-size: 20px; }
.act-tab.active .act-icon { background: rgba(255, 255, 255, .2); color: #fff; }
.act-body { flex: 1; display: flex; flex-direction: column; gap: 2px; min-width: 0; }
.act-title { font: 600 14.5px var(--body); color: var(--text); }
.act-tab.active .act-title { color: #fff; }
.act-desc { font-size: 12px; color: var(--text-3); }
.act-tab.active .act-desc { color: rgba(255, 255, 255, .85); }
.act-badge {
  font: 500 11px var(--mono); padding: 3px 8px; border-radius: 99px;
  background: var(--bg-hover); color: var(--text-2);
}
.act-tab.active .act-badge { background: rgba(255, 255, 255, .18); color: #fff; }
</style>
