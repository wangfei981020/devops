<template>
  <div class="dashboard">
    <!-- Portal 登录成功提示 -->
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
          <div class="stat-value">{{ data.projects_count }}</div>
          <div class="stat-label">项目数</div>
        </div>
        <div class="stat-card">
          <div class="stat-value">{{ data.total_issues }}</div>
          <div class="stat-label">总工单</div>
        </div>
        <div class="stat-card open">
          <div class="stat-value">{{ data.open_issues }}</div>
          <div class="stat-label">待处理</div>
        </div>
        <div class="stat-card resolved">
          <div class="stat-value">{{ data.resolved_issues }}</div>
          <div class="stat-label">已解决</div>
        </div>
      </div>

      <!-- 图表区域 -->
      <div class="charts-grid">
        <div class="card chart-card">
          <h3>状态分布</h3>
          <v-chart class="chart" :option="statusChartOption" autoresize />
        </div>
        <div class="card chart-card">
          <h3>优先级分布</h3>
          <v-chart class="chart" :option="priorityChartOption" autoresize />
        </div>
        <div class="card chart-card wide">
          <h3>负责人工作量 (近30天)</h3>
          <v-chart class="chart" :option="workloadChartOption" autoresize />
        </div>
      </div>

      <div v-if="configError" class="card" style="text-align:center;padding:40px;color:var(--text-muted)">
        <p>{{ configError }}</p>
        <router-link v-if="authStore.isAdmin" to="/settings" class="btn btn-primary" style="margin-top:12px">配置 Jira 连接</router-link>
      </div>
    </template>
  </div>
</template>

<script setup>
import { ref, computed, onMounted } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { use } from 'echarts/core'
import { PieChart, BarChart } from 'echarts/charts'
import { TitleComponent, TooltipComponent, LegendComponent, GridComponent } from 'echarts/components'
import { CanvasRenderer } from 'echarts/renderers'
import VChart from 'vue-echarts'
import api from '@/api'
import { useAuthStore } from '@/stores/auth'

use([PieChart, BarChart, TitleComponent, TooltipComponent, LegendComponent, GridComponent, CanvasRenderer])

const route = useRoute()
const router = useRouter()
const authStore = useAuthStore()
const loading = ref(true)
const configError = ref('')
const showPortalNotice = ref(false)
const data = ref({
  projects_count: 0, total_issues: 0, open_issues: 0, resolved_issues: 0,
  status_distribution: [], priority_distribution: [], assignee_workload: [],
})

onMounted(async () => {
  // Portal 登录成功提示
  if (route.query.portal_login === '1') {
    showPortalNotice.value = true
    router.replace({ path: '/dashboard' })
    setTimeout(() => { showPortalNotice.value = false }, 5000)
  }

  try {
    const res = await api.get('/api/dashboard')
    data.value = res.data.data
  } catch (e) {
    if (e.response?.data?.error?.includes('未配置')) {
      configError.value = 'Jira 连接未配置，请先在系统设置中配置 Jira 连接信息'
    } else {
      configError.value = e.response?.data?.error || '加载仪表盘失败'
    }
  } finally {
    loading.value = false
  }
})

const colors = ['#3B82F6', '#10B981', '#F59E0B', '#EF4444', '#8B5CF6', '#EC4899', '#06B6D4', '#84CC16']

const statusChartOption = computed(() => ({
  tooltip: { trigger: 'item', formatter: '{b}: {c} ({d}%)' },
  legend: { bottom: 0, textStyle: { color: '#94A3B8', fontSize: 12 } },
  color: colors,
  series: [{
    type: 'pie', radius: ['40%', '70%'], center: ['50%', '45%'],
    label: { show: false },
    data: data.value.status_distribution
  }]
}))

const priorityChartOption = computed(() => ({
  tooltip: { trigger: 'item', formatter: '{b}: {c} ({d}%)' },
  legend: { bottom: 0, textStyle: { color: '#94A3B8', fontSize: 12 } },
  color: ['#EF4444', '#F59E0B', '#3B82F6', '#10B981', '#94A3B8'],
  series: [{
    type: 'pie', radius: ['40%', '70%'], center: ['50%', '45%'],
    label: { show: false },
    data: data.value.priority_distribution
  }]
}))

const workloadChartOption = computed(() => {
  const sorted = [...(data.value.assignee_workload || [])].sort((a, b) => b.value - a.value).slice(0, 15)
  return {
    tooltip: { trigger: 'axis' },
    grid: { left: 120, right: 30, top: 10, bottom: 20 },
    xAxis: { type: 'value', splitLine: { lineStyle: { color: '#334155' } }, axisLabel: { color: '#94A3B8' } },
    yAxis: { type: 'category', data: sorted.map(i => i.name).reverse(), axisLabel: { color: '#94A3B8', fontSize: 12 } },
    series: [{
      type: 'bar', data: sorted.map(i => i.value).reverse(),
      itemStyle: { color: '#3B82F6', borderRadius: [0, 4, 4, 0] },
      barMaxWidth: 24
    }]
  }
})
</script>

<style scoped>
.stat-cards {
  display: grid;
  grid-template-columns: repeat(4, 1fr);
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
  font-size: 32px;
  font-weight: 700;
  color: var(--primary-light);
  font-family: var(--font-mono);
}
.stat-card.open .stat-value { color: var(--warning); }
.stat-card.resolved .stat-value { color: var(--success); }
.stat-label {
  font-size: 13px;
  color: var(--text-secondary);
  margin-top: 4px;
}

.charts-grid {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 16px;
}
.chart-card { min-height: 320px; }
.chart-card h3 {
  font-size: 14px;
  font-weight: 600;
  color: var(--text-secondary);
  margin-bottom: 12px;
}
.chart-card.wide { grid-column: 1 / -1; }
.chart { width: 100%; height: 260px; }

@media (max-width: 768px) {
  .stat-cards { grid-template-columns: repeat(2, 1fr); }
  .charts-grid { grid-template-columns: 1fr; }
}

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
