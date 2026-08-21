import { actionMessage } from '../../lib/actionMessage.js'
import { toErrorInfo } from '@ops/api'
import { tError, useTranslation } from '@ops/i18n'
import {
  AsyncBoundary,
  Badge,
  type BadgeTone,
  EmptyState,
  fromQuery,
  type LoadError,
  Skeleton,
} from '@ops/ui'
import { useSearch } from '@tanstack/react-router'
import { useState } from 'react'
import { WriteButton } from '../../components/WriteButton.js'
import { TaskSettingsDialog } from './SettingsDialog.js'
import { can, useSession } from '../../lib/session.js'
import {
  type ScheduledTask,
  type TaskFailure,
  type TaskFinding,
  type TaskHealth,
  type TaskRun,
  taskHealth,
  useCancelRun,
  useRetryFailures,
  useRunTask,
  useScheduledTasks,
  useTaskRuns,
  useToggleTask,
} from './queries.js'

const PERM = 'cmdb:run_task'
/** 执行记录是独立权限：能看任务 ≠ 能看它每次跑出了什么 */
const RUNS_PERM = 'menu:cmdb_task_runs'

const HEALTH_TONE: Record<TaskHealth, BadgeTone> = { never: 'mute', ok: 'ok', fail: 'bad' }

/**
 * 定时任务。
 *
 * 任务列表 + 每条任务的执行记录合成一页（旧版是两个菜单）。
 *
 * ⚠️ 这一页最容易出的错是**把"从没跑过"显示成"正常"**：
 * `last_ok` 这一列默认值是 1，直接拿来判健康，一个从来没被调度过的任务
 * 会显示绿色的"正常"，而同一行的"上次执行"写着"从没跑过"。
 * 自相矛盾，且偏向"没问题"——所以判据一律看 last_run_at（见 queries.ts）。
 */
export function CronPage() {
  const { t } = useTranslation()
  const query = useScheduledTasks()
  // 初始展开哪一条来自 URL：从巡检页点「看执行记录」跳过来时，
  // 要直接落在那条任务上并展开，而不是让人在 15 条里再找一遍。
  //
  // ⚠️ 只作为**初始值**，之后由用户的点击接管 ——
  // 做成完全受 URL 控制的话，点别的任务就得改 URL，而后退键会变成"折叠上一条"，
  // 那不是人期望的后退行为
  const { task: fromUrl } = useSearch({ from: '/runtime/cron' })
  const [openKey, setOpenKey] = useState<string | null>(fromUrl || null)
  const canSeeRuns = can(useSession().data, RUNS_PERM)
  // 只看需要处理的。默认关 —— 一进来就藏掉大半列表会让人以为任务丢了
  const [onlyBroken, setOnlyBroken] = useState(false)

  const toLoadError = (e: unknown): LoadError => {
    const n = toErrorInfo(e)
    return { cause: tError(t, n.messageKey, n.params), detail: n.detail, retryable: n.retryable, kind: n.kind === 'license' ? 'license' : undefined, feature: n.params?.feature as string | undefined }
  }

  return (
    <div className="mx-auto max-w-[1080px] p-5">
      <div className="mb-4 flex items-baseline gap-2.5">
        <h1 className="text-sm font-semibold text-foreground">{t('cron:title')}</h1>
        <p className="min-w-0 flex-1 text-xs text-muted-foreground">{t('cron:hint')}</p>
        {/* 🔴 「有几个坏了」必须在这一页说。
            这是**唯一能操作**的一页（立即执行 / 启停 / 执行记录），
            而它原来没有任何筛选和汇总 —— 于是"看到失败的那一页不能操作，
            能操作的那一页找不到失败的"（OPSCMDB-031 P1-43）。

            ⚠️ 这个数字必须在 AsyncBoundary **外面**算不了，所以放到里面渲染；
            这里只留标题行。 */}
      </div>

      <AsyncBoundary
        state={fromQuery(query, (d) => d.length === 0, toLoadError)}
        errorTitle={t('cron:error.title')}
        retryLabel={t('common:action.retry')}
        onRetry={() => void query.refetch()}
        pending={<Skeleton className="h-5 w-[60%]" />}
        empty={
          <EmptyState
            title={t('cron:empty.title')}
            // 一条定时任务都没有 = 迁移里的种子没跑，不是"这个系统不需要定时任务"
            reason={t('cron:empty.reason')}
            action={null}
          />
        }
      >
        {(tasks) => (
          <div className="flex flex-col">
            <BrokenBar
              tasks={tasks}
              t={t}
              onlyBroken={onlyBroken}
              onToggle={() => setOnlyBroken((v) => !v)}
            />
            {/* 失败的排前面：人是来找"哪个挂了"的 */}
            {[...tasks]
              .filter((x) => !onlyBroken || taskHealth(x) !== 'ok')
              .sort(byHealth)
              .map((task) => (
                <TaskRow
                  key={task.task_key}
                  task={task}
                  t={t}
                  canSeeRuns={canSeeRuns}
                  open={openKey === task.task_key}
                  onToggleOpen={() =>
                    setOpenKey((k) => (k === task.task_key ? null : task.task_key))
                  }
                />
              ))}
          </div>
        )}
      </AsyncBoundary>
    </div>
  )
}

