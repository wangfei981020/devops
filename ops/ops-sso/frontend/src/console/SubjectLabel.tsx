import { useTranslation } from '@ops/i18n'
import type { Rule } from '../api/types.js'

/**
 * 主体显示名。
 *
 * 界面上**不显示裸 ID** —— 没人记得住 7 是哪个组，而复核规则时
 * 第一件事就是"这条管的是谁"。走查时这条被专门挑出来过。
 *
 * 解析不到时不是留空，而是明确标成「已删除？」：
 * 主体被删而规则还在是真实会发生的（删组时没清规则），
 * 那条规则仍然在参与判定 —— 留空会被当成界面坏了，
 * 显示裸 ID 又看不出这是个该清理的异常。
 */
export function SubjectLabel({ rule }: { rule: Rule }) {
  const { t } = useTranslation()

  if (rule.subject_type === 'public') {
    return <b className="font-medium">{t('sso:policy.subject.public')}</b>
  }

  const kind = t(`sso:policy.subject.${rule.subject_type}`)
  return (
    <span>
      <b className="font-medium">{rule.subject_name ?? `#${rule.subject_id}`}</b>
      <span className="ml-1.5 text-[11px] text-muted-foreground">{kind}</span>
      {rule.subject_missing ? (
        <span className="ml-1.5 rounded-[var(--radius-sm)] bg-warning-bg px-1.5 py-0.5 text-[11px] text-warning">
          {t('sso:policy.subjectMissing')}
        </span>
      ) : null}
    </span>
  )
}
