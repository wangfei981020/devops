import { useTranslation } from '@ops/i18n'
import { AsyncBoundary, Skeleton, fromQuery } from '@ops/ui'
import { useQuery } from '@tanstack/react-query'
import { AlertTriangle, Check, Circle, Copy, HelpCircle } from 'lucide-react'
import { useMemo, useState } from 'react'
import { api } from '../../api/client.js'
import type { App, ListOf } from '../../api/types.js'
import { makeToLoadError } from '../../shared/errorText.js'

interface Onboarding {
  app: { id: number; name: string; code: string; connect_type: string; base_url: string }
  issuer: string
  discovery_url: string
  jwks_url: string
  issuer_is_local: boolean
  oidc_clients: { client_id: string; redirect_uris: string[]; enabled: boolean }[]
  routes: { host: string; upstream: string }[]
  path_rules: number
  policy: { allowed_users: number; total_users: number; unavailable?: boolean }
  traffic: { gateway_events: number; gateway_denied: number; oidc_attempts: number; last_at: string }
}

/**
 * 目标系统模板。
 *
 * 只存两样东西：**回调地址的固定形状**，和**那个系统里字段叫什么**。
 * 这两样正是接入时最容易错的：回调路径每家都不一样且写死在对方代码里，
 * 而字段名对不上会让人以为是我们这边没给。
 */
const TARGETS = [
  { key: 'harbor', callback: '/c/oidc/callback' },
  { key: 'gitlab', callback: '/users/auth/openid_connect/callback' },
  { key: 'grafana', callback: '/login/generic_oauth' },
  { key: 'generic', callback: '/oidc/callback' },
] as const

/**
 * 接入指引。
 *
 * # 为什么不是一份文档
 *
 * 文档的失败模式很固定：**手册里的值和实际跑着的值对不上**。
 * 而接入最常卡住的三件事恰好全是"值" —— issuer 抄错、回调地址差一个字符、
 * client_id 与实际不符。会过期的文档解决不了这个。
 *
 * 这一页上每一个值都取自**当前进程**，每一个勾都来自**当前库里的事实**。
 *
 * # 「我们能验的」和「验不了的」必须分开
 *
 * 对方系统里填对没有，我们看不见 —— 那台机器不归我们管。
 * 混成一个绿勾等于告诉客户"都好了"，而他一点就报错。
 */
export function OnboardingPage({ appID, onBack }: { appID: number; onBack: () => void }) {
  const { t } = useTranslation()
  const toLoadError = useMemo(() => makeToLoadError(t), [t])
  const [target, setTarget] = useState<string>('harbor')

  const q = useQuery({
    queryKey: ['onboarding', appID],
    queryFn: () => api.get<Onboarding>(`/apps/${appID}/onboarding`),
  })
  const state = fromQuery<Onboarding>(q, () => false, toLoadError)

  return (
    <>
      <button
        type="button"
        onClick={onBack}
        className="mb-3 cursor-pointer text-[12px] text-muted-foreground hover:text-foreground"
      >
        ← {t('sso:onboard.back')}
      </button>

      <AsyncBoundary
        state={state}
        errorTitle={t('sso:onboard.errorTitle')}
        retryLabel={t('retry')}
        onRetry={() => void q.refetch()}
        empty={null}
        pending={<Skeleton className="h-64 w-full" />}
      >
        {(d) => (
          <>
            <h1 className="text-xl font-semibold tracking-tight">
              {t('sso:onboard.title', { name: d.app.name })}
            </h1>
            <p className="mt-1 mb-4 max-w-[80ch] text-sm text-muted-foreground">
              {t('sso:onboard.intro')}
            </p>

            {/* issuer 指向本机时，任何外部下游都接不进来 —— 这条要摆在最上面，
                因为它一错，下面每一步都白做，而错误只会出现在对方那边。 */}
            {d.issuer_is_local ? (
              <p className="mb-4 max-w-[80ch] rounded-[var(--radius)] border border-destructive bg-danger-bg px-3 py-2 text-[12px]">
                {t('sso:onboard.issuerLocal', { issuer: d.issuer })}
              </p>
            ) : null}

            {d.app.connect_type === 'oidc' ? (
              <OIDCSteps d={d} target={target} onTarget={setTarget} />
            ) : d.app.connect_type === 'gateway' ? (
              <GatewaySteps d={d} />
            ) : (
              <p className="rounded-[var(--radius-md)] border border-warning bg-warning-bg p-4 text-[13px]">
                {t('sso:onboard.notSupported', { type: d.app.connect_type })}
              </p>
            )}

            <Traffic d={d} />
          </>
        )}
      </AsyncBoundary>
    </>
  )
}

