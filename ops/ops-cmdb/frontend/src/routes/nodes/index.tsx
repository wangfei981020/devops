import { toErrorInfo } from '@ops/api'
import { tError, type Locale, useTranslation } from '@ops/i18n'
import {
  AsyncBoundary,
  DataTable,
  EmptyState,
  type LoadError,
  type NoValueKind,
  SearchInput,
  Select,
  Pagination,
  TableSkeleton,
  fromQuery,
} from '@ops/ui'
import { useNavigate, useSearch } from '@tanstack/react-router'
import { type UsageMap, pickUsage, useLiveUsage } from '../../lib/liveUsage.js'
import { Server } from 'lucide-react'
import { useMemo } from 'react'
import { nodeColumns } from './columns.js'
import { type NodeListResult, type NodeStatusFilter, useNodes } from './queries.js'

const PAGE_SIZE = 50

export function NodesPage() {
  const { t, i18n } = useTranslation()
  const locale = i18n.language as Locale
  const { cluster, status, pool, page, size, q: keyword } = useSearch({ from: '/k8s/nodes' })
  const navigate = useNavigate({ from: '/k8s/nodes' })
  const patch = (next: Partial<{ cluster: string; status: NodeStatusFilter; pool: string; q: string; page: number; size: number }>) =>
    void navigate({ search: (prev) => ({ ...prev, ...next }) })

  const query = useNodes({ page, size, cluster, status, pool, q: keyword })

  const labels: Record<NoValueKind, string> = {
    na: '—',
    notIngested: t('common:state.notIngested'),
    stopped: t('common:state.unknown'),
    unknown: t('common:state.unknown'),
  }
  // 实时用量（node-exporter）。只在筛定单集群时查，理由同 Pod 页
  // ⚠️ 集群 id 从**数据行**里取，不要 Number(cluster) ——
  // URL 里的 cluster 是集群**名字**（如「本地 docker-desktop」），
  // Number() 得到 NaN，而 `NaN != null` 为真，于是 noCluster 判定失效、
  // enabled 又是 false，这一列会**永远停在加载态**。
  // 加载态是最坏的一种错：它既不是数据也不是错误，人只会一直等下去。
  const cid =
    cluster !== 'all' ? (query.data?.items?.[0]?.clusterId ?? null) : null
  const live = useLiveUsage('node', cid)

  const columns = useMemo(
    () => [
      ...nodeColumns(
        t,
        locale,
        labels,
        (ciId) => {
          // 跳到主机页并直接打开那台的抽屉 —— 两页互链的具体形态。
          // 用整页跳转而不是 router.navigate：跨路由带 search 参数时
          // 类型推导会成环（见 router.tsx 里 RedirectToDefault 的注释）
          window.location.href = `/resources/hosts?detail=${ciId}`
        },
        // 实时用量单元格由这里注入：它依赖页面上的 live 查询状态，
        // 而列的**顺序**归列定义文件管 —— 两件事分开
        ({ node }) => (
          <NodeLiveCell
            m={live.data}
            loading={live.isPending && cid != null}
            k={node}
            noCluster={cid == null}
            t={t}
          />
        ),
      ),
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

  const state = fromQuery<NodeListResult>(query, (d) => d.total === 0, toLoadError)
  const filtered = keyword !== '' || cluster !== 'all' || status !== 'all'

  return (
    <div className="flex flex-col">
      <AsyncBoundary
        state={state}
        errorTitle={t('nodes:error.title')}
        retryLabel={t('common:action.retry')}
        onRetry={() => void query.refetch()}
        pending={
          <div className="p-5">
            <TableSkeleton columns={[24, 12, 12, 12, 10, 12, 12, 10]} rows={6} />
          </div>
        }
        empty={
          <EmptyState
            icon={<Server />}
            title={t('nodes:empty.title')}
            reason={filtered ? t('nodes:empty.filtered') : t('nodes:empty.noSource')}
            action={
              filtered
                ? {
                    label: t('common:filter.clearAll'),
                    onClick: () => patch({ q: '', cluster: 'all', status: 'all', pool: 'all' }),
                  }
                : null
            }
          />
        }
      >
        {(data) => {
          const clusters = Object.keys(data.facets.cluster ?? {})
            .filter((k) => k !== 'all')
            .sort()
          const sf = data.facets.status ?? {}
          const staleCount = sf.stale ?? 0
          const pools = Object.keys(data.facets.pool ?? {})
            .filter((k) => k !== 'all')
            .sort()

          return (
            <>
              <div className="flex flex-wrap items-center gap-2 border-b border-border px-4 py-3">
                <SearchInput
                  value={keyword}
                  onChange={(v) => patch({ q: v })}
                  placeholder={t('nodes:filter.searchPlaceholder')}
                  clearLabel={t('common:filter.clearSearch')}
                  className="w-[228px]"
                />
                <Select<string>
                  label={t('nodes:filter.cluster')}
                  value={cluster}
                  onChange={(v) => patch({ cluster: v })}
                  options={[
                    { value: 'all', label: t('common:filter.all'), count: sfCount(data, 'cluster') },
                    ...clusters.map((c) => ({
                      value: c,
                      label: c,
                      count: data.facets.cluster?.[c] ?? 0,
                    })),
                  ]}
                />
                <Select<NodeStatusFilter>
                  label={t('nodes:filter.status')}
                  value={status}
                  onChange={(v) => patch({ status: v })}
                  options={[
                    { value: 'all', label: t('common:filter.all'), count: sf.all ?? 0 },
                    { value: 'ready', label: 'Ready', count: sf.ready ?? 0 },
                    { value: 'notready', label: 'NotReady', count: sf.notready ?? 0 },
                    // 🔴 「有压力」是独立一档，不是 Ready 的一种。
                    //	磁盘 100%、正在驱逐 Pod 的节点，ready_status 依然是 Ready ——
                    //	归进 Ready 的话，最危险的那一段（快撑不住但还没倒）
                    //	就永远筛不出来（OPSCMDB-042：它把 Jenkins 构建拖到 12 分钟拉不下镜像，
                    //	而节点列表显示「正常」）
                    { value: 'pressure', label: t('nodes:status.pressure'), count: sf.pressure ?? 0 },
                    // 单独一档：它不是 NotReady 的一种，
                    // 而是"我们根本不知道它现在什么状态"。
                    // ⚠️ 这一档同时包含「节点心跳停了」和「CMDB 采集停了」两种，
                    // 所以标签只能说「状态不可信」——写成「失联」会把采集故障
                    // 说成节点故障，把人指向错的排查方向
                    { value: 'stale', label: t('nodes:status.untrusted'), count: staleCount },
                  ]}
                />
                {pools.length > 0 ? (
                  <Select<string>
                    label={t('nodes:filter.pool')}
                    value={pool}
                    onChange={(v) => patch({ pool: v })}
                    options={[
                      { value: 'all', label: t('common:filter.all'), count: data.facets.pool?.all ?? 0 },
                      ...pools.map((p) => ({
                        value: p,
                        // '-' 是后端给「没有节点池」的归类键（自建集群没有这个概念）。
                        // 原样显示成 "-" 没人看得懂，翻成文字
                        label: p === '-' ? t('nodes:poolNone') : p,
                        count: data.facets.pool?.[p] ?? 0,
                      })),
                    ]}
                  />
                ) : null}
                {staleCount > 0 && status !== 'stale' ? (
                  <span className="text-xs text-danger">
                    {t('nodes:staleNote', { count: staleCount })}
                  </span>
                ) : null}
                <span className="ml-auto text-xs text-muted-foreground">
                  {t('nodes:total', { count: data.total })}
                </span>
              </div>

              <DataTable
                data={data.items}
                columns={columns}
                rowKey={(n) => `${n.clusterId}/${n.name}`}
              />

              <Pagination

                page={page}

                size={size}

                total={data.total}

                onPage={(p) => patch({ page: p })}

                onSize={(n) => patch({ size: n })}

                rangeLabel={(f, t2, tt) => t('common:pagination.range', { from: f, to: t2, total: tt })}

                totalLabel={(n) => t('common:pagination.total', { count: n })}

                perPageLabel={t('common:pagination.perPage')}

                prevLabel={t('common:pagination.prev')}

                nextLabel={t('common:pagination.next')}

              />
            </>
          )
        }}
      </AsyncBoundary>
    </div>
  )
}

function sfCount(data: NodeListResult, dim: string): number {
  return data.facets[dim]?.all ?? 0
}

/** 节点实时用量。四态分开，没有一种显示成 0 —— 理由见 lib/liveUsage.ts */
function NodeLiveCell({
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
      <span className="text-[11px] text-muted-foreground" title={t('nodes:live.pickCluster')}>
        —
      </span>
    )
  }
  if (loading) return <span className="text-[11px] text-muted-foreground">…</span>
  if (m && m.ok === false) {
    return (
      <span className="text-[11px] text-warning" title={m.error}>
        {t('nodes:live.unavailable')}
      </span>
    )
  }
  const cpu = pickUsage(m, k, 'cpu_pct')
  const mem = pickUsage(m, k, 'mem_pct')
  if (cpu === undefined && mem === undefined) {
    return <span className="text-[11px] text-muted-foreground">—</span>
  }
  const tone = (v?: number) => (v == null ? '' : v >= 90 ? 'text-danger' : v >= 75 ? 'text-warning' : '')
  return (
    <span className="tabular text-[11px] whitespace-nowrap">
      <span className={tone(cpu)}>{cpu !== undefined ? `${cpu.toFixed(0)}%` : '—'}</span>
      <span className="text-muted-foreground"> / </span>
      <span className={tone(mem)}>{mem !== undefined ? `${mem.toFixed(0)}%` : '—'}</span>
    </span>
  )
}
