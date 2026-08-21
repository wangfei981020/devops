import { shouldRetry } from '@ops/api'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { apiAction, apiGet } from '../../lib/fetchJson.js'

/**
 * 基础字典：环境 / 业务项目 / 生命周期状态。
 *
 * ⚠️ 全站所有下拉的取值都来自这里。前端任何地方写死一份枚举都会和这里分叉 ——
 * 已经栽过一次（角色下拉写死了两个数据库里不存在的角色）。
 */
export interface Env {
  id: number
  code: string
  name: string
  /** 英文显示名，可空 */
  name_en?: string
  /**
   * 徽章样式。旧版 Element Plus 的调色名（danger/warning/primary/success/info）。
   * ⚠️ 不能直接传给 Badge，词表不通用 —— 走 lib/envTone.ts 映射。
   */
  tag_type?: string
  sort_order?: number
}
export interface Project {
  id: number
  name: string
  remark?: string
  status?: string
  sort_order?: number
}
/**
 * 生命周期状态。
 *
 * ⚠️ 字段名和别的字典**不一样**：这里是 scope + label，不是 code + name。
 * 照着别的字典写会得到一排空标签 —— 不报错，就是空的。
 */
export interface LifecycleStatus {
  id: number
  /** domain / host / … 这个状态适用于哪类对象 */
  scope: string
  label: string
  color?: string
}
export interface CiType {
  id: number
  code: string
  name: string
}

export function useBasicDicts() {
  return useQuery({
    queryKey: ['basic-dicts'],
    queryFn: async () => {
      const [envs, projects, ciTypes, statuses] = await Promise.all([
        apiGet<Env[]>('/api/environments'),
        apiGet<Project[]>('/api/projects'),
        apiGet<CiType[]>('/api/ci-types'),
        apiGet<LifecycleStatus[]>('/api/lifecycle-statuses'),
      ])
      return { envs, projects, ciTypes, statuses }
    },
    staleTime: 60_000,
    retry: shouldRetry,
  })
}

function useDone() {
  const qc = useQueryClient()
  return () => void qc.invalidateQueries({ queryKey: ['basic-dicts'] })
}

/**
 * 新建/修改环境的请求体。
 *
 * ⚠️ 名字以 Body 结尾是 check-write-fields 的约定：它靠这个后缀
 * 把接口认成"请求体声明"。叫别的名字守卫看不见，会继续报
 * 「后端收得下、前端一次都没发过」—— 而那不是误报。
 */
export interface EnvBody {
  code: string
  name: string
  name_en?: string
  /** 徽章样式。旧版调色名，见 lib/envTone.ts */
  tag_type?: string
}

export function useCreateEnv() {
  const done = useDone()
  return useMutation({
    mutationFn: (b: EnvBody) => apiAction('/api/environments', 'POST', b),
    onSuccess: done,
  })
}

/**
 * 删除环境。
 *
 * ⚠️ 后端会先数"还有多少资产在用"，有引用时返回 409 并说明数量 ——
 * 前端**不要**把这个错误吞掉或改写成"删除失败"，那句话里的数字
 * 正是用户接下来要处理的工作量。
 */
export function useDeleteEnv() {
  const done = useDone()
  return useMutation({
    mutationFn: (id: number) => apiAction(`/api/environments/${id}`, 'DELETE'),
    onSuccess: done,
  })
}

export function useCreateProject() {
  const done = useDone()
  return useMutation({
    mutationFn: (b: { name: string; remark: string }) => apiAction('/api/projects', 'POST', b),
    onSuccess: done,
  })
}

export function useDeleteProject() {
  const done = useDone()
  return useMutation({
    mutationFn: (id: number) => apiAction(`/api/projects/${id}`, 'DELETE'),
    onSuccess: done,
  })
}

// ⚠️ 下面这些以前都没有前端入口：环境和项目**只能建和删，不能改**。
// 想把一个环境改个名，得先删掉再建 —— 而删除会被"还有 N 个资产在用"挡住，
// 于是这件事在界面上根本做不成。后端 PUT 一直都在。

export function useUpdateEnv() {
  const done = useDone()
  return useMutation({
    // name_en 可选：留空时英文界面回退显示中文名（见 lib/dicts.ts 的 dictLabel）
    mutationFn: ({ id, ...b }: EnvBody & { id: number }) =>
      apiAction(`/api/environments/${id}`, 'PUT', b),
    onSuccess: done,
  })
}

export function useUpdateProject() {
  const done = useDone()
  return useMutation({
    mutationFn: ({ id, ...b }: { id: number; name: string; remark: string }) =>
      apiAction(`/api/projects/${id}`, 'PUT', b),
    onSuccess: done,
  })
}

/**
 * 生命周期状态的增删改。
 *
 * ⚠️ 这一组在界面上一直被标成「系统内置」而只读 —— 那个标注是错的：
 * 后端三个写接口都在，它本来就是可配置字典。
 * 一个被写死成只读的可配置项，比没有这个功能更难发现。
 *
 * ⚠️ 字段是 scope + label，不是 code + name（见 LifecycleStatus）。
 */
export function useCreateStatus() {
  const done = useDone()
  return useMutation({
    mutationFn: (b: { scope: string; label: string; color?: string; sort_order?: number }) =>
      apiAction('/api/lifecycle-statuses', 'POST', b),
    onSuccess: done,
  })
}

export function useUpdateStatus() {
  const done = useDone()
  return useMutation({
    mutationFn: ({ id, ...b }: { id: number; label: string; color?: string; sort_order?: number }) =>
      apiAction(`/api/lifecycle-statuses/${id}`, 'PUT', b),
    onSuccess: done,
  })
}

export function useDeleteStatus() {
  const done = useDone()
  return useMutation({
    mutationFn: (id: number) => apiAction(`/api/lifecycle-statuses/${id}`, 'DELETE'),
    onSuccess: done,
  })
}


/**
 * 系统设置：提醒天数、审计保留期、飞书 Webhook 等。
 *
 * ⚠️ 保留期填 0 的语义是**永不清理**，不是"立刻清理"。
 * 这两个理解方向相反，界面上必须说清楚 —— 有人以为填 0 会关掉审计，
 * 实际是审计永远不删，库会一直涨。
 */
export function useSettings() {
  return useQuery({
    queryKey: ['settings'],
    queryFn: () => apiGet<Record<string, string>>('/api/settings'),
    staleTime: 60_000,
    retry: shouldRetry,
  })
}

export function useSaveSettings() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (patch: Record<string, string>) => apiAction('/api/settings', 'PUT', patch),
    onSuccess: () => void qc.invalidateQueries({ queryKey: ['settings'] }),
  })
}
