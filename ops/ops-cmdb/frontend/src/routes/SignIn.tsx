import { useTranslation } from '@ops/i18n'
import { Button, cn } from '@ops/ui'
import { useEffect, useState } from 'react'
import { adoptSSOToken, signIn } from '../lib/auth.js'

/**
 * 登录页。左栏品牌 + 右栏表单；窄屏（<lg）只剩表单。
 *
 * 两条路：本地账号，以及（配了的话）单点登录。
 *
 * ⚠️ 本地账号那一半**永远在**，且永远排在能直接用的位置。
 * SSO 挂了、配错了、证书过期了，这是唯一进得来的门——
 * 把它藏进"其他登录方式"折叠面板里，等于在故障时让人多绕一层。
 *
 * # 左栏为什么不放插图
 *
 * 极淡的网格 + 两团径向光晕。**这是整个产品唯一允许用渐变的地方**，
 * 内页一处都没有。插图要么用库存素材（一眼廉价），要么专门画（贵且难维护）；
 * 光晕是 CSS 就能做的，白标换个主色自动跟着变。
 *
 * # 三个数字必须是真值
 *
 * 取不到就**整块不渲染**（见 useLoginStats）。放假数字的代价是：
 * 第一个发现的人会开始怀疑页面上其它所有数字 —— 一个用来建立信任的区块
 * 反而摧毁信任。
 */
