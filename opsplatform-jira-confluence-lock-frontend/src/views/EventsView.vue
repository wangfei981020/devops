<template>
  <div>
    <div class="page-header">
      <h1>事件日志</h1>
      <p>Jira Webhook 触发的页面权限变更记录</p>
    </div>

    <!-- 筛选 -->
    <div class="card" style="padding: 14px 20px;">
      <div style="display: flex; gap: 12px; align-items: center; flex-wrap: wrap;">
        <input v-model="filters.issue_key" class="form-input" style="width: 200px;" placeholder="搜索 Issue Key..." @keyup.enter="loadEvents" />
        <select v-model="filters.status" class="form-select" style="width: 160px;" @change="loadEvents">
          <option value="">全部状态</option>
          <option value="success">成功</option>
          <option value="failed">失败</option>
          <option value="processing">处理中</option>
          <option value="skipped">已跳过</option>
        </select>
        <button class="btn btn-secondary btn-sm" @click="loadEvents">刷新</button>
      </div>
    </div>

    <!-- 表格 -->
    <div class="card">
      <div class="table-wrapper">
        <table>
          <thead>
            <tr>
              <th>ID</th>
              <th>Jira Issue</th>
              <th>Confluence 页面</th>
              <th>工作流状态</th>
              <th>动作</th>
              <th>状态</th>
              <th>重试</th>
              <th>时间</th>
              <th>操作</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="e in events" :key="e.id">
              <td style="color: var(--text-muted);">{{ e.id }}</td>
              <td><strong>{{ e.jira_issue_key }}</strong></td>
              <td>
                <a v-if="e.confluence_page_url" :href="e.confluence_page_url" target="_blank" style="color: var(--accent);">
                  {{ e.confluence_page_id }}
                </a>
                <span v-else style="color: var(--text-muted);">-</span>
              </td>
              <td>{{ e.transition_name }}</td>
              <td>
                <span v-if="e.action" :class="['badge', 'badge-' + e.action]">
                  {{ e.action === 'lock' ? '锁定' : '解锁' }}
                </span>
                <span v-else style="color: var(--text-muted);">-</span>
              </td>
              <td>
                <span :class="['badge', statusBadge(e.status)]">{{ statusText(e.status) }}</span>
              </td>
              <td style="color: var(--text-muted);">{{ e.retry_count }}</td>
              <td style="color: var(--text-muted); font-size: 12px;">{{ e.created_at }}</td>
              <td>
                <button v-if="e.status === 'failed'" class="btn btn-secondary btn-sm" @click="handleRetry(e.id)">重试</button>
                <button v-if="e.error_message" class="btn btn-secondary btn-sm" @click="showError(e)" style="margin-left: 4px;">详情</button>
              </td>
            </tr>
          </tbody>
        </table>
        <div v-if="events.length === 0" class="empty-state">
          <p>暂无事件记录</p>
        </div>
      </div>

      <!-- 分页 -->
      <div class="pagination" v-if="total > pageSize">
        <button :disabled="page <= 1" @click="page--; loadEvents()">上一页</button>
        <span>第 {{ page }} 页 / 共 {{ Math.ceil(total / pageSize) }} 页 ({{ total }} 条)</span>
        <button :disabled="page >= Math.ceil(total / pageSize)" @click="page++; loadEvents()">下一页</button>
      </div>
    </div>

    <!-- 错误详情弹窗 -->
    <div v-if="errorDetail" class="modal-overlay" @click.self="errorDetail = null">
      <div class="modal">
        <div class="modal-header">
          <span class="modal-title">错误详情 - {{ errorDetail.jira_issue_key }}</span>
          <button class="modal-close" @click="errorDetail = null">&times;</button>
        </div>
        <div style="background: var(--bg-primary); border-radius: 8px; padding: 14px; font-family: var(--font-mono); font-size: 13px; color: var(--danger); white-space: pre-wrap; max-height: 300px; overflow-y: auto;">{{ errorDetail.error_message }}</div>
      </div>
    </div>
  </div>
</template>

<script setup>
import { ref, onMounted, inject } from 'vue'
import { getEvents, retryEvent } from '@/api'

const toast = inject('toast')
const events = ref([])
const total = ref(0)
const page = ref(1)
const pageSize = 20
const filters = ref({ status: '', issue_key: '' })
const errorDetail = ref(null)

function statusBadge(s) {
  const map = { success: 'badge-success', failed: 'badge-danger', processing: 'badge-warning', skipped: 'badge-muted', pending: 'badge-info' }
  return map[s] || 'badge-muted'
}

function statusText(s) {
  const map = { success: '成功', failed: '失败', processing: '处理中', skipped: '已跳过', pending: '等待中' }
  return map[s] || s
}

async function loadEvents() {
  try {
    const res = await getEvents({ page: page.value, page_size: pageSize, ...filters.value })
    events.value = res.data.items
    total.value = res.data.total
  } catch (e) {
    toast(e.message, 'error')
  }
}

async function handleRetry(id) {
  try {
    await retryEvent(id)
    toast('已开始重试', 'success')
    setTimeout(loadEvents, 1000)
  } catch (e) {
    toast(e.message, 'error')
  }
}

function showError(e) {
  errorDetail.value = e
}

onMounted(loadEvents)
</script>
