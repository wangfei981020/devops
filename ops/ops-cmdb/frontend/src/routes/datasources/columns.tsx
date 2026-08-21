import { type Locale, formatRelativeTime, useTranslation } from '@ops/i18n'
import { Badge, type ColumnDef } from '@ops/ui'
import { Info } from 'lucide-react'
import type { DataSource } from './queries.js'

type TFn = (key: string, opts?: Record<string, unknown>) => string

export function dataSourceColumns(t: TFn): ColumnDef<DataSource>[] {
  return [
    {
      accessorKey: 'kind',
      header: t('datasources:column.kind'),
      cell: ({ row }) => <Badge tone="mute">{t(`datasources:kind.${row.original.kind}`)}</Badge>,
    },
    {
      accessorKey: 'name',
      header: t('datasources:column.name'),
      cell: ({ row }) => (
        <div className="flex min-w-0 flex-col">
          <span className="truncate font-medium text-foreground">{row.original.name}</span>
          {/* ⚠️ type 要能看出「接入方式不一样」。
              实测这一列里三个集群是 gke、一个是 generic ——
              后者用的是 kubeconfig 而不是云账号的 SA，
              而界面上只有一个小写单词，看不出这是两种东西（031 P2-57）。
              别名混排（其余三个是原名、它是「开发环境集群」）也在这里露出来，
              但显示名是用户自己设的，我们不该替他改回原名，
              能做的是把 type 的含义说清楚 */}
          <span
            className="font-mono text-xs text-muted-foreground"
            title={t(`datasources:typeHint.${row.original.type}`) !== `datasources:typeHint.${row.original.type}`
              ? t(`datasources:typeHint.${row.original.type}`)
              : undefined}
          >
            {row.original.type}
          </span>
        </div>
      ),
    },
    {
      id: 'credential',
      header: t('datasources:column.credential'),
      cell: ({ row }) => {
        const d = row.original
        // ⚠️ 只显示"配没配"。凭据内容接口本来就不返回（CONVENTIONS §3.4.2）
        if (!d.enabled) return <span className="text-xs text-muted-foreground">—</span>
        // 🔴 这一列描述的是**事实**（存没存），不是判断。
        // 标红要看 health —— 没存凭据在两种情况下完全正常：
        // GKE 从云账号继承、无鉴权的内网端点
        if (d.credential === 'inherited') {
          return (
            <span className="text-xs text-muted-foreground" title={t('datasources:inheritedHint')}>
              {t('datasources:inherited')}
            </span>
          )
        }
        if (d.has_credential) {
          return <span className="text-xs text-muted-foreground">{t('datasources:configured')}</span>
        }
        // ⚠️ 措辞必须是**同一个维度上的三个值**：已配置 / 未配置 / 继承云账号。
        //
        //	原来这里是「已配置」和「未配令牌」——一个说"配置"一个说"令牌"，
        //	看起来像两件不同的事，而它们是同一格的两个取值（031 P2-56）。
        //
        //	"未配置"这件事本身是不是问题，由旁边的「状态」列回答，
        //	不由这一列的措辞暗示 —— 这一列只陈述事实。
        return d.health === 'no_credential' ? (
          <Badge tone="bad">{t('datasources:noCredential')}</Badge>
        ) : (
          <span
            className="text-xs text-muted-foreground"
            title={
              d.cred_required === false ? t('datasources:notConfiguredOkHint') : undefined
            }
          >
            {t('datasources:notConfigured')}
          </span>
        )
      },
    },
    {
      id: 'lastSync',
      header: t('datasources:column.lastSync'),
      cell: ({ row }) => <LastSync ds={row.original} />,
    },
    {
      id: 'state',
      header: t('datasources:column.state'),
      cell: ({ row }) => {
        const d = row.original
        // 判据全部来自后端算好的 health：前端再推一遍必然和后端分叉，
        // 而分叉的表现就是「状态红」和「2 分钟前同步成功」出现在同一行
        const tone =
          d.health === 'no_credential'
            ? 'bad'
            : d.health === 'stale'
              ? 'warn'
              : d.health === 'never_synced'
                ? 'warn'
                : d.health === 'disabled'
                  ? 'mute'
                  : // ⚠️ `not_applicable`（观测端点没有同步记录）是**正常状态**，
                    // 必须用中性色。原来它和 never_synced 共用橙色，
                    // 于是同一行里颜色在报警、文字在安抚（P1-61）
                    d.health === 'not_applicable'
                    ? 'mute'
                    : 'ok'
        // ⚠️ 说明收进 tooltip，不在每一行铺开。
        //
        //	实测「观测端点是被查询时才用的」这段文字在页面上重复了 **6 次**，
        //	每条「从未同步」行下面完整重复一遍 —— 表格变成一堵文字墙，
        //	而真正的差异（数据源名、凭据状态）反而被淹没（P1-60）。
        //	说明本身是对的，问题在于它按行重复而不是按类出现一次。
        return (
          <div className="flex items-center gap-1">
            <span title={d.health_note || undefined}>
              <Badge tone={tone}>{t(`datasources:health.${d.health ?? 'ok'}`)}</Badge>
            </span>
            {d.health_note ? (
              <span title={d.health_note} className="inline-flex">
                <Info
                  className="size-3 shrink-0 cursor-help text-muted-foreground"
                  aria-label={d.health_note}
                />
              </span>
            ) : null}
            {/*
              ⚠️ 要确认观测端点通不通，得去「观测端点」页点「测试连通性」——
              而这一页是纯只读的（11 行 0 个按钮）。
              原来的做法是在说明里写「请点『测试连通性』」，但这一页没有那个按钮，
              人会在本页白找一圈（P1-59）。
              **给路就给能点的**：这里放一个真的能跳过去的链接。
            */}
            {d.kind === 'obs' ? (
              <a
                href="/admin/obs-endpoints"
                className="shrink-0 text-[11px] text-brand-text underline-offset-2 hover:underline"
              >
                {t('datasources:goTest')}
              </a>
            ) : null}
          </div>
        )
      },
    },
  ]
}

function LastSync({ ds }: { ds: DataSource }) {
  const { t, i18n } = useTranslation()
  if (!ds.last_sync_at) {
    return <span className="text-xs text-warning">{t('datasources:neverSynced')}</span>
  }
  return (
    <span className={`text-xs ${ds.stale ? 'text-warning' : 'text-muted-foreground'}`}>
      {formatRelativeTime(ds.last_sync_at, i18n.language as Locale)}
    </span>
  )
}