/**
 * 「N 个任务需要处理」+ 只看它们。
 *
 * ⚠️ "从没跑过"也算需要处理。它不是"还好"，是**这个任务从来没被调度过** ——
 * 而这一页的绿色徽章曾经就把从没跑过的任务显示成"正常"（见文件头注释）。
 *
 * ⚠️ 全绿时这一条也要在，写「都正常」。只在有问题时才出现的提示，
 * 看不到它的时候人分不清是"没问题"还是"这个提示坏了"。
 */
function BrokenBar({
  tasks,
  t,
  onlyBroken,
  onToggle,
}: {
  tasks: ScheduledTask[]
  t: (k: string, p?: Record<string, unknown>) => string
  onlyBroken: boolean
  onToggle: () => void
}) {
  const fail = tasks.filter((x) => taskHealth(x) === 'fail').length
  const never = tasks.filter((x) => taskHealth(x) === 'never').length
  const broken = fail + never

  if (broken === 0) {
    return (
      <p className="border-b border-border pb-2 text-xs text-muted-foreground">
        {t('cron:allHealthy', { count: tasks.length })}
      </p>
    )
  }
  return (
    <div className="flex items-baseline gap-2 border-b border-border pb-2">
      <span className="text-xs text-warning">
        {t('cron:brokenCount', { count: broken, fail, never })}
      </span>
      <button
        type="button"
        onClick={onToggle}
        className="cursor-pointer text-xs text-brand-text underline-offset-2 hover:underline"
      >
        {onlyBroken ? t('cron:showAll') : t('cron:showBrokenOnly')}
      </button>
    </div>
  )
}

const HEALTH_RANK: Record<TaskHealth, number> = { fail: 0, never: 1, ok: 2 }
function byHealth(a: ScheduledTask, b: ScheduledTask) {
  return HEALTH_RANK[taskHealth(a)] - HEALTH_RANK[taskHealth(b)]
}

