import { useTranslation } from '@ops/i18n'
import { Pwd } from '../../shared/Pwd.js'
import { AsyncBoundary, EmptyState, Skeleton, fromQuery } from '@ops/ui'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Building2 } from 'lucide-react'
import { useMemo, useState } from 'react'
import { ApiError, api } from '../../api/client.js'
import type { ListOf } from '../../api/types.js'
import { makeToLoadError } from '../../shared/errorText.js'

interface IdPConfig {
  id: number
  name: string
  issuer: string
  client_id: string
  has_secret: boolean
  auth_url: string
  token_url: string
  jwks_url: string
  redirect_uri: string
  scopes: string
  subject_claim: string
  name_claim: string
  email_claim: string
  groups_claim: string
  jit_create: boolean
  jit_groups: string
  enabled: boolean
}

/**
 * 预设。
 *
 * 存在的理由是 **subject_claim** 那一栏：三家给的稳定标识不一样，
 * 选错了不会报错 —— 人能登进来，但每次都被当成新用户，
 * 或者改个邮箱就变成另一个人、历史审计全对不上。
 * 这是接入时最贵的一个错，也是最不容易自己发现的。
 */
const PRESETS = [
  {
    key: 'entra',
    issuer: 'https://login.microsoftonline.com/<租户ID>/v2.0',
    scopes: 'openid profile email',
    subject: 'oid',
    name: 'name',
    email: 'preferred_username',
    groups: 'groups',
    discoverable: true,
  },
  {
    key: 'lark',
    issuer: 'https://open.feishu.cn',
    scopes: 'openid profile',
    subject: 'union_id',
    name: 'name',
    email: 'email',
    groups: '',
    discoverable: false,
  },
  {
    key: 'generic',
    issuer: '',
    scopes: 'openid profile email',
    subject: 'sub',
    name: 'name',
    email: 'email',
    groups: 'groups',
    discoverable: true,
  },
] as const

/**
 * 身份源。
 *
 * # 现状必须说清楚
 *
 * 这一页能把配置写进去，登录链路的代码也早就有（/oidc/start、/oidc/callback），
 * 但**整条链路还没有拿真实账号跑通过**。配好了不等于能登进来 ——
 * 界面上不能只显示一个绿色的「已启用」，那会让人以为已经可用。
 */
