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
  SecretInput,
  Select,
  TableSkeleton,
  TextArea,
  TextInput,
  type ColumnDef,
  fromQuery,
} from '@ops/ui'
import { WriteButton } from '../components/WriteButton.js'
import { useState } from 'react'
import { del, get, post, put, makeLoadError } from '../lib/api.js'

type Datasource = {
  id: number
  name: string
  type: string
  endpoint: string
  status: string
  probe_at?: string
  probe_ms: number
  probe_error: string
  last_data_at?: string
}

export function DatasourcesPage() {
  const { t } = useTranslation()
  const qc = useQueryClient()
  const [creating, setCreating] = useState(false)
  const [editRow, setEditRow] = useState<Datasource | null>(null)
  const query = useQuery({ queryKey: ['datasources'], queryFn: () => get<{ items: Datasource[] }>('/datasources') })
  const refresh = () => qc.invalidateQueries({ queryKey: ['datasources'] })

  const test = useMutation({
    mutationFn: (id: number) => post<{ ok: boolean; error?: string; latency_ms?: number }>(`/datasources/${id}/test`),
    onSuccess: refresh,
  })
  const remove = useMutation({ mutationFn: (id: number) => del(`/datasources/${id}`), onSuccess: refresh })

  const columns: ColumnDef<Datasource, unknown>[] = [
    { header: t('opsalert:datasources.colName'), accessorKey: 'name' },
    {
      header: t('opsalert:datasources.colType'),
      accessorKey: 'type',
      cell: ({ row }) => (
        <Badge tone="mute" dot={false}>
          {row.original.type}
        </Badge>
      ),
    },
    { header: t('opsalert:datasources.colEndpoint'), accessorKey: 'endpoint', cell: ({ row }) => <span className="font-mono text-xs">{row.original.endpoint}</span> },
    {
      header: t('opsalert:datasources.colStatus'),
      accessorKey: 'status',
      cell: ({ row }) => {
        const d = row.original
        // 不可达必须是显性状态。只在点"测试"时才知道的话，
        // 坏掉的数据源在没人点的那几天里表现为"一直没有告警"。
        if (d.status === 'down')
          return (
            <Badge tone="bad">
              {t('opsalert:datasources.unreachable')}：{d.probe_error.slice(0, 40)}
            </Badge>
          )
        if (d.status === 'up')
          return (
            <Badge tone="ok">
              {t('opsalert:datasources.normal')} · {d.probe_ms}ms
            </Badge>
          )
        return <Badge tone="mute">{t('opsalert:datasources.unprobed')}</Badge>
      },
    },
    {
      header: t('opsalert:rules.colOps'),
      id: 'ops',
      cell: ({ row }) => (
        <div className="flex gap-1">
          <WriteButton perm="alert:manage_datasources" size="sm" variant="ghost" loading={test.isPending} onClick={() => test.mutate(row.original.id)}>
            {t('opsalert:datasources.test')}
          </WriteButton>
          <WriteButton
            perm="alert:manage_datasources"
            size="sm"
            variant="ghost"
            onClick={() => setEditRow(row.original)}
          >
            {t('opsalert:rules.edit')}
          </WriteButton>
          <WriteButton perm="alert:manage_datasources" size="sm" variant="ghost" onClick={() => remove.mutate(row.original.id)}>
            {t('opsalert:datasources.delete')}
          </WriteButton>
        </div>
      ),
    },
  ]

  return (
    <div className="flex flex-col gap-3">
      <div className="flex items-center gap-2">
        <span className="text-xs text-muted-foreground">
          {t('opsalert:datasources.hint')}
        </span>
        <WriteButton perm="alert:manage_datasources" size="sm" className="ml-auto" onClick={() => setCreating(true)}>
          {t('opsalert:datasources.create')}
        </WriteButton>
      </div>
      {test.data && !test.data.ok && (
        <div className="rounded-md border border-danger/40 bg-danger-bg p-3 text-xs text-danger">
          {t('opsalert:datasources.testFailed')}：{test.data.error}
        </div>
      )}
      <AsyncBoundary
        state={fromQuery(query, (d) => d.items.length === 0, makeLoadError(t))}
        pending={<TableSkeleton columns={[20, 12, 34, 22, 12]} rows={4} />}
        empty={
          <EmptyState
            title={t('opsalert:datasources.emptyTitle')}
            reason={t('opsalert:datasources.emptyReason')}
            action={{ label: t('opsalert:datasources.create'), onClick: () => setCreating(true) }}
          />
        }
        errorTitle={t('opsalert:datasources.loadError')}
        retryLabel={t('action.retry')}
        onRetry={() => query.refetch()}
      >
        {(data) => <DataTable columns={columns} data={data.items} rowKey={(r) => String(r.id)} />}
      </AsyncBoundary>
      {creating && <CreateDatasourceDialog onClose={() => setCreating(false)} onCreated={refresh} />}
      {editRow && (
        <CreateDatasourceDialog
          editRow={editRow}
          onClose={() => setEditRow(null)}
          onCreated={refresh}
        />
      )}
    </div>
  )
}

