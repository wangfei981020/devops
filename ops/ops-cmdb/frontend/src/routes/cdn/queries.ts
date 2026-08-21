import { shouldRetry } from '@ops/api'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { apiAction, apiGet } from '../../lib/fetchJson.js'

/** Cloudflare 站点。字段用 optional：上游少给一个不该让整页崩。 */
export interface CdnZone {
  name?: string
  status?: string
  plan?: string
  /** 库里存的是逗号串，**接口层已拆成数组**（handlers/cdn.go:439 有说明）。
   *  ⚠️ 改这个类型前先看接口实际返回：两边不一致会在渲染期崩掉整页（OPSCMDB-012）。 */
  name_servers?: string[]
  /**
   * DNS 记录数。
   *
   * ⚠️ 字段名是 `dns_count`，**不是 dns_record_count**。
   * 声明错的时候这一列永远显示「未知」——好在页面用的是
   * `=== undefined` 三态判断而不是 `?? 0`，所以只是缺信息，
   * 没变成"这个站点一条 DNS 记录都没有"那种假结论。
   */
  dns_count?: number
  /** flexible = 回源明文。这是个真实风险，不能和其他模式一样淡 */
  ssl_mode?: string
  /** 暂停的 zone 配置不生效。和 active 长一样的话，人会以为规则在起作用 */
  paused?: boolean
  /**
   * 后端已经把风险判定好了（flexible 回源明文 / zone 非 active），
   * 而且**刻意做成数组**——一个站点可以同时命中多条，
   * 早期版本用单个字段互相覆盖，既 paused 又 flexible 的只显示一条。
   * ⚠️ 前端以前完全没接这两个字段，等于判定白做。
   */
  risk?: string
  risks?: string[]
  account?: string
  zone_id?: string
  synced_at?: string
}

/** CDN 厂商（cdns 表）。账号要挂在某个厂商下。 */
export interface CdnVendor {
  id: number
  name: string
  sort_order?: number
}

/**
 * CDN 账号。
 *
 * ⚠️ 租户级。token 只有"配没配"，接口从不回传。
 */
export interface CdnAccount {
  id: number
  cdn_id: number
  /** 厂商名，后端 JOIN 出来的 */
  cdn: string
  name: string
  account_tag: string
  enabled: boolean
  has_credential: boolean
  /** 空 = 从没同步过。⚠️ 不能压成"很久以前" */
  last_sync_at: string
  /** 上次同步的结果。失败时这里是唯一的线索 */
  last_result: string
}

export function useCdnZones() {
  return useQuery({
    queryKey: ['cdn-zones'],
    queryFn: () => apiGet<CdnZone[]>('/api/cdn/zones'),
    staleTime: 60_000,
    retry: shouldRetry,
  })
}

export function useCdnAccounts() {
  return useQuery({
    queryKey: ['cdn-accounts'],
    queryFn: () => apiGet<CdnAccount[]>('/api/cdn/accounts'),
    staleTime: 30_000,
    retry: shouldRetry,
  })
}

export function useCdnVendors() {
  return useQuery({
    queryKey: ['cdn-vendors'],
    queryFn: () => apiGet<CdnVendor[]>('/api/cdns'),
    staleTime: 5 * 60_000,
    retry: shouldRetry,
  })
}

function useDone() {
  const qc = useQueryClient()
  return () => {
    void qc.invalidateQueries({ queryKey: ['cdn-accounts'] })
    void qc.invalidateQueries({ queryKey: ['cdn-zones'] })
  }
}

export function useSaveCdnAccount() {
  const done = useDone()
  return useMutation({
    mutationFn: (b: {
      id?: number
      cdn_id: number
      name: string
      /** 留空 = 保持原 token 不变 */
      token: string
      account_tag: string
      enabled: boolean
    }) => apiAction('/api/cdn/accounts', 'POST', b),
    onSuccess: done,
  })
}

export function useDeleteCdnAccount() {
  const done = useDone()
  return useMutation({
    mutationFn: (id: number) => apiAction(`/api/cdn/accounts/${id}`, 'DELETE'),
    onSuccess: done,
  })
}

/** 新建 CDN 厂商。没有厂商就挂不了账号，所以这一步要能就地做。 */
/**
 * 改名 / 删除 CDN 厂商。
 *
 * ⚠️ 删除前后端会挡：还有账号挂在这个厂商下时删不掉。
 * 那句错误里的数量是用户接下来的工作量，不要改写成"删除失败"。
 */
export function useUpdateVendor() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: ({ id, name }: { id: number; name: string }) =>
      apiAction(`/api/cdns/${id}`, 'PUT', { name }),
    onSuccess: () => void qc.invalidateQueries({ queryKey: ['cdn-vendors'] }),
  })
}

export function useDeleteVendor() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (id: number) => apiAction(`/api/cdns/${id}`, 'DELETE'),
    onSuccess: () => void qc.invalidateQueries({ queryKey: ['cdn-vendors'] }),
  })
}

export function useCreateVendor() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (b: { name: string }) => apiAction('/api/cdns', 'POST', b),
    onSuccess: () => void qc.invalidateQueries({ queryKey: ['cdn-vendors'] }),
  })
}

/**
 * 测连通 / 立即同步。
 *
 * ⚠️ 这两个入口以前没有，而这一页恰恰有一个「从没同步过」的状态 ——
 * 看得见"没同步"，却没有任何办法让它同步一次。
 *
 * ⚠️ 两个接口都是 **HTTP 200 + `{ok:false, error}`** 的老约定（apiAction 会替我们
 * 把 ok:false 转成异常）。只看 HTTP 状态的话，"没配凭据"会显示成绿色的连通 ——
 * 实测在别处撞到过：三个没有 kubeconfig 的集群，测连通全绿。
 */
