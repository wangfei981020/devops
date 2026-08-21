import { shouldRetry } from '@ops/api'
import { useQuery } from '@tanstack/react-query'
import { apiGet } from '../../lib/fetchJson.js'

/**
 * 续费记录台账。
 *
 * ⚠️ 这是**扣过费的历史**，不是任务进度。批量续费那个 2 小时后就清了，
 * 真正要追溯"这个域名什么时候花了多少钱"只能看这里。
 *
 * ⚠️ `status` 必须三态显示：成功 / 失败 / **不确定**。
 * 不确定 = 已经发出扣费请求但没拿到厂商确认——它既不能当成功也不能当失败，
 * 压成 bool 会导致要么重复扣费、要么以为没续上（见 project_cmdb_domain_renew_safety）。
 */
export interface Renewal {
  id?: number
  domain?: string
  years?: number
  status?: string
  amount?: number
  currency?: string
  operator?: string
  at?: string
  detail?: string
  order_id?: string
}

export function useRenewals() {
  return useQuery({
    queryKey: ['renewals'],
    queryFn: () => apiGet<{ items?: Renewal[]; total?: number }>('/api/renewals'),
    staleTime: 30_000,
    retry: shouldRetry,
  })
}
