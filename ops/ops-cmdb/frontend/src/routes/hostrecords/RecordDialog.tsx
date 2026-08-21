import { toErrorInfo } from '@ops/api'
import { tError, useTranslation } from '@ops/i18n'
import { Banner, Dialog, Field, Select } from '@ops/ui'
import { useState } from 'react'
import { WriteButton } from '../../components/WriteButton.js'
import { useDomains } from '../domains/queries.js'
import { type HostRecord, type RecordInput, useCreateRecord, useUpdateRecord } from './queries.js'

const PERM = 'cmdb:manage_domains'

/** 与后端一致的生命周期枚举。⚠️ 原样照搬，不加"（生产）"这类解释性括号 */
const LIFE = ['', '使用中', '备用', '未使用', '待下线', '已下线']
const TYPES = ['A', 'AAAA', 'CNAME', 'TXT', 'MX']

/**
 * 新增 / 编辑主机头台账。
 *
 * ⚠️ 这是**台账**，不是 DNS。在这里填的东西不会写回任何解析服务商 ——
 * 它记录的是"这个主机头归谁、在哪个项目、还用不用"。
 * 真要改解析，去域名页的「解析记录」。这两件事分不清的话，
 * 人会在这里改完 origin_ip 然后等着它生效，而它永远不会。
 */
export function RecordDialog({
  initial,
  onClose,
}: {
  initial?: HostRecord
  onClose: () => void
}) {
  const { t } = useTranslation()
  const editing = initial != null
  const create = useCreateRecord()
  const update = useUpdateRecord()
  // 新增时要选挂在哪个域名下。取一页足够大的列表 —— 域名数量是几十的量级
  const domains = useDomains({ page: 1, size: 500, health: 'all' })
  const [ciId, setCiId] = useState(String(initial?.domain_ci_id ?? ''))
  const [v, setV] = useState<RecordInput>({
    host: initial?.host ?? '',
    record_type: initial?.record_type || 'A',
    cname: initial?.cname ?? '',
    origin_ip: initial?.origin_ip ?? '',
    cert_expiry_at: initial?.cert_expiry_at ?? '',
    project: initial?.project ?? '',
    env: initial?.env ?? '',
    module: initial?.module ?? '',
    life_status: initial?.life_status ?? '',
  })
  const busy = create.isPending || update.isPending
  const err = create.error ?? update.error
  const set = (k: keyof RecordInput) => (e: { target: { value: string } }) =>
    setV((p) => ({ ...p, [k]: e.target.value }))

  const blocked = !v.host.trim()
    ? t('hostrecords:form.needHost')
    : !editing && !ciId
      ? t('hostrecords:form.needDomain')
      : undefined

  return (
    <Dialog
      open
      onClose={onClose}
      title={t(editing ? 'hostrecords:form.editTitle' : 'hostrecords:form.createTitle')}
      description={t('hostrecords:form.desc')}
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
            loading={busy}
            blockedReason={blocked}
            onClick={() => {
              const done = { onSuccess: onClose }
              if (editing) update.mutate({ ...v, id: initial.id }, done)
              else create.mutate({ ...v, ciId: Number(ciId) }, done)
            }}
          >
            {t('common:action.save')}
          </WriteButton>
        </>
      }
    >
      <div className="flex flex-col gap-3">
        {/* 台账 ≠ DNS。这句话不写清楚，人会在这儿改 origin_ip 然后等它生效 */}
        <Banner tone="info">
          <span>{t('hostrecords:form.notDns')}</span>
        </Banner>

        {!editing ? (
          <Field label={t('hostrecords:form.domain')} hint={t('hostrecords:form.domainHint')}>
            <Select
              label={t('hostrecords:form.domain')}
              value={ciId}
              onChange={setCiId}
              options={(domains.data?.items ?? []).map((d) => ({
                value: String(d.ciId),
                label: d.name,
              }))}
            />
          </Field>
        ) : (
          <Field label={t('hostrecords:form.domain')}>
            <span className="text-[13px] text-muted-foreground">{initial.domain || '—'}</span>
          </Field>
        )}

        <div className="grid grid-cols-2 gap-3">
          <Field label={t('hostrecords:col.host')} hint={t('hostrecords:form.hostHint')}>
            <input value={v.host} onChange={set('host')} className={inputCls} placeholder="www" />
          </Field>
          <Field label={t('hostrecords:form.type')}>
            <Select
              label={t('hostrecords:form.type')}
              value={v.record_type ?? 'A'}
              onChange={(x) => setV((p) => ({ ...p, record_type: x }))}
              options={TYPES.map((x) => ({ value: x, label: x }))}
            />
          </Field>
          <Field label="CNAME">
            <input value={v.cname} onChange={set('cname')} className={inputCls} />
          </Field>
          <Field label={t('hostrecords:col.origin')} hint={t('hostrecords:form.originHint')}>
            <input value={v.origin_ip} onChange={set('origin_ip')} className={inputCls} />
          </Field>
          <Field label={t('hostrecords:col.project')}>
            <input value={v.project} onChange={set('project')} className={inputCls} />
          </Field>
          <Field label={t('hostrecords:form.env')}>
            <input value={v.env} onChange={set('env')} className={inputCls} />
          </Field>
          <Field label={t('hostrecords:col.module')}>
            <input value={v.module} onChange={set('module')} className={inputCls} />
          </Field>
          <Field label={t('hostrecords:col.state')}>
            <Select
              label={t('hostrecords:col.state')}
              value={v.life_status ?? ''}
              onChange={(x) => setV((p) => ({ ...p, life_status: x }))}
              options={LIFE.map((x) => ({
                value: x,
                label: x === '' ? t('hostrecords:form.lifeUnset') : x,
              }))}
            />
          </Field>
        </div>

        {/* ⚠️ 留空 = 不知道。这里填的是手工登记值，会被自动探测覆盖 */}
        <Field label={t('hostrecords:form.certExpiry')} hint={t('hostrecords:form.certExpiryHint')}>
          <input
            type="date"
            value={v.cert_expiry_at}
            onChange={set('cert_expiry_at')}
            className={inputCls}
          />
        </Field>

        {err ? (
          <Banner tone="bad">
            <span>{tError(t, toErrorInfo(err).messageKey, toErrorInfo(err).params)}</span>
            <span className="mt-0.5 block">{toErrorInfo(err).detail}</span>
          </Banner>
        ) : null}
      </div>
    </Dialog>
  )
}

const inputCls =
  'h-9 w-full rounded-[var(--radius)] border border-input bg-card px-2.5 text-[13px] ' +
  'text-foreground outline-none focus:border-primary focus:shadow-[0_0_0_2px_var(--ops-accent-ring)] ' +
  'disabled:cursor-not-allowed disabled:opacity-60'
