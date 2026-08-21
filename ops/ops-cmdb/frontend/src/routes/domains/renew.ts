import { shouldRetry } from '@ops/api'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { apiAction, apiGet } from '../../lib/fetchJson.js'

/**
 * 域名批量续费。
 *
 * # 这是全站唯一一个**真花钱**的操作
 *
 * 所以它的每一条约定都不是"体验优化"，而是防止把钱花错：
 *
 *  1. **必须先预览**。执行接口要求带上预览时看到的 `confirm_count` 和
 *     `quoted_amount`，服务端会核对 —— 对不上说明台账在预览之后变过
 *     （有人加了/删了域名），此时拒绝执行。避免"我以为在续 3 个，实际续了 8 个"。
 *  2. **失败 ≠ 没扣费**。厂商接口超时的时候，钱可能已经扣了。
 *     所以后端的重试有前置条件（回查到期日确认没扣），前端**绝不能**
 *     自己加"失败重试"按钮。
 *  3. **uncertain 是独立的一档**，不是失败也不是成功：厂商响应没拿到，
 *     但回查确认已扣费。这类没有订单号，要人去账单核对，**绝不能重试**。
 */
export interface RenewItem {
  domain: string
  ci_id?: number
  /** ok / not_found / unsupported / duplicated —— 原样透传 */
  status: string
  reason?: string
  expiry_before?: string
  /** 预览时的"预计续到" */
  expiry_expect?: string
  /** 执行后的"实际续到" */
  expiry_after?: string
  /** 单价/年。0 = **查不到报价**，不是免费 */
  price_per_year?: number
  currency?: string
  order_id?: string
  /** 预演模式：只打日志不真扣费 */
  dry_run?: boolean
  /** ⚠️ 已扣费但拿不到厂商确认。既不是成功也不是失败，绝不能重试 */
  uncertain?: boolean
  /** 实际打了几次厂商接口。>1 说明重试过，对账时要知道 */
  attempts?: number
  /**
   * ⚠️ 疑似多续费：到期日前进的幅度超出了按年数推算的范围。
   *
   * 这条出现在**成功**的条目上 —— 续费确实成功了，但可能多扣了钱。
   * 所以既不能把这一条渲染成失败（钱扣了、域名续了），
   * 也不能因为"状态是成功"就不显示它：这是重复扣费唯一会留下的痕迹。
   */
  overpay_note?: string
  msg?: string
}

export interface RenewPreview {
  items: RenewItem[]
  total: number
  /** 可续的数量。执行时要把它作为 confirm_count 回传 */
  renewable: number
  period: number
  /** 按币种汇总。多币种时不能相加 */
  totals: Record<string, number>
  /** 拿到报价的数量 */
  priced: number
  /** ⚠️ 可续但**没有报价**的数量。这些照样会被续，只是不知道花多少 */
  unpriced: number
  /** 超出单次上限被截掉的数量 */
  truncated?: number
  warning?: string
}

export function usePreviewRenew() {
  return useMutation({
    mutationFn: (v: { domains: string; period: number }) =>
      apiAction<RenewPreview & { ok?: boolean; error?: string }>('/api/domains/renew-batch/preview', 'POST', v),
  })
}

export interface RenewAccepted {
  job_id: string
  total: number
  accepted: boolean
  msg: string
}

/**
 * 真正执行。
 *
 * ⚠️ 参数里的 confirm_count / quoted_* 必须来自**这一次**预览的结果，
 * 不能是用户手填或前端记忆的旧值 —— 那样服务端的漂移检测就失效了。
 */
export function useExecuteRenew() {
  return useMutation({
    mutationFn: (v: {
      domains: string
      period: number
      confirm_count: number
      quoted_amount: number
      quoted_currency: string
    }) => apiAction<RenewAccepted & { ok?: boolean; error?: string }>('/api/domains/renew-batch', 'POST', v),
  })
}

export interface RenewJob {
  done?: boolean
  finished?: number
  total?: number
  items?: RenewItem[]
}

/** 轮询后台进度。任务只保留 2 小时，之后要去「续费记录」看。 */
export function useRenewJob(jobID: string | null) {
  return useQuery({
    queryKey: ['renew-job', jobID],
    queryFn: () => apiGet<RenewJob>(`/api/domains/renew-batch/${jobID}`),
    enabled: !!jobID,
    // 后台任务，2 秒一轮；跑完就停
    refetchInterval: (q) => (q.state.data?.done ? false : 2000),
    retry: shouldRetry,
  })
}

/**
 * 域名批量操作：同步、忽略、改状态、自动关联模块。
 *
 * ⚠️ 「忽略」不是「删除」也不是「正常」——它是**我们决定暂时不管**。
 * 忽略之后同步会跳过它，到期巡检也不再报它。所以忽略必须要有理由，
 * 而且在列表里要能一眼看出哪些是被忽略的（否则半年后没人知道为什么它不报警了）。
 */
export function useSyncDomains() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: () => apiAction<{ ok?: boolean; msg?: string }>('/api/domains/sync', 'POST'),
    onSuccess: () => void qc.invalidateQueries(),
  })
}

export function useBulkIgnore() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (v: { ci_ids: number[]; ignored: number; reason: string }) =>
      apiAction('/api/domains/bulk-ignore', 'POST', v),
    onSuccess: () => void qc.invalidateQueries(),
  })
}

export function useBulkStatus() {
  const qc = useQueryClient()
  return useMutation({
    /** status 传空串 = 清除，回到「未设置」 */
    mutationFn: (v: { ci_ids: number[]; status: string }) =>
      apiAction('/api/domains/bulk-status', 'POST', v),
    onSuccess: () => void qc.invalidateQueries(),
  })
}

/**
 * 从 K8s 入口（VirtualService / Ingress 的 hosts）自动回填模块。
 *
 * ⚠️ 只补空的，不覆盖已有值 —— 人工填过的不该被自动推断盖掉。
 */
/**
 * 自动关联的结果。
 *
 * ⚠️ `filled: 0` 必须能说清是**哪种** 0：
 *	没有可比对的入口数据（没得比）vs 有数据但域名对不上。
 *	两者的下一步完全相反，只显示"已完成"等于什么都没说（OPSCMDB-079）。
 */
export interface AutoLinkResult {
  ok?: boolean
  filled?: number
  scanned?: number
  msg?: string
  reason_key?: string
  reason_params?: Record<string, unknown>
  reason?: string
}

export function useAutoLinkModules() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: () =>
      apiAction<AutoLinkResult>('/api/domains/auto-link-modules', 'POST'),
    onSuccess: () => void qc.invalidateQueries(),
  })
}
