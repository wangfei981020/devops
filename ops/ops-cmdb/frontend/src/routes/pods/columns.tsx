import { ClusterCell } from '../../components/ClusterCell.js'
import { type Locale, formatRelativeTime } from '@ops/i18n'
import { Badge, type ColumnDef, NoValue, type NoValueKind } from '@ops/ui'
import { useState } from 'react'
import { ManifestDialog } from '../../components/ManifestDialog.js'
import { phaseTone } from '../hosts/podPhase.js'
import type { Pod } from './queries.js'

type TFn = (key: string, opts?: Record<string, unknown>) => string

export function podColumns(
  t: TFn,
  locale: Locale,
  labels: Record<NoValueKind, string>,
): ColumnDef<Pod>[] {
  return [
    {
      accessorKey: 'name',
      header: t('pods:column.name'),
      cell: ({ row }) => {
        const p = row.original
        return (
          <div className="flex min-w-0 flex-col">
            <span className="truncate font-medium text-foreground">{p.name}</span>
            <span className="truncate text-xs text-muted-foreground">
              {p.namespace}
              {p.workload ? ` · ${p.workload}` : ''}
            </span>
          </div>
        )
      },
    },
    {
      id: 'phase',
      header: t('pods:column.phase'),
      cell: ({ row }) => {
        const p = row.original
        return (
          <div className="flex min-w-0 flex-col items-start gap-0.5">
            {/* phase 与 reason 都原样显示：CrashLoopBackOff 是运维
                拿去 kubectl 和搜索引擎里查的词，翻译过来就对不上了 */}
            <Badge tone={phaseTone(p.phase)}>{p.phase || t('common:state.unknown')}</Badge>
            {p.reason ? (
              <span className="truncate font-mono text-[11px] text-warning" title={p.reason}>
                {p.reason}
              </span>
            ) : null}
          </div>
        )
      },
    },
    {
      accessorKey: 'restarts',
      header: t('pods:column.restarts'),
      cell: ({ row }) => {
        const p = row.original
        const n = p.restarts
        // 0 次不显示"0"：一列全是 0 的时候，真正重启过的那几行反而不显眼
        if (n === 0) return <span className="text-xs text-muted-foreground">—</span>
        // 🔴 重启 100 次和 21916 次是两个性质，不能同色。
        //
        //	实测生产：一个 Failed 且重启 **21916 次**的 Pod 存在了 3 个月
        //	（OPSCMDB-031 P2-18）。而 cluster_health 的 pod_high_restart 描述里
        //	正好写着「持续 CrashLoop 的服务，且往往长期无人发现」——
        //	页面把它标成橙色排在最前（判据对），但和重启 12 次的用同一个颜色。
        //
        //	⚠️ 存在时长也要一起说：持续 3 天和持续 3 个月是两回事，
        //	而"存在了 3 个月还在重启"正是「长期无人发现」的判据本身。
        const extreme = n >= 1000
        const rate =
          p.startTime && n > 0 ? restartsPerDay(n, p.startTime) : null
        return (
          <span
            className={extreme ? 'tabular font-medium text-danger' : 'tabular text-warning'}
            title={
              rate !== null
                ? t('pods:restartRateHint', { n, perDay: rate.toFixed(0) })
                : t('pods:restartHint', { n })
            }
          >
            {n}
          </span>
        )
      },
    },
    {
      accessorKey: 'clusterName',
      header: t('pods:column.cluster'),
      cell: ({ row }) => (
        <ClusterCell name={row.original.clusterName} display={row.original.clusterDisplay} />
      ),
    },
    {
      accessorKey: 'nodeName',
      header: t('pods:column.node'),
      cell: ({ row }) => {
        const p = row.original
        return (
          <div className="flex min-w-0 flex-col">
            {p.nodeName ? (
              <span className="truncate font-mono text-xs">{p.nodeName}</span>
            ) : (
              // 没有 nodeName = 还没被调度到任何节点上（Pending 的常见形态），
              // 这是**信息**不是缺失，所以用 na 而不是 unknown
              <NoValue kind="na" labels={labels} />
            )}
            {/* 🔴 Pod IP 此前是「类型里有、映射里取了、界面上零处引用」——
                看代码像接好了，看界面才发现没有。
                拿 IP 去 Envoy / 日志里反查"这个请求是谁发的"就靠它。
                和节点放一起：定位一个 Pod 时这两个信息总是一起用。 */}
            {p.podIp ? (
              <span className="truncate font-mono text-[11px] text-muted-foreground">
                {p.podIp}
              </span>
            ) : null}
          </div>
        )
      },
    },
    {
      /**
       * request · limit。**配置**不是用量。
       *
       * 「这个 Pod 申请了多少资源」是判断"是不是 request 写太大导致装不下"的
       * 唯一依据 —— 用量页有实际用量，但那回答不了这个问题（OPSCMDB-028 GAP-10）。
       *
       * ⚠️ 没配 request 要**显式说出来**，不能显示成 0 或「—」：
       *	BestEffort 的 Pod 在节点内存压力时最先被驱逐，
       *	这是一个需要被看见的状态，不是一格缺失数据。
       */
      id: 'resources',
      header: t('pods:column.resources'),
      cell: ({ row }) => {
        const p = row.original
        if (p.cpuReqM === null && p.memReqMi === null && p.cpuLimM === null && p.memLimMi === null) {
          return (
            <span className="text-[11px] text-warning" title={t('pods:resources.bestEffortHint')}>
              {t('pods:resources.bestEffort')}
            </span>
          )
        }
        // 单位跟着**每个数字**走，不要只挂在末尾：
        // `100 · —m` 会被读成"limit 是 —m"，而实际是"request 100m、没有 limit"
        const one = (v: number | null, unit: string) => (v === null ? '—' : `${v}${unit}`)
        const fmt = (req: number | null, lim: number | null, unit: string) =>
          `${one(req, unit)} · ${one(lim, unit)}`
        return (
          <div className="flex min-w-0 flex-col text-[11px] whitespace-nowrap">
            <span className="tabular">{fmt(p.cpuReqM, p.cpuLimM, 'm')}</span>
            <span className="tabular text-muted-foreground">
              {fmt(p.memReqMi, p.memLimMi, 'Mi')}
            </span>
          </div>
        )
      },
    },
    {
      id: 'age',
      header: t('pods:column.age'),
      cell: ({ row }) => {
        const p = row.original
        if (!p.startTime) return <NoValue kind="unknown" labels={labels} />
        // CrashLoop 的 Pod 会不断重启，这个值一直很小 —— 它本身就是线索，
        // 所以和重启次数放在一起看
        return (
          <span className="text-xs text-muted-foreground">
            {formatRelativeTime(p.startTime, locale)}
          </span>
        )
      },
    },
    {
      // 看 YAML。Pod 这一页尤其需要：诊断弹窗给的是结论，
      // 而「为什么是这个结论」往往要回到原始 spec 里找（挂了什么卷、哪个 init 是 root）
      id: 'yaml',
      header: '',
      cell: ({ row }) => <PodManifestButton p={row.original} label={t('manifest:view')} />,
    },
  ]
}

