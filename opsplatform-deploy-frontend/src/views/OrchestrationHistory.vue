<template>
  <div class="oh-page">
    <div class="page-head">
      <div>
        <h2>新增历史</h2>
        <p class="sub">新增模块提交后台异步执行，这里看每次的状态、commit 和失败原因</p>
      </div>
      <el-button :loading="loading" @click="load">刷新</el-button>
    </div>

    <el-table :data="list" border stripe v-loading="loading">
      <el-table-column label="时间" width="160">
        <template #default="{ row }">{{ fmtTime(row.created_at) }}</template>
      </el-table-column>
      <el-table-column label="环境" width="130">
        <template #default="{ row }"><code>{{ row.env_name }}</code></template>
      </el-table-column>
      <el-table-column label="类型" width="80">
        <template #default="{ row }">
          <el-tag size="small" :type="row.kind === 'batch' ? 'warning' : 'info'">{{ row.kind === 'batch' ? '批量' : '单个' }}</el-tag>
        </template>
      </el-table-column>
      <el-table-column label="模块" min-width="280">
        <template #default="{ row }"><span class="mod">{{ row.module_name }}</span></template>
      </el-table-column>
      <el-table-column label="状态" width="110">
        <template #default="{ row }">
          <el-tag size="small" :type="statusType(row.status)">{{ statusLabel(row.status) }}</el-tag>
        </template>
      </el-table-column>
      <el-table-column label="commit" width="120">
        <template #default="{ row }">
          <a v-if="row.commit_url" :href="row.commit_url" target="_blank" class="commit">{{ row.commit_sha }}</a>
          <span v-else class="muted">—</span>
        </template>
      </el-table-column>
      <el-table-column label="操作人" width="110">
        <template #default="{ row }">{{ row.operator || '—' }}</template>
      </el-table-column>
      <el-table-column label="" width="70">
        <template #default="{ row }">
          <el-button v-if="row.status === 'failed' && row.error_msg" link type="danger" @click="showErr(row)">原因</el-button>
        </template>
      </el-table-column>
    </el-table>

    <el-dialog v-model="errDlg" title="失败原因" width="720px" :close-on-click-modal="false">
      <pre class="err">{{ errText }}</pre>
      <template #footer><el-button @click="errDlg = false">关闭</el-button></template>
    </el-dialog>
  </div>
</template>

<script setup>
import { ref, onMounted, onUnmounted } from 'vue'
import dayjs from 'dayjs'
import * as api from '../api'

const list = ref([])
const loading = ref(false)
const errDlg = ref(false)
const errText = ref('')
let timer = null

function fmtTime(t) { return t ? dayjs(t).format('YYYY-MM-DD HH:mm') : '—' }
function statusLabel(s) { return { pending: '进行中', success: '成功', failed: '失败' }[s] || s }
function statusType(s) { return { pending: '', success: 'success', failed: 'danger' }[s] || 'info' }
function showErr(row) { errText.value = row.error_msg || ''; errDlg.value = true }

async function load() {
  loading.value = true
  try {
    list.value = (await api.listOrchTasks())?.list || []
    // 有进行中的就 5s 轮询刷新到终态
    if (list.value.some(t => t.status === 'pending')) startPoll()
    else stopPoll()
  } finally { loading.value = false }
}

function startPoll() { if (!timer) timer = setInterval(load, 5000) }
function stopPoll() { if (timer) { clearInterval(timer); timer = null } }

onMounted(load)
onUnmounted(stopPoll)
</script>

<style scoped>
.oh-page { padding: 16px 20px; }
.page-head { display: flex; justify-content: space-between; align-items: flex-start; }
.page-head h2 { margin: 0 0 4px; font-size: 18px; }
.sub { color: #909399; font-size: 13px; margin: 0 0 16px; }
.mod { font-family: var(--mono, monospace); font-size: 12.5px; }
code { font-family: var(--mono, monospace); }
.commit { font-family: var(--mono, monospace); color: var(--el-color-primary); text-decoration: none; }
.muted { color: #c0c4cc; }
.err { white-space: pre-wrap; word-break: break-all; background: #fef2f2; color: #991b1b; padding: 12px; border-radius: 6px; margin: 0; font-size: 12px; max-height: 55vh; overflow: auto; }
</style>
