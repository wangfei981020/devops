import { tError , formatList } from '@ops/i18n'
import { toErrorInfo } from '@ops/api'
import { Badge, Banner, Button, Dialog, DrawerSection, Select, Skeleton } from '@ops/ui'
import { useState } from 'react'
import { ScheduleBody } from './ScheduleDialog.js'
import {
  type Coverage,
  useAvailableVersions,
  useRepairHistory,
  useUpgradeHistory,
  useUpgradePlan,
  useUpgradeProgress,
} from './detail.js'

type TFn = (k: string, o?: Record<string, unknown>) => string
type View = 'history' | 'repair' | 'progress' | 'plan' | 'versions' | 'schedule'

/**
 * 一个集群的升级下钻。
 *
 * # 为什么是大弹窗而不是侧边抽屉
 *
 * 这里每一档都是**宽表**：升级历史有「起止时间 / 范围 / 池 / 谁发起 / 状态 /
 * 版本变化 / 耗时 / 来源」八列，节点池预案还要并排看多个池。
 * 抽屉宽度撑死 860，这些内容会挤成竖着的一条，看两行就要横向找。
 * 用户原话：「侧边的看有点小……现在看的不方便」。
 *
 * 弹窗给到 1240 且有 `max-w-[92vw]` 兜底，小屏自动收窄，不会顶出屏幕。
 *
 * # 为什么把官网排期表也收进来
 *
 * 卡片上那句「预计 X 月被自动升级」的依据就是排期表。
 * 分成两个入口的话，人得先关掉这个再去点另一个按钮，然后回来对。
 *
 * ⚠️ 六档共用一个弹窗，但**只有当前这一档在取数**（各 hook 的 enabled）。
 * 尤其「实时进度」是 15 秒轮询——六档一起挂载等于平时也在空转。
 */
export function ClusterDetailDialog({
  clusterID,
  clusterName,
  currentVersion,
  initialView = 'history',
  onClose,
  t,
}: {
  clusterID: number
  clusterName: string
  currentVersion?: string
  /** 从「官网排期表」按钮进来时直接落在排期那一档 */
  initialView?: View
  onClose: () => void
  t: TFn
}) {
  const [view, setView] = useState<View>(initialView)

  const views: { key: View; label: string }[] = [
    { key: 'history', label: t('upgrades:detail.history') },
    { key: 'repair', label: t('upgrades:detail.repair') },
    { key: 'progress', label: t('upgrades:detail.progress') },
    { key: 'plan', label: t('upgrades:detail.plan') },
    { key: 'versions', label: t('upgrades:detail.versions') },
    { key: 'schedule', label: t('upgrades:schedule.title') },
  ]

  return (
    <Dialog
      open
      onClose={onClose}
      title={clusterName}
      description={
        currentVersion ? t('upgrades:detail.subtitle', { version: currentVersion }) : undefined
      }
      closeLabel={t('common:action.close')}
      width={1240}
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

      {/* 各档自己带 px-4，这里把弹窗内边距抵消掉，表格才能铺满可用宽度 */}
      <div className="-mx-4">
        {view === 'history' ? <HistoryView clusterID={clusterID} t={t} /> : null}
        {view === 'repair' ? <RepairView clusterID={clusterID} t={t} /> : null}
        {view === 'progress' ? <ProgressView clusterID={clusterID} t={t} /> : null}
        {view === 'plan' ? <PlanView clusterID={clusterID} t={t} /> : null}
        {view === 'versions' ? <VersionsView clusterID={clusterID} t={t} /> : null}
        {/* 排期表是全局数据，不按集群过滤 —— 它解释的是卡片上那个预测日期的来源 */}
        {view === 'schedule' ? (
          <div className="px-4 py-3">
            <ScheduleBody />
          </div>
        ) : null}
      </div>
    </Dialog>
  )
}

/**
 * 覆盖范围说明。
 *
 * ⚠️ 这一段是这几个历史页面里**最重要**的内容，不是脚注。
 * 后端专门写了这段话来防止「一条都没有」被读成「没发生过」——
 * GCP 的 operations 保留期只有两周量级，早于首次采集的历史根本找不回来。
 */
