/**
 * 动作类接口（同步、续期、测连通…）成功后那句提示。
 *
 * 🔴 三级回退，顺序不能变：
 *
 *   1. msg_key + msg_params —— 迁移后的接口。后端只说 code，前端翻译
 *   2. msg                  —— **还没迁**的接口，后端直接发的中文句子
 *   3. 通用的"已保存"       —— 后端什么都没说
 *
 * 第 2 级是过渡期必需的：392 处响应文案不可能一次迁完，
 * 而在迁完之前，把已有的中文句子丢掉会让提示退化成一句"已保存"——
 * 那比中文更糟，因为原句里往往带着"约 1-3 分钟""完成后自动刷新"这类
 * 真正有用的信息（OPSCMDB-054）。
 *
 * ⚠️ 第 2 级在英文界面下仍然是中文。这是**已知的中间态**，
 *	语言切换按钮上的提示已经说明了这一点 —— 但它会随着迁移推进自然消失，
 *	所以不要为了"看起来干净"提前把它删掉。
 *
 * ⚠️ msg_params 是**后端真的开始发了才加上的**。
 *	上一版这里故意没有它 —— 声明一个后端从来不返回的字段，
 *	读到的就是 undefined，插值悄悄变成空而不报错（check-dead-fields 挡下过一次）。
 */
export interface ActionResult {
  msg_key?: string
  /** 插值参数，如 {count: 3}。⚠️ 加它的前提是后端真的发 —— 见下方注释 */
  msg_params?: Record<string, unknown>
  msg?: string
}

export function actionMessage(
  t: (key: string, params?: Record<string, unknown>) => string,
  resp: ActionResult | undefined | null,
  fallbackKey = 'common:write.saved',
): string {
  if (resp?.msg_key) return t(resp.msg_key, resp.msg_params)
  if (resp?.msg) return resp.msg
  return t(fallbackKey)
}
