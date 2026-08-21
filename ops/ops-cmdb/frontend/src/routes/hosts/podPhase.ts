import type { BadgeTone } from '@ops/ui'

/**
 * Pod phase → 语义色。
 *
 * 单独成文件是为了能测：这段逻辑的错误形态是「一片假故障」，
 * 而假故障在界面上看起来完全正常（红标就是红标），
 * 只有把每个 phase 的期望值写下来才拦得住。
 *
 * ⚠️ Succeeded 是 mute 不是 bad：跑完的 Job Pod 就该是 Succeeded。
 * 把它标红，每个有定时任务的节点上都会挂几条假故障 ——
 * 而假故障看多了，真故障就没人看了。
 *
 * ⚠️ default 是 bad 不是 mute：不认识的 phase 意味着我们的枚举过时了，
 * 当成"正常"会让新出现的故障态静默混过去。
 */
export function phaseTone(phase: string): BadgeTone {
  switch (phase) {
    case 'Running':
      return 'ok'
    case 'Succeeded':
      return 'mute'
    case 'Pending':
      return 'warn'
    case 'Failed':
      return 'bad'
    default:
      return 'bad'
  }
}
