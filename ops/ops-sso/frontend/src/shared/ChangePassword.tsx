import { useTranslation } from '@ops/i18n'
import { Pwd } from './Pwd.js'
import { useMutation } from '@tanstack/react-query'
import { type FormEvent, useState } from 'react'
import { api } from '../api/client.js'
import { Preferences } from './Preferences.js'
import { errorText } from './errorText.js'

/**
 * 强制改密。
 *
 * ⚠️ 这一页只是**体验层**。真正的强制在后端中间件里：
 * 标了必须改密的账号，所有业务接口一律 428，直接调接口同样被拦。
 * 前端跳这一页，是为了让人看到一个可操作的界面，
 * 而不是一堆"还差一步"的报错。
 */
export function ChangePassword({ onDone }: { onDone: () => void }) {
  const { t } = useTranslation()
  const [oldPwd, setOld] = useState('')
  const [newPwd, setNew] = useState('')
  const [confirm, setConfirm] = useState('')

  const change = useMutation({
    mutationFn: () => api.post('/auth/password', { old_password: oldPwd, new_password: newPwd }),
    onSuccess: onDone,
  })

  const mismatch = confirm.length > 0 && newPwd !== confirm
  const canSubmit = oldPwd && newPwd && confirm && !mismatch && !change.isPending

  return (
    <div className="relative mx-auto max-w-md p-12">
      <div className="absolute top-6 right-6">
        <Preferences />
      </div>
      <h1 className="text-xl font-semibold tracking-tight">{t('sso:changePwd.title')}</h1>
      <p className="mt-1 mb-6 text-sm text-muted-foreground">{t('sso:changePwd.why')}</p>

      <form
        onSubmit={(e: FormEvent) => {
          e.preventDefault()
          change.mutate()
        }}
      >
        {/* 密码管理器要靠这个字段判断记录属于谁，隐藏但必须存在 */}
        <input type="text" autoComplete="username" hidden readOnly value="" />
        {(
          [
            ['old', t('sso:changePwd.current'), oldPwd, setOld, 'current-password'],
            ['new', t('sso:changePwd.new'), newPwd, setNew, 'new-password'],
            ['cfm', t('sso:changePwd.again'), confirm, setConfirm, 'new-password'],
          ] as const
        ).map(([id, label, val, set, ac]) => (
          <div key={id} className="mb-3.5">
            <label className="mb-1.5 block text-xs font-medium text-muted-foreground" htmlFor={id}>
              {label}
            </label>
            <Pwd
              id={id}
              value={val}
              autoComplete={ac}
              onChange={(e) => set(e.target.value)}
              className="border-input px-3 py-2 text-sm focus:border-primary"
            />
          </div>
        ))}

        {mismatch ? (
          <p className="mb-3 text-xs text-danger">{t('sso:changePwd.mismatch')}</p>
        ) : null}
        {change.error ? (
          <p className="mb-3 rounded-[var(--radius)] border border-destructive bg-danger-bg p-2.5 text-xs text-foreground">
            {errorText(t, change.error)}
          </p>
        ) : null}

        <button
          type="submit"
          disabled={!canSubmit}
          className="h-9 w-full cursor-pointer rounded-[var(--radius)] bg-primary text-sm font-medium text-primary-foreground hover:bg-primary-hover disabled:cursor-not-allowed disabled:opacity-50"
        >
          {change.isPending ? t('sso:changePwd.saving') : t('sso:changePwd.submit')}
        </button>
      </form>

      <p className="mt-4 text-xs leading-relaxed text-muted-foreground">
        {t('sso:changePwd.revokeNote')}
      </p>
    </div>
  )
}
