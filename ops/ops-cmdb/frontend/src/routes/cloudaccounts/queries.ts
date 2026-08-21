import { shouldRetry } from '@ops/api'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { apiAction, apiGet, apiSend } from '../../lib/fetchJson.js'

/**
 * 云账号接入（GCP）。
 *
 * 结构是两层：**账号**下面挂若干**项目**，SA key 配在项目上。
 * 这么设计是因为 GCP 的权限就是按 project 给的 —— 一个 SA key 只能读它自己那个
 * project，硬要在账号层配一份"总 key"，要么权限过大，要么根本读不到别的项目。
 */
export interface CloudProject {
  id: number
  name: string
  project_id: string
  /** ⚠️ 只有"配没配"，接口不返回 SA key 内容 */
  has_cred: boolean
  last_sync_at: string
  last_result: string
  /**
   * 上次同步失败了。**由后端判定**，前端不再拿正则去认成功词。
   *
   * 🔴 原来是 /成功|完成|同步 \d+/ 这种**正向匹配** —— 一条措辞不同的
   * 成功消息就会被标成"需要注意"，于是两条同样成功的同步一绿一橙
   * （OPSCMDB-031 P2-53）。判据用否定式（认失败词）才不会随文案漂移。
   */
  last_failed?: boolean
  host_count: number
}

export interface CloudAccount {
  id: number
  name: string
  provider: string
  billing_export_dataset?: string
  projects?: CloudProject[]
}

export function useCloudAccounts() {
  return useQuery({
    queryKey: ['cloud-accounts'],
    // ⚠️ 这个老接口返回的是**裸数组**，不是 httpx 的 { items, total } 契约。
    // 直接当成 { items } 用的话 `d.items` 是 undefined，
    // 而崩点会出现在渲染期（读 .length），整页白屏 —— 不是一个能一眼看懂的报错。
    // 重构期两套形状并存，这里显式收敛，别指望它们长一样。
    queryFn: async () => {
      const d = await apiGet<CloudAccount[] | { items?: CloudAccount[] }>('/api/cloud-accounts')
      return { items: Array.isArray(d) ? d : (d.items ?? []) }
    },
    staleTime: 30_000,
    retry: shouldRetry,
  })
}

function useInvalidate() {
  const qc = useQueryClient()
  // 账号/项目一变，主机列表、成本、数据源页全都受影响。
  // 逐个失效容易漏，整体重取最省心
  return () => void qc.invalidateQueries()
}

export function useCreateAccount() {
  const done = useInvalidate()
  return useMutation({
    mutationFn: (body: { name: string; provider: string; billing_export_dataset?: string }) =>
      apiSend('/api/cloud-accounts', 'POST', body),
    onSuccess: done,
  })
}

export function useUpdateAccount() {
  const done = useInvalidate()
  return useMutation({
    mutationFn: ({ id, ...body }: { id: number; name: string; billing_export_dataset?: string }) =>
      apiSend(`/api/cloud-accounts/${id}`, 'PUT', body),
    onSuccess: done,
  })
}

export function useDeleteAccount() {
  const done = useInvalidate()
  return useMutation({
    mutationFn: (id: number) => apiSend(`/api/cloud-accounts/${id}`, 'DELETE'),
    onSuccess: done,
  })
}

export function useCreateProject() {
  const done = useInvalidate()
  return useMutation({
    mutationFn: ({ accountId, ...body }: { accountId: number; name: string; project_id: string; cred_json: string }) =>
      apiSend(`/api/cloud-accounts/${accountId}/projects`, 'POST', body),
    onSuccess: done,
  })
}

/**
 * 改项目的名字 / 换凭据。
 *
 * ⚠️ 以前只能加和删。凭据轮换（SA key 到期、被吊销）时只能删掉重建 ——
 * 而删除会把这个项目底下已采集的主机关联一并抹掉。
 *
 * ⚠️ `cred_json` **留空 = 不改凭据**，不是"清空凭据"。这两个理解方向相反：
 * 按后者做的话，每次改个名字都会把凭据洗掉，而症状要等到下次同步才出现。
 */
export function useUpdateProject() {
  const done = useInvalidate()
  return useMutation({
    mutationFn: ({ id, ...body }: { id: number; name: string; project_id: string; cred_json: string }) =>
      apiSend(`/api/cloud-projects/${id}`, 'PUT', body),
    onSuccess: done,
  })
}

