import { useTranslation } from '@ops/i18n'
import { useQuery } from '@tanstack/react-query'
import {
  AsyncBoundary,
  Badge,
  type BadgeTone,
  Button,
  EmptyState,
  Skeleton,
  fromQuery,
} from '@ops/ui'
import { get, makeLoadError } from '../lib/api.js'

type Incident = {
  id: number
  title: string
  severity: string
  status: string
  count: number
  first_at: string
  last_at: string
  acked_by: string
  labels: Record<string, string>
}

type SelfCheck = {
  datasources: { total: number; down: number }
  rules: { total: number; failing: number }
  notify_failed_24h: number
  silent_sources: string[]
}

export function severityTone(s: string): BadgeTone {
  switch (s) {
    case 'critical':
      return 'bad'
    case 'warning':
      return 'warn'
    case 'info':
      return 'info'
    default:
      // 不认识的严重度按最高处理：当成 info 会把上游新增的级别静默降级
      return 'bad'
  }
}

/** 状态文案。认不出的状态原样显示——译不出来就露出原值，比显示"未知"更有助于排查。 */
export function statusKey(s: string): string {
  return { firing: 'firing', acked: 'acked', suppressed: 'suppressed', resolved: 'resolved' }[s] ?? ''
}

export function WarRoomPage({ onNavigate }: { onNavigate: (k: string) => void }) {
  const { t } = useTranslation()
  const query = useQuery({
    queryKey: ['incidents', 'active'],
    queryFn: () => get<{ items: Incident[] }>('/incidents?status=active'),
    refetchInterval: 30_000,
  })
  const check = useQuery({ queryKey: ['selfcheck'], queryFn: () => get<SelfCheck>('/selfcheck') })


  const items = query.data?.items ?? []
  const unacked = items.filter((i) => i.status === 'firing')
  const sc = check.data

  return (
    <div className="flex flex-col gap-3">
      <div className="grid grid-cols-2 gap-2 lg:grid-cols-4">
        <Kpi
          label={t('opsalert:warroom.firing')}
          value={unacked.length}
          tone={unacked.length > 0 ? 'bad' : 'ok'}
          foot={unacked.length > 0 ? t('opsalert:warroom.unackedHint') : t('opsalert:warroom.unackedNone')}
        />
        <Kpi
          label={t('opsalert:warroom.dataSources')}
          value={sc ? `${sc.datasources.total - sc.datasources.down}/${sc.datasources.total}` : '—'}
          tone={sc?.datasources.down ? 'bad' : 'ok'}
          foot={
            sc?.datasources.down
              ? t('opsalert:warroom.dsBad', { count: sc.datasources.down })
              : t('opsalert:warroom.dsOk')
          }
        />
        <Kpi
          label={t('opsalert:warroom.ruleHealth')}
          value={sc ? `${sc.rules.total - sc.rules.failing}/${sc.rules.total}` : '—'}
          tone={sc?.rules.failing ? 'warn' : 'ok'}
          foot={
            sc?.rules.failing
              ? t('opsalert:warroom.ruleHealthBad', { count: sc.rules.failing })
              : t('opsalert:warroom.ruleHealthOk')
          }
        />
        <Kpi
          label={t('opsalert:warroom.delivery')}
          value={sc?.notify_failed_24h ?? 0}
          tone={sc?.notify_failed_24h ? 'bad' : 'ok'}
          foot={t('opsalert:warroom.deliveryHint')}
        />
      </div>

      <section className="flex flex-col gap-2">
        <header className="flex items-center gap-2 px-1 py-1">
          <h2 className="text-sm font-semibold">{t('opsalert:warroom.activeTitle')}</h2>
          <span className="text-xs text-muted-foreground">{t('opsalert:warroom.activeHint')}</span>
          <Button size="sm" variant="ghost" className="ml-auto" onClick={() => onNavigate('incidents')}>
            {t('opsalert:warroom.allIncidents')}
          </Button>
        </header>
        <AsyncBoundary
          state={fromQuery(query, (d) => d.items.length === 0, makeLoadError(t))}
          pending={<div className="flex flex-col gap-2 p-4">{[0, 1, 2].map((i) => <Skeleton key={i} className="h-12 w-full" />)}</div>}
          empty={
            <EmptyState
              // ⚠️ 空态必须区分「确实没事」和「安静得可疑」。
              // 检测链路有缺口时同样是空列表，把两者显示成一样的
              // 「暂无告警」，正是本产品要根治的问题。
              title={
                sc && (sc.rules.failing || sc.datasources.down)
                  ? t('opsalert:warroom.emptyTitleRisk')
                  : t('opsalert:warroom.allClear')
              }
              reason={
                sc?.rules.failing || sc?.datasources.down
                  ? t('opsalert:warroom.emptyReasonRisk', {
                      count: (sc?.rules.failing ?? 0) + (sc?.datasources.down ?? 0),
                    })
                  : t('opsalert:warroom.allClearReason')
              }
              action={{ label: t('opsalert:warroom.goSelfcheck'), onClick: () => onNavigate('selfcheck') }}
            />
          }
          errorTitle={t('opsalert:warroom.loadError')}
          retryLabel={t('action.retry')}
          onRetry={() => query.refetch()}
        >
          {(data) => (
            // 卡片流而不是紧凑表格：每条事件是一个要做决定的单元，
            // 挤在一起时人会漏掉中间几行——而值班漏看一行的代价是很大的。
            <ul className="flex flex-col gap-2">
              {data.items.map((inc) => (
                <li
                  key={inc.id}
                  className="flex gap-3 rounded-[var(--radius-lg)] bg-surface p-4 transition-colors hover:bg-secondary"
                >
                  <span
                    className={`w-1 shrink-0 rounded-full ${
                      inc.severity === 'critical' ? 'bg-danger' : 'bg-warning'
                    }`}
                    aria-hidden="true"
                  />
                  <div className="min-w-0 flex-1">
                    <div className="truncate text-[15px] font-medium">{inc.title}</div>
                    <div className="mt-1 flex flex-wrap items-center gap-2 text-xs text-muted-foreground">
                      <Badge tone={severityTone(inc.severity)}>
                        {t(`opsalert:severity.${inc.severity}`, inc.severity)}
                      </Badge>
                      <Badge tone={inc.status === 'firing' ? 'bad' : 'info'} dot={false}>
                        {t(`opsalert:status.${statusKey(inc.status)}`, inc.status)}
                      </Badge>
                      {Object.entries(inc.labels ?? {})
                        .filter(([k]) => k !== 'rule' && k !== 'severity')
                        .slice(0, 3)
                        .map(([k, v]) => (
                          <span key={k} className="rounded border border-border px-1.5 py-0.5 font-mono">
                            {k}={v}
                          </span>
                        ))}
                      <span>{t('opsalert:warroom.hits', { count: inc.count })}</span>
                      <span>
                        {t('opsalert:warroom.firstAt', {
                          time: new Date(inc.first_at).toLocaleTimeString(),
                        })}
                      </span>
                      {inc.acked_by && <span>{t('opsalert:warroom.handling', { who: inc.acked_by })}</span>}
                    </div>
                  </div>
                </li>
              ))}
            </ul>
          )}
        </AsyncBoundary>
      </section>

      {sc && sc.silent_sources.length > 0 && (
        <section className="rounded-[var(--radius-lg)] border-l-4 border-warning bg-warning-bg p-4">
          <h3 className="text-sm font-semibold">{t('opsalert:warroom.silentTitle')}</h3>
          <p className="mt-1 text-xs text-muted-foreground">
            {t('opsalert:warroom.silentBody', { names: sc.silent_sources.join('、') })}
          </p>
        </section>
      )}
    </div>
  )
}

