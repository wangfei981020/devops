<script setup>
import { ref, computed, onMounted, onUnmounted, watch } from 'vue'
import { useAppStore, useAuthStore } from '@/stores'
import api from '@/api'
import { saveAs } from 'file-saver'
import ScheduleAnalyticsView from './ScheduleAnalyticsView.vue'
import {
  DEFAULT_TIMEZONE, resolveTimezoneAt, parseTimeRange, dateStrToUtc,
  formatDateInTz, formatTimeInTz, tzOffsetLabel, tzOffsetMinutes, formatOffset,
  browserTimezone, timezoneOptions
} from '@/utils/timezone'

const appStore = useAppStore()
const authStore = useAuthStore()

// 权限检查
const canAddEmployee = computed(() => authStore.hasPermission('schedule:add_employee'))
const canEditEmployee = computed(() => authStore.hasPermission('schedule:edit_employee'))
const canDeleteEmployee = computed(() => authStore.hasPermission('schedule:delete_employee'))
const canBatchSchedule = computed(() => authStore.hasPermission('schedule:batch'))
const canConfigShift = computed(() => authStore.hasPermission('schedule:config'))
// v571: 统计分析 tab 权限（super_admin 自动通过；其它角色需要管理员授予 menu:schedule_analytics）
const canViewAnalytics = computed(() => authStore.hasPermission('menu:schedule_analytics'))
const canExport = computed(() => authStore.hasPermission('schedule:export'))
const canReset = computed(() => authStore.hasPermission('schedule:reset'))
const canEditShift = computed(() => authStore.hasPermission('schedule:edit_shift'))
// v772: 设置员工时区（影响跨时区视图与覆盖空档判定）
const canEditTimezone = computed(() => authStore.hasPermission('schedule:edit_timezone'))

const employees = ref([])
const loading = ref(false)
const currentYear = ref(new Date().getFullYear())
const currentMonth = ref(new Date().getMonth() + 1)

// 标签页切换 - v570: 加 stats 统计分析 tab
const activeTab = ref('schedule') // 'schedule' | 'contacts' | 'stats'

// 联系人管理
const contacts = ref([])
const contactsLoading = ref(false)
const showContactModal = ref(false)
const contactModalMode = ref('add')
const contactForm = ref({ name: '', phone: '', department: '', position: '', remark: '' })
const editingContactId = ref(null)
const contactSearchQuery = ref('')

const showEmployeeModal = ref(false)
const employeeForm = ref({ name: '', group_name: '', role: '运维工程师', role_en: '' })
const employeeModalMode = ref('add')
const editingEmployeeId = ref(null)

const showConfigModal = ref(false)
const showExportModal = ref(false)
const exportForm = ref({ mode: 'current', start: '', end: '' })
const exporting = ref(false)
const showBatchModal = ref(false)
const batchForm = ref({ employees: [], startDate: '', endDate: '', shiftCode: '', weekdays: [1, 2, 3, 4, 5] })
const selectedEmployees = ref([])

// 拖拽排序相关
const draggingEmployee = ref(null)
const dragOverEmployee = ref(null)

// 班次选择器
const showShiftPicker = ref(false)
const shiftPickerTarget = ref({ empId: null, dateStr: '', x: 0, y: 0 })

// ===== v772 跨时区排班 =====
// viewTz = 'local' 表示「各自本地」，即每个格子按员工本人当地日历排（默认，也是唯一可编辑的视角）；
// 其余取值是 IANA 时区名，此时格子按「班次开始时刻落在该时区的哪一天」重新归列。
const viewTz = ref('local')
const customTz = ref('')
const browserTz = browserTimezone()
const showCoverage = ref(true)
const coverageDetailDate = ref('')
// 空档汇总默认收起——它有好几屏高，展开着会把单日拆解挤到看不见的地方
const gapSummaryOpen = ref(false)
// 员工时区弹窗
const showTimezoneModal = ref(false)
const timezoneEmployee = ref(null)
const timezoneForm = ref({ timezone: 'Asia/Shanghai', effective_from: '' })
const tzOptionList = timezoneOptions()

// v774: 班次时间段的「按组 + 时区」覆盖。
// ig 和 sl 不是一家公司，班次口径不同：A/B 一致，C 不同——
// sl 的一天从 09:00 开始（A→B→C，C 跨到次日凌晨），
// ig 的一天从 00:00 开始（C→A→B，当天走完）。
const shiftOverrides = ref([])

// 解析某员工在某天的某个班次到底是什么时间段。
// ⚠️ 时区按日期解析，所以「改时区之前按老口径、之后按新口径」自动成立，
// 历史排班不需要回溯修改——这也是覆盖表 key 里必须带时区的原因。
function resolveShiftDef(emp, dateStr, code) {
  const base = shiftTypes.value.find(s => s.code === code)
  if (!base) return null
  const tz = resolveTimezoneAt(emp.timezones, dateStr)
  const group = emp.group_name || '未分组'
  const ov = shiftOverrides.value.find(o => o.group_name === group && o.timezone === tz && o.code === code)
  if (!ov) return { ...base, tz, overridden: false }
  return {
    ...base,
    time: ov.time_range || base.time,
    name: ov.name || base.name,
    tz,
    overridden: true
  }
}

// 某个 (组, 时区) 组合下的完整班次表，供归因、补法、完整性检查复用
function shiftDefsFor(group, tz) {
  return shiftTypes.value.map(base => {
    const ov = shiftOverrides.value.find(o => o.group_name === group && o.timezone === tz && o.code === base.code)
    return ov
      ? { ...base, time: ov.time_range || base.time, name: ov.name || base.name, overridden: true }
      : { ...base, overridden: false }
  })
}

// 实际生效的查看时区；'local' 时为空，表示不做重排
const effectiveViewTz = computed(() => (viewTz.value === 'local' ? '' : viewTz.value))
// 对照/覆盖基准时区：跨时区视角下就是该时区，本地视角下用北京时间当基准
const compareTz = computed(() => effectiveViewTz.value || DEFAULT_TIMEZONE)
// 跨时区视角下表格只读——在「北京 14 号」那一格点一下，系统无法判断你要改的是
// 员工本地的 13 号还是 14 号，写回去必然有一半是错的。要改班必须切回「各自本地」。
const gridEditable = computed(() => canEditShift.value && !effectiveViewTz.value)

function tzShortLabel(tz) {
  if (!tz) return ''
  return tz.split('/').pop().replace(/_/g, ' ')
}

function monthFirstDay() {
  return `${currentYear.value}-${String(currentMonth.value).padStart(2, '0')}-01`
}
function monthLastDay() {
  const last = new Date(currentYear.value, currentMonth.value, 0).getDate()
  return `${currentYear.value}-${String(currentMonth.value).padStart(2, '0')}-${String(last).padStart(2, '0')}`
}

// 员工在当前显示月份的时区跨度。
// ⚠️ 不能只取月初那天：月中改了时区的话，徽标不会变，看着像「保存失败」——实际存进去了。
// 判据用 UTC 偏移而不是时区名，这样 10 月底的冬夏令时切换（时区名没变但偏移变了）也能显示出来。
function employeeTzSpan(emp) {
  const first = monthFirstDay()
  const last = monthLastDay()
  const tzA = resolveTimezoneAt(emp.timezones, first)
  const tzB = resolveTimezoneAt(emp.timezones, last)
  const offA = tzOffsetMinutes(tzA, new Date(`${first}T12:00:00Z`))
  const offB = tzOffsetMinutes(tzB, new Date(`${last}T12:00:00Z`))
  return { first, last, tzA, tzB, offA, offB, changed: offA !== offB, tzChanged: tzA !== tzB }
}

// 兼容旧调用：需要单个时区时用月末那天的，改完时区能立刻反映出来
function employeeTzForMonth(emp) {
  return resolveTimezoneAt(emp.timezones, monthLastDay())
}

// 姓名列上那个小徽标。姓名列只有 120px，始终只显示一个值——当月最后生效的时区，
// 改完时区能立刻看到新值。当月内变过时区（或跨了冬夏令时）时靠徽标颜色提示，
// 完整的变更过程放在悬浮说明里，不占列宽。
function employeeTzBadge(emp) {
  return formatOffset(employeeTzSpan(emp).offB)
}

function employeeTzTitle(emp) {
  const s = employeeTzSpan(emp)
  const lines = []
  if (s.changed) {
    lines.push(s.tzChanged
      ? `本月内换过时区：${s.tzA}（${formatOffset(s.offA)}）→ ${s.tzB}（${formatOffset(s.offB)}）`
      : `本月内跨冬夏令时切换：${s.tzA} ${formatOffset(s.offA)} → ${formatOffset(s.offB)}`)
  } else {
    lines.push(`时区：${s.tzA}（${formatOffset(s.offA)}）`)
  }
  const hist = emp.timezones || []
  if (hist.length > 1) {
    lines.push('时区历史：')
    hist.forEach(h => lines.push(`  ${h.effective_from} 起 ${h.timezone}`))
  }
  const sample = shiftTypes.value.find(s2 => parseTimeRange(s2.time))
  if (sample && s.tzB !== compareTz.value) {
    const r = parseTimeRange(sample.time)
    const start = dateStrToUtc(s.last, r.startMin, s.tzB)
    const end = new Date(start.getTime() + r.durationMin * 60000)
    lines.push(`例：${sample.name} ${sample.time} = ${tzShortLabel(compareTz.value)} ${formatTimeInTz(start, compareTz.value)}-${formatTimeInTz(end, compareTz.value)}`)
  }
  if (canEditTimezone.value) lines.push('点击设置时区')
  return lines.join('\n')
}

// 把每个员工的每条排班展开成绝对时间区间。
// ⚠️ 结束时刻 = 开始时刻 + 时长（绝对相加），不是把 09:00 和 18:00 分别换算——
// 跨夏令时切换点的班次分别换算会让时长凭空多/少 1 小时。
const shiftEntries = computed(() => {
  const out = {}
  employees.value.forEach(emp => {
    const list = []
    const shifts = emp.shifts || {}
    Object.keys(shifts).forEach(srcDate => {
      const code = shifts[srcDate]
      if (!code) return
      // v774: 时间段按 (组 + 该日时区) 解析，不同公司的同一个代码可能不是一回事
      const cfg = resolveShiftDef(emp, srcDate, code)
      const tz = resolveTimezoneAt(emp.timezones, srcDate)
      const range = cfg ? parseTimeRange(cfg.time) : null
      if (range) {
        const startUtc = dateStrToUtc(srcDate, range.startMin, tz)
        const endUtc = new Date(startUtc.getTime() + range.durationMin * 60000)
        list.push({ empId: emp.id, empName: emp.name, code, cfg, srcDate, tz, startUtc, endUtc, hasInterval: true, isWorking: !!cfg.isWorking, overridden: !!cfg.overridden })
      } else {
        // 休假类（OD/OFF/H/PL/SL/AL/CT）没有时间段，不参与换算，也不算在岗
        list.push({ empId: emp.id, empName: emp.name, code, cfg, srcDate, tz, startUtc: null, endUtc: null, hasInterval: false, isWorking: false, overridden: false })
      }
    })
    out[emp.id] = list
  })
  return out
})

// 表格实际渲染用的格子内容：员工 -> 日期 -> 该格的班次列表。
// 跨时区视角下按「开始时刻落在目标时区的哪一天」归列，所以一格可能有两个班次、
// 也可能出现空格——这不是漏排，是同一段绝对时间在另一个时区里换了天。
const displayGrid = computed(() => {
  const tz = effectiveViewTz.value
  const grid = {}
  employees.value.forEach(emp => {
    const cells = {}
    ;(shiftEntries.value[emp.id] || []).forEach(e => {
      // 没有时间段的休假类无法重排，留在本人当地日期上
      const key = (tz && e.hasInterval) ? formatDateInTz(e.startUtc, tz) : e.srcDate
      if (!cells[key]) cells[key] = []
      cells[key].push(e)
    })
    Object.values(cells).forEach(arr => arr.sort((a, b) => (a.startUtc ? a.startUtc.getTime() : 0) - (b.startUtc ? b.startUtc.getTime() : 0)))
    grid[emp.id] = cells
  })
  return grid
})

function cellEntries(emp, dateStr) {
  return displayGrid.value[emp.id]?.[dateStr] || []
}

// 格子悬浮说明：本人本地时间 + 换算到对照时区的时间，跨天的标 +1
function entryTitle(entry) {
  const name = entry.cfg?.name || entry.code
  if (!entry.hasInterval) {
    return `${name} ${entry.code}\n无时间段，按本人当地日期 ${entry.srcDate} 显示`
  }
  const lines = [`${name} ${entry.code} · ${entry.cfg.time}`]
  // ⚠️ 起始日期必须由 startUtc 换算，不能直接用 srcDate：
  // 「24:00-09:00」的晚班排在 8/4，实际是从 8/5 00:00 开始的，
  // 拿 srcDate 配换算出来的时间会显示成「8/4 00:00」，日期和时间对不上。
  const localStart = formatDateInTz(entry.startUtc, entry.tz)
  lines.push(`${tzShortLabel(entry.tz)}（${entry.tz}）：${localStart} ${formatTimeInTz(entry.startUtc, entry.tz)} → ${dayHint(entry.endUtc, entry.tz, localStart)}`)
  if (entry.tz !== compareTz.value) {
    const startDate = formatDateInTz(entry.startUtc, compareTz.value)
    lines.push(`${tzShortLabel(compareTz.value)}（${compareTz.value}）：${startDate} ${formatTimeInTz(entry.startUtc, compareTz.value)} → ${dayHint(entry.endUtc, compareTz.value, startDate)}`)
  }
  if (entry.srcDate !== localStart) {
    lines.push(`排班表上这条排在本人当地 ${entry.srcDate}（24:00 起的班从次日开始）`)
  }
  if (effectiveViewTz.value && entry.srcDate !== formatDateInTz(entry.startUtc, effectiveViewTz.value)) {
    lines.push(`⚠️ 按开始时刻，这条在当前视角下归到了 ${formatDateInTz(entry.startUtc, effectiveViewTz.value)}`)
  }
  return lines.join('\n')
}

// 结束时刻，跨天时带 +1 提示
function dayHint(endUtc, tz, startDateStr) {
  const endDate = formatDateInTz(endUtc, tz)
  const t = formatTimeInTz(endUtc, tz)
  return endDate === startDateStr ? t : `${t}（+1 ${endDate}）`
}

// ===== 覆盖空档检查 =====
// 以对照时区为轴，把所有「在岗」班次展开成绝对时间区间，逐小时数人头。
// ⚠️ 依赖接口的 pad=1 多取前后各一天：1 号凌晨有没有人在岗，取决于上月最后一天的晚班。
const coverageByDay = computed(() => {
  const tz = compareTz.value
  const intervals = []
  employees.value.forEach(emp => {
    ;(shiftEntries.value[emp.id] || []).forEach(e => {
      if (e.hasInterval && e.isWorking) intervals.push([e.startUtc.getTime(), e.endUtc.getTime(), e.empName, e.code])
    })
  })
  const result = {}
  daysInMonth.value.forEach(d => {
    const hours = []
    for (let h = 0; h < 24; h++) {
      const s = dateStrToUtc(d.dateStr, h * 60, tz).getTime()
      const e = dateStrToUtc(d.dateStr, (h + 1) * 60, tz).getTime()
      const who = intervals.filter(([is, ie]) => is < e && ie > s).map(([, , name, code]) => `${name}(${code})`)
      hours.push(who)
    }
    result[d.dateStr] = hours
  })
  return result
})

// 某天的空档小时段，合并成连续区间。返回结构化数据（含绝对时间），
// 供列标红、常驻空档行、双时区对照和归因共用。
function coverageGapRanges(dateStr) {
  const hours = coverageByDay.value[dateStr] || []
  const tz = compareTz.value
  const out = []
  let start = -1
  const push = (h1, h2) => out.push({
    dateStr, h1, h2,
    text: `${String(h1).padStart(2, '0')}:00-${String(h2).padStart(2, '0')}:00`,
    short: `${String(h1).padStart(2, '0')}-${String(h2).padStart(2, '0')}`,
    startUtc: dateStrToUtc(dateStr, h1 * 60, tz).getTime(),
    endUtc: dateStrToUtc(dateStr, h2 * 60, tz).getTime(),
    fullDay: h1 === 0 && h2 === 24
  })
  for (let h = 0; h < 24; h++) {
    const empty = hours[h] && hours[h].length === 0
    if (empty && start < 0) start = h
    if (!empty && start >= 0) { push(start, h); start = -1 }
  }
  if (start >= 0) push(start, 24)
  return out
}

function coverageGaps(dateStr) {
  return coverageGapRanges(dateStr).map(g => g.text)
}

function hasGap(dateStr) {
  return (coverageByDay.value[dateStr] || []).some(who => who.length === 0)
}

// 空档段在某个时区的本地时间表示。跨天时带日期，因为同一段空档
// 在另一个时区里可能横跨两天——这正是「切换视角去看」不好用的原因。
function gapInZone(gap, tz) {
  const s = new Date(gap.startUtc)
  const e = new Date(gap.endUtc)
  const sd = formatDateInTz(s, tz)
  const ed = formatDateInTz(e, tz)
  const t = `${formatTimeInTz(s, tz)} – ${formatTimeInTz(e, tz)}`
  if (sd === gap.dateStr && (ed === gap.dateStr || ed === shiftDate(gap.dateStr, 1))) return { time: t, note: '当天' }
  if (sd === shiftDate(gap.dateStr, -1)) return { time: t, note: `前一天 ${md(sd)} 起` }
  return { time: t, note: md(sd) }
}

// 空档段在某时区的简写钟点，如 18-03。日历列宽只有 38px，只能放缩写；
// 完整时间（含跨天日期）在悬浮说明和下方汇总里。
function gapShortInZone(gap, tz) {
  const s = formatTimeInTz(new Date(gap.startUtc), tz).slice(0, 2)
  const e = formatTimeInTz(new Date(gap.endUtc), tz).slice(0, 2)
  return `${s}-${e}`
}

function shiftDate(dateStr, delta) {
  const [y, m, d] = dateStr.split('-').map(Number)
  const dt = new Date(Date.UTC(y, m - 1, d + delta))
  return `${dt.getUTCFullYear()}-${String(dt.getUTCMonth() + 1).padStart(2, '0')}-${String(dt.getUTCDate()).padStart(2, '0')}`
}
function md(dateStr) {
  const [, m, d] = dateStr.split('-')
  return `${Number(m)}/${Number(d)}`
}

