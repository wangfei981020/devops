import { tError } from '@ops/i18n'
import { toErrorInfo } from '@ops/api'
import { Badge, Banner, Button, Dialog, Skeleton } from '@ops/ui'
import { useState } from 'react'
import { useGateways, useHttpRoutes, useIngresses, useVirtualServices } from './ingress.js'

type TFn = (k: string, o?: Record<string, unknown>) => string
type View = 'ing' | 'vs' | 'gw' | 'route'

/**
 * 入口与域名。
 *
 * ⚠️ 对外域名不在 Service 上——Istio 环境里它在 VirtualService 的 hosts，
 * Gateway API 环境里在 HTTPRoute 的 hostnames。只看 Service 的话，
 * 「这个域名打到哪」永远查不出来。
 */
export function IngressDialog({
  clusterID,
  onClose,
  t,
}: {
  clusterID: number
  onClose: () => void
  t: TFn
}) {
  // ⚠️ 默认落在原生 Ingress 而不是 VirtualService：
  // 不是每个集群都上了 Istio，而 Ingress 是所有集群都有的那一档
  const [view, setView] = useState<View>('ing')
  const views: { key: View; label: string }[] = [
    { key: 'ing', label: t('services:ingress.ing') },
    { key: 'vs', label: t('services:ingress.vs') },
    { key: 'gw', label: t('services:ingress.gw') },
    { key: 'route', label: t('services:ingress.route') },
  ]

  return (
    <Dialog
      open
      onClose={onClose}
      title={t('services:ingress.title')}
      description={t('services:ingress.desc')}
      closeLabel={t('common:action.close')}
      width={1120}
      footer={
        <Button size="sm" onClick={onClose}>
          {t('common:action.close')}
        </Button>
      }
    >
      <div className="-mx-4 -mt-1 flex flex-wrap gap-1.5 border-b border-border px-4 pb-2.5">
        {views.map((v) => (
          <Button
            key={v.key}
            size="sm"
            variant={view === v.key ? 'primary' : undefined}
            onClick={() => setView(v.key)}
          >
            {v.label}
          </Button>
        ))}
      </div>
      <div className="-mx-4">
        {view === 'ing' ? <IngView cid={clusterID} t={t} /> : null}
        {view === 'vs' ? <VsView cid={clusterID} t={t} /> : null}
        {view === 'gw' ? <GwView cid={clusterID} t={t} /> : null}
        {view === 'route' ? <RouteView cid={clusterID} t={t} /> : null}
      </div>
    </Dialog>
  )
}

const Loading = () => (
  <div className="flex flex-col gap-2 px-4 py-3">
    <Skeleton className="h-4 w-[60%]" />
    <Skeleton className="h-4 w-[40%]" />
  </div>
)

function Failed({ e, t }: { e: unknown; t: TFn }) {
  const n = toErrorInfo(e)
  return (
    <div className="px-4 py-3">
      <Banner tone="bad">
        <span className="font-medium">{t('services:ingress.loadFailed')}</span>
        <span className="mt-0.5 block">{n.detail || tError(t, n.messageKey, n.params)}</span>
      </Banner>
    </div>
  )
}

/** 逗号串拆成标签。空串要变成空数组，否则会渲染出一个空标签。 */
const chips = (v?: string) =>
  (v ?? '')
    .split(',')
    .map((x) => x.trim())
    .filter(Boolean)

/** 原生 Ingress。形态与 VS 一致：域名是主角，后端服务次要 */
function IngView({ cid, t }: { cid: number; t: TFn }) {
  const q = useIngresses(cid, true)
  if (q.isPending) return <Loading />
  if (q.isError) return <Failed e={q.error} t={t} />
  const rows = q.data ?? []
  if (rows.length === 0)
    return (
      <p className="px-4 py-4 text-[13px] text-muted-foreground">{t('services:ingress.noIng')}</p>
    )
  return (
    <div className="max-h-[52vh] overflow-auto px-4 py-3">
      <div className="flex flex-col gap-2">
        {rows.map((v) => (
          <div key={v.id} className="rounded-[var(--radius)] border border-border p-2.5">
            <div className="flex flex-wrap items-center gap-2">
              <span className="font-mono text-xs">
                {v.namespace}/{v.name}
              </span>
              {/* 配没配 TLS 直接决定这个域名是不是明文对外 */}
              {chips(v.tls).length > 0 ? (
                <Badge tone="ok">TLS</Badge>
              ) : (
                <Badge tone="warn">{t('services:ingress.noTls')}</Badge>
              )}
            </div>
            <div className="mt-1 flex flex-wrap gap-1.5">
              {chips(v.hosts).length === 0 ? (
                // 没有 host 的 Ingress 是按路径匹配的默认后端，不是"没配好"
                <span className="text-[11px] text-muted-foreground">
                  {t('services:ingress.noHost')}
                </span>
              ) : (
                chips(v.hosts).map((h) => (
                  <span
                    key={h}
                    className="rounded-[var(--radius)] border border-border px-1.5 py-0.5 font-mono text-[11px] text-foreground"
                  >
                    {h}
                  </span>
                ))
              )}
            </div>
            {chips(v.svc_names).length > 0 ? (
              <p className="mt-1 text-[11px] text-muted-foreground">
                → {chips(v.svc_names).join(', ')}
              </p>
            ) : null}
          </div>
        ))}
      </div>
    </div>
  )
}

