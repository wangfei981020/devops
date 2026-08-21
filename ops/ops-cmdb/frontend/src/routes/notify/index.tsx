import { toErrorInfo } from '@ops/api'
import { tError, useTranslation } from '@ops/i18n'
import {
  AsyncBoundary,
  Banner,
  Dialog,
  EmptyState,
  Field,
  type LoadError,
  Skeleton,
  TextInput,
  fromQuery,
} from '@ops/ui'
import { useState } from 'react'
import { RowMenu } from '../../components/RowMenu.js'
import { WriteButton } from '../../components/WriteButton.js'
import {
  type LarkGroup,
  type NotifyUser,
  useCreateLarkGroup,
  useCreateNotifyUser,
  useDeleteLarkGroup,
  useDeleteNotifyUser,
  useLarkGroups,
  useNotifyUsers,
  useTestLarkGroup,
  useTestNotify,
} from './queries.js'

const PERM = 'cmdb:manage_notify'

/**
 * 通知人与飞书群。
 *
 * ⚠️ 这一页空着是**危险状态**，不是中性状态：
 * 没有群 = 巡检发现磁盘快满、证书快到期时没有任何人会收到，
 * 而定时任务本身仍然显示「成功」。这是一种静默失效 ——
 * 真出事的时候才会发现，而那时候已经晚了。
 *
 * 所以这一页把「发送测试」放在很显眼的位置：配置保存成功
 * 只证明字段写进库了，**只有真发出去一条才证明这条链路是通的**。
 */
