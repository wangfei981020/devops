import { type Locale, formatRelativeTime, useTranslation } from '@ops/i18n'
import { Badge, type BadgeTone, type ColumnDef } from '@ops/ui'
import type { Alert } from './queries.js'

type TFn = (key: string, opts?: Record<string, unknown>) => string

function tone(sev: string): BadgeTone {
  switch (sev.toLowerCase()) {
    case 'critical':
    case 'emergency':
    case '1':
      return 'bad'
    case 'warning':
    case '2':
      return 'warn'
    case 'info':
    case 'notice':
    case '3':
      return 'info'
    default:
      // 不认识的级别按最高处理：当成 info 会把上游新增的级别静默降级
      return 'bad'
  }
}

/**
 * 告警列表的列。
 *
 * # ⚠️ 字段名踩过的坑
 *
 * 这张表原来读的是 `summary` 和 `target`，而后端给的是 `rule_note` 和 `object`。
 * 结果：「摘要」列 279 行全是「—」，「对象」列压根不存在 ——
 * **后端数据完全正常，问题全在前端读错了名字**，而页面看起来只是"数据比较少"。
 *
 * 这已经是本项目第三次被"字段名对不上"咬到（前两次是包装对象 vs 裸数组）。
 * 根因是 168 个路由里只有 23 个有 swag 注解，类型只能手写。
 * 手写类型的唯一可靠办法：**先 curl 一次看真实响应**，别按字段名"应该叫什么"去推。
 */
