import { shouldRetry } from '@ops/api'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { apiAction, apiGet } from '../../lib/fetchJson.js'

/**
 * 通知人与飞书群。
 *
 * ⚠️ 这两份数据都是**租户级**的：A 租户的告警不该发进 B 租户的群。
 * 后端的 Scoped 层已经强制带租户条件，前端不需要传 tenant_id，
 * 但要知道自己看到的只是当前租户的那一份 —— 不要在文案里写成"全部群"。
 */
export interface NotifyUser {
  id: number
  name: string
  /** 飞书 open_id。⚠️ 不是密钥，但也不该整串到处显示 */
  open_id: string
  enabled: number
}

export interface LarkGroup {
  id: number
  name: string
  /** ⚠️ webhook 本身就是凭据：拿到它任何人都能往群里发消息 */
  webhook: string
}

export function useNotifyUsers() {
  return useQuery({
    queryKey: ['notify-users'],
    queryFn: () => apiGet<NotifyUser[]>('/api/notify-users'),
    staleTime: 30_000,
    retry: shouldRetry,
  })
}

export function useLarkGroups() {
  return useQuery({
    queryKey: ['lark-groups'],
    queryFn: async () => {
      // 🔴 后端改成了包装对象：除了群列表，还要告诉界面**全局兜底出口**配没配。
      //	它存在 settings.feishu_webhook 而不在 lark_groups 表里 ——
      //	只列表格的话，界面写着"一个群都没配"、任务记录却写着"已发送到全局兜底出口"，
      //	两句话直接打架（生产实测，OPSCMDB-080）。
      // ⚠️ 兼容裸数组：老后端还没升级时不能整页崩。
      const d = await apiGet<LarkGroup[] | { items?: LarkGroup[]; global_fallback_set?: boolean }>(
        '/api/lark-groups',
      )
      if (Array.isArray(d)) return { items: d, globalFallbackSet: undefined }
      return { items: d.items ?? [], globalFallbackSet: d.global_fallback_set === true }
    },
    staleTime: 30_000,
    retry: shouldRetry,
  })
}

function useDone(key: string) {
  const qc = useQueryClient()
  return () => void qc.invalidateQueries({ queryKey: [key] })
}

export function useCreateNotifyUser() {
  const done = useDone('notify-users')
  return useMutation({
    mutationFn: (body: { name: string; open_id: string }) =>
      apiAction('/api/notify-users', 'POST', body),
    onSuccess: done,
  })
}

export function useDeleteNotifyUser() {
  const done = useDone('notify-users')
  return useMutation({
    mutationFn: (id: number) => apiAction(`/api/notify-users/${id}`, 'DELETE'),
    onSuccess: done,
  })
}

export function useCreateLarkGroup() {
  const done = useDone('lark-groups')
  return useMutation({
    mutationFn: (body: { name: string; webhook: string }) =>
      apiAction('/api/lark-groups', 'POST', body),
    onSuccess: done,
  })
}

export function useDeleteLarkGroup() {
  const done = useDone('lark-groups')
  return useMutation({
    mutationFn: (id: number) => apiAction(`/api/lark-groups/${id}`, 'DELETE'),
    onSuccess: done,
  })
}

/**
 * 发一条测试消息。
 *
 * ⚠️ 这是这一页**唯一**能证明配置有效的动作。
 * 配置保存成功只代表字段写进库了，不代表 webhook 还有效、群还在、
 * 机器人还没被移出群 —— 而这三件事任何一件出问题，
 * 告警都会静默发不出去，且任务仍然显示"成功"。
 */
export function useTestNotify() {
  return useMutation({
    mutationFn: () => apiAction<{ ok?: boolean; msg?: string }>('/api/notify/test', 'POST'),
  })
}

/**
 * 给某个飞书群发一条测试消息。
 *
 * ⚠️ 和「通知测试」（/api/notify/test）不是一回事：那个走的是默认通道。
 * 群配错了 webhook 时，只有逐个群测才知道是哪一个坏了 ——
 * 而「告警发不出去」这件事，平时是完全看不见的。
 */
/**
 * 改群名 / 换 webhook。
 *
 * ⚠️ 以前只能建和删。webhook 换了（群重建、机器人重加）就只能删掉重建 ——
 * 而删除会连带丢掉这个群在别处的绑定关系。
 */
export function useUpdateLarkGroup() {
  const done = useDone('lark-groups')
  return useMutation({
    mutationFn: ({ id, ...b }: { id: number; name: string; webhook: string }) =>
      apiAction(`/api/lark-groups/${id}`, 'PUT', b),
    onSuccess: done,
  })
}

export function useTestLarkGroup() {
  return useMutation({
    retry: false,
    mutationFn: (id: number) =>
      apiAction<{ ok?: boolean; msg?: string }>(`/api/lark-groups/${id}/test`, 'POST'),
  })
}
