import { toErrorInfo } from '@ops/api'
import { clusterLabel } from '../../lib/clusterLabel.js'
import { tError, useTranslation } from '@ops/i18n'
import {
  AsyncBoundary,
  Badge,
  EmptyState,
  type LoadError,
  Pagination,
  SearchInput,
  Select,
  Skeleton,
  fromQuery,
} from '@ops/ui'
import { useNavigate, useSearch } from '@tanstack/react-router'
import { Coins } from 'lucide-react'
import { useMemo } from 'react'
import { useClusters } from '../clusters/queries.js'
import { type WasteData, type WasteRow, type WasteSummary, useWaste } from './queries.js'

type SortKey = 'cpu_waste' | 'mem_waste' | 'cpu_pct' | 'mem_pct'

/**
 * 闲置与浪费。
 *
 * # 🔴 这一页的每个数字都会被拿去做缩容决定
 *
 * 所以三条纪律在这里是硬要求：
 *
 * 1. **没有值就显示「—」，绝不显示 0**。`?? 0` 在这一页是危险代码：
 *    「申请 0」会被读成"没设 request"，而真相可能是"我们没取到这个字段"。
 * 2. **申请和实测必须都在**。只有一个的时候不要算百分比 ——
 *    分母缺失时算出来的比例是编的。
 * 3. **建议值要说清是单副本还是总量**。3 个副本 × 350m 和 350m 差三倍，
 *    照错的那个改会直接把服务压死。
 */
export function WastePage() {
  const { t } = useTranslation()
  const search = useSearch({ from: '/cost/waste' })
  const { cluster, q: keyword, page, size, sort } = search
  const navigate = useNavigate({ from: '/cost/waste' })
  const clusters = useClusters({ page: 1, size: 100 })
  const list = clusters.data?.items ?? []

  /**
   * 默认集群：优先生产。
   *
   * ⚠️ 原来取 `list[0]`，而列表第一个是 DEV —— 一个生产 CMDB 打开
   * 「闲置与浪费」默认给你看开发环境的浪费，而开发环境本来就该是超配的。
   * 更糟的是没人会注意到自己看的是哪个集群（P1-51）。
   */
  const current = useMemo(() => {
    if (cluster > 0) return cluster
    return (list.find((c) => c.environment === 'PROD') ?? list[0])?.id ?? 0
  }, [cluster, list])

  const query = useWaste(current)
  const setSearch = (patch: Record<string, string | number>) =>
    void navigate({ search: (s) => ({ ...s, ...patch }) })

  const toLoadError = (e: unknown): LoadError => {
    const n = toErrorInfo(e)
    return { cause: tError(t, n.messageKey, n.params), detail: n.detail, retryable: n.retryable, kind: n.kind === 'license' ? 'license' : undefined, feature: n.params?.feature as string | undefined }
  }

  return (
    <div className="flex flex-col">
      {/* ⚠️ 工具条在 AsyncBoundary 外面：筛出 0 条时改条件的控件不能跟着消失 */}
      <div className="flex flex-wrap items-center gap-2 border-b border-border px-4 py-3">
        <Select<string>
          label={t('waste:filter.cluster')}
          value={String(current)}
          onChange={(v) => setSearch({ cluster: Number(v), page: 1 })}
          // 别名和原名一起给：只显示别名时，四个选项混着两套命名，
          // 没人分得清「开发环境集群」对应接口里的哪一个
          options={list.map((c) => ({
            value: String(c.id),
            label:
              clusterLabel(c.displayName, c.name),
          }))}
        />
        <SearchInput
          value={keyword}
          onChange={(v) => setSearch({ q: v, page: 1 })}
          placeholder={t('waste:filter.searchPlaceholder')}
          clearLabel={t('common:filter.clearSearch')}
          className="w-[260px]"
        />
        <Select<string>
          label={t('waste:filter.sort')}
          value={sort}
          onChange={(v) => setSearch({ sort: v, page: 1 })}
          options={[
            { value: 'cpu_waste', label: t('waste:sort.cpuWaste') },
            { value: 'mem_waste', label: t('waste:sort.memWaste') },
            { value: 'cpu_pct', label: t('waste:sort.cpuPct') },
            { value: 'mem_pct', label: t('waste:sort.memPct') },
          ]}
        />
        {/* 这一页的前提要写在最显眼处：没有实测用量就没有"浪费"可谈 */}
        <span className="text-xs text-muted-foreground">{t('waste:needsMetrics')}</span>
      </div>

      <div className="mx-auto w-full max-w-[1100px] p-5">
        <AsyncBoundary
          state={fromQuery<WasteData>(query, (d) => d.items.length === 0, toLoadError)}
          errorTitle={t('waste:error.title')}
          retryLabel={t('common:action.retry')}
          onRetry={() => void query.refetch()}
          pending={<Skeleton className="h-5 w-[70%]" />}
          empty={
            <EmptyState
              icon={<Coins />}
              title={t('waste:empty.title')}
              // ⚠️ 空态说的是"没接指标源"，不是"没有浪费"。
              // 后者是一个我们根本没资格下的结论
              reason={query.data?.note || query.data?.error || t('waste:empty.noMetrics')}
              action={null}
            />
          }
        >
          {(d) => {
            const kw = keyword.trim().toLowerCase()
            const rows = [...d.items]
              .filter((r) =>
                kw ? `${r.namespace ?? ''}/${r.workload ?? ''}`.toLowerCase().includes(kw) : true,
              )
              .sort(sorters[sort as SortKey] ?? sorters.cpu_waste)
            const from = (page - 1) * size
            const pageRows = rows.slice(from, from + size)
            return (
              <div className="flex flex-col gap-4">
                {/* ⚠️ 汇总是这一页存在的目的。
                    后端一直在算 summary，前端一个字都没显示（P1-49）——
                    于是"一共浪费了 49.96 核"这个唯一能拿去汇报的数字不存在 */}
                <SummaryCard s={d.summary} t={t} />

                <section className="rounded-[var(--radius-lg)] border border-border bg-card">
                  <div className="flex flex-wrap items-baseline gap-2 border-b border-border px-4 py-2.5">
                    <h2 className="text-sm font-semibold text-foreground">{t('waste:title')}</h2>
                    <span className="text-xs text-muted-foreground">
                      {t('waste:rowCount', { shown: rows.length, total: d.items.length })}
                    </span>
                    {d.note ? (
                      <span className="text-xs text-muted-foreground">{d.note}</span>
                    ) : null}
                  </div>
                  <div className="overflow-x-auto">
                    <table className="w-full text-[12px]">
                      <thead>
                        <tr className="border-b border-border text-left text-[11px] text-muted-foreground">
                          <th className="px-3 py-2 font-medium">{t('waste:col.namespace')}</th>
                          <th className="px-3 py-2 font-medium">{t('waste:col.workload')}</th>
                          <th className="px-3 py-2 text-right font-medium">
                            {t('waste:col.replicas')}
                          </th>
                          <th className="px-3 py-2 text-right font-medium">{t('waste:col.cpu')}</th>
                          <th className="px-3 py-2 text-right font-medium">{t('waste:col.mem')}</th>
                          <th className="px-3 py-2 text-right font-medium">
                            {t('waste:col.suggest')}
                          </th>
                        </tr>
                      </thead>
                      <tbody>
                        {pageRows.map((r, i) => (
                          <Row key={`${r.namespace}/${r.workload}/${i}`} r={r} t={t} />
                        ))}
                      </tbody>
                    </table>
                  </div>
                  {/* 504 行一次性平铺没人看得完（P2-45） */}
                  <Pagination
                    page={page}
                    size={size}
                    total={rows.length}
                    onPage={(p) => setSearch({ page: p })}
                    onSize={(s) => setSearch({ size: s, page: 1 })}
                    rangeLabel={(f, to, total) =>
                      t('common:pagination.range', { from: f, to, total })
                    }
                    totalLabel={(total) => t('common:pagination.total', { count: total })}
                    perPageLabel={t('common:pagination.perPage')}
                    prevLabel={t('common:pagination.prev')}
                    nextLabel={t('common:pagination.next')}
                  />
                </section>
              </div>
            )
          }}
        </AsyncBoundary>
      </div>
    </div>
  )
}

