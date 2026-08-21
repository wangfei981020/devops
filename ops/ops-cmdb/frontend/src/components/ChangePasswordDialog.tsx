import { toErrorInfo } from '@ops/api'
import { tError, useTranslation } from '@ops/i18n'
import { Banner, Button, Dialog, Field } from '@ops/ui'
import { useMutation } from '@tanstack/react-query'
import { useState } from 'react'
import { apiAction } from '../lib/fetchJson.js'

/**
 * 自助改密码。
 *
 * ⚠️ 这个入口以前**根本不存在**：后端 `PUT /api/me/password` 一直在，
 * 用户管理页只有「管理员改别人的密码」。也就是说任何人想改自己的密码，
 * 都得去找管理员代改 —— 而管理员代改意味着**管理员知道了你的新密码**。
 *
 * ⚠️ 改成功后**所有会话都会被作废**（含当前这个），后端就是这么做的。
 * 界面必须提前说清楚并在成功后直接踢回登录页 ——
 * 否则用户会看到接下来每个请求都 401，以为系统坏了。
 * 「改密码」这个动作的场景之一就是怀疑密码泄露，不踢会话等于没改。
 */
export function ChangePasswordDialog({ onClose }: { onClose: () => void }) {
  const { t } = useTranslation()
  const [oldPw, setOld] = useState('')
  const [newPw, setNew] = useState('')
  const [confirm, setConfirm] = useState('')

  const mut = useMutation({
    retry: false,
    mutationFn: (v: { old_password: string; new_password: string }) =>
      apiAction<{ ok?: boolean; msg?: string }>('/api/me/password', 'PUT', v),
  })

  // 校验放在前端只是为了少跑一趟，判定权在后端（8 位下限、新旧不同都是后端的规则）
  const blocked = !oldPw
    ? t('user.pw.needOld')
    : newPw.length < 8
      ? t('user.pw.tooShort')
      : newPw === oldPw
        ? t('user.pw.sameAsOld')
        : newPw !== confirm
          ? t('user.pw.mismatch')
          : undefined

  if (mut.isSuccess) {
    return (
      <Dialog
        open
        onClose={() => window.location.reload()}
        title={t('user.pw.doneTitle')}
        closeLabel={t('common:action.close')}
        width={440}
        footer={
          <Button variant="primary" size="sm" onClick={() => window.location.reload()}>
            {t('user.pw.relogin')}
          </Button>
        }
      >
        <Banner tone="info">
          <span>{mut.data?.msg ?? t('user.pw.doneBody')}</span>
        </Banner>
      </Dialog>
    )
  }

  return (
    <Dialog
      open
      onClose={onClose}
      title={t('user.pw.title')}
      description={t('user.pw.desc')}
      closeLabel={t('common:action.close')}
      width={440}
      footer={
        <>
          <Button size="sm" onClick={onClose}>
            {t('common:action.cancel')}
          </Button>
          <Button
            variant="primary"
            size="sm"
            loading={mut.isPending}
            disabled={blocked !== undefined}
            title={blocked}
            onClick={() => mut.mutate({ old_password: oldPw, new_password: newPw })}
          >
            {t('common:action.save')}
          </Button>
        </>
      }
    >
      <div className="flex flex-col gap-3">
        {/* 提前说清楚会被踢下线。事后才知道的话，人会以为是系统坏了 */}
        <Banner tone="warn">
          <span>{t('user.pw.killsSessions')}</span>
        </Banner>
        <Field label={t('user.pw.old')}>
          <input
            type="password"
            autoComplete="current-password"
            value={oldPw}
            onChange={(e) => setOld(e.target.value)}
            className={inputCls}
          />
        </Field>
        <Field label={t('user.pw.new')} hint={t('user.pw.rule')}>
          <input
            type="password"
            autoComplete="new-password"
            value={newPw}
            onChange={(e) => setNew(e.target.value)}
            className={inputCls}
          />
        </Field>
        <Field label={t('user.pw.confirm')}>
          <input
            type="password"
            autoComplete="new-password"
            value={confirm}
            onChange={(e) => setConfirm(e.target.value)}
            className={inputCls}
          />
        </Field>
        {/* 校验没过时把原因摆出来，而不是只让保存按钮灰着 ——
            灰按钮不说话，人只能挨个字段猜 */}
        {blocked && (oldPw || newPw || confirm) ? (
          <span className="text-xs text-warning">{blocked}</span>
        ) : null}
        {mut.isError ? (
          <Banner tone="bad">
            <span>{tError(t, toErrorInfo(mut.error).messageKey, toErrorInfo(mut.error).params)}</span>
            <span className="mt-0.5 block">{toErrorInfo(mut.error).detail}</span>
          </Banner>
        ) : null}
      </div>
    </Dialog>
  )
}

const inputCls =
  'h-9 w-full rounded-[var(--radius)] border border-input bg-card px-2.5 text-[13px] ' +
  'text-foreground outline-none focus:border-primary focus:shadow-[0_0_0_2px_var(--ops-accent-ring)]'
