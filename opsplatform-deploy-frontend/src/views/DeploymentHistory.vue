<template>
  <div class="dh">
    <!-- KPI 行 -->
    <div class="kpis">
      <div class="kpi" v-for="k in kpis" :key="k.label">
        <div class="k-lbl">{{ k.label }}</div>
        <div class="k-val mono" :style="{color: k.color}">{{ k.value }}</div>
      </div>
    </div>

    <!-- 筛选 -->
    <div class="filter-bar">
      <div class="f-item">
        <label>项目</label>
        <select v-model="draft.project" @change="onProjectChange" class="sel">
          <option value="">全部</option>
          <option v-for="p in projects" :key="p" :value="p">{{ p }}</option>
        </select>
      </div>
      <div class="f-item">
        <label>环境</label>
        <select v-model="draft.env_type" class="sel" :disabled="!draft.project">
          <option value="">全部</option>
          <option v-for="e in envsForProject" :key="e" :value="e">{{ envLabel(e) }}</option>
        </select>
      </div>
      <div class="f-item">
        <label>操作类型</label>
        <select v-model="draft.action" class="sel">
          <option value="">全部</option>
          <option value="update_image">更新镜像</option>
          <option value="restart">重启服务</option>
          <option value="rollback">回滚</option>
        </select>
      </div>
      <div class="f-item">
        <label>状态</label>
        <select v-model="draft.status" class="sel">
          <option value="">全部</option>
          <option value="success">成功</option>
          <option value="partial">部分成功</option>
          <option value="failed">失败</option>
          <option value="pending">进行中</option>
          <option value="no_change">无变化</option>
        </select>
      </div>
      <div class="f-item">
        <label>操作人</label>
        <input v-model="draft.operator" class="inp" placeholder="模糊匹配" @keyup.enter="doSearch" />
      </div>
      <div class="f-item">
        <label>模块</label>
        <input v-model="draft.module" class="inp" placeholder="模糊匹配 user-client" @keyup.enter="doSearch" />
      </div>
      <div class="f-item wide">
        <label>时间范围</label>
        <div class="date-range">
          <input type="datetime-local" v-model="draft.time_from" class="inp" />
          <span class="sep">至</span>
          <input type="datetime-local" v-model="draft.time_to" class="inp" />
        </div>
      </div>
      <div class="f-actions">
        <button class="btn-primary" @click="doSearch">
          <el-icon><Search /></el-icon>查询
        </button>
        <button class="btn-ghost" @click="doReset">重置</button>
      </div>
    </div>

    <!-- 表格 -->
    <div class="tbl-wrap">
      <div v-if="loading" class="loading">加载中...</div>
      <div v-else-if="!list.length" class="empty">
        <div class="em-t">没有发布记录</div>
        <div class="em-d">调整筛选条件，或点「查询」重新获取</div>
      </div>
      <table v-else class="tbl">
        <thead>
          <tr>
            <th style="width:28px"></th>
            <th style="width:150px">时间</th>
            <th style="width:140px">项目环境</th>
            <th style="width:120px">操作</th>
            <th>涉及模块</th>
            <th style="width:100px">状态</th>
            <th style="width:110px">提交</th>
            <th style="width:90px">操作人</th>
          </tr>
        </thead>
        <tbody>
          <template v-for="(row, i) in list" :key="row.id">
            <tr :class="['data-row', {opened: expanded[i]}]" @click="toggle(i)">
              <td class="chev-cell">
                <el-icon :class="['chev', {on: expanded[i]}]"><ArrowRight /></el-icon>
              </td>
              <td class="mono">{{ fmtTime(row.created_at) }}</td>
              <td class="mono">
                {{ envName(row) }}
                <span :class="'env-chip ' + envType(row)" style="margin-left:4px">{{ envType(row).toUpperCase() }}</span>
                <span :class="'kind-mini ' + (isVmAction(row.action) ? 'vm' : 'k8s')" style="margin-left:4px">{{ isVmAction(row.action) ? 'VM' : 'K8s' }}</span>
              </td>
              <td>
                <span :class="'action-tag ' + row.action">{{ actionLabel(row.action) }}</span>
              </td>
              <td>
                <span v-for="(m, mi) in (row.module_names || []).slice(0, 3)" :key="mi" class="mod-chip">{{ m }}</span>
                <span v-if="(row.module_names || []).length > 3" class="mod-chip more">+{{ row.module_names.length - 3 }}</span>
                <span v-if="!row.module_names?.length" class="muted">—</span>
              </td>
              <td>
                <span :class="'status-tag ' + row.status">{{ statusLabel(row.status) }}</span>
              </td>
              <td>
                <a v-if="row.git_commit_url" :href="row.git_commit_url" target="_blank" class="commit mono" @click.stop>
                  {{ row.git_commit }}
                </a>
                <span v-else class="muted mono">—</span>
              </td>
              <td class="mono">{{ row.operator }}</td>
            </tr>
            <tr v-if="expanded[i]" class="detail-row">
              <td :colspan="8">
                <div class="detail-wrap">

                  <!-- VM 行专属：服务表（含版本+主机+per-service 日志按钮）；不显示 K8s 那套 Tag/同步结果 -->
                  <template v-if="isVmAction(row.action)">
                    <div class="section">
                      <div class="sec-lbl">
                        VM 部署明细
                        <span class="sec-sub">
                          {{ (row.module_names || []).length }} 个服务
                          <span v-if="row.duration_sec != null">· 总耗时 {{ row.duration_sec }}s</span>
                          · 操作 {{ actionLabel(row.action) }}
                        </span>
                      </div>
                      <table class="sub-tbl vm-tbl">
                        <thead>
                          <tr>
                            <th>服务</th>
                            <th>版本</th>
                            <th>状态</th>
                            <th>日志</th>
                          </tr>
                        </thead>
                        <tbody>
                          <tr v-for="svc in vmDetailRows(row)" :key="svc.service">
                            <td class="mono">{{ svc.service }}</td>
                            <td class="mono">
                              <span v-if="svc.version">{{ svc.version }}</span>
                              <span v-else class="muted">—</span>
                            </td>
                            <td>
                              <span :class="'st-pill ' + (svc.status || 'unknown')">{{ vmServiceStatusLabel(svc.status) }}</span>
                            </td>
                            <td>
                              <button class="view-logs-btn" @click.stop="openVmLog(row.id, row.status, svc.service)">
                                {{ row.status === 'pending' ? '实时' : '查看' }}
                              </button>
                            </td>
                          </tr>
                          <tr v-if="vmDetailRows(row).length === 0">
                            <td colspan="4" class="muted" style="text-align:center">无</td>
                          </tr>
                        </tbody>
                      </table>
                    </div>
                  </template>

                  <div v-else-if="row.action !== 'restart'" class="section">
                    <div class="sec-lbl">Tag 变更明细</div>
                    <table class="sub-tbl">
                      <thead><tr><th>模块</th><th>原 tag</th><th>新 tag</th></tr></thead>
                      <tbody>
                        <tr v-for="ch in (row.changes || [])" :key="ch.module">
                          <td class="mono">{{ ch.module }}</td>
                          <td class="mono tag-from">{{ ch.from_tag }}</td>
                          <td class="mono tag-to">{{ ch.to_tag }}</td>
                        </tr>
                        <tr v-if="!row.changes?.length"><td colspan="3" class="muted" style="text-align:center">无</td></tr>
                      </tbody>
                    </table>
                  </div>
                  <div v-else class="section">
                    <div class="sec-lbl">重启模块</div>
                    <div class="restart-mods">
                      <span v-for="m in (row.module_names || [])" :key="m" class="mod-chip">{{ m }}</span>
                    </div>
                  </div>

                  <div v-if="!isVmAction(row.action)" class="section">
                    <div class="sec-lbl">
                      同步结果
                      <span class="sec-sub">
                        模块数 <b>{{ row.argocd_results?.length || 0 }}</b>
                        <span v-if="row.duration_sec != null"> · 总耗时 <b>{{ row.duration_sec }}s</b></span>
                      </span>
                    </div>
                    <table class="sub-tbl">
                      <thead><tr><th>应用</th><th>同步</th><th>健康</th><th>单模块耗时</th><th>消息</th><th style="width:100px;">操作</th></tr></thead>
                      <tbody>
                        <tr v-for="r in (row.argocd_results || [])" :key="r.app">
                          <td class="mono">{{ r.app }}</td>
                          <td :class="'sync-' + (r.sync_status || '').toLowerCase()">{{ r.sync_status || '—' }}</td>
                          <td :class="'health-' + (r.health || '').toLowerCase()">{{ r.health || '—' }}</td>
                          <td class="mono">{{ liveDuration(r, row) }}s</td>
                          <td :class="['msg', msgClass(r)]">{{ msgText(r) }}</td>
                          <td>
                            <button v-if="canShowLogs(r)" class="view-logs-btn" @click="openLogsModal(row.id, r.app)">
                              查看日志
                            </button>
                            <span v-else class="muted">—</span>
                          </td>
                        </tr>
                        <tr v-if="!row.argocd_results?.length"><td colspan="6" class="muted" style="text-align:center">无</td></tr>
                      </tbody>
                    </table>
                  </div>

                  <div v-if="row.error_msg" class="section">
                    <div class="sec-lbl">错误信息</div>
                    <pre class="err-msg">{{ row.error_msg }}</pre>
                  </div>

                  <div class="section-actions">
                    <button
                      v-if="row.status === 'pending' && (auth.isAdmin || row.operator === auth.user?.username)"
                      class="btn-cancel sm"
                      :disabled="cancelingIds.has(row.id)"
                      @click.stop="onCancelRow(row)">
                      {{ cancelingIds.has(row.id) ? '取消中…' : '取消等待' }}
                    </button>
                    <button
                      v-if="row.action !== 'restart' && !isVmAction(row.action) && ['success','partial','failed'].includes(row.status) && (auth.isAdmin || auth.hasButton('rollback'))"
                      class="btn-primary sm"
                      @click.stop="onRollback(row)">
                      回滚此次发布
                    </button>
                  </div>
                </div>
              </td>
            </tr>
          </template>
        </tbody>
      </table>

      <div class="pager" v-if="list.length">
        <div class="pg-info">共 <b>{{ total }}</b> 条</div>
        <div class="pg-ctrl">
          <button :disabled="page <= 1" @click="page--; doSearch()">‹</button>
          <span>第 <b>{{ page }}</b> 页</span>
          <button :disabled="page * pageSize >= total" @click="page++; doSearch()">›</button>
          <select v-model="pageSize" @change="page = 1; doSearch()" class="sel-sm">
            <option :value="10">10 条/页</option>
            <option :value="20">20 条/页</option>
            <option :value="50">50 条/页</option>
            <option :value="100">100 条/页</option>
          </select>
        </div>
      </div>
    </div>

    <RollbackDialog v-model="rbVis" :deployment-id="rbID" @done="doSearch" />
    <PodLogsModal :show="logsModal.show" :deployment-id="logsModal.deploymentId"
      :app="logsModal.app" @close="logsModal.show = false" />

    <!-- VM ansible 日志弹窗：pending 走 SSE 实时流，终态走归档 -->
    <el-dialog v-model="vmLogDlg.vis"
      :title="`Ansible 日志 · #${vmLogDlg.deploymentId}`"
      width="900px"
      :close-on-click-modal="false"
      custom-class="vm-log-dialog">
      <div class="vm-log-status-bar" v-if="vmLogDlg.isLive">
        <span class="vm-log-live-dot"></span>
        <span>实时流（SSE）· 任务进行中，新日志会自动追加</span>
      </div>
      <div class="vm-log-status-bar done" v-else-if="vmLogDlg.deploymentId && vmLogDlg.text && !vmLogDlg.error && vmLogDlg.status !== 'pending'">
        <span>✅ 任务已完成</span>
      </div>
      <div v-if="vmLogDlg.loading && !vmLogDlg.text" class="vm-log-loading">加载中…</div>
      <div v-else-if="vmLogDlg.error" class="vm-log-error">
        <el-icon><Warning /></el-icon> {{ vmLogDlg.error }}
      </div>
      <pre v-else ref="vmLogPre" class="vm-log-pre">{{ vmLogDlg.text || '（空）' }}</pre>
      <template #footer>
        <span class="vm-log-meta">
          <span v-if="vmLogDlg.size">大小 <b>{{ formatBytes(vmLogDlg.size) }}</b></span>
          <span v-if="vmLogDlg.status"> · 状态 <b>{{ statusLabel(vmLogDlg.status) }}</b></span>
        </span>
        <el-button @click="copyVmLog" :disabled="!vmLogDlg.text">复制全文</el-button>
        <el-button @click="vmLogDlg.vis = false">关闭</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { ref, reactive, computed, onMounted, onUnmounted, watch, nextTick } from 'vue'
