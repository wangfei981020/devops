import { useTranslation } from '@ops/i18n'
import { Badge, Banner, Button, Dialog } from '@ops/ui'
import { useMutation, useQuery } from '@tanstack/react-query'
import { useState } from 'react'
import { WriteButton } from '../components/WriteButton.js'
import { get, post, put } from '../lib/api.js'

/**
 * 按场景建规则。
 *
 * 通用表单是按技术模型组织的（选 kind、写 LogQL、填分组维度），
 * 要求人先懂新系统才能建出第一条规则。模板反过来问业务问题
 * （"哪些错误码不用告警"），查询语句由后端生成。
 *
 * ⚠️ 生成的 LogQL **必须显示出来**。模板可以隐藏复杂度，但不能隐藏结果 ——
 * 看不到查询，出问题时人既不知道改哪里，也无法判断模板有没有理解错他的意图。
 *
 * ⚠️ 高级选项默认折叠而不是不给：模板盖不住的场景（改持续周期、改通知模板）
 * 还是要有出口，否则人只能退回去手写整条规则。
 */

interface TplField {
  key: string
  type: 'text' | 'tags' | 'number'
  required?: boolean
  help?: string
  default?: string
  example?: string
}

interface Template {
  key: string
  kind: string
  fields: TplField[]
}

interface Preview {
  kind: string
  query: string
  lookback_sec: number
  group_by?: string[] | null
  extra?: { ignored_codes?: string[] } | null
}

export interface EditingRule {
  id: number
  name: string
  template: string
  params: Record<string, string | string[]> | null
  datasource_id: number
  interval_sec: number
  for_periods: number
  severity: string
}

