import { useTranslation } from '@ops/i18n'
import { AsyncBoundary, Badge, Banner, Button, Skeleton, cn, fromQuery } from '@ops/ui'
import { Copy, Check } from 'lucide-react'
import { useState } from 'react'
import { makeLoadError } from '../../lib/api.js'
import { licenseBanner, statusTone } from './banner.js'
import { useActivate, useFingerprint, useLicense, type LicenseInfo } from './queries.js'

/** 容量项的显示名。认不出的键原样显示，不退化成"未知"。 */
const CAP_KEYS: Record<string, string> = {
  rules: 'opsalert:license.capRules',
  datasources: 'opsalert:license.capDatasources',
  users: 'opsalert:license.capUsers',
}

export function LicensePage() {
  const { t } = useTranslation()
  const q = useLicense()

  return (
    <div className="flex flex-col gap-3">
      <AsyncBoundary
        state={fromQuery(q, () => false, makeLoadError(t))}
        pending={<Skeleton className="h-64 w-full" />}
        empty={null}
        errorTitle={t('opsalert:license.loadError')}
        retryLabel={t('action.retry')}
        onRetry={() => q.refetch()}
      >
        {(info) => <Body info={info} />}
      </AsyncBoundary>
    </div>
  )
}

function Body({ info }: { info: LicenseInfo }) {
  const { t } = useTranslation()
  const spec = licenseBanner(info)

  return (
    <>
      {spec && <Banner tone={spec.tone}>{t(spec.key, spec.params)}</Banner>}

      <Panel title={t('opsalert:license.status')}>
        <dl className="grid gap-x-6 gap-y-2 sm:grid-cols-[max-content_1fr]">
          <Field label={t('opsalert:license.currentStatus')}>
            <Badge tone={statusTone(info.status)}>
              {t(`opsalert:license.st.${info.status}`, info.status)}
            </Badge>
            {info.readOnly && (
              <span className="ml-2 text-2xs text-danger">{t('opsalert:license.readOnly')}</span>
            )}
            {/* 每个状态配一句人话。七态里有五个会出横幅，
                另两个（正常 / 社区版）刻意不出横幅——但仍要能解释它是什么，
                否则"社区版"三个字会被当成一种故障 */}
            <p className="mt-1 text-2xs text-muted-foreground">
              {t(`opsalert:license.statusHint.${info.status}`, '')}
            </p>
          </Field>

          {info.status !== 'not_activated' && (
            <>
              <Field label={t('opsalert:license.licensee')}>
                {info.licensee?.org || t('state.noValue')}
              </Field>
              <Field label={t('opsalert:license.expiry')}>
                {info.perpetual ? (
                  t('opsalert:license.perpetual')
                ) : info.expiresAt ? (
                  // ⚠️ 日期和剩余天数一起给。只给日期的话人要自己算，
                  // 只给天数的话对不上合同
                  <>
                    <span className="font-mono">{new Date(info.expiresAt).toLocaleDateString()}</span>
                    {info.daysUntilExpiry !== null && (
                      <span className="ml-2 text-2xs text-muted-foreground">
                        {t('opsalert:license.daysLeft', { days: info.daysUntilExpiry })}
                      </span>
                    )}
                  </>
                ) : (
                  t('state.noValue')
                )}
              </Field>
              <Field label={t('opsalert:license.licenseId')}>
                <span className="font-mono text-2xs">{info.licenseId || t('state.noValue')}</span>
              </Field>
            </>
          )}
        </dl>
      </Panel>

      <Panel title={t('opsalert:license.capacity')}>
        {/* 当前上限来自哪里必须说清楚：未激活时是内置 CE 上限，
            激活后是授权里写的。不说的话，客户看到 100 会以为是产品硬限制 */}
        <p className="mb-2 text-2xs text-muted-foreground">
          {info.status === 'not_activated'
            ? t('opsalert:license.capFromCE')
            : t('opsalert:license.capFromLicense')}
        </p>
        <dl className="grid gap-x-6 gap-y-2 sm:grid-cols-[max-content_1fr]">
          {Object.keys(CAP_KEYS).map((k) => {
            const limit = info.status === 'not_activated' ? info.ceLimits[k] : info.capacity[k]
            return (
              <Field key={k} label={t(CAP_KEYS[k]!, k)}>
                {/* 🔴 0 表示不限，**不是"零个"**。渲染成 0 会让人以为
                    这一项被完全关掉了，而真相正好相反 */}
                {limit === 0 || limit == null
                  ? t('opsalert:license.unlimited')
                  : String(limit)}
              </Field>
            )
          })}
        </dl>
      </Panel>

      <Features info={info} />

      {info.canActivate && <Activate />}
      <Fingerprint />
    </>
  )
}

