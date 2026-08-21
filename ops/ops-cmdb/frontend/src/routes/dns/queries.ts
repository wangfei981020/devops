import { shouldRetry } from '@ops/api'
import { useQuery } from '@tanstack/react-query'
import { apiGet } from '../../lib/fetchJson.js'

/**
 * DNS 解析：**合并两个来源**（Cloudflare 与 GCP Cloud DNS）。
 *
 * # 为什么必须合并
 *
 * 同一个域名可能两边都配了记录，但**只有 NS 指向的那一边才生效**。
 * 做成两个入口的话，人一定会在错的那边改半天，改完发现"没生效"，
 * 然后怀疑是缓存、是 TTL、是 CDN ——唯独想不到自己改错了地方。
 *
 * 所以这一页把两边并排放，并且默认带一列「另一边也有」。
 * 后端的 dns-consistency 接口本来就在算这件事，界面上不该反而看不到。
 */

/** Cloudflare 的解析记录。 */
export interface CdnRecord {
  zone?: string
  name?: string
  type?: string
  content?: string
  /** 橙云：经 CDN 代理。它决定了这条记录解析出来是源站 IP 还是 CF 的 IP */
  proxied?: boolean
  ttl?: number
}

/** GCP Cloud DNS。 */
export interface CloudDnsResult {
  zones: { name?: string; dns_name?: string; visibility?: string; record_count?: number }[]
  /**
   * ⚠️ 解析目标的字段名是 **`targets`**，不是 `rrdatas`（那是库里的列名）。
   *
   * 读错名字的后果不是报错，是 `?? []` 把它兜成空数组 ——
   * 「解析目标」整列空白，而这一页存在的理由就是"看两边各解析到哪"。
   * 横幅还照常提示"2 个域名两边配了不同的解析"，你却看不到 GCP 侧是什么，
   * 无从判断该改哪边（OPSCMDB-013）。
   */
  records: { zone?: string; name?: string; type?: string; ttl?: number; targets?: string[] }[]
  record_count: number
  /** 后端给的"为什么是空的"。⚠️ 一定要显示，别自己编一句 */
  empty_hint?: string
}

/**
 * 两边的一致性。
 *
 * ⚠️ `not_comparable` 非空时，**「0 个冲突」不成立**——
 * 有一方根本没数据，比对本身没发生。把它当成"两边一致"是这一页
 * 最容易犯、也最贵的错：它会让人放心地不去查。
 */
export interface DnsConsistency {
  conflicts: {
    fqdn?: string
    type?: string
    /** 各方在这条记录上的目标，键是 gcp / cloudflare / godaddy */
    sides?: Record<string, string>
    issue?: string
    action?: string
    // key 版本给界面翻译，上面两个中文原句留给 MCP / 直接调 API 的人
    issue_key?: string
    issue_params?: Record<string, unknown>
    action_key?: string
    action_params?: Record<string, unknown>
  }[]
  conflict_count: number
  gcp_fqdn_count: number
  cloudflare_fqdn_count: number
  /** GoDaddy 侧。⚠️ 原来这一方**完全不在比对范围内**（OPSCMDB-031 P0-10） */
  godaddy_fqdn_count: number
  /**
   * 「配了但不生效」——记录所在的那一方**不是** NS 指向的那一方。
   *
   * 🔴 这是 P0-10 要答的问题：CF 上看得到、CMDB 上也看得到，
   * 于是认定配置没问题，而真正生效的那份在别处。
   */
  ineffective: {
    fqdn?: string
    configured_in?: string
    authoritative?: string
    ns?: string[]
    issue?: string
    action?: string
    issue_key?: string
    issue_params?: Record<string, unknown>
    action_key?: string
    action_params?: Record<string, unknown>
  }[]
  ineffective_count: number
  /** NS 同时指向多方：谁生效取决于递归解析器问到了哪台，本身就是要修的配置 */
  split_ns: { domain?: string; ns?: string[]; issue?: string; issue_key?: string }[]
  /**
   * 有多少域名没采到 NS、判不出托管方。
   *
   * ⚠️ >0 时 `ineffective` 一定是**不完整**的，界面必须说出来 ——
   * 否则人会把它反过来读成「其余的都生效」。
   */
  unknown_ns_domains: number
  /**
   * 每个域名的托管方，键是主域名。用来逐行标「这条生不生效」。
   *
   * ⚠️ **判不出的域名不在这张表里**。查不到 = 判不出 = 三态里的 null，
   * 绝不能兜成"生效"。
   */
  authority: Record<string, { provider: string; label: string; ns?: string[] }>
  not_comparable?: string
  /** unknown_ns_domains>0 时后端给的说明 */
  authority_incomplete?: string
}

export function useCdnRecords(params: { zone?: string; type?: string; q?: string }) {
  const qs = new URLSearchParams(
    Object.entries(params)
      .filter(([, v]) => v && v !== 'all')
      .map(([k, v]) => [k, String(v)]),
  ).toString()
  return useQuery({
    queryKey: ['cdn-dns', qs],
    queryFn: () => apiGet<CdnRecord[]>(`/api/cdn/dns-records${qs ? `?${qs}` : ''}`),
    staleTime: 60_000,
    retry: shouldRetry,
  })
}

export function useCloudDns(params: { q?: string }) {
  const qs = new URLSearchParams(
    Object.entries(params)
      .filter(([, v]) => v)
      .map(([k, v]) => [k, String(v)]),
  ).toString()
  return useQuery({
    queryKey: ['cloud-dns', qs],
    queryFn: () => apiGet<CloudDnsResult>(`/api/cloud-dns${qs ? `?${qs}` : ''}`),
    staleTime: 60_000,
    retry: shouldRetry,
  })
}

export function useDnsConsistency() {
  return useQuery({
    queryKey: ['dns-consistency'],
    queryFn: () => apiGet<DnsConsistency>('/api/dns-consistency'),
    staleTime: 60_000,
    retry: shouldRetry,
  })
}

/**
 * 注册商（GoDaddy）侧的解析记录。
 *
 * 🔴 这一方原来**完全不在这一页上**：页面只有 Cloudflare 和 GCP 两个来源，
 * 而 62 个域名全注册在 GoDaddy，其中相当一部分 NS 就指向 GoDaddy ——
 * 也就是说真正生效的那份解析，页面上一条都看不到（OPSCMDB-031 P0-10）。
 */
export interface RegistrarDnsRecord {
  domain: string
  fqdn: string
  name: string
  type: string
  data: string
  ttl: number
  protected: boolean
  /**
   * 这一份解析生不生效（按域名的 NS 判）。
   *
   * ⚠️ 三态：true=生效 / false=NS 指向别处，改了没用 / null=没采到 NS，判不出。
   * **不能** `?? true` —— 那会把"判不出"变成"生效"，是最坏的方向。
   */
  effective: boolean | null
  effective_note?: string
}

export function useRegistrarDns(params: { q?: string; type?: string }) {
  const qs = new URLSearchParams(
    Object.entries(params)
      .filter(([, v]) => v && v !== 'all')
      .map(([k, v]) => [k, String(v)]),
  ).toString()
  return useQuery({
    queryKey: ['registrar-dns', qs],
    queryFn: () =>
      apiGet<{ items: RegistrarDnsRecord[]; total: number }>(
        `/api/registrar/dns-records${qs ? `?${qs}` : ''}`,
      ),
    staleTime: 60_000,
    retry: shouldRetry,
  })
}
