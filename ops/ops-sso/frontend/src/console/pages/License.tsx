import { useTranslation } from '@ops/i18n'
import { AsyncBoundary, Skeleton, fromQuery } from '@ops/ui'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Check, Copy } from 'lucide-react'
import { useMemo, useState } from 'react'
import { ApiError, api } from '../../api/client.js'
import { makeToLoadError } from '../../shared/errorText.js'

interface FeatureReport {
  feature: string
  implemented: boolean
  granted: boolean
  usable: boolean
  reason: string
}
interface CapacityItem {
  item: string
  limit: number
  used?: number
  /** false 表示这一项**本来就不数**（如租户是全局的），不是数失败 */
  countable: boolean
}
/**
 * 后端返回的是**结构体**，不是一个字符串。
 *
 * ⚠️ 这里踩过一次：类型写成 `licensee?: string`，未激活时字段不存在所以毫无异常，
 * 一旦真装上一份 license，React 拿到对象直接抛 #31「Objects are not valid as
 * a React child」，整页崩。只有真激活才会暴露 —— 未激活态永远测不出来。
 */
interface Licensee {
  org: string
  tax_id?: string
  contact?: string
  email?: string
  scope_name?: string
}

interface LicenseState {
  status: string
  can_write: boolean
  activated: boolean
  features: FeatureReport[]
  capacity: CapacityItem[]
  fingerprint: string
  fingerprint_full: string
  license_id?: string
  licensee?: Licensee
  perpetual?: boolean
  expires_at?: string
  days_until_expiry?: number
  should_remind?: boolean
}

/**
 * 授权。
 *
 * # 这一页要能独立回答三个问题
 *
 *   1. 现在是什么状态、为什么（不是"未授权"三个字了事）
 *   2. 我要去申请授权，该把什么发给对方（安装指纹，可一键复制）
 *   3. 哪些功能能用、哪些不能用、不能用是因为**没买**还是**没做**
 *
 * 第 3 条的区分是硬要求：把没做的功能显示成"未购买"，
 * 客户付完钱当天就会发现 —— 那是最伤信任的一种事（LICENSING §11）。
 */
export function LicensePage() {
  const { t } = useTranslation()
  const toLoadError = useMemo(() => makeToLoadError(t), [t])
  const q = useQuery({ queryKey: ['license'], queryFn: () => api.get<LicenseState>('/license') })
  const state = fromQuery<LicenseState>(q, () => false, toLoadError)

  return (
    <>
      <h1 className="text-xl font-semibold tracking-tight">{t('sso:console.license')}</h1>
      <p className="mt-1 mb-5 max-w-[80ch] text-sm text-muted-foreground">
        {t('sso:license.intro')}
      </p>

      <AsyncBoundary
        state={state}
        errorTitle={t('sso:license.errorTitle')}
        retryLabel={t('retry')}
        onRetry={() => void q.refetch()}
        empty={null}
        pending={<Skeleton className="h-40 w-full" />}
      >
        {(d) => (
          <div className="grid gap-4 lg:grid-cols-[1fr_20rem]">
            <div className="grid gap-4">
              <StatusCard d={d} />
              <Features features={d.features} />
              <Capacity items={d.capacity} />
            </div>
            <div className="grid content-start gap-4">
              <Fingerprint d={d} />
              <Activate />
            </div>
          </div>
        )}
      </AsyncBoundary>
    </>
  )
}

