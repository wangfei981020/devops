import { useTranslation } from '@ops/i18n'
import { AsyncBoundary, Dialog, EmptyState, Skeleton, fromQuery } from '@ops/ui'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Boxes } from 'lucide-react'
import { useMemo, useState } from 'react'
import { ApiError, api } from '../../api/client.js'
import type { App, ListOf } from '../../api/types.js'
import { makeToLoadError } from '../../shared/errorText.js'
import { AppGroups } from '../AppGroups.js'
import { GatewayRoutes } from '../GatewayRoutes.js'
import { NewAppForm } from '../NewAppForm.js'
import { OnboardingPage } from './Onboarding.js'
import { ProviderInfo } from '../ProviderInfo.js'

/**
 * 接入指引挂在应用下面：接完一个应用，下一步就是照着它去对方系统里配。
 *
 * ⚠️ 必须**单独一层组件**来切换，不能在 AppsPage 里提前 return ——
 * 提前 return 会跳过后面的 useMemo/useQuery，两次渲染的 hook 数量不一致，
 * React 直接抛 #300。这类错误只在**运行时**出现，编译期完全看不出来。
 * （实测踩到：点「接入指引」整页进错误边界。）
 */
export function AppsPage() {
  const [guide, setGuide] = useState<number | null>(null)
  return guide === null ? (
    <AppsList onGuide={setGuide} />
  ) : (
    <OnboardingPage appID={guide} onBack={() => setGuide(null)} />
  )
}

