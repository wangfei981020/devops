<template>
  <div class="vm-panel">
    <div class="p-hd">
      <h3>
        <el-icon><Monitor /></el-icon>
        VM 部署
        <span class="env-tag" :class="envType">{{ envType }}</span>
      </h3>
      <div class="hd-r">
        <button class="btn ghost sm" @click="onScanServices" :disabled="scanning">
          {{ scanning ? '扫描中...' : '🔄 同步服务列表' }}
        </button>
      </div>
    </div>

    <div class="vm-body">
      <!-- 服务选择 -->
      <div class="row">
        <label>① 选服务</label>
        <el-select v-model="selectedService" filterable placeholder="选择 service" style="flex:1;"
          @change="onServiceChanged">
          <el-option v-for="s in services" :key="s.id" :label="s.name" :value="s.id">
            <div style="display:flex;justify-content:space-between;">
              <span>{{ s.name }}</span>
              <span class="hint" style="margin-left:12px;">
                {{ s.hosts?.length || 0 }} 台 · {{ s.current_version || '未发布' }}
              </span>
            </div>
          </el-option>
        </el-select>
      </div>
      <div class="row sub" v-if="currentService">
        <span class="hint">ansible group：<code>{{ currentService.ansible_group }}</code></span>
        <span class="hint">目标主机：<code>{{ (currentService.hosts || []).join(', ') || '—' }}</code></span>
        <span class="hint" v-if="currentService.current_version">
          当前版本：<code>{{ currentService.current_version }}</code>
        </span>
      </div>

      <!-- 同步代码 -->
      <div class="action-card rsync">
        <div class="ac-l">
          <div class="ac-title">📥 同步代码（rsync）</div>
          <div class="ac-desc">从代码服务器拉源码到 ansible 服务器，刷新版本列表。**不部署到目标 VM**。</div>
        </div>
        <button class="btn warn" @click="onRsync" :disabled="!selectedService || running">
          {{ runningAction === 'rsync' ? '同步中...' : '执行 rsync' }}
        </button>
      </div>

      <!-- 选版本 + 部署 -->
      <div class="row" v-if="selectedService">
        <label>② 选版本</label>
        <el-select v-model="selectedVersion" filterable placeholder="选择版本" style="flex:1;"
          :loading="loadingVersions">
          <el-option v-for="(v, idx) in versions" :key="v"
            :label="(idx === 0 ? '⭐ 最新 · ' : '') + v"
            :value="v" />
        </el-select>
        <button class="btn ghost sm" @click="loadVersions" :disabled="loadingVersions">
          {{ loadingVersions ? '加载中...' : '🔄 刷新' }}
        </button>
      </div>

      <div class="action-card deploy" v-if="selectedService">
        <div class="ac-l">
          <div class="ac-title">🚀 部署到 VM（update_version）</div>
          <div class="ac-desc">把选中版本通过 ansible-playbook 推到 <b>{{ (currentService?.hosts || []).length }}</b> 台目标 VM</div>
        </div>
        <button :class="['btn', envType === 'PROD' ? 'danger' : 'success']"
          @click="onDeploy"
          :disabled="!selectedService || !selectedVersion || running">
          <span v-if="runningAction === 'update_version'">部署中...</span>
          <span v-else-if="envType === 'PROD'">部署 PROD · 需二次确认</span>
          <span v-else>部署到 {{ envType }}</span>
        </button>
      </div>

      <!-- 进行中卡片：替代以前的黑底实时日志，跟 K8s BatchUpdatePanel 风格统一 -->
      <!-- 完整 ansible 日志去发布历史看（实时 SSE 或归档），这里只显示进度 -->
      <div v-if="running || lastDone" class="progress-card" :class="progressKind">
        <div class="pc-l">
          <el-icon class="pc-icon" :class="{ spin: running }">
            <Loading v-if="running" />
            <SuccessFilled v-else-if="lastDone?.status === 'success'" />
            <CircleCloseFilled v-else-if="lastDone?.status === 'failed'" />
            <Warning v-else />
          </el-icon>
          <div class="pc-text">
            <div class="pc-title">
              <span v-if="running">{{ runningAction === 'rsync' ? '同步中…' : '部署中…' }}</span>
              <span v-else-if="lastDone?.status === 'success'">✅ 完成</span>
              <span v-else-if="lastDone?.status === 'canceled'">⏹ 已取消</span>
              <span v-else>❌ 失败</span>
              <span class="pc-meta">
                · 任务 #{{ deploymentID }}
                <span v-if="elapsedSec > 0"> · 耗时 {{ elapsedSec }}s</span>
              </span>
            </div>
            <div class="pc-sub">
              <RouterLink :to="`/history?expand=${deploymentID}`" class="pc-link">
                {{ running ? '查看实时日志 →' : '查看完整日志 →' }}
              </RouterLink>
              <span v-if="lastDone?.error_msg" class="pc-err" :title="lastDone.error_msg">
                · {{ lastDone.error_msg.slice(0, 80) }}{{ lastDone.error_msg.length > 80 ? '…' : '' }}
              </span>
            </div>
          </div>
        </div>
        <div class="pc-r">
          <button v-if="running" class="btn ghost sm danger-hover" @click="onCancel">✕ 取消</button>
          <button v-else class="btn ghost sm" @click="lastDone = null; deploymentID = 0; elapsedSec = 0">关闭</button>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup>
