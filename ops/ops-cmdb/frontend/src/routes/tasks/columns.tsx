import { type Locale, formatRelativeTime, useTranslation } from '@ops/i18n'
import { Badge, type ColumnDef } from '@ops/ui'
import { useNavigate } from '@tanstack/react-router'
import { useState } from 'react'
import { WriteButton } from '../../components/WriteButton.js'
import { useRunTask } from '../cron/queries.js'
import { type Task, taskState } from './queries.js'

type TFn = (key: string, opts?: Record<string, unknown>) => string

export function taskColumns(t: TFn): ColumnDef<Task>[] {
  return [
    {
      accessorKey: 'name',
      header: t('tasks:column.name'),
      cell: ({ row }) => (
        <div className="flex min-w-0 flex-col">
          <span className="truncate font-medium text-foreground">{row.original.name}</span>
          <span className="truncate font-mono text-xs text-muted-foreground">
            {row.original.task_key}
          </span>
        </div>
      ),
    },
    {
      id: 'state',
      header: t('tasks:column.state'),
      cell: ({ row }) => {
        const s = taskState(row.original)
        // ⚠️ skipped 用中性色（mute），既不是绿也不是红：
        // 绿会让“一次都没采到数据”看着正常（OPSCMDB-006），
        // 红会让“没配依赖”天天报错、让人对告警脱敏
        const tone =
          s === 'failed' || s === 'overdue'
            ? 'bad'
            : s === 'never'
              ? 'warn'
              : s === 'ok'
                ? 'ok'
                : 'mute'
        return (
          <div className="flex flex-col items-start gap-0.5">
            <div className="flex items-center gap-1.5">
              <Badge tone={tone}>{t(`tasks:state.${s}`)}</Badge>
              {/* 🔴 「这个任务失败了会不会通知」在界面上此前完全看不出来。
                  一个关着通知的任务失败时是**静默**的 —— 它看起来和从没出过问题
                  一模一样。这条信息比"当前是不是失败"更该常驻：
                  当前失败你迟早会看到，静默失败你永远看不到。
                  ⚠️ 只标"不通知"，不标"通知"：后者是默认且安全的那一档。 */}
              {!row.original.notify_enabled ? (
                <span
                  className="rounded border border-warning/40 px-1 py-0.5 text-[11px] text-warning"
                  title={t('tasks:notifyOffHint')}
                >
                  {t('tasks:notifyOff')}
                </span>
              ) : null}
            </div>
            {/* 停摆要解释一句：任务不会报错，它只是不再执行 */}
            {s === 'overdue' ? (
              <span className="text-[11px] text-muted-foreground">{t('tasks:overdueHint')}</span>
            ) : null}
            {s === 'never' ? (
              <span className="text-[11px] text-muted-foreground">{t('tasks:neverHint')}</span>
            ) : null}
            {/*
              ⚠️ 「已停用 + 从没跑过」要额外说一句它的后果。
              实测 node_health_watch（节点健康预警）和 inspect（证书握手探测）
              都是这个状态 —— 也就是说节点出真问题时没有任何主动预警，
              证书是不是真的配对了也无从得知。
              而界面上「已停用」是个灰色的中性标签，看不出这意味着一整条能力缺失
              （P1-41 / P1-42）。
            */}
            {s === 'disabled' && !row.original.last_run_at ? (
              <span className="text-[11px] text-warning">{t('tasks:disabledNeverHint')}</span>
            ) : null}
          </div>
        )
      },
    },
    {
      accessorKey: 'schedule',
      header: t('tasks:column.schedule'),
      cell: ({ row }) => <span className="font-mono text-xs">{row.original.schedule || '—'}</span>,
    },
    {
      id: 'lastRun',
      header: t('tasks:column.lastRun'),
      cell: ({ row }) => <LastRun task={row.original} />,
    },
    {
      id: 'result',
      header: t('tasks:column.result'),
      cell: ({ row }) => <Result task={row.original} t={t} />,
    },
    {
      id: 'ops',
      // 操作列固定在最右侧。⚠️ 标了就必须真的排在数组最后（check-action-column 守这条）
      meta: { action: true },
      header: t('tasks:column.ops'),
      cell: ({ row }) => <RowOps task={row.original} t={t} />,
    },
  ]
}

/**
 * 结果文案。
 *
 * ⚠️ 长文案不能只靠横向滚动读。
 *
 *	实测最长的一条（relations_auto_link）是多行、几百字符的报告，
 *	原来只能拖着横条一点点看，而且 DOM 里 14 行的 `title` 全是 null，
 *	连悬停看全都做不到（P2-40 / P2-41）。
 *
 *	这里改成：默认截断到两行 + `title` 悬停 + 长文案给「展开」。
 *	展开而不是弹窗：这些文案是用来和同一行的状态对照读的，
 *	弹到另一个层里反而要来回切。
 */
