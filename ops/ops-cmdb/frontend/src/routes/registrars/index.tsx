import { toErrorInfo } from '@ops/api'
import { tError, useTranslation } from '@ops/i18n'
import {
  AsyncBoundary,
  Badge,
  Banner,
  Dialog,
  Field,
  type LoadError,
  SecretInput,
  Select,
  Skeleton,
  Switch,
  TextInput,
  fromQuery,
} from '@ops/ui'
import { useState } from 'react'
import { useTaskRuns } from '../cron/queries.js'
import { RowMenu } from '../../components/RowMenu.js'
import { WriteButton } from '../../components/WriteButton.js'
import { ObjectHistoryDialog } from '../../components/ObjectHistory.js'
import {
  NO_SYNC_PROVIDERS,
  PROVIDERS,
  type Registrar,
  useDeleteRegistrar,
  useRegistrars,
  useSaveRegistrar,
  useSourceUsage,
  useSyncProgress,
  useSyncRegistrar,
} from './queries.js'

const PERM = 'cmdb:manage_basic'

/**
 * 域名注册商接入。
 *
 * ⚠️ 这一页为空的后果不显眼但很实在：域名的到期日永远不会更新，
 * 而列表里那些域名看起来一切正常 —— 只有"最后同步"停在很久以前。
 * 到期提醒也就跟着失效了。
 */
