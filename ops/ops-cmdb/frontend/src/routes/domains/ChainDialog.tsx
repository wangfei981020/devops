import { toErrorInfo } from '@ops/api'
import { shouldRetry } from '@ops/api'
import { useTranslation } from '@ops/i18n'
import { Badge, Banner, Button, Dialog, Skeleton } from '@ops/ui'
import { useQuery } from '@tanstack/react-query'
import { apiGet } from '../../lib/fetchJson.js'

/**
 * 域名全链路。
 *
 * # 它回答的问题
 *
 * 「这个域名背后到底跑着什么」—— 域名出事时的第一个问题，
 * 而回答它现在要跨四五个页面：解析记录看回源、CDN 页看边缘证书、
 * 服务与入口看 Ingress、Pod 页看副本、节点页看落在哪。
 *
 * 后端 `/api/k8s/topology?domain=` 一直把整条链串好了
 * （域名 → CDN/证书 → Ingress/HTTPRoute/VirtualService → Service → Pod → 节点 → 集群），
 * 只是从来没有页面调过它（OPSCMDB-023 第一档）。
 *
 * # ⚠️ 两处特别容易读错，后端注释里都点明了
 *
 * 1. **CDN 边缘证书和 Gateway 的 TLS 证书是两张不同的证书**，
 *    到期时间互相独立 —— 边缘没过期不代表源站没过期。所以分开显示。
 * 2. 链路为空**不等于**这个域名没在用：可能它根本不走 K8s（纯 CDN 回源到 VM），
 *    也可能 Ingress 的 host 写法没匹配上。不说清楚会被读成"这域名没人用"。
 */

interface ChainPod {
  pod?: string
  node?: string
  workload?: string
}
interface ChainService {
  service?: string
  pods?: ChainPod[]
  workloads?: string[]
  nodes?: string[]
}
interface Chain {
  cluster?: string
  cluster_id?: number
  namespace?: string
  kind?: string
  name?: string
  tls?: string
  services?: ChainService[]
}
interface TopologyResult {
  domain?: string
  edge?: { cdn?: string; cname?: string; origin_ip?: string; cert_expiry?: string } | null
  cdn?: Record<string, unknown> | null
  cdn_cert?: { expires_on?: string; issuer?: string; status?: string } | null
  domain_info?: { project?: string; env?: string; module?: string } | null
  chains?: Chain[]
}

function useTopology(domain: string) {
  return useQuery({
    queryKey: ['domain-topology', domain],
    queryFn: () => apiGet<TopologyResult>(`/api/k8s/topology?domain=${encodeURIComponent(domain)}`),
    retry: shouldRetry,
  })
}

export function DomainChainDialog({ domain, onClose }: { domain: string; onClose: () => void }) {
  const { t } = useTranslation()
  const q = useTopology(domain)
  const d = q.data

  return (
    <Dialog
      open
      onClose={onClose}
      title={t('domains:chain.title', { domain })}
      description={t('domains:chain.desc')}
      closeLabel={t('common:action.close')}
      width={900}
      footer={
        <Button size="sm" onClick={onClose}>
          {t('common:action.close')}
        </Button>
      }
    >
      {q.isPending ? (
        <Skeleton className="h-24 w-full" />
      ) : q.isError ? (
        <Banner tone="bad">
          <span className="break-all">
            {toErrorInfo(q.error).detail || t(toErrorInfo(q.error).messageKey)}
          </span>
        </Banner>
      ) : (
        <div className="flex flex-col gap-3">
          <Edge d={d} t={t} />
          <Chains chains={d?.chains ?? []} t={t} />
        </div>
      )}
    </Dialog>
  )
}

type TFn = (k: string, o?: Record<string, unknown>) => string

/** 边缘接入：CDN / 回源 / 源站 / 边缘证书 */
function Edge({ d, t }: { d?: TopologyResult; t: TFn }) {
  const e = d?.edge
  const info = d?.domain_info
  return (
    <section className="rounded-[var(--radius)] border border-border p-3">
      <div className="flex flex-wrap items-baseline gap-2">
        <span className="text-[13px] font-medium text-foreground">{t('domains:chain.edge')}</span>
        {info?.project ? <Badge tone="mute">{info.project}</Badge> : null}
        {info?.env ? <Badge tone="mute">{info.env}</Badge> : null}
        {info?.module ? <Badge tone="mute">{info.module}</Badge> : null}
      </div>
      <dl className="mt-1.5 flex flex-col gap-1 text-xs">
        <Row label={t('domains:chain.cdn')} v={e?.cdn} t={t} />
        <Row label={t('domains:chain.cname')} v={e?.cname} t={t} />
        <Row label={t('domains:chain.origin')} v={e?.origin_ip} t={t} />
        {/* 🔴 两张证书必须分开列。
            CDN 边缘证书和 Gateway 的 TLS Secret 是**两张不同的证书**，
            到期时间互相独立 —— 边缘没过期不代表源站没过期。
            合成一行显示，会让人看一个日期就以为整条链都安全 */}
        <Row label={t('domains:chain.originCert')} v={e?.cert_expiry} t={t} />
        <Row
          label={t('domains:chain.edgeCert')}
          v={d?.cdn_cert?.expires_on}
          t={t}
          extra={d?.cdn_cert?.issuer}
        />
      </dl>
    </section>
  )
}

