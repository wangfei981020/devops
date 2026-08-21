import { toErrorInfo } from '@ops/api'
import { clusterLabel } from '../../lib/clusterLabel.js'
import { useTranslation } from '@ops/i18n'
import { Badge, Banner, Select, Skeleton, TextInput } from '@ops/ui'
import { useMemo, useState } from 'react'
import { useClusters } from '../clusters/queries.js'
import { useNodes } from '../nodes/queries.js'
import { useHostUsage, useUsage } from './queries.js'

/** 时间范围：显示给人看的标签 → 后端要的分钟数 */
const RANGES: { key: string; minutes: number }[] = [
  { key: '1h', minutes: 60 },
  { key: '6h', minutes: 360 },
  { key: '24h', minutes: 1440 },
  { key: '7d', minutes: 10080 },
]

/**
 * 资源使用率。
 *
 * ⚠️ 这一页最容易犯的错是把「没接 Prometheus」显示成「用量 0」。
 * 后端在没数据源时返回 ok:false + 原因，必须原样显示成**未接入**，
 * 而不是画一条贴着 0 的平线——那会被读成"这台机器很闲，可以缩容"。
 *
 * # ⚠️ 这一页曾经三个 bug 叠在一起，其中两个被第一个掩护着
 *
 * 1. 前端读 `series`，后端只给 Prometheus 原始信封（真实路径 `data.data.result`）
 *    → 整页显示「没有数据点。**可能是对象名写错了**」，而查询实际成功、
 *      返回了 61 个点。既把成功渲染成空，又反过来指责用户。
 * 2. 指标下拉传 `memory`，后端只认 `mem` → 选内存**查出来的是 CPU 曲线**。
 * 3. 时间范围传 `range=1h`，后端读的是 `minutes` → 范围下拉是纯装饰，
 *    选 7d 查的还是最近 1 小时。
 *
 * 2 和 3 的共同点是**不报错、不空、只是答非所问**，所以只要 1 还在，
 * 它们就永远不会被发现。修完 1 必须把这一页整体复验一遍。
 */
