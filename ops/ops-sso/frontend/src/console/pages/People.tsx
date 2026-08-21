import { useTranslation } from '@ops/i18n'
import { Pwd } from '../../shared/Pwd.js'
import { AsyncBoundary, EmptyState, Skeleton, fromQuery } from '@ops/ui'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Users } from 'lucide-react'
import { useMemo, useState } from 'react'
import { ApiError, api } from '../../api/client.js'
import type { ListOf } from '../../api/types.js'
import { makeToLoadError } from '../../shared/errorText.js'

interface PersonRow {
  id: number
  username: string
  display_name: string
  email: string
  source: string
  status: string
  is_break_glass: boolean
  sessions: number
  groups: string[]
  last_login_at?: string
  role_code: string
}
interface GroupRow {
  id: number
  name: string
  members: number
  rules: number
}

type Tab = 'people' | 'groups'

/**
 * 人员与用户组。
 *
 * 这一页是**复核用的**，不是建号用的：账号来自身份源同步或本地建号，
 * 而复核时要回答的是「这个人是谁、在哪些组里、还有几个会话活着、
 * 他上次登录是什么时候」——最后一项决定了哪些账号该清理。
 */
export function PeoplePage() {
  const { t } = useTranslation()
  const [tab, setTab] = useState<Tab>('people')

  return (
    <>
      <h1 className="text-xl font-semibold tracking-tight">{t('sso:console.identities')}</h1>
      <p className="mt-1 mb-4 max-w-[80ch] text-sm text-muted-foreground">{t('sso:people.intro')}</p>

      <div className="mb-3 inline-flex rounded-[var(--radius)] border border-border p-0.5">
        {(['people', 'groups'] as Tab[]).map((k) => (
          <button
            key={k}
            type="button"
            onClick={() => setTab(k)}
            aria-pressed={tab === k}
            className={[
              'cursor-pointer rounded-[calc(var(--radius)-2px)] px-3 py-1.5 text-[13px]',
              tab === k ? 'bg-brand-bg font-medium text-brand' : 'text-muted-foreground',
            ].join(' ')}
          >
            {t(`sso:people.tab.${k}`)}
          </button>
        ))}
      </div>

      {tab === 'people' ? <PeopleTable /> : <GroupsTable />}
    </>
  )
}

