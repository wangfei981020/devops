import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { apiAction, apiGet } from '../../lib/fetchJson.js'

/**
 * 域名下的解析记录（注册商侧，当前是 GoDaddy）。
 *
 * ⚠️ 和 routes/dns 那一页**不是一回事**：那一页看的是 Cloudflare / GCP Cloud DNS
 * 的记录并做一致性比对，只读；这里是注册商自己的 DNS，可写。
 * 一个域名的 NS 指向谁，才决定哪一边的记录真正生效。
 *
 * ⚠️ 这整套写接口在后端一直都在（7 个），前端一个都没接 ——
 * 于是"在 CMDB 里改一条解析"这件事做不了，而页面上看不出任何异常。
 */
export interface DnsRecord {
  id: number
  type: string
  name: string
  data: string
  ttl: number
  /** MX 才有。null = 该类型不适用，不是"优先级 0" */
  priority: number | null
  /** NS / SOA / _acme-challenge：厂商侧或签发流程在用，禁止写回 */
  protected: boolean
  synced_at: string
}

/** 后端支持写回的类型。其余类型只读展示，改要去厂商后台。 */
export const WRITABLE_TYPES = ['A', 'AAAA', 'CNAME', 'TXT', 'MX'] as const

export type WriteRes = {
  ok?: boolean
  error?: string
  msg?: string
  dry_run?: boolean
  env?: string
  /** 批量接口逐行回报：哪一行没做成、为什么。⚠️ 必须展示，否则"部分成功"看着像全成功 */
  errors?: { row?: number; id?: number; detail?: string; msg?: string }[]
  ok_count?: number
  fail_count?: number
}

export function useDnsRecords(ciId: number, enabled: boolean) {
  return useQuery({
    queryKey: ['dns-records', ciId],
    queryFn: () => apiGet<DnsRecord[]>(`/api/domains/${ciId}/dns-records`),
    enabled,
    staleTime: 15_000,
  })
}

function useInvalidate(ciId: number) {
  const qc = useQueryClient()
  return () => {
    void qc.invalidateQueries({ queryKey: ['dns-records', ciId] })
    // 域名列表上有「解析记录数」这一列，写完也得跟着变
    void qc.invalidateQueries({ queryKey: ['domain-list'] })
  }
}

export interface RecordInput {
  type: string
  name: string
  data: string
  ttl: number
  priority?: number | null
}

/**
 * 写回都不重试。
 *
 * DNS 写回是**对外部系统的非幂等调用**：新增失败可能是"已经建好了但响应丢了"，
 * 自动重试一次就是两条一模一样的解析。要重试由人点。
 */
const noRetry = { retry: false as const }

export function useCreateRecord(ciId: number) {
  const done = useInvalidate(ciId)
  return useMutation({
    ...noRetry,
    mutationFn: (v: RecordInput) =>
      apiAction<WriteRes>(`/api/domains/${ciId}/dns-records`, 'POST', v),
    onSuccess: done,
  })
}

export function useBatchCreateRecords(ciId: number) {
  const done = useInvalidate(ciId)
  return useMutation({
    ...noRetry,
    mutationFn: (records: RecordInput[]) =>
      apiAction<WriteRes>(`/api/domains/${ciId}/dns-records/batch`, 'POST', { records }),
    onSuccess: done,
  })
}

export function useUpdateRecord(ciId: number) {
  const done = useInvalidate(ciId)
  return useMutation({
    ...noRetry,
    mutationFn: ({ id, ...v }: { id: number; data: string; ttl: number; priority?: number | null }) =>
      apiAction<WriteRes>(`/api/dns-records/${id}`, 'PUT', v),
    onSuccess: done,
  })
}

export function useDeleteRecord(ciId: number) {
  const done = useInvalidate(ciId)
  return useMutation({
    ...noRetry,
    mutationFn: (id: number) => apiAction<WriteRes>(`/api/dns-records/${id}`, 'DELETE'),
    onSuccess: done,
  })
}

export function useBatchDeleteRecords(ciId: number) {
  const done = useInvalidate(ciId)
  return useMutation({
    ...noRetry,
    mutationFn: (ids: number[]) =>
      apiAction<WriteRes>(`/api/domains/${ciId}/dns-records/batch-delete`, 'POST', { ids }),
    onSuccess: done,
  })
}

export function useBatchUpdateRecords(ciId: number) {
  const done = useInvalidate(ciId)
  return useMutation({
    ...noRetry,
    mutationFn: (records: { id: number; data: string; ttl: number; priority?: number | null }[]) =>
      apiAction<WriteRes>(`/api/domains/${ciId}/dns-records/batch-update`, 'POST', { records }),
    onSuccess: done,
  })
}

/**
 * 解析一行文本成一条记录：`类型 主机名 记录值 [TTL] [优先级]`，空白分隔。
 *
 * ⚠️ 解析不出来的行**必须原样报回去**，不能跳过。
 * 静默跳过是这类批量入口最经典的坑：粘 50 行进去、写成功 48 条，
 * 界面显示"成功"，而那 2 条没人知道去哪了。
 */
export function parseRecordLine(line: string): RecordInput | { error: string } {
  const parts = line.trim().split(/\s+/)
  if (parts.length < 3) return { error: '至少要有 类型 主机名 记录值 三段' }
  const [type, name, data, ttlStr, prioStr] = parts
  const t = (type ?? '').toUpperCase()
  if (!(WRITABLE_TYPES as readonly string[]).includes(t)) {
    return { error: `类型 ${t} 不支持写回（支持 ${WRITABLE_TYPES.join('/')}）` }
  }
  // TTL 缺省交给后端兜底（后端会抬到最小 600），这里不自作主张填一个数
  const ttl = ttlStr ? Number(ttlStr) : 600
  if (Number.isNaN(ttl)) return { error: `TTL "${ttlStr}" 不是数字` }
  const priority = prioStr ? Number(prioStr) : undefined
  if (priority !== undefined && Number.isNaN(priority)) {
    return { error: `优先级 "${prioStr}" 不是数字` }
  }
  return { type: t, name: name ?? '@', data: data ?? '', ttl, priority }
}
