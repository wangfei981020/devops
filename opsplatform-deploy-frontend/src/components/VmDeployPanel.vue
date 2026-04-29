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
      <!-- 单服务 rsync 行（始终单选，rsync 不批量） -->
      <div class="row">
        <label>① 选服务（rsync 用）</label>
        <el-select v-model="rsyncService" filterable placeholder="选择 service" style="flex:1;">
          <el-option v-for="s in services" :key="s.id" :label="s.name" :value="s.name">
            <div style="display:flex;justify-content:space-between;">
              <span>{{ s.name }}</span>
              <span class="hint" style="margin-left:12px;">
                {{ s.hosts?.length || 0 }} 台 · {{ s.current_version || '未发布' }}
              </span>
            </div>
          </el-option>
        </el-select>
      </div>

      <div class="action-card rsync">
        <div class="ac-l">
          <div class="ac-title">📥 同步代码（rsync）</div>
          <div class="ac-desc">从代码服务器拉源码到 ansible 服务器，刷新版本列表。**不部署到目标 VM**。</div>
        </div>
        <button class="btn warn" @click="onRsync" :disabled="!rsyncService || submitting">
          {{ submitting ? '提交中...' : '执行 rsync' }}
        </button>
      </div>

      <!-- 批量部署主区 -->
      <div class="batch-section">
        <div class="batch-head">
          <div class="batch-title">🚀 批量部署到 VM (update_version)</div>
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
          <div class="row" style="padding-left:90px;">
            <el-checkbox v-model="autoUseLatest" @change="onAutoLatestToggle">
              全部用最新版（取消勾选可逐行选历史版本）
            </el-checkbox>
          </div>
        </div>

        <!-- 手动模式 -->
        <div v-if="mode === 'manual'" class="manual-mode">
          <div class="row">
            <label>① 输入清单</label>
            <textarea v-model="manualText" class="batch-textarea" rows="6"
              placeholder="每行一个：service[:version]，省略 version = 用最新&#10;G01_op_office:011994266913a15a7ab51c479129fd17d1dacf5c&#10;G01_anchor_web&#10;G01_xxx:5318b2c89abc"
              @blur="parseManual"></textarea>
          </div>
          <div class="row" style="padding-left:90px;">
            <button class="btn ghost sm" @click="parseManual">预览</button>
            <button class="btn ghost sm" @click="manualText = ''; manualParsed = []">清空</button>
            <span class="hint" style="margin-left:8px;">
              解析 <b>{{ manualParsed.length }}</b> 行
              <span v-if="manualErrors.length" style="color:var(--danger);">
                · <b>{{ manualErrors.length }}</b> 行错误
              </span>
            </span>
          </div>
        </div>

        <!-- 预览表（自动 / 手动 共用） -->
        <div v-if="batchPreview.length > 0" class="row" style="display:block;padding-left:0;">
          <label style="display:block; margin-left: 90px; font-size:11.5px; color:var(--text-2); margin-bottom:6px;">
            ② 部署清单预览（{{ batchPreview.length }} 个服务，{{ totalHosts }} 台目标主机）
          </label>
          <table class="batch-table">
            <thead>
              <tr>
                <th>服务</th>
                <th>版本</th>
                <th>目标主机</th>
                <th style="width:36px;"></th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="(p, idx) in batchPreview" :key="p.service" :class="{ invalid: !p.valid }">
                <td class="mono">{{ p.service }}</td>
                <td>
                  <span v-if="!p.valid" class="err-text">❌ {{ p.error }}</span>
                  <el-select v-else-if="mode === 'auto' && !autoUseLatest"
                    v-model="p.version" filterable size="small"
                    style="width:100%;"
                    :loading="p.loadingVersions"
                    @click="onPerRowVersionClick(p)">
                    <el-option v-for="(v, i) in (p.versionList || [])" :key="v"
                      :label="(i === 0 ? '⭐最新 · ' : '') + v" :value="v" />
                  </el-select>
                  <span v-else class="mono">
                    <span v-if="p.isLatest" class="latest-tag">⭐最新</span>
                    {{ p.version }}
                  </span>
                </td>
                <td class="mono hosts-cell">
                  <span v-if="p.hosts && p.hosts.length > 0">
                    {{ p.hosts.join(', ') }}
                  </span>
                  <span v-else class="hint">—</span>
                </td>
                <td>
                  <button v-if="mode === 'auto'" class="row-x" @click="removeFromBatch(idx)" title="从清单中移除">×</button>
                </td>
              </tr>
            </tbody>
          </table>
        </div>

        <!-- 批量部署按钮 -->
        <div class="action-card deploy" style="margin-top:12px;">
          <div class="ac-l">
            <div class="ac-title">🚀 部署到 VM</div>
            <div class="ac-desc">
              <span v-if="batchPreview.length === 0">先选服务（自动）或输入清单（手动）</span>
              <span v-else>
                共 <b>{{ validCount }}</b> 个服务 ·
                <b>{{ totalHosts }}</b> 台目标主机
                <span v-if="invalidCount > 0" style="color:var(--warning);">
                  · {{ invalidCount }} 行错误将跳过
                </span>
              </span>
            </div>
          </div>
          <button :class="['btn', envType === 'PROD' ? 'danger' : 'success']"
            @click="onBatchDeploy"
            :disabled="validCount === 0 || submitting">
            <span v-if="submitting">提交中...</span>
            <span v-else-if="envType === 'PROD'">部署 PROD · 需二次确认</span>
            <span v-else>部署 {{ validCount }} 个到 {{ envType }}</span>
          </button>
        </div>
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
})

