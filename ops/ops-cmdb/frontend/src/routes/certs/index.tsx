import { toErrorInfo } from '@ops/api'
import { tError, type Locale, useTranslation } from '@ops/i18n'
import { AsyncBoundary, Banner, DataTable, EmptyState, Pagination, SearchInput, Select, TableSkeleton, fromQuery, type LoadError, type NoValueKind } from '@ops/ui'
import { useNavigate, useSearch } from '@tanstack/react-router'
import { Button } from '@ops/ui'
import { WriteButton } from '../../components/WriteButton.js'
import { CertInspectDialog } from './InspectDialog.js'
import { ApplyCertDialog } from './ApplyCertDialog.js'
import { AcmeDialog } from './AcmeDialog.js'
import { ShieldCheck } from 'lucide-react'
import { useMemo, useState } from 'react'
import { certColumns } from './columns.js'
import { probeNoteText, type CertHealth, type CertListResult, useCerts } from './queries.js'

type TFn = (key: string, opts?: Record<string, unknown>) => string

const PAGE_SIZE = 50

export function CertsPage() {
  // 证书到期巡检（OPSCMDB-021）。注意与「运行/巡检」不是一回事
  const [inspect, setInspect] = useState(false)
  const { t, i18n } = useTranslation()
  const locale = i18n.language as Locale
  // 筛选走 URL，理由同主机页：排障时链接要能直接发给同事
  const { health, page, size, q: keyword } = useSearch({ from: '/resources/certs' })
  const [acmeOpen, setAcmeOpen] = useState(false)
  const [applyOpen, setApplyOpen] = useState(false)
  const navigate = useNavigate({ from: '/resources/certs' })
  const patch = (next: Partial<{ health: CertHealth; q: string; page: number; size: number }>) =>
    void navigate({ search: (prev) => ({ ...prev, ...next }) })

  const query = useCerts({ page, size, health, q: keyword })

  const labels: Record<NoValueKind, string> = {
    na: '—',
    notIngested: t('common:state.notIngested'),
    stopped: t('common:state.unknown'),
    unknown: t('common:state.unknown'),
  }
  const columns = useMemo(() => certColumns(t, locale, labels), [t, locale, labels])
  const toLoadError = useMemo(
    () =>
      (e: unknown): LoadError => {
        const n = toErrorInfo(e)
        return { cause: tError(t, n.messageKey, n.params), detail: n.detail, retryable: n.retryable, kind: n.kind === 'license' ? 'license' : undefined, feature: n.params?.feature as string | undefined }
      },
    [t],
  )

  const state = fromQuery<CertListResult>(query, (d) => d.total === 0, toLoadError)

  // 空态下 facets/total 都取不到 —— 各自不渲染，而不是显示 0
  const hf = query.data?.facets?.health
  const urgent = (hf?.expired ?? 0) + (hf?.failing ?? 0)
  const total = query.data?.total

  // ⚠️ 工具条**必须在 AsyncBoundary 外面**。
  // 写在 children 里的话，空态分支下整条工具条不渲染 —— 而"接入/同步"按钮就在上面，
  // 于是全新安装的人永远接不进第一条数据。判据：工具条只依赖筛选状态，不依赖数据。

  return (
    <div className="flex flex-col">
          <div className="flex flex-wrap items-center gap-2 border-b border-border px-4 py-3">
            <SearchInput
              value={keyword}
              onChange={(v) => patch({ q: v })}
              placeholder={t('certs:filter.searchPlaceholder')}
              clearLabel={t('common:filter.clearSearch')}
              className="w-[228px]"
            />
            <Select<CertHealth>
              label={t('certs:filter.health')}
              value={health}
              onChange={(v) => patch({ health: v })}
              options={[
                { value: 'all', label: t('common:filter.all'), count: hf?.all },
                { value: 'expired', label: t('certs:health.expired'), count: hf?.expired },
                { value: 'failing', label: t('certs:health.failing'), count: hf?.failing },
                // 「读不出到期日」排在「快到期」前面：我们对它一无所知，
                // 它完全可能已经过期了
                { value: 'unknown', label: t('certs:health.unknown'), count: hf?.unknown },
                { value: 'soon', label: t('certs:health.soon'), count: hf?.soon },
                { value: 'ok', label: t('certs:health.ok'), count: hf?.ok },
              ]}
            />
            {urgent > 0 && health === 'all' ? (
              <span className="text-xs text-danger">
                {t('certs:urgentNote', { count: urgent })}
              </span>
            ) : null}
            {total != null ? (
              <span className="ml-auto text-xs text-muted-foreground">
                {t('certs:total', { count: total })}
              </span>
            ) : (
              <span className="ml-auto" />
            )}
            {/* ACME 账号入口放在证书页：没有它自动续期会逐张失败，
                而那个失败只在定时任务的执行记录里能看到 */}
            <Button size="sm" onClick={() => setInspect(true)}>
          {t('certs:inspect.entry')}
        </Button>
        <WriteButton
              perm="cmdb:manage_integrations"
              size="sm"
              onClick={() => setAcmeOpen(true)}
            >
              {t('certs:acme.entry')}
            </WriteButton>
            {/* ⚠️ 申请证书的入口以前完全没有 —— 这一页只能配 ACME 账号，
                然后看着证书过期。后端 POST /api/certs 一直都在 */}
            <WriteButton
              perm="cmdb:issue_cert"
              variant="primary"
              size="sm"
              onClick={() => setApplyOpen(true)}
            >
              {t('certs:apply.entry')}
            </WriteButton>
          </div>

      <AsyncBoundary
        state={state}
        errorTitle={t('certs:error.title')}
        retryLabel={t('common:action.retry')}
        onRetry={() => void query.refetch()}
        pending={
          <div className="p-5">
            <TableSkeleton columns={[26, 8, 14, 10, 12, 14, 12]} rows={5} />
          </div>
        }
        empty={
          <EmptyState
            icon={<ShieldCheck />}
            title={t('certs:empty.title')}
            reason={
              keyword !== '' || health !== 'all'
                ? t('certs:empty.filtered')
                : t('certs:empty.noSource')
            }
            action={
              keyword !== '' || health !== 'all'
                ? { label: t('common:filter.clearAll'), onClick: () => patch({ q: '', health: 'all' }) }
                : null
            }
          />
        }
      >
        {(data) => {
          // 过期与续期失败单独数一份：它们不是普通取值，而是待办

          return (
            <>
              {/* 🔴 到期日整片为空时，必须说清为什么，并给出去哪儿处理。
                  不说的话一页空白的到期日看起来就是"数据还没采到"——
                  而真相可能是「证书临期提醒根本不工作」。
                  生产实测：890 张证书里 828 张没有到期日，因为「证书到期检测（443）」
                  任务被停用且从没跑过；两张生产网关证书因此活到剩 28 小时才被别的途径发现。
                  ⚠️ never 用 bad 不用 warn：这不是"数据旧了"，是一整类保护措施没在运行。 */}
              {data.caveat ? (
                <div className="px-5 pt-3">
                  <Banner tone={data.caveat.kind === 'never' ? 'bad' : 'warn'}>
                    <span className="font-medium">{t('certs:caveat.title')}</span>
                    <span className="mt-0.5 block">
                      {probeNoteText(t, data.caveat.note_key, data.caveat.note_params)}
                    </span>
                  </Banner>
                </div>
              ) : null}
              <DataTable data={data.items} columns={columns} rowKey={(c) => String(c.ciId)} />
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

      {acmeOpen ? <AcmeDialog onClose={() => setAcmeOpen(false)} /> : null}
      {applyOpen ? <ApplyCertDialog onClose={() => setApplyOpen(false)} /> : null}
      {inspect ? <CertInspectDialog onClose={() => setInspect(false)} t={t} /> : null}
    </div>
  )
}
