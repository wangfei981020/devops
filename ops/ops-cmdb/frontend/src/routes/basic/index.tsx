import { toErrorInfo } from '@ops/api'
import { tError, useTranslation } from '@ops/i18n'
import { AsyncBoundary, Badge, Banner, Dialog, EmptyState, Field, MutationError, Select, Skeleton, TextInput, fromQuery, type LoadError } from '@ops/ui'
import { Pencil, X } from 'lucide-react'
import { useState } from 'react'
import { WriteButton } from '../../components/WriteButton.js'
import { dictLabel } from '../../lib/dicts.js'
import { ENV_TAG_TYPES, envTone } from '../../lib/envTone.js'
import {
  useBasicDicts,
  useSaveSettings,
  useSettings,
  useCreateEnv,
  useCreateProject,
  useCreateStatus,
  useDeleteEnv,
  useDeleteProject,
  useDeleteStatus,
  useUpdateEnv,
  useUpdateProject,
  useUpdateStatus,
} from './queries.js'

const PERM = 'cmdb:manage_basic'

/**
 * 字典项。
 *
 * ⚠️ `name_en` / `label_en` 是**可选**的：没填时界面回退显示中文名。
 * 这些是客户能自己增删改的数据，不能要求每一项都填两种语言
 * —— 绝大多数客户只用一种。
 */
type Item = {
  id: number
  code: string
  name: string
  /**
   * 徽章样式的**原始值**（旧版调色名）。有值时 code 渲染成带色徽章（环境字典用）。
   * ⚠️ 存原始值而不是算好的 tone：编辑弹窗要拿它回填下拉，
   *	存 tone 的话得反向映射一次，而反向映射是不可逆的（多个值映射到同一个 tone）。
   */
  tagType?: string
  /** 备注。项目名常是缩写，备注才说得清它是什么（后端一直返回，此前零处渲染）*/
  remark?: string
  name_en?: string
  label?: string
  label_en?: string
}

/**
 * 基础配置：环境、业务项目、配置项类型、生命周期状态。
 *
 * ⚠️ 删除字典项**不是删一行数据**：引用它的资产不会跟着变，
 * 那个字符串还留在 hosts/cis 里，只是下拉里再也没有这一项 ——
 * 那批资产从筛选视图里消失，列表里却还在，没有任何报错。
 * 所以删除走后端的引用检查（有人在用就 409 + 数量），
 * 前端要把那句话原样显示出来。
 */