export function NotifyPage() {
  const { t } = useTranslation()
  const users = useNotifyUsers()
  const groups = useLarkGroups()
  const test = useTestNotify()
  const [addUser, setAddUser] = useState(false)
  const [addGroup, setAddGroup] = useState(false)
  const [delUser, setDelUser] = useState<NotifyUser | null>(null)
  const [delGroup, setDelGroup] = useState<LarkGroup | null>(null)

  const toLoadError = (e: unknown): LoadError => {
    const n = toErrorInfo(e)
    return { cause: tError(t, n.messageKey, n.params), detail: n.detail, retryable: n.retryable, kind: n.kind === 'license' ? 'license' : undefined, feature: n.params?.feature as string | undefined }
  }

  const groupList = groups.data?.items ?? []
  const noGroup = groups.isSuccess && groupList.length === 0
  // 全局兜底出口配在别处（基础配置 → 系统设置），不在这一页 ——
  // 它配着的话，"一个群都没配" ≠ "通知发不出去"（OPSCMDB-080）
  const hasFallback = groups.data?.globalFallbackSet === true

  return (
    <div className="mx-auto max-w-[900px] p-5">
      <div className="mb-4 flex items-baseline gap-2.5">
        <h1 className="text-sm font-semibold text-foreground">{t('notify:title')}</h1>
        <p className="min-w-0 flex-1 text-xs text-muted-foreground">{t('notify:hint')}</p>
      </div>

      {/* 一个群都没有时把后果说在最前面。等人翻到列表底部看到空态才知道，
          太晚了——他打开这一页多半就是因为"怎么没收到告警" */}
      {/* 🔴 三态，不是两态：
             有群 / 没群但有全局兜底 / 两者都没有。
             原来只判"有没有群"，于是配了兜底的实例也被告知"结果没人收得到"——
             那句话不成立，而人会照着它去配一个其实不需要的群，
             或者反过来以为通知彻底断了（生产实测两句话打架）。 */}
      {noGroup && hasFallback ? (
        <div className="mb-3">
          <Banner tone="info">
            <span className="font-medium">{t('notify:fallbackOnly.title')}</span>
            <span className="mt-0.5 block">{t('notify:fallbackOnly.body')}</span>
          </Banner>
        </div>
      ) : null}
      {noGroup && !hasFallback ? (
        <div className="mb-4">
          <Banner tone="warn">
            <span className="font-medium">{t('notify:noGroup.title')}</span>
            <span className="mt-0.5 block">{t('notify:noGroup.body')}</span>
          </Banner>
        </div>
      ) : null}

      <section className="mb-5 rounded-[var(--radius-lg)] border border-border p-4">
        <div className="mb-2.5 flex items-baseline gap-2.5">
          <div className="text-xs font-medium text-foreground">{t('notify:section.groups')}</div>
          <p className="min-w-0 flex-1 text-xs text-muted-foreground">
            {t('notify:section.groupsHint')}
          </p>
          <WriteButton perm={PERM} size="sm" onClick={() => setAddGroup(true)}>
            {t('notify:action.addGroup')}
          </WriteButton>
          <WriteButton
            perm={PERM}
            variant="primary"
            size="sm"
            loading={test.isPending}
            blockedReason={noGroup ? t('notify:action.testBlocked') : undefined}
            onClick={() => test.mutate()}
          >
            {t('notify:action.test')}
          </WriteButton>
        </div>

        {/* 测试结果三态分开：成功也要说清"去群里看"——
            接口返回成功只代表飞书收下了请求 */}
        {test.isSuccess ? (
          <Banner tone="info">
            <span>{test.data?.msg || t('notify:test.sent')}</span>
          </Banner>
        ) : null}
        {test.isError ? (
          <Banner tone="bad">
            <span className="font-medium">{t('notify:test.failed')}</span>
            <span className="mt-0.5 block">{toErrorInfo(test.error).detail}</span>
          </Banner>
        ) : null}

        <AsyncBoundary
          state={fromQuery(groups, (d) => d.items.length === 0, toLoadError)}
          errorTitle={t('notify:error.groups')}
          retryLabel={t('common:action.retry')}
          onRetry={() => void groups.refetch()}
          pending={<Skeleton className="h-5 w-[50%]" />}
          empty={
            <EmptyState
              title={t('notify:empty.groupTitle')}
              reason={t('notify:empty.groupReason')}
              action={null}
            />
          }
        >
          {(d) => (
            <div className="flex flex-col">
              {d.items.map((g) => (
                <div
                  key={g.id}
                  className="flex items-center gap-3 border-b border-border py-2 text-[13px] last:border-0"
                >
                  <span className="min-w-0 flex-1 truncate font-medium text-foreground">
                    {g.name}
                  </span>
                  {/* webhook 只显示尾段：它本身就是凭据，拿到就能往群里发消息。
                      但完全不显示的话，两个同名群就分不出哪个是哪个 */}
                  <code className="font-mono text-[11px] text-muted-foreground">
                    …{g.webhook.slice(-12)}
                  </code>
                  {/* ⚠️ 逐群测试的入口以前没有。webhook 配错时，
                      「告警发不出去」这件事平时完全看不见 —— 只有真发一条才知道 */}
                  <GroupTest id={g.id} t={t} />
                  <RowMenu
                    perm={PERM}
                    items={[
                      {
                        key: 'delete',
                        label: t('common:write.delete'),
                        onClick: () => setDelGroup(g),
                        danger: true,
                      },
                    ]}
                  />
                </div>
              ))}
            </div>
          )}
        </AsyncBoundary>
      </section>

      <section className="rounded-[var(--radius-lg)] border border-border p-4">
        <div className="mb-2.5 flex items-baseline gap-2.5">
          <div className="text-xs font-medium text-foreground">{t('notify:section.users')}</div>
          <p className="min-w-0 flex-1 text-xs text-muted-foreground">
            {t('notify:section.usersHint')}
          </p>
          <WriteButton perm={PERM} size="sm" onClick={() => setAddUser(true)}>
            {t('notify:action.addUser')}
          </WriteButton>
        </div>

        <AsyncBoundary
          state={fromQuery(users, (d) => d.length === 0, toLoadError)}
          errorTitle={t('notify:error.title')}
          retryLabel={t('common:action.retry')}
          onRetry={() => void users.refetch()}
          pending={<Skeleton className="h-5 w-[50%]" />}
          empty={
            <EmptyState
              title={t('notify:empty.title')}
              reason={t('notify:empty.reason')}
              action={null}
            />
          }
        >
          {(list) => (
            <div className="flex flex-col">
              {list.map((u) => (
                <div
                  key={u.id}
                  className="flex items-center gap-3 border-b border-border py-2 text-[13px] last:border-0"
                >
                  <span className="min-w-0 flex-1 truncate font-medium text-foreground">
                    {u.name}
                  </span>
                  <code className="max-w-[220px] truncate font-mono text-[11px] text-muted-foreground">
                    {u.open_id}
                  </code>
                  <RowMenu
                    perm={PERM}
                    items={[
                      {
                        key: 'delete',
                        label: t('common:write.delete'),
                        onClick: () => setDelUser(u),
                        danger: true,
                      },
                    ]}
                  />
                </div>
              ))}
            </div>
          )}
        </AsyncBoundary>
      </section>

      {addGroup ? <AddGroupDialog t={t} onClose={() => setAddGroup(false)} /> : null}
      {addUser ? <AddUserDialog t={t} onClose={() => setAddUser(false)} /> : null}
      {delGroup ? (
        <DeleteGroupDialog t={t} group={delGroup} onClose={() => setDelGroup(null)} />
      ) : null}
      {delUser ? <DeleteUserDialog t={t} user={delUser} onClose={() => setDelUser(null)} /> : null}
    </div>
  )
}

type T = (k: string, p?: Record<string, unknown>) => string