export function IdPsPage() {
  const { t } = useTranslation()
  const toLoadError = useMemo(() => makeToLoadError(t), [t])
  const [editing, setEditing] = useState<IdPConfig | 'new' | null>(null)

  const q = useQuery({
    queryKey: ['idp-configs'],
    queryFn: () => api.get<ListOf<IdPConfig>>('/idp-configs'),
  })
  const state = fromQuery<ListOf<IdPConfig>>(q, (d) => d.items.length === 0, toLoadError)

  return (
    <>
      <h1 className="text-xl font-semibold tracking-tight">{t('sso:console.idps')}</h1>
      <p className="mt-1 mb-3 max-w-[80ch] text-sm text-muted-foreground">{t('sso:idp.intro')}</p>

      {/* 状态如实说：配置能写，链路没验过。写成「已启用」会被读成「能用了」 */}
      <p className="mb-4 max-w-[80ch] rounded-[var(--radius)] border border-warning bg-warning-bg px-3 py-2 text-[12px]">
        {t('sso:idp.unverified')}
      </p>

      {editing ? (
        <IdPForm
          cfg={editing === 'new' ? null : editing}
          onDone={() => {
            setEditing(null)
            void q.refetch()
          }}
        />
      ) : (
        <button
          type="button"
          onClick={() => setEditing('new')}
          className="mb-4 cursor-pointer rounded-[var(--radius)] bg-brand px-3 py-1.5 text-[13px] font-medium text-brand-foreground"
        >
          {t('sso:idp.addBtn')}
        </button>
      )}

      <div className="overflow-hidden rounded-[var(--radius-md)] border border-border bg-card">
        <AsyncBoundary
          state={state}
          errorTitle={t('sso:idp.errorTitle')}
          retryLabel={t('retry')}
          onRetry={() => void q.refetch()}
          pending={<Skeleton className="m-4 h-20" />}
          empty={
            <EmptyState
              icon={<Building2 />}
              title={t('sso:idp.empty.title')}
              reason={t('sso:idp.empty.reason')}
              action={null}
            />
          }
        >
          {(d) => (
            <table className="w-full text-[13px]">
              <thead>
                <tr className="bg-muted text-xs text-muted-foreground">
                  <th className="px-3.5 py-2.5 text-left font-medium">{t('sso:idp.colName')}</th>
                  <th className="px-3.5 py-2.5 text-left font-medium">{t('sso:idp.colSubject')}</th>
                  <th className="px-3.5 py-2.5 text-left font-medium">{t('sso:idp.colJIT')}</th>
                  <th className="px-3.5 py-2.5 text-left font-medium">{t('sso:idp.colState')}</th>
                  <th className="px-3.5 py-2.5" />
                </tr>
              </thead>
              <tbody>
                {d.items.map((x) => (
                  <tr key={x.id} className="border-t border-border hover:bg-muted">
                    <td className="px-3.5 py-2.5">
                      <b className="font-medium">{x.name}</b>
                      <span className="ml-2 font-mono text-[11px] text-muted-foreground">
                        {x.client_id}
                      </span>
                    </td>
                    <td className="px-3.5 py-2.5">
                      <span className="font-mono text-[12px]">{x.subject_claim}</span>
                      {/* email 当稳定标识是最贵的那个错：人改邮箱就变成另一个人 */}
                      {x.subject_claim === 'email' ? (
                        <span className="ml-2 rounded-[var(--radius-sm)] bg-warning-bg px-1.5 py-0.5 text-[10px] text-warning">
                          {t('sso:idp.emailSubjectWarn')}
                        </span>
                      ) : null}
                    </td>
                    <td className="px-3.5 py-2.5 text-muted-foreground">
                      {x.jit_create ? t('sso:idp.jitOn') : t('sso:idp.jitOff')}
                    </td>
                    <td className="px-3.5 py-2.5">
                      <span
                        className={[
                          'rounded-[var(--radius-sm)] px-2 py-0.5 text-[11px]',
                          x.enabled ? 'bg-info-bg text-info' : 'bg-muted text-muted-foreground',
                        ].join(' ')}
                      >
                        {/* 有意不用绿色：绿色 = 已验证可用，而这里只是"开着" */}
                        {x.enabled ? t('sso:idp.enabled') : t('sso:idp.disabled')}
                      </span>
                      {!x.has_secret ? (
                        <span className="ml-1.5 rounded-[var(--radius-sm)] bg-danger-bg px-1.5 py-0.5 text-[10px] text-danger">
                          {t('sso:idp.noSecret')}
                        </span>
                      ) : null}
                    </td>
                    <td className="px-3.5 py-2.5 text-right">
                      <button
                        type="button"
                        onClick={() => setEditing(x)}
                        className="cursor-pointer rounded-[var(--radius)] border border-border px-2 py-1 text-[11px] hover:bg-secondary"
                      >
                        {t('sso:idp.edit')}
                      </button>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          )}
        </AsyncBoundary>
      </div>
    </>
  )
}

function IdPForm({ cfg, onDone }: { cfg: IdPConfig | null; onDone: () => void }) {
  const { t } = useTranslation()
  const qc = useQueryClient()
  const [f, setF] = useState({
    name: cfg?.name ?? '',
    issuer: cfg?.issuer ?? '',
    client_id: cfg?.client_id ?? '',
    client_secret: '',
    auth_url: cfg?.auth_url ?? '',
    token_url: cfg?.token_url ?? '',
    jwks_url: cfg?.jwks_url ?? '',
    redirect_uri: cfg?.redirect_uri ?? '',
    scopes: cfg?.scopes ?? 'openid profile email',
    subject_claim: cfg?.subject_claim ?? 'sub',
    name_claim: cfg?.name_claim ?? 'name',
    email_claim: cfg?.email_claim ?? 'email',
    groups_claim: cfg?.groups_claim ?? '',
    jit_create: cfg?.jit_create ?? false,
    jit_groups: cfg?.jit_groups ?? '',
    enabled: cfg?.enabled ?? false,
  })
  const [err, setErr] = useState<string | null>(null)
  const set = (k: keyof typeof f, v: string | boolean) => setF((s) => ({ ...s, [k]: v }))

  const applyPreset = (p: (typeof PRESETS)[number]) =>
    setF((s) => ({
      ...s,
      issuer: p.issuer || s.issuer,
      scopes: p.scopes,
      subject_claim: p.subject,
      name_claim: p.name,
      email_claim: p.email,
      groups_claim: p.groups,
    }))

  const discover = useMutation({
    mutationFn: () =>
      api.post<{ auth_url: string; token_url: string; jwks_url: string; issuer: string }>(
        '/idp-configs/discover',
        { issuer: f.issuer },
      ),
    onSuccess: (r) => {
      setErr(null)
      setF((s) => ({
        ...s,
        auth_url: r.auth_url,
        token_url: r.token_url,
        jwks_url: r.jwks_url,
        issuer: r.issuer || s.issuer,
      }))
    },
    onError: (e) => setErr(msg(e, t)),
  })

  const save = useMutation({
    mutationFn: () => (cfg ? api.put(`/idp-configs/${cfg.id}`, f) : api.post('/idp-configs', f)),
    onSuccess: () => {
      setErr(null)
      void qc.invalidateQueries({ queryKey: ['idp-configs'] })
      onDone()
    },
    onError: (e) => setErr(msg(e, t)),
  })

  return (
    <div className="mb-4 rounded-[var(--radius-md)] border border-border bg-card p-4">
      <h2 className="text-sm font-semibold">
        {cfg ? t('sso:idp.editTitle', { name: cfg.name }) : t('sso:idp.newTitle')}
      </h2>

      <div className="mt-3 flex flex-wrap items-center gap-2">
        <span className="text-[12px] text-muted-foreground">{t('sso:idp.preset')}</span>
        {PRESETS.map((p) => (
          <button
            key={p.key}
            type="button"
            onClick={() => applyPreset(p)}
            className="cursor-pointer rounded-[var(--radius)] border border-border px-2.5 py-1 text-[12px] hover:bg-secondary"
          >
            {t(`sso:idp.preset_${p.key}`)}
          </button>
        ))}
        <span className="text-[11px] text-muted-foreground">{t('sso:idp.presetHint')}</span>
      </div>

      <div className="mt-3 grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
        <F label={t('sso:idp.fName')} v={f.name} on={(v) => set('name', v)} />
        <F label={t('sso:idp.fClientID')} v={f.client_id} on={(v) => set('client_id', v)} />
        <F
          label={t('sso:idp.fSecret')}
          v={f.client_secret}
          on={(v) => set('client_secret', v)}
          type="password"
          hint={cfg ? t('sso:idp.fSecretKeep') : t('sso:idp.fSecretNew')}
        />
      </div>

      <div className="mt-3 flex flex-wrap items-end gap-2">
        <div className="min-w-[18rem] flex-1">
          <F label={t('sso:idp.fIssuer')} v={f.issuer} on={(v) => set('issuer', v)} />
        </div>
        <button
          type="button"
          disabled={!f.issuer || discover.isPending}
          onClick={() => discover.mutate()}
          className="cursor-pointer rounded-[var(--radius)] border border-border px-3 py-1.5 text-[13px] hover:bg-secondary disabled:opacity-50"
        >
          {discover.isPending ? t('sso:idp.discovering') : t('sso:idp.discover')}
        </button>
        <span className="text-[11px] text-muted-foreground">{t('sso:idp.discoverHint')}</span>
      </div>

      <div className="mt-3 grid gap-3 lg:grid-cols-3">
        <F label="authorization_endpoint" v={f.auth_url} on={(v) => set('auth_url', v)} mono />
        <F label="token_endpoint" v={f.token_url} on={(v) => set('token_url', v)} mono />
        <F label="jwks_uri" v={f.jwks_url} on={(v) => set('jwks_url', v)} mono />
      </div>

      <div className="mt-3 grid gap-3 sm:grid-cols-2 lg:grid-cols-4">
        <F
          label={t('sso:idp.fSubject')}
          v={f.subject_claim}
          on={(v) => set('subject_claim', v)}
          mono
          hint={t('sso:idp.fSubjectHint')}
        />
        <F label={t('sso:idp.fNameClaim')} v={f.name_claim} on={(v) => set('name_claim', v)} mono />
        <F
          label={t('sso:idp.fEmailClaim')}
          v={f.email_claim}
          on={(v) => set('email_claim', v)}
          mono
        />
        <F
          label={t('sso:idp.fGroupsClaim')}
          v={f.groups_claim}
          on={(v) => set('groups_claim', v)}
          mono
        />
      </div>

      <div className="mt-3 grid gap-3 sm:grid-cols-2">
        <F
          label={t('sso:idp.fRedirect')}
          v={f.redirect_uri}
          on={(v) => set('redirect_uri', v)}
          mono
          hint={t('sso:idp.fRedirectHint')}
        />
        <F label={t('sso:idp.fScopes')} v={f.scopes} on={(v) => set('scopes', v)} mono />
      </div>

      <label className="mt-3 flex cursor-pointer items-start gap-2 text-[12px]">
        <input
          type="checkbox"
          checked={f.jit_create}
          onChange={(e) => set('jit_create', e.target.checked)}
          className="mt-0.5"
        />
        <span>
          <b className="font-medium">{t('sso:idp.fJIT')}</b>
          {/* JIT 的代价必须写在勾选框旁边，不能藏在文档里 */}
          <span className="block text-muted-foreground">{t('sso:idp.fJITHint')}</span>
        </span>
      </label>

      <label className="mt-2 flex cursor-pointer items-center gap-2 text-[12px]">
        <input
          type="checkbox"
          checked={f.enabled}
          onChange={(e) => set('enabled', e.target.checked)}
        />
        {t('sso:idp.fEnabled')}
      </label>

      {err ? (
        <p className="mt-3 rounded-[var(--radius)] bg-danger-bg px-3 py-2 text-[12px] text-danger">
          {err}
        </p>
      ) : null}

      <div className="mt-4 flex gap-2">
        <button
          type="button"
          disabled={save.isPending}
          onClick={() => save.mutate()}
          className="cursor-pointer rounded-[var(--radius)] bg-brand px-3 py-1.5 text-[13px] font-medium text-brand-foreground disabled:opacity-50"
        >
          {save.isPending ? t('sso:idp.saving') : t('sso:idp.save')}
        </button>
        <button
          type="button"
          onClick={onDone}
          className="cursor-pointer rounded-[var(--radius)] border border-border px-3 py-1.5 text-[13px] hover:bg-secondary"
        >
          {t('sso:idp.cancel')}
        </button>
      </div>
    </div>
  )
}

function msg(e: unknown, t: (k: string, o?: Record<string, unknown>) => string): string {
  return e instanceof ApiError
    ? t(`errors.${e.code}`, { ...e.params, defaultValue: e.code })
    : t('error.unreachable')
}

function F({
  label,
  v,
  on,
  hint,
  mono,
  type,
}: {
  label: string
  v: string
  on: (v: string) => void
  hint?: string
  mono?: boolean
  type?: string
}) {
  return (
    <label className="block text-[12px]">
      <span className="mb-1 block text-muted-foreground">{label}</span>
      {type === 'password' ? (
        <Pwd
          value={v}
          onChange={(e) => on(e.target.value)}
          className={`border-border ${mono ? 'font-mono text-[12px]' : ''}`}
        />
      ) : (
        <input
          type={type ?? 'text'}
          value={v}
          onChange={(e) => on(e.target.value)}
          className={`w-full rounded-[var(--radius)] border border-border bg-background px-2.5 py-1.5 ${
            mono ? 'font-mono text-[12px]' : 'text-[13px]'
          }`}
        />
      )}
      {hint ? <span className="mt-1 block text-[11px] text-muted-foreground">{hint}</span> : null}
    </label>
  )
}
