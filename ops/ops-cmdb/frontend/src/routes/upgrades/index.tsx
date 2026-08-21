import { clusterLabel } from '../../lib/clusterLabel.js'
import { shouldRetry, toErrorInfo } from '@ops/api'
import { tError, useTranslation } from '@ops/i18n'
import {
  AsyncBoundary,
  Badge,
  Banner,
  type LoadError,
  NotIngested,
  Skeleton,
  fromQuery,
} from '@ops/ui'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { WriteButton } from '../../components/WriteButton.js'
import { apiAction, apiGet } from '../../lib/fetchJson.js'
import { useState } from 'react'
import { ClusterDetailDialog } from './ClusterDetailDialog.js'
import { NodeHealthPanel } from './NodeHealthPanel.js'
import { ScheduleDialog } from './ScheduleDialog.js'

const PERM = 'cmdb:manage_upgrade'

/**
 * 存一份升级前基线。
 *
 * # 为什么必须显式存
 *
 * 预案里的基线是**每次生成时现算的**。升完再生成一次拿到的是升级后的状态，
 * 于是没有任何东西可以对比 —— "升级前有几个 Pod 起不来"这个问题事后就答不了了。
 * 2026-07-31 UAT 那次是靠人工把 JSON 落盘到 docs 才保住的。
 *
 * ⚠️ 后端会拒绝为"没采过 Pod"的集群存基线：那种基线比对时两边都是 0，
 * 存下来只会给人一个"我有基线"的错觉。
 */
function useSaveBaseline(clusterID: number) {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: () => apiAction(`/api/gke/upgrade/baseline?cluster_id=${clusterID}`, 'POST'),
    onSuccess: () => void qc.invalidateQueries({ queryKey: ['gke-baselines', clusterID] }),
  })
}

function useBaselines(clusterID: number) {
  return useQuery({
    queryKey: ['gke-baselines', clusterID],
    queryFn: () =>
      apiGet<{ items?: { id: number; taken_at: string; target_version: string }[] }>(
        `/api/gke/upgrade/baselines?cluster_id=${clusterID}`,
      ),
    enabled: clusterID > 0,
    staleTime: 30_000,
    retry: shouldRetry,
  })
}

/**
 * 一个集群的升级态势。
 *
 * ⚠️ 这里几乎每个字段都可能是空的（没配 SA key 就什么都采不到）。
 * 空字符串一律当作**未采集**处理，不当作"没有排期"——
 * "没有排期"意味着不会被动，而真相可能是它明天就被 Google 升了。
 */
interface UpgradeRow {
  cluster_id: number
  display_name?: string
  name?: string
  environment?: string
  location?: string
  /** 采集成功与否。false 时下面的字段全部不可信 */
  synced?: boolean
  last_error?: string
  release_channel?: string
  current_master_version?: string
  inferred_target_version?: string
  predicted_upgrade_at?: string
  /** exact / month / quarter / unknown —— 精度必须显示，否则月粒度会被当成确切日期 */
  predicted_precision?: string
  predicted_window_text?: string
  days_left?: number | null
  blocked?: boolean
  paused_reason?: string
  /**
   * 暂停原因的**人话解释**。后端已经把枚举翻译好了（classifyPause），
   * 界面原来只显示原始枚举串：
   *   MAINTENANCE_WINDOW,CLUSTER_DISRUPTION_BUDGET_MINOR_UPGRADE
   * 而 MCP 的 gke_upgrade_status 对同一字段给的是
   *   「当前不在维护窗口内（常态，到窗口就会自动升级，不等于被挡住）」
   * ——同一个后端结论，AI 拿到人话，人拿到枚举（OPSCMDB-031 P1-20）。
   */
  pause_note?: string
  /** excluded / throttled / ""。throttled 是常态，不该和真被挡住同色 */
  pause_kind?: string
  effective_eos_at?: string
  effective_eos_days?: number | null
  skew_critical?: boolean
  skew_note?: string
  /**
   * 节点池。⚠️ 控制面版本 ≠ 节点版本 —— 这是这一页最容易误导人的地方。
   * 控制面先升、节点池后升，中间可能隔很久（甚至因为 auto_upgrade=false 一直不升）。
   * 只显示控制面版本会让人以为"整个集群都是这一版"，
   * 而真正决定业务能跑什么的是 kubelet 版本。
   */
  pools?: {
    name?: string
    node_count?: number
    version?: string
    auto_upgrade?: boolean
    auto_repair?: boolean
    upgrade_risk?: string
    risk_note?: string
  }[]
}

