import { shouldRetry } from '@ops/api'
import { useQuery } from '@tanstack/react-query'
import { apiGet } from '../../lib/fetchJson.js'

/**
 * 「版本与升级」的下钻数据：升级历史 / 节点修复历史 / 实时进度 / 升级计划 / 可用版本。
 *
 * 这些接口后端一直都有（表、路由、采集全齐），只是重写前端时没接回来。
 * 见 OPSCMDB-019。
 *
 * ⚠️ 贯穿这一组接口的一条纪律：**空结果不等于没发生过**。
 * 后端在每个历史类接口上都返回了 `coverage`，说明这份历史能覆盖到什么时候
 * （GCP 的 operations 保留期只有两周量级，早于首次采集的历史找不回来）。
 * 那段话必须原样显示——不显示的话，"最近没有升级过"这个结论会被当成事实，
 * 而真相可能只是"我们从上周才开始采"。
 */

/** 历史类接口共用的覆盖范围说明。 */
export interface Coverage {
  total: number
  earliest?: string
  latest?: string
  /** 后端写好的解释。⚠️ 原样显示，不要自己另写一句 */
  note?: string
  truncated?: boolean
  truncated_note?: string
  /** 修复历史专有：有多少条解析不出原因 */
  reason_unparsed?: number
  reason_note?: string
}

export interface UpgradeHistoryRow {
  cluster_id: number
  cluster: string
  scope: string
  pool: string
  /** AUTOMATIC=被 Google 自动升的 / MANUAL=我们手动升的 / 空=来源没给，如实标未知 */
  start_type: string
  state: string
  initial_version: string
  target_version: string
  started_at: string
  ended_at: string
  duration: string
  detail: string
  source: string
}

export function useUpgradeHistory(clusterID: number) {
  return useQuery({
    queryKey: ['gke-upgrade-history', clusterID],
    queryFn: () =>
      apiGet<{
        ok?: boolean
        error?: string
        rows: UpgradeHistoryRow[]
        stat: Record<string, number>
        coverage: Coverage
      }>(`/api/gke/upgrade/history?cluster_id=${clusterID}`),
    enabled: clusterID > 0,
    staleTime: 60_000,
    retry: shouldRetry,
  })
}

export interface RepairHistoryRow {
  cluster_id: number
  cluster: string
  op_name: string
  pool: string
  node_name: string
  /** 可能为空：REST v1 的 Operation 没有这个字段，只能从文本里猜 */
  repair_reason: string
  status: string
  started_at: string
  ended_at: string
  duration: string
  detail: string
  status_message: string
}

export function useRepairHistory(clusterID: number) {
  return useQuery({
    queryKey: ['gke-repair-history', clusterID],
    queryFn: () =>
      apiGet<{ ok?: boolean; error?: string; rows: RepairHistoryRow[]; coverage: Coverage }>(
        `/api/gke/repair-history?cluster_id=${clusterID}`,
      ),
    enabled: clusterID > 0,
    staleTime: 60_000,
    retry: shouldRetry,
  })
}

export interface NodeEvent {
  scope: string
  node: string
  pool: string
  event: string
  from: string
  to: string
  at: string
}

export interface PoolPace {
  pool: string
  at: string
  nodes: number
  batches: number
  total_minutes: number
  batch_size: number
  median_batch_minutes: number
  slowest_batch_minutes: number
  note: string
}

export interface ProgressResult {
  cluster_id: number
  cluster: string
  since: string
  events: NodeEvent[]
  pools: PoolPace[]
  extrapolate: string
  /** 采集此刻是否正常。false 时下面的"没有事件"不成立 */
  collection_healthy: boolean
  collection_note: string
  precision: string
  warnings: string[]
}

/**
 * 升级过程看板。
 *
 * ⚠️ 只在抽屉打开且用户切到这一档时才轮询（`enabled`），并且**只轮询这一个集群**。
 * 升级窗口期需要看到节点一批批换过去，但平时没人升级，
 * 让它常驻轮询等于给后端白加压力。
 */
export function useUpgradeProgress(clusterID: number, hours: number, live: boolean) {
  return useQuery({
    queryKey: ['gke-upgrade-progress', clusterID, hours],
    queryFn: () =>
      apiGet<ProgressResult>(`/api/gke/upgrade/progress?cluster_id=${clusterID}&hours=${hours}`),
    enabled: clusterID > 0 && live,
    // 升级期间节点是一批批变的，15 秒够用；关掉抽屉就停
    refetchInterval: live ? 15_000 : false,
    staleTime: 10_000,
    retry: shouldRetry,
  })
}