export function alertColumns(t: TFn): ColumnDef<Alert>[] {
  return [
    {
      id: 'severity',
      header: t('alerts:column.severity'),
      // 级别原样透传：不同告警系统的命名不一样，翻译过来反而对不上他们的控制台
      cell: ({ row }) => (
        <Badge tone={tone(row.original.severity ?? '')}>{row.original.severity || '—'}</Badge>
      ),
    },
    {
      id: 'rule',
      header: t('alerts:column.rule'),
      cell: ({ row }) => (
        <div className="flex min-w-0 flex-col">
          <span
            className="truncate text-[13px] text-foreground"
            // 剥掉级别后缀后，把夜莺控制台里的原名放进 title，方便对照
            title={row.original.rule_name_raw || row.original.rule_name}
          >
            {row.original.rule_name || '—'}
          </span>
          {/* 责任方（告警组）：知道该找谁比知道哪台机器更省时间 */}
          <span className="truncate text-xs text-muted-foreground" title={row.original.group}>
            {row.original.group || ''}
          </span>
        </div>
      ),
    },
    {
      /**
       * 业务标签：env / project / team。
       *
       * 「这条告警归谁」是收到告警后第一个问题，而 group（告警组）只到组一级。
       * ⚠️ 只挑这三个键 —— tags 里还有 rulename/prometheus 等一堆内部标签，
       *	全铺出来会把真正有用的那三个淹掉。
       */
      id: 'owner',
      header: t('alerts:column.owner'),
      cell: ({ row }) => {
        const tg = row.original.tags ?? {}
        const parts = [tg.env, tg.project, tg.team].filter(Boolean)
        if (parts.length === 0) {
          // 空 = 这条告警没带业务标签，**不是**"没归属"。两者处置不同：
          // 前者去补规则的标签，后者去找人
          return <span className="text-[11px] text-muted-foreground">{t('alerts:noOwnerTag')}</span>
        }
        return (
          <div className="flex flex-wrap gap-1">
            {parts.map((x) => (
              <span key={x} className="rounded border border-border px-1 py-0.5 text-[11px]">
                {x}
              </span>
            ))}
          </div>
        )
      },
    },
    {
      /**
       * 业务标签：env / project / team。
       *
       * 「这条告警归谁」是收到告警后第一个问题，而 group（告警组）只到组一级。
       * ⚠️ 只挑这三个键 —— tags 里还有 rulename/prometheus 等一堆内部标签，
       *	全铺出来会把真正有用的那三个淹掉。
       */
      id: 'owner',
      header: t('alerts:column.owner'),
      cell: ({ row }) => {
        const tg = row.original.tags ?? {}
        const parts = [tg.env, tg.project, tg.team].filter(Boolean)
        if (parts.length === 0) {
          // 空 = 这条告警没带业务标签，**不是**"没归属"。两者处置不同：
          // 前者去补规则的标签，后者去找人
          return <span className="text-[11px] text-muted-foreground">{t('alerts:noOwnerTag')}</span>
        }
        return (
          <div className="flex flex-wrap gap-1">
            {parts.map((x) => (
              <span key={x} className="rounded border border-border px-1 py-0.5 text-[11px]">
                {x}
              </span>
            ))}
          </div>
        )
      },
    },
    {
      // ⚠️ 这一列是新加的。没有它，47 条视频流告警会渲染成 47 行一模一样的字 ——
      // 「哪条流出问题了」这个唯一有用的信息恰好是被省掉的那个
      id: 'object',
      header: t('alerts:column.object'),
      cell: ({ row }) => (
        <span
          className="truncate font-mono text-xs text-foreground"
          title={row.original.object}
        >
          {row.original.object || '—'}
        </span>
      ),
    },
    {
      id: 'summary',
      header: t('alerts:column.summary'),
      cell: ({ row }) => (
        <span className="truncate text-xs text-muted-foreground" title={row.original.rule_note}>
          {row.original.rule_note || '—'}
        </span>
      ),
    },
    {
      // 触发值：判断"多严重"靠的是它（磁盘 70% 还是 98%），不是靠级别标签
      id: 'value',
      header: t('alerts:column.value'),
      cell: ({ row }) => (
        <span className="tabular text-[13px] text-foreground">
          {/* ⚠️ 后端给的是字符串。空串要显示「—」，别当成 0 */}
          {row.original.trigger_value || '—'}
        </span>
      ),
    },
    {
      id: 'cluster',
      header: t('alerts:column.cluster'),
      cell: ({ row }) => (
        <div className="flex min-w-0 flex-col">
          {/* 取不到真集群就显示「—」，不拿数据源名冒充 */}
          <span className="truncate text-[13px]">{row.original.cluster || '—'}</span>
          <span className="truncate text-xs text-muted-foreground">
            {row.original.datasource || ''}
          </span>
        </div>
      ),
    },
    {
      id: 'time',
      header: t('alerts:column.time'),
      cell: ({ row }) => {
        const a = row.original
        return (
          <div className="flex min-w-0 flex-col">
            <TriggerTime at={a.trigger_time} />
            <NotifyCount n={a.notified} />
            {/* 🔴 恢复状态与恢复时刻。
                历史视图里这是**最要紧**的一列 ——「昨天那条告警什么时候恢复的」
                正是事后追溯要问的，而告警的价值一半在事后。
                ⚠️ 当前视图里 recovered 恒为 false（还没恢复的才叫"当前"），
                所以只在真的恢复了时才显示，不占位置。 */}
            {a.recovered ? (
              <span className="text-[11px] text-success">
                {a.recover_time
                  ? t('alerts:recoveredAt', { at: a.recover_time })
                  : t('alerts:recovered')}
              </span>
            ) : null}
          </div>
        )
      },
    },
  ]
}

function TriggerTime({ at }: { at?: string }) {
  const { t, i18n } = useTranslation()
  if (!at) return <span className="text-xs text-muted-foreground">{t('common:state.unknown')}</span>
  return (
    <span className="text-xs text-muted-foreground" title={at}>
      {formatRelativeTime(at, i18n.language as Locale)}
    </span>
  )
}

/**
 * 已通知次数。
 *
 * ⚠️ 这个数字的意义不是"通知了几次"，而是**这条告警响了多久还没人管**。
 * 实测最高一条已通知 4481 次 —— 那不是一条告警，那是一个长期没人处理的问题。
 * 所以数字大要显式标出来，不能和「通知 2 次」用同一个灰色。
 */
function NotifyCount({ n }: { n?: number }) {
  const { t } = useTranslation()
  // ⚠️ 三态：undefined = 没这个字段，0 = 确实一次没通知过。不能压成同一个显示
  if (n == null) return null
  const loud = n >= 100
  return (
    <span className={`tabular text-xs ${loud ? 'text-warning' : 'text-muted-foreground'}`}>
      {t('alerts:notified', { count: n })}
    </span>
  )
}
