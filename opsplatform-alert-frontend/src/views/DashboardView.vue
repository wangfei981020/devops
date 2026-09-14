<template>
  <div>
    <transition name="fade">
      <div v-if="showPortalNotice" class="portal-notice">
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" width="18" height="18">
          <path d="M12 22s8-4 8-10V5l-8-3-8 3v7c0 6 8 10 8 10z"/>
          <polyline points="9 12 11 14 15 10"/>
        </svg>
        <span>SSO 登录成功，欢迎 {{ auth.user?.username }}</span>
        <button class="notice-close" @click="showPortalNotice = false">&times;</button>
      </div>
    </transition>

    <!-- The first question is whether the alerting itself is working. A rule
         that stopped running looks exactly like a quiet day, so it gets said
         out loud rather than left for someone to notice. -->
    <div class="card health" :class="healthClass">
      <div class="health-head">
        <span class="health-dot" aria-hidden="true"></span>
        <span class="health-title">{{ headline }}</span>
        <span class="health-time">最后检查 {{ data.checked_at ? formatTimeWithZone(data.checked_at) : "—" }}</span>
        <button class="btn btn-outline btn-sm health-refresh" :disabled="loading" @click="load">
          {{ loading ? '检查中…' : '重新检查' }}
        </button>
      </div>

      <!-- Nothing configured is not the same as nothing wrong. Until the three
           things below exist, no alert can fire, and saying "正常" would be the
           most misleading thing this page could show a new installation. -->
      <template v-if="setup.steps && !setup.configured">
        <p class="health-empty">还没有配置完成，现在不会有任何告警发出。按下面三步走完即可：</p>
        <ol class="setup-list">
          <li v-for="s in setup.steps" :key="s.key" class="setup-step" :class="{ 'is-done': s.done }">
            <span class="setup-mark" aria-hidden="true">{{ s.done ? '✓' : '' }}</span>
            <router-link v-if="!s.done" :to="s.link" class="setup-label">{{ s.label }}</router-link>
            <span v-else class="setup-label">{{ s.label }}</span>
          </li>
        </ol>
      </template>

      <ul v-else-if="attention.length" class="attn-list">
        <li v-for="item in attention" :key="item.kind" class="attn-item">
          <span class="badge" :class="item.severity === 'critical' ? 'badge-danger' : 'badge-warning'">
            {{ item.count }}
          </span>
          <router-link :to="item.link" class="attn-label">{{ item.label }}</router-link>
          <span v-if="item.detail" class="attn-detail">{{ item.detail }}</span>
        </li>
      </ul>
      <p v-else class="health-empty">
        规则按计划执行，渠道与数据源可用，24 小时内没有发送失败。
      </p>
    </div>

    <div class="stats-grid">
      <div v-if="auth.hasMenu('rules')" class="stat-card">
        <div class="label">告警规则</div>
        <div class="value">{{ counts.rules_total || 0 }}</div>
        <div class="sub-line">启用: {{ counts.rules_enabled || 0 }}</div>
      </div>
      <div v-if="auth.hasMenu('logs')" class="stat-card">
        <div class="label">今日告警</div>
        <div class="value" :class="{ danger: counts.today_failed > 0 }">{{ counts.today_alerts || 0 }}</div>
        <div class="sub-line">
          成功: {{ counts.today_success || 0 }} / <span :class="{ danger: counts.today_failed > 0 }">失败: {{ counts.today_failed || 0 }}</span>
        </div>
      </div>
      <div v-if="auth.hasMenu('connections')" class="stat-card">
        <div class="label">数据源连接</div>
        <div class="value">{{ (counts.es_connections || 0) + (counts.loki_connections || 0) }}</div>
        <div class="sub-line">
          ES {{ counts.es_active || 0 }}/{{ counts.es_connections || 0 }} ·
          Loki {{ counts.loki_active || 0 }}/{{ counts.loki_connections || 0 }}
        </div>
      </div>
      <div v-if="auth.hasMenu('lark')" class="stat-card">
        <div class="label">通知渠道</div>
        <div class="value">{{ counts.lark_configs || 0 }}</div>
        <div class="sub-line">活跃: {{ counts.lark_active || 0 }}</div>
      </div>
    </div>

    <!-- The chart wants width, so it takes a row of its own rather than being
         squeezed beside a list. -->
    <div v-if="auth.hasMenu('logs')" class="card">
      <div class="card-header">
        <div class="card-title">24 小时告警趋势</div>
        <span class="card-note">{{ trendTotal }} 条</span>
      </div>
      <template v-if="trendTotal > 0">
        <div class="spark-plot">
          <!-- Two guides, each labelled with the count it stands for, so the
               height of a bar can be read rather than only compared. -->
          <div v-for="g in yGuides" :key="g.value" class="spark-guide" :style="{ bottom: g.bottom }">
            <span class="spark-guide-label">{{ g.value }}</span>
          </div>
          <!-- The whole column is the hover target, not just the bar: an empty
               hour is a 2px hairline, and nobody can hit that. -->
          <div class="spark" @mouseleave="hovered = null">
            <div
              v-for="(p, i) in trend"
              :key="i"
              class="spark-col"
              :class="{ 'is-hovered': hovered === i }"
              :aria-label="`${p.label} ${p.count} 条`"
              @mouseenter="hovered = i"
            >
              <div class="spark-bar" :style="{ height: barHeight(p.count) }"></div>
              <div v-if="hovered === i" class="spark-tip" :class="tipAlign(i)">
                <b>{{ p.count }}</b> 条
                <span class="spark-tip-time">{{ p.label }}</span>
              </div>
            </div>
          </div>
        </div>
        <!-- The axis repeats the bars' own grid, one cell per hour, so a label
             sits under the bar it belongs to instead of only marking the ends.
             Every hour is rendered and CSS decides how many are shown, which is
             what lets the density drop on a narrow window without the track
             shifting and pulling the labels off their bars. -->
        <div class="spark-axis">
          <span v-for="(p, i) in trend" :key="i" class="spark-tick">{{ p.label }}</span>
        </div>
      </template>
      <div v-else class="empty-note">过去 24 小时没有产生告警。</div>
    </div>

    <!-- Three lists of the same shape and the same length cap, so the row reads
         as one component instead of a tall card beside a short one. -->
    <div class="dash-row dash-row-3">
      <div v-if="auth.hasMenu('logs')" class="card">
        <div class="card-header">
          <div class="card-title">最近告警</div>
          <router-link to="/alert-logs" class="card-note card-link">查看全部</router-link>
        </div>
        <ul v-if="recent.length" class="plain-list">
          <li v-for="a in recent" :key="a.id">
            <span class="badge" :class="severityClass(a.severity)">{{ severityLabel(a.severity, { short: true }) }}</span>
            <span class="li-main" :title="a.rule_name">{{ a.rule_name || `规则 #${a.rule_id}` }}</span>
            <span class="badge" :class="statusClass(a.status)">{{ statusLabel(a.status) }}</span>
            <span class="li-side">{{ shortTime(a.created_at) }}</span>
          </li>
        </ul>
        <div v-else class="empty-note">还没有告警记录。规则触发后会出现在这里。</div>
      </div>

      <div v-if="auth.hasMenu('rules')" class="card">
        <div class="card-header">
          <div class="card-title">最吵的规则</div>
          <span class="card-note">近 7 天</span>
        </div>
        <ul v-if="noisy.length" class="plain-list">
          <li v-for="n in noisy" :key="n.rule_id">
            <router-link :to="`/alert-rules/${n.rule_id}/edit`" class="li-main">
              {{ n.rule_name || `规则 #${n.rule_id}` }}
            </router-link>
            <span class="li-bar" :style="{ width: noisyWidth(n.count) }" aria-hidden="true"></span>
            <span class="li-side">{{ n.count }}</span>
          </li>
        </ul>
        <div v-else class="empty-note">近 7 天没有告警，无从排序。</div>
      </div>

      <div v-if="auth.hasMenu('mutes')" class="card">
        <div class="card-header">
          <div class="card-title">生效中的屏蔽</div>
          <router-link v-if="mutes.count" to="/mutes" class="card-note card-link">全部 {{ mutes.count }}</router-link>
        </div>
        <ul v-if="mutes.items?.length" class="plain-list">
          <li v-for="m in mutes.items" :key="`${m.rule_id}-${m.group_key}`">
            <span class="li-main">{{ m.group_key || m.rule_name || '—' }}</span>
            <span class="li-side" :title="formatTimeWithZone(m.mute_until)">{{ untilLabel(m.remaining_seconds) }}</span>
          </li>
        </ul>
        <div v-else class="empty-note">没有生效中的屏蔽。</div>
      </div>
    </div>

    <div class="card" v-if="auth.hasMenu('rules') || auth.hasMenu('connections') || auth.hasMenu('lark') || auth.hasMenu('logs')">
      <div class="card-header">
        <div class="card-title">快速操作</div>
      </div>
      <div class="flex gap-4">
        <router-link v-if="auth.hasMenu('rules')" to="/alert-rules/create" class="btn btn-primary">
          <Plus :size="16" /> 新建告警规则
        </router-link>
        <router-link v-if="auth.hasMenu('connections')" to="/es-connections" class="btn btn-outline">
          <Database :size="16" /> 管理 ES 连接
        </router-link>
        <router-link v-if="auth.hasMenu('lark')" to="/notify-channels" class="btn btn-outline">
          <Send :size="16" /> 管理通知渠道
        </router-link>
        <router-link v-if="auth.hasMenu('logs')" to="/alert-logs" class="btn btn-outline">
          <FileText :size="16" /> 查看告警日志
        </router-link>
      </div>
    </div>
  </div>
