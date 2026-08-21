import { shouldRetry } from '@ops/api'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { apiAction, apiGet } from '../../lib/fetchJson.js'

/**
 * 域名注册商接入。
 *
 * 没有它，域名的到期日就永远不会更新 —— 而界面上那些域名看起来一切正常，
 * 只是"最后同步"停在很久以前。dns_sync 这个定时任务也会直接跳过。
 *
 * ⚠️ 租户级：每个租户接自己的注册商账号，凭据不共享。
 */
export interface Registrar {
  id: number
  name: string
  /**
   * 这个厂商有没有真正的同步实现。
   * 🔴 **能选 ≠ 能用**：白名单一度有 5 个厂商可选，后端只实现了 godaddy。
   * 选了其余几个会保存成功、显示「已启用」，但同步永远不会发生，且无任何报错。
   */
  sync_supported?: boolean
  /** godaddy / namecheap / aliyun / cloudflare / other —— 原样透传 */
  provider: string
  /** 只有"配没配凭据"，接口从不回传内容 */
  has_cred: boolean
  /**
   * 预演模式：续费/写回只打日志，不真发请求、不扣费。
   * ⚠️ 开着的时候界面上必须显眼标出来 —— 否则会有人以为续费成功了。
   */
  dry_run: boolean
  enabled: number
}

export function useRegistrars() {
  return useQuery({
    queryKey: ['registrars'],
    queryFn: () => apiGet<{ items?: Registrar[] } | Registrar[]>('/api/registrars'),
    select: (d) => (Array.isArray(d) ? d : (d.items ?? [])),
    staleTime: 30_000,
    retry: shouldRetry,
  })
}

export interface RegistrarInput {
  id?: number
  name: string
  provider: string
  /** 明文输入，后端加密存。⚠️ 编辑时留空 = 保留原凭据 */
  credential: Record<string, string>
  dry_run: boolean
  enabled: number
}

export function useSaveRegistrar() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: ({ id, ...body }: RegistrarInput) =>
      id ? apiAction(`/api/registrars/${id}`, 'PUT', body) : apiAction('/api/registrars', 'POST', body),
    onSuccess: () => void qc.invalidateQueries({ queryKey: ['registrars'] }),
  })
}

export function useDeleteRegistrar() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (id: number) => apiAction(`/api/registrars/${id}`, 'DELETE'),
    onSuccess: () => void qc.invalidateQueries({ queryKey: ['registrars'] }),
  })
}

/**
 * 认得的注册商类型。**与后端 registrar.Providers 一字不差。**
 *
 * ⚠️ 这一份是从后端那张白名单抄来的常量，不是随手写的：
 * 打错一个字母（godady）后端会拒绝，但如果后端哪天没校验，
 * 就会静默建出一条永远不同步的记录 —— 界面上它显示"已启用"。
 */
export const PROVIDERS = ['godaddy', 'namecheap', 'aliyun', 'cloudflare', 'other'] as const

/**
 * 还没有同步实现的厂商。
 *
 * 🔴 与后端 dnsource.SyncSupported 对应。列在这里是为了在**选中的那一刻**
 * 就告诉用户"选了会发生什么"，而不是等保存成功、显示「已启用」之后，
 * 让他去猜为什么到期日一直不更新。
 *
 * ⚠️ 后端新增 adapter 时要同步删掉这里对应的项；
 * 真正的判据以接口回的 sync_supported 为准，这里只用于表单内的即时提示。
 */
export const NO_SYNC_PROVIDERS: string[] = ['namecheap', 'aliyun', 'cloudflare']

/**
 * 从这个注册商拉一次域名。
 *
 * ⚠️ 入口以前没有 —— 而域名页的「立即同步」是**遍历所有注册商**的。
 * 只有一个注册商坏掉时，全量同步会连带其它几个一起重跑（都在打外部 API）；
 * 更麻烦的是失败原因混在一起，看不出是哪一条接入的问题。
 */
export function useSyncRegistrar() {
  const qc = useQueryClient()
  return useMutation({
    retry: false,
    mutationFn: (id: number) =>
      apiAction<{ ok?: boolean; msg?: string }>(`/api/sources/${id}/sync`, 'POST'),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: ['registrars'] })
      void qc.invalidateQueries({ queryKey: ['domain-list'] })
    },
  })
}

/**
 * 后台同步进度。
 *
 * # 为什么需要它
 *
 * 「立即同步」按钮只反馈**本次点击**发出去了 —— 同步是后台跑的，
 * 按钮变回原样时同步往往还没跑完。于是界面上「已保存」和
 * 「其实同步中断了」长得一模一样。
 *
 * ⚠️ 三态里最要紧的是 `interrupted`：`running=1` 但心跳早停了，
 * 说明**跑它的那个副本已经退出**。它既不能显示成「同步中」
 * （那会让这个数据源再也点不动），也不能显示成「没在同步」
 * （那会掩盖掉这次同步其实中断了）。后端已经把这一档算好了。
 */
export interface SyncProgress {
  running?: boolean
  /** false = 从来没发起过同步。⚠️ 与「同步完了」不是一回事 */
  started?: boolean
  total?: number
  done?: number
  synced_domains?: number
  synced_records?: number
  imported_records?: number
  stale_domains?: number
  error?: string
  /** 跑它的副本，排障时直接对上 Pod 日志 */
  replica?: string
  /** 🔴 running=1 但副本已退出 —— 这次同步中断了 */
  interrupted?: boolean
  finished_at?: string
}

/**
 * ⚠️ 只在**同步进行中**轮询，跑完就停。
 *
 * 这个接口读的是我们自己的库（不像 CDN 体检那样打外部服务），
 * 所以轮询是安全的；但没有同步在跑时轮它没有意义，纯属浪费。
 */
export function useSyncProgress(id: number | null) {
  return useQuery({
    queryKey: ['sync-progress', id],
    enabled: id !== null,
    queryFn: () => apiGet<SyncProgress>(`/api/sources/${id}/sync-status`),
    refetchInterval: (q) => {
      const d = q.state.data as SyncProgress | undefined
      // 中断了也要停轮询 —— 它不会自己变好，继续轮只是刷日志
      return d?.running && !d.interrupted ? 2000 : false
    },
    retry: shouldRetry,
  })
}

/**
 * 注册商 API 的限流用量。
 *
 * # 为什么值得显示
 *
 * `last_limited_at` 是「域名到期日突然不更新了」的**静默根因**：
 * 打爆了厂商配额之后，同步会被限流拖慢甚至失败，
 * 而界面上域名列表看起来一切正常，只有「最后同步」停在很久以前。
 *
 * # 🔴 但这些数字有一个必须说出来的边界
 *
 * `Stats()` 返回的是**本副本进程内**的计数：
 *   · 重启归零
 *   · 多副本时各算各的（跨副本配额是另一层，不在这个数里）
 *
 * 所以「今日 0 次」**不等于**「今天没同步过」—— 可能是别的副本干的活，
 * 也可能是刚重启过。不写清楚的话，这个 0 会被读成"同步没跑"。
 */
export interface SourceUsage {
  minute_used?: number
  limit?: number
  today_total?: number
  /** 空 = 本副本启动以来没被限流过。⚠️ 不等于"从来没被限流过" */
  last_limited_at?: string
}

export function useSourceUsage(id: number) {
  return useQuery({
    queryKey: ['source-usage', id],
    queryFn: () => apiGet<SourceUsage>(`/api/sources/${id}/usage`),
    // 用量变化不快，且这是纯本地内存读取，不用频繁刷
    staleTime: 30_000,
    retry: shouldRetry,
  })
}
