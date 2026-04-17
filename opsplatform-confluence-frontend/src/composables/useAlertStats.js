import api from '@/api'

// 从 tags 中提取实例 IP
function extractInstance(event) {
  if (event.annotations?.instance) return event.annotations.instance
  const tags = event.tags || []
  for (const t of tags) {
    if (t.startsWith('instance=')) return t.split('=')[1]
  }
  return event.target_ident || ''
}

// 前端处理告警：去重 + 维护窗口合并 + 桌台归类
export function processAlertsLocal(events, windows) {
  const mwParsed = []
  for (const mw of (windows || [])) {
    const project = (mw.project || '').toUpperCase()
    const environment = (mw.environment || 'PROD').toUpperCase()
    const type = mw.maintenance_type
    const matchRules = (mw.match_rules || '').split(',').map(s => s.trim().toUpperCase()).filter(Boolean)
    const ops = (mw.operations || []).map(op => ({
      ip: (op.ip || '').trim(),
      timeStart: op.time_start || '',
      timeEnd: op.time_end || '',
      content: op.content || '',
    }))
    const mwBase = { project, environment, type, matchRules, ops }

    if (mw.repeat_mode === 'weekly' && mw.weekday && mw.time_start && mw.time_end) {
      const now = new Date()
      const rangeStart = new Date(now.getFullYear(), 0, 1)
      const rangeEnd = new Date(now.getFullYear(), 11, 31)
      const [sh, sm] = (mw.time_start || '00:00').split(':').map(Number)
      const [eh, em] = (mw.time_end || '23:59').split(':').map(Number)
      for (let d = new Date(rangeStart); d <= rangeEnd; d.setDate(d.getDate() + 1)) {
        const jsDay = d.getDay() === 0 ? 7 : d.getDay()
        if (jsDay === mw.weekday) {
          const start = new Date(d); start.setHours(sh, sm, 0, 0)
          const end = new Date(d); end.setHours(eh, em, 0, 0)
          mwParsed.push({ ...mwBase, start: start.getTime() / 1000, end: end.getTime() / 1000 })
        }
      }
    } else {
      const start = new Date(mw.start_time).getTime() / 1000
      const end = new Date(mw.end_time).getTime() / 1000
      if (start && end) mwParsed.push({ ...mwBase, start, end })
    }
  }

  const mwEvents = []
  const nonMWEvents = []
  for (const e of events) {
    let matchedMW = null
    const groupUpper = (e.group_name || '').toUpperCase()
    const ruleUpper = (e.rule_name || '').toUpperCase()
    const inst = extractInstance(e)
    for (const mw of mwParsed) {
      if (e.trigger_time >= mw.start && e.trigger_time <= mw.end &&
          groupUpper.includes(mw.project) && groupUpper.includes(mw.environment)) {
        if (mw.matchRules.length > 0 && !mw.matchRules.some(r => ruleUpper.includes(r))) continue
        if (mw.ops.length > 0 && !mw.ops.some(op => inst.includes(op.ip) && op.ip)) continue
        matchedMW = mw
        break
      }
    }
    if (matchedMW) mwEvents.push({ event: e, mw: matchedMW })
    else nonMWEvents.push(e)
  }

  // 维护窗口内：按 项目+窗口 合并为 1 条
  const mwAlerts = {}
  for (const { event, mw } of mwEvents) {
    const key = `${mw.project}_${mw.start}_${mw.end}`
    if (mwAlerts[key]) {
      mwAlerts[key].raw_count++
      mwAlerts[key]._rules.add(event.rule_name)
    } else {
      mwAlerts[key] = {
        rule_name: `${mw.project}维护`,
        severity: event.severity, trigger_time: mw.start, recover_time: mw.end,
        is_recovered: 1, group_name: event.group_name, instance: '', raw_count: 1,
        impact: '无影响', handler: '', status: '已处理', note: mw.type,
        is_maintenance: true, maintenance_type: mw.type,
        _rules: new Set([event.rule_name]),
      }
    }
  }
  for (const a of Object.values(mwAlerts)) {
    const rules = [...a._rules]
    a.rule_name += ` (${rules.slice(0, 3).join(', ')}${rules.length > 3 ? '...' : ''})`
    delete a._rules
  }

  // 非维护：按 规则名+实例 去重
  const dedupMap = {}
  for (const e of nonMWEvents) {
    const inst = extractInstance(e)
    const key = `${e.rule_name}__${inst}`
    const detail = { trigger_time: e.trigger_time, recover_time: e.recover_time, is_recovered: e.is_recovered, trigger_value: e.trigger_value }
    if (dedupMap[key]) {
      dedupMap[key].raw_count++
      dedupMap[key]._details.push(detail)
      if (e.trigger_time < dedupMap[key].trigger_time) dedupMap[key].trigger_time = e.trigger_time
      if (e.recover_time > dedupMap[key].recover_time) dedupMap[key].recover_time = e.recover_time
      if (!e.is_recovered) dedupMap[key].is_recovered = 0
    } else {
      dedupMap[key] = {
        rule_name: e.rule_name, severity: e.severity, trigger_time: e.trigger_time,
        recover_time: e.recover_time, is_recovered: e.is_recovered, group_name: e.group_name,
        instance: inst, raw_count: 1, _details: [detail],
        _raw: { rule_note: e.rule_note, tags: e.tags, annotations: e.annotations, cluster: e.cluster, target_ident: e.target_ident },
        impact: '', handler: '', status: e.is_recovered ? '已处理' : '', note: '',
        is_maintenance: false, maintenance_type: '',
      }
    }
  }

  // TcpPortDown 桌台归类（按 30 分钟窗口 + 前缀分组）
  const WINDOW = 30 * 60 // 30 分钟
  const tcpCandidates = []  // 带前缀的 TcpPortDown 告警，等待按时间窗口分组
  const otherAlerts = []
  for (const a of Object.values(dedupMap)) {
    if (a.rule_name.toUpperCase().includes('TCPPORTDOWN')) {
      const idTag = (a._raw?.tags || []).find(t => /^id=/i.test(t))
      if (idTag) {
        const rawId = idTag.split('=')[1] || ''
        const pureId = rawId.includes('_') ? rawId.split('_').pop() : rawId
        const prefix = (pureId.match(/^([A-Za-z]+)/)?.[1] || '').toUpperCase()
        if (prefix) {
          tcpCandidates.push({ ...a, _pureId: pureId, _prefix: prefix })
          continue
        }
      }
    }
    otherAlerts.push(a)
  }

  // 按前缀分组，再按时间排序，然后滑动窗口合并
  const byPrefix = {}
  for (const a of tcpCandidates) {
    if (!byPrefix[a._prefix]) byPrefix[a._prefix] = []
    byPrefix[a._prefix].push(a)
  }

  for (const prefix of Object.keys(byPrefix)) {
    const list = byPrefix[prefix].sort((x, y) => x.trigger_time - y.trigger_time)
    let group = null
    for (const a of list) {
      if (group && a.trigger_time - group.trigger_time <= WINDOW) {
        // 合并到当前窗口组
        group.raw_count += a.raw_count
        group._ids.add(a._pureId)
        group._details = group._details.concat(a._details || [])
        if (a.recover_time > group.recover_time) group.recover_time = a.recover_time
        if (!a.is_recovered) group.is_recovered = 0
      } else {
        // 开新组
        if (group) finalizeGroup(group, otherAlerts)
        group = { ...a, _ids: new Set([a._pureId]), _details: [...(a._details || [])] }
      }
    }
    if (group) finalizeGroup(group, otherAlerts)
  }

  function finalizeGroup(g, targetArr) {
    const ids = [...g._ids].sort()
    const prefix = g._prefix
    if (ids.length === 1) {
      g.rule_name = `TcpPortDown - ${ids[0]}`
      g.note = `${ids[0]} 端口异常`
      g.instance = g.instance || ''
    } else {
      g.rule_name = `TcpPortDown - ${prefix}系列 (${ids.length}台, 30分钟内)`
      g.note = `${prefix}系列桌台端口异常 (${ids.slice(0, 5).join(', ')}${ids.length > 5 ? '...' : ''})`
      g.instance = `${ids.length}台`
    }
    g.impact = '无影响'
    g.status = '已处理'
    delete g._ids
    delete g._pureId
    delete g._prefix
    targetArr.push(g)
  }

  const result = [...Object.values(mwAlerts), ...otherAlerts]
  result.sort((a, b) => b.trigger_time - a.trigger_time)
  return result
}

