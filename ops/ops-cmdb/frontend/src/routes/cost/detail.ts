import { shouldRetry } from '@ops/api'
import { useQuery } from '@tanstack/react-query'
import { apiGet } from '../../lib/fetchJson.js'

/**
 * 成本的四个下钻：月份 / 明细 / 归因 / 报表 + 闲置。
 *
 * 老 CMDB 有这几个，新版只接了一个总览数字（OPSCMDB-021）——
 * 于是「这个月贵了 300 刀」看得到，「贵在谁头上」查不到。
 *
 * ⚠️ 成本数字全是**按机型与磁盘估算**的，不是云账单。
 * 每个页面都要把这句话带出去，否则会被拿去对账然后发现对不上。
 */

export function useCostMonths() {
  return useQuery({
    queryKey: ['cost-months'],
    queryFn: () => apiGet<string[]>('/api/k8s/cost/months'),
    staleTime: 5 * 60_000,
    retry: shouldRetry,
  })
}

export interface CostItem {
  source?: string
  cluster_id?: number
  cluster?: string
  mode?: string
  gcp_project?: string
  biz_project?: string
  env?: string
  type?: string
  namespace?: string
  name?: string
  node?: string
  cost?: number
}

/**
 * 成本明细。
 *
 * 🔴 **没有月份维度**：这个接口从 k8s_pods / hosts 现算当前成本，
 *	不查 cost_snapshots。原来这里传 `month=` —— handler 根本不读它，
 *	于是界面上选 7 月看到的仍是当前值，不报错也不空（OPSCMDB-049）。
 *	要看历史月份用 useCostReport / useCostAttribution。
 */
export function useCostDetail() {
  return useQuery({
    queryKey: ['cost-detail'],
    queryFn: () =>
      apiGet<{ items?: CostItem[]; count?: number; total?: number; currency?: string }>(
        `/api/k8s/cost/detail`,
      ),
    staleTime: 60_000,
    retry: shouldRetry,
  })
}

/** 环比变化的「谁涨了」。⚠️ reason 是后端算出来的，别在前端另编一套说法。 */
export interface Mover {
  cluster?: string
  project?: string
  resource?: string
  type?: string
  old?: number
  new?: number
  delta?: number
  reason?: string
}

export function useCostAttribution(month: string, dim: string) {
  return useQuery({
    queryKey: ['cost-attribution', month, dim],
    queryFn: () =>
      apiGet<{ month?: string; delta?: number; movers?: Mover[] }>(
        `/api/k8s/cost/attribution?month=${encodeURIComponent(month)}&dim=${dim}`,
      ),
    enabled: !!month,
    staleTime: 60_000,
    retry: shouldRetry,
  })
}

export interface CostReport {
  anchor?: string
  period?: string
  dim?: string
  total?: number
  prev_total?: number
  delta?: number
  months?: string[]
  groups?: { name?: string; cost?: number }[]
  trend?: { month?: string; cost?: number }[]
}

/**
 * 月度报表。
 *
 * 🔴 参数名是 **anchor** 不是 month。
 *	这里原来传的是 `month=` —— 而 handler 只读 anchor，于是那个参数被静默忽略，
 *	**不管选哪个月都返回当前月的数据**。实测 2026-07 显示的是 8 月的 842.65，
 *	而 7 月真实是 2705.7，差 3 倍 —— 不报错、不空，只是答非所问（OPSCMDB-049）。
 *	响应里的 anchor 字段说的才是实际生效的月份，界面必须显示它。
 */
export function useCostReport(month: string) {
  return useQuery({
    queryKey: ['cost-report', month],
    queryFn: () => apiGet<CostReport>(`/api/k8s/cost/report?anchor=${encodeURIComponent(month)}`),
    enabled: !!month,
    staleTime: 60_000,
    retry: shouldRetry,
  })
}

export interface IdleCluster {
  cluster_id?: number
  cluster?: string
  nodes?: number
  actual_monthly_usd?: number
  allocated_monthly_usd?: number
  idle_monthly_usd?: number
  idle_yearly_usd?: number
  idle_pct?: number
  cpu_request_pct?: number
  mem_request_pct?: number
  /** 后端给的口径说明，**必须原样显示** —— 闲置的定义不写清楚会被误读成"浪费" */
  note?: string
}

/**
 * 闲置成本。
 *
 * ⚠️ 口径是「实付 − 已按 request 分摊」，衡量的是**买了没分配出去**，
 * 不是「分配了没用起来」。后者要看 resource-waste（那个依赖 Prometheus 实测）。
 * 两个概念混起来会得出完全相反的结论，所以这里把后端的 note 原样带出去。
 */
export function useIdleCost(clusterId: number) {
  return useQuery({
    queryKey: ['idle-cost', clusterId],
    queryFn: () =>
      apiGet<{ clusters?: IdleCluster[] }>(
        clusterId > 0 ? `/api/k8s/idle-cost?cluster_id=${clusterId}` : '/api/k8s/idle-cost',
      ),
    staleTime: 60_000,
    retry: shouldRetry,
  })
}
