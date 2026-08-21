import { toErrorInfo } from '@ops/api'
import { tError, useTranslation } from '@ops/i18n'
import { Badge, Banner, Button, Field, MutationError, SearchInput, Select, Skeleton } from '@ops/ui'
import { useMemo, useState } from 'react'
import { type GraphHops, type GraphNode, useEntries, useGraph } from './graphQueries.js'

/** 一列的宽度、节点高度、行距 —— 布局是纯算术，不需要图库 */
const COL_W = 220
const NODE_H = 34
const ROW_GAP = 14
const PAD = 16

/**
 * 资源拓扑图。
 *
 * # 为什么是分层 SVG 而不是力导向图
 *
 * 后端已经把布局的两个难点解决了（见 handlers/relations_graph.go）：
 * `layer` 字段直接给出列号，池折叠把 657 条边压到 40 条。
 *
 * 依赖关系**天然有方向**（证书→域名→入口→主机），固定分层能把这个方向
 * 一眼看出来；力导向会把它揉成毛线团，恰好抹掉最有价值的那个信息。
 * 所以这里就是按 layer 分列、按顺序排行的算术，不引任何图库。
 *
 * # ⚠️ 三处不能省
 *
 * 1. **折叠了多少要说**（stats）。隐去规模会让人以为图就这么大，
 *    然后拿一张缩略图去做"影响面只有这些"的判断。
 * 2. **池节点要能展开看成员**。只显示「35 台主机」而不给名字，
 *    等于告诉你有线索但不给。
 * 3. **空图要给原因**（note）。可能只是没跑过建边任务，不是功能坏了。
 */
export function RelationsGraphPage() {
  const { t } = useTranslation()
  const [kw, setKw] = useState('')
  const [picked, setPicked] = useState<{ id: number; name: string } | null>(null)
  const [dir, setDir] = useState<'forward' | 'reverse'>('forward')
  // 默认 2 跳与后端一致。给到 4 是因为 host → pod → service → ingress → domain
  // 恰好是 4 跳 —— 问"这台机器挂了影响哪些域名"时 2 跳走不到头
  const [hops, setHops] = useState<GraphHops>(2)
  const [openPool, setOpenPool] = useState<GraphNode | null>(null)

  // 至少 2 个字才搜，否则等于把全表拉回来
  const entries = useEntries(kw.trim().length >= 2 ? kw.trim() : '')
  const g = useGraph(picked?.id ?? null, dir, hops)

  const layout = useMemo(() => (g.data ? layoutGraph(g.data.nodes) : null), [g.data])

  return (
    <div className="flex h-full min-h-0">
      {/* ── 左：选起点 ── */}
      <aside className="flex w-[280px] shrink-0 flex-col gap-3 border-r border-border p-4">
        <Field label={t('relations:graph.entry')} hint={t('relations:graph.entryHint')}>
          <SearchInput
            value={kw}
            onChange={setKw}
            placeholder={t('relations:graph.searchPlaceholder')}
            clearLabel={t('common:filter.clearSearch')}
          />
        </Field>

        {kw.trim().length < 2 ? (
          <p className="text-xs text-muted-foreground">{t('relations:graph.typeMore')}</p>
        ) : entries.isPending ? (
          <Skeleton className="h-4 w-[70%]" />
        ) : entries.isError ? (
          <MutationError error={entries.error} toInfo={toErrorInfo} t={(k, p) => tError(t, k, p)} />
        ) : (entries.data ?? []).filter((e) => e.degree > 0).length === 0 ? (
          <p className="text-xs text-muted-foreground">{t('relations:graph.noEntry')}</p>
        ) : (
          <div className="flex min-h-0 flex-1 flex-col gap-1 overflow-auto">
            {(entries.data ?? [])
              .filter((e) => e.degree > 0)
              .map((e) => (
              <button
                key={e.ci_id}
                type="button"
                onClick={() => setPicked({ id: e.ci_id, name: e.name })}
                className={`flex cursor-pointer items-center gap-1.5 rounded-[var(--radius)] px-2 py-1.5 text-left text-[13px] hover:bg-secondary ${
                  picked?.id === e.ci_id ? 'bg-secondary' : ''
                }`}
              >
                <Badge tone="mute">{ciTypeLabel(e.type, t)}</Badge>
                <span className="min-w-0 flex-1 truncate">{e.name}</span>
              </button>
            ))}
          </div>
        )}
      </aside>

      {/* ── 右：图 ── */}
      <div className="flex min-w-0 flex-1 flex-col">
        <div className="flex flex-wrap items-center gap-3 border-b border-border px-4 py-2.5">
          <span className="text-[13px] font-medium text-foreground">
            {picked?.name ?? t('relations:graph.noPick')}
          </span>
          <Field label={t('relations:graph.dir')}>
            <Select
              label={t('relations:graph.dir')}
              value={dir}
              onChange={(v) => setDir(v as 'forward' | 'reverse')}
              options={[
                { value: 'forward', label: t('relations:graph.forward') },
                { value: 'reverse', label: t('relations:graph.reverse') },
              ]}
            />
          </Field>
          <Field label={t('relations:graph.hops')}>
            <Select
              label={t('relations:graph.hops')}
              value={String(hops)}
              onChange={(v) => setHops(Number(v) as GraphHops)}
              options={[1, 2, 3, 4].map((n) => ({
                value: String(n),
                label: t('relations:graph.hopsN', { count: n }),
              }))}
            />
          </Field>
          {/* ⚠️ 折叠规模必须显示 */}
          {g.data?.stats ? (
            <span className="text-xs text-muted-foreground">
              {t('relations:graph.stats', {
                shown: g.data.stats.shown_edges,
                raw: g.data.stats.raw_edges,
                pooled: g.data.stats.pooled_nodes,
              })}
            </span>
          ) : null}
        </div>

        <div className="min-h-0 flex-1 overflow-auto p-4">
          {!picked ? (
            <p className="text-[13px] text-muted-foreground">{t('relations:graph.pickFirst')}</p>
          ) : g.isPending ? (
            <Skeleton className="h-40 w-[80%]" />
          ) : g.isError ? (
            <Banner tone="bad">
              <span>{tError(t, toErrorInfo(g.error).messageKey, toErrorInfo(g.error).params)}</span>
              <span className="mt-0.5 block">{toErrorInfo(g.error).detail}</span>
            </Banner>
          ) : g.data?.note ? (
            // 空图给原因：可能只是没跑过建边任务
            <Banner tone="info">
              <span>{g.data.note}</span>
            </Banner>
          ) : layout && layout.nodes.length > 0 ? (
            <GraphSvg
              layout={layout}
              edges={g.data?.edges ?? []}
              centerId={picked.id}
              onPool={setOpenPool}
              t={t}
            />
          ) : (
            <Banner tone="info">
              <span>{t('relations:graph.empty')}</span>
            </Banner>
          )}
        </div>
      </div>

      {openPool ? <PoolDialog node={openPool} onClose={() => setOpenPool(null)} t={t} /> : null}
    </div>
  )
}

