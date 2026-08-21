import { shouldRetry } from '@ops/api'
import { useQuery } from '@tanstack/react-query'
import { apiGet } from '../../lib/fetchJson.js'

/**
 * 证书到期巡检。
 *
 * ⚠️ 与「运行 / 巡检」不是一回事：那个是**定时任务健康**，这个是**证书到期**。
 * 名字像，回答的问题完全不同（OPSCMDB-021 里专门标了这个陷阱）。
 *
 * 覆盖三类来源：
 *   `kind=online` 实际探测 443 拿到的
 *   `kind=domain` 域名注册到期（WHOIS）
 *   `kind=acme`   我方 ACME 签发的
 * 三类要分开看 —— 探测不到不代表证书不存在，可能只是那个域名不解析了。
 */
export interface InspectItem {
  kind?: string
  record_id?: number
  domain_ci_id?: number
  fqdn?: string
  domain?: string
  /** ⚠️ 空**不等于**没有到期日。配合 probe_state 才能知道是"没探过"还是"探失败了" */
  expiry_at?: string
  check_msg?: string
  ignored?: boolean
  ignore_reason?: string
  /** 所属主域名的生命周期状态：已下线的证书可以不续期 */
  domain_status?: string
  /** 解析目标地址 —— 内网判定的依据，界面上要能看见 */
  origin_ip?: string
  /**
   * 失败归类。⚠️ `scope=internal` 的**不是失败**：
   * 内网地址被公网巡检器探测，连不上是必然的。
   * 混进"检测失败 N"里会让那个数字虚高，然后没人再信它。
   */
  reason_key?: string
  reason_label?: string
  scope?: string
  /**
   * 这一条**有没有被探过**（只对 kind=online 有意义）。
   *
   * never  = 从没探过 → 到期日是「未知」，不是「没有到期日」
   * failed = 探了但失败 → 看 reason_label
   * ok     = 探到了
   *
   * ⚠️ 少了 never 这一档，「没探过」和「探了没结果」就分不开。
   * 实测 700 条全是 never，而界面渲染成 700 行「—」——
   * 「所有证书都没有到期日」是不可能的事，于是看的人只能怀疑数据坏了，
   * 或者干脆不再看这一页（P0-4）。
   */
  probe_state?: string
}

/**
 * ⚠️ 这个接口返回**包装对象**，不是裸数组。
 *
 * 本项目在这上面崩过两次页面（`{items,...}` 被当成数组，`o is not iterable`），
 * 所以这里把形状写死在类型里，并且 `useCertInspect` 统一在一处解包。
 *
 * `probe_note` 是这次改形状的**目的**：
 * 700 行空白本身传达不了任何信息，而顶层一句
 * 「负责探测的定时任务被停用且从没跑过，这些到期日全部未知」
 * 能直接指向下一步。
 */
export interface InspectResult {
  items?: InspectItem[]
  total?: number
  /** never / partial / ok —— 整批数据能不能信 */
  probe_state?: string
  /**
   * 语言包 key + 插值参数。
   * ⚠️ 后端**不发拼好的句子** —— 它拼的中文在英文界面上永远是中文（OPSCMDB-054）。
   *	判定仍在后端，前端只负责把它说成人话。
   */
  probe_note_key?: string
  probe_note_params?: Record<string, unknown>
  never_probed?: number
  probe_failed?: number
  /** ⚠️ 解析到内网地址的条数，**不算失败** */
  internal_targets?: number
}

export interface InspectData {
  items: InspectItem[]
  probeState: string
  probeNoteKey: string
  probeNoteParams?: Record<string, unknown>
  neverProbed: number
  probeFailed: number
  internalTargets: number
}

export function useCertInspect() {
  return useQuery<InspectData>({
    queryKey: ['cert-inspect'],
    queryFn: async () => {
      const d = await apiGet<InspectResult>('/api/cert-inspect')
      return {
        items: d.items ?? [],
        // 拿不到 probe_state 时不要默认成 'ok' ——
        // 那会把"不知道能不能信"说成"可以信"
        probeState: d.probe_state ?? '',
        probeNoteKey: d.probe_note_key ?? '',
        probeNoteParams: d.probe_note_params,
        neverProbed: d.never_probed ?? 0,
        probeFailed: d.probe_failed ?? 0,
        internalTargets: d.internal_targets ?? 0,
      }
    },
    staleTime: 60_000,
    retry: shouldRetry,
  })
}
