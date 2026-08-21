import { useTranslation } from '@ops/i18n'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import {
  AsyncBoundary,
  Badge,
  Button,
  EmptyState,
  Skeleton,
  cn,
  fromQuery,
} from '@ops/ui'
import { RangePicker } from '../components/RangePicker.js'
import { IncidentSeries } from '../components/Series.js'
import { Sparkline } from '../components/Trend.js'
import { WriteButton } from '../components/WriteButton.js'
import { ArrowLeft, CheckCircle2, CircleSlash, XCircle } from 'lucide-react'
import { useState, type ReactNode } from 'react'
import { get, post, makeLoadError } from '../lib/api.js'
import { rangeParam, useTimeRange } from '../lib/timerange.js'
import { severityTone, statusKey } from './WarRoom.js'

type Incident = {
  id: number
  title: string
  severity: string
  status: string
  count: number
  first_at: string
  last_at: string
  acked_by: string
  labels: Record<string, string>
  rule_id: number
  /** 所属规则最近 24 个周期的命中数，新→旧 */
  trend?: number[] | null
}

type TraceStep = { step: string; ok: boolean; reason?: string; detail?: Record<string, unknown> }
type Trace = { verdict: string; steps: TraceStep[]; at: string }

type Similar = {
  id: number
  status: string
  count: number
  first_at: string
  last_at: string
  /** 上次的恢复耗时。**没恢复过时这个字段不存在**，不是 0 */
  recovery_sec?: number
}

type Detail = {
  id: number
  title: string
  severity: string
  status: string
  count: number
  first_at: string
  last_at: string
  acked_by: string
  fingerprint: string
  labels: Record<string, string>
  sample: unknown
  similar: Similar[]
  timeline: { kind: string; actor: string; message: string; at: string }[]
  deliveries: { status: string; error: string; duration_ms: number; at: string; notifier: string; type: string }[]
  delivery_warning?: string
  /** 时间线/投递的**全量**条数。列表只给最近若干条，总数单独出，见后端 mcp_data.go */
  timeline_total: number
  delivery_total: number
  delivery_failed: number
}

/**
 * 判定链里的取值。
 *
 * ⚠️ 不能直接 String(v)：判定链的 detail 里混着数组和对象
 * （比如投递结果 results），String 出来是 `[object Object]` ——
 * 判定链是这个产品的核心卖点，在它里面显示 [object Object]
 * 等于把"凭什么这么判"这句话说了一半。
 */
function fmtDetail(v: unknown): string {
  if (v == null) return '-'
  if (typeof v === 'object') return JSON.stringify(v)
  return String(v)
}

/**
 * 把投递记录按「渠道 + 结果 + 错误」合并同类。
 *
 * 一条持续告警会反复投递，失败时每次的报错**一模一样**。
 * 逐条列出来的效果是：20 行完全相同的红字，把右栏顶到一千多像素高，
 * 「相似历史」被挤到屏幕外——而排查时真正要看的恰恰是后者。
 * 合并成一行「工单 Webhook · connection refused · 20 次」之后，
 * 信息一点没少（次数还更清楚了），却只占一行。
 */
type Delivery = Detail['deliveries'][number]
function groupDeliveries(items: Delivery[]): (Delivery & { times: number })[] {
  const out: (Delivery & { times: number })[] = []
  const idx = new Map<string, number>()
  for (const d of items) {
    const key = `${d.notifier}|${d.type}|${d.status}|${d.error}`
    const at = idx.get(key)
    if (at == null) {
      idx.set(key, out.length)
      out.push({ ...d, times: 1 })
    } else {
      out[at]!.times += 1
      // 耗时取最近一次：合并后再显示一个平均值会让人以为那是某一次的真实耗时
      out[at]!.duration_ms = d.duration_ms
    }
  }
  return out
}

/** 「共 N 条，只显示最近 M 条」。不说的话，截断看起来就像"就这么多"。 */
function truncHint(
  shown: number,
  total: number,
  t: (k: string, o?: Record<string, unknown>) => string,
): string | null {
  return total > shown ? t('opsalert:incidents.truncated', { shown, total }) : null
}

/** 判定链的步骤与终局都走 i18n；认不出的值原样显示，别退化成"未知"。 */
const VERDICT_KEYS: Record<string, string> = {
  fired: 'fired',
  not_fired: 'notFired',
  suppressed: 'suppressed',
  silenced: 'silenced',
  no_route: 'noRoute',
  delivery_failed: 'deliveryFailed',
}

