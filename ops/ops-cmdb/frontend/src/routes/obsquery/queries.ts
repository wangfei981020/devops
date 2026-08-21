import { useMutation } from '@tanstack/react-query'
import { apiGet } from '../../lib/fetchJson.js'

/**
 * 观测数据自助查询：直接写 PromQL / LogQL 打到数据源。
 *
 * # 为什么用 useMutation 而不是 useQuery
 *
 * 这两个接口**直接打到 Prometheus / Loki**，一次宽匹配的聚合查询能把数据源拖垮。
 * 用 useQuery 会带来自动重取（窗口聚焦、重新挂载、key 变化），
 * 而"用户改了半个字符"就不该触发一次真实查询。
 * 查询必须**只在点击时发生**，这正是 mutation 的语义。
 *
 * 同理由见 OPSCMDB-023 的处置约定第 2 条：实时类接口只在用户主动点时请求。
 */

export interface PromSeries {
  metric: Record<string, string>
  /** instant 查询的单值 */
  value?: number
  /** range 查询的时间序列 [[ts, val], ...] */
  values?: [number, string][]
}

export interface PromResult {
  ok: boolean
  error?: string
  detail?: string
  /** 后端实际发给 Prometheus 的语句（可能被注入了集群选择器）——排错时最要紧的一条 */
  query_sent?: string
  result_type?: string
  series_count?: number
  series?: PromSeries[]
  /**
   * ⚠️ 查询有没有被限制在本集群内。
   *
   * false 意味着结果可能混进了别的集群的数据 —— 那会让人把别人的指标
   * 当成自己的。必须显示出来，不能默默返回一堆数字。
   */
  cluster_isolated?: boolean
  note?: string
  truncated?: string
  /** 空结果的原因猜测。⚠️ 空 ≠ 该组件正常 */
  empty_hint?: string
  /** 集群标签值在数据源里根本不存在 —— 比 empty_hint 更确定的根因 */
  cluster_label_error?: unknown
}

export interface LokiResult {
  ok: boolean
  error?: string
  status?: number
  step?: string
  data?: unknown
}

export function usePromQuery() {
  return useMutation({
    retry: false,
    mutationFn: (v: { clusterId: number; query: string; minutes: number }) =>
      apiGet<PromResult>(
        `/api/obs/prom-query?cluster_id=${v.clusterId}&query=${encodeURIComponent(v.query)}&minutes=${v.minutes}`,
      ),
  })
}

export function useLokiQuery() {
  return useMutation({
    retry: false,
    mutationFn: (v: { clusterId: number; query: string; minutes: number }) =>
      apiGet<LokiResult>(
        `/api/obs/loki?cluster_id=${v.clusterId}&query=${encodeURIComponent(v.query)}&minutes=${v.minutes}`,
      ),
  })
}

/** 指标名检索：写 PromQL 前先确认指标存在，省掉一轮「查了个空」 */
export interface MetricsResult {
  ok: boolean
  error?: string
  total_in_prometheus?: number
  matched?: number
  metrics?: string[]
  truncated?: string
  empty_hint?: string
}

export function usePromMetrics() {
  return useMutation({
    retry: false,
    mutationFn: (v: { clusterId: number; keyword: string }) =>
      apiGet<MetricsResult>(
        `/api/obs/prom-metrics?cluster_id=${v.clusterId}&keyword=${encodeURIComponent(v.keyword)}`,
      ),
  })
}

/**
 * 从 Loki 的原始响应里抽出日志行。
 *
 * ⚠️ 后端是**原样透传** Loki 响应的，前端得自己认这个形状：
 * `{data:{data:{result:[{stream:{...}, values:[[ns, line], ...]}]}}}`。
 * 认不出来时返回空数组而不是抛异常 —— 但调用方必须把「认不出」
 * 和「确实没有日志」分开显示（见页面里的三态）。
 */
export function extractLokiLines(raw: unknown): { ts: string; line: string; labels: string }[] {
  const out: { ts: string; line: string; labels: string }[] = []
  const d = raw as {
    data?: { data?: { result?: { stream?: Record<string, string>; values?: [string, string][] }[] } }
  }
  const result = d?.data?.data?.result
  if (!Array.isArray(result)) return out
  for (const s of result) {
    const labels = Object.entries(s.stream ?? {})
      .filter(([k]) => k !== 'filename')
      .map(([k, v]) => `${k}=${v}`)
      .join(' ')
    for (const [ns, line] of s.values ?? []) {
      // Loki 的时间戳是纳秒字符串，除以 1e6 得到毫秒
      const ms = Number(ns) / 1e6
      out.push({
        ts: Number.isFinite(ms) ? new Date(ms).toLocaleString() : ns,
        line,
        labels,
      })
    }
  }
  // 最新的在前：排障看的是"刚刚发生了什么"
  return out.sort((a, b) => (a.ts < b.ts ? 1 : -1))
}

/**
 * 标签取值补全。
 *
 * # 为什么值得接
 *
 * 写 PromQL 最大的门槛不是语法，是**不知道标签有哪些取值** ——
 * `namespace="..."` 里该填什么，猜错了返回空，而空结果看起来
 * 和「确实没有数据」一模一样（本页顶部那句提示说的就是这件事）。
 *
 * ⚠️ 后端在 500 处截断，但会**如实报出真实总数**并给 `truncated` 说明。
 * 前端必须把那句话显示出来 —— 否则「一共 3000 个取值」会被读成「一共 500 个」。
 */
export interface PromLabelValues {
  ok?: boolean
  error?: string
  label?: string
  /** ⚠️ 真实总数，不是 values 的长度 */
  count?: number
  values?: string[]
  /** 超过 500 时后端给的说明。有值就必须显示 */
  truncated?: string
}

export function usePromLabelValues() {
  return useMutation({
    mutationFn: (p: { clusterId: number; label: string; metric?: string }) => {
      const u = new URLSearchParams({ cluster_id: String(p.clusterId), label: p.label })
      if (p.metric) u.set('metric', p.metric)
      return apiGet<PromLabelValues>(`/api/obs/prom-labels?${u.toString()}`)
    },
  })
}
