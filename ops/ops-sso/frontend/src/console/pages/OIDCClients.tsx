import { useTranslation } from '@ops/i18n'
import { AsyncBoundary, EmptyState, Skeleton, fromQuery } from '@ops/ui'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Check, Copy, KeySquare } from 'lucide-react'
import { useMemo, useState } from 'react'
import { ApiError, api } from '../../api/client.js'
import type { App, ListOf } from '../../api/types.js'
import { makeToLoadError } from '../../shared/errorText.js'

interface OIDCClient {
  id: number
  app_id: number
  client_id: string
  redirect_uris: string[]
  post_logout_uris: string[]
  scopes: string[]
  public_client: boolean
  require_pkce: boolean
  claims: string[]
  id_token_ttl_sec: number
  enabled: boolean
}

interface ClientList extends ListOf<OIDCClient> {
  issuer: string
}

/**
 * OIDC 客户端。
 *
 * # 这一页在接入流程里的位置
 *
 * 「应用」页把系统登记进来，这一页给它一把**能换 token 的钥匙**。
 * 下游拿 client_id / client_secret / issuer 三样东西去配自己的 SSO 登录。
 *
 * # 为什么 secret 只显示一次
 *
 * 库里存的是哈希。这不是为了防外人 —— 是为了防「我们自己」：
 * 一个能被接口读回来的密钥，迟早会出现在某次导出、某张截图、某个日志里。
 * 生产上出过的两个 P0 都是接口把凭据发给了不该看的人。
 */
