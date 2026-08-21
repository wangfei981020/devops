import { shouldRetry } from '@ops/api'
import { useQuery } from '@tanstack/react-query'
import { apiGet } from '../../lib/fetchJson.js'

/**
 * 中断预算（PDB）与自动伸缩（HPA）。
 *
 * # 为什么这两个放一起
 *
 * 它们回答的是同一类问题：**这次变更能不能安全做**。
 *
 * - PDB：驱逐节点时会不会被拒（`blocking` = 余量为 0，此刻驱逐任何一个 Pod 都会失败）
 * - HPA：当前副本数是不是已经贴着上限（贴着上限 = 再有流量就扛不住了）
 *
 * 升级、缩容、驱逐前该看的就是这两张表。分散在各处等于没有。
 */

export interface Pdb {
  namespace: string
  name: string
  min_available: string
  max_unavailable: string
  selector: string
  current_healthy: number
  desired_healthy: number
  expected_pods: number
  disruptions_allowed: number
  /** ⚠️ 余量为 0：此刻驱逐任何一个 Pod 都会被拒 */
  blocking: boolean
  /** 后端直接给结论（为什么卡住 / 卡住会怎样），别让前端自己推 */
  risk_note: string
}

/**
 * PDB 列表的响应。
 *
 * ⚠️ 这个接口返回的是**包装对象**，不是裸数组 —— 和同页的 hpas / node-pools 不一样。
 * 我第一版按数组写，页面当场 `o is not iterable` 崩掉。
 * 同一批接口形状不统一这件事本身就是坑，但既然后端已经这样了，
 * 前端必须按各自真实形状写，不能靠"看起来都是列表"去推。
 *
 * `collected` 是这里最有价值的字段：**没有 PDB** 和 **没采集过** 是两回事，
 * 前者意味着"驱逐时毫无保护"，后者意味着"这个结论不能信"。
 */
export interface PdbResp {
  items?: Pdb[]
  /** 余量为 0 的条数，后端算好的 */
  blocking?: number
  /** ⚠️ false = 这个集群还没采过 PDB，不是"没有 PDB" */
  collected?: boolean
  total?: number
}

export function usePdbs(clusterId: number | null) {
  return useQuery({
    queryKey: ['k8s-pdbs', clusterId],
    queryFn: () => apiGet<PdbResp>(`/api/k8s/pdbs?cluster_id=${clusterId}`),
    enabled: clusterId != null && clusterId > 0,
    staleTime: 60_000,
    retry: shouldRetry,
  })
}

export interface Hpa {
  id: number
  cluster_id: number
  namespace: string
  name: string
  target_kind: string
  target_name: string
  min_replicas: number
  max_replicas: number
  current_replicas: number
}

export function useHpas(clusterId: number | null) {
  return useQuery({
    queryKey: ['k8s-hpas', clusterId],
    queryFn: () => apiGet<Hpa[]>(`/api/k8s/hpas?cluster_id=${clusterId}`),
    enabled: clusterId != null && clusterId > 0,
    staleTime: 60_000,
    retry: shouldRetry,
  })
}

export interface NodePool {
  id: number
  cluster_id: number
  name: string
  machine_type: string
  node_count: number
  version: string
}

export function useNodePools(clusterId: number | null) {
  return useQuery({
    queryKey: ['k8s-node-pools', clusterId],
    queryFn: () => apiGet<NodePool[]>(`/api/k8s/node-pools?cluster_id=${clusterId}`),
    enabled: clusterId != null && clusterId > 0,
    staleTime: 60_000,
    retry: shouldRetry,
  })
}
