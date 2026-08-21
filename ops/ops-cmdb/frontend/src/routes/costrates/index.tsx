import { toErrorInfo } from '@ops/api'
import { tError, useTranslation } from '@ops/i18n'
import { AsyncBoundary, Badge, Banner, Dialog, EmptyState, Field, MutationError, Skeleton, TextInput, fromQuery, type LoadError } from '@ops/ui'
import { useState } from 'react'
import { RowMenu } from '../../components/RowMenu.js'
import { WriteButton } from '../../components/WriteButton.js'
import {
  type ComputeRate,
  type NodeCost,
  useNodeCosts,
  useSetNodeOverride,
  useSnapshotNow,
  type DiskRate,
  useComputeRates,
  useDeleteComputeRate,
  useDeleteDiskRate,
  useDiskRates,
  useSaveComputeRate,
  useSaveDiskRate,
} from './queries.js'

const PERM = 'cmdb:manage_cost_rates'

/**
 * 成本单价。
 *
 * ⚠️ 成本页上每一个数字都是用这里的单价算出来的，而**单价错了不会报错**——
 * 只会让整张报表安静地偏掉，然后有人拿着它去做缩容和预算决定。
 *
 * ⚠️ 区域+机型族没有匹配到单价时，那部分资源的成本是 0，
 * 在汇总里看起来就是"这批机器不花钱"。所以这一页要让缺失一眼可见。
 */
