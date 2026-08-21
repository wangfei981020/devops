import { type Locale, formatNumber, formatRelativeTime } from '@ops/i18n'
import { Badge, type ColumnDef, NoValue, type NoValueKind } from '@ops/ui'
import { type ReactNode, useState } from 'react'
import { ClusterCell } from '../../components/ClusterCell.js'
import { NodeImpactDialog } from './ImpactDialog.js'
import type { Node } from './queries.js'

type TFn = (key: string, opts?: Record<string, unknown>) => string

/**
 * 把 k8s 的内存量词（8125932Ki / 32Gi / 4194304）转成人读的 GiB。
 *
 * ⚠️ 认不出来时**原样返回**，不要返回空或 0 ——
 *	一个没见过的单位后缀（比如未来的 Ei）显示成原串，人还能自己看懂；
 *	显示成空白会被当成"这台没采到内存"。
 */
export function formatK8sMem(v: string | null | undefined): string {
  if (!v) return '—'
  const m = /^(\d+(?:\.\d+)?)(Ki|Mi|Gi|Ti|K|M|G|T)?$/.exec(v.trim())
  if (!m) return v
  const n = Number(m[1])
  const mul: Record<string, number> = {
    Ki: 1 / 1024 / 1024, Mi: 1 / 1024, Gi: 1, Ti: 1024,
    K: 1000 / 1024 / 1024 / 1024, M: 1000 / 1024 / 1024, G: 1000 / 1024, T: 1000,
  }
  const gib = n * (m[2] ? (mul[m[2]] ?? 0) : 1 / 1024 / 1024 / 1024)
  if (!Number.isFinite(gib) || gib <= 0) return v
  return `${gib >= 100 ? gib.toFixed(0) : gib.toFixed(1)} Gi`
}

