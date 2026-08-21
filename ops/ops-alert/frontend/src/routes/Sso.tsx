import { useTranslation } from '@ops/i18n'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { AsyncBoundary, Banner, Button, Select, Skeleton, TextInput, cn, fromQuery } from '@ops/ui'
import { Check, Copy, X } from 'lucide-react'
import { useEffect, useState } from 'react'
import { WriteButton } from '../components/WriteButton.js'
import { get, post, put, makeLoadError } from '../lib/api.js'

type Config = {
  enabled: boolean
  display_name: string
  issuer: string
  auto_discover: boolean
  auth_url: string
  token_url: string
  userinfo_url: string
  jwks_url: string
  client_id: string
  /** 只说配没配过。⚠️ 后端从不回原文 */
  client_secret_set: boolean
  scopes: string
  username_claim: string
  email_claim: string
  name_claim: string
  groups_claim: string
  claim_source: string
  jit_enabled: boolean
  default_role: string
  role_mapping: Record<string, string> | null
  redirect_uri: string
}
type TestResult = {
  ok: boolean
  steps: { step: string; ok: boolean; detail: string }[]
  redirect_uri: string
  caveat: string
}

/**
 * 单点登录配置。
 *
 * 各家子公司用什么身份源事先不知道（Azure AD / Keycloak / Okta / 自研），
 * 所以端点、claim 名、scope 全部可配。代价是配置项多 ——
 * 用「自动发现」和「测试连通性」两件事把这个代价压回去。
 */
