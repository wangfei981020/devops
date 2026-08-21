import { toErrorInfo } from '@ops/api'
import {
  formatDuration,
  formatNumber,
  formatRelativeTime,
  tError,
  type Locale,
  useTranslation,
} from '@ops/i18n'
import { AsyncBoundary, Badge, EmptyState, type LoadError, Skeleton, fromQuery } from '@ops/ui'
import { CheckCircle2 } from 'lucide-react'
import { type Situation, useDashboardCounts, useSituation } from './queries.js'

type TFn = (key: string, opts?: Record<string, unknown>) => string

export function OverviewPage() {
  const { t, i18n } = useTranslation()
  const locale = i18n.language as Locale
  const query = useSituation()
  const toLoadError = (e: unknown): LoadError => {
    const n = toErrorInfo(e)
    return { cause: tError(t, n.messageKey, n.params), detail: n.detail, retryable: n.retryable, kind: n.kind === 'license' ? 'license' : undefined, feature: n.params?.feature as string | undefined }
  }

  return (
    <div className="mx-auto max-w-[1000px] p-5">
      <AsyncBoundary
        state={fromQuery<Situation>(query, () => false, toLoadError)}
        errorTitle={t('overview:error.title')}
        retryLabel={t('common:action.retry')}
        onRetry={() => void query.refetch()}
        pending={
          <div className="flex flex-col gap-3">
            {[60, 90, 80, 70].map((w) => (
              <Skeleton key={w} className="h-5" style={{ width: `${w}%` }} />
            ))}
          </div>
        }
        empty={null}
      >
        {(s) => <Body s={s} t={t} locale={locale} />}
      </AsyncBoundary>
    </div>
  )
}