export function UsagePage() {
  const { t } = useTranslation()
  const clusters = useClusters({ page: 1, size: 200, env: 'all', q: '' })
  const list = clusters.data?.items ?? []
  const [cid, setCid] = useState(0)
  const [target, setTarget] = useState('node')
  const [name, setName] = useState('')
  const [metric, setMetric] = useState('cpu')
  const [range, setRange] = useState('1h')

  /**
   * 默认集群：优先生产。
   *
   * ⚠️ 原来取 `list[0]`，而列表默认排在最前的是 DEV ——
   * 一个生产 CMDB 打开使用率页默认选中开发环境，排障时最不常看的
   * 环境成了默认值，而且没人会注意到自己看的是哪个集群。
   */
  const defaultCid = useMemo(() => {
    const prod = list.find((c) => c.environment === 'PROD')
    return (prod ?? list[0])?.id ?? 0
  }, [list])
  const cur = cid > 0 ? cid : defaultCid
  const minutes = RANGES.find((r) => r.key === range)?.minutes ?? 60

  // 节点名形如 gke-infra-k8s-cluste-infra-k8s-cluste-07d2b880-bf5s ——
  // 52 个字符、两段截断的集群名加一串哈希，手输不现实（P1-39）。
  // 只在 target=node 时拉：其它类型另有自己的列表接口，不该顺带拉一堆节点回来
  const nodes = useNodes({ page: 1, size: 500, cluster: cur > 0 ? String(cur) : '', status: 'all' })
  const nodeNames = target === 'node' ? (nodes.data?.items ?? []).map((n) => n.name) : []

  const q = useUsage({ clusterId: cur, target, name, metric, minutes }, !!name)
  const notConfigured = q.data?.ok === false

  return (
    <div className="mx-auto max-w-[1080px] p-5">
      <div className="mb-4 flex items-baseline gap-2.5">
        <h1 className="text-sm font-semibold text-foreground">{t('usage:title')}</h1>
        <p className="min-w-0 flex-1 text-xs text-muted-foreground">{t('usage:hint')}</p>
      </div>

      <div className="mb-3 flex flex-wrap items-center gap-2">
        <Select<string>
          label={t('usage:cluster')}
          value={String(cur)}
          onChange={(v) => {
            setCid(Number(v))
            setName('') // 换集群后原来的对象名多半不存在了，留着只会查出空
          }}
          // ⚠️ 别名和原名一起显示。
          //
          //	原来是 `displayName || name` —— 只有配了中文别名的那个集群显示成
          //	「开发环境集群」，另外三个显示原名。四个选项混着两套命名，
          //	用户无从判断「开发环境集群」是哪一个、和另外三个是不是同一套体系。
          //	原名是接口和 MCP 里的标识，必须始终可见。
          options={list.map((c) => ({
            value: String(c.id),
            label: clusterLabel(c.displayName, c.name),
          }))}
        />
        <Select<string>
          label={t('usage:target')}
          value={target}
          onChange={(v) => {
            setTarget(v)
            setName('') // 对象类型变了，名字的含义也变了（节点名 ≠ Pod 名）
          }}
          options={[
            { value: 'node', label: t('usage:targetNode') },
            { value: 'pod', label: t('usage:targetPod') },
            { value: 'workload', label: t('usage:targetWorkload') },
            { value: 'host', label: t('usage:targetHost') },
          ]}
        />
        {target === 'node' && nodeNames.length > 0 ? (
          <Select<string>
            label={t('usage:name')}
            value={name}
            onChange={setName}
            options={[
              { value: '', label: t('usage:pickName') },
              ...nodeNames.map((n) => ({ value: n, label: n })),
            ]}
          />
        ) : (
          <TextInput
            value={name}
            onChange={(e) => setName(e.target.value)}
            placeholder={t('usage:namePlaceholder')}
          />
        )}
        <Select<string>
          label={t('usage:metric')}
          value={metric}
          onChange={setMetric}
          options={[
            { value: 'cpu', label: 'CPU' },
            // ⚠️ 值必须是 `mem`。后端 buildPromQL 现在两种都认了，
            // 但这里仍用它的正名，别依赖兼容层
            { value: 'mem', label: t('usage:memory') },
          ]}
        />
        <Select<string>
          label={t('usage:range')}
          value={range}
          onChange={setRange}
          options={RANGES.map((r) => ({ value: r.key, label: r.key }))}
        />
      </div>

      {!name && target === 'host' ? (
        /* 🔴 选了主机却没填 IP 时，别只说"请填一个" ——
           人来这一页最想问的是「**哪些**机器最闲」（缩容依据），
           而那需要先知道该看哪台，恰恰是这个问题本身。
           后端 /api/obs/host-usage 一直算着全量排行（还 join 了台账的
           主机名和规格），只是从没有页面读过（OPSCMDB-023 第二档）。 */
        <HostUsageRanking onPick={setName} t={t} />
      ) : !name ? (
        <Banner tone="info">
          <span>{t('usage:pickTarget')}</span>
        </Banner>
      ) : q.isPending ? (
        <Skeleton className="h-24 w-full" />
      ) : notConfigured ? (
        /* ⚠️ 未接入 ≠ 用量为 0。后端给的原因原样显示，不要自己编 */
        <Banner tone="warn">
          <span className="font-medium">{t('usage:notConfigured')}</span>
          <span className="mt-0.5 block">{q.data?.error}</span>
        </Banner>
      ) : (
        <>
          <UsageChart data={q.data} fallbackName={name} t={t} />
          {/* PromQL 摊开给人看：查出来的数对不对，最终只能靠它判断。
              也是自助排查"为什么是空"的唯一入口 */}
          {q.data?.promql ? (
            <details className="mt-3">
              <summary className="cursor-pointer text-xs text-muted-foreground">
                {t('usage:showPromql')}
              </summary>
              <pre className="mt-1.5 overflow-x-auto rounded-[var(--radius)] border border-border bg-secondary p-2 font-mono text-[11px] text-foreground">
                {q.data.promql}
              </pre>
            </details>
          ) : null}
        </>
      )}
    </div>
  )
}

