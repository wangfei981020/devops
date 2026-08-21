import { shouldRetry } from '@ops/api'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { apiAction, apiGet } from '../../lib/fetchJson.js'

/**
 * 镜像仓库。数据是**实时问 Harbor API** 拿的，不落库。
 *
 * 落库的意义在于"回看历史"，而仓库清单没有历史价值 —— 要的永远是此刻的状态。
 * 代价是 Harbor 挂了这一页就打不开，而那恰恰是应该被看见的（见错误态）。
 */
export interface HarborRegistry {
  id: number
  name: string
  url: string
  env: string
  enabled: boolean
  /** ⚠️ 只有"配没配密码"，后端不回传密文也不回传明文 */
  has_secret: boolean
}

export interface HarborProject {
  name: string
  repo_count: number
  public: boolean
  /**
   * 存储用量。⚠️ **null = 取不到**（robot 账号没有配额读权限），不是"占了 0 GB"。
   *
   * 原来是 number，取不到时后端给零值，界面显示「0 GB」——
   * 198 个仓库的项目显示 0 GB，而「0 GB + 未设配额」读起来是
   * "这个项目没占空间、也没有限制"（OPSCMDB-031 P1-25）。
   */
  used_gb: number | null
  /** ⚠️ -1 表示**未设配额**，不是"配额为 0"。用量再高也不会被 Harbor 拦 */
  quota_gb: number
  /** ⚠️ 同样 -1 = 不适用。当成 0% 会让一个没配额的项目显示成"用量健康" */
  used_pct: number
  severity: string
}

export interface HarborRepo {
  name: string
  artifacts: number
  pulls: number
  updated: string
  /** 多久没推过了。长期没更新的仓库多半是遗留，但**不等于可以删** */
  days_since_push: number
}

export function useRegistries() {
  return useQuery({
    queryKey: ['harbor', 'registries'],
    queryFn: () => apiGet<{ items: HarborRegistry[] }>('/api/harbor/registries'),
    staleTime: 60_000,
    retry: shouldRetry,
  })
}

export function useHarborProjects(registryId: number) {
  return useQuery({
    queryKey: ['harbor', 'projects', registryId],
    enabled: registryId > 0,
    queryFn: () =>
      apiGet<{ projects: HarborProject[]; count: number; note?: string }>(
        `/api/harbor/projects?registry_id=${registryId}`,
      ),
    staleTime: 60_000,
    retry: shouldRetry,
  })
}

export function useHarborRepos(registryId: number, project: string) {
  return useQuery({
    queryKey: ['harbor', 'repos', registryId, project],
    enabled: registryId > 0 && project !== '',
    queryFn: () =>
      apiGet<{ repositories: HarborRepo[]; count: number }>(
        `/api/harbor/repositories?registry_id=${registryId}&project=${encodeURIComponent(project)}`,
      ),
    staleTime: 60_000,
    retry: shouldRetry,
  })
}

/** 接入一个 Harbor。字段与后端 handlers/harbor.go 的 Save 对齐。 */
export interface RegistryInput {
  id?: number
  name: string
  url: string
  username: string
  /** ⚠️ 留空 = 保持原密码不变（编辑时）。接口从不回传它 */
  password: string
  env: string
  cluster_id?: number
  /** 自签证书时才勾。默认 false —— 关掉校验的决定必须是显式的 */
  skip_verify: boolean
  enabled: boolean
}

export function useSaveRegistry() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: ({ id, ...body }: RegistryInput) =>
      id
        ? apiAction(`/api/harbor/registries/${id}`, 'PUT', body)
        : apiAction('/api/harbor/registries', 'POST', body),
    onSuccess: () => void qc.invalidateQueries({ queryKey: ['harbor'] }),
  })
}

export function useDeleteRegistry() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (id: number) => apiAction(`/api/harbor/registries/${id}`, 'DELETE'),
    onSuccess: () => void qc.invalidateQueries({ queryKey: ['harbor'] }),
  })
}

/**
 * 测连通。**保存后必须测一次**。
 *
 * 保存成功只代表字段写进库了：地址打错、密码过期、Harbor 那边把这个账号
 * 禁用了，全都要等到下一次真去拉数据时才暴露 —— 而那时候界面上
 * 这条接入看着是"已启用"。
 */
export function useTestRegistry() {
  return useMutation({
    mutationFn: (id: number) =>
      apiAction<{ ok?: boolean; error?: string; version?: string }>(
        `/api/harbor/registries/${id}/test`,
      ),
  })
}

/**
 * Harbor 自身的健康状态。
 *
 * ⚠️ 这里最值钱的是 `gc`：Harbor 删镜像只是打标记，**不跑 GC 磁盘不会释放**。
 * "从未执行过 GC" 是个会慢慢撑满盘的隐患，而它在任何列表页上都看不出来。
 */
export interface HarborStatus {
  ok?: boolean
  registry?: string
  health?: string
  component_count?: number
  projects?: number
  repositories?: number
  gc?: { last_status?: string; last_at?: string; issue?: string }
  error?: string
}

export function useHarborStatus(registryId: number) {
  return useQuery({
    queryKey: ['harbor-status', registryId],
    queryFn: () => apiGet<HarborStatus>(`/api/harbor/status?registry_id=${registryId}`),
    enabled: registryId > 0,
    staleTime: 60_000,
    retry: shouldRetry,
  })
}
