import { useTranslation } from '@ops/i18n'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import {
  AsyncBoundary,
  Badge,
  Button,
  DataTable,
  Dialog,
  EmptyState,
  Field,
  Select,
  TableSkeleton,
  TextArea,
  TextInput,
  type ColumnDef,
  fromQuery,
} from '@ops/ui'
import { StatusDot, UptimeBar, type Slot } from '../components/Trend.js'
import { WriteButton } from '../components/WriteButton.js'
import { ImportRulesDialog } from './ImportRules.js'
import { RuleFromTemplateDialog, type EditingRule } from './RuleFromTemplate.js'
import { useState } from 'react'
import { get, post, put, makeLoadError, del } from '../lib/api.js'

type Rule = {
  id: number
  name: string
  kind: string
  severity: string
  enabled: boolean
  interval_sec: number
  last_run_at?: string
  last_error: string
  runs?: string[] | null
  consecutive_failures: number
  datasource: string
}

type Datasource = { id: number; name: string; type: string; status: string }
type DryRun = {
  ok: boolean
  error?: string
  result?: {
    hits: number
    groups: Record<string, number>
    will_fire: string[]
    took_ms: number
    truncated: boolean
    samples: string[]
  }
}

/** 场景 → i18n key。加场景时这里和语言包要一起改，check-i18n 会拦住漏翻。 */
interface RuleDetail {
  name: string
  kind: string
  datasource_id: number
  spec?: { query?: string }
  threshold?: number
  for_periods?: number
  group_by?: string[] | null
  severity?: string
  template?: string
  metrics_enabled?: boolean
  metrics_labels?: Record<string, string> | null
}

export const KIND_KEYS: Record<string, string> = {
  log_keyword: 'logKeyword',
  log_absent: 'logAbsent',
  log_spike: 'logSpike',
  log_field_threshold: 'logFieldThreshold',
}

