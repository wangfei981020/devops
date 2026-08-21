import { toErrorInfo } from '@ops/api'
import { tError, type Locale, useTranslation } from '@ops/i18n'
import {
  AsyncBoundary,
  Badge,
  DataTable,
  EmptyState,
  type LoadError,
  SearchInput,
  Select,
  Pagination,
  Skeleton,
  TableSkeleton,
  fromQuery,
} from '@ops/ui'
import { useNavigate, useSearch } from '@tanstack/react-router'
import { Network } from 'lucide-react'
import { useMemo } from 'react'
import { networkModeHint, networkModeLabel, subnetColumns } from './columns.js'
import { type SubnetListResult, useCloudNetworks, useSubnets } from './queries.js'

const PAGE_SIZE = 50

export function SubnetsPage() {
  const { t, i18n } = useTranslation()
  const locale = i18n.language as Locale
  const { region, project, page, size, q: keyword } = useSearch({ from: '/resources/subnets' })
  const navigate = useNavigate({ from: '/resources/subnets' })
  const patch = (next: Partial<{ region: string; project: string; q: string; page: number; size: number }>) =>
    void navigate({ search: (prev) => ({ ...prev, ...next }) })

  const query = useSubnets({ page, size, region, project, q: keyword })

  const columns = useMemo(() => subnetColumns(t, locale), [t, locale])
  const toLoadError = useMemo(
    () =>
      (e: unknown): LoadError => {
        const n = toErrorInfo(e)
        return { cause: tError(t, n.messageKey, n.params), detail: n.detail, retryable: n.retryable, kind: n.kind === 'license' ? 'license' : undefined, feature: n.params?.feature as string | undefined }
      },
    [t],
  )

  const state = fromQuery<SubnetListResult>(query, (d) => d.total === 0, toLoadError)
  const filtered = keyword !== '' || region !== 'all' || project !== 'all'

  return (
    <div className="flex flex-col">
      {/* VPC 一层放在子网上面：排查连通性时第一步是确认两边在不在同一个 VPC，
          而这一页原来只有子网，那个问题在界面上答不出来 */}
      <VpcStrip t={t} />
      <AsyncBoundary
        state={state}
        errorTitle={t('subnets:error.title')}
        retryLabel={t('common:action.retry')}
        onRetry={() => void query.refetch()}
        pending={
          <div className="p-5">
            <TableSkeleton columns={[24, 16, 20, 14, 14]} rows={6} />
          </div>
        }
        empty={
          <EmptyState
            icon={<Network />}
            title={t('subnets:empty.title')}
            reason={filtered ? t('subnets:empty.filtered') : t('subnets:empty.noSource')}
            action={
              filtered
                ? { label: t('common:filter.clearAll'), onClick: () => patch({ q: '', region: 'all', project: 'all' }) }
                : null
            }
          />
        }
      >
        {(data) => {
          const stale = data.items.filter((s) => s.stale).length

          return (
            <>
              <div className="flex flex-wrap items-center gap-2 border-b border-border px-4 py-3">
                <SearchInput
                  value={keyword}
                  onChange={(v) => patch({ q: v })}
                  placeholder={t('subnets:filter.searchPlaceholder')}
                  clearLabel={t('common:filter.clearSearch')}
                  className="w-[240px]"
                />
                <Select<string>
                  label={t('subnets:filter.region')}
                  value={region}
                  onChange={(v) => patch({ region: v })}
                  options={[
                    { value: 'all', label: t('common:filter.all'), count: data.facets.region?.all },
                    ...Object.keys(data.facets.region ?? {})
                      .filter((k) => k !== 'all' && k !== '')
                      .sort()
                      .map((v) => ({ value: v, label: v, count: data.facets.region?.[v] })),
                  ]}
                />
                <Select<string>
                  label={t('subnets:filter.project')}
                  value={project}
                  onChange={(v) => patch({ project: v })}
                  options={[
                    { value: 'all', label: t('common:filter.all'), count: data.facets.project?.all },
                    ...Object.keys(data.facets.project ?? {})
                      .filter((k) => k !== 'all' && k !== '')
                      .sort()
                      .map((v) => ({ value: v, label: v, count: data.facets.project?.[v] })),
                  ]}
                />
                {stale > 0 ? (
                  <span className="text-xs text-warning">
                    {t('subnets:staleNote', { count: stale })}
                  </span>
                ) : null}
                <span className="ml-auto text-xs text-muted-foreground">
                  {t('subnets:total', { count: data.total })}
                </span>
              </div>

              <DataTable data={data.items} columns={columns} rowKey={(s) => String(s.id)} />
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
 * VPC 概览条。
 *
 * ⚠️ 失败和空要分开：查询失败显示原因（这一页的子网列表可能还是好的），
 * 真的没有 VPC 才说没有 —— 而"没有 VPC"通常意味着云账号还没采到，不是真没有。
 */
function VpcStrip({ t }: { t: (k: string, p?: Record<string, unknown>) => string }) {
  const q = useCloudNetworks()
  if (q.isPending) return <div className="px-4 py-2"><Skeleton className="h-4 w-[30%]" /></div>
  if (q.isError) {
    return (
      <div className="px-4 py-2">
        <span className="text-xs text-danger" title={toErrorInfo(q.error).detail || undefined}>
          {tError(t, toErrorInfo(q.error).messageKey, toErrorInfo(q.error).params)}
        </span>
      </div>
    )
  }
  const rows = q.data ?? []
  if (rows.length === 0) {
    return (
      <div className="border-b border-border px-4 py-2">
        <span className="text-xs text-muted-foreground">{t('subnets:vpc.empty')}</span>
      </div>
    )
  }
  return (
    <div className="flex flex-wrap items-center gap-2 border-b border-border px-4 py-2">
      <span className="text-xs font-medium text-foreground">
        {t('subnets:vpc.title', { count: rows.length })}
      </span>
      {rows.map((n) => (
        <span
          key={`${n.project_id}/${n.name}`}
          className="flex items-center gap-1.5 rounded-[var(--radius)] border border-border px-2 py-0.5 text-[11px]"
          title={`${n.provider} · ${n.project}`}
        >
          <span className="font-mono text-foreground">{n.name}</span>
          {/* 同 columns.tsx：光一个 `custom` 看不懂（031 P2-7） */}
          <span title={networkModeHint(n.mode, t)}>
            <Badge tone="mute">{networkModeLabel(n.mode, t)}</Badge>
          </span>
          <span className="tabular text-muted-foreground">
            {t('subnets:vpc.counts', { subnets: n.subnet_count, fws: n.firewall_count })}
          </span>
        </span>
      ))}
    </div>
  )
}
