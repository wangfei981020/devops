import { ApiError, normalizeError, shouldRetry } from '@ops/api'
import { useQuery } from '@tanstack/react-query'
import { getToken } from '../../lib/auth.js'
import { apiGet } from '../../lib/fetchJson.js'

/**
 * 全局态势。
 *
 * ⚠️ 这一页**不新造判据**，只汇总各列表页已有的。首页说 3 条异常、
 * 点进去看到 5 条，是最快毁掉信任的方式。
 */
export interface AttentionItem {
  key: string
  /** ⚠️ null = 这项**没能统计出来**，不是 0 条。0 是好消息，null 是我们不知道 */
  count: number | null
  severity: 'high' | 'medium'
  /** 点进去看明细的目标，已带好筛选条件 */
  link: string
}

export interface Situation {
  attention: AttentionItem[]
  inventory: Record<string, number | null>
  /** 各类数据最后一次采集的时刻；缺 key = 那类数据没接入 */
  freshness: Record<string, string>
  generatedAt: string
}

interface RawSituation {
  attention?: {
    key?: string
    count?: number | null
    severity?: string
    link?: string
  }[]
  inventory?: Record<string, number | null>
  freshness?: Record<string, string>
  generated_at?: string
}

export function useSituation() {
  return useQuery({
    queryKey: ['overview', 'situation'],
    queryFn: async (): Promise<Situation> => {
      let res: Response
      try {
        res = await fetch('/api/overview', {
          headers: { Authorization: `Bearer ${getToken()}` },
        })
      } catch (e) {
        throw new ApiError(normalizeError(e, { path: '/api/overview' }))
      }
      if (!res.ok) {
        throw new ApiError(
          normalizeError(await res.json().catch(() => ({})), {
            path: '/api/overview',
            status: res.status,
          }),
        )
      }
      const d = (await res.json()) as RawSituation
      return {
        attention: (d.attention ?? []).map((a) => ({
          key: a.key ?? '',
          // `?? 0` 在这里最危险：会把一个没统计出来的维度显示成"0 条，正常"
          count: a.count === undefined || a.count === null ? null : a.count,
          severity: a.severity === 'high' ? 'high' : 'medium',
          link: a.link ?? '',
        })),
        inventory: d.inventory ?? {},
        freshness: d.freshness ?? {},
        generatedAt: d.generated_at ?? '',
      }
    },
    // 首页会被长时间开着，而"有什么不对劲"是会变的
    staleTime: 60_000,
    refetchInterval: 2 * 60_000,
    retry: shouldRetry,
  })
}

/**
 * 台账计数（含**线上实际在用**的证书到期数）。
 *
 * # 为什么要单独接这个
 *
 * 全局态势的「证书」那一格统计的是**我方签发**的证书，生产上常年是 0。
 * 页面为此加了一句提示：「线上实际在跑的证书有几百张，在另一个入口里」——
 * 说得对，但它只是**指向别处**，没给数字。
 *
 * 而证书是到期就出事故的东西。「有几百张」和「其中 3 张 30 天内到期」
 * 是完全不同的两件事，后者才需要今天就动手。
 *
 * ⚠️ 口径（后端注释里钉死的，别在前端重算）：
 *   cert_expired          我方签发且已过期 —— 常年 0，**不能**据此认为证书没问题
 *   online_cert_expired   线上探测到的已过期
 *   online_cert_expiring  线上探测到的 30 天内到期（与到期巡检页逐字对齐）
 */
export interface DashboardCounts {
  // 🔴 domain_total / record_total / cert_total **故意不声明**。
  //
  //	首页的「家底盘点」已经在显示域名数和证书数了，取自 /api/overview 的
  //	`inventory`，而且口径更准：inventory.domains 排除了 ignored 的域名、
  //	inventory.certs 数的是证书表；dashboard 那三个数的是 cis 表且不排除。
  //
  //	两份口径不同的数字摆在同一个首页上，读的人会去算这笔账、算不平
  //	就会怀疑数据有错（同 DNS 那页横幅与列表口径不一致的教训）。
  //	所以这里**不接**，而不是接上再想办法解释差异。
  //
  //	⚠️ 后端仍然返回它们（别的接入方可能在用），这里只是前端不读 —— 
  //	这是 check-dead-fields 的第二条出路「有正当理由不显示」，已登记进 ALLOW。
  online_cert_expiring?: number
  online_cert_expired?: number
  /**
   * 🔴 从没探测过的线上证书条数 —— 上面两个计数的**分母**。
   *
   *	它们的 SQL 都带 `cert_expiry_at IS NOT NULL`，没探过的被排除在外。
   *	所以两个数都是 0 时，可能是"都没问题"，也可能是"一条都没探过"，
   *	而后者下"没有临期证书"这个结论不成立（OPSCMDB-063）。
   */
  online_cert_unprobed?: number
}

export function useDashboardCounts() {
  return useQuery({
    queryKey: ['dashboard-counts'],
    queryFn: () => apiGet<DashboardCounts>('/api/dashboard'),
    staleTime: 60_000,
    retry: shouldRetry,
  })
}