const deployments = useDeploymentsStore()
const auth = useAuthStore()

const services = ref([])              // [{id, name, hosts:[], current_version}]
const scanning = ref(false)
const submitting = ref(false)

// rsync 用单选
const rsyncService = ref('')

// 批量模式：'auto' | 'manual'
const mode = ref('auto')
// 自动模式状态
const autoSelected = ref([])          // [serviceName, ...]
const autoUseLatest = ref(true)
// 手动模式状态
const manualText = ref('')
const manualParsed = ref([])          // [{service, version}]，省略 version 时为 ''
const manualErrors = ref([])          // [{line, raw, err}]

// 每个服务的版本列表缓存（避免 dropdown 每次打开重拉）
const versionCache = ref({})          // service → [v1, v2, v3...]

const envType = computed(() => props.vmProjectEnv?.env_type || 'UAT')

// batchPreview 是 自动/手动 共用的预览数据
//   {service, version, hosts, valid, error, isLatest, loadingVersions, versionList}
const batchPreview = ref([])

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
    // 切换 env 时清掉旧选择
    autoSelected.value = []
    manualText.value = ''
    manualParsed.value = []
    batchPreview.value = []
    rsyncService.value = ''
  }
}, { immediate: true })

// 自动模式：用户多选服务后，同步生成预览行
watch(autoSelected, (newList) => {
  if (mode.value !== 'auto') return
  // 保留已存在的 row（带 version 选择），新增的 row 用 latest
  const oldMap = new Map(batchPreview.value.map(p => [p.service, p]))
  batchPreview.value = newList.map(name => {
    const old = oldMap.get(name)
    if (old) return old
    const svc = services.value.find(s => s.name === name)
    return {
      service: name,
      version: '',           // 等 latest 加载或 dropdown 选完填
      hosts: svc?.hosts || [],
      valid: true,
      error: '',
      isLatest: autoUseLatest.value,
      loadingVersions: false,
      versionList: [],
    }
  })
  if (autoUseLatest.value) refreshLatestForAll()
}, { deep: false })