/**
 * 一行事实。
 *
 * ⚠️ 空值渲染成「未登记」而不是留白 —— 留白会被读成"这一项不适用"，
 * 而它实际含义是"我们没有这条信息"。
 */
function Row({ label, v, t, extra }: { label: string; v?: string; t: TFn; extra?: string }) {
  return (
    <div className="flex gap-2">
      <dt className="w-[120px] shrink-0 text-muted-foreground">{label}</dt>
      <dd className="min-w-0 flex-1 break-all">
        {v ? (
          <>
            <span className="font-mono text-foreground">{v}</span>
            {extra ? <span className="ml-1.5 text-muted-foreground">{extra}</span> : null}
          </>
        ) : (
          <span className="text-muted-foreground">{t('domains:chain.notRecorded')}</span>
        )}
      </dd>
    </div>
  )
}

function Chains({ chains, t }: { chains: Chain[]; t: TFn }) {
  if (chains.length === 0) {
    // 🔴 空链路 ≠ 这个域名没人用。两种常见原因都要说，否则会被读成"没人用"，
    //	而那正是会被拿去下线的那个域名
    return (
      <Banner tone="warn">
        <span className="font-medium">{t('domains:chain.noChain')}</span>
        <span className="mt-0.5 block">{t('domains:chain.noChainHint')}</span>
      </Banner>
    )
  }
  return (
    <div className="flex flex-col gap-2">
      {chains.map((c) => (
        <section
          key={`${c.cluster_id}-${c.namespace}-${c.name}`}
          className="rounded-[var(--radius)] border border-border p-3"
        >
          <div className="flex flex-wrap items-baseline gap-2 text-xs">
            <Badge tone="info">{c.kind}</Badge>
            <span className="font-mono text-foreground">
              {c.namespace}/{c.name}
            </span>
            <span className="text-muted-foreground">{c.cluster}</span>
            {/* TLS 为空 = 这条入口没配证书（纯 HTTP），是个值得看见的事实 */}
            {c.tls ? (
              <Badge tone="mute">TLS {c.tls}</Badge>
            ) : (
              <Badge tone="warn">{t('domains:chain.noTls')}</Badge>
            )}
          </div>

          {(c.services ?? []).length === 0 ? (
            <p className="mt-1 text-xs text-warning">{t('domains:chain.noService')}</p>
          ) : (
            <ul className="mt-1.5 flex flex-col gap-1.5">
              {(c.services ?? []).map((s) => (
                <li key={s.service} className="text-xs">
                  <span className="font-mono text-foreground">{s.service}</span>
                  {(s.pods ?? []).length === 0 ? (
                    // 🔴 Service 存在但没有 Pod = 打不通。这是整条链里最要紧的一档
                    <span className="ml-2 text-danger">{t('domains:chain.noPod')}</span>
                  ) : (
                    <span className="ml-2 text-muted-foreground">
                      {t('domains:chain.pods', {
                        n: (s.pods ?? []).length,
                        nodes: [...new Set((s.pods ?? []).map((p) => p.node).filter(Boolean))].length,
                      })}
                    </span>
                  )}
                  {(s.pods ?? []).length > 0 ? (
                    <div className="mt-0.5 flex flex-wrap gap-1">
                      {(s.pods ?? []).slice(0, 12).map((p) => (
                        <span
                          key={p.pod}
                          title={`${p.workload ?? ''} @ ${p.node ?? ''}`}
                          className="rounded border border-border px-1.5 py-0.5 font-mono text-[11px] text-muted-foreground"
                        >
                          {p.pod}
                        </span>
                      ))}
                      {(s.pods ?? []).length > 12 ? (
                        <span className="px-1 py-0.5 text-[11px] text-muted-foreground">
                          {t('domains:chain.morePods', { n: (s.pods ?? []).length - 12 })}
                        </span>
                      ) : null}
                    </div>
                  ) : null}
                </li>
              ))}
            </ul>
          )}
        </section>
      ))}
    </div>
  )
}
