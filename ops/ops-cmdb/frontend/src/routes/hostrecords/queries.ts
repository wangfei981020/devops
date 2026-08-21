import { shouldRetry } from '@ops/api'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { apiAction, apiGet } from '../../lib/fetchJson.js'

/**
 * 主机头台账。
 *
 * ⚠️ 和「DNS 解析」页是**两套数据**，别混：
 *   DNS 解析  —— Cloudflare / GCP 上此刻的实时解析记录
 *   主机头台账 —— 我们自己的台账：谁负责、属于哪个项目、哪些不用管
 *
 * 台账的价值在归属：没有归属的主机头，出事时找不到人；
 * 而「忽略」是**我们决定暂时不管**，不是"正常"也不是"删除"。
 */
export interface HostRecord {
  id: number
  /** 所属域名的 ci_id。新增记录挂在域名下，编辑时用来回填 */
  domain_ci_id: number
  domain: string
  cdn_id: number | null
  fqdn: string
  host: string
  record_type: string
  cname: string
  origin_ip: string
  /** 手填 origin_ip 为空时的推测值（不落库）。⚠️ 是推测，不能当事实用 */
  auto_origin_ip: string
  project: string
  env: string
  module: string
  life_status: string
  ignored: boolean
  ignore_reason: string
  /** 主域名已失效时的标签（已过户/已移出账号…）。这条记录多半也没意义了 */
  domain_gone: string
  cert_expiry_at: string
  cert_check_msg: string
  stale: boolean
}

/** status: '' = 未忽略的，'ignored' = 只看被忽略的，'all' = 全部 */
export function useHostRecords(status: string) {
  return useQuery({
    queryKey: ['host-records', status],
    queryFn: () => apiGet<HostRecord[]>(`/api/records${status ? `?status=${status}` : ''}`),
    staleTime: 30_000,
    retry: shouldRetry,
  })
}

function useDone() {
  const qc = useQueryClient()
  return () => void qc.invalidateQueries({ queryKey: ['host-records'] })
}

/**
 * 批量设归属。
 *
 * ⚠️ 三个字段都是**指针语义**：不传 = 不动那一项。
 * 传空串才是"清空"。前端要把这两种区分开 —— 一次批量操作里
 * 用户往往只想改项目，不该顺手把环境和模块清掉。
 */
export interface BulkUpdateBody {
  ids: number[]
  project?: string
  env?: string
  module?: string
  /**
   * 🔴 `set_*` 是**布尔开关**，语义是"这一项要不要动"，不能省掉直接传值。
   *
   * 省掉的话「不动」和「清空」就分不开了：源站 IP 和回源 CNAME 传空串
   * 在后端是**有意义的写入**（清掉手填值、回到自动推算），
   * 不是"没填所以跳过"。后端注释里专门交代过这一点。
   *
   * 这三项是换 CDN 或迁源站时最需要批量的（OPSCMDB-039），
   * 旧版 Domains.vue 有，新版只能一条条点进去改。
   */
  set_cdn?: boolean
  cdn_id?: number | null
  set_origin_ip?: boolean
  origin_ip?: string
  set_cname?: boolean
  cname?: string
}

export function useBulkUpdate() {
  const done = useDone()
  return useMutation({
    mutationFn: (v: BulkUpdateBody) => apiAction('/api/records/bulk-update', 'POST', v),
    onSuccess: done,
  })
}

/**
 * 批量忽略 / 取消忽略。
 *
 * ⚠️ 忽略之后：同步跳过它、到期巡检不再报它。所以**必须有理由**，
 * 否则半年后没人知道这条为什么不告警了。
 */
export function useBulkIgnoreRecords() {
  const done = useDone()
  return useMutation({
    mutationFn: (v: { ids: number[]; ignored: boolean; reason: string }) =>
      apiAction('/api/records/bulk-ignore', 'POST', v),
    onSuccess: done,
  })
}

/**
 * 回源规则：回源 CNAME → 源站 IP。
 *
 * 用于在 DNS 查不到时兜底推断源站。`used` 是**有多少条记录在用这条规则**——
 * 没人用的规则留着只会让人以为源站已经登记好了。
 */
