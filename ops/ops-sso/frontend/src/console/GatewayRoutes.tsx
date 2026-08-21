import { useTranslation } from '@ops/i18n'
import { AsyncBoundary, EmptyState, Skeleton, fromQuery } from '@ops/ui'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Network } from 'lucide-react'
import { useMemo, useState } from 'react'
import { ApiError, api } from '../api/client.js'
import type { App, ListOf } from '../api/types.js'
import { makeToLoadError } from '../shared/errorText.js'

interface Route {
  id: number
  app_id: number
  host: string
  upstream: string
  inject_mode: string
  app_name: string
  connect_type: string
}

/**
 * 网关路由：哪个域名的流量转到哪个后端，按哪个应用判权限。
 *
 * # 为什么这一屏必须有
 *
 * 它是**零改造接入绕不过去的一步**。在它之前只能直接写库 ——
 * 接一个老系统的流程是「界面里建应用 → 去连数据库插一行 → 再回界面配策略」，
 * 中间那步一出现，整条路就不是产品了。
 *
 * # 改动最多 30 秒生效
 *
 * 网关每 30s 重载一次路由表（热加载，改配置不重启 ——
 * 重启网关等于全公司瞬断）。这句话必须写在界面上：
 * 不写的话，人保存完立刻去试、发现还是旧的，会以为没保存成功再改一遍。
 */
