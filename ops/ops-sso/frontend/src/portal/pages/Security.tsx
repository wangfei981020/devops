import { useTranslation } from '@ops/i18n'
import { AsyncBoundary, Skeleton, fromQuery } from '@ops/ui'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Check, Copy, ShieldCheck } from 'lucide-react'
import { useMemo, useState } from 'react'
import { ApiError, api } from '../../api/client.js'
import { makeToLoadError } from '../../shared/errorText.js'

interface MFAStatus {
  enrolled: boolean
}
interface EnrollBegin {
  secret: string
  otpauth_uri: string
}

/**
 * 账号安全：二次验证。
 *
 * # 为什么绑定入口要在门户而不是控制台
 *
 * 需要绑 MFA 的是**每一个人**，不只是管理员。放进控制台等于告诉普通员工
 * "这不关你的事"，而他恰恰是最需要绑的那一批 —— 口令泄露的多数是普通账号。
 *
 * # 为什么必须先验一次再算绑定成功
 *
 * 只把密钥显示出来就当绑好了，会产生一种最难查的故障：
 * 人以为绑了，实际上验证器里的时间偏了、或者根本没扫上，
 * 直到下一次真要用 MFA 时才发现进不去 —— 而那时往往是紧急场景。
 * 所以要求当场输一次动态码，验过才落库。
 */
