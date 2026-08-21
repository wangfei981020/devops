import { tError } from '@ops/i18n'
import { toErrorInfo } from '@ops/api'
import { Badge, Banner, Skeleton } from '@ops/ui'
import { useNodeHealth } from './detail.js'

type TFn = (k: string, o?: Record<string, unknown>) => string

/**
 * 节点健康与自动修复。
 *
 * 放在「版本与升级」页而不是「节点」页：这里回答的不是"节点现在什么状态"
 * （那是节点页的事），而是**"它会不会被 GKE 悄悄重建"**——
 * auto-repair 触发后会 drain 并换掉节点，全程无通知，
 * 与"会不会被自动升级"是同一类问题，放一起才看得懂。
 *
 * ⚠️ 这一段最重要的不是那张表，是**任务开没开**。
 * `node_health_watch` 默认是关的，关着的时候"没有异常"只代表没在监控，
 * 而不是节点健康。后端专门返回了 task.note 来说这件事，必须顶在最前面。
 */
export function NodeHealthPanel({ t }: { t: TFn }) {
  const q = useNodeHealth()

  if (q.isPending) {
    return (
      <div className="mt-4 flex flex-col gap-2">
        <Skeleton className="h-4 w-[40%]" />
        <Skeleton className="h-4 w-[60%]" />
      </div>
    )
  }
  if (q.isError) {
    const n = toErrorInfo(q.error)
    return (
      <div className="mt-4">
        <Banner tone="bad">
          <span className="font-medium">{t('upgrades:nodeHealth.loadFailed')}</span>
          <span className="mt-0.5 block">{n.detail || tError(t, n.messageKey, n.params)}</span>
        </Banner>
      </div>
    )
  }
  const d = q.data
  if (d?.ok === false) {
    return (
      <div className="mt-4">
        <Banner tone="bad">
          <span className="font-medium">{t('upgrades:nodeHealth.loadFailed')}</span>
          <span className="mt-0.5 block">{d.error}</span>
        </Banner>
      </div>
    )
  }

  const rows = d?.rows ?? []
  const task = d?.task
  const watching = task?.found && task.enabled

  return (
    <section className="mt-5">
      <div className="mb-2 flex items-baseline gap-2.5">
        <h2 className="text-[13px] font-semibold text-foreground">{t('upgrades:nodeHealth.title')}</h2>
        <p className="min-w-0 flex-1 text-xs text-muted-foreground">{t('upgrades:nodeHealth.hint')}</p>
      </div>

      {/* ⚠️ 任务没开时，下面那张表是空的也不代表健康 —— 这句话必须在表之前 */}
      {!watching ? (
        <Banner tone="warn">
          <span className="font-medium">{t('upgrades:nodeHealth.notWatching')}</span>
          {task?.note ? <span className="mt-0.5 block">{task.note}</span> : null}
        </Banner>
      ) : null}

      {watching && rows.length === 0 ? (
        // 只有在确认"正在监控"之后，"当前无异常"才是一个成立的结论。
        // Banner 没有成功态（info|warn|bad）——那是刻意的，横幅是用来提示问题的
        <Banner tone="info">
          <span className="font-medium">{t('upgrades:nodeHealth.allGood')}</span>
          <span className="mt-0.5 block">
            {t('upgrades:nodeHealth.lastRun', {
              at: task?.last_run_at || '—',
              result: task?.last_result || '—',
            })}
          </span>
        </Banner>
      ) : null}

      {rows.length > 0 ? (
        <div className="overflow-x-auto rounded-[var(--radius-lg)] border border-border">
          <table className="w-full text-[13px]">
            <thead className="bg-card">
              <tr className="border-b border-border text-left text-xs text-muted-foreground">
                <th className="px-3 py-2 font-medium">{t('upgrades:nodeHealth.col.node')}</th>
                <th className="px-3 py-2 font-medium">{t('upgrades:nodeHealth.col.cluster')}</th>
                <th className="px-3 py-2 font-medium">{t('upgrades:nodeHealth.col.level')}</th>
                <th className="px-3 py-2 font-medium">{t('upgrades:nodeHealth.col.notReady')}</th>
                <th className="px-3 py-2 font-medium">{t('upgrades:nodeHealth.col.repairIn')}</th>
              </tr>
            </thead>
            <tbody>
              {rows.map((r) => (
                <tr key={`${r.cluster_id}-${r.node_name}`} className="border-b border-border last:border-b-0">
                  <td className="px-3 py-2 font-mono text-xs">{r.node_name}</td>
                  <td className="px-3 py-2 text-xs text-muted-foreground">{r.cluster || '—'}</td>
                  <td className="px-3 py-2">
                    <Badge tone={r.alert_level === 'red' ? 'bad' : r.alert_level === 'yellow' ? 'warn' : 'mute'}>
                      {r.alert_kind || r.alert_level || '—'}
                    </Badge>
                  </td>
                  <td className="px-3 py-2 text-xs">{r.not_ready_text || '—'}</td>
                  <td className="px-3 py-2 text-xs">
                    {/* 只有 GKE 有自动修复，其他 provider 留空而不是写"不会修复" */}
                    {r.repair_in_text || <span className="text-muted-foreground">—</span>}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      ) : null}

      {d?.thresholds?.note ? (
        <p className="mt-1.5 text-xs text-muted-foreground">{d.thresholds.note}</p>
      ) : null}
    </section>
  )
}