export function RulesPage() {
  const { t } = useTranslation()
  const qc = useQueryClient()
  const [creating, setCreating] = useState(false)
  const [importing, setImporting] = useState(false)
  const [fromTpl, setFromTpl] = useState(false)
  const [editing, setEditing] = useState<EditingRule | null>(null)
  // 手写规则没有模板参数可回填，编辑时只能退回手写表单。
  // 硬把它塞进模板表单的话，模板会用"空参数"重新生成查询，
  // 把人手写的 LogQL 直接冲掉——这种覆盖没有任何提示
  const [rawEditing, setRawEditing] = useState<number | null>(null)
  // 删除要二次确认：规则删了就没了，而列表里同类名字往往只差一个环境后缀，
  // 点错行的代价是把生产那条删掉
  const [removing, setRemoving] = useState<Rule | null>(null)
  const query = useQuery({ queryKey: ['rules'], queryFn: () => get<{ items: Rule[] }>('/rules') })

  const toggle = useMutation({
    mutationFn: (r: Rule) => post(`/rules/${r.id}/toggle`, { enabled: !r.enabled }),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['rules'] }),
  })

  const columns: ColumnDef<Rule, unknown>[] = [
    { header: t('opsalert:rules.colRule'), accessorKey: 'name' },
    {
      header: t('opsalert:rules.colKind'),
      accessorKey: 'kind',
      cell: ({ row }) => t(`opsalert:rules.kind.${KIND_KEYS[row.original.kind] ?? ''}`, row.original.kind),
    },
    { header: t('opsalert:rules.colDatasource'), accessorKey: 'datasource', cell: ({ row }) => <span className="font-mono text-xs">{row.original.datasource || '—'}</span> },
    { header: t('opsalert:rules.colInterval'), accessorKey: 'interval_sec', cell: ({ row }) => `${row.original.interval_sec}s` },
    {
      header: t('opsalert:rules.colLastRun'),
      accessorKey: 'last_run_at',
      cell: ({ row }) =>
        row.original.last_run_at ? (
          <span className="tabular-nums text-muted-foreground">{new Date(row.original.last_run_at).toLocaleTimeString()}</span>
        ) : (
          <span className="text-muted-foreground">{t('opsalert:rules.never')}</span>
        ),
    },
    {
      header: t('opsalert:rules.colRuns'),
      id: 'runs',
      cell: ({ row }: { row: { original: Rule } }) => {
        const runs = row.original.runs ?? []
        // 后端给的是新→旧，条子要按时间从左到右，所以反过来
        const slots: Slot[] = [...runs]
          .reverse()
          .map((o) => (o === 'error' ? 'bad' : o === 'no_data' ? 'warn' : 'ok'))
        // 从没跑过 → 整条置灰，而不是画一条空白：
        // 空白看起来像"这一列没做出来"，灰条明确表示"它没在跑"
        const shown: Slot[] = slots.length ? slots : (Array(24).fill('none') as Slot[])
        return (
          <UptimeBar
            slots={shown}
            label={t('opsalert:rules.runsLabel', {
              total: slots.length,
              failed: slots.filter((x) => x === 'bad').length,
            })}
          />
        )
      },
    },
    {
      header: t('opsalert:rules.colStatus'),
      accessorKey: 'enabled',
      cell: ({ row }) => {
        const r = row.original
        // 「查询失败」必须是显性状态：7 天触发 0 次可能是健康，
        // 也可能是查询一直在失败，两者绝不能长成一样。
        if (r.consecutive_failures > 0) {
          return (
            <StatusDot tone="firing">
              {t('opsalert:rules.queryFailed', { count: r.consecutive_failures })}
            </StatusDot>
          )
        }
        return (
          <StatusDot tone={r.enabled ? 'ok' : 'muted'}>
            {r.enabled ? t('opsalert:rules.running') : t('opsalert:rules.disabled')}
          </StatusDot>
        )
      },
    },
    {
      header: t('opsalert:rules.colOps'),
      id: 'ops',
      cell: ({ row }) => (
        <div className="flex items-center gap-1">
          <WriteButton
            perm="alert:manage_rules"
            size="sm"
            variant="ghost"
            onClick={() => toggle.mutate(row.original)}
          >
            {row.original.enabled ? t('opsalert:rules.disable') : t('opsalert:rules.enable')}
          </WriteButton>
          <WriteButton
            perm="alert:manage_rules"
            size="sm"
            variant="ghost"
            onClick={() => void openEdit(row.original.id)}
          >
            {t('opsalert:rules.edit')}
          </WriteButton>
          <WriteButton
            perm="alert:manage_rules"
            size="sm"
            variant="ghost"
            onClick={() => setRemoving(row.original)}
          >
            {t('opsalert:rules.delete')}
          </WriteButton>
        </div>
      ),
    },
  ]

  // 打开编辑：先取详情才知道它是不是模板建的
  async function openEdit(id: number) {
    const d = await get<EditingRule & { template?: string }>(`/rules/${id}`)
    if (d.template) setEditing({ ...d, id })
    else setRawEditing(id)
  }

  const remove = useMutation({
    mutationFn: (r: Rule) => del(`/rules/${r.id}`),
    onSuccess: () => {
      setRemoving(null)
      void query.refetch()
    },
  })

  return (
    <div className="flex flex-col gap-3">
      <Dialog
        open={removing !== null}
        onClose={() => setRemoving(null)}
        title={t('opsalert:rules.deleteTitle')}
        // 名字原样摆出来：同一批规则常常只差一个环境后缀，
        // 只说"确认删除吗"没法让人发现自己点错了行
        description={t('opsalert:rules.deleteDesc', { name: removing?.name ?? '' })}
        closeLabel={t('opsalert:rules.cancel')}
        footer={
          <div className="flex justify-end gap-2">
            <Button variant="ghost" onClick={() => setRemoving(null)}>
              {t('opsalert:rules.cancel')}
            </Button>
            <WriteButton
              perm="alert:manage_rules"
              variant="danger"
              loading={remove.isPending}
              onClick={() => removing && remove.mutate(removing)}
            >
              {t('opsalert:rules.delete')}
            </WriteButton>
          </div>
        }
      >
        <p className="text-sm text-muted-foreground">{t('opsalert:rules.deleteHint')}</p>
      </Dialog>
      <RuleFromTemplateDialog
        open={editing !== null}
        editing={editing}
        onClose={() => {
          setEditing(null)
          void query.refetch()
        }}
      />
      <RuleFromTemplateDialog
        open={fromTpl}
        onClose={() => {
          setFromTpl(false)
          void query.refetch()
        }}
      />
      <ImportRulesDialog
        open={importing}
        onClose={() => {
          setImporting(false)
          void query.refetch()
        }}
      />
      <div className="flex items-center gap-2">
        <span className="text-xs text-muted-foreground">
          {t('opsalert:rules.scopeHint')}
        </span>
        <WriteButton
          perm="alert:import"
          size="sm"
          variant="ghost"
          className="ml-auto"
          onClick={() => setImporting(true)}
        >
          {t('opsalert:import.entry')}
        </WriteButton>
        <WriteButton perm="alert:manage_rules" size="sm" variant="ghost" onClick={() => setCreating(true)}>
          {t('opsalert:rules.createRaw')}
        </WriteButton>
        <WriteButton perm="alert:manage_rules" size="sm" onClick={() => setFromTpl(true)}>
          {t('opsalert:rules.create')}
        </WriteButton>
      </div>

      <AsyncBoundary
        state={fromQuery(query, (d) => d.items.length === 0, makeLoadError(t))}
        pending={<TableSkeleton columns={[26, 12, 16, 8, 14, 12, 8]} rows={6} />}
        empty={
          <EmptyState
            title={t('opsalert:rules.emptyTitle')}
            reason={t('opsalert:rules.emptyReason')}
            action={{ label: t('opsalert:rules.create'), onClick: () => setCreating(true) }}
          />
        }
        errorTitle={t('opsalert:rules.loadError')}
        retryLabel={t('action.retry')}
        onRetry={() => query.refetch()}
      >
        {(data) => <DataTable columns={columns} data={data.items} rowKey={(r) => String(r.id)} />}
      </AsyncBoundary>

      {/* 失败原因直接列在下面，不用点进详情：
          规则悄悄失效是本产品要根治的头号问题，不能藏在二级页面里 */}
      {query.data?.items.some((r) => r.consecutive_failures > 0) && (
        <section className="rounded-lg border border-danger/40 bg-danger-bg p-4">
          <h3 className="text-sm font-semibold">{t('opsalert:rules.failingTitle')}</h3>
          <ul className="mt-2 flex flex-col gap-1 text-xs">
            {query.data.items
              .filter((r) => r.consecutive_failures > 0)
              .map((r) => (
                <li key={r.id}>
                  <span className="font-medium">{r.name}</span>
                  <span className="ml-2 text-muted-foreground">{r.last_error}</span>
                </li>
              ))}
          </ul>
        </section>
      )}

      {creating && (
        <CreateRuleDialog
          onClose={() => setCreating(false)}
          onCreated={() => qc.invalidateQueries({ queryKey: ['rules'] })}
        />
      )}
      {rawEditing !== null && (
        <CreateRuleDialog
          editId={rawEditing}
          onClose={() => setRawEditing(null)}
          onCreated={() => qc.invalidateQueries({ queryKey: ['rules'] })}
        />
      )}
    </div>
  )
}