function Result({ task, t }: { task: Task; t: TFn }) {
  const [open, setOpen] = useState(false)
  const r = task.last_result
  if (!r) return <span className="text-xs text-muted-foreground">—</span>
  const bad = taskState(task) === 'failed'
  // 多行或够长的才给展开：短文案加一个「展开」按钮只是噪音。
  //
  // ⚠️ 阈值从 90 降到 45：实测 460px 宽的列里，53 个中文字符就已经被裁了
  // （第一行 gke_upgrade_sync 是 53 字、裁掉 69px），而它当时没有展开按钮 ——
  // 于是那段文字既读不全、也没有办法读全。
  const long = r.length > 45 || r.includes('\n')
  return (
    // ⚠️ 必须给最大宽度。
    //
    //	`line-clamp-2` 只在有宽度约束时才折行 —— 没有约束时它会**横向撑开**，
    //	把表格从 1512px 撑到 2013px，于是我新加的「操作」列被挤到可视区之外，
    //	要横向滚动才看得到（实测就是这样：列在 DOM 里，人看不到）。
    //	而这一页加操作列的全部意义就是"看到失败之后能当场做点什么"。
    //
    //	⚠️ 这条只有截图 + 量 table.scrollWidth 才发现得了：
    //	DOM 里一切正常，document 级也没有横向溢出（表格容器自己 overflow-x:auto）。
    <div className="flex min-w-0 max-w-[460px] flex-col gap-0.5">
      {/*
        ⚠️ `whitespace-normal` 必须显式写上，`line-clamp-2` 才有用。

        DataTable 给每个 `td` 加了 `whitespace-nowrap`（表格单元格不折行，
        那是个合理的默认）。它继承到这里之后文字**不折行** ——
        于是 line-clamp 无从生效（只有一行可数），
        超出的部分被 overflow:hidden 横向裁掉。

        实测：`-webkit-line-clamp: 2` 在、`overflow: hidden` 也在，
        但 `scrollWidth - clientWidth = 69px`（第一行）、`132px`（第六行）——
        **裁的是横向，不是行数**。

        ⚠️ 这一条只有实测才发现得了：代码里 `line-clamp-2` 写得完全正确，
        是父级的一个 class 让它失效的。看代码看不出来。
      */}
      <span
        className={`text-xs ${bad ? 'text-danger' : 'text-muted-foreground'} ${
          open ? 'whitespace-pre-wrap break-words' : 'line-clamp-2 whitespace-normal break-words'
        }`}
        // 悬停看全：即使不展开也要能读到完整内容
        title={r}
      >
        {r}
      </span>
      {long ? (
        <button
          type="button"
          onClick={() => setOpen((v) => !v)}
          className="cursor-pointer self-start text-[11px] text-muted-foreground underline underline-offset-2 hover:text-foreground"
        >
          {open ? t('common:action.collapse') : t('common:action.expand')}
        </button>
      ) : null}
    </div>
  )
}

/**
 * 行内操作。
 *
 * 只给两个，且都是这一页看到问题之后的**下一步**：
 *   立即运行     —— 故障往往是可恢复的（实测 registrar_expiry_sync 手动一跑就成功）
 *   看执行记录   —— 失败原因、失败的是哪些对象，都在那边
 *
 * ⚠️ 不放「停用/启用」：那是调度管理，属于定时任务页。
 * 在体检页放一个能把任务关掉的按钮，误点的代价太大。
 */
function RowOps({ task, t }: { task: Task; t: TFn }) {
  const run = useRunTask()
  const navigate = useNavigate()
  const done = run.isSuccess && run.variables === task.task_key
  const failed = run.isError && run.variables === task.task_key

  return (
    <div className="flex shrink-0 flex-col items-start gap-1">
      <WriteButton
        perm="cmdb:run_task"
        size="sm"
        // ⚠️ WriteButton 不收裸 disabled，只收「禁用原因」——
        // 置灰不给原因，有权限的人会以为系统坏了。这里的原因是"上一次点击还在跑"
        blockedReason={
          run.isPending && run.variables === task.task_key
            ? t('tasks:action.running')
            : undefined
        }
        // 停用的任务点「立即运行」会怎样：后端仍然会跑（手动触发不看 enabled）。
        // 这正是想要的 —— 停用的任务也要能手动验一次，否则永远不知道它能不能跑
        onClick={() => run.mutate(task.task_key)}
      >
        {t('tasks:action.runNow')}
      </WriteButton>
      <button
        type="button"
        onClick={() => void navigate({ to: '/runtime/cron', search: { task: task.task_key } })}
        className="cursor-pointer text-[11px] text-muted-foreground underline underline-offset-2 hover:text-foreground"
      >
        {t('tasks:action.viewRuns')}
      </button>
      {/* 手动触发是**异步**的：成功只代表"任务收下了"，不代表跑完了。
          说成"已完成"会让人立刻去看结果，然后发现没变 */}
      {done ? <span className="text-[11px] text-success">{t('tasks:action.queued')}</span> : null}
      {failed ? (
        <span className="text-[11px] text-danger" title={String(run.error)}>
          {t('tasks:action.triggerFailed')}
        </span>
      ) : null}
    </div>
  )
}

function LastRun({ task }: { task: Task }) {
  const { t, i18n } = useTranslation()
  if (!task.last_run_at) {
    // 从没跑过 ≠ 刚跑完。空时间戳最容易被读成"没问题"
    return <span className="text-xs text-warning">{t('tasks:neverRun')}</span>
  }
  return (
    <span className={`text-xs ${task.overdue ? 'text-danger' : 'text-muted-foreground'}`}>
      {formatRelativeTime(task.last_run_at, i18n.language as Locale)}
    </span>
  )
}