/**
 * 数据源表单。新建与编辑共用。
 *
 * ⚠️ 凭据（密码 / api_key）**不回填**：后端出参里本来就不该带明文密钥。
 * 留空表示不改，填了才覆盖 —— 直接显示一串星号再原样提交，
 * 会把真实凭据改成那串星号。
 */
function CreateDatasourceDialog({
  onClose,
  onCreated,
  editRow,
}: {
  onClose: () => void
  onCreated: () => void
  editRow?: Datasource | null
}) {
  const { t } = useTranslation()
  const [name, setName] = useState(editRow?.name ?? '')
  const [type, setType] = useState<'loki' | 'elasticsearch' | 'opensearch'>(
    (editRow?.type as 'loki') ?? 'loki',
  )
  const [endpoint, setEndpoint] = useState(editRow?.endpoint ?? '')
  const [username, setUsername] = useState('')
  const [password, setPassword] = useState('')
  const [apiKey, setApiKey] = useState('')
  const [orgID, setOrgID] = useState('')
  const [indexPattern, setIndexPattern] = useState('')

  const create = useMutation({
    mutationFn: () => {
      const payload = {
        name,
        type,
        endpoint,
        // 编辑时留空的凭据不提交，避免把原值清掉
        auth: { username, password, api_key: apiKey, org_id: orgID },
        spec: { index_pattern: indexPattern },
      }
      // 两支分开写而不是 (cond ? put : post)(…)：静态扫描看不出后者调了什么接口
      if (editRow) return put(`/datasources/${editRow.id}`, payload)
      return post('/datasources', payload)
    },
    onSuccess: () => {
      onCreated()
      onClose()
    },
  })

  return (
    <Dialog
      open
      onClose={onClose}
      closeLabel={t('action.close')}
      title={editRow ? t('opsalert:datasources.editTitle') : t('opsalert:datasources.formTitle')}
      description={t('opsalert:datasources.formDesc')}
      footer={
        <>
          <Button variant="ghost" onClick={onClose}>
            {t('action.cancel')}
          </Button>
          <WriteButton perm="alert:manage_datasources" loading={create.isPending} blockedReason={!name || !endpoint ? t('opsalert:perm.fillRequired') : undefined} onClick={() => create.mutate()}>
            {t('action.save')}
          </WriteButton>
        </>
      }
    >
      <div className="flex flex-col gap-3">
        <Field label={t('opsalert:datasources.colName')} required>
          <TextInput value={name} onChange={(e) => setName(e.target.value)} />
        </Field>
        <Field label={t('opsalert:datasources.colType')}>
          <Select
            label={t('opsalert:datasources.colType')}
            value={type}
            onChange={setType}
            options={[
              { value: 'loki', label: 'Loki' },
              { value: 'elasticsearch', label: 'Elasticsearch' },
              { value: 'opensearch', label: 'OpenSearch' },
            ]}
          />
        </Field>
        <Field label={t('opsalert:datasources.colEndpoint')} hint={t('opsalert:datasources.endpointHint')} required>
          <TextInput value={endpoint} onChange={(e) => setEndpoint(e.target.value)} />
        </Field>
        {type === 'loki' ? (
          <Field label={t('opsalert:datasources.orgId')} hint={t('opsalert:datasources.orgIdHint')}>
            <TextInput value={orgID} onChange={(e) => setOrgID(e.target.value)} />
          </Field>
        ) : (
          <Field label={t('opsalert:datasources.indexPattern')} hint={t('opsalert:datasources.indexPatternHint')}>
            <TextInput value={indexPattern} onChange={(e) => setIndexPattern(e.target.value)} />
          </Field>
        )}
        <div className="grid grid-cols-2 gap-3">
          <Field label={t('opsalert:signIn.username')}>
            <TextInput value={username} onChange={(e) => setUsername(e.target.value)} />
          </Field>
          <Field label={t('opsalert:signIn.password')}>
            <SecretInput configured={false}
              placeholderConfigured={t('opsalert:datasources.secretConfigured')}
              placeholderEmpty=""
              showLabel={t('action.showSecret')}
              hideLabel={t('action.hideSecret')} value={password} onChange={(e) => setPassword(e.target.value)} />
          </Field>
        </div>
        {type !== 'loki' && (
          <Field label="API Key" hint={t('opsalert:datasources.apiKeyHint')}>
            <SecretInput configured={false}
              placeholderConfigured={t('opsalert:datasources.secretConfigured')}
              placeholderEmpty=""
              showLabel={t('action.showSecret')}
              hideLabel={t('action.hideSecret')} value={apiKey} onChange={(e) => setApiKey(e.target.value)} />
          </Field>
        )}
      </div>
    </Dialog>
  )
}