/**
 * 值班台的 KPI。
 *
 * 刻意不做成 CMDB 那种等宽细边框卡片：值班时人是"扫"这一行，不是"读"。
 * 左侧 3px 状态色条 + 大号数字，一米开外也能看出哪个格子不对劲；
 * 数字用 tabular-nums，刷新时不会左右跳。
 */
function Kpi({
  label,
  value,
  foot,
  tone = 'ok',
}: {
  label: string
  value: number | string
  foot?: string
  tone?: 'ok' | 'warn' | 'bad'
}) {
  const bar = tone === 'bad' ? 'bg-danger' : tone === 'warn' ? 'bg-warning' : 'bg-success'
  const num = tone === 'bad' ? 'text-danger' : tone === 'warn' ? 'text-warning' : 'text-foreground'
  return (
    // 紧凑统计条：告警界面要在一屏里放下尽量多的信息，
    // 宽松卡片会把「有没有事」推到需要滚动才看得全
    <div className="relative flex gap-3 overflow-hidden rounded-[var(--radius)] border border-border bg-card p-3">
      <span className={`absolute inset-y-0 left-0 w-[2px] ${bar}`} aria-hidden="true" />
      <div className="min-w-0 pl-2">
        <div className="text-2xs tracking-wide text-muted-foreground uppercase">{label}</div>
        <div className={`mt-1 text-2xl leading-none font-semibold tabular-nums ${num}`}>{value}</div>
        {foot ? <div className="mt-2 truncate text-xs text-muted-foreground">{foot}</div> : null}
      </div>
    </div>
  )
}
