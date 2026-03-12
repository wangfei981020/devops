<template>
  <div class="issue-detail-page">
    <!-- 顶部导航栏 -->
    <div class="detail-topbar">
      <button class="back-btn" @click="$router.back()">
        <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="m15 18-6-6 6-6"/></svg>
        返回列表
      </button>
      <div class="topbar-meta" v-if="issue">
        <span class="issue-key-inline">{{ issue.key }}</span>
        <span class="separator">/</span>
        <span class="project-name">{{ issue.fields?.project?.name }}</span>
      </div>
    </div>

    <div v-if="loading" class="loading"><div class="spinner"></div>加载中...</div>

    <template v-else-if="issue">
      <div class="detail-layout">
        <!-- 左侧主体 -->
        <div class="detail-main">
          <!-- 标题卡片 -->
          <div class="card hero-card">
            <div class="hero-top">
              <div class="issue-badges">
                <span class="issue-key">{{ issue.key }}</span>
                <span class="status-badge" :class="statusClass(issue.fields?.status)">
                  <span class="status-dot"></span>
                  {{ issue.fields?.status?.name }}
                </span>
                <span class="type-badge" v-if="issue.fields?.issuetype">
                  {{ issue.fields.issuetype.name }}
                </span>
                <span class="priority-badge" v-if="issue.fields?.priority" :class="'priority-' + (issue.fields.priority.name || '').toLowerCase()">
                  {{ issue.fields.priority.name }}
                </span>
              </div>
            </div>
            <h1 class="issue-title">{{ issue.fields?.summary }}</h1>

            <!-- 状态扭转 -->
            <div class="transitions-bar" v-if="transitions.length && authStore.hasPermission('jira:transition')">
              <svg class="transitions-icon" width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M18 8L22 12L18 16"/><path d="M2 12H22"/></svg>
              <span class="transitions-label">扭转状态</span>
              <div class="transitions-btns">
                <button
                  v-for="t in transitions"
                  :key="t.id"
                  class="transition-btn"
                  :class="transitionBtnClass(t)"
                  :disabled="transitioning"
                  @click="doTransition(t)"
                >
                  <svg v-if="transitioning" class="btn-spinner" width="14" height="14" viewBox="0 0 24 24"><circle cx="12" cy="12" r="10" stroke="currentColor" stroke-width="3" fill="none" stroke-dasharray="31.4" stroke-linecap="round"><animateTransform attributeName="transform" type="rotate" from="0 12 12" to="360 12 12" dur="0.8s" repeatCount="indefinite"/></circle></svg>
                  {{ t.name }}
                </button>
              </div>
            </div>
          </div>

          <!-- 描述 -->
          <div class="card section-card">
            <div class="section-header">
              <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8z"/><polyline points="14 2 14 8 20 8"/><line x1="16" y1="13" x2="8" y2="13"/><line x1="16" y1="17" x2="8" y2="17"/></svg>
              <h3>描述</h3>
            </div>
            <div class="description-content" v-if="issue.renderedFields?.description" v-html="issue.renderedFields.description"></div>
            <div class="description-empty" v-else>
              <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="10"/><path d="M12 16v-4"/><path d="M12 8h.01"/></svg>
              暂无描述
            </div>
          </div>

          <!-- 评论 -->
          <div class="card section-card" v-if="comments.length">
            <div class="section-header">
              <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M21 15a2 2 0 0 1-2 2H7l-4 4V5a2 2 0 0 1 2-2h14a2 2 0 0 1 2 2z"/></svg>
              <h3>评论</h3>
              <span class="section-count">{{ comments.length }}</span>
            </div>
            <div class="comments-list">
              <div class="comment-item" v-for="c in comments" :key="c.id">
                <div class="comment-avatar">{{ (c.author?.displayName || c.author?.name || '?')[0].toUpperCase() }}</div>
                <div class="comment-content">
                  <div class="comment-meta">
                    <span class="comment-author">{{ c.author?.displayName || c.author?.name }}</span>
                    <span class="comment-time">{{ formatRelativeDate(c.created) }}</span>
                  </div>
                  <div class="comment-body" v-html="c.renderedBody || c.body"></div>
                </div>
              </div>
            </div>
          </div>

          <!-- 变更历史 -->
          <div class="card section-card" v-if="changelog.length">
            <div class="section-header">
              <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><polyline points="12 8 12 12 14 14"/><circle cx="12" cy="12" r="10"/></svg>
              <h3>变更历史</h3>
              <span class="section-count">{{ changelog.length }}</span>
            </div>
            <div class="timeline">
              <div class="timeline-item" v-for="h in changelog" :key="h.id">
                <div class="timeline-dot"></div>
                <div class="timeline-content">
                  <div class="timeline-meta">
                    <span class="timeline-author">{{ h.author?.displayName || h.author?.name }}</span>
                    <span class="timeline-time">{{ formatRelativeDate(h.created) }}</span>
                  </div>
                  <div class="timeline-changes">
                    <div class="change-row" v-for="item in h.items" :key="item.field">
                      <span class="change-field">{{ fieldsStore.getFieldLabel(item.fieldId || item.field) }}</span>
                      <div class="change-values">
                        <span class="change-old" v-if="item.fromString">{{ item.fromString }}</span>
                        <svg class="change-arrow" width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M5 12h14"/><path d="m12 5 7 7-7 7"/></svg>
                        <span class="change-new">{{ item.toString || '空' }}</span>
                      </div>
                    </div>
                  </div>
                </div>
              </div>
            </div>
          </div>
        </div>

        <!-- 右侧属性面板 -->
        <div class="detail-sidebar">
          <!-- 人员信息 -->
          <div class="card sidebar-card">
            <h3 class="sidebar-title">
              <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M20 21v-2a4 4 0 0 0-4-4H8a4 4 0 0 0-4 4v2"/><circle cx="12" cy="7" r="4"/></svg>
              人员
            </h3>
            <div class="people-list">
              <div class="people-item" v-for="field in peopleFields" :key="field.id">
                <span class="people-role">{{ fieldsStore.getFieldLabel(field.id) }}</span>
                <div class="people-user">
                  <span class="user-avatar-sm">{{ (fieldsStore.getFieldDisplayValue(issue, field.id) || '?')[0].toUpperCase() }}</span>
                  <span>{{ fieldsStore.getFieldDisplayValue(issue, field.id) }}</span>
                </div>
              </div>
            </div>
          </div>

          <!-- 详细信息 -->
          <div class="card sidebar-card">
            <h3 class="sidebar-title">
              <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="10"/><path d="M12 16v-4"/><path d="M12 8h.01"/></svg>
              详细信息
            </h3>
            <div class="info-list">
              <div class="info-row" v-for="field in infoFields" :key="field.id">
                <span class="info-label">{{ fieldsStore.getFieldLabel(field.id) }}</span>
                <span class="info-value">{{ fieldsStore.getFieldDisplayValue(issue, field.id) }}</span>
              </div>
            </div>
          </div>

          <!-- 日期 -->
          <div class="card sidebar-card">
            <h3 class="sidebar-title">
              <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><rect x="3" y="4" width="18" height="18" rx="2" ry="2"/><line x1="16" y1="2" x2="16" y2="6"/><line x1="8" y1="2" x2="8" y2="6"/><line x1="3" y1="10" x2="21" y2="10"/></svg>
              日期
            </h3>
            <div class="info-list">
              <div class="info-row" v-for="field in dateFields" :key="field.id">
                <span class="info-label">{{ fieldsStore.getFieldLabel(field.id) }}</span>
                <span class="info-value">{{ fieldsStore.getFieldDisplayValue(issue, field.id) }}</span>
              </div>
            </div>
          </div>

          <!-- 标签/组件 -->
          <template v-for="field in arrayDetailFields" :key="field.id">
            <div class="card sidebar-card" v-if="hasArrayValue(issue, field.id)">
              <h3 class="sidebar-title">
                <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M20.59 13.41l-7.17 7.17a2 2 0 0 1-2.83 0L2 12V2h10l8.59 8.59a2 2 0 0 1 0 2.82z"/><line x1="7" y1="7" x2="7.01" y2="7"/></svg>
                {{ fieldsStore.getFieldLabel(field.id) }}
              </h3>
              <div class="tag-list">
                <span class="tag-item" v-for="v in getArrayValues(issue, field.id)" :key="v">{{ v }}</span>
              </div>
            </div>
          </template>

          <!-- 自定义字段 -->
          <div class="card sidebar-card" v-if="visibleCustomFields.length">
            <h3 class="sidebar-title">
              <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M12 3H5a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2h14a2 2 0 0 0 2-2v-7"/><path d="M18.375 2.625a2.121 2.121 0 1 1 3 3L12 15l-4 1 1-4Z"/></svg>
              自定义字段
            </h3>
            <div class="info-list">
              <div class="info-row" v-for="field in visibleCustomFields" :key="field.id">
                <span class="info-label">{{ field.name }}</span>
                <span class="info-value" :class="{ 'info-value-wrap': isLongValue(issue, field.id) }">
                  {{ fieldsStore.getFieldDisplayValue(issue, field.id) }}
                </span>
              </div>
            </div>
          </div>
        </div>
      </div>
    </template>
  </div>
