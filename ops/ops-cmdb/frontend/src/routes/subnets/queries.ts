import { queryKeys, shouldRetry } from '@ops/api'
import { useQuery } from '@tanstack/react-query'
import { api } from '../../lib/api.js'
import { apiGet } from '../../lib/fetchJson.js'

export interface Subnet {
  id: number
  name: string
  network: string
  /** auto 模式的 VPC 会自动在每个区域建子网 —— 这一列解释"它哪来的" */
  networkMode: string
  project: string
  region: string
  cidr: string
  gateway: string
  provider: string
  /** 云上已查不到它了。⚠️ 不能因此删掉：别的资源可能还引用着这个网段 */
  stale: boolean
  syncedAt: string | null
}

export interface SubnetListParams {
  page: number
  size: number
  q?: string
  region?: string
  project?: string
  [k: string]: string | number | undefined
}

export interface SubnetListResult {
  items: Subnet[]
  total: number
  /** 分面缺失（后端那条统计失败）与计数为 0 是两回事，前端不显示计数而不是显示 0 */
  facets: Record<string, Record<string, number> | undefined>
}

interface RawSubnet {
  id?: number
  name?: string
  network?: string
  network_mode?: string
  project?: string
  region?: string
  cidr?: string
  gateway?: string
  provider?: string
  stale?: boolean
  synced_at?: string
}

function toSubnet(r: RawSubnet): Subnet {
  return {
    id: r.id ?? 0,
    name: r.name ?? '',
    network: r.network ?? '',
    networkMode: r.network_mode ?? '',
    project: r.project ?? '',
    region: r.region ?? '',
    cidr: r.cidr ?? '',
    gateway: r.gateway ?? '',
    provider: r.provider ?? '',
    stale: r.stale === true,
    syncedAt: r.synced_at ? r.synced_at : null,
  }
}

export function useSubnets(params: SubnetListParams) {
  return useQuery({
    queryKey: queryKeys.subnets.list(params),
    queryFn: async (): Promise<SubnetListResult> => {
      const { data, error } = await api.GET('/cloud-subnet-list', { params: { query: params } })
      if (error) throw error
      return {
        items: ((data?.items ?? []) as RawSubnet[]).map(toSubnet),
        total: data?.total ?? 0,
        facets: (data?.facets ?? {}) as Record<string, Record<string, number> | undefined>,
      }
    },
    staleTime: 30_000,
    retry: shouldRetry,
  })
}

/**
 * VPC 网络。
 *
 * ⚠️ 这一页原来只有**子网**，没有 VPC 那一层（OPSCMDB-023 第二档）。
 * 后果是「这个项目有几个 VPC、哪个 VPC 下挂了多少子网和防火墙规则」
 * 在界面上答不出来 —— 而排查网络连通性时，第一步就是确认两边在不在同一个 VPC。
 *
 * 后端顺带算好了 subnet_count / firewall_count，比让人自己数省事。
 */
export interface CloudNetwork {
  provider: string
  project: string
  project_id: string
  name: string
  /** auto / custom —— GCP 的 VPC 模式，原样透传 */
  mode: string
  subnet_count: number
  firewall_count: number
}

export function useCloudNetworks() {
  return useQuery({
    queryKey: ['cloud-networks'],
    queryFn: () => apiGet<CloudNetwork[]>('/api/cloud-networks'),
    staleTime: 60_000,
    retry: shouldRetry,
  })
}
