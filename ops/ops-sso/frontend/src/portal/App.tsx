import { useTranslation } from '@ops/i18n'
import { AsyncBoundary, EmptyState, Skeleton, fromQuery } from '@ops/ui'
import { useQuery } from '@tanstack/react-query'
import { type ReactNode, useMemo, useState } from 'react'
import { ShieldOff } from 'lucide-react'
import { api } from '../api/client.js'
import type { PortalSection } from '../api/types.js'
import { AuthGate } from '../shared/AuthGate.js'
import { makeToLoadError } from '../shared/errorText.js'
import { Shell } from '../shared/Shell.js'
import { MyGrants } from './pages/MyGrants.js'
import { MySessions } from './pages/MySessions.js'
import { MyRequests } from './pages/MyRequests.js'
import { Security } from './pages/Security.js'
import { Status } from './pages/Status.js'

const NAV = [
  { key: 'apps', ready: true },
  { key: 'myGrants', ready: true },
  { key: 'myRequests', ready: true },
  { key: 'mySessions', ready: true },
  { key: 'security', ready: true },
  { key: 'status', ready: true },
] as const

export function PortalApp() {
  const { t } = useTranslation()
  const [active, setActive] = useState<string>('apps')

  let page: ReactNode
  if (active === 'myGrants') page = <MyGrants />
  else if (active === 'myRequests') page = <MyRequests />
  else if (active === 'mySessions') page = <MySessions />
  else if (active === 'security') page = <Security />
  else if (active === 'status') page = <Status />
  else page = <MySystems onRequestAccess={() => setActive('myRequests')} />

  return (
    <AuthGate>
      <Shell
        portal
        nav={
          <nav className="ml-3 flex gap-0.5">
            {NAV.map((n) => (
              <button
                key={n.key}
                type="button"
                onClick={() => setActive(n.key)}
                aria-current={active === n.key ? 'page' : undefined}
                className={[
                  'cursor-pointer rounded-[var(--radius)] px-2.5 py-1.5 text-[13px] transition-colors duration-150',
                  active === n.key
                    ? 'bg-brand-bg font-medium text-brand'
                    : 'text-muted-foreground hover:bg-secondary hover:text-foreground',
                  n.ready ? '' : 'opacity-50',
                ].join(' ')}
              >
                {t(`sso:portal.${n.key}`)}
              </button>
            ))}
          </nav>
        }
      >
        {page}
      </Shell>
    </AuthGate>
  )
}

function MySystems({ onRequestAccess }: { onRequestAccess: () => void }) {
  const { t } = useTranslation()
  const q = useQuery({
    queryKey: ['portal-apps'],
    queryFn: () => api.get<{ sections: PortalSection[] }>('/portal/apps'),
  })

  const toLoadError = useMemo(() => makeToLoadError(t), [t])
  const state = fromQuery<{ sections: PortalSection[] }>(
    q,
    (d) => d.sections.every((s) => s.items.length === 0),
    toLoadError,
  )

  const all = (q.data?.sections ?? []).flatMap((s) => s.items)
  const allowed = all.filter((a) => a.allowed).length
  const pending = all.length - allowed

  return (
    <>
      <h1 className="text-xl font-semibold tracking-tight">{t('sso:portal.title')}</h1>
      <p className="mt-1 mb-6 text-sm text-muted-foreground">
        {t('sso:portal.subtitle', { allowed, pending })}
      </p>

      {/* 三态由 AsyncBoundary 强制分流：加载骨架、空态、错误态各自不同，
          不允许共用同一个外观 —— 失败被渲染成空列表是最常见的那种事故。 */}
      <AsyncBoundary
        state={state}
        errorTitle={t('sso:portal.errorTitle')}
        retryLabel={t('retry')}
        onRetry={() => void q.refetch()}
        pending={
          <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-4">
            {[0, 1, 2, 3].map((i) => (
              <Skeleton key={i} className="h-[74px] w-full" />
            ))}
          </div>
        }
        empty={
          <EmptyState
            icon={<ShieldOff />}
            title={t('sso:portal.empty.title')}
            reason={t('sso:portal.empty.reason')}
            // 这个按钮以前是空的（onClick 什么都不做）——按钮存在却没有落点，
            // 比没有按钮更糟：人会以为自己点错了
            action={{ label: t('sso:portal.empty.action'), onClick: onRequestAccess }}
          />
        }
      >
        {(data) => data.sections.map((s) => (
          <section key={s.group_id} className="mb-7">
            <div className="mb-3 flex items-baseline gap-2.5">
              <h2 className="text-sm font-semibold">
                {/* 后端不返回中文：未分组时返回空串，前端用固定文案 */}
                {s.name || t('sso:portal.ungrouped')}
              </h2>
              <span className="text-xs text-muted-foreground">{s.items.length}</span>
            </div>
            <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-4">
              {s.items.map((a) => (
                <a
                  key={a.id}
                  href={a.allowed ? a.base_url || '#' : '#'}
                  aria-disabled={!a.allowed}
                  // 门户是**启动台**，不是中转页：点开一个系统之后，人还要回来
                  // 开下一个。在原标签页跳走等于每次都得按后退键。
                  //
                  // ⚠️ rel 里的 noopener 不是可选项：没有它，被打开的页面能拿到
                  // window.opener，从而把**门户这个标签页**导航到任意地址
                  // （reverse tabnabbing）。对一个 SSO 门户尤其致命 ——
                  // 用户按惯性切回来，看到的可能是一个伪造的登录页，
                  // 而地址栏此前确实是我们。
                  target={a.allowed ? '_blank' : undefined}
                  rel={a.allowed ? 'noopener noreferrer' : undefined}
                  className={[
                    'flex items-start gap-3 rounded-[var(--radius-md)] border border-border bg-card p-3.5',
                    a.allowed
                      ? 'hover:border-primary hover:bg-accent'
                      : 'opacity-70 hover:border-warning',
                  ].join(' ')}
                >
                  <span
                    className="grid size-9 shrink-0 place-items-center rounded-[var(--radius)] text-xs font-semibold text-primary-foreground"
                    style={{ background: a.icon_color }}
                  >
                    {a.icon_text}
                  </span>
                  <span className="min-w-0">
                    <b className="block text-sm font-medium">{a.name}</b>
                    <span className="mt-1 flex flex-wrap items-center gap-1.5">
                      <span className="text-[11px] text-muted-foreground">{a.env}</span>
                      {!a.allowed && (
                        // 进不去要说清**为什么**，不是让卡片默默变灰
                        <span className="rounded-[var(--radius-sm)] bg-warning-bg px-1.5 py-0.5 text-[11px] text-warning">
                          {t(`sso:reason.${a.reason}`, { defaultValue: a.reason })}
                        </span>
                      )}
                    </span>
                  </span>
                </a>
              ))}
            </div>
          </section>
        ))}
      </AsyncBoundary>
    </>
  )
}
