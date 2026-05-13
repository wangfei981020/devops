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
          <span :class="'env-chip ' + envTypeKey(currentEnv)">● {{ currentEnv.env_type.toUpperCase() }}</span>
          <span :class="'kind-chip ' + currentEnv._kind">{{ currentEnv._kind === 'vm' ? 'VM' : 'K8S' }}</span>
        </h1>
        <p v-if="currentEnv._kind === 'k8s'">
          <b>{{ modules.length }}</b> modules ·
          auto-sync <b :style="{color: currentEnv.auto_sync ? 'var(--success)' : 'var(--text-3)'}">{{ currentEnv.auto_sync ? 'ON' : 'OFF' }}</b>
        </p>
        <p v-else>
          VM 部署 · 项目代码 <b>{{ currentEnv.project_code || '—' }}</b> ·
          部署目录 <b>{{ currentEnv.ansible_root || '/etc/ansible' }}</b>
        </p>
      </div>
      <div class="hdr-r">
        <!-- K8s: modules count + 同步模块按钮 -->
        <template v-if="currentEnv._kind === 'k8s'">
          <div class="stat"><div class="k">Modules</div><div class="v">{{ modules.length }}</div></div>
          <button v-if="auth.isAdmin || auth.hasButton('scan_modules')" class="btn-rescan" :disabled="rescanning" @click="onRescan"
                  title="从 Git 仓库重新同步模块列表，写入 module 表">
            <el-icon :class="{ spin: rescanning }"><RefreshRight /></el-icon>
            <span>{{ rescanning ? '同步中...' : '同步模块' }}</span>
          </button>
        </template>
        <!-- VM: 服务同步按钮在 VmDeployPanel 内部，这里只显示 agent 信息 -->
        <template v-else>
          <div class="stat"><div class="k">Agent</div><div class="v">#{{ currentEnv.agent_id }}</div></div>
        </template>
      </div>
    </div>
    <div v-else class="hdr-card empty">
      <div>请选择一个项目和环境</div>
    </div>

    <!-- ===== Target + Action（同一行）===== -->
    <div class="panel">
      <div class="p-hd">
        <h3>
          <el-icon><Aim /></el-icon>
          目标选择
        </h3>
        <span class="sub">输入搜索 · 例如 g32u / g50p · ⭐ 最近使用置顶</span>
      </div>
      <div class="p-bd">
        <div class="sel-row">
          <span class="sel-k">目标</span>
          <el-select
            v-model="selectedID"
            filterable
            :filter-method="filterEnv"
            :placeholder="envs.length ? '搜索 project_env，例如 g32u / g50p' : '还没有项目，去「项目配置」添加'"
            :disabled="!envs.length"
            style="width:340px;"
            popper-class="env-select-popper"
            @visible-change="v => { if (!v) searchQuery = '' }">
            <el-option-group v-if="!searchQuery && filteredRecent.length" label="⭐ 最近使用">
              <el-option v-for="e in filteredRecent" :key="'r-'+e._selID" :value="e._selID" :label="e.name">
                <div class="opt-row">
                  <span :class="['opt-dot', envTypeKey(e)]"></span>
                  <span :class="['opt-name', { prod: envTypeKey(e) === 'prod' }]">{{ e.name }}</span>
                  <span :class="'kind-mini ' + e._kind">{{ e._kind === 'vm' ? 'VM' : 'K8S' }}</span>
                  <span :class="'env-chip-mini ' + envTypeKey(e)">{{ e.env_type.toUpperCase() }}</span>
                </div>
              </el-option>
            </el-option-group>
            <el-option-group v-for="grp in envGroups" :key="grp.project" :label="grp.project">
              <template v-for="e in grp.items" :key="e._selID">
                <el-option v-if="matchSearch(e)" :value="e._selID" :label="e.name">
                  <div class="opt-row">
                    <span :class="['opt-dot', envTypeKey(e)]"></span>
                    <span :class="['opt-name', { prod: envTypeKey(e) === 'prod' }]">{{ e.name }}</span>
                    <span :class="'kind-mini ' + e._kind">{{ e._kind === 'vm' ? 'VM' : 'K8S' }}</span>
                    <span :class="'env-chip-mini ' + envTypeKey(e)">{{ e.env_type.toUpperCase() }}</span>
                  </div>
                </el-option>
              </template>
            </el-option-group>
          </el-select>
          <span v-if="currentEnv" :class="'env-chip-inline ' + envTypeKey(currentEnv)">
            {{ currentEnv.env_type.toUpperCase() }}
          </span>

          <!-- 动作 seg control：K8s + VM 都有（K8s 是 update/restart，VM 是 rsync/update_version） -->
          <template v-if="currentEnv && currentEnv._kind === 'k8s'">
            <span class="sel-divider"></span>
            <span class="sel-k">动作</span>
            <div class="seg" role="tablist">
              <button :class="['seg-btn', { active: tab === 'update' }]"
                      @click="tab = 'update'">
                <el-icon><Upload /></el-icon>
                <span>批量更新镜像</span>
              </button>
              <button :class="['seg-btn', { active: tab === 'restart' }]"
                      @click="tab = 'restart'">
                <el-icon><RefreshRight /></el-icon>
                <span>批量重启服务</span>
              </button>
            </div>
          </template>
          <template v-if="currentEnv && currentEnv._kind === 'vm'">
            <span class="sel-divider"></span>
            <span class="sel-k">动作</span>
            <div class="seg" role="tablist">
              <!-- PROD 只显示「批量更新」（PROD 不允许 rsync，必须选历史版本） -->
              <!-- UAT/LPT 显示 3 个 tab，默认「批量 rsync + 更新」 -->
              <template v-if="currentEnv.env_type !== 'PROD'">
                <button :class="['seg-btn', { active: vmTab === 'rsync_and_update' }]"
                        @click="vmTab = 'rsync_and_update'">
                  <el-icon><Promotion /></el-icon>
                  <span>批量 rsync + 更新</span>
                </button>
                <button :class="['seg-btn', { active: vmTab === 'rsync' }]"
                        @click="vmTab = 'rsync'">
                  <el-icon><RefreshRight /></el-icon>
                  <span>批量 rsync</span>
                </button>
              </template>
              <button :class="['seg-btn', { active: vmTab === 'update_version' }]"
                      @click="vmTab = 'update_version'">
                <el-icon><Upload /></el-icon>
                <span>批量更新</span>
              </button>
            </div>
          </template>
        </div>
      </div>
    </div>

    <!-- ===== Rollback 模式横幅 ===== -->
    <div v-if="rollbackMode" class="rb-banner">
      <el-icon class="rb-icon"><RefreshLeft /></el-icon>
      <div class="rb-text">
        <div>⏮ <b>回滚模式</b> · 来源 #{{ rollbackMode.refDeploymentID }}</div>
        <div class="rb-tip">已自动填入需回滚的模块 + 目标版本，可编辑或删除行；提交将记为回滚操作</div>
      </div>
      <button class="rb-cancel" @click="cancelRollback">取消回滚</button>
    </div>

    <!-- ===== Workspace ===== -->
    <div class="panel" v-if="currentEnv">
      <!-- K8s: 批量更新镜像 / 批量重启服务 -->
      <template v-if="currentEnv._kind === 'k8s'">
        <BatchUpdatePanel v-if="tab === 'update'"
          :project-env="currentEnv" :modules="modules"
          :rollback-mode="rollbackMode"
          @done="handleDeployDone" @rollback-consumed="cancelRollback" />
        <RestartPanel v-else
          :project-env="currentEnv" :modules="modules" @done="handleRestartDone" />
      </template>
      <!-- VM: ansible playbook 部署，按 vmTab 决定 rsync 还是 update_version -->
      <VmDeployPanel v-else :vm-project-env="currentEnv" :tab="vmTab" />
    </div>

    <RollbackDialog v-model="rbVis" :deployment-id="rbID" @done="onRollbackDone" />
  </div>
