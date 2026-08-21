import { toErrorInfo } from '@ops/api'
import { tError, useTranslation } from '@ops/i18n'
import { Banner, Dialog, Field, MenuItem, MenuSeparator, MutationError, Popover, TextArea } from '@ops/ui'
import { BellOff, MoreHorizontal, Pencil, ShieldCheck, Trash2 } from 'lucide-react'
import { useState } from 'react'
import { WriteButton } from '../../components/WriteButton.js'
import { RecordDialog } from './RecordDialog.js'
import {
  type HostRecord,
  useCheckRecordCert,
  useDeleteRecord,
  useIgnoreRecordCert,
} from './queries.js'

const PERM = 'cmdb:manage_domains'

type Open = null | 'edit' | 'delete' | 'ignore'

/** 单条主机头的操作。检测证书就地跑，编辑/删除/忽略走弹窗。 */
export function RecordRowActions({ r }: { r: HostRecord }) {
  const { t } = useTranslation()
  const [open, setOpen] = useState<Open>(null)
  const check = useCheckRecordCert()
  const del = useDeleteRecord()

  return (
    <div className="flex items-center justify-end gap-2">
      {/* ⚠️ 探测结果必须区分三态：还没探 / 探到了 / 探不到。
          后端探不到时返回的是 HTTP 200 + ok:false —— 只看 HTTP 状态
          会把"连不上"渲染成成功，那正是这套系统要根治的病 */}
      {check.isPending ? (
        <span className="text-xs text-muted-foreground">{t('common:state.loading')}</span>
      ) : check.isError ? (
        <span className="max-w-[180px] truncate text-xs text-danger" title={toErrorInfo(check.error).detail || undefined}>
          {tError(t, toErrorInfo(check.error).messageKey, toErrorInfo(check.error).params)}
        </span>
      ) : check.data ? (
        check.data.ok ? (
          <span className="text-xs text-success">
            {check.data.cert_expiry_at}
            {check.data.warn ? ` · ${check.data.warn}` : ''}
          </span>
        ) : (
          <span className="max-w-[180px] truncate text-xs text-danger" title={check.data.msg}>
            {check.data.msg ?? t('hostrecords:cert.checkFailed')}
          </span>
        )
      ) : null}

      <Popover
        align="end"
        trigger={(p) => (
          <button
            type="button"
            {...p}
            aria-label={t('common:action.more')}
            className="flex size-7 cursor-pointer items-center justify-center rounded-[var(--radius)] text-muted-foreground transition-colors duration-150 hover:bg-secondary hover:text-foreground"
          >
            <MoreHorizontal className="size-4" />
          </button>
        )}
      >
        <div className="min-w-[176px] py-1">
          <MenuItem icon={<Pencil />} onClick={() => setOpen('edit')}>
            {t('common:action.edit')}
          </MenuItem>
          <MenuItem icon={<ShieldCheck />} onClick={() => check.mutate(r.id)}>
            {t('hostrecords:cert.check')}
          </MenuItem>
          <MenuItem icon={<BellOff />} onClick={() => setOpen('ignore')}>
            {t(r.ignored ? 'hostrecords:cert.unignore' : 'hostrecords:cert.ignore')}
          </MenuItem>
          <MenuSeparator />
          <MenuItem icon={<Trash2 />} danger onClick={() => setOpen('delete')}>
            {t('common:action.delete')}
          </MenuItem>
        </div>
      </Popover>

      {open === 'edit' ? <RecordDialog initial={r} onClose={() => setOpen(null)} /> : null}
      {open === 'ignore' ? <IgnoreDialog r={r} onClose={() => setOpen(null)} /> : null}
      {open === 'delete' ? (
        <Dialog
          open
          onClose={() => setOpen(null)}
          title={t('hostrecords:del.title')}
          description={r.fqdn}
          closeLabel={t('common:action.close')}
          width={460}
          footer={
            <>
              <WriteButton perm={PERM} size="sm" onClick={() => setOpen(null)}>
                {t('common:action.cancel')}
              </WriteButton>
              <WriteButton
                perm={PERM}
                variant="danger"
                size="sm"
                loading={del.isPending}
                onClick={() => del.mutate(r.id, { onSuccess: () => setOpen(null) })}
              >
                {t('common:action.delete')}
              </WriteButton>
            </>
          }
        >
          {/* 删台账 ≠ 删解析。真正的解析还在，下次同步这条多半会回来 */}
          <Banner tone="warn">
            <span>{t('hostrecords:del.note')}</span>
          </Banner>
          {del.isError ? (
            <MutationError error={del.error} toInfo={toErrorInfo} t={(k, p) => tError(t, k, p)} />
          ) : null}
        </Dialog>
      ) : null}
    </div>
  )
}

/**
 * 证书忽略。
 *
 * ⚠️ 开启忽略**必须填理由**，这是硬性的：忽略之后到期巡检不再报它，
 * 半年后没人记得当初为什么忽略，而那条理由往往已经不成立了。
 * 取消忽略不需要理由 —— 恢复告警没有风险。
 */
function IgnoreDialog({ r, onClose }: { r: HostRecord; onClose: () => void }) {
  const { t } = useTranslation()
  const ignore = useIgnoreRecordCert()
  const turningOn = !r.ignored
  const [reason, setReason] = useState(r.ignore_reason ?? '')

  return (
    <Dialog
      open
      onClose={onClose}
      title={t(turningOn ? 'hostrecords:cert.ignoreTitle' : 'hostrecords:cert.unignoreTitle')}
      description={r.fqdn}
      closeLabel={t('common:action.close')}
      width={480}
      footer={
        <>
          <WriteButton perm={PERM} size="sm" onClick={onClose}>
            {t('common:action.cancel')}
          </WriteButton>
          <WriteButton
            perm={PERM}
            variant="primary"
            size="sm"
            loading={ignore.isPending}
            blockedReason={
              turningOn && !reason.trim() ? t('hostrecords:cert.needReason') : undefined
            }
            onClick={() =>
              ignore.mutate(
                { id: r.id, ignored: turningOn, reason: reason.trim() },
                { onSuccess: onClose },
              )
            }
          >
            {t('common:action.save')}
          </WriteButton>
        </>
      }
    >
      <div className="flex flex-col gap-3">
        <Banner tone="warn">
          <span>
            {t(turningOn ? 'hostrecords:cert.ignoreNote' : 'hostrecords:cert.unignoreNote')}
          </span>
        </Banner>
        {turningOn ? (
          <Field label={t('hostrecords:cert.reason')} hint={t('hostrecords:cert.reasonHint')}>
            <TextArea value={reason} onChange={(e) => setReason(e.target.value)} rows={3} />
          </Field>
        ) : null}
        {ignore.isError ? (
          <MutationError error={ignore.error} toInfo={toErrorInfo} t={(k, p) => tError(t, k, p)} />
        ) : null}
      </div>
    </Dialog>
  )
}
