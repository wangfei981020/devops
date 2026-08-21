import { useTranslation } from '@ops/i18n'
import { Pwd } from '../../shared/Pwd.js'
import { AsyncBoundary, EmptyState, Skeleton, fromQuery } from '@ops/ui'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { KeyRound, LifeBuoy } from 'lucide-react'
import { useMemo, useState } from 'react'
import { ApiError, api } from '../../api/client.js'
import type { App, ListOf } from '../../api/types.js'
import { makeToLoadError } from '../../shared/errorText.js'

interface Probe {
  id: number
  app_id: number
  app_name: string
  username: string
  allowed_cidr: string
  interval_sec: number
  quiet_hours: string
  last_probe_at?: string
  last_result: string
  last_detail: string
  revoked: boolean
}

/**
 * 韧性：这套东西自己挂了怎么办。
 *
 * # 为什么这一页非有不可
 *
 * SSO 是所有系统的入口，它挂了就等于所有系统一起挂 —— 而且**连修的人
 * 都进不去**。这是这个品类最真实的风险，也是客户安全评审必问的一条。
 * 竞品普遍把这件事留给运维自己想办法。
 *
 * 两件事：
 *   - 探针：从外面定时试着登一次，证明"现在还能用"，而不是等人来报障
 *   - 应急通行码：SSO 自己不可用时，指定的人还能进得去
 */
export function ResiliencePage() {
  const { t } = useTranslation()

  return (
    <>
      <h1 className="text-xl font-semibold tracking-tight">{t('sso:console.resilience')}</h1>
      <p className="mt-1 mb-5 max-w-[80ch] text-sm text-muted-foreground">
        {t('sso:resilience.intro')}
      </p>

      <Probes />
      <BreakGlass />
    </>
  )
}

