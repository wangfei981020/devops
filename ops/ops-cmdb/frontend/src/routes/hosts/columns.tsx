import { type Locale, formatBytes, formatCurrency, formatRelativeTime } from '@ops/i18n'
import { Badge, type BadgeTone, type ColumnDef, NoValue, type NoValueKind } from '@ops/ui'
import type { Disk, Host, HostStatus } from './queries.js'

type TFn = (key: string, opts?: Record<string, unknown>) => string

const STATUS_TONE: Record<HostStatus, BadgeTone> = {
  running: 'ok',
  diskPressure: 'warn',
  notReady: 'bad',
  stopped: 'mute',
  // 已销毁用中性灰而不是红：它不是故障，是一个终态。
  // 用红色会让人以为出事了，然后浪费时间去查一台本就该消失的机器。
  destroyed: 'mute',
}

/** GB → 人类可读。走 1024 进制，和 df / kubectl 一致，否则同一块盘两处对不上。 */
function gb(size: number, locale: Locale): string {
  return formatBytes(size * 1024 ** 3, locale, 1)
}

export function buildColumns(
  t: TFn,
  locale: Locale,
  /**
   * 过期判据，来自后端（host_sync 的 cron 周期）。
   *
   * ⚠️ null / known=false 时**不标红**：取不到判据不等于数据是新的，
   * 但也不能凭空断言它是旧的。这时候只显示时间，让人自己判断
   * （OPSCMDB-031 P1-5）。
   */
  freshness: { staleAfterSeconds: number; known: boolean } | null,
): ColumnDef<Host, unknown>[] {
  const noValueLabels: Record<NoValueKind, string> = {
    na: t('hosts:value.notApplicable'),
    notIngested: t('common:state.notIngested'),
    stopped: t('hosts:value.billingStopped'),
    unknown: t('common:state.unknown'),
  }

  return [
    {
      accessorKey: 'name',
      header: t('hosts:column.name'),
      cell: (ctx) => <span className="font-medium text-foreground">{ctx.row.original.name}</span>,
    },
    {
      accessorKey: 'status',
      header: t('hosts:column.status'),
      cell: (ctx) => {
        const s = ctx.row.original.status
        return <Badge tone={STATUS_TONE[s]}>{t(`hosts:status.${s}`)}</Badge>
      },
    },
    {
      accessorKey: 'privateIp',
      header: t('hosts:column.privateIp'),
      cell: (ctx) => (
        // IP 用等宽：不等宽的话一列 IP 的点号位置全是错开的，扫视成本高
        <span className="font-mono text-xs text-muted-foreground">
          {ctx.row.original.privateIp}
        </span>
      ),
    },
    {
      id: 'cluster',
      header: t('hosts:column.cluster'),
      accessorFn: (h) => `${h.cluster}${h.nodePool ? ` / ${h.nodePool}` : ''}`,
      cell: (ctx) => {
        const h = ctx.row.original
        // 🔴 这一列以前显示的是 GCP **项目**（后端没给集群字段，前端拿 project 顶替）。
        //	项目和集群是两个维度，一个项目里可以有多个集群 —— 不报错、不为空，
        //	只是在回答另一个问题（OPSCMDB-037）。现在读真正的 cluster_name。
        //
        // ⚠️ 空要分两种：非 K8s 节点**本来就不属于任何集群**（正确的空），
        //	不能和"没采到"显示成一样。用 notApplicable 表示前者。
        return (
          <span>
            {h.cluster !== '' ? (
              h.cluster
            ) : (
              <span className="text-muted-foreground">{t('hosts:value.notApplicable')}</span>
            )}
            <span className="text-muted-foreground">
              {' / '}
              {h.nodePool ?? t('hosts:value.notApplicable')}
            </span>
          </span>
        )
      },
    },
    {
      // 项目 / 厂商：跨账号排障时"这台归谁管、在哪个云"。
      // 后端一直在返回 provider / project / project_name / account_name，
      // 前端此前一个字段都没声明（OPSCMDB-028 GAP-9）。
      id: 'project',
      header: t('hosts:column.project'),
      accessorFn: (h) => `${h.provider} ${h.projectName || h.project}`,
      cell: (ctx) => {
        const h = ctx.row.original
        return (
          <div className="flex min-w-0 flex-col">
            {/* 显示名优先、ID 兜底：GCP 的项目 ID 和显示名常常差很远 */}
            <span className="truncate text-[13px]" title={h.project}>
              {h.projectName || h.project || <NoValue kind="na" labels={noValueLabels} />}
            </span>
            <span className="truncate text-[11px] text-muted-foreground">
              {[h.provider, h.accountName].filter(Boolean).join(' · ')}
            </span>
          </div>
        )
      },
    },
    {
      // 区域 + 外网 IP。
      // 🔴 「这台机器有没有公网入口」是排暴露面时的第一个问题，此前新版答不了。
      //	⚠️ 没有外网 IP 是**正常且常见**的（内网机器），要显示成"无"而不是"—"，
      //	否则会被读成"没采到"，而那会让人以为可能有公网口只是没查到。
      id: 'network',
      header: t('hosts:column.network'),
      accessorFn: (h) => `${h.region} ${h.publicIp}`,
      cell: (ctx) => {
        const h = ctx.row.original
        return (
          <div className="flex min-w-0 flex-col">
            <span className="truncate text-[13px]">
              {h.region || <NoValue kind="na" labels={noValueLabels} />}
            </span>
            {h.publicIp !== '' ? (
              <span className="truncate font-mono text-[11px] text-warning" title={h.publicIp}>
                {h.publicIp}
              </span>
            ) : (
              <span className="text-[11px] text-muted-foreground">{t('hosts:value.noPublicIp')}</span>
            )}
          </div>
        )
      },
    },
    {
      /**
       * 规格 —— 直接写「几核几 G」，机型代号退居 tooltip。
       *
       * 之前这一列只显示 n2-standard-8：没人记得住它是几核几 G，
       * 跨云厂商（GCP 的 n2 / AWS 的 m5 / 自建机）更没法横向比。
       * 代号是给 API 用的，人要看的是数字。
       */
      id: 'spec',
      header: t('hosts:column.spec'),
      accessorFn: (h) => h.vcpu ?? -1,
      cell: (ctx) => {
        const h = ctx.row.original
        if (h.vcpu === null || h.memoryGb === null) {
          return <NoValue kind="unknown" labels={noValueLabels} />
        }
        return (
          <span className="tabular whitespace-nowrap" title={h.machineType}>
            {h.vcpu}
            <span className="text-muted-foreground">C</span>{' '}
            {h.memoryGb}
            <span className="text-muted-foreground">G</span>
          </span>
        )
      },
    },
    {
      /**
       * 磁盘 —— 显示总容量 + 盘数，并把**最满的那块**单独标出来。
       *
       * 只给一个加总的百分比会把问题藏起来：一台 boot 盘 96%、数据盘 71% 的机器，
       * 加权算下来才 79%，看着很健康，实际系统盘马上要写满了。
       * 而 kubelet 的 DiskPressure 恰恰是按单块盘判的。
       */
      id: 'disk',
      header: t('hosts:column.disk'),
      accessorFn: (h) => (h.disks ?? []).reduce((n, d) => n + d.sizeGb, 0),
      cell: (ctx) => <DiskCell host={ctx.row.original} t={t} locale={locale} labels={noValueLabels} />,
    },
    {
      id: 'cost',
      header: t('hosts:column.monthlyCost'),
      accessorFn: (h) => h.monthlyCost ?? -1,
      cell: (ctx) => {
        const h = ctx.row.original
        // ⚠️ 已销毁的机器**一律显示「已停止计费」**，不管后端返回什么数字。
        //
        // 后端的成本是按机型和磁盘**估算**的，不看 stale，所以对一台已经删掉的
        // 机器它照样能算出 US$112.50 —— 那是"如果还在跑会花多少"，不是实际账单。
        // 把它照原样显示，用户会以为一台不存在的机器还在花钱。
        if (h.status === 'destroyed') {
          return <NoValue kind="stopped" labels={noValueLabels} />
        }
        if (h.monthlyCost !== null)
          return <span className="tabular">{formatCurrency(h.monthlyCost, locale)}</span>
        // 自建机没有云账单 → 「—」。不能显示 $0：
        // $0 会被成本汇总当成真实的零成本资源。
        return <NoValue kind="na" labels={noValueLabels} />
      },
    },
    {
      /**
       * 日均 / 累计成本。
       *
       * 🔴 `cost_total` 是做成本复盘时**唯一**能回答"这台机器到今天一共花了多少"
       *	的字段。后端一直在返回 cost_daily / cost_total / cost_source，
       *	前端连字段都没声明（OPSCMDB-028 GAP-9）——只剩月成本，复盘做不了。
       *
       * ⚠️ estimate 与 bigquery 必须标出来：前者是按机型和磁盘**估算**的，
       *	后者是账单实数。把估算值当账单去对账会对不上，而两个数字长得一模一样。
       */
      id: 'costDetail',
      header: t('hosts:column.costDetail'),
      accessorFn: (h) => h.costTotal ?? -1,
      cell: (ctx) => {
        const h = ctx.row.original
        // 已销毁的机器同样不显示估算值，理由同月成本那一列
        if (h.status === 'destroyed') return <NoValue kind="stopped" labels={noValueLabels} />
        if (h.dailyCost === null && h.costTotal === null)
          return <NoValue kind="na" labels={noValueLabels} />
        return (
          <div className="flex min-w-0 flex-col">
            {/* 累计没算出来时**不显示这一行**，而不是渲染成「— 累计」——
                那读起来像"累计是 —"，其实是"这台还没有累计数据"。
                日均那一行仍在，人不会以为整格坏了 */}
            {h.costTotal !== null ? (
              <span className="tabular text-[13px]">
                {formatCurrency(h.costTotal, locale)}
                <span className="ml-1 text-[11px] text-muted-foreground">
                  {t('hosts:cost.totalSuffix')}
                </span>
              </span>
            ) : null}
            <span className="tabular text-[11px] text-muted-foreground">
              {h.dailyCost !== null
                ? t('hosts:cost.daily', { v: formatCurrency(h.dailyCost, locale) })
                : '—'}
              {h.costSource !== '' ? ` · ${t(`hosts:costSource.${h.costSource}`, { defaultValue: h.costSource })}` : ''}
            </span>
          </div>
        )
      },
    },
    {
      accessorKey: 'lastSyncAt',
      header: t('hosts:column.lastSync'),
      cell: (ctx) => {
        const at = ctx.row.original.lastSyncAt
        // 从没同步过 ≠ 刚刚同步过。没有值时显示「未知」而不是当前时间，
        // 否则一台从未采集成功的机器会显示成「几秒前」，看起来最新鲜。
        if (!at) return <NoValue kind="unknown" labels={noValueLabels} />
        // 🔴 超过判据要标出来。原来 17~18 小时前的数据也是一样的灰色小字，
        // 页面上没有任何"这可能已经过期了"的信号 —— 而主机台账是**快照**，
        // 一台已经挂了 17 小时的机器在这一页看着依然"运行中"
        const stale = isStale(at, freshness)
        return (
          <span
            className={stale ? 'tabular text-warning' : 'tabular text-muted-foreground'}
            // 列表用相对时间好扫，但 title 里给绝对时间 ——
            // 排障复盘时「3 天前」推不回具体时刻
            title={
              stale
                ? t('hosts:staleHint', { at, hours: Math.round(freshness!.staleAfterSeconds / 3600) })
                : at
            }
          >
            {formatRelativeTime(at, locale)}
          </span>
        )
      },
    },
  ]
}

