import { keyedText } from '../../lib/hintText.js'
import { toErrorInfo } from '@ops/api'
import { formatNumber, formatRelativeTime, tError, type Locale, useTranslation } from '@ops/i18n'
import { AsyncBoundary, Badge, EmptyState, type LoadError, Select, Skeleton, fromQuery } from '@ops/ui'
import { useNavigate, useSearch } from '@tanstack/react-router'
import { Database } from 'lucide-react'
import { useState } from 'react'
import { WriteButton } from '../../components/WriteButton.js'
import { RegistryDialog } from './RegistryDialog.js'
import {
  type HarborProject,
  type HarborRegistry,
  type HarborRepo,
  useDeleteRegistry,
  useHarborProjects,
  useHarborRepos,
  useRegistries,
  useHarborStatus,
  useTestRegistry,
} from './queries.js'

const PERM = 'cmdb:manage_integrations'

export function RegistryPage() {
  const { t, i18n } = useTranslation()
  const locale = i18n.language as Locale
  const { registry, project } = useSearch({ from: '/resources/registry' })
  const navigate = useNavigate({ from: '/resources/registry' })

  const regs = useRegistries()
  const list = regs.data?.items ?? []
  const current = registry > 0 ? registry : (list[0]?.id ?? 0)
  const projects = useHarborProjects(current)
  const projList = projects.data?.projects ?? []
  const currentProject = project !== '' ? project : (projList[0]?.name ?? '')
  const repos = useHarborRepos(current, currentProject)
  const [editing, setEditing] = useState<HarborRegistry | null | undefined>(undefined)
  const del = useDeleteRegistry()
  const test = useTestRegistry()
  const currentReg = list.find((r) => r.id === current)
  // Harbor 自身健康 + GC 状态（OPSCMDB-021）。GC 没跑过 = 删掉的镜像一直占盘
  const status = useHarborStatus(current)

  const toLoadError = (e: unknown): LoadError => {
    const n = toErrorInfo(e)
    return { cause: tError(t, n.messageKey, n.params), detail: n.detail, retryable: n.retryable, kind: n.kind === 'license' ? 'license' : undefined, feature: n.params?.feature as string | undefined }
  }

  const gc = status.data?.gc

  if (!regs.isPending && list.length === 0) {
    return (
      <div className="p-5">
        <EmptyState
          icon={<Database />}
          title={t('registry:empty.title')}
          reason={t('registry:empty.noSource')}
          // 原来这里是 null —— 一个"还没接入"的空态却不给接入入口，
          // 等于告诉用户"你缺东西"然后让他自己去找在哪补
          action={{ label: t('registry:action.add'), onClick: () => setEditing(null) }}
        />
        {editing !== undefined ? (
          <RegistryDialog initial={editing ?? undefined} onClose={() => setEditing(undefined)} />
        ) : null}
      </div>
    )
  }

  return (
    <div className="flex flex-col">
      <div className="flex flex-wrap items-center gap-2 border-b border-border px-4 py-3">
        <Select<string>
          label={t('registry:filter.registry')}
          value={String(current)}
          onChange={(v) => void navigate({ search: { registry: Number(v), project: '' } })}
          options={list.map((r) => ({ value: String(r.id), label: r.name }))}
        />
        <Select<string>
          label={t('registry:filter.project')}
          value={currentProject}
          onChange={(v) => void navigate({ search: { registry: current, project: v } })}
          options={projList.map((p) => ({ value: p.name, label: p.name, count: p.repo_count }))}
        />
        {/* 实时查询：Harbor 挂了这一页就打不开，而那正是该被看见的 */}
        <span className="text-xs text-muted-foreground">{t('registry:liveNote')}</span>

        <div className="ml-auto flex items-center gap-2">
          {/* 测连通的结果直接摆出来，不用弹窗：这一页本来就是在看仓库状态，
              多一个弹窗只会挡住下面的内容 */}
          {test.isSuccess ? (
            <span className="text-xs text-success">
              {t('common:write.testOk')}
              {test.data?.version ? ` · ${test.data.version}` : ''}
            </span>
          ) : null}
          {test.isError ? (
            <span
              className="max-w-[260px] truncate text-xs text-danger"
              title={toErrorInfo(test.error).detail}
            >
              {toErrorInfo(test.error).detail || t('common:write.testFail')}
            </span>
          ) : null}
          <WriteButton
            perm={PERM}
            size="sm"
            loading={test.isPending}
            blockedReason={current ? undefined : t('registry:action.pickFirst')}
            onClick={() => test.mutate(current)}
          >
            {t('common:write.test')}
          </WriteButton>
          <WriteButton
            perm={PERM}
            size="sm"
            blockedReason={currentReg ? undefined : t('registry:action.pickFirst')}
            onClick={() => currentReg && setEditing(currentReg)}
          >
            {t('common:write.edit')}
          </WriteButton>
          <WriteButton perm={PERM} variant="primary" size="sm" onClick={() => setEditing(null)}>
            {t('registry:action.add')}
          </WriteButton>
        </div>
      </div>

      {editing !== undefined ? (
        <RegistryDialog initial={editing ?? undefined} onClose={() => setEditing(undefined)} />
      ) : null}

      <div className="mx-auto w-full max-w-[940px] p-5">
        {/* Harbor 健康：⚠️ GC 从没跑过的话，删掉的镜像一直占着磁盘 —— 
          这件事在项目/仓库列表上完全看不出来 */}
      {status.data?.ok ? (
        <div className="mb-3 flex flex-wrap items-center gap-2.5 rounded-[var(--radius)] border border-border px-3 py-2 text-xs">
          <Badge tone={status.data.health === 'healthy' ? 'ok' : 'bad'}>
            {status.data.health}
          </Badge>
          <span className="text-muted-foreground">
            {t('registry:status.components', { n: status.data.component_count ?? 0 })}
          </span>
          {gc?.issue ? (
            <span className="text-warning">{keyedText(t, gc, 'issue')}</span>
          ) : gc?.last_at ? (
            <span className="text-muted-foreground">{t('registry:status.gcAt', { at: gc.last_at })}</span>
          ) : null}
        </div>
      ) : null}

      <AsyncBoundary
          state={fromQuery<{ projects: HarborProject[]; count: number }>(projects, () => false, toLoadError)}
          errorTitle={t('registry:error.projects')}
          retryLabel={t('common:action.retry')}
          onRetry={() => void projects.refetch()}
          pending={<Skeleton className="h-5 w-[60%]" />}
          empty={null}
        >
          {(p) => <Quota projects={p.projects} locale={locale} t={t} />}
        </AsyncBoundary>

        <div className="mt-5">
          <AsyncBoundary
            state={fromQuery<{ repositories: HarborRepo[]; count: number }>(
              repos,
              (d) => d.count === 0,
              toLoadError,
            )}
            errorTitle={t('registry:error.repos')}
            retryLabel={t('common:action.retry')}
            onRetry={() => void repos.refetch()}
            pending={<Skeleton className="h-5 w-[80%]" />}
            empty={
              <EmptyState
                icon={<Database />}
                title={t('registry:emptyRepos.title')}
                reason={t('registry:emptyRepos.reason')}
                action={null}
              />
            }
          >
            {(d) => <Repos repos={d.repositories} locale={locale} t={t} />}
          </AsyncBoundary>
        </div>
      </div>
    </div>
  )
}