type T = (k: string, p?: Record<string, unknown>) => string

/** 浪费量 = 申请 − 实测。两个都得有值才算，缺一个就排到最后而不是算成 0 */
function wasteOf(req?: number, used?: number) {
  if (req == null || used == null) return Number.NEGATIVE_INFINITY
  return req - used
}

const sorters: Record<SortKey, (a: WasteRow, b: WasteRow) => number> = {
  cpu_waste: (a, b) =>
    wasteOf(b.cpu_req_m, b.cpu_used_m) - wasteOf(a.cpu_req_m, a.cpu_used_m),
  mem_waste: (a, b) =>
    wasteOf(b.mem_req_mi, b.mem_used_mi) - wasteOf(a.mem_req_mi, a.mem_used_mi),
  // 使用率**升序**：越低越浪费，要排在前面
  cpu_pct: (a, b) => (a.cpu_usage_pct ?? 101) - (b.cpu_usage_pct ?? 101),
  mem_pct: (a, b) => (a.mem_usage_pct ?? 101) - (b.mem_usage_pct ?? 101),
}

/**
 * 全集群汇总。
 *
 * ⚠️ summary 为 null 时**不要渲染一堆 0**。
 * 「浪费 0 核」等于告诉人这个集群很健康，而真相是我们没拿到汇总。
 */
