import {
  Outlet,
  createRootRoute,
  createRoute,
  createRouter,
  useNavigate,
  useRouterState,
} from '@tanstack/react-router'
import { tError, useTranslation } from '@ops/i18n'
import { toErrorInfo } from '@ops/api'
import { Banner, ErrorBoundary, ErrorState, NoPermission } from '@ops/ui'
import { type ReactNode, useEffect, useState } from 'react'
import { Preferences } from './components/Preferences.js'
import { ChangePasswordDialog } from './components/ChangePasswordDialog.js'
import { type CurrentUser, UserMenu } from './components/UserMenu.js'
import { AppShell } from './layouts/AppShell.js'
import { NAV } from './layouts/nav.js'
import { getUser, signOut } from './lib/auth.js'
import { can, useRefreshPerms, useSession } from './lib/session.js'
import { AlertsPage } from './routes/alerts/index.js'
import { AuditPage } from './routes/audit/index.js'
import { CertsPage } from './routes/certs/index.js'
import { ObsEndpointsPage } from './routes/obsendpoints/index.js'
import { UsersPage } from './routes/users/index.js'
import { SSOPage } from './routes/sso/index.js'
import { McpPage } from './routes/mcp/index.js'
import { EventCenterPage } from './routes/eventcenter/index.js'
import { CronPage } from './routes/cron/index.js'
import { DnsPage } from './routes/dns/index.js'
import { CdnPage } from './routes/cdn/index.js'
import { CloudIpsPage } from './routes/cloudnet/ips.js'
import { FirewallsPage } from './routes/cloudnet/firewalls.js'
import { CloudIamPage } from './routes/cloudnet/iam.js'
import { UpgradesPage } from './routes/upgrades/index.js'
import { BasicPage } from './routes/basic/index.js'
import { NotifyPage } from './routes/notify/index.js'
import { RegistrarsPage } from './routes/registrars/index.js'
import { CostRatesPage } from './routes/costrates/index.js'
import { HostRecordsPage } from './routes/hostrecords/index.js'
import { CloudAccountsPage } from './routes/cloudaccounts/index.js'
import { RegistryPage } from './routes/registry/index.js'
import { PipelinesPage } from './routes/pipelines/index.js'
import { UsagePage } from './routes/usage/index.js'
import { ObsQueryPage } from './routes/obsquery/index.js'
import { DisruptionPage } from './routes/disruption/index.js'
import { WastePage } from './routes/waste/index.js'
import { CredentialsPage } from './routes/credentials/index.js'
import { EventsPage } from './routes/events/index.js'
import { CostPage } from './routes/cost/index.js'
import { DataSourcesPage } from './routes/datasources/index.js'
import { ExposurePage } from './routes/exposure/index.js'
import { ImpactPage } from './routes/impact/index.js'
import { RelationsPage } from './routes/relations/index.js'
import { TasksPage } from './routes/tasks/index.js'
import { HealthPage } from './routes/health/index.js'
import { OverviewPage } from './routes/overview/index.js'
import { SubnetsPage } from './routes/subnets/index.js'
import { LbsPage } from './routes/lbs/index.js'
import { DomainsPage } from './routes/domains/index.js'
import { ServicesPage } from './routes/services/index.js'
import { PvcsPage } from './routes/pvcs/index.js'
import { ClustersPage } from './routes/clusters/index.js'
import { NamespacesPage } from './routes/namespaces/index.js'
import { NodesPage } from './routes/nodes/index.js'
import { PodsPage } from './routes/pods/index.js'
import { WorkloadsPage } from './routes/workloads/index.js'
import { HostsPage } from './routes/hosts/index.js'
import { licenseBanner } from './routes/license/banner.js'
import { LicensePage } from './routes/license/index.js'
import { useLicense } from './routes/license/queries.js'

/**
 * 路由。
 *
 * 用代码式路由而不是文件式：文件式要装 vite 插件并生成路由树文件，
 * 而我们的菜单本来就有唯一数据源（`layouts/nav.ts`）——
 * 再引入一套"目录结构即路由"的约定，等于有了第二份真相。
 *
 * ⚠️ 筛选条件放在 URL 里是刻意的：
 * 排障时"你看这批机器"要能直接把链接发给同事。存在组件 state 里的话，
 * 分享出去的永远是默认视图，而对方看不出差别 —— 这是最容易被忽略的一种失真。
 */

const rootRoute = createRootRoute({
  component: RootLayout,
})

