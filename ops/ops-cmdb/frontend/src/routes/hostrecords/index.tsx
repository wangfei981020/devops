import { toErrorInfo } from '@ops/api'
import { tError, useTranslation } from '@ops/i18n'
import { AsyncBoundary, Badge, Banner, Dialog, EmptyState, Field, MutationError, NotIngested, Pagination, SearchInput, Select, Switch, TableSkeleton, TextInput, fromQuery, type LoadError } from '@ops/ui'
import { getRouteApi, useNavigate } from '@tanstack/react-router'
import { useState } from 'react'
import { WriteButton } from '../../components/WriteButton.js'
import { useBasicDicts } from '../basic/queries.js'
import { useCdnVendors } from '../cdn/queries.js'
import { OriginRulesDialog } from './OriginRulesDialog.js'
import { type HostRecord, useBulkIgnoreRecords, useBulkUpdate, useHostRecords } from './queries.js'
import { RecordDialog } from './RecordDialog.js'
import { RecordRowActions } from './RecordRowActions.js'

const route = getRouteApi('/resources/host-records')
const PERM = 'cmdb:manage_records'

/**
 * 主机头台账。
 *
 * ⚠️ 和「DNS 解析」页是两套数据：那边是 CF/GCP 上此刻的实时解析，
 * 这边是我们自己的台账（谁负责、属于哪个项目、哪些不用管）。
 * 名字必须分开，否则人点进来看到的不是想要的东西。
 */
