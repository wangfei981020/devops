<template>
  <div class="vm-panel">
    <div class="p-hd">
      <h3>
        <el-icon><Monitor /></el-icon>
        {{ tabLabel }}
        <span class="env-tag" :class="envType">{{ envType }}</span>
      </h3>
      <div class="hd-r">
        <button class="btn ghost sm" @click="onScanServices" :disabled="scanning">
          {{ scanning ? '扫描中...' : '🔄 同步服务列表' }}
        </button>
      </div>
    </div>

    <div class="vm-body">
      <!-- 子模式切换：自动 / 手动 -->
      <div class="batch-modes-row">
        <span class="row-k">输入方式</span>
        <div class="batch-modes">
          <button :class="['mode-btn', { active: mode === 'auto' }]" @click="mode = 'auto'">自动获取</button>
          <button :class="['mode-btn', { active: mode === 'manual' }]" @click="mode = 'manual'">手动输入</button>
        </div>
      </div>

      <!-- 自动模式 -->
      <div v-if="mode === 'auto'" class="auto-mode">
        <div class="row">
          <label>① 选服务</label>
          <el-select v-model="autoSelected" multiple filterable collapse-tags collapse-tags-tooltip
            placeholder="搜索/多选服务" style="flex:1;">
            <el-option v-for="s in services" :key="s.id" :label="s.name" :value="s.name">
              <div style="display:flex;justify-content:space-between;">
                <span>{{ s.name }}</span>
                <span class="hint" style="margin-left:12px;">
                  {{ s.hosts?.length || 0 }} 台 · {{ s.current_version || '未发布' }}
                </span>
              </div>
            </el-option>
          </el-select>
          <span class="hint" style="margin-left: 4px;">
            已选 <b>{{ autoSelected.length }}</b> / {{ services.length }}
          </span>
        </div>
        <!-- update_version 提示：每行独立选版本，默认⭐最新，可单独改 -->
        <div v-if="isUpdate" class="row" style="padding-left:90px;">
          <span class="hint">默认 ⭐最新 · 想用历史版本，点对应行的下拉单独改（互不影响）</span>
        </div>
      </div>

      <!-- 手动模式 -->
      <div v-if="mode === 'manual'" class="manual-mode">
        <div class="row">
          <label>① 输入清单</label>
          <textarea v-model="manualText" class="batch-textarea" rows="6"
            :placeholder="manualPlaceholder"
            @blur="parseManual"></textarea>
        </div>
        <div class="row" style="padding-left:90px;">
          <button class="btn ghost sm" @click="parseManual">预览</button>
          <button class="btn ghost sm" @click="manualText = ''; manualParsed = []; batchPreview = []">清空</button>
          <span class="hint" style="margin-left:8px;">
            解析 <b>{{ manualParsed.length }}</b> 行
            <span v-if="manualErrors.length" style="color:var(--danger);">
              · <b>{{ manualErrors.length }}</b> 行错误
            </span>
          </span>
        </div>
      </div>

      <!-- 预览表 -->
      <div v-if="batchPreview.length > 0" class="row" style="display:block;padding-left:0;">
        <label style="display:block; margin-left: 90px; font-size:11.5px; color:var(--text-2); margin-bottom:6px;">
          ② 清单预览（{{ batchPreview.length }} 个服务，{{ totalHosts }} 台目标主机{{ isUpdate ? '' : ' · rsync 不部署到目标 VM' }})
        </label>
        <table class="batch-table">
          <thead>
            <tr>
              <th>服务</th>
              <th v-if="isUpdate" style="width:480px;">版本</th>
              <th>目标主机</th>
              <th style="width:36px;"></th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="(p, idx) in batchPreview" :key="p.service" :class="{ invalid: !p.valid }">
              <td class="mono">{{ p.service }}</td>
              <td v-if="isUpdate">
                <span v-if="!p.valid" class="err-text">❌ {{ p.error }}</span>
                <!-- 自动模式：每行独立下拉，默认⭐最新版，可单独改 -->
                <el-select v-else-if="mode === 'auto'"
                  v-model="p.version" filterable size="small" style="width:100%;"
                  :loading="p.loadingVersions">
                  <el-option v-for="(v, i) in (p.versionList || [])" :key="v"
                    :label="(i === 0 ? '⭐最新 · ' : '') + v" :value="v" />
                </el-select>
                <!-- 手动模式：用户在 textarea 显式写了 version，preview 表格静态显示 -->
                <span v-else class="mono">
                  <span v-if="p.isLatest" class="latest-tag">⭐最新</span>
                  {{ p.version }}
                </span>
              </td>
              <td class="mono hosts-cell">
                <span v-if="!p.valid && !isUpdate" class="err-text">❌ {{ p.error }}</span>
                <span v-else-if="p.hosts && p.hosts.length > 0">
                  {{ p.hosts.join(', ') }}
                </span>
                <span v-else class="hint">—</span>
              </td>
              <td>
                <button class="row-x" @click="removeFromBatch(idx)" title="从清单中移除">×</button>
              </td>
            </tr>
          </tbody>
        </table>
      </div>

      <!-- 提交按钮 -->
      <div class="action-card" :class="isUpdate ? 'deploy' : 'rsync'" style="margin-top:12px;">
        <div class="ac-l">
          <div class="ac-title">{{ submitTitle }}</div>
          <div class="ac-desc">
            <span v-if="batchPreview.length === 0">
              {{ mode === 'auto' ? '先选服务（自动）' : '先输入清单（手动）' }}
            </span>
            <span v-else>
              共 <b>{{ validCount }}</b> 个服务
              <span v-if="isUpdate"> · <b>{{ totalHosts }}</b> 台目标主机</span>
              <span v-if="invalidCount > 0" style="color:var(--warning);">
                · {{ invalidCount }} 行错误将跳过
              </span>
            </span>
          </div>
        </div>
        <button :class="submitBtnClass" @click="onSubmit"
          :disabled="validCount === 0 || submitting">
          <span v-if="submitting">提交中...</span>
          <span v-else-if="envType === 'PROD'">{{ submitBtnText }} · 需二次确认</span>
          <span v-else>{{ submitBtnText }}</span>
        </button>
      </div>
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
  vmProjectEnv: { type: Object, required: true },
  // tab：'rsync' = 批量同步代码（不部署 VM）；'update_version' = 批量部署到 VM
  tab: { type: String, default: 'update_version' },
})

