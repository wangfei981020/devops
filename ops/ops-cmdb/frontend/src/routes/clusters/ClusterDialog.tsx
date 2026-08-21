import { toErrorInfo } from '@ops/api'
import { useTranslation } from '@ops/i18n'
import { Button, Dialog, Field, Select, Switch, TextArea, TextInput } from '@ops/ui'
import { useState } from 'react'
import { type Cluster, useSaveCluster } from './queries.js'

const ENVS = ['PROD', 'UAT', 'TEST', 'DEV']
const PROVIDERS = ['gke', 'generic']

/**
 * 纳管/编辑集群。
 *
 * 这一步是整个系统的入口：没有集群，「集群」那一组的 8 个页面全是空的。
 */
export function ClusterDialog({
  cluster,
  onClose,
}: {
  /** null = 新建 */
  cluster: Cluster | null
  onClose: () => void
}) {
  const { t } = useTranslation()
  const [name, setName] = useState(cluster?.name ?? '')
  const [displayName, setDisplayName] = useState(cluster?.displayName ?? '')
  const [env, setEnv] = useState(cluster?.environment || 'DEV')
  const [provider, setProvider] = useState(cluster?.provider || 'generic')
  const [location, setLocation] = useState(cluster?.location ?? '')
  const [promValue, setPromValue] = useState('')
  const [kubeconfig, setKubeconfig] = useState('')
  const [enabled, setEnabled] = useState(cluster?.enabled ?? true)
  const save = useSaveCluster()

  return (
    <Dialog
      open
      onClose={onClose}
      title={cluster ? t('clusters:editTitle', { name: cluster.displayName }) : t('clusters:addTitle')}
      description={t('clusters:addDesc')}
      closeLabel={t('common:action.close')}
      width={620}
      footer={
        <>
          <Button size="sm" onClick={onClose}>
            {t('common:write.cancel')}
          </Button>
          <Button
            size="sm"
            variant="primary"
            loading={save.isPending}
            disabled={name.trim() === ''}
            onClick={() =>
              save.mutate(
                {
                  id: cluster?.id,
                  name: name.trim(),
                  display_name: displayName.trim() || name.trim(),
                  environment: env,
                  provider,
                  location: location.trim(),
                  kubeconfig,
                  prom_cluster_value: promValue.trim(),
                  enabled: enabled ? 1 : 0,
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
        <Field label={t('clusters:field.name')} hint={t('clusters:field.nameHint')} required>
          <TextInput value={name} onChange={(e) => setName(e.target.value)} autoFocus />
        </Field>
        <Field label={t('clusters:field.displayName')} hint={t('clusters:field.displayNameHint')}>
          <TextInput value={displayName} onChange={(e) => setDisplayName(e.target.value)} />
        </Field>
        <div className="flex gap-3">
          <div className="flex-1">
            <Field label={t('clusters:field.env')}>
              {/* 环境值原样照搬，不加"（生产）"这类解释 */}
              <Select<string>
                label={t('clusters:field.env')}
                value={env}
                onChange={setEnv}
                options={ENVS.map((v) => ({ value: v, label: v }))}
              />
            </Field>
          </div>
          <div className="flex-1">
            <Field label={t('clusters:field.provider')} hint={t('clusters:field.providerHint')}>
              <Select<string>
                label={t('clusters:field.provider')}
                value={provider}
                onChange={setProvider}
                options={PROVIDERS.map((v) => ({ value: v, label: v }))}
              />
            </Field>
          </div>
        </div>
        <Field label={t('clusters:field.location')}>
          <TextInput value={location} onChange={(e) => setLocation(e.target.value)} />
        </Field>
        <Field label={t('clusters:field.promValue')} hint={t('clusters:field.promValueHint')}>
          <TextInput value={promValue} onChange={(e) => setPromValue(e.target.value)} />
        </Field>
        <Field label={t('clusters:field.kubeconfig')} hint={t('clusters:field.kubeconfigHint')}>
          {/* 已配的不回填 —— 接口不回传它。留空 = 不改 */}
          <TextArea
            rows={8}
            value={kubeconfig}
            onChange={(e) => setKubeconfig(e.target.value)}
            spellCheck={false}
            placeholder={
              cluster?.hasKubeconfig ? t('common:write.secretKeep') : t('common:write.secretEmpty')
            }
          />
        </Field>
        <Switch checked={enabled} onChange={setEnabled} label={t('clusters:field.enabled')} />
        {save.isError ? (
          <p className="text-xs text-danger">{t(toErrorInfo(save.error).messageKey)}</p>
        ) : null}
      </div>
    </Dialog>
  )
}
