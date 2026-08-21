import { toErrorInfo } from '@ops/api'
import { tError, useTranslation } from '@ops/i18n'
import {
  AsyncBoundary,
  Badge,
  type LoadError,
  NotIngested,
  Pagination,
  SearchInput,
  TableSkeleton,
  fromQuery,
} from '@ops/ui'
import { getRouteApi, useNavigate } from '@tanstack/react-router'
import { useFirewalls } from './queries.js'

const route = getRouteApi('/resources/firewalls')

/**
 * 防火墙规则。
 *
 * ⚠️ 高危规则排最前面，判定用**后端给的 risky**，不在前端另判一次：
 * 判据放两处必然分叉，而分叉的方向通常是前端更宽松（少一个条件），
 * 于是界面上一条高危规则看着像普通规则。
 *
 * ⚠️ 被禁用的规则要显式标出来。它不生效，但**留在那里**——
 * 和生效的规则长得一样的话，人会以为某个端口已经放开了（或已经封了）。
 */
export function FirewallsPage() {
  const { t } = useTranslation()
  const search = route.useSearch()
  const navigate = useNavigate()
  const query = useFirewalls()
  // ⚠️ 在边界外算：工具条在 AsyncBoundary 外面，不能依赖它的 children 参数。
  // 拿不到数据时是 0，按钮就不显示 —— 不会出现"有 0 条高危"这种没信息量的按钮
  const riskyCount = (query.data ?? []).filter((r) => r.high_risk).length

  const setSearch = (patch: Record<string, string | number | boolean>) =>
    void navigate({ to: '/resources/firewalls', search: { ...search, ...patch } })

  const toLoadError = (e: unknown): LoadError => {
    const n = toErrorInfo(e)
    return { cause: tError(t, n.messageKey, n.params), detail: n.detail, retryable: n.retryable, kind: n.kind === 'license' ? 'license' : undefined, feature: n.params?.feature as string | undefined }
  }

  return (
    <div className="flex h-full flex-col">
      <div className="flex flex-wrap items-center gap-2.5 border-b border-border px-4 py-2.5">
        <SearchInput
          value={search.q}
          onChange={(v) => setSearch({ q: v, page: 1 })}
          placeholder={t('cloudnet:fw.searchPlaceholder')}
          clearLabel={t('common:filter.clearSearch')}
          // 文案有 15 个汉字，用默认宽度会被截成"搜索规则名、放行端…"——
          // placeholder 的作用恰恰是告诉人"这里能搜什么"，截断等于把它作废
          className="w-[300px]"
        />
        {/*
          高危计数做成按钮，不是一行文字。
          「有 18 条高危」只是个数字，「是哪 18 条」才是人真正要的下一步；
          汇总项不可点是本项目反复出现的问题（同类：P1-9）。
        */}
        {riskyCount > 0 ? (
          <button
            type="button"
            onClick={() => setSearch({ risky: !search.risky, page: 1 })}
            className={`cursor-pointer rounded-[var(--radius)] border px-2.5 py-1 text-xs transition-colors duration-150 ${
              search.risky
                ? 'border-danger bg-danger/10 text-danger'
                : 'border-border text-muted-foreground hover:bg-secondary'
            }`}
          >
            {search.risky
              ? t('cloudnet:fw.riskyOnlyOn', { count: riskyCount })
              : t('cloudnet:fw.riskyOnly', { count: riskyCount })}
          </button>
        ) : null}
      </div>

      <AsyncBoundary
        state={fromQuery(query, (d) => d.length === 0, toLoadError)}
        errorTitle={t('cloudnet:fw.error')}
        retryLabel={t('common:action.retry')}
        onRetry={() => void query.refetch()}
        pending={<TableSkeleton columns={[22, 10, 8, 24, 20]} rows={8} />}
        empty={
          <div className="p-6">
            {/* 一条防火墙规则都没有几乎不可能：任何 VPC 都有默认规则。
                所以空 = 没采到，不是"没有规则" */}
            <NotIngested title={t('cloudnet:fw.empty.title')} reason={t('cloudnet:fw.empty.reason')} />
          </div>
        }
      >
        {(all) => {
          const kw = search.q.trim().toLowerCase()
          const rows = all
            .filter((r) => (search.risky ? r.high_risk === true : true))
            .filter((r) =>
              kw
                ? `${r.name ?? ''} ${r.protocols ?? ''} ${(r.source_ranges ?? []).join(',')}`
                    .toLowerCase()
                    .includes(kw)
                : true,
            )
            .sort((a, b) => Number(b.high_risk ?? false) - Number(a.high_risk ?? false))
          const from = (search.page - 1) * search.size
          const pageRows = rows.slice(from, from + search.size)
          return (
            <>
              <div className="min-h-0 flex-1 overflow-auto">
                <table className="w-full text-[13px]">
                  <thead className="sticky top-0 z-10 bg-card">
                    <tr className="border-b border-border text-left text-xs text-muted-foreground">
                      <th className="px-4 py-2 font-medium">{t('cloudnet:fw.col.name')}</th>
                      <th className="px-4 py-2 font-medium">{t('cloudnet:fw.col.direction')}</th>
                      <th className="px-4 py-2 font-medium">{t('cloudnet:fw.col.priority')}</th>
                      <th className="px-4 py-2 font-medium">{t('cloudnet:fw.col.source')}</th>
                      <th className="px-4 py-2 font-medium">{t('cloudnet:fw.col.allowed')}</th>
                    </tr>
                  </thead>
                  <tbody>
                    {pageRows.map((r, i) => (
                      <tr key={`${r.name}-${i}`} className="border-b border-border">
                        <td className="px-4 py-2">
                          <div className="flex items-center gap-2">
                            <span className="truncate">{r.name}</span>
                            {r.high_risk ? <Badge tone="bad">{t('cloudnet:fw.risky')}</Badge> : null}
                            {/* 禁用的规则不生效，但留在列表里。
                                不标出来的话，人会以为这个端口已经放开了 */}
                            {r.disabled ? <Badge tone="mute">{t('cloudnet:fw.disabled')}</Badge> : null}
                          </div>
                          {r.high_risk && r.risk_reason ? (
                            <p className="mt-0.5 text-[11px] text-danger">{r.risk_reason}</p>
                          ) : null}
                        </td>
                        <td className="px-4 py-2 text-xs">{r.direction ?? '—'}</td>
                        <td className="tabular px-4 py-2 text-xs">{r.priority ?? '—'}</td>
                        <td className="max-w-[240px] px-4 py-2 font-mono text-[11px]">
                          <div className="truncate">
                            {(r.source_ranges ?? []).join(', ') || '—'}
                          </div>
                          {/* 目标标签：这条规则**作用在哪些机器上**。旧版有这一列。
                              没有它，一条 0.0.0.0/0 放行 22 端口的规则看不出影响面 ——
                              是全 VPC 还是只有一台带标签的跳板机，风险差着数量级。
                              ⚠️ 空 = 作用于 **VPC 里所有实例**（GCP 语义），
                              这是最危险的那一档，必须显式说出来而不是留白。 */}
                          <div className="truncate text-muted-foreground">
                            {(r.target_tags ?? []).length > 0 ? (
                              <span title={(r.target_tags ?? []).join(', ')}>
                                {t('cloudnet:fw.targets')}: {(r.target_tags ?? []).join(', ')}
                              </span>
                            ) : (
                              <span className="text-warning">{t('cloudnet:fw.allInstances')}</span>
                            )}
                          </div>
                        </td>
                        <td className="max-w-[240px] truncate px-4 py-2 font-mono text-[11px]">
                          {r.protocols || '—'}
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