import PodLogsModal from '../components/PodLogsModal.vue'
import { useRoute, useRouter } from 'vue-router'
import dayjs from 'dayjs'
import { Search, ArrowRight, Warning } from '@element-plus/icons-vue'
import { listDeployments, listProjectEnvs, listVmProjectEnvs, getDeployment, cancelDeployment, fetchVmArchivedLog } from '../api'
import { ElMessage, ElMessageBox } from 'element-plus'
import RollbackDialog from '../components/RollbackDialog.vue'
import { useAuthStore } from '../stores/auth'

const route = useRoute()
const router = useRouter()
const auth = useAuthStore()

const list = ref([])
const envs = ref([])
const total = ref(0)
const page = ref(1)
const pageSize = ref(10)
const loading = ref(false)
const expanded = ref({})
const rbVis = ref(false)
const rbID = ref(null)

// 草稿：用户正在编辑但未触发查询
const blankFilters = () => ({
  project: '', env_type: '',
  action: '', status: '',
  operator: '',
  module: '',
  time_from: '', time_to: ''
})
const draft = reactive(blankFilters())

const projects = computed(() => {
  const set = new Set()
  envs.value.forEach(e => {
    const suffix = '-' + e.env_type
    const p = e.name.endsWith(suffix) ? e.name.slice(0, -suffix.length) : e.name
    set.add(p)
  })
  return [...set].sort()
})
const envsForProject = computed(() => {
  if (!draft.project) return []
  const set = new Set()
  envs.value.forEach(e => {
    const suffix = '-' + e.env_type
    const p = e.name.endsWith(suffix) ? e.name.slice(0, -suffix.length) : e.name
    if (p === draft.project) set.add(e.env_type)
  })
  return [...set]
})
function onProjectChange() { draft.env_type = '' }