function OIDCSteps({
  d,
  target,
  onTarget,
}: {
  d: Onboarding
  target: string
  onTarget: (v: string) => void
}) {
  const { t } = useTranslation()
  const tpl = TARGETS.find((x) => x.key === target) ?? TARGETS[3]
  const base = (d.app.base_url || 'https://<对方地址>').replace(/\/+$/, '')
  const expectedCallback = base + tpl.callback
  const client = d.oidc_clients[0]
  const callbackMatches = client?.redirect_uris.includes(expectedCallback) ?? false

  return (
    <>
      <div className="mb-4 flex flex-wrap items-center gap-2">
        <span className="text-[12px] text-muted-foreground">{t('sso:onboard.target')}</span>
        {TARGETS.map((x) => (
          <button
            key={x.key}
            type="button"
            onClick={() => onTarget(x.key)}
            aria-pressed={target === x.key}
            className={[
              'cursor-pointer rounded-[var(--radius)] border px-2.5 py-1 text-[12px]',
              target === x.key ? 'border-brand bg-brand-bg text-brand' : 'border-border',
            ].join(' ')}
          >
            {t(`sso:onboard.target_${x.key}`)}
          </button>
        ))}
      </div>

      <Step
        n={1}
        title={t('sso:onboard.o1')}
        done={!!client}
        body={
          !client ? (
            <p className="text-[12px] text-muted-foreground">{t('sso:onboard.o1none')}</p>
          ) : (
            <>
              <Row label="client_id" value={client.client_id} />
              <p className="mt-1.5 text-[11px] text-muted-foreground">
                {t('sso:onboard.o1secret')}
              </p>
            </>
          )
        }
      />

      <Step
        n={2}
        title={t('sso:onboard.o2')}
        done={callbackMatches}
        warn={!!client && !callbackMatches}
        body={
          <>
            <Row label={t('sso:onboard.shouldBe')} value={expectedCallback} />
            {client ? (
              <Row label={t('sso:onboard.current')} value={client.redirect_uris.join('  ') || '—'} />
            ) : null}
            {/* 回调地址差一个字符就会被拒，而对方报的错不会指回这里 */}
            <p className="mt-1.5 text-[11px] text-muted-foreground">{t('sso:onboard.o2hint')}</p>
          </>
        }
      />

      <Step
        n={3}
        title={t('sso:onboard.o3')}
        done={d.policy.allowed_users > 0}
        unknown={d.policy.unavailable}
        body={<PolicyBody p={d.policy} />}
      />

      <Step
        n={4}
        title={t('sso:onboard.o4')}
        unknown
        body={
          <>
            <p className="mb-2 text-[12px] text-muted-foreground">{t('sso:onboard.o4hint')}</p>
            <div className="overflow-hidden rounded-[var(--radius)] border border-border">
              <table className="w-full text-[12px]">
                <tbody>
                  <FieldRow label={t(`sso:onboard.f_${target}_endpoint`)} value={d.issuer} />
                  <FieldRow label="Client ID" value={client?.client_id ?? '—'} />
                  <FieldRow label="Client Secret" value={t('sso:onboard.secretOnce')} plain />
                  <FieldRow label={t('sso:onboard.fScope')} value="openid,profile,email" />
                  <FieldRow label={t('sso:onboard.fUsername')} value="preferred_username" />
                  <FieldRow label={t('sso:onboard.fGroups')} value="groups" />
                </tbody>
              </table>
            </div>
            {/* 我们的 OP 不发 refresh_token，是有意的 —— 这句必须给出来，
                否则对方按默认配置写上 offline_access，卡住时不知道为什么 */}
            <p className="mt-2 rounded-[var(--radius)] border border-warning bg-warning-bg px-2.5 py-1.5 text-[11px]">
              {t('sso:onboard.noRefresh')}
            </p>
          </>
        }
      />

      <details className="mt-3 rounded-[var(--radius-md)] border border-border bg-card p-3">
        <summary className="cursor-pointer text-[13px] font-medium">
          {t('sso:onboard.rawTitle')}
        </summary>
        <div className="mt-2 grid gap-1.5">
          <Row label="issuer" value={d.issuer} />
          <Row label={t('sso:onboard.discovery')} value={d.discovery_url} />
          <Row label="jwks_uri" value={d.jwks_url} />
        </div>
        <p className="mt-2 text-[11px] text-muted-foreground">{t('sso:onboard.rawHint')}</p>
      </details>
    </>
  )
}

