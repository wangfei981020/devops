import { LOCALES, type Locale, setLocale, useTranslation } from '@ops/i18n'
import { LocaleToggle, PreferencesMenu } from '@ops/ui'

/**
 * 顶栏的偏好控件。
 *
 * 主题与密度用共享包的 `PreferencesMenu`，**不在这里重新实现** ——
 * 重复实现迟早分叉：某天改了这里的密度档位而别的产品没改，
 * 两个产品的"紧凑"就不是一回事了，而这种偏差没人会专门去比对。
 *
 * 语言留在应用层，因为切换要拿到本应用的 i18n 实例。
 * 共享包不该依赖某个具体的 i18n 实例，那会把所有消费方绑死在一套初始化方式上。
 */
export function Preferences() {
  const { t, i18n } = useTranslation()
  const locale = i18n.language as Locale

  // 只有两种语言时，一键切换比下拉快得多 —— 下拉要点两次（展开 + 选）。
  // 等语言多到三种以上再换成下拉。
  const next = LOCALES[(LOCALES.indexOf(locale) + 1) % LOCALES.length] ?? LOCALES[0]

  return (
    <>
      <LocaleToggle
        current={locale === 'zh-CN' ? '中' : 'EN'}
        label={t('locale.label')}
        // 英文态下把已知局限标出来：后端产出的诊断说明目前只有中文（OPSCMDB-054）。
        // ⚠️ 中文态没有这个问题，所以不标 —— 一个恒亮的提示等于没有提示。
        hint={locale === 'en-US' ? t('locale.partialHint') : undefined}
        onToggle={() => void setLocale(i18n, next)}
      />
      <PreferencesMenu
        labels={{
          preferences: t('preferences'),
          theme: t('theme.label'),
          themeSystem: t('theme.system'),
          themeLight: t('theme.light'),
          themeDark: t('theme.dark'),
          density: t('density.label'),
          densityCompact: t('density.compact'),
          densityDefault: t('density.default'),
          densityComfortable: t('density.comfortable'),
        }}
      />
    </>
  )
}