// 拉取告警事件并处理
export async function fetchAndProcessAlerts({ stime, etime, connId, bgid, severities = [1, 2], onProgress, isCancelled }) {
  const connParam = connId ? `&conn_id=${connId}` : ''
  const bgidParam = bgid ? `&bgid=${bgid}` : ''

  const firstRes = await api.get(`/api/n9e/alert-events?stime=${stime}&etime=${etime}&limit=500&p=1${connParam}${bgidParam}`)
  const firstData = firstRes.data?.data || firstRes.data
  const total = firstData.total || 0
  let allEvents = firstData.list || []
  if (onProgress) onProgress(allEvents.length, total)

  if (total === 0) return { events: [], total: 0 }

  const totalPages = Math.ceil(total / 500)
  for (let p = 2; p <= totalPages; p++) {
    if (isCancelled && isCancelled()) return { events: allEvents, total, cancelled: true }
    const res = await api.get(`/api/n9e/alert-events?stime=${stime}&etime=${etime}&limit=500&p=${p}${connParam}${bgidParam}`)
    const d = res.data?.data || res.data
    allEvents = allEvents.concat(d.list || [])
    if (onProgress) onProgress(allEvents.length, total)
  }

  // 级别过滤（不在 1=S1, 2=S2, 3=S3 选择中的过滤掉）
  if (severities && severities.length < 3) {
    allEvents = allEvents.filter(e => severities.includes(e.severity))
  }

  return { events: allEvents, total }
}

export async function loadMaintenanceWindows() {
  try {
    const res = await api.get('/api/maintenance-windows')
    return res.data?.data || []
  } catch (e) {
    return []
  }
}
