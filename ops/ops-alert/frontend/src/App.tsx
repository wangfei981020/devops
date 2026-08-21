import { useTranslation } from '@ops/i18n'
import { useQuery } from '@tanstack/react-query'
import { Banner, NoPermission } from '@ops/ui'
import { useEffect, useState } from 'react'
import { AppShell } from './layouts/AppShell.js'
import { NAV_PERM } from './layouts/nav.js'
import { can, useSession } from './lib/session.js'
import { get } from './lib/api.js'
import { TimeRangeProvider, useFreshness } from './lib/timerange.js'
import { useLicense } from './routes/license/queries.js'
import { BriefPage } from './routes/Brief.js'
import { WarRoomPage } from './routes/WarRoom.js'
import { IncidentsPage } from './routes/Incidents.js'
import { RulesPage } from './routes/Rules.js'
import { TemplatesPage } from './routes/Templates.js'
import { UsersPage } from './routes/Users.js'
import { BacktestPage } from './routes/Backtest.js'
import { SilencesPage, NoiseTopPage } from './routes/Noise.js'
import { DatasourcesPage, NotifiersPage, RoutesPage } from './routes/Integrations.js'
import { SelfCheckPage, AuditPage } from './routes/Platform.js'
import { McpPage } from './routes/Mcp.js'
import { ExplorePage } from './routes/Explore.js'
import { ReportPage } from './routes/Report.js'
import { LicensePage } from './routes/license/index.js'
import { FeatureLocked } from './components/FeatureLocked.js'
import { MsgTemplatesPage } from './routes/MsgTemplates.js'
import { SsoPage } from './routes/Sso.js'

type SelfCheck = {
  datasources: { total: number; down: number }
  rules: { total: number; failing: number }
  notify_failed_24h: number
  silent_sources: string[]
}

/**
 * 浏览器标签页标题。复用导航的 i18n key，避免同一个页面出现两套名字。
 *
 * 顶部导航范式下页面内不再有面包屑——主导航 + tab 已经把"我在哪"说清了，
 * 再加一行面包屑是同一件事说三遍。
 */
/**
 * 会消费全局时间范围的页面。
 *
 * ⚠️ 名单严格按**后端真的接了 range 参数**来列，不按"看起来应该有"来列。
 * 目前只有 /incidents 和 /incidents/series 支持（api.go:297、timeseries.go）。
 * 把值班台、噪音榜也列进来的话，顶栏会摆出一个点了没反应的控件 ——
 * 那比没有控件更糟：人会以为列表已经被筛过了。
 * 后端给这些接口补上 range 之后，再往这里加。
 *
 * 名单写死而不是让页面自己"注册"：注册式的做法在页面还没挂载时
 * 顶栏不知道该不该显示控件，会先闪一下再消失。
 */
const RANGE_PAGES = new Set(['incidents'])

const TITLE_KEYS: Record<string, string> = {
  warroom: 'opsalert:nav.warroom',
  brief: 'opsalert:nav.brief',
  incidents: 'opsalert:nav.incidents',
  rules: 'opsalert:nav.rules',
  templates: 'opsalert:nav.templates',
  backtest: 'opsalert:nav.backtest',
  silences: 'opsalert:nav.silences',
  noisetop: 'opsalert:nav.noisetop',
  datasources: 'opsalert:nav.datasources',
  notifiers: 'opsalert:nav.notifiers',
  routes: 'opsalert:nav.routes',
  selfcheck: 'opsalert:nav.selfcheck',
  mcp: 'opsalert:nav.mcp',
  audit: 'opsalert:nav.audit',
  users: 'opsalert:nav.users',
  explore: 'opsalert:nav.explore',
  report: 'opsalert:nav.report',
  license: 'opsalert:nav.license',
  msgtpl: 'opsalert:nav.msgtpl',
  sso: 'opsalert:nav.sso',
}

