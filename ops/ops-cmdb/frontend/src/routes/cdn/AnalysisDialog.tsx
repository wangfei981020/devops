import { tError } from '@ops/i18n'
import { toErrorInfo } from '@ops/api'
import { Badge, Banner, Button, Dialog, Skeleton } from '@ops/ui'
import { useState } from 'react'
import {
  type Finding,
  useCdnCertificates,
  useCdnDomainCheck,
  useCdnRuleAnalysis,
  useCdnRules,
} from './analysis.js'

type TFn = (k: string, o?: Record<string, unknown>) => string
type View = 'rules' | 'analysis' | 'certs' | 'domains'

const sevTone = (s?: string) => (s === 'high' ? 'bad' : s === 'medium' ? 'warn' : 'mute')

/**
 * CDN 分析：规则 / 规则体检 / 边缘证书 / 域名走向。
 *
 * ⚠️ 全部读的是**上次同步的快照**，不是实时 Cloudflare 状态。
 * 改完 CF 配置要先同步再看——老版上因为这个栽过（看到的 403 是旧字符串）。
 */
export function CdnAnalysisDialog({ onClose, t }: { onClose: () => void; t: TFn }) {
  const [view, setView] = useState<View>('analysis')
  const views: { key: View; label: string }[] = [
    { key: 'analysis', label: t('cdn:analysis.ruleCheck') },
    { key: 'rules', label: t('cdn:analysis.rules') },
    { key: 'certs', label: t('cdn:analysis.certs') },
    { key: 'domains', label: t('cdn:analysis.domains') },
  ]

  return (
    <Dialog
      open
      onClose={onClose}
      title={t('cdn:analysis.title')}
      description={t('cdn:analysis.snapshotNote')}
      closeLabel={t('common:action.close')}
      width={1140}
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
        {view === 'analysis' ? <FindingsView q={useCdnRuleAnalysis()} pick="findings" t={t} /> : null}
        {view === 'rules' ? <RulesView t={t} /> : null}
        {view === 'certs' ? <CertsView t={t} /> : null}
        {view === 'domains' ? <FindingsView q={useCdnDomainCheck()} pick="items" t={t} /> : null}
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
        <span className="font-medium">{t('cdn:analysis.loadFailed')}</span>
        <span className="mt-0.5 block">{n.detail || tError(t, n.messageKey, n.params)}</span>
      </Banner>
    </div>
  )
}

/** 规则体检与域名走向共用一套 finding 形状，只是取的字段名不同。 */
function FindingsView({
  q,
  pick,
  t,
}: {
  q: { isPending: boolean; isError: boolean; error?: unknown; data?: Record<string, unknown> }
  pick: 'findings' | 'items'
  t: TFn
}) {
  if (q.isPending) return <Loading />
  if (q.isError) return <Failed e={q.error} t={t} />
  const rows = (q.data?.[pick] as Finding[] | undefined) ?? []
  if (rows.length === 0)
    return <p className="px-4 py-4 text-[13px] text-muted-foreground">{t('cdn:analysis.noFindings')}</p>

  return (
    <div className="max-h-[52vh] overflow-auto px-4 py-3">
      <div className="flex flex-col gap-2">
        {rows.map((f, i) => (
          <div key={`${f.fqdn ?? f.zone}-${i}`} className="rounded-[var(--radius)] border border-border p-2.5">
            <div className="flex flex-wrap items-center gap-2">
              <Badge tone={sevTone(f.severity)}>{f.severity}</Badge>
              <span className="font-mono text-xs">{f.fqdn || f.zone}</span>
              {f.type ? <Badge tone="mute">{f.type}</Badge> : null}
              {/* ⚠️ 没走 CDN 是真问题：以为挂了 CDN 实际直连源站，WAF/限流/缓存全都没生效 */}
              {f.via_cdn === false ? <Badge tone="warn">{t('cdn:analysis.notViaCdn')}</Badge> : null}
            </div>
            <p className="mt-1 text-[13px] text-foreground">{f.issue}</p>
            {f.content ? (
              <p className="mt-0.5 font-mono text-xs break-all text-muted-foreground">{f.content}</p>
            ) : null}
            {f.action ? <p className="mt-0.5 text-xs text-muted-foreground">{f.action}</p> : null}
          </div>
        ))}
      </div>
    </div>
  )
}