function Quota({
  projects,
  locale,
  t,
}: {
  projects: HarborProject[]
  locale: Locale
  t: (k: string, o?: Record<string, unknown>) => string
}) {
  return (
    <section className="rounded-[var(--radius-lg)] border border-border bg-card p-4">
      <h2 className="text-sm font-semibold text-foreground">{t('registry:quota')}</h2>
      <div className="mt-3 flex flex-col gap-1.5">
        {projects.map((p) => (
          <div key={p.name} className="flex items-center gap-3 text-[13px]">
            <span className="flex w-[160px] shrink-0 items-center gap-1.5">
              <span className="truncate text-foreground">{p.name}</span>
              {/* 🔴 公开项目 = **不用登录就能拉镜像**。旧版有「可见性」列。
                  一个本该私有的项目被建成公开的，界面上此前完全看不出来 ——
                  而这属于暴露面，不是一条普通属性。
                  ⚠️ 只标公开、不标私有：私有是默认且安全的那一档，
                  给它也挂个徽章会把真正要看的那个淹掉。 */}
              {p.public ? (
                <span title={t('registry:publicHint')}>
                  <Badge tone="warn">{t('registry:public')}</Badge>
                </span>
              ) : null}
            </span>
            <span className="tabular w-[70px] shrink-0 text-right text-muted-foreground">
              {/* ⚠️ null = 取不到（账号没权限），不是 0 GB。
                  显示 0 会让 198 个仓库的项目看起来是空的 */}
              {p.used_gb === null || p.used_gb === undefined
                ? <span className="text-warning" title={t('registry:usedUnknownHint')}>{t('registry:usedUnknown')}</span>
                : `${p.used_gb} GB`}
            </span>
            <span className="min-w-0 flex-1">
              {/* ⚠️ quota -1 = **未设配额**，不是"配额 0"。
                  画成 0% 的进度条会让人以为用量健康，而真相是根本没有上限在管它 */}
              {p.quota_gb < 0 ? (
                <span className="text-xs text-muted-foreground">{t('registry:noQuota')}</span>
              ) : (
                <span className="flex items-center gap-2">
                  <span className="h-1.5 flex-1 overflow-hidden rounded-full bg-border">
                    <span
                      className={`block h-full ${p.used_pct >= 90 ? 'bg-danger' : p.used_pct >= 75 ? 'bg-warning' : 'bg-primary'}`}
                      style={{ width: `${Math.min(100, Math.max(0, p.used_pct))}%` }}
                    />
                  </span>
                  <span className="tabular w-[52px] text-right text-xs">{p.used_pct}%</span>
                </span>
              )}
            </span>
            <span className="tabular w-[70px] shrink-0 text-right text-xs text-muted-foreground">
              {t('registry:repos', { count: p.repo_count })}
            </span>
          </div>
        ))}
      </div>
    </section>
  )
}

