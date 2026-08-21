import { toErrorInfo } from '@ops/api'
import { formatRelativeTime, tError, type Locale, useTranslation } from '@ops/i18n'
import { AsyncBoundary, Badge, Banner, Button, Dialog, Field, SecretInput, Select, Skeleton, TextInput, fromQuery, type LoadError } from '@ops/ui'
import { useState } from 'react'
import { RowMenu } from '../../components/RowMenu.js'
import { WriteButton } from '../../components/WriteButton.js'
import { roleOptions, useRoles } from '../../lib/roles.js'
import {
  type User,
  useChangeRole,
  useCreateUser,
  useDeleteUser,
  useKick,
  useResetPassword,
  useUsers,
} from './queries.js'
import { RolePanel } from './RolePanel.js'

const PERM = 'cmdb:manage_users'

export function UsersPage() {
  const { t, i18n } = useTranslation()
  const locale = i18n.language as Locale
  const query = useUsers()
  const rolesQuery = useRoles()
  const roleList = roleOptions(rolesQuery.data)
  const [adding, setAdding] = useState(false)
  const [pwFor, setPwFor] = useState<User | null>(null)
  const [delFor, setDelFor] = useState<User | null>(null)
  const [kickFor, setKickFor] = useState<User | null>(null)

  const toLoadError = (e: unknown): LoadError => {
    const n = toErrorInfo(e)
    return { cause: tError(t, n.messageKey, n.params), detail: n.detail, retryable: n.retryable, kind: n.kind === 'license' ? 'license' : undefined, feature: n.params?.feature as string | undefined }
  }

  return (
    <div className="mx-auto max-w-[980px] p-5">
      <div className="mb-4 flex items-baseline gap-2.5">
        <h1 className="text-sm font-semibold text-foreground">{t('users:title')}</h1>
        <p className="min-w-0 flex-1 text-xs text-muted-foreground">{t('users:hint')}</p>
        <WriteButton perm={PERM} variant="primary" size="sm" onClick={() => setAdding(true)}>
          {t('common:write.add')}
        </WriteButton>
      </div>

      {/* 🔴 放在列表**外面**：它不该被空态/错误态吞掉。
          用户管理页取不到列表时，这条警告反而更需要显示 ——
          「拉不到用户」和「后门口令是公开的」是两件独立的事。 */}
      {query.data?.adminWeakPassword ? (
        <div className="mb-3">
          <Banner tone="bad">
            <span className="font-medium">{t('users:weakAdmin.title')}</span>
            <span className="mt-0.5 block">{t('users:weakAdmin.detail')}</span>
          </Banner>
        </div>
      ) : null}

      <AsyncBoundary
        state={fromQuery<{ items: User[]; adminWeakPassword: boolean }>(
          query,
          (d) => d.items.length === 0,
          toLoadError,
        )}
        errorTitle={t('users:error.title')}
        retryLabel={t('common:action.retry')}
        onRetry={() => void query.refetch()}
        pending={<Skeleton className="h-5 w-[60%]" />}
        empty={null}
      >
        {(d) => (
          <div className="flex flex-col">
            {d.items.map((u) => (
              <UserRow
                key={u.id}
                user={u}
                roles={roleList}
                locale={locale}
                t={t}
                onResetPassword={() => setPwFor(u)}
                onDelete={() => setDelFor(u)}
                onKick={() => setKickFor(u)}
              />
            ))}
          </div>
        )}
      </AsyncBoundary>

      {/* 角色面板放在用户列表**之后**、AsyncBoundary **之外**：
          用户列表加载失败/为空时，角色清单本身还是读得到的，
          把它包进边界里会被一起吞掉 */}
      {rolesQuery.data && rolesQuery.data.length > 0 ? (
        <RolePanel roles={rolesQuery.data} t={t} />
      ) : null}

      {adding ? <AddUserDialog t={t} roles={roleList} onClose={() => setAdding(false)} /> : null}
      {pwFor ? <PasswordDialog t={t} user={pwFor} onClose={() => setPwFor(null)} /> : null}
      {delFor ? <DeleteUserDialog t={t} user={delFor} onClose={() => setDelFor(null)} /> : null}
      {kickFor ? <KickDialog t={t} user={kickFor} onClose={() => setKickFor(null)} /> : null}
    </div>
  )
}

