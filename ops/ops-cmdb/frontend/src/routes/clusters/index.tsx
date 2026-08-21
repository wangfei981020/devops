import { toErrorInfo } from '@ops/api'
import { clusterLabel } from '../../lib/clusterLabel.js'
import { tError, type Locale, useTranslation } from '@ops/i18n'
import {
  AsyncBoundary,
  DataTable,
  EmptyState,
  type LoadError,
  type NoValueKind,
  SearchInput,
  Select,
  Pagination,
  TableSkeleton,
  fromQuery,
} from '@ops/ui'
import { useNavigate, useSearch } from '@tanstack/react-router'
import { Boxes } from 'lucide-react'
import { Button, Dialog } from '@ops/ui'
import { RowMenu } from '../../components/RowMenu.js'
import { WriteButton } from '../../components/WriteButton.js'
import { useMemo, useState } from 'react'
import { ClusterDialog } from './ClusterDialog.js'
import { clusterColumns } from './columns.js'
import { type Cluster, type ClusterListResult, useClusters, useDeleteCluster, useSyncCluster, useTestCluster } from './queries.js'
import { DiscoverDialog } from './DiscoverDialog.js'
import { GovernDialog } from './GovernDialog.js'
import { ObjectHistoryDialog } from '../../components/ObjectHistory.js'

type TFn = (key: string, opts?: Record<string, unknown>) => string

const PAGE_SIZE = 50

