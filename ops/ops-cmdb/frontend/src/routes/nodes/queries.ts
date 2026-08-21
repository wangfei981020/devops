import { queryKeys, shouldRetry } from '@ops/api'
import { useQuery } from '@tanstack/react-query'
import { api } from '../../lib/api.js'

/**
 * 节点。主机页的对偶：同一台机器，回答的是"还能不能调度、上面跑了什么"。
 */
export interface Node {
  clusterId: number
  clusterName: string
  /** 别名。没设时等于 clusterName —— 渲染一律走 clusterLabel，不要自己判空 */
  clusterDisplay: string
  name: string
  pool: string
  roles: string
  internalIp: string
  machineType: string
  cpuCap: string
  memCap: string
  osImage: string
  kubelet: string
  /** 原样透传：Ready / NotReady / Unknown。不要归并成布尔量 */
  readyStatus: string
  /**
   * ⚠️ 心跳过期时 readyStatus **不可信**：kubelet 停止上报后，
   * 库里那个值会停在最后一次的状态上。界面必须先说"失联"，
   * 再谈它自称是什么。
   */
  heartbeatStale: boolean
  /**
   * 状态为什么不可信：
   *   'heartbeat'  = 这个节点的心跳停了 → 去查节点/kubelet
   *   'collection' = CMDB 的采集停了 → 去查采集，节点本身可能好好的
   * ⚠️ 两者处置完全不同，不能都写成「节点失联」。
   * 采集挂掉时所有节点的 hb_stale 会冻结在最后一次的值上，
   * 不单独标出来的话，「采集挂了」会被渲染成「一切正常」。
   */
  staleReason: 'heartbeat' | 'collection' | ''
  lastHeartbeat: string | null
  /** 压力位摘要。空串 = 没有压力位（不是没采到） */
  conditions: string
  /** 节点自报的 Pod 数 */
  podCount: number
  /** 我们实际采到的 Pod 行数。null = 这个集群的 Pod 还没采过 */
  podsCollected: number | null
  /** 0 = 没关联上主机台账，不是"没有主机" */
  hostCiId: number
  hostName: string
  syncedAt: string | null

  /**
   * 容量与装箱。
   * ⚠️ 这些是 **request/limit 的装箱率**，不是实际用量——
   * UAT 实测 request 装箱 48% 而实际 CPU 只用了 5%，差了将近十倍。
   * 拿装箱率回答"这台忙不忙"会得出完全相反的结论。
   * 实际用量要 Prometheus（当前 UAT 未接，见 OPSCMDB-028 GAP-1）。
   */
  allocCpuM: number
  reqCpuM: number
  limCpuM: number
  cpuReqPct: number
  cpuLimPct: number
  allocMemMi: number
  reqMemMi: number
  limMemMi: number
  memReqPct: number
  memLimPct: number
}

export type NodeStatusFilter = 'all' | 'ready' | 'notready' | 'stale' | 'pressure'

export interface NodeListParams {
  page: number
  size: number
  /** 节点池筛选。'all' 或空 = 不筛；'-' = 没有节点池的节点（自建集群） */
  pool?: string
  q?: string
  cluster?: string
  status?: NodeStatusFilter
  [k: string]: string | number | undefined
}

export interface NodeListResult {
  items: Node[]
  total: number
  facets: Record<string, Record<string, number>>
}

interface RawNode {
  cluster_id?: number
  cluster_name?: string
  cluster_display_name?: string
  name?: string
  pool?: string
  roles?: string
  internal_ip?: string
  machine_type?: string
  cpu_cap?: string
  mem_cap?: string
  os_image?: string
  kubelet_version?: string
  ready_status?: string
  heartbeat_stale?: boolean
  stale_reason?: string
  last_heartbeat?: string
  conditions?: string
  pod_count?: number
  pods_collected?: number | null
  host_ci_id?: number
  host_name?: string
  synced_at?: string
  alloc_cpu_m?: number
  req_cpu_m?: number
  lim_cpu_m?: number
  cpu_req_pct?: number
  cpu_lim_pct?: number
  alloc_mem_mi?: number
  req_mem_mi?: number
  lim_mem_mi?: number
  mem_req_pct?: number
  mem_lim_pct?: number
}

function toNode(r: RawNode): Node {
  return {
    clusterId: r.cluster_id ?? 0,
    clusterName: r.cluster_name ?? '',
    clusterDisplay: r.cluster_display_name ?? '',
    name: r.name ?? '',
    pool: r.pool ?? '',
    roles: r.roles ?? '',
    internalIp: r.internal_ip ?? '',
    machineType: r.machine_type ?? '',
    cpuCap: r.cpu_cap ?? '',
    memCap: r.mem_cap ?? '',
    osImage: r.os_image ?? '',
    kubelet: r.kubelet_version ?? '',
    readyStatus: r.ready_status ?? '',
    // ⚠️ 缺字段时兜底成 true（不可信）而不是 false。
    // 反过来会让一批状态未知的节点显示成"心跳正常"
    heartbeatStale: r.heartbeat_stale !== false,
    staleReason:
      r.stale_reason === 'heartbeat' || r.stale_reason === 'collection' ? r.stale_reason : '',
    lastHeartbeat: r.last_heartbeat ? r.last_heartbeat : null,
    conditions: r.conditions ?? '',
    podCount: r.pod_count ?? 0,
    podsCollected:
      r.pods_collected === undefined || r.pods_collected === null ? null : r.pods_collected,
    hostCiId: r.host_ci_id ?? 0,
    hostName: r.host_name ?? '',
    syncedAt: r.synced_at ? r.synced_at : null,
    allocCpuM: r.alloc_cpu_m ?? 0,
    reqCpuM: r.req_cpu_m ?? 0,
    limCpuM: r.lim_cpu_m ?? 0,
    cpuReqPct: r.cpu_req_pct ?? 0,
    cpuLimPct: r.cpu_lim_pct ?? 0,
    allocMemMi: r.alloc_mem_mi ?? 0,
    reqMemMi: r.req_mem_mi ?? 0,
    limMemMi: r.lim_mem_mi ?? 0,
    memReqPct: r.mem_req_pct ?? 0,
    memLimPct: r.mem_lim_pct ?? 0,
  }
}

export function useNodes(params: NodeListParams) {
  return useQuery({
    queryKey: queryKeys.nodes.list(params),
    queryFn: async (): Promise<NodeListResult> => {
      const { data, error } = await api.GET('/k8s/node-list', { params: { query: params } })
      if (error) throw error
      return {
        items: ((data?.items ?? []) as RawNode[]).map(toNode),
        total: data?.total ?? 0,
        facets: (data?.facets ?? {}) as Record<string, Record<string, number>>,
      }
    },
    staleTime: 30_000,
    retry: shouldRetry,
  })
}
