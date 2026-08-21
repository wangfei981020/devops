import { LOCALES, type Locale, setLocale, useTranslation } from '@ops/i18n'
import { LocaleToggle, PreferencesMenu } from '@ops/ui'

/**
 * 顶栏的偏好控件：语言、主题、密度。
 *
 * 主题与密度用共享包的 PreferencesMenu，不在这里重新实现——
 * 重复实现迟早分叉：某天改了这里的密度档位而别的产品没改，
 * 两个产品的"紧凑"就不是一回事了，而这种偏差没人会专门去比对。
 *
 * 语言留在应用层，因为切换要拿到本应用的 i18n 实例。
 */
export function Preferences() {
  const { t, i18n } = useTranslation()
  const locale = i18n.language as Locale
  const next = LOCALES[(LOCALES.indexOf(locale) + 1) % LOCALES.length] ?? LOCALES[0]

  return (
    <>
      <LocaleToggle
        current={locale === 'zh-CN' ? '中' : 'EN'}
        label={t('locale.label')}
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