</template>

<script setup>
import { ref, computed, onMounted, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ElMessage } from 'element-plus'
import { Aim, Upload, RefreshRight, RefreshLeft, Promotion } from '@element-plus/icons-vue'
import dayjs from 'dayjs'
import { listProjectEnvs, listVmProjectEnvs, listModules, scanModules, getRollbackPreview } from '../api'
import BatchUpdatePanel from '../components/BatchUpdatePanel.vue'
import RestartPanel from '../components/RestartPanel.vue'
import RollbackDialog from '../components/RollbackDialog.vue'
import VmDeployPanel from '../components/VmDeployPanel.vue'
import { useAuthStore } from '../stores/auth'

const auth = useAuthStore()
const route = useRoute()
const router = useRouter()

// 回滚模式：来自 ?rollback_from=<depID>。
// 传给 BatchUpdatePanel 后它会：预填 textarea + 修改按钮文案 + 提交时带 ref_deployment_id
const rollbackMode = ref(null)  // { refDeploymentID, prefillText } 或 null

function cancelRollback() {
  rollbackMode.value = null
  // 同时清掉 URL 的 rollback_from，避免刷新页面后又进入回滚模式
  if (route.query.rollback_from) {
    router.replace({ query: { ...route.query, rollback_from: undefined } })
  }
}