/** 状态 → 左侧色条。颜色只是加速扫视，文字始终在，色盲用户读得出。 */
const BAR: Record<string, string> = {
  firing: 'bg-danger',
  acked: 'bg-warning',
  suppressed: 'bg-muted-foreground/60',
  resolved: 'bg-success',
}

export function IncidentsPage({
  detailId,
  onOpen,
  onBack,
}: {
  /** 打开的事件 id。来自 hash（#incidents/123），所以链接可以直接甩给同事 */
  detailId: number | null
  onOpen: (id: number) => void
  onBack: () => void
}) {
  if (detailId != null) return <IncidentDetail id={detailId} onBack={onBack} />
  return <IncidentList onOpen={onOpen} />
}

function IncidentList({ onOpen }: { onOpen: (id: number) => void }) {
  const { t } = useTranslation()
  // 时间范围来自顶栏，不在页面里再放一份：
  // 同一屏出现两个时间筛选时，人得先分辨哪个管哪个
  const { range } = useTimeRange()
  const [status, setStatus] = useStatusFilter()

  const query = useQuery({
    queryKey: ['incidents', status, range],
    queryFn: () => get<{ items: Incident[] }>(`/incidents?status=${status}${rangeParam(range)}`),
    refetchInterval: 30_000,
  })

  return (
    <div className="flex flex-col gap-3">
      <IncidentSeries range={range} />

      {/* 筛选条只回答"这个模块里我要看哪一部分"。
          "看多久以内"归顶栏，因为它作用在整个产品上 */}
      <div className="flex flex-wrap items-center gap-1.5">
        {/* 窄屏专用：顶栏那份放不下时由它接手，读的是同一个 context */}
        <RangePicker className="mr-1 md:hidden" />
        {[
          ['active', t('opsalert:incidents.filterActive')],
          ['firing', t('opsalert:incidents.filterFiring')],
          ['suppressed', t('opsalert:incidents.filterSuppressed')],
          ['resolved', t('opsalert:incidents.filterResolved')],
          ['all', t('opsalert:incidents.filterAll')],
        ].map(([k, label]) => (
          <button
            key={k}
            type="button"
            onClick={() => setStatus(k as string)}
            className={cn(
              'cursor-pointer rounded-full border px-3 py-1 text-xs transition-colors',
              status === k
                ? 'border-primary bg-primary/10 text-primary'
                : 'border-border bg-card text-muted-foreground hover:border-muted-foreground hover:text-foreground',
            )}
            aria-pressed={status === k}
          >
            {label}
          </button>
        ))}
        {query.data && (
          // ⚠️ 明确说清"这是范围内的条数"。只写一个数字的话，
          // 时间范围把老事件切掉之后，看起来就像问题已经少了
          <span className="ml-auto text-2xs text-muted-foreground">
            {t('opsalert:incidents.countInRange', {
              count: query.data.items.length,
              range: t(`opsalert:range.${range || 'all'}`),
            })}
          </span>
        )}
      </div>

      <AsyncBoundary
        state={fromQuery(query, (d) => d.items.length === 0, makeLoadError(t))}
        pending={<Skeleton className="h-64 w-full" />}
        empty={
          <EmptyState
            title={t('opsalert:incidents.emptyTitle')}
            reason={t('opsalert:incidents.emptyReason')}
            action={null}
          />
        }
        errorTitle={t('opsalert:incidents.loadError')}
        retryLabel={t('action.retry')}
        onRetry={() => query.refetch()}
      >
        {(data) => (
          <ul className="divide-y divide-border overflow-hidden rounded-lg border border-border bg-card">
            {data.items.map((it) => (
              <IncidentRow key={it.id} it={it} onOpen={() => onOpen(it.id)} />
            ))}
          </ul>
        )}
      </AsyncBoundary>
    </div>
  )
}

/**
 * 一条事件。
 *
 * 左侧 3px 色条取代彩色胶囊徽章 —— PagerDuty / Better Stack 的做法：
 * 一屏十几条时，竖直的色带能一眼扫出"红的集中在哪一段"，
 * 而一排彩色胶囊会把视线打散，反而看不出分布。
 */
