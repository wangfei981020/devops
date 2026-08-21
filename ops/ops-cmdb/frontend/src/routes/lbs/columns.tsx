import { type Locale, formatNumber } from '@ops/i18n'
import { Badge, type ColumnDef, NoValue, type NoValueKind } from '@ops/ui'
import { type LB, lbHealth } from './queries.js'

type TFn = (key: string, opts?: Record<string, unknown>) => string

export function lbColumns(
  t: TFn,
  locale: Locale,
  labels: Record<NoValueKind, string>,
): ColumnDef<LB>[] {
  return [
    {
      accessorKey: 'name',
      header: t('lbs:column.name'),
      cell: ({ row }) => {
        const l = row.original
        return (
          <div className="flex min-w-0 flex-col">
            <span className="truncate font-medium text-foreground">{l.name}</span>
            <span className="truncate font-mono text-xs text-muted-foreground">
              {l.vip}
              {l.portRange ? `:${l.portRange}` : ''} {l.protocol}
            </span>
            {/*
              ⚠️ 全端口要单独标出来。
              `1-65535` 意味着这个 VIP 上**所有端口**都对外转发，
              而它原来和普通端口一样是灰色小字（P2-10）——
              端口范围在这一页从来没被当成风险维度看过。
            */}
            {isAllPorts(l.portRange) ? (
              <span className="mt-0.5" title={t('lbs:allPortsHint')}>
                <Badge tone="warn">{t('lbs:allPorts')}</Badge>
              </span>
            ) : null}
          </div>
        )
      },
    },
    {
      id: 'scheme',
      header: t('lbs:column.scheme'),
      cell: ({ row }) => {
        const s = row.original.scheme
        // EXTERNAL 用 info 而不是 danger：对外暴露是**事实**不是故障，
        // 标红会让整页看起来在报警，真问题反而不显眼
        return <Badge tone={s === 'EXTERNAL' ? 'info' : 'mute'}>{s || '—'}</Badge>
      },
    },
    {
      id: 'backends',
      header: t('lbs:column.backends'),
      cell: ({ row }) => {
        const l = row.original
        if (l.backends === null) {
          // ⚠️ 「没采过」不是「无后端」。生产上把这两者混为一谈，
          // 曾把 8 条正在服务的 LB 显示成"后端全空"
          return <NoValue kind="notIngested" labels={labels} />
        }
        if (l.backends === 0) {
          const h = lbHealth(l)
          // ⚠️ 0 有五种原因，只有一种是"真没后端"。
          // 全部标红的后果是 37 条正常 LB 显示成故障，而真故障淹没在里面
          if (h !== 'empty') {
            return (
              <span className="flex items-baseline gap-1.5">
                <span className="tabular text-muted-foreground">0</span>
                <span className="text-[11px] text-muted-foreground" title={t(`lbs:why.${h}`)}>
                  {t(`lbs:health.${h}`)}
                  {/* 由谁承载要说出名字 —— 「由 target 承载」不带名字等于没说 */}
                  {h === 'viaTarget' && l.target ? `：${l.target}` : ''}
                  {h === 'k8s' && l.k8sService ? `：${l.k8sService}` : ''}
                </span>
              </span>
            )
          }
          return (
            <span className="flex items-baseline gap-1.5">
              <span className="tabular text-danger">0</span>
              <span className="text-[11px] text-danger">{t('lbs:noBackendHint')}</span>
            </span>
          )
        }
        return <span className="tabular">{formatNumber(l.backends, locale)}</span>
      },
    },
    {
      id: 'health',
      header: t('lbs:column.health'),
      cell: ({ row }) => {
        const h = lbHealth(row.original)
        // 只有 empty（target 为空、真的一个后端都没有）和 lost（数据丢了）标红。
        // viaTarget / k8s 是**正常形态**，标红会让整页看起来在报警
        const tone =
          h === 'empty' || h === 'lost'
            ? 'bad'
            : h === 'unknown'
              ? 'warn'
              : h === 'stale'
                ? 'mute'
                : h === 'viaTarget' || h === 'k8s'
                  ? 'info'
                  : 'ok'
        return (
          <span title={t(`lbs:why.${h}`)}>
            <Badge tone={tone}>{t(`lbs:health.${h}`)}</Badge>
          </span>
        )
      },
    },
    {
      accessorKey: 'project',
      header: t('lbs:column.project'),
      cell: ({ row }) => (
        <span className="text-[13px]">
          {row.original.project}
          <span className="ml-1.5 text-xs text-muted-foreground">{row.original.region}</span>
        </span>
      ),
    },
  ]
}

/**
 * 是不是全端口转发。
 *
 * 只认真正覆盖整个端口空间的写法。`1-1024` 也很宽但不是"全部"，
 * 标成全端口会造成误报 —— 而误报会让这个标记很快被忽略。
 */
function isAllPorts(range?: string) {
  const v = (range ?? '').trim()
  return v === '1-65535' || v === '0-65535' || v === 'ALL' || v === 'all'
}
