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
        <button class="btn warn" @click="onRsync" :disabled="!selectedService || submitting">
          {{ submitting ? '提交中...' : '执行 rsync' }}
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
          :disabled="!selectedService || !selectedVersion || submitting">
          <span v-if="submitting">提交中...</span>
          <span v-else-if="envType === 'PROD'">部署 PROD · 需二次确认</span>
          <span v-else>部署到 {{ envType }}</span>
        </button>
      </div>

      <!-- 提交后没有进度卡片：状态去右上角 InflightDock 看，详情去发布历史 -->
    </div>
  </div>
</template>

<script setup>
import { ref, computed, watch } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Monitor } from '@element-plus/icons-vue'
import {
  scanVmServices, listVmServices, listVmServiceVersions,
  vmDeploy,
} from '../api'
import { useDeploymentsStore } from '../stores/deployments'
import { useAuthStore } from '../stores/auth'

const props = defineProps({
  vmProjectEnv: { type: Object, required: true },     // { id, name, env_type, ... }
})

const deployments = useDeploymentsStore()
const auth = useAuthStore()

const services = ref([])
const selectedService = ref(null)
const selectedVersion = ref('')
const versions = ref([])
const loadingVersions = ref(false)
const scanning = ref(false)
// submitting: 提交动作期间禁用按钮防重；提交后状态交给 InflightDock 跟踪
const submitting = ref(false)

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

// runAction：提交任务后把 deployment_id 注册进 InflightDock，剩下进度跟踪由 store 统一管。
//   不再面板内轮询 / 显示进度卡 / 提供取消按钮 —— 全部去右上角 dock 看
async function runAction(action) {
  if (submitting.value) return
  submitting.value = true
  try {
    const r = await vmDeploy({
      vm_project_env_id: props.vmProjectEnv.id,
      service: currentService.value.name,
      action,
      version: action === 'update_version' ? selectedVersion.value : undefined,
    })
    // 注册进 InflightDock
    deployments.startTracking(r.deployment_id, {
      action: 'vm_' + action,           // store 里 ACTION_LABEL 会映射成中文
      envName: props.vmProjectEnv.name,
      envType: envType.value,            // store 内部 toLowerCase
      modules: 1,                        // VM 单服务 = 1
      operator: auth.user?.username || '',
    })
    ElMessage.success(`已提交 · 任务 #${r.deployment_id}，右上角看进度`)
  } catch (e) {
    ElMessage.error('提交失败：' + (e?.response?.data?.message || e.message))
  } finally {
    submitting.value = false
  }
}
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
</style>
