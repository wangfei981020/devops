import { useTranslation } from '@ops/i18n'
import { AppShell as SharedShell, filterNav } from '@ops/ui'
import type { ReactNode } from 'react'
import { type Session, can } from '../lib/session.js'
import { NAV, type NavGroup } from './nav.js'

/**
 * 本产品的外壳。**只做三件事**：接上自己的菜单、权限判据、品牌信息。
 *
 * ⚠️ 侧栏本体在 `@ops/ui` 的 AppShell 里，不要在这里重新实现。
 * 原来三个产品各写各的（cmdb 351 行 / alert 394 行 / sso 87 行），
 * 结果不只是长得不一样 —— ops-alert 那份抄过去时漏掉了权限过滤，
 * 菜单不按权限收，用户点进去才拿到 403。
 * 这类漂移靠文档拦不住（约定早就写着），只有"没有第二份"才拦得住。
 * 守卫：tooling/scripts/check-shell-consistency.mjs
 */
export interface AppShellProps {
  activeKey: string
  onNavigate: (key: string) => void
  breadcrumb: ReactNode
  toolbar?: ReactNode
  children: ReactNode
  /** 当前会话。`undefined` = 还没拿到（加载中或失败），此时不渲染菜单项 */
  session: Session | undefined
  /** 权限是否还在加载。与"加载失败"必须分开，见共享外壳里的说明 */
  permsPending: boolean
  /** 全局横幅（授权过期等） */
  banner?: ReactNode
}

/**
 * 按权限过滤菜单。过滤规则本身在 `@ops/ui` 的 filterNav 里，
 * 这里只负责把本产品的会话结构翻译成 can(perm) 判据。
 */
export function visibleNav(session: Session | undefined): NavGroup[] {
  if (!session) return []
  return filterNav(NAV, (perm) => hasPerm(session, perm)) as NavGroup[]
}

/**
 * 共享外壳的 perm 是可选的：**没有 perm = 无需权限，直接显示**。
 * 本产品的 can() 要求必传，所以在这里补上这条语义。
 *
 * ⚠️ 缺省方向必须是"显示"而不是"隐藏"：
 * 反过来的话，任何忘了写 perm 的菜单项会对所有人静默消失，
 * 而那看起来和"功能没做"一模一样。
 */
function hasPerm(session: Session | undefined, perm?: string): boolean {
  if (!session) return false
  return perm ? can(session, perm) : true
}

export function AppShell({ session, permsPending, ...rest }: AppShellProps) {
  const { t } = useTranslation()
  return (
    <SharedShell
      {...rest}
      nav={session ? NAV : []}
      can={(perm) => hasPerm(session, perm)}
      brand="OpsPlane"
      version={__APP_VERSION__}
      permsPending={permsPending}
      t={t}
      /*
        窄屏折叠。共享外壳里这是 opt-in（默认 false），理由见 @ops/ui 的 AppShell：
        已上线产品要各自验证过窄屏表现再开，不该被一次共享包升级顺带改掉布局。

        🔴 本产品验过了，所以在这里开：
        生产 v0.109.0 实测 390px 下 7/7 页横向溢出 34px，侧栏仍占 216px（视口的 55%），
        内容区只剩 174px —— 说明文字每行 1 个字竖排成条、金额被右边界切断、
        页头按钮挤出视口（OPSCMDB-031 P0-24）。

        ⚠️ 能力早就做好了、也构建进了 v0.109.0，只是这个开关没人打开 ——
        所以它在进度表上是"已修"，在生产上是原样。
        这类"做完了但没接上"是本项目最高频的缺陷形状（一轮验收撞了 17 次）。
      */
      responsive
    />
  )
}
