import { clusterLabel } from '../../lib/clusterLabel.js'
import { type Locale, formatRelativeTime, useTranslation } from '@ops/i18n'
import type { ColumnDef } from '@ops/ui'
import type { K8sEvent } from './queries.js'
import { ExpandableText } from '../../components/ExpandableText.js'

type TFn = (key: string, opts?: Record<string, unknown>) => string

export function eventColumns(t: TFn): ColumnDef<K8sEvent>[] {
  return [
    {
      accessorKey: 'reason',
      header: t('events:column.reason'),
      cell: ({ row }) => (
        // 原值显示：FailedScheduling / ImagePullBackOff 是拿去 kubectl 和搜索引擎里查的词
        <span className="font-mono text-xs font-medium text-warning">{row.original.reason}</span>
      ),
    },
    {
      id: 'object',
      header: t('events:column.object'),
      cell: ({ row }) => (
        <div className="flex min-w-0 flex-col">
          <span className="truncate text-[13px] text-foreground">{row.original.obj_name}</span>
          <span className="truncate text-xs text-muted-foreground">
            {row.original.kind} · {row.original.namespace} ·{' '}
            {clusterLabel(row.original.cluster_display_name, row.original.cluster_name)}
          </span>
        </div>
      ),
    },
    {
      accessorKey: 'message',
      header: t('events:column.message'),
      cell: ({ row }) => (
        // 限宽：不限的话消息会把后面的「次数」「最后发生」挤出屏幕，
        // 而那两列恰恰是判断严重度用的。
        //
        // ⚠️ 但截断的位置正好是排障最要紧的部分（Pod 名 / Secret 名 / 容器名），
        // 所以必须能点开看全文 —— 只有 title 不够，它不是一个看得见的入口
        // （OPSCMDB-031 P2-20）
        <ExpandableText
          text={row.original.message}
          className="text-xs text-muted-foreground"
        />
      ),
    },
    {
      accessorKey: 'count',
      header: t('events:column.count'),
      cell: ({ row }) => {
        const n = row.original.count
        // 重复多次要显眼：一次 FailedScheduling 可能是巧合，300 次是持续故障
        return <span className={`tabular ${n >= 10 ? 'text-danger' : ''}`}>{n}</span>
      },
    },
    {
      id: 'last',
      header: t('events:column.last'),
      cell: ({ row }) => <LastAt at={row.original.last_at} first={row.original.first_at} />,
    },
  ]
}

/**
 * 最近一次 + 首次。
 *
 * 🔴 只显示「最近」会把持续故障读成刚发生的：
 * 一条 count=300 的 FailedScheduling，最近一次是 2 分钟前，
 * 首次可能是**三天前** —— 那是完全不同的两件事
 * （"刚出问题"去看最近的变更，"三天了"说明没人管）。
 * 旧版有这两个时间，新版只剩一个。
 *
 * ⚠️ 首次与最近相同时不重复显示：单次事件这两个值一样，
 *	重复一遍只是噪音。
 */
function LastAt({ at, first }: { at?: string; first?: string }) {
  const { t, i18n } = useTranslation()
  if (!at) return <span className="text-xs text-muted-foreground">{t('common:state.unknown')}</span>
  const locale = i18n.language as Locale
  return (
    <div className="flex min-w-0 flex-col">
      <span className="text-xs text-muted-foreground">{formatRelativeTime(at, locale)}</span>
      {first && first !== at ? (
        <span className="text-[11px] text-muted-foreground">
          {t('events:firstAt', { at: formatRelativeTime(first, locale) })}
        </span>
      ) : null}
    </div>
  )
}
