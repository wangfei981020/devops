<template>
  <div class="cluster-detail" v-loading="loading">
    <!-- Hero：返回按钮 + 集群名 + 紧凑元信息 -->
    <header class="hero">
      <button class="back-btn" @click="$router.back()">
        <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
          <path d="M19 12H5M12 19l-7-7 7-7" stroke-linecap="round" stroke-linejoin="round" />
        </svg>
        <span>返回</span>
      </button>

      <h1 class="cluster-name">{{ cluster?.name || '加载中...' }}</h1>

      <div class="meta-row" v-if="cluster">
        <span class="meta-item">
          <span class="meta-label">项目</span>
          <span class="meta-value">{{ cluster.project_id }}</span>
        </span>
        <span class="meta-divider" />
        <span class="meta-item">
          <span class="meta-label">区域</span>
          <span class="meta-value">{{ cluster.location }}</span>
        </span>
        <span class="meta-divider" />
        <span class="meta-item">
          <span class="meta-label">最后刷新</span>
          <span class="meta-value">{{ formatTime(snap?.last_refreshed_at) }}</span>
        </span>
      </div>
    </header>

    <!-- KPI 卡片：4 列响应式网格 -->
    <section class="kpi-grid" v-if="snap">
      <!-- 当前版本 -->
      <div class="kpi-card">
        <div class="kpi-label">当前版本</div>
        <div class="kpi-primary mono">{{ snap.current_version || '-' }}</div>
        <div class="kpi-sub">
          <span class="muted">最新</span>
          <code class="ver-mono">{{ snap.latest_available_version || '-' }}</code>
        </div>
      </div>

      <!-- 落后版本 -->
      <div class="kpi-card" :class="behindAccent(snap.current_to_latest_versions_behind)">
        <div class="kpi-label">落后版本</div>
        <div class="kpi-primary">
          <span class="kpi-number">{{ snap.current_to_latest_versions_behind ?? 0 }}</span>
          <span class="kpi-unit">个</span>
        </div>
        <div class="kpi-sub">
          <span class="muted">算术差</span>
          <span>{{ formatDiff(snap.current_to_latest_version_diff) }}</span>
        </div>
      </div>

      <!-- EOL 倒计时 -->
      <div class="kpi-card" :class="eolAccent(eolDaysLeft)">
        <div class="kpi-label">标准 EOL 剩余</div>
        <div class="kpi-primary">
          <span class="kpi-number">{{ eolDaysLeft ?? '-' }}</span>
          <span class="kpi-unit" v-if="eolDaysLeft !== null">天</span>
        </div>
        <div class="kpi-sub">
          <span class="muted">{{ formatDate(snap.std_support_end) || '未知' }}</span>
        </div>
      </div>

      <!-- 节点池总览 -->
      <div class="kpi-card">
        <div class="kpi-label">节点池 / 节点</div>
        <div class="kpi-primary">
          <span class="kpi-number">{{ nodepoolPanels.length }}</span>
          <span class="kpi-slash">/</span>
          <span class="kpi-number">{{ totalNodeCount }}</span>
        </div>
        <div class="kpi-sub" v-if="overallOldestAge !== null">
          <span class="muted">最老节点</span>
          <span :class="ageColorClass(overallOldestAge)">{{ overallOldestAge }} 天</span>
        </div>
      </div>
    </section>

    <!-- 节点池卡片网格 -->
    <section class="section">
      <div class="section-head">
        <h2 class="section-title">节点池</h2>
        <p class="section-hint">"最老 N 天" 是该节点池里存活最久的 VM 已运行天数 ≈ 当前版本至少跑了多久没动。点击 "查看节点" 看每个 VM 明细。</p>
      </div>

      <div class="np-grid" v-if="nodepoolPanels.length">
        <article v-for="np in nodepoolPanels" :key="np.name" class="np-card">
          <div class="np-card-head">
            <div class="np-title-row">
              <span class="np-name" :title="np.name">{{ np.name }}</span>
              <span v-if="np.oldestAgeDays !== null" class="age-pill" :class="ageColorClass(np.oldestAgeDays)">
                <span class="dot" />
                最老 {{ np.oldestAgeDays }} 天
              </span>
              <span v-else class="age-pill age-unknown">
                <span class="dot" />
                无节点数据
              </span>
            </div>
          </div>

          <dl class="np-stats">
            <div class="stat">
              <dt>版本</dt>
              <dd><code class="ver-mono">{{ np.currentVersion || '-' }}</code></dd>
            </div>
            <div class="stat">
              <dt>节点数</dt>
              <dd>
                <span class="num">{{ np.nodeCount }}</span>
              </dd>
            </div>
            <div class="stat">
              <dt>落后</dt>
              <dd>
                <span :class="behindTextClass(np.behind)">{{ np.behind }} 个</span>
              </dd>
            </div>
            <div class="stat">
              <dt>EOL</dt>
              <dd :class="eolTextClass(daysUntil(np.stdEol))">
                {{ formatDate(np.stdEol) || '-' }}
              </dd>
            </div>
          </dl>

          <div class="np-card-foot">
            <button
              class="view-nodes-btn"
              :disabled="!np.nodes.length"
              @click="openNodesDialog(np)"
            >
              {{ np.nodes.length ? `查看 ${np.nodes.length} 个节点` : '暂无节点明细' }}
            </button>
          </div>
        </article>
      </div>

      <el-empty v-else description="暂无节点池数据" :image-size="80" />
    </section>

    <!-- 版本变更历史：折叠 -->
    <section class="section">
      <el-collapse v-model="historyOpen" class="history-collapse">
        <el-collapse-item name="history">
          <template #title>
            <div class="history-head">
              <h2 class="section-title inline">版本变更历史</h2>
              <span class="section-hint inline">起始时间是首次纳管监控的时间，不一定是真实升级时间</span>
            </div>
          </template>

          <div class="history-block">
            <div class="history-subtitle">
              <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                <circle cx="12" cy="12" r="10" />
                <path d="M12 6v6l4 2" stroke-linecap="round" />
              </svg>
              集群控制平面
            </div>
            <el-timeline v-if="history.cluster && history.cluster.length">
              <el-timeline-item
                v-for="(e, idx) in history.cluster"
                :key="idx"
                :type="e.current ? 'primary' : 'info'"
                :timestamp="formatRange(e)"
                placement="top"
              >
                <div class="history-row">
                  <code class="ver-mono">{{ e.version }}</code>
                  <span v-if="e.current" class="tag-current">当前</span>
                  <span class="duration">运行 {{ e.duration_days }} 天</span>
                </div>
              </el-timeline-item>
            </el-timeline>
            <el-empty v-else description="暂无变更记录" :image-size="60" />
          </div>

          <div v-for="(entries, npName) in history.nodepools" :key="npName" class="history-block">
            <div class="history-subtitle">
              <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                <rect x="3" y="3" width="18" height="18" rx="2" />
                <path d="M3 9h18M9 3v18" />
              </svg>
              节点池 {{ npName }}
            </div>
            <el-timeline>
              <el-timeline-item
                v-for="(e, idx) in entries"
                :key="idx"
                :type="e.current ? 'primary' : 'info'"
                :timestamp="formatRange(e)"
                placement="top"
              >
                <div class="history-row">
                  <code class="ver-mono">{{ e.version }}</code>
                  <span v-if="e.current" class="tag-current">当前</span>
                  <span class="duration">运行 {{ e.duration_days }} 天</span>
                </div>
              </el-timeline-item>
            </el-timeline>
          </div>
        </el-collapse-item>
      </el-collapse>
    </section>

    <!-- 节点明细 Dialog -->
    <el-dialog
      v-model="nodesDialogOpen"
      :title="`节点池 ${activeNp?.name || ''} — ${activeNp?.nodeCount || 0} 个节点`"
      width="900px"
      destroy-on-close
    >
      <el-table v-if="activeNp" :data="activeNp.nodes" stripe size="small" :max-height="500">
        <el-table-column prop="name" label="Node 名" min-width="320" show-overflow-tooltip />
        <el-table-column prop="zone" label="Zone" min-width="120" />
        <el-table-column label="AGE" min-width="110" sortable :sort-by="(row) => row.age_days">
          <template #default="{ row }">
            <span class="age-pill compact" :class="ageColorClass(row.age_days)">
              <span class="dot" />
              {{ row.age_days }} 天
            </span>
          </template>
        </el-table-column>
        <el-table-column prop="version" label="版本" min-width="170">
          <template #default="{ row }">
            <code class="ver-mono">{{ row.version }}</code>
          </template>
        </el-table-column>
        <el-table-column label="GCP 创建时间" min-width="180">
          <template #default="{ row }">{{ formatTime(row.gcp_created_at) }}</template>
        </el-table-column>
      </el-table>
    </el-dialog>
  </div>
