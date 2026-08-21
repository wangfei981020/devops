import { toErrorInfo } from '@ops/api'
import { tError, useTranslation } from '@ops/i18n'
import { Banner, Dialog, Field, SecretInput, Select, Switch, TextInput } from '@ops/ui'
import { useState } from 'react'
import { WriteButton } from '../../components/WriteButton.js'
import {
  type CdnAccount,
  useCdnVendors,
  useCreateVendor,
  useSaveCdnAccount,
} from './queries.js'

/**
 * ⚠️ 账号写操作要的是 `cmdb:sync_cdn`，不是 manage_cdn。
 *
 * 后端 perm.go 里 `/api/cdn/` 这条前缀规则把**写**映射到了 sync_cdn，
 * 而 `/api/cdns`（厂商）才是 manage_cdn。名字看着像反的，但前端必须按
 * 后端实际要求来 —— 写成 manage_cdn 的后果是按钮亮着、点下去 403，
 * 而用户完全看不出为什么。
 *
 * （两个权限在所有内置角色里都是成对给的，所以今天没有实际差别；
 *   写对是为了以后有人拆分角色时不出事。）
 */
const PERM = 'cmdb:sync_cdn'
const VENDOR_PERM = 'cmdb:manage_cdn'

/**
 * 接入 / 编辑一个 CDN 账号。
 *
 * ⚠️ 没有厂商就挂不了账号，所以这里允许**就地建厂商** ——
 * 否则用户点进来会看到一个空下拉，然后得自己猜去哪儿建。
 */
export function AccountDialog({
  initial,
  onClose,
}: {
  initial?: CdnAccount
  onClose: () => void
}) {
  const { t } = useTranslation()
  const vendors = useCdnVendors()
  const save = useSaveCdnAccount()
  const createVendor = useCreateVendor()
  const [newVendor, setNewVendor] = useState('')
  const [f, setF] = useState({
    cdn_id: initial?.cdn_id ?? 0,
    name: initial?.name ?? '',
    token: '',
    account_tag: initial?.account_tag ?? '',
    enabled: initial?.enabled ?? true,
  })
  const set = <K extends keyof typeof f>(k: K, v: (typeof f)[K]) => setF((p) => ({ ...p, [k]: v }))

  const list = vendors.data ?? []
  const noVendor = vendors.isSuccess && list.length === 0
  const effectiveCdnID = f.cdn_id || list[0]?.id || 0

  return (
    <Dialog
      open
      onClose={onClose}
      title={initial ? t('cdn:dialog.editAccount') : t('cdn:dialog.addAccount')}
      description={t('cdn:dialog.accountDesc')}
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
            blockedReason={
              !f.name
                ? t('cdn:field.nameRequired')
                : !effectiveCdnID
                  ? t('cdn:field.vendorRequired')
                  : undefined
            }
            onClick={() =>
              save.mutate(
                { id: initial?.id, ...f, cdn_id: effectiveCdnID },
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
        {/* 一个厂商都没有：就地建，不要把人赶去别的页面 */}
        {noVendor ? (
          <div className="flex flex-col gap-1.5">
            <Banner tone="info">
              <span>{t('cdn:vendor.none')}</span>
            </Banner>
            <div className="flex gap-2">
              <TextInput
                value={newVendor}
                onChange={(e) => setNewVendor(e.target.value)}
                placeholder="Cloudflare"
              />
              <WriteButton
                perm={VENDOR_PERM}
                size="sm"
                loading={createVendor.isPending}
                blockedReason={newVendor ? undefined : t('cdn:vendor.needName')}
                onClick={() => createVendor.mutate({ name: newVendor })}
              >
                {t('cdn:vendor.create')}
              </WriteButton>
            </div>
          </div>
        ) : (
          <div className="flex flex-col gap-1.5">
            <Select<string>
              label={t('cdn:field.vendor')}
              value={String(effectiveCdnID)}
              onChange={(v) => set('cdn_id', Number(v))}
              options={list.map((v) => ({ value: String(v.id), label: v.name }))}
              className="self-start"
            />
            {/* 目前只实现了 Cloudflare 适配。选别的能存，但同步时会被拒 ——
                与其让人事后撞上，不如提前说 */}
            <span className="text-xs leading-relaxed text-muted-foreground">
              {t('cdn:field.vendorHint')}
            </span>
          </div>
        )}

        <Field label={t('cdn:field.accountName')} required>
          <TextInput value={f.name} onChange={(e) => set('name', e.target.value)} autoFocus />
        </Field>

        <Field label={t('cdn:field.accountTag')} hint={t('cdn:field.accountTagHint')}>
          <TextInput value={f.account_tag} onChange={(e) => set('account_tag', e.target.value)} />
        </Field>

        <Field
          label={t('cdn:field.token')}
          hint={initial?.has_credential ? t('common:write.secretKeep') : t('cdn:field.tokenHint')}
        >
          <SecretInput
            configured={initial?.has_credential ?? false}
            placeholderConfigured="••••••••"
            placeholderEmpty=""
            showLabel={t('common:action.showSecret')}
            hideLabel={t('common:action.hideSecret')}
            value={f.token}
            onChange={(e) => set('token', e.target.value)}
          />
        </Field>

        <Switch
          checked={f.enabled}
          onChange={(v) => set('enabled', v)}
          label={t('cdn:field.enabled')}
        />

        <Banner tone="info">
          <span>{t('cdn:dialog.syncNote')}</span>
        </Banner>

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
