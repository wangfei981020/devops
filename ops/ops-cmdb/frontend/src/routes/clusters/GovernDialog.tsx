import { tError } from '@ops/i18n'
import { toErrorInfo } from '@ops/api'
import { Badge, Banner, Button, Dialog, Skeleton } from '@ops/ui'
import { useState } from 'react'
import {
  useConfigAudit,
  useNodeCapacity,
  useNsOverview,
  useOrphans,
  useSecurityAudit,
  useWorkloadChanges,
} from './govern.js'

type TFn = (k: string, o?: Record<string, unknown>) => string
type View = 'orphans' | 'capacity' | 'ns' | 'changes' | 'security' | 'config'

/**
 * 集群治理下钻。老版分散在好几个页面，这里收成一个弹窗六档。
 *
 * ⚠️ **只有当前档在取数**：六个都是实时算的，一起挂载等于每开一次弹窗
 * 给集群打六发请求，其中五发没人看。
 */
export function GovernDialog({
  clusterID,
  clusterName,
  onClose,
  t,
}: {
  clusterID: number
  clusterName: string
  onClose: () => void
  t: TFn
}) {
  const [view, setView] = useState<View>('orphans')
  const views: { key: View; label: string }[] = [
    { key: 'orphans', label: t('clusters:govern.orphans') },
    { key: 'capacity', label: t('clusters:govern.capacity') },
    { key: 'ns', label: t('clusters:govern.ns') },
    { key: 'changes', label: t('clusters:govern.changes') },
    { key: 'security', label: t('clusters:govern.security') },
    { key: 'config', label: t('clusters:govern.config') },
  ]

  return (
    <Dialog
      open
      onClose={onClose}
      title={clusterName}
      description={t('clusters:govern.desc')}
      closeLabel={t('common:action.close')}
      width={1180}
      footer={
        <Button size="sm" onClick={onClose}>
          {t('common:action.close')}
        </Button>
      }
    >
      <div className="-mx-4 -mt-1 flex flex-wrap gap-1.5 border-b border-border px-4 pb-2.5">
        {views.map((v) => (
          <Button
            key={v.key}
            size="sm"
            variant={view === v.key ? 'primary' : undefined}
            onClick={() => setView(v.key)}
          >
            {v.label}
          </Button>
        ))}
      </div>
      <div className="-mx-4">
        {view === 'orphans' ? <OrphansView cid={clusterID} t={t} /> : null}
        {view === 'capacity' ? <CapacityView cid={clusterID} t={t} /> : null}
        {view === 'ns' ? <NsView cid={clusterID} t={t} /> : null}
        {view === 'changes' ? <ChangesView cid={clusterID} t={t} /> : null}
        {view === 'security' ? <SecurityView cid={clusterID} t={t} /> : null}
        {view === 'config' ? <ConfigView cid={clusterID} t={t} /> : null}
      </div>
    </Dialog>
  )
}

const Loading = () => (
  <div className="flex flex-col gap-2 px-4 py-3">
    <Skeleton className="h-4 w-[60%]" />
    <Skeleton className="h-4 w-[40%]" />
  </div>
)

function Failed({ e, t }: { e: unknown; t: TFn }) {
  const n = toErrorInfo(e)
  return (
    <div className="px-4 py-3">
      <Banner tone="bad">
        <span className="font-medium">{t('clusters:govern.loadFailed')}</span>
        <span className="mt-0.5 block">{n.detail || tError(t, n.messageKey, n.params)}</span>
      </Banner>
    </div>
  )
}

function Empty({ text }: { text: string }) {
  return <p className="px-4 py-4 text-[13px] text-muted-foreground">{text}</p>
}

