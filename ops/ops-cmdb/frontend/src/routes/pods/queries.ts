import { queryKeys, shouldRetry } from '@ops/api'
import { useQuery } from '@tanstack/react-query'
import { api } from '../../lib/api.js'

export interface Pod {
  clusterId: number
  clusterName: string
  /** 集群别名；没设时等于 clusterName。渲染一律走 clusterLabel（OPSCMDB-078） */
  clusterDisplay: string
  namespace: string
  name: string
  nodeName: string
  workload: string
  /** k8s 原值，不翻译：运维拿这个词直接去 kubectl 里搜 */
  phase: string
  /** CrashLoopBackOff / ImagePullBackOff 等，同样原样显示 */
  reason: string
  restarts: number
  /**
   * Pod IP。
   * 🔴 这个字段此前是**最坏的形态**：类型里有、映射里取了、界面上零处引用 ——
   *	看代码像"接好了"，看界面才发现没有（OPSCMDB-028 GAP-10）。
   *	拿 IP 去 Envoy / 日志里反查"这个请求是谁发的"，靠的就是它。
   */
  podIp: string
  /**
   * request / limit。**配置**不是用量，两回事。
   *
   * 用量页有实际用量，但"这个 Pod 申请了多少"要看 request ——
   * 判断「是不是 request 写太大导致装不下」只能靠它，实际用量回答不了。
   * ⚠️ null = 没配（BestEffort），不是 0。0 和"没配"在 k8s 语义里不同：
   *	没配 request 的 Pod 在节点内存压力时最先被驱逐。
   */
  cpuReqM: number | null
  cpuLimM: number | null
  memReqMi: number | null
  memLimMi: number | null
  startTime: string | null
  syncedAt: string | null
}

export type PodHealth = 'all' | 'bad' | 'restarted' | 'ok'

export interface PodListParams {
  page: number
  size: number
  q?: string
  cluster?: string
  namespace?: string
  health?: PodHealth
  /**
   * 工作负载 / 节点下钻。后端 pod-list 的 filter 里一直有这两个
   * （handlers/k8s_resources.go:138），前端此前只接了 cluster/namespace/health/q。
   *
   * ⚠️ 靠关键词搜工作负载名**碰巧**能命中（Pod 名 = 工作负载名 + hash），
   *	但那是命名巧合不是设计：Job / CronJob 的 Pod 名与工作负载名对不上时就搜不到，
   *	而"搜不到"会被读成"这个工作负载没有 Pod"（OPSCMDB-028 GAP-11）。
   */
  workload?: string
  node?: string
  [k: string]: string | number | undefined
}

export interface PodListResult {
  items: Pod[]
  total: number
  /** 分面缺失（后端那条统计查询失败）与计数为 0 是两回事，见下方注释 */
  facets: Record<string, Record<string, number> | undefined>
}

interface RawPod {
  cluster_id?: number
  cluster_name?: string
  cluster_display_name?: string
  namespace?: string
  name?: string
  node_name?: string
  workload?: string
  phase?: string
  reason?: string
  restarts?: number
  pod_ip?: string
  cpu_req_m?: number
  cpu_lim_m?: number
  mem_req_mi?: number
  mem_lim_mi?: number
  start_time?: string
  synced_at?: string
}

function toPod(r: RawPod): Pod {
  return {
    clusterId: r.cluster_id ?? 0,
    clusterName: r.cluster_name ?? '',
    clusterDisplay: r.cluster_display_name ?? '',
    namespace: r.namespace ?? '',
    name: r.name ?? '',
    nodeName: r.node_name ?? '',
    workload: r.workload ?? '',
    phase: r.phase ?? '',
    reason: r.reason ?? '',
    restarts: r.restarts ?? 0,
    podIp: r.pod_ip ?? '',
    // ⚠️ 0 视为「没配」而不是「配了 0」：k8s 里不存在 request=0 的有效配置，
    //	而"没配 request"（BestEffort）是一个要单独看见的状态 —— 它决定驱逐顺序。
    cpuReqM: r.cpu_req_m ? r.cpu_req_m : null,
    cpuLimM: r.cpu_lim_m ? r.cpu_lim_m : null,
    memReqMi: r.mem_req_mi ? r.mem_req_mi : null,
    memLimMi: r.mem_lim_mi ? r.mem_lim_mi : null,
    startTime: r.start_time ? r.start_time : null,
    syncedAt: r.synced_at ? r.synced_at : null,
  }
}

export function usePods(params: PodListParams) {
  return useQuery({
    queryKey: queryKeys.pods.list(params),
    queryFn: async (): Promise<PodListResult> => {
      const { data, error } = await api.GET('/k8s/pod-list', { params: { query: params } })
      if (error) throw error
      return {
        items: ((data?.items ?? []) as RawPod[]).map(toPod),
        total: data?.total ?? 0,
        facets: (data?.facets ?? {}) as Record<string, Record<string, number> | undefined>,
      }
    },
    staleTime: 30_000,
    retry: shouldRetry,
  })
}
