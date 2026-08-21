import { toErrorInfo } from '@ops/api'
import { tError, type Locale, useTranslation } from '@ops/i18n'
import {
  AsyncBoundary,
  Badge,
  Banner,
  type LoadError,
  NotIngested,
  Pagination,
  SearchInput,
  Select,
  TableSkeleton,
  fromQuery,
} from '@ops/ui'
import { getRouteApi, useNavigate } from '@tanstack/react-router'
import {
  type DnsConsistency,
  useCdnRecords,
  useCloudDns,
  useDnsConsistency,
  useRegistrarDns,
} from './queries.js'
import { ByDomainView } from './ByDomainView.js'

const route = getRouteApi('/resources/dns')

/** 统一后的一行，两个来源都映射到这个形状。 */
interface Row {
  source: 'cloudflare' | 'gcp' | 'godaddy'
  zone: string
  name: string
  type: string
  value: string
  proxied: boolean | null
  ttl: number | null
  /**
   * 这条记录生不生效（按域名 NS 判）。三态，`null` = 判不出。
   *
   * ⚠️ 只有注册商这一路目前给得出。CF / GCP 两路要给，得在各自接口里
   * 也接上 NS 判定 —— 那时候这一列才对所有行有意义。
   * 在此之前 CF/GCP 行是 `null`（判不出），不是 `true`。
   */
  effective: boolean | null
  effectiveNote?: string
}

/**
 * DNS 解析。
 *
 * 合并 Cloudflare 与 GCP Cloud DNS 两个来源 —— 理由见 queries.ts。
 *
 * ⚠️ 这一页的头等大事是**别把"没比对成"显示成"两边一致"**。
 * 后端在一方没数据时会返回 not_comparable，那时候「0 个冲突」不成立。
 */