function Features({ info }: { info: LicenseInfo }) {
  const { t } = useTranslation()
  const names = Object.keys(info.features).sort()
  return (
    <Panel title={t('opsalert:license.features')}>
      <ul className="flex flex-col gap-1.5">
        {names.map((f) => {
          const st = info.features[f]!
          // 🔴 三态，不是两态：
          //   有且已实现 → 可用
          //   有但没实现 → 「买了这版还没做」，客户该等版本而不是报障
          //   没有       → 需要授权
          // 把后两者合并成一个"不可用"，客户会去催采购，而那没用
          // ⚠️ 判据是 granted，不是 has。has 已经把 implemented 折进去了，
          // 用它的话「买了但这版没做」会被显示成「需要授权」
          const tone = st.granted && st.implemented ? 'ok' : st.granted ? 'warn' : 'mute'
          const label = st.granted
            ? st.implemented
              ? t('opsalert:license.fAvailable')
              : t('opsalert:license.fNotImplemented')
            : t('opsalert:license.fNeedsLicense')
          return (
            <li key={f} className="flex items-center gap-2 text-xs">
              <span
                className={cn(
                  'size-1.5 rounded-full',
                  tone === 'ok' ? 'bg-success' : tone === 'warn' ? 'bg-warning' : 'bg-muted-foreground/50',
                )}
                aria-hidden="true"
              />
              <span className="font-medium">{t(`opsalert:license.f.${f}`, f)}</span>
              <span className="ml-auto text-2xs text-muted-foreground">{label}</span>
            </li>
          )
        })}
      </ul>
    </Panel>
  )
}

function Fingerprint() {
  const { t } = useTranslation()
  const q = useFingerprint()
  const [copied, setCopied] = useState(false)
  return (
    <Panel title={t('opsalert:license.fingerprint')}>
      <p className="mb-2 text-2xs text-muted-foreground">{t('opsalert:license.fingerprintHint')}</p>
      {q.isPending && <Skeleton className="h-8 w-full" />}
      {q.isError && (
        // 算不出指纹是真故障（连不上库），不能渲染成空 ——
        // 空白会被读成"这套安装没有指纹"，而那是不可能的
        <Banner tone="bad">{t('opsalert:license.fingerprintError')}</Banner>
      )}
      {q.data && (
        <div className="flex items-center gap-2">
          {/* select-all：这串要复制给签发方，被截断就签出一份永远对不上的授权 */}
          <code className="min-w-0 flex-1 select-all break-all rounded border border-border bg-muted/40 px-2 py-1.5 font-mono text-[11px]">
            {q.data.fingerprint}
          </code>
          <Button
            size="sm"
            variant="ghost"
            onClick={() => {
              navigator.clipboard.writeText(q.data.fingerprint)
              setCopied(true)
              setTimeout(() => setCopied(false), 1500)
            }}
          >
            {copied ? <Check className="size-3.5" /> : <Copy className="size-3.5" />}
            {copied ? t('opsalert:license.copied') : t('opsalert:license.copy')}
          </Button>
        </div>
      )}
    </Panel>
  )
}

function Activate() {
  const { t } = useTranslation()
  const [token, setToken] = useState('')
  const act = useActivate()
  return (
    <Panel title={t('opsalert:license.activate')}>
      <textarea
        value={token}
        onChange={(e) => setToken(e.target.value)}
        rows={4}
        placeholder={t('opsalert:license.tokenPlaceholder')}
        className="w-full rounded-md border border-border bg-card px-2.5 py-2 font-mono text-[11px] outline-none focus:border-primary"
      />
      <div className="mt-2 flex items-center gap-2">
        <Button
          variant="primary"
          disabled={!token.trim()}
          loading={act.isPending}
          onClick={() => act.mutate(token)}
        >
          {t('opsalert:license.doActivate')}
        </Button>
        {act.isError && (
          <span className="text-xs text-danger">{String((act.error as Error).message)}</span>
        )}
        {act.isSuccess && (
          // ⚠️ 明说"其余副本 20 秒内生效"。不说的话，管理员在另一个标签页
          // （可能打到别的 Pod）看到仍是未激活，会以为激活失败并反复重贴
          <span className="text-xs text-success">{act.data.note}</span>
        )}
      </div>
    </Panel>
  )
}

// statusTone 与 @ops/ui 的 BadgeTone 恰好同名同义，直接透传。
// 保留这层薄封装是为了：哪天两边的取值集合分叉，编译期就会在这里失败，
// 而不是渲染出一个没有样式的徽章。

function Field({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div className="contents">
      <dt className="text-xs text-muted-foreground">{label}</dt>
      <dd className="text-xs">{children}</dd>
    </div>
  )
}

function Panel({ title, children }: { title: string; children: React.ReactNode }) {
  return (
    <section className="rounded-lg border border-border bg-card">
      <h3 className="border-b border-border px-3.5 py-2 text-xs font-semibold">{title}</h3>
      <div className="px-3.5 py-3">{children}</div>
    </section>
  )
}
