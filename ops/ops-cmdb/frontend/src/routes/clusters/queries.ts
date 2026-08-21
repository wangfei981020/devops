import { queryKeys, shouldRetry } from '@ops/api'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api } from '../../lib/api.js'
import { apiAction, apiSend } from '../../lib/fetchJson.js'

/**
 * 集群列表。
 *
 * ⚠️ 计数用 `number | null`：null 是「集群纳管了但没采到」，
 * 0 是「采到了，里面确实是空的」。压成 0 会让一个采集断了的集群
 * 在界面上显示成"空集群"，而空集群是不需要任何人去处理的。
 */
export interface Cluster {
  id: number
  name: string
  displayName: string
  environment: string
  provider: string
  location: string
  projectId: string
  enabled: boolean
  /** 集群侧资源是否采进来了。false 时下面的计数全是 null */
  ingested: boolean
  nodes: number | null
  nodesReady: number | null
  pods: number | null
  podsBad: number | null
  /** 多于一个 = 正在升级中，或者升级卡住了 */
  kubeletVersions: string[]
  syncedAt: string | null
  hasKubeconfig: boolean
}

export type EnvFilter = string
export type ClusterSort = 'name' | 'env' | 'nodes' | 'pods' | 'synced'
export type ClusterSortParam = ClusterSort | `-${ClusterSort}`

export interface ClusterListParams {
  page: number
  size: number
  q?: string
  env?: string
  sort?: ClusterSortParam
  [k: string]: string | number | undefined
}

export interface ClusterListResult {
  items: Cluster[]
  total: number
  facets: Record<string, Record<string, number>>
}

interface RawCluster {
  id?: number
  name?: string
  display_name?: string
  environment?: string
  provider?: string
  location?: string
  project_id?: string
  enabled?: boolean
  ingested?: boolean
  nodes?: number | null
  nodes_ready?: number | null
  pods?: number | null
  pods_bad?: number | null
  kubelet_versions?: string[]
  synced_at?: string
  has_kubeconfig?: boolean
}

/** `?? 0` 在这里是错的：见 Cluster 的注释。undefined 与 null 一律归为 null。 */
function num(v: number | null | undefined): number | null {
  return v === undefined || v === null ? null : v
}

function toCluster(r: RawCluster): Cluster {
  return {
    id: r.id ?? 0,
    name: r.name ?? '',
    displayName: r.display_name || (r.name ?? ''),
    environment: r.environment ?? '',
    provider: r.provider ?? '',
    location: r.location ?? '',
    projectId: r.project_id ?? '',
    enabled: r.enabled !== false,
    ingested: r.ingested === true,
    nodes: num(r.nodes),
    nodesReady: num(r.nodes_ready),
    pods: num(r.pods),
    podsBad: num(r.pods_bad),
    kubeletVersions: r.kubelet_versions ?? [],
    syncedAt: r.synced_at ? r.synced_at : null,
    hasKubeconfig: r.has_kubeconfig === true,
  }
}

export function useClusters(params: ClusterListParams) {
  return useQuery({
    queryKey: queryKeys.clusters.list(params),
    queryFn: async (): Promise<ClusterListResult> => {
      const { data, error } = await api.GET('/k8s/cluster-list', {
        params: { query: params },
      })
      if (error) throw error
      return {
        items: ((data?.items ?? []) as RawCluster[]).map(toCluster),
        total: data?.total ?? 0,
        facets: (data?.facets ?? {}) as Record<string, Record<string, number>>,
      }
    },
    staleTime: 30_000,
    retry: shouldRetry,
  })
}

/** 纳管一个集群需要填的东西。 */
export interface ClusterInput {
  name: string
  display_name: string
  environment: string
  provider: string
  location: string
  /**
   * ⚠️ 留空 = 保持原值（编辑）/ 不配（新建）。接口从不回传它。
   *
   * 只读采集需要的是 get/list/watch 权限。k8s 自带的 `view` 角色**不含集群级资源**
   * （nodes、PV），拿它去采会得到一句 `nodes is forbidden` ——
   * 这时候最容易图省事绑 cluster-admin，别那么干。
   */
  kubeconfig: string
  /**
   * 该集群在 Prometheus 指标里 cluster 标签的取值。
   * 与集群名不一致时**必须填**，否则所有带集群隔离的查询都会静默返回空。
   */
  prom_cluster_value: string
  enabled: number
  /**
   * GKE 经云账号连接所需的四件套（自动发现导入时一并带上）。
   *
   * ⚠️ 后端 `k8ssource/pool.go` 里连接方式的判定是：
   * `provider=="gke" && cloud_account_id>0 && endpoint!="" && ca_data!=""`。
   * **四个缺一个就退回"未配置连接方式"**——而发现接口本来就把
   * endpoint 和 ca 都返回了，不带上纯属把已有的信息扔掉，
   * 逼用户再手配一次 kubeconfig。
   */
  cloud_account_id?: number
  project_id?: string
  endpoint?: string
  /** 集群 CA（base64 PEM），来自发现结果的 `ca` */
  ca_data?: string
}

function useDone() {
  const qc = useQueryClient()
  return () => void qc.invalidateQueries()
}

export function useSaveCluster() {
  const done = useDone()
  return useMutation({
    mutationFn: ({ id, ...body }: ClusterInput & { id?: number }) =>
      id ? apiSend(`/api/k8s/clusters/${id}`, 'PUT', body) : apiSend('/api/k8s/clusters', 'POST', body),
    onSuccess: done,
  })
}

export function useDeleteCluster() {
  const done = useDone()
  return useMutation({
    mutationFn: (id: number) => apiSend(`/api/k8s/clusters/${id}`, 'DELETE'),
    onSuccess: done,
  })
}

/** 测连通。**保存后立刻测**：凭据错的集群和没接一样，但列表里它看着是好的。 */
export function useTestCluster() {
  return useMutation({
    mutationFn: (id: number) =>
      apiAction<{ ok?: boolean; nodes?: number; version?: string; error?: string }>(
        `/api/k8s/clusters/${id}/test`,
      ),
  })
}

/** 立即采集一次。后台异步，成功只代表任务收下了。 */
export function useSyncCluster() {
  const done = useDone()
  return useMutation({
    mutationFn: (id: number) => apiSend(`/api/k8s/clusters/${id}/sync`, 'POST'),
    onSuccess: done,
  })
}
