import { clusterLabel } from '../../lib/clusterLabel.js'
import type { Locale } from '@ops/i18n'
import { Badge, type ColumnDef } from '@ops/ui'
import type { Service } from './queries.js'

type TFn = (key: string, opts?: Record<string, unknown>) => string

export function serviceColumns(t: TFn, _locale: Locale): ColumnDef<Service>[] {
  return [
    {
      accessorKey: 'name',
      header: t('services:column.name'),
      cell: ({ row }) => {
        const s = row.original
        return (
          <div className="flex min-w-0 flex-col">
            <span className="truncate font-medium text-foreground">{s.name}</span>
            <span className="truncate text-xs text-muted-foreground">
              {s.namespace} · {clusterLabel(s.clusterDisplay, s.clusterName)}
            </span>
          </div>
        )
      },
    },
    {
      accessorKey: 'type',
      header: t('services:column.type'),
      cell: ({ row }) => {
        const s = row.original
        return (
          <div className="flex min-w-0 flex-col">
            <span className="font-mono text-xs">{s.type}</span>
            {/* ClusterIP：集群内访问这个 Service 用的 VIP。旧版有独立一列，新版丢了。
                排「Pod 连不上这个服务」时，第一步就是拿这个 IP 去 Pod 里 curl ——
                没有它只能去 kubectl 里再查一次。
                ⚠️ ExternalName 类型没有 ClusterIP，那是**正常**的，不显示即可；
                而 ClusterIP 类型却空着才是异常，所以不兜底成「—」。 */}
            {s.clusterIp !== '' && s.clusterIp !== 'None' ? (
              <span className="truncate font-mono text-[11px] text-muted-foreground">
                {s.clusterIp}
              </span>
            ) : s.clusterIp === 'None' ? (
              // Headless Service：k8s 明确写 None。它没有 VIP 是设计如此，
              // 直接说出来比留空好 —— 留空会被当成"没采到"
              <span className="text-[11px] text-muted-foreground">
                {t('services:headless')}
              </span>
            ) : null}
          </div>
        )
      },
    },
    {
      id: 'entry',
      header: t('services:column.entry'),
      cell: ({ row }) => {
        const s = row.original
        // ⚠️ 「一直没拿到外部 IP」要单独说。云厂商配额用尽、子网没空 IP 时
        // 就是这个现象，而 Service 本身看起来一切正常
        if (s.pendingLb) {
          return (
            <div className="flex min-w-0 flex-col items-start gap-0.5">
              <Badge tone="warn">{t('services:pendingLb')}</Badge>
              <span className="text-[11px] text-muted-foreground">
                {t('services:pendingLbHint')}
              </span>
            </div>
          )
        }
        if (s.hosts.length > 0) {
          return (
            <div className="flex min-w-0 flex-col">
              {s.hosts.slice(0, 2).map((h) => (
                <span key={h} className="truncate font-mono text-xs text-foreground">
                  {h}
                </span>
              ))}
              {s.hosts.length > 2 ? (
                <span className="text-[11px] text-muted-foreground">
                  {t('services:moreHosts', { count: s.hosts.length - 2 })}
                </span>
              ) : null}
            </div>
          )
        }
        if (s.externalIp !== '') {
          return (
            <div className="flex min-w-0 flex-col">
              <span className="truncate font-mono text-xs">{s.externalIp}</span>
              {/* 内网 VIP 要明说，否则它看起来和公网 IP 一模一样，
                  而两者在安全复核里的分量完全不同 */}
              {s.internalLb ? (
                <span className="text-[11px] text-muted-foreground">
                  {t('services:internalVip')}
                </span>
              ) : null}
            </div>
          )
        }
        // 内部服务没有入口是**正常**的，不是缺失
        return <span className="text-xs text-muted-foreground">{t('services:internalOnly')}</span>
      },
    },
    {
      id: 'exposed',
      header: t('services:column.exposure'),
      cell: ({ row }) => {
        const s = row.original
        if (s.pendingLb) return <Badge tone="warn">{t('services:exposure.pending')}</Badge>
        // 对外暴露用 info：这是**事实**不是故障。安全复核关心它，
        // 但标红会让整页看起来在报警
        return s.exposed ? (
          <Badge tone="info">{t('services:exposure.exposed')}</Badge>
        ) : (
          <Badge tone="mute">{t('services:exposure.internal')}</Badge>
        )
      },
    },
    {
      accessorKey: 'ports',
      header: t('services:column.ports'),
      cell: ({ row }) => (
        <span className="truncate font-mono text-xs text-muted-foreground">
          {row.original.ports || '—'}
        </span>
      ),
    },
  ]
}