export function useVerifyCdnAccount() {
  const qc = useQueryClient()
  return useMutation({
    retry: false,
    mutationFn: (id: number) =>
      apiAction<{ ok?: boolean; msg?: string }>(`/api/cdn/accounts/${id}/verify`, 'POST'),
    onSuccess: () => void qc.invalidateQueries({ queryKey: ['cdn-accounts'] }),
  })
}

export function useSyncCdnAccount() {
  const qc = useQueryClient()
  return useMutation({
    retry: false,
    mutationFn: (id: number) =>
      apiAction<{ ok?: boolean; msg?: string }>(`/api/cdn/accounts/${id}/sync`, 'POST'),
    onSuccess: () => void qc.invalidateQueries(),
  })
}

/**
 * CDN token 权限体检。
 *
 * # ⚠️ 这个接口和本文件里其它接口口径完全不同
 *
 * 其它 `/api/cdn/*` 读的都是**上次同步落库的快照**；这一个**实时打 Cloudflare**。
 *
 * 为什么必须实时：改完 CF 权限后去查快照，看到的仍是旧的
 * 「明细获取失败: 403」，据此会得出「权限没生效」的错误结论 —— 本项目踩过一次。
 * 体检必须绕开快照。
 *
 * ⚠️ 所以它**只在用户主动点时请求**（useMutation 而不是 useQuery）：
 * 不预取、不轮询、不随页面挂载而触发。它直接打到外部服务，
 * 一次全账号体检后端给了 90 秒超时。
 */
export interface CdnProbeCheck {
  name?: string
  /** 对应 CF 控制台权限项的**原文** —— 让人能照着去勾 */
  need?: string
  endpoint?: string
  ok?: boolean
  /** 0 = 请求根本没发出去。⚠️ 与「发出去了但被拒」是两回事 */
  http_code?: number
  verdict?: string
  /** CF 原样返回的错误，不做加工 */
  detail?: string
  /** 不通会导致哪个 MCP 工具没数据 —— 这一条是「所以呢」的答案 */
  impact?: string
}

export interface CdnProbeAccount {
  account_id?: number
  provider?: string
  /** false = 连 client 都没拿到（没配 token / 厂商不支持），此时 probe 为空 */
  ok?: boolean
  error?: string
  probe?: {
    token_id?: string
    token_state?: string
    expires_on?: string
    probed_zone?: string
    probed_at?: string
    summary?: string
    checks?: CdnProbeCheck[]
  }
}

export function useCdnTokenCheck() {
  return useMutation({
    mutationFn: (zone?: string) =>
      apiGet<{ realtime?: boolean; note?: string; accounts?: CdnProbeAccount[]; ok?: boolean; error?: string }>(
        `/api/cdn/token-check${zone ? `?zone=${encodeURIComponent(zone)}` : ''}`,
      ),
  })
}

/**
 * CDN 实时取证：逐条请求时序 / 安全事件。
 *
 * # 🔴 这两个接口存在的理由是「跨公网扯皮」
 *
 * 对方说「请求早就发出去了，你们很久才收到」时，双方各执一词，
 * 因为**中间那段没有任何一方看得见**。
 *
 * `datetime_cst`（CF 边缘收到该请求的时刻）就是那个缺失的锚点：
 * 把它与「对方发出的时刻」「我方应用读到的时刻」三者一比，
 * 就能把总延迟切成「到达 CF 之前」和「CF 之后」两段 —— 责任立刻分清。
 *
 * ⚠️ 实时打 CF，不读快照。只在用户主动查时请求。
 */
export interface CdnTrafficResult {
  ok?: boolean
  error?: string
  hint?: string
  realtime?: boolean
  zone?: string
  window_cst?: string
  total?: number
  /** 字段是动态的（可自定义 fields），所以是宽松的 map */
  requests?: Record<string, unknown>[]
  /** 怎么读这些时刻 —— 这一段是整个功能的价值所在，必须显示 */
  how_to_read?: string
  /** 🔴 空 ≠ 没有请求：该数据集按套餐有采样与保留期限制 */
  empty_meaning?: string
  truncated?: string
}

export interface CdnSecurityResult {
  ok?: boolean
  error?: string
  window_cst?: string
  total?: number
  by_action?: { name?: string; count?: number }[]
  by_rule?: { name?: string; count?: number }[]
  events?: Record<string, unknown>[]
  empty_meaning?: string
  truncated?: string
}

function qs(params: Record<string, string | number | undefined>) {
  const u = new URLSearchParams()
  for (const [k, v] of Object.entries(params)) {
    if (v !== undefined && v !== '' && v !== 'all') u.set(k, String(v))
  }
  const s = u.toString()
  return s ? `?${s}` : ''
}

export function useCdnTraffic() {
  return useMutation({
    mutationFn: (p: { zone: string; host?: string; path?: string; client_ip?: string; minutes?: number }) =>
      apiGet<CdnTrafficResult>(`/api/cdn/traffic${qs(p)}`),
  })
}

export function useCdnSecurityEvents() {
  return useMutation({
    mutationFn: (p: { zone: string; host?: string; minutes?: number }) =>
      apiGet<CdnSecurityResult>(`/api/cdn/security-events${qs(p)}`),
  })
}