function useUpgradeOverview() {
  return useQuery({
    queryKey: ['gke-upgrade-overview'],
    queryFn: () => apiGet<{ clusters: UpgradeRow[] }>('/api/gke/upgrade/overview'),
    staleTime: 60_000,
    retry: shouldRetry,
  })
}

/**
 * 版本与升级。
 *
 * 回答一个问题：**这个集群什么时候会被自动升级，升了会不会出事**。
 *
 * ⚠️ 预测日期必须带精度。GKE 官方给的排期很多是月（2026-09）或季度粒度，
 * 显示成"2026-09-01"会被人当成确切日期去排停机窗口 ——
 * 而实际可能落在整个九月的任何一天。
 */
export function UpgradesPage() {
  const { t } = useTranslation()
  const [scheduleOpen, setScheduleOpen] = useState(false)
  /** 打开哪个集群的下钻抽屉。null=没开——不开就不取那五档的数，尤其别让实时进度空转 */
  const [detailFor, setDetailFor] = useState<UpgradeRow | null>(null)
  const query = useUpgradeOverview()

  const toLoadError = (e: unknown): LoadError => {
    const n = toErrorInfo(e)
    return { cause: tError(t, n.messageKey, n.params), detail: n.detail, retryable: n.retryable, kind: n.kind === 'license' ? 'license' : undefined, feature: n.params?.feature as string | undefined }
  }

  return (
    <div className="mx-auto max-w-[1080px] p-5">
      <div className="mb-4 flex items-baseline gap-2.5">
        <h1 className="text-sm font-semibold text-foreground">{t('upgrades:title')}</h1>
        <p className="min-w-0 flex-1 text-xs text-muted-foreground">{t('upgrades:hint')}</p>
        {/* 排期表入口：只在官网解析出错时才需要动它，所以放在次要位置 */}
        <WriteButton perm={PERM} size="sm" onClick={() => setScheduleOpen(true)}>
          {t('upgrades:schedule.entry')}
        </WriteButton>
      </div>

      <AsyncBoundary
        state={fromQuery(query, (d) => d.clusters.length === 0, toLoadError)}
        errorTitle={t('upgrades:error.title')}
        retryLabel={t('common:action.retry')}
        onRetry={() => void query.refetch()}
        pending={<Skeleton className="h-5 w-[60%]" />}
        empty={<NotIngested title={t('upgrades:empty.title')} reason={t('upgrades:empty.reason')} />}
      >
        {(d) => (
          <div className="flex flex-col gap-2">
            {/* 快到期 / 被挡住的排前面 */}
            {[...d.clusters].sort(byUrgency).map((c) => (
              <ClusterUpgradeCard key={c.cluster_id} row={c} t={t} onDetail={() => setDetailFor(c)} />
            ))}
          </div>
        )}
      </AsyncBoundary>

      {/* 节点健康与自动修复：与"会不会被自动升级"是同一类问题（都会悄悄换掉节点），
          所以放在这一页而不是节点页 */}
      <NodeHealthPanel t={t} />

      {scheduleOpen ? <ScheduleDialog onClose={() => setScheduleOpen(false)} /> : null}
      {detailFor ? (
        <ClusterDetailDialog
          clusterID={detailFor.cluster_id}
          clusterName={
            clusterLabel(detailFor.display_name, detailFor.name) || String(detailFor.cluster_id)
          }
          currentVersion={detailFor.current_master_version}
          onClose={() => setDetailFor(null)}
          t={t}
        />
      ) : null}
    </div>
  )
}