function AppsList({ onGuide }: { onGuide: (id: number) => void }) {
  const { t } = useTranslation()
  const [adding, setAdding] = useState(false)
  const [editing, setEditing] = useState<App | null>(null)
  const [deleting, setDeleting] = useState<App | null>(null)
  const toLoadError = useMemo(() => makeToLoadError(t), [t])
  const q = useQuery({ queryKey: ['apps'], queryFn: () => api.get<ListOf<App>>('/apps') })
  const state = fromQuery<ListOf<App>>(q, (d) => d.total === 0, toLoadError)

  return (
    <>
      <h1 className="text-xl font-semibold tracking-tight">{t('sso:console.apps')}</h1>
      <p className="mt-1 mb-4 max-w-[80ch] text-sm text-muted-foreground">
        {t('sso:apps.intro')}
      </p>

      {editing ? (
        <div className="mb-4">
          <NewAppForm
            // key：从 A 切到 B 时必须重建表单。不加的话 React 复用同一个组件实例，
            // useState 的初值只在第一次渲染取，于是编辑 B 显示的是 A 的内容 ——
            // 而人不会怀疑表单，会以为是自己点错了行。
            key={editing.id}
            app={editing}
            onDone={() => {
              setEditing(null)
              void q.refetch()
            }}
          />
        </div>
      ) : adding ? (
        <div className="mb-4">
          <NewAppForm onDone={() => setAdding(false)} />
        </div>
      ) : (
        <button
          type="button"
          onClick={() => setAdding(true)}
          className="mb-4 cursor-pointer rounded-[var(--radius)] bg-brand px-3 py-1.5 text-[13px] font-medium text-brand-foreground"
        >
          {t('sso:apps.addBtn')}
        </button>
      )}

      {/* 接入信息放在应用列表**上面**：接第一个应用时就要用到它，
          而不是接完之后再去别处找。 */}
      <ProviderInfo />

      <div className="mb-4 overflow-hidden rounded-[var(--radius-md)] border border-border bg-card">
        <AsyncBoundary
          state={state}
          errorTitle={t('sso:apps.errorTitle')}
          retryLabel={t('retry')}
          onRetry={() => void q.refetch()}
          pending={
            <div className="space-y-2 p-4">
              {[0, 1, 2].map((i) => (
                <Skeleton key={i} className="h-12 w-full" />
              ))}
            </div>
          }
          empty={
            <EmptyState
              icon={<Boxes />}
              title={t('sso:apps.empty.title')}
              reason={t('sso:apps.empty.reason')}
              action={null}
            />
          }
        >
          {(data) => (
            <table className="w-full text-[13px]">
              <thead>
                <tr className="bg-muted text-xs text-muted-foreground">
                  <th className="px-3.5 py-2.5 text-left font-medium">{t('sso:apps.colApp')}</th>
                  <th className="px-3.5 py-2.5 text-left font-medium">{t('sso:apps.colConnect')}</th>
                  <th className="px-3.5 py-2.5 text-left font-medium">{t('sso:apps.colEnv')}</th>
                  <th className="px-3.5 py-2.5 text-left font-medium">{t('sso:apps.colDenied')}</th>
                  <th className="px-3.5 py-2.5" />
                </tr>
              </thead>
              <tbody>
                {data.items.map((a) => (
                  <tr key={a.id} className="border-t border-border hover:bg-muted">
                    <td className="px-3.5 py-2.5">
                      <div className="flex items-center gap-2.5">
                        <span
                          className="grid size-7 shrink-0 place-items-center rounded-[var(--radius-sm)] text-[11px] font-semibold text-primary-foreground"
                          style={{ background: a.icon_color }}
                        >
                          {a.icon_text}
                        </span>
                        <span>
                          <b className="block font-medium">{a.name}</b>
                          <span className="font-mono text-[11px] text-muted-foreground">
                            {a.code}
                          </span>
                        </span>
                      </div>
                    </td>
                    <td className="px-3.5 py-2.5">
                      <span className="rounded-[var(--radius-sm)] bg-info-bg px-2 py-0.5 text-[11px] text-info">
                        {a.connect_type}
                      </span>
                    </td>
                    <td className="px-3.5 py-2.5 text-muted-foreground">{a.env}</td>
                    <td className="px-3.5 py-2.5 text-[12px] text-muted-foreground">
                      {/* 无权限时是否在门户露出来，是个真实的策略选择：
                          藏起来更安全，但员工不知道有这个系统可以申请 */}
                      {a.show_when_denied ? t('sso:apps.showDenied') : t('sso:apps.hideDenied')}
                    </td>
                    <td className="px-3.5 py-2.5 text-right whitespace-nowrap">
                      <button
                        type="button"
                        onClick={() => onGuide(a.id)}
                        className={rowBtn}
                      >
                        {t('sso:apps.guide')}
                      </button>
                      <button
                        type="button"
                        onClick={() => {
                          setAdding(false)
                          setEditing(a)
                        }}
                        className={`ml-1.5 ${rowBtn}`}
                      >
                        {t('sso:apps.edit')}
                      </button>
                      <button
                        type="button"
                        onClick={() => setDeleting(a)}
                        className={`ml-1.5 ${rowBtn}`}
                      >
                        {t('sso:apps.delete')}
                      </button>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          )}
        </AsyncBoundary>
      </div>

      {deleting ? (
        <DeleteAppDialog
          app={deleting}
          onClose={() => setDeleting(null)}
          onDone={() => {
            setDeleting(null)
            void q.refetch()
          }}
        />
      ) : null}

      <GatewayRoutes />
      <AppGroups />
    </>
  )
}

const rowBtn =
  'cursor-pointer rounded-[var(--radius)] border border-border px-2 py-1 text-[11px] hover:bg-secondary'

interface AppDeps {
  oidc_clients: number
  routes: number
  path_rules: number
  policies: number
  groups: number
  credentials: number
  requests: number
}

/**
 * 删应用的二次确认。
 *
 * # 为什么不是一句「确定删除吗」
 *
 * 删一个应用不只是列表里少一行：它名下的授权规则、接口级策略、
 * **网关路由**会一起失效。路由尤其致命 —— 那是一个域名的入口，
 * 删了之后那个域名立刻没人接，而点删除的人未必知道它挂着路由。
 *
 * 所以先去后端把真实数量取回来摆在这里。写死一句"会一起删掉相关配置"
 * 等于没说：它既没告诉人删了几条，也没告诉人有没有路由。
 *
 * # 影响面取不回来时，不能装作没有
 *
 * 三态：查询中 / 查不到 / 查到了。查不到时**不显示 0**，
 * 而是明说"算不出影响面"并让删除按钮变成"仍要删除" ——
 * 把"没有挂件"和"不知道有没有挂件"渲染成同一个样子，是这套系统里
 * 反复强调不能犯的错。
 */
function DeleteAppDialog({
  app,
  onClose,
  onDone,
}: {
  app: App
  onClose: () => void
  onDone: () => void
}) {
  const { t } = useTranslation()
  const qc = useQueryClient()
  const [err, setErr] = useState<string | null>(null)

  const deps = useQuery({
    queryKey: ['app-deps', app.id],
    queryFn: () => api.get<AppDeps>(`/apps/${app.id}/dependents`),
    // 这个数字是给人做决定用的，必须是此刻的真值，不能吃缓存
    staleTime: 0,
    gcTime: 0,
  })

  const del = useMutation({
    mutationFn: () => api.del(`/apps/${app.id}`),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: ['apps'] })
      void qc.invalidateQueries({ queryKey: ['app-routes'] })
      void qc.invalidateQueries({ queryKey: ['oidc-clients'] })
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

  const d = deps.data
  const rows: Array<[string, number]> = d
    ? [
        ['oidcClients', d.oidc_clients],
        ['routes', d.routes],
        ['pathRules', d.path_rules],
        ['policies', d.policies],
        ['groups', d.groups],
        ['credentials', d.credentials],
      ]
    : []
  const total = rows.reduce((n, [, v]) => n + v, 0)

  return (
    <Dialog
      open
      onClose={onClose}
      title={t('sso:apps.delTitle', { name: app.name })}
      description={t('sso:apps.delDesc')}
      closeLabel={t('sso:apps.cancel')}
      width={520}
      footer={
        <div className="flex items-center gap-2">
          <button
            type="button"
            disabled={del.isPending}
            onClick={() => del.mutate()}
            className="cursor-pointer rounded-[var(--radius)] bg-destructive px-3 py-1.5 text-[13px] font-medium text-primary-foreground disabled:opacity-50"
          >
            {del.isPending
              ? t('sso:apps.deleting')
              : deps.isError
                ? t('sso:apps.delAnyway')
                : t('sso:apps.delConfirm')}
          </button>
          <button
            type="button"
            onClick={onClose}
            className="cursor-pointer rounded-[var(--radius)] border border-border px-3 py-1.5 text-[13px] hover:bg-secondary"
          >
            {t('sso:apps.cancel')}
          </button>
        </div>
      }
    >
      {deps.isPending ? (
        <Skeleton className="h-24 w-full" />
      ) : deps.isError ? (
        // ⚠️ 查不到影响面 ≠ 没有影响面。这里显示 0 会让人以为删了没事。
        <p className="rounded-[var(--radius)] bg-warning-bg px-3 py-2 text-[12px] text-warning">
          {t('sso:apps.delDepsUnavailable')}
        </p>
      ) : total === 0 ? (
        <p className="text-[12px] text-muted-foreground">{t('sso:apps.delNoDeps')}</p>
      ) : (
        <ul className="space-y-1 text-[12px]">
          {rows
            .filter(([, v]) => v > 0)
            .map(([k, v]) => (
              <li key={k} className="flex items-baseline gap-2">
                <span className="font-mono text-[13px] font-semibold">{v}</span>
                <span>{t(`sso:apps.dep.${k}`)}</span>
              </li>
            ))}
        </ul>
      )}

      {/* 路由单独再说一次：它是唯一一条"删了之后外面立刻有人访问不了"的 */}
      {d && d.routes > 0 ? (
        <p className="mt-2 rounded-[var(--radius)] bg-danger-bg px-3 py-2 text-[12px] text-danger">
          {t('sso:apps.delRouteWarn', { count: d.routes })}
        </p>
      ) : null}

      {/* 申请记录不删，也要说 —— 不说的话人会以为历史被抹掉了 */}
      {d && d.requests > 0 ? (
        <p className="mt-2 text-[11px] text-muted-foreground">
          {t('sso:apps.delKeepRequests', { count: d.requests })}
        </p>
      ) : null}

      {err ? (
        <p className="mt-2 rounded-[var(--radius)] bg-danger-bg px-3 py-2 text-[12px] text-danger">
          {err}
        </p>
      ) : null}
    </Dialog>
  )
}
