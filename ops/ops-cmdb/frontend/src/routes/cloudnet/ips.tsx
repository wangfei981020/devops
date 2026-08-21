import { toErrorInfo } from '@ops/api'
import { tError, useTranslation } from '@ops/i18n'
import {
  AsyncBoundary,
  Badge,
  type LoadError,
  NotIngested,
  Pagination,
  SearchInput,
  Select,
  TableSkeleton,
  fromQuery,
} from '@ops/ui'
import { getRouteApi, useNavigate } from '@tanstack/react-router'
import { useCloudIps } from './queries.js'

const route = getRouteApi('/resources/ips')

/**
 * IP 台账。
 *
 * ⚠️ 闲置的排最前面：预留了却没绑任何东西的静态 IP **在持续计费**，
 * 而它在云控制台里和正常 IP 长得一模一样。这一页存在的主要理由就是把它们捞出来。
 */
export function CloudIpsPage() {
  const { t } = useTranslation()
  const search = route.useSearch()
  const navigate = useNavigate()
  const query = useCloudIps()

  const setSearch = (patch: Record<string, string | number>) =>
    void navigate({ to: '/resources/ips', search: { ...search, ...patch } })

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
          placeholder={t('cloudnet:ips.searchPlaceholder')}
          clearLabel={t('common:filter.clearSearch')}
        />
        <Select<string>
          label={t('cloudnet:ips.filter.usage')}
          value={search.usage}
          onChange={(v) => setSearch({ usage: v, page: 1 })}
          options={[
            { value: 'all', label: t('common:filter.all') },
            { value: 'idle', label: t('cloudnet:ips.idleOnly') },
          ]}
        />
      </div>

      <AsyncBoundary
        state={fromQuery(query, (d) => d.length === 0, toLoadError)}
        errorTitle={t('cloudnet:ips.error')}
        retryLabel={t('common:action.retry')}
        onRetry={() => void query.refetch()}
        pending={<TableSkeleton columns={[16, 10, 26, 16, 12]} rows={8} />}
        empty={
          <div className="p-6">
            <NotIngested title={t('cloudnet:ips.empty.title')} reason={t('cloudnet:ips.empty.reason')} />
          </div>
        }
      >
        {(all) => {
          const kw = search.q.trim().toLowerCase()
          const rows = all
            .filter((r) => (search.usage === 'idle' ? r.idle === true : true))
            .filter((r) =>
              kw
                ? `${r.ip ?? ''} ${r.owner ?? ''} ${r.project ?? ''}`.toLowerCase().includes(kw)
                : true,
            )
            // 闲置在前：这一页的价值就在这几行
            .sort((a, b) => Number(b.idle ?? false) - Number(a.idle ?? false))
          const from = (search.page - 1) * search.size
          const pageRows = rows.slice(from, from + search.size)
          return (
            <>
              <div className="min-h-0 flex-1 overflow-auto">
                <table className="w-full text-[13px]">
                  <thead className="sticky top-0 z-10 bg-card">
                    <tr className="border-b border-border text-left text-xs text-muted-foreground">
                      <th className="px-4 py-2 font-medium">{t('cloudnet:ips.col.ip')}</th>
                      <th className="px-4 py-2 font-medium">{t('cloudnet:ips.col.kind')}</th>
                      <th className="px-4 py-2 font-medium">{t('cloudnet:ips.col.owner')}</th>
                      <th className="px-4 py-2 font-medium">{t('cloudnet:ips.col.region')}</th>
                      <th className="px-4 py-2 font-medium">{t('cloudnet:ips.col.usage')}</th>
                    </tr>
                  </thead>
                  <tbody>
                    {pageRows.map((r) => (
                      <tr key={`${r.ip}-${r.kind}`} className="border-b border-border">
                        <td className="px-4 py-2 font-mono text-xs">{r.ip}</td>
                        <td className="px-4 py-2">
                          {/* 🔴 优先用码值按 locale 渲染；没有码值才回落到后端那份中文
                              （老版本后端不发 kind_code）。原样透传会让英文界面显示中文 */}
                          <Badge tone="mute">
                            {r.kind_code
                              ? t(`cloudnet:ipKind.${r.kind_code}`)
                              : (r.kind ?? '—')}
                          </Badge>
                        </td>
                        <td className="truncate px-4 py-2">
                          <OwnerCell owner={r.owner} unbound={r.owner_unbound} t={t} />
                        </td>
                        <td className="px-4 py-2 text-xs text-muted-foreground">
                          {r.region || '—'}
                        </td>
                        <td className="px-4 py-2">
                          {r.idle ? (
                            <Badge tone="warn">{t('cloudnet:ips.idle')}</Badge>
                          ) : (
                            <span className="text-xs text-muted-foreground">
                              {t('cloudnet:ips.inUse')}
                            </span>
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

/**
 * 「使用者」列。
 *
 * ⚠️ GKE 给 LB 转发规则/后端服务生成的内部名是一串 32 位十六进制，
 * 对人完全没有意义：
 *
 *   a02592d05529140cb9dd097cc094cad8
 *   a9ead988e636d4446a6389fc1c7fb14c
 *
 * 而"这个 IP 是谁在用"正是这一列存在的**唯一理由**（OPSCMDB-031 P1-11）。
 * 同一列里 `public-uat-router`、`fgt-1-eu-west3` 这些可读的值一对比就很明显。
 *
 * 理想做法是反查到它所属的 Service/Ingress（CMDB 里有 k8s_services 和 LB 后端数据）。
 * 那需要后端做关联，还没做；在那之前**至少要标注这串东西是什么**，
 * 并告诉人去哪儿查 —— 裸展示一串哈希等于把问题丢回给用户。
 */
function OwnerCell({
  owner,
  unbound,
  t,
}: {
  owner?: string
  unbound?: boolean
  t: (k: string, o?: Record<string, unknown>) => string
}) {
  // 「未绑定」是占位符不是负责人名 —— 后端标了 unbound 就按 locale 渲染
  if (unbound) return <span className="text-muted-foreground">{t('cloudnet:misc.unbound')}</span>
  if (!owner) return <span className="text-muted-foreground">—</span>
  if (!isGKEGeneratedName(owner)) return <span>{owner}</span>
  return (
    <span className="inline-flex min-w-0 items-center gap-1.5" title={t('cloudnet:ips.gkeNameHint')}>
      <span className="truncate font-mono text-xs text-muted-foreground">{owner}</span>
      <Badge tone="mute">{t('cloudnet:ips.gkeName')}</Badge>
    </span>
  )
}

/**
 * 是不是 GKE 自动生成的名字。
 *
 * 判据：32 位纯十六进制。
 * ⚠️ 刻意卡死长度，不用"看着像哈希"这种模糊判断 ——
 * 客户自己起的名字里也可能有一段十六进制，误判会把可读的名字标成"自动生成"，
 * 那比不标更糟。
 */
function isGKEGeneratedName(s: string) {
  return /^[0-9a-f]{32}$/.test(s)
}