export function nodeColumns(
  t: TFn,
  locale: Locale,
  labels: Record<NoValueKind, string>,
  onOpenHost: (ciId: number) => void,
  /** 实时用量单元格。由页面注入 —— 它依赖页面上的 live 查询状态 */
  LiveCell: (p: { node: string }) => ReactNode,
): ColumnDef<Node>[] {
  return [
    {
      accessorKey: 'name',
      header: t('nodes:column.name'),
      cell: ({ row }) => {
        const n = row.original
        return (
          <div className="flex min-w-0 flex-col">
            <span className="truncate font-medium text-foreground">{n.name}</span>
            <span className="truncate font-mono text-xs text-muted-foreground">
              {n.internalIp || '—'}
              {n.pool ? ` · ${n.pool}` : ''}
            </span>
          </div>
        )
      },
    },
    {
      accessorKey: 'clusterName',
      header: t('nodes:column.cluster'),
      // 🔴 技术名必须可见：它才是 kubectl / PromQL 标签 / 提工单时用的标识。
      //
      //	这一列原来只显示后端拍平后的一个值（装的是别名），
      //	于是「开发环境集群」在整页上找不到对应的 dev-k8s-cluster-01（OPSCMDB-078）。
      //
      // ⚠️ 这一列窄，不能直接铺 `别名（技术名）`。用与 NODE 列一致的
      //	「主名 + 副行」排版：主行别名、副行技术名；两者相同时不重复渲染。
      //	title 仍给全站统一口径的完整串，便于复制。
      cell: ({ row }) => (
        <ClusterCell name={row.original.clusterName} display={row.original.clusterDisplay} />
      ),
    },
    {
      id: 'status',
      header: t('nodes:column.status'),
      cell: ({ row }) => {
        const n = row.original
        // ⚠️ 心跳过期时**先说失联**，不要显示它自称的 Ready。
        // kubelet 停止上报后库里那个值会停在最后一次的状态上，
        // 直接显示等于替一个联系不上的节点担保
        if (n.heartbeatStale) {
          // 采集停了 ≠ 节点失联。前者要去查 CMDB 的采集，
          // 后者要去查节点本身——混成一句话会把人指向错的方向
          const collection = n.staleReason === 'collection'
          return (
            <div className="flex flex-col items-start gap-0.5">
              <Badge tone={collection ? 'warn' : 'bad'}>
                {collection ? t('nodes:status.uncollected') : t('nodes:status.stale')}
              </Badge>
              <span className="text-[11px] text-muted-foreground">
                {collection
                  ? t('nodes:collectionStaleHint')
                  : t('nodes:staleHint', { status: n.readyStatus || '—' })}
              </span>
            </div>
          )
        }
        return (
          <Badge tone={n.readyStatus === 'Ready' ? 'ok' : 'bad'}>
            {/* k8s 原值不翻译：运维是拿这个词去 kubectl 里搜的 */}
            {n.readyStatus || t('common:state.unknown')}
          </Badge>
        )
      },
    },
    {
      id: 'conditions',
      header: t('nodes:column.conditions'),
      cell: ({ row }) => {
        const c = row.original.conditions
        // 空 = 没有压力位，是**好事**，用 — 而不是"未接入"：
        // 采不到的话整行都不会存在
        if (!c) return <span className="text-xs text-muted-foreground">—</span>
        return (
          <span className="text-xs text-warning" title={c}>
            {c}
          </span>
        )
      },
    },
    {
      // 🔴 这一列是**可分配量**（allocatable），不是节点的原始容量。
      //	两者能差出一截：kubelet 会给系统进程和 eviction 预留一块，
      //	一台 16 核的机器 allocatable 常常只有 15.9 核。
      //	下面还有一列是原始容量 —— 它们**曾经顶着同一个表头「容量」**，
      //	于是看起来像重复列，而实际上回答的是两个问题：
      //	  可分配：调度器还能放多少（装箱率的分母）
      //	  容 量：这台机器本身多大（判断"放不放得下一个 8 核 Pod"）
      id: 'allocatable',
      header: t('nodes:column.allocatable'),
      cell: ({ row }) => {
        const n = row.original
        // 容量采不到时不要显示 "0核 / 0Gi" —— 那看着像一台空机器，
        // 实际是我们没采到。0 在这里不可能是真值
        if (n.allocCpuM <= 0 && n.allocMemMi <= 0) {
          return <NoValue kind="unknown" labels={labels} />
        }
        return (
          <span className="tabular whitespace-nowrap text-xs">
            {t('nodes:capacityValue', {
              cpu: (n.allocCpuM / 1000).toFixed(0),
              mem: (n.allocMemMi / 1024).toFixed(1),
            })}
          </span>
        )
      },
    },
    {
      id: 'cpuPacking',
      header: t('nodes:column.cpuPacking'),
      cell: ({ row }) => <RequestsCell
        reqPct={row.original.cpuReqPct}
        limPct={row.original.cpuLimPct}
        reqText={`${(row.original.reqCpuM / 1000).toFixed(1)}`}
        limText={`${(row.original.limCpuM / 1000).toFixed(1)}`}
        allocText={`${(row.original.allocCpuM / 1000).toFixed(0)}`}
        unit={t('nodes:unit.cores')}
        known={row.original.allocCpuM > 0}
        labels={labels}
        t={t}
      />,
    },
    {
      id: 'memPacking',
      header: t('nodes:column.memPacking'),
      cell: ({ row }) => <RequestsCell
        reqPct={row.original.memReqPct}
        limPct={row.original.memLimPct}
        reqText={`${(row.original.reqMemMi / 1024).toFixed(1)}`}
        limText={`${(row.original.limMemMi / 1024).toFixed(1)}`}
        allocText={`${(row.original.allocMemMi / 1024).toFixed(1)}`}
        unit={t('nodes:unit.gib')}
        known={row.original.allocMemMi > 0}
        labels={labels}
        t={t}
      />,
    },
    {
      id: 'pods',
      header: t('nodes:column.pods'),
      cell: ({ row }) => {
        const n = row.original
        if (n.podsCollected === null) return <NoValue kind="notIngested" labels={labels} />
        const drift = n.podCount !== n.podsCollected
        return (
          <span className="tabular">
            {formatNumber(n.podsCollected, locale)}
            {/* 自报与采到的对不上：两个数都摆出来。
                挑一个显示就是替用户断定哪份可信，而我们并不知道 */}
            {drift ? (
              <span className="ml-1.5 text-xs text-warning">
                {t('nodes:podDrift', { reported: n.podCount })}
              </span>
            ) : null}
          </span>
        )
      },
    },
    {
      accessorKey: 'kubelet',
      header: t('nodes:column.kubelet'),
      cell: ({ row }) => {
        const n = row.original
        return (
          <div className="flex min-w-0 flex-col">
            {n.kubelet ? (
              <span className="font-mono text-xs">{n.kubelet}</span>
            ) : (
              <NoValue kind="unknown" labels={labels} />
            )}
            {/* 操作系统镜像。旧版有「版本」列（kubelet + OS 一起），新版只剩 kubelet。
                升级 GKE、排「这台跟别的不一样」时要拿它对：
                同一节点池里混着两个 COS 版本，是升级只升了一半的直接证据。 */}
            {n.osImage ? (
              <span className="truncate text-[11px] text-muted-foreground" title={n.osImage}>
                {n.osImage}
              </span>
            ) : null}
          </div>
        )
      },
    },
    {
      /**
       * 容量。旧版有这一列，新版丢了。
       *
       * ⚠️ 与「装箱率」是两回事：装箱率是 request 占容量的比例，
       *	这里是**容量本身**。判断"这台能不能放下一个 8 核的 Pod"要看容量，
       *	看装箱率答不了 —— 而两个数字都是百分号和数字，很容易混。
       */
      id: 'capacity',
      header: t('nodes:column.capacity'),
      cell: ({ row }) => {
        const n = row.original
        if (!n.cpuCap && !n.memCap) return <NoValue kind="unknown" labels={labels} />
        return (
          <span className="tabular text-[11px] whitespace-nowrap text-muted-foreground">
            {/* ⚠️ memCap 是 k8s 的原始串（如 8125932Ki）——原样显示要人心算。
                这一列的用途是"放不放得下"，而没人拿 Ki 做这个判断。 */}
            {n.cpuCap || '—'} · {formatK8sMem(n.memCap)}
          </span>
        )
      },
    },
    {
      id: 'host',
      header: t('nodes:column.host'),
      cell: ({ row }) => {
        const n = row.original
        // 0 = 没关联上，不是"没有主机"。留白会被当成这台机器不用管
        if (n.hostCiId === 0) {
          return (
            <span className="text-xs text-muted-foreground" title={t('nodes:unlinkedHint')}>
              {t('nodes:unlinked')}
            </span>
          )
        }
        // ⚠️ 主机名很长（`gke-infra-k8s-cluster-02-demo-pool-01-…`），
        //	原来右侧被整个切掉、要横向滚动才看得到，而且**没有任何入口看全名**
        //	（OPSCMDB-031 P2-17）。
        //	truncate 让它在单元格内规规矩矩省略，title 兜住全名。
        return (
          <button
            type="button"
            onClick={() => onOpenHost(n.hostCiId)}
            title={n.hostName}
            className="block max-w-full cursor-pointer truncate text-left text-[13px] text-brand-text underline-offset-2 hover:underline"
          >
            {n.hostName}
          </button>
        )
      },
    },
    {
      id: 'heartbeat',
      header: t('nodes:column.heartbeat'),
      cell: ({ row }) => {
        const n = row.original
        if (!n.lastHeartbeat) return <NoValue kind="unknown" labels={labels} />
        return (
          <span className={`text-xs ${n.heartbeatStale ? 'text-danger' : 'text-muted-foreground'}`}>
            {formatRelativeTime(n.lastHeartbeat, locale)}
          </span>
        )
      },
    },
    {
      // 节点的实时 CPU/内存百分比。⚠️ 和「装箱率」不是一回事：
      // 装箱率是 request 占 allocatable（预留了多少），这是真实用了多少。
      // 两者差得很远时才是"request 虚高"的证据 —— 分开显示才看得出来。
      //
      // 🔴 这一列原来是在 index.tsx 里 `...nodeColumns()` **之后追加**的，
      //	于是操作列被挤到倒数第二位。13 列的表横向溢出 147px 时，
      //	它整个在屏幕外，而被 sticky 钉住的其实是它左边那一列。
      //	列的顺序只能有一份真相，就是这个数组。
      id: 'live',
      header: t('nodes:live.header'),
      cell: ({ row }) => <LiveCell node={row.original.name} />,
    },
    {
      // 「这个节点挂了会影响谁」—— 驱逐/维护前必看。
      // ⚠️ 走的是 /api/k8s/impact（实际采到的 K8s 拓扑），
      // 和 /topology/impact 页的 CI 关系图不是同一个问题
      id: 'impact',
      header: '',
      // 操作列固定在最右侧。⚠️ 标了它就必须真的排在数组最后 ——
      // check-action-column 守这条
      meta: { action: true },
      cell: ({ row }) => <ImpactButton n={row.original} label={t('nodes:impact.entry')} />,
    },
  ]
}

