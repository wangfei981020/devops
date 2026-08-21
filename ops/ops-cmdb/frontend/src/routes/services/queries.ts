import { queryKeys, shouldRetry } from '@ops/api'
import { useQuery } from '@tanstack/react-query'
import { api } from '../../lib/api.js'

export interface Service {
  clusterId: number
  clusterName: string
  /** 集群别名；没设时等于 clusterName。渲染一律走 clusterLabel（OPSCMDB-078） */
  clusterDisplay: string
  namespace: string
  name: string
  /** ClusterIP / NodePort / LoadBalancer / ExternalName，原样透传 */
  type: string
  clusterIp: string
  externalIp: string
  ports: string
  /** 指向这个 Service 的 Ingress 主机名 */
  hosts: string[]
  /** ⚠️ 判据是"有外部 IP 或有 Ingress 主机名"，不是 type==LoadBalancer —— 内网 LB 也是那个类型 */
  exposed: boolean
  /** 拿到的是私网 VIP：内网 LB，不算对外暴露（k8s 把它也写进 external_ip） */
  internalLb: boolean
  /** LoadBalancer 一直没拿到外部 IP：**卡住了**，而 Service 看起来一切正常 */
  pendingLb: boolean
  syncedAt: string | null
}

/**
 * exposure 是**封闭枚举**（由我们计算出来，不是外部自由值）。
 *
 * 用字面量联合而不是 string：传一个不在其中的值，编译期就报错。
 * 写成 string 的话，后端会静默忽略未知值 —— 筛选看起来生效了，其实没筛。
 */
export type SvcExposure = 'all' | 'exposed' | 'pending' | 'internal'

export interface ServiceListParams {
  page: number
  size: number
  q?: string
  cluster?: string
  namespace?: string
  exposure?: SvcExposure
  [k: string]: string | number | undefined
}

export interface ServiceListResult {
  items: Service[]
  total: number
  /** 分面缺失（后端那条统计失败）与计数为 0 是两回事，前端不显示计数而不是显示 0 */
  facets: Record<string, Record<string, number> | undefined>
}

interface RawService {
  cluster_id?: number
  cluster_name?: string
  cluster_display_name?: string
  namespace?: string
  name?: string
  type?: string
  cluster_ip?: string
  external_ip?: string
  ports?: string
  hosts?: string[]
  exposed?: boolean
  internal_lb?: boolean
  pending_lb?: boolean
  synced_at?: string
}

function toService(r: RawService): Service {
  return {
    clusterId: r.cluster_id ?? 0,
    clusterName: r.cluster_name ?? '',
    clusterDisplay: r.cluster_display_name ?? '',
    namespace: r.namespace ?? '',
    name: r.name ?? '',
    type: r.type ?? '',
    clusterIp: r.cluster_ip ?? '',
    externalIp: r.external_ip ?? '',
    ports: r.ports ?? '',
    hosts: r.hosts ?? [],
    exposed: r.exposed === true,
    internalLb: r.internal_lb === true,
    pendingLb: r.pending_lb === true,
    syncedAt: r.synced_at ? r.synced_at : null,
  }
}

export function useServices(params: ServiceListParams) {
  return useQuery({
    queryKey: queryKeys.services.list(params),
    queryFn: async (): Promise<ServiceListResult> => {
      const { data, error } = await api.GET('/k8s/service-list', { params: { query: params } })
      if (error) throw error
      return {
        items: ((data?.items ?? []) as RawService[]).map(toService),
        total: data?.total ?? 0,
        facets: (data?.facets ?? {}) as Record<string, Record<string, number> | undefined>,
      }
    },
    staleTime: 30_000,
    retry: shouldRetry,
  })
}
