<template>
  <div v-loading="loading">
    <el-page-header @back="$router.back()" title="返回">
      <template #content>集群详情</template>
    </el-page-header>
    <el-descriptions v-if="cluster" :title="cluster.name" :column="2" border style="margin-top:16px">
      <el-descriptions-item label="项目">{{ cluster.project_id }}</el-descriptions-item>
      <el-descriptions-item label="区域">{{ cluster.location }}</el-descriptions-item>
      <el-descriptions-item label="当前版本">
        {{ snap?.current_version || '-' }}
        <el-tag v-if="clusterCurrentDays !== null" type="info" size="small" style="margin-left:8px;">运行 {{ clusterCurrentDays }} 天</el-tag>
      </el-descriptions-item>
      <el-descriptions-item label="可升级最大">{{ snap?.max_upgradable_version || '-' }}</el-descriptions-item>
      <el-descriptions-item label="GKE 最新">{{ snap?.latest_available_version || '-' }}</el-descriptions-item>
      <el-descriptions-item label="落后(可升级)">
        <VersionDiffBadge v-if="snap" :behind="snap.current_to_max_versions_behind" :diff="snap.current_to_max_version_diff" />
      </el-descriptions-item>
      <el-descriptions-item label="落后(最新)">
        <VersionDiffBadge v-if="snap" :behind="snap.current_to_latest_versions_behind" :diff="snap.current_to_latest_version_diff" />
      </el-descriptions-item>
      <el-descriptions-item label="标准EOL"><EolCell :date="snap?.std_support_end" /></el-descriptions-item>
      <el-descriptions-item label="扩展EOL"><EolCell :date="snap?.ext_support_end" /></el-descriptions-item>
      <el-descriptions-item label="最后刷新">{{ formatTime(snap?.last_refreshed_at) }}</el-descriptions-item>
    </el-descriptions>

    <h3 style="margin-top:24px;">节点池</h3>
    <div class="hint">"最老 N 天" = 该节点池里存活最久的 VM 已运行天数，等价于"当前节点版本至少跑了多少天没动"。点击展开看每个 node 明细。</div>

    <el-collapse v-model="activePanels" class="np-collapse">
      <el-collapse-item
        v-for="np in nodepoolPanels"
        :key="np.name"
        :name="np.name"
      >
        <template #title>
          <div class="np-header">
            <span class="np-name">{{ np.name }}</span>
            <el-tag v-if="np.oldestAgeDays !== null" :type="ageTagType(np.oldestAgeDays)" size="small">
              最老 {{ np.oldestAgeDays }} 天
            </el-tag>
            <span class="np-meta">
              <span class="label">版本</span>
              <code class="ver">{{ np.currentVersion || '-' }}</code>
            </span>
            <span class="np-meta">
              <span class="label">节点数</span>
              {{ np.nodeCount }}
            </span>
            <span class="np-meta">
              <span class="label">落后</span>
              <VersionDiffBadge :behind="np.behind" :diff="np.diff" />
            </span>
            <span class="np-meta">
              <span class="label">EOL</span>
              <EolCell :date="np.stdEol" />
            </span>
          </div>
        </template>

        <el-table v-if="np.nodes && np.nodes.length" :data="np.nodes" stripe size="small">
          <el-table-column prop="name" label="Node 名" min-width="340" />
          <el-table-column prop="zone" label="Zone" min-width="140" />
          <el-table-column label="AGE" min-width="100">
            <template #default="{ row }">
              <el-tag :type="ageTagType(row.age_days)" size="small">{{ row.age_days }} 天</el-tag>
            </template>
          </el-table-column>
          <el-table-column prop="version" label="版本" min-width="180" />
          <el-table-column label="GCP 创建时间" min-width="180">
            <template #default="{ row }">{{ formatTime(row.gcp_created_at) }}</template>
          </el-table-column>
        </el-table>
        <el-empty v-else description="暂无节点明细（Compute API 权限未授予 / 节点池缩为 0 / 还没 scrape）" :image-size="60" />
      </el-collapse-item>
    </el-collapse>

    <!-- 版本变更历史 -->
    <h3 style="margin-top:32px;">版本变更历史</h3>
    <div class="hint">起始时间是首次纳管监控的时间，不一定是真实升级时间。</div>

    <div class="history-block">
      <div class="history-subtitle">集群控制平面</div>
      <el-timeline v-if="history.cluster && history.cluster.length">
        <el-timeline-item
          v-for="(e, idx) in history.cluster"
          :key="idx"
          :type="e.current ? 'primary' : 'info'"
          :timestamp="formatRange(e)"
          placement="top"
        >
          <div class="history-row">
            <code class="ver">{{ e.version }}</code>
            <el-tag v-if="e.current" type="success" size="small">当前</el-tag>
            <span class="duration">运行 {{ e.duration_days }} 天</span>
          </div>
        </el-timeline-item>
      </el-timeline>
      <el-empty v-else description="暂无变更记录" :image-size="60" />
    </div>

    <div v-for="(entries, npName) in history.nodepools" :key="npName" class="history-block">
      <div class="history-subtitle">节点池 {{ npName }}</div>
      <el-timeline>
        <el-timeline-item
          v-for="(e, idx) in entries"
          :key="idx"
          :type="e.current ? 'primary' : 'info'"
          :timestamp="formatRange(e)"
          placement="top"
        >
          <div class="history-row">
            <code class="ver">{{ e.version }}</code>
            <el-tag v-if="e.current" type="success" size="small">当前</el-tag>
            <span class="duration">运行 {{ e.duration_days }} 天</span>
          </div>
        </el-timeline-item>
      </el-timeline>
    </div>
  </div>
