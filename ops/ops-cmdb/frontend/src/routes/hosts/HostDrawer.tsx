import { toErrorInfo } from '@ops/api'
import { formatBytes, formatCurrency, formatDateTime, tError, type Locale, useTranslation } from '@ops/i18n'
import {
  AsyncBoundary,
  Badge,
  Drawer,
  DrawerField,
  DrawerSection,
  type LoadError,
  NoValue,
  type NoValueKind,
  Skeleton,
  fromQuery,
} from '@ops/ui'
import { phaseTone } from './podPhase.js'
import { type HostDetail, useHostDetail } from './queries.js'

type TFn = (key: string, opts?: Record<string, unknown>) => string

export function HostDrawer({ ciId, onClose }: { ciId: string | null; onClose: () => void }) {
  const { t, i18n } = useTranslation()
  const locale = i18n.language as Locale
  const query = useHostDetail(ciId)

  const toLoadError = (e: unknown): LoadError => {
    const n = toErrorInfo(e)
    return { cause: tError(t, n.messageKey, n.params), detail: n.detail, retryable: n.retryable, kind: n.kind === 'license' ? 'license' : undefined, feature: n.params?.feature as string | undefined }
  }
  // 详情永远不会是"空"——要么查到了，要么 404（那是 error）。
  // 所以判空恒为 false，空态分支只是为了满足 AsyncBoundary 的四态约束。
  const state = fromQuery<HostDetail>(query, () => false, toLoadError)

  return (
    <Drawer
      open={ciId !== null}
      onClose={onClose}
      closeLabel={t('common:action.close')}
      title={query.data?.host.name ?? t('hosts:detail.title')}
      subtitle={
        query.data ? `${query.data.host.privateIp} · ${query.data.host.cluster}` : undefined
      }
    >
      <AsyncBoundary
        state={state}
        errorTitle={t('hosts:error.title')}
        retryLabel={t('common:action.retry')}
        onRetry={() => void query.refetch()}
        pending={
          <div className="flex flex-col gap-3 p-4">
            {[70, 90, 60, 80, 50].map((w) => (
              <Skeleton key={w} className="h-3" style={{ width: `${w}%` }} />
            ))}
          </div>
        }
        empty={null}
      >
        {(d) => <DetailBody detail={d} t={t} locale={locale} />}
      </AsyncBoundary>
    </Drawer>
  )
}

