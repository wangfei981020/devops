<template>
  <div class="page">
    <div class="page-head">
      <span class="page-title">告警</span>
      <div>
        <el-radio-group v-model="state" size="small" @change="load">
          <el-radio-button value="current">当前活跃</el-radio-button>
          <el-radio-button value="history">历史</el-radio-button>
        </el-radio-group>
        <el-select v-if="state === 'history'" v-model="hours" size="small" style="width:110px;margin-left:8px" @change="load">
          <el-option label="最近 6 小时" :value="6" />
          <el-option label="最近 24 小时" :value="24" />
          <el-option label="最近 7 天" :value="168" />
        </el-select>
        <el-button :icon="Refresh" style="margin-left:8px" @click="load">刷新</el-button>
      </div>
    </div>

    <LoadError :error="error" title="告警加载失败" @retry="load" />

    <!-- 没接入不是错误，但也绝不能显示成"当前无告警" -->
    <el-alert v-if="notConfigured" type="info" :closable="false" show-icon
              title="尚未接入夜莺">
      <div class="tip">
        到「接入管理 → 数据源」新增一个类型为 <code>n9e</code> 的接入点即可。
        接入后这里会显示当前活跃告警，事件中心也会把 critical / warning 级并进时间线。
      </div>
    </el-alert>

    <template v-else>
      <div class="sum">
        <span class="chip crit" :class="{ on: sev === 'critical' }" @click="toggleSev('critical')">
          紧急 <b>{{ count('critical') }}</b>
        </span>
        <span class="chip warn" :class="{ on: sev === 'warning' }" @click="toggleSev('warning')">
          告警 <b>{{ count('warning') }}</b>
        </span>
        <span class="chip info" :class="{ on: sev === 'info' }" @click="toggleSev('info')">
          提醒 <b>{{ count('info') }}</b>
        </span>
        <el-input v-model="kw" placeholder="搜对象 / 规则" clearable size="small" style="width:200px;margin-left:12px" />
        <span class="muted" style="margin-left:auto">
          取回 {{ returned }} / 夜莺共 {{ total }} 条
          <el-tooltip v-if="truncated" :content="hint">
            <el-tag type="warning" size="small" effect="plain" style="margin-left:6px">已截断</el-tag>
          </el-tooltip>
        </span>
      </div>

      <el-table :data="shown" v-loading="loading" size="small" border style="width:100%">
        <el-table-column label="级别" width="80">
          <template #default="{ row }">
            <el-tag :type="sevType(row.severity)" size="small" effect="dark">{{ sevLabel(row.severity) }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column prop="object" label="对象" min-width="220" show-overflow-tooltip>
          <template #default="{ row }"><span class="mono">{{ row.object }}</span></template>
        </el-table-column>
        <el-table-column prop="rule_name" label="规则" min-width="180" show-overflow-tooltip />
        <el-table-column label="触发值" width="100" align="right">
          <template #default="{ row }"><b v-if="row.trigger_value">{{ row.trigger_value }}</b><span v-else class="muted">—</span></template>
        </el-table-column>
        <el-table-column label="标签" min-width="170" show-overflow-tooltip>
          <template #default="{ row }">
            <el-tag v-for="(v, k) in row.tags" :key="k" size="small" effect="plain" style="margin-right:4px">{{ k }}={{ v }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column prop="trigger_time" label="首次触发" width="160" />
        <el-table-column label="已通知" width="80" align="center">
          <template #default="{ row }">
            <el-tooltip content="通知次数越多说明持续越久">
              <span>{{ row.notified || '—' }}</span>
            </el-tooltip>
          </template>
        </el-table-column>
        <el-table-column v-if="state === 'history'" label="状态" width="90">
          <template #default="{ row }">
            <el-tag v-if="row.recovered" type="success" size="small" effect="plain">已恢复</el-tag>
            <el-tag v-else type="danger" size="small" effect="plain">未恢复</el-tag>
          </template>
        </el-table-column>
      </el-table>
    </template>
  </div>
</template>

<script setup>
// 告警页（数据来自夜莺，只读）。
//
// 和事件中心的分工：事件中心只收 critical/warning，是"平台最近出了什么事"的
// 概览；这里能看到全部级别和触发值细节。实测生产上 info 级有 89 条，
// 全灌进事件中心会把变更/到期/K8s 事件全挤没。
import { ref, computed, onMounted } from 'vue'
import { Refresh } from '@element-plus/icons-vue'
import { listAlerts } from '../api/cmdb'
import { useLoadState } from '../composables/useLoadState'
import LoadError from '../components/LoadError.vue'

const { loading, error, run } = useLoadState()
const rows = ref([])
const total = ref(0)
const returned = ref(0)
const truncated = ref(false)
const hint = ref('')
const notConfigured = ref(false)
const state = ref('current')
const hours = ref(24)
const sev = ref('')
const kw = ref('')

async function load() {
  const r = await run(() => listAlerts({ state: state.value, hours: hours.value, limit: 500 }))
  if (!r) return
  notConfigured.value = r.configured === false
  rows.value = r.list || []
  total.value = r.total || 0
  returned.value = r.returned ?? rows.value.length
  truncated.value = !!r.truncated
  hint.value = r.hint || ''
}
onMounted(load)

function count(s) { return rows.value.filter((x) => x.severity === s).length }
function toggleSev(s) { sev.value = sev.value === s ? '' : s }

const shown = computed(() => {
  const k = kw.value.trim().toLowerCase()
  return rows.value.filter((x) => {
    if (sev.value && x.severity !== sev.value) return false
    if (!k) return true
    return (x.object || '').toLowerCase().includes(k) || (x.rule_name || '').toLowerCase().includes(k)
  })
})

function sevLabel(s) { return { critical: '紧急', warning: '告警', info: '提醒' }[s] || s }
function sevType(s) { return { critical: 'danger', warning: 'warning', info: 'info' }[s] || 'info' }
</script>

<style scoped>
.sum { display: flex; align-items: center; gap: 8px; margin-bottom: 12px; }
.chip { padding: 4px 12px; border-radius: 14px; font-size: 13px; cursor: pointer; border: 1px solid transparent; user-select: none; }
.chip b { margin-left: 4px; font-size: 15px; }
.chip.crit { background: #fef0f0; color: #c45656; }
.chip.warn { background: #fdf6ec; color: #b88230; }
.chip.info { background: #f4f4f5; color: #73767a; }
.chip.on { border-color: currentColor; }
.tip { font-size: 12.5px; line-height: 1.8; color: #606266; }
.mono { font-family: ui-monospace, Menlo, monospace; font-size: 12.5px; }
.muted { color: #909399; font-size: 12px; }
code { background: #f5f7fa; padding: 1px 5px; border-radius: 3px; }
</style>