export function Security() {
  const { t } = useTranslation()
  const toLoadError = useMemo(() => makeToLoadError(t), [t])
  const qc = useQueryClient()
  const [begin, setBegin] = useState<EnrollBegin | null>(null)
  const [code, setCode] = useState('')
  const [err, setErr] = useState<string | null>(null)
  const [copied, setCopied] = useState(false)

  const q = useQuery({ queryKey: ['mfa'], queryFn: () => api.get<MFAStatus>('/mfa/status') })
  const state = fromQuery<MFAStatus>(q, () => false, toLoadError)

  const start = useMutation({
    mutationFn: () => api.post<EnrollBegin>('/mfa/enroll', {}),
    onSuccess: (r) => {
      setErr(null)
      setBegin(r)
    },
    onError: (e) => setErr(errText(e, t)),
  })

  const confirm = useMutation({
    mutationFn: () => api.post('/mfa/enroll/confirm', { code: code.trim() }),
    onSuccess: () => {
      setErr(null)
      setBegin(null)
      setCode('')
      void qc.invalidateQueries({ queryKey: ['mfa'] })
    },
    onError: (e) => setErr(errText(e, t)),
  })

  return (
    <>
      <h1 className="text-xl font-semibold tracking-tight">{t('sso:portal.security')}</h1>
      <p className="mt-1 mb-4 max-w-[80ch] text-sm text-muted-foreground">{t('sso:sec.intro')}</p>

      <AsyncBoundary
        state={state}
        errorTitle={t('sso:sec.errorTitle')}
        retryLabel={t('retry')}
        onRetry={() => void q.refetch()}
        empty={null}
        pending={<Skeleton className="h-28 w-full" />}
      >
        {(d) => (
          <section className="max-w-2xl rounded-[var(--radius-md)] border border-border bg-card p-4">
            <div className="flex items-center gap-2.5">
              <ShieldCheck
                className={`size-5 ${d.enrolled ? 'text-success' : 'text-muted-foreground'}`}
              />
              <div>
                <b className="block text-sm font-semibold">{t('sso:sec.totpTitle')}</b>
                <span className="text-[12px] text-muted-foreground">
                  {d.enrolled ? t('sso:sec.enrolled') : t('sso:sec.notEnrolled')}
                </span>
              </div>
            </div>

            {d.enrolled ? (
              // 已绑定就不给「重新绑定」按钮：重绑会让旧的立刻失效，
              // 而误点的人下一次登录就进不来了。真要换设备，走管理员重置 ——
              // 那条路上有人工确认，也留审计。
              <p className="mt-3 rounded-[var(--radius)] bg-muted px-3 py-2 text-[12px] text-muted-foreground">
                {t('sso:sec.rebindHint')}
              </p>
            ) : begin ? (
              <div className="mt-4">
                <p className="text-[13px]">{t('sso:sec.step1')}</p>
                <div className="mt-2 flex items-center gap-2">
                  <code className="min-w-0 flex-1 overflow-x-auto rounded-[var(--radius-sm)] bg-muted px-2 py-1.5 font-mono text-[13px] tracking-wider whitespace-nowrap">
                    {begin.secret}
                  </code>
                  <button
                    type="button"
                    onClick={() => {
                      void navigator.clipboard.writeText(begin.secret).then(
                        () => {
                          setCopied(true)
                          setTimeout(() => setCopied(false), 1500)
                        },
                        () => {
                          /* 非 https 下浏览器拒绝剪贴板。值就在屏幕上，手工选中即可 */
                        },
                      )
                    }}
                    className="shrink-0 cursor-pointer rounded-[var(--radius)] border border-border p-1.5 text-muted-foreground hover:bg-secondary"
                  >
                    {copied ? (
                      <Check className="size-3.5 text-success" />
                    ) : (
                      <Copy className="size-3.5" />
                    )}
                  </button>
                </div>
                {/* 不画二维码：那需要引一个库，而手工输入密钥同样可行。
                    给 otpauth:// 链接，支持的验证器能直接打开。 */}
                <p className="mt-1.5 text-[11px] text-muted-foreground">{t('sso:sec.secretHint')}</p>

                <p className="mt-4 text-[13px]">{t('sso:sec.step2')}</p>
                <div className="mt-2 flex flex-wrap items-center gap-2">
                  <input
                    value={code}
                    onChange={(e) => setCode(e.target.value.replace(/\D/g, '').slice(0, 6))}
                    inputMode="numeric"
                    placeholder="000000"
                    className="w-32 rounded-[var(--radius)] border border-border bg-background px-2.5 py-1.5 text-center font-mono text-[15px] tracking-[0.3em]"
                  />
                  <button
                    type="button"
                    disabled={code.length !== 6 || confirm.isPending}
                    onClick={() => confirm.mutate()}
                    className="cursor-pointer rounded-[var(--radius)] bg-brand px-3 py-1.5 text-[13px] font-medium text-brand-foreground disabled:cursor-default disabled:opacity-50"
                  >
                    {confirm.isPending ? t('sso:sec.confirming') : t('sso:sec.confirm')}
                  </button>
                  <button
                    type="button"
                    onClick={() => {
                      setBegin(null)
                      setCode('')
                      setErr(null)
                    }}
                    className="cursor-pointer rounded-[var(--radius)] border border-border px-3 py-1.5 text-[13px] hover:bg-secondary"
                  >
                    {t('sso:sec.cancel')}
                  </button>
                </div>
                <p className="mt-2 text-[11px] text-muted-foreground">{t('sso:sec.confirmWhy')}</p>
              </div>
            ) : (
              <div className="mt-4">
                <p className="mb-2 text-[12px] text-muted-foreground">{t('sso:sec.whyBind')}</p>
                <button
                  type="button"
                  disabled={start.isPending}
                  onClick={() => start.mutate()}
                  className="cursor-pointer rounded-[var(--radius)] bg-brand px-3 py-1.5 text-[13px] font-medium text-brand-foreground disabled:opacity-50"
                >
                  {start.isPending ? t('sso:sec.starting') : t('sso:sec.start')}
                </button>
              </div>
            )}

            {err ? (
              <p className="mt-3 rounded-[var(--radius)] bg-danger-bg px-3 py-2 text-[12px] text-danger">
                {err}
              </p>
            ) : null}
          </section>
        )}
      </AsyncBoundary>
    </>
  )
}

function errText(e: unknown, t: (k: string, o?: Record<string, unknown>) => string): string {
  return e instanceof ApiError
    ? t(`errors.${e.code}`, { ...e.params, defaultValue: e.code })
    : t('error.unreachable')
}