watch(mode, () => {
  // 切 tab 重置预览
  batchPreview.value = []
  if (mode.value === 'manual') {
    manualParsed.value = []
    manualErrors.value = []
  }
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

// 给一个服务加载版本列表（带缓存），返回 versions 数组
async function loadVersionsFor(serviceName) {
  if (versionCache.value[serviceName]) return versionCache.value[serviceName]
  const svc = services.value.find(s => s.name === serviceName)
  if (!svc) return []
  try {
    const r = await listVmServiceVersions(svc.id)
    const versions = r.versions || []
    versionCache.value[serviceName] = versions
    return versions
  } catch (e) {
    ElMessage.error(`${serviceName} 版本列表失败：${e?.response?.data?.message || e.message}`)
    return []
  }
}

// 全部用最新版：批量拉所有 row 的最新版填进去
async function refreshLatestForAll() {
  await Promise.all(batchPreview.value.map(async (p) => {
    if (!p.valid) return
    const versions = await loadVersionsFor(p.service)
    if (versions.length > 0) {
      p.version = versions[0]
      p.isLatest = true
    }
  }))
}

function onAutoLatestToggle(checked) {
  if (checked) {
    refreshLatestForAll()
  } else {
    // 取消"全部最新"→ 让每行可选历史版本，自动给每行预拉版本列表
    batchPreview.value.forEach(p => {
      p.isLatest = false
      if (p.versionList.length === 0) {
        loadVersionsFor(p.service).then(vs => { p.versionList = vs })
      }
    })
  }
}

async function onPerRowVersionClick(p) {
  if (p.versionList.length > 0) return
  p.loadingVersions = true
  p.versionList = await loadVersionsFor(p.service)
  p.loadingVersions = false
}

function removeFromBatch(idx) {
  const removed = batchPreview.value[idx]
  batchPreview.value.splice(idx, 1)
  // 同步从 autoSelected 移除（让多选 chip 也消失）
  if (mode.value === 'auto') {
    autoSelected.value = autoSelected.value.filter(s => s !== removed.service)
  }
}

// 手动模式：解析 textarea
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

  // 校验每个 service 是否存在 + 加载版本（缺省时取最新）
  const preview = []
  for (const p of parsed) {
    const svc = services.value.find(s => s.name === p.service)
    if (!svc) {
      preview.push({
        service: p.service, version: p.version || '—',
        hosts: [], valid: false,
        error: '服务不存在（先点"同步服务列表"）',
        isLatest: false, loadingVersions: false, versionList: [],
      })
      continue
    }
    let version = p.version
    let isLatest = false
    if (!version) {
      // 没指定版本 → 拉最新
      const versions = await loadVersionsFor(p.service)
      if (versions.length === 0) {
        preview.push({
          service: p.service, version: '—',
          hosts: svc.hosts || [], valid: false,
          error: '版本列表为空（先 rsync 同步代码）',
          isLatest: false, loadingVersions: false, versionList: versions,
        })
        continue
      }
      version = versions[0]
      isLatest = true
    }
    preview.push({
      service: p.service, version,
      hosts: svc.hosts || [], valid: true, error: '',
      isLatest, loadingVersions: false, versionList: [],
    })
  }
  batchPreview.value = preview
}

async function onRsync() {
  if (!rsyncService.value || submitting.value) return
  submitting.value = true
  try {
    const r = await vmDeploy({
      vm_project_env_id: props.vmProjectEnv.id,
      action: 'rsync',
      services: [{ service: rsyncService.value }],
    })
    deployments.startTracking(r.deployment_id, {
      action: 'vm_rsync',
      envName: props.vmProjectEnv.name,
      envType: envType.value,
      modules: 1,
      operator: auth.user?.username || '',
    })
    ElMessage.success(`已提交 rsync · 任务 #${r.deployment_id}，右上角看进度`)
  } catch (e) {
    ElMessage.error('提交失败：' + (e?.response?.data?.message || e.message))
  } finally { submitting.value = false }
}

async function onBatchDeploy() {
  if (validCount.value === 0 || submitting.value) return
  const validRows = batchPreview.value.filter(p => p.valid && p.version)
  if (validRows.length !== validCount.value) {
    ElMessage.warning('有些行还没选版本，先补齐')
    return
  }

  // PROD 二次确认（带详细列表）
  if (envType.value === 'PROD') {
    const lines = validRows.map(p => `· <b>${p.service}</b> → <code>${p.version.slice(0, 12)}...</code>`).join('<br>')
    try {
      await ElMessageBox.confirm(
        `即将批量部署 <b>${validRows.length}</b> 个服务到 <b>PROD</b>，影响 ${totalHosts.value} 台机器：<br><br>${lines}`,
        '⚠ PROD 二次确认',
        { type: 'warning', dangerouslyUseHTMLString: true,
          confirmButtonText: '确认部署 PROD', confirmButtonClass: 'el-button--danger' })
    } catch { return }
  }

  submitting.value = true
  try {
    const r = await vmDeploy({
      vm_project_env_id: props.vmProjectEnv.id,
      action: 'update_version',
      services: validRows.map(p => ({ service: p.service, version: p.version })),
    })
    deployments.startTracking(r.deployment_id, {
      action: 'vm_update_version',
      envName: props.vmProjectEnv.name,
      envType: envType.value,
      modules: validRows.length,
      operator: auth.user?.username || '',
    })
    ElMessage.success(`已提交 ${validRows.length} 个服务 · 任务 #${r.deployment_id}，右上角看进度`)
    // 清空选择，避免误重复提交
    if (mode.value === 'auto') {
      autoSelected.value = []
    } else {
      manualText.value = ''
      manualParsed.value = []
    }
    batchPreview.value = []
  } catch (e) {
    ElMessage.error('提交失败：' + (e?.response?.data?.message || e.message))
  } finally { submitting.value = false }
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

/* 批量区 */
.batch-section {
  border: 1px solid var(--border-soft);
  border-radius: 8px;
  padding: 12px 14px;
  background: #fcfcfd;
  margin-top: 4px;
}
.batch-head { display: flex; align-items: center; justify-content: space-between; margin-bottom: 12px; }
.batch-title { font-weight: 600; font-size: 13px; color: var(--text); }
.batch-modes { display: inline-flex; background: #fff; border: 1px solid var(--border); border-radius: 6px; overflow: hidden; }
.mode-btn {
  background: #fff; border: none; border-left: 1px solid var(--border);
  padding: 5px 12px; font: 500 12px var(--body); color: var(--text-2);
  cursor: pointer; transition: all .12s;
}
.mode-btn:first-child { border-left: none; }
.mode-btn:hover:not(.active) { color: var(--primary); }
.mode-btn.active { background: var(--primary); color: #fff; }

.auto-mode, .manual-mode { display: flex; flex-direction: column; gap: 10px; margin-bottom: 12px; }
.batch-textarea {
  width: 100%; min-height: 120px;
  border: 1px solid var(--border); border-radius: 6px;
  padding: 10px 12px;
  font: 500 12.5px/1.7 var(--mono);
  background: #fff; color: var(--text);
  resize: vertical;
}
.batch-textarea:focus { outline: none; border-color: var(--primary); }

/* 预览表 */
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
.batch-table .hosts-cell { max-width: 200px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; color: var(--text-2); }
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