function OrphansView({ cid, t }: { cid: number; t: TFn }) {
  const q = useOrphans(cid, true)
  if (q.isPending) return <Loading />
  if (q.isError) return <Failed e={q.error} t={t} />
  const rows = q.data?.items ?? []
  if (rows.length === 0) return <Empty text={t('clusters:govern.noOrphans')} />
  return (
    <div className="max-h-[52vh] overflow-auto px-4 py-3">
      <div className="flex flex-col gap-2">
        {rows.map((o, i) => (
          <div key={`${o.kind}-${o.name}-${i}`} className="rounded-[var(--radius)] border border-border p-2.5">
            <div className="flex flex-wrap items-center gap-2">
              <Badge tone="warn">{o.kind}</Badge>
              <span className="font-mono text-xs">{o.namespace ? `${o.namespace}/` : ''}{o.name}</span>
            </div>
            <p className="mt-1 text-[13px] text-foreground">{o.reason}</p>
            {o.detail ? <p className="mt-0.5 text-xs text-muted-foreground">{o.detail}</p> : null}
            {/* 处置建议由后端给：孤儿资源该不该删要看业务，前端不替它下结论 */}
            {o.action ? <p className="mt-0.5 text-xs text-muted-foreground">{o.action}</p> : null}
          </div>
        ))}
      </div>
    </div>
  )
}

function CapacityView({ cid, t }: { cid: number; t: TFn }) {
  const q = useNodeCapacity(cid, true)
  if (q.isPending) return <Loading />
  if (q.isError) return <Failed e={q.error} t={t} />
  const rows = q.data ?? []
  if (rows.length === 0) return <Empty text={t('clusters:govern.noCapacity')} />
  return (
    <div className="max-h-[52vh] overflow-auto">
      <table className="w-full text-[13px]">
        <thead className="sticky top-0 z-10 bg-card">
          <tr className="border-b border-border text-left text-xs text-muted-foreground">
            <th className="px-4 py-2 font-medium">{t('clusters:govern.col.node')}</th>
            <th className="px-4 py-2 text-right font-medium">CPU request</th>
            <th className="px-4 py-2 text-right font-medium">CPU limit</th>
            <th className="px-4 py-2 text-right font-medium">Mem request</th>
            <th className="px-4 py-2 text-right font-medium">Mem limit</th>
          </tr>
        </thead>
        <tbody>
          {rows.map((n, i) => (
            <tr key={`${n.node}-${i}`} className="border-b border-border last:border-0">
              <td className="px-4 py-1.5 font-mono text-xs">{n.node || '—'}</td>
              <td className="tabular px-4 py-1.5 text-right text-xs">{(n.cpu_req_pct ?? 0).toFixed(1)}%</td>
              {/* limit 超 100% 是**超卖**，不是错误——但要标出来，节点压力大时它是第一嫌疑 */}
              <td className="tabular px-4 py-1.5 text-right text-xs">
                <span className={(n.cpu_lim_pct ?? 0) > 100 ? 'text-warning' : undefined}>
                  {(n.cpu_lim_pct ?? 0).toFixed(1)}%
                </span>
              </td>
              <td className="tabular px-4 py-1.5 text-right text-xs">{(n.mem_req_pct ?? 0).toFixed(1)}%</td>
              <td className="tabular px-4 py-1.5 text-right text-xs">
                <span className={(n.mem_lim_pct ?? 0) > 100 ? 'text-warning' : undefined}>
                  {(n.mem_lim_pct ?? 0).toFixed(1)}%
                </span>
              </td>
            </tr>
          ))}
        </tbody>
      </table>
      <p className="px-4 py-2 text-xs text-muted-foreground">{t('clusters:govern.capacityNote')}</p>
    </div>
  )
}

