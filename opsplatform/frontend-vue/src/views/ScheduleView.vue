<script setup>
import { ref, computed, onMounted, watch } from 'vue'
import { useAppStore, useAuthStore } from '@/stores'
import api from '@/api'

const appStore = useAppStore()
const authStore = useAuthStore()

// 权限检查
const canAddEmployee = computed(() => authStore.hasPermission('schedule:add_employee'))
const canEditEmployee = computed(() => authStore.hasPermission('schedule:edit_employee'))
const canDeleteEmployee = computed(() => authStore.hasPermission('schedule:delete_employee'))
const canBatchSchedule = computed(() => authStore.hasPermission('schedule:batch'))
const canConfigShift = computed(() => authStore.hasPermission('schedule:config'))
const canExport = computed(() => authStore.hasPermission('schedule:export'))
const canReset = computed(() => authStore.hasPermission('schedule:reset'))
const canEditShift = computed(() => authStore.hasPermission('schedule:edit_shift'))

const employees = ref([])
const loading = ref(false)
const currentYear = ref(new Date().getFullYear())
const currentMonth = ref(new Date().getMonth() + 1)

const showEmployeeModal = ref(false)
const employeeForm = ref({ name: '', group_name: '', role: '运维工程师' })
const employeeModalMode = ref('add')
const editingEmployeeId = ref(null)

const showConfigModal = ref(false)
const showBatchModal = ref(false)
const batchForm = ref({ employees: [], startDate: '', endDate: '', shiftCode: '', weekdays: [1, 2, 3, 4, 5] })
const selectedEmployees = ref([])

// 拖拽排序相关
const draggingEmployee = ref(null)
const dragOverEmployee = ref(null)

// 班次选择器
const showShiftPicker = ref(false)
const shiftPickerTarget = ref({ empId: null, dateStr: '', x: 0, y: 0 })

const shiftTypes = ref([
  { code: 'A', label: 'A', name: '早班', time: '09:00-18:00', color: '#3a84ff', isDuty: false },
  { code: 'B', label: 'B', name: '中班', time: '15:00-24:00', color: '#ff9c01', isDuty: false },
  { code: 'C', label: 'C', name: '晚班', time: '24:00-09:00', color: '#8b5cf6', isDuty: false },
  { code: 'D', label: 'D', name: '值班', time: '全天', color: '#ea3636', isDuty: true },
  { code: 'OD', label: 'OD', name: '周末休', time: '-', color: '#6b7280', isDuty: false },
  { code: 'OFF', label: 'OFF', name: '排班休', time: '-', color: '#94a3b8', isDuty: false },
  { code: 'H', label: 'H', name: '公共假期', time: '-', color: '#10b981', isDuty: false },
  { code: 'PL', label: 'PL', name: '事假', time: '-', color: '#f59e0b', isDuty: false },
  { code: 'SL', label: 'SL', name: '病假', time: '-', color: '#ef4444', isDuty: false },
  { code: 'AL', label: 'AL', name: '年假', time: '-', color: '#06b6d4', isDuty: false },
  { code: 'CT', label: 'CT', name: '调休', time: '-', color: '#a855f7', isDuty: false }
])

const weekDays = ['日', '一', '二', '三', '四', '五', '六']

const daysInMonth = computed(() => {
  const days = []
  const year = currentYear.value
  const month = currentMonth.value
  const totalDays = new Date(year, month, 0).getDate()
  
  for (let day = 1; day <= totalDays; day++) {
    const date = new Date(year, month - 1, day)
    const dateStr = `${year}-${String(month).padStart(2, '0')}-${String(day).padStart(2, '0')}`
    days.push({
      day,
      dateStr,
      weekDay: date.getDay(),
      isWeekend: date.getDay() === 0 || date.getDay() === 6
    })
  }
  return days
})

const groupedEmployees = computed(() => {
  const groups = {}
  employees.value.forEach(emp => {
    const group = emp.group_name || '未分组'
    if (!groups[group]) groups[group] = []
    groups[group].push(emp)
  })
  return groups
})

const dailyStats = computed(() => {
  const stats = {}
  daysInMonth.value.forEach(d => {
    const dateStr = d.dateStr
    stats[dateStr] = { A: 0, B: 0, C: 0, D: 0 }
    employees.value.forEach(emp => {
      const shift = emp.shifts?.[dateStr]
      if (shift === 'A') stats[dateStr].A++
      else if (shift === 'B') stats[dateStr].B++
      else if (shift === 'C') stats[dateStr].C++
      else if (shift === 'D') stats[dateStr].D++
    })
  })
  return stats
})

onMounted(() => {
  loadSchedule()
  loadShiftConfig()
})

watch([currentYear, currentMonth], () => {
  loadSchedule()
})

async function loadSchedule() {
  loading.value = true
  try {
    const res = await api.get(`/api/schedule?year=${currentYear.value}&month=${currentMonth.value}`)
    console.log('排班数据API响应:', res.data)
    if (res.data && res.data.length > 0) {
      console.log('第一个员工的shifts:', res.data[0].shifts)
    }
    employees.value = res.data || []
  } catch (e) {
    console.error(e)
    appStore.showToast('加载排班数据失败', 'error')
  } finally {
    loading.value = false
  }
}

async function loadShiftConfig() {
  try {
    const res = await api.get('/api/schedule/config')
    if (res.data && res.data.length > 0) {
      shiftTypes.value = res.data
    }
  } catch (e) { console.error(e) }
}

async function updateShift(employeeId, dateStr, shiftType) {
  try {
    await api.post('/api/schedule/shift', {
      employeeId,
      date: dateStr,
      shiftType
    })
    const emp = employees.value.find(e => e.id === employeeId)
    if (emp) {
      if (!emp.shifts) emp.shifts = {}
      if (shiftType) emp.shifts[dateStr] = shiftType
      else delete emp.shifts[dateStr]
    }
  } catch (e) {
    appStore.showToast('保存失败', 'error')
  }
}

function openShiftPicker(emp, dateStr, event) {
  const rect = event.target.getBoundingClientRect()
  shiftPickerTarget.value = {
    empId: emp.id,
    dateStr: dateStr,
    currentShift: emp.shifts?.[dateStr] || '',
    x: rect.left,
    y: rect.bottom + 4
  }
  showShiftPicker.value = true
}

function selectShift(shiftCode) {
  if (shiftPickerTarget.value.empId) {
    updateShift(shiftPickerTarget.value.empId, shiftPickerTarget.value.dateStr, shiftCode)
  }
  showShiftPicker.value = false
}

function closeShiftPicker() {
  showShiftPicker.value = false
}

function getShiftStyle(shiftCode) {
  const shift = shiftTypes.value.find(s => s.code === shiftCode)
  if (!shift) return {}
  return { background: shift.color, color: 'white' }
}

function getShiftInfo(shiftCode) {
  return shiftTypes.value.find(s => s.code === shiftCode) || null
}

