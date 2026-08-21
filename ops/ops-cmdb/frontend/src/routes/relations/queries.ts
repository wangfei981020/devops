export interface Relation {
  id: number
  src_ci_id: number
  src_name: string
  src_type: string
  dst_ci_id: number
  dst_name: string
  dst_type: string
  rel_type: string
  /** sync = 采集推断（下轮同步可能消失）/ manual = 人工登记（不会） */
  origin: string
}

import { shouldRetry } from '@ops/api'
import { useQuery } from '@tanstack/react-query'
import { apiGet } from '../../lib/fetchJson.js'

export interface RelationListResult {
  items: Relation[]
  total: number
  /** 分面缺失 ≠ 计数为 0，前端不显示计数而不是显示 0 */
  facets: Record<string, Record<string, number> | undefined>
}

export function useRelations(params: Record<string, string | number>) {
  const qs = new URLSearchParams(
    Object.entries(params).filter(([, v]) => v !== '' && v !== undefined).map(([k, v]) => [k, String(v)]),
  ).toString()
  return useQuery({
    queryKey: ['relations', qs],
    queryFn: () => apiGet<RelationListResult>(`/api/relation-list?${qs}`),
    staleTime: 30_000,
    retry: shouldRetry,
  })
}

import { useMutation, useQueryClient } from '@tanstack/react-query'
import { apiAction } from '../../lib/fetchJson.js'

/**
 * 手工建一条关系。
 *
 * ⚠️ 图谱里的边大部分是自动推出来的（证书护着哪个域名、域名指向哪个 LB）。
 * 手工建边是给推不出来的那些用的 —— 比如跨系统的依赖。
 * 入口以前没有，于是"图上少一条边"这件事只能干看着。
 */
export function useCreateRelation() {
  const qc = useQueryClient()
  return useMutation({
    retry: false,
    mutationFn: (b: { src_ci_id: number; dst_ci_id: number; rel_type: string }) =>
      apiAction('/api/relations', 'POST', b),
    onSuccess: () => void qc.invalidateQueries({ queryKey: ['relations'] }),
  })
}

export function useDeleteRelation() {
  const qc = useQueryClient()
  return useMutation({
    retry: false,
    mutationFn: (id: number) => apiAction(`/api/relations/${id}`, 'DELETE'),
    onSuccess: () => void qc.invalidateQueries({ queryKey: ['relations'] }),
  })
}

/**
 * 拓扑里的域名清单：哪些对外域名落在这套资源上。
 *
 * ⚠️ 它回答的是「这个项目/模块对外暴露了哪些域名」——
 * 和「资源图谱」的边不是一回事，图谱只画已记录的关系，
 * 而域名是从主机头台账推出来的，两者互补。
 */
export interface TopoDomain {
  name?: string
  project?: string
  env?: string
  module?: string
}

export function useTopologyDomains() {
  return useQuery({
    queryKey: ['topology-domains'],
    queryFn: () => apiGet<TopoDomain[]>('/api/k8s/topology-domains'),
    staleTime: 60_000,
    retry: shouldRetry,
  })
}
