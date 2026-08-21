import { type Locale, formatNumber, formatRelativeTime } from '@ops/i18n'
import { clusterLabel } from '../../lib/clusterLabel.js'
import { Badge, type ColumnDef, NoValue, type NoValueKind } from '@ops/ui'
import { useState } from 'react'
import { SyncStateDialog } from './SyncStateDialog.js'
import type { Cluster } from './queries.js'

type TFn = (key: string, opts?: Record<string, unknown>) => string

export function clusterColumns(
  t: TFn,
  locale: Locale,
  labels: Record<NoValueKind, string>,
): ColumnDef<Cluster>[] {
  /**
   * 计数单元格。
   *
   * ⚠️ 三种情况必须分开渲染：
   *   null      没采到 → 「未接入」（警告色，这是采集缺口）
   *   0         采到了，确实是空的 → 正常的 0
   *   n / total 有分母时显示"就绪/总数"，只显示总数会藏掉 NotReady
   */
  const count = (value: number | null, ok?: number | null) => {
    if (value === null) return <NoValue kind="notIngested" labels={labels} />
    if (ok === null || ok === undefined || ok === value) {
      // 全就绪时只显示总数 —— 4 个集群全写成 18/18 会更难扫。
      //
      // ⚠️ 但那个数字必须**说得清是什么**：列头原来写「节点(就绪/总数)」，
      //	承诺两个值却只给一个，读的人无法判断 18 是"18 台就绪"还是"共 18 台"
      //	（OPSCMDB-031 P1-18）。列头已改成不承诺，这里再补一个 title 兜底。
      return (
        <span className="tabular" title={t('clusters:allReadyHint')}>
          {formatNumber(value, locale)}
        </span>
      )
    }
    return (
      <span className="tabular">
        <span className="text-warning">{formatNumber(ok, locale)}</span>
        <span className="text-muted-foreground"> / {formatNumber(value, locale)}</span>
      </span>
    )
  }

  return [
    {
      accessorKey: 'displayName',
      header: t('clusters:column.name'),
      cell: ({ row }) => {
        const c = row.original
        return (
          <div className="flex min-w-0 items-baseline gap-2">
            <span className="truncate font-medium text-foreground">{c.displayName}</span>
            {/* 展示名和真名不同时要把真名带上：kubectl 的 context、
                告警里的 cluster 标签用的都是真名，只显示展示名对不上 */}
            {c.name !== c.displayName ? (
              <span className="truncate font-mono text-xs text-muted-foreground">{c.name}</span>
            ) : null}
            {!c.enabled ? <Badge tone="mute">{t('clusters:disabled')}</Badge> : null}
          </div>
        )
      },
    },
    {
      accessorKey: 'environment',
      header: t('clusters:column.env'),
      // 环境值原样显示（PROD/UAT），不加"（生产）"之类的解释
      cell: ({ row }) => <span className="font-mono text-xs">{row.original.environment}</span>,
    },
    {
      accessorKey: 'location',
      header: t('clusters:column.location'),
      cell: ({ row }) => (
        <span className="text-xs text-muted-foreground">
          {row.original.provider}
          {row.original.location ? ` · ${row.original.location}` : ''}
        </span>
      ),
    },
    {
      id: 'nodes',
      header: t('clusters:column.nodes'),
      cell: ({ row }) => count(row.original.nodes, row.original.nodesReady),
    },
    {
      id: 'pods',
      header: t('clusters:column.pods'),
      cell: ({ row }) => {
        const c = row.original
        if (c.pods === null) return <NoValue kind="notIngested" labels={labels} />
        return (
          <span className="tabular">
            {formatNumber(c.pods, locale)}
            {/* 异常数只在 >0 时出现。恒定显示 "0 异常" 会让这一列
                天天有个数字在那儿，真出问题时反而不显眼 */}
            {c.podsBad !== null && c.podsBad > 0 ? (
              <span className="ml-1.5 text-warning">
                {t('clusters:podsBad', { count: c.podsBad })}
              </span>
            ) : null}
          </span>
        )
      },
    },
    {
      id: 'version',
      header: t('clusters:column.version'),
      cell: ({ row }) => {
        const vs = row.original.kubeletVersions
        if (vs.length === 0) {
          // ⚠️ 两种空要分开：集群整个没采到 → 未接入；
          // 采到了但节点上没有版本字段 → 未知（采集链路在跑，只是这一项缺）。
          // 都写成"未接入"会让人去查集群连通性，而问题其实在采集器少取了一个字段
          return <NoValue kind={row.original.ingested ? 'unknown' : 'notIngested'} labels={labels} />
        }
        if (vs.length === 1) return <span className="font-mono text-xs">{vs[0]}</span>
        // 多版本并存 = 正在升级，或者升级卡住了。
        // 只显示一个（比如最高的）会把"卡住"这件事藏起来，
        // 而那正是版本这一列存在的理由
        // ⚠️ 光一个橙色的 `+1` 看不懂：新用户不知道它是"还有 1 个其它版本"
        //	（OPSCMDB-031 P2-14）。title 里把完整清单和它的含义一起给出来 ——
        //	多版本并存 = 正在升级，或者升级卡住了，而后者正是这一列存在的理由
        return (
          <span
            className="font-mono text-xs text-warning"
            title={t('clusters:versionDriftHint', { n: vs.length, list: vs.join(', ') })}
          >
            {vs[0]} {t('clusters:versionMore', { n: vs.length - 1 })}
          </span>
        )
      },
    },
    {
      id: 'synced',
      header: t('clusters:column.synced'),
      cell: ({ row }) => {
        const c = row.original
        if (!c.syncedAt) {
          // 从没采到过。与"采过但很久没采了"不同：后者有时间戳可看
          return <NoValue kind="notIngested" labels={labels} />
        }
        return (
          <span className="text-xs text-muted-foreground">
            {formatRelativeTime(c.syncedAt, locale)}
          </span>
        )
      },
    },
    {
      // 采集明细下钻。整个 CMDB 的结论都建立在"采到的数据是新的"这个前提上，
      // 而「最后同步 3 分钟前」这一个数字看不出**哪一类资源**没采到
      id: 'sync',
      header: '',
      cell: ({ row }) => <SyncButton c={row.original} label={t('clusters:sync.entry')} />,
    },
  ]
}

function SyncButton({ c, label }: { c: Cluster; label: string }) {
  const [open, setOpen] = useState(false)
  return (
    <>
      <button
        type="button"
        onClick={() => setOpen(true)}
        className="cursor-pointer rounded-[var(--radius)] px-1.5 py-0.5 text-[11px] text-brand underline-offset-2 hover:underline"
      >
        {label}
      </button>
      {open ? (
        <SyncStateDialog
          clusterId={c.id}
          clusterName={clusterLabel(c.displayName, c.name)}
          onClose={() => setOpen(false)}
        />
      ) : null}
    </>
  )
}