const deployments = useDeploymentsStore()
const auth = useAuthStore()

const services = ref([])
const scanning = ref(false)
const submitting = ref(false)

const mode = ref('auto')              // 'auto' | 'manual'
const autoSelected = ref([])
const manualText = ref('')
const manualParsed = ref([])
const manualErrors = ref([])

const batchPreview = ref([])

const envType = computed(() => props.vmProjectEnv?.env_type || 'UAT')
const isUpdate = computed(() => props.tab === 'update_version')
const tabLabel = computed(() => isUpdate.value ? '批量更新' : '批量 rsync')

const submitTitle = computed(() =>
  isUpdate.value ? '🚀 更新到 VM' : '📥 rsync 从 206 同步代码')
const submitBtnText = computed(() =>
  isUpdate.value ? `更新 ${validCount.value} 个到 ${envType.value}`
                 : `rsync ${validCount.value} 个`)
const submitBtnClass = computed(() => {
  if (envType.value === 'PROD') return 'btn danger'
  return isUpdate.value ? 'btn success' : 'btn warn'
})

const manualPlaceholder = computed(() => {
  if (isUpdate.value) {
    return `每行一个：service[:version]，省略 version = 用最新
G01_op_office:011994266913a15a7ab51c479129fd17d1dacf5c
G01_anchor_web
G01_xxx:5318b2c89abc`
  }
  return `每行一个 service（rsync 不需要版本号）
G01_op_office
G01_anchor_web
G01_xxx`
})

