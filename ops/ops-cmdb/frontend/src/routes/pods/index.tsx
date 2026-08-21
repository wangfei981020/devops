import { toErrorInfo } from '@ops/api'
import { tError, type Locale, useTranslation } from '@ops/i18n'
import {
  AsyncBoundary,
  Button,
  DataTable,
  EmptyState,
  type LoadError,
  type NoValueKind,
  SearchInput,
  Select,
  TableSkeleton,
  fromQuery,
} from '@ops/ui'
import { useNavigate, useSearch } from '@tanstack/react-router'
import { type UsageMap, pickUsage, useLiveUsage } from '../../lib/liveUsage.js'
import { Boxes, ChevronLeft, ChevronRight, X } from 'lucide-react'
import { useMemo, useState } from 'react'
import { podColumns } from './columns.js'
import { PodDiagDialog } from './PodDiagDialog.js'
import type { PodTarget } from './diag.js'
import { type PodHealth, type PodListResult, usePods } from './queries.js'

const PAGE_SIZE = 50

export function PodsPage() {
  const { t, i18n } = useTranslation()
  const locale = i18n.language as Locale
  const { page, cluster, namespace, health, q: keyword, workload, node } = useSearch({
    from: '/k8s/pods',
  })
  const navigate = useNavigate({ from: '/k8s/pods' })
  // 改筛选回到第一页：停在第 5 页而结果只有 2 页，用户看到空列表会以为没数据
  const patch = (
    next: Partial<{
      cluster: string
      namespace: string
      health: PodHealth
      q: string
      workload: string
      node: string
      page: number
    }>,
  ) =>
    void navigate({
      search: (prev) => ({ ...prev, ...next, ...(next.page === undefined ? { page: 1 } : {}) }),
    })

  const query = usePods({
    page,
    size: PAGE_SIZE,
    cluster,
    namespace,
    health,
    q: keyword,
    workload,
    node,
  })

  const labels: Record<NoValueKind, string> = {
    na: t('pods:notScheduled'),
    notIngested: t('common:state.notIngested'),
    stopped: t('common:state.unknown'),
    unknown: t('common:state.unknown'),
  }
  // 排障目标：点行打开日志/事件/诊断。老版有这三个入口，新版一直缺（OPSCMDB-021）
  const [diagFor, setDiagFor] = useState<PodTarget | null>(null)
  // 实时用量。⚠️ 只在筛定了单个集群时查 —— 用量是按集群打 Prometheus 的，
  // 跨集群列表时没有一个"正确的集群"可选，瞎猜一个查回来的数是别人的
  // ⚠️ 集群 id 从**数据行**里取，不要 Number(cluster) ——
  // URL 里的 cluster 是集群**名字**（如「本地 docker-desktop」），
  // Number() 得到 NaN，而 `NaN != null` 为真，于是 noCluster 判定失效、
  // enabled 又是 false，这一列会**永远停在加载态**。
  // 加载态是最坏的一种错：它既不是数据也不是错误，人只会一直等下去。
  const cid =
    cluster !== 'all' ? (query.data?.items?.[0]?.clusterId ?? null) : null
  const live = useLiveUsage('pod', cid)

  const columns = useMemo(
    () => [
      ...podColumns(t, locale, labels),
      {
        // 「现在用了多少」——列表数据来自采集库（几分钟一轮），这一列是实时的。
        // 分开请求：Prometheus 挂了只影响这一列，整张表照常可用
        id: 'live',
        header: t('pods:live.header'),
        cell: ({ row }: { row: { original: { namespace: string; name: string } } }) => (
          <LiveUsageCell
            m={live.data}
            loading={live.isPending && cid != null}
            k={`${row.original.namespace}/${row.original.name}`}
            noCluster={cid == null}
            t={t}
          />
        ),
      },
      {
        id: 'actions',
        header: '',
        // 操作列固定在最右侧。这一页的操作列由页面提供（要用页面上的诊断弹窗状态）
        meta: { action: true },
        cell: ({ row }: { row: { original: { clusterId: number; namespace: string; name: string } } }) => (
          <div className="flex justify-end">
            <Button
              size="sm"
              onClick={() =>
                setDiagFor({
                  clusterId: row.original.clusterId,
                  namespace: row.original.namespace,
                  name: row.original.name,
                })
              }
            >
              {t('pods:diag.entry')}
            </Button>
          </div>
        ),
      },
    ],
    [t, locale, labels, live.data, live.isPending, cid],
  )
  const toLoadError = useMemo(
    () =>
      (e: unknown): LoadError => {
        const n = toErrorInfo(e)
        return { cause: tError(t, n.messageKey, n.params), detail: n.detail, retryable: n.retryable, kind: n.kind === 'license' ? 'license' : undefined, feature: n.params?.feature as string | undefined }
      },
    [t],
  )

  const state = fromQuery<PodListResult>(query, (d) => d.total === 0, toLoadError)
  const filtered =
    keyword !== '' ||
    cluster !== 'all' ||
    namespace !== 'all' ||
    health !== 'all' ||
    workload !== '' ||
    node !== ''

  return (
    <div className="flex flex-col">
      <AsyncBoundary
        state={state}
        errorTitle={t('pods:error.title')}
        retryLabel={t('common:action.retry')}
        onRetry={() => void query.refetch()}
        pending={
          <div className="p-5">
            <TableSkeleton columns={[28, 14, 8, 14, 20, 10]} rows={8} />
          </div>
        }
        empty={
          <EmptyState
            icon={<Boxes />}
            title={t('pods:empty.title')}
            reason={filtered ? t('pods:empty.filtered') : t('pods:empty.noSource')}
            action={
              filtered
                ? {
                    label: t('common:filter.clearAll'),
                    onClick: () =>
                      patch({
                        q: '',
                        cluster: 'all',
                        namespace: 'all',
                        health: 'all',
                        workload: '',
                        node: '',
                      }),
                  }
                : null
            }
          />
        }
      >
        {(data) => {
          const lastPage = Math.max(1, Math.ceil(data.total / PAGE_SIZE))
          const hf = data.facets.health
          // ⚠️ 分面**缺失**（后端那条统计查询失败）与计数为 0 不是一回事。
          // 缺失时不显示计数，而不是显示 0 —— 显示 0 会被读成"确实一条都没有"
          const cnt = (dim: string, k: string): number | undefined => data.facets[dim]?.[k]

          return (
            <>
              <div className="flex flex-wrap items-center gap-2 border-b border-border px-4 py-3">
                <SearchInput
                  value={keyword}
                  onChange={(v) => patch({ q: v })}
                  placeholder={t('pods:filter.searchPlaceholder')}
                  clearLabel={t('common:filter.clearSearch')}
                  className="w-[240px]"
                />
                {/* 🔴 下钻来的过滤必须**显式可见**且可撤销。
                    从工作负载页点进来时列表被缩窄了，如果界面上没有任何痕迹，
                    人会以为"这个集群就这几个 Pod"—— 一个不说明自己存在的过滤器，
                    和一个悄悄返回错误结果的查询是同一类问题。
                    它不做成 Select 是因为取值来自上一页，不是一个可枚举的维度。 */}
                {workload !== '' ? (
                  <FilterChip
                    label={t('pods:filter.workload')}
                    value={workload}
                    onClear={() => patch({ workload: '' })}
                  />
                ) : null}
                {node !== '' ? (
                  <FilterChip
                    label={t('pods:filter.node')}
                    value={node}
                    onClear={() => patch({ node: '' })}
                  />
                ) : null}
                <Select<string>
                  label={t('pods:filter.cluster')}
                  value={cluster}
                  onChange={(v) => patch({ cluster: v })}
                  options={[
                    { value: 'all', label: t('common:filter.all'), count: cnt('cluster', 'all') },
                    ...Object.keys(data.facets.cluster ?? {})
                      .filter((k) => k !== 'all')
                      .sort()
                      .map((c) => ({ value: c, label: c, count: cnt('cluster', c) })),
                  ]}
                />
                <Select<string>
                  label={t('pods:filter.namespace')}
                  value={namespace}
                  onChange={(v) => patch({ namespace: v })}
                  options={[
                    { value: 'all', label: t('common:filter.all'), count: cnt('namespace', 'all') },
                    ...Object.keys(data.facets.namespace ?? {})
                      .filter((k) => k !== 'all')
                      .sort()
                      .map((n) => ({ value: n, label: n, count: cnt('namespace', n) })),
                  ]}
                />
                <Select<PodHealth>
                  label={t('pods:filter.health')}
                  value={health}
                  onChange={(v) => patch({ health: v })}
                  options={[
                    { value: 'all', label: t('common:filter.all'), count: hf?.all },
                    { value: 'bad', label: t('pods:health.bad'), count: hf?.bad },
                    { value: 'restarted', label: t('pods:health.restarted'), count: hf?.restarted },
                    { value: 'ok', label: t('pods:health.ok'), count: hf?.ok },
                  ]}
                />
                <span className="ml-auto text-xs text-muted-foreground">
                  {t('pods:total', { count: data.total })}
                </span>
              </div>

              <DataTable
                data={data.items}
                columns={columns}
                rowKey={(p) => `${p.clusterId}/${p.namespace}/${p.name}`}
              />

              {/* Pod 是十万级，分页是必须的（不像集群/节点那样一页放得下）。
                  后端也在 SQL 里分页，不是取全量再切 */}
              {lastPage > 1 ? (
                <div className="flex items-center justify-end gap-2 border-t border-border px-4 py-2.5">
                  <span className="tabular text-xs text-muted-foreground">
                    {page} / {lastPage}
                  </span>
                  <Button
                    size="sm"
                    disabled={page <= 1}
                    onClick={() => patch({ page: page - 1 })}
                    icon={<ChevronLeft className="size-3.5" />}
                    aria-label={t('common:pagination.prev')}
                  />
                  <Button
                    size="sm"
                    disabled={page >= lastPage}
                    onClick={() => patch({ page: page + 1 })}
                    icon={<ChevronRight className="size-3.5" />}
                    aria-label={t('common:pagination.next')}
                  />
                </div>
              ) : null}
            </>
          )
        }}
      </AsyncBoundary>
      {diagFor ? (
        <PodDiagDialog target={diagFor} onClose={() => setDiagFor(null)} t={t} />
      ) : null}
    </div>
  )
}