function RootLayout() {
  const { t } = useTranslation()
  const navigate = useNavigate()
  // activeKey 由 URL 派生，不再是组件 state ——
  // 否则前进/后退时菜单高亮会和页面内容对不上
  const pathname = useRouterState({ select: (s) => s.location.pathname })
  const activeKey = NAV.flatMap((g) => g.items).find((i) => i.path === pathname)?.key ?? ''

  // 展示用的用户信息取本地缓存（刷新时不闪），
  // **权限**则一律用 /api/me 现取的那份 —— 两者的时效要求完全不同
  const cached = getUser()
  const user: CurrentUser = {
    name: cached?.displayName ?? cached?.username ?? '',
    account: cached?.username ?? '',
  }
  const me = useSession()
  const refreshPerms = useRefreshPerms()
  // 授权状态：全站横幅要用。
  // ⚠️ 放在 shell 而不是各页面 —— 过期是**整个系统**的状态，
  // 只在授权页显示的话，客户是在别的页面上撞见写操作失败的
  const [changingPw, setChangingPw] = useState(false)
  const lic = useLicense()
  const banner = licenseBanner(lic.data)

  const crumb = NAV.flatMap((g) => g.items.map((i) => ({ group: g, item: i }))).find(
    (x) => x.item.path === pathname,
  )

  return (
    <AppShell
      activeKey={activeKey}
      onNavigate={(key) => {
        const item = NAV.flatMap((g) => g.items).find((i) => i.key === key)
        if (item && !item.planned) void navigate({ to: item.path })
      }}
      breadcrumb={
        crumb ? (
          <>
            {t(crumb.group.labelKey)} /{' '}
            <b className="font-semibold text-foreground">{t(crumb.item.labelKey)}</b>
          </>
        ) : null
      }
      toolbar={
        <>
          {/*
            🔴 刷新结果必须分三态显示，而且 `stale` 绝不能说成"已刷新"。

            后端拉不到最新权限时（网络抖动 / portal token 过期）**沿用旧快照**
            并置 stale:true —— 沿用是对的（刷新失败不该把人踢出去），
            但说成"已刷新"会让用户认为菜单已是最新的，
            然后把一个仍然错的菜单当成事实。
          */}
          {refreshPerms.isPending ? (
            <span className="text-[11px] text-muted-foreground">{t('common:state.loading')}</span>
          ) : refreshPerms.isError ? (
            <span className="text-[11px] text-danger">{t('common:user.refreshFailed')}</span>
          ) : refreshPerms.data?.stale ? (
            <span className="max-w-[260px] text-[11px] text-warning">
              {t('common:user.refreshStale')}
            </span>
          ) : refreshPerms.isSuccess ? (
            <span className="text-[11px] text-success">{t('common:user.refreshed')}</span>
          ) : null}
          <Preferences />
          <div className="mx-1 h-4 w-px bg-border" />
          <UserMenu
            user={user}
            onSignOut={() => void signOut()}
            // 只有 portal / SSO 账号有可刷的权限：本地账号的角色就在 CMDB 自己库里，
            // 改完立刻生效，没有"等会话过期"这个问题
            onRefreshPerms={
              me.data?.authSource === 'portal' ? () => refreshPerms.mutate() : undefined
            }
            // 只有本地账号能在这儿改密码：portal / SSO 账号的密码不在 CMDB 这边
            onChangePassword={
              me.data?.authSource === 'local' ? () => setChangingPw(true) : undefined
            }
          />
        </>
      }
      session={me.data}
      permsPending={me.isPending}
      banner={
        banner ? (
          <Banner
            tone={banner.tone}
            action={
              // 横幅必须给出路。只说"已过期"而不告诉人去哪看，
              // 他只能反复读这一行
              <button
                type="button"
                onClick={() => void navigate({ to: '/admin/license' })}
                className="shrink-0 cursor-pointer text-xs underline underline-offset-2 opacity-80 hover:opacity-100"
              >
                {t('common:license.viewLicense')}
              </button>
            }
          >
            {t(banner.key, banner.params)}
          </Banner>
        ) : null
      }
    >
      <PermissionGate
        pending={me.isPending}
        error={me.isError ? me.error : null}
        onRetry={() => void me.refetch()}
        allowed={can(me.data, crumb?.item.perm ?? '')}
        code={crumb?.item.perm ?? ''}
      >
        {/*
          渲染期异常兜底。放在 Outlet 外面而不是最外层，是为了**保住菜单和顶栏**：
          一个页面炸了，用户还能点去别的地方，而不是对着一片白屏只能刷新。

          ⚠️ 这一层现在只兜**路由之外**的渲染异常。页面组件自己抛的异常
          交给路由的 defaultErrorComponent（见 createRouter）——
          理由见下面那段。
        */}
        <ErrorBoundary
          key={pathname}
          resetKey={pathname}
          fallback={(err, reset) => (
            <ErrorState
              title={t('common:crash.title')}
              error={{
                cause: t('common:crash.cause'),
                // 原始异常给运维贴工单用，不翻译
                detail: err.message,
                retryable: true,
              }}
              retryLabel={t('common:crash.retry')}
              onRetry={reset}
            />
          )}
        >
          <Outlet />
        </ErrorBoundary>
      </PermissionGate>
      {changingPw ? <ChangePasswordDialog onClose={() => setChangingPw(false)} /> : null}
    </AppShell>
  )
}

/**
 * 页面级权限闸。
 *
 * 存在的理由是**深链**：菜单过滤只挡住了点击这条路径，
 * 而分享出来的链接、收藏夹、浏览器补全都能直接落到页面上。
 *
 * ⚠️ 权限还没到手时渲染 `null` 而不是内容：
 * 先渲染再撤下会让无权限的数据闪一下 —— 那一帧是真的把数据给出去了，
 * 截图、录屏都能留下来。
 *
 * ⚠️ 这里挡住的只是界面。真正的拦截在后端（未映射的路由 fail-closed），
 * 前端这层被绕过也拿不到数据。
 */
function PermissionGate({
  pending,
  error,
  onRetry,
  allowed,
  code,
  children,
}: {
  pending: boolean
  /** 权限**没取到**（接口挂了/网络断），与"取到了但没权限"是两回事 */
  error: unknown
  onRetry: () => void
  allowed: boolean
  code: string
  children: ReactNode
}) {
  const { t } = useTranslation()
  if (pending) return null

  // ⚠️ 取权限失败**绝不能**渲染成「无权限」。
  // 两者界面上一模一样，但用户的下一步完全相反：一个该找管理员要权限
  // （而管理员会发现他明明有），一个该看服务端还活着没有。
  // 这是全站「失败态不能退化成正常态」在权限上的那一份。
  if (error !== null) {
    const n = toErrorInfo(error)
    return (
      <ErrorState
        title={t('common:perm.loadFailed')}
        error={{ cause: tError(t, n.messageKey, n.params), detail: n.detail, retryable: n.retryable, kind: n.kind === 'license' ? 'license' : undefined, feature: n.params?.feature as string | undefined }}
        retryLabel={t('common:action.retry')}
        onRetry={onRetry}
      />
    )
  }

  // 没有对应菜单项的路径（notFound 等）不做权限判断，交给下游渲染
  if (!code || allowed) return <>{children}</>
  return (
    <NoPermission
      title={t('common:perm.title')}
      reason={t('common:perm.reason')}
      code={code}
      copyLabel={t('common:perm.copy')}
      copiedLabel={t('common:perm.copied')}
    />
  )
}

