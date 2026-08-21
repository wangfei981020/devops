import { useTranslation } from '@ops/i18n'
import { useQuery } from '@tanstack/react-query'
import { ErrorState, Skeleton } from '@ops/ui'
import { get, makeLoadError } from '../lib/api.js'
import { SERIES_FALLBACK, type TimeRange } from '../lib/timerange.js'

type Point = { t: number; critical: number; warning: number; info: number }
type Series = { step_sec: number; points: Point[] }

/** 峰值。图表内外都要显示，算两遍容易一个改了另一个没改 */
function peakOf(points: Point[]): number {
  return Math.max(0, ...points.map((p) => p.critical + p.warning + p.info))
}

/**
 * 告警数量时序图。
 *
 * 画的是**新增速率**，不是当前未恢复数。一条挂了三天没恢复的告警
 * 会把"未恢复数"曲线整体抬高，而值班时要看的是"什么时候开始变糟的"、
 * "有没有跟着某次发布跳起来"——那是新增速率才回答得了的。
 */
export function IncidentSeries({ range }: { range: TimeRange }) {
  const { t } = useTranslation()
  // 「全部」时退回一个有限窗口。副标题会写明实际画的是哪一段，
  // 不能让人以为这根线覆盖了全部历史。
  const effective = range || SERIES_FALLBACK
  const query = useQuery({
    queryKey: ['incidents', 'series', effective],
    queryFn: () => get<Series>(`/incidents/series?range=${effective}`),
    refetchInterval: 60_000,
  })

  const stepLabel = query.data
    ? t('opsalert:series.step', { minutes: Math.max(1, Math.round(query.data.step_sec / 60)) })
    : ''

  return (
    <section className="rounded-lg border border-border bg-card">
      <header className="flex flex-wrap items-center gap-x-3 gap-y-1 border-b border-border px-3.5 py-2.5">
        <h3 className="text-xs font-semibold">{t('opsalert:series.title')}</h3>
        <span className="text-2xs text-muted-foreground">
          {t(`opsalert:series.window.${effective}`, effective)}
          {stepLabel ? ` · ${stepLabel}` : ''}
          {` · ${t('opsalert:series.scope')}`}
        </span>
        <div className="ml-auto flex items-center gap-3 text-2xs text-muted-foreground">
          {query.data && <span className="font-mono">{t('opsalert:series.peak', { peak: peakOf(query.data.points) })}</span>}
          <span className="inline-flex items-center gap-1.5">
            <i className="inline-block size-2 rounded-[2px] bg-danger" aria-hidden="true" />
            {t('opsalert:severity.critical')}
          </span>
          <span className="inline-flex items-center gap-1.5">
            <i className="inline-block size-2 rounded-[2px] bg-warning" aria-hidden="true" />
            {t('opsalert:severity.warning')}
          </span>
        </div>
      </header>
      <div className="px-3.5 py-3">
        {query.isPending && <Skeleton className="h-[150px] w-full" />}
        {query.isError && (
          // ⚠️ 图表加载失败必须显式说失败。画一张空图（或者干脆不画）
          // 在这里读作"这段时间很太平"，而真相是数据根本没取到。
          <ErrorState
            title={t('opsalert:series.loadError')}
            error={makeLoadError(t)(query.error)}
            retryLabel={t('action.retry')}
            onRetry={() => query.refetch()}
          />
        )}
        {query.data && <Chart points={query.data.points} stepSec={query.data.step_sec} />}
      </div>
    </section>
  )
}

const W = 900
const H = 100

/**
 * ⚠️ 图形和文字**分开画**。
 *
 * 第一版把刻度文字也放进 SVG，于是两难：
 *   preserveAspectRatio="none" 铺满宽度 → 文字被横向拉成扁字；
 *   等比缩放不失真 → 高度跟着宽度长，1700px 宽的屏上这张次要图表
 *   自己长到 250px 高，把下面的事件列表整个挤出首屏。
 * 拆开之后两个问题一起没了：路径非等比铺满（线条本来就没有"正确宽高比"），
 * 文字回到 HTML 里，字号固定、能换行、能被翻译。
 */
