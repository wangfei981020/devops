<template>
  <div class="dashboard">
    <transition name="fade">
      <div v-if="showPortalNotice" class="portal-notice">
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" width="18" height="18">
          <path d="M12 22s8-4 8-10V5l-8-3-8 3v7c0 6 8 10 8 10z"/>
          <polyline points="9 12 11 14 15 10"/>
        </svg>
        <span>SSO 登录成功，欢迎 {{ authStore.displayName }}</span>
        <button class="notice-close" @click="showPortalNotice = false">
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" width="14" height="14">
            <line x1="18" y1="6" x2="6" y2="18"/><line x1="6" y1="6" x2="18" y2="18"/>
          </svg>
        </button>
      </div>
    </transition>

    <div v-if="loading" class="loading"><div class="spinner"></div>加载中...</div>

    <template v-else>
      <!-- 统计卡片 -->
      <div class="stat-cards">
        <div class="stat-card">
          <div class="stat-value">{{ data.total_spaces || 0 }}</div>
          <div class="stat-label">空间数</div>
        </div>
        <div class="stat-card">
          <div class="stat-value">{{ data.current_user?.displayName || '-' }}</div>
          <div class="stat-label">Confluence 用户</div>
        </div>
      </div>

      <!-- 最近更新 -->
      <div class="card" v-if="recentContent.length">
        <h3>最近更新的内容</h3>
        <div class="table-wrapper">
          <table>
            <thead>
              <tr>
                <th>标题</th>
                <th>空间</th>
                <th>类型</th>
                <th>版本</th>
                <th>更新者</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="item in recentContent" :key="item.id" @click="$router.push('/content/' + item.id)">
                <td>{{ item.title }}</td>
                <td>{{ item.space?.name || '-' }}</td>
                <td><span class="badge" :class="item.type">{{ item.type === 'page' ? '页面' : '博客' }}</span></td>
                <td>v{{ item.version?.number || 1 }}</td>
                <td>{{ item.version?.by?.displayName || '-' }}</td>
              </tr>
            </tbody>
          </table>
        </div>
      </div>

      <div v-if="configError" class="card" style="text-align:center;padding:40px;color:var(--text-muted)">
        <p>{{ configError }}</p>
        <router-link v-if="authStore.isAdmin" to="/settings" class="btn btn-primary" style="margin-top:12px">配置 Confluence 连接</router-link>
      </div>
    </template>
  </div>
</template>

<script setup>
import { ref, onMounted } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import api from '@/api'
import { useAuthStore } from '@/stores/auth'

const route = useRoute()
const router = useRouter()
const authStore = useAuthStore()
const loading = ref(true)
const configError = ref('')
const showPortalNotice = ref(false)
const data = ref({})
const recentContent = ref([])

onMounted(async () => {
  if (route.query.portal_login === '1') {
    showPortalNotice.value = true
    router.replace({ path: '/dashboard' })
    setTimeout(() => { showPortalNotice.value = false }, 5000)
  }

  try {
    const res = await api.get('/api/dashboard')
    data.value = res.data.data
    // 解析最近内容
    const rc = data.value.recent_content
    if (rc?.results) {
      recentContent.value = rc.results
    }
  } catch (e) {
    if (e.response?.data?.error?.includes('未配置')) {
      configError.value = 'Confluence 连接未配置，请先在系统设置中配置 Confluence 连接信息'
    } else {
      configError.value = e.response?.data?.error || '加载仪表盘失败'
    }
  } finally {
    loading.value = false
  }
})
</script>

<style scoped>
.stat-cards {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(200px, 1fr));
  gap: 16px;
  margin-bottom: 20px;
}

.stat-card {
  background: var(--bg-card);
  border: 1px solid var(--border);
  border-radius: var(--radius-lg);
  padding: 20px;
  text-align: center;
}
.stat-value {
  font-size: 28px;
  font-weight: 700;
  color: var(--primary-light);
  font-family: var(--font-mono);
}
.stat-label {
  font-size: 13px;
  color: var(--text-secondary);
  margin-top: 4px;
}

h3 {
  font-size: 14px;
  font-weight: 600;
  color: var(--text-secondary);
  margin-bottom: 12px;
}

.badge.page { background: rgba(76,154,255,0.15); color: #4C9AFF; }
.badge.blogpost { background: rgba(255,153,31,0.15); color: #FF991F; }

.portal-notice {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 14px 18px;
  margin-bottom: 16px;
  background: linear-gradient(135deg, rgba(16, 185, 129, 0.12) 0%, rgba(6, 182, 212, 0.12) 100%);
  border: 1px solid rgba(16, 185, 129, 0.25);
  border-radius: var(--radius-lg);
  color: #10b981;
  font-size: 14px;
  font-weight: 500;
}
.portal-notice svg:first-child { flex-shrink: 0; color: #10b981; }
.portal-notice span { flex: 1; }
.notice-close {
  background: none;
  border: none;
  color: #10b981;
  cursor: pointer;
  padding: 4px;
  opacity: 0.6;
}
.notice-close:hover { opacity: 1; }
.fade-enter-active, .fade-leave-active { transition: opacity 0.3s; }
.fade-enter-from, .fade-leave-to { opacity: 0; }
</style>