</template>

<script setup>
import { ref, computed, onMounted, inject } from 'vue'
import { useRoute } from 'vue-router'
import api from '@/api'
import { useFieldsStore } from '@/stores/fields'
import { useAuthStore } from '@/stores/auth'

const route = useRoute()
const toast = inject('toast')
const confirm = inject('confirm')
const fieldsStore = useFieldsStore()
const authStore = useAuthStore()
const loading = ref(true)
const issue = ref(null)
const comments = ref([])
const changelog = ref([])
const transitions = ref([])
const transitioning = ref(false)

// 人员字段
const peopleFields = computed(() => {
  const ids = ['assignee', 'reporter', 'creator']
  return ids.map(id => ({ id })).filter(f => {
    const val = issue.value?.fields?.[f.id]
    return val !== null && val !== undefined
  })
})

// 信息字段（非人员、非日期）
const infoFields = computed(() => {
  const ids = ['issuetype', 'priority', 'project', 'resolution']
  return ids.map(id => ({ id })).filter(f => {
    const val = issue.value?.fields?.[f.id]
    return val !== null && val !== undefined
  })
})

// 日期字段
const dateFields = computed(() => {
  const ids = ['created', 'updated', 'resolutiondate', 'duedate']
  return ids.map(id => ({ id })).filter(f => {
    const val = issue.value?.fields?.[f.id]
    return val !== null && val !== undefined
  })
})