import { ref, computed, onUnmounted, watch } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Monitor, Loading, SuccessFilled, CircleCloseFilled, Warning } from '@element-plus/icons-vue'
import { RouterLink } from 'vue-router'
import {
  scanVmServices, listVmServices, listVmServiceVersions,
  vmDeploy, vmDeployCancel, getDeployment,
} from '../api'

const props = defineProps({
  vmProjectEnv: { type: Object, required: true },     // { id, name, env_type, ... }
})

const services = ref([])
const selectedService = ref(null)
const selectedVersion = ref('')
const versions = ref([])
const loadingVersions = ref(false)
const scanning = ref(false)

// 进行中状态：跟 K8s BatchUpdatePanel 一样轮询 getDeployment(id) 每 2s
// 不再实时拉 ansible 日志（那个去发布历史里看）
const deploymentID = ref(0)
const runningAction = ref('') // 'rsync' | 'update_version' | ''
const running = computed(() => runningAction.value !== '')
const elapsedSec = ref(0)
const lastDone = ref(null) // { status, error_msg } 终态后留 30s 给用户看
let pollTimer = null
let elapsedTimer = null

const envType = computed(() => props.vmProjectEnv?.env_type || 'UAT')
const currentService = computed(() => services.value.find(s => s.id === selectedService.value))

const progressKind = computed(() => {
  if (running.value) return 'running'
  if (!lastDone.value) return ''
  return lastDone.value.status // success / failed / canceled
})

watch(() => props.vmProjectEnv?.id, (id) => {
  if (id) loadServices()
}, { immediate: true })

async function loadServices() {
  try {
    services.value = await listVmServices(props.vmProjectEnv.id) || []
  } catch (e) {
    ElMessage.error('加载服务列表失败：' + (e?.response?.data?.message || e.message))
  }
}

async function onScanServices() {
  if (scanning.value) return
  scanning.value = true
  try {
    const r = await scanVmServices(props.vmProjectEnv.id)
    ElMessage.success(`已同步 ${r.count} 个服务`)
    await loadServices()
  } catch (e) {
    ElMessage.error('扫描失败：' + (e?.response?.data?.message || e.message))
  } finally { scanning.value = false }
}

async function onServiceChanged() {
  selectedVersion.value = ''
  versions.value = []
  await loadVersions()
}

async function loadVersions() {
  if (!selectedService.value) return
  loadingVersions.value = true
  try {
    const r = await listVmServiceVersions(selectedService.value)
    versions.value = r.versions || []
    if (versions.value.length > 0 && !selectedVersion.value) {
      selectedVersion.value = versions.value[0]   // 默认最新
    }
  } catch (e) {
    ElMessage.error('版本列表失败：' + (e?.response?.data?.message || e.message))
    versions.value = []
  } finally { loadingVersions.value = false }
}

async function onRsync() {
  if (!currentService.value) return
  await runAction('rsync')
}

