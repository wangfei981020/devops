import { useTranslation } from '@ops/i18n'
import { useQuery } from '@tanstack/react-query'

/**
 * 「你现在这个地址不是签发方地址」的提示。
 *
 * # 这是在解决什么
 *
 * 会话 Cookie 是**按主机名**存的。同一个服务用两个地址打开
 * （`localhost:30834` 和 `192.168.55.101:30834`），浏览器当成两回事：
 * 在前者登录，Cookie 落在 `localhost` 上。
 *
 * 而下游应用是被送到 **issuer 地址**（`192.168.55.101`）来登录的，
 * 那边没有那个 Cookie —— 于是人明明刚在控制台登录过，
 * 从 Harbor 点过来还是被要求再登一次。
 *
 * 现象是「登录不生效」，真因是「你登在了另一个地址上」。
 * 这中间没有任何线索能让人自己想到，所以必须由界面说出来。
 *
 * # 为什么不干脆自动跳转
 *
 * 自动跳会把一次「地址写错了」变成一次「莫名其妙被弹走」，
 * 而且并非所有多地址访问都是错的（将来可能有多个入口）。
 * 给出判断依据和一键切换，比替人做决定好。
 *
 * # 判据取自 discovery，不是写死的
 *
 * `/.well-known/openid-configuration` 是公开端点（登录页也能读），
 * 里面的 issuer 就是下游被送去的那个地址 —— 与其在前端另存一份配置，
 * 不如直接读那个**下游实际使用的**值，两处不可能不一致。
 */
export function IssuerHostNotice() {
  const { t } = useTranslation()

  const q = useQuery({
    queryKey: ['discovery-issuer'],
    queryFn: async () => {
      const r = await fetch('/.well-known/openid-configuration')
      if (!r.ok) throw new Error(String(r.status))
      return (await r.json()) as { issuer: string }
    },
    staleTime: 5 * 60 * 1000,
    retry: false,
  })

  // 读不到就不显示。
  // ⚠️ 这里刻意不走"失败要显示错误态"那条规矩：这是一条**提醒**，
  // 不是一个状态展示。读不到 discovery 只说明我们无法判断地址对不对，
  // 而在这个位置弹一条红色的失败，会盖过用户正在做的事（登录），
  // 并且对他毫无可操作性。真正该报错的地方是接入信息那一屏。
  if (!q.data?.issuer) return null

  let issuerOrigin: string
  try {
    issuerOrigin = new URL(q.data.issuer).origin
  } catch {
    return null
  }
  if (issuerOrigin === window.location.origin) return null

  const target = issuerOrigin + window.location.pathname + window.location.search

  return (
    <div className="mb-3 rounded-[var(--radius)] border border-warning bg-warning-bg px-3 py-2 text-[12px]">
      <b className="block font-medium text-warning">{t('sso:issuerHost.title')}</b>
      <span className="mt-0.5 block text-foreground">
        {t('sso:issuerHost.body', { issuer: issuerOrigin, current: window.location.origin })}
      </span>
      <a
        href={target}
        className="mt-1.5 inline-block rounded-[var(--radius)] border border-border px-2 py-1 text-[11px] hover:bg-secondary"
      >
        {t('sso:issuerHost.switch', { issuer: issuerOrigin })}
      </a>
    </div>
  )
}
