<template>
  <div class="page">
    <div class="page-head">
      <span class="page-title">执行记录</span>
      <el-button :icon="Refresh" @click="load">刷新</el-button>
    </div>
    <div class="muted" style="margin-bottom:14px">定时任务每次执行的历史（运行中可实时看进度/耗时，失败可查具体原因并重试），保留 90 天。配置任务请到「定时任务」页。</div>

    <el-card shadow="never">
      <div class="filters">
        <el-select v-model="q.task_key" clearable placeholder="全部任务" style="width:220px" @change="reload">
          <el-option v-for="t in tasks" :key="t.task_key" :label="t.name" :value="t.task_key" />
        </el-select>
        <el-select v-model="q.status" clearable placeholder="全部结果" style="width:150px" @change="reload">
          <el-option label="🔵 运行中" value="running" />
          <el-option label="✅ 成功" value="ok" />
          <el-option label="⚠️ 部分成功" value="partial" />
          <el-option label="❌ 失败" value="fail" />
          <el-option label="⏱ 超时中止" value="timeout" />
          <el-option label="⛔ 已取消" value="cancelled" />
          <el-option label="🔌 中断" value="interrupted" />
        </el-select>
        <el-select v-model="q.days" style="width:130px" @change="reload">
          <el-option label="近 24 小时" :value="1" />
          <el-option label="近 7 天" :value="7" />
          <el-option label="近 30 天" :value="30" />
          <el-option label="近 90 天" :value="90" />
        </el-select>
        <span class="grow" />
        <span class="muted">共 {{ total }} 次 · 失败/部分 {{ badCount }}</span>
      </div>

      <LoadError :error="loadErr" @retry="load" />
      <el-table :data="rows" size="small" v-loading="loading" @row-click="openDetail" style="cursor:pointer">
        <el-table-column label="结果" width="120"><template #default="{ row }">
          <el-tag :type="stType(row.status)" size="small" :effect="row.status==='running'?'dark':'light'">{{ stLabel(row.status) }}</el-tag>
        </template></el-table-column>
        <el-table-column label="任务名" min-width="200"><template #default="{ row }">
          <b>{{ row.name }}</b>
          <el-tag v-if="row.trigger_by==='manual'" size="small" type="info" effect="plain" style="margin-left:6px">手动</el-tag>
          <el-tag v-else-if="row.trigger_by==='retry'" size="small" type="warning" effect="plain" style="margin-left:6px">重试</el-tag>
        </template></el-table-column>
        <el-table-column label="执行时间" width="170"><template #default="{ row }">{{ row.status==='running' ? row.started_at : row.finished_at }}</template></el-table-column>
        <el-table-column label="结果摘要" min-width="260"><template #default="{ row }">
          <template v-if="row.status==='running'">
            <span class="muted">处理中 {{ row.progress || '…' }}</span>
            <el-progress v-if="pct(row)>=0" :percentage="pct(row)" :stroke-width="6" style="max-width:220px" />
          </template>
          <template v-else>
            <!-- 摘要现在带具体对象，可能很长：列表页省略显示（悬停看全文），完整内容在详情抽屉 -->
            <el-tooltip :content="row.summary" :disabled="(row.summary||'').length <= 90" placement="top-start">
              <span class="sum-text">{{ row.summary || '—' }}</span>
            </el-tooltip>
            <span v-if="row.failures.length" class="fail-badge">{{ row.failures.length }} 项失败</span>
            <span v-if="row.findings.length" :class="['find-badge', worstLevel(row.findings)]">{{ row.findings.length }} 项发现</span>
          </template>
        </template></el-table-column>
        <el-table-column label="耗时" width="90"><template #default="{ row }">
          <span v-if="row.status==='running'" class="running-time">⏱ {{ elapsed(row) }}s</span>
          <span v-else>{{ dur(row.duration_ms) }}</span>
        </template></el-table-column>
        <el-table-column label="通知" width="90" align="center"><template #default="{ row }">
          <el-tooltip :content="notifyTip(row)"><span>{{ notifyIcon(row.notify_state) }}</span></el-tooltip>
        </template></el-table-column>
        <el-table-column label="操作" width="130" fixed="right"><template #default="{ row }">
          <el-button v-if="row.status==='running'" link type="danger" :icon="CircleClose" :loading="cancelling[row.id]" @click.stop="cancelRun(row)">取消</el-button>
          <el-button link type="primary" :icon="View" @click.stop="openDetail(row)">详情</el-button>
        </template></el-table-column>
      </el-table>

      <div class="pager">
        <el-pagination background layout="prev, pager, next" :total="total"
          :page-size="q.limit" :current-page="page" @current-change="onPage" />
      </div>
    </el-card>

    <el-drawer v-model="dlg" :title="cur.name" size="540px" :close-on-press-escape="true">
      <template v-if="cur.id">
        <div class="d-head">
          <el-tag :type="stType(cur.status)" size="default" :effect="cur.status==='running'?'dark':'light'">{{ stLabel(cur.status) }}</el-tag>
        </div>
        <el-descriptions :column="1" border size="small" style="margin-top:14px">
          <el-descriptions-item label="执行时间">{{ cur.started_at || cur.finished_at }}</el-descriptions-item>
          <el-descriptions-item v-if="cur.status==='running'" label="已耗时"><span class="running-time">⏱ {{ elapsed(cur) }}s（实时）</span></el-descriptions-item>
          <el-descriptions-item v-else label="耗时">{{ dur(cur.duration_ms) }}</el-descriptions-item>
          <el-descriptions-item v-if="cur.status==='running'" label="进度">
            {{ cur.progress || '…' }}
            <el-progress v-if="pct(cur)>=0" :percentage="pct(cur)" :stroke-width="8" />
          </el-descriptions-item>
          <el-descriptions-item label="触发方式">{{ trigLabel(cur.trigger_by) }}</el-descriptions-item>
          <el-descriptions-item v-if="cur.status!=='running'" label="结果">{{ cur.summary || '—' }}</el-descriptions-item>
        </el-descriptions>

        <!-- 发现明细：告警任务查出来的问题项。这里必须能看到具体对象——
             此前只有一句「危险 1 项、偏高 3 项」，不知道是哪个盘，等于没有告警。 -->
        <div v-if="cur.findings.length" class="sec">
          <div class="sec-title">🔎 发现明细（{{ cur.findings.length }}）</div>
          <el-table :data="cur.findings" size="small" :show-header="true">
            <el-table-column label="级别" width="76"><template #default="{ row }">
              <el-tag size="small" :type="lvType(row.level)" effect="dark">{{ lvLabel(row.level) }}</el-tag>
            </template></el-table-column>
            <el-table-column prop="target" label="对象" min-width="200" show-overflow-tooltip />
            <el-table-column prop="value" label="数值" width="90" />
            <el-table-column prop="detail" label="说明" min-width="200" show-overflow-tooltip />
          </el-table>
        </div>

        <div v-if="cur.failures.length" class="sec">
          <div class="sec-title" style="display:flex;align-items:center">
            ❌ 失败明细（{{ cur.failures.length }}）
            <el-button size="small" type="warning" :icon="RefreshRight" :loading="retrying" style="margin-left:auto" @click="retryFailures">重试失败项</el-button>
          </div>
          <ul class="fail-list">
            <li v-for="(f, i) in cur.failures" :key="i">
              <b>{{ f.target || '?' }}</b><span class="muted"> — {{ f.reason || f }}</span>
            </li>
          </ul>
        </div>

        <div class="sec">
          <div class="sec-title">📨 通知投递</div>
          <el-descriptions :column="1" border size="small">
            <el-descriptions-item label="状态"><span>{{ notifyIcon(cur.notify_state) }} {{ notifyText(cur.notify_state) }}</span></el-descriptions-item>
            <el-descriptions-item label="Lark 群">{{ cur.notify_group || '—' }}</el-descriptions-item>
            <el-descriptions-item label="@ 的人">{{ cur.notify_at || '—' }}</el-descriptions-item>
          </el-descriptions>
        </div>

        <div class="sec">
          <el-button v-if="cur.status==='running'" type="danger" :icon="CircleClose" :loading="cancelling[cur.id]" @click="cancelRun(cur)">取消执行</el-button>
          <el-button type="primary" :icon="VideoPlay" :loading="rerunning" :disabled="cur.status==='running'" @click="rerun">重跑此任务（全量）</el-button>
        </div>
      </template>
    </el-drawer>
  </div>
