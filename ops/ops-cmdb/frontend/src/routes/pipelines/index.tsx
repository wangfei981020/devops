import { toErrorInfo } from '@ops/api'
import { clusterLabel } from '../../lib/clusterLabel.js'
import { type Locale, formatRelativeTime, useTranslation } from '@ops/i18n'
import { Badge, Banner, Button, Dialog, Select, Skeleton } from '@ops/ui'
import { useState } from 'react'
import { useClusters } from '../clusters/queries.js'
import {
  type PipelineRun,
  useDevOpsProjects,
  usePipelineLog,
  usePipelineRuns,
} from './queries.js'

/**
 * 构建流水线（KubeSphere DevOps）。
 *
 * # 这一页干什么
 *
 * 「构建为什么挂了」—— 原来必须登 Jenkins / KubeSphere 控制台才能回答。
 * 后端整条链一直是通的（projects → runs → log），只是没有入口
 * （OPSCMDB-023 第三档 + P0-2）。
 *
 * 实测一路查到源码行号：
 *   `bet_service.go:601:44: undefined: bet.IGameBettorBeforeBalance`
 *   —— 依赖库升版后接口没了，连续三次构建失败。
 *
 * # ⚠️ 这一页最容易出的错：把「已拉取的这批」当成全量
 *
 * KubeSphere 单次最多拉 100 条运行记录（每条都带完整 Jenkinsfile，
 * 拉多了会超响应体积上限）。所以「失败 39 次」指的是
 * **最近 100 次里的 39 次**，而不是这个项目一共失败 39 次。
 *
 * 后端给了 `not_all_fetched` 说清这件事，界面必须显示 ——
 * 不显示的话，人会据此得出"就这几条失败"然后收工。
 */
export function PipelinesPage() {
  const { t, i18n } = useTranslation()
  const locale = i18n.language as Locale
  const clusters = useClusters({ page: 1, size: 100 })
  const list = clusters.data?.items ?? []
  const [cid, setCid] = useState(0)
  const [ns, setNs] = useState('')
  const [onlyFailed, setOnlyFailed] = useState(true)
  const [logFor, setLogFor] = useState<PipelineRun | null>(null)

  const cur = cid > 0 ? cid : (list[0]?.id ?? 0)
  const projects = useDevOpsProjects(cur)
  const runs = usePipelineRuns(cur, ns, onlyFailed)

  return (
    <div className="flex flex-col">
      <div className="flex flex-wrap items-center gap-2 border-b border-border px-4 py-3">
        <Select<string>
          label={t('pipelines:cluster')}
          value={String(cur)}
          onChange={(v) => {
            setCid(Number(v))
            setNs('') // 换集群后 DevOps 项目完全不同，旧的选择没有意义
          }}
          options={list.map((c) => ({ value: String(c.id), label: clusterLabel(c.displayName, c.name) }))}
        />
        <Select<string>
          label={t('pipelines:project')}
          value={ns}
          onChange={setNs}
          options={[
            { value: '', label: t('pipelines:pickProject') },
            ...(projects.data?.projects ?? []).map((p) => ({
              value: p.namespace ?? '',
              // Terminating 的项目要标出来：它下面的流水线多半已经查不到了
              label: p.phase === 'Active' ? (p.namespace ?? '') : `${p.namespace} (${p.phase})`,
            })),
          ]}
        />
        <Select<string>
          label={t('pipelines:scope')}
          value={onlyFailed ? 'failed' : 'all'}
          onChange={(v) => setOnlyFailed(v === 'failed')}
          options={[
            { value: 'failed', label: t('pipelines:onlyFailed') },
            { value: 'all', label: t('pipelines:allRuns') },
          ]}
        />
      </div>

      <div className="p-4">
        {projects.isError ? (
          <Banner tone="bad">
            <span className="break-all">
              {toErrorInfo(projects.error).detail || t(toErrorInfo(projects.error).messageKey)}
            </span>
          </Banner>
        ) : projects.data?.ok === false ? (
          // 后端知道具体原因（数据源没配 / 令牌权限不足），原样显示
          <Banner tone="warn">
            <span>{projects.data.error}</span>
          </Banner>
        ) : (projects.data?.projects ?? []).length === 0 ? (
          /* 🔴 0 个项目时不能说「先选一个（共 0 个）」——那是在让人做一件做不到的事。
             后端已经说清了为什么（命名空间还没采到 / 这个集群没装 DevOps），
             原样显示它比前端能编的任何文案都准确 */
          <Banner tone="warn">
            <span className="font-medium">{t('pipelines:noProjects')}</span>
            {projects.data?.hint ? (
              <span className="mt-0.5 block">{projects.data.hint}</span>
            ) : null}
          </Banner>
        ) : ns === '' ? (
          <Banner tone="info">
            <span>{t('pipelines:pickFirst', { n: (projects.data?.projects ?? []).length })}</span>
          </Banner>
        ) : (
          <Runs
            q={runs}
            locale={locale}
            t={t}
            onOpenLog={(r) => setLogFor(r)}
          />
        )}
      </div>

      {logFor ? (
        <LogDialog
          clusterId={cur}
          namespace={ns}
          run={logFor}
          onClose={() => setLogFor(null)}
          t={t}
        />
      ) : null}
    </div>
  )
}