function byUrgency(a: UpgradeRow, b: UpgradeRow) {
  // 没采到的排最后：它们没有可比的数字，混在前面会挤掉真正紧急的
  const av = a.synced ? (a.effective_eos_days ?? a.days_left ?? 9999) : 99999
  const bv = b.synced ? (b.effective_eos_days ?? b.days_left ?? 9999) : 99999
  return av - bv
}

function ClusterUpgradeCard({
  row,
  t,
  onDetail,
}: {
  row: UpgradeRow
  t: (k: string, p?: Record<string, unknown>) => string
  onDetail: () => void
}) {
  return (
    <div className="rounded-[var(--radius-lg)] border border-border p-4">
      <div className="flex items-center gap-2">
        <span className="text-[13px] font-medium text-foreground">
          {clusterLabel(row.display_name, row.name)}
        </span>
        <Badge tone="mute">{row.environment ?? '—'}</Badge>
        <span className="text-xs text-muted-foreground">{row.location}</span>
        {row.release_channel ? <Badge tone="info">{row.release_channel}</Badge> : null}
        {row.blocked ? <Badge tone="warn">{t('upgrades:blocked')}</Badge> : null}
        {/* 下钻入口：升级历史 / 修复历史 / 实时进度 / 预案 / 可用版本。
            即使这个集群"没采到"也要能点——历史是另一套数据，采集断了不影响看过去发生过什么 */}
        <button
          type="button"
          onClick={onDetail}
          className="ml-auto rounded border border-border px-2 py-0.5 text-xs text-foreground hover:bg-card"
        >
          {t('upgrades:detail.entry')}
        </button>
      </div>

      {/* 采集失败时**只显示这一条**，不显示下面那些必然为空的字段。
          把空字段摆出来会让人以为"这个集群没有升级排期" */}
      {!row.synced ? (
        <div className="mt-2">
          <Banner tone="warn">
            <span className="font-medium">{t('upgrades:notSynced')}</span>
            <span className="mt-0.5 block">
              {row.last_error || t('upgrades:notSyncedUnknown')}
            </span>
          </Banner>
        </div>
      ) : (
        <div className="mt-2 grid grid-cols-[auto_1fr] gap-x-4 gap-y-1 text-xs">
          <span className="text-muted-foreground">{t('upgrades:field.current')}</span>
          <span className="font-mono text-foreground">
            {row.current_master_version || '—'}
            <span className="ml-1.5 font-sans text-[11px] text-muted-foreground">
              {t('upgrades:field.controlPlaneOnly')}
            </span>
          </span>

          <span className="text-muted-foreground">{t('upgrades:field.nodeVersions')}</span>
          <span className="text-foreground">
            <NodePoolVersions pools={row.pools} master={row.current_master_version} t={t} />
          </span>

          <span className="text-muted-foreground">{t('upgrades:field.target')}</span>
          <span className="font-mono text-foreground">{row.inferred_target_version || '—'}</span>

          <span className="text-muted-foreground">{t('upgrades:field.predicted')}</span>
          <span className="text-foreground">
            {row.predicted_window_text || row.predicted_upgrade_at || '—'}
            {/* 精度是这一行的关键信息，不是脚注 */}
            {row.predicted_precision && row.predicted_precision !== 'exact' ? (
              <span className="ml-1.5 text-muted-foreground">
                （{t(`upgrades:precision.${row.predicted_precision}`, {
                  defaultValue: row.predicted_precision,
                })}）
              </span>
            ) : null}
          </span>

          <span className="text-muted-foreground">{t('upgrades:field.eos')}</span>
          <span className="text-foreground">
            {row.effective_eos_at || '—'}
            {row.effective_eos_days != null ? (
              <span className="ml-1.5 text-muted-foreground">
                {t('upgrades:daysLeft', { count: row.effective_eos_days })}
              </span>
            ) : null}
          </span>
        </div>
      )}

      {/* 基线：只对采集成功的集群给入口 —— 没采过 Pod 的基线后端会拒，
          与其让人点了看报错，不如按钮就置灰并说明 */}
      <div className="mt-2.5 flex items-center gap-2">
        <BaselineBar row={row} t={t} />
      </div>

      {row.pause_note || row.paused_reason ? (
        // ⚠️ 优先显示人话，原始枚举放 title（运维要拿它去 GCP 控制台核对）。
        //	throttled = 常态（等维护窗口），不该和真被挡住用同一个颜色 ——
        //	把"到点就会自动升级"标成告警色，等于制造一个不存在的问题
        <p
          className={`mt-1.5 text-[11px] ${
            row.pause_kind === 'throttled' ? 'text-muted-foreground' : 'text-warning'
          }`}
          title={row.paused_reason}
        >
          {row.pause_note || row.paused_reason}
        </p>
      ) : null}
      {row.skew_critical && row.skew_note ? (
        <p className="mt-1.5 text-[11px] text-danger">{row.skew_note}</p>
      ) : null}
    </div>
  )
}


