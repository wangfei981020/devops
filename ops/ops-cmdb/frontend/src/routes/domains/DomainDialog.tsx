import { toErrorInfo } from '@ops/api'
import { tError, useTranslation } from '@ops/i18n'
import { Banner, Dialog, Field } from '@ops/ui'
import { useState } from 'react'
import { WriteButton } from '../../components/WriteButton.js'
import { type DomainInput, useCreateDomain, useUpdateDomain } from './queries.js'

const PERM = 'cmdb:manage_domains'

/**
 * 新增 / 编辑域名。
 *
 * ⚠️ 这个入口以前**根本不存在**：后端 `POST /api/domains` 和 `PUT /api/domains/:ciid`
 * 一直在，权限码配了、审计也登记了，前端一处没调。
 * 于是"手工录一个注册商同步不到的域名"这件事做不了 —— 而页面上看不出任何异常。
 *
 * ⚠️ 到期日**允许留空**。空 = 不知道，不是"今天到期"。
 * 列表页对 null 有专门的渲染（「未知」而不是「还剩 0 天」），
 * 这里绝不能为了"字段完整"塞一个今天的日期进去。
 */
export function DomainDialog({
  initial,
  onClose,
}: {
  /** 传 undefined = 新增；传对象 = 编辑 */
  initial?: DomainInput & { ciId: number }
  onClose: () => void
}) {
  const { t } = useTranslation()
  const editing = initial != null
  const [v, setV] = useState<DomainInput>({
    name: initial?.name ?? '',
    project: initial?.project ?? '',
    env: initial?.env ?? '',
    module: initial?.module ?? '',
    owner: initial?.owner ?? '',
    expiry_at: initial?.expiry_at ?? '',
  })
  const create = useCreateDomain()
  const update = useUpdateDomain()
  const busy = create.isPending || update.isPending
  const err = create.error ?? update.error

  const set = (k: keyof DomainInput) => (e: { target: { value: string } }) =>
    setV((p) => ({ ...p, [k]: e.target.value }))

  function submit() {
    const done = { onSuccess: onClose }
    if (editing) update.mutate({ ...v, ciId: initial.ciId }, done)
    else create.mutate(v, done)
  }

  return (
    <Dialog
      open
      onClose={onClose}
      title={t(editing ? 'domains:form.editTitle' : 'domains:form.createTitle')}
      description={t('domains:form.desc')}
      closeLabel={t('common:action.close')}
      width={520}
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
            blockedReason={v.name.trim() ? undefined : t('domains:form.needName')}
            onClick={submit}
          >
            {t('common:action.save')}
          </WriteButton>
        </>
      }
    >
      <div className="flex flex-col gap-3">
        <Field label={t('domains:column.name')} hint={t('domains:form.nameHint')}>
          <input
            value={v.name}
            onChange={set('name')}
            disabled={editing}
            placeholder="example.com"
            className={inputCls}
          />
        </Field>
        <div className="grid grid-cols-2 gap-3">
          <Field label={t('domains:column.project')}>
            <input value={v.project} onChange={set('project')} className={inputCls} />
          </Field>
          <Field label={t('domains:column.env')}>
            <input value={v.env} onChange={set('env')} className={inputCls} />
          </Field>
          <Field label={t('domains:column.module')}>
            <input value={v.module} onChange={set('module')} className={inputCls} />
          </Field>
          <Field label={t('domains:column.owner')}>
            <input value={v.owner} onChange={set('owner')} className={inputCls} />
          </Field>
        </div>
        <Field label={t('domains:column.expiry')} hint={t('domains:form.expiryHint')}>
          <input type="date" value={v.expiry_at} onChange={set('expiry_at')} className={inputCls} />
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
