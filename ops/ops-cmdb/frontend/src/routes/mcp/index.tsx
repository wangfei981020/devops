import { toErrorInfo } from '@ops/api'
import { formatRelativeTime, tError, type Locale, useTranslation } from '@ops/i18n'
import {
  AsyncBoundary,
  Badge,
  Banner,
  Button,
  Dialog,
  Field,
  fromQuery,
  type LoadError,
  Select,
  Skeleton,
  TextInput,
} from '@ops/ui'
import { useState } from 'react'
import { RowMenu } from '../../components/RowMenu.js'
import { WriteButton } from '../../components/WriteButton.js'
import { roleOptions, useRoles } from '../../lib/roles.js'
import {
  type McpToken,
  useCreateToken,
  useDeleteToken,
  useMcpInfo,
  useMcpTokens,
  useUpdateToken,
} from './queries.js'

const PERM = 'cmdb:manage_mcp'

/**
 * MCP 接入。
 *
 * 让 AI 客户端（Claude Code 等）以**只读**方式连进来查数据。
 *
 * ⚠️ 这一页的核心不是"生成一个 token"，而是"这条接入能看到什么"。
 * 所以角色是必填项，而且和人用的是同一套权限码——
 * 不给 AI 开第二条通道，是这个功能唯一不能让步的地方。
 */
export function McpPage() {
  const { t, i18n } = useTranslation()
  const locale = i18n.language as Locale
  const info = useMcpInfo()
  const tokens = useMcpTokens()
  // AI 和人走同一套权限码，所以角色清单也走同一个接口
  const roles = roleOptions(useRoles().data)
  const [adding, setAdding] = useState(false)
  const [created, setCreated] = useState<{ name: string; token: string } | null>(null)
  const [delFor, setDelFor] = useState<McpToken | null>(null)
  const [roleFor, setRoleFor] = useState<{ token: McpToken; roleCode: string } | null>(null)
  const [expiryFor, setExpiryFor] = useState<McpToken | null>(null)

  const toLoadError = (e: unknown): LoadError => {
    const n = toErrorInfo(e)
    return { cause: tError(t, n.messageKey, n.params), detail: n.detail, retryable: n.retryable, kind: n.kind === 'license' ? 'license' : undefined, feature: n.params?.feature as string | undefined }
  }

  return (
    <div className="mx-auto max-w-[980px] p-5">
      <div className="mb-4 flex items-baseline gap-2.5">
        <h1 className="text-sm font-semibold text-foreground">{t('mcp:title')}</h1>
        <p className="min-w-0 flex-1 text-xs text-muted-foreground">{t('mcp:hint')}</p>
        <WriteButton perm={PERM} variant="primary" size="sm" onClick={() => setAdding(true)}>
          {t('mcp:action.newToken')}
        </WriteButton>
      </div>

      <AsyncBoundary
        state={fromQuery(info, () => false, toLoadError)}
        errorTitle={t('mcp:error.info')}
        retryLabel={t('common:action.retry')}
        onRetry={() => void info.refetch()}
        pending={<Skeleton className="h-5 w-[50%]" />}
        empty={null}
      >
        {(d) => (
          <div className="mb-5 flex flex-col gap-3">
            <div className="grid grid-cols-[auto_1fr] gap-x-4 gap-y-1.5 rounded-md border border-border p-4 text-xs">
              <span className="text-muted-foreground">{t('mcp:field.endpoint')}</span>
              <code className="font-mono text-foreground">{d.endpoint}</code>
              <span className="text-muted-foreground">{t('mcp:field.transport')}</span>
              <code className="font-mono text-foreground">{d.transport}</code>
              <span className="text-muted-foreground">{t('mcp:field.tools')}</span>
              <span className="text-foreground">
                {/* 两个数都给：只说"可用 10 个"客户不知道自己少了什么，
                    只说"共 78 个"又看不出现在能用多少 */}
                {t('mcp:toolCount', { avail: d.tools_licensed, total: d.tools_total })}
              </span>
            </div>

            {!d.full_licensed ? (
              <Banner tone="info">
                <span className="font-medium">{t('mcp:tier.title')}</span>
                <span className="mt-0.5 block">{t('mcp:tier.body')}</span>
              </Banner>
            ) : null}
          </div>
        )}
      </AsyncBoundary>

      <AsyncBoundary
        state={fromQuery(tokens, (d) => d.items.length === 0, toLoadError)}
        errorTitle={t('mcp:error.tokens')}
        retryLabel={t('common:action.retry')}
        onRetry={() => void tokens.refetch()}
        pending={<Skeleton className="h-5 w-[60%]" />}
        empty={
          <div className="rounded-md border border-dashed border-border px-4 py-8 text-center">
            <div className="text-xs font-medium text-foreground">{t('mcp:empty.title')}</div>
            <div className="mt-1 text-xs text-muted-foreground">{t('mcp:empty.reason')}</div>
          </div>
        }
      >
        {(d) => (
          <div className="flex flex-col">
            {d.items.map((tk) => (
              <TokenRow
                key={tk.id}
                token={tk}
                roles={roles}
                locale={locale}
                t={t}
                onDelete={() => setDelFor(tk)}
                onChangeRole={(roleCode) => setRoleFor({ token: tk, roleCode })}
                onEditExpiry={() => setExpiryFor(tk)}
              />
            ))}
          </div>
        )}
      </AsyncBoundary>

      {adding ? (
        <NewTokenDialog
          t={t}
          roles={roles}
          onClose={() => setAdding(false)}
          onCreated={(name, token) => {
            setAdding(false)
            setCreated({ name, token })
          }}
        />
      ) : null}
      {created ? <ShowTokenDialog t={t} {...created} onClose={() => setCreated(null)} /> : null}
      {delFor ? <DeleteDialog t={t} token={delFor} onClose={() => setDelFor(null)} /> : null}
      {roleFor ? (
        <ChangeRoleDialog
          t={t}
          token={roleFor.token}
          roleCode={roleFor.roleCode}
          roleLabel={roles.find((r) => r.value === roleFor.roleCode)?.label ?? roleFor.roleCode}
          onClose={() => setRoleFor(null)}
        />
      ) : null}
      {expiryFor ? (
        <ExpiryDialog t={t} token={expiryFor} locale={locale} onClose={() => setExpiryFor(null)} />
      ) : null}
    </div>
  )
}

