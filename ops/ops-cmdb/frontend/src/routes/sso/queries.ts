import { shouldRetry } from '@ops/api'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { apiAction, apiGet } from '../../lib/fetchJson.js'

/**
 * SSO（OIDC）接入配置。
 *
 * ⚠️ CMDB 在这里是 **RP（依赖方）**：它去连别人的 IdP。
 * 不要把这页读成"把 CMDB 变成一个 IdP"。
 */
export interface IdPConfig {
  enabled: boolean
  display_name: string
  issuer: string
  client_id: string
  scopes: string
  username_claim: string
  name_claim: string
  jit_enabled: boolean
  jit_role_code: string
  /** 允许 issuer 指向内网 / 用 http。自建身份源需要它，默认关 */
  allow_private: boolean
  /** 密钥只有"配没配"，值任何接口都不回传 */
  has_secret: boolean
  /** 后端按当前访问地址算出来的，给客户去 IdP 那边登记 */
  redirect_uri: string
  updated_at?: string
  /** 最近一次登录失败的短码。空 = 没有待处理的失败 */
  last_error?: string
  last_error_at?: string
  last_error_user?: string
  /** 身份源实际返回的 claim 名字。配「用户名取自」时照着填 */
  last_claims?: string[]
}

export function useIdPConfig() {
  return useQuery({
    queryKey: ['idp-config'],
    queryFn: () => apiGet<IdPConfig>('/api/idp-config'),
    staleTime: 30_000,
    retry: shouldRetry,
  })
}

export function useSaveIdP() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (body: Partial<IdPConfig> & { client_secret?: string }) =>
      apiAction('/api/idp-config', 'PUT', body),
    onSuccess: () => void qc.invalidateQueries({ queryKey: ['idp-config'] }),
  })
}

export interface Endpoints {
  issuer: string
  authorization_endpoint: string
  token_endpoint: string
  jwks_uri: string
}

/**
 * 探测 IdP 的端点。
 *
 * 在保存之前就能知道地址对不对 —— 否则填错的代价是"点了登录，
 * 转到一个报错页，再转回来"，而那个报错页是对方的，说的是他们的话。
 */
export function useDiscover() {
  return useMutation({
    mutationFn: (v: { issuer: string; allow_private: boolean }) =>
      apiAction<Endpoints & { ok?: boolean }>('/api/idp-config/discover', 'POST', v),
  })
}
