import { shouldRetry } from '@ops/api'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { apiAction, apiGet } from '../../lib/fetchJson.js'

export interface AuditLog {
  id: number
  username: string
  /** 动作码，原样显示：cloud_account.create / registrar.update …… */
  action: string
  target: string
  target_type: string
  status: string
  error_msg: string
  method: string
  path: string
  perm_code: string
  ip: string
  at: string
  duration_ms: number
  change_count: number
  actor_source: string
}

export function useAuditLogs(params: Record<string, string | number>) {
  const qs = new URLSearchParams(
    Object.entries(params).filter(([, v]) => v !== '' && v !== undefined).map(([k, v]) => [k, String(v)]),
  ).toString()
  return useQuery({
    queryKey: ['audit', qs],
    queryFn: async () => {
      const d = await apiGet<{ list?: AuditLog[]; total?: number }>(`/api/audit-logs?${qs}`)
      const items = d.list ?? []
      // ⚠️ 拿不到 total 就用条数，不要编一个。审计页的条数被人当证据用
      return { items, total: d.total ?? items.length }
    },
    staleTime: 15_000,
    retry: shouldRetry,
  })
}

/**
 * 一条审计记录下的具体字段变更。
 *
 * ⚠️ `revertable` 与 `revert_blocked_reason` 是**成对**的：
 * 不可回滚时必须把原因显示出来 —— 一个灰着的「回滚」按钮不给理由，
 * 人会以为是权限问题，然后去找管理员，而真正的原因可能是
 * "这条变更会改动外部系统（DNS 解析），暂不支持一键回滚"。
 */
export interface AuditChange {
  id: number
  seq: number
  table: string
  row_pk: string
  op: string
  /**
   * 字段级前后值。
   *
   * ⚠️ 键名是后端的 **`old` / `new`**（见 handlers/audit_record.go 的 diffMaps），
   * 不是 before/after。这里原来写的是 `{ before, after }` ——
   * 类型对不上不会报错（两边都是可选字段），运行时全部取到 undefined，
   * 于是**每一条变更都渲染成「— → —」**：数据一直好好记着，
   * 界面上却把「改了名字」显示成「从无到无」。
   *
   * ⚠️ 一共**三种形态**，少认一种就会显示错：
   *
   *   { old, new }        普通字段：前后值都给（可能被 maskValue 部分脱敏）
   *   { changed: true }   敏感字段（密码、token…）：只记"改过"，**不记值**
   *   { old, new: null }  字段被清空/删除
   *
   * 第二种是最容易漏的：只读 old/new 的话它俩都是 undefined，
   * 于是"改了密码"被渲染成「— → —」，看着像审计坏了 ——
   * 而实际上那是**正确且有意**的脱敏。不该看起来像出错的，也不能看起来像出错。
   */
  diff: Record<string, { old?: unknown; new?: unknown; changed?: boolean }> | null
  /** local / external / none —— 原样透传 */
  revert_kind: string
  revertable: boolean
  revert_blocked_reason: string
}

export function useAuditChanges(logID: number | null) {
  return useQuery({
    queryKey: ['audit-changes', logID],
    queryFn: () => apiGet<{ list: AuditChange[] }>(`/api/audit-logs/${logID}/changes`),
    enabled: !!logID,
    staleTime: 15_000,
    retry: shouldRetry,
  })
}

/**
 * 回滚一条字段变更。
 *
 * ⚠️ `force` 的语义是「我知道这条记录后来被别人改过，仍然要覆盖」。
 * 默认必须是 false —— 不带 force 的失败（409）是一道保护，
 * 前端不能为了"让它成功"就自动重试带上 force。
 */
export function useRevertChange() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (v: { cid: number; force: boolean }) =>
      apiAction(`/api/audit-changes/${v.cid}/revert`, 'POST', { force: v.force }),
    onSuccess: () => void qc.invalidateQueries(),
  })
}
