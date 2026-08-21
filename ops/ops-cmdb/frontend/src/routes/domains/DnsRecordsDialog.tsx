import { toErrorInfo } from '@ops/api'
import { tError, useTranslation } from '@ops/i18n'
import { Badge, Banner, Dialog, Field, Select, TextArea } from '@ops/ui'
import { Lock, Pencil, Trash2 } from 'lucide-react'
import { useMemo, useState } from 'react'
import { WriteButton } from '../../components/WriteButton.js'
import {
  type DnsRecord,
  type RecordInput,
  WRITABLE_TYPES,
  type WriteRes,
  parseRecordLine,
  useBatchCreateRecords,
  useBatchDeleteRecords,
  useBatchUpdateRecords,
  useCreateRecord,
  useDeleteRecord,
  useDnsRecords,
  useUpdateRecord,
} from './dnsQueries.js'
import type { Domain } from './queries.js'

const PERM = 'cmdb:manage_domains'

type Mode = 'list' | 'add' | 'bulkAdd' | 'bulkTtl'

/**
 * 一个域名的解析记录管理。
 *
 * # ⚠️ 受保护记录必须能看见、且必须不可点
 *
 * NS / SOA / _acme-challenge 后端会拒绝写回。如果界面把它们藏起来，
 * 人会以为"这个域名没有 NS 记录"；如果照常给出删除按钮，
 * 人会点了之后收到一个看不懂的 400。所以：**显示，标出来，禁用操作**。
 *
 * # ⚠️ 批量的"部分成功"是这一页最重要的状态
 *
 * 后端逐行校验，合法的写、非法的回报行号和原因。前端**必须逐行展示**——
 * 只显示一句"已提交"的话，粘 50 行成功 48 条，那 2 条就永远消失了。
 */