export function BasicPage() {
  const { t } = useTranslation()
  const query = useBasicDicts()
  const [adding, setAdding] = useState<'env' | 'project' | null>(null)
  const [editing, setEditing] = useState<{ kind: 'env' | 'project'; item: Item } | null>(null)
  // id=0 表示新增一条生命周期状态
  const [statusEdit, setStatusEdit] = useState<Item | null>(null)
  const [err, setErr] = useState<string>('')

  const delEnv = useDeleteEnv()
  const delProject = useDeleteProject()
  const delStatus = useDeleteStatus()

  const toLoadError = (e: unknown): LoadError => {
    const n = toErrorInfo(e)
    return { cause: tError(t, n.messageKey, n.params), detail: n.detail, retryable: n.retryable, kind: n.kind === 'license' ? 'license' : undefined, feature: n.params?.feature as string | undefined }
  }

  // 后端 409 那句话里带着"还有多少条在用"，是用户接下来的工作量。
  // 改写成"删除失败"就把最有用的部分丢了
  const onDelErr = (e: unknown) => {
    const n = toErrorInfo(e)
    // 先翻译 messageKey（后端给的是 code+参数），detail 只是兜底。
    // 反过来的话，"还有 18 条在用"会退化成 "DELETE /api/environments/1 → 409"
    const msg = tError(t, n.messageKey, n.params)
    setErr(msg === n.messageKey ? n.detail || t('basic:deleteFailed') : msg)
  }

  return (
    <div className="mx-auto max-w-[900px] p-5">
      <div className="mb-4 flex items-baseline gap-2.5">
        <h1 className="text-sm font-semibold text-foreground">{t('basic:title')}</h1>
        <p className="min-w-0 flex-1 text-xs text-muted-foreground">{t('basic:hint')}</p>
      </div>

      {err ? (
        <div className="mb-3">
          <Banner tone="warn">
            <span className="font-medium">{t('basic:cannotDelete')}</span>
            <span className="mt-0.5 block">{err}</span>
          </Banner>
        </div>
      ) : null}

      <AsyncBoundary
        state={fromQuery(query, () => false, toLoadError)}
        errorTitle={t('basic:error.title')}
        retryLabel={t('common:action.retry')}
        onRetry={() => void query.refetch()}
        pending={<Skeleton className="h-5 w-[60%]" />}
        empty={null}
      >
        {(d) => (
          <div className="flex flex-col gap-5">
            <DictBlock
              title={t('basic:section.envs')}
              desc={t('basic:desc.envs')}
              empty={t('basic:empty.envs')}
              // 带上 tag_type：一个"改了看不见效果"的设置比没有这个设置更糟，
              // 所以至少在设置它的这一页要能立刻看到结果
              items={d.envs.map((e) => ({
                id: e.id,
                code: e.code,
                name: e.name,
                name_en: e.name_en,
                tagType: e.tag_type ?? 'info',
              }))}
              onAdd={() => setAdding('env')}
              onEdit={(it) => setEditing({ kind: 'env', item: it })}
              onDelete={(id) => {
                setErr('')
                delEnv.mutate(id, { onError: onDelErr })
              }}
              addLabel={t('basic:action.addEnv')}
              t={t}
            />
            <DictBlock
              title={t('basic:section.projects')}
              desc={t('basic:desc.projects')}
              empty={t('basic:empty.projects')}
              // 业务项目没有 code，只有 name —— 两列显示同一个值反而误导
              // 备注跟着显示：项目名往往是缩写，备注才说得清它是什么。
              // 后端一直在返回 remark，前端此前零处渲染（OPSCMDB-038）
              items={d.projects.map((p) => ({
                id: p.id,
                code: '',
                name: p.name,
                remark: p.remark,
              }))}
              onAdd={() => setAdding('project')}
              onEdit={(it) => setEditing({ kind: 'project', item: it })}
              onDelete={(id) => {
                setErr('')
                delProject.mutate(id, { onError: onDelErr })
              }}
              addLabel={t('basic:action.addProject')}
              t={t}
            />
            {/* 下面两个是**系统内置**的，改了会让采集逻辑对不上，所以只读 */}
            <DictBlock
              title={t('basic:section.ciTypes')}
              desc={t('basic:desc.ciTypes')}
              empty={t('basic:empty.ciTypes')}
              items={d.ciTypes.map((c) => ({ id: c.id, code: c.code, name: c.name }))}
              readonlyNote={t('basic:builtinNote')}
              t={t}
            />
            <SettingsBlock t={t} />

            {/* ⚠️ 这一块原来标着「系统内置」而只读 —— 那个标注是**错的**：
                后端 lifecycle-statuses 的增删改三个接口都在，它本来就是可配置字典。
                一个被写死成只读的可配置项，比没有这个功能更难发现：
                页面上有一句"内置，不可改"的说明，看的人就不会再去想了。 */}
            <DictBlock
              title={t('basic:section.statuses')}
              desc={t('basic:desc.statuses')}
              empty={t('basic:empty.statuses')}
              // ⚠️ 这个接口给的是 scope + label，不是 code + name。
              // 照搬别的字典的字段名会渲染出一排空标签——不报错，就是空的
              items={(d.statuses ?? []).map((s) => ({ id: s.id, code: s.scope, name: s.label }))}
              onAdd={() => setStatusEdit({ id: 0, code: '', name: '' })}
              onEdit={(it) => setStatusEdit(it)}
              onDelete={(id) => {
                setErr('')
                delStatus.mutate(id, { onError: onDelErr })
              }}
              addLabel={t('basic:action.addStatus')}
              t={t}
            />
          </div>
        )}
      </AsyncBoundary>

      {adding ? (
        <AddDialog kind={adding} onClose={() => setAdding(null)} />
      ) : null}
      {editing ? (
        <EditDictDialog
          kind={editing.kind}
          item={editing.item}
          onClose={() => setEditing(null)}
        />
      ) : null}
      {statusEdit ? (
        <StatusDialog item={statusEdit} onClose={() => setStatusEdit(null)} />
      ) : null}
    </div>
  )
}