function CoverageNote({ cov, t }: { cov?: Coverage; t: TFn }) {
  if (!cov) return null
  return (
    <div className="px-4 py-2.5">
      <Banner tone={cov.total === 0 ? 'warn' : 'info'}>
        <span className="font-medium">
          {t('upgrades:detail.coverage', {
            total: cov.total,
            earliest: cov.earliest || '—',
            latest: cov.latest || '—',
          })}
        </span>
        {cov.note ? <span className="mt-0.5 block">{cov.note}</span> : null}
        {cov.truncated_note ? <span className="mt-0.5 block">{cov.truncated_note}</span> : null}
        {cov.reason_note ? <span className="mt-0.5 block">{cov.reason_note}</span> : null}
      </Banner>
    </div>
  )
}

/** 取数失败要说出来，不能退化成"没有记录"。 */
function Failed({ e, t }: { e: unknown; t: TFn }) {
  const n = toErrorInfo(e)
  return (
    <div className="px-4 py-3">
      <Banner tone="bad">
        <span className="font-medium">{t('upgrades:detail.loadFailed')}</span>
        <span className="mt-0.5 block">{n.detail || tError(t, n.messageKey, n.params)}</span>
      </Banner>
    </div>
  )
}

function Loading() {
  return (
    <div className="flex flex-col gap-2 px-4 py-3">
      <Skeleton className="h-4 w-[70%]" />
      <Skeleton className="h-4 w-[45%]" />
      <Skeleton className="h-4 w-[60%]" />
    </div>
  )
}

function HistoryView({ clusterID, t }: { clusterID: number; t: TFn }) {
  const q = useUpgradeHistory(clusterID)
  if (q.isPending) return <Loading />
  if (q.isError) return <Failed e={q.error} t={t} />
  const d = q.data
  if (d?.ok === false) return <Failed e={new Error(d.error)} t={t} />
  const rows = d?.rows ?? []
  const stat = d?.stat ?? {}

  return (
    <>
      <CoverageNote cov={d?.coverage} t={t} />
      {rows.length > 0 ? (
        <DrawerSection title={t('upgrades:detail.startTypeStat')}>
          <div className="flex flex-wrap gap-2 text-xs">
            {/* 自动升级是这一页的重点：被 Google 自动升的次数说明我们对版本没有控制权 */}
            <Badge tone={(stat.AUTOMATIC ?? 0) > 0 ? 'warn' : 'mute'}>
              {t('upgrades:detail.auto', { n: stat.AUTOMATIC ?? 0 })}
            </Badge>
            <Badge tone="mute">{t('upgrades:detail.manual', { n: stat.MANUAL ?? 0 })}</Badge>
            {(stat.UNKNOWN ?? 0) > 0 ? (
              // 来源没给 startType 时如实标未知，不能算进自动或手动
              <Badge tone="mute">{t('upgrades:detail.unknownType', { n: stat.UNKNOWN })}</Badge>
            ) : null}
            {(stat.FAILED ?? 0) > 0 ? (
              <Badge tone="bad">{t('upgrades:detail.failed', { n: stat.FAILED })}</Badge>
            ) : null}
          </div>
        </DrawerSection>
      ) : null}
      {rows.map((r, i) => (
        <DrawerSection
          key={`${r.started_at}-${r.pool}-${i}`}
          title={`${r.started_at || '—'} · ${r.scope}${r.pool ? ` / ${r.pool}` : ''}`}
        >
          <div className="flex flex-wrap items-center gap-2 text-[13px]">
            <Badge tone={r.start_type === 'AUTOMATIC' ? 'warn' : r.start_type ? 'mute' : 'mute'}>
              {r.start_type || t('upgrades:detail.unknown')}
            </Badge>
            <Badge tone={r.state === 'FAILED' ? 'bad' : r.state === 'DONE' ? 'ok' : 'mute'}>
              {r.state || '—'}
            </Badge>
            <span className="font-mono text-xs text-muted-foreground">
              {r.initial_version || '—'} → {r.target_version || '—'}
            </span>
            {r.duration ? (
              <span className="text-xs text-muted-foreground">{r.duration}</span>
            ) : null}
            {r.source ? <span className="text-[11px] text-muted-foreground">{r.source}</span> : null}
          </div>
          {r.detail ? (
            <p className="mt-1 text-xs break-words text-muted-foreground">{r.detail}</p>
          ) : null}
        </DrawerSection>
      ))}
    </>
  )
}