function DetailBody({ detail, t, locale }: { detail: HostDetail; t: TFn; locale: Locale }) {
  const h = detail.host
  const labels: Record<NoValueKind, string> = {
    na: t('hosts:value.notApplicable'),
    notIngested: t('common:state.notIngested'),
    stopped: t('hosts:value.billingStopped'),
    unknown: t('common:state.unknown'),
  }
  const gb = (n: number) => formatBytes(n * 1024 ** 3, locale, 1)

  return (
    <>
      <DrawerSection title={t('hosts:detail.spec')}>
        <DrawerField label={t('hosts:column.status')}>
          <Badge tone={h.status === 'destroyed' ? 'mute' : 'ok'}>
            {t(`hosts:status.${h.status}`)}
          </Badge>
        </DrawerField>
        <DrawerField label={t('hosts:column.spec')}>
          {h.vcpu !== null && h.memoryGb !== null ? (
            <span className="tabular">
              {h.vcpu} vCPU · {h.memoryGb} GB
            </span>
          ) : (
            <NoValue kind="unknown" labels={labels} />
          )}
        </DrawerField>
        <DrawerField label={t('hosts:column.machineType')}>
          <span className="font-mono text-xs">{h.machineType}</span>
        </DrawerField>
        <DrawerField label={t('hosts:detail.zone')}>{h.zone || '—'}</DrawerField>
        <DrawerField label={t('hosts:detail.os')}>{h.os || '—'}</DrawerField>
        <DrawerField label={t('hosts:detail.preemptible')}>
          {h.preemptible ? t('hosts:detail.yes') : t('hosts:detail.no')}
        </DrawerField>
        <DrawerField label={t('hosts:column.lastSync')}>
          {h.lastSyncAt ? (
            <span className="tabular">{formatDateTime(h.lastSyncAt, locale, { withSeconds: true })}</span>
          ) : (
            <NoValue kind="unknown" labels={labels} />
          )}
        </DrawerField>
      </DrawerSection>

      <DrawerSection title={t('hosts:detail.disks')}>
        {detail.disks.length === 0 ? (
          <NoValue kind="na" labels={labels} />
        ) : (
          <div className="flex flex-col gap-2">
            {/* 逐块列出，不加总 —— 加总会把"某一块快满了"这件事藏起来，
                而那正是打开详情要看的东西 */}
            {detail.disks.map((d) => (
              <div key={d.name} className="rounded-[var(--radius)] border border-border p-2.5">
                <div className="flex items-baseline gap-2">
                  <span className="text-[13px] font-medium text-foreground">{d.name}</span>
                  <Badge tone="mute" dot={false}>
                    {d.role === 'boot' ? t('hosts:detail.bootDisk') : t('hosts:detail.dataDisk')}
                  </Badge>
                  <span className="tabular ml-auto text-[13px]">{gb(d.sizeGb)}</span>
                </div>
                <div className="mt-1.5 flex items-baseline gap-3 text-xs text-muted-foreground">
                  {d.type ? <span className="font-mono">{d.type}</span> : null}
                  {d.mountPoint ? (
                    <span className="font-mono">{d.mountPoint}</span>
                  ) : null}
                  <span className="ml-auto">
                    {d.usedPercent === null ? (
                      <NoValue kind="unknown" labels={labels} />
                    ) : (
                      <UsageBar percent={d.usedPercent} />
                    )}
                  </span>
                </div>
              </div>
            ))}
          </div>
        )}
      </DrawerSection>

      <DrawerSection title={t('hosts:detail.pods')}>
        <PodsBlock detail={detail} t={t} labels={labels} />
      </DrawerSection>

      <DrawerSection title={t('hosts:detail.cost')}>
        <DrawerField label={t('hosts:detail.costMonth')}>
          {h.status === 'destroyed' ? (
            <NoValue kind="stopped" labels={labels} />
          ) : (
            <span className="tabular">{formatCurrency(detail.costHourly * 730, locale)}</span>
          )}
        </DrawerField>
        <DrawerField label={t('hosts:detail.costHourly')}>
          <span className="tabular">{formatCurrency(detail.costHourly, locale)}</span>
        </DrawerField>
        {/* 明说是估算。不说的话，用户会拿它和云账单对，对不上就来问 */}
        <p className="mt-1.5 text-[11px] text-muted-foreground">{t('hosts:detail.costEstimate')}</p>
      </DrawerSection>

      <DrawerSection title={t('hosts:detail.domains')}>
        {detail.relatedDomains.length === 0 ? (
          <p className="text-xs text-muted-foreground">{t('hosts:detail.noDomains')}</p>
        ) : (
          <div className="flex flex-col gap-1">
            {detail.relatedDomains.map((r) => (
              <div key={r.fqdn} className="flex items-baseline gap-2 text-[13px]">
                <span className="text-foreground">{r.fqdn}</span>
                <span className="font-mono text-xs text-muted-foreground">{r.ip}</span>
              </div>
            ))}
          </div>
        )}
      </DrawerSection>

      <DrawerSection title={t('hosts:detail.labels')}>
        {Object.keys(detail.host.labels).length === 0 ? (
          <p className="text-xs text-muted-foreground">{t('hosts:detail.noLabels')}</p>
        ) : (
          <div className="flex flex-wrap gap-1.5">
            {Object.entries(detail.host.labels).map(([k, v]) => (
              <span
                key={k}
                className="rounded-[var(--radius-sm)] bg-secondary px-1.5 py-0.5 font-mono text-[11px] text-muted-foreground"
              >
                {k}={v}
              </span>
            ))}
          </div>
        )}
      </DrawerSection>
    </>
  )
}

