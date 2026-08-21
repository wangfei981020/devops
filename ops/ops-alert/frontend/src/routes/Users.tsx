import { useTranslation } from '@ops/i18n'
import {
  AsyncBoundary,
  Badge,
  Button,
  DataTable,
  Dialog,
  EmptyState,
  TableSkeleton,
  fromQuery,
} from '@ops/ui'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import { WriteButton } from '../components/WriteButton.js'
import { del, get, makeLoadError, post, put } from '../lib/api.js'

/**
 * 用户与角色。
 *
 * ⚠️ 角色下拉**从接口取**，前端不写死。CMDB 那边写死过：界面上给出两个
 * 数据库里根本不存在的角色，选了保存报"角色不存在"，而用户明明是从下拉里选的。
 *
 * ⚠️ 「不受限」的角色（管理员）不靠权限码生效，所以它的权限数再小也不代表权限少。
 * 列表里必须把这件事说清楚，否则看着 0 条权限的管理员会被误认为没权限。
 */

interface User {
  id: number
  username: string
  display_name: string
  auth_source: string
  role_code: string
  role_name: string
  unrestricted: boolean
  status: string
}

interface Role {
  code: string
  name: string
  description: string
  is_builtin: boolean
  unrestricted: boolean
  perm_count: number
  user_count: number
}

