import { toErrorInfo } from '@ops/api'
import { formatCurrency, formatNumber, tError, type Locale, useTranslation } from '@ops/i18n'
import { AsyncBoundary, type LoadError, Skeleton, fromQuery } from '@ops/ui'
import { Button } from '@ops/ui'
import { useState } from 'react'
import { WriteButton } from '../../components/WriteButton.js'
import { CostDetailDialog } from './CostDetailDialog.js'
import { type CostOverview, useCostOverview, useCostSnapshot } from './queries.js'

export function CostPage() {
  // 总览只回答「多少钱」。下钻回答「花在谁头上 / 为什么涨 / 哪条最贵 / 哪些白买了」
  const [drill, setDrill] = useState(false)
  const { t, i18n } = useTranslation()
  const locale = i18n.language as Locale
  const query = useCostOverview()
  const toLoadError = (e: unknown): LoadError => {
    const n = toErrorInfo(e)
    return { cause: tError(t, n.messageKey, n.params), detail: n.detail, retryable: n.retryable, kind: n.kind === 'license' ? 'license' : undefined, feature: n.params?.feature as string | undefined }
  }

  return (
    <div className="mx-auto max-w-[900px] p-5">
      {/* ⚠️ 快照入口在 AsyncBoundary 外面：一份快照都没有时报表是空的，
          而那正是最需要点这个按钮的时候 */}
      <div className="mb-3 flex items-center gap-2">
        <span className="min-w-0 flex-1 text-xs text-muted-foreground">
          {t('cost:snapshot.hint')}
        </span>
        <Button size="sm" onClick={() => setDrill(true)}>
          {t('cost:drill.entry')}
        </Button>
        <SnapshotButton t={t} />
      </div>
      <AsyncBoundary
        state={fromQuery<CostOverview>(query, () => false, toLoadError)}
        errorTitle={t('cost:error.title')}
        retryLabel={t('common:action.retry')}
        onRetry={() => void query.refetch()}
        pending={
          <div className="flex flex-col gap-3">
            {[50, 80, 70].map((w) => (
              <Skeleton key={w} className="h-5" style={{ width: `${w}%` }} />
            ))}
          </div>
        }
        empty={null}
      >
        {(d) => (
          <div className="flex flex-col gap-5">
            <section className="rounded-[var(--radius-lg)] border border-border bg-card p-4">
              <div className="flex items-baseline gap-2.5">
                <span className="tabular text-2xl font-semibold text-foreground">
                  {formatCurrency(d.total_monthly, locale)}
                </span>
                <span className="text-xs text-muted-foreground">{t('cost:perMonth')}</span>
              </div>
              {/* ⚠️ "估算"必须紧贴数字，不能只写在页脚。
                  客户会拿它和云账单对，对不上就来问 */}
              <p className="mt-1.5 text-xs leading-relaxed text-muted-foreground">
                {t('cost:estimateHint')}
              </p>
              {d.unpriced_hosts > 0 ? (
                // 算不出来的机器按 0 计进了总额 —— 必须说，否则总额偏低而没人质疑
                <p className="mt-1.5 text-xs text-warning">
                  {t('cost:unpricedNote', { count: d.unpriced_hosts })}
                </p>
              ) : null}
              {d.destroyed_excluded > 0 ? (
                <p className="mt-1 text-xs text-muted-foreground">
                  {t('cost:destroyedNote', { count: d.destroyed_excluded })}
                </p>
              ) : null}
              {/*
                ⚠️ 「按默认档估」必须显示出来。
                这才是这一页真正的静默降级 —— 不是"算成 0"（那个很醒目），
                而是**用一个可能不对的价格算出一个看起来正常的数**（P1-47）。
                实测 europe-west3 没有费率行，那台机器按 default 档估出 141.05/月，
                而真实价格差多少无人知晓，`unpriced_hosts` 还显示 0。
              */}
              {(d.fallback_priced_hosts ?? 0) > 0 ? (
                <p className="mt-1 text-xs text-warning">
                  {t('cost:fallbackNote', {
                    count: d.fallback_priced_hosts,
                    regions: (d.fallback_regions ?? []).join('、') || '—',
                  })}
                </p>
              ) : null}
            </section>

            <Breakdown
              title={t('cost:byProject')}
              rows={d.by_project}
              total={d.total_monthly}
              locale={locale}
              t={t}
              unattributedHint={t('cost:unattributedProject')}
            />
            <Breakdown
              title={t('cost:byEnv')}
              rows={d.by_env}
              total={d.total_monthly}
              locale={locale}
              t={t}
              unattributedHint={t('cost:unattributedEnv')}
            />
          </div>
        )}
      </AsyncBoundary>
      {drill ? <CostDetailDialog onClose={() => setDrill(false)} t={t} /> : null}
    </div>
  )
}

