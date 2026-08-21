import { useQuery } from '@tanstack/react-query'
import { apiGet } from './fetchJson.js'

/**
 * 实时用量：给列表页补一列「现在用了多少」。
 *
 * # 为什么是单独一次请求而不是并进列表接口
 *
 * 列表数据来自 CMDB 的采集库（几分钟一轮），而用量要打 Prometheus 实时查。
 * 合并的话，Prometheus 一挂整张列表就跟着挂 —— 而列表本身是好的。
 * 分开之后，用量那一列失败只影响那一列，其余照常可用。
 *
 * # ⚠️ 三态，不能退化成 0
 *
 * - 数据源没配 / 连不上 → `ok:false` + error，那一列显示「查不到」
 * - 连上了但这个对象没有数据 → map 里没这个 key，显示「—」
 * - 有数据 → 显示数值
 *
 * 把前两种显示成 0 是最坏的做法：0% CPU 看起来像「这机器闲着」，
 * 于是有人拿它去做缩容决策，而真相是根本没查到。
 */

export interface UsageMap {
  ok: boolean
  error?: string
  /** key 形如 "ns/pod"（Pod）、节点名（节点）、"ns/pvc"（PVC） */
  usage?: Record<string, Record<string, number>>
  empty_hint?: string
  cluster_label_error?: unknown
  /** PVC 专有：用量数据的可信度说明 */
  accuracy?: unknown
}

type Kind = 'pod' | 'node' | 'pvc'

const PATH: Record<Kind, string> = {
  pod: '/api/k8s/pod-usage',
  node: '/api/k8s/node-usage',
  pvc: '/api/k8s/pvc-usage',
}

/**
 * 拉某集群的全量实时用量。
 *
 * ⚠️ 只在给了 clusterId 时才查。列表页可以跨集群展示，
 * 但用量是**按集群**打 Prometheus 的 —— 没选定集群时不该瞎猜一个去查。
 */
export function useLiveUsage(kind: Kind, clusterId: number | null) {
  return useQuery({
    queryKey: ['live-usage', kind, clusterId],
    queryFn: () => apiGet<UsageMap>(`${PATH[kind]}?cluster_id=${clusterId}`),
    enabled: clusterId != null && clusterId > 0,
    // 实时用量，但也别打太勤 —— 一次请求拉的是全集群
    staleTime: 30_000,
    refetchOnWindowFocus: false,
    retry: false,
  })
}

/** 取一个对象的用量。返回 undefined = 没这条数据（和「值为 0」不是一回事） */
export function pickUsage(
  m: UsageMap | undefined,
  key: string,
  field: string,
): number | undefined {
  if (!m?.ok || !m.usage) return undefined
  return m.usage[key]?.[field]
}
