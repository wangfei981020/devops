import { useTranslation } from '@ops/i18n'
import { AppShell as SharedShell, cn } from '@ops/ui'
import type { ReactNode } from 'react'
import { RangePicker } from '../components/RangePicker.js'
import { NAV } from './nav.js'
import { filterNav } from '@ops/ui'

export type { NavItem, NavGroup } from '@ops/ui'
export { NAV } from './nav.js'

/**
 * 本产品的外壳。**只做三件事**：接上自己的菜单、权限判据、品牌信息。
 *
 * ⚠️ 侧栏本体在 `@ops/ui` 的 AppShell 里，不要在这里重新实现。
 * 这个文件原来有 394 行，是从 ops-cmdb 抄过来再改的 ——
 * 抄的过程中漏掉了**权限过滤**：菜单不按权限收，用户点进去才拿到 403。
 * 这正是"各写一份"的代价，而且靠文档拦不住（约定早就写着 CONVENTIONS §2.7.1）。
 * 守卫：tooling/scripts/check-shell-consistency.mjs
 */
export interface AppShellProps {
  /** 权限判据。由 App 传入 useSession() 的结果，见 lib/session.ts */
  can: (perm?: string) => boolean
  /** 权限还在加载：侧栏显示骨架而不是"什么都没有" */
  permsPending?: boolean
  activeKey: string
  onNavigate: (key: string) => void
  /** 全局状态灯：告警系统自己有没有问题。ok = 检测链路完整 */
  systemOk?: boolean
  systemHint?: string
  banner?: ReactNode
  username: string
  /**
   * 当前页面吃不吃这个时间范围。
   *
   * ⚠️ 不吃就**不显示**，而不是显示一个点了没反应的控件。
   * 顶栏常驻控件的问题在于它长得像"对整个产品生效"，
   * 在规则页上摆一个时间范围，人会以为规则列表被它筛过了。
   */
  rangeApplies: boolean
  /** 上一次成功刷新距今多少秒。null = 还没成功刷过 */
  liveAgeSec: number | null
  /** 数据已经不新鲜了。灯会从绿变黄，而不是继续假装实时 */
  liveStale: boolean
  /**
   * 功能是否已授权。用于两件事：EE 角标、以及把 hideWhenLocked 的项藏掉。
   *
   * ⚠️ 授权状态还在加载时**当成已授权**（返回 true）。
   * 当成未授权的话，每次刷新都会先闪一下"菜单少了几项"再补回来 ——
   * 而那一瞬间看起来像是授权掉了。
   */
  hasFeature: (feature: string) => boolean
  badges?: Record<string, number>
  children: ReactNode
}

export function AppShell({
  can,
  permsPending,
  activeKey,
  onNavigate,
  systemOk,
  systemHint,
  banner,
  username,
  rangeApplies,
  liveAgeSec,
  liveStale,
  hasFeature,
  badges,
  children,
}: AppShellProps) {
  const { t } = useTranslation()
  return (
    <SharedShell
      // ⚠️ 过滤在这里做而不是在 SharedShell 里：SharedShell 只按 can 过滤，
      // 而"未授权就不显示"是本产品的商业决定，不该塞进共享外壳
      nav={filterNav(NAV, can, hasFeature)}
      // 判据来自后端 /api/v1/me，与 internal/api/perm.go 用的是同一套权限码。
      // 前端这一层只决定显不显示，拦截在后端 PermGuard。
      can={can}
      permsPending={permsPending}
      brand="OpsAlert"
      version={__APP_VERSION__}
      activeKey={activeKey}
      onNavigate={onNavigate}
      badges={badges}
      banner={banner}
      breadcrumb={null}
      toolbar={
        <div className="flex min-w-0 flex-wrap items-center justify-end gap-2 text-xs text-muted-foreground">
          {/* 全局时间范围。告警产品的标配交互：改一次，图表、统计、
              列表一起跟着变。放顶栏而不是各页自己一份，是因为
              同一屏上两个时间筛选必然出现"两个数字对不上"。 */}
          {/* 全局时间范围。改一次，图表、统计、列表一起跟着变。
              ⚠️ md 以下藏起来，由页面内容区顶部那份接手（同一个 context，
              互斥显示）—— 顶栏在 390px 视口里塞不下，实测会把文档撑到
              526px 宽，整页横向滚动。 */}
          {rangeApplies && <RangePicker className="hidden md:flex" />}

          {/* 实时指示灯。
              ⚠️ 它**不是装饰**：读的是"上一次成功刷新离现在多久"。
              常亮绿灯在故障时最会骗人——后端挂了、轮询在失败、
              标签页被浏览器冻结，界面照样一片祥和，
              而你正盯着十分钟前的数据做判断。 */}
          {rangeApplies && liveAgeSec != null && (
            <span
              className={cn(
                'hidden items-center gap-1.5 rounded-md border border-border px-2 py-1 md:inline-flex',
                liveStale ? 'text-warning' : 'text-success',
              )}
              title={t('opsalert:live.updatedAgo', { seconds: liveAgeSec })}
            >
              <span
                className={cn(
                  'size-1.5 rounded-full',
                  liveStale ? 'bg-warning' : 'animate-ops-pulse bg-success',
                )}
                aria-hidden="true"
              />
              {/* 文字随状态变，颜色不是唯一线索 */}
              <span>{liveStale ? t('opsalert:live.stale') : t('opsalert:live.on')}</span>
            </span>
          )}

          {/* 状态灯：告警系统自检。放顶栏是因为它描述的是**整个系统**，
              而不是当前这一页 —— 跟着页面走的话，换一页就看不见了 */}
          {systemOk != null ? (
            <span className="flex items-center gap-1.5" title={systemHint}>
              <span
                className={cn('size-1.5 rounded-full', systemOk ? 'bg-success' : 'bg-warning')}
                aria-hidden="true"
              />
              <span>{systemOk ? t('opsalert:selfcheck.healthy') : t('opsalert:selfcheck.unhealthy')}</span>
            </span>
          ) : null}
          <span className="max-w-24 truncate">{username}</span>
        </div>
      }
      t={t}
    >
      {children}
    </SharedShell>
  )
}