function RepairView({ clusterID, t }: { clusterID: number; t: TFn }) {
  const q = useRepairHistory(clusterID)
  if (q.isPending) return <Loading />
  if (q.isError) return <Failed e={q.error} t={t} />
  const d = q.data
  if (d?.ok === false) return <Failed e={new Error(d.error)} t={t} />
  const rows = d?.rows ?? []

  return (
    <>
      <CoverageNote cov={d?.coverage} t={t} />
      {rows.map((r, i) => (
        <DrawerSection key={`${r.op_name}-${i}`} title={`${r.started_at || '—'} · ${r.node_name}`}>
          <div className="flex flex-wrap items-center gap-2 text-[13px]">
            <Badge tone={r.status === 'DONE' ? 'ok' : 'mute'}>{r.status || '—'}</Badge>
            {r.pool ? <span className="text-xs text-muted-foreground">{r.pool}</span> : null}
            {r.duration ? <span className="text-xs text-muted-foreground">{r.duration}</span> : null}
          </div>
          {/* 原因解析不出来时留空并说明，不能显示成"没有原因" */}
          <p className="mt-1 text-xs text-foreground">
            {r.repair_reason || t('upgrades:detail.reasonUnparsed')}
          </p>
          {r.status_message || r.detail ? (
            <p className="mt-1 text-xs break-words text-muted-foreground">
              {r.status_message || r.detail}
            </p>
          ) : null}
        </DrawerSection>
      ))}
    </>
  )
}

function ProgressView({ clusterID, t }: { clusterID: number; t: TFn }) {
  const [hours, setHours] = useState(24)
  const q = useUpgradeProgress(clusterID, hours, true)
  return (
    <>
      <div className="flex items-center gap-2 px-4 py-2.5">
        <Select<string>
          label={t('upgrades:detail.window')}
          value={String(hours)}
          onChange={(v) => setHours(Number(v))}
          options={[
            { value: '6', label: t('upgrades:detail.h6') },
            { value: '24', label: t('upgrades:detail.h24') },
            { value: '72', label: t('upgrades:detail.h72') },
            { value: '168', label: t('upgrades:detail.h168') },
          ]}
        />
        <span className="text-xs text-muted-foreground">{t('upgrades:detail.livePolling')}</span>
      </div>
      {q.isPending ? <Loading /> : null}
      {q.isError ? <Failed e={q.error} t={t} /> : null}
      {q.data ? (
        <>
          {/* ⚠️ 采集不正常时，"没有事件"不代表没在升级 */}
          {!q.data.collection_healthy ? (
            <div className="px-4 py-2.5">
              <Banner tone="warn">
                <span className="font-medium">{t('upgrades:detail.collectionBroken')}</span>
                <span className="mt-0.5 block">{q.data.collection_note}</span>
              </Banner>
            </div>
          ) : null}
          {(q.data.warnings ?? []).length > 0 ? (
            <div className="px-4 pb-2.5">
              <Banner tone="warn">
                {q.data.warnings.map((w) => (
                  <span key={w} className="block">
                    {w}
                  </span>
                ))}
              </Banner>
            </div>
          ) : null}
          {q.data.pools.map((p) => (
            <DrawerSection key={p.pool} title={p.pool}>
              <div className="flex flex-wrap gap-x-4 gap-y-1 text-[13px]">
                <span>{t('upgrades:detail.paceNodes', { n: p.nodes, b: p.batches })}</span>
                <span>{t('upgrades:detail.paceBatchSize', { n: p.batch_size })}</span>
                <span>{t('upgrades:detail.paceMedian', { n: p.median_batch_minutes })}</span>
                <span>{t('upgrades:detail.paceSlowest', { n: p.slowest_batch_minutes })}</span>
                <span className="font-medium">
                  {t('upgrades:detail.paceTotal', { n: p.total_minutes })}
                </span>
              </div>
              {p.note ? <p className="mt-1 text-xs text-muted-foreground">{p.note}</p> : null}
            </DrawerSection>
          ))}
          <DrawerSection title={t('upgrades:detail.events', { n: q.data.events.length })}>
            {q.data.events.length === 0 ? (
              <p className="text-xs text-muted-foreground">{t('upgrades:detail.noEvents')}</p>
            ) : (
              <div className="flex flex-col gap-1">
                {q.data.events.slice(0, 200).map((e, i) => (
                  <div key={`${e.node}-${e.at}-${i}`} className="flex flex-wrap gap-2 text-xs">
                    <span className="text-muted-foreground">{e.at}</span>
                    <span className="font-mono">{e.node}</span>
                    <Badge tone="mute">{e.event}</Badge>
                    {e.from || e.to ? (
                      <span className="font-mono text-muted-foreground">
                        {e.from || '—'} → {e.to || '—'}
                      </span>
                    ) : null}
                  </div>
                ))}
              </div>
            )}
          </DrawerSection>
          {q.data.extrapolate ? (
            <DrawerSection title={t('upgrades:detail.extrapolate')}>
              <p className="text-xs text-muted-foreground">{q.data.extrapolate}</p>
            </DrawerSection>
          ) : null}
        </>
      ) : null}
    </>
  )
}