function Repos({
  repos,
  locale,
  t,
}: {
  repos: HarborRepo[]
  locale: Locale
  t: (k: string, o?: Record<string, unknown>) => string
}) {
  return (
    <section className="rounded-[var(--radius-lg)] border border-border bg-card p-4">
      <h2 className="text-sm font-semibold text-foreground">{t('registry:repoList')}</h2>
      {/*
        🔴 用**表格**而不是裸 div 排版（CONVENTIONS §2.7.8）。

        原来是卡片式裸排版，中间那个数字（12,932）没有任何列头或标签 ——
        读者只能猜它是什么（OPSCMDB-031 P2-22）。而那条约定的理由
        正是"没有表头时读者只能猜"。

        「20 版本」自带单位所以还能读懂，恰好证明了问题：
        同一行里有单位的看得懂、没单位的看不懂。
      */}
      <table className="mt-2.5 w-full text-[13px]">
        <thead>
          <tr className="border-b border-border text-left text-xs text-muted-foreground">
            <th className="py-1.5 font-medium">{t('registry:col.repo')}</th>
            <th className="py-1.5 text-right font-medium">{t('registry:col.artifacts')}</th>
            <th className="py-1.5 text-right font-medium" title={t('registry:col.pullsHint')}>
              {t('registry:col.pulls')}
            </th>
            <th className="py-1.5 text-right font-medium">{t('registry:col.lastPush')}</th>
          </tr>
        </thead>
        <tbody>
          {repos.map((r) => (
            <tr key={r.name} className="border-b border-border last:border-b-0">
              <td className="max-w-0 truncate py-2 font-mono text-xs text-foreground" title={r.name}>
                {r.name}
              </td>
              <td className="tabular w-[80px] py-2 text-right text-muted-foreground">
                {formatNumber(r.artifacts, locale)}
              </td>
              <td className="tabular w-[80px] py-2 text-right text-muted-foreground">
                {formatNumber(r.pulls, locale)}
              </td>
              <td
                className={`w-[100px] py-2 text-right text-xs ${r.days_since_push >= 180 ? 'text-warning' : 'text-muted-foreground'}`}
              >
                {r.updated ? formatRelativeTime(r.updated, locale) : t('common:state.unknown')}
              </td>
            </tr>
          ))}
        </tbody>
      </table>
      {/* 长期没推 ≠ 可以删：回滚可能就指着某个老版本 */}
      <p className="mt-2 text-[11px] text-muted-foreground">{t('registry:staleHint')}</p>
    </section>
  )
}
