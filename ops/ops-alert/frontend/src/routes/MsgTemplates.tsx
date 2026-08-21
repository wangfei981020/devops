import { useTranslation } from '@ops/i18n'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { AsyncBoundary, Badge, Banner, Button, EmptyState, Skeleton, TextInput, cn, fromQuery } from '@ops/ui'
import { WriteButton } from '../components/WriteButton.js'
import { useState } from 'react'
import { del, get, post, put, makeLoadError } from '../lib/api.js'

type Tpl = {
  id: number
  name: string
  description: string
  channel_type: string
  title_tmpl: string
  body_tmpl: string
  is_builtin: boolean
  created_by: string
  updated_at: string | null
}
type Variable = { name: string; desc: string }
type Preview = { title: string; body: string; warning: string; sample_from: string }

/**
 * 告警文案模板库。
 *
 * 文案原来写死在代码里，部署方想改措辞只能改代码重新构建 ——
 * 而这套系统要交付给多个子公司自建部署，他们没有这条路。
 *
 * 🔴 三条设计约束，界面上都要能看出来：
 *   1. 渲染失败**不吞告警** —— 回落到内置文案照常发，并在消息里附一句警告
 *   2. 预览拿**真实事件**渲染 —— 假数据每个变量都有值，会把"这变量其实取不到"藏起来
 *   3. 内置模板不可改不可删 —— 它是兜底，兜底必须永远可用
 */
export function MsgTemplatesPage() {
  const { t } = useTranslation()
  const qc = useQueryClient()
  const q = useQuery({
    queryKey: ['msg-templates'],
    queryFn: () => get<{ items: Tpl[]; variables: Variable[] }>('/msg-templates'),
  })
  const [editing, setEditing] = useState<Tpl | null>(null)
  const [creating, setCreating] = useState(false)

  const remove = useMutation({
    mutationFn: (id: number) => del(`/msg-templates/${id}`),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['msg-templates'] }),
  })

  return (
    <div className="flex flex-col gap-3">
      <Banner tone="info">{t('opsalert:msgtpl.intro')}</Banner>

      <div className="flex items-center gap-2">
        <WriteButton perm="alert:manage_msg_templates" onClick={() => { setCreating(true); setEditing(null) }}>
          {t('opsalert:msgtpl.create')}
        </WriteButton>
      </div>

      {remove.isError && <Banner tone="bad">{String((remove.error as Error).message)}</Banner>}

      <AsyncBoundary
        state={fromQuery(q, (d) => d.items.length === 0, makeLoadError(t))}
        pending={<Skeleton className="h-48 w-full" />}
        empty={<EmptyState title={t('opsalert:msgtpl.emptyTitle')} reason={t('opsalert:msgtpl.emptyReason')} action={null} />}
        errorTitle={t('opsalert:msgtpl.loadError')}
        retryLabel={t('action.retry')}
        onRetry={() => q.refetch()}
      >
        {(data) => (
          <>
            <ul className="divide-y divide-border overflow-hidden rounded-lg border border-border bg-card">
              {data.items.map((tpl) => (
                <li key={tpl.id} className="flex flex-wrap items-start gap-x-3 gap-y-1 px-3.5 py-2.5">
                  <span className="flex min-w-0 flex-1 flex-col gap-0.5">
                    <span className="flex items-center gap-2">
                      <span className="truncate text-sm font-medium">{tpl.name}</span>
                      {tpl.is_builtin && <Badge tone="mute">{t('opsalert:msgtpl.builtin')}</Badge>}
                      {tpl.channel_type && <Badge tone="info">{tpl.channel_type}</Badge>}
                    </span>
                    {tpl.description && (
                      <span className="text-2xs text-muted-foreground">{tpl.description}</span>
                    )}
                    <code className="mt-1 whitespace-pre-wrap break-all font-mono text-[10.5px] text-muted-foreground">
                      {tpl.title_tmpl}
                    </code>
                  </span>
                  <span className="flex shrink-0 gap-1">
                    <Button size="sm" variant="ghost" onClick={() => { setEditing(tpl); setCreating(false) }}>
                      {tpl.is_builtin ? t('action.view') : t('action.edit')}
                    </Button>
                    {!tpl.is_builtin && (
                      <WriteButton
                        perm="alert:manage_msg_templates"
                        loading={remove.isPending}
                        onClick={() => remove.mutate(tpl.id)}
                      >
                        {t('action.delete')}
                      </WriteButton>
                    )}
                  </span>
                </li>
              ))}
            </ul>

            {(editing || creating) && (
              <Editor
                tpl={editing}
                variables={data.variables}
                onClose={() => { setEditing(null); setCreating(false) }}
                onSaved={() => {
                  qc.invalidateQueries({ queryKey: ['msg-templates'] })
                  setEditing(null)
                  setCreating(false)
                }}
              />
            )}
          </>
        )}
      </AsyncBoundary>
    </div>
  )
}

