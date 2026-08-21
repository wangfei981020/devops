import { toErrorInfo } from '@ops/api'
import { tError, useTranslation } from '@ops/i18n'
import { Banner, Dialog, Field, SecretInput, Select, Switch, TextInput } from '@ops/ui'
import { useState } from 'react'
import { WriteButton } from '../../components/WriteButton.js'
import { useEnvOptions } from '../../lib/dicts.js'
import { type HarborRegistry, useSaveRegistry } from './queries.js'

const PERM = 'cmdb:manage_integrations'

/**
 * 接入 / 编辑一个镜像仓库。
 *
 * ⚠️ 这份配置是**租户级**的：每个租户接自己的 Harbor。
 * 后端的 Scoped 层强制带租户条件，前端不传 tenant_id。
 */
export function RegistryDialog({
  initial,
  onClose,
}: {
  /** 传入 = 编辑，不传 = 新建 */
  initial?: HarborRegistry
  onClose: () => void
}) {
  const { t } = useTranslation()
  const save = useSaveRegistry()
  const envs = useEnvOptions()
  const [f, setF] = useState({
    name: initial?.name ?? '',
    url: initial?.url ?? '',
    username: '',
    password: '',
    env: initial?.env ?? '',
    skip_verify: false,
    enabled: initial?.enabled ?? true,
  })
  const set = <K extends keyof typeof f>(k: K, v: (typeof f)[K]) =>
    setF((p) => ({ ...p, [k]: v }))

  return (
    <Dialog
      open
      onClose={onClose}
      title={initial ? t('registry:dialog.edit') : t('registry:dialog.add')}
      description={t('registry:dialog.desc')}
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
            blockedReason={f.name && f.url ? undefined : t('registry:field.required')}
            onClick={() =>
              save.mutate({ id: initial?.id, ...f }, { onSuccess: onClose })
            }
          >
            {t('common:write.save')}
          </WriteButton>
        </>
      }
    >
      <div className="flex flex-col gap-3">
        <Field label={t('registry:field.name')} required>
          <TextInput value={f.name} onChange={(e) => set('name', e.target.value)} autoFocus />
        </Field>
        <Field label={t('registry:field.url')} hint={t('registry:field.urlHint')} required>
          <TextInput
            value={f.url}
            onChange={(e) => set('url', e.target.value)}
            placeholder="https://harbor.example.com"
          />
        </Field>
        <Field label={t('registry:field.username')}>
          <TextInput value={f.username} onChange={(e) => set('username', e.target.value)} />
        </Field>
        <Field
          label={t('registry:field.password')}
          hint={initial?.has_secret ? t('common:write.secretKeep') : t('registry:field.passwordHint')}
        >
          <SecretInput
            configured={initial?.has_secret ?? false}
            placeholderConfigured="••••••••"
            placeholderEmpty=""
            showLabel={t('common:action.showSecret')}
            hideLabel={t('common:action.hideSecret')}
            value={f.password}
            onChange={(e) => set('password', e.target.value)}
          />
        </Field>
        <Field label={t('registry:field.env')} hint={t('registry:field.envHint')}>
          {/* 环境从字典取，不手输 —— 与观测端点同样的理由：
              手输打错时不会报错，只是这条记录在按环境查找时永远匹配不上（CONVENTIONS §2.7.4） */}
          <Select<string>
            label={t('registry:field.env')}
            value={f.env}
            onChange={(v) => set('env', v)}
            options={[
              { value: '', label: t('registry:field.envAny') },
              ...(envs.data ?? []).map((e) => ({ value: e.code, label: `${e.code} · ${e.name}` })),
            ]}
          />
          {envs.isError ? (
            <p className="mt-1 text-xs text-danger">{t('registry:field.envLoadFailed')}</p>
          ) : null}
        </Field>

        {/* 关掉证书校验必须是显式动作，而且要说清代价。
            默认勾上的话，自签证书能用了，但中间人也一起放行了 */}
        <div className="flex flex-col gap-1.5">
          <Switch
            checked={f.skip_verify}
            onChange={(v) => set('skip_verify', v)}
            label={t('registry:field.skipVerify')}
          />
          <span className="text-xs leading-relaxed text-muted-foreground">
            {t('registry:field.skipVerifyHint')}
          </span>
        </div>

        <Switch
          checked={f.enabled}
          onChange={(v) => set('enabled', v)}
          label={t('registry:field.enabled')}
        />

        <Banner tone="info">
          {/* 保存 ≠ 能用。这句话放在保存按钮上方，保证被看到 */}
          <span>{t('registry:dialog.testAfterSave')}</span>
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
