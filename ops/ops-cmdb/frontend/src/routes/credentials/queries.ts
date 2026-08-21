import { shouldRetry } from '@ops/api'
import { useQuery } from '@tanstack/react-query'
import { apiGet } from '../../lib/fetchJson.js'
import type { DataSource } from '../datasources/queries.js'

/**
 * 凭据管理。
 *
 * ⚠️ 这一页**看不到任何凭据内容**，也不该能看到。接口只返回"配没配"。
 * 它回答的是三个问题：
 *   · 哪些地方存着凭据（攻击面清单）
 *   · 有没有该配却没配的（那个数据源采不到任何东西）
 *   · 有没有配了却一直同步失败的（凭据可能已经失效/被吊销）
 *
 * 轮换与修改走各自的配置页，不在这里做 —— 这里是**盘点**视角。
 */
export type Credential = DataSource

export function useCredentials() {
  return useQuery({
    queryKey: ['credentials'],
    queryFn: () => apiGet<{ items: Credential[]; total: number }>('/api/datasource-list?size=200'),
    staleTime: 60_000,
    retry: shouldRetry,
  })
}
