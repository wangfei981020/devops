import { tError, type Locale, useTranslation } from '@ops/i18n'
import {
  AsyncBoundary,
  Badge,
  Button,
  DataTable,
  EmptyState,
  type LoadError,
  SearchInput,
  Select,
  TableSkeleton,
  fromQuery,
} from '@ops/ui'
import { toErrorInfo } from '@ops/api'
import { ChevronLeft, ChevronRight, Download, Server } from 'lucide-react'
import { useNavigate, useSearch } from '@tanstack/react-router'
import { useMemo, useState } from 'react'
import { HostDrawer } from './HostDrawer.js'
import { buildColumns, isStale } from './columns.js'
import { type Host, type HostListResult, type StatusFilter, useHosts } from './queries.js'

type TFn = (key: string, opts?: Record<string, unknown>) => string

const PAGE_SIZE = 50

/**
 * 把 API 异常翻译成界面能消费的形态。
 *
 * 文案一律走语言包 —— 后端只返回 error code + 参数，
 * 拼好的句子发过来的话，英文界面就永远漏中文。
 */
function makeToLoadError(t: TFn) {
  return (e: unknown): LoadError => {
    const n = toErrorInfo(e)
    return {
      cause: tError(t, n.messageKey, n.params),
      detail: n.detail,
      // 鉴权、永久性、授权限制类重试一万次也一样，不给重试按钮免得让人白等
      retryable: n.retryable,
    }
  }
}

