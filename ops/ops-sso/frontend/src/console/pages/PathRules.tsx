import { useTranslation } from '@ops/i18n'
import { AsyncBoundary, EmptyState, Skeleton, fromQuery } from '@ops/ui'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Route } from 'lucide-react'
import { useMemo, useState } from 'react'
import { ApiError, api } from '../../api/client.js'
import type { App, ListOf } from '../../api/types.js'
import { makeToLoadError } from '../../shared/errorText.js'

interface PathRule {
  id: number
  app_id: number
  methods: string
  path_pattern: string
  subject_type: string
  subject_id: number
  decision: 'allow' | 'challenge' | 'deny'
  require_ticket: boolean
  device_state: string
  time_window: string
  source_kind: string
  mfa_ttl_sec: number
  note?: string
}

interface SimResult {
  /**
   * 判定停在哪一层。
   *
   * `app` = 他连这个应用都进不去，**接口级规则根本没轮到判定**。
   * 这两种"拒绝"的下一步完全不同：一个去改应用级策略，一个去改这里。
   * 不区分的话，人会在这一页反复调规则，而问题根本不在这一层。
   */
  stage: 'app' | 'path'
  decision: string
  reason: string
  decided_by?: PathRule
}

/**
 * 接口级策略。
 *
 * # 和「策略」那一页的区别
 *
 * 那一页管的是「进不进得去这个系统」，这一页管的是「进去之后能动哪些接口」。
 * 两者粒度差一个数量级：一个人能登进发布系统，不代表他能调删除接口。
 *
 * # 为什么它是网关的事
 *
 * 判定发生在网关，被代管的老系统自己**一行代码都不用改** ——
 * 这正是「零改造」能卖出去的地方：给一个连角色都没有的老系统，
 * 加上按接口的权限控制。
 *
 * ⚠️ 只对网关代管的应用生效。OIDC 直连的应用流量不经过网关，
 * 这里配了也不会被执行 —— 界面必须说出来，否则配的人会以为已经生效。
 */
