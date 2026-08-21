import { toErrorInfo } from '@ops/api'
import { tError, useTranslation } from '@ops/i18n'
import { Badge, Banner, Dialog, Field, Select } from '@ops/ui'
import { useState } from 'react'
import { WriteButton } from '../../components/WriteButton.js'
import {
  type Domain,
  useGodaddyDetail,
  useRenewDomain,
  useSetAutoRenew,
} from './queries.js'

const PERM = 'cmdb:manage_domains'

/**
 * 单个域名：厂商侧状态 + 续费 + 自动续费开关。
 *
 * # 为什么把这三件事放在一个弹窗里
 *
 * 它们共用同一份前置事实：**这个域名在注册商那边现在是什么状态**。
 * 分成三个入口的话，每个都得自己去拉一次 godaddy-detail（打厂商接口），
 * 而且人会在不知道"自动续费已经开着"的情况下手工续一次费 —— 白扣一年。
 *
 * # ⚠️ 三处不能省的诚实
 *
 * 1. `detail_ok:false` 时**不显示自动续费开关**。读不出来就是读不出来，
 *    渲染成"未开启"会让人以为关着而去手工续费。
 * 2. 续费价拿不到时不显示金额，而不是显示 0 —— 0 元续费是不存在的事。
 * 3. dry_run（数据源是预演模式）必须常驻横幅。预演下点"续费"什么都不会发生，
 *    而返回是成功的 —— 不说清楚的话，人会以为续上了。
 */
