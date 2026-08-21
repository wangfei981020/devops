import { toErrorInfo } from '@ops/api'
import { clusterLabel } from '../../lib/clusterLabel.js'
import { tError, useTranslation } from '@ops/i18n'
import { Badge, Banner, Button, Field, MutationError, Select, TextArea } from '@ops/ui'
import { useState } from 'react'
import { useClusters } from '../clusters/queries.js'
import {
  type PromSeries,
  extractLokiLines,
  useLokiQuery,
  usePromLabelValues,
  usePromMetrics,
  usePromQuery,
} from './queries.js'

type Tab = 'metrics' | 'logs'

const RANGES = ['15', '60', '360', '1440']

/**
 * 观测数据自助查询：直接写 PromQL / LogQL。
 *
 * # 为什么值得单独一页
 *
 * 各资源页给的是**预设好的**曲线和日志（某个 Pod、某段时间）。
 * 排障到一定深度必然要自己拼查询 —— 跨 Pod 比对、按标签聚合、
 * 找一个页面上根本没展示的指标。没有这一页，这一步只能回 Grafana / kubectl，
 * 「不登录服务器排障」就断在这儿（OPSCMDB-023 第一档）。
 *
 * # ⚠️ 这一页最容易骗人的三处
 *
 * 1. **空结果 ≠ 组件正常**。后端给了 `empty_hint` 说明可能的原因，必须显示。
 * 2. **`cluster_isolated: false` 意味着结果里可能混着别的集群的数据** ——
 *    那会让人把别人的指标当成自己的。这条要显眼。
 * 3. **截断要说出来**。只返回前 N 条序列而不说，人会拿它当全集做判断。
 *
 * 查询只在点「执行」时发生（useMutation 而不是 useQuery）：
 * 这两个接口直接打到数据源，一次宽匹配聚合就能把它拖垮。
 */
