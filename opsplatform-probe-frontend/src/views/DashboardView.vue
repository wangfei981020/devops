<template>
  <div class="dashboard">
    <div class="page-header">
      <h2>概览</h2>
      <el-button :icon="Refresh" circle @click="load" />
    </div>

    <div class="stat-grid">
      <div class="stat-card stat-green">
        <div class="stat-icon"><el-icon :size="22"><CircleCheck /></el-icon></div>
        <div class="stat-body">
          <div class="stat-label">在线 Agent</div>
          <div class="stat-num">{{ stats.agents_online || 0 }}</div>
        </div>
      </div>
      <div class="stat-card stat-gray">
        <div class="stat-icon"><el-icon :size="22"><CircleClose /></el-icon></div>
        <div class="stat-body">
          <div class="stat-label">离线 Agent</div>
          <div class="stat-num">{{ stats.agents_offline || 0 }}</div>
        </div>
      </div>
      <div class="stat-card stat-orange" :class="{ 'stat-alert': stats.agents_pending > 0 }">
        <div class="stat-icon"><el-icon :size="22"><Warning /></el-icon></div>
        <div class="stat-body">
          <div class="stat-label">待审批</div>
          <div class="stat-num">{{ stats.agents_pending || 0 }}</div>
        </div>
      </div>
      <div class="stat-card stat-blue">
        <div class="stat-icon"><el-icon :size="22"><Aim /></el-icon></div>
        <div class="stat-body">
          <div class="stat-label">探测目标</div>
          <div class="stat-num">{{ stats.targets || 0 }}</div>
        </div>
      </div>
      <div class="stat-card stat-cyan">
        <div class="stat-icon"><el-icon :size="22"><DataLine /></el-icon></div>
        <div class="stat-body">
          <div class="stat-label">今日探测</div>
          <div class="stat-num">{{ stats.today_total || 0 }}</div>
        </div>
      </div>
      <div class="stat-card stat-red" :class="{ 'stat-alert': stats.today_failed > 0 }">
        <div class="stat-icon"><el-icon :size="22"><CircleClose /></el-icon></div>
        <div class="stat-body">
          <div class="stat-label">今日失败</div>
          <div class="stat-num">{{ stats.today_failed || 0 }}</div>
        </div>
      </div>
      <div class="stat-card stat-success-rate">
        <div class="stat-icon"><el-icon :size="22"><TrendCharts /></el-icon></div>
        <div class="stat-body">
          <div class="stat-label">成功率</div>
          <div class="stat-num">{{ ((stats.today_success_rate||1)*100).toFixed(1) }}%</div>
        </div>
      </div>
    </div>

    <el-card class="panel" shadow="never">
      <template #header>
        <div class="panel-header">
          <span><el-icon><Warning /></el-icon> 最近失败</span>
          <el-tag size="small" type="danger" v-if="stats.recent_failures?.length">{{ stats.recent_failures.length }}</el-tag>
        </div>
      </template>
      <el-table :data="stats.recent_failures || []" size="small" :show-header="true" empty-text="暂无失败记录 🎉">
        <el-table-column prop="agent_id" label="Agent" min-width="160" />
        <el-table-column prop="target_name" label="目标" min-width="140" />
        <el-table-column prop="target_addr" label="地址" min-width="200" show-overflow-tooltip />
        <el-table-column label="错误" min-width="200" show-overflow-tooltip>
          <template #default="{row}">
            <el-tag type="danger" size="small">{{ row.error }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column prop="probed_at" label="时间" width="180" />
      </el-table>
    </el-card>
  </div>
</template>

<script setup>
import { ref, onMounted } from 'vue'
import api from '../api/client'
import { Refresh } from '@element-plus/icons-vue'

const stats = ref({})
async function load() {
  const r = await api.get('/dashboard')
  stats.value = r.data || {}
}
onMounted(load)
</script>

<style scoped>
.dashboard { padding: 4px 4px 20px; }
.page-header { display: flex; justify-content: space-between; align-items: center; margin-bottom: 16px; }
.page-header h2 { margin: 0; color: #1f2d3d; font-weight: 600; }

.stat-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(220px, 1fr));
  gap: 12px;
  margin-bottom: 16px;
}

.stat-card {
  background: #fff;
  border-radius: 8px;
  padding: 18px 18px;
  display: flex;
  align-items: center;
  gap: 14px;
  border: 1px solid #ebeef5;
  border-left-width: 4px;
  transition: all .2s;
  cursor: default;
}
.stat-card:hover { transform: translateY(-2px); box-shadow: 0 6px 16px rgba(0,0,0,0.06); }

.stat-icon {
  width: 44px; height: 44px; border-radius: 10px;
  display: flex; align-items: center; justify-content: center;
  flex-shrink: 0;
}
.stat-body { flex: 1; min-width: 0; }
.stat-label { color: #909399; font-size: 13px; margin-bottom: 4px; }
.stat-num { font-size: 26px; font-weight: 700; color: #1f2d3d; line-height: 1.1; }

.stat-green { border-left-color: #67c23a; }
.stat-green .stat-icon { background: #f0f9eb; color: #67c23a; }
.stat-gray { border-left-color: #909399; }
.stat-gray .stat-icon { background: #f4f4f5; color: #909399; }
.stat-orange { border-left-color: #e6a23c; }
.stat-orange .stat-icon { background: #fdf6ec; color: #e6a23c; }
.stat-blue { border-left-color: #409eff; }
.stat-blue .stat-icon { background: #ecf5ff; color: #409eff; }
.stat-cyan { border-left-color: #17a2b8; }
.stat-cyan .stat-icon { background: #e8f7fa; color: #17a2b8; }
.stat-red { border-left-color: #f56c6c; }
.stat-red .stat-icon { background: #fef0f0; color: #f56c6c; }
.stat-success-rate { border-left-color: #67c23a; }
.stat-success-rate .stat-icon { background: #f0f9eb; color: #67c23a; }
.stat-success-rate .stat-num { color: #67c23a; }

.stat-alert {
  animation: pulse 2s infinite;
}
@keyframes pulse {
  0%, 100% { box-shadow: 0 0 0 0 rgba(230,162,60,0); }
  50% { box-shadow: 0 0 0 6px rgba(230,162,60,0.15); }
}

.panel {
  border-radius: 8px;
  border: 1px solid #ebeef5;
}
.panel-header { display: flex; justify-content: space-between; align-items: center; }
.panel-header span { display: flex; align-items: center; gap: 8px; color: #1f2d3d; font-weight: 600; }
:deep(.el-card__header) { padding: 14px 18px; }
</style>
