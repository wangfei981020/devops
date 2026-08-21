import { useTranslation } from '@ops/i18n'
import { ErrorBoundary, ErrorState, NoPermission } from '@ops/ui'
import {
  Boxes, Building2, Clock, FileText, Grid3x3, Inbox, KeyRound, KeySquare, LayoutDashboard, LifeBuoy, Route, ShieldCheck, Users,
} from 'lucide-react'
import { type ReactNode, useState } from 'react'
import { AuthGate } from '../shared/AuthGate.js'
import { useMe } from '../shared/session.js'
import { Shell } from '../shared/Shell.js'
import { AppsPage } from './pages/Apps.js'
import { AuditPage } from './pages/Audit.js'
import { IdPsPage } from './pages/IdPs.js'
import { LicensePage } from './pages/License.js'
import { MatrixPage } from './pages/Matrix.js'
import { OIDCClientsPage } from './pages/OIDCClients.js'
import { OverviewPage } from './pages/Overview.js'
import { PeoplePage } from './pages/People.js'
import { PathRulesPage } from './pages/PathRules.js'
import { PoliciesPage } from './pages/Policies.js'
import { RequestsPage } from './pages/Requests.js'
import { ResiliencePage } from './pages/Resilience.js'
import { SessionsPage } from './pages/Sessions.js'

/**
 * 控制台。
 *
 * 导航用组件状态而不是路由：现在只有两页有实现，引路由等于为一个
 * 还没定型的信息架构先付出代价。等页面多起来、需要分享链接时再换 ——
 * 那时才知道 URL 该长什么样。
 */
const NAV = [
  { key: 'overview', icon: LayoutDashboard, ready: true },
  { key: 'apps', icon: Boxes, ready: true },
  { key: 'policies', icon: ShieldCheck, ready: true },
  { key: 'pathRules', icon: Route, ready: true },
  { key: 'oidcClients', icon: KeySquare, ready: true },
  { key: 'idps', icon: Building2, ready: true },
  { key: 'identitiesMatrix', icon: Grid3x3, ready: true },
  { key: 'identities', icon: Users, ready: true },
  { key: 'sessions', icon: Clock, ready: true },
  { key: 'requests', icon: Inbox, ready: true },
  { key: 'audit', icon: FileText, ready: true },
  { key: 'resilience', icon: LifeBuoy, ready: true },
  { key: 'license', icon: KeyRound, ready: true },
] as const

/**
 * 侧栏分段。
 *
 * 顺序不是随便排的：**先看后配再管**。
 * 出事时的动线是 总览 → 授权关系/审计（看发生了什么）→ 策略（改），
 * 而"管"那一段（人员/会话/授权）是低频的，放最后。
 */
const SECTIONS = [
  { title: 'watch', items: ['overview', 'identitiesMatrix', 'audit'] },
  { title: 'configure', items: ['apps', 'oidcClients', 'idps', 'policies', 'pathRules', 'resilience'] },
  { title: 'manage', items: ['requests', 'identities', 'sessions', 'license'] },
] as const

export function ConsoleApp() {
  return (
    <AuthGate>
      <ConsoleInner />
    </AuthGate>
  )
}

/**
 * 非管理员进到控制台时给**一页**说明，而不是让他把九个页面各点一遍、
 * 每个都撞一个红色的 403。
 *
 * ⚠️ 这只是体验层。控制台仍然是可访问的路径，真正的拦截在后端
 * adminOnly 中间件里 —— 不给入口不等于进不去，接口才是门。
 * 生产上建议再加一层：在网关/Ingress 上把 /console 限到内网或办公网段。
 */