export function PathRulesPage() {
  const { t } = useTranslation()
  const toLoadError = useMemo(() => makeToLoadError(t), [t])
  const qc = useQueryClient()
  const [appID, setAppID] = useState('')

  const apps = useQuery({ queryKey: ['apps'], queryFn: () => api.get<ListOf<App>>('/apps') })
  const appList = apps.data?.items ?? []
  const current = appList.find((a) => String(a.id) === appID)

  const q = useQuery({
    queryKey: ['path-rules', appID],
    queryFn: () => api.get<ListOf<PathRule>>(`/path-rules?app_id=${appID}`),
    enabled: appID !== '',
  })
  const state = fromQuery<ListOf<PathRule>>(q, (d) => d.items.length === 0, toLoadError)

  const allowAll = useMutation({
    mutationFn: () =>
      api.post('/path-rules', {
        app_id: Number(appID),
        methods: '',
        // ⚠️ 子树通配是 /**，不是 /* —— 后者会被当成字面量路径，永远匹配不到。
        path_pattern: '/**',
        subject_type: 'public',
        subject_id: 0,
        decision: 'allow',
        note: '兜底：应用级放行后，接口默认全放',
      }),
    onSuccess: () => void qc.invalidateQueries({ queryKey: ['path-rules'] }),
  })

  const del = useMutation({
    mutationFn: (id: number) => api.del(`/path-rules/${id}`),
    onSuccess: () => void qc.invalidateQueries({ queryKey: ['path-rules'] }),
  })

  return (
    <>
      <h1 className="text-xl font-semibold tracking-tight">{t('sso:console.pathRules')}</h1>
      <p className="mt-1 mb-4 max-w-[80ch] text-sm text-muted-foreground">{t('sso:path.intro')}</p>

      <label className="mb-4 block max-w-sm text-[12px]">
        <span className="mb-1 block text-muted-foreground">{t('sso:path.pickApp')}</span>
        <select
          value={appID}
          onChange={(e) => setAppID(e.target.value)}
          className="w-full rounded-[var(--radius)] border border-border bg-background px-2.5 py-1.5 text-[13px]"
        >
          <option value="">{t('sso:path.pickAppPlaceholder')}</option>
          {appList.map((a) => (
            <option key={a.id} value={a.id}>
              {a.name}（{a.env}）
            </option>
          ))}
        </select>
      </label>

      {appID === '' ? (
        <p className="rounded-[var(--radius-md)] border border-border bg-card p-6 text-center text-sm text-muted-foreground">
          {t('sso:path.pickFirst')}
        </p>
      ) : (
        <>
          {/* 不走网关的应用配了也不生效，这句必须在配置之前说 */}
          {current && current.connect_type !== 'gateway' && current.connect_type !== 'formfill' ? (
            <p className="mb-3 rounded-[var(--radius)] border border-warning bg-warning-bg px-3 py-2 text-[12px]">
              {t('sso:path.notGateway', { type: current.connect_type })}
            </p>
          ) : null}

          {/* ⚠️ 无规则 = 全拒。不说的话，管理员会看着「策略」页显示能进、
              而每个请求都 403，三个界面互相矛盾。这是实测踩到的。 */}
          {q.data && q.data.items.length === 0 ? (
            <div className="mb-3 rounded-[var(--radius)] border border-warning bg-warning-bg px-3 py-2 text-[12px]">
              <p>{t('sso:path.noRules')}</p>
              <button
                type="button"
                disabled={allowAll.isPending}
                onClick={() => allowAll.mutate()}
                className="mt-2 cursor-pointer rounded-[var(--radius)] border border-border bg-card px-2.5 py-1 text-[12px] hover:bg-secondary disabled:opacity-50"
              >
                {t('sso:path.allowAll')}
              </button>
            </div>
          ) : null}

          <div className="grid gap-4 lg:grid-cols-[minmax(0,1fr)_20rem]">
            <div className="grid gap-4">
              <div className="overflow-hidden rounded-[var(--radius-md)] border border-border bg-card">
                <AsyncBoundary
                  state={state}
                  errorTitle={t('sso:path.errorTitle')}
                  retryLabel={t('retry')}
                  onRetry={() => void q.refetch()}
                  pending={<Skeleton className="m-4 h-20" />}
                  empty={
                    <EmptyState
                      icon={<Route />}
                      title={t('sso:path.empty.title')}
                      reason={t('sso:path.empty.reason')}
                      action={null}
                    />
                  }
                >
                  {(d) => (
                    <div className="overflow-x-auto">
                      <table className="w-full min-w-[720px] text-[13px]">
                        <thead>
                          <tr className="bg-muted text-xs text-muted-foreground">
                            <th className="px-3.5 py-2.5 text-left font-medium">
                              {t('sso:path.colWhat')}
                            </th>
                            <th className="px-3.5 py-2.5 text-left font-medium">
                              {t('sso:path.colWho')}
                            </th>
                            <th className="px-3.5 py-2.5 text-left font-medium">
                              {t('sso:path.colDecision')}
                            </th>
                            <th className="px-3.5 py-2.5" />
                          </tr>
                        </thead>
                        <tbody>
                          {d.items.map((r) => (
                            <tr key={r.id} className="border-t border-border hover:bg-muted">
                              <td className="px-3.5 py-2.5">
                                <span className="font-mono text-[12px]">
                                  <b className="font-semibold">{r.methods || 'ANY'}</b>{' '}
                                  {r.path_pattern}
                                </span>
                              </td>
                              <td className="px-3.5 py-2.5 text-muted-foreground">
                                {t(`sso:policy.subject.${r.subject_type}`, {
                                  defaultValue: r.subject_type,
                                })}
                                {r.subject_id ? ` #${r.subject_id}` : ''}
                              </td>
                              <td className="px-3.5 py-2.5">
                                <Decision r={r} />
                              </td>
                              <td className="px-3.5 py-2.5 text-right">
                                <button
                                  type="button"
                                  onClick={() => del.mutate(r.id)}
                                  className="cursor-pointer rounded-[var(--radius)] border border-border px-2 py-1 text-[11px] hover:bg-secondary"
                                >
                                  {t('sso:path.delete')}
                                </button>
                              </td>
                            </tr>
                          ))}
                        </tbody>
                      </table>
                    </div>
                  )}
                </AsyncBoundary>
              </div>

              <NewPathRule appID={Number(appID)} />
            </div>

            <Simulate appID={Number(appID)} />
          </div>
        </>
      )}
    </>
  )
}

