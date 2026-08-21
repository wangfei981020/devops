import { keyedText } from '../../lib/hintText.js'
import { tError } from '@ops/i18n'
import { toErrorInfo } from '@ops/api'
import { Badge, Banner, Button, Dialog, Select, Skeleton } from '@ops/ui'
import { useState } from 'react'
import {
  type CostItem,
  type Mover,
  useCostAttribution,
  useCostDetail,
  useCostMonths,
  useCostReport,
  useIdleCost,
} from './detail.js'

type TFn = (k: string, o?: Record<string, unknown>) => string
type View = 'report' | 'attribution' | 'detail' | 'idle'

const usd = (n?: number) => `US$${(n ?? 0).toFixed(2)}`

/**
 * 成本下钻：报表 / 归因 / 明细 / 闲置。
 *
 * 总览页只回答「这个月多少钱」。这四档回答的是后面那三个问题：
 * **花在谁头上**（报表按维度分组）、**为什么比上月贵**（归因给出 movers）、
 * **具体哪一条**（明细到 Pod/磁盘）、**哪些是白买的**（闲置）。
 *
 * ⚠️ 所有数字都是**按机型与磁盘估算**，不是云账单。这句话每一档都要带着，
 * 否则会被拿去对账，然后"对不上"变成对整个成本模块的不信任。
 */
export function CostDetailDialog({ onClose, t }: { onClose: () => void; t: TFn }) {
  const months = useCostMonths()
  const [month, setMonth] = useState('')
  const [view, setView] = useState<View>('report')
  const list = months.data ?? []
  const cur = month || list[0] || ''

  const views: { key: View; label: string }[] = [
    { key: 'report', label: t('cost:drill.report') },
    { key: 'attribution', label: t('cost:drill.attribution') },
    { key: 'detail', label: t('cost:drill.detail') },
    { key: 'idle', label: t('cost:drill.idle') },
  ]

  return (
    <Dialog
      open
      onClose={onClose}
      title={t('cost:drill.title')}
      description={t('cost:drill.estimateNote')}
      closeLabel={t('common:action.close')}
      width={1180}
      footer={
        <Button size="sm" onClick={onClose}>
          {t('common:action.close')}
        </Button>
      }
    >
      <div className="-mx-4 -mt-1 flex flex-wrap items-center gap-1.5 border-b border-border px-4 pb-2.5">
        {views.map((v) => (
          <Button
            key={v.key}
            size="sm"
            variant={view === v.key ? 'primary' : undefined}
            onClick={() => setView(v.key)}
          >
            {v.label}
          </Button>
        ))}
        {/* 闲置和明细都是"当前"状态，没有月份维度 —— 选月份对它无意义，所以这两档不显示。
            ⚠️ 明细原来是显示的：它照常发 month= 而 handler 根本不读，
               于是选 7 月看到的仍是当前值 —— 一个静默的"答非所问"（OPSCMDB-049）。 */}
        {view !== 'idle' && view !== 'detail' && list.length > 0 ? (
          <Select<string>
            label={t('cost:drill.month')}
            value={cur}
            onChange={setMonth}
            options={list.map((m) => ({ value: m, label: m }))}
          />
        ) : null}
      </div>

      <div className="-mx-4">
        {view === 'report' ? <ReportView month={cur} t={t} /> : null}
        {view === 'attribution' ? <AttributionView month={cur} t={t} /> : null}
        {view === 'detail' ? <DetailView t={t} /> : null}
        {view === 'idle' ? <IdleView t={t} /> : null}
      </div>
    </Dialog>
  )
}

function Failed({ e, t }: { e: unknown; t: TFn }) {
  const n = toErrorInfo(e)
  return (
    <div className="px-4 py-3">
      <Banner tone="bad">
        <span className="font-medium">{t('cost:drill.loadFailed')}</span>
        <span className="mt-0.5 block">{n.detail || tError(t, n.messageKey, n.params)}</span>
      </Banner>
    </div>
  )
}

const Loading = () => (
  <div className="flex flex-col gap-2 px-4 py-3">
    <Skeleton className="h-4 w-[60%]" />
    <Skeleton className="h-4 w-[40%]" />
  </div>
)