type T = (k: string, p?: Record<string, unknown>) => string
type Placed = GraphNode & { x: number; y: number }

/** 按 layer 分列、列内顺序排行。纯算术 —— 这就是不需要图库的原因 */
function layoutGraph(nodes: GraphNode[]) {
  const byLayer = new Map<number, GraphNode[]>()
  for (const n of nodes) {
    const arr = byLayer.get(n.layer) ?? []
    arr.push(n)
    byLayer.set(n.layer, arr)
  }
  const layers = [...byLayer.keys()].sort((a, b) => a - b)
  const placed: Placed[] = []
  layers.forEach((ly, col) => {
    const arr = byLayer.get(ly) ?? []
    arr.forEach((n, row) => {
      placed.push({ ...n, x: PAD + col * COL_W, y: PAD + row * (NODE_H + ROW_GAP) })
    })
  })
  const maxRows = Math.max(...layers.map((l) => (byLayer.get(l) ?? []).length), 1)
  return {
    nodes: placed,
    width: PAD * 2 + Math.max(layers.length, 1) * COL_W,
    height: PAD * 2 + maxRows * (NODE_H + ROW_GAP),
  }
}

function GraphSvg({
  layout,
  edges,
  centerId,
  onPool,
  t,
}: {
  layout: { nodes: Placed[]; width: number; height: number }
  edges: { src: string; dst: string; rel_type: string; count?: number }[]
  centerId: number
  onPool: (n: GraphNode) => void
  t: T
}) {
  const pos = new Map(layout.nodes.map((n) => [n.id, n]))
  const NW = COL_W - 40

  return (
    <svg
      width={layout.width}
      height={layout.height}
      // 图比容器宽是常态，让它自己横向滚动而不是压缩变形
      style={{ minWidth: layout.width }}
      role="img"
      aria-label={t('relations:graph.svgLabel')}
    >
      <title>{t('relations:graph.svgLabel')}</title>
      {edges.map((e) => {
        const a = pos.get(e.src)
        const b = pos.get(e.dst)
        if (!a || !b) return null
        const x1 = a.x + NW
        const y1 = a.y + NODE_H / 2
        const x2 = b.x
        const y2 = b.y + NODE_H / 2
        const mx = (x1 + x2) / 2
        return (
          <g key={`${e.src}->${e.dst}-${e.rel_type}`}>
            <path
              d={`M ${x1} ${y1} C ${mx} ${y1}, ${mx} ${y2}, ${x2} ${y2}`}
              fill="none"
              // ⚠️ 用 currentColor + 语义类，不要写 var(--x, #888) 这种兜底 ——
              // 兜底值绕过了 token 层，白标换主题时那几处会保持原样，
              // 而这种问题肉眼看不出来（check-hardcoded-colors 抓的就是它）
              stroke="currentColor"
              className="text-border-strong"
              strokeWidth={e.count && e.count > 1 ? 2 : 1}
            />
            {/* 折叠边标出代表多少条：一条线代表 35 条关系时不说，会被当成一对一 */}
            {e.count && e.count > 1 ? (
              <text
                x={mx}
                y={(y1 + y2) / 2 - 3}
                textAnchor="middle"
                className="fill-current text-muted-foreground"
                style={{ fontSize: 10 }}
              >
                ×{e.count}
              </text>
            ) : null}
          </g>
        )
      })}

      {layout.nodes.map((n) => {
        const isCenter = n.ci_id === centerId
        return (
          <g
            key={n.id}
            transform={`translate(${n.x},${n.y})`}
            onClick={() => n.pool && onPool(n)}
            style={{ cursor: n.pool ? 'pointer' : 'default' }}
          >
            <rect
              width={NW}
              height={NODE_H}
              rx={6}
              className={
                isCenter
                  ? 'fill-secondary stroke-primary'
                  : 'fill-card stroke-border'
              }
              strokeWidth={isCenter ? 2 : 1}
            />
            <text
              x={8}
              y={13}
              className="fill-current text-muted-foreground"
              style={{ fontSize: 9 }}
            >
              {/* ⚠️ 类型要翻成人话：新增的 K8s 两类在库里是 `k8s_service` /
                  `k8s_ingress`，直出会在图上显示成一个下划线代号。
                  认不出的类型原样显示（新增类型时不至于变成空白） */}
              {ciTypeLabel(n.type, t)}
              {n.pool ? ` · ${t('relations:graph.pool', { count: n.count ?? 0 })}` : ''}
            </text>
            <text
              x={8}
              y={26}
              className="fill-current text-foreground"
              style={{ fontSize: 11, fontFamily: 'ui-monospace, monospace' }}
            >
              {n.name.length > 26 ? `${n.name.slice(0, 25)}…` : n.name}
            </text>
          </g>
        )
      })}
    </svg>
  )
}