function openEmployeeModal(mode, emp = null) {
  employeeModalMode.value = mode
  if (mode === 'edit' && emp) {
    editingEmployeeId.value = emp.id
    employeeForm.value = { name: emp.name, group_name: emp.group_name || '', role: emp.role }
  } else {
    editingEmployeeId.value = null
    employeeForm.value = { name: '', group_name: '', role: '运维工程师' }
  }
  showEmployeeModal.value = true
}

async function saveEmployee() {
  if (!employeeForm.value.name) {
    appStore.showToast('请输入姓名', 'error')
    return
  }
  try {
    if (employeeModalMode.value === 'edit') {
      const emp = employees.value.find(e => e.id === editingEmployeeId.value)
      if (emp) {
        emp.name = employeeForm.value.name
        emp.group_name = employeeForm.value.group_name
        emp.role = employeeForm.value.role
        await api.post('/api/schedule', [emp])
      }
    } else {
      await api.post('/api/schedule/employee', employeeForm.value)
    }
    showEmployeeModal.value = false
    loadSchedule()
    appStore.showToast('保存成功', 'success')
  } catch (e) {
    appStore.showToast('保存失败', 'error')
  }
}

async function deleteEmployee(emp) {
  const confirmed = await appStore.showConfirm({
    type: 'danger', title: '确认删除',
    message: `确定要删除员工 "${emp.name}" 吗？其所有排班记录也会被删除。`,
    confirmText: '删除', cancelText: '取消'
  })
  if (!confirmed) return
  try {
    await api.delete(`/api/schedule/employee?id=${emp.id}`)
    loadSchedule()
    appStore.showToast('删除成功', 'success')
  } catch (e) {
    appStore.showToast('删除失败', 'error')
  }
}

function prevMonth() {
  if (currentMonth.value === 1) {
    currentMonth.value = 12
    currentYear.value--
  } else {
    currentMonth.value--
  }
}

function nextMonth() {
  if (currentMonth.value === 12) {
    currentMonth.value = 1
    currentYear.value++
  } else {
    currentMonth.value++
  }
}

function goToToday() {
  currentYear.value = new Date().getFullYear()
  currentMonth.value = new Date().getMonth() + 1
}

async function saveShiftConfig() {
  try {
    await api.post('/api/schedule/config', shiftTypes.value)
    showConfigModal.value = false
    appStore.showToast('班次配置已保存', 'success')
  } catch (e) {
    appStore.showToast('保存失败', 'error')
  }
}

function addShiftType() {
  shiftTypes.value.push({
    code: '', label: '', name: '', time: '', color: '#3a84ff', isDuty: false
  })
}

function removeShiftType(idx) {
  if (shiftTypes.value.length <= 1) {
    appStore.showToast('至少保留一个班次', 'warning')
    return
  }
  shiftTypes.value.splice(idx, 1)
}

// 拖拽开始
function onDragStart(emp, event) {
  draggingEmployee.value = emp
  event.dataTransfer.effectAllowed = 'move'
  event.dataTransfer.setData('text/plain', emp.id)
  event.target.closest('tr').classList.add('dragging')
}

// 拖拽结束
function onDragEnd(event) {
  draggingEmployee.value = null
  dragOverEmployee.value = null
  event.target.closest('tr')?.classList.remove('dragging')
  document.querySelectorAll('.drag-over').forEach(el => el.classList.remove('drag-over'))
}

// 拖拽经过
function onDragOver(emp, event) {
  event.preventDefault()
  if (draggingEmployee.value && draggingEmployee.value.id !== emp.id) {
    dragOverEmployee.value = emp
    event.target.closest('tr')?.classList.add('drag-over')
  }
}

// 拖拽离开
function onDragLeave(event) {
  event.target.closest('tr')?.classList.remove('drag-over')
}

// 放置
function onDrop(emp, event) {
  event.preventDefault()
  if (!draggingEmployee.value || draggingEmployee.value.id === emp.id) return
  
  const fromIdx = employees.value.findIndex(e => e.id === draggingEmployee.value.id)
  const toIdx = employees.value.findIndex(e => e.id === emp.id)
  
  if (fromIdx !== -1 && toIdx !== -1) {
    const [moved] = employees.value.splice(fromIdx, 1)
    employees.value.splice(toIdx, 0, moved)
    saveEmployeeOrder()
  }
  
  draggingEmployee.value = null
  dragOverEmployee.value = null
  document.querySelectorAll('.drag-over, .dragging').forEach(el => {
    el.classList.remove('drag-over', 'dragging')
  })
}

async function saveEmployeeOrder() {
  try {
    const orderData = employees.value.map((e, i) => ({ id: e.id, sort_order: i }))
    await api.post('/api/schedule/employee/order', orderData)
  } catch (e) {
    console.error('保存排序失败', e)
  }
}

async function exportToExcel() {
  const header = ['姓名', '组别', ...daysInMonth.value.map(d => `${d.day}日(${weekDays[d.weekDay]})`)]
  const rows = employees.value.map(emp => {
    const row = [emp.name, emp.group_name || '']
    daysInMonth.value.forEach(d => {
      row.push(emp.shifts?.[d.dateStr] || '')
    })
    return row
  })

  let csv = '\uFEFF'
  csv += header.join(',') + '\n'
  rows.forEach(row => {
    csv += row.map(cell => `"${cell}"`).join(',') + '\n'
  })

  csv += '\n班次说明\n'
  shiftTypes.value.forEach(s => {
    csv += `${s.code},${s.name},${s.time}\n`
  })

  const blob = new Blob([csv], { type: 'text/csv;charset=utf-8;' })
  const url = URL.createObjectURL(blob)
  const link = document.createElement('a')
  link.href = url
  link.download = `排班表_${currentYear.value}年${currentMonth.value}月.csv`
  link.click()
  URL.revokeObjectURL(url)
  appStore.showToast('导出成功', 'success')
}

async function resetSchedule() {
  const confirmed = await appStore.showConfirm({
    type: 'warning',
    title: '确认重置排班',
    message: `确定要重置 ${currentYear.value}年${currentMonth.value}月 的所有排班数据吗？此操作不可恢复！`,
    okText: '确定重置',
    cancelText: '取消'
  })
  if (!confirmed) return

  try {
    const res = await api.post('/api/schedule/reset', {
      year: currentYear.value,
      month: currentMonth.value
    })
    appStore.showToast(res.data.message || '重置成功', 'success')
    loadSchedule()
  } catch (e) {
    console.error('重置排班失败:', e)
    appStore.showToast('重置排班失败: ' + (e.response?.data || e.message), 'error')
  }
}

function hasMissingShifts(dateStr) {
  const stats = dailyStats.value[dateStr]
  return stats && (stats.A === 0 || stats.B === 0 || stats.C === 0)
}

function hasDutyPerson(dateStr) {
  return dailyStats.value[dateStr]?.D > 0
}