export function DnsRecordsDialog({ d, onClose }: { d: Domain; onClose: () => void }) {
  const { t } = useTranslation()
  const list = useDnsRecords(d.ciId, true)
  const [mode, setMode] = useState<Mode>('list')
  const [sel, setSel] = useState<Set<number>>(new Set())
  const [editing, setEditing] = useState<DnsRecord | null>(null)

  const create = useCreateRecord(d.ciId)
  const bulkCreate = useBatchCreateRecords(d.ciId)
  const update = useUpdateRecord(d.ciId)
  const del = useDeleteRecord(d.ciId)
  const bulkDel = useBatchDeleteRecords(d.ciId)
  const bulkUpd = useBatchUpdateRecords(d.ciId)

  const rows = list.data ?? []
  // 受保护的不能进选择集 —— 否则批量删除会带上它们，然后整批被后端拒
  const selectable = useMemo(() => rows.filter((r) => !r.protected), [rows])
  const allSelected = selectable.length > 0 && sel.size === selectable.length

  const toggle = (id: number) =>
    setSel((p) => {
      const n = new Set(p)
      if (!n.delete(id)) n.add(id)
      return n
    })

  const lastRes = [create, bulkCreate, update, del, bulkDel, bulkUpd].find(
    (m) => m.isSuccess || m.isError,
  )

  return (
    <Dialog
      open
      onClose={onClose}
      title={t('domains:dns.title', { domain: d.name })}
      description={t('domains:dns.desc')}
      closeLabel={t('common:action.close')}
      width={860}
      footer={
        <WriteButton perm={PERM} size="sm" onClick={onClose}>
          {t('common:action.close')}
        </WriteButton>
      }
    >
      <div className="flex flex-col gap-3">
        {/* 工具条。⚠️ 在列表外面 —— 一个域名解析记录为空时，
            正是最需要"新增"的时候，不能因为空态而消失 */}
        <div className="flex flex-wrap items-center gap-2">
          <WriteButton perm={PERM} size="sm" onClick={() => setMode('add')}>
            {t('domains:dns.add')}
          </WriteButton>
          <WriteButton perm={PERM} size="sm" onClick={() => setMode('bulkAdd')}>
            {t('domains:dns.bulkAdd')}
          </WriteButton>
          <span className="ml-auto text-xs text-muted-foreground">
            {t('domains:dns.selected', { count: sel.size })}
          </span>
          <WriteButton
            perm={PERM}
            size="sm"
            blockedReason={sel.size === 0 ? t('domains:dns.needSelect') : undefined}
            onClick={() => setMode('bulkTtl')}
          >
            {t('domains:dns.bulkTtl')}
          </WriteButton>
          <WriteButton
            perm={PERM}
            variant="danger"
            size="sm"
            loading={bulkDel.isPending}
            blockedReason={sel.size === 0 ? t('domains:dns.needSelect') : undefined}
            onClick={() => bulkDel.mutate([...sel], { onSuccess: () => setSel(new Set()) })}
          >
            {t('domains:dns.bulkDelete')}
          </WriteButton>
        </div>

        {mode === 'add' ? (
          <RecordForm
            onCancel={() => setMode('list')}
            busy={create.isPending}
            onSubmit={(v) => create.mutate(v, { onSuccess: () => setMode('list') })}
          />
        ) : null}

        {mode === 'bulkAdd' ? (
          <BulkAddForm
            onCancel={() => setMode('list')}
            busy={bulkCreate.isPending}
            onSubmit={(recs) => bulkCreate.mutate(recs, { onSuccess: () => setMode('list') })}
          />
        ) : null}

        {mode === 'bulkTtl' ? (
          <BulkTtlForm
            count={sel.size}
            onCancel={() => setMode('list')}
            busy={bulkUpd.isPending}
            onSubmit={(ttl) => {
              const recs = rows
                .filter((r) => sel.has(r.id))
                .map((r) => ({ id: r.id, data: r.data, ttl, priority: r.priority }))
              bulkUpd.mutate(recs, { onSuccess: () => setMode('list') })
            }}
          />
        ) : null}

        {editing ? (
          <RecordForm
            initial={editing}
            onCancel={() => setEditing(null)}
            busy={update.isPending}
            onSubmit={(v) =>
              update.mutate(
                { id: editing.id, data: v.data, ttl: v.ttl, priority: v.priority },
                { onSuccess: () => setEditing(null) },
              )
            }
          />
        ) : null}

        <ResultBanner res={lastRes} />

        {/* ── 列表 ── */}
        {list.isPending ? (
          <span className="text-[13px] text-muted-foreground">{t('common:state.loading')}</span>
        ) : list.isError ? (
          <Banner tone="bad">
            <span>{tError(t, toErrorInfo(list.error).messageKey, toErrorInfo(list.error).params)}</span>
            <span className="mt-0.5 block">{toErrorInfo(list.error).detail}</span>
          </Banner>
        ) : rows.length === 0 ? (
          // ⚠️ 空 ≠ 没同步过。这里说清楚两种可能，别让人以为域名真没解析
          <Banner tone="info">
            <span>{t('domains:dns.empty')}</span>
          </Banner>
        ) : (
          <div className="max-h-[46vh] overflow-auto rounded-[var(--radius)] border border-border">
            <table className="w-full text-[13px]">
              <thead className="sticky top-0 bg-card">
                <tr className="border-b border-border text-left text-xs text-muted-foreground">
                  <th className="w-8 px-2 py-2">
                    <input
                      type="checkbox"
                      aria-label={t('domains:dns.selectAll')}
                      checked={allSelected}
                      onChange={() =>
                        setSel(allSelected ? new Set() : new Set(selectable.map((r) => r.id)))
                      }
                    />
                  </th>
                  <th className="px-2 py-2">{t('domains:dns.col.type')}</th>
                  <th className="px-2 py-2">{t('domains:dns.col.name')}</th>
                  <th className="px-2 py-2">{t('domains:dns.col.data')}</th>
                  <th className="px-2 py-2">TTL</th>
                  <th className="px-2 py-2">{t('domains:dns.col.priority')}</th>
                  <th className="w-16 px-2 py-2" />
                </tr>
              </thead>
              <tbody>
                {rows.map((r) => (
                  <tr key={r.id} className="border-b border-border/60 last:border-0">
                    <td className="px-2 py-1.5">
                      {r.protected ? (
                        <Lock className="size-3.5 text-muted-foreground" />
                      ) : (
                        <input
                          type="checkbox"
                          aria-label={`${r.type} ${r.name}`}
                          checked={sel.has(r.id)}
                          onChange={() => toggle(r.id)}
                        />
                      )}
                    </td>
                    <td className="px-2 py-1.5">
                      <Badge tone="mute">{r.type}</Badge>
                    </td>
                    <td className="max-w-[160px] truncate px-2 py-1.5" title={r.name}>
                      {r.name}
                    </td>
                    <td className="max-w-[280px] truncate px-2 py-1.5 tabular" title={r.data}>
                      {r.data}
                    </td>
                    <td className="px-2 py-1.5 tabular">{r.ttl}</td>
                    {/* ⚠️ priority 为 null 是"该类型不适用"，显示 — 而不是 0 */}
                    <td className="px-2 py-1.5 tabular">{r.priority ?? '—'}</td>
                    <td className="px-2 py-1.5">
                      {r.protected ? (
                        // 受保护的显示原因而不是禁用的按钮：人要知道为什么不能改
                        <span className="text-xs text-muted-foreground" title={t('domains:dns.protectedWhy')}>
                          {t('domains:dns.protected')}
                        </span>
                      ) : (
                        <span className="flex gap-1">
                          <IconBtn label={t('common:action.edit')} onClick={() => setEditing(r)}>
                            <Pencil className="size-3.5" />
                          </IconBtn>
                          <IconBtn
                            label={t('common:action.delete')}
                            danger
                            onClick={() => del.mutate(r.id)}
                          >
                            <Trash2 className="size-3.5" />
                          </IconBtn>
                        </span>
                      )}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </div>
    </Dialog>
  )
}

/** 写回结果。⚠️ 逐行错误必须展开，"部分成功"不能压成一句"已提交" */
function ResultBanner({
  res,
}: {
  res?: { isSuccess: boolean; isError: boolean; data?: WriteRes; error?: unknown }
}) {
  const { t } = useTranslation()
  if (!res) return null
  if (res.isError) {
    const n = toErrorInfo(res.error)
    // 🔴 失败时也要显示逐行明细。
    //	后端在"全部行校验不通过"这类错误里附带了 errors 数组（哪一行为什么没做成），
    //	只显示一句"全部行校验不通过"，人下一个问题必然是"哪一行"。
    //	（成功路径下面已经在显示它了，失败路径一度漏了。）
    const failRows = (n.extra?.errors ?? []) as {
      row?: number
      id?: number
      detail?: string
      msg?: string
    }[]
    return (
      <Banner tone="bad">
        <span>{tError(t, n.messageKey, n.params)}</span>
        <span className="mt-0.5 block">{n.detail}</span>
        {failRows.length > 0 ? (
          <ul className="mt-1 flex flex-col gap-0.5">
            {failRows.map((e, i) => (
              <li key={`${e.row ?? e.id ?? i}`} className="text-xs">
                {e.row != null ? `#${e.row} ` : ''}
                {e.msg ?? e.detail}
              </li>
            ))}
          </ul>
        ) : null}
      </Banner>
    )
  }
  const d = res.data
  if (!d) return null
  const errs = d.errors ?? []
  return (
    <Banner tone={errs.length > 0 ? 'warn' : 'info'}>
      <span className="font-medium">{d.msg ?? t('common:write.saved')}</span>
      {d.dry_run ? (
        <span className="mt-0.5 block">{t('domains:dns.dryRunNote', { env: d.env ?? '' })}</span>
      ) : null}
      {errs.length > 0 ? (
        <ul className="mt-1 flex flex-col gap-0.5">
          {errs.map((e, i) => (
            <li key={`${e.row ?? e.id ?? i}`} className="text-xs">
              {e.row != null ? `#${e.row} ` : ''}
              {e.msg ?? e.detail}
            </li>
          ))}
        </ul>
      ) : null}
    </Banner>
  )
}

function IconBtn({
  children,
  label,
  danger,
  onClick,
}: {
  children: React.ReactNode
  label: string
  danger?: boolean
  onClick: () => void
}) {
  return (
    <button
      type="button"
      aria-label={label}
      title={label}
      onClick={onClick}
      className={`flex size-6 cursor-pointer items-center justify-center rounded-[var(--radius)] transition-colors duration-150 ${
        danger
          ? 'text-muted-foreground hover:bg-danger-bg hover:text-danger'
          : 'text-muted-foreground hover:bg-secondary hover:text-foreground'
      }`}
    >
      {children}
    </button>
  )
}

/** 单条新增 / 编辑。编辑时类型和主机名锁死 —— 后端只支持改值/TTL/优先级 */
function RecordForm({
  initial,
  onCancel,
  onSubmit,
  busy,
}: {
  initial?: DnsRecord
  onCancel: () => void
  onSubmit: (v: RecordInput) => void
  busy: boolean
}) {
  const { t } = useTranslation()
  const [v, setV] = useState<RecordInput>({
    type: initial?.type ?? 'A',
    name: initial?.name ?? '@',
    data: initial?.data ?? '',
    ttl: initial?.ttl ?? 600,
    priority: initial?.priority ?? undefined,
  })
  const locked = initial != null

  return (
    <div className="flex flex-col gap-3 rounded-[var(--radius)] border border-border bg-secondary/40 p-3">
      <div className="grid grid-cols-4 gap-3">
        <Field label={t('domains:dns.col.type')}>
          {/* 编辑时类型不可改：后端只支持改值/TTL/优先级。
              这里渲染成静态文本而不是禁用的下拉 —— 禁用的下拉看着像"暂时不能改"，
              静态文本才是"这个字段就是不能改" */}
          {locked ? (
            <div className="flex h-9 items-center">
              <Badge tone="mute">{v.type}</Badge>
            </div>
          ) : (
            <Select
              label={t('domains:dns.col.type')}
              value={v.type}
              onChange={(x) => setV((p) => ({ ...p, type: x }))}
              options={WRITABLE_TYPES.map((x) => ({ value: x, label: x }))}
            />
          )}
        </Field>
        <Field label={t('domains:dns.col.name')} hint={locked ? undefined : t('domains:dns.nameHint')}>
          <input
            value={v.name}
            disabled={locked}
            onChange={(e) => setV((p) => ({ ...p, name: e.target.value }))}
            className={inputCls}
          />
        </Field>
        <Field label="TTL" hint={t('domains:dns.ttlHint')}>
          <input
            type="number"
            value={v.ttl}
            onChange={(e) => setV((p) => ({ ...p, ttl: Number(e.target.value) }))}
            className={inputCls}
          />
        </Field>
        {v.type === 'MX' ? (
          <Field label={t('domains:dns.col.priority')}>
            <input
              type="number"
              value={v.priority ?? 10}
              onChange={(e) => setV((p) => ({ ...p, priority: Number(e.target.value) }))}
              className={inputCls}
            />
          </Field>
        ) : null}
      </div>
      <Field label={t('domains:dns.col.data')}>
        <input
          value={v.data}
          onChange={(e) => setV((p) => ({ ...p, data: e.target.value }))}
          className={inputCls}
        />
      </Field>
      <div className="flex gap-2">
        <WriteButton perm={PERM} size="sm" onClick={onCancel}>
          {t('common:action.cancel')}
        </WriteButton>
        <WriteButton
          perm={PERM}
          variant="primary"
          size="sm"
          loading={busy}
          blockedReason={v.data.trim() ? undefined : t('domains:dns.needData')}
          onClick={() => onSubmit(v)}
        >
          {t('common:action.save')}
        </WriteButton>
      </div>
    </div>
  )
}

/** 批量新增：一行一条。解析不出来的行**当场标出来**，不提交、也不静默跳过 */
function BulkAddForm({
  onCancel,
  onSubmit,
  busy,
}: {
  onCancel: () => void
  onSubmit: (v: RecordInput[]) => void
  busy: boolean
}) {
  const { t } = useTranslation()
  const [text, setText] = useState('')
  const lines = text.split('\n').map((l) => l.trim()).filter(Boolean)
  const parsed = lines.map((l, i) => ({ i: i + 1, line: l, out: parseRecordLine(l) }))
  const bad = parsed.filter((p) => 'error' in p.out)
  const good = parsed.filter((p) => !('error' in p.out)).map((p) => p.out as RecordInput)

  return (
    <div className="flex flex-col gap-3 rounded-[var(--radius)] border border-border bg-secondary/40 p-3">
      <Field label={t('domains:dns.bulkAdd')} hint={t('domains:dns.bulkHint')}>
        <TextArea
          value={text}
          onChange={(e) => setText(e.target.value)}
          rows={6}
          placeholder={'A  www      10.0.0.1   600\nCNAME api   www.example.com\nMX @        mail.example.com  600  10'}
        />
      </Field>
      {/* ⚠️ 坏行在提交前就摆出来。粘 50 行成功 48 条、界面只说"成功"，
          剩下 2 条就永远消失了 —— 这是批量入口最经典的坑 */}
      {bad.length > 0 ? (
        <Banner tone="bad">
          <span className="font-medium">{t('domains:dns.badLines', { count: bad.length })}</span>
          <ul className="mt-1 flex flex-col gap-0.5">
            {bad.map((p) => (
              <li key={p.i} className="text-xs">
                #{p.i} {p.line} — {(p.out as { error: string }).error}
              </li>
            ))}
          </ul>
        </Banner>
      ) : null}
      <div className="flex items-center gap-2">
        <span className="text-xs text-muted-foreground">
          {t('domains:dns.willAdd', { count: good.length })}
        </span>
        <WriteButton perm={PERM} size="sm" className="ml-auto" onClick={onCancel}>
          {t('common:action.cancel')}
        </WriteButton>
        <WriteButton
          perm={PERM}
          variant="primary"
          size="sm"
          loading={busy}
          blockedReason={
            good.length === 0
              ? t('domains:dns.needLines')
              : bad.length > 0
                ? t('domains:dns.fixBadFirst')
                : undefined
          }
          onClick={() => onSubmit(good)}
        >
          {t('common:action.save')}
        </WriteButton>
      </div>
    </div>
  )
}

/** 批量改 TTL —— 选中记录的值和优先级原样带过去，只动 TTL */
function BulkTtlForm({
  count,
  onCancel,
  onSubmit,
  busy,
}: {
  count: number
  onCancel: () => void
  onSubmit: (ttl: number) => void
  busy: boolean
}) {
  const { t } = useTranslation()
  const [ttl, setTtl] = useState(600)
  return (
    <div className="flex flex-wrap items-end gap-3 rounded-[var(--radius)] border border-border bg-secondary/40 p-3">
      <Field label={t('domains:dns.bulkTtl')} hint={t('domains:dns.ttlHint')}>
        <input
          type="number"
          value={ttl}
          onChange={(e) => setTtl(Number(e.target.value))}
          className={inputCls}
        />
      </Field>
      <span className="pb-1.5 text-xs text-muted-foreground">
        {t('domains:dns.willUpdate', { count })}
      </span>
      <WriteButton perm={PERM} size="sm" className="ml-auto" onClick={onCancel}>
        {t('common:action.cancel')}
      </WriteButton>
      <WriteButton perm={PERM} variant="primary" size="sm" loading={busy} onClick={() => onSubmit(ttl)}>
        {t('common:action.save')}
      </WriteButton>
    </div>
  )
}

const inputCls =
  'h-9 w-full rounded-[var(--radius)] border border-input bg-card px-2.5 text-[13px] ' +
  'text-foreground outline-none focus:border-primary focus:shadow-[0_0_0_2px_var(--ops-accent-ring)] ' +
  'disabled:cursor-not-allowed disabled:opacity-60'