type Notifier = { id: number; name: string; type: string; status: string }

export function NotifiersPage() {
  const { t } = useTranslation()
  const qc = useQueryClient()
  const [creating, setCreating] = useState(false)
  const query = useQuery({ queryKey: ['notifiers'], queryFn: () => get<{ items: Notifier[] }>('/notifiers') })
  const refresh = () => qc.invalidateQueries({ queryKey: ['notifiers'] })
  const test = useMutation({ mutationFn: (id: number) => post<{ ok: boolean; error?: string }>(`/notifiers/${id}/test`) })
  const remove = useMutation({ mutationFn: (id: number) => del(`/notifiers/${id}`), onSuccess: refresh })

  const columns: ColumnDef<Notifier, unknown>[] = [
    { header: t('opsalert:datasources.colName'), accessorKey: 'name' },
    { header: t('opsalert:datasources.colType'), accessorKey: 'type' },
    {
      header: t('opsalert:datasources.colStatus'),
      accessorKey: 'status',
      cell: ({ row }) => <Badge tone="ok">{row.original.status}</Badge>,
    },
    {
      header: t('opsalert:rules.colOps'),
      id: 'ops',
      cell: ({ row }) => (
        <div className="flex gap-1">
          <WriteButton perm="alert:manage_notifiers" size="sm" variant="ghost" loading={test.isPending} onClick={() => test.mutate(row.original.id)}>
            {t('opsalert:notifiers.sendTest')}
          </WriteButton>
          <WriteButton perm="alert:manage_notifiers" size="sm" variant="ghost" onClick={() => remove.mutate(row.original.id)}>
            {t('opsalert:datasources.delete')}
          </WriteButton>
        </div>
      ),
    },
  ]

  return (
    <div className="flex flex-col gap-3">
      <div className="flex items-center gap-2">
        <span className="text-xs text-muted-foreground">{t('opsalert:notifiers.hint')}</span>
        <WriteButton perm="alert:manage_notifiers" size="sm" className="ml-auto" onClick={() => setCreating(true)}>
          {t('opsalert:notifiers.create')}
        </WriteButton>
      </div>
      {test.data && (
        <div className={`rounded-md border p-3 text-xs ${test.data.ok ? 'border-success/50 bg-success/5' : 'border-danger/40 bg-danger-bg text-danger'}`}>
          {test.data.ok
            ? t('opsalert:notifiers.testOk')
            : `${t('opsalert:notifiers.testFailed')}：${test.data.error}`}
        </div>
      )}
      <AsyncBoundary
        state={fromQuery(query, (d) => d.items.length === 0, makeLoadError(t))}
        pending={<TableSkeleton columns={[30, 20, 20, 30]} rows={3} />}
        empty={
          <EmptyState
            title={t('opsalert:notifiers.emptyTitle')}
            reason={t('opsalert:notifiers.emptyReason')}
            action={{ label: t('opsalert:notifiers.create'), onClick: () => setCreating(true) }}
          />
        }
        errorTitle={t('opsalert:notifiers.loadError')}
        retryLabel={t('action.retry')}
        onRetry={() => query.refetch()}
      >
        {(data) => <DataTable columns={columns} data={data.items} rowKey={(r) => String(r.id)} />}
      </AsyncBoundary>
      {creating && <CreateNotifierDialog onClose={() => setCreating(false)} onCreated={refresh} />}
    </div>
  )
}

