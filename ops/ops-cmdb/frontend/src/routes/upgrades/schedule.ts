import { shouldRetry } from '@ops/api'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { apiAction, apiGet } from '../../lib/fetchJson.js'

/**
 * GKE 官网版本排期表。
 *
 * ⚠️ `auto_upgrade_precision` 必须显示：官网给的很多是**月**或**季度**粒度
 * （2026-09 / 2026-Q4），显示成 `2026-09-01` 会被人当成确切日期去排停机窗口，
 * 而实际可能落在整个九月的任何一天。
 *
 * ⚠️ `is_manual` = 这一格被人工覆盖过，定时同步不会再冲掉它。
 * 不标出来的话，官网改了排期而这里不动，没人知道为什么。
 */
export interface ScheduleRow {
  id: number
  channel: string
  /** ⚠️ 后端字段名是 `minor_version`。之前声明成 version/minor —— 两个都不存在，
   *  于是版本列每一行都渲染成「—」，而数据一直好好地在那儿（同 OPSCMDB-013 那类）。 */
  minor_version?: string
  /** 支持截止（标准）。排停机窗口时它和自动升级日期一样重要 */
  eos_standard_at?: string
  eos_standard_days?: number
  auto_upgrade_raw: string
  auto_upgrade_at: string
  /** day / month / quarter —— 原样透传 */
  auto_upgrade_precision: string
  auto_upgrade_days?: number
  is_manual?: boolean
}

export function useVersionSchedule() {
  return useQuery({
    queryKey: ['gke-version-schedule'],
    queryFn: () => apiGet<{ ok?: boolean; rows: ScheduleRow[] }>('/api/gke/version-schedule'),
    staleTime: 5 * 60_000,
    retry: shouldRetry,
  })
}

/**
 * 手工覆盖某一格的自动升级日期。**官网解析出错时的兜底。**
 *
 * ⚠️ 覆盖之后打上 `is_manual=1`，定时同步不再更新这一行 ——
 * 也就是说官网后来改了排期，这里也不会跟着变。所以覆盖是有代价的，
 * 界面上必须能看出哪些格子被"钉住"了。
 */
export function useOverrideSchedule() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (v: {
      id: number
      auto_upgrade_raw: string
      auto_upgrade_at: string
      auto_upgrade_precision: string
    }) => apiAction(`/api/gke/version-schedule/${v.id}`, 'PUT', v),
    onSuccess: () => void qc.invalidateQueries({ queryKey: ['gke-version-schedule'] }),
  })
}

/** 清除覆盖，让这一格重新跟着官网同步走。 */
export function useClearOverride() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (id: number) => apiAction(`/api/gke/version-schedule/${id}/override`, 'DELETE'),
    onSuccess: () => void qc.invalidateQueries({ queryKey: ['gke-version-schedule'] }),
  })
}