/** 主机列表。筛选条件全部走 URL，刷新与分享都不丢。 */
const hostsRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/resources/hosts',
  validateSearch: (raw: Record<string, unknown>) => {
    const status = String(raw.status ?? 'all')
    return {
      // 校验而不是直接透传：URL 是用户可以随手改的，
      // 塞一个非法状态进来会让筛选静默失效（后端忽略未知值），
      // 而界面上那个下拉却显示着它 —— 看起来筛了，其实没筛。
      status: (['all', 'running', 'stopped', 'destroyed'].includes(status)
        ? status
        : 'all') as 'all' | 'running' | 'stopped' | 'destroyed',
      project: String(raw.project ?? 'all'),
      // 云厂商。后端 facets 一直算着这一维，此前前端没接（OPSCMDB-028 GAP-9）
      provider: String(raw.provider ?? 'all'),
      q: String(raw.q ?? ''),
      page: Math.max(1, Number(raw.page) || 1),
      // 打开的详情抽屉。放进 URL 是为了「看这台机器」也能直接发链接 ——
      // 只存组件 state 的话，分享出去的永远是列表首页。
      //
      // ⚠️ 用 number 而不是 string：路由默认按 JSON 序列化 search，
      // 字符串会被加上引号变成 detail=%2291%22 —— 手工拼链接时极易出错，
      // 而数字是干净的 detail=91。0 表示没打开。
      detail: Number(raw.detail) || 0,
    }
  },
  component: HostsPage,
})


/**
 * 根路径重定向。
 *
 * ⚠️ 这里刻意用原生跳转，不用 `redirect()` 也不用 `useNavigate()`。
 *
 * 两者的类型参数都需要完整的 router 类型，而形成了一条闭环：
 *   router → routeTree → indexRoute → 本组件 → useNavigate → Register → router
 * TS 推断不出来，整棵树的路径类型会退化成 `never`，表现是所有 navigate 都编译不过。
 *
 * 而这个场景本来就适合原生跳转：它是应用入口的一次性重定向，
 * 不需要保留任何客户端状态。
 *
 * `replace` 是必须的：不换掉历史记录的话，用户按后退会回到 `/`，
 * 又被弹回来，形成一个退不出去的循环。
 */
function RedirectToDefault() {
  useEffect(() => {
    // 🔴 落到「全局态势」，不是主机列表。
    // 首屏要回答的是"系统现在有没有问题"——态势页有需要关注的事、家底、数据新鲜度。
    // 落在主机列表上，用户第一眼看到的是一张平铺的资产表，看不出任何异常。
    window.location.replace('/overview')
  }, [])
  return null
}

const indexRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/',
  component: RedirectToDefault,
})

/**
 * 未实现的页面。
 *
 * 有路由但明说"还没做"，比 404 或空白页强：
 * 用户点进来知道是路线图上的，不会怀疑自己权限不对或系统坏了。
 */
function PlannedPage() {
  const { t } = useTranslation()
  const pathname = useRouterState({ select: (s) => s.location.pathname })
  const item = NAV.flatMap((g) => g.items).find((i) => i.path === pathname)

  // 🔴 「路线图上还没做」和「这个地址不存在」必须分开。
  //
  // 这个组件同时被 planned 菜单项和 notFound 使用，于是**任意打错的 URL**
  // 都会得到「这个功能在路线图上，还没有开始做」——一个明确但完全错误的结论。
  // 打错地址、或从旧链接跳进来的人，会以为某个功能规划中，而实际只是地址错了。
  //
  // 判据：能在菜单里找到这个路径 → 是规划中的功能；找不到 → 就是没这个页面。
  if (!item) {
    return (
      <div className="flex flex-col items-center justify-center gap-2 px-6 py-20 text-center">
        <p className="text-sm font-semibold text-foreground">{t('notFound.title')}</p>
        <p className="max-w-[42ch] text-xs text-muted-foreground">
          {t('notFound.hint', { path: pathname })}
        </p>
        <a
          href="/overview"
          className="mt-1 text-xs text-brand-text underline-offset-2 hover:underline"
        >
          {t('notFound.backHome')}
        </a>
      </div>
    )
  }
  return (
    <div className="flex flex-col items-center justify-center gap-2 px-6 py-20 text-center">
      <p className="text-sm font-semibold text-foreground">{t(item.labelKey)}</p>
      <p className="max-w-[42ch] text-xs text-muted-foreground">{t('planned.hint')}</p>
    </div>
  )
}

/**
 * 路由树必须是**静态**的。
 *
 * 一开始我用 `NAV.filter(planned).map(createRoute)` 给未实现的菜单批量生成占位路由，
 * 结果整棵树的路径类型退化成 `never` —— TanStack Router 的类型安全依赖静态结构，
 * 动态生成等于把它的核心能力关掉了，而表现是 `navigate()` 全都编译不过。
 *
 * 而且那些路由本来就不需要：planned 菜单项在 AppShell 里是 disabled 的，点不动；
 * 直接在地址栏输路径会走 notFound，而 notFound 渲染的正是同一个 PlannedPage。
 * 少一批路由，行为完全一样。
 */
/** 集群列表。筛选走 URL，同主机页。 */
const clustersRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/k8s/clusters',
  validateSearch: (raw: Record<string, unknown>) => ({
    // 页码与每页条数进 URL：分享出去的链接要能落到同一页，
    // 而且刷新不会跳回第一页。size 夹在 20–200，防手改成 100000 把后端拖垮
    page: Math.max(1, Number(raw.page) || 1),
    size: Math.min(200, Math.max(20, Number(raw.size) || 50)),
    // 环境是自由值（PROD/UAT/TEST/DEV 之外客户还可能自定义），
    // 所以这里只做类型收敛不做枚举校验；后端查不到就是空结果，
    // 而空结果的文案会说明是筛选太窄
    env: String(raw.env ?? 'all'),
    q: String(raw.q ?? ''),
  }),
  component: ClustersPage,
})