export function HostRecordsPage() {
  const { t } = useTranslation()
  const search = route.useSearch()
  const navigate = useNavigate()
  const query = useHostRecords(search.status)
  const dicts = useBasicDicts()
  const [selected, setSelected] = useState<Set<number>>(new Set())
  const [bulk, setBulk] = useState<'attr' | 'ignore' | null>(null)
  const [rulesOpen, setRulesOpen] = useState(false)
  const [adding, setAdding] = useState(false)

  const setSearch = (patch: Record<string, string | number>) =>
    void navigate({ to: '/resources/host-records', search: { ...search, ...patch } })

  const toLoadError = (e: unknown): LoadError => {
    const n = toErrorInfo(e)
    return { cause: tError(t, n.messageKey, n.params), detail: n.detail, retryable: n.retryable, kind: n.kind === 'license' ? 'license' : undefined, feature: n.params?.feature as string | undefined }
  }

  const toggle = (id: number) =>
    setSelected((s) => {
      const next = new Set(s)
      if (next.has(id)) next.delete(id)
      else next.add(id)
      return next
    })

  return (
    <div className="flex h-full flex-col">
      <div className="flex flex-wrap items-center gap-2.5 border-b border-border px-4 py-2.5">
        <SearchInput
          value={search.q}
          onChange={(v) => setSearch({ q: v, page: 1 })}
          placeholder={t('hostrecords:searchPlaceholder')}
          clearLabel={t('common:filter.clearSearch')}
        />
        <Select<string>
          label={t('hostrecords:filter.scope')}
          value={search.status}
          onChange={(v) => setSearch({ status: v, page: 1 })}
          options={[
            { value: '', label: t('hostrecords:scope.active') },
            { value: 'ignored', label: t('hostrecords:scope.ignored') },
            { value: 'all', label: t('common:filter.all') },
          ]}
        />

        {/* 选中之后才出现批量操作：常态下这两个按钮点了也没意义 */}
        {selected.size > 0 ? (
          <>
            <span className="text-xs text-muted-foreground">
              {t('hostrecords:selected', { count: selected.size })}
            </span>
            <WriteButton perm={PERM} size="sm" onClick={() => setBulk('attr')}>
              {t('hostrecords:bulk.setAttr')}
            </WriteButton>
            <WriteButton perm={PERM} size="sm" onClick={() => setBulk('ignore')}>
              {t('hostrecords:bulk.ignore')}
            </WriteButton>
          </>
        ) : null}

        <div className="ml-auto flex items-center gap-2">
          <WriteButton perm={PERM} size="sm" onClick={() => setRulesOpen(true)}>
            {t('hostrecords:originRules.entry')}
          </WriteButton>
          {/* ⚠️ 新增入口在工具条上（AsyncBoundary 外面）：台账为空时正是最需要
              手工补一条的时候，不能因为走了空态分支就整个消失 */}
          <WriteButton perm={PERM} variant="primary" size="sm" onClick={() => setAdding(true)}>
            {t('hostrecords:form.createTitle')}
          </WriteButton>
        </div>
      </div>

      <AsyncBoundary
        state={fromQuery(query, (d) => d.length === 0, toLoadError)}
        errorTitle={t('hostrecords:error.title')}
        retryLabel={t('common:action.retry')}
        onRetry={() => void query.refetch()}
        pending={<TableSkeleton columns={[4, 28, 14, 14, 14, 12]} rows={8} />}
        empty={
          <div className="p-6">
            {search.status === 'ignored' ? (
              <EmptyState
                title={t('hostrecords:empty.noIgnored')}
                reason={t('hostrecords:empty.noIgnoredReason')}
                action={null}
              />
            ) : (
              // 台账为空多半是域名还没同步过，不是"我们没有主机头"
              <NotIngested
                title={t('hostrecords:empty.title')}
                reason={t('hostrecords:empty.reason')}
              />
            )}
          </div>
        }
      >
        {(all) => {
          const kw = search.q.trim().toLowerCase()
          const rows = all.filter((r) =>
            kw ? `${r.fqdn} ${r.project} ${r.module} ${r.cname}`.toLowerCase().includes(kw) : true,
          )
          const from = (search.page - 1) * search.size
          const pageRows = rows.slice(from, from + search.size)
          return (
            <>
              <CoverageBanner all={all} t={t} />
              <div className="min-h-0 flex-1 overflow-auto">
                <table className="w-full text-[13px]">
                  <thead className="sticky top-0 z-10 bg-card">
                    <tr className="border-b border-border text-left text-xs text-muted-foreground">
                      <th className="w-8 px-3 py-2" />
                      <th className="px-3 py-2 font-medium">{t('hostrecords:col.fqdn')}</th>
                      <th className="px-3 py-2 font-medium">{t('hostrecords:col.project')}</th>
                      <th className="px-3 py-2 font-medium">{t('hostrecords:col.module')}</th>
                      <th className="px-3 py-2 font-medium">{t('hostrecords:col.origin')}</th>
                      <th className="px-3 py-2 font-medium" title={t('hostrecords:col.stateHint')}>
                        {t('hostrecords:col.state')}
                      </th>
                      <th className="w-12 px-3 py-2" />
                    </tr>
                  </thead>
                  <tbody>
                    {pageRows.map((r) => (
                      <Row key={r.id} r={r} checked={selected.has(r.id)} onToggle={toggle} t={t} />
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
          )
        }}
      </AsyncBoundary>

      {bulk === 'attr' ? (
        <BulkAttrDialog
          ids={[...selected]}
          projects={(dicts.data?.projects ?? []).map((p) => p.name)}
          envs={(dicts.data?.envs ?? []).map((e) => e.code)}
          onClose={() => setBulk(null)}
          onDone={() => {
            setBulk(null)
            setSelected(new Set())
          }}
        />
      ) : null}
      {bulk === 'ignore' ? (
        <BulkIgnoreDialog
          ids={[...selected]}
          onClose={() => setBulk(null)}
          onDone={() => {
            setBulk(null)
            setSelected(new Set())
          }}
        />
      ) : null}
      {rulesOpen ? <OriginRulesDialog onClose={() => setRulesOpen(false)} /> : null}
      {adding ? <RecordDialog onClose={() => setAdding(false)} /> : null}
    </div>
  )
}

type T = (k: string, p?: Record<string, unknown>) => string

function Row({
  r,
  checked,
  onToggle,
  t,
}: {
  r: HostRecord
  checked: boolean
  onToggle: (id: number) => void
  t: T
}) {
  return (
    <tr className="border-b border-border last:border-0">
      <td className="px-3 py-1.5">
        <input
          type="checkbox"
          checked={checked}
          onChange={() => onToggle(r.id)}
          aria-label={r.fqdn}
        />
      </td>
      <td className="px-3 py-1.5 font-mono text-xs">
        {r.fqdn}
        {/* 主域名已失效：这条记录多半也没意义了，但它在列表里看着正常 */}
        {r.domain_gone ? (
          <span className="ml-1.5">
            <Badge tone="bad">{r.domain_gone}</Badge>
          </span>
        ) : null}
      </td>
      <td className="px-3 py-1.5">
        {r.project || (
          <span className="text-xs text-warning" title={t('hostrecords:noProjectHint')}>
            {t('hostrecords:noProject')}
          </span>
        )}
      </td>
      <td className="px-3 py-1.5 text-xs">{r.module || '—'}</td>
      <td className="px-3 py-1.5 font-mono text-[11px]">
        {/*
          ⚠️ `origin_ip` 这一列里混进了**状态值**。
          
          实测大量行的值是 `Parked`（域名停放）—— 那是一种状态，不是 IP 地址。
          把它塞进「源站 IP」列有两个后果（OPSCMDB-031 P1-16）：
            · 右边就有一个专门的「状态」列，而且是空的
            · 任何按"源站 IP"做的筛选/统计都会把 Parked 当成一个 IP 值
          
          数据本身是上游给的，改不了；但**渲染时要按语义分流** ——
          非 IP 的值挪到状态列去显示，这一列只留真正的地址。
        */}
        {r.origin_ip && !isNonAddressOrigin(r.origin_ip) ? (
          r.origin_ip
        ) : r.auto_origin_ip ? (
          // 推测值必须标出来：它没落库，也可能是错的。
          // 和手填值长得一样的话，人会把推测当成登记过的事实
          <span className="text-muted-foreground">
            {r.auto_origin_ip}
            <span className="ml-1 italic">{t('hostrecords:guessed')}</span>
          </span>
        ) : (
          '—'
        )}
      </td>
      <td className="px-3 py-1.5">
        {r.ignored ? (
          <Badge tone="mute">{t('hostrecords:ignored')}</Badge>
        ) : r.stale ? (
          <Badge tone="warn">{t('hostrecords:stale')}</Badge>
        ) : r.origin_ip && isNonAddressOrigin(r.origin_ip) ? (
          // 从「源站 IP」列分流过来的状态值 —— 它本来就属于这一列
          <Badge tone="mute">{r.origin_ip}</Badge>
        ) : (
          <span className="text-xs text-muted-foreground">—</span>
        )}
      </td>
      <td className="px-3 py-1.5">
        <RecordRowActions r={r} />
      </td>
    </tr>
  )
}

function BulkAttrDialog({
  ids,
  projects,
  envs,
  onClose,
  onDone,
}: {
  ids: number[]
  projects: string[]
  envs: string[]
  onClose: () => void
  onDone: () => void
}) {
  const { t } = useTranslation()
  const m = useBulkUpdate()
  // undefined = 不动这一项；'' = 清空。两者必须分开，
  // 否则一次只想改项目的批量操作会把环境和模块一起清掉
  const [project, setProject] = useState<string | undefined>(undefined)
  const [env, setEnv] = useState<string | undefined>(undefined)
  const [mod, setMod] = useState<string | undefined>(undefined)
  // 🔴 CDN / 源站 IP / 回源 CNAME 走后端的 `set_*` 布尔开关。
  //	和上面三项一样是「保持 / 清空 / 设为」三态，但表达方式不同：
  //	那三项靠指针（不传=不动），这三项靠一个显式布尔 ——
  //	因为它们传空串是**有意义的写入**（清掉手填值、回到自动推算），
  //	不能靠"没填"来表示"不动"。
  const [cdnId, setCdnId] = useState<string | undefined>(undefined)
  const [originIp, setOriginIp] = useState<string | undefined>(undefined)
  const [cname, setCname] = useState<string | undefined>(undefined)
  const vendors = useCdnVendors()

  return (
    <Dialog
      open
      onClose={onClose}
      title={t('hostrecords:bulk.attrTitle', { count: ids.length })}
      description={t('hostrecords:bulk.attrDesc')}
      closeLabel={t('common:action.close')}
      footer={
        <>
          <WriteButton perm={PERM} size="sm" onClick={onClose}>
            {t('common:action.cancel')}
          </WriteButton>
          <WriteButton
            perm={PERM}
            variant="primary"
            size="sm"
            loading={m.isPending}
            blockedReason={
              project === undefined &&
              env === undefined &&
              mod === undefined &&
              cdnId === undefined &&
              originIp === undefined &&
              cname === undefined
                ? t('hostrecords:bulk.nothingSet')
                : undefined
            }
            onClick={() =>
              m.mutate(
                {
                  ids,
                  project,
                  env,
                  module: mod,
                  // ⚠️ set_* 只在**真的要动**时才发 true。
                  //	恒发 true 的话，用户只想改项目也会把 CDN 一起清掉。
                  ...(cdnId !== undefined
                    ? { set_cdn: true, cdn_id: cdnId === '' ? null : Number(cdnId) }
                    : {}),
                  ...(originIp !== undefined
                    ? { set_origin_ip: true, origin_ip: originIp }
                    : {}),
                  ...(cname !== undefined ? { set_cname: true, cname } : {}),
                },
                { onSuccess: onDone },
              )
            }
          >
            {t('common:write.save')}
          </WriteButton>
        </>
      }
    >
      <div className="flex flex-col gap-3">
        <div className="flex flex-col gap-1.5">
          <Select<string>
            label={t('hostrecords:col.project')}
            value={project ?? '__keep__'}
            onChange={(v) => setProject(v === '__keep__' ? undefined : v)}
            options={[
              { value: '__keep__', label: t('hostrecords:bulk.keep') },
              { value: '', label: t('hostrecords:bulk.clear') },
              ...projects.map((p) => ({ value: p, label: p })),
            ]}
            className="self-start"
          />
          <Select<string>
            label={t('hostrecords:col.env')}
            value={env ?? '__keep__'}
            onChange={(v) => setEnv(v === '__keep__' ? undefined : v)}
            options={[
              { value: '__keep__', label: t('hostrecords:bulk.keep') },
              { value: '', label: t('hostrecords:bulk.clear') },
              ...envs.map((e) => ({ value: e, label: e })),
            ]}
            className="self-start"
          />
        </div>
        <Field label={t('hostrecords:col.module')} hint={t('hostrecords:bulk.moduleHint')}>
          <TextInput
            value={mod ?? ''}
            onChange={(e) => setMod(e.target.value)}
            placeholder={t('hostrecords:bulk.keep')}
          />
        </Field>

        {/* 换 CDN / 迁源站时最需要批量的三项。旧版有，新版此前只能一条条改。 */}
        <Select<string>
          label={t('hostrecords:bulk.cdn')}
          value={cdnId ?? '__keep__'}
          onChange={(v) => setCdnId(v === '__keep__' ? undefined : v)}
          options={[
            { value: '__keep__', label: t('hostrecords:bulk.keep') },
            { value: '', label: t('hostrecords:bulk.clear') },
            ...(vendors.data ?? []).map((v) => ({ value: String(v.id), label: v.name })),
          ]}
          className="self-start"
        />
        <Field label={t('hostrecords:bulk.originIp')} hint={t('hostrecords:bulk.originIpHint')}>
          <div className="flex items-center gap-2">
            {/* 「要不要动这一项」用一个显式勾选表达，而不是靠输入框空不空 ——
                空串在这里是"清掉手填值"，是一个真实的写入动作。 */}
            <Switch
              checked={originIp !== undefined}
              onChange={(on) => setOriginIp(on ? '' : undefined)}
              label={t('hostrecords:bulk.setThis')}
            />
            <TextInput
              value={originIp ?? ''}
              onChange={(e) => setOriginIp(e.target.value)}
              disabled={originIp === undefined}
              placeholder={t('hostrecords:bulk.autoDerive')}
              className="flex-1 font-mono"
            />
          </div>
        </Field>
        <Field label={t('hostrecords:bulk.cname')} hint={t('hostrecords:bulk.cnameHint')}>
          <div className="flex items-center gap-2">
            <Switch
              checked={cname !== undefined}
              onChange={(on) => setCname(on ? '' : undefined)}
              label={t('hostrecords:bulk.setThis')}
            />
            <TextInput
              value={cname ?? ''}
              onChange={(e) => setCname(e.target.value)}
              disabled={cname === undefined}
              placeholder={t('hostrecords:bulk.autoDerive')}
              className="flex-1 font-mono"
            />
          </div>
        </Field>
        {m.isError ? (
          <MutationError error={m.error} toInfo={toErrorInfo} t={(k, p) => tError(t, k, p)} />
        ) : null}
      </div>
    </Dialog>
  )
}

function BulkIgnoreDialog({
  ids,
  onClose,
  onDone,
}: {
  ids: number[]
  onClose: () => void
  onDone: () => void
}) {
  const { t } = useTranslation()
  const m = useBulkIgnoreRecords()
  const [reason, setReason] = useState('')

  return (
    <Dialog
      open
      onClose={onClose}
      title={t('hostrecords:bulk.ignoreTitle', { count: ids.length })}
      description={t('hostrecords:bulk.ignoreDesc')}
      closeLabel={t('common:action.close')}
      footer={
        <>
          <WriteButton perm={PERM} size="sm" onClick={onClose}>
            {t('common:action.cancel')}
          </WriteButton>
          <WriteButton
            perm={PERM}
            size="sm"
            loading={m.isPending}
            onClick={() => m.mutate({ ids, ignored: false, reason: '' }, { onSuccess: onDone })}
          >
            {t('hostrecords:bulk.unignore')}
          </WriteButton>
          <WriteButton
            perm={PERM}
            variant="primary"
            size="sm"
            loading={m.isPending}
            // 理由必填：半年后没人记得这条为什么不告警了
            blockedReason={reason ? undefined : t('hostrecords:bulk.needReason')}
            onClick={() => m.mutate({ ids, ignored: true, reason }, { onSuccess: onDone })}
          >
            {t('hostrecords:bulk.doIgnore')}
          </WriteButton>
        </>
      }
    >
      <div className="flex flex-col gap-3">
        <Banner tone="warn">
          <span>{t('hostrecords:bulk.ignoreWarn')}</span>
        </Banner>
        <Field
          label={t('hostrecords:bulk.reason')}
          hint={t('hostrecords:bulk.reasonHint')}
          required
        >
          <TextInput
            value={reason}
            onChange={(e) => setReason(e.target.value)}
            placeholder={t('hostrecords:bulk.reasonPlaceholder')}
            autoFocus
          />
        </Field>
        {m.isError ? (
          <MutationError error={m.error} toInfo={toErrorInfo} t={(k, p) => tError(t, k, p)} />
        ) : null}
      </div>
    </Dialog>
  )
}

/**
 * `origin_ip` 里的值是不是**非地址**（即：其实是个状态）。
 *
 * 实测上游会往这一列写 `Parked`（域名停放）。
 *
 * ⚠️ 判据是"解析不成 IP 也不是域名形态"，而不是枚举 `Parked` 一个词 ——
 * 上游随时可能加别的状态词（`Suspended`、`Pending` …），
 * 硬编码一个词的话下一个词又会漏到 IP 列里去。
 */
function isNonAddressOrigin(v: string) {
  const s = v.trim()
  if (!s) return false
  // IPv4 / IPv6 / 域名形态都算地址
  if (/^\d{1,3}(\.\d{1,3}){3}$/.test(s)) return false
  if (s.includes(':')) return false // IPv6
  if (/^[a-z0-9-]+(\.[a-z0-9-]+)+$/i.test(s)) return false // CNAME 目标
  return true
}

/**
 * 「归属登记」的覆盖率。
 *
 * # 为什么要有这一条
 *
 * 实测生产：828 条主机头里 **828 条**没有归属项目、828 条没有模块、
 * 828 条没有状态 —— 五列里四列几乎不承载信息（OPSCMDB-031 P1-17）。
 *
 * 每一行都诚实地标了「未归属」（橙色，不是空白，这一点是对的），
 * 但**逐行诚实加不出一个结论**：翻 17 页才能发现"一条都没有"，
 * 而这不是 828 个各自的小问题，是**一件事没做**——
 * 自动关联模块要么没跑过、要么没生效。
 *
 * ⚠️ 全都登记好时这一条也要在（写「都已登记」）。
 * 只在有问题时出现的提示，看不到它的时候人分不清是"没问题"还是"提示坏了"。
 */
function CoverageBanner({
  all,
  t,
}: {
  all: HostRecord[]
  t: (k: string, o?: Record<string, unknown>) => string
}) {
  if (all.length === 0) return null
  // ⚠️ 只数**归属登记**这两项（项目、模块）。
  //
  //	不数「状态」列 —— 那一列渲染的不是 life_status，而是"有没有例外"
  //	（已忽略 / 域名已移出 / 停放）。它整列是「—」恰恰说明一切正常，
  //	把它算进"没登记"会凭空造出一个不存在的缺口
  //	（我自己第一版就这么写了，界面上当场自相矛盾：横幅说"都已登记"，
  //	而状态列 4 行全是「—」）。
  const noProject = all.filter((r) => !r.project).length
  const noModule = all.filter((r) => !r.module).length

  if (noProject === 0 && noModule === 0) {
    return (
      <p className="border-b border-border px-4 py-2 text-xs text-muted-foreground">
        {t('hostrecords:coverage.allDone', { total: all.length })}
      </p>
    )
  }
  // 三项全缺、且是**全部**缺 —— 这是"这件事没做"，不是"漏了几条"，
  // 说法必须不一样，否则人会去一条条补，而正确的下一步是跑一次自动关联
  const allMissing = noProject === all.length && noModule === all.length
  return (
    <div className="border-b border-border px-4 py-2">
      <p className="text-xs text-warning">
        {allMissing
          ? t('hostrecords:coverage.noneAtAll', { total: all.length })
          : t('hostrecords:coverage.partial', {
              total: all.length,
              noProject,
              noModule,
            })}
      </p>
      <p className="mt-0.5 text-xs text-muted-foreground">{t('hostrecords:coverage.next')}</p>
    </div>
  )
}
