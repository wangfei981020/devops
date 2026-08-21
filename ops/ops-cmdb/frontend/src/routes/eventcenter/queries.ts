import { shouldRetry } from '@ops/api'
import { useQuery } from '@tanstack/react-query'
import { apiGet } from '../../lib/fetchJson.js'

/**
 * 事件中心：平台层的统一时间线。
 *
 * ⚠️ 和「集群事件」是两个东西，名字不能混：
 *   事件中心 = CMDB 自己产生的（到期、变更、同步失败）+ 汇进来的 K8s Warning
 *   集群事件 = apiserver 的 Event 对象
 * 都叫"事件"的话，人点进来看不到想要的东西，还会以为是数据没采上来。
 */
export interface PlatformEvent {
  time: string
  /** expiry / change / sync / k8s / alert —— 原样透传，不翻译成中文再判断 */
  source: string
  /** critical / warning / info */
  level: string
  /**
   * 这一条**为什么**是这个级别。后端给。
   *
   * 🔴 一片红而不说为什么，等于要求人自己去背映射表 —— 而背不出来的
   * 结果是不再看颜色（OPSCMDB-031 P2-37：首屏 14 行全红）。
   */
  level_why?: string
  object: string
  title: string
  message: string
  /**
   * 集群名。
   *
   * ⚠️ 后端**一直在给**这个字段，界面上却没有集群列 —— 309 条事件跨多个集群，
   * 人无从分辨哪条属于哪个集群（OPSCMDB-031 P1-36）。
   *
   * 对照告警页 P1-30：那边**有**集群列但填的是数据源名；
   * 这边**有**正确的集群数据却不显示。两页各错一半。
   *
   * 空 = 这条事件不属于任何集群（域名到期、证书这类平台级事件），是正常的。
   */
  cluster?: string
  /** 未来会发生的（如"证书 15 天后到期"），不是已经发生的 */
  upcoming: boolean
  count: number
}

export interface EventCenterResult {
  events: PlatformEvent[]
  /** ⚠️ 本次**返回**的条数，不是总数。总数看 total */
  count: number
  by_level: Record<string, number>
  /**
   * 还**没发生**的到期预告条数（域名/证书快到期了）。
   *
   * ⚠️ 后端连字段名都叫 upcoming 了，语义完全明确，前端一直没接 ——
   * 这 5 条是整页 309 条里**唯一还来得及处理**的，其余全是已经发生的事，
   * 而它们混在 309 条 K8s 报错里按严重度排，翻到第几页全看运气（P1-33）。
   *
   * 这一条还是 P0-15「域名到期三条通路全断」的第一条：
   * 界面看不到 + 提醒发不出去 → 只有主动用 MCP 查才知道有 5 个域名 17 天后到期。
   */
  upcoming: number
  /** 截断前的真实总量。⚠️ 和 count 不是一回事 */
  total: number
  /** true = 事件太多被截断了，你看到的不是全部 */
  truncated: boolean
  limit: number
  /**
   * 合并掉了多少条（同一个对象反复报同一件事会被合并成一行 ×N）。
   *
   * ⚠️ 必须显示。合并后 total 变小是预期行为，
   * 但不说出来的话人会以为事件凭空少了 —— 后端注释里专门交代过这一点。
   */
  merged_away: number
  raw_total: number
}

export interface EventCenterParams {
  days: number
  source?: string
  level?: string
  [k: string]: string | number | undefined
}

/** 只看还来得及处理的（upcoming），本地筛 —— 后端没有这个参数 */
export function onlyUpcoming(events: PlatformEvent[]) {
  return events.filter((e) => e.upcoming)
}

export function useEventCenter(params: EventCenterParams) {
  const qs = new URLSearchParams(
    Object.entries(params)
      .filter(([, v]) => v !== '' && v !== undefined && v !== 'all')
      .map(([k, v]) => [k, String(v)]),
  ).toString()
  return useQuery({
    queryKey: ['event-center', qs],
    queryFn: () => apiGet<EventCenterResult>(`/api/k8s/event-center?${qs}`),
    staleTime: 30_000,
    retry: shouldRetry,
  })
}
