import { useTranslation } from '@ops/i18n'
import { AsyncBoundary, Skeleton, fromQuery } from '@ops/ui'
import { useQuery } from '@tanstack/react-query'
import { get, makeLoadError } from '../lib/api.js'

/**
 * 值班简报。
 *
 * # 和 OpsPlane 那套刻意不同的地方
 *
 * CMDB 的界面是「查东西」用的：卡片网格 + 数据表格，你知道要找什么，界面帮你找到。
 * 值班时的第一眼要回答的却是另一个问题 ——「现在到底有没有事」。
 * 那是**一句结论**，不是一张表。
 *
 * 所以这一页按编辑排版组织：
 *   1. 一句大字结论（衬线，字号大到不用读第二眼）
 *   2. 一条时间轴（告警本来就是时间序列；表格会把"先后"压成一个可排序字段）
 *   3. 一段收尾：检测链路本身好不好
 *
 * 没有卡片、没有徽章堆、没有等宽栅格 —— 这些正是让所有后台长得一样的东西。
 *
 * # 不变的部分
 *
 * 技术纪律与其它页面完全一致：四态、i18n、颜色走 token、权限判据在后端。
 * 视觉可以分家，"失败要看起来像失败"这类约定不能分家。
 */

interface Incident {
  id: number
  title: string
  severity: string
  status: string
  count: number
  first_at: string
  labels?: Record<string, string> | null
}

interface SelfCheck {
  datasources: { total: number; down: number }
  rules: { total: number; failing: number }
  notify_failed_24h: number
  silent_sources: string[]
}

