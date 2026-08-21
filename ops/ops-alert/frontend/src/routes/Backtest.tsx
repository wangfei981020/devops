import { useTranslation } from '@ops/i18n'
import { useMutation, useQuery } from '@tanstack/react-query'
import { Badge, Button, Field, Select, TextArea, TextInput } from '@ops/ui'
import { WriteButton } from '../components/WriteButton.js'
import { useState } from 'react'
import { get, post } from '../lib/api.js'

type Datasource = { id: number; name: string; type: string }
type Result = {
  windows: number
  fires: number
  per_day: Record<string, number>
  per_group: Record<string, number>
  night_fires: number
  daily_avg: number
  query_errors: number
  note: string
}
type Compare = {
  base_fires: number
  draft_fires: number
  reduction_pct: number
  night_before: number
  night_after: number
  missed_groups: string[]
  missed_warning: boolean
}
type Backtest = {
  status: string
  error: string
  result?: { draft: Result; base?: Result; compare?: Compare; base_error?: string }
}

/**
 * 回放实验室。
 *
 * 回答三个问题：真实故障还抓不抓得到、噪音降了多少、深夜会不会叫醒人。
 * 全程沙箱——不发通知、不产生事件、不影响现网规则。
 */
export function BacktestPage() {
  const { t } = useTranslation()
  const sources = useQuery({ queryKey: ['datasources'], queryFn: () => get<{ items: Datasource[] }>('/datasources') })
  const [dsID, setDsID] = useState('')
  const [query, setQuery] = useState('')
  const [groupBy, setGroupBy] = useState('')
  const [draftThreshold, setDraftThreshold] = useState('20')
  const [baseThreshold, setBaseThreshold] = useState('5')
  const [days, setDays] = useState<'3' | '7' | '14' | '30'>('7')
  const [jobID, setJobID] = useState<number | null>(null)

  const start = useMutation({
    mutationFn: () => {
      const common = {
        datasource_id: Number(dsID),
        query,
        lookback_sec: 300,
        for_periods: 2,
        interval_sec: 60,
        group_by: groupBy ? groupBy.split(',').map((s) => s.trim()) : [],
      }
      return post<{ id: number }>('/backtests', {
        draft: { ...common, threshold: Number(draftThreshold) },
        base_draft: { ...common, threshold: Number(baseThreshold) },
        compare: true,
        days: Number(days),
      })
    },
    onSuccess: (res) => setJobID(res.id),
  })

  const job = useQuery({
    queryKey: ['backtest', jobID],
    queryFn: () => get<Backtest>(`/backtests/${jobID}`),
    enabled: jobID != null,
    // 回放在后台跑，轮询到 done 为止。
    refetchInterval: (q) => (q.state.data?.status === 'running' ? 2000 : false),
  })

  const r = job.data?.result
  const dsOptions = (sources.data?.items ?? []).map((d) => ({ value: String(d.id), label: d.name }))

  return (
    <div className="grid grid-cols-1 gap-4 lg:grid-cols-2">
      <section className="rounded-lg border border-border p-4">
        <h2 className="text-sm font-semibold">{t('opsalert:backtest.paramsTitle')}</h2>
        <p className="mt-1 text-xs text-muted-foreground">{t('opsalert:backtest.paramsHint')}</p>
        <div className="mt-3 flex flex-col gap-3">
          <Field label={t('opsalert:rules.form.datasource')} required>
            <Select label={t('opsalert:rules.form.datasource')} value={dsID} onChange={setDsID} options={dsOptions} />
          </Field>
          <Field label={t('opsalert:rules.form.query')} required>
            <TextArea rows={3} value={query} onChange={(e) => setQuery(e.target.value)} />
          </Field>
          <div className="grid grid-cols-2 gap-3">
            <Field label={t('opsalert:rules.form.groupBy')} hint={t('opsalert:rules.form.groupByHint')}>
              <TextInput value={groupBy} onChange={(e) => setGroupBy(e.target.value)} />
            </Field>
            <Field label={t('opsalert:backtest.range')}>
              <Select
                label={t('opsalert:backtest.range')}
                value={days}
                onChange={setDays}
                options={(['3', '7', '14', '30'] as const).map((d) => ({
                  value: d,
                  label: t('opsalert:backtest.days', { count: Number(d) }),
                }))}
              />
            </Field>
          </div>
          <div className="grid grid-cols-2 gap-3">
            <Field label={t('opsalert:backtest.baseThreshold')} hint={t('opsalert:backtest.baseHint')}>
              <TextInput value={baseThreshold} onChange={(e) => setBaseThreshold(e.target.value)} />
            </Field>
            <Field label={t('opsalert:backtest.draftThreshold')} hint={t('opsalert:backtest.draftHint')}>
              <TextInput value={draftThreshold} onChange={(e) => setDraftThreshold(e.target.value)} />
            </Field>
          </div>
          <WriteButton perm="alert:run_backtest" loading={start.isPending} blockedReason={!dsID || !query ? t('opsalert:perm.fillRequired') : undefined} onClick={() => start.mutate()}>
            {t('opsalert:backtest.start')}
          </WriteButton>
        </div>
      </section>

      <section className="rounded-lg border border-border p-4">
        <h2 className="text-sm font-semibold">{t('opsalert:backtest.resultTitle')}</h2>
        {jobID == null && (
          <p className="mt-4 text-xs text-muted-foreground">
            {t('opsalert:backtest.idle')}
          </p>
        )}
        {job.data?.status === 'running' && (
          <p className="mt-4 text-xs text-muted-foreground">{t('opsalert:backtest.running')}</p>
        )}
        {job.data?.status === 'failed' && (
          <p className="mt-4 text-xs text-danger">
            {t('opsalert:backtest.failed')}：{job.data.error}
          </p>
        )}
        {r?.draft && (
          <div className="mt-3 flex flex-col gap-3 text-sm">
            <div className="grid grid-cols-3 gap-3">
              <Stat label={t('opsalert:backtest.fires')} value={r.draft.fires} />
              <Stat label={t('opsalert:backtest.dailyAvg')} value={r.draft.daily_avg.toFixed(1)} />
              <Stat
                label={t('opsalert:backtest.nightFires')}
                value={r.draft.night_fires}
                tone={r.draft.night_fires > 0 ? 'warn' : 'ok'}
              />
            </div>

            {r.compare && (
              <div className="rounded-md border border-border p-3 text-xs">
                <div>
                  {t('opsalert:backtest.compare', {
                    base: r.compare.base_fires,
                    draft: r.compare.draft_fires,
                    dir:
                      r.compare.reduction_pct >= 0
                        ? t('opsalert:backtest.down')
                        : t('opsalert:backtest.up'),
                    pct: Math.abs(r.compare.reduction_pct).toFixed(1),
                  })}
                </div>
                <div className="mt-1 text-muted-foreground">
                  {t('opsalert:backtest.nightCompare', {
                    before: r.compare.night_before,
                    after: r.compare.night_after,
                  })}
                </div>
                {/* 「少吵了」必须和「漏报了」一起看：
                    阈值调到无穷大也能让噪音降 100%。 */}
                {r.compare.missed_warning ? (
                  <div className="mt-2 rounded border border-warning/50 bg-warning/10 p-2">
                    <div className="font-medium">{t('opsalert:backtest.missedWarning')}</div>
                    <div className="mt-1 font-mono">{r.compare.missed_groups.join('、')}</div>
                  </div>
                ) : (
                  <div className="mt-2 text-success">{t('opsalert:backtest.missedNone')}</div>
                )}
              </div>
            )}

            {r.draft.query_errors > 0 && (
              <div className="rounded-md border border-warning/50 bg-warning/10 p-2 text-xs">
                {t('opsalert:backtest.queryErrors', { count: r.draft.query_errors })}
              </div>
            )}
            {r.draft.note && <div className="text-xs text-muted-foreground">{r.draft.note}</div>}

            <div>
              <div className="mb-1 text-xs font-medium">{t('opsalert:backtest.byGroup')}</div>
              <div className="flex flex-wrap gap-1.5">
                {Object.entries(r.draft.per_group)
                  .sort((a, b) => b[1] - a[1])
                  .slice(0, 10)
                  .map(([g, n]) => (
                    <Badge key={g} tone="mute" dot={false}>
                      {g || t('opsalert:incidents.filterAll')} × {n}
                    </Badge>
                  ))}
              </div>
            </div>
          </div>
        )}
      </section>
    </div>
  )
}

function Stat({ label, value, tone }: { label: string; value: number | string; tone?: 'ok' | 'warn' }) {
  return (
    <div className={`rounded-md border p-3 ${tone === 'warn' ? 'border-warning/50' : 'border-border'}`}>
      <div className="text-xs text-muted-foreground">{label}</div>
      <div className="mt-1 text-xl font-semibold tabular-nums">{value}</div>
    </div>
  )
}