function TaskRow({
  task,
  t,
  open,
  onToggleOpen,
  canSeeRuns,
}: {
  task: ScheduledTask
  t: (k: string, p?: Record<string, unknown>) => string
  open: boolean
  onToggleOpen: () => void
  canSeeRuns: boolean
}) {
  const run = useRunTask()
  const toggle = useToggleTask()
  const [settingsFor, setSettingsFor] = useState<ScheduledTask | null>(null)
  const health = taskHealth(task)

  return (
    <div className="border-b border-border last:border-0">
      <div className="flex items-center gap-3 py-2.5">
        <div className="min-w-0 flex-1">
          <div className="flex items-center gap-2">
            <span className="truncate text-[13px] font-medium text-foreground">{task.name}</span>
            <code className="font-mono text-[11px] text-muted-foreground">{task.task_key}</code>
            <Badge tone={HEALTH_TONE[health]}>{t(`cron:health.${health}`)}</Badge>
            {task.enabled !== 1 ? <Badge tone="mute">{t('cron:disabled')}</Badge> : null}
          </div>
          <div className="mt-0.5 flex flex-wrap items-center gap-3 text-[11px] text-muted-foreground">
            {/* cron 表达式原样显示：运维要拿它去核对，翻译成"每天 3 点"反而对不上 */}
            <span className="font-mono">{task.schedule}</span>
            <span>
              {t('cron:lastRun')}:{' '}
              {task.last_run_at ? (
                task.last_run_at
              ) : (
                // 从没跑过要显式说出来，不能留空——留空看起来像"这一列没数据"
                <span className="italic">{t('cron:neverRun')}</span>
              )}
            </span>
            {/* 被禁用的任务不该显示"下次执行"：它永远不会到来 */}
            {task.enabled === 1 && task.next_run_at ? (
              <span>
                {t('cron:nextRun')}: {task.next_run_at}
              </span>
            ) : null}
          </div>
          {/* 上次失败的原因直接摆出来，不藏在展开里：
              这一行存在的意义就是回答"它是不是挂了、为什么" */}
          {health === 'fail' && task.last_result ? (
            <p className="mt-1 truncate text-[11px] text-danger" title={task.last_result}>
              {task.last_result}
            </p>
          ) : null}
        </div>

        <WriteButton
          perm={PERM}
          size="sm"
          loading={run.isPending}
          onClick={() => run.mutate(task.task_key)}
        >
          {t('cron:action.runNow')}
        </WriteButton>
        <WriteButton
          perm={PERM}
          size="sm"
          onClick={() => toggle.mutate({ key: task.task_key, enabled: task.enabled !== 1 })}
        >
          {t(task.enabled === 1 ? 'cron:action.disable' : 'cron:action.enable')}
        </WriteButton>
        {/* 🔴 频率与通知设置的入口。
            这五项（频率/通知开关/发到哪个群/什么时机/@谁）后端一直收着、
            库里一直生效，而界面上**既看不见也改不了** —— 只能改库（OPSCMDB-039）。 */}
        <WriteButton perm={PERM} size="sm" onClick={() => setSettingsFor(task)}>
          {t('cron:settings.entry')}
        </WriteButton>
        {/* 没有执行记录权限的人**不显示这个按钮**，而不是点开看到空白 */}
        {canSeeRuns ? (
          <button
            type="button"
            onClick={onToggleOpen}
            className="cursor-pointer rounded-[var(--radius)] px-2 py-1 text-xs text-muted-foreground hover:bg-secondary hover:text-foreground"
          >
            {t(open ? 'cron:action.hideRuns' : 'cron:action.showRuns')}
          </button>
        ) : null}
      </div>

      {open && canSeeRuns ? <TaskRuns taskKey={task.task_key} t={t} /> : null}
      {settingsFor ? (
        <TaskSettingsDialog task={settingsFor} onClose={() => setSettingsFor(null)} />
      ) : null}
    </div>
  )
}