function PeopleTable() {
  const { t } = useTranslation()
  const qc = useQueryClient()
  const toLoadError = useMemo(() => makeToLoadError(t), [t])
  const [adding, setAdding] = useState(false)
  const [err, setErr] = useState<string | null>(null)
  const q = useQuery({ queryKey: ['users'], queryFn: () => api.get<ListOf<PersonRow>>('/users') })

  const refresh = () => {
    void qc.invalidateQueries({ queryKey: ['users'] })
    void qc.invalidateQueries({ queryKey: ['sessions'] })
    void qc.invalidateQueries({ queryKey: ['overview'] })
  }
  const onErr = (e: unknown) =>
    setErr(
      e instanceof ApiError
        ? t(`errors.${e.code}`, { ...e.params, defaultValue: e.code })
        : t('error.unreachable'),
    )

  const setStatus = useMutation({
    mutationFn: (v: { id: number; disabled: boolean }) =>
      api.put(`/users/${v.id}/status`, { disabled: v.disabled }),
    onSuccess: () => {
      setErr(null)
      refresh()
    },
    onError: onErr,
  })
  const setRole = useMutation({
    mutationFn: (v: { id: number; admin: boolean }) =>
      api.put(`/users/${v.id}/role`, { role_code: v.admin ? 'admin' : 'member' }),
    onSuccess: () => {
      setErr(null)
      refresh()
    },
    onError: onErr,
  })
  const state = fromQuery<ListOf<PersonRow>>(q, (d) => d.items.length === 0, toLoadError)

  return (
    <>
      {adding ? (
        <div className="mb-3">
          <NewUserForm onDone={() => { setAdding(false); refresh() }} />
        </div>
      ) : (
        <button
          type="button"
          onClick={() => setAdding(true)}
          className="mb-3 cursor-pointer rounded-[var(--radius)] bg-brand px-3 py-1.5 text-[13px] font-medium text-brand-foreground"
        >
          {t('sso:people.addBtn')}
        </button>
      )}

      {err ? (
        <p className="mb-3 rounded-[var(--radius)] border border-destructive bg-danger-bg px-3 py-2 text-[12px] text-danger">
          {err}
        </p>
      ) : null}

    <div className="overflow-hidden rounded-[var(--radius-md)] border border-border bg-card">
      <AsyncBoundary
        state={state}
        errorTitle={t('sso:people.errorTitle')}
        retryLabel={t('retry')}
        onRetry={() => void q.refetch()}
        pending={
          <div className="space-y-2 p-4">
            {[0, 1, 2].map((i) => (
              <Skeleton key={i} className="h-10 w-full" />
            ))}
          </div>
        }
        empty={
          <EmptyState
            icon={<Users />}
            title={t('sso:people.empty.title')}
            reason={t('sso:people.empty.reason')}
            action={null}
          />
        }
      >
        {(data) => (
          <div className="overflow-x-auto">
            <table className="w-full min-w-[860px] text-[13px]">
              <thead>
                <tr className="bg-muted text-xs text-muted-foreground">
                  <th className="px-3.5 py-2.5 text-left font-medium">{t('sso:people.colPerson')}</th>
                  <th className="px-3.5 py-2.5 text-left font-medium">{t('sso:people.colGroups')}</th>
                  <th className="px-3.5 py-2.5 text-left font-medium">{t('sso:people.colSource')}</th>
                  <th className="px-3.5 py-2.5 text-left font-medium">
                    {t('sso:people.colLastLogin')}
                  </th>
                  <th className="px-3.5 py-2.5 text-left font-medium">
                    {t('sso:people.colSessions')}
                  </th>
                  <th className="px-3.5 py-2.5" />
                </tr>
              </thead>
              <tbody>
                {data.items.map((u) => (
                  <tr key={u.id} className="border-t border-border hover:bg-muted">
                    <td className="px-3.5 py-2.5">
                      <b className="font-medium">{u.display_name}</b>
                      <span className="ml-2 font-mono text-[11px] text-muted-foreground">
                        {u.username}
                      </span>
                      {u.status !== 'active' ? (
                        <span className="ml-2 rounded-[var(--radius-sm)] bg-muted px-1.5 py-0.5 text-[10px] text-muted-foreground">
                          {t(`sso:people.status.${u.status}`, { defaultValue: u.status })}
                        </span>
                      ) : null}
                      {u.is_break_glass ? (
                        // 应急账号必须一眼可辨：它绕开的正是这套系统本身
                        <span className="ml-2 rounded-[var(--radius-sm)] bg-warning-bg px-1.5 py-0.5 text-[10px] text-warning">
                          {t('sso:people.breakGlass')}
                        </span>
                      ) : null}
                    </td>
                    <td className="px-3.5 py-2.5">
                      {u.groups.length === 0 ? (
                        <span className="text-muted-foreground">—</span>
                      ) : (
                        <span className="flex flex-wrap gap-1">
                          {u.groups.map((g) => (
                            <span
                              key={g}
                              className="rounded-[var(--radius-sm)] bg-secondary px-1.5 py-0.5 text-[11px]"
                            >
                              {g}
                            </span>
                          ))}
                        </span>
                      )}
                    </td>
                    <td className="px-3.5 py-2.5 text-muted-foreground">
                      {t(`sso:people.source.${u.source}`, { defaultValue: u.source })}
                    </td>
                    <td className="whitespace-nowrap px-3.5 py-2.5">
                      {u.last_login_at ? (
                        <span className="text-muted-foreground">{fmt(u.last_login_at)}</span>
                      ) : (
                        // 「从没登录过」和「最近没登」是两回事：前者是个该清理的账号
                        <span className="text-warning">{t('sso:people.neverLoggedIn')}</span>
                      )}
                    </td>
                    <td className="px-3.5 py-2.5 tabular-nums">
                      {u.sessions > 0 ? u.sessions : <span className="text-muted-foreground">—</span>}
                    </td>
                    <td className="whitespace-nowrap px-3.5 py-2.5 text-right">
                      <button
                        type="button"
                        disabled={setRole.isPending}
                        onClick={() => setRole.mutate({ id: u.id, admin: u.role_code !== 'admin' })}
                        className="mr-1.5 cursor-pointer rounded-[var(--radius)] border border-border px-2 py-1 text-[11px] hover:bg-secondary disabled:opacity-40"
                      >
                        {u.role_code === 'admin' ? t('sso:people.demote') : t('sso:people.promote')}
                      </button>
                      <button
                        type="button"
                        disabled={setStatus.isPending}
                        onClick={() =>
                          setStatus.mutate({ id: u.id, disabled: u.status === 'active' })
                        }
                        className="cursor-pointer rounded-[var(--radius)] border border-border px-2 py-1 text-[11px] hover:bg-secondary disabled:opacity-40"
                      >
                        {u.status === 'active' ? t('sso:people.disable') : t('sso:people.enable')}
                      </button>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </AsyncBoundary>
    </div>
    </>
  )
}

/**
 * 建一个本地账号。
 *
 * 只建本地账号：身份源来的用户由登录时自动创建，在这里手工建同名的
 * 会撞唯一索引，而那个报错看起来像"用户名被占用"，让人以为是重名。
 */
function NewUserForm({ onDone }: { onDone: () => void }) {
  const { t } = useTranslation()
  const [username, setUsername] = useState('')
  const [display, setDisplay] = useState('')
  const [password, setPassword] = useState('')
  const [admin, setAdmin] = useState(false)
  const [err, setErr] = useState<string | null>(null)

  const m = useMutation({
    mutationFn: () =>
      api.post('/users', {
        username: username.trim(),
        display_name: display.trim(),
        password,
        role_code: admin ? 'admin' : 'member',
      }),
    onSuccess: () => {
      setErr(null)
      onDone()
    },
    onError: (e) =>
      setErr(
        e instanceof ApiError
          ? t(`errors.${e.code}`, { ...e.params, defaultValue: e.code })
          : t('error.unreachable'),
      ),
  })

  return (
    <div className="rounded-[var(--radius-md)] border border-border bg-card p-4">
      <h2 className="text-sm font-semibold">{t('sso:people.newTitle')}</h2>
      <div className="mt-3 grid gap-3 sm:grid-cols-3">
        <label className="block text-[12px]">
          <span className="mb-1 block text-muted-foreground">{t('sso:people.fUsername')}</span>
          <input
            value={username}
            onChange={(e) => setUsername(e.target.value.replace(/\s/g, ''))}
            className="w-full rounded-[var(--radius)] border border-border bg-background px-2.5 py-1.5 text-[13px]"
          />
        </label>
        <label className="block text-[12px]">
          <span className="mb-1 block text-muted-foreground">{t('sso:people.fDisplay')}</span>
          <input
            value={display}
            onChange={(e) => setDisplay(e.target.value)}
            className="w-full rounded-[var(--radius)] border border-border bg-background px-2.5 py-1.5 text-[13px]"
          />
        </label>
        <label className="block text-[12px]">
          <span className="mb-1 block text-muted-foreground">{t('sso:people.fPassword')}</span>
          <Pwd
            value={password}
            onChange={(e) => setPassword(e.target.value)}
            className="border-border"
          />
        </label>
      </div>
      <label className="mt-3 flex cursor-pointer items-center gap-2 text-[12px]">
        <input type="checkbox" checked={admin} onChange={(e) => setAdmin(e.target.checked)} />
        {t('sso:people.fAdmin')}
      </label>
      {err ? (
        <p className="mt-3 rounded-[var(--radius)] bg-danger-bg px-3 py-2 text-[12px] text-danger">
          {err}
        </p>
      ) : null}
      <div className="mt-3 flex items-center gap-2">
        <button
          type="button"
          disabled={username.trim() === '' || password === '' || m.isPending}
          onClick={() => m.mutate()}
          className="cursor-pointer rounded-[var(--radius)] bg-brand px-3 py-1.5 text-[13px] font-medium text-brand-foreground disabled:cursor-default disabled:opacity-50"
        >
          {m.isPending ? t('sso:people.creating') : t('sso:people.create')}
        </button>
        <button
          type="button"
          onClick={onDone}
          className="cursor-pointer rounded-[var(--radius)] border border-border px-3 py-1.5 text-[13px] hover:bg-secondary"
        >
          {t('sso:people.cancel')}
        </button>
        {/* 初装口令必须改：建号的人知道它，所以它在被本人改掉之前不该能做任何事 */}
        <span className="text-[11px] text-muted-foreground">{t('sso:people.mustChangeHint')}</span>
      </div>
    </div>
  )
}

function GroupsTable() {
  const { t } = useTranslation()
  const toLoadError = useMemo(() => makeToLoadError(t), [t])
  const q = useQuery({
    queryKey: ['user-groups'],
    queryFn: () => api.get<ListOf<GroupRow>>('/user-groups'),
  })
  const state = fromQuery<ListOf<GroupRow>>(q, (d) => d.items.length === 0, toLoadError)

  return (
    <div className="overflow-hidden rounded-[var(--radius-md)] border border-border bg-card">
      <AsyncBoundary
        state={state}
        errorTitle={t('sso:people.errorTitle')}
        retryLabel={t('retry')}
        onRetry={() => void q.refetch()}
        pending={
          <div className="space-y-2 p-4">
            {[0, 1].map((i) => (
              <Skeleton key={i} className="h-10 w-full" />
            ))}
          </div>
        }
        empty={
          <EmptyState
            icon={<Users />}
            title={t('sso:people.emptyGroups.title')}
            reason={t('sso:people.emptyGroups.reason')}
            action={null}
          />
        }
      >
        {(data) => (
          <table className="w-full text-[13px]">
            <thead>
              <tr className="bg-muted text-xs text-muted-foreground">
                <th className="px-3.5 py-2.5 text-left font-medium">{t('sso:people.colGroup')}</th>
                <th className="px-3.5 py-2.5 text-left font-medium">{t('sso:people.colMembers')}</th>
                <th className="px-3.5 py-2.5 text-left font-medium">{t('sso:people.colRules')}</th>
              </tr>
            </thead>
            <tbody>
              {data.items.map((g) => (
                <tr key={g.id} className="border-t border-border hover:bg-muted">
                  <td className="px-3.5 py-2.5 font-medium">{g.name}</td>
                  <td className="px-3.5 py-2.5 tabular-nums">{g.members}</td>
                  <td className="px-3.5 py-2.5">
                    {g.rules > 0 ? (
                      <span className="tabular-nums">{g.rules}</span>
                    ) : (
                      // 没有任何规则引用的组，配了也不起作用 ——
                      // 而它长得和生效中的组一模一样，必须标出来
                      <span className="rounded-[var(--radius-sm)] bg-warning-bg px-2 py-0.5 text-[11px] text-warning">
                        {t('sso:people.unusedGroup')}
                      </span>
                    )}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </AsyncBoundary>
    </div>
  )
}

function fmt(s: string) {
  const d = new Date(s)
  if (Number.isNaN(d.getTime())) return s
  const p = (n: number) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())} ${p(d.getHours())}:${p(d.getMinutes())}`
}
