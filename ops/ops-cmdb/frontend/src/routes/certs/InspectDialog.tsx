import { probeNoteText } from './queries.js'
import { toErrorInfo } from '@ops/api'
import { Badge, Banner, Button, Dialog, Skeleton } from '@ops/ui'
import { useState } from 'react'
import { type InspectItem, useCertInspect } from './inspect.js'

type TFn = (k: string, o?: Record<string, unknown>) => string

/**
 * 证书到期巡检。
 *
 * ⚠️ 排序按剩余天数升序：这一页存在的唯一理由就是「哪张先死」，
 * 按名字或按来源排都会把最紧急的那张埋在中间。
 *
 * # ⚠️ 这一页原来有三个叠在一起的问题
 *
 * 1. **排序压根没生效**：排序键读的是 `r.days_left`，而后端从来没有这个字段 ——
 *    `(a.days_left ?? 9999) - (b.days_left ?? 9999)` 恒等于 0。
 *    剩余天数那一段也因此永远不显示。到期日就在 `expiry_at` 里，前端自己算就行。
 * 2. **来源只判两种**（online / 其它=台账），而后端给三种（online / domain / acme）——
 *    域名注册到期和我方签发的证书被显示成同一个「台账」。
 * 3. **700 行「—」**：探测从没跑过时，到期日和检测结果两列全空，
 *    而「所有证书都没有到期日」是不可能的事（P0-4）。
 *    现在顶层用 `probe_note` 说清楚是"没探过"还是"探失败了"，以及去哪儿处理。
 */
export function CertInspectDialog({ onClose, t }: { onClose: () => void; t: TFn }) {
  const q = useCertInspect()
  // 「只看有问题的」：700 条里真正要处理的通常是少数
  const [onlyIssues, setOnlyIssues] = useState(false)

  const all = q.data?.items ?? []
  const rows = [...all]
    .filter((r) => (onlyIssues ? isIssue(r) : true))
    // ⚠️ 剩余天数**前端算**（后端只给 expiry_at）。
    // 算不出来的（没到期日）排到最后而不是当成 0 天 ——
    // 当成 0 会把"不知道"顶到"今天就过期"的位置，制造一堆假紧急
    .sort((a, b) => (daysLeft(a) ?? 99999) - (daysLeft(b) ?? 99999))

  return (
    <Dialog
      open
      onClose={onClose}
      title={t('certs:inspect.title')}
      description={t('certs:inspect.desc')}
      closeLabel={t('common:action.close')}
      width={1040}
      footer={
        <Button size="sm" onClick={onClose}>
          {t('common:action.close')}
        </Button>
      }
    >
      {q.isPending ? (
        <div className="flex flex-col gap-2 py-3">
          <Skeleton className="h-4 w-[60%]" />
          <Skeleton className="h-4 w-[40%]" />
        </div>
      ) : q.isError ? (
        <Banner tone="bad">
          <span className="font-medium">{t('certs:inspect.loadFailed')}</span>
          <span className="mt-0.5 block">
            {toErrorInfo(q.error).detail || t(toErrorInfo(q.error).messageKey)}
          </span>
        </Banner>
      ) : (
        <div className="flex flex-col gap-2.5">
          {/*
            ⚠️ 「这批数据能不能信」要放在最上面，在表格之前。
            放在表格下面或者不放，人会先读 700 行「—」并自己编一个解释出来。
          */}
          {q.data?.probeNoteKey ? (
            <Banner tone={q.data.probeState === 'never' ? 'bad' : 'warn'}>
              <span className="font-medium">{t('certs:inspect.probeTitle')}</span>
              <span className="mt-0.5 block">
                {probeNoteText(t, q.data.probeNoteKey, q.data.probeNoteParams)}
              </span>
            </Banner>
          ) : null}

          {/* 内网目标要单独说清不是失败，否则「检测失败 N」会虚高，然后没人再信它 */}
          {(q.data?.internalTargets ?? 0) > 0 ? (
            <p className="text-xs text-muted-foreground">
              {t('certs:inspect.internalNote', { count: q.data?.internalTargets })}
            </p>
          ) : null}

          {all.length === 0 ? (
            <p className="py-4 text-[13px] text-muted-foreground">{t('certs:inspect.none')}</p>
          ) : (
            <>
              <div className="flex flex-wrap items-center gap-2">
                <button
                  type="button"
                  onClick={() => setOnlyIssues((v) => !v)}
                  className={`cursor-pointer rounded-[var(--radius)] border px-2.5 py-1 text-xs transition-colors duration-150 ${
                    onlyIssues
                      ? 'border-warning bg-warning/10 text-warning'
                      : 'border-border text-muted-foreground hover:bg-secondary'
                  }`}
                >
                  {onlyIssues ? t('certs:inspect.allRows') : t('certs:inspect.onlyIssues')}
                </button>
                <span className="text-xs text-muted-foreground">
                  {t('certs:inspect.rowCount', { shown: rows.length, total: all.length })}
                </span>
              </div>

              <div className="-mx-4 max-h-[52vh] overflow-auto">
                <table className="w-full text-[13px]">
                  <thead className="sticky top-0 z-10 bg-card">
                    <tr className="border-b border-border text-left text-xs text-muted-foreground">
                      <th className="px-4 py-2 font-medium">{t('certs:inspect.col.fqdn')}</th>
                      <th className="px-4 py-2 font-medium">{t('certs:inspect.col.source')}</th>
                      <th className="px-4 py-2 font-medium">{t('certs:inspect.col.expiry')}</th>
                      <th className="px-4 py-2 font-medium">{t('certs:inspect.col.msg')}</th>
                    </tr>
                  </thead>
                  <tbody>
                    {rows.map((r, i) => (
                      <Row key={`${r.kind}-${r.fqdn}-${i}`} r={r} t={t} />
                    ))}
                  </tbody>
                </table>
              </div>
            </>
          )}
        </div>
      )}
    </Dialog>
  )
}