function RulesView({ t }: { t: TFn }) {
  const q = useCdnRules()
  if (q.isPending) return <Loading />
  if (q.isError) return <Failed e={q.error} t={t} />
  const rows = q.data?.rules ?? []
  if (rows.length === 0)
    return <p className="px-4 py-4 text-[13px] text-muted-foreground">{t('cdn:analysis.noRules')}</p>

  return (
    <div className="max-h-[52vh] overflow-auto px-4 py-3">
      <div className="flex flex-col gap-2">
        {rows.map((r, i) => (
          <div key={`${r.rule_id}-${i}`} className="rounded-[var(--radius)] border border-border p-2.5">
            <div className="flex flex-wrap items-center gap-2">
              <span className="text-[13px] font-medium text-foreground">{r.name || r.rule_id}</span>
              <Badge tone="mute">{r.zone_name}</Badge>
              <Badge tone={r.kind === 'block' ? 'bad' : 'info'}>{r.kind}</Badge>
              {/* 停用的规则要显眼：它在列表里和生效的长得一样，但什么都不做 */}
              {r.status !== 'enabled' ? <Badge tone="warn">{r.status}</Badge> : null}
            </div>
            <p className="mt-1 font-mono text-[11px] break-all text-muted-foreground">{r.expression}</p>
            <p className="mt-0.5 font-mono text-[11px] break-all text-muted-foreground">{r.actions}</p>
          </div>
        ))}
      </div>
    </div>
  )
}

function CertsView({ t }: { t: TFn }) {
  const q = useCdnCertificates()
  if (q.isPending) return <Loading />
  if (q.isError) return <Failed e={q.error} t={t} />
  const rows = q.data?.certificates ?? []
  if (rows.length === 0)
    return <p className="px-4 py-4 text-[13px] text-muted-foreground">{t('cdn:analysis.noCerts')}</p>

  return (
    <div className="max-h-[52vh] overflow-auto">
      <table className="w-full text-[13px]">
        <thead className="sticky top-0 z-10 bg-card">
          <tr className="border-b border-border text-left text-xs text-muted-foreground">
            <th className="px-4 py-2 font-medium">{t('cdn:analysis.col.hosts')}</th>
            <th className="px-4 py-2 font-medium">{t('cdn:analysis.col.issuer')}</th>
            <th className="px-4 py-2 font-medium">{t('cdn:analysis.col.expires')}</th>
            <th className="px-4 py-2 font-medium">{t('cdn:analysis.col.status')}</th>
          </tr>
        </thead>
        <tbody>
          {rows.map((c, i) => (
            <tr key={`${c.pack_id}-${i}`} className="border-b border-border last:border-0">
              <td className="px-4 py-1.5 font-mono text-xs break-all">{c.hosts}</td>
              <td className="px-4 py-1.5 text-xs text-muted-foreground">{c.issuer}</td>
              <td className="px-4 py-1.5 text-xs">
                {c.expires_on}
                {/* 负数天要说成"已过期 N 天"，显示成"还剩 -10 天"没人看得懂 */}
                <span className="ml-1.5 text-[11px] text-muted-foreground">
                  {(c.days_left ?? 0) < 0
                    ? t('cdn:analysis.expiredDays', { n: Math.abs(c.days_left ?? 0) })
                    : t('cdn:analysis.daysLeft', { n: c.days_left ?? 0 })}
                </span>
              </td>
              <td className="px-4 py-1.5">
                <Badge tone={c.issue ? 'bad' : 'ok'}>{c.issue || c.status}</Badge>
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  )
}
