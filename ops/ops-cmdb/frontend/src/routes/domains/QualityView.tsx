import { toErrorInfo } from '@ops/api'
import { formatDateTime, tError, type Locale, useTranslation } from '@ops/i18n'
import { shouldRetry } from '@ops/api'
import { AsyncBoundary, Badge, Banner, type LoadError, NotIngested, fromQuery } from '@ops/ui'
import { useQuery } from '@tanstack/react-query'
import { apiGet } from '../../lib/fetchJson.js'

/**
 * 域名访问质量：台账 vs **客户端实际拿到的东西**（OPSCMDB-035）。
 *
 * # 这一页存在的理由
 *
 * Grafana 看得到客户端实际拿到的证书，看不到我们登记的是什么；
 * CMDB 看得到台账，看不到客户端到底拿到了什么。
 * **两边对不上 = 出事了**，而且是最难查的那种（证书换了但没生效 /
 * 改在了 NS 没指向的那一方）。
 *
 * 🔴 **刻意不做监控墙**：判据会打架（"现在这一秒"vs"这份台账多旧"），
 * 而且夜莺 + OpsAlert 已经在做告警了，再做一个就是第三份真相。
 */
interface ProbeTarget {
  instance: string
  host: string
  root_domain?: string
  /** ⚠️ 缺失 = 这个目标没有 TLS 拨测（http:// 的），不是"证书没到期" */
  cert_expiry_at?: string
  cert_days_left?: number
  /** ⚠️ 缺失 = 没采到，与 false（探测失败）不同 */
  probe_ok?: boolean
}

interface QualityResult {
  ok?: boolean
  configured?: boolean
  hint?: string
  empty_hint?: string
  error?: string
  summary?: {
    probed?: number
    matched?: number
    unledgered?: number
    unprobed?: number
    cert_expiring_30d?: number
    probe_failing?: number
  }
  matched?: ProbeTarget[]
  unledgered?: ProbeTarget[]
  unprobed?: string[]
}

function useQuality() {
  return useQuery({
    queryKey: ['domain-quality'],
    queryFn: () => apiGet<QualityResult>('/api/domains/quality'),
    staleTime: 60_000,
    retry: shouldRetry,
  })
}