// v2 是因为 selID 从纯 number 变成 'k8s-1'/'vm-2' 字符串，老 key 数据自动失效
const RECENT_KEY = 'deploy_recent_envs_v2'
const RECENT_MAX = 3

// envs 同时包含 K8s 和 VM 两种环境，每条加 _kind ('k8s'|'vm') 和 _selID ('k8s-1'/'vm-2')
const envs = ref([])
const selectedID = ref(null)  // _selID 字符串
const modules = ref([])       // 仅 K8s 用
const tab = ref('update')     // K8s seg: 'update' | 'restart'
const vmTab = ref('rsync_and_update')  // VM seg: 'rsync_and_update' (默认) | 'rsync' | 'update_version'

// 切换环境时重置 vmTab：PROD 只能用 update_version，UAT/LPT 默认 rsync_and_update
const currentEnv = computed(() => envs.value.find(e => e._selID === selectedID.value))
// 小工具：env_type 归一化到小写，给 chip CSS class 用（K8s 是 uat/prod，VM 是 UAT/LPT/PROD）
function envTypeKey(e) { return (e?.env_type || '').toLowerCase() }
const recent = ref(null)
const rbVis = ref(false)
const rbID = ref(null)

// 搜索查询：el-select 的 :filter-method 会调用 filterEnv 同步到这里，
// 配合 v-if matchSearch 在选项 v-for 里隐藏不匹配项；非空时也隐藏"最近使用"分组
const searchQuery = ref('')
function filterEnv(q) { searchQuery.value = (q || '').toLowerCase().trim() }

const rescanning = ref(false)
async function onRescan() {
  // 仅 K8s env 有 scanModules（VM 用 VmDeployPanel 内的"同步服务列表"按钮）
  if (!currentEnv.value || currentEnv.value._kind !== 'k8s' || rescanning.value) return
  rescanning.value = true
  try {
    const r = await scanModules(currentEnv.value.id)
    await loadModules()
    const n = r?.count ?? modules.value.length
    ElMessage.success(`已扫描：${n} 个模块`)
  } catch (_) {
    // 全局拦截器已经弹了错误 toast
  } finally {
    rescanning.value = false
  }
}
function matchSearch(e) {
  if (!searchQuery.value) return true
  return e.name.toLowerCase().includes(searchQuery.value)
}

function projectOf(e) {
  if (!e) return ''
  const suffix = '-' + e.env_type
  return e.name.endsWith(suffix) ? e.name.slice(0, -suffix.length) : e.name
}

// 按 project 名分组（每组里有 UAT/PROD），UAT 在前
const envGroups = computed(() => {
  const m = new Map()
  envs.value.forEach(e => {
    const p = projectOf(e)
    if (!m.has(p)) m.set(p, [])
    m.get(p).push(e)
  })
  const groups = []
  for (const [project, items] of m.entries()) {
    items.sort((a, b) => (a.env_type === 'uat' ? -1 : 1))
    groups.push({ project, items })
  }
  groups.sort((a, b) => a.project.localeCompare(b.project))
  return groups
})

// 最近使用：localStorage 保存最近 3 个 _selID（'k8s-1'/'vm-2'）；watch selectedID 维护
const recentSelIDs = ref(JSON.parse(localStorage.getItem(RECENT_KEY) || '[]'))
const filteredRecent = computed(() => {
  return recentSelIDs.value
    .map(s => envs.value.find(e => e._selID === s))
    .filter(Boolean)
    .slice(0, RECENT_MAX)
})

watch(selectedID, (selID) => {
  const env = envs.value.find(e => e._selID === selID)
  if (!env) return
  const list = recentSelIDs.value.filter(s => s !== selID)
  list.unshift(selID)
  recentSelIDs.value = list.slice(0, RECENT_MAX)
  localStorage.setItem(RECENT_KEY, JSON.stringify(recentSelIDs.value))
  // VM env 没有 K8s modules，清空老的避免误显示
  if (env._kind === 'k8s') {
    loadModules()
  } else {
    modules.value = []
    tab.value = 'update' // 切回 K8s 时默认 update tab
    // VM env 切换：PROD 只能 update_version；UAT/LPT 默认 rsync_and_update
    vmTab.value = env.env_type === 'PROD' ? 'update_version' : 'rsync_and_update'
  }
})

