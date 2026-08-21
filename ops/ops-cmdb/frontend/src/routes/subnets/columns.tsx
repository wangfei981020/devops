import type { Locale } from '@ops/i18n'
import { Badge, type ColumnDef } from '@ops/ui'
import type { Subnet } from './queries.js'

type TFn = (key: string, opts?: Record<string, unknown>) => string

export function subnetColumns(t: TFn, _locale: Locale): ColumnDef<Subnet>[] {
  return [
    {
      accessorKey: 'name',
      header: t('subnets:column.name'),
      cell: ({ row }) => {
        const s = row.original
        return (
          <div className="flex min-w-0 items-baseline gap-2">
            <span className="truncate font-medium text-foreground">{s.name}</span>
            {/* 云上已删除的**保留并标注**：别的资源可能还引用着这个网段，
                直接从台账里抹掉，就再也查不出"这条规则指向的子网没了" */}
            {s.stale ? <Badge tone="warn">{t('subnets:stale')}</Badge> : null}
          </div>
        )
      },
    },
    {
      accessorKey: 'cidr',
      header: t('subnets:column.cidr'),
      cell: ({ row }) => <span className="tabular font-mono text-[13px]">{row.original.cidr}</span>,
    },
    {
      id: 'network',
      header: t('subnets:column.network'),
      cell: ({ row }) => {
        const s = row.original
        return (
          <div className="flex min-w-0 flex-col">
            <span className="truncate text-[13px]">{s.network}</span>
            {/* auto 模式的 VPC 会在每个区域自动建子网 —— 看到意料之外的子网时，
                这一行能立刻解释它是哪来的。

                ⚠️ 但它光秃秃一个 `custom` / `auto` 挂在 VPC 名下面，
                既没有列头也没有说明，看起来像个来路不明的标签
                （OPSCMDB-031 P2-7）。信息有用，前提是看得懂 ——
                所以补 title，并把已知的两个取值翻成人话。 */}
            <span
              className="text-xs text-muted-foreground"
              title={s.networkMode ? networkModeHint(s.networkMode, t) : undefined}
            >
              {s.networkMode ? networkModeLabel(s.networkMode, t) : '—'}
            </span>
          </div>
        )
      },
    },
    {
      accessorKey: 'region',
      header: t('subnets:column.region'),
      cell: ({ row }) => <span className="font-mono text-xs">{row.original.region}</span>,
    },
    {
      accessorKey: 'project',
      header: t('subnets:column.project'),
      cell: ({ row }) => <span className="text-[13px]">{row.original.project}</span>,
    },
  ]
}

/**
 * VPC 子网模式的人话说法。
 *
 * ⚠️ 认不出的取值**原样显示**：云厂商加了新模式时，
 * 显示一个陌生的词好过显示一个错的解释，也好过显示「—」
 * （那会让人以为没采到）。
 */
export function networkModeLabel(mode: string, t: TFn): string {
  const key = `subnets:mode.${mode.toLowerCase()}`
  const label = t(key)
  return label === key ? mode : label
}

export function networkModeHint(mode: string, t: TFn): string {
  const key = `subnets:modeHint.${mode.toLowerCase()}`
  const hint = t(key)
  return hint === key ? t('subnets:modeHint.unknown', { mode }) : hint
}
