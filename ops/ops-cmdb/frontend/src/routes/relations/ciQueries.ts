import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { apiAction, apiGet } from '../../lib/fetchJson.js'

/**
 * 配置项（CI）—— CMDB 里所有对象的公共底座：域名、证书、主机、集群都是 CI。
 *
 * ⚠️ 后端 4 个写接口（建/改/删/打标签）以前**一个都没接**。
 * 后果是：采集不到的东西就永远进不了台账 —— 机房里的物理机、别人代管的服务、
 * 一条纯粹用来挂关系的逻辑对象。而"CMDB 里没有"和"现实中没有"
 * 在界面上长得一模一样。
 */
export interface CI {
  id: number
  type: string
  name: string
  project: string
  env: string
  module: string
  owner: string
  status: string
  remark: string
  labels?: Record<string, string>
}

export interface CIQuery {
  q?: string
  type?: string
  env?: string
  project?: string
}

export function useCIs(params: CIQuery, enabled = true) {
  const qs = new URLSearchParams(
    Object.entries(params)
      .filter(([, v]) => v !== '' && v !== undefined && v !== 'all')
      .map(([k, v]) => [k, String(v)]),
  ).toString()
  return useQuery({
    queryKey: ['cis', qs],
    queryFn: () => apiGet<CI[]>(`/api/cis${qs ? `?${qs}` : ''}`),
    enabled,
    staleTime: 30_000,
  })
}

export interface CIInput {
  type: string
  name: string
  project?: string
  env?: string
  module?: string
  owner?: string
  status?: string
  remark?: string
}

function useDone() {
  const qc = useQueryClient()
  return () => {
    void qc.invalidateQueries({ queryKey: ['cis'] })
    // 关系图谱按 CI 名展示，改名之后那边也得跟着变
    void qc.invalidateQueries({ queryKey: ['relations'] })
  }
}

export function useCreateCI() {
  const done = useDone()
  return useMutation({
    retry: false,
    mutationFn: (v: CIInput) => apiAction<{ ok?: boolean; error?: string; id?: number }>('/api/cis', 'POST', v),
    onSuccess: done,
  })
}

export function useUpdateCI() {
  const done = useDone()
  return useMutation({
    retry: false,
    mutationFn: ({ id, ...v }: CIInput & { id: number }) =>
      apiAction(`/api/cis/${id}`, 'PUT', v),
    onSuccess: done,
  })
}

/**
 * 删除 CI。
 *
 * ⚠️ 域名、证书这些**有自己专属页面**的类型不要从这里删 —— 那些页面的删除
 * 会一并清掉附属数据（解析记录、续费台账）。从 CI 这一层删只删主体，
 * 附属数据会变成没人认领的孤儿。界面上按类型挡住了这一点。
 */
export function useDeleteCI() {
  const done = useDone()
  return useMutation({
    retry: false,
    mutationFn: (id: number) => apiAction(`/api/cis/${id}`, 'DELETE'),
    onSuccess: done,
  })
}

/** 标签整体替换（不是增量合并）—— 传进去的就是最终结果 */
export function useUpdateCILabels() {
  const done = useDone()
  return useMutation({
    retry: false,
    mutationFn: ({ id, labels }: { id: number; labels: Record<string, string> }) =>
      apiAction(`/api/cis/${id}/labels`, 'PUT', { labels }),
    onSuccess: done,
  })
}
