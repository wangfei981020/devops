import { useTranslation } from '@ops/i18n'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { AsyncBoundary, Banner, Button, Skeleton, TextInput, cn, fromQuery } from '@ops/ui'
import { WriteButton } from '../components/WriteButton.js'
import { useEffect, useState } from 'react'
import { get, post, put, makeLoadError } from '../lib/api.js'

type Config = {
  enabled: boolean
  send_at: string
  notifier_ids: number[]
  last_sent_on: string | null
  last_error: string
  configured: boolean
}
type Notifier = { id: number; name: string; type: string }
type Preview = { fields: { key: string; value: string }[]; detail: string }

/**
 * 日报。
 *
 * 旧系统的日报开关挂在**每条规则**上，模板按 domain 硬编码。
 * 二十条规则都打开的话，早上会收到二十封各自只讲一条规则的邮件，
 * 而值班的人要回答的是"昨天整体怎么样、有没有我漏掉的" ——
 * 那个问题需要横向汇总，逐规则的邮件恰恰答不了。所以这里是租户级的一份。
 */
export function ReportPage() {
  const { t } = useTranslation()
  const qc = useQueryClient()
  const cfg = useQuery({ queryKey: ['report'], queryFn: () => get<Config>('/report') })
  const notifiers = useQuery({
    queryKey: ['notifiers'],
    queryFn: () => get<{ items: Notifier[] }>('/notifiers'),
  })

  const [enabled, setEnabled] = useState(false)
  const [sendAt, setSendAt] = useState('09:00')
  const [picked, setPicked] = useState<number[]>([])
  // 服务端配置到达后灌进表单。用 useEffect 而不是 initialState：
  // 首次渲染时 query 还在 pending，initialState 会把表单锁在默认值上，
  // 表现为"我明明配过，打开却还是默认的"
  useEffect(() => {
    if (!cfg.data) return
    setEnabled(cfg.data.enabled)
    setSendAt(cfg.data.send_at)
    setPicked(cfg.data.notifier_ids)
  }, [cfg.data])

  const save = useMutation({
    mutationFn: () => put('/report', { enabled, send_at: sendAt, notifier_ids: picked }),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['report'] }),
  })
  const preview = useMutation({ mutationFn: () => post<Preview>('/report/preview') })
  const sendNow = useMutation({
    mutationFn: () => post<{ sent: number; total: number }>('/report/send'),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['report'] }),
  })

  return (
    <div className="flex flex-col gap-3">
      <AsyncBoundary
        state={fromQuery(cfg, () => false, makeLoadError(t))}
        pending={<Skeleton className="h-40 w-full" />}
        // 日报配置永远有内容（没配过时后端返回默认值），所以不会走空态
        empty={null}
        errorTitle={t('opsalert:report.loadError')}
        retryLabel={t('action.retry')}
        onRetry={() => cfg.refetch()}
      >
        {(data) => (
          <>
            {/* 上次发送的结果。⚠️ 有错误就必须显示——"开了日报却从没收到"
                是最难查的一类，因为界面上开关是绿的，日志里什么都没有 */}
            {data.last_error && <Banner tone="bad">{data.last_error}</Banner>}
            {data.enabled && !data.last_error && data.last_sent_on && (
              <Banner tone="info">{t('opsalert:report.lastSent', { date: data.last_sent_on })}</Banner>
            )}
            {data.enabled && !data.last_sent_on && (
              // 开着但从没发过：可能只是还没到第一个发送时刻，也可能是坏的。
              // 说清楚"还没发过"本身，比不说要好
              <Banner tone="warn">{t('opsalert:report.neverSent')}</Banner>
            )}

            <section className="rounded-lg border border-border bg-card">
              <h3 className="border-b border-border px-3.5 py-2 text-xs font-semibold">
                {t('opsalert:report.settings')}
              </h3>
              <div className="flex flex-col gap-3 px-3.5 py-3">
                <label className="flex items-center gap-2 text-sm">
                  <input
                    type="checkbox"
                    checked={enabled}
                    onChange={(e) => setEnabled(e.target.checked)}
                    className="size-4 cursor-pointer accent-[var(--color-primary)]"
                  />
                  {t('opsalert:report.enable')}
                </label>

                <label className="flex max-w-40 flex-col gap-1">
                  <span className="text-2xs text-muted-foreground">{t('opsalert:report.sendAt')}</span>
                  <TextInput value={sendAt} onChange={(e) => setSendAt(e.target.value)} placeholder="09:00" />
                </label>

                <div className="flex flex-col gap-1.5">
                  <span className="text-2xs text-muted-foreground">{t('opsalert:report.notifiers')}</span>
                  {notifiers.data?.items.length === 0 ? (
                    <span className="text-xs text-muted-foreground">{t('opsalert:report.noNotifiers')}</span>
                  ) : (
                    <div className="flex flex-wrap gap-1.5">
                      {(notifiers.data?.items ?? []).map((n) => {
                        const on = picked.includes(n.id)
                        return (
                          <button
                            key={n.id}
                            type="button"
                            aria-pressed={on}
                            onClick={() =>
                              setPicked(on ? picked.filter((x) => x !== n.id) : [...picked, n.id])
                            }
                            className={cn(
                              'cursor-pointer rounded-full border px-3 py-1 text-xs transition-colors',
                              on
                                ? 'border-primary bg-primary/10 text-primary'
                                : 'border-border text-muted-foreground hover:text-foreground',
                            )}
                          >
                            {n.name}
                          </button>
                        )
                      })}
                    </div>
                  )}
                  {/* 开着却没选渠道 = 永远收不到。保存时后端也会拦，
                      但在这里先说一句，省得填完再被打回来 */}
                  {enabled && picked.length === 0 && (
                    <span className="text-2xs text-danger">{t('opsalert:report.needNotifier')}</span>
                  )}
                </div>

                <div className="flex flex-wrap gap-2">
                  <WriteButton
                    perm="alert:manage_report"
                    loading={save.isPending}
                    blockedReason={enabled && picked.length === 0 ? t('opsalert:report.needNotifier') : undefined}
                    onClick={() => save.mutate()}
                  >
                    {t('action.save')}
                  </WriteButton>
                  <Button variant="ghost" loading={preview.isPending} onClick={() => preview.mutate()}>
                    {t('opsalert:report.preview')}
                  </Button>
                  {/* 试发是真的往外发消息，所以要写权限。
                      有它的理由：日报一天只有一次验证机会，
                      配错了要等到第二天早上才知道，然后再等一天 */}
                  <WriteButton
                    perm="alert:manage_report"
                    loading={sendNow.isPending}
                    blockedReason={picked.length === 0 ? t('opsalert:report.needNotifier') : undefined}
                    onClick={() => sendNow.mutate()}
                  >
                    {t('opsalert:report.sendNow')}
                  </WriteButton>
                </div>

                {save.isError && <Banner tone="bad">{String((save.error as Error).message)}</Banner>}
                {sendNow.isError && <Banner tone="bad">{String((sendNow.error as Error).message)}</Banner>}
                {sendNow.isSuccess && (
                  <Banner tone="info">
                    {t('opsalert:report.sentOk', { sent: sendNow.data.sent, total: sendNow.data.total })}
                  </Banner>
                )}
              </div>
            </section>
          </>
        )}
      </AsyncBoundary>

      {preview.data && (
        <section className="rounded-lg border border-border bg-card">
          <h3 className="border-b border-border px-3.5 py-2 text-xs font-semibold">
            {t('opsalert:report.previewTitle')}
          </h3>
          <div className="flex flex-col gap-3 px-3.5 py-3">
            <dl className="grid gap-x-4 gap-y-1.5 sm:grid-cols-[max-content_1fr]">
              {preview.data.fields.map((f) => (
                <div key={f.key} className="contents">
                  <dt className="text-xs text-muted-foreground">{f.key}</dt>
                  <dd className="text-xs">{f.value}</dd>
                </div>
              ))}
            </dl>
            <pre className="overflow-auto rounded border border-border bg-muted/40 p-2.5 font-mono text-[11px] leading-relaxed whitespace-pre-wrap">
              {preview.data.detail}
            </pre>
          </div>
        </section>
      )}
      {preview.isError && (
        <Banner tone="bad">{String((preview.error as Error).message)}</Banner>
      )}
    </div>
  )
}
