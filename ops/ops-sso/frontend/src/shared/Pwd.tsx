import { useTranslation } from '@ops/i18n'
import { PasswordInput } from '@ops/ui'

/**
 * 全站统一的密码输入框。
 *
 * # 为什么要在这里再包一层
 *
 * `@ops/ui` 的 PasswordInput 不认识 i18n（设计系统不该依赖语言包），
 * 所以「显示口令 / 隐藏口令」这两个词要由用它的人给。
 * 让每个页面各给一次，六个地方就会出现六种说法 ——
 * 而这两个词是给读屏用户念的，不一致等于同一个按钮换了个名字。
 *
 * # 约定
 *
 * **凡是 type="password" 的地方，一律用这个组件，不要直接写 input。**
 * 圆点回显把「密码错了」和「密码打错了」变成同一个现象，
 * 而这两件事的下一步完全不同。已经因为这个排查过一次登录失败，
 * 最后是大小写打错一个字母。
 */
export function Pwd(props: Omit<React.ComponentProps<typeof PasswordInput>, 'showLabel' | 'hideLabel'>) {
  const { t } = useTranslation()
  return <PasswordInput {...props} showLabel={t('sso:pwd.show')} hideLabel={t('sso:pwd.hide')} />
}
