import { useTranslation } from '@ops/i18n'
import { AsyncBoundary, Skeleton, fromQuery } from '@ops/ui'
import { useQuery } from '@tanstack/react-query'
import { ShieldAlert, ShieldCheck } from 'lucide-react'
import { useMemo } from 'react'
import { api } from '../../api/client.js'
import { makeToLoadError } from '../../shared/errorText.js'

interface Overview {
  apps: number | null
  rules: number | null
  users: number | null
  sessions: number | null
  denied_24h: number | null
  apps_without_rules: number | null
  probes_failing: number | null
  probes_never_run: number | null
  audit_chain: { ok: boolean; checked: number; broken_at_seq?: number; reason?: string } | null
  degraded: string[]
}

/**
 * 总览。
 *
 * 只放**需要有人动手**的东西。一屏漂亮的数字没有用 ——
 * 看的人要能立刻答出「现在有没有问题、下一步做什么」，
 * 所以异常项排在规模项前面，且异常为 0 时才是灰的。
 */
export function OverviewPage({ onGo }: { onGo: (key: string) => void }) {
  const { t } = useTranslation()
  const toLoadError = useMemo(() => makeToLoadError(t), [t])
  const q = useQuery({ queryKey: ['overview'], queryFn: () => api.get<Overview>('/overview') })
  const state = fromQuery<Overview>(q, () => false, toLoadError)

  return (
    <>
      <h1 className="text-xl font-semibold tracking-tight">{t('sso:console.overview')}</h1>
      <p className="mt-1 mb-5 max-w-[80ch] text-sm text-muted-foreground">
        {t('sso:overview.intro')}
      </p>

      <AsyncBoundary
        state={state}
        errorTitle={t('sso:overview.errorTitle')}
        retryLabel={t('retry')}
        onRetry={() => void q.refetch()}
        // 总览没有空态：一个全新环境的正确答案是"各项都是 0"，
        // 而不是一张空页 —— 0 本身就是有用的信息（还没接任何应用）。
        empty={null}
        pending={
          <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-4">
            {[0, 1, 2, 3, 4, 5, 6, 7].map((i) => (
              <Skeleton key={i} className="h-20 w-full" />
            ))}
          </div>
        }
      >
        {(d) => (
          <>
            {/* ?? [] 不是多余的：Go 的 nil 切片会序列化成 null，
                直接 .length 就是整页白屏。后端已经修成永远返回数组，
                但前端不该因为后端某天回退成 null 就整页挂掉。 */}
            {(d.degraded ?? []).length > 0 ? (
              // ⚠️ 降级项必须说出来。少一项而不说，看的人会把 null 当成 0，
              // 也就是把「不知道」当成「没问题」。
              <p className="mb-4 rounded-[var(--radius)] border border-warning bg-warning-bg px-3 py-2 text-[12px]">
                {t('sso:overview.degraded', { items: (d.degraded ?? []).join('、') })}
              </p>
            ) : null}

            <ChainCard chain={d.audit_chain} onGo={onGo} />

            <h2 className="mt-6 mb-2 text-[15px] font-semibold">{t('sso:overview.attention')}</h2>
            <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-4">
              <Stat
                label={t('sso:overview.appsWithoutRules')}
                value={d.apps_without_rules}
                hint={t('sso:overview.appsWithoutRulesHint')}
                bad={(d.apps_without_rules ?? 0) > 0}
                onClick={() => onGo('policies')}
              />
              <Stat
                label={t('sso:overview.probesFailing')}
                value={d.probes_failing}
                hint={t('sso:overview.probesFailingHint')}
                bad={(d.probes_failing ?? 0) > 0}
                onClick={() => onGo('resilience')}
              />
              <Stat
                label={t('sso:overview.probesNeverRun')}
                value={d.probes_never_run}
                hint={t('sso:overview.probesNeverRunHint')}
                bad={(d.probes_never_run ?? 0) > 0}
                onClick={() => onGo('resilience')}
              />
              <Stat
                label={t('sso:overview.denied24h')}
                value={d.denied_24h}
                hint={t('sso:overview.denied24hHint')}
                onClick={() => onGo('audit')}
              />
            </div>

            <h2 className="mt-6 mb-2 text-[15px] font-semibold">{t('sso:overview.scale')}</h2>
            <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-4">
              <Stat label={t('sso:console.apps')} value={d.apps} onClick={() => onGo('apps')} />
              <Stat label={t('sso:overview.rules')} value={d.rules} onClick={() => onGo('policies')} />
              <Stat
                label={t('sso:overview.users')}
                value={d.users}
                onClick={() => onGo('identities')}
              />
              <Stat
                label={t('sso:overview.sessions')}
                value={d.sessions}
                onClick={() => onGo('sessions')}
              />
            </div>
          </>
        )}
      </AsyncBoundary>
    </>
  )
}

function Stat({
  label,
  value,
  hint,
  bad,
  onClick,
}: {
  label: string
  value: number | null
  hint?: string
  bad?: boolean
  onClick: () => void
}) {
  const { t } = useTranslation()
  return (
    <button
      type="button"
      onClick={onClick}
      className={[
        'cursor-pointer rounded-[var(--radius-md)] border p-3.5 text-left transition-colors duration-150 hover:bg-muted',
        bad ? 'border-warning bg-warning-bg' : 'border-border bg-card',
      ].join(' ')}
    >
      <span className="block text-xs text-muted-foreground">{label}</span>
      {value === null ? (
        // 取不到 ≠ 0。写成 0 就是把「不知道」报告成「没问题」。
        <span className="mt-1 block text-[15px] text-warning">{t('sso:overview.unavailable')}</span>
      ) : (
        <span className="mt-1 block text-2xl font-semibold tabular-nums">{value}</span>
      )}
      {hint ? <span className="mt-1 block text-[11px] text-muted-foreground">{hint}</span> : null}
    </button>
  )
}

function ChainCard({
  chain,
  onGo,
}: {
  chain: Overview['audit_chain']
  onGo: (key: string) => void
}) {
  const { t } = useTranslation()
  const tone =
    chain === null
      ? 'border-warning bg-warning-bg'
      : chain.ok
        ? 'border-success bg-success-bg'
        : 'border-destructive bg-danger-bg'
  return (
    <button
      type="button"
      onClick={() => onGo('audit')}
      className={`flex w-full cursor-pointer items-center gap-3 rounded-[var(--radius-md)] border p-4 text-left ${tone}`}
    >
      {chain?.ok ? (
        <ShieldCheck className="size-5 shrink-0 text-success" />
      ) : (
        <ShieldAlert className="size-5 shrink-0 text-warning" />
      )}
      <span>
        <b className="block text-sm font-semibold">{t('sso:audit.chain.title')}</b>
        <span className="text-xs text-muted-foreground">
          {chain === null
            ? t('sso:audit.chain.unavailable')
            : chain.ok
              ? t('sso:audit.chain.ok', { count: chain.checked })
              : t('sso:audit.chain.broken', { seq: chain.broken_at_seq ?? '?' })}
        </span>
      </span>
    </button>
  )
}
