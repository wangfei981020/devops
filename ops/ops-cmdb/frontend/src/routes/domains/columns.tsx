import type { Locale } from '@ops/i18n'
import { Badge, type ColumnDef, NoValue, type NoValueKind } from '@ops/ui'
import { DomainRowActions } from './DomainRowActions.js'
import type { Domain } from './queries.js'

type TFn = (key: string, opts?: Record<string, unknown>) => string

/** 到期天数的通用渲染：null 一律走 unknown（警告色），不是灰色的 —。 */
function expiryCell(
  days: number | null,
  at: string,
  t: TFn,
  labels: Record<NoValueKind, string>,
) {
  if (days === null) return <NoValue kind="unknown" labels={labels} />
  const tone = days < 0 ? 'text-danger' : days <= 30 ? 'text-warning' : 'text-muted-foreground'
  return (
    <div className="flex min-w-0 flex-col">
      <span className={`tabular text-[13px] ${tone}`}>
        {days < 0 ? t('domains:expiredDaysAgo', { count: -days }) : t('domains:daysLeft', { count: days })}
      </span>
      <span className="tabular text-xs text-muted-foreground">{at}</span>
    </div>
  )
}

export function domainColumns(
  t: TFn,
  _locale: Locale,
  labels: Record<NoValueKind, string>,
): ColumnDef<Domain>[] {
  return [
    {
      accessorKey: 'name',
      header: t('domains:column.name'),
      cell: ({ row }) => {
        const d = row.original
        return (
          <div className="flex min-w-0 flex-col">
            <div className="flex min-w-0 items-baseline gap-2">
              <span className="truncate font-medium text-foreground">{d.name}</span>
              {/* 被忽略的要显式标出来并带上理由：半年后没人记得当初为什么忽略，
                  而那条理由往往已经不成立了 */}
              {d.ignored ? <Badge tone="mute">{t('domains:ignored')}</Badge> : null}
            </div>
            <span className="truncate text-xs text-muted-foreground">
              {d.ignored && d.ignoreReason ? d.ignoreReason : d.dnsProvider || '—'}
            </span>
          </div>
        )
      },
    },
    {
      // 三个维度分开成列，不合成一个"健康"：
      // 域名续费、DNS、证书 —— 处理的人和动作完全不同
      id: 'expiry',
      header: t('domains:column.expiry'),
      cell: ({ row }) => expiryCell(row.original.daysLeft, row.original.expiryAt, t, labels),
      // 🔴 **必须有 accessorFn，光写 enableSorting 没用。**
      //
      //	TanStack 的判据是（table-core 8.21.3 / RowSorting.js:178）：
      //	  enableSorting !== false && table.enableSorting !== false && **!!column.accessorFn**
      //	最后一项是硬条件 —— 只有 id 的列，无论怎么设 enableSorting 都排不了。
      //	我先只加了 enableSorting，表头照样点不动，翻到库源码才看见这一条。
      //
      // ⚠️ accessorFn 返回**用于排序的值**（这里是剩余天数），
      //	显示仍走 cell —— 两者不必一致。
      accessorFn: (d) => d.daysLeft,
      enableSorting: true,
      // ⚠️ 第一次点必须是**升序**（最快到期的排最前）。
      //	TanStack 对数字列默认 sortDescFirst=true —— 点一次看到的是
      //	"最不紧急的那批"，而这一列存在的理由恰恰是回答"谁先到期"。
      //	实测过：点第一次得到 356/356/356/102，要点两次才看见 102。
      sortDescFirst: false,
      // 🔴 可排序。62 个域名默认按字母序，"谁先到期"这个问题只能靠一行行看
      //	（OPSCMDB-071）——而这一列的存在意义就是回答那个问题。
      //
      // ⚠️ null（读不出到期日）排**最后**，不能当成"很久以后"。
      //	它的真实含义是"我们不知道，它可能下周就被释放"——
      //	排到末尾至少不会被误读成安全；混在大数里则会（同 exposure 的 portRisk）。
      sortingFn: (a, b) => {
        const av = a.original.daysLeft
        const bv = b.original.daysLeft
        if (av === null && bv === null) return 0
        if (av === null) return 1
        if (bv === null) return -1
        return av - bv
      },
    },
    {
      id: 'resolve',
      header: t('domains:column.resolve'),
      cell: ({ row }) => {
        const r = row.original.resolveStatus
        if (!r) return <NoValue kind="unknown" labels={labels} />
        // 解析状态原样透传：nxdomain / timeout 是运维直接拿去 dig 的词
        return <Badge tone={r === 'ok' ? 'ok' : 'bad'}>{r}</Badge>
      },
    },
    {
      id: 'cert',
      header: t('domains:column.cert'),
      cell: ({ row }) => {
        const d = row.original
        if (d.certDaysLeft === null) {
          return (
            <div className="flex min-w-0 flex-col">
              <NoValue kind="unknown" labels={labels} />
              {d.certCheckMsg ? (
                <span className="truncate text-[11px] text-muted-foreground" title={d.certCheckMsg}>
                  {d.certCheckMsg}
                </span>
              ) : null}
            </div>
          )
        }
        return expiryCell(d.certDaysLeft, d.certExpiryAt, t, labels)
      },
      accessorFn: (d) => d.certDaysLeft, // 同上：没有 accessorFn 就排不了
      enableSorting: true,
      sortDescFirst: false, // 同上：先看最快到期的
      // 与「注册到期」同一套规矩：可排序，且 null 排最后。
      // ⚠️ 证书这一列的 null 尤其不能当大数 —— 生产上 828 条从没探测过，
      //	把它们排在"很久以后"等于给一批未知的证书发合格证（OPSCMDB-063）。
      sortingFn: (a, b) => {
        const av = a.original.certDaysLeft
        const bv = b.original.certDaysLeft
        if (av === null && bv === null) return 0
        if (av === null) return 1
        if (bv === null) return -1
        return av - bv
      },
    },
    {
      accessorKey: 'records',
      header: t('domains:column.records'),
      cell: ({ row }) => <span className="tabular">{row.original.records}</span>,
    },
    {
      // 行操作列。放最后且右对齐 —— 表格通用约定，人不用找
      id: 'actions',
      // 操作列固定在最右侧。⚠️ 标了就必须真的排在数组最后（check-action-column 守这条）
      meta: { action: true },
      header: '',
      cell: ({ row }) => <DomainRowActions d={row.original} />,
    },
  ]
}