// 数组类型标准字段
const arrayDetailFields = computed(() => {
  return ['labels', 'components', 'fixVersions'].map(id => ({ id }))
})

// 过滤掉内部对象格式的自定义字段（如 Development 等 JSON dump）
const HIDDEN_PATTERNS = ['com.atlassian', 'Bean@', 'summaryBean', '{', 'stateCount']
const visibleCustomFields = computed(() => {
  if (!fieldsStore.loaded || !issue.value) return []
  return fieldsStore.customFields.filter(f => {
    const val = issue.value.fields?.[f.id]
    if (val === null || val === undefined || val === '') return false
    if (Array.isArray(val) && val.length === 0) return false
    const display = fieldsStore.getFieldDisplayValue(issue.value, f.id)
    return !HIDDEN_PATTERNS.some(p => display.includes(p))
  })
})

function hasArrayValue(iss, fieldId) {
  const val = iss.fields?.[fieldId]
  return Array.isArray(val) && val.length > 0
}

function getArrayValues(iss, fieldId) {
  const val = iss.fields?.[fieldId]
  if (!Array.isArray(val)) return []
  return val.map(v => {
    if (typeof v === 'string') return v
    return v.displayName || v.name || v.value || String(v)
  })
}

function isLongValue(iss, fieldId) {
  const display = fieldsStore.getFieldDisplayValue(iss, fieldId)
  return display.length > 30
}

onMounted(async () => {
  await fieldsStore.fetchFields()
  const key = route.params.key
  try {
    const [issueRes, commentsRes, transRes] = await Promise.all([
      api.get(`/api/jira/issues/${key}?expand=changelog,renderedFields`),
      api.get(`/api/jira/issues/${key}/comments`),
      api.get(`/api/jira/issues/${key}/transitions`)
    ])
    issue.value = issueRes.data.data
    comments.value = commentsRes.data.data?.comments || []
    changelog.value = issue.value?.changelog?.histories || []
    transitions.value = transRes.data.data?.transitions || []
  } catch (e) {
    console.error('获取工单详情失败:', e)
  } finally {
    loading.value = false
  }
})