</template>

<script setup>
import { ref, onMounted, computed } from 'vue'
import { useRoute } from 'vue-router'
import { listClusters } from '../api/clusters'
import { getVersionHistory } from '../api/version_history'
import { getNodes } from '../api/nodes'
import VersionDiffBadge from '../components/VersionDiffBadge.vue'
import EolCell from '../components/EolCell.vue'

const route = useRoute()
const loading = ref(false)
const cluster = ref(null)
const history = ref({ cluster: [], nodepools: {} })
const nodesData = ref({ nodepools: [] })
const activePanels = ref([])
const snap = computed(() => cluster.value?.snapshot)

// 集群控制平面的"当前版本运行天数"——只能从 version_history 拿（控制平面不是 VM，没 createTime）
const clusterCurrentDays = computed(() => {
  const cur = (history.value.cluster || []).find(e => e.current)
  return cur ? cur.duration_days : null
})

// 把 snap.nodepools（版本/EOL/落后）和 nodesData.nodepools（VM 明细 / 最老天数）按名字合并。
// 优先用 nodesData 的顺序（最新 scrape 结果），落后版本/EOL 等从 snap 补。
const nodepoolPanels = computed(() => {
  const npFromSnap = {}
  for (const n of (snap.value?.nodepools || [])) {
    npFromSnap[n.name] = n
  }
  const result = []
  const seen = new Set()
  // 先按 nodes API 顺序
  for (const g of (nodesData.value.nodepools || [])) {
    const snapNp = npFromSnap[g.name] || {}
    result.push(buildPanel(g.name, snapNp, g))
    seen.add(g.name)
  }
  // 再补 snap 里有但 nodes API 没返的（比如 Compute API 权限缺失，全空）
  for (const n of (snap.value?.nodepools || [])) {
    if (!seen.has(n.name)) {
      result.push(buildPanel(n.name, n, null))
    }
  }
  return result
})

function buildPanel(name, snapNp, nodesGroup) {
  return {
    name,
    currentVersion: snapNp.current_version || '',
    behind: snapNp.current_to_max_versions_behind ?? 0,
    diff: snapNp.current_to_max_version_diff ?? 0,
    stdEol: snapNp.std_support_end || '',
    nodeCount: nodesGroup?.node_count ?? 0,
    oldestAgeDays: nodesGroup?.oldest_age_days ?? null,
    newestAgeDays: nodesGroup?.newest_age_days ?? null,
    nodes: nodesGroup?.nodes || [],
  }
}

// AGE 颜色：> 365 天危险，> 180 天警告，否则正常
function ageTagType(days) {
  if (days == null) return 'info'
  if (days > 365) return 'danger'
  if (days > 180) return 'warning'
  return 'info'
}

function formatTime(s) {
  if (!s) return '-'
  return new Date(s).toLocaleString('zh-CN')
}

function fmtDate(s) {
  if (!s) return '-'
  return new Date(s).toLocaleDateString('zh-CN')
}

function formatRange(e) {
  if (e.current) return `${fmtDate(e.started_at)} 起 · 至今`
  return `${fmtDate(e.started_at)} → ${fmtDate(e.ended_at)}`
}

onMounted(async () => {
  loading.value = true
  try {
    const all = await listClusters()
    cluster.value = all.find(c => String(c.id) === String(route.params.id))
    if (cluster.value) {
      const [hist, nodes] = await Promise.all([
        getVersionHistory(cluster.value.id),
        getNodes(cluster.value.id).catch(() => ({ nodepools: [] })),
      ])
      history.value = hist
      nodesData.value = nodes
    }
  } finally {
    loading.value = false
  }
})
</script>

<style scoped>
.hint { color: #999; font-size: 12px; margin-bottom: 12px; }
.history-block { background: var(--bg-card); border: 1px solid var(--border); border-radius: 8px; padding: 16px 18px; margin-bottom: 16px; }
.history-subtitle { font-weight: 600; font-size: 14px; margin-bottom: 12px; color: var(--text); }
.history-row { display: flex; align-items: center; gap: 12px; }
.ver { font-family: var(--mono); font-size: 13px; background: #f3f4f6; padding: 2px 8px; border-radius: 4px; }
.duration { color: var(--text-2); font-size: 12px; }

.np-collapse { border-top: 1px solid var(--border); }
.np-header { display: flex; align-items: center; gap: 18px; flex-wrap: wrap; width: 100%; padding-right: 12px; }
.np-name { font-weight: 600; font-size: 14px; min-width: 240px; }
.np-meta { display: flex; align-items: center; gap: 6px; font-size: 13px; color: var(--text); }
.np-meta .label { color: var(--text-2); font-size: 12px; }
</style>
