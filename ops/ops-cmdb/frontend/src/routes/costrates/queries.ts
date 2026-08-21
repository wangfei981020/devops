import { shouldRetry } from '@ops/api'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { apiAction, apiGet } from '../../lib/fetchJson.js'

/**
 * 成本单价表。
 *
 * # 这一页为什么重要
 *
 * 成本页上的每一个数字都是用这里的单价算出来的。**单价错了不会报错**，
 * 只会让整张成本报表安静地偏掉 —— 而人会拿着那张表去做缩容和预算决定。
 *
 * ⚠️ 区域 + 机型族没有匹配到单价时，那部分资源的成本是 **0**，
 * 而 0 在汇总里看起来就是"这批机器不花钱"。所以缺哪些单价必须能一眼看出来。
 */
export interface ComputeRate {
  id: number
  region: string
  machine_family: string
  vcpu_hour_usd: number
  ram_gb_hour_usd: number
  note?: string
}

export interface DiskRate {
  id: number
  region: string
  disk_type: string
  gb_month_usd: number
  note?: string
}

export function useComputeRates() {
  return useQuery({
    queryKey: ['compute-rates'],
    queryFn: () => apiGet<ComputeRate[]>('/api/cloud-compute-rates'),
    staleTime: 60_000,
    retry: shouldRetry,
  })
}

export function useDiskRates() {
  return useQuery({
    queryKey: ['disk-rates'],
    queryFn: () => apiGet<DiskRate[]>('/api/cloud-disk-rates'),
    staleTime: 60_000,
    retry: shouldRetry,
  })
}

function useDone(key: string) {
  const qc = useQueryClient()
  return () => void qc.invalidateQueries({ queryKey: [key] })
}

export function useSaveComputeRate() {
  const done = useDone('compute-rates')
  return useMutation({
    mutationFn: (b: {
      id?: number
      region: string
      machine_family: string
      vcpu_hour_usd: number
      ram_gb_hour_usd: number
      note?: string
    }) =>
      b.id
        ? apiAction(`/api/cloud-compute-rates/${b.id}`, 'PUT', b)
        : apiAction('/api/cloud-compute-rates', 'POST', b),
    onSuccess: done,
  })
}

export function useDeleteComputeRate() {
  const done = useDone('compute-rates')
  return useMutation({
    mutationFn: (id: number) => apiAction(`/api/cloud-compute-rates/${id}`, 'DELETE'),
    onSuccess: done,
  })
}

export function useSaveDiskRate() {
  const done = useDone('disk-rates')
  return useMutation({
    mutationFn: (b: {
      id?: number
      region: string
      disk_type: string
      gb_month_usd: number
      note?: string
    }) =>
      b.id
        ? apiAction(`/api/cloud-disk-rates/${b.id}`, 'PUT', b)
        : apiAction('/api/cloud-disk-rates', 'POST', b),
    onSuccess: done,
  })
}

export function useDeleteDiskRate() {
  const done = useDone('disk-rates')
  return useMutation({
    mutationFn: (id: number) => apiAction(`/api/cloud-disk-rates/${id}`, 'DELETE'),
    onSuccess: done,
  })
}

/**
 * 逐节点的月成本，以及它是**怎么算出来的**。
 *
 * ⚠️ `source` 是这一页最重要的字段：
 *   `manual`      —— 人工覆盖，单价表管不着它
 *   `费率:xxx`     —— 按哪条单价匹配出来的
 *   `不计费(本地)`  —— 自建集群，本来就不算钱
 * 没有它的话，一个"月成本 0"的节点看不出是"免费"还是"没匹配到单价"。
 */
export interface NodeCost {
  cluster_id: number
  cluster: string
  name: string
  mode: string
  monthly: number
  source: string
}

export function useNodeCosts() {
  return useQuery({
    queryKey: ['cost-nodes'],
    queryFn: () => apiGet<NodeCost[]>('/api/k8s/cost/nodes'),
    staleTime: 60_000,
    retry: shouldRetry,
  })
}

/**
 * 人工覆盖某个节点的月成本。
 *
 * ⚠️ 覆盖之后这个节点**不再跟着单价表走**——改了单价表也不会影响它。
 * 所以界面上必须能一眼看出哪些是被覆盖的，否则以后单价调整时
 * 会有一批节点悄悄保持旧值。传 0 = 取消覆盖，回到按费率算。
 */
export function useSetNodeOverride() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (v: { cluster_id: number; name: string; monthly: number }) =>
      apiAction('/api/k8s/cost/node-override', 'POST', v),
    onSuccess: () => void qc.invalidateQueries({ queryKey: ['cost-nodes'] }),
  })
}

/**
 * 立即为某个月打一份成本快照。
 *
 * 快照是**月度报表和环比的数据来源**：不打快照，月底之后就再也算不出
 * "上个月花了多少" —— 单价和资源都变了，事后回算得到的是今天的价格。
 */
export function useSnapshotNow() {
  return useMutation({
    mutationFn: (month: string) =>
      apiAction<{ ok?: boolean; count?: number }>(
        `/api/k8s/cost/snapshot${month ? `?month=${month}` : ''}`,
        'POST',
      ),
  })
}
