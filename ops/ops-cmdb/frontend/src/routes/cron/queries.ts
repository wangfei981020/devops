import type { ActionResult } from '../../lib/actionMessage.js'
import { shouldRetry } from '@ops/api'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { apiAction, apiGet } from '../../lib/fetchJson.js'

/**
 * 定时任务与它们的执行记录。
 *
 * ⚠️ 旧版把这两样拆成两个菜单，但人要回答的问题只有一个：
 * **这个任务跑成功了吗**。拆开意味着每次都要在两页之间跳，
 * 还得自己把执行记录对回是哪条任务。
 */
export interface ScheduledTask {
  task_key: string
  name: string
  enabled: number
  /** cron 表达式，原样显示——运维要拿它去核对，翻译成"每天 3 点"反而对不上 */
  schedule: string
  /** 空串 = 从没跑过。⚠️ 不能压成"很久以前" */
  last_run_at: string
  last_result: string
  /**
   * ⚠️ 这个字段默认是 1。
   *
   * 也就是说**从没执行过**的任务它也是 1 —— 直接拿来显示"正常"，
   * 会得到一个和同一行"上次执行：从没跑过"自相矛盾的结论，
   * 而且矛盾的方向偏向"没问题"。判跑没跑过一律看 last_run_at。
   */
  last_ok: number
  next_run_at: string
  notify_enabled: number
  lark_group_id: string | null
  notify_when: string
  at_user_ids: string[]
}

/** 一个没跑成的目标。⚠️ 有 reason，不要只显示 target —— 原因才是排障的落点 */
export interface TaskFailure {
  target: string
  reason: string
}

/**
 * 任务**查出来的问题**（不是任务本身失败）。
 *
 * ⚠️ 与 TaskFailure 严格区分：
 *   failure = 这次没跑成的目标（连不上 / 超时），要重试
 *   finding = 这次跑成了、并且查出来的问题（磁盘 94%、节点 NotReady），要处置
 * 后端专门加了这个结构，理由是「历史里只留下一句『危险 1 项』，等于没有告警」。
 * 前端不接的话，那个努力等于白做。
 */
export interface TaskFinding {
  level: string
  target: string
  value?: string
  detail?: string
}

export interface TaskRun {
  id: number
  task_key: string
  name: string
  /** ok / fail / running …… 原样透传 */
  status: string
  summary: string
  /**
   * ⚠️ 后端**一直都在返回**这个字段，前端类型里以前压根没声明。
   *
   * 于是「展开执行记录」和列表页看到的是同一句话
   * 「注册商到期同步完成: 更新 0 个, 无变化 0 个, 1 个数据源失败」——
   * 不说哪个数据源、不给原因，展开了等于没展开（OPSCMDB-031 P1-44）。
   * 而 failures 里其实有 target 和 reason（"凭据不可用：..."/ "拉取域名列表失败：..."）。
   */
  failures?: TaskFailure[]
  findings?: TaskFinding[]
  /** 飞书投递状态：sent / failed / skipped / none */
  notify_state?: string
  notify_group?: string
  notify_at?: string
  trigger_by: string
  duration_ms: number
  started_at: string
  finished_at: string
  progress: string
}

export function useScheduledTasks() {
  return useQuery({
    queryKey: ['scheduled-tasks'],
    queryFn: () => apiGet<ScheduledTask[]>('/api/scheduled-tasks'),
    staleTime: 15_000,
    retry: shouldRetry,
  })
}

/** 某条任务的执行记录；不传 taskKey 则是全局流水。 */
export function useTaskRuns(taskKey: string | undefined, size = 20) {
  const qs = new URLSearchParams({ size: String(size) })
  if (taskKey) qs.set('task_key', taskKey)
  return useQuery({
    queryKey: ['task-runs', taskKey ?? 'all', size],
    queryFn: () => apiGet<{ items: TaskRun[]; total?: number }>(`/api/task-runs?${qs}`),
    staleTime: 10_000,
    retry: shouldRetry,
  })
}

export function useRunTask() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (key: string) => apiAction(`/api/scheduled-tasks/${key}/run`, 'POST'),
    // 手动触发是异步的：成功只代表"任务收下了"，不代表跑完了。
    // 所以三个列表都要失效，让用户看到那条 running 记录出现
    //
    // ⚠️ `tasks` 也要失效：巡检页现在也能触发任务了，
    // 不失效的话在巡检页点了「立即运行」，那一行的状态**不会变**——
    // 看起来像按钮没生效，于是有人会连点好几次。
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: ['task-runs'] })
      void qc.invalidateQueries({ queryKey: ['scheduled-tasks'] })
      void qc.invalidateQueries({ queryKey: ['tasks'] })
    },
  })
}