function VsView({ cid, t }: { cid: number; t: TFn }) {
  const q = useVirtualServices(cid, true)
  if (q.isPending) return <Loading />
  if (q.isError) return <Failed e={q.error} t={t} />
  const rows = q.data ?? []
  if (rows.length === 0)
    return <p className="px-4 py-4 text-[13px] text-muted-foreground">{t('services:ingress.noVs')}</p>

  return (
    <div className="max-h-[52vh] overflow-auto px-4 py-3">
      <div className="flex flex-col gap-2">
        {rows.map((v) => (
          <div key={v.id} className="rounded-[var(--radius)] border border-border p-2.5">
            <div className="flex flex-wrap items-center gap-2">
              <span className="font-mono text-xs">{v.namespace}/{v.name}</span>
            </div>
            {/* 域名是这一页的主角，单独一行放大显示 */}
            <div className="mt-1 flex flex-wrap gap-1.5">
              {chips(v.hosts).map((h) => (
                <span key={h} className="rounded border border-border px-1.5 py-0.5 font-mono text-[11px] text-foreground">
                  {h}
                </span>
              ))}
            </div>
            <div className="mt-1 flex flex-wrap gap-3 text-[11px] text-muted-foreground">
              {v.gateways ? <span>{t('services:ingress.viaGw', { gw: v.gateways })}</span> : null}
              {v.backends ? <span>→ {v.backends}</span> : null}
            </div>
          </div>
        ))}
      </div>
    </div>
  )
}

function GwView({ cid, t }: { cid: number; t: TFn }) {
  const q = useGateways(cid, true)
  if (q.isPending) return <Loading />
  if (q.isError) return <Failed e={q.error} t={t} />
  const rows = q.data ?? []
  if (rows.length === 0)
    return <p className="px-4 py-4 text-[13px] text-muted-foreground">{t('services:ingress.noGw')}</p>

  return (
    <div className="max-h-[52vh] overflow-auto px-4 py-3">
      <div className="flex flex-col gap-2">
        {rows.map((g) => (
          <div key={g.id} className="rounded-[var(--radius)] border border-border p-2.5">
            <div className="flex flex-wrap items-center gap-2">
              <span className="font-mono text-xs">{g.namespace}/{g.name}</span>
              {/* Istio 与 Gateway API 是两套东西，标出来免得混着看 */}
              <Badge tone="mute">{g.api_group?.includes('istio') ? 'Istio' : 'Gateway API'}</Badge>
              {g.addresses ? <span className="font-mono text-[11px]">{g.addresses}</span> : null}
            </div>
            <p className="mt-1 font-mono text-[11px] break-all text-muted-foreground">{g.listeners}</p>
            {g.tls_secrets ? (
              <p className="mt-0.5 text-[11px] text-muted-foreground">TLS: {g.tls_secrets}</p>
            ) : null}
          </div>
        ))}
      </div>
    </div>
  )
}

function RouteView({ cid, t }: { cid: number; t: TFn }) {
  const q = useHttpRoutes(cid, true)
  if (q.isPending) return <Loading />
  if (q.isError) return <Failed e={q.error} t={t} />
  const rows = q.data ?? []
  if (rows.length === 0)
    return <p className="px-4 py-4 text-[13px] text-muted-foreground">{t('services:ingress.noRoute')}</p>

  return (
    <div className="max-h-[52vh] overflow-auto px-4 py-3">
      <div className="flex flex-col gap-2">
        {rows.map((r) => (
          <div key={r.id} className="rounded-[var(--radius)] border border-border p-2.5">
            <span className="font-mono text-xs">{r.namespace}/{r.name}</span>
            <div className="mt-1 flex flex-wrap gap-1.5">
              {chips(r.hostnames).map((h) => (
                <span key={h} className="rounded border border-border px-1.5 py-0.5 font-mono text-[11px]">
                  {h}
                </span>
              ))}
            </div>
            <div className="mt-1 text-[11px] text-muted-foreground">
              {r.parents ? `${t('services:ingress.parent')}: ${r.parents}` : null}
              {r.backends ? ` → ${r.backends}` : null}
            </div>
          </div>
        ))}
      </div>
    </div>
  )
}