/**
 * 轻量 hash 路由。页面就这么几个，引一个路由库的收益抵不过它的升级负担。
 *
 * 支持一级子路径：`#incidents/123` → page=incidents, param=123。
 * 事件详情必须有自己的地址 —— 值班时最常见的动作就是把某一条事件
 * 的链接甩进群里，而抽屉式详情根本没有可分享的地址。
 */
function useHashRoute(): [string, string, (k: string) => void] {
  const read = () => location.hash.slice(1) || 'warroom'
  const [raw, setRaw] = useState(read)
  useEffect(() => {
    const onHash = () => setRaw(read())
    addEventListener('hashchange', onHash)
    return () => removeEventListener('hashchange', onHash)
  }, [])
  const slash = raw.indexOf('/')
  const page = slash < 0 ? raw : raw.slice(0, slash)
  const param = slash < 0 ? '' : raw.slice(slash + 1)
  return [page, param, (k: string) => { location.hash = k }]
}

export function App() {
  return (
    <TimeRangeProvider>
      <Console />
    </TimeRangeProvider>
  )
}

/** 时间范围要在 AppShell 和各页面之间共享，所以 Provider 必须在外面一层。 */
function Console() {
  const { t } = useTranslation()
  const [active, param, navigate] = useHashRoute()

  // 自检每 60 秒拉一次：它同时供侧边栏角标和全局横幅使用。
  const check = useQuery({
    queryKey: ['selfcheck'],
    queryFn: () => get<SelfCheck>('/selfcheck'),
    refetchInterval: 60_000,
  })
  const incidents = useQuery({
    queryKey: ['incidents', 'active', 'count'],
    queryFn: () => get<{ items: unknown[] }>('/incidents?status=active'),
    refetchInterval: 30_000,
  })
  // 顶栏那盏灯读的就是这个查询的成功时刻：它是全站刷得最勤的一个，
  // 它停了就意味着整个界面都在看旧数据
  const live = useFreshness(incidents.dataUpdatedAt || undefined, 30_000)
  // 授权状态决定菜单里哪些 EE 项显示、哪些藏起来。
  //
  // ⚠️ 加载中一律返回 true（当成已授权）。当成未授权的话，
  // 每次刷新都会先闪一下"菜单少了几项"再补回来，看起来像授权掉了。
  const lic = useLicense()
  const hasFeature = (f: string) => (lic.data ? (lic.data.features[f]?.has ?? false) : true)

  // 顶栏不再有面包屑，标签页标题成为"我在哪"的唯一持久线索——
  // 多开几个标签排障时，全是 "OpsAlert" 会分不清哪个是哪个。
  useEffect(() => {
    document.title = `${t(TITLE_KEYS[active] ?? active)} · OpsAlert`
  }, [active, t])

  const session = useSession()
  // 菜单按权限收了，但 hash 是可以手输的（也可能是收藏夹里的旧链接）。
  // 没有守卫的话，无权的人会看到一个正常页面外壳 + 满屏 403 错误态，
  // 分不清是"我没权限"还是"系统坏了"。
  //
  // ⚠️ 权限还在加载时**不判**：那一瞬间 permissions 是空集，
  // 判了就会先闪一下"无权访问"再跳回正常页面。
  const activePerm = NAV_PERM[active] ?? ''
  const denied = !session.isPending && activePerm !== '' && !can(session.data, activePerm)
  // 用户名以后端为准，localStorage 只是首屏渲染前的占位：
  // 换账号登录而 localStorage 没更新时，顶栏会显示上一个人的名字
  const username = session.data?.displayName || readUsername()
  const sc = check.data

  // 全局横幅只留给「告警系统自己出问题」这一类：
  // 数据源不可达、日志断流、通知投递失败。业务告警在值班台里，
  // 混在一起会让真正致命的那条被淹没。
  const problems: string[] = []
  if (sc?.datasources.down) problems.push(t('opsalert:banner.dsDown', { count: sc.datasources.down }))
  if (sc?.rules.failing) problems.push(t('opsalert:banner.rulesFailing', { count: sc.rules.failing }))
  if (sc?.silent_sources.length)
    problems.push(t('opsalert:banner.sourcesSilent', { count: sc.silent_sources.length }))
  if (sc?.notify_failed_24h)
    problems.push(t('opsalert:banner.notifyFailed', { count: sc.notify_failed_24h }))

  return (
    <AppShell
      // ⚠️ perm 为空 = 该项无需权限，必须放行。
      // 直接写 can(session.data, perm ?? '') 的话，空串会走进"有没有这个码"
      // 的判断并返回 false —— 没标 perm 的分组会整个消失。
      can={(perm) => !perm || can(session.data, perm)}
      permsPending={session.isPending}
      activeKey={active}
      onNavigate={navigate}
      username={username}
      // ⚠️ 只在真正按时间范围取数的页面上显示这个控件。
      // 规则页、通知渠道页跟时间无关，摆一个点了没反应的时间范围
      // 会让人以为列表被筛过了。
      rangeApplies={RANGE_PAGES.has(active)}
      liveAgeSec={live.ageSec}
      liveStale={live.stale}
      hasFeature={hasFeature}
      badges={{ incidents: incidents.data?.items.length ?? 0 }}
      systemOk={sc ? problems.length === 0 : undefined}
      systemHint={
        problems.length > 0
          ? t('opsalert:banner.selfcheckAbnormal', { problems: problems.join(t('opsalert:banner.separator')) })
          : t('opsalert:selfcheck.healthy')
      }
      banner={
        problems.length > 0 ? (
          <Banner tone="warn">
            {t('opsalert:banner.selfcheckAbnormal', { problems: problems.join(t('opsalert:banner.separator')) })}
          </Banner>
        ) : undefined
      }
    >
      {denied ? (
        // 用共享的 NoPermission 而不是自己写一个：它已经把「露出权限码」
        // 「不自动跳首页」这些结论固化进去了，各产品重写一遍必然丢掉其中几条
        <NoPermission
          title={t('opsalert:perm.deniedTitle')}
          reason={t('opsalert:perm.deniedReason')}
          code={activePerm}
          copyLabel={t('opsalert:perm.copy')}
          copiedLabel={t('opsalert:perm.copied')}
        />
      ) : (
        <>
          {active === 'warroom' && <WarRoomPage onNavigate={navigate} />}
          {active === 'brief' && <BriefPage onNavigate={navigate} />}
          {active === 'incidents' && (
            <IncidentsPage
              detailId={param ? Number(param) : null}
              onOpen={(id) => navigate(`incidents/${id}`)}
              onBack={() => navigate('incidents')}
            />
          )}
          {active === 'rules' && <RulesPage />}
          {active === 'templates' && <TemplatesPage />}
          {active === 'backtest' && (
            <FeatureLocked feature="backtest">
              <BacktestPage />
            </FeatureLocked>
          )}
          {active === 'silences' && <SilencesPage />}
          {active === 'noisetop' && (
            <FeatureLocked feature="noise">
              <NoiseTopPage />
            </FeatureLocked>
          )}
          {active === 'datasources' && <DatasourcesPage />}
          {active === 'notifiers' && <NotifiersPage />}
          {active === 'routes' && <RoutesPage />}
          {active === 'selfcheck' && <SelfCheckPage />}
          {active === 'mcp' && <McpPage />}
          {active === 'audit' && <AuditPage />}
          {active === 'users' && <UsersPage />}
          {active === 'explore' && <ExplorePage />}
          {active === 'report' && <ReportPage />}
          {active === 'license' && <LicensePage />}
          {active === 'msgtpl' && <MsgTemplatesPage />}
          {active === 'sso' && <SsoPage />}
        </>
      )}
    </AppShell>
  )
}

function readUsername(): string {
  return localStorage.getItem('opsalert.username') ?? 'unknown'
}
