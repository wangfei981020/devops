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
  Pagination,
  TableSkeleton,
  fromQuery,
} from '@ops/ui'
import { useNavigate, useSearch } from '@tanstack/react-router'
import { Network } from 'lucide-react'
import { useMemo, useState } from 'react'
import { IngressDialog } from './IngressDialog.js'
import { serviceColumns } from './columns.js'
import { type SvcExposure, type ServiceListResult, useServices } from './queries.js'

const PAGE_SIZE = 50

export function ServicesPage() {
  // 入口与域名：Service 上没有对外域名，它在 VirtualService/HTTPRoute 上
  const [ingressFor, setIngressFor] = useState(0)
  const { t, i18n } = useTranslation()
  const locale = i18n.language as Locale
  const { cluster, namespace, exposure, page, size, q: keyword } = useSearch({ from: '/k8s/services' })
  const navigate = useNavigate({ from: '/k8s/services' })
  const patch = (next: Partial<{ cluster: string; namespace: string; exposure: SvcExposure; q: string; page: number; size: number }>) =>
    void navigate({ search: (prev) => ({ ...prev, ...next }) })

  const query = useServices({ page, size, cluster, namespace, exposure, q: keyword })

  const columns = useMemo(() => serviceColumns(t, locale), [t, locale])
  const toLoadError = useMemo(
    () =>
      (e: unknown): LoadError => {
        const n = toErrorInfo(e)
        return { cause: tError(t, n.messageKey, n.params), detail: n.detail, retryable: n.retryable, kind: n.kind === 'license' ? 'license' : undefined, feature: n.params?.feature as string | undefined }
      },
    [t],
  )

  const state = fromQuery<ServiceListResult>(query, (d) => d.total === 0, toLoadError)
  const filtered = keyword !== '' || cluster !== 'all' || namespace !== 'all' || exposure !== 'all'

  return (
    <div className="flex flex-col">
      {/* ⚠️ 必须在 AsyncBoundary **外面**。放 children 里的话，
          筛选筛空时整条工具条连同这个按钮一起消失（OPSCMDB-001 那个坑，
          我自己又踩了一次）。而 Service 列表为空恰恰是最需要看入口的时候 */}
      <div className="flex flex-wrap items-center justify-end gap-2 border-b border-border px-4 py-2">
        <Button
          size="sm"
          disabled={cluster === 'all'}
          title={cluster === 'all' ? t('services:ingress.pickCluster') : undefined}
          onClick={() => setIngressFor(Number(cluster))}
        >
          {t('services:ingress.entry')}
        </Button>
      </div>

      <AsyncBoundary
        state={state}
        errorTitle={t('services:error.title')}
        retryLabel={t('common:action.retry')}
        onRetry={() => void query.refetch()}
        pending={
          <div className="p-5">
            <TableSkeleton columns={[24, 12, 24, 12, 16]} rows={6} />
          </div>
        }
        empty={
          <EmptyState
            icon={<Network />}
            title={t('services:empty.title')}
            reason={filtered ? t('services:empty.filtered') : t('services:empty.noSource')}
            action={
              filtered
                ? { label: t('common:filter.clearAll'), onClick: () => patch({ q: '', cluster: 'all', namespace: 'all', exposure: 'all' }) }
                : null
            }
          />
        }
      >
        {(data) => {
          const ef = data.facets.exposure
          const pending = ef?.pending ?? 0

          return (
            <>
              <div className="flex flex-wrap items-center gap-2 border-b border-border px-4 py-3">
                <SearchInput
                  value={keyword}
                  onChange={(v) => patch({ q: v })}
                  placeholder={t('services:filter.searchPlaceholder')}
                  clearLabel={t('common:filter.clearSearch')}
                  className="w-[240px]"
                />
                <Select<string>
                  label={t('services:filter.cluster')}
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
                  label={t('services:filter.namespace')}
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
                <Select<SvcExposure>
                  label={t('services:filter.exposure')}
                  value={exposure}
                  onChange={(v) => patch({ exposure: v })}
                  options={[
                    { value: 'all', label: t('common:filter.all'), count: ef?.all },
                    { value: 'exposed', label: t('services:exposure.exposed'), count: ef?.exposed },
                    { value: 'pending', label: t('services:exposure.pending'), count: ef?.pending },
                    { value: 'internal', label: t('services:exposure.internal'), count: ef?.internal },
                  ]}
                />
                {pending > 0 ? (
                  <span className="text-xs text-warning">
                    {t('services:pendingNote', { count: pending })}
                  </span>
                ) : null}
                <span className="ml-auto text-xs text-muted-foreground">
                  {t('services:total', { count: data.total })}
                </span>
              </div>

              <DataTable data={data.items} columns={columns} rowKey={(s) => `${s.clusterId}/${s.namespace}/${s.name}`} />
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
      {ingressFor > 0 ? (
        <IngressDialog clusterID={ingressFor} onClose={() => setIngressFor(0)} t={t} />
      ) : null}
    </div>
  )
}
