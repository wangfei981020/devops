import { describe, expect, it } from 'vitest'
import { phaseTone } from './podPhase.js'

describe('phaseTone', () => {
  it('Running 是正常', () => {
    expect(phaseTone('Running')).toBe('ok')
  })

  // 这条是这个文件存在的主要理由
  it('Succeeded 不能标红 —— 跑完的 Job 是正常终态', () => {
    expect(phaseTone('Succeeded')).toBe('mute')
  })

  it('Pending 是警告不是故障：调度中还有希望', () => {
    expect(phaseTone('Pending')).toBe('warn')
  })

  it('Failed 是故障', () => {
    expect(phaseTone('Failed')).toBe('bad')
  })

  // 不认识的枚举值当成正常，等于把新出现的故障态静默放过
  it.each(['Unknown', 'CrashLoopBackOff', '', 'SomePhaseK8sAddsIn2030'])(
    '不认识的 phase %s 按故障处理',
    (phase) => {
      expect(phaseTone(phase)).toBe('bad')
    },
  )
})