export function QualityView({ locale }: { locale: Locale }) {
  const { t } = useTranslation()
  const query = useQuality()
  const toLoadError = (e: unknown): LoadError => {
    const n = toErrorInfo(e)
    return { cause: tError(t, n.messageKey, n.params), detail: n.detail, retryable: n.retryable, kind: n.kind === 'license' ? 'license' : undefined, feature: n.params?.feature as string | undefined }
  }

  return (
    <AsyncBoundary
      state={fromQuery<QualityResult>(query, () => false, toLoadError)}
      errorTitle={t('domains:quality.errorTitle')}
      retryLabel={t('common:action.retry')}
      onRetry={() => void query.refetch()}
      pending={<div className="p-5 text-xs text-muted-foreground">{t('common:state.loading')}</div>}
      empty={null}
    >
      {(d) => {
        // ⚠️ 「没接数据源」和「没有拨测数据」是两件事，下一步完全不同：
        //	前者去配数据源，后者说明 blackbox 没在采。
        //	混成一句"暂无数据"两件事都没法处置。
        if (d.configured === false) {
          return <NotIngested title={t('domains:quality.noSource')} reason={d.hint ?? ''} />
        }
        const s = d.summary ?? {}
        return (
          <div className="flex flex-col gap-3 p-4">
            {d.empty_hint ? (
              <Banner tone="warn">
                <span>{d.empty_hint}</span>
              </Banner>
            ) : null}

            <div className="flex flex-wrap gap-4 text-xs">
              <Stat label={t('domains:quality.probed')} v={s.probed} />
              <Stat label={t('domains:quality.matched')} v={s.matched} />
              {/* 🔴 这两个是这一页的重点，不是普通计数 */}
              <Stat label={t('domains:quality.unledgered')} v={s.unledgered} tone="warn" />
              <Stat label={t('domains:quality.unprobed')} v={s.unprobed} tone="warn" />
              <Stat label={t('domains:quality.expiring')} v={s.cert_expiring_30d} tone="bad" />
              <Stat label={t('domains:quality.failing')} v={s.probe_failing} tone="bad" />
            </div>

            <p className="text-[11px] leading-relaxed text-muted-foreground">
              {t('domains:quality.explain')}
            </p>

            {(d.matched?.length ?? 0) > 0 ? (
              <section>
                <h3 className="mb-1 text-[11px] font-medium tracking-wide text-muted-foreground uppercase">
                  {t('domains:quality.matchedTitle')}
                </h3>
                <div className="flex flex-col divide-y divide-border rounded-[var(--radius)] border border-border">
                  {d.matched?.slice(0, 60).map((p) => (
                    <div key={p.host} className="flex flex-wrap items-center gap-3 px-3 py-1.5 text-xs">
                      <span className="min-w-[260px] truncate font-mono" title={p.instance}>
                        {p.host}
                      </span>
                      <span className="min-w-[160px] text-muted-foreground">{p.root_domain}</span>
                      {/* 客户端**实际拿到**的证书到期日。台账里没有这个信息 */}
                      {p.cert_days_left !== undefined ? (
                        <span
                          className={
                            p.cert_days_left <= 14
                              ? 'text-danger'
                              : p.cert_days_left <= 30
                                ? 'text-warning'
                                : 'text-muted-foreground'
                          }
                          title={p.cert_expiry_at ? formatDateTime(p.cert_expiry_at, locale) : ''}
                        >
                          {t('domains:quality.certDays', { n: p.cert_days_left })}
                        </span>
                      ) : (
                        // 没有 TLS 拨测 ≠ 证书没问题。说清楚是"没测"
                        <span className="text-muted-foreground">{t('domains:quality.noTls')}</span>
                      )}
                      {p.probe_ok === false ? (
                        <Badge tone="bad">{t('domains:quality.probeFailed')}</Badge>
                      ) : null}
                    </div>
                  ))}
                </div>
                {(d.matched?.length ?? 0) > 60 ? (
                  // 静默截断会让人以为"就这些"。说出来
                  <p className="mt-1 text-[11px] text-muted-foreground">
                    {t('domains:quality.truncated', { shown: 60, total: d.matched?.length })}
                  </p>
                ) : null}
              </section>
            ) : null}

            {(d.unledgered?.length ?? 0) > 0 ? (
              <section>
                <h3 className="mb-1 text-[11px] font-medium tracking-wide text-warning uppercase">
                  {t('domains:quality.unledgeredTitle')}
                </h3>
                <p className="mb-1 text-[11px] text-muted-foreground">
                  {t('domains:quality.unledgeredHint')}
                </p>
                <div className="flex flex-wrap gap-1">
                  {d.unledgered?.map((p) => (
                    <span key={p.host} className="rounded border border-warning/40 px-1.5 py-0.5 font-mono text-[11px]">
                      {p.host}
                    </span>
                  ))}
                </div>
              </section>
            ) : null}

            {(d.unprobed?.length ?? 0) > 0 ? (
              <section>
                <h3 className="mb-1 text-[11px] font-medium tracking-wide text-warning uppercase">
                  {t('domains:quality.unprobedTitle')}
                </h3>
                <p className="mb-1 text-[11px] text-muted-foreground">
                  {t('domains:quality.unprobedHint')}
                </p>
                <div className="flex flex-wrap gap-1">
                  {d.unprobed?.map((n) => (
                    <span key={n} className="rounded border border-border px-1.5 py-0.5 font-mono text-[11px]">
                      {n}
                    </span>
                  ))}
                </div>
              </section>
            ) : null}
          </div>
        )
      }}
    </AsyncBoundary>
  )
}

function Stat({ label, v, tone }: { label: string; v?: number; tone?: 'warn' | 'bad' }) {
  const { t } = useTranslation()
  return (
    <span className="flex flex-col">
      {/* ⚠️ undefined = 没算出来，与 0 不同。显示 0 会被当成"确认没有" */}
      <span
        className={`tabular text-[15px] ${
          v === undefined ? 'text-muted-foreground' : v > 0 && tone === 'bad' ? 'text-danger' : v > 0 && tone === 'warn' ? 'text-warning' : ''
        }`}
      >
        {v === undefined ? t('common:state.unknown') : v}
      </span>
      <span className="text-[11px] text-muted-foreground">{label}</span>
    </span>
  )
}