function Decision({ r }: { r: PathRule }) {
  const { t } = useTranslation()
  const tone = {
    allow: 'bg-success-bg text-success',
    challenge: 'bg-warning-bg text-warning',
    deny: 'bg-danger-bg text-danger',
  }[r.decision]
  return (
    <span className="flex flex-wrap items-center gap-1.5">
      <span className={`rounded-[var(--radius-sm)] px-2 py-0.5 text-[11px] ${tone}`}>
        {t(`sso:path.decision.${r.decision}`)}
      </span>
      {/* 附加条件要露出来：一条"放行"如果带着必须有工单，行为和纯放行完全不同 */}
      {r.require_ticket ? (
        <span className="rounded-[var(--radius-sm)] bg-muted px-1.5 py-0.5 text-[10px] text-muted-foreground">
          {t('sso:path.needTicket')}
        </span>
      ) : null}
      {r.device_state ? (
        <span className="rounded-[var(--radius-sm)] bg-muted px-1.5 py-0.5 text-[10px] text-muted-foreground">
          {r.device_state}
        </span>
      ) : null}
      {r.time_window ? (
        <span className="rounded-[var(--radius-sm)] bg-muted px-1.5 py-0.5 text-[10px] text-muted-foreground">
          {r.time_window}
        </span>
      ) : null}
    </span>
  )
}

function NewPathRule({ appID }: { appID: number }) {
  const { t } = useTranslation()
  const qc = useQueryClient()
  const [methods, setMethods] = useState('')
  const [pattern, setPattern] = useState('')
  const [subjectType, setSubjectType] = useState('public')
  const [subjectID, setSubjectID] = useState('')
  const [decision, setDecision] = useState('deny')
  const [requireTicket, setTicket] = useState(false)
  const [note, setNote] = useState('')
  const [err, setErr] = useState<string | null>(null)

  const m = useMutation({
    mutationFn: () =>
      api.post('/path-rules', {
        app_id: appID,
        methods: methods.trim().toUpperCase(),
        path_pattern: pattern.trim(),
        subject_type: subjectType,
        subject_id: Number(subjectID) || 0,
        decision,
        require_ticket: requireTicket,
        note: note.trim(),
      }),
    onSuccess: () => {
      setErr(null)
      setPattern('')
      setNote('')
      void qc.invalidateQueries({ queryKey: ['path-rules'] })
    },
    onError: (e) =>
      setErr(
        e instanceof ApiError
          ? t(`errors.${e.code}`, { ...e.params, defaultValue: e.code })
          : t('error.unreachable'),
      ),
  })

  return (
    <div className="rounded-[var(--radius-md)] border border-border bg-card p-4">
      <h2 className="text-sm font-semibold">{t('sso:path.newTitle')}</h2>
      <div className="mt-3 grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
        <Labeled label={t('sso:path.fMethods')} hint={t('sso:path.fMethodsHint')}>
          <input
            value={methods}
            onChange={(e) => setMethods(e.target.value)}
            placeholder="DELETE,PUT"
            className={inputCls}
          />
        </Labeled>
        <Labeled label={t('sso:path.fPattern')} hint={t('sso:path.fPatternHint')}>
          <input
            value={pattern}
            onChange={(e) => setPattern(e.target.value)}
            placeholder="/api/v1/hosts/**"
            className={inputCls}
          />
        </Labeled>
        <Labeled label={t('sso:path.fDecision')}>
          <select
            value={decision}
            onChange={(e) => setDecision(e.target.value)}
            className={inputCls}
          >
            <option value="allow">{t('sso:path.decision.allow')}</option>
            <option value="challenge">{t('sso:path.decision.challenge')}</option>
            <option value="deny">{t('sso:path.decision.deny')}</option>
          </select>
        </Labeled>
        <Labeled label={t('sso:path.fSubject')}>
          <select
            value={subjectType}
            onChange={(e) => setSubjectType(e.target.value)}
            className={inputCls}
          >
            {['public', 'user', 'group', 'role', 'dept'].map((s) => (
              <option key={s} value={s}>
                {t(`sso:policy.subject.${s}`, { defaultValue: s })}
              </option>
            ))}
          </select>
        </Labeled>
        {subjectType !== 'public' ? (
          <Labeled label={t('sso:path.fSubjectID')}>
            <input
              value={subjectID}
              onChange={(e) => setSubjectID(e.target.value.replace(/\D/g, ''))}
              className={inputCls}
            />
          </Labeled>
        ) : null}
        <Labeled label={t('sso:path.fNote')} hint={t('sso:path.fNoteHint')}>
          <input value={note} onChange={(e) => setNote(e.target.value)} className={inputCls} />
        </Labeled>
      </div>

      <label className="mt-3 flex cursor-pointer items-center gap-2 text-[12px]">
        <input
          type="checkbox"
          checked={requireTicket}
          onChange={(e) => setTicket(e.target.checked)}
        />
        {t('sso:path.fRequireTicket')}
      </label>

      {err ? (
        <p className="mt-3 rounded-[var(--radius)] bg-danger-bg px-3 py-2 text-[12px] text-danger">
          {err}
        </p>
      ) : null}

      <button
        type="button"
        disabled={pattern.trim() === '' || m.isPending}
        onClick={() => m.mutate()}
        className="mt-3 cursor-pointer rounded-[var(--radius)] bg-brand px-3 py-1.5 text-[13px] font-medium text-brand-foreground disabled:cursor-default disabled:opacity-50"
      >
        {m.isPending ? t('sso:path.adding') : t('sso:path.add')}
      </button>
    </div>
  )
}

