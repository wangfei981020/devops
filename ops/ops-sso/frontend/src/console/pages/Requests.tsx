import { useTranslation } from '@ops/i18n'
import { AsyncBoundary, EmptyState, Skeleton, fromQuery } from '@ops/ui'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Inbox } from 'lucide-react'
import { useMemo, useState } from 'react'
import { ApiError, api } from '../../api/client.js'
import type { ListOf } from '../../api/types.js'
import { makeToLoadError } from '../../shared/errorText.js'

export interface AccessRequest {
  id: number
  requester_id: number
  requester_name: string
  app_id: number
  app_name: string
  scope: string
  reason: string
  ticket_ref: string
  duration_sec: number
  status: 'pending' | 'approved' | 'rejected' | 'blocked'
  risk: string
  approver_name?: string
  approver_note?: string
  created_at: string
  decided_at?: string
  expires_at?: string
  active?: boolean
  needs_two_step?: boolean
  blocked_rule?: string
  blocked_note?: string
}

const TABS = ['pending', 'approved', 'rejected', 'blocked'] as const

/**
 * 临时提权：申请与审批。
 *
 * # 为什么要有「临时」这一套
 *
 * 没有它，运维面对"我今天要看一下生产日志"只有两个选择：
 * 拒绝，或者**给一条永久规则**。而永久规则从来没有人回来删 ——
 * 权限系统里积压最快的垃圾就是这么来的。
 * 临时提权把"给"和"到期收回"绑在一起，收回不需要谁记得。
 *
 * # 为什么审批要看到「他现在已经有什么」
 *
 * 审批人最常问的是"他是不是本来就该有"。答不出来的时候，
 * 默认动作是点批准 —— 那样这道审批就只是个橡皮图章。
 */
export function RequestsPage() {
  const { t } = useTranslation()
  const qc = useQueryClient()
  const toLoadError = useMemo(() => makeToLoadError(t), [t])
  const [tab, setTab] = useState<(typeof TABS)[number]>('pending')
  const [err, setErr] = useState<string | null>(null)

  const q = useQuery({
    queryKey: ['access-requests', tab],
    queryFn: () => api.get<ListOf<AccessRequest>>(`/access-requests?status=${tab}`),
  })
  const state = fromQuery<ListOf<AccessRequest>>(q, (d) => d.items.length === 0, toLoadError)

  const decide = useMutation({
    mutationFn: (v: { id: number; approve: boolean; note: string }) =>
      api.post(`/access-requests/${v.id}/decide`, { approve: v.approve, note: v.note }),
    onSuccess: () => {
      setErr(null)
      void qc.invalidateQueries({ queryKey: ['access-requests'] })
    },
    onError: (e) =>
      setErr(
        e instanceof ApiError
          ? t(`errors.${e.code}`, { ...e.params, defaultValue: e.code })
          : t('error.unreachable'),
      ),
  })

  return (
    <>
      <h1 className="text-xl font-semibold tracking-tight">{t('sso:console.requests')}</h1>
      <p className="mt-1 mb-4 max-w-[80ch] text-sm text-muted-foreground">{t('sso:req.intro')}</p>

      <div className="mb-3 inline-flex rounded-[var(--radius)] border border-border p-0.5">
        {TABS.map((k) => (
          <button
            key={k}
            type="button"
            onClick={() => setTab(k)}
            aria-pressed={tab === k}
            className={[
              'cursor-pointer rounded-[calc(var(--radius)-2px)] px-3 py-1.5 text-[13px]',
              tab === k ? 'bg-brand-bg font-medium text-brand' : 'text-muted-foreground',
            ].join(' ')}
          >
            {t(`sso:req.status.${k}`)}
          </button>
        ))}
      </div>

      {err ? (
        <p className="mb-3 rounded-[var(--radius)] border border-destructive bg-danger-bg px-3 py-2 text-[12px] text-danger">
          {err}
        </p>
      ) : null}

      <AsyncBoundary
        state={state}
        errorTitle={t('sso:req.errorTitle')}
        retryLabel={t('retry')}
        onRetry={() => void q.refetch()}
        pending={
          <div className="space-y-2">
            {[0, 1].map((i) => (
              <Skeleton key={i} className="h-24 w-full" />
            ))}
          </div>
        }
        empty={
          <EmptyState
            icon={<Inbox />}
            title={t(`sso:req.empty.${tab}.title`)}
            reason={t(`sso:req.empty.${tab}.reason`)}
            action={null}
          />
        }
      >
        {(d) => (
          <div className="space-y-2">
            {d.items.map((r) => (
              <RequestCard
                key={r.id}
                r={r}
                busy={decide.isPending}
                onDecide={(approve, note) => decide.mutate({ id: r.id, approve, note })}
              />
            ))}
          </div>
        )}
      </AsyncBoundary>
    </>
  )
}

