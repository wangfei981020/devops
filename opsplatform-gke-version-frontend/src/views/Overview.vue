<template>
  <div v-loading="loading">
    <!-- KPI 卡片 -->
    <div class="kpi-grid">
      <div class="kpi-card">
        <div class="kpi-label">监控集群</div>
        <div class="kpi-value">{{ data.total_clusters || 0 }}</div>
      </div>
      <div class="kpi-card">
        <div class="kpi-label">监控节点池</div>
        <div class="kpi-value">{{ data.total_nodepools || 0 }}</div>
      </div>
      <div class="kpi-card" :class="{ warning: data.clusters_behind > 0 }">
        <div class="kpi-label">需升级集群（≥5）</div>
        <div class="kpi-value">{{ data.clusters_behind || 0 }} <span v-if="data.clusters_behind > 0">⚠️</span></div>
      </div>
      <div class="kpi-card" :class="{ warning: data.nodepools_behind > 0 }">
        <div class="kpi-label">需升级节点池（≥5）</div>
        <div class="kpi-value">{{ data.nodepools_behind || 0 }} <span v-if="data.nodepools_behind > 0">⚠️</span></div>
      </div>
      <div class="kpi-card" :class="{ danger: data.eol_within_90d > 0 }">
        <div class="kpi-label">EOL ≤ 90 天</div>
        <div class="kpi-value">{{ data.eol_within_90d || 0 }} <span v-if="data.eol_within_90d > 0">🔥</span></div>
      </div>
    </div>

    <!-- Top5 集群 -->
    <div class="block">
      <div class="block-title">落后最严重的集群 Top 5</div>
      <el-table :data="data.top_clusters || []" stripe size="small" empty-text="暂无落后集群">
        <el-table-column label="集群名">
          <template #default="{ row }">
            <router-link :to="`/clusters/${row.id}`">{{ row.name }}</router-link>
          </template>
        </el-table-column>
        <el-table-column prop="project_id" label="项目" />
        <el-table-column prop="location" label="区域" width="120" />
        <el-table-column prop="current_version" label="当前版本" />
        <el-table-column prop="latest_available_version" label="GKE 最新" />
        <el-table-column label="落后版本数" width="160">
          <template #default="{ row }">
            <el-tag :type="badgeType(row.current_to_latest_versions_behind)">
              {{ row.current_to_latest_versions_behind }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column label="标准 EOL" width="180">
          <template #default="{ row }">
            <span :style="{ color: eolColor(row.days_to_std_eol) }">
              {{ row.std_support_end || '-' }}
              <span v-if="row.days_to_std_eol !== null && row.days_to_std_eol !== undefined">
                (剩 {{ row.days_to_std_eol }} 天)
              </span>
            </span>
          </template>
        </el-table-column>
      </el-table>
    </div>

    <!-- Top5 节点池 -->
    <div class="block">
      <div class="block-title">落后最严重的节点池 Top 5</div>
      <el-table :data="data.top_nodepools || []" stripe size="small" empty-text="暂无落后节点池">
        <el-table-column label="集群">
          <template #default="{ row }">
            <router-link :to="`/clusters/${row.cluster_id}`">{{ row.cluster_name }}</router-link>
          </template>
        </el-table-column>
        <el-table-column prop="nodepool_name" label="节点池" />
        <el-table-column prop="current_version" label="当前版本" />
        <el-table-column prop="latest_available_version" label="GKE 最新" />
        <el-table-column label="落后版本数" width="160">
          <template #default="{ row }">
            <el-tag :type="badgeType(row.current_to_latest_versions_behind)">
              {{ row.current_to_latest_versions_behind }}
            </el-tag>
          </template>
        </el-table-column>
      </el-table>
    </div>

    <!-- EOL 紧急告警 -->
    <div class="block" v-if="(data.eol_alerts || []).length > 0">
      <div class="block-title eol-title">🔥 紧急 EOL 告警</div>
      <ul class="eol-list">
        <li v-for="a in data.eol_alerts" :key="a.cluster_id">
          <router-link :to="`/clusters/${a.cluster_id}`">{{ a.cluster_name }}</router-link>
          ：标准支持 <b>{{ a.std_support_end }}</b> 到期（剩 <b :class="a.days_remaining <= 30 ? 'red' : 'orange'">{{ a.days_remaining }}</b> 天）
        </li>
      </ul>
    </div>
  </div>
</template>

<script setup>
import { ref, onMounted } from 'vue'
import { getOverview } from '../api/overview'

const data = ref({})
const loading = ref(false)

async function load() {
  loading.value = true
  try {
    data.value = await getOverview()
  } finally {
    loading.value = false
  }
}

function badgeType(n) {
  if (n === 0) return 'success'
  if (n <= 5) return 'warning'
  return 'danger'
}
function eolColor(d) {
  if (d === null || d === undefined) return '#999'
  if (d <= 30) return '#f56c6c'
  if (d <= 90) return '#e6a23c'
  return '#67c23a'
}
onMounted(load)
</script>

<style scoped>
.kpi-grid { display: grid; grid-template-columns: repeat(5, 1fr); gap: 16px; margin-bottom: 24px; }
.kpi-card { background: var(--bg-card); border: 1px solid var(--border); border-radius: 8px; padding: 18px 20px; }
.kpi-card.warning { border-color: #e6a23c; background: #fdf6ec; }
.kpi-card.danger { border-color: #f56c6c; background: #fef0f0; }
.kpi-label { font-size: 12.5px; color: var(--text-2); margin-bottom: 6px; }
.kpi-value { font-size: 28px; font-weight: 600; color: var(--text); }

.block { background: var(--bg-card); border: 1px solid var(--border); border-radius: 8px; padding: 18px; margin-bottom: 16px; }
.block-title { font-size: 14px; font-weight: 600; margin-bottom: 12px; color: var(--text); }
.block-title.eol-title { color: #f56c6c; }

.eol-list { list-style: none; padding: 0; margin: 0; }
.eol-list li { padding: 6px 0; font-size: 13px; }
.eol-list .red { color: #f56c6c; }
.eol-list .orange { color: #e6a23c; }
</style>
