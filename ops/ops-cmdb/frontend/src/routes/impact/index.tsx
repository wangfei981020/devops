import { toErrorInfo } from '@ops/api'
import { tError, useTranslation , formatList } from '@ops/i18n'
import {
  AsyncBoundary,
  Badge,
  EmptyState,
  type LoadError,
  SearchInput,
  Skeleton,
  fromQuery,
} from '@ops/ui'
import { useNavigate, useSearch } from '@tanstack/react-router'
import { Workflow } from 'lucide-react'
import { useState } from 'react'
import { useEntries } from '../relations/graphQueries.js'
import { type Impact, useImpact } from './queries.js'

export function ImpactPage() {
  const { t } = useTranslation()
  const { ci } = useSearch({ from: '/topology/impact' })
  const navigate = useNavigate({ from: '/topology/impact' })
  const query = useImpact(ci)
  const toLoadError = (e: unknown): LoadError => {
    const n = toErrorInfo(e)
    return { cause: tError(t, n.messageKey, n.params), detail: n.detail, retryable: n.retryable, kind: n.kind === 'license' ? 'license' : undefined, feature: n.params?.feature as string | undefined }
  }

  return (
    <div className="flex flex-col">
      {/*
        🔴 原来这里是一个 `type="number"` 的输入框，只收内部数字 ID。
        
        两个问题叠在一起，导致这一页在界面上**走不通**：
          1. 本轮验证的 21 个列表页**没有一个显示资源 ID**
             （主机/节点/Pod/工作负载/服务/域名/存储卷/…），
             而空态却写着「输入资源 ID（列表页每行都能拿到）」——
             指向一个不存在的位置（P1-28，与证书页空态指向不存在的菜单同类）
          2. 运维手里有的是**名字**（gke-uat-…-fw2h、g32cf.com），不是内部 ID。
             要求先把名字转成 ID，等于把系统的内部实现暴露成了使用前提（P1-29）
        
        ⚠️ 对照 MCP 侧：`node_impact` 接受的是**节点名**。
        同一个能力，AI 用名字、界面要 ID —— 能力早就有，只是没接给人用。
        
        改成按名字搜（复用关系图谱页已有的 entries 接口，不另造一个）。
      */}
      <div className="flex flex-wrap items-center gap-2 border-b border-border px-4 py-3">
        <ResourcePicker
          currentId={ci}
          onPick={(id) => void navigate({ search: { ci: id } })}
          t={t}
        />
      </div>

      <div className="mx-auto w-full max-w-[820px] p-5">
        {ci === 0 ? (
          <EmptyState
            icon={<Workflow />}
            title={t('impact:pick.title')}
            reason={t('impact:pick.reason')}
            action={null}
          />
        ) : (
          <AsyncBoundary
            state={fromQuery<Impact>(query, () => false, toLoadError)}
            errorTitle={t('impact:error.title')}
            retryLabel={t('common:action.retry')}
            onRetry={() => void query.refetch()}
            pending={<Skeleton className="h-5 w-[60%]" />}
            empty={null}
          >
            {(d) => (
              <div className="flex flex-col gap-3">
                <h2 className="text-sm font-semibold text-foreground">
                  {t('impact:title', { name: d.root })}
                </h2>

                {/* ⚠️ 「没有关系数据」和「影响面为空」是两回事。
                    前者说明我们不知道，后者才是"动它不影响别人" */}
                {!d.has_relations ? (
                  <div className="rounded-[var(--radius-lg)] border border-warning/25 bg-warning-bg p-3.5">
                    <p className="text-[13px] text-warning">{t('impact:noRelations')}</p>
                  </div>
                ) : d.affected.length === 0 ? (
                  <p className="text-[13px] text-muted-foreground">{t('impact:none')}</p>
                ) : (
                  <div className="flex flex-col">
                    {d.affected.map((n) => (
                      <div
                        key={n.ci_id}
                        className="flex items-baseline gap-2.5 border-b border-border py-2 text-[13px] last:border-b-0"
                      >
                        <Badge tone={n.depth === 1 ? 'bad' : n.depth === 2 ? 'warn' : 'mute'}>
                          {t('impact:depth', { depth: n.depth })}
                        </Badge>
                        <span className="min-w-0 flex-1 truncate text-foreground">{n.name}</span>
                        <span className="text-xs text-muted-foreground">{n.type}</span>
                        <span className="font-mono text-[11px] text-muted-foreground">{n.via}</span>
                      </div>
                    ))}
                  </div>
                )}

                {d.truncated ? (
                  // 截断必须说：一份悄悄少了一半的影响面清单，比没有这份清单更危险
                  <p className="text-xs text-warning">{t('impact:truncated')}</p>
                ) : null}
              </div>
            )}
          </AsyncBoundary>
        )}
      </div>
    </div>
  )
}

