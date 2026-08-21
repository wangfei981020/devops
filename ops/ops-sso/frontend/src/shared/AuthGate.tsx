import { useTranslation } from '@ops/i18n'
import { Skeleton } from '@ops/ui'
import { type ReactNode, useEffect } from 'react'
import { ApiError } from '../api/client.js'
import { useMe } from './session.js'
import { LoginPage } from './LoginPage.js'
import { ChangePassword } from './ChangePassword.js'

/**
 * 认证闸门：没登录就只能看到登录页。
 *
 * # 401 不是错误态
 *
 * `/auth/me` 返回 401 意味着"还没登录"，是**正常状态**，
 * 必须渲染登录页而不是错误页。把它当错误处理的话，
 * 新用户第一次打开就会看到一个红色的失败提示 —— 而他什么都没做错。
 *
 * 其余错误（网络不通、后端 500）才是真错误，那些要如实报出来，
 * 绝不能伪装成"请登录" —— 否则用户会反复输密码，
 * 而问题其实在后端。
 */
export function AuthGate({ children }: { children: ReactNode }) {
  const { t } = useTranslation()
  const { data: me, isPending, error, refetch } = useMe()

  // 登录（或强制改密）完成之后回到来的地方。
  //
  // 后端把人从 /oidc/authorize 赶到登录页时带了 ?next=，但**前端一直没读它** ——
  // 表现是：从 Harbor 点过来、登录成功，然后停在门户首页，
  // 要自己再点一次那个应用才能继续。看起来像"登录没生效"。
  const ready = !isPending && !error && me !== undefined && !me.must_change_password
  const next = ready ? safeNext() : null
  useEffect(() => {
    if (next) window.location.replace(next)
  }, [next])

  if (isPending) {
    // 骨架屏而不是转圈：复刻真实布局，内容到位时不跳
    return (
      <div className="mx-auto max-w-[1360px] space-y-3 p-6">
        <Skeleton className="h-7 w-40" />
        <Skeleton className="h-4 w-72" />
        <Skeleton className="h-48 w-full" />
      </div>
    )
  }

  if (error) {
    const unauth = error instanceof ApiError && error.status === 401
    if (unauth) return <LoginPage onSignedIn={() => void refetch()} />

    // 真错误：说清是什么、给重试入口，不要伪装成"请登录"
    return (
      <div className="mx-auto max-w-lg p-10 text-center">
        <p className="text-sm font-semibold text-foreground">{t('sso:login.backendDown')}</p>
        <p className="mt-2 text-sm text-muted-foreground">
          {error instanceof ApiError ? error.code : String(error)}
        </p>
        <button
          type="button"
          onClick={() => void refetch()}
          className="mt-4 cursor-pointer rounded-[var(--radius)] border border-border px-3 py-1.5 text-sm hover:bg-secondary"
        >
          {t('retry')}
        </button>
      </div>
    )
  }

  // 强制改密：后端对所有业务接口返 428，前端跳这一页只是体验层。
  // 直接调接口一样被拦 —— 能绕过的强制等于没有。
  if (me?.must_change_password) return <ChangePassword onDone={() => void refetch()} />

  // 正在跳走就别渲染内容：先闪一下门户再跳，看着像点错了
  if (next) return null

  return <>{children}</>
}

/**
 * 取出 ?next= 并确认它只指向本站。
 *
 * ⚠️ 不校验的话这就是一个**开放重定向**：`?next=https://evil.com` 会把
 * 登录页变成钓鱼跳板 —— 受害者看到的是我们的域名、我们的登录框，
 * 登完却被送去别处。这类洞在 SSO 上尤其值钱，因为登录页天然被信任。
 *
 * 判据只有一条：**必须是本站的绝对路径**。
 * `//evil.com` 是协议相对 URL，浏览器当跨站处理，所以要单独挡掉 ——
 * 只判 startsWith('/') 是不够的，这一步最容易漏。
 */
function safeNext(): string | null {
  const raw = new URLSearchParams(window.location.search).get('next')
  if (!raw) return null
  if (!raw.startsWith('/') || raw.startsWith('//')) return null
  return raw
}