function UserRow({
  user,
  roles,
  locale,
  t,
  onResetPassword,
  onDelete,
  onKick,
}: {
  user: User
  /** 角色下拉的选项，从接口取（见 lib/roles.ts）。写死会和数据库分叉 */
  roles: { value: string; label: string }[]
  locale: Locale
  t: (k: string, o?: Record<string, unknown>) => string
  onResetPassword: () => void
  onDelete: () => void
  onKick: () => void
}) {
  const changeRole = useChangeRole()
  const kick = useKick()

  return (
    <div className="flex items-center gap-3 border-b border-border py-2.5 text-[13px] last:border-b-0">
      <div className="min-w-0 flex-1">
        <div className="flex items-baseline gap-2">
          <span className="truncate font-medium text-foreground">{user.username}</span>
          <span className="truncate text-xs text-muted-foreground">{user.display_name}</span>
          {/* 来源只用于展示。判"是不是管理员"一律看 is_admin，不看这个 */}
          <Badge tone="mute">{user.auth_source}</Badge>
          {user.is_admin ? <Badge tone="info">{t('users:unrestricted')}</Badge> : null}
        </div>
        <div className="mt-0.5 flex items-baseline gap-3 text-xs text-muted-foreground">
          <span>
            {user.last_login_at
              ? t('users:lastLogin', { time: formatRelativeTime(user.last_login_at, locale) })
              : t('users:neverLoggedIn')}
          </span>
          {user.active_sessions > 0 ? (
            <span>{t('users:sessions', { count: user.active_sessions })}</span>
          ) : null}
        </div>
      </div>

      {/* 后端算好的"这一条能不能改"。前端不自己推导 ——
          "最后一个管理员不能降权"这种规则只有后端知道全局状态 */}
      {user.can_change_role ? (
        <Select<string>
          label={t('users:role')}
          value={user.role_code}
          onChange={(v) =>
            changeRole.mutate(
              { id: user.id, role_code: v },
              // 改完权限**必须**踢一次：会话里存着旧的权限快照
              { onSuccess: () => kick.mutate(user.id) },
            )
          }
          // ⚠️ 遗留账号的 role_code 是空串，而空串**不在**选项里 ——
          // 下拉会显示成「—」，看着像"没设角色"这件小事，
          // 实际含义是"不受任何权限码约束"（沿用升级前的全权限）。
          // 和 AI 接入页的遗留不受限令牌是同一个坑，解法也一样：
          // 把当前值补进选项，让人看得见自己正在从什么改成什么。
          options={
            user.role_code === ''
              ? [{ value: '', label: t('users:legacyUnrestricted') }, ...roles]
              : roles
          }
        />
      ) : (
        <span
          className="w-[150px] shrink-0 text-right text-xs text-muted-foreground"
          title={t('users:notEditableHint', { where: user.editable_in })}
        >
          {user.role_name}
        </span>
      )}

      {/* 敏感操作一律进 ⋯ 菜单，与观测端点/注册商/云账号保持一致
          （OPSCMDB-029 定的规范，这一页原来漏了 —— 031 P2-62）。
          「重置密码」和「踢下线」都是能立刻影响别人登录状态的操作，
          放在行上，划过列表时手一抖就点到了。

          ⚠️ can_delete / can_change_password 都由**后端**给
          （「最后一个管理员不能删」这种规则只有后端知道全局状态）。
          为 false 时不渲染该项，而不是渲染一个点不动的项。 */}
      <RowMenu
        perm={PERM}
        items={[
          ...(user.can_change_password
            ? [{ key: 'pw', label: t('users:resetPassword'), onClick: onResetPassword }]
            : []),
          { key: 'kick', label: t('users:kick'), onClick: onKick, danger: true },
          ...(user.can_delete
            ? [{ key: 'delete', label: t('common:write.delete'), onClick: onDelete, danger: true }]
            : []),
        ]}
      />
    </div>
  )
}

function AddUserDialog({
  t,
  roles,
  onClose,
}: {
  t: (k: string, o?: Record<string, unknown>) => string
  roles: { value: string; label: string }[]
  onClose: () => void
}) {
  const [username, setUsername] = useState('')
  const [display, setDisplay] = useState('')
  const [password, setPassword] = useState('')
  const [role, setRole] = useState('cmdb_viewer')
  const create = useCreateUser()

  return (
    <Dialog
      open
      onClose={onClose}
      title={t('users:addTitle')}
      description={t('users:addDesc')}
      closeLabel={t('common:action.close')}
      footer={
        <>
          <Button size="sm" onClick={onClose}>
            {t('common:write.cancel')}
          </Button>
          <Button
            size="sm"
            variant="primary"
            loading={create.isPending}
            disabled={username.trim() === '' || password === ''}
            onClick={() =>
              create.mutate(
                {
                  username: username.trim(),
                  display_name: display.trim() || username.trim(),
                  password,
                  role_code: role,
                },
                { onSuccess: onClose },
              )
            }
          >
            {t('common:write.save')}
          </Button>
        </>
      }
    >
      <div className="flex flex-col gap-3">
        <Field label={t('users:field.username')} required>
          <TextInput value={username} onChange={(e) => setUsername(e.target.value)} autoFocus />
        </Field>
        <Field label={t('users:field.display')}>
          <TextInput value={display} onChange={(e) => setDisplay(e.target.value)} />
        </Field>
        <Field label={t('users:field.password')} hint={t('users:field.passwordHint')} required>
          <SecretInput
            configured={false}
            placeholderConfigured=""
            placeholderEmpty={t('users:field.passwordPlaceholder')}
            showLabel={t('common:action.showSecret')}
            hideLabel={t('common:action.hideSecret')}
            value={password}
            onChange={(e) => setPassword(e.target.value)}
          />
        </Field>
        <Field label={t('users:role')} hint={t('users:field.roleHint')}>
          <Select<string> label={t('users:role')} value={role} onChange={setRole} options={roles} />
        </Field>
        {create.isError ? (
          <p className="text-xs text-danger">{t(toErrorInfo(create.error).messageKey)}</p>
        ) : null}
      </div>
    </Dialog>
  )
}