</template>

<script setup>
import { ref, onMounted, computed } from 'vue'
import { useRoute } from 'vue-router'
import { listClusters } from '../api/clusters'
import { getVersionHistory } from '../api/version_history'
import { getNodes } from '../api/nodes'

const route = useRoute()
const loading = ref(false)
const cluster = ref(null)
const history = ref({ cluster: [], nodepools: {} })
const nodesData = ref({ nodepools: [] })
const historyOpen = ref([]) // 默认收起；点击展开
const nodesDialogOpen = ref(false)
const activeNp = ref(null)

const snap = computed(() => cluster.value?.snapshot)

// 合并 snap.nodepools（版本/EOL）+ nodesData.nodepools（VM 明细）
const nodepoolPanels = computed(() => {
  const npFromSnap = {}
  for (const n of (snap.value?.nodepools || [])) npFromSnap[n.name] = n
  const result = []
  const seen = new Set()
  for (const g of (nodesData.value.nodepools || [])) {
    result.push(buildPanel(g.name, npFromSnap[g.name] || {}, g))
    seen.add(g.name)
  }
  for (const n of (snap.value?.nodepools || [])) {
    if (!seen.has(n.name)) result.push(buildPanel(n.name, n, null))
  }
  return result
})

const totalNodeCount = computed(() =>
  nodepoolPanels.value.reduce((sum, np) => sum + (np.nodeCount || 0), 0)
)