function envLabel(v) { return { uat: 'UAT', prod: 'PROD', lpt: 'LPT' }[v.toLowerCase()] || v }
// envs 是 K8s + VM 合并的扁平数组，每条带 _kind 和原始 env_type
// VM env_type 是 UAT/LPT/PROD 大写，K8s 是 uat/prod 小写。envType() 统一返小写做 chip class
// K8s 和 VM env id 在不同表，可能撞号 → 必须根据 row.action 是否 vm_ 前缀来过滤 _kind
function envForRow(row) {
  if (!row) return null
  const kind = isVmAction(row.action) ? 'vm' : 'k8s'
  return envs.value.find(e => e._kind === kind && e.id === row.project_env_id)
}
function envName(row) { return envForRow(row)?.name || row?.project_env_id || '' }
function envType(row) { return (envForRow(row)?.env_type || 'uat').toLowerCase() }
function fmtTime(s) { return dayjs(s).format('YYYY-MM-DD HH:mm') }
function isVmAction(a) { return typeof a === 'string' && a.startsWith('vm_') }
function actionLabel(a) {
  return {
    update_image: '更新镜像', restart: '重启服务', rollback: '回滚',
    vm_rsync: 'VM rsync', vm_update_version: 'VM 更新',
  }[a] || a
}