function CreateNotifierDialog({ onClose, onCreated }: { onClose: () => void; onCreated: () => void }) {
  const { t } = useTranslation()
  const [name, setName] = useState('')
  const [type, setType] = useState<'feishu' | 'webhook'>('feishu')
  const [url, setUrl] = useState('')
  const [secret, setSecret] = useState('')

  const create = useMutation({
    mutationFn: () =>
      post('/notifiers', {
        name,
        type,
        config: type === 'feishu' ? { webhook_url: url, secret } : { url, secret },
      }),
    onSuccess: () => {
      onCreated()
      onClose()
    },
  })

  return (
    <Dialog
      open
      onClose={onClose}
      closeLabel={t('action.close')}
      title={t('opsalert:notifiers.formTitle')}
      footer={
        <>
          <Button variant="ghost" onClick={onClose}>
            {t('action.cancel')}
          </Button>
          <WriteButton perm="alert:manage_notifiers" loading={create.isPending} blockedReason={!name || !url ? t('opsalert:perm.fillRequired') : undefined} onClick={() => create.mutate()}>
            {t('action.save')}
          </WriteButton>
        </>
      }
    >
      <div className="flex flex-col gap-3">
        <Field label={t('opsalert:datasources.colName')} required>
          <TextInput value={name} onChange={(e) => setName(e.target.value)} />
        </Field>
        <Field label={t('opsalert:datasources.colType')}>
          <Select
            label={t('opsalert:datasources.colType')}
            value={type}
            onChange={setType}
            options={[
              { value: 'feishu', label: t('opsalert:notifiers.typeFeishu') },
              { value: 'webhook', label: t('opsalert:notifiers.typeWebhook') },
            ]}
          />
        </Field>
        <Field
          label={type === 'feishu' ? t('opsalert:notifiers.url') : t('opsalert:notifiers.urlCallback')}
          required
        >
          <TextInput value={url} onChange={(e) => setUrl(e.target.value)} />
        </Field>
        <Field
          label={t('opsalert:notifiers.secret')}
          hint={
            type === 'feishu'
              ? t('opsalert:notifiers.secretHintFeishu')
              : t('opsalert:notifiers.secretHintWebhook')
          }
        >
          <SecretInput configured={false}
              placeholderConfigured={t('opsalert:datasources.secretConfigured')}
              placeholderEmpty=""
              showLabel={t('action.showSecret')}
              hideLabel={t('action.hideSecret')} value={secret} onChange={(e) => setSecret(e.target.value)} />
        </Field>
      </div>
    </Dialog>
  )
}

type Route = {
  id: number
  name: string
  matchers: { key: string; op: string; value: string }[]
  notifier_ids: number[]
  repeat_sec: number
  is_fallback: boolean
}

