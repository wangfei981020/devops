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
import { Settings } from 'lucide-react'
import { useMemo } from 'react'
import { dataSourceColumns } from './columns.js'
import { type DataSourceListResult, useDataSources } from './queries.js'

export function DataSourcesPage() {
  const { t } = useTranslation()
  const { kind, page, size, q: keyword } = useSearch({ from: '/admin/datasources' })
  const navigate = useNavigate({ from: '/admin/datasources' })
  const patch = (next: Record<string, string | number>) =>
    void navigate({ search: (prev) => ({ ...prev, ...next }) })

  const query = useDataSources({ page, size, q: keyword, kind })
  const columns = useMemo(() => dataSourceColumns(t), [t])
  const toLoadError = (e: unknown): LoadError => {
    const n = toErrorInfo(e)
    return { cause: tError(t, n.messageKey, n.params), detail: n.detail, retryable: n.retryable, kind: n.kind === 'license' ? 'license' : undefined, feature: n.params?.feature as string | undefined }
  }
  const filtered = keyword !== '' || kind !== 'all'

  return (
    <div className="flex flex-col">
      <AsyncBoundary
        state={fromQuery<DataSourceListResult>(query, (d) => d.total === 0, toLoadError)}
        errorTitle={t('datasources:error.title')}
        retryLabel={t('common:action.retry')}
        onRetry={() => void query.refetch()}
        pending={
          <div className="p-5">
            <TableSkeleton columns={[12, 26, 14, 18, 18]} rows={6} />
          </div>
        }
        empty={
          <EmptyState
            icon={<Settings />}
            title={t('datasources:empty.title')}
            reason={filtered ? t('datasources:empty.filtered') : t('datasources:empty.noSource')}
            action={
              filtered
                ? { label: t('common:filter.clearAll'), onClick: () => patch({ q: '', kind: 'all' }) }
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
                placeholder={t('datasources:filter.searchPlaceholder')}
                clearLabel={t('common:filter.clearSearch')}
                className="w-[240px]"
              />
              <Select<string>
                label={t('datasources:filter.kind')}
                value={kind}
                onChange={(v) => patch({ kind: v })}
                options={[
                  { value: 'all', label: t('common:filter.all'), count: data.facets.kind?.all },
                  ...Object.keys(data.facets.kind ?? {})
                    .filter((k) => k !== 'all' && k !== '')
                    .sort()
                    .map((v) => ({ value: v, label: t(`datasources:kind.${v}`, { defaultValue: v }), count: data.facets.kind?.[v] })),
                ]}
              />
              {data.items.filter((d) => d.health === 'no_credential').length > 0 ? (
                <span className="text-xs text-danger">
                  {t('datasources:noCredNote', {
                    count: data.items.filter((d) => d.health === 'no_credential').length,
                  })}
                </span>
              ) : null}
              <span className="ml-auto text-xs text-muted-foreground">
                {t('datasources:total', { count: data.total })}
              </span>
            </div>
            <DataTable data={data.items} columns={columns} rowKey={(x) => `${x.kind}/${x.name}`} />
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