function openBatchModal() {
  const year = currentYear.value
  const month = currentMonth.value
  const lastDay = new Date(year, month, 0).getDate()
  batchForm.value = {
    employees: [],
    startDate: `${year}-${String(month).padStart(2, '0')}-01`,
    endDate: `${year}-${String(month).padStart(2, '0')}-${String(lastDay).padStart(2, '0')}`,
    shiftCode: 'A',
    weekdays: [1, 2, 3, 4, 5]
  }
  selectedEmployees.value = []
  showBatchModal.value = true
}

function toggleEmployeeSelection(emp) {
  const idx = selectedEmployees.value.findIndex(e => e.id === emp.id)
  if (idx >= 0) {
    selectedEmployees.value.splice(idx, 1)
  } else {
    selectedEmployees.value.push(emp)
  }
}

function selectAllEmployees() {
  if (selectedEmployees.value.length === employees.value.length) {
    selectedEmployees.value = []
  } else {
    selectedEmployees.value = [...employees.value]
  }
}

async function applyBatchShift() {
  if (selectedEmployees.value.length === 0) {
    appStore.showToast('请选择员工', 'error')
    return
  }
  if (!batchForm.value.shiftCode) {
    appStore.showToast('请选择班次', 'error')
    return
  }

  const startDate = new Date(batchForm.value.startDate + 'T00:00:00')
  const endDate = new Date(batchForm.value.endDate + 'T23:59:59')
  const updates = []

  const currentDate = new Date(startDate)
  while (currentDate <= endDate) {
    const weekday = currentDate.getDay()
    if (batchForm.value.weekdays.includes(weekday)) {
      const year = currentDate.getFullYear()
      const month = String(currentDate.getMonth() + 1).padStart(2, '0')
      const day = String(currentDate.getDate()).padStart(2, '0')
      const dateStr = `${year}-${month}-${day}`
      selectedEmployees.value.forEach(emp => {
        updates.push({ employee_id: emp.id, date: dateStr, shift_code: batchForm.value.shiftCode })
      })
    }
    currentDate.setDate(currentDate.getDate() + 1)
  }

  if (updates.length === 0) {
    appStore.showToast('没有符合条件的日期', 'warning')
    return
  }

  try {
    console.log('批量排班请求:', updates)
    const res = await api.post('/api/schedule/batch', { updates })
    console.log('批量排班响应:', res.data)
    const successCount = res.data?.success || updates.length
    appStore.showToast(`已更新 ${successCount} 条排班记录`, 'success')
    showBatchModal.value = false
    console.log('开始重新加载排班数据...')
    await loadSchedule()
    console.log('排班数据加载完成:', employees.value)
  } catch (e) {
    console.error('批量排班失败:', e)
    appStore.showToast('批量设置失败: ' + (e.response?.data || e.message), 'error')
  }
}

const weekdayOptions = [
  { value: 0, label: '周日' },
  { value: 1, label: '周一' },
  { value: 2, label: '周二' },
  { value: 3, label: '周三' },
  { value: 4, label: '周四' },
  { value: 5, label: '周五' },
  { value: 6, label: '周六' }
]
</script>