function PasswordDialog({
  t,
  user,
  onClose,
}: {
  t: (k: string, o?: Record<string, unknown>) => string
  user: User
  onClose: () => void
}) {
  const [password, setPassword] = useState('')
  const reset = useResetPassword()
  const kick = useKick()
  return (
    <Dialog
      open
      onClose={onClose}
      title={t('users:resetTitle', { name: user.username })}
      description={t('users:resetDesc')}
      closeLabel={t('common:action.close')}
      footer={
        <>
          <Button size="sm" onClick={onClose}>
            {t('common:write.cancel')}
          </Button>
          <Button
            size="sm"
            variant="primary"
            loading={reset.isPending}
            disabled={password === ''}
            onClick={() =>
              reset.mutate(
                { id: user.id, password },
                // 改完密码也踢：旧会话仍然有效，等于密码没换
                {
                  onSuccess: () => {
                    kick.mutate(user.id)
                    onClose()
                  },
                },
              )
            }
          >
            {t('common:write.save')}
          </Button>
        </>
      }
    >
      <Field label={t('users:field.newPassword')} hint={t('users:field.passwordHint')} required>
        <SecretInput
          configured={false}
          placeholderConfigured=""
          placeholderEmpty={t('users:field.passwordPlaceholder')}
          showLabel={t('common:action.showSecret')}
          hideLabel={t('common:action.hideSecret')}
          value={password}
          onChange={(e) => setPassword(e.target.value)}
        />
      </Field>
      {reset.isError ? (
        <p className="mt-2 text-xs text-danger">{t(toErrorInfo(reset.error).messageKey)}</p>
      ) : null}
    </Dialog>
  )
}

function DeleteUserDialog({
  t,
  user,
  onClose,
}: {
  t: (k: string, o?: Record<string, unknown>) => string
  user: User
  onClose: () => void
}) {
  const del = useDeleteUser()
  return (
    <Dialog
      open
      onClose={onClose}
      title={t('common:write.deleteConfirm', { name: user.username })}
      description={t('users:deleteDesc')}
      closeLabel={t('common:action.close')}
      footer={
        <>
          <Button size="sm" onClick={onClose}>
            {t('common:write.cancel')}
          </Button>
          <Button
            size="sm"
            variant="danger"
            loading={del.isPending}
            onClick={() => del.mutate(user.id, { onSuccess: onClose })}
          >
            {t('common:write.delete')}
          </Button>
        </>
      }
    >
      <p className="text-[13px] leading-relaxed text-foreground">{t('users:deleteImpact')}</p>
      {del.isError ? (
        <p className="mt-2 text-xs text-danger">{t(toErrorInfo(del.error).messageKey)}</p>
      ) : null}
    </Dialog>
  )
}

/**
 * 踢下线要二次确认。
 *
 * 它**立刻生效**：对方正在填的表单会在下一次请求时 401，填了一半的内容没了。
 * 原来它是行内一个普通按钮，点下去没有任何确认（OPSCMDB-031 P2-62）。
 */
function KickDialog({
  t,
  user,
  onClose,
}: {
  t: (k: string, o?: Record<string, unknown>) => string
  user: User
  onClose: () => void
}) {
  const kick = useKick()
  return (
    <Dialog
      open
      onClose={onClose}
      title={t('users:kickConfirm', { name: user.username })}
      closeLabel={t('common:action.close')}
      footer={
        <>
          <Button size="sm" onClick={onClose}>
            {t('common:write.cancel')}
          </Button>
          <Button
            size="sm"
            variant="danger"
            loading={kick.isPending}
            onClick={() => kick.mutate(user.id, { onSuccess: onClose })}
          >
            {t('users:kick')}
          </Button>
        </>
      }
    >
      <p className="text-[13px] leading-relaxed text-foreground">{t('users:kickImpact')}</p>
      {kick.isError ? (
        <p className="mt-2 text-xs text-danger">{t(toErrorInfo(kick.error).messageKey)}</p>
      ) : null}
    </Dialog>
  )
}
