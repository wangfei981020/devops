import { RelationRowActions } from './RelationRowActions.js'
import { Badge, type ColumnDef } from '@ops/ui'
import type { Relation } from './queries.js'

type TFn = (key: string, opts?: Record<string, unknown>) => string

export function relationColumns(t: TFn): ColumnDef<Relation>[] {
  return [
    {
      accessorKey: 'src_name',
      header: t('relations:column.src'),
      cell: ({ row }) => (
        <div className="flex min-w-0 flex-col">
          <span className="truncate text-foreground">{row.original.src_name}</span>
          <span className="text-xs text-muted-foreground">{row.original.src_type}</span>
        </div>
      ),
    },
    {
      accessorKey: 'rel_type',
      header: t('relations:column.rel'),
      cell: ({ row }) => <span className="font-mono text-xs">{row.original.rel_type}</span>,
    },
    {
      accessorKey: 'dst_name',
      header: t('relations:column.dst'),
      cell: ({ row }) => (
        <div className="flex min-w-0 flex-col">
          <span className="truncate text-foreground">{row.original.dst_name}</span>
          <span className="text-xs text-muted-foreground">{row.original.dst_type}</span>
        </div>
      ),
    },
    {
      id: 'origin',
      header: t('relations:column.origin'),
      cell: ({ row }) => {
        const o = row.original.origin
        // 采集推断的关系下一轮同步可能就没了，人工登记的不会 —— 处置方式不同
        return o === 'manual' ? (
          <Badge tone="info">{t('relations:origin.manual')}</Badge>
        ) : (
          <Badge tone="mute">{t('relations:origin.sync')}</Badge>
        )
      },
    },
    {
      id: 'actions',
      // 操作列固定在最右侧。⚠️ 标了就必须真的排在数组最后（check-action-column 守这条）
      meta: { action: true },
      header: '',
      cell: ({ row }) => <RelationRowActions r={row.original} />,
    },
  ]
}
