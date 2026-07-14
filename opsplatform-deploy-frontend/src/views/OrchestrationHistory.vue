<template>
  <div class="oh-page">
    <div class="page-head">
      <div>
        <h2>新增历史</h2>
        <p class="sub">新增模块提交后台异步执行：git 提交 → 真部署则轮询 ArgoCD 到就绪。这里看状态、同步结果、是否启动成功、失败报错</p>
      </div>
      <el-button :loading="loading" @click="load">刷新</el-button>
    </div>

    <el-table :data="list" border stripe v-loading="loading" row-key="id">
      <el-table-column type="expand">
        <template #default="{ row }">
          <div class="detail">
            <div v-if="row.error_msg" class="note" :class="row.status">
              <b>{{ row.status === 'failed' ? '失败原因' : '说明' }}：</b>{{ row.error_msg }}
            </div>
            <div class="sec-lbl">同步结果<span v-if="row.duration_sec"> · 耗时 {{ row.duration_sec }}s</span></div>
            <table class="sub-tbl" v-if="row.argocd_results && row.argocd_results.length">
              <thead><tr><th>应用</th><th>同步</th><th>健康</th><th>耗时</th><th>消息</th><th style="width:96px">操作</th></tr></thead>
              <tbody>
                <tr v-for="a in row.argocd_results" :key="a.app">
                  <td class="mono">{{ a.app }}</td>
                  <td :class="'sync-' + (a.sync_status||'').toLowerCase()">{{ a.sync_status || '—' }}</td>
                  <td :class="'health-' + (a.health||'').toLowerCase()">{{ a.health || '—' }}</td>
                  <td class="mono">{{ a.duration_sec != null ? a.duration_sec + 's' : '—' }}</td>
                  <td class="msg" :class="msgClass(a)">{{ a.msg || '—' }}</td>
                  <td><el-button v-if="canLogs(a)" link type="primary" size="small" @click="openLogs(row, a.app)">查看日志</el-button><span v-else class="muted">—</span></td>
                </tr>
              </tbody>
            </table>
            <div v-else class="muted small">无同步结果（预演未部署 / git 阶段失败 / 环境未开自动同步）</div>
          </div>
        </template>
      </el-table-column>
      <el-table-column label="时间" width="150"><template #default="{ row }">{{ fmtTime(row.created_at) }}</template></el-table-column>
      <el-table-column label="环境" width="120"><template #default="{ row }"><code>{{ row.env_name }}</code></template></el-table-column>
      <el-table-column label="类型" width="70"><template #default="{ row }">
        <el-tag size="small" :type="row.kind === 'batch' ? 'warning' : 'info'">{{ row.kind === 'batch' ? '批量' : '单个' }}</el-tag>
      </template></el-table-column>
      <el-table-column label="模块" min-width="240"><template #default="{ row }"><span class="mono">{{ row.module_name }}</span></template></el-table-column>
      <el-table-column label="状态" width="110"><template #default="{ row }">
        <el-tag size="small" :type="statusType(row.status)">{{ statusLabel(row.status) }}</el-tag>
      </template></el-table-column>
      <el-table-column label="commit" width="110"><template #default="{ row }">
        <a v-if="row.commit_url" :href="row.commit_url" target="_blank" class="commit">{{ row.commit_sha }}</a>
        <span v-else class="muted">—</span>
      </template></el-table-column>
      <el-table-column label="操作人" width="100"><template #default="{ row }">{{ row.operator || '—' }}</template></el-table-column>
      <el-table-column label="操作" width="90"><template #default="{ row }">
        <el-button v-if="canRetry(row)" link type="primary" size="small" :loading="retrying.has(row.id)" @click="onRetry(row)">重试</el-button>
        <span v-else class="muted">—</span>
      </template></el-table-column>
    </el-table>

    <!-- pod 日志弹窗 -->
    <el-dialog v-model="logDlg.show" :title="`Pod 日志 · ${logDlg.app}`" width="900px" :close-on-click-modal="false" top="6vh">
      <div class="log-bar">
        <el-select v-model="logDlg.pod" size="small" style="width:340px" @change="loadLogs" placeholder="选择 Pod">
          <el-option v-for="p in logDlg.pods" :key="p.name" :label="`${p.name}  (${p.status_reason||p.health})`" :value="p.name" />
        </el-select>
        <el-switch v-model="logDlg.previous" active-text="上一次(崩溃前)" @change="loadLogs" style="margin-left:12px" />
        <el-button size="small" :loading="logDlg.loading" @click="loadLogs" style="margin-left:auto">刷新</el-button>
      </div>
      <pre class="logs" v-loading="logDlg.loading">{{ logDlg.text || '（无日志 / 选择 Pod）' }}</pre>
    </el-dialog>
  </div>
</template>

<script setup>
import { ref, reactive, onMounted, onUnmounted } from 'vue'
import dayjs from 'dayjs'
import { ElMessage, ElMessageBox } from 'element-plus'
import * as api from '../api'

const list = ref([])
const loading = ref(false)
const retrying = reactive(new Set())
let timer = null

