import { queryKeys, shouldRetry } from '@ops/api'
import { useQuery } from '@tanstack/react-query'
import { api } from '../../lib/api.js'

export interface Domain {
  ciId: number
  name: string
  registrar: string
  dnsProvider: string
  /** ⚠️ null = 注册到期日读不出来，不是"还有 0 天"。它可能下周就被释放 */
  daysLeft: number | null
  expiryAt: string
  /** 证书到期与域名注册到期是**两件事**，处理的人和动作都不同 */
  certDaysLeft: number | null
  certExpiryAt: string
  certCheckMsg: string
  /** ok / nxdomain / timeout …，原样透传 */
  resolveStatus: string
  /** ⚠️ 被忽略不等于没问题：客观状态一点没变，只是我们决定暂时不管 */
  ignored: boolean
  ignoreReason: string
  /**
   * 主机头台账（domain_records）条数 —— 「这个域名下我们登记了几条业务解析」。
   * ⚠️ 与 dnsRecords 是**两张表**，不能互相冒充。
   */
  records: number
  /**
   * 注册商侧真实解析记录（dns_records）条数。
   * 🔴 「DNS 解析」页按域名视图那个计数必须用这个：
   *	用 records 的话，点开的弹窗（管的是注册商解析）会显示"没有解析记录"，
   *	而行上写着「2 条记录」—— 实测 dev-example.com 就是台账 2 条、注册商侧 0 条。
   */
  dnsRecords: number
  syncedAt: string | null
}

/**
 * health 是**封闭枚举**（由我们计算出来，不是外部自由值）。
 *
 * 用字面量联合而不是 string：传一个不在其中的值，编译期就报错。
 * 写成 string 的话，后端会静默忽略未知值 —— 筛选看起来生效了，其实没筛。
 */
export type DomainHealth = 'all' | 'expired' | 'unresolved' | 'unknown' | 'soon' | 'cert_soon' | 'ignored' | 'ok'

export interface DomainListParams {
  page: number
  size: number
  q?: string
  health?: DomainHealth
  [k: string]: string | number | undefined
}

export interface DomainListResult {
  items: Domain[]
  total: number
  /** 分面缺失（后端那条统计失败）与计数为 0 是两回事，前端不显示计数而不是显示 0 */
  facets: Record<string, Record<string, number> | undefined>
}

interface RawDomain {
  ci_id?: number
  name?: string
  registrar?: string
  dns_provider?: string
  days_left?: number | null
  expiry_at?: string
  cert_days_left?: number | null
  cert_expiry_at?: string
  cert_check_msg?: string
  resolve_status?: string
  ignored?: boolean
  ignore_reason?: string
  records?: number
  dns_records?: number
  synced_at?: string
}

function toDomain(r: RawDomain): Domain {
  return {
    ciId: r.ci_id ?? 0,
    name: r.name ?? '',
    registrar: r.registrar ?? '',
    dnsProvider: r.dns_provider ?? '',
    daysLeft: r.days_left === undefined || r.days_left === null ? null : r.days_left,
    expiryAt: r.expiry_at ?? '',
    certDaysLeft:
      r.cert_days_left === undefined || r.cert_days_left === null ? null : r.cert_days_left,
    certExpiryAt: r.cert_expiry_at ?? '',
    certCheckMsg: r.cert_check_msg ?? '',
    resolveStatus: r.resolve_status ?? '',
    ignored: r.ignored === true,
    ignoreReason: r.ignore_reason ?? '',
    records: r.records ?? 0,
    dnsRecords: r.dns_records ?? 0,
    syncedAt: r.synced_at ? r.synced_at : null,
  }
}

export function useDomains(params: DomainListParams) {
  return useQuery({
    queryKey: queryKeys.domains.list(params),
    queryFn: async (): Promise<DomainListResult> => {
      const { data, error } = await api.GET('/domain-list', { params: { query: params } })
      if (error) throw error
      return {
        items: ((data?.items ?? []) as RawDomain[]).map(toDomain),
        total: data?.total ?? 0,
        facets: (data?.facets ?? {}) as Record<string, Record<string, number> | undefined>,
      }
    },
    staleTime: 30_000,
    retry: shouldRetry,
  })
}

// ─────────────────────────────────────────────────────────────────
// 写操作
//
// ⚠️ 这一整块以前是空的：后端 14 个域名写接口全在、权限码配了、审计也登记了，
// 前端一处没调。从任何单侧看都正常 —— 后端有路由有测试，前端页面能开不报错，
// 只有把两侧对起来才看得见（check-write-coverage.mjs 就是干这个的）。
//
// 这不是"功能没做"，是"做完了没接线"。
// ─────────────────────────────────────────────────────────────────

import { useMutation, useQueryClient } from '@tanstack/react-query'
import { apiAction, apiGet } from '../../lib/fetchJson.js'

export interface DomainInput {
  name: string
  project?: string
  env?: string
  module?: string
  owner?: string
  status?: string
  registrar_id?: number | null
  dns_provider?: string
  /** "2006-01-02" 或空。⚠️ 空 = 不知道，不是"今天到期" */
  expiry_at?: string
}