/**
 * 按**名字**选资源。
 *
 * ⚠️ 复用关系图谱页的 `/api/relations/entries` —— 不另造一个搜索接口。
 * 两处各写一份的话，同一个名字在两页可能搜出不同结果，
 * 而"影响面"和"关系图"本来就该看到同一批对象。
 */
function ResourcePicker({
  currentId,
  onPick,
  t,
}: {
  currentId: number
  onPick: (id: number) => void
  t: (k: string, o?: Record<string, unknown>) => string
}) {
  const [kw, setKw] = useState('')
  // 至少 2 个字才搜，否则等于把全表拉回来
  const entries = useEntries(kw.trim().length >= 2 ? kw.trim() : '')
  const all = entries.data ?? []
  // 🔴 三态：可做影响分析的（有关系边）/ 存在但没有关系边 / 真的没有。
  //
  //	后端在"一条有边的都没搜到"时会回查主表，把 degree=0 的结果带回来。
  //	这两类**必须分开渲染**：合成一句 `No matching resource` 时，
  //	用户搜主机页第一行的 `g32-prod-db-manager` 得到"查无此物"，
  //	只会以为 CMDB 没采到这台机器（OPSCMDB-077）。
  //	而"没有关系边"的占多数：实测主机 46%、域名 79% 属于这一类。
  const list = all.filter((e) => e.degree > 0)
  const unlinked = all.filter((e) => e.degree === 0)
  const picked = list.find((e) => e.ci_id === currentId)

  return (
    <div className="flex flex-wrap items-center gap-2">
      <SearchInput
        value={kw}
        onChange={setKw}
        placeholder={t('impact:searchPlaceholder')}
        clearLabel={t('common:filter.clearSearch')}
        className="w-[280px]"
      />
      {kw.trim().length >= 2 && list.length > 0 ? (
        <div className="flex max-w-[560px] flex-wrap gap-1">
          {list.slice(0, 8).map((e) => (
            <button
              key={e.ci_id}
              type="button"
              onClick={() => onPick(e.ci_id)}
              className={`flex cursor-pointer items-center gap-1 rounded-[var(--radius)] border px-2 py-0.5 text-xs transition-colors duration-150 ${
                e.ci_id === currentId
                  ? 'border-brand bg-brand/10 text-brand-text'
                  : 'border-border text-foreground hover:bg-secondary'
              }`}
            >
              <Badge tone="mute">{e.type}</Badge>
              <span className="max-w-[200px] truncate">{e.name}</span>
            </button>
          ))}
        </div>
      ) : null}
      {/* 选中的对象要一直显示 —— 搜索框清空后人还得知道自己在看谁 */}
      {picked && kw.trim().length < 2 ? (
        <span className="text-xs text-muted-foreground">
          {t('impact:current', { name: picked.name })}
        </span>
      ) : null}
      {/* 存在但没有关系边：说清楚"资源在、只是分析不了"，并把名字列出来 —
          用户下一步该做的是去它的详情页，而不是怀疑自己记错了名字 */}
      {kw.trim().length >= 2 && !entries.isPending && list.length === 0 && unlinked.length > 0 ? (
        <span className="max-w-[560px] text-xs text-warning">
          {t('impact:existsButUnlinked', {
            names: formatList(
              t,
              unlinked.slice(0, 3).map((e) => e.name),
            ),
            count: unlinked.length,
          })}
        </span>
      ) : null}
      {kw.trim().length >= 2 && !entries.isPending && all.length === 0 ? (
        <span className="text-xs text-muted-foreground">{t('impact:noMatch')}</span>
      ) : null}
    </div>
  )
}
