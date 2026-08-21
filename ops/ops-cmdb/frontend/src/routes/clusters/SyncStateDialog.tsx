import { toErrorInfo } from '@ops/api'
import { tError, useTranslation } from '@ops/i18n'
import { Badge, Banner, Button, Dialog, Skeleton } from '@ops/ui'
import { useQuery } from '@tanstack/react-query'
import { apiGet } from '../../lib/fetchJson.js'

interface ResourceState {
  resource: string
  ok: boolean
  count: number
  duration_ms: number
  err: string
  /** never / failed / stale / fresh —— 后端算好的四态，原样用 */
  freshness: string
  /** 距上次成功采集多少秒。-1 = 从没采过 */
  age_sec: number
  last_sync?: string
  /**
   * 这一类**有一部分没采到**的原因（CRD 没装 / 缺权限 / 开关没开）。
   *
   * ⚠️ 和 err 不是一回事：err 是"这一轮失败了"，
   * skip_note 是"采集成功了，但少了一整块，而 count 看着是正常的"。
   * 实测某集群 `gateways: ok=true, count=6`（6 个全是 Istio 的），
   * Gateway API 那一套一个都没采到 —— 所有指标都正常，
   * 于是这个缺口躲过了所有新鲜度检查（P0-11）。
   */
  skip_note?: string
}

interface ClusterSyncState {
  cluster_id: number
  cluster_name: string
  resources: ResourceState[]
}

/**
 * ⚠️ 这个接口返回的是**包装对象** `{checked_at, clusters:[...]}`，不是裸数组。
 *
 * 我在同一天里第二次踩这个坑（上一次是 /api/k8s/pdbs 的 {items,...}）。
 * 教训不是"下次注意"——**光靠注意是没用的**，而是：
 * 写前端类型之前先 `curl` 一次看真实形状，别按"看起来应该是列表"去推。
 */
interface SyncStateResp {
  checked_at?: string
  clusters?: ClusterSyncState[]
}

/**
 * 采集明细：这个集群的每一类资源，最后一次采是什么时候、成没成。
 *
 * # 为什么必须能点进来
 *
 * 整个 CMDB 的结论都建立在"采到的数据是新的"这个前提上。
 * 集群页原来只显示节点数、Pod 数 —— 那些数字**看不出是什么时候采的**，
 * 也看不出某一类资源是不是压根没采到。
 *
 * 后端 `/api/k8s/sync-state` 一直都在（OPSCMDB-023 第二档），前端没接。
 *
 * # ⚠️ 四态各有各的下一步，不能合并
 *
 * - `never`  从没采过 → 这一类的所有结论都不成立（不是"没有这种资源"）
 * - `failed` 上次采失败 → 看 err，多半是权限或连通性
 * - `stale`  太久没更新 → 采集器可能停了
 * - `fresh`  正常
 *
 * 把前三种都渲染成灰色的"—"，等于把三种故障说成了同一件事。
 */
