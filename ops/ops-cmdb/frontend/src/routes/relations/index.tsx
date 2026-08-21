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
import { Share2 } from 'lucide-react'
import { useMemo, useState } from 'react'
import { WriteButton } from '../../components/WriteButton.js'
import { RelationDialog } from './RelationDialog.js'
import { RelationsGraphPage } from './GraphPage.js'
import { relationColumns } from './columns.js'
import { type RelationListResult, useRelations,
  useTopologyDomains } from './queries.js'

export function RelationsPage() {
  // 拓扑域名：这套资源对外暴露了哪些域名（OPSCMDB-021）
  const domains = useTopologyDomains()
  const { t } = useTranslation()
  const { rel_type, page, size, q: keyword, view } = useSearch({ from: '/topology' })
  const navigate = useNavigate({ from: '/topology' })
  const patch = (next: Record<string, string | number>) =>
    void navigate({ search: (prev) => ({ ...prev, ...next }) })

  const query = useRelations({ page, size, q: keyword, rel_type })
  const [adding, setAdding] = useState(false)
  const columns = useMemo(() => relationColumns(t), [t])
  const toLoadError = (e: unknown): LoadError => {
    const n = toErrorInfo(e)
    return { cause: tError(t, n.messageKey, n.params), detail: n.detail, retryable: n.retryable, kind: n.kind === 'license' ? 'license' : undefined, feature: n.params?.feature as string | undefined }
  }
  const filtered = keyword !== '' || rel_type !== 'all'

  // ⚠️ 工具条**必须在 AsyncBoundary 外面**（同一个 bug 的第四处）。
  // 写在 children 里的话，图谱为空时整条工具条不渲染 ——
  // 而"手工建一条边"正是空图谱时唯一能做的事。
  // 判据：工具条只依赖筛选状态，不依赖数据；用到数据的只有计数，可空。
  const facets = query.data?.facets?.rel_type
  const total = query.data?.total

  // 视图切换条。⚠️ 放在最外层、不依赖任何数据 ——
  // 图查不出来时也得能切回列表，否则人被卡在一个空页面上
  const switcher = (
    <div className="flex items-center gap-2 border-b border-border px-4 py-2">
      {(['graph', 'list'] as const).map((v) => (
        <button
          key={v}
          type="button"
          onClick={() => void navigate({ search: (p) => ({ ...p, view: v }) })}
          className={`cursor-pointer rounded-[var(--radius)] border px-2.5 py-1 text-[13px] transition-colors duration-150 ${
            view === v
              ? 'border-primary bg-secondary text-foreground'
              : 'border-border text-muted-foreground hover:bg-secondary'
          }`}
        >
          {t(`relations:view.${v}`)}
        </button>
      ))}
    </div>
  )

  if (view === 'graph') {
    return (
      <div className="flex h-full min-h-0 flex-col">
        {switcher}
        <div className="min-h-0 flex-1">
          <RelationsGraphPage />
        </div>
      </div>
    )
  }

  return (
    <div className="flex flex-col">
      {switcher}
      <div className="flex flex-wrap items-center gap-2 border-b border-border px-4 py-3">
        <SearchInput
          value={keyword}
          onChange={(v) => patch({ q: v })}
          placeholder={t('relations:filter.searchPlaceholder')}
          clearLabel={t('common:filter.clearSearch')}
          className="w-[240px]"
        />
        <Select<string>
          label={t('relations:filter.relType')}
          value={rel_type}
          onChange={(v) => patch({ rel_type: v })}
          options={[
            { value: 'all', label: t('common:filter.all'), count: facets?.all },
            ...Object.keys(facets ?? {})
              .filter((k) => k !== 'all' && k !== '')
              .sort()
              .map((v) => ({ value: v, label: t(`relations:rel_type.${v}`, { defaultValue: v }), count: facets?.[v] })),
          ]}
        />
        <span className="text-xs text-muted-foreground">{t('relations:coverageNote')}</span>
        {total != null ? (
          <span className="ml-auto text-xs text-muted-foreground">
            {t('relations:total', { count: total })}
          </span>
        ) : (
          <span className="ml-auto" />
        )}
        {/* ⚠️ 建边入口必须在这儿：图上一条边都没有时，正是最需要手工补一条的时候 */}
        <WriteButton perm="cmdb:manage_basic" size="sm" onClick={() => setAdding(true)}>
          {t('relations:form.entry')}
        </WriteButton>
      </div>

      {/* 域名与图谱互补：图谱画已记录的关系，域名是从主机头台账推出来的。
          两个都空才说明"没关联"，只有一个空不能下结论 */}
      {(domains.data?.length ?? 0) > 0 ? (
        <DomainTags domains={domains.data ?? []} t={t} />
      ) : null}

      <AsyncBoundary
        state={fromQuery<RelationListResult>(query, (d) => d.total === 0, toLoadError)}
        errorTitle={t('relations:error.title')}
        retryLabel={t('common:action.retry')}
        onRetry={() => void query.refetch()}
        pending={
          <div className="p-5">
            <TableSkeleton columns={[26, 14, 26, 16, 12]} rows={6} />
          </div>
        }
        empty={
          <EmptyState
            icon={<Share2 />}
            title={t('relations:empty.title')}
            reason={filtered ? t('relations:empty.filtered') : t('relations:empty.noSource')}
            action={
              filtered
                ? { label: t('common:filter.clearAll'), onClick: () => patch({ q: '', rel_type: 'all' }) }
                : null
            }
          />
        }
      >
        {(data) => (
          <>
            <DataTable data={data.items} columns={columns} rowKey={(x) => String(x.id)} />
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

      {adding ? <RelationDialog onClose={() => setAdding(false)} /> : null}
    </div>
  )
}

/**
 * 对外域名。
 *
 * # 原来的问题
 *
 * 775 个域名平铺占了大半屏，且混杂了多种东西 —— 通配符 `*`、`@`、
 * IP 地址、32 位哈希串（ACME 校验用的）和正常域名混在一起，
 * 没有分组、没有搜索、也没说点击会发生什么（OPSCMDB-031 P2-23）。
 *
 * # 三个改动
 *
 * 1. **默认折叠**。这一块是补充信息，不该抢走主表格的屏幕
 * 2. **有模块的排前面**。带模块 = 已经登记了归属，那才是有用的那批；
 *    哈希串和通配符天然没有模块，自然沉底
 * 3. **说清楚它是什么**，以及为什么里面有 `*` 和一串哈希
 *
 * ⚠️ 仍然截断，但把「还有 N 条没显示」写出来 ——
 * 静默截断会让人以为看到的就是全部。
 */
function DomainTags({
  domains,
  t,
}: {
  // name 可能缺（接口是可选字段）。⚠️ 缺名字的不该渲染成一个空标签 ——
  // 空标签看起来像 UI 坏了，而它其实是一条没有名字的数据
  domains: { name?: string; module?: string }[]
  t: (k: string, o?: Record<string, unknown>) => string
}) {
  const [open, setOpen] = useState(false)
  const [kw, setKw] = useState('')

  const q = kw.trim().toLowerCase()
  const named = domains.filter((d) => (d.name ?? '').trim() !== '')
  const matched = q
    ? named.filter((d) => `${d.name} ${d.module ?? ''}`.toLowerCase().includes(q))
    : named
  // 有模块的排前面：那是已经登记了归属的那批，也是唯一有排查价值的
  const sorted = [...matched].sort(
    (a, b) => Number(!!b.module) - Number(!!a.module) || (a.name ?? '').localeCompare(b.name ?? ''),
  )
  const shown = sorted.slice(0, 40)
  const hidden = sorted.length - shown.length

  return (
    <div className="mb-3 rounded-[var(--radius)] border border-border px-3 py-2">
      <button
        type="button"
        onClick={() => setOpen((v) => !v)}
        className="flex w-full cursor-pointer items-baseline gap-2 text-left"
      >
        <h2 className="text-[11px] font-medium tracking-wide text-muted-foreground uppercase">
          {t('relations:domains.title', { n: named.length })}
        </h2>
        <span className="min-w-0 flex-1 text-[11px] text-muted-foreground">
          {open ? t('relations:domains.collapse') : t('relations:domains.expand')}
        </span>
      </button>

      {open ? (
        <>
          {/* 说清楚里面为什么有 `*` 和一串哈希 —— 不说的话它们看着像脏数据 */}
          <p className="mt-1 text-[11px] leading-relaxed text-muted-foreground">
            {t('relations:domains.hint')}
          </p>
          <input
            value={kw}
            onChange={(e) => setKw(e.target.value)}
            placeholder={t('relations:domains.searchPlaceholder')}
            className="mt-1.5 h-7 w-[220px] rounded-[var(--radius)] border border-border bg-card px-2 text-xs text-foreground"
          />
          <div className="mt-1.5 flex flex-wrap gap-1.5">
            {shown.map((d) => (
              <span
                key={d.name}
                className="rounded border border-border px-1.5 py-0.5 font-mono text-[11px]"
              >
                {d.name}
                {d.module ? <span className="ml-1 text-muted-foreground">{d.module}</span> : null}
              </span>
            ))}
          </div>
          {/* ⚠️ 截断必须说出来。静默截断会让人以为看到的就是全部 */}
          {hidden > 0 ? (
            <p className="mt-1 text-[11px] text-warning">
              {t('relations:domains.truncated', { n: hidden })}
            </p>
          ) : null}
          {sorted.length === 0 ? (
            <p className="mt-1 text-[11px] text-muted-foreground">
              {t('relations:domains.noMatch', { kw })}
            </p>
          ) : null}
        </>
      ) : null}
    </div>
  )
}