function GatewaySteps({ d }: { d: Onboarding }) {
  const { t } = useTranslation()
  return (
    <>
      <Step
        n={1}
        title={t('sso:onboard.g1')}
        done={d.routes.length > 0}
        body={
          d.routes.length === 0 ? (
            <p className="text-[12px] text-muted-foreground">{t('sso:onboard.g1none')}</p>
          ) : (
            <>
              {d.routes.map((r) => (
                <Row key={r.host} label={r.host} value={r.upstream} />
              ))}
              <p className="mt-1.5 text-[11px] text-muted-foreground">{t('sso:onboard.g1hint')}</p>
            </>
          )
        }
      />
      <Step
        n={2}
        title={t('sso:onboard.g2')}
        done={d.policy.allowed_users > 0}
        unknown={d.policy.unavailable}
        body={<PolicyBody p={d.policy} />}
      />
      <Step
        n={3}
        title={t('sso:onboard.g3')}
        done={d.path_rules > 0}
        warn={d.path_rules === 0}
        body={
          <p className="text-[12px] text-muted-foreground">
            {d.path_rules === 0 ? t('sso:onboard.g3none') : t('sso:onboard.g3ok', { n: d.path_rules })}
          </p>
        }
      />
      <Step
        n={4}
        title={t('sso:onboard.g4')}
        unknown
        body={<p className="text-[12px] text-muted-foreground">{t('sso:onboard.g4hint')}</p>}
      />
    </>
  )
}

/**
 * 有没有真的来过流量。
 *
 * 这是这一页比文档多出来的全部价值：「配完了但从没人来过」和「来了但被拒」，
 * 下一步完全不同 —— 前者去查对方系统的配置，后者去查这边的策略。
 */
function Traffic({ d }: { d: Onboarding }) {
  const { t } = useTranslation()
  const total = d.traffic.gateway_events + d.traffic.oidc_attempts
  return (
    <section
      className={[
        'mt-4 rounded-[var(--radius-md)] border p-4',
        total === 0 ? 'border-warning bg-warning-bg' : 'border-border bg-card',
      ].join(' ')}
    >
      <h2 className="text-sm font-semibold">{t('sso:onboard.trafficTitle')}</h2>
      {total === 0 ? (
        <p className="mt-1 text-[12px]">{t('sso:onboard.trafficNone')}</p>
      ) : (
        <p className="mt-1 text-[12px]">
          {t('sso:onboard.trafficSome', {
            n: total,
            denied: d.traffic.gateway_denied,
            at: d.traffic.last_at || '—',
          })}
        </p>
      )}
    </section>
  )
}

