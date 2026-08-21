import { toErrorInfo } from '@ops/api'
import { Badge, Banner, Button, Dialog, Skeleton } from '@ops/ui'
import { useEffect } from 'react'
import { type CdnProbeAccount, type CdnProbeCheck, useCdnTokenCheck } from './queries.js'

type TFn = (k: string, o?: Record<string, unknown>) => string

/**
 * CDN token 权限体检。
 *
 * # 这个弹窗回答的问题
 *
 * 「CDN 相关的数据为什么是空的 / 为什么某一列显示获取失败」——
 * 绝大多数时候答案是 **token 少勾了一个权限**，而这件事在别的任何页面上
 * 都看不出来：列表只会显示「明细获取失败: 403」或者干脆是空的。
 *
 * # ⚠️ 它和这一页其它数据口径不同
 *
 * 其它 `/api/cdn/*` 读的是**上次同步落库的快照**；这一个**实时打 Cloudflare**。
 * 改完 CF 权限后去查快照，看到的仍是旧的 403，据此会得出
 * 「权限没生效」的错误结论 —— 本项目踩过一次，所以体检必须绕开快照。
 *
 * 因此它**只在用户点开弹窗时请求一次**，不预取、不轮询：
 * 它直接打到外部服务，后端给了 90 秒超时。
 */
export function CdnTokenCheckDialog({ onClose, t }: { onClose: () => void; t: TFn }) {
  const probe = useCdnTokenCheck()

  // 打开即探一次。⚠️ 依赖数组必须是空的 —— 这是「用户主动触发」的那一次，
  // 不是随渲染反复触发的预取
  // biome-ignore lint/correctness/useExhaustiveDependencies: 只在挂载时探一次，见上
  useEffect(() => {
    probe.mutate(undefined)
  }, [])

  const d = probe.data

  return (
    <Dialog
      open
      onClose={onClose}
      title={t('cdn:tokenCheck.title')}
      description={t('cdn:tokenCheck.desc')}
      closeLabel={t('common:action.close')}
      width={900}
      footer={
        <>
          <Button size="sm" loading={probe.isPending} onClick={() => probe.mutate(undefined)}>
            {t('cdn:tokenCheck.recheck')}
          </Button>
          <Button size="sm" variant="primary" onClick={onClose}>
            {t('common:action.close')}
          </Button>
        </>
      }
    >
      {probe.isPending ? (
        <>
          <Skeleton className="h-5 w-[60%]" />
          {/* 一次全账号体检可能要几十秒（后端 90s 超时）。不说的话，
              用户会以为卡住了然后反复点 —— 而每一次点都是真的打 CF */}
          <p className="mt-2 text-xs text-muted-foreground">{t('cdn:tokenCheck.slow')}</p>
        </>
      ) : probe.isError ? (
        <Banner tone="bad">
          <span className="font-medium">{t('cdn:tokenCheck.failed')}</span>
          <span className="mt-0.5 block break-all">
            {toErrorInfo(probe.error).detail || t(toErrorInfo(probe.error).messageKey)}
          </span>
        </Banner>
      ) : d?.ok === false ? (
        // 后端给的业务级失败（如「没有配置任何 CDN 账号」）—— 原样显示，
        // 它比前端能编的任何文案都准确
        <Banner tone="warn">
          <span>{d.error}</span>
        </Banner>
      ) : (
        <div className="flex flex-col gap-3">
          {/* 🔴 口径必须写在最前面。这一页别的数据都是快照，只有这里是实时，
              不说清楚的话「体检通过了但列表还是 403」会被当成 bug */}
          {d?.note ? (
            <Banner tone="info">
              <span>{d.note}</span>
            </Banner>
          ) : null}

          {(d?.accounts ?? []).map((a) => (
            <AccountResult key={a.account_id ?? a.provider} a={a} t={t} />
          ))}

          {/* 一个账号都没返回 ≠ 都正常。接口给了空数组时要说出来，
              不能渲染成一片空白让人以为"没问题" */}
          {(d?.accounts ?? []).length === 0 ? (
            <p className="text-xs text-warning">{t('cdn:tokenCheck.noAccounts')}</p>
          ) : null}
        </div>
      )}
    </Dialog>
  )
}

