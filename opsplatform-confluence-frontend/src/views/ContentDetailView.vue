<template>
  <div>
    <div class="header-bar">
      <button class="btn btn-sm" @click="goBack">← 返回</button>
      <span v-if="content" class="breadcrumb">
        <router-link :to="'/spaces/' + content.space?.key">{{ content.space?.name }}</router-link>
        <span class="sep">/</span>
        <span>{{ content.title }}</span>
      </span>
    </div>

    <div v-if="loading" class="loading"><div class="spinner"></div>加载中...</div>

    <template v-else-if="content">
      <!-- 页面标题 -->
      <div class="card page-header">
        <div class="page-meta">
          <span class="badge" :class="content.type">{{ content.type === 'page' ? '页面' : '博客' }}</span>
          <span class="version">v{{ content.version?.number || 1 }}</span>
          <span class="author" v-if="content.version?.by">{{ content.version.by.displayName }}</span>
          <span class="date" v-if="content.version?.when">{{ content.version.when }}</span>
        </div>
        <h1>{{ content.title }}</h1>
      </div>

      <!-- 标签 -->
      <div class="labels" v-if="content.metadata?.labels?.results?.length">
        <span class="label-tag" v-for="label in content.metadata.labels.results" :key="label.name">{{ label.name }}</span>
      </div>

      <!-- 页面内容 -->
      <div class="card content-body" v-html="content.body?.storage?.value || '<p>暂无内容</p>'"></div>

      <!-- 子页面 -->
      <div class="card" v-if="children.length">
        <h3>子页面</h3>
        <div class="children-list">
          <router-link v-for="child in children" :key="child.id" :to="'/content/' + child.id" class="child-item">
            {{ child.title }}
          </router-link>
        </div>
      </div>

      <!-- 评论 -->
      <div class="card" v-if="comments.length">
        <h3>评论 ({{ comments.length }})</h3>
        <div class="comment" v-for="c in comments" :key="c.id">
          <div class="comment-header">
            <span class="comment-author">{{ c.version?.by?.displayName || '匿名' }}</span>
            <span class="comment-date">{{ c.version?.when || '' }}</span>
          </div>
          <div class="comment-body" v-html="c.body?.storage?.value || ''"></div>
        </div>
      </div>
    </template>
  </div>
</template>

<script setup>
import { ref, onMounted } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import api from '@/api'

const route = useRoute()
const router = useRouter()
const contentId = route.params.id
const loading = ref(true)
const content = ref(null)
const children = ref([])
const comments = ref([])

function goBack() {
  if (content.value?.space?.key) {
    router.push('/spaces/' + content.value.space.key)
  } else {
    router.back()
  }
}

onMounted(async () => {
  try {
    const [contentRes, childrenRes, commentsRes] = await Promise.all([
      api.get(`/api/confluence/content/${contentId}`),
      api.get(`/api/confluence/content/${contentId}/children?limit=50`),
      api.get(`/api/confluence/content/${contentId}/comments?limit=50`)
    ])
    content.value = contentRes.data.data
    children.value = childrenRes.data.data?.results || []
    comments.value = commentsRes.data.data?.results || []
  } catch (e) { /* ignore */ }
  finally { loading.value = false }
})
</script>

<style scoped>
.header-bar {
  display: flex;
  align-items: center;
  gap: 16px;
  margin-bottom: 16px;
}
.breadcrumb {
  font-size: 14px;
  color: var(--text-secondary);
}
.breadcrumb a {
  color: var(--primary-light);
  text-decoration: none;
}
.breadcrumb a:hover { text-decoration: underline; }
.sep { margin: 0 6px; }

.page-header { margin-bottom: 16px; }
.page-header h1 {
  font-size: 24px;
  font-weight: 700;
  color: var(--text-primary);
  margin-top: 10px;
}
.page-meta {
  display: flex;
  align-items: center;
  gap: 12px;
  font-size: 13px;
  color: var(--text-secondary);
}
.badge.page { background: rgba(76,154,255,0.15); color: #4C9AFF; }
.badge.blogpost { background: rgba(255,153,31,0.15); color: #FF991F; }
.version { font-family: var(--font-mono); }

.labels {
  display: flex;
  gap: 6px;
  flex-wrap: wrap;
  margin-bottom: 16px;
}
.label-tag {
  padding: 3px 10px;
  background: var(--bg-tertiary);
  border-radius: 9999px;
  font-size: 12px;
  color: var(--text-secondary);
}

.content-body {
  margin-bottom: 16px;
  line-height: 1.7;
  font-size: 15px;
}
.content-body :deep(img) { max-width: 100%; border-radius: var(--radius); }
.content-body :deep(code) { font-family: var(--font-mono); background: var(--bg-tertiary); padding: 2px 6px; border-radius: 4px; font-size: 13px; }
.content-body :deep(pre) { background: var(--bg-tertiary); padding: 16px; border-radius: var(--radius); overflow-x: auto; }
.content-body :deep(table) { width: 100%; border-collapse: collapse; }
.content-body :deep(td), .content-body :deep(th) { border: 1px solid var(--border); padding: 8px 12px; }

h3 {
  font-size: 14px;
  font-weight: 600;
  color: var(--text-secondary);
  margin-bottom: 12px;
}

.children-list { display: flex; flex-direction: column; gap: 4px; }
.child-item {
  padding: 8px 12px;
  border-radius: var(--radius);
  color: var(--primary-light);
  text-decoration: none;
  transition: background var(--transition);
}
.child-item:hover { background: var(--bg-hover); }

.card + .card { margin-top: 16px; }

.comment { padding: 12px 0; border-bottom: 1px solid var(--border); }
.comment:last-child { border-bottom: none; }
.comment-header { display: flex; gap: 12px; margin-bottom: 6px; }
.comment-author { font-weight: 500; font-size: 13px; }
.comment-date { font-size: 12px; color: var(--text-muted); }
.comment-body { font-size: 14px; line-height: 1.6; }
</style>
