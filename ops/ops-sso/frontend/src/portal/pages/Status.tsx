import { useTranslation } from '@ops/i18n'
import { AsyncBoundary, EmptyState, Skeleton, fromQuery } from '@ops/ui'
import { useQuery } from '@tanstack/react-query'
import { Activity } from 'lucide-react'
import { useMemo } from 'react'
import { api } from '../../api/client.js'
import type { ListOf } from '../../api/types.js'
import { makeToLoadError } from '../../shared/errorText.js'

interface StatusRow {
  id: number
  code: string
  name: string
  env: string
  icon_text: string
  icon_color: string
  allowed: boolean
  state: 'ok' | 'failing' | 'never' | 'uncovered'
  detail?: string
  checked_at?: string
}

/**
 * 服务状态：我能用的系统现在是不是还活着。
 *
 * # 这一页要替代的东西
 *
 * 「是不是只有我进不去」——没有这一页的话，答案只能靠在群里喊一声。
 * 而且喊的人越多，越像是系统挂了，实际上可能只是他一个人的问题。
 *
 * # 显示的是探针最近一次的结果，不是现场探测
 *
 * 现场探测会把门户变成放大器（一百个人刷新 = 一百轮对所有下游的请求），
 * 也等于给任何登录用户一个从服务端发起内网请求的入口。
 * 所以这里只把后台按固定频率跑出来的结论拿出来看 ——
 * 代价是有延迟，页面必须把「什么时候测的」一起显示出来。
 */
export function Status() {
  const { t } = useTranslation()
  const toLoadError = useMemo(() => makeToLoadError(t), [t])
  const q = useQuery({
    queryKey: ['portal-status'],
    queryFn: () => api.get<ListOf<StatusRow>>('/portal/status'),
  })
  const state = fromQuery<ListOf<StatusRow>>(q, (d) => d.items.length === 0, toLoadError)

  return (
    <>
      <h1 className="text-xl font-semibold tracking-tight">{t('sso:portal.status')}</h1>
      <p className="mt-1 mb-5 max-w-[80ch] text-sm text-muted-foreground">
        {t('sso:status.intro')}
      </p>

      <AsyncBoundary
        state={state}
        errorTitle={t('sso:status.errorTitle')}
        retryLabel={t('retry')}
        onRetry={() => void q.refetch()}
        pending={
          <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
            {[0, 1, 2].map((i) => (
              <Skeleton key={i} className="h-20 w-full" />
            ))}
          </div>
        }
        empty={
          <EmptyState
            icon={<Activity />}
            title={t('sso:status.empty.title')}
            reason={t('sso:status.empty.reason')}
            action={null}
          />
        }
      >
        {(d) => (
          <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
            {d.items.map((s) => (
              <Card key={s.id} row={s} />
            ))}
          </div>
        )}
      </AsyncBoundary>
    </>
  )
}

function Card({ row }: { row: StatusRow }) {
  const { t } = useTranslation()
  // 四态各有各的颜色。⚠️「未覆盖」绝不能画成绿色 ——
  // 那是拿"我们没在看"冒充"没问题"。
  const tone = {
    ok: 'border-success bg-success-bg text-success',
    failing: 'border-destructive bg-danger-bg text-danger',
    never: 'border-warning bg-warning-bg text-warning',
    uncovered: 'border-border bg-muted text-muted-foreground',
  }[row.state]

  return (
    <div className="rounded-[var(--radius-md)] border border-border bg-card p-3.5">
      <div className="flex items-center gap-2.5">
        <span
          className="grid size-7 shrink-0 place-items-center rounded-[var(--radius-sm)] text-[11px] font-semibold text-primary-foreground"
          style={{ background: row.icon_color }}
        >
          {row.icon_text}
        </span>
        <span className="min-w-0 flex-1">
          <b className="block truncate text-[13px] font-medium">{row.name}</b>
          <span className="text-[11px] text-muted-foreground">{row.env}</span>
        </span>
        <span className={`shrink-0 rounded-[var(--radius-sm)] border px-2 py-0.5 text-[11px] ${tone}`}>
          {t(`sso:status.state.${row.state}`)}
        </span>
      </div>

      {row.state === 'failing' && row.detail ? (
        <p className="mt-2 rounded-[var(--radius-sm)] bg-danger-bg px-2 py-1 text-[11px] text-danger">
          {row.detail}
        </p>
      ) : null}

      <p className="mt-2 text-[11px] text-muted-foreground">
        {row.checked_at
          ? t('sso:status.checkedAt', { at: fmt(row.checked_at) })
          : // 没有时间就说清没有。写「刚刚」或留空都会被读成"这是实时的"
            t('sso:status.neverChecked')}
      </p>

      {!row.allowed ? (
        // 服务活着 ≠ 你进得去。两件事混在一起，人会拿着"显示正常"
        // 去追为什么自己进不去。
        <p className="mt-1 text-[11px] text-muted-foreground">{t('sso:status.noAccess')}</p>
      ) : null}
    </div>
  )
}

function fmt(s: string) {
  const d = new Date(s)
  if (Number.isNaN(d.getTime())) return s
  const p = (n: number) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())} ${p(d.getHours())}:${p(d.getMinutes())}`
}
