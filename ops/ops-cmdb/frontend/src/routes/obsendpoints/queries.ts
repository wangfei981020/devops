import { shouldRetry } from '@ops/api'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { apiAction, apiGet, apiSend } from '../../lib/fetchJson.js'

export interface ObsEndpoint {
  id: number
  name: string
  /** prometheus / loki / kubesphere / n9e —— 后端有白名单，打错会被拒 */
  type: string
  url: string
  env: string
  cluster_id: number
  cluster_label: string
  enabled: number
  has_token?: boolean
}

/**
 * ⚠️ 类型必须从后端白名单里选，不能自由输入。
 * 打错一个字母（n9E / nightingle）会静默建出一条永远不工作的记录：
 * 查询时按 type 找不到源，而界面上它显示"已启用"。
 */
export const OBS_TYPES = ['prometheus', 'loki', 'kubesphere', 'n9e'] as const

/**
 * 集群隔离标签的常见取值，给下拉当兜底选项。
 *
 * ⚠️ 这些是**标签名**不是标签值 —— 值来自集群自身的配置（k8s_clusters.prom_cluster_value）。
 * 字段名以前叫「集群标签值」、帮助文字写「标签的取值」，两处都是错的，
 * 照字面填个集群名进去，查询会静默返回 0 条。
 *
 * 能探测到真实标签名时优先用探测结果，这里只是探测不到时的候选。
 */
export const CLUSTER_LABEL_PRESETS = ['cluster', 'k8s_cluster', 'cluster_name'] as const

export interface LabelNamesResult {
  ok: boolean
  /** 该类型数据源是否有"标签名"这个概念（Loki / 夜莺没有） */
  supported: boolean
  names: string[]
  all_count?: number
  /** 说明文案的语言包 key。⚠️ 优先于 note —— note 是留给 MCP 的中文原句（OPSCMDB-054） */
  note_key?: string
  note_params?: { total?: number }
  note?: string
  error_key?: string
  error_params?: { reason?: string }
  error?: string
}

/**
 * 探测数据源里实际有哪些标签名。
 *
 * ⚠️ 只对已保存的数据源可用（要拿它的地址和令牌去问）。
 * 新建时还没有 id，只能给候选值 —— 这一点必须在界面上说明白，
 * 不然用户会以为"这个源探测不出标签"。
 */
export function useLabelNames(id: number | null, type: string) {
  return useQuery({
    queryKey: ['obs-label-names', id],
    queryFn: () => apiGet<LabelNamesResult>(`/api/obs-endpoints/${id}/label-names`),
    // 只在编辑一个已存在的 prometheus 源时才探测：
    // 其它类型探了也没有标签概念，新建时没有 id
    enabled: !!id && id > 0 && type === 'prometheus',
    staleTime: 60_000,
    retry: false,
  })
}

export function useObsEndpoints() {
  return useQuery({
    queryKey: ['obs-endpoints'],
    queryFn: async () => {
      const d = await apiGet<ObsEndpoint[] | { items?: ObsEndpoint[] }>('/api/obs-endpoints')
      return { items: Array.isArray(d) ? d : (d.items ?? []) }
    },
    staleTime: 30_000,
    retry: shouldRetry,
  })
}

function useDone() {
  const qc = useQueryClient()
  return () => void qc.invalidateQueries()
}

export interface ObsInput {
  name: string
  type: string
  url: string
  env: string
  cluster_id: number
  cluster_label: string
  /** 留空 = 保持原值（编辑）/ 不配（新建）。接口从不回传它 */
  token: string
  enabled: number
}

export function useSaveObs() {
  const done = useDone()
  return useMutation({
    mutationFn: ({ id, ...body }: ObsInput & { id?: number }) =>
      id ? apiSend(`/api/obs-endpoints/${id}`, 'PUT', body) : apiSend('/api/obs-endpoints', 'POST', body),
    onSuccess: done,
  })
}

export function useDeleteObs() {
  const done = useDone()
  return useMutation({
    mutationFn: (id: number) => apiSend(`/api/obs-endpoints/${id}`, 'DELETE'),
    onSuccess: done,
  })
}

/** 测连通性。**保存前先测**：配错了地址的数据源和没配一样，但界面上看着是好的。 */
/** 连通性测试的完整结果。⚠️ 后端给的远不止「通/不通」，都要用上 */
export interface ObsTestResult {
  ok?: boolean
  error?: string
  /** HTTP 状态码 */
  status?: number
  /** 实际探通的那条路径 —— 地址填错时这一条最能说明问题 */
  path?: string
  /** 耗时。「连通」和「连通但花了 8 秒」是两种健康度 */
  duration_ms?: number
  /** 每条候选路径的尝试结果。失败时这里才有真正的原因 */
  tried?: { path?: string; status?: number; error?: string }[]
}

export function useTestObs() {
  return useMutation({
    // ⚠️ 结果里带 status / path / duration_ms / tried —— 原来前端只用了 ok，
    // 把这么多信息压成了「连通」两个字（P1-64）
    mutationFn: (id: number) => apiAction<ObsTestResult>(`/api/obs-endpoints/${id}/test`),
  })
}


