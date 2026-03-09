<template>
  <div>
    <button class="btn" @click="$router.back()" style="margin-bottom:16px">&larr; 返回</button>

    <div v-if="loading" class="loading"><div class="spinner"></div>加载中...</div>

    <template v-else-if="issue">
      <div class="detail-layout">
        <!-- 左侧主体 -->
        <div class="detail-main">
          <div class="card">
            <div class="issue-header">
              <span class="issue-key">{{ issue.key }}</span>
              <span class="badge" :style="statusStyle(issue.fields?.status)">{{ issue.fields?.status?.name }}</span>
            </div>
            <h2 class="issue-title">{{ issue.fields?.summary }}</h2>

            <!-- 状态扭转按钮 -->
            <div class="transitions" v-if="transitions.length && authStore.hasPermission('jira:transition')">
              <span class="transitions-label">扭转状态：</span>
              <button
                v-for="t in transitions"
                :key="t.id"
                class="btn btn-sm transition-btn"
                :class="transitionBtnClass(t)"
                :disabled="transitioning"
                @click="doTransition(t)"
              >
                {{ t.name }}
              </button>
            </div>

            <div class="section">
              <h3>描述</h3>
              <div class="description" v-if="issue.renderedFields?.description" v-html="issue.renderedFields.description"></div>
              <div class="description empty" v-else>暂无描述</div>
            </div>
          </div>

          <!-- 评论 -->
          <div class="card" v-if="comments.length">
            <h3>评论 ({{ comments.length }})</h3>
            <div class="comment" v-for="c in comments" :key="c.id">
              <div class="comment-header">
                <strong>{{ c.author?.displayName || c.author?.name }}</strong>
                <span class="comment-date">{{ formatDate(c.created) }}</span>
              </div>
              <div class="comment-body" v-html="c.renderedBody || c.body"></div>
            </div>
          </div>

          <!-- 变更历史 -->
          <div class="card" v-if="changelog.length">
            <h3>变更历史</h3>
            <div class="changelog-item" v-for="h in changelog" :key="h.id">
              <div class="changelog-header">
                <strong>{{ h.author?.displayName || h.author?.name }}</strong>
                <span class="comment-date">{{ formatDate(h.created) }}</span>
              </div>
              <div class="changelog-changes" v-for="item in h.items" :key="item.field">
                <span class="field-name">{{ fieldsStore.getFieldLabel(item.fieldId || item.field) }}</span>:
                <span class="old-value">{{ item.fromString || '空' }}</span> →
                <span class="new-value">{{ item.toString || '空' }}</span>
              </div>
            </div>
          </div>
        </div>

        <!-- 右侧属性面板 -->
        <div class="detail-sidebar">
          <!-- 标准属性 -->
          <div class="card">
            <h3>属性</h3>
            <div class="prop-list">
              <div class="prop" v-for="field in standardDetailFields" :key="field.id">
                <span class="prop-label">{{ fieldsStore.getFieldLabel(field.id) }}</span>
                <span>{{ fieldsStore.getFieldDisplayValue(issue, field.id) }}</span>
              </div>
            </div>

            <!-- 标签/组件等数组字段 -->
            <template v-for="field in arrayDetailFields" :key="field.id">
              <div v-if="hasArrayValue(issue, field.id)" style="margin-top:12px">
                <span class="prop-label">{{ fieldsStore.getFieldLabel(field.id) }}</span>
                <div class="labels">
                  <span class="label-tag" v-for="v in getArrayValues(issue, field.id)" :key="v">{{ v }}</span>
                </div>
              </div>
            </template>
          </div>

          <!-- 自定义字段 -->
          <div class="card" v-if="customDetailFields.length">
            <h3>自定义字段</h3>
            <div class="prop-list">
              <template v-for="field in customDetailFields" :key="field.id">
                <div class="prop" v-if="hasFieldValue(issue, field.id)">
                  <span class="prop-label">{{ field.name }}</span>
                  <span :class="{ 'prop-value-long': isLongValue(issue, field.id) }">
                    {{ fieldsStore.getFieldDisplayValue(issue, field.id) }}
                  </span>
                </div>
              </template>
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
const fieldsStore = useFieldsStore()
const authStore = useAuthStore()
const loading = ref(true)
const issue = ref(null)
const comments = ref([])
const changelog = ref([])
const transitions = ref([])
const transitioning = ref(false)

// 标准字段（右侧属性面板，非数组类型）
const standardDetailFields = computed(() => {
  const show = ['issuetype', 'priority', 'assignee', 'reporter', 'project', 'resolution', 'created', 'updated', 'resolutiondate', 'duedate', 'creator']
  return show.map(id => ({ id })).filter(f => {
    const val = issue.value?.fields?.[f.id]
    return val !== null && val !== undefined
  })
})

// 数组类型标准字段
const arrayDetailFields = computed(() => {
  return ['labels', 'components', 'fixVersions'].map(id => ({ id }))
})

// 自定义字段（有值的）
const customDetailFields = computed(() => {
  if (!fieldsStore.loaded || !issue.value) return []
  return fieldsStore.customFields.filter(f => {
    const val = issue.value.fields?.[f.id]
    return val !== null && val !== undefined && val !== '' && !(Array.isArray(val) && val.length === 0)
  })
})

