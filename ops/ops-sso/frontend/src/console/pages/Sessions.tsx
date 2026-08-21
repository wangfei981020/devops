import { useTranslation } from '@ops/i18n'
import { AsyncBoundary, EmptyState, Skeleton, fromQuery } from '@ops/ui'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Clock } from 'lucide-react'
import { useMemo, useState } from 'react'
import { api } from '../../api/client.js'
import type { ListOf } from '../../api/types.js'
import { makeToLoadError } from '../../shared/errorText.js'

interface SessionRow {
  id: number
  user_id: number
  username: string
  display_name: string
  source: string
  client_ip: string
  user_agent: string
  created_at: string
  last_seen_at: string
  expires_at: string
  current: boolean
}

/**
 * 在线会话（管理员视角）。
 *
 * 出事时第一个要看的东西：某个账号被盗用、某人今天离职，
 * 要能立刻看到他还有几个会话活着，并且**当场踢掉**。
 * 改策略是不够的 —— 已经登进去的会话不会因为规则变了就自己断。
 */
export function SessionsPage() {
  const { t } = useTranslation()
  const toLoadError = useMemo(() => makeToLoadError(t), [t])
  const qc = useQueryClient()
  const [failed, setFailed] = useState<number | null>(null)

  const q = useQuery({
    queryKey: ['sessions'],
    queryFn: () => api.get<ListOf<SessionRow>>('/sessions'),
  })
  const state = fromQuery<ListOf<SessionRow>>(q, (d) => d.items.length === 0, toLoadError)

  const revoke = useMutation({
    mutationFn: (id: number) => api.del(`/sessions/${id}`),
    onSuccess: () => {
      setFailed(null)
      void qc.invalidateQueries({ queryKey: ['sessions'] })
    },
    // 失败必须说出来。乐观地把行删掉、实际没踢下线，是这一页最危险的错 ——
    // 处理安全事件的人会以为已经处理完了。
    onError: (_e, id) => setFailed(id),
  })

  return (
    <>
      <h1 className="text-xl font-semibold tracking-tight">{t('sso:console.sessions')}</h1>
      <p className="mt-1 mb-4 max-w-[80ch] text-sm text-muted-foreground">
        {t('sso:sessions2.intro')}
      </p>

      {failed !== null ? (
        <p className="mb-3 rounded-[var(--radius)] border border-destructive bg-danger-bg px-3 py-2 text-[12px] text-danger">
          {t('sso:sessions2.revokeFailed')}
        </p>
      ) : null}

      <div className="overflow-hidden rounded-[var(--radius-md)] border border-border bg-card">
        <AsyncBoundary
          state={state}
          errorTitle={t('sso:sessions2.errorTitle')}
          retryLabel={t('retry')}
          onRetry={() => void q.refetch()}
          pending={
            <div className="space-y-2 p-4">
              {[0, 1, 2].map((i) => (
                <Skeleton key={i} className="h-10 w-full" />
              ))}
            </div>
          }
          empty={
            <EmptyState
              icon={<Clock />}
              title={t('sso:sessions2.empty.title')}
              reason={t('sso:sessions2.empty.reason')}
              action={null}
            />
          }
        >
          {(data) => (
            <div className="overflow-x-auto">
              <table className="w-full min-w-[900px] text-[13px]">
                <thead>
                  <tr className="bg-muted text-xs text-muted-foreground">
                    <th className="px-3.5 py-2.5 text-left font-medium">
                      {t('sso:sessions2.colWho')}
                    </th>
                    <th className="px-3.5 py-2.5 text-left font-medium">
                      {t('sso:sessions2.colHow')}
                    </th>
                    <th className="px-3.5 py-2.5 text-left font-medium">
                      {t('sso:sessions2.colWhere')}
                    </th>
                    <th className="px-3.5 py-2.5 text-left font-medium">
                      {t('sso:sessions2.colLastSeen')}
                    </th>
                    <th className="px-3.5 py-2.5 text-left font-medium">
                      {t('sso:sessions2.colExpires')}
                    </th>
                    <th className="px-3.5 py-2.5" />
                  </tr>
                </thead>
                <tbody>
                  {data.items.map((s) => (
                    <tr key={s.id} className="border-t border-border hover:bg-muted">
                      <td className="px-3.5 py-2.5">
                        <b className="font-medium">{s.display_name}</b>
                        <span className="ml-2 font-mono text-[11px] text-muted-foreground">
                          {s.username}
                        </span>
                        {s.current ? (
                          <span className="ml-2 rounded-[var(--radius-sm)] bg-brand-bg px-1.5 py-0.5 text-[10px] text-brand">
                            {t('sso:sessions2.thisIsYou')}
                          </span>
                        ) : null}
                      </td>
                      <td className="px-3.5 py-2.5">
                        <span
                          className={[
                            'rounded-[var(--radius-sm)] px-2 py-0.5 text-[11px]',
                            // 应急通道登进来的必须显眼：它绕开的正是这套系统本身
                            s.source === 'break_glass'
                              ? 'bg-warning-bg text-warning'
                              : 'bg-info-bg text-info',
                          ].join(' ')}
                        >
                          {t(`sso:sessions2.source.${s.source}`, { defaultValue: s.source })}
                        </span>
                      </td>
                      <td className="px-3.5 py-2.5">
                        <span className="font-mono text-[11px] text-muted-foreground">
                          {s.client_ip}
                        </span>
                        <span className="ml-2 inline-block max-w-[26ch] truncate align-bottom text-[11px] text-muted-foreground">
                          {s.user_agent || '—'}
                        </span>
                      </td>
                      <td className="whitespace-nowrap px-3.5 py-2.5 text-muted-foreground">
                        {fmt(s.last_seen_at)}
                      </td>
                      <td className="whitespace-nowrap px-3.5 py-2.5 text-muted-foreground">
                        {fmt(s.expires_at)}
                      </td>
                      <td className="px-3.5 py-2.5 text-right">
                        <button
                          type="button"
                          // 自己那条禁掉：点完自己就登出了，而他大概率
                          // 正在处理一起安全事件。要退出自己有「退出登录」。
                          disabled={s.current || revoke.isPending}
                          onClick={() => revoke.mutate(s.id)}
                          title={s.current ? t('sso:sessions2.cannotRevokeSelf') : undefined}
                          className="cursor-pointer rounded-[var(--radius)] border border-border px-2 py-1 text-[11px] hover:bg-secondary disabled:cursor-default disabled:opacity-40"
                        >
                          {t('sso:sessions2.revoke')}
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
    </>
  )
}

function fmt(s: string) {
  const d = new Date(s)
  if (Number.isNaN(d.getTime())) return s
  const p = (n: number) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())} ${p(d.getHours())}:${p(d.getMinutes())}`
}