export function SsoPage() {
  const { t } = useTranslation()
  const qc = useQueryClient()
  const q = useQuery({ queryKey: ['sso'], queryFn: () => get<Config>('/sso') })

  const [f, setF] = useState<Config | null>(null)
  const [secret, setSecret] = useState('')
  const [mappingText, setMappingText] = useState('')
  useEffect(() => {
    if (!q.data) return
    setF(q.data)
    setMappingText(
      Object.entries(q.data.role_mapping ?? {})
        .map(([g, r]) => `${g}=${r}`)
        .join('\n'),
    )
  }, [q.data])

  const save = useMutation({
    mutationFn: () => {
      const mapping: Record<string, string> = {}
      for (const line of mappingText.split('\n')) {
        const i = line.indexOf('=')
        if (i <= 0) continue
        const g = line.slice(0, i).trim()
        const r = line.slice(i + 1).trim()
        if (g && r) mapping[g] = r
      }
      // ⚠️ 密钥留空 = 不改。这样改别的字段时不用重输一遍 ——
      // "每次保存都要重填密钥"必然导致有人把它清空
      return put('/sso', { ...f, client_secret: secret, role_mapping: mapping })
    },
    onSuccess: () => {
      setSecret('')
      qc.invalidateQueries({ queryKey: ['sso'] })
    },
  })
  const test = useMutation({ mutationFn: () => post<TestResult>('/sso/test') })

  return (
    <div className="flex flex-col gap-3">
      <AsyncBoundary
        state={fromQuery(q, () => false, makeLoadError(t))}
        pending={<Skeleton className="h-96 w-full" />}
        empty={null}
        errorTitle={t('opsalert:sso.loadError')}
        retryLabel={t('action.retry')}
        onRetry={() => q.refetch()}
      >
        {() =>
          f && (
            <>
              <Banner tone="info">{t('opsalert:sso.intro')}</Banner>

              <Panel title={t('opsalert:sso.callback')}>
                {/* 🔴 这一串要一字不差地填进 IdP 的应用配置。
                    差一个字符（少个端口、http 写成 https）就被拒，
                    而 IdP 只回一句 "redirect_uri mismatch"，不告诉你差在哪 */}
                <p className="mb-2 text-2xs text-muted-foreground">{t('opsalert:sso.callbackHint')}</p>
                <CopyRow value={f.redirect_uri} />
              </Panel>

              <Panel title={t('opsalert:sso.basic')}>
                <Toggle
                  label={t('opsalert:sso.enable')}
                  checked={f.enabled}
                  onChange={(v) => setF({ ...f, enabled: v })}
                />
                <Row label={t('opsalert:sso.displayName')} hint={t('opsalert:sso.displayNameHint')}>
                  <TextInput value={f.display_name} onChange={(e) => setF({ ...f, display_name: e.target.value })} />
                </Row>
                <Row label={t('opsalert:sso.issuer')}>
                  <TextInput value={f.issuer} onChange={(e) => setF({ ...f, issuer: e.target.value })} />
                </Row>
                <Toggle
                  label={t('opsalert:sso.autoDiscover')}
                  hint={t('opsalert:sso.autoDiscoverHint')}
                  checked={f.auto_discover}
                  onChange={(v) => setF({ ...f, auto_discover: v })}
                />
                {!f.auto_discover && (
                  <>
                    <Row label={t('opsalert:sso.authUrl')}>
                      <TextInput value={f.auth_url} onChange={(e) => setF({ ...f, auth_url: e.target.value })} />
                    </Row>
                    <Row label={t('opsalert:sso.tokenUrl')}>
                      <TextInput value={f.token_url} onChange={(e) => setF({ ...f, token_url: e.target.value })} />
                    </Row>
                    <Row label={t('opsalert:sso.userinfoUrl')}>
                      <TextInput value={f.userinfo_url} onChange={(e) => setF({ ...f, userinfo_url: e.target.value })} />
                    </Row>
                  </>
                )}
                <Row label={t('opsalert:sso.clientId')}>
                  <TextInput value={f.client_id} onChange={(e) => setF({ ...f, client_id: e.target.value })} />
                </Row>
                <Row
                  label={t('opsalert:sso.clientSecret')}
                  hint={
                    f.client_secret_set
                      ? t('opsalert:sso.secretSetHint')
                      : t('opsalert:sso.secretUnsetHint')
                  }
                >
                  <TextInput
                    type="password"
                    value={secret}
                    onChange={(e) => setSecret(e.target.value)}
                    placeholder={f.client_secret_set ? t('opsalert:sso.secretKeep') : ''}
                  />
                </Row>
                <Row label={t('opsalert:sso.scopes')}>
                  <TextInput value={f.scopes} onChange={(e) => setF({ ...f, scopes: e.target.value })} />
                </Row>
              </Panel>

              <Panel title={t('opsalert:sso.claims')}>
                {/* 🔴 claim 来源是最容易踩的一项。默认「两处都取」，
                    因为有的 IdP 把 name/email 只放在 userinfo 里 */}
                <Row label={t('opsalert:sso.claimSource')} hint={t('opsalert:sso.claimSourceHint')}>
                  <Select
                    label={t('opsalert:sso.claimSource')}
                    value={f.claim_source}
                    onChange={(v) => setF({ ...f, claim_source: v })}
                    options={[
                      { value: 'both', label: t('opsalert:sso.srcBoth') },
                      { value: 'id_token', label: t('opsalert:sso.srcIdToken') },
                      { value: 'userinfo', label: t('opsalert:sso.srcUserinfo') },
                    ]}
                  />
                </Row>
                <Row label={t('opsalert:sso.usernameClaim')} hint={t('opsalert:sso.usernameClaimHint')}>
                  <TextInput value={f.username_claim} onChange={(e) => setF({ ...f, username_claim: e.target.value })} />
                </Row>
                <Row label={t('opsalert:sso.emailClaim')}>
                  <TextInput value={f.email_claim} onChange={(e) => setF({ ...f, email_claim: e.target.value })} />
                </Row>
                <Row label={t('opsalert:sso.nameClaim')}>
                  <TextInput value={f.name_claim} onChange={(e) => setF({ ...f, name_claim: e.target.value })} />
                </Row>
                <Row label={t('opsalert:sso.groupsClaim')} hint={t('opsalert:sso.groupsClaimHint')}>
                  <TextInput value={f.groups_claim} onChange={(e) => setF({ ...f, groups_claim: e.target.value })} />
                </Row>
              </Panel>

              <Panel title={t('opsalert:sso.provisioning')}>
                <Toggle
                  label={t('opsalert:sso.jit')}
                  hint={t('opsalert:sso.jitHint')}
                  checked={f.jit_enabled}
                  onChange={(v) => setF({ ...f, jit_enabled: v })}
                />
                <Row label={t('opsalert:sso.defaultRole')} hint={t('opsalert:sso.defaultRoleHint')}>
                  <Select
                    label={t('opsalert:sso.defaultRole')}
                    value={f.default_role}
                    onChange={(v) => setF({ ...f, default_role: v })}
                    options={[
                      { value: 'viewer', label: 'viewer' },
                      { value: 'oncall', label: 'oncall' },
                      { value: 'rule_admin', label: 'rule_admin' },
                    ]}
                  />
                </Row>
                <Row label={t('opsalert:sso.roleMapping')} hint={t('opsalert:sso.roleMappingHint')}>
                  <textarea
                    value={mappingText}
                    onChange={(e) => setMappingText(e.target.value)}
                    rows={4}
                    placeholder={'ops-admin=rule_admin\nsre=oncall'}
                    className="w-full rounded-md border border-border bg-card px-2.5 py-2 font-mono text-[11px] outline-none focus:border-primary"
                  />
                </Row>
              </Panel>

              <div className="flex flex-wrap items-center gap-2">
                <WriteButton perm="alert:manage_sso" loading={save.isPending} onClick={() => save.mutate()}>
                  {t('action.save')}
                </WriteButton>
                <Button variant="ghost" loading={test.isPending} onClick={() => test.mutate()}>
                  {t('opsalert:sso.test')}
                </Button>
              </div>
              {save.isError && <Banner tone="bad">{String((save.error as Error).message)}</Banner>}
              {save.isSuccess && <Banner tone="info">{t('opsalert:sso.saved')}</Banner>}

              {test.data && (
                <Panel title={t('opsalert:sso.testResult')}>
                  <ul className="flex flex-col gap-1.5">
                    {test.data.steps.map((st, i) => (
                      <li key={i} className="flex items-start gap-2 text-xs">
                        {st.ok ? (
                          <Check className="mt-0.5 size-3.5 shrink-0 text-success" />
                        ) : (
                          <X className="mt-0.5 size-3.5 shrink-0 text-danger" />
                        )}
                        <span className="font-medium">{st.step}</span>
                        <span className="min-w-0 flex-1 break-all text-muted-foreground">{st.detail}</span>
                      </li>
                    ))}
                  </ul>
                  {/* ⚠️ 必须说清这次测试**没有**验什么。
                      不说的话"测试通过"会被当成"配好了" */}
                  <p className="mt-2.5 text-2xs text-muted-foreground">{test.data.caveat}</p>
                </Panel>
              )}
              {test.isError && <Banner tone="bad">{String((test.error as Error).message)}</Banner>}
            </>
          )
        }
      </AsyncBoundary>
    </div>
  )
}

