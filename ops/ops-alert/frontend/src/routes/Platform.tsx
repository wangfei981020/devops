import { useTranslation } from '@ops/i18n'
import { useQuery } from '@tanstack/react-query'
import {
  AsyncBoundary,
  Badge,
  DataTable,
  EmptyState,
  Skeleton,
  TableSkeleton,
  type ColumnDef,
  fromQuery,
} from '@ops/ui'
import { get, makeLoadError } from '../lib/api.js'

type SelfCheck = {
  datasources: { total: number; down: number }
  rules: { total: number; failing: number }
  notify_failed_24h: number
  silent_sources: string[]
}

/**
 * 系统自检：告警系统本身还活着吗。
 *
 * 开源方案普遍要靠外部 Dead man's switch 手工搭，而「没有告警」
 * 和「告警系统坏了」在界面上长得一模一样。这一页就是把两者分开。
 */
export function SelfCheckPage() {
  const { t } = useTranslation()
  const query = useQuery({
    queryKey: ['selfcheck'],
    queryFn: () => get<SelfCheck>('/selfcheck'),
    refetchInterval: 30_000,
  })

  if (query.isPending) return <Skeleton className="h-40 w-full" />
  if (query.isError) {
    return (
      <div className="rounded-md border border-danger/40 bg-danger-bg p-4 text-sm text-danger">
        {t('opsalert:selfcheck.failed', { error: String(query.error) })}
      </div>
    )
  }
  const d = query.data
  const healthy = d.datasources.down === 0 && d.rules.failing === 0 && d.silent_sources.length === 0

  return (
    <div className="flex flex-col gap-4">
      <div
        className={`rounded-lg border p-4 ${healthy ? 'border-success/50 bg-success/5' : 'border-warning/50 bg-warning/10'}`}
      >
        <div className="text-sm font-semibold">
          {healthy ? t('opsalert:selfcheck.healthy') : t('opsalert:selfcheck.unhealthy')}
        </div>
      </div>

      <div className="grid grid-cols-2 gap-3 md:grid-cols-4">
        <Card
          label={t('opsalert:selfcheck.datasources')}
          value={`${d.datasources.total - d.datasources.down}/${d.datasources.total}`}
          bad={d.datasources.down > 0}
          foot={
            d.datasources.down > 0
              ? t('opsalert:selfcheck.dsDown', { count: d.datasources.down })
              : t('opsalert:selfcheck.dsAllUp')
          }
        />
        <Card
          label={t('opsalert:selfcheck.rules')}
          value={`${d.rules.total - d.rules.failing}/${d.rules.total}`}
          bad={d.rules.failing > 0}
          foot={
            d.rules.failing > 0
              ? t('opsalert:selfcheck.rulesFailing', { count: d.rules.failing })
              : t('opsalert:selfcheck.rulesAllOk')
          }
        />
        <Card
          label={t('opsalert:selfcheck.notifyFailed')}
          value={d.notify_failed_24h}
          bad={d.notify_failed_24h > 0}
          foot={t('opsalert:selfcheck.notifyHint')}
        />
        <Card
          label={t('opsalert:selfcheck.silentSources')}
          value={d.silent_sources.length}
          bad={d.silent_sources.length > 0}
          foot={t('opsalert:selfcheck.silentHint')}
        />
      </div>

      {d.silent_sources.length > 0 && (
        <section className="rounded-lg border border-border p-4">
          <h3 className="text-sm font-semibold">{t('opsalert:selfcheck.silentDetail')}</h3>
          <ul className="mt-2 flex flex-col gap-1 text-xs">
            {d.silent_sources.map((s) => (
              <li key={s}>
                <span className="font-mono">{s}</span>
                <span className="ml-2 text-muted-foreground">{t('opsalert:selfcheck.silentDetailHint')}</span>
              </li>
            ))}
          </ul>
        </section>
      )}
    </div>
  )
}

function Card({ label, value, foot, bad }: { label: string; value: number | string; foot: string; bad: boolean }) {
  return (
    <div className={`rounded-lg border p-4 ${bad ? 'border-danger/40 bg-danger-bg' : 'border-border'}`}>
      <div className="text-xs text-muted-foreground">{label}</div>
      <div className="mt-1 text-2xl font-semibold tabular-nums">{value}</div>
      <div className="mt-1 text-xs text-muted-foreground">{foot}</div>
    </div>
  )
}

type Audit = {
  actor: string
  action: string
  target_type: string
  target_id: string
  ip: string
  at: string
}

export function AuditPage() {
  const { t } = useTranslation()
  const query = useQuery({ queryKey: ['audit'], queryFn: () => get<{ items: Audit[] }>('/audit') })

  const columns: ColumnDef<Audit, unknown>[] = [
    { header: t('opsalert:audit.colTime'), accessorKey: 'at', cell: ({ row }) => <span className="tabular-nums">{new Date(row.original.at).toLocaleString()}</span> },
    { header: t('opsalert:audit.colActor'), accessorKey: 'actor' },
    { header: t('opsalert:audit.colAction'), accessorKey: 'action', cell: ({ row }) => <Badge tone="mute" dot={false}>{row.original.action}</Badge> },
    { header: t('opsalert:audit.colTarget'), id: 'target', cell: ({ row }) => <span className="font-mono text-xs">{row.original.target_type}#{row.original.target_id}</span> },
    { header: t('opsalert:audit.colIp'), accessorKey: 'ip', cell: ({ row }) => <span className="font-mono text-xs">{row.original.ip}</span> },
  ]

  return (
    <AsyncBoundary
      state={fromQuery(query, (d) => d.items.length === 0, makeLoadError(t))}
      pending={<TableSkeleton columns={[20, 14, 20, 30, 16]} rows={8} />}
      empty={
        <EmptyState
          title={t('opsalert:audit.emptyTitle')}
          reason={t('opsalert:audit.emptyReason')}
          action={null}
        />
      }
      errorTitle={t('opsalert:audit.loadError')}
      retryLabel={t('action.retry')}
      onRetry={() => query.refetch()}
    >
      {(data) => <DataTable columns={columns} data={data.items} rowKey={(r) => `${r.at}-${r.action}-${r.target_id}`} />}
    </AsyncBoundary>
  )
}