type Res = { ok?: boolean; error?: string; msg?: string }

/** 列表失效。写完必须刷新，否则用户看到的还是改之前的那份 */
function useInvalidate() {
  const qc = useQueryClient()
  return () => void qc.invalidateQueries({ queryKey: ['domain-list'] })
}

export function useCreateDomain() {
  const done = useInvalidate()
  return useMutation({
    mutationFn: (v: DomainInput) => apiAction<Res>('/api/domains', 'POST', v),
    onSuccess: done,
  })
}

export function useUpdateDomain() {
  const done = useInvalidate()
  return useMutation({
    mutationFn: ({ ciId, ...v }: DomainInput & { ciId: number }) =>
      apiAction<Res>(`/api/domains/${ciId}`, 'PUT', v),
    onSuccess: done,
  })
}

export function useDeleteDomain() {
  const done = useInvalidate()
  return useMutation({
    mutationFn: (ciId: number) => apiAction<Res>(`/api/domains/${ciId}`, 'DELETE'),
    onSuccess: done,
  })
}

/** 从注册商刷新单个域名的到期日等信息 */
export function useRefreshDomain() {
  const done = useInvalidate()
  return useMutation({
    mutationFn: (ciId: number) => apiAction<Res>(`/api/domains/${ciId}/refresh`, 'POST'),
    onSuccess: done,
  })
}

/** 刷新全部。⚠️ 会逐个打注册商 API，域名多时慢，按钮要有 loading */
export function useRefreshAllDomains() {
  const done = useInvalidate()
  return useMutation({
    mutationFn: () => apiAction<Res>('/api/domains/refresh-all', 'POST'),
    onSuccess: done,
  })
}

/** 从注册商同步这个域名的解析记录 */
export function useSyncDomainRecords() {
  const done = useInvalidate()
  return useMutation({
    mutationFn: (ciId: number) => apiAction<Res>(`/api/domains/${ciId}/sync-records`, 'POST'),
    onSuccess: done,
  })
}

/**
 * 开/关自动续费。
 *
 * ⚠️ 这是**写回注册商**的操作，不是改本地标记 —— 关掉之后域名到期真的不会续。
 * 界面上必须二次确认，且文案要说清楚"这会改注册商那边的设置"。
 */
export function useSetAutoRenew() {
  const done = useInvalidate()
  return useMutation({
    mutationFn: ({ ciId, enabled }: { ciId: number; enabled: boolean }) =>
      apiAction<Res>(`/api/domains/${ciId}/auto-renew`, 'POST', { enabled }),
    onSuccess: done,
  })
}

/** 检测该域名下所有解析记录的证书 */
export function useCheckAllCerts() {
  const done = useInvalidate()
  return useMutation({
    mutationFn: (ciId: number) => apiAction<Res>(`/api/domains/${ciId}/check-all-certs`, 'POST'),
    onSuccess: done,
  })
}

/**
 * 单个域名的厂商侧详情：到期日、自动续费开关、续费价。
 *
 * ⚠️ `detail_ok:false` 是**降级**不是失败：价格可能还在，但到期/自动续费读不出来。
 * 界面必须把这两半分开渲染 —— 把读不到的那半显示成"未开启"，
 * 会让人以为自动续费关着而去手工续费，而它其实开着。
 */
export interface GodaddyDetail {
  domain: string
  env: string
  dry_run: boolean
  detail_ok: boolean
  renew_auto?: boolean
  privacy?: boolean
  status?: string
  expires?: string
  price_per_year?: number
  currency?: string
}

export function useGodaddyDetail(ciId: number, enabled: boolean) {
  return useQuery({
    queryKey: ['godaddy-detail', ciId],
    queryFn: () => apiGet<GodaddyDetail>(`/api/domains/${ciId}/godaddy-detail`),
    enabled,
    // 打的是厂商接口，别自动重取
    staleTime: 60_000,
    refetchOnWindowFocus: false,
    retry: false,
  })
}

/** 单个域名续费的返回。字段语义与批量续费一致，见 renew.ts 的注释。 */
export interface RenewOneResult {
  ok?: boolean
  dry_run?: boolean
  env?: string
  order_id?: string
  expiry_before?: string
  expiry_after?: string
  ledger_saved?: boolean
  msg?: string
  /** 台账没写上（钱已扣，记录缺失）—— 要人工补录 */
  warning?: string
  /** ⚠️ 疑似重复扣费。这是**成功路径上的警告**，必须单独醒目展示 */
  overpay_note?: string
}

/**
 * 单个域名续费。⚠️ 真实扣费，非幂等。
 *
 * 失败**绝不自动重试**：失败不等于没扣费，重试可能就是第二次扣款。
 * 重试必须由人在回查到期日之后决定（见 project_cmdb_domain_renew_safety）。
 */
export function useRenewDomain() {
  const done = useInvalidate()
  return useMutation({
    mutationFn: ({ ciId, period }: { ciId: number; period: number }) =>
      apiAction<RenewOneResult>(`/api/domains/${ciId}/renew`, 'POST', { period }),
    retry: false,
    onSuccess: done,
  })
}
