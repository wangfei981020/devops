import type { BannerTone } from '@ops/ui'
import type { LicenseInfo, LicenseStatus } from './queries.js'

// ⚠️ 本文件与 ops-cmdb 的同名文件**逐字相同**（除语言包命名空间）。
// 里面每条注释都对应一次真实踩坑：grace 的负数天数被 Math.max 吞掉、
// 七态文案不能合并、not_activated 不该报警。两处各写一份必然分叉。
// banner.test.ts 一并带过来了 —— 它才是这些结论的守卫。

/** 横幅内容。null = 这个状态不需要横幅。 */
export interface BannerSpec {
  tone: BannerTone
  /** 语言包 key */
  key: string
  params: Record<string, number | string>
}

/** 宽限期天数，与 licensekit 的 GraceDays / FingerprintGraceDays 一致。 */
export const GRACE_DAYS = 14

/**
 * 授权状态 → 顶部横幅。
 *
 * ⚠️ 七种状态**各自的文案必须不同**。把 `finger_mismat` 和 `expired`
 * 写成同一句"授权无效"，换来的是客户每次都要打电话问 ——
 * 一个该去重新签发（他刚迁完库），一个该去续费，动作完全不同。
 *
 * ⚠️ 两个状态刻意不出横幅：
 *   active         一切正常，不打扰
 *   not_activated  社区版是**正常状态不是错误**。挂横幅等于天天催客户买，
 *                  而他可能本来就只打算用社区版
 *
 * ⚠️ 色调也要分开：把"14 天后到期"和"已经只读"都渲染成红条，
 * 客户会把前者也当故障报；反过来把只读渲染成灰提示，
 * 又会让人一直以为是自己点错了。
 */
export function licenseBanner(info: LicenseInfo | undefined): BannerSpec | null {
  if (!info) return null

  switch (info.status) {
    case 'grace':
      // 还能用，只是到期了。剩余天数按宽限期算，不是按到期日。
      //
      // ⚠️ grace 状态下 daysUntilExpiry 是**负数**（已过期几天），这里要的就是
      // 用它去抵扣宽限期。早先写成 `Math.max(0, days) + GRACE_DAYS`，
      // 负数被 max 吃成 0，于是整个宽限期里横幅恒显示"还剩 14 天"，
      // 到第 15 天毫无预兆地转只读 —— 提前提醒的意义正好被抹掉。
      // 夹 0 只作下界兜底（时钟回拨等边界），不能用来吞掉负号。
      return {
        tone: 'warn',
        key: 'opsalert:license.graceBanner',
        params: { days: Math.max(0, GRACE_DAYS + (info.daysUntilExpiry ?? 0)) },
      }

    case 'finger_mismat':
      return {
        tone: 'warn',
        key: 'opsalert:license.fingerprintBanner',
        params: { days: GRACE_DAYS },
      }

    case 'expired':
      return { tone: 'bad', key: 'opsalert:license.expiredBanner', params: {} }

    case 'lapsed':
      return { tone: 'bad', key: 'opsalert:license.lapsedBanner', params: { days: 30 } }

    case 'not_licensed':
      return { tone: 'bad', key: 'opsalert:license.notLicensedBanner', params: {} }

    case 'active':
      // 快到期时提前提醒。用 shouldRemind 而不是自己算天数 ——
      // 提前多少天提醒是授权规范定的（30 天），前端自己定一个值就会和后端对不上
      if (info.shouldRemind && info.daysUntilExpiry !== null) {
        return {
          tone: 'info',
          key: 'opsalert:license.remindBanner',
          params: { days: info.daysUntilExpiry, grace: GRACE_DAYS },
        }
      }
      return null

    case 'not_activated':
      return null

    default: {
      // 认不出来的状态**不能当成正常**：共享库新增了一态而前端没跟上时，
      // 静默不显示等于把一个真实的授权问题藏起来。
      // 这里用 never 断言，新增状态时编译期就会失败。
      const never: never = info.status
      return {
        tone: 'warn',
        key: 'opsalert:license.unknownStatus',
        params: { status: String(never) },
      }
    }
  }
}

/** 状态徽章的色调。授权页上用，与横幅同一套语义。 */
export function statusTone(s: LicenseStatus): 'ok' | 'warn' | 'bad' | 'mute' {
  switch (s) {
    case 'active':
      return 'ok'
    case 'grace':
    case 'finger_mismat':
      return 'warn'
    case 'expired':
    case 'lapsed':
    case 'not_licensed':
      return 'bad'
    case 'not_activated':
      // 社区版是正常状态，用中性色。标红会让人以为装坏了
      return 'mute'
    default:
      return 'bad'
  }
}