function TokenRow({
  token,
  roles,
  locale,
  t,
  onDelete,
  onChangeRole,
  onEditExpiry,
}: {
  token: McpToken
  roles: { value: string; label: string }[]
  locale: Locale
  t: (k: string, p?: Record<string, unknown>) => string
  onDelete: () => void
  onChangeRole: (roleCode: string) => void
  onEditExpiry: () => void
}) {
  const update = useUpdateToken()
  return (
    <div className="flex items-center gap-3 border-b border-border py-2.5 last:border-0">
      <div className="min-w-0 flex-1">
        <div className="flex items-center gap-2">
          <span className="truncate text-[13px] font-medium text-foreground">{token.name}</span>
          <code className="font-mono text-[11px] text-muted-foreground">{token.hint}…</code>
          {!token.enabled ? <Badge tone="mute">{t('mcp:state.disabled')}</Badge> : null}
          {/* 不受限的令牌要显眼：它绕开了整套权限体系，而这一点在别处看不出来 */}
          {token.unrestricted ? <Badge tone="warn">{t('mcp:state.unrestricted')}</Badge> : null}
          {/* 🔴 过期的令牌调用**会被后端拒**。它必须和「停用」一样显眼，
              否则界面上看着正常、接入方那头全是 401，两边对不上 */}
          {token.expired ? <Badge tone="bad">{t('mcp:state.expired')}</Badge> : null}
          {token.expiring_soon ? <Badge tone="warn">{t('mcp:state.expiringSoon')}</Badge> : null}
        </div>
        <div className="mt-0.5 flex items-center gap-3 text-[11px] text-muted-foreground">
          <span>
            {t('mcp:field.role')}:{' '}
            {token.unrestricted
              ? t('mcp:role.unrestricted')
              : (roles.find((r) => r.value === token.role_code)?.label ?? token.role_code)}
          </span>
          {/* ⚠️ 这一行才是接入方真正关心的数字：它按**这条令牌的角色**算，
              与顶部那个「授权包含 N 个」不是一回事。本会话就是照顶部那个数
              规划接入、实际只拿到 67 个（OPSCMDB-008）。 */}
          {token.tools != null ? (
            <span title={t('mcp:toolsForTokenHint')}>
              {t('mcp:toolsForToken', { n: token.tools })}
            </span>
          ) : null}
          <span>
            {t('mcp:field.lastUsed')}: {/* 从没用过 ≠ 很久没用。前者是没接上，后者才能回收 */}
            {token.last_used_at ? (
              <span title={token.last_used_at}>
                {formatRelativeTime(token.last_used_at, locale)}
                {token.last_used_ip ? ` · ${token.last_used_ip}` : ''}
              </span>
            ) : (
              // 从没用过 ≠ 很久没用：前者是没接上（配错了），
              // 后者才谈得上回收。压成同一种灰字就分不出来了
              <span className="italic">{t('mcp:neverUsed')}</span>
            )}
          </span>
          {/*
            🔴 「谁建的、什么时候建的」必须显示。
            
            这是一个能**只读访问全库 87 个工具**的令牌（含成本、IAM、凭据位置、暴露面）。
            "谁批准了它、什么时候批的"是审计的第一个问题 ——
            后端一直记着 `created_by` / `created_at`，界面只显示"最后使用"
            （OPSCMDB-031 P1-69，第 17 次「后端有、前端没接」，
            **而这次漏掉的是审计信息本身**）。
          */}
          {token.created_by || token.created_at ? (
            <span title={token.created_at}>
              {t('mcp:field.createdBy', {
                who: token.created_by || t('common:state.unknown'),
                when: token.created_at ? formatRelativeTime(token.created_at, locale) : '—',
              })}
            </span>
          ) : null}
          {/*
            🔴 「永久有效」必须写出来，不能留空白。
            
            这是一个能只读访问全库 87 个工具的凭据，而它默认永不失效。
            空白会被读成"这一项没填"，实际含义是"永远不会失效"
            （OPSCMDB-031 P1-70）。让它是一个**看得见的决定**，
            而不是一个看不见的默认。
          */}
          <button
            type="button"
            onClick={onEditExpiry}
            className={`cursor-pointer underline-offset-2 hover:underline ${
              token.expired ? 'text-danger' : token.expiring_soon ? 'text-warning' : ''
            }`}
          >
            {t('mcp:field.expires')}:{' '}
            {token.expires_at ? (
              <span title={token.expires_at}>{formatRelativeTime(token.expires_at, locale)}</span>
            ) : (
              <span className="italic">{t('mcp:neverExpires')}</span>
            )}
          </button>
        </div>
      </div>

      <Select<string>
        label={t('mcp:field.role')}
        value={token.unrestricted ? '' : token.role_code}
        options={[
          // 遗留的不受限令牌：把当前值也放进选项里，否则下拉显示空白，
          // 看起来像"没设角色"，而它其实是最宽的那一档
          ...(token.unrestricted ? [{ value: '', label: t('mcp:role.unrestricted') }] : []),
          ...roles,
        ]}
        // ⚠️ 换角色 = 改变这条令牌能调用的工具集合，是一次**权限变更**。
        //	原来是行内下拉直接生效，随手一点就改了（OPSCMDB-031 P2-64）。
        //	对照用户与角色页：那里改角色受 can_change_role 约束，
        //	同类操作不能一严一松
        onChange={(v) => v && v !== token.role_code && onChangeRole(v)}
      />
      <WriteButton
        perm={PERM}
        size="sm"
        onClick={() => update.mutate({ id: token.id, enabled: !token.enabled })}
      >
        {token.enabled ? t('mcp:action.disable') : t('mcp:action.enable')}
      </WriteButton>
      <RowMenu
        perm={PERM}
        items={[
          {
            key: 'delete',
            label: t('common:write.delete'),
            onClick: onDelete,
            danger: true,
          },
        ]}
      />
    </div>
  )
}

