import { ApiError, normalizeError, shouldRetry } from '@ops/api'
import { useQuery } from '@tanstack/react-query'
import { getToken } from '../../lib/auth.js'

/**
 * 集群体检。
 *
 * ⚠️ 这个接口有一条重要性质：**任何一项检查查不成，整个体检就不出结论**
 * （后端返回 error.healthIncomplete）。体检的输出是"没问题"这种断言，
 * 把"查询失败"当成"没查到问题"是最糟的失效模式 ——
 * 所以前端也绝不能把它降级成"本次没发现问题"。
 */
export interface Finding {
  /** critical / warning / info，原样透传 */
  severity: string
  /** 数据可信度 / 工作负载 / 节点 / 存储 / 成本 / 治理 —— 后端给的分类 */
  category: string
  title: string
  detail: string
  /**
   * 给**人**看的处置动作。
   *
   * ⚠️ 后端另有 `mcp_hint` 字段装 MCP 工具链，那个**不要在界面上显示**。
   * 原来两者共用一个 action 字段，于是界面上出现「用 list_pods 按 restarts 排序」——
   * 一个运维在网页里执行不了它，也不知道那是什么（P0-5）。
   */
  action: string
  count: number
  /**
   * count 数的是**什么东西**：pod / node / workload / pvc / hpa / resource / image。
   *
   * ⚠️ 没有它时，计数紧挨着 category 渲染，「33」+「工作负载」被读成
   * 「33 个工作负载」——而重启的是 Pod，两者可能差好几倍。
   * category 是归类，不是单位（P1-10）。
   */
  unit: string
  key: string
}

export interface HealthReport {
  summary: { total: number; critical: number; warning: number; info: number }
  findings: Finding[]
  /**
   * 🔴 这一轮**跑了哪几项检查** —— findings 的分母。
   *
   *	只有命中列表的话，两个集群条数不同时分不清
   *	"跑了没命中"和"这项没跑"，而两者的下一步完全相反（OPSCMDB-076）。
   * ⚠️ 老后端不返回它时是 undefined（= 不知道跑了几项），
   *	界面据此不显示 —— 不能兜成 0，那会变成"一项都没跑"。
   */
  checks?: string[]
}

interface RawReport {
  summary?: { total?: number; critical?: number; warning?: number; info?: number }
  checks?: string[]
  findings?: {
    severity?: string
    category?: string
    title?: string
    detail?: string
    action?: string
    count?: number
    unit?: string
    key?: string
  }[]
}

export function useHealth(clusterId: number) {
  return useQuery({
    queryKey: ['health', clusterId],
    enabled: clusterId > 0,
    queryFn: async (): Promise<HealthReport> => {
      const path = `/api/k8s/health?cluster_id=${clusterId}`
      let res: Response
      try {
        res = await fetch(path, { headers: { Authorization: `Bearer ${getToken()}` } })
      } catch (e) {
        throw new ApiError(normalizeError(e, { path }))
      }
      if (!res.ok) {
        throw new ApiError(
          normalizeError(await res.json().catch(() => ({})), { path, status: res.status }),
        )
      }
      const d = (await res.json()) as RawReport
      return {
        summary: {
          total: d.summary?.total ?? 0,
          critical: d.summary?.critical ?? 0,
          warning: d.summary?.warning ?? 0,
          info: d.summary?.info ?? 0,
        },
        // ⚠️ 不写 ?? []：空数组会被显示成"跑了 0 项"，
        //	而字段缺失的真实含义是"这个后端还没告诉我们"
        checks: d.checks,
        findings: (d.findings ?? []).map((f) => ({
          severity: f.severity ?? '',
          category: f.category ?? '',
          title: f.title ?? '',
          detail: f.detail ?? '',
          action: f.action ?? '',
          count: f.count ?? 0,
          unit: f.unit ?? '',
          key: f.key ?? '',
        })),
      }
    },
    staleTime: 60_000,
    retry: shouldRetry,
  })
}
