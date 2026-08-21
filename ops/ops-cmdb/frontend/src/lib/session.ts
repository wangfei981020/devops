/**
 * 当前会话的身份与权限。
 *
 * ⚠️ 权限**每次进应用都从 `/api/me` 现取**，不用登录时那份缓存。
 * 角色是会在服务端被改的：管理员今天把某人的权限收走了，
 * 而他浏览器里还留着昨天登录时那份 —— 菜单照常显示、点进去 403，
 * 看起来像系统坏了。权限只有一个可信来源，就是后端此刻的答案。
 *
 * ⚠️ 前端这一层**只决定显不显示**，拦截全在后端（`permPrefixRules`，
 * 未映射的路由 fail-closed）。改一行前端能多看见一个菜单，但拿不到数据。
 */

import { ApiError, normalizeError, shouldRetry } from '@ops/api'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { getToken, signOut } from './auth.js'
import { apiGet } from './fetchJson.js'

export interface Session {
  username: string
  displayName: string
  /** 登录来源，**只用于展示**，不参与任何权限判断 */
  authSource: string
  /**
   * 是否不受权限表约束。
   *
   * ⚠️ 判据一律以后端这个字段为准。前端**禁止**按 authSource
   * 之类自行推导"本地账号就是管理员"—— 这条已经栽过三次，
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

/** 有没有某个权限码。管理员全放行，与后端 `IsAdmin` 的语义一致。 */
export function can(session: Session | undefined, code: string): boolean {
  if (!session) return false
  if (session.isAdmin) return true
  return session.permissions.has(code)
}

export function useSession() {
  return useQuery({
    queryKey: ['session', 'me'],
    // 没 token 就不发请求：未登录时打 /api/me 只会拿一串 401，
    // 让人以为接口坏了
    enabled: getToken() !== null,
    queryFn: async (): Promise<Session> => {
      // 走裸 fetch 而不是生成的客户端：/api/me 没进 OpenAPI 注解
      // （它不是业务接口）。但抛出的仍是 ApiError，
      // 否则这一处的错误处理会和全站不一样，AsyncBoundary 也接不住。
      let res: Response
      try {
        res = await fetch('/api/me', { headers: { Authorization: `Bearer ${getToken()}` } })
      } catch (e) {
        throw new ApiError(normalizeError(e, { method: 'GET', path: '/api/me' }))
      }
      if (!res.ok) {
        // token 过期就地回登录页。不这么做的话，用户看到的是
        // 一个"权限加载失败"的错误态，而真正该做的事是重新登录
        if (res.status === 401) signOut()
        throw new ApiError(
          normalizeError(await res.json().catch(() => ({})), {
            method: 'GET',
            path: '/api/me',
            status: res.status,
          }),
        )
      }
      const d = (await res.json()) as RawMe
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
    retry: shouldRetry,
  })
}

/**
 * 刷新我的权限（不重登）。
 *
 * # 为什么需要
 *
 * 管理员在运维平台改完角色后，用户这边**要等会话过期才生效** ——
 * 中间那段时间菜单是错的：被撤权的还看得见入口（点进去 403），
 * 新加权限的还看不见。
 *
 * # 🔴 `stale: true` 绝不能显示成"刷新成功"
 *
 * 后端拉不到最新权限时（网络抖动 / portal token 过期）**沿用旧快照**
 * 并置 `stale: true` —— 这是对的（刷新失败不该把人踢出去），
 * 但界面上必须说清楚"这次没刷到，你看到的还是旧的"。
 *
 * 说成"已刷新"是这里最坏的失败方式：用户会据此认为菜单已经是最新的，
 * 然后把一个仍然错的菜单当成事实。
 */
export function useRefreshPerms() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: () =>
      apiGet<{ permissions?: Record<string, boolean>; is_admin?: boolean; stale?: boolean }>(
        '/api/refresh-permissions',
      ),
    onSuccess: () => {
      // 刷到了就把会话查询作废，让侧栏按新权限重算
      void qc.invalidateQueries({ queryKey: ['session', 'me'] })
    },
  })
}
