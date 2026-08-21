import { queryKeys, shouldRetry } from '@ops/api'
import { useQuery } from '@tanstack/react-query'
import { api } from '../../lib/api.js'

export interface Workload {
  clusterId: number
  clusterName: string
  /** 集群别名；没设时等于 clusterName。渲染一律走 clusterLabel（OPSCMDB-078） */
  clusterDisplay: string
  namespace: string
  /** Deployment / StatefulSet / DaemonSet / CronJob，k8s 原值不翻译 */
  kind: string
  name: string
  replicasDesired: number
  replicasReady: number
  image: string
  imageTag: string
  status: string
  syncedAt: string | null
}

/**
 * 健康度。
 *
 * ⚠️ `scaledZero` 必须独立于 `down`：两者都是"就绪 0 个"，
 * 但一个要立刻处理，一个什么都不用做（停服、CronJob 模板、金丝雀留空）。
 * 合成一档就是在每套环境里制造一批假故障，而假故障多了真的就没人看了。
 */
export type WorkloadHealth = 'all' | 'down' | 'degraded' | 'scaled_zero' | 'ok'

/**
 * 健康度值 → 语言包 key。
 *
 * ⚠️ 不能直接拿接口枚举值当 key。i18next 把 `_zero` / `_one` / `_few` /
 * `_many` / `_other` 当**复数后缀**（它们是 CLDR 的复数类别），
 * 于是 `health.scaled_zero` 会被解析成"key=scaled 的 zero 形式"——
 * 语言包检查会报"缺 _one/_other、多出 _zero"，而运行时那条文案直接取不到。
 * 接口枚举保持蛇形（那是 API 契约），语言包 key 一律 camelCase。
 */
export function healthLabelKey(h: Exclude<WorkloadHealth, 'all'>): string {
  return h === 'scaled_zero' ? 'scaledZero' : h
}

export function healthOf(w: Workload): Exclude<WorkloadHealth, 'all'> {
  if (w.replicasDesired === 0) return 'scaled_zero'
  if (w.replicasReady === 0) return 'down'
  if (w.replicasReady < w.replicasDesired) return 'degraded'
  return 'ok'
}

export interface WorkloadListParams {
  page: number
  size: number
  q?: string
  cluster?: string
  namespace?: string
  health?: WorkloadHealth
  [k: string]: string | number | undefined
}

export interface WorkloadListResult {
  items: Workload[]
  total: number
  facets: Record<string, Record<string, number> | undefined>
}

interface RawWorkload {
  cluster_id?: number
  cluster_name?: string
  cluster_display_name?: string
  namespace?: string
  kind?: string
  name?: string
  replicas_desired?: number
  replicas_ready?: number
  image?: string
  image_tag?: string
  status?: string
  synced_at?: string
}

function toWorkload(r: RawWorkload): Workload {
  return {
    clusterId: r.cluster_id ?? 0,
    clusterName: r.cluster_name ?? '',
    clusterDisplay: r.cluster_display_name ?? '',
    namespace: r.namespace ?? '',
    kind: r.kind ?? '',
    name: r.name ?? '',
    replicasDesired: r.replicas_desired ?? 0,
    replicasReady: r.replicas_ready ?? 0,
    image: r.image ?? '',
    imageTag: r.image_tag ?? '',
    status: r.status ?? '',
    syncedAt: r.synced_at ? r.synced_at : null,
  }
}

export function useWorkloads(params: WorkloadListParams) {
  return useQuery({
    queryKey: queryKeys.workloads.list(params),
    queryFn: async (): Promise<WorkloadListResult> => {
      const { data, error } = await api.GET('/k8s/workload-list', { params: { query: params } })
      if (error) throw error
      return {
        items: ((data?.items ?? []) as RawWorkload[]).map(toWorkload),
        total: data?.total ?? 0,
        facets: (data?.facets ?? {}) as Record<string, Record<string, number> | undefined>,
      }
    },
    staleTime: 30_000,
    retry: shouldRetry,
  })
}