/**
 * 极简折线：用 SVG 画，不引图表库。
 *
 * 这一页要的是"趋势对不对、有没有尖峰"，不是精确读数——
 * 为此引一个几百 KB 的图表库不划算。
 */
function UsageChart({
  data,
  fallbackName,
  t,
}: {
  data?: {
    series?: { name?: string; points?: { t?: number; v?: number }[] }[]
    unit?: string
    empty_hint?: string
  }
  /** 聚合查询的 metric 是空的 {}，序列没有名字 —— 用用户填的对象名兜底 */
  fallbackName: string
  t: (k: string, o?: Record<string, unknown>) => string
}) {
  const series = (data?.series ?? []).filter((s) => (s.points ?? []).length > 0)
  if (series.length === 0) {
    return (
      <Banner tone="info">
        {/*
          ⚠️ 空的原因用后端给的 `empty_hint`，不要写死成「可能是对象名写错了」。
          原来那句文案在**查询成功、返回 61 个点**的情况下也照样显示，
          于是照它排查的人会一直去检查自己的输入 —— 而问题根本不在那儿。
          错误提示指向错误的方向，比不给提示更费时间。
        */}
        <span>{data?.empty_hint || t('usage:noSeries')}</span>
      </Banner>
    )
  }
  return (
    <div className="flex flex-col gap-3">
      {series.map((s, i) => {
        const pts = s.points ?? []
        const vals = pts.map((p) => p.v ?? 0)
        const max = Math.max(...vals, 0.0001)
        const path = pts
          .map(
            (p, idx) =>
              `${(idx / Math.max(pts.length - 1, 1)) * 100},${100 - ((p.v ?? 0) / max) * 100}`,
          )
          .join(' ')
        const label = s.name || fallbackName
        return (
          <section
            // 聚合后 name 是空串，多条序列的 key 会撞 —— 加索引兜底
            key={`${s.name ?? ''}-${i}`}
            className="rounded-[var(--radius-lg)] border border-border p-3"
          >
            <div className="mb-1.5 flex flex-wrap items-baseline gap-2">
              <span className="font-mono text-xs text-foreground">{label}</span>
              <Badge tone="mute">
                {t('usage:peak', { v: fmtVal(max, data?.unit), unit: unitLabel(data?.unit) })}
              </Badge>
              {/* 点数要说：31 个点和 61 个点画出来一样宽，但一个是采样稀疏的 */}
              <span className="text-[11px] text-muted-foreground">
                {t('usage:points', { count: pts.length })}
              </span>
            </div>
            <svg viewBox="0 0 100 100" preserveAspectRatio="none" className="h-24 w-full">
              <title>{label}</title>
              <polyline
                points={path}
                fill="none"
                stroke="currentColor"
                strokeWidth="1"
                className="text-brand"
                vectorEffect="non-scaling-stroke"
              />
            </svg>
          </section>
        )
      })}
    </div>
  )
}

/** 字节按 GiB 显示：内存指标的原值是字节，`3865470976` 这种数没人读得出来 */
function fmtVal(v: number, unit?: string) {
  if (unit === 'bytes') return (v / 1073741824).toFixed(2)
  return v.toFixed(2)
}

function unitLabel(unit?: string) {
  if (unit === 'bytes') return 'GiB'
  if (unit === 'cores') return 'cores'
  return unit ?? ''
}

/**
 * 非 K8s 主机的用量排行。点一行把 IP 填进上面的输入框看时序。
 *
 * ⚠️ 用量字段**取不到时是 undefined 而不是 0** —— 必须分开渲染：
 * 显示 0% 会让一台"没采到指标"的机器看起来像"完全空闲"，
 * 而那正是会被拿去缩容的那一台。
 */
