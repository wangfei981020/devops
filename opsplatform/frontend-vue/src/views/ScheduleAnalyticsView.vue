<script setup>
import { ref, computed, onMounted, watch } from 'vue'
import { useAppStore, useAuthStore } from '@/stores'
import api from '@/api'

const appStore = useAppStore()
const authStore = useAuthStore()

const canExport = computed(() => authStore.hasPermission('schedule_analytics:export'))
const canSetTarget = computed(() => authStore.hasPermission('schedule_analytics:set_target'))

const currentYear = ref(new Date().getFullYear())
const currentMonth = ref(new Date().getMonth() + 1)
const employees = ref([])
const shiftTypes = ref([])
const loading = ref(false)

// 月度应工作天数配置
const targetWorkDays = ref(0)  // 当前数据库值
const targetInput = ref(0)     // 输入框值（用于"保存"按钮控制脏状态）
const targetUpdatedBy = ref('')
const targetUpdatedAt = ref('')
const isDirty = computed(() => Number(targetInput.value) !== Number(targetWorkDays.value))

// 分类规则
const REST_CODES = ['OD', 'OFF', 'H', 'CT']
const LEAVE_CODES = ['PL', 'SL', 'AL']
function categorize(code) {
  if (REST_CODES.includes(code)) return 'rest'
  if (LEAVE_CODES.includes(code)) return 'leave'
  return 'work'
}

const daysInMonth = computed(() => {
  const y = currentYear.value
  const m = currentMonth.value
  const total = new Date(y, m, 0).getDate()
  return Array.from({ length: total }, (_, i) => {
    const d = i + 1
    const date = new Date(y, m - 1, d)
    return {
      day: d,
      dateStr: `${y}-${String(m).padStart(2, '0')}-${String(d).padStart(2, '0')}`,
      isWeekend: date.getDay() === 0 || date.getDay() === 6
    }
  })
})

const groupFilter = ref('')
const allGroups = computed(() => {
  const s = new Set()
  employees.value.forEach(e => { if (e.group_name) s.add(e.group_name) })
  return Array.from(s)
})
const filteredEmployees = computed(() => {
  if (!groupFilter.value) return employees.value
  return employees.value.filter(e => e.group_name === groupFilter.value)
})

// 每个员工的三段统计
const employeeStats = computed(() => {
  const map = {}
  employees.value.forEach(emp => {
    const stat = {
      work: { total: 0, items: [] },
      rest: { total: 0, items: [] },
      leave: { total: 0, items: [] }
    }
    const counts = {}
    daysInMonth.value.forEach(d => {
      const shift = emp.shifts?.[d.dateStr]
      if (shift) counts[shift] = (counts[shift] || 0) + 1
    })
    shiftTypes.value.forEach(t => {
      const c = counts[t.code] || 0
      if (c === 0) return
      const cat = categorize(t.code)
      stat[cat].total += c
      stat[cat].items.push({ code: t.code, name: t.name, count: c, color: t.color })
    })
    map[emp.id] = stat
  })
  return map
})

// 全员概览
const overview = computed(() => {
  let work = 0, rest = 0, leave = 0
  filteredEmployees.value.forEach(emp => {
    const s = employeeStats.value[emp.id]
    if (!s) return
    work  += s.work.total
    rest  += s.rest.total
    leave += s.leave.total
  })
  const expectedPerPerson = Number(targetWorkDays.value) || 0
  const expectedTotal = expectedPerPerson * filteredEmployees.value.length
  const reachRate = expectedTotal > 0 ? Math.round((work / expectedTotal) * 1000) / 10 : 0
  return { work, rest, leave, expected: expectedTotal, expectedPerPerson, reachRate, peopleCount: filteredEmployees.value.length }
})

