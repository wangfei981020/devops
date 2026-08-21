import { shouldRetry } from '@ops/api'
import { useQuery } from '@tanstack/react-query'
import { apiGet, apiGetText } from '../../lib/fetchJson.js'

/**
 * Pod 排障三件套：日志 / 事件 / 规则诊断。
 *
 * 老 CMDB 有这三个入口，新版一直没接（OPSCMDB-021）——缺了等于出事时要回 kubectl，
 * 而 CMDB 的立项理由之一就是"不登服务器也能定位"。
 *
 * ⚠️ 三个都是**实时打到集群**的（日志走 APIServer 代理到 kubelet），
 * 不是查库。所以：只在抽屉打开、且当前就在看这一档时才请求，绝不预取、绝不轮询。
 */

export interface PodTarget {
  clusterId: number
  namespace: string
  name: string
}

const qs = (t: PodTarget, extra: Record<string, string | number> = {}) =>
  new URLSearchParams({
    cluster_id: String(t.clusterId),
    namespace: t.namespace,
    pod: t.name,
    ...Object.fromEntries(Object.entries(extra).map(([k, v]) => [k, String(v)])),
  }).toString()

/**
 * 容器日志。
 *
 * ⚠️ 返回的是 **text/plain 不是 JSON**。
 * 而且 kubelet 取不到时后端会退到 Loki，并在正文最前面加两行 `#` 注释说明
 * "这是历史日志、以及 kubelet 为什么失败" —— **那两行必须原样显示给用户**，
 * 否则他会把 Loki 的历史日志当成实时日志来判断问题还在不在。
 */
export function usePodLogs(
  t: PodTarget | null,
  tail: number,
  previous: boolean,
  container = '',
  /**
   * ⚠️ 必须等容器名单回来再发。
   *	否则会先发一次不带 container 的请求 —— 多容器 Pod 上那次必然 502，
   *	名单回来后才重发正确的。用户看到的是"先闪一下错误再出日志"，
   *	而网络面板里留着一条红色的失败请求，排查的人会以为是真故障。
   */
  ready = true,
) {
  return useQuery({
    queryKey: ['pod-logs', t?.clusterId, t?.namespace, t?.name, tail, previous, container],
    queryFn: () =>
      apiGetText(
        `/api/k8s/pod-logs?${qs(t!, { tail, previous: previous ? 1 : 0, ...(container ? { container } : {}) })}`,
      ),
    enabled: !!t && ready,
    // 日志是一次性快照：自动刷新会让人正在读的那一屏跳走
    staleTime: Number.POSITIVE_INFINITY,
    retry: shouldRetry,
  })
}

export interface PodEvent {
  type?: string
  reason?: string
  message?: string
  count?: number
  last_seen?: string
}

export function usePodEvents(t: PodTarget | null) {
  return useQuery({
    queryKey: ['pod-events', t?.clusterId, t?.namespace, t?.name],
    queryFn: () => apiGet<PodEvent[]>(`/api/k8s/pod-events?${qs(t!)}`),
    enabled: !!t,
    staleTime: 15_000,
    retry: shouldRetry,
  })
}

/** 一条处置建议。`link` 有值时可以直接点过去（如证书页、服务页）。 */
export interface DiagSolution {
  text: string
  link?: string
}

/**
 * 规则诊断的结论。⚠️ `matched=false` 时 root_cause 没有意义，别渲染成"没问题"。
 *
 * 🔴 字段名踩过坑：这里原来写的是 `suggestions?: string[]` 和 `rule?: string`，
 * 而后端给的一直是 **`solutions`（对象数组）** 和 **`provider`**。
 * 名字对不上，`r.suggestions` 永远是 undefined ——
 * **诊断弹窗的「处置建议」整段从来没显示过**，后端白算了。
 *
 * 而且它躲过了 check-field-names：那个守卫查的是"前端用的字段名在后端出参全集里有没有"，
 * `suggestions` 在别的接口里确实存在（命名空间归属建议），于是判定通过。
 * ⚠️ **字段名在后端某处存在 ≠ 在这个接口存在**。
 */
export interface DiagnoseResult {
  result?: {
    matched?: boolean
    root_cause?: string
    evidence?: string[]
    solutions?: DiagSolution[]
    /**
     * 从日志里捞出来的报错行（只有规则给不出具体根因时才有）。
     *
     * 🔴 这是**证据不是判定**：每一行都是日志里原样存在的。
     * 它存在的理由是成本 —— 能在日志里看清的，就不该花钱问 AI。
     */
    extracted?: {
      lines?: string[]
      scanned?: number
      suppressed?: number
      no_error_found?: boolean
      note?: string
    }
    /** 判定来源：rule / ai:xxx。⚠️ 判前缀不要判全等，型号会变 */
    provider?: string
    /** 置信度 high/medium/low。⚠️ AI 判的由后端封顶在 medium */
    confidence?: string
    /**
     * 这一条**为什么走了 / 为什么没走** AI 兜底。
     *
     * 🔴 没走时也要给，而且要显示出来：
     *	「这个 Pod 怎么没让 AI 看看」是人对 AI 兜底的第一个疑问，
     *	只写进后端日志的话没人会去翻。
     */
    ai_gate?: {
      allowed?: boolean
      reason?: string
      code?: string
      /** 逐层结论：规则判了什么、日志里捞到几条报错、有没有相关变更 */
      layers_tried?: string[]
    }
  }
  context?: Record<string, unknown>
}

export function usePodDiagnose(t: PodTarget | null) {
  return useQuery({
    queryKey: ['pod-diagnose', t?.clusterId, t?.namespace, t?.name],
    queryFn: () => apiGet<DiagnoseResult>(`/api/k8s/diagnose?${qs(t!)}`),
    enabled: !!t,
    staleTime: 30_000,
    retry: shouldRetry,
  })
}

export interface PodContainerInfo {
  name: string
  init: boolean
  /** 常见注入容器（istio-proxy 等）。只影响默认选中项，**不隐藏** */
  sidecar: boolean
  ready: boolean
  state?: string
}

/**
 * Pod 里有哪些容器。
 *
 * 🔴 多容器 Pod 不指定容器时，kubelet **直接报 400**（不是"取第一个"）——
 *	接了 Istio 的集群里几乎每个业务 Pod 都带 istio-proxy，
 *	所以没有这个选择器等于那些 Pod 的日志根本看不了（OPSCMDB-049）。
 */
export function usePodContainers(t: PodTarget | null) {
  return useQuery({
    queryKey: ['pod-containers', t?.clusterId, t?.namespace, t?.name],
    queryFn: () => apiGet<{ items: PodContainerInfo[] }>(`/api/k8s/pod-containers?${qs(t!)}`),
    enabled: !!t,
    staleTime: 60_000,
    retry: shouldRetry,
  })
}
