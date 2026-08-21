import type { BadgeTone } from '@ops/ui'

/**
 * 环境的徽章色。
 *
 * # 为什么需要这一层映射
 *
 * `environments.tag_type` 存的是**旧版 Element Plus 的调色名**
 * （danger / warning / primary / success / info，见 migration 003 的种子数据），
 * 而新版 Badge 用的是语义 tone（ok / warn / bad / info / mute）。
 * 两套词表不通用，直接把库里的值传给 Badge 会全部落到兜底色上 ——
 * 不报错、不为空，只是所有环境长得一模一样。
 *
 * 🔴 生产标红是有实际价值的，不是装饰：它是「你正在动的是生产」这句话的视觉形式。
 * 此前新版把所有环境徽章硬编码成 `tone="mute"`，PROD 和 DEV 看起来完全一样，
 * 而 `tag_type` 这一列在库里一直有值、也一直被后端收着 ——
 * 既看不见也改不了（OPSCMDB-039）。
 *
 * ⚠️ 认不出来的值一律回落 `mute`，**不要**猜。
 * 客户可以自己建环境并填任意 tag_type，猜错颜色比统一中性色更坏：
 * 一个本该醒目的生产环境被染成绿色，比它是灰色危险得多。
 */
const TONE: Record<string, BadgeTone> = {
  danger: 'bad',
  warning: 'warn',
  success: 'ok',
  primary: 'info',
  info: 'mute',
}

export function envTone(tagType?: string | null): BadgeTone {
  if (!tagType) return 'mute'
  return TONE[tagType] ?? 'mute'
}

/** 可选的徽章样式。给字典编辑器用 —— 取值域必须和上面这张表一致。 */
export const ENV_TAG_TYPES = ['info', 'primary', 'success', 'warning', 'danger'] as const
