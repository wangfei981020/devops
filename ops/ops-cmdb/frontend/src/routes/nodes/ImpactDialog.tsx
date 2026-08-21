import { toErrorInfo } from '@ops/api'
import { tError, useTranslation } from '@ops/i18n'
import { Badge, Banner, Button, Dialog, Skeleton } from '@ops/ui'
import { useQuery } from '@tanstack/react-query'
import { apiGet } from '../../lib/fetchJson.js'

interface NodeImpact {
  cluster_id: number
  node: string
  pods?: { namespace: string; pod: string; workload: string; phase: string }[]
  workloads?: string[]
  services?: string[]
  ingresses?: string[]
  domains?: string[]
}

/**
 * 这个节点挂了会影响谁。
 *
 * # 为什么这条独立于「变更影响面」页
 *
 * 前端已有的 `/topology/impact` 页调的是 `/api/impact?ci_id=`，走的是
 * **CMDB 的 CI 关系图**。这里调的是 `/api/k8s/impact?node=`，
 * 走的是**实际采到的 K8s 拓扑**（Pod 落在哪个节点 → Service → Ingress/VS → 域名）。
 *
 * 两者回答的不是同一个问题，后者才是节点维护/驱逐前要看的那个
 * （OPSCMDB-023 第一档，后端一直都在，前端没接）。
 *
 * # ⚠️ 空结果 ≠ 没有影响
 *
 * 域名一栏为空可能是「这个节点上的服务确实不对外」，也可能是
 * **Ingress/VirtualService 还没采到**。这两种情况下运维的下一步完全相反，
 * 所以空的时候要说清楚是哪一种，不能只留一片空白让人默认「没影响，可以直接драй」。
 */
export function NodeImpactDialog({
  clusterId,
  node,
  onClose,
}: {
  clusterId: number
  node: string
  onClose: () => void
}) {
  const { t } = useTranslation()
  const q = useQuery({
    queryKey: ['node-impact', clusterId, node],
    queryFn: () =>
      apiGet<NodeImpact>(
        `/api/k8s/impact?cluster_id=${clusterId}&node=${encodeURIComponent(node)}`,
      ),
    staleTime: 30_000,
    retry: false,
  })
  const d = q.data

  return (
    <Dialog
      open
      onClose={onClose}
      title={t('nodes:impact.title')}
      description={node}
      closeLabel={t('common:action.close')}
      width={820}
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

        {d ? (
          <>
            {/* 对外域名放最前面：它是「用户会不会有感知」的唯一答案 */}
            <Section
              title={t('nodes:impact.domains')}
              items={d.domains ?? []}
              tone="danger"
              emptyHint={t('nodes:impact.noDomains')}
              t={t}
            />
            <Section title={t('nodes:impact.ingresses')} items={d.ingresses ?? []} t={t} />
            <Section title={t('nodes:impact.services')} items={d.services ?? []} t={t} />
            <Section title={t('nodes:impact.workloads')} items={d.workloads ?? []} t={t} />

            <section className="flex flex-col gap-1.5">
              <h3 className="text-xs font-medium text-foreground">
                {t('nodes:impact.pods', { count: (d.pods ?? []).length })}
              </h3>
              {(d.pods ?? []).length === 0 ? (
                <p className="text-xs text-muted-foreground">{t('nodes:impact.noPods')}</p>
              ) : (
                <div className="max-h-[30vh] overflow-auto rounded-[var(--radius)] border border-border">
                  <table className="w-full text-[11px]">
                    <tbody>
                      {(d.pods ?? []).map((p) => (
                        <tr
                          key={`${p.namespace}/${p.pod}`}
                          className="border-b border-border/60 last:border-0"
                        >
                          <td className="px-2 py-1 font-mono text-muted-foreground">
                            {p.namespace}
                          </td>
                          <td className="px-2 py-1 font-mono">{p.pod}</td>
                          <td className="px-2 py-1 text-muted-foreground">{p.workload || '—'}</td>
                          <td className="px-2 py-1">
                            {/* phase 原样透传：运维拿这个词直接去 kubectl 里搜 */}
                            <Badge tone={p.phase === 'Running' ? 'ok' : 'warn'}>{p.phase}</Badge>
                          </td>
                        </tr>
                      ))}
                    </tbody>
                  </table>
                </div>
              )}
            </section>
          </>
        ) : null}
      </div>
    </Dialog>
  )
}

function Section({
  title,
  items,
  tone,
  emptyHint,
  t,
}: {
  title: string
  items: string[]
  tone?: 'danger'
  /** 为空时要说清楚是「确实没有」还是「可能没采到」，不能留白 */
  emptyHint?: string
  t: (k: string, p?: Record<string, unknown>) => string
}) {
  return (
    <section className="flex flex-col gap-1.5">
      <h3 className="text-xs font-medium text-foreground">
        {title} <span className="tabular text-muted-foreground">{items.length}</span>
      </h3>
      {items.length === 0 ? (
        <p className="text-xs text-muted-foreground">{emptyHint ?? t('nodes:impact.none')}</p>
      ) : (
        <div className="flex flex-wrap gap-1.5">
          {items.map((x) => (
            <span
              key={x}
              className={`rounded-[var(--radius)] border px-1.5 py-0.5 font-mono text-[11px] ${
                tone === 'danger'
                  ? 'border-danger/40 bg-danger-bg text-danger'
                  : 'border-border text-foreground'
              }`}
            >
              {x}
            </span>
          ))}
        </div>
      )}
    </section>
  )
}