export function CostRatesPage() {
  const { t } = useTranslation()
  const compute = useComputeRates()
  const disk = useDiskRates()
  const [editCompute, setEditCompute] = useState<ComputeRate | null | undefined>(undefined)
  const [editDisk, setEditDisk] = useState<DiskRate | null | undefined>(undefined)
  const delCompute = useDeleteComputeRate()
  const snapshot = useSnapshotNow()
  const delDisk = useDeleteDiskRate()

  const toLoadError = (e: unknown): LoadError => {
    const n = toErrorInfo(e)
    return { cause: tError(t, n.messageKey, n.params), detail: n.detail, retryable: n.retryable, kind: n.kind === 'license' ? 'license' : undefined, feature: n.params?.feature as string | undefined }
  }

  return (
    <div className="mx-auto max-w-[900px] p-5">
      <div className="mb-4 flex items-baseline gap-2.5">
        <h1 className="text-sm font-semibold text-foreground">{t('costrates:title')}</h1>
        <p className="min-w-0 flex-1 text-xs text-muted-foreground">{t('costrates:hint')}</p>
        {snapshot.isSuccess ? (
          <span className="text-xs text-success">
            {t('costrates:snapshot.done', { count: snapshot.data?.count ?? 0 })}
          </span>
        ) : null}
        {snapshot.isError ? (
          <span className="max-w-[240px] truncate text-xs text-danger" title={toErrorInfo(snapshot.error).detail || undefined}>
          {tError(t, toErrorInfo(snapshot.error).messageKey, toErrorInfo(snapshot.error).params)}
        </span>
        ) : null}
        <WriteButton
          perm={PERM}
          size="sm"
          loading={snapshot.isPending}
          onClick={() => snapshot.mutate('')}
        >
          {t('costrates:snapshot.take')}
        </WriteButton>
      </div>

      <div className="mb-4">
        <Banner tone="warn">
          <span className="font-medium">{t('costrates:warn.title')}</span>
          <span className="mt-0.5 block">{t('costrates:warn.body')}</span>
        </Banner>
      </div>

      <section className="mb-5 rounded-[var(--radius-lg)] border border-border p-4">
        <div className="mb-2.5 flex items-baseline gap-2.5">
          <div className="text-xs font-medium text-foreground">{t('costrates:section.compute')}</div>
          <p className="min-w-0 flex-1 text-xs text-muted-foreground">
            {t('costrates:section.computeHint')}
          </p>
          <WriteButton perm={PERM} size="sm" onClick={() => setEditCompute(null)}>
            {t('costrates:action.addCompute')}
          </WriteButton>
        </div>

        <AsyncBoundary
          state={fromQuery(compute, (d) => d.length === 0, toLoadError)}
          errorTitle={t('costrates:error.compute')}
          retryLabel={t('common:action.retry')}
          onRetry={() => void compute.refetch()}
          pending={<Skeleton className="h-5 w-[50%]" />}
          empty={
            <EmptyState
              title={t('costrates:empty.computeTitle')}
              reason={t('costrates:empty.computeReason')}
              action={{ label: t('costrates:action.addCompute'), onClick: () => setEditCompute(null) }}
            />
          }
        >
          {(rows) => (
            <table className="w-full text-[13px]">
              <thead>
                <tr className="border-b border-border text-left text-xs text-muted-foreground">
                  <th className="py-1.5 font-medium">{t('costrates:col.region')}</th>
                  <th className="py-1.5 font-medium">{t('costrates:col.family')}</th>
                  <th className="py-1.5 text-right font-medium">{t('costrates:col.vcpuHour')}</th>
                  <th className="py-1.5 text-right font-medium">{t('costrates:col.ramHour')}</th>
                  <th />
                </tr>
              </thead>
              <tbody>
                {rows.map((r) => (
                  <tr key={r.id} className="border-b border-border last:border-0">
                    <td className="py-1.5 font-mono text-xs">{r.region}</td>
                    <td className="py-1.5 font-mono text-xs">{r.machine_family}</td>
                    <td className="tabular py-1.5 text-right">${r.vcpu_hour_usd}</td>
                    <td className="tabular py-1.5 text-right">${r.ram_gb_hour_usd}</td>
                    <td className="py-1.5 text-right">
                      <RowMenu
                        perm={PERM}
                        items={[
                          { key: 'edit', label: t('common:write.edit'), onClick: () => setEditCompute(r) },
                          {
                            key: 'delete',
                            label: t('common:write.delete'),
                            onClick: () => delCompute.mutate(r.id),
                            danger: true,
                          },
                        ]}
                      />
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          )}
        </AsyncBoundary>
      </section>

      <section className="rounded-[var(--radius-lg)] border border-border p-4">
        <div className="mb-2.5 flex items-baseline gap-2.5">
          <div className="text-xs font-medium text-foreground">{t('costrates:section.disk')}</div>
          <p className="min-w-0 flex-1 text-xs text-muted-foreground">
            {t('costrates:section.diskHint')}
          </p>
          <WriteButton perm={PERM} size="sm" onClick={() => setEditDisk(null)}>
            {t('costrates:action.addDisk')}
          </WriteButton>
        </div>

        <AsyncBoundary
          state={fromQuery(disk, (d) => d.length === 0, toLoadError)}
          errorTitle={t('costrates:error.disk')}
          retryLabel={t('common:action.retry')}
          onRetry={() => void disk.refetch()}
          pending={<Skeleton className="h-5 w-[50%]" />}
          empty={
            <EmptyState
              title={t('costrates:empty.diskTitle')}
              reason={t('costrates:empty.diskReason')}
              action={{ label: t('costrates:action.addDisk'), onClick: () => setEditDisk(null) }}
            />
          }
        >
          {(rows) => (
            <table className="w-full text-[13px]">
              <thead>
                <tr className="border-b border-border text-left text-xs text-muted-foreground">
                  <th className="py-1.5 font-medium">{t('costrates:col.region')}</th>
                  <th className="py-1.5 font-medium">{t('costrates:col.diskType')}</th>
                  <th className="py-1.5 text-right font-medium">{t('costrates:col.gbMonth')}</th>
                  <th />
                </tr>
              </thead>
              <tbody>
                {rows.map((r) => (
                  <tr key={r.id} className="border-b border-border last:border-0">
                    <td className="py-1.5 font-mono text-xs">{r.region}</td>
                    <td className="py-1.5 font-mono text-xs">{r.disk_type}</td>
                    <td className="tabular py-1.5 text-right">${r.gb_month_usd}</td>
                    <td className="py-1.5 text-right">
                      <RowMenu
                        perm={PERM}
                        items={[
                          { key: 'edit', label: t('common:write.edit'), onClick: () => setEditDisk(r) },
                          {
                            key: 'delete',
                            label: t('common:write.delete'),
                            onClick: () => delDisk.mutate(r.id),
                            danger: true,
                          },
                        ]}
                      />
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          )}
        </AsyncBoundary>
      </section>

      <NodeOverrideBlock t={t} />

      {editCompute !== undefined ? (
        <RateDialog
          kind="compute"
          initial={editCompute ?? undefined}
          onClose={() => setEditCompute(undefined)}
        />
      ) : null}
      {editDisk !== undefined ? (
        <RateDialog kind="disk" initial={editDisk ?? undefined} onClose={() => setEditDisk(undefined)} />
      ) : null}
    </div>
  )
}

function RateDialog({
  kind,
  initial,
  onClose,
}: {
  kind: 'compute' | 'disk'
  initial?: ComputeRate | DiskRate
  onClose: () => void
}) {
  const { t } = useTranslation()
  const saveCompute = useSaveComputeRate()
  const saveDisk = useSaveDiskRate()
  const m = kind === 'compute' ? saveCompute : saveDisk

  const c = initial as ComputeRate | undefined
  const d = initial as DiskRate | undefined
  const [f, setF] = useState({
    region: initial?.region ?? '',
    family: c?.machine_family ?? '',
    vcpu: String(c?.vcpu_hour_usd ?? ''),
    ram: String(c?.ram_gb_hour_usd ?? ''),
    diskType: d?.disk_type ?? '',
    gbMonth: String(d?.gb_month_usd ?? ''),
  })
  const set = <K extends keyof typeof f>(k: K, v: string) => setF((p) => ({ ...p, [k]: v }))

  const ready =
    kind === 'compute' ? f.region && f.family && f.vcpu && f.ram : f.region && f.diskType && f.gbMonth

  return (
    <Dialog
      open
      onClose={onClose}
      title={t(kind === 'compute' ? 'costrates:action.addCompute' : 'costrates:action.addDisk')}
      description={t('costrates:dialog.desc')}
      closeLabel={t('common:action.close')}
      footer={
        <>
          <WriteButton perm={PERM} size="sm" onClick={onClose}>
            {t('common:action.cancel')}
          </WriteButton>
          <WriteButton
            perm={PERM}
            variant="primary"
            size="sm"
            loading={m.isPending}
            blockedReason={ready ? undefined : t('costrates:field.required')}
            onClick={() =>
              kind === 'compute'
                ? saveCompute.mutate(
                    {
                      id: initial?.id,
                      region: f.region,
                      machine_family: f.family,
                      vcpu_hour_usd: Number(f.vcpu),
                      ram_gb_hour_usd: Number(f.ram),
                    },
                    { onSuccess: onClose },
                  )
                : saveDisk.mutate(
                    {
                      id: initial?.id,
                      region: f.region,
                      disk_type: f.diskType,
                      gb_month_usd: Number(f.gbMonth),
                    },
                    { onSuccess: onClose },
                  )
            }
          >
            {t('common:write.save')}
          </WriteButton>
        </>
      }
    >
      <div className="flex flex-col gap-3">
        <Field label={t('costrates:col.region')} hint={t('costrates:field.regionHint')} required>
          <TextInput
            value={f.region}
            onChange={(e) => set('region', e.target.value)}
            placeholder="asia-east1"
            autoFocus
          />
        </Field>
        {kind === 'compute' ? (
          <>
            <Field label={t('costrates:col.family')} hint={t('costrates:field.familyHint')} required>
              <TextInput
                value={f.family}
                onChange={(e) => set('family', e.target.value)}
                placeholder="n2"
              />
            </Field>
            <Field label={t('costrates:col.vcpuHour')} required>
              {/* 单价用 text 而不是 number：number 输入框在部分浏览器里
                  滚轮会静默改值，而这是个会影响整张成本报表的数字 */}
              <TextInput
                value={f.vcpu}
                onChange={(e) => set('vcpu', e.target.value)}
                inputMode="decimal"
                placeholder="0.0316"
              />
            </Field>
            <Field label={t('costrates:col.ramHour')} required>
              <TextInput
                value={f.ram}
                onChange={(e) => set('ram', e.target.value)}
                inputMode="decimal"
                placeholder="0.0042"
              />
            </Field>
          </>
        ) : (
          <>
            <Field label={t('costrates:col.diskType')} hint={t('costrates:field.diskTypeHint')} required>
              <TextInput
                value={f.diskType}
                onChange={(e) => set('diskType', e.target.value)}
                placeholder="pd-balanced"
              />
            </Field>
            <Field label={t('costrates:col.gbMonth')} required>
              <TextInput
                value={f.gbMonth}
                onChange={(e) => set('gbMonth', e.target.value)}
                inputMode="decimal"
                placeholder="0.10"
              />
            </Field>
          </>
        )}
        {m.isError ? (
          <MutationError error={m.error} toInfo={toErrorInfo} t={(k, p) => tError(t, k, p)} />
        ) : null}
      </div>
    </Dialog>
  )
}


/**
 * 节点成本覆盖。
 *
 * ⚠️ 默认只显示**被覆盖过的**和**没匹配到单价的**两类：
 * 全量列出来的话，几百个正常节点会把真正需要关注的那几行埋掉。
 * 而这一页要回答的恰恰是"哪些节点的成本不是按单价表算的"。
 */
function NodeOverrideBlock({ t }: { t: (k: string, p?: Record<string, unknown>) => string }) {
  const q = useNodeCosts()
  const set = useSetNodeOverride()
  const [showAll, setShowAll] = useState(false)
  const [editing, setEditing] = useState<NodeCost | null>(null)
  const [val, setVal] = useState('')

  const all = q.data ?? []
  // 需要关注的：人工覆盖的，或者算出来是 0 但又不是"本地不计费"的
  const notable = all.filter(
    (n) => n.source === 'manual' || (n.monthly === 0 && !n.source.includes('不计费')),
  )
  const rows = showAll ? all : notable

  return (
    <section className="mt-5 rounded-[var(--radius-lg)] border border-border p-4">
      <div className="mb-2.5 flex items-baseline gap-2.5">
        <div className="text-xs font-medium text-foreground">{t('costrates:section.nodes')}</div>
        <p className="min-w-0 flex-1 text-xs text-muted-foreground">
          {t('costrates:section.nodesHint')}
          {/*
            🔴 三个不同的资源数必须解释它们的关系。

            同一页上：成本总览 63 台 / 节点成本覆盖 46 个 / 台账里
            is_k8s_node 只有 31 台。三个数都对，但页面不说它们的口径，
            读的人会以为其中两个是错的（OPSCMDB-031 P2-44）。

            —— 而“怀疑数据是错的”比任何单个显示问题都贵：
            一旦不信，整页成本数据就不会被拿去做决策了。
          */}
          <span className="ml-1" title={t('costrates:nodes.countsExplainHint')}>
            {t('costrates:nodes.countsExplain')}
          </span>
        </p>
        <button
          type="button"
          onClick={() => setShowAll((v) => !v)}
          className="cursor-pointer text-xs text-brand-text underline-offset-2 hover:underline"
        >
          {showAll
            ? t('costrates:nodes.showNotable')
            : t('costrates:nodes.showAll', { count: all.length })}
        </button>
      </div>

      {q.isPending ? (
        <Skeleton className="h-5 w-[40%]" />
      ) : rows.length === 0 ? (
        <div className="py-3 text-center">
          <p className="text-xs text-muted-foreground">
            {t('costrates:nodes.allByRate', { count: all.length })}
          </p>
          {/*
            🔴 这句"没有匹配不到单价的"必须**写清楚范围**。
            它统计的是 K8s 节点，而主机台账里还有一批**非节点 VM**
            （实测 63 台主机 = 31 台节点 + 32 台非节点），它们不在这个检查里。
            于是这句话读起来像是对全部资源的保证 ——
            而实测唯一一台真的落在单价表外的主机（fgt-1-eu-west3，
            `is_k8s_node=false`）**恰好就在范围外**（OPSCMDB-031 P1-48）。
            一个诚实的局部结论，被读成了全局结论。
          */}
          <p className="mt-1 text-xs text-warning">{t('costrates:nodes.scopeCaveat')}</p>
        </div>
      ) : (
        <table className="w-full text-[13px]">
          <thead>
            <tr className="border-b border-border text-left text-xs text-muted-foreground">
              <th className="py-1.5 font-medium">{t('costrates:nodes.node')}</th>
              <th className="py-1.5 font-medium">{t('costrates:nodes.cluster')}</th>
              <th className="py-1.5 text-right font-medium">{t('costrates:nodes.monthly')}</th>
              <th className="py-1.5 font-medium">{t('costrates:nodes.source')}</th>
              <th />
            </tr>
          </thead>
          <tbody>
            {rows.map((n) => (
              <tr key={`${n.cluster_id}-${n.name}`} className="border-b border-border last:border-0">
                <td className="py-1.5 font-mono text-xs">{n.name}</td>
                <td className="py-1.5 text-xs text-muted-foreground">{n.cluster}</td>
                <td className="tabular py-1.5 text-right">${n.monthly}</td>
                <td className="py-1.5">
                  {/* source 分三态：人工覆盖 / 按费率 / 本地不计费。
                      压成一句"$0"就看不出是免费还是没匹配到单价 */}
                  {n.source === 'manual' ? (
                    <Badge tone="warn">{t('costrates:nodes.manual')}</Badge>
                  ) : n.monthly === 0 && !n.source.includes('不计费') ? (
                    <Badge tone="bad">{t('costrates:nodes.noRate')}</Badge>
                  ) : (
                    <span className="text-[11px] text-muted-foreground">{n.source}</span>
                  )}
                </td>
                <td className="py-1.5 text-right">
                  <WriteButton
                    perm={PERM}
                    size="sm"
                    onClick={() => {
                      setEditing(n)
                      setVal(n.source === 'manual' ? String(n.monthly) : '')
                    }}
                  >
                    {t('costrates:nodes.override')}
                  </WriteButton>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      )}

      {editing ? (
        <Dialog
          open
          onClose={() => setEditing(null)}
          title={t('costrates:nodes.overrideTitle', { name: editing.name })}
          description={t('costrates:nodes.overrideDesc')}
          closeLabel={t('common:action.close')}
          footer={
            <>
              <WriteButton perm={PERM} size="sm" onClick={() => setEditing(null)}>
                {t('common:action.cancel')}
              </WriteButton>
              <WriteButton
                perm={PERM}
                variant="primary"
                size="sm"
                loading={set.isPending}
                onClick={() =>
                  set.mutate(
                    {
                      cluster_id: editing.cluster_id,
                      name: editing.name,
                      monthly: Number(val) || 0,
                    },
                    { onSuccess: () => setEditing(null) },
                  )
                }
              >
                {t('common:write.save')}
              </WriteButton>
            </>
          }
        >
          <Field label={t('costrates:nodes.monthly')} hint={t('costrates:nodes.zeroHint')}>
            <TextInput value={val} onChange={(e) => setVal(e.target.value)} inputMode="decimal" />
          </Field>
        </Dialog>
      ) : null}
    </section>
  )
}
