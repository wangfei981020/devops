import { ApiError, createCmdbClient, normalizeError } from '@ops/api'
import { getToken, signOut } from './auth.js'

/**
 * 本应用的 API 客户端。
 *
 * baseUrl 用相对路径 `/api`：把后端地址编进构建产物的话，换个环境就得重新构建。
 * 走相对路径，同一个镜像能跑遍所有环境，由 nginx 决定反代到哪。
 */
export const api = createCmdbClient({
  baseUrl: '/api',
  headers: () => {
    const t = getToken()
    const h: Record<string, string> = {}
    if (t) h.Authorization = `Bearer ${t}`
    return h
  },
  onUnauthorized: () => {
    // token 过期就地清掉并回登录页。
    // 不这么做的话，用户会看到满屏"登录已失效"的错误态却不知道该干什么。
    signOut()
  },
})

export { ApiError, normalizeError }