</template>

<script setup>
import { ref, computed, onMounted } from 'vue'
import api from '../api'
import { severityClass, severityLabel, statusClass, statusLabel } from '../utils/severity'
import { formatShortTime, formatRemaining, formatTimeWithZone } from '../utils/datetime'
import { useAuthStore } from '../stores/auth'
import { Plus, Database, Send, FileText } from 'lucide-vue-next'

const auth = useAuthStore()
const data = ref({})
const loading = ref(false)
const hovered = ref(null)
const showPortalNotice = ref(auth.user?.auth_source === 'portal')

const counts = computed(() => data.value.counts || {})
const attention = computed(() => data.value.attention || [])
const trend = computed(() => data.value.trend_24h || [])
const recent = computed(() => data.value.recent_alerts || [])
const mutes = computed(() => data.value.active_mutes || { count: 0, items: [] })
const noisy = computed(() => data.value.noisy_rules || [])

const trendTotal = computed(() => trend.value.reduce((sum, p) => sum + p.count, 0))
const trendMax = computed(() => Math.max(1, ...trend.value.map(p => p.count)))
const noisyMax = computed(() => Math.max(1, ...noisy.value.map(n => n.count)))

const setup = computed(() => data.value.setup || {})

const headline = computed(() => {
  if (setup.value.steps && !setup.value.configured) {
    return `尚未完成配置（${setup.value.done}/${setup.value.total}）`
  }
  return attention.value.length ? `${attention.value.length} 项待处理` : '告警链路正常'
})

