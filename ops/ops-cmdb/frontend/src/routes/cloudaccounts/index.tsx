import { toErrorInfo } from '@ops/api'
import { formatRelativeTime, tError, type Locale, useTranslation } from '@ops/i18n'
import {
  AsyncBoundary,
  Badge,
  Button,
  Dialog,
  EmptyState,
  Field,
  type LoadError,
  SecretInput,
  Skeleton,
  TextArea,
  TextInput,
  fromQuery,
} from '@ops/ui'
import { useQueryClient } from '@tanstack/react-query'
import { Cloud } from 'lucide-react'
import { useEffect, useRef, useState } from 'react'
import { RowMenu } from '../../components/RowMenu.js'
import { WriteButton } from '../../components/WriteButton.js'
import {
  type CloudAccount,
  type CloudProject,
  useCloudAccounts,
  useCreateAccount,
  useCreateProject,
  useUpdateProject,
  useDeleteAccount,
  useDeleteProject,
  useSyncAccount,
  useSyncProject,
  useSyncStatus,
} from './queries.js'

const PERM_MANAGE = 'cmdb:manage_cloud_projects'

export function CloudAccountsPage() {
  const { t, i18n } = useTranslation()
  const locale = i18n.language as Locale
  const query = useCloudAccounts()
  const [addAccount, setAddAccount] = useState(false)
  const [addProjectTo, setAddProjectTo] = useState<CloudAccount | null>(null)
  const [confirmDelete, setConfirmDelete] = useState<CloudAccount | null>(null)

  const toLoadError = (e: unknown): LoadError => {
    const n = toErrorInfo(e)
    return { cause: tError(t, n.messageKey, n.params), detail: n.detail, retryable: n.retryable, kind: n.kind === 'license' ? 'license' : undefined, feature: n.params?.feature as string | undefined }
  }

  return (
    <div className="mx-auto max-w-[900px] p-5">
      <div className="mb-4 flex items-baseline gap-2.5">
        <h1 className="text-sm font-semibold text-foreground">{t('cloudaccounts:title')}</h1>
        <p className="min-w-0 flex-1 text-xs text-muted-foreground">{t('cloudaccounts:hint')}</p>
        <WriteButton perm={PERM_MANAGE} variant="primary" size="sm" onClick={() => setAddAccount(true)}>
          {t('common:write.add')}
        </WriteButton>
      </div>

      <AsyncBoundary
        state={fromQuery<{ items: CloudAccount[] }>(query, (d) => d.items.length === 0, toLoadError)}
        errorTitle={t('cloudaccounts:error.title')}
        retryLabel={t('common:action.retry')}
        onRetry={() => void query.refetch()}
        pending={<Skeleton className="h-5 w-[60%]" />}
        empty={
          <EmptyState
            icon={<Cloud />}
            title={t('cloudaccounts:empty.title')}
            reason={t('cloudaccounts:empty.reason')}
            action={{ label: t('cloudaccounts:empty.action'), onClick: () => setAddAccount(true) }}
          />
        }
      >
        {(d) => (
          <div className="flex flex-col gap-4">
            {d.items.map((a) => (
              <AccountCard
                key={a.id}
                account={a}
                locale={locale}
                t={t}
                onAddProject={() => setAddProjectTo(a)}
                onDelete={() => setConfirmDelete(a)}
              />
            ))}
          </div>
        )}
      </AsyncBoundary>

      {addAccount ? <AddAccountDialog t={t} onClose={() => setAddAccount(false)} /> : null}
      {addProjectTo ? (
        <AddProjectDialog t={t} account={addProjectTo} onClose={() => setAddProjectTo(null)} />
      ) : null}
      {confirmDelete ? (
        <DeleteDialog t={t} account={confirmDelete} onClose={() => setConfirmDelete(null)} />
      ) : null}
    </div>
  )
}