</template>

<script setup>
import { ref, computed, onMounted, onUnmounted } from 'vue'
import { ElMessage } from 'element-plus'
import { Refresh, VideoPlay, RefreshRight, CircleClose, View } from '@element-plus/icons-vue'
import { listTaskRuns, listScheduledTasks, runScheduledTask, retryTaskRunFailures, cancelTaskRun } from '../api/cmdb'
import { normalizeError } from '../api/http'
import LoadError from '../components/LoadError.vue'
import { useAppStore } from '../stores/app'

const app = useAppStore()
const tasks = ref([]), rows = ref([]), total = ref(0), loading = ref(false)
const loadErr = ref('')
const q = ref({ task_key: '', status: '', days: 7, limit: 20 })
const page = ref(1)
const dlg = ref(false), cur = ref({}), rerunning = ref(false), retrying = ref(false)
const cancelling = ref({})
async function cancelRun(row) {
  try {
    await app.showConfirm(`取消运行中的「${row.name}」？会中止任务并标为已取消`)
    cancelling.value = { ...cancelling.value, [row.id]: true }
    const r = await cancelTaskRun(row.id)
    ElMessage.success(r.ok ? '已取消' : (r.msg || '该记录已结束')); setTimeout(load, 800)
  } catch (e) { if (e !== 'cancel') ElMessage.error(e.response?.data?.error || '取消失败') }
  finally { cancelling.value = { ...cancelling.value, [row.id]: false } }
}
const nowTick = ref(Date.now())
let timer = null

const badCount = computed(() => rows.value.filter((r) => r.status !== 'ok' && r.status !== 'running').length)
const hasRunning = computed(() => rows.value.some((r) => r.status === 'running'))