function SummaryCard({ s, t }: { s: WasteSummary | null; t: T }) {
  if (!s) {
    return (
      <p className="text-xs text-muted-foreground">{t('waste:summary.missing')}</p>
    )
  }
  return (
    <section className="rounded-[var(--radius-lg)] border border-border bg-card p-4">
      <div className="flex flex-wrap items-baseline gap-2">
        <h2 className="text-sm font-semibold text-foreground">{t('waste:summary.title')}</h2>
        {/* 建议值的依据要写出来：不说系数，那些建议数字就没有来处 */}
        {s.suggest_factor != null ? (
          <span className="text-xs text-muted-foreground">
            {t('waste:summary.factor', { f: s.suggest_factor })}
          </span>
        ) : null}
      </div>
      <div className="mt-3 grid grid-cols-1 gap-3 sm:grid-cols-2">
        <Stat
          label={t('waste:summary.cpu')}
          req={fmt(s.cpu_request_cores, 'cores')}
          used={fmt(s.cpu_used_cores, 'cores')}
          wasted={fmt(s.cpu_wasted_cores, 'cores')}
          pct={s.cpu_usage_pct}
          t={t}
        />
        <Stat
          label={t('waste:summary.mem')}
          req={fmt(s.mem_request_gi, 'Gi')}
          used={fmt(s.mem_used_gi, 'Gi')}
          wasted={fmt(s.mem_wasted_gi, 'Gi')}
          pct={s.mem_usage_pct}
          t={t}
        />
      </div>
    </section>
  )
}

function Stat({
  label,
  req,
  used,
  wasted,
  pct,
  t,
}: {
  label: string
  req: string
  used: string
  wasted: string
  pct?: number
  t: T
}) {
  return (
    <div className="rounded-[var(--radius)] border border-border p-3">
      <div className="flex flex-wrap items-baseline gap-2">
        <span className="text-xs text-muted-foreground">{label}</span>
        {/* 使用率高低的含义相反：低=超配可以砍，高=贴着上限不能砍。
            所以低于 30% 标黄（可优化），高于 80% 标红（别动它） */}
        {pct != null ? (
          <Badge tone={pct >= 80 ? 'bad' : pct < 30 ? 'warn' : 'ok'}>{pct}%</Badge>
        ) : (
          <Badge tone="mute">—</Badge>
        )}
      </div>
      <p className="mt-1.5 text-[13px] text-foreground">
        {t('waste:summary.line', { req, used })}
      </p>
      <p className="tabular mt-0.5 text-[13px] text-warning">
        {t('waste:summary.wasted', { v: wasted })}
      </p>
    </div>
  )
}

function Row({ r, t }: { r: WasteRow; t: T }) {
  return (
    <tr className="border-b border-border/60 last:border-0">
      {/* ⚠️ 命名空间和工作负载分列。合成一格既没法单独排序，
          也让「同一个 ns 下有多少个超配」看不出来（P2-46） */}
      <td className="px-3 py-1.5 font-mono text-muted-foreground">{r.namespace || '—'}</td>
      <td className="px-3 py-1.5 font-mono text-foreground">{r.workload || '—'}</td>
      <td className="tabular px-3 py-1.5 text-right">{r.replicas ?? '—'}</td>
      <td className="tabular px-3 py-1.5 text-right">
        <Pair used={r.cpu_used_m} req={r.cpu_req_m} unit="m" pct={r.cpu_usage_pct} />
      </td>
      <td className="tabular px-3 py-1.5 text-right">
        <Pair used={r.mem_used_mi} req={r.mem_req_mi} unit="Mi" pct={r.mem_usage_pct} />
      </td>
      <td className="px-3 py-1.5 text-right">
        {/* 没配 request 的没有建议值，后端给了 note 说明原因 —— 显示它而不是「—」 */}
        {r.note ? (
          <span className="text-[11px] text-warning">{r.note}</span>
        ) : (
          <span className="tabular text-foreground">
            {t('waste:suggestPair', {
              cpu: r.suggest_cpu_req_m ?? '—',
              mem: r.suggest_mem_req_mi ?? '—',
            })}
          </span>
        )}
      </td>
    </tr>
  )
}

/**
 * 「实测 / 申请」一对数。
 *
 * ⚠️ 这里绝不能用 `?? 0`。
 * 申请值缺失显示成 0 会被读成"没设 request，赶紧加"；
 * 实测值缺失显示成 0 会被读成"没人用，砍掉" —— 后者会误删在跑的服务。
 * 缺就显示「—」，让人知道这个数字**不存在**，而不是等于零。
 */
function Pair({
  used,
  req,
  unit,
  pct,
}: {
  used?: number
  req?: number
  unit: string
  pct?: number
}) {
  const lowUsage = pct != null && pct < 30
  return (
    <span className="inline-flex items-baseline gap-1">
      <span className={lowUsage ? 'text-warning' : 'text-foreground'}>
        {used == null ? '—' : used}
      </span>
      <span className="text-muted-foreground">/</span>
      <span className="text-muted-foreground">
        {req == null ? '—' : req}
        {unit}
      </span>
      {/* 百分比只在申请和实测都有时才有意义 */}
      {pct != null && used != null && req != null ? (
        <span className={`ml-0.5 ${pct >= 80 ? 'text-danger' : 'text-muted-foreground'}`}>
          {pct}%
        </span>
      ) : null}
    </span>
  )
}

function fmt(v: number | undefined, unit: string) {
  return v == null ? '—' : `${v} ${unit}`
}
