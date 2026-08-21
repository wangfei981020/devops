import { shouldRetry } from '@ops/api'
import { useQuery } from '@tanstack/react-query'
import { apiGet } from '../../lib/fetchJson.js'

/**
 * 资源使用率：实时查 Prometheus 的 CPU/内存曲线。
 *
 * 覆盖 K8s（Pod / 工作负载 / 节点）与传统主机——老版就是一页管两边，
 * 因为排查"这台机器为什么慢"时，人不关心它是不是跑在 K8s 里。
 *
 * ⚠️ 这是**实时查询**，不是查库。没配 Prometheus 时后端返回
 * `{ok:false, error:"未配置可用的 prometheus 数据源"}` —— 那是**未接入**，
 * 不是"用量为 0"。两者必须显示成不同的东西，否则会被读成"这机器很闲"。
 */
export interface UsagePoint {
  t?: number
  v?: number
}

export interface UsageSeries {
  name?: string
  points?: UsagePoint[]
}

export interface UsageResult {
  ok?: boolean
  error?: string
  /**
   * 后端解析好的时序。
   *
   * ⚠️ 后端**同时**返回 `data`（Prometheus 原始信封，给 MCP 和排障用）。
   * 不要去读 `data` —— 真实路径是 `data.data.result`（两层信封），
   * 差一层就取到 undefined，页面会把"查询成功"渲染成"没有数据点"。
   */
  series?: UsageSeries[]
  unit?: string
  promql?: string
  /** true = 查询成功但确实没有序列。和 ok:false（查询失败）不是一回事 */
  empty?: boolean
  /** 空的可能原因，由后端给。⚠️ 不要自己编成"可能是名字写错了" */
  empty_hint?: string
}

export interface UsageParams {
  clusterId: number
  target: string
  namespace?: string
  name?: string
  metric: string
  /** 时间窗，分钟。⚠️ 见下面的注释：后端参数名是 minutes，不是 range */
  minutes: number
}

export function useUsage(p: UsageParams, enabled: boolean) {
  const qs = new URLSearchParams({
    cluster_id: String(p.clusterId),
    target: p.target,
    metric: p.metric,
    // ⚠️ 参数名是 `minutes` 且值是**数字**。
    //
    //	原来传的是 `range=1h` —— 后端压根没有 range 参数，
    //	`strconv.ParseInt("1h")` 失败后静默退回默认 60 分钟。
    //	于是时间范围下拉是个**纯装饰**：选 24h、选 7d，查的都是最近 1 小时，
    //	而图能正常画出来，没有任何地方提示范围没生效。
    //	（这个 bug 被 P0-14 掩护了很久：整页显示"无数据"，没人验到范围切换。）
    //
    //	参数名对不上但有默认值兜底 = 最难发现的一类 bug：
    //	不报错、不空、只是答非所问。
    minutes: String(p.minutes),
    ...(p.namespace ? { namespace: p.namespace } : {}),
    ...(p.name ? { name: p.name } : {}),
  }).toString()
  return useQuery({
    queryKey: ['obs-usage', qs],
    queryFn: () => apiGet<UsageResult>(`/api/obs/usage?${qs}`),
    enabled: enabled && p.clusterId > 0 && !!p.name,
    staleTime: 30_000,
    retry: shouldRetry,
  })
}

/**
 * 非 K8s 主机的用量排行。
 *
 * # 为什么需要它
 *
 * 这一页的 `target=host` 要**手输 IP 查单台** —— 而人来这一页时最想问的是
 * 「**哪些**机器最闲」（缩容依据），那需要先知道该看哪台，
 * 恰恰是这个问题本身。
 *
 * 后端把 Prometheus 的实测用量和 CMDB 台账的主机名/规格 join 好了，
 * 所以能直接读成「这台 8C16G 只用了 5%」。
 *
 * ⚠️ 后端查不到任何主机指标时给 `ok:false` + 具体原因
 * （标签缺失 / 筛选条件写错），必须原样显示 —— 它比前端能编的准确得多。
 */
export interface HostUsageRow {
  ip?: string
  host_name?: string
  env?: string
  project?: string
  team?: string
  /** 实测使用率。⚠️ 取不到时字段**不存在**，不是 0 */
  cpu_pct?: number
  mem_pct?: number
  vcpu?: number
  mem_gb?: number
}

export function useHostUsage(enabled: boolean) {
  return useQuery({
    queryKey: ['host-usage'],
    enabled,
    queryFn: () =>
      apiGet<{ ok?: boolean; error?: string; count?: number; items?: HostUsageRow[] }>(
        '/api/obs/host-usage',
      ),
    staleTime: 30_000,
    retry: shouldRetry,
  })
}
