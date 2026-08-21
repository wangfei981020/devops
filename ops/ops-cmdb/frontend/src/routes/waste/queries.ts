import { shouldRetry } from '@ops/api'
import { useQuery } from '@tanstack/react-query'
import { apiGet } from '../../lib/fetchJson.js'

/**
 * 闲置与浪费。
 *
 * ⚠️ 这一页**必须有 Prometheus 实测用量**才有意义：它比的是
 * "申请了多少（request）"和"实际用了多少"。没接指标源时，
 * 我们只知道申请值 —— 而只看申请值得出的"浪费"是凭空捏造的。
 * 所以没数据时要明说"未接入指标源"，绝不给一个看起来很专业的 0。
 *
 * # 🔴 这一页的字段名错过一次，是全项目危险性最高的一次
 *
 * 类型里原来写的是 `cpu_request_m` / `mem_request_mi`，后端给的是
 * `cpu_req_m` / `mem_req_mi`。504 行的「申请」值**全部渲染成 0**。
 * 而这一页的输出会被直接拿去做缩容决定：
 *
 * - 读成「申请 679.8m，实测 0」 → 结论"这东西没人用，砍掉"
 * - 读成「申请 0，实测 679.8m」 → 结论"没设 request，赶紧加"
 *
 * **两种读法都是错的，第一种会直接导致误删在跑的服务。**
 * 真实数据是 CPU 用 5.7%（确实超配）但内存用 24%，infra 的 logstash
 * 内存更是 88% —— 照"实测 0"去砍内存会立刻 OOM。
 *
 * 所以这个文件里的每个字段名都必须和 `handlers/resource_waste.go`
 * 的 `wasteItem` 逐字一致。改动前先 curl 一次核对，别按"应该叫什么"去推。
 */
export interface WasteRow {
  namespace?: string
  workload?: string
  /** 副本数。建议值是**单副本**的，没有它就无法换算总量 */
  replicas?: number
  /** ⚠️ `cpu_req_m`，不是 cpu_request_m */
  cpu_req_m?: number
  cpu_used_m?: number
  cpu_usage_pct?: number
  /** ⚠️ `mem_req_mi`，不是 mem_request_mi */
  mem_req_mi?: number
  mem_used_mi?: number
  mem_usage_pct?: number
  /** ⚠️ `suggest_cpu_req_m`，不是 suggest_cpu_m */
  suggest_cpu_req_m?: number
  suggest_mem_req_mi?: number
  /** 没配 request 时后端给的说明（BestEffort）。有它就没有建议值 */
  note?: string
}

/** 全集群汇总。⚠️ 「一共浪费了多少」是这一页存在的**目的**，不是附加信息 */
export interface WasteSummary {
  cpu_request_cores?: number
  cpu_used_cores?: number
  cpu_usage_pct?: number
  cpu_wasted_cores?: number
  mem_request_gi?: number
  mem_used_gi?: number
  mem_usage_pct?: number
  mem_wasted_gi?: number
  /** 建议值 = 实测 × 这个系数。不显示它，建议值就成了没有依据的数字 */
  suggest_factor?: number
}

export interface WasteResult {
  items?: WasteRow[]
  list?: WasteRow[]
  summary?: WasteSummary
  /** 后端在没接指标源时会给出说明 —— 原样透传给用户 */
  note?: string
  ok?: boolean
  error?: string
}

export interface WasteData {
  items: WasteRow[]
  summary: WasteSummary | null
  note: string
  error: string
}

export function useWaste(clusterId: number) {
  return useQuery<WasteData>({
    queryKey: ['waste', clusterId],
    enabled: clusterId > 0,
    queryFn: async () => {
      const d = await apiGet<WasteResult>(`/api/k8s/resource-waste?cluster_id=${clusterId}`)
      const items = d.items ?? d.list ?? []
      // summary 拿不到就是 null，不要造一个全 0 的对象 ——
      // 全 0 的汇总会显示成「浪费 0 核」，也就是「这个集群很健康」
      return { items, summary: d.summary ?? null, note: d.note ?? '', error: d.error ?? '' }
    },
    staleTime: 5 * 60_000,
    retry: shouldRetry,
  })
}
