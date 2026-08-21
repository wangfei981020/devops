import { toErrorInfo } from '@ops/api'
import { tError, useTranslation } from '@ops/i18n'
import { Badge, Banner, Dialog, Field, Select, TextArea } from '@ops/ui'
import { useState } from 'react'
import { WriteButton } from '../../components/WriteButton.js'
import {
  type RenewItem,
  type RenewPreview,
  useExecuteRenew,
  usePreviewRenew,
  useRenewJob,
} from './renew.js'

// 执行续费要 manage_domains；预览只要菜单权限（见后端 permExactRules）
const PERM = 'cmdb:manage_domains'

/**
 * 批量续费。**两步走，不能跳过第一步。**
 *
 * 界面上刻意做成"预览 → 确认"两个阶段，而不是一个带确认框的按钮：
 * 续费是非幂等的真扣费操作，用户必须先看到**具体续哪几个、一共多少钱**，
 * 才谈得上确认。一个只写着"确认续费 8 个域名？"的弹窗不构成确认 ——
 * 它没告诉人要花多少钱。
 */
export function RenewDialog({ onClose }: { onClose: () => void }) {
  const { t } = useTranslation()
  const [text, setText] = useState('')
  const [period, setPeriod] = useState(1)
  const preview = usePreviewRenew()
  const exec = useExecuteRenew()
  const [jobID, setJobID] = useState<string | null>(null)
  const job = useRenewJob(jobID)

  const p = preview.data
  // 预览之后改了域名清单/年数，之前那份报价就作废了 —— 必须重新预览。
  // 不清掉的话，用户会拿着旧报价去确认一个新清单
  const invalidate = () => preview.reset()

  const running = !!jobID

  return (
    <Dialog
      open
      onClose={onClose}
      title={t('domains:renew.title')}
      description={t('domains:renew.desc')}
      closeLabel={t('common:action.close')}
      width={720}
      footer={
        running ? (
          <WriteButton perm={PERM} variant="primary" size="sm" onClick={onClose}>
            {t('common:action.close')}
          </WriteButton>
        ) : (
          <>
            <WriteButton perm={PERM} size="sm" onClick={onClose}>
              {t('common:action.cancel')}
            </WriteButton>
            {!p ? (
              <WriteButton
                perm={PERM}
                variant="primary"
                size="sm"
                loading={preview.isPending}
                blockedReason={text.trim() ? undefined : t('domains:renew.needDomains')}
                onClick={() => preview.mutate({ domains: text, period })}
              >
                {t('domains:renew.preview')}
              </WriteButton>
            ) : (
              <WriteButton
                perm={PERM}
                variant="danger"
                size="sm"
                loading={exec.isPending}
                blockedReason={p.renewable > 0 ? undefined : t('domains:renew.nothingToRenew')}
                onClick={() =>
                  exec.mutate(
                    {
                      domains: text,
                      period,
                      // ⚠️ 三个值都来自**这一次**预览，不是用户填的、也不是记忆的旧值。
                      // 服务端拿它们核对台账有没有在预览之后变过
                      confirm_count: p.renewable,
                      quoted_amount: mainTotal(p).amount,
                      quoted_currency: mainTotal(p).currency,
                    },
                    { onSuccess: (d) => setJobID(d.job_id) },
                  )
                }
              >
                {t('domains:renew.confirm', { count: p.renewable })}
              </WriteButton>
            )}
          </>
        )
      }
    >
      <div className="flex flex-col gap-3">
        {/* 执行中：只显示进度，不再让人改输入 */}
        {running ? (
          <RenewProgress job={job.data} t={t} />
        ) : (
          <>
            <Field label={t('domains:renew.domains')} hint={t('domains:renew.domainsHint')}>
              <TextArea
                rows={5}
                value={text}
                onChange={(e) => {
                  setText(e.target.value)
                  invalidate()
                }}
                placeholder={'example.com\nfoo.net'}
              />
            </Field>

            <Select<string>
              label={t('domains:renew.period')}
              value={String(period)}
              onChange={(v) => {
                setPeriod(Number(v))
                invalidate()
              }}
              options={[1, 2, 3, 5].map((y) => ({
                value: String(y),
                label: t('domains:renew.years', { count: y }),
              }))}
              className="self-start"
            />

            {preview.isError ? (
              <Banner tone="bad">
                <span>
                  {tError(t, toErrorInfo(preview.error).messageKey, toErrorInfo(preview.error).params)}
                </span>
                <span className="mt-0.5 block">{toErrorInfo(preview.error).detail}</span>
                {/* 🔴 "逐条原因见下"这句话必须真的有下文。
                    后端在"没有可续费的域名"里附带了每个域名为什么不能续 ——
                    只显示那一句而不列出来，人下一个问题必然是"那到底为什么"。
                    （文案说了见下、下面却什么都没有，比不说更糟。） */}
                {(() => {
                  const rows = (toErrorInfo(preview.error).extra?.items ?? []) as {
                    domain?: string
                    reason?: string
                    msg?: string
                  }[]
                  return rows.length > 0 ? (
                    <ul className="mt-1 flex flex-col gap-0.5">
                      {rows.map((r, i) => (
                        <li key={r.domain ?? i} className="text-xs">
                          {r.domain ? `${r.domain} — ` : ''}
                          {r.msg ?? r.reason}
                        </li>
                      ))}
                    </ul>
                  ) : null
                })()}
              </Banner>
            ) : null}

            {p ? <PreviewResult p={p} t={t} /> : null}

            {exec.isError ? (
              <Banner tone="bad">
                <span className="font-medium">{t('domains:renew.execFailed')}</span>
                <span className="mt-0.5 block">{toErrorInfo(exec.error).detail}</span>
              </Banner>
            ) : null}
          </>
        )}
      </div>
    </Dialog>
  )
}

