/**
 * 判定的**视觉语义**：七态各自长什么样，只在这里定义一次。
 *
 * 🔴 为什么要单独一个文件：判定色至少出现在三个地方 ——
 * 单元格标记、行首色带、顶部统计条。三处各写一份 map，
 * 改了其中一处忘了另外两处，界面会出现「统计条说 3 个冲突、
 * 表里只有 1 行是红的」这种自相矛盾，而它不报错、也不好复现。
 */

/** 五类语气。判定有七个，但**看的人只需要分五类**。 */
export type VerdictKind =
  | 'ok' // 一致
  | 'diff' // 确实不一样（落后/超前）
  | 'bad' // 确实有问题（冲突）
  | 'unknown' // 🔴 不知道 —— 与 bad 严格分开，见下
  | 'none' // 该列没有这个服务

/**
 * 🔴 unknown 不用红。
 *
 * no_data 和「非版本化 tag」都属于「我们没读到 / 判不了」，不是「对方出事了」。
 * 对方 token 一过期，整整一列都是 no_data —— 如果画成红，一眼看去像
 * 「对方全线故障」，而事实只是我们没拿到数据。
 *
 * 但也不能画成普通灰，那会被当成中性事实一扫而过。
 * 用**虚线边框**：灰色说明它不是故障，虚线说明这里缺了东西。
 */
export const KIND: Record<string, VerdictKind> = {
  same: 'ok',
  behind: 'diff',
  ahead: 'diff',
  conflict: 'bad',
  unknown: 'unknown',
  no_data: 'unknown',
  missing_here: 'none',
  missing_base: 'none',
}

/** 单元格判定标记的样式。方角 + 左竖线 —— 密集表格里圆角胶囊会和行边界打架。 */
export const CHIP: Record<VerdictKind, string> = {
  ok: 'bg-success-bg text-success shadow-[inset_2px_0_0_var(--color-success)]',
  diff: 'bg-warning-bg text-warning shadow-[inset_2px_0_0_var(--color-warning)]',
  bad: 'bg-danger-bg text-danger shadow-[inset_2px_0_0_var(--color-danger)]',
  unknown: 'border border-dashed border-border-strong text-muted-foreground',
  none: 'bg-muted text-muted-foreground shadow-[inset_2px_0_0_var(--color-border-strong)]',
}

/** 行首色带。unknown 用虚线质感（CSS 里画成断续条），其余是实心。 */
export const STRIPE: Record<VerdictKind, string> = {
  ok: 'bg-success',
  diff: 'bg-warning',
  bad: 'bg-danger',
  unknown: 'ops-stripe-dashed',
  none: 'bg-border-strong',
}

/** 统计条上的数字颜色。 */
export const STAT: Record<VerdictKind, string> = {
  ok: 'text-success',
  diff: 'text-warning',
  bad: 'text-danger',
  unknown: 'text-muted-foreground',
  none: 'text-muted-foreground',
}

/**
 * 越靠前越该被看到。行首色带取这一行里**最该被看到**的那个判定 ——
 * 一行有 8 列，人先看色带决定要不要细读这一行。
 *
 * bad 在最前是显然的；unknown 排在 none 之前，是因为
 * 「这一列我们没读到」比「这一列确实没有」更需要人去处理。
 */
const ORDER: VerdictKind[] = ['bad', 'diff', 'unknown', 'none', 'ok']

/** 一行的结论：取该行所有格子里优先级最高的那个语气。 */
export function rowKind(verdicts: string[]): VerdictKind {
  const kinds = new Set(verdicts.map((v) => KIND[v] ?? 'none'))
  return ORDER.find((k) => kinds.has(k)) ?? 'ok'
}

/**
 * 归因的视觉表达。
 *
 * 🔴 配色的分工是刻意的，与判定色的分工同源：
 *   not_synced / sync_failed → 红：**是我们的锅**，对方想发都发不了
 *   synced                   → 灰：镜像到位了，差异的原因在对方，我们没什么要做的
 *   unknown                  → 虚线灰：我们不知道，去把复制规则绑上组织
 *
 * ⚠️ synced 刻意**不用绿**：绿会被读成「这一格没问题」，
 * 而它其实仍然是一个差异 —— 只是责任不在我们。判定色（黄/红）负责表达"有差异"，
 * 归因只回答"该找谁"，两套语义不能互相抢。
 */
export const SYNC_CHIP: Record<string, string> = {
  synced: 'bg-muted text-muted-foreground shadow-[inset_2px_0_0_var(--color-border-strong)]',
  sync_failed: 'bg-danger-bg text-danger shadow-[inset_2px_0_0_var(--color-danger)]',
  not_synced: 'bg-danger-bg text-danger shadow-[inset_2px_0_0_var(--color-danger)]',
  unknown: 'border border-dashed border-border-strong text-muted-foreground',
}