function AddGroupDialog({ t, onClose }: { t: T; onClose: () => void }) {
  const [name, setName] = useState('')
  const [webhook, setWebhook] = useState('')
  const create = useCreateLarkGroup()
  return (
    <Dialog
      open
      onClose={onClose}
      title={t('notify:dialog.addGroup')}
      description={t('notify:dialog.addGroupDesc')}
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
            blockedReason={name && webhook ? undefined : t('notify:field.bothRequired')}
            onClick={() => create.mutate({ name, webhook }, { onSuccess: onClose })}
          >
            {t('common:write.create')}
          </WriteButton>
        </>
      }
    >
      <div className="flex flex-col gap-3">
        <Field label={t('notify:field.groupName')} hint={t('notify:field.groupNameHint')} required>
          <TextInput value={name} onChange={(e) => setName(e.target.value)} autoFocus />
        </Field>
        <Field label={t('notify:field.webhook')} hint={t('notify:field.webhookHint')} required>
          <TextInput
            value={webhook}
            onChange={(e) => setWebhook(e.target.value)}
            placeholder="https://open.feishu.cn/open-apis/bot/v2/hook/..."
          />
        </Field>
        {create.isError ? (
          <Banner tone="bad">
            <span>{tError(t, toErrorInfo(create.error).messageKey, toErrorInfo(create.error).params)}</span>
          </Banner>
        ) : null}
      </div>
    </Dialog>
  )
}

function AddUserDialog({ t, onClose }: { t: T; onClose: () => void }) {
  const [name, setName] = useState('')
  const [openID, setOpenID] = useState('')
  const create = useCreateNotifyUser()
  return (
    <Dialog
      open
      onClose={onClose}
      title={t('notify:dialog.addUser')}
      description={t('notify:dialog.addUserDesc')}
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
            blockedReason={name && openID ? undefined : t('notify:field.bothRequired')}
            onClick={() => create.mutate({ name, open_id: openID }, { onSuccess: onClose })}
          >
            {t('common:write.create')}
          </WriteButton>
        </>
      }
    >
      <div className="flex flex-col gap-3">
        <Field label={t('notify:field.userName')} required>
          <TextInput value={name} onChange={(e) => setName(e.target.value)} autoFocus />
        </Field>
        <Field label={t('notify:field.openId')} hint={t('notify:field.openIdHint')} required>
          <TextInput
            value={openID}
            onChange={(e) => setOpenID(e.target.value)}
            placeholder="ou_xxxxxxxx"
          />
        </Field>
        {create.isError ? (
          <Banner tone="bad">
            <span>{tError(t, toErrorInfo(create.error).messageKey, toErrorInfo(create.error).params)}</span>
          </Banner>
        ) : null}
      </div>
    </Dialog>
  )
}

function DeleteGroupDialog({ t, group, onClose }: { t: T; group: LarkGroup; onClose: () => void }) {
  const del = useDeleteLarkGroup()
  return (
    <Dialog
      open
      onClose={onClose}
      title={t('notify:dialog.delGroup')}
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
            onClick={() => del.mutate(group.id, { onSuccess: onClose })}
          >
            {t('common:write.delete')}
          </WriteButton>
        </>
      }
    >
      {/* 说清后果：删群不是删一条记录，是让绑在它上面的任务从此发不出通知 */}
      <p className="text-[13px] leading-relaxed text-foreground">
        {t('notify:delete.group', { name: group.name })}
      </p>
    </Dialog>
  )
}

function DeleteUserDialog({ t, user, onClose }: { t: T; user: NotifyUser; onClose: () => void }) {
  const del = useDeleteNotifyUser()
  return (
    <Dialog
      open
      onClose={onClose}
      title={t('notify:dialog.delUser')}
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
            onClick={() => del.mutate(user.id, { onSuccess: onClose })}
          >
            {t('common:write.delete')}
          </WriteButton>
        </>
      }
    >
      <p className="text-[13px] leading-relaxed text-foreground">
        {t('notify:delete.user', { name: user.name })}
      </p>
    </Dialog>
  )
}

/** 往这个群发一条测试消息。结果就地显示，失败要带上原因 */
function GroupTest({
  id,
  t,
}: {
  id: number
  t: (k: string, p?: Record<string, unknown>) => string
}) {
  const test = useTestLarkGroup()
  return (
    <>
      {test.isPending ? (
        <span className="text-[11px] text-muted-foreground">{t('common:state.loading')}</span>
      ) : test.isError ? (
        <span
          className="max-w-[200px] truncate text-[11px] text-danger"
          title={toErrorInfo(test.error).detail}
        >
          {tError(t, toErrorInfo(test.error).messageKey, toErrorInfo(test.error).params)}
        </span>
      ) : test.isSuccess ? (
        <span className="text-[11px] text-success">
          {test.data?.msg ?? t('notify:group.testSent')}
        </span>
      ) : null}
      <WriteButton perm={PERM} size="sm" onClick={() => test.mutate(id)}>
        {t('notify:group.test')}
      </WriteButton>
    </>
  )
}