export function BriefPage({ onNavigate }: { onNavigate: (k: string) => void }) {
  const { t } = useTranslation()
  const incidents = useQuery({
    queryKey: ['incidents'],
    queryFn: () => get<{ items: Incident[] }>('/incidents'),
  })
  const check = useQuery({ queryKey: ['selfcheck'], queryFn: () => get<SelfCheck>('/selfcheck') })

  const sc = check.data
  const items = incidents.data?.items ?? []
  const firing = items.filter((i) => i.status === 'firing')
  // ⚠️ 链路有缺口时，「没有告警」不能说成「没事」。
  // 这条判断和值班台是同一套 —— 视觉分家，判据不分家。
  const gaps = (sc?.rules.failing ?? 0) + (sc?.datasources.down ?? 0) + (sc?.silent_sources.length ?? 0)

  return (
    <AsyncBoundary
      state={fromQuery(incidents, () => false, makeLoadError(t))}
      pending={<Skeleton className="h-72 w-full" />}
      empty={null}
      errorTitle={t('opsalert:brief.loadError')}
      retryLabel={t('action.retry')}
      onRetry={() => incidents.refetch()}
    >
      {() => (
        <article className="brief mx-auto flex max-w-3xl flex-col gap-10 py-6">
          {/* ── 结论 ────────────────────────────────────────────── */}
          <header className="flex flex-col gap-3">
            <time className="font-mono text-2xs tracking-widest text-muted-foreground uppercase">
              {t('opsalert:brief.asOf', { time: new Date().toLocaleString() })}
            </time>
            <h1 className="brief-verdict">
              {firing.length === 0 && gaps === 0
                ? t('opsalert:brief.verdictClear')
                : firing.length === 0
                  ? t('opsalert:brief.verdictQuietButGaps')
                  : t('opsalert:brief.verdictFiring', { count: firing.length })}
            </h1>
            <p className="max-w-prose text-sm leading-relaxed text-muted-foreground">
              {sc
                ? t('opsalert:brief.subtitle', {
                    rules: sc.rules.total - sc.rules.failing,
                    total: sc.rules.total,
                    sources: sc.datasources.total - sc.datasources.down,
                    allSources: sc.datasources.total,
                  })
                : t('state.loading')}
            </p>
          </header>

          {/* ── 时间轴 ──────────────────────────────────────────── */}
          {items.length > 0 ? (
            <section className="flex flex-col gap-4">
              <h2 className="text-2xs font-medium tracking-widest text-muted-foreground uppercase">
                {t('opsalert:brief.timeline')}
              </h2>
              <div className="brief-spine flex flex-col gap-6">
                {items.slice(0, 8).map((inc) => (
                  <div
                    key={inc.id}
                    className="brief-node relative flex flex-col gap-1"
                    data-tone={inc.severity === 'critical' ? 'danger' : 'warning'}
                  >
                    <div className="flex items-baseline gap-3">
                      <time className="font-mono text-xs text-muted-foreground">
                        {new Date(inc.first_at).toLocaleTimeString()}
                      </time>
                      <h3 className="text-[15px] leading-snug font-medium">{inc.title}</h3>
                    </div>
                    <p className="font-mono text-xs text-muted-foreground">
                      {[inc.labels?.namespace, inc.labels?.container].filter(Boolean).join(' / ') ||
                        t('state.noValue')}
                      {' · '}
                      {t('opsalert:brief.hits', { count: inc.count })}
                    </p>
                  </div>
                ))}
              </div>
            </section>
          ) : (
            <section className="flex flex-col gap-2">
              <hr className="brief-rule" />
              <p className="max-w-prose py-2 text-sm leading-relaxed text-muted-foreground">
                {gaps === 0
                  ? t('opsalert:brief.noneReason')
                  : t('opsalert:brief.noneButGaps', { count: gaps })}
              </p>
            </section>
          )}

          {/* ── 检测链路本身 ────────────────────────────────────── */}
          <section className="flex flex-col gap-4">
            <hr className="brief-rule" />
            <h2 className="text-2xs font-medium tracking-widest text-muted-foreground uppercase">
              {t('opsalert:brief.chain')}
            </h2>
            {/* 三个数字并排，但用大衬线字而不是 KPI 卡：
                它们是同一句话的三个部分，不是三个独立的指标块 */}
            <div className="flex flex-wrap gap-x-12 gap-y-6">
              <Figure
                value={sc ? `${sc.datasources.total - sc.datasources.down}/${sc.datasources.total}` : '—'}
                label={t('opsalert:brief.sources')}
                bad={!!sc?.datasources.down}
                badText={t('opsalert:brief.sourcesDown', { count: sc?.datasources.down ?? 0 })}
              />
              <Figure
                value={sc ? `${sc.rules.total - sc.rules.failing}/${sc.rules.total}` : '—'}
                label={t('opsalert:brief.rules')}
                bad={!!sc?.rules.failing}
                badText={t('opsalert:brief.rulesFailing', { count: sc?.rules.failing ?? 0 })}
              />
              <Figure
                value={String(sc?.notify_failed_24h ?? 0)}
                label={t('opsalert:brief.delivery')}
                bad={!!sc?.notify_failed_24h}
                badText={t('opsalert:brief.deliveryFailed')}
              />
            </div>
            <button
              type="button"
              className="cursor-pointer self-start text-xs text-primary underline-offset-4 hover:underline"
              onClick={() => onNavigate('selfcheck')}
            >
              {t('opsalert:brief.toSelfcheck')}
            </button>
          </section>
        </article>
      )}
    </AsyncBoundary>
  )
}

/**
 * 一个数字 + 一行说明。
 *
 * ⚠️ 出问题时**多一行说明**而不是换个颜色了事：
 * 「3/4」配红色只说明有问题，不说明是哪一个、要不要现在管。
 */
function Figure({
  value,
  label,
  bad,
  badText,
}: {
  value: string
  label: string
  bad: boolean
  badText: string
}) {
  return (
    <div className="flex flex-col gap-1">
      <span className={`brief-figure ${bad ? 'text-danger' : ''}`}>{value}</span>
      <span className="text-xs text-muted-foreground">{label}</span>
      {bad ? <span className="text-xs text-danger">{badText}</span> : null}
    </div>
  )
}
