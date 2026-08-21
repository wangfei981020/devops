import { clusterLabel } from '../../lib/clusterLabel.js'
import type { Locale } from '@ops/i18n'
import { Badge, type ColumnDef } from '@ops/ui'
import type { PVC } from './queries.js'

type TFn = (key: string, opts?: Record<string, unknown>) => string

export function pvcColumns(t: TFn, _locale: Locale): ColumnDef<PVC>[] {
  return [
    {
      accessorKey: 'name',
      header: t('pvcs:column.name'),
      cell: ({ row }) => {
        const p = row.original
        return (
          <div className="flex min-w-0 flex-col">
            <span className="truncate font-medium text-foreground">{p.name}</span>
            <span className="truncate text-xs text-muted-foreground">
              {p.namespace} · {clusterLabel(p.clusterDisplay, p.clusterName)}
            </span>
          </div>
        )
      },
    },
    {
      accessorKey: 'status',
      header: t('pvcs:column.status'),
      cell: ({ row }) => {
        const s = row.original.status
        // 原值透传。Lost / Pending 是运维直接拿去 kubectl 里查的词
        const tone = s === 'Bound' ? 'ok' : s === 'Pending' ? 'warn' : 'bad'
        return <Badge tone={tone}>{s || t('common:state.unknown')}</Badge>
      },
    },
    {
      accessorKey: 'capacity',
      header: t('pvcs:column.capacity'),
      cell: ({ row }) => <span className="tabular text-[13px]">{row.original.capacity || '—'}</span>,
    },
    {
      accessorKey: 'storageClass',
      header: t('pvcs:column.storageClass'),
      cell: ({ row }) => {
        const p = row.original
        return (
          <div className="flex min-w-0 flex-col">
            <span className="font-mono text-xs">{p.storageClass || '—'}</span>
            {/* PV 名。旧版有独立一列，新版丢了。
                它是 PVC 和云盘之间的**唯一**关联键：去 GCP 控制台找那块盘、
                查快照、确认删了 PVC 之后盘还在不在，都要拿它。
                ⚠️ Pending 的 PVC 还没绑上 PV，这时空着是**正确**的，
                所以只在 Bound 时才提示"应该有却没有"。 */}
            {p.volumeName !== '' ? (
              <span className="truncate font-mono text-[11px] text-muted-foreground" title={p.volumeName}>
                {p.volumeName}
              </span>
            ) : p.status === 'Bound' ? (
              // 已绑定却没有 PV 名 = 采集缺了一块，不是"这个 PVC 没有盘"
              <span className="text-[11px] text-warning">{t('pvcs:noVolumeName')}</span>
            ) : null}
          </div>
        )
      },
    },
    {
      id: 'usage',
      header: t('pvcs:column.usage'),
      cell: ({ row }) => {
        const p = row.original
        if (!p.orphan) {
          return <span className="text-xs text-muted-foreground">{t('pvcs:inUse')}</span>
        }
        return (
          <span className="inline-flex items-baseline gap-1.5">
            {/* ⚠️ 只说"当前没有使用者"，**不写"可回收"**：
                定时任务的卷、刚 drain 完的有状态服务都会短暂没人用，
                写成可回收会让人放心地删掉别人的数据 */}
            <span className="text-xs text-warning" title={t('pvcs:orphanHint')}>
              {t('pvcs:orphan')}
            </span>
            {/* 🔴 紧跟金额。
                「1Ti 当前无使用者」只是个中性事实，
                后面跟一个「$102.4/月」才产生行动力 —— 而后端一直算好了这个数，
                这一页一个字都没显示（P1-23，本轮第 9 次「后端算了前端没接」）。
                ⚠️ 算不出来时**不显示**，不显示 $0 —— 那会被读成"这个卷不要钱" */}
            {p.monthlyUsd ? (
              <span className="tabular text-xs text-warning" title={t('pvcs:costHint')}>
                {t('pvcs:perMonth', { usd: p.monthlyUsd })}
              </span>
            ) : null}
          </span>
        )
      },
    },
  ]
}