function Body({ s, t, locale }: { s: Situation; t: TFn; locale: Locale }) {
  const unknown = s.attention.filter((a) => a.count === null).length

  return (
    <div className="flex flex-col gap-5">
      <section className="rounded-[var(--radius-lg)] border border-border bg-card p-4">
        <div className="flex items-baseline gap-2.5">
          <h2 className="text-sm font-semibold text-foreground">{t('overview:attention')}</h2>
          {/* 数据有多旧要摆在最显眼处：整页数字看起来都像"此刻"，而它们是快照 */}
          <span className="ml-auto text-xs text-muted-foreground">
            {t('overview:generatedAt', {
              time: formatRelativeTime(s.generatedAt, locale),
            })}
          </span>
        </div>

        {s.attention.length === 0 ? (
          <div className="py-6">
            <EmptyState
              icon={<CheckCircle2 />}
              title={t('overview:allClear.title')}
              // ⚠️ 明说"这是在已接入的数据源范围内"。不说的话，
              // 一个采集全断的环境也会显示"一切正常"
              reason={t('overview:allClear.reason')}
              action={null}
            />
          </div>
        ) : (
          <div className="mt-3 flex flex-col">
            {s.attention.map((a) => (
              <a
                key={a.key}
                href={a.link}
                className="flex items-center gap-3 border-b border-border py-2 text-[13px] last:border-b-0 hover:bg-secondary"
              >
                {a.count === null ? (
                  // ⚠️ 统计失败**不能**显示成 0。它排在最前，并明说是"没统计出来"——
                  // 在首页上给一个没看过的维度发合格证，是这一页最坏的失效方式
                  <Badge tone="warn">{t('overview:notCounted')}</Badge>
                ) : (
                  <span
                    className={`tabular w-10 text-right font-semibold ${
                      a.severity === 'high' ? 'text-danger' : 'text-warning'
                    }`}
                  >
                    {formatNumber(a.count, locale)}
                  </span>
                )}
                <span className="min-w-0 flex-1 text-foreground">
                  {t(`overview:item.${a.key}`)}
                </span>
                <span className="text-xs text-muted-foreground">{t('overview:view')}</span>
              </a>
            ))}
          </div>
        )}

        {unknown > 0 ? (
          <p className="mt-2.5 text-xs text-warning">
            {t('overview:notCountedNote', { count: unknown })}
          </p>
        ) : null}
      </section>

      <section className="rounded-[var(--radius-lg)] border border-border bg-card p-4">
        <h2 className="text-sm font-semibold text-foreground">{t('overview:inventory')}</h2>
        <div className="mt-3 grid grid-cols-2 gap-x-6 gap-y-2 sm:grid-cols-3">
          {['hosts', 'clusters', 'nodes', 'pods', 'domains', 'certs'].map((k) => {
            const v = s.inventory[k]
            return (
              <div key={k} className="flex items-baseline gap-2">
                <span className="tabular text-lg font-semibold text-foreground">
                  {/* 同样：没统计出来显示 — 而不是 0 */}
                  {v === null || v === undefined ? '—' : formatNumber(v, locale)}
                </span>
                <span className="text-xs text-muted-foreground">{t(`overview:inv.${k}`)}</span>
              </div>
            )
          })}
        </div>
        {/*
          ⚠️ 「证书 0」是真的，但它只统计**我方签发**的那一类。
          线上实际在跑的证书有几百张（从域名/CDN 侧探测出来的），在另一个入口里。
          不写这一句的话，看首页的人会得出"证书没纳管"的结论然后跑去别处查 ——
          而证书恰恰是到期就出事故的东西，首页显示 0 等于把这块监督整个关掉（P0-4）。
        */}
        <p className="mt-2.5 text-xs leading-relaxed text-muted-foreground">
          {t('overview:certScopeNote')}
        </p>
        {/*
          🔴 把上面那句提示变成**真实数字**。

          它原来只是「线上实际在跑的证书有几百张，在另一个入口里」——
          指向别处，但没说那边到底有没有事。而证书是到期就出事故的东西：
          「有几百张」和「其中 3 张 30 天内到期」是完全不同的两件事，
          后者今天就得动手。

          后端 /api/dashboard 一直算着这两个数，只是从没有页面读过它
          （OPSCMDB-023 第二档）。
        */}
        <OnlineCertCounts />
      </section>

      <section className="rounded-[var(--radius-lg)] border border-border bg-card p-4">
        <h2 className="text-sm font-semibold text-foreground">{t('overview:freshness')}</h2>
        <p className="mt-1 text-xs text-muted-foreground">{t('overview:freshnessHint')}</p>
        <div className="mt-2.5 flex flex-col gap-1.5">
          {/*
            ⚠️ 「证书」这一行原来没有，而它是最该有的一行。
            家底里「证书 0」看着像个确定的事实，没人会去质疑；
            而这里一行「证书 从未采集」能让人立刻知道那个 0 不可信 ——
            这正是 P0-4 只能靠 MCP 交叉比对才发现的原因（P1-6）。
          */}
          {['hosts', 'k8s', 'domains', 'certs'].map((k) => {
            const at = s.freshness[k]
            const stale = at ? isStale(at, STALE_AFTER_H[k] ?? 6) : false
            return (
              <div key={k} className="flex items-baseline gap-3 text-[13px]">
                <span className="w-[88px] shrink-0 text-muted-foreground">
                  {t(`overview:source.${k}`)}
                </span>
                {at ? (
                  <>
                    {/*
                      ⚠️ 超过阈值要有视觉区分。
                      实测云主机是「8 小时前」而阈值是 6 小时 —— 已经超了，
                      但它和「集群资源 54 秒前」是同一个灰色，看不出区别（P1-7）。
                      「数据可能已经不准」是个需要行动的信号，不能和正常态同色。
                    */}
                    <span className={stale ? 'text-warning' : 'text-foreground'}>
                      {formatRelativeTime(at, locale)}
                    </span>
                    {stale ? (
                      // ⚠️ 提示里要给**实际**超了多久，不能只给阈值。
                      //
                      //	我第一版写的是「已超过 6 小时未更新」，而那一行的时间是「上周」——
                      //	读起来像"刚超一点点"，实际差了一整周。
                      //	一个把严重程度说小的提示，比不提示更容易让人放过它。
                      <span className="text-[11px] text-warning">
                        {t('overview:staleHint', {
                          actual: staleFor(at, locale),
                          hours: STALE_AFTER_H[k] ?? 6,
                        })}
                      </span>
                    ) : null}
                  </>
                ) : (
                  // 缺 key = 这类数据没接入。显示一个假的时间会让人以为在采
                  <span className="text-warning">{t('common:state.notIngested')}</span>
                )}
              </div>
            )
          })}
        </div>
      </section>
    </div>
  )
}