// vmDetailRows 从 row 拼出 VM 明细行：
//   优先 row.vm_task_map（含每个 service 的 status/log_object_key 等真实状态）；
//   否则回退到 row.changes（仅有 module + to_tag），status 用 row.status 兜底显示
//   再回退到 module_names（极老数据，单服务无版本）
function vmDetailRows(row) {
  if (Array.isArray(row.vm_task_map) && row.vm_task_map.length) {
    return row.vm_task_map.map(t => ({
      service: t.service,
      version: t.version || '',
      status: t.status || row.status,
    }))
  }
  if (Array.isArray(row.changes) && row.changes.length) {
    return row.changes.map(c => ({
      service: c.module,
      version: c.to_tag || '',
      status: row.status,
    }))
  }
  return (row.module_names || []).map(m => ({
    service: m, version: '', status: row.status,
  }))
}
function vmServiceStatusLabel(s) {
  return {
    pending: '执行中', running: '执行中',
    success: '成功', failed: '失败', canceled: '已取消',
  }[s] || s || '—'
}
function statusLabel(s) {
  return { success: '成功', partial: '部分成功', failed: '失败', pending: '进行中', no_change: '无变化', canceled: '已取消' }[s] || s
}

const kpis = ref([
  { label: '当前页数', value: '0' },
  { label: '成功率', value: '—', color: 'var(--success)' },
  { label: '重启次数', value: '0' },
  { label: '平均耗时', value: '0s' }
])
function computeKPIs() {
  const n = list.value.length
  const ok = list.value.filter(d => d.status === 'success').length
  const restart = list.value.filter(d => d.action === 'restart').length
  const avg = n ? Math.round(list.value.reduce((a, b) => a + (b.duration_sec || 0), 0) / n) : 0
  kpis.value = [
    { label: '当前页数', value: n.toString() },
    { label: '成功率', value: n ? Math.round(ok * 100 / n) + '%' : '—', color: 'var(--success)' },
    { label: '重启次数', value: restart.toString() },
    { label: '平均耗时', value: avg + 's' }
  ]
}

async function doSearch() {
  loading.value = true
  try {
    const params = { page: page.value, page_size: pageSize.value }
    if (draft.project) params.project = draft.project
    if (draft.env_type) params.env_type = draft.env_type
    if (draft.action) params.action = draft.action
    if (draft.status) params.status = draft.status
    if (draft.operator) params.operator = draft.operator
    if (draft.module) params.module = draft.module.trim()
    if (draft.time_from) params.time_from = draft.time_from.replace('T', ' ') + ':00'
    if (draft.time_to) params.time_to = draft.time_to.replace('T', ' ') + ':59'
    const r = await listDeployments(params)
    list.value = r.list || []
    total.value = r.total || 0
    computeKPIs()
    // 列表里若有 pending 项，启动轮询自动刷新到终态；否则关掉轮询
    if (hasPending()) startPolling()
    else stopPolling()
  } finally { loading.value = false }
}

// ---- 单模块耗时秒表插值 ----
// 后端在每条 argocd_results 上带 last_polled_at（写 DB 时填入 server time），前端
// 用它做插值锚点：刷新页面后仍能根据 (now - last_polled_at) 继续累积秒数，避免
// "刷新后从 180 重新涨"的视觉 bug。
//
// 终态判定优先级：
//   1) 父 deployment.status 是终态（success/failed/partial/no_change）→ 全部行用 duration_sec
//   2) 单行 sync/health 是终态（Healthy/Degraded/Failed/Timeout）→ 该行用 duration_sec
//   3) 其它 → 用 last_polled_at 插值
const tick = ref(0)
let tickTimer = null

function isParentTerminal(row) {
  const s = (row?.status || '').toLowerCase()
  return s === 'success' || s === 'failed' || s === 'partial' || s === 'no_change'
}
function isTerminal(r) {
  const h = (r.health || '').toLowerCase()
  const s = (r.sync_status || '').toLowerCase()
  return h === 'healthy' || h === 'degraded' ||
    s === 'failed' || s === 'timeout' || s === 'canceled'
}
// ---- 消息列 color-coded 辅助 ----
function msgClass(r) {
  const s = (r.sync_status || '').toLowerCase()
  const h = (r.health || '').toLowerCase()
  if (h === 'healthy' && s === 'synced') return 'ok'
  if (s === 'failed' || s === 'timeout' || h === 'degraded') return 'fail'
  return ''
}
function msgText(r) {
  const cls = msgClass(r)
  if (cls === 'ok') return '✓ 成功'
  if (cls === 'fail') {
    // 去掉冗余的「失败 ·」前缀（颜色 + ✗ 已经说明），保留具体原因
    const m = (r.msg || '').replace(/^失败\s*·\s*/, '')
    return m ? `✗ ${m}` : '✗ 失败'
  }
  return r.msg || '—'
}

// ---- 查看日志 modal ----
const logsModal = reactive({ show: false, deploymentId: 0, app: '' })
function canShowLogs(r) {
  // 只对失败行显示按钮（同步=Failed/Timeout 或 健康=Degraded）
  const s = (r.sync_status || '').toLowerCase()
  const h = (r.health || '').toLowerCase()
  return s === 'failed' || s === 'timeout' || h === 'degraded'
}
function openLogsModal(deploymentId, app) {
  logsModal.deploymentId = deploymentId
  logsModal.app = app
  logsModal.show = true
}

// ---- VM ansible 日志弹窗 ----
//   pending 状态 → 走 /vm-logs?stream=true&service=... 实时 SSE
//   终态 → 走 /vm-archived-log?service=... 拿 MinIO 归档
//   service 参数批量场景必填（前端传哪个服务的日志）；单服务场景传也兼容
const vmLogDlg = reactive({
  vis: false, deploymentId: 0, status: '', service: '',
  text: '', size: 0,
  loading: false, error: '',
  isLive: false,
})
const vmLogPre = ref(null)
let vmLogAbort = null