// One critical item makes the whole panel critical; warnings alone stay amber.
// An unconfigured platform is amber rather than green: nothing is broken, but
// nothing is working either.
const healthClass = computed(() => {
  if (setup.value.steps && !setup.value.configured) return 'health-warn'
  if (!attention.value.length) return 'health-ok'
  return attention.value.some(a => a.severity === 'critical') ? 'health-critical' : 'health-warn'
})

// A zero-count hour still gets a hairline, so the axis reads as a timeline
// rather than a gap.
function barHeight(count) {
  if (!count) return '2px'
  return `${Math.max(6, Math.round((count / trendMax.value) * 100))}%`
}

// A tooltip centred on the first or last column would hang off the card, so the
// ends anchor to their own edge instead.
function tipAlign(i) {
  if (i <= 1) return 'tip-left'
  if (i >= trend.value.length - 2) return 'tip-right'
  return ''
}

// Guides at the peak and half of it. Below three the halfway mark rounds onto
// the peak and the two labels collide, so a small chart shows only its peak.
const yGuides = computed(() => {
  const max = trendMax.value
  const guides = [{ value: max, bottom: '100%' }]
  if (max >= 3) guides.push({ value: Math.round(max / 2), bottom: '50%' })
  return guides
})
function noisyWidth(count) {
  return `${Math.max(4, Math.round((count / noisyMax.value) * 100))}%`
}

// Timestamps arrive as instants and are rendered in the platform's display
// zone, same as everywhere else. Mutes read as "how long do I still have", from
// a server-computed second count, so the countdown never depends on the
// browser's clock agreeing with the server's.
const shortTime = formatShortTime
const untilLabel = formatRemaining

async function load() {
  loading.value = true
  try {
    const res = await api.get('/dashboard')
    if (res.code === 0) data.value = res.data
  } catch (e) { /* ignore */ } finally {
    loading.value = false
  }
}

onMounted(() => {
  if (showPortalNotice.value) {
    setTimeout(() => { showPortalNotice.value = false }, 5000)
  }
  load()
})
</script>

<style scoped>
/* Health panel — the one block that changes colour, so the colour means
   something. Everything else on the page stays neutral. */
