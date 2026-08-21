import { tError } from '@ops/i18n'
import { toErrorInfo } from '@ops/api'
import { Badge, Banner, Button, Dialog, Select, Skeleton } from '@ops/ui'
import { useState } from 'react'
import { type PodTarget, usePodContainers, usePodDiagnose, usePodEvents, usePodLogs } from './diag.js'

type TFn = (k: string, o?: Record<string, unknown>) => string
type View = 'logs' | 'events' | 'diagnose'

/**
 * Pod 排障弹窗：日志 / 事件 / 诊断。
 *
 * 为什么是大弹窗：日志本身就要宽，换行截断会让人读不下去。
 *
 * ⚠️ 三档**只有当前这一档在请求**。它们都是实时打到集群的
 * （日志要经 APIServer 代理到节点 kubelet），三个一起挂载等于每开一次弹窗
 * 就给集群打三发请求，而其中两发用户根本没看。
 */
export function PodDiagDialog({
  target,
  onClose,
  t,
}: {
  target: PodTarget
  onClose: () => void
  t: TFn
}) {
  const [view, setView] = useState<View>('diagnose')

  const views: { key: View; label: string }[] = [
    { key: 'diagnose', label: t('pods:diag.diagnose') },
    { key: 'logs', label: t('pods:diag.logs') },
    { key: 'events', label: t('pods:diag.events') },
  ]

  return (
    <Dialog
      open
      onClose={onClose}
      title={target.name}
      description={`${target.namespace}`}
      closeLabel={t('common:action.close')}
      width={1100}
      footer={
        <Button size="sm" onClick={onClose}>
          {t('common:action.close')}
        </Button>
      }
    >
      <div className="-mx-4 -mt-1 flex flex-wrap gap-1.5 border-b border-border px-4 pb-2.5">
        {views.map((v) => (
          <Button
            key={v.key}
            size="sm"
            variant={view === v.key ? 'primary' : undefined}
            onClick={() => setView(v.key)}
          >
            {v.label}
          </Button>
        ))}
      </div>

      <div className="-mx-4">
        {view === 'diagnose' ? <DiagnoseView target={target} t={t} /> : null}
        {view === 'logs' ? <LogsView target={target} t={t} /> : null}
        {view === 'events' ? <EventsView target={target} t={t} /> : null}
      </div>
    </Dialog>
  )
}

function Failed({ e, t }: { e: unknown; t: TFn }) {
  const n = toErrorInfo(e)
  return (
    <div className="px-4 py-3">
      <Banner tone="bad">
        <span className="font-medium">{t('pods:diag.loadFailed')}</span>
        {/* 后端已经把 kubelet 那类报错翻译过了（diag.ExplainLogError），原样显示 */}
        <span className="mt-0.5 block">{n.detail || tError(t, n.messageKey, n.params)}</span>
      </Banner>
    </div>
  )
}

const Loading = () => (
  <div className="flex flex-col gap-2 px-4 py-3">
    <Skeleton className="h-4 w-[70%]" />
    <Skeleton className="h-4 w-[45%]" />
  </div>
)