// 员工达成状态
function reachState(empId) {
  const s = employeeStats.value[empId]
  const expected = Number(targetWorkDays.value) || 0
  if (!s || expected === 0) return { label: '未配置', tone: 'unset', delta: 0 }
  const actual = s.work.total
  if (actual < expected) return { label: `差 ${expected - actual} 天`, tone: 'low', delta: actual - expected }
  if (actual === expected) return { label: '达成', tone: 'ok', delta: 0 }
  return { label: `超 ${actual - expected} 天 加班`, tone: 'high', delta: actual - expected }
}

// 月份切换
function prevMonth() {
  if (currentMonth.value === 1) { currentMonth.value = 12; currentYear.value-- }
  else currentMonth.value--
}
function nextMonth() {
  if (currentMonth.value === 12) { currentMonth.value = 1; currentYear.value++ }
  else currentMonth.value++
}
function today() {
  const now = new Date()
  currentYear.value = now.getFullYear()
  currentMonth.value = now.getMonth() + 1
}

// 加载数据
async function loadAll() {
  loading.value = true
  try {
    const [scheduleRes, configRes, targetRes] = await Promise.all([
      api.get(`/api/schedule?year=${currentYear.value}&month=${currentMonth.value}`),
      api.get('/api/schedule/config'),
      api.get(`/api/schedule/month-target?year=${currentYear.value}&month=${currentMonth.value}`)
    ])
    employees.value = scheduleRes.data || []
    if (configRes.data && configRes.data.length > 0) shiftTypes.value = configRes.data
    const t = targetRes.data || {}
    targetWorkDays.value = Number(t.expected_work_days || 0)
    targetInput.value = targetWorkDays.value
    targetUpdatedBy.value = t.updated_by || ''
    targetUpdatedAt.value = t.updated_at || ''
  } catch (e) {
    console.error('加载排班统计失败', e)
    appStore.showToast('加载失败', 'error')
  } finally {
    loading.value = false
  }
}

async function saveTarget() {
  const v = Number(targetInput.value)
  if (!Number.isInteger(v) || v < 0 || v > 31) {
    appStore.showToast('应工作天数必须是 0-31 之间的整数', 'error')
    return
  }
  try {
    await api.put('/api/schedule/month-target', {
      year: currentYear.value,
      month: currentMonth.value,
      expected_work_days: v
    })
    targetWorkDays.value = v
    appStore.showToast('保存成功', 'success')
    // 刷新更新人/时间
    const res = await api.get(`/api/schedule/month-target?year=${currentYear.value}&month=${currentMonth.value}`)
    targetUpdatedBy.value = res.data?.updated_by || ''
    targetUpdatedAt.value = res.data?.updated_at || ''
  } catch (e) {
    appStore.showToast('保存失败: ' + (e.response?.data || e.message), 'error')
  }
}

// 导出 Excel（CSV，跟现有排班页风格一致）
function exportExcel() {
  const cols = ['组别', '姓名', '职位', '应工作', '实工作', '休息', '请假', '达成状态', '工作明细', '休息明细', '请假明细']
  const rows = [cols.join(',')]
  filteredEmployees.value.forEach(emp => {
    const s = employeeStats.value[emp.id]
    const rs = reachState(emp.id)
    const workDetail = (s.work.items || []).map(it => `${it.name} ${it.count}`).join(' / ')
    const restDetail = (s.rest.items || []).map(it => `${it.name} ${it.count}`).join(' / ')
    const leaveDetail = (s.leave.items || []).map(it => `${it.name} ${it.count}`).join(' / ')
    rows.push([
      emp.group_name || '', emp.name, emp.role || '',
      targetWorkDays.value || '-', s.work.total, s.rest.total, s.leave.total, rs.label,
      `"${workDetail}"`, `"${restDetail}"`, `"${leaveDetail}"`
    ].join(','))
  })
  const csv = '﻿' + rows.join('\n')
  const blob = new Blob([csv], { type: 'text/csv;charset=utf-8;' })
  const url = URL.createObjectURL(blob)
  const link = document.createElement('a')
  link.href = url
  link.download = `排班统计_${currentYear.value}年${currentMonth.value}月.csv`
  link.click()
  URL.revokeObjectURL(url)
  appStore.showToast('导出成功', 'success')
}