export interface OriginRule {
  id: number
  cname: string
  origin_ip: string
  used: number
}

export function useOriginRules() {
  return useQuery({
    queryKey: ['origin-rules'],
    queryFn: () => apiGet<OriginRule[]>('/api/origin-rules'),
    staleTime: 60_000,
    retry: shouldRetry,
  })
}

export function useSaveOriginRule() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (v: { cname: string; origin_ip: string }) =>
      apiAction('/api/origin-rules', 'POST', v),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: ['origin-rules'] })
      void qc.invalidateQueries({ queryKey: ['host-records'] })
    },
  })
}

export function useDeleteOriginRule() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (id: number) => apiAction(`/api/origin-rules/${id}`, 'DELETE'),
    onSuccess: () => void qc.invalidateQueries({ queryKey: ['origin-rules'] }),
  })
}

// ─────────────────────────────────────────────────────────────────
// 单条写操作
//
// ⚠️ 这一块以前是空的：后端 Create / Update / Delete / CheckCert / Ignore
// 五个接口全在，前端只接了两个批量的。结果是台账**只能批量改归属**，
// 单条既不能新增也不能删 —— 而页面上看不出任何异常。
// ─────────────────────────────────────────────────────────────────

/** 与后端 recordIn 一一对应。⚠️ cert_expiry_at 留空 = 不知道，不是"今天到期" */
export interface RecordInput {
  host: string
  record_type: string
  cdn_id?: number | null
  cname?: string
  origin_ip?: string
  cert_expiry_at?: string
  project?: string
  env?: string
  module?: string
  life_status?: string
}

type Res = { ok?: boolean; error?: string; msg?: string }

/** 新增一条主机头。挂在某个域名下 —— 后端会校验该域名属于本租户 */
export function useCreateRecord() {
  const done = useDone()
  return useMutation({
    retry: false,
    mutationFn: ({ ciId, ...v }: RecordInput & { ciId: number }) =>
      apiAction<Res>(`/api/domains/${ciId}/records`, 'POST', v),
    onSuccess: done,
  })
}

export function useUpdateRecord() {
  const done = useDone()
  return useMutation({
    retry: false,
    mutationFn: ({ id, ...v }: RecordInput & { id: number }) =>
      apiAction<Res>(`/api/records/${id}`, 'PUT', v),
    onSuccess: done,
  })
}

export function useDeleteRecord() {
  const done = useDone()
  return useMutation({
    retry: false,
    mutationFn: (id: number) => apiAction<Res>(`/api/records/${id}`, 'DELETE'),
    onSuccess: done,
  })
}

/**
 * 探测这条记录的证书。
 *
 * ⚠️ 后端的约定是 **HTTP 200 + `{ok:false, msg:"…"}`** 表示探不到，
 * 不是 4xx。所以调用方必须看 ok 字段，只看 HTTP 状态会把
 * "连不上、证书取不到"渲染成成功 —— 那正是这套系统要根治的病。
 */
export interface CheckCertRes {
  ok?: boolean
  fqdn?: string
  cert_expiry_at?: string
  /** ok=true 时的附带告警（比如证书快过期、链不完整） */
  warn?: string
  /** ok=false 时的失败原因 */
  msg?: string
}

export function useCheckRecordCert() {
  const done = useDone()
  return useMutation({
    retry: false,
    mutationFn: (id: number) => apiAction<CheckCertRes>(`/api/records/${id}/check-cert`, 'POST'),
    onSuccess: done,
  })
}

/**
 * 单条证书忽略 / 取消忽略。
 *
 * ⚠️ 忽略之后证书到期巡检不再报它，所以开启时**必须写理由**。
 * 半年后没人记得当初为什么忽略，而那条理由往往已经不成立了。
 */
export function useIgnoreRecordCert() {
  const done = useDone()
  return useMutation({
    retry: false,
    mutationFn: ({ id, ignored, reason }: { id: number; ignored: boolean; reason: string }) =>
      apiAction<Res>(`/api/records/${id}/cert-ignore`, 'PUT', { ignored, reason }),
    onSuccess: done,
  })
}
