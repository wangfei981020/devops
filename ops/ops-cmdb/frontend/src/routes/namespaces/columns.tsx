import { ClusterCell } from '../../components/ClusterCell.js'
import { formatNumber, type Locale } from '@ops/i18n'
import { Badge, type ColumnDef, NoValue, type NoValueKind } from '@ops/ui'
import type { Namespace } from './queries.js'

type TFn = (key: string, opts?: Record<string, unknown>) => string

export function namespaceColumns(
  t: TFn,
  locale: Locale,
  labels: Record<NoValueKind, string>,
): ColumnDef<Namespace>[] {
  const count = (v: number | null) =>
    v === null ? (
      // 没采过 ≠ 0 个。填 0 会让一个采集断了的集群里所有命名空间
      // 看起来都是空的，而空命名空间是不需要处理的
      <NoValue kind="notIngested" labels={labels} />
    ) : (
      <span className="tabular">{formatNumber(v, locale)}</span>
    )

  return [
    {
      accessorKey: 'name',
      header: t('namespaces:column.name'),
      cell: ({ row }) => {
        const n = row.original
        return (
          <div className="flex min-w-0 flex-col">
            <span className="truncate font-medium text-foreground">{n.name}</span>
            {/* 没登记归属项目时明说，不要留白 —— 留白会被当成"这个命名空间没人管"。

                ⚠️ 未归属用**警告色**，和主机头台账保持一致（OPSCMDB-031 P1-21）：
                同一个概念（没关联到业务项目）原来一页灰色小字、一页橙色徽标，
                两个页面各自判断了一次"这算不算待办"。它算 ——
                成本归集、告警找人、下线评估全靠这条关联，缺了就没人认领。 */}
            {n.project ? (
              <span className="truncate text-xs text-muted-foreground">{n.project}</span>
            ) : (
              <span className="truncate text-xs text-warning" title={t('namespaces:noProjectHint')}>
                {t('namespaces:noProject')}
              </span>
            )}
          </div>
        )
      },
    },
    {
      accessorKey: 'clusterName',
      header: t('namespaces:column.cluster'),
      cell: ({ row }) => (
        <ClusterCell name={row.original.clusterName} display={row.original.clusterDisplay} />
      ),
    },
    {
      id: 'phase',
      header: t('namespaces:column.phase'),
      cell: ({ row }) => {
        const p = row.original.phase
        // Terminating 要显眼：卡在这个状态会一直占着名字，
        // 让同名重建持续失败 —— 它不是"正在正常删除"就完事了
        if (p === 'Terminating') {
          return (
            <div className="flex flex-col items-start gap-0.5">
              <Badge tone="warn">Terminating</Badge>
              <span className="text-[11px] text-muted-foreground">
                {t('namespaces:terminatingHint')}
              </span>
            </div>
          )
        }
        return <Badge tone={p === 'Active' ? 'ok' : 'mute'}>{p || t('common:state.unknown')}</Badge>
      },
    },
    {
      id: 'workloads',
      header: t('namespaces:column.workloads'),
      cell: ({ row }) => count(row.original.workloads),
    },
    {
      id: 'pods',
      header: t('namespaces:column.pods'),
      cell: ({ row }) => {
        const n = row.original
        if (n.pods === null) return <NoValue kind="notIngested" labels={labels} />
        return (
          <span className="tabular">
            {formatNumber(n.pods, locale)}
            {/* 异常数只在 >0 时出现：恒定显示 "0 异常" 会让真出问题时反而不显眼 */}
            {n.podsBad !== null && n.podsBad > 0 ? (
              <span className="ml-1.5 text-warning">
                {t('namespaces:podsBad', { count: n.podsBad })}
              </span>
            ) : null}
          </span>
        )
      },
    },
  ]
}
