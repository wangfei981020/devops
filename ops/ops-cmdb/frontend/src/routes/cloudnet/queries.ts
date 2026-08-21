import { shouldRetry } from '@ops/api'
import { useQuery } from '@tanstack/react-query'
import { apiGet } from '../../lib/fetchJson.js'

/** IP 台账：静态 IP + 主机内外网 IP + LB VIP 的聚合。 */
export interface CloudIp {
  ip?: string
  /** VIP / STATIC / HOST_EXTERNAL …… 原样透传 */
  kind?: string
  owner?: string
  project?: string
  provider?: string
  region?: string
  /** 预留了但没绑到任何东西 —— 白花钱 */
  idle?: boolean
}

export interface Firewall {
  name?: string
  network?: string
  direction?: string
  priority?: number
  action?: string
  /** 库里是逗号串，**接口层已拆成数组**（network_resources.go 有说明） */
  source_ranges?: string[]
  /**
   * 放行的协议与端口，形如 `tcp:6379;udp:53`。
   *
   * ⚠️ 字段名是 `protocols`，**不是 `allowed`**。
   * 这里原来声明成 `allowed`，于是「放行」列 73 行全部渲染成「—」——
   * 后端数据完全正常，问题全在名字对不上，而页面看着只像"这列没数据"。
   */
  protocols?: string
  /** 同上：接口给数组 */
  target_tags?: string[]
  /**
   * 后端判定的高危（任意来源放行敏感端口 / 未限端口）。不要在前端另判一次。
   *
   * ⚠️ 字段名是 `high_risk`，**不是 `risky`**（同批接口里 IAM 那个才叫 risky）。
   * 声明错的后果：后端判出 18 条高危，页面上一条都没标红 ——
   * 这比整列显示「—」危险得多，因为它把"有高危"显示成了"没高危"。
   */
  high_risk?: boolean
  /** 高危的具体理由（开了哪几个敏感端口 / 未限端口）。只有 high_risk 时才有 */
  risk_reason?: string
  disabled?: boolean
}

export interface IamBinding {
  project?: string
  member?: string
  role?: string
  /**
   * 风险等级：critical / high / medium / 空串。
   *
   * 🔴 这里原来声明的是 `risky?: boolean` + `risk_reason` —— 后端**从不返回**那两个名字。
   *	后端算好的是 `severity` 和 `issue`（handlers/cloud_iam_dns.go:76）。
   *	于是 IAM 页那一列永远是「—」：过宽授权（owner / allUsers 公开授权）
   *	在界面上**永远不标红**，而排序也是空转。
   *	名字对不上不会报错，只是那一列什么都不显示 —— 又一次"算了没人用"。
   *	（`risky` 是防火墙那批接口里的名字，两批混了；由 check-field-endpoints 抓出。）
   *
   * ⚠️ 空串 = 这条绑定没有风险，**不是**"没判"。后端只在 severity 非空时才带这两个字段。
   */
  severity?: string
  /** 具体问题描述，如「拥有 roles/owner」。比"过宽"两个字可处置得多 */
  issue?: string
}

export interface IamResult {
  items: IamBinding[]
  total: number
  summary: Record<string, number>
  /**
   * ⚠️ 这句必须显示。后端的原话是：
   * 「任何 GCP 项目都至少有一条权限绑定，所以这说明尚未采集成功，而不是没有风险」。
   * 渲染成普通空态，一个采集挂掉的权限审计页看起来就是"你很安全"。
   */
  empty_hint?: string
}

export function useCloudIps() {
  return useQuery({
    queryKey: ['cloud-ips'],
    queryFn: () => apiGet<CloudIp[]>('/api/cloud-ips'),
    staleTime: 60_000,
    retry: shouldRetry,
  })
}

export function useFirewalls() {
  return useQuery({
    queryKey: ['cloud-firewalls'],
    queryFn: () => apiGet<Firewall[]>('/api/cloud-firewalls'),
    staleTime: 60_000,
    retry: shouldRetry,
  })
}

export function useIam(only: string) {
  const qs = only === 'issues' ? '?only=issues' : ''
  return useQuery({
    queryKey: ['cloud-iam', only],
    queryFn: () => apiGet<IamResult>(`/api/cloud-iam${qs}`),
    staleTime: 60_000,
    retry: shouldRetry,
  })
}