type TFn = (k: string, o?: Record<string, unknown>) => string

function Runs({
  q,
  locale,
  t,
  onOpenLog,
}: {
  q: ReturnType<typeof usePipelineRuns>
  locale: Locale
  t: TFn
  onOpenLog: (r: PipelineRun) => void
}) {
  if (q.isPending) return <Skeleton className="h-24 w-full" />
  if (q.isError) {
    return (
      <Banner tone="bad">
        <span className="break-all">
          {toErrorInfo(q.error).detail || t(toErrorInfo(q.error).messageKey)}
        </span>
      </Banner>
    )
  }
  const d = q.data
  if (d?.ok === false) {
    return (
      <Banner tone="bad">
        <span>{d.error}</span>
      </Banner>
    )
  }
  const items = d?.items ?? []

  return (
    <div className="flex flex-col gap-2.5">
      <p className="text-xs text-muted-foreground">
        {t('pipelines:summary', { total: d?.total ?? 0, failed: d?.failed ?? 0 })}
      </p>

      {/*
        🔴 两条截断都要显示，而且 not_all_fetched 更要紧。

        truncated       = 筛出来的没给全
        not_all_fetched = **压根没拉全** —— 没拉到的连筛都没筛过，
                          所以上面那个 total/failed 不是全量

        不显示的话，「失败 39 次」会被读成"这个项目一共失败 39 次"，
        而实际是最近 100 次里的 39 次，另有 541 次没看过。
      */}
      {d?.not_all_fetched ? (
        <Banner tone="warn">
          <span>{d.not_all_fetched}</span>
        </Banner>
      ) : null}
      {d?.truncated ? (
        <Banner tone="info">
          <span>{d.truncated}</span>
        </Banner>
      ) : null}

      {items.length === 0 ? (
        <p className="py-3 text-[13px] text-muted-foreground">{t('pipelines:noRuns')}</p>
      ) : (
        <table className="w-full text-[13px]">
          <thead>
            <tr className="border-b border-border text-left text-xs text-muted-foreground">
              <th className="py-1.5 font-medium">{t('pipelines:col.pipeline')}</th>
              <th className="py-1.5 font-medium">{t('pipelines:col.run')}</th>
              <th className="py-1.5 font-medium">{t('pipelines:col.phase')}</th>
              <th className="py-1.5 font-medium">{t('pipelines:col.creator')}</th>
              <th className="py-1.5 font-medium">{t('pipelines:col.start')}</th>
              <th className="py-1.5" />
            </tr>
          </thead>
          <tbody>
            {items.map((r) => (
              <tr key={r.name} className="border-b border-border last:border-0">
                <td className="py-1.5 font-mono text-xs text-foreground">{r.pipeline}</td>
                <td className="tabular py-1.5 text-xs">#{r.run}</td>
                <td className="py-1.5">
                  <Badge tone={r.phase === 'Failed' ? 'bad' : r.phase === 'Succeeded' ? 'ok' : 'mute'}>
                    {r.phase}
                  </Badge>
                </td>
                {/* creator 为空是常见的（定时触发/webhook），显示「—」而不是留白 */}
                <td className="py-1.5 text-xs text-muted-foreground">{r.creator || '—'}</td>
                <td className="py-1.5 text-xs text-muted-foreground" title={r.start}>
                  {r.start ? formatRelativeTime(r.start, locale) : '—'}
                </td>
                <td className="py-1.5 text-right">
                  <button
                    type="button"
                    onClick={() => onOpenLog(r)}
                    className="cursor-pointer text-xs text-brand underline-offset-2 hover:underline"
                  >
                    {t('pipelines:viewLog')}
                  </button>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </div>
  )
}

/**
 * 构建日志。
 *
 * ⚠️ 只在点开时取：它实时打 KubeSphere，而且日志可能很大
 * （后端默认只给尾部 60 行 + 从全文抽出的报错行）。
 */
function LogDialog({
  clusterId,
  namespace,
  run,
  onClose,
  t,
}: {
  clusterId: number
  namespace: string
  run: PipelineRun
  onClose: () => void
  t: TFn
}) {
  const q = usePipelineLog(clusterId, namespace, run.pipeline ?? '', run.run ?? '')
  const d = q.data

  return (
    <Dialog
      open
      onClose={onClose}
      title={t('pipelines:log.title', { pipeline: run.pipeline, run: run.run })}
      description={t('pipelines:log.desc')}
      closeLabel={t('common:action.close')}
      width={980}
      footer={
        <Button size="sm" onClick={onClose}>
          {t('common:action.close')}
        </Button>
      }
    >
      {q.isPending ? (
        <Skeleton className="h-24 w-full" />
      ) : q.isError ? (
        <Banner tone="bad">
          <span className="break-all">
            {toErrorInfo(q.error).detail || t(toErrorInfo(q.error).messageKey)}
          </span>
        </Banner>
      ) : d?.ok === false ? (
        <Banner tone="bad">
          <span>{d.error}</span>
        </Banner>
      ) : (
        <div className="flex flex-col gap-2.5">
          {/* 🔴 抽出的报错行排在最前面 —— 那是「为什么失败」的直接答案。
              后端已经从全文里挑好了，把它埋在日志下面等于白挑 */}
          {(d?.errors ?? []).length > 0 ? (
            <section className="rounded-[var(--radius)] border border-danger/40 p-3">
              <p className="text-xs font-medium text-danger">{t('pipelines:log.errors')}</p>
              <ul className="mt-1 flex flex-col gap-0.5">
                {(d?.errors ?? []).map((e) => (
                  <li key={`${e.line}-${e.text}`} className="flex gap-2 text-xs">
                    <span className="shrink-0 tabular text-muted-foreground">L{e.line}</span>
                    <span className="min-w-0 flex-1 break-all font-mono text-foreground">
                      {e.text}
                    </span>
                  </li>
                ))}
              </ul>
            </section>
          ) : (
            // 没抽出报错行 ≠ 没有错误：可能是失败模式没被规则覆盖。
            // 说清楚，否则人会以为"日志里没报错"然后去查别的地方
            <p className="text-xs text-warning">{t('pipelines:log.noErrors')}</p>
          )}

          <div>
            <p className="text-[11px] text-muted-foreground">
              {t('pipelines:log.tailNote', {
                lines: d?.total_lines ?? 0,
                bytes: d?.total_bytes ?? 0,
              })}
            </p>
            <pre className="mt-1 max-h-[46vh] overflow-auto rounded-[var(--radius)] border border-border bg-secondary/40 p-2 font-mono text-[11px] whitespace-pre-wrap text-foreground">
              {d?.tail}
            </pre>
          </div>
        </div>
      )}
    </Dialog>
  )
}