function Editor({
  tpl,
  variables,
  onClose,
  onSaved,
}: {
  tpl: Tpl | null
  variables: Variable[]
  onClose: () => void
  onSaved: () => void
}) {
  const { t } = useTranslation()
  const readOnly = tpl?.is_builtin ?? false
  const [name, setName] = useState(tpl?.name ?? '')
  const [desc, setDesc] = useState(tpl?.description ?? '')
  const [channel, setChannel] = useState(tpl?.channel_type ?? '')
  const [title, setTitle] = useState(tpl?.title_tmpl ?? '{{rule}}')
  const [body, setBody] = useState(tpl?.body_tmpl ?? '')

  const save = useMutation({
    mutationFn: () => {
      const payload = {
        name, description: desc, channel_type: channel,
        title_tmpl: title, body_tmpl: body,
      }
      return tpl ? put(`/msg-templates/${tpl.id}`, payload) : post('/msg-templates', payload)
    },
    onSuccess: onSaved,
  })
  const preview = useMutation({
    mutationFn: () => post<Preview>('/msg-templates/preview', { title_tmpl: title, body_tmpl: body }),
  })

  return (
    <section className="rounded-lg border border-border bg-card">
      <header className="flex items-center gap-2 border-b border-border px-3.5 py-2">
        <h3 className="text-xs font-semibold">
          {tpl ? (readOnly ? t('opsalert:msgtpl.viewTitle') : t('opsalert:msgtpl.editTitle')) : t('opsalert:msgtpl.createTitle')}
        </h3>
        <Button size="sm" variant="ghost" className="ml-auto" onClick={onClose}>
          {t('action.close')}
        </Button>
      </header>
      <div className="grid gap-3 px-3.5 py-3 lg:grid-cols-[minmax(0,1fr)_280px]">
        <div className="flex flex-col gap-2.5">
          {readOnly && <Banner tone="info">{t('opsalert:msgtpl.builtinReadonly')}</Banner>}

          <Field label={t('opsalert:msgtpl.name')}>
            <TextInput value={name} onChange={(e) => setName(e.target.value)} disabled={readOnly} />
          </Field>
          <Field label={t('opsalert:msgtpl.desc')}>
            <TextInput value={desc} onChange={(e) => setDesc(e.target.value)} disabled={readOnly} />
          </Field>
          <Field label={t('opsalert:msgtpl.channel')} hint={t('opsalert:msgtpl.channelHint')}>
            <TextInput
              value={channel}
              onChange={(e) => setChannel(e.target.value)}
              placeholder={t('opsalert:msgtpl.channelAny')}
              disabled={readOnly}
            />
          </Field>
          <Field label={t('opsalert:msgtpl.title')}>
            <TextInput value={title} onChange={(e) => setTitle(e.target.value)} disabled={readOnly} />
          </Field>
          <Field label={t('opsalert:msgtpl.body')}>
            <textarea
              value={body}
              onChange={(e) => setBody(e.target.value)}
              rows={8}
              disabled={readOnly}
              className="w-full rounded-md border border-border bg-card px-2.5 py-2 font-mono text-[11px] outline-none focus:border-primary disabled:opacity-60"
            />
          </Field>

          <div className="flex flex-wrap items-center gap-2">
            {!readOnly && (
              <WriteButton
                perm="alert:manage_msg_templates"
                loading={save.isPending}
                blockedReason={!name || !body ? t('opsalert:msgtpl.needNameBody') : undefined}
                onClick={() => save.mutate()}
              >
                {t('action.save')}
              </WriteButton>
            )}
            {/* 预览对内置模板也开放：值班的人收到看不懂的告警时，
                第一件事就是来看这条文案是怎么拼出来的 */}
            <Button variant="ghost" loading={preview.isPending} onClick={() => preview.mutate()}>
              {t('opsalert:msgtpl.preview')}
            </Button>
          </div>

          {save.isError && <Banner tone="bad">{String((save.error as Error).message)}</Banner>}
          {preview.isError && <Banner tone="bad">{String((preview.error as Error).message)}</Banner>}

          {preview.data && (
            <div className="flex flex-col gap-2 rounded-md border border-border bg-muted/30 p-3">
              <div className="text-2xs text-muted-foreground">
                {t('opsalert:msgtpl.renderedFrom', { from: preview.data.sample_from })}
              </div>
              {/* ⚠️ 未定义变量的警告必须显示。不显示的话，模板里的错别字
                  会一直存在，直到某天有人问"这条告警怎么写着 (无此变量)" */}
              {preview.data.warning && <Banner tone="warn">{preview.data.warning}</Banner>}
              <div className="text-sm font-medium">{preview.data.title}</div>
              <pre className="whitespace-pre-wrap break-all font-mono text-[11px] leading-relaxed">
                {preview.data.body}
              </pre>
            </div>
          )}
        </div>

        <aside className="flex flex-col gap-2">
          <h4 className="text-2xs font-semibold text-muted-foreground">{t('opsalert:msgtpl.vars')}</h4>
          <p className="text-2xs text-muted-foreground">{t('opsalert:msgtpl.varsHint')}</p>
          <ul className="flex flex-col gap-1.5">
            {variables.map((v) => (
              <li key={v.name} className="text-2xs">
                <button
                  type="button"
                  disabled={readOnly}
                  onClick={() => setBody((b) => b + '{{' + v.name + '}}')}
                  className={cn(
                    'rounded border border-border px-1 font-mono',
                    readOnly ? 'opacity-60' : 'cursor-pointer hover:border-primary hover:text-primary',
                  )}
                >
                  {'{{' + v.name + '}}'}
                </button>
                <span className="ml-1.5 text-muted-foreground">{v.desc}</span>
              </li>
            ))}
          </ul>
          <p className="mt-1 text-2xs text-muted-foreground">{t('opsalert:msgtpl.varsExtra')}</p>
        </aside>
      </div>
    </section>
  )
}

function Field({ label, hint, children }: { label: string; hint?: string; children: React.ReactNode }) {
  return (
    <label className="flex flex-col gap-1">
      <span className="text-2xs text-muted-foreground">{label}</span>
      {children}
      {hint && <span className="text-2xs text-muted-foreground">{hint}</span>}
    </label>
  )
}