/**
 * 各类数据「多久没更新就算旧」的阈值（小时）。
 *
 * ⚠️ 按采集频率给，不要统一成一个数：
 * 集群资源两分钟一轮，超过 1 小时就明显不对；
 * 主机同步是小时级的，6 小时才算旧；
 * 证书 443 探测是每天一次，48 小时内都正常。
 * 统一阈值会让高频的那类漏报、低频的那类天天误报。
 */
const STALE_AFTER_H: Record<string, number> = {
  hosts: 6,
  k8s: 1,
  domains: 24,
  certs: 48,
}

function isStale(at: string, hours: number) {
  const ms = new Date(at).getTime()
  if (Number.isNaN(ms)) return false // 解析不出来别当成"旧"，那是另一个问题
  return Date.now() - ms > hours * 3600_000
}

/**
 * 实际有多久没更新了。
 *
 * 🔴 单位由 formatDuration 按 locale 出，**不要自己拼** ——
 *	原来写的是 `${h} 小时`，EN 模式下渲染成
 *	`11 小时 without an update`，同一句话里中英混排（OPSCMDB-062）。
 *
 * 🔴 取整也交给它：它和上一格的 formatRelativeTime 共用同一套取整，
 *	所以「12 hours ago」和「12 hours without an update」必然是同一个数。
 *	原来这里 floor、那里 round，同一行显示出两个差 1 小时的数字。
 */
function staleFor(at: string, locale: Locale) {
  const ms = Date.now() - new Date(at).getTime()
  if (!Number.isFinite(ms) || ms < 0) return '?'
  return formatDuration(ms, locale)
}

/**
 * 线上实际在用的证书到期计数。
 *
 * ⚠️ 三态：取不到就什么都不显示，**不要显示 0** ——
 * 「查不到」和「没有到期的」在这里是相反的结论，而后者会让人放心地不去查。
 */
function OnlineCertCounts() {
  const { t } = useTranslation()
  const q = useDashboardCounts()
  if (q.isPending || q.isError || !q.data) return null
  const d = q.data
  const expired = d.online_cert_expired ?? 0
  const expiring = d.online_cert_expiring ?? 0
  const unprobed = d.online_cert_unprobed ?? 0
  if (expired === 0 && expiring === 0) {
    // 🔴 「都没有」这句话在**有大量未探测**时不成立。
    //
    //	两个计数的 SQL 都带 cert_expiry_at IS NOT NULL —— 没探过的被排除在外，
    //	所以 828 条从没探测的情况下它们都是 0，而真相是"我们不知道"。
    //	生产上这句绿字与「两张 44 小时后到期的生产网关证书」同时存在
    //	（OPSCMDB-063 / PROD-CERT-20260819）。
    //
    // ⚠️ 用警告色而不是绿色：它不是"没问题"，是"这个结论不完整"。
    if (unprobed > 0) {
      return (
        <p className="mt-1 text-xs text-warning">
          {t('overview:onlineCertUnprobed', { n: unprobed })}
        </p>
      )
    }
    // 明确说"都没有"，而不是留白 —— 留白会被读成"这一项没查"
    return (
      <p className="mt-1 text-xs text-success">{t('overview:onlineCertOk')}</p>
    )
  }
  return (
    <p className="mt-1 text-xs">
      {expired > 0 ? (
        <span className="text-danger">{t('overview:onlineCertExpired', { n: expired })}</span>
      ) : null}
      {expired > 0 && expiring > 0 ? <span className="mx-1 text-muted-foreground">·</span> : null}
      {expiring > 0 ? (
        <span className="text-warning">{t('overview:onlineCertExpiring', { n: expiring })}</span>
      ) : null}
      {/* 🔴 有命中时**也要**说分母。
          「1 个已过期」和「另有 828 个不知道」是两件独立的事 ——
          只说前者，人会以为已经把范围看全了，然后去处理那 1 个就收工。
          （第一版只在 expired/expiring 都为 0 时才说，漏了这一支。） */}
      {unprobed > 0 ? (
        <>
          <span className="mx-1 text-muted-foreground">·</span>
          <span className="text-warning">{t('overview:onlineCertUnprobedShort', { n: unprobed })}</span>
        </>
      ) : null}
    </p>
  )
}