async function loadEnvs() {
  // 同时拉 K8s 和 VM 两种环境，统一打 _kind / _selID 标记
  const [k8s, vm] = await Promise.all([
    listProjectEnvs().catch(() => []),
    listVmProjectEnvs().catch(() => []),
  ])
  const k8sEnvs = (k8s || []).map(e => ({ ...e, _kind: 'k8s', _selID: `k8s-${e.id}` }))
  const vmEnvs  = (vm  || []).map(e => ({ ...e, _kind: 'vm',  _selID: `vm-${e.id}`  }))
  envs.value = [...k8sEnvs, ...vmEnvs]

  // 如果 URL 带 rollback_from，优先按回滚预览选中对应 env；否则按"最近使用"
  const rbFrom = route.query.rollback_from ? Number(route.query.rollback_from) : null
  if (rbFrom) {
    await enterRollbackMode(rbFrom)
    return
  }
  if (envs.value.length && !selectedID.value) {
    const last = recentSelIDs.value.find(s => envs.value.some(e => e._selID === s))
    const target = last
      ? envs.value.find(e => e._selID === last)
      : envs.value[0]
    selectedID.value = target._selID
  }
}

// 加载回滚预览 → 自动选中项目环境 → 拼 textarea 预填串 → 切到 update tab
// 注意：回滚仅 K8s（VM 没回滚概念，部署是 update_version）
async function enterRollbackMode(depID) {
  try {
    const preview = await getRollbackPreview(depID)
    if (!preview?.modules?.length) {
      ElMessage.warning('该发布记录没有可回滚的模块')
      return
    }
    // 选中对应 K8s env
    const peID = preview.project_env_id
    const targetEnv = envs.value.find(e => e._kind === 'k8s' && e.id === peID)
    if (!targetEnv) {
      ElMessage.error('找不到对应的项目环境，可能已被删除')
      return
    }
    selectedID.value = targetEnv._selID
    // 只取 can_rollback=true（当前 tag 还是上次发布的 to_tag，回到 from_tag 才有意义）
    const lines = preview.modules
      .filter(m => m.can_rollback)
      .map(m => `${m.module}:${m.to_tag}`)
    if (!lines.length) {
      ElMessage.warning('这些模块当前 tag 已经不是此次发布的结果，无需回滚')
      return
    }
    rollbackMode.value = {
      refDeploymentID: depID,
      prefillText: lines.join('\n'),
    }
    tab.value = 'update'  // 切到批量更新镜像 tab
  } catch (e) {
    console.error('enter rollback mode failed:', e)
  }
}
async function loadModules() {
  if (!currentEnv.value || currentEnv.value._kind !== 'k8s') return
  modules.value = (await listModules(currentEnv.value.id)) || []
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
.hdr-r { display: flex; gap: 16px; align-items: center; }

.btn-rescan {
  display: inline-flex; align-items: center; gap: 6px;
  background: #fff; border: 1px solid var(--border); color: var(--text-2);
  padding: 7px 12px; border-radius: 6px;
  font: 500 12.5px var(--body); cursor: pointer; transition: all .12s;
}
.btn-rescan:hover:not(:disabled) { border-color: var(--primary); color: var(--primary); background: #f0f7ff; }
.btn-rescan:disabled { opacity: .55; cursor: not-allowed; }
.btn-rescan .el-icon { font-size: 14px; }
.btn-rescan .el-icon.spin { animation: btnspin 1s linear infinite; }
@keyframes btnspin { to { transform: rotate(360deg); } }
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

/* ===== Selector row (目标 + 动作 同一行) ===== */
.sel-row {
  display: flex; align-items: center; flex-wrap: wrap; gap: 12px;
  padding: 6px 0;
}
.sel-k {
  font-size: 11px; color: var(--text-3); text-transform: uppercase; letter-spacing: .8px;
  font-weight: 600;
}
.sel-divider {
  display: inline-block; width: 1px; height: 20px;
  background: var(--border); margin: 0 6px;
}
.empty-hint { color: var(--text-3); font-size: 12px; padding: 6px 0; }

/* Segmented control（代替以前的大 act-tab） */
.seg {
  display: inline-flex; background: #fff;
  border: 1px solid var(--border); border-radius: 6px; overflow: hidden;
}
.seg-btn {
  background: #fff; border: none; border-left: 1px solid var(--border);
  padding: 7px 14px; font: 500 13px var(--body); color: var(--text-2);
  cursor: pointer; display: inline-flex; align-items: center; gap: 6px;
  transition: all .12s;
}
.seg-btn:first-child { border-left: none; }
.seg-btn:hover:not(:disabled):not(.active) { color: var(--primary); background: #f0f7ff; }
.seg-btn.active { background: var(--primary); color: #fff; font-weight: 600; }
.seg-btn.active + .seg-btn { border-left-color: var(--primary); }
.seg-btn:disabled { opacity: .45; cursor: not-allowed; }
.seg-btn .el-icon { font-size: 14px; }

/* el-select 选项内布局 + 内联 chip */
.sel-v { display: flex; align-items: center; gap: 12px; flex-wrap: wrap; }
.env-chip-inline {
  font-family: var(--mono); font-size: 11px; font-weight: 700; letter-spacing: .5px;
  padding: 3px 8px; border-radius: 4px;
}
.env-chip-inline.uat { background: #ecfdf5; color: #059669; }
.env-chip-inline.lpt { background: #eff6ff; color: #1d4ed8; }
.env-chip-inline.prod { background: #fef2f2; color: #dc2626; }

/* K8S / VM 区分徽标（header h1 内） */
.kind-chip {
  font-family: var(--mono); font-size: 10px; font-weight: 700; letter-spacing: .6px;
  padding: 2px 7px; border-radius: 3px; margin-left: 4px;
  border: 1px solid;
}
.kind-chip.k8s { background: #f0f7ff; color: #1d4ed8; border-color: #bfdbfe; }
.kind-chip.vm  { background: #faf5ff; color: #7c3aed; border-color: #ddd6fe; }

/* VM 动作占位提示 */
.vm-action-hint {
  font: 500 12px var(--body); color: var(--text-3);
}
.vm-action-hint b { font-family: var(--mono); color: var(--text-2); font-weight: 600; }

/* 回滚模式横幅 */
.rb-banner {
  display: flex; align-items: center; gap: 12px;
  background: linear-gradient(90deg, #fef3c7, #fff);
  border: 1px solid #fde68a; border-left: 4px solid #f59e0b;
  border-radius: var(--radius);
  padding: 12px 16px; margin-bottom: 14px;
}
.rb-icon { color: #b45309; font-size: 22px; flex-shrink: 0; }
.rb-text { flex: 1; font-size: 13px; color: #78350f; line-height: 1.55; }
.rb-text b { font-family: var(--mono); color: #92400e; }
.rb-tip { font-size: 11.5px; color: #92400e; margin-top: 2px; }
.rb-cancel {
  padding: 5px 12px; border: 1px solid #fcd34d; background: #fff;
  color: #92400e; border-radius: 4px; cursor: pointer;
  font: 500 12px var(--body);
}
.rb-cancel:hover { background: #fef3c7; }
</style>

<!-- el-select 弹层样式（非 scoped，因为 popper 被 teleport 到 body 外）-->
<style>
.env-select-popper .opt-row {
  display: flex; align-items: center; gap: 8px; width: 100%;
  font-family: 'Fira Code', 'JetBrains Mono', Consolas, monospace;
}
.env-select-popper .opt-dot {
  width: 8px; height: 8px; border-radius: 50%; flex-shrink: 0;
}
.env-select-popper .opt-dot.uat { background: #10b981; }
.env-select-popper .opt-dot.lpt { background: #2563eb; }
.env-select-popper .opt-dot.prod {
  background: #ef4444;
  box-shadow: 0 0 0 3px rgba(239, 68, 68, .15);
}
.env-select-popper .opt-name {
  flex: 1; font-size: 12.5px; color: #1f2937;
}
.env-select-popper .opt-name.prod { font-weight: 700; color: #b91c1c; }
.env-select-popper .env-chip-mini {
  font-size: 10px; font-weight: 700; letter-spacing: .4px;
  padding: 1px 6px; border-radius: 3px; flex-shrink: 0;
}
.env-select-popper .env-chip-mini.uat { background: #ecfdf5; color: #059669; }
.env-select-popper .env-chip-mini.lpt { background: #eff6ff; color: #1d4ed8; }
.env-select-popper .env-chip-mini.prod { background: #fef2f2; color: #dc2626; }
.env-select-popper .kind-mini {
  font-size: 9.5px; font-weight: 700; letter-spacing: .5px;
  padding: 1px 5px; border-radius: 3px; flex-shrink: 0;
  border: 1px solid;
}
.env-select-popper .kind-mini.k8s { background: #f0f7ff; color: #1d4ed8; border-color: #bfdbfe; }
.env-select-popper .kind-mini.vm  { background: #faf5ff; color: #7c3aed; border-color: #ddd6fe; }
</style>