function NewTokenDialog({
  t,
  roles,
  onClose,
  onCreated,
}: {
  t: (k: string, p?: Record<string, unknown>) => string
  roles: { value: string; label: string }[]
  onClose: () => void
  onCreated: (name: string, token: string) => void
}) {
  const [name, setName] = useState('')
  const [role, setRole] = useState('cmdb_viewer')
  // ⚠️ 默认空 = 永久。默认值本身没问题（强制有效期会逼所有人定期换令牌，
  // 漏做一次就是 AI 全线 401），但它必须是一个**看得见的选择** ——
  // 所以下面的提示会在留空时明确写出"这条令牌永远不会失效"
  const [expires, setExpires] = useState('')
  const create = useCreateToken()

  return (
    <Dialog
      open
      onClose={onClose}
      title={t('mcp:dialog.newTitle')}
      description={t('mcp:dialog.newDesc')}
      closeLabel={t('common:action.close')}
      footer={
        <>
          <WriteButton perm={PERM} size="sm" onClick={onClose}>
            {t('common:action.cancel')}
          </WriteButton>
          <WriteButton
            perm={PERM}
            variant="primary"
            size="sm"
            loading={create.isPending}
            blockedReason={name ? undefined : t('mcp:field.nameHint')}
            onClick={() =>
              create.mutate(
                { name, role_code: role, expires_at: expires },
                { onSuccess: (d) => onCreated(name, d.token) },
              )
            }
          >
            {t('common:write.create')}
          </WriteButton>
        </>
      }
    >
      <div className="flex flex-col gap-3">
        <Field label={t('mcp:field.name')} hint={t('mcp:field.nameHint')} required>
          <TextInput
            value={name}
            onChange={(e) => setName(e.target.value)}
            placeholder="Claude Code - 运维组"
            autoFocus
          />
        </Field>

        <Field label={t('mcp:field.expires')} hint={t('mcp:expiry.hint')}>
          <TextInput type="date" value={expires} onChange={(e) => setExpires(e.target.value)} />
          {/* 🔴 留空的后果要当场说出来，不能等建完了才在列表里发现。
              这是一个能只读访问全库 87 个工具的凭据（OPSCMDB-031 P1-70） */}
          {expires === '' ? (
            <p className="mt-1 text-xs text-warning">{t('mcp:expiry.createNeverWarn')}</p>
          ) : null}
        </Field>

        <div className="flex flex-col gap-1.5">
          <Select<string>
            label={t('mcp:field.role')}
            value={role}
            options={roles}
            onChange={setRole}
            className="self-start"
          />
          {/* 这段话是这个弹窗里最重要的一句：多数人会默认"AI 只是查一下"
              而不去想它能查到多少 */}
          <span className="text-xs leading-relaxed text-muted-foreground">
            {t('mcp:field.roleHint')}
          </span>
        </div>

        {create.isError ? (
          <Banner tone="bad">
            <span className="font-medium">
              {tError(t, toErrorInfo(create.error).messageKey, toErrorInfo(create.error).params)}
            </span>
          </Banner>
        ) : null}
      </div>
    </Dialog>
  )
}

