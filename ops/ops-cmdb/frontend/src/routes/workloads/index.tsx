import { toErrorInfo } from '@ops/api'
import { tError, type Locale, useTranslation } from '@ops/i18n'
import {
  AsyncBoundary,
  Button,
  DataTable,
  EmptyState,
  type LoadError,
  SearchInput,
  Select,
  TableSkeleton,
  fromQuery,
} from '@ops/ui'
import { useNavigate, useSearch } from '@tanstack/react-router'
import { ChevronLeft, ChevronRight, Package } from 'lucide-react'
import { useMemo } from 'react'
import { workloadColumns } from './columns.js'
import { type WorkloadHealth, type WorkloadListResult, useWorkloads } from './queries.js'

const PAGE_SIZE = 50

export function WorkloadsPage() {
  const { t, i18n } = useTranslation()
  const locale = i18n.language as Locale
  const { page, cluster, namespace, health, q: keyword } = useSearch({ from: '/k8s/workloads' })
  const navigate = useNavigate({ from: '/k8s/workloads' })
  // 改筛选回到第一页：停在第 5 页而结果只有 2 页，用户看到空列表会以为没数据
  const patch = (
    next: Partial<{
      cluster: string
      namespace: string
      health: WorkloadHealth
      q: string
      page: number
    }>,
  ) =>
    void navigate({
      search: (prev) => ({ ...prev, ...next, ...(next.page === undefined ? { page: 1 } : {}) }),
    })

  const query = useWorkloads({ page, size: PAGE_SIZE, cluster, namespace, health, q: keyword })

  const columns = useMemo(() => workloadColumns(t, locale), [t, locale])
  const toLoadError = useMemo(
    () =>
      (e: unknown): LoadError => {
        const n = toErrorInfo(e)
        return { cause: tError(t, n.messageKey, n.params), detail: n.detail, retryable: n.retryable, kind: n.kind === 'license' ? 'license' : undefined, feature: n.params?.feature as string | undefined }
      },
    [t],
  )

  const state = fromQuery<WorkloadListResult>(query, (d) => d.total === 0, toLoadError)
  const filtered =
    keyword !== '' || cluster !== 'all' || namespace !== 'all' || health !== 'all'

  return (
    <div className="flex flex-col">
      <AsyncBoundary
        state={state}
        errorTitle={t('workloads:error.title')}
        retryLabel={t('common:action.retry')}
        onRetry={() => void query.refetch()}
        pending={
          <div className="p-5">
            <TableSkeleton columns={[28, 14, 8, 14, 20, 10]} rows={8} />
          </div>
        }
        empty={
          <EmptyState
            icon={<Package />}
            title={t('workloads:empty.title')}
            reason={filtered ? t('workloads:empty.filtered') : t('workloads:empty.noSource')}
            action={
              filtered
                ? {
                    label: t('common:filter.clearAll'),
                    onClick: () =>
                      patch({ q: '', cluster: 'all', namespace: 'all', health: 'all' }),
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
                  placeholder={t('workloads:filter.searchPlaceholder')}
                  clearLabel={t('common:filter.clearSearch')}
                  className="w-[240px]"
                />
                <Select<string>
                  label={t('workloads:filter.cluster')}
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
                  label={t('workloads:filter.namespace')}
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
                <Select<WorkloadHealth>
                  label={t('workloads:filter.health')}
                  value={health}
                  onChange={(v) => patch({ health: v })}
                  options={[
                    { value: 'all', label: t('common:filter.all'), count: hf?.all },
                    { value: 'down', label: t('workloads:health.down'), count: hf?.down },
                    { value: 'degraded', label: t('workloads:health.degraded'), count: hf?.degraded },
                    { value: 'ok', label: t('workloads:health.ok'), count: hf?.ok },
                    // 缩到 0 放最后：它是正常状态，排在故障档前面会让人以为要处理
                    {
                      value: 'scaled_zero',
                      label: t('workloads:health.scaledZero'),
                      count: hf?.scaled_zero,
                    },
                  ]}
                />
                {/*
                  ⚠️ 异常汇总要在工具栏里复述出来。
                  
                  同类页面（负载均衡「N 个确认无后端」、命名空间「N 个卡在 Terminating」、
                  节点「N 个状态不可信」）都有这一条，**唯独工作负载页没有**——
                  而 1287 条里有 49 个"全挂"，那恰恰是这一页最该先看到的数字（P1-22）。
                  
                  更要紧的是**跳转两端要呼应**：用户从全局态势点「49 工作负载全挂 → 查看」
                  跳进来，落地页却不提这个数字，他无法确认自己看的是不是那 49 个。
                  （P0-3 是同一类：那次是首页有数、列表 0 条；这次是首页有数、列表不提。）
                  
                  做成可点的筛选，而不只是一行字 —— 「有 49 个」要能变成「是哪 49 个」。
                */}
                {(hf?.down ?? 0) > 0 ? (
                  <button
                    type="button"
                    onClick={() => patch({ health: health === 'down' ? 'all' : 'down', page: 1 })}
                    className={`cursor-pointer rounded-[var(--radius)] border px-2.5 py-1 text-xs transition-colors duration-150 ${
                      health === 'down'
                        ? 'border-danger bg-danger/10 text-danger'
                        : 'border-border text-danger hover:bg-secondary'
                    }`}
                  >
                    {health === 'down'
                      ? t('workloads:downNoteOn', { count: hf?.down })
                      : t('workloads:downNote', { count: hf?.down })}
                  </button>
                ) : null}
                <span className="ml-auto text-xs text-muted-foreground">
                  {t('workloads:total', { count: data.total })}
                </span>
              </div>

              <DataTable
                data={data.items}
                columns={columns}
                rowKey={(w) => `${w.clusterId}/${w.namespace}/${w.kind}/${w.name}`}
              />

              {/* 同 Pod：后端在 SQL 里分页，不是取全量再切 */}
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
    </div>
  )
}