export function HostsPage() {
  const { t, i18n } = useTranslation()
  const locale = i18n.language as Locale
  // 筛选状态存在 URL 里，不是组件 state。
  //
  // 排障时"你看这批机器"要能直接把链接发给同事；存在组件里的话，
  // 分享出去的永远是默认视图，而对方看不出差别 —— 最容易被忽略的一种失真。
  // 顺带前进/后退也自动可用。
  const { page, status, project, provider, q: keyword, detail } = useSearch({ from: '/resources/hosts' })
  const navigate = useNavigate({ from: '/resources/hosts' })

  /** 改筛选条件时回到第一页 —— 停在第 5 页而结果只有 2 页，用户看到空列表会以为没数据。 */
  const patch = (
    next: Partial<{
      status: StatusFilter
      project: string
      provider: string
      q: string
      page: number
      detail: number
    }>,
  ) =>
    void navigate({
      search: (prev) => ({ ...prev, ...next, ...(next.page === undefined ? { page: 1 } : {}) }),
    })

  const query = useHosts({ page, size: PAGE_SIZE, status, project, provider, q: keyword })
  const freshness = query.data?.freshness ?? null
  const columns = useMemo(() => buildColumns(t, locale, freshness), [t, locale, freshness])
  const toLoadError = useMemo(() => makeToLoadError(t), [t])

  // 空态判据是「本页没数据且总数为 0」。
  // 只看 items.length 的话，翻到越界页码也会被当成"没有数据"，
  // 而那其实是"这一页没有"。
  const state = fromQuery<HostListResult>(query, (d) => d.total === 0, toLoadError)

  return (
    <div className="flex flex-col">
      <AsyncBoundary
        state={state}
        errorTitle={t('hosts:error.title')}
        retryLabel={t('common:action.retry')}
        onRetry={() => void query.refetch()}
        pending={
          <div className="p-5">
            {/* 列宽与真实表格一致，数据到位时不跳动 */}
            <TableSkeleton columns={[22, 10, 12, 16, 10, 12, 10, 10]} rows={8} />
          </div>
        }
        empty={
          <EmptyState
            icon={<Server />}
            title={t('hosts:emptyNoSource.title')}
            reason={t('hosts:emptyNoSource.hint')}
            action={{
              label: t('hosts:emptyNoSource.action'),
              // ⚠️ 这里原来写的是 `window.location.hash = '#/admin/datasources'`。
              // 本应用用的是 history 路由**不是 hash 路由** —— 设置 hash 只会
              // 在地址栏尾巴上挂一个 #，页面纹丝不动。
              // 按钮看着正常、点了没反应、控制台也不报错，是个纯死按钮。
              onClick: () =>
                void navigate({ to: '/admin/datasources', search: { page: 1, size: 50, kind: 'all', q: '' } }),
            }}
          />
        }
      >
        {(data) => {
          const f = data.facets
          const statusCount = (k: string) => f.status?.[k] ?? 0
          const totalAll =
            statusCount('running') + statusCount('stopped') + statusCount('destroyed')
          const projects = Object.keys(f.project ?? {}).sort()
          // ⚠️ 判据要涵盖所有维度：漏一个的话，只按厂商筛出 0 条时
          //	空态会说「还没有主机数据」——那是"没有"，而真相是"这个条件下没有"
          const filtered =
            status !== 'all' || project !== 'all' || provider !== 'all' || keyword !== ''
          const providers = Object.keys(f.provider ?? {}).sort()
          const lastPage = Math.max(1, Math.ceil(data.total / PAGE_SIZE))

          return (
            <>
              <div className="flex flex-wrap items-center gap-2 border-b border-border px-4 py-3">
                <SearchInput
                  value={keyword}
                  onChange={(v) => patch({ q: v })}
                  placeholder={t('hosts:filter.searchPlaceholder')}
                  clearLabel={t('common:filter.clearSearch')}
                  className="w-[228px]"
                />
                {/* 计数来自服务端 facets，是**全量**统计而非当前页 ——
                    按当前页统计的话这些数字全是错的，而且看起来完全正常 */}
                <Select<StatusFilter>
                  label={t('common:filter.status')}
                  value={status}
                  onChange={(v) => patch({ status: v })}
                  options={[
                    { value: 'all', label: t('hosts:filter.all'), count: totalAll },
                    { value: 'running', label: t('hosts:filter.running'), count: statusCount('running') },
                    { value: 'stopped', label: t('hosts:filter.stopped'), count: statusCount('stopped') },
                    {
                      value: 'destroyed',
                      label: t('hosts:filter.destroyed'),
                      count: statusCount('destroyed'),
                    },
                  ]}
                />
                {/* 🔴 标签必须是「项目」不是「集群」。
                    这个下拉筛的是 `project`（GCP 项目），而标签写着「集群」——
                    于是选一个项目、以为筛的是集群。项目和集群是两个维度，
                    一个项目里可以有多个集群（OPSCMDB-037 的第二处）。 */}
                <Select
                  label={t('hosts:filter.project')}
                  value={project}
                  onChange={(v) => patch({ project: v })}
                  options={[
                    { value: 'all', label: t('hosts:filter.all'), count: totalAll },
                    ...projects.map((p) => ({ value: p, label: p, count: f.project?.[p] ?? 0 })),
                  ]}
                />
                {/* 厂商筛选：后端 facets 一直在算这一维、Filters 也认它，
                    只是前端从来没接 —— 算了没人用（OPSCMDB-028 GAP-9）。
                    ⚠️ 只有一个厂商时不显示：一个只有「全部 + GCP」的下拉
                    是在暗示"还有别的厂商"，而事实是只接了一个。 */}
                {providers.length > 1 ? (
                  <Select
                    label={t('hosts:filter.provider')}
                    value={provider}
                    onChange={(v) => patch({ provider: v })}
                    options={[
                      { value: 'all', label: t('hosts:filter.all'), count: totalAll },
                      ...providers.map((p) => ({
                        value: p,
                        label: p,
                        count: f.provider?.[p] ?? 0,
                      })),
                    ]}
                  />
                ) : null}

                {/* 🔴 「这批数据可能已经过期了」必须在工具条上说一次。
                    逐行标红只有翻到那一列才看得见，而人是先看状态列的 ——
                    一台挂了 17 小时的机器在这一页看着依然「运行中」
                    （OPSCMDB-031 P1-5）。 */}
                <StaleBanner
                  items={data.items}
                  freshness={freshness}
                  t={t}
                  onGoDatasources={() =>
                    void navigate({
                      to: '/admin/datasources',
                      search: { page: 1, size: 50, kind: 'all', q: '' },
                    })
                  }
                />

                {statusCount('destroyed') > 0 ? (
                  // 明确告诉用户口径：成本里排除了已销毁机器。
                  // 不说的话，界面上的成本和账单对不上，用户会怀疑数据错了。
                  <Badge tone="info" dot={false} className="h-6">
                    {t('hosts:note.destroyedExcluded', { count: statusCount('destroyed') })}
                  </Badge>
                ) : null}

                <Button
                  variant="ghost"
                  size="sm"
                  className="ml-auto"
                  icon={<Download className="size-3.5" />}
                >
                  {t('common:action.export')}
                </Button>
              </div>

              {data.items.length === 0 ? (
                // 筛选后为空 ≠ 本来就没有数据。
                // 两种空态的原因和出路完全不同，必须分开写。
                <EmptyState
                  icon={<Server />}
                  title={t('hosts:empty.title')}
                  reason={t('hosts:empty.hint', { total: totalAll })}
                  action={{
                    label: t('hosts:empty.action'),
                    onClick: () => patch({ status: 'all', project: 'all', provider: 'all', q: '' }),
                  }}
                />
              ) : (
                <DataTable<Host>
                  columns={columns}
                  data={data.items}
                  rowKey={(h) => h.id}
                  selectedKey={detail ? String(detail) : null}
                  // 再点一次同一行收起 —— 连续看多台时不用先去点关闭
                  onRowClick={(h) => patch({ detail: Number(h.id) === detail ? 0 : Number(h.id), page })}
                  maxHeight="calc(100vh - 210px)"
                />
              )}

              <div className="flex items-center gap-3 border-t border-border px-4 py-2 text-xs text-muted-foreground">
                <span>
                  {/*
                    搜索词存在时不显示「共 N 台」——后端的 facets 是基于关键词过滤后的
                    结果集统计的，那个 N 恒等于匹配数，写出来就成了「匹配 2 台，共 2 台」，
                    读起来像系统里总共只有 2 台。
                    只有维度筛选（状态/集群）时，facets 才是全量，对比才有意义。
                  */}
                  {keyword
                    ? t('hosts:filter.matchedOnly', { count: data.total })
                    : filtered
                      ? t('hosts:filter.matched', { count: data.total, total: totalAll })
                      : t('common:pagination.total', { count: data.total })}
                </span>
                {data.total > PAGE_SIZE ? (
                  <div className="ml-auto flex items-center gap-2">
                    <Button
                      variant="ghost"
                      size="sm"
                      disabled={page <= 1}
                      onClick={() => patch({ page: page - 1 })}
                      aria-label={t('common:pagination.prev')}
                    >
                      <ChevronLeft className="size-3.5" />
                    </Button>
                    <span className="tabular">
                      {page} / {lastPage}
                    </span>
                    <Button
                      variant="ghost"
                      size="sm"
                      disabled={page >= lastPage}
                      onClick={() => patch({ page: page + 1 })}
                      aria-label={t('common:pagination.next')}
                    >
                      <ChevronRight className="size-3.5" />
                    </Button>
                  </div>
                ) : null}
              </div>
            </>
          )
        }}
      </AsyncBoundary>

      <HostDrawer ciId={detail ? String(detail) : null} onClose={() => patch({ detail: 0, page })} />
    </div>
  )
}