/**
 * 只出现一次的令牌。
 *
 * ⚠️ 必须说清"关掉就再也看不到了"，并且**在关闭按钮旁边说**。
 * 说在顶部的话，人复制完就直接点右上角 × 了，
 * 然后来问"我在哪儿再看一次" —— 答案是没有。
 */
function ShowTokenDialog({
  t,
  name,
  token,
  onClose,
}: {
  t: (k: string, p?: Record<string, unknown>) => string
  name: string
  token: string
  onClose: () => void
}) {
  const [copied, setCopied] = useState(false)
  const snippet = `{
  "mcpServers": {
    "cmdb": {
      "type": "http",
      "url": "${window.location.origin}/api/mcp",
      "headers": { "Authorization": "Bearer ${token}" }
    }
  }
}`
  return (
    <Dialog
      open
      onClose={onClose}
      title={t('mcp:dialog.createdTitle', { name })}
      closeLabel={t('common:action.close')}
      width={620}
      footer={
        <>
          {/* 提醒放在按钮旁边，不放顶部：人复制完就直接点关闭了，
              顶部那句话早滚出视线了 */}
          <span className="mr-auto text-[11px] text-muted-foreground">
            {t('mcp:created.closeWarning')}
          </span>
          <WriteButton
            perm={PERM}
            size="sm"
            onClick={() => {
              void navigator.clipboard.writeText(snippet).then(() => setCopied(true))
            }}
          >
            {copied ? t('mcp:action.copied') : t('mcp:action.copy')}
          </WriteButton>
          <WriteButton perm={PERM} variant="primary" size="sm" onClick={onClose}>
            {t('mcp:action.savedIt')}
          </WriteButton>
        </>
      }
    >
      <div className="flex flex-col gap-3">
        <Banner tone="warn">
          <span className="font-medium">{t('mcp:created.onceTitle')}</span>
          <span className="mt-0.5 block">{t('mcp:created.onceBody')}</span>
        </Banner>

        <Field label={t('mcp:field.token')}>
          <TextInput value={token} readOnly className="font-mono" />
        </Field>

        {/* 给能直接粘的配置，而不是让人照着文档拼。
            拼错的部分（endpoint 少了 /api、header 写成 X-Token）
            报错都长得像"连不上"，查起来很费劲 */}
        <Field label={t('mcp:field.snippet')} hint={t('mcp:field.snippetHint')}>
          <pre className="max-h-[220px] overflow-auto rounded-[var(--radius)] border border-border bg-background p-2.5 font-mono text-[11px] leading-relaxed text-foreground">
            {snippet}
          </pre>
        </Field>
      </div>
    </Dialog>
  )
}