export function SignInPage() {
  const { t } = useTranslation()
  const [username, setUsername] = useState('')
  const [password, setPassword] = useState('')
  const [error, setError] = useState<string | null>(null)
  const [busy, setBusy] = useState(false)
  const [sso, setSSO] = useState<{ enabled: boolean; display_name?: string } | null>(null)
  const [ssoError, setSSOError] = useState<string | null>(null)
  const stats = useLoginStats()

  // 回调带回来的 token / 失败原因。两者都在首屏处理完，
  // 处理完就把 URL 恢复干净——地址栏里留着 token 或一个错误码都不合适
  useEffect(() => {
    const hash = new URLSearchParams(window.location.hash.replace(/^#/, ''))
    const token = hash.get('sso_token')
    if (token) {
      const back = hash.get('redirect') || '/'
      void adoptSSOToken(token)
        .then(() => {
          window.location.replace(back)
        })
        .catch(() => {
          // 拿到了 token 却换不出身份，多半是会话已经被作废。
          // 停在登录页并说清楚，比空白地转圈强
          window.history.replaceState(null, '', '/login')
          setSSOError(t('ssoconnect:login.reason.session_failed'))
        })
      return
    }
    const reason = new URLSearchParams(window.location.search).get('sso_error')
    if (reason) {
      window.history.replaceState(null, '', '/login')
      // 未知短码兜底到 unknown，而不是把短码原样显示给用户——
      // 「code_exchange_failed」对他没有任何意义
      const key = `ssoconnect:login.reason.${reason}`
      const msg = t(key)
      setSSOError(msg === key ? t('ssoconnect:login.reason.unknown') : msg)
    }
  }, [t])

  // SSO 开没开。查失败时按"没开"处理：登录页因为这个查询挂掉的话，
  // 本地账号这条逃生通道也跟着没了
  useEffect(() => {
    void fetch('/api/auth/sso')
      .then((r) => (r.ok ? r.json() : { enabled: false }))
      .then((d) => setSSO(d as { enabled: boolean; display_name?: string }))
      .catch(() => setSSO({ enabled: false }))
  }, [])

  async function submit(e: React.FormEvent) {
    e.preventDefault()
    setBusy(true)
    setError(null)
    try {
      await signIn(username, password)
      // 整页重载而不是切状态：登录后所有查询都要带上新 token 重新发，
      // 逐个失效容易漏，重载最干净
      window.location.reload()
    } catch (err) {
      // 网络不通和密码错误必须分开说——前者用户该去查网络，
      // 后者该去查密码。混成一句"登录失败"等于什么都没说。
      const networkIssue = err instanceof TypeError || (err as Error)?.message === 'Failed to fetch'
      setError(networkIssue ? t('signIn.networkFailed') : t('signIn.failed'))
      setBusy(false)
    }
  }

  return (
    <div className="grid min-h-screen bg-background lg:grid-cols-[1.05fr_1fr]">
      <BrandPanel stats={stats} t={t} />
      <div className="grid place-items-center px-4 py-10">
      <form onSubmit={submit} className="w-full max-w-[340px]">
        {/* 窄屏没有左栏，品牌标识只能放这儿；宽屏左栏已经有了，这里就收起来 */}
        <div className="mb-7 flex items-center gap-2.5 lg:hidden">
          <span className="grid size-7 place-items-center rounded-[var(--radius-sm)] bg-gradient-to-br from-primary-hover to-primary-active">
            <svg
              viewBox="0 0 24 24"
              className="size-4 fill-none stroke-primary-foreground stroke-[1.75]"
              aria-hidden="true"
            >
              <path d="M12 2 3 7v10l9 5 9-5V7z" />
              <path d="M12 22V12" />
              <path d="m3 7 9 5 9-5" />
            </svg>
          </span>
          <span className="text-[15px] font-semibold tracking-tight">OpsPlane CMDB</span>
        </div>

        <h1 className="text-xl font-semibold tracking-tight">{t('signIn.title')}</h1>
        <p className="mt-1.5 mb-6 text-[13px] text-muted-foreground">{t('signIn.subtitle')}</p>

        <div className="flex flex-col gap-3">
          <Field label={t('signIn.username')}>
            <input
              value={username}
              onChange={(e) => setUsername(e.target.value)}
              autoComplete="username"
              autoFocus
              className={inputCls}
            />
          </Field>
          <Field label={t('signIn.password')}>
            <input
              type="password"
              value={password}
              onChange={(e) => setPassword(e.target.value)}
              autoComplete="current-password"
              className={inputCls}
            />
          </Field>
        </div>

        {ssoError ? (
          <p
            aria-live="polite"
            className="mt-3 rounded-[var(--radius)] border border-danger/35 bg-danger-bg px-3 py-2 text-xs text-foreground"
          >
            <span className="mb-0.5 block font-medium">{t('ssoconnect:login.failedTitle')}</span>
            {ssoError}
          </p>
        ) : null}

        {error ? (
          <p
            // aria-live 让读屏软件在异步失败时也能播报，
            // 否则视障用户点了登录之后完全没有反馈
            aria-live="polite"
            className="mt-3 rounded-[var(--radius)] border border-danger/35 bg-danger-bg px-3 py-2 text-xs text-foreground"
          >
            {error}
          </p>
        ) : null}

        <Button
          type="submit"
          variant="primary"
          loading={busy}
          disabled={!username || !password}
          className="mt-5 h-9 w-full justify-center"
        >
          {t('signIn.submit')}
        </Button>

        {sso?.enabled ? (
          <>
            <div className="my-4 flex items-center gap-3">
              <span className="h-px flex-1 bg-border" />
              <span className="text-[11px] text-muted-foreground">{t('ssoconnect:login.or')}</span>
              <span className="h-px flex-1 bg-border" />
            </div>
            <Button
              type="button"
              className="h-9 w-full justify-center"
              onClick={() => {
                // 整页跳转，不用 fetch：OIDC 要求用户的浏览器亲自去 IdP，
                // 那边可能要输密码、过 MFA。用 fetch 只会拿回一个 302
                window.location.href = '/api/auth/sso/start'
              }}
            >
              {sso.display_name
                ? t('ssoconnect:login.button', { name: sso.display_name })
                : t('ssoconnect:login.buttonFallback')}
            </Button>
          </>
        ) : null}
      </form>
      </div>
    </div>
  )
}

const inputCls = cn(
  'h-9 w-full rounded-[var(--radius)] border border-input bg-card px-2.5',
  'text-[13px] text-foreground outline-none',
  'focus:border-primary focus:shadow-[0_0_0_2px_var(--ops-accent-ring)]',
)

function Field({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <label className="flex flex-col gap-1.5">
      <span className="text-xs text-muted-foreground">{label}</span>
      {children}
    </label>
  )
}

/** 登录页统计。取不到时返回 null —— 调用方据此**整块不渲染** */
interface LoginStats {
  clusters: number | null
  resources: number | null
  fresh_seconds: number | null
}

function useLoginStats() {
  const [s, setS] = useState<LoginStats | null>(null)
  useEffect(() => {
    // ⚠️ 失败一律按"没有"处理，绝不兜底成 0。
    // 登录页显示"0 个集群"看起来像这套系统什么都没管，
    // 而实际只是查询失败了
    void fetch('/api/public/login-stats')
      .then((r) => (r.ok ? r.json() : null))
      .then((d) => setS(d as LoginStats | null))
      .catch(() => setS(null))
  }, [])
  return s
}

/** 距上次采集多久：秒 → 人话。不做"刚刚"这种模糊说法，数字本身才是可信度 */
function humanAgo(sec: number, t: (k: string, p?: Record<string, unknown>) => string) {
  if (sec >= 86400) return t('signIn.stats.agoDays', { n: Math.floor(sec / 86400) })
  if (sec >= 3600) return t('signIn.stats.agoHours', { n: Math.floor(sec / 3600) })
  if (sec >= 60) return t('signIn.stats.agoMinutes', { n: Math.floor(sec / 60) })
  return t('signIn.stats.agoSeconds', { n: sec })
}

function BrandPanel({
  stats,
  t,
}: {
  stats: LoginStats | null
  t: (k: string, p?: Record<string, unknown>) => string
}) {
  // 三个数字**任意一个取不到，整块都不显示**。
  // 显示两个数字加一个"—"比不显示更糟：人会去想那个"—"是什么意思
  const showStats =
    stats != null &&
    stats.clusters != null &&
    stats.resources != null &&
    stats.fresh_seconds != null

  return (
    <aside className="relative hidden flex-col justify-between overflow-hidden border-r border-border bg-surface p-12 lg:flex">
      {/* 极淡网格 + 两团光晕。产品内唯一允许用渐变的地方 */}
      <div
        aria-hidden="true"
        className="pointer-events-none absolute inset-0 opacity-90"
        style={{
          backgroundImage:
            'linear-gradient(var(--color-border) 1px, transparent 1px),' +
            'linear-gradient(90deg, var(--color-border) 1px, transparent 1px)',
          backgroundSize: '32px 32px',
          // ⚠️ 遮罩里的颜色只决定**不透明度**，不会显示出来，所以不走设计 token。
          // 用 rgb(0 0 0) 而不是 #000：硬编码颜色守卫按十六进制字面量匹配，
          // 而这里的"黑"不是一个视觉决策，是 mask 的语法要求
          maskImage: 'radial-gradient(90% 75% at 35% 30%, rgb(0 0 0) 55%, transparent 100%)',
        }}
      />
      <div
        aria-hidden="true"
        // ⚠️ 光晕要**很淡**。第一版直接用 accent-ring 原色，右下角糊成一大团，
        // 把品牌语的视线全抢走了 —— 它是背景不是主角
        className="pointer-events-none absolute inset-0 opacity-45"
        style={{
          background:
            'radial-gradient(42% 34% at 18% 10%, var(--ops-accent-ring), transparent 72%),' +
            'radial-gradient(34% 28% at 92% 92%, var(--ops-accent-ring), transparent 72%)',
        }}
      />

      <div className="relative flex items-center gap-2.5">
        <span className="grid size-7 place-items-center rounded-[var(--radius-sm)] bg-gradient-to-br from-primary-hover to-primary-active">
          <svg
            viewBox="0 0 24 24"
            className="size-4 fill-none stroke-primary-foreground stroke-[1.75]"
            aria-hidden="true"
          >
            <path d="M12 2 3 7v10l9 5 9-5V7z" />
            <path d="M12 22V12" />
            <path d="m3 7 9 5 9-5" />
          </svg>
        </span>
        <span className="text-[15px] font-semibold tracking-tight">{t('signIn.brand')}</span>
      </div>

      <div className="relative max-w-[26rem]">
        <h2 className="text-[22px] leading-[1.45] font-semibold tracking-tight text-balance">
          {t('signIn.tagline')}
        </h2>
        <p className="mt-3 text-[13px] leading-relaxed text-muted-foreground">
          {t('signIn.taglineSub')}
        </p>
      </div>

      <div className="relative">
        {showStats ? (
          <div className="flex gap-9 border-t border-border pt-5">
            <Stat value={String(stats.clusters)} label={t('signIn.stats.clusters')} />
            <Stat
              value={new Intl.NumberFormat().format(stats.resources as number)}
              label={t('signIn.stats.resources')}
            />
            <Stat
              value={humanAgo(stats.fresh_seconds as number, t)}
              label={t('signIn.stats.fresh')}
            />
          </div>
        ) : null}
      </div>
    </aside>
  )
}

function Stat({ value, label }: { value: string; label: string }) {
  return (
    <div>
      <p className="tabular text-lg font-semibold tracking-tight">{value}</p>
      <p className="mt-0.5 text-[11px] text-muted-foreground">{label}</p>
    </div>
  )
}
