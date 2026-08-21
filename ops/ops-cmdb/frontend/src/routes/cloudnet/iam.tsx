import { toErrorInfo } from '@ops/api'
import { tError, useTranslation } from '@ops/i18n'
import {
  AsyncBoundary,
  Badge,
  type LoadError,
  NotIngested,
  Pagination,
  Select,
  TableSkeleton,
  fromQuery,
} from '@ops/ui'
import { getRouteApi, useNavigate } from '@tanstack/react-router'
import { useIam } from './queries.js'

const route = getRouteApi('/security/cloud-iam')

/**
 * 云权限审计（GCP IAM）。
 *
 * ⚠️ 这一页最危险的失败方式是**把"没采到"渲染成"没有风险"**。
 *
 * 任何 GCP 项目都至少有一条权限绑定，所以空列表只可能是采集没成功。
 * 渲染成普通空态的话，一个采集挂掉的权限审计页看起来就是"你很安全" ——
 * 而这是最贵的一种错：它让人**放心**，于是不再去查。
 * 后端已经把这句话放进 empty_hint 了，这里必须原样显示。
 */
export function CloudIamPage() {
  const { t } = useTranslation()
  const search = route.useSearch()
  const navigate = useNavigate()
  const query = useIam(search.only)

  const setSearch = (patch: Record<string, string | number>) =>
    void navigate({ to: '/security/cloud-iam', search: { ...search, ...patch } })

  const toLoadError = (e: unknown): LoadError => {
    const n = toErrorInfo(e)
    return { cause: tError(t, n.messageKey, n.params), detail: n.detail, retryable: n.retryable, kind: n.kind === 'license' ? 'license' : undefined, feature: n.params?.feature as string | undefined }
  }

  return (
    <div className="flex h-full flex-col">
      <div className="flex flex-wrap items-center gap-2.5 border-b border-border px-4 py-2.5">
        <Select<string>
          label={t('cloudnet:iam.filter.scope')}
          value={search.only}
          onChange={(v) => setSearch({ only: v, page: 1 })}
          options={[
            { value: 'all', label: t('cloudnet:iam.allBindings') },
            { value: 'issues', label: t('cloudnet:iam.issuesOnly') },
          ]}
          /*
            ⚠️ 只在「一条数据都没有」时禁用（only='all' 且空）。
            
            没有数据可筛还能点开，会让人以为是自己筛错了才没结果
            （OPSCMDB-031 P2-49）。
            
            ⚠️ 但 only='issues' 筛出 0 条时**绝不能禁**——
            那时候切回「全部授权」正是唯一的出路。
            两种空必须分开判。
          */
          disabledReason={
            search.only === 'all' && query.data && query.data.items.length === 0
              ? t('cloudnet:iam.filter.noDataToFilter')
              : undefined
          }
        />
      </div>

      <AsyncBoundary
        state={fromQuery(query, (d) => d.items.length === 0, toLoadError)}
        errorTitle={t('cloudnet:iam.error')}
        retryLabel={t('common:action.retry')}
        onRetry={() => void query.refetch()}
        pending={<TableSkeleton columns={[18, 34, 30, 12]} rows={8} />}
        empty={
          <div className="p-6">
            {/* 后端的 empty_hint 优先——它才知道是没配凭据还是缺 securityReviewer。
                只有筛"仅风险项"时空才是好消息，那一档单独说 */}
            {search.only === 'issues' ? (
              <NotIngested
                title={t('cloudnet:iam.empty.noIssuesTitle')}
                reason={t('cloudnet:iam.empty.noIssuesReason')}
              />
            ) : (
              <NotIngested
                title={t('cloudnet:iam.empty.title')}
                reason={query.data?.empty_hint ?? t('cloudnet:iam.empty.reason')}
              />
            )}
          </div>
        }
      >
        {(d) => {
          // 按风险等级排，高的在前。⚠️ 用后端给的 severity，
          //	原来读的 `risky` 后端根本不返回，这个排序一直是空转的
          const sevRank = (s?: string) =>
            s === 'critical' ? 3 : s === 'high' ? 2 : s === 'medium' ? 1 : 0
          const rows = [...d.items].sort((a, b) => sevRank(b.severity) - sevRank(a.severity))
          const from = (search.page - 1) * search.size
          const pageRows = rows.slice(from, from + search.size)
          return (
            <>
              <div className="min-h-0 flex-1 overflow-auto">
                <table className="w-full text-[13px]">
                  <thead className="sticky top-0 z-10 bg-card">
                    <tr className="border-b border-border text-left text-xs text-muted-foreground">
                      <th className="px-4 py-2 font-medium">{t('cloudnet:iam.col.project')}</th>
                      <th className="px-4 py-2 font-medium">{t('cloudnet:iam.col.member')}</th>
                      <th className="px-4 py-2 font-medium">{t('cloudnet:iam.col.role')}</th>
                      <th className="px-4 py-2 font-medium">{t('cloudnet:iam.col.risk')}</th>
                    </tr>
                  </thead>
                  <tbody>
                    {pageRows.map((r, i) => (
                      <tr key={`${r.project}-${r.member}-${r.role}-${i}`} className="border-b border-border">
                        <td className="px-4 py-2 text-xs">{r.project ?? '—'}</td>
                        <td className="max-w-[320px] truncate px-4 py-2 font-mono text-[11px]" title={r.member}>
                          {r.member}
                        </td>
                        <td className="max-w-[280px] truncate px-4 py-2 font-mono text-[11px]">
                          {r.role}
                        </td>
                        <td className="px-4 py-2">
                          {r.severity ? (
                            // 原因直接显示在徽章里，不藏 title：
                            // "过宽"三个字没法处置，"拥有 roles/owner"一眼知道要改什么。
                            // ⚠️ 等级要分色：critical 和 medium 都染成红色的话，
                            //	一眼看不出先处理哪几条
                            <Badge tone={r.severity === 'medium' ? 'warn' : 'bad'}>
                              {r.issue || t('cloudnet:iam.risky')}
                            </Badge>
                          ) : (
                            <span className="text-xs text-muted-foreground">—</span>
                          )}
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
              <Pagination
                page={search.page}
                size={search.size}
                total={rows.length}
                onPage={(p) => setSearch({ page: p })}
                onSize={(s) => setSearch({ size: s })}
                rangeLabel={(f, to, total) => t('common:pagination.range', { from: f, to, total })}
                totalLabel={(total) => t('common:pagination.total', { count: total })}
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
