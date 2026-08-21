import { shouldRetry } from '@ops/api'
import { useQuery } from '@tanstack/react-query'
import { apiGet } from '../../lib/fetchJson.js'

/**
 * CDN 的四个分析视角：规则 / 规则体检 / 边缘证书 / 域名走向检查。
 *
 * 老 CMDB 有这四个，新版只列了站点（OPSCMDB-021）——
 * 于是「站点在」看得到，「规则对不对、证书快过期没、解析有没有绕开 CDN」查不到。
 *
 * ⚠️ 这四个读的都是**快照**（上次同步时的 Cloudflare 配置），不是实时。
 * 改完 CF 配置必须先同步再看，否则看到的是旧状态 —— 这个坑在老版上栽过。
 */

export interface CdnRule {
  zone_name?: string
  rule_id?: string
  name?: string
  phase?: string
  kind?: string
  priority?: number
  status?: string
  expression?: string
  actions?: string
  last_updated?: string
}

export function useCdnRules() {
  return useQuery({
    queryKey: ['cdn-rules'],
    queryFn: () => apiGet<{ rules?: CdnRule[] }>('/api/cdn/rules'),
    staleTime: 60_000,
    retry: shouldRetry,
  })
}

export interface CdnCert {
  zone_name?: string
  pack_id?: string
  hosts?: string
  issuer?: string
  status?: string
  expires_on?: string
  days_left?: number
  issue?: string
}

export function useCdnCertificates() {
  return useQuery({
    queryKey: ['cdn-certificates'],
    queryFn: () => apiGet<{ certificates?: CdnCert[] }>('/api/cdn/certificates'),
    staleTime: 60_000,
    retry: shouldRetry,
  })
}

export interface Finding {
  severity?: string
  zone?: string
  fqdn?: string
  type?: string
  content?: string
  via_cdn?: boolean
  issue?: string
  action?: string
}

/** 规则体检：找缺失的强制 HTTPS、互相覆盖的规则等。结论与建议都由后端给。 */
export function useCdnRuleAnalysis() {
  return useQuery({
    queryKey: ['cdn-rule-analysis'],
    queryFn: () => apiGet<{ findings?: Finding[] }>('/api/cdn/rule-analysis'),
    staleTime: 60_000,
    retry: shouldRetry,
  })
}

/**
 * 域名走向检查：这条解析到底有没有经过 CDN。
 *
 * ⚠️ `via_cdn=false` 是**真问题**：以为挂了 CDN、实际直连源站，
 * 那么 WAF、限流、缓存全都没生效，而界面上任何地方都看不出来。
 */
/**
 * ⚠️ 没有按域名筛的参数。
 *
 *	这里原来收一个 `domain` 并拼进 URL —— 而 handler 读的是 `zone`（按 zone 筛），
 *	`domain` 根本不读。唯一的调用点传的又是空串，所以那条分支从没跑过：
 *	一个传了也不生效、但看起来像能用的参数（OPSCMDB-049）。
 *	真要按 zone 筛，加的是 zone 参数，不是复活这个。
 */
export function useCdnDomainCheck() {
  return useQuery({
    queryKey: ['cdn-domain-check'],
    queryFn: () => apiGet<{ items?: Finding[] }>('/api/cdn/domain-check'),
    staleTime: 60_000,
    retry: shouldRetry,
  })
}
