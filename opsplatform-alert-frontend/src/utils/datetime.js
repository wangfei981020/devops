// One place that turns an instant into text.
//
// The API sends instants (RFC3339, e.g. "2026-09-07T20:31:42Z") and never a
// pre-formatted local string. Which wall clock a person sees is decided here,
// from the platform's configured display timezone — so changing that setting
// re-renders history rather than rewriting it, and every page moves together.
//
// Before this, six views each carried their own `new Date(t).toLocaleString()`,
// which silently used whatever timezone the viewer's laptop was set to, and one
// view hard-coded Asia/Shanghai. Two people looking at the same alert could
// read two different times and neither could tell.

// Set from the platform settings at startup. Empty means "use the browser's
// zone", which is the only sensible thing to do before the setting has loaded.
//
// This is a ref, not a plain variable, and that is load-bearing. The setting
// arrives from the API after the first paint, so a page renders once with the
// fallback zone and then has to render again with the real one. A plain
// variable gives Vue nothing to track: the timestamps stay on the browser's
// zone until something unrelated happens to invalidate them, which is exactly
// the confusion this whole feature exists to remove. Reading the ref inside the
// formatters makes it a dependency of every template that shows a time.
import { ref } from 'vue'

const displayTimezone = ref('')

export function setDisplayTimezone(name) {
  displayTimezone.value = name || ''
}

export function getDisplayTimezone() {
  return displayTimezone.value
}

function zoneOption() {
  return displayTimezone.value ? { timeZone: displayTimezone.value } : {}
}

function toDate(value) {
  if (value === null || value === undefined || value === '') return null
  const d = value instanceof Date ? value : new Date(value)
  return Number.isNaN(d.getTime()) ? null : d
}

// 'sv-SE' formats as "2026-09-08 04:31:42" — the layout used across the
// backend and the alert messages, so the two always agree.
function parts(date, opts) {
  return new Intl.DateTimeFormat('sv-SE', {
    ...zoneOption(),
    hour12: false,
    ...opts,
  }).format(date)
}

/** "2026-09-08 04:31:42" in the configured zone. */
export function formatTime(value) {
  const d = toDate(value)
  if (!d) return '-'
  return parts(d, {
    year: 'numeric', month: '2-digit', day: '2-digit',
    hour: '2-digit', minute: '2-digit', second: '2-digit',
  })
}

/**
 * "09-08 04:31" — for dense lists where the year is noise.
 *
 * Derived from the full format rather than asked of Intl directly: a
 * month-and-day-only pattern comes back locale-shaped ("07/09"), which reads
 * as either the 7th of September or the 9th of July depending on the reader.
 */
export function formatShortTime(value) {
  const full = formatTime(value)
  return full === '-' ? '-' : full.slice(5, 16)
}

/** "2026-09-08" in the configured zone. */
export function formatDate(value) {
  const d = toDate(value)
  if (!d) return '-'
  return parts(d, { year: 'numeric', month: '2-digit', day: '2-digit' })
}

// The offset has to be derived from the instant, not stored: a zone's offset
// changes across the year. Formatting the instant in the target zone and
// reading it back as if it were UTC gives the exact offset that applies then.
function offsetMinutes(date) {
  if (!displayTimezone.value) return -date.getTimezoneOffset()
  const dtf = new Intl.DateTimeFormat('en-US', {
    timeZone: displayTimezone.value, hour12: false,
    year: 'numeric', month: '2-digit', day: '2-digit',
    hour: '2-digit', minute: '2-digit', second: '2-digit',
  })
  const p = Object.fromEntries(dtf.formatToParts(date).map(x => [x.type, x.value]))
  const asUTC = Date.UTC(+p.year, +p.month - 1, +p.day, +p.hour % 24, +p.minute, +p.second)
  return Math.round((asUTC - date.getTime()) / 60000)
}

export function formatOffset(value) {
  const d = toDate(value) || new Date()
  const mins = offsetMinutes(d)
  const sign = mins < 0 ? '-' : '+'
  const abs = Math.abs(mins)
  return `${sign}${String(Math.floor(abs / 60)).padStart(2, '0')}:${String(abs % 60).padStart(2, '0')}`
}

/**
 * "2026-09-08 04:31:42 (+08:00 Asia/Shanghai)" — the same form the alert
 * messages use. For places where the reader may not share the zone.
 */
export function formatTimeWithZone(value) {
  const d = toDate(value)
  if (!d) return '-'
  const zone = displayTimezone.value || Intl.DateTimeFormat().resolvedOptions().timeZone
  return `${formatTime(d)} (${formatOffset(d)} ${zone})`
}

/** "3 分钟后到期" / "2 小时后到期", from a server-computed second count. */
export function formatRemaining(seconds) {
  if (!Number.isFinite(seconds) || seconds <= 0) return '即将到期'
  const mins = Math.round(seconds / 60)
  if (mins < 60) return `${mins} 分钟后到期`
  const hours = Math.round(mins / 60)
  if (hours < 48) return `${hours} 小时后到期`
  return `${Math.round(hours / 24)} 天后到期`
}
