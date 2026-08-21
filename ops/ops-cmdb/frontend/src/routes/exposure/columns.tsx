import { Badge, type ColumnDef } from '@ops/ui'
import type { Exposure } from './queries.js'

type TFn = (key: string, opts?: Record<string, unknown>) => string

export function exposureColumns(
  t: TFn,
  /**
   * 当前结果里「归属」有几个不同的取值。
   *
   * 🔴 全表同值的列**不承载信息**，而它头上还挂着排序箭头 ——
   * 箭头承诺了一次排序，但点下去什么都不会变（OPSCMDB-031 P2-48，
   * 与告警页 P1-30 同类）。承诺做不到的事比不承诺更坏。
   *
   * ⚠️ 不删这一列：筛选之后取值可能就不止一个了，
   * 而一个会消失又出现的列比一个恒定的列更让人困惑。
   * 只关掉排序，并在表头说明"当前结果里只有一个取值"。
   */
  scopeValues: string[] = [],
): ColumnDef<Exposure>[] {
  const scopeConstant = scopeValues.length === 1
  return [
    {
      accessorKey: 'kind',
      header: t('exposure:column.kind'),
      cell: ({ row }) => <Badge tone="info">{t(`exposure:kind.${row.original.kind}`)}</Badge>,
    },
    {
      accessorKey: 'name',
      header: t('exposure:column.name'),
      cell: ({ row }) => (
        <span className="truncate font-medium text-foreground">{row.original.name}</span>
      ),
    },
    {
      accessorKey: 'endpoint',
      header: t('exposure:column.endpoint'),
      cell: ({ row }) => (
        <span className="truncate font-mono text-xs text-foreground">{row.original.endpoint}</span>
      ),
    },
    {
      accessorKey: 'ports',
      header: t('exposure:column.ports'),
      /*
        🔴 按**风险**排，不按字符串排。

        表头本来就有排序箭头，但字符串排序下 `1-65535` 和 `80-15090`
        的先后完全不反映风险大小 —— 而这一列排序的唯一用途就是
        "找出哪些开得最宽"（OPSCMDB-031 P2-47）。

        一个排得动、但排出来没有意义的箭头，比没有箭头更坏：
        它承诺了一件它做不到的事。

        排序键 = 暴露的端口数量。"没探测到"排在最后而不是当 0 ——
        当 0 会让一批未知的入口看起来最安全（这一列的三态纪律见 cell）。
      */
      sortingFn: (a, b) => portRisk(a.original.ports) - portRisk(b.original.ports),
      cell: ({ row }) => {
        const p = row.original.ports ?? ''
        if (!p) {
          // ⚠️ 空**不是"没有端口"**，是"我们没探测"。
          //
          //	一台有公网 IP 的主机开了哪些端口，完全由防火墙规则决定，
          //	而这个产品自己就有防火墙数据 —— 只是两张表没打通。
          //	原来这里渲染成「—」，与**同一行**的「防护」列自相矛盾：
          //	那一列坚持区分"不知道"，这一列却把"不知道"渲染成了像"没有"的破折号
          //	（OPSCMDB-031 P1-52）。同一行里两种纪律，读的人无从判断哪个可信。
          return (
            <span className="text-xs text-warning" title={t('exposure:portsUnknownHint')}>
              {t('exposure:portsUnknown')}
            </span>
          )
        }
        // 🔴 全端口对公网开放必须标出来。
        //
        //	实测 52 条公网入口里 **26 条**是 1-65535（占一半），
        //	而界面把它和 `80-15090`、`ESP` 用同一个灰色渲染 —— 完全看不出区别。
        //	端口范围在这个产品里一直没被当成风险维度（防火墙页也踩过同一条，P2-10）。
        const all = isAllPorts(p)
        return (
          <span className="inline-flex items-center gap-1.5">
            <span className={`font-mono text-xs ${all ? 'text-danger' : 'text-muted-foreground'}`}>
              {p}
            </span>
            {all ? (
              <span title={t('exposure:allPortsHint')}>
                <Badge tone="bad">{t('exposure:allPorts')}</Badge>
              </span>
            ) : null}
          </span>
        )
      },
    },
    {
      id: 'protected',
      header: t('exposure:column.protected'),
      cell: ({ row }) =>
        row.original.protected === null ? (
          // ⚠️ 判不了就说判不了。显示"未防护"是在出一份假的安全报告
          <span className="text-xs text-warning">{t('exposure:unknown')}</span>
        ) : row.original.protected ? (
          <Badge tone="ok">{t('exposure:protected')}</Badge>
        ) : (
          <Badge tone="bad">{t('exposure:unprotected')}</Badge>
        ),
    },
    {
      accessorKey: 'scope',
      header: scopeConstant
        ? `${t('exposure:column.scope')} · ${t('exposure:scopeConstant')}`
        : t('exposure:column.scope'),
      // 全表同值时关掉排序：箭头点了不会变，那是个假承诺
      enableSorting: !scopeConstant,
      cell: ({ row }) => <span className="text-[13px]">{row.original.scope}</span>,
    },
  ]
}

/**
 * 是不是全端口对外。
 *
 * 只认真正覆盖整个端口空间的写法 —— `1-1024` 也很宽但不是"全部"，
 * 标成全端口会造成误报，而误报会让这个标记很快被忽略。
 * （与防火墙页的 isAllPorts 判据保持一致：两处给出不同结论会让人无所适从。）
 */
function isAllPorts(ports: string) {
  return ports
    .split(/[,;\s]+/)
    .some((seg) => {
      const v = seg.trim().replace(/^(tcp|udp|esp|ah|sctp|icmp)[:/]?/i, '')
      return v === '1-65535' || v === '0-65535' || /^all$/i.test(v)
    })
}

/**
 * 端口范围的风险排序键 = 暴露了多少个端口。
 *
 * ⚠️ 「没探测到」返回 -1（排最后），不返回 0：
 * 0 会让一批**未知**的入口排在"最安全"那一端，
 * 而未知恰恰是这一页最该看的东西。
 */
function portRisk(ports: string | null | undefined): number {
  const p = (ports ?? '').trim()
  if (!p) return -1
  let total = 0
  for (const seg of p.split(/[,\s]+/)) {
    if (!seg) continue
    const m = seg.match(/^(\d+)-(\d+)$/)
    if (m) {
      total += Math.max(0, Number(m[2]) - Number(m[1]) + 1)
      continue
    }
    if (/^\d+$/.test(seg)) {
      total += 1
      continue
    }
    // ESP / AH 这类协议名没有端口概念，算 1 个入口
    total += 1
  }
  return total
}