/** 池节点展开：只显示「35 台主机」而不给名字，等于有线索不给 */
function PoolDialog({
  node,
  onClose,
  t,
}: {
  node: GraphNode
  onClose: () => void
  t: T
}) {
  return (
    <div className="fixed inset-0 z-40 flex items-center justify-center bg-foreground/40 p-4">
      <div className="flex max-h-[70vh] w-[560px] flex-col rounded-[var(--radius-lg)] border border-border-strong bg-card shadow-pop">
        <div className="flex items-baseline gap-2 border-b border-border px-4 py-3">
          <span className="text-[13px] font-medium text-foreground">{node.name}</span>
          <span className="text-xs text-muted-foreground">
            {t('relations:graph.pool', { count: node.count ?? 0 })}
          </span>
          <Button size="sm" className="ml-auto" onClick={onClose}>
            {t('common:action.close')}
          </Button>
        </div>
        <div className="min-h-0 flex-1 overflow-auto p-3">
          {(node.members ?? []).length === 0 ? (
            <p className="text-xs text-muted-foreground">{t('relations:graph.noMembers')}</p>
          ) : (
            <div className="flex flex-col gap-1">
              {(node.members ?? []).map((m) => (
                <div key={m.ci_id} className="flex items-center gap-2 text-[12px]">
                  <Badge tone="mute">{ciTypeLabel(m.type, t)}</Badge>
                  <span className="font-mono">{m.name}</span>
                </div>
              ))}
            </div>
          )}
        </div>
      </div>
    </div>
  )
}

/**
 * CI 类型代号 → 人话。
 *
 * ⚠️ 认不出的**原样显示**，不给「未知」：新增一个类型时，
 * 显示代号还能让人猜到是什么，显示「未知」则把信息彻底抹掉。
 */
function ciTypeLabel(type: string, t: (k: string) => string): string {
  const key = `relations:ciType.${type}`
  const label = t(key)
  return label === key ? type : label
}