const overallOldestAge = computed(() => {
  let max = null
  for (const np of nodepoolPanels.value) {
    if (np.oldestAgeDays !== null && (max === null || np.oldestAgeDays > max)) {
      max = np.oldestAgeDays
    }
  }
  return max
})

const eolDaysLeft = computed(() => daysUntil(snap.value?.std_support_end))

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

function openNodesDialog(np) {
  activeNp.value = np
  nodesDialogOpen.value = true
}

// === 颜色规则（语义化） ===
// AGE：> 365 天 红，> 180 天 橙，正常 蓝
function ageColorClass(days) {
  if (days == null) return 'age-unknown'
  if (days > 365) return 'age-danger'
  if (days > 180) return 'age-warning'
  return 'age-ok'
}
// 落后版本数：> 20 红，> 10 橙，> 5 黄，否则 ok
function behindAccent(n) {
  if (n == null) return ''
  if (n > 20) return 'accent-danger'
  if (n > 10) return 'accent-warning'
  if (n > 5) return 'accent-notice'
  return ''
}
function behindTextClass(n) {
  if (n == null) return ''
  if (n > 20) return 'text-danger'
  if (n > 10) return 'text-warning'
  if (n > 5) return 'text-notice'
  return 'text-ok'
}
// EOL 剩余天数：< 30 红，< 90 橙，< 180 黄，否则 ok
function eolAccent(days) {
  if (days == null) return ''
  if (days < 30) return 'accent-danger'
  if (days < 90) return 'accent-warning'
  if (days < 180) return 'accent-notice'
  return ''
}
function eolTextClass(days) {
  if (days == null) return ''
  if (days < 30) return 'text-danger'
  if (days < 90) return 'text-warning'
  if (days < 180) return 'text-notice'
  return ''
}