function DiagnoseView({ target, t }: { target: PodTarget; t: TFn }) {
  const q = usePodDiagnose(target)
  if (q.isPending) return <Loading />
  if (q.isError) return <Failed e={q.error} t={t} />
  const r = q.data?.result

  // ⚠️ 没命中规则 ≠ 这个 Pod 没问题。规则库覆盖不到的故障多得是，
  // 显示成"未发现问题"会让人放心地不再往下查 —— 那比不给结论更糟
  if (!r?.matched) {
    return (
      <div className="px-4 py-3">
        <Banner tone="warn">
          <span className="font-medium">{t('pods:diag.noMatch')}</span>
          <span className="mt-0.5 block">{t('pods:diag.noMatchHint')}</span>
        </Banner>
      </div>
    )
  }

  // provider 形如 `ai:claude-haiku-4-5`。判前缀而不是全等 ——
  // 型号会变，写死型号名的话换个模型这个标记就悄悄失效了
  const isAI = (r.provider ?? '').startsWith('ai:')

  return (
    <div className="flex flex-col gap-3 px-4 py-3">
      <Banner tone="bad">
        <div className="flex flex-wrap items-center gap-2">
          <span className="font-medium">{r.root_cause}</span>
          {/* 🔴 AI 判定必须**一眼可辨**。
              规则给的是"日志里写着 OutOfMemoryError"，AI 给的是"看起来像" ——
              两者混在一起显示，等于把猜测当成了证据。
              置信度也由后端封顶在 medium，不会出现 AI 判的 high。 */}
          {isAI ? <Badge tone="warn">{t('pods:diag.aiBadge')}</Badge> : null}
        </div>
        {r.provider ? (
          /* provider 是内部值（rule / ai:xxx），直接怼进文案会读成「判定来源：rule」 */
          <span className="mt-0.5 block text-xs">
            {isAI
              ? t('pods:diag.byAI', { model: r.provider.slice(3) })
              : t('pods:diag.byRule', {
                  rule: r.provider === 'rule' ? t('pods:diag.providerRule') : r.provider,
                })}
          </span>
        ) : null}
        {isAI ? (
          <span className="mt-1 block text-xs">{t('pods:diag.aiCaveat')}</span>
        ) : null}
      </Banner>

      {/* 「为什么没走 AI」也要说出来 —— 只写日志的话没人会去翻，
          而这正是人对 AI 兜底的第一个疑问。
          ⚠️ 走了 AI 的情况上面已经标过，这里只说没走的。 */}
      {r.ai_gate && !r.ai_gate.allowed ? (
        <section className="rounded-[var(--radius)] border border-border px-3 py-2">
          <h3 className="mb-1 text-[11px] font-medium tracking-wide text-muted-foreground uppercase">
            {t('pods:diag.aiGate')}
          </h3>
          <p className="text-xs text-muted-foreground">{r.ai_gate.reason}</p>
          {(r.ai_gate.layers_tried?.length ?? 0) > 0 ? (
            <ul className="mt-1 flex flex-col gap-0.5">
              {r.ai_gate.layers_tried?.map((l, i) => (
                <li key={i} className="text-[11px] text-muted-foreground">
                  · {l}
                </li>
              ))}
            </ul>
          ) : null}
        </section>
      ) : null}

      {/* 🔴 捞出来的报错行排在「依据」之前、紧跟结论 ——
          规则判不出来时，这几行才是人真正要看的东西。
          排到下面去等于没做：人扫完泛化结论就已经去翻 kubectl 了 */}
      {r.extracted ? (
        <section>
          <h3 className="mb-1 text-[11px] font-medium tracking-wide text-muted-foreground uppercase">
            {t('pods:diag.extracted')}
          </h3>
          {(r.extracted.lines?.length ?? 0) > 0 ? (
            <ul className="flex flex-col gap-1">
              {r.extracted.lines?.map((l, i) => (
                <li
                  key={l}
                  className={`font-mono text-xs break-words whitespace-normal ${
                    i === 0 ? 'text-danger' : 'text-foreground'
                  }`}
                >
                  {l}
                </li>
              ))}
            </ul>
          ) : (
            /* ⚠️ 一条都没捞到不是「没问题」，是「应用什么都没说」——
               必须把这个区别说出来，否则会被读成日志正常 */
            <Banner tone="warn">
              <span>{r.extracted.note}</span>
            </Banner>
          )}
          {(r.extracted.lines?.length ?? 0) > 0 ? (
            <p className="mt-1 text-[11px] text-muted-foreground">{r.extracted.note}</p>
          ) : null}
        </section>
      ) : null}

      {(r.evidence?.length ?? 0) > 0 ? (
        <section>
          <h3 className="mb-1 text-[11px] font-medium tracking-wide text-muted-foreground uppercase">
            {t('pods:diag.evidence')}
          </h3>
          <ul className="flex flex-col gap-1">
            {r.evidence?.map((e) => (
              <li key={e} className="font-mono text-xs break-words text-foreground">
                {e}
              </li>
            ))}
          </ul>
        </section>
      ) : null}

      {(r.solutions?.length ?? 0) > 0 ? (
        <section>
          <h3 className="mb-1 text-[11px] font-medium tracking-wide text-muted-foreground uppercase">
            {t('pods:diag.suggestions')}
          </h3>
          {/* 只给方案，不代执行 —— CMDB 是只读的，动手在控制台/kubectl */}
          <ul className="flex flex-col gap-1">
            {r.solutions?.map((sg) => (
              <li key={sg.text} className="text-[13px] leading-relaxed text-foreground">
                {sg.text}
                {/* 后端能算出该去哪一页（证书页/服务页）时直接给链接 ——
                    「去证书页查这个域名」和一个能点的链接之间，差的正是人要花的那几步 */}
                {sg.link ? (
                  <a
                    href={sg.link}
                    className="ml-1.5 cursor-pointer text-brand-text underline-offset-2 hover:underline"
                  >
                    {t('pods:diag.goThere')}
                  </a>
                ) : null}
              </li>
            ))}
          </ul>
          <p className="mt-1.5 text-xs text-muted-foreground">{t('pods:diag.readOnlyNote')}</p>
        </section>
      ) : null}
    </div>
  )
}

