/**
 * 跨时区排班的时区换算 (v772)
 *
 * 为什么整个换算只在这里实现一份：
 *   浏览器自带 IANA 时区库（Intl），冬夏令时规则会随系统更新，比自己维护偏移表可靠。
 *   后端只负责存和校验时区名，不做换算，避免前后端两套实现慢慢分叉。
 *
 * 三个必须守住的规则：
 *   1. 时区一律用 IANA 名（Europe/Belgrade），不用 UTC+2 这种固定偏移。
 *      塞尔维亚 3 月底~10 月底是 CEST(UTC+2)、其余是 CET(UTC+1)，跟北京差 6 或 7 小时来回切。
 *   2. 班次结束时刻 = 开始时刻 + 时长（绝对时间相加），不能把起止时刻各自换算。
 *      跨夏令时切换点的班次分别换算，时长会凭空多出或少掉 1 小时。
 *   3. 时区名解析不了必须 warn + 回落，不能静默当成默认时区。
 */

export const DEFAULT_TIMEZONE = 'Asia/Shanghai'

const formatterCache = new Map()
const badTimezones = new Set()

/** 拿一个时区的 Intl 格式化器；时区名非法时回落默认时区并告警（每个坏值只告警一次） */
function formatterFor(timeZone) {
  const tz = timeZone || DEFAULT_TIMEZONE
  if (formatterCache.has(tz)) return formatterCache.get(tz)
  let f
  try {
    f = new Intl.DateTimeFormat('en-US', {
      timeZone: tz,
      hour12: false,
      year: 'numeric', month: '2-digit', day: '2-digit',
      hour: '2-digit', minute: '2-digit', second: '2-digit'
    })
    // 构造成功不代表时区有效，某些实现要真的格式化一次才报错
    f.format(new Date())
  } catch (e) {
    if (!badTimezones.has(tz)) {
      badTimezones.add(tz)
      console.warn(`[排班] 无法识别的时区 "${tz}"，回落到 ${DEFAULT_TIMEZONE}。班次时间换算结果会是错的，请到员工时区设置里改正。`, e)
    }
    f = formatterFor(DEFAULT_TIMEZONE)
  }
  formatterCache.set(tz, f)
  return f
}

/** 某个绝对时刻在指定时区的墙上时间各字段 */
export function tzParts(timeZone, date) {
  const parts = formatterFor(timeZone).formatToParts(date)
  const o = {}
  for (const p of parts) o[p.type] = p.value
  return {
    year: +o.year,
    month: +o.month,
    day: +o.day,
    // hour12:false 在部分实现里午夜会给出 "24"，取模归零
    hour: (+o.hour) % 24,
    minute: +o.minute,
    second: +o.second
  }
}

/** 某个绝对时刻在指定时区的 UTC 偏移（分钟，东为正） */
export function tzOffsetMinutes(timeZone, date) {
  const p = tzParts(timeZone, date)
  const asUtc = Date.UTC(p.year, p.month - 1, p.day, p.hour, p.minute, p.second)
  return Math.round((asUtc - date.getTime()) / 60000)
}

/** 偏移显示成 UTC+2 / UTC+5:30 */
export function formatOffset(minutes) {
  const sign = minutes < 0 ? '-' : '+'
  const abs = Math.abs(minutes)
  const h = Math.floor(abs / 60)
  const m = abs % 60
  return m === 0 ? `UTC${sign}${h}` : `UTC${sign}${h}:${String(m).padStart(2, '0')}`
}

/** 某时区在某天（默认今天）的偏移标签，如 "UTC+2" */
export function tzOffsetLabel(timeZone, date = new Date()) {
  return formatOffset(tzOffsetMinutes(timeZone, date))
}

/**
 * 把「某时区的某天某分钟」转成绝对时刻。
 * minutes 允许 >= 1440（例如 24:00 表示次日 0 点），Date.UTC 会自动进位。
 *
 * 做两次偏移探测是为了夏令时切换日：先按猜测的偏移落到一个时刻，
 * 如果那个时刻的实际偏移不同（说明跨过了切换点），用实际偏移再算一次。
 */
export function zonedToUtc(year, month, day, minutes, timeZone) {
  const naive = Date.UTC(year, month - 1, day, 0, minutes)
  const guessOffset = tzOffsetMinutes(timeZone, new Date(naive))
  let ts = naive - guessOffset * 60000
  const realOffset = tzOffsetMinutes(timeZone, new Date(ts))
  if (realOffset !== guessOffset) ts = naive - realOffset * 60000
  return new Date(ts)
}

/** 'YYYY-MM-DD' + 分钟 + 时区 -> 绝对时刻 */
export function dateStrToUtc(dateStr, minutes, timeZone) {
  const [y, m, d] = String(dateStr).split('-').map(Number)
  return zonedToUtc(y, m, d, minutes, timeZone)
}

/** 绝对时刻在指定时区的日期 'YYYY-MM-DD' */
export function formatDateInTz(date, timeZone) {
  const p = tzParts(timeZone, date)
  return `${p.year}-${String(p.month).padStart(2, '0')}-${String(p.day).padStart(2, '0')}`
}