// 当月实际用到的时区集合，双时区对照和归因都基于它，不写死任何时区
const activeTimezones = computed(() => {
  const set = new Set()
  employees.value.forEach(emp => {
    daysInMonth.value.forEach(d => set.add(resolveTimezoneAt(emp.timezones, d.dateStr)))
  })
  if (set.size === 0) set.add(DEFAULT_TIMEZONE)
  return [...set].sort()
})

// 空档归因：这段时间「本该由哪天的哪个班次覆盖」。
// ⚠️ 这是确定性计算不是猜测：把每个在岗班次放到候选日期上算出绝对区间，
// 看它是否压住这段空档。关键在候选日期包含【前一天】——
// 「24:00-09:00」的晚班是次日 0 点上班，所以某天凌晨的覆盖取决于前一天排了谁，
// 这也是「格子里明明有 C 却报空档」的真正原因。
function gapCauses(gap) {
  const cand = {}
  // v774: 班次时间段按 (组, 时区) 各不相同，所以要遍历真实存在的组合，
  // 不能拿全局班次表套到所有时区上——那会算出「贝尔格莱德的 C 是 24:00-09:00」这种
  // 该组根本不存在的班。
  for (const { group, tz } of groupTzPairs.value) {
    const cfgs = shiftDefsFor(group, tz).filter(s => s.isWorking && parseTimeRange(s.time))
    for (const cfg of cfgs) {
      const r = parseTimeRange(cfg.time)
      for (const d of [shiftDate(gap.dateStr, -1), gap.dateStr]) {
        const s = dateStrToUtc(d, r.startMin, tz).getTime()
        const e = s + r.durationMin * 60000
        const ov = Math.min(e, gap.endUtc) - Math.max(s, gap.startUtc)
        if (ov <= 0) continue
        // key 带上时间段：同一时区下若两个组对同一代码的定义相同就合并成一条，
        // 定义不同（ig 的 C vs sl 的 C）则各算各的，不会互相盖掉
        const key = `${d}|${cfg.code}|${tz}|${cfg.time}`
        if (!cand[key] || cand[key].hours < ov / 3600000) {
          cand[key] = {
            date: d, code: cfg.code, name: cfg.name, time: cfg.time, tz,
            hours: ov / 3600000,
            // 前一天排的班为什么能管到今天，两种情况不能混为一谈：
            // 24:00 起的班（startMin>=1440）是次日 0 点才上班；
            // 15:00-24:00 这种是前一天下午上班、跨夜延续过来的。
            prevDay: d !== gap.dateStr,
            startsNextDay: r.startMin >= 1440,
            // 那天真的有人排这个班吗——排了却仍空档，说明是别的原因。
            // 必须连时间段一起比：同一个 C，ig 的人和 sl 的人不是同一段时间
            who: employees.value
              .filter(emp => emp.shifts?.[d] === cfg.code
                && resolveTimezoneAt(emp.timezones, d) === tz
                && resolveShiftDef(emp, d, cfg.code)?.time === cfg.time)
              .map(emp => emp.name)
          }
        }
      }
    }
  }
  return Object.values(cand).sort((a, b) => b.hours - a.hours).slice(0, 3)
}

// 反过来的映射：某个时区里有哪些组的人。
// ⚠️ 组和时区是多对多的——同一个组可能有人在国外、也有人在国内，
// 所以光看时区名不知道是谁，光看组名不知道几点上班，两个都得标出来。
const timezoneGroups = computed(() => {
  const map = {}
  employees.value.forEach(emp => {
    const g = emp.group_name || '未分组'
    daysInMonth.value.forEach(d => {
      const tz = resolveTimezoneAt(emp.timezones, d.dateStr)
      ;(map[tz] ||= new Set()).add(g)
    })
  })
  return Object.fromEntries(Object.entries(map).map(([tz, set]) => [tz, [...set].sort()]))
})

function tzGroupsLabel(tz) {
  const gs = timezoneGroups.value[tz]
  return gs && gs.length ? gs.join('、') : ''
}

// 各组当月用到的时区。组和时区不是硬绑定的——同一个组里可以有人在别的时区，
// 所以按「组 × 该组实际用到的时区」逐个算，不假设一个组只有一个时区。
const groupTimezones = computed(() => {
  const map = {}
  employees.value.forEach(emp => {
    const g = emp.group_name || '未分组'
    const set = (map[g] ||= new Set())
    daysInMonth.value.forEach(d => set.add(resolveTimezoneAt(emp.timezones, d.dateStr)))
  })
  return Object.keys(map).sort().map(name => ({ name, tzs: [...map[name]].sort() }))
})

// 当月真实存在的 (组, 时区) 组合。班次定义、完整性判定、补法建议都以它为准——
// 拿全局班次表套所有时区会算出该组根本不存在的班。
const groupTzPairs = computed(() =>
  groupTimezones.value.flatMap(g => g.tzs.map(tz => ({ group: g.name, tz })))
)

// 「这段空档交给哪个组补、该排什么班」。
// 不指定具体的人——只算出组 + 日期 + 班次，谁去上由人定。
// 关键在于同一个班次代码落到不同时区是完全不同的绝对时段：
// C 晚班在北京是 24:00-09:00，在贝尔格莱德也是 24:00-09:00，
// 但换算成北京时间后一个是 00:00-09:00、另一个是 06:00-15:00，能补的空档不一样。
function gapFixOptions(gap) {
  const gapHours = (gap.endUtc - gap.startUtc) / 3600000
  return groupTimezones.value.map(group => {
    const seen = {}
    group.tzs.forEach(tz => {
      // v774: 用这个组在这个时区下的真实班次定义
      shiftDefsFor(group.name, tz).filter(s => s.isWorking && parseTimeRange(s.time)).forEach(cfg => {
        const r = parseTimeRange(cfg.time)
        ;[shiftDate(gap.dateStr, -1), gap.dateStr].forEach(d => {
          const s = dateStrToUtc(d, r.startMin, tz).getTime()
          const e = s + r.durationMin * 60000
          const ov = Math.min(e, gap.endUtc) - Math.max(s, gap.startUtc)
          if (ov <= 0) return
          const key = `${tz}|${cfg.code}|${d}`
          const covered = ov / 3600000
          const opt = {
            tz, code: cfg.code, name: cfg.name, time: cfg.time, date: d,
            covered,
            full: covered >= gapHours - 1e-6,
            prevDay: d !== gap.dateStr,
            startsNextDay: r.startMin >= 1440,
            // 换算到基准时区，让人直接看出「排下去实际是几点到几点」
            inBasis: `${formatTimeInTz(new Date(s), compareTz.value)}-${formatTimeInTz(new Date(e), compareTz.value)}`,
            // 这个组这天已经有几个人排了这个班——是 0 才需要加人
            already: employees.value.filter(emp =>
              (emp.group_name || '未分组') === group.name &&
              emp.shifts?.[d] === cfg.code &&
              resolveTimezoneAt(emp.timezones, d) === tz).length
          }
          if (!seen[key] || seen[key].covered < covered) seen[key] = opt
        })
      })
    })
    const opts = Object.values(seen).sort((a, b) => (b.full - a.full) || (b.covered - a.covered))
    const full = opts.filter(o => o.full)
    return {
      group: group.name,
      tzs: group.tzs,
      // 有能一次补齐的就只给这些；没有就给能补最多的两条，并说明还差多少
      options: full.length ? full.slice(0, 2) : opts.slice(0, 2),
      solvable: full.length > 0,
      gapHours
    }
  }).filter(g => g.options.length > 0)
}

// 按「空档时段」聚合成模式：同一段时段往往在多天重复出现（比如每周五凌晨），
// 逐天列 31 条没法看，聚合后一眼就是两三条规律。
const gapPatterns = computed(() => {
  const map = {}
  const firstOfMonth = daysInMonth.value[0]?.dateStr
  daysInMonth.value.forEach(d => {
    coverageGapRanges(d.dateStr).forEach(g => {
      const p = (map[g.text] ||= {
        text: g.text, fullDay: g.fullDay, hours: g.h2 - g.h1, days: [], sample: g
      })
      p.days.push(d.dateStr)
      // 归因样本尽量避开当月 1 号：它的前一天在本月之外，
      // 会算出「上月最后一天无人排班」这种月初边界特例，对不上其余几天的真实原因
      if (p.sample.dateStr === firstOfMonth && d.dateStr !== firstOfMonth) p.sample = g
    })
  })
  return Object.values(map).sort((a, b) => b.hours - a.hours || b.days.length - a.days.length)
})

// 全月每小时人手分布：找出长期单人无备份的时段。
// 只标 0 人的红格看不出「常年只有 1 人」这种隐患。
const hourlyProfile = computed(() => {
  const days = daysInMonth.value
  const out = []
  for (let h = 0; h < 24; h++) {
    let sum = 0, min = Infinity, zero = 0
    days.forEach(d => {
      const n = (coverageByDay.value[d.dateStr]?.[h] || []).length
      sum += n
      if (n < min) min = n
      if (n === 0) zero++
    })
    out.push({
      h, zero,
      min: min === Infinity ? 0 : min,
      avg: days.length ? +(sum / days.length).toFixed(1) : 0
    })
  }
  return out
})
const hourlyMax = computed(() => Math.max(1, ...hourlyProfile.value.map(x => x.avg)))

function coverageDayTitle(dateStr) {
  const gaps = coverageGapRanges(dateStr)
  const head = `${dateStr} 覆盖情况（${tzShortLabel(compareTz.value)} 时间）`
  if (gaps.length === 0) return `${head}\n全天有人在岗`
  const lines = [head]
  gaps.forEach(g => {
    lines.push(`空档 ${g.text}`)
    activeTimezones.value.filter(tz => tz !== compareTz.value).forEach(tz => {
      const z = gapInZone(g, tz)
      lines.push(`  = ${tzShortLabel(tz)} ${z.time}（${z.note}）`)
    })
    // 归因直接给在这里。用户最容易困惑的就是「格子里明明有 C 却报空档」——
    // 因为 24:00 起的班是次日 0 点上班，某天凌晨要看的是【前一天】排了谁。
    const top = gapCauses(g)[0]
    if (top) {
      const when = top.prevDay
        ? (top.startsNextDay ? `前一天 ${md(top.date)} 的${top.name}（24:00 起 = 次日 0 点上班）` : `前一天 ${md(top.date)} 的${top.name}（跨夜延续过来）`)
        : `当天的${top.name}`
      lines.push(`  本该由 ${when} 覆盖 → ${top.who.length ? '已排：' + top.who.join('、') : '无人排此班'}`)
    }
  })
  lines.push('点击查看逐小时明细，下方汇总有完整归因')
  return lines.join('\n')
}

function toggleCoverageDetail(dateStr) {
  coverageDetailDate.value = coverageDetailDate.value === dateStr ? '' : dateStr
}

// v777: 某一天的完整拆解。分两段看，缺一不可：
//  ① 今天这 24 小时谁在岗——含前一天延续过来的班（凌晨那几个小时几乎都是前一天排的）
//  ② 今天格子里排的班分别落到哪里——ig 的中班会溢出到次日 00:00，
//     sl 的晚班整段都在次日，只看①的话根本看不出「今天排的夜班岗位有没有人」
// 再加交接点，把人数掉到 0 的时刻直接标出来。
const dayDetail = computed(() => {
  const d = coverageDetailDate.value
  if (!d) return null
  const tz = compareTz.value
  const dayStart = dateStrToUtc(d, 0, tz).getTime()
  const dayEnd = dateStrToUtc(d, 1440, tz).getTime()
  const span = dayEnd - dayStart || 1

  // ① 覆盖今天的班次（可能来自前一天，也可能延续到明天）
  const onDuty = []
  employees.value.forEach(emp => {
    ;(shiftEntries.value[emp.id] || []).forEach(e => {
      if (!e.hasInterval || !e.isWorking) return
      const s = e.startUtc.getTime()
      const en = e.endUtc.getTime()
      if (s >= dayEnd || en <= dayStart) return
      onDuty.push({
        emp, entry: e,
        fromPrev: e.srcDate !== d,
        left: Math.max(0, (s - dayStart) / span * 100),
        width: Math.max(1, (Math.min(en, dayEnd) - Math.max(s, dayStart)) / span * 100),
        cutLeft: s < dayStart,
        cutRight: en > dayEnd
      })
    })
  })
  onDuty.sort((a, b) => a.entry.startUtc - b.entry.startUtc)

  // ② 今天格子里排的（含休假，也含实际整段落在明天的）
  const scheduled = []
  employees.value.forEach(emp => {
    const code = emp.shifts?.[d]
    if (!code) return
    const e = (shiftEntries.value[emp.id] || []).find(x => x.srcDate === d)
    scheduled.push({
      emp, code, entry: e,
      // 这条排的班有没有整段落在今天之外——正是「今天排的夜班明天才干活」那种情况
      spillsToNextDay: !!e && e.hasInterval && e.startUtc.getTime() >= dayEnd
    })
  })

  // ③ 交接点：今天范围内每个上下班时刻，谁上谁下、之后剩几个
  const evs = []
  employees.value.forEach(emp => {
    ;(shiftEntries.value[emp.id] || []).forEach(e => {
      if (!e.hasInterval || !e.isWorking) return
      const s = e.startUtc.getTime()
      const en = e.endUtc.getTime()
      if (s >= dayStart && s < dayEnd) evs.push({ t: s, type: 'on', emp, entry: e })
      if (en > dayStart && en <= dayEnd) evs.push({ t: en, type: 'off', emp, entry: e })
    })
  })
  const grouped = {}
  evs.forEach(x => { (grouped[x.t] ||= []).push(x) })
  // 零点那一刻已经在岗的人数：必须严格「早于」00:00 开始的才算。
  // 用 <= 会把 00:00 整点上班的人算进起始人数，而他同时又出现在 00:00 那个交接点里，
  // 于是一个人被数两遍，后面每一步的人数全部偏大。
  let cur = onDuty.filter(o => o.entry.startUtc.getTime() < dayStart).length
  const startCount = cur
  const changes = Object.keys(grouped).map(Number).sort((a, b) => a - b).map(t => {
    const g = grouped[t]
    const before = cur
    g.forEach(x => { cur += x.type === 'on' ? 1 : -1 })
    return {
      t, before, after: cur,
      // 落在当天末尾那一刻的交接，显示成 24:00 并标「次日」——
      // 直接写 00:00 会和当天开头那个 00:00 混淆
      atDayEnd: t === dayEnd,
      on: g.filter(x => x.type === 'on'),
      off: g.filter(x => x.type === 'off')
    }
  })

  return {
    date: d, tz, onDuty, scheduled, changes, startCount,
    hours: (coverageByDay.value[d] || []).map((who, h) => ({ h, count: who.length, who }))
  }
})

function ddTime(ms, tz) {
  return formatTimeInTz(new Date(ms), tz)
}
// 班次的实际时段文字；跨到次日的把日期也带上，否则「00:00」看不出是哪天
function ddRange(entry, tz) {
  const sd = formatDateInTz(entry.startUtc, tz)
  const ed = formatDateInTz(entry.endUtc, tz)
  const s = `${formatTimeInTz(entry.startUtc, tz)}`
  const e = `${formatTimeInTz(entry.endUtc, tz)}`
  return sd === ed ? `${s} – ${e}` : `${md(sd)} ${s} → ${md(ed)} ${e}`
}

const coverageSummary = computed(() => {
  let gapDays = 0
  daysInMonth.value.forEach(d => { if (coverageGaps(d.dateStr).length > 0) gapDays++ })
  return gapDays
})

// ===== v775 当前在岗 =====
// 跨时区团队里「今天」是个歧义词——北京 8/14 00:30 时贝尔格莱德还是 8/13 18:30，
// 群里问「今天晚班是谁」两边理解的不是同一个班。这一条按绝对时刻算，绕开「今天」。
const nowTick = ref(Date.now())
let nowTimer = null

// 只有在看当前月份时才有意义，翻到历史月份就不显示
const todayInView = computed(() => {
  const t = new Date()
  return t.getFullYear() === currentYear.value && (t.getMonth() + 1) === currentMonth.value
})

function nowInTz(tz) {
  const d = new Date(nowTick.value)
  return `${md(formatDateInTz(d, tz))} ${formatTimeInTz(d, tz)}`
}

// 此刻正在上班的人
const onCallNow = computed(() => {
  const t = nowTick.value
  const list = []
  employees.value.forEach(emp => {
    ;(shiftEntries.value[emp.id] || []).forEach(e => {
      if (!e.hasInterval || !e.isWorking) return
      if (e.startUtc.getTime() <= t && e.endUtc.getTime() > t) {
        list.push({ emp, entry: e, endsIn: e.endUtc.getTime() - t, leader: isLeader(emp) })
      }
    })
  })
  return list.sort((a, b) => a.endsIn - b.endsIn)
})

// 所有上下班时刻，用来算「下一次换班」
const shiftEvents = computed(() => {
  const evs = []
  employees.value.forEach(emp => {
    ;(shiftEntries.value[emp.id] || []).forEach(e => {
      if (!e.hasInterval || !e.isWorking) return
      evs.push({ t: e.startUtc.getTime(), type: 'on', emp, entry: e })
      evs.push({ t: e.endUtc.getTime(), type: 'off', emp, entry: e })
    })
  })
  return evs.sort((a, b) => a.t - b.t)
})

// 接下来两次换班时刻，各自谁上谁下
const nextChanges = computed(() => {
  const t = nowTick.value
  const future = shiftEvents.value.filter(x => x.t > t)
  const times = []
  for (const x of future) {
    if (!times.includes(x.t)) times.push(x.t)
    if (times.length >= 2) break
  }
  return times.map(tt => ({
    at: tt,
    inMs: tt - t,
    on: future.filter(x => x.t === tt && x.type === 'on'),
    off: future.filter(x => x.t === tt && x.type === 'off')
  }))
})

// 换班名单按「组 + 时区」拆开。同一个绝对时刻，A 组在贝尔格莱德是当地 09:00 上班、
// B 组在上海是当地 15:00 上班——混成一行就看不出谁几点上，也看不出是哪个组。
function groupChangeEvents(list) {
  const map = {}
  list.forEach(x => {
    const group = x.emp.group_name || '未分组'
    const key = `${group}|${x.entry.tz}`
    ;(map[key] ||= { group, tz: x.entry.tz, items: [] }).items.push(x)
  })
  return Object.values(map).sort((a, b) => a.group.localeCompare(b.group) || a.tz.localeCompare(b.tz))
}

function humanDuration(ms) {
  const m = Math.max(0, Math.round(ms / 60000))
  if (m < 60) return `${m} 分钟`
  const h = Math.floor(m / 60)
  const mm = m % 60
  return mm ? `${h} 小时 ${mm} 分` : `${h} 小时`
}

