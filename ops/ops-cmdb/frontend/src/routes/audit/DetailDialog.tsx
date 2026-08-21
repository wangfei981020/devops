import { toErrorInfo } from '@ops/api'
import { tError, useTranslation } from '@ops/i18n'
import { Badge, Banner, Dialog, Skeleton } from '@ops/ui'
import { useState } from 'react'
import { WriteButton } from '../../components/WriteButton.js'
import { type AuditChange, type AuditLog, useAuditChanges, useRevertChange } from './queries.js'

const PERM = 'cmdb:revert_change'

/**
 * 一条审计记录的完整详情：谁、在哪、干了什么、改前改后、结果如何。
 *
 * # 为什么每一行都要能点开
 *
 * 这个弹窗原来叫 ChangesDialog，**只有 `change_count > 0` 时才可点** ——
 * 于是所有动作型操作（同步、续费、测连通、登录、MCP 调用）在列表上
 * 是一个死的「—」，点不动。
 *
 * 而后端其实每条都记了 method / path / perm_code / duration_ms / trace_id /
 * actor_source / error_msg 这一整套，前端一个都没显示。
 * 「看不到详情」不是没记，是记了没拿出来。
 *
 * ⚠️ **没有行快照 ≠ 没有详情**。一次域名同步的详情是
 * 「谁在什么时候、凭哪个权限码、调了哪个接口、耗时多久、结果如何」；
 * 一次续费的详情还包括订单号和金额（记在 target 里）。
 * 把这些藏起来，等于让人对着一行「source.sync success」发呆。
 */
export function DetailDialog({ log, onClose }: { log: AuditLog; onClose: () => void }) {
  const { t } = useTranslation()
  // change_count 为 0 时不必去查变更 —— 那一趟必然返回空数组
  const q = useAuditChanges(log.change_count > 0 ? log.id : null)
  const revert = useRevertChange()
  const [confirming, setConfirming] = useState<AuditChange | null>(null)

  const list = q.data?.list ?? []

  return (
    <Dialog
      open
      onClose={onClose}
      title={t('audit:detail.title')}
      description={log.action}
      closeLabel={t('common:action.close')}
      width={820}
      footer={
        <WriteButton perm={PERM} size="sm" onClick={onClose}>
          {t('common:action.close')}
        </WriteButton>
      }
    >
      <div className="flex flex-col gap-4">
        {/* ── 这次操作 ── */}
        <section className="flex flex-col gap-2">
          <h3 className="text-xs font-medium text-foreground">{t('audit:detail.whatHappened')}</h3>
          <div className="grid grid-cols-2 gap-x-4 gap-y-1.5 rounded-[var(--radius)] border border-border bg-secondary/40 p-3">
            <Row label={t('audit:column.time')} value={log.at} mono />
            <Row
              label={t('audit:column.actor')}
              value={
                <span className="flex items-center gap-1.5">
                  <span>{log.username || t('audit:detail.noActor')}</span>
                  {/* 来源要显式标出来：同一个用户名，从界面点的和 MCP 调的
                      是两回事，出事时的追查方向完全不同 */}
                  <Badge tone="mute">{log.actor_source || 'local'}</Badge>
                </span>
              }
            />
            <Row label="IP" value={log.ip || '—'} mono />
            <Row
              label={t('audit:detail.duration')}
              value={log.duration_ms != null ? `${log.duration_ms} ms` : '—'}
              mono
            />
            <Row
              label={t('audit:detail.endpoint')}
              value={log.method || log.path ? `${log.method} ${log.path}` : '—'}
              mono
              span
            />
            <Row
              label={t('audit:detail.permCode')}
              // 凭哪个权限码放行的 —— 事后反查"这个权限是不是给宽了"
              value={log.perm_code || t('audit:detail.noPerm')}
              mono
            />
            <Row
              label={t('audit:column.status')}
              value={<StatusBadge status={log.status} t={t} />}
            />
          </div>

          {/* 失败原因单独一条横幅：它是这一屏最重要的信息，不该挤在网格里 */}
          {log.error_msg ? (
            <Banner tone="bad">
              <span className="font-medium">{t('audit:detail.errorTitle')}</span>
              <span className="mt-0.5 block break-all">{log.error_msg}</span>
            </Banner>
          ) : null}

          {/* ⚠️ accepted 要在详情里再说一次去哪看真结果 ——
              这正是当初 401 被绿色盖住的那个场景 */}
          {log.status === 'accepted' ? (
            <Banner tone="info">
              <span>{t('audit:detail.acceptedBody')}</span>
            </Banner>
          ) : null}
        </section>

        {/* ── 操作对象 ── */}
        <section className="flex flex-col gap-2">
          <h3 className="text-xs font-medium text-foreground">{t('audit:detail.target')}</h3>
          <div className="grid grid-cols-2 gap-x-4 gap-y-1.5 rounded-[var(--radius)] border border-border bg-secondary/40 p-3">
            <Row label={t('audit:detail.targetType')} value={log.target_type || '—'} mono />
            {/* target 里常常带着关键信息（域名、订单号、年数），要完整显示不截断 */}
            <Row label={t('audit:column.target')} value={log.target || '—'} mono span />
          </div>
        </section>

        {/* ── 改了什么 ── */}
        <section className="flex flex-col gap-2">
          <h3 className="text-xs font-medium text-foreground">{t('audit:detail.changes')}</h3>

          {log.change_count === 0 ? (
            // ⚠️ 空白会被读成"没改动"。这里要说清楚是哪一种：
            // 动作型操作本来就不改单行（同步、测连通），或者改的是外部系统。
            <Banner tone="info">
              <span>{t('audit:detail.noSnapshot')}</span>
            </Banner>
          ) : q.isPending ? (
            <Skeleton className="h-5 w-[50%]" />
          ) : q.isError ? (
            <Banner tone="bad">
              <span>{tError(t, toErrorInfo(q.error).messageKey, toErrorInfo(q.error).params)}</span>
              <span className="mt-0.5 block">{toErrorInfo(q.error).detail}</span>
            </Banner>
          ) : list.length === 0 ? (
            // 计数说有、明细取不到：这是不一致，不能显示成"没有改动"
            <Banner tone="warn">
              <span>{t('audit:detail.countMismatch', { count: log.change_count })}</span>
            </Banner>
          ) : (
            <div className="flex flex-col gap-3">
              {/* 🔴 主文案走 message_key（可翻译），`detail` 只作技术细节的补充。
                  原来直接渲染 `detail` —— 那是后端拼好的**中文**句子，
                  英文界面下就是一句中文，而且只在回滚失败时才出现（OPSCMDB-054）。
                  ⚠️ hint 要一起显示：回滚被拒时那句话写着唯一的出路
                  （「去 DNS 记录页按原值手动改回」），丢了就只剩"失败了"。 */}
              {revert.isError ? (
                <RevertError err={revert.error} t={t} />
              ) : null}
              {revert.isSuccess ? (
                <Banner tone="info">
                  <span>{t('audit:changes.reverted')}</span>
                </Banner>
              ) : null}

              {list.map((ch) => (
                <ChangeCard
                  key={ch.id}
                  ch={ch}
                  t={t}
                  reverting={revert.isPending && confirming?.id === ch.id}
                  onRevert={() => setConfirming(ch)}
                />
              ))}
            </div>
          )}
        </section>
      </div>

      {confirming ? (
        <ConfirmRevert
          change={confirming}
          onClose={() => setConfirming(null)}
          onConfirm={(force) =>
            revert.mutate(
              { cid: confirming.id, force },
              { onSuccess: () => setConfirming(null) },
            )
          }
          pending={revert.isPending}
        />
      ) : null}
    </Dialog>
  )
}

