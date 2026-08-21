import { toErrorInfo } from '@ops/api'
import { formatNumber, tError, type Locale, useTranslation } from '@ops/i18n'
import {
  AsyncBoundary,
  Badge,
  Button,
  type LoadError,
  Skeleton,
  fromQuery,
} from '@ops/ui'
import { useState } from 'react'
import { can, useSession } from '../../lib/session.js'
import { statusTone } from './banner.js'
import { type LicenseInfo, useActivate, useLicense } from './queries.js'

type TFn = (key: string, opts?: Record<string, unknown>) => string

/** 容量项的显示顺序。固定顺序，不跟着 map 遍历跑。 */
const CAP_ITEMS = ['nodes', 'clusters', 'cloud_accounts', 'seats', 'tenants', 'retention_days']

export function LicensePage() {
  const { t } = useTranslation()
  const query = useLicense()
  const toLoadError = (e: unknown): LoadError => {
    const n = toErrorInfo(e)
    return { cause: tError(t, n.messageKey, n.params), detail: n.detail, retryable: n.retryable, kind: n.kind === 'license' ? 'license' : undefined, feature: n.params?.feature as string | undefined }
  }

  return (
    <div className="mx-auto max-w-[880px] p-5">
      <AsyncBoundary
        state={fromQuery<LicenseInfo>(query, () => false, toLoadError)}
        errorTitle={t('common:license.loadFailed')}
        retryLabel={t('common:action.retry')}
        onRetry={() => void query.refetch()}
        pending={
          <div className="flex flex-col gap-3">
            {[40, 90, 70, 60].map((w) => (
              <Skeleton key={w} className="h-4" style={{ width: `${w}%` }} />
            ))}
          </div>
        }
        empty={null}
      >
        {(info) => <Body info={info} t={t} />}
      </AsyncBoundary>
    </div>
  )
}

function Body({ info, t }: { info: LicenseInfo; t: TFn }) {
  const { i18n } = useTranslation()
  const locale = i18n.language as Locale
  const me = useSession()
  // 激活是管理动作。没权限的人也能看状态（否则他只会看到一堆
  // 点了没反应的按钮，而不知道原因），但看不到激活框
  const canActivate = can(me.data, 'menu:cmdb_basic')

  return (
    <div className="flex flex-col gap-5">
      <section className="rounded-[var(--radius-lg)] border border-border bg-card p-4">
        <div className="flex items-center gap-2.5">
          <h2 className="text-sm font-semibold text-foreground">{t('common:license.title')}</h2>
          <Badge tone={statusTone(info.status)}>{t(`common:license.status.${info.status}`)}</Badge>
          {info.readOnly ? (
            <span className="text-xs text-danger">{t('common:license.readOnlyTag')}</span>
          ) : null}
        </div>
        <p className="mt-1.5 text-xs leading-relaxed text-muted-foreground">
          {t(`common:license.statusHint.${info.status}`)}
        </p>

        <dl className="mt-3.5 grid grid-cols-[auto_1fr] gap-x-4 gap-y-1.5 text-[13px]">
          {info.licensee ? (
            <>
              {/* ⚠️ 空值显示「未填写」而不是留白。
                  一个既没有值也没有说明的空白行读起来像渲染出错，
                  而这一栏在授权信息里，看的人会怀疑整块数据都不可信（P2-66） */}
              <Field label={t('common:license.org')}>
                {info.licensee.org || t('common:state.notFilled')}
              </Field>
              <Field label={t('common:license.scope')}>
                {info.licensee.scopeName || t('common:state.notFilled')}
              </Field>
              <Field label={t('common:license.contact')}>
                {info.licensee.contact || t('common:state.notFilled')}
              </Field>
              <Field label={t('common:license.licenseId')}>
                <span className="font-mono text-xs">{info.licenseId}</span>
              </Field>
            </>
          ) : null}
          <Field label={t('common:license.expiry')}>
            {info.perpetual ? (
              t('common:license.perpetual')
            ) : info.expiresAt ? (
              <span className="tabular">
                {info.expiresAt}
                {info.daysUntilExpiry !== null ? (
                  <span className="ml-2 text-muted-foreground">
                    {t('common:license.daysLeft', { count: info.daysUntilExpiry })}
                  </span>
                ) : null}
              </span>
            ) : (
              '—'
            )}
          </Field>
        </dl>
      </section>

      <Fingerprint info={info} t={t} />

      <section className="rounded-[var(--radius-lg)] border border-border bg-card p-4">
        <h2 className="text-sm font-semibold text-foreground">{t('common:license.capacity')}</h2>
        {/* 明说超限不会拦操作。不说的话，客户看到红字第一反应是"系统要停了"，
            而实际上我们从不因为超限拒绝写入（临时扩容结果系统罢工会丢客户） */}
        <p className="mt-1 text-xs text-muted-foreground">{t('common:license.capacityHint')}</p>
        <table className="mt-3 w-full text-[13px]">
          <thead>
            <tr className="border-b border-border text-left text-[11px] tracking-wide text-muted-foreground uppercase">
              <th className="pb-1.5 font-medium">{t('common:license.capItem')}</th>
              <th className="pb-1.5 text-right font-medium">{t('common:license.capUsed')}</th>
              <th className="pb-1.5 text-right font-medium">{t('common:license.capLimit')}</th>
            </tr>
          </thead>
          <tbody>
            {CAP_ITEMS.map((item) => {
              const limit = info.capacity[item] ?? 0
              const used = info.usage[item]
              // 保留天数的「永不清理」= 语义上无限，超过任何有限上限。
              //
              // ⚠️ 后端现在显式给 `retention_unlimited`，**不要从 `used === 0` 推断**：
              //	0 同时可能是"没取到"和"永不清理"，而这两者的下一步相反。
              //	原来这一格渲染成「—」，看起来像"没数据"，
              //	而实际是配了 0（永不清理）且已经超出授权上限 3650 天，
              //	`exceeded` 里却什么都没有（OPSCMDB-031 P1-73）。
              const unlimited = item === 'retention_days' && info.retentionUnlimited
              const over = unlimited
                ? limit > 0
                : used !== undefined && limit > 0 && used > limit
              return (
                <tr key={item} className="border-b border-border last:border-b-0">
                  <td className="py-1.5">{t(`common:license.cap.${item}`)}</td>
                  <td className={`tabular py-1.5 text-right ${over ? 'text-warning' : ''}`}>
                    {/* 三态：无限 / 有值 / 没取到。「—」只用于最后一种 */}
                    {unlimited ? (
                      <span title={t('common:license.retentionUnlimitedHint')}>
                        {t('common:license.retentionUnlimited')}
                      </span>
                    ) : used === undefined ? (
                      '—'
                    ) : (
                      formatNumber(used, locale)
                    )}
                  </td>
                  <td className="tabular py-1.5 text-right text-muted-foreground">
                    {limit > 0 ? formatNumber(limit, locale) : t('common:license.unlimited')}
                  </td>
                </tr>
              )
            })}
          </tbody>
        </table>
      </section>

      <Features info={info} t={t} />

      {canActivate ? <Activate t={t} /> : null}
    </div>
  )
}

