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
import { Share2 } from 'lucide-react'
import { useMemo } from 'react'
import { lbColumns } from './columns.js'
import { type LBHealth, type LBListResult, useLBs } from './queries.js'

const PAGE_SIZE = 50

export function LbsPage() {
  const { t, i18n } = useTranslation()
  const locale = i18n.language as Locale
  const { scheme, health, page, size, q: keyword } = useSearch({ from: '/resources/lbs' })
  const navigate = useNavigate({ from: '/resources/lbs' })
  const patch = (next: Partial<{ scheme: string; health: LBHealth; q: string; page: number; size: number }>) =>
    void navigate({ search: (prev) => ({ ...prev, ...next }) })

  const query = useLBs({ page, size, scheme, health, q: keyword })
  const labels: Record<NoValueKind, string> = {
    na: '—',
    notIngested: t('common:state.notIngested'),
    stopped: t('common:state.unknown'),
    unknown: t('common:state.unknown'),
  }

  const columns = useMemo(() => lbColumns(t, locale, labels), [t, locale, labels])
  const toLoadError = useMemo(
    () =>
      (e: unknown): LoadError => {
        const n = toErrorInfo(e)
        return { cause: tError(t, n.messageKey, n.params), detail: n.detail, retryable: n.retryable, kind: n.kind === 'license' ? 'license' : undefined, feature: n.params?.feature as string | undefined }
      },
    [t],
  )

  const state = fromQuery<LBListResult>(query, (d) => d.total === 0, toLoadError)
  const filtered = keyword !== '' || scheme !== 'all' || health !== 'all'

  return (
    <div className="flex flex-col">
      <AsyncBoundary
        state={state}
        errorTitle={t('lbs:error.title')}
        retryLabel={t('common:action.retry')}
        onRetry={() => void query.refetch()}
        pending={
          <div className="p-5">
            <TableSkeleton columns={[26, 12, 12, 14, 20]} rows={6} />
          </div>
        }
        empty={
          <EmptyState
            icon={<Share2 />}
            title={t('lbs:empty.title')}
            reason={filtered ? t('lbs:empty.filtered') : t('lbs:empty.noSource')}
            action={
              filtered
                ? { label: t('common:filter.clearAll'), onClick: () => patch({ q: '', scheme: 'all', health: 'all' }) }
                : null
            }
          />
        }
      >
        {(data) => {
          const hf = data.facets.health
          // 「确认无后端」和「没采过」分别数：前者是真问题，后者是我们的采集缺口
          const empty = hf?.empty ?? 0
          const unknown = hf?.unknown ?? 0

          return (
            <>
              <div className="flex flex-wrap items-center gap-2 border-b border-border px-4 py-3">
                <SearchInput
                  value={keyword}
                  onChange={(v) => patch({ q: v })}
                  placeholder={t('lbs:filter.searchPlaceholder')}
                  clearLabel={t('common:filter.clearSearch')}
                  className="w-[240px]"
                />
                <Select<string>
                  label={t('lbs:filter.scheme')}
                  value={scheme}
                  onChange={(v) => patch({ scheme: v })}
                  options={[
                    { value: 'all', label: t('common:filter.all'), count: data.facets.scheme?.all },
                    ...Object.keys(data.facets.scheme ?? {})
                      .filter((k) => k !== 'all' && k !== '')
                      .sort()
                      .map((v) => ({ value: v, label: v, count: data.facets.scheme?.[v] })),
                  ]}
                />
                <Select<LBHealth>
                  label={t('lbs:filter.health')}
                  value={health}
                  onChange={(v) => patch({ health: v })}
                  options={[
                    { value: 'all', label: t('common:filter.all'), count: hf?.all },
                    { value: 'empty', label: t('lbs:health.empty'), count: hf?.empty },
                    { value: 'unknown', label: t('lbs:health.unknown'), count: hf?.unknown },
                    { value: 'stale', label: t('lbs:health.stale'), count: hf?.stale },
                    { value: 'ok', label: t('lbs:health.ok'), count: hf?.ok },
                  ]}
                />
                {empty > 0 ? (
                  <span className="text-xs text-danger">{t('lbs:emptyNote', { count: empty })}</span>
                ) : null}
                {unknown > 0 ? (
                  <span className="text-xs text-warning">
                    {t('lbs:unknownNote', { count: unknown })}
                  </span>
                ) : null}
                <span className="ml-auto text-xs text-muted-foreground">
                  {t('lbs:total', { count: data.total })}
                </span>
              </div>

              <DataTable data={data.items} columns={columns} rowKey={(l) => String(l.id)} />
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
