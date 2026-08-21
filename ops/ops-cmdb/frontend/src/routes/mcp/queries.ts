import { shouldRetry } from '@ops/api'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { apiAction, apiGet } from '../../lib/fetchJson.js'

/** MCP 服务端状态。工具数分「当前可用」和「产品总数」两个数。 */
export interface McpInfo {
  enabled: boolean
  endpoint: string
  transport: string
  /** 当前授权下可用的工具数 */
  /**
   * ⚠️ 字段名是 **`tools_licensed`**（授权档次的上限），后端在修 OPSCMDB-008 时
   * 从 `tools` 改过来的 —— 前端没跟，于是「当前可用 __ 个」那里一直是空的。
   *
   * 它不是"某条令牌实际能用的数量"：每条令牌还要再过一层角色过滤。
   */
  tools_licensed: number
  /** 产品里一共有多少个 */
  tools_total: number
  /** 是否包含全量工具（企业版能力） */
  full_licensed: boolean
}

export interface McpToken {
  /** 这条令牌按其角色**实际**能看到几个工具 */
  tools?: number
  id: number
  name: string
  /** 令牌前 8 位，用来认出是哪一条。完整值只在创建时出现一次 */
  hint: string
  role_code: string
  enabled: boolean
  /** 不受权限约束（升级前遗留的令牌）。界面上必须显眼标出 */
  unrestricted: boolean
  created_by: string
  created_at: string
  /**
   * null = 从没被使用过。
   *
   * ⚠️ 不能压成"很久以前"：「建了没人用」和「用过但很久没用」
   * 要采取的动作不一样——前者是配错了没接上，后者是可以回收。
   */
  last_used_at: string | null
  last_used_ip: string
  /**
   * null = **永久有效**。
   *
   * ⚠️ 界面必须把"永久"明说出来，不能只留空白 —— 空白会被读成
   * "这一项没填"，而它的实际含义是"这个能读全库的凭据永远不会失效"
   * （OPSCMDB-031 P1-70）。
   */
  expires_at: string | null
  /** 已过期。调用会被后端拒（不只是界面上标一下） */
  expired: boolean
  /** 30 天内到期。留出换发时间，而不是等 AI 全线 401 才发现 */
  expiring_soon: boolean
}

export function useMcpInfo() {
  return useQuery({
    queryKey: ['mcp-info'],
    queryFn: () => apiGet<McpInfo>('/api/mcp/info'),
    staleTime: 30_000,
    retry: shouldRetry,
  })
}

export function useMcpTokens() {
  return useQuery({
    queryKey: ['mcp-tokens'],
    queryFn: () => apiGet<{ items: McpToken[] }>('/api/mcp/tokens'),
    staleTime: 15_000,
    retry: shouldRetry,
  })
}

function useDone() {
  const qc = useQueryClient()
  return () => void qc.invalidateQueries({ queryKey: ['mcp-tokens'] })
}

/** 新建令牌。返回里带明文 token —— **这是它唯一一次出现**。 */
export function useCreateToken() {
  const done = useDone()
  return useMutation({
    mutationFn: (body: { name: string; role_code: string; expires_at?: string }) =>
      apiAction<{ ok?: boolean; id: number; token: string }>('/api/mcp/tokens', 'POST', body),
    onSuccess: done,
  })
}

export function useUpdateToken() {
  const done = useDone()
  return useMutation({
    mutationFn: ({
      id,
      ...body
    }: { id: number; enabled?: boolean; role_code?: string; expires_at?: string }) =>
      apiAction(`/api/mcp/tokens/${id}`, 'PUT', body),
    onSuccess: done,
  })
}

export function useDeleteToken() {
  const done = useDone()
  return useMutation({
    mutationFn: (id: number) => apiAction(`/api/mcp/tokens/${id}`, 'DELETE'),
    onSuccess: done,
  })
}