function BaselineBar({
  row,
  t,
}: {
  row: UpgradeRow
  t: (k: string, p?: Record<string, unknown>) => string
}) {
  const save = useSaveBaseline(row.cluster_id)
  const list = useBaselines(row.cluster_id)
  const items = list.data?.items ?? []
  return (
    <>
      <span className="text-[11px] text-muted-foreground">
        {items.length > 0
          ? t('upgrades:baseline.last', { at: items[0]?.taken_at })
          : t('upgrades:baseline.none')}
      </span>
      <WriteButton
        perm={PERM}
        size="sm"
        loading={save.isPending}
        blockedReason={row.synced ? undefined : t('upgrades:baseline.needSync')}
        onClick={() => save.mutate()}
      >
        {t('upgrades:baseline.save')}
      </WriteButton>
      {save.isError ? (
        <span className="text-[11px] text-danger" title={toErrorInfo(save.error).detail || undefined}>
          {tError(t, toErrorInfo(save.error).messageKey, toErrorInfo(save.error).params)}
        </span>
      ) : null}
      {save.isSuccess ? (
        <span className="text-[11px] text-success">{t('upgrades:baseline.saved')}</span>
      ) : null}
    </>
  )
}

/**
 * 节点池版本。
 *
 * 🔴 控制面版本和节点版本是两回事，必须分开显示。
 * GKE 先升控制面、后升节点池，两者可以差一个次版本长期共存；
 * 节点池 auto_upgrade=false 时甚至会一直停在旧版。
 * 页面上只有"当前版本"一行时，所有人都会默认那就是节点的版本。
 *
 * ⚠️ 没有节点池数据时显示"未采到"，不能显示成空 ——
 * 空会被读成"这个集群没有节点池"，而那不可能。
 */
function NodePoolVersions({
  pools,
  master,
  t,
}: {
  pools?: UpgradeRow['pools']
  master?: string
  t: (k: string, o?: Record<string, unknown>) => string
}) {
  if (!pools || pools.length === 0) {
    return <span className="text-xs text-muted-foreground">{t('upgrades:field.poolsUnknown')}</span>
  }
  return (
    <span className="flex flex-col gap-0.5">
      {pools.map((p) => {
        // 与控制面版本不一致要标出来：这正是"节点还没升"的信号，
        // 也是排查"为什么某个 API 在节点上不可用"的第一个要看的地方
        const drift = !!master && !!p.version && p.version !== master
        return (
          <span key={p.name} className="flex flex-wrap items-baseline gap-x-1.5 text-xs">
            <span className="text-muted-foreground">{p.name}</span>
            <span className="font-mono text-foreground">{p.version || '—'}</span>
            <span className="text-muted-foreground">
              {t('upgrades:field.poolNodes', { count: p.node_count ?? 0 })}
            </span>
            {drift ? (
              <span className="text-warning">{t('upgrades:field.versionDrift')}</span>
            ) : null}
            {/* auto_upgrade 关掉意味着它不会跟着排期走 —— 排期那一行对它不成立 */}
            {p.auto_upgrade === false ? (
              <span className="text-warning">{t('upgrades:field.autoUpgradeOff')}</span>
            ) : null}
          </span>
        )
      })}
    </span>
  )
}