type T = (k: string, p?: Record<string, unknown>) => string

function DictBlock({
  title,
  desc,
  empty,
  items,
  onAdd,
  onEdit,
  onDelete,
  addLabel,
  readonlyNote,
  t,
}: {
  title: string
  desc: string
  empty: string
  items: Item[]
  onAdd?: () => void
  onEdit?: (it: Item) => void
  onDelete?: (id: number) => void
  addLabel?: string
  /** 内置字典：改了会让采集逻辑对不上，所以不给写入口 */
  readonlyNote?: string
  t: T
}) {
  // 只为拿 language：字典显示名要按语言选，而 t() 帮不上忙（那些名字是数据不是文案）
  const { i18n } = useTranslation()
  return (
    <section className="rounded-[var(--radius-lg)] border border-border p-4">
      <div className="flex items-baseline gap-2.5">
        <div className="text-xs font-medium text-foreground">{title}</div>
        <p className="min-w-0 flex-1 text-xs leading-relaxed text-muted-foreground">{desc}</p>
        {onAdd && addLabel ? (
          <WriteButton perm={PERM} size="sm" onClick={onAdd}>
            {addLabel}
          </WriteButton>
        ) : (
          <span className="shrink-0 text-[11px] text-muted-foreground">{readonlyNote}</span>
        )}
      </div>

      <div className="mt-2.5">
        {items.length === 0 ? (
          // 标题说"是什么状态"，原因说"会有什么后果"——两句一样等于只说了一半
          <EmptyState title={t('basic:emptyTitle')} reason={empty} action={null} />
        ) : (
          <div className="flex flex-wrap gap-1.5">
            {items.map((it) => (
              <span
                key={it.id}
                className="flex items-center gap-1.5 rounded-[var(--radius)] border border-border py-1 pr-1 pl-2 text-xs"
              >
                {/* 枚举值原样显示，中文名放旁边而不是替换掉它：
                    接口和 MCP 里用的都是 code，替换掉就对不上了 */}
                {it.code ? (
                  it.tagType ? (
                    <Badge tone={envTone(it.tagType)}>{it.code}</Badge>
                  ) : (
                    <code className="font-mono text-[11px] text-foreground">{it.code}</code>
                  )
                ) : null}
                {/* ⚠️ 按当前语言取显示名。
                    这些是**数据**不是文案（客户能自己增删改），语言包里没有它们，
                    所以英文界面下原来这一列全是中文（P1-76）。
                    没填英文名时 dictLabel 回退中文 —— 空白比中文更糟 */}
                <span className="text-muted-foreground">{dictLabel(it, i18n.language)}</span>
                {it.remark ? (
                  <span className="max-w-[180px] truncate text-[11px] text-muted-foreground" title={it.remark}>
                    {it.remark}
                  </span>
                ) : null}
                {/* ⚠️ 编辑入口以前没有：只能建和删。想改个名得先删再建，
                    而删除会被「还有 N 个资产在用」挡住 —— 于是这件事做不成 */}
                {onEdit ? (
                  <button
                    type="button"
                    onClick={() => onEdit(it)}
                    title={t('common:write.edit')}
                    aria-label={t('common:write.edit')}
                    className="cursor-pointer rounded p-0.5 text-muted-foreground hover:bg-secondary hover:text-foreground"
                  >
                    <Pencil className="size-3" aria-hidden="true" />
                  </button>
                ) : null}
                {onDelete ? (
                  <button
                    type="button"
                    onClick={() => onDelete(it.id)}
                    title={t('common:write.delete')}
                    aria-label={t('common:write.delete')}
                    className="cursor-pointer rounded p-0.5 text-muted-foreground hover:bg-secondary hover:text-danger"
                  >
                    <X className="size-3" aria-hidden="true" />
                  </button>
                ) : null}
              </span>
            ))}
          </div>
        )}
      </div>
    </section>
  )
}

