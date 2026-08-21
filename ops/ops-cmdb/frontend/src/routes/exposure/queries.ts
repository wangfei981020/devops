export interface Exposure {
  kind: string
  name: string
  endpoint: string
  ports: string
  scope: string
  /**
   * ⚠️ null = 我们**没法判断**有没有防护（没接 CDN/WAF 数据源），不是"没有防护"。
   * 兜底成 false 会让这张清单声称"这些入口全都裸奔"——
   * 假报告比没有报告更糟。
   */
  protected: boolean | null
}

import { shouldRetry } from '@ops/api'
import { useQuery } from '@tanstack/react-query'
import { apiGet } from '../../lib/fetchJson.js'

export interface ExposureListResult {
  items: Exposure[]
  total: number
  /** 分面缺失 ≠ 计数为 0，前端不显示计数而不是显示 0 */
  facets: Record<string, Record<string, number> | undefined>
}

export function useExposures(params: Record<string, string | number>) {
  const qs = new URLSearchParams(
    Object.entries(params).filter(([, v]) => v !== '' && v !== undefined).map(([k, v]) => [k, String(v)]),
  ).toString()
  return useQuery({
    queryKey: ['exposure', qs],
    queryFn: () => apiGet<ExposureListResult>(`/api/exposure-list?${qs}`),
    staleTime: 30_000,
    retry: shouldRetry,
  })
}
