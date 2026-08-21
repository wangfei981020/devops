import { useTranslation } from '@ops/i18n'
import { AsyncBoundary, EmptyState, Skeleton, fromQuery } from '@ops/ui'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Inbox } from 'lucide-react'
import { useMemo, useState } from 'react'
import { ApiError, api } from '../../api/client.js'
import type { ListOf, PortalSection } from '../../api/types.js'
import { makeToLoadError } from '../../shared/errorText.js'

interface MyRequest {
  id: number
  app_id: number
  app_name: string
  reason: string
  duration_sec: number
  status: 'pending' | 'approved' | 'rejected' | 'blocked'
  created_at: string
  decided_at?: string
  expires_at?: string
  active?: boolean
  approver_note?: string
  blocked_rule?: string
  blocked_note?: string
}

/** 常用时长。给按钮而不是输入框：绝大多数申请就是这三档，填数字只会让人填错单位。 */
const DURATIONS = [4, 8, 24] as const

/**
 * 我的申请。
 *
 * # 为什么申请必须能自助发起
 *
 * 不能自助的话，"我要权限"这件事就退回到群里 @ 一声 ——
 * 谁批的、为什么批、什么时候到期，全都不在系统里，
 * 半年后复核只能靠翻聊天记录。
 *
 * # 为什么默认给短时长
 *
 * 人会选默认值。默认 8 小时和默认 30 天，产生的是两种完全不同的权限台账。
 */
export function MyRequests() {
  const { t } = useTranslation()
  const toLoadError = useMemo(() => makeToLoadError(t), [t])
  const q = useQuery({
    queryKey: ['my-requests'],
    queryFn: () => api.get<ListOf<MyRequest>>('/access-requests?mine=1'),
  })
  const state = fromQuery<ListOf<MyRequest>>(q, (d) => d.items.length === 0, toLoadError)

  return (
    <>
      <h1 className="text-xl font-semibold tracking-tight">{t('sso:portal.myRequests')}</h1>
      <p className="mt-1 mb-4 max-w-[80ch] text-sm text-muted-foreground">
        {t('sso:myreq.intro')}
      </p>

      <NewRequest onDone={() => void q.refetch()} />

      <h2 className="mt-6 mb-2 text-[15px] font-semibold">{t('sso:myreq.history')}</h2>
      <AsyncBoundary
        state={state}
        errorTitle={t('sso:myreq.errorTitle')}
        retryLabel={t('retry')}
        onRetry={() => void q.refetch()}
        pending={<Skeleton className="h-20 w-full" />}
        empty={
          <EmptyState
            icon={<Inbox />}
            title={t('sso:myreq.empty.title')}
            reason={t('sso:myreq.empty.reason')}
            action={null}
          />
        }
      >
        {(d) => (
          <div className="space-y-2">
            {d.items.map((r) => (
              <div key={r.id} className="rounded-[var(--radius-md)] border border-border bg-card p-3.5">
                <div className="flex flex-wrap items-baseline gap-2">
                  <b className="text-[13px] font-medium">{r.app_name}</b>
                  <StatusChip r={r} />
                  <span className="flex-1" />
                  <span className="text-[11px] text-muted-foreground">{fmt(r.created_at)}</span>
                </div>
                <p className="mt-1.5 text-[12px] text-muted-foreground">{r.reason}</p>
                {r.status === 'blocked' ? (
                  <p className="mt-2 rounded-[var(--radius)] border border-warning bg-warning-bg px-2.5 py-1.5 text-[12px]">
                    {t('sso:myreq.blocked', { rule: r.blocked_rule ?? '—' })}
                    {r.blocked_note ? ` ${r.blocked_note}` : ''}
                  </p>
                ) : null}
                {r.approver_note ? (
                  <p className="mt-2 text-[12px]">
                    <span className="text-muted-foreground">{t('sso:myreq.note')}：</span>
                    {r.approver_note}
                  </p>
                ) : null}
              </div>
            ))}
          </div>
        )}
      </AsyncBoundary>
    </>
  )
}

function StatusChip({ r }: { r: MyRequest }) {
  const { t } = useTranslation()
  // 已批准但已经过期，**不能还显示成「已批准」** —— 那会让人以为权限还在，
  // 进不去时先去怀疑系统坏了。
  const expired = r.status === 'approved' && r.expires_at != null && r.active === false
  const key = expired ? 'expired' : r.status
  const tone = {
    pending: 'bg-info-bg text-info',
    approved: 'bg-success-bg text-success',
    rejected: 'bg-danger-bg text-danger',
    blocked: 'bg-warning-bg text-warning',
    expired: 'bg-muted text-muted-foreground',
  }[key]
  return (
    <span className={`rounded-[var(--radius-sm)] px-2 py-0.5 text-[11px] ${tone}`}>
      {t(`sso:myreq.status.${key}`)}
      {key === 'approved' && r.expires_at ? ` · ${t('sso:myreq.until', { at: fmt(r.expires_at) })}` : ''}
    </span>
  )
}