function AccountCard({
  account,
  locale,
  t,
  onAddProject,
  onDelete,
}: {
  account: CloudAccount
  locale: Locale
  t: (k: string, o?: Record<string, unknown>) => string
  onAddProject: () => void
  onDelete: () => void
}) {
  const sync = useSyncAccount()
  const syncProject = useSyncProject()
  const delProjectM = useDeleteProject()
  const status = useSyncStatus(account.id)
  const qc = useQueryClient()
  const [delProject, setDelProject] = useState<CloudProject | null>(null)
  const [editProject, setEditProject] = useState<CloudProject | null>(null)
  const projects = account.projects ?? []
  const noCred = projects.filter((p) => !p.has_cred).length

  const running = status.data?.running ?? false

  /**
   * 「我刚点了同步」的本地状态。
   *
   * # 2026-08-19：收尾判据已简化（OPSCMDB-043 D1）
   *
   * 这里原来有三条收尾判据，前提是「后端把进度存在**进程内存**里，
   * 轮询打到别的 pod 就读不到」。**那个前提早就不成立了** ——
   * 进度已落库（`host_sync_progress` 表，见 handlers/hosts.go 的 hsPersist），
   * 任何副本都读得到，而且后端返回的 `running` **已经是 live**：
   * 它把 `updated_at` 超过 30 分钟的那一行当作副本已死，并在 error 里说明。
   *
   * 🔴 另外两条不只是多余，它们现在会给出**错误结论**：
   *
   *   ① 3 分钟超时兜底 —— 后端的存活 TTL 是 **30 分钟**（与 leases 对齐，
   *      因为大项目同步很慢）。客户端 3 分钟就解锁，等于在同步**真的还在跑**时
   *      放开按钮，人会再点一次。
   *
   *   ② 「项目行的最后同步/台数变了」—— 账号级同步会把每个项目各跑一遍，
   *      于是**第一个**项目完成时行就变了，界面当场判成"全部完成"，
   *      而后面几个项目还在跑。
   *
   * 所以现在只留一条：轮询看到这一轮结束。
   *
   * ⚠️ `fresh` 那道判断**必须保留**：库里会一直留着上一轮的结果
   * （started=true / running=false），不加的话刚点完就会被上一轮的"已完成"
   * 立刻判成完成 —— 按钮闪一下就解禁，等于没做。
   *
   * # 🔴 作用域必须区分账号级和项目级
   *
   * 原来它是个布尔：点**某一个项目**的同步也会把它置位，
   * 而每一行的判据是 `projectRunning(p.id) || pending` ——
   * 于是点一个项目，**三个项目全部显示「同步中」**。
   * 后端其实只同步了那一个，是界面把状态串了。
   *
   *   'account'  账号级同步（它确实会把每个项目各跑一遍，所有行都该置灰）
   *   number     只有这一个项目在跑
   *   null       空闲
   */
  const [pending, setPending] = useState<'account' | number | null>(null)
  const startedAt = useRef(0)

  useEffect(() => {
    if (pending === null) return
    // ⚠️ 必须确认这份进度是**这次触发之后**取到的。
    // 后端的进度状态会一直留着上一轮的结果（started=true / running=false），
    // 不加这道判断的话，刚点完就会被上一轮的"已完成"立刻判成完成 ——
    // 按钮闪一下就解禁，等于没做。
    const fresh = status.dataUpdatedAt > startedAt.current
    // ⚠️ 收尾判据也要按作用域看：项目级同步时账号级 running 未必置位，
    //	只看账号级的话，单项目同步会被立刻判成"已完成"
    const stillRunning =
      pending === 'account'
        ? (status.data?.running ?? false)
        : (status.data?.projects?.some((p) => p.project_id === pending && p.running) ?? false) ||
          (status.data?.running ?? false)
    // 只认这一条。另外两条（行变了 / 3 分钟超时）会误判，理由见上方注释。
    if (fresh && status.data?.started === true && !stillRunning) {
      setPending(null)
      void qc.invalidateQueries()
    }
  }, [pending, status.data, qc])

  // 🔴 兜底超时。
  //
  //	上面那条判据依赖轮询把新数据取回来 —— 而轮询本身也可能不启动
  //	（实测：refetchInterval 只看账号级 running，点单项目时它是 false，
  //	 于是轮询根本没开，「同步中…」永不结束，OPSCMDB-075）。
  //	那个 bug 已经修了，但**同类路径不止一条**：网络断了、页面挂起、
  //	后端进度行没落库…… 任何一条都会让这个状态卡死。
  //
  // ⚠️ 注释里曾写「3 分钟超时会误判所以不用」——那个权衡反了：
  //	"误判成完成"顶多让人多点一次同步；
  //	"永远显示同步中"会让整行操作永久不可用，而且看不出是界面坏了。
  //	所以给一个**明显长于**任何正常同步的上限（15 分钟，与后端 ctx 超时一致）。
  useEffect(() => {
    if (pending === null) return
    const id = setTimeout(
      () => {
        setPending(null)
        void qc.invalidateQueries()
      },
      15 * 60 * 1000,
    )
    return () => clearTimeout(id)
  }, [pending, qc])

  // 同步期间定期重拉列表：last_sync_at / 台数是落库的，哪个副本都读得到，
  // 所以即使轮询一直打在没状态的那个 pod 上，界面也能等到结果
  useEffect(() => {
    if (pending === null) return
    const id = setInterval(
      () => void qc.invalidateQueries({ queryKey: ['cloud-accounts'] }),
      2500,
    )
    return () => clearInterval(id)
  }, [pending, qc])

  /** 账号级：本地发起了**账号级**同步，或轮询看到账号级在跑 */
  const busy = pending === 'account' || running
  /**
   * 某个项目此刻在不在跑。
   *
   * 三个来源任一成立即算：轮询看到它在跑 / 本地刚点了它 /
   * 账号级同步在跑（那确实会把每个项目各跑一遍）。
   * ⚠️ 关键是**别把"点了别的项目"也算进来** —— 那正是原来的 bug。
   */
  const projectRunning = (pid: number) =>
    (status.data?.projects?.some((p) => p.project_id === pid && p.running) ?? false) ||
    pending === pid ||
    pending === 'account' ||
    running

  /**
   * 点同步后：本地立刻进入"在跑"，同时把轮询点着。
   * scope 决定置灰范围 —— 'account' 全置灰，项目 id 只置灰那一行。
   */
  const kick = (scope: 'account' | number) => () => {
    startedAt.current = Date.now()
    setPending(scope)
    void status.refetch()
  }

  return (
    <section className="rounded-[var(--radius-lg)] border border-border bg-card p-4">
      <div className="flex items-baseline gap-2.5">
        <span className="text-[13px] font-semibold text-foreground">{account.name}</span>
        <Badge tone="mute">{account.provider}</Badge>
        <span className="min-w-0 flex-1 text-xs text-muted-foreground">
          {t('cloudaccounts:projectCount', { count: projects.length })}
          {noCred > 0 ? (
            // 没配 SA key 的项目采不到任何东西，而它在列表里看起来和正常的一样
            <span className="ml-2 text-danger">{t('cloudaccounts:noCred', { count: noCred })}</span>
          ) : null}
        </span>
        {/* 同步中一律置灰：同步打的是云厂商 API，重复触发会撞配额 */}
        <WriteButton
          perm={PERM_MANAGE}
          size="sm"
          loading={sync.isPending || busy}
          onClick={() =>
            sync.mutate(account.id, { onSuccess: kick('account'), onError: () => setPending(null) })
          }
        >
          {busy ? t('cloudaccounts:syncing') : t('common:write.syncNow')}
        </WriteButton>
        <WriteButton perm={PERM_MANAGE} size="sm" onClick={onAddProject}>
          {t('cloudaccounts:addProject')}
        </WriteButton>
        <RowMenu
          perm={PERM_MANAGE}
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

      {/* 同步是后台异步的。不把进度显示出来的话，界面会一直停在旧值，
          而用户看到的就是"点了没反应"。四种状态各说各的话，不合并：
          触发失败 / 进行中 / 上一轮失败 / 上一轮完成 */}
      {sync.isError ? (
        // 触发就失败（没有项目、或全都在同步中）——比后台结果更要紧，排在最前
        <p className="mt-1.5 text-xs text-danger">{toErrorInfo(sync.error).detail || t(toErrorInfo(sync.error).messageKey)}</p>
      ) : busy ? (
        <p className="mt-1.5 text-xs text-info">
          {/* 实例清单还没拉回来时 total 是 0，显示 "0/0" 会像坏了 */}
          {status.data?.total
            ? t('cloudaccounts:syncProgress', {
                done: status.data.done ?? 0,
                total: status.data.total,
              })
            : t('cloudaccounts:syncPreparing')}
        </p>
      ) : sync.isSuccess && !status.data?.started ? (
        // 已受理但后端进度还没登记上的极短窗口
        <p className="mt-1.5 text-xs text-info">{t('cloudaccounts:syncQueued')}</p>
      ) : status.data?.started && status.data.error ? (
        <p className="mt-1.5 text-xs text-danger">
          {t('cloudaccounts:syncFailed', { error: status.data.error })}
        </p>
      ) : status.data?.started ? (
        <p className="mt-1.5 text-xs text-success">
          {status.data.stale
            ? t('cloudaccounts:syncDoneStale', {
                synced: status.data.synced ?? 0,
                stale: status.data.stale,
              })
            : t('cloudaccounts:syncDone', { synced: status.data.synced ?? 0 })}
        </p>
      ) : null}

      <div className="mt-3 flex flex-col">
        {projects.map((p) => (
          <div
            key={p.id}
            className="flex items-baseline gap-3 border-b border-border py-2 text-[13px] last:border-b-0"
          >
            <span className="min-w-0 flex-1 truncate font-mono text-xs text-foreground">
              {p.project_id}
            </span>
            {p.has_cred ? (
              <span className="text-xs text-muted-foreground">{t('cloudaccounts:credOk')}</span>
            ) : (
              <Badge tone="bad">{t('cloudaccounts:credMissing')}</Badge>
            )}
            <span className="tabular w-[70px] shrink-0 text-right text-xs text-muted-foreground">
              {t('cloudaccounts:hosts', { count: p.host_count })}
            </span>
            <span className="w-[92px] shrink-0 text-right text-xs text-muted-foreground">
              {p.last_sync_at
                ? formatRelativeTime(p.last_sync_at, locale)
                : t('cloudaccounts:neverSynced')}
            </span>
            {/*
              🔴 同步结果必须挂在**它自己那一行**。
              
              原来两个项目的 last_result 被聚合到了卡片级（见下方那段已删掉的
              `projects.find(...)`）—— 于是界面变成：
                GCP  2 个项目
                同步完成: 38 台在用          ← 挂在账号标题下
                  csc5002-public-uat  25 台
                  csc5002-infra       38 台
                同步 25 台在用                ← 挂在卡片最底部
              账号级读起来像「这个账号一共同步了 38 台」，
              **而实际是 25+38=63 台**（OPSCMDB-031 P1-58）。
              
              ⚠️ 同一个"取其中一个项目当账号结果"的逻辑也导致了凭据管理页
              报 38 台而巡检页报 63 台（P1-56）—— **一个根因，两页表现**。
            */}
            <span
              className={`w-[150px] shrink-0 truncate text-right text-xs ${
                // ⚠️ 判据来自后端 last_failed。前端再推一遍必然分叉，
                //	而分叉的表现正是“两条同样成功的同步一绿一橙”（031 P2-53）。
                //	老后端不返回该字段时按“没失败”处理 —— 保守方向是
                //	不误报，真失败的文案本身会写在旁边
                p.last_failed ? 'text-warning' : 'text-muted-foreground'
              }`}
              title={p.last_result}
            >
              {p.last_result || ''}
            </span>
            {/* 逐项目同步：整账号同步会把所有项目跑一遍，而排查时
                往往只想重跑出问题的那一个 */}
            {/* 账号级同步会把每个项目各跑一遍，所以这一行也要跟着置灰，
                否则用户会在整批跑的过程中又单独点某个项目 */}
            <WriteButton
              perm={PERM_MANAGE}
              size="sm"
              loading={
                (syncProject.isPending && syncProject.variables === p.id) || projectRunning(p.id)
              }
              blockedReason={p.has_cred ? undefined : t('cloudaccounts:needCred')}
              onClick={() =>
                syncProject.mutate(p.id, { onSuccess: kick(p.id), onError: () => setPending(null) })
              }
            >
              {projectRunning(p.id) ? t('cloudaccounts:syncing') : t('common:write.syncNow')}
            </WriteButton>
            {/* 编辑入口以前没有：凭据轮换时只能删掉重建，
                而删除会把这个项目下已采集的主机关联一并抹掉 */}
            <RowMenu
              perm={PERM_MANAGE}
              items={[
                { key: 'edit', label: t('common:write.edit'), onClick: () => setEditProject(p) },
                {
                  key: 'delete',
                  label: t('common:write.delete'),
                  onClick: () => setDelProject(p),
                  danger: true,
                },
              ]}
            />
          </div>
        ))}
      </div>
      {editProject ? (
        <EditProjectDialog t={t} project={editProject} onClose={() => setEditProject(null)} />
      ) : null}
      {delProject ? (
        <Dialog
          open
          onClose={() => setDelProject(null)}
          title={t('cloudaccounts:delProject.title')}
          closeLabel={t('common:action.close')}
          footer={
            <>
              <WriteButton perm={PERM_MANAGE} size="sm" onClick={() => setDelProject(null)}>
                {t('common:action.cancel')}
              </WriteButton>
              <WriteButton
                perm={PERM_MANAGE}
                variant="danger"
                size="sm"
                loading={delProjectM.isPending}
                onClick={() =>
                  delProjectM.mutate(delProject.id, { onSuccess: () => setDelProject(null) })
                }
              >
                {t('common:write.delete')}
              </WriteButton>
            </>
          }
        >
          {/* 说清后果：删项目不是删一行配置，是让这批资源从此不再更新，
              而它们在列表里看起来仍然正常 */}
          <p className="text-[13px] leading-relaxed text-foreground">
            {t('cloudaccounts:delProject.body', { project: delProject.project_id })}
          </p>
        </Dialog>
      ) : null}

      {/*
        ⚠️ 这里原来有一段卡片级的 `projects.find(...)` —— **取第一个项目的结果**
        显示在卡片底部。那正是 P1-58 的根因：两个项目各自的结果被摆到了
        账号标题下和卡片最底部，而不是各自那一行。
        结果已经挪进每个项目行里，这里不再聚合。
        
        账号级要显示的是**汇总**（一共几台），不是"其中某一个项目的结果"。
      */}
      {projects.length > 0 ? (
        <p className="mt-2 text-xs text-muted-foreground">
          {t('cloudaccounts:totalHosts', {
            projects: projects.length,
            hosts: projects.reduce((n, p) => n + (p.host_count ?? 0), 0),
          })}
        </p>
      ) : null}
    </section>
  )
}

