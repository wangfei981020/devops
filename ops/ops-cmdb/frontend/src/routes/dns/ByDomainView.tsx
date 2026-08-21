import { toErrorInfo } from '@ops/api'
import { formatDateTime, tError, type Locale, useTranslation } from '@ops/i18n'
import {
  AsyncBoundary,
  Badge,
  EmptyState,
  type LoadError,
  Pagination,
  SearchInput,
  TableSkeleton,
  fromQuery,
} from '@ops/ui'
import { Globe, RefreshCw } from 'lucide-react'
import { useState } from 'react'
import { WriteButton } from '../../components/WriteButton.js'
import { DnsRecordsDialog } from '../domains/DnsRecordsDialog.js'
import {
  type Domain,
  type DomainListResult,
  useDomains,
  useSyncDomainRecords,
} from '../domains/queries.js'

// ⚠️ 同步和编辑是**两个**权限码，别混：
//	POST /api/domains/:ciid/sync-records → cmdb:sync_domains
//	记录的增删改（DnsRecordsDialog 内部自己判）→ cmdb:manage_domains
//	写错的话不报错也不白屏：不受限管理员一律放行，自测完全正常，
//	而普通角色看到的是一个永远置灰、提示里还写着不存在权限码的按钮。
const PERM = 'cmdb:sync_domains'

/**
 * 「按域名」视图 —— 复刻旧版 CMDB 的「DNS 记录」页。
 *
 * # 为什么要有它
 *
 * 菜单点「DNS 解析」，人想问的是**「这个域名下有哪些解析」**，
 * 而这一页原本给的是跨来源平铺的记录列表（三方对账、"配了但不生效"判定）。
 * 两个都有用，但前者是日常操作、后者是排查特定问题 ——
 * 把低频的放菜单上、把高频的藏进域名页的弹窗里，就是 OPSCMDB-036 的全部内容。
 * 代价很实在：旧版一屏能干的事，新版要「进域名页 → 找到域名 → ⋯ → 解析记录」三次点击。
 *
 * 🔴 **不是替换掉平铺视图**，是并列。平铺视图有旧版没有的东西
 * （三方并列 + 生效判定，生产上刚靠它查出 17 条真问题）。
 * 两者回答的是不同问题，合并成一个的结果通常是两个都答不好。
 *
 * # 域名清单用哪一份
 *
 * 用注册商台账 `/api/domains`（旧版也是这一份），不从解析记录反推 zone。
 * 反推会多出"没在注册商登记、但 Cloudflare 上有 zone"的域名 ——
 * 那种确实最容易失管，但那是**比旧版更好**而不是**追平旧版**，
 * 属于另一件事，要分开做、分开验（OPSCMDB-036 里写明了）。
 *
 * # 记录的增删改
 *
 * 直接复用 `DnsRecordsDialog`（531 行，新增/批量/编辑/删除/批量改 TTL/
 * 受保护记录不可删，一样不少）。**不要再写一份** —— 两份实现必然分叉，
 * 而分叉出来的差异会表现成"在这个入口能删、在那个入口删不掉"。
 */
