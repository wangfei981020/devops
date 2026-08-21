import { useTranslation } from '@ops/i18n'
import { AsyncBoundary, EmptyState, Skeleton, fromQuery } from '@ops/ui'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { FileText, ShieldCheck, ShieldAlert } from 'lucide-react'
import { useMemo, useState } from 'react'
import { api } from '../../api/client.js'
import { makeToLoadError } from '../../shared/errorText.js'

interface AccessEvent {
  id: number
  request_id: string
  occurred_at: string
  user_id: number
  user_label: string
  app_code: string
  method: string
  path: string
  decision: string
  reason: string
  matched_rule: number
  client_ip: string
  gateway: string
}
interface AuditLog {
  id: number
  actor_id: number
  actor_name: string
  action: string
  object_type: string
  object_id: number
  detail: string
  client_ip: string
  created_at: string
}
/**
 * 后端这两个列表返回的是 {items, limit}，**没有 total** —— 别复用 ListOf<T>，
 * 那个类型带 total，会让人以为能算总页数，而运行期它是 undefined。
 */
interface Paged<T> {
  items: T[]
  limit: number
}

interface ChainResult {
  ok: boolean
  checked: number
  broken_at_seq?: number
  reason?: string
}

type Tab = 'access' | 'changes'

/**
 * 访问审计。
 *
 * 分两张表，因为它们回答的是**两个不同的问题**，混在一起两个都答不好：
 *   - 访问事件：谁在什么时候进了哪个系统（网关每次放行/拦截都记一条，量大）
 *   - 配置变更：谁改了策略、应用、探针（量小，但每条都要能追责）
 *
 * 出事时先看配置变更（"是谁把那条拒绝删了"），再看访问事件
 * （"删掉之后谁进来了"）—— 顺序是反的，所以变更放在能一眼看到的位置。
 */
export function AuditPage() {
  const { t } = useTranslation()
  const [tab, setTab] = useState<Tab>('access')

  return (
    <>
      <h1 className="text-xl font-semibold tracking-tight">{t('sso:console.audit')}</h1>
      <p className="mt-1 mb-4 max-w-[80ch] text-sm text-muted-foreground">{t('sso:audit.intro')}</p>

      <ChainStatus />
      <Anchors />

      <div className="mt-4 mb-3 inline-flex rounded-[var(--radius)] border border-border p-0.5">
        {(['access', 'changes'] as Tab[]).map((k) => (
          <button
            key={k}
            type="button"
            onClick={() => setTab(k)}
            aria-pressed={tab === k}
            className={[
              'cursor-pointer rounded-[calc(var(--radius)-2px)] px-3 py-1.5 text-[13px]',
              tab === k ? 'bg-brand-bg font-medium text-brand' : 'text-muted-foreground',
            ].join(' ')}
          >
            {t(`sso:audit.tab.${k}`)}
          </button>
        ))}
      </div>

      {tab === 'access' ? <AccessEvents /> : <ChangeLog />}
    </>
  )
}

/**
 * 审计链自校验。
 *
 * 审计日志最大的软肋是「改了没人知道」—— 有权限进库的人可以直接 UPDATE。
 * 每条日志带前一条的哈希，改任何一条，从它往后全对不上。
 * ⚠️ 这**不能**防止有人整段删除后重算全链（那需要外部锚定，见 audit_anchors），
 * 所以文案不能吹成"不可篡改"，只能说"改过能看出来"。
 */