/** 节点列表。主机页的对偶，两页互相链接。 */
const nodesRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/k8s/nodes',
  validateSearch: (raw: Record<string, unknown>) => {
    const st = String(raw.status ?? 'all')
    return {
    // 页码与每页条数进 URL：分享出去的链接要能落到同一页，
    // 而且刷新不会跳回第一页。size 夹在 20–200，防手改成 100000 把后端拖垮
    page: Math.max(1, Number(raw.page) || 1),
    size: Math.min(200, Math.max(20, Number(raw.size) || 50)),
      cluster: String(raw.cluster ?? 'all'),
      // 节点池名字是集群给的，枚举不出来，只能透传。
      // 传了不存在的池子时后端会返回 0 条 —— 这跟 status 不同：
      // status 是固定枚举，非法值必须夹回 all，否则筛选静默失效
      pool: String(raw.pool ?? 'all'),
      // 校验而不是透传：塞个非法值进来会让筛选静默失效，
      // 而下拉里还显示着它 —— 看起来筛了，其实没筛
      // 🔴 'pressure'：磁盘/内存/PID 压力。节点仍是 Ready 但快撑不住了 ——
      //	这一档不放进枚举的话，triage 给出的 /k8s/nodes?status=pressure
      //	会被夹回 all，人点过去看到的是全部节点（OPSCMDB-042）
      status: (['all', 'ready', 'notready', 'stale', 'pressure'].includes(st) ? st : 'all') as
        | 'all'
        | 'ready'
        | 'notready'
        | 'stale'
        | 'pressure',
      q: String(raw.q ?? ''),
    }
  },
  component: NodesPage,
})

/** Pod 列表。后端在 SQL 里分页（十万级，不能全取到内存）。 */
const podsRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/k8s/pods',
  validateSearch: (raw: Record<string, unknown>) => {
    const h = String(raw.health ?? 'all')
    return {
      cluster: String(raw.cluster ?? 'all'),
      namespace: String(raw.namespace ?? 'all'),
      health: (['all', 'bad', 'restarted', 'ok'].includes(h) ? h : 'all') as
        | 'all'
        | 'bad'
        | 'restarted'
        | 'ok',
      q: String(raw.q ?? ''),
      // 🔴 工作负载 / 节点下钻。后端 pod-list 一直支持这两个过滤，前端没接。
      //
      //	此前只能靠关键词搜工作负载名，那是**碰巧**能命中
      //	（Pod 名 = 工作负载名 + hash）；Job / CronJob 的 Pod 名与工作负载名
      //	对不上时就搜不到，而"搜不到"会被读成"这个工作负载没有 Pod"。
      //	进 URL 是为了「这个工作负载的 Pod」能直接发链接（OPSCMDB-028 GAP-11）。
      workload: String(raw.workload ?? ''),
      node: String(raw.node ?? ''),
      page: Math.max(1, Number(raw.page) || 1),
    }
  },
  component: PodsPage,
})

/** 工作负载列表。健康度四档，`scaled_zero` 是正常状态不是故障。 */
const workloadsRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/k8s/workloads',
  validateSearch: (raw: Record<string, unknown>) => {
    const h = String(raw.health ?? 'all')
    return {
      cluster: String(raw.cluster ?? 'all'),
      namespace: String(raw.namespace ?? 'all'),
      health: (['all', 'down', 'degraded', 'scaled_zero', 'ok'].includes(h) ? h : 'all') as
        | 'all'
        | 'down'
        | 'degraded'
        | 'scaled_zero'
        | 'ok',
      q: String(raw.q ?? ''),
      page: Math.max(1, Number(raw.page) || 1),
    }
  },
  component: WorkloadsPage,
})

/** 命名空间列表。默认把卡在 Terminating 的顶到最前。 */
const namespacesRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/k8s/namespaces',
  validateSearch: (raw: Record<string, unknown>) => ({
    // 页码与每页条数进 URL：分享出去的链接要能落到同一页，
    // 而且刷新不会跳回第一页。size 夹在 20–200，防手改成 100000 把后端拖垮
    page: Math.max(1, Number(raw.page) || 1),
    size: Math.min(200, Math.max(20, Number(raw.size) || 50)),
    cluster: String(raw.cluster ?? 'all'),
    // phase 是 k8s 的原值（Active/Terminating），不做枚举收敛：
    // 新版本加了别的相位时，写死枚举会把它静默变成 all
    phase: String(raw.phase ?? 'all'),
    q: String(raw.q ?? ''),
  }),
  component: NamespacesPage,
})

/** 证书列表。默认按严重度排：已过期 → 续期失败 → 读不出到期日 → 快到期。 */
const certsRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/resources/certs',
  validateSearch: (raw: Record<string, unknown>) => {
    const h = String(raw.health ?? 'all')
    return {
    // 页码与每页条数进 URL：分享出去的链接要能落到同一页，
    // 而且刷新不会跳回第一页。size 夹在 20–200，防手改成 100000 把后端拖垮
    page: Math.max(1, Number(raw.page) || 1),
    size: Math.min(200, Math.max(20, Number(raw.size) || 50)),
      health: (['all', 'expired', 'failing', 'unknown', 'soon', 'ok'].includes(h) ? h : 'all') as
        | 'all'
        | 'expired'
        | 'failing'
        | 'unknown'
        | 'soon'
        | 'ok',
      q: String(raw.q ?? ''),
    }
  },
  component: CertsPage,
})

/** 子网列表。云上已删除的保留并标注（别的资源可能还引用着那个网段）。 */
const subnetsRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/resources/subnets',
  validateSearch: (raw: Record<string, unknown>) => ({
    // 页码与每页条数进 URL：分享出去的链接要能落到同一页，
    // 而且刷新不会跳回第一页。size 夹在 20–200，防手改成 100000 把后端拖垮
    page: Math.max(1, Number(raw.page) || 1),
    size: Math.min(200, Math.max(20, Number(raw.size) || 50)),
    region: String(raw.region ?? 'all'),
    project: String(raw.project ?? 'all'),
    q: String(raw.q ?? ''),
  }),
  component: SubnetsPage,
})