export function DnsPage() {
  const { t, i18n } = useTranslation()
  const search = route.useSearch()
  const navigate = useNavigate()
  const setView = (v: string) =>
    // 换视图时把分页归位：两个视图的条目数完全不同（域名 62 个 vs 记录上千条），
    // 带着第 12 页切过去会落在一张空表上，而"空"在这里会被读成"没有解析"
    void navigate({ to: '/resources/dns', search: { ...search, view: v, page: 1 } })

  // 🔴 「按域名」是默认视图。它和平铺视图并列，不是替换关系 ——
  //	理由见 ByDomainView 的注释（OPSCMDB-036）。
  //	⚠️ 三个数据源的 hook 必须**无条件调用**（React hooks 规则），
  //	所以不能写成 `if (view === 'byDomain') return <ByDomainView/>` 之后再调。
  //	按域名视图下这三个请求确实是多余的开销，但换来的是不违反 hooks 规则；
  //	要省的话得把平铺视图整体拆成子组件，那是另一次重构。
  const cf = useCdnRecords({ q: search.q, type: search.type })
  const gcp = useCloudDns({ q: search.q })
  const consistency = useDnsConsistency()
  const gd = useRegistrarDns({ q: search.q, type: search.type })

  /**
   * 「这条记录生不生效」——逐行判，三方一视同仁。
   *
   * 🔴 原来只有注册商那一路给得出，CF / GCP 两路一律显示「判不出」。
   *	但托管方判据是**域名级**的：既然知道 g32cf.com 的 NS 指向 GoDaddy，
   *	那 CF 上它的每一条记录都判得出——就是不生效。
   *	对判得出的行显示「判不出」，等于把结论藏起来（OPSCMDB-031 P0-10）。
   *
   * ⚠️ 查不到托管方时返回 null，**不是** true。
   */
  const effectiveOf = (source: Row['source'], fqdn: string): [boolean | null, string | undefined] => {
    const auth = consistency.data?.authority
    if (!auth) return [null, undefined]
    // NS 挂在主域名上，逐级往上找（和后端 authorityOf 同一套走法）
    let f = fqdn.toLowerCase().replace(/\.$/, '')
    for (;;) {
      const hit = auth[f]
      if (hit) {
        if (hit.provider === source) return [true, undefined]
        return [false, t('dns:effective.noNote', { authoritative: hit.label, ns: (hit.ns ?? []).join(', ') })]
      }
      const i = f.indexOf('.')
      if (i < 0) return [null, undefined]
      f = f.slice(i + 1)
      if (!f.includes('.')) return [null, undefined]
    }
  }

  const effTuple = (source: Row['source'], fqdn: string) => {
    const [effective, effectiveNote] = effectiveOf(source, fqdn)
    return { effective, effectiveNote }
  }

  const setSearch = (patch: Record<string, string | number>) =>
    void navigate({ to: '/resources/dns', search: { ...search, ...patch } })

  const toLoadError = (e: unknown): LoadError => {
    const n = toErrorInfo(e)
    return { cause: tError(t, n.messageKey, n.params), detail: n.detail, retryable: n.retryable, kind: n.kind === 'license' ? 'license' : undefined, feature: n.params?.feature as string | undefined }
  }

  // 两个来源合并成一份。任一来源在加载/失败时，另一份照常显示 ——
  // 一边挂了就整页报错的话，本来能用的那一半也没了
  const rows: Row[] = [
    ...(cf.data ?? []).map(
      (r): Row => ({
        source: 'cloudflare',
        zone: r.zone ?? '',
        name: r.name ?? '',
        type: r.type ?? '',
        value: r.content ?? '',
        proxied: r.proxied ?? null,
        ttl: r.ttl ?? null,
        ...effTuple('cloudflare', r.name ?? ''),
      }),
    ),
    ...(gcp.data?.records ?? []).map(
      (r): Row => ({
        source: 'gcp',
        zone: r.zone ?? '',
        name: r.name ?? '',
        type: r.type ?? '',
        value: (r.targets ?? []).join(', '),
        // GCP 没有"橙云"这个概念。null 表示不适用，不是 false
        proxied: null,
        ttl: r.ttl ?? null,
        ...effTuple('gcp', r.name ?? ''),
      }),
    ),
    // 🔴 注册商（GoDaddy）这一路是本次补上的。NS 指向 GoDaddy 的域名，
    //    真正生效的解析在这里 —— 它原来一条都不在这一页上
    ...(gd.data?.items ?? []).map(
      (r): Row => ({
        source: 'godaddy',
        zone: r.domain,
        name: r.fqdn,
        type: r.type,
        value: r.data,
        proxied: null,
        ttl: r.ttl,
        // 注册商那一路后端已经判好了，前端不重算 —— 两处各算一次必然分叉
        effective: r.effective,
        effectiveNote: r.effective_note,
      }),
    ),
  ].filter((r) => (search.source === 'all' ? true : r.source === search.source))

  const bothEmpty = rows.length === 0
  const anyPending = cf.isPending || gcp.isPending || gd.isPending
  const anyError = cf.isError || gcp.isError || gd.isError

  const from = (search.page - 1) * search.size
  const pageRows = rows.slice(from, from + search.size)

  const tabs = (
    <div className="flex items-center gap-1 border-b border-border px-4 pt-2.5">
      {(
        [
          ['byDomain', t('dns:view.byDomain'), t('dns:view.byDomainHint')],
          ['all', t('dns:view.all'), t('dns:view.allHint')],
        ] as const
      ).map(([v, label, hint]) => (
        <button
          key={v}
          type="button"
          onClick={() => setView(v)}
          title={hint}
          className={
            search.view === v
              ? 'cursor-pointer border-b-2 border-brand px-3 py-1.5 text-[13px] font-medium text-foreground'
              : 'cursor-pointer border-b-2 border-transparent px-3 py-1.5 text-[13px] text-muted-foreground hover:text-foreground'
          }
        >
          {label}
        </button>
      ))}
    </div>
  )

  if (search.view === 'byDomain') {
    return (
      <div className="flex h-full flex-col">
        {tabs}
        <ByDomainView
          q={search.q}
          page={search.page}
          size={search.size}
          onSearch={setSearch}
          locale={i18n.language as Locale}
        />
      </div>
    )
  }

  return (
    <div className="flex h-full flex-col">
      {tabs}
      <div className="flex flex-wrap items-center gap-2.5 border-b border-border px-4 py-2.5">
        <SearchInput
          value={search.q}
          onChange={(v) => setSearch({ q: v, page: 1 })}
          placeholder={t('dns:searchPlaceholder')}
          clearLabel={t('common:filter.clearSearch')}
        />
        <Select<string>
          label={t('dns:filter.source')}
          value={search.source}
          onChange={(v) => setSearch({ source: v, page: 1 })}
          options={[
            { value: 'all', label: t('common:filter.all') },
            { value: 'cloudflare', label: t('dns:source.cloudflare') },
            { value: 'gcp', label: t('dns:source.gcp') },
            { value: 'godaddy', label: t('dns:source.godaddy') },
          ]}
        />
        <Select<string>
          label={t('dns:filter.type')}
          value={search.type}
          onChange={(v) => setSearch({ type: v, page: 1 })}
          options={[
            { value: 'all', label: t('common:filter.all') },
            ...['A', 'AAAA', 'CNAME', 'TXT', 'MX', 'NS'].map((x) => ({ value: x, label: x })),
          ]}
        />
      </div>

      {/* 一致性横幅。三种状态各说各的话，绝不合并 */}
      {consistency.data ? <ConsistencyBanner data={consistency.data} t={t} /> : null}

      {/* 🔴 GCP 托管区（Cloud DNS zone）此前**整块不可见** ——
          后端一直返回 zones（含每个区的 record_count），前端一条都没渲染。
          它回答的是「GCP 侧到底托管着哪些区、各有多少条」，
          没有它，上面那句「GCP 0 条」是"真的没有"还是"没采到"就分不出来。
          ⚠️ 记录数为 0 的区要显示出来而不是过滤掉：一个存在但空的托管区
          本身就是问题（建了没用，或者记录被误删）。 */}
      {(gcp.data?.zones?.length ?? 0) > 0 ? (
        <div className="flex flex-wrap items-center gap-1.5 border-b border-border px-4 py-2 text-[11px]">
          <span className="text-muted-foreground">{t('dns:zones.label')}</span>
          {(gcp.data?.zones ?? []).map((z) => (
            <span
              key={z.name ?? z.dns_name}
              className="rounded border border-border px-1.5 py-0.5 font-mono"
              title={z.visibility ? t('dns:zones.visibility', { v: z.visibility }) : undefined}
            >
              {z.dns_name ?? z.name}
              <span
                className={
                  (z.record_count ?? 0) === 0 ? 'ml-1 text-warning' : 'ml-1 text-muted-foreground'
                }
              >
                {t('dns:zones.records', { n: z.record_count ?? 0 })}
              </span>
            </span>
          ))}
        </div>
      ) : null}

      {anyPending ? (
        <TableSkeleton columns={[10, 26, 8, 30, 10]} rows={8} />
      ) : bothEmpty ? (
        // 两边都空：优先显示后端给的原因（它才知道是没配凭据还是缺权限）
        <div className="p-6">
          <NotIngested
            title={t('dns:empty.title')}
            reason={
              gcp.data?.empty_hint ??
              (anyError ? t('dns:empty.partialError') : t('dns:empty.reason'))
            }
          />
        </div>
      ) : (
        <>
          <div className="min-h-0 flex-1 overflow-auto">
            <table className="w-full text-[13px]">
              <thead className="sticky top-0 z-10 bg-card">
                <tr className="border-b border-border text-left text-xs text-muted-foreground">
                  <th className="px-4 py-2 font-medium">{t('dns:col.source')}</th>
                  <th className="px-4 py-2 font-medium">{t('dns:col.name')}</th>
                  <th className="px-4 py-2 font-medium">{t('dns:col.type')}</th>
                  <th className="px-4 py-2 font-medium">{t('dns:col.value')}</th>
                  <th className="px-4 py-2 font-medium">{t('dns:col.proxied')}</th>
                  <th className="px-4 py-2 font-medium" title={t('dns:col.effectiveHint')}>
                    {t('dns:col.effective')}
                  </th>
                </tr>
              </thead>
              <tbody>
                {pageRows.map((r, i) => (
                  <tr key={`${r.source}-${r.name}-${r.type}-${i}`} className="border-b border-border">
                    <td className="px-4 py-2">
                      <Badge tone={r.source === 'cloudflare' ? 'info' : r.source === 'godaddy' ? 'warn' : 'mute'}>
                        {t(`dns:source.${r.source}`)}
                      </Badge>
                    </td>
                    <td className="truncate px-4 py-2 font-mono text-xs">{r.name}</td>
                    <td className="px-4 py-2 font-mono text-xs">{r.type}</td>
                    <td className="max-w-[380px] truncate px-4 py-2 font-mono text-xs" title={r.value}>
                      {r.value}
                      {/* 🔴 解析到公共 DNS / 保留地址几乎可以肯定是占位或误配。
                          实测生产：`lscfzone.com A 8.8.8.8` —— 把网站的 A 记录
                          指到 Google Public DNS 上（OPSCMDB-031 P2-12）。
                          这是运维一眼可见的异常，但界面上和正常记录长得一样。 */}
                      {suspiciousTargetKey(r.type, r.value) ? (
                        <span
                          className="ml-1.5"
                          title={t(`dns:suspicious.${suspiciousTargetKey(r.type, r.value)}`)}
                        >
                          <Badge tone="warn">{t('dns:suspicious.label')}</Badge>
                        </span>
                      ) : null}
                    </td>
                    <td className="px-4 py-2">
                      {/* null = 这个来源没有"经 CDN 代理"这个概念，不是 false。
                          显示成"否"等于替 GCP 的记录断言它没走 CDN */}
                      {r.proxied === null ? (
                        <span className="text-xs text-muted-foreground">—</span>
                      ) : r.proxied ? (
                        <Badge tone="warn">{t('dns:proxied')}</Badge>
                      ) : (
                        <Badge tone="mute">{t('dns:direct')}</Badge>
                      )}
                    </td>
                    <td className="px-4 py-2">
                      {/* 🔴 三态，且 null 绝不能渲染成「生效」。
                          「判不出」和「生效」在排查时导向完全相反的动作 */}
                      {r.effective === true ? (
                        <Badge tone="ok">{t('dns:effective.yes')}</Badge>
                      ) : r.effective === false ? (
                        // Badge 不吃 title，套一层 span 才能把「为什么不生效」带出来
                        <span title={r.effectiveNote}>
                          <Badge tone="bad">{t('dns:effective.no')}</Badge>
                        </span>
                      ) : (
                        <span
                          className="text-xs text-muted-foreground"
                          title={r.effectiveNote ?? t('dns:effective.unknownHint')}
                        >
                          {t('dns:effective.unknown')}
                        </span>
                      )}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
          <Pagination
            page={search.page}
            size={search.size}
            total={rows.length}
            onPage={(p) => setSearch({ page: p })}
            onSize={(s) => setSearch({ size: s })}
            rangeLabel={(f, to, total) => t('common:pagination.range', { from: f, to, total })}
            totalLabel={(total) => t('common:pagination.total', { count: total })}
            perPageLabel={t('common:pagination.perPage')}
            prevLabel={t('common:pagination.prev')}
            nextLabel={t('common:pagination.next')}
          />
        </>
      )}
    </div>
  )
}

function ConsistencyBanner({
  data,
  t,
}: {
  data: DnsConsistency
  t: (k: string, p?: Record<string, unknown>) => string
}) {
  const chips = (raw: (string | undefined)[]) => {
    // 同一个域名可能因为**多个来源**各上榜一次（CF 一条、GCP 一条）。
    // 芯片上只有域名，重复出现看着像 bug；去重后数量与横幅里的条数不同，
    // 所以横幅报的是"条数"、芯片列的是"哪些域名"，两者本来就不该相等
    const items = [...new Set(raw.filter(Boolean) as string[])]
    return (
    <div className="mt-1 flex flex-wrap gap-1 font-mono text-[11px]">
      {items.slice(0, 8).map((x) => (
        <span key={x} className="rounded border border-current/30 px-1.5 py-0.5">
          {x}
        </span>
      ))}
      {items.length > 8 ? (
        <span className="px-1.5 py-0.5 opacity-70">{t('dns:consistency.more', { n: items.length - 8 })}</span>
      ) : null}
    </div>
    )
  }

  return (
    <div className="flex flex-col gap-2">
      {/*
        🔴 「配了但不生效」排在最前面。
        它比「目标不一致」更要命：不一致至少有一方是对的，
        而配在没生效的那一方，是**改了完全没效果**，
        排查时人还会拿着这份配置证明"没问题"（OPSCMDB-031 P0-10）。
      */}
      {data.ineffective_count > 0 ? (
        <Banner tone="bad">
          <span className="font-medium">
            {t('dns:consistency.ineffective', { count: data.ineffective_count })}
          </span>
          {/* action_key 优先（可翻译），没有的退回后端直接给的中文。
              ⚠️ 这句是**唯一的出路**（"要么把记录改到 X，要么把 NS 切到 Y"），
                 丢了就只剩一句"有 N 条不生效" */}
          <span className="mt-0.5 block">
            {data.ineffective[0]?.action_key
              ? tError(t, data.ineffective[0].action_key, data.ineffective[0].action_params)
              : data.ineffective[0]?.action}
          </span>
          {chips(data.ineffective.map((x) => x.fqdn))}
        </Banner>
      ) : null}

      {data.split_ns.length > 0 ? (
        <Banner tone="warn">
          <span className="font-medium">
            {t('dns:consistency.splitNs', { count: data.split_ns.length })}
          </span>
          <span className="mt-0.5 block">
            {data.split_ns[0]?.issue_key
              ? tError(t, data.split_ns[0].issue_key, undefined)
              : data.split_ns[0]?.issue}
          </span>
          {chips(data.split_ns.map((x) => x.domain))}
        </Banner>
      ) : null}

      {/* ⚠️ 比对没成立。这一档必须**独立于"没有冲突"**存在：
          显示成"两边一致"会让人放心地不去查，而真相是我们根本没比 */}
      {/*
        🔴 横幅报的是**域名数**，列表底下报的是**记录数** ——
        两个数字天然对不上（实测横幅 GCP 0 / CF 36，列表共 45 条）。
        读的人会去算这笔账，算不平就会怀疑数据有错
        （OPSCMDB-031 P2-11）。所以口径必须写在字面上。
      */}
      {data.not_comparable ? (
        <Banner tone="warn">
          <span className="font-medium">{t('dns:consistency.notComparable')}</span>
          <span className="mt-0.5 block">{data.not_comparable}</span>
        </Banner>
      ) : null}

      {data.conflict_count > 0 ? (
        <Banner tone="bad">
          <span className="font-medium">
            {t('dns:consistency.conflicts', { count: data.conflict_count })}
          </span>
          <span className="mt-0.5 block">
            {/* 能判出托管方时后端会直接说是哪一方生效，比通用文案有用得多 */}
            {data.conflicts[0]?.action_key
              ? tError(t, data.conflicts[0].action_key, data.conflicts[0].action_params)
              : (data.conflicts[0]?.action ?? t('dns:consistency.conflictHint'))}
          </span>
          {chips(data.conflicts.map((c) => c.fqdn))}
        </Banner>
      ) : null}

      {/*
        🔴 比对覆盖面**常驻显示**。
        三方各比了多少 FQDN，是判断「这次比对到底覆盖了多少」的唯一依据 ——
        而它此前只在 `not_comparable` 里出现，也就是**只有某一方完全没数据时才看得见**。
        三方都有数据时，「0 个冲突」背后是比了 3 条还是 3000 条，界面上完全无从判断。

        ⚠️ 写明是 **FQDN 数**不是记录条数：列表底下报的是记录条数，
        两个数字天然对不上（实测横幅 GCP 0 / CF 36、列表共 45 条），
        读的人会去算这笔账、算不平就怀疑数据有错（OPSCMDB-031 P2-11）。
      */}
      <div className="px-0.5 text-[11px] text-muted-foreground">
        {t('dns:consistency.scope', {
          gcp: data.gcp_fqdn_count,
          cf: data.cloudflare_fqdn_count,
          gd: data.godaddy_fqdn_count,
        })}
      </div>

      {/* ⚠️ 判据不完整必须说出来。
          「0 条不生效」建立在"每个域名的 NS 都采到了"这个前提上，
          前提不成立时那个 0 是没有意义的 —— 而它看起来完全正常。
          ⚠️ 这里显示的是后端给的**整句话**，而那句话开头就是 unknown_ns_domains 的数字
          （handlers/cloud_iam_dns.go），所以数字本身不需要再单独渲染一遍。 */}
      {data.authority_incomplete ? (
        <Banner tone="warn">
          <span className="font-medium">{t('dns:consistency.authorityIncomplete')}</span>
          <span className="mt-0.5 block">{data.authority_incomplete}</span>
        </Banner>
      ) : null}
    </div>
  )
}

/**
 * 解析目标看着就不对的几类。返回文案 key，正常返回 null。
 *
 * ⚠️ 只标**几乎可以肯定是错的**那几类，不做启发式猜测。
 * 这一列每多一个误报，真正的异常就少一分可信度 ——
 * 而这一页已经有过"红色失去信号价值"的教训（事件中心 P2-37）。
 *
 * ⚠️ 只对 A/AAAA 生效：TXT 记录里出现 8.8.8.8 完全可能是正常内容
 * （SPF、验证串），标它是纯误报。
 */
function suspiciousTargetKey(type: string, value: string): string | null {
  if (type !== 'A' && type !== 'AAAA') return null
  const v = value.trim()
  // 公共 DNS 解析器的地址。把网站 A 记录指到这里，请求根本到不了业务
  const publicResolvers = new Set([
    '8.8.8.8',
    '8.8.4.4',
    '1.1.1.1',
    '1.0.0.1',
    '9.9.9.9',
    '223.5.5.5',
    '223.6.6.6',
    '114.114.114.114',
  ])
  if (publicResolvers.has(v)) return 'publicResolver'
  // 环回 / 未指定：解析到这里等于解析到访问者自己
  if (v === '127.0.0.1' || v === '0.0.0.0' || v === '::1' || v === '::') return 'loopback'
  // 私网地址出现在**公网**域名的解析里，外部访问者永远连不上。
  // ⚠️ 内部域名指向私网是正常的，所以这一条只提示、不判错
  if (/^10\./.test(v) || /^192\.168\./.test(v) || /^172\.(1[6-9]|2\d|3[01])\./.test(v)) {
    return 'privateRange'
  }
  return null
}