export function DomainOpsDialog({ d, onClose }: { d: Domain; onClose: () => void }) {
  const { t } = useTranslation()
  const detail = useGodaddyDetail(d.ciId, true)
  const renew = useRenewDomain()
  const auto = useSetAutoRenew()
  // ⚠️ 存成字符串：Select 的值类型受限于 string。
  // 提交前转成数字 —— 后端对 period 有 1-10 的强校验，转不出来会被 400 拒掉，
  // 不会静默按 1 年续（真金白银，静默默认是最坏的做法）
  const [periodStr, setPeriod] = useState('1')
  const period = Number(periodStr)
  // 续费要二次确认：这一步之后就真扣钱了
  const [confirming, setConfirming] = useState(false)

  const info = detail.data
  const price = info?.price_per_year
  const done = renew.data

  return (
    <Dialog
      open
      onClose={onClose}
      title={d.name}
      description={t('domains:ops.desc')}
      closeLabel={t('common:action.close')}
      width={560}
      footer={
        <WriteButton perm={PERM} size="sm" onClick={onClose}>
          {t('common:action.close')}
        </WriteButton>
      }
    >
      <div className="flex flex-col gap-3">
        {/* 预演模式常驻。它决定了下面所有按钮到底会不会真的动到注册商 */}
        {info?.dry_run ? (
          <Banner tone="warn">
            <span className="font-medium">{t('domains:ops.dryRun.title')}</span>
            <span className="mt-0.5 block">{t('domains:ops.dryRun.body', { env: info.env })}</span>
          </Banner>
        ) : null}

        {detail.isPending ? (
          <span className="text-[13px] text-muted-foreground">{t('common:state.loading')}</span>
        ) : null}

        {detail.isError ? (
          <Banner tone="bad">
            <span>{tError(t, toErrorInfo(detail.error).messageKey, toErrorInfo(detail.error).params)}</span>
            <span className="mt-0.5 block">{toErrorInfo(detail.error).detail}</span>
          </Banner>
        ) : null}

        {/* 厂商侧读不出来：这不是"一切正常"，下面的自动续费开关整个不给 */}
        {info && !info.detail_ok ? (
          <Banner tone="warn">
            <span className="font-medium">{t('domains:ops.detailDegraded.title')}</span>
            <span className="mt-0.5 block">{t('domains:ops.detailDegraded.body')}</span>
          </Banner>
        ) : null}

        {info?.detail_ok ? (
          <div className="grid grid-cols-3 gap-3 rounded-[var(--radius)] border border-border bg-secondary/40 p-3">
            <Stat label={t('domains:ops.expires')} value={info.expires ?? '—'} />
            <Stat
              label={t('domains:ops.autoRenew')}
              value={
                <Badge tone={info.renew_auto ? 'ok' : 'warn'}>
                  {t(info.renew_auto ? 'common:state.on' : 'common:state.off')}
                </Badge>
              }
            />
            <Stat label={t('domains:ops.registrarStatus')} value={info.status ?? '—'} />
            {/* 隐私保护（WHOIS 代持）。旧版续费弹窗有这一项。
                关掉意味着注册人的姓名/邮箱/电话在 WHOIS 上是公开的 ——
                续费时顺手确认一眼，比事后被扒出来强。
                ⚠️ undefined = 注册商没返回这一项，不是"没开"：
                两者处置不同（前者去查接口，后者去开保护）。 */}
            <Stat
              label={t('domains:ops.privacy')}
              value={
                info.privacy === undefined ? (
                  <span className="text-xs text-muted-foreground">{t('common:state.unknown')}</span>
                ) : (
                  <Badge tone={info.privacy ? 'ok' : 'warn'}>
                    {t(info.privacy ? 'common:state.on' : 'common:state.off')}
                  </Badge>
                )
              }
            />
          </div>
        ) : null}

        {/* ── 续费 ── */}
        <div className="flex flex-wrap items-end gap-3 border-t border-border pt-3">
          <Field label={t('domains:renew.period')}>
            <Select
              label={t('domains:renew.period')}
              value={periodStr}
              onChange={setPeriod}
              options={[1, 2, 3, 5].map((n) => ({
                value: String(n),
                label: t('domains:renew.years', { count: n }),
              }))}
            />
          </Field>
          {/* 价格拿不到就不显示，而不是显示 0 —— 0 元续费不存在 */}
          {price != null && price > 0 ? (
            <span className="pb-1.5 text-[13px] text-muted-foreground">
              {t('domains:ops.estimate', {
                amount: (price * period).toFixed(2),
                currency: info?.currency ?? '',
              })}
            </span>
          ) : (
            <span className="pb-1.5 text-[13px] text-muted-foreground">
              {t('domains:ops.noPrice')}
            </span>
          )}
          <WriteButton
            perm={PERM}
            variant="primary"
            size="sm"
            className="ml-auto"
            loading={renew.isPending}
            onClick={() => setConfirming(true)}
          >
            {t('domains:ops.renewNow')}
          </WriteButton>
        </div>

        {confirming ? (
          <Banner tone="warn">
            <span className="font-medium">{t('domains:ops.confirm.title')}</span>
            <span className="mt-0.5 block">
              {t('domains:ops.confirm.body', { domain: d.name, count: period })}
            </span>
            <span className="mt-2 flex gap-2">
              <WriteButton perm={PERM} size="sm" onClick={() => setConfirming(false)}>
                {t('common:action.cancel')}
              </WriteButton>
              <WriteButton
                perm={PERM}
                variant="danger"
                size="sm"
                loading={renew.isPending}
                onClick={() => {
                  setConfirming(false)
                  renew.mutate({ ciId: d.ciId, period })
                }}
              >
                {t('domains:ops.confirm.ok')}
              </WriteButton>
            </span>
          </Banner>
        ) : null}

        {/* ⚠️ 续费失败**不给重试按钮**：失败不等于没扣费。
            必须先回查到期日确认没扣，才能再点一次 —— 那一步只能由人做 */}
        {renew.isError ? (
          <Banner tone="bad">
            <span>{tError(t, toErrorInfo(renew.error).messageKey, toErrorInfo(renew.error).params)}</span>
            <span className="mt-0.5 block">{toErrorInfo(renew.error).detail}</span>
            <span className="mt-1 block font-medium">{t('domains:ops.failNote')}</span>
          </Banner>
        ) : null}

        {done ? (
          <Banner tone={done.overpay_note || done.warning ? 'warn' : 'info'}>
            <span className="font-medium">{done.msg ?? t('common:write.saved')}</span>
            {done.order_id ? (
              <span className="mt-0.5 block tabular">
                {t('domains:ops.orderId')}: {done.order_id}
              </span>
            ) : null}
            {done.expiry_before || done.expiry_after ? (
              <span className="mt-0.5 block tabular">
                {done.expiry_before || '—'} → {done.expiry_after || '—'}
              </span>
            ) : null}
            {/* 台账没写上 = 记录问题；多扣费 = 钱的问题。两者下一步完全不同，分开显示 */}
            {done.warning ? <span className="mt-1 block">{done.warning}</span> : null}
            {done.overpay_note ? (
              <span className="mt-1 block font-medium text-warning">{done.overpay_note}</span>
            ) : null}
          </Banner>
        ) : null}

        {/* ── 自动续费。只有厂商侧状态读到了才给，否则不知道在改什么 ── */}
        {info?.detail_ok ? (
          <div className="flex flex-wrap items-center gap-2 border-t border-border pt-3">
            <span className="text-[13px] text-muted-foreground">
              {t('domains:ops.autoRenewHint')}
            </span>
            <WriteButton
              perm={PERM}
              size="sm"
              className="ml-auto"
              loading={auto.isPending}
              onClick={() => {
                auto.mutate(
                  { ciId: d.ciId, enabled: !info.renew_auto },
                  { onSuccess: () => void detail.refetch() },
                )
              }}
            >
              {t(info.renew_auto ? 'domains:ops.turnOffAuto' : 'domains:ops.turnOnAuto')}
            </WriteButton>
            {auto.isError ? (
              <span className="w-full text-xs text-danger" title={toErrorInfo(auto.error).detail || undefined}>
          {tError(t, toErrorInfo(auto.error).messageKey, toErrorInfo(auto.error).params)}
        </span>
            ) : null}
          </div>
        ) : null}
      </div>
    </Dialog>
  )
}

function Stat({ label, value }: { label: string; value: React.ReactNode }) {
  return (
    <div className="flex min-w-0 flex-col gap-1">
      <span className="text-xs text-muted-foreground">{label}</span>
      <span className="truncate text-[13px] text-foreground tabular">{value}</span>
    </div>
  )
}
