import { toErrorInfo } from '@ops/api'
import { Badge, Banner, Button, Dialog, Skeleton } from '@ops/ui'
import { useRenewals } from './renewals.js'

type TFn = (k: string, o?: Record<string, unknown>) => string

/** 续费记录。⚠️ 三态：成功 / 失败 / 不确定——不确定不能归到任何一边。 */
export function RenewalsDialog({ onClose, t }: { onClose: () => void; t: TFn }) {
  const q = useRenewals()
  const rows = q.data?.items ?? []

  const tone = (s?: string) =>
    s === 'success' ? 'ok' : s === 'failed' ? 'bad' : 'warn'

  return (
    <Dialog
      open
      onClose={onClose}
      title={t('domains:renewals.title')}
      description={t('domains:renewals.desc')}
      closeLabel={t('common:action.close')}
      width={1000}
      footer={
        <Button size="sm" onClick={onClose}>
          {t('common:action.close')}
        </Button>
      }
    >
      {q.isPending ? (
        <Skeleton className="h-5 w-[50%]" />
      ) : q.isError ? (
        <Banner tone="bad">
          <span className="font-medium">{t('domains:renewals.loadFailed')}</span>
          <span className="mt-0.5 block">
            {toErrorInfo(q.error).detail || t(toErrorInfo(q.error).messageKey)}
          </span>
        </Banner>
      ) : rows.length === 0 ? (
        <p className="py-4 text-[13px] text-muted-foreground">{t('domains:renewals.none')}</p>
      ) : (
        <div className="-mx-4 max-h-[56vh] overflow-auto">
          <table className="w-full text-[13px]">
            <thead className="sticky top-0 z-10 bg-card">
              <tr className="border-b border-border text-left text-xs text-muted-foreground">
                <th className="px-4 py-2 font-medium">{t('domains:renewals.col.at')}</th>
                <th className="px-4 py-2 font-medium">{t('domains:renewals.col.domain')}</th>
                <th className="px-4 py-2 font-medium">{t('domains:renewals.col.years')}</th>
                <th className="px-4 py-2 font-medium">{t('domains:renewals.col.amount')}</th>
                <th className="px-4 py-2 font-medium">{t('domains:renewals.col.status')}</th>
                <th className="px-4 py-2 font-medium">{t('domains:renewals.col.operator')}</th>
              </tr>
            </thead>
            <tbody>
              {rows.map((r) => (
                <tr key={r.id} className="border-b border-border last:border-0">
                  <td className="px-4 py-1.5 font-mono text-xs whitespace-nowrap">{r.at || '—'}</td>
                  <td className="px-4 py-1.5 font-mono text-xs">{r.domain}</td>
                  <td className="tabular px-4 py-1.5 text-xs">{r.years ?? '—'}</td>
                  <td className="tabular px-4 py-1.5 text-xs">
                    {r.amount != null ? `${r.currency ?? ''}${r.amount}` : '—'}
                  </td>
                  <td className="px-4 py-1.5">
                    {/* ⚠️ 不确定态单独一档：已扣费但没拿到确认，重试前必须回查厂商到期日 */}
                    <Badge tone={tone(r.status)}>{r.status || '—'}</Badge>
                    {r.detail ? (
                      <span className="ml-1.5 text-[11px] text-muted-foreground">{r.detail}</span>
                    ) : null}
                  </td>
                  <td className="px-4 py-1.5 text-xs text-muted-foreground">{r.operator || '—'}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </Dialog>
  )
}
