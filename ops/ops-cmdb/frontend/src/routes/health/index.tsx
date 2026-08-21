import { toErrorInfo } from '@ops/api'
import { clusterLabel } from '../../lib/clusterLabel.js'
import { tError, useTranslation } from '@ops/i18n'
import {
  AsyncBoundary,
  Badge,
  type BadgeTone,
  EmptyState,
  type LoadError,
  Select,
  Skeleton,
  fromQuery,
} from '@ops/ui'
import { useNavigate, useSearch } from '@tanstack/react-router'
import { useState } from 'react'
import { HealthDetailDialog } from './DetailDialog.js'
import { CheckCircle2 } from 'lucide-react'
import { useClusters } from '../clusters/queries.js'
import { type HealthReport, useHealth } from './queries.js'

type TFn = (key: string, opts?: Record<string, unknown>) => string

function severityTone(s: string): BadgeTone {
  switch (s) {
    case 'critical':
      return 'bad'
    case 'warning':
      return 'warn'
    case 'info':
      return 'info'
    default:
      // 不认识的严重度按最高处理：当成 info 会把上游新增的告警级别静默降级
      return 'bad'
  }
}

export function HealthPage() {
  const { t } = useTranslation()
  const { cluster } = useSearch({ from: '/overview/clusters' })
  const navigate = useNavigate({ from: '/overview/clusters' })
  const clusters = useClusters({ page: 1, size: 100 })

  // 没选集群时给一个默认 —— 体检是"针对某个集群"的，没有全局版本
  const list = clusters.data?.items ?? []
  const current = cluster > 0 ? cluster : defaultClusterId(list)
  const query = useHealth(current)

  const toLoadError = (e: unknown): LoadError => {
    const n = toErrorInfo(e)
    return { cause: tError(t, n.messageKey, n.params), detail: n.detail, retryable: n.retryable, kind: n.kind === 'license' ? 'license' : undefined, feature: n.params?.feature as string | undefined }
  }

  return (
    <div className="flex flex-col">
      <div className="flex flex-wrap items-center gap-2 border-b border-border px-4 py-3">
        <Select<string>
          label={t('health:filter.cluster')}
          value={String(current)}
          onChange={(v) => void navigate({ search: { cluster: Number(v) } })}
          options={list.map((c) => ({ value: String(c.id), label: clusterLabel(c.displayName, c.name) }))}
        />
      </div>

      <div className="mx-auto w-full max-w-[900px] p-5">
        <AsyncBoundary
          state={fromQuery<HealthReport>(query, () => false, toLoadError)}
          errorTitle={t('health:error.title')}
          retryLabel={t('common:action.retry')}
          onRetry={() => void query.refetch()}
          pending={
            <div className="flex flex-col gap-3">
              {[70, 90, 60].map((w) => (
                <Skeleton key={w} className="h-5" style={{ width: `${w}%` }} />
              ))}
            </div>
          }
          empty={null}
        >
          {(r) => <Report r={r} t={t} clusterId={current} />}
        </AsyncBoundary>
      </div>
    </div>
  )
}