function PlanView({ clusterID, t }: { clusterID: number; t: TFn }) {
  const versions = useAvailableVersions(clusterID, 'master')
  const [target, setTarget] = useState('')
  const q = useUpgradePlan(clusterID, target)

  return (
    <>
      <div className="flex flex-wrap items-center gap-2 px-4 py-2.5">
        <Select<string>
          label={t('upgrades:detail.target')}
          value={target}
          onChange={setTarget}
          options={[
            { value: '', label: t('upgrades:detail.targetInferred') },
            ...(versions.data?.versions ?? []).map((v) => ({ value: v, label: v })),
          ]}
        />
        {/* 版本清单没采到时必须说，否则下拉框空着会被读成"没有可升的版本" */}
        {versions.data?.note ? (
          <span className="max-w-[520px] text-xs text-warning">{versions.data.note}</span>
        ) : null}
      </div>
      {q.isPending ? <Loading /> : null}
      {q.isError ? <Failed e={q.error} t={t} /> : null}
      {q.data ? (
        <>
          {q.data.error ? (
            <div className="px-4 py-2.5">
              <Banner tone="warn">{q.data.error}</Banner>
            </div>
          ) : null}
          {(q.data.warnings ?? []).length > 0 ? (
            <div className="px-4 pb-2.5">
              <Banner tone="warn">
                {q.data.warnings?.map((w) => (
                  <span key={w} className="block">
                    {w}
                  </span>
                ))}
              </Banner>
            </div>
          ) : null}
          {/* ⚠️ 没采到 PDB 时，「无阻塞」这个结论不成立 */}
          {q.data.pdbs_collected === false ? (
            <div className="px-4 pb-2.5">
              <Banner tone="warn">
                <span className="font-medium">{t('upgrades:detail.pdbNotCollected')}</span>
                {q.data.pdb_note ? <span className="mt-0.5 block">{q.data.pdb_note}</span> : null}
              </Banner>
            </div>
          ) : null}
          {q.data.total_estimate ? (
            <DrawerSection title={t('upgrades:detail.totalEstimate')}>
              <EstimateLine est={q.data.total_estimate} t={t} />
            </DrawerSection>
          ) : null}
          {(q.data.pools ?? []).map((p) => (
            <DrawerSection key={p.name} title={`${p.name} · ${p.node_count} ${t('upgrades:detail.nodesUnit')}`}>
              <div className="flex flex-wrap gap-x-4 gap-y-1 text-[13px]">
                <span className="font-mono text-xs">{p.current_version || '—'}</span>
                <Badge tone="mute">{p.strategy || '—'}</Badge>
                {p.extra_nodes_needed > 0 ? (
                  // 配额不够会直接把升级卡住，这个数字必须显眼
                  <Badge tone="warn">
                    {t('upgrades:detail.extraNodes', { n: p.extra_nodes_needed })}
                  </Badge>
                ) : null}
              </div>
              <div className="mt-1">
                <EstimateLine est={p.estimate} t={t} />
              </div>
              {p.quota_note ? (
                <p className="mt-1 text-xs text-muted-foreground">{p.quota_note}</p>
              ) : null}
            </DrawerSection>
          ))}
          {(q.data.blocking_pdbs ?? []).length > 0 ? (
            <DrawerSection title={t('upgrades:detail.blockingPdbs')}>
              {q.data.blocking_pdbs?.map((p, i) => (
                <p key={`${p.namespace}-${p.name}-${i}`} className="text-xs text-danger">
                  {p.namespace}/{p.name} {p.note ? `· ${p.note}` : ''}
                </p>
              ))}
            </DrawerSection>
          ) : null}
          {(q.data.console_steps ?? []).length > 0 ? (
            <DrawerSection title={t('upgrades:detail.consoleSteps')}>
              <ol className="flex list-decimal flex-col gap-1 pl-4 text-xs text-foreground">
                {q.data.console_steps?.map((s) => (
                  <li key={s}>{s}</li>
                ))}
              </ol>
            </DrawerSection>
          ) : null}
          {(q.data.verification ?? []).length > 0 ? (
            <DrawerSection title={t('upgrades:detail.verification')}>
              <ul className="flex list-disc flex-col gap-1 pl-4 text-xs text-foreground">
                {q.data.verification?.map((s) => (
                  <li key={s}>{s}</li>
                ))}
              </ul>
            </DrawerSection>
          ) : null}
        </>
      ) : null}
    </>
  )
}

