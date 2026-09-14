<template>
  <div>
    <div class="card">
      <div class="card-header">
        <div class="card-title">平台设置</div>
      </div>

      <h3 class="form-section form-section-flush">显示时区</h3>

      <div class="form-notice">
        <strong>这个设置影响两件事：</strong>界面和告警消息里所有时间的显示，以及规则 Cron 表达式的解释 ——
        <code>0 3 * * *</code> 指的是这个时区的凌晨 3 点。
      </div>

      <div class="form-row">
        <div class="form-group">
          <label class="form-label">时区</label>
          <select v-model="zone" class="form-select">
            <option v-for="z in zones" :key="z" :value="z">{{ zoneLabel(z) }}</option>
          </select>
          <div class="form-hint">
            {{ zone ? '按 IANA 时区名，夏令时自动跟随' : '未指定时区：界面按浏览器所在时区显示，定时规则按服务器时区执行' }}
          </div>
        </div>
        <div class="form-group">
          <label class="form-label">当前时间</label>
          <div class="form-static">{{ preview }}</div>
          <div class="form-hint">{{ dirty ? '这是切换后的效果，尚未保存' : '当前生效的时区' }}</div>
        </div>
      </div>

      <h3 class="form-section">告警日志保留</h3>

      <div class="form-row form-row-2-1">
        <div class="form-group">
          <label class="form-label">保留天数</label>
          <select v-model.number="retention" class="form-select">
            <option :value="0">不清理（保留全部）</option>
            <option :value="30">30 天</option>
            <option :value="60">60 天</option>
            <option :value="90">90 天</option>
            <option :value="180">180 天</option>
            <option :value="365">365 天</option>
          </select>
          <div class="form-hint">
            {{ retention
              ? `每天 03:30 自动删除 ${retention} 天前的告警日志，分批执行，不会锁住正在写入的表`
              : '默认不清理。告警日志会一直累积，需要时可在「告警日志」页手动清理' }}
          </div>
        </div>
        <div class="form-group">
          <label class="form-label">当前记录数</label>
          <div class="form-static">{{ settings.logRows?.toLocaleString?.() ?? '—' }}</div>
          <div class="form-hint">保存后不会立刻删除，等当晚的定时任务执行</div>
        </div>
      </div>

      <!-- One save for the whole page, so it sits after the last field rather
           than between two sections it would appear to belong to. -->
      <div class="form-actions">
        <button class="btn btn-primary" :disabled="!dirty || saving" @click="save">
          {{ saving ? '保存中…' : '保存' }}
        </button>
        <button class="btn btn-outline" :disabled="!dirty || saving" @click="reset">取消</button>
        <span v-if="dirty" class="form-actions-note">{{ changeSummary }}</span>
      </div>

      <h3 class="form-section">关于日志原文</h3>
      <p class="settings-note">
        告警消息里的错误栈按<strong>原文</strong>输出，其中的时间戳来自产生日志的那个系统，
        <strong>不做任何转换</strong>。它们是排查用的证据 —— 改写之后就没法拿去源系统检索了，
        而且我们无从确定它们本来是哪个时区。上面这个设置只影响我们自己打的时间。
      </p>
      <pre class="settings-sample">🔴 UAT 支付超时告警
时间：{{ sampleAlertTime }}

错误栈（原文，时间为来源系统时间）：
2026-09-07 20:31:42 ERROR PaymentGateway timeout
  at com.g32.pay.Gateway.call(Gateway.java:88)</pre>
    </div>
  </div>
</template>

<script setup>
import { ref, computed, onMounted, onUnmounted, watch } from 'vue'
import { useSettingsStore } from '../stores/settings'
import { useToast } from '../stores/ui'
import { formatTimeWithZone, getDisplayTimezone, setDisplayTimezone } from '../utils/datetime'

const settings = useSettingsStore()
const toast = useToast()
const zone = ref('')
const retention = ref(0)
const saving = ref(false)
const now = ref(new Date())

