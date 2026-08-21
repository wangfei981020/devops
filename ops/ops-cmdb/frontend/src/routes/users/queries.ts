import { shouldRetry } from '@ops/api'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { apiGet, apiSend } from '../../lib/fetchJson.js'

export interface User {
  id: number
  username: string
  display_name: string
  /** local / portal / oidc —— **只用于展示，不参与权限判断** */
  auth_source: string
  /** ⚠️ 是否不受权限表约束。判据一律以后端这个字段为准（这条栽过三次） */
  is_admin: boolean
  role_code: string
  role_name: string
  active_sessions: number
  last_login_at: string
  created_at: string
  /**
   * 后端算好的"这一条能不能改"。前端**不要自己推导** ——
   * 比如"最后一个管理员不能降权"这种规则只有后端知道全局状态。
   */
  can_change_role: boolean
  can_change_password: boolean
  can_delete: boolean
  /** 这个账号归哪边管（CMDB / 运维平台）。外部来源的改不了 */
  editable_in: string
}

export function useUsers() {
  return useQuery({
    queryKey: ['users'],
    queryFn: async () => {
      const d = await apiGet<{ list?: User[]; admin_weak_password?: boolean }>('/api/users')
      return {
        items: d.list ?? [],
        // 🔴 本地 admin 仍在用已知默认口令。这个账号权限校验全放行，
        //	而后端只在启动日志里 WARN —— 日志没人天天看，必须显示在这一页
        //	（OPSCMDB-074）。
        // ⚠️ 用 === true 而不是真值判断：字段缺失（老后端）时应该是"不知道"，
        //	而不是"没问题"—— 但界面上"不知道"与"没问题"都不显示警告，
        //	区别在于**不要**因为 undefined 就断言安全。
        adminWeakPassword: d.admin_weak_password === true,
      }
    },
    staleTime: 30_000,
    retry: shouldRetry,
  })
}

function useDone() {
  const qc = useQueryClient()
  return () => void qc.invalidateQueries()
}

export function useCreateUser() {
  const done = useDone()
  return useMutation({
    mutationFn: (body: {
      username: string
      display_name: string
      password: string
      role_code: string
    }) => apiSend('/api/users', 'POST', body),
    onSuccess: done,
  })
}

export function useChangeRole() {
  const done = useDone()
  return useMutation({
    mutationFn: ({ id, role_code }: { id: number; role_code: string }) =>
      apiSend(`/api/users/${id}/role`, 'PUT', { role_code }),
    onSuccess: done,
  })
}

export function useResetPassword() {
  const done = useDone()
  return useMutation({
    mutationFn: ({ id, password }: { id: number; password: string }) =>
      apiSend(`/api/users/${id}/password`, 'PUT', { password }),
    onSuccess: done,
  })
}

/**
 * 踢下线。改完权限**必须**踢一次 —— 会话里存着旧的权限快照，
 * 不踢的话新权限要等他自己重新登录才生效，而他不会知道要重新登录。
 */
export function useKick() {
  const done = useDone()
  return useMutation({
    mutationFn: (id: number) => apiSend(`/api/users/${id}/kick`, 'POST'),
    onSuccess: done,
  })
}

export function useDeleteUser() {
  const done = useDone()
  return useMutation({
    mutationFn: (id: number) => apiSend(`/api/users/${id}`, 'DELETE'),
    onSuccess: done,
  })
}
