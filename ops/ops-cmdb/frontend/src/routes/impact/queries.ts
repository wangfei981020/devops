import { shouldRetry } from '@ops/api'
import { useQuery } from '@tanstack/react-query'
import { apiGet } from '../../lib/fetchJson.js'

export interface ImpactNode {
  ci_id: number
  name: string
  type: string
  depth: number
  via: string
}

export interface Impact {
  root: string
  affected: ImpactNode[]
  /** 因深度上限而截断。⚠️ 截断了必须说 —— 悄悄少一半的影响面比没有更危险 */
  truncated: boolean
  /**
   * ⚠️ 这个资源有没有**任何**关系记录。
   * false 时界面要说"我们没有它的关系数据"，而不是"影响面：无"——
   * 后者会被当成"随便动"，而那正是变更把线上打挂的经典路径。
   */
  has_relations: boolean
}

export function useImpact(ciId: number) {
  return useQuery({
    queryKey: ['impact', ciId],
    enabled: ciId > 0,
    queryFn: () => apiGet<Impact>(`/api/impact?ci_id=${ciId}`),
    staleTime: 60_000,
    retry: shouldRetry,
  })
}