<template>
  <div class="schedule-page">
    <div class="page-header">
      <h2>排班管理</h2>
      <div class="header-actions">
        <button v-if="canConfigShift" class="btn btn-secondary" @click="showConfigModal = true">
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M12.22 2h-.44a2 2 0 0 0-2 2v.18a2 2 0 0 1-1 1.73l-.43.25a2 2 0 0 1-2 0l-.15-.08a2 2 0 0 0-2.73.73l-.22.38a2 2 0 0 0 .73 2.73l.15.1a2 2 0 0 1 1 1.72v.51a2 2 0 0 1-1 1.74l-.15.09a2 2 0 0 0-.73 2.73l.22.38a2 2 0 0 0 2.73.73l.15-.08a2 2 0 0 1 2 0l.43.25a2 2 0 0 1 1 1.73V20a2 2 0 0 0 2 2h.44a2 2 0 0 0 2-2v-.18a2 2 0 0 1 1-1.73l.43-.25a2 2 0 0 1 2 0l.15.08a2 2 0 0 0 2.73-.73l.22-.39a2 2 0 0 0-.73-2.73l-.15-.08a2 2 0 0 1-1-1.74v-.5a2 2 0 0 1 1-1.74l.15-.09a2 2 0 0 0 .73-2.73l-.22-.38a2 2 0 0 0-2.73-.73l-.15.08a2 2 0 0 1-2 0l-.43-.25a2 2 0 0 1-1-1.73V4a2 2 0 0 0-2-2z"/><circle cx="12" cy="12" r="3"/></svg>
          班次配置
        </button>
        <button v-if="canBatchSchedule" class="btn btn-secondary" @click="openBatchModal">
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><rect x="3" y="4" width="18" height="18" rx="2" ry="2"/><line x1="16" y1="2" x2="16" y2="6"/><line x1="8" y1="2" x2="8" y2="6"/><line x1="3" y1="10" x2="21" y2="10"/><path d="M9 16l2 2 4-4"/></svg>
          批量排班
        </button>
        <button v-if="canReset" class="btn btn-secondary btn-danger-outline" @click="resetSchedule">
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M3 12a9 9 0 1 0 9-9 9.75 9.75 0 0 0-6.74 2.74L3 8"/><path d="M3 3v5h5"/></svg>
          重置排班
        </button>
        <button v-if="canExport" class="btn btn-secondary" @click="exportToExcel">
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4"/><polyline points="7 10 12 15 17 10"/><line x1="12" y1="15" x2="12" y2="3"/></svg>
          导出Excel
        </button>
        <button v-if="canAddEmployee" class="btn btn-primary" @click="openEmployeeModal('add')">
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><line x1="12" y1="5" x2="12" y2="19"/><line x1="5" y1="12" x2="19" y2="12"/></svg>
          添加员工
        </button>
      </div>
    </div>

    <div class="month-nav">
      <button class="nav-btn" @click="prevMonth"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><polyline points="15 18 9 12 15 6"/></svg></button>
      <span class="current-month">{{ currentYear }}年{{ currentMonth }}月</span>
      <button class="nav-btn" @click="nextMonth"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><polyline points="9 18 15 12 9 6"/></svg></button>
      <button class="btn btn-text" @click="goToToday">今天</button>
    </div>

    <div class="shift-legend">
      <div v-for="s in shiftTypes" :key="s.code" class="legend-item" :style="{ '--shift-color': s.color }">
        <span class="legend-code" :style="{ background: s.color }">{{ s.code }}</span>
        <span class="legend-name">{{ s.name }}</span>
        <span class="legend-time" v-if="s.time && s.time !== '-'">{{ s.time }}</span>
        <span class="legend-duty" v-if="s.isDuty">值班</span>
      </div>
    </div>

    <div class="schedule-container">
      <div v-if="loading" class="loading-state">加载中...</div>
      <div v-else-if="employees.length === 0" class="empty-state">
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5"><path d="M17 21v-2a4 4 0 0 0-4-4H5a4 4 0 0 0-4 4v2"/><circle cx="9" cy="7" r="4"/><path d="M23 21v-2a4 4 0 0 0-3-3.87"/><path d="M16 3.13a4 4 0 0 1 0 7.75"/></svg>
        <div>暂无员工</div>
        <p>点击"添加员工"开始排班</p>
      </div>
      <div v-else class="schedule-table-wrapper">
        <table class="schedule-table">
          <colgroup>
            <col class="col-name-def">
            <col class="col-role-def">
            <col v-for="d in daysInMonth" :key="'col-'+d.dateStr" class="col-day-def">
          </colgroup>
          <thead>
            <tr class="header-row">
              <th class="sticky-name">姓名</th>
              <th class="sticky-role">职位</th>
              <th v-for="d in daysInMonth" :key="d.dateStr" class="th-day" :class="{ weekend: d.isWeekend, warning: hasMissingShifts(d.dateStr) }">
                <div class="day-num">{{ d.day }}</div>
                <div class="day-week">{{ weekDays[d.weekDay] }}</div>
              </th>
            </tr>
          </thead>
          <tbody>
            <template v-for="(groupEmps, groupName) in groupedEmployees" :key="groupName">
              <tr class="group-row" v-if="Object.keys(groupedEmployees).length > 1">
                <td :colspan="daysInMonth.length + 2" class="group-cell">{{ groupName }}</td>
              </tr>
              <tr v-for="emp in groupEmps" :key="emp.id" class="employee-row" 
                    draggable="true"
                    @dragstart="onDragStart(emp, $event)"
                    @dragend="onDragEnd($event)"
                    @dragover="onDragOver(emp, $event)"
                    @dragleave="onDragLeave($event)"
                    @drop="onDrop(emp, $event)">
                <td class="sticky-name td-name">
                  <div class="emp-info">
                    <div class="drag-handle" title="拖拽排序">
                      <svg viewBox="0 0 24 24" fill="currentColor"><circle cx="9" cy="6" r="1.5"/><circle cx="15" cy="6" r="1.5"/><circle cx="9" cy="12" r="1.5"/><circle cx="15" cy="12" r="1.5"/><circle cx="9" cy="18" r="1.5"/><circle cx="15" cy="18" r="1.5"/></svg>
                    </div>
                    <span class="emp-name">{{ emp.name }}</span>
                    <div class="emp-actions" v-if="canEditEmployee || canDeleteEmployee">
                      <button v-if="canEditEmployee" class="action-btn sm" @click.stop="openEmployeeModal('edit', emp)" title="编辑"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M11 5H6a2 2 0 00-2 2v11a2 2 0 002 2h11a2 2 0 002-2v-5m-1.414-9.414a2 2 0 112.828 2.828L11.828 15H9v-2.828l8.586-8.586z"/></svg></button>
                      <button v-if="canDeleteEmployee" class="action-btn sm danger" @click.stop="deleteEmployee(emp)" title="删除"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M19 7l-.867 12.142A2 2 0 0116.138 21H7.862a2 2 0 01-1.995-1.858L5 7m5 4v6m4-6v6m1-10V4a1 1 0 00-1-1h-4a1 1 0 00-1 1v3M4 7h16"/></svg></button>
                    </div>
                  </div>
                </td>
                <td class="sticky-role td-role">{{ emp.role || '运维工程师' }}</td>
                <td v-for="d in daysInMonth" :key="d.dateStr" class="td-shift" :class="{ weekend: d.isWeekend, clickable: canEditShift }" @click="canEditShift && openShiftPicker(emp, d.dateStr, $event)">
                  <span v-if="emp.shifts?.[d.dateStr]" class="shift-badge" :style="getShiftStyle(emp.shifts[d.dateStr])" :title="getShiftInfo(emp.shifts[d.dateStr])?.name + ' ' + getShiftInfo(emp.shifts[d.dateStr])?.time">
                    {{ emp.shifts[d.dateStr] }}
                  </span>
                </td>
              </tr>
            </template>
            <tr class="stats-row">
              <td class="sticky-name stats-label">统计</td>
              <td class="sticky-role"></td>
              <td v-for="d in daysInMonth" :key="d.dateStr" class="td-stats" :class="{ warning: hasMissingShifts(d.dateStr) }">
                <div class="stats-mini">
                  <span class="stat-a" :class="{ zero: dailyStats[d.dateStr]?.A === 0 }">A:{{ dailyStats[d.dateStr]?.A }}</span>
                  <span class="stat-b" :class="{ zero: dailyStats[d.dateStr]?.B === 0 }">B:{{ dailyStats[d.dateStr]?.B }}</span>
                  <span class="stat-c" :class="{ zero: dailyStats[d.dateStr]?.C === 0 }">C:{{ dailyStats[d.dateStr]?.C }}</span>
                  <span class="stat-d" v-if="dailyStats[d.dateStr]?.D > 0">D:{{ dailyStats[d.dateStr]?.D }}</span>
                </div>
              </td>
            </tr>
          </tbody>
        </table>
      </div>
    </div>

    <!-- 添加/编辑员工弹窗 -->
    <Teleport to="body">
      <div class="modal-overlay" :class="{ active: showEmployeeModal }">
        <div class="modal employee-modal">
          <div class="modal-header">
            <h2>{{ employeeModalMode === 'edit' ? '编辑员工' : '添加员工' }}</h2>
            <button class="modal-close" @click="showEmployeeModal = false"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><line x1="18" y1="6" x2="6" y2="18"/><line x1="6" y1="6" x2="18" y2="18"/></svg></button>
          </div>
          <form class="modal-form" @submit.prevent="saveEmployee">
            <div class="modal-body">
              <div class="form-group"><label>姓名 *</label><input type="text" class="form-input" v-model="employeeForm.name" placeholder="员工姓名" required /></div>
              <div class="form-group"><label>职位</label><input type="text" class="form-input" v-model="employeeForm.role" placeholder="运维工程师" /></div>
            </div>
            <div class="modal-footer">
              <button type="button" class="btn btn-secondary" @click="showEmployeeModal = false">取消</button>
              <button type="submit" class="btn btn-primary">保存</button>
            </div>
          </form>
        </div>
      </div>
    </Teleport>

    <!-- 批量排班弹窗 -->
    <Teleport to="body">
      <div class="modal-overlay" :class="{ active: showBatchModal }">
        <div class="modal batch-modal">
          <div class="modal-header">
            <h2>批量排班</h2>
            <button class="modal-close" @click="showBatchModal = false"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><line x1="18" y1="6" x2="6" y2="18"/><line x1="6" y1="6" x2="18" y2="18"/></svg></button>
          </div>
          <div class="modal-body">
            <div class="batch-section">
              <div class="batch-section-title">
                <span>选择员工</span>
                <button class="btn btn-text btn-sm" @click="selectAllEmployees">{{ selectedEmployees.length === employees.length ? '取消全选' : '全选' }}</button>
              </div>
              <div class="employee-select-grid">
                <label v-for="emp in employees" :key="emp.id" class="emp-checkbox" :class="{ selected: selectedEmployees.some(e => e.id === emp.id) }">
                  <input type="checkbox" :checked="selectedEmployees.some(e => e.id === emp.id)" @change="toggleEmployeeSelection(emp)" />
                  <span class="emp-checkbox-name">{{ emp.name }}</span>
                  <span class="emp-checkbox-role">{{ emp.role || '运维工程师' }}</span>
                </label>
              </div>
            </div>
            <div class="batch-section">
              <div class="batch-section-title">日期范围</div>
              <div class="date-range-row">
                <input type="date" class="form-input" v-model="batchForm.startDate" />
                <span class="date-sep">至</span>
                <input type="date" class="form-input" v-model="batchForm.endDate" />
              </div>
            </div>
            <div class="batch-section">
              <div class="batch-section-title">适用星期</div>
              <div class="weekday-select">
                <label v-for="wd in weekdayOptions" :key="wd.value" class="weekday-chip" :class="{ active: batchForm.weekdays.includes(wd.value) }">
                  <input type="checkbox" :value="wd.value" v-model="batchForm.weekdays" />
                  {{ wd.label }}
                </label>
              </div>
            </div>
            <div class="batch-section">
              <div class="batch-section-title">班次类型</div>
              <div class="shift-select-grid">
                <label v-for="s in shiftTypes" :key="s.code" class="shift-option" :class="{ active: batchForm.shiftCode === s.code }" :style="batchForm.shiftCode === s.code ? { borderColor: s.color, background: s.color + '20' } : {}">
                  <input type="radio" name="batchShift" :value="s.code" v-model="batchForm.shiftCode" />
                  <span class="shift-code" :style="{ background: s.color }">{{ s.code }}</span>
                  <span class="shift-name">{{ s.name }}</span>
                </label>
              </div>
            </div>
          </div>
          <div class="modal-footer">
            <div class="batch-summary" :class="{ warning: selectedEmployees.length === 0 }">
              <svg v-if="selectedEmployees.length === 0" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><circle cx="12" cy="12" r="10"/><line x1="12" y1="8" x2="12" y2="12"/><line x1="12" y1="16" x2="12.01" y2="16"/></svg>
              {{ selectedEmployees.length === 0 ? '请选择员工' : '已选 ' + selectedEmployees.length + ' 人' }}
            </div>
            <button type="button" class="btn btn-secondary" @click="showBatchModal = false">取消</button>
            <button type="button" class="btn btn-primary" @click="applyBatchShift">应用排班</button>
          </div>
        </div>
      </div>
    </Teleport>

    <!-- 班次配置弹窗 -->
    <Teleport to="body">
      <div class="modal-overlay" :class="{ active: showConfigModal }">
        <div class="modal config-modal">
          <div class="modal-header">
            <h2>班次配置</h2>
            <button class="modal-close" @click="showConfigModal = false"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><line x1="18" y1="6" x2="6" y2="18"/><line x1="6" y1="6" x2="18" y2="18"/></svg></button>
          </div>
          <div class="modal-body">
            <div class="config-list">
              <div class="config-header">
                <span class="cfg-col-code">代码</span>
                <span class="cfg-col-name">名称</span>
                <span class="cfg-col-time">时间段</span>
                <span class="cfg-col-color">颜色</span>
                <span class="cfg-col-duty">值班</span>
                <span class="cfg-col-action">操作</span>
              </div>
              <div v-for="(s, idx) in shiftTypes" :key="idx" class="config-item">
                <input type="text" class="cfg-input cfg-input-code" v-model="s.code" placeholder="A" maxlength="4" @input="s.label = s.code" />
                <input type="text" class="cfg-input cfg-input-name" v-model="s.name" placeholder="早班" />
                <input type="text" class="cfg-input cfg-input-time" v-model="s.time" placeholder="09:00-18:00" />
                <div class="cfg-color-wrap">
                  <input type="color" class="cfg-color" v-model="s.color" title="选择颜色" />
                  <span class="cfg-color-preview" :style="{ background: s.color }">{{ s.code }}</span>
                </div>
                <label class="cfg-duty"><input type="checkbox" v-model="s.isDuty" /><span class="duty-text">{{ s.isDuty ? '是' : '否' }}</span></label>
                <button class="cfg-delete-btn" @click="removeShiftType(idx)" title="删除班次"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M19 7l-.867 12.142A2 2 0 0116.138 21H7.862a2 2 0 01-1.995-1.858L5 7m5 4v6m4-6v6m1-10V4a1 1 0 00-1-1h-4a1 1 0 00-1 1v3M4 7h16"/></svg></button>
              </div>
            </div>
            <button type="button" class="btn btn-add-shift" @click="addShiftType">
              <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><line x1="12" y1="5" x2="12" y2="19"/><line x1="5" y1="12" x2="19" y2="12"/></svg>
              添加班次
            </button>
          </div>
          <div class="modal-footer">
            <button type="button" class="btn btn-secondary" @click="showConfigModal = false">取消</button>
            <button type="button" class="btn btn-primary" @click="saveShiftConfig">保存配置</button>
          </div>
        </div>
      </div>
    </Teleport>

    <!-- 班次选择器弹窗 -->
    <Teleport to="body">
      <div v-if="showShiftPicker" class="shift-picker-overlay" @click="closeShiftPicker">
        <div class="shift-picker" :style="{ left: shiftPickerTarget.x + 'px', top: shiftPickerTarget.y + 'px' }" @click.stop>
          <div class="shift-picker-header">选择班次</div>
          <div class="shift-picker-grid">
            <button v-for="s in shiftTypes" :key="s.code" class="shift-picker-item" :class="{ active: shiftPickerTarget.currentShift === s.code }" @click="selectShift(s.code)">
              <span class="sp-code" :style="{ background: s.color }">{{ s.code }}</span>
              <span class="sp-name">{{ s.name }}</span>
            </button>
            <button class="shift-picker-item clear-btn" :class="{ active: !shiftPickerTarget.currentShift }" @click="selectShift('')">
              <span class="sp-code sp-clear">×</span>
              <span class="sp-name">清空</span>
            </button>
          </div>
        </div>
      </div>
    </Teleport>
  </div>
