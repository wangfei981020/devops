import { useTranslation } from '@ops/i18n'
import { ShieldCheck } from 'lucide-react'
import type { ReactNode } from 'react'
import { Preferences } from './Preferences.js'
import { useMe } from './session.js'
import { UserMenu } from './UserMenu.js'

/**
 * 门户与控制台共用的外壳。
 *
 * # 为什么两边导航形态不同
 *
 * 一开始两边都用顶栏，控制台加到 9 项时英文下就放不下了（被迫加横向滚动），
 * 而它还要再长 5 项。所以：
 *
 *   控制台 → **左侧栏**。项目多、还会长、每项是一个工作区；
 *            同类管理端（Keycloak / Authentik / Okta）全是这个形态。
 *   门户   → **顶栏**。只有 4 项，主体是磁贴网格，要的是横向空间；
 *            给 4 个链接切掉 15rem 宽度不划算。
 *
 * 形态不同但**顶栏保持一致**（品牌、偏好、用户菜单同一套）——
 * 管理员先是员工再是管理员，两边的"我是谁、怎么退出"必须在同一个位置。
 */
export function Shell({
  nav,
  sidebar,
  children,
  /** 门户传 true：顶栏给管理员一个进控制台的按钮。 */
  portal,
}: {
  /** 顶栏导航（门户用） */
  nav?: ReactNode
  /** 侧边栏导航（控制台用）。两者只该传一个。 */
  sidebar?: ReactNode
  children: ReactNode
  portal?: boolean
}) {
  const { t } = useTranslation()
  // Shell 一定在 AuthGate 里面，所以这里必定已经有身份；
  // 用同一个 query key，不会多发一次请求。
  const { data: me } = useMe()

  return (
    <div className="min-h-screen bg-background text-foreground">
      <header className="sticky top-0 z-20 flex h-13 items-center gap-3 border-b border-border bg-surface px-4 sm:px-6">
        <span className="grid size-6.5 shrink-0 place-items-center rounded-[var(--radius)] bg-primary text-[11px] font-semibold text-primary-foreground">
          SSO
        </span>
        <b className="shrink-0 text-sm font-semibold">{t('sso:brand')}</b>
        {nav}
        <span className="flex-1" />

        {/* 管理后台给**独立按钮**，不藏在头像菜单里。
            管理员一天要进好几次，每次两步点击（点头像 → 点菜单项）
            是纯粹的摩擦；而它对普通用户不显示，也不占谁的位置。 */}
        {portal && me?.is_admin ? (
          <a
            href="/console"
            className="flex shrink-0 items-center gap-1.5 rounded-[var(--radius)] border border-border px-2.5 py-1 text-[12px] hover:bg-secondary"
          >
            <ShieldCheck className="size-3.5 text-muted-foreground" />
            <span className="hidden sm:inline">{t('sso:user.toConsole')}</span>
          </a>
        ) : null}

        <Preferences />
        {me ? <UserMenu me={me} showConsoleLink={portal} /> : null}
      </header>

      {sidebar ? (
        <div className="flex">
          {/* sticky + 自带滚动：菜单长过一屏时它自己滚，不跟着正文走 */}
          <aside className="sticky top-13 hidden h-[calc(100vh-3.25rem)] w-56 shrink-0 overflow-y-auto border-r border-border bg-surface p-3 md:block">
            {sidebar}
          </aside>
          {/* 窄屏放不下侧栏：横向滚的一条，而不是把正文挤没 */}
          <div className="min-w-0 flex-1">
            <div className="border-b border-border bg-surface p-2 md:hidden">{sidebar}</div>
            <main className="mx-auto max-w-[1360px] p-4 sm:p-6">{children}</main>
          </div>
        </div>
      ) : (
        <main className="mx-auto max-w-[1360px] p-4 sm:p-6">{children}</main>
      )}
    </div>
  )
}