export function SyncStateDialog({
  clusterId,
  clusterName,
  onClose,
}: {
  clusterId: number
  clusterName: string
  onClose: () => void
}) {
  const { t } = useTranslation()
  const q = useQuery({
    queryKey: ['k8s-sync-state', clusterId],
    queryFn: () => apiGet<SyncStateResp>(`/api/k8s/sync-state?cluster_id=${clusterId}`),
    staleTime: 30_000,
    retry: false,
  })

  const all = q.data?.clusters ?? []
  const st = all.find((x) => x.cluster_id === clusterId) ?? all[0]
  const rows = st?.resources ?? []
  // ⚠️ 部分跳过的也算有问题：它的 freshness 是 fresh、count 有数字，
  // 唯独少了一整块。只按 freshness 筛会把这种缺口漏掉
  const bad = rows.filter((r) => r.freshness !== 'fresh' || !!r.skip_note)

  return (
    <Dialog
      open
      onClose={onClose}
      title={t('clusters:sync.title')}
      description={clusterName}
      closeLabel={t('common:action.close')}
      width={760}
      footer={
        <Button variant="primary" size="sm" onClick={onClose}>
          {t('common:action.close')}
        </Button>
      }
    >
      <div className="flex flex-col gap-3">
        {q.isPending ? <Skeleton className="h-5 w-[40%]" /> : null}
        {q.isError ? (
          <Banner tone="bad">
            <span>{tError(t, toErrorInfo(q.error).messageKey, toErrorInfo(q.error).params)}</span>
            <span className="mt-0.5 block">{toErrorInfo(q.error).detail}</span>
          </Banner>
        ) : null}

        {q.isSuccess && rows.length === 0 ? (
          // ⚠️ 一条采集记录都没有 = 这个集群从没采过，不是"没有资源"
          <Banner tone="warn">
            <span>{t('clusters:sync.neverAny')}</span>
          </Banner>
        ) : null}

        {bad.length > 0 ? (
          <Banner tone="warn">
            <span>{t('clusters:sync.badCount', { count: bad.length, total: rows.length })}</span>
          </Banner>
        ) : null}

        {rows.length > 0 ? (
          <div className="max-h-[54vh] overflow-auto rounded-[var(--radius)] border border-border">
            <table className="w-full text-[12px]">
              <thead className="sticky top-0 bg-card">
                <tr className="border-b border-border text-left text-[11px] text-muted-foreground">
                  <th className="px-2 py-2 font-medium">{t('clusters:sync.resource')}</th>
                  <th className="px-2 py-2 font-medium">{t('clusters:sync.state')}</th>
                  <th className="px-2 py-2 font-medium">{t('clusters:sync.lastSync')}</th>
                  <th className="px-2 py-2 font-medium">{t('clusters:sync.count')}</th>
                  <th className="px-2 py-2 font-medium">{t('clusters:sync.err')}</th>
                </tr>
              </thead>
              <tbody>
                {[...rows]
                  // 有问题的排前面：这个弹窗是用来找问题的，不是用来欣赏正常项的
                  .sort(
                    (a, b) =>
                      Number(a.freshness === 'fresh' && !a.skip_note) -
                      Number(b.freshness === 'fresh' && !b.skip_note),
                  )
                  .map((r) => (
                    <tr key={r.resource} className="border-b border-border/60 last:border-0">
                      <td className="px-2 py-1.5 font-mono">{r.resource}</td>
                      <td className="px-2 py-1.5">
                        <FreshnessBadge f={r.freshness} t={t} />
                        {/* fresh + 有跳过 = 「部分」。不标的话这一行看着完全正常 */}
                        {r.skip_note && r.freshness === 'fresh' ? (
                          <span className="ml-1">
                            <Badge tone="warn">{t('clusters:sync.partial')}</Badge>
                          </span>
                        ) : null}
                      </td>
                      <td className="px-2 py-1.5 tabular text-muted-foreground">
                        {/* age -1 = 从没采过。显示成 "0 秒前" 会变成最危险的谎 */}
                        {r.age_sec < 0 ? t('clusters:sync.never') : (r.last_sync ?? '—')}
                      </td>
                      <td className="px-2 py-1.5 tabular">{r.count}</td>
                      <td className="max-w-[240px] px-2 py-1.5">
                        {/* 失败原因红色，跳过说明用警告色 —— 两者的下一步不同：
                            前者查报错，后者补 CRD/权限/开关 */}
                        {r.err ? (
                          <span className="block truncate text-danger" title={r.err}>
                            {r.err}
                          </span>
                        ) : null}
                        {r.skip_note ? (
                          <span className="block text-warning" title={r.skip_note}>
                            {r.skip_note}
                          </span>
                        ) : null}
                        {!r.err && !r.skip_note ? '' : null}
                      </td>
                    </tr>
                  ))}
              </tbody>
            </table>
          </div>
        ) : null}
      </div>
    </Dialog>
  )
}

function FreshnessBadge({
  f,
  t,
}: {
  f: string
  t: (k: string, p?: Record<string, unknown>) => string
}) {
  // ⚠️ 未知取值也要显示出来，不能默默当成正常 —— 后端加了新状态时
  // 这里会显示原值，而不是把它悄悄归到 fresh 里
  if (f === 'fresh') return <Badge tone="ok">{t('clusters:sync.fresh')}</Badge>
  if (f === 'stale') return <Badge tone="warn">{t('clusters:sync.stale')}</Badge>
  if (f === 'failed') return <Badge tone="bad">{t('clusters:sync.failed')}</Badge>
  if (f === 'never') return <Badge tone="bad">{t('clusters:sync.never')}</Badge>
  return <Badge tone="warn">{f}</Badge>
}