function DeleteDialog({
  t,
  token,
  onClose,
}: {
  t: (k: string, p?: Record<string, unknown>) => string
  token: McpToken
  onClose: () => void
}) {
  const del = useDeleteToken()
  return (
    <Dialog
      open
      onClose={onClose}
      title={t('mcp:dialog.deleteTitle')}
      closeLabel={t('common:action.close')}
      footer={
        <>
          <WriteButton perm={PERM} size="sm" onClick={onClose}>
            {t('common:action.cancel')}
          </WriteButton>
          <WriteButton
            perm={PERM}
            variant="danger"
            size="sm"
            loading={del.isPending}
            onClick={() => del.mutate(token.id, { onSuccess: onClose })}
          >
            {t('common:write.delete')}
          </WriteButton>
        </>
      }
    >
      <div className="flex flex-col gap-3">
        <p className="text-[13px] leading-relaxed text-foreground">
          {t('mcp:delete.body', { name: token.name })}
        </p>
        {/* 从没用过的令牌删起来没风险，用过的要提醒。两种情况说同一句话
            等于把"这条正在被人用"这个关键事实藏起来了 */}
        {token.last_used_at ? (
          <Banner tone="warn">
            <span>{t('mcp:delete.inUse', { at: token.last_used_at, ip: token.last_used_ip })}</span>
          </Banner>
        ) : null}
        {del.isError ? (
          <Banner tone="bad">
            <span>{tError(t, toErrorInfo(del.error).messageKey, toErrorInfo(del.error).params)}</span>
          </Banner>
        ) : null}
      </div>
    </Dialog>
  )
}