/**
 * requests / limits 单元格：两条，各自带百分比与绝对值。
 *
 * 术语用 K8s 原词 requests / limits，不要自造"装箱"之类的说法——
 * 运维是拿这两个词去 kubectl / 文档 / 同事对话里用的，翻译一层只会增加对照成本。
 *
 * 🔴 这是 **已 requests / 可分配**，不是**实际用量**。
 * UAT 实测：request 装箱 48%，而实际 CPU 只用了 5% —— 差近十倍。
 * 所以列名必须写「装箱」不能写「使用率」，否则会有人拿它去做扩缩容决策。
 * 实际用量要接 Prometheus（见 OPSCMDB-028 GAP-1）。
 *
 * limit 超过 100% 是**正常现象**（超卖），不是错误：
 * limit 是上限不是预留，K8s 本来就允许所有容器 limit 之和大于节点容量。
 * 但超得太多意味着同时打满时会互相抢，所以 >150% 标黄提示，不标红。
 * request 超 100% 才是真问题（调度器不会这么排，出现即数据有误）。
 *
 * ⚠️ limits 要**两档**，不是一档。实测 `...demo-pool-01` 是 2 核的节点、
 * limits 合计 9.4 核（472%），和 155% 用同一个橙色 ——
 * 而 472% 与 155% 是两个量级的风险（OPSCMDB-031 P2-16）。
 * 同色等于把它们说成同一件事，而这一列存在的理由正是区分它们。
 */
