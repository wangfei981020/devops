import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import type { ReactNode } from 'react'
import { ApiError } from '../api/client.js'

/**
 * 全局查询客户端。
 *
 * ⚠️ **4xx 一律不重试**：401/403/428 是"结论"不是"抖动"，
 * 重试三次只会让用户多等两秒才看到登录页，而且会在审计里
 * 留下三条一模一样的失败记录。只有 5xx 和网络错误值得重试。
 */
const client = new QueryClient({
  defaultOptions: {
    queries: {
      retry: (count, err) => {
        if (err instanceof ApiError && err.status >= 400 && err.status < 500) return false
        return count < 2
      },
      staleTime: 15_000,
      refetchOnWindowFocus: false,
    },
    mutations: { retry: false },
  },
})

export function Query({ children }: { children: ReactNode }) {
  return <QueryClientProvider client={client}>{children}</QueryClientProvider>
}