async function openVmLog(depID, status, service = '') {
  closeVmLog()
  Object.assign(vmLogDlg, {
    vis: true, deploymentId: depID, status: status || '', service,
    text: '', size: 0, loading: true, error: '',
    isLive: status === 'pending',
  })
  if (status === 'pending') {
    streamVmLog(depID, service)
  } else {
    try {
      const r = await fetchVmArchivedLog(depID, service)
      vmLogDlg.text = r.text
      vmLogDlg.size = r.size
      vmLogDlg.status = r.status || status
    } catch (e) {
      vmLogDlg.error = e.message || '加载失败'
    } finally {
      vmLogDlg.loading = false
      nextTick(() => { if (vmLogPre.value) vmLogPre.value.scrollTop = vmLogPre.value.scrollHeight })
    }
  }
}

// streamVmLog 用 fetch+ReadableStream 拉 SSE（EventSource 不能带 Authorization）
async function streamVmLog(depID, service) {
  vmLogAbort = new AbortController()
  const token = localStorage.getItem('deploy_token') || ''
  try {
    const sp = new URLSearchParams({ stream: 'true', since: '0' })
    if (service) sp.set('service', service)
    const resp = await fetch(`/api/deployments/${depID}/vm-logs?` + sp.toString(), {
      headers: { Authorization: 'Bearer ' + token },
      signal: vmLogAbort.signal,
    })
    vmLogDlg.loading = false
    if (!resp.ok || !resp.body) {
      vmLogDlg.error = `HTTP ${resp.status}`
      return
    }
    const reader = resp.body.getReader()
    const dec = new TextDecoder()
    while (true) {
      const { value, done } = await reader.read()
      if (done) {
        // 后端 SSE 流结束 = 任务终态。banner 切「✅ 任务已完成」
        vmLogDlg.isLive = false
        break
      }
      const chunk = dec.decode(value)
      // SSE 解析：每行 'data: xxx' → 拼回正文
      for (const line of chunk.split('\n')) {
        if (line.startsWith('data: ')) {
          vmLogDlg.text += line.slice(6) + '\n'
        }
      }
      vmLogDlg.size = vmLogDlg.text.length
      nextTick(() => { if (vmLogPre.value) vmLogPre.value.scrollTop = vmLogPre.value.scrollHeight })
    }
  } catch (e) {
    if (e.name !== 'AbortError') {
      vmLogDlg.error = '[stream error] ' + e.message
    }
  } finally {
    vmLogDlg.loading = false
  }
}

function closeVmLog() {
  if (vmLogAbort) { vmLogAbort.abort(); vmLogAbort = null }
}

// 弹窗关闭 → 中断 SSE
watch(() => vmLogDlg.vis, (v) => { if (!v) closeVmLog() })

async function copyVmLog() {
  try {
    await navigator.clipboard.writeText(vmLogDlg.text || '')
    ElMessage.success('已复制到剪贴板')
  } catch { ElMessage.error('复制失败，请手动选中') }
}
function formatBytes(n) {
  if (n < 1024) return n + ' B'
  if (n < 1024 * 1024) return (n / 1024).toFixed(1) + ' KB'
  return (n / 1024 / 1024).toFixed(2) + ' MB'
}

function liveDuration(r, row) {
  void tick.value // 读 tick 触发响应式
  if (isParentTerminal(row) || isTerminal(r)) return r.duration_sec ?? 0
  // 用后端 last_polled_at 做锚点，刷新页面后也能继续累积
  const base = r.duration_sec ?? 0
  const polledStr = r.last_polled_at
  if (!polledStr) return base
  const polledMs = new Date(polledStr).getTime()
  if (isNaN(polledMs)) return base
  const delta = Math.max(0, Math.floor((Date.now() - polledMs) / 1000))
  return base + delta
}

// ---- 轮询 pending 项 ----
// 不重新拉整个列表，只对每条 pending 单独查 /deployments/:id，避免页码抖动和 KPI 波动
const POLL_INTERVAL_MS = 5000
let pollTimer = null
function hasPending() {
  return list.value.some(d => d.status === 'pending')
}
async function pollPending() {
  const ids = list.value.filter(d => d.status === 'pending').map(d => d.id)
  if (!ids.length) { stopPolling(); return }
  const results = await Promise.allSettled(ids.map(id => getDeployment(id)))
  let statusChanged = false
  results.forEach((r, i) => {
    if (r.status !== 'fulfilled' || !r.value) return
    const id = ids[i]
    const idx = list.value.findIndex(d => d.id === id)
    if (idx < 0) return
    // 无条件替换整条 — 即使 status 仍是 pending，argocd_results/duration_sec 也可能在变
    // （后端在 pending 期间每 5s 推一次中间态的 argocd_results JSON）
    if (list.value[idx].status !== r.value.status) statusChanged = true
    list.value[idx] = r.value
  })
  if (statusChanged) computeKPIs()
  if (!hasPending()) stopPolling()
}
function startPolling() {
  if (pollTimer) return
  pollTimer = setInterval(pollPending, POLL_INTERVAL_MS)
}
function stopPolling() {
  if (pollTimer) { clearInterval(pollTimer); pollTimer = null }
}
function doReset() {
  Object.assign(draft, blankFilters())
  page.value = 1
  doSearch()
}

function toggle(i) { expanded.value[i] = !expanded.value[i] }

// 点回滚 → 跳转部署控制台，URL 带 rollback_from=<depID>
// 部署控制台会自动把 changes 预填进 textarea，允许用户编辑/删行后提交
function onRollback(row) {
  router.push({ path: '/deploy', query: { rollback_from: String(row.id) } })
}