async function doTransition(t) {
  const targetName = t.to?.name || t.name
  const ok = await confirm({
    title: '状态扭转',
    message: `确定将工单状态扭转为「${targetName}」？`,
    type: 'warning',
    okText: '确定扭转'
  })
  if (!ok) return
  transitioning.value = true
  try {
    const key = route.params.key
    await api.post(`/api/jira/issues/${key}/transitions`, { transition_id: t.id })
    toast?.('状态扭转成功', 'success')
    const [issueRes, transRes] = await Promise.all([
      api.get(`/api/jira/issues/${key}?expand=changelog,renderedFields`),
      api.get(`/api/jira/issues/${key}/transitions`)
    ])
    issue.value = issueRes.data.data
    changelog.value = issue.value?.changelog?.histories || []
    transitions.value = transRes.data.data?.transitions || []
  } catch (e) {
    toast?.(e.response?.data?.error || '扭转失败', 'error')
  } finally {
    transitioning.value = false
  }
}

function transitionBtnClass(t) {
  const cat = t.to?.statusCategory?.key
  if (cat === 'done') return 'tr-done'
  if (cat === 'indeterminate') return 'tr-progress'
  return 'tr-new'
}

function statusClass(status) {
  if (!status) return ''
  const cat = status.statusCategory?.key
  if (cat === 'done') return 'status-done'
  if (cat === 'indeterminate') return 'status-progress'
  if (cat === 'new') return 'status-new'
  return 'status-default'
}

function formatDate(d) {
  if (!d) return '-'
  return new Date(d).toLocaleString('zh-CN')
}

function formatRelativeDate(d) {
  if (!d) return '-'
  const now = new Date()
  const date = new Date(d)
  const diff = now - date
  const mins = Math.floor(diff / 60000)
  if (mins < 1) return '刚刚'
  if (mins < 60) return `${mins} 分钟前`
  const hours = Math.floor(mins / 60)
  if (hours < 24) return `${hours} 小时前`
  const days = Math.floor(hours / 24)
  if (days < 7) return `${days} 天前`
  return date.toLocaleString('zh-CN')
}
</script>

<style scoped>
.issue-detail-page {
  max-width: 1400px;
}

/* 顶部导航 */
.detail-topbar {
  display: flex;
  align-items: center;
  gap: 16px;
  margin-bottom: 20px;
  padding-bottom: 16px;
  border-bottom: 1px solid var(--border);
}
.back-btn {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  padding: 6px 14px;
  background: var(--bg-secondary);
  border: 1px solid var(--border);
  border-radius: var(--radius);
  color: var(--text-secondary);
  font-size: 13px;
  font-family: var(--font-sans);
  cursor: pointer;
  transition: all var(--transition);
}
.back-btn:hover {
  background: var(--bg-hover);
  color: var(--text-primary);
  border-color: var(--border-light);
}
.topbar-meta {
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: 13px;
  color: var(--text-muted);
}
.issue-key-inline {
  font-family: var(--font-mono);
  color: var(--primary-light);
  font-weight: 600;
}
.separator { color: var(--border-light); }
.project-name { color: var(--text-secondary); }

/* 布局 */
.detail-layout { display: flex; gap: 24px; }
.detail-main { flex: 1; display: flex; flex-direction: column; gap: 20px; min-width: 0; }
.detail-sidebar { width: 340px; flex-shrink: 0; display: flex; flex-direction: column; gap: 16px; }

/* Hero 卡片 */
.hero-card {
  padding: 24px 28px;
  border-top: 3px solid var(--primary-light);
}
.hero-top { margin-bottom: 16px; }
.issue-badges {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-wrap: wrap;
}
.issue-key {
  font-family: var(--font-mono);
  font-size: 13px;
  font-weight: 700;
  color: var(--primary-light);
  background: rgba(59,130,246,0.1);
  padding: 3px 10px;
  border-radius: var(--radius-sm);
  letter-spacing: 0.02em;
}

