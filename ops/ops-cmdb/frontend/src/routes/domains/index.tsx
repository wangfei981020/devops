import { actionMessage } from '../../lib/actionMessage.js'
import { toErrorInfo } from '@ops/api'
import { tError, type Locale, useTranslation } from '@ops/i18n'
import {
  AsyncBoundary,
  Banner,
  Button,
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
import { QualityView } from './QualityView.js'
import { WriteButton } from '../../components/WriteButton.js'
import { DomainDialog } from './DomainDialog.js'
import { RenewDialog } from './RenewDialog.js'
import { RenewalsDialog } from './RenewalsDialog.js'
import { useAutoLinkModules, useSyncDomains } from './renew.js'
import { Globe } from 'lucide-react'
import { useMemo, useState } from 'react'
import { domainColumns } from './columns.js'
import { type DomainHealth, type DomainListResult, useDomains } from './queries.js'

const PAGE_SIZE = 50

export function DomainsPage() {
  const { t, i18n } = useTranslation()
  const locale = i18n.language as Locale
  const { view, health, page, size, q: keyword } = useSearch({ from: '/resources/domains' })
  const navigate = useNavigate({ from: '/resources/domains' })
  const patch = (next: Partial<{ view: string; health: DomainHealth; q: string; page: number; size: number }>) =>
    void navigate({ search: (prev) => ({ ...prev, ...next }) })

  const query = useDomains({ page, size, health, q: keyword })
  const [renewing, setRenewing] = useState(false)
  // 续费记录：扣费历史台账（OPSCMDB-021）
  const [renewals, setRenewals] = useState(false)
  const [adding, setAdding] = useState(false)
  const sync = useSyncDomains()
  const autoLink = useAutoLinkModules()
  const labels: Record<NoValueKind, string> = {
    na: '—',
    notIngested: t('common:state.notIngested'),
    stopped: t('common:state.unknown'),
    unknown: t('common:state.unknown'),
  }

  const columns = useMemo(() => domainColumns(t, locale, labels), [t, locale, labels])
  const toLoadError = useMemo(
    () =>
      (e: unknown): LoadError => {
        const n = toErrorInfo(e)
        return { cause: tError(t, n.messageKey, n.params), detail: n.detail, retryable: n.retryable, kind: n.kind === 'license' ? 'license' : undefined, feature: n.params?.feature as string | undefined }
      },
    [t],
  )

  const state = fromQuery<DomainListResult>(query, (d) => d.total === 0, toLoadError)
  const filtered = keyword !== '' || health !== 'all'

  // ⚠️ 工具条**必须在 AsyncBoundary 外面**。
  //
  // 原来它写在 children 里，于是 total===0 走空态分支时整条工具条不渲染 ——
  // 而「立即同步」就在那条工具条上。结果是个死循环：
  // 要同步才有域名，有域名才看得见同步按钮。全新安装的人永远进不来。
  //
  // 判据很简单：**工具条只依赖筛选状态，不依赖数据**。
  // 唯一用到数据的是筛选项右边的计数，那个可空（facets?.xxx），空态下不显示数字即可。
  const facets = query.data?.facets?.health
  // 「N 个需要立刻处理」与总数：空态下都取不到，各自不渲染而不是显示 0
  const urgent = (facets?.expired ?? 0) + (facets?.unresolved ?? 0)
  const total = query.data?.total

  const tabs = (
    <div className="flex items-center gap-1 border-b border-border px-4 pt-2.5">
      {(
        [
          ['ledger', t('domains:view.ledger')],
          ['quality', t('domains:view.quality')],
        ] as const
      ).map(([v, label]) => (
        <button
          key={v}
          type="button"
          onClick={() => patch({ view: v })}
          className={
            view === v
              ? 'cursor-pointer border-b-2 border-brand px-3 py-1.5 text-[13px] font-medium text-foreground'
              : 'cursor-pointer border-b-2 border-transparent px-3 py-1.5 text-[13px] text-muted-foreground hover:text-foreground'
          }
        >
          {label}
        </button>
      ))}
    </div>
  )

  // 🔴 对账做成**页签**而不是新菜单：它回答的还是"域名怎么样"这个问题。
  //	单独占一个菜单会让人以为那是另一套东西，而它恰恰要和台账并排看。
  if (view === 'quality') {
    return (
      <div className="flex flex-col">
        {tabs}
        <QualityView locale={locale} />
      </div>
    )
  }

  return (
    <div className="flex flex-col">
          {tabs}
          <div className="flex flex-wrap items-center gap-2 border-b border-border px-4 py-3">
            <SearchInput
              value={keyword}
              onChange={(v) => patch({ q: v })}
              placeholder={t('domains:filter.searchPlaceholder')}
              clearLabel={t('common:filter.clearSearch')}
              className="w-[240px]"
            />
            <Select<DomainHealth>
              label={t('domains:filter.health')}
              value={health}
              onChange={(v) => patch({ health: v })}
              options={[
                { value: 'all', label: t('common:filter.all'), count: facets?.all },
                { value: 'expired', label: t('domains:health.expired'), count: facets?.expired },
                { value: 'unresolved', label: t('domains:health.unresolved'), count: facets?.unresolved },
                { value: 'unknown', label: t('domains:health.unknown'), count: facets?.unknown },
                { value: 'soon', label: t('domains:health.soon'), count: facets?.soon },
                { value: 'cert_soon', label: t('domains:health.certSoon'), count: facets?.cert_soon },
                // 被忽略的单独一档：它不是"正常"，只是我们决定暂时不管
                { value: 'ignored', label: t('domains:health.ignored'), count: facets?.ignored },
                { value: 'ok', label: t('domains:health.ok'), count: facets?.ok },
              ]}
            />
            {urgent > 0 ? (
              <span className="text-xs text-danger">
                {t('domains:urgentNote', { count: urgent })}
              </span>
            ) : null}
            {total != null ? (
              <span className="ml-auto text-xs text-muted-foreground">
                {t('domains:total', { count: total })}
              </span>
            ) : (
              <span className="ml-auto" />
            )}
            {/* 同步 / 自动关联：低频但必须有入口。
                结果直接显示在按钮旁边，不弹窗——这一页本来就是在看域名状态 */}
            {sync.isSuccess || autoLink.isSuccess ? (
              <span className="text-xs text-success">
                {actionMessage(t, sync.data ?? autoLink.data)}
              </span>
            ) : null}
            {sync.isError || autoLink.isError ? (
              (() => {
                // 🔴 主文案在前，技术细节挂 title。
                //	这里原来只显示 detail —— 而 detail 是**技术描述位**，
                //	动作类接口（HTTP 200 + ok:false）把后端那句中文原封不动放在那儿，
                //	于是英文界面下显示的是中文（实测）。
                //	主文案走 error_key（apiAction 已经归一好了），中文原句退到 title。
                const n = toErrorInfo(sync.error ?? autoLink.error)
                return (
                  <span
                    className="max-w-[240px] truncate text-xs text-danger"
                    title={n.detail || undefined}
                  >
                    {tError(t, n.messageKey, n.params)}
                  </span>
                )
              })()
            ) : null}
            <WriteButton
              perm="cmdb:sync_domains"
              size="sm"
              loading={sync.isPending}
              onClick={() => sync.mutate()}
            >
              {t('common:write.syncNow')}
            </WriteButton>
            <WriteButton
              perm="cmdb:manage_domains"
              size="sm"
              loading={autoLink.isPending}
              onClick={() => autoLink.mutate()}
            >
              {t('domains:autoLink')}
            </WriteButton>
            {/* ⚠️ 新增入口必须在这儿：注册商同步不到的域名（别处买的、内部域名）
                以前完全没有录入口 —— 后端 POST /api/domains 一直在，前端没接 */}
            <WriteButton perm="cmdb:manage_domains" size="sm" onClick={() => setAdding(true)}>
              {t('domains:form.createTitle')}
            </WriteButton>
            {/*
              续费入口放在域名页而不是单独一个菜单：
              人是在这一页看到"哪些快到期"之后才想续的。

              ⚠️ **但它不能是主按钮。**

              这是整个工具栏里唯一一个会**真实扣钱**的操作，原来却做成了
              紫色主按钮、放在最右边视觉最重的位置，还紧挨着「新增域名」，
              两者相距不到一个按钮宽度（OPSCMDB-031 P0-9）。

              CONVENTIONS §2.7.7 规定破坏性操作不做常驻主按钮，理由是
              "最少用的操作不该是视觉最重的"。**花钱的操作比删除更严格**：
              删除还能从备份恢复，扣费不能撤销 —— 而且续费是非幂等写，
              失败不等于没扣费（见 RenewDialog 里的说明）。

              所以：降成次要按钮 + 文案里带上「会扣费」+ 和左边的按钮之间
              加一道分隔，让它在视觉上不属于"日常操作"那一组。
            */}
            <span className="mx-0.5 h-4 w-px shrink-0 bg-border" />
            <WriteButton
              perm="cmdb:manage_domains"
              size="sm"
              onClick={() => setRenewing(true)}
            >
              {t('domains:renew.entry')}
            </WriteButton>
            {/*
              🔴 续费**记录**原来没有任何打开入口：`setRenewals` 全文件只有
              `onClose` 里那一处调用、传的是 false，state 永远是 false，
              弹窗永远不渲染（OPSCMDB-034）。git log 确认这个触发器
              从来没写过，不是改版时弄丢的。

              ⚠️ 它和左边那个「续费」是两回事，别混：
                续费    会真实扣费的写操作
                续费记录 只读，看历史每一笔的结果

              而这一页**最需要**这份记录：续费是非幂等写，失败不等于没扣费，
              出问题时第一件事就是回来查"那一笔到底成没成"。
              入口都没有的话，只能去翻数据库。
            */}
            <Button size="sm" onClick={() => setRenewals(true)}>
              {t('domains:renewals.entry')}
            </Button>
          </div>

      {/*
        ⚠️ 整列都是「未知」时，原因必须说出来。

        实测「解析」「证书到期」两列 62 条全是「未知」——
        「未知」比空白好（没把缺失渲染成正常），但**没说为什么**：
        用户无法判断是"这些域名没配 CDN/证书"还是"数据还没采"，
        而这两者的下一步完全相反（P1-12）。

        原因是**整列共同的**（对应的检测没跑过），所以放一条横幅比
        每一格加 tooltip 有效得多 —— 62 个 tooltip 说的是同一句话。
      */}
      <ColumnUnknownNote rows={query.data?.items ?? []} t={t} />

      <AsyncBoundary
        state={state}
        errorTitle={t('domains:error.title')}
        retryLabel={t('common:action.retry')}
        onRetry={() => void query.refetch()}
        pending={
          <div className="p-5">
            <TableSkeleton columns={[26, 14, 12, 18, 8]} rows={6} />
          </div>
        }
        empty={
          <EmptyState
            icon={<Globe />}
            title={t('domains:empty.title')}
            reason={filtered ? t('domains:empty.filtered') : t('domains:empty.noSource')}
            action={
              filtered
                ? { label: t('common:filter.clearAll'), onClick: () => patch({ q: '', health: 'all' }) }
                : null
            }
          />
        }
      >
        {(data) => {
          const hf = data.facets.health
          const urgent = (hf?.expired ?? 0) + (hf?.unresolved ?? 0)

          return (
            <>

              <DataTable data={data.items} columns={columns} rowKey={(d) => String(d.ciId)} />
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

      {renewing ? <RenewDialog onClose={() => setRenewing(false)} /> : null}
      {renewals ? <RenewalsDialog onClose={() => setRenewals(false)} t={t} /> : null}
      {adding ? <DomainDialog onClose={() => setAdding(false)} /> : null}
    </div>
  )
}

/**
 * 「解析」「证书到期」两列整列未知时的原因说明。
 *
 * ⚠️ 只在**整列**都未知时出现。零星几条未知是正常的（某个域名确实没配证书），
 * 而整列未知一定是链路没通 —— 后者才需要指路。
 *
 * 判据用当前页的数据，够了：整列未知时任何一页都是全未知。
 */
function ColumnUnknownNote({
  rows,
  t,
}: {
  // 数据由父组件传进来 —— 子组件再调一次 useDomains 虽然会被 react-query 去重，
  // 但那是"碰巧不重复请求"，不是"设计上只请求一次"
  rows: { resolveStatus: string; certDaysLeft: number | null }[]
  t: (k: string, p?: Record<string, unknown>) => string
}) {
  if (rows.length === 0) return null
  const allResolveUnknown = rows.every((r) => !r.resolveStatus)
  const allCertUnknown = rows.every((r) => r.certDaysLeft === null)
  if (!allResolveUnknown && !allCertUnknown) return null
  return (
    <div className="px-4 pt-3">
      <Banner tone="warn">
        <span className="font-medium">{t('domains:unknownColumns.title')}</span>
        {allResolveUnknown ? (
          <span className="mt-0.5 block">{t('domains:unknownColumns.resolve')}</span>
        ) : null}
        {allCertUnknown ? (
          <span className="mt-0.5 block">{t('domains:unknownColumns.cert')}</span>
        ) : null}
      </Banner>
    </div>
  )
}