function TaskRuns({
  taskKey,
  t,
}: {
  taskKey: string
  t: (k: string, p?: Record<string, unknown>) => string
}) {
  const query = useTaskRuns(taskKey)
  const toLoadError = (e: unknown): LoadError => {
    const n = toErrorInfo(e)
    return { cause: tError(t, n.messageKey, n.params), detail: n.detail, retryable: n.retryable, kind: n.kind === 'license' ? 'license' : undefined, feature: n.params?.feature as string | undefined }
  }
  return (
    <div className="mb-2.5 rounded-[var(--radius)] bg-surface p-3">
      <AsyncBoundary
        state={fromQuery(query, (d) => (d.items ?? []).length === 0, toLoadError)}
        errorTitle={t('cron:error.runs')}
        retryLabel={t('common:action.retry')}
        onRetry={() => void query.refetch()}
        pending={<Skeleton className="h-4 w-[40%]" />}
        empty={
          <p className="py-2 text-center text-xs text-muted-foreground">{t('cron:runs.empty')}</p>
        }
      >
        {(d) => (
          <div className="flex flex-col gap-1">
            {(d.items ?? []).map((r) => (
              <div key={r.id} className="flex items-start gap-3 text-[11px]">
                <span className="tabular w-[130px] shrink-0 text-muted-foreground">
                  {r.started_at}
                </span>
                {/* 🔴 「执行成功」不等于「结果送出去了」。
                       任务算出了 3 张证书临期、然后 notify_state=none（没有任何投递出口）——
                       而状态徽章是绿色的 ok。人扫一眼这一列就走了，
                       那三张证书直到过期都没人知道（OPSCMDB-080）。
                       所以成功 + 没送达要降级成警告色，让它不像"一切正常"。
                    ⚠️ 只降**颜色**不改 status 文本：状态本身确实是 ok，
                       改文本会让人以为任务失败了，那是另一种误导。 */}
                <Badge
                  tone={
                    r.status === 'running'
                      ? 'info'
                      : r.status !== 'ok'
                        ? 'bad'
                        : r.notify_state === 'none' || r.notify_state === 'failed'
                          ? 'warn'
                          : 'ok'
                  }
                  title={
                    r.status === 'ok' && (r.notify_state === 'none' || r.notify_state === 'failed')
                      ? t('cron:runs.okButNotDelivered')
                      : undefined
                  }
                >
                  {r.status}
                </Badge>
                <span className="w-[90px] shrink-0 text-muted-foreground">
                  {/* 触发方式要能看出来：手动触发和自动重试的失败，处置方式不同 */}
                  {t(`cron:trigger.${r.trigger_by}`, { defaultValue: r.trigger_by })}
                </span>
                <span className="tabular w-[70px] shrink-0 text-right text-muted-foreground">
                  {r.duration_ms} ms
                </span>
                <span className="min-w-0 flex-1 break-words text-foreground/80">
                  {r.summary}
                  {/*
                    ⚠️ 失败明细必须展开到这里。
                    后端一直返回 failures（含 target + reason），前端类型里以前没声明，
                    于是「展开执行记录」和列表页给的是同一句「1 个数据源失败」——
                    不说哪个、不给原因，展开了等于没展开（P1-44）。
                    「执行记录」应当是排障的终点。
                  */}
                  <RunFailures items={r.failures ?? []} t={t} />
                  <RunFindings items={r.findings ?? []} t={t} />
                  <NotifyState run={r} t={t} />
                </span>
                {/* ⚠️ 这两个入口以前没有：后端 retry-failures / cancel 一直都在。
                    没有它们的话，一次同步 200 个失败 3 个，只能整批重跑；
                    卡住的执行只能干等 */}
                <RunActions run={r} t={t} />
              </div>
            ))}
          </div>
        )}
      </AsyncBoundary>
    </div>
  )
}

/**
 * 单次执行的操作。按状态给不同的动作 —— 给一个此刻点了必然报错的按钮
 * 比不给更糟。
 */
function RunActions({
  run,
  t,
}: {
  run: TaskRun
  t: (k: string, p?: Record<string, unknown>) => string
}) {
  const retry = useRetryFailures()
  const cancel = useCancelRun()
  const busy = retry.isPending || cancel.isPending
  const err = retry.error ?? cancel.error
  const done = retry.data ?? cancel.data

  // running 才能取消；失败过的才谈得上"重跑失败项"
  const canCancel = run.status === 'running'
  const canRetry = run.status !== 'running' && run.status !== 'ok'
  if (!canCancel && !canRetry) return null

  return (
    <span className="flex shrink-0 items-center gap-2">
      {busy ? <span className="text-muted-foreground">{t('common:state.loading')}</span> : null}
      {err ? (
        <span className="max-w-[160px] truncate text-danger" title={toErrorInfo(err).detail}>
          {tError(t, toErrorInfo(err).messageKey, toErrorInfo(err).params)}
        </span>
      ) : done ? (
        // 🔴 走 actionMessage 的三级回退：msg_key（可翻译）→ msg（后端还没迁的中文，
        //	留给 MCP / 直接调 API 的人）→ 通用文案。直接读 done.msg 的话，
        //	英文界面上就是一句中文（OPSCMDB-054）
        <span className="text-success">{actionMessage(t, done)}</span>
      ) : null}
      {canRetry ? (
        <WriteButton perm="cmdb:run_task" size="sm" onClick={() => retry.mutate(run.id)}>
          {t('cron:runs.retryFailures')}
        </WriteButton>
      ) : null}
      {canCancel ? (
        <WriteButton
          perm="cmdb:run_task"
          variant="danger"
          size="sm"
          onClick={() => cancel.mutate(run.id)}
        >
          {t('cron:runs.cancel')}
        </WriteButton>
      ) : null}
    </span>
  )
}

