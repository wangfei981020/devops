import { toErrorInfo } from '@ops/api'
import { tError, useTranslation } from '@ops/i18n'
import { Banner, Button, Dialog, Skeleton } from '@ops/ui'
import { useQuery } from '@tanstack/react-query'
import { apiGet } from '../../lib/fetchJson.js'

/**
 * 体检项下钻：把「56 个 Pod 重启超 100 次」变成具体是哪 56 个。
 *
 * # 为什么必须有
 *
 * 集群健康页原来只有汇总数字，点不进去 —— 看到「56」之后唯一的出路是
 * 回命令行自己拼 kubectl。后端 `/api/k8s/health/detail` 一直都在
 * （OPSCMDB-023 第一档），前端没接。
 *
 * # ⚠️ 三种「没有明细」要分开说
 *
 * 后端对拿不到明细的情况给的是 `unsupported` 而不是空数组，因为这三件事完全不同：
 *
 *   1. 这一项**本来就不支持下钻**（noDrillHint）—— 比如某些聚合指标
 *   2. key 拼错了 —— 后端会把支持的 key 全列出来
 *   3. 支持下钻，**确实一条都没有**
 *
 * 前两种都必须原样显示后端那句话，绝不能渲染成「没有数据」——
 * 那会让人以为问题已经消失了。
 */
export function HealthDetailDialog({
  clusterId,
  itemKey,
  title,
  onClose,
}: {
  clusterId: number
  /** 体检项的 key，如 pod_high_restart */
  itemKey: string
  title: string
  onClose: () => void
}) {
  const { t } = useTranslation()

  const q = useQuery({
    queryKey: ['health-detail', clusterId, itemKey],
    queryFn: () =>
      apiGet<{
        key: string
        columns?: string[]
        rows?: unknown[][]
        count?: number
        note?: string
        /** 跨对象规律：这批问题是不是挤在同一个节点/命名空间上 */
        patterns?: { column: string; value: string; count: number; total: number; hint: string }[]
        truncated?: string
        unsupported?: string
      }>(`/api/k8s/health/detail?cluster_id=${clusterId}&key=${encodeURIComponent(itemKey)}`),
    staleTime: 30_000,
    retry: false,
  })

  const d = q.data
  const rows = d?.rows ?? []
  const cols = d?.columns ?? []

  return (
    <Dialog
      open
      onClose={onClose}
      title={title}
      description={itemKey}
      closeLabel={t('common:action.close')}
      width={900}
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

        {/* ⚠️ 原样显示后端那句话。它可能是「这项不支持下钻」，
            也可能是「key 不认识，支持的有 xxx」——都不是「没有数据」 */}
        {d?.unsupported ? (
          <Banner tone="info">
            <span>{d.unsupported}</span>
          </Banner>
        ) : null}

        {/* 🔴 跨对象规律排在清单**前面**，而且用 warn 不用 info。
            它回答的是「这是 10 个独立故障还是 1 个节点故障」——
            排在清单下面就等于没有：人扫完 11 行早就开始一个个查了。
            实测 DEV：11 个 Failed Pod 里 10 个在 node12 */}
        {(d?.patterns?.length ?? 0) > 0 ? (
          <div className="flex flex-col gap-1.5">
            {d?.patterns?.map((p) => (
              <Banner key={`${p.column}-${p.value}`} tone="warn">
                <span>{p.hint}</span>
              </Banner>
            ))}
          </div>
        ) : null}

        {d?.note ? (
          <Banner tone="info">
            <span>{d.note}</span>
          </Banner>
        ) : null}

        {/* 截断必须说出来：不说的话，人会拿前 N 条当成全部去做判断 */}
        {d?.truncated ? (
          <Banner tone="warn">
            <span>{d.truncated}</span>
          </Banner>
        ) : null}

        {rows.length > 0 ? (
          <div className="max-h-[56vh] overflow-auto rounded-[var(--radius)] border border-border">
            <table className="w-full text-[12px]">
              <thead className="sticky top-0 bg-card">
                <tr className="border-b border-border text-left text-[11px] text-muted-foreground">
                  {cols.map((c) => (
                    <th key={c} className="px-2 py-2 font-medium">
                      {c}
                    </th>
                  ))}
                </tr>
              </thead>
              <tbody>
                {rows.map((r, i) => (
                  // 行没有稳定 id，用整行内容做 key —— 同内容的两行本来就该被视作同一行
                  <tr key={`${i}-${r.join('|')}`} className="border-b border-border/60 last:border-0">
                    {r.map((cell, j) => (
                      <td
                        key={`${cols[j] ?? j}`}
                        className="max-w-[260px] truncate px-2 py-1.5 font-mono text-[11px]"
                        title={String(cell ?? '')}
                      >
                        {/* null 和空串要分开：一个是没这个字段，一个是值为空 */}
                        {cell === null || cell === undefined ? (
                          <span className="text-muted-foreground">—</span>
                        ) : (
                          String(cell)
                        )}
                      </td>
                    ))}
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        ) : !d?.unsupported && q.isSuccess ? (
          // 支持下钻、请求成功、确实 0 条 —— 这才是真的「没有」
          <Banner tone="info">
            <span>{t('health:detail.empty')}</span>
          </Banner>
        ) : null}
      </div>
    </Dialog>
  )
}