export function useToggleTask() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: ({ key, enabled }: { key: string; enabled: boolean }) =>
      apiAction(`/api/scheduled-tasks/${key}`, 'PUT', { enabled: enabled ? 1 : 0 }),
    onSuccess: () => void qc.invalidateQueries({ queryKey: ['scheduled-tasks'] }),
  })
}

/**
 * 任务的频率与通知设置。
 *
 * 🔴 后端 `PUT /api/scheduled-tasks/:key` 一直收这五个字段
 * （schedule / notify_enabled / lark_group_id / notify_when / at_user_ids），
 * 前端**只发 enabled** —— 于是「改执行频率」「开关飞书通知」「发到哪个群」
 * 「什么时机」「@谁」这五件事新版界面一件也做不了，而旧版全都有。
 * 读取侧同样断着：那四个字段取回来零处渲染。
 * 也就是说这块配置**既看不见也改不了，但数据一直在库里生效**（OPSCMDB-039）。
 *
 * ⚠️ 只发**改动过**的字段。全量发的话，两个人同时开着这个弹窗时，
 * 后保存的那个会把前一个的改动整体覆盖回自己打开时的旧值。
 *
 * ⚠️ 名字必须以 Body/Input/Payload/Req 结尾：check-write-fields 靠这个后缀
 *	把接口认成"请求体声明"。叫 TaskSettingsPatch 的话守卫看不见它，
 *	而我们传给 apiAction 的是一个不透明对象、字段名不出现在调用处 ——
 *	于是守卫会继续报「后端收得下、前端一次都没发过」。
 *	那不是误报：从守卫的视角，确实没有任何证据表明这些字段发出去了。
 */
export interface TaskSettingsBody {
  schedule?: string
  notify_enabled?: number
  lark_group_id?: number | null
  notify_when?: string
  at_user_ids?: string[]
}

export function useUpdateTaskSettings() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: ({ key, patch }: { key: string; patch: TaskSettingsBody }) =>
      apiAction(`/api/scheduled-tasks/${key}`, 'PUT', patch),
    onSuccess: () => void qc.invalidateQueries({ queryKey: ['scheduled-tasks'] }),
  })
}

/**
 * 任务的健康判定。**三态**，不是布尔。
 *
 * 「从没跑过」和「跑过但失败」要采取的动作完全不同：
 * 前者是没注册上/被禁用了，后者是逻辑挂了。压成一个"异常"就分不出来。
 */
export type TaskHealth = 'never' | 'ok' | 'fail'

export function taskHealth(t: ScheduledTask): TaskHealth {
  if (!t.last_run_at) return 'never'
  return t.last_ok === 1 ? 'ok' : 'fail'
}

/**
 * 重跑一次执行里失败的那些项。
 *
 * ⚠️ 和「立即执行整个任务」不是一回事：那个会把已经成功的也再跑一遍。
 * 一次同步 200 个对象失败 3 个时，差别是 3 次外部调用和 200 次。
 */
export function useRetryFailures() {
  const qc = useQueryClient()
  return useMutation({
    retry: false,
    mutationFn: (runId: number) =>
      apiAction<ActionResult & { ok?: boolean }>(`/api/task-runs/${runId}/retry-failures`, 'POST'),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: ['task-runs'] })
      void qc.invalidateQueries({ queryKey: ['scheduled-tasks'] })
    },
  })
}

/**
 * 取消一次还在跑的执行。
 *
 * ⚠️ 取消**不会回滚已经做完的那部分** —— 半途停下的同步会留下一份写了一半的数据。
 * 界面上要说清楚，否则人会以为点了取消就当没发生过。
 */
export function useCancelRun() {
  const qc = useQueryClient()
  return useMutation({
    retry: false,
    mutationFn: (runId: number) =>
      apiAction<ActionResult & { ok?: boolean }>(`/api/task-runs/${runId}/cancel`, 'POST'),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: ['task-runs'] })
      void qc.invalidateQueries({ queryKey: ['scheduled-tasks'] })
    },
  })
}