function RequestsCell({
  reqPct,
  limPct,
  reqText,
  limText,
  allocText,
  unit,
  known,
  labels,
  t,
}: {
  reqPct: number
  limPct: number
  reqText: string
  limText: string
  allocText: string
  unit: string
  known: boolean
  labels: Record<NoValueKind, string>
  t: TFn
}) {
  if (!known) return <NoValue kind="unknown" labels={labels} />
  const reqTone =
    reqPct > 100 ? 'text-danger' : reqPct >= 85 ? 'text-warning' : 'text-foreground'
  // 三档：正常 / 超卖（>150%，同时打满会抢）/ 极端超卖（>300%，一个量级之外）
  const limTone =
    limPct > 300 ? 'text-danger' : limPct > 150 ? 'text-warning' : 'text-muted-foreground'
  const limHint =
    limPct > 300
      ? t('nodes:packing.limExtremeHint', { pct: limPct.toFixed(0) })
      : limPct > 150
        ? t('nodes:packing.limOverHint')
        : undefined
  return (
    <div className="flex min-w-0 flex-col gap-0.5 whitespace-nowrap">
      <span className={`tabular text-xs ${reqTone}`}>
        {t('nodes:packing.req', { pct: reqPct.toFixed(0), used: reqText, total: allocText, unit })}
      </span>
      <span className={`tabular text-[11px] ${limTone}`} title={limHint}>
        {t('nodes:packing.lim', { pct: limPct.toFixed(0), used: limText, unit })}
      </span>
    </div>
  )
}

/** 行内「影响面」按钮。弹窗按需挂载，不点不请求 */
function ImpactButton({ n, label }: { n: Node; label: string }) {
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
        <NodeImpactDialog clusterId={n.clusterId} node={n.name} onClose={() => setOpen(false)} />
      ) : null}
    </>
  )
}