// 取消等待：仅 status=pending 的发布可取消
//   K8s：git 已 push，后台同步会继续；只是不再等待
//   VM：会真的发 SIGTERM 杀 ansible-playbook 进程，可能让目标机器留半部署状态
const cancelingIds = reactive(new Set())
async function onCancelRow(row) {
  if (cancelingIds.has(row.id)) return
  // 按 K8s / VM 给不同提示
  const isVm = isVmAction(row.action)
  const message = isVm
    ? 'VM 任务取消会向 ansible 控制机发 SIGTERM 杀 ansible-playbook 进程，<b style="color:#dc2626;">可能让目标机器留在半部署状态</b>。\n\n确认要取消吗？'
    : '代码已提交仓库，后台同步会继续进行。\n取消后只是不再等待状态反馈。\n\n如果 30 分钟内仍未完成，请人工检查对应服务的 Pod 是否已正常启动。'
  const title = isVm ? '⚠ 取消 VM 任务（不可逆）' : '确认取消等待？'
  try {
    await ElMessageBox.confirm(message, title, {
      type: 'warning',
      dangerouslyUseHTMLString: isVm,
      confirmButtonText: isVm ? '确认取消（接受半部署风险）' : '确定取消',
      cancelButtonText: isVm ? '不取消' : '继续等待',
      confirmButtonClass: isVm ? 'el-button--danger' : '',
      autofocus: false,
    })
  } catch (_) { return }
  cancelingIds.add(row.id)
  try {
    const r = await cancelDeployment(row.id)
    if (r?.result === 'stale') {
      ElMessage.info('该任务已结束，无需取消')
    } else if (isVm) {
      ElMessage.success('已发取消请求 · agent 杀进程后状态会变 canceled')
    } else {
      ElMessage.success('已取消等待 · 后台同步会继续进行')
    }
    await doSearch()
  } catch (e) {
    ElMessage.error(e?.response?.data?.message || e.message || '取消失败')
  } finally {
    cancelingIds.delete(row.id)
  }
}

onMounted(async () => {
  // K8s 和 VM 环境都要拉，前端按 row.action 是否 vm_ 前缀判断走哪个 detail 块
  // env id 是不同表的，但加载到同一数组按 _kind 区分；envName/envType 通过 id 在数组里找
  // 注意：K8s 和 VM 的 id 来自不同表，可能撞号 → 这里要么按"先 K8s 后 VM"的顺序保留 K8s 优先匹配
  // 因为 listDeployments 返回的 row.project_env_id 实际意义按 row.action 是否 vm_ 区分：
  // - K8s action → K8s project_env.id
  // - VM action  → vm_project_env.id
  // 所以查找时也得带上 row 上下文，下面 envFor()/envName()/envType() 都改成接 row
  const [k8s, vm] = await Promise.all([listProjectEnvs(), listVmProjectEnvs().catch(() => [])])
  envs.value = [
    ...(k8s || []).map(e => ({ ...e, _kind: 'k8s' })),
    ...(vm  || []).map(e => ({ ...e, _kind: 'vm'  })),
  ]
  await doSearch()
  // 1s tick 驱动 liveDuration 插值，让非终态行的耗时每秒 +1；终态行不受影响
  tickTimer = setInterval(() => { tick.value++ }, 1000)
  // Lark 通知「查看发布详情」按钮带 ?expand=<id>，加载完自动把该行展开
  const expandID = route.query.expand ? Number(route.query.expand) : null
  if (expandID) {
    await nextTick()
    const idx = list.value.findIndex(d => d.id === expandID)
    if (idx >= 0) expanded.value[idx] = true
  }
})

onUnmounted(() => {
  stopPolling()
  if (tickTimer) { clearInterval(tickTimer); tickTimer = null }
})

// pending 全部结束后清理 anchors，避免长时间积累
// liveAnchors 已被 last_polled_at 锚点替代，无需再做清理 watcher
</script>

<style scoped>
.dh { }