function Chart({ points, stepSec }: { points: Point[]; stepSec: number }) {
  const { t } = useTranslation()
  const peak = Math.max(1, ...points.map((p) => p.critical + p.warning + p.info))
  const total = points.reduce((n, p) => n + p.critical + p.warning + p.info, 0)

  // ⚠️ 全零**不画两条贴底的平线**。贴底平线跟"低而平稳"长得一模一样，
  // 而全零常常意味着规则根本没跑或者查询一直在失败。
  // 这条规则和 Sparkline 里的是同一条，两处都栽过。
  if (total === 0) {
    return (
      <div className="flex h-[130px] items-center justify-center text-xs text-muted-foreground">
        {t('opsalert:series.zero')}
      </div>
    )
  }

  const x = (i: number) => (points.length === 1 ? W / 2 : (i / (points.length - 1)) * W)
  const y = (v: number) => H - (v / peak) * H

  const line = (pick: (p: Point) => number) =>
    points.map((p, i) => `${i === 0 ? 'M' : 'L'}${x(i).toFixed(1)},${y(pick(p)).toFixed(1)}`).join(' ')
  const area = (pick: (p: Point) => number) => `${line(pick)} L${W},${H} L0,${H} Z`

  // 时间刻度取 4 个，够定位又不至于挤在一起
  const ticks = [0, Math.floor(points.length / 3), Math.floor((points.length * 2) / 3), points.length - 1]
    .filter((v, n, arr) => arr.indexOf(v) === n)
    .map((i) => new Date(points[i]!.t * 1000).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' }))

  return (
    <figure className="m-0">
      <svg
        viewBox={`0 0 ${W} ${H}`}
        preserveAspectRatio="none"
        className="h-[130px] w-full"
        role="img"
        aria-label={t('opsalert:series.aria', { total, peak })}
      >
        <defs>
          <linearGradient id="ops-series-c" x1="0" y1="0" x2="0" y2="1">
            <stop offset="0%" style={{ stopColor: 'var(--color-danger)' }} stopOpacity="0.32" />
            <stop offset="100%" style={{ stopColor: 'var(--color-danger)' }} stopOpacity="0" />
          </linearGradient>
          <linearGradient id="ops-series-w" x1="0" y1="0" x2="0" y2="1">
            <stop offset="0%" style={{ stopColor: 'var(--color-warning)' }} stopOpacity="0.26" />
            <stop offset="100%" style={{ stopColor: 'var(--color-warning)' }} stopOpacity="0" />
          </linearGradient>
        </defs>
        {/* vectorEffect 是非等比缩放下的必需品：没有它，横线会被压成发丝、
            竖向笔画被拉粗，同一根 1px 的线在不同方向粗细不一样 */}
        <g className="stroke-border" strokeWidth="1" vectorEffect="non-scaling-stroke">
          {[0.25, 0.5, 0.75, 1].map((f) => (
            <line key={f} x1="0" x2={W} y1={H * f} y2={H * f} vectorEffect="non-scaling-stroke" />
          ))}
        </g>
        <path d={area((p) => p.warning)} fill="url(#ops-series-w)" />
        <path
          d={line((p) => p.warning)}
          fill="none"
          strokeWidth="1.8"
          strokeLinejoin="round"
          vectorEffect="non-scaling-stroke"
          className="stroke-warning"
        />
        <path d={area((p) => p.critical)} fill="url(#ops-series-c)" />
        <path
          d={line((p) => p.critical)}
          fill="none"
          strokeWidth="1.8"
          strokeLinejoin="round"
          vectorEffect="non-scaling-stroke"
          className="stroke-danger"
        />
      </svg>
      <figcaption className="mt-1 flex justify-between font-mono text-2xs text-muted-foreground">
        {ticks.map((label, i) => (
          <span key={i}>{label}</span>
        ))}
      </figcaption>
      <span className="sr-only">{t('opsalert:series.step', { minutes: Math.max(1, Math.round(stepSec / 60)) })}</span>
    </figure>
  )
}