export function ByDomainView({
  q,
  page,
  size,
  onSearch,
  locale,
}: {
  q: string
  page: number
  size: number
  onSearch: (patch: Record<string, string | number>) => void
  locale: Locale
}) {
  const { t } = useTranslation()
  const query = useDomains({ page, size, q })
  const sync = useSyncDomainRecords()
  const [openFor, setOpenFor] = useState<Domain | null>(null)
  const [syncing, setSyncing] = useState<number | null>(null)

  const toLoadError = (e: unknown): LoadError => {
    const n = toErrorInfo(e)
    return { cause: tError(t, n.messageKey, n.params), detail: n.detail, retryable: n.retryable, kind: n.kind === 'license' ? 'license' : undefined, feature: n.params?.feature as string | undefined }
  }

  return (
    <div className="flex flex-col">
      {/* ⚠️ 搜索框在 AsyncBoundary 外面：筛出 0 条时用来改条件的框不能跟着消失 */}
      <div className="flex flex-wrap items-center gap-2.5 border-b border-border px-4 py-2.5">
        <SearchInput
          value={q}
          onChange={(v) => onSearch({ q: v, page: 1 })}
          placeholder={t('dns:byDomain.searchPlaceholder')}
          clearLabel={t('common:filter.clearSearch')}
          className="w-[260px]"
        />
        {query.data ? (
          <span className="ml-auto text-xs text-muted-foreground">
            {t('dns:byDomain.total', { count: query.data.total })}
          </span>
        ) : null}
      </div>

      <AsyncBoundary
        state={fromQuery<DomainListResult>(query, (d) => d.items.length === 0, toLoadError)}
        errorTitle={t('dns:error.title')}
        retryLabel={t('common:action.retry')}
        onRetry={() => void query.refetch()}
        pending={
          <div className="p-5">
            <TableSkeleton columns={[26, 10, 14, 14, 16, 10, 10]} rows={8} />
          </div>
        }
        empty={
          <EmptyState
            icon={<Globe />}
            title={t('dns:byDomain.empty.title')}
            reason={q !== '' ? t('dns:byDomain.empty.filtered') : t('dns:byDomain.empty.none')}
            action={
              q !== ''
                ? { label: t('common:filter.clearAll'), onClick: () => onSearch({ q: '', page: 1 }) }
                : null
            }
          />
        }
      >
        {(data) => (
          <>
            <div className="flex flex-col divide-y divide-border">
              {data.items.map((d) => (
                <div key={d.ciId} className="flex flex-wrap items-center gap-3 px-4 py-2.5">
                  <button
                    type="button"
                    onClick={() => setOpenFor(d)}
                    className="min-w-[220px] cursor-pointer text-left text-[13px] font-medium text-brand-text underline-offset-2 hover:underline"
                  >
                    {d.name}
                  </button>

                  {/* 域名注册到期。⚠️ null = 读不出来，不是"还有 0 天"——
                      它可能下周就被释放。所以不能显示成 0 或空白。 */}
                  <span className="min-w-[150px] text-xs text-muted-foreground">
                    {d.daysLeft === null ? (
                      <span className="text-warning">{t('dns:byDomain.expiryUnknown')}</span>
                    ) : (
                      <>
                        {d.expiryAt ? formatDateTime(d.expiryAt, locale) : '—'}
                        <span className={d.daysLeft <= 30 ? 'ml-1 text-danger' : 'ml-1'}>
                          {t('dns:byDomain.daysLeft', { n: d.daysLeft })}
                        </span>
                      </>
                    )}
                  </span>

                  <span className="min-w-[90px] text-xs text-muted-foreground">
                    {d.registrar || '—'}
                  </span>

                  {/* 🔴 这里数的必须是**注册商侧的解析记录**（dnsRecords），
                      不是主机头台账（records）—— 点开的弹窗管的就是前者。
                      用错的话行上写着「2 条记录」、弹窗里却说"没有解析记录"
                      （实测 dev-example.com：台账 2 条、注册商侧 0 条）。
                      ⚠️ 0 条要显示成警示色而不是灰色：一个已登记的域名在注册商侧
                      一条解析都没有，要么是还没同步过，要么是解析真的没了 —— 都得看一眼。 */}
                  <Badge tone={d.dnsRecords > 0 ? 'info' : 'warn'}>
                    {t('dns:byDomain.records', { n: d.dnsRecords })}
                  </Badge>
                  {/* 台账条数单独标，别和上面那个混：两张表、两件事 */}
                  {d.records > 0 ? (
                    <span className="text-[11px] text-muted-foreground" title={t('dns:byDomain.ledgerHint')}>
                      {t('dns:byDomain.ledger', { n: d.records })}
                    </span>
                  ) : null}

                  {/* 最近同步。⚠️ null = 从没同步过，和"刚同步完"必须能分开：
                      台账是快照，不显示同步时间的话，三天前的数据会被当成当前状态。 */}
                  <span className="min-w-[150px] text-xs text-muted-foreground">
                    {d.syncedAt ? (
                      formatDateTime(d.syncedAt, locale)
                    ) : (
                      <span className="text-warning">{t('dns:byDomain.neverSynced')}</span>
                    )}
                  </span>

                  <WriteButton
                    perm={PERM}
                    size="sm"
                    className="ml-auto"
                    loading={sync.isPending && syncing === d.ciId}
                    onClick={() => {
                      setSyncing(d.ciId)
                      sync.mutate(d.ciId, { onSettled: () => setSyncing(null) })
                    }}
                  >
                    <RefreshCw className="size-3.5" aria-hidden="true" />
                    {t('dns:byDomain.sync')}
                  </WriteButton>
                </div>
              ))}
            </div>

            <Pagination
              page={page}
              size={size}
              total={data.total}
              onPage={(p) => onSearch({ page: p })}
              onSize={(n) => onSearch({ size: n, page: 1 })}
              rangeLabel={(f, to, total) => t('common:pagination.range', { from: f, to, total })}
              totalLabel={(total) => t('common:pagination.total', { count: total })}
              perPageLabel={t('common:pagination.perPage')}
              prevLabel={t('common:pagination.prev')}
              nextLabel={t('common:pagination.next')}
            />
          </>
        )}
      </AsyncBoundary>

      {/* 复用域名页那套完整的记录管理，不重写 */}
      {openFor ? <DnsRecordsDialog d={openFor} onClose={() => setOpenFor(null)} /> : null}
    </div>
  )
}
