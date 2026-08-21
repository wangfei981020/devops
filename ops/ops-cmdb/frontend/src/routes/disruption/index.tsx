import { toErrorInfo } from '@ops/api'
import { clusterLabel } from '../../lib/clusterLabel.js'
import { tError, useTranslation } from '@ops/i18n'
import { Badge, Banner, Field, Select, Skeleton } from '@ops/ui'
import { useState } from 'react'
import { useClusters } from '../clusters/queries.js'
import { type Hpa, type NodePool, type Pdb, useHpas, useNodePools, usePdbs } from './queries.js'

type Tab = 'pdb' | 'hpa' | 'pool'

/**
 * 变更前检查：中断预算 / 自动伸缩 / 节点池。
 *
 * # 为什么合成一页
 *
 * 三张表回答的是同一个问题：**这次升级、缩容、驱逐能不能安全做**。
 *
 * - PDB：驱逐时会不会被拒（余量为 0 就是会）
 * - HPA：副本数是不是已经贴着上限（贴着 = 再来流量就扛不住）
 * - 节点池：这批机器是什么规格、几台、什么版本
 *
 * 后端三个接口一直都在，前端一个都没接（OPSCMDB-023 第二档）。
 * 分散成三个菜单项的话，"变更前该看什么"这件事就散了。
 */
export function DisruptionPage() {
  const { t } = useTranslation()
  const [tab, setTab] = useState<Tab>('pdb')
  const clusters = useClusters({ page: 1, size: 100 })
  const list = clusters.data?.items ?? []
  const [cluster, setCluster] = useState('')
  const cid = Number(cluster || list[0]?.id || 0) || null

  return (
    <div className="flex flex-col gap-4 p-5">
      <div className="flex flex-wrap items-end gap-3">
        {(['pdb', 'hpa', 'pool'] as Tab[]).map((x) => (
          <button
            key={x}
            type="button"
            onClick={() => setTab(x)}
            className={`cursor-pointer rounded-[var(--radius)] border px-2.5 py-1 text-[13px] transition-colors duration-150 ${
              tab === x
                ? 'border-primary bg-secondary text-foreground'
                : 'border-border text-muted-foreground hover:bg-secondary'
            }`}
          >
            {t(`disruption:tab.${x}`)}
          </button>
        ))}
        <Field label={t('disruption:cluster')}>
          <Select
            label={t('disruption:cluster')}
            value={cluster || String(list[0]?.id ?? '')}
            onChange={setCluster}
            options={list.map((c) => ({ value: String(c.id), label: clusterLabel(c.displayName, c.name) }))}
          />
        </Field>
        <span className="pb-1.5 text-xs text-muted-foreground">{t(`disruption:hint.${tab}`)}</span>
      </div>

      {tab === 'pdb' ? <PdbView cid={cid} t={t} /> : null}
      {tab === 'hpa' ? <HpaView cid={cid} t={t} /> : null}
      {tab === 'pool' ? <PoolView cid={cid} t={t} /> : null}
    </div>
  )
}

type T = (k: string, p?: Record<string, unknown>) => string

function Shell({
  q,
  empty,
  t,
  children,
}: {
  q: { isPending: boolean; isError: boolean; error: unknown; data?: unknown[] }
  empty: string
  t: T
  children: React.ReactNode
}) {
  if (q.isPending) return <Skeleton className="h-5 w-[40%]" />
  if (q.isError) {
    return (
      <Banner tone="bad">
        <span>{tError(t, toErrorInfo(q.error).messageKey, toErrorInfo(q.error).params)}</span>
        <span className="mt-0.5 block">{toErrorInfo(q.error).detail}</span>
      </Banner>
    )
  }
  // ⚠️ 空要说清楚是「没有配」而不是「没查到」——这两件事的下一步相反
  if ((q.data ?? []).length === 0) {
    return (
      <Banner tone="info">
        <span>{empty}</span>
      </Banner>
    )
  }
  return <>{children}</>
}