/**
 * 这条记录的同步时间是不是已经超出判据。
 *
 * ⚠️ 判据取不到时一律返回 false —— 不知道的时候不报警，
 * 也不假装没问题（列上仍然显示真实时间，人能自己看出 17 小时）。
 */
export function isStale(
  at: string,
  freshness: { staleAfterSeconds: number; known: boolean } | null,
): boolean {
  if (!freshness || !freshness.known || freshness.staleAfterSeconds <= 0) return false
  const ms = Date.parse(at)
  if (Number.isNaN(ms)) return false
  return Date.now() - ms > freshness.staleAfterSeconds * 1000
}

/** 单块盘满到什么程度算值得标出来。与 kubelet DiskPressure 的默认阈值同量级。 */
const DISK_WARN = 80
const DISK_CRIT = 90

function DiskCell({
  host,
  t,
  locale,
  labels,
}: {
  host: Host
  t: TFn
  locale: Locale
  labels: Record<NoValueKind, string>
}) {
  const { disks } = host

  // null =「没采到」，[] =「确认没有盘」。两者含义完全不同，不能都渲染成空白。
  if (disks === null) {
    // 已销毁的机器谈磁盘没有意义；其余情况是真的没采到
    return <NoValue kind={host.status === 'destroyed' ? 'na' : 'unknown'} labels={labels} />
  }
  if (disks.length === 0) return <NoValue kind="na" labels={labels} />

  const total = disks.reduce((n, d) => n + d.sizeGb, 0)
  const measured = disks.filter((d): d is Disk & { usedPercent: number } => d.usedPercent !== null)
  const fullest = measured.length
    ? measured.reduce((a, b) => (a.usedPercent >= b.usedPercent ? a : b))
    : null

  const tone =
    fullest === null
      ? ''
      : fullest.usedPercent >= DISK_CRIT
        ? 'text-danger'
        : fullest.usedPercent >= DISK_WARN
          ? 'text-warning'
          : 'text-muted-foreground'

  // 明细走原生 title：表格里每行挂一个 Popover 会让 DOM 重一个数量级，
  // 而这是「偶尔看一眼」的信息，不值得那个代价。
  const detail = disks
    .map(
      (d) =>
        `${t(`hosts:disk.${d.role}`)} ${d.name} · ${gb(d.sizeGb, locale)}` +
        `${d.type ? ` · ${d.type}` : ''}` +
        `${d.usedPercent !== null ? ` · ${d.usedPercent}%` : ''}`,
    )
    .join('\n')

  return (
    <span className="inline-flex items-baseline gap-1.5 whitespace-nowrap" title={detail}>
      <span className="tabular">{gb(total, locale)}</span>
      {disks.length > 1 ? (
        <span className="text-xs text-muted-foreground">
          {t('hosts:disk.count', { count: disks.length })}
        </span>
      ) : null}
      {fullest && fullest.usedPercent >= DISK_WARN ? (
        <span className={`tabular text-xs ${tone}`}>{fullest.usedPercent}%</span>
      ) : null}
    </span>
  )
}