/** 剩余天数。⚠️ 没有到期日时返回 undefined，**不要返回 0** */
function daysLeft(r: InspectItem): number | undefined {
  if (!r.expiry_at) return undefined
  const ms = new Date(`${r.expiry_at}T00:00:00`).getTime()
  if (Number.isNaN(ms)) return undefined
  return Math.floor((ms - Date.now()) / 86400000)
}

/** 要处理的：已过期 / 30 天内到期 / 探测失败 / 从没探过。内网目标不算 */
function isIssue(r: InspectItem) {
  if (r.ignored) return false
  if (r.scope === 'internal') return false
  if (r.probe_state === 'never' || r.probe_state === 'failed') return true
  const d = daysLeft(r)
  return d != null && d <= 30
}

function Row({ r, t }: { r: InspectItem; t: TFn }) {
  const d = daysLeft(r)
  return (
    <tr className="border-b border-border last:border-0">
      <td className="px-4 py-1.5 font-mono text-xs break-all">
        {r.fqdn}
        {/* 主域名已下线的证书不用续期 —— 这是"可以不管"的唯一合法理由，要显示出来 */}
        {r.domain_status && r.domain_status !== 'active' ? (
          <span className="ml-1.5 text-[11px] text-muted-foreground">{r.domain_status}</span>
        ) : null}
      </td>
      <td className="px-4 py-1.5">
        {/* 三种来源要分开：域名注册到期、我方签发、线上实测，处置方式完全不同 */}
        <Badge tone="mute">{t(`certs:inspect.kind.${r.kind}`, { defaultValue: r.kind ?? '—' })}</Badge>
        {r.ignored ? <Badge tone="warn">{t('certs:inspect.ignored')}</Badge> : null}
      </td>
      <td className="px-4 py-1.5 text-xs">
        {/*
          ⚠️ 到期日为空时**说明为什么**，不要只给一个「—」。
          「从没探过」和「探了失败」的下一步完全不同，而「—」把两者说成了同一件事。
        */}
        {r.expiry_at ? (
          <>
            {r.expiry_at}
            {d != null ? (
              <span
                className={`ml-1.5 text-[11px] ${
                  d < 0 ? 'text-danger' : d <= 30 ? 'text-warning' : 'text-muted-foreground'
                }`}
              >
                {d < 0
                  ? t('certs:inspect.expiredDays', { n: Math.abs(d) })
                  : t('certs:inspect.daysLeft', { n: d })}
              </span>
            ) : null}
          </>
        ) : r.probe_state === 'never' ? (
          <span className="text-warning">{t('certs:inspect.neverProbed')}</span>
        ) : r.probe_state === 'failed' ? (
          <span className="text-danger">{t('certs:inspect.probeFailed')}</span>
        ) : (
          <span className="text-muted-foreground">—</span>
        )}
      </td>
      <td className="px-4 py-1.5 text-xs">
        {/*
          失败原因原样显示：「解析不到」和「证书过期」要处置的是两件事。
          ⚠️ scope=internal 用中性色：内网地址被公网巡检器探测连不上是必然的，
          标红会让人去修一个不存在的问题
        */}
        {r.reason_label ? (
          <span className={r.scope === 'internal' ? 'text-muted-foreground' : 'text-danger'}>
            {r.reason_label}
          </span>
        ) : null}
        <span className="block text-muted-foreground" title={r.check_msg}>
          {r.check_msg || (r.reason_label ? '' : '—')}
        </span>
        {/* 解析目标地址：内网判定的依据，人要能核对 */}
        {r.origin_ip ? (
          <span className="block font-mono text-[11px] text-muted-foreground">{r.origin_ip}</span>
        ) : null}
      </td>
    </tr>
  )
}
