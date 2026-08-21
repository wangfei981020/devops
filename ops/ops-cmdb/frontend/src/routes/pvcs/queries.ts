import { queryKeys, shouldRetry } from '@ops/api'
import { useQuery } from '@tanstack/react-query'
import { api } from '../../lib/api.js'

export interface PVC {
  clusterId: number
  clusterName: string
  /** 集群别名；没设时等于 clusterName。渲染一律走 clusterLabel（OPSCMDB-078） */
  clusterDisplay: string
  namespace: string
  name: string
  /** Bound / Pending / Lost，原样透传 */
  status: string
  capacity: string
  storageClass: string
  volumeName: string
  /** ⚠️ 只表示"当前没有使用者"，**不等于可以删** —— 定时任务的卷、刚 drain 完的服务都会短暂没人用 */
  orphan: boolean
  /**
   * 每月多少钱。⚠️ 0 / undefined = **算不出来**（费率表里没有对应档），不是免费。
   * 所以界面上算不出来时不显示，而不是显示 $0。
   */
  monthlyUsd: number
  syncedAt: string | null
}

/**
 * health 是**封闭枚举**（由我们计算出来，不是外部自由值）。
 *
 * 用字面量联合而不是 string：传一个不在其中的值，编译期就报错。
 * 写成 string 的话，后端会静默忽略未知值 —— 筛选看起来生效了，其实没筛。
 */
export type PVCHealth = 'all' | 'lost' | 'pending' | 'orphan' | 'ok'

export interface PVCListParams {
  page: number
  size: number
  q?: string
  cluster?: string
  health?: PVCHealth
  [k: string]: string | number | undefined
}

export interface PVCListResult {
  items: PVC[]
  total: number
  /** 分面缺失（后端那条统计失败）与计数为 0 是两回事，前端不显示计数而不是显示 0 */
  facets: Record<string, Record<string, number> | undefined>
}

interface RawPVC {
  cluster_id?: number
  cluster_name?: string
  cluster_display_name?: string
  namespace?: string
  name?: string
  status?: string
  capacity?: string
  storage_class?: string
  volume_name?: string
  orphan?: boolean
  monthly_usd?: number
  synced_at?: string
}

function toPVC(r: RawPVC): PVC {
  return {
    clusterId: r.cluster_id ?? 0,
    clusterName: r.cluster_name ?? '',
    clusterDisplay: r.cluster_display_name ?? '',
    namespace: r.namespace ?? '',
    name: r.name ?? '',
    status: r.status ?? '',
    capacity: r.capacity ?? '',
    storageClass: r.storage_class ?? '',
    volumeName: r.volume_name ?? '',
    orphan: r.orphan === true,
    monthlyUsd: r.monthly_usd ?? 0,
    syncedAt: r.synced_at ? r.synced_at : null,
  }
}

export function usePVCs(params: PVCListParams) {
  return useQuery({
    queryKey: queryKeys.pvcs.list(params),
    queryFn: async (): Promise<PVCListResult> => {
      const { data, error } = await api.GET('/k8s/pvc-list', { params: { query: params } })
      if (error) throw error
      return {
        items: ((data?.items ?? []) as RawPVC[]).map(toPVC),
        total: data?.total ?? 0,
        facets: (data?.facets ?? {}) as Record<string, Record<string, number> | undefined>,
      }
    },
    staleTime: 30_000,
    retry: shouldRetry,
  })
}
