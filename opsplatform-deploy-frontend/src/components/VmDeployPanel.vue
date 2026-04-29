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
                {{ s.hosts?.length || 0 }} 台 · {{ s.current_version ? s.current_version.slice(0,8) : '未发布' }}
              </span>
            </div>
          </el-option>
        </el-select>
      </div>
      <div class="row sub" v-if="currentService">
        <span class="hint">ansible group：<code>{{ currentService.ansible_group }}</code></span>
        <span class="hint">目标主机：<code>{{ (currentService.hosts || []).join(', ') || '—' }}</code></span>
        <span class="hint" v-if="currentService.current_version">
          当前版本：<code>{{ currentService.current_version.slice(0,12) }}</code>
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
            :label="(idx === 0 ? '⭐ 最新 · ' : '') + v.slice(0,16) + '...'"
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

      <!-- 实时日志 -->
      <div v-if="logsVisible" class="logs-block">
        <div class="logs-hd">
          <span>实时日志（{{ runningAction || '—' }}）</span>
          <span class="hint" v-if="taskID">task_id: {{ taskID.slice(0,8) }}...</span>
          <span style="flex:1;"></span>
          <button v-if="running" class="btn ghost sm danger-hover" @click="onCancel">✕ 取消</button>
          <button class="btn ghost sm" @click="logsVisible = false">隐藏</button>
        </div>
        <pre ref="logArea" class="logs-pre">{{ logBuf }}</pre>
      </div>
    </div>
  </div>
</template>

<script setup>
import { ref, computed, onUnmounted, nextTick, watch } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Monitor } from '@element-plus/icons-vue'
import {
  scanVmServices, listVmServices, listVmServiceVersions,
  vmDeploy, vmDeployCancel, vmDeployLogsURL,
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

const taskID = ref('')
const deploymentID = ref(0)
const runningAction = ref('') // 'rsync' | 'update_version' | ''
const running = computed(() => runningAction.value !== '')
const logsVisible = ref(false)
const logBuf = ref('')
const logArea = ref(null)
let abortCtrl = null

const envType = computed(() => props.vmProjectEnv?.env_type || 'UAT')
const currentService = computed(() => services.value.find(s => s.id === selectedService.value))

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
  // PROD 要二次确认
  if (envType.value === 'PROD') {
    try {
      await ElMessageBox.confirm(
        `即将把 <b>${currentService.value.name}</b> 部署到 <b>PROD</b> ${currentService.value.hosts?.length || 0} 台机器，版本 ${selectedVersion.value.slice(0,12)}...`,
        '⚠ PROD 二次确认',
        { type: 'warning', dangerouslyUseHTMLString: true, confirmButtonText: '确认部署 PROD', confirmButtonClass: 'el-button--danger' })
    } catch { return }
  }
  await runAction('update_version')
}

async function runAction(action) {
  runningAction.value = action
  logBuf.value = ''
  logsVisible.value = true
  taskID.value = ''
  deploymentID.value = 0
  try {
    const r = await vmDeploy({
      vm_project_env_id: props.vmProjectEnv.id,
      service: currentService.value.name,
      action,
      version: action === 'update_version' ? selectedVersion.value : undefined,
    })
    deploymentID.value = r.deployment_id
    taskID.value = r.task_id
    ElMessage.success(`已提交 · deployment ${r.deployment_id}`)
    streamLogs(r.deployment_id)
  } catch (e) {
    runningAction.value = ''
    ElMessage.error('提交失败：' + (e?.response?.data?.message || e.message))
  }
}

// SSE 流式日志：用 fetch + ReadableStream（EventSource 不能带 Authorization 头）
async function streamLogs(depID) {
  abortCtrl = new AbortController()
  const url = vmDeployLogsURL(depID, 0, true)
  const token = localStorage.getItem('deploy_token') || ''
  try {
    const resp = await fetch(url, {
      headers: { Authorization: 'Bearer ' + token },
      signal: abortCtrl.signal,
    })
    if (!resp.ok || !resp.body) {
      logBuf.value += `[error] HTTP ${resp.status}\n`
      runningAction.value = ''
      return
    }
    const reader = resp.body.getReader()
    const dec = new TextDecoder()
    while (true) {
      const { value, done } = await reader.read()
      if (done) break
      const chunk = dec.decode(value)
      // 解析 SSE：data: ... 行拼回
      for (const line of chunk.split('\n')) {
        if (line.startsWith('data: ')) {
          logBuf.value += line.slice(6) + '\n'
        } else if (line.startsWith('event: end')) {
          runningAction.value = ''
        }
      }
      nextTick(() => {
        if (logArea.value) logArea.value.scrollTop = logArea.value.scrollHeight
      })
    }
  } catch (e) {
    if (e.name !== 'AbortError') {
      logBuf.value += `\n[stream error] ${e.message}\n`
    }
  } finally {
    runningAction.value = ''
    // 任务结束后刷新当前 service（current_version 可能更新了）
    loadServices()
  }
}

async function onCancel() {
  if (!deploymentID.value) return
  try {
    await vmDeployCancel(deploymentID.value)
    ElMessage.success('已请求取消')
  } catch (e) {
    ElMessage.error('取消失败：' + (e?.response?.data?.message || e.message))
  }
}

onUnmounted(() => {
  if (abortCtrl) abortCtrl.abort()
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
.action-card.deploy { } /* PROD 时按钮自己变红 */
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

.logs-block { border: 1px solid var(--border); border-radius: 6px; overflow: hidden; }
.logs-hd { display: flex; align-items: center; gap: 10px; padding: 8px 12px; background: #f9fafb; border-bottom: 1px solid var(--border-soft); font-size: 12px; }
.logs-pre { margin: 0; padding: 10px 14px; background: #1e1e1e; color: #d4d4d4; font: 500 11.5px/1.7 var(--mono); max-height: 480px; overflow: auto; white-space: pre-wrap; word-break: break-all; }
</style>