function hasFieldValue(issue, fieldId) {
  const val = issue.fields?.[fieldId]
  if (val === null || val === undefined || val === '') return false
  if (Array.isArray(val) && val.length === 0) return false
  return true
}

function hasArrayValue(issue, fieldId) {
  const val = issue.fields?.[fieldId]
  return Array.isArray(val) && val.length > 0
}

function getArrayValues(issue, fieldId) {
  const val = issue.fields?.[fieldId]
  if (!Array.isArray(val)) return []
  return val.map(v => {
    if (typeof v === 'string') return v
    return v.displayName || v.name || v.value || String(v)
  })
}

function isLongValue(issue, fieldId) {
  const display = fieldsStore.getFieldDisplayValue(issue, fieldId)
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
  if (!confirm(`确定将工单状态扭转为「${t.to?.name || t.name}」？`)) return
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
  if (cat === 'done') return 'btn-done'
  if (cat === 'indeterminate') return 'btn-progress'
  return 'btn-new'
}

function formatDate(d) {
  if (!d) return '-'
  return new Date(d).toLocaleString('zh-CN')
}

function statusStyle(status) {
  if (!status) return {}
  const cat = status.statusCategory?.key
  const colors = { done: '#10B981', new: '#3B82F6', indeterminate: '#F59E0B' }
  const bg = colors[cat] || '#94A3B8'
  return { background: bg + '20', color: bg, borderColor: bg + '40' }
}
</script>

<style scoped>
.detail-layout { display: flex; gap: 20px; }
.detail-main { flex: 1; display: flex; flex-direction: column; gap: 16px; min-width: 0; }
.detail-sidebar { width: 320px; flex-shrink: 0; display: flex; flex-direction: column; gap: 16px; }

.issue-header { display: flex; align-items: center; gap: 12px; margin-bottom: 8px; }
.issue-key {
  font-family: var(--font-mono);
  font-size: 14px;
  font-weight: 600;
  color: var(--primary-light);
  background: rgba(59,130,246,0.1);
  padding: 2px 8px;
  border-radius: 4px;
}
.badge { border: 1px solid; padding: 2px 10px; }
.issue-title { font-size: 20px; font-weight: 600; margin-bottom: 16px; }

.transitions { display: flex; align-items: center; gap: 8px; flex-wrap: wrap; margin-bottom: 20px; padding: 12px; background: var(--card-bg, rgba(30,40,60,0.5)); border-radius: var(--radius); border: 1px solid var(--border); }
.transitions-label { font-size: 13px; color: var(--text-secondary); font-weight: 500; }
.transition-btn { font-size: 12px; padding: 4px 14px; border-radius: 20px; }
.btn-done { background: rgba(16,185,129,0.15); color: #10B981; border: 1px solid rgba(16,185,129,0.3); }
.btn-done:hover { background: rgba(16,185,129,0.25); }
.btn-progress { background: rgba(245,158,11,0.15); color: #F59E0B; border: 1px solid rgba(245,158,11,0.3); }
.btn-progress:hover { background: rgba(245,158,11,0.25); }
.btn-new { background: rgba(59,130,246,0.15); color: #3B82F6; border: 1px solid rgba(59,130,246,0.3); }
.btn-new:hover { background: rgba(59,130,246,0.25); }

.section h3 { font-size: 14px; color: var(--text-secondary); margin-bottom: 8px; }
.description { font-size: 14px; line-height: 1.7; color: var(--text-primary); }
.description.empty { color: var(--text-muted); font-style: italic; }
.description :deep(img) { max-width: 100%; border-radius: var(--radius); }

.comment { padding: 12px 0; border-bottom: 1px solid var(--border); }
.comment:last-child { border-bottom: none; }
.comment-header { display: flex; align-items: center; gap: 8px; margin-bottom: 6px; font-size: 13px; }
.comment-date { color: var(--text-muted); font-size: 12px; }
.comment-body { font-size: 14px; line-height: 1.6; }

.changelog-item { padding: 8px 0; border-bottom: 1px solid var(--border); font-size: 13px; }
.changelog-header { display: flex; gap: 8px; margin-bottom: 4px; }
.field-name { font-weight: 600; color: var(--text-secondary); }
.old-value { color: var(--danger); text-decoration: line-through; }
.new-value { color: var(--success); }

.prop-list { display: flex; flex-direction: column; gap: 10px; margin-top: 12px; }
.prop { display: flex; justify-content: space-between; font-size: 13px; gap: 8px; }
.prop-label { color: var(--text-muted); font-size: 12px; flex-shrink: 0; }
.prop-value-long { font-size: 12px; word-break: break-all; text-align: right; max-width: 200px; }

.labels { display: flex; flex-wrap: wrap; gap: 4px; margin-top: 4px; }
.label-tag {
  font-size: 11px;
  padding: 2px 8px;
  background: rgba(59,130,246,0.1);
  color: var(--primary-light);
  border-radius: 9999px;
}

@media (max-width: 768px) {
  .detail-layout { flex-direction: column; }
  .detail-sidebar { width: 100%; }
}
</style>