function ReportView({ month, t }: { month: string; t: TFn }) {
  const q = useCostReport(month)
  if (q.isPending) return <Loading />
  if (q.isError) return <Failed e={q.error} t={t} />
  const d = q.data
  const delta = d?.delta ?? 0

  // 🔴 后端回的 anchor 是**实际生效**的月份，未必等于用户选的那个。
  //	两者不一致时必须说出来 —— 这正是那个 bug 藏了这么久的原因：
  //	参数名传错被静默忽略，界面照常显示一个数字，没人知道它是别的月份的
  //	（实测选 7 月显示的是 8 月的 842.65，而 7 月真实 2705.7）。
  const mismatched = !!d?.anchor && d.anchor !== month

  return (
    <div className="flex flex-col gap-3 px-4 py-3">
      {mismatched ? (
        <Banner tone="warn">
          <span>{t('cost:drill.anchorMismatch', { asked: month, got: d?.anchor })}</span>
        </Banner>
      ) : null}
      <div className="flex flex-wrap items-baseline gap-3">
        <span className="text-lg font-semibold text-foreground">{usd(d?.total)}</span>
        {/* 数字旁边标清是哪个月的：一个孤零零的金额没法自证属于哪段时间 */}
        <span className="text-xs text-muted-foreground">{d?.anchor ?? month}</span>
        {/* 环比要标方向：只给一个绝对值看不出是涨了还是省了 */}
        <Badge tone={delta > 0 ? 'bad' : delta < 0 ? 'ok' : 'mute'}>
          {delta > 0 ? '+' : ''}
          {usd(delta)}
        </Badge>
        <span className="text-xs text-muted-foreground">
          {t('cost:drill.vsPrev', { prev: usd(d?.prev_total) })}
        </span>
      </div>

      <section>
        <h3 className="mb-1.5 text-[11px] font-medium tracking-wide text-muted-foreground uppercase">
          {t('cost:drill.byDim', { dim: d?.dim ?? '' })}
        </h3>
        <div className="overflow-x-auto rounded-[var(--radius)] border border-border">
          <table className="w-full text-[13px]">
            <tbody>
              {(d?.groups ?? []).map((g) => (
                <tr key={g.name} className="border-b border-border last:border-0">
                  <td className="px-3 py-1.5">{g.name || '—'}</td>
                  <td className="tabular px-3 py-1.5 text-right">{usd(g.cost)}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </section>

      {(d?.trend?.length ?? 0) > 0 ? (
        <section>
          <h3 className="mb-1.5 text-[11px] font-medium tracking-wide text-muted-foreground uppercase">
            {t('cost:drill.trend')}
          </h3>
          {/* 不画图：这一列月份数不多，数字比折线更好对账，也不用引图表库 */}
          <div className="flex flex-wrap gap-2 text-xs">
            {d?.trend
              ?.filter((p) => (p.cost ?? 0) > 0)
              .map((p) => (
                <span key={p.month} className="rounded border border-border px-2 py-1">
                  <span className="text-muted-foreground">{p.month}</span>{' '}
                  <span className="tabular font-medium">{usd(p.cost)}</span>
                </span>
              ))}
          </div>
        </section>
      ) : null}
    </div>
  )
}

function AttributionView({ month, t }: { month: string; t: TFn }) {
  // 默认 resource：后端原本就是资源级归因，选择器此前是**无效**的
  // （dim 传了但没读）。默认值改成实际生效的那个，避免"一打开就和显示不符"
  const [dim, setDim] = useState('resource')
  const q = useCostAttribution(month, dim)

  return (
    <div className="flex flex-col">
      <div className="flex items-center gap-2 border-b border-border px-4 py-2">
        <Select<string>
          label={t('cost:drill.dim')}
          value={dim}
          onChange={setDim}
          options={[
            { value: 'resource', label: t('cost:drill.dimResource') },
            { value: 'project', label: t('cost:drill.dimProject') },
            { value: 'cluster', label: t('cost:drill.dimCluster') },
            { value: 'type', label: t('cost:drill.dimType') },
          ]}
        />
      </div>
      {q.isPending ? (
        <Loading />
      ) : q.isError ? (
        <Failed e={q.error} t={t} />
      ) : (
        <div className="px-4 py-3">
          <p className="mb-2 text-[13px] text-foreground">
            {t('cost:drill.deltaTotal', { delta: usd(q.data?.delta) })}
          </p>
          <div className="overflow-x-auto rounded-[var(--radius)] border border-border">
            <table className="w-full text-[13px]">
              <thead className="bg-card">
                <tr className="border-b border-border text-left text-xs text-muted-foreground">
                  <th className="px-3 py-2 font-medium">{t('cost:drill.col.resource')}</th>
                  <th className="px-3 py-2 font-medium">{t('cost:drill.col.owner')}</th>
                  <th className="px-3 py-2 text-right font-medium">{t('cost:drill.col.old')}</th>
                  <th className="px-3 py-2 text-right font-medium">{t('cost:drill.col.new')}</th>
                  <th className="px-3 py-2 text-right font-medium">{t('cost:drill.col.delta')}</th>
                  <th className="px-3 py-2 font-medium">{t('cost:drill.col.reason')}</th>
                </tr>
              </thead>
              <tbody>
                {(q.data?.movers ?? []).map((m: Mover, i) => (
                  <tr key={`${m.resource}-${i}`} className="border-b border-border last:border-0">
                    <td className="px-3 py-1.5 font-mono text-xs">{m.resource || '—'}</td>
                    <td className="px-3 py-1.5 text-xs">{m.project || m.cluster || '—'}</td>
                    <td className="tabular px-3 py-1.5 text-right text-xs">{usd(m.old)}</td>
                    <td className="tabular px-3 py-1.5 text-right text-xs">{usd(m.new)}</td>
                    <td className="tabular px-3 py-1.5 text-right">
                      <Badge tone={(m.delta ?? 0) > 0 ? 'bad' : 'ok'}>
                        {(m.delta ?? 0) > 0 ? '+' : ''}
                        {usd(m.delta)}
                      </Badge>
                    </td>
                    {/* 原因由后端给：前端另编一套说法会和账对不上 */}
                    <td className="px-3 py-1.5 text-xs text-muted-foreground">
                      {keyedText(t, m, 'reason') || '—'}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </div>
      )}
    </div>
  )
}

function DetailView({ t }: { t: TFn }) {
  // ⚠️ 明细是**当前**的实时估算（从 k8s_pods / hosts 现算），没有月份维度。
  //	接口也不接受 month —— 传了会被静默忽略。要看历史月份用「报表」或「归因」。
  const q = useCostDetail()
  if (q.isPending) return <Loading />
  if (q.isError) return <Failed e={q.error} t={t} />
  const items = q.data?.items ?? []
  // 按金额倒序：明细几十上百条，人只关心最贵的那几条
  const sorted = [...items].sort((a, b) => (b.cost ?? 0) - (a.cost ?? 0))

  return (
    <div className="flex flex-col">
      <p className="px-4 py-2 text-xs text-muted-foreground">
        {t('cost:drill.detailCount', { n: q.data?.count ?? 0, total: usd(q.data?.total) })}
      </p>
      <div className="max-h-[52vh] overflow-auto">
        <table className="w-full text-[13px]">
          <thead className="sticky top-0 z-10 bg-card">
            <tr className="border-b border-border text-left text-xs text-muted-foreground">
              <th className="px-4 py-2 font-medium">{t('cost:drill.col.object')}</th>
              <th className="px-4 py-2 font-medium">{t('cost:drill.col.cluster')}</th>
              <th className="px-4 py-2 font-medium">{t('cost:drill.col.owner')}</th>
              <th className="px-4 py-2 font-medium">{t('cost:drill.col.type')}</th>
              <th className="px-4 py-2 text-right font-medium">{t('cost:drill.col.cost')}</th>
            </tr>
          </thead>
          <tbody>
            {sorted.map((it: CostItem, i) => (
              <tr key={`${it.name}-${i}`} className="border-b border-border last:border-0">
                <td className="px-4 py-1.5">
                  <span className="font-mono text-xs">{it.name || '—'}</span>
                  {it.namespace ? (
                    <span className="ml-1.5 text-[11px] text-muted-foreground">{it.namespace}</span>
                  ) : null}
                </td>
                <td className="px-4 py-1.5 text-xs text-muted-foreground">{it.cluster || '—'}</td>
                <td className="px-4 py-1.5 text-xs">{it.biz_project || it.gcp_project || '—'}</td>
                <td className="px-4 py-1.5 text-xs text-muted-foreground">{it.type || '—'}</td>
                <td className="tabular px-4 py-1.5 text-right text-xs">{usd(it.cost)}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </div>
  )
}

function IdleView({ t }: { t: TFn }) {
  const q = useIdleCost(0)
  if (q.isPending) return <Loading />
  if (q.isError) return <Failed e={q.error} t={t} />
  const rows = q.data?.clusters ?? []

  return (
    <div className="flex flex-col gap-2 px-4 py-3">
      {rows.map((c) => (
        <section key={c.cluster_id} className="rounded-[var(--radius)] border border-border p-3">
          <div className="flex flex-wrap items-baseline gap-2.5">
            <span className="text-[13px] font-semibold text-foreground">{c.cluster}</span>
            <Badge tone={(c.idle_pct ?? 0) >= 50 ? 'bad' : (c.idle_pct ?? 0) >= 25 ? 'warn' : 'mute'}>
              {t('cost:drill.idlePct', { pct: (c.idle_pct ?? 0).toFixed(1) })}
            </Badge>
            <span className="text-xs text-muted-foreground">
              {t('cost:drill.idleAmount', {
                monthly: usd(c.idle_monthly_usd),
                yearly: usd(c.idle_yearly_usd),
              })}
            </span>
          </div>
          <div className="mt-1 flex flex-wrap gap-3 text-xs text-muted-foreground">
            <span>{t('cost:drill.actual', { v: usd(c.actual_monthly_usd) })}</span>
            <span>{t('cost:drill.allocated', { v: usd(c.allocated_monthly_usd) })}</span>
            <span>CPU request {(c.cpu_request_pct ?? 0).toFixed(1)}%</span>
            <span>Mem request {(c.mem_request_pct ?? 0).toFixed(1)}%</span>
          </div>
          {/* ⚠️ 口径说明必须原样带出：闲置=买了没分配，不等于"分配了没用"。
              两者混淆会得出相反的结论 */}
          {c.note ? <p className="mt-1.5 text-xs text-muted-foreground">{c.note}</p> : null}
        </section>
      ))}
    </div>
  )
}