function stLabel(s) { return { running: '🔵 运行中', ok: '✅ 成功', partial: '⚠️ 部分成功', fail: '❌ 失败', timeout: '⏱ 超时中止', cancelled: '⛔ 已取消', interrupted: '🔌 中断' }[s] || s }
function stType(s) { return { running: 'primary', ok: 'success', partial: 'warning', fail: 'danger', timeout: 'danger', cancelled: 'info', interrupted: 'warning' }[s] || 'info' }
function trigLabel(t) { return { manual: '手动立即运行', retry: '重试失败项', cron: '定时(cron)' }[t] || '定时(cron)' }
function dur(ms) { return ms >= 1000 ? (ms / 1000).toFixed(1) + 's' : ms + 'ms' }
function notifyIcon(s) { return s === 'sent' ? '📨' : s === 'failed' ? '⚠️' : '—' }
function notifyText(s) { return { sent: '已送达', failed: 'Lark 发送失败', skipped: '按配置未发送', none: '未配置通知群' }[s] || '—' }
function notifyTip(r) { return notifyText(r.notify_state) + (r.notify_group ? `（${r.notify_group}）` : '') }
// 发现明细：级别 → 颜色/中文。critical 用红色，是"现在就得处理"，别和 warning 混色
function lvType(l) { return { critical: 'danger', warning: 'warning', info: 'info' }[l] || 'info' }
function lvLabel(l) { return { critical: '危险', warning: '偏高', info: '提示' }[l] || l }
// 列表页徽标取最严重的那一级：一眼看出这次跑出来有没有红的
function worstLevel(fs) {
  if (fs.some((f) => f.level === 'critical')) return 'lv-crit'
  if (fs.some((f) => f.level === 'warning')) return 'lv-warn'
  return 'lv-info'
}
// 运行中实时耗时（本地计时，秒）
function elapsed(row) {
  if (!row.started_at) return 0
  const start = new Date(row.started_at.replace(' ', 'T')).getTime()
  return Math.max(0, Math.floor((nowTick.value - start) / 1000))
}
// 进度百分比，取不到返回 -1
function pct(row) {
  const m = (row.progress || '').match(/^(\d+)\/(\d+)$/)
  if (!m || +m[2] === 0) return -1
  return Math.min(100, Math.round((+m[1] / +m[2]) * 100))
}

async function load() {
  loading.value = true
  try {
    const params = { ...q.value, offset: (page.value - 1) * q.value.limit }
    const data = await listTaskRuns(params)
    rows.value = data.items; total.value = data.total
    if (dlg.value && cur.value.id) { // 抽屉开着时同步刷新当前行
      const fresh = rows.value.find((r) => r.id === cur.value.id)
      if (fresh) cur.value = fresh
    }
  } catch (e) { ElMessage.error('加载失败') } finally { loading.value = false }
}
function reload() { page.value = 1; load() }
function onPage(p) { page.value = p; load() }
function openDetail(row) { cur.value = row; dlg.value = true }
async function rerun() {
  rerunning.value = true
  try { await runScheduledTask(cur.value.task_key); ElMessage.success('已触发，运行中可实时看进度'); dlg.value = false; setTimeout(load, 1000) }
  catch (e) { ElMessage.error('触发失败') } finally { rerunning.value = false }
}
async function retryFailures() {
  retrying.value = true
  try { const r = await retryTaskRunFailures(cur.value.id); ElMessage.success(r.msg || '已触发重试'); dlg.value = false; setTimeout(load, 1000) }
  catch (e) { ElMessage.error(e.response?.data?.error || '重试失败') } finally { retrying.value = false }
}

onMounted(async () => {
  try { tasks.value = await listScheduledTasks() } catch (e) { loadErr.value = normalizeError(e).message }
  load()
  // 每秒推进"已耗时"；有运行中任务时每 3 秒轮询一次进度
  timer = setInterval(() => {
    nowTick.value = Date.now()
    if (hasRunning.value && Math.floor(nowTick.value / 1000) % 3 === 0) load()
  }, 1000)
})
onUnmounted(() => { if (timer) clearInterval(timer) })
</script>

<style scoped>
.filters { display:flex; gap:10px; align-items:center; margin-bottom:12px; }
.grow { flex:1; }
.pager { display:flex; justify-content:flex-end; margin-top:12px; }
.fail-badge { margin-left:8px; color:#e6a23c; font-size:12px; }
.sum-text { display:-webkit-box; -webkit-line-clamp:2; line-clamp:2; -webkit-box-orient:vertical; overflow:hidden; }
.find-badge { margin-left:8px; font-size:12px; }
.find-badge.lv-crit { color:#f56c6c; font-weight:600; }
.find-badge.lv-warn { color:#e6a23c; }
.find-badge.lv-info { color:#909399; }
.running-time { color:#409eff; font-variant-numeric: tabular-nums; }
.d-head { margin-top:4px; }
.sec { margin-top:18px; }
.sec-title { font-weight:600; margin-bottom:8px; }
.fail-list { margin:0; padding-left:18px; max-height:240px; overflow:auto; }
.fail-list li { font-size:13px; line-height:1.8; word-break:break-all; }
</style>