function RequestCard({
  r,
  busy,
  onDecide,
}: {
  r: AccessRequest
  busy: boolean
  onDecide: (approve: boolean, note: string) => void
}) {
  const { t } = useTranslation()
  const [note, setNote] = useState('')

  return (
    <div className="rounded-[var(--radius-md)] border border-border bg-card p-4">
      <div className="flex flex-wrap items-baseline gap-x-2 gap-y-1">
        <b className="text-[14px] font-semibold">{r.requester_name}</b>
        <span className="text-[13px] text-muted-foreground">{t('sso:req.wants')}</span>
        <b className="text-[14px] font-semibold">{r.app_name}</b>
        {r.scope ? (
          <span className="rounded-[var(--radius-sm)] bg-muted px-1.5 py-0.5 font-mono text-[11px] text-muted-foreground">
            {r.scope}
          </span>
        ) : null}
        <span className="flex-1" />
        <span className="text-[11px] text-muted-foreground">{fmt(r.created_at)}</span>
      </div>

      {/* 时长要显眼：审批人真正在决定的是"给多久"，而不只是"给不给" */}
      <div className="mt-2 flex flex-wrap gap-x-4 gap-y-1 text-[12px]">
        <span>
          <span className="text-muted-foreground">{t('sso:req.duration')}：</span>
          {hours(r.duration_sec)}
        </span>
        {r.ticket_ref ? (
          <span>
            <span className="text-muted-foreground">{t('sso:req.ticket')}：</span>
            {r.ticket_ref}
          </span>
        ) : null}
        {r.risk ? (
          <span>
            <span className="text-muted-foreground">{t('sso:req.risk')}：</span>
            {t(`sso:req.riskLevel.${r.risk}`, { defaultValue: r.risk })}
          </span>
        ) : null}
      </div>

      <p className="mt-2 rounded-[var(--radius)] bg-muted px-3 py-2 text-[13px]">
        {r.reason || <span className="text-muted-foreground">{t('sso:req.noReason')}</span>}
      </p>

      {r.status === 'blocked' ? (
        // SoD 拦截和人工拒绝必须分开：前者是制度不允许，批不了；
        // 后者是这次不给，下次可以再申请。混在一起会让人反复提同一个申请。
        <p className="mt-2 rounded-[var(--radius)] border border-warning bg-warning-bg px-3 py-2 text-[12px]">
          {t('sso:req.blockedBy', { rule: r.blocked_rule ?? '—' })}
          {r.blocked_note ? ` ${r.blocked_note}` : ''}
        </p>
      ) : null}

      {r.status === 'pending' ? (
        <div className="mt-3 flex flex-wrap items-center gap-2">
          <input
            value={note}
            onChange={(e) => setNote(e.target.value)}
            placeholder={t('sso:req.notePlaceholder')}
            className="min-w-[16rem] flex-1 rounded-[var(--radius)] border border-border bg-background px-2.5 py-1.5 text-[13px]"
          />
          <button
            type="button"
            disabled={busy}
            onClick={() => onDecide(true, note)}
            className="cursor-pointer rounded-[var(--radius)] bg-brand px-3 py-1.5 text-[13px] font-medium text-brand-foreground disabled:opacity-50"
          >
            {t('sso:req.approve')}
          </button>
          <button
            type="button"
            disabled={busy}
            onClick={() => onDecide(false, note)}
            className="cursor-pointer rounded-[var(--radius)] border border-border px-3 py-1.5 text-[13px] hover:bg-secondary disabled:opacity-50"
          >
            {t('sso:req.reject')}
          </button>
          {/* 自己批自己后端会拒。写在这里，免得点了才知道 */}
          <span className="text-[11px] text-muted-foreground">{t('sso:req.noSelfApprove')}</span>
        </div>
      ) : (
        <div className="mt-2 text-[12px] text-muted-foreground">
          {t(`sso:req.decidedBy.${r.status}`, {
            who: r.approver_name ?? '—',
            at: r.decided_at ? fmt(r.decided_at) : '—',
            defaultValue: r.status,
          })}
          {r.approver_note ? `：${r.approver_note}` : ''}
          {r.expires_at ? (
            <span className="ml-2">
              {/* 到期了就是到期了。approved 但已过期还显示成"已批准"，
                  会让人以为权限还在。 */}
              {r.active
                ? t('sso:req.activeUntil', { at: fmt(r.expires_at) })
                : t('sso:req.expiredAt', { at: fmt(r.expires_at) })}
            </span>
          ) : null}
        </div>
      )}
    </div>
  )
}

function hours(sec: number): string {
  if (!sec) return '—'
  const h = Math.round(sec / 3600)
  return h >= 24 ? `${Math.round(h / 24)}d` : `${h}h`
}

function fmt(s: string) {
  const d = new Date(s)
  if (Number.isNaN(d.getTime())) return s
  const p = (n: number) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())} ${p(d.getHours())}:${p(d.getMinutes())}`
}