/* 状态徽章 */
.status-badge {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  padding: 3px 12px;
  border-radius: 9999px;
  font-size: 12px;
  font-weight: 600;
  letter-spacing: 0.02em;
}
.status-dot {
  width: 7px;
  height: 7px;
  border-radius: 50%;
  background: currentColor;
}
.status-done { background: rgba(16,185,129,0.12); color: #10B981; }
.status-progress { background: rgba(245,158,11,0.12); color: #F59E0B; }
.status-new { background: rgba(59,130,246,0.12); color: #3B82F6; }
.status-default { background: rgba(148,163,184,0.12); color: #94A3B8; }

.type-badge {
  font-size: 11px;
  padding: 2px 8px;
  background: rgba(148,163,184,0.1);
  color: var(--text-secondary);
  border-radius: var(--radius-sm);
  font-weight: 500;
}
.priority-badge {
  font-size: 11px;
  padding: 2px 8px;
  border-radius: var(--radius-sm);
  font-weight: 600;
}
.priority-highest, .priority-blocker { background: rgba(239,68,68,0.12); color: #EF4444; }
.priority-high { background: rgba(249,115,22,0.12); color: #F97316; }
.priority-medium { background: rgba(245,158,11,0.12); color: #F59E0B; }
.priority-low { background: rgba(59,130,246,0.12); color: #3B82F6; }
.priority-lowest { background: rgba(148,163,184,0.1); color: #94A3B8; }

.issue-title {
  font-size: 22px;
  font-weight: 700;
  line-height: 1.4;
  color: var(--text-primary);
  margin: 0;
}

/* 扭转状态栏 */
.transitions-bar {
  display: flex;
  align-items: center;
  gap: 10px;
  margin-top: 20px;
  padding: 14px 16px;
  background: var(--bg-primary);
  border-radius: var(--radius);
  border: 1px solid var(--border);
}
.transitions-icon { color: var(--text-muted); flex-shrink: 0; }
.transitions-label {
  font-size: 12px;
  color: var(--text-muted);
  font-weight: 500;
  flex-shrink: 0;
  text-transform: uppercase;
  letter-spacing: 0.05em;
}
.transitions-btns { display: flex; gap: 8px; flex-wrap: wrap; }
.transition-btn {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  padding: 5px 16px;
  border-radius: 9999px;
  font-size: 12px;
  font-weight: 600;
  font-family: var(--font-sans);
  cursor: pointer;
  transition: all var(--transition);
  border: 1px solid transparent;
}
.transition-btn:disabled { opacity: 0.5; cursor: wait; }
.btn-spinner { animation: spin 0.8s linear infinite; }
.tr-done { background: rgba(16,185,129,0.12); color: #10B981; border-color: rgba(16,185,129,0.25); }
.tr-done:hover:not(:disabled) { background: rgba(16,185,129,0.22); }
.tr-progress { background: rgba(245,158,11,0.12); color: #F59E0B; border-color: rgba(245,158,11,0.25); }
.tr-progress:hover:not(:disabled) { background: rgba(245,158,11,0.22); }
.tr-new { background: rgba(59,130,246,0.12); color: #3B82F6; border-color: rgba(59,130,246,0.25); }
.tr-new:hover:not(:disabled) { background: rgba(59,130,246,0.22); }

/* Section 卡片 */
.section-card { padding: 24px 28px; }
.section-header {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-bottom: 16px;
  color: var(--text-secondary);
}
.section-header h3 {
  font-size: 15px;
  font-weight: 600;
  color: var(--text-primary);
  margin: 0;
}
.section-header svg { flex-shrink: 0; }
.section-count {
  font-size: 11px;
  font-weight: 600;
  background: var(--bg-tertiary);
  color: var(--text-muted);
  padding: 1px 7px;
  border-radius: 9999px;
  margin-left: 2px;
}

/* 描述 */
.description-content {
  font-size: 14px;
  line-height: 1.8;
  color: var(--text-primary);
}
.description-content :deep(img) { max-width: 100%; border-radius: var(--radius); margin: 8px 0; }
.description-content :deep(a) { color: var(--primary-light); }
.description-content :deep(code) {
  background: var(--bg-tertiary);
  padding: 1px 6px;
  border-radius: 4px;
  font-family: var(--font-mono);
  font-size: 13px;
}
.description-content :deep(pre) {
  background: var(--bg-primary);
  padding: 14px;
  border-radius: var(--radius);
  overflow-x: auto;
  border: 1px solid var(--border);
}
.description-empty {
  display: flex;
  align-items: center;
  gap: 8px;
  color: var(--text-muted);
  font-size: 14px;
  font-style: italic;
  padding: 20px 0;
}

/* 评论 */
.comments-list { display: flex; flex-direction: column; gap: 2px; }
.comment-item {
  display: flex;
  gap: 12px;
  padding: 14px 0;
  border-bottom: 1px solid var(--border);
}
.comment-item:last-child { border-bottom: none; padding-bottom: 0; }
.comment-avatar {
  width: 34px;
  height: 34px;
  border-radius: 50%;
  background: linear-gradient(135deg, var(--primary), var(--primary-light));
  color: #fff;
  display: flex;
  align-items: center;
  justify-content: center;
  font-size: 14px;
  font-weight: 700;
  flex-shrink: 0;
}
.comment-content { flex: 1; min-width: 0; }
.comment-meta {
  display: flex;
  align-items: center;
  gap: 10px;
  margin-bottom: 6px;
}
.comment-author { font-size: 13px; font-weight: 600; color: var(--text-primary); }
.comment-time { font-size: 12px; color: var(--text-muted); }
.comment-body {
  font-size: 14px;
  line-height: 1.7;
  color: var(--text-secondary);
}

/* 时间线 */
.timeline {
  position: relative;
  padding-left: 24px;
}
.timeline::before {
  content: '';
  position: absolute;
  left: 7px;
  top: 6px;
  bottom: 6px;
  width: 2px;
  background: var(--border);
  border-radius: 1px;
}
.timeline-item {
  position: relative;
  padding-bottom: 20px;
}
.timeline-item:last-child { padding-bottom: 0; }
.timeline-dot {
  position: absolute;
  left: -20px;
  top: 6px;
  width: 12px;
  height: 12px;
  border-radius: 50%;
  background: var(--bg-card);
  border: 2px solid var(--primary-light);
  z-index: 1;
}
.timeline-meta {
  display: flex;
  align-items: center;
  gap: 10px;
  margin-bottom: 8px;
}
.timeline-author { font-size: 13px; font-weight: 600; color: var(--text-primary); }
.timeline-time { font-size: 12px; color: var(--text-muted); }
.timeline-changes { display: flex; flex-direction: column; gap: 4px; }
.change-row {
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: 13px;
  flex-wrap: wrap;
}
.change-field {
  font-weight: 500;
  color: var(--text-secondary);
  background: var(--bg-tertiary);
  padding: 1px 8px;
  border-radius: 4px;
  font-size: 12px;
}
.change-values {
  display: flex;
  align-items: center;
  gap: 6px;
}
.change-old {
  color: var(--danger);
  text-decoration: line-through;
  opacity: 0.8;
  font-size: 12px;
}
.change-arrow { color: var(--text-muted); flex-shrink: 0; }
.change-new {
  color: var(--success);
  font-weight: 500;
  font-size: 12px;
}

/* 侧边栏卡片 */
.sidebar-card { padding: 18px 20px; }
.sidebar-title {
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: 12px;
  font-weight: 600;
  color: var(--text-muted);
  text-transform: uppercase;
  letter-spacing: 0.06em;
  margin-bottom: 14px;
  padding-bottom: 10px;
  border-bottom: 1px solid var(--border);
}
.sidebar-title svg { color: var(--text-muted); }

/* 人员列表 */
.people-list { display: flex; flex-direction: column; gap: 12px; }
.people-item { display: flex; align-items: center; justify-content: space-between; }
.people-role { font-size: 12px; color: var(--text-muted); }
.people-user {
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: 13px;
  font-weight: 500;
  color: var(--text-primary);
}
.user-avatar-sm {
  width: 24px;
  height: 24px;
  border-radius: 50%;
  background: linear-gradient(135deg, var(--primary), var(--primary-light));
  color: #fff;
  display: flex;
  align-items: center;
  justify-content: center;
  font-size: 11px;
  font-weight: 700;
}

/* 信息列表 */
.info-list { display: flex; flex-direction: column; gap: 11px; }
.info-row {
  display: flex;
  justify-content: space-between;
  align-items: flex-start;
  gap: 12px;
}
.info-label {
  font-size: 12px;
  color: var(--text-muted);
  flex-shrink: 0;
}
.info-value {
  font-size: 13px;
  color: var(--text-primary);
  font-weight: 500;
  text-align: right;
}
.info-value-wrap {
  font-size: 12px;
  word-break: break-all;
  max-width: 200px;
  line-height: 1.5;
}

/* 标签 */
.tag-list {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
}
.tag-item {
  font-size: 12px;
  padding: 3px 10px;
  background: rgba(59,130,246,0.08);
  color: var(--primary-light);
  border-radius: 9999px;
  border: 1px solid rgba(59,130,246,0.15);
  font-weight: 500;
}

@keyframes spin { to { transform: rotate(360deg); } }

@media (max-width: 900px) {
  .detail-layout { flex-direction: column; }
  .detail-sidebar { width: 100%; }
}
</style>
