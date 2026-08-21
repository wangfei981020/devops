import { useTranslation } from '@ops/i18n'

/**
 * 趋势线与执行状况条 —— 告警产品区别于通用后台的两个标志性元素。
 *
 * 参考 Grafana Alerting / Better Stack：列表里光有一个计数说不出
 * 「在恶化还是已平息」，而这恰恰是值班时第一个要判断的事。
 */

export type TrendTone = 'firing' | 'pending' | 'ok' | 'muted'

const STROKE: Record<TrendTone, string> = {
  firing: 'stroke-danger',
  pending: 'stroke-warning',
  ok: 'stroke-success',
  muted: 'stroke-muted-foreground',
}

/**
 * 迷你趋势线。
 *
 * ⚠️ 传空数组时**不画一条平线**，而是显示占位符：
 * 平线看起来像"很稳定"，而真相是没有数据 —— 这正是本产品要根治的那类误读。
 */
export function Sparkline({
  points,
  tone = 'muted',
  width = 120,
  height = 24,
  label,
}: {
  points: number[]
  tone?: TrendTone
  width?: number
  height?: number
  /** 给读屏用的一句话描述，例如「最近 12 个周期命中数，趋势上升」 */
  label: string
}) {
  const { t } = useTranslation()
  if (points.length < 2) {
    return (
      <span className="font-mono text-2xs text-muted-foreground" title={t('opsalert:trend.noData')}>
        {t('state.noValue')}
      </span>
    )
  }
  const max = Math.max(...points)
  const min = Math.min(...points)
  // ⚠️ 全零**不画平线**。
  // 平线在告警列表里读作"很稳定"，而全零的真相往往是查询一直在失败、
  // 一条都没命中 —— 把"没数据"画成"平稳"，正是本产品要根治的那类误读。
  // 实测撞到过：假数据源停了之后，整列趋势都是一条漂亮的横线。
  if (max === 0) {
    return (
      <span className="font-mono text-2xs text-muted-foreground" title={t('opsalert:trend.allZero')}>
        {t('opsalert:trend.zero')}
      </span>
    )
  }
  // 全等且非零：是真的平稳，画居中的线（同时避免除零）
  const span = max - min || 1
  const step = width / (points.length - 1)
  const d = points
    .map((v, i) => `${i === 0 ? 'M' : 'L'}${(i * step).toFixed(1)},${(height - 2 - ((v - min) / span) * (height - 4)).toFixed(1)}`)
    .join(' ')
  return (
    <svg width={width} height={height} viewBox={`0 0 ${width} ${height}`} role="img" aria-label={label}>
      <path d={d} fill="none" strokeWidth="1.5" className={STROKE[tone]} />
    </svg>
  )
}

export type Slot = 'ok' | 'warn' | 'bad' | 'none'

const SLOT_BG: Record<Slot, string> = {
  ok: 'bg-success',
  warn: 'bg-warning',
  bad: 'bg-danger',
  none: 'bg-border',
}

/**
 * 执行状况条：每格一个检查周期。
 *
 * 这是「7 天触发 0 次到底是健康还是坏了」的答案 ——
 *   全绿   一直在跑，确实没命中
 *   后半红 查询在失败，这条规则已经不告警了
 *   全灰   压根没跑（已停用）
 * 三种情况在纯数字列表里长得一模一样。
 */
export function UptimeBar({
  slots,
  label,
  height = 22,
}: {
  slots: Slot[]
  label: string
  height?: number
}) {
  const { t } = useTranslation()
  if (slots.length === 0) {
    return <span className="font-mono text-2xs text-muted-foreground">{t('state.noValue')}</span>
  }
  return (
    <span className="flex items-end gap-[2px]" style={{ height }} role="img" aria-label={label}>
      {slots.map((s, i) => (
        <i
          key={i}
          className={`min-w-[2px] flex-1 rounded-[1px] ${SLOT_BG[s]} ${s === 'none' ? 'opacity-50' : 'opacity-90'}`}
          style={{ height: '100%' }}
        />
      ))}
    </span>
  )
}

/**
 * 状态点 + 文字。取代彩色胶囊徽章。
 *
 * ⚠️ 颜色**不是唯一指示**：文字始终在，色盲用户与黑白打印都读得出。
 */
export function StatusDot({ tone, children }: { tone: TrendTone; children: React.ReactNode }) {
  const color =
    tone === 'firing'
      ? 'text-danger'
      : tone === 'pending'
        ? 'text-warning'
        : tone === 'ok'
          ? 'text-success'
          : 'text-muted-foreground'
  return (
    <span className={`inline-flex items-center gap-1.5 text-xs font-medium ${color}`}>
      <span className="size-1.5 rounded-full bg-current" aria-hidden="true" />
      {children}
    </span>
  )
}