function ChainStatus() {
  const { t } = useTranslation()
  const m = useMutation({ mutationFn: () => api.get<ChainResult>('/audit-logs/verify') })
  const r = m.data

  return (
    <div className="flex flex-wrap items-center gap-3 rounded-[var(--radius-md)] border border-border bg-card px-4 py-3">
      <span className="text-[13px]">{t('sso:audit.chain.title')}</span>
      <button
        type="button"
        onClick={() => m.mutate()}
        disabled={m.isPending}
        className="cursor-pointer rounded-[var(--radius)] border border-border px-2.5 py-1 text-[12px] hover:bg-secondary disabled:cursor-default disabled:opacity-60"
      >
        {m.isPending ? t('sso:audit.chain.checking') : t('sso:audit.chain.check')}
      </button>

      {m.isError ? (
        // 校验**跑不起来** ≠ 校验不通过。前者是接口问题，后者是数据问题，
        // 两者的下一步动作完全不同，绝不能显示成同一个红色结论。
        <span className="rounded-[var(--radius-sm)] bg-warning-bg px-2 py-1 text-[12px] text-warning">
          {t('sso:audit.chain.unavailable')}
        </span>
      ) : null}

      {r ? (
        r.ok ? (
          <span className="flex items-center gap-1.5 rounded-[var(--radius-sm)] bg-success-bg px-2 py-1 text-[12px] text-success">
            <ShieldCheck className="size-3.5" />
            {t('sso:audit.chain.ok', { count: r.checked })}
          </span>
        ) : (
          <span className="flex items-center gap-1.5 rounded-[var(--radius-sm)] bg-danger-bg px-2 py-1 text-[12px] text-danger">
            <ShieldAlert className="size-3.5" />
            {t('sso:audit.chain.broken', { seq: r.broken_at_seq ?? '?' })}
            {r.reason ? (
              <span className="opacity-80">
                （{t(`sso:audit.chain.reason.${r.reason}`, { defaultValue: r.reason })}）
              </span>
            ) : null}
          </span>
        )
      ) : null}

      <span className="w-full text-[11px] leading-relaxed text-muted-foreground">
        {t('sso:audit.chain.hint')}
      </span>
    </div>
  )
}

interface Anchor {
  seq: number
  hash: string
  created_at: string
  signature_ok: boolean
  matches_chain?: boolean
  entry_missing: boolean
  exported_to: string
}
interface AnchorList {
  items: Anchor[]
  signing_enabled: boolean
}

/**
 * 锚点。
 *
 * # 它补的是哈希链补不了的那个洞
 *
 * 链能发现"改一条、删一条、插一条"，防不住**从某点起整条重算** ——
 * 有库权限的人把 audit_logs 全部重写，链自洽，校验照样通过。
 * 锚点把某一时刻的链头签下来，之后再重算就会和已签的对不上。
 *
 * # ⚠️ 锚点留在库里等于没锚
 *
 * 能重写链的人同样能删改锚点表。所以「已签发」不等于「已受保护」，
 * 界面必须把这两件事分开说 —— 合成一个绿色标记，
 * 给的是"已经有防护了"的错觉，而它一点防护都没有。
 */
