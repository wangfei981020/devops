import { useTranslation } from '@ops/i18n'
import { AsyncBoundary, EmptyState, Skeleton, fromQuery } from '@ops/ui'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { MonitorSmartphone } from 'lucide-react'
import { useMemo } from 'react'
import { api } from '../../api/client.js'
import type { ListOf, Session } from '../../api/types.js'
import { makeToLoadError } from '../../shared/errorText.js'

/**
 * 我的会话。
 *
 * 这一页的价值在于**自助止损**：怀疑账号被盗时，不用等管理员，
 * 自己就能把别处的会话踢掉。等管理员的那段时间正是损失发生的时候。
 */
export function MySessions() {
  const { t, i18n } = useTranslation()
  const toLoadError = useMemo(() => makeToLoadError(t), [t])
  const qc = useQueryClient()

  const q = useQuery({
    queryKey: ['my-sessions'],
    queryFn: () => api.get<ListOf<Session>>('/auth/sessions'),
  })
  const state = fromQuery<ListOf<Session>>(q, (d) => d.total === 0, toLoadError)

  const revoke = useMutation({
    mutationFn: (id: number) => api.del(`/auth/sessions/${id}`),
    onSuccess: () => void qc.invalidateQueries({ queryKey: ['my-sessions'] }),
  })

  const fmt = (iso: string) =>
    new Date(iso).toLocaleString(i18n.language, { dateStyle: 'short', timeStyle: 'short' })

  return (
    <>
      <h1 className="text-xl font-semibold tracking-tight">{t('sso:sessions.title')}</h1>
      <p className="mt-1 mb-5 max-w-[80ch] text-sm text-muted-foreground">
        {t('sso:sessions.intro')}
      </p>

      <div className="overflow-hidden rounded-[var(--radius-md)] border border-border bg-card">
        <AsyncBoundary
          state={state}
          errorTitle={t('sso:sessions.errorTitle')}
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
            // 一个会话都没有是不可能的 —— 你正在用一个。所以这是异常，要说破。
            <EmptyState
              icon={<MonitorSmartphone />}
              title={t('sso:sessions.empty.title')}
              reason={t('sso:sessions.empty.reason')}
              action={{ label: t('retry'), onClick: () => void q.refetch() }}
            />
          }
        >
          {(data) => (
            <table className="w-full text-[13px]">
              <thead>
                <tr className="bg-muted text-xs text-muted-foreground">
                  <th className="px-3.5 py-2.5 text-left font-medium">
                    {t('sso:sessions.colDevice')}
                  </th>
                  <th className="px-3.5 py-2.5 text-left font-medium">
                    {t('sso:sessions.colIp')}
                  </th>
                  <th className="px-3.5 py-2.5 text-left font-medium">
                    {t('sso:sessions.colLastSeen')}
                  </th>
                  <th />
                </tr>
              </thead>
              <tbody>
                {data.items.map((s) => (
                  <tr key={s.id} className="border-t border-border hover:bg-muted">
                    <td className="max-w-[320px] px-3.5 py-2.5">
                      <span className="block truncate" title={s.user_agent}>
                        {s.user_agent || '—'}
                      </span>
                      <span className="text-[11px] text-muted-foreground">{s.source}</span>
                    </td>
                    <td className="px-3.5 py-2.5 font-mono text-[12px]">{s.client_ip}</td>
                    <td className="px-3.5 py-2.5 text-[12px] text-muted-foreground">
                      {fmt(s.last_seen_at)}
                    </td>
                    <td className="px-3.5 py-2.5 text-right">
                      {s.current ? (
                        <span className="rounded-[var(--radius-sm)] bg-success-bg px-2 py-0.5 text-[11px] text-success">
                          {t('sso:sessions.current')}
                        </span>
                      ) : (
                        <button
                          type="button"
                          onClick={() => revoke.mutate(s.id)}
                          className="cursor-pointer rounded-[var(--radius)] border border-border px-2 py-1 text-[11px] hover:border-destructive hover:text-destructive"
                        >
                          {t('sso:sessions.revoke')}
                        </button>
                      )}
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