export function RuleFromTemplateDialog({
  open,
  onClose,
  initialTemplate,
  editing,
}: {
  open: boolean
  onClose: () => void
  /** 从模板库点进来时直接进表单，跳过"选场景"这一步 */
  initialTemplate?: string | null
  /** 编辑已有规则：用它当初填的模板参数回填，而不是让人反推 LogQL */
  editing?: EditingRule | null
}) {
  const { t } = useTranslation()
  // ⚠️ 不默认取第一个数据源：多集群环境里"查错集群"的规则跑起来一切正常，
  // 只是永远查不到该查的日志
  const [datasourceId, setDatasourceId] = useState<number | null>(null)
  const datasources = useQuery({
    queryKey: ['datasources'],
    queryFn: () => get<{ items: { id: number; name: string; type: string }[] }>('/datasources'),
    enabled: open,
  })
  const [tpl, setTpl] = useState<Template | null>(null)
  const [values, setValues] = useState<Record<string, string | string[]>>({})
  const [name, setName] = useState('')
  const [advanced, setAdvanced] = useState(false)
  const [interval, setIntervalSec] = useState('300')
  const [forPeriods, setForPeriods] = useState('1')
  const [severity, setSeverity] = useState('critical')
  const [preview, setPreview] = useState<Preview | null>(null)
  const [err, setErr] = useState('')

  const templates = useQuery({
    queryKey: ['rule-templates'],
    queryFn: () => get<{ items: Template[] }>('/rules/templates'),
    enabled: open,
  })

  // 指定了模板（新建）或在编辑已有规则时，直接进表单。
  // 放在渲染期而不是 effect 里：走 effect 会先渲染一帧"选场景"再跳走，像闪了一下
  const wantKey = editing?.template || initialTemplate
  if (open && wantKey && !tpl) {
    const hit = templates.data?.items.find((x) => x.key === wantKey)
    if (hit) {
      const init: Record<string, string | string[]> = {}
      for (const f of hit.fields) init[f.key] = f.type === 'tags' ? [] : (f.default ?? '')
      setTpl(hit)
      // 编辑时用规则存下来的参数覆盖默认值。缺字段的按默认值走 ——
      // 模板后来加了新字段时，老规则不该因此打不开
      setValues(editing?.params ? { ...init, ...editing.params } : init)
      if (editing) {
        setName(editing.name)
        setDatasourceId(editing.datasource_id)
        setIntervalSec(String(editing.interval_sec))
        setForPeriods(String(editing.for_periods))
        setSeverity(editing.severity)
      }
    }
  }

  const doPreview = useMutation({
    mutationFn: () =>
      post<Preview>('/rules/templates/preview', { template: tpl?.key, params: values }),
    onSuccess: (d) => {
      setPreview(d)
      setErr('')
    },
    // 模板参数不合法是用户输入问题，把后端给的原因原样显示
    onError: (e: Error & { detail?: string }) => setErr(e.detail || e.message),
  })

  const create = useMutation({
    mutationFn: () => {
      const payload = {
        name,
        kind: preview?.kind,
        datasource_id: datasourceId,
        spec: { query: preview?.query },
        interval_sec: Number(interval) || 300,
        lookback_sec: preview?.lookback_sec,
        for_periods: Number(forPeriods) || 1,
        group_by: preview?.group_by ?? [],
        severity,
        template: tpl?.key,
        params: values,
      }
      if (editing) return put(`/rules/${editing.id}`, payload)
      return post('/rules', payload)
    },
    onSuccess: () => {
      reset()
      onClose()
    },
  })

  function reset() {
    setTpl(null)
    setValues({})
    setName('')
    setPreview(null)
    setErr('')
    setAdvanced(false)
    setDatasourceId(null)
  }

  function setField(k: string, v: string | string[]) {
    setValues((s) => ({ ...s, [k]: v }))
    // 改了参数，之前生成的查询就过期了 —— 留着它会让人以为保存的是新的
    setPreview(null)
  }

  const missing = (tpl?.fields ?? []).filter(
    (f) => f.required && !(Array.isArray(values[f.key]) ? (values[f.key] as string[]).length : values[f.key]),
  )

  return (
    <Dialog
      open={open}
      onClose={() => {
        reset()
        onClose()
      }}
      title={tpl ? t(`opsalert:tplDef.${tpl.key}.name`) : t('opsalert:tpl.title')}
      description={tpl ? t(`opsalert:tplDef.${tpl.key}.when`) : t('opsalert:tpl.desc')}
      closeLabel={t('opsalert:rules.cancel')}
      width={760}
      footer={
        <div className="flex justify-end gap-2">
          <Button
            variant="ghost"
            onClick={() => {
              // 指定了模板时没有"上一步"可回，直接关闭
              if (tpl && !initialTemplate) reset()
              else {
                reset()
                onClose()
              }
            }}
          >
            {tpl && !initialTemplate ? t('opsalert:tpl.back') : t('opsalert:rules.cancel')}
          </Button>
          {tpl ? (
            <>
              <Button
                variant="default"
                loading={doPreview.isPending}
                disabled={missing.length > 0}
                title={missing.length ? t('opsalert:tpl.fillFirst') : undefined}
                onClick={() => doPreview.mutate()}
              >
                {t('opsalert:tpl.preview')}
              </Button>
              <WriteButton
                perm="alert:manage_rules"
                loading={create.isPending}
                blockedReason={
                  !preview
                    ? t('opsalert:tpl.previewFirst')
                    : !name.trim()
                      ? t('opsalert:tpl.needName')
                      : datasourceId === null
                        ? t('opsalert:import.pickDatasource')
                        : undefined
                }
                onClick={() => create.mutate()}
              >
                {editing ? t('opsalert:tpl.saveEdit') : t('opsalert:tpl.save')}
              </WriteButton>
            </>
          ) : null}
        </div>
      }
    >
      {!tpl ? (
        <div className="flex flex-col gap-2">
          {(templates.data?.items ?? []).map((it) => (
            <button
              key={it.key}
              type="button"
              className="cursor-pointer rounded-md border border-border p-3 text-left transition-colors hover:bg-muted"
              onClick={() => {
                setTpl(it)
                const init: Record<string, string | string[]> = {}
                for (const f of it.fields) init[f.key] = f.type === 'tags' ? [] : (f.default ?? '')
                setValues(init)
              }}
            >
              <div className="text-sm font-medium">{t(`opsalert:tplDef.${it.key}.name`)}</div>
              <div className="mt-1 text-xs text-muted-foreground">
                {t(`opsalert:tplDef.${it.key}.when`)}
              </div>
            </button>
          ))}
        </div>
      ) : (
        <div className="flex flex-col gap-3">
          <Field label={t('opsalert:import.datasource')} required>
            <select
              className="w-full rounded-md border border-border bg-background px-2 py-1.5 text-sm"
              value={datasourceId ?? ''}
              onChange={(e) => setDatasourceId(e.target.value ? Number(e.target.value) : null)}
            >
              <option value="">{t('opsalert:import.pickDatasource')}</option>
              {(datasources.data?.items ?? []).map((d) => (
                <option key={d.id} value={d.id}>
                  {d.name}（{d.type}）
                </option>
              ))}
            </select>
          </Field>

          <Field label={t('opsalert:tpl.name')} required>
            <input
              className="w-full rounded-md border border-border bg-background px-2 py-1.5 text-sm"
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder={t('opsalert:tpl.namePlaceholder')}
            />
          </Field>

          {tpl.fields.map((f) => (
            <Field
              key={f.key}
              label={t(`opsalert:tplDef.${tpl.key}.f.${f.key}.label`)}
              help={t(`opsalert:tplDef.${tpl.key}.f.${f.key}.help`, { defaultValue: '' })}
              required={f.required}
            >
              {f.type === 'tags' ? (
                <TagsInput
                  value={(values[f.key] as string[]) ?? []}
                  onChange={(v) => setField(f.key, v)}
                  placeholder={f.example ?? ''}
                  addLabel={t('opsalert:tpl.add')}
                />
              ) : (
                <input
                  className="w-full rounded-md border border-border bg-background px-2 py-1.5 font-mono text-xs"
                  value={(values[f.key] as string) ?? ''}
                  inputMode={f.type === 'number' ? 'numeric' : undefined}
                  onChange={(e) => setField(f.key, e.target.value)}
                  placeholder={f.example ?? ''}
                />
              )}
            </Field>
          ))}

          <button
            type="button"
            className="cursor-pointer text-left text-xs text-muted-foreground hover:text-foreground"
            onClick={() => setAdvanced((v) => !v)}
          >
            {advanced ? '▾ ' : '▸ '}
            {t('opsalert:tpl.advanced')}
          </button>
          {advanced ? (
            <div className="grid grid-cols-3 gap-2">
              <Field label={t('opsalert:tpl.interval')} help={t('opsalert:tpl.intervalHelp')}>
                <input
                  className="w-full rounded-md border border-border bg-background px-2 py-1.5 text-sm"
                  value={interval}
                  onChange={(e) => setIntervalSec(e.target.value)}
                />
              </Field>
              <Field label={t('opsalert:tpl.forPeriods')} help={t('opsalert:tpl.forPeriodsHelp')}>
                <input
                  className="w-full rounded-md border border-border bg-background px-2 py-1.5 text-sm"
                  value={forPeriods}
                  onChange={(e) => setForPeriods(e.target.value)}
                />
              </Field>
              <Field label={t('opsalert:tpl.severity')}>
                <select
                  className="w-full rounded-md border border-border bg-background px-2 py-1.5 text-sm"
                  value={severity}
                  onChange={(e) => setSeverity(e.target.value)}
                >
                  <option value="critical">{t('opsalert:severity.critical')}</option>
                  <option value="warning">{t('opsalert:severity.warning')}</option>
                  <option value="info">{t('opsalert:severity.info')}</option>
                </select>
              </Field>
            </div>
          ) : null}

          {err ? <Banner tone="bad">{err}</Banner> : null}

          {preview ? (
            <div className="rounded-md border border-border p-3">
              <div className="mb-2 flex flex-wrap items-center gap-2 text-xs">
                <Badge tone="ok">{t('opsalert:tpl.generated')}</Badge>
                <span className="font-mono text-muted-foreground">{preview.kind}</span>
                <span className="text-muted-foreground">
                  {t('opsalert:tpl.lookback', { sec: preview.lookback_sec })}
                </span>
                {preview.extra?.ignored_codes?.length ? (
                  <Badge tone="warn">
                    {t('opsalert:tpl.ignored', { count: preview.extra.ignored_codes.length })}
                  </Badge>
                ) : null}
              </div>
              {/* 生成的查询原样摆出来：模板隐藏复杂度，但不能隐藏结果 */}
              <pre className="overflow-x-auto rounded bg-muted p-2 font-mono text-xs">
                {preview.query}
              </pre>
            </div>
          ) : null}
        </div>
      )}
    </Dialog>
  )
}

