import { I18nextProvider } from '@ops/i18n'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import { App } from './App.js'
import { SignInPage } from './routes/SignIn.js'
import { SsoLandingPage } from './routes/SsoLanding.js'
import { getToken } from './lib/api.js'
import { i18n } from './i18n.js'
import './styles.css'

const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      // 切回标签页就重新拉：值班台上的数据几十秒就过期，
      // 拿着陈旧数据判断"现在有没有事"比看到转圈危险得多。
      refetchOnWindowFocus: true,
      retry: 1,
    },
  },
})

const root = document.getElementById('root')
if (!root) throw new Error('#root not found')

createRoot(root).render(
  <StrictMode>
    <QueryClientProvider client={queryClient}>
      <I18nextProvider i18n={i18n}>
        {/* 未登录直接给登录页，不进主应用：
            让未授权的骨架屏闪一下，看起来像数据泄露了一瞬间 */}
        {/* SSO 落地页要在**有没有令牌之前**判断：它的职责就是把令牌存下来。
            放在 getToken() 之后的话，回调回来时还没有令牌，
            会被直接渲染成登录页 —— 令牌白拿了一次，表现为"点了 SSO 又回到登录页" */}
        {location.hash.startsWith('#/sso') ? (
          <SsoLandingPage />
        ) : getToken() ? (
          <App />
        ) : (
          <SignInPage />
        )}
      </I18nextProvider>
    </QueryClientProvider>
  </StrictMode>,
)