/** 负载均衡列表。后端数三态：没采过 / 确认 0 个 / 正常。 */
const lbsRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/resources/lbs',
  validateSearch: (raw: Record<string, unknown>) => ({
    // 页码与每页条数进 URL：分享出去的链接要能落到同一页，
    // 而且刷新不会跳回第一页。size 夹在 20–200，防手改成 100000 把后端拖垮
    page: Math.max(1, Number(raw.page) || 1),
    size: Math.min(200, Math.max(20, Number(raw.size) || 50)),
    scheme: String(raw.scheme ?? 'all'),
    // ⚠️ 枚举要和 lbHealth 的返回值一致。
    //
    //	原来只有 5 个值，而「后端为 0」实际有五种不同原因：
    //	  viaTarget 由 target instance/proxy 承载（FortiGate 转发规则、Gateway API）
    //	  k8s       后端是 K8s Pod/NEG，实例组里看不到
    //	  lost      采集时追溯到了、读出来没有 → 数据丢了
    //	把它们全归进 empty，界面就会把 37 条正常 LB 报成"确认无后端"（P0-8）。
    health: (
      ['all', 'stale', 'unknown', 'viaTarget', 'k8s', 'lost', 'empty', 'ok'].includes(
        String(raw.health ?? 'all'),
      )
        ? String(raw.health ?? 'all')
        : 'all'
    ) as 'all' | 'stale' | 'unknown' | 'viaTarget' | 'k8s' | 'lost' | 'empty' | 'ok',
    q: String(raw.q ?? ''),
  }),
  component: LbsPage,
})

/** 域名列表。注册到期 / 解析 / 证书三个维度分开成列。 */
const domainsRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/resources/domains',
  validateSearch: (raw: Record<string, unknown>) => ({
    // 视图：台账 / 访问质量对账（OPSCMDB-035）。
    // ⚠️ 默认是台账 —— 对账是排查用的，日常打开这一页是来看域名清单的
    view: String(raw.view) === 'quality' ? 'quality' : 'ledger',
    // 页码与每页条数进 URL：分享出去的链接要能落到同一页，
    // 而且刷新不会跳回第一页。size 夹在 20–200，防手改成 100000 把后端拖垮
    page: Math.max(1, Number(raw.page) || 1),
    size: Math.min(200, Math.max(20, Number(raw.size) || 50)),
    // 白名单校验而不是透传：塞个非法值进来会让筛选静默失效，
    // 而下拉里还显示着它 —— 看起来筛了，其实没筛
    health: (['all', 'expired', 'unresolved', 'unknown', 'soon', 'cert_soon', 'ignored', 'ok'].includes(
      String(raw.health ?? 'all'),
    )
      ? String(raw.health ?? 'all')
      : 'all') as 'all' | 'expired' | 'unresolved' | 'unknown' | 'soon' | 'cert_soon' | 'ignored' | 'ok',
    q: String(raw.q ?? ''),
  }),
  component: DomainsPage,
})

/** 服务与入口。Service 为行，指向它的 Ingress 主机名并入同一行。 */
const servicesRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/k8s/services',
  validateSearch: (raw: Record<string, unknown>) => ({
    // 页码与每页条数进 URL：分享出去的链接要能落到同一页，
    // 而且刷新不会跳回第一页。size 夹在 20–200，防手改成 100000 把后端拖垮
    page: Math.max(1, Number(raw.page) || 1),
    size: Math.min(200, Math.max(20, Number(raw.size) || 50)),
    cluster: String(raw.cluster ?? 'all'),
    namespace: String(raw.namespace ?? 'all'),
    exposure: (['all', 'exposed', 'pending', 'internal'].includes(String(raw.exposure ?? 'all'))
      ? String(raw.exposure ?? 'all')
      : 'all') as 'all' | 'exposed' | 'pending' | 'internal',
    q: String(raw.q ?? ''),
  }),
  component: ServicesPage,
})

/** 存储卷列表。Lost / Pending 排最前。 */
const pvcsRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/k8s/pvcs',
  validateSearch: (raw: Record<string, unknown>) => ({
    // 页码与每页条数进 URL：分享出去的链接要能落到同一页，
    // 而且刷新不会跳回第一页。size 夹在 20–200，防手改成 100000 把后端拖垮
    page: Math.max(1, Number(raw.page) || 1),
    size: Math.min(200, Math.max(20, Number(raw.size) || 50)),
    cluster: String(raw.cluster ?? 'all'),
    health: (['all', 'lost', 'pending', 'orphan', 'ok'].includes(String(raw.health ?? 'all'))
      ? String(raw.health ?? 'all')
      : 'all') as 'all' | 'lost' | 'pending' | 'orphan' | 'ok',
    q: String(raw.q ?? ''),
  }),
  component: PvcsPage,
})

/** 全局态势。只汇总各列表页已有的判据，不新造判据。 */
const overviewRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/overview',
  component: OverviewPage,
})

/** 集群体检。任何一项查不成就整体不出结论 —— 见 queries.ts 的注释。 */
const healthRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/overview/clusters',
  validateSearch: (raw: Record<string, unknown>) => ({
    // 0 = 没指定，页面取第一个集群
    cluster: Number(raw.cluster) || 0,
  }),
  component: HealthPage,
})

/** 巡检：定时任务与最近一次执行结果。含"从没跑过"「早该跑却没跑」两种静默失败。 */
const tasksRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/runtime/inspection',
  validateSearch: (raw: Record<string, unknown>) => ({
    // 页码与每页条数进 URL：分享出去的链接要能落到同一页，
    // 而且刷新不会跳回第一页。size 夹在 20–200，防手改成 100000 把后端拖垮
    page: Math.max(1, Number(raw.page) || 1),
    size: Math.min(200, Math.max(20, Number(raw.size) || 50)),
    state: String(raw.state ?? 'all'),
    q: String(raw.q ?? ''),
  }),
  component: TasksPage,
})

/** 成本总览。全是**估算**，不是账单。 */
const costRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/cost',
  component: CostPage,
})

/** 暴露面。判据与各资源页一致；防护状态判不了就说判不了。 */
const exposureRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/security/exposure',
  validateSearch: (raw: Record<string, unknown>) => ({
    // 页码与每页条数进 URL：分享出去的链接要能落到同一页，
    // 而且刷新不会跳回第一页。size 夹在 20–200，防手改成 100000 把后端拖垮
    page: Math.max(1, Number(raw.page) || 1),
    size: Math.min(200, Math.max(20, Number(raw.size) || 50)),
    kind: String(raw.kind ?? 'all'),
    q: String(raw.q ?? ''),
  }),
  component: ExposurePage,
})