function NsView({ cid, t }: { cid: number; t: TFn }) {
  const q = useNsOverview(cid, true)
  if (q.isPending) return <Loading />
  if (q.isError) return <Failed e={q.error} t={t} />
  const rows = q.data?.failures ?? []
  if (rows.length === 0) return <Empty text={t('clusters:govern.noFailures')} />
  return (
    <div className="max-h-[52vh] overflow-auto">
      <table className="w-full text-[13px]">
        <thead className="sticky top-0 z-10 bg-card">
          <tr className="border-b border-border text-left text-xs text-muted-foreground">
            <th className="px-4 py-2 font-medium">{t('clusters:govern.col.ns')}</th>
            <th className="px-4 py-2 font-medium">{t('clusters:govern.col.pod')}</th>
            <th className="px-4 py-2 font-medium">{t('clusters:govern.col.reason')}</th>
            <th className="px-4 py-2 text-right font-medium">{t('clusters:govern.col.restarts')}</th>
          </tr>
        </thead>
        <tbody>
          {rows.map((f, i) => (
            <tr key={`${f.pod}-${i}`} className="border-b border-border last:border-0">
              <td className="px-4 py-1.5 text-xs">{f.namespace}</td>
              <td className="px-4 py-1.5 font-mono text-xs">{f.pod}</td>
              <td className="px-4 py-1.5">
                <Badge tone="bad">{f.reason || f.phase || '—'}</Badge>
              </td>
              <td className="tabular px-4 py-1.5 text-right text-xs">{f.restarts ?? 0}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  )
}

function ChangesView({ cid, t }: { cid: number; t: TFn }) {
  const q = useWorkloadChanges(cid, true)
  if (q.isPending) return <Loading />
  if (q.isError) return <Failed e={q.error} t={t} />
  const rows = q.data ?? []
  if (rows.length === 0) return <Empty text={t('clusters:govern.noChanges')} />
  return (
    <div className="max-h-[52vh] overflow-auto px-4 py-3">
      <div className="flex flex-col gap-2">
        {rows.map((c) => (
          <div key={c.id} className="rounded-[var(--radius)] border border-border p-2.5">
            <div className="flex flex-wrap items-center gap-2 text-xs">
              <span className="font-mono">{c.changed_at}</span>
              <Badge tone="mute">{c.kind}</Badge>
              <span className="font-mono">{c.namespace}/{c.name}</span>
              <Badge tone="info">{c.field}</Badge>
            </div>
            {/* 新旧值并排：排障时"改了什么"比"改过"重要得多 */}
            <div className="mt-1 flex flex-col gap-0.5 font-mono text-[11px] break-all">
              <span className="text-muted-foreground">- {c.old_value || '—'}</span>
              <span className="text-foreground">+ {c.new_value || '—'}</span>
            </div>
          </div>
        ))}
      </div>
    </div>
  )
}

function SecurityView({ cid, t }: { cid: number; t: TFn }) {
  const [includePlatform, setIncludePlatform] = useState(false)
  const q = useSecurityAudit(cid, true, includePlatform)
  if (q.isPending) return <Loading />
  if (q.isError) return <Failed e={q.error} t={t} />
  const rows = q.data?.findings ?? []
  const hidden = q.data?.platform_hidden ?? 0

  // 🔴 隐藏提示要在**空结果时也显示**。
  //	"没有发现问题" + 悄悄藏了 12 个平台组件 = 一句不成立的话，
  //	而这恰恰是最容易被当成结论拿走的一句。
  const hiddenBanner =
    hidden > 0 ? (
      <Banner
        tone="info"
        action={
          <Button size="sm" onClick={() => setIncludePlatform(true)}>
            {t('clusters:govern.showPlatform')}
          </Button>
        }
      >
        {/* ⚠️ 不用后端那句 note：它写的是"传 include_platform=1"，
            那是对 API 调用方说的话，界面上的人没法"传参数" */}
        <span>{t('clusters:govern.platformHidden', { count: hidden })}</span>
      </Banner>
    ) : null

  if (rows.length === 0) {
    return (
      <div className="flex flex-col gap-3 px-4 py-3">
        {hiddenBanner}
        <Empty text={t('clusters:govern.noFindings')} />
      </div>
    )
  }
  return (
    <div className="max-h-[52vh] overflow-auto px-4 py-3">
      <div className="flex flex-col gap-2">
        {hiddenBanner}
        {/* 外面套一层 flex：直接放在 flex-col 里会被拉满整行（实测），
            一个横贯整屏的次要按钮看着像主操作 */}
        {includePlatform ? (
          <div className="flex">
            <Button size="sm" onClick={() => setIncludePlatform(false)}>
              {t('clusters:govern.hidePlatform')}
            </Button>
          </div>
        ) : null}
        {rows.map((f, i) => (
          <div key={`${f.workload}-${i}`} className="rounded-[var(--radius)] border border-border p-2.5">
            <div className="flex flex-wrap items-center gap-2">
              <Badge tone={f.severity === 'high' ? 'bad' : f.severity === 'medium' ? 'warn' : 'mute'}>
                {f.severity}
              </Badge>
              <span className="font-mono text-xs">{f.namespace}/{f.workload || f.pod}</span>
              {/* 平台组件单独标：它们大多是装出来就这样的，和业务负载不该同等对待 */}
              {f.platform_component ? (
                <Badge tone="mute">{t('clusters:govern.platformComponent')}</Badge>
              ) : null}
              {/* 归并后的影响面。1 个不标（那就是它自己），>1 必须标出来 —— 
                  否则 16 个特权容器在界面上只是一行 */}
              {(f.pod_count ?? 0) > 1 ? (
                <span className="text-xs text-muted-foreground" title={(f.sample_pods ?? []).join('\n')}>
                  {t('clusters:govern.affectsPods', { count: f.pod_count })}
                </span>
              ) : null}
            </div>
            <ul className="mt-1 flex flex-col gap-0.5">
              {(f.risks ?? []).map((r) => (
                <li key={r} className="text-[13px] text-foreground">{r}</li>
              ))}
            </ul>
          </div>
        ))}
      </div>
    </div>
  )
}

function ConfigView({ cid, t }: { cid: number; t: TFn }) {
  const [includeUnused, setIncludeUnused] = useState(false)
  const q = useConfigAudit(cid, true, includeUnused)
  if (q.isPending) return <Loading />
  if (q.isError) return <Failed e={q.error} t={t} />
  const cap = q.data?.capability ?? {}
  const rows = q.data?.items ?? []

  return (
    <div className="flex flex-col gap-3 px-4 py-3">
      {/* ⚠️ 判定能力说明必须顶在最前：有些结论在这个集群上根本下不了
          （比如没开 secret inventory 时判不了 Secret 是否存在），
          不说清楚的话「没有发现问题」会被当成「没有问题」 */}
      {Object.keys(cap).length > 0 ? (
        <Banner tone="info">
          <span className="font-medium">{t('clusters:govern.capability')}</span>
          {Object.entries(cap)
            .filter(([, v]) => typeof v === 'string')
            .map(([k, v]) => (
              <span key={k} className="mt-0.5 block text-xs">
                {k}: {String(v)}
              </span>
            ))}
        </Banner>
      ) : null}

      {rows.length === 0 ? (
        <Empty text={t('clusters:govern.noConfigIssues')} />
      ) : (
        <div className="flex flex-col gap-2">
          {rows.map((it, i) => (
            <div key={`${it.name}-${i}`} className="rounded-[var(--radius)] border border-border p-2.5">
              <div className="flex flex-wrap items-center gap-2 text-xs">
                <Badge tone="warn">{it.kind}</Badge>
                <span className="font-mono">{it.namespace}/{it.workload || it.name}</span>
              </div>
              <p className="mt-1 text-[13px] text-foreground">{it.issue}</p>
              {it.detail ? <p className="mt-0.5 text-xs text-muted-foreground">{it.detail}</p> : null}
            </div>
          ))}
        </div>
      )}

      {/* 无人引用的 ConfigMap：清理用，默认不查（要额外遍历全部 ConfigMap）。
          ⚠️ 这一段与上面的"问题清单"是两回事：没人引用**不是问题**，
             只是可以清理的东西。混在一起会让人以为集群里多了一堆故障。 */}
      <section className="border-t border-border pt-3">
        <div className="flex flex-wrap items-center gap-2">
          <span className="text-[13px] font-medium text-foreground">
            {t('clusters:govern.unusedTitle')}
          </span>
          <Button size="sm" onClick={() => setIncludeUnused((v) => !v)}>
            {includeUnused ? t('clusters:govern.unusedHide') : t('clusters:govern.unusedShow')}
          </Button>
        </div>
        {includeUnused ? (
          q.isFetching ? (
            <Loading />
          ) : (q.data?.unused_configmaps ?? []).length === 0 ? (
            <p className="mt-1.5 text-xs text-muted-foreground">{t('clusters:govern.unusedNone')}</p>
          ) : (
            <div className="mt-2 flex flex-col gap-1.5">
              {(q.data?.unused_configmaps ?? []).map((u, i) => (
                <div
                  key={`${u.namespace}-${u.ref_name}-${i}`}
                  className="rounded-[var(--radius)] border border-border px-2.5 py-1.5"
                >
                  <span className="font-mono text-xs">
                    {u.namespace}/{u.ref_name}
                  </span>
                  {u.detail ? (
                    <p className="mt-0.5 text-xs text-muted-foreground">{u.detail}</p>
                  ) : null}
                </div>
              ))}
            </div>
          )
        ) : (
          <p className="mt-1.5 text-xs text-muted-foreground">{t('clusters:govern.unusedHint')}</p>
        )}
      </section>
    </div>
  )
}
