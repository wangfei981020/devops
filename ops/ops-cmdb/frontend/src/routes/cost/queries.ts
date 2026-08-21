import { shouldRetry } from '@ops/api'
import { useQuery } from '@tanstack/react-query'
import { apiGet } from '../../lib/fetchJson.js'

export interface CostRow {
  key: string
  monthly: number
  count: number
}

export interface CostOverview {
  total_monthly: number
  /** 恒为 true：这一页永远是估算，不是账单 */
  estimated: boolean
  by_project: CostRow[]
  by_env: CostRow[]
  /**
   * ⚠️ 没能匹配到费率、按 0 计的机器数。它们不是"免费的"，是我们**算不出来**的。
   * 不显示的话，总额偏低，而偏低的成本报表没人会去质疑。
   */
  unpriced_hosts: number
  /** 已销毁但仍在台账里的机器数（不计入成本）。说明它们为什么不在总额里 */
  destroyed_excluded: number
  /**
   * 按**默认档**估价的机器数。
   *
   * ⚠️ 这是这一页真正的静默降级，比 `unpriced_hosts` 隐蔽得多：
   * 算不出来（0 元）很醒目，而回退默认档会用一个可能不对的价格
   * 算出一个**看起来完全正常的数**。原来它被记成"已定价"，
   * 界面上没有任何痕迹（P1-47）。
   */
  fallback_priced_hosts?: number
  /** 缺费率的区域。光说"有 3 台按默认档估"没法行动，要知道去给哪个区域补 */
  fallback_regions?: string[]
}

export function useCostOverview() {
  return useQuery({
    queryKey: ['cost', 'overview'],
    queryFn: () => apiGet<CostOverview>('/api/cost/overview'),
    staleTime: 5 * 60_000,
    retry: shouldRetry,
  })
}

import { useMutation, useQueryClient } from '@tanstack/react-query'
import { apiAction } from '../../lib/fetchJson.js'

/**
 * 给当月打一份成本快照。
 *
 * ⚠️ 快照是**成本报表的原料**：没有快照就没有环比，"这个月涨了多少"
 * 这个问题在界面上会一直显示不出来 —— 而它看起来只是"数据还没到"。
 * 定时任务每月打一次，这个按钮是给"刚接入想立刻看到数"和"补一个漏掉的月份"用的。
 */
export function useCostSnapshot() {
  const qc = useQueryClient()
  return useMutation({
    retry: false,
    // ⚠️ 路径写成固定形状 + query 参数拼在后面：
    // 把 `${...}` 拼进路径中段的话，静态扫描（check-write-coverage）
    // 认不出这是哪个接口 —— 一个已经接好的入口会被报成"没接线"。
    // month 传空串时后端取当月（见 SnapshotNow）。
    mutationFn: (month?: string) =>
      apiAction<{ ok?: boolean; msg?: string; count?: number }>(
        `/api/k8s/cost/snapshot?month=${month ?? ''}`,
        'POST',
      ),
    onSuccess: () => void qc.invalidateQueries(),
  })
}
