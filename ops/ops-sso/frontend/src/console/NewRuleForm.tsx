import { useTranslation } from '@ops/i18n'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { Plus } from 'lucide-react'
import { type FormEvent, useState } from 'react'
import { api } from '../api/client.js'
import { errorText } from '../shared/errorText.js'

/**
 * 新增规则。
 *
 * # 为什么「为什么加这条」是必填
 *
 * 半年后复核时，那一栏是唯一能看的东西。没有它，规则表就是一堆
 * 谁也不敢删的历史遗留 —— 而不敢删的规则会一直堆到没人看得懂为止。
 */
export function NewRuleForm({
  scope,
  scopeId,
}: {
  scope: 'global' | 'group' | 'app'
  scopeId: number
}) {
  const { t } = useTranslation()
  const qc = useQueryClient()
  const [subjectType, setSubjectType] = useState<'public' | 'dept' | 'role' | 'group' | 'user'>(
    'user',
  )
  const [subjectId, setSubjectId] = useState('')
  const [effect, setEffect] = useState<'allow' | 'deny'>('allow')
  const [enforced, setEnforced] = useState(false)
  const [note, setNote] = useState('')

  const create = useMutation({
    mutationFn: () =>
      api.post('/policies', {
        scope,
        scope_id: scopeId,
        subject_type: subjectType,
        subject_id: subjectType === 'public' ? 0 : Number(subjectId),
        effect,
        enforced,
        note,
      }),
    onSuccess: () => {
      setSubjectId('')
      setNote('')
      void qc.invalidateQueries({ queryKey: ['policies'] })
    },
  })

  // public 主体没有 ID，其余必须给
  const needsId = subjectType !== 'public'
  const canSubmit = (!needsId || Number(subjectId) > 0) && note.trim().length > 0 && !create.isPending

  return (
    <form
      onSubmit={(e: FormEvent) => {
        e.preventDefault()
        create.mutate()
      }}
      className="mt-3 rounded-[var(--radius-md)] border border-border bg-card p-4"
    >
      <h2 className="mb-3 text-sm font-semibold">{t('sso:policy.newRule')}</h2>

      <div className="mb-3 grid gap-3 sm:grid-cols-[1fr_1fr_1fr]">
        <Field label={t('sso:policy.colSubject')}>
          <select
            value={subjectType}
            onChange={(e) => setSubjectType(e.target.value as typeof subjectType)}
            className={inputCls}
          >
            {(['user', 'group', 'role', 'dept', 'public'] as const).map((s) => (
              <option key={s} value={s}>
                {t(`sso:policy.subject.${s}`)}
              </option>
            ))}
          </select>
        </Field>

        <Field label={needsId ? t('sso:policy.subjectId') : ' '}>
          {needsId ? (
            <input
              value={subjectId}
              onChange={(e) => setSubjectId(e.target.value)}
              placeholder={t('sso:policy.subjectIdHint')}
              className={inputCls}
            />
          ) : (
            <span className="block py-1.5 text-[12px] text-muted-foreground">
              {t('sso:policy.publicNoId')}
            </span>
          )}
        </Field>

        <Field label={t('sso:policy.colEffect')}>
          <select
            value={effect}
            onChange={(e) => setEffect(e.target.value as 'allow' | 'deny')}
            className={inputCls}
          >
            <option value="allow">{t('sso:policy.effect.allow')}</option>
            <option value="deny">{t('sso:policy.effect.deny')}</option>
          </select>
        </Field>
      </div>

      {/* 强制只对拒绝有意义 —— 不做「强制放行」，那是关不掉的后门 */}
      {effect === 'deny' ? (
        <label className="mb-3 flex items-start gap-2 text-[13px]">
          <input
            type="checkbox"
            checked={enforced}
            onChange={(e) => setEnforced(e.target.checked)}
            className="mt-0.5"
          />
          <span>
            {t('sso:policy.enforced')}
            <span className="block text-[11px] text-muted-foreground">
              {t('sso:policy.enforcedHint')}
            </span>
          </span>
        </label>
      ) : null}

      <Field label={t('sso:policy.colWhy')}>
        <input
          value={note}
          onChange={(e) => setNote(e.target.value)}
          placeholder={t('sso:policy.noteHint')}
          className={inputCls}
        />
      </Field>

      {create.error ? (
        <p className="mt-3 rounded-[var(--radius)] border border-destructive bg-danger-bg p-2.5 text-xs">
          {errorText(t, create.error)}
        </p>
      ) : null}

      <div className="mt-4 flex items-center gap-3">
        <button
          type="submit"
          disabled={!canSubmit}
          className="inline-flex h-8 cursor-pointer items-center gap-1.5 rounded-[var(--radius)] bg-primary px-3 text-[13px] font-medium text-primary-foreground hover:bg-primary-hover disabled:cursor-not-allowed disabled:opacity-50"
        >
          <Plus className="size-3.5" />
          {create.isPending ? t('sso:policy.adding') : t('sso:policy.add')}
        </button>
        <span className="text-[11px] text-muted-foreground">{t('sso:policy.addHint')}</span>
      </div>
    </form>
  )
}

const inputCls =
  'w-full rounded-[var(--radius)] border border-input bg-background px-2.5 py-1.5 text-[13px] focus:border-primary focus:outline-none'

function Field({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <label className="block">
      <span className="mb-1.5 block text-xs font-medium text-muted-foreground">{label}</span>
      {children}
    </label>
  )
}