export function RegistrarsPage() {
  const { t } = useTranslation()
  const query = useRegistrars()
  const [editing, setEditing] = useState<Registrar | null | undefined>(undefined)
  const [delFor, setDelFor] = useState<Registrar | null>(null)
  const [historyFor, setHistoryFor] = useState<Registrar | null>(null)

  const toLoadError = (e: unknown): LoadError => {
    const n = toErrorInfo(e)
    return { cause: tError(t, n.messageKey, n.params), detail: n.detail, retryable: n.retryable, kind: n.kind === 'license' ? 'license' : undefined, feature: n.params?.feature as string | undefined }
  }

  return (
    <div className="mx-auto max-w-[900px] p-5">
      <div className="mb-4 flex items-baseline gap-2.5">
        <h1 className="text-sm font-semibold text-foreground">{t('registrars:title')}</h1>
        <p className="min-w-0 flex-1 text-xs text-muted-foreground">
          {t('registrars:hint')}{' '}
          {/*
            ⚠️ 「接入注册商」按钮不说明可选范围，用户无从判断
            "我的域名能不能管起来"（OPSCMDB-031 P2-59）。
            所以在页头直接说清：能自动同步的只有 GoDaddy，
            其余几个只能登记。

            ⚠️ 这句话的口径由 NO_SYNC_PROVIDERS 决定，而那份常量
            由 check-sync-providers 守卫钉死在后端 SyncSupported 上 ——
            不会出现"文案说支持、实际不支持"。
          */}
          <span className="text-warning">{t('registrars:supportedHint')}</span>
        </p>
        <WriteButton perm={PERM} variant="primary" size="sm" onClick={() => setEditing(null)}>
          {t('registrars:action.add')}
        </WriteButton>
      </div>

      {/*
        ⚠️ 「上次自动同步成不成」必须在这一页显示。

        原来这一页只有名称、厂商、几个徽章，以及一个「立即同步」按钮 ——
        而那个按钮只反馈**本次点击**的结果。于是 registrar_expiry_sync
        连续失败 5 天（每天 2 次、共 10 次全 fail）时，这一页看起来完全正常
        （OPSCMDB-031 P1-65）。
        而这一页恰恰是唯一让人想起"域名到期日是谁在更新"的地方。

        取的是定时任务的最近一次执行记录 —— 和「定时任务」页同一个数据源，
        不另算一套判据。
      */}
      <SyncHealth t={t} />

      <AsyncBoundary
        state={fromQuery(query, (d) => d.length === 0, toLoadError)}
        errorTitle={t('registrars:error.title')}
        retryLabel={t('common:action.retry')}
        onRetry={() => void query.refetch()}
        pending={<Skeleton className="h-5 w-[60%]" />}
        empty={
          <div className="rounded-[var(--radius-lg)] border border-dashed border-border px-6 py-10 text-center">
            <p className="text-[13px] font-medium text-foreground">{t('registrars:empty.title')}</p>
            <p className="mx-auto mt-1 max-w-[520px] text-xs leading-relaxed text-muted-foreground">
              {t('registrars:empty.reason')}
            </p>
          </div>
        }
      >
        {(list) => (
          <div className="flex flex-col">
            {list.map((r) => (
              <div
                key={r.id}
                className="flex items-center gap-3 border-b border-border py-2.5 text-[13px] last:border-0"
              >
                <div className="min-w-0 flex-1">
                  <div className="flex items-center gap-2">
                    <span className="truncate font-medium text-foreground">{r.name}</span>
                    <Badge tone="mute">
                      {t(`registrars:provider.${r.provider}`, { defaultValue: r.provider })}
                    </Badge>
                    {r.enabled !== 1 ? (
                      <Badge tone="mute">{t('registrars:disabled')}</Badge>
                    ) : null}
                    {/*
                      ⚠️ 预演模式的说明必须就在徽章旁边，不能只在编辑弹窗里。

                      这是一个**决定写操作是否真执行**的开关，而域名续费是非幂等写
                      （失败不等于没扣费）。原来列表页只有「预演模式」四个字、
                      整页 tooltip 扫描确认它没有任何解释 ——
                      任何人看到这一页都无法判断"现在点续费到底会不会真花钱"
                      （OPSCMDB-031 P0-21）。

                      说明要覆盖三件事，缺一件人就得自己猜：
                        · 什么会被跳过（续费、DNS 写回等**写**请求）
                        · 什么不受影响（到期日同步是读 GoDaddy + 写自己的库，照常）
                        · 关掉之后会发生什么
                    */}
                    {r.dry_run ? (
                      <span title={t('registrars:dryRunBadgeHint')}>
                        <Badge tone="warn">{t('registrars:dryRun')}</Badge>
                      </span>
                    ) : (
                      // ⚠️ 关掉时也要说 —— 「没有徽章」传达不了「续费会真扣费」这件事。
                      // 一个只在安全态显示提示、危险态什么都不显示的界面是反的
                      <span title={t('registrars:liveBadgeHint')}>
                        <Badge tone="bad">{t('registrars:live')}</Badge>
                      </span>
                    )}
                    {/*
                      凭据状态**两态都显示**。
                      原来只在 has_cred=false 时标红，于是配好的那些什么都不显示 ——
                      而"凭据到底配没配"恰恰是续费之前最该确认的一件事。
                      （031 P1-66 报「has_cred 没接」也正是因为它当时是 true、什么都看不见。）
                    */}
                    {r.has_cred ? (
                      <Badge tone="ok">{t('registrars:hasCred')}</Badge>
                    ) : (
                      <Badge tone="bad">{t('registrars:noCred')}</Badge>
                    )}
                  </div>
                </div>
                {/* 单条同步：域名页那个「立即同步」是遍历全部注册商的，
                    排查某一条接入时不该把其它几条也拖去重跑 */}
                <RegistrarSync id={r.id} t={t} />
                {r.sync_supported === false && r.provider !== 'other' ? (
                  // 🔴 不能只显示「已启用」——那会让人以为它在同步。
                  // 这条提示要说清后果（到期日不会更新），不是笼统的"不支持"
                  <span title={t('registrars:noSyncHint')}>
                    <Badge tone="warn">{t('registrars:noSync')}</Badge>
                  </span>
                ) : null}
                <RowMenu
                  perm={PERM}
                  items={[
                    { key: 'edit', label: t('common:write.edit'), onClick: () => setEditing(r) },
                    // 「到期日怎么不更新了」—— 常见根因是有人换了凭据或停用了它，
                    // 而那件事只在审计里留了痕
                    {
                      key: 'history',
                      label: t('audit:objectHistory.entry'),
                      onClick: () => setHistoryFor(r),
                    },
                    {
                      key: 'delete',
                      label: t('common:write.delete'),
                      onClick: () => setDelFor(r),
                      danger: true,
                    },
                  ]}
                />
              </div>
            ))}
          </div>
        )}
      </AsyncBoundary>

      {editing !== undefined ? (
        <RegistrarDialog initial={editing ?? undefined} onClose={() => setEditing(undefined)} />
      ) : null}
      {delFor ? <DeleteDialog reg={delFor} onClose={() => setDelFor(null)} /> : null}
      {historyFor ? (
        <ObjectHistoryDialog
          table="registrars"
          pk={String(historyFor.id)}
          title={historyFor.name}
          onClose={() => setHistoryFor(null)}
        />
      ) : null}
    </div>
  )
}