/**
 * 试算：这个人调这个接口，网关会怎么判。
 *
 * 接口级策略比应用级更容易配错 —— 通配符、方法列表、顺序都可能出错，
 * 而配错的后果是**线上某个接口突然全员 403**。上线前能先跑一遍，
 * 比出事后再回滚强得多。
 */
function Simulate({ appID }: { appID: number }) {
  const { t } = useTranslation()
  const [userID, setUserID] = useState('1')
  const [method, setMethod] = useState('GET')
  const [path, setPath] = useState('/api/v1/hosts')

  const m = useMutation({
    mutationFn: () =>
      api.post<SimResult>('/path-rules/simulate', {
        user_id: Number(userID),
        app_id: appID,
        method,
        path,
      }),
  })

  return (
    <aside className="rounded-[var(--radius-md)] border border-border bg-card p-4">
      <h2 className="text-sm font-semibold">{t('sso:path.simTitle')}</h2>
      <p className="mt-1 mb-3 text-[12px] text-muted-foreground">{t('sso:path.simDesc')}</p>

      <div className="grid gap-2.5">
        <Labeled label={t('sso:path.simUser')}>
          <input
            value={userID}
            onChange={(e) => setUserID(e.target.value.replace(/\D/g, ''))}
            className={inputCls}
          />
        </Labeled>
        <Labeled label={t('sso:path.simMethod')}>
          <select value={method} onChange={(e) => setMethod(e.target.value)} className={inputCls}>
            {['GET', 'POST', 'PUT', 'DELETE', 'PATCH'].map((x) => (
              <option key={x}>{x}</option>
            ))}
          </select>
        </Labeled>
        <Labeled label={t('sso:path.simPath')}>
          <input value={path} onChange={(e) => setPath(e.target.value)} className={inputCls} />
        </Labeled>
      </div>

      <button
        type="button"
        disabled={m.isPending}
        onClick={() => m.mutate()}
        className="mt-3 w-full cursor-pointer rounded-[var(--radius)] bg-brand px-3 py-1.5 text-[13px] font-medium text-brand-foreground disabled:opacity-50"
      >
        {m.isPending ? t('sso:path.simRunning') : t('sso:path.simRun')}
      </button>

      {m.isError ? (
        <p className="mt-3 rounded-[var(--radius)] bg-danger-bg px-2.5 py-1.5 text-[12px] text-danger">
          {t('sso:path.simFailed')}
        </p>
      ) : null}

      {m.data ? (
        <div
          className={[
            'mt-3 rounded-[var(--radius)] border p-3 text-[13px]',
            m.data.decision === 'allow'
              ? 'border-success bg-success-bg'
              : m.data.decision === 'challenge'
                ? 'border-warning bg-warning-bg'
                : 'border-destructive bg-danger-bg',
          ].join(' ')}
        >
          <b className="block font-semibold">
            {t(`sso:path.decision.${m.data.decision}`, { defaultValue: m.data.decision })}
          </b>
          {m.data.stage === 'app' ? (
            <span className="mt-1 block text-[12px]">{t('sso:path.stoppedAtApp')}</span>
          ) : null}
          <span className="text-[12px] text-muted-foreground">
            {t(`sso:path.reason.${m.data.reason}`, { defaultValue: m.data.reason })}
            {/* 命中哪条要说出来：不说的话，改错了也不知道该改哪一条 */}
            {m.data.decided_by ? ` · #${m.data.decided_by.id}` : ''}
          </span>
        </div>
      ) : null}
    </aside>
  )
}

const inputCls =
  'w-full rounded-[var(--radius)] border border-border bg-background px-2.5 py-1.5 text-[13px]'

function Labeled({
  label,
  hint,
  children,
}: {
  label: string
  hint?: string
  children: React.ReactNode
}) {
  return (
    <label className="block text-[12px]">
      <span className="mb-1 block text-muted-foreground">{label}</span>
      {children}
      {hint ? <span className="mt-1 block text-[11px] text-muted-foreground">{hint}</span> : null}
    </label>
  )
}