function AddAccountDialog({ t, onClose }: { t: (k: string, o?: Record<string, unknown>) => string; onClose: () => void }) {
  const [name, setName] = useState('')
  const [dataset, setDataset] = useState('')
  const create = useCreateAccount()

  return (
    <Dialog
      open
      onClose={onClose}
      title={t('cloudaccounts:addTitle')}
      description={t('cloudaccounts:addDesc')}
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
            disabled={name.trim() === ''}
            onClick={() =>
              create.mutate(
                { name: name.trim(), provider: 'gcp', billing_export_dataset: dataset.trim() },
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
        <Field label={t('cloudaccounts:field.name')} hint={t('cloudaccounts:field.nameHint')} required>
          <TextInput value={name} onChange={(e) => setName(e.target.value)} autoFocus />
        </Field>
        <Field label={t('cloudaccounts:field.dataset')} hint={t('cloudaccounts:field.datasetHint')}>
          <TextInput value={dataset} onChange={(e) => setDataset(e.target.value)} />
        </Field>
        {create.isError ? (
          <p className="text-xs text-danger">{t(toErrorInfo(create.error).messageKey)}</p>
        ) : null}
      </div>
    </Dialog>
  )
}

function AddProjectDialog({
  t,
  account,
  onClose,
}: {
  t: (k: string, o?: Record<string, unknown>) => string
  account: CloudAccount
  onClose: () => void
}) {
  const [projectId, setProjectId] = useState('')
  const [cred, setCred] = useState('')
  const create = useCreateProject()

  return (
    <Dialog
      open
      onClose={onClose}
      title={t('cloudaccounts:addProjectTitle', { account: account.name })}
      description={t('cloudaccounts:addProjectDesc')}
      closeLabel={t('common:action.close')}
      width={560}
      footer={
        <>
          <Button size="sm" onClick={onClose}>
            {t('common:write.cancel')}
          </Button>
          <Button
            size="sm"
            variant="primary"
            loading={create.isPending}
            disabled={projectId.trim() === ''}
            onClick={() =>
              create.mutate(
                {
                  accountId: account.id,
                  name: projectId.trim(),
                  project_id: projectId.trim(),
                  cred_json: cred.trim(),
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
        <Field
          label={t('cloudaccounts:field.projectId')}
          hint={t('cloudaccounts:field.projectIdHint')}
          required
        >
          <TextInput value={projectId} onChange={(e) => setProjectId(e.target.value)} autoFocus />
        </Field>
        <Field label={t('cloudaccounts:field.saKey')} hint={t('cloudaccounts:field.saKeyHint')}>
          {/* SA key 是一整段 JSON，用多行框。它落库前会被 AES 加密，
              而且接口永远不会把它读回来 */}
          <TextArea rows={7} value={cred} onChange={(e) => setCred(e.target.value)} spellCheck={false} />
        </Field>
        {create.isError ? (
          <p className="text-xs text-danger">{t(toErrorInfo(create.error).messageKey)}</p>
        ) : null}
      </div>
    </Dialog>
  )
}

function DeleteDialog({
  t,
  account,
  onClose,
}: {
  t: (k: string, o?: Record<string, unknown>) => string
  account: CloudAccount
  onClose: () => void
}) {
  const del = useDeleteAccount()
  return (
    <Dialog
      open
      onClose={onClose}
      title={t('common:write.deleteConfirm', { name: account.name })}
      description={t('common:write.deleteHint')}
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
            onClick={() => del.mutate(account.id, { onSuccess: onClose })}
          >
            {t('common:write.delete')}
          </Button>
        </>
      }
    >
      {/* 删除影响面写清楚：这个账号下的项目、采集、以及已有主机会怎样 */}
      <p className="text-[13px] leading-relaxed text-foreground">
        {t('cloudaccounts:deleteImpact', { count: account.projects?.length ?? 0 })}
      </p>
      {del.isError ? (
        <p className="mt-2 text-xs text-danger">{t(toErrorInfo(del.error).messageKey)}</p>
      ) : null}
    </Dialog>
  )
}

/**
 * 编辑云项目。
 *
 * ⚠️ 凭据框留空 = **不改凭据**（后端就是这么定义的）。文案必须写清楚，
 * 否则有人改个名字顺手清空了凭据，而症状要等到下次同步才出现。
 */
function EditProjectDialog({
  t,
  project,
  onClose,
}: {
  t: (k: string, o?: Record<string, unknown>) => string
  project: CloudProject
  onClose: () => void
}) {
  const [name, setName] = useState(project.name || project.project_id)
  const [cred, setCred] = useState('')
  const update = useUpdateProject()

  return (
    <Dialog
      open
      onClose={onClose}
      title={t('cloudaccounts:editProjectTitle', { project: project.project_id })}
      description={t('cloudaccounts:editProjectDesc')}
      closeLabel={t('common:action.close')}
      width={560}
      footer={
        <>
          <Button size="sm" onClick={onClose}>
            {t('common:write.cancel')}
          </Button>
          <Button
            size="sm"
            variant="primary"
            loading={update.isPending}
            disabled={name.trim() === ''}
            onClick={() =>
              update.mutate(
                {
                  id: project.id,
                  name: name.trim(),
                  project_id: project.project_id,
                  cred_json: cred.trim(),
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
        <Field label={t('cloudaccounts:field.projectId')}>
          {/* project_id 是云上的真实标识，改了就指向另一个项目了 —— 只读 */}
          <span className="font-mono text-[13px] text-muted-foreground">{project.project_id}</span>
        </Field>
        <Field label={t('cloudaccounts:field.displayName')} required>
          <TextInput value={name} onChange={(e) => setName(e.target.value)} autoFocus />
        </Field>
        <Field label={t('cloudaccounts:field.saKey')} hint={t('cloudaccounts:field.saKeyKeepHint')}>
          <TextArea rows={7} value={cred} onChange={(e) => setCred(e.target.value)} spellCheck={false} />
        </Field>
        {update.isError ? (
          <p className="text-xs text-danger">{t(toErrorInfo(update.error).messageKey)}</p>
        ) : null}
      </div>
    </Dialog>
  )
}