// ===== 员工时区设置 =====
function openTimezoneModal(emp) {
  timezoneEmployee.value = emp
  const today = new Date()
  const todayStr = `${today.getFullYear()}-${String(today.getMonth() + 1).padStart(2, '0')}-${String(today.getDate()).padStart(2, '0')}`
  timezoneForm.value = {
    // 默认值取「生效日期那天」的时区，两个字段口径一致，避免看着像要改其实没改
    timezone: resolveTimezoneAt(emp.timezones, todayStr),
    effective_from: todayStr
  }
  showTimezoneModal.value = true
}

async function saveTimezone() {
  const emp = timezoneEmployee.value
  if (!emp) return
  if (!timezoneForm.value.timezone || !timezoneForm.value.effective_from) {
    appStore.showToast('请填写时区和生效日期', 'error')
    return
  }
  try {
    await api.post('/api/schedule/timezone', {
      employee_id: emp.id,
      timezone: timezoneForm.value.timezone,
      effective_from: timezoneForm.value.effective_from
    })
    appStore.showToast('时区已保存', 'success')
    // 保存即关闭，和「班次配置」等其他弹窗一致。
    // 原来留在弹窗里是想让人接着加第二段时区历史，但那是少数情况，
    // 多数人保存完看不到弹窗关掉会以为没存上。要加多段就再点开一次。
    showTimezoneModal.value = false
    await loadSchedule()
  } catch (e) {
    appStore.showToast('保存失败: ' + (e.response?.data || e.message), 'error')
  }
}

async function deleteTimezoneEntry(entry) {
  const confirmed = await appStore.showConfirm({
    type: 'danger', title: '删除时区记录',
    message: `确定删除「${entry.effective_from} 起 ${entry.timezone}」这条记录吗？删除后该日期之后将沿用上一条时区。`,
    confirmText: '删除', cancelText: '取消'
  })
  if (!confirmed) return
  try {
    await api.delete(`/api/schedule/timezone?id=${entry.id}`)
    appStore.showToast('已删除', 'success')
    const empId = timezoneEmployee.value?.id
    await loadSchedule()
    timezoneEmployee.value = employees.value.find(e => e.id === empId) || null
  } catch (e) {
    appStore.showToast('删除失败: ' + (e.response?.data || e.message), 'error')
  }
}

function applyCustomTz() {
  if (!customTz.value) return
  viewTz.value = customTz.value
}

// isWorking v772: 是否算「有人在岗」，覆盖空档检查用。不能拿 isDuty 顶替——
// isDuty 是「值班」（A+ 是值班但 A 不是，两者都算在岗），休假类两个标记都为假。
const shiftTypes = ref([
  { code: 'A', label: 'A', name: '早班', time: '09:00-18:00', time_en: '09:00 - 18:00', color: '#3a84ff', isDuty: false, isWorking: true },
  { code: 'B', label: 'B', name: '中班', time: '15:00-24:00', time_en: '15:00 - 24:00', color: '#ff9c01', isDuty: false, isWorking: true },
  { code: 'C', label: 'C', name: '晚班', time: '24:00-09:00', time_en: '24:00 - 09:00', color: '#8b5cf6', isDuty: false, isWorking: true },
  { code: 'D', label: 'D', name: '值班', time: '全天', time_en: 'Duty', color: '#ea3636', isDuty: true, isWorking: true },
  { code: 'OD', label: 'OD', name: '周末休', time: '-', time_en: 'Weekend off', color: '#6b7280', isDuty: false, isWorking: false },
  { code: 'OFF', label: 'OFF', name: '排班休', time: '-', time_en: 'Scheduled off', color: '#94a3b8', isDuty: false, isWorking: false },
  { code: 'H', label: 'H', name: '公共假期', time: '-', time_en: 'Holidays', color: '#10b981', isDuty: false, isWorking: false },
  { code: 'PL', label: 'PL', name: '事假', time: '-', time_en: 'Personal Leave', color: '#f59e0b', isDuty: false, isWorking: false },
  { code: 'SL', label: 'SL', name: '病假', time: '-', time_en: 'Sick Leave', color: '#ef4444', isDuty: false, isWorking: false },
  { code: 'AL', label: 'AL', name: '年假', time: '-', time_en: 'Annual Leave', color: '#06b6d4', isDuty: false, isWorking: false },
  { code: 'CT', label: 'CT', name: '调休', time: '-', time_en: 'Change Shift', color: '#a855f7', isDuty: false, isWorking: false }
])

const weekDays = ['日', '一', '二', '三', '四', '五', '六']

const daysInMonth = computed(() => {
  const days = []
  const year = currentYear.value
  const month = currentMonth.value
  const totalDays = new Date(year, month, 0).getDate()
  const now = new Date()
  const todayStr = `${now.getFullYear()}-${String(now.getMonth() + 1).padStart(2, '0')}-${String(now.getDate()).padStart(2, '0')}`

  for (let day = 1; day <= totalDays; day++) {
    const date = new Date(year, month - 1, day)
    const dateStr = `${year}-${String(month).padStart(2, '0')}-${String(day).padStart(2, '0')}`
    days.push({
      day,
      dateStr,
      weekDay: date.getDay(),
      isWeekend: date.getDay() === 0 || date.getDay() === 6,
      isToday: dateStr === todayStr
    })
  }
  return days
})

// v776: 一级按组、二级按时区分小节。
// 组和时区不是一一对应的（ig 组里可以有人在上海），把同时区的人排在一起，
// 谁在哪个时区一眼可见，也和覆盖计算的口径对得上——决定 24 小时排满的是时区不是组。
// ⚠️ 时区归属取月末那天，和姓名旁的徽标口径一致；月中搬过家的人会归到搬完之后的时区，
// 他前半月的班仍按原时区换算（徽标会是琥珀色，提示当月内变过）。
const groupedEmployees = computed(() => {
  const groups = {}
  employees.value.forEach(emp => {
    const group = emp.group_name || '未分组'
    ;(groups[group] ||= []).push(emp)
  })
  return Object.keys(groups).sort().map(name => {
    const byTz = {}
    groups[name].forEach(emp => {
      ;(byTz[employeeTzForMonth(emp)] ||= []).push(emp)
    })
    const sections = Object.keys(byTz)
      .map(tz => ({ tz, emps: byTz[tz] }))
      // 人多的时区排前面，让主力时区在上；人数相同按时区名，保证顺序稳定
      .sort((a, b) => b.emps.length - a.emps.length || a.tz.localeCompare(b.tz))
    return { name, sections, total: groups[name].length }
  })
})

// 小节标题上的时区偏移，用月末那天算（夏令时下不同月份会不一样）
function sectionOffsetLabel(tz) {
  return tzOffsetLabel(tz, new Date(`${monthLastDay()}T12:00:00Z`))
}

// 已有组别选项（供添加/编辑员工时下拉选择，也可手输新组名）
const groupOptions = computed(() => {
  const set = new Set()
  employees.value.forEach(emp => {
    if (emp.group_name) set.add(emp.group_name)
  })
  return [...set].sort()
})

// v567: 每日统计 = 所有班次类型（不止 A/B/C/D）
// 1 号大家都 H -> 显示 "H:8"；普通工作日显示 "A:3 B:2 C:2 OFF:1" 等非零项
// v772: 统计行跟着「当前视角看到的格子」走，否则跨时区视角下表格和统计对不上。
// ⚠️ 但这只是运营视角的「当天在岗人数」，不是考勤依据——考勤（应工作天数/达成/缺勤）
// 一律按员工本人当地日历算，在独立的统计分析页，不受视角影响。
const dailyStats = computed(() => {
  const stats = {}
  daysInMonth.value.forEach(d => {
    const dateStr = d.dateStr
    // 初始化所有已知班次为 0
    const dayStat = {}
    shiftTypes.value.forEach(t => { dayStat[t.code] = 0 })
    employees.value.forEach(emp => {
      cellEntries(emp, dateStr).forEach(e => {
        // 未在 shiftTypes 里的自定义班次也兜底计数
        dayStat[e.code] = (dayStat[e.code] || 0) + 1
      })
    })
    stats[dateStr] = dayStat
  })
  return stats
})

// v775: 组长判定。规则和后端 defaultRoleEn 保持一致（含「组长」或 Leader）。
// ⚠️ role 是自由文本，靠关键字认人不够稳（「副组长」也会命中）。
// 如果以后要更可靠，应该在员工表上加一个显式的 is_leader 列，别继续堆关键字。
function isLeader(emp) {
  const role = emp.role || ''
  return role.includes('组长') || /leader/i.test(role)
}

// 给底部"统计"格用：按 shiftTypes 顺序返回 [{code, count, color}, ...]，只含非零
function dailyStatItems(dateStr) {
  const s = dailyStats.value[dateStr]
  if (!s) return []
  return shiftTypes.value
    .map(t => ({ code: t.code, count: s[t.code] || 0, color: t.color }))
    .filter(it => it.count > 0)
}

// v569: 「本月统计」整列已迁移到独立页面 /system/schedule-analytics

onMounted(() => {
  loadSchedule()
  loadShiftConfig()
  // 「当前在岗」要跟着走，30 秒一跳足够（换班精度到分钟）
  nowTimer = setInterval(() => { nowTick.value = Date.now() }, 30000)
})

onUnmounted(() => {
  if (nowTimer) clearInterval(nowTimer)
})

watch([currentYear, currentMonth], () => {
  loadSchedule()
})