export function ObsQueryPage() {
  const { t } = useTranslation()
  const [tab, setTab] = useState<Tab>('metrics')
  const clusters = useClusters({ page: 1, size: 100 })
  const list = clusters.data?.items ?? []
  const [cluster, setCluster] = useState('')
  const [minutes, setMinutes] = useState('60')
  const [q, setQ] = useState('')
  const [kw, setKw] = useState('')

  const prom = usePromQuery()
  const loki = useLokiQuery()
  const metrics = usePromMetrics()
  const cid = Number(cluster || list[0]?.id || 0)
  const running = prom.isPending || loki.isPending

  const run = () => {
    if (!q.trim() || !cid) return
    const v = { clusterId: cid, query: q.trim(), minutes: Number(minutes) }
    if (tab === 'metrics') prom.mutate(v)
    else loki.mutate(v)
  }

  return (
    <div className="flex flex-col gap-4 p-5">
      {/* 两种查询共用一套表单，只是打到不同的数据源 */}
      <div className="flex flex-wrap items-center gap-2">
        {(['metrics', 'logs'] as Tab[]).map((x) => (
          <button
            key={x}
            type="button"
            onClick={() => setTab(x)}
            className={`cursor-pointer rounded-[var(--radius)] border px-2.5 py-1 text-[13px] transition-colors duration-150 ${
              tab === x
                ? 'border-primary bg-secondary text-foreground'
                : 'border-border text-muted-foreground hover:bg-secondary'
            }`}
          >
            {t(`obsquery:tab.${x}`)}
          </button>
        ))}
        <span className="ml-2 text-xs text-muted-foreground">{t(`obsquery:hint.${tab}`)}</span>
      </div>

      <div className="flex flex-wrap items-end gap-3">
        <Field label={t('obsquery:cluster')}>
          <Select
            label={t('obsquery:cluster')}
            value={cluster || String(list[0]?.id ?? '')}
            onChange={setCluster}
            options={list.map((c) => ({ value: String(c.id), label: clusterLabel(c.displayName, c.name) }))}
          />
        </Field>
        <Field label={t('obsquery:rangeLabel')}>
          <Select
            label={t('obsquery:rangeLabel')}
            value={minutes}
            onChange={setMinutes}
            options={RANGES.map((m) => ({ value: m, label: t(`obsquery:range.${m}`) }))}
          />
        </Field>
        <Button variant="primary" size="sm" loading={running} onClick={run}>
          {t('obsquery:run')}
        </Button>
      </div>

      <Field
        label={t(tab === 'metrics' ? 'obsquery:promql' : 'obsquery:logql')}
        hint={t(`obsquery:example.${tab}`)}
      >
        <TextArea
          value={q}
          onChange={(e) => setQ(e.target.value)}
          rows={3}
          spellCheck={false}
          placeholder={
            tab === 'metrics'
              ? 'sum by (namespace) (rate(container_cpu_usage_seconds_total[5m]))'
              : '{namespace="doris"} |= "error"'
          }
        />
      </Field>

      {/* 指标名检索：写 PromQL 前先确认指标存在，省掉一轮"查了个空还以为组件正常" */}
      {tab === 'metrics' ? (
        <div className="flex flex-wrap items-end gap-3 rounded-[var(--radius)] border border-border bg-secondary/40 p-3">
          <Field label={t('obsquery:findMetric')} hint={t('obsquery:findMetricHint')}>
            <input
              value={kw}
              onChange={(e) => setKw(e.target.value)}
              placeholder="cpu"
              className="h-9 w-[220px] rounded-[var(--radius)] border border-input bg-card px-2.5 text-[13px] text-foreground outline-none focus:border-primary"
            />
          </Field>
          <Button
            size="sm"
            loading={metrics.isPending}
            onClick={() => kw.trim() && metrics.mutate({ clusterId: cid, keyword: kw.trim() })}
          >
            {t('obsquery:search')}
          </Button>
          {/* 🔴 写 PromQL 最大的门槛不是语法，是**不知道标签有哪些取值**：
              `namespace="..."` 里该填什么，猜错了返回空，而空结果看起来
              和「确实没有数据」一模一样 —— 正是这一页顶部那句提示说的事。
              后端 /api/obs/prom-labels 一直都在，只是从没有页面调过。 */}
          <LabelValues cid={cid} onPick={(v) => setQ((x) => x + v)} t={t} />
          {metrics.data ? (
            <div className="w-full">
              {metrics.data.ok === false ? (
                <Banner tone="bad">
                  <span>{metrics.data.error}</span>
                </Banner>
              ) : metrics.data.empty_hint ? (
                <Banner tone="warn">
                  <span>{metrics.data.empty_hint}</span>
                </Banner>
              ) : (
                <>
                  <p className="mb-1 text-[11px] text-muted-foreground">
                    {t('obsquery:matched', {
                      matched: metrics.data.matched ?? 0,
                      total: metrics.data.total_in_prometheus ?? 0,
                    })}
                    {metrics.data.truncated ? ` · ${metrics.data.truncated}` : ''}
                  </p>
                  <div className="flex flex-wrap gap-1">
                    {(metrics.data.metrics ?? []).map((m) => (
                      <button
                        key={m}
                        type="button"
                        // 点一下填进查询框：手抄指标名最容易抄错
                        onClick={() => setQ(m)}
                        className="cursor-pointer rounded-[var(--radius)] border border-border px-1.5 py-0.5 font-mono text-[11px] hover:bg-secondary"
                      >
                        {m}
                      </button>
                    ))}
                  </div>
                </>
              )}
            </div>
          ) : null}
        </div>
      ) : null}

      {tab === 'metrics' ? <PromResultView m={prom} t={t} /> : <LokiResultView m={loki} t={t} />}

      {/*
        🔴 还没查过时不要留一整屏空白。

        实测 862px 的可视区里内容只占顶部约 240px，其余全空
        （OPSCMDB-031 P2-39）。这一页天然是"先输入后看结果"，
        但"等人输入"和"什么都不给"是两回事 ——
        写 PromQL / LogQL 最大的门槛恰恰是**不知道从哪句开始**。

        所以给几条能直接点的常用查询。它们同时兼作示例：
        点一下就知道这一页长什么样，也顺带确认了数据源通不通。
      */}
      {!(tab === 'metrics' ? prom.data : loki.data) ? (
        <StarterQueries
          tab={tab}
          t={t}
          onPick={(picked) => {
            // 填进输入框再跑：让人看得见自己在查什么，也方便接着改
            setQ(picked)
            if (!cid) return
            const v = { clusterId: cid, query: picked, minutes: Number(minutes) }
            if (tab === 'metrics') prom.mutate(v)
            else loki.mutate(v)
          }}
        />
      ) : null}
    </div>
  )
}