/**
 * 该节点上的 Pod。
 *
 * 三种"没有 Pod"必须长得不一样，这是整个抽屉里最容易退化的地方：
 *   · 不是集群节点        —— 正常，什么都不用做
 *   · 是节点但集群没接入  —— 采集缺口，我们没数据（不是"上面没东西"）
 *   · 接了但确实是空的    —— 节点是空的，可能刚扩容或被 drain 了
 * 把后两种渲染成同一片空白，等于替客户宣称"这台机器闲着"。
 */
function PodsBlock({
  detail,
  t,
  labels,
}: {
  detail: HostDetail
  t: TFn
  labels: Record<NoValueKind, string>
}) {
  if (detail.nodeLink === 'none') {
    return <NoValue kind="na" labels={labels} />
  }
  if (detail.nodeLink === 'not_ingested') {
    return (
      <div className="flex flex-col gap-1">
        <NoValue kind="notIngested" labels={labels} />
        <p className="text-xs text-muted-foreground">{t('hosts:detail.nodeNotIngested')}</p>
      </div>
    )
  }

  const node = detail.node
  const drift = node !== null && node.podCount !== detail.pods.length

  return (
    <div className="flex flex-col gap-2">
      {node ? (
        <div className="flex items-baseline gap-2 text-xs text-muted-foreground">
          <span className="text-foreground">{node.clusterName || `#${node.clusterId}`}</span>
          {node.pool ? <span className="font-mono">{node.pool}</span> : null}
          {/* Ready 之外的一律标红：NotReady / Unknown 都意味着这台机器上的
              Pod 状态本身就不可信，得先让人看见节点这一层出了问题 */}
          {node.readyStatus && node.readyStatus !== 'Ready' ? (
            <Badge tone="bad">{node.readyStatus}</Badge>
          ) : null}
          <span className="tabular ml-auto">
            {t('hosts:detail.podCount', { count: detail.pods.length })}
          </span>
        </div>
      ) : null}

      {/* 两张表由不同轮次的同步写入，对不上说明其中一份是旧的。
          我们不知道哪份新，所以两个数都摆出来让人自己判断 ——
          挑一个显示就等于替用户断定哪份可信 */}
      {drift && node ? (
        <p className="text-xs text-warning">
          {t('hosts:detail.podCountDrift', { reported: node.podCount, listed: detail.pods.length })}
        </p>
      ) : null}

      {detail.pods.length === 0 ? (
        <p className="text-xs text-muted-foreground">{t('hosts:detail.noPods')}</p>
      ) : (
        <div className="flex flex-col">
          {detail.pods.map((p) => (
            <div
              key={`${p.namespace}/${p.name}`}
              className="flex items-baseline gap-2 border-b border-border py-1 text-[13px] last:border-b-0"
            >
              <span className="shrink-0 font-mono text-xs text-muted-foreground">{p.namespace}</span>
              <span className="min-w-0 flex-1 truncate text-foreground" title={p.name}>
                {p.name}
              </span>
              {p.restarts > 0 ? (
                <span className="tabular shrink-0 text-xs text-warning">
                  {t('hosts:detail.restarts', { count: p.restarts })}
                </span>
              ) : null}
              {/* phase 原样显示，不翻译：Running / CrashLoopBackOff 是 k8s 的
                  原值，运维就是按这个词去 kubectl 里搜的，译成中文反而对不上 */}
              <Badge tone={phaseTone(p.phase)}>{p.phase}</Badge>
            </div>
          ))}
        </div>
      )}
    </div>
  )
}

/** 用量条。阈值与列表页一致，避免同一块盘在两处显示不同的严重度。 */
function UsageBar({ percent }: { percent: number }) {
  const tone =
    percent >= 90 ? 'bg-danger' : percent >= 80 ? 'bg-warning' : 'bg-muted-foreground'
  return (
    <span className="inline-flex items-center gap-1.5">
      <span className="h-1 w-16 overflow-hidden rounded-full bg-border">
        <span className={`block h-full ${tone}`} style={{ width: `${Math.min(100, percent)}%` }} />
      </span>
      <span className="tabular">{percent}%</span>
    </span>
  )
}