function NewRequest({ onDone }: { onDone: () => void }) {
  const { t } = useTranslation()
  const qc = useQueryClient()
  const [appID, setAppID] = useState('')
  const [reason, setReason] = useState('')
  const [hours, setHours] = useState<number>(8)
  const [err, setErr] = useState<string | null>(null)
  const [ok, setOk] = useState(false)

  // 门户接口只返回这个人看得见的应用，正是"能申请哪些"的正确集合
  const apps = useQuery({
    queryKey: ['portal-apps'],
    queryFn: () => api.get<{ sections: PortalSection[] }>('/portal/apps'),
  })
  const options = (apps.data?.sections ?? [])
    .flatMap((s) => s.items)
    .filter((a) => !a.allowed)

  const m = useMutation({
    mutationFn: () =>
      api.post('/access-requests', {
        app_id: Number(appID),
        reason: reason.trim(),
        duration_sec: hours * 3600,
      }),
    onSuccess: () => {
      setErr(null)
      setOk(true)
      setReason('')
      setAppID('')
      void qc.invalidateQueries({ queryKey: ['my-requests'] })
      onDone()
    },
    onError: (e) => {
      setOk(false)
      setErr(
        e instanceof ApiError
          ? t(`errors.${e.code}`, { ...e.params, defaultValue: e.code })
          : t('error.unreachable'),
      )
    },
  })

  return (
    <div className="rounded-[var(--radius-md)] border border-border bg-card p-4">
      <h2 className="text-sm font-semibold">{t('sso:myreq.newTitle')}</h2>

      {options.length === 0 && !apps.isPending ? (
        // 没有可申请的对象，和"接口坏了"要分开说
        <p className="mt-2 text-[12px] text-muted-foreground">{t('sso:myreq.nothingToRequest')}</p>
      ) : (
        <>
          <div className="mt-3 grid gap-3 sm:grid-cols-[minmax(0,1fr)_auto]">
            <label className="block text-[12px]">
              <span className="mb-1 block text-muted-foreground">{t('sso:myreq.fApp')}</span>
              <select
                value={appID}
                onChange={(e) => setAppID(e.target.value)}
                className="w-full rounded-[var(--radius)] border border-border bg-background px-2.5 py-1.5 text-[13px]"
              >
                <option value="">{t('sso:myreq.pickApp')}</option>
                {options.map((a) => (
                  <option key={a.id} value={a.id}>
                    {a.name}（{a.env}）
                  </option>
                ))}
              </select>
            </label>
            <div className="text-[12px]">
              <span className="mb-1 block text-muted-foreground">{t('sso:myreq.fDuration')}</span>
              <div className="flex gap-1">
                {DURATIONS.map((h) => (
                  <button
                    key={h}
                    type="button"
                    onClick={() => setHours(h)}
                    aria-pressed={hours === h}
                    className={[
                      'cursor-pointer rounded-[var(--radius)] border px-2.5 py-1.5 text-[13px]',
                      hours === h ? 'border-brand bg-brand-bg text-brand' : 'border-border',
                    ].join(' ')}
                  >
                    {t('sso:myreq.hours', { n: h })}
                  </button>
                ))}
              </div>
            </div>
          </div>

          <label className="mt-3 block text-[12px]">
            <span className="mb-1 block text-muted-foreground">{t('sso:myreq.fReason')}</span>
            <input
              value={reason}
              onChange={(e) => setReason(e.target.value)}
              placeholder={t('sso:myreq.reasonPlaceholder')}
              className="w-full rounded-[var(--radius)] border border-border bg-background px-2.5 py-1.5 text-[13px]"
            />
          </label>

          {err ? (
            <p className="mt-2 rounded-[var(--radius)] bg-danger-bg px-2.5 py-1.5 text-[12px] text-danger">
              {err}
            </p>
          ) : null}
          {ok ? (
            <p className="mt-2 rounded-[var(--radius)] bg-success-bg px-2.5 py-1.5 text-[12px] text-success">
              {t('sso:myreq.submitted')}
            </p>
          ) : null}

          <div className="mt-3 flex items-center gap-2">
            <button
              type="button"
              disabled={!appID || reason.trim() === '' || m.isPending}
              onClick={() => m.mutate()}
              className="cursor-pointer rounded-[var(--radius)] bg-brand px-3 py-1.5 text-[13px] font-medium text-brand-foreground disabled:cursor-default disabled:opacity-50"
            >
              {m.isPending ? t('sso:myreq.submitting') : t('sso:myreq.submit')}
            </button>
            {/* 理由必填的原因写在旁边，否则人只会随手填个「需要」 */}
            <span className="text-[11px] text-muted-foreground">{t('sso:myreq.reasonWhy')}</span>
          </div>
        </>
      )}
    </div>
  )
}

function fmt(s: string) {
  const d = new Date(s)
  if (Number.isNaN(d.getTime())) return s
  const p = (n: number) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())} ${p(d.getHours())}:${p(d.getMinutes())}`
}
