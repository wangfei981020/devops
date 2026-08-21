import { describe, expect, it } from 'vitest'
import { formatK8sMem } from './columns.js'

/**
 * k8s 的内存量词转人读单位。
 *
 * 🔴 起因：节点页的「容量」列原样显示 `8125932Ki` —— 那一列的用途是判断
 * "这台放不放得下一个 8 核 Pod"，而没人拿 Ki 做这个判断。
 */
describe('formatK8sMem', () => {
  it('常见量词都能转', () => {
    expect(formatK8sMem('8125932Ki')).toBe('7.7 Gi') // 8125932/1024² = 7.749
    expect(formatK8sMem('32Gi')).toBe('32.0 Gi')
    expect(formatK8sMem('131072Mi')).toBe('128 Gi')
    expect(formatK8sMem('1Ti')).toBe('1024 Gi')
  })

  it('裸数字按字节算', () => {
    // 4 GiB = 4294967296 字节
    expect(formatK8sMem('4294967296')).toBe('4.0 Gi')
  })

  /**
   * ⚠️ 这一条是关键：**认不出来要原样返回**。
   *
   * 返回空或 0 的话，一个没见过的单位后缀会被显示成
   * 「这台没采到内存」—— 而事实是我们采到了，只是没认出格式。
   * 那是把"不认识"渲染成了"没有"。
   */
  it('认不出来的原样返回，不能变成空或 0', () => {
    for (const weird of ['8125932Ei', 'abc', '12 GB', '1.5x']) {
      const got = formatK8sMem(weird)
      expect(got).toBe(weird)
      expect(got).not.toBe('')
      expect(got).not.toContain('NaN')
    }
  })

  it('真的没有值时才显示占位符', () => {
    expect(formatK8sMem('')).toBe('—')
    expect(formatK8sMem(null)).toBe('—')
    expect(formatK8sMem(undefined)).toBe('—')
  })
})