export function GatewayRoutes() {
  const { t } = useTranslation()
  const toLoadError = useMemo(() => makeToLoadError(t), [t])
  const qc = useQueryClient()
  const [editing, setEditing] = useState<Route | 'new' | null>(null)
  const [err, setErr] = useState<string | null>(null)

  const q = useQuery({
    queryKey: ['app-routes'],
    queryFn: () => api.get<ListOf<Route>>('/app-routes'),
  })
  const state = fromQuery<ListOf<Route>>(q, (d) => d.items.length === 0, toLoadError)

  const del = useMutation({
    mutationFn: (id: number) => api.del(`/app-routes/${id}`),
    onSuccess: () => {
      setErr(null)
      void qc.invalidateQueries({ queryKey: ['app-routes'] })
    },
    onError: (e) =>
      setErr(
        e instanceof ApiError
          ? t(`errors.${e.code}`, { ...e.params, defaultValue: e.code })
          : t('error.unreachable'),
      ),
  })

  return (
    <section className="mt-4 rounded-[var(--radius-md)] border border-border bg-card p-4">
      <h2 className="text-sm font-semibold">{t('sso:routes.title')}</h2>
      <p className="mt-1 mb-3 max-w-[80ch] text-[12px] text-muted-foreground">
        {t('sso:routes.desc')}
      </p>

      {editing ? (
        <RouteForm
          route={editing === 'new' ? null : editing}
          onDone={() => {
            setEditing(null)
            void q.refetch()
          }}
        />
      ) : (
        <button
          type="button"
          onClick={() => setEditing('new')}
          className="mb-3 cursor-pointer rounded-[var(--radius)] border border-border px-2.5 py-1 text-[12px] hover:bg-secondary"
        >
          {t('sso:routes.addBtn')}
        </button>
      )}

      {err ? (
        <p className="mb-3 rounded-[var(--radius)] bg-danger-bg px-3 py-2 text-[12px] text-danger">
          {err}
        </p>
      ) : null}

      <div className="overflow-hidden rounded-[var(--radius)] border border-border">
        <AsyncBoundary
          state={state}
          errorTitle={t('sso:routes.errorTitle')}
          retryLabel={t('retry')}
          onRetry={() => void q.refetch()}
          pending={<Skeleton className="m-3 h-14" />}
          empty={
            <EmptyState
              icon={<Network />}
              title={t('sso:routes.empty.title')}
              reason={t('sso:routes.empty.reason')}
              action={null}
            />
          }
        >
          {(d) => (
            <div className="overflow-x-auto">
              <table className="w-full min-w-[680px] text-[13px]">
                <thead>
                  <tr className="bg-muted text-xs text-muted-foreground">
                    <th className="px-3.5 py-2 text-left font-medium">{t('sso:routes.colHost')}</th>
                    <th className="px-3.5 py-2 text-left font-medium">
                      {t('sso:routes.colUpstream')}
                    </th>
                    <th className="px-3.5 py-2 text-left font-medium">{t('sso:routes.colApp')}</th>
                    <th className="px-3.5 py-2" />
                  </tr>
                </thead>
                <tbody>
                  {d.items.map((r) => (
                    <tr key={r.id} className="border-t border-border hover:bg-muted">
                      <td className="px-3.5 py-2 font-mono text-[12px]">{r.host}</td>
                      <td className="px-3.5 py-2 font-mono text-[12px] text-muted-foreground">
                        {r.upstream}
                      </td>
                      <td className="px-3.5 py-2">
                        {r.app_name}
                        {/* 应用不是网关接入方式的话，这条路由虽然存在，
                            但流量根本不会走到网关 —— 配了等于没配 */}
                        {r.connect_type !== 'gateway' && r.connect_type !== 'formfill' ? (
                          <span className="ml-2 rounded-[var(--radius-sm)] bg-warning-bg px-1.5 py-0.5 text-[10px] text-warning">
                            {t('sso:routes.notGatewayApp', { type: r.connect_type })}
                          </span>
                        ) : null}
                      </td>
                      <td className="px-3.5 py-2 text-right whitespace-nowrap">
                        <button
                          type="button"
                          onClick={() => setEditing(r)}
                          className="mr-1.5 cursor-pointer rounded-[var(--radius)] border border-border px-2 py-1 text-[11px] hover:bg-secondary"
                        >
                          {t('sso:routes.edit')}
                        </button>
                        <button
                          type="button"
                          disabled={del.isPending}
                          onClick={() => del.mutate(r.id)}
                          title={t('sso:routes.deleteWarn')}
                          className="cursor-pointer rounded-[var(--radius)] border border-border px-2 py-1 text-[11px] hover:bg-secondary disabled:opacity-40"
                        >
                          {t('sso:routes.delete')}
                        </button>
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

function RouteForm({ route, onDone }: { route: Route | null; onDone: () => void }) {
  const { t } = useTranslation()
  const qc = useQueryClient()
  const [appID, setAppID] = useState(route ? String(route.app_id) : '')
  const [host, setHost] = useState(route?.host ?? '')
  const [upstream, setUpstream] = useState(route?.upstream ?? '')
  const [err, setErr] = useState<string | null>(null)

  const apps = useQuery({ queryKey: ['apps'], queryFn: () => api.get<ListOf<App>>('/apps') })

  const m = useMutation({
    mutationFn: () => {
      const body = { app_id: Number(appID), host: host.trim(), upstream: upstream.trim() }
      return route ? api.put(`/app-routes/${route.id}`, body) : api.post('/app-routes', body)
    },
    onSuccess: () => {
      setErr(null)
      void qc.invalidateQueries({ queryKey: ['app-routes'] })
      onDone()
    },
    onError: (e) =>
      setErr(
        e instanceof ApiError
          ? t(`errors.${e.code}`, { ...e.params, defaultValue: e.code })
          : t('error.unreachable'),
      ),
  })

  return (
    <div className="mb-3 rounded-[var(--radius)] border border-border bg-background p-3">
      <div className="grid gap-3 sm:grid-cols-3">
        <label className="block text-[12px]">
          <span className="mb-1 block text-muted-foreground">{t('sso:routes.fHost')}</span>
          <input
            value={host}
            onChange={(e) => setHost(e.target.value.toLowerCase())}
            placeholder="grafana.example.com"
            className={inputCls}
          />
          <span className="mt-1 block text-[11px] text-muted-foreground">
            {t('sso:routes.fHostHint')}
          </span>
        </label>
        <label className="block text-[12px]">
          <span className="mb-1 block text-muted-foreground">{t('sso:routes.fUpstream')}</span>
          <input
            value={upstream}
            onChange={(e) => setUpstream(e.target.value)}
            placeholder="http://10.42.6.31:3000"
            className={inputCls}
          />
          <span className="mt-1 block text-[11px] text-muted-foreground">
            {t('sso:routes.fUpstreamHint')}
          </span>
        </label>
        <label className="block text-[12px]">
          <span className="mb-1 block text-muted-foreground">{t('sso:routes.fApp')}</span>
          <select value={appID} onChange={(e) => setAppID(e.target.value)} className={inputCls}>
            <option value="">{t('sso:routes.pickApp')}</option>
            {(apps.data?.items ?? []).map((a) => (
              <option key={a.id} value={a.id}>
                {a.name}（{a.env}）
              </option>
            ))}
          </select>
          <span className="mt-1 block text-[11px] text-muted-foreground">
            {t('sso:routes.fAppHint')}
          </span>
        </label>
      </div>

      {err ? (
        <p className="mt-2 rounded-[var(--radius)] bg-danger-bg px-2.5 py-1.5 text-[12px] text-danger">
          {err}
        </p>
      ) : null}

      <div className="mt-3 flex items-center gap-2">
        <button
          type="button"
          disabled={!appID || host.trim() === '' || upstream.trim() === '' || m.isPending}
          onClick={() => m.mutate()}
          className="cursor-pointer rounded-[var(--radius)] bg-brand px-3 py-1.5 text-[13px] font-medium text-brand-foreground disabled:cursor-default disabled:opacity-50"
        >
          {m.isPending ? t('sso:routes.saving') : t('sso:routes.save')}
        </button>
        <button
          type="button"
          onClick={onDone}
          className="cursor-pointer rounded-[var(--radius)] border border-border px-3 py-1.5 text-[13px] hover:bg-secondary"
        >
          {t('sso:routes.cancel')}
        </button>
        {/* 生效延迟必须说：不说的话人会以为没保存成功，再改一遍 */}
        <span className="text-[11px] text-muted-foreground">{t('sso:routes.reloadHint')}</span>
      </div>
    </div>
  )
}

const inputCls =
  'w-full rounded-[var(--radius)] border border-border bg-background px-2.5 py-1.5 text-[13px]'
