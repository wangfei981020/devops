<template>
  <div>
    <div v-if="loading" class="loading"><div class="spinner"></div>加载中...</div>

    <template v-else-if="issue">
      <!-- 顶部导航 -->
      <div class="card" style="margin-bottom:16px;padding:16px">
        <div style="display:flex;align-items:center;gap:12px">
          <button class="btn btn-sm" @click="goBack">← 返回</button>
          <span class="issue-key-lg">{{ issue.key }}</span>
          <span class="badge status-badge" :class="getStatusClass(issue.fields?.status?.statusCategory?.key)">
            {{ issue.fields?.status?.name }}
          </span>
          <span class="badge" :class="'priority-' + (issue.fields?.priority?.name || '').toLowerCase()">
            {{ issue.fields?.priority?.name }}
          </span>
        </div>
      </div>

      <div class="detail-layout">
        <!-- 左侧主体 -->
        <div class="detail-main">
          <!-- 标题 -->
          <div class="card" style="margin-bottom:16px;padding:24px">
            <h2 style="margin:0 0 16px;font-size:20px;color:var(--text-primary);line-height:1.4">
              {{ issue.fields?.summary }}
            </h2>

            <!-- 描述 -->
            <div v-if="issue.fields?.description" class="section">
              <h4 class="section-title">描述</h4>
              <div class="description-content" v-html="renderDescription(issue.fields.description)"></div>
            </div>
            <div v-else class="section">
              <h4 class="section-title">描述</h4>
              <p style="color:var(--text-muted)">暂无描述</p>
            </div>
          </div>

          <!-- 评论 -->
          <div class="card" style="padding:24px" v-if="comments.length > 0">
            <h4 class="section-title" style="margin-bottom:16px">评论 ({{ comments.length }})</h4>
            <div v-for="(comment, idx) in comments" :key="idx" class="comment-item">
              <div class="comment-header">
                <span class="comment-author">{{ comment.author?.displayName || comment.author?.name }}</span>
                <span class="comment-date">{{ formatDateTime(comment.created) }}</span>
              </div>
              <div class="comment-body" v-html="renderDescription(comment.body)"></div>
            </div>
          </div>
        </div>

        <!-- 右侧信息面板 -->
        <div class="detail-sidebar">
          <div class="card" style="padding:20px">
            <h4 style="margin:0 0 16px;font-size:14px;color:var(--text-primary)">详细信息</h4>

            <div class="info-rows">
              <div class="info-row">
                <span class="info-label">类型</span>
                <span class="info-value">{{ issue.fields?.issuetype?.name }}</span>
              </div>
              <div class="info-row">
                <span class="info-label">优先级</span>
                <span class="info-value">{{ issue.fields?.priority?.name || '-' }}</span>
              </div>
              <div class="info-row">
                <span class="info-label">状态</span>
                <span class="info-value">{{ issue.fields?.status?.name }}</span>
              </div>
              <div class="info-row">
                <span class="info-label">项目</span>
                <span class="info-value link" @click="$router.push('/jira/project/' + issue.fields?.project?.key)">
                  {{ issue.fields?.project?.name }}
                </span>
              </div>
              <div class="info-row">
                <span class="info-label">经办人</span>
                <span class="info-value">{{ issue.fields?.assignee?.displayName || '-' }}</span>
              </div>
              <div class="info-row">
                <span class="info-label">报告人</span>
                <span class="info-value">{{ issue.fields?.reporter?.displayName || '-' }}</span>
              </div>
              <div class="info-row" v-if="issue.fields?.resolution">
                <span class="info-label">解决方案</span>
                <span class="info-value">{{ issue.fields.resolution.name }}</span>
              </div>
              <div class="info-row" v-if="issue.fields?.labels?.length">
                <span class="info-label">标签</span>
                <span class="info-value">
                  <span v-for="l in issue.fields.labels" :key="l" class="badge" style="margin-right:4px;font-size:11px">{{ l }}</span>
                </span>
              </div>
              <div class="info-row" v-if="issue.fields?.components?.length">
                <span class="info-label">组件</span>
                <span class="info-value">{{ issue.fields.components.map(c => c.name).join(', ') }}</span>
              </div>
              <div class="info-row" v-if="issue.fields?.fixVersions?.length">
                <span class="info-label">修复版本</span>
                <span class="info-value">{{ issue.fields.fixVersions.map(v => v.name).join(', ') }}</span>
              </div>
              <div class="info-row">
                <span class="info-label">创建时间</span>
                <span class="info-value">{{ formatDateTime(issue.fields?.created) }}</span>
              </div>
              <div class="info-row">
                <span class="info-label">更新时间</span>
                <span class="info-value">{{ formatDateTime(issue.fields?.updated) }}</span>
              </div>
            </div>
          </div>
        </div>
      </div>
    </template>

    <div v-else class="card" style="text-align:center;padding:40px;color:var(--text-muted)">
      {{ error || '工单不存在' }}
    </div>
  </div>
