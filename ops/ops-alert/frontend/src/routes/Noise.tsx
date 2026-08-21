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
  TableSkeleton,
  TextInput,
  type ColumnDef,
  fromQuery,
} from '@ops/ui'
import { WriteButton } from '../components/WriteButton.js'
import { useState } from 'react'
import { del, get, post, makeLoadError } from '../lib/api.js'

type Silence = {
  id: number
  kind: string
  matchers: { key: string; op: string; value: string }[]
  comment: string
  starts_at: string
  ends_at: string
  renew_count: number
  created_by: string
  active: boolean
  suspicious: boolean
}

export function SilencesPage() {
  const { t } = useTranslation()
  const qc = useQueryClient()
  const [creating, setCreating] = useState(false)
  const query = useQuery({ queryKey: ['silences'], queryFn: () => get<{ items: Silence[] }>('/silences') })
  const refresh = () => qc.invalidateQueries({ queryKey: ['silences'] })
  const remove = useMutation({ mutationFn: (id: number) => del(`/silences/${id}`), onSuccess: refresh })

  const columns: ColumnDef<Silence, unknown>[] = [
    {
      header: t('opsalert:silences.colScope'),
      id: 'matchers',
      cell: ({ row }) => (
        <span className="font-mono text-xs">
          {row.original.matchers?.map((m) => `${m.key}${m.op || '='}${m.value}`).join(' AND ')}
        </span>
      ),
    },
    { header: t('opsalert:silences.colKind'), accessorKey: 'kind', cell: ({ row }) => <Badge tone="mute" dot={false}>{row.original.kind}</Badge> },
    { header: t('opsalert:silences.colComment'), accessorKey: 'comment' },
    {
      header: t('opsalert:silences.colEnds'),
      accessorKey: 'ends_at',
      cell: ({ row }) => <span className="tabular-nums text-muted-foreground">{new Date(row.original.ends_at).toLocaleString()}</span>,
    },
    {
      header: t('opsalert:silences.colState'),
      id: 'state',
      cell: ({ row }) =>
        // 反复续期是「假装没问题」最常见的方式，标出来让它无处躲。
        row.original.suspicious ? (
          <Badge tone="bad">{t('opsalert:silences.suspicious', { count: row.original.renew_count })}</Badge>
        ) : row.original.active ? (
          <Badge tone="warn">{t('opsalert:silences.active')}</Badge>
        ) : (
          <Badge tone="mute">{t('opsalert:silences.inactive')}</Badge>
        ),
    },
    {
      header: t('opsalert:rules.colOps'),
      id: 'ops',
      cell: ({ row }) => (
        <WriteButton perm="alert:manage_silences" size="sm" variant="ghost" onClick={() => remove.mutate(row.original.id)}>
          {t('opsalert:silences.revoke')}
        </WriteButton>
      ),
    },
  ]

  return (
    <div className="flex flex-col gap-3">
      <div className="flex items-center gap-2">
        <span className="text-xs text-muted-foreground">
          {t('opsalert:silences.hint')}
        </span>
        <WriteButton perm="alert:manage_silences" size="sm" className="ml-auto" onClick={() => setCreating(true)}>
          {t('opsalert:silences.create')}
        </WriteButton>
      </div>
      <AsyncBoundary
        state={fromQuery(query, (d) => d.items.length === 0, makeLoadError(t))}
        pending={<TableSkeleton columns={[28, 10, 20, 18, 14, 10]} rows={3} />}
        empty={
          <EmptyState
            title={t('opsalert:silences.emptyTitle')}
            reason={t('opsalert:silences.emptyReason')}
            action={null}
          />
        }
        errorTitle={t('opsalert:silences.loadError')}
        retryLabel={t('action.retry')}
        onRetry={() => query.refetch()}
      >
        {(data) => <DataTable columns={columns} data={data.items} rowKey={(r) => String(r.id)} />}
      </AsyncBoundary>
      {creating && <CreateSilenceDialog onClose={() => setCreating(false)} onCreated={refresh} />}
    </div>
  )
}

