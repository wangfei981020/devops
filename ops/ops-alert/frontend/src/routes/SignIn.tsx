import { useTranslation } from '@ops/i18n'
import { Button, Field, PasswordInput, TextInput } from '@ops/ui'
import { AlertTriangle } from 'lucide-react'
import { type FormEvent, useEffect, useState } from 'react'
import { ApiError, get, post, setToken } from '../lib/api.js'

export function SignInPage() {
  const { t } = useTranslation()
  const [username, setUsername] = useState('')
  const [password, setPassword] = useState('')
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)
  // SSO 按钮显不显示由后端说了算。前端自己判断的话，
  // 改一次配置要重新发前端版本
  const [sso, setSso] = useState<{ enabled: boolean; display_name: string } | null>(null)
  useEffect(() => {
    get<{ enabled: boolean; display_name: string }>('/auth/oidc/config')
      .then(setSso)
      // 取不到就当没开 SSO：登录页照常显示密码框，是安全的降级。
      // ⚠️ 但不能把错误吞成"没开"之后还静默——控制台留一条，
      // 否则"SSO 按钮不见了"会查很久
      .catch((e) => {
        console.warn('[opsalert] SSO 配置读取失败，按未启用处理', e)
        setSso({ enabled: false, display_name: '' })
      })
  }, [])
  // 回调失败时后端会带着原因跳回来。必须显示出来 ——
  // 不显示的话用户只看到"又回到登录页了"，完全不知道发生了什么
  const ssoError = new URLSearchParams(location.hash.split('?')[1] ?? '').get('sso_error')

  async function submit(e: FormEvent) {
    e.preventDefault()
    setBusy(true)
    setError('')
    try {
      const res = await post<{ token: string; username: string; role: string }>('/auth/login', {
        username,
        password,
      })
      setToken(res.token)
      localStorage.setItem('opsalert.username', res.username)
      localStorage.setItem('opsalert.role', res.role)
      location.reload()
    } catch (err) {
      // 用户不存在与密码错误在后端就是同一个响应，这里不再细分——
      // 区分开等于告诉攻击者哪些账号存在。
      setError(
        err instanceof ApiError && err.status === 401
          ? t('opsalert:signIn.failed')
          : t('opsalert:signIn.unreachable'),
      )
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="grid min-h-screen place-items-center bg-background text-foreground">
      <form onSubmit={submit} className="w-80 rounded-lg border border-border bg-card p-6">
        <div className="mb-5 flex items-center gap-2">
          <span className="grid h-7 w-7 place-items-center rounded bg-primary text-primary-foreground">
            <AlertTriangle className="h-4 w-4" />
          </span>
          <div>
            <div className="text-sm font-semibold">{t('opsalert:brand')}</div>
            <div className="text-xs text-muted-foreground">{t('opsalert:signIn.subtitle')}</div>
          </div>
        </div>
        {/* 回调失败的原因。后端把它带在 hash 里跳回来 ——
            不显示的话用户只看到"又回到登录页了"，完全不知道发生了什么，
            而 SSO 接入期间这个信息是唯一的线索 */}
        {ssoError && (
          <div className="mb-3 rounded border border-danger/40 bg-danger-bg px-2.5 py-2 text-xs break-all text-danger">
            {ssoError}
          </div>
        )}

        {sso?.enabled && (
          <div className="mb-4 flex flex-col gap-2">
            <Button
              type="button"
              variant="primary"
              onClick={() => { location.href = '/api/v1/auth/oidc/login' }}
            >
              {t('opsalert:signIn.ssoWith', { name: sso.display_name })}
            </Button>
            {/* 分隔线上写"或用本地账号"，而不是只画一条线：
                接了 SSO 之后本地账号是逃生通道，得让人知道它还在 */}
            <div className="flex items-center gap-2 text-2xs text-muted-foreground">
              <span className="h-px flex-1 bg-border" />
              {t('opsalert:signIn.orLocal')}
              <span className="h-px flex-1 bg-border" />
            </div>
          </div>
        )}

        <div className="flex flex-col gap-3">
          <Field label={t('opsalert:signIn.username')}>
            <TextInput value={username} onChange={(e) => setUsername(e.target.value)} autoFocus />
          </Field>
          <Field label={t('opsalert:signIn.password')}>
            <PasswordInput showLabel={t('action.showSecret')} hideLabel={t('action.hideSecret')} value={password} onChange={(e) => setPassword(e.target.value)} />
          </Field>
          {error && <div className="text-xs text-danger">{error}</div>}
          <Button type="submit" disabled={busy || !username || !password}>
            {busy ? t('opsalert:signIn.submitting') : t('opsalert:signIn.submit')}
          </Button>
        </div>
      </form>
    </div>
  )
}
