<template>
  <div>
    <div v-if="loading" class="loading"><div class="spinner"></div>加载中...</div>
    <div v-else>
      <div class="table-wrapper">
        <table>
          <thead>
            <tr>
              <th>Key</th>
              <th>项目名称</th>
              <th>负责人</th>
              <th>类型</th>
              <th>操作</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="p in projects" :key="p.key" @click="viewProject(p.key)">
              <td><span class="project-key">{{ p.key }}</span></td>
              <td>{{ p.name }}</td>
              <td>{{ p.lead?.displayName || p.lead?.name || '-' }}</td>
              <td>{{ p.projectTypeKey || '-' }}</td>
              <td>
                <button class="btn btn-sm btn-primary" @click.stop="viewIssues(p.key)">查看工单</button>
              </td>
            </tr>
          </tbody>
        </table>
      </div>
      <div v-if="error" class="error-tip">
        <span class="error-icon">⚠</span>
        <div>
          <div class="error-title">获取项目失败</div>
          <div class="error-detail">{{ error }}</div>
        </div>
      </div>
      <div v-else-if="!projects.length" class="empty-tip">暂无项目数据，请先在<router-link to="/settings">系统设置</router-link>中配置 Jira 连接</div>
    </div>
  </div>
</template>

<script setup>
import { ref, onMounted } from 'vue'
import { useRouter } from 'vue-router'
import api from '@/api'

const router = useRouter()
const loading = ref(true)
const projects = ref([])
const error = ref('')

onMounted(async () => {
  try {
    const res = await api.get('/api/jira/projects')
    projects.value = res.data.data || []
  } catch (e) {
    error.value = e.response?.data?.error || e.message || '未知错误'
  } finally {
    loading.value = false
  }
})

function viewProject(key) {
  router.push({ path: '/issues', query: { project: key } })
}

function viewIssues(key) {
  router.push({ path: '/issues', query: { project: key } })
}
</script>

<style scoped>
.project-key {
  font-family: var(--font-mono);
  font-size: 13px;
  font-weight: 600;
  color: var(--primary-light);
  background: rgba(59,130,246,0.1);
  padding: 2px 8px;
  border-radius: 4px;
}
.error-tip {
  display: flex;
  align-items: flex-start;
  gap: 12px;
  padding: 16px 20px;
  border-radius: var(--radius);
  background: rgba(239,68,68,0.08);
  border: 1px solid rgba(239,68,68,0.2);
  margin-top: 20px;
}
.error-icon { font-size: 20px; }
.error-title { font-weight: 600; color: var(--danger); margin-bottom: 4px; }
.error-detail { font-size: 13px; color: var(--text-secondary); line-height: 1.5; }
.empty-tip {
  text-align: center;
  padding: 40px;
  color: var(--text-secondary);
}
.empty-tip a { color: var(--primary-light); text-decoration: underline; }
</style>