const validCount = computed(() => batchPreview.value.filter(p => p.valid).length)
const invalidCount = computed(() => batchPreview.value.filter(p => !p.valid).length)
const totalHosts = computed(() => {
  const set = new Set()
  batchPreview.value.forEach(p => { if (p.valid) (p.hosts || []).forEach(h => set.add(h)) })
  return set.size
})

watch(() => props.vmProjectEnv?.id, (id) => {
  if (id) {
    loadServices()
    resetState()
  }
}, { immediate: true })

// 切 tab 重置选择 + 预览
watch(() => props.tab, () => { resetState() })

function resetState() {
  autoSelected.value = []
  manualText.value = ''
  manualParsed.value = []
  manualErrors.value = []
  batchPreview.value = []
}

// 自动模式：multi-select 同步到 batchPreview
//   每个新加的 row：update_version 时立即拉版本列表，默认填⭐最新版
//   已存在的 row（用户改过版本）保留
watch(autoSelected, async (newList) => {
  if (mode.value !== 'auto') return
  const oldMap = new Map(batchPreview.value.map(p => [p.service, p]))
  batchPreview.value = newList.map(name => {
    const old = oldMap.get(name)
    if (old) return old
    const svc = services.value.find(s => s.name === name)
    return {
      service: name,
      version: '',
      hosts: svc?.hosts || [],
      valid: true,
      error: '',
      isLatest: false,
      loadingVersions: false,
      versionList: [],
    }
  })
  // update_version 模式：给每个新 row 并行拉版本列表 + 默认填⭐最新
  // 并行：N 个服务 = 单次延迟（~300ms），不是 N × 300ms 串行
  if (isUpdate.value) {
    await Promise.all(batchPreview.value.map(async (p) => {
      if (!p.valid) return
      if (p.versionList.length > 0) return // 已加载过，跳过
      p.loadingVersions = true
      const versions = await loadVersionsFor(p.service)
      p.versionList = versions
      if (versions.length > 0 && !p.version) {
        p.version = versions[0]
        p.isLatest = true
      }
      p.loadingVersions = false
    }))
  }
}, { deep: false })

watch(mode, () => {
  // 子模式 auto/manual 切换重置
  batchPreview.value = []
  manualParsed.value = []
  manualErrors.value = []
})

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

async function loadVersionsFor(serviceName) {
  // 不缓存：避免出现 A 用户看到旧版本、B 用户看到新版本的不一致。
  // 性能由调用方用 Promise.all 并行化承接，单次后端 ~300ms 用户可接受。
  const svc = services.value.find(s => s.name === serviceName)
  if (!svc) return []
  try {
    const r = await listVmServiceVersions(svc.id)
    return r.versions || []
  } catch (e) {
    ElMessage.error(`${serviceName} 版本列表失败：${e?.response?.data?.message || e.message}`)
    return []
  }
}

function removeFromBatch(idx) {
  const removed = batchPreview.value[idx]
  batchPreview.value.splice(idx, 1)
  if (mode.value === 'auto') {
    autoSelected.value = autoSelected.value.filter(s => s !== removed.service)
  }
}

async function parseManual() {
  if (mode.value !== 'manual') return
  const lines = manualText.value.split('\n').map(l => l.trim()).filter(Boolean)
  const parsed = []
  const errors = []
  const seen = new Set()
  lines.forEach((raw, i) => {
    if (raw.startsWith('#')) return
    const [name, ver] = raw.split(':').map(s => s.trim())
    if (!name) {
      errors.push({ line: i + 1, raw, err: '空 service' })
      return
    }
    if (seen.has(name)) {
      errors.push({ line: i + 1, raw, err: '重复 service: ' + name })
      return
    }
    seen.add(name)
    parsed.push({ service: name, version: ver || '' })
  })
  manualParsed.value = parsed
  manualErrors.value = errors

  // 先拼好「需要拉版本」的项，并行拉，再组装 preview
  const items = parsed.map(p => {
    const svc = services.value.find(s => s.name === p.service)
    return {
      p, svc,
      needFetch: !!svc && isUpdate.value && !p.version,
    }
  })
  const fetchResults = await Promise.all(
    items.map(it => it.needFetch ? loadVersionsFor(it.p.service) : Promise.resolve(null))
  )

  const preview = items.map(({ p, svc }, i) => {
    if (!svc) {
      return {
        service: p.service, version: isUpdate.value ? (p.version || '—') : '',
        hosts: [], valid: false,
        error: '服务不存在（先点"同步服务列表"）',
        isLatest: false, loadingVersions: false, versionList: [],
      }
    }
    let version = ''
    let isLatest = false
    if (isUpdate.value) {
      version = p.version
      if (!version) {
        const versions = fetchResults[i] || []
        if (versions.length === 0) {
          return {
            service: p.service, version: '—',
            hosts: svc.hosts || [], valid: false,
            error: '版本列表为空（先 rsync 同步代码）',
            isLatest: false, loadingVersions: false, versionList: versions,
          }
        }
        version = versions[0]
        isLatest = true
      }
    }
    return {
      service: p.service, version,
      hosts: svc.hosts || [], valid: true, error: '',
      isLatest, loadingVersions: false, versionList: [],
    }
  })
  batchPreview.value = preview
}

