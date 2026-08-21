import { useTranslation } from '@ops/i18n'
import { IssuerHostNotice } from './IssuerHostNotice.js'
import { Pwd } from './Pwd.js'
import { useMutation, useQuery } from '@tanstack/react-query'
import { KeyRound, ShieldCheck } from 'lucide-react'
import { type FormEvent, useState } from 'react'
import { api } from '../api/client.js'
import type { Identity, ListOf } from '../api/types.js'
import { Preferences } from './Preferences.js'
import { errorText } from './errorText.js'

interface Idp {
  id: number
  name: string
  login_url: string
}

/**
 * 登录页：SSO 主路径 + 本地账号逃生通道。
 *
 * # 为什么逃生通道要做得比 SSO 弱一档
 *
 * 它是应急路径，不该看起来像日常入口。做得一样显眼的话，
 * 员工会习惯性用本地账号 —— 而本地账号绕开了企业身份源的
 * 离职流程、MFA 与审计，那正是这套系统要消灭的东西。
 */
export function LoginPage({ onSignedIn }: { onSignedIn: () => void }) {
  const { t } = useTranslation()
  const [username, setUsername] = useState('')
  const [password, setPassword] = useState('')

  // 上游身份源列表决定要不要显示 SSO 按钮。取不到就只显示本地登录 ——
  // 这一步失败不该把整个登录页变成错误页，否则身份源配置出问题时
  // 连应急通道都进不去，而应急通道恰恰是为这种时候准备的。
  const idps = useQuery({
    queryKey: ['idp-list'],
    queryFn: () => api.get<ListOf<Idp>>('/auth/idp-list'),
    retry: false,
  })

  const login = useMutation({
    mutationFn: () => api.post<Identity>('/auth/login', { username, password }),
    onSuccess: onSignedIn,
  })

  function submit(e: FormEvent) {
    e.preventDefault()
    login.mutate()
  }

  const hasIdp = (idps.data?.items?.length ?? 0) > 0

  return (
    <div className="grid min-h-screen lg:grid-cols-[1fr_460px]">
      {/* 左栏：说清这套系统是干什么的。窄屏隐藏，登录本身才是主任务 */}
      <aside className="hidden flex-col justify-between border-r border-border bg-surface p-12 lg:flex">
        <div className="flex items-center gap-2.5">
          <span className="grid size-7 place-items-center rounded-[var(--radius)] bg-primary text-[11px] font-semibold text-primary-foreground">
            SSO
          </span>
          <b className="font-semibold">{t('sso:brand')}</b>
        </div>
        <div>
          <h1 className="text-3xl leading-snug font-semibold tracking-tight">
            {t('sso:login.headline')}
          </h1>
          <p className="mt-4 max-w-[42ch] text-sm text-muted-foreground">
            {t('sso:login.tagline')}
          </p>
        </div>
        <span />
      </aside>

      <main className="relative flex flex-col justify-center p-12">
        {/* 登录页不走 Shell，但**必须有语言与主题开关** ——
            看不懂中文的人恰恰是在这一屏被挡住的，进不去就没机会切。 */}
        <div className="absolute top-6 right-6">
          <Preferences />
        </div>
        <h2 className="text-xl font-semibold tracking-tight">{t('sso:login.title')}</h2>
        <p className="mt-1 mb-6 text-sm text-muted-foreground">{t('sso:login.subtitle')}</p>

        {hasIdp ? (
          <>
            {idps.data?.items.map((idp) => (
              <a
                key={idp.id}
                href={idp.login_url}
                className="mb-2 flex h-10 w-full items-center justify-center gap-2 rounded-[var(--radius)] bg-primary text-sm font-medium text-primary-foreground hover:bg-primary-hover"
              >
                <ShieldCheck className="size-4" />
                {t('sso:login.withIdp', { name: idp.name })}
              </a>
            ))}
            <p className="mb-5 text-xs text-muted-foreground">{t('sso:login.idpHint')}</p>
            <div className="mb-5 flex items-center gap-3 text-xs text-muted-foreground">
              <span className="h-px flex-1 bg-border" />
              {t('sso:login.or')}
              <span className="h-px flex-1 bg-border" />
            </div>
          </>
        ) : (
          // 没配身份源时说清楚，而不是让页面上凭空只有一个本地登录框
          <p className="mb-5 rounded-[var(--radius)] border border-border bg-muted p-3 text-xs text-muted-foreground">
            {t('sso:login.noIdp')}
          </p>
        )}

        {/* 「登在了另一个地址上」是这一页最容易出现、又最没线索的问题：
            人刚在控制台登录过，被下游送过来还是看到登录框。 */}
        <IssuerHostNotice />

        <form onSubmit={submit}>
          <label className="mb-1.5 block text-xs font-medium text-muted-foreground" htmlFor="u">
            {t('sso:login.username')}
          </label>
          <input
            id="u"
            value={username}
            onChange={(e) => setUsername(e.target.value)}
            autoComplete="username"
            className="mb-3.5 w-full rounded-[var(--radius)] border border-input bg-background px-3 py-2 text-sm focus:border-primary focus:outline-none"
          />
          <label className="mb-1.5 block text-xs font-medium text-muted-foreground" htmlFor="p">
            {t('sso:login.password')}
          </label>
          {/* 口令可见开关。
              没有它的时候，"用户名或口令不对"这句话是**无法自证的** ——
              人不知道自己是打错了、开着大写锁、还是粘贴时多带了个空格，
              只能一遍遍重试（而重试多了还会撞上锁定）。
              实际就撞到过：口令是对的，输入时大小写错了一个字母。 */}
          <Pwd
            id="p"
            value={password}
            onChange={(e) => setPassword(e.target.value)}
            autoComplete="current-password"
            className="border-input py-2 pl-3 text-sm focus:border-primary"
          />

          {login.error ? (
            <p className="mt-3 rounded-[var(--radius)] border border-destructive bg-danger-bg p-2.5 text-xs text-foreground">
              {errorText(t, login.error)}
            </p>
          ) : null}

          <button
            type="submit"
            disabled={login.isPending || !username || !password}
            className="mt-5 h-9 w-full cursor-pointer rounded-[var(--radius)] border border-border-strong text-sm font-medium hover:bg-secondary disabled:cursor-not-allowed disabled:opacity-50"
          >
            <span className="inline-flex items-center gap-2">
              <KeyRound className="size-4" />
              {login.isPending ? t('sso:login.signingIn') : t('sso:login.signIn')}
            </span>
          </button>
        </form>

        <p className="mt-5 text-xs leading-relaxed text-muted-foreground">
          {t('sso:login.localHint')}
        </p>
      </main>
    </div>
  )
}
