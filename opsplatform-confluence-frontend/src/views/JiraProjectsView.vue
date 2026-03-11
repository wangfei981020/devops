<template>
  <div>
    <ConnectionSelector type="jira" v-model="connId" @connection-change="onConnChange" />

    <div v-if="loading" class="loading"><div class="spinner"></div>加载中...</div>

    <template v-else>

      <div v-if="error" class="card" style="text-align:center;padding:40px;color:var(--text-muted)">{{ error }}</div>

      <div v-else>
        <!-- 搜索过滤 -->
        <div class="card" style="margin-bottom:16px;padding:16px">
          <div style="display:flex;gap:12px;align-items:center">
            <input v-model="filterText" type="text" class="input" placeholder="搜索项目名称或 Key..." style="max-width:360px" />
            <span style="color:var(--text-muted);font-size:13px">共 {{ filteredProjects.length }} 个项目</span>
          </div>
        </div>

        <!-- 项目卡片网格 -->
        <div class="project-grid">
          <div
            v-for="proj in filteredProjects"
            :key="proj.key"
            class="project-card"
            @click="$router.push('/jira/project/' + proj.key)"
          >
            <div class="project-card-header">
              <div class="project-avatar-placeholder">{{ proj.key.charAt(0) }}</div>
              <div class="project-info">
                <div class="project-name">{{ proj.name }}</div>
                <div class="project-key">{{ proj.key }}</div>
              </div>
            </div>
            <div class="project-card-body">
              <div v-if="proj.lead" class="project-meta">
                <span class="meta-label">负责人</span>
                <span class="meta-value">{{ proj.lead.displayName || proj.lead.name }}</span>
              </div>
              <div class="project-meta">
                <span class="meta-label">类型</span>
                <span class="meta-value">{{ proj.projectTypeKey || 'software' }}</span>
              </div>
            </div>
            <div class="project-card-footer">
              <button class="btn btn-sm" @click.stop="$router.push('/jira/project/' + proj.key)">查看工单</button>
            </div>
          </div>
        </div>

        <div v-if="filteredProjects.length === 0 && !loading" class="card" style="text-align:center;padding:40px;color:var(--text-muted)">
          没有匹配的项目
        </div>
      </div>
    </template>
  </div>
</template>

<script setup>
import { ref, computed, onMounted } from 'vue'
import api from '@/api'
import ConnectionSelector from '@/components/ConnectionSelector.vue'

const loading = ref(true)
const error = ref('')
const projects = ref([])
const filterText = ref('')
const allowedProjects = ref([])
const connId = ref('')

const filteredProjects = computed(() => {
  let list = projects.value
  if (allowedProjects.value.length > 0) {
    list = list.filter(p => allowedProjects.value.includes(p.key))
  }
  if (!filterText.value) return list
  const q = filterText.value.toLowerCase()
  return list.filter(p =>
    p.key.toLowerCase().includes(q) || p.name.toLowerCase().includes(q)
  )
})

async function fetchProjects() {
  loading.value = true
  error.value = ''
  try {
    const params = connId.value ? { conn_id: connId.value } : {}
    const res = await api.get('/api/jira/projects', { params })
    projects.value = res.data.data || []
  } catch (e) {
    error.value = e.response?.data?.error || '加载 Jira 项目失败，请检查 Jira 连接配置'
  }
  loading.value = false
}

function onConnChange(conn) {
  // 从连接的 config 中读取 projects 过滤
  if (conn && conn.config) {
    const cfg = typeof conn.config === 'string' ? JSON.parse(conn.config) : conn.config
    const raw = cfg.projects || ''
    allowedProjects.value = raw.split(',').map(s => s.trim()).filter(Boolean)
  } else {
    allowedProjects.value = []
  }
  fetchProjects()
}

onMounted(() => {
  // 初始加载由 ConnectionSelector 的 connection-change 触发
})
</script>

<style scoped>
.project-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(300px, 1fr));
  gap: 16px;
}
.project-card {
  background: var(--bg-card);
  border: 1px solid var(--border);
  border-radius: var(--radius-lg);
  padding: 20px;
  cursor: pointer;
  transition: all var(--transition);
}
.project-card:hover {
  border-color: var(--primary-light);
  box-shadow: 0 0 0 1px var(--primary-light), var(--shadow);
  transform: translateY(-2px);
}
.project-card-header {
  display: flex;
  align-items: center;
  gap: 14px;
  margin-bottom: 16px;
}
.project-avatar {
  width: 44px;
  height: 44px;
  border-radius: var(--radius);
  flex-shrink: 0;
}
.project-avatar-placeholder {
  width: 44px;
  height: 44px;
  border-radius: var(--radius);
  background: linear-gradient(135deg, var(--primary), var(--primary-light));
  color: #fff;
  display: flex;
  align-items: center;
  justify-content: center;
  font-size: 20px;
  font-weight: 700;
  flex-shrink: 0;
}
.project-info { flex: 1; min-width: 0; }
.project-name {
  font-size: 15px;
  font-weight: 600;
  color: var(--text-primary);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.project-key {
  font-family: var(--font-mono);
  font-size: 12px;
  color: var(--primary-light);
  margin-top: 2px;
}
.project-card-body {
  display: flex;
  flex-direction: column;
  gap: 8px;
  margin-bottom: 16px;
}
.project-meta {
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: 13px;
}
.meta-label {
  color: var(--text-muted);
  min-width: 50px;
}
.meta-value {
  color: var(--text-secondary);
}
.project-card-footer {
  display: flex;
  justify-content: flex-end;
}
</style>