export interface AvailableVersions {
  cluster_id: number
  project_id: string
  location: string
  kind: string
  versions: string[]
  total: number
  /** 空清单的原因。⚠️ 空 ≠ 没有可升的版本，多半是还没采过 */
  note?: string
}

export function useAvailableVersions(clusterID: number, kind: 'master' | 'node') {
  return useQuery({
    queryKey: ['gke-available-versions', clusterID, kind],
    queryFn: () =>
      apiGet<AvailableVersions>(
        `/api/gke/available-versions?cluster_id=${clusterID}&kind=${kind}`,
      ),
    enabled: clusterID > 0,
    staleTime: 5 * 60_000,
    retry: shouldRetry,
  })
}

/** 单个池/控制面的耗时估算。`basis` 说明这个数字怎么来的。 */
export interface Estimate {
  min_minutes: number
  max_minutes: number
  /** 实测 / API 参数 / 经验区间 —— ⚠️ 必须显示 */
  basis: string
  batches: number
  /** 缺了哪些参数导致只能给区间 */
  incomplete?: string[]
  /** true = 用了本集群实测值 */
  measured: boolean
}

export interface PoolPlan {
  name: string
  node_count: number
  current_version: string
  strategy: string
  batch_node_count?: number | null
  batch_percentage?: number | null
  rollout_policy?: string
  estimate: Estimate
  /** 升级期间临时多占的节点数 —— 配额不够会直接卡住升级 */
  extra_nodes_needed: number
  quota_note?: string
}

/**
 * 升级预案。
 *
 * ⚠️ 两个字段决定这份预案能不能信：
 * - `estimate.basis` / `measured`：是实测节奏还是经验区间。
 *   拿经验区间当实测去排停机窗口是会出事的。
 * - `pdbs_collected`：false 时「无阻塞 PDB」这个结论**不成立**，
 *   只是没采到而已。后端专门给了这个字段，别把它当成"没有阻塞"。
 */
export interface PlanResult {
  cluster_id?: number
  cluster?: string
  environment?: string
  project_id?: string
  location?: string
  target_version?: string
  generated_at?: string
  control_plane?: { current_version?: string; target_version?: string; estimate: Estimate }
  pools?: PoolPlan[]
  blocking_pdbs?: { namespace?: string; name?: string; note?: string }[]
  /** false → 「无阻塞 PDB」不成立 */
  pdbs_collected?: boolean
  pdb_note?: string
  total_estimate?: Estimate
  console_steps?: string[]
  verification?: string[]
  warnings?: string[]
  error?: string
}

export function useUpgradePlan(clusterID: number, target: string) {
  return useQuery({
    queryKey: ['gke-upgrade-plan', clusterID, target],
    queryFn: () =>
      apiGet<PlanResult>(
        `/api/gke/upgrade/plan?cluster_id=${clusterID}${target ? `&target_version=${encodeURIComponent(target)}` : ''}`,
      ),
    enabled: clusterID > 0,
    staleTime: 60_000,
    retry: shouldRetry,
  })
}

export interface NodeHealthRow {
  cluster_id: number
  cluster: string
  provider: string
  node_name: string
  alert_level: string
  alert_kind: string
  disk_pct: number
  last_alert_at: string
  not_ready_since: string
  not_ready_seconds?: number
  not_ready_text?: string
  /** 距 GKE 自动修复还有多久。GKE 集群才有 */
  repair_in_text?: string
}

/**
 * 节点健康与自动修复。
 *
 * ⚠️ `task.enabled=false` 时，"没有异常"**不成立**——那只代表没在监控。
 * 后端专门返回了 task 状态和 note 来表达这件事，界面必须把它顶在最前面。
 */
export function useNodeHealth() {
  return useQuery({
    queryKey: ['gke-node-health'],
    queryFn: () =>
      apiGet<{
        ok?: boolean
        error?: string
        rows: NodeHealthRow[]
        task: {
          found: boolean
          enabled: boolean
          schedule: string
          last_run_at: string
          last_result: string
          note: string
        }
        thresholds: Record<string, string>
      }>('/api/gke/node-health'),
    staleTime: 30_000,
    retry: shouldRetry,
  })
}