function Field({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <>
      <dt className="text-muted-foreground">{label}</dt>
      <dd className="min-w-0 text-foreground">{children}</dd>
    </>
  )
}

/**
 * 安装指纹。
 *
 * 必须能一键复制**完整值**：签发授权时要用它，客户得报得出来。
 * 只给截断显示的话，他会照着屏幕手抄，抄错一位签出来的授权永远对不上，
 * 而错误信息只会说"指纹不匹配"，看不出是抄错了。
 */
function Fingerprint({ info, t }: { info: LicenseInfo; t: TFn }) {
  const [copied, setCopied] = useState(false)
  return (
    <section className="rounded-[var(--radius-lg)] border border-border bg-card p-4">
      <h2 className="text-sm font-semibold text-foreground">{t('common:license.fingerprint')}</h2>
      <p className="mt-1 text-xs leading-relaxed text-muted-foreground">
        {t('common:license.fingerprintHint')}
      </p>
      <div className="mt-2.5 flex items-center gap-2">
        <code className="min-w-0 flex-1 truncate rounded-[var(--radius)] border border-border bg-secondary px-2.5 py-1.5 font-mono text-xs text-foreground">
          {info.fingerprint}
        </code>
        <Button
          size="sm"
          onClick={() => {
            void navigator.clipboard
              ?.writeText(info.fingerprint)
              .then(() => setCopied(true))
              // 复制失败不弹错：完整值就在旁边摆着，手抄也行。
              // 但**不能假装成功** —— 显示"已复制"而剪贴板是空的，
              // 客户会去粘一个不存在的东西
              .catch(() => setCopied(false))
          }}
        >
          {copied ? t('common:license.copied') : t('common:license.copy')}
        </Button>
      </div>
    </section>
  )
}