function RegistrarDialog({
  initial,
  onClose,
}: {
  initial?: Registrar
  onClose: () => void
}) {
  const { t } = useTranslation()
  const save = useSaveRegistrar()
  const [f, setF] = useState({
    name: initial?.name ?? '',
    provider: initial?.provider ?? 'godaddy',
    key: '',
    secret: '',
    dry_run: initial?.dry_run ?? false,
    enabled: initial?.enabled ?? 1,
  })
  const set = <K extends keyof typeof f>(k: K, v: (typeof f)[K]) => setF((p) => ({ ...p, [k]: v }))

  return (
    <Dialog
      open
      onClose={onClose}
      title={initial ? t('registrars:dialog.edit') : t('registrars:dialog.add')}
      description={t('registrars:dialog.desc')}
      closeLabel={t('common:action.close')}
      width={560}
      footer={
        <>
          <WriteButton perm={PERM} size="sm" onClick={onClose}>
            {t('common:action.cancel')}
          </WriteButton>
          <WriteButton
            perm={PERM}
            variant="primary"
            size="sm"
            loading={save.isPending}
            blockedReason={f.name ? undefined : t('registrars:field.nameRequired')}
            onClick={() =>
              save.mutate(
                {
                  id: initial?.id,
                  name: f.name,
                  provider: f.provider,
                  // 留空 = 保留原凭据。整个对象为空时后端不动它。
                  // 🔴 键名必须是 api_key / api_secret —— 后端 dnsource 与 acme 两处
                  // 读的都是这两个名字。曾经这里发的是 key/secret，后端取到空串，
                  // 认证头拼成 "sso-key :"，表现为"密钥明明是对的却一直不通"，
                  // 而界面显示"已配置"、保存也成功，没有任何地方报错。
                  credential:
                    f.key || f.secret ? { api_key: f.key, api_secret: f.secret } : {},
                  dry_run: f.dry_run,
                  enabled: f.enabled,
                },
                { onSuccess: onClose },
              )
            }
          >
            {t('common:write.save')}
          </WriteButton>
        </>
      }
    >
      <div className="flex flex-col gap-3">
        <Field label={t('registrars:field.name')} required>
          <TextInput value={f.name} onChange={(e) => set('name', e.target.value)} autoFocus />
        </Field>

        <div className="flex flex-col gap-1.5">
          <Select<string>
            label={t('registrars:field.provider')}
            value={f.provider}
            onChange={(v) => set('provider', v)}
            options={PROVIDERS.map((p) => ({ value: p, label: t(`registrars:provider.${p}`) }))}
            className="self-start"
          />
          <span className="text-xs leading-relaxed text-muted-foreground">
            {t('registrars:field.providerHint')}
          </span>
          {/* 🔴 选中即说清后果，不要等保存完再让人猜。
              Cloudflare 尤其要单独说：它在本系统里的真正接入点是「CDN 站点」页
              （API Token + account_tag），在注册商这里选它只会登记、不会同步，
              而用户看到下拉里有 Cloudflare，自然会以为 token 就配在这里 */}
          {f.provider === 'cloudflare' ? (
            <span className="text-xs leading-relaxed text-warning">
              {t('registrars:field.cloudflareHint')}
            </span>
          ) : NO_SYNC_PROVIDERS.includes(f.provider) ? (
            <span className="text-xs leading-relaxed text-warning">
              {t('registrars:field.noSyncSelected')}
            </span>
          ) : null}
        </div>

        <Field
          label={t('registrars:field.key')}
          hint={initial?.has_cred ? t('common:write.secretKeep') : t('registrars:field.keyHint')}
        >
          <TextInput value={f.key} onChange={(e) => set('key', e.target.value)} />
        </Field>
        <Field label={t('registrars:field.secret')}>
          <SecretInput
            configured={initial?.has_cred ?? false}
            placeholderConfigured="••••••••"
            placeholderEmpty=""
            showLabel={t('common:action.showSecret')}
            hideLabel={t('common:action.hideSecret')}
            value={f.secret}
            onChange={(e) => set('secret', e.target.value)}
          />
        </Field>

        <div className="flex flex-col gap-1.5">
          <Switch
            checked={f.dry_run}
            onChange={(v) => set('dry_run', v)}
            label={t('registrars:field.dryRun')}
          />
          <span className="text-xs leading-relaxed text-muted-foreground">
            {t('registrars:field.dryRunHint')}
          </span>
        </div>

        <Switch
          checked={f.enabled === 1}
          onChange={(v) => set('enabled', v ? 1 : 0)}
          label={t('registrars:field.enabled')}
        />

        {save.isError ? (
          <Banner tone="bad">
            <span>{tError(t, toErrorInfo(save.error).messageKey, toErrorInfo(save.error).params)}</span>
            <span className="mt-0.5 block">{toErrorInfo(save.error).detail}</span>
          </Banner>
        ) : null}
      </div>
    </Dialog>
  )
}

