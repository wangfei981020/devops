import { type Resources, createI18n } from '@ops/i18n'

import enCommon from '@ops/i18n/locales/en-US/common.json'
import enNav from '@ops/i18n/locales/en-US/nav.json'
import enSso from '@ops/i18n/locales/en-US/sso.json'
import zhCommon from '@ops/i18n/locales/zh-CN/common.json'
import zhNav from '@ops/i18n/locales/zh-CN/nav.json'
import zhSso from '@ops/i18n/locales/zh-CN/sso.json'

/**
 * 门户与控制台共用同一份语言包。
 *
 * 两个入口各自打包，但命名空间不拆 —— 拆开之后同一个词
 * （"应用"、"策略"、"会话"）会在两边各翻一次，迟早不一致。
 */
const resources: Resources = {
  // nav 复用共享包：@ops/ui 的 AppShell 自己要取 nav:search.* 四个 key。
  // 不注册的话侧边栏搜索按钮上会直接写着 "search.open" —— 功能正常、
  // 控制台干净，只有那几个字母是错的。check-shell-i18n 现在会拦下。
  'zh-CN': { common: zhCommon, nav: zhNav, sso: zhSso },
  'en-US': { common: enCommon, nav: enNav, sso: enSso },
}

export const i18n = createI18n(resources)