function HostUsageRanking({
  onPick,
  t,
}: {
  onPick: (ip: string) => void
  t: (k: string, p?: Record<string, unknown>) => string
}) {
  const q = useHostUsage(true)
  if (q.isPending) return <Skeleton className="h-24 w-full" />
  if (q.isError) {
    return (
      <Banner tone="bad">
        <span>{toErrorInfo(q.error).detail || t(toErrorInfo(q.error).messageKey)}</span>
      </Banner>
    )
  }
  const d = q.data
  if (d?.ok === false) {
    // 后端说清了可能的原因（标签缺失 / 筛选写错），原样显示 ——
    // 它比前端能编的准确得多
    return (
      <Banner tone="warn">
        <span className="font-medium">{t('usage:hostRank.cannot')}</span>
        <span className="mt-0.5 block">{d.error}</span>
      </Banner>
    )
  }
  const items = d?.items ?? []
  if (items.length === 0) {
    return (
      <Banner tone="warn">
        <span>{t('usage:hostRank.empty')}</span>
      </Banner>
    )
  }
  return (
    <section className="rounded-[var(--radius)] border border-border">
      <p className="border-b border-border px-3 py-2 text-xs text-muted-foreground">
        {t('usage:hostRank.title', { n: items.length })}
      </p>
      <table className="w-full text-[13px]">
        <thead>
          <tr className="border-b border-border text-left text-xs text-muted-foreground">
            <th className="px-3 py-1.5 font-medium">{t('usage:hostRank.host')}</th>
            <th className="px-3 py-1.5 font-medium">{t('usage:hostRank.spec')}</th>
            <th className="px-3 py-1.5 text-right font-medium">CPU</th>
            <th className="px-3 py-1.5 text-right font-medium">{t('usage:hostRank.mem')}</th>
          </tr>
        </thead>
        <tbody>
          {items.map((r) => (
            <tr
              key={r.ip}
              onClick={() => r.ip && onPick(r.ip)}
              className="cursor-pointer border-b border-border last:border-0 hover:bg-secondary"
            >
              <td className="px-3 py-1.5">
                <span className="text-foreground">{r.host_name || r.ip}</span>
                {/* 责任组。用量排行的下一步动作是「找人」——
                    「这台 92% 了」不说归谁，看的人还得再查一次台账。
                    ⚠️ 空 = 采集里没带 team 标签，不是"没人负责"。 */}
                {r.team ? (
                  <span className="ml-1.5 rounded border border-border px-1 py-0.5 text-[11px] text-muted-foreground">
                    {r.team}
                  </span>
                ) : null}
                {r.host_name ? (
                  <span className="ml-1.5 font-mono text-[11px] text-muted-foreground">{r.ip}</span>
                ) : null}
              </td>
              <td className="px-3 py-1.5 text-xs text-muted-foreground">
                {/* 规格取不到就留空，不要写 0C0G */}
                {r.vcpu ? `${r.vcpu}C` : ''}
                {r.mem_gb ? ` ${r.mem_gb}G` : ''}
              </td>
              <td className="px-3 py-1.5 text-right"><Pct v={r.cpu_pct} t={t} /></td>
              <td className="px-3 py-1.5 text-right"><Pct v={r.mem_pct} t={t} /></td>
            </tr>
          ))}
        </tbody>
      </table>
    </section>
  )
}

/**
 * 用量百分比。
 *
 * 🔴 `undefined`（没采到）**绝不能**渲染成 0% ——
 * 那会让一台没采到指标的机器看起来"完全空闲"，
 * 而这张表的用途正是挑出闲置机器去缩容。把"不知道"渲染成"最闲"，
 * 是这张表最坏的失败方式。
 */
function Pct({ v, t }: { v?: number; t: (k: string, p?: Record<string, unknown>) => string }) {
  if (v === undefined || v === null) {
    return (
      <span className="text-xs text-warning" title={t('usage:hostRank.noMetricHint')}>
        {t('usage:hostRank.noMetric')}
      </span>
    )
  }
  const tone = v >= 85 ? 'text-danger' : v >= 60 ? 'text-warning' : 'text-muted-foreground'
  return <span className={`tabular ${tone}`}>{v.toFixed(1)}%</span>
}