export function UsersPage() {
  const { t } = useTranslation()
  const qc = useQueryClient()
  const [creating, setCreating] = useState(false)
  const [pwFor, setPwFor] = useState<User | null>(null)
  const [removing, setRemoving] = useState<User | null>(null)
  const [err, setErr] = useState('')

  const users = useQuery({ queryKey: ['users'], queryFn: () => get<{ items: User[] }>('/users') })
  const roles = useQuery({ queryKey: ['roles'], queryFn: () => get<{ items: Role[] }>('/roles') })
  const refresh = () => qc.invalidateQueries({ queryKey: ['users'] })

  // 后端的拒绝理由要原样显示：「这是最后一个管理员」这种话，
  // 换成泛泛的"操作失败"就等于让人反复试
  const onErr = (e: Error & { detail?: string }) => setErr(e.detail || e.message)

  const changeRole = useMutation({
    mutationFn: (v: { id: number; role: string }) => put(`/users/${v.id}/role`, { role_code: v.role }),
    onSuccess: () => {
      setErr('')
      refresh()
    },
    onError: onErr,
  })
  const changeStatus = useMutation({
    mutationFn: (v: { id: number; status: string }) => put(`/users/${v.id}/status`, { status: v.status }),
    onSuccess: () => {
      setErr('')
      refresh()
    },
    onError: onErr,
  })
  const remove = useMutation({
    mutationFn: (u: User) => del(`/users/${u.id}`),
    onSuccess: () => {
      setRemoving(null)
      setErr('')
      refresh()
    },
    onError: (e: Error & { detail?: string }) => {
      setRemoving(null)
      onErr(e)
    },
  })

  const columns = [
    { header: t('opsalert:users.colName'), accessorKey: 'username' },
    { header: t('opsalert:users.colDisplay'), accessorKey: 'display_name' },
    {
      header: t('opsalert:users.colSource'),
      id: 'src',
      cell: ({ row }: { row: { original: User } }) => (
        <span className="font-mono text-xs text-muted-foreground">{row.original.auth_source}</span>
      ),
    },
    {
      header: t('opsalert:users.colRole'),
      id: 'role',
      cell: ({ row }: { row: { original: User } }) => (
        <div className="flex items-center gap-2">
          <select
            className="rounded-md border border-border bg-background px-2 py-1 text-xs"
            value={row.original.role_code}
            onChange={(e) => changeRole.mutate({ id: row.original.id, role: e.target.value })}
          >
            {(roles.data?.items ?? []).map((r) => (
              <option key={r.code} value={r.code}>
                {r.name}
              </option>
            ))}
          </select>
          {row.original.unrestricted ? <Badge tone="warn">{t('opsalert:users.admin')}</Badge> : null}
        </div>
      ),
    },
    {
      header: t('opsalert:users.colStatus'),
      id: 'status',
      cell: ({ row }: { row: { original: User } }) => (
        <Badge tone={row.original.status === 'active' ? 'ok' : 'mute'}>
          {t(`opsalert:users.status.${row.original.status}`, row.original.status)}
        </Badge>
      ),
    },
    {
      header: t('opsalert:rules.colOps'),
      id: 'ops',
      cell: ({ row }: { row: { original: User } }) => (
        <div className="flex items-center gap-1">
          <WriteButton
            perm="alert:manage_users"
            size="sm"
            variant="ghost"
            onClick={() =>
              changeStatus.mutate({
                id: row.original.id,
                status: row.original.status === 'active' ? 'disabled' : 'active',
              })
            }
          >
            {row.original.status === 'active' ? t('opsalert:users.disable') : t('opsalert:users.enable')}
          </WriteButton>
          <WriteButton
            perm="alert:manage_users"
            size="sm"
            variant="ghost"
            blockedReason={
              row.original.auth_source !== 'local' ? t('opsalert:users.notLocal') : undefined
            }
            onClick={() => setPwFor(row.original)}
          >
            {t('opsalert:users.resetPw')}
          </WriteButton>
          <WriteButton
            perm="alert:manage_users"
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

  return (
    <div className="flex flex-col gap-3">
      {err ? (
        <div className="rounded-md border border-danger/40 bg-danger/10 px-3 py-2 text-sm text-danger">
          {err}
        </div>
      ) : null}

      <div className="flex items-center gap-2">
        <span className="text-xs text-muted-foreground">{t('opsalert:users.hint')}</span>
        <WriteButton
          perm="alert:manage_users"
          size="sm"
          className="ml-auto"
          onClick={() => setCreating(true)}
        >
          {t('opsalert:users.create')}
        </WriteButton>
      </div>

      <AsyncBoundary
        state={fromQuery(users, (d) => d.items.length === 0, makeLoadError(t))}
        pending={<TableSkeleton columns={[18, 18, 12, 22, 12, 18]} rows={4} />}
        empty={
          <EmptyState
            title={t('opsalert:users.emptyTitle')}
            reason={t('opsalert:users.emptyReason')}
            action={{ label: t('opsalert:users.create'), onClick: () => setCreating(true) }}
          />
        }
        errorTitle={t('opsalert:users.loadError')}
        retryLabel={t('action.retry')}
        onRetry={() => users.refetch()}
      >
        {(d) => <DataTable columns={columns} data={d.items} rowKey={(r) => String(r.id)} />}
      </AsyncBoundary>

      <RolesCard roles={roles.data?.items ?? []} />

      {creating && (
        <UserFormDialog
          roles={roles.data?.items ?? []}
          onClose={() => setCreating(false)}
          onSaved={refresh}
          onError={onErr}
        />
      )}
      {pwFor && (
        <PasswordDialog user={pwFor} onClose={() => setPwFor(null)} onError={onErr} />
      )}
      <Dialog
        open={removing !== null}
        onClose={() => setRemoving(null)}
        title={t('opsalert:users.deleteTitle')}
        description={t('opsalert:users.deleteDesc', { name: removing?.username ?? '' })}
        closeLabel={t('action.cancel')}
        footer={
          <>
            <Button variant="ghost" onClick={() => setRemoving(null)}>
              {t('action.cancel')}
            </Button>
            <WriteButton
              perm="alert:manage_users"
              variant="danger"
              loading={remove.isPending}
              onClick={() => removing && remove.mutate(removing)}
            >
              {t('opsalert:rules.delete')}
            </WriteButton>
          </>
        }
      >
        <p className="text-sm text-muted-foreground">{t('opsalert:users.deleteHint')}</p>
      </Dialog>
    </div>
  )
}

/** 角色一览。只读——角色是内置的，改权限要改代码里的映射表和迁移。 */
function RolesCard({ roles }: { roles: Role[] }) {
  const { t } = useTranslation()
  return (
    <section className="rounded-lg border border-border bg-card p-4">
      <h3 className="text-sm font-semibold">{t('opsalert:users.rolesTitle')}</h3>
      <p className="mt-1 text-xs text-muted-foreground">{t('opsalert:users.rolesHint')}</p>
      <div className="mt-3 grid gap-2 md:grid-cols-2 xl:grid-cols-4">
        {roles.map((r) => (
          <div key={r.code} className="rounded-md border border-border p-3">
            <div className="flex items-center gap-2">
              <span className="text-sm font-medium">{r.name}</span>
              {r.unrestricted ? <Badge tone="warn">{t('opsalert:users.admin')}</Badge> : null}
            </div>
            <div className="mt-1 font-mono text-xs text-muted-foreground">{r.code}</div>
            <p className="mt-1.5 text-xs leading-relaxed text-muted-foreground">{r.description}</p>
            <div className="mt-2 text-xs text-muted-foreground">
              {/* ⚠️ 不受限的角色不靠权限码生效，显示"0 条权限"会让人以为它没权限 */}
              {r.unrestricted
                ? t('opsalert:users.allPerms')
                : t('opsalert:users.permCount', { count: r.perm_count })}
              {' · '}
              {t('opsalert:users.userCount', { count: r.user_count })}
            </div>
          </div>
        ))}
      </div>
    </section>
  )
}

function UserFormDialog({
  roles,
  onClose,
  onSaved,
  onError,
}: {
  roles: Role[]
  onClose: () => void
  onSaved: () => void
  onError: (e: Error & { detail?: string }) => void
}) {
  const { t } = useTranslation()
  const [username, setUsername] = useState('')
  const [display, setDisplay] = useState('')
  const [password, setPassword] = useState('')
  // 默认给最小权限的角色，而不是列表第一个 —— 第一个恰好是管理员时，
  // 手一快就建出一个管理员，而界面上看不出刚刚发生了什么
  const [role, setRole] = useState(roles.find((r) => !r.unrestricted)?.code ?? '')

  const create = useMutation({
    mutationFn: () =>
      post('/users', { username, display_name: display, password, role_code: role }),
    onSuccess: () => {
      onSaved()
      onClose()
    },
    onError: (e: Error & { detail?: string }) => {
      onError(e)
      onClose()
    },
  })

  return (
    <Dialog
      open
      onClose={onClose}
      title={t('opsalert:users.create')}
      description={t('opsalert:users.createDesc')}
      closeLabel={t('action.close')}
      footer={
        <>
          <Button variant="ghost" onClick={onClose}>
            {t('action.cancel')}
          </Button>
          <WriteButton
            perm="alert:manage_users"
            loading={create.isPending}
            blockedReason={
              !username || password.length < 8 || !role
                ? t('opsalert:users.needFields')
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
        <Labeled label={t('opsalert:users.colName')}>
          <input
            className="w-full rounded-md border border-border bg-background px-2 py-1.5 text-sm"
            value={username}
            onChange={(e) => setUsername(e.target.value)}
          />
        </Labeled>
        <Labeled label={t('opsalert:users.colDisplay')}>
          <input
            className="w-full rounded-md border border-border bg-background px-2 py-1.5 text-sm"
            value={display}
            onChange={(e) => setDisplay(e.target.value)}
          />
        </Labeled>
        <Labeled label={t('opsalert:users.password')} help={t('opsalert:users.pwRule')}>
          <input
            type="password"
            className="w-full rounded-md border border-border bg-background px-2 py-1.5 text-sm"
            value={password}
            onChange={(e) => setPassword(e.target.value)}
          />
        </Labeled>
        <Labeled label={t('opsalert:users.colRole')}>
          <select
            className="w-full rounded-md border border-border bg-background px-2 py-1.5 text-sm"
            value={role}
            onChange={(e) => setRole(e.target.value)}
          >
            {roles.map((r) => (
              <option key={r.code} value={r.code}>
                {r.name}
              </option>
            ))}
          </select>
        </Labeled>
      </div>
    </Dialog>
  )
}

function PasswordDialog({
  user,
  onClose,
  onError,
}: {
  user: User
  onClose: () => void
  onError: (e: Error & { detail?: string }) => void
}) {
  const { t } = useTranslation()
  const [pw, setPw] = useState('')
  const save = useMutation({
    mutationFn: () => put(`/users/${user.id}/password`, { password: pw }),
    onSuccess: onClose,
    onError: (e: Error & { detail?: string }) => {
      onError(e)
      onClose()
    },
  })
  return (
    <Dialog
      open
      onClose={onClose}
      title={t('opsalert:users.resetPwTitle', { name: user.username })}
      description={t('opsalert:users.resetPwDesc')}
      closeLabel={t('action.close')}
      footer={
        <>
          <Button variant="ghost" onClick={onClose}>
            {t('action.cancel')}
          </Button>
          <WriteButton
            perm="alert:manage_users"
            loading={save.isPending}
            blockedReason={pw.length < 8 ? t('opsalert:users.pwRule') : undefined}
            onClick={() => save.mutate()}
          >
            {t('action.save')}
          </WriteButton>
        </>
      }
    >
      <Labeled label={t('opsalert:users.password')} help={t('opsalert:users.pwRule')}>
        <input
          type="password"
          className="w-full rounded-md border border-border bg-background px-2 py-1.5 text-sm"
          value={pw}
          onChange={(e) => setPw(e.target.value)}
        />
      </Labeled>
    </Dialog>
  )
}

function Labeled({
  label,
  help,
  children,
}: {
  label: string
  help?: string
  children: React.ReactNode
}) {
  return (
    <label className="flex flex-col gap-1">
      <span className="text-xs font-medium">{label}</span>
      {children}
      {help ? <span className="text-xs text-muted-foreground">{help}</span> : null}
    </label>
  )
}
