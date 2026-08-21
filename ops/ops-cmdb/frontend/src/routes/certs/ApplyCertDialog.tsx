import { toErrorInfo } from '@ops/api'
import { tError, useTranslation } from '@ops/i18n'
import { Banner, Dialog, Field, Select, TextArea } from '@ops/ui'
import { useState } from 'react'
import { WriteButton } from '../../components/WriteButton.js'
import { useDomains } from '../domains/queries.js'
import { type CertApplyInput, useAcmeAccounts, useApplyCert } from './queries.js'

const PERM = 'cmdb:issue_cert'

/**
 * 申请证书。
 *
 * # ⚠️ 没有 ACME 账号时不能让人往下填
 *
 * 后端要求 `acme_account_id` 必填，而自动续期任务在没有对应 CA 的账号时
 * 会**逐张证书失败**。所以这里第一步就是检查账号：一条都没有时，
 * 表单整个不给，直接告诉人先去建账号 —— 而不是让人填完一屏再收一个 400。
 *
 * # ⚠️ CA 必须和账号的 CA 一致
 *
 * 续期时是按 ca 找账号的，对不上等于没配。所以 CA 不让人自由选，
 * 而是跟着所选账号走 —— 少一个能填错的地方。
 */
export function ApplyCertDialog({ onClose }: { onClose: () => void }) {
  const { t } = useTranslation()
  const accounts = useAcmeAccounts()
  const domains = useDomains({ page: 1, size: 500, health: 'all' })
  const apply = useApplyCert()

  // ⚠️ 字段名是 `registered`，不是 has_key。
  //
  //	写错的后果比"少一列信息"严重得多：undefined 是 falsy，
  //	`filter(a => a.has_key)` **恒为空数组** ——
  //	于是申请证书的账户下拉永远没有选项，整个申请流程不可用，
  //	而界面上看起来只是"你还没有配 ACME 账户"。
  const usable = (accounts.data ?? []).filter((a) => a.registered)
  const [acct, setAcct] = useState('')
  const [cn, setCn] = useState('')
  const [sans, setSans] = useState('')
  const [challenge, setChallenge] = useState('dns-01')
  const [domainCi, setDomainCi] = useState('')
  const [autoRenew, setAutoRenew] = useState('1')
  const [staging, setStaging] = useState('0')

  const account = usable.find((a) => String(a.id) === acct)

  // 账号一条都没有 / 有但都没私钥：两种情况的下一步不一样，分开说
  if (!accounts.isPending && usable.length === 0) {
    return (
      <Dialog
        open
        onClose={onClose}
        title={t('certs:apply.title')}
        closeLabel={t('common:action.close')}
        width={480}
        footer={
          <WriteButton perm={PERM} size="sm" onClick={onClose}>
            {t('common:action.close')}
          </WriteButton>
        }
      >
        <Banner tone="warn">
          <span className="font-medium">{t('certs:apply.noAccount.title')}</span>
          <span className="mt-0.5 block">
            {t(
              (accounts.data ?? []).length === 0
                ? 'certs:apply.noAccount.none'
                : 'certs:apply.noAccount.noKey',
            )}
          </span>
        </Banner>
      </Dialog>
    )
  }

  const blocked = !cn.trim()
    ? t('certs:apply.needCn')
    : !acct
      ? t('certs:apply.needAccount')
      : undefined

  function submit() {
    const v: CertApplyInput = {
      cn: cn.trim(),
      // 一行一个，空行丢掉。⚠️ CN 本身不用重复写进 SANs，后端会带上
      sans: sans.split('\n').map((s) => s.trim()).filter(Boolean),
      ca: account?.ca ?? 'letsencrypt',
      challenge,
      acme_account_id: Number(acct),
      domain_ci_id: domainCi ? Number(domainCi) : undefined,
      auto_renew: Number(autoRenew),
      renew_days: 30,
      staging: staging === '1',
    }
    apply.mutate(v, { onSuccess: onClose })
  }

  return (
    <Dialog
      open
      onClose={onClose}
      title={t('certs:apply.title')}
      description={t('certs:apply.desc')}
      closeLabel={t('common:action.close')}
      width={560}
      footer={
        <>
          <WriteButton perm={PERM} size="sm" onClick={onClose}>
            {t('common:action.cancel')}
          </WriteButton>
          <WriteButton
            perm={PERM}
            variant="primary"
            size="sm"
            loading={apply.isPending}
            blockedReason={blocked}
            onClick={submit}
          >
            {t('certs:apply.submit')}
          </WriteButton>
        </>
      }
    >
      <div className="flex flex-col gap-3">
        {/* ⚠️ 配额是这一页最容易踩的坑，放在最前面 */}
        <Banner tone="info">
          <span>{t('certs:apply.quotaNote')}</span>
        </Banner>

        <div className="grid grid-cols-2 gap-3">
          <Field label={t('certs:apply.account')} hint={t('certs:apply.accountHint')}>
            <Select
              label={t('certs:apply.account')}
              value={acct}
              onChange={setAcct}
              options={usable.map((a) => ({
                value: String(a.id),
                label: `${a.ca} · ${a.email || t('certs:apply.noEmail')}`,
              }))}
            />
          </Field>
          <Field label={t('certs:apply.challenge')} hint={t('certs:apply.challengeHint')}>
            <Select
              label={t('certs:apply.challenge')}
              value={challenge}
              onChange={setChallenge}
              options={[
                { value: 'dns-01', label: 'dns-01' },
                { value: 'http-01', label: 'http-01' },
              ]}
            />
          </Field>
        </div>

        <Field label={t('certs:apply.cn')} hint={t('certs:apply.cnHint')}>
          <input
            value={cn}
            onChange={(e) => setCn(e.target.value)}
            placeholder="example.com"
            className={inputCls}
          />
        </Field>
        <Field label={t('certs:apply.sans')} hint={t('certs:apply.sansHint')}>
          <TextArea value={sans} onChange={(e) => setSans(e.target.value)} rows={3} />
        </Field>

        <div className="grid grid-cols-2 gap-3">
          <Field label={t('certs:apply.domain')} hint={t('certs:apply.domainHint')}>
            <Select
              label={t('certs:apply.domain')}
              value={domainCi}
              onChange={setDomainCi}
              options={[
                { value: '', label: t('certs:apply.noDomain') },
                ...(domains.data?.items ?? []).map((d) => ({
                  value: String(d.ciId),
                  label: d.name,
                })),
              ]}
            />
          </Field>
          <Field label={t('certs:apply.autoRenew')}>
            <Select
              label={t('certs:apply.autoRenew')}
              value={autoRenew}
              onChange={setAutoRenew}
              options={[
                { value: '1', label: t('common:state.on') },
                { value: '0', label: t('common:state.off') },
              ]}
            />
          </Field>
        </div>

        {/* ⚠️ staging 签出来的证书浏览器不认。默认关，且选上之后要显式警告 */}
        <Field label={t('certs:apply.staging')} hint={t('certs:apply.stagingHint')}>
          <Select
            label={t('certs:apply.staging')}
            value={staging}
            onChange={setStaging}
            options={[
              { value: '0', label: t('certs:apply.stagingOff') },
              { value: '1', label: t('certs:apply.stagingOn') },
            ]}
          />
        </Field>
        {staging === '1' ? (
          <Banner tone="warn">
            <span>{t('certs:apply.stagingWarn')}</span>
          </Banner>
        ) : null}

        {apply.isError ? (
          <Banner tone="bad">
            <span>{tError(t, toErrorInfo(apply.error).messageKey, toErrorInfo(apply.error).params)}</span>
            <span className="mt-0.5 block">{toErrorInfo(apply.error).detail}</span>
          </Banner>
        ) : null}
      </div>
    </Dialog>
  )
}

const inputCls =
  'h-9 w-full rounded-[var(--radius)] border border-input bg-card px-2.5 text-[13px] ' +
  'text-foreground outline-none focus:border-primary focus:shadow-[0_0_0_2px_var(--ops-accent-ring)]'