function Field({
  label,
  help,
  required,
  children,
}: {
  label: string
  help?: string
  required?: boolean
  children: React.ReactNode
}) {
  return (
    <label className="flex flex-col gap-1">
      <span className="text-xs font-medium">
        {label}
        {required ? <span className="text-danger"> *</span> : null}
      </span>
      {children}
      {help ? <span className="text-xs text-muted-foreground">{help}</span> : null}
    </label>
  )
}

/** 多值输入。回车或点「添加」入列，点标签上的 × 删除。 */
function TagsInput({
  value,
  onChange,
  placeholder,
  addLabel,
}: {
  value: string[]
  onChange: (v: string[]) => void
  placeholder: string
  addLabel: string
}) {
  const [draft, setDraft] = useState('')
  function add() {
    const v = draft.trim()
    // 去重：同一个命名空间填两遍会让生成的正则出现重复分支
    if (v && !value.includes(v)) onChange([...value, v])
    setDraft('')
  }
  return (
    <div className="flex flex-col gap-1.5">
      <div className="flex gap-1.5">
        <input
          className="w-full rounded-md border border-border bg-background px-2 py-1.5 font-mono text-xs"
          value={draft}
          placeholder={placeholder}
          onChange={(e) => setDraft(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === 'Enter') {
              e.preventDefault()
              add()
            }
          }}
        />
        <Button size="sm" variant="default" onClick={add} disabled={!draft.trim()}>
          {addLabel}
        </Button>
      </div>
      {value.length ? (
        <div className="flex flex-wrap gap-1">
          {value.map((v) => (
            <span
              key={v}
              className="inline-flex items-center gap-1 rounded border border-border px-1.5 py-0.5 font-mono text-xs"
            >
              {v}
              <button
                type="button"
                className="cursor-pointer text-muted-foreground hover:text-danger"
                onClick={() => onChange(value.filter((x) => x !== v))}
                aria-label={`remove ${v}`}
              >
                ×
              </button>
            </span>
          ))}
        </div>
      ) : null}
    </div>
  )
}