function PodManifestButton({ p, label }: { p: Pod; label: string }) {
  const [open, setOpen] = useState(false)
  return (
    <>
      <button
        type="button"
        onClick={() => setOpen(true)}
        className="cursor-pointer rounded-[var(--radius)] px-1.5 py-0.5 text-[11px] text-brand-text underline-offset-2 hover:underline"
      >
        {label}
      </button>
      {open ? (
        <ManifestDialog
          clusterId={p.clusterId}
          kind="pod"
          namespace={p.namespace}
          name={p.name}
          onClose={() => setOpen(false)}
        />
      ) : null}
    </>
  )
}

/**
 * 平均每天重启多少次。
 *
 * 把「重启 21916 次」变成「平均每天 243 次」—— 后者才说得清严重度，
 * 因为绝对次数会被存在时长稀释：跑了 3 个月的 2 万次和跑了 1 天的 2 万次
 * 是完全不同的两件事（OPSCMDB-031 P2-18）。
 *
 * ⚠️ 不足一天的按一天算，避免把「刚建 10 分钟就重启 5 次」除成一个巨大的数字
 * 而显得比真实情况更吓人。
 */
function restartsPerDay(restarts: number, startTime: string): number | null {
  const ms = Date.parse(startTime)
  if (Number.isNaN(ms)) return null
  const days = Math.max(1, (Date.now() - ms) / 86_400_000)
  return restarts / days
}