/**
 * 一格实时用量。
 *
 * ⚠️ 四种状态必须分开，**没有一种可以显示成 0**：
 *   没选集群 / 查询中 / 查不到（数据源挂了）/ 这个对象没数据。
 * 0% CPU 看起来像"这 Pod 闲着"，有人会拿它去做缩容决策 —— 而真相可能是根本没查到。
 */
function LiveUsageCell({
  m,
  loading,
  k,
  noCluster,
  t,
}: {
  m: UsageMap | undefined
  loading: boolean
  k: string
  noCluster: boolean
  t: (x: string, p?: Record<string, unknown>) => string
}) {
  if (noCluster) {
    return (
      <span className="text-[11px] text-muted-foreground" title={t('pods:live.pickCluster')}>
        —
      </span>
    )
  }
  if (loading) return <span className="text-[11px] text-muted-foreground">…</span>
  if (m && m.ok === false) {
    return (
      <span className="text-[11px] text-warning" title={m.error}>
        {t('pods:live.unavailable')}
      </span>
    )
  }
  const cpu = pickUsage(m, k, 'cpu_m')
  const mem = pickUsage(m, k, 'mem_mi')
  if (cpu === undefined && mem === undefined) {
    return <span className="text-[11px] text-muted-foreground">—</span>
  }
  return (
    <span className="tabular text-[11px] whitespace-nowrap text-foreground">
      {cpu !== undefined ? `${Math.round(cpu)}m` : '—'} / {mem !== undefined ? `${Math.round(mem)}Mi` : '—'}
    </span>
  )
}


/**
 * 下钻过滤的可见标记。
 *
 * 从别的页面带过来的过滤条件（工作负载 / 节点）没有可枚举的取值域，
 * 做不成下拉，但**必须看得见**：看不见的过滤会让缩窄后的列表
 * 被读成"总共就这么多"。带一个叉，让人能一步退回全量。
 */
function FilterChip({
  label,
  value,
  onClear,
}: {
  label: string
  value: string
  onClear: () => void
}) {
  return (
    <span className="inline-flex h-8 items-center gap-1.5 rounded-[var(--radius)] border border-brand/40 bg-brand/10 px-2.5 text-[13px] whitespace-nowrap">
      <span className="text-muted-foreground">{label}</span>
      <span className="font-mono text-xs" title={value}>
        {value}
      </span>
      <button
        type="button"
        onClick={onClear}
        aria-label={`${label}: ${value}`}
        title={label}
        className="cursor-pointer text-muted-foreground hover:text-foreground"
      >
        <X className="size-3.5" />
      </button>
    </span>
  )
}