function Probes() {
  const { t } = useTranslation()
  const qc = useQueryClient()
  const toLoadError = useMemo(() => makeToLoadError(t), [t])
  const [adding, setAdding] = useState(false)
  const [revokeErr, setRevokeErr] = useState<number | null>(null)
  const q = useQuery({ queryKey: ['probes'], queryFn: () => api.get<ListOf<Probe>>('/probes') })
  const revoke = useMutation({
    mutationFn: (id: number) => api.del(`/probes/${id}`),
    onSuccess: () => {
      setRevokeErr(null)
      void qc.invalidateQueries({ queryKey: ['probes'] })
      void qc.invalidateQueries({ queryKey: ['overview'] })
    },
    // 吊销失败必须说出来。乐观地把行划掉、实际没吊销，
    // 意味着一份还能用的凭据被当成已经作废了。
    onError: (_e, id) => setRevokeErr(id),
  })
  const state = fromQuery<ListOf<Probe>>(q, (d) => d.items.length === 0, toLoadError)

  return (
    <section className="mb-6">
      <h2 className="mb-1 text-[15px] font-semibold">{t('sso:resilience.probes.title')}</h2>
      <p className="mb-3 max-w-[80ch] text-[13px] text-muted-foreground">
        {t('sso:resilience.probes.desc')}
      </p>

      {/* ⚠️ 执行器还没做。这条必须在配置入口之前 —— 界面此前描述的是一个
          不存在的能力（"定时走一遍真实登录"），而全仓没有任何代码去拨测。 */}
      <p className="mb-3 rounded-[var(--radius)] border border-warning bg-warning-bg px-3 py-2 text-[12px]">
        {t('sso:resilience.probes.notRunning')}
      </p>

      {adding ? (
        <div className="mb-3">
          <NewProbeForm onDone={() => setAdding(false)} />
        </div>
      ) : (
        <button
          type="button"
          onClick={() => setAdding(true)}
          className="mb-3 cursor-pointer rounded-[var(--radius)] border border-border px-2.5 py-1 text-[12px] hover:bg-secondary"
        >
          {t('sso:resilience.probes.addBtn')}
        </button>
      )}

      {revokeErr !== null ? (
        <p className="mb-3 rounded-[var(--radius)] border border-destructive bg-danger-bg px-3 py-2 text-[12px] text-danger">
          {t('sso:resilience.probes.revokeFailed')}
        </p>
      ) : null}

      <div className="overflow-hidden rounded-[var(--radius-md)] border border-border bg-card">
        <AsyncBoundary
          state={state}
          errorTitle={t('sso:resilience.probes.errorTitle')}
          retryLabel={t('retry')}
          onRetry={() => void q.refetch()}
          pending={
            <div className="space-y-2 p-4">
              {[0, 1].map((i) => (
                <Skeleton key={i} className="h-10 w-full" />
              ))}
            </div>
          }
          empty={
            <EmptyState
              icon={<LifeBuoy />}
              title={t('sso:resilience.probes.empty.title')}
              reason={t('sso:resilience.probes.empty.reason')}
              action={null}
            />
          }
        >
          {(data) => (
            <div className="overflow-x-auto">
              <table className="w-full min-w-[820px] text-[13px]">
                <thead>
                  <tr className="bg-muted text-xs text-muted-foreground">
                    <th className="px-3.5 py-2.5 text-left font-medium">
                      {t('sso:resilience.probes.colApp')}
                    </th>
                    <th className="px-3.5 py-2.5 text-left font-medium">
                      {t('sso:resilience.probes.colHealth')}
                    </th>
                    <th className="px-3.5 py-2.5 text-left font-medium">
                      {t('sso:resilience.probes.colLast')}
                    </th>
                    <th className="px-3.5 py-2.5 text-left font-medium">
                      {t('sso:resilience.probes.colFrom')}
                    </th>
                    <th className="px-3.5 py-2.5" />
                  </tr>
                </thead>
                <tbody>
                  {data.items.map((p) => (
                    <tr key={p.id} className="border-t border-border hover:bg-muted">
                      <td className="px-3.5 py-2.5">
                        <b className="font-medium">{p.app_name}</b>
                        <span className="ml-2 font-mono text-[11px] text-muted-foreground">
                          {p.username}
                        </span>
                      </td>
                      <td className="px-3.5 py-2.5">
                        <Health probe={p} />
                      </td>
                      <td className="whitespace-nowrap px-3.5 py-2.5 text-muted-foreground">
                        {p.last_probe_at ? fmt(p.last_probe_at) : '—'}
                        <span className="ml-2 text-[11px]">
                          {t('sso:resilience.probes.every', { n: p.interval_sec })}
                        </span>
                      </td>
                      <td className="whitespace-nowrap px-3.5 py-2.5 font-mono text-[11px] text-muted-foreground">
                        {p.allowed_cidr || '—'}
                      </td>
                      <td className="px-3.5 py-2.5 text-right">
                        {p.revoked ? (
                          <span className="text-[11px] text-muted-foreground">
                            {t('sso:resilience.probes.revoked')}
                          </span>
                        ) : (
                          <button
                            type="button"
                            disabled={revoke.isPending}
                            onClick={() => revoke.mutate(p.id)}
                            className="cursor-pointer rounded-[var(--radius)] border border-border px-2 py-1 text-[11px] hover:bg-secondary disabled:opacity-40"
                          >
                            {t('sso:resilience.probes.revokeBtn')}
                          </button>
                        )}
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
        </AsyncBoundary>
      </div>
    </section>
  )
}

/**
 * 探针健康态是**四态**，不是「好 / 坏」。
 *
 * 「从没测过」和「测过没问题」长得一样，是这类页面最常见的谎：
 * 一个从来没跑起来的探针会一直显示绿色，而它恰恰什么都没在保障。
 * 「已吊销」同理 —— 停了就该退回未覆盖，不能停在最后一次成功上。
 */
function Health({ probe }: { probe: Probe }) {
  const { t } = useTranslation()
  if (probe.revoked) {
    return (
      <span className="rounded-[var(--radius-sm)] bg-muted px-2 py-0.5 text-[11px] text-muted-foreground">
        {t('sso:resilience.probes.revoked')}
      </span>
    )
  }
  if (!probe.last_result) {
    return (
      <span className="rounded-[var(--radius-sm)] bg-warning-bg px-2 py-0.5 text-[11px] text-warning">
        {t('sso:resilience.probes.never')}
      </span>
    )
  }
  const ok = probe.last_result === 'ok'
  return (
    <span
      className={[
        'rounded-[var(--radius-sm)] px-2 py-0.5 text-[11px]',
        ok ? 'bg-success-bg text-success' : 'bg-danger-bg text-danger',
      ].join(' ')}
      title={probe.last_detail || undefined}
    >
      {ok ? t('sso:resilience.probes.ok') : t('sso:resilience.probes.failing')}
      {!ok && probe.last_detail ? `：${probe.last_detail}` : ''}
    </span>
  )
}

/**
 * 新增探针。
 *
 * ⚠️ 这里要填一个**能登进被测系统的账号**。所以：
 *   - 必须用只读账号，不能用管理员 —— 探针会长期持有这份凭据
 *   - 来源网段必填（后端也会拒），不限来源等于给了一把万能钥匙
 *   - 口令存进库时加密，接口从不返回 —— 填错只能重填，看不回来
 */
function NewProbeForm({ onDone }: { onDone: () => void }) {
  const { t } = useTranslation()
  const qc = useQueryClient()
  const [appID, setAppID] = useState('')
  const [username, setUsername] = useState('')
  const [secret, setSecret] = useState('')
  const [cidr, setCIDR] = useState('')
  const [interval, setInterval] = useState('300')
  const [err, setErr] = useState<string | null>(null)

  const apps = useQuery({ queryKey: ['apps'], queryFn: () => api.get<ListOf<App>>('/apps') })

  const m = useMutation({
    mutationFn: () =>
      api.post('/probes', {
        app_id: Number(appID),
        username: username.trim(),
        secret,
        allowed_cidr: cidr.trim(),
        interval_sec: Number(interval) || 300,
      }),
    onSuccess: () => {
      setErr(null)
      void qc.invalidateQueries({ queryKey: ['probes'] })
      void qc.invalidateQueries({ queryKey: ['overview'] })
      onDone()
    },
    onError: (e) =>
      setErr(
        e instanceof ApiError
          ? t(`errors.${e.code}`, { ...e.params, defaultValue: e.code })
          : t('error.unreachable'),
      ),
  })

  const ready = appID !== '' && username.trim() !== '' && cidr.trim() !== ''

  return (
    <div className="rounded-[var(--radius-md)] border border-border bg-card p-4">
      <h3 className="text-[13px] font-semibold">{t('sso:resilience.probes.newTitle')}</h3>
      <div className="mt-3 grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
        <label className="block text-[12px]">
          <span className="mb-1 block text-muted-foreground">
            {t('sso:resilience.probes.fApp')}
          </span>
          <select
            value={appID}
            onChange={(e) => setAppID(e.target.value)}
            className="w-full rounded-[var(--radius)] border border-border bg-background px-2.5 py-1.5 text-[13px]"
          >
            <option value="">{t('sso:resilience.probes.pickApp')}</option>
            {(apps.data?.items ?? []).map((a) => (
              <option key={a.id} value={a.id}>
                {a.name}（{a.env}）
              </option>
            ))}
          </select>
        </label>
        <Labeled label={t('sso:resilience.probes.fUser')} hint={t('sso:resilience.probes.fUserHint')}>
          <input
            value={username}
            onChange={(e) => setUsername(e.target.value)}
            className="w-full rounded-[var(--radius)] border border-border bg-background px-2.5 py-1.5 text-[13px]"
          />
        </Labeled>
        <Labeled label={t('sso:resilience.probes.fSecret')} hint={t('sso:resilience.probes.fSecretHint')}>
          <Pwd
            value={secret}
            onChange={(e) => setSecret(e.target.value)}
            className="border-border"
          />
        </Labeled>
        <Labeled label={t('sso:resilience.probes.fCIDR')} hint={t('sso:resilience.probes.fCIDRHint')}>
          <input
            value={cidr}
            onChange={(e) => setCIDR(e.target.value)}
            placeholder="10.0.0.0/8"
            className="w-full rounded-[var(--radius)] border border-border bg-background px-2.5 py-1.5 text-[13px]"
          />
        </Labeled>
        <Labeled
          label={t('sso:resilience.probes.fInterval')}
          hint={t('sso:resilience.probes.fIntervalHint')}
        >
          <input
            value={interval}
            onChange={(e) => setInterval(e.target.value.replace(/\D/g, ''))}
            className="w-full rounded-[var(--radius)] border border-border bg-background px-2.5 py-1.5 text-[13px]"
          />
        </Labeled>
      </div>

      {err ? (
        <p className="mt-3 rounded-[var(--radius)] bg-danger-bg px-3 py-2 text-[12px] text-danger">
          {err}
        </p>
      ) : null}

      <div className="mt-3 flex items-center gap-2">
        <button
          type="button"
          disabled={!ready || m.isPending}
          onClick={() => m.mutate()}
          className="cursor-pointer rounded-[var(--radius)] bg-brand px-3 py-1.5 text-[13px] font-medium text-brand-foreground disabled:cursor-default disabled:opacity-50"
        >
          {m.isPending ? t('sso:resilience.probes.saving') : t('sso:resilience.probes.save')}
        </button>
        <button
          type="button"
          onClick={onDone}
          className="cursor-pointer rounded-[var(--radius)] border border-border px-3 py-1.5 text-[13px] hover:bg-secondary"
        >
          {t('sso:resilience.bg.cancel')}
        </button>
        <span className="text-[11px] text-muted-foreground">
          {t('sso:resilience.probes.readOnlyWarn')}
        </span>
      </div>
    </div>
  )
}

function Labeled({
  label,
  hint,
  children,
}: {
  label: string
  hint?: string
  children: React.ReactNode
}) {
  return (
    <label className="block text-[12px]">
      <span className="mb-1 block text-muted-foreground">{label}</span>
      {children}
      {hint ? <span className="mt-1 block text-[11px] text-muted-foreground">{hint}</span> : null}
    </label>
  )
}

/** 应急通行码。 */
function BreakGlass() {
  const { t } = useTranslation()
  const [userID, setUserID] = useState('')
  const [codes, setCodes] = useState<string[] | null>(null)

  const m = useMutation({
    mutationFn: (id: number) =>
      api.post<{ codes: string[] }>('/auth/break-glass/issue', { user_id: id, count: 5, days: 180 }),
    onSuccess: (r) => setCodes(r.codes),
  })

  return (
    <section>
      <h2 className="mb-1 text-[15px] font-semibold">{t('sso:resilience.bg.title')}</h2>
      <p className="mb-3 max-w-[80ch] text-[13px] text-muted-foreground">
        {t('sso:resilience.bg.desc')}
      </p>

      <div className="rounded-[var(--radius-md)] border border-border bg-card p-4">
        <div className="flex flex-wrap items-end gap-3">
          <label className="text-[13px]">
            <span className="mb-1 block text-muted-foreground">{t('sso:resilience.bg.forUser')}</span>
            <input
              value={userID}
              onChange={(e) => setUserID(e.target.value.replace(/\D/g, ''))}
              placeholder={t('sso:resilience.bg.userPlaceholder')}
              className="w-44 rounded-[var(--radius)] border border-border bg-background px-2.5 py-1.5 text-[13px]"
            />
          </label>
          <button
            type="button"
            disabled={!userID || m.isPending}
            onClick={() => m.mutate(Number(userID))}
            className="cursor-pointer rounded-[var(--radius)] bg-brand px-3 py-1.5 text-[13px] font-medium text-brand-foreground disabled:cursor-default disabled:opacity-50"
          >
            {m.isPending ? t('sso:resilience.bg.issuing') : t('sso:resilience.bg.issue')}
          </button>
          <span className="text-[11px] text-muted-foreground">{t('sso:resilience.bg.params')}</span>
        </div>

        {m.isError ? (
          <p className="mt-3 rounded-[var(--radius)] bg-danger-bg px-3 py-2 text-[12px] text-danger">
            {t('sso:resilience.bg.failed')}
          </p>
        ) : null}

        {codes ? (
          <div className="mt-4 rounded-[var(--radius)] border border-warning bg-warning-bg p-3">
            {/* 明文只出现这一次。关掉就再也拿不回来 —— 这句必须在码的上面，
                不能在下面：人会先复制再读说明。 */}
            <p className="mb-2 text-[12px] font-medium">{t('sso:resilience.bg.printAndSeal')}</p>
            <ul className="grid gap-1 font-mono text-[13px]">
              {codes.map((c) => (
                <li key={c}>{c}</li>
              ))}
            </ul>
          </div>
        ) : null}
      </div>
    </section>
  )
}

function fmt(s: string) {
  const d = new Date(s)
  if (Number.isNaN(d.getTime())) return s
  const p = (n: number) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())} ${p(d.getHours())}:${p(d.getMinutes())}`
}
