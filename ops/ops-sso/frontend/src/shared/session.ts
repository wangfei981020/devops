import { useQuery } from '@tanstack/react-query'
import { api } from '../api/client.js'
import type { Identity } from '../api/types.js'

/**
 * 当前登录身份。
 *
 * ⚠️ **权限判据一律以后端返回为准**，前端禁止按登录来源之类自行推导 ——
 * 这条在别的项目上栽过三次（CONVENTIONS §2.10）。
 */
export function useMe() {
  return useQuery({
    queryKey: ['me'],
    queryFn: () => api.get<Identity>('/auth/me'),
    // 401 不重试：没登录就是没登录，重试三次只是让用户多等两秒才看到登录页
    retry: false,
    staleTime: 30_000,
  })
}
