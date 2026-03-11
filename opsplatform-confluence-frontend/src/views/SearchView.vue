<template>
  <div>
    <div class="search-bar card">
      <input class="input" v-model="keyword" placeholder="搜索页面内容..." @keyup.enter="doSearch" />
      <button class="btn btn-primary" @click="doSearch" :disabled="searching">{{ searching ? '搜索中...' : '搜索' }}</button>
    </div>

    <div v-if="searching" class="loading"><div class="spinner"></div>搜索中...</div>

    <div v-else-if="searched" class="card">
      <h3>搜索结果 ({{ results.length }})</h3>
      <div v-if="!results.length" style="text-align:center;padding:20px;color:var(--text-muted)">未找到匹配内容</div>

      <div class="table-wrapper" v-else>
        <table>
          <thead>
            <tr>
              <th>标题</th>
              <th>空间</th>
              <th>类型</th>
              <th>最后更新</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="item in results" :key="item.content?.id" @click="$router.push('/content/' + item.content?.id)">
              <td>{{ item.content?.title || item.title }}</td>
              <td>{{ item.content?.space?.name || '-' }}</td>
              <td><span class="badge" :class="item.content?.type">{{ item.content?.type === 'page' ? '页面' : '博客' }}</span></td>
              <td class="date-cell">{{ item.lastModified || '-' }}</td>
            </tr>
          </tbody>
        </table>
      </div>
    </div>
  </div>
</template>

<script setup>
import { ref } from 'vue'
import api from '@/api'

const keyword = ref('')
const searching = ref(false)
const searched = ref(false)
const results = ref([])

async function doSearch() {
  if (!keyword.value.trim()) return
  searching.value = true
  searched.value = false
  try {
    const res = await api.get('/api/confluence/search', { params: { keyword: keyword.value, limit: 50 } })
    results.value = res.data.data?.results || []
  } catch (e) {
    results.value = []
  } finally {
    searching.value = false
    searched.value = true
  }
}
</script>

<style scoped>
.search-bar {
  display: flex;
  gap: 12px;
  align-items: center;
  margin-bottom: 16px;
}
.search-bar .input { flex: 1; }
h3 {
  font-size: 14px;
  font-weight: 600;
  color: var(--text-secondary);
  margin-bottom: 12px;
}
.badge.page { background: rgba(76,154,255,0.15); color: #4C9AFF; }
.badge.blogpost { background: rgba(255,153,31,0.15); color: #FF991F; }
.date-cell { font-size: 13px; color: var(--text-secondary); white-space: nowrap; }
</style>