function AddDialog({ kind, onClose }: { kind: 'env' | 'project'; onClose: () => void }) {
  const { t } = useTranslation()
  const [code, setCode] = useState('')
  const [name, setName] = useState('')
  // 徽章样式。默认 info（映射成中性灰）—— 和后端的默认值一致，
  // 两边默认值不同的话，不填这一项时新建出来的环境和界面预览对不上
  const [tagType, setTagType] = useState('info')
  const createEnv = useCreateEnv()
  const createProject = useCreateProject()
  const m = kind === 'env' ? createEnv : createProject

  return (
    <Dialog
      open
      onClose={onClose}
      title={t(kind === 'env' ? 'basic:action.addEnv' : 'basic:action.addProject')}
      description={t(kind === 'env' ? 'basic:dialog.envDesc' : 'basic:dialog.projectDesc')}
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
            loading={m.isPending}
            blockedReason={
              (kind === 'env' ? code && name : name) ? undefined : t('basic:field.required')
            }
            onClick={() =>
              kind === 'env'
                ? createEnv.mutate({ code, name, tag_type: tagType }, { onSuccess: onClose })
                : createProject.mutate({ name, remark: '' }, { onSuccess: onClose })
            }
          >
            {t('common:write.create')}
          </WriteButton>
        </>
      }
    >
      <div className="flex flex-col gap-3">
        {kind === 'env' ? (
          <Field label={t('basic:field.code')} hint={t('basic:field.codeHint')} required>
            <TextInput
              value={code}
              onChange={(e) => setCode(e.target.value.toUpperCase())}
              placeholder="STAGING"
              autoFocus
            />
          </Field>
        ) : null}
        <Field label={t('basic:field.name')} required>
          <TextInput
            value={name}
            onChange={(e) => setName(e.target.value)}
            autoFocus={kind === 'project'}
          />
        </Field>
        {/* 🔴 徽章样式。后端一直收着 tag_type、库里也一直有值，
            而新版界面既不显示也改不了 —— 环境徽章全被硬编码成中性灰，
            PROD 和 DEV 看起来一模一样（OPSCMDB-039）。
            生产标红不是装饰，它是「你正在动的是生产」这句话的视觉形式。 */}
        {kind === 'env' ? (
          <Field label={t('basic:field.tagType')} hint={t('basic:field.tagTypeHint')}>
            <div className="flex items-center gap-2">
              <Select<string>
                label={t('basic:field.tagType')}
                value={tagType}
                onChange={setTagType}
                options={ENV_TAG_TYPES.map((v) => ({
                  value: v,
                  label: t(`basic:tagType.${v}`),
                }))}
              />
              {/* 现场预览：颜色这种东西，看名字不如看一眼 */}
              <Badge tone={envTone(tagType)}>{code || t('basic:field.tagTypePreview')}</Badge>
            </div>
          </Field>
        ) : null}
        {m.isError ? (
          <MutationError error={m.error} toInfo={toErrorInfo} t={(k, p) => tError(t, k, p)} fallbackKey="basic:createFailed" />
        ) : null}
      </div>
    </Dialog>
  )
}


/**
 * 系统设置。
 *
 * 放在基础配置页而不是单独一个菜单：这些都是"设一次就不动"的东西，
 * 单独占一个菜单只会让人每次找它都要想一下在哪。
 */