/**
 * 功能清单。
 *
 * # ⚠️ 必须同时给「有什么」和「缺什么」
 *
 * 原来只列已启用的 5 个 code。两个问题：
 *
 * 1. 是**原始 code**（`audit_rollback`、`mcp_full`、`version_upgrade`），
 *    界面不解释它们分别是什么
 * 2. 只有"有什么"，没有"缺什么" —— 客户无法判断
 *    "我这一档比更高档少了哪些能力"，也就无从产生升级动机
 *
 * 而这是**分档产品最该说清楚的一件事**（企业版规范里
 * "分档必须可见、`Has()` 不能写成全有全无"讲的就是它）。
 *
 * 对照 AI 接入页做对的地方：那里是「当前可用 87 个，产品共 87 个」双计数，
 * 受限时一眼看出差多少。授权页恰恰最该这么做，反而没做（OPSCMDB-031 P1-74）。
 */
function Features({ info, t }: { info: LicenseInfo; t: TFn }) {
  const label = (code: string) =>
    t(`common:license.feature.${code}`, { defaultValue: code })
  return (
    <section className="rounded-[var(--radius-lg)] border border-border bg-card p-4">
      <div className="flex flex-wrap items-baseline gap-2">
        <h2 className="text-sm font-semibold text-foreground">{t('common:license.features')}</h2>
        {/* 双计数：受限时一眼看出差多少 */}
        {info.featuresTotal > 0 ? (
          <span className="text-xs text-muted-foreground">
            {t('common:license.featureCount', {
              on: info.features.length,
              total: info.featuresTotal,
            })}
          </span>
        ) : null}
      </div>
      {info.features.length === 0 ? (
        // ⚠️ 这里的空**不等于"没买"**：功能要同时"授权里有"且"已经实现"才会亮。
        // 重构期绝大多数能力还在旧系统里，所以空是正常的。
        // 不写清楚的话，客户会以为自己买的东西没生效
        <p className="mt-1.5 text-xs leading-relaxed text-muted-foreground">
          {t('common:license.noFeatures')}
        </p>
      ) : (
        <div className="mt-2.5 flex flex-wrap gap-1.5">
          {info.features.map((f) => (
            <span
              key={f}
              className="rounded-[var(--radius-sm)] bg-secondary px-2 py-0.5 text-[11px] text-foreground"
              // code 放 title：客户报问题时说的是 code，界面显示中文名
              title={f}
            >
              {label(f)}
            </span>
          ))}
        </div>
      )}

      {/* 缺什么 —— 这一段原来完全不存在 */}
      {info.featuresMissing.length > 0 ? (
        <div className="mt-3 border-t border-border pt-2.5">
          <p className="text-xs text-muted-foreground">{t('common:license.featuresMissing')}</p>
          <div className="mt-1.5 flex flex-wrap gap-1.5">
            {info.featuresMissing.map((f) => (
              <span
                key={f}
                // 用虚线边框而不是实心底：这些是"没有"的东西，
                // 和上面已启用的一眼分得开
                className="rounded-[var(--radius-sm)] border border-dashed border-border px-2 py-0.5 text-[11px] text-muted-foreground"
                title={f}
              >
                {label(f)}
              </span>
            ))}
          </div>
        </div>
      ) : null}
    </section>
  )
}

function Activate({ t }: { t: TFn }) {
  const [token, setToken] = useState('')
  const activate = useActivate()
  const err = activate.error ? toErrorInfo(activate.error) : null

  return (
    <section className="rounded-[var(--radius-lg)] border border-border bg-card p-4">
      <h2 className="text-sm font-semibold text-foreground">{t('common:license.activate')}</h2>
      <p className="mt-1 text-xs leading-relaxed text-muted-foreground">
        {t('common:license.activateHint')}
      </p>
      <textarea
        value={token}
        onChange={(e) => setToken(e.target.value)}
        spellCheck={false}
        rows={4}
        placeholder={t('common:license.activatePlaceholder')}
        className="mt-2.5 w-full resize-y rounded-[var(--radius)] border border-border bg-background px-2.5 py-2 font-mono text-xs text-foreground outline-none focus:border-border-strong"
      />
      <div className="mt-2 flex items-center gap-3">
        <Button
          variant="primary"
          loading={activate.isPending}
          disabled={token.trim() === ''}
          onClick={() => activate.mutate(token)}
        >
          {t('common:license.activateAction')}
        </Button>
        {err ? (
          <span className="text-xs text-danger">{tError(t, err.messageKey, err.params)}</span>
        ) : activate.isSuccess ? (
          <span className="text-xs text-success">{t('common:license.activated')}</span>
        ) : null}
      </div>
    </section>
  )
}
