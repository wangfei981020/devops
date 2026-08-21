import { toErrorInfo } from '@ops/api'
import { tError, useTranslation } from '@ops/i18n'
import {
  AsyncBoundary,
  DataTable,
  EmptyState,
  type LoadError,
  Pagination,
  SearchInput,
  Select,
  TableSkeleton,
  fromQuery,
} from '@ops/ui'
import { useNavigate, useSearch } from '@tanstack/react-router'
import { Radar } from 'lucide-react'
import { useMemo } from 'react'
import { taskColumns } from './columns.js'
import { type TaskListResult, useTasks } from './queries.js'

/**
 * 巡检。
 *
 * # 这一页和「定时任务」页的分工
 *
 * 同一批任务的两个视图，但回答不同的问题（P1-43 质疑过为什么有两个）：
 *
 *   巡检页     体检结论 ——「有没有东西坏了、坏在哪」，按状态筛，失败的一眼可见
 *   定时任务页 调度管理 ——「开关、频率、每一次执行的明细」
 *
 * 所以两页必须**互相有入口**：在巡检页看到一条失败，下一步是去看它的执行记录；
 * 这一页现在每行都有「去看执行记录」。
 *
 * # ⚠️ 原来这一页是纯只读的
 *
 * 15 个任务里 2 个在失败，界面把失败诚实地说出来了，
 * 但**行内没有任何操作按钮**（DOM 实测 rowButtons: []）——
 * 既不能重跑，也没有去看原因的入口，只能等下一次 cron（P1-40）。
 * 「诚实之后没有下一步」等于把人留在原地。
 */
export function TasksPage() {
  const { t } = useTranslation()
  const { state, page, size, q: keyword } = useSearch({ from: '/runtime/inspection' })
  const navigate = useNavigate({ from: '/runtime/inspection' })
  const patch = (next: Record<string, string | number>) =>
    void navigate({ search: (prev) => ({ ...prev, ...next }) })

  const query = useTasks({ page, size, q: keyword, state })
  const columns = useMemo(() => taskColumns(t), [t])
  const toLoadError = (e: unknown): LoadError => {
    const n = toErrorInfo(e)
    return { cause: tError(t, n.messageKey, n.params), detail: n.detail, retryable: n.retryable, kind: n.kind === 'license' ? 'license' : undefined, feature: n.params?.feature as string | undefined }
  }
  const filtered = keyword !== '' || state !== 'all'
  const facets = query.data?.facets
  const broken = (facets?.state?.failed ?? 0) + (facets?.state?.overdue ?? 0)

  return (
    <div className="flex flex-col">
      {/*
        ⚠️ 工具条在 AsyncBoundary **外面**。
        放里面的话，筛出 0 条之后用来改条件的搜索框和状态下拉会一起消失 ——
        而那正是最需要它们的时刻。这一页尤其要紧：状态筛选是它的主要用法。
      */}
      <div className="flex flex-wrap items-center gap-2 border-b border-border px-4 py-3">
        <SearchInput
          value={keyword}
          onChange={(v) => patch({ q: v, page: 1 })}
          placeholder={t('tasks:filter.searchPlaceholder')}
          clearLabel={t('common:filter.clearSearch')}
          className="w-[240px]"
        />
        <Select<string>
          label={t('tasks:filter.state')}
          value={state}
          onChange={(v) => patch({ state: v, page: 1 })}
          options={[
            { value: 'all', label: t('common:filter.all'), count: facets?.state?.all },
            ...Object.keys(facets?.state ?? {})
              .filter((k) => k !== 'all' && k !== '')
              .sort()
              .map((v) => ({
                value: v,
                label: t(`tasks:state.${v}`, { defaultValue: v }),
                count: facets?.state?.[v],
              })),
          ]}
        />
        {/* 「N 个任务需要处理」做成可点：数字告诉你有事，点一下才告诉你是哪几条 */}
        {broken > 0 ? (
          <button
            type="button"
            onClick={() => patch({ state: state === 'failed' ? 'all' : 'failed', page: 1 })}
            className="cursor-pointer rounded-[var(--radius)] border border-danger bg-danger/10 px-2.5 py-1 text-xs text-danger transition-colors duration-150 hover:bg-danger/15"
          >
            {t('tasks:brokenNote', { count: broken })}
          </button>
        ) : null}
        {query.data ? (
          <span className="ml-auto text-xs text-muted-foreground">
            {t('tasks:total', { count: query.data.total })}
          </span>
        ) : null}
      </div>

      <AsyncBoundary
        state={fromQuery<TaskListResult>(query, (d) => d.total === 0, toLoadError)}
        errorTitle={t('tasks:error.title')}
        retryLabel={t('common:action.retry')}
        onRetry={() => void query.refetch()}
        pending={
          <div className="p-5">
            <TableSkeleton columns={[26, 13, 14, 14, 22, 11]} rows={6} />
          </div>
        }
        empty={
          <EmptyState
            icon={<Radar />}
            title={t('tasks:empty.title')}
            reason={filtered ? t('tasks:empty.filtered') : t('tasks:empty.noSource')}
            action={
              filtered
                ? {
                    label: t('common:filter.clearAll'),
                    onClick: () => patch({ q: '', state: 'all', page: 1 }),
                  }
                : null
            }
          />
        }
      >
        {(data) => (
          <>
            <DataTable data={data.items} columns={columns} rowKey={(x) => x.task_key} />
            <Pagination
              page={page}
              size={size}
              total={data.total}
              onPage={(p) => patch({ page: p })}
              onSize={(n) => patch({ size: n, page: 1 })}
              rangeLabel={(f, t2, tt) =>
                t('common:pagination.range', { from: f, to: t2, total: tt })
              }
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