/** 状态一律带原因。只说「未授权」等于让人去猜下一步。 */
function StatusCard({ d }: { d: LicenseState }) {
  const { t } = useTranslation()
  const ok = d.status === 'active'
  const warn = d.status === 'grace' || d.status === 'finger_mismat'
  // ⚠️ 未激活**不是故障**：社区版照常可写、照常判定访问。
  // 画成红色等于告诉每个新装的客户"你的系统有问题"，
  // 而他什么都没做错。这和「失败态不能退化成空态」是同一条纪律的反面：
  // 正常态也不能被渲染成失败态。
  const neutral = d.status === 'not_activated'

  return (
    <section
      className={[
        'rounded-[var(--radius-md)] border p-4',
        ok
          ? 'border-success bg-success-bg'
          : neutral
            ? 'border-border bg-card'
            : warn
              ? 'border-warning bg-warning-bg'
              : 'border-destructive bg-danger-bg',
      ].join(' ')}
    >
      <div className="flex flex-wrap items-baseline gap-x-3 gap-y-1">
        <b className="text-sm font-semibold">{t(`sso:license.status.${d.status}`, { defaultValue: d.status })}</b>
        <span className="text-[12px] text-muted-foreground">
          {t(`sso:license.statusWhy.${d.status}`, { defaultValue: '' })}
        </span>
      </div>

      <dl className="mt-3 grid gap-1.5 text-[13px] sm:grid-cols-2">
        <Field label={t('sso:license.licensee')} value={d.licensee?.org} />
        {/* 授权范围（生产/测试/试用）要露出来：拿测试授权装到生产上
            是真实发生过的事，而两者在界面上不写就完全看不出区别。 */}
        <Field label={t('sso:license.scope')} value={d.licensee?.scope_name} />
        <Field label={t('sso:license.licenseId')} value={d.license_id} mono />
        <Field
          label={t('sso:license.expiry')}
          value={
            d.perpetual
              ? t('sso:license.perpetual')
              : d.expires_at
                ? d.days_until_expiry === undefined
                  ? d.expires_at
                  : // ⚠️ 天数会是负的（已经过期了）。套进「还有 N 天」就成了
                    // 「还有 -5 天」这种病句 —— 过期后必须换一句话说。
                    d.days_until_expiry >= 0
                    ? t('sso:license.expiryWithDays', {
                        date: d.expires_at,
                        days: d.days_until_expiry,
                      })
                    : t('sso:license.expiryPast', {
                        date: d.expires_at,
                        days: -d.days_until_expiry,
                      })
                : undefined
          }
        />
        <Field
          label={t('sso:license.writable')}
          value={d.can_write ? t('sso:license.writableYes') : t('sso:license.writableNo')}
        />
      </dl>

      {/* 到期提醒：由后端的 ShouldRemind() 决定何时开始喊，
          前端不自己算天数 —— 两边各算一套必然会分叉。 */}
      {/* ⚠️ lapsed 时不显示这条：它说的是「请尽快续期」，
          而 lapsed 的定义就是**续期已经不足以恢复**。
          状态行已经说清要重新采购，再喊一句续期只会让人白跑一趟。 */}
      {d.should_remind && d.days_until_expiry !== undefined && d.status !== 'lapsed' ? (
        <p className="mt-3 rounded-[var(--radius)] border border-warning bg-warning-bg px-3 py-2 text-[12px]">
          {d.days_until_expiry >= 0
            ? t('sso:license.remindSoon', { days: d.days_until_expiry })
            : t('sso:license.remindExpired', { days: -d.days_until_expiry })}
        </p>
      ) : null}
    </section>
  )
}

function Field({ label, value, mono }: { label: string; value?: string; mono?: boolean }) {
  const { t } = useTranslation()
  // 运行期兜底：万一后端某个字段又变成对象/数组，这里降级成一行提示，
  // 而不是让 React 抛 #31 把**整页**打没。类型是第一道防线，
  // 但类型只在编译期，后端改形状不会让前端编译失败。
  if (value != null && typeof value !== 'string') {
    return (
      <div className="flex gap-2">
        <dt className="w-24 shrink-0 text-muted-foreground">{label}</dt>
        <dd className="min-w-0 truncate text-warning">{t('sso:license.badShape')}</dd>
      </div>
    )
  }
  return (
    <div className="flex gap-2">
      <dt className="w-24 shrink-0 text-muted-foreground">{label}</dt>
      {/* 没有值就写「未激活时没有」，不要留空 —— 空白会被读成"加载没出来" */}
      <dd className={mono ? 'min-w-0 truncate font-mono text-[12px]' : 'min-w-0 truncate'}>
        {value || <span className="text-muted-foreground">{t('sso:license.notYet')}</span>}
      </dd>
    </div>
  )
}

/**
 * 功能清单。
 *
 * 三种「用不了」必须分开：**没做**（not_implemented）、**没买**（not_granted）、
 * **买了但当前状态用不了**（过期/只读）。它们的下一步完全不同：
 * 找我们、找销售、去续费。
 */
