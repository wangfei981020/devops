import { toErrorInfo } from '@ops/api'
import { tError, useTranslation } from '@ops/i18n'
import {
  AsyncBoundary,
  Badge,
  type BadgeTone,
  Banner,
  EmptyState,
  type LoadError,
  Pagination,
  SearchInput,
  Select,
  TableSkeleton,
  fromQuery,
} from '@ops/ui'
import { getRouteApi, useNavigate } from '@tanstack/react-router'
import { type PlatformEvent, useEventCenter } from './queries.js'

const route = getRouteApi('/runtime/events')

/** 级别 → 色调。**不认识的级别按异常处理**，不按 info。 */
function levelTone(level: string): BadgeTone {
  if (level === 'critical') return 'bad'
  if (level === 'warning') return 'warn'
  if (level === 'info') return 'mute'
  return 'warn'
}

/**
 * 事件中心。
 *
 * 排障时的第一站：**最近平台上出了什么事**——证书要到期了、镜像被换了、
 * 采集挂了、集群报了 Warning，全在一条时间线上。
 *
 * ⚠️ 默认按严重度而不是时间排：人是来找"哪里不对劲"的，
 * 按时间排会把唯一那条 critical 埋在 80 条 info 中间（§2.7）。
 */
export function EventCenterPage() {
  const { t } = useTranslation()
  const search = route.useSearch()
  const navigate = useNavigate()
  const query = useEventCenter({ days: search.days, source: search.source, level: search.level })

  const setSearch = (patch: Record<string, string | number>) =>
    void navigate({ to: '/runtime/events', search: { ...search, ...patch } })

  const toLoadError = (e: unknown): LoadError => {
    const n = toErrorInfo(e)
    return { cause: tError(t, n.messageKey, n.params), detail: n.detail, retryable: n.retryable, kind: n.kind === 'license' ? 'license' : undefined, feature: n.params?.feature as string | undefined }
  }
  const filtered =
    search.source !== 'all' || search.level !== 'all' || search.q !== '' || !!search.upcoming

  return (
    <div className="flex h-full flex-col">
      <div className="flex flex-wrap items-center gap-2.5 border-b border-border px-4 py-2.5">
        <Select<string>
          label={t('eventcenter:filter.days')}
          value={String(search.days)}
          onChange={(v) => setSearch({ days: Number(v), page: 1 })}
          options={[7, 30, 90].map((d) => ({
            value: String(d),
            label: t('eventcenter:filter.lastDays', { days: d }),
          }))}
        />
        <Select<string>
          label={t('eventcenter:filter.source')}
          value={search.source}
          onChange={(v) => setSearch({ source: v, page: 1 })}
          options={[
            { value: 'all', label: t('common:filter.all') },
            // 值原样透传给后端，中文只在 label 上
            ...['expiry', 'change', 'sync', 'k8s', 'alert'].map((s) => ({
              value: s,
              label: t(`eventcenter:source.${s}`),
            })),
          ]}
        />
        <Select<string>
          label={t('eventcenter:filter.level')}
          value={search.level}
          onChange={(v) => setSearch({ level: v, page: 1 })}
          options={[
            { value: 'all', label: t('common:filter.all') },
            // by_level 是后端算好的**全量**分布。带上计数，下拉才回答得了
            // 「切过去还有多少」—— 那正是下拉里那个数字存在的意义。
            // ⚠️ 分布缺失时不显示计数，而不是显示 0：
            //	0 会被读成"确实一条都没有"，而真相是我们没算出来。
            ...['critical', 'warning', 'info'].map((l) => ({
              value: l,
              label: t(`eventcenter:level.${l}`),
              count: query.data?.by_level?.[l],
            })),
          ]}
        />
        <SearchInput
          value={search.q}
          onChange={(v) => setSearch({ q: v, page: 1 })}
          placeholder={t('eventcenter:filter.searchPlaceholder')}
          clearLabel={t('common:filter.clearSearch')}
          className="w-[240px]"
        />
        {/*
          ⚠️ 「还来得及处理的有 N 条」必须是个**入口**，不是一行字。
          这些是整页里唯一还能采取行动的事件，其余都已经发生了。
          做成可点的筛选，才能从"有 5 条"变成"是哪 5 条"。
        */}
        {(query.data?.upcoming ?? 0) > 0 ? (
          <button
            type="button"
            onClick={() => setSearch({ upcoming: search.upcoming ? '' : '1', page: 1 })}
            className={`cursor-pointer rounded-[var(--radius)] border px-2.5 py-1 text-xs transition-colors duration-150 ${
              search.upcoming
                ? 'border-brand bg-brand/10 text-brand'
                : 'border-border text-muted-foreground hover:bg-secondary'
            }`}
          >
            {search.upcoming
              ? t('eventcenter:upcomingOnlyOn', { count: query.data?.upcoming })
              : t('eventcenter:upcomingOnly', { count: query.data?.upcoming })}
          </button>
        ) : null}
      </div>

      {/* 截断和合并都要说出来：不说的话「309 条」会被当成"平台上就发生了这些事" */}
      {query.data?.truncated || (query.data?.merged_away ?? 0) > 0 ? (
        <div className="px-4 pt-3">
          <Banner tone={query.data?.truncated ? 'warn' : 'info'}>
            <span>
              {query.data?.truncated
                ? t('eventcenter:truncated', {
                    shown: query.data?.count,
                    total: query.data?.total,
                    limit: query.data?.limit,
                  })
                : t('eventcenter:merged', { count: query.data?.merged_away })}
            </span>
          </Banner>
        </div>
      ) : null}

      <AsyncBoundary
        state={fromQuery(query, (d) => d.events.length === 0, toLoadError)}
        errorTitle={t('eventcenter:error.title')}
        retryLabel={t('common:action.retry')}
        onRetry={() => void query.refetch()}
        pending={<TableSkeleton columns={[12, 10, 20, 40]} rows={10} />}
        empty={
          <EmptyState
            title={t('eventcenter:empty.title')}
            // ⚠️ 空在这一页是**好消息**，但也可能是筛太窄了。两种都说清楚
            reason={
              filtered
                ? t('eventcenter:empty.filtered')
                : t('eventcenter:empty.quiet', { days: search.days })
            }
            action={
              filtered
                ? {
                    label: t('common:filter.clearAll'),
                    onClick: () =>
                      setSearch({ source: 'all', level: 'all', q: '', upcoming: '', page: 1 }),
                  }
                : null
            }
          />
        }
      >
        {(d) => {
          const kw = search.q.trim().toLowerCase()
          const sorted = d.events
            .filter((e) => (search.upcoming ? e.upcoming : true))
            .filter((e) =>
              kw
                ? `${e.object} ${e.title} ${e.message}`.toLowerCase().includes(kw)
                : true,
            )
            .sort(bySeverityThenTime)
          const from = (search.page - 1) * search.size
          const page = sorted.slice(from, from + search.size)
          return (
            <>
              <div className="min-h-0 flex-1 overflow-auto">
                {page.map((e, i) => (
                  <EventRow key={`${e.time}-${e.object}-${i}`} event={e} t={t} />
                ))}
              </div>
              <Pagination
                page={search.page}
                size={search.size}
                total={sorted.length}
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
 * 严重度优先，同级按时间倒序。
 *
 * 不认识的级别排在 info 前面而不是后面——上游新增的级别多半是更严重的那种，
 * 排到最后等于把它藏起来。
 */
const RANK: Record<string, number> = { critical: 0, warning: 1, info: 3 }
function bySeverityThenTime(a: PlatformEvent, b: PlatformEvent) {
  const ra = RANK[a.level] ?? 2
  const rb = RANK[b.level] ?? 2
  if (ra !== rb) return ra - rb
  return a.time < b.time ? 1 : -1
}

function EventRow({
  event,
  t,
}: {
  event: PlatformEvent
  t: (k: string, p?: Record<string, unknown>) => string
}) {
  return (
    <div className="flex items-start gap-3 border-b border-border px-4 py-2.5 text-[13px] last:border-0">
      <span className="tabular w-[130px] shrink-0 text-xs text-muted-foreground">{event.time}</span>
      {/* 级别徽章挂上「为什么是这个级别」。红色只有在能解释自己的时候才有信号价值 */}
      <span title={event.level_why || undefined}>
        <Badge tone={levelTone(event.level)}>{t(`eventcenter:level.${event.level}`)}</Badge>
      </span>
      <Badge tone="mute">{t(`eventcenter:source.${event.source}`)}</Badge>
      {/* ⚠️ 集群要显示出来：309 条事件跨多个集群，不显示的话无从分辨（P1-36）。
          空是正常的 —— 域名到期、证书这类是平台级事件，不属于任何集群 */}
      <span className="w-[150px] shrink-0 truncate text-xs text-muted-foreground" title={event.cluster}>
        {event.cluster || ''}
      </span>
      <div className="min-w-0 flex-1">
        <div className="flex items-baseline gap-2">
          <span className="truncate font-medium text-foreground">{event.title}</span>
          {/* upcoming = 还没发生。不标出来的话，"证书已过期"和"15 天后到期"
              在时间线上长得一模一样，而两者的紧迫度差很远 */}
          {event.upcoming ? <Badge tone="info">{t('eventcenter:upcoming')}</Badge> : null}
          {event.count > 1 ? (
            <span className="tabular shrink-0 text-[11px] text-muted-foreground">
              ×{event.count}
            </span>
          ) : null}
        </div>
        <div className="truncate text-xs text-muted-foreground" title={event.message}>
          {event.message}
        </div>
      </div>
      {/* ⚠️ 对象列必须有 title。
          同一行的「消息」列有 tooltip、这一列没有 —— 那是遗漏不是取舍，
          而被切掉的恰恰是"哪个东西出的事"，悬停也救不回来（P1-34） */}
      <span
        className="w-[220px] shrink-0 truncate text-right font-mono text-[11px] text-muted-foreground"
        title={event.object}
      >
        {event.object}
      </span>
    </div>
  )
}
