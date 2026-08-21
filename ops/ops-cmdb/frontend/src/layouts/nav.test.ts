import { describe, expect, it } from 'vitest'
import type { Session } from '../lib/session.js'
import { visibleNav } from './AppShell.js'
import { NAV } from './nav.js'

function session(over: Partial<Session>): Session {
  return {
    username: 'u',
    displayName: 'u',
    authSource: 'local',
    isAdmin: false,
    permissions: new Set<string>(),
    ...over,
  }
}

const ALL_ITEMS = NAV.flatMap((g) => g.items).length

describe('visibleNav', () => {
  it('管理员看到全部菜单', () => {
    const nav = visibleNav(session({ isAdmin: true }))
    expect(nav.flatMap((g) => g.items).length).toBe(ALL_ITEMS)
  })

  it('只按权限码过滤，不看 authSource', () => {
    // 「本地账号 = 管理员」这条推导已经栽过三次。
    // 本地账号但 isAdmin=false 且没有任何权限码时，就该一个菜单都没有。
    const nav = visibleNav(session({ authSource: 'local' }))
    expect(nav).toEqual([])
  })

  it('整组都没权限时连组标题一起去掉', () => {
    // 只剩一个标题的空分组看起来像加载失败
    const nav = visibleNav(session({ permissions: new Set(['menu:cmdb_hosts']) }))
    expect(nav.map((g) => g.key)).toEqual(['cloud'])
    expect(nav[0]?.items.map((i) => i.key)).toEqual(['hosts'])
  })

  it('没有会话（还没拿到 /api/me）时什么都不显示', () => {
    // 兜底成"全显示"会在权限到手前泄露入口，
    // 而那一瞬间足够让人点进去
    expect(visibleNav(undefined)).toEqual([])
  })

  it('每个菜单项都配了权限码', () => {
    // perm 在类型上是必填，但写成空串能绕过去 ——
    // 空串会让 can() 恒为 true，等于这一项没有权限约束
    const blank = NAV.flatMap((g) => g.items).filter((i) => i.perm.trim() === '')
    expect(blank.map((i) => i.key)).toEqual([])
  })
})
