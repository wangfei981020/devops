import { useTranslation } from '@ops/i18n'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import {
  AsyncBoundary,
  Badge,
  Banner,
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

type Token = {
  id: number
  name: string
  role: string
  enabled: boolean
  last_used_at?: string
  last_used_ip: string
  created_by: string
  created_at: string
}

/**
 * MCP 接入：把只读能力开给 AI。
 *
 * 一个接入方一条令牌，而不是全局一把钥匙——全局令牌泄露后既定位不到是谁泄的，
 * 也没法只吊销一个接入方。
 */
export function McpPage() {
  const { t } = useTranslation()
  const qc = useQueryClient()
  const [creating, setCreating] = useState(false)
  const [issued, setIssued] = useState<string | null>(null)
  const query = useQuery({ queryKey: ['mcp-tokens'], queryFn: () => get<{ items: Token[] }>('/mcp/tokens') })
  const refresh = () => qc.invalidateQueries({ queryKey: ['mcp-tokens'] })
  const revoke = useMutation({ mutationFn: (id: number) => del(`/mcp/tokens/${id}`), onSuccess: refresh })

  const columns: ColumnDef<Token, unknown>[] = [
    { header: t('opsalert:mcp.colConsumer'), accessorKey: 'name' },
    { header: t('opsalert:mcp.colRole'), accessorKey: 'role', cell: ({ row }) => <Badge tone="mute" dot={false}>{row.original.role}</Badge> },
    {
      header: t('opsalert:mcp.colLastUsed'),
      accessorKey: 'last_used_at',
      cell: ({ row }) =>
        row.original.last_used_at ? (
          <span className="tabular-nums text-muted-foreground">
            {new Date(row.original.last_used_at).toLocaleString()}
            {row.original.last_used_ip && <span className="ml-2 font-mono text-xs">{row.original.last_used_ip}</span>}
          </span>
        ) : (
          // 从没用过的令牌是纯风险：它只可能被别人用
          <Badge tone="warn">{t('opsalert:mcp.neverUsed')}</Badge>
        ),
    },
    { header: t('opsalert:mcp.colCreatedBy'), accessorKey: 'created_by' },
    {
      header: t('opsalert:rules.colOps'),
      id: 'ops',
      cell: ({ row }) => (
        <WriteButton perm="alert:manage_mcp" size="sm" variant="ghost" onClick={() => revoke.mutate(row.original.id)}>
          {t('opsalert:mcp.revoke')}
        </WriteButton>
      ),
    },
  ]

  return (
    <div className="flex flex-col gap-3">
      <div className="flex items-start gap-2">
        <p className="max-w-2xl text-xs text-muted-foreground">
          {t('opsalert:mcp.hint')}
        </p>
        <WriteButton perm="alert:manage_mcp" size="sm" className="ml-auto shrink-0" onClick={() => setCreating(true)}>
          {t('opsalert:mcp.create')}
        </WriteButton>
      </div>

      {issued && (
        <Banner tone="warn">
          <div>
            <div className="font-medium">{t('opsalert:mcp.issuedTitle')}</div>
            <code className="mt-1 block font-mono text-xs break-all">{issued}</code>
            <div className="mt-1 text-xs">{t('opsalert:mcp.issuedHint')}</div>
          </div>
        </Banner>
      )}

      <AsyncBoundary
        state={fromQuery(query, (d) => d.items.length === 0, makeLoadError(t))}
        pending={<TableSkeleton columns={[24, 12, 30, 18, 16]} rows={3} />}
        empty={
          <EmptyState
            title={t('opsalert:mcp.emptyTitle')}
            reason={t('opsalert:mcp.emptyReason')}
            action={{ label: t('opsalert:mcp.create'), onClick: () => setCreating(true) }}
          />
        }
        errorTitle={t('opsalert:mcp.loadError')}
        retryLabel={t('action.retry')}
        onRetry={() => query.refetch()}
      >
        {(data) => <DataTable columns={columns} data={data.items} rowKey={(r) => String(r.id)} />}
      </AsyncBoundary>

      <section className="rounded-lg border border-border p-4">
        <h3 className="text-sm font-semibold">{t('opsalert:mcp.howTo')}</h3>
        <pre className="mt-2 overflow-x-auto rounded bg-muted p-3 font-mono text-[11px] leading-relaxed">{`# JSON-RPC over HTTP，端点就是本服务的 /api/v1/mcp
curl -X POST http://<地址>/api/v1/mcp \\
  -H "X-MCP-Token: <令牌>" \\
  -d '{"jsonrpc":"2.0","id":1,"method":"tools/list"}'

# Claude Code / 支持 MCP 的客户端里配置：
{ "mcpServers": { "opsalert": {
    "url": "http://<地址>/api/v1/mcp",
    "headers": { "X-MCP-Token": "<令牌>" } } } }`}</pre>
        <div className="mt-3 text-xs text-muted-foreground">
          {t('opsalert:mcp.toolsHint')}
        </div>
      </section>

      {creating && (
        <CreateTokenDialog
          onClose={() => setCreating(false)}
          onCreated={(tok) => {
            setIssued(tok)
            refresh()
          }}
        />
      )}
    </div>
  )
}

function CreateTokenDialog({
  onClose,
  onCreated,
}: {
  onClose: () => void
  onCreated: (token: string) => void
}) {
  const { t } = useTranslation()
  const [name, setName] = useState('')
  const create = useMutation({
    mutationFn: () => post<{ token: string }>('/mcp/tokens', { name, role: 'viewer' }),
    onSuccess: (res) => {
      onCreated(res.token)
      onClose()
    },
  })

  return (
    <Dialog
      open
      onClose={onClose}
      closeLabel={t('action.close')}
      title={t('opsalert:mcp.formTitle')}
      description={t('opsalert:mcp.formDesc')}
      footer={
        <>
          <Button variant="ghost" onClick={onClose}>
            {t('action.cancel')}
          </Button>
          <WriteButton perm="alert:manage_mcp" loading={create.isPending} blockedReason={!name ? t('opsalert:perm.fillRequired') : undefined} onClick={() => create.mutate()}>
            {t('opsalert:mcp.submit')}
          </WriteButton>
        </>
      }
    >
      <Field label={t('opsalert:mcp.consumerName')} hint={t('opsalert:mcp.consumerHint')} required>
        <TextInput value={name} onChange={(e) => setName(e.target.value)} />
      </Field>
    </Dialog>
  )
}
