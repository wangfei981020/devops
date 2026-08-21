export interface K8sEvent {
  cluster_id: number
  cluster_name: string
  /** 集群别名；没设时等于 cluster_name。渲染走 clusterLabel（OPSCMDB-078） */
  cluster_display_name?: string
  namespace: string
  kind: string
  obj_name: string
  /** k8s 原值：FailedScheduling / BackOff / Unhealthy…… 运维直接拿去搜的词 */
  reason: string
  message: string
  /** k8s 自己聚合的重复次数。1 次和 300 次是完全不同的严重度 */
  count: number
  first_at?: string
  last_at?: string
}

import { shouldRetry } from '@ops/api'
import { useQuery } from '@tanstack/react-query'
import { apiGet } from '../../lib/fetchJson.js'

export interface K8sEventListResult {
  items: K8sEvent[]
  total: number
  facets: Record<string, Record<string, number> | undefined>
}

export function useK8sEvents(params: Record<string, string | number>) {
  const qs = new URLSearchParams(
    Object.entries(params)
      .filter(([, v]) => v !== '' && v !== undefined)
      .map(([k, v]) => [k, String(v)]),
  ).toString()
  return useQuery({
    queryKey: ['events', qs],
    queryFn: () => apiGet<K8sEventListResult>(`/api/k8s/event-list?${qs}`),
    staleTime: 30_000,
    retry: shouldRetry,
  })
}