/**
 * 手写规则表单。新建与编辑共用。
 *
 * ⚠️ 编辑必须支持：**导入进来的规则没有模板参数**，模板表单回填不了它们，
 * 这里是它们唯一的修改入口。只做新建的话，迁移过来的规则就成了只读的。
 */
function CreateRuleDialog({
  onClose,
  onCreated,
  editId,
}: {
  onClose: () => void
  onCreated: () => void
  /** 传了就是编辑：先拉详情预填，保存走 PUT */
  editId?: number | null
}) {
  const { t } = useTranslation()
  const sources = useQuery({ queryKey: ['datasources'], queryFn: () => get<{ items: Datasource[] }>('/datasources') })
  const detail = useQuery({
    queryKey: ['rule', editId],
    queryFn: () => get<RuleDetail>(`/rules/${editId}`),
    enabled: !!editId,
  })
  const [name, setName] = useState('')
  const [kind, setKind] = useState<'log_keyword' | 'log_absent' | 'log_spike' | 'log_field_threshold'>('log_keyword')
  const [dsID, setDsID] = useState('')
  const [query, setQuery] = useState('')
  const [threshold, setThreshold] = useState('3')
  const [forPeriods, setForPeriods] = useState('2')
  const [groupBy, setGroupBy] = useState('')
  const [severity, setSeverity] = useState<'critical' | 'warning' | 'info'>('warning')
  // 指标导出。静态标签用 "k=v,k=v" 的文本形式而不是键值对表格：
  // 这个字段绝大多数规则填 0~2 个标签，一张可增删行的表格是过度设计
  const [metricsEnabled, setMetricsEnabled] = useState(false)
  const [metricsLabelsText, setMetricsLabelsText] = useState('')
  const [filled, setFilled] = useState(false)

  // 详情到了就预填一次。用 filled 挡住重复预填，
  // 否则每次 refetch 都会把人正在改的内容冲回原值
  if (editId && detail.data && !filled) {
    const d = detail.data
    setName(d.name)
    setKind(d.kind as typeof kind)
    setDsID(String(d.datasource_id))
    setQuery(d.spec?.query ?? '')
    setThreshold(String(d.threshold ?? 3))
    setForPeriods(String(d.for_periods ?? 2))
    setGroupBy((d.group_by ?? []).join(','))
    setSeverity((d.severity as typeof severity) ?? 'warning')
    // ⚠️ 编辑时必须回填指标配置，否则保存一次就把它清空了 ——
    // 表单里没有的字段会以默认值（关闭 / 空标签）覆盖回去，
    // 而界面上完全看不出发生过这件事
    setMetricsEnabled(d.metrics_enabled ?? false)
    setMetricsLabelsText(
      Object.entries(d.metrics_labels ?? {})
        .map(([k, v]) => `${k}=${v}`)
        .join(','),
    )
    setFilled(true)
  }
  const [dry, setDry] = useState<DryRun | null>(null)

  const dryRun = useMutation({
    mutationFn: () =>
      post<DryRun>('/rules/dryrun', {
        datasource_id: Number(dsID),
        query,
        threshold: Number(threshold),
        group_by: groupBy ? groupBy.split(',').map((s) => s.trim()) : [],
      }),
    onSuccess: setDry,
  })

  const create = useMutation({
    mutationFn: () => {
      // ⚠️ metrics_labels 用 "k=v,k=v" 的输入形式，这里解析。
      // 解析失败的片段直接丢掉而不是报错：这个字段是可选的附加项，
      // 因为一个多余的逗号就拒绝保存整条规则，代价不成比例。
      // 真正会出事的（保留标签、非法标签名）由后端 400 拦下并说明原因。
      const metricsLabels: Record<string, string> = {}
      for (const pair of metricsLabelsText.split(',')) {
        const i = pair.indexOf('=')
        if (i <= 0) continue
        const k = pair.slice(0, i).trim()
        const v = pair.slice(i + 1).trim()
        if (k) metricsLabels[k] = v
      }
      const payload = {
        name,
        kind,
        datasource_id: Number(dsID),
        spec: { query },
        threshold: Number(threshold),
        for_periods: Number(forPeriods),
        group_by: groupBy ? groupBy.split(',').map((s) => s.trim()) : [],
        severity,
        notify_resolved: true,
        metrics_enabled: metricsEnabled,
        metrics_labels: metricsLabels,
      }
      if (editId) return put(`/rules/${editId}`, payload)
      return post('/rules', payload)
    },
    onSuccess: () => {
      onCreated()
      onClose()
    },
  })

  const dsOptions = (sources.data?.items ?? []).map((d) => ({ value: String(d.id), label: `${d.name}（${d.type}）` }))

  return (
    <Dialog
      open
      onClose={onClose}
      closeLabel={t('action.close')}
      width={640}
      title={t('opsalert:rules.form.title')}
      description={t('opsalert:rules.form.desc')}
      footer={
        <>
          <Button variant="ghost" onClick={onClose}>
            {t('action.cancel')}
          </Button>
          <Button variant="default" loading={dryRun.isPending} disabled={!dsID || !query} onClick={() => dryRun.mutate()}>
            {t('opsalert:rules.form.dryRun')}
          </Button>
          <WriteButton perm="alert:manage_rules" loading={create.isPending} blockedReason={!name || !dsID || !query ? t('opsalert:perm.fillRequired') : undefined} onClick={() => create.mutate()}>
            {t('opsalert:rules.form.save')}
          </WriteButton>
        </>
      }
    >
      <div className="flex flex-col gap-3">
        <Field label={t('opsalert:rules.form.name')} required>
          <TextInput value={name} onChange={(e) => setName(e.target.value)} />
        </Field>
        <div className="grid grid-cols-2 gap-3">
          <Field label={t('opsalert:rules.form.scene')}>
            <Select
              label={t('opsalert:rules.form.scene')}
              value={kind}
              onChange={setKind}
              options={Object.entries(KIND_KEYS).map(([v, k]) => ({
                value: v as typeof kind,
                label: t(`opsalert:rules.kind.${k}`),
              }))}
            />
          </Field>
          <Field label={t('opsalert:rules.form.datasource')} required>
            <Select label={t('opsalert:rules.form.datasource')} value={dsID} onChange={setDsID} options={dsOptions} />
          </Field>
        </div>
        <Field
          label={t('opsalert:rules.form.query')}
          hint={t('opsalert:rules.form.queryHint')}
          required
        >
          <TextArea rows={3} value={query} onChange={(e) => setQuery(e.target.value)} />
        </Field>
        <div className="grid grid-cols-4 gap-3">
          <Field label={t('opsalert:rules.form.threshold')} hint={t('opsalert:rules.form.thresholdHint')}>
            <TextInput value={threshold} onChange={(e) => setThreshold(e.target.value)} />
          </Field>
          <Field label={t('opsalert:rules.form.forPeriods')} hint={t('opsalert:rules.form.forHint')}>
            <TextInput value={forPeriods} onChange={(e) => setForPeriods(e.target.value)} />
          </Field>
          <Field label={t('opsalert:rules.form.groupBy')} hint={t('opsalert:rules.form.groupByHint')}>
            <TextInput value={groupBy} onChange={(e) => setGroupBy(e.target.value)} placeholder="container" />
          </Field>
          <Field label={t('opsalert:rules.form.metricsEnabled')} hint={t('opsalert:rules.metricsHint')}>
            <label className="flex items-center gap-2 text-sm">
              <input
                type="checkbox"
                checked={metricsEnabled}
                onChange={(e) => setMetricsEnabled(e.target.checked)}
                className="size-4 cursor-pointer accent-[var(--color-primary)]"
              />
              {t('opsalert:rules.metricsEnabled')}
            </label>
          </Field>
          {metricsEnabled && (
            <Field label={t('opsalert:rules.metricsLabels')} hint={t('opsalert:rules.metricsLabelsHint')}>
              <TextInput
                value={metricsLabelsText}
                onChange={(e) => setMetricsLabelsText(e.target.value)}
                placeholder="project=G32,env=PROD"
              />
            </Field>
          )}
          <Field label={t('opsalert:rules.form.severity')}>
            <Select
              label={t('opsalert:rules.form.severity')}
              value={severity}
              onChange={setSeverity}
              options={[
                { value: 'critical', label: t('opsalert:severity.critical') },
                { value: 'warning', label: t('opsalert:severity.warning') },
                { value: 'info', label: t('opsalert:severity.info') },
              ]}
            />
          </Field>
        </div>

        {dry && (
          <div className={`rounded-md border p-3 text-xs ${dry.ok ? 'border-border' : 'border-danger/40 bg-danger-bg'}`}>
            {!dry.ok ? (
              // 数据源的原始报错原样带出：LogQL / DSL 的错误提示很具体，
              // 吞掉它只留"试运行失败"会让人无从改起。
              <div className="text-danger">
                {t('opsalert:rules.form.dryRunFailed')}：{dry.error}
              </div>
            ) : (
              <>
                <div className="mb-1 font-medium">
                  {t('opsalert:rules.form.dryRunSummary', {
                    hits: dry.result?.hits ?? 0,
                    groups: Object.keys(dry.result?.groups ?? {}).length,
                    ms: dry.result?.took_ms ?? 0,
                  })}
                  {dry.result?.truncated && (
                    <span className="ml-2 text-warning">{t('opsalert:rules.form.truncated')}</span>
                  )}
                </div>
                <div className="text-muted-foreground">
                  {dry.result?.will_fire.length
                    ? t('opsalert:rules.form.willFire', { groups: dry.result.will_fire.join('、') })
                    : t('opsalert:rules.form.willFireNone')}
                </div>
                {dry.result?.samples?.length ? (
                  <pre className="mt-2 max-h-32 overflow-auto rounded bg-muted p-2 font-mono text-[11px]">
                    {dry.result.samples.join('\n')}
                  </pre>
                ) : null}
              </>
            )}
          </div>
        )}
      </div>
    </Dialog>
  )
}
