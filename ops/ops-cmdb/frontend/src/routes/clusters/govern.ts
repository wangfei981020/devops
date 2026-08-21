import { shouldRetry } from '@ops/api'
import { useQuery } from '@tanstack/react-query'
import { apiGet } from '../../lib/fetchJson.js'

/**
 * 集群治理下钻：孤儿资源 / 节点容量 / 命名空间总览 / 工作负载变更 / 安全审计 / 配置审计。
 *
 * 这六个老 CMDB 都有，新版一直没接（OPSCMDB-021）。放在集群页下钻而不是各建一页：
 * 它们回答的都是**"这个集群现在怎么样"**，而人是在集群列表里挑一个开始看的。
 *
 * ⚠️ 六个都按 cluster_id 取数，且都是实时算的（不是查快照）。所以只有当前那一档在请求。
 */

const q = (cid: number, extra = '') => `?cluster_id=${cid}${extra}`

export interface Orphan {
  kind?: string
  namespace?: string
  name?: string
  reason?: string
  detail?: string
  action?: string
}

export function useOrphans(cid: number, enabled: boolean) {
  return useQuery({
    queryKey: ['k8s-orphans', cid],
    queryFn: () => apiGet<{ items?: Orphan[]; ok?: boolean; error?: string }>(`/api/k8s/orphans${q(cid)}`),
    enabled: enabled && cid > 0,
    staleTime: 60_000,
    retry: shouldRetry,
  })
}

export interface NodeCapacity {
  node?: string
  alloc_cpu_m?: number
  alloc_mem_mi?: number
  req_cpu_m?: number
  req_mem_mi?: number
  lim_cpu_m?: number
  lim_mem_mi?: number
  cpu_req_pct?: number
  mem_req_pct?: number
  cpu_lim_pct?: number
  mem_lim_pct?: number
}

export function useNodeCapacity(cid: number, enabled: boolean) {
  return useQuery({
    queryKey: ['k8s-node-capacity', cid],
    queryFn: () => apiGet<NodeCapacity[]>(`/api/k8s/node-capacity${q(cid)}`),
    enabled: enabled && cid > 0,
    staleTime: 60_000,
    retry: shouldRetry,
  })
}

export interface NsFailure {
  namespace?: string
  workload?: string
  pod?: string
  phase?: string
  reason?: string
  restarts?: number
}

export function useNsOverview(cid: number, enabled: boolean) {
  return useQuery({
    queryKey: ['k8s-ns-overview', cid],
    queryFn: () =>
      apiGet<{ failed?: number; failures?: NsFailure[]; summary?: unknown; ok?: boolean }>(
        `/api/k8s/ns-overview${q(cid)}`,
      ),
    enabled: enabled && cid > 0,
    staleTime: 60_000,
    retry: shouldRetry,
  })
}

export interface WorkloadChange {
  id?: number
  changed_at?: string
  kind?: string
  namespace?: string
  name?: string
  field?: string
  old_value?: string
  new_value?: string
}

export function useWorkloadChanges(cid: number, enabled: boolean) {
  return useQuery({
    queryKey: ['k8s-changes', cid],
    queryFn: () => apiGet<WorkloadChange[]>(`/api/k8s/changes${q(cid)}`),
    enabled: enabled && cid > 0,
    staleTime: 30_000,
    retry: shouldRetry,
  })
}

export interface SecurityFinding {
  severity?: string
  namespace?: string
  workload?: string
  pod?: string
  risks?: string[]
  platform_component?: boolean
  /**
   * 🔴 这条问题影响多少个 Pod。同一 workload 的副本在后端被归并成一条。
   *
   *	后端注释写得很明白：「必须显式给出：归并之后"16 条"变"1 条"，
   *	不说清影响面会显得问题变小了」—— 而前端类型里一度连这个字段都没有，
   *	16 个特权容器在界面上就是一行。
   */
  pod_count?: number
  /** 归并掉的 Pod 名（后端最多留几个）。排查时要能落到具体实例 */
  sample_pods?: string[]
  note?: string
}

export interface SecurityAuditResult {
  count?: number
  findings?: SecurityFinding[]
  /**
   * 🔴 隐藏了多少个平台组件（CNI/CSI/监控等，特权是设计使然，默认不列）。
   *
   *	**必须显示出来**。不显示的话这份结果看着就是「全集群只有这几个特权容器」——
   *	而实际上还有十几个没列。后端专门返回了这个数字和 note，
   *	前端一度把它们整个丢掉（实测注入 platform_hidden=12，界面上一个字都没有）。
   *	同一个文件里 ConfigAudit 的 capability 讲的是同一个道理：
   *	**哪些结论下不了，比下了什么结论更重要**。
   */
  platform_hidden?: number
  /** 后端给的说明。⚠️ 里面写着"传 include_platform=1"——那是对 API 调用方说的，
   *  界面上要给的是一个**开关**，不是让人去传参数 */
  note?: string
}

export function useSecurityAudit(cid: number, enabled: boolean, includePlatform = false) {
  return useQuery({
    queryKey: ['k8s-security-audit', cid, includePlatform],
    queryFn: () =>
      apiGet<SecurityAuditResult>(
        `/api/k8s/security-audit${q(cid)}${includePlatform ? '&include_platform=1' : ''}`,
      ),
    enabled: enabled && cid > 0,
    staleTime: 60_000,
    retry: shouldRetry,
  })
}

/**
 * 配置审计。
 *
 * ⚠️ `capability` 是这一档最重要的字段：它说明**哪些结论是能下的、哪些下不了**。
 * 比如 Secret 存在性在没开 secret inventory 的集群上根本判不了 ——
 * 那时候「没有发现问题」是不成立的，必须把这句话显示出来。
 */
export interface ConfigAudit {
  capability?: Record<string, unknown>
  items?: {
    kind?: string
    namespace?: string
    workload?: string
    name?: string
    issue?: string
    detail?: string
  }[]
  count?: number
  /**
   * 无人引用的 ConfigMap（只在 include_unused=1 时返回）。清理用。
   * ⚠️ 字段名是 unused_configmaps，不是 unused —— 名字对不上会被 ?? 兜成空数组，
   *	界面表现为"打开开关什么也没多出来"，而不是报错。
   */
  unused_configmaps?: {
    severity?: string
    status?: string
    namespace?: string
    ref_name?: string
    detail?: string
  }[]
}

export function useConfigAudit(cid: number, enabled: boolean, includeUnused = false) {
  return useQuery({
    queryKey: ['k8s-config-audit', cid, includeUnused],
    queryFn: () =>
      apiGet<ConfigAudit>(`/api/k8s/config-audit${q(cid)}${includeUnused ? '&include_unused=1' : ''}`),
    enabled: enabled && cid > 0,
    staleTime: 60_000,
    retry: shouldRetry,
  })
}
