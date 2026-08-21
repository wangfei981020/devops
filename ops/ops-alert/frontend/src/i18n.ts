import { createI18n, type Resources } from '@ops/i18n'

import zhCommon from '@ops/i18n/locales/zh-CN/common.json'
import zhNav from '@ops/i18n/locales/zh-CN/nav.json'
import zhAlert from '@ops/i18n/locales/zh-CN/opsalert.json'
import enCommon from '@ops/i18n/locales/en-US/common.json'
import enNav from '@ops/i18n/locales/en-US/nav.json'
import enAlert from '@ops/i18n/locales/en-US/opsalert.json'

/**
 * 本产品的文案在 `opsalert` 命名空间里，不往 CMDB 的命名空间里塞——
 * 两个产品共用一份 nav.json 的话，谁改一个 key 都会波及对方，
 * 而 check-i18n 只会告诉你"key 对不上"，不会告诉你是谁动的。
 *
 * common / nav 复用共享包：重试、关闭、主题、密度这些每个产品都一样，
 * 各写一份迟早出现"这个产品叫『重试』、那个叫『再试一次』"。
 */
export const i18n = createI18n({
  'zh-CN': { common: zhCommon, nav: zhNav, opsalert: zhAlert },
  'en-US': { common: enCommon, nav: enNav, opsalert: enAlert },
} as unknown as Resources)
