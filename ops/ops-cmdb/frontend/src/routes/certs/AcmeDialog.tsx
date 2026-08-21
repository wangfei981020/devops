import { toErrorInfo } from '@ops/api'
import { tError, useTranslation } from '@ops/i18n'
import { Badge, Banner, Dialog, Field, MutationError, Select, Skeleton, TextInput } from '@ops/ui'
import { useState } from 'react'
import { WriteButton } from '../../components/WriteButton.js'
import { useAcmeAccounts, useCreateAcme, useDeleteAcme } from './queries.js'

// acme-accounts 归在「集成」权限下（后端 perm.go:139），不是证书权限
const PERM = 'cmdb:manage_integrations'

/** 支持的 CA。**必须与证书上记录的 ca 完全一致**，续期时按它找账号。 */
const CAS = ['letsencrypt', 'zerossl', 'buypass']

/**
 * ACME 账号。
 *
 * ⚠️ 没有账号时，证书自动续期任务会**逐张证书失败**，
 * 报「无对应 ACME 账户（ca=letsencrypt）」—— 而旧版里根本没有地方能配。
 * 实测那个任务就是这么挂着的。
 */
export function AcmeDialog({ onClose }: { onClose: () => void }) {
  const { t } = useTranslation()
  const list = useAcmeAccounts()
  const create = useCreateAcme()
  const del = useDeleteAcme()
  const [email, setEmail] = useState('')
  const [ca, setCa] = useState('letsencrypt')

  const accounts = list.data ?? []

  return (
    <Dialog
      open
      onClose={onClose}
      title={t('certs:acme.title')}
      description={t('certs:acme.desc')}
      closeLabel={t('common:action.close')}
      width={620}
      footer={
        <>
          <WriteButton perm={PERM} size="sm" onClick={onClose}>
            {t('common:action.close')}
          </WriteButton>
          <WriteButton
            perm={PERM}
            variant="primary"
            size="sm"
            loading={create.isPending}
            blockedReason={email ? undefined : t('certs:acme.needEmail')}
            onClick={() => create.mutate({ email, ca }, { onSuccess: () => setEmail('') })}
          >
            {t('certs:acme.add')}
          </WriteButton>
        </>
      }
    >
      <div className="flex flex-col gap-3">
        {/* 一个账号都没有：直说后果，这是自动续期挂掉的直接原因 */}
        {list.isSuccess && accounts.length === 0 ? (
          <Banner tone="warn">
            <span className="font-medium">{t('certs:acme.emptyTitle')}</span>
            <span className="mt-0.5 block">{t('certs:acme.emptyBody')}</span>
          </Banner>
        ) : null}

        <div className="flex items-end gap-2">
          <Field label={t('certs:acme.email')} hint={t('certs:acme.emailHint')} required>
            <TextInput
              value={email}
              onChange={(e) => setEmail(e.target.value)}
              placeholder="ops@example.com"
              autoFocus
            />
          </Field>
          <Select<string>
            label={t('certs:acme.ca')}
            value={ca}
            onChange={setCa}
            options={CAS.map((c) => ({ value: c, label: c }))}
          />
        </div>
        <span className="-mt-1 text-xs leading-relaxed text-muted-foreground">
          {t('certs:acme.caHint')}
        </span>

        {create.isError ? (
          <MutationError error={create.error} toInfo={toErrorInfo} t={(k, p) => tError(t, k, p)} />
        ) : null}

        {list.isPending ? (
          <Skeleton className="h-5 w-[50%]" />
        ) : accounts.length > 0 ? (
          <div className="flex flex-col rounded-[var(--radius)] border border-border">
            {accounts.map((a) => (
              <div
                key={a.id}
                className="flex items-center gap-3 border-b border-border px-3 py-2 text-[13px] last:border-0"
              >
                <span className="min-w-0 flex-1 truncate">{a.email}</span>
                <Badge tone="mute">{a.ca}</Badge>
                {/* 没有账号私钥 = 这条是摆设，续期照样会失败 */}
                {!a.registered ? <Badge tone="bad">{t('certs:acme.noKey')}</Badge> : null}
                {a.status && a.status !== 'valid' ? (
                  <Badge tone="warn">{a.status}</Badge>
                ) : null}
                <WriteButton
                  perm={PERM}
                  size="sm"
                  variant="danger"
                  onClick={() => del.mutate(a.id)}
                >
                  {t('common:write.delete')}
                </WriteButton>
              </div>
            ))}
          </div>
        ) : null}
      </div>
    </Dialog>
  )
}
