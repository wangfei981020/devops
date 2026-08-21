import { toErrorInfo } from '@ops/api'
import { formatRelativeTime, tError, type Locale, useTranslation } from '@ops/i18n'
import { AsyncBoundary, Badge, type LoadError, Skeleton, fromQuery } from '@ops/ui'
import { type Credential, useCredentials } from './queries.js'

export function CredentialsPage() {
  const { t, i18n } = useTranslation()
  const locale = i18n.language as Locale
  const query = useCredentials()
  const toLoadError = (e: unknown): LoadError => {
    const n = toErrorInfo(e)
    return { cause: tError(t, n.messageKey, n.params), detail: n.detail, retryable: n.retryable, kind: n.kind === 'license' ? 'license' : undefined, feature: n.params?.feature as string | undefined }
  }

  return (
    <div className="mx-auto max-w-[900px] p-5">
      <AsyncBoundary
        state={fromQuery<{ items: Credential[]; total: number }>(query, () => false, toLoadError)}
        errorTitle={t('credentials:error.title')}
        retryLabel={t('common:action.retry')}
        onRetry={() => void query.refetch()}
        pending={<Skeleton className="h-5 w-[60%]" />}
        empty={null}
      >
        {(d) => {
          /*
            🔴 分组判据是「**该有凭据却没有**」，不是「凭据字段为空」。
            
            三个条件缺一不可：
              enabled          —— 停用的数据源不配凭据是对的
              cred_required    —— 只有云账号没凭据就一定采不到；
                                  集群走 SA / 云账号继承，观测端点内网无鉴权
              credential==none —— 继承来的也算有
            
            原来只看 `!has_credential`，于是 3 个 2~4 分钟前刚同步成功的
            K8s 集群被列进「启用了但没配凭据」（OPSCMDB-031 P2-50）。
            
            ⚠️ 后端老版本不返回 cred_required（undefined）。这时**退回旧行为**
            而不是当成 false —— 当成 false 会让「缺凭据」永远是 0，
            那是把一个真实的安全缺口静默抹平，比误报严重得多。
          */
          const missing = d.items.filter(
            (x) =>
              x.enabled &&
              (x.cred_required ?? !x.has_credential) &&
              (x.credential ? x.credential === 'none' : !x.has_credential),
          )
          const stale = d.items.filter((x) => x.enabled && x.has_credential && x.stale)
          const withCred = d.items.filter((x) => x.has_credential)
          // 「不需要在这里配凭据」的那批单列一档：它们不是问题，
          // 但完全不提会让人以为盘点漏了它们（这一页是**攻击面清单**，
          // 少一行会被读成"这个数据源不存在"）
          const notApplicable = d.items.filter(
            (x) => x.enabled && !x.has_credential && x.cred_required === false,
          )
          return (
            <div className="flex flex-col gap-5">
              <section className="rounded-[var(--radius-lg)] border border-border bg-card p-4">
                <h2 className="text-sm font-semibold text-foreground">{t('credentials:title')}</h2>
                {/* 这一页看不到凭据内容，也不该能看到。说明白，免得有人来问"在哪看密钥" */}
                <p className="mt-1 text-xs leading-relaxed text-muted-foreground">
                  {t('credentials:hint')}
                </p>
                <div className="mt-3 flex flex-wrap gap-x-6 gap-y-2">
                  <Stat n={withCred.length} label={t('credentials:stat.stored')} />
                  <Stat n={missing.length} label={t('credentials:stat.missing')} bad={missing.length > 0} />
                  <Stat n={stale.length} label={t('credentials:stat.stale')} warn={stale.length > 0} />
                </div>
              </section>

              {missing.length > 0 ? (
                <Group
                  title={t('credentials:missingTitle')}
                  hint={t('credentials:missingHint')}
                  tone="bad"
                  items={missing}
                  locale={locale}
                  t={t}
                />
              ) : null}
              {notApplicable.length > 0 ? (
                <Group
                  title={t('credentials:naTitle')}
                  hint={t('credentials:naHint')}
                  tone="mute"
                  items={notApplicable}
                  locale={locale}
                  t={t}
                />
              ) : null}
              {stale.length > 0 ? (
                <Group
                  title={t('credentials:staleTitle')}
                  hint={t('credentials:staleHint')}
                  tone="warn"
                  items={stale}
                  locale={locale}
                  t={t}
                />
              ) : null}
              <Group
                title={t('credentials:storedTitle')}
                hint={t('credentials:storedHint')}
                tone="mute"
                items={withCred}
                locale={locale}
                t={t}
              />
            </div>
          )
        }}
      </AsyncBoundary>
    </div>
  )
}

function Stat({ n, label, bad, warn }: { n: number; label: string; bad?: boolean; warn?: boolean }) {
  return (
    <div className="flex items-baseline gap-2">
      <span
        className={`tabular text-lg font-semibold ${bad ? 'text-danger' : warn ? 'text-warning' : 'text-foreground'}`}
      >
        {n}
      </span>
      <span className="text-xs text-muted-foreground">{label}</span>
    </div>
  )
}

function Group({
  title,
  hint,
  tone,
  items,
  locale,
  t,
}: {
  title: string
  hint: string
  tone: 'bad' | 'warn' | 'mute'
  items: Credential[]
  locale: Locale
  t: (k: string, o?: Record<string, unknown>) => string
}) {
  return (
    <section className="rounded-[var(--radius-lg)] border border-border bg-card p-4">
      <h3 className="text-sm font-semibold text-foreground">{title}</h3>
      <p className="mt-1 text-xs text-muted-foreground">{hint}</p>
      <div className="mt-2.5 flex flex-col">
        {items.map((c) => (
          <div
            key={`${c.kind}/${c.name}`}
            className="flex items-baseline gap-2.5 border-b border-border py-2 text-[13px] last:border-b-0"
          >
            <Badge tone={tone}>{t(`datasources:kind.${c.kind}`)}</Badge>
            <span className="min-w-0 flex-1 truncate text-foreground">{c.name}</span>
            <span className="font-mono text-xs text-muted-foreground">{c.type}</span>
            <span className="w-[92px] shrink-0 text-right text-xs text-muted-foreground">
              {c.last_sync_at ? formatRelativeTime(c.last_sync_at, locale) : t('datasources:neverSynced')}
            </span>
          </div>
        ))}
      </div>
    </section>
  )
}