function LogsView({ target, t }: { target: PodTarget; t: TFn }) {
  const [tail, setTail] = useState(200)
  const [previous, setPrevious] = useState(false)
  const [container, setContainer] = useState('')
  const cs = usePodContainers(target)
  const containers = cs.data?.items ?? []

  // 默认选谁：优先第一个**非 sidecar** 的业务容器。
  // ⚠️ 不能直接取 containers[0] —— istio-proxy 经常排在前面，
  //	那样一打开日志看到的全是代理的访问日志，业务日志要手动切才看得到。
  // ⚠️ 也不能在只有一个容器时强行传 container：单容器 Pod 传了没坏处，
  //	但容器名对不上时反而会报错，不如让后端按默认走。
  const effective =
    container || (containers.length > 1 ? (containers.find((c) => !c.sidecar && !c.init) ?? containers[0])?.name : '')
  // 容器名单没回来之前不发日志请求（见 usePodLogs 的 ready 参数）。
  // 名单接口自己失败时也要放行 —— 否则集群连不上就连"日志取不到"这个错误都看不见了
  const q = usePodLogs(target, tail, previous, effective ?? '', !cs.isPending)

  return (
    <div className="flex flex-col">
      <div className="flex flex-wrap items-center gap-2 border-b border-border px-4 py-2">
        {/* 只在真的有多个容器时才出现 —— 单容器 Pod 加个只有一项的下拉是噪音 */}
        {containers.length > 1 ? (
          <Select<string>
            label={t('pods:diag.container')}
            value={effective ?? ''}
            onChange={setContainer}
            options={containers.map((c) => ({
              value: c.name,
              // 状态标在选项里：不用挨个点就能看出哪个容器有问题。
              // init 也列出来 —— Pod 卡在 Init 时那是唯一有信息的地方
              label:
                c.name +
                (c.init ? t('pods:diag.initSuffix') : '') +
                (c.state && c.state !== 'Running' ? ` · ${c.state}` : ''),
            }))}
          />
        ) : null}
        <Select<string>
          label={t('pods:diag.tail')}
          value={String(tail)}
          onChange={(v) => setTail(Number(v))}
          options={[200, 500, 1000, 2000].map((n) => ({ value: String(n), label: String(n) }))}
        />
        {/* 上一个容器实例的日志：CrashLoopBackOff 时当前容器往往还没写日志就又挂了，
            真正有信息的是**上一次**那份 */}
        <Button size="sm" variant={previous ? 'primary' : undefined} onClick={() => setPrevious((v) => !v)}>
          {t('pods:diag.previous')}
        </Button>
        <Button size="sm" loading={q.isFetching} onClick={() => void q.refetch()}>
          {t('common:action.retry')}
        </Button>
      </div>

      {q.isPending ? (
        <Loading />
      ) : q.isError ? (
        <Failed e={q.error} t={t} />
      ) : (
        <pre className="max-h-[52vh] overflow-auto px-4 py-3 font-mono text-[11px] leading-relaxed whitespace-pre-wrap text-foreground">
          {/* 后端在 kubelet 取不到时会退到 Loki，并在正文最前面加两行 # 说明。
              那两行必须原样显示 —— 否则用户会把历史日志当成实时日志 */}
          {q.data || t('pods:diag.noLogs')}
        </pre>
      )}
    </div>
  )
}

function EventsView({ target, t }: { target: PodTarget; t: TFn }) {
  const q = usePodEvents(target)
  if (q.isPending) return <Loading />
  if (q.isError) return <Failed e={q.error} t={t} />
  const rows = q.data ?? []

  if (rows.length === 0) {
    return (
      <div className="px-4 py-3">
        {/* 事件有保留期（默认 1 小时），空列表不代表没发生过 */}
        <Banner tone="info">
          <span className="font-medium">{t('pods:diag.noEvents')}</span>
          <span className="mt-0.5 block">{t('pods:diag.noEventsHint')}</span>
        </Banner>
      </div>
    )
  }

  return (
    <div className="max-h-[52vh] overflow-auto">
      <table className="w-full text-[13px]">
        <thead className="sticky top-0 z-10 bg-card">
          <tr className="border-b border-border text-left text-xs text-muted-foreground">
            <th className="px-4 py-2 font-medium">{t('pods:diag.col.time')}</th>
            <th className="px-4 py-2 font-medium">{t('pods:diag.col.reason')}</th>
            <th className="px-4 py-2 font-medium">{t('pods:diag.col.message')}</th>
            <th className="px-4 py-2 font-medium">{t('pods:diag.col.count')}</th>
          </tr>
        </thead>
        <tbody>
          {rows.map((e, i) => (
            <tr key={`${e.reason}-${e.last_seen}-${i}`} className="border-b border-border last:border-0">
              <td className="px-4 py-2 font-mono text-xs whitespace-nowrap">{e.last_seen || '—'}</td>
              <td className="px-4 py-2">
                <Badge tone={e.type === 'Warning' ? 'warn' : 'mute'}>{e.reason || '—'}</Badge>
              </td>
              <td className="px-4 py-2 text-xs break-words">{e.message || '—'}</td>
              <td className="px-4 py-2 tabular text-xs">{e.count ?? '—'}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  )
}
