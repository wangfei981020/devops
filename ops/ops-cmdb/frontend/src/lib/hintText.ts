/**
 * 后端给的**补充说明**（hint）：现在是什么状况、下一步去哪儿做什么。
 *
 * 三级回退，和 actionMessage 同一套规矩：
 *
 *   1. hint_key —— 迁移后的接口
 *   2. hint                   —— **还没迁**的接口，后端直接发的中文
 *   3. 空串                    —— 后端没给（调用方自己决定显不显示）
 *
 * ⚠️ 第 2 级在英文界面下仍是中文，是**已知的中间态**（OPSCMDB-054）。
 *	它会随迁移推进自然消失 —— 但在那之前不能丢掉：
 *	hint 里往往写着「去『管理 → 观测端点』添加一个类型为 n9e 的接入点」
 *	这种唯一的出路，丢了就只剩一句"没有数据"。
 *
 * 🔴 与 actionMessage 的区别：这里第 3 级是**空串**不是通用文案。
 *	hint 是补充说明，没有就不显示；硬凑一句"请稍后重试"只会占地方。
 */
export interface WithHint {
  hint_key?: string
  hint?: string
  // ⚠️ **故意没有** hint_params：后端目前一个带参数的 hint 都没有。
  //	声明一个后端从来不返回的字段，读到的就是 undefined，
  //	插值悄悄变成空而不报错（check-dead-fields 已经挡下过两次）。
  //	等真出现带参数的 hint，连同后端一起加。
}

export function hintText(
  t: (key: string, params?: Record<string, unknown>) => string,
  o: WithHint | undefined | null,
): string {
  if (o?.hint_key) return t(o.hint_key)
  return o?.hint ?? ''
}
