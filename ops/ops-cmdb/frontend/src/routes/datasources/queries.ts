export interface DataSource {
  kind: string
  name: string
  type: string
  enabled: boolean
  /** ⚠️ 只有"配没配"，**没有凭据内容** —— 接口本来就不返回 */
  /**
   * 本条记录里存没存凭据。
   * 🔴 **不要拿它当健康判据**——「没存凭据」≠「采不到东西」：
   * GKE 走云账号 service account 不存 kubeconfig；内网 Prometheus/Loki 无鉴权。
   * 健康状况看 health 字段。
   */
  has_credential: boolean
  /** configured=本条存了 / inherited=不用自己存（GKE 走云账号）/ none=没存 */
  credential?: 'configured' | 'inherited' | 'none'
  /**
   * 这类数据源**没凭据就一定采不到**（目前只有云账号是这样）。
   *
   * 🔴 「该有凭据却没有」和「凭据字段为空」是两件事。凭据管理页原来按
   * `has_credential=false` 分组，于是 3 个刚同步成功的 K8s 集群被列进
   * 「启用了但没配凭据」——它们走集群内 SA / 云账号继承，本来就不需要配
   * （OPSCMDB-031 P2-50）。误报和真问题混在同一个数字里，那个数字就没人信了。
   */
  cred_required?: boolean
  /** ok / no_credential / stale / never_synced / disabled —— 后端算好的唯一判据 */
  health?: string
  /** 这个判定是怎么来的，给人看 */
  health_note?: string
  last_sync_at?: string
  last_result: string
  /** 早该同步了却没有：数据源静默停摆 */
  stale: boolean
}

import { shouldRetry } from '@ops/api'
import { useQuery } from '@tanstack/react-query'
import { apiGet } from '../../lib/fetchJson.js'

export interface DataSourceListResult {
  items: DataSource[]
  total: number
  /** 分面缺失 ≠ 计数为 0，前端不显示计数而不是显示 0 */
  facets: Record<string, Record<string, number> | undefined>
}

export function useDataSources(params: Record<string, string | number>) {
  const qs = new URLSearchParams(
    Object.entries(params).filter(([, v]) => v !== '' && v !== undefined).map(([k, v]) => [k, String(v)]),
  ).toString()
  return useQuery({
    queryKey: ['datasources', qs],
    queryFn: () => apiGet<DataSourceListResult>(`/api/datasource-list?${qs}`),
    staleTime: 30_000,
    retry: shouldRetry,
  })
}