export function RoutesPage() {
  const { t } = useTranslation()
  const qc = useQueryClient()
  const [creating, setCreating] = useState(false)
  const query = useQuery({ queryKey: ['routes'], queryFn: () => get<{ items: Route[] }>('/routes') })
  const notifiers = useQuery({ queryKey: ['notifiers'], queryFn: () => get<{ items: Notifier[] }>('/notifiers') })
  const [labels, setLabels] = useState('severity=critical,env=prod')
  const [sim, setSim] = useState<{ reason: string; notifiers: string[]; will_notify: boolean } | null>(null)

  const simulate = useMutation({
    mutationFn: () => {
      const obj: Record<string, string> = {}
      for (const pair of labels.split(',')) {
        const [k, v] = pair.split('=')
        if (k && v) obj[k.trim()] = v.trim()
      }
      return post<{ reason: string; notifiers: string[]; will_notify: boolean }>('/routes/simulate', { labels: obj })
    },
    onSuccess: setSim,
  })
  const remove = useMutation({
    mutationFn: (id: number) => del(`/routes/${id}`),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['routes'] }),
  })

  const nameOf = (id: number) => notifiers.data?.items.find((n) => n.id === id)?.name ?? `#${id}`

  return (
    <div className="flex flex-col gap-4">
      {creating && (
        <CreateRouteDialog
          onClose={() => setCreating(false)}
          onCreated={() => qc.invalidateQueries({ queryKey: ['routes'] })}
        />
      )}
      <AsyncBoundary
        state={fromQuery(query, (d) => d.items.length === 0, makeLoadError(t))}
        pending={<TableSkeleton columns={[24, 34, 22, 20]} rows={3} />}
        empty={
          <EmptyState
            title={t('opsalert:routes.emptyTitle')}
            reason={t('opsalert:routes.emptyReason')}
            action={null}
          />
        }
        errorTitle={t('opsalert:routes.loadError')}
        retryLabel={t('action.retry')}
        onRetry={() => query.refetch()}
      >
        {(data) => (
          <ul className="divide-y divide-border rounded-lg border border-border">
            {data.items.map((r) => (
              <li key={r.id} className="flex items-center gap-3 px-4 py-3 text-sm">
                <div className="min-w-0 flex-1">
                  <div className="flex items-center gap-2">
                    <span className="font-medium">{r.name}</span>
                    {r.is_fallback && (
                      <Badge tone="mute" dot={false}>
                        {t('opsalert:routes.fallback')}
                      </Badge>
                    )}
                  </div>
                  <div className="mt-1 font-mono text-xs text-muted-foreground">
                    {r.matchers?.length
                      ? r.matchers.map((m) => `${m.key} ${m.op || '='} ${m.value}`).join(' AND ')
                      : t('opsalert:routes.noCondition')}
                  </div>
                </div>
                <div className="text-xs text-muted-foreground">
                  → {r.notifier_ids?.map(nameOf).join('、') || t('opsalert:routes.noNotifier')}
                  <span className="ml-2">{t('opsalert:routes.repeatSec', { sec: r.repeat_sec })}</span>
                </div>
                <WriteButton perm="alert:manage_routes"
                  size="sm"
                  variant="ghost"
                  blockedReason={r.is_fallback ? t('opsalert:perm.fallbackKeep') : undefined}
                  onClick={() => remove.mutate(r.id)}
                >
                  {t('opsalert:datasources.delete')}
                </WriteButton>
              </li>
            ))}
          </ul>
        )}
      </AsyncBoundary>

      <section className="rounded-lg border border-border p-4">
        <h3 className="text-sm font-semibold">{t('opsalert:routes.simulateTitle')}</h3>
        <p className="mt-1 text-xs text-muted-foreground">{t('opsalert:routes.simulateHint')}</p>
        <div className="mt-3 flex items-end gap-2">
          <div className="flex-1">
            <Field label={t('opsalert:routes.labels')} hint={t('opsalert:routes.labelsHint')}>
              <TextInput value={labels} onChange={(e) => setLabels(e.target.value)} />
            </Field>
          </div>
          <Button loading={simulate.isPending} onClick={() => simulate.mutate()}>
            {t('opsalert:routes.simulate')}
          </Button>
        </div>
        {sim && (
          <div className={`mt-3 rounded-md border p-3 text-xs ${sim.will_notify ? 'border-border' : 'border-danger/40 bg-danger-bg'}`}>
            <div>{sim.reason}</div>
            <div className="mt-1 text-muted-foreground">
              {sim.will_notify
                ? t('opsalert:routes.willNotify', { names: sim.notifiers.join('、') })
                : t('opsalert:routes.willNotNotify')}
            </div>
          </div>
        )}
      </section>
    </div>
  )
}

/**
 * 新建通知路由。
 *
 * ⚠️ 匹配条件用 `k=v,k=v` 而不是让人写 JSON：路由是运维每天要改的东西，
 * 让人手写 JSON 等于每次都可能因为一个引号而配错，而配错的表现是
 * "告警发去了别的群"——没人会立刻发现。
 *
 * ⚠️ 兜底路由不在这里建：它由系统保证存在且不可删（删了就会有告警无处可去）。
 */