function Report({ r, t, clusterId }: { r: HealthReport; t: TFn; clusterId: number }) {
  // 下钻弹窗：点计数才挂载，不点不请求
  const [drill, setDrill] = useState<{ key: string; title: string } | null>(null)
  if (r.findings.length === 0) {
    return (
      <EmptyState
        icon={<CheckCircle2 />}
        title={t('health:clear.title')}
        // ⚠️ 措辞必须限定在"已采集到的数据范围内"。
        // 说成"集群没问题"就是替一份可能不完整的数据背书
        reason={
          r.checks?.length
            ? `${t('health:clear.reason')} ${t('health:checksRan', { count: r.checks.length })}`
            : t('health:clear.reason')
        }
        action={null}
      />
    )
  }
  return (
    <div className="flex flex-col gap-3">
      <div className="flex items-baseline gap-3 text-[13px]">
        <span className="text-muted-foreground">{t('health:summary')}</span>
        <span className="tabular text-danger">
          {t('health:critical', { count: r.summary.critical })}
        </span>
        <span className="tabular text-warning">
          {t('health:warning', { count: r.summary.warning })}
        </span>
        <span className="tabular text-muted-foreground">
          {t('health:info', { count: r.summary.info })}
        </span>
        {/* 🔴 分母：这一轮**跑了几项检查**。
            没有它的话，两个集群条数不同时人分不清是"跑了没命中"还是"这项没跑"——
            而两者的下一步完全相反（一个是好消息，一个要去接数据源）。
            OPSCMDB-076：g32-prod 少一条「重启异常」，交叉验证后确认是没命中，
            但界面上看不出来。 */}
        {r.checks?.length ? (
          <span className="tabular text-muted-foreground">
            {t('health:checksRan', { count: r.checks.length })}
          </span>
        ) : null}
      </div>

      {r.findings.map((f, i) => (
        <section
          key={`${f.severity}-${f.title}-${i}`}
          className="rounded-[var(--radius-lg)] border border-border bg-card p-3.5"
        >
          <div className="flex min-w-0 items-baseline gap-2.5">
            <Badge tone={severityTone(f.severity)}>{f.severity}</Badge>
            <span className="min-w-0 flex-1 text-[13px] font-medium text-foreground">
              {f.title}
            </span>
            {/* ⚠️ 计数可点：只显示「56」而点不进去，人看到之后唯一的出路是回命令行。
                没有 key 的项后端不支持下钻，那就保持成纯文本，别给一个点了没反应的数字 */}
            {f.count > 0 ? (
              f.key ? (
                <button
                  type="button"
                  onClick={() => setDrill({ key: f.key, title: f.title })}
                  className="tabular cursor-pointer text-xs text-brand-text underline-offset-2 hover:underline"
                >
                  {/* ⚠️ 计数必须带单位。
                      光一个「33」挨着后面的分类「工作负载」，会被读成"33 个工作负载"，
                      而重启的是 Pod —— 一个工作负载有 N 个副本，两者差好几倍，
                      而这个数会被用来估算影响面（P1-10） */}
                  {countLabel(f, t)}
                </button>
              ) : (
                <span className="tabular text-xs text-muted-foreground">{countLabel(f, t)}</span>
              )
            ) : null}
            {/* 分类用括号包起来：和紧邻的计数视觉上分开，否则两者连读像"33 工作负载" */}
            <span className="shrink-0 text-[11px] text-muted-foreground">（{f.category}）</span>
          </div>
          {/*
            ⚠️ 必须能折行（break-words）。
            detail 里会出现**很长且不含空格**的内容 —— 比如 Prometheus 查询失败时
            带出来的整条 URL-encoded PromQL。不折行的话浏览器不会断开它，
            超出的部分被直接裁掉：实测 scrollWidth 1281 / clientWidth 839，
            **442px 的内容读不到**，而排障的关键
            （`dial tcp: lookup vmselect.monitoring.svc ... no such host`）
            恰好在被裁掉的尾部。
            ⚠️ 这一条只有截图 + 量 scrollWidth 才发现得了：
            document 级没有横向溢出，接口返回的字段也完全正常。
          */}
          <p className="mt-1.5 break-words text-xs leading-relaxed text-muted-foreground">
            {f.detail}
          </p>
          {/* 有处置建议就显示：只说"有 56 个 Pod 重启超 100 次"没法处置 */}
          {f.action ? (
            // 处置里也可能带长命令（kubectl --field-selector=...），同样要折行
            <p className="mt-1.5 break-words text-xs leading-relaxed text-foreground">
              {f.action}
            </p>
          ) : null}
        </section>
      ))}

      {drill ? (
        <HealthDetailDialog
          clusterId={clusterId}
          itemKey={drill.key}
          title={drill.title}
          onClose={() => setDrill(null)}
        />
      ) : null}
    </div>
  )
}

/**
 * 「33 个 Pod」而不是光一个「33」。
 *
 * 后端给了 unit（pod / node / workload / pvc / hpa / resource / image）。
 * 拿不到 unit 时**只显示数字**，不要瞎猜一个单位 ——
 * 猜错的单位比没有单位更糟：它看起来是确定的。
 */
function countLabel(f: { count: number; unit: string }, t: (k: string, o?: Record<string, unknown>) => string) {
  if (!f.unit) return String(f.count)
  return t(`health:unit.${f.unit}`, { count: f.count, defaultValue: String(f.count) })
}

/**
 * 没指定集群时打开哪一个。
 *
 * # 为什么不能是"列表第一个"
 *
 * 原来取 `list[0]`，也就是**接口返回顺序**里的第一个 —— 实测落到了 DEV，
 * 而 4 个集群里 UAT/PROD 才是更该先看的（OPSCMDB-031 P2-5）。
 *
 * 更糟的是 URL 写着 `?cluster=0`：`0` 看起来像"未选/全部"，
 * 却对应到了某一个具体集群。一个看起来像"没选"的值静默地做了选择。
 *
 * ⚠️ 按环境重要度挑，而不是按返回顺序 —— 后者会随数据库里的 id 顺序变，
 * 也就是说"默认打开哪个集群"这件事会因为新增一个集群而悄悄改变。
 */
function defaultClusterId(list: { id: number; environment?: string }[]): number {
  if (list.length === 0) return 0
  const rank = (env: string | undefined) => {
    switch ((env ?? '').toUpperCase()) {
      case 'PROD':
        return 0
      case 'UAT':
        return 1
      case 'TEST':
        return 2
      case 'DEV':
        return 3
      default:
        // 认不出的环境排在已知的之后、但在没有环境的之前。
        // 客户可能自定义环境名（灰度等），不该因为不认识就排到最后
        return 4
    }
  }
  return [...list].sort((a, b) => rank(a.environment) - rank(b.environment))[0]?.id ?? 0
}