const zones = computed(() => {
  const list = [...(settings.availableZones || [])]
  // A zone configured by hand stays selectable even if it is not on the shortlist.
  if (settings.displayTimezone && !list.includes(settings.displayTimezone)) {
    list.unshift(settings.displayTimezone)
  }
  // The empty value is a real choice, not a missing one: it is what an
  // installation that never configured a timezone runs on, and picking it again
  // is how you go back.
  return ['', ...list]
})

const browserZone = Intl.DateTimeFormat().resolvedOptions().timeZone

function zoneLabel(z) {
  return z || `跟随本机（当前浏览器：${browserZone}）`
}

// Empty is a valid selection, so this compares values rather than testing for
// one being set — otherwise "go back to following the machine" is unsavable.
const dirty = computed(() =>
  zone.value !== settings.displayTimezone ||
  retention.value !== settings.logRetentionDays)

// Previewing means rendering in the zone being considered, not the saved one,
// so the helper is pointed at it just long enough to format and then restored.
const preview = computed(() => {
  const saved = getDisplayTimezone()
  try {
    setDisplayTimezone(zone.value)
    return formatTimeWithZone(now.value)
  } finally {
    setDisplayTimezone(saved)
  }
})

const sampleAlertTime = computed(() => preview.value)

// One save covers both sections, so the note names whichever actually changed.
const changeSummary = computed(() => {
  const parts = []
  if (zone.value !== settings.displayTimezone) {
    parts.push(`时区 ${settings.displayTimezone || '跟随本机'} → ${zone.value || '跟随本机'}`)
  }
  if (retention.value !== settings.logRetentionDays) {
    const label = d => (d ? `${d} 天` : '不清理')
    parts.push(`保留 ${label(settings.logRetentionDays)} → ${label(retention.value)}`)
  }
  return parts.join('　·　')
})

watch(() => settings.displayTimezone, v => { if (!dirty.value) zone.value = v })
watch(() => settings.logRetentionDays, v => { if (!dirty.value) retention.value = v })

function reset() {
  zone.value = settings.displayTimezone
  retention.value = settings.logRetentionDays
}

async function save() {
  saving.value = true
  try {
    const zoneChanged = zone.value !== settings.displayTimezone
    const res = await settings.save({ zone: zone.value, retentionDays: retention.value })
    if (res.code === 0) {
      toast.success(zoneChanged
        ? `显示时区已改为 ${settings.displayTimezone || '跟随本机'}，已有记录会按新时区重新显示`
        : '设置已保存')
      zone.value = settings.displayTimezone
      retention.value = settings.logRetentionDays
    } else {
      toast.error(res.message || '保存失败')
    }
  } catch (e) {
    toast.error(e.response?.data?.message || '保存失败')
  } finally {
    saving.value = false
  }
}

// The preview ticks so the chosen zone reads as a live clock rather than a
// frozen sample.
let ticker = null

onMounted(async () => {
  if (!settings.loaded) await settings.load()
  zone.value = settings.displayTimezone
  retention.value = settings.logRetentionDays
  ticker = setInterval(() => { now.value = new Date() }, 1000)
})

onUnmounted(() => {
  if (ticker) clearInterval(ticker)
})
</script>

<style scoped>
.form-static {
  padding: var(--space-8) 0;
  font-size: var(--fs-16);
  font-variant-numeric: tabular-nums;
  color: var(--text);
}
.settings-note {
  font-size: var(--fs-14);
  color: var(--text-secondary);
  max-width: 70ch;
  line-height: 1.7;
}
.settings-sample {
  margin-top: var(--space-12);
  padding: var(--space-12) var(--space-16);
  background: var(--bg);
  border: 1px solid var(--border);
  border-radius: 6px;
  font-family: ui-monospace, "SF Mono", Menlo, monospace;
  font-size: var(--fs-12);
  line-height: 1.7;
  color: var(--text);
  overflow-x: auto;
}
</style>