function CreateSilenceDialog({ onClose, onCreated }: { onClose: () => void; onCreated: () => void }) {
  const { t } = useTranslation()
  const [key, setKey] = useState('')
  const [value, setValue] = useState('')
  const [hours, setHours] = useState('2')
  const [comment, setComment] = useState('')

  const create = useMutation({
    mutationFn: () =>
      post('/silences', {
        kind: 'silence',
        matchers: [{ key, op: '=', value }],
        hours: Number(hours),
        comment,
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
      title={t('opsalert:silences.formTitle')}
      description={t('opsalert:silences.formDesc')}
      footer={
        <>
          <Button variant="ghost" onClick={onClose}>
            {t('action.cancel')}
          </Button>
          <WriteButton perm="alert:manage_silences" loading={create.isPending} blockedReason={!key || !value || !hours ? t('opsalert:perm.fillRequired') : undefined} onClick={() => create.mutate()}>
            {t('opsalert:silences.submit')}
          </WriteButton>
        </>
      }
    >
      <div className="flex flex-col gap-3">
        <div className="grid grid-cols-2 gap-3">
          <Field label={t('opsalert:silences.labelKey')} required>
            <TextInput value={key} onChange={(e) => setKey(e.target.value)} placeholder="container" />
          </Field>
          <Field label={t('opsalert:silences.labelValue')} required>
            <TextInput value={value} onChange={(e) => setValue(e.target.value)} placeholder="wallet-api" />
          </Field>
        </div>
        <Field label={t('opsalert:silences.hours')} required hint={t('opsalert:silences.hoursHint')}>
          <TextInput value={hours} onChange={(e) => setHours(e.target.value)} />
        </Field>
        <Field label={t('opsalert:silences.reason')} hint={t('opsalert:silences.reasonHint')}>
          <TextInput value={comment} onChange={(e) => setComment(e.target.value)} />
        </Field>
      </div>
    </Dialog>
  )
}

type NoiseItem = {
  rule_id: number
  name: string
  fires_7d: number
  false_positives: number
  fp_rate: number
  night_fires: number
  advice: string
}

export function NoiseTopPage() {
  const { t } = useTranslation()
  const query = useQuery({ queryKey: ['noise'], queryFn: () => get<{ items: NoiseItem[] }>('/noise/top') })

  const columns: ColumnDef<NoiseItem, unknown>[] = [
    { header: t('opsalert:noise.colRule'), accessorKey: 'name' },
    { header: t('opsalert:noise.colFires'), accessorKey: 'fires_7d', cell: ({ row }) => <span className="tabular-nums">{row.original.fires_7d}</span> },
    {
      header: t('opsalert:noise.colFpRate'),
      accessorKey: 'fp_rate',
      cell: ({ row }) => (
        <span className={`tabular-nums ${row.original.fp_rate >= 50 ? 'text-danger' : ''}`}>
          {row.original.fp_rate.toFixed(0)}%
        </span>
      ),
    },
    { header: t('opsalert:noise.colNight'), accessorKey: 'night_fires', cell: ({ row }) => <span className="tabular-nums">{row.original.night_fires}</span> },
    { header: t('opsalert:noise.colAdvice'), accessorKey: 'advice', cell: ({ row }) => <span className="text-xs text-muted-foreground">{row.original.advice}</span> },
  ]

  return (
    <div className="flex flex-col gap-3">
      <p className="text-xs text-muted-foreground">
        {t('opsalert:noise.hint')}
      </p>
      <AsyncBoundary
        state={fromQuery(query, (d) => d.items.length === 0, makeLoadError(t))}
        pending={<TableSkeleton columns={[30, 12, 12, 12, 34]} rows={5} />}
        empty={
          <EmptyState
            title={t('opsalert:noise.emptyTitle')}
            reason={t('opsalert:noise.emptyReason')}
            action={null}
          />
        }
        errorTitle={t('opsalert:noise.loadError')}
        retryLabel={t('action.retry')}
        onRetry={() => query.refetch()}
      >
        {(data) => <DataTable columns={columns} data={data.items} rowKey={(r) => String(r.rule_id)} />}
      </AsyncBoundary>
    </div>
  )
}