/**
 * 「本页有 N 台主机的数据已超过判据」。
 *
 * ⚠️ 只统计**当前页**，文案也要这么写。说成"共 N 台"是错的
 * （后端没给全量的过期计数），而一个错的总数比没有总数更糟。
 *
 * ⚠️ 判据取不到（freshness.known=false）时这一条不出现 ——
 * 不知道的时候不报警。列上仍然显示真实时间，人能自己看出 17 小时。
 */
function StaleBanner({
  items,
  freshness,
  t,
  onGoDatasources,
}: {
  items: Host[]
  freshness: { staleAfterSeconds: number; known: boolean } | null
  t: (k: string, o?: Record<string, unknown>) => string
  onGoDatasources: () => void
}) {
  if (!freshness?.known) return null
  const n = items.filter((h) => h.lastSyncAt && isStale(h.lastSyncAt, freshness)).length
  if (n === 0) return null
  return (
    <div className="flex items-center gap-1.5">
      <Badge tone="warn" dot={false} className="h-6">
        {t('hosts:note.stale', {
          count: n,
          hours: Math.round(freshness.staleAfterSeconds / 3600),
        })}
      </Badge>
      {/* 光说"过期了"没有下一步。同步是在数据源页做的 */}
      <button
        type="button"
        onClick={onGoDatasources}
        className="cursor-pointer text-xs text-brand underline-offset-2 hover:underline"
      >
        {t('hosts:note.staleAction')}
      </button>
    </div>
  )
}
