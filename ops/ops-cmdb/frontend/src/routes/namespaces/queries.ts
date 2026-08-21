import { queryKeys, shouldRetry } from '@ops/api'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api } from '../../lib/api.js'
import { apiAction, apiGet } from '../../lib/fetchJson.js'

export interface Namespace {
  clusterId: number
  clusterName: string
  /** 集群别名；没设时等于 clusterName。渲染一律走 clusterLabel（OPSCMDB-078） */
  clusterDisplay: string
  name: string
  /** Active / Terminating，k8s 原值不翻译 */
  phase: string
  /** 空 = 没有归属登记，不是"没有项目" */
  project: string
  /** null = 该集群还没采过这类资源；0 = 确实是空的 */
  workloads: number | null
  pods: number | null
  podsBad: number | null
  syncedAt: string | null
}

export interface NamespaceListParams {
  page: number
  size: number
  q?: string
  cluster?: string
  phase?: string
  [k: string]: string | number | undefined
}

export interface NamespaceListResult {
  items: Namespace[]
  total: number
  facets: Record<string, Record<string, number> | undefined>
}

interface RawNamespace {
  cluster_id?: number
  cluster_name?: string
  cluster_display_name?: string
  name?: string
  phase?: string
  project?: string
  workloads?: number | null
  pods?: number | null
  pods_bad?: number | null
  synced_at?: string
}

/** `?? 0` 在这里是错的：null 表示没采过，0 表示确实是空的。 */
function num(v: number | null | undefined): number | null {
  return v === undefined || v === null ? null : v
}

function toNamespace(r: RawNamespace): Namespace {
  return {
    clusterId: r.cluster_id ?? 0,
    clusterName: r.cluster_name ?? '',
    clusterDisplay: r.cluster_display_name ?? '',
    name: r.name ?? '',
    phase: r.phase ?? '',
    project: r.project ?? '',
    workloads: num(r.workloads),
    pods: num(r.pods),
    podsBad: num(r.pods_bad),
    syncedAt: r.synced_at ? r.synced_at : null,
  }
}

export function useNamespaces(params: NamespaceListParams) {
  return useQuery({
    queryKey: queryKeys.namespaces.list(params),
    queryFn: async (): Promise<NamespaceListResult> => {
      const { data, error } = await api.GET('/k8s/namespace-list', { params: { query: params } })
      if (error) throw error
      return {
        items: ((data?.items ?? []) as RawNamespace[]).map(toNamespace),
        total: data?.total ?? 0,
        facets: (data?.facets ?? {}) as Record<string, Record<string, number> | undefined>,
      }
    },
    staleTime: 30_000,
    retry: shouldRetry,
  })
}

/**
 * 命名空间 → 业务项目 的归属。
 *
 * # 为什么这一页值得单独做
 *
 * 成本归属全靠它。没有归属的命名空间，它们的钱会全部落进「未归属」——
 * 而「未归属」这一坨在成本报表上看起来像一个巨大的、无人负责的项目。
 *
 * ⚠️ 后端给的 `suggest` 是**建议不是结论**：它按命名前缀猜，
 * 平台组件（argocd、istio-system）会被显式标成"不属于任何业务项目"。
 * 界面必须把 `suggest_reason` 显示出来，否则人无法判断该不该采纳。
 */
export interface NsProject {
  name: string
  /** 当前归属。空 = 未归属 */
  project: string
  /** 系统建议的归属。空 = 猜不出来 */
  suggest: string
  /** 为什么这么建议 / 为什么猜不出来。**必须显示** */
  suggest_reason: string
  /** exact / prefix / none / platform —— 匹配规则，原样透传 */
  suggest_rule: string
}

export function useNsProjects(clusterID: number) {
  return useQuery({
    queryKey: ['ns-projects', clusterID],
    queryFn: () => apiGet<NsProject[]>(`/api/k8s/ns-projects?cluster_id=${clusterID}`),
    enabled: clusterID > 0,
    staleTime: 30_000,
    retry: shouldRetry,
  })
}

export function useSetNsProject(clusterID: number) {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (v: { namespace: string; project: string }) =>
      apiAction('/api/k8s/ns-projects', 'POST', { cluster_id: clusterID, ...v }),
    onSuccess: () => void qc.invalidateQueries({ queryKey: ['ns-projects', clusterID] }),
  })
}

export interface AutoResult {
  dry_run?: boolean
  applied?: number
  will_apply?: number
  items: NsProject[]
  stat?: Record<string, number>
  msg?: string
  /** 一个项目都没有时后端给的说明 */
  error?: string
  hint?: string
  hint_key?: string
}

/**
 * 按命名规律自动归属。
 *
 * ⚠️ **默认预览**（`dry_run` 不传 = 预览）。真写入要显式传 `dry_run=0`。
 * 这是后端的设计，前端不能绕过去 —— 一次点击改掉几十个命名空间的成本归属，
 * 必须先让人看到要改哪些。
 */
export function useAutoNsProjects(clusterID: number) {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (apply: boolean) =>
      apiAction<AutoResult>(
        `/api/k8s/ns-projects/auto?cluster_id=${clusterID}${apply ? '&dry_run=0' : ''}`,
        'POST',
      ),
    onSuccess: (_d, apply) => {
      if (apply) void qc.invalidateQueries({ queryKey: ['ns-projects', clusterID] })
    },
  })
}
