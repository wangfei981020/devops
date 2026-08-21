import { ClusterCell } from '../../components/ClusterCell.js'
import type { Locale } from '@ops/i18n'
import { Badge, type ColumnDef } from '@ops/ui'
import { useState } from 'react'
import { ManifestDialog } from '../../components/ManifestDialog.js'
import { type Workload, healthLabelKey, healthOf } from './queries.js'

type TFn = (key: string, opts?: Record<string, unknown>) => string

export function workloadColumns(t: TFn, _locale: Locale): ColumnDef<Workload>[] {
  return [
    {
      accessorKey: 'name',
      header: t('workloads:column.name'),
      cell: ({ row }) => {
        const w = row.original
        return (
          <div className="flex min-w-0 flex-col">
            <span className="truncate font-medium text-foreground">{w.name}</span>
            <span className="truncate text-xs text-muted-foreground">
              {w.namespace} · {w.kind}
            </span>
          </div>
        )
      },
    },
    {
      id: 'replicas',
      header: t('workloads:column.replicas'),
      cell: ({ row }) => {
        const w = row.original
        const h = healthOf(w)
        // ⚠️ 0/0 用中性色 + 一句说明，绝不能和 0/3 同色。
        // 两者都是"就绪 0 个"，但一个要立刻处理，一个什么都不用做
        if (h === 'scaled_zero') {
          return (
            <span className="flex items-baseline gap-1.5">
              <span className="tabular text-muted-foreground">0 / 0</span>
              <span className="text-[11px] text-muted-foreground">
                {t('workloads:health.scaledZero')}
              </span>
            </span>
          )
        }
        const tone =
          h === 'down' ? 'text-danger' : h === 'degraded' ? 'text-warning' : 'text-foreground'
        return (
          <span className="tabular">
            <span className={tone}>{w.replicasReady}</span>
            <span className="text-muted-foreground"> / {w.replicasDesired}</span>
          </span>
        )
      },
    },
    {
      id: 'health',
      header: t('workloads:column.health'),
      cell: ({ row }) => {
        const h = healthOf(row.original)
        const tone = h === 'down' ? 'bad' : h === 'degraded' ? 'warn' : h === 'ok' ? 'ok' : 'mute'
        return <Badge tone={tone}>{t(`workloads:health.${healthLabelKey(h)}`)}</Badge>
      },
    },
    {
      id: 'image',
      header: t('workloads:column.image'),
      cell: ({ row }) => {
        const w = row.original
        // tag 单独强调：排障时问的是"跑的是哪个 tag"，
        // 而完整镜像串里的仓库地址往往长到把这一列挤没
        return (
          <div className="flex min-w-0 flex-col">
            <span className="truncate font-mono text-xs text-foreground">
              {w.imageTag || '—'}
            </span>
            <span className="truncate font-mono text-[11px] text-muted-foreground" title={w.image}>
              {w.image}
            </span>
          </div>
        )
      },
    },
    {
      accessorKey: 'clusterName',
      header: t('workloads:column.cluster'),
      cell: ({ row }) => (
        <ClusterCell name={row.original.clusterName} display={row.original.clusterDisplay} />
      ),
    },
    {
      // 看 YAML：CMDB 采的是字段，排障要看的是原文（OPSCMDB-023 第一档）。
      // ⚠️ 只在点击时请求 —— 后端每次都写审计，预取会把审计灌成噪音。
      id: 'yaml',
      // 操作列固定在最右侧。⚠️ 标了就必须真的排在数组最后（check-action-column 守这条）
      meta: { action: true },
      header: '',
      cell: ({ row }) => (
        <div className="flex items-center justify-end gap-2">
          {/* 🔴 工作负载 → Pod 的下钻。
              旧版点一个工作负载就能列出它的 Pod，新版只剩「查看 manifest」——
              而后端 pod-list 一直支持 workload 过滤。
              此前只能靠关键词搜工作负载名，那是**碰巧**能命中
              （Pod 名 = 工作负载名 + hash）；Job / CronJob 对不上时就搜不到，
              而"搜不到"会被读成"这个工作负载没有 Pod"（OPSCMDB-028 GAP-11）。
              ⚠️ 用整页跳转而不是 router.navigate：跨路由带 search 参数时
              类型推导会成环（同 nodes 页跳主机页的处理）。 */}
          <button
            type="button"
            onClick={() => {
              const w = row.original
              const qs = new URLSearchParams({
                cluster: w.clusterName,
                namespace: w.namespace,
                workload: w.name,
              })
              window.location.href = `/k8s/pods?${qs.toString()}`
            }}
            className="cursor-pointer text-[13px] text-brand-text underline-offset-2 hover:underline"
          >
            {t('workloads:action.viewPods')}
          </button>
          <ManifestButton w={row.original} label={t('manifest:view')} />
        </div>
      ),
    },
  ]
}

/** 行内「YAML」按钮。弹窗按需挂载，不点就不请求 */
function ManifestButton({ w, label }: { w: Workload; label: string }) {
  const [open, setOpen] = useState(false)
  return (
    <>
      <button
        type="button"
        onClick={() => setOpen(true)}
        className="cursor-pointer rounded-[var(--radius)] px-1.5 py-0.5 text-[11px] text-brand-text underline-offset-2 hover:underline"
      >
        {label}
      </button>
      {open ? (
        <ManifestDialog
          clusterId={w.clusterId}
          kind={w.kind}
          namespace={w.namespace}
          name={w.name}
          onClose={() => setOpen(false)}
        />
      ) : null}
    </>
  )
}