/**
 * 耗时估算一行。
 *
 * ⚠️ `basis` 和 `measured` 必须显示：**经验区间和实测值不是一回事**。
 * 拿经验区间当实测去排停机窗口，是这个页面最容易造成的实际损失。
 */
function EstimateLine({ est, t }: { est: { min_minutes: number; max_minutes: number; basis: string; batches: number; incomplete?: string[]; measured: boolean }; t: TFn }) {
  return (
    <div className="text-[13px]">
      <span className="font-medium">
        {t('upgrades:detail.estimateRange', { min: est.min_minutes, max: est.max_minutes })}
      </span>
      <Badge tone={est.measured ? 'ok' : 'warn'}>
        {est.measured ? t('upgrades:detail.measured') : t('upgrades:detail.estimated')}
      </Badge>
      {est.basis ? <span className="ml-2 text-xs text-muted-foreground">{est.basis}</span> : null}
      {(est.incomplete ?? []).length > 0 ? (
        <p className="mt-0.5 text-xs text-warning">
          {t('upgrades:detail.incomplete', { list: formatList(t, est.incomplete ?? []) })}
        </p>
      ) : null}
    </div>
  )
}

function VersionsView({ clusterID, t }: { clusterID: number; t: TFn }) {
  const [kind, setKind] = useState<'master' | 'node'>('master')
  const q = useAvailableVersions(clusterID, kind)
  return (
    <>
      <div className="flex items-center gap-2 px-4 py-2.5">
        <Select<string>
          label={t('upgrades:detail.kind')}
          value={kind}
          onChange={(v) => setKind(v as 'master' | 'node')}
          options={[
            { value: 'master', label: t('upgrades:detail.kindMaster') },
            { value: 'node', label: t('upgrades:detail.kindNode') },
          ]}
        />
      </div>
      {q.isPending ? <Loading /> : null}
      {q.isError ? <Failed e={q.error} t={t} /> : null}
      {q.data ? (
        <>
          {q.data.note ? (
            <div className="px-4 pb-2.5">
              {/* 空清单 ≠ 没有可用版本。后端给的原因原样显示 */}
              <Banner tone="warn">{q.data.note}</Banner>
            </div>
          ) : null}
          <DrawerSection
            title={t('upgrades:detail.versionsIn', {
              project: q.data.project_id || '—',
              location: q.data.location || '—',
              n: q.data.total,
            })}
          >
            <div className="flex flex-wrap gap-1.5">
              {q.data.versions.map((v) => (
                <span key={v} className="rounded border border-border px-1.5 py-0.5 font-mono text-xs">
                  {v}
                </span>
              ))}
            </div>
          </DrawerSection>
        </>
      ) : null}
    </>
  )
}
