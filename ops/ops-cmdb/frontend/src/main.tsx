import { I18nextProvider } from '@ops/i18n'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import { RouterProvider } from '@tanstack/react-router'
import { router } from './router.js'
import { isSignedIn } from './lib/auth.js'
import { SignInPage } from './routes/SignIn.js'
import { i18n } from './i18n.js'
import './styles.css'

const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      // 切回标签页就重新拉：运维数据几分钟就过期，
      // 拿着陈旧数据做判断比看到转圈危险得多。
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
        {/* 未登录直接给登录页，不进路由 ——
            让未授权的骨架屏闪一下，看起来像数据泄露了一瞬间 */}
        {isSignedIn() ? <RouterProvider router={router} /> : <SignInPage />}
      </I18nextProvider>
    </QueryClientProvider>
  </StrictMode>,
)