type T = (k: string, p?: Record<string, unknown>) => string

function PromResultView({
  m,
  t,
}: {
  m: ReturnType<typeof usePromQuery>
  t: T
}) {
  if (m.isError) {
    return (
      <MutationError error={m.error} toInfo={toErrorInfo} t={(k, p) => tError(t, k, p)} />
    )
  }
  const d = m.data
  if (!d) return null

  return (
    <div className="flex flex-col gap-3">
      {/* 后端实际发出去的语句：它可能被注入了集群选择器，和你写的不一样。
          排错时这是第一个要看的东西 */}
      {d.query_sent ? (
        <div className="rounded-[var(--radius)] border border-border bg-secondary/40 p-2">
          <span className="text-[11px] text-muted-foreground">{t('obsquery:sent')}</span>
          <pre className="mt-0.5 overflow-x-auto font-mono text-[11px] text-foreground">
            {d.query_sent}
          </pre>
        </div>
      ) : null}

      {d.ok === false ? (
        <Banner tone="bad">
          <span className="font-medium">{d.error}</span>
          {d.detail ? <span className="mt-0.5 block break-all">{d.detail}</span> : null}
          {d.cluster_label_error ? (
            <span className="mt-1 block break-all">{JSON.stringify(d.cluster_label_error)}</span>
          ) : null}
        </Banner>
      ) : null}

      {/* ⚠️ 没隔离到本集群 = 结果里可能混着别的集群的数据 */}
      {d.ok !== false && d.cluster_isolated === false ? (
        <Banner tone="warn">
          <span className="font-medium">{t('obsquery:notIsolated')}</span>
          {d.note ? <span className="mt-0.5 block">{d.note}</span> : null}
        </Banner>
      ) : null}

      {d.truncated ? (
        <Banner tone="warn">
          <span>{d.truncated}</span>
        </Banner>
      ) : null}

      {/* ⚠️ 空 ≠ 组件正常，原样显示后端给的原因 */}
      {d.empty_hint ? (
        <Banner tone="info">
          <span>{d.empty_hint}</span>
        </Banner>
      ) : null}

      {(d.series ?? []).length > 0 ? (
        <div className="max-h-[52vh] overflow-auto rounded-[var(--radius)] border border-border">
          <table className="w-full text-[11px]">
            <thead className="sticky top-0 bg-card">
              <tr className="border-b border-border text-left text-muted-foreground">
                <th className="px-2 py-2 font-medium">{t('obsquery:col.labels')}</th>
                <th className="w-[140px] px-2 py-2 font-medium">{t('obsquery:col.value')}</th>
              </tr>
            </thead>
            <tbody>
              {(d.series ?? []).map((s, i) => (
                <tr key={`${i}-${labelText(s)}`} className="border-b border-border/60 last:border-0">
                  <td className="px-2 py-1.5 font-mono break-all">{labelText(s)}</td>
                  <td className="px-2 py-1.5 tabular">{lastValue(s, t)}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      ) : null}

      {d.series_count !== undefined ? (
        <p className="text-[11px] text-muted-foreground">
          {t('obsquery:seriesCount', { count: d.series_count })} · {d.result_type ?? ''}
        </p>
      ) : null}
    </div>
  )
}

function labelText(s: PromSeries): string {
  const name = s.metric.__name__ ?? ''
  const rest = Object.entries(s.metric)
    .filter(([k]) => k !== '__name__')
    .map(([k, v]) => `${k}="${v}"`)
    .join(', ')
  return rest ? `${name}{${rest}}` : name || '{}'
}

/** range 查询取最后一个点；instant 直接取值。取不到就说取不到，不显示 0 */
function lastValue(s: PromSeries, t: T): string {
  if (typeof s.value === 'number') return String(s.value)
  const vs = s.values ?? []
  const last = vs[vs.length - 1]
  if (!last) return t('common:state.unknown')
  return String(last[1])
}

function LokiResultView({ m, t }: { m: ReturnType<typeof useLokiQuery>; t: T }) {
  if (m.isError) {
    return (
      <MutationError error={m.error} toInfo={toErrorInfo} t={(k, p) => tError(t, k, p)} />
    )
  }
  const d = m.data
  if (!d) return null

  if (d.ok === false) {
    return (
      <Banner tone="bad">
        <span>{d.error ?? t('obsquery:lokiFailed')}</span>
      </Banner>
    )
  }

  const lines = extractLokiLines(d)
  return (
    <div className="flex flex-col gap-3">
      {lines.length === 0 ? (
        // ⚠️ 三态：查询成功但没有行 —— 可能标签写错、时间窗太短、也可能确实没日志。
        // 后端这个接口是原样透传的，没有 empty_hint，所以这句话由前端补
        <Banner tone="info">
          <span>{t('obsquery:noLogs')}</span>
        </Banner>
      ) : (
        <>
          <p className="text-[11px] text-muted-foreground">
            {t('obsquery:lineCount', { count: lines.length })}
            {d.step ? ` · step=${d.step}` : ''}
          </p>
          <div className="max-h-[56vh] overflow-auto rounded-[var(--radius)] border border-border bg-secondary/40">
            {lines.map((l, i) => (
              <div
                key={`${l.ts}-${i}`}
                className="flex gap-2 border-b border-border/40 px-2 py-1 last:border-0"
              >
                <span className="tabular w-[150px] shrink-0 text-[10px] text-muted-foreground">
                  {l.ts}
                </span>
                <span className="min-w-0 flex-1 font-mono text-[11px] break-all text-foreground">
                  {l.line}
                </span>
              </div>
            ))}
          </div>
          {lines[0]?.labels ? (
            <p className="text-[11px] text-muted-foreground">
              <Badge tone="mute">{lines[0].labels}</Badge>
            </p>
          ) : null}
        </>
      )}
    </div>
  )
}

/** 常用查询示例。点一下直接跑。 */
const STARTER_PROMQL = [
  { key: 'cpuTop', q: 'topk(10, sum by (pod) (rate(container_cpu_usage_seconds_total[5m])))' },
  { key: 'memTop', q: 'topk(10, sum by (pod) (container_memory_working_set_bytes))' },
  { key: 'restarts', q: 'topk(10, kube_pod_container_status_restarts_total)' },
  { key: 'nodeCpu', q: '100 - (avg by (instance) (rate(node_cpu_seconds_total{mode="idle"}[5m])) * 100)' },
]

const STARTER_LOGQL = [
  { key: 'errors', q: '{namespace=~".+"} |= "error"' },
  { key: 'oom', q: '{namespace=~".+"} | json | reason="OOMKilling"' },
  { key: 'warnEvents', q: '{namespace=~".+"} | json | type="Warning"' },
]

function StarterQueries({
  tab,
  t,
  onPick,
}: {
  tab: 'metrics' | 'logs'
  t: T
  onPick: (q: string) => void
}) {
  const list = tab === 'metrics' ? STARTER_PROMQL : STARTER_LOGQL
  return (
    <div className="rounded-[var(--radius)] border border-dashed border-border p-3">
      <p className="text-[11px] text-muted-foreground">{t('obsquery:starter.title')}</p>
      <div className="mt-2 flex flex-col gap-1.5">
        {list.map((x) => (
          <button
            key={x.key}
            type="button"
            onClick={() => onPick(x.q)}
            className="cursor-pointer rounded-[var(--radius)] border border-border px-2 py-1.5 text-left hover:bg-secondary"
          >
            <span className="block text-xs text-foreground">{t(`obsquery:starter.${x.key}`)}</span>
            {/* 语句本身也摆出来：这一页的用户是要自己写查询的，
                看得见语句才学得会，而不是点一个黑盒按钮 */}
            <span className="mt-0.5 block overflow-x-auto font-mono text-[11px] whitespace-pre text-muted-foreground">
              {x.q}
            </span>
          </button>
        ))}
      </div>
    </div>
  )
}

/**
 * 标签取值补全。填一个标签名（namespace / pod / job…），列出它有哪些取值。
 *
 * ⚠️ 点一个值直接**追加进查询框**，而不是替换 —— 人往往是写到一半才来查的。
 */
function LabelValues({
  cid,
  onPick,
  t,
}: {
  cid: number
  onPick: (v: string) => void
  t: T
}) {
  const [label, setLabel] = useState('')
  const m = usePromLabelValues()
  const d = m.data
  return (
    <div className="w-full">
      <div className="flex items-end gap-2">
        <Field label={t('obsquery:labelValues.label')} hint={t('obsquery:labelValues.hint')}>
          <input
            value={label}
            onChange={(e) => setLabel(e.target.value)}
            placeholder="namespace"
            className="h-9 w-[180px] rounded-[var(--radius)] border border-input bg-card px-2.5 text-[13px] text-foreground outline-none focus:border-primary"
          />
        </Field>
        <Button
          size="sm"
          loading={m.isPending}
          onClick={() => label.trim() && cid && m.mutate({ clusterId: cid, label: label.trim() })}
        >
          {t('obsquery:labelValues.query')}
        </Button>
      </div>

      {d ? (
        d.ok === false ? (
          // 取不到要说成取不到 —— 退化成"没有取值"会让人以为这个标签不存在
          <Banner tone="bad">
            <span>{d.error}</span>
          </Banner>
        ) : (
          <div className="mt-1.5">
            <p className="text-[11px] text-muted-foreground">
              {t('obsquery:labelValues.count', { n: d.count ?? 0 })}
            </p>
            {/* ⚠️ 截断说明必须显示：后端报的 count 是**真实总数**，
                而下面只列了前 500 个。不说的话，两个数字对不上会被当成 bug；
                更糟的是有人会拿 500 当"一共这么多"去估规模 */}
            {d.truncated ? (
              <p className="mt-0.5 text-[11px] text-warning">{d.truncated}</p>
            ) : null}
            {(d.values ?? []).length === 0 ? (
              <p className="mt-0.5 text-[11px] text-warning">
                {t('obsquery:labelValues.empty', { label: d.label })}
              </p>
            ) : (
              <div className="mt-1 flex max-h-[120px] flex-wrap gap-1 overflow-auto">
                {(d.values ?? []).map((v) => (
                  <button
                    key={v}
                    type="button"
                    onClick={() => onPick(`${d.label}="${v}"`)}
                    className="cursor-pointer rounded-[var(--radius)] border border-border px-1.5 py-0.5 font-mono text-[11px] hover:bg-secondary"
                  >
                    {v}
                  </button>
                ))}
              </div>
            )}
          </div>
        )
      ) : null}
    </div>
  )
}
