import { useTranslation } from '@ops/i18n'
import { AsyncBoundary, EmptyState, Skeleton, fromQuery } from '@ops/ui'
import { useQuery } from '@tanstack/react-query'
import { Clock } from 'lucide-react'
import { useMemo } from 'react'
import { api } from '../../api/client.js'
import type { ListOf } from '../../api/types.js'
import { makeToLoadError } from '../../shared/errorText.js'

interface TempGrant {
  id: number
  app_id: number
  scope: string
  reason: string
  ticket_ref?: string
  status: string
  approver_id?: number
  remaining_sec?: number
}

/**
 * 我的授权。
 *
 * ⚠️ **目前只有临时提权**。后端 `/portal/grants` 返回的是审批产生的临时授权，
 * 不含用户组、部门这类长期授权 —— 那些还没有对应的接口。
 *
 * 这一点必须在界面上说破。如果不说，用户看到一个空列表会以为
 * "我什么权限都没有"，而实际上他可能有一堆长期授权 ——
 * 把「查不到」显示成「没有」，是这类界面最常见的谎报。
 */
export function MyGrants() {
  const { t } = useTranslation()
  const toLoadError = useMemo(() => makeToLoadError(t), [t])
  const q = useQuery({
    queryKey: ['my-grants'],
    queryFn: () => api.get<ListOf<TempGrant>>('/portal/grants'),
  })
  const state = fromQuery<ListOf<TempGrant>>(q, (d) => d.total === 0, toLoadError)

  const hhmm = (sec?: number) => {
    if (sec == null) return '—'
    if (sec <= 0) return t('sso:grants.expired')
    const h = Math.floor(sec / 3600)
    const m = Math.floor((sec % 3600) / 60)
    return t('sso:grants.remaining', { h, m })
  }

  return (
    <>
      <h1 className="text-xl font-semibold tracking-tight">{t('sso:grants.title')}</h1>
      <p className="mt-1 mb-3 max-w-[80ch] text-sm text-muted-foreground">
        {t('sso:grants.intro')}
      </p>
      {/* 覆盖范围的限制放在最显眼处，而不是脚注 */}
      <p className="mb-5 max-w-[80ch] rounded-[var(--radius)] border border-warning bg-warning-bg p-3 text-xs leading-relaxed">
        {t('sso:grants.scopeCaveat')}
      </p>

      <div className="overflow-hidden rounded-[var(--radius-md)] border border-border bg-card">
        <AsyncBoundary
          state={state}
          errorTitle={t('sso:grants.errorTitle')}
          retryLabel={t('retry')}
          onRetry={() => void q.refetch()}
          pending={
            <div className="space-y-2 p-4">
              {[0, 1].map((i) => (
                <Skeleton key={i} className="h-12 w-full" />
              ))}
            </div>
          }
          empty={
            <EmptyState
              icon={<Clock />}
              title={t('sso:grants.empty.title')}
              reason={t('sso:grants.empty.reason')}
              action={null}
            />
          }
        >
          {(data) => (
            <table className="w-full text-[13px]">
              <thead>
                <tr className="bg-muted text-xs text-muted-foreground">
                  <th className="px-3.5 py-2.5 text-left font-medium">{t('sso:grants.colScope')}</th>
                  <th className="px-3.5 py-2.5 text-left font-medium">{t('sso:grants.colWhy')}</th>
                  <th className="px-3.5 py-2.5 text-left font-medium">{t('sso:grants.colLeft')}</th>
                </tr>
              </thead>
              <tbody>
                {data.items.map((g) => (
                  <tr key={g.id} className="border-t border-border hover:bg-muted">
                    <td className="px-3.5 py-2.5">{g.scope}</td>
                    <td className="px-3.5 py-2.5 text-[12px] text-muted-foreground">
                      {g.reason}
                      {g.ticket_ref ? ` · ${g.ticket_ref}` : ''}
                    </td>
                    <td className="px-3.5 py-2.5">
                      <span
                        className={[
                          'rounded-[var(--radius-sm)] px-2 py-0.5 text-[11px]',
                          (g.remaining_sec ?? 0) < 3600
                            ? 'bg-warning-bg text-warning'
                            : 'bg-muted text-muted-foreground',
                        ].join(' ')}
                      >
                        {hhmm(g.remaining_sec)}
                      </span>
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