/**
 * 换角色的二次确认。
 *
 * 换角色 = 改变这条令牌能调用的工具集合，是一次**权限变更**，
 * 而它原来是行内下拉直接生效（OPSCMDB-031 P2-64）。
 *
 * ⚠️ 弹窗里要说清**从什么变成什么**，以及工具数会怎么变 ——
 * 只说"确定要改吗"等于把决定推回给用户而不给他判断依据。
 */
function ChangeRoleDialog({
  t,
  token,
  roleCode,
  roleLabel,
  onClose,
}: {
  t: (k: string, p?: Record<string, unknown>) => string
  token: McpToken
  roleCode: string
  roleLabel: string
  onClose: () => void
}) {
  const update = useUpdateToken()
  return (
    <Dialog
      open
      onClose={onClose}
      title={t('mcp:changeRole.title', { name: token.name })}
      closeLabel={t('common:action.close')}
      footer={
        <>
          <Button size="sm" onClick={onClose}>
            {t('common:write.cancel')}
          </Button>
          <Button
            size="sm"
            variant="primary"
            loading={update.isPending}
            onClick={() => update.mutate({ id: token.id, role_code: roleCode }, { onSuccess: onClose })}
          >
            {t('common:write.save')}
          </Button>
        </>
      }
    >
      <p className="text-[13px] leading-relaxed text-foreground">
        {t('mcp:changeRole.body', { to: roleLabel })}
      </p>
      <p className="mt-1.5 text-xs text-muted-foreground">{t('mcp:changeRole.impact')}</p>
      {update.isError ? (
        <p className="mt-2 text-xs text-danger">{t(toErrorInfo(update.error).messageKey)}</p>
      ) : null}
    </Dialog>
  )
}

/**
 * 设有效期。
 *
 * ⚠️ 留空 = 永久有效，而这一点必须在弹窗里写出来 ——
 * 一个空输入框旁边什么都不写，人会以为"不填就是必填项没填"。
 */
function ExpiryDialog({
  t,
  token,
  locale,
  onClose,
}: {
  t: (k: string, p?: Record<string, unknown>) => string
  token: McpToken
  locale: Locale
  onClose: () => void
}) {
  const update = useUpdateToken()
  // 回填成 yyyy-MM-dd 供 date 输入框用
  const [value, setValue] = useState(token.expires_at ? token.expires_at.slice(0, 10) : '')
  return (
    <Dialog
      open
      onClose={onClose}
      title={t('mcp:expiry.title', { name: token.name })}
      description={t('mcp:expiry.desc')}
      closeLabel={t('common:action.close')}
      footer={
        <>
          <Button size="sm" onClick={onClose}>
            {t('common:write.cancel')}
          </Button>
          <Button
            size="sm"
            variant="primary"
            loading={update.isPending}
            onClick={() =>
              update.mutate({ id: token.id, expires_at: value }, { onSuccess: onClose })
            }
          >
            {t('common:write.save')}
          </Button>
        </>
      }
    >
      <Field label={t('mcp:field.expires')} hint={t('mcp:expiry.hint')}>
        <TextInput type="date" value={value} onChange={(e) => setValue(e.target.value)} />
      </Field>
      {/* 当前状态明说一次，让人知道自己是在改什么 */}
      <p className="mt-2 text-xs text-muted-foreground">
        {token.expires_at
          ? t('mcp:expiry.current', { when: formatRelativeTime(token.expires_at, locale) })
          : t('mcp:expiry.currentNever')}
      </p>
      {value === '' ? (
        // 清空 = 改成永久。这是个权限面变大的动作，要说出来
        <p className="mt-1 text-xs text-warning">{t('mcp:expiry.clearWarn')}</p>
      ) : null}
      {update.isError ? (
        <p className="mt-2 text-xs text-danger">{t(toErrorInfo(update.error).messageKey)}</p>
      ) : null}
    </Dialog>
  )
}
