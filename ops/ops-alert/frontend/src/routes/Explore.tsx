import { useTranslation } from '@ops/i18n'
import { useMutation, useQuery } from '@tanstack/react-query'
import { Banner, Button, EmptyState, Select, Skeleton, TextInput, cn } from '@ops/ui'
import { Search } from 'lucide-react'
import { useState } from 'react'
import { get, post } from '../lib/api.js'

type Datasource = { id: number; name: string; type: string; status: string }
type Hit = {
  time: string
  line: string
  labels?: Record<string, string> | null
  fields?: Record<string, unknown> | null
}
type ExploreResult = {
  hits: Hit[]
  total: number
  took_ms: number
  truncated: boolean
  kind: string
  from: string
  to: string
}

const RANGES: [number, string][] = [
  [900, '15m'],
  [3600, '1h'],
  [21600, '6h'],
  [86400, '24h'],
]

/**
 * 日志检索。
 *
 * 存在的理由只有一个：回答「为什么没告警」。
 * 用的是**规则用的那条链路**（同一个数据源、同一套查询语法），
 * 所以查出来的就是规则看到的 —— 去 Kibana 查一遍的问题在于
 * 那边的索引、时间字段、语法可能都不一样，于是
 * 「我在 Kibana 里能查到」和「规则确实没命中」会同时成立。
 */
export function ExplorePage() {
  const { t } = useTranslation()
  const [dsID, setDsID] = useState<number | null>(null)
  const [expr, setExpr] = useState('')
  const [rangeSec, setRangeSec] = useState(3600)

  const sources = useQuery({
    queryKey: ['datasources'],
    queryFn: () => get<{ items: Datasource[] }>('/datasources'),
  })

  const search = useMutation({
    mutationFn: () =>
      post<ExploreResult>('/explore', {
        datasource_id: dsID,
        expr,
        range_sec: rangeSec,
        limit: 200,
      }),
  })

  const options = [
    { value: '', label: t('opsalert:explore.pickDatasource') },
    ...(sources.data?.items ?? []).map((d) => ({
      value: String(d.id),
      // 数据源不可达时在下拉里就标出来：选中一个坏掉的数据源查出 0 条，
      // 会被读成"没有这样的日志"，而真相是根本没查成
      label: d.status === 'ok' ? d.name : `${d.name} · ${t('opsalert:explore.dsDown')}`,
    })),
  ]

  const res = search.data
  return (
    <div className="flex flex-col gap-3">
      <Banner tone="info">{t('opsalert:explore.intro')}</Banner>

      <section className="rounded-lg border border-border bg-card p-3.5">
        <div className="flex flex-wrap items-end gap-2">
          <Select
            label={t('opsalert:explore.datasource')}
            value={dsID == null ? '' : String(dsID)}
            onChange={(v) => setDsID(v ? Number(v) : null)}
            options={options}
          />
          <label className="flex min-w-72 flex-1 flex-col gap-1">
            <span className="text-2xs text-muted-foreground">
              {t('opsalert:explore.expr')}
              {/* 语法随数据源类型而变，这里明说，别让人猜 */}
              {res?.kind ? ` · ${t(`opsalert:explore.syntax.${res.kind}`, res.kind)}` : ''}
            </span>
            <TextInput
              value={expr}
              onChange={(e) => setExpr(e.target.value)}
              placeholder={t('opsalert:explore.exprPlaceholder')}
              onKeyDown={(e) => {
                if (e.key === 'Enter' && dsID) search.mutate()
              }}
            />
          </label>
          <div className="flex gap-1">
            {RANGES.map(([sec, label]) => (
              <button
                key={sec}
                type="button"
                onClick={() => setRangeSec(sec)}
                aria-pressed={rangeSec === sec}
                className={cn(
                  'cursor-pointer rounded-md border px-2.5 py-1.5 text-xs transition-colors',
                  rangeSec === sec
                    ? 'border-primary bg-primary/10 text-primary'
                    : 'border-border bg-card text-muted-foreground hover:text-foreground',
                )}
              >
                {label}
              </button>
            ))}
          </div>
          <Button
            variant="primary"
            disabled={!dsID}
            loading={search.isPending}
            onClick={() => search.mutate()}
          >
            <Search className="size-3.5" aria-hidden="true" />
            {t('opsalert:explore.run')}
          </Button>
        </div>
      </section>

      {search.isPending && <Skeleton className="h-64 w-full" />}

      {search.isError && (
        // ⚠️ 查询失败必须显式说失败，绝不能渲染成"没有日志"。
        // 空结果在这个页面上读作"日志确实没进来"——和真相正好相反，
        // 而排查的人会顺着这个错误结论往下走很远。
        <Banner tone="bad">
          {t('opsalert:explore.failed')}：{String((search.error as Error)?.message ?? search.error)}
        </Banner>
      )}

      {res && (
        <>
          <div className="flex flex-wrap items-center gap-x-3 gap-y-1 text-2xs text-muted-foreground">
            <span>{t('opsalert:explore.matched', { count: res.hits.length })}</span>
            <span>{t('opsalert:explore.took', { ms: res.took_ms })}</span>
            <span className="font-mono">
              {new Date(res.from).toLocaleString()} ~ {new Date(res.to).toLocaleString()}
            </span>
            {res.truncated && (
              // ⚠️ 截断必须说出来。200 条看起来就是"一共 200 条"，
              // 而真相可能是几万条——拿它估算日志量会差一个数量级
              <span className="rounded bg-warning-bg px-1.5 py-0.5 text-warning">
                {t('opsalert:explore.truncated')}
              </span>
            )}
          </div>

          {res.hits.length === 0 ? (
            <EmptyState
              title={t('opsalert:explore.emptyTitle')}
              reason={t('opsalert:explore.emptyReason')}
              action={null}
            />
          ) : (
            <ul className="divide-y divide-border overflow-hidden rounded-lg border border-border bg-card">
              {res.hits.map((h, i) => (
                <li key={i} className="px-3.5 py-2">
                  <div className="flex flex-wrap items-baseline gap-2">
                    <span className="shrink-0 font-mono text-2xs tabular-nums text-muted-foreground">
                      {new Date(h.time).toLocaleString()}
                    </span>
                    {Object.entries(h.labels ?? {}).map(([k, v]) => (
                      <span key={k} className="rounded border border-border px-1 font-mono text-[10px] text-muted-foreground">
                        {k}={v}
                      </span>
                    ))}
                  </div>
                  {/* 日志原文用 pre + 换行：一行几百字符的 JSON 被截断的话，
                      要找的那个字段往往正好在被截掉的那半边 */}
                  <pre className="mt-1 font-mono text-[11px] leading-relaxed whitespace-pre-wrap break-all">
                    {h.line}
                  </pre>
                </li>
              ))}
            </ul>
          )}
        </>
      )}

      {!res && !search.isPending && !search.isError && (
        <EmptyState
          title={t('opsalert:explore.startTitle')}
          reason={t('opsalert:explore.startReason')}
          action={null}
        />
      )}
    </div>
  )
}
