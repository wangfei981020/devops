import { toErrorInfo } from '@ops/api'
import { tError, useTranslation } from '@ops/i18n'
import { Banner, Dialog, Field, MutationError, Skeleton, TextInput } from '@ops/ui'
import { useState } from 'react'
import { WriteButton } from '../../components/WriteButton.js'
import { useDeleteOriginRule, useOriginRules, useSaveOriginRule } from './queries.js'

const PERM = 'cmdb:manage_records'

/**
 * 回源规则：回源 CNAME → 源站 IP。
 *
 * 用于 DNS 查不到时兜底推断源站。规则推出来的 IP 在台账里会标成「推测」——
 * 它没落库，也可能过时。
 */
export function OriginRulesDialog({ onClose }: { onClose: () => void }) {
  const { t } = useTranslation()
  const q = useOriginRules()
  const save = useSaveOriginRule()
  const del = useDeleteOriginRule()
  const [cname, setCname] = useState('')
  const [ip, setIp] = useState('')

  const rules = q.data ?? []

  return (
    <Dialog
      open
      onClose={onClose}
      title={t('hostrecords:originRules.title')}
      description={t('hostrecords:originRules.desc')}
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
            loading={save.isPending}
            blockedReason={cname && ip ? undefined : t('hostrecords:originRules.needBoth')}
            onClick={() =>
              save.mutate(
                { cname, origin_ip: ip },
                {
                  onSuccess: () => {
                    setCname('')
                    setIp('')
                  },
                },
              )
            }
          >
            {t('common:write.add')}
          </WriteButton>
        </>
      }
    >
      <div className="flex flex-col gap-3">
        <div className="flex items-end gap-2">
          <Field label={t('hostrecords:originRules.cname')} hint={t('hostrecords:originRules.cnameHint')}>
            <TextInput
              value={cname}
              onChange={(e) => setCname(e.target.value)}
              placeholder="cdn.example.com"
            />
          </Field>
          <Field label={t('hostrecords:originRules.ip')}>
            <TextInput value={ip} onChange={(e) => setIp(e.target.value)} placeholder="10.0.0.1" />
          </Field>
        </div>

        {save.isError ? (
          <MutationError error={save.error} toInfo={toErrorInfo} t={(k, p) => tError(t, k, p)} />
        ) : null}

        {q.isPending ? (
          <Skeleton className="h-5 w-[40%]" />
        ) : rules.length === 0 ? (
          <p className="py-3 text-center text-xs text-muted-foreground">
            {t('hostrecords:originRules.empty')}
          </p>
        ) : (
          <div className="flex flex-col rounded-[var(--radius)] border border-border">
            {rules.map((r) => (
              <div
                key={r.id}
                className="flex items-center gap-3 border-b border-border px-3 py-2 text-[13px] last:border-0"
              >
                <code className="min-w-0 flex-1 truncate font-mono text-xs">{r.cname}</code>
                <span className="text-muted-foreground">→</span>
                <code className="font-mono text-xs">{r.origin_ip}</code>
                {/* 用量：没人用的规则留着会让人以为源站已经登记好了 */}
                <span className="tabular w-[70px] shrink-0 text-right text-[11px] text-muted-foreground">
                  {r.used > 0
                    ? t('hostrecords:originRules.used', { count: r.used })
                    : t('hostrecords:originRules.unused')}
                </span>
                <WriteButton
                  perm={PERM}
                  size="sm"
                  variant="danger"
                  onClick={() => del.mutate(r.id)}
                >
                  {t('common:write.delete')}
                </WriteButton>
              </div>
            ))}
          </div>
        )}
      </div>
    </Dialog>
  )
}