/** 资源图谱：已记录的关系。没有关系记录 ≠ 资源孤立。 */
const relationsRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/topology',
  validateSearch: (raw: Record<string, unknown>) => ({
    // 页码与每页条数进 URL：分享出去的链接要能落到同一页，
    // 而且刷新不会跳回第一页。size 夹在 20–200，防手改成 100000 把后端拖垮
    page: Math.max(1, Number(raw.page) || 1),
    size: Math.min(200, Math.max(20, Number(raw.size) || 50)),
    rel_type: String(raw.rel_type ?? 'all'),
    q: String(raw.q ?? ''),
    // 图 / 列表两个视图。⚠️ 默认给图 ——「资源图谱」这个菜单名承诺的就是图，
    // 而它一直点进去是张表（OPSCMDB-023 第一档：老版 K8sTopology.vue 的核心能力）
    view: raw.view === 'list' ? 'list' : 'graph',
  }),
  component: RelationsPage,
})

/** 变更影响面。沿已记录的关系往外 3 层。 */
const impactRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/topology/impact',
  validateSearch: (raw: Record<string, unknown>) => ({ ci: Number(raw.ci) || 0 }),
  component: ImpactPage,
})

/** 数据源接入状况。只显示"配没配"，**不返回任何凭据内容**。 */
const dataSourcesRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/admin/datasources',
  validateSearch: (raw: Record<string, unknown>) => ({
    // 页码与每页条数进 URL：分享出去的链接要能落到同一页，
    // 而且刷新不会跳回第一页。size 夹在 20–200，防手改成 100000 把后端拖垮
    page: Math.max(1, Number(raw.page) || 1),
    size: Math.min(200, Math.max(20, Number(raw.size) || 50)),
    kind: String(raw.kind ?? 'all'),
    q: String(raw.q ?? ''),
  }),
  component: DataSourcesPage,
})

/** 事件（仅 Warning）。etcd 只留 1 小时，这里是落库后的可回看副本。 */
const eventsRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/k8s/events',
  validateSearch: (raw: Record<string, unknown>) => ({
    // 页码与每页条数进 URL：分享出去的链接要能落到同一页，
    // 而且刷新不会跳回第一页。size 夹在 20–200，防手改成 100000 把后端拖垮
    page: Math.max(1, Number(raw.page) || 1),
    size: Math.min(200, Math.max(20, Number(raw.size) || 50)),
    cluster: String(raw.cluster ?? 'all'),
    namespace: String(raw.namespace ?? 'all'),
    reason: String(raw.reason ?? 'all'),
    q: String(raw.q ?? ''),
  }),
  component: EventsPage,
})

/** 告警。读已接入的告警系统；没接时空态要说清是"没接"而不是"没告警"。 */
const alertsRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/runtime/alerts',
  validateSearch: (raw: Record<string, unknown>) => ({
    q: String(raw.q ?? ''),
    // 级别进 URL：「只看 critical」是排障时最常用的一步，要能分享和刷新保持。
    // ⚠️ 只收后端认得的三个值，别的一律当"不限" ——
    // 传个 severity=S1 过去会被下推成夜莺不认识的参数，结果静默返回全部
    severity: ['critical', 'warning', 'info'].includes(String(raw.severity))
      ? String(raw.severity)
      : '',
    // 当前 / 历史。⚠️ 后端只认这两个值（handlers/alerts.go 的 state），
    // 别的一律当 current —— 传个后端不认的值会静默走 current 分支，
    // 于是「历史」标签亮着、内容却是当前告警
    state: String(raw.state) === 'history' ? 'history' : 'current',
    // 历史视图的时间窗（小时）。后端范围 1~720，越界会被兜回 24
    hours: [6, 24, 168, 720].includes(Number(raw.hours)) ? Number(raw.hours) : 24,
    // 🔴 env 决定用**哪一个夜莺接入点**。不传 = 只看"不限环境"的那条，
    //	其余环境的告警看不见也不报错 —— 看着像"这个环境很安静"。
    //	可选值由后端 `envs` 给（只列真配了 n9e 的环境），前端不硬编码。
    env: String(raw.env ?? ''),
  }),
  component: AlertsPage,
})

/** 凭据盘点。看不到任何凭据内容，只回答"存在哪、缺不缺、失效没有"。 */
const credentialsRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/security/credentials',
  component: CredentialsPage,
})

/** 镜像仓库。实时问 Harbor API，不落库（仓库清单没有历史价值）。 */
const registryRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/resources/registry',
  validateSearch: (raw: Record<string, unknown>) => ({
    registry: Number(raw.registry) || 0,
    project: String(raw.project ?? ''),
  }),
  component: RegistryPage,
})

/** 闲置与浪费。需要 Prometheus 实测用量，没接时空态说"未接入"而不是"没有浪费"。 */
const wasteRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/cost/waste',
  validateSearch: (raw: Record<string, unknown>) => ({
    cluster: Number(raw.cluster) || 0,
    q: String(raw.q ?? ''),
    page: Math.max(1, Number(raw.page) || 1),
    size: Math.min(200, Math.max(20, Number(raw.size) || 50)),
    // 排序进 URL：「按内存浪费排」和「按 CPU 浪费排」得出的优化顺序不同，
    // 讨论时要能把同一个视图发给对方
    sort: ['cpu_waste', 'mem_waste', 'cpu_pct', 'mem_pct'].includes(String(raw.sort))
      ? String(raw.sort)
      : 'cpu_waste',
  }),
  component: WastePage,
})

/** 云账号接入。GCP 的 SA key 配在**项目**上，不是账号上（权限就是按 project 给的）。 */
/** 资源使用率：实时查 Prometheus 的 CPU/内存曲线，覆盖 K8s 与传统主机（OPSCMDB-021）。 */
const usageRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/runtime/usage',
  component: UsagePage,
})

const disruptionRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/k8s/disruption',
  component: DisruptionPage,
})

const obsQueryRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/runtime/obs-query',
  component: ObsQueryPage,
})

const cloudAccountsRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/admin/cloud-accounts',
  component: CloudAccountsPage,
})

/** 用户与角色。"能不能改这一条"由后端算好（最后一个管理员不能降权等）。 */
const usersRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/admin/users',
  component: UsersPage,
})

/** 审计日志。只记写操作，时间用绝对时间。 */
const auditRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/admin/audit',
  validateSearch: (raw: Record<string, unknown>) => ({
    q: String(raw.q ?? ''),
    // 分页进 URL：审计是**事后追溯**用的，「第 7 页那一条」要能直接发给同事
    page: Math.max(1, Number(raw.page) || 1),
    size: Math.min(200, Math.max(20, Number(raw.size) || 50)),
    // 筛选维度同样进 URL，理由同上：「AI 昨天改了什么」这个查询要能直接发出去。
    // ⚠️ 枚举值必须与后端**实际产出**的一致，不能照旧版抄：
    //	actor_source = portal | local | mcp | system（handlers/common.go actorSourceOf）
    //	status       = success | fail | accepted（handlers/audit_routes.go auditStatusOf）
    //	旧版有「被拒绝」这一档，而新后端把 403 归进 fail —— 照抄会做出一个
    //	永远返回空的筛选项，那比没有这个筛选更坏。
    actor_source: String(raw.actor_source ?? ''),
    status: String(raw.status ?? ''),
    username: String(raw.username ?? ''),
    target_type: String(raw.target_type ?? ''),
    // 时间窗用小时数，0 = 不限。审计查的是"那天几点"，所以同时把 since 落进 URL
    hours: Math.max(0, Number(raw.hours) || 0),
    // 只看有字段变更的：动作型操作（同步/续费/测连通）没有 diff，
    // 排查"谁改了配置"时它们是噪音
    changed_only: raw.changed_only === '1' || raw.changed_only === true ? '1' : '',
  }),
  component: AuditPage,
})

/** 观测端点接入（Prometheus / Loki / KubeSphere / 夜莺）。类型必须从白名单选。 */
const obsRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/admin/obs-endpoints',
  component: ObsEndpointsPage,
})

/**
 * 单点登录接入。
 *
 * ⚠️ 归在「系统管理」而不是和用户管理合并成一页：
 * 配 IdP 是**一次性**的运维动作，管账号是日常动作。
 * 放一起的话，每天要用的那半页永远得先滚过一段没人再看的表单。
 */
const ssoRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/admin/sso',
  component: SSOPage,
})

/** IP 台账。闲置的静态 IP 在持续计费，这一页主要就是把它们捞出来。 */
const ipsRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/resources/ips',
  validateSearch: (raw: Record<string, unknown>) => ({
    q: String(raw.q ?? ''),
    usage: String(raw.usage ?? 'all'),
    page: Math.max(1, Number(raw.page) || 1),
    size: Math.min(200, Math.max(20, Number(raw.size) || 50)),
  }),
  component: CloudIpsPage,
})

/** 防火墙规则。高危判定用后端的 high_risk，前端不另判一次。 */
const firewallsRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/resources/firewalls',
  validateSearch: (raw: Record<string, unknown>) => ({
    q: String(raw.q ?? ''),
    page: Math.max(1, Number(raw.page) || 1),
    size: Math.min(200, Math.max(20, Number(raw.size) || 50)),
    // 只看高危进 URL：「先看高危」是打开这一页的第一动作，要能分享和刷新保持
    risky: raw.risky === true || raw.risky === 'true',
  }),
  component: FirewallsPage,
})

/** 云权限审计（GCP IAM）。⚠️ 空 = 没采到，不是"没有风险"，见页面注释。 */
const cloudIamRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/security/cloud-iam',
  validateSearch: (raw: Record<string, unknown>) => ({
    only: String(raw.only ?? 'all'),
    page: Math.max(1, Number(raw.page) || 1),
    size: Math.min(200, Math.max(20, Number(raw.size) || 50)),
  }),
  component: CloudIamPage,
})

/** GKE 版本与升级。只读，执行在云控制台。 */
const upgradesRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/k8s/upgrades',
  component: UpgradesPage,
})

/** 基础配置：环境 / 项目 / CI 类型字典。全站下拉的取值来源。 */
const basicRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/admin/basic',
  component: BasicPage,
})

/**
 * 主机头台账。
 *
 * ⚠️ 与 /resources/dns（DNS 解析）是**两套数据**：那边是 CF/GCP 上此刻的
 * 实时解析，这边是我们自己的台账（谁负责、属于哪个项目、哪些不用管）。
 */
const hostRecordsRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/resources/host-records',
  validateSearch: (raw: Record<string, unknown>) => ({
    q: String(raw.q ?? ''),
    status: String(raw.status ?? ''),
    page: Math.max(1, Number(raw.page) || 1),
    size: Math.min(200, Math.max(20, Number(raw.size) || 50)),
  }),
  component: HostRecordsPage,
})

/** 成本单价。⚠️ 单价错了不会报错，只会让整张成本报表安静地偏掉。 */
const costRatesRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/cost/rates',
  component: CostRatesPage,
})

/** 注册商接入。没有它，域名到期日永远不更新，而域名列表看着正常。 */
const registrarsRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/admin/registrars',
  component: RegistrarsPage,
})

/** 通知人。⚠️ 空列表是危险状态：任务照常"成功"，只是没人收到。 */
const notifyRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/admin/notify',
  component: NotifyPage,
})

/**
 * DNS 解析。**合并 Cloudflare 与 GCP Cloud DNS 两个来源**——
 * 只有 NS 指向的那一边生效，分成两页会让人在错的那边改半天。
 */
const dnsRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/resources/dns',
  validateSearch: (raw: Record<string, unknown>) => ({
    q: String(raw.q ?? ''),
    source: String(raw.source ?? 'all'),
    type: String(raw.type ?? 'all'),
    // 🔴 视图。**默认「按域名」** —— 人点「DNS 解析」时想问的是
    //	「这个域名下有哪些解析」，而不是三方对账（OPSCMDB-036）。
    //	平铺视图保留：它有旧版没有的三方并列 + 「配了但不生效」判定，
    //	生产上刚靠它查出 17 条真问题。两者回答不同的问题，不能合并。
    view: String(raw.view) === 'all' ? 'all' : 'byDomain',
    page: Math.max(1, Number(raw.page) || 1),
    size: Math.min(200, Math.max(20, Number(raw.size) || 50)),
  }),
  component: DnsPage,
})

/** 构建流水线（KubeSphere DevOps）。项目 → 运行记录 → 日志 三级下钻。 */
const pipelinesRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/platform/pipelines',
  component: PipelinesPage,
})

/** CDN 站点（Cloudflare）。归在「域名与入口」，理由见页面注释。 */
const cdnRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/resources/cdn',
  component: CdnPage,
})

/**
 * 事件中心。平台层的统一时间线。
 *
 * ⚠️ 与 /k8s/events（集群事件）是两个东西：那个是 apiserver 的 Event 对象，
 * 这个是 CMDB 自己产生的（到期/变更/采集失败）+ 汇进来的 K8s Warning。
 */
const eventCenterRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/runtime/events',
  validateSearch: (raw: Record<string, unknown>) => ({
    days: Math.min(90, Math.max(1, Number(raw.days) || 30)),
    source: String(raw.source ?? 'all'),
    level: String(raw.level ?? 'all'),
    page: Math.max(1, Number(raw.page) || 1),
    size: Math.min(200, Math.max(20, Number(raw.size) || 50)),
    q: String(raw.q ?? ''),
    // 「只看还来得及处理的」进 URL：这是打开这一页最有价值的一个视图，
    // 要能直接把链接发给同事（「你看这几个域名快到期了」）
    upcoming: raw.upcoming === '1' || raw.upcoming === true ? '1' : '',
  }),
  component: EventCenterPage,
})

/** 定时任务（含执行记录）。旧版拆成两个菜单，合并见页面注释。 */
const cronRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/runtime/cron',
  // task 进 URL：巡检页看到一条失败，点「看执行记录」要能直接落到那条任务上
  // 并且展开它。没有这个参数的话，跳过来还要在 15 条里自己找一遍
  validateSearch: (raw: Record<string, unknown>) => ({ task: String(raw.task ?? '') }),
  component: CronPage,
})

/**
 * AI 接入（MCP）。
 *
 * ⚠️ 归在「系统管理」：它发的是**凭据**，和用户管理、SSO 是同一类动作。
 * 放到「集成」或「工具」下会让人以为它只是个开关。
 */
const mcpRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/admin/mcp',
  component: McpPage,
})

/** 授权页。任何登录用户都能看状态；激活框按权限显隐（见页面内部）。 */
const licenseRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/admin/license',
  component: LicensePage,
})

const routeTree = rootRoute.addChildren([indexRoute, hostsRoute, clustersRoute, nodesRoute, podsRoute, workloadsRoute, namespacesRoute, certsRoute,
  subnetsRoute, lbsRoute, domainsRoute, servicesRoute, pvcsRoute,
  overviewRoute, healthRoute,
  tasksRoute, costRoute, exposureRoute, relationsRoute, impactRoute, dataSourcesRoute,
  eventsRoute, alertsRoute, credentialsRoute, registryRoute, wasteRoute,
  usageRoute, obsQueryRoute, disruptionRoute, cloudAccountsRoute,
  usersRoute, ssoRoute, mcpRoute, auditRoute, obsRoute, eventCenterRoute, cronRoute, dnsRoute, cdnRoute,
  pipelinesRoute,
  ipsRoute, firewallsRoute, cloudIamRoute, upgradesRoute, basicRoute, notifyRoute, registrarsRoute, costRatesRoute, hostRecordsRoute,
  licenseRoute,
])

/**
 * 页面组件抛异常时的兜底。
 *
 * # ⚠️ 为什么必须由路由来管，而不是外面套一个 ErrorBoundary
 *
 * 原来是在 Outlet 外面套 `<ErrorBoundary key={pathname}>`，指望换页时
 * key 变化让它重建。实测（React 19.2 + TanStack Router 1.170）**不生效**：
 * 一页崩溃后切到下一页，URL、面包屑、侧栏高亮**全都更新了**，
 * 内容区却还显示着上一页的错误，要再切一次才恢复。
 * 补 `resetKey` 也没用 —— 说明那个边界压根没收到新 props，
 * 也就是说卡住的不是边界，是**路由的 match 没能切换**。
 *
 * 代价不只是难看：崩溃会**传染成假象**。上一轮全量验证就因此把两个
 * 本来好好的页面（/overview、/resources/lbs）误判成崩溃，
 * 单独整页加载复验才发现二者都是好的（OPSCMDB-014）。
 * **一个会伪造 bug 的 bug，比它自己造成的故障更贵** ——
 * 它让整轮验证结论都不可信。
 *
 * defaultErrorComponent 是路由自己的错误边界：它按 match 挂载，
 * 导航到别的路由时那个 match 连同错误状态一起被丢弃，
 * 不依赖 React 对错误边界的重建时机。
 */
function RouteCrash({ error, reset }: { error: Error; reset: () => void }) {
  const { t } = useTranslation()
  return (
    <ErrorState
      title={t('common:crash.title')}
      error={{
        cause: t('common:crash.cause'),
        // 原始异常给运维贴工单用，不翻译
        detail: error.message,
        retryable: true,
      }}
      retryLabel={t('common:crash.retry')}
      onRetry={reset}
    />
  )
}

export const router = createRouter({
  routeTree,
  // 路径写错时不要白屏。给一个能回到正常页面的出口。
  defaultNotFoundComponent: PlannedPage,
  // 页面渲染抛异常时由路由兜底（见 RouteCrash 顶部注释）
  defaultErrorComponent: RouteCrash,
})

declare module '@tanstack/react-router' {
  interface Register {
    router: typeof router
  }
}