type T = (k: string, p?: Record<string, unknown>) => string

/** 主币种与金额。多币种时取金额最大的那个报给服务端核对。 */
function mainTotal(p: RenewPreview): { currency: string; amount: number } {
  const entries = Object.entries(p.totals ?? {})
  const top = entries.sort((a, b) => b[1] - a[1])[0]
  if (!top) return { currency: '', amount: 0 }
  return { currency: top[0], amount: top[1] }
}

function PreviewResult({ p, t }: { p: RenewPreview; t: T }) {
  const totals = Object.entries(p.totals ?? {})
  return (
    <div className="flex flex-col gap-2.5">
      {/* 截断必须说：多出来的那些用户以为也会被续 */}
      {p.truncated ? (
        <Banner tone="warn">
          <span>{p.warning}</span>
        </Banner>
      ) : null}

      <div className="rounded-[var(--radius)] border border-border p-3">
        <div className="flex flex-wrap items-baseline gap-x-4 gap-y-1 text-[13px]">
          <span className="font-medium text-foreground">
            {t('domains:renew.willRenew', { count: p.renewable })}
          </span>
          {totals.map(([cur, amt]) => (
            <span key={cur} className="tabular font-medium text-foreground">
              {cur} {amt.toFixed(2)}
            </span>
          ))}
          {/* 多币种不能相加，分开列 —— 加起来的数字是假的 */}
          {totals.length > 1 ? (
            <span className="text-xs text-muted-foreground">
              {t('domains:renew.multiCurrency')}
            </span>
          ) : null}
        </div>

        {/* ⚠️ 这一条是这个弹窗里最重要的提示：
            有域名查不到报价，它们照样会被续，只是不知道花多少钱 */}
        {p.unpriced > 0 ? (
          <p className="mt-1.5 text-xs leading-relaxed text-warning">
            {t('domains:renew.unpriced', { count: p.unpriced })}
          </p>
        ) : null}
      </div>

      <div className="max-h-[240px] overflow-auto rounded-[var(--radius)] border border-border">
        <table className="w-full min-w-[560px] text-[13px]">
          <tbody>
            {p.items.map((it) => (
              <tr key={it.domain} className="border-b border-border last:border-0">
                <td className="px-3 py-1.5 font-mono text-xs">{it.domain}</td>
                <td className="px-3 py-1.5">
                  {it.status === 'ok' ? (
                    <Badge tone="ok">{t('domains:renew.status.ok')}</Badge>
                  ) : (
                    // 不可续的原因原样显示：not_found / unsupported / duplicated
                    // 各自要做的事完全不同
                    <Badge tone="mute">
                      {t(`domains:renew.status.${it.status}`, { defaultValue: it.status })}
                    </Badge>
                  )}
                </td>
                <td className="px-3 py-1.5 text-xs text-muted-foreground">
                  {it.reason || it.expiry_before || ''}
                </td>
                <td className="tabular px-3 py-1.5 text-right text-xs">
                  {/* 0 = 查不到报价，不是免费。显示成 0 会让人以为不花钱 */}
                  {it.status !== 'ok' ? (
                    ''
                  ) : it.price_per_year ? (
                    `${it.currency ?? ''} ${it.price_per_year}`
                  ) : (
                    <span className="text-warning">{t('domains:renew.noPrice')}</span>
                  )}
                </td>
                <td className="px-3 py-1.5 text-right text-xs text-muted-foreground">
                  {it.expiry_expect ? `→ ${it.expiry_expect}` : ''}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>

      <Banner tone="warn">
        <span className="font-medium">{t('domains:renew.moneyWarning.title')}</span>
        <span className="mt-0.5 block">{t('domains:renew.moneyWarning.body')}</span>
      </Banner>
    </div>
  )
}

function RenewProgress({
  job,
  t,
}: {
  job: { done?: boolean; finished?: number; total?: number; items?: RenewItem[] } | undefined
  t: T
}) {
  const items = job?.items ?? []
  return (
    <div className="flex flex-col gap-2.5">
      <Banner tone="info">
        <span>
          {job?.done
            ? t('domains:renew.jobDone')
            : t('domains:renew.jobRunning', {
                finished: job?.finished ?? 0,
                total: job?.total ?? 0,
              })}
        </span>
      </Banner>

      <div className="max-h-[320px] overflow-auto rounded-[var(--radius)] border border-border">
        <table className="w-full min-w-[560px] text-[13px]">
          <tbody>
            {items.map((it) => (
              <tr key={it.domain} className="border-b border-border last:border-0">
                <td className="px-3 py-1.5 font-mono text-xs">{it.domain}</td>
                <td className="px-3 py-1.5">
                  {/* 三态，绝不合并：
                      uncertain = 已扣费但没拿到确认，要人去账单核对且**不能重试** */}
                  {it.uncertain ? (
                    <Badge tone="warn">{t('domains:renew.status.uncertain')}</Badge>
                  ) : it.expiry_after ? (
                    <Badge tone="ok">{t('domains:renew.status.renewed')}</Badge>
                  ) : (
                    <Badge tone="bad">{t('domains:renew.status.failed')}</Badge>
                  )}
                </td>
                <td className="px-3 py-1.5 text-xs text-muted-foreground">
                  {it.msg || it.reason}
                  {/* 打过几次厂商接口。对账时要知道"这个不止发过一次请求" */}
                  {it.attempts && it.attempts > 1 ? (
                    <span className="ml-1.5 text-warning">
                      {t('domains:renew.attempts', { count: it.attempts })}
                    </span>
                  ) : null}
                  {/* ⚠️ 挂在**成功**的行上：续上了，但到期日前进得比该有的多。
                      单独一行显示，不和 msg 挤在一起——这是关于钱的 */}
                  {it.overpay_note ? (
                    <span className="mt-0.5 block text-warning">{it.overpay_note}</span>
                  ) : null}
                </td>
                <td className="px-3 py-1.5 text-right font-mono text-[11px] text-muted-foreground">
                  {it.order_id || ''}
                </td>
                <td className="px-3 py-1.5 text-right text-xs">
                  {it.expiry_after ? `→ ${it.expiry_after}` : ''}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>

      {items.some((i) => i.uncertain) ? (
        <Banner tone="warn">
          <span className="font-medium">{t('domains:renew.uncertainNote.title')}</span>
          <span className="mt-0.5 block">{t('domains:renew.uncertainNote.body')}</span>
        </Banner>
      ) : null}

      {/* 逐行那条容易被划过去，底部再汇总一次。
          这些条目状态都是"已续费"（绿色），只有这条横幅会让人去查账单 */}
      {items.some((i) => i.overpay_note) ? (
        <Banner tone="warn">
          <span className="font-medium">{t('domains:renew.overpayNote.title')}</span>
          <span className="mt-0.5 block">{t('domains:renew.overpayNote.body')}</span>
          <span className="mt-1 block">
            {items
              .filter((i) => i.overpay_note)
              .map((i) => i.domain)
              .join('、')}
          </span>
        </Banner>
      ) : null}
    </div>
  )
}