function IncidentRow({ it, onOpen }: { it: Incident; onOpen: () => void }) {
  const { t } = useTranslation()
  const pts = [...(it.trend ?? [])].reverse() // 后端给的是新→旧，画线要从左到右
  return (
    <li>
      <button
        type="button"
        onClick={onOpen}
        className="grid w-full cursor-pointer grid-cols-[3px_minmax(0,1fr)_auto] items-center gap-x-3.5 py-2.5 pr-3.5 text-left transition-colors hover:bg-muted/50 sm:grid-cols-[3px_minmax(0,1fr)_120px_100px_auto]"
      >
        <span className={cn('h-full min-h-9 rounded-r-sm', BAR[it.status] ?? 'bg-muted-foreground/40')} aria-hidden="true" />
        <span className="min-w-0">
          <span className="flex items-center gap-2">
            <Badge tone={severityTone(it.severity)}>
              {t(`opsalert:severity.${it.severity}`, it.severity)}
            </Badge>
            <span className="truncate text-sm font-medium">{it.title}</span>
          </span>
          <span className="mt-1 flex flex-wrap gap-x-2.5 gap-y-0.5 font-mono text-2xs text-muted-foreground">
            <span>{t(`opsalert:status.${statusKey(it.status)}`, it.status)}</span>
            <span>×{it.count}</span>
            <span>{new Date(it.first_at).toLocaleString()}</span>
            {it.acked_by && <span>@{it.acked_by}</span>}
          </span>
        </span>
        <span className="hidden sm:block">
          <Sparkline
            points={pts}
            tone={it.status === 'firing' ? 'firing' : 'muted'}
            width={104}
            height={22}
            label={t('opsalert:incidents.trendLabel', { count: pts.length })}
          />
        </span>
        <span className="hidden text-right font-mono text-xs tabular-nums text-muted-foreground sm:block">
          {sinceLabel(it.last_at, t)}
        </span>
        <span className="text-2xs text-muted-foreground">{t('opsalert:incidents.open')}</span>
      </button>
    </li>
  )
}

/** 距今多久。绝对时间已经在上一行了，这里给的是"还在不在烧"的直觉。 */
function sinceLabel(at: string, t: (k: string, o?: Record<string, unknown>) => string): string {
  const sec = Math.max(0, Math.round((Date.now() - new Date(at).getTime()) / 1000))
  if (sec < 60) return t('opsalert:since.sec', { n: sec })
  if (sec < 3600) return t('opsalert:since.min', { n: Math.round(sec / 60) })
  if (sec < 86400) return t('opsalert:since.hour', { n: Math.round(sec / 3600) })
  return t('opsalert:since.day', { n: Math.round(sec / 86400) })
}

/**
 * 状态筛选。存 sessionStorage 而不是组件 state：
 * 点进详情再返回时，组件已经卸载重建，不存的话会跳回默认的「未恢复」——
 * 挨个看已恢复事件的人得每次重选一遍。
 */
const STATUS_KEY = 'opsalert.incidents.status'
function useStatusFilter(): [string, (s: string) => void] {
  const [v, setV] = useState(() => sessionStorage.getItem(STATUS_KEY) ?? 'active')
  return [
    v,
    (s: string) => {
      sessionStorage.setItem(STATUS_KEY, s)
      setV(s)
    },
  ]
}

/**
 * 事件详情：内容区 + 右侧排查栏。加上左边的主导航就是三栏。
 *
 * 右栏放的是**排查时要对照的东西**——命中样本、投递结果、相似历史、时间线。
 * 抽屉装不下这些：520px 宽度里样本日志会折成一团，而值班时
 * 「上次同样的问题多久好的」和「这次告警到底送出去没有」必须同屏可见，
 * 点开三个不同抽屉去凑齐它们，等于把判断成本转嫁给最忙的那个人。
 */
