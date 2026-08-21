import { type NavGroup } from '@ops/ui'
import { Activity, Newspaper, AlertTriangle, BellOff, Database, FileSearch, History, LayoutTemplate, Mail, Plug, Radar, Route, Search, Send, ShieldCheck, SlidersHorizontal, KeyRound, LogIn, MessageSquareText, Users } from 'lucide-react'

/**
 * 菜单的**唯一**数据源。外壳由 @ops/ui 的 AppShell 统一提供。
 *
 * ⚠️ 不要在组件里再内嵌一份菜单 —— 原来这个数组就写在 AppShell.tsx 里，
 * 于是"改外壳"和"改菜单"变成同一件事，抄外壳的人连菜单一起抄走了。
 *
 * ⚠️ 每一项的 perm 必须与后端 `internal/api/perm.go` 的映射表**同名**。
 * 这里写错不会报错，只会让菜单和接口的判据分叉：
 * 菜单看得见、点进去 403（写多了），或菜单看不见、其实有权限（写少了）。
 * 后端那张表是唯一真相，这里是它的投影。
 * 见 CONVENTIONS §2.7.5。
 */
export const NAV: NavGroup[] = [
  {
    key: 'watch',
    labelKey: 'opsalert:nav.group.watch',
    icon: Activity,
    items: [
      { key: 'warroom', labelKey: 'opsalert:nav.warroom', icon: Activity, perm: 'menu:alert_warroom' },
      // 同一批数据的另一种呈现：编辑式简报。与值班台并存而不是替换 ——
      // 「扫一眼有没有事」和「逐条处理」是两种用法，塞进一个页面必然互相妥协
      { key: 'brief', labelKey: 'opsalert:nav.brief', icon: Newspaper, perm: 'menu:alert_warroom' },
      { key: 'incidents', labelKey: 'opsalert:nav.incidents', icon: AlertTriangle, perm: 'menu:alert_incidents' },
      // 日志检索归"战情"而不是"接入"：它是排障动作，
      // 用的时候人在处理某条告警，而不是在配数据源
      { key: 'explore', labelKey: 'opsalert:nav.explore', icon: Search, perm: 'menu:alert_explore' },
      { key: 'report', labelKey: 'opsalert:nav.report', icon: Mail, perm: 'menu:alert_report' },
    ],
  },
  {
    key: 'detect',
    labelKey: 'opsalert:nav.group.detect',
    icon: SlidersHorizontal,
    items: [
      { key: 'rules', labelKey: 'opsalert:nav.rules', icon: SlidersHorizontal, perm: 'menu:alert_rules' },
      // 模板与规则同属"检测"，共用 menu:alert_rules：能看规则的人就该能看模板，
      // 单独一个权限码只会多一处要维护、且必然有人忘了配
      { key: 'templates', labelKey: 'opsalert:nav.templates', icon: LayoutTemplate, perm: 'menu:alert_rules' },
      { key: 'backtest', labelKey: 'opsalert:nav.backtest', icon: History, perm: 'menu:alert_backtest', feature: 'backtest' },
    ],
  },
  {
    key: 'noise',
    labelKey: 'opsalert:nav.group.noise',
    icon: Radar,
    items: [
      { key: 'silences', labelKey: 'opsalert:nav.silences', icon: BellOff, perm: 'menu:alert_silences' },
      { key: 'noisetop', labelKey: 'opsalert:nav.noisetop', icon: Radar, perm: 'menu:alert_noisetop', feature: 'noise' },
    ],
  },
  {
    key: 'integrate',
    labelKey: 'opsalert:nav.group.integrate',
    icon: Database,
    items: [
      { key: 'datasources', labelKey: 'opsalert:nav.datasources', icon: Database, perm: 'menu:alert_datasources' },
      { key: 'notifiers', labelKey: 'opsalert:nav.notifiers', icon: Send, perm: 'menu:alert_notifiers' },
      // 文案模板挨着通知渠道放：它是"发出去长什么样"，
      // 与"发到哪里"是同一件事的两面
      { key: 'msgtpl', labelKey: 'opsalert:nav.msgtpl', icon: MessageSquareText, perm: 'menu:alert_templates_msg' },
      { key: 'routes', labelKey: 'opsalert:nav.routes', icon: Route, perm: 'menu:alert_routes' },
    ],
  },
  {
    key: 'platform',
    labelKey: 'opsalert:nav.group.platform',
    icon: ShieldCheck,
    items: [
      { key: 'selfcheck', labelKey: 'opsalert:nav.selfcheck', icon: ShieldCheck, perm: 'menu:alert_selfcheck' },
      // ⚠️ AI 接入未授权时**完全不显示**（不是打 EE 角标）。
      // 这是商业决定：其余 EE 功能希望被看见并促成购买，这一项不希望。
      // 后端 ee/mcp 的路由同时返回 404 —— 只藏菜单是藏不住的。
      { key: 'mcp', labelKey: 'opsalert:nav.mcp', icon: Plug, perm: 'menu:alert_mcp', feature: 'mcp_full', hideWhenLocked: true },
      { key: 'audit', labelKey: 'opsalert:nav.audit', icon: FileSearch, perm: 'menu:alert_audit' },
      // 账号与角色。单独一个权限码：能看审计不等于能建账号
      { key: 'users', labelKey: 'opsalert:nav.users', icon: Users, perm: 'menu:alert_users' },
      // 授权只给 admin：状态里含档次、到期日、安装指纹与客户名，属于商务信息
      // SSO 只给 admin：这里能配的东西等于"谁能进这套系统"
      { key: 'sso', labelKey: 'opsalert:nav.sso', icon: LogIn, perm: 'menu:alert_sso' },
      { key: 'license', labelKey: 'opsalert:nav.license', icon: KeyRound, perm: 'menu:alert_license' },
    ],
  },
]

/**
 * 菜单 key → 权限码。由 NAV 推导，不手写第二份。
 *
 * 页面级守卫用它判断"这个页面能不能进"。手写一份的话，
 * 加菜单时只改一处 → 菜单收了但页面进得去，或反过来。
 */
export const NAV_PERM: Record<string, string> = Object.fromEntries(
  NAV.flatMap((g) => g.items.map((i) => [i.key, i.perm ?? ''])).filter(([, p]) => p),
)