onMounted(loadAll)
watch([currentYear, currentMonth], loadAll)
</script>

<template>
  <div class="page-container">
    <div class="page-header">
      <div>
        <h2 class="page-title">排班统计分析</h2>
        <p class="page-desc">按月查看每个员工的工作 / 休息 / 请假分布，对照应工作天数判定达成。</p>
      </div>
      <button v-if="canExport" class="btn btn-primary" @click="exportExcel">
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4"/><polyline points="7 10 12 15 17 10"/><line x1="12" y1="15" x2="12" y2="3"/></svg>
        导出 Excel
      </button>
    </div>

    <div class="toolbar">
      <div class="month-nav">
        <button class="nav-btn" @click="prevMonth">‹</button>
        <span class="month-label">{{ currentYear }}年{{ currentMonth }}月</span>
        <button class="nav-btn" @click="nextMonth">›</button>
        <button class="btn-link" @click="today">今天</button>
      </div>
      <div class="filter-group">
        <label>组别：</label>
        <select v-model="groupFilter" class="select">
          <option value="">全部</option>
          <option v-for="g in allGroups" :key="g" :value="g">{{ g }}</option>
        </select>
      </div>
    </div>

    <!-- 月度应工作天数配置 -->
    <div class="target-card">
      <div class="target-left">
        <span class="target-label">本月应工作天数：</span>
        <input type="number" min="0" max="31" v-model.number="targetInput"
               :disabled="!canSetTarget"
               class="target-input" />
        <span class="target-unit">天</span>
        <button v-if="canSetTarget" class="btn btn-primary btn-sm"
                :disabled="!isDirty" @click="saveTarget">💾 保存</button>
        <span v-if="!canSetTarget" class="readonly-hint">（只有管理员可改）</span>
      </div>
      <div class="target-right" v-if="targetUpdatedAt">
        <span class="updated-info">最近修改：{{ targetUpdatedBy || '-' }} · {{ targetUpdatedAt }}</span>
      </div>
      <div class="target-right" v-else>
        <span class="updated-info muted">尚未配置（请填写后保存）</span>
      </div>
    </div>

    <!-- 全员概览 -->
    <div class="overview-grid">
      <div class="overview-card oc-expected">
        <div class="oc-label">应工作（人天）</div>
        <div class="oc-value">{{ overview.expected }}</div>
        <div class="oc-sub">{{ overview.expectedPerPerson }} × {{ overview.peopleCount }} 人</div>
      </div>
      <div class="overview-card oc-work">
        <div class="oc-label">实工作（人天）</div>
        <div class="oc-value">{{ overview.work }}</div>
        <div class="oc-sub">达成 {{ overview.reachRate }}%</div>
      </div>
      <div class="overview-card oc-rest">
        <div class="oc-label">休息（人天）</div>
        <div class="oc-value">{{ overview.rest }}</div>
      </div>
      <div class="overview-card oc-leave">
        <div class="oc-label">请假（人天）</div>
        <div class="oc-value">{{ overview.leave }}</div>
      </div>
    </div>

    <!-- 员工明细 -->
    <div class="detail-table-wrapper">
      <table class="detail-table">
        <thead>
          <tr>
            <th>组别</th>
            <th>姓名</th>
            <th>职位</th>
            <th>应 / 实</th>
            <th>🟢 工作</th>
            <th>🔵 休息</th>
            <th>🟡 请假</th>
            <th>达成状态</th>
          </tr>
        </thead>
        <tbody>
          <tr v-if="loading"><td colspan="8" class="loading-cell">加载中...</td></tr>
          <tr v-else-if="filteredEmployees.length === 0"><td colspan="8" class="empty-cell">暂无员工</td></tr>
          <tr v-else v-for="emp in filteredEmployees" :key="emp.id">
            <td class="group-cell">{{ emp.group_name || '-' }}</td>
            <td class="name-cell">{{ emp.name }}</td>
            <td class="role-cell">{{ emp.role || '-' }}</td>
            <td class="ae-cell">
              <span class="ae-expected">{{ targetWorkDays || '-' }}</span>
              <span class="ae-sep">/</span>
              <span class="ae-actual">{{ employeeStats[emp.id]?.work?.total || 0 }}</span>
            </td>
            <td class="cat-cell">
              <div class="cat-total work">{{ employeeStats[emp.id]?.work?.total || 0 }}</div>
              <div v-for="it in employeeStats[emp.id]?.work?.items || []" :key="it.code" class="cat-item">
                <span class="cat-item-name">{{ it.name }}</span>
                <span class="cat-item-count">{{ it.count }}</span>
              </div>
            </td>
            <td class="cat-cell">
              <div class="cat-total rest">{{ employeeStats[emp.id]?.rest?.total || 0 }}</div>
              <div v-for="it in employeeStats[emp.id]?.rest?.items || []" :key="it.code" class="cat-item">
                <span class="cat-item-name">{{ it.name }}</span>
                <span class="cat-item-count">{{ it.count }}</span>
              </div>
            </td>
            <td class="cat-cell">
              <div class="cat-total leave">{{ employeeStats[emp.id]?.leave?.total || 0 }}</div>
              <div v-for="it in employeeStats[emp.id]?.leave?.items || []" :key="it.code" class="cat-item">
                <span class="cat-item-name">{{ it.name }}</span>
                <span class="cat-item-count">{{ it.count }}</span>
              </div>
            </td>
            <td>
              <span class="reach-badge" :class="'reach-' + reachState(emp.id).tone">{{ reachState(emp.id).label }}</span>
            </td>
          </tr>
        </tbody>
      </table>
    </div>
  </div>