/**
 * 「有几个人能进」而不是「有没有配规则」。
 *
 * ⚠️ 第一版数的是 allow 规则条数，结果一条给别人的全局 allow
 * 就让这一步打了绿勾 —— 而实际没有任何人能进这个应用。
 * 绿勾说的是"配好了"，那就必须真的配好了。
 */
function PolicyBody({ p }: { p: Onboarding['policy'] }) {
  const { t } = useTranslation()
  if (p.unavailable) {
    return <p className="text-[12px] text-warning">{t('sso:onboard.o3unavailable')}</p>
  }
  return (
    <p className="text-[12px] text-muted-foreground">
      {p.allowed_users > 0
        ? t('sso:onboard.o3ok', { n: p.allowed_users, total: p.total_users })
        : t('sso:onboard.o3hint')}
    </p>
  )
}

function Step({
  n,
  title,
  done,
  warn,
  unknown,
  body,
}: {
  n: number
  title: string
  done?: boolean
  warn?: boolean
  unknown?: boolean
  body: React.ReactNode
}) {
  const { t } = useTranslation()
  return (
    <section className="mb-3 rounded-[var(--radius-md)] border border-border bg-card p-4">
      <div className="flex items-start gap-2.5">
        <span className="mt-0.5 shrink-0">
          {/* 三态，不是两态：「做完了」「还没做」「我们看不见」。
              把第三种画成灰勾或红叉都是撒谎。 */}
          {unknown ? (
            <HelpCircle className="size-4 text-muted-foreground" />
          ) : done ? (
            <Check className="size-4 text-success" />
          ) : warn ? (
            <AlertTriangle className="size-4 text-warning" />
          ) : (
            <Circle className="size-4 text-muted-foreground" />
          )}
        </span>
        <div className="min-w-0 flex-1">
          <b className="text-[13px] font-medium">
            {n}. {title}
          </b>
          {unknown ? (
            <span className="ml-2 rounded-[var(--radius-sm)] bg-muted px-1.5 py-0.5 text-[10px] text-muted-foreground">
              {t('sso:onboard.cannotVerify')}
            </span>
          ) : null}
          <div className="mt-2">{body}</div>
        </div>
      </div>
    </section>
  )
}

function Row({ label, value }: { label: string; value: string }) {
  const [copied, setCopied] = useState(false)
  return (
    <div className="flex items-center gap-2 text-[12px]">
      <span className="w-28 shrink-0 text-muted-foreground">{label}</span>
      <code className="min-w-0 flex-1 overflow-x-auto rounded-[var(--radius-sm)] bg-muted px-2 py-1 font-mono whitespace-nowrap">
        {value}
      </code>
      <button
        type="button"
        onClick={() => {
          void navigator.clipboard.writeText(value).then(
            () => {
              setCopied(true)
              setTimeout(() => setCopied(false), 1500)
            },
            () => {
              /* 非 https 下浏览器拒绝剪贴板；值就在屏幕上 */
            },
          )
        }}
        className="shrink-0 cursor-pointer rounded-[var(--radius)] border border-border p-1 text-muted-foreground hover:bg-secondary"
      >
        {copied ? <Check className="size-3.5 text-success" /> : <Copy className="size-3.5" />}
      </button>
    </div>
  )
}

function FieldRow({ label, value, plain }: { label: string; value: string; plain?: boolean }) {
  return (
    <tr className="border-t border-border first:border-t-0">
      <td className="w-56 px-3 py-1.5 text-muted-foreground">{label}</td>
      <td className={plain ? 'px-3 py-1.5' : 'px-3 py-1.5 font-mono'}>{value}</td>
    </tr>
  )
}

/** 应用列表里的入口用得到：判断这个应用有没有接完。 */
export function useOnboardingApps() {
  return useQuery({ queryKey: ['apps'], queryFn: () => api.get<ListOf<App>>('/apps') })
}