// === 工具 ===
function daysUntil(dateStr) {
  if (!dateStr) return null
  const d = new Date(dateStr)
  if (isNaN(d.getTime())) return null
  return Math.floor((d.getTime() - Date.now()) / 86400000)
}
function formatDate(s) {
  if (!s) return ''
  const d = new Date(s)
  if (isNaN(d.getTime())) return ''
  return d.toLocaleDateString('zh-CN', { year: 'numeric', month: '2-digit', day: '2-digit' })
}
function formatTime(s) {
  if (!s) return '-'
  return new Date(s).toLocaleString('zh-CN', { hour12: false })
}
function formatRange(e) {
  if (e.current) return `${formatDate(e.started_at)} 起 · 至今`
  return `${formatDate(e.started_at)} → ${formatDate(e.ended_at)}`
}
function formatDiff(v) {
  if (v == null) return '-'
  return Number(v).toFixed(4)
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
/* ===== 整体布局 ===== */
.cluster-detail {
  display: flex;
  flex-direction: column;
  gap: 24px;
  max-width: 1440px;
  margin: 0 auto;
}

/* ===== Hero ===== */
.hero {
  display: flex;
  flex-direction: column;
  gap: 12px;
}
.back-btn {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  background: none;
  border: none;
  padding: 6px 0;
  color: var(--text-2, #64748b);
  cursor: pointer;
  font-size: 13px;
  width: fit-content;
  transition: color 150ms ease;
}
.back-btn:hover { color: #1E40AF; }
.cluster-name {
  font-size: 26px;
  font-weight: 700;
  color: var(--text, #0F172A);
  margin: 0;
  line-height: 1.2;
  letter-spacing: -0.01em;
}
.meta-row {
  display: flex;
  align-items: center;
  gap: 16px;
  flex-wrap: wrap;
  font-size: 13px;
}
.meta-item { display: inline-flex; gap: 6px; align-items: baseline; }
.meta-label { color: var(--text-2, #64748b); }
.meta-value { color: var(--text, #0F172A); font-weight: 500; }
.meta-divider {
  width: 1px;
  height: 12px;
  background: var(--border, #e2e8f0);
}

/* ===== KPI 卡片网格 ===== */
.kpi-grid {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(220px, 1fr));
  gap: 12px;
}
.kpi-card {
  background: var(--bg-card, #ffffff);
  border: 1px solid var(--border, #e2e8f0);
  border-left: 3px solid var(--border, #e2e8f0);
  border-radius: 8px;
  padding: 16px 18px;
  display: flex;
  flex-direction: column;
  gap: 6px;
  transition: border-color 200ms ease, box-shadow 200ms ease;
}
.kpi-card:hover {
  box-shadow: 0 1px 3px rgba(15, 23, 42, 0.08), 0 4px 8px rgba(15, 23, 42, 0.04);
}
.kpi-label {
  font-size: 12px;
  color: var(--text-2, #64748b);
  font-weight: 500;
  letter-spacing: 0.02em;
  text-transform: uppercase;
}
.kpi-primary {
  display: flex;
  align-items: baseline;
  gap: 4px;
  font-size: 22px;
  font-weight: 600;
  color: var(--text, #0F172A);
  line-height: 1.2;
}
.kpi-primary.mono { font-family: var(--mono, 'Fira Code', ui-monospace, monospace); font-size: 17px; word-break: break-all; }
.kpi-number { font-size: 26px; font-weight: 700; font-variant-numeric: tabular-nums; }
.kpi-unit { font-size: 13px; color: var(--text-2, #64748b); margin-left: 2px; }
.kpi-slash { color: var(--text-2, #94a3b8); margin: 0 4px; font-weight: 400; }
.kpi-sub {
  display: flex;
  gap: 6px;
  font-size: 12px;
  color: var(--text, #0F172A);
  align-items: center;
}
.muted { color: var(--text-2, #64748b); }

.accent-notice  { border-left-color: #F59E0B; }
.accent-warning { border-left-color: #F97316; }
.accent-danger  { border-left-color: #EF4444; }

/* ===== Section ===== */
.section { display: flex; flex-direction: column; gap: 12px; }
.section-head { display: flex; flex-direction: column; gap: 4px; }
.section-title {
  font-size: 16px;
  font-weight: 600;
  color: var(--text, #0F172A);
  margin: 0;
  letter-spacing: -0.01em;
}
.section-title.inline { display: inline; }
.section-hint {
  font-size: 12px;
  color: var(--text-2, #94a3b8);
  margin: 0;
}
.section-hint.inline { display: inline; margin-left: 12px; }

/* ===== 节点池卡片网格 ===== */
.np-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(320px, 1fr));
  gap: 12px;
}
.np-card {
  background: var(--bg-card, #ffffff);
  border: 1px solid var(--border, #e2e8f0);
  border-radius: 8px;
  display: flex;
  flex-direction: column;
  overflow: hidden;
  transition: border-color 200ms ease, box-shadow 200ms ease;
}
.np-card:hover {
  border-color: #cbd5e1;
  box-shadow: 0 1px 3px rgba(15, 23, 42, 0.08), 0 4px 8px rgba(15, 23, 42, 0.04);
}
.np-card-head {
  padding: 14px 16px 10px;
  border-bottom: 1px dashed var(--border, #e2e8f0);
}
.np-title-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 10px;
}
.np-name {
  font-weight: 600;
  font-size: 13px;
  color: var(--text, #0F172A);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  flex: 1;
}

/* AGE Pill */
.age-pill {
  display: inline-flex;
  align-items: center;
  gap: 5px;
  padding: 3px 8px;
  border-radius: 12px;
  font-size: 11px;
  font-weight: 500;
  border: 1px solid transparent;
  white-space: nowrap;
}
.age-pill.compact { padding: 2px 8px; }
.age-pill .dot {
  width: 6px;
  height: 6px;
  border-radius: 50%;
  display: inline-block;
}
.age-ok      { background: #EFF6FF; color: #1E40AF; border-color: #BFDBFE; }
.age-ok      .dot { background: #3B82F6; }
.age-warning { background: #FFF7ED; color: #C2410C; border-color: #FED7AA; }
.age-warning .dot { background: #F97316; }
.age-danger  { background: #FEF2F2; color: #B91C1C; border-color: #FECACA; }
.age-danger  .dot { background: #EF4444; }
.age-unknown { background: #F1F5F9; color: #64748B; border-color: #E2E8F0; }
.age-unknown .dot { background: #CBD5E1; }

/* 节点池统计 grid */
.np-stats {
  display: grid;
  grid-template-columns: repeat(2, 1fr);
  gap: 10px 16px;
  padding: 14px 16px;
  margin: 0;
}
.stat { display: flex; flex-direction: column; gap: 2px; }
.stat dt {
  font-size: 11px;
  color: var(--text-2, #64748b);
  font-weight: 500;
  letter-spacing: 0.02em;
}
.stat dd {
  margin: 0;
  font-size: 13px;
  color: var(--text, #0F172A);
  font-weight: 500;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.stat .num { font-variant-numeric: tabular-nums; }

.text-ok      { color: #047857; }
.text-notice  { color: #B45309; }
.text-warning { color: #C2410C; }
.text-danger  { color: #B91C1C; }

/* 节点池底部按钮 */
.np-card-foot {
  padding: 10px 16px;
  border-top: 1px solid var(--border, #e2e8f0);
  background: #F8FAFC;
}
.view-nodes-btn {
  width: 100%;
  background: transparent;
  border: 1px solid var(--border, #e2e8f0);
  border-radius: 6px;
  padding: 6px 12px;
  font-size: 12px;
  color: #1E40AF;
  cursor: pointer;
  transition: background-color 200ms ease, border-color 200ms ease;
  font-weight: 500;
}
.view-nodes-btn:hover:not(:disabled) {
  background: #EFF6FF;
  border-color: #93C5FD;
}
.view-nodes-btn:disabled {
  color: #94A3B8;
  cursor: not-allowed;
  background: #F1F5F9;
}

/* ===== 版本变更历史 ===== */
.history-collapse :deep(.el-collapse-item__header) {
  padding: 6px 0;
  border-bottom: none;
}
.history-collapse :deep(.el-collapse-item__wrap) {
  border-bottom: none;
}
.history-head { display: flex; align-items: baseline; gap: 12px; flex-wrap: wrap; }
.history-block {
  background: var(--bg-card, #ffffff);
  border: 1px solid var(--border, #e2e8f0);
  border-radius: 8px;
  padding: 16px 18px;
  margin-bottom: 12px;
}
.history-subtitle {
  display: inline-flex;
  align-items: center;
  gap: 8px;
  font-weight: 600;
  font-size: 13px;
  margin-bottom: 14px;
  color: var(--text, #0F172A);
}
.history-subtitle svg { color: var(--text-2, #94a3b8); }
.history-row { display: flex; align-items: center; gap: 10px; }
.ver-mono {
  font-family: var(--mono, 'Fira Code', ui-monospace, monospace);
  font-size: 12px;
  background: #F1F5F9;
  padding: 2px 8px;
  border-radius: 4px;
  color: #0F172A;
}
.tag-current {
  font-size: 11px;
  background: #D1FAE5;
  color: #047857;
  padding: 1px 8px;
  border-radius: 10px;
  font-weight: 500;
}
.duration { color: var(--text-2, #64748b); font-size: 12px; font-variant-numeric: tabular-nums; }

/* 响应式 */
@media (max-width: 768px) {
  .cluster-name { font-size: 22px; }
  .kpi-card { padding: 14px 16px; }
  .kpi-primary { font-size: 18px; }
  .kpi-number { font-size: 22px; }
}

@media (prefers-reduced-motion: reduce) {
  *, *::before, *::after {
    animation-duration: 0.01ms !important;
    transition-duration: 0.01ms !important;
  }
}
</style>