.health {
  border-left: 3px solid var(--border);
}
.health-ok { border-left-color: var(--success); }
.health-warn { border-left-color: var(--warning); }
.health-critical { border-left-color: var(--danger); }

.health-head {
  display: flex;
  align-items: center;
  gap: var(--space-10);
}
.health-dot {
  width: 8px;
  height: 8px;
  border-radius: 50%;
  background: var(--border);
  flex-shrink: 0;
}
.health-ok .health-dot { background: var(--success); }
.health-warn .health-dot { background: var(--warning); }
.health-critical .health-dot { background: var(--danger); }

.health-title { font-size: var(--fs-16); font-weight: 600; }
.health-time {
  font-size: var(--fs-14);
  color: var(--text-secondary);
  font-variant-numeric: tabular-nums;
}
.health-refresh { margin-left: auto; }
.health-empty {
  margin-top: var(--space-10);
  font-size: var(--fs-14);
  color: var(--text-secondary);
}

.attn-list {
  list-style: none;
  margin-top: var(--space-12);
  display: flex;
  flex-direction: column;
  gap: var(--space-8);
}
.attn-item {
  display: flex;
  align-items: center;
  gap: var(--space-10);
  font-size: var(--fs-14);
}
.attn-item .badge { font-variant-numeric: tabular-nums; min-width: 28px; justify-content: center; }

/* Setup checklist. Numbered because the steps genuinely run in order — a rule
   cannot be created before a source and a channel exist. */
.setup-list {
  list-style: none;
  margin-top: var(--space-12);
  counter-reset: setup;
  display: flex;
  flex-direction: column;
  gap: var(--space-8);
}
.setup-step {
  counter-increment: setup;
  display: flex;
  align-items: center;
  gap: var(--space-10);
  font-size: var(--fs-14);
}
.setup-mark {
  width: 22px;
  height: 22px;
  flex-shrink: 0;
  border-radius: 50%;
  display: grid;
  place-items: center;
  font-size: var(--fs-12);
  border: 1px solid var(--border);
  color: var(--text-secondary);
}
.setup-mark::before { content: counter(setup); }
.setup-step.is-done .setup-mark {
  border-color: var(--success);
  background: var(--success);
  color: #fff;
}
/* The tick replaces the number once the step is done. */
.setup-step.is-done .setup-mark::before { content: none; }
.setup-label { color: var(--text); text-decoration: none; }
a.setup-label:hover { color: var(--primary); text-decoration: underline; }
.setup-step.is-done .setup-label { color: var(--text-secondary); }
.attn-label { color: var(--text); text-decoration: none; font-weight: 500; }
.attn-label:hover { color: var(--primary); text-decoration: underline; }
.attn-detail { color: var(--text-secondary); }

/* Panel rows share the form grid's rhythm so the page reads on one system.
   Cards in a row stretch to a common height: a ragged bottom edge is the thing
   that reads as untidy, and auto-fit keeps the row filled when a permission
   hides one of the panels. */
.dash-row {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(280px, 1fr));
  gap: var(--space-16);
  align-items: stretch;
}
.dash-row > .card {
  margin-bottom: var(--space-16);
  display: flex;
  flex-direction: column;
}
/* The list takes the slack so every card in the row ends on the same line. */
.dash-row > .card > .plain-list,
.dash-row > .card > .empty-note { flex: 1; }

.card-note {
  font-size: var(--fs-14);
  color: var(--text-secondary);
  font-variant-numeric: tabular-nums;
}
.card-link { text-decoration: none; }
.card-link:hover { color: var(--primary); text-decoration: underline; }

.empty-note {
  font-size: var(--fs-14);
  color: var(--text-secondary);
  padding: var(--space-16) 0;
}

/* Hourly counts as bars: honest for counts, and no path maths. */
.spark-plot {
  position: relative;
  /* Room on the right for the guide labels, so they never sit over a bar. */
  padding-right: 32px;
}
.spark {
  display: flex;
  align-items: flex-end;
  gap: var(--space-3);
  height: 96px;
}
.spark-col {
  flex: 1;
  display: flex;
  align-items: flex-end;
  height: 100%;
  position: relative;
  cursor: default;
}
.spark-bar {
  width: 100%;
  background: var(--primary);
  border-radius: 2px 2px 0 0;
  min-height: 2px;
  transition: height var(--dur) var(--ease), background-color var(--dur-fast) var(--ease);
}
/* Hovering anywhere in the column tints its bar, so it is obvious which hour
   the tooltip is describing. */
