import { shouldRetry } from '@ops/api'
import { useQuery } from '@tanstack/react-query'
import { apiGet } from '../../lib/fetchJson.js'

/**
 * 告警。数据来自已接入的告警系统（夜莺等），我们只是**读**它。
 *
 * ⚠️ 没接告警数据源时，这一页是空的 —— 而"没有告警"和"没接告警系统"
 * 是完全不同的两件事。后端已经用 `configured` 明确告诉我们是哪一种，
 * 空态必须读它，不要靠"列表为空"去猜。
 */
export interface Alert {
  id?: number
  /** 已剥掉 " - S3" 这类级别后缀（级别有独立一列，重复显示只是挤地方） */
  rule_name?: string
  /** 被剥掉后缀时才有：夜莺控制台里的原始规则名，用于对照 */
  rule_name_raw?: string
  /** 摘要。⚠️ 后端字段叫 rule_note，不叫 summary —— 名字对不上会整列显示「—」 */
  rule_note?: string
  severity?: string
  /** 告警对象：哪台机器 / 哪个域名 / 哪条流。⚠️ 后端字段叫 object，不叫 target */
  object?: string
  /**
   * 真正的集群名，取自 tags.cluster。
   * ⚠️ 可能为空 —— 空就显示「—」，**不要退回 datasource 冒充集群**。
   */
  cluster?: string
  /** 夜莺顶层 cluster 字段的真实含义：数据源名（实测全是 "VictoriaMetrics"） */
  datasource?: string
  /** 告警组 = 责任方，形如「运维部门-业务运维组-PROD-G01」 */
  group?: string
  /** 触发值，字符串（"70.34"）。判断严重程度靠它，不是靠级别 */
  trigger_value?: string
  /** 已通知次数。数字大 = 这条已经响了很久没人处理 */
  notified?: number
  recovered?: boolean
  tags?: Record<string, string>
  trigger_time?: string
  recover_time?: string
}

export interface AlertListResult {
  items?: Alert[]
  list?: Alert[]
  total?: number
  /** ⚠️ false = 没接告警系统。此时空列表**不代表**没有告警 */
  configured?: boolean
  hint?: string
  hint_key?: string
  /** 夜莺侧总数 > 本次取回数 */
  truncated?: boolean
  returned?: number
  /** "filtered" = 筛完是空的，不是本来就没有 */
  empty_reason?: string
  /** 配了夜莺接入点的环境清单。前端照它渲染环境选择器，不自己编 */
  envs?: string[]
  /**
   * 取环境清单失败的原因。
   * ⚠️ 与 `envs: []` 严格区分：空数组 = 确实没有绑环境的接入点；
   *	有 envs_error = 我们**不知道**有哪些环境。
   *	两者都藏起选择器的话，"查不出来"这件事就此消失。
   */
  envs_error?: string
  /** 本次实际用的环境，原样回显 */
  env?: string
  filtered_out?: number
  kw_filtered_out?: number
}

/** 页面要用到的、除列表本身之外的全部状态。一个都不能丢，丢了就变成"看起来正常" */
export interface AlertsData {
  items: Alert[]
  total: number
  configured: boolean
  hint: string
  hintKey: string
  truncated: boolean
  filteredEmpty: boolean
  /**
   * 本次实际取回多少条、被筛掉多少条。
   *
   * 🔴 不说出来的话，一个短列表分不清是「筛掉了」还是「本来就少」。
   *	夜莺侧 total 是 292，界面显示 3 条 —— 光看这两个数字，
   *	人无从知道中间少掉的 289 条是被 severity 筛掉的、被关键词筛掉的，
   *	还是被 limit 截断的。三种情况下一步动作完全不同。
   */
  returned: number
  filteredOut: number
  kwFilteredOut: number
  /**
   * 可选环境。
   * ⚠️ 空数组 = 后端说"没有任何绑了环境的夜莺接入点"，
   * 这时环境选择器不该显示 —— 显示一个只有「全部」的下拉是在暗示
   * "还有别的环境，只是你没选"，而事实是只有一个接入点。
   */
  envs: string[]
  /** 非空 = 环境清单取不到（不是"没有环境"）。界面必须说出来 */
  envsError: string
}

export function useAlerts(params: Record<string, string | number>) {
  const qs = new URLSearchParams(
    Object.entries(params)
      .filter(([, v]) => v !== '' && v !== undefined)
      .map(([k, v]) => [k, String(v)]),
  ).toString()
  return useQuery<AlertsData>({
    queryKey: ['alerts', qs],
    queryFn: async () => {
      const d = await apiGet<AlertListResult>(`/api/alerts?${qs}`)
      // 老接口的出参形状还没统一到 httpx 的 items/total 契约，这里兼容两种。
      // ⚠️ 不要在这里"猜"总数：拿不到 total 就用条数，而不是编一个
      const items = d.items ?? d.list ?? []
      return {
        items,
        total: d.total ?? items.length,
        // ⚠️ 默认 true：读不到这个字段时宁可不说"未接入"，
        // 把已接入的环境说成未接入会让人去改一个本来是对的配置
        configured: d.configured !== false,
        // hint_key 优先（可翻译），没有的退回后端直接给的中文（见 lib/hintText.ts）
        hintKey: d.hint_key ?? '',
        hint: d.hint ?? '',
        truncated: d.truncated === true,
        filteredEmpty: d.empty_reason === 'filtered',
        // ⚠️ 后端只在真的筛掉了才给这两个字段，缺失时是 0（没筛掉），
        //	不是"不知道"—— 所以这里 ?? 0 是对的
        returned: d.returned ?? items.length,
        filteredOut: d.filtered_out ?? 0,
        kwFilteredOut: d.kw_filtered_out ?? 0,
        envs: d.envs ?? [],
        envsError: d.envs_error ?? '',
      }
    },
    staleTime: 30_000,
    retry: shouldRetry,
  })
}