</template>

<style scoped>
.schedule-page { display: flex; flex-direction: column; gap: 16px; }
.page-header { display: flex; justify-content: space-between; align-items: center; flex-wrap: wrap; gap: 12px; }
.page-header h2 { font-size: 20px; font-weight: 600; margin: 0; }
.header-actions { display: flex; gap: 10px; }

.btn { display: inline-flex; align-items: center; gap: 6px; padding: 9px 16px; border-radius: 8px; border: none; cursor: pointer; font-size: 13px; font-weight: 500; transition: all 0.2s; }
.btn svg { width: 15px; height: 15px; }
.btn-primary { background: var(--primary); color: white; }
.btn-primary:hover { background: #2563eb; }
.btn-secondary { background: var(--bg-hover); color: var(--text-primary); border: 1px solid var(--border-color); }
.btn-secondary:hover { border-color: var(--primary); }
.btn-danger-outline { border-color: #ef4444; color: #ef4444; }
.btn-danger-outline:hover { background: rgba(239, 68, 68, 0.1); border-color: #dc2626; color: #dc2626; }
.btn-text { background: transparent; color: var(--primary); padding: 8px 12px; }

.month-nav { display: flex; align-items: center; gap: 16px; }
.nav-btn { width: 36px; height: 36px; border-radius: 8px; border: 1px solid var(--border-color); background: var(--bg-card); color: var(--text-primary); cursor: pointer; display: flex; align-items: center; justify-content: center; transition: all 0.2s; }
.nav-btn:hover { border-color: var(--primary); color: var(--primary); }
.nav-btn svg { width: 18px; height: 18px; }
.current-month { font-size: 18px; font-weight: 600; min-width: 140px; text-align: center; }

.shift-legend { display: flex; flex-wrap: wrap; gap: 8px; padding: 4px 0; }
.legend-item { display: flex; align-items: center; gap: 5px; padding: 5px 8px; border-radius: 6px; background: var(--bg-hover); border: 1px solid var(--border-color); font-size: 11px; white-space: nowrap; }
.legend-code { padding: 2px 6px; border-radius: 4px; font-weight: 700; color: white; font-size: 10px; min-width: 18px; text-align: center; }
.legend-name { font-weight: 500; color: var(--text-primary); white-space: nowrap; }
.legend-time { color: var(--text-muted); font-size: 10px; white-space: nowrap; }
.legend-duty { font-size: 9px; color: #ef4444; font-weight: 600; background: rgba(239,68,68,0.1); padding: 1px 4px; border-radius: 3px; }
.legend-duty { background: #ef4444; color: white; padding: 1px 6px; border-radius: 4px; font-size: 10px; font-weight: 600; }

.schedule-container { flex: 1; background: var(--bg-card); border: 1px solid var(--border-color); border-radius: 12px; overflow: hidden; }
.loading-state, .empty-state { display: flex; flex-direction: column; align-items: center; justify-content: center; height: 300px; color: var(--text-muted); }
.empty-state svg { width: 48px; height: 48px; margin-bottom: 12px; opacity: 0.5; }
.empty-state p { font-size: 13px; margin-top: 6px; }

.schedule-table-wrapper { overflow: auto; max-height: calc(100vh - 320px); position: relative; }
.schedule-table { border-collapse: separate; border-spacing: 0; table-layout: fixed; width: max-content; }

/* 用 colgroup 统一列宽 - 必须与sticky left值一致 */
.col-name-def { width: 120px; min-width: 120px; max-width: 120px; }
.col-role-def { width: 80px; min-width: 80px; max-width: 80px; }
.col-day-def { width: 38px; min-width: 38px; max-width: 38px; }

.schedule-table th, .schedule-table td { border: 1px solid var(--border-color); border-left: none; text-align: center; vertical-align: middle; box-sizing: border-box; white-space: nowrap; }
.schedule-table th:first-child, .schedule-table td:first-child { border-left: 1px solid var(--border-color); }
.schedule-table thead { position: sticky; top: 0; z-index: 20; }
.header-row th { background: var(--bg-hover); font-weight: 600; font-size: 12px; height: 50px; }

/* Sticky 姓名列 - width必须与col-name-def一致 */
.sticky-name { position: sticky; left: 0; z-index: 15; background: var(--bg-hover); padding: 4px 6px !important; text-align: left; width: 120px; min-width: 120px; max-width: 120px; box-sizing: border-box; }
.header-row .sticky-name { z-index: 25; }
.td-name { background: var(--bg-card); }
.stats-row .sticky-name { background: linear-gradient(180deg, rgba(59,130,246,0.12), rgba(59,130,246,0.05)); }

/* Sticky 职位列 - left必须等于姓名列宽度120px */
.sticky-role { position: sticky; left: 120px; z-index: 15; background: var(--bg-hover); padding: 4px 6px !important; box-shadow: 2px 0 4px rgba(0,0,0,0.08); white-space: nowrap; width: 80px; min-width: 80px; max-width: 80px; box-sizing: border-box; }
.header-row .sticky-role { z-index: 25; }
.td-role { background: var(--bg-card); font-size: 11px; color: var(--text-secondary); white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
.stats-row .sticky-role { background: linear-gradient(180deg, rgba(59,130,246,0.12), rgba(59,130,246,0.05)); }

/* 日期表头 */
.th-day { padding: 4px 2px !important; width: 38px; min-width: 38px; max-width: 38px; box-sizing: border-box; }
.th-day.weekend { background: rgba(239, 68, 68, 0.15); }
.th-day.warning { background: rgba(251, 191, 36, 0.25); }
.day-num { font-size: 12px; font-weight: 600; line-height: 1.3; }
.day-week { font-size: 9px; color: var(--text-muted); line-height: 1.2; }

.employee-row td { background: var(--bg-card); }
.employee-row:hover td { background: var(--bg-hover); }
.employee-row:hover .sticky-name, .employee-row:hover .sticky-group { background: var(--bg-hover); }

.group-row .group-cell { background: var(--bg-hover); font-weight: 600; font-size: 13px; padding: 8px 12px !important; text-align: left; color: var(--text-secondary); }

.employee-row:hover td { background: rgba(59, 130, 246, 0.05); }
.employee-row:hover .sticky-col, .employee-row:hover .sticky-col-2 { background: rgba(59, 130, 246, 0.05); }
.emp-info { display: flex; align-items: center; gap: 4px; width: 100%; }
.drag-handle { width: 14px; height: 20px; display: flex; align-items: center; justify-content: center; cursor: grab; color: var(--text-muted); opacity: 0.2; transition: all 0.15s; flex-shrink: 0; }
.drag-handle:hover { opacity: 1; color: var(--primary); }
.drag-handle:active { cursor: grabbing; }
.drag-handle svg { width: 12px; height: 12px; }
.employee-row:hover .drag-handle { opacity: 0.5; }
.employee-row.dragging { opacity: 0.5; background: rgba(59,130,246,0.1) !important; }
.employee-row.dragging td { background: rgba(59,130,246,0.1) !important; }
.employee-row.drag-over { background: rgba(59,130,246,0.15) !important; }
.employee-row.drag-over td { background: rgba(59,130,246,0.15) !important; border-top: 2px solid var(--primary) !important; }
.emp-name { font-size: 12px; font-weight: 500; flex: 1; min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.emp-actions { display: flex; gap: 2px; opacity: 0.2; transition: opacity 0.2s; flex-shrink: 0; }
.employee-row:hover .emp-actions { opacity: 1; }
.action-btn.sm { width: 18px; height: 18px; border-radius: 3px; border: none; background: rgba(59,130,246,0.1); color: var(--primary); cursor: pointer; display: flex; align-items: center; justify-content: center; transition: all 0.15s; }
.action-btn.sm:hover { background: var(--primary); color: #fff; }
.action-btn.sm.danger { background: rgba(239,68,68,0.1); color: #ef4444; }
.action-btn.sm.danger:hover { background: #ef4444; color: #fff; }
.action-btn.sm svg { width: 10px; height: 10px; }

.td-shift { height: 40px; cursor: pointer; transition: background 0.15s; padding: 4px 2px !important; }
.td-shift:hover { background: rgba(59, 130, 246, 0.15) !important; }
.td-shift.weekend { background: rgba(239, 68, 68, 0.06); }

.shift-badge { display: inline-flex; align-items: center; justify-content: center; width: 32px; height: 24px; border-radius: 4px; font-size: 11px; font-weight: 700; }

.stats-row td { background: linear-gradient(180deg, rgba(59,130,246,0.12), rgba(59,130,246,0.05)); border-top: 2px solid var(--primary) !important; }
.stats-label { font-size: 12px; font-weight: 600; text-align: center !important; color: var(--primary); }
.td-stats { font-size: 9px; padding: 4px 2px !important; }
.td-stats.warning { background: rgba(251, 191, 36, 0.15); }
.stats-mini { display: flex; flex-direction: column; gap: 1px; line-height: 1.15; }
.stat-a { color: #3a84ff; font-weight: 500; }
.stat-b { color: #ff9c01; font-weight: 500; }
.stat-c { color: #8b5cf6; font-weight: 500; }
.stat-d { color: #ea3636; font-weight: 600; }
.stats-mini .zero { color: rgba(239, 68, 68, 0.5); font-weight: 400; }

/* 班次选择器 */
.shift-picker-overlay { position: fixed; top: 0; left: 0; right: 0; bottom: 0; z-index: 9999; }
.shift-picker { position: fixed; background: var(--bg-card); border-radius: 10px; box-shadow: 0 8px 32px rgba(0,0,0,0.2); border: 1px solid var(--border-color); min-width: 200px; max-width: 320px; animation: pickerIn 0.15s ease-out; z-index: 10000; }
@keyframes pickerIn { from { opacity: 0; transform: translateY(-8px); } to { opacity: 1; transform: translateY(0); } }
.shift-picker-header { padding: 10px 14px; font-size: 13px; font-weight: 600; color: var(--text-secondary); border-bottom: 1px solid var(--border-color); }
.shift-picker-grid { display: grid; grid-template-columns: repeat(3, 1fr); gap: 6px; padding: 10px; }
.shift-picker-item { display: flex; flex-direction: column; align-items: center; gap: 4px; padding: 10px 6px; border-radius: 8px; border: 1px solid var(--border-color); background: var(--bg-primary); cursor: pointer; transition: all 0.15s; }
.shift-picker-item:hover { border-color: var(--primary); background: rgba(59,130,246,0.08); }
.shift-picker-item.active { border-color: var(--primary); background: rgba(59,130,246,0.12); box-shadow: 0 0 0 2px rgba(59,130,246,0.2); }
.sp-code { display: inline-flex; align-items: center; justify-content: center; width: 28px; height: 22px; border-radius: 4px; font-size: 12px; font-weight: 700; color: white; }
.sp-clear { background: var(--text-muted); }
.sp-name { font-size: 11px; color: var(--text-secondary); white-space: nowrap; }
.shift-picker-item.clear-btn .sp-code { background: #9ca3af; }
</style>

<style>
/* 弹窗基础样式由全局 base.css 控制 display 属性 */
.modal-overlay.active {
  display: flex !important;
}
.modal {
  background: var(--bg-card);
  border-radius: 16px;
  border: 1px solid var(--border-color);
  display: flex;
  flex-direction: column;
  box-shadow: 0 25px 60px rgba(0, 0, 0, 0.4);
  overflow: hidden;
  animation: modalSlideIn 0.2s ease-out;
}
@keyframes modalSlideIn {
  from { opacity: 0; transform: translateY(-20px) scale(0.96); }
  to { opacity: 1; transform: translateY(0) scale(1); }
}
.modal-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 20px 24px;
  border-bottom: 1px solid var(--border-color);
  flex-shrink: 0;
  background: linear-gradient(180deg, rgba(59,130,246,0.08), transparent);
}
.modal-header h2 {
  font-size: 18px;
  font-weight: 600;
  margin: 0;
}
.modal-close {
  width: 34px;
  height: 34px;
  border-radius: 8px;
  border: none;
  background: var(--bg-hover);
  color: var(--text-secondary);
  cursor: pointer;
  display: flex;
  align-items: center;
  justify-content: center;
  transition: all 0.2s;
}
.modal-close:hover {
  background: #ea3636;
  color: white;
}
.modal-close svg {
  width: 18px;
  height: 18px;
}
.modal-form {
  display: flex;
  flex-direction: column;
  flex: 1;
  overflow: hidden;
}
.modal-body {
  padding: 24px;
  overflow-y: auto;
  flex: 1;
}
.modal-footer {
  display: flex;
  justify-content: flex-end;
  align-items: center;
  gap: 12px;
  padding: 16px 24px;
  border-top: 1px solid var(--border-color);
  flex-shrink: 0;
  background: var(--bg-hover);
}
.modal-footer .btn {
  padding: 11px 24px;
  font-size: 14px;
}

/* 表单样式 */
.form-group {
  display: flex;
  flex-direction: column;
  gap: 8px;
  margin-bottom: 18px;
}
.form-group label {
  font-size: 13px;
  font-weight: 500;
  color: var(--text-secondary);
}
.form-input {
  padding: 12px 16px;
  border: 1px solid var(--border-color);
  border-radius: 10px;
  background: var(--bg-input);
  color: var(--text-primary);
  font-size: 14px;
  width: 100%;
  box-sizing: border-box;
  transition: border-color 0.2s, box-shadow 0.2s;
}
.form-input:focus {
  border-color: var(--primary);
  outline: none;
  box-shadow: 0 0 0 3px rgba(59,130,246,0.2);
}
.form-input::placeholder {
  color: var(--text-muted);
}

/* 员工弹窗 */
.employee-modal {
  width: 440px;
}

/* 批量排班弹窗 */
.batch-modal {
  width: 640px;
  max-height: 90vh;
}
.batch-section {
  margin-bottom: 20px;
}
.batch-section-title {
  display: flex;
  align-items: center;
  justify-content: space-between;
  font-size: 13px;
  font-weight: 600;
  color: var(--text-secondary);
  margin-bottom: 12px;
  padding-bottom: 8px;
  border-bottom: 1px solid var(--border-color);
}
.btn-sm { padding: 4px 10px !important; font-size: 12px !important; }
.employee-select-grid {
  display: grid;
  grid-template-columns: repeat(3, 1fr);
  gap: 8px;
  max-height: 180px;
  overflow-y: auto;
  padding: 4px;
}
.emp-checkbox {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 10px 12px;
  border: 1px solid var(--border-color);
  border-radius: 8px;
  cursor: pointer;
  transition: all 0.15s;
  background: var(--bg-card);
}
.emp-checkbox:hover {
  border-color: var(--primary);
  background: rgba(59,130,246,0.05);
}
.emp-checkbox.selected {
  border-color: var(--primary);
  background: rgba(59,130,246,0.1);
}
.emp-checkbox input { width: 16px; height: 16px; accent-color: var(--primary); cursor: pointer; }
.emp-checkbox-name { font-size: 13px; font-weight: 500; color: var(--text-primary); }
.emp-checkbox-role { font-size: 11px; color: var(--text-muted); margin-left: auto; }
.date-range-row {
  display: flex;
  align-items: center;
  gap: 12px;
}
.date-range-row .form-input { flex: 1; }
.date-sep { color: var(--text-muted); font-size: 13px; }
.weekday-select {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
}
.weekday-chip {
  display: flex;
  align-items: center;
  gap: 6px;
  padding: 8px 14px;
  border: 1px solid var(--border-color);
  border-radius: 20px;
  cursor: pointer;
  font-size: 13px;
  color: var(--text-secondary);
  transition: all 0.15s;
  background: var(--bg-card);
}
.weekday-chip:hover { border-color: var(--primary); }
.weekday-chip.active {
  border-color: var(--primary);
  background: var(--primary);
  color: white;
}
.weekday-chip input { display: none; }
.shift-select-grid {
  display: grid;
  grid-template-columns: repeat(4, 1fr);
  gap: 10px;
}
.shift-option {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 10px 12px;
  border: 2px solid var(--border-color);
  border-radius: 10px;
  cursor: pointer;
  transition: all 0.15s;
  background: var(--bg-card);
}
.shift-option:hover { border-color: var(--primary); }
.shift-option.active { border-width: 2px; }
.shift-option input { display: none; }
.shift-code {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 28px;
  height: 22px;
  border-radius: 4px;
  font-size: 11px;
  font-weight: 700;
  color: white;
}
.shift-name { font-size: 12px; color: var(--text-primary); }
.batch-summary {
  display: flex;
  align-items: center;
  gap: 6px;
  margin-right: auto;
  font-size: 13px;
  font-weight: 500;
  color: var(--text-secondary);
  padding: 6px 12px;
  border-radius: 6px;
  background: var(--bg-hover);
}
.batch-summary svg { width: 16px; height: 16px; }
.batch-summary.warning {
  background: rgba(239, 68, 68, 0.15);
  color: #ef4444;
  border: 1px solid rgba(239, 68, 68, 0.3);
}

/* 班次配置弹窗 */
.config-modal {
  width: 820px;
  max-height: 85vh;
}
.config-list {
  display: flex;
  flex-direction: column;
  gap: 6px;
  max-height: 400px;
  overflow-y: auto;
}
.config-header {
  display: grid;
  grid-template-columns: 60px 80px 1fr 100px 60px 40px;
  gap: 10px;
  padding: 8px 10px;
  background: var(--bg-hover);
  border-radius: 8px;
  margin-bottom: 8px;
  position: sticky;
  top: 0;
  z-index: 5;
}
.config-header span {
  font-size: 11px;
  font-weight: 600;
  color: var(--text-muted);
  text-transform: uppercase;
  letter-spacing: 0.5px;
}
.cfg-col-code { }
.cfg-col-name { }
.cfg-col-time { }
.cfg-col-color { text-align: center; }
.cfg-col-duty { text-align: center; }
.cfg-col-action { text-align: center; }
.config-item {
  display: grid;
  grid-template-columns: 60px 80px 1fr 100px 60px 40px;
  gap: 10px;
  align-items: center;
  padding: 8px 10px;
  border-radius: 8px;
  transition: background 0.15s;
  border: 1px solid transparent;
}
.config-item:hover {
  background: rgba(59,130,246,0.06);
  border-color: rgba(59,130,246,0.15);
}
.cfg-input {
  padding: 8px 10px;
  border: 1px solid var(--border-color);
  border-radius: 6px;
  background: var(--bg-input);
  color: var(--text-primary);
  font-size: 13px;
  width: 100%;
  box-sizing: border-box;
  transition: border-color 0.2s;
}
.cfg-input:focus {
  border-color: var(--primary);
  outline: none;
}
.cfg-input-code { font-weight: 600; text-align: center; }
.cfg-input-name { }
.cfg-input-time { }
.cfg-color-wrap { display: flex; align-items: center; gap: 6px; }
.cfg-color {
  width: 32px;
  height: 32px;
  border: 2px solid var(--border-color);
  border-radius: 6px;
  cursor: pointer;
  padding: 0;
  flex-shrink: 0;
  transition: border-color 0.2s;
}
.cfg-color:hover {
  border-color: var(--primary);
}
.cfg-color::-webkit-color-swatch-wrapper {
  padding: 2px;
}
.cfg-color::-webkit-color-swatch {
  border-radius: 4px;
  border: none;
}
.cfg-color-preview {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  min-width: 28px;
  height: 22px;
  padding: 0 6px;
  border-radius: 4px;
  font-size: 11px;
  font-weight: 700;
  color: white;
}
.cfg-duty {
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 4px;
  cursor: pointer;
}
.cfg-duty input {
  width: 18px;
  height: 18px;
  accent-color: var(--primary);
  cursor: pointer;
}
.duty-text { font-size: 11px; color: var(--text-muted); }
.cfg-delete-btn {
  width: 28px;
  height: 28px;
  border: none;
  background: rgba(239,68,68,0.1);
  color: #ef4444;
  border-radius: 6px;
  cursor: pointer;
  display: flex;
  align-items: center;
  justify-content: center;
  transition: all 0.15s;
}
.cfg-delete-btn:hover {
  background: #ef4444;
  color: white;
}
.cfg-delete-btn svg { width: 14px; height: 14px; }
.btn-add-shift {
  margin-top: 12px;
  width: 100%;
  padding: 10px;
  border: 2px dashed var(--border-color);
  background: transparent;
  color: var(--text-muted);
  border-radius: 8px;
  cursor: pointer;
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 6px;
  font-size: 13px;
  transition: all 0.2s;
}
.btn-add-shift:hover {
  border-color: var(--primary);
  color: var(--primary);
  background: rgba(59,130,246,0.05);
}
.btn-add-shift svg { width: 16px; height: 16px; }
</style>