function PdbView({ cid, t }: { cid: number | null; t: T }) {
  const q = usePdbs(cid)
  // ⚠️ 这个接口给的是包装对象 {items, blocking, collected}，不是裸数组
  const rows = q.data?.items ?? []
  // 卡住的排最前：这一页存在的理由就是"升级前先看哪些会拦路"
  const sorted = [...rows].sort((a, b) => Number(b.blocking) - Number(a.blocking))
  const blocking = q.data?.blocking ?? rows.filter((r) => r.blocking).length

  // ⚠️ 没采过 ≠ 没有 PDB。前者这一页的结论完全不能信，后者是真的没保护
  if (q.isSuccess && q.data?.collected === false) {
    return (
      <Banner tone="warn">
        <span className="font-medium">{t('disruption:notCollected.title')}</span>
        <span className="mt-0.5 block">{t('disruption:notCollected.body')}</span>
      </Banner>
    )
  }

  return (
    <Shell q={{ ...q, data: rows }} empty={t('disruption:empty.pdb')} t={t}>
      {blocking > 0 ? (
        <Banner tone="warn">
          <span className="font-medium">{t('disruption:blockingCount', { count: blocking })}</span>
          <span className="mt-0.5 block">{t('disruption:blockingHint')}</span>
        </Banner>
      ) : null}
      <div className="overflow-auto rounded-[var(--radius)] border border-border">
        <table className="w-full text-[12px]">
          <thead className="bg-card">
            <tr className="border-b border-border text-left text-[11px] text-muted-foreground">
              <th className="px-2 py-2 font-medium">{t('disruption:col.object')}</th>
              <th className="px-2 py-2 font-medium">{t('disruption:col.policy')}</th>
              <th className="px-2 py-2 font-medium">{t('disruption:col.healthy')}</th>
              <th className="px-2 py-2 font-medium">{t('disruption:col.allowed')}</th>
              <th className="px-2 py-2 font-medium">{t('disruption:col.risk')}</th>
            </tr>
          </thead>
          <tbody>
            {sorted.map((p: Pdb) => (
              <tr
                key={`${p.namespace}/${p.name}`}
                className="border-b border-border/60 last:border-0"
              >
                <td className="px-2 py-1.5 font-mono">
                  {p.namespace}/{p.name}
                </td>
                <td className="px-2 py-1.5 text-muted-foreground">
                  {p.min_available ? `minAvailable=${p.min_available}` : ''}
                  {p.max_unavailable ? `maxUnavailable=${p.max_unavailable}` : ''}
                  {/* selector：这条 PDB 到底罩着哪些 Pod。
                      PDB 挡住 drain 时，「它保护的是什么」决定了下一步是等还是改策略。
                      ⚠️ 空 selector 在 k8s 里意味着**匹配该命名空间下所有 Pod**，
                      是最容易误伤的一种写法，必须显式说出来而不是留白。 */}
                  <div className="truncate text-[11px] text-muted-foreground" title={p.selector}>
                    {p.selector !== ''
                      ? p.selector
                      : <span className="text-warning">{t('disruption:selectorAll')}</span>}
                  </div>
                </td>
                <td className="px-2 py-1.5 tabular">
                  {p.current_healthy}/{p.expected_pods}
                  {/* 🔴 下限要显示出来。
                      「健康 3/3」看不出为什么 allowed=0，而「下限 3」一说就明白：
                      当前健康数已经贴着 k8s 算出来的最低要求，再驱逐一个就破线。
                      这是排「drain 卡住了」时唯一能直接给出答案的数字。 */}
                  <span className="ml-1 text-[11px] text-muted-foreground">
                    {t('disruption:floor', { n: p.desired_healthy })}
                  </span>
                </td>
                <td className="px-2 py-1.5">
                  {/* 余量 0 是**此刻驱逐就会失败**，不是"配置得比较严" */}
                  {p.blocking ? (
                    <Badge tone="bad">{p.disruptions_allowed}</Badge>
                  ) : (
                    <span className="tabular">{p.disruptions_allowed}</span>
                  )}
                </td>
                {/* 后端直接给了结论，原样显示 —— 别让人自己从数字推 */}
                <td className="px-2 py-1.5 text-muted-foreground">{p.risk_note || '—'}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </Shell>
  )
}

function HpaView({ cid, t }: { cid: number | null; t: T }) {
  const q = useHpas(cid)
  const rows = q.data ?? []
  return (
    <Shell q={q} empty={t('disruption:empty.hpa')} t={t}>
      <div className="overflow-auto rounded-[var(--radius)] border border-border">
        <table className="w-full text-[12px]">
          <thead className="bg-card">
            <tr className="border-b border-border text-left text-[11px] text-muted-foreground">
              <th className="px-2 py-2 font-medium">{t('disruption:col.object')}</th>
              <th className="px-2 py-2 font-medium">{t('disruption:col.target')}</th>
              <th className="px-2 py-2 font-medium">{t('disruption:col.replicas')}</th>
            </tr>
          </thead>
          <tbody>
            {rows.map((h: Hpa) => {
              // 贴着上限 = 再有流量就扛不住了，这是要提前知道的
              const atMax = h.max_replicas > 0 && h.current_replicas >= h.max_replicas
              return (
                <tr key={h.id} className="border-b border-border/60 last:border-0">
                  <td className="px-2 py-1.5 font-mono">
                    {h.namespace}/{h.name}
                  </td>
                  <td className="px-2 py-1.5 text-muted-foreground">
                    {h.target_kind}/{h.target_name}
                  </td>
                  <td className="px-2 py-1.5">
                    <span className={`tabular ${atMax ? 'text-warning' : ''}`}>
                      {h.current_replicas}
                    </span>
                    <span className="tabular text-muted-foreground">
                      {' '}
                      ({h.min_replicas}–{h.max_replicas})
                    </span>
                    {atMax ? (
                      <span className="ml-1.5">
                        <Badge tone="warn">{t('disruption:atMax')}</Badge>
                      </span>
                    ) : null}
                  </td>
                </tr>
              )
            })}
          </tbody>
        </table>
      </div>
    </Shell>
  )
}

function PoolView({ cid, t }: { cid: number | null; t: T }) {
  const q = useNodePools(cid)
  const rows = q.data ?? []
  return (
    <Shell q={q} empty={t('disruption:empty.pool')} t={t}>
      <div className="overflow-auto rounded-[var(--radius)] border border-border">
        <table className="w-full text-[12px]">
          <thead className="bg-card">
            <tr className="border-b border-border text-left text-[11px] text-muted-foreground">
              <th className="px-2 py-2 font-medium">{t('disruption:col.pool')}</th>
              <th className="px-2 py-2 font-medium">{t('disruption:col.machine')}</th>
              <th className="px-2 py-2 font-medium">{t('disruption:col.nodes')}</th>
              <th className="px-2 py-2 font-medium">{t('disruption:col.version')}</th>
            </tr>
          </thead>
          <tbody>
            {rows.map((p: NodePool) => (
              <tr key={p.id} className="border-b border-border/60 last:border-0">
                <td className="px-2 py-1.5 font-mono">{p.name}</td>
                <td className="px-2 py-1.5 text-muted-foreground">{p.machine_type || '—'}</td>
                <td className="px-2 py-1.5 tabular">{p.node_count}</td>
                {/* 版本参差是升级前最该发现的事，原样显示不做加工 */}
                <td className="px-2 py-1.5 font-mono text-muted-foreground">{p.version || '—'}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </Shell>
  )
}
