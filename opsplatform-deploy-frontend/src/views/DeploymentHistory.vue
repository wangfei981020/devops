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
        <input v-model="draft.operator" class="inp" placeholder="模糊匹配" />
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
                {{ envName(row.project_env_id) }}
                <span :class="'env-chip ' + envType(row.project_env_id)" style="margin-left:4px">{{ envType(row.project_env_id).toUpperCase() }}</span>
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

                  <div v-if="row.action !== 'restart'" class="section">
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

                  <div class="section">
                    <div class="sec-lbl">
                      ArgoCD 结果
                      <span class="sec-sub">
                        模块数 <b>{{ row.argocd_results?.length || 0 }}</b>
                        <span v-if="row.duration_sec != null"> · 总耗时 <b>{{ row.duration_sec }}s</b></span>
                      </span>
                    </div>
                    <table class="sub-tbl">
                      <thead><tr><th>应用</th><th>同步</th><th>健康</th><th>单模块耗时</th><th>消息</th></tr></thead>
                      <tbody>
                        <tr v-for="r in (row.argocd_results || [])" :key="r.app">
                          <td class="mono">{{ r.app }}</td>
                          <td :class="'sync-' + (r.sync_status || '').toLowerCase()">{{ r.sync_status || '—' }}</td>
                          <td :class="'health-' + (r.health || '').toLowerCase()">{{ r.health || '—' }}</td>
                          <td class="mono">{{ liveDuration(r) }}s</td>
                          <td class="msg">{{ r.msg || '—' }}</td>
                        </tr>
                        <tr v-if="!row.argocd_results?.length"><td colspan="5" class="muted" style="text-align:center">无</td></tr>
                      </tbody>
                    </table>
                  </div>

                  <div v-if="row.error_msg" class="section">
                    <div class="sec-lbl">错误信息</div>
                    <pre class="err-msg">{{ row.error_msg }}</pre>
                  </div>

                  <div class="section-actions">
                    <button
                      v-if="row.action !== 'restart' && ['success','partial'].includes(row.status)"
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
  </div>
</template>

<script setup>
import { ref, reactive, computed, onMounted, onUnmounted, watch, nextTick } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import dayjs from 'dayjs'
import { Search, ArrowRight } from '@element-plus/icons-vue'
import { listDeployments, listProjectEnvs, getDeployment } from '../api'
import RollbackDialog from '../components/RollbackDialog.vue'

const route = useRoute()
const router = useRouter()

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

function envLabel(v) { return { uat: 'UAT', prod: 'PROD' }[v] || v }
function envName(id) { return envs.value.find(e => e.id === id)?.name || id }
function envType(id) { return envs.value.find(e => e.id === id)?.env_type || 'uat' }
function fmtTime(s) { return dayjs(s).format('YYYY-MM-DD HH:mm') }
function actionLabel(a) {
  return { update_image: '更新镜像', restart: '重启服务', rollback: '回滚' }[a] || a
}
function statusLabel(s) {
  return { success: '成功', partial: '部分成功', failed: '失败', pending: '进行中', no_change: '无变化' }[s] || s
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
// 后端每 5s 推一次真实 duration_sec，前端在两次推送之间按本地时间 +1 插值，
// 让非终态的行耗时看起来每秒都在跳。终态（Healthy/Degraded/failed/timeout）直接用后端值。
const tick = ref(0)
let tickTimer = null
// 每个 app 独立记录"上次收到新值时的本地时间 + 当时的 duration_sec"
// key = row.id + '|' + app   value = { base: number, baseAt: number }
const liveAnchors = reactive({})
function isTerminal(r) {
  const h = (r.health || '').toLowerCase()
  const s = (r.sync_status || '').toLowerCase()
  return h === 'healthy' || h === 'degraded' || s === 'failed' || s === 'timeout'
}
function liveDuration(r) {
  // 读 tick 触发响应式
  void tick.value
  if (isTerminal(r)) return r.duration_sec ?? 0
  const key = r.app || ''
  const anchor = liveAnchors[key]
  const now = Date.now()
  // 如果后端推来的 duration 比 anchor 里的大，更新 anchor
  if (!anchor || (r.duration_sec ?? 0) !== anchor.base) {
    liveAnchors[key] = { base: r.duration_sec ?? 0, baseAt: now }
    return r.duration_sec ?? 0
  }
  // anchor 已对齐，根据本地时间插值（每秒 +1）
  const delta = Math.floor((now - anchor.baseAt) / 1000)
  return anchor.base + delta
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

onMounted(async () => {
  envs.value = (await listProjectEnvs()) || []
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
watch(list, () => {
  const stillActive = new Set()
  list.value.forEach(row => {
    (row.argocd_results || []).forEach(r => {
      if (!isTerminal(r)) stillActive.add(r.app)
    })
  })
  Object.keys(liveAnchors).forEach(k => {
    if (!stillActive.has(k)) delete liveAnchors[k]
  })
}, { deep: true })
</script>

<style scoped>
.dh { }

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
</style>
