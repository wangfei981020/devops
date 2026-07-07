<template>
  <div class="page">
    <div class="page-head">
      <span class="page-title">执行记录</span>
      <el-button :icon="Refresh" @click="load">刷新</el-button>
    </div>
    <div class="muted" style="margin-bottom:14px">定时任务每次执行的历史（含失败明细与 Lark 投递状态），保留 90 天。配置任务请到「定时任务」页。</div>

    <el-card shadow="never">
      <div class="filters">
        <el-select v-model="q.task_key" clearable placeholder="全部任务" style="width:220px" @change="reload">
          <el-option v-for="t in tasks" :key="t.task_key" :label="t.name" :value="t.task_key" />
        </el-select>
        <el-select v-model="q.status" clearable placeholder="全部结果" style="width:150px" @change="reload">
          <el-option label="✅ 成功" value="ok" />
          <el-option label="⚠️ 部分成功" value="partial" />
          <el-option label="❌ 失败" value="fail" />
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

      <el-table :data="rows" size="small" v-loading="loading" @row-click="openDetail" style="cursor:pointer">
        <el-table-column label="结果" width="110"><template #default="{ row }">
          <el-tag :type="stType(row.status)" size="small">{{ stLabel(row.status) }}</el-tag>
        </template></el-table-column>
        <el-table-column label="任务名" min-width="200"><template #default="{ row }">
          <b>{{ row.name }}</b>
          <el-tag v-if="row.trigger_by==='manual'" size="small" type="info" effect="plain" style="margin-left:6px">手动</el-tag>
        </template></el-table-column>
        <el-table-column label="执行时间" width="170"><template #default="{ row }">{{ row.finished_at }}</template></el-table-column>
        <el-table-column label="结果摘要" min-width="260"><template #default="{ row }">
          <span>{{ row.summary || '—' }}</span>
          <span v-if="row.failures.length" class="fail-badge">{{ row.failures.length }} 项失败</span>
        </template></el-table-column>
        <el-table-column label="耗时" width="90"><template #default="{ row }">{{ dur(row.duration_ms) }}</template></el-table-column>
        <el-table-column label="通知" width="90" align="center"><template #default="{ row }">
          <el-tooltip :content="notifyTip(row)"><span>{{ notifyIcon(row.notify_state) }}</span></el-tooltip>
        </template></el-table-column>
      </el-table>

      <div class="pager">
        <el-pagination background layout="prev, pager, next" :total="total"
          :page-size="q.limit" :current-page="page" @current-change="onPage" />
      </div>
    </el-card>

    <el-drawer v-model="dlg" :title="cur.name" size="520px" :close-on-press-escape="true">
      <template v-if="cur.id">
        <div class="d-head">
          <el-tag :type="stType(cur.status)" size="default">{{ stLabel(cur.status) }}</el-tag>
        </div>
        <el-descriptions :column="1" border size="small" style="margin-top:14px">
          <el-descriptions-item label="执行时间">{{ cur.finished_at }}</el-descriptions-item>
          <el-descriptions-item label="耗时">{{ dur(cur.duration_ms) }}</el-descriptions-item>
          <el-descriptions-item label="触发方式">{{ cur.trigger_by==='manual' ? '手动立即运行' : '定时(cron)' }}</el-descriptions-item>
          <el-descriptions-item label="结果">{{ cur.summary || '—' }}</el-descriptions-item>
        </el-descriptions>

        <div v-if="cur.failures.length" class="sec">
          <div class="sec-title">❌ 失败明细（{{ cur.failures.length }}）</div>
          <ul class="fail-list">
            <li v-for="(f, i) in cur.failures" :key="i">{{ f }}</li>
          </ul>
        </div>

        <div class="sec">
          <div class="sec-title">📨 通知投递</div>
          <el-descriptions :column="1" border size="small">
            <el-descriptions-item label="状态">
              <span>{{ notifyIcon(cur.notify_state) }} {{ notifyText(cur.notify_state) }}</span>
            </el-descriptions-item>
            <el-descriptions-item label="Lark 群">{{ cur.notify_group || '—' }}</el-descriptions-item>
            <el-descriptions-item label="@ 的人">{{ cur.notify_at || '—' }}</el-descriptions-item>
          </el-descriptions>
        </div>

        <div class="sec">
          <el-button type="primary" :icon="VideoPlay" :loading="running" @click="rerun">重跑此任务</el-button>
        </div>
      </template>
    </el-drawer>
  </div>
</template>

<script setup>
import { ref, computed, onMounted } from 'vue'
import { ElMessage } from 'element-plus'
import { Refresh, VideoPlay } from '@element-plus/icons-vue'
import { listTaskRuns, listScheduledTasks, runScheduledTask } from '../api/cmdb'

const tasks = ref([]), rows = ref([]), total = ref(0), loading = ref(false)
const q = ref({ task_key: '', status: '', days: 7, limit: 20 })
const page = ref(1)
const dlg = ref(false), cur = ref({}), running = ref(false)

const badCount = computed(() => rows.value.filter((r) => r.status !== 'ok').length)

function stLabel(s) { return s === 'ok' ? '✅ 成功' : s === 'partial' ? '⚠️ 部分成功' : '❌ 失败' }
function stType(s) { return s === 'ok' ? 'success' : s === 'partial' ? 'warning' : 'danger' }
function dur(ms) { return ms >= 1000 ? (ms / 1000).toFixed(1) + 's' : ms + 'ms' }
function notifyIcon(s) { return s === 'sent' ? '📨' : s === 'failed' ? '⚠️' : '—' }
function notifyText(s) {
  return { sent: '已送达', failed: 'Lark 发送失败', skipped: '按配置未发送', none: '未配置通知群' }[s] || '—'
}
function notifyTip(r) { return notifyText(r.notify_state) + (r.notify_group ? `（${r.notify_group}）` : '') }

async function load() {
  loading.value = true
  try {
    const params = { ...q.value, offset: (page.value - 1) * q.value.limit }
    const data = await listTaskRuns(params)
    rows.value = data.items; total.value = data.total
  } catch (e) { ElMessage.error('加载失败') } finally { loading.value = false }
}
function reload() { page.value = 1; load() }
function onPage(p) { page.value = p; load() }
function openDetail(row) { cur.value = row; dlg.value = true }
async function rerun() {
  running.value = true
  try { await runScheduledTask(cur.value.task_key); ElMessage.success('已触发，约几秒后刷新看新记录'); dlg.value = false; setTimeout(load, 3000) }
  catch (e) { ElMessage.error('触发失败') } finally { running.value = false }
}
onMounted(async () => {
  try { tasks.value = await listScheduledTasks() } catch (e) {}
  load()
})
</script>

<style scoped>
.filters { display:flex; gap:10px; align-items:center; margin-bottom:12px; }
.grow { flex:1; }
.pager { display:flex; justify-content:flex-end; margin-top:12px; }
.fail-badge { margin-left:8px; color:#e6a23c; font-size:12px; }
.d-head { margin-top:4px; }
.sec { margin-top:18px; }
.sec-title { font-weight:600; margin-bottom:8px; }
.fail-list { margin:0; padding-left:18px; max-height:220px; overflow:auto; }
.fail-list li { font-size:13px; line-height:1.7; color:#606266; word-break:break-all; }
</style>