function SettingsBlock({ t }: { t: T }) {
  const q = useSettings()
  const save = useSaveSettings()
  const [draft, setDraft] = useState<Record<string, string> | null>(null)
  const cur = draft ?? q.data ?? {}
  const set = (k: string, v: string) => setDraft({ ...cur, [k]: v })

  return (
    <section className="rounded-[var(--radius-lg)] border border-border p-4">
      <div className="flex items-baseline gap-2.5">
        <div className="text-xs font-medium text-foreground">{t('basic:section.settings')}</div>
        <p className="min-w-0 flex-1 text-xs text-muted-foreground">{t('basic:desc.settings')}</p>
        <WriteButton
          perm={PERM}
          size="sm"
          loading={save.isPending}
          blockedReason={draft ? undefined : t('basic:settings.noChange')}
          onClick={() => draft && save.mutate(draft, { onSuccess: () => setDraft(null) })}
        >
          {t('common:write.save')}
        </WriteButton>
      </div>

      {q.isPending ? (
        <Skeleton className="mt-2.5 h-5 w-[40%]" />
      ) : (
        <div className="mt-2.5 flex flex-col gap-3">
          <Field label={t('basic:settings.remindDays')} hint={t('basic:settings.remindDaysHint')}>
            <TextInput
              value={cur.remind_days ?? ''}
              onChange={(e) => set('remind_days', e.target.value)}
              placeholder="30,15,7,1"
            />
          </Field>
          <Field
            label={t('basic:settings.auditRetention')}
            hint={t('basic:settings.auditRetentionHint')}
          >
            <TextInput
              value={cur.audit_retention_days ?? ''}
              onChange={(e) => set('audit_retention_days', e.target.value)}
              inputMode="numeric"
              placeholder="0"
            />
          </Field>
          <Field label={t('basic:settings.webhook')} hint={t('basic:settings.webhookHint')}>
            <TextInput
              value={cur.feishu_webhook ?? ''}
              onChange={(e) => set('feishu_webhook', e.target.value)}
            />
          </Field>
        </div>
      )}

      {save.isError ? (
        <div className="mt-2">
          <MutationError error={save.error} toInfo={toErrorInfo} t={(k, p) => tError(t, k, p)} />
        </div>
      ) : null}
    </section>
  )
}