function CreateRouteDialog({ onClose, onCreated }: { onClose: () => void; onCreated: () => void }) {
  const { t } = useTranslation()
  const notifiers = useQuery({ queryKey: ['notifiers'], queryFn: () => get<{ items: Notifier[] }>('/notifiers') })
  const [name, setName] = useState('')
  const [matchers, setMatchers] = useState('severity=critical')
  const [picked, setPicked] = useState<number[]>([])
  const [repeatSec, setRepeatSec] = useState('14400')
  const [cont, setCont] = useState(false)

  const create = useMutation({
    mutationFn: () => {
      const obj: Record<string, string> = {}
      for (const pair of matchers.split(',')) {
        const [k, v] = pair.split('=')
        if (k?.trim() && v?.trim()) obj[k.trim()] = v.trim()
      }
      return post('/routes', {
        name,
        matchers: obj,
        notifier_ids: picked,
        repeat_sec: Number(repeatSec) || 0,
        continue: cont,
      })
    },
    onSuccess: () => {
      onCreated()
      onClose()
    },
  })

  return (
    <Dialog
      open
      onClose={onClose}
      closeLabel={t('action.close')}
      title={t('opsalert:routes.formTitle')}
      description={t('opsalert:routes.formDesc')}
      footer={
        <>
          <Button variant="ghost" onClick={onClose}>
            {t('action.cancel')}
          </Button>
          <WriteButton
            perm="alert:manage_routes"
            loading={create.isPending}
            blockedReason={
              !name
                ? t('opsalert:perm.fillRequired')
                : picked.length === 0
                  ? t('opsalert:routes.needNotifier')
                  : undefined
            }
            onClick={() => create.mutate()}
          >
            {t('action.save')}
          </WriteButton>
        </>
      }
    >
      <div className="flex flex-col gap-3">
        <label className="flex flex-col gap-1">
          <span className="text-xs font-medium">{t('opsalert:routes.colName')}</span>
          <input
            className="rounded-md border border-border bg-background px-2 py-1.5 text-sm"
            value={name}
            onChange={(e) => setName(e.target.value)}
          />
        </label>
        <label className="flex flex-col gap-1">
          <span className="text-xs font-medium">{t('opsalert:routes.matchers')}</span>
          <input
            className="rounded-md border border-border bg-background px-2 py-1.5 font-mono text-xs"
            value={matchers}
            onChange={(e) => setMatchers(e.target.value)}
            placeholder="severity=critical,env=prod"
          />
          <span className="text-xs text-muted-foreground">{t('opsalert:routes.matchersHelp')}</span>
        </label>
        <div className="flex flex-col gap-1">
          <span className="text-xs font-medium">{t('opsalert:routes.notifiers')}</span>
          <div className="flex flex-col gap-1">
            {(notifiers.data?.items ?? []).map((n) => (
              <label key={n.id} className="flex cursor-pointer items-center gap-2 text-sm">
                <input
                  type="checkbox"
                  checked={picked.includes(n.id)}
                  onChange={() =>
                    setPicked((p) => (p.includes(n.id) ? p.filter((x) => x !== n.id) : [...p, n.id]))
                  }
                />
                {n.name}
              </label>
            ))}
          </div>
        </div>
        <div className="grid grid-cols-2 gap-3">
          <label className="flex flex-col gap-1">
            <span className="text-xs font-medium">{t('opsalert:routes.repeat')}</span>
            <input
              className="rounded-md border border-border bg-background px-2 py-1.5 text-sm"
              value={repeatSec}
              onChange={(e) => setRepeatSec(e.target.value)}
            />
            <span className="text-xs text-muted-foreground">{t('opsalert:routes.repeatHelp')}</span>
          </label>
          <label className="flex items-center gap-2 self-end text-sm">
            <input type="checkbox" checked={cont} onChange={(e) => setCont(e.target.checked)} />
            {t('opsalert:routes.continue')}
          </label>
        </div>
      </div>
    </Dialog>
  )
}