function ConsoleInner() {
  const { t } = useTranslation()
  const { data: me } = useMe()
  const [active, setActive] = useState<string>('overview')

  if (me && !me.is_admin) {
    return (
      <Shell>
        <NoPermission
          title={t('sso:console.notAdminTitle')}
          reason={t('sso:console.notAdminReason')}
          code="role: admin"
          copyLabel={t('sso:console.copyForAdmin')}
          copiedLabel={t('sso:console.copied')}
        />
      </Shell>
    )
  }

  let page: ReactNode
  if (active === 'overview') page = <OverviewPage onGo={setActive} />
  else if (active === 'apps') page = <AppsPage />
  else if (active === 'policies') page = <PoliciesPage />
  else if (active === 'identitiesMatrix') page = <MatrixPage />
  else if (active === 'audit') page = <AuditPage />
  else if (active === 'requests') page = <RequestsPage />
  else if (active === 'pathRules') page = <PathRulesPage />
  else if (active === 'oidcClients') page = <OIDCClientsPage />
  else if (active === 'idps') page = <IdPsPage />
  else if (active === 'resilience') page = <ResiliencePage />
  else if (active === 'identities') page = <PeoplePage />
  else if (active === 'sessions') page = <SessionsPage />
  else if (active === 'license') page = <LicensePage />
  else page = <NotBuiltYet name={t(`sso:console.${active}`)} />

  return (
    <>
      <Shell
        sidebar={
          // 分组带小标题：9 项平铺时找一项要从头扫到尾，
          // 分成「看什么 / 配什么 / 管什么」三段之后，
          // 人是先定位到段再找项，扫的东西少一个量级。
          <nav className="flex gap-1 overflow-x-auto md:block md:space-y-4 md:overflow-visible">
            {SECTIONS.map((sec) => (
              <div key={sec.title} className="shrink-0 md:shrink">
                <span className="mb-1 hidden px-2 text-[11px] font-medium text-muted-foreground md:block">
                  {t(`sso:console.sec.${sec.title}`)}
                </span>
                <div className="flex gap-1 md:block md:space-y-0.5">
                  {sec.items.map((key) => {
                    const n = NAV.find((x) => x.key === key)
                    if (!n) return null
                    const Icon = n.icon
                    return (
                      <button
                        key={n.key}
                        type="button"
                        onClick={() => setActive(n.key)}
                        aria-current={active === n.key ? 'page' : undefined}
                        className={[
                          'flex w-full shrink-0 cursor-pointer items-center gap-2 whitespace-nowrap rounded-[var(--radius)] px-2.5 py-1.5 text-left text-[13px] transition-colors duration-150',
                          active === n.key
                            ? 'bg-brand-bg font-medium text-brand'
                            : 'text-muted-foreground hover:bg-secondary hover:text-foreground',
                        ].join(' ')}
                      >
                        <Icon className="size-4 shrink-0" />
                        {t(`sso:console.${n.key}`)}
                      </button>
                    )
                  })}
                </div>
              </div>
            ))}
          </nav>
        }
      >
        {/* 单页崩了不该把整个控制台变成白屏。resetKey 传当前页：
            切到别的页就自动复位，而不是逼人刷新（刷新往往又回到那一页）。 */}
        <ErrorBoundary
          resetKey={active}
          fallback={(err) => (
            <ErrorState
              title={t('sso:console.renderCrash')}
              error={{
                cause: t('sso:console.renderCrashReason'),
                // 原始报错留给排查用。它是英文的技术细节，不进语言包 ——
                // 翻译它反而会让人搜不到对应的代码。
                detail: err.message,
                retryable: true,
              }}
              retryLabel={t('sso:console.renderCrashBack')}
              onRetry={() => setActive('overview')}
            />
          )}
        >
          {page}
        </ErrorBoundary>
      </Shell>
    </>
  )
}

function NotBuiltYet({ name }: { name: string }) {
  const { t } = useTranslation()
  return (
    <div className="rounded-[var(--radius-md)] border border-border bg-card p-10 text-center">
      <p className="text-sm font-semibold">{name}</p>
      {/* 说清是"还没做"而不是"出错了"——两者的下一步完全不同 */}
      <p className="mx-auto mt-2 max-w-[46ch] text-sm text-muted-foreground">
        {t('sso:console.notBuilt')}
      </p>
    </div>
  )
}