function IncidentDetail({ id, onBack }: { id: number; onBack: () => void }) {
  const { t } = useTranslation()
  const qc = useQueryClient()
  const detail = useQuery({ queryKey: ['incident', id], queryFn: () => get<Detail>(`/incidents/${id}`) })
  const trace = useQuery({ queryKey: ['incident', id, 'trace'], queryFn: () => get<Trace>(`/incidents/${id}/trace`) })

  const ack = useMutation({
    mutationFn: () => post(`/incidents/${id}/ack`),
    onSuccess: () => {
      detail.refetch()
      qc.invalidateQueries({ queryKey: ['incidents'] })
    },
  })

  const d = detail.data
  return (
    <div className="flex flex-col gap-3">
      <div className="flex flex-wrap items-center gap-2">
        <Button size="sm" variant="ghost" onClick={onBack}>
          <ArrowLeft className="size-3.5" aria-hidden="true" />
          {t('opsalert:incidents.back')}
        </Button>
        {d && (
          <>
            <Badge tone={severityTone(d.severity)}>{t(`opsalert:severity.${d.severity}`, d.severity)}</Badge>
            <h2 className="min-w-0 truncate text-sm font-semibold">{d.title}</h2>
            <span className="font-mono text-2xs text-muted-foreground">
              {t('opsalert:incidents.detailSummary', { count: d.count, time: new Date(d.first_at).toLocaleString() })}
            </span>
            {d.status === 'firing' && (
              <span className="ml-auto">
                <WriteButton perm="alert:ack_incident" loading={ack.isPending} onClick={() => ack.mutate()}>
                  {t('opsalert:warroom.ack')}
                </WriteButton>
              </span>
            )}
          </>
        )}
      </div>

      {detail.isPending && <Skeleton className="h-80 w-full" />}
      {detail.isError && (
        <div className="rounded-lg border border-danger/40 bg-danger-bg px-4 py-3 text-sm text-danger">
          {t('opsalert:incidents.detailError')}：{String(detail.error)}
        </div>
      )}

      {d && (
        // 窄屏折成一栏：右栏内容跟到下面去，而不是挤成不可读的窄条
        <div className="grid gap-3 lg:grid-cols-[minmax(0,1fr)_300px]">
          <div className="flex flex-col gap-3">
            <Panel title={t('opsalert:incidents.chain')}>
              {/* 这一段是产品的核心差异点：不只说触发了，还说清凭什么触发。
                  查询失败时 trace 也存在，正好解释"为什么没告警"。 */}
              {trace.isPending && <Skeleton className="h-24 w-full" />}
              {trace.isError && (
                <div className="text-xs text-muted-foreground">{t('opsalert:incidents.chainNone')}</div>
              )}
              {trace.data && (
                <>
                  <div className="mb-2 text-xs">
                    {t('opsalert:incidents.chainVerdict')}：
                    <span className="font-medium">
                      {t(`opsalert:verdict.${VERDICT_KEYS[trace.data.verdict] ?? ''}`, trace.data.verdict)}
                    </span>
                  </div>
                  <ol className="flex flex-col gap-2">
                    {trace.data.steps.map((s, i) => (
                      <li key={`${s.step}-${i}`} className="flex gap-2 text-xs">
                        <span className="mt-0.5 shrink-0">
                          {s.ok ? (
                            <CheckCircle2 className="size-3.5 text-success" />
                          ) : s.step === 'deliver' || s.step === 'route' ? (
                            <XCircle className="size-3.5 text-danger" />
                          ) : (
                            <CircleSlash className="size-3.5 text-muted-foreground" />
                          )}
                        </span>
                        <div className="min-w-0">
                          <div className="font-medium">{t(`opsalert:step.${s.step}`, s.step)}</div>
                          {s.reason && <div className="text-muted-foreground">{s.reason}</div>}
                          {s.detail && (
                            <div className="mt-0.5 break-all font-mono text-[11px] text-muted-foreground">
                              {Object.entries(s.detail).map(([k, v]) => `${k}=${fmtDetail(v)}`).join('  ')}
                            </div>
                          )}
                        </div>
                      </li>
                    ))}
                  </ol>
                </>
              )}
            </Panel>

            <Panel title={t('opsalert:incidents.labels')}>
              <div className="flex flex-wrap gap-1.5">
                {Object.entries(d.labels ?? {}).map(([k, v]) => (
                  <span key={k} className="rounded border border-border px-1.5 py-0.5 font-mono text-[11px]">
                    {k}={v}
                  </span>
                ))}
              </div>
              <div className="mt-2 break-all font-mono text-2xs text-muted-foreground">
                {t('opsalert:incidents.fingerprint')}: {d.fingerprint}
              </div>
            </Panel>

            <Panel title={t('opsalert:incidents.timeline')}>
              {truncHint(d.timeline.length, d.timeline_total, t) && (
                <div className="mb-2 text-2xs text-muted-foreground">
                  {truncHint(d.timeline.length, d.timeline_total, t)}
                </div>
              )}
              <ul className="flex flex-col gap-1.5 text-xs">
                {d.timeline.map((e, i) => (
                  <li key={i}>
                    <span className="font-mono tabular-nums text-muted-foreground">
                      {new Date(e.at).toLocaleTimeString()}
                    </span>
                    <span className="ml-2">{e.message}</span>
                    {e.actor && <span className="ml-1 text-muted-foreground">· {e.actor}</span>}
                  </li>
                ))}
              </ul>
            </Panel>
          </div>

          <aside className="flex flex-col gap-3">
            <Panel title={t('opsalert:incidents.sample')}>
              {d.sample == null ? (
                <div className="text-xs text-muted-foreground">{t('opsalert:incidents.sampleNone')}</div>
              ) : (
                <pre className="max-h-52 overflow-auto rounded border border-border bg-muted/40 p-2 font-mono text-[10.5px] leading-relaxed whitespace-pre-wrap break-all">
                  {typeof d.sample === 'string' ? d.sample : JSON.stringify(d.sample, null, 2)}
                </pre>
              )}
            </Panel>

            <Panel title={t('opsalert:incidents.deliveries')}>
              {d.delivery_warning && (
                <div className="mb-2 rounded border border-danger/40 bg-danger-bg px-2 py-1.5 text-2xs text-danger">
                  {d.delivery_warning}
                </div>
              )}
              {truncHint(d.deliveries.length, d.delivery_total, t) && (
                <div className="mb-2 text-2xs text-muted-foreground">
                  {truncHint(d.deliveries.length, d.delivery_total, t)}
                </div>
              )}
              {d.deliveries.length === 0 ? (
                <div className="text-xs text-muted-foreground">{t('opsalert:incidents.deliveriesNone')}</div>
              ) : (
                <ul className="flex flex-col gap-2 text-xs">
                  {groupDeliveries(d.deliveries).map((n, i) => (
                    <li key={i} className="flex flex-wrap items-start gap-x-2 gap-y-1">
                      <Badge tone={n.status === 'sent' ? 'ok' : 'bad'}>
                        {n.status === 'sent'
                          ? t('opsalert:incidents.deliverySucceeded')
                          : t('opsalert:incidents.deliveryFailed')}
                      </Badge>
                      <span className="min-w-0 flex-1 truncate">{n.notifier || n.type}</span>
                      <span className="shrink-0 text-2xs text-muted-foreground">
                        {n.times > 1 ? t('opsalert:incidents.timesN', { n: n.times }) : ''}
                        {n.status === 'sent' ? ` ${n.duration_ms}ms` : ''}
                      </span>
                      {n.status !== 'sent' && n.error && (
                        // ⚠️ 报错串必须能整段选中复制——排查的人要把它贴进工单，
                        // 截断一半的 URL 贴出去等于没贴
                        <span className="w-full select-all break-all font-mono text-2xs text-danger">{n.error}</span>
                      )}
                    </li>
                  ))}
                </ul>
              )}
            </Panel>

            <Panel title={t('opsalert:incidents.similar')}>
              {d.similar.length === 0 ? (
                // "第一次出现"是个结论，不是"没数据"。写清楚它，
                // 因为它直接改变判断：没有历史可参照，只能从头查
                <div className="text-xs text-muted-foreground">{t('opsalert:incidents.similarNone')}</div>
              ) : (
                <ul className="flex flex-col gap-1.5 text-xs">
                  {d.similar.map((s) => (
                    <li key={s.id} className="flex flex-wrap items-baseline gap-x-2">
                      <span className="font-mono text-2xs text-muted-foreground">
                        {new Date(s.first_at).toLocaleDateString()}
                      </span>
                      <span>{t(`opsalert:status.${statusKey(s.status)}`, s.status)}</span>
                      <span className="font-mono text-2xs text-muted-foreground">×{s.count}</span>
                      {/* ⚠️ recovery_sec 缺失 = 那次根本没恢复过，
                          不能显示成 0 分钟（会读成"上次瞬间就好了"） */}
                      <span className="ml-auto text-2xs">
                        {s.recovery_sec == null
                          ? t('opsalert:incidents.neverRecovered')
                          : t('opsalert:incidents.recoveredIn', { minutes: Math.max(1, Math.round(s.recovery_sec / 60)) })}
                      </span>
                    </li>
                  ))}
                </ul>
              )}
            </Panel>
          </aside>
        </div>
      )}
    </div>
  )
}

function Panel({ title, children }: { title: string; children: ReactNode }) {
  return (
    <section className="rounded-lg border border-border bg-card">
      <h3 className="border-b border-border px-3.5 py-2 text-xs font-semibold">{title}</h3>
      <div className="px-3.5 py-3">{children}</div>
    </section>
  )
}