function DeleteDialog({ reg, onClose }: { reg: Registrar; onClose: () => void }) {
  const { t } = useTranslation()
  const del = useDeleteRegistrar()
  return (
    <Dialog
      open
      onClose={onClose}
      title={t('registrars:dialog.del')}
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
            onClick={() => del.mutate(reg.id, { onSuccess: onClose })}
          >
            {t('common:write.delete')}
          </WriteButton>
        </>
      }
    >
      <p className="text-[13px] leading-relaxed text-foreground">
        {t('registrars:delete.body', { name: reg.name })}
      </p>
    </Dialog>
  )
}

function RegistrarSync({
  id,
  t,
}: {
  id: number
  t: (k: string, p?: Record<string, unknown>) => string
}) {
  const sync = useSyncRegistrar()
  // 🔴 点完之后才开始盯进度：同步是后台跑的，按钮变回原样时往往还没跑完。
  //	不盯的话，界面上「已保存」和「其实同步中断了」长得一模一样
  //	（OPSCMDB-023 第三档）。
  const [watching, setWatching] = useState(false)
  const prog = useSyncProgress(watching ? id : null)
  const p = prog.data

  return (
    <>
      {sync.isPending ? (
        <span className="text-[11px] text-muted-foreground">{t('common:state.loading')}</span>
      ) : sync.isError ? (
        <span
          className="max-w-[220px] truncate text-[11px] text-danger"
          title={toErrorInfo(sync.error).detail}
        >
          {tError(t, toErrorInfo(sync.error).messageKey, toErrorInfo(sync.error).params)}
        </span>
      ) : p?.interrupted ? (
        // 🔴 中断：跑它的副本已经退出。既不是"同步中"也不是"没在同步"，
        //	必须自成一档，否则这个数据源看起来要么永远在转、要么像没点过
        <span className="max-w-[260px] truncate text-[11px] text-danger" title={p.error}>
          {t('registrars:sync.interrupted', { replica: p.replica || '—' })}
        </span>
      ) : p?.running ? (
        <span className="text-[11px] text-muted-foreground">
          {t('registrars:sync.progress', { done: p.done ?? 0, total: p.total ?? 0 })}
        </span>
      ) : p?.started && p.finished_at ? (
        <span className="flex items-center gap-1.5 text-[11px]" title={p.error || undefined}>
          <span className="text-success">
            {t('registrars:sync.done', {
              domains: p.synced_domains ?? 0,
              records: p.synced_records ?? 0,
            })}
          </span>
          {/* 新导入了多少条：只说"同步了 1200 条"看不出这次有没有变化，
              而"新增 0 条"和"新增 37 条"对下一步的意义完全不同 */}
          {(p.imported_records ?? 0) > 0 ? (
            <span className="text-info">
              {t('registrars:sync.imported', { n: p.imported_records })}
            </span>
          ) : null}
          {/* 🔴 注册商侧查不到的域名。
              这不是"少同步了几条"，是**这个域名在注册商那边不见了** ——
              可能已被转出或过期。不说出来的话，一次显示「完成」的同步
              会盖住一件需要立刻查的事。 */}
          {(p.stale_domains ?? 0) > 0 ? (
            <span className="text-warning" title={t('registrars:sync.staleHint')}>
              {t('registrars:sync.stale', { n: p.stale_domains })}
            </span>
          ) : null}
        </span>
      ) : sync.isSuccess ? (
        <span className="text-[11px] text-success">{sync.data?.msg ?? t('common:write.saved')}</span>
      ) : null}
      <ApiUsage id={id} t={t} />
      <WriteButton
        perm={PERM}
        size="sm"
        onClick={() => sync.mutate(id, { onSuccess: () => setWatching(true) })}
      >
        {t('common:write.syncNow')}
      </WriteButton>
    </>
  )
}

