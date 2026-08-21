import { queryKeys, shouldRetry } from '@ops/api'
import { useQuery } from '@tanstack/react-query'
import { api } from '../../lib/api.js'

export interface LB {
  id: number
  name: string
  project: string
  region: string
  /** EXTERNAL / INTERNAL，原样透传 */
  scheme: string
  vip: string
  portRange: string
  protocol: string
  target: string
  provider: string
  /**
   * ⚠️ 三态，别压成数字：
   *   null 这个项目的后端压根没采过 —— 我们不知道
   *   0    采过了，确认一个后端都没有 —— 这条 LB 打不通
   *   n    正常
   * 生产上出过事故：8 条正在服务的 LB 被显示成"无后端"，
   * 真因是那条查询从来没成功过，而"没采到"被渲染成了"确实是 0"。
   */
  backends: number | null
  backendState: string
  /**
   * 命中的 K8s Service（"集群 · 命名空间/服务名"），空 = 没对上。
   *
   * ⚠️ 后端一直在返回 `k8s_service`，前端类型里没声明 ——
   * 于是 GKE 的 Service type=LoadBalancer（后端是 Pod/NEG，实例组里看不到）
   * 只能显示成"0 个后端"，而它们**正在服务**。
   * 生产实测 8 条命中 8 条，全是 UAT 在跑的 Kafka / ZK / RocketMQ / Istio 入口。
   */
  k8sService: string
  stale: boolean
  syncedAt: string | null
}

/**
 * 后端健康判定。
 *
 * # 🔴 必须读 backendState，不能只看条数
 *
 * 原来的判据是「backends === 0 → empty（无后端）」，完全没用 `backendState` ——
 * 而后端那几个状态装的正是**为什么是 0**：
 *
 *   unresolved   上游某一跳拉失败了，有没有后端我们不知道
 *   unsupported  有 target，但不是 targetPool/backendService（FortiGate 转发规则、
 *                Gateway API 的 target proxy）→ 流量由那个 target 承载，**不是没后端**
 *   k8s          VIP 对上了 K8s Service（GKE 的后端是 Pod/NEG，实例组里看不到）
 *   lost         采集时追溯到了、读出来没有 → 数据丢了
 *   none         target 为空，这才是真的一个后端都没有
 *
 * 只看条数的后果：生产 49 条里 37 条被判成"确认无后端"（33 条 fgt-* + 4 条 gkegw1-*），
 * 工具栏红字「37 个确认无后端」，而**真正有问题的是 0 条**（OPSCMDB-031 P0-8）。
 *
 * ⚠️ 判据线索本项目反复出现：**集中的异常分布先怀疑判据**。
 * 37 条"故障"里 33 条同名前缀、同一个 target，这种分布不可能是真实故障。
 */
export type LBHealthState = 'stale' | 'unknown' | 'viaTarget' | 'k8s' | 'lost' | 'empty' | 'ok'

export function lbHealth(l: LB): LBHealthState {
  if (l.stale) return 'stale'
  // 状态优先于条数：条数为 0 有五种完全不同的原因
  switch (l.backendState) {
    case 'unresolved':
      return 'unknown'
    case 'unsupported':
      return 'viaTarget'
    case 'k8s':
      return 'k8s'
    case 'lost':
      return 'lost'
  }
  if (l.backends === null) return 'unknown'
  if (l.backends === 0) return 'empty'
  return 'ok'
}

/**
 * health 是**封闭枚举**（由我们计算出来，不是外部自由值）。
 *
 * 用字面量联合而不是 string：传一个不在其中的值，编译期就报错。
 * 写成 string 的话，后端会静默忽略未知值 —— 筛选看起来生效了，其实没筛。
 */
export type LBHealth = 'all' | 'stale' | 'unknown' | 'viaTarget' | 'k8s' | 'lost' | 'empty' | 'ok'

export interface LBListParams {
  page: number
  size: number
  q?: string
  scheme?: string
  health?: LBHealth
  [k: string]: string | number | undefined
}

export interface LBListResult {
  items: LB[]
  total: number
  /** 分面缺失（后端那条统计失败）与计数为 0 是两回事，前端不显示计数而不是显示 0 */
  facets: Record<string, Record<string, number> | undefined>
}

interface RawLB {
  id?: number
  name?: string
  project?: string
  region?: string
  scheme?: string
  vip?: string
  port_range?: string
  protocol?: string
  target?: string
  k8s_service?: string
  provider?: string
  backends?: number | null
  backend_state?: string
  stale?: boolean
  synced_at?: string
}

function toLB(r: RawLB): LB {
  return {
    id: r.id ?? 0,
    name: r.name ?? '',
    project: r.project ?? '',
    region: r.region ?? '',
    scheme: r.scheme ?? '',
    vip: r.vip ?? '',
    portRange: r.port_range ?? '',
    protocol: r.protocol ?? '',
    target: r.target ?? '',
    k8sService: r.k8s_service ?? '',
    provider: r.provider ?? '',
    // `?? 0` 在这里会把"没采过"说成"无后端"，正是生产上那次误报的成因
    backends: r.backends === undefined || r.backends === null ? null : r.backends,
    backendState: r.backend_state ?? '',
    stale: r.stale === true,
    syncedAt: r.synced_at ? r.synced_at : null,
  }
}

export function useLBs(params: LBListParams) {
  return useQuery({
    queryKey: queryKeys.lbs.list(params),
    queryFn: async (): Promise<LBListResult> => {
      const { data, error } = await api.GET('/cloud-lb-list', { params: { query: params } })
      if (error) throw error
      return {
        items: ((data?.items ?? []) as RawLB[]).map(toLB),
        total: data?.total ?? 0,
        facets: (data?.facets ?? {}) as Record<string, Record<string, number> | undefined>,
      }
    },
    staleTime: 30_000,
    retry: shouldRetry,
  })
}