function CopyRow({ value }: { value: string }) {
  const { t } = useTranslation()
  const [copied, setCopied] = useState(false)
  return (
    <div className="flex items-center gap-2">
      <code className="min-w-0 flex-1 select-all break-all rounded border border-border bg-muted/40 px-2 py-1.5 font-mono text-[11px]">
        {value}
      </code>
      <Button
        size="sm"
        variant="ghost"
        onClick={() => {
          navigator.clipboard.writeText(value)
          setCopied(true)
          setTimeout(() => setCopied(false), 1500)
        }}
      >
        {copied ? <Check className="size-3.5" /> : <Copy className="size-3.5" />}
        {copied ? t('opsalert:license.copied') : t('opsalert:license.copy')}
      </Button>
    </div>
  )
}

function Toggle({
  label,
  hint,
  checked,
  onChange,
}: {
  label: string
  hint?: string
  checked: boolean
  onChange: (v: boolean) => void
}) {
  return (
    <div className="flex flex-col gap-0.5">
      <label className="flex items-center gap-2 text-sm">
        <input
          type="checkbox"
          checked={checked}
          onChange={(e) => onChange(e.target.checked)}
          className="size-4 cursor-pointer accent-[var(--color-primary)]"
        />
        {label}
      </label>
      {hint && <span className="ml-6 text-2xs text-muted-foreground">{hint}</span>}
    </div>
  )
}

function Row({ label, hint, children }: { label: string; hint?: string; children: React.ReactNode }) {
  return (
    <label className="flex flex-col gap-1">
      <span className="text-2xs text-muted-foreground">{label}</span>
      {children}
      {hint && <span className="text-2xs text-muted-foreground">{hint}</span>}
    </label>
  )
}

function Panel({ title, children }: { title: string; children: React.ReactNode }) {
  return (
    <section className={cn('rounded-lg border border-border bg-card')}>
      <h3 className="border-b border-border px-3.5 py-2 text-xs font-semibold">{title}</h3>
      <div className="flex flex-col gap-2.5 px-3.5 py-3">{children}</div>
    </section>
  )
}