/**
 * 分摊明细。
 *
 * # ⚠️ 空 key 是「未归属」，不是「—」
 *
 * 「业务项目」（成本归属用的字典）和主机上的 `project`（GCP 云项目）是**两个概念**。
 * 业务项目字典是空的时候，后端把全部成本归进一个 key 为空串的分组 ——
 * **这是正确行为**，不是读错了列。
 *
 * 但原来这里渲染成 `{r.key || '—'}` + 一根占满 100% 的进度条：
 * 一个匿名的「—」占掉全部成本，看不出这是"还没归属"，也不知道下一步该干什么
 * （P1-57 / P2-43）。
 *
 * 正确写法是把话说完：叫它「未归属」，并指出去哪里建业务项目 ——
 * 就像基础配置页那个空态做的那样（031 把那一条评为正面样板）。
 *
 * # ⚠️ 只有一个分组时不画进度条
 *
 * 进度条必然满格，不传达任何信息，反而让人以为"这里有对比"。
 */
function Breakdown({
  title,
  rows,
  total,
  locale,
  t,
  unattributedHint,
}: {
  title: string
  rows: { key: string; monthly: number; count: number }[]
  total: number
  locale: Locale
  t: (k: string, o?: Record<string, unknown>) => string
  /** 空 key 那一行下面要说的话（不同分组指向不同的配置项） */
  unattributedHint?: string
}) {
  // 一个分组时进度条恒满格，画了等于没画
  const showBar = rows.length > 1
  const allUnattributed = rows.length === 1 && !rows[0]?.key
  return (
    <section className="rounded-[var(--radius-lg)] border border-border bg-card p-4">
      <h2 className="text-sm font-semibold text-foreground">{title}</h2>
      {/* 全部落在「未归属」时，整节开头就说清楚 —— 别让人自己从一行「—」里推 */}
      {allUnattributed && unattributedHint ? (
        <p className="mt-1.5 text-xs leading-relaxed text-warning">{unattributedHint}</p>
      ) : null}
      <div className="mt-3 flex flex-col gap-1.5">
        {rows.map((r) => {
          // 总额为 0 时不画条：除以 0 会得到 NaN，而 NaN% 的宽度渲染出来是满条，
          // 看起来像"这一项占了全部"
          const pct = total > 0 ? (r.monthly / total) * 100 : 0
          const unattributed = !r.key
          return (
            <div key={r.key || '__unattributed__'} className="flex flex-col">
              <div className="flex items-center gap-3 text-[13px]">
                <span
                  className={`w-[140px] shrink-0 truncate ${
                    unattributed ? 'text-warning' : 'text-foreground'
                  }`}
                >
                  {/* 「未归属」是一个**结论**，「—」只是一个占位符 */}
                  {r.key || t('cost:unattributed')}
                </span>
                {showBar ? (
                  <span className="h-1.5 flex-1 overflow-hidden rounded-full bg-border">
                    <span className="block h-full bg-primary" style={{ width: `${pct}%` }} />
                  </span>
                ) : (
                  <span className="flex-1" />
                )}
                <span className="tabular w-[92px] shrink-0 text-right">
                  {formatCurrency(r.monthly, locale)}
                </span>
                <span className="tabular w-[52px] shrink-0 text-right text-xs text-muted-foreground">
                  {t('cost:hosts', { count: r.count })}
                </span>
              </div>
              {/* 混在多个分组里的那一条「未归属」也要指路 */}
              {unattributed && !allUnattributed && unattributedHint ? (
                <p className="mt-0.5 text-[11px] text-muted-foreground">{unattributedHint}</p>
              ) : null}
            </div>
          )
        })}
      </div>
    </section>
  )
}

/** 立即打快照。结果就地显示 —— 打完几条也是有用的信息 */
function SnapshotButton({ t }: { t: (k: string, p?: Record<string, unknown>) => string }) {
  const snap = useCostSnapshot()
  return (
    <>
      {snap.isError ? (
        <span className="max-w-[240px] truncate text-xs text-danger" title={toErrorInfo(snap.error).detail || undefined}>
          {tError(t, toErrorInfo(snap.error).messageKey, toErrorInfo(snap.error).params)}
        </span>
      ) : snap.isSuccess ? (
        <span className="text-xs text-success">
          {snap.data?.msg ?? t('cost:snapshot.done', { count: snap.data?.count ?? 0 })}
        </span>
      ) : null}
      <WriteButton
        perm="cmdb:manage_cost_rates"
        size="sm"
        loading={snap.isPending}
        onClick={() => snap.mutate(undefined)}
      >
        {t('cost:snapshot.action')}
      </WriteButton>
    </>
  )
}