/* ===== K8s / VM 区分徽章（跟 Dashboard 配色一致） ===== */
.kind-mini {
  display: inline-block;
  font: 700 9.5px 'Fira Code', monospace;
  letter-spacing: .5px;
  padding: 1px 5px; border-radius: 3px;
  vertical-align: middle;
}
.kind-mini.vm  { background: #faf5ff; color: #7c3aed; border: 1px solid #ddd6fe; }
.kind-mini.k8s { background: #eff6ff; color: #1d4ed8; border: 1px solid #bfdbfe; }
.vm-detail-grid { display: flex; gap: 24px; flex-wrap: wrap; font-size: 12.5px; padding: 4px 0; }
.vm-detail-grid > div { color: var(--text); }
.vm-detail-grid b { font-family: 'Fira Code', monospace; font-weight: 600; margin-left: 4px; }
.vm-detail-grid .muted { color: var(--text-3); }

/* ===== VM 日志弹窗 ===== */
:deep(.vm-log-dialog .el-dialog__body) { padding: 0 22px 12px; }
.vm-log-loading, .vm-log-error {
  padding: 60px 20px; text-align: center; color: var(--text-2);
}
.vm-log-error { color: var(--danger); display: flex; align-items: center; justify-content: center; gap: 6px; }
.vm-log-pre {
  margin: 0; padding: 14px 16px;
  background: #1e1e1e; color: #d4d4d4;
  font: 500 12px/1.65 'Fira Code', monospace;
  max-height: 65vh; overflow: auto;
  white-space: pre-wrap; word-break: break-all;
  border-radius: 6px;
}
.vm-log-meta {
  font-size: 12px; color: var(--text-3); margin-right: auto;
}
.vm-log-meta b { font-family: 'Fira Code', monospace; color: var(--text-2); font-weight: 600; }
:deep(.vm-log-dialog .el-dialog__footer) { display: flex; align-items: center; }
.vm-log-status-bar {
  display: flex; align-items: center; gap: 8px;
  margin-bottom: 8px; padding: 6px 12px;
  background: #eff6ff; border: 1px solid #bfdbfe; border-radius: 4px;
  font-size: 12px; color: #1e40af;
}
.vm-log-status-bar.done {
  background: #ecfdf5; border-color: #a7f3d0; color: #059669;
}
.vm-log-live-dot {
  width: 8px; height: 8px; border-radius: 50%;
  background: #ef4444;
  animation: vm-live-pulse 1.2s infinite;
}
@keyframes vm-live-pulse {
  0%, 100% { opacity: 1; }
  50% { opacity: .4; }
}

/* ===== KPI ===== */
.kpis {
  display: grid;
  grid-template-columns: repeat(4, 1fr);
  gap: 14px;
  margin-bottom: 14px;
}
.kpi {
  background: var(--bg-card);
  border: 1px solid var(--border);
  border-radius: var(--radius);
  padding: 14px 18px;
}
.k-lbl { font-size: 11px; color: var(--text-3); text-transform: uppercase; letter-spacing: .6px; font-weight: 600; margin-bottom: 4px; }
.k-val { font-size: 24px; font-weight: 700; color: var(--text); letter-spacing: -.5px; }

/* ===== 筛选 ===== */
.filter-bar {
  background: var(--bg-card);
  border: 1px solid var(--border);
  border-radius: var(--radius);
  padding: 14px 16px;
  margin-bottom: 14px;
  display: flex;
  flex-wrap: wrap;
  gap: 14px;
  align-items: flex-end;
}
.f-item { display: flex; flex-direction: column; gap: 4px; min-width: 120px; }
.f-item.wide { min-width: 400px; }
.f-item label {
  font-size: 11px; color: var(--text-3);
  text-transform: uppercase; letter-spacing: .6px; font-weight: 600;
}
.sel, .inp {
  padding: 7px 10px;
  background: #fff;
  border: 1px solid var(--border);
  border-radius: 5px;
  font: 500 12.5px var(--body);
  color: var(--text);
}
.sel:focus, .inp:focus { outline: none; border-color: var(--primary); box-shadow: 0 0 0 3px rgba(59,130,246,.12); }
.sel:disabled { background: var(--bg-hover); color: var(--text-3); cursor: not-allowed; }
.sel-sm { padding: 3px 8px; font-size: 12px; border: 1px solid var(--border); border-radius: 4px; background: #fff; }

.date-range { display: flex; gap: 8px; align-items: center; }
.date-range .inp { width: 180px; font-family: var(--mono); font-size: 12px; padding: 6px 9px; }
.date-range .sep { color: var(--text-3); font-size: 12px; flex-shrink: 0; }

.f-actions { display: flex; gap: 6px; align-items: center; }
.btn-primary, .btn-ghost {
  padding: 8px 18px; border-radius: 5px; font: 500 12.5px var(--body); cursor: pointer;
  display: inline-flex; align-items: center; gap: 6px;
}
.btn-primary { background: var(--primary); color: #fff; border: 1px solid var(--primary); }
.btn-primary:hover { background: var(--primary-dark); }
.btn-primary .el-icon { font-size: 13px; }
.btn-primary.sm { padding: 6px 14px; font-size: 12px; }
.btn-ghost { background: #fff; border: 1px solid var(--border); color: var(--text); }
.btn-ghost:hover { border-color: var(--primary); color: var(--primary); }

/* ===== 表格 ===== */
.tbl-wrap {
  background: var(--bg-card);
  border: 1px solid var(--border);
  border-radius: var(--radius);
  overflow: hidden;
}
.loading, .empty { padding: 60px 20px; text-align: center; color: var(--text-3); }
.empty .em-t { font-size: 14px; color: var(--text-2); font-weight: 500; margin-bottom: 4px; }
.empty .em-d { font-size: 12px; }

.tbl { width: 100%; border-collapse: collapse; font-size: 13px; }
.tbl th {
  background: var(--bg-input);
  text-align: left;
  padding: 10px 14px;
  border-bottom: 1px solid var(--border);
  color: var(--text-2);
  font-size: 11px;
  text-transform: uppercase;
  letter-spacing: .6px;
  font-weight: 600;
  position: sticky; top: 0; z-index: 1;
}
.tbl td {
  padding: 11px 14px;
  border-bottom: 1px solid var(--border-soft);
  vertical-align: middle;
}
.data-row { cursor: pointer; transition: background .1s; }
.data-row:hover { background: var(--bg-input); }
.data-row.opened { background: var(--primary-bg); }

.chev-cell { text-align: center; padding-left: 14px; }
.chev { color: var(--text-3); font-size: 12px; transition: transform .15s; }
.chev.on { transform: rotate(90deg); color: var(--primary); }

.mono { font-family: var(--mono); font-size: 12px; }
.muted { color: var(--text-3); }

.action-tag {
  display: inline-block;
  padding: 2px 8px;
  border-radius: 3px;
  font-size: 11px;
  font-weight: 500;
  font-family: var(--body);
  white-space: nowrap;
}
.action-tag.update_image { background: #eff6ff; color: #1e40af; }
.action-tag.restart { background: #fef3c7; color: #92400e; }
.action-tag.rollback { background: #fce7f3; color: #9f1239; }

.status-tag {
  display: inline-block;
  padding: 2px 8px;
  border-radius: 10px;
  font-size: 11px;
  font-weight: 500;
}
.status-tag.success { background: var(--success-bg); color: var(--success-dark); }
.status-tag.partial { background: #fef3c7; color: #92400e; }
.status-tag.failed { background: var(--danger-bg); color: var(--danger-dark); }
.status-tag.pending { background: #eff6ff; color: #1e40af; }
.status-tag.canceled { background: #f3f4f6; color: #4b5563; }
.btn-cancel { padding: 6px 14px; font-size: 12px; border-radius: 4px; background: #fff; color: #6b7280; border: 1px solid #d1d5db; cursor: pointer; transition: all .12s; }
.btn-cancel:hover:not(:disabled) { color: #dc2626; border-color: #fca5a5; background: #fef2f2; }
.btn-cancel:disabled { opacity: .5; cursor: not-allowed; }
.status-tag.no_change { background: var(--bg-hover); color: var(--text-2); }

.mod-chip {
  display: inline-block;
  background: var(--bg-hover);
  color: var(--text-2);
  padding: 1px 6px;
  border-radius: 3px;
  font-family: var(--mono);
  font-size: 11px;
  margin-right: 3px;
  margin-bottom: 2px;
}
.mod-chip.more { background: #e5e7eb; color: var(--text-3); }
.env-chip { font-size: 9.5px; padding: 1px 5px; }

.commit { color: var(--primary); text-decoration: none; }
.commit:hover { text-decoration: underline; }

/* expanded detail */
.detail-row td { background: var(--bg-input); padding: 0; border-bottom: 1px solid var(--border); }
.detail-wrap { padding: 16px 28px 18px 54px; display: flex; flex-direction: column; gap: 14px; }
.section {}
.sec-lbl {
  font-size: 10.5px; color: var(--text-3);
  text-transform: uppercase; letter-spacing: .8px; font-weight: 600;
  margin-bottom: 6px;
  display: flex; align-items: center; gap: 12px;
}
.sec-sub {
  font-size: 11px; color: var(--text-2); font-weight: 400;
  text-transform: none; letter-spacing: 0;
  font-family: var(--mono);
}
.sec-sub b { color: var(--text); font-weight: 600; }
.sub-tbl { width: 100%; border-collapse: collapse; background: #fff; border: 1px solid var(--border); border-radius: 4px; overflow: hidden; }
.sub-tbl th { background: var(--bg-hover); color: var(--text-2); font-size: 10.5px; font-weight: 600; padding: 6px 10px; text-align: left; text-transform: uppercase; letter-spacing: .4px; }
.sub-tbl td { padding: 6px 10px; border-top: 1px solid var(--border-soft); font-size: 12px; }
.tag-from { color: var(--text-3); text-decoration: line-through; }
.tag-to { color: var(--success-dark); font-weight: 600; }
.sync-synced, .health-healthy { color: var(--success-dark); font-family: var(--mono); font-size: 11.5px; }
.sync-outofsync, .health-degraded { color: var(--danger-dark); font-family: var(--mono); font-size: 11.5px; }
.health-progressing { color: var(--primary); font-family: var(--mono); font-size: 11.5px; }
.msg { color: var(--text-2); font-size: 11.5px; }

.restart-mods { display: flex; flex-wrap: wrap; gap: 4px; padding: 4px 0; }

.err-msg {
  background: #fef2f2;
  border: 1px solid #fecaca;
  color: #7f1d1d;
  padding: 10px 12px;
  border-radius: 4px;
  font-family: var(--mono);
  font-size: 11.5px;
  white-space: pre-wrap;
  max-height: 160px;
  overflow: auto;
}

.section-actions { margin-top: 4px; }

/* 分页 */
.pager {
  padding: 12px 16px;
  border-top: 1px solid var(--border);
  display: flex;
  justify-content: space-between;
  align-items: center;
  background: var(--bg-input);
}
.pg-info { font-size: 12.5px; color: var(--text-2); }
.pg-info b { color: var(--text); font-family: var(--mono); font-weight: 600; }
.pg-ctrl { display: flex; align-items: center; gap: 10px; font-size: 12.5px; color: var(--text-2); }
.pg-ctrl button {
  background: #fff; border: 1px solid var(--border); color: var(--text);
  width: 28px; height: 28px; border-radius: 4px;
  cursor: pointer; font-family: var(--mono); font-size: 14px;
}
.pg-ctrl button:disabled { opacity: .35; cursor: not-allowed; }
.pg-ctrl button:hover:not(:disabled) { border-color: var(--primary); color: var(--primary); }
.pg-ctrl b { color: var(--text); font-family: var(--mono); font-weight: 600; }

/* 消息列 color-coded：成功绿、失败红、进行中保持原色 */
.sub-tbl .msg.ok   { color: #10b981; font-weight: 500; }
.sub-tbl .msg.fail { color: #ef4444; font-weight: 500; }

/* 查看日志按钮（仅失败行显示） */
.view-logs-btn {
  padding: 3px 10px;
  border: 1px solid #fda4af;
  border-radius: 4px;
  background: #fff5f5;
  color: #b91c1c;
  font-size: 11.5px;
  cursor: pointer;
  transition: all .15s;
}
.view-logs-btn:hover {
  background: #fee2e2;
  border-color: #ef4444;
  color: #991b1b;
}
</style>
