import { useTranslation } from '@ops/i18n'
import { AsyncBoundary, EmptyState, Skeleton, fromQuery } from '@ops/ui'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { ShieldCheck } from 'lucide-react'
import { useMemo, useState } from 'react'
import { api } from '../../api/client.js'
import type { App, Decision, ListOf, Rule } from '../../api/types.js'
import { errorText, makeToLoadError } from '../../shared/errorText.js'
import { DecisionChain } from '../DecisionChain.js'
import { NewRuleForm } from '../NewRuleForm.js'
import { SubjectLabel } from '../SubjectLabel.js'

/**
 * 策略页。
 *
 * structure.md 要求这一页「不能只是表单，管理员需要看到这条规则实际会命中谁」——
 * 所以右侧常驻试算，左侧才是规则表。
 */
export function PoliciesPage() {
  const { t } = useTranslation()
  const toLoadError = useMemo(() => makeToLoadError(t), [t])
  const qc = useQueryClient()

  // 三层作用域各自管理，接口一次只返回一层 —— 这是后端有意的设计。
  // 前端**必须给切换器**，否则配在「单个应用」上的规则永远看不见，
  // 而那恰恰是最常用的一层。少了它，界面会让人以为规则丢了。
  const [scope, setScope] = useState<'global' | 'group' | 'app'>('global')
  const [scopeId, setScopeId] = useState(0)

  const apps = useQuery({ queryKey: ['apps'], queryFn: () => api.get<ListOf<App>>('/apps') })
  const appList = apps.data?.items ?? []
  // 切到「单个应用」时默认选第一个，否则 scope_id=0 查出来永远是空
  const effScopeId = scope === 'app' ? scopeId || appList[0]?.id || 0 : 0

  const rules = useQuery({
    queryKey: ['policies', scope, effScopeId],
    queryFn: () =>
      api.get<ListOf<Rule> & { scope: string }>(
        `/policies?scope=${scope}&scope_id=${effScopeId}`,
      ),
    enabled: scope !== 'app' || effScopeId > 0,
  })
  const state = fromQuery<ListOf<Rule> & { scope: string }>(
    rules,
    (d) => d.total === 0,
    toLoadError,
  )

  const del = useMutation({
    mutationFn: (id: number) => api.del(`/policies/${id}`),
    onSuccess: () => void qc.invalidateQueries({ queryKey: ['policies'] }),
  })

  return (
    <div className="grid gap-4 lg:grid-cols-[1fr_360px]">
      <div className="min-w-0">
        <h1 className="text-xl font-semibold tracking-tight">{t('sso:console.policies')}</h1>
        {/* 判定顺序写在页面上而不是文档里：改规则的人当场就要能看到 */}
        <p className="mt-1 mb-5 max-w-[80ch] text-sm text-muted-foreground">
          {t('sso:policy.order')}
        </p>

        <div className="mb-3 flex flex-wrap items-center gap-2">
          <div className="inline-flex rounded-[var(--radius)] border border-border p-0.5">
            {(['global', 'group', 'app'] as const).map((s) => (
              <button
                key={s}
                type="button"
                onClick={() => setScope(s)}
                aria-pressed={scope === s}
                className={[
                  'cursor-pointer rounded-[var(--radius-sm)] px-2.5 py-1 text-[12px]',
                  scope === s ? 'bg-brand-bg font-medium text-brand' : 'text-muted-foreground',
                ].join(' ')}
              >
                {t(`sso:policy.scope.${s}`)}
              </button>
            ))}
          </div>
          {scope === 'app' ? (
            <select
              value={effScopeId}
              onChange={(e) => setScopeId(Number(e.target.value))}
              className="rounded-[var(--radius)] border border-input bg-background px-2.5 py-1 text-[12px]"
            >
              {appList.map((a) => (
                <option key={a.id} value={a.id}>
                  {a.name}
                </option>
              ))}
            </select>
          ) : null}
          <span className="text-[11px] text-muted-foreground">
            {t(`sso:policy.scopeHint.${scope}`)}
          </span>
        </div>

        <div className="overflow-hidden rounded-[var(--radius-md)] border border-border bg-card">
          <AsyncBoundary
            state={state}
            errorTitle={t('sso:policy.errorTitle')}
            retryLabel={t('retry')}
            onRetry={() => void rules.refetch()}
            pending={
              <div className="space-y-2 p-4">
                {[0, 1, 2].map((i) => (
                  <Skeleton key={i} className="h-12 w-full" />
                ))}
              </div>
            }
            empty={
              <EmptyState
                icon={<ShieldCheck />}
                title={t('sso:policy.empty.title')}
                // 空 ≠ 无害：没有规则时默认拒绝，等于谁都进不去
                reason={t('sso:policy.empty.reason')}
                action={null}
              />
            }
          >
            {(data) => (
              <table className="w-full text-[13px]">
                <thead>
                  <tr className="bg-muted text-xs text-muted-foreground">
                    <th className="px-3.5 py-2.5 text-left font-medium">
                      {t('sso:policy.colSubject')}
                    </th>
                    <th className="px-3.5 py-2.5 text-left font-medium">
                      {t('sso:policy.colEffect')}
                    </th>
                    <th className="px-3.5 py-2.5 text-left font-medium">
                      {t('sso:policy.colWhy')}
                    </th>
                    <th />
                  </tr>
                </thead>
                <tbody>
                  {data.items.map((r) => (
                    <tr key={r.id} className="border-t border-border hover:bg-muted">
                      <td className="px-3.5 py-2.5">
                        <SubjectLabel rule={r} />
                        <span className="ml-2 text-[11px] text-muted-foreground">
                          {t(`sso:policy.scope.${r.scope}`)}
                        </span>
                      </td>
                      <td className="px-3.5 py-2.5">
                        <span
                          className={[
                            'rounded-[var(--radius-sm)] px-2 py-0.5 text-[11px] font-medium',
                            r.effect === 'allow'
                              ? 'bg-success-bg text-success'
                              : 'bg-danger-bg text-danger',
                          ].join(' ')}
                        >
                          {t(`sso:policy.effect.${r.effect}`)}
                        </span>
                        {r.enforced ? (
                          <span className="ml-1.5 rounded-[var(--radius-sm)] bg-warning-bg px-2 py-0.5 text-[11px] text-warning">
                            {t('sso:policy.enforced')}
                          </span>
                        ) : null}
                      </td>
                      <td className="px-3.5 py-2.5 text-[12px] text-muted-foreground">
                        {r.note || '—'}
                      </td>
                      <td className="px-3.5 py-2.5 text-right">
                        <button
                          type="button"
                          onClick={() => del.mutate(r.id)}
                          className="cursor-pointer rounded-[var(--radius)] border border-border px-2 py-1 text-[11px] hover:border-destructive hover:text-destructive"
                        >
                          {t('sso:policy.delete')}
                        </button>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            )}
          </AsyncBoundary>
        </div>

        <NewRuleForm scope={scope} scopeId={effScopeId} />

        <p className="mt-3 rounded-[var(--radius)] border border-border bg-muted p-3 text-xs leading-relaxed text-muted-foreground">
          {t('sso:policy.noEnforcedAllow')}
        </p>
      </div>

      <Simulator />
    </div>
  )
}

/** 试算：他现在到底能不能进。 */
function Simulator() {
  const { t } = useTranslation()
  const [userId, setUserId] = useState('1')
  const [appId, setAppId] = useState('')

  const apps = useQuery({ queryKey: ['apps'], queryFn: () => api.get<ListOf<App>>('/apps') })
  const sim = useMutation({
    mutationFn: () =>
      api.post<Decision>('/policies/simulate', {
        user_id: Number(userId),
        app_id: Number(appId),
      }),
  })

  const list = apps.data?.items ?? []
  const effectiveApp = appId || (list[0] ? String(list[0].id) : '')

  return (
    <aside className="rounded-[var(--radius-md)] border border-border bg-card p-4">
      <h2 className="text-sm font-semibold">{t('sso:policy.simulate')}</h2>
      <p className="mt-1 mb-4 text-xs text-muted-foreground">{t('sso:policy.simulateHint')}</p>

      <label className="mb-1.5 block text-xs font-medium text-muted-foreground" htmlFor="uid">
        {t('sso:policy.who')}
      </label>
      <input
        id="uid"
        value={userId}
        onChange={(e) => setUserId(e.target.value)}
        className="mb-3 w-full rounded-[var(--radius)] border border-input bg-background px-2.5 py-1.5 text-[13px] focus:border-primary focus:outline-none"
      />

      <label className="mb-1.5 block text-xs font-medium text-muted-foreground" htmlFor="aid">
        {t('sso:policy.whichApp')}
      </label>
      <select
        id="aid"
        value={effectiveApp}
        onChange={(e) => setAppId(e.target.value)}
        className="mb-4 w-full rounded-[var(--radius)] border border-input bg-background px-2.5 py-1.5 text-[13px] focus:border-primary focus:outline-none"
      >
        {list.map((a) => (
          <option key={a.id} value={a.id}>
            {a.name}
          </option>
        ))}
      </select>

      <button
        type="button"
        disabled={!effectiveApp || sim.isPending}
        onClick={() => {
          setAppId(effectiveApp)
          sim.mutate()
        }}
        className="mb-4 h-8 w-full cursor-pointer rounded-[var(--radius)] bg-primary text-[13px] font-medium text-primary-foreground hover:bg-primary-hover disabled:cursor-not-allowed disabled:opacity-50"
      >
        {sim.isPending ? t('sso:policy.simulating') : t('sso:policy.simulate')}
      </button>

      {sim.error ? (
        <p className="rounded-[var(--radius)] border border-destructive bg-danger-bg p-2.5 text-xs">
          {errorText(t, sim.error)}
        </p>
      ) : null}

      {sim.data ? <DecisionChain decision={sim.data} /> : null}
    </aside>
  )
}
