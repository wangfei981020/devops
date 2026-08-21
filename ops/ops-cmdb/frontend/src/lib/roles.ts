import { shouldRetry } from '@ops/api'
import { useQuery } from '@tanstack/react-query'
import { apiGet } from './fetchJson.js'

/**
 * 可分配的角色，**从接口取**。
 *
 * # 为什么不能在前端写死
 *
 * 写死过，然后两份真相就分叉了：界面上给出 `cmdb_operator` 和 `cmdb_security`
 * 两个选项——**数据库里根本没有这两个角色**；而真实存在的 `cmdb_asset`、
 * `cmdb_cluster` 在界面上一个都选不到。
 *
 * 后果分两种，都不好查：
 *   - 选了不存在的：后端校验拒绝（如果它校验了），报"角色不存在"，
 *     而用户明明是从下拉里选的，会以为是系统坏了
 *   - 后端没校验的地方：写进去一个空权限的角色代号，
 *     那个账号从此每个页面都 403，而列表里它看着一切正常
 *
 * 同一份清单当时散在三个页面里（用户管理、SSO 的 JIT 角色、MCP 令牌），
 * 改一处漏两处几乎是必然的。所以收口成这一个 hook。
 *
 * ⚠️ 同理适用于所有**字典类**下拉：环境、项目、CI 类型、通知渠道。
 * 判据很简单：这份清单的真相在数据库里，那前端就不该有第二份。
 */
export interface Role {
  code: string
  name: string
  description: string
  perm_count: number
  is_builtin: boolean
  /** 不受权限码约束（管理员）。它的 perm_count 再大也不是靠权限码生效的 */
  unrestricted: boolean
  /**
   * 具体的权限码。用户与角色页把它翻成"能进哪些页面、能做哪些操作"。
   *
   * ⚠️ 只有 perm_count 回答不了"这个角色能做什么"——
   * 而那正是分配角色时唯一要问的问题（OPSCMDB-031 P1-67）。
   */
  permissions: string[]
}

export function useRoles() {
  return useQuery({
    queryKey: ['local-roles'],
    queryFn: async () => {
      const d = await apiGet<{ list: Role[] }>('/api/local-roles')
      return d.list ?? []
    },
    // 角色几乎不变，缓存久一点；但不能是 Infinity——
    // 加了新角色之后不该要求用户刷新整页才看得到
    staleTime: 5 * 60_000,
    retry: shouldRetry,
  })
}

/**
 * 角色下拉的选项。
 *
 * ⚠️ 取不到时返回**空数组**，而不是回退到一份写死的清单。
 * 回退的话，用户会在接口挂掉时看到一个"看起来正常"的下拉，
 * 选完保存失败——而失败原因和下拉里那几项毫无关系。
 * 空下拉至少能让人看出是这里没数据。
 */
export function roleOptions(roles: Role[] | undefined) {
  return (roles ?? []).map((r) => ({
    value: r.code,
    label: r.unrestricted ? `${r.name}（不受限）` : r.name,
  }))
}
