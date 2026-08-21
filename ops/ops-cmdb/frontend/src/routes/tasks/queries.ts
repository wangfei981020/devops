export interface Task {
  task_key: string
  name: string
  enabled: boolean
  schedule: string
  last_run_at?: string
  last_result: string
  /** ⚠️ null = 从没跑过，无从谈成败。当成 false（失败）或 true（成功）都是撒谎 */
  last_ok: boolean | null
  /**
   * ⚠️ 早该跑了却没跑 —— 这是"调度器静默停摆"的唯一信号。
   * 调度器挂掉时任务不会报错，它只是**不再执行**，
   * 界面上永远显示着上一次的成功结果，而数据在慢慢变旧。
   */
  overdue: boolean
  notify_enabled: boolean
}

export type TaskState = 'disabled' | 'never' | 'failed' | 'overdue' | 'ok'

export function taskState(t: Task): TaskState {
  if (!t.enabled) return 'disabled'
  // ⚠️ 先看有没有跑过。last_ok 是带默认值的列，从没执行过时也可能是 true ——
  // 只信它的话，一个从没被调度到的任务会显示「正常」
  if (!t.last_run_at || t.last_ok === null) return 'never'
  if (!t.last_ok) return 'failed'
  if (t.overdue) return 'overdue'
  return 'ok'
}

import { shouldRetry } from '@ops/api'
import { useQuery } from '@tanstack/react-query'
import { apiGet } from '../../lib/fetchJson.js'

export interface TaskListResult {
  items: Task[]
  total: number
  /** 分面缺失 ≠ 计数为 0，前端不显示计数而不是显示 0 */
  facets: Record<string, Record<string, number> | undefined>
}

export function useTasks(params: Record<string, string | number>) {
  const qs = new URLSearchParams(
    Object.entries(params).filter(([, v]) => v !== '' && v !== undefined).map(([k, v]) => [k, String(v)]),
  ).toString()
  return useQuery({
    queryKey: ['tasks', qs],
    queryFn: () => apiGet<TaskListResult>(`/api/task-list?${qs}`),
    staleTime: 30_000,
    retry: shouldRetry,
  })
}
