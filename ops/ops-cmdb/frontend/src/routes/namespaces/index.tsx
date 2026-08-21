import { toErrorInfo } from '@ops/api'
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
import { useState } from 'react'
import { WriteButton } from '../../components/WriteButton.js'
import { NsProjectDialog } from './NsProjectDialog.js'
import { Layers } from 'lucide-react'
import { useMemo } from 'react'
import { namespaceColumns } from './columns.js'
import { type NamespaceListResult, useNamespaces } from './queries.js'

type TFn = (key: string, opts?: Record<string, unknown>) => string

const PAGE_SIZE = 50

export function NamespacesPage() {
  const { t, i18n } = useTranslation()
  const locale = i18n.language as Locale
  // 筛选走 URL，理由同主机页：排障时链接要能直接发给同事
  const { cluster, phase, page, size, q: keyword } = useSearch({ from: '/k8s/namespaces' })
  const navigate = useNavigate({ from: '/k8s/namespaces' })
  const patch = (next: Partial<{ cluster: string; phase: string; q: string; page: number; size: number }>) =>
    void navigate({ search: (prev) => ({ ...prev, ...next }) })

  const query = useNamespaces({ page, size, cluster, phase, q: keyword })
  const [nsProjectFor, setNsProjectFor] = useState(0)

  const labels: Record<NoValueKind, string> = {
    na: '—',
    notIngested: t('common:state.notIngested'),
    stopped: t('common:state.unknown'),
    unknown: t('common:state.unknown'),
  }
  const columns = useMemo(() => namespaceColumns(t, locale, labels), [t, locale, labels])
  const toLoadError = useMemo(
    () =>
      (e: unknown): LoadError => {
        const n = toErrorInfo(e)
        return { cause: tError(t, n.messageKey, n.params), detail: n.detail, retryable: n.retryable, kind: n.kind === 'license' ? 'license' : undefined, feature: n.params?.feature as string | undefined }
      },
    [t],
  )

  const state = fromQuery<NamespaceListResult>(query, (d) => d.total === 0, toLoadError)

  return (
    <div className="flex flex-col">
      {/* ⚠️ 写按钮必须放在 AsyncBoundary **外面**。放在 children 里的话，
          列表为空（或筛选筛空）时整条工具条连同这个按钮一起不渲染，
          而"没有数据"恰恰是最需要去配归属的时候（OPSCMDB-001）。
          空态下拿不到 items，按钮置灰并说明原因——置灰要给理由，见 WriteButton */}
      <div className="flex flex-wrap items-center justify-end gap-2 border-b border-border px-4 py-2">
        <WriteButton
          perm="cmdb:manage_basic"
          size="sm"
          blockedReason={
            !cluster || cluster === 'all'
              ? t('namespaces:nsproject.pickCluster')
              : (query.data?.items?.length ?? 0) === 0
                ? t('namespaces:nsproject.needData')
                : undefined
          }
          onClick={() => setNsProjectFor(clusterIdOf(query.data?.items ?? [], cluster))}
        >
          {t('namespaces:nsproject.entry')}
        </WriteButton>
      </div>

      <AsyncBoundary
        state={state}
        errorTitle={t('namespaces:error.title')}
        retryLabel={t('common:action.retry')}
        onRetry={() => void query.refetch()}
        pending={
          <div className="p-5">
            <TableSkeleton columns={[26, 8, 14, 10, 12, 14, 12]} rows={5} />
          </div>
        }
        empty={
          <EmptyState
            icon={<Layers />}
            title={t('namespaces:empty.title')}
            reason={
              keyword !== '' || cluster !== 'all' || phase !== 'all'
                ? t('namespaces:empty.filtered')
                : t('namespaces:empty.noSource')
            }
            action={
              keyword !== '' || cluster !== 'all' || phase !== 'all'
                ? { label: t('common:filter.clearAll'), onClick: () => patch({ q: '', cluster: 'all', phase: 'all' }) }
                : null
            }
          />
        }
      >
        {(data) => {
          const clusters = Object.keys(data.facets.cluster ?? {})
            .filter((k) => k !== 'all')
            .sort()
          const phases = Object.keys(data.facets.phase ?? {})
            .filter((k) => k !== 'all' && k !== '')
            .sort()
          // Terminating 单独数一份：它不是一个普通取值，而是一条**待办**
          const stuck = data.items.filter((n) => n.phase === 'Terminating').length

          return (
            <>
              <div className="flex flex-wrap items-center gap-2 border-b border-border px-4 py-3">
                <SearchInput
                  value={keyword}
                  onChange={(v) => patch({ q: v })}
                  placeholder={t('namespaces:filter.searchPlaceholder')}
                  clearLabel={t('common:filter.clearSearch')}
                  className="w-[228px]"
                />
                <Select<string>
                  label={t('namespaces:filter.cluster')}
                  value={cluster}
                  onChange={(v) => patch({ cluster: v })}
                  options={[
                    { value: 'all', label: t('common:filter.all'), count: data.facets.cluster?.all },
                    ...clusters.map((c) => ({
                      value: c,
                      label: c,
                      count: data.facets.cluster?.[c],
                    })),
                  ]}
                />
                <Select<string>
                  label={t('namespaces:filter.phase')}
                  value={phase}
                  onChange={(v) => patch({ phase: v })}
                  options={[
                    { value: 'all', label: t('common:filter.all'), count: data.facets.phase?.all },
                    ...phases.map((p) => ({
                      value: p,
                      label: p,
                      count: data.facets.phase?.[p],
                    })),
                  ]}
                />
                {stuck > 0 && phase !== 'Terminating' ? (
                  // 明说有几个卡在 Terminating。不说的话，它们只是列表里
                  // 几行普通的橙色，而它们会一直占着名字让重建失败
                  <span className="text-xs text-warning">
                    {t('namespaces:stuckNote', { count: stuck })}
                  </span>
                ) : null}
                <span className="ml-auto text-xs text-muted-foreground">
                  {t('namespaces:total', { count: data.total })}
                </span>
              </div>

              <DataTable data={data.items} columns={columns} rowKey={(n) => `${n.clusterId}/${n.name}`} />
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

      {nsProjectFor > 0 ? (
        <NsProjectDialog clusterID={nsProjectFor} onClose={() => setNsProjectFor(0)} />
      ) : null}
    </div>
  )
}

/**
 * 从当前列表里取出这个集群名对应的 cluster_id。
 *
 * ⚠️ 筛选用的是集群**名**，而归属接口要的是 **id** —— 两者不通用。
 * 取不到时返回 0，按钮那边会因此置灰（而不是发一个 cluster_id=0 的请求，
 * 那种请求后端会当成"没指定集群"，返回的东西和你以为的不是一回事）。
 */
function clusterIdOf(items: { clusterId: number; clusterName: string }[], name: string): number {
  return items.find((i) => i.clusterName === name)?.clusterId ?? 0
}