type T = (k: string, p?: Record<string, unknown>) => string

function StatusBadge({ status, t }: { status: string; t: T }) {
  if (status === 'success') return <Badge tone="ok">{status}</Badge>
  if (status === 'accepted') return <Badge tone="info">{status}</Badge>
  return <Badge tone="bad">{status || t('common:state.unknown')}</Badge>
}

function Row({
  label,
  value,
  mono,
  span,
}: {
  label: string
  value: React.ReactNode
  mono?: boolean
  span?: boolean
}) {
  return (
    <div className={`flex min-w-0 flex-col gap-0.5 ${span ? 'col-span-2' : ''}`}>
      <span className="text-[11px] text-muted-foreground">{label}</span>
      <span className={`text-[13px] break-all text-foreground ${mono ? 'font-mono text-xs' : ''}`}>
        {value}
      </span>
    </div>
  )
}

/** 一条行级变更：表、主键、操作、字段前后值，以及回滚入口 */
function ChangeCard({
  ch,
  t,
  reverting,
  onRevert,
}: {
  ch: AuditChange
  t: T
  reverting: boolean
  onRevert: () => void
}) {
  const fields = Object.entries(ch.diff ?? {})
  return (
    <div className="rounded-[var(--radius)] border border-border p-3">
      <div className="flex flex-wrap items-center gap-2 text-[13px]">
        <code className="font-mono text-xs">{ch.table}</code>
        <Badge tone="mute">{ch.op}</Badge>
        <code className="font-mono text-[11px] text-muted-foreground">#{ch.row_pk}</code>
        <div className="ml-auto flex items-center gap-2">
          {/* 不可回滚：原因直接摆出来，不藏在 title 里 —— 一个灰着的按钮
              不给理由，人会以为是权限问题去找管理员 */}
          {!ch.revertable ? (
            <span className="max-w-[380px] text-[11px] text-warning">
              {ch.revert_blocked_reason}
            </span>
          ) : null}
          <WriteButton
            perm={PERM}
            size="sm"
            variant="danger"
            loading={reverting}
            blockedReason={ch.revertable ? undefined : ch.revert_blocked_reason}
            onClick={onRevert}
          >
            {t('audit:changes.revert')}
          </WriteButton>
        </div>
      </div>

      {/* 字段级前后值。⚠️ 只显示"改了 3 个字段"等于没说 */}
      {fields.length > 0 ? (
        <div className="mt-2 overflow-x-auto">
          <table className="w-full text-[11px]">
            <thead>
              <tr className="text-left text-muted-foreground">
                <th className="w-[180px] py-1 font-normal">{t('audit:detail.field')}</th>
                <th className="py-1 font-normal">{t('audit:detail.before')}</th>
                <th className="w-6" />
                <th className="py-1 font-normal">{t('audit:detail.after')}</th>
              </tr>
            </thead>
            <tbody>
              {fields.map(([field, v]) => (
                <tr key={field} className="align-top">
                  <td className="py-0.5 pr-2">
                    <code className="font-mono text-muted-foreground">{field}</code>
                  </td>
                  {/* 值完整显示、可换行：截断的旧值没法用来核对 */}
                  {/* ⚠️ 键名是 old/new 不是 before/after；且敏感字段只给 changed:true。
                      三种形态见 queries.ts 的注释 */}
                  {v?.changed ? (
                    <td className="py-0.5 text-warning" colSpan={3}>
                      {t('audit:detail.maskedChanged')}
                    </td>
                  ) : (
                    <>
                      <td className="py-0.5 pr-2 break-all text-danger line-through">
                        {fmt(v?.old)}
                      </td>
                      <td className="py-0.5 text-muted-foreground">→</td>
                      <td className="py-0.5 break-all text-success">{fmt(v?.new)}</td>
                    </>
                  )}
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      ) : (
        // 有这条变更但 diff 为空：说明快照没拍上，不是"什么都没改"
        <p className="mt-2 text-[11px] text-warning">{t('audit:detail.noDiff')}</p>
      )}
    </div>
  )
}

/** ⚠️ 空字符串和 null 要分开：一个是"改成了空"，一个是"这个字段没有值" */
function fmt(v: unknown): string {
  if (v === null || v === undefined) return '—'
  if (typeof v === 'string') return v === '' ? '(空)' : v
  return JSON.stringify(v)
}

function ConfirmRevert({
  change,
  onClose,
  onConfirm,
  pending,
}: {
  change: AuditChange
  onClose: () => void
  onConfirm: (force: boolean) => void
  pending: boolean
}) {
  const { t } = useTranslation()
  const [force, setForce] = useState(false)
  return (
    <Dialog
      open
      onClose={onClose}
      title={t('audit:changes.confirmTitle')}
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
            loading={pending}
            onClick={() => onConfirm(force)}
          >
            {t('audit:changes.revert')}
          </WriteButton>
        </>
      }
    >
      <div className="flex flex-col gap-3">
        <p className="text-[13px] leading-relaxed text-foreground">
          {t('audit:changes.confirmBody', { table: change.table, pk: change.row_pk })}
        </p>
        {/* force 不是"更保险的重试"，是"我知道别人改过、仍然覆盖"。
            所以必须由用户主动勾，而不是失败后自动带上 */}
        <label className="flex items-start gap-2 text-xs text-muted-foreground">
          <input
            type="checkbox"
            checked={force}
            onChange={(e) => setForce(e.target.checked)}
            className="mt-0.5"
          />
          <span>{t('audit:changes.forceHint')}</span>
        </label>
      </div>
    </Dialog>
  )
}

/**
 * 回滚失败的提示：标题 + 主文案 + 出路。
 *
 * ⚠️ 三段都要有主文案兜底 —— 后端还没迁完的接口只发中文 `error`，
 *	那时 tError 会回退到它（见 @ops/i18n 的 tError）。
 */
function RevertError({
  err,
  t,
}: {
  err: unknown
  t: (k: string, p?: Record<string, unknown>) => string
}) {
  const info = toErrorInfo(err)
  const extra = info.extra as { hint_key?: string; hint?: string } | undefined
  const hint = extra?.hint_key ? t(extra.hint_key) : (extra?.hint ?? '')
  return (
    <Banner tone="bad">
      <span className="font-medium">{t('audit:changes.revertFailed')}</span>
      <span className="mt-0.5 block">{tError(t, info.messageKey, info.params) || info.detail}</span>
      {hint !== '' ? <span className="mt-0.5 block text-xs opacity-90">{hint}</span> : null}
    </Banner>
  )
}