async function onSubmit() {
  if (validCount.value === 0 || submitting.value) return
  const validRows = batchPreview.value.filter(p => p.valid)
  if (isUpdate.value) {
    if (validRows.some(p => !p.version)) {
      ElMessage.warning('有些行还没选版本，先补齐')
      return
    }
  }

  // PROD 二次确认（rsync + update_version 都要确认；但 rsync 提示更轻量）
  if (envType.value === 'PROD') {
    let lines, title, btnText
    if (isUpdate.value) {
      lines = validRows.map(p => `· <b>${p.service}</b> → <code>${p.version}</code>`).join('<br>')
      title = '⚠ PROD 更新二次确认'
      btnText = '确认更新 PROD'
    } else {
      lines = validRows.map(p => `· <b>${p.service}</b>`).join('<br>')
      title = '⚠ PROD rsync 二次确认'
      btnText = '确认 rsync PROD'
    }
    const desc = isUpdate.value
      ? `即将批量更新 <b>${validRows.length}</b> 个模块到 <b>PROD</b>，影响 ${totalHosts.value} 台机器：`
      : `即将批量 rsync <b>${validRows.length}</b> 个 PROD 模块的源码（从 206 同步，不更新到目标机器）：`
    try {
      await ElMessageBox.confirm(
        `${desc}<br><br>${lines}`,
        title,
        { type: 'warning', dangerouslyUseHTMLString: true,
          confirmButtonText: btnText, confirmButtonClass: 'el-button--danger' })
    } catch { return }
  }

  submitting.value = true
  try {
    const payload = {
      vm_project_env_id: props.vmProjectEnv.id,
      action: isUpdate.value ? 'update_version' : 'rsync',
      services: validRows.map(p => isUpdate.value
        ? { service: p.service, version: p.version }
        : { service: p.service }),
    }
    const r = await vmDeploy(payload)
    deployments.startTracking(r.deployment_id, {
      action: isUpdate.value ? 'vm_update_version' : 'vm_rsync',
      envName: props.vmProjectEnv.name,
      envType: envType.value,
      modules: validRows.length,
      operator: auth.user?.username || '',
    })
    ElMessage.success(`已提交 ${validRows.length} 个服务 · 任务 #${r.deployment_id}，右上角看进度`)
    resetState()
  } catch (e) {
    if (e?.code === 40901 && Array.isArray(e?.data?.conflicts) && e.data.conflicts.length) {
      showConflictDialog(e.data.conflicts)
    } else {
      ElMessage.error('提交失败：' + (e?.response?.data?.message || e?.message || e))
    }
  } finally { submitting.value = false }
}

function fmtElapsed(startedAt) {
  if (!startedAt) return '—'
  const s = Math.max(0, Math.floor((Date.now() - new Date(startedAt).getTime()) / 1000))
  if (s < 60) return `${s}s`
  return `${Math.floor(s / 60)}m${s % 60}s`
}

const VM_ACTION_LABEL = {
  vm_rsync: 'VM rsync',
  vm_update_version: 'VM 更新',
}

