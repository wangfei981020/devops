import { useTranslation } from '@ops/i18n'
import { useEffect } from 'react'
import { setToken } from '../lib/api.js'

/**
 * SSO 回调落地页。
 *
 * 后端把会话令牌放在 URL **片段**里跳回来（`#/sso?token=…`）。
 *
 * ⚠️ 为什么是片段而不是 query：片段不会被发到服务端，
 * 因此不会进 nginx / Ingress 的访问日志，也不会出现在 Referer 头里。
 * 用 query 传令牌的话，一串能直接登录的凭据会散落在每一层反代的日志里。
 *
 * ⚠️ 读完立刻把地址栏清干净：留着的话用户一复制链接就把自己的会话发出去了。
 */
export function SsoLandingPage() {
  const { t } = useTranslation()
  useEffect(() => {
    const token = new URLSearchParams(location.hash.split('?')[1] ?? '').get('token')
    if (!token) {
      location.replace('/#/login?sso_error=' + encodeURIComponent('回调里没有令牌'))
      return
    }
    setToken(token)
    // 🔴 必须先用 history.replaceState 抹掉 hash，**再** reload。
    //
    // 写成 `location.replace('/') ; location.reload()` 会死循环：
    // 当前地址是 `/#/sso?token=…`，replace 到 `/` 只是改 hash，
    // 属于**同文档导航**，不会重新加载；紧跟的 reload() 于是把
    // 带 token 的原地址又load 回来 → 本组件再跑一遍 → 无限循环。
    // 实测每 75ms 重载一次，控制台刷出上千条日志，页面卡在这一屏。
    //
    // replaceState 不触发导航，只把地址栏改干净；随后的 reload
    // 加载的就是干净的 `/`。顺带也满足"不留历史记录"——
    // 按后退键不会回到这个带令牌的地址。
    history.replaceState(null, '', '/')
    location.reload()
  }, [])
  return (
    <div className="grid min-h-screen place-items-center bg-background text-sm text-muted-foreground">
      {t('opsalert:signIn.ssoLanding')}
    </div>
  )
}