/** 单个项目的同步进度。 */
export interface CloudSyncProject {
  project_id: number
  project: string
  running: boolean
  total: number
  done: number
  synced: number
  stale: number
  error: string
}

/**
 * 一轮后台同步的实时进度（后端按账号聚合 + 每项目明细）。
 *
 * ⚠️ `started=false` 表示后端**这个进程内**没有这个账号的同步记录，
 * 不等于"从没同步过"——进程重启后就没了。判"跑没跑过"要看
 * 项目行上的 `last_sync_at`，不要用这个字段。
 */
export interface CloudSyncStatus {
  running: boolean
  started: boolean
  total?: number
  done?: number
  synced?: number
  stale?: number
  error?: string
  projects?: CloudSyncProject[]
}

/**
 * 轮询后台同步进度。
 *
 * # 为什么必须无条件挂载，而不是"点了同步才开始查"
 *
 * 同步可能是**别人触发的**，也可能是用户在同步途中**刷新了页面**。
 * 只在本次点击之后才轮询的话，这两种情况下界面又会退回"看着像没同步"——
 * 而那正是这个功能要修的毛病本身。
 *
 * 跑完就停：`refetchInterval` 返回 false，不会一直空转。
 * 点同步后由调用方 `refetch()` 一次把轮询重新点着
 * （后端在返回 202 之前就登记了进度，所以立刻查得到 running=true）。
 */
export function useSyncStatus(accountId: number) {
  return useQuery({
    queryKey: ['cloud-sync-status', accountId],
    queryFn: () => apiGet<CloudSyncStatus>(`/api/cloud-accounts/${accountId}/sync-status`),
    // 1.5 秒一轮：同步通常几秒到几十秒，再快只是徒增请求。
    //
    // 🔴 判据必须**account 级或任一 project 级在跑**，不能只看 account 级。
    //
    //	只看 `data.running` 的后果：点某一个项目的同步时，账号级 running 是 false，
    //	于是**轮询根本没启动** → dataUpdatedAt 永不更新 →
    //	页面那个 `fresh = dataUpdatedAt > startedAt` 永远为 false →
    //	`pending` 永远清不掉 → 「同步中…」永不结束（生产 + 本地各实测一次，
    //	连续采样 24 秒进度停在 62/62 不动，OPSCMDB-075）。
    //
    //	后端一直是对的：那一刻 account 与三个 project 的 running 全是 false，
    //	是前端在自己骗自己。
    refetchInterval: (q) => {
      const d = q.state.data
      const anyRunning = (d?.running ?? false) || (d?.projects?.some((p) => p.running) ?? false)
      return anyRunning ? 1500 : false
    },
    retry: shouldRetry,
  })
}

export function useSyncAccount() {
  const done = useInvalidate()
  return useMutation({
    mutationFn: (id: number) => apiSend(`/api/cloud-accounts/${id}/sync`, 'POST'),
    // 同步是后台异步的，这里成功只代表"任务收下了"，不代表数据到了。
    // 真正的完成由 useSyncStatus 轮询出来 —— 那时候才刷新列表
    onSuccess: done,
  })
}

/**
 * 单个云项目的操作。
 *
 * ⚠️ 「立即同步」是**异步**的：成功只代表任务收下了，不代表数据已经更新。
 * 界面上不能因此显示"同步完成"——那会让人立刻去看数据，然后以为同步没生效。
 */
export function useSyncProject() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (pid: number) => apiAction(`/api/cloud-projects/${pid}/sync`, 'POST'),
    // 这里只是把"任务已受理"落到界面上。数据真正到位是在轮询看到
    // running 变 false 之后 —— 那一刻才重新拉列表（见 index.tsx 的 useEffect）
    onSuccess: () => void qc.invalidateQueries({ queryKey: ['cloud-accounts'] }),
  })
}

/**
 * 删除一个云项目。
 *
 * ⚠️ 这会连带停掉这个项目下所有资源的采集。已采集的历史数据不会被删，
 * 但会**从此不再更新**——而列表里那些主机看起来仍然正常，
 * 只是"最后同步"停在删除那一刻。
 */
export function useDeleteProject() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (pid: number) => apiAction(`/api/cloud-projects/${pid}`, 'DELETE'),
    onSuccess: () => void qc.invalidateQueries({ queryKey: ['cloud-accounts'] }),
  })
}
