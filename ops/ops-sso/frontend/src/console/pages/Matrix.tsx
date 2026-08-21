import { useTranslation } from '@ops/i18n'
import { AsyncBoundary, EmptyState, Skeleton, fromQuery } from '@ops/ui'
import { useQuery } from '@tanstack/react-query'
import { Check, Grid3x3, X } from 'lucide-react'
import { useMemo, useState } from 'react'
import { api } from '../../api/client.js'
import type { Decision } from '../../api/types.js'
import { makeToLoadError } from '../../shared/errorText.js'
import { DecisionChain } from '../DecisionChain.js'

interface MatrixApp {
  id: number
  code: string
  name: string
  env: string
}
interface MatrixRow {
  user_id: number
  username: string
  display_name: string
  cells?: Record<string, { allowed: boolean; reason: string }>
  /** true 表示这一行判定失败 —— 不是"没权限"，是算不出来 */
  error?: boolean
}
interface MatrixData {
  apps: MatrixApp[]
  rows: MatrixRow[]
  total_users: number
  truncated: boolean
  limit: number
}

/**
 * 授权关系：谁能进哪个应用。
 *
 * 策略页回答「规则长什么样」，这一页回答「**结果是什么**」——
 * 那是从规则反推出来的，人脑推不了：一个人可能同时命中全局拒绝、
 * 分组放行、单应用强制拒绝三条，最终结论要跑一遍判定才知道。
 * 而审计问的恰恰是结果，不是规则。
 */
export function MatrixPage() {
  const { t } = useTranslation()
  const toLoadError = useMemo(() => makeToLoadError(t), [t])
  const [detail, setDetail] = useState<{ user: MatrixRow; app: MatrixApp } | null>(null)

  const q = useQuery({
    queryKey: ['access-matrix'],
    queryFn: () => api.get<MatrixData>('/access-matrix'),
  })
  const state = fromQuery<MatrixData>(q, (d) => d.rows.length === 0 || d.apps.length === 0, toLoadError)

  return (
    <>
      <h1 className="text-xl font-semibold tracking-tight">{t('sso:console.identitiesMatrix')}</h1>
      <p className="mt-1 mb-5 max-w-[80ch] text-sm text-muted-foreground">{t('sso:matrix.intro')}</p>

      <div className="overflow-hidden rounded-[var(--radius-md)] border border-border bg-card">
        <AsyncBoundary
          state={state}
          errorTitle={t('sso:matrix.errorTitle')}
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
              icon={<Grid3x3 />}
              title={t('sso:matrix.empty.title')}
              reason={t('sso:matrix.empty.reason')}
              action={null}
            />
          }
        >
          {(data) => (
            <>
              {data.truncated ? (
                // ⚠️ 截断必须说出来。少了的那些人不会有任何痕迹，
                // 而「看不到」会被读成「没有」。
                <p className="border-b border-warning bg-warning-bg px-4 py-2.5 text-xs">
                  {t('sso:matrix.truncated', { shown: data.rows.length, total: data.total_users })}
                </p>
              ) : null}
              <div className="overflow-x-auto">
                <table className="w-full text-[13px]">
                  <thead>
                    <tr className="bg-muted text-xs text-muted-foreground">
                      {/* 「人」列吃掉富余宽度，应用列才不会被均摊成一片空白 */}
                      <th className="sticky left-0 w-full bg-muted px-3.5 py-2.5 text-left font-medium">
                        {t('sso:matrix.colUser')}
                      </th>
                      {data.apps.map((a) => (
                        <th
                          key={a.id}
                          className="whitespace-nowrap px-3.5 py-2.5 text-center font-medium"
                        >
                          {a.name}
                          <span className="block text-[10px] font-normal opacity-70">{a.env}</span>
                        </th>
                      ))}
                    </tr>
                  </thead>
                  <tbody>
                    {data.rows.map((r) => (
                      <tr key={r.user_id} className="border-t border-border hover:bg-muted">
                        <td className="sticky left-0 bg-card px-3.5 py-2.5">
                          <b className="font-medium">{r.display_name || r.username}</b>
                          <span className="ml-2 font-mono text-[11px] text-muted-foreground">
                            {r.username}
                          </span>
                        </td>
                        {data.apps.map((a) => {
                          if (r.error) {
                            // 判定失败 ≠ 没权限。必须和「拒绝」长得不一样，
                            // 否则会被当成"这个人本来就进不去"而放过。
                            return (
                              <td key={a.id} className="px-3.5 py-2.5 text-center">
                                <span className="rounded-[var(--radius-sm)] bg-warning-bg px-2 py-0.5 text-[11px] text-warning">
                                  {t('sso:matrix.cellError')}
                                </span>
                              </td>
                            )
                          }
                          const cell = r.cells?.[String(a.id)]
                          const allowed = cell?.allowed ?? false
                          return (
                            <td key={a.id} className="px-3.5 py-2.5 text-center">
                              <button
                                type="button"
                                onClick={() => setDetail({ user: r, app: a })}
                                title={t('sso:matrix.clickHint')}
                                className={[
                                  'inline-grid size-6 cursor-pointer place-items-center rounded-[var(--radius-sm)]',
                                  allowed
                                    ? 'bg-success-bg text-success'
                                    : 'bg-danger-bg text-danger',
                                ].join(' ')}
                              >
                                {allowed ? <Check className="size-3.5" /> : <X className="size-3.5" />}
                              </button>
                            </td>
                          )
                        })}
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            </>
          )}
        </AsyncBoundary>
      </div>

      {detail ? <CellDetail detail={detail} onClose={() => setDetail(null)} /> : null}
    </>
  )
}

/** 点开一格：这个人对这个应用的完整判定链路。 */
function CellDetail({
  detail,
  onClose,
}: {
  detail: { user: MatrixRow; app: MatrixApp }
  onClose: () => void
}) {
  const { t } = useTranslation()
  const q = useQuery({
    queryKey: ['simulate', detail.user.user_id, detail.app.id],
    queryFn: () =>
      api.post<Decision>('/policies/simulate', {
        user_id: detail.user.user_id,
        app_id: detail.app.id,
      }),
  })

  return (
    <div className="mt-4 rounded-[var(--radius-md)] border border-border bg-card p-4">
      <div className="mb-3 flex items-center gap-2">
        <h2 className="text-sm font-semibold">
          {t('sso:matrix.detailTitle', {
            user: detail.user.display_name || detail.user.username,
            app: detail.app.name,
          })}
        </h2>
        <span className="flex-1" />
        <button
          type="button"
          onClick={onClose}
          className="cursor-pointer rounded-[var(--radius)] border border-border px-2 py-1 text-[11px] hover:bg-secondary"
        >
          {t('sso:matrix.close')}
        </button>
      </div>
      {q.isPending ? <Skeleton className="h-24 w-full" /> : null}
      {q.data ? <DecisionChain decision={q.data} /> : null}
    </div>
  )
}