export function ClustersPage() {
  const [discovering, setDiscovering] = useState(false)
  const { t, i18n } = useTranslation()
  const locale = i18n.language as Locale
  // 筛选走 URL，理由同主机页：排障时链接要能直接发给同事
  const { env, page, size, q: keyword } = useSearch({ from: '/k8s/clusters' })
  const navigate = useNavigate({ from: '/k8s/clusters' })
  const patch = (next: Partial<{ env: string; q: string; page: number; size: number }>) =>
    void navigate({ search: (prev) => ({ ...prev, ...next }) })

  const [editing, setEditing] = useState<Cluster | 'new' | null>(null)
  const [delFor, setDelFor] = useState<Cluster | null>(null)
  const [historyFor, setHistoryFor] = useState<Cluster | null>(null)
  // 治理下钻：孤儿/容量/命名空间/变更/安全/配置 六档（OPSCMDB-021）
  const [governFor, setGovernFor] = useState<Cluster | null>(null)
  const query = useClusters({ page, size, env, q: keyword })

  const labels: Record<NoValueKind, string> = {
    na: t('clusters:value.notApplicable'),
    notIngested: t('common:state.notIngested'),
    stopped: t('common:state.unknown'),
    unknown: t('common:state.unknown'),
  }
  const columns = useMemo(
    () => [
      ...clusterColumns(t, locale, labels),
      {
        id: 'actions',
        header: '',
        // 操作列固定在最右侧。这一页的操作列由页面提供（要用页面上的弹窗状态），
        // 所以标记打在这里而不是 columns.tsx
        meta: { action: true },
        cell: ({ row }: { row: { original: Cluster } }) => (
          <RowActions
            cluster={row.original}
            onEdit={() => setEditing(row.original)}
            onDelete={() => setDelFor(row.original)}
            onGovern={() => setGovernFor(row.original)}
            onHistory={() => setHistoryFor(row.original)}
          />
        ),
      },
    ],
    [t, locale, labels],
  )
  const toLoadError = useMemo(
    () =>
      (e: unknown): LoadError => {
        const n = toErrorInfo(e)
        return { cause: tError(t, n.messageKey, n.params), detail: n.detail, retryable: n.retryable, kind: n.kind === 'license' ? 'license' : undefined, feature: n.params?.feature as string | undefined }
      },
    [t],
  )

  const state = fromQuery<ClusterListResult>(query, (d) => d.total === 0, toLoadError)

  // 空态下 facets/total 都取不到 —— 各自不渲染，而不是显示 0
  const envFacets = query.data?.facets?.env
  const total = query.data?.total
  const envs = Object.keys(envFacets ?? {})
    .filter((k) => k !== 'all')
    .sort()
  // 「N 个集群还没采到数据」：空态下没有 items，这条提示自然也不显示
  const gaps = query.data?.items?.filter((c) => !c.ingested).length ?? 0

  // ⚠️ 工具条**必须在 AsyncBoundary 外面**。
  // 写在 children 里的话，空态分支下整条工具条不渲染 —— 而"接入/同步"按钮就在上面，
  // 于是全新安装的人永远接不进第一条数据。判据：工具条只依赖筛选状态，不依赖数据。

  return (
    <div className="flex flex-col">
          <div className="flex flex-wrap items-center gap-2 border-b border-border px-4 py-3">
            <SearchInput
              value={keyword}
              onChange={(v) => patch({ q: v })}
              placeholder={t('clusters:filter.searchPlaceholder')}
              clearLabel={t('common:filter.clearSearch')}
              className="w-[228px]"
            />
            <Select<string>
              label={t('clusters:filter.env')}
              value={env}
              onChange={(v) => patch({ env: v })}
              options={[
                { value: 'all', label: t('common:filter.all'), count: envFacets?.all },
                ...envs.map((e) => ({
                  value: e,
                  label: e,
                  count: envFacets?.[e],
                })),
              ]}
            />
            {gaps > 0 ? (
              // 明说有几个集群没采到。不说的话，那几行的"未接入"
              // 会被当成这些集群本来就是空的
              <span className="text-xs text-warning">
                {t('clusters:gapNote', { count: gaps })}
              </span>
            ) : null}
            {total != null ? (
              <span className="ml-auto text-xs text-muted-foreground">
                {t('clusters:total', { count: total })}
              </span>
            ) : (
              <span className="ml-auto" />
            )}
            <WriteButton
              perm="cmdb:manage_clusters"
              size="sm"
              onClick={() => setDiscovering(true)}
            >
              {t('clusters:discover.entry')}
            </WriteButton>
            <WriteButton
              perm="cmdb:manage_clusters"
              variant="primary"
              size="sm"
              onClick={() => setEditing('new')}
            >
              {t('common:write.add')}
            </WriteButton>
          </div>

      <AsyncBoundary
        state={state}
        errorTitle={t('clusters:error.title')}
        retryLabel={t('common:action.retry')}
        onRetry={() => void query.refetch()}
        pending={
          <div className="p-5">
            <TableSkeleton columns={[26, 8, 14, 10, 12, 14, 12]} rows={5} />
          </div>
        }
        empty={
          <EmptyState
            icon={<Boxes />}
            title={t('clusters:empty.title')}
            reason={
              keyword !== '' || env !== 'all'
                ? t('clusters:empty.filtered')
                : t('clusters:empty.noSource')
            }
            action={
              keyword !== '' || env !== 'all'
                ? { label: t('common:filter.clearAll'), onClick: () => patch({ q: '', env: 'all' }) }
                : null
            }
          />
        }
      >
        {(data) => {
          // 采集缺口单独数一份：它不是一个筛选维度，而是一条**待办**。
          // 混进环境下拉里的话，没人会主动去点它

          return (
            <>

              <DataTable data={data.items} columns={columns} rowKey={(c) => String(c.id)} />
              <Pagination
                page={page}
                size={size}
                total={data.total}
                onPage={(p) => patch({ page: p })}
                onSize={(n) => patch({ size: n })}
                rangeLabel={(f, t2, tt) => t('common:pagination.range', { from: f, to: t2, total: tt })}
                totalLabel={(n) => t('common:pagination.total', { count: n })}
                perPageLabel={t('common:pagination.perPage')}
                prevLabel={t('common:pagination.prev')}
                nextLabel={t('common:pagination.next')}
              />
            </>
          )
        }}
      </AsyncBoundary>

      {editing ? (
        <ClusterDialog cluster={editing === 'new' ? null : editing} onClose={() => setEditing(null)} />
      ) : null}
      {delFor ? <DeleteClusterDialog cluster={delFor} onClose={() => setDelFor(null)} /> : null}
      {historyFor ? (
        <ObjectHistoryDialog
          table="k8s_clusters"
          pk={String(historyFor.id)}
          title={clusterLabel(historyFor.displayName, historyFor.name)}
          onClose={() => setHistoryFor(null)}
        />
      ) : null}
      {discovering ? <DiscoverDialog onClose={() => setDiscovering(false)} /> : null}
      {governFor ? (
        <GovernDialog
          clusterID={governFor.id}
          clusterName={clusterLabel(governFor.displayName, governFor.name)}
          onClose={() => setGovernFor(null)}
          t={t}
        />
      ) : null}
    </div>
  )
}

