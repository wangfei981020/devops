import { toErrorInfo } from '@ops/api'
import { tError, useTranslation } from '@ops/i18n'
import {
  AsyncBoundary,
  Badge,
  DataTable,
  EmptyState,
  type LoadError,
  NotLicensed,
  SearchInput,
  Select,
  Pagination,
  TableSkeleton,
  fromQuery,
} from '@ops/ui'
import { useNavigate, useSearch } from '@tanstack/react-router'
import { ShieldAlert } from 'lucide-react'
import { useMemo } from 'react'
import { exposureColumns } from './columns.js'
import { type ExposureListResult, useExposures } from './queries.js'

export function ExposurePage() {
  const { t } = useTranslation()
  const { kind, page, size, q: keyword } = useSearch({ from: '/security/exposure' })
  const navigate = useNavigate({ from: '/security/exposure' })
  const patch = (next: Record<string, string | number>) =>
    void navigate({ search: (prev) => ({ ...prev, ...next }) })

  const query = useExposures({ page, size, q: keyword, kind })
  // 「归属」列在当前结果里有几个不同取值 —— 只有一个时它就是个常量列，
  // 排序箭头点了不会变（031 P2-48）
  const scopeValues = useMemo(
    () => [...new Set((query.data?.items ?? []).map((x) => x.scope).filter(Boolean))],
    [query.data],
  )
  const columns = useMemo(() => exposureColumns(t, scopeValues), [t, scopeValues])
  const toLoadError = (e: unknown): LoadError => {
    const n = toErrorInfo(e)
    return { cause: tError(t, n.messageKey, n.params), detail: n.detail, retryable: n.retryable, kind: n.kind === 'license' ? 'license' : undefined, feature: n.params?.feature as string | undefined }
  }
  const filtered = keyword !== '' || kind !== 'all'

  return (
    <div className="flex flex-col">
      <AsyncBoundary
        state={fromQuery<ExposureListResult>(query, (d) => d.total === 0, toLoadError)}
        errorTitle={t('exposure:error.title')}
        retryLabel={t('common:action.retry')}
        onRetry={() => void query.refetch()}
        // 暴露面是第一个受读门控的付费功能（OPSCMDB-072）。
        // 没买时要说"这在授权之外"，而不是渲染成加载失败 —— 重试解决不了。
        notLicensed={(feature) => (
          <NotLicensed
            title={t('common:license.notLicensedTitle')}
            reason={t('common:license.notLicensedReason')}
            feature={feature}
          />
        )}
        pending={
          <div className="p-5">
            <TableSkeleton columns={[12, 26, 26, 16, 14]} rows={6} />
          </div>
        }
        empty={
          <EmptyState
            icon={<ShieldAlert />}
            title={t('exposure:empty.title')}
            reason={filtered ? t('exposure:empty.filtered') : t('exposure:empty.noSource')}
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
                placeholder={t('exposure:filter.searchPlaceholder')}
                clearLabel={t('common:filter.clearSearch')}
                className="w-[240px]"
              />
              <Select<string>
                label={t('exposure:filter.kind')}
                value={kind}
                onChange={(v) => patch({ kind: v })}
                options={[
                  { value: 'all', label: t('common:filter.all'), count: data.facets.kind?.all },
                  ...Object.keys(data.facets.kind ?? {})
                    .filter((k) => k !== 'all' && k !== '')
                    .sort()
                    .map((v) => ({ value: v, label: t(`exposure:kind.${v}`, { defaultValue: v }), count: data.facets.kind?.[v] })),
                ]}
              />
              {/* 防护状态判不了这件事要常驻提示，不能只写在某一行的单元格里 */}
              <span className="text-xs text-warning">{t('exposure:protectionUnknown')}</span>
              <span className="ml-auto text-xs text-muted-foreground">
                {t('exposure:total', { count: data.total })}
              </span>
            </div>
            <DataTable data={data.items} columns={columns} rowKey={(x) => `${x.kind}/${x.name}/${x.endpoint}`} />
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
