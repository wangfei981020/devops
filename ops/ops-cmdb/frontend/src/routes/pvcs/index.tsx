import { toErrorInfo } from '@ops/api'
import { tError, type Locale, useTranslation } from '@ops/i18n'
import {
  AsyncBoundary,
  DataTable,
  EmptyState,
  type LoadError,
  SearchInput,
  Select,
  Pagination,
  TableSkeleton,
  fromQuery,
} from '@ops/ui'
import { useNavigate, useSearch } from '@tanstack/react-router'
import { type UsageMap, pickUsage, useLiveUsage } from '../../lib/liveUsage.js'
import { HardDrive } from 'lucide-react'
import { useMemo } from 'react'
import { pvcColumns } from './columns.js'
import { type PVCHealth, type PVCListResult, usePVCs } from './queries.js'

const PAGE_SIZE = 50

export function PvcsPage() {
  const { t, i18n } = useTranslation()
  const locale = i18n.language as Locale
  const { cluster, health, page, size, q: keyword } = useSearch({ from: '/k8s/pvcs' })
  const navigate = useNavigate({ from: '/k8s/pvcs' })
  const patch = (next: Partial<{ cluster: string; health: PVCHealth; q: string; page: number; size: number }>) =>
    void navigate({ search: (prev) => ({ ...prev, ...next }) })

  const query = usePVCs({ page, size, cluster, health, q: keyword })

  // PVC 实时使用率。⚠️ 这一列和「容量」不同：容量是申请了多少，这是真用了多少。
  // 孤儿盘和"申请 500Gi 只用 3Gi"这两类浪费，只有这一列能看出来
  // ⚠️ 集群 id 从**数据行**里取，不要 Number(cluster) ——
  // URL 里的 cluster 是集群**名字**（如「本地 docker-desktop」），
  // Number() 得到 NaN，而 `NaN != null` 为真，于是 noCluster 判定失效、
  // enabled 又是 false，这一列会**永远停在加载态**。
  // 加载态是最坏的一种错：它既不是数据也不是错误，人只会一直等下去。
  const cid =
    cluster !== 'all' ? (query.data?.items?.[0]?.clusterId ?? null) : null
  const live = useLiveUsage('pvc', cid)

  const columns = useMemo(
    () => [
      ...pvcColumns(t, locale),
      {
        id: 'live',
        header: t('pvcs:live.header'),
        cell: ({ row }: { row: { original: { namespace: string; name: string } } }) => (
          <PvcLiveCell
            m={live.data}
            loading={live.isPending && cid != null}
            k={`${row.original.namespace}/${row.original.name}`}
            noCluster={cid == null}
            t={t}
          />
        ),
      },
    ],
    [t, locale, live.data, live.isPending, cid],
  )
  const toLoadError = useMemo(
    () =>
      (e: unknown): LoadError => {
        const n = toErrorInfo(e)
        return { cause: tError(t, n.messageKey, n.params), detail: n.detail, retryable: n.retryable, kind: n.kind === 'license' ? 'license' : undefined, feature: n.params?.feature as string | undefined }
      },
    [t],
  )

  const state = fromQuery<PVCListResult>(query, (d) => d.total === 0, toLoadError)
  const filtered = keyword !== '' || cluster !== 'all' || health !== 'all'

  return (
    <div className="flex flex-col">
      <AsyncBoundary
        state={state}
        errorTitle={t('pvcs:error.title')}
        retryLabel={t('common:action.retry')}
        onRetry={() => void query.refetch()}
        pending={
          <div className="p-5">
            <TableSkeleton columns={[26, 10, 10, 16, 12]} rows={6} />
          </div>
        }
        empty={
          <EmptyState
            icon={<HardDrive />}
            title={t('pvcs:empty.title')}
            reason={filtered ? t('pvcs:empty.filtered') : t('pvcs:empty.noSource')}
            action={
              filtered
                ? { label: t('common:filter.clearAll'), onClick: () => patch({ q: '', cluster: 'all', health: 'all' }) }
                : null
            }
          />
        }
      >
        {(data) => {
          const hf = data.facets.health
          const broken = (hf?.lost ?? 0) + (hf?.pending ?? 0)

          return (
            <>
              <div className="flex flex-wrap items-center gap-2 border-b border-border px-4 py-3">
                <SearchInput
                  value={keyword}
                  onChange={(v) => patch({ q: v })}
                  placeholder={t('pvcs:filter.searchPlaceholder')}
                  clearLabel={t('common:filter.clearSearch')}
                  className="w-[240px]"
                />
                <Select<string>
                  label={t('pvcs:filter.cluster')}
                  value={cluster}
                  onChange={(v) => patch({ cluster: v })}
                  options={[
                    { value: 'all', label: t('common:filter.all'), count: data.facets.cluster?.all },
                    ...Object.keys(data.facets.cluster ?? {})
                      .filter((k) => k !== 'all' && k !== '')
                      .sort()
                      .map((v) => ({ value: v, label: v, count: data.facets.cluster?.[v] })),
                  ]}
                />
                <Select<PVCHealth>
                  label={t('pvcs:filter.health')}
                  value={health}
                  onChange={(v) => patch({ health: v })}
                  options={[
                    { value: 'all', label: t('common:filter.all'), count: hf?.all },
                    { value: 'lost', label: t('pvcs:health.lost'), count: hf?.lost },
                    { value: 'pending', label: t('pvcs:health.pending'), count: hf?.pending },
                    { value: 'orphan', label: t('pvcs:health.orphan'), count: hf?.orphan },
                    { value: 'ok', label: t('pvcs:health.ok'), count: hf?.ok },
                  ]}
                />
                {broken > 0 ? (
                  <span className="text-xs text-danger">
                    {t('pvcs:brokenNote', { count: broken })}
                  </span>
                ) : null}
                {/*
                  ⚠️ 「有问题」只统计了 Pending/Lost（实测 1 个），
                  而「无人使用但仍在计费」这一类**数量大得多、且直接对应真金白银**
                  （实测首屏 17 行里 16 行是无使用者，DEV 21 个合计 $866/月），
                  工具栏原来完全没提（P1-24）。
                  做成可点筛选：「有 N 个」要能变成「是哪 N 个」。
                */}
                {(hf?.orphan ?? 0) > 0 ? (
                  <button
                    type="button"
                    onClick={() => patch({ health: health === 'orphan' ? 'all' : 'orphan', page: 1 })}
                    className={`cursor-pointer rounded-[var(--radius)] border px-2.5 py-1 text-xs transition-colors duration-150 ${
                      health === 'orphan'
                        ? 'border-warning bg-warning/10 text-warning'
                        : 'border-border text-warning hover:bg-secondary'
                    }`}
                  >
                    {health === 'orphan'
                      ? t('pvcs:orphanNoteOn', { count: hf?.orphan })
                      : t('pvcs:orphanNote', { count: hf?.orphan })}
                  </button>
                ) : null}
                <span className="ml-auto text-xs text-muted-foreground">
                  {t('pvcs:total', { count: data.total })}
                </span>
              </div>

              <DataTable data={data.items} columns={columns} rowKey={(p) => `${p.clusterId}/${p.namespace}/${p.name}`} />
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

/**
 * PVC 实时使用率。
 *
 * ⚠️ 除了通用的四态，这里还要显示后端给的 `empty_hint` ——
 * 「集群标签对但一条 PVC 用量都没查到」很可能是没采 kubelet_volume_stats_*，
 * 那是采集缺口，不是「所有盘都是空的」。
 */
function PvcLiveCell({
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
      <span className="text-[11px] text-muted-foreground" title={t('pvcs:live.pickCluster')}>
        —
      </span>
    )
  }
  if (loading) return <span className="text-[11px] text-muted-foreground">…</span>
  if (m && (m.ok === false || m.empty_hint)) {
    return (
      <span className="text-[11px] text-warning" title={m.error ?? m.empty_hint}>
        {t('pvcs:live.unavailable')}
      </span>
    )
  }
  const pct = pickUsage(m, k, 'pct')
  const used = pickUsage(m, k, 'used_gi')
  if (pct === undefined) return <span className="text-[11px] text-muted-foreground">—</span>
  const tone = pct >= 90 ? 'text-danger' : pct >= 75 ? 'text-warning' : 'text-foreground'
  // 🔴 `node-fs` = 这个数字是**宿主机整体水位**，不是本卷用量。
  //	后端一直在返回这个说明，界面此前完全没显示 —— 于是一个几乎空着的卷
  //	会因为宿主机盘满而显示成 92%，看着就该扩容（OPSCMDB-084）。
  const acc = m?.accuracy?.[k]
  const caveat =
    acc?.level === 'node-fs'
      ? acc.note_key
        ? t(acc.note_key, acc.note_params)
        : (acc.note ?? '')
      : ''
  return (
    <span
      className={`tabular text-[11px] whitespace-nowrap ${caveat !== '' ? 'text-muted-foreground' : tone}`}
      title={caveat || undefined}
    >
      {pct.toFixed(0)}%{used !== undefined ? ` · ${used.toFixed(1)}Gi` : ''}
      {caveat !== '' ? (
        // 数字本身要降调（它不代表本卷），并给一个看得见的标记 ——
        // 只放 title 的话，不悬停就完全看不出这个数字的含义变了
        <span className="ml-1 text-warning" aria-label={caveat}>
          ⚠
        </span>
      ) : null}
    </span>
  )
}