/** 绝对时刻在指定时区的时间 'HH:MM' */
export function formatTimeInTz(date, timeZone) {
  const p = tzParts(timeZone, date)
  return `${String(p.hour).padStart(2, '0')}:${String(p.minute).padStart(2, '0')}`
}

/** 绝对时刻在指定时区落在当天的第几个小时（0-23） */
export function hourInTz(date, timeZone) {
  return tzParts(timeZone, date).hour
}

/**
 * 解析班次时间段。
 * 支持 "09:00-18:00"、"24:00-09:00"（24:00 = 次日 0 点）、"全天"。
 * "-" / 空 表示不是上班班次（OD/OFF/H/PL/SL/AL/CT），返回 null。
 * ⚠️ 和后端 parseShiftTimeRange 是同一套规则，改一边要改另一边。
 */
export function parseTimeRange(timeRange) {
  const s = String(timeRange || '').trim()
  if (!s || s === '-') return null
  if (s === '全天') return { startMin: 0, durationMin: 1440 }

  const parts = s.split('-')
  if (parts.length !== 2) {
    console.warn(`[排班] 无法解析的班次时间段 "${timeRange}"，该班次不参与跨时区换算与覆盖统计`)
    return null
  }
  const start = parseHHMM(parts[0])
  const end = parseHHMM(parts[1])
  if (start === null || end === null) {
    console.warn(`[排班] 无法解析的班次时间段 "${timeRange}"，该班次不参与跨时区换算与覆盖统计`)
    return null
  }
  let duration = end - start
  if (duration <= 0) duration += 1440 // 跨天，如 24:00-09:00
  // ⚠️ startMin 不能对 1440 取模。晚班「24:00-09:00」的 24:00 指的是当天结束那一刻，
  // 也就是次日 0 点；取模成 0 会把整个班次提前一整天（8/13 的晚班会算成 8/13 凌晨上班）。
  // dateStrToUtc 里的 Date.UTC 会自动把 1440 分钟进位到次日，保持原值即可。
  return { startMin: start, durationMin: duration }
}

function parseHHMM(v) {
  const seg = String(v).trim().split(':')
  if (seg.length !== 2) return null
  const h = Number(seg[0])
  const m = Number(seg[1])
  if (!Number.isInteger(h) || !Number.isInteger(m)) return null
  if (h < 0 || h > 24 || m < 0 || m > 59) return null
  return h * 60 + m
}

/**
 * 解析某员工在某天所在的时区：取 effective_from <= 该日 的最后一条。
 * history 是 [{timezone, effective_from}]，可以无序。
 * 一条都匹配不上时回落默认时区并告警——静默回落会把「时区没配对」显示成「配好了」。
 */
export function resolveTimezoneAt(history, dateStr) {
  if (!Array.isArray(history) || history.length === 0) {
    return DEFAULT_TIMEZONE
  }
  const sorted = [...history].sort((a, b) => String(a.effective_from).localeCompare(String(b.effective_from)))
  let tz = ''
  for (const h of sorted) {
    if (String(h.effective_from) > dateStr) break
    tz = h.timezone
  }
  if (!tz) {
    console.warn(`[排班] 日期 ${dateStr} 早于该员工最早的时区记录(${sorted[0].effective_from})，回落 ${DEFAULT_TIMEZONE}`)
    return DEFAULT_TIMEZONE
  }
  return tz
}

/** 浏览器所在时区 */
export function browserTimezone() {
  try {
    return Intl.DateTimeFormat().resolvedOptions().timeZone || DEFAULT_TIMEZONE
  } catch (e) {
    console.warn('[排班] 读取浏览器时区失败，回落', DEFAULT_TIMEZONE, e)
    return DEFAULT_TIMEZONE
  }
}

/** 可选时区列表：优先用浏览器的完整 IANA 列表，取不到时退回常用清单 */
export function timezoneOptions() {
  const common = [
    'Asia/Shanghai', 'Asia/Hong_Kong', 'Asia/Singapore', 'Asia/Tokyo', 'Asia/Seoul',
    'Asia/Bangkok', 'Asia/Manila', 'Asia/Jakarta', 'Asia/Kolkata', 'Asia/Dubai',
    'Europe/Belgrade', 'Europe/Berlin', 'Europe/Paris', 'Europe/Madrid', 'Europe/Rome',
    'Europe/Amsterdam', 'Europe/Warsaw', 'Europe/Prague', 'Europe/Bucharest', 'Europe/Athens',
    'Europe/London', 'Europe/Lisbon', 'Europe/Moscow', 'Europe/Istanbul', 'Europe/Kyiv',
    'America/New_York', 'America/Chicago', 'America/Denver', 'America/Los_Angeles',
    'America/Sao_Paulo', 'Australia/Sydney', 'Pacific/Auckland', 'UTC'
  ]
  try {
    if (typeof Intl.supportedValuesOf === 'function') {
      const all = Intl.supportedValuesOf('timeZone')
      if (Array.isArray(all) && all.length > 0) {
        // 常用的排前面，其余按字母序跟在后面
        const rest = all.filter(tz => !common.includes(tz))
        return [...common.filter(tz => all.includes(tz) || tz === 'UTC'), ...rest]
      }
    }
  } catch (e) {
    console.warn('[排班] 读取浏览器时区列表失败，使用内置常用清单', e)
  }
  return common
}