/** 行内操作：测连通 / 立即采集 / 编辑 / 删除。 */
function RowActions({
  cluster,
  onEdit,
  onDelete,
  onGovern,
  onHistory,
}: {
  cluster: Cluster
  onEdit: () => void
  onDelete: () => void
  onGovern: () => void
  onHistory: () => void
}) {
  const { t } = useTranslation()
  const test = useTestCluster()
  const sync = useSyncCluster()
  return (
    <div className="flex items-center justify-end gap-1.5">
      {/* 测连通的结果就地显示。凭据错的集群和没接一样，
          但列表里它看着是好的 —— 所以这个按钮比"保存成功"重要 */}
      {test.isSuccess ? (
        <span className="text-xs text-success">{t('common:write.testOk')}</span>
      ) : test.isError ? (
        // 原因直接显示，不藏在 title 里：「连不上」三个字没法处置，
        // 「未配置连接方式」一眼就知道要去干什么
        <span className="max-w-[260px] truncate text-xs text-danger" title={toErrorInfo(test.error).detail}>
          {toErrorInfo(test.error).detail || t('common:write.testFail')}
        </span>
      ) : null}
      <WriteButton perm="cmdb:manage_clusters" size="sm" loading={test.isPending} onClick={() => test.mutate(cluster.id)}>
        {t('common:write.test')}
      </WriteButton>
      <WriteButton perm="cmdb:manage_clusters" size="sm" loading={sync.isPending} onClick={() => sync.mutate(cluster.id)}>
        {t('common:write.syncNow')}
      </WriteButton>
      {/* 治理是只读下钻，不需要写权限。
          ⚠️ 「测试连通性」「立即同步」这两个动作明确，而「治理」是个抽象词，
          光看按钮不知道点下去会发生什么（OPSCMDB-031 P2-15）——
          所以 title 里把六档下钻逐个列出来 */}
      <Button size="sm" onClick={onGovern} title={t('clusters:govern.entryHint')}>
        {t('clusters:govern.entry')}
      </Button>
      <RowMenu
        perm="cmdb:manage_clusters"
        items={[
          { key: 'edit', label: t('common:write.edit'), onClick: onEdit },
          // 「这个集群昨天还在采数据，今天怎么没了」—— 多半是有人改了凭据
          // 或者停用了它，而那件事只在审计里留了痕（OPSCMDB-023 第三档）
          { key: 'history', label: t('audit:objectHistory.entry'), onClick: onHistory },
          {
            key: 'delete',
            label: t('common:write.delete'),
            onClick: onDelete,
            danger: true,
          },
        ]}
      />
    </div>
  )
}

function DeleteClusterDialog({ cluster, onClose }: { cluster: Cluster; onClose: () => void }) {
  const { t } = useTranslation()
  const del = useDeleteCluster()
  return (
    <Dialog
      open
      onClose={onClose}
      title={t('common:write.deleteConfirm', { name: cluster.displayName })}
      description={t('common:write.deleteHint')}
      closeLabel={t('common:action.close')}
      footer={
        <>
          <Button size="sm" onClick={onClose}>{t('common:write.cancel')}</Button>
          <Button size="sm" variant="danger" loading={del.isPending} onClick={() => del.mutate(cluster.id, { onSuccess: onClose })}>
            {t('common:write.delete')}
          </Button>
        </>
      }
    >
      {/* 删除影响面：这个集群下的节点/Pod/工作负载等采集数据会怎样 */}
      <p className="text-[13px] leading-relaxed text-foreground">{t('clusters:deleteImpact')}</p>
      {del.isError ? <p className="mt-2 text-xs text-danger">{t(toErrorInfo(del.error).messageKey)}</p> : null}
    </Dialog>
  )
}