.spark-col.is-hovered .spark-bar { background: var(--primary-hover); }
.spark-col.is-hovered::before {
  content: '';
  position: absolute;
  inset: 0;
  background: rgba(79, 70, 229, 0.06);
  border-radius: 2px;
}

.spark-tip {
  position: absolute;
  bottom: calc(100% + 6px);
  left: 50%;
  transform: translateX(-50%);
  z-index: 2;
  padding: var(--space-6) var(--space-10);
  border-radius: 6px;
  background: var(--text);
  color: #fff;
  font-size: var(--fs-12);
  line-height: 1.4;
  white-space: nowrap;
  box-shadow: var(--shadow-md);
  pointer-events: none;
}
.spark-tip b { font-size: var(--fs-14); font-variant-numeric: tabular-nums; }
.spark-tip-time {
  margin-left: var(--space-6);
  opacity: 0.75;
  font-variant-numeric: tabular-nums;
}
.spark-tip.tip-left { left: 0; transform: none; }
.spark-tip.tip-right { left: auto; right: 0; transform: none; }

/* Guides sit behind the bars and stop short of the labels. */
.spark-guide {
  position: absolute;
  left: 0;
  right: 32px;
  border-top: 1px dashed var(--border);
  pointer-events: none;
}
.spark-guide-label {
  position: absolute;
  left: 100%;
  top: -0.7em;
  padding-left: var(--space-6);
  font-size: var(--fs-12);
  color: var(--text-secondary);
  font-variant-numeric: tabular-nums;
  white-space: nowrap;
}

/* Same flex track as the bars, so a tick lands under its own bar. */
.spark-axis {
  display: flex;
  gap: var(--space-3);
  margin-top: var(--space-6);
  padding-right: 32px;
  font-size: var(--fs-12);
  color: var(--text-secondary);
  font-variant-numeric: tabular-nums;
}
/* Every hour keeps its cell so the track never moves; only some cells show
   their label. There are 24 buckets, so 3n and 6n both land on the last one —
   the newest hour always carries a label, which is the one being read.
   Hidden rather than removed, so a shown label may bleed into its neighbours'
   space without anything to collide with. */
.spark-tick {
  flex: 1;
  min-width: 0;
  text-align: center;
  white-space: nowrap;
  overflow: visible;
  visibility: hidden;
}
.spark-tick:nth-child(3n) { visibility: visible; }

@media (max-width: 1500px) {
  .spark-tick:nth-child(3n) { visibility: hidden; }
  .spark-tick:nth-child(6n) { visibility: visible; }
}
@media (max-width: 1000px) {
  .spark-tick:nth-child(6n) { visibility: hidden; }
  .spark-tick:nth-child(12n) { visibility: visible; }
}

.plain-list {
  list-style: none;
  display: flex;
  flex-direction: column;
}
.plain-list li {
  display: flex;
  align-items: center;
  gap: var(--space-8);
  padding: var(--space-8) 0;
  border-bottom: 1px solid var(--border);
  font-size: var(--fs-14);
}
.plain-list li:last-child { border-bottom: none; }
.li-main {
  flex: 1;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  color: var(--text);
  text-decoration: none;
}
a.li-main:hover { color: var(--primary); text-decoration: underline; }
.li-side {
  color: var(--text-secondary);
  font-variant-numeric: tabular-nums;
  white-space: nowrap;
}
/* A quantity bar rather than a second number column — the ranking is the
   point, the exact count is the detail beside it. */
.li-bar {
  height: 4px;
  max-width: 120px;
  background: rgba(79, 70, 229, 0.25);
  border-radius: 2px;
  flex-shrink: 0;
}

.sub-line {
  font-size: var(--fs-14);
  color: #475569;
  font-variant-numeric: tabular-nums;
}
.sub-line .danger {
  color: var(--danger);
  font-weight: 600;
}
.portal-notice {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 10px 16px;
  margin-bottom: 16px;
  background: rgba(16, 185, 129, 0.1);
  border: 1px solid rgba(16, 185, 129, 0.3);
  border-radius: 8px;
  color: #10b981;
  font-size: 14px;
}
.notice-close {
  margin-left: auto;
  background: none;
  border: none;
  color: #10b981;
  cursor: pointer;
  font-size: 18px;
  line-height: 1;
  opacity: 0.6;
}
.notice-close:hover { opacity: 1; }
.fade-enter-active, .fade-leave-active { transition: opacity 0.3s; }
.fade-enter-from, .fade-leave-to { opacity: 0; }

@media (max-width: 900px) {
  .dash-row, .dash-row-2-1 { grid-template-columns: 1fr; }
}
</style>