function Features({ features }: { features: FeatureReport[] }) {
  const { t } = useTranslation()
  return (
    <section className="overflow-hidden rounded-[var(--radius-md)] border border-border bg-card">
      <h2 className="border-b border-border px-4 py-2.5 text-sm font-semibold">
        {t('sso:license.features')}
      </h2>
      <table className="w-full text-[13px]">
        <tbody>
          {features.map((f) => (
            <tr key={f.feature} className="border-t border-border first:border-t-0">
              <td className="px-4 py-2.5">{t(`sso:license.feature.${f.feature}`, { defaultValue: f.feature })}</td>
              <td className="px-4 py-2.5 text-right">
                {f.usable ? (
                  <span className="rounded-[var(--radius-sm)] bg-success-bg px-2 py-0.5 text-[11px] text-success">
                    {t('sso:license.usable')}
                  </span>
                ) : (
                  <span
                    className={[
                      'rounded-[var(--radius-sm)] px-2 py-0.5 text-[11px]',
                      // 「没做」是我们的事，用中性色；「没买」是商务的事，用提示色。
                      // 两者同色的话，客户会拿着"没做"去找销售，销售拿着"没买"来找我们。
                      f.reason === 'not_implemented'
                        ? 'bg-muted text-muted-foreground'
                        : 'bg-warning-bg text-warning',
                    ].join(' ')}
                  >
                    {t(`sso:license.reason.${f.reason}`, { defaultValue: f.reason })}
                  </span>
                )}
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </section>
  )
}

function Capacity({ items }: { items: CapacityItem[] }) {
  const { t } = useTranslation()
  return (
    <section className="overflow-hidden rounded-[var(--radius-md)] border border-border bg-card">
      <h2 className="border-b border-border px-4 py-2.5 text-sm font-semibold">
        {t('sso:license.capacity')}
      </h2>
      <table className="w-full text-[13px]">
        <tbody>
          {items.map((c) => (
            <tr key={c.item} className="border-t border-border first:border-t-0">
              <td className="px-4 py-2.5">{t(`sso:license.cap.${c.item}`, { defaultValue: c.item })}</td>
              <td className="px-4 py-2.5 text-right tabular-nums">
                {/* ⚠️ 上限 0 表示「不限」，不是「一个都不给」（LICENSING §0.1）。
                    照着数字直接渲染会让人以为产品被锁死了。 */}
                {/* 三种情况要分开：数出来了 / 本来就不数 / 数失败。
                    后两者长得一样的话，一个正常状态会被当成故障去排查。 */}
                {!c.countable ? null : c.used === undefined ? (
                  <span className="text-warning">{t('sso:license.usedUnknown')}</span>
                ) : (
                  <span>{c.used}</span>
                )}
                <span className="text-muted-foreground">
                  {c.countable ? ' / ' : t('sso:license.limitOnly')}
                  {c.limit === 0 ? t('sso:license.unlimited') : c.limit}
                </span>
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </section>
  )
}

/** 安装指纹：申请授权时要发给对方的那串。必须能一键复制完整值。 */
function Fingerprint({ d }: { d: LicenseState }) {
  const { t } = useTranslation()
  const [copied, setCopied] = useState(false)
  const copy = async () => {
    try {
      // ⚠️ 复制的是**完整**指纹，不是屏幕上那个截断版 ——
      // 拿截断版去签发，会签出一份永远对不上的授权。
      await navigator.clipboard.writeText(d.fingerprint_full)
      setCopied(true)
      setTimeout(() => setCopied(false), 1500)
    } catch {
      // 非 https 下浏览器会拒掉剪贴板。不弹错：值就在屏幕上，手工选中即可
    }
  }

  return (
    <section className="rounded-[var(--radius-md)] border border-border bg-card p-4">
      <h2 className="text-sm font-semibold">{t('sso:license.fingerprint')}</h2>
      <p className="mt-1 mb-2 text-[12px] text-muted-foreground">{t('sso:license.fingerprintDesc')}</p>
      <div className="flex items-center gap-2">
        <code className="min-w-0 flex-1 overflow-x-auto rounded-[var(--radius-sm)] bg-muted px-2 py-1 font-mono text-[12px] whitespace-nowrap">
          {d.fingerprint}
        </code>
        <button
          type="button"
          onClick={() => void copy()}
          title={t('sso:license.copyFull')}
          className="shrink-0 cursor-pointer rounded-[var(--radius)] border border-border p-1.5 text-muted-foreground hover:bg-secondary"
        >
          {copied ? <Check className="size-3.5 text-success" /> : <Copy className="size-3.5" />}
        </button>
      </div>
      <p className="mt-2 text-[11px] text-muted-foreground">{t('sso:license.copyHint')}</p>
    </section>
  )
}

function Activate() {
  const { t } = useTranslation()
  const qc = useQueryClient()
  const [token, setToken] = useState('')
  const [err, setErr] = useState<string | null>(null)

  const m = useMutation({
    mutationFn: () => api.post<unknown>('/license', { token }),
    onSuccess: () => {
      setErr(null)
      setToken('')
      void qc.invalidateQueries({ queryKey: ['license'] })
    },
    onError: (e) => {
      // 失败原因必须原样说清：验签不过 / 不含本产品 / 已过期，
      // 三者的下一步完全不同。统一说"激活失败"等于让人重试到放弃。
      setErr(
        e instanceof ApiError
          ? t(`errors.${e.code}`, { ...e.params, defaultValue: e.code })
          : t('error.unreachable'),
      )
    },
  })

  return (
    <section className="rounded-[var(--radius-md)] border border-border bg-card p-4">
      <h2 className="text-sm font-semibold">{t('sso:license.activate')}</h2>
      <p className="mt-1 mb-2 text-[12px] text-muted-foreground">{t('sso:license.activateDesc')}</p>
      <textarea
        value={token}
        onChange={(e) => setToken(e.target.value)}
        rows={5}
        placeholder={t('sso:license.tokenPlaceholder')}
        className="w-full resize-y rounded-[var(--radius)] border border-border bg-background p-2 font-mono text-[11px]"
      />
      {err ? (
        <p className="mt-2 rounded-[var(--radius)] bg-danger-bg px-2 py-1.5 text-[12px] text-danger">
          {err}
        </p>
      ) : null}
      <button
        type="button"
        disabled={!token.trim() || m.isPending}
        onClick={() => m.mutate()}
        className="mt-2 w-full cursor-pointer rounded-[var(--radius)] bg-brand px-3 py-1.5 text-[13px] font-medium text-brand-foreground disabled:cursor-default disabled:opacity-50"
      >
        {m.isPending ? t('sso:license.activating') : t('sso:license.activateBtn')}
      </button>
      <p className="mt-2 text-[11px] text-muted-foreground">{t('sso:license.activateSafe')}</p>
    </section>
  )
}