export function OIDCClientsPage() {
  const { t } = useTranslation()
  const toLoadError = useMemo(() => makeToLoadError(t), [t])
  const [adding, setAdding] = useState(false)

  const q = useQuery({
    queryKey: ['oidc-clients'],
    queryFn: () => api.get<ClientList>('/oidc-clients'),
  })
  const apps = useQuery({ queryKey: ['apps'], queryFn: () => api.get<ListOf<App>>('/apps') })
  const appName = (id: number) => apps.data?.items.find((a) => a.id === id)?.name ?? `#${id}`

  const state = fromQuery<ClientList>(q, (d) => d.items.length === 0, toLoadError)

  return (
    <>
      <h1 className="text-xl font-semibold tracking-tight">{t('sso:console.oidcClients')}</h1>
      <p className="mt-1 mb-4 max-w-[80ch] text-sm text-muted-foreground">{t('sso:oidc.intro')}</p>

      {q.data?.issuer ? <IssuerBox issuer={q.data.issuer} /> : null}

      {adding ? (
        <div className="mb-4">
          <NewClient onDone={() => setAdding(false)} />
        </div>
      ) : (
        <button
          type="button"
          onClick={() => setAdding(true)}
          className="mb-4 cursor-pointer rounded-[var(--radius)] bg-brand px-3 py-1.5 text-[13px] font-medium text-brand-foreground"
        >
          {t('sso:oidc.addBtn')}
        </button>
      )}

      <div className="overflow-hidden rounded-[var(--radius-md)] border border-border bg-card">
        <AsyncBoundary
          state={state}
          errorTitle={t('sso:oidc.errorTitle')}
          retryLabel={t('retry')}
          onRetry={() => void q.refetch()}
          pending={<Skeleton className="m-4 h-20" />}
          empty={
            <EmptyState
              icon={<KeySquare />}
              title={t('sso:oidc.empty.title')}
              reason={t('sso:oidc.empty.reason')}
              action={null}
            />
          }
        >
          {(d) => (
            <div className="overflow-x-auto">
              <table className="w-full min-w-[760px] text-[13px]">
                <thead>
                  <tr className="bg-muted text-xs text-muted-foreground">
                    <th className="px-3.5 py-2.5 text-left font-medium">{t('sso:oidc.colApp')}</th>
                    <th className="px-3.5 py-2.5 text-left font-medium">
                      {t('sso:oidc.colClientID')}
                    </th>
                    <th className="px-3.5 py-2.5 text-left font-medium">
                      {t('sso:oidc.colRedirect')}
                    </th>
                    <th className="px-3.5 py-2.5 text-left font-medium">{t('sso:oidc.colKind')}</th>
                  </tr>
                </thead>
                <tbody>
                  {d.items.map((c) => (
                    <tr key={c.id} className="border-t border-border hover:bg-muted">
                      <td className="px-3.5 py-2.5">{appName(c.app_id)}</td>
                      <td className="px-3.5 py-2.5 font-mono text-[12px]">{c.client_id}</td>
                      <td className="px-3.5 py-2.5">
                        {c.redirect_uris.length === 0 ? (
                          // 没有回调地址的客户端登不进来。空着看起来像"还没配"，
                          // 但它其实是坏的 —— 要标出来。
                          <span className="text-warning">{t('sso:oidc.noRedirect')}</span>
                        ) : (
                          <span className="font-mono text-[11px] text-muted-foreground">
                            {c.redirect_uris.join('  ')}
                          </span>
                        )}
                      </td>
                      <td className="px-3.5 py-2.5">
                        <span className="rounded-[var(--radius-sm)] bg-muted px-2 py-0.5 text-[11px] text-muted-foreground">
                          {c.public_client ? t('sso:oidc.publicClient') : t('sso:oidc.confidential')}
                        </span>
                        {!c.enabled ? (
                          <span className="ml-1.5 rounded-[var(--radius-sm)] bg-warning-bg px-2 py-0.5 text-[11px] text-warning">
                            {t('sso:oidc.disabled')}
                          </span>
                        ) : null}
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
        </AsyncBoundary>
      </div>
    </>
  )
}

/** 下游要填的三行，直接复制。 */
function IssuerBox({ issuer }: { issuer: string }) {
  const { t } = useTranslation()
  return (
    <div className="mb-4 rounded-[var(--radius-md)] border border-border bg-card p-4">
      <h2 className="text-sm font-semibold">{t('sso:oidc.issuerTitle')}</h2>
      <p className="mt-1 mb-2 text-[12px] text-muted-foreground">{t('sso:oidc.issuerDesc')}</p>
      <div className="grid gap-1.5">
        <CopyRow label="issuer" value={issuer} />
        <CopyRow label={t('sso:oidc.discovery')} value={`${issuer}/.well-known/openid-configuration`} />
        <CopyRow label={t('sso:oidc.jwks')} value={`${issuer}/oidc/jwks`} />
      </div>
    </div>
  )
}

function CopyRow({ label, value }: { label: string; value: string }) {
  const [copied, setCopied] = useState(false)
  return (
    <div className="flex items-center gap-2 text-[12px]">
      <span className="w-24 shrink-0 text-muted-foreground">{label}</span>
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
              /* 非 https 下浏览器会拒掉剪贴板。不弹错：值就在屏幕上 */
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

function NewClient({ onDone }: { onDone: () => void }) {
  const { t } = useTranslation()
  const qc = useQueryClient()
  const [appID, setAppID] = useState('')
  // client_id 不再让人填：填出来的一定是 harbor / prod 这种好猜的值，
  // 而 client_id 是公开的 —— 可枚举等于替攻击者省掉侦察那一步。
  // 建完由后端回填到这里显示。
  const [clientID, setClientID] = useState('')
  const [redirect, setRedirect] = useState('')
  const [publicClient, setPublic] = useState(false)
  const [err, setErr] = useState<string | null>(null)
  const [secret, setSecret] = useState<string | null>(null)

  const apps = useQuery({ queryKey: ['apps'], queryFn: () => api.get<ListOf<App>>('/apps') })

  const m = useMutation({
    mutationFn: () =>
      api.post<{ client_id: string; client_secret: string }>('/oidc-clients', {
        app_id: Number(appID),
        // 一行一个，空行去掉 —— 粘贴多行是最常见的输入方式
        redirect_uris: redirect
          .split('\n')
          .map((s) => s.trim())
          .filter(Boolean),
        public_client: publicClient,
      }),
    onSuccess: (r) => {
      setErr(null)
      setClientID(r.client_id)
      setSecret(r.client_secret)
      void qc.invalidateQueries({ queryKey: ['oidc-clients'] })
    },
    onError: (e) =>
      setErr(
        e instanceof ApiError
          ? t(`errors.${e.code}`, { ...e.params, defaultValue: e.code })
          : t('error.unreachable'),
      ),
  })

  if (secret) {
    return (
      <div className="rounded-[var(--radius-md)] border border-warning bg-warning-bg p-4">
        {/* 提示在密钥**上面**：人会先复制再读说明 */}
        <p className="text-[13px] font-medium">{t('sso:oidc.secretOnce')}</p>
        <div className="mt-2 grid gap-1.5">
          <CopyRow label="client_id" value={clientID} />
          <CopyRow label="client_secret" value={secret} />
        </div>
        <button
          type="button"
          onClick={() => {
            setSecret(null)
            onDone()
          }}
          className="mt-3 cursor-pointer rounded-[var(--radius)] border border-border px-3 py-1.5 text-[13px] hover:bg-secondary"
        >
          {t('sso:oidc.savedIt')}
        </button>
      </div>
    )
  }

  return (
    <div className="rounded-[var(--radius-md)] border border-border bg-card p-4">
      <h2 className="text-sm font-semibold">{t('sso:oidc.newTitle')}</h2>
      <div className="mt-3 grid gap-3 sm:grid-cols-2">
        <label className="block text-[12px]">
          <span className="mb-1 block text-muted-foreground">{t('sso:oidc.fApp')}</span>
          <select value={appID} onChange={(e) => setAppID(e.target.value)} className={inputCls}>
            <option value="">{t('sso:oidc.pickApp')}</option>
            {(apps.data?.items ?? []).map((a) => (
              <option key={a.id} value={a.id}>
                {a.name}（{a.env}）
              </option>
            ))}
          </select>
        </label>
        <div className="text-[12px]">
          <span className="mb-1 block text-muted-foreground">{t('sso:oidc.fClientID')}</span>
          <p className="rounded-[var(--radius)] border border-dashed border-border px-2.5 py-1.5 text-[12px] text-muted-foreground">
            {t('sso:oidc.clientIDAuto')}
          </p>
        </div>
      </div>

      <label className="mt-3 block text-[12px]">
        <span className="mb-1 block text-muted-foreground">{t('sso:oidc.fRedirect')}</span>
        <textarea
          value={redirect}
          onChange={(e) => setRedirect(e.target.value)}
          rows={3}
          placeholder={'https://cmdb.example.com/oidc/callback'}
          className={`${inputCls} resize-y font-mono text-[12px]`}
        />
        <span className="mt-1 block text-[11px] text-muted-foreground">
          {t('sso:oidc.fRedirectHint')}
        </span>
      </label>

      <label className="mt-3 flex cursor-pointer items-start gap-2 text-[12px]">
        <input
          type="checkbox"
          checked={publicClient}
          onChange={(e) => setPublic(e.target.checked)}
          className="mt-0.5"
        />
        <span>
          <b className="font-medium">{t('sso:oidc.fPublic')}</b>
          <span className="block text-muted-foreground">{t('sso:oidc.fPublicHint')}</span>
        </span>
      </label>

      {err ? (
        <p className="mt-3 rounded-[var(--radius)] bg-danger-bg px-3 py-2 text-[12px] text-danger">
          {err}
        </p>
      ) : null}

      <div className="mt-3 flex gap-2">
        <button
          type="button"
          disabled={!appID || redirect.trim() === '' || m.isPending}
          onClick={() => m.mutate()}
          className="cursor-pointer rounded-[var(--radius)] bg-brand px-3 py-1.5 text-[13px] font-medium text-brand-foreground disabled:cursor-default disabled:opacity-50"
        >
          {m.isPending ? t('sso:oidc.creating') : t('sso:oidc.create')}
        </button>
        <button
          type="button"
          onClick={onDone}
          className="cursor-pointer rounded-[var(--radius)] border border-border px-3 py-1.5 text-[13px] hover:bg-secondary"
        >
          {t('sso:oidc.cancel')}
        </button>
      </div>
    </div>
  )
}

const inputCls =
  'w-full rounded-[var(--radius)] border border-border bg-background px-2.5 py-1.5 text-[13px]'
