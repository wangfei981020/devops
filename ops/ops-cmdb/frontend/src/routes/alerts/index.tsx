import { hintText } from '../../lib/hintText.js'
import { toErrorInfo } from '@ops/api'
import { tError, useTranslation } from '@ops/i18n'
import {
  AsyncBoundary,
  Banner,
  DataTable,
  EmptyState,
  Field,
  type LoadError,
  SearchInput,
  Select,
  TableSkeleton,
  fromQuery,
} from '@ops/ui'
import { useNavigate, useSearch } from '@tanstack/react-router'
import { AlertTriangle } from 'lucide-react'
import { useMemo } from 'react'
import { alertColumns } from './columns.js'
import { type AlertsData, useAlerts } from './queries.js'

export function AlertsPage() {
  const { t } = useTranslation()
  const { q: keyword, severity, state, hours, env } = useSearch({ from: '/runtime/alerts' })
  const navigate = useNavigate({ from: '/runtime/alerts' })
  const query = useAlerts({
    q: keyword,
    severity: severity ?? '',
    limit: 200,
    state,
    // hours 只对历史视图有意义，当前视图下后端不读它。
    // 仍然只在 history 时发：发一个用不上的参数会让 queryKey 无谓地变，
    // 切级别时白白多打一次请求
    hours: state === 'history' ? hours : '',
    env,
  })
  const columns = useMemo(() => alertColumns(t), [t])
  // ⚠️ 判据要涵盖**所有**筛选维度。只看关键词和级别的话，
  //	按 env 筛出 0 条时页面会说「当前很太平」——
  //	而真相是那个环境的告警根本没被取到。这一页把"看不见"说成"没有"
  //	的代价最直接：人会因此不去处理一个正在响的告警。
  const filtered =
    keyword !== '' || (severity ?? '') !== '' || env !== '' || state === 'history'
  const toLoadError = (e: unknown): LoadError => {
    const n = toErrorInfo(e)
    return { cause: tError(t, n.messageKey, n.params), detail: n.detail, retryable: n.retryable, kind: n.kind === 'license' ? 'license' : undefined, feature: n.params?.feature as string | undefined }
  }

  return (
    <div className="flex flex-col">
      {/*
        ⚠️ 工具条必须在 AsyncBoundary **外面**。
        放里面的话，空态/错误态会把整条工具条一起吞掉 —— 于是筛出 0 条之后，
        用来改条件的搜索框和级别下拉恰好在最需要它们的时候消失了，
        用户只能退出重进。这个坑在生产验证时已经踩过一次（按钮消失 ≠ 没权限）。
      */}
      <div className="flex flex-wrap items-center gap-2 border-b border-border px-4 py-3">
        <SearchInput
          value={keyword}
          onChange={(v) => void navigate({ search: (s) => ({ ...s, q: v }) })}
          placeholder={t('alerts:filter.searchPlaceholder')}
          clearLabel={t('common:filter.clearSearch')}
          className="w-[240px]"
        />
        <Field label={t('alerts:column.severity')}>
          <Select
            label={t('alerts:column.severity')}
            value={severity ?? ''}
            onChange={(v) => void navigate({ search: (s) => ({ ...s, severity: v }) })}
            options={[
              { value: '', label: t('alerts:filter.allSeverity') },
              { value: 'critical', label: 'critical' },
              { value: 'warning', label: 'warning' },
              { value: 'info', label: 'info' },
            ]}
          />
        </Field>
        {/* 当前 / 历史。旧版有这个切换，新版丢了 —— 于是「昨天那条告警什么时候恢复的」
            这类事后追溯完全做不了，而告警的价值一半在事后 */}
        <Field label={t('alerts:filter.state')}>
          <Select
            label={t('alerts:filter.state')}
            value={state}
            onChange={(v) => void navigate({ search: (s) => ({ ...s, state: v }) })}
            options={[
              { value: 'current', label: t('alerts:state.current') },
              { value: 'history', label: t('alerts:state.history') },
            ]}
          />
        </Field>
        {/* 时间窗只在历史视图下出现：当前告警没有"时间窗"这个概念，
            让它常驻会让人以为当前视图也被时间过滤了 */}
        {state === 'history' ? (
          <Field label={t('alerts:filter.window')}>
            <Select
              label={t('alerts:filter.window')}
              value={String(hours)}
              onChange={(v) => void navigate({ search: (s) => ({ ...s, hours: Number(v) }) })}
              options={[
                { value: '6', label: t('alerts:window.h6') },
                { value: '24', label: t('alerts:window.h24') },
                { value: '168', label: t('alerts:window.d7') },
                { value: '720', label: t('alerts:window.d30') },
              ]}
            />
          </Field>
        ) : null}
        {/* 🔴 环境选择器只在**真的有多个接入点**时出现。
            选项来自后端的 envs（实际配了 n9e 的环境），不硬编码 ——
            硬编码一份的话，选到没配的环境会静默返回空，
            而空在这一页会被读成「这个环境很安静」。 */}
        {/* 取不到环境清单时说出来，而不是把选择器藏掉当作"没有环境" */}
        {query.data?.envsError ? (
          <span className="text-xs text-warning" title={query.data.envsError}>
            {t('alerts:filter.envsUnavailable')}
          </span>
        ) : null}
        {(query.data?.envs.length ?? 0) > 0 ? (
          <Field label={t('alerts:filter.env')}>
            <Select
              label={t('alerts:filter.env')}
              value={env}
              onChange={(v) => void navigate({ search: (s) => ({ ...s, env: v }) })}
              options={[
                { value: '', label: t('alerts:filter.allEnv') },
                ...(query.data?.envs ?? []).map((e) => ({ value: e, label: e })),
              ]}
            />
          </Field>
        ) : null}
        <span className="text-xs text-muted-foreground">{t('alerts:sourceNote')}</span>
        {query.data ? (
          <span className="ml-auto flex items-center gap-2 text-xs text-muted-foreground">
            {/* 取回数和夜莺侧总数分开说：只报一个数会被当成"线上就这些" */}
            <span>
              {/* 用后端给的 returned 而不是 items.length：两者理应相等，
                  不等就说明前端在某处又筛了一道而没说 —— 那种静默过滤是本仓最忌讳的 */}
              {t('alerts:count', { shown: query.data.returned, total: query.data.total })}
            </span>
            {/* 🔴 被筛掉多少要单独说。
                只报"显示 3 / 共 292"的话，中间少掉的 289 条到底是被级别筛掉的、
                被关键词筛掉的，还是被 limit 截断的，界面上完全分不出来 ——
                而这三种情况下一步动作完全不同（放宽级别 / 改关键词 / 加 limit）。 */}
            {query.data.filteredOut > 0 ? (
              <span className="text-warning">
                {t('alerts:filteredOut.severity', { n: query.data.filteredOut })}
              </span>
            ) : null}
            {query.data.kwFilteredOut > 0 ? (
              <span className="text-warning">
                {t('alerts:filteredOut.keyword', { n: query.data.kwFilteredOut })}
              </span>
            ) : null}
          </span>
        ) : null}
      </div>

      {/* 截断提示也在边界外：它说的是"你看到的不是全部"，空态时同样成立 */}
      {query.data?.truncated ? (
        <div className="px-4 pt-3">
          <Banner tone="warn">
            <span>{hintText(t, {
              hint_key: query.data.hintKey,
              hint: query.data.hint,
            }) || t('alerts:truncated')}</span>
          </Banner>
        </div>
      ) : null}

      <AsyncBoundary
        // 空的判据用 items.length，不用 total ——
        // total 是**夜莺侧的总数**，被 severity/关键词筛空时 total 仍然是 279，
        // 于是"筛出 0 条"会走进"有数据"分支渲染一张空表
        state={fromQuery<AlertsData>(query, (d) => d.items.length === 0, toLoadError)}
        errorTitle={t('alerts:error.title')}
        retryLabel={t('common:action.retry')}
        onRetry={() => void query.refetch()}
        pending={
          <div className="p-5">
            <TableSkeleton columns={[9, 22, 20, 22, 8, 11, 8]} rows={6} />
          </div>
        }
        empty={
          <EmptyState
            icon={<AlertTriangle />}
            title={t('alerts:empty.title')}
            // ⚠️ 「没有告警」「没接告警系统」「筛完是空」是三件事，下一步各不相同。
            //
            //	原来这里只看 keyword 二分：没输关键词就一律说"未接入告警系统"。
            //	接着两个后果同时出现——
            //	  · 生产接了夜莺、只是当前无告警时，页面说"没接入"，
            //	    会有人去改一个本来正确的配置；
            //	  · 按级别筛空时也说"没接入"，真相是 279 条告警一条都没被取到。
            //	后端的 configured / empty_reason 已经把答案给了，读它，别猜。
            reason={
              !query.data?.configured
                ? hintText(t, {
                    hint_key: query.data?.hintKey,
                    hint: query.data?.hint,
                  }) || t('alerts:empty.noSource')
                : query.data?.filteredEmpty || filtered
                  ? t('alerts:empty.filtered')
                  : t('alerts:empty.quiet')
            }
            action={
              filtered
                ? {
                    label: t('common:filter.clearAll'),
                    // ⚠️ 必须列全所有筛选维度。少写一个的话，
                    //	「清除全部」点完还留着那一维，人会以为清除按钮坏了。
                    //	state 回 current、hours 回默认 24 —— 这两个不是"清空"是"回默认"。
                    onClick: () =>
                      void navigate({
                        search: { q: '', severity: '', state: 'current', hours: 24, env: '' },
                      }),
                  }
                : null
            }
          />
        }
      >
        {(data) => (
          <DataTable
            data={data.items}
            columns={columns}
            // 老接口不保证有 id：拿规则名 + 触发时间兜底，比用数组下标稳（下标会在
            // 排序/筛选变化时错位，导致 React 复用错行）
            rowKey={(a) => String(a.id ?? `${a.rule_name}-${a.trigger_time}`)}
          />
        )}
      </AsyncBoundary>
    </div>
  )
}