async function onDeploy() {
  if (!currentService.value || !selectedVersion.value) return
  if (envType.value === 'PROD') {
    try {
      await ElMessageBox.confirm(
        `即将把 <b>${currentService.value.name}</b> 部署到 <b>PROD</b> ${currentService.value.hosts?.length || 0} 台机器，版本 ${selectedVersion.value}。`,
        '⚠ PROD 二次确认',
        { type: 'warning', dangerouslyUseHTMLString: true, confirmButtonText: '确认部署 PROD', confirmButtonClass: 'el-button--danger' })
    } catch { return }
  }
  await runAction('update_version')
}

async function runAction(action) {
  runningAction.value = action
  deploymentID.value = 0
  elapsedSec.value = 0
  lastDone.value = null
  try {
    const r = await vmDeploy({
      vm_project_env_id: props.vmProjectEnv.id,
      service: currentService.value.name,
      action,
      version: action === 'update_version' ? selectedVersion.value : undefined,
    })
    deploymentID.value = r.deployment_id
    ElMessage.success(`已提交 · 任务 #${r.deployment_id}`)
    startPolling(r.deployment_id)
  } catch (e) {
    runningAction.value = ''
    ElMessage.error('提交失败：' + (e?.response?.data?.message || e.message))
  }
}

// 轮询 deployment 状态：每 2s 一次拉 status / duration_sec / error_msg
function startPolling(depID) {
  stopPolling()
  // 本地秒表（终态前每秒 +1，给用户即时反馈，避免只靠后端 duration_sec 显示卡顿）
  const startTs = Date.now()
  elapsedTimer = setInterval(() => {
    if (!running.value) return
    elapsedSec.value = Math.floor((Date.now() - startTs) / 1000)
  }, 1000)
  pollTimer = setInterval(async () => {
    try {
      const dep = await getDeployment(depID)
      if (!dep) return
      // 后端 duration_sec 在终态时才靠谱；进行中用本地秒表
      if (dep.status && dep.status !== 'pending') {
        elapsedSec.value = dep.duration_sec ?? elapsedSec.value
        lastDone.value = { status: dep.status, error_msg: dep.error_msg || '' }
        runningAction.value = ''
        stopPolling()
        // 任务结束后刷新当前 service（current_version 可能更新了）
        await loadServices()
        const tip = {
          success: '✅ 部署成功',
          failed: '❌ 部署失败，去发布历史看完整日志',
          canceled: '⏹ 已取消',
        }[dep.status] || `状态：${dep.status}`
        if (dep.status === 'success') ElMessage.success(tip)
        else if (dep.status === 'canceled') ElMessage.warning(tip)
        else ElMessage.error(tip)
      }
    } catch (e) {
      // 单次拉失败不打断，下次 tick 继续
      console.warn('[vm-poll] tick failed:', e?.message)
    }
  }, 2000)
}

function stopPolling() {
  if (pollTimer) { clearInterval(pollTimer); pollTimer = null }
  if (elapsedTimer) { clearInterval(elapsedTimer); elapsedTimer = null }
}

async function onCancel() {
  if (!deploymentID.value) return
  try {
    await ElMessageBox.confirm(
      'VM 任务取消会向 ansible 控制机发 SIGTERM 杀掉 ansible-playbook 进程，**可能让目标机器留在半部署状态**。继续吗？',
      '⚠ 取消 VM 任务',
      { type: 'warning', dangerouslyUseHTMLString: true, confirmButtonText: '确认取消', confirmButtonClass: 'el-button--danger' })
  } catch { return }
  try {
    await vmDeployCancel(deploymentID.value)
    ElMessage.success('已请求取消，等 agent 真正杀进程后状态会更新')
  } catch (e) {
    ElMessage.error('取消失败：' + (e?.response?.data?.message || e.message))
  }
}

onUnmounted(() => {
  stopPolling()
})
</script>

<style scoped>
.vm-panel { display: flex; flex-direction: column; }
.p-hd { padding: 14px 20px; border-bottom: 1px solid var(--border-soft); display: flex; justify-content: space-between; align-items: center; }
.p-hd h3 { font: 600 14px/1 var(--body); color: var(--text); display: flex; align-items: center; gap: 8px; margin: 0; }
.p-hd h3 .el-icon { color: var(--primary); font-size: 16px; }