</template>

<style scoped>
.page-container { padding: 20px; }
.page-header { display: flex; justify-content: space-between; align-items: flex-start; margin-bottom: 16px; }
.page-title { font-size: 20px; font-weight: 600; margin: 0; }
.page-desc { font-size: 13px; color: var(--text-secondary); margin: 4px 0 0; }

.btn { display: inline-flex; align-items: center; gap: 6px; padding: 8px 14px; border-radius: 8px; border: 1px solid var(--border-color); background: var(--bg-card); color: var(--text-color); cursor: pointer; font-size: 13px; transition: all 0.15s; }
.btn:hover:not(:disabled) { background: var(--bg-hover); }
.btn:disabled { opacity: 0.5; cursor: not-allowed; }
.btn-primary { background: var(--primary); color: #fff; border-color: var(--primary); }
.btn-primary:hover:not(:disabled) { filter: brightness(1.1); }
.btn-sm { padding: 4px 10px; font-size: 12px; }
.btn svg { width: 14px; height: 14px; }
.btn-link { background: transparent; border: none; color: var(--primary); padding: 4px 8px; cursor: pointer; font-size: 13px; }

.toolbar { display: flex; justify-content: space-between; align-items: center; gap: 16px; margin-bottom: 16px; padding: 10px 14px; background: var(--bg-card); border: 1px solid var(--border-color); border-radius: 10px; }
.month-nav { display: flex; align-items: center; gap: 8px; }
.nav-btn { width: 28px; height: 28px; border-radius: 6px; border: 1px solid var(--border-color); background: var(--bg-card); color: var(--text-color); cursor: pointer; font-size: 16px; }
.nav-btn:hover { background: var(--bg-hover); }
.month-label { min-width: 100px; text-align: center; font-weight: 600; font-size: 14px; }
.filter-group { display: flex; align-items: center; gap: 6px; }
.filter-group label { font-size: 13px; color: var(--text-secondary); }
.select { padding: 6px 10px; border: 1px solid var(--border-color); border-radius: 6px; background: var(--bg-card); color: var(--text-color); font-size: 13px; }

.target-card { display: flex; justify-content: space-between; align-items: center; padding: 12px 16px; background: var(--bg-card); border: 1px solid var(--border-color); border-radius: 10px; margin-bottom: 16px; gap: 12px; flex-wrap: wrap; }
.target-left { display: flex; align-items: center; gap: 8px; flex-wrap: wrap; }
.target-label { font-weight: 600; }
.target-input { width: 80px; padding: 6px 10px; border: 1px solid var(--border-color); border-radius: 6px; background: var(--bg-card); color: var(--text-color); font-size: 14px; text-align: center; }
.target-input:disabled { opacity: 0.6; cursor: not-allowed; }
.target-unit { color: var(--text-secondary); font-size: 13px; }
.readonly-hint { font-size: 12px; color: var(--text-secondary); margin-left: 6px; }
.updated-info { font-size: 12px; color: var(--text-secondary); }
.updated-info.muted { font-style: italic; color: #f59e0b; }

.overview-grid { display: grid; grid-template-columns: repeat(4, 1fr); gap: 12px; margin-bottom: 16px; }
.overview-card { padding: 14px 16px; border: 1px solid var(--border-color); border-radius: 10px; background: var(--bg-card); }
.oc-label { font-size: 12px; color: var(--text-secondary); margin-bottom: 6px; }
.oc-value { font-size: 24px; font-weight: 700; line-height: 1.1; }
.oc-sub { font-size: 11px; color: var(--text-secondary); margin-top: 4px; }
.oc-expected .oc-value { color: var(--primary); }
.oc-work .oc-value { color: #10b981; }
.oc-rest .oc-value { color: #3a84ff; }
.oc-leave .oc-value { color: #f59e0b; }

.detail-table-wrapper { background: var(--bg-card); border: 1px solid var(--border-color); border-radius: 10px; overflow: auto; }
.detail-table { width: 100%; border-collapse: collapse; }
.detail-table th, .detail-table td { padding: 10px 12px; text-align: left; border-bottom: 1px solid var(--border-color); font-size: 13px; vertical-align: top; }
.detail-table th { background: var(--bg-card); font-weight: 600; font-size: 12px; color: var(--text-secondary); position: sticky; top: 0; z-index: 5; }
.detail-table tbody tr:hover { background: var(--bg-hover); }
.loading-cell, .empty-cell { text-align: center; padding: 30px; color: var(--text-secondary); }

.group-cell { color: var(--text-secondary); white-space: nowrap; }
.name-cell { font-weight: 600; }
.role-cell { font-size: 12px; color: var(--text-secondary); }

.ae-cell { font-variant-numeric: tabular-nums; white-space: nowrap; }
.ae-expected { color: var(--text-secondary); }
.ae-sep { margin: 0 4px; color: var(--text-secondary); }
.ae-actual { font-weight: 700; font-size: 15px; }

.cat-cell { min-width: 130px; }
.cat-total { font-size: 15px; font-weight: 700; margin-bottom: 4px; }
.cat-total.work { color: #10b981; }
.cat-total.rest { color: #3a84ff; }
.cat-total.leave { color: #f59e0b; }
.cat-item { display: flex; justify-content: space-between; padding-left: 8px; font-size: 12px; line-height: 1.5; color: var(--text-secondary); }
.cat-item-count { font-variant-numeric: tabular-nums; font-weight: 600; }

.reach-badge { display: inline-block; padding: 3px 10px; border-radius: 12px; font-size: 12px; font-weight: 600; }
.reach-ok { background: rgba(16, 185, 129, 0.15); color: #10b981; }
.reach-high { background: rgba(58, 132, 255, 0.15); color: #3a84ff; }
.reach-low { background: rgba(249, 115, 22, 0.15); color: #f97316; }
.reach-unset { background: rgba(148, 163, 184, 0.15); color: var(--text-secondary); }
</style>