/**
 * 到期日同步的健康状况。
 *
 * ⚠️ 三态，不能压成"正常/异常"：
 *   从没跑过  → 任务可能没启用，到期日从来没被更新过
 *   上次失败  → 显示失败摘要（现在后端会把「哪个源+什么原因」拼进去）
 *   上次成功  → 显示时间，让人能判断新鲜度
 *
 * 拿不到执行记录时**什么都不显示**，不要显示"正常" ——
 * 那会把"不知道"说成"没问题"。
 */
function SyncHealth({ t }: { t: (k: string, p?: Record<string, unknown>) => string }) {
  const runs = useTaskRuns('registrar_expiry_sync', 1)
  const last = runs.data?.items?.[0]
  if (runs.isPending || runs.isError) return null
  if (!last) {
    return (
      <Banner tone="warn">
        <span className="font-medium">{t('registrars:sync.neverRan')}</span>
        <span className="mt-0.5 block">{t('registrars:sync.neverRanHint')}</span>
      </Banner>
    )
  }
  const ok = last.status === 'ok'
  return (
    <Banner tone={ok ? 'info' : 'bad'}>
      <span className="font-medium">
        {ok
          ? t('registrars:sync.lastOk', { at: last.started_at })
          : t('registrars:sync.lastFail', { at: last.started_at, status: last.status })}
      </span>
      {/* 失败摘要原样显示：后端现在会把「哪个数据源（什么原因）」拼进 summary，
          而那句话是排障的唯一落点 */}
      {!ok && last.summary ? (
        <span className="mt-0.5 block">{last.summary}</span>
      ) : null}
      {/* 失败明细里有 target+reason，比摘要更全 */}
      {!ok && (last.failures ?? []).length > 0 ? (
        <ul className="mt-1 flex flex-col gap-0.5">
          {(last.failures ?? []).map((f) => (
            <li key={`${f.target}-${f.reason}`} className="text-[11px]">
              <span className="font-mono">{f.target}</span>：{f.reason}
            </li>
          ))}
        </ul>
      ) : null}
    </Banner>
  )
}

/**
 * 注册商 API 的限流用量。
 *
 * `last_limited_at` 是「域名到期日突然不更新了」的静默根因 ——
 * 打爆厂商配额后同步会被拖慢甚至失败，而域名列表看起来一切正常。
 *
 * # 🔴 数字的边界必须写在 title 里
 *
 * 这些是**本副本进程内**的计数：重启归零、多副本各算各的。
 * 所以「今日 0 次」不等于「今天没同步过」—— 可能是别的副本干的活。
 * 不说清楚的话，这个 0 会被读成"同步没跑"，然后去查一个不存在的问题。
 */
function ApiUsage({ id, t }: { id: number; t: (k: string, p?: Record<string, unknown>) => string }) {
  const q = useSourceUsage(id)
  // 拿不到就什么都不显示 —— 不要显示 0，那是把"不知道"说成"没用过"
  if (q.isPending || q.isError || !q.data) return null
  const u = q.data
  const limited = !!u.last_limited_at
  return (
    <span
      className={`text-[11px] ${limited ? 'text-warning' : 'text-muted-foreground'}`}
      title={t('registrars:usage.hint')}
    >
      {t('registrars:usage.text', {
        minute: u.minute_used ?? 0,
        limit: u.limit ?? 0,
        today: u.today_total ?? 0,
      })}
      {limited ? ` · ${t('registrars:usage.limited', { at: u.last_limited_at })}` : ''}
    </span>
  )
}