.env-tag { font-size: 11px; padding: 2px 8px; border-radius: 4px; font-family: var(--mono); margin-left: 4px; }
.env-tag.UAT { background: #ecfdf5; color: #059669; }
.env-tag.LPT { background: #eff6ff; color: #1d4ed8; }
.env-tag.PROD { background: #fef2f2; color: #dc2626; }

.vm-body { padding: 16px 20px; display: flex; flex-direction: column; gap: 12px; }

.row { display: flex; align-items: center; gap: 10px; }
.row label { width: 80px; font-size: 12.5px; color: var(--text-2); font-weight: 500; }
.row.sub { padding-left: 90px; flex-wrap: wrap; }
.row.sub .hint { margin-right: 14px; }

code { font-family: var(--mono); background: #f3f4f6; padding: 1px 6px; border-radius: 3px; font-size: 11.5px; color: var(--text-2); }

.action-card { display: flex; align-items: center; gap: 14px; padding: 12px 16px; border: 1px solid var(--border); border-radius: 6px; background: #fafbfc; }
.action-card.rsync { border-color: #fde68a; background: #fffbeb; }
.action-card.deploy { border-color: #a7f3d0; background: #ecfdf5; }
.ac-l { flex: 1; }
.ac-title { font-weight: 600; font-size: 13px; color: var(--text); margin-bottom: 4px; }
.ac-desc { font-size: 11.5px; color: var(--text-3); }

.btn { background: #fff; border: 1px solid var(--border); color: var(--text); padding: 7px 14px; border-radius: 5px; font: 500 12.5px var(--body); cursor: pointer; }
.btn.ghost:hover { border-color: var(--primary); color: var(--primary); }
.btn.warn { background: #f59e0b; color: #fff; border-color: #f59e0b; }
.btn.warn:hover:not(:disabled) { background: #d97706; }
.btn.success { background: var(--success); color: #fff; border-color: var(--success); }
.btn.success:hover:not(:disabled) { background: var(--success-dark); }
.btn.danger { background: var(--danger); color: #fff; border-color: var(--danger); }
.btn.danger:hover:not(:disabled) { background: var(--danger-dark); }
.btn:disabled { opacity: .4; cursor: not-allowed; }
.btn.sm { padding: 4px 10px; font-size: 11.5px; }
.btn.danger-hover:hover:not(:disabled) { color: var(--danger); border-color: var(--danger); background: #fef2f2; }

.hint { font-size: 11.5px; color: var(--text-3); }

/* 进行中卡片：跟 K8s 那边同款风格 */
.progress-card {
  display: flex; align-items: center; gap: 14px;
  padding: 14px 18px;
  border: 1px solid; border-radius: 8px;
  margin-top: 4px;
}
.progress-card.running { background: #eff6ff; border-color: #bfdbfe; }
.progress-card.success { background: #ecfdf5; border-color: #a7f3d0; }
.progress-card.failed  { background: #fef2f2; border-color: #fecaca; }
.progress-card.canceled { background: #fff7ed; border-color: #fed7aa; }

.pc-l { display: flex; align-items: center; gap: 14px; flex: 1; min-width: 0; }
.pc-r { display: flex; align-items: center; gap: 8px; }
.pc-icon { font-size: 28px; flex-shrink: 0; }
.progress-card.running .pc-icon { color: #1d4ed8; }
.progress-card.success .pc-icon { color: #059669; }
.progress-card.failed .pc-icon { color: #dc2626; }
.progress-card.canceled .pc-icon { color: #c2410c; }
.pc-icon.spin { animation: pc-spin 1s linear infinite; }
@keyframes pc-spin { to { transform: rotate(360deg); } }

.pc-text { flex: 1; min-width: 0; }
.pc-title { font: 600 14px/1.3 var(--body); color: var(--text); display: flex; align-items: baseline; gap: 6px; flex-wrap: wrap; }
.pc-meta { color: var(--text-3); font-weight: 400; font-family: var(--mono); font-size: 12px; }
.pc-sub { font-size: 12px; color: var(--text-2); margin-top: 4px; }
.pc-link { color: var(--primary); text-decoration: none; font-weight: 500; }
.pc-link:hover { text-decoration: underline; }
.pc-err {
  margin-left: 6px; color: var(--danger);
  font-family: var(--mono); font-size: 11.5px;
  overflow: hidden; text-overflow: ellipsis; white-space: nowrap;
  max-width: 50%;
  display: inline-block; vertical-align: bottom;
}
</style>