function AccountResult({ a, t }: { a: CdnProbeAccount; t: TFn }) {
  // 连 client 都没拿到：没配 token，或者这个厂商还没实现体检。
  // ⚠️ 这不是「没问题」，必须和"体检通过"区分开
  if (a.ok === false) {
    return (
      <section className="rounded-[var(--radius)] border border-border p-3">
        <div className="flex items-baseline gap-2">
          <span className="text-[13px] font-medium text-foreground">{a.provider}</span>
          <Badge tone="bad">{t('cdn:tokenCheck.cannotProbe')}</Badge>
        </div>
        <p className="mt-1 break-all text-xs text-danger">{a.error}</p>
      </section>
    )
  }

  const p = a.probe
  const checks = p?.checks ?? []
  const failed = checks.filter((c) => c.ok !== true)

  return (
    <section className="rounded-[var(--radius)] border border-border p-3">
      <div className="flex flex-wrap items-baseline gap-2">
        <span className="text-[13px] font-medium text-foreground">{a.provider}</span>
        {p?.token_state ? (
          <Badge tone={p.token_state === 'active' ? 'ok' : 'bad'}>{p.token_state}</Badge>
        ) : null}
        {failed.length === 0 ? (
          <Badge tone="ok">{t('cdn:tokenCheck.allPass', { n: checks.length })}</Badge>
        ) : (
          <Badge tone="bad">{t('cdn:tokenCheck.someFail', { n: failed.length })}</Badge>
        )}
        {/* token_id 是拿去和 CF 控制台对照的 —— 客户有多个 token 时，
            不给 id 就不知道该去改哪一个 */}
        {p?.token_id ? (
          <code className="font-mono text-[11px] text-muted-foreground">{p.token_id}</code>
        ) : null}
      </div>

      {p?.summary ? <p className="mt-1 text-xs text-muted-foreground">{p.summary}</p> : null}
      <p className="mt-0.5 text-[11px] text-muted-foreground">
        {t('cdn:tokenCheck.probedAt', {
          zone: p?.probed_zone || t('common:state.unknown'),
          at: p?.probed_at || t('common:state.unknown'),
        })}
        {p?.expires_on ? ` · ${t('cdn:tokenCheck.expiresOn', { at: p.expires_on })}` : ''}
      </p>

      {checks.length === 0 ? (
        // 没有任何检查项 ≠ 全通过
        <p className="mt-2 text-xs text-warning">{t('cdn:tokenCheck.noChecks')}</p>
      ) : (
        <ul className="mt-2 flex flex-col gap-1.5">
          {/* 不通过的排前面：人是来找"哪一项没勾"的 */}
          {[...checks]
            .sort((x, y) => Number(x.ok === true) - Number(y.ok === true))
            .map((c) => (
              <CheckRow key={`${c.name}-${c.endpoint}`} c={c} t={t} />
            ))}
        </ul>
      )}
    </section>
  )
}

function CheckRow({ c, t }: { c: CdnProbeCheck; t: TFn }) {
  const ok = c.ok === true
  return (
    <li className="flex gap-2 text-xs">
      <span className={`mt-0.5 shrink-0 ${ok ? 'text-success' : 'text-danger'}`}>
        {ok ? '✓' : '✗'}
      </span>
      <div className="min-w-0 flex-1">
        <div className="flex flex-wrap items-baseline gap-1.5">
          <span className="text-foreground">{c.name}</span>
          {/* http_code 0 = 请求根本没发出去。⚠️ 与「发出去了但被拒」是两回事：
              前者查网络/地址，后者查权限。压成同一种显示会让人查错方向 */}
          {c.http_code !== undefined && c.http_code !== 0 ? (
            <code className="font-mono text-[11px] text-muted-foreground">
              HTTP {c.http_code}
            </code>
          ) : c.http_code === 0 ? (
            <span className="text-[11px] text-warning">{t('cdn:tokenCheck.notSent')}</span>
          ) : null}
        </div>
        {!ok ? (
          <>
            {/* 🔴 失败要分类型。
                后端给的 verdict 有 no_permission 和 error 两种，
                下一步动作完全不同：前者去 CF 控制台勾一个权限项，
                后者是请求本身出了问题（令牌无效 / 网络不通），勾权限没用。
                只显示一个 ✗ 会把这两件事压成一件，人只能挨个试。 */}
            {c.verdict && c.verdict !== 'pass' ? (
              <p className="mt-0.5">
                <span className="rounded bg-warning-bg px-1.5 py-0.5 text-[11px] text-warning">
                  {t(`cdn:tokenCheck.verdict.${c.verdict}`, { defaultValue: c.verdict })}
                </span>
              </p>
            ) : null}
            {/* 🔴 「要勾哪一项」用的是 CF 控制台的**原文**，
                翻译过来反而找不到 —— 客户是拿着这句话去界面上比对的 */}
            {c.need ? (
              <p className="mt-0.5 text-muted-foreground">
                {t('cdn:tokenCheck.need')}: <code className="font-mono">{c.need}</code>
              </p>
            ) : null}
            {/* 「所以呢」——不通会导致哪个能力没数据。
                只说"权限不足"，人不知道值不值得去改 */}
            {c.impact ? <p className="mt-0.5 text-warning">{c.impact}</p> : null}
            {c.detail ? (
              <p className="mt-0.5 break-all text-muted-foreground">{c.detail}</p>
            ) : null}
          </>
        ) : null}
      </div>
    </li>
  )
}