/** 失败明细：哪个对象、为什么。这是「执行记录」存在的理由 */
function RunFailures({
  items,
  t,
}: {
  items: TaskFailure[]
  t: (k: string, p?: Record<string, unknown>) => string
}) {
  if (items.length === 0) return null
  return (
    <ul className="mt-1 flex flex-col gap-0.5">
      {items.map((f) => (
        <li key={`${f.target}-${f.reason}`} className="flex gap-1.5">
          <span className="shrink-0 font-mono text-danger">
            {f.target || t('cron:runs.noTarget')}
          </span>
          {/* 原因原样显示，不截断：「凭据不可用：401」和「超时」的处置完全不同，
              而这两句话的差别恰好在尾部 */}
          <span className="min-w-0 break-words text-danger/85">{f.reason}</span>
        </li>
      ))}
    </ul>
  )
}

/**
 * 查出来的问题。
 *
 * ⚠️ 和失败明细分开渲染、用不同颜色：任务成功但查出 3 个磁盘超标，
 * 和任务本身失败 3 次，是两件完全不同的事，混在一起会互相掩护。
 */
function RunFindings({
  items,
  t,
}: {
  items: TaskFinding[]
  t: (k: string, p?: Record<string, unknown>) => string
}) {
  if (items.length === 0) return null
  return (
    <ul className="mt-1 flex flex-col gap-0.5">
      {/* key 用内容而不是下标：这一串会随每次执行重排，
          用下标做 key 会让 React 复用错行 */}
      {items.map((f) => (
        <li key={`${f.level}-${f.target}-${f.detail}`} className="flex flex-wrap gap-1.5">
          <Badge tone={f.level === 'critical' ? 'bad' : f.level === 'warning' ? 'warn' : 'info'}>
            {f.level}
          </Badge>
          <span className="font-mono text-foreground/80">{f.target}</span>
          {f.value ? <span className="tabular text-foreground">{f.value}</span> : null}
          {f.detail ? <span className="text-muted-foreground">{f.detail}</span> : null}
        </li>
      ))}
      <li className="text-muted-foreground">{t('cron:runs.findingNote')}</li>
    </ul>
  )
}

/**
 * 飞书投递状态。
 *
 * ⚠️ 「任务成功」和「提醒送达了」是两件事。
 * 一个到期提醒任务跑成功、但因为没配群而跳过投递，界面上只显示绿色的「ok」——
 * 于是没人知道提醒其实没发出去（这正是 P0-15/P0-20 那条链的最后一环）。
 */
function NotifyState({
  run,
  t,
}: {
  run: TaskRun
  t: (k: string, p?: Record<string, unknown>) => string
}) {
  const st = run.notify_state ?? ''
  if (st === '' || st === 'sent') {
    // 送达了就不占地方（成功是常态）；但要能看出发到哪个群去了
    return st === 'sent' && run.notify_group ? (
      <span
        className="ml-1.5 text-muted-foreground"
        // 发送时刻放 title 里：常态下不占地方，但要查「通知到底几点发的」时拿得到。
        // ⚠️ 它和任务结束时刻可能差很远（投递重试/限流），
        //	而"什么时候收到的"正是对账飞书消息时唯一的锚点。
        title={run.notify_at ? t('cron:runs.notifyAt', { at: run.notify_at }) : undefined}
      >
        {t('cron:runs.notifySent', { group: run.notify_group })}
      </span>
    ) : null
  }
  return (
    <span className={`ml-1.5 ${st === 'failed' ? 'text-danger' : 'text-warning'}`}>
      {t(`cron:runs.notify.${st}`, { defaultValue: st })}
    </span>
  )
}
