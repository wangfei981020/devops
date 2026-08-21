import { toErrorInfo } from '@ops/api'
import { tError, useTranslation } from '@ops/i18n'
import {
  AsyncBoundary,
  Badge,
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
import { Radar } from 'lucide-react'
import { useMemo } from 'react'
import { eventColumns } from './columns.js'
import { type K8sEventListResult, useK8sEvents } from './queries.js'

export function EventsPage() {
  const { t } = useTranslation()
  const { cluster, namespace, reason, page, size, q: keyword } = useSearch({ from: '/k8s/events' })
  const navigate = useNavigate({ from: '/k8s/events' })
  const patch = (next: Record<string, string | number>) =>
    void navigate({ search: (prev) => ({ ...prev, ...next }) })

  const query = useK8sEvents({ page, size, q: keyword, cluster, namespace, reason })
  const columns = useMemo(() => eventColumns(t), [t])
  const toLoadError = (e: unknown): LoadError => {
    const n = toErrorInfo(e)
    return { cause: tError(t, n.messageKey, n.params), detail: n.detail, retryable: n.retryable, kind: n.kind === 'license' ? 'license' : undefined, feature: n.params?.feature as string | undefined }
  }
  const filtered = keyword !== '' || cluster !== 'all' || namespace !== 'all' || reason !== 'all'

  return (
    <div className="flex flex-col">
      <AsyncBoundary
        state={fromQuery<K8sEventListResult>(query, (d) => d.total === 0, toLoadError)}
        errorTitle={t('events:error.title')}
        retryLabel={t('common:action.retry')}
        onRetry={() => void query.refetch()}
        pending={
          <div className="p-5">
            <TableSkeleton columns={[30, 14, 16, 16, 12]} rows={6} />
          </div>
        }
        empty={
          <EmptyState
            icon={<Radar />}
            title={t('events:empty.title')}
            reason={filtered ? t('events:empty.filtered') : t('events:empty.noSource')}
            action={
              filtered
                ? { label: t('common:filter.clearAll'), onClick: () => patch({ q: '', cluster: 'all', namespace: 'all', reason: 'all' }) }
                : null
            }
          />
        }
      >
        {(data) => (
          <>
            <div className="flex flex-wrap items-center gap-2 border-b border-border px-4 py-3">
              <SearchInput
                value={keyword}
                onChange={(v) => patch({ q: v })}
                placeholder={t('events:filter.searchPlaceholder')}
                clearLabel={t('common:filter.clearSearch')}
                className="w-[240px]"
              />
              <Select<string>
                label={t('events:filter.cluster')}
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
              <Select<string>
                label={t('events:filter.namespace')}
                value={namespace}
                onChange={(v) => patch({ namespace: v })}
                options={[
                  { value: 'all', label: t('common:filter.all'), count: data.facets.namespace?.all },
                  ...Object.keys(data.facets.namespace ?? {})
                    .filter((k) => k !== 'all' && k !== '')
                    .sort()
                    .map((v) => ({ value: v, label: v, count: data.facets.namespace?.[v] })),
                ]}
              />
              <Select<string>
                label={t('events:filter.reason')}
                value={reason}
                onChange={(v) => patch({ reason: v })}
                options={[
                  { value: 'all', label: t('common:filter.all'), count: data.facets.reason?.all },
                  ...Object.keys(data.facets.reason ?? {})
                    .filter((k) => k !== 'all' && k !== '')
                    .sort()
                    .map((v) => ({ value: v, label: v, count: data.facets.reason?.[v] })),
                ]}
              />
              {/* 只有 Warning 这件事要常驻说明 —— 不说的话，
                  "最近没有事件"会被读成"最近什么都没发生" */}
              <span className="text-xs text-muted-foreground">{t('events:warningOnly')}</span>
              <span className="ml-auto text-xs text-muted-foreground">
                {t('events:total', { count: data.total })}
              </span>
            </div>
            <DataTable data={data.items} columns={columns} rowKey={(x) => `${x.cluster_id}/${x.namespace}/${x.obj_name}/${x.reason}/${x.last_at ?? ''}`} />
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
        )}
      </AsyncBoundary>
    </div>
  )
}
