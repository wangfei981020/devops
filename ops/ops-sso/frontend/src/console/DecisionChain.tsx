import { useTranslation } from '@ops/i18n'
import { Check, ShieldOff, X } from 'lucide-react'
import type { Decision, Rule, TraceStep } from '../api/types.js'
import { SubjectLabel } from './SubjectLabel.js'

/**
 * 判定链路：把「他为什么进不去」摊开给人看。
 *
 * # 为什么这一屏值得单独做
 *
 * 「403 从哪来」是这个品类公认的排障黑洞 —— Keycloak 的 403 可能来自
 * 授权策略、角色映射、audience、CORS、反向代理任意一层，官方排障文档
 * 的方法就是挨层去猜。竞品把判定结果显示成一个 allow/deny 字段，
 * **我们显示它是怎么走到那个结果的**。
 *
 * # 为什么要把「被盖住的规则」也列出来
 *
 * 最常见的工单不是「他不该进却进了」，而是「我明明给他配了放行，
 * 他还是进不去」。那种情况下配的人以为已经生效，而规则被更高优先级
 * 的拒绝盖住了 —— 只显示最终结论的话，这件事永远查不出来。
 * 这类「配了没用」的规则也是策略系统里积压最快的垃圾。
 */
export function DecisionChain({ decision }: { decision: Decision }) {
  const { t } = useTranslation()
  const won = decision.trace.find((s) => s.outcome === 'won' || s.outcome === 'enforced')
  const shadowed = decision.trace.filter((s) => s !== won)

  return (
    <div>
      <div
        className={[
          'mb-4 flex items-center gap-3 rounded-[var(--radius-md)] border p-4',
          decision.allowed
            ? 'border-success bg-success-bg'
            : 'border-destructive bg-danger-bg',
        ].join(' ')}
      >
        <span
          className={[
            'grid size-9 shrink-0 place-items-center rounded-[var(--radius)]',
            decision.allowed ? 'text-success' : 'text-danger',
          ].join(' ')}
        >
          {decision.allowed ? <Check className="size-5" /> : <X className="size-5" />}
        </span>
        <div>
          <b className="block text-sm font-semibold">
            {decision.allowed ? t('sso:policy.resultAllow') : t('sso:policy.resultDeny')}
          </b>
          <span className="text-xs text-muted-foreground">
            {t(`sso:policy.reason.${decision.reason}`, { defaultValue: decision.reason })}
          </span>
        </div>
      </div>

      {decision.trace.length === 0 ? (
        // 一条规则都没命中 ≠ 数据没加载出来。这里必须说清是"没命中"，
        // 否则会被读成"接口挂了"，而两者的下一步动作完全不同。
        <p className="rounded-[var(--radius)] border border-border bg-muted p-3 text-xs text-muted-foreground">
          {t('sso:policy.noRuleMatched')}
        </p>
      ) : (
        <>
          <p className="mb-2 text-xs text-muted-foreground">
            {t('sso:policy.traceHint', { count: decision.trace.length })}
          </p>
          <div className="space-y-2">
            {won ? <RuleRow step={won} winner /> : null}
            {shadowed.map((s) => (
              <RuleRow key={s.rule.id} step={s} />
            ))}
          </div>

          {shadowed.length > 0 ? (
            <div className="mt-3 rounded-[var(--radius)] border border-warning bg-warning-bg p-3 text-xs leading-relaxed">
              {t('sso:policy.shadowedWarning', { count: shadowed.length })}
            </div>
          ) : null}
        </>
      )}
    </div>
  )
}

function RuleRow({ step, winner }: { step: TraceStep; winner?: boolean }) {
  const { t } = useTranslation()
  const r = step.rule
  return (
    <div
      className={[
        'flex items-start gap-3 rounded-[var(--radius)] border p-3',
        winner
          ? r.effect === 'allow'
            ? 'border-success bg-success-bg'
            : 'border-destructive bg-danger-bg'
          : // 被盖住的降透明度：它们是背景信息，不该和结论抢注意力
            'border-border bg-background opacity-70',
      ].join(' ')}
    >
      <span
        className={[
          'shrink-0 rounded-[var(--radius-sm)] px-2 py-0.5 text-[11px] font-medium',
          winner ? 'bg-card' : 'bg-muted text-muted-foreground',
        ].join(' ')}
      >
        {t(`sso:policy.outcome.${step.outcome}`)}
      </span>
      <div className="min-w-0 flex-1">
        <div className="text-[13px]">
          <SubjectLabel rule={r} />
          <span className="text-muted-foreground">
            {' · '}
            {t(`sso:policy.scope.${r.scope}`)}
            {' · '}
            {t(`sso:policy.effect.${r.effect}`)}
            {r.enforced ? ` · ${t('sso:policy.enforced')}` : ''}
          </span>
        </div>
        {r.note ? <div className="mt-1 text-[11px] text-muted-foreground">{r.note}</div> : null}
        {/* 把排序依据也露出来：光说"被盖了"没用，要能看出凭什么 */}
        <div className="mt-1 flex gap-3 text-[11px] text-muted-foreground">
          <span>{t('sso:policy.subjectRank', { n: step.subject_rank })}</span>
          <span>{t('sso:policy.scopeRank', { n: step.scope_rank })}</span>
        </div>
      </div>
      <span className="shrink-0 font-mono text-[11px] text-muted-foreground">#{r.id}</span>
    </div>
  )
}

export const DecisionEmptyIcon = ShieldOff