function Anchors() {
  const { t } = useTranslation()
  const qc = useQueryClient()
  const q = useQuery({
    queryKey: ['audit-anchors'],
    queryFn: () => api.get<AnchorList>('/audit-anchors'),
  })
  const sign = useMutation({
    mutationFn: () => api.post<{ created: number }>('/audit-anchors', {}),
    onSuccess: () => void qc.invalidateQueries({ queryKey: ['audit-anchors'] }),
  })

  const items = q.data?.items ?? []
  const broken = items.filter((a) => a.signature_ok && a.matches_chain === false)
  const exported = items.filter((a) => a.exported_to !== '').length

  const download = () => {
    void api.get<unknown>('/audit-anchors/export').then((data) => {
      const blob = new Blob([JSON.stringify(data, null, 2)], { type: 'application/json' })
      const url = URL.createObjectURL(blob)
      const a = document.createElement('a')
      a.href = url
      a.download = 'audit-anchors.json'
      a.click()
      URL.revokeObjectURL(url)
    })
  }

  return (
    <section className="mt-4 rounded-[var(--radius-md)] border border-border bg-card p-4">
      <div className="flex flex-wrap items-center gap-2">
        <span className="text-[13px] font-medium">{t('sso:anchor.title')}</span>
        <span className="flex-1" />
        <button
          type="button"
          disabled={!q.data?.signing_enabled || sign.isPending}
          onClick={() => sign.mutate()}
          className="cursor-pointer rounded-[var(--radius)] border border-border px-2.5 py-1 text-[12px] hover:bg-secondary disabled:opacity-40"
        >
          {sign.isPending ? t('sso:anchor.signing') : t('sso:anchor.signNow')}
        </button>
        <button
          type="button"
          disabled={items.length === 0}
          onClick={download}
          className="cursor-pointer rounded-[var(--radius)] border border-border px-2.5 py-1 text-[12px] hover:bg-secondary disabled:opacity-40"
        >
          {t('sso:anchor.export')}
        </button>
      </div>

      {q.data && !q.data.signing_enabled ? (
        // 没配私钥就是**没在锚定**，不是"还没到时间"。空列表不能自己解释自己。
        <p className="mt-2 rounded-[var(--radius)] border border-warning bg-warning-bg px-3 py-2 text-[12px]">
          {t('sso:anchor.noKey')}
        </p>
      ) : null}

      {broken.length > 0 ? (
        <p className="mt-2 rounded-[var(--radius)] border border-destructive bg-danger-bg px-3 py-2 text-[12px] text-danger">
          {t('sso:anchor.mismatch', { seq: broken.map((a) => a.seq).join(', ') })}
        </p>
      ) : null}

      {sign.data && sign.data.created === 0 ? (
        // 「没签」和「签了」要分开说：链头没动过就不重复签，
        // 显示成"已签发"会让人以为刚才那一刻被钉住了，而其实没有。
        <p className="mt-2 rounded-[var(--radius)] bg-muted px-3 py-2 text-[12px] text-muted-foreground">
          {t('sso:anchor.nothingNew')}
        </p>
      ) : null}

      <p className="mt-2 text-[11px] leading-relaxed text-muted-foreground">
        {t('sso:anchor.exportWhy', { total: items.length, exported })}
      </p>

      {items.length > 0 ? (
        <div className="mt-2 overflow-x-auto">
          <table className="w-full min-w-[560px] text-[12px]">
            <thead>
              <tr className="text-xs text-muted-foreground">
                <th className="py-1.5 text-left font-medium">{t('sso:anchor.colSeq')}</th>
                <th className="py-1.5 text-left font-medium">{t('sso:anchor.colAt')}</th>
                <th className="py-1.5 text-left font-medium">{t('sso:anchor.colSig')}</th>
                <th className="py-1.5 text-left font-medium">{t('sso:anchor.colMatch')}</th>
              </tr>
            </thead>
            <tbody>
              {items.slice(0, 8).map((a) => (
                <tr key={a.seq} className="border-t border-border">
                  <td className="py-1.5 font-mono">#{a.seq}</td>
                  <td className="py-1.5 text-muted-foreground">{fmt(a.created_at)}</td>
                  <td className="py-1.5">
                    {a.signature_ok ? (
                      <span className="text-success">{t('sso:anchor.sigOK')}</span>
                    ) : (
                      <span className="text-danger">{t('sso:anchor.sigBad')}</span>
                    )}
                  </td>
                  <td className="py-1.5">
                    {a.entry_missing ? (
                      // 锚点对应的那条记录没了，本身就是信号，不是"查不到"
                      <span className="text-danger">{t('sso:anchor.entryGone')}</span>
                    ) : a.matches_chain ? (
                      <span className="text-success">{t('sso:anchor.match')}</span>
                    ) : (
                      <span className="text-danger">{t('sso:anchor.mismatchOne')}</span>
                    )}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      ) : null}
    </section>
  )
}

const PAGE = 50

function AccessEvents() {
  const { t } = useTranslation()
  const toLoadError = useMemo(() => makeToLoadError(t), [t])
  const [decision, setDecision] = useState('')
  const [page, setPage] = useState(0)

  const q = useQuery({
    queryKey: ['access-events', decision, page],
    queryFn: () =>
      api.get<Paged<AccessEvent>>(
        `/access-events?limit=${PAGE}&offset=${page * PAGE}${decision ? `&decision=${decision}` : ''}`,
      ),
  })
  const state = fromQuery<Paged<AccessEvent>>(q, (d) => d.items.length === 0, toLoadError)

  return (
    <>
      <div className="mb-3 flex flex-wrap items-center gap-2">
        {['', 'allow', 'deny'].map((v) => (
          <button
            key={v || 'all'}
            type="button"
            onClick={() => {
              setDecision(v)
              setPage(0) // 换筛选必须回第 1 页，否则会停在一个空页上，看起来像"没数据"
            }}
            aria-pressed={decision === v}
            className={[
              'cursor-pointer rounded-[var(--radius)] border px-2.5 py-1 text-[12px]',
              decision === v
                ? 'border-brand bg-brand-bg text-brand'
                : 'border-border text-muted-foreground hover:bg-secondary',
            ].join(' ')}
          >
            {t(`sso:audit.filter.${v || 'all'}`)}
          </button>
        ))}
      </div>

      <div className="overflow-hidden rounded-[var(--radius-md)] border border-border bg-card">
        <AsyncBoundary
          state={state}
          errorTitle={t('sso:audit.errorTitle')}
          retryLabel={t('retry')}
          onRetry={() => void q.refetch()}
          pending={
            <div className="space-y-2 p-4">
              {[0, 1, 2, 3].map((i) => (
                <Skeleton key={i} className="h-9 w-full" />
              ))}
            </div>
          }
          empty={
            <EmptyState
              icon={<FileText />}
              title={t('sso:audit.emptyEvents.title')}
              reason={t(
                page > 0 ? 'sso:audit.emptyEvents.pastEnd' : 'sso:audit.emptyEvents.reason',
              )}
              action={null}
            />
          }
        >
          {(data) => (
            <div className="overflow-x-auto">
              <table className="w-full min-w-[880px] text-[13px]">
                <thead>
                  <tr className="bg-muted text-xs text-muted-foreground">
                    <th className="px-3.5 py-2.5 text-left font-medium">{t('sso:audit.colTime')}</th>
                    <th className="px-3.5 py-2.5 text-left font-medium">{t('sso:audit.colWho')}</th>
                    <th className="px-3.5 py-2.5 text-left font-medium">{t('sso:audit.colWhat')}</th>
                    <th className="px-3.5 py-2.5 text-left font-medium">
                      {t('sso:audit.colDecision')}
                    </th>
                    <th className="px-3.5 py-2.5 text-left font-medium">{t('sso:audit.colFrom')}</th>
                  </tr>
                </thead>
                <tbody>
                  {data.items.map((e) => (
                    <tr key={e.id} className="border-t border-border hover:bg-muted">
                      <td className="whitespace-nowrap px-3.5 py-2.5 text-muted-foreground">
                        {fmt(e.occurred_at)}
                      </td>
                      <td className="px-3.5 py-2.5">{e.user_label || `#${e.user_id}`}</td>
                      <td className="px-3.5 py-2.5">
                        <b className="font-medium">{e.app_code}</b>
                        <span className="ml-2 font-mono text-[11px] text-muted-foreground">
                          {e.method} {e.path}
                        </span>
                      </td>
                      <td className="whitespace-nowrap px-3.5 py-2.5">
                        <span
                          className={[
                            'rounded-[var(--radius-sm)] px-2 py-0.5 text-[11px]',
                            e.decision === 'allow'
                              ? 'bg-success-bg text-success'
                              : 'bg-danger-bg text-danger',
                          ].join(' ')}
                        >
                          {t(`sso:audit.decision.${e.decision}`, { defaultValue: e.decision })}
                        </span>
                        {/* 命中哪条规则要露出来：审计的价值在于能回到那条规则上 */}
                        {e.matched_rule ? (
                          <span className="ml-1.5 font-mono text-[11px] text-muted-foreground">
                            #{e.matched_rule}
                          </span>
                        ) : null}
                      </td>
                      <td className="whitespace-nowrap px-3.5 py-2.5 font-mono text-[11px] text-muted-foreground">
                        {e.client_ip}
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
        </AsyncBoundary>
      </div>

      {/* 后端只给了 limit/offset，没有总数 —— 所以这里只能做上下页，
          不能显示「第 3/17 页」。宁可不显示，也不能编一个总数出来。 */}
      <div className="mt-3 flex items-center gap-2 text-[12px]">
        <button
          type="button"
          disabled={page === 0}
          onClick={() => setPage((p) => Math.max(0, p - 1))}
          className="cursor-pointer rounded-[var(--radius)] border border-border px-2.5 py-1 hover:bg-secondary disabled:cursor-default disabled:opacity-40"
        >
          {t('sso:audit.prev')}
        </button>
        <span className="text-muted-foreground">{t('sso:audit.pageNo', { n: page + 1 })}</span>
        <button
          type="button"
          disabled={(q.data?.items.length ?? 0) < PAGE}
          onClick={() => setPage((p) => p + 1)}
          className="cursor-pointer rounded-[var(--radius)] border border-border px-2.5 py-1 hover:bg-secondary disabled:cursor-default disabled:opacity-40"
        >
          {t('sso:audit.next')}
        </button>
      </div>
    </>
  )
}

function ChangeLog() {
  const { t } = useTranslation()
  const toLoadError = useMemo(() => makeToLoadError(t), [t])
  const q = useQuery({
    queryKey: ['audit-logs'],
    queryFn: () => api.get<Paged<AuditLog>>('/audit-logs?limit=100'),
  })
  const state = fromQuery<Paged<AuditLog>>(q, (d) => d.items.length === 0, toLoadError)

  return (
    <div className="overflow-hidden rounded-[var(--radius-md)] border border-border bg-card">
      <AsyncBoundary
        state={state}
        errorTitle={t('sso:audit.errorTitle')}
        retryLabel={t('retry')}
        onRetry={() => void q.refetch()}
        pending={
          <div className="space-y-2 p-4">
            {[0, 1, 2].map((i) => (
              <Skeleton key={i} className="h-9 w-full" />
            ))}
          </div>
        }
        empty={
          <EmptyState
            icon={<FileText />}
            title={t('sso:audit.emptyChanges.title')}
            reason={t('sso:audit.emptyChanges.reason')}
            action={null}
          />
        }
      >
        {(data) => (
          <div className="overflow-x-auto">
            <table className="w-full min-w-[760px] text-[13px]">
              <thead>
                <tr className="bg-muted text-xs text-muted-foreground">
                  <th className="px-3.5 py-2.5 text-left font-medium">{t('sso:audit.colTime')}</th>
                  <th className="px-3.5 py-2.5 text-left font-medium">{t('sso:audit.colActor')}</th>
                  <th className="px-3.5 py-2.5 text-left font-medium">{t('sso:audit.colAction')}</th>
                  <th className="px-3.5 py-2.5 text-left font-medium">{t('sso:audit.colDetail')}</th>
                  <th className="px-3.5 py-2.5 text-left font-medium">{t('sso:audit.colFrom')}</th>
                </tr>
              </thead>
              <tbody>
                {data.items.map((l) => (
                  <tr key={l.id} className="border-t border-border hover:bg-muted">
                    <td className="whitespace-nowrap px-3.5 py-2.5 text-muted-foreground">
                      {fmt(l.created_at)}
                    </td>
                    <td className="px-3.5 py-2.5">{l.actor_name || `#${l.actor_id}`}</td>
                    <td className="px-3.5 py-2.5">
                      <span className="rounded-[var(--radius-sm)] bg-info-bg px-2 py-0.5 text-[11px] text-info">
                        {t(`sso:audit.action.${l.action}`, { defaultValue: l.action })}
                      </span>
                      <span className="ml-2 text-[11px] text-muted-foreground">
                        {l.object_type}
                        {l.object_id ? ` #${l.object_id}` : ''}
                      </span>
                    </td>
                    <td className="max-w-[36ch] truncate px-3.5 py-2.5 text-[12px] text-muted-foreground">
                      {l.detail || '—'}
                    </td>
                    <td className="whitespace-nowrap px-3.5 py-2.5 font-mono text-[11px] text-muted-foreground">
                      {l.client_ip}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </AsyncBoundary>
    </div>
  )
}

/** 审计里的时间必须到秒：分钟粒度对不上网关日志，就没法交叉验证。 */
function fmt(s: string) {
  const d = new Date(s)
  if (Number.isNaN(d.getTime())) return s
  const p = (n: number) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())} ${p(d.getHours())}:${p(d.getMinutes())}:${p(d.getSeconds())}`
}