async function loadSchedule() {
  loading.value = true
  try {
    // v772: pad=1 多取前后各一天。覆盖空档检查要看到边界外的班次——
    // 1 号凌晨有没有人在岗取决于上月最后一天的晚班，不多取就会误报成空档。
    const res = await api.get(`/api/schedule?year=${currentYear.value}&month=${currentMonth.value}&pad=1`)
    console.log('排班数据API响应:', res.data)
    if (res.data && res.data.length > 0) {
      console.log('第一个员工的shifts:', res.data[0].shifts)
      console.log('第一个员工的时区历史:', res.data[0].timezones)
    }
    employees.value = (res.data || []).map(e => ({ ...e, timezones: e.timezones || [] }))
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
  await loadShiftOverrides()
}

async function loadShiftOverrides() {
  try {
    const res = await api.get('/api/schedule/shift-overrides')
    shiftOverrides.value = res.data || []
    if (shiftOverrides.value.length) {
      console.log('[排班] 班次组覆盖:', shiftOverrides.value.map(o => `${o.group_name}/${o.timezone}/${o.code}=${o.time_range}`).join(', '))
    }
  } catch (e) {
    // 取不到就退回全局定义。必须 warn——静默退回会让 ig 的 C 按 sl 的口径算，差一整天
    console.warn('[排班] 班次组覆盖读取失败，全部按全局班次定义计算（跨公司班次差异不会生效）', e)
    shiftOverrides.value = []
  }
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
  const pickerWidth = 320
  const pickerHeight = 360
  const margin = 8

  let x = rect.left
  let y = rect.bottom + 4

  // 右边界检测：弹窗超出视口右侧时向左偏移
  if (x + pickerWidth > window.innerWidth - margin) {
    x = window.innerWidth - pickerWidth - margin
  }
  // 左边界保护
  if (x < margin) x = margin

  // 下边界检测：弹窗超出视口底部时改为向上弹出
  if (y + pickerHeight > window.innerHeight - margin) {
    y = rect.top - pickerHeight - 4
  }
  // 上边界保护
  if (y < margin) y = margin

  shiftPickerTarget.value = {
    empId: emp.id,
    dateStr: dateStr,
    currentShift: emp.shifts?.[dateStr] || '',
    x,
    y
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

// v614: 班次徽章字色按底色自动切黑白
// 之前无条件用白字，浅色班次（OFF #fdf0b5 对比度 1.15、H #fff200 1.17、OD 1.44、AL 1.53）
// 等于白纸写白字，整张表看着发虚。这里按 WCAG 相对亮度算对比度，
// 白字对比度低于 3:1 就换深字，深色班次（A/C/A+/CT）维持白字不变。
// ⚠️ 判定规则要和后端导出 Excel 的 shiftFontColor 保持一致，否则页面和导出的表两个样。
const SHIFT_DARK_TEXT = '#1f2937'

function relativeLuminance(hex) {
  const h = String(hex || '').replace('#', '').trim()
  const full = h.length === 3 ? h.split('').map(c => c + c).join('') : h
  if (!/^[0-9a-fA-F]{6}$/.test(full)) {
    console.warn('[排班] 无法识别的班次颜色值，按深色处理:', hex)
    return 0
  }
  const chan = i => {
    const c = parseInt(full.slice(i, i + 2), 16) / 255
    return c <= 0.03928 ? c / 12.92 : Math.pow((c + 0.055) / 1.055, 2.4)
  }
  return 0.2126 * chan(0) + 0.7152 * chan(2) + 0.0722 * chan(4)
}

function contrastWithWhite(hex) {
  return 1.05 / (relativeLuminance(hex) + 0.05)
}

function shiftTextColor(hex) {
  return contrastWithWhite(hex) < 3 ? SHIFT_DARK_TEXT : '#ffffff'
}

// 浅底配深色描边、深底配白色描边，否则浅底上那圈半透明白描边会把边缘也冲淡
function shiftRingShadow(hex) {
  return contrastWithWhite(hex) < 3
    ? 'inset 0 0 0 1px rgba(15,23,42,0.16), 0 1px 1.5px rgba(15,23,42,0.18)'
    : 'inset 0 0 0 1px rgba(255,255,255,0.28), 0 1px 1.5px rgba(15,23,42,0.18)'
}

function getShiftStyle(shiftCode) {
  const shift = shiftTypes.value.find(s => s.code === shiftCode)
  if (!shift) return {}
  return {
    background: shift.color,
    color: shiftTextColor(shift.color),
    boxShadow: shiftRingShadow(shift.color)
  }
}

function getShiftInfo(shiftCode) {
  return shiftTypes.value.find(s => s.code === shiftCode) || null
}

function openEmployeeModal(mode, emp = null) {
  employeeModalMode.value = mode
  if (mode === 'edit' && emp) {
    editingEmployeeId.value = emp.id
    employeeForm.value = { name: emp.name, group_name: emp.group_name || '', role: emp.role, role_en: emp.role_en || '', timezone: employeeTzForMonth(emp) }
  } else {
    editingEmployeeId.value = null
    employeeForm.value = { name: '', group_name: '', role: '运维工程师', role_en: '', timezone: DEFAULT_TIMEZONE }
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
        emp.role_en = employeeForm.value.role_en
        await api.post('/api/schedule', [emp])
      }
    } else {
      const res = await api.post('/api/schedule/employee', employeeForm.value)
      // v772: 新员工立刻写一条 1970-01-01 的时区基线，不然要等后端下次启动才补，
      // 这期间该员工的班次会按默认时区显示，跨时区视图和覆盖统计都是错的
      const newId = res.data?.id
      if (newId && employeeForm.value.timezone) {
        try {
          await api.post('/api/schedule/timezone', {
            employee_id: newId,
            timezone: employeeForm.value.timezone,
            effective_from: '1970-01-01'
          })
        } catch (e) {
          console.warn('[排班] 新员工时区基线写入失败，将按默认时区处理:', e)
          appStore.showToast('员工已添加，但时区没设置成功，请在姓名旁的时区徽标里补设', 'warning')
        }
      }
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
  // 前端先查一遍重复代码，不用等后端来回一趟才知道哪里错了
  const seen = {}
  for (const s of shiftTypes.value) {
    const code = String(s.code || '').trim()
    if (!code) {
      appStore.showToast('有班次没填代码', 'error')
      return
    }
    if (seen[code]) {
      appStore.showToast(`班次代码「${code}」重复了。同一个代码只能有一条全局定义；想让某个组用不同时间段请用下方「按组覆盖」`, 'error')
      return
    }
    seen[code] = true
  }
  try {
    await api.post('/api/schedule/config', shiftTypes.value)
    showConfigModal.value = false
    appStore.showToast('班次配置已保存', 'success')
    await loadShiftConfig()
  } catch (e) {
    // 把后端说的原因带出来。原来只显示「保存失败」，等于让人自己猜
    appStore.showToast('保存失败: ' + (e.response?.data || e.message), 'error')
  }
}

function addShiftType() {
  shiftTypes.value.push({
    code: '', label: '', name: '', time: '', time_en: '', color: '#3a84ff', isDuty: false, isWorking: true
  })
}

// 「在岗」只有在时间段能解析出来时才有意义：没有时间段就贡献不了任何覆盖时段，
// 勾上只会让覆盖图显示成绿的而实际没人。后端也有同样的强制回退。
function canBeWorking(cfg) {
  return parseTimeRange(cfg.time) !== null
}

// ===== v774 班次按组覆盖 =====
const overrideForm = ref({ group_name: '', timezone: '', code: '', time_range: '', name: '' })

function overrideBaseTime(code) {
  return shiftTypes.value.find(s => s.code === code)?.time || ''
}

async function saveShiftOverride() {
  const f = overrideForm.value
  if (!f.group_name || !f.timezone || !f.code || !f.time_range) {
    appStore.showToast('组别、时区、班次、时间段都要填', 'error')
    return
  }
  try {
    await api.post('/api/schedule/shift-overrides', f)
    appStore.showToast('已保存', 'success')
    overrideForm.value = { group_name: '', timezone: '', code: '', time_range: '', name: '' }
    await loadShiftOverrides()
  } catch (e) {
    appStore.showToast('保存失败: ' + (e.response?.data || e.message), 'error')
  }
}

async function deleteShiftOverride(o) {
  const confirmed = await appStore.showConfirm({
    type: 'danger', title: '删除班次覆盖',
    message: `确定删除「${o.group_name} · ${o.timezone} · ${o.code} = ${o.time_range}」吗？删除后该组该时区下的 ${o.code} 将改用全局定义 ${overrideBaseTime(o.code)}。`,
    confirmText: '删除', cancelText: '取消'
  })
  if (!confirmed) return
  try {
    await api.delete(`/api/schedule/shift-overrides?id=${o.id}`)
    appStore.showToast('已删除', 'success')
    await loadShiftOverrides()
  } catch (e) {
    appStore.showToast('删除失败: ' + (e.response?.data || e.message), 'error')
  }
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

// v763: 导出改由后端 excelize 生成真正的 xlsx（带班次填充色、按组分块、底部图例）
// 旧实现是前端拼 CSV，CSV 存不下颜色，Excel 打开永远是全白的
function currentMonthStr() {
  return `${currentYear.value}-${String(currentMonth.value).padStart(2, '0')}`
}

function openExportModal() {
  const cur = currentMonthStr()
  exportForm.value = { mode: 'current', start: cur, end: cur }
  showExportModal.value = true
}

async function doExport() {
  let start = currentMonthStr()
  let end = start
  if (exportForm.value.mode === 'range') {
    start = exportForm.value.start
    end = exportForm.value.end
    if (!start || !end) {
      appStore.showToast('请选择起止月份', 'error')
      return
    }
    if (end < start) {
      appStore.showToast('结束月份不能早于开始月份', 'error')
      return
    }
  }

  exporting.value = true
  try {
    const res = await api.get('/api/schedule/export', {
      params: { start, end },
      responseType: 'blob'
    })
    const blob = res.data
    // 后端报错时返回的是纯文本，responseType=blob 下不会走 catch，这里显式识别一次
    if (blob.type && !blob.type.includes('spreadsheetml')) {
      const text = await blob.text()
      throw new Error(text || '导出失败')
    }
    const filename = start === end
      ? `排班表_${start}.xlsx`
      : `排班表_${start}_至_${end}.xlsx`
    saveAs(blob, filename)
    showExportModal.value = false
    appStore.showToast('导出成功', 'success')
  } catch (e) {
    let msg = e.message || '导出失败'
    if (e.response?.data instanceof Blob) {
      msg = (await e.response.data.text()) || msg
    }
    console.error('[排班导出] 失败', e)
    appStore.showToast(`导出失败：${msg}`, 'error')
  } finally {
    exporting.value = false
  }
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

// 联系人过滤
const filteredContacts = computed(() => {
  if (!contactSearchQuery.value) return contacts.value
  const q = contactSearchQuery.value.toLowerCase()
  return contacts.value.filter(c => 
    c.name?.toLowerCase().includes(q) ||
    c.phone?.toLowerCase().includes(q) ||
    c.department?.toLowerCase().includes(q) ||
    c.position?.toLowerCase().includes(q)
  )
})

// 加载联系人列表
async function loadContacts() {
  contactsLoading.value = true
  try {
    const res = await api.get('/api/schedule/contacts')
    contacts.value = res.data || []
  } catch (e) {
    console.error('加载联系人失败:', e)
    contacts.value = []
  } finally {
    contactsLoading.value = false
  }
}

// 打开联系人弹窗
function openContactModal(mode, contact = null) {
  contactModalMode.value = mode
  if (mode === 'edit' && contact) {
    editingContactId.value = contact.id
    contactForm.value = { 
      name: contact.name || '', 
      phone: contact.phone || '', 
      department: contact.department || '', 
      position: contact.position || '',
      remark: contact.remark || ''
    }
  } else {
    editingContactId.value = null
    contactForm.value = { name: '', phone: '', department: '', position: '', remark: '' }
  }
  showContactModal.value = true
}

// 保存联系人
async function saveContact() {
  if (!contactForm.value.name?.trim()) {
    appStore.showToast('请填写姓名', 'error')
    return
  }
  if (!contactForm.value.phone?.trim()) {
    appStore.showToast('请填写电话', 'error')
    return
  }
  try {
    if (contactModalMode.value === 'add') {
      await api.post('/api/schedule/contacts', contactForm.value)
      appStore.showToast('联系人添加成功', 'success')
    } else {
      await api.put(`/api/schedule/contacts/${editingContactId.value}`, contactForm.value)
      appStore.showToast('联系人更新成功', 'success')
    }
    showContactModal.value = false
    await loadContacts()
  } catch (e) {
    appStore.showToast('保存失败: ' + (e.response?.data || e.message), 'error')
  }
}

// 删除联系人
async function deleteContact(contact) {
  const confirmed = await appStore.showConfirm({ type: 'danger', title: '删除联系人', message: `确定要删除联系人 "${contact.name}" 吗？`, okText: '删除', cancelText: '取消' })
  if (!confirmed) return
  try {
    await api.delete(`/api/schedule/contacts/${contact.id}`)
    appStore.showToast('联系人已删除', 'success')
    await loadContacts()
  } catch (e) {
    appStore.showToast('删除失败: ' + (e.response?.data || e.message), 'error')
  }
}

// 切换标签页时加载数据
watch(activeTab, (tab) => {
  if (tab === 'contacts' && contacts.value.length === 0) {
    loadContacts()
  }
})
</script>

<template>
  <div class="schedule-page">
    <div class="page-header">
      <h2>排班管理</h2>
      <div class="page-tabs">
        <button class="tab-btn" :class="{ active: activeTab === 'schedule' }" @click="activeTab = 'schedule'">
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><rect x="3" y="4" width="18" height="18" rx="2" ry="2"/><line x1="16" y1="2" x2="16" y2="6"/><line x1="8" y1="2" x2="8" y2="6"/><line x1="3" y1="10" x2="21" y2="10"/></svg>
          排班表
        </button>
        <button class="tab-btn" :class="{ active: activeTab === 'contacts' }" @click="activeTab = 'contacts'">
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M22 16.92v3a2 2 0 0 1-2.18 2 19.79 19.79 0 0 1-8.63-3.07 19.5 19.5 0 0 1-6-6 19.79 19.79 0 0 1-3.07-8.67A2 2 0 0 1 4.11 2h3a2 2 0 0 1 2 1.72 12.84 12.84 0 0 0 .7 2.81 2 2 0 0 1-.45 2.11L8.09 9.91a16 16 0 0 0 6 6l1.27-1.27a2 2 0 0 1 2.11-.45 12.84 12.84 0 0 0 2.81.7A2 2 0 0 1 22 16.92z"/></svg>
          联系人电话
        </button>
        <button v-if="canViewAnalytics" class="tab-btn" :class="{ active: activeTab === 'stats' }" @click="activeTab = 'stats'">
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M18 20V10M12 20V4M6 20v-6"/></svg>
          统计分析
        </button>
      </div>
      <div class="header-actions" v-show="activeTab === 'schedule'">
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
        <button v-if="canExport" class="btn btn-secondary" @click="openExportModal">
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4"/><polyline points="7 10 12 15 17 10"/><line x1="12" y1="15" x2="12" y2="3"/></svg>
          导出Excel
        </button>
        <button v-if="canAddEmployee" class="btn btn-primary" @click="openEmployeeModal('add')">
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><line x1="12" y1="5" x2="12" y2="19"/><line x1="5" y1="12" x2="19" y2="12"/></svg>
          添加员工
        </button>
      </div>
    </div>

    <!-- 排班表内容 -->
    <template v-if="activeTab === 'schedule'">
    <div class="month-nav">
      <button class="nav-btn" @click="prevMonth"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><polyline points="15 18 9 12 15 6"/></svg></button>
      <span class="current-month">{{ currentYear }}年{{ currentMonth }}月</span>
      <button class="nav-btn" @click="nextMonth"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><polyline points="9 18 15 12 9 6"/></svg></button>
      <button class="btn btn-text" @click="goToToday">今天</button>
    </div>

    <!-- v772: 跨时区视角切换 -->
    <div class="tz-toolbar">
      <span class="tz-toolbar-label">
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><circle cx="12" cy="12" r="10"/><path d="M2 12h20M12 2a15.3 15.3 0 0 1 4 10 15.3 15.3 0 0 1-4 10 15.3 15.3 0 0 1-4-10 15.3 15.3 0 0 1 4-10z"/></svg>
        查看时区
      </span>
      <div class="tz-seg">
        <button class="tz-seg-btn" :class="{ active: viewTz === 'local' }" @click="viewTz = 'local'" title="每个格子按员工本人当地日历排。这是唯一可以编辑班次的视角。">各自本地</button>
        <button class="tz-seg-btn" :class="{ active: viewTz === 'Asia/Shanghai' }" @click="viewTz = 'Asia/Shanghai'" title="按北京时间重新归列：班次归到它「开始时刻」落在北京的那一天">北京</button>
        <button class="tz-seg-btn" :class="{ active: viewTz === browserTz }" @click="viewTz = browserTz" :title="'按你浏览器所在时区 ' + browserTz + ' 重新归列'">我的时区</button>
      </div>
      <select class="tz-custom" v-model="customTz" @change="applyCustomTz">
        <option value="">其他时区…</option>
        <option v-for="tz in tzOptionList" :key="tz" :value="tz">{{ tz }}</option>
      </select>

      <!-- 跨时区视角下表格只读，把原因说清楚，否则用户会以为是权限问题或坏了 -->
      <span v-if="effectiveViewTz" class="tz-readonly-hint">
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><rect x="3" y="11" width="18" height="11" rx="2"/><path d="M7 11V7a5 5 0 0 1 10 0v4"/></svg>
        <!-- 文字必须整段包在一个 span 里：外层是 flex，裸文本节点和 <b> 会各自变成 flex item 被压成竖排 -->
        <span>当前是 <b>{{ tzShortLabel(effectiveViewTz) }}</b> 视角，表格只读。班次按<b>开始时刻</b>归列：24:00 起的晚班本就落在次日（同时区的人也一样），一格可能出现两个班次、也可能是空格——这是同一段时间在这个时区里换了天，不是漏排。要改班请切回「各自本地」。</span>
      </span>

      <label class="tz-coverage-toggle">
        <input type="checkbox" v-model="showCoverage" />
        覆盖空档检查
        <span v-if="showCoverage && coverageSummary > 0" class="tz-gap-badge" :title="`本月有 ${coverageSummary} 天存在无人在岗的时段（按 ${tzShortLabel(compareTz)} 时间）`">{{ coverageSummary }} 天有空档</span>
      </label>
    </div>

    <!-- v775: 当前在岗。按绝对时刻算，不依赖「今天」这个跨时区下有歧义的说法 -->
    <div v-if="todayInView" class="oncall-bar">
      <div class="oc-clocks">
        <span class="oc-title">此刻</span>
        <span v-for="tz in activeTimezones" :key="tz" class="oc-clock">
          <b>{{ tzShortLabel(tz) }}</b> {{ nowInTz(tz) }}
        </span>
      </div>

      <div class="oc-body">
        <div class="oc-col">
          <div class="oc-col-title">在岗 {{ onCallNow.length }} 人</div>
          <div v-if="onCallNow.length === 0" class="oc-empty">当前无人在岗</div>
          <div v-for="o in onCallNow" :key="o.emp.id + o.entry.srcDate + o.entry.code" class="oc-person">
            <span class="oc-group">{{ o.emp.group_name || '未分组' }}</span>
            <span class="oc-shift" :style="getShiftStyle(o.entry.code)">{{ o.entry.code }}</span>
            <span class="oc-name">{{ o.emp.name }}</span>
            <span v-if="o.leader" class="oc-leader">组长</span>
            <!-- 下班时刻按【本人当地】显示：张伟人在贝尔格莱德，给他看北京时间等于让他自己减 6 小时 -->
            <span class="oc-tz">{{ tzShortLabel(o.entry.tz) }} {{ formatTimeInTz(o.entry.endUtc, o.entry.tz) }} 下班</span>
            <span class="oc-left">{{ humanDuration(o.endsIn) }}后</span>
          </div>
        </div>

        <div class="oc-col">
          <div class="oc-col-title">接下来换班</div>
          <div v-if="nextChanges.length === 0" class="oc-empty">本月剩余排班里没有换班点了</div>
          <div v-for="c in nextChanges" :key="c.at" class="oc-change">
            <!-- 换班时刻各时区并排。日期一律带上——两个时区的日期常常不是同一天，
                 那正是最容易看错的地方（北京 8/14 00:00 时贝尔格莱德还是 8/13 18:00） -->
            <div class="oc-change-head">
              <b v-for="(tz, i) in activeTimezones" :key="tz" class="oc-at">
                <span v-if="i > 0" class="oc-sep">·</span>
                {{ tzShortLabel(tz) }} {{ md(formatDateInTz(new Date(c.at), tz)) }} {{ formatTimeInTz(new Date(c.at), tz) }}
              </b>
              <span class="oc-in">{{ humanDuration(c.inMs) }}后</span>
            </div>
            <!-- 按组和时区拆行，并给出各自的当地时刻 -->
            <div v-if="c.off.length" class="oc-flow off">
              <span class="oc-flow-tag">下班</span>
              <div class="oc-flow-groups">
                <div v-for="g in groupChangeEvents(c.off)" :key="'off'+g.group+g.tz" class="oc-flow-line">
                  <span class="oc-fg">{{ g.group }}</span>
                  <span class="oc-ftz">{{ tzShortLabel(g.tz) }} {{ formatTimeInTz(new Date(c.at), g.tz) }}</span>
                  <span v-for="x in g.items" :key="'off'+x.emp.id+x.entry.srcDate" class="oc-mini">{{ x.emp.name }}<i>{{ x.entry.code }}</i></span>
                </div>
              </div>
            </div>
            <div v-if="c.on.length" class="oc-flow on">
              <span class="oc-flow-tag">上班</span>
              <div class="oc-flow-groups">
                <div v-for="g in groupChangeEvents(c.on)" :key="'on'+g.group+g.tz" class="oc-flow-line">
                  <span class="oc-fg">{{ g.group }}</span>
                  <span class="oc-ftz">{{ tzShortLabel(g.tz) }} {{ formatTimeInTz(new Date(c.at), g.tz) }}</span>
                  <span v-for="x in g.items" :key="'on'+x.emp.id+x.entry.srcDate" class="oc-mini">{{ x.emp.name }}<i>{{ x.entry.code }}</i></span>
                </div>
              </div>
            </div>
          </div>
        </div>
      </div>
    </div>

    <div class="shift-legend">
      <div v-for="s in shiftTypes" :key="s.code" class="legend-item" :style="{ '--shift-color': s.color }">
        <span class="legend-code" :style="{ background: s.color, color: shiftTextColor(s.color) }">{{ s.code }}</span>
        <span class="legend-name">{{ s.name }}</span>
        <span class="legend-time" v-if="s.time && s.time !== '-'">{{ s.time }}</span>
        <span class="legend-duty" v-if="s.isDuty">值班</span>
      </div>
      <!-- 图例是全局的，不同员工时区不同，这里只能标明口径；具体换算在员工行和格子的悬浮说明里 -->
      <div class="legend-tz-note">时间为员工本地时间</div>
      <!-- v774: 有组用了不同的班次口径就在图例上说明，否则同一个 C 在表里代表两种时段却毫无提示 -->
      <div v-if="shiftOverrides.length" class="legend-override-note"
           :title="shiftOverrides.map(o => `${o.group_name} · ${tzShortLabel(o.timezone)} 的 ${o.code} = ${o.time_range}${o.name ? '（' + o.name + '）' : ''}，全局是 ${overrideBaseTime(o.code)}`).join('\n')">
        ⚠️ 有 {{ shiftOverrides.length }} 条按组的班次差异
        <span class="lon-detail">{{ shiftOverrides.map(o => `${o.group_name} 的 ${o.code}=${o.time_range}`).join('；') }}</span>
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
              <th v-for="d in daysInMonth" :key="d.dateStr" class="th-day"
                  :class="{ weekend: d.isWeekend, today: d.isToday, 'gap-col': showCoverage && hasGap(d.dateStr) }"
                  :title="showCoverage && hasGap(d.dateStr) ? coverageDayTitle(d.dateStr) : ''">
                <div class="day-num">{{ d.day }}</div>
                <div class="day-week">{{ weekDays[d.weekDay] }}</div>
              </th>
            </tr>
          </thead>
          <tbody>
            <template v-for="g in groupedEmployees" :key="g.name">
              <tr class="group-row">
                <td :colspan="daysInMonth.length + 2" class="group-cell">
                  {{ g.name }}
                  <span class="group-count">{{ g.total }} 人</span>
                  <!-- 一个组横跨多个时区时点出来，这会影响 ABC 是否真能排满 24 小时 -->
                  <span v-if="g.sections.length > 1" class="group-multi-tz">横跨 {{ g.sections.length }} 个时区</span>
                </td>
              </tr>
              <template v-for="sec in g.sections" :key="g.name + sec.tz">
                <tr class="tz-section-row">
                  <td :colspan="daysInMonth.length + 2" class="tz-section-cell">
                    <span class="tzs-name">{{ tzShortLabel(sec.tz) }}</span>
                    <span class="tzs-full">{{ sec.tz }}</span>
                    <span class="tzs-off">{{ sectionOffsetLabel(sec.tz) }}</span>
                    <span class="tzs-count">{{ sec.emps.length }} 人</span>
                  </td>
                </tr>
              <tr v-for="emp in sec.emps" :key="emp.id" class="employee-row"
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
                    <!-- 姓名和时区徽标上下两行：姓名列只有 120px，横排会把名字挤成 0 宽 -->
                    <span class="emp-name-wrap">
                      <span class="emp-name" :class="{ leader: isLeader(emp) }"
                            :title="isLeader(emp) ? emp.name + ' 是组长，排的班不计入每日 A/B/C 完整性检查（但仍算在岗人力）' : ''">
                        {{ emp.name }}<i v-if="isLeader(emp)" class="leader-dot" aria-hidden="true"></i>
                      </span>
                      <!-- v772: 时区徽标。点开可设置时区及生效日期 -->
                      <button class="tz-badge"
                              :class="{ foreign: employeeTzForMonth(emp) !== compareTz, changed: employeeTzSpan(emp).changed }"
                              :title="employeeTzTitle(emp)"
                              :disabled="!canEditTimezone"
                              @click.stop="canEditTimezone && openTimezoneModal(emp)">{{ employeeTzBadge(emp) }}</button>
                    </span>
                    <div class="emp-actions" v-if="canEditEmployee || canDeleteEmployee">
                      <button v-if="canEditEmployee" class="action-btn sm" @click.stop="openEmployeeModal('edit', emp)" title="编辑"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M11 5H6a2 2 0 00-2 2v11a2 2 0 002 2h11a2 2 0 002-2v-5m-1.414-9.414a2 2 0 112.828 2.828L11.828 15H9v-2.828l8.586-8.586z"/></svg></button>
                      <button v-if="canDeleteEmployee" class="action-btn sm danger" @click.stop="deleteEmployee(emp)" title="删除"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M19 7l-.867 12.142A2 2 0 0116.138 21H7.862a2 2 0 01-1.995-1.858L5 7m5 4v6m4-6v6m1-10V4a1 1 0 00-1-1h-4a1 1 0 00-1 1v3M4 7h16"/></svg></button>
                    </div>
                  </div>
                </td>
                <td class="sticky-role td-role">{{ emp.role || '运维工程师' }}</td>
                <td v-for="d in daysInMonth" :key="d.dateStr" class="td-shift"
                    :class="{ weekend: d.isWeekend, clickable: gridEditable, today: d.isToday, 'multi-shift': cellEntries(emp, d.dateStr).length > 1, 'gap-col': showCoverage && hasGap(d.dateStr) }"
                    @click="gridEditable && openShiftPicker(emp, d.dateStr, $event)">
                  <!-- 跨时区视角下一格可能有两个班次：前一天的晚班和当天的早班，在这个时区里落到了同一天 -->
                  <span v-for="(entry, i) in cellEntries(emp, d.dateStr)" :key="entry.srcDate + '-' + i"
                        class="shift-badge" :style="getShiftStyle(entry.code)" :title="entryTitle(entry)">
                    {{ entry.code }}
                  </span>
                </td>
              </tr>
              </template>
            </template>
            <tr class="stats-row">
              <td class="sticky-name stats-label" :title="effectiveViewTz
                    ? `按 ${tzShortLabel(effectiveViewTz)} 时间统计当天在岗人数，仅供运营参考。考勤（应工作天数/达成/缺勤）在统计分析页，恒按员工本人当地日历算，不受视角影响。`
                    : '按员工本人当地日历统计'">
                {{ effectiveViewTz ? `在岗(${tzShortLabel(effectiveViewTz)})` : '统计' }}
              </td>
              <td class="sticky-role"></td>
              <td v-for="d in daysInMonth" :key="d.dateStr" class="td-stats" :class="{ today: d.isToday }">
                <div class="stats-mini">
                  <span v-for="it in dailyStatItems(d.dateStr)" :key="it.code" class="stat-item" :style="{ color: it.color }">
                    {{ it.code }}:{{ it.count }}
                  </span>
                </div>
              </td>
            </tr>
            <!-- v772: 覆盖空档条。每天一根 24 格迷你条，红格 = 该小时全组无人在岗 -->
            <tr v-if="showCoverage" class="coverage-row">
              <td class="sticky-name coverage-label" :title="`以 ${compareTz} 为基准，把每个人的班次按各自时区换算成绝对时间后逐小时数人头。红色 = 该小时无人在岗。\n点任意一天可展开当天的完整拆解。`">
                覆盖({{ tzShortLabel(compareTz) }})
                <span class="row-hint">点日期看详情</span>
              </td>
              <td class="sticky-role"></td>
              <td v-for="d in daysInMonth" :key="d.dateStr" class="td-coverage"
                  :class="{ today: d.isToday, active: coverageDetailDate === d.dateStr, 'gap-col': hasGap(d.dateStr) }"
                  :title="coverageDayTitle(d.dateStr)" @click="toggleCoverageDetail(d.dateStr)">
                <div class="coverage-bar">
                  <i v-for="(who, h) in coverageByDay[d.dateStr]" :key="h"
                     class="coverage-hour" :class="who.length === 0 ? 'gap' : (who.length === 1 ? 'thin' : 'ok')"></i>
                </div>
              </td>
            </tr>
            <!-- v773: 空档时段常驻显示，不用点开。列宽只有 38px，用 06-09 的缩写。
                 v776: 每个时区各一行——同一段空档在北京和贝尔格莱德是不同的钟点，
                 只给一个口径的话，另一边的人还得自己换算。 -->
            <tr v-if="showCoverage" v-for="tz in activeTimezones" :key="'gaprow-' + tz" class="gap-row">
              <td class="sticky-name gap-label"
                  :title="`该行时段按 ${tz} 计。同一段空档在不同时区是不同的钟点，所以每个时区各列一行。`">
                空档<span class="gap-label-tz">{{ tzShortLabel(tz) }}</span>
              </td>
              <td class="sticky-role"></td>
              <td v-for="d in daysInMonth" :key="d.dateStr" class="td-gap"
                  :class="{ today: d.isToday, 'gap-col': hasGap(d.dateStr) }"
                  :title="coverageDayTitle(d.dateStr)" @click="toggleCoverageDetail(d.dateStr)">
                <template v-if="hasGap(d.dateStr)">
                  <div v-for="g in coverageGapRanges(d.dateStr)" :key="g.short + tz" class="gap-time">
                    {{ gapShortInZone(g, tz) }}
                  </div>
                </template>
                <span v-else class="gap-dot">·</span>
              </td>
            </tr>
          </tbody>
        </table>
      </div>

      <!-- v777: 某一天的完整拆解。两段式——今天谁在岗 + 今天格子里排的班落到哪里。
           只看前者的话，「今天排的夜班岗位有没有人」永远看不出来 -->
      <div v-if="showCoverage && dayDetail" class="day-detail">
        <div class="dd-head">
          <span class="dd-date">{{ dayDetail.date }}</span>
          <span class="dd-gaps" v-if="coverageGaps(dayDetail.date).length">
            空档 {{ coverageGaps(dayDetail.date).join('、') }}（{{ tzShortLabel(compareTz) }}）
          </span>
          <span class="dd-ok" v-else>全天有人在岗</span>
          <button class="dd-close" @click="coverageDetailDate = ''">
            <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><line x1="18" y1="6" x2="6" y2="18"/><line x1="6" y1="6" x2="18" y2="18"/></svg>
          </button>
        </div>

        <!-- ① 今天这 24 小时谁在岗 -->
        <div class="dd-sec">
          <div class="dd-title">① 今天这 24 小时谁在岗 <span class="dd-sub">按 {{ tzShortLabel(compareTz) }} 时间画；灰底=前一天排的班延续过来</span></div>
          <div class="dd-ruler">
            <span v-for="h in 9" :key="h" class="dd-tick">{{ String((h-1)*3).padStart(2,'0') }}</span>
          </div>
          <div v-if="dayDetail.onDuty.length === 0" class="dd-empty">这一天全天没有任何人在岗</div>
          <div v-for="(o, i) in dayDetail.onDuty" :key="'od'+i" class="dd-row">
            <span class="dd-who">
              {{ o.emp.name }}
              <i class="dd-code" :style="getShiftStyle(o.entry.code)">{{ o.entry.code }}</i>
            </span>
            <span class="dd-track">
              <i class="dd-bar" :class="{ prev: o.fromPrev, cutl: o.cutLeft, cutr: o.cutRight }"
                 :style="{ left: o.left + '%', width: o.width + '%' }"></i>
            </span>
            <span class="dd-meta">
              {{ o.emp.group_name || '未分组' }}·{{ tzShortLabel(o.entry.tz) }}
              <b>{{ ddRange(o.entry, compareTz) }}</b>
              <em>本人 {{ o.entry.cfg.time }}</em>
              <span v-if="o.fromPrev" class="dd-prevtag">{{ md(o.entry.srcDate) }} 排的</span>
            </span>
          </div>
          <div class="dd-hours">
            <span class="dd-hours-label">在岗</span>
            <i v-for="h in dayDetail.hours" :key="h.h" class="dd-hour"
               :class="h.count === 0 ? 'zero' : (h.count === 1 ? 'thin' : 'ok')"
               :title="`${String(h.h).padStart(2,'0')}:00  ${h.count} 人  ${h.who.join('、') || '无人'}`">{{ h.count }}</i>
          </div>
        </div>

        <!-- ② 今天格子里排的班，实际落到哪里 -->
        <div class="dd-sec">
          <div class="dd-title">② 今天格子里排的班，实际落在哪 <span class="dd-sub">排在今天 ≠ 今天干活，晚班常常整段落在明天</span></div>
          <div v-if="dayDetail.scheduled.length === 0" class="dd-empty">今天没有任何排班</div>
          <div v-for="(s, i) in dayDetail.scheduled" :key="'sc'+i" class="dd-line">
            <span class="dd-who">
              {{ s.emp.name }}
              <i class="dd-code" :style="getShiftStyle(s.code)">{{ s.code }}</i>
            </span>
            <span class="dd-meta">
              {{ s.emp.group_name || '未分组' }}·{{ tzShortLabel(resolveTimezoneAt(s.emp.timezones, dayDetail.date)) }}
              <template v-if="s.entry && s.entry.hasInterval">
                {{ s.entry.cfg.name }} 本人 {{ s.entry.cfg.time }}
                → <b>{{ tzShortLabel(compareTz) }} {{ ddRange(s.entry, compareTz) }}</b>
                <span v-if="s.spillsToNextDay" class="dd-spill">整段落在次日</span>
              </template>
              <template v-else>{{ s.entry?.cfg?.name || '' }}（不在岗）</template>
            </span>
          </div>
        </div>

        <!-- ③ 交接点 -->
        <div class="dd-sec">
          <div class="dd-title">③ 今天的交接点 <span class="dd-sub">谁下、谁上、之后还剩几个人</span></div>
          <div class="dd-chg start">
            <span class="dd-chg-t">00:00</span>
            <span class="dd-chg-body">起始在岗</span>
            <span class="dd-chg-n" :class="{ zero: dayDetail.startCount === 0 }">{{ dayDetail.startCount }} 人</span>
          </div>
          <div v-for="(c, i) in dayDetail.changes" :key="'ch'+i" class="dd-chg" :class="{ danger: c.after === 0 }">
            <span class="dd-chg-t">
              {{ c.atDayEnd ? '24:00' : ddTime(c.t, compareTz) }}
              <em v-if="c.atDayEnd">次日</em>
              <em v-for="tz in activeTimezones.filter(z => z !== compareTz)" :key="tz">/ {{ ddTime(c.t, tz) }}</em>
            </span>
            <span class="dd-chg-body">
              <span v-if="c.off.length" class="dd-off">↓ {{ c.off.map(x => x.emp.name + '(' + x.entry.code + ')').join(' ') }}</span>
              <span v-if="c.on.length" class="dd-on">↑ {{ c.on.map(x => x.emp.name + '(' + x.entry.code + ')').join(' ') }}</span>
              <!-- 只有真的掉到 0 才叫「没人接」。有人下班但还剩几个人在岗，不构成断档 -->
              <span v-if="c.after === 0" class="dd-noone">⚠️ 无人在岗</span>
            </span>
            <span class="dd-chg-n" :class="{ zero: c.after === 0 }">{{ c.before }} → {{ c.after }} 人</span>
          </div>
        </div>
      </div>
      <!-- v773: 空档汇总。按时段聚合成模式，双时区并排，附归因 -->
      <div v-if="showCoverage && gapPatterns.length > 0" class="gap-summary">
        <!-- 默认收起：它很长，展开着的话点日期展开的单日拆解会被挤到很下面 -->
        <div class="gap-summary-head clickable" @click="gapSummaryOpen = !gapSummaryOpen">
          <svg class="gs-caret" :class="{ open: gapSummaryOpen }" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><polyline points="9 18 15 12 9 6"/></svg>
          <span class="gs-title">空档汇总</span>
          <span class="gs-count">{{ gapPatterns.length }} 种时段 · 共 {{ coverageSummary }} 天</span>
          <span class="gs-sub">{{ gapSummaryOpen ? `时段按 ${compareTz} 计；每条同时给出其他时区的对照` : '点开看每段空档的归因和补法' }}</span>
        </div>

        <div v-show="gapSummaryOpen" v-for="p in gapPatterns" :key="p.text" class="gap-pattern">
          <div class="gp-head">
            <!-- 按空档时长分级：连续 6 小时以上无人和缺 1 小时不是一回事 -->
            <span class="gp-sev" :class="{ partial: p.hours < 6 }">无人在岗 {{ p.hours }} 小时</span>
            <span class="gp-count"><b>{{ p.days.length }}</b> 天 · {{ p.text }}</span>
          </div>

          <!-- 同一段空档在各时区的表示。跨天的标出来——这正是切视角看不清的地方 -->
          <div class="gp-zones">
            <div v-for="tz in activeTimezones" :key="tz" class="gp-zone" :class="{ basis: tz === compareTz }">
              <span class="gz-name">
                {{ tzShortLabel(tz) }}
                <!-- 带上组名：光看时区名不知道是哪些人，而且一个组可能横跨两个时区 -->
                <em v-if="tzGroupsLabel(tz)" class="gz-groups">{{ tzGroupsLabel(tz) }}</em>
              </span>
              <span class="gz-time">{{ gapInZone(p.sample, tz).time }}</span>
              <span class="gz-note">{{ gapInZone(p.sample, tz).note }}</span>
            </div>
          </div>

          <div class="gp-days">
            <span class="gp-days-label">发生在</span>
            <button v-for="d in p.days" :key="d" class="gp-daychip"
                    :class="{ active: coverageDetailDate === d }"
                    @click="toggleCoverageDetail(d)">{{ md(d) }}</button>
          </div>

          <!-- 归因：这段本该由哪天的哪个班次覆盖。
               ⚠️ 重点是「哪天」——24:00 起的晚班从次日 0 点上班，
               所以凌晨空档要看前一天排了谁，不是看当天格子里有没有 C -->
          <div class="gp-cause">
            <!-- 必须用 p.sample.dateStr：样本日会为避开月初边界而挪，写 days[0] 会和下面算出来的日期对不上 -->
            <div class="gpc-title">这段本该由谁覆盖（以 {{ md(p.sample.dateStr) }} 为例）</div>
            <div v-for="c in gapCauses(p.sample)" :key="c.date + c.code + c.tz" class="gpc-item">
              <span class="gpc-date">{{ md(c.date) }}</span>
              <span class="gpc-shift" :style="getShiftStyle(c.code)">{{ c.code }}</span>
              <span class="gpc-desc">
                {{ c.name }}（{{ tzShortLabel(c.tz) }} {{ c.time }}）
                <template v-if="c.prevDay && c.startsNextDay">· 前一天排的班，24:00 起 = 次日 0 点上班</template>
                <template v-else-if="c.prevDay">· 前一天排的班，跨夜延续过来</template>
                · 可补 {{ c.hours }} 小时
              </span>
              <span class="gpc-who" :class="{ none: c.who.length === 0 }">
                {{ c.who.length ? '当天已排：' + c.who.join('、') : '当天无人排此班' }}
              </span>
            </div>
          </div>

          <!-- v774: 按组给补法。只说「哪个组、哪天、排什么班」，不指定具体的人 -->
          <div class="gp-fix">
            <div class="gpf-title">
              怎么补（以 {{ md(p.sample.dateStr) }} 为例）
              <span class="gpf-hint">只给到组和班次，谁去上由你定</span>
            </div>
            <div class="gpf-grid">
              <div v-for="f in gapFixOptions(p.sample)" :key="f.group" class="gpf-group">
                <div class="gpf-ghead">
                  <span class="gpf-gname">{{ f.group }}</span>
                  <span class="gpf-gtz">{{ f.tzs.map(tzShortLabel).join(' / ') }}</span>
                  <span v-if="!f.solvable" class="gpf-nofix">单个班次补不齐</span>
                </div>
                <div v-for="o in f.options" :key="o.code + o.date + o.tz" class="gpf-opt">
                  <span class="gpf-date">{{ md(o.date) }}</span>
                  <span class="gpf-shift" :style="getShiftStyle(o.code)">{{ o.code }}</span>
                  <!-- 组内有多个时区时必须点明由哪边的人排：同一个班次代码在两个时区
                       落到的绝对时段完全不同，补不补得上是两回事 -->
                  <span v-if="f.tzs.length > 1" class="gpf-bytz">{{ tzShortLabel(o.tz) }} 的人排</span>
                  <span class="gpf-desc">
                    {{ o.name }} {{ o.time }}
                    <template v-if="o.prevDay && o.startsNextDay">（前一天排，24:00 起 = 次日 0 点上班）</template>
                    <template v-else-if="o.prevDay">（前一天排，跨夜延续过来）</template>
                    <span class="gpf-basis">→ {{ tzShortLabel(compareTz) }} {{ o.inBasis }}</span>
                  </span>
                  <span class="gpf-verdict" :class="o.full ? 'ok' : 'part'">
                    {{ o.full ? '补齐全部 ' + f.gapHours + ' 小时' : '只能补 ' + o.covered + ' 小时，还差 ' + (f.gapHours - o.covered) }}
                  </span>
                  <span class="gpf-already" v-if="o.already > 0">该组当天已有 {{ o.already }} 人排此班</span>
                </div>
              </div>
            </div>
          </div>
        </div>

        <!-- 全月人手分布：只标 0 人看不出「常年只有 1 人」这种隐患 -->
        <div v-show="gapSummaryOpen" class="gp-profile">
          <div class="gpp-title">
            全月每小时在岗人数（{{ tzShortLabel(compareTz) }}）
            <span class="gpp-legend">
              <i class="lg ok"></i>2 人以上
              <i class="lg thin"></i>长期仅 1 人
              <i class="lg gap"></i>出现过无人
            </span>
          </div>
          <div class="gpp-band">
            <div v-for="p in hourlyProfile" :key="p.h" class="gpp-col"
                 :class="p.zero > 0 ? 'bad' : (p.avg <= 1.5 ? 'warn' : '')"
                 :title="`${tzShortLabel(compareTz)} ${String(p.h).padStart(2,'0')}:00-${String(p.h+1).padStart(2,'0')}:00\n平均 ${p.avg} 人，最少 ${p.min} 人${p.zero ? '\n⚠️ ' + p.zero + ' 天无人在岗' : ''}`">
              <span class="gpp-avg">{{ p.avg }}</span>
              <i class="gpp-bar" :style="{ height: Math.round(p.avg / hourlyMax * 46) + 2 + 'px' }"></i>
              <span class="gpp-tick">{{ p.h % 3 === 0 ? String(p.h).padStart(2, '0') : '' }}</span>
            </div>
          </div>
        </div>
      </div>

      <!-- v772: 覆盖空档逐小时明细 -->
    </div>
    </template>

    <!-- 联系人电话页面 -->
    <template v-if="activeTab === 'contacts'">
      <div class="contacts-section">
        <div class="contacts-toolbar">
          <div class="search-box">
            <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><circle cx="11" cy="11" r="8"/><line x1="21" y1="21" x2="16.65" y2="16.65"/></svg>
            <input type="text" v-model="contactSearchQuery" placeholder="搜索姓名、电话、部门...">
          </div>
          <button class="btn btn-primary" @click="openContactModal('add')">
            <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><line x1="12" y1="5" x2="12" y2="19"/><line x1="5" y1="12" x2="19" y2="12"/></svg>
            添加联系人
          </button>
        </div>

        <div v-if="contactsLoading" class="loading-state">加载中...</div>
        <div v-else-if="filteredContacts.length === 0" class="empty-state">
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5"><path d="M22 16.92v3a2 2 0 0 1-2.18 2 19.79 19.79 0 0 1-8.63-3.07 19.5 19.5 0 0 1-6-6 19.79 19.79 0 0 1-3.07-8.67A2 2 0 0 1 4.11 2h3a2 2 0 0 1 2 1.72 12.84 12.84 0 0 0 .7 2.81 2 2 0 0 1-.45 2.11L8.09 9.91a16 16 0 0 0 6 6l1.27-1.27a2 2 0 0 1 2.11-.45 12.84 12.84 0 0 0 2.81.7A2 2 0 0 1 22 16.92z"/></svg>
          <div>暂无联系人</div>
          <p>点击"添加联系人"开始添加</p>
        </div>
        <div v-else class="contacts-table-wrapper">
          <table class="contacts-table">
            <thead>
              <tr>
                <th>姓名</th>
                <th>电话</th>
                <th>部门</th>
                <th>职位</th>
                <th>备注</th>
                <th class="th-actions">操作</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="contact in filteredContacts" :key="contact.id">
                <td class="td-name">{{ contact.name }}</td>
                <td class="td-phone">
                  <a :href="'tel:' + contact.phone" class="phone-link">{{ contact.phone }}</a>
                </td>
                <td>{{ contact.department || '-' }}</td>
                <td>{{ contact.position || '-' }}</td>
                <td class="td-remark">{{ contact.remark || '-' }}</td>
                <td class="td-actions">
                  <button class="btn-icon" title="编辑" @click="openContactModal('edit', contact)">
                    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M11 4H4a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2h14a2 2 0 0 0 2-2v-7"/><path d="M18.5 2.5a2.121 2.121 0 0 1 3 3L12 15l-4 1 1-4 9.5-9.5z"/></svg>
                  </button>
                  <button class="btn-icon btn-danger" title="删除" @click="deleteContact(contact)">
                    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><polyline points="3 6 5 6 21 6"/><path d="M19 6v14a2 2 0 0 1-2 2H7a2 2 0 0 1-2-2V6m3 0V4a2 2 0 0 1 2-2h4a2 2 0 0 1 2 2v2"/></svg>
                  </button>
                </td>
              </tr>
            </tbody>
          </table>
        </div>
      </div>
    </template>

    <!-- 统计分析页面 v570: tab 内嵌; v571: 加权限守门（防止从外部 setting activeTab=stats 绕过 tab 按钮显隐） -->
    <template v-if="activeTab === 'stats' && canViewAnalytics">
      <ScheduleAnalyticsView />
    </template>

    <!-- 添加/编辑联系人弹窗 -->
    <Teleport to="body">
      <div class="modal-overlay" :class="{ active: showContactModal }">
        <div class="modal contact-modal">
          <div class="modal-header">
            <h2>{{ contactModalMode === 'edit' ? '编辑联系人' : '添加联系人' }}</h2>
            <button class="modal-close" @click="showContactModal = false"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><line x1="18" y1="6" x2="6" y2="18"/><line x1="6" y1="6" x2="18" y2="18"/></svg></button>
          </div>
          <form class="modal-form" @submit.prevent="saveContact">
            <div class="modal-body">
              <div class="form-group"><label>姓名 *</label><input type="text" class="form-input" v-model="contactForm.name" placeholder="联系人姓名" required /></div>
              <div class="form-group"><label>电话 *</label><input type="tel" class="form-input" v-model="contactForm.phone" placeholder="联系电话" required /></div>
              <div class="form-group"><label>部门</label><input type="text" class="form-input" v-model="contactForm.department" placeholder="所属部门" /></div>
              <div class="form-group"><label>职位</label><input type="text" class="form-input" v-model="contactForm.position" placeholder="职位/岗位" /></div>
              <div class="form-group"><label>备注</label><textarea class="form-input" v-model="contactForm.remark" placeholder="备注信息" rows="3"></textarea></div>
            </div>
            <div class="modal-footer">
              <button type="button" class="btn btn-secondary" @click="showContactModal = false">取消</button>
              <button type="submit" class="btn btn-primary">保存</button>
            </div>
          </form>
        </div>
      </div>
    </Teleport>

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
              <div class="form-group">
                <label>组别</label>
                <input type="text" class="form-input" v-model="employeeForm.group_name" list="schedule-group-options" placeholder="选择已有组别或输入新组名" />
                <datalist id="schedule-group-options">
                  <option v-for="g in groupOptions" :key="g" :value="g" />
                </datalist>
              </div>
              <div class="form-group"><label>职位</label><input type="text" class="form-input" v-model="employeeForm.role" placeholder="运维工程师" /></div>
              <div class="form-group" v-if="employeeModalMode === 'add'">
                <label>时区</label>
                <select class="form-input" v-model="employeeForm.timezone">
                  <option v-for="tz in tzOptionList" :key="tz" :value="tz">{{ tz }}</option>
                </select>
                <div class="form-hint">该员工班次时间按哪个时区解释。以后搬去别的时区，在排班表姓名旁的时区徽标里新增一段生效日期即可，历史排班不受影响。</div>
              </div>
              <div class="form-group">
                <label>职位（英文）</label>
                <input type="text" class="form-input" v-model="employeeForm.role_en" list="schedule-role-en-options" placeholder="YW Team" />
                <datalist id="schedule-role-en-options">
                  <option value="YW Leader" />
                  <option value="YW Team" />
                </datalist>
                <div class="form-hint">导出 Excel 的「组别/日期」列用；留空自动按中文职位推导（含"组长"→ YW Leader，其余 → YW Team）</div>
              </div>
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

    <!-- 导出 Excel 弹窗 (v763) -->
    <Teleport to="body">
      <div class="modal-overlay" :class="{ active: showExportModal }">
        <div class="modal export-modal">
          <div class="modal-header">
            <h2>导出 Excel</h2>
            <button class="modal-close" @click="showExportModal = false"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><line x1="18" y1="6" x2="6" y2="18"/><line x1="6" y1="6" x2="18" y2="18"/></svg></button>
          </div>
          <div class="modal-body">
            <div class="export-options">
              <label class="export-option" :class="{ active: exportForm.mode === 'current' }">
                <input type="radio" value="current" v-model="exportForm.mode" />
                <span class="export-option-title">当前月</span>
                <span class="export-option-desc">{{ currentYear }}年{{ currentMonth }}月，单个 sheet</span>
              </label>
              <label class="export-option" :class="{ active: exportForm.mode === 'range' }">
                <input type="radio" value="range" v-model="exportForm.mode" />
                <span class="export-option-title">月份区间</span>
                <span class="export-option-desc">每个月一个 sheet，最多 12 个月</span>
              </label>
            </div>
            <div v-if="exportForm.mode === 'range'" class="export-range">
              <div class="form-group">
                <label>开始月份</label>
                <input type="month" class="form-input" v-model="exportForm.start" />
              </div>
              <div class="form-group">
                <label>结束月份</label>
                <input type="month" class="form-input" v-model="exportForm.end" />
              </div>
            </div>
            <div class="form-hint">导出的表格按组分块、班次格填对应颜色，底部附班次图例。颜色取自「班次配置」。</div>
          </div>
          <div class="modal-footer">
            <button type="button" class="btn btn-secondary" @click="showExportModal = false">取消</button>
            <button type="button" class="btn btn-primary" :disabled="exporting" @click="doExport">{{ exporting ? '生成中...' : '导出' }}</button>
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
                <span class="cfg-col-time-en">英文说明</span>
                <span class="cfg-col-color">颜色</span>
                <span class="cfg-col-duty">值班</span>
                <span class="cfg-col-duty" title="是否算「有人在岗」，跨时区覆盖空档检查用。时间段填「-」的休假类不能勾。">在岗</span>
                <span class="cfg-col-action">操作</span>
              </div>
              <div v-for="(s, idx) in shiftTypes" :key="idx" class="config-item">
                <input type="text" class="cfg-input cfg-input-code" v-model="s.code" placeholder="A" maxlength="4" @input="s.label = s.code" />
                <input type="text" class="cfg-input cfg-input-name" v-model="s.name" placeholder="早班" />
                <input type="text" class="cfg-input cfg-input-time" v-model="s.time" placeholder="09:00-18:00" />
                <input type="text" class="cfg-input cfg-input-time-en" v-model="s.time_en" placeholder="Weekend off" title="导出 Excel 图例的「时间」列" />
                <div class="cfg-color-wrap">
                  <input type="color" class="cfg-color" v-model="s.color" title="选择颜色" />
                  <span class="cfg-color-preview" :style="{ background: s.color }">{{ s.code }}</span>
                </div>
                <label class="cfg-duty"><input type="checkbox" v-model="s.isDuty" /><span class="duty-text">{{ s.isDuty ? '是' : '否' }}</span></label>
                <label class="cfg-duty" :title="canBeWorking(s) ? '算作有人在岗，参与覆盖空档检查' : '时间段无法解析（如「-」），不能算在岗；勾了也会被后端打回'">
                  <input type="checkbox" v-model="s.isWorking" :disabled="!canBeWorking(s)" />
                  <span class="duty-text">{{ s.isWorking && canBeWorking(s) ? '是' : '否' }}</span>
                </label>
                <button class="cfg-delete-btn" @click="removeShiftType(idx)" title="删除班次"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M19 7l-.867 12.142A2 2 0 0116.138 21H7.862a2 2 0 01-1.995-1.858L5 7m5 4v6m4-6v6m1-10V4a1 1 0 00-1-1h-4a1 1 0 00-1 1v3M4 7h16"/></svg></button>
              </div>
            </div>
            <button type="button" class="btn btn-add-shift" @click="addShiftType">
              <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><line x1="12" y1="5" x2="12" y2="19"/><line x1="5" y1="12" x2="19" y2="12"/></svg>
              添加班次
            </button>

            <!-- v774: 按组 + 时区覆盖班次时间段 -->
            <div class="ov-section">
              <div class="ov-title">
                按组覆盖
                <span class="ov-hint">
                  不同公司的班次口径可能不一样。比如 sl 的一天从 09:00 开始（C 是 24:00-09:00，跨到次日凌晨），
                  ig 的一天从 0 点开始（C 是 00:00-09:00，当天就上完）。这里只填不一样的那几个，其余走上面的全局定义。
                </span>
              </div>

              <div v-if="shiftOverrides.length" class="ov-list">
                <div v-for="o in shiftOverrides" :key="o.id" class="ov-item">
                  <span class="ov-grp">{{ o.group_name }}</span>
                  <span class="ov-tz">{{ o.timezone }}</span>
                  <span class="ov-code" :style="getShiftStyle(o.code)">{{ o.code }}</span>
                  <span class="ov-time">{{ o.time_range }}</span>
                  <span class="ov-name" v-if="o.name">称作「{{ o.name }}」</span>
                  <span class="ov-base">全局是 {{ overrideBaseTime(o.code) || '未定义' }}</span>
                  <button class="ov-del" title="删除，改回全局定义" @click="deleteShiftOverride(o)">
                    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><line x1="18" y1="6" x2="6" y2="18"/><line x1="6" y1="6" x2="18" y2="18"/></svg>
                  </button>
                </div>
              </div>
              <div v-else class="ov-empty">还没有任何覆盖，所有组都按上面的全局班次定义计算。</div>

              <div class="ov-form">
                <select class="cfg-input ov-in-grp" v-model="overrideForm.group_name">
                  <option value="">选组别</option>
                  <option v-for="g in groupOptions" :key="g" :value="g">{{ g }}</option>
                </select>
                <select class="cfg-input ov-in-tz" v-model="overrideForm.timezone">
                  <option value="">选时区</option>
                  <option v-for="tz in tzOptionList" :key="tz" :value="tz">{{ tz }}</option>
                </select>
                <select class="cfg-input ov-in-code" v-model="overrideForm.code">
                  <option value="">选班次</option>
                  <option v-for="s in shiftTypes" :key="s.code" :value="s.code">{{ s.code }} {{ s.name }}</option>
                </select>
                <input type="text" class="cfg-input ov-in-time" v-model="overrideForm.time_range"
                       :placeholder="overrideForm.code ? '全局: ' + overrideBaseTime(overrideForm.code) : '00:00-09:00'" />
                <input type="text" class="cfg-input ov-in-name" v-model="overrideForm.name" placeholder="这组的叫法(可空)" />
                <button type="button" class="btn btn-secondary ov-add" @click="saveShiftOverride">添加覆盖</button>
              </div>
            </div>
          </div>
          <div class="modal-footer">
            <button type="button" class="btn btn-secondary" @click="showConfigModal = false">取消</button>
            <button type="button" class="btn btn-primary" @click="saveShiftConfig">保存配置</button>
          </div>
        </div>
      </div>
    </Teleport>

    <!-- 员工时区弹窗 v772 -->
    <Teleport to="body">
      <div class="modal-overlay" :class="{ active: showTimezoneModal }">
        <div class="modal timezone-modal">
          <div class="modal-header">
            <h2>{{ timezoneEmployee?.name }} · 时区</h2>
            <button class="modal-close" @click="showTimezoneModal = false"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><line x1="18" y1="6" x2="6" y2="18"/><line x1="6" y1="6" x2="18" y2="18"/></svg></button>
          </div>
          <div class="modal-body" v-if="timezoneEmployee">
            <div class="tz-hist">
              <div class="tz-hist-title">时区历史</div>
              <div class="tz-hist-hint">
                时区带生效日期，改时区是「新增一段」而不是覆盖——历史月份的班次仍按当时的时区换算，不会被追溯改写。
                冬夏令时由时区自动处理（塞尔维亚 3 月底~10 月底比北京晚 6 小时，其余时候晚 7 小时），不需要手工调。
              </div>
              <div class="tz-hist-list">
                <div v-for="h in (timezoneEmployee.timezones || [])" :key="h.id" class="tz-hist-item">
                  <span class="tz-hist-from">{{ h.effective_from }} 起</span>
                  <span class="tz-hist-tz">{{ h.timezone }}</span>
                  <button v-if="canEditTimezone && (timezoneEmployee.timezones || []).indexOf(h) > 0"
                          class="tz-hist-del" title="删除这条" @click="deleteTimezoneEntry(h)">
                    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><line x1="18" y1="6" x2="6" y2="18"/><line x1="6" y1="6" x2="18" y2="18"/></svg>
                  </button>
                  <span v-else class="tz-hist-base" title="最早的一条是基线，删了之后早于其余记录的日期就没有时区可解析">基线</span>
                </div>
              </div>
            </div>

            <div class="tz-add" v-if="canEditTimezone">
              <div class="tz-hist-title">新增/修改一段</div>
              <div class="form-group">
                <label>时区</label>
                <select class="form-input" v-model="timezoneForm.timezone">
                  <option v-for="tz in tzOptionList" :key="tz" :value="tz">{{ tz }}</option>
                </select>
              </div>
              <div class="form-group">
                <label>从哪天起生效</label>
                <input type="date" class="form-input" v-model="timezoneForm.effective_from" />
                <div class="form-hint">填员工本人当地日期。同一天已有记录时视为修改那一条。</div>
              </div>
            </div>
            <div class="tz-add" v-else>
              <div class="form-hint">没有「设置员工时区」权限，只能查看。</div>
            </div>
          </div>
          <div class="modal-footer">
            <button type="button" class="btn btn-secondary" @click="showTimezoneModal = false">关闭</button>
            <button v-if="canEditTimezone" type="button" class="btn btn-primary" @click="saveTimezone">保存</button>
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
              <span class="sp-code" :style="{ background: s.color, color: shiftTextColor(s.color) }">{{ s.code }}</span>
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

/* 标签页切换 */
.page-tabs { display: flex; gap: 4px; margin-left: 24px; }
.tab-btn { display: inline-flex; align-items: center; gap: 6px; padding: 8px 16px; border-radius: 8px; border: 1px solid transparent; background: transparent; color: var(--text-secondary); cursor: pointer; font-size: 13px; font-weight: 500; transition: all 0.2s; }
.tab-btn svg { width: 16px; height: 16px; }
.tab-btn:hover { background: var(--bg-hover); color: var(--text-primary); }
.tab-btn.active { background: var(--primary); color: white; border-color: var(--primary); }

/* 联系人页面 */
.contacts-section { display: flex; flex-direction: column; gap: 16px; }
.contacts-toolbar { display: flex; justify-content: space-between; align-items: center; gap: 16px; flex-wrap: wrap; }
.contacts-toolbar .search-box { display: flex; align-items: center; gap: 8px; padding: 8px 14px; background: var(--bg-card); border: 1px solid var(--border-color); border-radius: 8px; min-width: 280px; }
.contacts-toolbar .search-box svg { width: 18px; height: 18px; color: var(--text-muted); flex-shrink: 0; }
.contacts-toolbar .search-box input { flex: 1; border: none; background: transparent; color: var(--text-primary); font-size: 14px; outline: none; }
.contacts-toolbar .search-box input::placeholder { color: var(--text-muted); }

.contacts-table-wrapper { background: var(--bg-card); border-radius: 12px; border: 1px solid var(--border-color); overflow: auto; max-height: calc(100vh - 240px); }
.contacts-table { width: 100%; border-collapse: collapse; }
.contacts-table th, .contacts-table td { padding: 12px 16px; text-align: left; border-bottom: 1px solid var(--border-color); }
.contacts-table thead { position: sticky; top: 0; z-index: 10; background: var(--bg-hover); }
.contacts-table th { font-weight: 600; font-size: 13px; color: var(--text-secondary); }
.contacts-table tbody tr { transition: background 0.15s; }
.contacts-table tbody tr:hover { background: var(--bg-hover); }
.contacts-table tbody tr:last-child td { border-bottom: none; }
.contacts-table .td-name { font-weight: 500; color: var(--text-primary); }
.contacts-table .td-phone { font-family: 'SF Mono', 'Monaco', 'Consolas', monospace; }
.contacts-table .phone-link { color: var(--primary); text-decoration: none; }
.contacts-table .phone-link:hover { text-decoration: underline; }
.contacts-table .td-remark { max-width: 200px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; color: var(--text-secondary); }
.contacts-table .th-actions { width: 100px; text-align: center; }
.contacts-table .td-actions { text-align: center; }
.contacts-table .btn-icon { width: 32px; height: 32px; border-radius: 6px; border: none; background: transparent; color: var(--text-secondary); cursor: pointer; display: inline-flex; align-items: center; justify-content: center; transition: all 0.15s; }
.contacts-table .btn-icon:hover { background: var(--bg-hover); color: var(--primary); }
.contacts-table .btn-icon.btn-danger:hover { background: rgba(239, 68, 68, 0.1); color: #ef4444; }
.contacts-table .btn-icon svg { width: 16px; height: 16px; }

/* 联系人弹窗 */
.contact-modal { width: 480px; max-width: 95vw; }

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
.legend-code { padding: 2px 6px; border-radius: 4px; font-weight: 700; color: #fff; font-size: 10px; min-width: 18px; text-align: center; }
.legend-name { font-weight: 500; color: var(--text-primary); white-space: nowrap; }
.legend-time { color: var(--text-muted); font-size: 10px; white-space: nowrap; }
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
/* v566: thead 整体加不透明 bg-card 作底色 + 每个 th 显式不透明背景。
   原因: var(--bg-hover) 在 dark-mode 下定义成 rgba(255,255,255,0.05) 半透明白,
   sticky 表头滚动时张三（第一行）的班次徽章会直接透到表头里，跟日期数字重叠. */
.schedule-table thead { position: sticky; top: 0; z-index: 20; background: var(--bg-card); }
.header-row th { background: var(--bg-card); font-weight: 600; font-size: 12px; height: 50px; }

/* Sticky 姓名列 - width必须与col-name-def一致 */
/* Sticky 姓名列 - 深色模式用不透明背景 */
.sticky-name { position: sticky; left: 0; z-index: 15; background: #1e2433; padding: 4px 6px !important; text-align: left; width: 120px; min-width: 120px; max-width: 120px; box-sizing: border-box; }
.header-row .sticky-name { z-index: 25; background: #141824; }
.td-name { background: #1a1f2e; }
.stats-row .sticky-name { background: #1a2744; }
.employee-row:hover .sticky-name { background: #252d3d; }

/* Sticky 职位列 - left必须等于姓名列宽度120px */
.sticky-role { position: sticky; left: 120px; z-index: 15; background: #1e2433; padding: 4px 6px !important; box-shadow: 4px 0 8px rgba(0,0,0,0.3); white-space: nowrap; width: 80px; min-width: 80px; max-width: 80px; box-sizing: border-box; }
.header-row .sticky-role { z-index: 25; background: #141824; }
.td-role { background: #1a1f2e; font-size: 11px; color: var(--text-secondary); white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
.stats-row .sticky-role { background: #1a2744; }
.employee-row:hover .sticky-role { background: #252d3d; }

/* 浅色模式固定列 */
body.light-mode .sticky-name { background: #f1f5f9; }
body.light-mode .header-row .sticky-name { background: #f8fafc; }
body.light-mode .td-name { background: #ffffff; }
body.light-mode .stats-row .sticky-name { background: #e0f2fe; }
body.light-mode .employee-row:hover .sticky-name { background: #f1f5f9; }

body.light-mode .sticky-role { background: #f1f5f9; box-shadow: 4px 0 8px rgba(0,0,0,0.1); }
body.light-mode .header-row .sticky-role { background: #f8fafc; }
body.light-mode .td-role { background: #ffffff; }
body.light-mode .stats-row .sticky-role { background: #e0f2fe; }
body.light-mode .employee-row:hover .sticky-role { background: #f1f5f9; }

/* 日期表头 */
.th-day { padding: 4px 2px !important; width: 38px; min-width: 38px; max-width: 38px; box-sizing: border-box; }
/* v566: weekend / warning 也改成 bg-card 底色 (bg-hover 在 dark-mode 是半透明) */
/* 周末改柔和中性(不再用红);红/琥珀只留给缺班告警 */
.th-day.weekend { background-color: var(--bg-card); background-image: linear-gradient(rgba(100, 116, 139, 0.10), rgba(100, 116, 139, 0.10)); }
.th-day.warning { background-color: var(--bg-card); background-image: linear-gradient(rgba(251, 191, 36, 0.25), rgba(251, 191, 36, 0.25)); box-shadow: inset 0 -2px 0 rgba(251, 191, 36, 0.7); }
.day-num { font-size: 12.5px; font-weight: 600; line-height: 1.3; }
.day-week { font-size: 10px; color: var(--text-muted); line-height: 1.2; }

/* 今天高亮列(优先级高于周末/告警) */
.th-day.today { background-color: var(--bg-card); background-image: linear-gradient(rgba(59, 130, 246, 0.18), rgba(59, 130, 246, 0.18)); color: var(--primary); box-shadow: inset 0 -2px 0 var(--primary); }
.th-day.today .day-week { color: var(--primary); }
.th-day.today .day-num::after { content: '今天'; display: block; font-size: 8.5px; font-weight: 700; letter-spacing: .04em; }
.td-shift.today { background: rgba(59, 130, 246, 0.06); box-shadow: inset 1px 0 0 rgba(59,130,246,.35), inset -1px 0 0 rgba(59,130,246,.35); }
.td-shift.today:hover { background: rgba(59, 130, 246, 0.15) !important; }
.td-stats.today { background: rgba(59, 130, 246, 0.10); }

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
.td-shift.weekend { background: rgba(100, 116, 139, 0.05); }

/* 班次徽章:圆角+内描边+轻投影,更有质感 */
.shift-badge { display: inline-flex; align-items: center; justify-content: center; width: 32px; height: 24px; border-radius: 7px; font-size: 11px; font-weight: 700; box-shadow: inset 0 0 0 1px rgba(255,255,255,0.28), 0 1px 1.5px rgba(15,23,42,0.18); }

.stats-row td { background: linear-gradient(180deg, rgba(59,130,246,0.12), rgba(59,130,246,0.05)); border-top: 2px solid var(--primary) !important; }
.stats-label { font-size: 12px; font-weight: 600; text-align: center !important; color: var(--primary); }
.td-stats { font-size: 9px; padding: 4px 2px !important; }
.td-stats.warning { background: rgba(251, 191, 36, 0.15); }
.stats-mini { display: flex; flex-direction: column; gap: 1px; line-height: 1.15; }
/* v567: 班次统计颜色直接用 shiftTypes 的 color (内联 :style) */
.stats-mini .stat-item { font-weight: 600; white-space: nowrap; }

/* v569: 「本月统计」列样式已删除（迁移到 /system/schedule-analytics 独立页面） */

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

/* 导出 Excel 弹窗 (v763) */
.export-modal {
  width: 480px;
}
.export-options {
  display: flex;
  flex-direction: column;
  gap: 10px;
  margin-bottom: 14px;
}
.export-option {
  display: grid;
  grid-template-columns: auto 1fr;
  grid-template-rows: auto auto;
  column-gap: 10px;
  align-items: center;
  padding: 12px 14px;
  border: 1px solid var(--border-color);
  border-radius: 8px;
  cursor: pointer;
  transition: all 0.15s;
}
.export-option:hover {
  border-color: rgba(59,130,246,0.4);
}
.export-option.active {
  border-color: var(--primary);
  background: rgba(59,130,246,0.06);
}
.export-option input[type="radio"] {
  grid-row: 1 / 3;
  margin: 0;
  cursor: pointer;
}
.export-option-title {
  font-size: 13px;
  font-weight: 600;
  color: var(--text-primary);
}
.export-option-desc {
  font-size: 12px;
  color: var(--text-muted);
}
.export-range {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 12px;
  margin-bottom: 12px;
}
.form-hint {
  margin-top: 6px;
  font-size: 12px;
  line-height: 1.5;
  color: var(--text-muted);
}

/* 班次配置弹窗 */
.config-modal {
  width: 940px;
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
  grid-template-columns: 60px 80px 1fr 1fr 100px 60px 40px;
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
.cfg-col-time-en { }
.cfg-col-color { text-align: center; }
.cfg-col-duty { text-align: center; }
.cfg-col-action { text-align: center; }
.config-item {
  display: grid;
  grid-template-columns: 60px 80px 1fr 1fr 100px 60px 40px;
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
.cfg-input-time-en { }
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

/* ===== v772 跨时区排班 ===== */
.tz-toolbar {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 10px;
  padding: 8px 12px;
  margin-bottom: 8px;
  background: var(--bg-card);
  border: 1px solid var(--border-color);
  border-radius: 8px;
  font-size: 12px;
}
.tz-toolbar-label {
  display: inline-flex;
  align-items: center;
  gap: 5px;
  color: var(--text-secondary);
  font-weight: 600;
}
.tz-toolbar-label svg { width: 14px; height: 14px; }
.tz-seg {
  display: inline-flex;
  border: 1px solid var(--border-color);
  border-radius: 6px;
  overflow: hidden;
}
.tz-seg-btn {
  padding: 4px 12px;
  border: none;
  background: var(--bg-card);
  color: var(--text-secondary);
  font-size: 12px;
  cursor: pointer;
  border-right: 1px solid var(--border-color);
  transition: all 0.15s;
}
.tz-seg-btn:last-child { border-right: none; }
.tz-seg-btn:hover { background: var(--bg-hover); }
.tz-seg-btn.active { background: var(--primary); color: #fff; }
.tz-custom {
  padding: 4px 8px;
  border: 1px solid var(--border-color);
  border-radius: 6px;
  background: var(--bg-input);
  color: var(--text-primary);
  font-size: 12px;
  max-width: 160px;
}
.tz-readonly-hint {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  flex: 1 1 320px;
  min-width: 260px;
  padding: 4px 10px;
  border-radius: 6px;
  background: rgba(245, 158, 11, 0.16);
  color: #fbbf24;
  line-height: 1.5;
}
.tz-readonly-hint svg { width: 14px; height: 14px; flex-shrink: 0; }
.tz-coverage-toggle {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  color: var(--text-secondary);
  cursor: pointer;
  margin-left: auto;
  white-space: nowrap;
}
.tz-gap-badge {
  padding: 2px 7px;
  border-radius: 10px;
  background: rgba(239, 68, 68, 0.2);
  color: #f87171;
  font-weight: 600;
}
.legend-tz-note {
  color: var(--text-muted);
  font-size: 11px;
  align-self: center;
}

/* 姓名列的时区徽标。姓名列固定 120px，徽标必须排在姓名下面一行，
   横排会把 .emp-name 的 flex 宽度挤成 0，名字整列消失 */
.emp-name-wrap {
  display: flex;
  flex-direction: column;
  align-items: flex-start;
  gap: 1px;
  flex: 1;
  min-width: 0;
  line-height: 1.25;
}
.emp-name-wrap .emp-name { flex: none; max-width: 100%; }
.tz-badge {
  flex-shrink: 0;
  padding: 0 4px;
  border: 1px solid var(--border-color);
  border-radius: 4px;
  background: var(--bg-hover);
  color: var(--text-muted);
  font-size: 9px;
  line-height: 1.5;
  cursor: pointer;
  white-space: nowrap;
}
.tz-badge:hover:not(:disabled) { border-color: var(--primary); color: var(--primary); }
.tz-badge:disabled { cursor: default; }
/* 跟当前对照时区不一致的人标出来，一眼看出谁不在本地 */
.tz-badge.foreign {
  background: rgba(59, 130, 246, 0.2);
  border-color: rgba(59, 130, 246, 0.4);
  color: #93c5fd;
  font-weight: 600;
}
/* 当月内时区（或冬夏令时偏移）发生过变化，用琥珀色标出来 */
.tz-badge.changed {
  background: rgba(245, 158, 11, 0.2);
  border-color: rgba(245, 158, 11, 0.5);
  color: #fbbf24;
  font-weight: 700;
}

/* 一格两个班次（跨时区视角下前一天的晚班和当天的班落到同一天）。
   必须上下堆叠：横排时两个徽章的最小内容宽度会把 38px 的日期列撑到 61px，
   整表跟着变宽，一屏少显示好几天。 */
.td-shift.multi-shift { padding: 1px 0 !important; }
.td-shift.multi-shift .shift-badge {
  display: flex;
  width: 30px;
  height: 15px;
  margin: 1px auto;
  border-radius: 4px;
  font-size: 9px;
}

/* 覆盖空档条 */
.coverage-row td { background: var(--bg-card); }
.coverage-label {
  font-size: 11px;
  color: var(--text-secondary);
  font-weight: 600;
}
.td-coverage { cursor: pointer; padding: 3px 2px !important; }
/* 这两行是唯一能展开单日拆解的入口，悬停要给足反馈，否则没人知道可以点 */
.td-coverage:hover, .td-gap:hover { background: var(--bg-hover); outline: 1px solid var(--primary); outline-offset: -1px; }
.td-coverage.active { outline: 2px solid var(--primary); outline-offset: -2px; }
.row-hint {
  display: block; font-size: 9px; font-weight: 400; color: var(--primary);
  opacity: .85; line-height: 1.2; margin-top: 1px;
}
.coverage-bar {
  display: flex;
  height: 10px;
  gap: 0;
  border-radius: 2px;
  overflow: hidden;
}
.coverage-hour { flex: 1 1 0; display: block; }
.coverage-hour.ok { background: #22c55e; }
/* 只有一个人在岗：不是空档但也没有冗余，单独一档色 */
.coverage-hour.thin { background: #a3e635; }
.coverage-hour.gap { background: #ef4444; }

/* v777: 单日拆解面板 */
.day-detail {
  margin-top: 10px; border: 1px solid var(--border-color); border-radius: 8px;
  background: var(--bg-card); overflow: hidden;
}
.dd-head {
  display: flex; align-items: center; gap: 12px; padding: 9px 14px;
  border-bottom: 1px solid var(--border-color); background: var(--bg-primary); font-size: 12.5px;
}
.dd-date { font-weight: 650; color: var(--text-primary); font-variant-numeric: tabular-nums; }
.dd-gaps { color: #f87171; font-weight: 600; }
.dd-ok { color: #4ade80; font-weight: 600; }
.dd-close { margin-left: auto; border: none; background: transparent; color: var(--text-muted); cursor: pointer; padding: 2px; }
.dd-close svg { width: 14px; height: 14px; }

.dd-sec { padding: 11px 14px; }
.dd-sec + .dd-sec { border-top: 1px solid var(--border-color); }
.dd-title { font-size: 12px; font-weight: 650; color: var(--text-primary); margin-bottom: 8px; }
.dd-sub { font-size: 11px; font-weight: 400; color: var(--text-muted); margin-left: 8px; }
.dd-empty { font-size: 12px; color: var(--text-muted); }

.dd-ruler { display: flex; margin-left: 104px; margin-right: 240px; margin-bottom: 3px; }
.dd-tick { flex: 1; font-size: 9px; color: var(--text-muted); font-variant-numeric: tabular-nums; }
.dd-tick:last-child { flex: 0; }
.dd-row { display: flex; align-items: center; gap: 8px; margin-bottom: 3px; }
.dd-who { width: 96px; flex-shrink: 0; font-size: 11.5px; color: var(--text-primary); display: flex; align-items: center; gap: 5px; }
.dd-code { display: inline-flex; align-items: center; justify-content: center; width: 22px; height: 15px; border-radius: 3px; font-size: 8.5px; font-weight: 700; font-style: normal; }
.dd-track { flex: 1; height: 14px; background: var(--bg-primary); border-radius: 3px; position: relative; overflow: hidden; }
.dd-bar { position: absolute; top: 0; bottom: 0; background: #3a84ff; border-radius: 3px; }
.dd-bar.prev { background: #64748b; }
.dd-bar.cutl { border-top-left-radius: 0; border-bottom-left-radius: 0; }
.dd-bar.cutr { border-top-right-radius: 0; border-bottom-right-radius: 0; }
.dd-meta { width: 232px; flex-shrink: 0; font-size: 10.5px; color: var(--text-muted); display: flex; gap: 5px; flex-wrap: wrap; align-items: baseline; }
.dd-meta b { color: var(--text-primary); font-weight: 600; font-variant-numeric: tabular-nums; }
.dd-meta em { font-style: normal; opacity: .8; }
.dd-prevtag { background: rgba(100,116,139,.25); border-radius: 3px; padding: 0 4px; }

.dd-hours { display: flex; align-items: center; gap: 2px; margin-top: 7px; margin-left: 104px; margin-right: 240px; }
.dd-hours-label { position: absolute; margin-left: -100px; font-size: 10.5px; color: var(--text-secondary); }
.dd-hour {
  flex: 1; text-align: center; font-size: 8.5px; font-style: normal;
  line-height: 15px; border-radius: 2px; color: #0b1220; font-variant-numeric: tabular-nums; cursor: help;
}
.dd-hour.ok { background: #22c55e; }
.dd-hour.thin { background: #eab308; }
.dd-hour.zero { background: #ef4444; color: #fff; font-weight: 700; }

.dd-line { display: flex; align-items: center; gap: 8px; font-size: 11.5px; margin-bottom: 3px; }
.dd-line .dd-meta { width: auto; flex: 1; }
.dd-spill { background: rgba(234,179,8,.18); color: #eab308; border-radius: 3px; padding: 0 5px; font-weight: 600; }

.dd-chg { display: flex; align-items: center; gap: 10px; font-size: 11.5px; padding: 3px 0; }
.dd-chg + .dd-chg { border-top: 1px dashed var(--border-color); }
.dd-chg.start { color: var(--text-muted); }
.dd-chg-t { width: 118px; flex-shrink: 0; font-weight: 600; color: var(--text-primary); font-variant-numeric: tabular-nums; }
.dd-chg-t em { font-style: normal; font-weight: 400; color: var(--text-muted); font-size: 10.5px; margin-left: 3px; }
.dd-chg-body { flex: 1; display: flex; gap: 10px; flex-wrap: wrap; }
.dd-off { color: #f87171; }
.dd-on { color: #4ade80; }
.dd-noone { color: #f87171; font-weight: 700; }
.dd-chg-n { color: var(--text-secondary); font-variant-numeric: tabular-nums; }
.dd-chg-n.zero { color: #f87171; font-weight: 700; }
.dd-chg.danger { background: rgba(239,68,68,.10); }

body.light-mode .dd-gaps, body.light-mode .dd-off, body.light-mode .dd-noone,
body.light-mode .dd-chg-n.zero { color: #dc2626; }
body.light-mode .dd-ok, body.light-mode .dd-on { color: #15803d; }
body.light-mode .dd-spill { background: rgba(161,98,7,.14); color: #a16207; }
body.light-mode .dd-hour.ok { background: #16a34a; color: #fff; }
body.light-mode .dd-hour.thin { background: #ca8a04; color: #fff; }

/* v774: 班次按组覆盖 */
.ov-section { margin-top: 16px; border-top: 1px solid var(--border-color); padding-top: 13px; }
.ov-title { font-size: 13px; font-weight: 650; color: var(--text-primary); margin-bottom: 8px; }
.ov-hint { display: block; font-size: 11.5px; font-weight: 400; color: var(--text-muted); line-height: 1.65; margin-top: 3px; }
.ov-list { display: flex; flex-direction: column; gap: 5px; margin-bottom: 10px; }
.ov-item {
  display: flex; align-items: center; gap: 9px; flex-wrap: wrap;
  padding: 6px 10px; border: 1px solid var(--border-color); border-radius: 6px;
  background: var(--bg-primary); font-size: 12px;
}
.ov-grp { font-weight: 700; color: var(--text-primary); }
.ov-tz { color: var(--text-secondary); font-size: 11px; }
.ov-code {
  display: inline-flex; align-items: center; justify-content: center;
  width: 26px; height: 18px; border-radius: 4px; font-size: 9px; font-weight: 700;
}
.ov-time { font-weight: 600; color: var(--text-primary); font-variant-numeric: tabular-nums; }
.ov-name { color: var(--text-secondary); font-size: 11px; }
.ov-base { color: var(--text-muted); font-size: 10.5px; margin-left: auto; }
.ov-del { border: none; background: transparent; color: var(--text-muted); cursor: pointer; padding: 2px; }
.ov-del:hover { color: #ef4444; }
.ov-del svg { width: 13px; height: 13px; }
.ov-empty { font-size: 12px; color: var(--text-muted); margin-bottom: 10px; }
.ov-form { display: flex; gap: 6px; flex-wrap: wrap; align-items: center; }
/* .cfg-input 自带 width:100%，不覆盖的话这几个控件会各占一整行 */
.ov-form .cfg-input { width: auto; flex: 0 1 auto; padding: 6px 8px; font-size: 12px; }
.ov-in-grp, .ov-in-code { width: 108px; }
.ov-in-tz { width: 168px; }
.ov-in-time { width: 132px; }
.ov-in-name { width: 128px; }
.ov-add { flex-shrink: 0; padding: 6px 12px; font-size: 12px; }

.legend-override-note {
  font-size: 11px; color: #eab308; align-self: center;
  background: rgba(234, 179, 8, 0.14); border-radius: 4px; padding: 2px 8px; cursor: help;
}
.lon-detail { color: var(--text-muted); margin-left: 5px; }

/* v776: 组内的时区小节。比组标题浅一级，靠左缩进表示层级 */
.tz-section-row td { background: var(--bg-primary); }
.tz-section-cell {
  text-align: left !important;
  padding: 3px 10px 3px 26px !important;
  font-size: 10.5px;
  color: var(--text-muted);
  position: relative;
}
/* 缩进处画一小段竖线，表明这是组的下一层 */
.tz-section-cell::before {
  content: '';
  position: absolute;
  left: 14px; top: 50%; width: 7px; height: 1px;
  background: var(--border-color);
}
.tzs-name { font-weight: 700; color: var(--text-secondary); margin-right: 6px; }
.tzs-full { margin-right: 6px; opacity: 0.75; }
.tzs-off {
  font-variant-numeric: tabular-nums; font-weight: 600;
  color: var(--primary); margin-right: 8px;
}
.tzs-count { opacity: 0.8; }
.group-count { font-size: 10.5px; font-weight: 400; color: var(--text-muted); margin-left: 8px; }
.group-multi-tz {
  font-size: 10px; font-weight: 600; margin-left: 8px;
  color: #eab308; background: rgba(234, 179, 8, 0.16);
  border-radius: 3px; padding: 0 6px;
}

/* v775: 当前在岗条 */
.oncall-bar {
  border: 1px solid var(--border-color);
  border-radius: 8px;
  background: var(--bg-card);
  margin-bottom: 8px;
  overflow: hidden;
}
.oc-clocks {
  display: flex; align-items: center; gap: 14px; flex-wrap: wrap;
  padding: 7px 14px; border-bottom: 1px solid var(--border-color);
  background: var(--bg-primary); font-size: 12px;
}
.oc-title { font-weight: 650; color: var(--text-primary); font-size: 12.5px; }
.oc-clock { color: var(--text-secondary); font-variant-numeric: tabular-nums; }
.oc-clock b { color: var(--text-primary); font-weight: 600; margin-right: 4px; }
.oc-body { display: grid; grid-template-columns: repeat(auto-fit, minmax(320px, 1fr)); gap: 1px; background: var(--border-color); }
.oc-col { background: var(--bg-card); padding: 9px 13px; display: flex; flex-direction: column; gap: 5px; }
.oc-col-title { font-size: 11.5px; font-weight: 650; color: var(--text-secondary); }
.oc-empty { font-size: 12px; color: var(--text-muted); }
.oc-person { display: flex; align-items: center; gap: 6px; font-size: 12px; flex-wrap: wrap; }
.oc-group { font-size: 10.5px; color: var(--text-muted); min-width: 32px; }
.oc-shift {
  display: inline-flex; align-items: center; justify-content: center;
  width: 24px; height: 17px; border-radius: 4px; font-size: 9px; font-weight: 700;
}
.oc-name { font-weight: 600; color: var(--text-primary); }
.oc-leader {
  font-size: 9px; font-weight: 700; padding: 0 4px; border-radius: 3px;
  background: rgba(234, 179, 8, 0.18); color: #eab308;
}
.oc-tz { font-size: 10.5px; color: var(--text-muted); font-variant-numeric: tabular-nums; }
.oc-left { font-size: 11px; color: var(--text-secondary); margin-left: auto; font-variant-numeric: tabular-nums; }
.oc-change { display: flex; flex-direction: column; gap: 2px; padding: 4px 0; }
.oc-change + .oc-change { border-top: 1px dashed var(--border-color); }
.oc-change-head { display: flex; align-items: baseline; gap: 6px; font-size: 12px; flex-wrap: wrap; }
.oc-change-head b.oc-at { color: var(--text-primary); font-variant-numeric: tabular-nums; font-weight: 600; }
.oc-sep { color: var(--text-muted); font-weight: 400; margin-right: 4px; }
.oc-in { font-size: 11px; color: var(--text-muted); }
.oc-flow { font-size: 11.5px; display: flex; align-items: flex-start; gap: 6px; }
.oc-flow.off { color: #f87171; }
.oc-flow.on { color: #4ade80; }
.oc-flow-tag { flex-shrink: 0; font-weight: 600; }
.oc-flow-groups { display: flex; flex-direction: column; gap: 1px; }
.oc-flow-line { display: flex; align-items: center; gap: 6px; flex-wrap: wrap; }
.oc-fg { color: var(--text-secondary); font-weight: 600; min-width: 30px; }
.oc-ftz {
  color: var(--text-muted); font-variant-numeric: tabular-nums;
  font-size: 10.5px; min-width: 96px;
}
.oc-mini { color: var(--text-primary); display: inline-flex; align-items: center; gap: 3px; }
.oc-mini i { font-style: normal; font-size: 9px; color: var(--text-muted); }

/* v775: 组长标记。组长排的班不计入每日 A/B/C 完整性检查 */
.emp-name.leader { color: #eab308; }
.leader-dot {
  display: inline-block; width: 4px; height: 4px; border-radius: 50%;
  background: #eab308; margin-left: 3px; vertical-align: 2px;
}

/* v773: 有空档的日期整列标红，横向扫一眼就能定位 */
.gap-col { background: rgba(239, 68, 68, 0.15) !important; }
.th-day.gap-col { box-shadow: inset 0 -2.5px 0 #ef4444; }
.th-day.gap-col .day-num { color: #ef4444; }

/* v773: 空档时段常驻行。列宽 38px，用 06-09 缩写 */
.gap-row td { background: var(--bg-card); }
.gap-label { font-size: 11px; font-weight: 600; color: #ef4444; }
.gap-label-tz { font-weight: 400; color: var(--text-muted); font-size: 9.5px; margin-left: 4px; }
.td-gap { cursor: pointer; padding: 2px 1px !important; line-height: 1.3; }
.td-gap:hover { background: var(--bg-hover); }
.gap-time {
  font-size: 9px; font-weight: 700; color: #ef4444;
  letter-spacing: -0.02em; font-variant-numeric: tabular-nums;
}
.gap-dot { color: var(--text-muted); font-size: 10px; }

/* v773: 空档汇总 */
.gap-summary {
  margin-top: 10px;
  border: 1px solid var(--border-color);
  border-radius: 8px;
  background: var(--bg-card);
  overflow: hidden;
}
.gap-summary-head {
  display: flex; align-items: baseline; gap: 10px; flex-wrap: wrap;
  padding: 9px 14px; border-bottom: 1px solid var(--border-color);
  background: var(--bg-primary);
}
.gs-title { font-size: 13px; font-weight: 650; color: var(--text-primary); }
.gs-sub { font-size: 11.5px; color: var(--text-muted); }
.gap-summary-head.clickable { cursor: pointer; user-select: none; }
.gap-summary-head.clickable:hover { background: var(--bg-hover); }
.gs-caret { width: 13px; height: 13px; color: var(--text-muted); transition: transform .18s; flex-shrink: 0; }
.gs-caret.open { transform: rotate(90deg); }
.gs-count {
  font-size: 11.5px; font-weight: 600; color: #f87171;
  background: rgba(239, 68, 68, 0.14); border-radius: 4px; padding: 1px 8px;
}
body.light-mode .gs-count { background: rgba(220, 38, 38, 0.11); color: #dc2626; }
@media (prefers-reduced-motion: reduce) { .gs-caret { transition: none; } }

.gap-pattern { padding: 13px 15px; display: flex; flex-direction: column; gap: 10px; }
.gap-pattern + .gap-pattern { border-top: 1px solid var(--border-color); }
.gp-head { display: flex; align-items: center; gap: 9px; }
.gp-sev {
  font-size: 11px; font-weight: 700; padding: 2px 9px; border-radius: 4px;
  background: #dc2626; color: #fff;
}
.gp-sev.partial { background: #eab308; color: #4a3200; }
.gp-count { font-size: 12px; color: var(--text-secondary); }
.gp-count b { font-size: 15px; color: var(--text-primary); font-weight: 650; }

.gp-zones {
  display: grid; grid-template-columns: repeat(auto-fit, minmax(215px, 1fr));
  gap: 1px; background: var(--border-color);
  border: 1px solid var(--border-color); border-radius: 6px; overflow: hidden;
}
.gp-zone { background: var(--bg-primary); padding: 8px 11px; display: flex; align-items: baseline; gap: 8px; }
.gp-zone.basis { background: var(--bg-hover); }
.gz-name { font-size: 11px; color: var(--text-secondary); min-width: 68px; display: flex; flex-direction: column; gap: 1px; }
.gz-groups { font-style: normal; font-size: 10px; font-weight: 600; color: var(--primary); }
.gz-time { font-size: 15px; font-weight: 600; color: var(--text-primary); font-variant-numeric: tabular-nums; }
.gz-note { font-size: 10px; color: var(--text-muted); }

.gp-days { display: flex; flex-wrap: wrap; gap: 5px; align-items: center; }
.gp-days-label { font-size: 11px; color: var(--text-secondary); }
.gp-daychip {
  font-size: 11px; padding: 1px 7px; border-radius: 4px; cursor: pointer;
  background: rgba(239, 68, 68, 0.12); color: #ef4444;
  border: 1px solid rgba(239, 68, 68, 0.35); font-weight: 600;
  font-variant-numeric: tabular-nums;
}
.gp-daychip:hover, .gp-daychip.active { background: #ef4444; color: #fff; }

.gp-cause { border-left: 2px solid var(--border-color); padding-left: 11px; display: flex; flex-direction: column; gap: 5px; }
.gpc-title { font-size: 11.5px; color: var(--text-secondary); font-weight: 600; }
.gpc-item { display: flex; align-items: center; gap: 7px; flex-wrap: wrap; font-size: 11.5px; }
.gpc-date {
  font-weight: 700; color: var(--text-primary); min-width: 34px;
  font-variant-numeric: tabular-nums;
}
.gpc-shift {
  display: inline-flex; align-items: center; justify-content: center;
  width: 24px; height: 17px; border-radius: 4px; font-size: 9px; font-weight: 700;
}
.gpc-desc { color: var(--text-secondary); }
.gpc-who { color: var(--text-muted); font-size: 11px; }
.gpc-who.none { color: #ef4444; font-weight: 600; }

/* v774: 按组给补法 */
.gp-fix { border-left: 2px solid var(--primary); padding-left: 11px; display: flex; flex-direction: column; gap: 7px; }
.gpf-title { font-size: 11.5px; font-weight: 600; color: var(--text-primary); display: flex; align-items: baseline; gap: 8px; flex-wrap: wrap; }
.gpf-hint { font-size: 11px; font-weight: 400; color: var(--text-muted); }
.gpf-grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(330px, 1fr)); gap: 8px; }
.gpf-group {
  border: 1px solid var(--border-color); border-radius: 6px;
  background: var(--bg-primary); padding: 8px 10px;
  display: flex; flex-direction: column; gap: 6px;
}
.gpf-ghead { display: flex; align-items: baseline; gap: 8px; flex-wrap: wrap; }
.gpf-gname { font-size: 12.5px; font-weight: 700; color: var(--text-primary); }
.gpf-gtz { font-size: 10.5px; color: var(--text-muted); }
.gpf-nofix { font-size: 10.5px; color: #eab308; font-weight: 600; }
.gpf-opt { display: flex; align-items: center; gap: 6px; flex-wrap: wrap; font-size: 11.5px; line-height: 1.6; }
.gpf-date { font-weight: 700; color: var(--text-primary); font-variant-numeric: tabular-nums; }
.gpf-shift {
  display: inline-flex; align-items: center; justify-content: center;
  width: 24px; height: 17px; border-radius: 4px; font-size: 9px; font-weight: 700;
}
.gpf-desc { color: var(--text-secondary); }
.gpf-bytz {
  font-size: 10px; font-weight: 600; color: var(--primary);
  background: rgba(59, 130, 246, 0.14); border-radius: 3px; padding: 0 5px;
}
.gpf-basis { color: var(--text-muted); font-variant-numeric: tabular-nums; }
.gpf-verdict { font-weight: 600; font-size: 11px; padding: 0 6px; border-radius: 4px; }
.gpf-verdict.ok { color: #22c55e; background: rgba(34, 197, 94, 0.14); }
.gpf-verdict.part { color: #eab308; background: rgba(234, 179, 8, 0.14); }
.gpf-already { font-size: 10.5px; color: var(--text-muted); }

.gp-profile { border-top: 1px solid var(--border-color); padding: 12px 15px 14px; }
.gpp-title { font-size: 11.5px; color: var(--text-secondary); font-weight: 600; display: flex; align-items: center; gap: 12px; flex-wrap: wrap; }
.gpp-legend { display: flex; align-items: center; gap: 6px; font-weight: 400; color: var(--text-muted); }
.gpp-legend .lg { width: 9px; height: 9px; border-radius: 2px; display: inline-block; margin-left: 6px; }
.gpp-legend .lg.ok { background: #22c55e; }
.gpp-legend .lg.thin { background: #eab308; }
.gpp-legend .lg.gap { background: #ef4444; }
.gpp-band { display: grid; grid-template-columns: repeat(24, 1fr); gap: 2px; align-items: end; margin-top: 8px; }
.gpp-col { display: flex; flex-direction: column; align-items: stretch; gap: 2px; cursor: default; }
.gpp-avg { font-size: 8.5px; color: var(--text-muted); text-align: center; font-variant-numeric: tabular-nums; }
.gpp-bar { background: #22c55e; border-radius: 2px 2px 0 0; display: block; }
.gpp-col.warn .gpp-bar { background: #eab308; }
.gpp-col.bad .gpp-bar { background: #ef4444; }
.gpp-tick { font-size: 8.5px; color: var(--text-muted); text-align: center; height: 11px; font-variant-numeric: tabular-nums; }
.gpp-col.bad .gpp-tick { color: #ef4444; font-weight: 600; }


/* 时区弹窗 */
.timezone-modal { max-width: 480px; }
.tz-hist-title { font-size: 13px; font-weight: 600; color: var(--text-primary); margin-bottom: 6px; }
.tz-hist-hint {
  font-size: 12px;
  color: var(--text-muted);
  line-height: 1.6;
  margin-bottom: 10px;
}
.tz-hist-list { display: flex; flex-direction: column; gap: 6px; margin-bottom: 18px; }
.tz-hist-item {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 6px 10px;
  border: 1px solid var(--border-color);
  border-radius: 6px;
  font-size: 12px;
  background: var(--bg-primary);
}
.tz-hist-from { color: var(--text-secondary); }
.tz-hist-tz { color: var(--text-primary); font-weight: 600; }
.tz-hist-base { margin-left: auto; color: var(--text-muted); font-size: 11px; }
.tz-hist-del {
  margin-left: auto;
  border: none;
  background: transparent;
  color: var(--text-muted);
  cursor: pointer;
  padding: 2px;
}
.tz-hist-del:hover { color: #dc2626; }
.tz-hist-del svg { width: 13px; height: 13px; }
.tz-add { border-top: 1px solid var(--border-color); padding-top: 14px; }

/* 浅色主题覆盖。
   ⚠️ 本项目的主题是 body.light-mode 这个 class 切的、默认深色，
   不是跟随系统的 prefers-color-scheme——用媒体查询会导致
   「系统浅色 + 应用深色」和「系统深色 + 应用浅色」两种组合下配色全错。 */
body.light-mode .tz-readonly-hint { background: rgba(245, 158, 11, 0.12); color: #b45309; }
body.light-mode .tz-gap-badge { background: rgba(239, 68, 68, 0.14); color: #dc2626; }
body.light-mode .tz-badge.foreign { background: rgba(59, 130, 246, 0.12); color: #2563eb; }
body.light-mode .tz-badge.changed { background: rgba(180, 83, 9, 0.12); border-color: rgba(180, 83, 9, 0.4); color: #b45309; }
body.light-mode .emp-name.leader { color: #a16207; }
body.light-mode .leader-dot { background: #a16207; }
body.light-mode .oc-leader { background: rgba(161, 98, 7, 0.14); color: #a16207; }
body.light-mode .oc-flow.off { color: #dc2626; }
body.light-mode .oc-flow.on { color: #15803d; }
body.light-mode .group-multi-tz { background: rgba(161, 98, 7, 0.14); color: #a16207; }
body.light-mode .legend-override-note { background: rgba(161, 98, 7, 0.13); color: #a16207; }
body.light-mode /* 浅色下 --bg-primary 是透明的，小节行会和白色员工行糊在一起，必须给实底 */
body.light-mode .tz-section-row td { background: #f8fafc; }
body.light-mode .tz-section-cell { color: #64748b; }
body.light-mode .tzs-name { color: #475569; }
body.light-mode .gap-col { background: rgba(220, 38, 38, 0.07) !important; }
body.light-mode .gap-label,
body.light-mode .gap-time,
body.light-mode .th-day.gap-col .day-num,
body.light-mode .gpc-who.none { color: #dc2626; }
body.light-mode .th-day.gap-col { box-shadow: inset 0 -2.5px 0 #dc2626; }
body.light-mode .gp-daychip { background: rgba(220, 38, 38, 0.1); color: #dc2626; border-color: rgba(220, 38, 38, 0.3); }
body.light-mode .gp-daychip:hover, body.light-mode .gp-daychip.active { background: #dc2626; color: #fff; }
/* 浅底上 #22c55e / #eab308 的对比度只有 2 出头，等于浅色写浅色，必须压深 */
body.light-mode .gpf-verdict.ok { color: #15803d; background: rgba(21, 128, 61, 0.12); }
body.light-mode .gpf-verdict.part { color: #a16207; background: rgba(161, 98, 7, 0.13); }
body.light-mode .gpf-nofix { color: #a16207; }
body.light-mode .gp-sev.partial { background: #ca8a04; color: #fff; }
</style>