/** 编辑环境 / 项目。⚠️ code 是被别处引用的枚举值，改它要谨慎（提示里说明） */
function EditDictDialog({
  kind,
  item,
  onClose,
}: {
  kind: 'env' | 'project'
  item: Item
  onClose: () => void
}) {
  const { t } = useTranslation()
  const [code, setCode] = useState(item.code)
  const [name, setName] = useState(item.name)
  const [nameEn, setNameEn] = useState(item.name_en ?? '')
  const [tagType, setTagType] = useState(item.tagType ?? 'info')
  const updEnv = useUpdateEnv()
  const updProject = useUpdateProject()
  const m = kind === 'env' ? updEnv : updProject

  return (
    <Dialog
      open
      onClose={onClose}
      title={t(kind === 'env' ? 'basic:action.editEnv' : 'basic:action.editProject')}
      description={t(kind === 'env' ? 'basic:dialog.envDesc' : 'basic:dialog.projectDesc')}
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
            loading={m.isPending}
            blockedReason={
              (kind === 'env' ? code && name : name) ? undefined : t('basic:field.required')
            }
            onClick={() =>
              kind === 'env'
                ? updEnv.mutate(
                    { id: item.id, code, name, name_en: nameEn, tag_type: tagType },
                    { onSuccess: onClose },
                  )
                : updProject.mutate({ id: item.id, name, remark: '' }, { onSuccess: onClose })
            }
          >
            {t('common:action.save')}
          </WriteButton>
        </>
      }
    >
      <div className="flex flex-col gap-3">
        {kind === 'env' ? (
          <>
            {/* ⚠️ code 是接口和 MCP 里实际用的值，改掉之后旧的筛选链接会失效 */}
            <Field label={t('basic:field.code')} hint={t('basic:field.codeChangeHint')} required>
              <input value={code} onChange={(e) => setCode(e.target.value)} className={dictInput} />
            </Field>
            <Field label={t('basic:field.tagType')} hint={t('basic:field.tagTypeHint')}>
              <div className="flex items-center gap-2">
                <Select<string>
                  label={t('basic:field.tagType')}
                  value={tagType}
                  onChange={setTagType}
                  options={ENV_TAG_TYPES.map((v) => ({ value: v, label: t(`basic:tagType.${v}`) }))}
                />
                <Badge tone={envTone(tagType)}>{code || t('basic:field.tagTypePreview')}</Badge>
              </div>
            </Field>
          </>
        ) : null}
        <Field label={t('basic:field.name')} required>
          <input value={name} onChange={(e) => setName(e.target.value)} className={dictInput} />
        </Field>
        {/* ⚠️ 英文名要能填，否则这一列永远只有内置项有值。
            **不设为必填**：绝大多数客户只用一种语言，逼他们每个字典项填两遍
            是在为一个用不到的功能收税 —— 留空时英文界面回退显示中文名 */}
        {kind === 'env' ? (
          <Field label={t('basic:field.nameEn')} hint={t('basic:field.nameEnHint')}>
            <input value={nameEn} onChange={(e) => setNameEn(e.target.value)} className={dictInput} />
          </Field>
        ) : null}
        {m.isError ? (
          <MutationError error={m.error} toInfo={toErrorInfo} t={(k, p) => tError(t, k, p)} />
        ) : null}
      </div>
    </Dialog>
  )
}

/**
 * 生命周期状态的新增 / 编辑。
 *
 * ⚠️ 字段是 scope + label。scope 建后不可改 —— 它决定这个状态用在哪类对象上，
 * 改掉等于把一批已经打好的标签挪到别的类型下。
 */
function StatusDialog({ item, onClose }: { item: Item; onClose: () => void }) {
  const { t } = useTranslation()
  const creating = item.id === 0
  const [scope, setScope] = useState(item.code)
  const [label, setLabel] = useState(item.name)
  const create = useCreateStatus()
  const update = useUpdateStatus()
  const m = creating ? create : update

  return (
    <Dialog
      open
      onClose={onClose}
      title={t(creating ? 'basic:action.addStatus' : 'basic:action.editStatus')}
      description={t('basic:dialog.statusDesc')}
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
            loading={m.isPending}
            blockedReason={scope && label ? undefined : t('basic:field.required')}
            onClick={() =>
              creating
                ? create.mutate({ scope, label }, { onSuccess: onClose })
                : update.mutate({ id: item.id, label }, { onSuccess: onClose })
            }
          >
            {t(creating ? 'common:write.create' : 'common:action.save')}
          </WriteButton>
        </>
      }
    >
      <div className="flex flex-col gap-3">
        <Field label={t('basic:field.scope')} hint={t('basic:field.scopeHint')} required>
          <input
            value={scope}
            disabled={!creating}
            onChange={(e) => setScope(e.target.value)}
            placeholder="domain"
            className={dictInput}
          />
        </Field>
        <Field label={t('basic:field.label')} required>
          <input value={label} onChange={(e) => setLabel(e.target.value)} className={dictInput} />
        </Field>
        {m.isError ? (
          <MutationError error={m.error} toInfo={toErrorInfo} t={(k, p) => tError(t, k, p)} />
        ) : null}
      </div>
    </Dialog>
  )
}

const dictInput =
  'h-9 w-full rounded-[var(--radius)] border border-input bg-card px-2.5 text-[13px] ' +
  'text-foreground outline-none focus:border-primary focus:shadow-[0_0_0_2px_var(--ops-accent-ring)] ' +
  'disabled:cursor-not-allowed disabled:opacity-60'
