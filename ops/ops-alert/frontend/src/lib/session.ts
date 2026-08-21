/**
 * 当前会话的身份与权限。
 *
 * ⚠️ 权限**每次进应用都从 `/api/v1/me` 现取**，不用登录时那份缓存。
 * 角色是会在服务端被改的：管理员今天把某人的权限收走了，
 * 而他浏览器里还留着昨天登录时那份 —— 菜单照常显示、点进去 403，
 * 看起来像系统坏了。权限只有一个可信来源，就是后端此刻的答案。
 *
 * ⚠️ 前端这一层**只决定显不显示**，拦截全在后端
 * （internal/api/perm.go 的 PermGuard，未映射的路由 fail-closed）。
 * 改一行前端能多看见一个菜单，但拿不到数据。
 */

import { useQuery } from '@tanstack/react-query'
import { ApiError, api, getToken } from './api.js'

export interface Session {
  username: string
  displayName: string
  /** 登录来源，**只用于展示**，不参与任何权限判断 */
  authSource: string
  /**
   * 是否不受权限码约束。
   *
   * ⚠️ 判据一律以后端这个字段为准。前端**禁止**按 authSource
   * 之类自行推导"本地账号就是管理员" —— 这条在 CMDB 那边栽过三次，
   * 而且每次都是本地账号被降权之后才暴露。
   */
  isAdmin: boolean
  /** 权限码集合。后端给的是 {码: true}，这里转成 Set 便于判定 */
  permissions: Set<string>
}

interface RawMe {
  username?: string
  display_name?: string
  auth_source?: string
  is_admin?: boolean
  permissions?: Record<string, boolean> | null
}

/** 有没有某个权限码。管理员全放行，与后端 hasPerm 的语义一致。 */
export function can(session: Session | undefined, code: string): boolean {
  if (!session) return false
  if (session.isAdmin) return true
  return session.permissions.has(code)
}

export function useSession() {
  return useQuery({
    queryKey: ['session', 'me'],
    // 没 token 就不发请求：未登录时打 /me 只会拿一串 401，
    // 让人以为接口坏了
    enabled: getToken() !== null,
    queryFn: async (): Promise<Session> => {
      // 走本产品自己的 api()：它已经处理了 401（清 token + 回登录页）。
      // 单独写一份 fetch 的话，会话过期在这一处的表现会和全站不一样
      const d = await api<RawMe>('/me')
      return {
        username: d.username ?? '',
        displayName: d.display_name || (d.username ?? ''),
        authSource: d.auth_source ?? '',
        // ⚠️ 缺字段时兜底成 false 而不是 true。
        // 反过来会让一次接口异常把所有人临时变成管理员
        isAdmin: d.is_admin === true,
        permissions: new Set(
          Object.entries(d.permissions ?? {})
            .filter(([, v]) => v)
            .map(([k]) => k),
        ),
      }
    },
    // 权限变更后不该等到下次刷新才生效，但也不必每次切页都打一遍
    staleTime: 60_000,
    // 401/403 不重试：那是身份问题，重试多少次都一样，
    // 只会把登录页的出现推迟几秒
    retry: (count, err) => !(err instanceof ApiError && [401, 403].includes(err.status)) && count < 2,
  })
}