function fmtTime(t) { return t ? dayjs(t).format('YYYY-MM-DD HH:mm') : '—' }
function statusLabel(s) { return { pending: '进行中', success: '成功', failed: '失败' }[s] || s }
function statusType(s) { return { pending: '', success: 'success', failed: 'danger' }[s] || 'info' }
function msgClass(a) { const s=(a.sync_status||'').toLowerCase(), h=(a.health||'').toLowerCase(); return (s==='failed'||s==='timeout'||h==='degraded')?'fail':(h==='healthy'?'ok':'') }
function canLogs(a) { const s=(a.sync_status||'').toLowerCase(), h=(a.health||'').toLowerCase(); return s==='failed'||s==='timeout'||h==='degraded'||h==='progressing' }
// 只有真部署失败的单个任务可重试(预演/批量不行)
function canRetry(row) { return row.kind === 'single' && !row.disable && (row.status === 'failed') }

async function load() {
  loading.value = true
  try {
    list.value = (await api.listOrchTasks())?.list || []
    if (list.value.some(t => t.status === 'pending')) startPoll(); else stopPoll()
  } finally { loading.value = false }
}
function startPoll() { if (!timer) timer = setInterval(load, 5000) }
function stopPoll() { if (timer) { clearInterval(timer); timer = null } }

async function onRetry(row) {
  try {
    await ElMessageBox.confirm(`重试将重新触发 ArgoCD 同步并轮询模块 ${row.module_name} 的部署状态。确认？`, '重试部署', { type: 'warning', confirmButtonText: '重试', cancelButtonText: '取消' })
  } catch { return }
  retrying.add(row.id)
  try {
    await api.retryOrchTask(row.id)
    ElMessage.success('已重新触发，正在跟踪部署状态')
    await load()
  } catch (e) {
    ElMessage.error(e?.response?.data?.message || e.message || '重试失败')
  } finally { retrying.delete(row.id) }
}

// ---- pod 日志 ----
const logDlg = reactive({ show: false, taskId: null, app: '', pods: [], pod: '', previous: false, text: '', loading: false })
async function openLogs(row, app) {
  Object.assign(logDlg, { show: true, taskId: row.id, app, pods: [], pod: '', previous: false, text: '', loading: true })
  try {
    logDlg.pods = (await api.getOrchTaskPods(row.id, app))?.pods || []
    logDlg.pod = logDlg.pods[0]?.name || ''
    if (logDlg.pod) await loadLogs()
  } catch (e) {
    logDlg.text = '拉取 Pod 列表失败：' + (e?.response?.data?.message || e.message)
  } finally { logDlg.loading = false }
}
async function loadLogs() {
  if (!logDlg.pod) return
  logDlg.loading = true
  try {
    const p = logDlg.pods.find(x => x.name === logDlg.pod)
    const r = await api.getOrchTaskPodLogs(logDlg.taskId, { app: logDlg.app, pod: logDlg.pod, namespace: p?.namespace || '', previous: logDlg.previous, tail_lines: 500 })
    logDlg.text = r?.logs || '（无日志）'
  } catch (e) {
    logDlg.text = '拉取日志失败：' + (e?.response?.data?.message || e.message)
  } finally { logDlg.loading = false }
}

onMounted(load)
onUnmounted(stopPoll)
</script>

<style scoped>
.oh-page { padding: 16px 20px; }
.page-head { display: flex; justify-content: space-between; align-items: flex-start; }
.page-head h2 { margin: 0 0 4px; font-size: 18px; }
.sub { color: #909399; font-size: 13px; margin: 0 0 16px; max-width: 900px; }
.mono, code { font-family: var(--mono, monospace); font-size: 12.5px; }
.commit { font-family: var(--mono, monospace); color: var(--el-color-primary); text-decoration: none; }
.muted { color: #c0c4cc; } .small { font-size: 12px; }
.detail { padding: 8px 12px; }
.note { padding: 8px 12px; border-radius: 6px; margin-bottom: 10px; font-size: 12.5px; white-space: pre-wrap; }
.note.failed { background: #fef2f2; color: #991b1b; }
.note.success, .note.pending { background: #eff6ff; color: #1e40af; }
.sec-lbl { font-size: 12px; color: #909399; margin-bottom: 6px; }
.sub-tbl { width: 100%; border-collapse: collapse; font-size: 12.5px; }
.sub-tbl th, .sub-tbl td { border: 1px solid #ebeef5; padding: 5px 8px; text-align: left; }
.sub-tbl th { background: #fafafa; color: #606266; font-weight: 600; }
.msg.fail { color: #dc2626; } .msg.ok { color: #16a34a; }
.sync-synced { color: #16a34a; } .sync-failed, .sync-timeout { color: #dc2626; }
.health-healthy { color: #16a34a; } .health-degraded { color: #dc2626; } .health-progressing { color: #d97706; }
.log-bar { display: flex; align-items: center; margin-bottom: 10px; }
.logs { background: #0b1021; color: #d1d5db; padding: 12px; border-radius: 6px; font-family: var(--mono, monospace); font-size: 12px; white-space: pre-wrap; word-break: break-all; max-height: 60vh; overflow: auto; margin: 0; }
</style>
