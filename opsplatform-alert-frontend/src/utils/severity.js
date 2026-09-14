// One place that turns a severity or a delivery status into text and a badge.
//
// Three copies of this used to sit in three views, and they had drifted: the
// rules list only knew S1/S2/S3, so a rule carrying the schema's own default of
// "warning" rendered there as a grey badge reading the raw word, while the same
// rule read 「警告」 on every other page.
//
// Two vocabularies are in play because the column's default is "warning" while
// the UI offers S1/S2/S3. Both are mapped here rather than migrated: the stored
// values belong to installations already running, and rewriting them would be
// risking real data to tidy a label.

const SEVERITY = {
  S1:       { cls: 'badge-danger',  label: 'S1 灾难', short: 'S1' },
  S2:       { cls: 'badge-warning', label: 'S2 严重', short: 'S2' },
  S3:       { cls: 'badge-info',    label: 'S3 警告', short: 'S3' },
  critical: { cls: 'badge-danger',  label: '严重',    short: '严重' },
  warning:  { cls: 'badge-warning', label: '警告',    short: '警告' },
  info:     { cls: 'badge-info',    label: '信息',    short: '信息' },
}

const STATUS = {
  success: { cls: 'badge-success', label: '成功' },
  failed:  { cls: 'badge-danger',  label: '失败' },
  // A fan-out that reached some channels and not others. Amber, not green:
  // something did not arrive.
  partial: { cls: 'badge-warning', label: '部分成功' },
}

export function severityClass(s) {
  return SEVERITY[s]?.cls || 'badge-gray'
}

/** Dense lists pass short: true, where the level alone carries the meaning. */
export function severityLabel(s, { short = false } = {}) {
  const entry = SEVERITY[s]
  if (!entry) return s
  return short ? entry.short : entry.label
}

export function statusClass(s) {
  return STATUS[s]?.cls || 'badge-gray'
}

export function statusLabel(s) {
  return STATUS[s]?.label || s
}
