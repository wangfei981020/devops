import type { TFunction } from '@ops/i18n'
import type { LoadError } from '@ops/ui'
import { ApiError } from '../api/client.js'

/**
 * 把错误码翻成人话。
 *
 * 找不到对应文案时**显示错误码本身**，不是笼统一句"操作失败" ——
 * 码能被搜索、能贴进工单，而"操作失败"什么也定位不了。
 */
export function errorText(t: TFunction, e: unknown): string {
  if (e instanceof ApiError) {
    const key = `errors.${e.code}`
    const text = t(key, { ...e.params, defaultValue: '' })
    return text || e.code
  }
  return String(e)
}

/**
 * 把错误转成 @ops/ui 要的 LoadError。
 *
 * `cause` 必须是**已翻译**的文本 —— ui 包不含语言包，也不该含：
 * 组件库里一旦出现中文字面量，英文界面就会零星漏中文，
 * 而且只在错误路径上出现，正常测试根本走不到。
 */
export function makeToLoadError(t: TFunction) {
  return (e: unknown): LoadError => {
    const api = e instanceof ApiError ? e : null
    return {
      cause: errorText(t, e),
      detail: api ? `HTTP ${api.status}${api.requestId ? ` · ${api.requestId}` : ''}` : undefined,
      // 4xx 重试一万次也一样，不给重试按钮免得让人白等
      retryable: !api || api.status >= 500 || api.status === 0,
    }
  }
}
