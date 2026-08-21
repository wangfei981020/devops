import zh from '@ops/i18n/locales/zh-CN/opsalert.json'
import { describe, expect, it } from 'vitest'
import { GRACE_DAYS, licenseBanner, statusTone } from './banner.js'
import type { LicenseInfo, LicenseStatus } from './queries.js'

const ALL: LicenseStatus[] = [
  'active',
  'grace',
  'expired',
  'lapsed',
  'finger_mismat',
  'not_activated',
  'not_licensed',
]

function info(over: Partial<LicenseInfo>): LicenseInfo {
  return {
    status: 'active',
    readOnly: false,
    daysUntilExpiry: 100,
    shouldRemind: false,
    perpetual: false,
    licensee: null,
    expiresAt: null,
    licenseId: '',
    capacity: {},
    ceLimits: {},
    features: {},
    canActivate: true,
    ...over,
  }
}

/**
 * 从语言包里按 `opsalert:license.x` 取值，用于比对文案是否真的各不相同。
 *
 * ⚠️ 前缀是命名空间名，必须与 import 的那份 json 对应。
 * 从 CMDB 搬过来时这里还剥着 `common:`，而 key 已经是 `opsalert:` ——
 * 于是每个 key 都取不到值、全部返回空串，测试报的是
 * "expected '' not to be ''"，看起来像语言包漏了文案，实际是取值路径错了。
 */
function copy(key: string): string {
  const path = key.replace('opsalert:', '').split('.')
  let cur: unknown = zh
  for (const k of path) cur = (cur as Record<string, unknown>)?.[k]
  return typeof cur === 'string' ? cur : ''
}

describe('licenseBanner', () => {
  it('正常与社区版不挂横幅', () => {
    // 社区版是**正常状态不是错误**。挂横幅等于天天催客户买，
    // 而他可能本来就只打算用社区版
    expect(licenseBanner(info({ status: 'active' }))).toBeNull()
    expect(licenseBanner(info({ status: 'not_activated' }))).toBeNull()
  })

  it('快到期时按后端的 shouldRemind 提醒，不自己算天数', () => {
    // 提前多少天提醒是授权规范定的，前端自己定一个值就会和后端对不上
    expect(licenseBanner(info({ shouldRemind: false, daysUntilExpiry: 3 }))).toBeNull()
    const b = licenseBanner(info({ shouldRemind: true, daysUntilExpiry: 3 }))
    expect(b?.tone).toBe('info')
    expect(b?.params.days).toBe(3)
  })

  it('五种非正常状态各有横幅', () => {
    for (const s of ['grace', 'expired', 'lapsed', 'finger_mismat', 'not_licensed'] as const) {
      expect(licenseBanner(info({ status: s })), s).not.toBeNull()
    }
  })

  // 这条是整个文件存在的理由
  it('七种状态的文案必须两两不同', () => {
    const texts = ALL.map((s) => {
      const b = licenseBanner(info({ status: s, shouldRemind: false }))
      // 没有横幅的状态用状态说明来比，一样不能重复
      return b ? copy(b.key) : copy(`opsalert:license.statusHint.${s}`)
    })
    for (const t of texts) expect(t).not.toBe('')
    expect(new Set(texts).size).toBe(ALL.length)
  })

  it('色调分三档，不能全红', () => {
    // 把"14 天后到期"和"已经只读"都渲染成红条，客户会把前者也当故障报；
    // 反过来把只读渲染成灰提示，又会让人一直以为是自己点错了
    expect(licenseBanner(info({ status: 'grace' }))?.tone).toBe('warn')
    expect(licenseBanner(info({ status: 'finger_mismat' }))?.tone).toBe('warn')
    expect(licenseBanner(info({ status: 'expired' }))?.tone).toBe('bad')
    expect(licenseBanner(info({ status: 'lapsed' }))?.tone).toBe('bad')
    expect(licenseBanner(info({ status: 'not_licensed' }))?.tone).toBe('bad')
  })

  it('宽限期剩余天数随已过期天数递减', () => {
    // grace 状态下 daysUntilExpiry 是**负数**（已过期几天），它要去抵扣宽限期。
    //
    // 早先这里写的是 `Math.max(0, days) + GRACE_DAYS`，负号被吃掉，
    // 于是整个宽限期内横幅恒显示"还剩 14 天"，第 15 天毫无预兆转只读 ——
    // 提前提醒的全部意义正好被抹掉。这条用例当时还把该行为写成了期望。
    expect(licenseBanner(info({ status: 'grace', daysUntilExpiry: -3 }))?.params.days).toBe(
      GRACE_DAYS - 3,
    )
    expect(licenseBanner(info({ status: 'grace', daysUntilExpiry: 0 }))?.params.days).toBe(
      GRACE_DAYS,
    )
    // 夹 0 只作下界兜底（时钟回拨等边界），不能显示负数天
    expect(licenseBanner(info({ status: 'grace', daysUntilExpiry: -30 }))?.params.days).toBe(0)
  })

  it('没有数据时不挂横幅', () => {
    // 加载中就报"授权异常"，是把"还不知道"说成了"出问题了"
    expect(licenseBanner(undefined)).toBeNull()
  })
})

describe('statusTone', () => {
  it('社区版用中性色，不标红', () => {
    // 标红会让人以为装坏了
    expect(statusTone('not_activated')).toBe('mute')
  })

  it('每个状态都有色调', () => {
    for (const s of ALL) expect(statusTone(s)).toBeTruthy()
  })
})
