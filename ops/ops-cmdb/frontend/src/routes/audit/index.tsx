import { toErrorInfo } from '@ops/api'
import { formatDateTime, tError, type Locale, useTranslation } from '@ops/i18n'
import {
  AsyncBoundary,
  Badge,
  DataTable,
  EmptyState,
  type ColumnDef,
  Pagination,
  type LoadError,
  SearchInput,
  Select,
  TableSkeleton,
  fromQuery,
} from '@ops/ui'
import { useNavigate, useSearch } from '@tanstack/react-router'
import { FileSearch } from 'lucide-react'
import { useMemo, useState } from 'react'
import { type AuditLog, useAuditLogs } from './queries.js'
import { DetailDialog } from './DetailDialog.js'

export function AuditPage() {
  const [detailFor, setDetailFor] = useState<AuditLog | null>(null)
  const { t, i18n } = useTranslation()
  const locale = i18n.language as Locale
  const {
    q: keyword,
    page,
    size,
    actor_source: actorSource,
    status: statusFilter,
    username: userFilter,
    target_type: targetType,
    hours,
    changed_only: changedOnly,
  } = useSearch({ from: '/admin/audit' })
  const navigate = useNavigate({ from: '/admin/audit' })
  // 改任何筛选条件都要回到第 1 页：留在第 7 页会看到一张空表，
  // 而"空"在这一页会被读成"没有这样的记录"
  const patch = (next: Record<string, string | number>) =>
    void navigate({ search: (s) => ({ ...s, ...next, page: 1 }) })
  // ⚠️ 参数名必须是 `page` / `size`。
  //
  //	原来传的是 `limit: 200` —— 后端压根不认 `limit`（它读 page / size / page_size），
  //	于是每次都返回默认的 10 条。界面因此固定显示 10 行、右上角写着「共 258 条」，
  //	248 条审计记录在界面上完全不可达（OPSCMDB-031 P0-23）。
  //
  //	这不是"分页没实现"，是**参数名对不上而后端有默认值兜底** ——
  //	不报错、不空，只是永远给你前 10 条。
  //	对审计而言这是功能性失效：审计的用途就是事后追溯，只能看最近 10 条等于没有审计。
  //	而它还和「审计保留天数=0（永不清理）」叠在一起：数据永久累积，界面永远只给 10 条。
  // 🔴 时间窗直接把**小时数**发给后端，前端不做任何时区换算。
  //
  //	原本想在这里算好绝对时间发 `since`，那会差 8 小时：
  //	`audit_logs.at` 是 DATETIME，而 DSN 里 time_zone='+08:00'，
  //	库里存的是马尼拉时间；浏览器 `toISOString()` 给的是 UTC，
  //	MySQL 还解析不了尾巴上的 `.000Z`。两个问题都**不报错**，
  //	只会把「近 6 小时」悄悄变成另一个时间窗。
  //	后端用 NOW() 在库里算，会话时区与列天然一致。
  const query = useAuditLogs({
    q: keyword,
    page,
    size,
    actor_source: actorSource,
    status: statusFilter,
    username: userFilter,
    target_type: targetType,
    // ⚠️ 0 = 不限，这时**不能发**这个参数：
    //	useAuditLogs 只滤掉 '' 和 undefined，数字 0 会照发成 `hours=0`，
    //	后端解析出 0 就打一条「hours 不是正整数」的 WARN ——
    //	默认状态每次查询都刷一条告警，是把噪音塞进日志，
    //	而日志里的噪音会让人开始忽略真正的 WARN。
    hours: hours > 0 ? hours : '',
    changed_only: changedOnly,
  })

  const columns = useMemo<ColumnDef<AuditLog>[]>(
    () => [
      {
        id: 'at',
        header: t('audit:column.at'),
        cell: ({ row }) => (
          // ⚠️ 审计用**绝对时间**，不用"3 分钟前"。
          // 审计的用途是"当时到底几点发生的"，相对时间在复盘时毫无价值
          <span className="tabular text-xs text-muted-foreground">
            {formatDateTime(row.original.at, locale, { withSeconds: true })}
          </span>
        ),
      },
      {
        accessorKey: 'username',
        header: t('audit:column.who'),
        cell: ({ row }) => {
          const r = row.original
          // 🔴 来源要在行上看得见，不能只做成筛选条件。
          //	「AI 干的还是人干的」是扫一眼列表就该分辨出来的事 ——
          //	要先想到去筛才看得到，等于这条信息在默认视图里不存在。
          //	mcp 用警示色：机器身份不受 RBAC 约束，它的写操作值得多看一眼。
          const src = r.actor_source
          const tone = src === 'mcp' ? 'warn' : src === 'system' ? 'info' : 'mute'
          return (
            <div className="flex min-w-0 flex-col gap-0.5">
              <div className="flex min-w-0 items-center gap-1.5">
                <span className="truncate text-[13px] text-foreground">
                  {/* 用户名可能为空（MCP 早期记录），空着比显示"—"更容易被当成渲染坏了 */}
                  {r.username || t('common:state.unknown')}
                </span>
                {src ? (
                  <Badge tone={tone}>{t(`audit:source.${src}`, { defaultValue: src })}</Badge>
                ) : null}
              </div>
              <span className="truncate font-mono text-[11px] text-muted-foreground">{r.ip}</span>
            </div>
          )
        },
      },
      {
        accessorKey: 'action',
        header: t('audit:column.action'),
        cell: ({ row }) => (
          <div className="flex min-w-0 flex-col">
            {/* 动作码原样显示：cloud_account.create 是可以直接拿去 grep 的 */}
            <span className="font-mono text-xs">{row.original.action}</span>
            {/* method + path 补在下面：光看动作码不知道调的是哪个接口，
                而排障时"哪个接口"往往比"什么动作"更直接 */}
            {row.original.method || row.original.path ? (
              <span className="truncate font-mono text-[11px] text-muted-foreground">
                {row.original.method} {row.original.path}
              </span>
            ) : null}
          </div>
        ),
      },
      {
        accessorKey: 'target',
        header: t('audit:column.target'),
        cell: ({ row }) => (
          <span className="truncate text-[13px]" title={row.original.target}>
            {row.original.target || row.original.target_type || '—'}
          </span>
        ),
      },
      {
        id: 'status',
        header: t('audit:column.status'),
        cell: ({ row }) => {
          const r = row.original
          if (r.status === 'success') return <Badge tone="ok">{r.status}</Badge>
          // ⚠️ accepted 既不是成功也不是失败：接口返回 202，活儿在后台跑，
          // 结果一两分钟后才落到执行记录里。
          // 渲染成绿色会骗人（同步其实 401 了），渲染成红色也骗人（它可能正在正常跑）——
          // 所以给中性色 + 一句"去哪看真结果"。
          if (r.status === 'accepted') {
            return (
              <div className="flex min-w-0 flex-col items-start gap-0.5">
                <Badge tone="info">{r.status}</Badge>
                <span className="max-w-[220px] text-[11px] text-muted-foreground">
                  {t('audit:acceptedNote')}
                </span>
              </div>
            )
          }
          return (
            <div className="flex min-w-0 flex-col items-start gap-0.5">
              <Badge tone="bad">{r.status || t('common:state.unknown')}</Badge>
              {/* 失败原因要显示：一条"失败"而不说为什么的审计等于没记 */}
              {r.error_msg ? (
                <span className="max-w-[220px] truncate text-[11px] text-danger" title={r.error_msg}>
                  {r.error_msg}
                </span>
              ) : null}
            </div>
          )
        },
      },
      {
        id: 'detail',
        header: t('audit:column.detail'),
        // ⚠️ **每一行都要能点开**。
        //
        // 原来只有 change_count > 0 才可点，于是所有动作型操作
        // （同步、续费、测连通、登录、MCP 调用）在这一列是个死的「—」。
        // 而它们恰恰是最需要看详情的那批 —— 一次同步到底调了哪个接口、
        // 凭什么权限、跑了多久、报了什么错，后端全都记着，只是没地方看。
        cell: ({ row }) => (
          <button
            type="button"
            onClick={() => setDetailFor(row.original)}
            className="cursor-pointer text-[13px] text-brand-text underline-offset-2 hover:underline"
          >
            {row.original.change_count > 0
              ? t('audit:detail.entryWithCount', { count: row.original.change_count })
              : t('audit:detail.entry')}
          </button>
        ),
      },
    ],
    [t, locale],
  )

  const filtered =
    keyword !== '' ||
    actorSource !== '' ||
    statusFilter !== '' ||
    userFilter !== '' ||
    targetType !== '' ||
    changedOnly !== '' ||
    hours > 0

  const toLoadError = (e: unknown): LoadError => {
    const n = toErrorInfo(e)
    return { cause: tError(t, n.messageKey, n.params), detail: n.detail, retryable: n.retryable, kind: n.kind === 'license' ? 'license' : undefined, feature: n.params?.feature as string | undefined }
  }

  return (
    <div className="flex flex-col">
      {/* ⚠️ 工具条在 AsyncBoundary 外面：筛出 0 条时改条件的搜索框不能跟着消失 */}
      <div className="flex flex-wrap items-center gap-2 border-b border-border px-4 py-3">
        <SearchInput
          value={keyword}
          onChange={(v) => patch({ q: v })}
          placeholder={t('audit:filter.searchPlaceholder')}
          clearLabel={t('common:filter.clearSearch')}
          className="w-[260px]"
        />
        {/* 🔴 来源筛选是这一页最要紧的一维。
            MCP 是机器身份、不受 RBAC 约束，「AI 都干了什么」是把 MCP 放进生产前
            必须能随时回答的问题 —— 而在这之前 AI 的写操作和人的写操作是混在一起的。 */}
        <Select<string>
          label={t('audit:filter.source')}
          value={actorSource}
          onChange={(v) => patch({ actor_source: v })}
          options={[
            { value: '', label: t('common:filter.all') },
            // ⚠️ 这四个值来自 handlers/common.go 的 actorSourceOf，不是我编的枚举。
            //	多写一个后端不产出的值 = 一个永远筛出 0 条的选项。
            { value: 'mcp', label: t('audit:source.mcp') },
            { value: 'local', label: t('audit:source.local') },
            { value: 'portal', label: t('audit:source.portal') },
            { value: 'system', label: t('audit:source.system') },
          ]}
        />
        <Select<string>
          label={t('audit:filter.status')}
          value={statusFilter}
          onChange={(v) => patch({ status: v })}
          options={[
            { value: '', label: t('common:filter.all') },
            { value: 'success', label: t('audit:status.success') },
            { value: 'fail', label: t('audit:status.fail') },
            // accepted 单独一档：它既不是成功也不是失败（202 受理，真结果在执行记录里）。
            // 并进 success 会把"同步其实 401 了"显示成绿色
            { value: 'accepted', label: t('audit:status.accepted') },
          ]}
        />
        <Select<string>
          label={t('audit:filter.window')}
          value={String(hours)}
          onChange={(v) => patch({ hours: Number(v) })}
          options={[
            { value: '0', label: t('audit:window.all') },
            { value: '6', label: t('audit:window.h6') },
            { value: '24', label: t('audit:window.h24') },
            { value: '168', label: t('audit:window.d7') },
            { value: '720', label: t('audit:window.d30') },
          ]}
        />
        <Select<string>
          label={t('audit:filter.changed')}
          value={changedOnly}
          onChange={(v) => patch({ changed_only: v })}
          options={[
            { value: '', label: t('common:filter.all') },
            // 动作型操作（同步/续费/测连通）没有字段变更，
            // 查"谁改了配置"时它们是噪音
            { value: '1', label: t('audit:changed.only') },
          ]}
        />
        {/* 记什么、不记什么要说准 —— 说明与行为不符会让人把"查不到"当成"没发生" */}
        <span className="text-xs text-muted-foreground">{t('audit:writeOnlyNote')}</span>
        {query.data ? (
          <span className="ml-auto text-xs text-muted-foreground">
            {t('audit:total', { count: query.data.total })}
          </span>
        ) : null}
      </div>

      <AsyncBoundary
        // 空的判据用本页条数，不用 total：翻到超出范围的页码时 total 仍是 258，
        // 那会走进"有数据"分支渲染一张空表
        state={fromQuery<{ items: AuditLog[]; total: number }>(
          query,
          (d) => d.items.length === 0,
          toLoadError,
        )}
        errorTitle={t('audit:error.title')}
        retryLabel={t('common:action.retry')}
        onRetry={() => void query.refetch()}
        pending={
          <div className="p-5">
            <TableSkeleton columns={[16, 14, 20, 24, 14, 8]} rows={8} />
          </div>
        }
        empty={
          <EmptyState
            icon={<FileSearch />}
            title={t('audit:empty.title')}
            // ⚠️ 判据必须涵盖**所有**筛选维度。只看 keyword 的话，
            //	用「来源=mcp」筛出 0 条时会说「还没有任何审计记录」——
            //	那是在说"没发生过"，而事实是"这个条件下没有"。
            //	差别很大：前者会让人停止排查。
            reason={filtered ? t('audit:empty.filtered') : t('audit:empty.none')}
            action={
              filtered
                ? {
                    label: t('common:filter.clearAll'),
                    onClick: () =>
                      void navigate({
                        search: {
                          q: '',
                          page: 1,
                          size,
                          actor_source: '',
                          status: '',
                          username: '',
                          target_type: '',
                          hours: 0,
                          changed_only: '',
                        },
                      }),
                  }
                : null
            }
          />
        }
      >
        {(data) => (
          <>
            <DataTable data={data.items} columns={columns} rowKey={(r) => String(r.id)} />
            {/* 本产品其它列表页都有分页，唯独审计日志没有 —— 那是遗漏不是取舍 */}
            <Pagination
              page={page}
              size={size}
              total={data.total}
              onPage={(p) => void navigate({ search: (s) => ({ ...s, page: p }) })}
              onSize={(n) => void navigate({ search: (s) => ({ ...s, size: n, page: 1 }) })}
              rangeLabel={(f, to, total) => t('common:pagination.range', { from: f, to, total })}
              totalLabel={(total) => t('common:pagination.total', { count: total })}
              perPageLabel={t('common:pagination.perPage')}
              prevLabel={t('common:pagination.prev')}
              nextLabel={t('common:pagination.next')}
            />
          </>
        )}
      </AsyncBoundary>

      {detailFor ? (
        <DetailDialog log={detailFor} onClose={() => setDetailFor(null)} />
      ) : null}
    </div>
  )
}