</template>

<script setup>
import { ref, onMounted } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import api from '@/api'

const route = useRoute()
const router = useRouter()
const issueKey = route.params.key

const loading = ref(true)
const error = ref('')
const issue = ref(null)
const comments = ref([])

function goBack() {
  if (window.history.length > 1) {
    router.back()
  } else {
    const projKey = issueKey.split('-')[0]
    router.push('/jira/project/' + projKey)
  }
}

function getStatusClass(categoryKey) {
  if (categoryKey === 'done') return 'status-done'
  if (categoryKey === 'indeterminate') return 'status-progress'
  return 'status-open'
}

function formatDateTime(dt) {
  if (!dt) return '-'
  return dt.replace('T', ' ').substring(0, 19)
}

function renderDescription(text) {
  if (!text) return ''
  // 简单处理换行和特殊字符
  return text
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/\n/g, '<br>')
    .replace(/\[([^\]]+)\|([^\]]+)\]/g, '<a href="$2" target="_blank" style="color:var(--primary-light)">$1</a>')
    .replace(/\{code[^}]*\}([\s\S]*?)\{code\}/g, '<pre style="background:var(--bg-tertiary);padding:12px;border-radius:6px;overflow-x:auto;font-size:13px">$1</pre>')
    .replace(/\*([^*]+)\*/g, '<strong>$1</strong>')
    .replace(/_([^_]+)_/g, '<em>$1</em>')
}

onMounted(async () => {
  try {
    const res = await api.get('/api/jira/issue', { params: { key: issueKey } })
    issue.value = res.data.data || null
    // 提取评论
    if (issue.value?.fields?.comment?.comments) {
      comments.value = issue.value.fields.comment.comments
    }
  } catch (e) {
    error.value = e.response?.data?.error || '加载工单详情失败'
  }
  loading.value = false
})
</script>

<style scoped>
.issue-key-lg {
  font-family: var(--font-mono);
  font-size: 16px;
  font-weight: 600;
  color: var(--primary-light);
}

.detail-layout {
  display: grid;
  grid-template-columns: 1fr 320px;
  gap: 16px;
  align-items: start;
}
@media (max-width: 900px) {
  .detail-layout {
    grid-template-columns: 1fr;
  }
}

.section { margin-top: 20px; }
.section-title {
  font-size: 14px;
  font-weight: 600;
  color: var(--text-secondary);
  margin: 0 0 12px;
  padding-bottom: 8px;
  border-bottom: 1px solid var(--border);
}

.description-content {
  font-size: 14px;
  line-height: 1.7;
  color: var(--text-primary);
  word-break: break-word;
}

.info-rows {
  display: flex;
  flex-direction: column;
  gap: 12px;
}
.info-row {
  display: flex;
  align-items: flex-start;
  gap: 8px;
  font-size: 13px;
}
.info-label {
  color: var(--text-muted);
  min-width: 70px;
  flex-shrink: 0;
}
.info-value {
  color: var(--text-primary);
  word-break: break-word;
}
.info-value.link {
  color: var(--primary-light);
  cursor: pointer;
}
.info-value.link:hover { text-decoration: underline; }

.status-badge { font-size: 12px; }
.status-done { background: rgba(16,185,129,0.15); color: var(--success); }
.status-progress { background: rgba(76,154,255,0.15); color: var(--primary-light); }
.status-open { background: rgba(100,116,139,0.15); color: var(--text-secondary); }

.priority-highest, .priority-high { color: var(--danger); background: rgba(239,68,68,0.1); }
.priority-medium { color: var(--warning); background: rgba(245,158,11,0.1); }
.priority-low, .priority-lowest { color: var(--success); background: rgba(16,185,129,0.1); }

.comment-item {
  padding: 14px 0;
  border-bottom: 1px solid var(--border);
}
.comment-item:last-child { border-bottom: none; }
.comment-header {
  display: flex;
  align-items: center;
  gap: 12px;
  margin-bottom: 8px;
}
.comment-author {
  font-weight: 600;
  font-size: 13px;
  color: var(--text-primary);
}
.comment-date {
  font-size: 12px;
  color: var(--text-muted);
}
.comment-body {
  font-size: 14px;
  line-height: 1.6;
  color: var(--text-secondary);
}
</style>