function showConflictDialog(conflicts) {
  const lines = conflicts.map(c =>
    `<li style="margin:6px 0;">
       <code style="font-family:var(--mono,'Fira Code',monospace);color:#1f2937;">${c.service}</code>
       <div style="font-size:11.5px;color:#6b7280;margin-top:2px;">
         操作人: <b>${c.operator || '—'}</b>
         · 任务号: <b>#${c.deployment_id}</b>
         · 已运行: <b>${fmtElapsed(c.started_at)}</b>
         · 操作: ${VM_ACTION_LABEL[c.action] || c.action || '—'}
       </div>
     </li>`
  ).join('')
  const title = `⚠ 无法发布：${conflicts.length} 个服务有任务在跑`
  const body = `
    <div style="font-size:13px;color:#374151;margin-bottom:8px;">
      以下服务已有发布任务在执行中，请等它跑完再发：
    </div>
    <ul style="padding-left:18px;margin:0;list-style-type:disc;">${lines}</ul>
    <div style="margin-top:12px;font-size:11.5px;color:#9ca3af;">
      可以先取消勾选这些服务，把其它服务先发出去。
    </div>
  `
  ElMessageBox.alert(body, title, {
    dangerouslyUseHTMLString: true,
    confirmButtonText: '我知道了',
    customClass: 'vm-conflict-dialog',
  }).catch(() => {})
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
.row-k { width: 80px; font-size: 12.5px; color: var(--text-2); font-weight: 500; }

.batch-modes-row { display: flex; align-items: center; gap: 10px; }
.batch-modes { display: inline-flex; background: #fff; border: 1px solid var(--border); border-radius: 6px; overflow: hidden; }
.mode-btn {
  background: #fff; border: none; border-left: 1px solid var(--border);
  padding: 6px 14px; font: 500 12.5px var(--body); color: var(--text-2);
  cursor: pointer; transition: all .12s;
}
.mode-btn:first-child { border-left: none; }
.mode-btn:hover:not(.active) { color: var(--primary); }
.mode-btn.active { background: var(--primary); color: #fff; }

.auto-mode, .manual-mode { display: flex; flex-direction: column; gap: 10px; }

.batch-textarea {
  width: 100%; min-height: 120px;
  border: 1px solid var(--border); border-radius: 6px;
  padding: 10px 12px;
  font: 500 12.5px/1.7 var(--mono);
  background: #fff; color: var(--text);
  resize: vertical;
}
.batch-textarea:focus { outline: none; border-color: var(--primary); }

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

.hint { font-size: 11.5px; color: var(--text-3); }
.hint b { color: var(--text); font-family: var(--mono); font-weight: 600; }

.batch-table {
  width: 100%; border-collapse: collapse;
  font-size: 12.5px; margin-top: 4px;
  background: #fff; border: 1px solid var(--border-soft); border-radius: 6px; overflow: hidden;
}
.batch-table th {
  background: #f9fafb; text-align: left;
  padding: 8px 10px; border-bottom: 1px solid var(--border);
  color: var(--text-2); font-weight: 500;
  font-size: 11px; text-transform: uppercase; letter-spacing: .4px;
}
.batch-table td {
  padding: 7px 10px; border-bottom: 1px solid var(--border-soft);
  vertical-align: middle;
}
.batch-table tr:last-child td { border-bottom: none; }
.batch-table tr.invalid td { background: #fef2f2; }
.batch-table .mono { font-family: var(--mono); font-size: 12px; color: var(--text); }
.batch-table .hosts-cell { max-width: 260px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; color: var(--text-2); }
.err-text { color: var(--danger); font-size: 11.5px; }
.latest-tag {
  font-family: var(--mono); font-size: 10px;
  padding: 1px 6px; background: #fef3c7; color: #92400e;
  border-radius: 3px; margin-right: 4px;
}
.row-x {
  background: transparent; border: none; color: var(--text-3);
  font-size: 16px; cursor: pointer; padding: 0 4px;
}
.row-x:hover { color: var(--danger); }
</style>
